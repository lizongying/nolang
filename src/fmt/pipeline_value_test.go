package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestFormatPreservesPipelineValue guards that `no fmt` keeps the
// short-circuit-pipeline-as-value form `x = A -> B -> V` in a `->` chain.
//
// The parser lowers `x = cond -> v` into an IfExpression that is NOT flagged
// RTStandalone (it is a VALUE, not a statement). The generic IfExpression
// printer used to fall through and emit `x = cond: { v }` — and `subject: { arms }`
// re-parses as a match-as-value, not a pipeline: `x = 2 > 1: { 42 }` silently
// assigns `1` (the boolean) instead of `42`. The formatter must round-trip the
// pipeline through the `->` syntax and stay idempotent.
func TestFormatPreservesPipelineValue(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "single node pipeline value",
			src: `main = () {
    x = 2 > 1 -> 42
    print(x)
}
`,
		},
		{
			name: "false cond keeps previous value",
			src: `main = () {
    y = 7
    y = 1 > 2 -> 42
    print(y)
}
`,
		},
		{
			name: "owned value in tail",
			src: `main = () {
    s = 'old'
    s = 1 > 2 -> 'new'
    print(s)
}
`,
		},
		{
			name: "chained pipeline value",
			src: `main = () {
    x = 2 > 1 -> 42 -> 43
    print(x)
}
`,
		},
		{
			name: "identifier node pipeline value",
			src: `main = () {
    z = 2 > 1 -> 42
    z = z -> 7
    print(z)
}
`,
		},
		{
			name: "block body pipeline value",
			src: `main = () {
    x = 2 > 1 -> {
        y = 40
        y + 2
    }
    print(x)
}
`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out := FormatFile(tc.src)
			if out != tc.src {
				t.Errorf("pipeline-value form was rewritten\n--- want ---\n%s\n--- got ---\n%s", tc.src, out)
			}
			// The formatted output must re-parse cleanly.
			p := parser.New(lexer.New(out))
			p.ParseProgram()
			if perrs := p.Errors(); len(perrs) > 0 {
				t.Fatalf("formatted output no longer parses:\n%s\n--- output ---\n%s", strings.Join(perrs, "\n"), out)
			}
			// The output must still be a pipeline value: an IfExpression whose
			// RTStandalone flag is NOT set at the top of the assignment.
			q := parser.New(lexer.New(out))
			prog := q.ParseProgram()
			fn := prog.Statements[0].(*parser.FunctionDefinition)
			st := fn.Body.Statements[0].(*parser.LetStatement)
			if _, ok := st.Value.(*parser.InfixExpression); ok && strings.Contains(tc.src, "->") {
				t.Errorf("pipeline lost entirely, value re-parsed as plain expression:\n%s", out)
			}
			// And formatting must be idempotent.
			if again := FormatFile(out); again != out {
				t.Errorf("formatting not idempotent\n--- first ---\n%s\n--- second ---\n%s", out, again)
			}
		})
	}
}
