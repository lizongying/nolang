package mir

import (
	"strings"
	"testing"
)

// These tests pin the LLVM type of an `?f32` option payload.
//
// WHY THE ASSERTION IS ON THE LLVM TYPE, NOT ON STDOUT
// ----------------------------------------------------
// The bug is a REINTERPRETATION, and the wrong number it produces looks like a
// perfectly plausible runtime value, so a stdout test would only pin the
// symptom. What is actually wrong is the type the codegen chooses for the
// payload slot, so that is what these tests assert.
//
// THE BUG
// -------
// MIR has no f32: both float kinds are spelled `double`
// (builtin_call.go / emitBuiltinConv). But optionPayloadLLVMType's switch
// listed only `"double", "f64"`, so an `?f32` fell through to the trailing
// `return "i64"`. The boxed double was stored into an i64-typed slot and read
// back as an integer, so the payload came out as the raw IEEE-754 bit pattern:
//
//	b ?f32 = number.f64-to-f32(3.5)
//	print(b)     -> 4615063718147915776   ; 0x400C000000000000
//
// where `?f64` (correctly typed `double`) printed 3.5 and `?i64` printed 7.
// The user-visible victim was std: `txt.to-f32` / `str.to-f32` return `?f32`,
// so every caller saw f64 bits instead of the parsed number.
//
// The same omission in the format-field dispatch (hir2mir) made
// `print('{g}')` with `g f32` a hard codegen failure:
// "interp: format field type f32 not lowered".

// TestOptionF32PayloadUsesDoubleType pins the payload-type table directly: every
// float spelling must agree, because MIR stores them all as `double`.
func TestOptionF32PayloadUsesDoubleType(t *testing.T) {
	c := &codegen{sb: &strings.Builder{}}
	for _, raw := range []string{"f32", "f64", "float", "double"} {
		if got := c.optionPayloadLLVMType(raw); got != "double" {
			t.Errorf("optionPayloadLLVMType(%q) = %q, want \"double\"", raw, got)
		}
	}
	// Sanity: the integer lane must still be i64, or the fix over-applied.
	for _, raw := range []string{"i64", "bool", "byte", "char"} {
		if got := c.optionPayloadLLVMType(raw); got != "i64" {
			t.Errorf("optionPayloadLLVMType(%q) = %q, want \"i64\"", raw, got)
		}
	}
}

// TestOptionF32PeelEmitsDoubleLoad lowers an `?f32` box/peel end to end and
// checks the emitted IR reads the payload as a double rather than as i64.
//
// This is the belt to the table test's braces: the table pins the classification,
// this pins that the classification actually reaches the emitter.
func TestOptionF32PeelEmitsDoubleLoad(t *testing.T) {
	mod := lowerForTest(t, `main = () {
    d f64 = 3.5
    b ?f32 = number.f64-to-f32(d)
    print(b)
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(ir, "= load double, double*") {
		t.Errorf("emitted IR has no `load double` for the ?f32 peel:\n%s", ir)
	}
}

// TestFormatFieldF32Lowers pins the other half: an f32 format field must be
// dispatched to the fmt-f64 helper instead of aborting codegen.
//
// The assertion is "the f32 refusal is gone", not "EmitLLVM succeeds": lowering
// this standalone snippet does not link std, so fmt-f64 is legitimately an
// unknown callee here. What the fix changes is the *classification* of `f32`,
// and the classification's failure mode is the refusal message below.
func TestFormatFieldF32Lowers(t *testing.T) {
	mod := lowerForTest(t, `main = () {
    g f32 = number.f64-to-f32(3.5)
    print('g={g}')
}
`)
	_, err := mod.EmitLLVM()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "format field type f32 not lowered") {
		t.Fatalf("f32 format field still refused after the fix: %v", err)
	}
	t.Logf("note: EmitLLVM failed for an unrelated reason (std not linked): %v", err)
}
