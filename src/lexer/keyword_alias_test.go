package lexer

import "testing"

// 关键字别名：ctn⇒CONTINUE、brk⇒BREAK、rtn⇒RETURN（2026-11 新增）。
// lexer 必须将它们识别为对应关键字 token，且 Literal 保留源码拼写
// （`no fmt` 依赖 Literal 维持别名原拼写，见 src/fmt/stmt.go）。
func TestKeywordAliasLookup(t *testing.T) {
	cases := []struct {
		word string
		want TokenType
	}{
		{"ctn", CONTINUE},
		{"brk", BREAK},
		{"rtn", RETURN},
		// 长形式与旧行为不变
		{"continue", CONTINUE},
		{"break", BREAK},
		{"return", RETURN},
		// 非关键字（含前缀重合的普通标识符）仍走 IDENT
		{"ctn-x", ILLEGAL}, // lookupKeyword 未命中返回 ILLEGAL，调用端视为 IDENT
		{"rtn2", ILLEGAL},
		{"broken", ILLEGAL},
	}
	for _, tc := range cases {
		if got := lookupKeyword(tc.word); got != tc.want {
			t.Errorf("lookupKeyword(%q) = %s, want %s", tc.word, got, tc.want)
		}
	}
}

func TestKeywordAliasLexing(t *testing.T) {
	lex := New("ctn\nbrk\nrtn\n")
	want := []struct {
		typ TokenType
		lit string
	}{
		{CONTINUE, "ctn"},
		{NEWLINE, "\n"},
		{BREAK, "brk"},
		{NEWLINE, "\n"},
		{RETURN, "rtn"},
	}
	for i, w := range want {
		tok := lex.NextToken()
		if tok.Type != w.typ {
			t.Fatalf("token %d: type = %s, want %s", i, tok.Type, w.typ)
		}
		if tok.Literal != w.lit {
			t.Errorf("token %d: literal = %q, want %q (fmt relies on alias spelling)", i, tok.Literal, w.lit)
		}
	}
}
