package mir

import "testing"

// TestComptimeDoesNotFoldReassignedLocalComparison pins a soundness bug in the
// implicit comptime evaluator.
//
// A local initialized to a compile-time constant (e.g. `start = -1`) and then
// reassigned inside a loop must NOT be recorded as a compile-time constant for
// folding. Before the fix, the KLet binding path entered such a name into
// l.constLocals, so a later `start < 0` comparison — whose FIRST read precedes
// the first reassignment in lowering order — folded to the constant `true`,
// corrupting runtime control flow. The real-world symptom was txt.fields
// returning the LAST character of each word ("e o e") instead of the words.
//
// The fix pre-scans the function body for multiply-written locals and excludes
// them from constLocals, so the comparison is emitted at runtime.
//
// Signal: with the fix, `start < 0` lowers as a runtime signed-less-than
// (OpLt). With the bug it is folded away and no OpLt is emitted. We use a flag
// loop so `start < 0` is the ONLY `<` comparison in g (the loop condition and
// the `i >= n` exit test use no OpLt), making the count unambiguous.
func TestComptimeDoesNotFoldReassignedLocalComparison(t *testing.T) {
	src := `
main = () () {
	g(4)
}
g = (n i64) (out i64) {
	start = -1
	i = 0
	keep = true
	(keep) {
		start < 0 -> start = i
		i = i + 1
		i >= n -> keep = false
	}
	out = start
}
`
	on := lowerSrc(t, src)
	// With the fix, `start < 0` is lowered as a runtime OpLt; with the bug it is
	// folded to a constant and the comparison disappears entirely.
	if mirCountOp(on, OpLt) == 0 {
		t.Errorf("comptime on: `start < 0` comparison was folded away; a reassigned local must be compared at runtime (got 0 OpLt, expected >= 1)")
	}
}

// TestComptimeFoldsSingleAssignmentLocal confirms the fix does NOT disable
// legitimate folding: a local written exactly once stays eligible. A single
// `let x = c` followed by a use of `x` in an arithmetic fold must still fold.
func TestComptimeFoldsSingleAssignmentLocal(t *testing.T) {
	src := `
main = () () {
	g()
}
g = () (out i64) {
	x = 5
	out = x + 3
}
`
	on := lowerSrc(t, src)
	// `x + 3` folds to 8 (x is a single-assignment constant), so the constant 8
	// must be materialized.
	if !constInts(on)[8] {
		t.Errorf("comptime on: single-assignment local `x = 5` was not folded into `x + 3`; constant 8 missing")
	}
}
