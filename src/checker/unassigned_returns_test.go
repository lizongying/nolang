package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestUnassignedReturnsWarning verifies that the checker reports a warning
// when a named result parameter is never assigned in the function body.
func TestUnassignedReturnsWarning(t *testing.T) {
	src := `foo = () (a i64, b i64) {
    a = 42
    // b is never assigned — will be zero-filled
}

main = () {
    x = 0
    y = 0
    x, y = foo()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "result parameter 'b'") && strings.Contains(r.Message, "never assigned") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning about unassigned result parameter 'b', got %d results: %v", len(results), results)
	}
}

// TestAllReturnsAssigned verifies that no warning is reported when all
// result parameters are assigned.
func TestAllReturnsAssigned(t *testing.T) {
	src := `foo = () (a i64, b i64) {
    a = 42
    b = 99
}

main = () {
    x = 0
    y = 0
    x, y = foo()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when all returns are assigned, got %d: %v", len(results), results)
	}
}

// TestNullableReturnSkipped verifies that nullable result parameters are
// not reported by ValidateUnassignedReturns (they are handled by
// ValidateUninitOutputParams instead).
func TestNullableReturnSkipped(t *testing.T) {
	src := `foo = () (a i64, b ?str) {
    a = 42
    // b is nullable and never assigned — handled by ValidateUninitOutputParams
}

main = () {
    x = 0
    x = foo()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "'b'") {
			t.Fatalf("nullable parameter 'b' should not be reported by ValidateUnassignedReturns: %s", r.Message)
		}
	}
}

// TestMultiAssignReturnsAssigned verifies that result parameters assigned
// via multi-assignment (e.g. a, b = func()) are recognized as assigned.
func TestMultiAssignReturnsAssigned(t *testing.T) {
	src := `bar = () (r i64, s i64) {
    r, s = quux()
}

quux = () (x i64, y i64) {
    x = 1
    y = 2
}

main = () {
    a = 0
    b = 0
    a, b = bar()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when returns are assigned via multi-assign, got %d: %v", len(results), results)
	}
}
