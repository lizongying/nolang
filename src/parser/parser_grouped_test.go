package parser

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestParseGroupedExpr(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple negative int", "s = (-42).to-str()"},
		{"grouped infix", "s = (-42 - 1).to-str()"},
		{"grouped prefix infix", "s = (-9223372036854775807 - 1).to-str()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			program := p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Logf("parse errors: %v", p.Errors())
			}
			fmt.Printf("=== %s ===\n", tt.name)
			for i, stmt := range program.Statements {
				fmt.Printf("  stmt[%d]: %T\n", i, stmt)
				if letStmt, ok := stmt.(*LetStatement); ok {
					fmt.Printf("    value: %s\n", describeExpr(letStmt.Value))
				}
			}
		})
	}
}

func describeExpr(e Expression) string {
	switch v := e.(type) {
	case *AssignExpression:
		return fmt.Sprintf("Assign(value=%s)", describeExpr(v.Value))
	case *CallExpression:
		return fmt.Sprintf("Call(fn=%s, args=%d)", describeExpr(v.Function), len(v.Arguments))
	case *DotExpression:
		return fmt.Sprintf("Dot(recv=%T(%s), prop=%s)", v.Receiver, describeExpr(v.Receiver), v.Property)
	case *GroupedExpression:
		return fmt.Sprintf("Grouped(%s)", describeExpr(v.Expression))
	case *InfixExpression:
		return fmt.Sprintf("Infix(%s %s %s)", describeExpr(v.Left), v.Operator, describeExpr(v.Right))
	case *PrefixExpression:
		return fmt.Sprintf("Prefix(%s %s)", v.Operator, describeExpr(v.Right))
	case *IntegerLiteral:
		return fmt.Sprintf("Int(%d)", v.Value)
	case *Identifier:
		return fmt.Sprintf("Id(%s)", v.Value)
	default:
		return fmt.Sprintf("%T", e)
	}
}
