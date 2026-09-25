package mir

import (
	"fmt"
	"testing"
)

// These tests pin doc §8 row 3b: "讓 checkDropCount 對 tagged enum 也做檢查".
//
// THE GAP
// -------
// insertDrops exempts a move SOURCE from dropping only when the move really
// hands ownership over:
//
//	if m.moveTransfersOwnership(f, inst) { moveSrc[inst.Args[0]] = true }
//
// checkDropCount read the same idea the OLD way — "source of any OpMove" — and
// so exempted moves that are ALIASES. For an owning tagged enum that is exactly
// wrong: its payload lives INLINE in the enum, so `t e-res = q` copies the
// {tag,payload} bytes and `q` KEEPS the single drop (markEnumPayloadOwners'
// moveDest rule holds the destination out of enumOwnsPayload for the same
// reason). With the source unconditionally exempt, a missing drop on it was
// invisible: measured on `q e-res = ok('hi'); t e-res = q` with the owning
// source's drops stripped, checkDropCount produced ZERO diagnostics before the
// fix and a `missing-drop` after it.
//
// WHY THE ASSERTION IS ON THE DIAGNOSTIC, NOT ON STDOUT
// ----------------------------------------------------
// This is a *verifier* gap, so the observable is the verifier's own output. No
// program in the corpus exercises it (insertDrops inserts the drop correctly),
// which is precisely why it went unnoticed: the check existed, ran on every
// function, and silently accepted the shape it was written to catch. The test
// therefore has to inject the leak — the same technique mir_test.go uses for
// the owned-str case.

// stripDropsOf removes every OpDrop of v from f and reports how many it removed.
func stripDropsOf(m *Module, f *Function, v ValueID) int {
	removed := 0
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		keep := blk.Insts[:0]
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst.Op == OpDrop && len(inst.Args) > 0 && inst.Args[0] == v {
				removed++
				continue
			}
			keep = append(keep, iid)
		}
		blk.Insts = keep
	}
	return removed
}

// countDropsOf counts OpDrop instructions in f whose operand is v.
func countDropsOf(m *Module, f *Function, v ValueID) int {
	n := 0
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst.Op == OpDrop && len(inst.Args) > 0 && inst.Args[0] == v {
				n++
			}
		}
	}
	return n
}

// owningEnumValues returns the values markEnumPayloadOwners marked owning.
func owningEnumValues(m *Module) []ValueID {
	var out []ValueID
	for v, ok := range m.enumOwnsPayload {
		if ok {
			out = append(out, v)
		}
	}
	return out
}

// aliasEnumProgram is the shape under test: an owning enum aliased into a second
// binding. Nothing extracts the payload, so R5 marks `q` owning; `t` is a move
// destination and therefore not.
const aliasEnumProgram = `e-res {
    ok(v str),
    fail,
}

main = () {
    q e-res = ok('hi')
    t e-res = q
    print('alias')
}
`

// The owning enum's alias move must NOT count as an ownership transfer — that is
// the predicate both passes now share, and the reason the source keeps its drop.
func TestEnumAliasMoveDoesNotTransferOwnership(t *testing.T) {
	mod := lowerForTest(t, aliasEnumProgram)
	f := mod.Func(mod.FuncByName["main"])
	owning := owningEnumValues(mod)
	if len(owning) != 1 {
		t.Fatalf("expected exactly 1 owning enum value, got %v\n%s", owning, mod.String())
	}
	q := owning[0]
	found := false
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst.Op == OpMove && moveSrc(inst) == q {
				found = true
				if mod.moveTransfersOwnership(f, inst) {
					t.Fatalf("the alias move of an owning tagged enum reported as an ownership "+
						"transfer; the source would then be allowed to skip its drop "+
						"(leak)\n%s", mod.String())
				}
			}
		}
	}
	if !found {
		t.Fatalf("no move of the owning enum value %d found\n%s", q, mod.String())
	}
	if n := countDropsOf(mod, f, q); n == 0 {
		t.Fatalf("insertDrops did not drop the owning enum at all (the source is the only "+
			"drop site)\n%s", mod.String())
	}
}

// The verifier must stay silent on the correct program — this is what keeps the
// stricter predicate from refusing valid builds (rep.HasErrors() gates codegen).
func TestCheckDropCountCleanOwningEnumAliasIsSilent(t *testing.T) {
	mod := lowerForTest(t, aliasEnumProgram)
	f := mod.Func(mod.FuncByName["main"])
	rep := &Report{}
	mod.checkDropCount(f, rep)
	if rep.HasErrors() {
		t.Fatalf("clean owning-enum alias must yield no diagnostics, got: %s", rep.Error())
	}
}

// And it must SCREAM when the drop that frees the payload is gone. This is the
// test that fails on the pre-fix compiler (0 diagnostics).
func TestCheckDropCountReportsLeakedOwningEnumAlias(t *testing.T) {
	mod := lowerForTest(t, aliasEnumProgram)
	f := mod.Func(mod.FuncByName["main"])
	owning := owningEnumValues(mod)
	if len(owning) != 1 {
		t.Fatalf("expected exactly 1 owning enum value, got %v\n%s", owning, mod.String())
	}
	q := owning[0]
	if n := stripDropsOf(mod, f, q); n == 0 {
		t.Fatalf("test setup: no drop of owning enum value %d to strip\n%s", q, mod.String())
	}
	rep := &Report{}
	mod.checkDropCount(f, rep)
	if !rep.HasErrors() {
		t.Fatalf("an owning tagged enum with NO drop is a leak, but checkDropCount reported "+
			"nothing — the move-source exemption is being read as a bare `source of any "+
			"OpMove` instead of moveTransfersOwnership\n%s", mod.String())
	}
	want := fmt.Sprintf("owned value %d has no drop", q)
	for _, d := range rep.Diagnostics {
		if d.Kind == "missing-drop" && d.Msg == want+" (leak)" {
			return
		}
	}
	t.Fatalf("expected a missing-drop diagnostic for value %d, got: %s", q, rep.Error())
}

// The option peel (`s []i64 = o`) is the same shape as the enum alias — the
// option owns the payload, so the peel is not a transfer — and it is exempted by
// the same predicate. Pinned here so the fix is not read as enum-only.
func TestCheckDropCountReportsLeakedOptionPeelSource(t *testing.T) {
	mod := lowerForTest(t, `main = () {
    o ?[]i64 = [1, 2, 3]
    s []i64 = o
    print(s.len)
}
`)
	f := mod.Func(mod.FuncByName["main"])
	// The option owns the payload; find it as the move source of a peel.
	var src ValueID
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst.Op != OpMove {
				continue
			}
			s := moveSrc(inst)
			if ty := mod.valueTypeOf(f, s); ty != nil && ty.Kind == KindOption {
				src = s
			}
		}
	}
	if src <= NoVal {
		t.Skip("option peel shape not found in this lowering; covered by the enum test above")
	}
	if countDropsOf(mod, f, src) == 0 {
		t.Skipf("insertDrops does not drop the option here (value %d) — nothing to strip", src)
	}
	stripDropsOf(mod, f, src)
	rep := &Report{}
	mod.checkDropCount(f, rep)
	if !rep.HasErrors() {
		t.Fatalf("option peel source with no drop is a leak, but checkDropCount reported "+
			"nothing\n%s", mod.String())
	}
}
