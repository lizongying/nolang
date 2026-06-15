package fmt

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestDebugParens(t *testing.T) {
	cases := []string{
		"off + 512 + blocks * 512",
		"c >= 48 && c <= 57",
		"off + 124 + i",
		"off + 512 <= n",
		"48 || c == 0",
		"(sz + 511) / 512",
	}
	for _, c := range cases {
		fmt.Printf("\n=== %q ===\n", c)
		l := lexer.New(c)
		p := parser.New(l)
		prog := p.ParseProgram()
		if prog != nil && len(prog.Statements) > 0 {
			if es, ok := prog.Statements[0].(*parser.ExpressionStatement); ok {
				printExpr(es.Expression, "")
			}
		}
	}
}

func printExpr(e parser.Expression, indent string) {
	switch x := e.(type) {
	case *parser.InfixExpression:
		fmt.Printf("%sInfixExpr{op: %q}\n", indent, x.Operator)
		fmt.Printf("%s  Left:\n", indent)
		printExpr(x.Left, indent+"    ")
		fmt.Printf("%s  Right:\n", indent)
		printExpr(x.Right, indent+"    ")
	case *parser.Identifier:
		fmt.Printf("%sIdent{%s}\n", indent, x.Value)
	case *parser.IntegerLiteral:
		fmt.Printf("%sInt{%s}\n", indent, x.Token.Literal)
	default:
		fmt.Printf("%s%T\n", indent, x)
	}
}
