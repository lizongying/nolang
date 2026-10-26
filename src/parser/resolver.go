// resolver.go — 解析与语义分离：语义副表（side-table）与 Resolver pass。
//
// 设计目标：parser 只产“语法 AST”（不含任何语义字段）；类型推断、注解、
// embed、平台键等全部语义信息集中存放于 SemanticContext（side-table），由独立的
// ResolveProgram pass 在解析之后、lowering 之前收尾计算。下游（lowering /
// transpiler / formatter / lsp）统一通过 prog.Sem.XxxOf(node) 读取语义信息，
// 不再直接访问 AST 节点上的语义字段。
//
// ⚠️ 排版 trivia（RTFlags / `{` 同行註釋）**不在**本副表：Stage 2（2026-10-23）
// 已遷到獨立的 `FmtTrivia` 表（`Program.FmtTrivia`，見 fmttrivia.go）——它只服務
// formatter，與語義無關。
//
// ⚠️ `NodeSemantics` 這個「以 AST 節點指針為鍵的語義副表」已於 Stage 7
// （2026-10-23）整體刪除：它的 18 個欄位分四期遷到 `FmtTrivia`（Stage 2）/
// `AnnTable`（Stage 3）/ `TypeFacts`（Stage 4）/ `NodeFacts`（Stage 5），殼被
// 留到最後一次無欄位搬運的機械刪除。其中 Stage 3 的 `AnnTable` 也已於
// 2026-10-26 刪除（註解回歸 AST，見 parser/annotations.go）—— 今日語義副表
// 只剩 `TypeFacts` / `NodeFacts` 兩張。
package parser

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
	// RTPipelineValue 標記「作為值」的短路管道 `x = A -> B -> V`（parser/stmt.go
	// 在 var/assignment 右側構造、刻意不設 RTStandalone 的 IfExpression）。
	// Formatter 據此原樣輸出 `cond -> body`；缺此標誌時會掉進通用 IfExpression
	// 打印分支輸出 `cond: { body }`，而該寫法會重新解析成 match-as-value，
	// 靜默改變語義（`x = 2 > 1 -> 42` 變 `x = 2 > 1: { 42 }` 後 x 變成布爾 1）。
	RTPipelineValue
	// RTEmptyArmDefault 標記值-match 的空臂（`->` 後不寫任何東西）在 lowering
	// 時被合成為「被匹配 option 內部型別的零值」語句。Formatter 據此把該臂還原成
	// 空臂輸出（否則 `->` 會被改寫成 `-> 0`，破壞 `no fmt` 冪等）。見 buildMatchDesugar（D4）。
	// 「值語境 vs 陳述語境」的區分記在 MatchArm.ValueCtx（自 2026-10-16 起已導出；
	// 但 SurfaceMatch 必在 ParseProgram 內被 lowering 展開，故它仍不會進任何 blob），
	// 不用 RTFlag。
	RTEmptyArmDefault
)

// SemanticContext 是解析/语义分离后的“副表”。
//
//   - anns / typeFacts / nodeFacts：三張以 AST 節點（指針，包裝為 Node 接口）
//     為鍵的語義事實表（見 anntable.go / typefacts.go / nodefacts.go）。
//   - VarTypes / EnumVariants / DeclaredVars：類型推斷結果
//     （原 parser.varDeclTypes / enumVariantNames / declaredVars）。nolang 是單遍
//     遞歸下降解析器，部分類型感知（如 match arm 分類、方法返回型別推斷）必須在
//     解析當下完成；這些推斷結果統一寫入本 side-table，而非散落在 parser 私有字段，
//     從而實現“語義結果集中存放、AST 節點零語義字段”。
type SemanticContext struct {
	// 🔴 註解**不在**本副表裡：`#{...}` 條目是語法的一部分，住在節點上
	// （`AnnotatedNode.Annotations`，見 parser/annotations.go）。`AnnotationsOf` /
	// `RawAnnotationsOf` / `PlatformKeysOf` / `GenericParamsOf` 都是對節點的純查詢。
	//
	// 歷史：Stage 3 曾在此放 `anns *AnnTable`（節點指針鍵的 side-table），
	// 導致尾隨註解只存在於表裡、AST 完全不承載，且 clone/merge/blob 各需一條
	// 手工通道。已整條刪除。

	// typeFacts 是 Program 級型別事實表（Stage 4，見 typefacts.go）：以 AST 節點
	// 為鍵的 `InferredType` / `TypeOverride` 兩件事實。
	// `Program.TypeFacts()` 是外部出口。
	typeFacts *TypeFacts

	// nodeFacts 是 Program 級節點事實表（Stage 5，見 nodefacts.go）：以 AST 節點
	// 為鍵的剩餘 9 項事實（embed / Callee / IndexInBounds / 溯源三元組 /
	// union 標記）。`Program.NodeFacts()` 是外部出口。
	nodeFacts *NodeFacts

	// captureFacts 是結構捕獲（option capture）的判定結果表（S3-b 第一步，見
	// capturefacts.go）：lowerer 為每個參與捕獲的 LetStatement 登記「內層型別 +
	// 各條 __cap_N 綁定」，供日後 tohir 直接產出 `KLet` + `KIf` 鏈（捕獲不再改寫
	// AST）。當下**只寫不讀**，無消費方。
	//
	// ⚠️ 刻意不進 blob（不加入 Serialize*/Restore*）：它是編譯期降糖產物，
	// 反序列化後由 lowerer 重算即可；不鏡像不造成行為差異。
	captureFacts *CaptureFacts

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

	// IfaceImpls：介面名 -> 實作者型別列表，由 build.devirtualizeInterfaceVars
	// 在攤平方法呼叫前從「合併後程式」推導並寫入。刻意掛在 Program 的語義副表
	// 而不是全域變數：它是**單一編譯單元的狀態**，若跨檔案共用，`no vet <dir>`
	// 的平行處理會讓前一個檔（例如引了 sqlite 驅動的 database-sql.no）的映射
	// 污染下一個檔（ffi-mysql.no），把它的介面呼叫攤平成根本不存在的
	// `sqlite.db-sqlite.query`，最後報成「'...' is not defined」假警報。
	IfaceImpls map[string][]string

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

	// UnionMembers 是「類型解構 match 偵測專用」的 union 別名表：
	// 別名名 → 直接成員型別名（如 num → [i32, i64, f64]），由 parseTypeAlias
	// 解析 `num = i32 | i64 | f64` 時填寫（parser/type.go:728-741）。
	//
	// 它原本只活在 *Parser.unionMembers 上，而 parser 實例在 ParseProgram 返回
	// 後即被丟棄 —— 今天沒問題，因為 lowering 就跑在同一個 parser 裡。一旦
	// 降糖搬到 H0（design doc §8 / S2），H0 只拿到 Program，這張表就整個消失，
	// isUnionType 一律回 false，所有 union match 靜默改走 `x === i32` 執行期
	// 比較 —— 不報錯、不炸，只是靜默產生錯誤的 HIR。type-match.no 實測即此。
	// 故這裡掛到語義副表匯出，與 IdxLocalTypes 同一模式。
	UnionMembers map[string][]string

	// ExternFuncSigs / ExternMethodSigs：跨文件 / 內置（std / 外部 C）的函數與
	// 方法簽名快照，由 transpiler 的 SetExternSignatures 注入同一份 map。
	//
	// H0 降糖的 scratch parser 只用 c.prog.Statements 重建 funcSignatures，拿不到
	// 內置簽名；而 lowerUnwrapAssign 的 ?= 解箱型別推斷（inferTypeFromCallExpr）
	// 正是查這張表。缺少時對內置呼叫（如 fs.fstat-size）無法推斷，unwrap 變數
	// 無型別 → MIR 轉出現 "move src slot" 崩潰（src/std/fs.no 的
	// read-bytes: `size ?= fs.fstat-size(.fd)` 即命中）。故把簽名快照掛到語義
	// 副表，供 scratchParser 合併進 funcSignatures / methodSignatures。
	// 與 IdxLocalTypes / UnionMembers 同一模式：語義事實從被丟棄的 Parser 實例
	// 匯出到可跨 H0 存活的 side-table。
	ExternFuncSigs   map[string][]string
	ExternMethodSigs map[string][]string
}

// NewSemanticContext 建立空語義副表。
func NewSemanticContext() *SemanticContext {
	return &SemanticContext{
		typeFacts:          NewTypeFacts(),
		nodeFacts:          NewNodeFacts(),
		VarTypes:           make(map[string]string),
		EnumVariants:       make(map[string][]string),
		DeclaredVars:       make(map[string]bool),
		EnumVariantPayload: make(map[string]map[string]string),
		EnumVariantFields:  make(map[string]map[string][]string),
		FuncVarTypes:       make(map[string]map[string]string),
		FuncDeclaredVars:   make(map[string]map[string]bool),
		IfaceImpls:         make(map[string][]string),
	}
}

// Merge 將另一份語義副表合併進本表（模塊合併時使用：merged program 匯集多個
// 模塊的 AST 節點，各節點的語義信息也必須匯集到同一張 side-table）。
// 四張 node-keyed 表以節點指針為鍵，不會衝突；名稱級映射（VarTypes 等）僅在
// 本表缺失時填入。
func (s *SemanticContext) Merge(other *SemanticContext) {
	if s == nil || other == nil {
		return
	}
	// 合併型別事實表／節點事實表／捕獲事實表（不覆蓋既有條目）。
	// 實例化／合併期三張表都可能為 nil（反序列化重建的 Sem 不帶表），
	// ensure 惰性建立。
	// ⚠️ 註解不在此列：它們住在 AST 節點上，隨 AST 深拷貝/拼接自動跟隨。
	s.typeFacts = s.typeFacts.ensure()
	s.typeFacts.Merge(other.typeFacts)
	s.nodeFacts = s.nodeFacts.ensure()
	s.nodeFacts.Merge(other.nodeFacts)
	// captureFacts 隨表帶出（S3-c 前沿 (a)）：merged/build 路徑的 checker
	// 護欄（optunuse/ovfhndld，見 checker.go 的 CaptureFactOf 斷言）與未來的
	// H0 直產都經 merged.Sem.CaptureFactOf 查表；少了這條合併，fresh parse
	// 寫入的捕獲判定在 merged 路徑恆查不到（CaptureFactOf 恆 nil）。
	s.captureFacts = s.captureFacts.ensure()
	s.captureFacts.Merge(other.captureFacts)
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
	// 合併介面實作對映（不覆蓋既有條目）。正常情況下 IfaceImpls 是在模組合併
	// 之後才由 devirtualizeInterfaceVars 寫入，這裡只是讓 Sem 的合併語意完整。
	for k, v := range other.IfaceImpls {
		if _, exists := s.IfaceImpls[k]; !exists {
			s.SetIfaceImpls(k, v)
		}
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
	// Merge union aliases (don't overwrite existing entries).
	for name, members := range other.UnionMembers {
		if _, exists := s.UnionMembers[name]; !exists {
			s.SetUnionMembers(name, members)
		}
	}
	// Merge cross-file / builtin signature snapshots (don't overwrite).
	// These drive the H0 scratch parser's ?= RHS type inference
	// (inferTypeFromCallExpr → returnTypeFromCallExpr); dropping them here
	// makes the unwrap temp lose its ?T type (void slot → MIR codegen
	// "move src slot" error on builtin method calls like str.to-i64).
	s.MergeExternSigs(other.ExternFuncSigs, other.ExternMethodSigs)
}

// MergeExternSigs merges cross-file / builtin function & method signature
// snapshots into this context with "don't overwrite existing" semantics,
// mirroring SetExternSignatures. Exposed separately from Merge so the
// transpiler can re-inject the authoritative std/extern signatures even when
// program.Sem lacks them (e.g. a restored Tier-1 AST cache never serialized
// them). Keeps the same local-priority rule: existing keys win.
func (s *SemanticContext) MergeExternSigs(funcSigs, methodSigs map[string][]string) {
	if s == nil {
		return
	}
	if len(funcSigs) > 0 {
		if s.ExternFuncSigs == nil {
			s.ExternFuncSigs = make(map[string][]string, len(funcSigs))
		}
		for k, v := range funcSigs {
			if _, exists := s.ExternFuncSigs[k]; !exists {
				s.ExternFuncSigs[k] = v
			}
		}
	}
	if len(methodSigs) > 0 {
		if s.ExternMethodSigs == nil {
			s.ExternMethodSigs = make(map[string][]string, len(methodSigs))
		}
		for k, v := range methodSigs {
			if _, exists := s.ExternMethodSigs[k]; !exists {
				s.ExternMethodSigs[k] = v
			}
		}
	}
}

// SetUnionMembers records a union alias and its direct member type names.
func (s *SemanticContext) SetUnionMembers(name string, members []string) {
	if s == nil || name == "" {
		return
	}
	if s.UnionMembers == nil {
		s.UnionMembers = make(map[string][]string)
	}
	s.UnionMembers[name] = members
}

// SetEmbedData 設定節點的嵌入字節（由 Resolver 的 embed 解析填入 side-table）。
//
// Stage 5 起轉發到 `NodeFacts`（`s.nodeFacts`），不再是已於 Stage 7 刪除的
// nodeSem 副表的欄位。
func (s *SemanticContext) SetEmbedData(n Node, data []byte) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetEmbedData(n, data)
}

// SetEmbedFiles sets the directory embed file map for a node.
// Stage 5 起轉發到 `NodeFacts`。
func (s *SemanticContext) SetEmbedFiles(n Node, files map[string][]byte) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetEmbedFiles(n, files)
}

// ---- 節點級語義（side-table）----

// SetRawAnnotations 記錄某節點的原始 #{...} 註解條目（parser 解析期呼叫）。
//
// 存儲是**節點本身**（`AnnotatedNode.Annotations`，語法即真相）。
// 覆蓋語義（整批替換）保持不變。nil 節點安全。
func (s *SemanticContext) SetRawAnnotations(n Node, entries []*AnnotationEntry) {
	setAnnotationsOf(n, entries)
}

// SetIndexInBounds records that an IndexExpression's read is proven in bounds.
// The fact is semantic metadata and never changes the syntax AST.
//
// Stage 5 起轉發到 `NodeFacts`（`s.nodeFacts`）。`yes == false` 只清既有條目、
// 不新建的不對稱語義由表層保留（見 `NodeFacts.SetIndexInBounds`）。
func (s *SemanticContext) SetIndexInBounds(n Node, yes bool) {
	if s == nil {
		return
	}
	if !yes {
		s.nodeFacts.SetIndexInBounds(n, false)
		return
	}
	s.nodeFacts.ensure().SetIndexInBounds(n, true)
}

// IndexIsInBounds reports the bounds analyzer's proof for an AST node. Nil-safe.
func (s *SemanticContext) IndexIsInBounds(n Node) bool {
	if s == nil {
		return false
	}
	return s.nodeFacts.IndexInBoundsOf(n)
}

// SetInferredType 為一個 LetStatement 記錄 lowering 推斷出的 ?T 型別字串。
// 這是純語義事實：不會改寫 AST，可在 SkipLowering 路徑下仍提供同一份證明。
// empty 字串會被忽略（nil receiver / nil 節點安全）。
func (s *SemanticContext) SetInferredType(n Node, typ string) {
	if s == nil {
		return
	}
	s.typeFacts.ensure().SetInferred(n, typ)
}

// InferredType 回傳該節點的推斷 ?T 型別字串（如 `"?i64"`），未記錄時回 ""。
// Nil receiver / nil 節點安全。
func (s *SemanticContext) InferredType(n Node) string {
	if s == nil {
		return ""
	}
	return s.typeFacts.InferredOf(n)
}

// HasInferredType 報告節點是否帶推斷的 ?T 型別（Nil-safe）。
func (s *SemanticContext) HasInferredType(n Node) bool {
	return s.InferredType(n) != ""
}

// SetTypeOverrideOf 記錄整程式裝配為節點定下的**有效型別覆蓋**（具現化 /
// 具象化結果）。覆蓋語義：優先於節點宣告的 Type。empty 字串會被忽略
// （nil receiver / nil 節點安全）。
func (s *SemanticContext) SetTypeOverrideOf(n Node, typ string) {
	if s == nil {
		return
	}
	s.typeFacts.ensure().SetOverride(n, typ)
}

// TypeOverrideOf 回傳節點的有效型別覆蓋字串，未記錄時回 ""。nil receiver /
// nil 節點安全。
func (s *SemanticContext) TypeOverrideOf(n Node) string {
	if s == nil {
		return ""
	}
	return s.typeFacts.OverrideOf(n)
}

// TypeFactsTab 回傳本副表的型別事實表（外部出口：blob 序列化 / clone / 合併用）。
// 命名加 Tab 後綴是因為 `Program.TypeFacts()` 與 `SemanticContext` 上的方法名
// 空間不同——但 `TypeFacts` 已被 `TypeFact` 的複數型別名佔用，故避免混淆。
// nil receiver 安全，返回 nil。
func (s *SemanticContext) TypeFactsTab() *TypeFacts {
	if s == nil {
		return nil
	}
	return s.typeFacts
}

// NodeFactsTab 回傳本副表的節點事實表（外部出口：blob 序列化 / clone / 合併用）。
// 命名加 Tab 後綴的原因同 `TypeFactsTab`：`NodeFacts` 已被複數型別名佔用。
// nil receiver 安全，返回 nil。
func (s *SemanticContext) NodeFactsTab() *NodeFacts {
	if s == nil {
		return nil
	}
	return s.nodeFacts
}

// SetModuleConstOf 標記一個 LetStatement 是「模組級常量」（可參與常量傳播）。
//
// 這是裝配（H1）寫入的語義事實，不是語法：原 AST 欄位
// `LetStatement.IsModuleConst` 由整程式合併 pass 就地寫入，違反嚴格範圍
// 不變量（§1.4）。S5 把它搬進 side-table。寫入者：
//   - build/transpiler.go 的模組合併（use 展開 + 自動載入 std 模組體）
//   - build/transpiler.go 的可變全局變量回收（`false` 覆寫）
//
// 讀取者：parser/tohir.go 的 KLet flags、checker.ResolveModuleConstants、
// parser/dump 的 moduleConst 列。
func (s *SemanticContext) SetModuleConstOf(n Node, yes bool) {
	if s == nil {
		return
	}
	if !yes {
		s.nodeFacts.SetModuleConst(n, false)
		return
	}
	s.nodeFacts.ensure().SetModuleConst(n, true)
}

// ModuleConstOf 回傳該節點是否被標記為模組級常量。Nil-safe。
func (s *SemanticContext) ModuleConstOf(n Node) bool {
	if s == nil || n == nil {
		return false
	}
	return s.nodeFacts.ModuleConstOf(n)
}

// SetSourceFileOf 記錄節點的來源檔路徑。Nil-safe（`s == nil || n == nil` 早退）。
//
// 這是裝配（H1）寫入的語義事實，不是語法：原 AST 欄位
// `CommentedNode.SourceFile` 由整程式合併 pass 就地寫入，違反嚴格範圍
// 不變量（§1.4）。S5 把它搬進 side-table。
//
// 注意舊 AST 欄位的 `SetSourceFile` 只對「嵌有 CommentedNode 的語句型別」
// 生效（其它型別靜默 no-op）。side-table 版對任何 Node 都生效 —— 包括
// 表達式與合成節點。這正是需要的：`SetSourceFileDeep` 要遞迴到
// FunctionLiteral / IfExpression 臂內的語句，而那些語句同樣嵌 CommentedNode，
// 舊版的型別開關其實漏掉了它們（靠 `setSourceFileDeepOnExpr` 才補上）。
func (s *SemanticContext) SetSourceFileOf(n Node, file string) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetSourceFile(n, file)
}

// SourceFileOf 回傳節點的來源檔路徑；未記錄時回空字串。Nil-safe。
func (s *SemanticContext) SourceFileOf(n Node) string {
	if s == nil || n == nil {
		return ""
	}
	return s.nodeFacts.SourceFileOf(n)
}

// SetModuleOwnerOf 記錄節點的來源模組短名。Nil-safe。
func (s *SemanticContext) SetModuleOwnerOf(n Node, owner string) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetModuleOwner(n, owner)
}

// ModuleOwnerOf 回傳節點的來源模組短名；未記錄（主程式自身語句）回空字串。
// Nil-safe。
func (s *SemanticContext) ModuleOwnerOf(n Node) string {
	if s == nil || n == nil {
		return ""
	}
	return s.nodeFacts.ModuleOwnerOf(n)
}

// SetVariadicUnionOf 記錄函式的 union variadic 標記（checker.ValidateUnionTypes
// 寫入）。Nil-safe。
func (s *SemanticContext) SetVariadicUnionOf(n Node, union string) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetVariadicUnion(n, union)
}

// VariadicUnionOf 回傳函式的 union variadic 標記；未記錄回空字串。Nil-safe。
func (s *SemanticContext) VariadicUnionOf(n Node) string {
	if s == nil || n == nil {
		return ""
	}
	return s.nodeFacts.VariadicUnionOf(n)
}

// SetGenericUnionOf 記錄函式的單一 union 標記（checker.ValidateUnionTypes
// 寫入）。Nil-safe。
func (s *SemanticContext) SetGenericUnionOf(n Node, union string) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetGenericUnion(n, union)
}

// GenericUnionOf 回傳函式的單一 union 標記；未記錄回空字串。Nil-safe。
func (s *SemanticContext) GenericUnionOf(n Node) string {
	if s == nil || n == nil {
		return ""
	}
	return s.nodeFacts.GenericUnionOf(n)
}

// SetSourceFileDeep 遞迴地把來源檔路徑寫到語句本身與其所有巢狀語句上
// （函式體、區塊、for 的 Init/Body、if/else 臂、群組表達式），並下潛到
// LetStatement.Value / ExpressionStatement.Expression 這兩個唯一能攜帶
// 巢狀語句的表達式掛載點。
//
// 為什麼必須 Deep：合併後只有頂層語句被淺標記時，函式體內的語句
// SourceFile 留空，lint 端拿不到來源檔就回退成主檔路徑，而 Line/Column
// 取自節點本身（模組座標）——報出 `main.no:1098` 這種主檔根本沒有的行號。
//
// 呼叫點全部在整程式合併 pass（build/transpiler.go 的 processUseAndMerge、
// build/transpiler_stdload.go 的自動載入），傳入的是**合併後存活的
// entry program 的 Sem**：模組自己的 Sem 隨該模組的 Program 一起被丟棄，
// 寫在那裡等於沒寫。
func (s *SemanticContext) SetSourceFileDeep(stmt Statement, file string) {
	if s == nil || stmt == nil {
		return
	}
	s.SetSourceFileOf(stmt, file)
	switch st := stmt.(type) {
	case *FunctionDefinition:
		s.SetSourceFileDeepOnBlock(st.Body, file)
	case *ForStatement:
		s.SetSourceFileDeep(st.Init, file)
		s.SetSourceFileDeepOnBlock(st.Body, file)
	case *BlockStatement:
		s.SetSourceFileDeepOnBlock(st, file)
	case *LetStatement:
		s.setSourceFileDeepOnExpr(st.Value, file)
	case *ExpressionStatement:
		s.setSourceFileDeepOnExpr(st.Expression, file)
	}
}

// SetSourceFileDeepOnBlock 對區塊內每條語句執行 Deep 標記。Nil-safe。
func (s *SemanticContext) SetSourceFileDeepOnBlock(b *BlockStatement, file string) {
	if s == nil || b == nil {
		return
	}
	for _, inner := range b.Statements {
		s.SetSourceFileDeep(inner, file)
	}
}

// setSourceFileDeepOnExpr 下潛能攜帶巢狀語句的表達式：IfExpression 的
// Consequence/Alternative 臂、GroupedExpression。
//
// ⚠️ 已知缺口（2026-10-26 發現，**未**在本輪修）：`*FunctionLiteral` 的 Body 不在
// 這裡下潛。實測本語料裡 `name = (params) (results) { ... }` 全部解析成
// `*FunctionDefinition`（由 `SetSourceFileDeep` 的第一個 case 覆蓋），所以目前
// 沒有可觀測後果——補上這一臂對 `./parser ./checker ./build` 全量測試零差異。
// 但模組頂層的**匿名**函式字面量（值型別是 FunctionLiteral 而非 FunctionDefinition）
// 仍會漏標來源檔，lint 端會把它的行號歸到主檔。真要修需先有可證偽的語料，
// 不宜夾帶進註解重構這一輪。
func (s *SemanticContext) setSourceFileDeepOnExpr(e Expression, file string) {
	if s == nil || e == nil {
		return
	}
	switch x := e.(type) {
	case *IfExpression:
		s.SetSourceFileDeepOnBlock(x.Consequence, file)
		s.SetSourceFileDeepOnBlock(x.Alternative, file)
	case *GroupedExpression:
		s.setSourceFileDeepOnExpr(x.Expression, file)
	}
}

// SetCallee 記錄某個 CallExpression 被整程式改寫成了哪一種呼叫（見 CalleeKind）。
// 由 build/transpiler.go 的 resolveMethodCall 在攤平／改名當下寫入。
// nil receiver / nil 節點安全。
func (s *SemanticContext) SetCallee(n Node, k CalleeKind) {
	if s == nil {
		return
	}
	s.nodeFacts.ensure().SetCallee(n, k)
}

// CalleeOf 回報節點被改寫成的呼叫種類；從未記錄時返回 CalleeUnknown
// （源碼原樣的呼叫，或未經本機制改寫者）。nil receiver 安全。
func (s *SemanticContext) CalleeOf(n Node) CalleeKind {
	if s == nil || n == nil {
		return CalleeUnknown
	}
	return s.nodeFacts.CalleeOf(n)
}

// AnnotationsOf 返回節點的註解條目（無則 nil）。
//
// 🔴 語法即真相：條目住在節點本身（`AnnotatedNode.Annotations`），本方法是
// 對節點的純查詢，不再有 side-table。因此 receiver 只為保持既有呼叫點簽名而
// 存在，**與 `s` 是否為 nil 無關**（舊實現 `s == nil` 時返回 nil，會把「節點
// 上有註解但 sem 為空」誤判成無註解）。
//
// 與 `RawAnnotationsOf` 恆等（兩者讀同一份節點存儲）：舊的
// `AnnotationsOf(n) || RawAnnotationsOf(n)` 雙查是歷史包袱，保留不影響正確性。
func (s *SemanticContext) AnnotationsOf(n Node) []*AnnotationEntry {
	return annotationsOf(n)
}

// SetAnnotations 直接設定節點的註解條目。
// 供工具（如 `no fmt --fix=overflow`）在 ParseProgram 之後、不重跑
// ResolveProgram（重跑會把 #{generic=[K,V]} 等泛型參數重複附加到函式定義）
// 的前提下，把新增的註解條目（如 overflow=wrap）寫入節點，使 formatter
// 的 attachedAnnotations 能讀到並輸出。nil 節點安全。
func (s *SemanticContext) SetAnnotations(n Node, entries []*AnnotationEntry) {
	setAnnotationsOf(n, entries)
}

// RawAnnotationsOf 返回節點在「解析期」由 #{...} 收集的原始註解條目（無則 nil）。
// 見 `AnnotationsOf` 的註解：兩者讀同一份節點存儲，恆等。
func (s *SemanticContext) RawAnnotationsOf(n Node) []*AnnotationEntry {
	return annotationsOf(n)
}

// RawAnnotationsOf 是 Parser 級便利訪問器，轉發到語義副表，供呼叫方在
// ResolveProgram 之前讀取 `#{...}` 的原始註解條目。nil 安全。
func (p *Parser) RawAnnotationsOf(n Node) []*AnnotationEntry {
	if p == nil {
		return nil
	}
	return p.sem.RawAnnotationsOf(n)
}

// PlatformKeysOf 返回節點的平台註解 key（無則 nil）。
//
// 純函數：從節點上的註解條目即時算出（`ExtractPlatformKeys`），無物化欄位。
func (s *SemanticContext) PlatformKeysOf(n Node) []string {
	return ExtractPlatformKeys(annotationsOf(n))
}

// GenericParamsOf 返回節點的泛型參數名（無則 nil）。
// 純函數（見 `PlatformKeysOf` 註解）。
func (s *SemanticContext) GenericParamsOf(n Node) []string {
	return extractGenericParams(annotationsOf(n))
}

// EmbedDataOf 返回節點的嵌入字節（無則 nil）。nil receiver 安全。
// Stage 5 起轉發到 `NodeFacts`。
func (s *SemanticContext) EmbedDataOf(n Node) []byte {
	if s == nil {
		return nil
	}
	return s.nodeFacts.EmbedDataOf(n)
}

// EmbedFilesOf returns the directory embed file map for a node (nil if none).
// Stage 5 起轉發到 `NodeFacts`。
func (s *SemanticContext) EmbedFilesOf(n Node) map[string][]byte {
	if s == nil {
		return nil
	}
	return s.nodeFacts.EmbedFilesOf(n)
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

// SetIfaceImpls 記錄「介面名 -> 實作者型別列表」（供方法呼叫攤平時的
// 單一實作者回退）。範圍是單一 Program，禁止改成全域：見 IfaceImpls 的註解。
func (s *SemanticContext) SetIfaceImpls(iface string, impls []string) {
	if s.IfaceImpls == nil {
		s.IfaceImpls = make(map[string][]string)
	}
	s.IfaceImpls[iface] = impls
}

// IfaceImplsOf 回傳介面的實作者列表；未記錄（或無語義副表）時回傳 nil。
func (s *SemanticContext) IfaceImplsOf(iface string) []string {
	return s.IfaceImpls[iface]
}

// IsDeclared 報告變數是否已宣告。
func (s *SemanticContext) IsDeclared(name string) bool {
	return s.DeclaredVars[name]
}

// ---- Resolver pass ----

// ResolveProgram 是“解析/语义分离”的独立语义 pass：在解析之后、lowering 之前执行。
// 类型推断结果由 parser 增量写入 VarTypes/EnumVariants，此处无需重复计算。
// embed 文件读取（需真实文件系统与包根目录）由 ResolveEmbeds 在 transpiler 阶段执行。
//
// Stage 3 起本 pass 只剩一件事：把註解派生的函數泛型（#{generic=[K,V]}）物化回
// `FuncSignature.GenericParams`。原本它還負責把 RawAnnotations 拷貝成
// Annotations、並算出 PlatformKeys / GenericParams 三個物化欄位——那三件事
// 隨著註解改為節點原生承載全部消失：Annotations 與 RawAnnotations 本就同讀一份
// 存儲，後兩者降為純函數（`PlatformKeysOf` / `GenericParamsOf`）。
func ResolveProgram(prog *Program) {
	if prog == nil || prog.Sem == nil {
		return
	}
	// 註解住在節點上，故這裡走一遍樹（而不是遍歷一張表）。seen 去重：
	// walkProgramNodes 按 **occurrence** 走訪，共享節點會出現多次。
	seen := make(map[Node]bool)
	walkProgramNodes(prog, func(n Node) {
		if seen[n] {
			return
		}
		seen[n] = true
		// 註解派生的函數泛型（#{generic=[K,V]}）物化回 FuncSignature.GenericParams：
		// 單態化管線（monomorphizeGenerics/cloneAndSubstitute）以該字段為具現化狀態
		// （具現化後清空），保留與顯式 <K,V> 泛型一致的處理路徑。
		fd, ok := n.(*FunctionDefinition)
		if !ok {
			return
		}
		for _, name := range extractGenericParams(annotationsOf(fd)) {
			fd.GenericParams = append(fd.GenericParams, &Identifier{Value: name})
		}
	})
}
