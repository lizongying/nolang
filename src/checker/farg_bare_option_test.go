package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// countFxxoptargErrors runs ValidateFuncArgs and returns the number of
// fxxoptarg (bare option argument) errors.
func countFxxoptargErrors(t *testing.T, src string) int {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	n := 0
	for _, r := range ValidateFuncArgs(prog, "") {
		if r.TraceID == "fxxoptarg" {
			n++
		}
	}
	return n
}

// TestBareOptionArgConstPasses verifies the Phase-2 refinement:
// if the compiler can prove at compile time that an argument is non-option,
// it passes. A constant arithmetic expression (e.g. `10+20`) already resolves
// to the plain `i64` type (arithmetic overflow is modeled as option only where
// the *result* is option-bearing; a constant is provably non-option), so
// `f(10+20)` must NOT be flagged.
func TestBareOptionArgConstPasses(t *testing.T) {
	src := `f = (x i64) (r i64) {
	r = x * 2
}
g = () (res i64) {
	res = f(10+20)
}
`
	if n := countFxxoptargErrors(t, src); n != 0 {
		t.Fatalf("const arg wrongly rejected: got %d fxxoptarg errors", n)
	}
}

// TestBareOptionArgBareVariableRejected verifies that a non-const option-typed
// value (?i64) passed where a plain i64 is expected is rejected, forcing
// explicit unwrap (match / force-unwrap). This is the case the user reported:
// `r i64 = f(v)` with `v ?i64` must produce the fxxoptarg error.
func TestBareOptionArgBareVariableRejected(t *testing.T) {
	src := `f = (x i64) (r i64) {
	r = x * 2
}
get = () (r ?i64) {
	r = 42
}
g = () (res i64) {
	w = get()
	res = f(w)
}
`
	if n := countFxxoptargErrors(t, src); n != 1 {
		t.Fatalf("expected exactly 1 fxxoptarg error, got %d", n)
	}
}

// TestBareOptionArgLegitOptionParamOK verifies that passing a ?i64 to a function
// whose parameter is explicitly ?i64 is still allowed (no false positive).
func TestBareOptionArgLegitOptionParamOK(t *testing.T) {
	src := `f = (x ?i64) (r i64) {
	r = 1
}
get = () (r ?i64) {
	r = 42
}
g = () (res i64) {
	w = get()
	res = f(w)
}
`
	if n := countFxxoptargErrors(t, src); n != 0 {
		t.Fatalf("legit ?T->?T param wrongly rejected: got %d fxxoptarg errors", n)
	}
}
