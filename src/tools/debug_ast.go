//go:build ignore

package main

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	input := `INVSBOX = '\x52\x09\x6a\xd5\x30\x36\xa5\x38\xbf\x40\xa3\x9e\x81\xf3\xd7\xfb' +
           '\x7c\xe3\x39\x82\x9b\x2f\xff\x87\x34\x8e\x43\x44\xc4\xde\xe9\xcb' +
           '\x54\x7b\x94\x32\xa6\xc2\x23\x3d\xee\x4c\x95\x0b\x42\xfa\xc3\x4e'`

	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		for _, err := range p.Errors() {
			fmt.Println("Parse error:", err)
		}
	}

	fmt.Printf("Program has %d statements\n", len(program.Statements))
	for i, stmt := range program.Statements {
		fmt.Printf("Statement %d: %T\n", i, stmt)
		if ls, ok := stmt.(*parser.LetStatement); ok {
			fmt.Printf("  Name: %s\n", ls.Name)
			fmt.Printf("  Value: %T\n", ls.Value)
			dumpExpr(ls.Value, 2)
		}
	}
}

func dumpExpr(expr parser.Expression, indent int) {
	prefix := strings.Repeat("  ", indent)
	switch e := expr.(type) {
	case *parser.StringLiteral:
		fmt.Printf("%sStringLiteral{line: %d, val: %s}\n", prefix, e.Token.Line, e.Value[:20])
	case *parser.InfixExpression:
		fmt.Printf("%sInfixExpression{op: %s, line: %d}\n", prefix, e.Operator, e.Token.Line)
		dumpExpr(e.Left, indent+1)
		dumpExpr(e.Right, indent+1)
	default:
		fmt.Printf("%s%T\n", prefix, expr)
	}
}
