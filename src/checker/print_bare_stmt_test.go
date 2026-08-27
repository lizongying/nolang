package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestBarePrintStatementRejected verifies that the invalid syntax
// `print 'hi'` (a function used as a bare value without being called)
// is rejected at validation time with a clear diagnostic, instead of
// leaking broken IR to the backend (which previously surfaced as an
// opaque `use of undefined value '%print'` error from opt).
//
// `print 'hi'` parses into two ExpressionStatements: the bare identifier
// `print` then the bare string literal `'hi'`. The bare identifier is
// rejected; the trailing literal is the block value of the top-level
// block and is therefore permitted.
func TestBarePrintStatementRejected(t *testing.T) {
	src := "print 'hi'\n"
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	errs := ValidateTypes(prog)
	var found bool
	for _, r := range errs {
		if strings.Contains(r.Message, "'print' is a function and must be called") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about 'print' being a function, got: %v", errs)
	}
}

// TestValidPrintCallNotRejected ensures the VALID syntax print('hi') is NOT
// falsely flagged by the bare-expression-statement check.
func TestValidPrintCallNotRejected(t *testing.T) {
	src := "print('hi')\n"
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	errs := ValidateTypes(prog)
	for _, r := range errs {
		if strings.Contains(r.Message, "is a function and must be called") ||
			strings.Contains(r.Message, "cannot be just a literal value") {
			t.Fatalf("valid print('hi') falsely rejected: %s", r.Message)
		}
	}
}

// TestBareLiteralAsBlockValueAllowed ensures a bare literal that is the
// final expression of a block (i.e. the block value, as in
// `r = if 1 { 'hello' }`) is NOT rejected, while a bare literal used as a
// plain statement IS rejected.
func TestBareLiteralAsBlockValueAllowed(t *testing.T) {
	// block value: allowed
	srcOK := "r str = if 1 { 'hello' } else { 'world' }\n"
	l := lexer.New(srcOK)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	for _, r := range ValidateTypes(prog) {
		if strings.Contains(r.Message, "cannot be just a literal value") {
			t.Fatalf("block-value literal falsely rejected: %s", r.Message)
		}
	}

	// bare literal as a plain statement (not the block value): rejected
	srcBad := "x i64 = 1\n'stray'\ny i64 = 2\n"
	l2 := lexer.New(srcBad)
	p2 := parser.New(l2)
	prog2 := p2.ParseProgram()
	if errs := p2.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	var found bool
	for _, r := range ValidateTypes(prog2) {
		if strings.Contains(r.Message, "cannot be just a literal value") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected bare literal statement to be rejected, got: %v", ValidateTypes(prog2))
	}
}
