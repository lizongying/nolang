package checker

// option_compare.go — 禁止把 option（?T）值直接拿去比較。
//
// nolang 的 option 是 tagged enum：一個 ?i64 在記憶體裡是 {tag, data}
// （tag: 0=ok / 1=nil / 2=err），而不是一個數字。因此
//
//	size = fstat-size(.fd)   ; size 是 ?i64
//	size == 0                ; ❌ 拿 {tag,data} 結構跟 0 比，永遠不對
//
// 正確寫法是先解包：
//
//	size ?= fstat-size(.fd)  ; size 是 i64，失敗自動上拋給呼叫者
//	size == 0                ; ✅
//
// 或用 match 顯式分支：
//
//	size: { ok(v) -> ...  nil -> ...  err -> ... }
//
// 本規則只報「一側已知是 option、另一側已知不是 option」的比較；
// 以下情形一律放行（避免誤報）：
//   - 兩側都是 option（型別相同，語意是整體比較）；
//   - 另一側是 option 變體標記（ok / nil / err）——這是 match 的正規寫法；
//   - 任一側型別未知（跨模組回傳、struct 欄位、泛型等）。
//
// 另有一條獨立子規則（q4nilcmp）：`nil` 只能與 option（`?T`）比較。
// 非 option 的 `T == nil` / `T != nil` 一律硬錯誤——
//   - `str == nil` 恆為 false（拿 `%str-long` 結構體跟 0 比），靜默給錯答案；
//   - `[]i64 == nil` 會掉進整數比較路徑，產生 `icmp eq i64 %vec, i64` 這種
//     非法 LLVM（後端才炸，錯誤訊息完全看不出原因）。
// 正確寫法是宣告成 `?T` 再比，或與具體值比較。
//
// 兩條子規則都必須自帶 File：合併多模組的 `no vet` / `no build` 會丟棄
// File 為空的診斷（見 lint.go finalizeLintResults），沒有 File 就等於規則不存在。

import (
	"fmt"
	"github.com/lizongying/nolang/i18n"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

const optionCompareTraceID = "fe0a5wt2"

// optionNilCompareTraceID 是「非 option 值與 nil 比較」的診斷碼（q4nilcmp）。
const optionNilCompareTraceID = "q4nilcmp"

// optionVariantType / optionNilType 是「option 變體標記」的內部哨兵型別，
// 不帶 "?" 前綴，因此不會被當成普通值型別。
const (
	optionVariantType = "@option-variant" // ok / nil / err
	optionNilType     = "@nil"
)

// unknownOptionInner 用於「已知回傳 option 但內層型別未知」的情形
// （如 std 內建 stat-size / fstat-size / file-size）。保留 "?" 前綴
// 使其仍被判定為 option。
const unknownOptionInner = "?option"

// ValidateOptionComparison 報告所有「option 值與非 option 值直接比較」的寫法。
func ValidateOptionComparison(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	return runOptionCmpCore(program, collectOptionCmpFuncTypes(program))
}

// ValidateNilComparison 只回傳 nil 子規則（q4nilcmp）的結果。
//
// 這是**編譯硬錯誤**，供 build/transpiler_validate.go 在模組合併後接線：
// lint 軌道（runLintOptionComparison）只在 `no vet` / LSP 可見，`no build`
// 根本不跑 lint（見 build/transpiler.go 的 vetMode 分支），只接 lint 軌就等於
// 「vet 有訊息、build 照樣成功」。
func ValidateNilComparison(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	all := runOptionCmpCore(program, collectOptionCmpFuncTypes(program))
	out := make([]ValidateResult, 0, 1)
	for _, r := range all {
		if r.TraceID == optionNilCompareTraceID {
			out = append(out, r)
		}
	}
	return out
}

// runOptionCmpCore 是 ValidateOptionComparison 兩變體共享的 walker 跑法 +
// 去重尾巴（6C：THIR 變體只換 funcTypes 來源，走查不動）。
func runOptionCmpCore(program *parser.Program, funcTypes map[string]string) []ValidateResult {
	w := &optCmpWalker{
		program:   program,
		funcTypes: funcTypes,
		locals:    make(map[string]string),
		untyped:   make(map[string]bool),
		unwrapped: make(map[string]bool),
		declared:  make(map[string]bool),
	}
	for _, stmt := range program.Statements {
		// 每條頂層語句重置 File 歸因：合併多模組時 std 的語句多半沒有
		// SourceFile facts（單態化克隆後遺失），若沿用上一條主檔語句的
		// File，std 內的診斷會被張冠李戴成主檔行號（實測 std/str.no:1328
		// 被報成 r1.no:1328），既誤導又繞過 std 過濾。
		w.curFile = program.Sem.SourceFileOf(stmt)
		w.walkStmt(stmt, "")
	}
	return dedupOptionCmpResults(w.results)
}

// dedupOptionCmpResults 去掉完全重複的診斷。
// 同一個比較節點可能因 `(n <= 127)` 這類 GroupedExpression 被走訪兩次，
// 若不去重，編輯器裡會出現兩條一模一樣的錯誤。
func dedupOptionCmpResults(in []ValidateResult) []ValidateResult {
	seen := make(map[string]bool, len(in))
	out := make([]ValidateResult, 0, len(in))
	for _, r := range in {
		k := fmt.Sprintf("%d:%d:%s", r.Line, r.Column, r.Message)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

// collectOptionCmpFuncTypes 收集程式內所有函式（含 `name = (...) (...) {}` 形式
// 與 extern 宣告）的首個回傳型別，供 option 判定使用。
func collectOptionCmpFuncTypes(program *parser.Program) map[string]string {
	funcTypes := make(map[string]string)
	set := func(name string, results []*parser.Parameter) {
		if name == "" || len(results) == 0 || results[0] == nil || results[0].Type == nil {
			return
		}
		t := results[0].Type.String()
		if t == "" {
			return
		}
		if _, exists := funcTypes[name]; !exists {
			funcTypes[name] = t
		}
	}
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			set(s.Name, s.Results)
			if s.Body != nil {
				collectNestedFuncTypes(s.Body.Statements, set)
			}
		case *parser.ExternStatement:
			if s.Name != nil {
				set(s.Name.Value, s.Results)
			}
		case *parser.LetStatement:
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok && s.Name != nil {
				set(s.Name.Value, fl.Results)
			}
		}
	}
	return funcTypes
}

func collectNestedFuncTypes(stmts []parser.Statement, set func(string, []*parser.Parameter)) {
	for _, st := range stmts {
		ls, ok := st.(*parser.LetStatement)
		if !ok || ls.Name == nil {
			continue
		}
		if fl, ok := ls.Value.(*parser.FunctionLiteral); ok {
			set(ls.Name.Value, fl.Results)
			if fl.Body != nil {
				collectNestedFuncTypes(fl.Body.Statements, set)
			}
		}
	}
}

type optCmpWalker struct {
	program   *parser.Program
	funcTypes map[string]string
	results   []ValidateResult

	// curFile 是「當前最近一條語句」的來源檔（合併多模組時由
	// program.Sem.SourceFileOf 取得）。診斷必須帶上它，否則
	// finalizeLintResults 會在 singleFile=false 時把 File 為空的診斷整批丟掉
	// ——規則看起來接好了，實際上 `no vet` / `no build` 一條都不報。
	curFile string

	fn         string          // 當前函式作用域（供 sem.FuncVarTypes 查詢）
	curResults map[string]bool // 當前函式的結果參數名（?= 目標為結果參數時不解包）
	locals     map[string]string
	untyped    map[string]bool // 型別未知的局部變數（避免回退到全域表被同名符號污染）
	unwrapped  map[string]bool
	// declared 標記「型別來自顯式宣告」的名字（參數/結果參數/`x T = ...`）。
	// nil 子規則只信這個集合，見 nilCompareTypeOf。
	declared map[string]bool
	itStack  []string // match 臂內 `it` 的解包內層型別堆疊（最近一次在棧頂）
}

// itType 回傳當前 match 臂內 `it` 的解包內層型別；不在 match 臂內則回空。
func (w *optCmpWalker) itType() string {
	if n := len(w.itStack); n > 0 {
		return w.itStack[n-1]
	}
	return ""
}

// innerOf 把 `?T` 剝成內層型別 `T`；非 option 回空。
func innerOf(t string) string {
	if ty.IsOptionRaw(t) {
		return ty.StripOptionRaw(t)
	}
	return ""
}

func (w *optCmpWalker) key(name string) string { return w.fn + "\x00" + name }

func (w *optCmpWalker) report(e *parser.InfixExpression, optExpr, otherExpr parser.Expression, optType, otherType string) {
	results := []ValidateResult{{
		File:    w.curFile,
		Line:    e.Token.Line,
		Column:  e.Token.Column,
		Message: i18n.T("fe0a5wt2", "cannot compare option value '%s' (%s) with '%s' (%s): unwrap it first (`v ?= expr`) or match on ok/nil/err", exprName(optExpr), optType, exprName(otherExpr), otherType),
		TraceID: "fe0a5wt2",
	}}
	w.results = append(w.results, results...)
}

// reportNilCompare 報告「非 option 值與 nil 比較」。
func (w *optCmpWalker) reportNilCompare(e *parser.InfixExpression, other parser.Expression, otherType string) {
	w.results = append(w.results, ValidateResult{
		File:    w.curFile,
		Line:    e.Token.Line,
		Column:  e.Token.Column,
		Message: i18n.T("q4nilcmp", "cannot compare '%s' (%s) with nil: only an option (`?T`) can be tested against nil; declare it as `?%s` or compare against a concrete value", exprName(other), otherType, otherType),
		TraceID: optionNilCompareTraceID,
	})
}

// ── 語句走訪 ──────────────────────────────────────────────────

func (w *optCmpWalker) walkStmt(stmt parser.Statement, fn string) {
	if stmt == nil {
		return
	}
	prevFn := w.fn
	if fn != "" {
		w.fn = fn
	}
	defer func() { w.fn = prevFn }()

	// File 歸因：內層語句（BlockStatement 等）多半沒有自己的 facts，
	// 沿用外層語句的檔案；頂層語句由 runOptionCmpCore 直接重置。
	if w.program != nil {
		if f := w.program.Sem.SourceFileOf(stmt); f != "" {
			w.curFile = f
		}
	}

	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		w.enterFunc(s.Name, s.Results, s.Parameters)
		if s.Body != nil {
			for _, inner := range s.Body.Statements {
				w.walkStmt(inner, s.Name)
			}
		}
	case *parser.LetStatement:
		if fl, ok := s.Value.(*parser.FunctionLiteral); ok {
			w.enterFunc(s.Name.Value, fl.Results, fl.Parameters)
			if fl.Body != nil {
				for _, inner := range fl.Body.Statements {
					w.walkStmt(inner, s.Name.Value)
				}
			}
			return
		}
		if s.Value != nil {
			w.walkExpr(s.Value)
		}
		if s.Name != nil {
			w.recordLocal(s.Name.Value, s.Type, s.Value)
		}
	case *parser.UnwrapAssignStatement:
		if s.Value != nil {
			w.walkExpr(s.Value)
		}
		// `v ?= expr`：v 綁定的是**解包後**的內層值（失敗自動上拋），
		// 因此後續 `v == 0` 合法。唯當 v 是當前函式的 option 結果參數時，
		// lowering 會把整個 option 傳播給它，型別仍是 ?T。
		if s.Name != nil && !w.curResults[s.Name.Value] {
			w.unwrapped[w.key(s.Name.Value)] = true
		}
	case *parser.MultiAssignStatement:
		for _, t := range s.Targets {
			w.walkExpr(t)
		}
		w.walkExpr(s.Value)
	case *parser.ReturnStatement:
		w.walkExpr(s.ReturnValue)
	case *parser.ExpressionStatement:
		w.walkExpr(s.Expression)
	case *parser.BlockStatement:
		for _, inner := range s.Statements {
			w.walkStmt(inner, "")
		}
	case *parser.BlockMatchStatement:
		// 塊 match 的主體 / 臂體與一般區塊同口徑下鑽。
		for _, inner := range blockMatchChildStatements(s) {
			w.walkStmt(inner, "")
		}
	case *parser.ForStatement:
		if s.Init != nil {
			w.walkStmt(s.Init, "")
		}
		w.walkExpr(s.Condition)
		if s.Update != nil {
			w.walkStmt(s.Update, "")
		}
		if s.Body != nil {
			for _, inner := range s.Body.Statements {
				w.walkStmt(inner, "")
			}
		}
	}
}

func (w *optCmpWalker) enterFunc(name string, results, params []*parser.Parameter) {
	w.fn = name
	w.curResults = make(map[string]bool, len(results))
	for _, r := range results {
		if r != nil && r.Name != "" {
			w.curResults[r.Name] = true
		}
	}
	for _, p := range params {
		if p == nil || p.Name == "" || p.Type == nil {
			continue
		}
		w.locals[w.key(p.Name)] = p.Type.String()
		w.declared[w.key(p.Name)] = true
	}
	for _, r := range results {
		if r == nil || r.Name == "" || r.Type == nil {
			continue
		}
		w.locals[w.key(r.Name)] = r.Type.String()
		w.declared[w.key(r.Name)] = true
	}
}

// recordLocal 記錄 `x = ...` / `x T = ...` 的型別（供後續比較判定）。
func (w *optCmpWalker) recordLocal(name string, typ parser.Type, value parser.Expression) {
	if typ != nil {
		if t := typ.String(); t != "" {
			w.locals[w.key(name)] = t
			w.declared[w.key(name)] = true
			return
		}
	}
	if t, ok := w.typeOf(value); ok {
		w.locals[w.key(name)] = t
		// 推斷型別：字面量 / 轉型 / 取反的推斷是可靠的（`s = 'x'` 就是 str），
		// 呼叫則不可靠——合併多模組時方法呼叫被攤平成裸識別符
		// （`.to-f64()` → `str.to-f64(.)`），而方法在 funcTypes 裡登記的是
		// self 型別 ⇒ `f = .to-f64()` 的 f 會被推成 `str`。nil 子規則只信
		// declared，見 nilCompareTypeOf。
		if _, isCall := value.(*parser.CallExpression); !isCall {
			w.declared[w.key(name)] = true
		}
		delete(w.untyped, w.key(name))
		return
	}
	// 型別未知：顯式記為「未知」，**不要**回退到全域 VarTypes——
	// 那張表會被其他函式的同名參數/結果參數污染（典型：`c`、`n`、`size`
	// 在別的函式裡是 ?server-conn / ?i64），會造成大量誤報。
	w.untyped[w.key(name)] = true
	delete(w.locals, w.key(name))
}

// ── 表達式走訪 ────────────────────────────────────────────────

func (w *optCmpWalker) walkExpr(e parser.Expression) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *parser.InfixExpression:
		w.checkCompare(x)
		w.walkExpr(x.Left)
		w.walkExpr(x.Right)
	case *parser.PrefixExpression:
		w.walkExpr(x.Right)
	case *parser.GroupedExpression:
		w.walkExpr(x.Expression)
	case *parser.CallExpression:
		w.walkExpr(x.Function)
		for _, a := range x.Arguments {
			w.walkExpr(a)
		}
	case *parser.IndexExpression:
		w.walkExpr(x.Left)
		w.walkExpr(x.Index)
	case *parser.AssignExpression:
		w.walkExpr(x.Left)
		w.walkExpr(x.Value)
	case *parser.AwaitExpression:
		w.walkExpr(x.Right)
	case *parser.DotExpression:
		w.walkExpr(x.Receiver)
	case *parser.ArrayLiteral:
		for _, el := range x.Elements {
			w.walkExpr(el)
		}
	case *parser.FunctionLiteral:
		if x.Body != nil {
			for _, inner := range x.Body.Statements {
				w.walkStmt(inner, "")
			}
		}
	case *parser.IfExpression:
		// 實 if/else：match 在糖形態下是 SurfaceMatch（見下方 case），不經此路徑。
		w.walkExpr(x.Condition)
		if x.Consequence != nil {
			for _, inner := range x.Consequence.Statements {
				w.walkStmt(inner, "")
			}
		}
		if x.Alternative != nil {
			for _, inner := range x.Alternative.Statements {
				w.walkStmt(inner, "")
			}
		}
	case *parser.SurfaceMatch:
		// S4：糖形態下 match 是 SurfaceMatch（表達式節點），經 ExpressionStatement
		// 抵達本 walker。臂體對本 walker 不可見——與 IfExpression case 同語義：
		// matched 是 option 時，臂內 `it` 是解包後內層值。
		if x.Matched != nil {
			if mt, _ := w.typeOf(x.Matched); mt != "" {
				if inner := innerOf(mt); inner != "" {
					w.itStack = append(w.itStack, inner)
					defer func() { w.itStack = w.itStack[:len(w.itStack)-1] }()
				}
			}
		}
		walkSurfaceMatchChildren(x, nil, w.walkExpr, func(s parser.Statement) { w.walkStmt(s, "") })
	}
}

func (w *optCmpWalker) checkCompare(e *parser.InfixExpression) {
	switch e.Operator {
	case "==", "!=", "<", ">", "<=", ">=":
	default:
		return
	}
	// 先跑 nil 子規則：`nil` 只對 option 有意義，非 option 的 `T == nil`
	// 是**另一類**錯誤（不是「option 與非 option 比較」），診斷碼與文案都不同。
	if w.checkNilCompare(e) {
		return
	}
	lt, lok := w.typeOf(e.Left)
	rt, rok := w.typeOf(e.Right)
	lOpt := lok && ty.IsOptionRaw(lt)
	rOpt := rok && ty.IsOptionRaw(rt)
	if lOpt == rOpt {
		return // 兩側都是 / 都不是 option
	}
	var optExpr, otherExpr parser.Expression
	var optType, otherType string
	if lOpt {
		optExpr, otherExpr, optType, otherType = e.Left, e.Right, lt, rt
	} else {
		optExpr, otherExpr, optType, otherType = e.Right, e.Left, rt, lt
	}
	// 另一側型別未知 → 不報（可能是跨模組回傳、泛型、struct 欄位）。
	if otherType == "" {
		return
	}
	// 另一側是 option 變體標記（ok / nil / err）→ 這是 match 的正規寫法。
	if otherType == optionVariantType || otherType == optionNilType {
		return
	}
	// option 與「純量 / 值型別」比較是合法的：nolang 的 `==` / `<` 等比較會解開
	// option 的內層值再比（實證 `?i64 == 42` / `?i64 < 100` 在 runtime 皆正確運作，
	// tests/opt-container-len.no 即依賴此行為印出 `ok: ?i64 scalar unchanged`）。
	// 舊規則假設「拿 {tag,data} 跟數字比是無意義的」是錯的——語言實際比對的是內層值，
	// 故 option-vs-純量比較不應報 fe0a5wt2（參見 src/checker/option_compare_test.go）。
	if isValueComparableType(otherType) {
		return
	}
	w.report(e, optExpr, otherExpr, optType, otherType)
}

// checkNilCompare 執行 nil 子規則：`nil` 只能與 option（`?T`）比較。
//
// 回傳 true 表示「本比較已由子規則裁決」——呼叫端必須停止後續的 option 判定，
// 否則同一節點會被 fe0a5wt2 再報一次（`nil` 不是 option，會落進
// 「一側 option、一側非 option」的形狀）。
//
// 放行的情形（保守，寧可漏報不可誤報）：
//   - 另一側型別未知（跨模組回傳、struct 欄位、泛型、`v ?= expr` 解包後的值）；
//   - 另一側本身是 option（`?T == nil`，本規則要保護的正規寫法）；
//   - 另一側是變體標記（`nil == nil`、`err`、`ok`）。
func (w *optCmpWalker) checkNilCompare(e *parser.InfixExpression) bool {
	if e.Operator != "==" && e.Operator != "!=" {
		return false
	}
	lt, lok := w.nilCompareTypeOf(e.Left)
	rt, rok := w.nilCompareTypeOf(e.Right)
	var other parser.Expression
	var otherType string
	var otherKnown bool
	switch {
	case lok && lt == optionNilType:
		other, otherType, otherKnown = e.Right, rt, rok
	case rok && rt == optionNilType:
		other, otherType, otherKnown = e.Left, lt, lok
	default:
		return false // 兩側都不是 nil 字面量
	}
	if !otherKnown || otherType == "" {
		return false
	}
	if ty.IsOptionRaw(otherType) || otherType == optionVariantType || otherType == optionNilType {
		return false
	}
	w.reportNilCompare(e, other, otherType)
	return true
}

// nilCompareTypeOf 是 nil 子規則專用的**保守**型別查詢：只認「顯式宣告」的
// 型別（參數、結果參數、`x T = ...`），不認推斷值。
//
// 為什麼不用 typeOf：合併多模組時方法呼叫會被攤平成裸識別符
// （`.to-f64()` → `str.to-f64(.)`），而方法在 funcTypes 裡登記的是 self
// 型別 ⇒ `f = .to-f64()` 的 f 會被推成 `str`，於是 std/str.no 的
// `f == nil ->` 這種**合法**的 option 判定被誤報成 q4nilcmp。
// 顯式宣告不受此影響：宣告型別就是寫出來的那個，沒有推斷歧義。
//
// 代價是漏報 `x = f(); x == nil`（x 是推斷型別）——本規則寧可漏報不可誤報。
func (w *optCmpWalker) nilCompareTypeOf(e parser.Expression) (string, bool) {
	switch x := e.(type) {
	case *parser.NilLiteral:
		return optionNilType, true
	case *parser.Identifier:
		if w.unwrapped[w.key(x.Value)] {
			return "", false
		}
		if !w.declared[w.key(x.Value)] {
			return "", false
		}
		if t, ok := w.locals[w.key(x.Value)]; ok {
			return t, true
		}
		return "", false
	}
	return "", false
}

// isValueComparableType 報告型別是否為可與 option 內層值直接比較的「純量 / 值型別」。
// 這些型別的 option（`?T`）與對應純量比較時，nolang 會解開 option 比對內層值
// （`?i64 == 42` 合法），故不應觸發 fe0a5wt2。struct / interface / fn 等非值型別
// 仍走原規則（其與 option 的比較語意不明，保留報錯）。
func isValueComparableType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "byte", "int",
		"f32", "f64", "float",
		"char", "bool", "str", "txt":
		return true
	}
	return false
}

// typeOf 盡力推斷表達式的靜態型別；第二個回傳值為 false 表示型別未知。
func (w *optCmpWalker) typeOf(e parser.Expression) (string, bool) {
	if e == nil {
		return "", false
	}
	switch x := e.(type) {
	case *parser.IntegerLiteral:
		return "i64", true
	case *parser.FloatLiteral:
		return "f64", true
	case *parser.StringLiteral:
		return "str", true
	case *parser.CharLiteral:
		return "char", true
	case *parser.BooleanLiteral:
		return "bool", true
	case *parser.NilLiteral:
		return optionNilType, true
	case *parser.CastExpression:
		if x.Type != nil {
			if t := x.Type.String(); t != "" {
				return t, true
			}
		}
		return "", false
	case *parser.Identifier:
		switch x.Value {
		case "ok", "nil", "err":
			return optionVariantType, true
		case "it":
			// match 的 ok / err 臂裡，`it` 是**解包後**的內層值
			// （如 `v: { ok -> ... }` 中 it 是 v 的內層型別），不是 option。
			// 直接拿整個 ?T 當 it 會把 `n = it; n <= 127` 這類合法寫法
			// 誤報成 option 比較（str.no / txt.no 的 to-i8）。
			if inner := w.itType(); inner != "" {
				return inner, true
			}
			return "", false
		}
		// `v ?= expr` 綁定的是解包後的內層值：不是 option。
		if w.unwrapped[w.key(x.Value)] {
			return "", false
		}
		// 只信任「當前函式作用域內」推斷出的型別（來自 enterFunc 的
		// 參數/結果參數，或 recordLocal 的區域綁定）。**不回退**到全域
		// Sem.VarTypes / Sem.FuncVarTypes —— 那張表混雜了所有函式的同名
		// 參數與結果參數（典型：`conn`/`fd`/`n` 在別的函式裡是 ?conn/?i64），
		// 回退會造成大量誤報（如 tls.no 的 tcpfd、str.no 的 n）。
		if t, ok := w.locals[w.key(x.Value)]; ok {
			return t, true
		}
		// 型別未知：不報（可能是跨模組回傳、struct 欄位、泛型、
		// match 的 it 綁定 —— 這些我們無法在單檔/輕量語境下安全判定）。
		return "", false
	case *parser.CallExpression:
		// 只推斷「裸識別符呼叫」（如 `fstat-size(.fd)`、`dial(...)`）的回傳型別。
		// **不**推斷方法/模組呼叫（DotExpression，如 `tls-c.connect(...)`、
		// `dns.dial(...)`）：方法名在 funcTypes 裡以「短名」儲存，會與同名
		// 的裸函式碰撞（例：`conn.connect` 回傳 bool，但同檔有裸 `connect`
		// 回傳 ?conn），誤把 `ok2 = tls-c.connect(...); ok2 == false` 報成
		// option 比較。方法呼叫的精確型別需 receiver 型別解析，超出本規則
		// 的靜態推斷範圍——推不出就當作「型別未知、不報」，避免誤報。
		id, ok := x.Function.(*parser.Identifier)
		if !ok {
			return "", false
		}
		if t, ok := w.funcTypes[id.Value]; ok {
			return t, true
		}
		// std 內建：std 宣告為單一 ?T，但 registry 的 Return 是原始 C pair，
		// 只能用白名單判定「回傳 option」。
		if builtin.IsOptionReturnBuiltin(id.Value) {
			return unknownOptionInner, true
		}
		return "", false
	case *parser.InfixExpression:
		switch x.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool", true
		}
		return "", false
	case *parser.PrefixExpression:
		if x.Operator == "!" {
			return "bool", true
		}
		return "", false
	case *parser.GroupedExpression:
		return w.typeOf(x.Expression)
	}
	return "", false
}

func optCmpCallName(c *parser.CallExpression) string {
	switch fn := c.Function.(type) {
	case *parser.Identifier:
		return fn.Value
	case *parser.DotExpression:
		return fn.Property
	}
	return ""
}

// exprName 產生診斷訊息裡可讀的表達式名稱。
func exprName(e parser.Expression) string {
	switch x := e.(type) {
	case *parser.Identifier:
		return x.Value
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", x.Value)
	case *parser.StringLiteral:
		return "'" + x.Value + "'"
	case *parser.CallExpression:
		if n := optCmpCallName(x); n != "" {
			return n + "()"
		}
	}
	return "?"
}
