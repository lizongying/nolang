package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD for the spurious "argument N of 'f': expected 'str', got
// 'err'/'nil'" (trace 6fgg3htw) when a match's `ok` arm is written AFTER a
// sentinel (`nil`/`err`) arm and uses `it` as a plain payload value.
//
// Root cause: the option match emits a synthetic `it` binding per arm. The
// nil/err arms carry Type annotations ("nil"/"err") which ValidateFuncArgs
// recorded into the shared varTypes map; the later `ok` arm's `it` binding is
// UNTYPED whenever the matched variable's type is unresolved at parse time
// (e.g. a cross-module `?str` return). The untyped binding previously only
// inferred when `it` was absent from varTypes, so the stale "err"/"nil" leaked
// into the ok arm and `f(it)` was flagged as passing an err/nil where str was
// expected. ValidateTypes already clears the stale type; this guards that
// ValidateFuncArgs now does the same.
func TestMatchItAfterSentinelArmNoFalseArgType(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "ok after nil+err, same-module ?str",
			src: `trim = (a str) (b str) {
    b = a
}
get-val = (s str) (v ?str) {
    {
        s == 'x' -> {
            v = 'hello'
            return
        }
    }
    v = nil
}
use-it = (s str) (out str) {
    out = ''
    val = get-val(s)
    val: {
        nil -> return
        err -> return
        ok -> out = trim(it)
    }
}
`,
		},
		{
			name: "ok after nil, same-module ?str",
			src: `trim = (a str) (b str) {
    b = a
}
get-val = (s str) (v ?str) {
    v = nil
}
use-it = (s str) (out str) {
    out = ''
    val = get-val(s)
    val: {
        nil -> return
        err -> return
        ok -> out = trim(it)
    }
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
			for _, r := range ValidateTypes(prog) {
				if r.TraceID == "6fgg3htw" || strings.Contains(r.Message, "expected 'str', got") {
					t.Errorf("false positive from ValidateTypes: %s", r.Message)
				}
			}
			for _, r := range ValidateFuncArgs(prog, "") {
				if r.TraceID == "6fgg3htw" || strings.Contains(r.Message, "expected 'str', got") {
					t.Errorf("false positive from ValidateFuncArgs: %s", r.Message)
				}
			}
		})
	}
}
