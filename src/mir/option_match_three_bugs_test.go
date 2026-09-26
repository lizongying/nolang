package mir

import (
	"regexp"
	"strings"
	"testing"
)

// These tests pin three option/match bugs fixed on 2026-09-26.
//
// WHY EVERY ASSERTION IS ON THE EMITTED IR, NOT ON STDOUT
// -------------------------------------------------------
// All three are REINTERPRETATION bugs: the program compiles, exits 0, and
// produces a plausible-but-wrong value (or, for #3, an error that names the
// wrong type). A stdout test would only pin the symptom; what is actually wrong
// is the LLVM the backend chooses, so that is what these assert.
//
//   #1 a module-level `v ?i64 = 7` global + match -> NO arm executes
//   #2 `?bool` match `ok(b) -> print(b)` is always false
//   #3 an option used as a FUNCTION PARAMETER + match -> hard opt-verify
//
// See test/README.md (the "已知 bug 清單" table) for the user-visible symptoms.

// TestOptionGlobalEmitsOptionInitializer pins bug #1.
//
// A module-level `v ?i64 = 7` used to be emitted as `@v = private global i64 7`:
// emitGlobals derives the global's LLVM type from ConstText's first token, and
// foldConstText returned the bare inner scalar `"i64 7"`. main then read @v as
// %option, so the `=== ok` test compared an %option against an ok-wrapped
// %option and never matched — every arm was skipped and rc stayed 0.
// lowerGlobalRef now folds option-typed globals through foldOptionGlobalConst.
func TestOptionGlobalEmitsOptionInitializer(t *testing.T) {
	mod := lowerForTest(t, `v ?i64 = 7
main = () {
    v: { ok(x) -> print(x) nil -> print('NIL') err -> print('ERR') }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(ir, "@v = private global %option") {
		t.Errorf("module-level ?i64 global is not emitted as an %%option constant:\n%s", ir)
	}
	// The payload must be folded too, or the global is a correctly-typed some(0).
	if !strings.Contains(ir, "i64 7") {
		t.Errorf("module-level ?i64 global lost its folded payload:\n%s", ir)
	}
}

// TestOptionBoolPeelTruncatesToI1 pins bug #2.
//
// The option slot stores a bool as a 0/1 i64, so peeling an `?bool` loads an
// i64. The bound variable's slot is i1, and coerce had no i64->i1 case, so the
// peel fell through to the "structurally incompatible" fallback and stored the
// i64 through a bitcast pointer into a 1-byte slot; the following `load i1`
// then read 0. `print(x)` still said `true` (print_option_bool truncates
// correctly), which is what made this so confusing.
var optionBoolPeelTrunc = regexp.MustCompile(`trunc i64 %mvu\d+ to i1`)

func TestOptionBoolPeelTruncatesToI1(t *testing.T) {
	mod := lowerForTest(t, `mk = () (r ?bool) { r = true }
main = () {
    z ?bool = mk()
    z: { ok(b) -> print(b) nil -> print('NIL') err -> print('ERR') }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// %mvu is emitMove's option-peel load register, so this matches the peel
	// specifically and not print_option_bool's own (already correct) trunc.
	if !optionBoolPeelTrunc.MatchString(ir) {
		t.Errorf("?bool peel does not truncate the i64 payload to i1:\n%s", ir)
	}
}

// TestOptionParamMatchPeelsBeforeClone pins bug #3.
//
// `g = (r ?str) { r: { ok(s) -> print(s) } }` failed opt-verify with
// "'%lv' defined with type '%option' but expected '%str-long'". emitMove's
// borrowed-parameter guard fired on `move str = <param>` and emitted
// `str_clone(<param>)` — correct when the parameter IS a %str-long, but for a
// `?str` parameter the loaded value is the whole %option, so the clone was
// handed an option. The guard now skips option-typed sources and lets the
// option-peel branch (which peels, then clones the payload) handle them.
func TestOptionParamMatchPeelsBeforeClone(t *testing.T) {
	mod := lowerForTest(t, `g = (r ?str) {
    r: { ok(s) -> print(s) nil -> print('NIL') err -> print('ERR') }
}
main = () {
    x ?str = 'hi'
    g(x)
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// The fix's signature: the parameter's option SLOT is addressed (peel), not
	// the whole option value loaded and cloned.
	if !strings.Contains(ir, "getelementptr") || !strings.Contains(ir, "i32 0, i32 1") {
		t.Errorf("option parameter is not peeled via its payload slot:\n%s", ir)
	}
	if !strings.Contains(ir, "load %str-long, %str-long*") {
		t.Errorf("peeled ?str payload is not loaded as %%str-long:\n%s", ir)
	}
	// Regression guard: cloning the WHOLE option is exactly the bug. It has to
	// be register-precise, because both halves are legitimate on their own —
	// the arm's tag test really does `load %option` off %p0, and str_clone
	// really is called on other (peeled) values. The bug is one register doing
	// both: loaded as %option, then handed to @str_clone.
	optLoads := regexp.MustCompile(`%(\w+) = load %option, %option\* %p0`)
	for _, m := range optLoads.FindAllStringSubmatch(ir, -1) {
		if strings.Contains(ir, "@str_clone(%str-long %"+m[1]+")") {
			t.Errorf("str_clone is still applied to the whole %%option parameter (%%%s):\n%s", m[1], ir)
		}
	}
}
