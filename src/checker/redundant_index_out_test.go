package checker

// D11 regression guard: the `#{index-out = DEF}` annotation was removed.
// Any residual occurrence must be rejected at parse time with `idxourmv`,
// and programs that consume the index option explicitly must still parse
// cleanly. The old "redundant idxoutrd HINT" lint no longer exists.

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseErrors parses src on the surface AST and returns the joined parser
// error strings (does not fail the test on errors, so it can assert on them).
func parseErrors(src string) string {
	p := parser.New(lexer.New(src))
	p.SkipLowering = true
	p.ParseProgram()
	return strings.Join(p.Errors(), "\n")
}

func TestResidualIndexOutAnnotationRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "standalone line",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    #{index-out=zero}
    x = a[0]
}`,
		},
		{
			name: "trailing annotation",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    x = a[0] #{index-out=zero}
}`,
		},
		{
			name: "merged with other annotation keys",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    #{overflow=wrap, index-out=zero}
    x = a[0]
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseErrors(c.src)
			if !strings.Contains(got, "[idxourmv]") {
				t.Fatalf("expected residual index-out to be rejected with idxourmv, got:\n%s", got)
			}
			if !strings.Contains(got, "D11") {
				t.Fatalf("idxourmv message must state D11 removed the annotation, got:\n%s", got)
			}
			// The message must NOT recommend re-adding #{index-out} as a fix.
			if strings.Contains(got, "use `#{index-out") || strings.Contains(got, "annotate `#{index-out") {
				t.Fatalf("idxourmv must not suggest re-adding #{index-out}, got:\n%s", got)
			}
		})
	}
}

func TestD11ExplicitConsumptionFormsParseClean(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "unwrap assign propagation",
			src: `f = () (res ?i64) {
    a [4]i64 = [1, 2, 3, 4]
    x ?= a[0]
    res ?= x + 1
}`,
		},
		{
			name: "value match",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    x = a[0]: {
        ok -> it
        -> 0
    }
}`,
		},
		{
			name: "place match",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    a[0]: {
        ok -> it = 9
        -> _
    }
}`,
		},
		{
			name: "option target binding",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    x ?i64 = a[0]
    _ = x
}`,
		},
		{
			name: "explicit discard",
			src: `f = () {
    a [4]i64 = [1, 2, 3, 4]
    _ = a[0]
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseErrors(c.src)
			if strings.Contains(got, "[idxourmv]") {
				t.Fatalf("valid D11 form must not emit idxourmv, got:\n%s", got)
			}
		})
	}
}
