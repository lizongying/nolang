package mir

import (
	"testing"
)

// TestComptimeFoldsMultiResult: a pure function with TWO named results, called
// in arg-form (`g(5, a, b)` where a, b are caller locals that receive the out
// parameters), folds at compile time. The two results (6 = 5+1, 10 = 5*2) are
// materialised as constants and moved into a and b; the call to g disappears.
//
// Both directions are asserted (per the comptime test convention): with
// comptime ON the constants 6 and 10 exist and g's call is gone; with it OFF
// neither constant exists and g's call survives. The OFF arm proves the
// constants were produced by the fold, not by some unrelated lowering.
func TestComptimeFoldsMultiResult(t *testing.T) {
	src := `g = (n i64) (a i64, b i64) {
    a = n + 1
    b = n * 2
}
main = () () {
    a i64 = 0
    b i64 = 0
    g(5, a, b)
    print(a)
    print(b)
}
`
	on, off := comptimePair(t, src)
	if !constInts(on)[6] {
		t.Errorf("comptime on: folded result a=6 missing; consts=%v", constInts(on))
	}
	if !constInts(on)[10] {
		t.Errorf("comptime on: folded result b=10 missing; consts=%v", constInts(on))
	}
	if hasCallSym(on, "g") {
		t.Errorf("g(5, a, b) kept its call instead of folding; syms=%v", callSyms(on))
	}
	if constInts(off)[6] {
		t.Errorf("comptime off: constant 6 should not exist; consts=%v", constInts(off))
	}
	if constInts(off)[10] {
		t.Errorf("comptime off: constant 10 should not exist; consts=%v", constInts(off))
	}
	if !hasCallSym(off, "g") {
		t.Errorf("comptime off: g's call should survive; syms=%v", callSyms(off))
	}
}

// TestComptimeLeavesMultiResultRuntimeCallAlone is the soundness reverse guard:
// the SAME multi-result function called with a RUNTIME argument (a range-loop
// index) cannot fold, so the call survives and no constant leaks out of it.
func TestComptimeLeavesMultiResultRuntimeCallAlone(t *testing.T) {
	on, _ := comptimePair(t, `g = (n i64) (a i64, b i64) {
    a = n + 1
    b = n * 2
}
main = () () {
    i <- [0..3) {
        a i64 = 0
        b i64 = 0
        g(i, a, b)
        print(a)
        print(b)
    }
}
`)
	if !hasCallSym(on, "g") {
		t.Errorf("multi-result call with a runtime argument was folded away; syms=%v", callSyms(on))
	}
}
