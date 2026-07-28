package parser

import (
	"fmt"
	"os"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestDebugItBindings(t *testing.T) {
	src, err := os.ReadFile("../../src/std/net/sse.no")
	if err != nil {
		t.Skipf("cannot read sse.no: %v", err)
	}
	l := lexer.New(string(src))
	p := New(l)
	p.Filename = "sse.no"
	prog := p.ParseProgram()
	fmt.Printf("=== parser errors: %v\n", p.Errors())

	var dump func(node Node, depth int)
	dump = func(node Node, depth int) {
		indent := ""
		for i := 0; i < depth; i++ {
			indent += "  "
		}
		fmt.Printf("%s%T\n", indent, node)
		switch n := node.(type) {
		case *Program:
			for _, s := range n.Statements {
				dump(s, depth)
			}
		case *FunctionDefinition:
			fmt.Printf("%s  >>> FUNCTION %q\n", indent, n.Name)
			if n.Body != nil {
				fmt.Printf("%s  (body is *parser.BlockStatement)\n", indent)
				for _, s := range n.Body.Statements {
					dump(s, depth+1)
				}
			}
		case *BlockStatement:
			for _, s := range n.Statements {
				dump(s, depth+1)
			}
		case *LetStatement:
			if n.Value != nil {
				dump(n.Value, depth+1)
			}
		case *ExpressionStatement:
			if n.Expression != nil {
				dump(n.Expression, depth+1)
			}
		case *IfExpression:
			if n.Consequence != nil {
				fmt.Printf("%s  (consequence is *parser.BlockStatement)\n", indent)
				for _, s := range n.Consequence.Statements {
					dump(s, depth+1)
				}
			}
			if n.Alternative != nil {
				fmt.Printf("%s  (alternative is *parser.BlockStatement)\n", indent)
				for _, s := range n.Alternative.Statements {
					dump(s, depth+1)
				}
			}
		case *CallExpression:
			fmt.Printf("%s  (function is %T)\n", indent, n.Function)
			if n.Function != nil {
				dump(n.Function, depth+1)
			}
			for _, a := range n.Arguments {
				dump(a, depth+1)
			}
		}
	}
	dump(prog, 0)
}
