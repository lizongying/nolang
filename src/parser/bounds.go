// bounds.go — 靜態「可證明越界安全」索引讀取分析。
//
// 背景：arr/vec/slice 的直接變數索引「讀取」`v[i]` 預設回傳 option<elem>（越界為
// nil），必須被顯式處理（`?=` 上拋 / `#{index-out = DEF}` 取預設 / 宣告為 option /
// `_ =` 丟棄）。但當編譯期就能證明下標恆落於 `[0, len)` 時，該讀取永遠不會越界，
// option 包裝（以及配套的 `#{index-out}` 註解）其實是冗餘的。
//
// 本檔提供一個**保守**的判定：只有在下列全部成立時，才把一個索引讀取標為
// 「可證明在界內」（in-bounds）——
//
//  1. 基底 `v` 是**定長陣列** `[N]T`（N 為整數字面量）。切片 `[]T` / vec 的運行期
//     長度無法靜態得知（如 `x []byte = with-len(32)`），一律保守視為可能越界。
//  2. 下標 `i` 可證明恆在 `[0, N)`：
//     a) 整數字面量 `k` 且 `0 <= k < N`；或
//     b) `i` 恰為「外層 for-range 迴圈變數」，該迴圈形如 `i <- [a..b)`（端點皆整數
//        字面量），套用開閉區間後取值區間 `[lo..hi]` 滿足 `lo >= 0 && hi < N`，且
//        迴圈體內從未對 `i` 賦值 / 重新宣告 / 被巢狀同名 for-range 重綁。
//
// codegen（lowering）、`no vet`（ValidateUnhandledIndex）、`no fmt`（移除冗餘
// `#{index-out}`）三方必須共用同一份判定，否則會出現「fmt 刪了註解 → vet/編譯立刻
// 報未處理越界」的口徑不一致回歸（與 #{overflow} 移除的教訓同源）。故本檔把判定
// 集中在此，三个消费者分别调用：
//   - parser lowering：AnalyzInBoundsForFunc（每函數一份，喂給 isSafeIndexBase）
//   - checker：ProvablyInBoundsIndexReads（全程式，跳過 in-bounds 讀取的上報）
//   - fmt：AnalyzeInBoundsIndex（全程式，取 StmtRemovable 決定刪哪條註解）
package parser

import (
	"reflect"
	"strconv"
	"strings"
)

// arrayStaticLen 解析定長陣列型別字串 `[N]T` → (N, true)。
// 切片 `[]T`（中括號內空）、`vec[T]`、`[?]T`、`[n]T`（n 為識別符）等一律回傳 false，
// 因為其運行期長度無法靜態確定（保守）。
func arrayStaticLen(lt string) (int64, bool) {
	lt = strings.TrimPrefix(lt, "?")
	if !strings.HasPrefix(lt, "[") {
		return 0, false
	}
	i := strings.Index(lt, "]")
	if i < 0 {
		return 0, false
	}
	inner := lt[1:i]
	if inner == "" {
		return 0, false // 切片 []T
	}
	n, err := strconv.ParseInt(inner, 10, 64)
	if err != nil || n < 0 {
		return 0, false // [?] / [name] / 非數字：保守視為不定長
	}
	return n, true
}

// rangeLiteralBounds 計算 for-range `i <- rng` 實際迭代的下標閉區間 [lo..hi]。
// 僅當 Start/End 皆為整數字面量時回傳 ok。空迴圈（無迭代）回傳 ok 且 lo > hi。
func rangeLiteralBounds(rng *RangeExpression) (lo, hi int64, ok bool) {
	if rng == nil || rng.Start == nil || rng.End == nil {
		return 0, 0, false
	}
	s, isInt := rng.Start.(*IntegerLiteral)
	if !isInt {
		return 0, 0, false
	}
	e, isInt := rng.End.(*IntegerLiteral)
	if !isInt {
		return 0, 0, false
	}
	start, end := s.Value, e.Value
	if start <= end {
		lo = start
		if !rng.LeftInc {
			lo = start + 1
		}
		hi = end
		if !rng.RightInc {
			hi = end - 1
		}
	} else {
		// 降序區間（如 [5..1)）：迭代值從高到低，端點同樣決定取值上下界。
		hi = start
		if !rng.LeftInc {
			hi = start - 1
		}
		lo = end
		if !rng.RightInc {
			lo = end + 1
		}
	}
	return lo, hi, true
}

// boundsCtx 記錄當前作用域內每個 for-range 迴圈變數的取值閉區間 [lo..hi]。
type boundsCtx map[string][2]int64

func (c boundsCtx) clone() boundsCtx {
	n := make(boundsCtx, len(c))
	for k, v := range c {
		n[k] = v
	}
	return n
}

// boundsAnalyzer 在一個函數作用域內執行標記遍歷（pass A）。
type boundsAnalyzer struct {
	sem      *SemanticContext
	funcName string
	idxLocal map[string]string
	inb      map[*IndexExpression]bool
	// removable / annRemovable 在走訪過程中（作用域正確時）就地填充，避免結束後
	// 另起反射遍歷時 funcName/idxLocal 已退回全域作用域而查不到區域型別。
	removable     map[Statement]bool
	annRemovable  map[*AnnotationStatement]bool
}

// typeOf 取該名稱在「當前函數作用域」的靜態型別，**僅查 idxLocal**（即
// IdxLocalTypes，lower 預掃描所得）。刻意不回退 FuncVarType / 全域 VarTypes：
// 合併 std 的 vet 下，這些回退會被其它模組的同名變數污染（checker.ValidateUnhandledIndex
// 的 isSafeBase 同樣只用 IdxLocalTypes，見其註解）。本分析必須與 isSafeBase 對
// 「何為容器基底」的取值口徑完全一致，否則會出現「codegen 已把該讀取判為 in-bounds
// 並停止 option 降級，但 checker 重新分析時口徑不同 → 未標記 → 誤報 ERROR」。
// 回傳去 `?` 前綴的型別字串（查不到回傳 ""）。
func (a *boundsAnalyzer) typeOf(name string) string {
	if t := a.idxLocal[name]; t != "" {
		return strings.TrimPrefix(t, "?")
	}
	return ""
}

// resolveIdxLocal 依函數名取 IdxLocalTypes[fn]。合併 std vet 時 AST 裡的函數名可能
// 帶多餘模組前綴（如 "path.path.join"），而 IdxLocalTypes 的鍵是 lower 期記錄的單
// 前綴名（"path.join"）。與 checker.ValidateUnhandledIndex 的 isSafeBase 同口徑：先試
// 原名，再試「去掉第一段前綴」的名字，命中哪個用哪個。
func resolveIdxLocal(sem *SemanticContext, name string) map[string]string {
	if sem == nil || sem.IdxLocalTypes == nil {
		return nil
	}
	if m := sem.IdxLocalTypes[name]; m != nil {
		return m
	}
	if i := strings.Index(name, "."); i >= 0 {
		if m := sem.IdxLocalTypes[name[i+1:]]; m != nil {
			return m
		}
	}
	return nil
}

// isContainerBase 報告索引基底是否為「直接變數基底的 arr/vec/slice」（會產生 option
// 讀取的那類）。與 isSafeIndexBase 同口徑：僅 Identifier 基底、容器型別。
func (a *boundsAnalyzer) isContainerBase(e *IndexExpression) bool {
	if e == nil {
		return false
	}
	id, ok := e.Left.(*Identifier)
	if !ok {
		return false
	}
	return containerElemType(a.typeOf(id.Value)) != ""
}

// mark 在給定 ctx 下判定 e 是否可證明在界內，是則記入 inb。
func (a *boundsAnalyzer) mark(e *IndexExpression, ctx boundsCtx) {
	if !a.isContainerBase(e) {
		return
	}
	id := e.Left.(*Identifier)
	n, ok := arrayStaticLen(a.typeOf(id.Value))
	if !ok {
		return // 切片 / vec：不定長，保守
	}
	switch idx := e.Index.(type) {
	case *IntegerLiteral:
		if idx.Value >= 0 && idx.Value < n {
			a.inb[e] = true
		}
	case *Identifier:
		if b, ok := ctx[idx.Value]; ok && b[0] >= 0 && b[1] < n {
			a.inb[e] = true
		}
	}
}

// markExpr 遞迴走訪表達式子樹，標記 in-bounds 讀取。
func (a *boundsAnalyzer) markExpr(e Expression, ctx boundsCtx) {
	switch v := e.(type) {
	case nil:
		return
	case *IndexExpression:
		a.mark(v, ctx)
		a.markExpr(v.Left, ctx)
		a.markExpr(v.Index, ctx)
	case *AssignExpression:
		// Left 為寫入目標（不產生 option 讀取）：仍遞迴其 Index 內層讀取，
		// 但不在此 mark 該寫入節點本身。
		a.markWriteTargetExpr(v.Left, ctx)
		a.markExpr(v.Value, ctx)
	case *InfixExpression:
		a.markExpr(v.Left, ctx)
		a.markExpr(v.Right, ctx)
	case *PrefixExpression:
		a.markExpr(v.Right, ctx)
	case *GroupedExpression:
		a.markExpr(v.Expression, ctx)
	case *CallExpression:
		a.markExpr(v.Function, ctx)
		for _, arg := range v.Arguments {
			a.markExpr(arg, ctx)
		}
	case *DotExpression:
		a.markExpr(v.Receiver, ctx)
	case *ArrayLiteral:
		for _, el := range v.Elements {
			a.markExpr(el, ctx)
		}
	case *SliceExpression:
		a.markExpr(v.Left, ctx)
		if v.Range != nil {
			a.markExpr(v.Range.Start, ctx)
			a.markExpr(v.Range.End, ctx)
		}
	case *IfExpression:
		a.markExpr(v.Condition, ctx)
		a.markBlock(v.Consequence, ctx)
		a.markBlock(v.Alternative, ctx)
	case *ConditionalExpression:
		a.markExpr(v.Condition, ctx)
		a.markExpr(v.Consequence, ctx)
		a.markExpr(v.Alternative, ctx)
	case *CastExpression:
		a.markExpr(v.Expr, ctx)
	}
	// 未列舉的運算式型別：保守略過（不標記 → 不會誤刪）。
}

// markWriteTargetExpr 處理寫入目標（assign-left）：若為索引寫入，僅遞迴其 Index 的
// 內層讀取，寫入節點本身不是 option 讀取，不標記。
func (a *boundsAnalyzer) markWriteTargetExpr(e Expression, ctx boundsCtx) {
	if idx, ok := e.(*IndexExpression); ok {
		a.markExpr(idx.Index, ctx)
		return
	}
	a.markExpr(e, ctx)
}

// markStmt 遞迴走訪陳述，維護 for-range 迴圈變數 ctx，標記 in-bounds 讀取。
func (a *boundsAnalyzer) markStmt(s Statement, ctx boundsCtx) {
	switch v := s.(type) {
	case nil:
		return
	case *LetStatement:
		a.markExpr(v.Value, ctx)
	case *ReturnStatement:
		a.markExpr(v.ReturnValue, ctx)
	case *ExpressionStatement:
		a.markExpr(v.Expression, ctx)
	case *MultiAssignStatement:
		for _, t := range v.Targets {
			a.markWriteTargetExpr(t, ctx)
		}
		a.markExpr(v.Value, ctx)
	case *UnwrapAssignStatement:
		a.markWriteTargetExpr(v.Target, ctx)
		a.markExpr(v.Value, ctx)
	case *BlockStatement:
		a.markBlock(v, ctx)
	case *FunctionDefinition:
		a.markFunc(v)
	case *ForStatement:
		a.markFor(v, ctx)
	}
}

func (a *boundsAnalyzer) markBlock(b *BlockStatement, ctx boundsCtx) {
	if b == nil {
		return
	}
	a.markStmts(b.Statements, ctx)
}

func (a *boundsAnalyzer) markStmts(list []Statement, ctx boundsCtx) {
	// 第一輪：標記各陳述子樹的 in-bounds 讀取（含遞迴進巢狀區塊 / 函數）。
	for _, s := range list {
		a.markStmt(s, ctx)
	}
	// 第二輪：此刻 a.inb 已填妥本清單（含巢狀）的標記，且作用域正確（funcName /
	// idxLocal 為本清單所屬函數），就地計算 removable，避免走訪結束後另起遍歷時
	// 作用域已退回全域而查不到區域容器型別。
	for _, s := range list {
		a.computeRemovable(s)
	}
	a.scanAnn(list)
}

// computeRemovable：若陳述 s 至少有一個容器索引讀取、且其整棵子樹的容器索引讀取
// 全部 in-bounds，則標記 s 為可移除 `#{index-out}`。
func (a *boundsAnalyzer) computeRemovable(s Statement) {
	if _, ok := s.(*AnnotationStatement); ok {
		return
	}
	reads := a.enumContainerReads(s)
	if len(reads) == 0 {
		return
	}
	for idx := range reads {
		if !a.inb[idx] {
			return
		}
	}
	if a.removable != nil {
		a.removable[s] = true
	}
}

// scanAnn 掃描同一清單內「獨立成行」的 #{index-out} 註解，若其管轄的下一條陳述可
// 移除，則標記該註解節點可移除。管轄語意與 parser.applyLineIndexOutAnnotations 一致。
func (a *boundsAnalyzer) scanAnn(list []Statement) {
	if a.annRemovable == nil {
		return
	}
	for i, s := range list {
		as, ok := s.(*AnnotationStatement)
		if !ok || !hasIndexOutEntry(as.Entries) {
			continue
		}
		for j := i + 1; j < len(list); j++ {
			next := list[j]
			if next == nil {
				continue
			}
			if as2, isAnn := next.(*AnnotationStatement); isAnn {
				if hasIndexOutEntry(as2.Entries) {
					break
				}
				continue
			}
			if a.removable[next] {
				a.annRemovable[as] = true
			}
			break
		}
	}
}

// markFor 處理 for 迴圈：若為「i <- [字面量區間)」且 i 在迴圈體內未被重寫/重綁，
// 則在走訪迴圈體時把 i 的取值區間加入 ctx（新建 map，避免污染同級其它陳述）。
func (a *boundsAnalyzer) markFor(fs *ForStatement, ctx boundsCtx) {
	if fs.Init != nil {
		a.markStmt(fs.Init, ctx)
	}
	if fs.Condition != nil {
		a.markExpr(fs.Condition, ctx)
	}
	if fs.Update != nil {
		a.markStmt(fs.Update, ctx)
	}
	bodyCtx := ctx
	if it := fs.IterRange; it != nil && it.Variable != "" && it.Range != nil {
		if lo, hi, ok := rangeLiteralBounds(it.Range); ok {
			// 迴圈體（含巢狀子迴圈頭）若對同名變數赋值 / 重綁，保守放棄此迴圈的
			// 界內推斷（該變數取值不再恆等於迭代值）。
			if !reassignsIdent(fs.Body, it.Variable) {
				bodyCtx = ctx.clone()
				bodyCtx[it.Variable] = [2]int64{lo, hi}
			}
		}
	}
	a.markBlock(fs.Body, bodyCtx)
}

// markFunc 為巢狀函數定義建立獨立作用域（型別表、清空外層迴圈 ctx）。
func (a *boundsAnalyzer) markFunc(fd *FunctionDefinition) {
	if fd == nil || fd.Body == nil {
		return
	}
	savedName, savedLocal := a.funcName, a.idxLocal
	a.idxLocal = resolveIdxLocal(a.sem, fd.Name)
	a.funcName = fd.Name
	a.markStmts(fd.Body.Statements, boundsCtx{})
	a.funcName, a.idxLocal = savedName, savedLocal
}

// reassignsIdent 報告 body 內是否出現對 name 的賦值 / 重新宣告 / for-range 重綁
// （任何會改變其取值的手段）。用泛型走訪，寧多勿少：任何可疑寫入都算。
func reassignsIdent(body *BlockStatement, name string) bool {
	if body == nil {
		return false
	}
	found := false
	walkNodes(body, func(n interface{}) {
		if found {
			return
		}
		switch v := n.(type) {
		case *LetStatement:
			if v.Name != nil && v.Name.Value == name {
				found = true
			}
		case *AssignExpression:
			if id, ok := v.Left.(*Identifier); ok && id.Value == name {
				found = true
			}
		case *UnwrapAssignStatement:
			if v.Name != nil && v.Name.Value == name {
				found = true
			}
			if id, ok := v.Target.(*Identifier); ok && id.Value == name {
				found = true
			}
		case *MultiAssignStatement:
			for _, t := range v.Targets {
				if id, ok := t.(*Identifier); ok && id.Value == name {
					found = true
				}
			}
		case *ForStatement:
			if v.IterRange != nil && v.IterRange.Variable == name {
				found = true
			}
		}
	})
	return found
}

// enumContainerReads 泛型走訪 s 的整棵子樹，收集所有「直接變數基底容器索引」讀取
// 節點（排除指派目標的寫入索引：`a[i] = ...` 的左側 `a[i]`、多-target / ?= 目標）。
// 寫入不產生 option 讀取，不應计入「全部讀取都在界內」的判定，否則像
// `p[i] = p-tmp[i]`（p 為切片寫入、p-tmp[i] 為定長陣列讀取）會被誤判為不可移除。
func (a *boundsAnalyzer) enumContainerReads(s Statement) map[*IndexExpression]bool {
	writes := map[*IndexExpression]bool{}
	walkNodes(s, func(n interface{}) {
		switch v := n.(type) {
		case *AssignExpression:
			if idx, ok := v.Left.(*IndexExpression); ok {
				writes[idx] = true
			}
		case *MultiAssignStatement:
			for _, t := range v.Targets {
				if idx, ok := t.(*IndexExpression); ok {
					writes[idx] = true
				}
			}
		case *UnwrapAssignStatement:
			if idx, ok := v.Target.(*IndexExpression); ok {
				writes[idx] = true
			}
		}
	})
	out := map[*IndexExpression]bool{}
	walkNodes(s, func(n interface{}) {
		if idx, ok := n.(*IndexExpression); ok && !writes[idx] && a.isContainerBase(idx) {
			out[idx] = true
		}
	})
	return out
}

// InBoundsIndex 是分析結果。
type InBoundsIndex struct {
	// Reads：可證明在界內的索引讀取節點集合（供 codegen / checker 跳過 option 處理）。
	Reads map[*IndexExpression]bool
	// StmtRemovable：其「整棵子樹」所有容器索引讀取皆可證明在界內（且至少一個）的陳述。
	// formatter 據此移除附加/管轄於該陳述的 `#{index-out}` 註解。
	StmtRemovable map[Statement]bool
	// AnnRemovable：獨立成行的 `#{index-out}` 註解陳述（其管轄的下一條陳述全部讀取
	// 在界內），formatter 據此移除整條獨立註解。
	AnnRemovable map[*AnnotationStatement]bool
}

// analyzeFunc 在一個函數作用域上執行分析：markStmts 已就地填妥 inb / removable。
func analyzeFunc(sem *SemanticContext, funcName string, idxLocal map[string]string, body *BlockStatement) (map[*IndexExpression]bool, map[Statement]bool) {
	a := &boundsAnalyzer{
		sem: sem, funcName: funcName, idxLocal: idxLocal,
		inb:         map[*IndexExpression]bool{},
		removable:   map[Statement]bool{},
		annRemovable: map[*AnnotationStatement]bool{},
	}
	if body != nil {
		a.markStmts(body.Statements, boundsCtx{})
	}
	return a.inb, a.removable
}

// AnalyzeInBoundsIndex 對整份程式執行分析（含所有函數定義）。供 fmt / checker 使用。
// 型別表取自 program.Sem（IdxLocalTypes / FuncVarTypes / VarTypes），與 lowering
// 期一致。markStmts 走訪時會就地、在正確函數作用域下填妥 inb / removable /
// annRemovable，無須結束後另起反射遍歷。
func AnalyzeInBoundsIndex(prog *Program) InBoundsIndex {
	res := InBoundsIndex{
		Reads:         map[*IndexExpression]bool{},
		StmtRemovable: map[Statement]bool{},
		AnnRemovable:  map[*AnnotationStatement]bool{},
	}
	if prog == nil {
		return res
	}
	sem := prog.Sem
	var idxLocal map[string]string
	if sem != nil {
		idxLocal = sem.IdxLocalTypes[""]
	}
	a := &boundsAnalyzer{
		sem:          sem,
		funcName:     "",
		idxLocal:     idxLocal,
		inb:          map[*IndexExpression]bool{},
		removable:    res.StmtRemovable,
		annRemovable: res.AnnRemovable,
	}
	a.markStmts(prog.Statements, boundsCtx{})
	for k, v := range a.inb {
		res.Reads[k] = v
	}
	return res
}

// hasIndexOutEntry 報告註解條目中是否含有 index-out 鍵。
func hasIndexOutEntry(entries []*AnnotationEntry) bool {
	for _, e := range entries {
		if e != nil && e.Key == "index-out" {
			return true
		}
	}
	return false
}

// AnalyzInBoundsForFunc 供 lowering 呼叫：在一個函數定義上計算 in-bounds 讀取集合。
// idxLocal 為該函數的「安全索引專用」本地型別表（p.idxLocalTypes[fd.Name]）。
func AnalyzInBoundsForFunc(sem *SemanticContext, fd *FunctionDefinition, idxLocal map[string]string) map[*IndexExpression]bool {
	if fd == nil || fd.Body == nil {
		return nil
	}
	inb, _ := analyzeFunc(sem, fd.Name, idxLocal, fd.Body)
	return inb
}

// ProvablyInBoundsIndexReads 回傳整份程式中「可證明越界安全」的索引讀取節點集合。
// checker 的 ValidateUnhandledIndex 用它跳過這類讀取的上報，確保 `no fmt` 移除冗餘
// `#{index-out}` 後 `no vet` 不會新增 ERROR（口徑與 codegen 一致）。
func ProvablyInBoundsIndexReads(prog *Program) map[*IndexExpression]bool {
	return AnalyzeInBoundsIndex(prog).Reads
}

// ---- 泛型 AST 走訪（反射）----
//
// 覆蓋所有 AST 節點型別，避免逐型別 switch 遺漏導致「少收集到一個讀取 → 誤判全在
// 界內 → 誤刪註解」的不安全方向。AST 以介面持有子節點、皆為指標，可能共享（DAG），
// 故以指標去重。

// walkNodes 走訪 root 子樹，對每個可尋址節點（指標背後的值）呼叫 visit(node)。
func walkNodes(root interface{}, visit func(node interface{})) {
	if root == nil || visit == nil {
		return
	}
	seen := map[uintptr]bool{}
	walkReflect(reflect.ValueOf(root), visit, seen)
}

func walkReflect(v reflect.Value, visit func(interface{}), seen map[uintptr]bool) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if seen[ptr] {
			return
		}
		seen[ptr] = true
		if visit != nil {
			visit(v.Interface())
		}
		walkReflect(v.Elem(), visit, seen)
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		walkReflect(v.Elem(), visit, seen)
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkReflect(v.Index(i), visit, seen)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue // 非導出欄位（其 Interface() 會 panic，跳過）
			}
			walkReflect(v.Field(i), visit, seen)
		}
	}
}
