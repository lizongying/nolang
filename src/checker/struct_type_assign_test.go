package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateUndefinedVarsStructTypeFieldAssign verifies that assigning a field
// directly on a struct type name (e.g. `s.x = 7` where `s` is defined as
// `s { x i64 }`) is reported as an error. The user must instantiate the struct
// first (e.g. `s0 = s {}` then `s0.x = 7`).
func TestValidateUndefinedVarsStructTypeFieldAssign(t *testing.T) {
	src := `s {
    x i64
}

s.x = 7
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUndefinedVars(prog, "")
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "cannot assign field 'x' on struct type 's'") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about assigning field on struct type, got: %+v", results)
	}
}

// TestValidateUndefinedVarsStructInstanceFieldAssign verifies that assigning a
// field on a struct INSTANCE (e.g. `s0.x = 7` where `s0 = s {}`) does NOT
// produce an error.
func TestValidateUndefinedVarsStructInstanceFieldAssign(t *testing.T) {
	src := `s {
    x i64
}

s0 = s {}
s0.x = 7
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUndefinedVars(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "cannot assign field") {
			t.Fatalf("unexpected error: %s", r.Message)
		}
	}
}

// TestValidateUndefinedVarsBuiltinTypeFieldAssign verifies that assigning fields
// on built-in type names (e.g. `i8.MIN = -128`) does NOT produce an error,
// because built-in types are not in validationStructNames.
func TestValidateUndefinedVarsBuiltinTypeFieldAssign(t *testing.T) {
	src := `i8.MIN = -128
i8.MAX = 127
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUndefinedVars(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "cannot assign field") {
			t.Fatalf("unexpected error for built-in type: %s", r.Message)
		}
	}
}
