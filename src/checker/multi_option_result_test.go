package checker

import (
	"strings"
	"testing"
)

// countMultiOption counts the `moptres1` diagnostics in a result list.
func countMultiOption(results []ValidateResult) int {
	n := 0
	for _, r := range results {
		if r.TraceID == multiOptionResultTraceID {
			n++
		}
	}
	return n
}

func countMultiOptionLints(lints []LintResult) int {
	n := 0
	for _, l := range lints {
		if l.TraceID == multiOptionResultTraceID {
			n++
		}
	}
	return n
}

// Two `?T` results are the defect the rule exists for: `?=` can only deposit
// the failure into one of them, so the caller receives a half-filled tuple.
func TestMultiOptionResultsRejectsTwoOptionResults(t *testing.T) {
	src := `pair = () (a ?i64, b ?i64) {
    a = ok(1)
    b = ok(2)
}
`
	res := ValidateMultiOptionResults(parseProg(t, src))
	if got := countMultiOption(res); got != 1 {
		t.Fatalf("expected exactly 1 diagnostic, got %d: %+v", got, res)
	}
	msg := ""
	for _, r := range res {
		if r.TraceID == multiOptionResultTraceID {
			msg = r.Message
		}
	}
	for _, want := range []string{"'pair'", "2 option", "'a'", "'b'"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %q: %s", want, msg)
		}
	}
	// Three option results are the same defect, and the count in the message
	// must be the real one (not a hardcoded 2).
	res3 := ValidateMultiOptionResults(parseProg(t, `tri = () (a ?i64, b ?i64, c ?str) {
    a = ok(1)
    b = ok(2)
    c = ok('x')
}
`))
	if got := countMultiOption(res3); got != 1 {
		t.Fatalf("3 option results: expected 1 diagnostic, got %d", got)
	}
	for _, r := range res3 {
		if r.TraceID == multiOptionResultTraceID && !strings.Contains(r.Message, "3 option") {
			t.Errorf("message should report 3 option results: %s", r.Message)
		}
	}
}

// The legal shapes. This is the non-vacuity control for the rejection test:
// a validator that flagged every result list would pass the test above.
func TestMultiOptionResultsAllowsAtMostOne(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"void", `f = () {
    print(1)
}
`},
		{"single plain", `f = () (a i64) {
    a = 1
}
`},
		{"single option", `f = () (a ?i64) {
    a = ok(1)
}
`},
		{"plain plus one option", `f = () (a i64, b ?i64) {
    a = 1
    b = ok(2)
}
`},
		{"option plus plains", `f = () (a ?i64, b i64, c str) {
    a = ok(1)
    b = 2
    c = 'x'
}
`},
		{"two plains", `f = () (a i64, b i64) {
    a = 1
    b = 2
}
`},
		{"option params are not results", `f = (a ?i64, b ?i64) (r i64) {
    r = 1
}
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countMultiOption(ValidateMultiOptionResults(parseProg(t, c.src))); got != 0 {
				t.Fatalf("expected no diagnostic, got %d", got)
			}
		})
	}
}

// A method's implicit `self` receiver is an out-param the caller aliases to its
// receiver, not a produced value, so it must not consume the option budget —
// and the count in the message must exclude it.
func TestMultiOptionResultsMethodReceiverNotCounted(t *testing.T) {
	bad := `box { v i64 }

box.pair = () (a ?i64, b ?i64) {
    a = ok(.v)
    b = ok(.v)
}
`
	res := ValidateMultiOptionResults(parseProg(t, bad))
	if got := countMultiOption(res); got != 1 {
		t.Fatalf("method with 2 option results: expected 1 diagnostic, got %d: %+v", got, res)
	}
	for _, r := range res {
		if r.TraceID == multiOptionResultTraceID {
			if !strings.Contains(r.Message, "'box.pair'") {
				t.Errorf("message should name the method: %s", r.Message)
			}
			if strings.Contains(r.Message, "3 option") {
				t.Errorf("the implicit self receiver must not be counted: %s", r.Message)
			}
		}
	}

	good := `box { v i64 }

box.get = () (a i64, b ?i64) {
    a = .v
    b = ok(.v)
}
`
	if got := countMultiOption(ValidateMultiOptionResults(parseProg(t, good))); got != 0 {
		t.Fatalf("method with 1 option result: expected no diagnostic, got %d", got)
	}
}

// FFI declarations produce the same ambiguous tuple, so they obey the same rule.
func TestMultiOptionResultsExtern(t *testing.T) {
	src := `#{c}
pkg-take = (p i64) (r1 ?i64, r2 ?i64)
`
	res := ValidateMultiOptionResults(parseProg(t, src))
	if got := countMultiOption(res); got != 1 {
		t.Fatalf("extern with 2 option results: expected 1 diagnostic, got %d: %+v", got, res)
	}
	for _, r := range res {
		if r.TraceID == multiOptionResultTraceID && !strings.Contains(r.Message, "'pkg-take'") {
			t.Errorf("message should name the extern: %s", r.Message)
		}
	}
}

// Monomorphised instances copy the template's token positions, so the
// position-keyed dedupe must collapse them onto ONE diagnostic at the line the
// author wrote. A name-keyed skip ("contains __") would instead exempt a user
// function that happens to carry a double underscore, so that shape is pinned
// separately below.
func TestMultiOptionResultsGenericTemplateReportedOnce(t *testing.T) {
	src := `[t]
twice = (v t) (a ?t, b ?t) {
    a = ok(v)
    b = ok(v)
}
`
	if got := countMultiOption(ValidateMultiOptionResults(parseProg(t, src))); got != 1 {
		t.Fatalf("generic template: expected exactly 1 diagnostic, got %d", got)
	}
}

// A user function whose name contains `__` must still be reported — the rule is
// about the signature, not about who generated the function.
func TestMultiOptionResultsReportsUnderscoreNames(t *testing.T) {
	src := `my__pair = () (a ?i64, b ?i64) {
    a = ok(1)
    b = ok(2)
}
`
	if got := countMultiOption(ValidateMultiOptionResults(parseProg(t, src))); got != 1 {
		t.Fatalf("user function with __ in the name: expected 1 diagnostic, got %d", got)
	}
}

// The hard-error path: ValidateTypes is what `no build` / `no run` /
// validateProgram (JS+WASM) and the LSP's non-skipping lint run consult.
func TestMultiOptionResultsWiredIntoValidateTypes(t *testing.T) {
	src := `pair = () (a ?i64, b ?i64) {
    a = ok(1)
    b = ok(2)
}
`
	if got := countMultiOption(ValidateTypes(parseProg(t, src))); got != 1 {
		t.Fatalf("ValidateTypes: expected 1 diagnostic, got %d", got)
	}
}

// The vet path runs RunAllLints on the MERGED program with SkipTypeChecks, so
// the rule must be re-run there or it disappears from `no vet` for any violation
// that only exists in an imported module (the pre-merge hard-error list never
// sees those).
func TestMultiOptionResultsWiredIntoLintsWhenTypeChecksSkipped(t *testing.T) {
	src := `pair = () (a ?i64, b ?i64) {
    a = ok(1)
    b = ok(2)
}
`
	lints := RunAllLints(parseProg(t, src), LintOptions{SourcePath: "src/app.no", SkipTypeChecks: true})
	if got := countMultiOptionLints(lints); got != 1 {
		t.Fatalf("vet path: expected exactly 1 lint, got %d", got)
	}
	for _, l := range lints {
		if l.TraceID == multiOptionResultTraceID && l.Severity != LintError {
			t.Errorf("severity should be ERROR, got %v", l.Severity)
		}
	}
}

// ...and the LSP path (SkipTypeChecks=false) must NOT report it twice: there it
// arrives through RunAllLints' ValidateTypes block, so the extra block is gated.
func TestMultiOptionResultsNotDuplicatedWhenTypeChecksRun(t *testing.T) {
	src := `pair = () (a ?i64, b ?i64) {
    a = ok(1)
    b = ok(2)
}
`
	lints := RunAllLints(parseProg(t, src), LintOptions{SourcePath: "src/app.no"})
	if got := countMultiOptionLints(lints); got != 1 {
		t.Fatalf("lsp path: expected exactly 1 lint, got %d", got)
	}
}
