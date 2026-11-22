package mir

import (
	"strings"
	"testing"
)

// These tests pin the INT -> FLOAT conversion on a STRUCT FIELD store.
//
// WHY THE ASSERTION IS ON THE EMITTED IR, NOT ON STDOUT
// ------------------------------------------------------
// The bug produced INVALID IR, so the program never ran: `r.x = 10` on a
// `rect{ x f64 }` emitted `store double %lv, double* %gp` with %lv still
// defined as i64. opt rejected the module ("'%lv' defined with type 'i64' but
// expected 'double'") and clang only surfaced it at LINK time, so a stdout test
// could not exist. The observable is therefore the conversion instruction.
//
// THE BUG
// -------
// emitSetField re-derives the store type from the field DECLARATION, but the
// value keeps the RHS's own type. The int-width branch only fired when BOTH
// sides were integers, so an integer RHS reaching an f64/f32 field fell through
// to the generic `store <fieldLT> <valV>` with no sitofp. Arithmetic
// (coerceInt) and emitIndexStore both converted; the field store did not, which
// forced every f64 literal in noui to be spelled with a `.0`
// (`r.x = 10.0`, `rect-hit(r, 50.0, 30.0)`).
//
// Both a GLOBAL struct (noui's geom usage) and a function-local struct are
// covered, plus f32, so a future refactor cannot fix only one shape.
//
// WHY THE SLICE, NOT THE WHOLE MODULE
// -----------------------------------
// EmitLLVM always appends the runtime prelude, and the number renderer
// (`f64_render`) contains its own `sitofp i64 ... to double`. Asserting on the
// whole module therefore cannot distinguish the user's store from the runtime's
// conversion — an early version of TestIntLiteralToF32FieldEmitsSitofp passed
// on that unrelated `to float`. Every assertion below is scoped to the user
// entry function `@_nolang_main`.

// mainFnIR returns the emitted IR of the user's `main`, i.e. the define block
// for @_nolang_main only.
func mainFnIR(t *testing.T, ir string) string {
	t.Helper()
	const marker = "define void @_nolang_main("
	i := strings.Index(ir, marker)
	if i < 0 {
		t.Fatalf("emitted IR has no %s block:\n%s", marker, ir)
	}
	rest := ir[i+len(marker):]
	if j := strings.Index(rest, "\ndefine "); j >= 0 {
		return ir[i : i+len(marker)+j]
	}
	if j := strings.Index(rest, "\ndeclare "); j >= 0 {
		return ir[i : i+len(marker)+j]
	}
	return ir[i:]
}

func TestIntLiteralToF64FieldEmitsSitofp(t *testing.T) {
	mod := lowerForTest(t, `
rect { x f64  y f64 }
r rect
main = () {
    r.x = 10
    r.y = 20
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := mainFnIR(t, ir)
	if !strings.Contains(body, "sitofp") {
		t.Errorf("global f64 struct field store emits no sitofp (invalid `store double %%i64`):\n%s", body)
	}
}

func TestIntLiteralToLocalF64FieldEmitsSitofp(t *testing.T) {
	mod := lowerForTest(t, `
rect { x f64  y f64 }
main = () {
    r rect
    r.x = 10
    r.y = 20
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := mainFnIR(t, ir)
	if !strings.Contains(body, "sitofp") {
		t.Errorf("local f64 struct field store emits no sitofp (invalid `store double %%i64`):\n%s", body)
	}
}

// MIR has no f32: both float kinds are spelled `double` (see f32_option_payload_test.go),
// so the conversion targets `double` here too. What matters is that it exists.
func TestIntLiteralToF32FieldEmitsSitofp(t *testing.T) {
	mod := lowerForTest(t, `
box { x f32  y f32 }
main = () {
    b box
    b.x = 7
    b.y = 8
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := mainFnIR(t, ir)
	if !strings.Contains(body, "sitofp") {
		t.Errorf("f32 struct field store emits no sitofp (invalid `store double %%i64`):\n%s", body)
	}
}

// TestFloatFieldStillAcceptsFloatLiteral is the guard against over-applying the
// fix: a float RHS into a float field must NOT grow a sitofp. Scoped to the
// user function for the reason described at the top of this file.
func TestFloatFieldStillAcceptsFloatLiteral(t *testing.T) {
	mod := lowerForTest(t, `
rect { x f64  y f64 }
main = () {
    r rect
    r.x = 10.0
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := mainFnIR(t, ir)
	if strings.Contains(body, "sitofp") {
		t.Errorf("float RHS into a float field should not need sitofp:\n%s", body)
	}
}
