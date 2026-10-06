package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// A `f (a ..T)` spread parameter accepts ANY number of trailing arguments —
// that is the whole point of the form (`number.max(3, 1, 4, 1, 5)`). The
// argument-count guard in checkCallArgsInExpr counted the spread parameter as
// ONE input, so every call with two or more elements was rejected:
//
//	function 'mx' expects at most 1 input argument(s), got 2 [oarg2]
//
// Std-module calls escaped this only because the guard runs on the *merged*
// program's Identifier callees and `number.max(...)` reaches codegen as a
// DotExpression — which is how the mis-lowered spread (see
// mir/variadic_argform_test.go) stayed invisible: the language-level variadic
// functions in std could be called, the ones defined in the user's own file
// could not.

func runFuncArgs(t *testing.T, src string) []ValidateResult {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return ValidateFuncArgs(prog, "")
}

// TestVariadicCallAcceptsManyArgs: a local spread function called with three
// arguments must not be reported as taking too many.
func TestVariadicCallAcceptsManyArgs(t *testing.T) {
	src := `mx = (a ..i64) (r i64) {
    #{index-out=zero}
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> {
            #{index-out=zero}
            r = a[i]
        }
    }
}

v = mx(3, 1, 7)
print(v)
`
	for _, r := range runFuncArgs(t, src) {
		if strings.Contains(r.Message, "expects at most") {
			t.Fatalf("variadic call rejected as taking too many arguments: L%d:C%d %s",
				r.Line, r.Column, r.Message)
		}
	}
}

// TestVariadicCallChecksElementTypes: relaxing the count must not relax the
// element type — the spread parameter's ELEMENT type (not its `[]T` form) is
// what every trailing argument has to satisfy.
func TestVariadicCallChecksElementTypes(t *testing.T) {
	src := `mx = (a ..i64) (r i64) {
    #{index-out=zero}
    r = a[0]
}

s str = 'abc'
v = mx(3, s)
print(v)
`
	var found bool
	for _, r := range runFuncArgs(t, src) {
		if strings.Contains(r.Message, "expected 'i64'") || strings.Contains(r.Message, "argument 2") {
			found = true
		}
		// `[]i64` as the expected type of a spread element is the bug this
		// asserts against: the element of `..i64` is `i64`, never a slice.
		if strings.Contains(r.Message, "expected '[]i64'") {
			t.Fatalf("spread element compared against the slice type: L%d %s", r.Line, r.Message)
		}
	}
	if !found {
		t.Fatal("expected a str-into-..i64 argument type error, got none")
	}
}
