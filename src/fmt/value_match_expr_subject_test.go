package fmt

// value_match_expr_subject_test.go — `r = a % b: { … }` 的 formatter 冪等門禁。
//
// 在 parser 把 `: {` 附加到中綴主語之前，`no fmt` 會把這段原始碼印成
// `r = a % b` + 一條獨立的裸 match（兩條陳述），語義完全走樣。修好 parse
// 形狀後 formatter 直接從 SurfaceMatch 渲然主語即可，但必須保證：
//   ① 不折回兩條陳述；② 不漏 `__match_subj` 之類合成名；③ 二次 format
//      輸出逐字相同；④ 格式化結果仍能重新 parse。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

const valueMatchExprSubjectFmtSrc = `f = (seconds i64) (out str) {
    rem = seconds % 3600: {
        ok -> it

        -> 0
    }
    sec = rem % 60: {
        ok -> it

        -> 0
    }
    out = rem.to-str() - ':' - sec.to-str()
}
`

func TestFormatValueMatchExprSubjectIdempotent(t *testing.T) {
	out, ok, errs := formatProgram(valueMatchExprSubjectFmtSrc)
	if !ok {
		t.Fatalf("format failed: %v", errs)
	}
	for _, want := range []string{
		"rem = seconds % 3600: {",
		"sec = rem % 60: {",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted output lost the inline match subject %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "__match_subj") {
		t.Fatalf("formatted output leaked a synthetic match subject:\n%s", out)
	}

	out2, ok, errs := formatProgram(out)
	if !ok {
		t.Fatalf("second format failed: %v", errs)
	}
	if out2 != out {
		t.Fatalf("format is not idempotent:\n--- pass 1 ---\n%s\n--- pass 2 ---\n%s", out, out2)
	}

	p := parser.New(lexer.New(out))
	p.SkipLowering = true
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("formatted output no longer parses: %v\noutput:\n%s", p.Errors(), out)
	}
}
