package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestEprintOptionArgIsSourceLocated pins the checker half of the codegen
// diagnostic gap: eprint cannot lower `?T`, so vet must reject the argument
// at its own source position instead of staying green and leaving the failure
// to MIR codegen.
func TestEprintOptionArgIsSourceLocated(t *testing.T) {
	src := `main = () () {
  x ?str = nil
  eprint(x)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var got []ValidateResult
	for _, r := range ValidatePrintFormat(prog) {
		if r.TraceID == eprintOptionArgTraceID {
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		t.Fatalf("expected one eprint option diagnostic, got %d: %v", len(got), got)
	}
	if got[0].Line != 3 || got[0].Column <= 0 {
		t.Errorf("diagnostic location = %d:%d, want line 3 and column > 0", got[0].Line, got[0].Column)
	}
	if !strings.Contains(got[0].Message, "?str") {
		t.Errorf("diagnostic does not name the option type: %q", got[0].Message)
	}

	// A direct std call has no local variable to infer from; its option
	// result type comes from the baked std signature table.
	src = "main = () () {\n  eprint(os.arg(1))\n}\n"
	l = lexer.New(src)
	p = parser.New(l)
	prog = p.ParseProgram()
	got = nil
	for _, r := range ValidatePrintFormat(prog) {
		if r.TraceID == eprintOptionArgTraceID {
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		t.Fatalf("expected eprint(os.arg(1)) to be flagged, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "?str") {
		t.Errorf("direct-call diagnostic does not name the option type: %q", got[0].Message)
	}
}

// TestEprintOptionArgAllowsScalars guards against over-suppression: ordinary
// eprint arguments must stay untouched.
func TestEprintOptionArgAllowsScalars(t *testing.T) {
	src := `main = () () {
  x str = 'ok'
  eprint(x)
  eprint(1)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	for _, r := range ValidatePrintFormat(prog) {
		if r.TraceID == eprintOptionArgTraceID {
			t.Fatalf("unexpected eprint option diagnostic: %+v", r)
		}
	}
}
