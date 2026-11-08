package lexer

import (
	"strings"
	"testing"
)

// TestNonASCIITextIsReportedAsWholeRunes 釘住「一個漢字 = 一個 token」。
//
// 舊版 isLetter 對 >= 0x80 的**位元組**做 unicode.IsLetter(rune(ch))，
// rune(0xE5) 是 `å`（U+00E5）⇒ 多字節字元的首字節被當成字母，識別字在
// 第二個位元組處斷掉：
//
//	可以  →  IDENT("\xe5") + ILLEGAL("\x8f") + ILLEGAL("\x8f")
//
// 於是 `no fmt --md` 把一個漢字報成看不懂的 `illegal token "\u008f"`。
// 現在每個漢字是一個完整碼點的 ILLEGAL，Literal 就是那個字本身。
func TestNonASCIITextIsReportedAsWholeRunes(t *testing.T) {
	l := New("可以 = 1\n")
	var illegal []Token
	for _, tok := range l.allTokens {
		if tok.Type == ILLEGAL {
			illegal = append(illegal, tok)
		}
	}
	if len(illegal) != 2 {
		t.Fatalf("expected 2 ILLEGAL tokens (one per rune), got %d: %v", len(illegal), illegal)
	}
	for i, want := range []string{"可", "以"} {
		if illegal[i].Literal != want {
			t.Fatalf("illegal[%d] = %q, want %q", i, illegal[i].Literal, want)
		}
		if !strings.Contains(illegal[i].ErrMsg, "non-ASCII") {
			t.Fatalf("illegal[%d] ErrMsg = %q, want a non-ASCII identifier explanation", i, illegal[i].ErrMsg)
		}
	}
}

// TestNonLetterRuneIsSingleIllegalToken 釘住「非字母的多字節碼點整顆報告」。
// `→`(E2 86 92) 必須是一個 Literal=="→" 的 ILLEGAL，而不是三個位元組碎片；
// 否則診斷訊息印出的是 `\u00e2\u0086\u0092`（`\u0086` 這種半顆位元組）。
func TestNonLetterRuneIsSingleIllegalToken(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"arrow", "x = 1 → 2\n", "→"},
		{"ideographic comma", "x = 1、2\n", "、"},
		{"full width paren", "x = （1\n", "（"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := New(tc.input)
			var illegal []Token
			for _, tok := range l.allTokens {
				if tok.Type == ILLEGAL {
					illegal = append(illegal, tok)
				}
			}
			if len(illegal) != 1 {
				t.Fatalf("expected exactly 1 ILLEGAL token, got %d (%v)", len(illegal), illegal)
			}
			if illegal[0].Literal != tc.want {
				t.Fatalf("illegal literal = %q, want %q", illegal[0].Literal, tc.want)
			}
			if illegal[0].ErrMsg != "" {
				t.Fatalf("punctuation must not get the identifier hint, got ErrMsg %q", illegal[0].ErrMsg)
			}
		})
	}
}

// TestUnderscorePrefixStaysASCII 釘住 `_` 前綴的 peek 判斷也是 ASCII 的：
// `_名` 不是識別字（`_` 單獨成為 UNDERSCORE），`名` 單獨報 ILLEGAL。
func TestUnderscorePrefixStaysASCII(t *testing.T) {
	l := New("_名 = 1\n")
	if got := l.allTokens[0]; got.Type != UNDERSCORE {
		t.Fatalf("first token = %s %q, want UNDERSCORE", got.Type, got.Literal)
	}
	if got := l.allTokens[1]; got.Type != ILLEGAL || got.Literal != "名" {
		t.Fatalf("second token = %s %q, want ILLEGAL %q", got.Type, got.Literal, "名")
	}
}

// TestASCIIIdentifierStillIntact 反向護欄：ASCII 識別字（含 `-`、數字）照舊。
func TestASCIIIdentifierStillIntact(t *testing.T) {
	l := New("read-line2 = 1\n")
	if got := l.allTokens[0]; got.Type != IDENT || got.Literal != "read-line2" {
		t.Fatalf("first token = %s %q, want IDENT %q", got.Type, got.Literal, "read-line2")
	}
}
