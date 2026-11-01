package mir

import (
	"strings"
	"testing"
)

// TestBoolPrintersRenderTrueFalse pins the prelude's bool printers to the
// "true"/"false" spelling — the one bool.to-str() produces.
//
// print(b) used to write the i1 representation ("1"/"0") through @.true/@.false,
// so print(b), '{b}' and 'b=' - b disagreed with each other and with an
// explicit b.to-str(). eprint(b) additionally widened i1 to i64 and reused
// @eprint_i64, which is the same "1"/"0" leak on the stderr side.
func TestBoolPrintersRenderTrueFalse(t *testing.T) {
	src := "main = () () {\n  b = true\n  print(b)\n  eprint(b)\n}\n"
	mod, _ := lowerSrcWithDiags(t, src)
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// The shared constants (also used by @print_option_bool) must carry the
	// textual spelling, NUL-terminated so the inbounds GEP stays in bounds.
	for _, want := range []string{`c"true\00"`, `c"false\00"`} {
		if !strings.Contains(ll, want) {
			t.Fatalf("prelude is missing the %s constant", want)
		}
	}
	// No leftover 1-byte "1"/"0" constants: they were print_bool's old output.
	for _, bad := range []string{`[1 x i8] c"1"`, `[1 x i8] c"0"`} {
		if strings.Contains(ll, bad) {
			t.Fatalf("prelude still defines the integer bool constant %s", bad)
		}
	}
	// eprint(bool) must use its own printer rather than widening to i64.
	if !strings.Contains(ll, "call void @eprint_bool(i1") {
		t.Fatalf("eprint(bool) must call @eprint_bool, not widen to i64 and reuse @eprint_i64")
	}
	if strings.Contains(ll, "@eprint_i64(i64 %peb") {
		t.Fatalf("eprint(bool) still routes through @eprint_i64")
	}
}

// TestBoolInterpUsesFmtBool pins `'{b}'` to std fmt-bool. The format-field
// dispatcher used to send bool down the integer path (isIntegerMIRType accepts
// bool), rendering "1"/"0"; fmt-bool renders "true"/"false" and also honours
// the format spec (`{b:>6}`).
func TestBoolInterpUsesFmtBool(t *testing.T) {
	src := "main = () () {\n  b = true\n  print('x={b}')\n}\n"
	mod, _ := lowerSrcWithDiags(t, src)

	hasSym := func(sym string) bool {
		for i := range mod.Insts {
			if mod.Insts[i].Op == OpCall && mod.Insts[i].Sym == sym {
				return true
			}
		}
		return false
	}
	if !hasSym("fmt-bool") {
		t.Fatalf("a bool format field must lower through fmt-bool")
	}
	if hasSym("fmt-int") || hasSym("fmt-uint") {
		t.Fatalf("a bool format field must not lower through the integer formatter")
	}
}
