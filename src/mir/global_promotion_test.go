package mir

import (
	"testing"
)

// globalNamed reports whether the module materialised a module-level global
// whose emitted symbol is name.
func globalNamed(mod *Module, name string) bool {
	for i := range mod.Globals {
		if mod.Globals[i].Name == name {
			return true
		}
	}
	return false
}

// THE RULE PINNED HERE
//
// A script's lowercase top-level binding is a local of the synthetic `main`
// (docs/docs/lang/syntax.md) UNLESS a function genuinely consumes it as module
// state. "Consumes" means the function reads the name or assigns it while it is
// NOT shadowed by one of that function's own parameters / named results / loop
// variables — a plain `name = expr` in a body is an ASSIGNMENT to the module
// binding, not a shadowing declaration (see funcDeclaresName).
//
// The predicate is deliberately NARROW. Dropping the global is not free,
// because several top-level shapes are NOT inlined by synthesizeMainForTopLevel
// — most importantly the integer newtype `int`, which has no registered struct
// layout. A dropped binding then vanishes entirely and later uses lower to
// void/undef or read a stale slot:
//
//	a int = 10 / b int = 2 / q = a / b   -> sdiv void undef, undef
//	q = a / 2                            -> prints 0, not 5
//	my-stdin fd = 0 / my-stdin == 0 ->   -> trace/BPT trap
//
// (test-div-mod-option, test-div-zero, test-fd-newtype.)
//
// ⚠️ This predicate does NOT defend against another module's function
// clobbering the binding — that is globalVisible's job (a module binding is
// only visible inside the module that declares it). See global_owner_test.go.

// TestLowercaseConstantWrittenBySameModuleHelperStaysGlobal pins the shape that
// a naive "a function mentions the name, so it shadows it" rule breaks: a
// helper that ACCUMULATES into a script binding. `add`'s `total = total + n` is
// an assignment to the module binding (there is no parameter shadowing it), so
// `total` must stay module storage or the helper writes a dead local:
//
//	total = 0 / add = (n i64) { total = total + n } / add(3) / add(4)
//
// prints 7 with the global and 0 without it. Measured: a `funcDeclaresName`
// that counted a plain `let` as a shadow made this print 0.
func TestLowercaseConstantWrittenBySameModuleHelperStaysGlobal(t *testing.T) {
	mod := lowerHIR(t, `total = 0
add = (n i64) {
    #{overflow=wrap}
    total = total + n
}
add(3)
add(4)
print(total)
`)
	if !globalNamed(mod, "total") {
		t.Errorf("a lowercase binding that a helper function ASSIGNS must stay a "+
			"module global (its `total = total + n` is an assignment, not a "+
			"shadowing declaration); globals=%v", globalNames(mod))
	}
}

// TestLowercaseConstantShadowedByParamStaysLocal is the collision case that
// nameDeclaredAsLocalInAnyFunc still exists for: a function whose PARAMETER
// shadows the name cannot be a consumer of the module binding, so a lowercase
// script constant of that name has no cross-frame reader and stays a local of
// main — the behaviour docs/docs/lang/syntax.md promises.
func TestLowercaseConstantShadowedByParamStaysLocal(t *testing.T) {
	mod := lowerHIR(t, `i = 9
f = (i i64) (r i64) {
    r = i
}
print('i={i}')
print(f(3))
`)
	if globalNamed(mod, "i") {
		t.Errorf("a lowercase script constant whose name a function SHADOWS with its "+
			"own parameter must stay a local of main; globals=%v", globalNames(mod))
	}
}

// TestLowercaseConstantWithoutCollisionStaysGlobal pins the other side of the
// narrow gate: with no function declaring the name there is nothing to collide
// with, so the binding keeps its module global. This is intentional — see the
// newtype note above — and a future change that drops it unconditionally must
// update this test deliberately.
func TestLowercaseConstantWithoutCollisionStaysGlobal(t *testing.T) {
	mod := lowerHIR(t, `z = 0
print(z)
`)
	if !globalNamed(mod, "z") {
		t.Errorf("a lowercase constant with no colliding function local should keep "+
			"its module global; globals=%v", globalNames(mod))
	}
}

// TestGlobalStyleNameStaysGlobal is the over-suppression guard: the documented
// spelling for module data is uppercase (or `_` + uppercase), and those MUST
// keep their module storage. TestSetTargetPlatformEmptyFallsBackToHost depends
// on the same rule for `#{mac-arm64} PV = 111`.
func TestGlobalStyleNameStaysGlobal(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"uppercase", "PV = 111\nprint(PV)\n", "PV"},
		{"underscore + uppercase", "_PV = 111\nprint(_PV)\n", "_PV"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := lowerHIR(t, tc.src)
			if !globalNamed(mod, tc.want) {
				t.Errorf("expected a module global %q; globals=%v", tc.want, globalNames(mod))
			}
		})
	}
}

// TestLowercaseConstantReadByFunctionStaysGlobal pins the other half of the
// rule: a lowercase top-level binding that a real helper function READS is
// genuine shared module state and must stay a global. std/log.no's `level` is
// exactly this shape (set-level writes it, debug/info/warn/error read it), so
// a name-based rule alone would break the logger.
func TestLowercaseConstantReadByFunctionStaysGlobal(t *testing.T) {
	mod := lowerHIR(t, `level = 1
bump = () () {
    level = 2
}
read = () (r i64) {
    r = level
}
bump()
print(read())
`)
	if !globalNamed(mod, "level") {
		t.Errorf("a lowercase binding read by a helper function must stay a global; globals=%v",
			globalNames(mod))
	}
}

// TestLowercaseConstantAssignedByFunctionStaysGlobal is the minimal form of the
// accumulator shape above: the ONLY function mentioning the name ASSIGNS it
// (`i = 0`), which is a write to the module binding, not a shadow.
func TestLowercaseConstantAssignedByFunctionStaysGlobal(t *testing.T) {
	mod := lowerHIR(t, `i = 9
f = () (r i64) {
    i = 0
    r = i
}
print('i={i}')
print(f())
`)
	if !globalNamed(mod, "i") {
		t.Errorf("a lowercase binding that a function ASSIGNS must stay a module "+
			"global; globals=%v", globalNames(mod))
	}
}
