package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// promote_test.go — 「整數 → bigint」隱式提升在 AST 上的落點。
//
// 政策：賦值位置上，整數進一個「有唯一無損構造子」的結構體是安全且明確的，
// 所以 `x bigint = 5` / `x bigint = k` 不要求使用者手寫 `bigint.from-i64(...)`。
// 改寫發生在裝配工作副本上（newCompileAssembly），合成形態必須與用戶手寫
// `bigint.from-i64(5)` 解析出來的節點一致 —— 否則後面每一次前綴 / 方法解析
// 都會把它當成另一種東西。這組測試就是pin住那個形態與「什麼該改、什麼不該改」。

func parsePromoteSrc(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// promotionCallee 回報 expr 是否為 `<mod>.<prop>(...)` 形態的呼叫，是則回
// "mod.prop"，否則回 ""。
func promotionCallee(expr parser.Expression) string {
	ce, ok := expr.(*parser.CallExpression)
	if !ok {
		return ""
	}
	dot, ok := ce.Function.(*parser.DotExpression)
	if !ok {
		return ""
	}
	recv, ok := dot.Receiver.(*parser.Identifier)
	if !ok {
		return ""
	}
	return recv.Value + "." + dot.Property
}

// walkLets 走遍程式（含函式體與巢狀區塊），對每個 LetStatement 呼叫 fn。
func walkLets(stmts []parser.Statement, fn func(*parser.LetStatement)) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			fn(s)
		case *parser.FunctionDefinition:
			if s.Body != nil {
				walkLets(s.Body.Statements, fn)
			}
		case *parser.BlockStatement:
			walkLets(s.Statements, fn)
		case *parser.ForStatement:
			if s.Body != nil {
				walkLets(s.Body.Statements, fn)
			}
		case *parser.ExpressionStatement:
			walkLetsExpr(s.Expression, fn)
		}
	}
}

func walkLetsExpr(expr parser.Expression, fn func(*parser.LetStatement)) {
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			walkLets(e.Consequence.Statements, fn)
		}
		if e.Alternative != nil {
			walkLets(e.Alternative.Statements, fn)
		}
	case *parser.GroupedExpression:
		walkLetsExpr(e.Expression, fn)
	}
}

func letValueOf(t *testing.T, prog *parser.Program, name string) parser.Expression {
	t.Helper()
	var got parser.Expression
	found := false
	walkLets(prog.Statements, func(l *parser.LetStatement) {
		if l.Name != nil && l.Name.Value == name {
			got, found = l.Value, true
		}
	})
	if !found {
		t.Fatalf("no let statement named %q", name)
	}
	return got
}

// TestPromoteScalarAssignmentWrapsIntegers 是政策的主體：字面量、算術式、
// 已標注為整數的識別字，三種右值都要變成顯式構造子呼叫。
func TestPromoteScalarAssignmentWrapsIntegers(t *testing.T) {
	src := `f = () (out i64) {
    k i64 = 2
    a bigint = 5
    b bigint = 1 + 2
    c bigint = k
    out = 0
}
`
	prog := parsePromoteSrc(t, src)
	promoteScalarAssignments(prog)
	for _, name := range []string{"a", "b", "c"} {
		if got := promotionCallee(letValueOf(t, prog, name)); got != "bigint.from-i64" {
			t.Errorf("%s: expected promotion to bigint.from-i64, got %q", name, got)
		}
	}
	// k 是 i64 宣告，決不能被包成結構體。
	if got := promotionCallee(letValueOf(t, prog, "k")); got != "" {
		t.Errorf("k: expected no rewrite, got %q", got)
	}
}

// TestPromoteScalarAssignmentLeavesUnknownAndForeign 是「不改寫」的一半：
// 右值型別證明不了（呼叫、欄位存取）、右值已是結構體、目標是別的結構體、
// 目標帶 option 修飾 —— 四者都必须原樣留著，後端在這些形態上沒有轉換。
func TestPromoteScalarAssignmentLeavesUnknownAndForeign(t *testing.T) {
	src := `node {
    limbs []i64
}

mk = () (out i64) {
    out = 0
}

f = () (out i64) {
    n bigint = mk()
    m bigint = n
    p node = 5
    q ?bigint = 5
    r bigint = "5"
    out = 0
}
`
	prog := parsePromoteSrc(t, src)
	promoteScalarAssignments(prog)
	for _, name := range []string{"n", "m", "p", "q", "r"} {
		if got := promotionCallee(letValueOf(t, prog, name)); got != "" {
			t.Errorf("%s: expected no rewrite, got %q", name, got)
		}
	}
}

// TestPromoteScalarAssignmentTopLevel pin 頂層作用域：頂層 let，以及無標注的
// 重新賦值（parser 把 `x = k` 也當成 LetStatement，Type 為 nil）同樣要吃提升。
func TestPromoteScalarAssignmentTopLevel(t *testing.T) {
	src := `x bigint = 1
k i64 = 2
x = k
`
	prog := parsePromoteSrc(t, src)
	promoteScalarAssignments(prog)
	if got := promotionCallee(letValueOf(t, prog, "x")); got != "bigint.from-i64" {
		t.Errorf("top-level reassignment: expected bigint.from-i64, got %q", got)
	}
	// 只改一條：宣告那行的 1 在第二次走訪時已被替換，這裡確認沒有多出包裹層。
	if inner, ok := letValueOf(t, prog, "x").(*parser.CallExpression); ok {
		if _, isCall := inner.Arguments[0].(*parser.Identifier); !isCall {
			t.Errorf("expected the promotion to wrap the original rvalue identifier, got %T", inner.Arguments[0])
		}
	}
}

// TestPromoteScalarAssignmentSkipsLocalSameNameStruct 卡住「同名不同家」：
// 入口檔案自己宣告的 `bigint` 不是 std 的 bigint，規則的構造子與它無關，
// 包下去會解析到另一家的函式 —— 又是一次靜默誤編譯。
func TestPromoteScalarAssignmentSkipsLocalSameNameStruct(t *testing.T) {
	src := `bigint {
    sign i64
}

f = () (out i64) {
    x bigint = 5
    out = 0
}
`
	prog := parsePromoteSrc(t, src)
	promoteScalarAssignments(prog)
	if got := promotionCallee(letValueOf(t, prog, "x")); got != "" {
		t.Errorf("local struct named bigint must not be promoted, got %q", got)
	}
}

// TestPromoteScalarAssignmentIsIdempotent 保證跑兩次不會套兩層構造子：
// `bigint.from-i64(...)` 的右值現在是呼叫，證明不了整數 ⇒ 原樣留著。
func TestPromoteScalarAssignmentIsIdempotent(t *testing.T) {
	src := `f = () (out i64) {
    x bigint = 5
    out = 0
}
`
	prog := parsePromoteSrc(t, src)
	promoteScalarAssignments(prog)
	once := letValueOf(t, prog, "x")
	promoteScalarAssignments(prog)
	twice := letValueOf(t, prog, "x")
	if promotionCallee(twice) != "bigint.from-i64" {
		t.Fatalf("second run changed the shape: %q", promotionCallee(twice))
	}
	if ce, ok := twice.(*parser.CallExpression); ok {
		if promotionCallee(ce.Arguments[0]) != "" {
			t.Errorf("double-wrapped promotion: %v", promotionCallee(ce.Arguments[0]))
		}
	}
	if once == twice {
		// 同一指標是正確的（未改寫），只記錄下來供讀代码的人確認語義。
		t.Log("value pointer unchanged (no second wrap)")
	}
}
