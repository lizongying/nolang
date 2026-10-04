package mir

import (
	"strings"
	"testing"
)

// ptrPointeeLeafSrc is a struct whose heap is reachable ONLY through a
// `#{inline=false}` pointer field: `outer.p` is an `inner*`, and `inner` owns an
// inline `str` leaf. The clone walk (emitPtrFieldsClone) has always given the
// pointee a private copy of that leaf; the drop walk did not free it.
const ptrPointeeLeafSrc = `
inner { name str }
outer { p inner #{inline=false} }

main = () {
    o outer
    o.p.name = 'hello'
    print(o.p.name.len())
}
`

// TestPtrFieldPointeeOwnedLeafIsFreed pins the free half of the pointer-field
// pair.
//
// emitStructDropHelper recursed into a pointee only when that pointee had
// POINTER FIELDS of its own. A pointee that owns inline leaves and nothing else
// therefore got no destructor at all: the outer destructor freed the pointee
// BLOCK and stopped, leaking every leaf buffer inside it. The clone side was
// already ahead of it (emitPtrFieldsClone deep-copies the pointee's leaves), so
// this was a pure leak, not a double free waiting to be triggered.
//
// It is invisible on stdout, which is why it needs a pin: the same program runs
// to completion and prints the right answer either way.
//
// Measured on `outer { p inner #{inline=false} }` with `o.p.name = i.to-str()`
// + `print(o.p.name)` in a loop: control 14.60 MB @200k iterations, fixed
// 8.18 MB @200k (slopes 64 -> 32 B/iter, i.e. exactly one 32-byte block), and
// the fixed run matches the NO-pointer `box { name str }` case byte-for-byte
// (8,175,616 B) — the pointee's leaf is now fully freed.
//
// The probe shape matters: the string must DEPEND on the loop variable and be
// READ. A constant (`o.p.name = 'hello world'`) is loop-invariant, so opt -O3
// deletes the entire loop (str_from_const and drop_outer both become dead) and
// BOTH sides measure a flat 1.6 MB — a false "no leak".
//
// `json.parse` is the real-world case: `json { pool json-pool #{inline=false} }`
// and `json-pool` owns five %vec buffers with no pointer fields — so
// @__nolang_drop_json_json_pool was never emitted, and the destructor freed the
// pool block while leaking all five buffers.
func TestPtrFieldPointeeOwnedLeafIsFreed(t *testing.T) {
	ir := mustEmit(t, ptrPointeeLeafSrc)
	outer := irFunc(ir, "__nolang_drop_outer")
	if outer == "" {
		t.Fatalf("no destructor emitted for the pointer-field struct:\n%s", ir)
	}
	if !strings.Contains(outer, "@__nolang_drop_inner") {
		t.Fatalf("the pointer-field destructor does not recurse into its pointee, so the pointee's owned leaves leak:\n%s", outer)
	}
}
