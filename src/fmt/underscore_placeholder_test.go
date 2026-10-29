package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestFormatPreservesUnderscorePlaceholder guards that `no fmt` keeps the `_`
// no-op placeholder the corpus writes in match arms (`-> _`, 2800+ sites in
// src/std + test/std + tests) and as a bare statement in a block.
//
// `_` parses to an ExpressionStatement whose Expression is nil — the same shape
// as the `;`-separator and synthetic placeholders the formatter filters out —
// so the arm-body / block filters used to swallow it and print a bare `->`.
// That is not a cosmetic round-trip: an empty else arm means "default zero
// value / nothing written", while `-> _` is the author spelling "deliberately
// unhandled". Dropping it also made `no fmt` non-idempotent on every file
// using the idiom (first run removed `_`, so `-w` churned sources).
func TestFormatPreservesUnderscorePlaceholder(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// 值語境的 option 解包 match（src/std/str.no decode-cp 的形態）
			name: "value_context_wildcard_arm",
			src: `f = (i i64) (val byte) {
    b-opt = .byte(i)
    b = b-opt: {
        ok -> it

        -> _
    }
    b
}
`,
			want: `f = (i i64) (val byte) {
    b-opt = .byte(i)
    b = b-opt: {
        ok -> it

        -> _
    }
    b
}
`,
		},
		{
			// 裸 match（陳述語境）的兜底臂
			name: "bare_match_wildcard_arm",
			src: `g = (x i64) (val i64) {
    a = 0
    {
        x == 1 -> a = 1

        -> _
    }
    a
}
`,
			want: `g = (x i64) (val i64) {
    a = 0
    {
        x == 1 -> a = 1

        -> _
    }
    a
}
`,
		},
		{
			// 非通配臂帶 `_` 體：同一路徑，一併守
			name: "named_arm_body",
			src: `h = (x i64) (val i64) {
    r = x: {
        1 -> 100

        -> _
    }
    r
}
`,
			want: `h = (x i64) (val i64) {
    r = x: {
        1 -> 100

        -> _
    }
    r
}
`,
		},
		{
			name: "bare_statement_in_block",
			src: `k = () {
    _
    print('hi')
}
`,
			want: `k = () {
    _
    print('hi')
}
`,
		},
		{
			// 顯式 block 內的單條 `_` 收攏成行內形態（仍可重解析、且收斂）
			name: "block_body_collapses_to_inline",
			src: `m = (x i64) (val i64) {
    r = x: {
        1 -> 100

        -> {
            _
        }
    }
    r
}
`,
			want: `m = (x i64) (val i64) {
    r = x: {
        1 -> 100

        -> _
    }
    r
}
`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out, ok, errs := formatProgram(tc.src)
			if !ok {
				t.Fatalf("format failed: %v", errs)
			}
			// formatProgram 不保證尾換行，兩側一律去尾再比，避免假失敗。
			if got := strings.TrimRight(out, "\n"); got != strings.TrimRight(tc.want, "\n") {
				t.Errorf("unexpected output:\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
			// `_` 必須在輸出裡，且不能被降成空臂
			if !strings.Contains(out, "-> _") && !strings.Contains(out, "\n    _\n") {
				t.Errorf("`_` placeholder dropped by formatter:\n%s", out)
			}
			// 輸出要能重解析，並收斂（第二次格式化不再改變源碼）
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
