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
