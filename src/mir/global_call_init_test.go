package mir

import "testing"

// A GLOBAL-STYLE container initialized by a runtime call and referenced from a
// helper function must be module storage (BUG: it used to be inlined into the
// synthetic main as a local %vec, while the helper resolved the same name to a
// void const — "vec.push: needs receiver and element").
//
//	BUF []i64 = with-len(0)
//	add = (v i64) { BUF.push(v) }
//	add(7)
//
// The language reference requires module-level data to use an uppercase name
// (docs/docs/lang/syntax.md; isGlobalStyleName), so a lowercase call-initialized
// binding is a main-local by design and is NOT covered here.
func TestGlobalCallInitIsNotInlinedIntoMain(t *testing.T) {
	mod := lowerForTest(t, `BUF []i64 = with-len(0)
add = (v i64) {
    BUF.push(v)
}
add(7)
print(BUF.len().to_str())
`)
	slot := globalSlotNamed(t, mod, "BUF")
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatalf("synthetic main not found in lowered module:\n%s", mod.String())
	}
	if !funcReferencesValue(mod, fid, slot) {
		t.Fatalf("synthetic main never references module global `BUF` (value %d): a "+
			"with-len-initialized global was inlined as a local %%vec, so the helper `add` "+
			"pushes into a different (empty) global.\n%s", slot, mod.String())
	}
}

// The with-len(N) initializer of a referenced module global must actually run
// and write its sized %%vec into @BUF. Registering the global without executing
// the initializer leaves it as zeroinitializer (len 0), so BUF[N-1] is OOB and
// helper functions observe an empty vector even though the source asked for N.
func TestGlobalCallInitWritesSizedVecIntoGlobal(t *testing.T) {
	mod := lowerForTest(t, `BUF []i64 = with-len(3)
mark = () {
    BUF[0] = 10
    BUF[1] = 20
    BUF[2] = 30
}
mark()
print(BUF[2].to_str())
`)
	slot := globalSlotNamed(t, mod, "BUF")
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatalf("synthetic main not found:\n%s", mod.String())
	}
	// main must (a) emit a with-len call and (b) move the result into @BUF.
	if n := funcCallCount(mod, fid, "with-len"); n < 1 {
		t.Fatalf("synthetic main emits %d with-len call(s), want >= 1 — the global's "+
			"size initializer was dropped.\n%s", n, mod.String())
	}
	if n := globalWriteBackMoves(mod, "main", slot); n < 1 {
		t.Fatalf("synthetic main performs %d move(s) into module global `BUF` (value %d), "+
			"want >= 1 — with-len's sized %%vec never reached the global.\n%s", n, slot, mod.String())
	}
}

// funcCallCount counts OpCall instructions in fid whose symbol text contains sym.
func funcCallCount(mod *Module, fid FuncID, sym string) int {
	f := mod.Func(fid)
	if f == nil {
		return 0
	}
	n := 0
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst != nil && inst.Op == OpCall && containsFold(inst.Sym, sym) {
				n++
			}
		}
	}
	return n
}

func containsFold(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
