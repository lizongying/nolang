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
