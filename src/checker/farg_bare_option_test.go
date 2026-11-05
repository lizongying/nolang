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
		if r.TraceID == "3604icnf" {
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

// countReturnCountErrors runs ValidateFuncArgs and returns the number of
// rfzrw1nh (return-count mismatch) errors.
func countReturnCountErrors(t *testing.T, src string) int {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	n := 0
	for _, r := range ValidateFuncArgs(prog, "") {
		if r.TraceID == "rfzrw1nh" {
			n++
		}
	}
	return n
}

// TestMultiAssignStdMethodReturnCountRejected is a regression guard for the
// invalid IR `icmp ne void undef, 0`. Std METHOD signatures live in
// CollectStdMethodSigs, a table separate from CollectStdModuleSignatures;
// lookupReturnCount must consult it. A legacy `(T, ok bool)` destructure
// against a `?T`-returning std method (`s, found = it.str()`) has to be
// rejected at source level with rfzrw1nh, instead of silently dropping the
// extra target and later reading it as an unbound void value.
func TestMultiAssignStdMethodReturnCountRejected(t *testing.T) {
	for _, src := range []string{
		"main = () {\n\ts, found = it.str()\n}\n",
		"main = () {\n\ts, found = json.str(v)\n}\n",
	} {
		if n := countReturnCountErrors(t, src); n != 1 {
			t.Fatalf("expected 1 rfzrw1nh error for %q, got %d", src, n)
		}
	}
}

// TestMultiAssignUserFunctionReturnCountOK verifies no false positive: a
// user-defined function with two result parameters may still be destructured
// into two targets.
func TestMultiAssignUserFunctionReturnCountOK(t *testing.T) {
	src := "two = () (p i64, q i64) {\n\tp = 1\n\tq = 2\n}\nmain = () {\n\ta, b = two()\n}\n"
	if n := countReturnCountErrors(t, src); n != 0 {
		t.Fatalf("legit 2-target destructure wrongly rejected: got %d rfzrw1nh errors", n)
	}
}
