package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestStripLocPrefix(t *testing.T) {
	pos, clean := stripLocPrefix("line 5, column 12: something broke")
	if pos.Line != 5 || pos.Column != 12 {
		t.Fatalf("pos = %+v, want line 5 col 12", pos)
	}
	if clean != "something broke" {
		t.Fatalf("clean = %q, want %q", clean, "something broke")
	}

	pos, clean = stripLocPrefix("no prefix here")
	if pos.Line != 0 || pos.Column != 0 {
		t.Fatalf("pos = %+v, want zero", pos)
	}
	if clean != "no prefix here" {
		t.Fatalf("clean = %q", clean)
	}
}

func TestFormatDiags(t *testing.T) {
	diags := []Diagnostic{
		{Filename: "a.no", Pos: lexer.Position{Line: 1, Column: 2}, Severity: SeverityError, Code: "E_X", Message: "boom"},
		{Pos: lexer.Position{Line: 3, Column: 4}, Severity: SeverityWarning, Code: "W_Y", Message: "careful"},
		{Pos: lexer.Position{Line: 5, Column: 6}, Severity: SeverityError, Code: "E_Z", Message: "again"},
	}
	errs := formatDiags(diags, SeverityError)
	if len(errs) != 2 {
		t.Fatalf("error count = %d, want 2", len(errs))
	}
	if !strings.Contains(errs[0], "[E_X]") || !strings.Contains(errs[0], "line 1, column 2") {
		t.Fatalf("err[0] = %q, want loc + code", errs[0])
	}
	warns := formatDiags(diags, SeverityWarning)
	if len(warns) != 1 || !strings.Contains(warns[0], "[W_Y]") {
		t.Fatalf("warns = %v, want single W_Y", warns)
	}
}

// TestFormatDiagnostics pins the canonical compile-error shape used by
// `no build` / `no vet` for parser diagnostics. Three properties matter, and all
// three were violated by the old `fmt.Errorf("parser errors: %v", p.Errors())`:
//
//  1. no filename — the caller owns the file context (the "validation errors: "
//     channel does the same), and the consumer re-renders it;
//  2. the location appears ONCE, as "line L, column C: " — never again inside
//     the message, or the CLI prints it twice;
//  3. the code goes in the trailing "[CODE]" slot the checker already uses, and
//     the separator is "; " — never Go's slice rendering "[a b]", which glues
//     the tail of one message onto the head of the next.
func TestFormatDiagnostics(t *testing.T) {
	diags := []Diagnostic{
		{Filename: "sha3.no", Pos: lexer.Position{Line: 24, Column: 23}, Severity: SeverityError,
			Code: "E_GENERAL", Message: "expected comma or right parenthesis, got ASSIGN(=) instead"},
		{Filename: "sha3.no", Pos: lexer.Position{Line: 24, Column: 18}, Severity: SeverityError,
			Code: "E_GENERAL", Message: "expected expression after operator '*'"},
		{Filename: "sha3.no", Pos: lexer.Position{Line: 1, Column: 1}, Severity: SeverityWarning,
			Code: "W_X", Message: "ignored"},
	}

	got := FormatDiagnostics(diags, SeverityError)
	want := "line 24, column 23: expected comma or right parenthesis, got ASSIGN(=) instead [E_GENERAL]; " +
		"line 24, column 18: expected expression after operator '*' [E_GENERAL]"
	if got != want {
		t.Fatalf("FormatDiagnostics =\n  %q\nwant\n  %q", got, want)
	}
	if strings.Contains(got, "sha3.no") {
		t.Errorf("filename must not be embedded (caller owns the file context): %q", got)
	}
	if strings.Contains(got, "[") && strings.HasPrefix(got, "[") {
		t.Errorf("must not be Go slice rendering: %q", got)
	}

	// A diagnostic without a code keeps the bare "line L, column C: message" shape.
	bare := FormatDiagnostics([]Diagnostic{
		{Pos: lexer.Position{Line: 2, Column: 7}, Severity: SeverityError, Message: "no code here"},
	}, SeverityError)
	if bare != "line 2, column 7: no code here" {
		t.Fatalf("bare = %q", bare)
	}

	// Nothing of the requested severity -> empty string, not "[]".
	if s := FormatDiagnostics(diags, Severity(42)); s != "" {
		t.Fatalf("empty severity = %q, want \"\"", s)
	}
}

// TestStructuredDiagnosticsThroughFatalf ensures that a deep fatal error:
//   - is caught by the per-statement recover (no panic escapes ParseProgram),
//   - is recorded as a structured Diagnostic (position + code + severity),
//   - and still surfaces through the legacy Errors() []string API.
func TestStructuredDiagnosticsThroughFatalf(t *testing.T) {
	src := "1, 2 = 3\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatal("ParseProgram returned nil")
	}
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatal("expected at least one error, got none")
	}
	if !strings.Contains(errs[0], "line 1, column") {
		t.Fatalf("legacy error format lost: %q", errs[0])
	}

	diags := p.Diagnostics()
	if len(diags) == 0 {
		t.Fatal("expected structured diagnostics, got none")
	}
	d := diags[0]
	if d.Pos.Line != 1 {
		t.Fatalf("diag pos = %+v, want line 1", d.Pos)
	}
	if d.Severity != SeverityError {
		t.Fatalf("diag severity = %v, want error", d.Severity)
	}
	if d.Code != "E_EXPECTED_IDENT" {
		t.Fatalf("diag code = %q, want E_EXPECTED_IDENT", d.Code)
	}
}
