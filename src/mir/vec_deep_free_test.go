package mir

import (
	"strings"
	"testing"
)

// vecDeepFreeSrc has a nested slice that reaches a drop site. `mk()` builds a
// []i64, which `a.push(...)` deep-clones into a's own element buffer (see
// emitBuiltinVecPush), so dropping `a` must release that element too.
const vecDeepFreeSrc = `
mk = () (v []i64) {
    v.push(1)
    v.push(2)
}

main = () {
    a [][]i64
    a.push(mk())
    print(a.len())
}
`

// vecDeepFreeFlatSrc is the control: a slice of SCALARS owns no per-element
// heap memory, so its drop must stay the shallow @vec_free. Without this half
// the test would also pass if every drop had been made deep.
const vecDeepFreeFlatSrc = `
main = () {
    v []i64
    v.push(1)
    print(v.len())
}
`

// TestDropOfNestedSliceFreesElements pins the deep-free half of the clone/free
// pair. emitBuiltinVecPush gives a `[][]T` container its own copy of every
// element, so the drop has to release those copies: @vec_free frees only the
// outer buffer, and the elements leak one block per push. Measured before this
// existed (allocator accounting, -O0, N = 100000): a [][]i64 push loop held
// 100047 live blocks instead of 47.
//
// The assertion is on the emitted IR rather than on a runtime counter because a
// leak is invisible to stdout — the pre-fix compiler runs this program happily
// and prints the right answer.
func TestDropOfNestedSliceFreesElements(t *testing.T) {
	ir := mustEmit(t, vecDeepFreeSrc)

	// The helper exists, is specialised on the element type, and is called from
	// the drop site.
	if !strings.Contains(ir, "define void @__nolang_vec_free_") {
		t.Fatalf("no deep-free helper was emitted for a [][]i64 drop:\n%s", irFunc(ir, "_nolang_main"))
	}
	if !strings.Contains(ir, "call void @__nolang_vec_free_") {
		t.Errorf("the [][]i64 drop does not call the deep-free helper")
	}

	// The helper must free each element and then the outer buffer, and it must
	// mirror @vec_free's borrowed-view skip (cap == 0) — a view over another
	// container's storage must not have its elements freed.
	for _, want := range []string{
		"icmp eq i64 %cap, 0",                           // borrowed-view skip, same as @vec_free
		"getelementptr inbounds %vec, ptr %ptr, i64 %i", // walk the elements
		"call void @__nolang_vec_free_",                 // per-element (recursive) release
		"call void @nolang_free(ptr %ptr)",              // then the outer buffer
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("deep-free helper is missing %q", want)
		}
	}
}

// TestDropOfScalarSliceStaysShallow is the negative half: the deep free is
// applied exactly where the deep CLONE is, no wider. A `[]i64` container was
// never given private element buffers, so freeing "elements" would be a
// type-confused free of scalar data.
func TestDropOfScalarSliceStaysShallow(t *testing.T) {
	ir := mustEmit(t, vecDeepFreeFlatSrc)
	if strings.Contains(ir, "@__nolang_vec_free_") {
		t.Errorf("a scalar slice drop was routed through the deep-free helper")
	}
	if !strings.Contains(ir, "call void @vec_free(") {
		t.Errorf("a scalar slice drop no longer uses @vec_free:\n%s", irFunc(ir, "_nolang_main"))
	}
}

func mustEmit(t *testing.T, src string) string {
	t.Helper()
	mod := lowerForTest(t, src)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	return ir
}
