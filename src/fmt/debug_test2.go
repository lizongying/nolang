package fmt

import (
	"testing"
	"fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestDebugBug2(t *testing.T) {
	code := `foo = (a i64, b i64) {
    if a > 0 {
        if c > 0 {
            x = 1
        }
        if d > 0 {
            y = 1
        }
    }
}`
	cleanCode, _, _ := stripAndClassify(code)
	l := lexer.New(cleanCode)
	p := parser.New(l)
	program := p.ParseProgram()
	fmt.Printf("Errors: %v\n", p.Errors())
	fmt.Printf("Statements: %d\n", len(program.Statements))
	for i, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			fmt.Printf("[%d] FunctionDefinition name=%q body=%d stmts\n", i, s.Name, len(s.Body.Statements))
			for j, bodyStmt := range s.Body.Statements {
				if es, ok := bodyStmt.(*parser.ExpressionStatement); ok {
					if ie, ok := es.Expression.(*parser.IfExpression); ok {
						fmt.Printf("  [%d] IfExpression condition=%v consequence=%d stmts\n", j, ie.Condition, len(ie.Consequence.Statements))
						for k, consStmt := range ie.Consequence.Statements {
							if es2, ok := consStmt.(*parser.ExpressionStatement); ok {
								fmt.Printf("    [%d] %T\n", k, es2.Expression)
							} else {
								fmt.Printf("    [%d] %T\n", k, consStmt)
							}
						}
					}
				}
			}
		}
	}
}
