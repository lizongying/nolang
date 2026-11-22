package mir

import "testing"

// These tests pin MODULE STORAGE for SCALAR (non-container) globals.
//
// WHY THEY EXIST
// --------------
// The registration loop and synthesizeMainForTopLevel both decided "is this
// top-level binding module storage or a local of the synthetic main?" from the
// initializer's SHAPE: a call initializer and/or a nested-block reassignment
// made them treat the binding as main-local. That is only sound for container
// types, where a helper reading the name resolves to a fresh empty container
// and the symptom is a wrong length. For a SCALAR handle the helper resolved
// the name to `undef` and passed garbage downstream.
//
// noui hit exactly that: `ED = cmp-editor()` inside the init guard, read back
// by `cmp-editor-sync` through a helper, attached the rows to handle undef
// while the real editor stayed empty (kids=0) or crashed. Measured A/B with
// the frozen compiler: the demo exited in 0.33s with no output; with this fix
// it runs all 900 frames and renders 7 rows.
//
// The language reference requires module-level data to be UPPERCASE
// (docs/docs/lang/syntax.md; isGlobalStyleName), so the shapes below are the
// documented ones. A lowercase top-level binding referenced by a function is a
// compile error by design and is NOT covered here.

// An UPPERCASE scalar global that is reassigned inside a nested block must stay
// module storage, and a helper reading it must read the global.
//
//	ED = 0
//	f = () (m i64) { m = ED }
//	(1 == 1) -> { ED = 7 }
//
// Block-only assignment is the sole legal way to reassign a top-level binding
// (a second top-level `let` of the same name is a duplicate-variable error), so
// "has a nested write" cannot be a reason to demote the binding to a main-local
// — main-editor.no's `ED = 0` + `ED = cmp-editor()` inside the init guard is
// exactly this shape.
func TestScalarGlobalWithNestedWriteIsModuleStorage(t *testing.T) {
	mod := lowerForTest(t, `ED = 0
f = () (m i64) {
    m = ED
}
(1 == 1) -> {
    ED = 7
}
r = f()
print(r.to-str())
`)
	slot := globalSlotNamed(t, mod, "ED")
	fid, ok := mod.FuncByName["f"]
	if !ok {
		t.Fatalf("function `f` not found in lowered module:\n%s", mod.String())
	}
	if !funcReferencesValue(mod, fid, slot) {
		t.Fatalf("helper `f` never references the module global `ED` (value %d): "+
			"the nested-block reassignment demoted it to a local of the synthetic "+
			"main, so `f` read undef instead of the handle.\n%s", slot, mod.String())
	}
}

// An UPPERCASE scalar global whose initializer is a runtime CALL must be module
// storage too, and the synthetic main must write the initializer's result into
// it — otherwise the helper reads the zero initializer while main holds the
// real value in a private local.
//
//	ED = os.now-us()
//	f = () (m i64) { m = ED }
//
// The initializer has to be an OPAQUE runtime call: a user function whose body
// is a literal is constant-folded away by the checker (measured: `mk = () (h
// i64) { h = 7 }; ED = mk()` lowered to a bare `const i64 7`), which would
// leave this test asserting nothing.
func TestScalarGlobalCallInitIsWrittenBack(t *testing.T) {
	mod := lowerForTest(t, `ED = os.now-us()
f = () (m i64) {
    m = ED
}
r = f()
print(r.to-str())
`)
	slot := globalSlotNamed(t, mod, "ED")
	if n := globalWriteBackMoves(mod, "main", slot); n < 1 {
		t.Fatalf("synthetic main performs %d move(s) into the global `ED` slot (id %d), "+
			"want >= 1 — the call initializer stayed in a main-local and the helper "+
			"read the zero initializer.\n%s", n, slot, mod.String())
	}
	fid, ok := mod.FuncByName["f"]
	if !ok {
		t.Fatalf("function `f` not found in lowered module:\n%s", mod.String())
	}
	if !funcReferencesValue(mod, fid, slot) {
		t.Fatalf("helper `f` never references the module global `ED` (value %d).\n%s", slot, mod.String())
	}
}

// The scalar shape WITHOUT a nested write and WITHOUT a call initializer was
// already correct; this guard keeps the two fixes from being "simplified" back
// to a shape test.
func TestScalarGlobalPlainInitStillModuleStorage(t *testing.T) {
	mod := lowerForTest(t, `ED = 7
f = () (m i64) {
    m = ED
}
r = f()
print(r.to-str())
`)
	slot := globalSlotNamed(t, mod, "ED")
	fid, ok := mod.FuncByName["f"]
	if !ok {
		t.Fatalf("function `f` not found in lowered module:\n%s", mod.String())
	}
	if !funcReferencesValue(mod, fid, slot) {
		t.Fatalf("helper `f` never references the module global `ED` (value %d).\n%s", slot, mod.String())
	}
}
