package parser

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestZZDumpTopLevelSliceMatch(t *testing.T) {
	src := "s []u32 = with-len(4)\ns[0]: {\n  ok -> it = 0x61626380\n  -> _\n}\nx i64 = 1633837952\nx == 1633837952 -> exit(42)\n-> exit(7)\n"
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = false
	p.SkipSafeIndexLowering = false
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("nil prog; errs=%v", p.Errors())
	}
	for i, s := range prog.Statements {
		fmt.Printf("TOP STMT[%d] = %T\n", i, s)
		zzDumpStmt(s, "  ", 0)
	}
}

func zzDumpStmt(s Statement, indent string, depth int) {
	if s == nil || depth > 6 {
		return
	}
	switch v := s.(type) {
	case *ExpressionStatement:
		fmt.Printf("%sExpressionStatement expr=%T\n", indent, v.Expression)
		zzDumpExpr(v.Expression, indent+"  ", depth+1)
	case *LetStatement:
		fmt.Printf("%sLetStatement name=%v value=%T\n", indent, v.Name, v.Value)
		zzDumpExpr(v.Value, indent+"  ", depth+1)
	case *ForStatement:
		fmt.Printf("%sForStatement\n", indent)
		zzDumpExpr(v.Condition, indent+"  cond=", depth+1)
		zzDumpStmt(v.Body, indent+"  body=", depth+1)
	case *BlockStatement:
		for _, c := range v.Statements {
			zzDumpStmt(c, indent, depth+1)
		}
	default:
		fmt.Printf("%s%T\n", indent, s)
	}
}

func zzDumpExpr(e Expression, indent string, depth int) {
	if e == nil || depth > 8 {
		return
	}
	switch v := e.(type) {
	case *IfExpression:
		fmt.Printf("%sIfExpression\n", indent)
		zzDumpExpr(v.Condition, indent+"  cond=", depth+1)
		if v.Consequence != nil {
			zzDumpStmt(v.Consequence, indent+"  then=", depth+1)
		}
		if v.Alternative != nil {
			zzDumpStmt(v.Alternative, indent+"  else=", depth+1)
		}
	case *SurfaceMatch:
		fmt.Printf("%sSurfaceMatch matched=%T arms=%d\n", indent, v.Matched, len(v.Arms))
		for j, a := range v.Arms {
			fmt.Printf("%s  arm[%d] cond=%T isWild=%v isDotVal=%v\n", indent, j, a.Condition, a.IsWildcard, a.IsDotVal)
			zzDumpStmt(a.Body, indent+"    body=", depth+1)
		}
	case *AssignExpression:
		fmt.Printf("%sAssignExpression IsSynthetic=%v left=%T value=%T\n", indent, v.IsSynthetic, v.Left, v.Value)
	case *IndexExpression:
		fmt.Printf("%sIndexExpression left=%T index=%T\n", indent, v.Left, v.Index)
	default:
		fmt.Printf("%s%T\n", indent, e)
	}
}
