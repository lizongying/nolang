package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD for the spurious "cannot assign err value to ?i64 variable"
// (trace 15w45dqk) when using `?=` with a builtin option-return function.
//
// Root cause: the desugared option match emits a synthetic `it` binding per
// arm. The nil/err arms carry Type annotations ("nil" / "err") which the
// checker recorded into varTypes["it"]; the ok/wildcard arm's shared `it`
// binding has NO type annotation when the matched type is unknown at parse
// time (builtin option-return calls like stat-size/fstat-size), so the stale
// "err" leaked into the ok arm and every `x = it` / `v = it` in it inferred
// "err" — later surfacing as a false type-mismatch error.
//
// The checker now clears the stale type when it encounters an untyped shared
// `it` binding, so the arm body infers no (wrong) type instead of "err".
func TestUnwrapAssignBuiltinNoErrValueFalsePositive(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "unwrap into result param from builtin",
			src: `f = (fd fd) (size ?i64) {
    size ?= fstat-size(.fd)
}
`,
		},
		{
			name: "unwrap into local then re-assign to result param",
			src: `g = () (v ?i64) {
    v ?= stat-size('/tmp/x')
    sz = v
}
`,
		},
		{
			name: "unwrap then arithmetic on unwrapped value",
			src: `h = () (v ?i64) {
    v ?= file-size('/tmp/x')
    r = v + 1
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.src))
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			results := ValidateTypes(prog)
			for _, r := range results {
				if r.TraceID == "15w45dqk" || strings.Contains(r.Message, "cannot assign err value") {
					t.Errorf("false positive: %s", r.Message)
				}
			}
		})
	}
}
