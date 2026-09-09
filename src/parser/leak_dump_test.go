package parser

import (
	"fmt"
	"os"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func exprKind(e Expression) string {
	if e == nil {
		return "<nil>"
	}
	switch v := e.(type) {
	case *InfixExpression:
		return fmt.Sprintf("Infix(%s)", v.Operator)
	case *CallExpression:
		return "Call"
	case *IfExpression:
		return "If"
	case *Identifier:
		return "Ident(" + v.Value + ")"
	case *PrefixExpression:
		return "Prefix(" + v.Operator + ")"
	default:
		return fmt.Sprintf("%T", e)
	}
}

func dumpStmt(i int, st Statement, indent string) {
	switch v := st.(type) {
	case *FunctionDefinition:
		fmt.Printf("%s[%d] FUNC %q bodyLen=%d\n", indent, i, v.Name, func() int {
			if v.Body != nil {
				return len(v.Body.Statements)
			}
			return -1
		}())
		if v.Body != nil {
			for j, bs := range v.Body.Statements {
				dumpStmt(j, bs, indent+"    ")
			}
		}
	case *LetStatement:
		nm := ""
		if v.Name != nil {
			nm = v.Name.Value
		}
		fmt.Printf("%s[%d] LET name=%q value=%s\n", indent, i, nm, exprKind(v.Value))
	case *ExpressionStatement:
		fmt.Printf("%s[%d] EXPR %s\n", indent, i, exprKind(v.Expression))
	default:
		fmt.Printf("%s[%d] %T\n", indent, i, st)
	}
}

func TestDumpTxtFromHex(t *testing.T) {
	data, err := os.ReadFile("../../src/std/txt.no")
	if err != nil {
		t.Skipf("file not found: %v", err)
		return
	}
	l := lexer.New(string(data))
	p := New(l)
	p.Filename = "txt.no"
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			t.Logf("parse error: %s", e)
		}
	}
	fmt.Println("==== TOP LEVEL ====")
	for i, st := range prog.Statements {
		dumpStmt(i, st, "")
	}
}
