package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (redundant #{index-out=...} on provably-in-bounds reads is
// removed by `no fmt`).
//
// When the base of a container index READ is a fixed-length array `[N]T` and the
// index is statically known to lie in `[0, N)` (an integer literal, or a for-range
// loop variable whose literal bounds fit inside the array), the read can never go
// out of bounds. codegen (isSafeIndexBase) therefore stops wrapping it in an
// option, so a `#{index-out = DEF}` handling is redundant. `no fmt` must strip it.
//
// The decision is shared with codegen and `no vet` via parser/bounds.go, so that
// "fmt deletes the annotation" never causes "vet/compile immediately errors".
// These tests pin that three-way consistency.

// parseFull runs the same pipeline `no vet` uses (default parse, with safe-index
// lowering ON) so the checker-side validators see the lowered program.
func parseFull(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// TestFormatRemovesProvablyInBoundsIndexOut: a fixed-array read whose index is a
// for-range loop variable provably in `[0, N)` loses its `#{index-out=zero}`.
func TestFormatRemovesProvablyInBoundsIndexOut(t *testing.T) {
	input := "main = () {\n" +
		"    p-tmp [32]byte\n" +
		"    p []byte = with-len(32)\n" +
		"    i <- [0..32): {\n" +
		"        #{index-out=zero}\n" +
		"        p[i] = p-tmp[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if strings.Contains(out, "index-out") {
		t.Errorf("provably-in-bounds #{index-out} was not removed:\n%s", out)
	}
	if !strings.Contains(out, "p[i] = p-tmp[i]") {
		t.Errorf("statement body changed unexpectedly:\n%s", out)
	}
	// Idempotency: a second pass must not change anything.
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	if out2 := FormatProgramWithOverflow(program2, out, nil, nil); out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatRemovesAttachedInBoundsIndexOut covers the *attached* (trailing on the
// same line) annotation path.
func TestFormatRemovesAttachedInBoundsIndexOut(t *testing.T) {
	input := "get = () (res i64) {\n" +
		"    a [4]i64 = [10, 20, 30, 40]\n" +
		"    i <- [0..4): {\n" +
		"        res = a[i] #{index-out=zero}\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if strings.Contains(out, "index-out") {
		t.Errorf("attached provably-in-bounds #{index-out} was not removed:\n%s", out)
	}
}

// TestFormatPreservesIndexOutForSlice: a slice read (`[]T`, runtime length unknown)
// is NOT provably in-bounds, so its `#{index-out=zero}` must be preserved.
func TestFormatPreservesIndexOutForSlice(t *testing.T) {
	input := "get = (v []i64) (res i64) {\n" +
		"    i <- [0..4): {\n" +
		"        #{index-out=zero}\n" +
		"        res = v[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("slice (non-provable) #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestFormatPreservesIndexOutForParamBound: for-range bound is a non-literal
// parameter, so the read is not provable and the annotation is preserved.
func TestFormatPreservesIndexOutForParamBound(t *testing.T) {
	input := "get = (n i64) (res i64) {\n" +
		"    a [4]i64 = [10, 20, 30, 40]\n" +
		"    i <- [0..n): {\n" +
		"        #{index-out=zero}\n" +
		"        res = a[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("non-literal-bound #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestIndexOutRemovalConsistentWithChecker is the crux of three-way agreement:
// after `no fmt` strips a redundant `#{index-out}`, the checker (which `no vet`
// runs on the lowered program) must NOT then report the read as an unhandled
// out-of-bounds index. If fmt removed something the checker still needs, vet would
// newly error — the exact regression class shared with #{overflow} removal.
func TestIndexOutRemovalConsistentWithChecker(t *testing.T) {
	input := "main = () {\n" +
		"    p-tmp [32]byte\n" +
		"    p []byte = with-len(32)\n" +
		"    i <- [0..32): {\n" +
		"        #{index-out=zero}\n" +
		"        p[i] = p-tmp[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if strings.Contains(out, "index-out") {
		t.Fatalf("precondition: annotation should have been removed:\n%s", out)
	}
	// Feed the annotation-free output through the checker pipeline.
	res := checker.ValidateUnhandledIndex(parseFull(t, out), "src/app.no")
	for _, r := range res {
		if strings.Contains(r.Message, "越界") || r.TraceID == "idxhndld" {
			t.Fatalf("fmt removed the annotation but checker still reports unhandled index: %+v\n(source after fmt)\n%s", r, out)
		}
	}
}
