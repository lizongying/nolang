// resolver.go — 解析与语义分离：语义副表（side-table）与 Resolver pass。
//
// 设计目标：parser 只产“语法 AST”（不含任何语义字段）；类型推断、注解、
// embed、平台键等全部语义信息集中存放于 SemanticContext（side-table），由独立的
// ResolveProgram pass 在解析之后、lowering 之前收尾计算。下游（lowering /
// transpiler / formatter / lsp）统一通过 prog.Sem.XxxOf(node) 读取语义信息，
// 不再直接访问 AST 节点上的语义字段。
package parser

// NodeSemantics 存放单个 AST 节点的语义信息（原分散在节点上的 Annotations /
// PlatformKeys / EmbedData / GenericParams 字段，以及类型推断用的结果之外的
// 节点级数据）。
//
// RawAnnotations 是 parser 在解析期从 #{...} 直接收集的原始註解条目；
// ResolveProgram 据此推算出 Annotations / PlatformKeys / GenericParams /
// EmbedData 等“成品”语义，存入同一结构。
type NodeSemantics struct {
	// RawAnnotations 解析期收錄的原始 #{...} 註解條目（未經篩選）。
	RawAnnotations []*AnnotationEntry

	// 以下欄位由 ResolveProgram 計算填充：
	Annotations   []*AnnotationEntry // 過濾後的註解條目（與 RawAnnotations 相同集合，便於下游直接取用）
	PlatformKeys  []string           // 平台註解 key（如 ["mac-arm64"]）；空 = 平台通用
	GenericParams []string           // 泛型型別參數名（來自 #{generic=[K,V]}）
	EmbedData     []byte             // 編譯期嵌入的文件字節（來自 #{embed=...}）
	EmbedFiles    map[string][]byte  // directory embed (relative path -> content, #{embed=dir})

	// RTFlags 是 fmt 往返（round-trip）專用的表層語法標誌位（裸 match /
	// wildcard arm / standalone if / rawCond 包裝層 / elif）。僅 formatter
	// 讀取，編譯管線（build/checker/codegen）不讀。
	RTFlags RTFlag

	// OpeningBraceComment 保存 `{` 同行註釋（BlockStatement 或裸 match 的
	// IfExpression）。僅 formatter 讀取。
	OpeningBraceComment *CommentGroup

	// Callee 記錄「這個 CallExpression 被改寫成了什麼」（見 CalleeKind）。
	//
	// 用途：整程式 pass 會把方法呼叫 `recv.m(args)` 攤平為自由函式呼叫
	// `Type.m(recv, args)`（接收者 unshift 進 Arguments，被呼叫者換成
	// Identifier），語法上的 DotExpression 隨之消失。攤平後「第一個實參曾
	// 是接收者」這件事在 AST 上已不可自證，過去只能靠「被呼叫者的名字是否
	// 撞上某個方法定義」反推——那既會漏（同名 alias）也會誤（自由函式被
	// 誤認）。改寫時在此留一筆，就把這個事實變成**顯式記錄**。
	//
	// 寫入者（全部在整程式 pass 中，皆早於 RunAllLints）：
	//   - build/transpiler.go 的 resolveMethodCall（攤平方法呼叫，兩個分支）
	//     → CalleeMethod。此時 program 即 merged，其 Sem 已建好並歸併完各模塊。
	//   - checker.go 的 resolveModuleCallsInExpr（衝突函式改名 `module.fn`）
	//     → CalleeModuleFn。
	// 讀取者：checker（目前唯一消費點是 ValidateUnassignedReturns 系列的
	// collectAssignedNamesInExpr）。
	Callee CalleeKind
}

// CalleeKind 描述一個 CallExpression 在整程式改寫後代表哪一類呼叫。
//
// 注意：**原始碼永遠寫不出帶點的 Identifier**——lexer 的 isLetter 含 `-`
// （這才是 `enc-conn` 合法的原因）但不含 `.`，readIdentifier 只吃
// isLetter||isDigit。所以「被呼叫者是 Identifier 且名字帶點」只可能來自
// 改寫，源碼在此一律先給 DotExpression。
type CalleeKind uint8

const (
	// CalleeUnknown 未記錄：源碼原樣的呼叫，或未經本機制改寫的呼叫。
	CalleeUnknown CalleeKind = iota
	// CalleeMethod：由 `recv.m(args)` 攤平而來的（用戶自訂）方法呼叫，
	// Arguments[0] 是接收者。接收者可被該方法改寫（如 `ec.init(c, key)`）。
	//
	// 已知的非寫入者：checker 自己的 resolveSelfInExpr 也會把方法體內的
	// `self.m(args)` 改寫成 `Type.m(self, args)`（同樣是 AST、同樣早於
	// RunAllLints），但**刻意不記錄**——它的接收者恆為 `self`，而 `self`
	// 已被 declaredResults 從返回參數中剔除，永不參與「未賦值返回參數」
	// 這類判定。因此這裡不收錄它，不是漏寫。
	CalleeMethod
	// CalleeModuleFn：由模組限定自由函式呼叫改寫而來（衝突改名為
	// `module.fn`）。Arguments[0] 是**普通實參**，不是接收者，按值傳入。
	//
	// 它與 CalleeMethod 的**形狀完全相同**（皆為 `Identifier{"帶點名"}` +
	// 首實參），兩者共用同一個字串命名空間（見 build/module_prefix.go）；
	// 記錄下來才區分得出「首實參是不是接收者」。
	CalleeModuleFn
)

// RTFlag 是 fmt 往返專用的表層語法標誌位集合。
type RTFlag uint8

const (
	// RTBareMatch 標記 IfExpression 來自裸 match 表達式 `{ cond -> body }`，
	// 格式化器應輸出新式語法而非 if/else。
	RTBareMatch RTFlag = 1 << iota
	// RTMatchWildcard 標記此 arm 為 catch-all wildcard `->`（desugar 時
	// Condition 被設為 IntegerLiteral(1)，此標誌讓 formatter 直接識別）。
	RTMatchWildcard
	// RTStandalone 標記 `cond -> body` 形式的裸 if-then 表達式（無外層 `{ }`）。
	RTStandalone
	// RTMatchWrapper 標記 rawCond 包裝層 `if 1 { it = matched; <if-chain> }`。
	RTMatchWrapper
	// RTElif 標記來自 deprecated `elif` desugar 的 IfExpression。
	RTElif
	// RTElseNewline 標記 standalone if-then 的 else arm 是在新行上被附加的
	// （在 parseBlockStatement 循環中），與同行 `cond -> body -> elseBody` 區分。
	// Formatter 根據此標誌在新行輸出 `->`。
	RTElseNewline
)

// SemanticContext 是解析/语义分离后的“副表”。
//
//  - nodeSem：以 AST 節點（指針，包裝為 Node 接口）為鍵的語義信息表。
//  - VarTypes / EnumVariants / DeclaredVars：類型推斷結果
//    （原 parser.varDeclTypes / enumVariantNames / declaredVars）。nolang 是單遍
//    遞歸下降解析器，部分類型感知（如 match arm 分類、方法返回型別推斷）必須在
//    解析當下完成；這些推斷結果統一寫入本 side-table，而非散落在 parser 私有字段，
//    從而實現“語義結果集中存放、AST 節點零語義字段”。
type SemanticContext struct {
	nodeSem map[Node]*NodeSemantics

	// 類型推斷結果（原 parser 私有符號表）。
	VarTypes     map[string]string
	EnumVariants map[string][]string
	DeclaredVars map[string]bool

	// EnumVariantPayload：枚舉名 → 變體名 → 載荷型別字串（無載荷的單元變體為 ""）。
	// 標籤列舉（tagged enum）的載荷型別是「編譯器根據被匹配變數靜態型別自動補全
	// 完整命名」所必需的：match 分支寫裸名 `some`，解析期據此查出其載荷型別，
	// 供析構綁定 `some(v) ->` 的 `v` 綁型與 codegen 的 data 欄位拷貝。
	// 內建 option 也登記於此（ok→t、err→str、nil→""），保持資料驅動一致。
	EnumVariantPayload map[string]map[string]string
	// EnumVariantFields：枚舉名 → 變體名 → 載荷欄位型別列表（單元變體為 nil）。
	// 多欄位變體（如 rect(w f64, h f64)）的載荷以「合成結構體」表示，
	// 型別名為 `<枚舉名>.<變體名>`（由 codegen 登記為具名 LLVM struct）。
	EnumVariantFields map[string]map[string][]string

	// FuncVarTypes stores per-function-local variable types, keyed by
	// function name → variable name → type. This prevents same-named
	// locals in different functions (e.g. `r` in parse-i64 and parse-f64)
	// from colliding in the global VarTypes map during lowering.
	FuncVarTypes     map[string]map[string]string
	FuncDeclaredVars map[string]map[string]bool

	// IdxLocalTypes 是「安全索引專用」的解析器本地型別表（函數作用域）：
	// 記錄由 to-vec() 等 RHS 推導出的容器型別（如 `av = a.to-vec()` → []i64），
	// 供 checker 的 ValidateUnhandledIndex 判定索引基底是否為 arr/vec/slice。
	// parser 實例在 ParseProgram 返回後即被丟棄，故 lowering 階段將此表掛到
	// 語義副表匯出，使 checker 能複現 isSafeIndexBase 的型別查詢。
	IdxLocalTypes map[string]map[string]string
}

// NewSemanticContext 建立空語義副表。
func NewSemanticContext() *SemanticContext {
	return &SemanticContext{
		nodeSem:            make(map[Node]*NodeSemantics),
		VarTypes:           make(map[string]string),
		EnumVariants:       make(map[string][]string),
		DeclaredVars:       make(map[string]bool),
		EnumVariantPayload: make(map[string]map[string]string),
		EnumVariantFields:  make(map[string]map[string][]string),
		FuncVarTypes:       make(map[string]map[string]string),
		FuncDeclaredVars:   make(map[string]map[string]bool),
	}
}

// Merge 將另一份語義副表合併進本表（模塊合併時使用：merged program 匯集多個
// 模塊的 AST 節點，各節點的語義信息也必須匯集到同一張 side-table）。
// nodeSem 以節點指針為鍵，不會衝突；名稱級映射（VarTypes 等）僅在本表缺失時填入。
func (s *SemanticContext) Merge(other *SemanticContext) {
	if s == nil || other == nil {
		return
	}
	if s.nodeSem == nil {
		s.nodeSem = make(map[Node]*NodeSemantics)
	}
	for n, ns := range other.nodeSem {
		if _, exists := s.nodeSem[n]; !exists {
			s.nodeSem[n] = ns
		}
	}
	for k, v := range other.VarTypes {
		if _, exists := s.VarTypes[k]; !exists {
			s.SetVarType(k, v)
		}
	}
	for k, v := range other.EnumVariants {
		if _, exists := s.EnumVariants[k]; !exists {
			s.SetEnumVariants(k, v)
		}
	}
	for enumName, variants := range other.EnumVariantPayload {
		for vn, pt := range variants {
			s.SetEnumVariantPayload(enumName, vn, pt)
		}
	}
	for enumName, variants := range other.EnumVariantFields {
		for vn, fs := range variants {
			s.SetEnumVariantFields(enumName, vn, fs)
		}
	}
	for k := range other.DeclaredVars {
		s.SetDeclared(k)
	}
	// Merge per-function variable types (don't overwrite existing entries).
	for fn, vars := range other.FuncVarTypes {
		for vn, vt := range vars {
			if existing, ok := s.FuncVarTypes[fn]; ok {
				if _, exists := existing[vn]; !exists {
					existing[vn] = vt
				}
			} else {
				s.SetFuncVarType(fn, vn, vt)
			}
		}
	}
	// Merge per-function declared vars.
	for fn, vars := range other.FuncDeclaredVars {
		for vn := range vars {
			s.SetFuncDeclared(fn, vn)
		}
	}
	// Merge safe-index local types (don't overwrite existing entries).
	for fn, vars := range other.IdxLocalTypes {
		for vn, vt := range vars {
			if s.IdxLocalTypes == nil {
				s.IdxLocalTypes = make(map[string]map[string]string)
			}
			if s.IdxLocalTypes[fn] == nil {
				s.IdxLocalTypes[fn] = make(map[string]string)
			}
			if _, exists := s.IdxLocalTypes[fn][vn]; !exists {
				s.IdxLocalTypes[fn][vn] = vt
			}
		}
	}
}

// SetEmbedData 設定節點的嵌入字節（由 Resolver 的 embed 解析填入 side-table）。
func (s *SemanticContext) SetEmbedData(n Node, data []byte) {
	s.ensure(n).EmbedData = data
}

// SetEmbedFiles sets the directory embed file map for a node.
func (s *SemanticContext) SetEmbedFiles(n Node, files map[string][]byte) {
	s.ensure(n).EmbedFiles = files
}

// ---- 節點級語義（side-table）----

// SetRawAnnotations 記錄某節點的原始 #{...} 註解條目（parser 解析期呼叫）。
func (s *SemanticContext) SetRawAnnotations(n Node, entries []*AnnotationEntry) {
	s.ensure(n).RawAnnotations = entries
}

// ensure 取得（必要時創建）節點的 NodeSemantics 條目。
func (s *SemanticContext) ensure(n Node) *NodeSemantics {
	if s.nodeSem == nil {
		s.nodeSem = make(map[Node]*NodeSemantics)
	}
	ns, ok := s.nodeSem[n]
	if !ok {
		ns = &NodeSemantics{}
		s.nodeSem[n] = ns
	}
	return ns
}

// SetRTFlag 為節點疊加 fmt 往返標誌位（parser/lowering 寫入）。
func (s *SemanticContext) SetRTFlag(n Node, fl RTFlag) {
	if s == nil || n == nil {
		return
	}
	s.ensure(n).RTFlags |= fl
}

// HasRTFlag 報告節點是否帶指定往返標誌。nil receiver 安全。
func (s *SemanticContext) HasRTFlag(n Node, fl RTFlag) bool {
	if s == nil {
		return false
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.RTFlags&fl != 0
	}
	return false
}

// SetCallee 記錄某個 CallExpression 被整程式改寫成了哪一種呼叫（見 CalleeKind）。
// 由 build/transpiler.go 的 resolveMethodCall 在攤平／改名當下寫入。
// nil receiver / nil 節點安全。
func (s *SemanticContext) SetCallee(n Node, k CalleeKind) {
	if s == nil || n == nil {
		return
	}
	s.ensure(n).Callee = k
}

// CalleeOf 回報節點被改寫成的呼叫種類；從未記錄時返回 CalleeUnknown
// （源碼原樣的呼叫，或未經本機制改寫者）。nil receiver 安全。
func (s *SemanticContext) CalleeOf(n Node) CalleeKind {
	if s == nil || n == nil {
		return CalleeUnknown
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.Callee
	}
	return CalleeUnknown
}

// SetOpeningBraceComment 設定節點的 `{` 同行註釋；cg 為 nil 時清除。
func (s *SemanticContext) SetOpeningBraceComment(n Node, cg *CommentGroup) {
	if s == nil || n == nil {
		return
	}
	if cg == nil {
		if ns, ok := s.nodeSem[n]; ok {
			ns.OpeningBraceComment = nil
		}
		return
	}
	s.ensure(n).OpeningBraceComment = cg
}

// OpeningBraceCommentOf 返回節點的 `{` 同行註釋（無則 nil）。nil receiver 安全。
func (s *SemanticContext) OpeningBraceCommentOf(n Node) *CommentGroup {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.OpeningBraceComment
	}
	return nil
}

// HasSemantics 報告該節點是否帶有任意語義信息。
func (s *SemanticContext) HasSemantics(n Node) bool {
	if s == nil {
		return false
	}
	_, ok := s.nodeSem[n]
	return ok
}

// AnnotationsOf 返回節點的註解條目（無則 nil）。nil receiver 安全。
func (s *SemanticContext) AnnotationsOf(n Node) []*AnnotationEntry {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.Annotations
	}
	return nil
}

// SetAnnotations 直接設定節點的「已解析」註解條目（ns.Annotations）。
// 供工具（如 `no fmt --fix=overflow`）在 ParseProgram 之後、不重跑
// ResolveProgram（重跑會把 #{generic=[K,V]} 等泛型參數重複附加到函式定義）
// 的前提下，把新增的註解條目（如 overflow=wrap）寫入解析側表，使 formatter
// 的 attachedAnnotations 能讀到並輸出。nil receiver / nil 節點安全。
func (s *SemanticContext) SetAnnotations(n Node, entries []*AnnotationEntry) {
	if s == nil || n == nil {
		return
	}
	s.ensure(n).Annotations = entries
}

// RawAnnotationsOf 返回節點在「解析期」由 #{...} 收集的原始註解條目（無則 nil）。
// 解析期 Annotations 尚未由 ResolveProgram 從 RawAnnotations 拷貝，故解析期邏輯
// （如 blockLevelOverflowMode / propagateOverflowInStmts）必須讀此欄位，否則永遠看不到
// 語句自帶（attached）的註解。nil receiver 安全。
func (s *SemanticContext) RawAnnotationsOf(n Node) []*AnnotationEntry {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.RawAnnotations
	}
	return nil
}

// RawAnnotationsOf 是 Parser 級便利訪問器，轉發到語義副表，供呼叫方在
// ResolveProgram 之前讀取 `#{...}` 的原始註解條目。nil 安全。
func (p *Parser) RawAnnotationsOf(n Node) []*AnnotationEntry {
	if p == nil {
		return nil
	}
	return p.sem.RawAnnotationsOf(n)
}

// PlatformKeysOf 返回節點的平台註解 key（無則 nil）。nil receiver 安全。
func (s *SemanticContext) PlatformKeysOf(n Node) []string {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.PlatformKeys
	}
	return nil
}

// GenericParamsOf 返回節點的泛型參數名（無則 nil）。nil receiver 安全。
func (s *SemanticContext) GenericParamsOf(n Node) []string {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.GenericParams
	}
	return nil
}

// EmbedDataOf 返回節點的嵌入字節（無則 nil）。nil receiver 安全。
func (s *SemanticContext) EmbedDataOf(n Node) []byte {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.EmbedData
	}
	return nil
}

// EmbedFilesOf returns the directory embed file map for a node (nil if none).
func (s *SemanticContext) EmbedFilesOf(n Node) map[string][]byte {
	if s == nil {
		return nil
	}
	if ns, ok := s.nodeSem[n]; ok {
		return ns.EmbedFiles
	}
	return nil
}

// ---- 類型推斷結果（side-table）----

// SetVarType 記錄變數宣告型別（含 ? 前綴表示 Option）。
func (s *SemanticContext) SetVarType(name, typ string) {
	if s.VarTypes == nil {
		s.VarTypes = make(map[string]string)
	}
	s.VarTypes[name] = typ
}

// SetFuncVarType records a function-local variable type in FuncVarTypes.
// If funcName is empty, falls back to the global VarTypes.
func (s *SemanticContext) SetFuncVarType(funcName, varName, typ string) {
	if funcName == "" {
		s.SetVarType(varName, typ)
		return
	}
	if s.FuncVarTypes == nil {
		s.FuncVarTypes = make(map[string]map[string]string)
	}
	if s.FuncVarTypes[funcName] == nil {
		s.FuncVarTypes[funcName] = make(map[string]string)
	}
	s.FuncVarTypes[funcName][varName] = typ
}

// FuncVarType queries a function-local variable type. Returns ("", false) if
// not found in the per-function map, falling back to the global VarTypes.
func (s *SemanticContext) FuncVarType(funcName, varName string) (string, bool) {
	if s.FuncVarTypes != nil {
		if vars, ok := s.FuncVarTypes[funcName]; ok {
			if t, ok := vars[varName]; ok {
				return t, true
			}
		}
	}
	// Fallback to global VarTypes (module-level vars, params, etc.)
	t, ok := s.VarTypes[varName]
	return t, ok
}

// SetFuncDeclared marks a variable as declared within a function scope.
func (s *SemanticContext) SetFuncDeclared(funcName, varName string) {
	if funcName == "" {
		s.SetDeclared(varName)
		return
	}
	if s.FuncDeclaredVars == nil {
		s.FuncDeclaredVars = make(map[string]map[string]bool)
	}
	if s.FuncDeclaredVars[funcName] == nil {
		s.FuncDeclaredVars[funcName] = make(map[string]bool)
	}
	s.FuncDeclaredVars[funcName][varName] = true
}

// IsFuncDeclared checks if a variable was declared within a function scope.
// Falls back to the global DeclaredVars only when the function is not tracked
// in FuncDeclaredVars (e.g. empty funcName or module-level code).
// When the function IS tracked, only per-function scope is checked — this
// prevents same-named locals in different functions (e.g. `r` in t-reader-init
// and `r` in t-reader-read-byte) from interfering with type inference.
func (s *SemanticContext) IsFuncDeclared(funcName, varName string) bool {
	if funcName != "" && s.FuncDeclaredVars != nil {
		if vars, ok := s.FuncDeclaredVars[funcName]; ok {
			return vars[varName]
		}
	}
	return s.IsDeclared(varName)
}

// VarType 查詢變數型別。
func (s *SemanticContext) VarType(name string) (string, bool) {
	t, ok := s.VarTypes[name]
	return t, ok
}

// SetEnumVariants 記錄枚舉型別的變體名列表。
func (s *SemanticContext) SetEnumVariants(name string, variants []string) {
	if s.EnumVariants == nil {
		s.EnumVariants = make(map[string][]string)
	}
	s.EnumVariants[name] = variants
}

// EnumVariantsOf 查詢枚舉變體名列表。
func (s *SemanticContext) EnumVariantsOf(name string) ([]string, bool) {
	v, ok := s.EnumVariants[name]
	return v, ok
}

// SetEnumVariantPayload 登記某枚舉變體的載荷型別（單元變體傳 ""）。
func (s *SemanticContext) SetEnumVariantPayload(enumName, variantName, payloadType string) {
	if s.EnumVariantPayload == nil {
		s.EnumVariantPayload = make(map[string]map[string]string)
	}
	if s.EnumVariantPayload[enumName] == nil {
		s.EnumVariantPayload[enumName] = make(map[string]string)
	}
	s.EnumVariantPayload[enumName][variantName] = payloadType
}

// EnumVariantPayloadOf 查詢某枚舉變體的載荷型別。回傳 (型別, 是否存在)；
// 型別為 "" 表示單元變體（無載荷）。多欄位變體回傳其合成結構體型別名
// （`<枚舉名>.<變體名>`）。
func (s *SemanticContext) EnumVariantPayloadOf(enumName, variantName string) (string, bool) {
	if s.EnumVariantPayload == nil {
		return "", false
	}
	if variants, ok := s.EnumVariantPayload[enumName]; ok {
		pt, ok := variants[variantName]
		return pt, ok
	}
	return "", false
}

// SetEnumVariantFields 登記某枚舉變體的載荷欄位型別列表（含欄位名由 Fields 提供）。
func (s *SemanticContext) SetEnumVariantFields(enumName, variantName string, types []string) {
	if s.EnumVariantFields == nil {
		s.EnumVariantFields = make(map[string]map[string][]string)
	}
	if s.EnumVariantFields[enumName] == nil {
		s.EnumVariantFields[enumName] = make(map[string][]string)
	}
	s.EnumVariantFields[enumName][variantName] = types
}

// EnumVariantFieldsOf 查詢某枚舉變體的載荷欄位型別列表（單元變體回傳空列表）。
func (s *SemanticContext) EnumVariantFieldsOf(enumName, variantName string) []string {
	if s.EnumVariantFields == nil {
		return nil
	}
	if variants, ok := s.EnumVariantFields[enumName]; ok {
		return variants[variantName]
	}
	return nil
}

// SetDeclared 標記變數已宣告。
func (s *SemanticContext) SetDeclared(name string) {
	if s.DeclaredVars == nil {
		s.DeclaredVars = make(map[string]bool)
	}
	s.DeclaredVars[name] = true
}

// IsDeclared 報告變數是否已宣告。
func (s *SemanticContext) IsDeclared(name string) bool {
	return s.DeclaredVars[name]
}

// ---- Resolver pass ----

// ResolveProgram 是“解析/语义分离”的独立语义 pass：在解析之后、lowering 之前执行。
// 它对每个带原始註解的節點收尾計算平台键与泛型参数，写入 side-table。
// 类型推断结果由 parser 增量写入 VarTypes/EnumVariants，此处无需重复计算。
// embed 文件读取（需真实文件系统与包根目录）由 ResolveEmbeds 在 transpiler 阶段执行。
func ResolveProgram(prog *Program) {
	if prog == nil || prog.Sem == nil {
		return
	}
	sem := prog.Sem
	for n, ns := range sem.nodeSem {
		entries := ns.RawAnnotations
		ns.Annotations = entries
		ns.PlatformKeys = ExtractPlatformKeys(entries)
		ns.GenericParams = extractGenericParams(entries)
		sem.nodeSem[n] = ns

		// 註解派生的函數泛型（#{generic=[K,V]}）物化回 FuncSignature.GenericParams：
		// 單態化管線（monomorphizeGenerics/cloneAndSubstitute）以該字段為具現化狀態
		// （具現化後清空），保留與顯式 <K,V> 泛型一致的處理路徑。
		if fd, ok := n.(*FunctionDefinition); ok {
			for _, name := range ns.GenericParams {
				fd.GenericParams = append(fd.GenericParams, &Identifier{Value: name})
			}
		}
	}
}
