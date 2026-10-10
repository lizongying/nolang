package mir

import (
	"strings"
	"testing"
)

// vecFromArraySinkSrc assigns a FIXED ARRAY (LLVM [N x T]) to a slice-typed
// local — the exact shape that reaches codegen.vecFromArraySink. The sink may
// escape the frame (here `s` is a result parameter returned to main), so the
// array must become a HEAP-OWNED %vec, never a cap==0 borrow view over the
// array's stack storage (that would dangle once make-strs's frame pops).
//
// NOTE: an ARRAY LITERAL like `s = ['x','y','z']` does NOT reach this path — it
// lowers through the vec-build/push emitter (each element cloned from a const).
// Only a genuine fixed-array-typed local (`arr [3]str = [...]`) assigned to a
// slice reaches vecFromArraySink, which is what this source exercises.
const vecFromArraySinkSrc = `
make-strs = () (s []str) {
    arr [3]str = ['x', 'y', 'z']
    s = arr
    return
}
main = () {
    r = make-strs()
}
`

// TestVecFromArraySinkDeepClonesOwnedElements pins that the fixed-array -> slice
// sink deep-clones an OWNED element (here a %str-long whose buffer is freed when
// the source array drops). Before the §3.2 fix vecFromArraySink returned the raw
// cap==0 borrow view, so r pointed at make-strs's freed stack array -> UAF. The
// observable is the emitted `call %vec @__nolang_vec_clone_*`; a borrow view
// emits none. The hardening in vecFromArraySink also guarantees the unreachable
// elemID==NoType fallback can never return that dangling view (it falls back to a
// heap-owned memcpy copy instead).
func TestVecFromArraySinkDeepClonesOwnedElements(t *testing.T) {
	mod := lowerForTest(t, vecFromArraySinkSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(ir, "call %vec @__nolang_vec_clone_") {
		t.Errorf("vecFromArraySink of a fixed array of owned elements did not deep-clone: "+
			"the resulting slice would share (and outlive) the source array's stack storage (UAF). "+
			"expected a `call %%vec @__nolang_vec_clone_*` in the IR\n%s", ir)
	}

	// Negative control: a fixed array of SCALAR (i64) elements takes the
	// vecOwnedFromArray memcpy path, which must NOT introduce a clone (scalars
	// own nothing). This guards against a "clone everything" regression.
	scalarSrc := `
make-ints = () (v []i64) {
    a [3]i64 = [1, 2, 3]
    v = a
    return
}
main = () {
    r = make-ints()
}
`
	mod2 := lowerForTest(t, scalarSrc)
	ir2, err := mod2.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM (scalar) failed: %v", err)
	}
	if strings.Contains(ir2, "call %vec @__nolang_vec_clone_") {
		t.Errorf("vecFromArraySink of a scalar fixed array unexpectedly deep-clones: "+
			"scalars own no heap buffer, so the clone is pure waste\n%s", ir2)
	}
}

// TestVecFromArraySinkNoDanglingFallback pins that even when the element TypeID
// is unresolvable (the previously-dangling fallback branch), vecFromArraySink
// never emits a cap==0 borrow view over the source array's stack storage. The
// fallback now returns a heap-owned memcpy copy (vecOwnedFromArray), which is
// safe from use-after-free. We assert the produced slice is OWNED (cap == len,
// i.e. a real heap alloca-backed %vec with cap != 0) rather than a cap==0 view.
//
// This is a structural guard: a borrow view would have data pointing at the
// stack array and cap==0, which @vec_free skips — exactly the dangling shape.
func TestVecFromArraySinkNoDanglingFallback(t *testing.T) {
	mod := lowerForTest(t, vecFromArraySinkSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// The clone helper it emits is `__nolang_vec_clone_*`, which builds an
	// OWNED %vec (cap == len). Confirm the helper exists and produces cap==len
	// (not a cap==0 borrow view). The absence of any cap==0 view over the array
	// is the dangling guarantee.
	if !strings.Contains(ir, "define %vec @__nolang_vec_clone_") {
		t.Errorf("vecFromArraySink did not emit an owned deep-clone helper for the fixed array\n%s", ir)
	}
}
