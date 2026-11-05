package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// REGRESSION GUARD (removing an "ineffective" annotation can break parsing).
//
// `#{overflow=wrap}` before a bare `->` arm does nothing for overflow, but it
// is load-bearing syntactically: the parser's else-folding only inspects the
// directly preceding statement, so the annotation keeps `-> body` as a
// separate sibling instead of folding it onto the nearest `cond ->` (which
// would be rejected as a bare else outside an if-group, k3r7mbqd). Stripping
// the annotation therefore makes `no fmt` emit source it cannot read back.
// Real victim: notools/src/awk.no awk-is-numeric.
func TestFormatKeepsAnnotationBeforeBareElse(t *testing.T) {
	input := "f = (s str) (yes bool) {\n" +
		"    yes = false\n" +
		"    (idx < 2 && yes) {\n" +
		"        s.byte(idx) == 48 -> idx = idx + 1\n" +
		"\n" +
		"        #{overflow=wrap}\n" +
		"        -> yes = false\n" +
		"    }\n" +
		"}\n"

	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if !strings.Contains(out, "#{overflow=wrap}") {
		t.Fatalf("annotation before bare '->' was removed; output would not re-parse:\n%s", out)
	}
	if _, errs := FormatFileWithErrors(out); len(errs) > 0 {
		t.Fatalf("formatted output is not parseable: %v\n%s", errs, out)
	}

	// Idempotency: a second pass must not change anything.
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	relevant2, governed2 := checker.OverflowAnnotationRelevance(program2)
	out2 := FormatProgramWithOverflow(program2, out, relevant2, governed2)
	if out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}
