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
	a uin8 = 8

	// 函数定义
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

	tokens := []TokenType{
		NEWLINE,                // 0
		NEWLINE,                // 1
		IDENT, ASSIGN, INT, NEWLINE,          // 2-5
		IDENT, ASSIGN, STRING, NEWLINE,       // 6-9
		IDENT, ASSIGN, FLOAT, NEWLINE,        // 10-13
		IDENT, IDENT, ASSIGN, INT,            // 14-17
		NEWLINE, NEWLINE, NEWLINE,            // 18-20
		IDENT, ASSIGN, LPAREN,                // 21-23
		IDENT, COMMA, IDENT,                  // 24-26
		RPAREN, LBRACE, NEWLINE,              // 27-29
		IDENT, ADD, IDENT, NEWLINE,           // 30-33
		RBRACE,                                // 34
		NEWLINE, NEWLINE, NEWLINE,            // 35-37
		IDENT, ASSIGN, IDENT, LPAREN,         // 38-41
		INT, COMMA, INT, RPAREN,              // 42-45
		NEWLINE, NEWLINE, NEWLINE,             // 46-48
		IDENT, ASSIGN, NIL,                  // 49-51
		NEWLINE,                              // 52
		IDENT, QUESTION, ASSIGN, STRING,       // 53-56
		NEWLINE, NEWLINE, NEWLINE,             // 57-59
		IF, IDENT, GREATER, INT, LBRACE,       // 60-64
		NEWLINE, IDENT, NEWLINE,               // 65-67
		RBRACE, ELSE, LBRACE,                  // 68-70
		NEWLINE, INT, NEWLINE,                 // 71-73
		RBRACE,                                // 74
		NEWLINE,                               // 75
		EOF,                                   // 76
	}

	i := 0
	for token := lex.NextToken(); token.Type != EOF; token = lex.NextToken() {
		if i >= len(tokens) {
			t.Fatalf("expected %d tokens, got more", len(tokens))
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
