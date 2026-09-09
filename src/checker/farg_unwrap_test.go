package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// Phase-2 rule (tracked in working memory): a bare arithmetic argument passed
// to an *option-returning* function (one whose result type is `?T`) must be
// explicitly unwrapped with `?=`. This forces the caller to acknowledge the
// option (overflow can be raised for bare arithmetic in nolang). The call is
// exempt when it is the RHS of a `?=` unwrap (including `_ ?=`).
//
// Void-returning functions (e.g. io.outln) and non-arithmetic arguments are
// NOT affected — `io.outln(10 + 20)` must keep working.

const fargUnwrapProbeHeader = `f = (x i64) (r ?i64) {
	r = x
}
g = () (res ?i64) {
`

const fargUnwrapProbeFooter = `}
`

func countFargErrors(t *testing.T, label, body string) int {
	t.Helper()
	src := fargUnwrapProbeHeader + body + fargUnwrapProbeFooter
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("[%s] parse errors: %v", label, errs)
	}
	results := ValidateFuncArgs(prog, "")
	n := 0
	for _, r := range results {
		if r.TraceID == "f10x20nf" {
			n++
		}
	}
	return n
}

func TestFargUnwrapBareArithmeticRejected(t *testing.T) {
	// f(10+20): bare arithmetic arg to an option-returning fn → MUST be rejected.
	if n := countFargErrors(t, "bare-call", "f(10+20)\n"); n != 1 {
		t.Fatalf("bare-call: expected 1 f10x20nf error, got %d", n)
	}
	// f(a+b) with variable arithmetic arg → MUST be rejected.
	if n := countFargErrors(t, "var-arith-arg", "a i64 = 1\nb i64 = 2\nf(a + b)\n"); n != 1 {
		t.Fatalf("var-arith-arg: expected 1 f10x20nf error, got %d", n)
	}
}

func TestFargUnwrapExemptWhenUnwrapped(t *testing.T) {
	// a ?= f(10+20): call is the RHS of `?=` → allowed.
	if n := countFargErrors(t, "result-unwrap-a", "a ?= f(10+20)\n"); n != 0 {
		t.Fatalf("result-unwrap-a: expected 0 errors, got %d", n)
	}
	// _ ?= f(10+20): discard unwrap must also be exempt (lexer tokenizes `_` as
	// UNDERSCORE, so it must still route through parseUnwrapAssignStatement).
	if n := countFargErrors(t, "result-unwrap-underscore", "_ ?= f(10+20)\n"); n != 0 {
		t.Fatalf("result-unwrap-underscore: expected 0 errors, got %d", n)
	}
	// f(b) with a concrete i64 argument (previously obtained via `b ?= 10+20`,
	// which is now a compile error per the立项 directive — see
	// TestUnwrapAssignNonOptionRHSError) → allowed (no f10x20nf).
	if n := countFargErrors(t, "arg-unwrap", "b i64 = 30\nf(b)\n"); n != 0 {
		t.Fatalf("arg-unwrap: expected 0 errors, got %d", n)
	}
}

// TestFargUnwrapExemptBareOptionArgUnderQuestionAssign verifies that a bare
// ?T argument inside a `?=` RHS is exempt from the fxxoptarg rule — the `?=`
// itself is the required explicit unwrap (mirrors the f10x20nf exemption).
func TestFargUnwrapExemptBareOptionArgUnderQuestionAssign(t *testing.T) {
	src := `f = (x i64) (r ?i64) {
	r = x
}
g = () (res ?i64) {
	v ?i64 = 5
	a ?= f(v)
	res = a
}
`
	if n := countFxxoptargErrors(t, src); n != 0 {
		t.Fatalf("bare ?T arg under ?= must be exempt from fxxoptarg, got %d", n)
	}
}

func TestFargUnwrapPlainAndVoidOK(t *testing.T) {
	// Plain (non-arithmetic) i64 argument → allowed.
	if n := countFargErrors(t, "plain-arg", "w i64 = 30\nf(w)\n"); n != 0 {
		t.Fatalf("plain-arg: expected 0 errors, got %d", n)
	}
	// Void-returning function with a bare arithmetic arg → NOT affected.
	voidSrc := `prt = (x i64) {
}
p = () (res ?i64) {
	prt(10 + 20)
}
`
	l := lexer.New(voidSrc)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("void parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		if r.TraceID == "f10x20nf" {
			t.Fatalf("void-returning call must not be flagged, got: %v", r)
		}
	}
}
