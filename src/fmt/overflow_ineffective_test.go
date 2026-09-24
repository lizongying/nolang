package fmt

import (
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (ineffective #{overflow=...} is removed by `no fmt`).
//
// When a `#{overflow = ...}` annotation governs a statement that contains no
// integer-overflow-prone arithmetic (e.g. a line that is purely string
// concatenation where the `-` is concat, not subtraction), the annotation does
// nothing and must be stripped. A whole annotation line that becomes empty after
// the overflow entry is dropped (e.g. `#{overflow=wrap}` alone, or the overflow
// portion of `#{intrinsic, overflow=wrap}`) must also disappear, and the result
// must stay idempotent (no dangling blank line).
//
// The formatter receives the relevance info from the caller (checker), matching
// the `no fmt` CLI path; it never imports the checker itself.
func TestFormatRemovesIneffectiveOverflow(t *testing.T) {
	input := "main = () {\n" +
		"    #{overflow=wrap}\n" +
		"    s = \"a\" - \"b\"\n" +
		"    #{overflow=wrap}\n" +
		"    n = 1 + 2\n" +
		"    #{intrinsic, overflow=wrap}\n" +
		"    t = \"x\" - \"y\"\n" +
		"}\n"
	expected := "main = () {\n" +
		"    s = \"a\" - \"b\"\n" +
		"\n" +
		"    #{overflow=wrap}\n" +
		"    n = 1 + 2\n" +
		"\n" +
		"    #{intrinsic}\n" +
		"    t = \"x\" - \"y\"\n" +
		"}\n"

	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if out != expected {
		t.Errorf("FormatProgramWithOverflow()\n--- got ---\n%s\n--- want ---\n%s", out, expected)
	}

	// Idempotency: a second pass (re-parsing) must not change anything.
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	relevant2, governed2 := checker.OverflowAnnotationRelevance(program2)
	out2 := FormatProgramWithOverflow(program2, out, relevant2, governed2)
	if out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatRemovesIneffectiveAttachedOverflow covers the *attached* (IDENT-
// initial) annotation path: when the ineffective overflow is attached directly
// to a string-concat statement, no dangling blank line is left behind.
func TestFormatRemovesIneffectiveAttachedOverflow(t *testing.T) {
	input := "g = () {\n" +
		"    #{overflow=wrap}\n" +
		"    s = \"a\" - \"b\"\n" +
		"}\n" +
		"\n" +
		"h = () {\n" +
		"    #{mac-arm64, overflow=wrap}\n" +
		"    s = \"x\" - \"y\"\n" +
		"}\n"
	expected := "g = () {\n" +
		"    s = \"a\" - \"b\"\n" +
		"}\n" +
		"\n" +
		"h = () {\n" +
		"\n" +
		"    #{mac-arm64}\n" +
		"    s = \"x\" - \"y\"\n" +
		"}\n"

	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if out != expected {
		t.Errorf("FormatProgramWithOverflow()\n--- got ---\n%s\n--- want ---\n%s", out, expected)
	}
}

// TestFormatPreservesEffectiveOverflow confirms that a *valid* overflow
// annotation (governing real integer arithmetic) is never stripped — the
// over-approximation is conservative, so we only remove provably-ineffective ones.
func TestFormatPreservesEffectiveOverflow(t *testing.T) {
	input := "f = (a i64, b i64) (c i64) {\n" +
		"    #{overflow=wrap}\n" +
		"    c = a - b\n" +
		"}\n"
	expected := "f = (a i64, b i64) (c i64) {\n" +
		"\n" +
		"    #{overflow=wrap}\n" +
		"    c = a - b\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if out != expected {
		t.Errorf("effective overflow was stripped (should be preserved):\n--- got ---\n%s\n--- want ---\n%s", out, expected)
	}
}

// parseForTest parses source with the same options the `no fmt` CLI uses.
func parseForTest(src string) *parser.Program {
	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	return p.ParseProgram()
}
