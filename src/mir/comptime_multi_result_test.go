package mir

import (
	"strings"
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

// An explicit arg-form call must allocate one result per named output before
// bindOutParams can copy those results into the caller's destinations. Methods
// also exercise the receiver/self exclusion in resultTypesOfCallee.
func TestMultiResultMethodArgFormBindsAllLocalOutputs(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	mod := lowerForTest(t, `probe {
}

probe.resolve = (name str) (source-out i64, column-out i64, matches-out i64) {
    source-out = 1
    column-out = 2
    matches-out = 3
}

main = () () {
    p probe
    source i64 = -1
    column i64 = -1
    matches i64 = -1
    p.resolve('id', source, column, matches)
    print(source)
    print(column)
    print(matches)
}
`)
	assertArgFormOutputMoves(t, mod, ".resolve", 3)
}

// Output destinations are not restricted to function locals. A module global
// named as an explicit out-argument must receive the corresponding result just
// like an ordinary assignment to that global.
func TestMultiResultArgFormBindsModuleGlobalOutputs(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	mod := lowerForTest(t, `resolve = (name str) (source-out i64, column-out i64, matches-out i64) {
    source-out = 1
    column-out = 2
    matches-out = 3
}

SOURCE i64 = -1
COLUMN i64 = -1
MATCHES i64 = -1

main = () () {
    resolve('id', SOURCE, COLUMN, MATCHES)
    print(SOURCE)
    print(COLUMN)
    print(MATCHES)
}
`)
	assertArgFormOutputMoves(t, mod, "resolve", 3)
}

func assertArgFormOutputMoves(t *testing.T, mod *Module, calleeSuffix string, outputs int) {
	t.Helper()
	var call *Inst
	for i := range mod.Insts {
		inst := &mod.Insts[i]
		if inst.Op == OpCall && strings.HasSuffix(inst.Sym, calleeSuffix) {
			call = inst
			break
		}
	}
	if call == nil {
		t.Fatalf("no call to %q was lowered\n%s", calleeSuffix, mod.String())
	}
	if len(call.Results) != outputs {
		t.Fatalf("call to %q allocated %d results; want %d\n%s", call.Sym, len(call.Results), outputs, mod.String())
	}
	if len(call.Args) < outputs {
		t.Fatalf("call to %q has %d arguments; want at least %d output destinations", call.Sym, len(call.Args), outputs)
	}
	base := len(call.Args) - outputs
	for k, result := range call.Results {
		destination := call.Args[base+k]
		found := false
		for i := range mod.Insts {
			move := &mod.Insts[i]
			if move.Op == OpMove && move.Dst == NoVal && len(move.Args) == 2 &&
				move.Args[0] == result && move.Args[1] == destination {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("result %d of %q was not moved into its explicit output destination %d\n%s", k, call.Sym, destination, mod.String())
		}
	}
}
