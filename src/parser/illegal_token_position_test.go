package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestInfixIllegalTokenIsReported 釘住「中綴位置的詞法錯誤 token 必須報告」。
//
// `y = 1 → 2` 裡 `→` 不是任何中綴運算子，parseExpression 在此結束並回傳
// `1`，若不上報，上層語句掃描會把 `→` 靜默跳過 ⇒ 源碼被解析成兩條陳述
// (`y = 1` / `2`) 且零診斷。`no fmt` 於是會把非法字元從源碼與 Markdown
// 代碼塊裡悄悄刪掉（覆寫文檔內容），必須留一個錯誤讓格式化放棄該檔案/塊。
func TestInfixIllegalTokenIsReported(t *testing.T) {
	src := "y = 1 → 2\n"
	l := lexer.New(src)
	p := New(l)
	p.ParseProgram()

	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatalf("expected a diagnostic for the infix illegal token, got none")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "→") {
		t.Fatalf("diagnostic must show the rune itself, got: %s", joined)
	}
	for _, leak := range []string{`\u0086`, `\u00e2`, `\u0092`} {
		if strings.Contains(joined, leak) {
			t.Fatalf("diagnostic leaked a raw byte escape %s instead of the rune: %s", leak, joined)
		}
	}
}

// TestNonASCIIIdentifierRejectedReadably 釘住 `可以 = 1` 的診斷品質：
// 非 ASCII 識別字目前不支援（codegen 直發符號名），但錯誤訊息必須說清楚，
// 不能是舊版那種 `illegal token "\u008f"` 式的半顆位元組噪音。
func TestNonASCIIIdentifierRejectedReadably(t *testing.T) {
	src := "可以 = 1\n"
	l := lexer.New(src)
	p := New(l)
	p.ParseProgram()

	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatalf("expected a diagnostic for the non-ASCII identifier, got none")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "non-ASCII") {
		t.Fatalf("diagnostic must explain that identifiers are ASCII-only, got: %s", joined)
	}
	if strings.Contains(joined, `\u008f`) {
		t.Fatalf("diagnostic leaked a byte escape instead of the rune: %s", joined)
	}
}
