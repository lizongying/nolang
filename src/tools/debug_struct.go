//go:build ignore

package main

import (
	"fmt"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func dumpExpr(e parser.Expression, indent string) {
	if e == nil {
		fmt.Printf("%s<nil>\n", indent)
		return
	}
	switch v := e.(type) {
	case *parser.Identifier:
		fmt.Printf("%sIdentifier(%s)\n", indent, v.Value)
	case *parser.StringLiteral:
		fmt.Printf("%sString(%q)\n", indent, v.Value)
	case *parser.IntegerLiteral:
		fmt.Printf("%sInt(%d)\n", indent, v.Value)
	case *parser.PrefixExpression:
		fmt.Printf("%sPrefix(%s)\n", indent, v.Operator)
		dumpExpr(v.Right, indent+"  ")
	case *parser.InfixExpression:
		fmt.Printf("%sInfix(%s)\n", indent, v.Operator)
		dumpExpr(v.Left, indent+"  ")
		dumpExpr(v.Right, indent+"  ")
	case *parser.StructLiteral:
		fmt.Printf("%sStructLit(%s)\n", indent, v.Type)
		for _, f := range v.Fields {
			fmt.Printf("%s  field %s:\n", indent, f.Name)
			dumpExpr(f.Value, indent+"    ")
		}
	default:
		fmt.Printf("%s%T\n", indent, e)
	}
}

func dumpStmt(s parser.Statement, indent string) {
	switch v := s.(type) {
	case *parser.LetStatement:
		fmt.Printf("%sLet(%s =\n", indent, v.Name.Value)
		dumpExpr(v.Value, indent+"  ")
		fmt.Printf("%s)\n", indent)
	case *parser.ExpressionStatement:
		fmt.Printf("%sExprStmt:\n", indent)
		dumpExpr(v.Expression, indent+"  ")
	case *parser.FunctionDefinition:
		fmt.Printf("%sFunc(%s)\n", indent, v.Name)
		if v.Body != nil {
			for _, st := range v.Body.Statements {
				dumpStmt(st, indent+"  ")
			}
		}
	default:
		fmt.Printf("%s%T\n", indent, s)
	}
}

func main() {
	src := `main = () {
    fp = file { fd: -1 }
    print('ok')
}
main()
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	for _, stmt := range prog.Statements {
		dumpStmt(stmt, "")
	}
}
