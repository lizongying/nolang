package mir

import (
	"regexp"
	"strings"
	"testing"
)

// TestDivZeroGuardSurvivesOverflowAnnotation pins the rule that the integer
// division-by-zero guard is a property of the OPERATION, not of the result type.
//
// Background: `a / b` produces an `option<int>` unless the statement carries an
// `#{overflow=...}` annotation, which turns the result into a plain int. The
// divisor guard used to live inside the option-wrapping branch, so annotating a
// division with `#{overflow=wrap}` silently DELETED the guard and emitted a raw
// `sdiv` — undefined behaviour (LLVM is then licensed to fold `a / 0`, and
// `sdiv (MIN, -1)` raises #DE on x86-64) with no diagnostic at all. That is the
// worst possible combination, and it is exactly the path
// tests/test-div-zero.no exercises.
//
// The guard now runs on both paths and the two paths differ only in how the
// danger is REPORTED:
//
//	option path (?int result)      -> option tag = err(2); no stderr, no helper
//	plain-int path (annotated)     -> defined result (0) + stderr report
//
// Each assertion below has a negative control, because "the IR contains a
// `select i1`" is also true of the option path:
//
//	(a) the annotated division keeps the zero check, the safe-divisor select and
//	    the MIN/-1 check, and calls the report helper;
//	(b) it forces a defined result (a select of 0) instead of leaving the raw
//	    `sdiv` value;
//	(c) the option path keeps its guard and must NOT grow a report call — a
//	    correct program must not get noise on stderr;
//	(d) float division is untouched (IEEE-754 defines /0.0);
//	(e) a function with no division gets neither the helper nor the call.
func TestDivZeroGuardSurvivesOverflowAnnotation(t *testing.T) {
	// Comptime OFF: every probe is a pure function called with literal
	// arguments, so folding would erase the calls and with them the emitted
	// @wrapped/@modded/@optdiv/@addonly bodies this test inspects.
	t.Setenv("NOLANG_COMPTIME", "0")
	const src = `wrapped = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    r = a / b
}
modded = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    r = a % b
}
optdiv = (a i64, b i64) (r ?i64) {
    r = a / b
}
fdiv = (a f64, b f64) (r f64) {
    r = a / b
}
addonly = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    r = a + b
}
main = () () {
    print(wrapped(10, 0))
    print(modded(10, 0))
    print(optdiv(10, 0))
    print(fdiv(1.0, 0.0))
    print(addonly(1, 2))
}
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	const helper = "__nolang_div_zero_report"

	wrapped := extractDefineBlock(t, ll, "@wrapped")
	if wrapped == "" {
		t.Fatal("@wrapped not found in the emitted IR")
	}
	// (a) the guard is present on the plain-int path.
	if !regexp.MustCompile(`icmp eq i64 %\S+, 0`).MatchString(wrapped) {
		t.Errorf("@wrapped (annotated `r = a / b`) has no divisor-is-zero check — the annotation deleted the guard again:\n%s", wrapped)
	}
	if !strings.Contains(wrapped, "select i1") {
		t.Errorf("@wrapped has no safe-divisor select; a raw sdiv may be emitted with a 0 divisor:\n%s", wrapped)
	}
	if !regexp.MustCompile(`icmp eq i64 %\S+, -9223372036854775808`).MatchString(wrapped) {
		t.Errorf("@wrapped must keep the (MIN, -1) guard — `sdiv (MIN, -1)` is UB / #DE, and it must not be exempted by the overflow annotation:\n%s", wrapped)
	}
	if !strings.Contains(wrapped, "call void @"+helper) {
		t.Errorf("@wrapped must report the zero divisor on stderr (the plain-int path has no option to inspect):\n%s", wrapped)
	}
	// (b) a defined result, not the raw sdiv value.
	if !regexp.MustCompile(`%\S+ = select i1 %\S+, i64 0, i64 %\S+`).MatchString(wrapped) {
		t.Errorf("@wrapped must force a DEFINED result for a zero divisor (select 0) instead of storing the raw sdiv value:\n%s", wrapped)
	}

	// The same for `%`.
	modded := extractDefineBlock(t, ll, "@modded")
	if !strings.Contains(modded, "call void @"+helper) || !strings.Contains(modded, "srem") {
		t.Errorf("@modded (`r = a %% b` annotated) must keep the guard and the report:\n%s", modded)
	}

	// (c) the option path: guard yes, stderr report NO.
	optdiv := extractDefineBlock(t, ll, "@optdiv")
	if !regexp.MustCompile(`icmp eq i64 %\S+, 0`).MatchString(optdiv) {
		t.Errorf("@optdiv (unannotated `r = a / b`) lost its divisor guard:\n%s", optdiv)
	}
	if strings.Contains(optdiv, helper) {
		t.Errorf("@optdiv must NOT call the stderr report — on the option path the danger travels through the err tag, so a correct program must stay silent:\n%s", optdiv)
	}
	// The structured-error restructure (compiler-synthesized err.err) replaced
	// the single `select i1 %dz, i64 2, i64 0` with a guard branch into an err
	// block that heap-boxes an err.err payload — a select cannot build that. The
	// pinned semantic is unchanged: the zero divisor must still materialize the
	// err tag (2), so assert on the STORED tag rather than the select shape.
	if !regexp.MustCompile(`store %option \{ i64 2,`).MatchString(optdiv) {
		t.Errorf("@optdiv must still turn a zero divisor into the err tag (2):\n%s", optdiv)
	}

	// (d) float division is IEEE-754 defined (inf/nan) and must not be guarded.
	fdiv := extractDefineBlock(t, ll, "@fdiv")
	if strings.Contains(fdiv, helper) || strings.Contains(fdiv, "icmp eq double") {
		t.Errorf("@fdiv (f64 / f64) must stay a bare fdiv — 1.0/0.0 is inf, not UB:\n%s", fdiv)
	}

	// (e) no division anywhere in @addonly -> no call. Together with (a) this is
	// what makes the assertion non-vacuous: the helper is emitted lazily, so a
	// module-wide `strings.Contains(ll, helper)` would prove nothing.
	addonly := extractDefineBlock(t, ll, "@addonly")
	if strings.Contains(addonly, helper) {
		t.Errorf("@addonly has no division and must not call the report helper:\n%s", addonly)
	}

	// The helper body itself: a conditional write to fd 2, exactly once.
	helperBody := extractDefineBlock(t, ll, "@"+helper)
	if helperBody == "" {
		t.Fatalf("@%s must be defined in a module that divides on the plain-int path:\n%s", helper, ll)
	}
	if !strings.Contains(helperBody, "call i64 @write(i32 2,") {
		t.Errorf("@%s must write its diagnostic to stderr (fd 2):\n%s", helper, helperBody)
	}
	if !strings.Contains(ll, "@nolang_msg_div_zero") {
		t.Errorf("the diagnostic string constant @nolang_msg_div_zero is missing from the module")
	}
	if n := strings.Count(ll, "define void @"+helper); n != 1 {
		t.Errorf("the helper must be emitted exactly once, got %d definitions", n)
	}
}

// TestDivZeroGuardIsAbsentWithoutDivision is the module-level non-vacuity guard
// for the test above: a program with no integer division at all must contain
// neither the helper nor the message constant, which is what makes "the helper
// is emitted lazily" a checkable property rather than a comment.
func TestDivZeroGuardIsAbsentWithoutDivision(t *testing.T) {
	const src = `plain = (a i64, b i64) (r i64) {
    #{overflow=wrap}
    r = a + b
}
main = () () {
    print(plain(1, 2))
}
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if strings.Contains(ll, "__nolang_div_zero_report") || strings.Contains(ll, "@nolang_msg_div_zero") {
		t.Errorf("a program with no integer division must not carry the div-by-zero helper or its message")
	}
}
