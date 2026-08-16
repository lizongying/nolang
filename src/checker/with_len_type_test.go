package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateTypesWithLenNoTypeAnnotation verifies that using with-len without
// an explicit type annotation on the LHS is reported as an error, because the
// compiler cannot infer the element type and defaults to []i64 (8 bytes/element).
//
// Example:
//   buf = with-len(n)       // ERROR: cannot infer type
//   buf []byte = with-len(n) // OK: type inferred from LHS annotation
func TestValidateTypesWithLenNoTypeAnnotation(t *testing.T) {
	src := `save = (path str) (ok bool) {
    n = 14
    buf = with-len(n)
    buf[0] = 66
    ok = true
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateTypes(prog)
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "cannot infer type for 'buf'") &&
			strings.Contains(r.Message, "with-len") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about with-len type inference, got: %+v", results)
	}
}

// TestValidateTypesWithLenWithTypeAnnotation verifies that with-len WITH an
// explicit type annotation does NOT produce an error.
func TestValidateTypesWithLenWithTypeAnnotation(t *testing.T) {
	src := `save = (path str) (ok bool) {
    n = 14
    buf []byte = with-len(n)
    buf[0] = 66
    ok = true
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateTypes(prog)
	for _, r := range results {
		if strings.Contains(r.Message, "with-len") {
			t.Errorf("unexpected with-len error with type annotation: %s", r.Message)
		}
	}
}

// TestValidateTypesWithCapNoTypeAnnotation verifies that with-cap without
// an explicit type annotation is also reported as an error.
func TestValidateTypesWithCapNoTypeAnnotation(t *testing.T) {
	src := `save = (path str) (ok bool) {
    n = 14
    buf = with-cap(n)
    ok = true
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateTypes(prog)
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "cannot infer type for 'buf'") &&
			strings.Contains(r.Message, "with-cap") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about with-cap type inference, got: %+v", results)
	}
}

// TestValidateFuncArgsWithLenNoTypeAnnotation verifies the same check in
// the ValidateFuncArgs path (used by LSP and no vet).
func TestValidateFuncArgsWithLenNoTypeAnnotation(t *testing.T) {
	src := `save = (path str) (ok bool) {
    n = 14
    buf = with-len(n)
    buf[0] = 66
    ok = true
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "cannot infer type for 'buf'") &&
			strings.Contains(r.Message, "with-len") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about with-len type inference, got: %+v", results)
	}
}
