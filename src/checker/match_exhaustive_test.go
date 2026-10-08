package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func parseProg(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func countMatchNonex(results []ValidateResult) int {
	n := 0
	for _, r := range results {
		if r.TraceID == "match-nonex" {
			n++
		}
	}
	return n
}

// The reported case: `size: { ok -> {...} }` on an option, with no nil/err arm
// and no wildcard `->`. Must be reported.
func TestNonExhaustiveMatchOkOnly(t *testing.T) {
	src := `f = () () {
    size = stat-size("x")
    size: {
        ok -> {
            n = 1
        }
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if countMatchNonex(results) != 1 {
		t.Fatalf("expected 1 match-nonex, got %d: %v", len(results), results)
	}
	if !strings.Contains(results[0].Message, "size") {
		t.Errorf("expected message to name the matched option, got: %s", results[0].Message)
	}
}

// Exhaustive: nil/err handled plus a wildcard `->` catch-all. Must NOT report.
func TestExhaustiveMatchOkNilElseNotReported(t *testing.T) {
	src := `f = () () {
    b: {
        err -> n = 1

        nil -> n = 2

        -> n = 3
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for exhaustive match, got %d: %v", n, results)
	}
}

// ok + nil without err is NOT exhaustive: err is a real variant of any ?T
// (std's read-stdin-str returns ?str and matches err ->). Must be reported.
func TestExhaustiveMatchOkNilNoWildcardNotReported(t *testing.T) {
	src := `f = () () {
    b: {
        ok -> n = 1

        nil -> n = 2
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 1 {
		t.Fatalf("expected 1 match-nonex for ok+nil (err missing), got %d: %v", n, results)
	}
	if !strings.Contains(results[0].Message, "err") {
		t.Errorf("expected message to list missing err, got: %s", results[0].Message)
	}
}

// ok + nil + err without a wildcard IS exhaustive. Must NOT report.
func TestExhaustiveMatchOkNilErrNoWildcardNotReported(t *testing.T) {
	src := `f = () () {
    b: {
        ok -> n = 1

        nil -> n = 2

        err -> n = 3
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for ok+nil+err match, got %d: %v", n, results)
	}
}

// A match over a plain (non-option) value tests no option variant, so it is out
// of scope for this rule and must NOT be reported (low-noise).
func TestNonExhaustiveMatchPlainValueNotReported(t *testing.T) {
	src := `f = () () {
    x = 1
    x: {
        1 -> n = 1

        2 -> n = 2
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for non-option match, got %d: %v", n, results)
	}
}

// Bare guard blocks (`{ cond -> body }`) have no matched expression and must
// never be reported.
func TestNonExhaustiveMatchGuardBlockNotReported(t *testing.T) {
	src := `f = () () {
    n = 0
    {
        n > 1 -> n = 1

        n > 0 -> n = 2
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for guard block, got %d: %v", n, results)
	}
}

// Each match is reported exactly once, even when nested inside another match's
// arm body (chained arms must not be double-reported).
func TestNonExhaustiveMatchReportedOnce(t *testing.T) {
	src := `f = () () {
    a = stat-size("x")
    a: {
        ok -> {
            b = stat-size("y")
            b: {
                ok -> n = 1
            }
        }
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 2 {
		t.Fatalf("expected exactly 2 match-nonex (one per match), got %d: %v", n, results)
	}
}

// GUARD TEST FOR THE RULE'S RAISON D'ÊTRE.
//
// The parser's [RAL] check ("option match must handle all branches") only fires
// when matchedIsOption() can prove the subject is an option, and that helper
// only recognises a bare *Identifier whose type is already in p.sem.VarTypes
// with a "?" prefix. A match on a CALL RESULT (`foo(): { ok -> ... }`) therefore
// parses cleanly with NO [RAL] diagnostic — this is the gap that
// ValidateNonExhaustiveMatch exists to cover.
//
// If someone "simplifies" this rule away because [RAL] looks like it subsumes
// it, this test fails: proof that the two checks are complementary, not
// redundant.
func TestNonExhaustiveMatchCallSubject(t *testing.T) {
	src := `foo = () (v ?i64) {
    v = 1
}
bar = () {
    foo(): {
        ok -> io.outln('ok')
    }
}
`
	// Must parse WITHOUT error — i.e. [RAL] stays silent here.
	prog := parseProg(t, src)

	// ...and the checker rule must catch it.
	results := ValidateNonExhaustiveMatch(prog)
	if n := countMatchNonex(results); n != 1 {
		t.Fatalf("expected 1 match-nonex for call-subject match, got %d: %v", n, results)
	}
	if !strings.Contains(results[0].Message, "foo()") {
		t.Errorf("expected message to name the callee, got: %s", results[0].Message)
	}
}

// Tagged enums are NOT options. `q: { ok(v) -> ... fail -> ... }` matches the
// enum's OWN variants — `ok` and `fail` merely share a name with the option
// vocabulary. Demanding nil/err arms there is a false positive: the match is
// already exhaustive over its enum (this is what tests/tagged-enum*.no hit).
func TestNonExhaustiveMatchTaggedEnumNotReported(t *testing.T) {
	src := `e-res {
    ok(v str),
    fail,
}
show = (q e-res) {
    q: {
        ok(v) -> print('show: ' - v)

        fail -> print('show: fail')
    }
}
`
	prog := parseProg(t, src)
	// Sanity: the enum type must actually be known, otherwise this test would
	// pass for the wrong reason (unknown types stay reported by design).
	if _, ok := prog.Sem.EnumVariants["e-res"]; !ok {
		t.Fatalf("parser did not register enum e-res: %v", prog.Sem.EnumVariants)
	}
	results := ValidateNonExhaustiveMatch(prog)
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for tagged-enum match, got %d: %v", n, results)
	}
}

// Same exemption must hold for a method declared with the `=` form
// (`box.report = (q e-res)`): parseMethodDefinition renames the function to
// "box.report" AFTER parseFunctionDefinition registered param/local types
// under the bare "report", so without re-keying FuncVarTypes the subject
// lookup falls back to the globals, misses `q`, and the enum match is falsely
// reported as a non-exhaustive option match (tests/tagged-enum-cross-fn.no).
func TestNonExhaustiveMatchTaggedEnumEqFormMethod(t *testing.T) {
	src := `e-res {
    ok(v str),
    fail,
}
box {
    n i64
}
box.report = (q e-res) {
    q: {
        ok(v) -> print('box.ok: ' - v)

        fail -> print('box.fail')
    }
}
`
	prog := parseProg(t, src)
	// Sanity: the param type must be registered under the FULL method name.
	if tt, ok := prog.Sem.FuncVarType("box.report", "q"); !ok || tt != "e-res" {
		t.Fatalf("FuncVarTypes not re-keyed to full method name: q=%q ok=%v", tt, ok)
	}
	results := ValidateNonExhaustiveMatch(prog)
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for tagged-enum match in `=`-form method, got %d: %v", n, results)
	}
}

// Matches synthesised by the `?=` lowering (`a ?= x`) have no source `{ ... }`
// block to add arms to — MatchEndPos stays zero. Reporting them produces
// diagnostics the user literally cannot act on (they name internal temporaries
// like `__opt_1`), so they must be skipped.
func TestNonExhaustiveMatchSynthesizedUnwrapNotReported(t *testing.T) {
	// `?=` is only legal inside a function with an option-typed result param
	// (it returns the failure through it). The arithmetic RHS is what forces the
	// lowering to unwrap each operand into a synthetic `__opt_N` match — that is
	// the shape reported in tests/safe-index.no.
	src := `f = () (r ?i64) {
    b ?i64 = 1
    c ?i64 = 2
    d ?i64 = 3
    a ?= b + c + d
    r = a
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for synthesized ?= match, got %d: %v", n, results)
	}
}

// Counterpart of the above: a call-subject match that DOES handle all three
// variants must not be reported (the rule is about exhaustiveness, not about
// the subject shape).
func TestExhaustiveMatchCallSubjectNotReported(t *testing.T) {
	src := `foo = () (v ?i64) {
    v = 1
}
bar = () {
    foo(): {
        ok -> io.outln('ok')

        nil -> io.outln('nil')

        err -> io.outln('err')
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex when all variants are handled, got %d: %v", n, results)
	}
}

// A combined `nil || err` arm claims BOTH failure variants, so `ok` + `nil || err`
// is exhaustive and must NOT be reported. This is the case the parser's [RAL]
// check already accepts; the exhaust checker must agree. Regression guard for the
// bug where armVariantName returned only the first listed pattern (nil) and left
// err looking missing.
func TestExhaustiveMatchOkCombinedNilErrNotReported(t *testing.T) {
	src := `f = () () {
    b: {
        ok -> {
            n = 1
        }

        nil || err -> {
            n = 2
        }
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 0 {
		t.Fatalf("expected 0 match-nonex for ok + (nil || err), got %d: %v", n, results)
	}
}

// An `err || ok` combined arm that omits nil must still report the missing nil
// arm: the combined-pattern handling must collect exactly the listed variants and
// no more.
func TestNonExhaustiveMatchErrOkCombinedMissingNil(t *testing.T) {
	src := `f = () () {
    b: {
        err || ok -> {
            n = 1
        }
    }
}
`
	results := ValidateNonExhaustiveMatch(parseProg(t, src))
	if n := countMatchNonex(results); n != 1 {
		t.Fatalf("expected 1 match-nonex for (err || ok) missing nil, got %d: %v", n, results)
	}
	if !strings.Contains(results[0].Message, "nil") {
		t.Errorf("expected message to list missing nil, got: %s", results[0].Message)
	}
}
