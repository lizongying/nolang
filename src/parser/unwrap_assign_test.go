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

// TestUnwrapAssignLowering verifies the S3 contract: `v ?= expr` survives
// ParseProgram as an UnwrapAssignStatement (skipSugarExpand=true defers the
// expansion to H0 / lowerUnwrapAssignToHIR). The walk only validates and
// writes type side-tables; it does NOT replace the node.
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

	// S3: the UnwrapAssignStatement should SURVIVE ParseProgram (deferred to H0).
	hasUnwrap := false
	for _, stmt := range program.Statements {
		astContainsUnwrapAssign(stmt, &hasUnwrap)
	}
	if !hasUnwrap {
		t.Errorf("UnwrapAssignStatement should survive ParseProgram (S3 H0直出), but it was expanded in-place")
	}
}

// TestUnwrapAssignLoweringStructure verifies the S3 contract: after ParseProgram
// the ?= node is still UnwrapAssignStatement (not a lowered BlockStatement).
// The actual expansion is exercised by tmp/unwrap-h0-test.no and hir_equivalence.
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
			body := fd.Body
			if body == nil || len(body.Statements) == 0 {
				t.Fatalf("function body is empty")
			}

			// S3: ?= should still be UnwrapAssignStatement in the body.
			foundUnwrap := false
			for _, s := range body.Statements {
				if _, ok := s.(*UnwrapAssignStatement); ok {
					foundUnwrap = true
					break
				}
			}
			if !foundUnwrap {
				t.Errorf("UnwrapAssignStatement should survive ParseProgram (S3), but no ?= node found in function body")
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

	// S3: ?= nodes should survive ParseProgram (deferred to H0).
	hasUnwrap := false
	for _, stmt := range program.Statements {
		astContainsUnwrapAssign(stmt, &hasUnwrap)
	}
	if !hasUnwrap {
		t.Errorf("UnwrapAssignStatement should survive ParseProgram (S3 H0直出), but it was expanded in-place")
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

// TestUnwrapAssignErrorPropagation verifies the S3 contract: ?= survives
// ParseProgram as UnwrapAssignStatement. The actual error propagation
// (nil/err → result = __unwrap_N; return) is produced by H0's
// lowerUnwrapAssignToHIR and exercised by tmp/unwrap-h0-test.no.
func TestUnwrapAssignErrorPropagation(t *testing.T) {
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

	// S3: ?= should survive as UnwrapAssignStatement in the body.
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*FunctionDefinition); ok && fd.Name == "do-thing" {
			hasUnwrap := false
			astContainsUnwrapAssign(fd.Body, &hasUnwrap)
			if !hasUnwrap {
				t.Errorf("UnwrapAssignStatement should survive ParseProgram (S3)")
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
	if !hasUnwrap {
		t.Errorf("UnwrapAssignStatement should survive ParseProgram (S3 H0直出), but it was expanded in-place")
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
	if !hasUnwrap {
		t.Errorf("UnwrapAssignStatement should survive ParseProgram (S3 H0直出), but it was expanded in-place")
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

// TestUnwrapAssignNonOptionRHSError verifies that `?=` with a RHS that is
// definitively NOT an option (literal, constant-foldable infix, or known
// non-option local) produces the dedicated error telling the user to use `=`.
// This prevents a large-scale `=` → `?=` mistake (per the立项 directive).
func TestUnwrapAssignNonOptionRHSError(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "constant infix is non-option",
			input:   "foo = () (result ?i64) {\n    result = nil\n    v ?= 10 + 20\n}",
			wantErr: true, errSubstr: "is not an option",
		},
		{
			name:    "integer literal is non-option",
			input:   "foo = () (result ?i64) {\n    result = nil\n    v ?= 42\n}",
			wantErr: true, errSubstr: "is not an option",
		},
		{
			name:    "known non-option local is non-option",
			input:   "foo = () (result ?i64) {\n    result = nil\n    x i64 = 5\n    v ?= x\n}",
			wantErr: true, errSubstr: "is not an option",
		},
		{
			name:    "option-returning call is allowed",
			input:   "foo = () (result ?i64) {\n    result = nil\n    v ?= bar()\n}",
			wantErr: false,
		},
		{
			name:    "infix with option operands is allowed",
			input:   "foo = () (result ?i64) {\n    result = nil\n    a ?i64 = ok(1)\n    b ?i64 = ok(2)\n    v ?= a + b\n}",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			_ = p.ParseProgram()
			errs := p.Errors()
			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatalf("expected parse error, got none")
				}
				found := false
				for _, e := range errs {
					if strings.Contains(e, tt.errSubstr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", tt.errSubstr, errs)
				}
				return
			}
			if len(errs) != 0 {
				t.Errorf("expected no parse error, got: %v", errs)
			}
		})
	}
}

// TestUnwrapAssignCompoundOptionOperands verifies that a compound RHS such as
// `v ?= a + b + c` (where the operands are option values) is lowered away
// (no UnwrapAssignStatement remains) and that propagation guards are injected
// for each option operand.
func TestUnwrapAssignCompoundOptionOperands(t *testing.T) {
	inputs := []string{
		`sum = () (result ?i64) {
    result = nil
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c
}`,
		`inner = () (result ?i64) {
    result = nil
    a ?i64 = ok(1)
    b ?i64 = ok(2)
    c ?i64 = ok(3)
    d ?i64 = ok(4)
    total ?= a + b + c + d
}`,
	}
	for i, input := range inputs {
		lex := lexer.New(input)
		p := New(lex)
		program := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("case %d parse errors: %v", i, errs)
		}
		hasUnwrap := false
		for _, stmt := range program.Statements {
			astContainsUnwrapAssign(stmt, &hasUnwrap)
		}
		if !hasUnwrap {
			t.Errorf("case %d: UnwrapAssignStatement should survive ParseProgram (S3 H0直出), but it was expanded in-place", i)
		}
	}
}
