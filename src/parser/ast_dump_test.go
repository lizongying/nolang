package parser

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestDumpTestPowSyntax(t *testing.T) {
	src := `test-pow() {
    r = 0
    pow(2, 10, r)
    println-i64(r)
}
test-pow()
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()

	fmt.Printf("=== Errors: %v ===\n", p.Errors())
	fmt.Printf("=== %d statements ===\n", len(prog.Statements))
	for i, stmt := range prog.Statements {
		fmt.Printf("[%d] %T\n", i, stmt)
		switch s := stmt.(type) {
		case *ExpressionStatement:
			fmt.Printf("    Expression: %T\n", s.Expression)
			if ce, ok := s.Expression.(*CallExpression); ok {
				fmt.Printf("    Call func: %T, %d args\n", ce.Function, len(ce.Arguments))
			}
		case *BlockStatement:
			fmt.Printf("    Block: %d statements\n", len(s.Statements))
			for j, bs := range s.Statements {
				fmt.Printf("    [%d] %T\n", j, bs)
			}
		case *FunctionDefinition:
			fmt.Printf("    Function: name=%s, %d params, body=%d stmts\n",
				s.Name, len(s.Parameters), len(s.Body.Statements))
		}
	}
}
