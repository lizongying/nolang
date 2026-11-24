package checker

// d1_arg_test.go — D1（option 不得隱式拆包）在實參位置的回歸守門。
//
// 2026-11-24 之前 resolveExprType 刻意不帶 funcTypes，任何「呼叫結果當實參」
// 都解析成 ""，3604icnf 直接 continue：`s.push(s.byte(1))` 在 vet 下 0 error、
// run 卻把 ?byte 的 tag 丟掉（輸出 abcb）。本組測試守住修復後的三條路徑：
// builtin/std 方法、用戶函式、以及 ?= 的顯式解包豁免。

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

func d1CallMethod(recv, method string, args ...parser.Expression) *parser.CallExpression {
	return &parser.CallExpression{
		Function:  &parser.DotExpression{Receiver: &parser.Identifier{Value: recv}, Property: method},
		Arguments: args,
	}
}

func d1CountTrace(results []ValidateResult, trace string) int {
	n := 0
	for _, r := range results {
		if r.TraceID == trace {
			n++
		}
	}
	return n
}

// TestD1BuiltinArgReported std/builtin 方法實參是裸 option 呼叫時必須報
// 3604icnf（str.push 期望 char，實參是 ?byte）。
func TestD1BuiltinArgReported(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("no injected StdSigKey: std tables unavailable (use make test)")
	}
	inner := d1CallMethod("s", "byte", &parser.IntegerLiteral{Value: 1})
	call := d1CallMethod("s", "push", inner)
	vt := map[string]string{"s": "str"}

	got := checkStdMethodCallArgs(call, call.Function.(*parser.DotExpression), vt, nil, nil, "", false)
	if n := d1CountTrace(got, "3604icnf"); n != 1 {
		t.Fatalf("expected exactly 1 3604icnf for s.push(s.byte(1)), got %d: %+v", n, got)
	}
}

// TestD1BuiltinArgUnwrappedAllowed 同一呼叫先绑定成 plain 變數後不得再報 ——
// 這是推薦的改寫方式，也是 std 遷移採用的模式。
func TestD1BuiltinArgUnwrappedAllowed(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("no injected StdSigKey")
	}
	call := d1CallMethod("s", "push", &parser.Identifier{Value: "cv"})
	vt := map[string]string{"s": "str", "cv": "byte"}

	got := checkStdMethodCallArgs(call, call.Function.(*parser.DotExpression), vt, nil, nil, "", false)
	if n := d1CountTrace(got, "3604icnf"); n != 0 {
		t.Fatalf("plain bound argument must not be reported, got %d: %+v", n, got)
	}
}

// TestD1UserFuncCallResultReported 用戶函式也必須認得「呼叫結果當實參」的
// option 型別（p(q())，q 回 ?i64、p 收 i64）。
func TestD1UserFuncCallResultReported(t *testing.T) {
	sigs := map[string]*funcSig{
		"p": {ParamTypes: []paramInfo{{Name: "v", Type: "i64"}}},
		"q": {ResultTypes: []paramInfo{{Type: "?i64"}}},
	}
	inner := &parser.CallExpression{Function: &parser.Identifier{Value: "q"}}
	call := &parser.CallExpression{
		Function:  &parser.Identifier{Value: "p"},
		Arguments: []parser.Expression{inner},
	}

	got := checkCallArgsInExpr(call, sigs, nil, nil, false, nil)
	if n := d1CountTrace(got, "3604icnf"); n != 1 {
		t.Fatalf("expected exactly 1 3604icnf for p(q()), got %d: %+v", n, got)
	}
	// ?= 的 RHS 由 underUnwrap 豁免：?= 本身就是顯式解包。
	got = checkCallArgsInExpr(call, sigs, nil, nil, true, nil)
	if n := d1CountTrace(got, "3604icnf"); n != 0 {
		t.Fatalf("?= RHS must be exempt, got %d: %+v", n, got)
	}
}

// TestD1IdentifierInOkArmNotReported ok 臂內的 subject 識別字在 surface AST
// 裡仍帶 option 型別；payload 與形參不同型時不得誤報（net.no 的 ?conn →
// net.conn 實例）。
func TestD1IdentifierInOkArmNotReported(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("no injected StdSigKey")
	}
	call := d1CallMethod("e", "init", &parser.Identifier{Value: "c"})
	vt := map[string]string{"e": "enc-conn", "c": "?conn"}

	got := checkStdMethodCallArgs(call, call.Function.(*parser.DotExpression), vt, nil, nil, "", false)
	if n := d1CountTrace(got, "3604icnf"); n != 0 {
		t.Fatalf("ok-arm subject must not be reported, got %d: %+v", n, got)
	}
}
