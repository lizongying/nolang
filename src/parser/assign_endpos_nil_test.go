package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestAssignExpressionEndPosNilValue guards against a nil-pointer panic when an
// AssignExpression is left with a nil Value by error recovery.
//
// Malformed input such as `n = .len` (a dot-self expression missing its right
// operand) still produces an *AssignExpression node, but with Value == nil.
// ParseProgram's attachInlineComment -> stmtTokenEndLine path calls EndPos() on
// every statement, so a non-nil-safe AssignExpression.EndPos() crashed the whole
// parse (surfacing as an LSP formatting panic). EndPos() must fall back to the
// Token position instead of dereferencing the nil Value.
func TestAssignExpressionEndPosNilValue(t *testing.T) {
	// Direct: an AssignExpression with nil Value must not panic.
	ae := &AssignExpression{Token: lexer.Token{Line: 3, Column: 5}}
	if pos := ae.EndPos(); pos.Line != 3 {
		t.Errorf("EndPos() on nil-Value AssignExpression: got line %d, want 3", pos.Line)
	}

	// End-to-end: parsing malformed input that yields a nil-Value assignment
	// must complete without panicking (the parse error itself is acceptable).
	src := "str.len = () (n i64 {\n    n = .len\n}"
	l := lexer.New(src)
	p := New(l)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ParseProgram panicked on malformed input: %v", r)
			}
		}()
		_ = p.ParseProgram()
	}()
}
