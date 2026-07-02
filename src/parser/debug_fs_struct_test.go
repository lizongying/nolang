package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestDebugFsStructNo(t *testing.T) {
	source := `main = () {
    tmp = '/tmp/nolang-fs-test.txt'
    payload = 'hello nolang file struct'

    f-w ?file
    f-w = open(tmp, file-opts{
        mode: o-wronly | o-creat
        perm: perm-600
        excl: true
        truncate: true
        append: false
    })
    f-w: {
        err -> print('fail: open-write err')
        nil -> print('fail: open-write nil')
        ok -> print('ok branch')
    }
}

main()
`
	l := lexer.New(source)
	p := New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		for _, e := range p.Errors() {
			t.Logf("Parse error: %v", e)
		}
	}
	t.Logf("Program has %d statements", len(program.Statements))
	for i, stmt := range program.Statements {
		t.Logf("[%d] %T", i, stmt)
		dumpStmtForTest(t, stmt, 1)
	}
}

func dumpStmtForTest(t *testing.T, stmt Statement, indent int) {
	prefix := strings.Repeat("  ", indent)
	switch s := stmt.(type) {
	case *FunctionDefinition:
		t.Logf("%sFunc: %s (params=%d, results=%d)", prefix, s.Name, len(s.Parameters), len(s.Results))
		if s.Body != nil {
			for _, st := range s.Body.Statements {
				dumpStmtForTest(t, st, indent+1)
			}
		}
	case *LetStatement:
		name := ""
		if s.Name != nil {
			name = s.Name.Value
		}
		t.Logf("%sLet: %s", prefix, name)
		dumpExprForTest(t, s.Value, indent+1)
	case *ExpressionStatement:
		dumpExprForTest(t, s.Expression, indent)
	default:
		t.Logf("%s%T", prefix, stmt)
	}
}

func dumpExprForTest(t *testing.T, expr Expression, indent int) {
	if expr == nil {
		return
	}
	prefix := strings.Repeat("  ", indent)
	switch e := expr.(type) {
	case *Identifier:
		t.Logf("%sIdent: %s", prefix, e.Value)
	case *IntegerLiteral:
		t.Logf("%sInt: %d", prefix, e.Value)
	case *StringLiteral:
		t.Logf("%sStr: %q", prefix, e.Value)
	case *InfixExpression:
		t.Logf("%sInfix: %s", prefix, e.Operator)
		dumpExprForTest(t, e.Left, indent+1)
		dumpExprForTest(t, e.Right, indent+1)
	case *CallExpression:
		if id, ok := e.Function.(*Identifier); ok {
			t.Logf("%sCall: %s (args=%d)", prefix, id.Value, len(e.Arguments))
		} else {
			t.Logf("%sCall: %T (args=%d)", prefix, e.Function, len(e.Arguments))
		}
		for _, a := range e.Arguments {
			dumpExprForTest(t, a, indent+1)
		}
	case *DotExpression:
		t.Logf("%sDot: %s", prefix, e.Property)
		dumpExprForTest(t, e.Receiver, indent+1)
	case *StructLiteral:
		t.Logf("%sStructLiteral: %s (fields=%d)", prefix, e.Type, len(e.Fields))
		for _, f := range e.Fields {
			t.Logf("%s  %s =", prefix, f.Name)
			dumpExprForTest(t, f.Value, indent+1)
		}
	case *IfExpression:
		t.Logf("%sIfExpression", prefix)
		dumpExprForTest(t, e.Condition, indent+1)
		if e.Consequence != nil {
			t.Logf("%sThen:", prefix)
			for _, st := range e.Consequence.Statements {
				dumpStmtForTest(t, st, indent+2)
			}
		}
	default:
		t.Logf("%s%T", prefix, expr)
		fmt.Printf("DEBUG: %T = %+v\n", expr, expr)
	}
}
