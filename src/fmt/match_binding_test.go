package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestFormatPreservesMatchArmBinding guards that `no fmt` does not drop match-arm
// destructuring bindings. The compiler desugars `ok(v) -> ...` / `rect(w, h) -> ...`
// into synthetic `v = matched` / `w = it.f0` statements; the formatter must
// reconstruct the `(v)` / `(w, h)` suffix from those, otherwise the formatted
// source loses the binding and the arm body's references become undefined.
func TestFormatPreservesMatchArmBinding(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // substrings that must appear in the formatted output
	}{
		{
			name: "option single binding",
			src: `main = () {
    r ?i64 = ok(42)
    r: {
        ok(v) -> print(v)
        nil -> print('n')
        err -> print('e')
    }
}
`,
			want: []string{"ok(v) ->", "nil ->", "err ->"},
		},
		{
			name: "enum single binding",
			src: `shape {
    circle(r f64),
    dot,
}

main = () {
    s shape = circle(1.0)
    s: {
        circle(r) -> print(r)
        dot -> print('d')
    }
}
`,
			want: []string{"circle(r) ->", "dot ->"},
		},
		{
			name: "enum multi binding",
			src: `shape {
    rect(w f64, h f64),
    dot,
}

main = () {
    s shape = rect(1.0, 2.0)
    s: {
        rect(w, h) -> print(w)
        dot -> print('d')
    }
}
`,
			want: []string{"rect(w, h) ->", "dot ->"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out := FormatFile(tc.src)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("formatted output lost %q\n--- output ---\n%s", w, out)
				}
			}
			// The formatted output must re-parse cleanly.
			p := parser.New(lexer.New(out))
			p.ParseProgram()
			if perrs := p.Errors(); len(perrs) > 0 {
				t.Fatalf("formatted output no longer parses:\n%s\n--- output ---\n%s", strings.Join(perrs, "\n"), out)
			}
			// And formatting must be idempotent.
			if again := FormatFile(out); again != out {
				t.Errorf("formatting not idempotent\n--- first ---\n%s\n--- second ---\n%s", out, again)
			}
		})
	}
}
