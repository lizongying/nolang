package lexer

import (
	"fmt"
	"testing"
)

func TestLexer(t *testing.T) {
	input := `
	// 隐式变量声明
	x = 10
	y = 'hello'
	z = 3.14

	// 函数定义
	add = (a, b) {
		a + b
	}

	// 函数调用
	result = add(5, 3)

	// 可空类型
	nullableValue = nil

	// 条件表达式
	if x > 5 {
		x
	} else {
		0
	}
	`

	lex := New(input)

	tokens := []TokenType{
		NEWLINE,                              // 0: opening \n after backtick
		COMMENT,                              // 1: // 隐式变量声明
		NEWLINE,                              // 2
		IDENT, ASSIGN, INT, NEWLINE,          // 2-5: x = 10
		IDENT, ASSIGN, STRING, NEWLINE,       // 6-9: y = 'hello'
		IDENT, ASSIGN, FLOAT, NEWLINE,        // 10-13: z = 3.14
		NEWLINE,                              // 14: blank line
		COMMENT,                              // 15: // 函数定义
		NEWLINE,                              // 16
		IDENT, ASSIGN, LPAREN,                // 17-19: add = (
		IDENT, COMMA, IDENT,                  // 20-22: a, b
		RPAREN, LBRACE, NEWLINE,              // 23-25: ) {
		IDENT, ADD, IDENT, NEWLINE,           // 26-29: a + b
		RBRACE,                               // 30: }
		NEWLINE,                              // 31: newline after }
		NEWLINE,                              // 32: blank line
		COMMENT,                              // 33: // 函数调用
		NEWLINE,                              // 34
		IDENT, ASSIGN, IDENT, LPAREN,         // 35-38: result = add(
		INT, COMMA, INT, RPAREN,              // 39-42: 5, 3)
		NEWLINE,                              // 43
		NEWLINE,                              // 44: blank line
		COMMENT,                              // 45: // 可空类型
		NEWLINE,                              // 46
		IDENT, ASSIGN, NIL,                   // 47-49: nullableValue = nil
		NEWLINE,                              // 50
		NEWLINE,                              // 51: blank line
		COMMENT,                              // 52: // 条件表达式
		NEWLINE,                              // 53
		IF, IDENT, GREATER, INT, LBRACE,       // 54-58: if x > 5 {
		NEWLINE, IDENT, NEWLINE,               // 59-61
		RBRACE, ELSE, LBRACE,                  // 62-64: } else {
		NEWLINE, INT, NEWLINE,                 // 65-67
		RBRACE,                                // 68: }
		NEWLINE,                               // 69
		EOF,                                   // 70
	}

	i := 0
	for token := lex.NextToken(); token.Type != EOF; token = lex.NextToken() {
		if i >= len(tokens) {
			t.Fatalf("token %d: expected %s, got more", i, tokens[len(tokens)-1].String())
		}

		expectedType := tokens[i]
		if token.Type != expectedType {
			t.Errorf("token %d: expected type %s, got %s", i, expectedType.String(), token.Type.String())
		}

		i++
	}

	if i != len(tokens)-1 { // 减去 EOF
		t.Fatalf("expected %d tokens, got %d", len(tokens)-1, i)
	}
}

func TestLexer1(t *testing.T) {
	input := `

	add = (a, b) {
		a + b
	}

	// 函数调用
	result = add(5, 3)

	// 可空类型
	nullableValue = nil
	nullableString? = 'test'

	// 条件表达式
	if x > 5 {
		x
	} else {
		0
	}
	`

	lex := New(input)

	i := 0
	for token := lex.NextToken(); token.Type != EOF; token = lex.NextToken() {
		fmt.Println(token)
		i++
	}
}

func TestLexerRegexLiteral(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  TokenType
		wantPat   string
		wantFlags string
	}{
		{
			name:      "simple pattern",
			input:     "/abc/",
			wantType:  REGEX,
			wantPat:   "abc",
			wantFlags: "",
		},
		{
			name:      "pattern with flags",
			input:     "/abc/gi",
			wantType:  REGEX,
			wantPat:   "abc",
			wantFlags: "gi",
		},
		{
			name:      "digit class",
			input:     "/\\d+/",
			wantType:  REGEX,
			wantPat:   "\\d+",
			wantFlags: "",
		},
		{
			name:      "escaped slash",
			input:     "/a\\/b/",
			wantType:  REGEX,
			wantPat:   "a\\/b",
			wantFlags: "",
		},
		{
			// Note: a truly empty pattern would be "//" which collides with
			// line comments (same as JavaScript). Use (?:) for an empty match.
			name:      "empty-matching pattern",
			input:     "/(?:)/",
			wantType:  REGEX,
			wantPat:   "(?:)",
			wantFlags: "",
		},
		{
			name:      "pattern with char class",
			input:     "/[a-z]+/",
			wantType:  REGEX,
			wantPat:   "[a-z]+",
			wantFlags: "",
		},
		{
			name:      "anchors and quantifiers",
			input:     "/^hello.*world$/",
			wantType:  REGEX,
			wantPat:   "^hello.*world$",
			wantFlags: "",
		},
		{
			name:      "global flag only",
			input:     "/test/g",
			wantType:  REGEX,
			wantPat:   "test",
			wantFlags: "g",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := New(tt.input)
			tok := lex.NextToken()
			if tok.Type != tt.wantType {
				t.Errorf("token type: expected %s, got %s", tt.wantType.String(), tok.Type.String())
			}
			if tok.Literal != tt.wantPat {
				t.Errorf("pattern: expected %q, got %q", tt.wantPat, tok.Literal)
			}
			if tok.Raw != tt.wantFlags {
				t.Errorf("flags: expected %q, got %q", tt.wantFlags, tok.Raw)
			}
			// Next token should be EOF
			eof := lex.NextToken()
			if eof.Type != EOF {
				t.Errorf("expected EOF after regex literal, got %s", eof.Type.String())
			}
		})
	}
}

func TestLexerRegexLiteralInExpression(t *testing.T) {
	// Ensure regex literal works in a realistic expression context: after
	// '=' the '/' starts a regex, not division.
	input := "re = /\\d+/"
	lex := New(input)

	tokens := []TokenType{IDENT, ASSIGN, REGEX, EOF}
	patterns := []string{"", "", "\\d+", ""}
	flags := []string{"", "", "", ""}

	i := 0
	for tok := lex.NextToken(); ; tok = lex.NextToken() {
		if i >= len(tokens) {
			t.Fatalf("unexpected extra token: %s", tok.Type.String())
		}
		if tok.Type != tokens[i] {
			t.Fatalf("token %d: expected %s, got %s (literal=%q)", i, tokens[i].String(), tok.Type.String(), tok.Literal)
		}
		if tokens[i] == REGEX {
			if tok.Literal != patterns[i] {
				t.Errorf("token %d pattern: expected %q, got %q", i, patterns[i], tok.Literal)
			}
			if tok.Raw != flags[i] {
				t.Errorf("token %d flags: expected %q, got %q", i, flags[i], tok.Raw)
			}
		}
		if tok.Type == EOF {
			break
		}
		i++
	}
}

func TestLexerRegexVsDivision(t *testing.T) {
	// After value-producing tokens, '/' must be division, not a regex.
	input := "a / b"
	lex := New(input)

	tokens := []TokenType{IDENT, QUO, IDENT, EOF}
	i := 0
	for tok := lex.NextToken(); ; tok = lex.NextToken() {
		if i >= len(tokens) {
			t.Fatalf("unexpected extra token: %s", tok.Type.String())
		}
		if tok.Type != tokens[i] {
			t.Fatalf("token %d: expected %s, got %s (literal=%q)", i, tokens[i].String(), tok.Type.String(), tok.Literal)
		}
		if tok.Type == EOF {
			break
		}
		i++
	}
}

func TestLexerRegexAfterPunctuation(t *testing.T) {
	// '(' and ',' expect an expression, so '/' starts a regex.
	input := "f(/abc/, /xy/g)"
	lex := New(input)

	tokens := []TokenType{IDENT, LPAREN, REGEX, COMMA, REGEX, RPAREN, EOF}
	patterns := []string{"", "", "abc", "", "xy", "", ""}
	flags := []string{"", "", "", "", "g", "", ""}

	i := 0
	for tok := lex.NextToken(); ; tok = lex.NextToken() {
		if i >= len(tokens) {
			t.Fatalf("unexpected extra token: %s", tok.Type.String())
		}
		if tok.Type != tokens[i] {
			t.Fatalf("token %d: expected %s, got %s (literal=%q)", i, tokens[i].String(), tok.Type.String(), tok.Literal)
		}
		if tokens[i] == REGEX {
			if tok.Literal != patterns[i] {
				t.Errorf("token %d pattern: expected %q, got %q", i, patterns[i], tok.Literal)
			}
			if tok.Raw != flags[i] {
				t.Errorf("token %d flags: expected %q, got %q", i, flags[i], tok.Raw)
			}
		}
		if tok.Type == EOF {
			break
		}
		i++
	}
}

func TestLexerRegexVsImportExportPath(t *testing.T) {
	// After '@' (export) and '#' (use) directives, a '/' is a path separator,
	// not a regex literal.
	tests := []struct {
		name   string
		input  string
		tokens []TokenType
	}{
		{name: "export with leading slash", input: "@ /src/utils.greet a", tokens: []TokenType{AT, QUO, IDENT, QUO, IDENT, DOT, IDENT, IDENT, EOF}},
		{name: "use with leading slash", input: "# /utils/math.add", tokens: []TokenType{USE, QUO, IDENT, QUO, IDENT, DOT, IDENT, EOF}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := New(tt.input)
			i := 0
			for tok := lex.NextToken(); ; tok = lex.NextToken() {
				if i >= len(tt.tokens) {
					t.Fatalf("%s: unexpected extra token: %s", tt.name, tok.Type.String())
				}
				if tok.Type != tt.tokens[i] {
					t.Fatalf("%s: token %d: expected %s, got %s (literal=%q)", tt.name, i, tt.tokens[i].String(), tok.Type.String(), tok.Literal)
				}
				// No REGEX tokens should appear in these inputs
				if tok.Type == REGEX {
					t.Errorf("%s: unexpected REGEX token at position %d (literal=%q)", tt.name, i, tok.Literal)
				}
				if tok.Type == EOF {
					break
				}
				i++
			}
		})
	}
}

func TestLexerBitwiseAssignOperators(t *testing.T) {
	// 驗證位運算複合賦值運算子能被正確詞法分析。
	// 包含：&= |= ^= <<= >>= &^= 以及 &^（非賦值形式）。
	tests := []struct {
		name   string
		input  string
		tokens []TokenType
	}{
		{name: "and assign", input: "x &= y", tokens: []TokenType{IDENT, AND_ASSIGN, IDENT, EOF}},
		{name: "or assign", input: "x |= y", tokens: []TokenType{IDENT, OR_ASSIGN, IDENT, EOF}},
		{name: "xor assign", input: "x ^= y", tokens: []TokenType{IDENT, XOR_ASSIGN, IDENT, EOF}},
		{name: "shl assign", input: "x <<= y", tokens: []TokenType{IDENT, SHL_ASSIGN, IDENT, EOF}},
		{name: "shr assign", input: "x >>= y", tokens: []TokenType{IDENT, SHR_ASSIGN, IDENT, EOF}},
		{name: "and-not", input: "x &^ y", tokens: []TokenType{IDENT, AND_NOT, IDENT, EOF}},
		{name: "and-not assign", input: "x &^= y", tokens: []TokenType{IDENT, AND_NOT_ASSIGN, IDENT, EOF}},
		{name: "shl still works", input: "x << y", tokens: []TokenType{IDENT, SHL, IDENT, EOF}},
		{name: "shr still works", input: "x >> y", tokens: []TokenType{IDENT, SHR, IDENT, EOF}},
		{name: "and still works", input: "x & y", tokens: []TokenType{IDENT, AND, IDENT, EOF}},
		{name: "or still works", input: "x | y", tokens: []TokenType{IDENT, OR, IDENT, EOF}},
		{name: "xor still works", input: "x ^ y", tokens: []TokenType{IDENT, XOR, IDENT, EOF}},
		{name: "less still works", input: "x < y", tokens: []TokenType{IDENT, LESS, IDENT, EOF}},
		{name: "greater still works", input: "x > y", tokens: []TokenType{IDENT, GREATER, IDENT, EOF}},
		{name: "less equals still works", input: "x <= y", tokens: []TokenType{IDENT, LESS_EQUALS, IDENT, EOF}},
		{name: "greater equals still works", input: "x >= y", tokens: []TokenType{IDENT, GREATER_EQUALS, IDENT, EOF}},
		{name: "land still works", input: "x && y", tokens: []TokenType{IDENT, LAND, IDENT, EOF}},
		{name: "lor still works", input: "x || y", tokens: []TokenType{IDENT, LOR, IDENT, EOF}},
		{name: "question assign", input: "v ?= expr", tokens: []TokenType{IDENT, QUESTION_ASSIGN, IDENT, EOF}},
		{name: "question still works", input: "x ? 1 : 2", tokens: []TokenType{IDENT, QUESTION, INT, COLON, INT, EOF}},
		{name: "question nil still works", input: "x = ?", tokens: []TokenType{IDENT, ASSIGN, QUESTION, EOF}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := New(tt.input)
			i := 0
			for tok := lex.NextToken(); ; tok = lex.NextToken() {
				if i >= len(tt.tokens) {
					t.Fatalf("%s: unexpected extra token: %s", tt.name, tok.Type.String())
				}
				if tok.Type != tt.tokens[i] {
					t.Fatalf("%s: token %d: expected %s, got %s (literal=%q)",
						tt.name, i, tt.tokens[i].String(), tok.Type.String(), tok.Literal)
				}
				if tok.Type == EOF {
					break
				}
				i++
			}
		})
	}
}
