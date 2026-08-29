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
}

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

	// FuncVarTypes stores per-function-local variable types, keyed by
	// function name → variable name → type. This prevents same-named
	// locals in different functions (e.g. `r` in parse-i64 and parse-f64)
	// from colliding in the global VarTypes map during lowering.
	FuncVarTypes     map[string]map[string]string
	FuncDeclaredVars map[string]map[string]bool
}

// NewSemanticContext 建立空語義副表。
func NewSemanticContext() *SemanticContext {
	return &SemanticContext{
		nodeSem:          make(map[Node]*NodeSemantics),
		VarTypes:         make(map[string]string),
		EnumVariants:     make(map[string][]string),
		DeclaredVars:     make(map[string]bool),
		FuncVarTypes:     make(map[string]map[string]string),
		FuncDeclaredVars: make(map[string]map[string]bool),
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
