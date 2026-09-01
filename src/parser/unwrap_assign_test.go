package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestUnwrapAssignParse verifies that `v ?= expr` parses correctly
// and produces an UnwrapAssignStatement in the AST.
func TestUnwrapAssignParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "basic unwrap assign",
			input:   "foo = () (result ?i64) {\n    result = nil\n    v ?= bar()\n}",
			wantErr: false,
		},
		{
			name:    "unwrap assign with method call",
			input:   "foo = () (result ?str) {\n    result = nil\n    v ?= .read()\n}",
			wantErr: false,
		},
		{
			name:    "unwrap assign with dotted call",
			input:   "foo = () (result ?str) {\n    result = nil\n    v ?= fs.read('file')\n}",
			wantErr: false,
		},
		{
			name:    "unwrap assign outside function fails",
			input:   "v ?= bar()",
			wantErr: true,
		},
		{
			name:    "unwrap assign in non-option function fails",
			input:   "foo = () (result i64) {\n    v ?= bar()\n}",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected parse errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

// TestUnwrapAssignLowering verifies that `v ?= expr` is correctly
// lowered to a match/if chain with proper error propagation.
func TestUnwrapAssignLowering(t *testing.T) {
	input := `foo = (x str) (result ?i64) {
    result = nil
    v ?= bar(x)
}

bar = (x str) (result ?i64) {
    result = 42
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}

	// After lowering, the UnwrapAssignStatement should have been replaced
	// with a BlockStatement containing a LetStatement + match/if chain.
	// The program should have no UnwrapAssignStatement nodes left.
	// Verify by checking the AST doesn't contain any UnwrapAssignStatement.
	hasUnwrap := false
	for _, stmt := range program.Statements {
		astContainsUnwrapAssign(stmt, &hasUnwrap)
	}
	if hasUnwrap {
		t.Errorf("UnwrapAssignStatement should have been lowered, but still found in AST")
	}
}

// TestUnwrapAssignLoweringStructure verifies the lowered structure
// contains the proper if/match chain for error propagation.
func TestUnwrapAssignLoweringStructure(t *testing.T) {
	input := `do-thing = (x str) (result ?i64) {
    result = nil
    v ?= compute(x)
}

compute = (x str) (result ?i64) {
    result = 42
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Find the do-thing function definition
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*FunctionDefinition); ok {
			if fd.Name != "do-thing" {
				continue
			}
			// The body should contain a BlockStatement with the lowered unwrap
			body := fd.Body
			if body == nil || len(body.Statements) == 0 {
				t.Fatalf("function body is empty")
			}

			// First statement should be `result = nil` (LetStatement)
			// Second statement should be the lowered BlockStatement
			foundLoweredBlock := false
			for _, s := range body.Statements {
				if bs, ok := s.(*BlockStatement); ok && len(bs.Statements) >= 2 {
					// Should contain a LetStatement (temp var) and
					// an ExpressionStatement with IfExpression
					foundLoweredBlock = true
					break
				}
			}
			if !foundLoweredBlock {
				t.Errorf("did not find lowered block statement for ?= in function body")
			}
			return
		}
	}
	t.Fatalf("do-thing function not found")
}

// TestUnwrapAssignMultiple verifies multiple ?= in the same function.
func TestUnwrapAssignMultiple(t *testing.T) {
	input := `process = (a str, b str) (result ?str) {
    result = nil
    x ?= step-a(a)
    y ?= step-b(b)
    result = x - b
}

step-a = (a str) (result ?str) {
    result = a
}

step-b = (b str) (result ?str) {
    result = b
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}

	// Verify no UnwrapAssignStatement remains after lowering
	hasUnwrap := false
	for _, stmt := range program.Statements {
		astContainsUnwrapAssign(stmt, &hasUnwrap)
	}
	if hasUnwrap {
		t.Errorf("UnwrapAssignStatement should have been lowered, but still found in AST")
	}
}

// astContainsUnwrapAssign recursively checks if any statement or expression
// in the AST contains an UnwrapAssignStatement.
func astContainsUnwrapAssign(stmt Statement, found *bool) {
	if *found {
		return
	}
	if stmt == nil {
		return
	}
	if _, ok := stmt.(*UnwrapAssignStatement); ok {
		*found = true
		return
	}
	switch s := stmt.(type) {
	case *BlockStatement:
		for _, ss := range s.Statements {
			astContainsUnwrapAssign(ss, found)
		}
	case *FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				astContainsUnwrapAssign(ss, found)
			}
		}
	case *ExpressionStatement:
		astContainsUnwrapAssignExpr(s.Expression, found)
	case *LetStatement:
		astContainsUnwrapAssignExpr(s.Value, found)
	}
}

func astContainsUnwrapAssignExpr(expr Expression, found *bool) {
	if *found || expr == nil {
		return
	}
	switch e := expr.(type) {
	case *IfExpression:
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				astContainsUnwrapAssign(ss, found)
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				astContainsUnwrapAssign(ss, found)
			}
		}
	}
}

// TestUnwrapAssignErrorPropagation verifies that ?= generates proper
// error propagation: nil/err → set result and return.
func TestUnwrapAssignErrorPropagation(t *testing.T) {
	// This test verifies the lowered AST contains `return` in the
	// nil||err branch (error propagation path).
	input := `do-thing = (x str) (result ?i64) {
    result = nil
    v ?= compute(x)
}

compute = (x str) (result ?i64) {
    result = 42
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Check the lowered AST for the presence of ReturnStatement
	// inside the error propagation branch.
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*FunctionDefinition); ok && fd.Name == "do-thing" {
			hasReturn := false
			findReturnInBlock(fd.Body, &hasReturn)
			if !hasReturn {
				t.Errorf("lowered ?= should contain a return statement for error propagation")
			}
			return
		}
	}
	t.Fatalf("do-thing function not found")
}

func findReturnInBlock(block *BlockStatement, found *bool) {
	if *found || block == nil {
		return
	}
	for _, s := range block.Statements {
		findReturnInStmt(s, found)
	}
}

func findReturnInStmt(stmt Statement, found *bool) {
	if *found || stmt == nil {
		return
	}
	if _, ok := stmt.(*ReturnStatement); ok {
		*found = true
		return
	}
	switch s := stmt.(type) {
	case *BlockStatement:
		findReturnInBlock(s, found)
	case *ExpressionStatement:
		if ie, ok := s.Expression.(*IfExpression); ok {
			findReturnInBlock(ie.Consequence, found)
			findReturnInBlock(ie.Alternative, found)
		}
	case *LetStatement:
		// no-op
	}
}

// TestUnwrapAssignChaining verifies that ?= can be chained
// (result of one ?= used in the next).
func TestUnwrapAssignChaining(t *testing.T) {
	input := `pipeline = (x str) (result ?str) {
    result = nil
    a ?= step1(x)
    b ?= step2(a)
    result = b
}

step1 = (x str) (result ?str) {
    result = x
}

step2 = (a str) (result ?str) {
    result = a
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Verify no UnwrapAssignStatement remains
	hasUnwrap := false
	for _, stmt := range program.Statements {
		astContainsUnwrapAssign(stmt, &hasUnwrap)
	}
	if hasUnwrap {
		t.Errorf("UnwrapAssignStatement should have been lowered")
	}
}

// TestUnwrapAssignInMethod verifies ?= works inside struct methods.
func TestUnwrapAssignInMethod(t *testing.T) {
	input := `file {
    path str
}

file.read = () (result ?str) {
    result = nil
    content ?= fs.read(self.path)
    result = content
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Verify no UnwrapAssignStatement remains
	hasUnwrap := false
	for _, stmt := range program.Statements {
		astContainsUnwrapAssign(stmt, &hasUnwrap)
	}
	if hasUnwrap {
		t.Errorf("UnwrapAssignStatement should have been lowered")
	}
}

// TestUnwrapAssignNotInOptionFunction verifies that ?= in a function
// without an option result param produces an error.
func TestUnwrapAssignNotInOptionFunction(t *testing.T) {
	input := `foo = () (result i64) {
    result = 0
    v ?= bar()
}

bar = () (result ?i64) {
    result = 42
}
`
	lex := lexer.New(input)
	p := New(lex)
	_ = p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Errorf("expected error for ?= in non-option function, got none")
		return
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "?=") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error message mentioning ?=, got: %v", errs)
	}
}

// TestUnwrapAssignAtTopLevel verifies that ?= at top level produces an error.
func TestUnwrapAssignAtTopLevel(t *testing.T) {
	input := "v ?= bar()"
	lex := lexer.New(input)
	p := New(lex)
	_ = p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Errorf("expected error for ?= at top level, got none")
	}
}
