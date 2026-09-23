package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// option_capture_test.go — 「Option 捕獲賦值」lowering pass 的回歸測試。
//
// 語意（見 .trae/documents/option-capture-assign.md §1）：
//
//	a = b + c / d     →  a 推斷為 ?T，nil/err 就地存進 a，流程繼續
//
// 取代了舊的「option 回傳函式內裸 `x = v[i]` 自動改寫為 `x ?= v[i]`」上拋語意
// （maybeAutoPropagateIndex 已移除）。純算術（+ - * 取負 shl）本身不是可錯源，
// 否則所有算術都會變成 option；但在捕獲路徑（結果已是 option）觸發運行期溢出檢查。

// parseFnBody 解析 src 並回傳名為 fnName 的函式（方法取 `recv.name` 的尾段）
// 之函式體陳述。解析錯誤直接致命。
func parseFnBody(t *testing.T, src, fnName string) []Statement {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	for _, s := range prog.Statements {
		fd, ok := s.(*FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		if fd.Name == fnName || strings.HasSuffix(fd.Name, "."+fnName) {
			return fd.Body.Statements
		}
	}
	t.Fatalf("function %q not found in %q", fnName, src)
	return nil
}

// flattenStmts 深度優先展開陳述樹（BlockStatement、ExpressionStatement 內嵌的
// IfExpression 臂體），供斷言生成的 `__cap_N` 綁定與最終賦值確實存在。
func flattenStmts(stmts []Statement, out *[]Statement) {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		*out = append(*out, s)
		switch v := s.(type) {
		case *BlockStatement:
			flattenStmts(v.Statements, out)
		case *ExpressionStatement:
			flattenIfExpr(v.Expression, out)
		}
	}
}

func flattenIfExpr(e Expression, out *[]Statement) {
	ife, ok := e.(*IfExpression)
	if !ok || ife == nil {
		return
	}
	if ife.Consequence != nil {
		flattenStmts(ife.Consequence.Statements, out)
	}
	if ife.Alternative != nil {
		flattenStmts(ife.Alternative.Statements, out)
	}
}

// findLet 在展開後的陳述中尋找名為 name 的 LetStatement。
func findLet(stmts []Statement, name string) *LetStatement {
	var all []Statement
	flattenStmts(stmts, &all)
	for _, s := range all {
		if ls, ok := s.(*LetStatement); ok && ls.Name != nil && ls.Name.Value == name {
			return ls
		}
	}
	return nil
}

// capTmpNames 回傳展開後所有 `__cap_N` 綁定的變數名（依出現順序）。
func capTmpNames(stmts []Statement) []string {
	var all []Statement
	flattenStmts(stmts, &all)
	var names []string
	for _, s := range all {
		if ls, ok := s.(*LetStatement); ok && ls.Name != nil && strings.HasPrefix(ls.Name.Value, "__cap_") {
			names = append(names, ls.Name.Value)
		}
	}
	return names
}

// TestCaptureInnerDivMod 驗證 `a = b + c / d` 這種「內部可錯子表達式」被就地
// 捕獲：生成 `__cap_1 ?i64 = c / d` + 三臂 match + ok 臂 `a = b + __cap_1`。
func TestCaptureInnerDivMod(t *testing.T) {
	const src = `f = (b i64, c i64, d i64) {
    a = b + c / d
    print(a)
}`
	body := parseFnBody(t, src, "f")

	if got := capTmpNames(body); len(got) != 1 || got[0] != "__cap_1" {
		t.Fatalf("want exactly one __cap_1 binding, got %v", got)
	}
	capBind := findLet(body, "__cap_1")
	if capBind == nil {
		t.Fatal("missing __cap_1 binding")
	}
	if capBind.Type == nil || typeString(capBind.Type) != "?i64" {
		t.Fatalf("__cap_1 type = %v, want ?i64", capBind.Type)
	}
	if !capBind.IsSynthetic {
		t.Error("__cap_1 binding must be IsSynthetic (guard against re-lowering)")
	}
	inf, ok := capBind.Value.(*InfixExpression)
	if !ok || inf.Operator != "/" {
		t.Fatalf("__cap_1 value = %T, want InfixExpression `/`", capBind.Value)
	}
	// 最終賦值：ok 臂內的 `a = b + __cap_1`，且 a 被推斷為 ?i64。
	final := findLet(body, "a")
	if final == nil {
		t.Fatal("missing final assignment to a")
	}
	if final.Type == nil || typeString(final.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (capture)", final.Type)
	}
	if !final.IsSynthetic {
		t.Error("final assignment must be IsSynthetic")
	}
}

// TestCaptureRootSafeIndex 驗證根為安全索引的 `a = v[i]`：只合成 `?elem` 註解
// （無結構 desugar），越界即 nil 即捕獲。
//
// 安全索引的捕獲以「所在函式有 option 結果參數」為閘門（鏡像被取代的
// maybeAutoPropagateIndex 的既有範圍，見 lowering.enclosingFuncHasOptResult）。
func TestCaptureRootSafeIndex(t *testing.T) {
	const src = `f = (v []i64, i i64) (r ?i64) {
    a = v[i]
    print(a)
}`
	body := parseFnBody(t, src, "f")
	a := findLet(body, "a")
	if a == nil {
		t.Fatal("missing a")
	}
	if a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (capture of safe index)", a.Type)
	}
	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("root-index capture must not desugar into __cap_N, got %v", got)
	}
	if len(body) != 2 {
		t.Fatalf("root-index capture must not add statements, body len = %d", len(body))
	}
}

// TestCaptureRootSafeIndexGatedOutOfOptionFunc 釘住閘門：非 option 結果的函式
// （以及頂層指令稿）內，裸 `a = v[i]` 維持舊行為（純量元素型別，不改寫成
// option）—— 否則之後以普通值使用 a 的地方（呼叫引數、結構體欄位）會踩到
// option→純量尚未支援的 codegen 路徑。
func TestCaptureRootSafeIndexGatedOutOfOptionFunc(t *testing.T) {
	const src = `f = (v []i64, i i64) {
    a = v[i]
    print(a)
}`
	body := parseFnBody(t, src, "f")
	a := findLet(body, "a")
	if a == nil {
		t.Fatal("missing a")
	}
	if a.Type != nil && strings.HasPrefix(typeString(a.Type), "?") {
		t.Fatalf("a type = %v, want plain element type (capture gated off)", typeString(a.Type))
	}
	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("no capture expected outside option-result functions, got %v", got)
	}
}

// TestCaptureOptionOperand 驗證 option 型運算元被當成可錯源捕獲（`a = x + 1`，
// x: ?i64）。純算術 `+` 本身不是可錯源，觸發源是 x。
func TestCaptureOptionOperand(t *testing.T) {
	const src = `f = (x ?i64) {
    a = x + 1
    print(a)
}`
	body := parseFnBody(t, src, "f")
	a := findLet(body, "a")
	if a == nil {
		t.Fatal("missing a")
	}
	if a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (capture of option operand)", a.Type)
	}
	if got := capTmpNames(body); len(got) != 1 {
		t.Fatalf("want 1 __cap_N binding for the option operand, got %v", got)
	}
}

// TestCaptureNestedDivMod 驗證巢狀雙除法只取最外層節點：`a = x + (b / c) / d`
// 只合成一個 `__cap_N`（節點是整個 `(b / c) / d`），不遞歸展開內層除法
// （方案 §8：與 `?=` 現狀對齊，不擴大捕獲範圍）。
func TestCaptureNestedDivMod(t *testing.T) {
	const src = `f = (x i64, b i64, c i64, d i64) {
    a = x + (b / c) / d
    print(a)
}`
	body := parseFnBody(t, src, "f")
	if got := capTmpNames(body); len(got) != 1 {
		t.Fatalf("nested divmod must capture only the outermost node, got %v", got)
	}
	capBind := findLet(body, "__cap_1")
	if capBind == nil {
		t.Fatal("missing __cap_1 binding")
	}
	inf, ok := capBind.Value.(*InfixExpression)
	if !ok || inf.Operator != "/" {
		t.Fatalf("__cap_1 value = %T, want the whole `(b / c) / d` InfixExpression `/`", capBind.Value)
	}
	a := findLet(body, "a")
	if a == nil || a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64", a)
	}
}

// TestCaptureRootDivModLeftToInfer 釘住分工：根即 `/` `%` 時由
// inferOptionDivMod 就地合成 `?T` 註解（無結構 desugar），捕獲 pass 不介入
// —— 兩者對「根除法」不得同時改寫，否則同一節點被重複展開。
func TestCaptureRootDivModLeftToInfer(t *testing.T) {
	const src = `f = (b i64, d i64) {
    a = b / d
    print(a)
}`
	body := parseFnBody(t, src, "f")
	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("root divmod must be left to inferOptionDivMod, got %v", got)
	}
	a := findLet(body, "a")
	if a == nil || a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (inferOptionDivMod)", a)
	}
	if len(body) != 2 {
		t.Fatalf("root divmod must not add statements, body len = %d", len(body))
	}
}

// TestCaptureCallArgDivMod 驗證作為呼叫引數的內部除法仍被捕獲（`a = g(b / c)`）：
// 捕獲點在引數內，結果型別 T 取 g 的回傳型別（方案 §1「T 推斷：根是呼叫 →
// 回傳型別去 ?」），ok 臂再以 `g(__cap_1)` 重新求值。
func TestCaptureCallArgDivMod(t *testing.T) {
	const src = `g = (v i64) (r i64) { r = v }
f = (b i64, c i64) {
    a = g(b / c)
    print(a)
}`
	body := parseFnBody(t, src, "f")
	if got := capTmpNames(body); len(got) != 1 {
		t.Fatalf("want 1 __cap_N binding for the div in the call argument, got %v", got)
	}
	capBind := findLet(body, "__cap_1")
	if capBind == nil || capBind.Type == nil || typeString(capBind.Type) != "?i64" {
		t.Fatalf("__cap_1 type = %v, want ?i64", capBind)
	}
	a := findLet(body, "a")
	if a == nil || a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (capture; T = g's return type)", a)
	}
}

// TestCaptureSkippedCases 驗證保守跳過的情形（維持今日行為，交 checker 或既有
// 路徑處理）。
func TestCaptureSkippedCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// wantType 是目標變數的型別字串；wantCap 是期望的 __cap_N 數量。
		varName  string
		wantType string
		wantCap  int
	}{
		{
			name: "overflow annotation on the statement",
			src: `f = (b i64, c i64, d i64) {
    #{overflow=wrap}
    a = b + c / d
    print(a)
}`,
			varName: "a", wantType: "", wantCap: 0,
		},
		{
			// 外層陳述帶行註解 → 整棵子樹（含臂體）跳過。AES key-expand 迴歸。
			// 用 option 結果函式，否則索引捕獲本就被閘門擋下，測不到 enclosingOvf。
			name: "enclosing overflow annotation covers arm bodies",
			src: `f = (v []i64, i i64) (r ?i64) {
    #{overflow=wrap}
    {
        i == 0 -> {
            w = (v[i] << 24) | (v[i + 1] << 16)
            print(w)
        }
    }
}`,
			varName: "w", wantType: "", wantCap: 0,
		},
		{
			name: "explicit plain annotation is user intent",
			src: `f = (b i64, c i64, d i64) {
    a i64 = b + c / d
    print(a)
}`,
			varName: "a", wantType: "i64", wantCap: 0,
		},
		{
			// 先宣告 plain、再賦值 → 無法把 option 存進 plain 變數，交 checker。
			name: "predeclared plain local reassignment",
			src: `f = (v []i64, i i64) {
    a i64 = 0
    a = v[i]
    print(a)
}`,
			varName: "a", wantType: "i64", wantCap: 0,
		},
		{
			name: "index-out annotation on the statement",
			src: `f = (v []i64, i i64) {
    #{index-out = 0}
    a = v[i]
    print(a)
}`,
			varName: "a", wantType: "", wantCap: 0,
		},
		{
			// 純算術、無可錯源 → 不是捕獲觸發源（否則所有算術都變 option）。
			name: "pure arithmetic is not a capture source",
			src: `f = (b i64, c i64) {
    a = b + c
    print(a)
}`,
			varName: "a", wantType: "", wantCap: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := parseFnBody(t, tc.src, "f")
			ls := findLet(body, tc.varName)
			if ls == nil {
				t.Fatalf("no LetStatement named %q", tc.varName)
			}
			gotType := ""
			if ls.Type != nil {
				gotType = typeString(ls.Type)
			}
			if gotType != tc.wantType {
				t.Errorf("%s type = %q, want %q", tc.varName, gotType, tc.wantType)
			}
			if got := capTmpNames(body); len(got) != tc.wantCap {
				t.Errorf("__cap_N count = %d (%v), want %d", len(got), got, tc.wantCap)
			}
		})
	}
}

// TestCaptureUnderscoreTarget 驗證 `_ = expr` 照常捕獲（目標名恰為 `_`，合成
// `?T` 亦有效 —— 運行期安全求值，除零不會 UB）；豁免 unused lint 與
// ovfhndld/idxhndld 是 checker 側的責任。
func TestCaptureUnderscoreTarget(t *testing.T) {
	const src = `f = (v []i64, i i64) (r ?i64) {
    _ = v[i]
    print('done')
}`
	body := parseFnBody(t, src, "f")
	us := findLet(body, "_")
	if us == nil {
		t.Fatal("`_ = v[i]` must parse as a LetStatement named `_`")
	}
	if us.Type == nil || typeString(us.Type) != "?i64" {
		t.Fatalf("`_` type = %v, want ?i64 (capture)", us.Type)
	}
}

// TestCaptureIdempotent 驗證生成的語句皆為 IsSynthetic，不會被 walk 二次改寫
// （重解析同一份已改寫的 AST 不應再長出新的 __cap_N）。
func TestCaptureIdempotent(t *testing.T) {
	const src = `f = (b i64, c i64, d i64) {
    a = b + c / d
    print(a)
}`
	body := parseFnBody(t, src, "f")
	var all []Statement
	flattenStmts(body, &all)
	for _, s := range all {
		ls, ok := s.(*LetStatement)
		if !ok || ls.Name == nil {
			continue
		}
		if strings.HasPrefix(ls.Name.Value, "__cap_") && !ls.IsSynthetic {
			t.Errorf("generated %s is not IsSynthetic", ls.Name.Value)
		}
	}
	if got := capTmpNames(body); len(got) != 1 {
		t.Fatalf("want exactly 1 __cap_N, got %v", got)
	}
}

// TestCaptureSkippedForFmt 保護 formatter 路徑：`no fmt` 必須看到未經改寫的表層
// AST，否則會把合成的 `?i64` 與展開的 match 印回原始碼。
func TestCaptureSkippedForFmt(t *testing.T) {
	const src = "f = (b i64, c i64, d i64) {\n    a = b + c / d\n}\n"
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil; errors=%v", p.Errors())
	}
	for _, s := range prog.Statements {
		fd, ok := s.(*FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, b := range fd.Body.Statements {
			ls, ok := b.(*LetStatement)
			if !ok || ls.Name == nil || ls.Name.Value != "a" {
				continue
			}
			if ls.Type != nil {
				t.Fatalf("fmt path synthesized type %q; formatter would leak it", typeString(ls.Type))
			}
			if len(fd.Body.Statements) != 1 {
				t.Fatalf("fmt path expanded the statement into %d statements", len(fd.Body.Statements))
			}
			return
		}
	}
	t.Fatal("no LetStatement named a")
}

// TestCaptureCrossFunctionTypeIsolation 釘住跨函式型別污染迴歸：
// `FuncVarType` 查不到時會回退到全域 VarTypes，於是前一個函式的同名參數
// （f 的 `c i64`）會被後一個函式（h 的區域 `c`）誤認為「已宣告為非 option」，
// 靜默跳過 h 的捕獲 —— c 保持純量、溢出檢查消失、match 走錯臂、執行期崩潰。
// 捕獲 pass 的「已宣告為非 option」判定必須用函式作用域（localDefinitelyNonOption）。
func TestCaptureCrossFunctionTypeIsolation(t *testing.T) {
	const src = `f = (b i64, c i64, d i64) (r i64) {
    r = b + c / d
}
h = (m i64) (r i64) {
    y ?i64 = m
    c = y + 1
    r = c
}`
	body := parseFnBody(t, src, "h")
	if body == nil {
		t.Fatal("function h not found")
	}
	c := findLet(body, "c")
	if c == nil {
		t.Fatal("missing c in h")
	}
	if c.Type == nil || !strings.HasPrefix(typeString(c.Type), "?") {
		t.Fatalf("h's c type = %v, want an option (capture of option operand)", c.Type)
	}
	if got := capTmpNames(body); len(got) != 1 {
		t.Fatalf("h must capture exactly one node, got %v", got)
	}
}

// TestCaptureAlreadyDeclaredLiteralTargetSkipped 釘住「已存在的變數不改型別」：
// 目標若已在本函式宣告過（即使只是 `a = 1` 這種由字面量推斷、語義表不記型別的
// 宣告），捕獲 pass 不碰它 —— 改寫會讓它先前所有普通值語境的使用突然拿到
// option，且 `a ?T = nil` 預宣告會先清掉它的既有值（RHS 在 ok 臂才求值，讀到 nil）。
func TestCaptureAlreadyDeclaredLiteralTargetSkipped(t *testing.T) {
	const src = `f = (c i64, d i64) {
    a = 1
    a = a + c / d
    print(a)
}`
	body := parseFnBody(t, src, "f")
	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("already-declared plain target must not be captured, got %v", got)
	}
	var all []Statement
	flattenStmts(body, &all)
	for _, s := range all {
		ls, ok := s.(*LetStatement)
		if !ok || ls.Name == nil || ls.Name.Value != "a" {
			continue
		}
		if _, isNilLit := ls.Value.(*NilLiteral); isNilLit {
			t.Fatal("must not emit `a ?T = nil` pre-declaration for an already-declared variable")
		}
	}
}

// TestCaptureUnderscoreStatementBoundary 釘住 parser 邊界修正：`_ = expr` 緊接在
// 另一條陳述之後時，skipToStatementEnd 必須停在 `_`（UNDERSCORE 是語句邊界），
// 否則開頭的 `_` 與 `=` 被吃掉、整條陳述靜默退化成裸表達式陳述。
func TestCaptureUnderscoreStatementBoundary(t *testing.T) {
	const src = `f = (v []i64, i i64) {
    z i64 = 0
    _ = v[i]
    _ = z / 2
    print('done')
}`
	body := parseFnBody(t, src, "f")
	if len(body) != 4 {
		t.Fatalf("body len = %d, want 4 (`_ =` must not be swallowed)", len(body))
	}
	for _, idx := range []int{1, 2} {
		ls, ok := body[idx].(*LetStatement)
		if !ok {
			t.Fatalf("body[%d] = %T, want *LetStatement", idx, body[idx])
		}
		if ls.Name == nil || ls.Name.Value != "_" {
			t.Fatalf("body[%d] name = %v, want `_`", idx, ls.Name)
		}
	}
}
