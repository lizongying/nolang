package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsInfixExprOnArrayElement reproduces the issue where
// calling a function with `v[d] ^ v[a]` (where v is []u64) is wrongly
// reported as "expected 'u64', got 'i64'".
//
// Root cause: resolveExprType handles IndexExpression (can extract element
// type from []u64 → u64), but has no case for InfixExpression. So bitwise
// XOR (^) falls through to inferExprType's default which returns "i64".
func TestValidateFuncArgsInfixExprOnArrayElement(t *testing.T) {
	src := `rotr = (x u64, n i64) (r u64) {
    r = x
}

blake-g = (v []u64, a i64, b i64, c i64, d i64) {
    v[d] = rotr(v[d] ^ v[a], 32)
    v[b] = rotr(v[b] ^ v[c], 24)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected 'u64', got 'i64'") {
			t.Errorf("false positive: %s", r.Message)
		}
	}
}
