package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestFormatPreservesIndependentGuardArms guards that `no fmt` does not drop
// every branch after the first in a bare `{ cond -> body }` block whose arms are
// all non-wildcard (independent-guard form). Such a block is desugared by
// buildBareMatchDesugar into a RTMatchWrapper holding one sibling
// ExpressionStatement per arm; the formatter used to recurse into only the first
// arm and return, silently deleting the rest (the revwalk.no guard regression).
func TestFormatPreservesIndependentGuardArms(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    []string // substrings that must survive formatting
		wantLen int      // number of `->` arms expected in output
	}{
		{
			name: "two guards",
			src: `resolve = (ref str) (tags-ok bool) {
    {
        tags-ok -> return
        ref.len-bytes() == 40 -> return
    }
}
`,
			want:    []string{"tags-ok ->", "ref.len-bytes() == 40 ->"},
			wantLen: 2,
		},
		{
			name: "guard with block body",
			src: `resolve = (ref str) (tags-ok bool) {
    {
        tags-ok -> return
        ref.len-bytes() == 40 -> {
            o = ref
        }
    }
}
`,
			want:    []string{"tags-ok ->", "ref.len-bytes() == 40 ->", "o = ref"},
			wantLen: 2,
		},
		{
			name: "three guards",
			src: `emit = (i i64) (out str) {
    {
        i + 1 < 16 -> out = 'a'
        i + 2 < 16 -> out = 'b'
        i + 3 < 16 -> out = 'c'
    }
}
`,
			want:    []string{"< 16 -> out = 'a'", "< 16 -> out = 'b'", "< 16 -> out = 'c'"},
			wantLen: 3,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out, ok, errs := formatProgram(tc.src)
			if !ok {
				t.Fatalf("format failed: %v", errs)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("arm %q dropped by formatter:\n%s", w, out)
				}
			}
			if got := strings.Count(out, "->"); got != tc.wantLen {
				t.Errorf("expected %d arms, got %d in:\n%s", tc.wantLen, got, out)
			}
			// Output must re-parse and be idempotent (house style separates arms
			// with a blank line, which must be stable across repeated runs).
			l := lexer.New(out)
			p := parser.New(l)
			p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Errorf("formatted output no longer parses: %v\n%s", p.Errors(), out)
			}
			out2, _, _ := formatProgram(out)
			if out2 != out {
				t.Errorf("formatter not idempotent:\n--- 1 ---\n%s\n--- 2 ---\n%s", out, out2)
			}
		})
	}
}
