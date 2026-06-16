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
