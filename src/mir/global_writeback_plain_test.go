package mir

import "testing"

// Regression guard for the nojq "memory runaway" defect, whose root cause was a
// DROPPED WRITE-BACK into a plain module-level global.
//
// THE BUG
// -------
// A function body that does `#{overflow=wrap}` (or any signed-integer) arithmetic
// on a module-level PLAIN global and reassigns it:
//
//	cnt i64 = 0
//	bump = () { cnt = cnt + 1 #{overflow=wrap} }
//
// The KLet (re-binding) node's value lowers to `?i64` (D6: signed int arith
// yields `?T`; D1: `#{overflow=...}` no longer demotes to plain), while the
// target `cnt` is a plain `i64` global. hir2mir's global write-back used to
// require `val type == global type`, so `?i64 != i64` made it SKIP the store and
// fall through to the "new binding" path, which binds the name to a fresh LOCAL
// slot. The plain global slot was therefore NEVER updated: nojq's lexer cursor
// `_lx-pos = _lx-pos + 1 #{overflow=wrap}` never advanced -> infinite loop that
// keeps concatenating `w = w - cd.to-str()` -> RSS blow-up (SIGKILL / OOM).
//
// THE FIX
// -------
// hir2mir's global write-back now also accepts an option value whose peeled
// element type equals the plain global's type (section 13.1 option->plain sink:
// codegen emitMove peels the payload into the plain slot). The observable is a
// MOVE whose destination is the global's slot, which pre-fix was absent.
//
// WHY THE ASSERTION IS ON THE MIR MOVE, NOT STDOUT
// ------------------------------------------------
// The runaway never terminates (a stdout test would hang), and the pre-fix value
// (`cnt=0`) is indistinguishable from "never ran" at compile time. The MIR
// decision - did the write-back into the global slot happen at all - is exactly
// the property the fix restores, and it is non-vacuous: the same predicate is
// false on the pre-fix compiler.

// cntGlobalSlot returns the global value id for a module-level `cnt` declared
// `cnt i64 = 0` and confirms it is a PLAIN (non-option) type.
func cntGlobalSlot(t *testing.T, mod *Module) ValueID {
	t.Helper()
	for _, g := range mod.Globals {
		if g.Name != "cnt" {
			continue
		}
		ty := mod.Type(g.Type)
		if ty == nil {
			t.Fatalf("global cnt has no resolvable type (id %d):\n%s", g.Type, mod.String())
		}
		if ty.Kind == KindOption {
			t.Fatalf("regression guard expects a PLAIN global, but cnt is option-typed (%s):\n%s", ty.Raw, mod.String())
		}
		return g.Init
	}
	t.Fatalf("module global `cnt` not registered (bump must read it for the accumulator to be a global):\n%s", mod.String())
	return NoVal
}

// globalWriteBackMoves counts move-like instructions in function `name` whose
// destination is `slot` - i.e. how many times the body writes back into the
// shared global slot.
func globalWriteBackMoves(mod *Module, name string, slot ValueID) int {
	fid, ok := mod.FuncByName[name]
	if !ok {
		return -1
	}
	f := mod.Func(fid)
	if f == nil {
		return -1
	}
	n := 0
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if !moveLike(inst) {
				continue
			}
			if moveDst(inst) == slot {
				n++
			}
		}
	}
	return n
}

const plainGlobalOverflowAccumSrc = `cnt i64 = 0
bump = () {
    cnt = cnt + 1 #{overflow=wrap}
}
bump()
bump()
bump()
print(cnt.to-str())
`

// The write-back half: bump must move into the plain global slot. Pre-fix, the
// `?i64 != i64` gate skipped the store and bound the name to a fresh local, so
// the global never advanced (nojq lexer runaway).
func TestPlainGlobalOverflowAccumWritesBackToGlobal(t *testing.T) {
	mod := lowerForTest(t, plainGlobalOverflowAccumSrc)
	slot := cntGlobalSlot(t, mod)
	if n := globalWriteBackMoves(mod, "bump", slot); n < 1 {
		t.Fatalf("function `bump` performs %d move(s) into the plain global `cnt` slot (id %d), "+
			"want >= 1. hir2mir's global write-back must accept an option value whose peeled "+
			"element type equals the plain global's type; on the pre-fix gate (`val type == "+
			"global type`) the `?i64 != i64` store was skipped and the name re-bound to a fresh "+
			"local, so the global never updated - nojq's `_lx-pos = _lx-pos + 1 #{overflow=wrap}` "+
			"lexer cursor stuck -> infinite concat -> RSS blow-up.\n%s", n, slot, mod.String())
	}
}

// The companion half: the SAME write-back must happen WITHOUT the explicit
// `#{overflow=wrap}` annotation. D6 types every signed-integer arithmetic result
// as `?T`, so `cnt = cnt + 1` lowers to `?i64` too and hit the identical dropped
// store on the pre-fix compiler - the annotation is not what breaks it, the
// `?i64 != i64` gate is. This guards the annotation-vs-plain spelling so both
// spellings of a global accumulator stay covered by the peel write-back.
func TestPlainGlobalPlainAccumWritesBackToGlobal(t *testing.T) {
	mod := lowerForTest(t, `cnt i64 = 0
bump = () {
    cnt = cnt + 1
}
bump()
print(cnt.to-str())
`)
	slot := cntGlobalSlot(t, mod)
	if n := globalWriteBackMoves(mod, "bump", slot); n < 1 {
		t.Fatalf("function `bump` performs %d move(s) into the plain global `cnt` slot (id %d), "+
			"want >= 1; a same-type (`i64`) write-back must still work after widening the gate.\n%s",
			n, slot, mod.String())
	}
}
