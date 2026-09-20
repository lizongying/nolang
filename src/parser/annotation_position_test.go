package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseAnnotationErrs parses src and returns the parser diagnostics.
func parseAnnotationErrs(t *testing.T, src string) []string {
	t.Helper()
	p := New(lexer.New(src))
	if prog := p.ParseProgram(); prog == nil {
		t.Fatal("ParseProgram returned nil")
	}
	return p.Errors()
}

// TestAnnotationPrefixRejected pins the annotation placement rule: `#{...}` may
// only be written on its own line above the target, or trailing on the target's
// line. The same-line prefix form is a parse error — for statements, struct
// fields, match arms and FFI declarations alike. The same rule is what the LSP
// reports (it surfaces parser diagnostics verbatim), so this test is the single
// source of truth for both surfaces.
func TestAnnotationPrefixRejected(t *testing.T) {
	cases := map[string]string{
		"statement":    "#{overflow = wrap} x i8 = a + 100\n",
		"index-out":    "#{index-out = 0} res = arr[i]\n",
		"struct field": "pt {\n    x i64\n}\ns {\n    #{inline} p pt\n}\n",
		"match arm":    "f = (n i64) (out i64) {\n    n: {\n        1 -> out = 1\n        #{overflow = wrap} 2 -> out = 2\n    }\n}\n",
		"ffi decl":     "#{c} puts = (s str) ()\n",
		// `if <cond> #{...} {` — the annotation sits between the condition and the
		// body brace. That scan is lenient and used to swallow the group whole
		// (annotation neither applied nor reported), so it is pinned here.
		"if condition": "x = 1\nif x > 0 #{overflow = wrap} {\n    print(x)\n}\n",
		"else body":    "x = 1\nif x > 0 {\n    print(x)\n} else #{overflow = wrap} {\n    print(x)\n}\n",
	}
	for name, src := range cases {
		errs := parseAnnotationErrs(t, src)
		if len(errs) == 0 {
			t.Errorf("%s: same-line prefix annotation must be rejected, got no errors", name)
			continue
		}
		if joined := strings.Join(errs, "\n"); !strings.Contains(joined, "前方同一行") {
			t.Errorf("%s: errors %v do not mention the prefix rule", name, errs)
		}
	}
}

// TestAnnotationAboveAndTrailingAccepted is the control: both legal positions
// (own line above, same-line trailing) must stay accepted — including the
// trailing struct-field form `p pt #{inline}` that replaced the prefix spelling.
func TestAnnotationAboveAndTrailingAccepted(t *testing.T) {
	cases := map[string]string{
		"above statement":    "a i8 = 100\n#{overflow = wrap}\nb i8 = a + 100\n",
		"trailing statement": "a i8 = 100\nb i8 = a + 100 #{overflow = wrap}\n",
		"above field":        "pt {\n    x i64\n}\ns {\n    #{inline}\n    p pt\n}\n",
		"trailing field":     "pt {\n    x i64\n}\ns {\n    p pt #{inline}\n}\n",
		"trailing index-out": "arr [3]i64 = [1, 2, 3]\ni = 5\nres = arr[i] #{index-out = 7}\n",
		// Control for the `if` scan: an own-line annotation above the first
		// statement *inside* the body is the normal legal spelling and must not
		// be caught by the new check.
		"above statement in if body": "x = 1\nif x > 0 {\n    #{overflow = wrap}\n    b i8 = 100\n}\n",
	}
	for name, src := range cases {
		if errs := parseAnnotationErrs(t, src); len(errs) > 0 {
			t.Errorf("%s: legal placement reported errors: %v", name, errs)
		}
	}
}

// TestTrailingAnnotationBindsToPrecedingStatement guards the reason the
// trailing form is worth allowing: `stmt #{...}` must attach to the statement it
// trails. Attaching it to the *next* statement made `x = v[i] #{index-out=0}`
// (the spelling the LSP quickfix inserts) silently apply to the wrong target.
func TestTrailingAnnotationBindsToPrecedingStatement(t *testing.T) {
	src := "a i8 = 100\nb i8 = a + 100 #{overflow = wrap}\nprint(b)\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(prog.Statements) != 3 {
		t.Fatalf("statements = %d, want 3", len(prog.Statements))
	}
	anns := p.sem.RawAnnotationsOf(prog.Statements[1])
	if len(anns) != 1 || anns[0].Key != "overflow" {
		t.Fatalf("trailed statement annotations = %v, want one overflow entry", anns)
	}
	// The following statement must NOT have inherited it.
	if next := p.sem.RawAnnotationsOf(prog.Statements[2]); len(next) != 0 {
		t.Fatalf("next statement wrongly carries %v", next)
	}
}
