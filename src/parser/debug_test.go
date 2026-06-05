package parser

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestDebugParser(t *testing.T) {
	input := `
	// 隐式变量声明
	x = 10
	y = 'hello'
	z = 3.14

	// 函数定义
	add = func(a, b) {
		return a + b
	}
	`

	// 打印所有令牌
	fmt.Println("Tokens:")
	lex := lexer.New(input)
	for token := lex.NextToken(); token.Type != lexer.EOF; token = lex.NextToken() {
		fmt.Printf("Type: %s, Literal: %q, Line: %d, Column: %d\n", token.Type.String(), token.Literal, token.Line, token.Column)
	}

	// 重新初始化 lexer 和 parser
	lex = lexer.New(input)
	p := New(lex)

	// 解析程序
	program := p.ParseProgram()

	// 打印解析结果
	fmt.Println("\nParse Result:")
	for i, stmt := range program.Statements {
		fmt.Printf("Statement %d: %T\n", i, stmt)
	}

	// 打印错误
	fmt.Println("\nErrors:")
	for _, err := range p.Errors() {
		fmt.Printf("Error: %s\n", err)
	}
}
