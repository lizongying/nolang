package lexer

import "testing"

// rawStringValue 取输入中第一个 STRING token 的字面量，便于断言反引号串的内容。
func rawStringValue(t *testing.T, input string) string {
	t.Helper()
	l := New(input)
	for _, tok := range l.allTokens {
		if tok.Type == STRING {
			return tok.Literal
		}
	}
	t.Fatalf("no STRING token in %q", input)
	return ""
}

// TestRawStringKeepsFirstCharOfEachContinuationLine 钉住续行首字符不被吞掉。
// 旧实现用 SaveState/RestoreState 回卷「跳过换行 + 跳过空白 + 不是结尾」这段
// 扫描，而 LexerState 只存 token 游标，回卷是空操作 ⇒ 每行多读一个字符。
func TestRawStringKeepsFirstCharOfEachContinuationLine(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"two lines", "`\nline1\nline2\n`\n", "line1\nline2"},
		{"three lines", "`\na\nb\nc\n`\n", "a\nb\nc"},
		{"indented continuation", "`\n  a\n  b\n`\n", "  a\n  b"},
		{"leading blanks only line", "`\na\n   \nb\n`\n", "a\n   \nb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawStringValue(t, tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRawStringAcceptsCRLF 钉住 Windows 源码：开头 ` 后可跟 CRLF，且串内 CR
// 一律丢弃（与 Go 规范一致），CRLF 与 LF 源码得到逐字节相同的值。
func TestRawStringAcceptsCRLF(t *testing.T) {
	crlf := "s = `\r\nline1\r\nline2\r\n`\r\n"
	lf := "s = `\nline1\nline2\n`\n"
	if got, want := rawStringValue(t, crlf), rawStringValue(t, lf); got != want {
		t.Fatalf("CRLF input yields %q, LF input yields %q; want identical", got, want)
	}
	if got := rawStringValue(t, crlf); got != "line1\nline2" {
		t.Fatalf("CRLF input yields %q, want %q", got, "line1\nline2")
	}

	// 结尾反引号所在行同样可以是 CRLF。
	if got := rawStringValue(t, "`\r\na\r\n  `\r\n"); got != "a" {
		t.Fatalf("got %q, want %q", got, "a")
	}
	// 开头只跟 LF 时 CR 不复存在，内容里的孤立 CR 也被丢弃。
	if got := rawStringValue(t, "`\na\rb\n`\n"); got != "ab" {
		t.Fatalf("got %q, want %q", got, "ab")
	}
}

// TestRawStringClosingLineRules 结尾 ` 必须单独成行；同一行后面还有内容则不
// 算结尾，继续作为内容。
func TestRawStringClosingLineRules(t *testing.T) {
	// 结尾行前有缩进仍算结尾。
	if got := rawStringValue(t, "`\na\n\t`\n"); got != "a" {
		t.Fatalf("got %q, want %q", got, "a")
	}
	// 结尾行「仅空白 + `」即算结尾：` 右侧是否还有内容不做检查（沿用既有
	// 行为，本次只修 CRLF/续行丢字符，不改结尾行判定口径）。行内 ` 之后的
	// 字符照常由普通扫描切分。
	if got := rawStringValue(t, "`\na\n`  x\n"); got != "a" {
		t.Fatalf("got %q, want %q", got, "a")
	}
	l2 := New("`\na\n`  x\n")
	if l2.allTokens[0].Type != STRING || l2.allTokens[0].Literal != "a" {
		t.Fatalf("first token = %#v, want STRING %q", l2.allTokens[0], "a")
	}
	found := false
	for _, tok := range l2.allTokens[1:] {
		if tok.Type == IDENT && tok.Literal == "x" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected IDENT x after the closing backtick, tokens: %v", l2.allTokens)
	}
	// 结尾 ` 之后紧随的源码继续被正常切分。
	l := New("`\na\n`\nx = 1\n")
	var types []TokenType
	for _, tok := range l.allTokens {
		types = append(types, tok.Type)
	}
	want := []TokenType{STRING, NEWLINE, IDENT, ASSIGN, INT, NEWLINE, EOF}
	if len(types) != len(want) {
		t.Fatalf("token types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("token types = %v, want %v", types, want)
		}
	}
}

// TestRawStringOpeningNeedsNewline 开头 ` 后必须是换行：注解式 `text` 不是
// 原始串（退化为空串，后续字符由普通扫描处理）。
func TestRawStringOpeningNeedsNewline(t *testing.T) {
	if got := rawStringValue(t, "`inline`\n"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
