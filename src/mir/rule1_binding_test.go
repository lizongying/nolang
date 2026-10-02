package mir

import "testing"

// rule1_binding_test.go — the hir2mir half of RULE 1
// (NOLANG-OWNERSHIP-MODEL.md §1.1).
//
// alias_forward_test.go pins the pass that could UNDO an assignment-point copy
// (lowerDeadSourceCopiesToMoves). This file pins the place such a copy is
// CREATED: a fresh binding whose initializer is a bare name, i.e.
//
//	c = s
//
// where `c` is new and `s` is a value. That path used to bind BOTH names onto
// one SSA value — and therefore one drop — so
//
//	s = 'hello'; c = s; c[0] = "H"
//
// really did change `s` (measured before the fix: `Hello` / `Hello`; after:
// `hello` / `Hello`). The owned-`str` case is the one this file adds; owned
// slices already had their own lowering (a fresh slice binding lowers to
// OpClone directly), and the other owned kinds are deliberately still excluded
// — see the RULE 1 note in hir2mir.go and §4.2(a) of the model doc.
//
// WHY THE ASSERTION IS ON THE MIR AND NOT ON STDOUT
// -------------------------------------------------
// For the read-only shapes the program prints the same bytes either way; what
// differs is that the destination now owns its own buffer and is dropped
// separately. A stdout test cannot see that, and neither can a golden sweep.
// The clone count is the observable that makes the rule non-regressible: put
// the shared-SSA-value binding back and these tests fail while every program
// still prints the right answer.
//
// WHICH TESTS DISCRIMINATE. Run against the pre-fix compiler, the four
// live-source tests below FAIL (clones=0) and the two dead-source tests PASS.
// The dead-source pair is kept as a GUARD anyway: the new binding path emits
// the copy unconditionally, so if the liveness fix-up in insertDrops ever
// stopped turning a dead-source copy back into a move, those two would start
// cloning and fail — they pin the interaction, not the defect.
//
// The dangerous direction of the rule itself is under-cloning (a silent
// use-after-free), so the live-source tests assert the clone is PRESENT and the
// dead-source tests are paired with a live-source guard that must still clone.

// ─────────────────────────────────────────────────────────────────────────────
// Live source ⇒ the destination gets its own copy.
// ─────────────────────────────────────────────────────────────────────────────

// TestOwnedStrBindingIsCloned: `c = s` with `s` read again afterwards. Both
// names are live, so each must own its own buffer and be dropped separately.
func TestOwnedStrBindingIsCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    s = 'hello'
    c = s
    print(s)
    print(c)
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 1 {
		t.Errorf("`c = s` (live str source) was not cloned: clones=%d, want 1\n%s", clones, dump)
	}
	if drops != 2 {
		t.Errorf("two str owners must be dropped independently: drops=%d, want 2\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestOwnedStrWrittenAliasIsCloned is the observable defect this path was fixed
// for: a write through the alias must not be visible through the source, which
// is impossible while both names share one value. Under the old binding this
// test saw clones=0.
func TestOwnedStrWrittenAliasIsCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    s = 'hello'
    c = s
    c[0] = "H"
    print(s)
    print(c)
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 1 {
		t.Errorf("writing through the alias must not reach the source, but no clone was emitted: "+
			"clones=%d, want 1\n%s", clones, dump)
	}
	if drops != 2 {
		t.Errorf("two str owners must be dropped independently: drops=%d, want 2\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestOwnedStrAliasChainIsCloned: every link of `c = s`, `d = c` is its own
// copy, so the three names are three owners. A broken rewrite can delete
// exactly as many clones as a correct one, so the operand-definition check
// runs too.
func TestOwnedStrAliasChainIsCloned(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    s = 'hello'
    c = s
    d = c
    print(s)
    print(c)
    print(d)
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 2 {
		t.Errorf("str alias chain: clones=%d, want 2 (one per binding)\n%s", clones, dump)
	}
	if drops != 3 {
		t.Errorf("str alias chain: drops=%d, want 3 (one per owner)\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// NOTE — the `str`-PARAMETER shape is deliberately NOT tested here. At HEAD a
// parameter was already excluded from the alias path (`isOwnedLocal` is false
// for a parameter, so `!isOwnedLocal` held and the copy path was taken), so a
// `c = p` test passes before and after and would pin nothing. It was written,
// measured to pass on the pre-fix compiler, and removed rather than kept as an
// always-true check.

// ─────────────────────────────────────────────────────────────────────────────
// Dead source ⇒ a move, which produces no alias either.
//
// The binding path emits the copy unconditionally (it does not consult the
// source's liveness); insertDrops applies the liveness rule afterwards and
// turns a dead-source copy back into a zero-copy move.
//
// Both tests in this section PASS on the pre-fix compiler as well — the alias
// path produced a single drop then, and the move path produces a single drop
// now. They are guards on the interaction, not on the defect: they fail if the
// dead-source fix-up ever regresses into cloning.
// ─────────────────────────────────────────────────────────────────────────────

// TestOwnedStrBindingDeadSourceIsMoved: `s` is never read after the binding, so
// there is nothing to copy — the assignment is a transfer and the buffer is
// dropped exactly once.
func TestOwnedStrBindingDeadSourceIsMoved(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    s = 'hello'
    c = s
    print(c)
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("source-dead str binding still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("a moved str must be dropped exactly once: drops=%d, want 1\n%s", drops, dump)
	}
	assertEveryOperandDefined(t, mod, "main")
}

// TestOwnedStrSourceLiveAfterBindingIsNotMoved is the guard on the test above:
// one read of the source AFTER the binding keeps it live, so the copy must
// stay. Without this the dead-source test could pass for the wrong reason.
func TestOwnedStrSourceLiveAfterBindingIsNotMoved(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    s = 'hello'
    c = s
    print(c)
    print(s)
}
`)
	clones, _, dump := aliasCounts(t, mod, "main")
	if clones == 0 {
		t.Errorf("source still live after the binding was NOT cloned: clones=0\n%s", dump)
	}
}

// TestOwnedStrBindingDeadAfterEarlierReadsIsMoved pins the boundary: reads
// BEFORE the binding do not keep the source live at the copy.
func TestOwnedStrBindingDeadAfterEarlierReadsIsMoved(t *testing.T) {
	mod := lowerForTest(t, `
main = () {
    s = 'hello'
    print(s)
    c = s
    print(c)
}
`)
	clones, drops, dump := aliasCounts(t, mod, "main")
	if clones != 0 {
		t.Errorf("source dead at the copy still cloned: clones=%d, want 0\n%s", clones, dump)
	}
	if drops != 1 {
		t.Errorf("a moved str must be dropped exactly once: drops=%d, want 1\n%s", drops, dump)
	}
}
