package main

import (
	"fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	input := `status {status1, status2, status3, status4}

test-match = () {
    s status
    s: {
        status1 -> log('status1')
        status2 -> log('status2')
        status3 -> log('status3')
        status4 -> log('status4')
        -> log('else')
    }
}
`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	for _, err := range p.Errors() {
		fmt.Println("PARSE ERROR:", err)
	}
	for _, warn := range p.Warnings() {
		fmt.Println("PARSE WARN:", warn)
	}

	for _, stmt := range program.Statements {
		fmt.Printf("== Statement: %T\n", stmt)
		switch s := stmt.(type) {
		case *parser.EnumDefinition:
			fmt.Printf("  EnumDefinition: name=%s\n", s.Name)
			for _, v := range s.Values {
				fmt.Printf("    Value: name=%s, index=%d\n", v.Name, v.Value)
			}
		case *parser.FunctionDefinition:
			fmt.Printf("  FunctionDefinition: name=%s\n", s.Name)
			if s.Body != nil {
				for _, b := range s.Body.Statements {
					fmt.Printf("    Body stmt: %T\n", b)
					if ls, ok := b.(*parser.LetStatement); ok {
						fmt.Printf("      Let: name=%s", ls.Name.Value)
						if ls.Type != nil {
							fmt.Printf(", type=%s", ls.Type.String())
						}
						fmt.Println()
					}
					if es, ok := b.(*parser.ExpressionStatement); ok {
						fmt.Printf("      Expr: %T\n", es.Expression)
						if ife, ok := es.Expression.(*parser.IfExpression); ok {
							fmt.Printf("        IfExpr: IsBareMatch=%v, MatchedExpr=%v\n",
								program.Sem.HasRTFlag(ife, parser.RTBareMatch), ife.MatchedExpr)
							dumpIfExpr(ife, "        ")
						}
					}
				}
			}
		}
	}
}

func dumpIfExpr(ife *parser.IfExpression, indent string) {
	fmt.Printf("%sCondition: %T\n", indent, ife.Condition)
	if inf, ok := ife.Condition.(*parser.InfixExpression); ok {
		fmt.Printf("%s  Infix: left=%T, op=%s, right=%T\n", indent, inf.Left, inf.Operator, inf.Right)
		if id, ok := inf.Right.(*parser.Identifier); ok {
			fmt.Printf("%s  Right ident: %s\n", indent, id.Value)
		}
	}
	if il, ok := ife.Condition.(*parser.IntegerLiteral); ok {
		fmt.Printf("%s  IntLit: %d\n", indent, il.Value)
	}
	if ife.Consequence != nil {
		fmt.Printf("%sConsequence: %d stmts\n", indent, len(ife.Consequence.Statements))
		for _, s := range ife.Consequence.Statements {
			fmt.Printf("%s  %T\n", indent, s)
		}
	}
	if ife.Alternative != nil {
		fmt.Printf("%sAlternative: %d stmts\n", indent, len(ife.Alternative.Statements))
		for _, s := range ife.Alternative.Statements {
			fmt.Printf("%s  %T\n", indent, s)
			if ife2, ok := s.(*parser.ExpressionStatement); ok {
				if innerIf, ok := ife2.Expression.(*parser.IfExpression); ok {
					dumpIfExpr(innerIf, indent+"  ")
				}
			}
		}
	}
}
