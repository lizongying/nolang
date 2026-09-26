package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// printfDeprTraceID is defined in deprecated_printf.go; this file pins the
// behaviour of ValidateDeprecatedPrintf through the production entry point.

// validatePrintfDepr runs ValidateTypes (which appends ValidateDeprecatedPrintf)
// and returns only the removed-builtin diagnostics.
func validatePrintfDepr(t *testing.T, src string) []ValidateResult {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\nsrc:\n%s", errs, src)
	}
	var out []ValidateResult
	for _, r := range ValidateTypes(prog) {
		if r.TraceID == printfDeprTraceID {
			out = append(out, r)
		}
	}
	return out
}

// TestPrintfDeprFlagsCalls pins the core rule: every call to printf / eprintf
// — bare or `fmt.`-qualified, with or without format fields, in any statement
// or expression position — is a hard, source-located error carrying the
// migration hint.
func TestPrintfDeprFlagsCalls(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantSub  string
		wantHits int
	}{
		{
			name:     "bare printf, no field",
			src:      "printf('plain')\n",
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			name:     "bare printf with field",
			src:      "x = 7\nprintf('x={x}')\n",
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			name:     "bare eprintf",
			src:      "eprintf('plain')\n",
			wantSub:  "eprintf() is removed",
			wantHits: 1,
		},
		{
			name:     "zero-arg printf is still a call",
			src:      "printf()\n",
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			name:     "fmt-qualified printf",
			src:      "x = 7\nfmt.printf('x={x}')\n",
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			name:     "fmt-qualified eprintf",
			src:      "fmt.eprintf('e')\n",
			wantSub:  "eprintf() is removed",
			wantHits: 1,
		},
		{
			// Nested as an argument: the walker must descend into call args,
			// not just top-level statement expressions.
			name:     "as an argument",
			src:      "x = 7\nprint(printf('x={x}'))\n",
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			// Inside a loop body.
			name: "inside a while body",
			src: `x = 7
i = 0
(i < 2) {
  printf('loop={i}')
  i = i + 1
}
`,
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			// Inside a match / if arm.
			name: "inside a match arm",
			src: `x = 7
y: {
  x == 7 -> printf('a={x}')
  _ -> print('b')
}
`,
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			// Inside a function body (the FunctionDefinition case).
			name: "inside a function body",
			src: `f = () () {
  printf('in f')
}
`,
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			// The generic TEMPLATE is reported (the generated `__` instances
			// are skipped, so this is the one and only hit).
			name: "inside a generic template",
			src: `f = (v t) () {
  printf('v={v}')
}
`,
			wantSub:  "printf() is removed",
			wantHits: 1,
		},
		{
			// Two calls on two lines: one diagnostic each.
			name:     "two calls",
			src:      "printf('a')\neprintf('b')\n",
			wantSub:  "() is removed",
			wantHits: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validatePrintfDepr(t, tc.src)
			if len(got) != tc.wantHits {
				t.Fatalf("got %d printf-depr, want %d: %+v", len(got), tc.wantHits, got)
			}
			for _, r := range got {
				if !strings.Contains(r.Message, tc.wantSub) {
					t.Errorf("message %q does not contain %q", r.Message, tc.wantSub)
				}
				if r.Line <= 0 || r.Column <= 0 {
					t.Errorf("diagnostic is not source-located: %+v", r)
				}
			}
		})
	}
}

// TestPrintfDeprIgnoresSurvivingBuiltins is the over-suppression guard: the
// print family that still lowers (print / eprint / format / sprintf) must NOT
// be touched. `sprintf` in particular shares the prefix and must stay silent.
func TestPrintfDeprIgnoresSurvivingBuiltins(t *testing.T) {
	src := `x = 7
print('x={x}')
eprint('e')
a = format('x={x}')
b = sprintf('y={x}')
print('result={x}', 42, 'result={x}')
`
	if got := validatePrintfDepr(t, src); len(got) != 0 {
		for _, r := range got {
			t.Errorf("false positive at L%d:C%d: %s", r.Line, r.Column, r.Message)
		}
		t.Fatalf("expected no printf-depr on the surviving print family, got %d", len(got))
	}
}

// TestPrintfDeprIgnoresUserMethod pins the shape discrimination: only a bare
// identifier and a `fmt.`-qualified callee count. A user method that merely
// shares the name (`p.printf()`) must not be flagged — `callName` flattens
// every DotExpression to its property, so matching on that would misfire.
//
// Note the receiver is NOT `fmt`, which is the whole point: the rule keys on
// the receiver being the `fmt` module, not on the property name alone.
func TestPrintfDeprIgnoresUserMethod(t *testing.T) {
	src := `w { n i64 }

w.printf = () (r i64) { .n }

p = w { n: 3 }
p.printf()
`
	if got := validatePrintfDepr(t, src); len(got) != 0 {
		for _, r := range got {
			t.Errorf("false positive at L%d:C%d: %s", r.Line, r.Column, r.Message)
		}
		t.Fatalf("expected no printf-depr on a user-defined method, got %d", len(got))
	}
}

// TestPrintfDeprDistinguishesUserMethodFromBuiltin is the sharp version of the
// test above: a file that has BOTH a user method named printf AND a genuine
// bare printf() call must report exactly one diagnostic, on the bare call.
func TestPrintfDeprDistinguishesUserMethodFromBuiltin(t *testing.T) {
	src := `w { n i64 }

w.printf = () (r i64) { .n }

p = w { n: 3 }
print(p.printf())
printf('boom')
`
	got := validatePrintfDepr(t, src)
	if len(got) != 1 {
		t.Fatalf("got %d printf-depr, want exactly 1 (the bare call): %+v", len(got), got)
	}
	if got[0].Line != 7 {
		t.Errorf("diagnostic on L%d, want L7 (the bare printf call, not the user method at L5)", got[0].Line)
	}
}

// TestPrintfDeprSkipsMonomorphized pins the generated-code skip: a
// monomorphized instance (name contains `__`) is compiler output, so a
// diagnostic against it would point at a line the user never wrote.
func TestPrintfDeprSkipsMonomorphized(t *testing.T) {
	src := `f__i64 = () () {
  printf('generated')
}
`
	if got := validatePrintfDepr(t, src); len(got) != 0 {
		for _, r := range got {
			t.Errorf("diagnostic on generated code at L%d:C%d: %s", r.Line, r.Column, r.Message)
		}
		t.Fatalf("expected no printf-depr on a monomorphized instance, got %d", len(got))
	}
}
