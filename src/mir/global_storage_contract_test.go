package mir

import "testing"

// These two guards pin the module-global storage contract. Both defects were
// silent: the program compiled, ran, and printed the WRONG value with rc=0.
//
// WHY THE ASSERTION IS ON THE MIR, NOT STDOUT
// -------------------------------------------
// `lowerForTest` stops at the lowered module, so stdout is not available here;
// and the property that matters is a structural one — "does a store into the
// shared slot exist at all". The end-to-end halves live in
// compiler/tests/probe-glob.no (owned []str global filled by a helper, read
// back by another helper) and the accumulator shape below, both measured
// against the built compiler.

// cntGlobal(t, mod) is declared in global_writeback_plain_test.go and returns
// the value id of the module global `cnt`.

// A module-level accumulator whose right-hand side is an OPTION MATCH must
// still write into the global.
//
//	D6: `cnt = cnt + 1` alone is an option-into-plain type error, so every real
//	accumulator is spelled with the unwrap match. hir2mir's exprCapture path
//	(the one that turns `x = <match>` into an if-chain + a shared sink slot)
//	bound the name to that sink whenever the name had no local yet — which for
//	a module global is ALWAYS — so the name became a fresh local, the
//	globalVisible write-back below was never reached, and @cnt stayed at its
//	initializer. Measured through the compiler: three `bump()` calls printed 0.
func TestPlainGlobalMatchRHSWritesBackToGlobal(t *testing.T) {
	mod := lowerForTest(t, `CNT i64 = 0
bump = () {
    CNT = CNT + 1: {
        ok -> it

        -> CNT
    }
}
bump()
print(CNT.to-str())
`)
	slot := globalSlotNamed(t, mod, "CNT")
	if n := globalWriteBackMoves(mod, "bump", slot); n < 1 {
		t.Fatalf("function `bump` performs %d move(s) into the global `CNT` slot (id %d), want >= 1. "+
			"The exprCapture path must NOT alias the name to the match sink when the name is a "+
			"module global; without the store the accumulator never advances.\n%s",
			n, slot, mod.String())
	}
}

// A DECLARATION-ONLY module global (`BUF []i64`, no initializer) that a helper
// function reads/writes must stay module storage and must NOT be inlined as a
// local of the synthetic main.
//
// The registration loop registers such a binding as a mutable global precisely
// because a helper function needs it (`declOnly`). synthesizeMainForTopLevel
// then re-decided the same question from the initializer's foldability and
// inlined an empty local %vec into main — so `add(1)` pushed into @BUF while
// `print(BUF.len())` read main's private empty copy. Measured: 0 instead of 1.
func TestDeclOnlyGlobalIsNotInlinedIntoMain(t *testing.T) {
	mod := lowerForTest(t, `BUF []i64
add = (v i64) {
    BUF.push(v)
}
add(1)
print(BUF.len().to_str())
`)
	slot := globalSlotNamed(t, mod, "BUF")
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatalf("synthetic main not found in lowered module:\n%s", mod.String())
	}
	if !funcReferencesValue(mod, fid, slot) {
		t.Fatalf("synthetic main never references the module global `BUF` (value %d): it was "+
			"inlined as a local %%vec, so main reads a private empty copy while `add` pushes "+
			"into the global.\n%s", slot, mod.String())
	}
}

// globalSlotNamed returns the value id of the module global `name`.
func globalSlotNamed(t *testing.T, mod *Module, name string) ValueID {
	t.Helper()
	for _, g := range mod.Globals {
		if g.Name == name {
			return g.Init
		}
	}
	t.Fatalf("module global %q not registered:\n%s", name, mod.String())
	return NoVal
}

// funcReferencesValue reports whether any instruction of function `fid` takes
// `v` as an operand (source or destination slot).
func funcReferencesValue(mod *Module, fid FuncID, v ValueID) bool {
	f := mod.Func(fid)
	if f == nil {
		return false
	}
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			for _, a := range inst.Args {
				if a == v {
					return true
				}
			}
		}
	}
	return false
}
