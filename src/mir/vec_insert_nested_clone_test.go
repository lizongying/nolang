package mir

import (
	"strings"
	"testing"
)

// vecInsertNestedCloneSrc inserts a nested slice (`[]i64`) into a `[][]i64`.
// The insert must deep-clone the element's backing buffer, just like
// vec.push and `vec[i] = x` already do — otherwise the inserted element shares
// the source local's buffer, and the source local's drop frees it out from
// under the container (UAF / double-free).
const vecInsertNestedCloneSrc = `
main = () {
    outer [][]i64
    inner []i64 = [7, 8, 9]
    outer.insert(0, inner)
    inner = [100, 200, 300]
}
`

// TestVecInsertClonesNestedVecElement pins the deep-clone of a %vec element on
// the `vec.insert(idx, x)` path.
//
// There are exactly three by-value element writes that must give a container its
// own copy of an owned element: vec.push (emitBuiltinVecPush), the index store
// (emitIndexStore), and vec.insert (emitBuiltinVecInsert). The first two already
// deep-clone %vec elements; vec.insert was the one that did NOT — it cloned only
// %str-long elements and left %vec elements sharing the source buffer. The bug
// is invisible in stdout (a small shared buffer read back fine until the source
// dropped), so the observable is the emitted IR: a `__nolang_vec_clone_*` call
// inside the insert, exactly as push emits.
//
// Before the fix this source lowered to IR containing NO `vec_clone` call; after
// the fix emitBuiltinVecInsert mirrors the push/assign branch and emits one.
func TestVecInsertClonesNestedVecElement(t *testing.T) {
	mod := lowerForTest(t, vecInsertNestedCloneSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	if !strings.Contains(ir, "call %vec @__nolang_vec_clone_") {
		t.Errorf("vec.insert of a nested slice did not deep-clone its element: "+
			"the container shares the source local's buffer (UAF/double-free). "+
			"expected a `call %%vec @__nolang_vec_clone_*` in the IR\n%s", ir)
	}

	// Negative control: a scalar-element insert must NOT introduce a clone for
	// that insert — scalars own nothing. This guards against a "clone everything"
	// regression that would waste a malloc per push/insert of scalars.
	scalarSrc := `
main = () {
    v []i64
    v.insert(0, 5)
}
`
	mod2 := lowerForTest(t, scalarSrc)
	ir2, err := mod2.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM (scalar) failed: %v", err)
	}
	if strings.Contains(ir2, "call %vec @__nolang_vec_clone_") {
		t.Errorf("vec.insert of a scalar element unexpectedly deep-clones: "+
			"scalars own no heap buffer, so the clone is pure waste\n%s", ir2)
	}
}
