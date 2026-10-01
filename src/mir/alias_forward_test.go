package mir

import (
	"fmt"
	"strings"
	"testing"
)

// These tests pin tier C of the hybrid ownership model (correction A in
// NOLANG-OWNERSHIP-MODEL.md §1.2): an alias that is only ever READ costs no
// copy, while every alias that is written — or whose source is written, rebound
// or handed to a callee — keeps the conservative clone.
//
// WHY THE ASSERTIONS ARE ON THE MIR AND NOT ON STDOUT
// ---------------------------------------------------
// The whole point of the change is that the observable behaviour is IDENTICAL;
// what differs is that one deep copy is gone. A stdout test cannot see the
// difference, and neither can a golden sweep — which is exactly why the sweep
// having zero diffs is a NECESSARY but not SUFFICIENT check here. Pinning the
// instruction stream is what makes the win non-regressible: put the clone back
// and these tests fail while every program still prints the right answer.
//
// The dangerous direction is under-cloning (a silent use-after-free), so every
// test below that describes a hazard asserts that the clone is STILL THERE.

// aliasCounts returns the OpClone / OpDrop counts in func `name`, plus a
// textual op dump used in failure messages.
func aliasCounts(t *testing.T, mod *Module, name string) (clones, drops int, dump string) {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
	var b strings.Builder
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
			switch inst.Op {
			case OpClone:
				clones++
			case OpDrop:
				drops++
			}
			fmt.Fprintf(&b, "    %s\n", inst.Op)
		}
	}
	return clones, drops, b.String()
}

// TestReadOnlySliceAliasIsNotCloned is the case correction A exists for: `b = a`
// where `b` is only read and `a` is still live. Today's lowering clones
// unconditionally; with tier C the alias shares the source's buffer and there is
// exactly ONE drop.
func TestReadOnlySliceAliasIsNotCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(a[0])
    print(b[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("read-only alias still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("shared alias must leave exactly one drop: drops=%d, want 1\n%s", drops, dump)
	}
}

// TestReadOnlyAliasInsideLoopIsNotCloned: the alias is read only, but the read
// sits in a loop body, i.e. in a different block from the binding.
func TestReadOnlyAliasInsideLoopIsNotCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    i i64 = 0
    for i <- [0..3) {
        print(b[i])
    }
    print(a[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("read-only alias in a loop still cloned: clones=%d, want 0\n%s", clones, dump)
	}
}

// TestWrittenAliasStillCloned: `b[0] = 99` must not be visible through `a`, so
// the copy has to stay. This is the rule the whole safety argument rests on.
func TestWrittenAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    b[0] = 99
    print(a[0])
    print(b[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias written through was NOT cloned: clones=0, want >=1\n%s", dump)
	}
	if drops != 2 {
		t.Errorf("independent copies must each drop once: drops=%d, want 2\n%s", drops, dump)
	}
}

// TestSourceWrittenAfterAliasStillCloned: writing the SOURCE while the alias is
// live is the mirror hazard — the alias must not observe the write.
func TestSourceWrittenAfterAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    a[0] = 99
    print(a[0])
    print(b[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("source written under a live alias was NOT cloned: clones=0\n%s", dump)
	}
}

// TestReboundSourceAliasStillCloned: `a = [9,9,9]` drops a's old buffer, so an
// alias sharing it would dangle. This is the exact defect the unconditional
// binding-time clone was introduced for (hir2mir.go:2996).
func TestReboundSourceAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    a = [9, 9, 9]
    print(b[0])
    print(a[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias outliving a rebind of its source was NOT cloned: clones=0\n%s", dump)
	}
}

// TestAliasPassedToCalleeStillCloned: a callee receives a BORROWED pointer and
// can write through it (measured: `mutate(v []i64) { v[0] = 99 }` really does
// change the caller's buffer), so handing the alias to a call is a write as far
// as the alias's independence is concerned.
func TestAliasPassedToCalleeStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
mutate = (v []i64) {
    v[0] = 99
}
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    mutate(b)
    print(a[0])
    print(b[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias passed to a callee was NOT cloned: clones=0\n%s", dump)
	}
}

// TestSourcePassedToCalleeUnderAliasStillCloned is the mirror of the above: a
// callee given the SOURCE may mutate it, and the alias would then observe a
// change a private copy would not have seen.
func TestSourcePassedToCalleeUnderAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
mutate = (v []i64) {
    v[0] = 99
}
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(b[0])
    mutate(a)
    print(a[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("source passed to a callee under a live alias was NOT cloned: clones=0\n%s", dump)
	}
}

// assertEveryOperandDefined is the invariant that a deleted definition breaks,
// and it is the only thing that catches the alias-chain bug: the broken version
// deleted exactly as many clones as the fixed one, so a clone-count assertion
// passes on both.
func assertEveryOperandDefined(t *testing.T, mod *Module, name string) {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
	defined := map[ValueID]bool{}
	for _, p := range f.Params {
		defined[p] = true
	}
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := mod.Inst(iid); inst != nil && inst.Dst > NoVal {
				defined[inst.Dst] = true
			}
		}
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
				if a > NoVal && !defined[a] {
					t.Errorf("value %d used by %s has no definition", a, inst.Op)
				}
			}
		}
		if blk.Term != nil {
			for _, a := range blk.Term.Args {
				if a > NoVal && !defined[a] {
					t.Errorf("value %d used by terminator %s has no definition", a, blk.Term.Op)
				}
			}
		}
	}
}

// TestForwardedAliasChainLeavesNoDanglingValue: `c = b` where `b` was itself
// forwarded must be re-pointed at the SURVIVING source. Deciding every rewrite
// up front and applying them afterwards left `c` reading a value whose
// definition had been deleted, and the LLVM backend then refused the module
// outright — tests/mem-safety/nested-container-clone.no failed with
// "EmitLLVM: index slot: value id 124 (type %vec) has no slot in func 2".
func TestForwardedAliasChainLeavesNoDanglingValue(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    c []i64 = b
    print(a[0])
    print(b[0])
    print(c[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("read-only alias chain still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("shared aliases must leave exactly one drop: drops=%d, want 1\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestBorrowedEnumPayloadAliasIsNotForwarded: the payload peeled out of a tagged
// enum by OpEnumField is a BORROW — the enum owns the buffer and frees it, and
// on the second extraction the enum is marked as the payload's owner. Forwarding
// an alias of that peel makes the alias outlive the enum's drop.
//
// This is exactly what tests/tagged-enum-two-match.no caught: the second match's
// `for x <- items` iterated the freed payload and printed nothing at all.
func TestBorrowedEnumPayloadAliasIsNotForwarded(t *testing.T) {
	mod := lowerForTest(t, `
list-res {
    ok(items []str),
    fail,
}
main = () {
    l []str = ['p', 'q']
    q list-res = ok(l)
    q: {
        ok(items) -> {
            for x <- items: {
                print('a: ' - x)
            }
        }

        fail -> print('fail a')
    }
    q: {
        ok(items) -> {
            for x <- items: {
                print('b: ' - x)
            }
        }

        fail -> print('fail b')
    }
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias of a borrowed enum payload was forwarded: clones=0, want >=1\n%s", dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasUsedAfterSourceLastUseIsStillSafe checks the guard does not simply
// refuse everything: a read-only alias that outlives the source's last explicit
// read must still forward (the single drop is moved to cover the alias).
func TestAliasUsedAfterSourceLastUseIsStillSafe(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(b[0])
    print(b[1])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("read-only alias still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("shared alias must leave exactly one drop: drops=%d, want 1\n%s", drops, dump)
	}
}

// TestSourceDeadAliasIsMovedNotCloned pins the OTHER half of tier C (§1.2's
// third bullet): when the source has no use left after the binding, `b = a` is
// a MOVE — the alias takes over the buffer — so not even a deferred clone is
// needed, whatever the alias does with it afterwards.
//
// This is the case hir2mir's unconditional binding-time clone (hir2mir.go:3003)
// over-pays for: `b` is WRITTEN, so the read-only forwarding refuses, yet `a` is
// provably dead, so a copy is pure waste. The observable output is unchanged —
// only the allocation disappears.
func TestSourceDeadAliasIsMovedNotCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    b[0] = 99
    print(b[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("source-dead alias still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("a moved buffer must be dropped exactly once: drops=%d, want 1\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestSourceDeadAfterEarlierReadsAliasIsMoved: the source may still be READ
// before the binding — only its DEADNESS after the binding matters. This is the
// boundary the move predicate has to get right, so it gets its own case.
func TestSourceDeadAfterEarlierReadsAliasIsMoved(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    print(a[0])
    b []i64 = a
    b[0] = 99
    print(b[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("source-dead alias still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("a moved buffer must be dropped exactly once: drops=%d, want 1\n%s", drops, dump)
	}
}

// TestSourceLiveAfterBindingIsNotMoved is the guard on the above: one read of
// the source AFTER the binding keeps it live, so the copy must stay. Without
// this the "source dead" test could be passing for the wrong reason.
func TestSourceLiveAfterBindingIsNotMoved(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    b[0] = 99
    print(a[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("source still live after the binding was NOT cloned: clones=0\n%s", dump)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.2 "first write": a WRITTEN alias whose source is already dead by the time
// of the write. The copy is unobservable — nothing can read the source after
// the write — so the alias may take over the source's buffer (a MOVE).
//
// These are the cases hir2mir's unconditional binding-time clone over-pays for
// and the read-only rule refuses, yet where `a` is provably dead at the WRITE
// even though it is still live at the BINDING.
// ─────────────────────────────────────────────────────────────────────────────

// TestAliasWrittenAfterSourceLastReadIsNotCloned is the case §4.2 exists for and
// the read-only rule cannot reach: `a` is READ after the binding (so the
// "source dead ⇒ move" shortcut does not apply at the binding), but `a` is DEAD
// by the time `b[0] = 99` runs. Sharing the buffer is then indistinguishable
// from copying it.
func TestAliasWrittenAfterSourceLastReadIsNotCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(a[0])
    b[0] = 99
    print(b[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("alias written after the source's last read still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("a moved buffer must be dropped exactly once: drops=%d, want 1\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasWrittenAfterSourceLastReadInLoop: same shape, but the alias's write
// and the source's last read sit in different blocks, so the predicate has to
// hold across blocks (liveOut) rather than within one. `a`'s last read is in the
// entry block; the write is in the loop body.
func TestAliasWrittenAfterSourceLastReadInLoop(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(a[0])
    i i64 = 0
    for i <- [0..3) {
        b[i] = 7
    }
    print(b[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("alias written in a loop still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasWrittenBeforeSourceLastReadStillCloned is the guard on the above: the
// write comes BEFORE the source's last read, so a read of `a` WOULD observe the
// write and the copy must stay. Without this the positive test could be passing
// because the rule fires unconditionally.
func TestAliasWrittenBeforeSourceLastReadStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(b[0])
    b[0] = 99
    print(a[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias written while the source is still live was NOT cloned: clones=0\n%s", dump)
	}
}

// TestAliasWrittenWithSecondAliasStillCloned pins (w3): a SECOND copy of the
// source makes the relaxation unsound. `c` is read-only and therefore forwarded
// onto `a`; if `b`'s write also went to `a`, `print(c[0])` would read 99 where a
// private copy reads 1. So the relaxation must refuse `b` as soon as `a` is
// copied more than once.
func TestAliasWrittenWithSecondAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    c []i64 = a
    print(a[0])
    b[0] = 99
    print(b[0])
    print(c[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias written while a second alias of the source exists was NOT cloned: clones=0\n%s", dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasChainWrittenStillCloned pins (w4): the source is copied only once, but
// the ALIAS is copied. `c = b` would be re-pointed at `a` and could then be
// forwarded itself, turning a private copy into a third observer — a case the
// "copies of the source" count cannot see.
func TestAliasChainWrittenStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    c []i64 = b
    print(a[0])
    b[0] = 99
    print(c[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias written while a copy of the ALIAS exists was NOT cloned: clones=0\n%s", dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasOfParameterWrittenStillCloned pins (w2): a slice PARAMETER is the
// caller's buffer — `mutate(v []i64) { v[0] = 99 }` really does change the
// caller's slice — so a write through an alias of it is observable OUTSIDE the
// frame, where no liveness fact in this function can rule it out. The read-only
// path may forward a parameter because it never writes; this one may not.
func TestAliasOfParameterWrittenStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
takes = (p []i64) {
    b []i64 = p
    print(p[0])
    b[0] = 99
    print(b[0])
}
main = () {
    a []i64 = [1, 2, 3]
    takes(a)
}
`)
	clones, _, dump := aliasCounts(t, mod, "takes")
	if clones == 0 {
		t.Errorf("alias of a slice PARAMETER was written without a clone: clones=0\n%s", dump)
	}
}

// TestAliasWrittenThenEscapedStillCloned pins the escape half of (r1): the write
// itself would be allowed, but the alias is then handed to a callee, which may
// retain it past the source's single drop. An escape always keeps the clone.
func TestAliasWrittenThenEscapedStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
mutate = (v []i64) {
    v[0] = 99
}
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(a[0])
    mutate(b)
    print(b[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias written and then escaped to a callee was NOT cloned: clones=0\n%s", dump)
	}
}
