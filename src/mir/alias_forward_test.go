package mir

import (
	"fmt"
	"strings"
	"testing"
)

// These tests pin RULE 1 of the hybrid ownership model
// (NOLANG-OWNERSHIP-MODEL.md §1.1): a value-to-value assignment `a = b` NEVER
// produces a zero-copy alias. It is a deep copy, or — when the source is dead
// at the copy — a MOVE, which produces no alias either.
//
// This file used to pin the OPPOSITE for the read-only case: "correction A" /
// tier C deleted the copy whenever the alias was only ever read, leaving the
// two names sharing one buffer with a single drop. That is gone (2026-10-02).
// The reason is the one in §1.1: arbitrary value-to-value aliasing has no
// semantic value and costs a whole extra class of cases for the analysis to get
// right (alias chains, second aliases, cross-block aliases, parameter aliases,
// option-peel aliases). One deep copy buys back "a value has exactly one
// owner".
//
// ELEMENT AND FIELD READS ARE NOT THIS PASS'S BUSINESS. `a = b[i]` and
// `a = b.c` are borrowing reads, not OpMove/OpClone bindings, so they keep
// their view semantics — that is the "short alias" idiom, and it is what the
// borrow refcounting (OpRetain / OpRelease) exists for.
//
// WHY THE ASSERTIONS ARE ON THE MIR AND NOT ON STDOUT
// ---------------------------------------------------
// For the slice shapes the observable behaviour is identical either way — what
// differs is that a deep copy is now paid for. A stdout test cannot see that,
// and neither can a golden sweep. Pinning the instruction stream is what makes
// the rule non-regressible: put the forwarding back and these tests fail while
// every program still prints the right answer.
//
// The dangerous direction is under-cloning (a silent use-after-free), so every
// test below that describes a hazard asserts that the clone is STILL THERE, and
// every "source dead ⇒ move" test is paired with a live-source guard that must
// still clone.

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

// assertEveryOperandDefined is the invariant a deleted definition breaks, and it
// is the only thing that catches an alias-chain bug: a broken rewrite can delete
// exactly as many clones as a correct one, so a clone-count assertion passes on
// both.
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

// ─────────────────────────────────────────────────────────────────────────────
// Rule 1, positive: `a = b` with a LIVE source is a deep copy, whatever the
// alias is used for. Each of these shapes used to lose its clone.
// ─────────────────────────────────────────────────────────────────────────────

// TestReadOnlySliceAliasIsCloned is the shape correction A existed for and rule
// 1 now forbids: `b = a` where `b` is only read and `a` is still live. The copy
// must STAY — two owners, two drops.
func TestReadOnlySliceAliasIsCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    print(a[0])
    print(b[0])
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 1 {
		t.Errorf("value-to-value alias was not cloned: clones=%d, want 1\n%s", clones, dump)
	}
	if drops != 2 {
		t.Errorf("two owners must be dropped independently: drops=%d, want 2\n%s", drops, dump)
	}
}

// TestReadOnlyAliasInsideLoopIsCloned: same, but the read sits in a loop body,
// i.e. in a different block from the binding. The clone is a property of the
// BINDING, so where the alias is read cannot matter.
func TestReadOnlyAliasInsideLoopIsCloned(t *testing.T) {
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
	if clones != 1 {
		t.Errorf("alias bound outside a loop was not cloned: clones=%d, want 1\n%s", clones, dump)
	}
}

// TestAliasChainIsClonedAndLeavesNoDanglingValue: every link of `b = a`,
// `c = b` is its own copy. This also re-checks the property that made the old
// pass rewrite in program order — each operand must still have a definition,
// because the LLVM backend refuses a value with no slot
// ("EmitLLVM: index slot: value id 124 (type %vec) has no slot in func 2",
// tests/mem-safety/nested-container-clone.no).
func TestAliasChainIsClonedAndLeavesNoDanglingValue(t *testing.T) {
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
	if clones != 2 {
		t.Errorf("alias chain: clones=%d, want 2 (one per binding)\n%s", clones, dump)
	}
	if drops != 3 {
		t.Errorf("alias chain: drops=%d, want 3 (one per owner)\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasWrittenAfterSourceLastReadIsCloned: `a` is read after the binding,
// so it is live at the copy — and a write through the alias later must not be
// visible through `a`. Under the old pass the source's later death at the WRITE
// made this a share; rule 1 keeps the copy regardless.
func TestAliasWrittenAfterSourceLastReadIsCloned(t *testing.T) {
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
	if clones != 1 {
		t.Errorf("written alias was not cloned: clones=%d, want 1\n%s", clones, dump)
	}
	if drops != 2 {
		t.Errorf("two owners must be dropped independently: drops=%d, want 2\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasWrittenAfterSourceLastReadInLoop: the same shape with the write in a
// loop body, i.e. in a different block from the binding. The old predicate had
// to reason across blocks here; rule 1 does not care where the write is.
func TestAliasWrittenAfterSourceLastReadInLoopIsCloned(t *testing.T) {
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
	if clones != 1 {
		t.Errorf("alias written in a loop was not cloned: clones=%d, want 1\n%s", clones, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestBorrowedEnumPayloadAliasIsCloned: the payload peeled out of a tagged enum
// by OpEnumField is a BORROW — the enum owns the buffer and frees it. A copy of
// that peel must therefore stay a COPY: turning it into a move would hand the
// destination a buffer the enum still frees (a double free, not a leak).
//
// This is exactly what tests/tagged-enum-two-match.no caught when the "source
// dead ⇒ move" rule first shipped BELOW the borrow test: the second match's
// `for x <- items` iterated the freed payload and printed nothing at all.
func TestBorrowedEnumPayloadAliasIsCloned(t *testing.T) {
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
		t.Errorf("a borrow-sourced copy was turned into a move: clones=0, want >=1\n%s", dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// ─────────────────────────────────────────────────────────────────────────────
// Rule 1, second case: the source is DEAD at the copy, so there is nothing to
// copy — the assignment is a MOVE, which produces no alias either.
//
// These are the cases hir2mir's unconditional binding-time clone over-pays for:
// the source's liveness is not consulted there, so the transfer is paid for as
// a deep copy even when the source is provably dead.
// ─────────────────────────────────────────────────────────────────────────────

// TestSourceDeadAliasIsMovedNotCloned: `b` is written and `a` is never read
// again, so `b = a` is a plain ownership transfer.
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

// TestAliasUsedAfterSourceLastUseIsMoved: the alias outlives the source's last
// use entirely, so the binding is a move and the single drop covers the buffer.
func TestAliasUsedAfterSourceLastUseIsMoved(t *testing.T) {
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
		t.Errorf("source-dead alias still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("a moved buffer must be dropped exactly once: drops=%d, want 1\n%s", drops, dump)
	}
}

// TestSourceLiveAfterBindingIsNotMoved is the guard on the three above: one
// read of the source AFTER the binding keeps it live, so the copy must stay.
// Without this the "source dead" tests could be passing for the wrong reason.
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
// The hazards. Under the old pass these were the rules that kept the relaxation
// sound; under rule 1 they are simply cases where the copy must be present, and
// they are what makes the positive tests non-vacuous. Every one of them asserts
// the clone is STILL THERE.
// ─────────────────────────────────────────────────────────────────────────────

// TestWrittenAliasStillCloned: `b[0] = 99` must not be visible through `a`.
// This is the rule the whole safety argument rests on.
func TestWrittenAliasStillCloned(t *testing.T) {
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
		t.Errorf("written alias was not cloned: clones=0\n%s", dump)
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
// binding-time clone was introduced for (hir2mir.go, "a fresh binding has no
// pre-existing slot to move into").
func TestReboundSourceAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    a = [9, 9, 9]
    print(b[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias of a rebound source was NOT cloned: clones=0\n%s", dump)
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
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("alias handed to a callee was NOT cloned: clones=0\n%s", dump)
	}
}

// TestSourcePassedToCalleeUnderAliasStillCloned is the mirror of the above: a
// callee handed the SOURCE may write through it while the alias is live.
func TestSourcePassedToCalleeUnderAliasStillCloned(t *testing.T) {
	mod := lowerForTest(t, `
mutate = (v []i64) {
    v[0] = 99
}
main = () {
    a []i64 = [1, 2, 3]
    b []i64 = a
    mutate(a)
    print(b[0])
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("source handed to a callee under a live alias was NOT cloned: clones=0\n%s", dump)
	}
}

// TestAliasWrittenWithSecondAliasStillCloned: a SECOND copy of the source is a
// second observer. Under the old pass this was rule (w3) — the relaxation had
// to refuse `b` as soon as `a` was copied more than once, because if `c` were
// forwarded onto `a`, `b`'s write would land in `c`'s view too. Under rule 1
// every one of them is a copy, so the property holds structurally.
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
	if clones < 2 {
		t.Errorf("two copies of one source: clones=%d, want >=2\n%s", clones, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasChainWrittenStillCloned: the source is copied once, but the ALIAS is
// copied. Under the old pass `c = b` would be re-pointed at `a` and could then
// be forwarded itself, turning a private copy into a third observer — a case
// the "copies of the source" count could not see (rule (w4)).
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
	if clones < 2 {
		t.Errorf("alias chain with a write: clones=%d, want >=2\n%s", clones, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestAliasOfParameterWrittenStillCloned: a slice PARAMETER is the caller's
// buffer — `mutate(v []i64) { v[0] = 99 }` really does change the caller's
// slice — so a write through an alias of it is observable OUTSIDE the frame,
// where no liveness fact in this function can rule it out. The old read-only
// path forwarded parameters (it never wrote); rule 1 copies them.
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

// TestAliasWrittenThenEscapedStillCloned pins the escape half: the write itself
// would be allowed, but the alias is then handed to a callee, which may retain
// it past the source's single drop. An escape always keeps the clone.
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
