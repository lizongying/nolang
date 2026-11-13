package parser

import (
	"strings"
	"testing"
)

// Parameter default values (`name type = expr`) were REMOVED from the language.
//
// Why: a default made a function's arity ambiguous (one declaration, many legal
// call shapes) and forced the MIR lowerer to re-lower the default expression at
// EVERY call site, which is the monomorphisation blow-up this removal kills.
// Optionality is now expressed with `?T`: the caller passes `nil` / `ok(...)`
// explicitly and the body handles it with `match`.
//
// The rejection must be a PARSER error (not a checker rule), so `no ast`,
// `no build`, `no fmt`, `no vet` and the LSP all see the same diagnostic. The
// code is the stable programmatic code E_DEFAULT_PARAM; the message is inline
// English on purpose (empty i18n ID — msgscan must not grow an empty catalog
// key, see (*Parser).rejectDefaultParam).
const defaultParamRef = "[E_DEFAULT_PARAM]"

func TestDefaultParameterIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line string
	}{
		{
			name: "plain",
			src:  "foo = (a i64, b i64 = 10) {\n}\n",
			line: "line 1,",
		},
		{
			// The exact shape the user asked to forbid: an option-typed
			// parameter with a default.
			name: "option-type-no-space",
			src:  "foo = (a i64, b ?i64=10) {\n}\n",
			line: "line 1,",
		},
		{
			name: "option-type-spaced",
			src:  "foo = (a i64, b ?i64 = nil) {\n}\n",
			line: "line 1,",
		},
		{
			name: "method",
			src:  "pt {\n    x i64\n}\npt.m = (a i64, b i64 = 2) {\n}\n",
			line: "line 4,",
		},
		{
			name: "colon-syntax",
			src:  "foo: (a i64, b i64 = 2) {\n}\n",
			line: "line 1,",
		},
		{
			// parseArrayTypeMethodDefinition is a SEPARATE parameter parser
			// (decl.go); it needs the same rejection.
			name: "slice-method",
			src:  "[]i64.sum = (n i64 = 1) {\n}\n",
			line: "line 1,",
		},
		{
			// Error recovery must track parenthesis depth: stopping at the
			// inner comma would turn one precise error into a cascade.
			name: "call-in-default",
			src:  "foo = (a i64, b i64 = bar(1, 2)) {\n}\n",
			line: "line 1,",
		},
		{
			name: "array-literal-default",
			src:  "foo = (a []i64 = [1, 2]) {\n}\n",
			line: "line 1,",
		},
		{
			// Default on a non-trailing parameter (the old checker rule
			// demanded defaults be last; the parser now rejects any of them).
			name: "non-trailing",
			src:  "foo = (a i64 = 1, b i64) {\n}\n",
			line: "line 1,",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseErrors(t, tc.src)
			if !strings.Contains(got, defaultParamRef) {
				t.Fatalf("want %s, got: %q", defaultParamRef, got)
			}
			if n := strings.Count(got, defaultParamRef); n != 1 {
				t.Fatalf("want exactly 1 diagnostic (no cascade), got %d: %q", n, got)
			}
			if !strings.Contains(got, tc.line) {
				t.Fatalf("diagnostic should point at %q, got: %q", tc.line, got)
			}
		})
	}
}

// Controls: every shape that merely LOOKS like a default must keep parsing.
// `?T` is the sanctioned way to express optionality, and `=` remains legal as
// the function-definition marker, an enum's explicit discriminant, a let
// initializer and an assignment.
func TestLegalFormsWithEqualsStayAccepted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"option-param", "foo = (a i64, b ?i64) {\n}\n"},
		{"option-param-multi", "foo = (a ?i64, b ?str, c ?[]i64) {\n}\n"},
		{"variadic", "foo = (a ..i64) (n i64) {\n    n = 0\n}\n"},
		{"enum-explicit-value", "e {\n    a = 1\n    b\n}\n"},
		{"let-init", "f = () (n i64) {\n    x i64 = 10\n    n = x\n}\n"},
		{"assignment", "f = () (n i64) {\n    n = 1\n    n = n + 1\n}\n"},
		{"track-caller-plain-loc", "#{track-caller}\ng = (msg str, loc str) {\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseErrors(t, tc.src); got != "" {
				t.Fatalf("must keep parsing, got: %q", got)
			}
		})
	}
}
