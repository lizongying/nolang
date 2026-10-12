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
//	a = g(b / c)     →  a 推斷為 ?T，nil/err 就地存進 a，流程繼續
//
// 取代了舊的「option 回傳函式內裸 `x = v[i]` 自動改寫為 `x ?= v[i]`」上拋語意
// （maybeAutoPropagateIndex 已移除）。
//
// D6（統一 option 規則）改變了分工：**根為算術（`+ - * / %`）時不再結構 desugar**，
// 由 inferOptionArith 就地合成 `?T`（巢狀可錯子運算式交由 codegen 逐層包裹）；
// 本 pass 只負責「根不是算術」的情形 —— 例如 `a = g(b / c)`（捕獲點在引數內）、
// `a = x + 1`（x 為 ?T，根雖是 `+` 但 inferOptionArith 因運算元非純量而推斷失敗）。
// 索引捕獲（根為安全索引）見 TestCaptureRootSafeIndex。

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

// TestCaptureRootArithLeftToInfer 釘住 D6 的分工：根為算術（`+ - * / %`）時
// 不再結構 desugar，由 inferOptionArith 就地合成 `?T`（巢狀可錯子運算式由
// codegen 逐層包裹）。`a = b + c / d` 必須得到 `a : ?i64`、零個 `__cap_N`、
// 且不新增陳述。
func TestCaptureRootArithLeftToInfer(t *testing.T) {
	const src = `f = (b i64, c i64, d i64) {
    a = b + c / d
    print(a)
}`
	body := parseFnBody(t, src, "f")

	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("root arithmetic must be left to inferOptionArith, got %v", got)
	}
	a := findLet(body, "a")
	if a == nil {
		t.Fatal("missing a")
	}
	if a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (inferOptionArith)", a.Type)
	}
	if len(body) != 2 {
		t.Fatalf("root arithmetic must not add statements, body len = %d", len(body))
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

// TestCaptureNestedArithLeftToInfer 釘住巢狀算術同樣交給 inferOptionArith：
// `a = x + (b / c) / d` 根為 `+`、右運算元為巢狀 `/`，兩者皆屬 D6，故不生成
// `__cap_N`，只合成 `a : ?i64`（巢狀包裹由 codegen 負責）。
func TestCaptureNestedArithLeftToInfer(t *testing.T) {
	const src = `f = (x i64, b i64, c i64, d i64) {
    a = x + (b / c) / d
    print(a)
}`
	body := parseFnBody(t, src, "f")
	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("nested arithmetic must be left to inferOptionArith, got %v", got)
	}
	a := findLet(body, "a")
	if a == nil || a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64", a)
	}
	if len(body) != 2 {
		t.Fatalf("nested arithmetic must not add statements, body len = %d", len(body))
	}
}

// TestCaptureRootDivModLeftToInfer 釘住分工：根即 `/` `%` 時由
// inferOptionArith 就地合成 `?T` 註解（無結構 desugar），捕獲 pass 不介入
// —— 兩者對「根除法」不得同時改寫，否則同一節點被重複展開。
func TestCaptureRootDivModLeftToInfer(t *testing.T) {
	const src = `f = (b i64, d i64) {
    a = b / d
    print(a)
}`
	body := parseFnBody(t, src, "f")
	if got := capTmpNames(body); len(got) != 0 {
		t.Fatalf("root divmod must be left to inferOptionArith, got %v", got)
	}
	a := findLet(body, "a")
	if a == nil || a.Type == nil || typeString(a.Type) != "?i64" {
		t.Fatalf("a type = %v, want ?i64 (inferOptionArith)", a)
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
			// D1：`#{overflow=wrap}` 不再讓結果退回 plain int（也不再讓子樹免於
			// 捕獲）。根為算術 → inferOptionArith 合成 ?i64，零個 __cap_N。
			name: "overflow annotation no longer unwraps",
			src: `f = (b i64, c i64, d i64) {
    #{overflow=wrap}
    a = b + c / d
    print(a)
}`,
			varName: "a", wantType: "?i64", wantCap: 0,
		},
		{
			// D1 + D7：外層 `#{overflow=wrap}` 不再讓臂體免於捕獲。臂體內的兩個
			// 安全索引 `v[i]` / `v[i+1]`（v: []i64，i 為參數）各自成為可錯源，
			// 被就地捕獲 → w 推斷為 ?i64、生成 2 個 __cap_N。
			//
			// 註：`<<` 與 `|` 不屬 D6 的 `+ - * / %`，故運算子本身不產生 option；
			// 觸發源是索引讀取。此案例原為 AES key-expand 的「靜默漏檢」迴歸，
			// 統一規則下改為顯式捕獲（遷移由 `no fmt --fix=unwrap` 處理）。
			name: "enclosing overflow annotation no longer covers arm bodies",
			src: `f = (v []i64, i i64) (r ?i64) {
    #{overflow=wrap}
    {
        i == 0 -> {
            w = (v[i] << 24) | (v[i + 1] << 16)
            print(w)
        }
    }
}`,
			varName: "w", wantType: "?i64", wantCap: 2,
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
			// D6：純算術本身就是可錯源 → 推斷為 ?T，但不走結構捕獲（根為算術）。
			name: "pure arithmetic becomes option without capture",
			src: `f = (b i64, c i64) {
    a = b + c
    print(a)
}`,
			varName: "a", wantType: "?i64", wantCap: 0,
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
// （重解析同一份已改寫的 AST 不應再長出新的 __cap_N）。用呼叫引數內的除法作為
// 仍走結構捕獲的形態（根為算術者已由 inferOptionArith 處理，見上）。
func TestCaptureIdempotent(t *testing.T) {
	const src = `g = (v i64) (r i64) { r = v }
f = (b i64, c i64) {
    a = g(b / c)
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
