package parser

// compound_assign_test.go — `+=` / `-=` / `*=` / `/=` / `%=` 廢棄 + 降糖的回歸測試。
//
// 規則（2026-10-13 用戶定案）：
//   - 這五個運算符提示廢棄（LSP warning）；
//   - 語義等價於 `a ?= a + 1`（統一 option 規則下右式是 `?T`，所以走 `?=` 的
//     「解包 + 失敗上拋」路徑），parser 直接降糖成 UnwrapAssignStatement；
//   - no fmt 渲染 surface AST，輸出的正是重寫後的寫法（見 fmt 側的回歸測試）。
//
// 位元複合賦算（`&=` / `|=` / `^=` / `<<=` / `>>=` / `&^=`）不產生 option，不在
// 這張表裡 —— 維持「parser 不支援」的現狀（parse error）。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseSurfaceWarn 與 bounds_test.go 的 parseSurface 相同（保留 surface AST），
// 但同時回傳 parser —— 複合賦算的廢棄提示是解析期診斷，只有 parser 看得到。
func parseSurfaceWarn(t *testing.T, src string) (*Parser, *Program) {
	t.Helper()
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	return p, prog
}

// caseOf 回傳函式體內第 idx 條陳述。
func caseOf(t *testing.T, stmts []Statement, idx int) Statement {
	t.Helper()
	if idx >= len(stmts) {
		t.Fatalf("statement %d missing (have %d)", idx, len(stmts))
	}
	return stmts[idx]
}

// TestCompoundAssignDesugarsToUnwrapAssign 逐個運算符驗證降糖結果：
// UnwrapAssignStatement + DeprecatedOp 記下原運算符 + Value 為 `a <op> <expr>`。
func TestCompoundAssignDesugarsToUnwrapAssign(t *testing.T) {
	cases := []struct {
		op     string // 源碼裡的複合賦算運算符
		infix  string // 降糖後的二元運算符
		expr   string // 右式字面量
		litTyp string
	}{
		{"+=", "+", "1", "IntegerLiteral"},
		{"-=", "-", "2", "IntegerLiteral"},
		{"*=", "*", "3", "IntegerLiteral"},
		{"/=", "/", "4", "IntegerLiteral"},
		{"%=", "%", "5", "IntegerLiteral"},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			// SmallIntLiteral 之類的包裝與否無所謂，用一般整數字面量即可。
			src := "foo = () (result ?i64) {\n    result = nil\n    a i64 = 10\n    a " + c.op + " " + c.expr + "\n}"
			_, prog := parseSurfaceWarn(t, src)
			fd, ok := prog.Statements[0].(*FunctionDefinition)
			if !ok {
				t.Fatalf("statement 0 is %T, want *FunctionDefinition", prog.Statements[0])
			}
			uas, ok := caseOf(t, fd.Body.Statements, 2).(*UnwrapAssignStatement)
			if !ok {
				t.Fatalf("`a %s %s` is %T, want *UnwrapAssignStatement", c.op, c.expr,
					caseOf(t, fd.Body.Statements, 2))
			}
			if uas.DeprecatedOp != c.op {
				t.Errorf("DeprecatedOp = %q, want %q", uas.DeprecatedOp, c.op)
			}
			if uas.Name == nil || uas.Name.Value != "a" {
				t.Errorf("Name = %+v, want identifier a", uas.Name)
			}
			inf, ok := uas.Value.(*InfixExpression)
			if !ok {
				t.Fatalf("Value is %T, want *InfixExpression (a %s %s)", uas.Value, c.op, c.expr)
			}
			if inf.Operator != c.infix {
				t.Errorf("Operator = %q, want %q", inf.Operator, c.infix)
			}
			if inf.Left == nil || inf.Right == nil {
				t.Fatalf("InfixExpression missing operands: %+v", inf)
			}
			if id, ok := inf.Left.(*Identifier); !ok || id.Value != "a" {
				t.Errorf("Left = %v, want identifier a", inf.Left)
			}
		})
	}
}

// TestCompoundAssignEmitsParserWarning 五個運算符都要在解析期記一條
// W_COMPOUND_ASSIGN 警告（CLI 管線 lowering 後只剩這條，見 build/transpiler.go）。
func TestCompoundAssignEmitsParserWarning(t *testing.T) {
	src := "foo = () (result ?i64) {\n    result = nil\n    a i64 = 10\n    a += 1\n    a -= 1\n    a *= 2\n    a /= 2\n    a %= 3\n}"
	p, _ := parseSurfaceWarn(t, src)
	warns := p.WarningsByCode(WarnCompoundAssign)
	if len(warns) != 5 {
		t.Fatalf("expected 5 %s warnings, got %d: %v", WarnCompoundAssign, len(warns), warns)
	}
	// 位置必須落在運算各行（3..7）的目標變數上，列在 LSP quickfix 才有意義。
	for i, w := range warns {
		if !strings.Contains(w, "line "+itoa(4+i)) {
			t.Errorf("warning %d lacks the expected line anchor: %q", i, w)
		}
		if !strings.Contains(w, "?=") {
			t.Errorf("warning %d does not suggest the `?=` rewrite: %q", i, w)
		}
	}
}

// TestGenuineUnwrapAssignNoWarning 對照組：手寫的 `a ?= a + 1` 不該有廢棄警告，
// 也不該被標 DeprecatedOp —— 證明上面那條抓的確實是「複合賦算」而不是 `?=`。
func TestGenuineUnwrapAssignNoWarning(t *testing.T) {
	src := "foo = () (result ?i64) {\n    result = nil\n    a i64 = 10\n    a ?= a + 1\n}"
	p, prog := parseSurfaceWarn(t, src)
	if n := len(p.WarningsByCode(WarnCompoundAssign)); n != 0 {
		t.Fatalf("genuine `?=` must not warn, got %d: %v", n, p.WarningsByCode(WarnCompoundAssign))
	}
	fd := prog.Statements[0].(*FunctionDefinition)
	uas, ok := fd.Body.Statements[2].(*UnwrapAssignStatement)
	if !ok {
		t.Fatalf("statement 2 is %T, want *UnwrapAssignStatement", fd.Body.Statements[2])
	}
	if uas.DeprecatedOp != "" {
		t.Errorf("DeprecatedOp = %q, want empty for a hand-written `?=`", uas.DeprecatedOp)
	}
}

// countUnwrapAssign 統計 program 裡的 UnwrapAssignStatement 節點數（含臂體）。
func countUnwrapAssign(stmts []Statement) int {
	n := 0
	var walk func(s Statement)
	walk = func(s Statement) {
		switch v := s.(type) {
		case nil:
			return
		case *UnwrapAssignStatement:
			n++
			walkStmtExpr(v.Value, walk)
		case *BlockStatement:
			for _, c := range v.Statements {
				walk(c)
			}
		case *ExpressionStatement:
			walkStmtExpr(v.Expression, walk)
		case *ForStatement:
			if v.Body != nil {
				for _, c := range v.Body.Statements {
					walk(c)
				}
			}
		case *FunctionDefinition:
			if v.Body != nil {
				for _, c := range v.Body.Statements {
					walk(c)
				}
			}
		}
	}
	for _, s := range stmts {
		walk(s)
	}
	return n
}

// walkStmtExpr 走進表達式裡的 if/match 臂體（降糖後的 `?=` 會以 BlockStatement
// 形態出現在 ExpressionStatement 內，這裡只需覆蓋本測試用到的形態）。
func walkStmtExpr(e Expression, walk func(Statement)) {
	ie, ok := e.(*IfExpression)
	if !ok {
		return
	}
	if ie.Consequence != nil {
		for _, c := range ie.Consequence.Statements {
			walk(c)
		}
	}
	if ie.Alternative != nil {
		for _, c := range ie.Alternative.Statements {
			walk(c)
		}
	}
}

// TestCompoundAssignIndexTargetNotDesugared 對照組：`a[i] += 1` 不給降糖後門
// （重複求值下標，且下標自己現在就是 option）—— 必須**不產生**
// UnwrapAssignStatement、也不許發 W_COMPOUND_ASSIGN 警告，維持原來的
// 「parse 後結構壞掉 → 編譯期報錯」現狀，而不是靜靜變成別的語義。
func TestCompoundAssignIndexTargetNotDesugared(t *testing.T) {
	for _, op := range []string{"+=", "-=", "*=", "/=", "%="} {
		t.Run(op, func(t *testing.T) {
			src := "foo = () (result ?i64) {\n    result = nil\n    a [3]i64 = [1, 2, 3]\n    a[0] " + op + " 1\n}"
			p, prog := parseSurfaceWarn(t, src)
			if n := len(p.WarningsByCode(WarnCompoundAssign)); n != 0 {
				t.Fatalf("`a[0] %s 1` warned (%d), want no warning — index targets are not desugared", op, n)
			}
			if n := countUnwrapAssign(prog.Statements); n != 0 {
				t.Fatalf("`a[0] %s 1` produced %d UnwrapAssignStatement, want 0", op, n)
			}
		})
	}
}

// TestBitwiseCompoundAssignNotDesugared 對照組：位元複合賦算不在廢棄名單內
// （它們不產生 option），同樣不得降糖、不得警告。
func TestBitwiseCompoundAssignNotDesugared(t *testing.T) {
	for _, op := range []string{"&=", "|=", "^=", "<<=", ">>=", "&^="} {
		t.Run(op, func(t *testing.T) {
			src := "foo = () (result ?i64) {\n    result = nil\n    a i64 = 10\n    a " + op + " 1\n}"
			p, prog := parseSurfaceWarn(t, src)
			if n := len(p.WarningsByCode(WarnCompoundAssign)); n != 0 {
				t.Fatalf("`a %s 1` warned (%d), want no warning — bitwise compound assigns are out of scope", op, n)
			}
			if n := countUnwrapAssign(prog.Statements); n != 0 {
				t.Fatalf("`a %s 1` produced %d UnwrapAssignStatement, want 0", op, n)
			}
		})
	}
}

// itoa 避免為了一個斷言多引一個 strconv（測試檔風格與 repo 一致）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
