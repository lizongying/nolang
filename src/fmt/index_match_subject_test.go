package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// A match whose subject is a safe-index expression (`parts[i]: { ok -> ... }`)
// is desugared by the parser into a synthetic `__match_subj_L_C` variable so the
// option `it` binding and ok/nil/err tag comparisons lower correctly. The
// formatter must NOT leak that synthetic name back into the source — it has to
// recover the original subject expression, otherwise the formatted output no
// longer parses (`IDENT: { ... }` is a label + block, not a match).
//
// Regression guard for TestFormatReparseClean/../std/txt.no (replace-n / txt-join).
func TestFormatIndexMatchSubjectNotDesugared(t *testing.T) {
	src := "f = (parts []txt) (s str) {\n    i <- [0..parts.len()): {\n        parts[i]: {\n            ok -> {\n                s = it\n            }\n\n            -> {\n                s = ''\n            }\n        }\n    }\n}\n"
	out, ok, errs := formatProgram(src)
	if !ok {
		t.Fatalf("format failed: %v", errs)
	}
	if strings.Contains(out, "__match_subj") {
		t.Fatalf("formatted output leaked synthetic match subject:\n%s", out)
	}
	if !strings.Contains(out, "parts[i]: {") {
		t.Fatalf("formatted output should render the original subject `parts[i]: {`, got:\n%s", out)
	}
	l := lexer.New(out)
	p := parser.New(l)
	p.SkipUnwrapLowering = true
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("formatted output no longer parses: %v\noutput:\n%s", p.Errors(), out)
	}
}
