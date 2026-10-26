package parser

// nodefacts.go — Program 級節點事實表（Stage 5 of the THIR plan,
// docs/design/nolang-parser-lowering-invariants.md §3.5）。
//
// 背景：`NodeSemantics` 原本有 12 個欄位，Stage 3 遷走註解類 4 個、Stage 4 遷走
// 型別類 3 個，剩下的 9 個是「改寫事實 / 溯源 / union / embed」四類：
//
//   - `Callee`：整程式 pass 攤平方法呼叫／衝突改名後留下的**改寫事實**；
//   - `IndexInBounds`：**解析期**的證明（bounds 分析器寫入）；
//   - `ModuleConst` / `SourceFile` / `ModuleOwner`：模組合併期的**溯源**三元組；
//   - `VariadicUnion` / `GenericUnion`：checker 的 union 標記；
//   - `EmbedData` / `EmbedFiles`：`#{embed=...}` 裝配期讀檔結果。
//
// 搬出 nodeSem 後 `NodeSemantics` 成為空殼 —— 這是刻意的：先把欄位全部搬走、
// 空殼留到 Stage 7 純刪（已完成），中途任一檔 REGRESS 都能二分出是哪一期引入的。
//
// 🔴 為什麼這 9 個欄位裝**一張**表而不是按類別拆成三張：`FmtTrivia`（Stage 2）/
// `TypeFacts`（Stage 4）之所以分表，是因為它們的寫入階段或 blob 策略不同
// （FmtTrivia 不進 blob）。（Stage 3 的第三張表 `AnnTable` 已於 2026-10-26
// 整條刪除 —— `#{...}` 是語法，條目住在 AST 節點上，不該是 side-table。）剩餘這 9 個
// 欄位的寫入階段雖然橫跨解析期／checker／裝配期，但**沒有一個有「不進 blob」
// 的理由**（C9 明確點名 `embed data` / `IndexInBounds` / `ModuleConst` /
// `SourceFile` / `ModuleOwner` 五項為 MUST bump），拆分只會多出三張要各自接
// 序列化、Clone、Merge 的表，換不到任何隔離收益。故單表裝全部 9 欄。
//
// 🔴 本輪是**純重構**：`SemanticContext` 上 18 個訪問器的簽名全部不變、方法體
// 改為轉發，故下游讀寫點（checker 約 50 處 `SourceFileOf`、build、tohir、
// dump、mir）零改動。

// NodeFacts 存放一批節點的節點事實，以 AST 節點指針為鍵。
//
// entries 刻意未導出：這樣所有以反射遍歷 Program 欄位的 walker
// （astcache.go 的 occurrence 走查、sugar_seam_test / sugar_kinds_test 的
// 普查）都看不見它，無需在三處跳過名單裡各加一次。
type NodeFacts struct {
	entries map[Node]*NodeFact
}

// NodeFact 是單一節點的事實條目。
//
// 與 `NodeFacts` 相反，本型別**必須導出**：Clone / Merge 要按欄位操作它。
type NodeFact struct {
	// EmbedData 是 `#{embed=<file>}` 的檔案字節。由裝配期
	// （build/transpiler_inject.go）讀真實檔案系統後寫入 —— 發生在快取
	// **之後**，但 C9 仍把它列為 MUST bump：Tier-0 std blob 若與程式碼
	// 版本錯位，`embed` 的內容會是另一個版本的檔案。
	EmbedData []byte

	// EmbedFiles 是 `#{embed=<dir>}` 的目錄嵌入對映（相對路徑 → 內容）。
	//
	// ⚠️ 這是整張表裡唯一帶 map 的條目欄位：gob 對 map 按隨機迭代序編碼，
	// 直接進線會讓 blob 不可重現（genstdsig 的「內容未變就不重寫」守衛永遠
	// 不觸發、兩次建置產出不同的 `no` 二進位）。故 `NodeFact` 在
	// astcache_det.go 有對應的 `detNodeFact` 影子把它排成有序 slice。
	EmbedFiles map[string][]byte

	// Callee 記錄「這個 CallExpression 被改寫成了什麼」（見 CalleeKind）。
	//
	// 用途：整程式 pass 會把方法呼叫 `recv.m(args)` 攤平為自由函式呼叫
	// `Type.m(recv, args)`（接收者 unshift 進 Arguments，被呼叫者換成
	// Identifier），語法上的 DotExpression 隨之消失。攤平後「第一個實參曾
	// 是接收者」這件事在 AST 上已不可自證，過去只能靠「被呼叫者的名字是否
	// 撞上某個方法定義」反推 —— 那既會漏（同名 alias）也會誤（自由函式被
	// 誤認）。改寫時在此留一筆，就把這個事實變成**顯式記錄**。
	//
	// 寫入者（全部在整程式 pass 中，皆早於 RunAllLints）：
	//   - build/transpiler.go 的 resolveMethodCall（攤平方法呼叫，兩個分支）
	//     → CalleeMethod。
	//   - checker.go 的 resolveModuleCallsInExpr（衝突函式改名 `module.fn`）
	//     → CalleeModuleFn。
	// 讀取者：checker（collectAssignedNamesInExpr）。
	Callee CalleeKind

	// IndexInBounds 是 bounds 分析器對該 IndexExpression 的證明結果。
	//
	// 寫入者：解析期的 bounds 分析（`SetIndexInBounds`）。讀取者：checker 的
	// 索引守衛豁免、build 的 `#{index-out}` 決策。
	//
	// ⚠️ C9 明確點名 `IndexInBounds` 為 MUST bump 的解析期 side table。
	IndexInBounds bool

	// ModuleConst 標記 LetStatement 為「模組級常量」（可參與常量傳播）。
	//
	// 寫入者：build/transpiler.go 的模組合併（use 展開 + 自動載入 std 模組體）
	// 與可變全局變量回收（以 `false` 覆寫）。
	// 讀取者：parser/tohir.go 的 KLet flags、checker.ResolveModuleConstants、
	// parser/dump 的 moduleConst 列。
	ModuleConst bool

	// SourceFile 記錄節點來源的原始碼檔案路徑。
	//
	// 這個欄位的存在理由不是「省一個 map」而是**歸因**：模組合併後
	// `Line/Column` 取自節點自身（模組座標），若診斷的 `File` 一律用主檔
	// 路徑，就會報出 `main.no:1098` 這種主檔根本沒有的行號。故每個被合併
	// 進 entry program 的語句（含函式體內的巢狀語句，見
	// SemanticContext.SetSourceFileDeep）都必須自帶來源檔。
	//
	// 主程式自身的頂層語句也會被標記（`t.sourcePath`），使型別檢查期就能
	// 按檔名豁免 std 實作檔（vec.no / byte.no / str.no）的棄用告警。
	//
	// 寫入者：整程式合併 pass（build/transpiler.go 的 processUseAndMerge、
	// build/transpiler_stdload.go 的自動載入）。讀取者：checker 約 50 處。
	SourceFile string

	// ModuleOwner 記錄節點來源的模組短名（如 "sql" / "path"）。空字串 =
	// 主程式自身語句。
	//
	// 讀取者：prefixCollidingFunctions（同名頂層函式衝突時只有模組側定義
	// 改名為 `module.fn`）、tohir 的 FuncOwners、checker 的模組函式解析。
	ModuleOwner string

	// VariadicUnion 記錄「variadic 參數的元素型別是 union alias」時的
	// union 名稱（供 codegen 單態化）。空字串 = 非 union variadic。
	//
	// 寫入者：checker.ValidateUnionTypes（每個被驗證的 program 各寫一份，
	// monomorphizeUnions 會在合併後的 program 上重跑一次）。
	// 讀取者：tohir 的 funcLike（HIR S2 槽）、monomorphizeUnions 的範本判定、
	// dump 的 Variadic{union: …} 列。
	VariadicUnion string

	// GenericUnion 記錄「非 variadic 函式的參數/結果整個只用同一個 union
	// alias」時的 union 名稱（如 abs = (a num) (r num)）。
	//
	// 讀取者：tohir 的 funcLike（發射 KUnionName）、monomorphizeUnions 的
	// 範本判定。克隆體（cloneUnionVariant）**不繼承** —— 實例化後即具體函式，
	// 新節點天然無條目即空字串，等同舊版「克隆後清空」。這正是本表必須以
	// **AST 指針**為鍵的原因：若改用函式名之類的穩定鍵，`cloneUnionVariant`
	// 把 `fd.Name` 改成 `max__i64` 後仍會命中同一條目。
	GenericUnion string
}

// NewNodeFacts 建立一張空的節點事實表。
func NewNodeFacts() *NodeFacts {
	return &NodeFacts{entries: make(map[Node]*NodeFact)}
}

// GobEncode / GobDecode 讓本表在 gob 面前是「什麼都不帶」的空殼。
//
// 為什麼需要這兩個空方法：本表**沒有導出字段**（entries 刻意未導出，好讓反射
// walker 看不見它），而 gob 對「無導出字段的型別」會直接報錯
// `gob: type parser.NodeFacts has no exported fields`。
//
// ⚠️ 這**不代表**節點事實進 blob —— 它們根本不進（見 semviews.go 檔案頭）。
// 解碼後的樹上沒有任何節點事實，由 `BuildSemViews` 呼叫 `prepareInBoundsMetadata`
// 重算；Trace/Callee/embed 這類裝配期事實則本來就寫在快取**之後**。這對空方法只
// 服務「直接 gob 編碼 `*parser.Program`」的路徑（實測只有 mmast_test.go 為模擬
// 上一代格式而這麼做）。
func (t *NodeFacts) GobEncode() ([]byte, error) { return nil, nil }

func (t *NodeFacts) GobDecode([]byte) error { return nil }

// ensure 取得（必要時建立）本表；nil receiver 會建一張新表返回。
func (t *NodeFacts) ensure() *NodeFacts {
	if t == nil {
		return NewNodeFacts()
	}
	if t.entries == nil {
		t.entries = make(map[Node]*NodeFact)
	}
	return t
}

// fact 取出 n 的條目（不存在則建立）。呼叫前必須已排除 nil 表。
func (t *NodeFacts) fact(n Node) *NodeFact {
	if t.entries == nil {
		t.entries = make(map[Node]*NodeFact)
	}
	f, ok := t.entries[n]
	if !ok {
		f = &NodeFact{}
		t.entries[n] = f
	}
	return f
}

// ---- embed ----

// SetEmbedData 設定節點的嵌入字節（由 Resolver 的 embed 解析填入 side-table）。
func (t *NodeFacts) SetEmbedData(n Node, data []byte) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).EmbedData = data
}

// EmbedDataOf 回傳節點的嵌入字節（無則 nil）。nil 安全。
func (t *NodeFacts) EmbedDataOf(n Node) []byte {
	if t == nil || n == nil {
		return nil
	}
	if f, ok := t.entries[n]; ok {
		return f.EmbedData
	}
	return nil
}

// SetEmbedFiles 設定節點的目錄嵌入對映。nil 安全。
func (t *NodeFacts) SetEmbedFiles(n Node, files map[string][]byte) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).EmbedFiles = files
}

// EmbedFilesOf 回傳節點的目錄嵌入對映（無則 nil）。nil 安全。
func (t *NodeFacts) EmbedFilesOf(n Node) map[string][]byte {
	if t == nil || n == nil {
		return nil
	}
	if f, ok := t.entries[n]; ok {
		return f.EmbedFiles
	}
	return nil
}

// ---- 改寫事實 ----

// SetCallee 記錄某個 CallExpression 被整程式改寫成了哪一種呼叫（見 CalleeKind）。
// nil receiver / nil 節點安全。
func (t *NodeFacts) SetCallee(n Node, k CalleeKind) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).Callee = k
}

// CalleeOf 回報節點被改寫成的呼叫種類；從未記錄時返回 CalleeUnknown
// （源碼原樣的呼叫，或未經本機制改寫者）。nil receiver 安全。
func (t *NodeFacts) CalleeOf(n Node) CalleeKind {
	if t == nil || n == nil {
		return CalleeUnknown
	}
	if f, ok := t.entries[n]; ok {
		return f.Callee
	}
	return CalleeUnknown
}

// SetIndexInBounds 記錄某個 IndexExpression 的讀取已被證明在界內。
//
// ⚠️ 與其它 setter 不對稱：`yes == false` 時**只清既有條目、不新建** —— 原
// `SemanticContext.SetIndexInBounds` 的守衛必須逐字保留（條目集合會影響 blob
// 版面與合併結果：Merge 視「鍵已存在」為「不覆蓋」）。
func (t *NodeFacts) SetIndexInBounds(n Node, yes bool) {
	if t == nil || n == nil {
		return
	}
	if !yes {
		if f, ok := t.entries[n]; ok {
			f.IndexInBounds = false
		}
		return
	}
	t.fact(n).IndexInBounds = true
}

// IndexInBoundsOf 回傳 bounds 分析器的證明結果（無條目時 false）。nil 安全。
func (t *NodeFacts) IndexInBoundsOf(n Node) bool {
	if t == nil || n == nil {
		return false
	}
	if f, ok := t.entries[n]; ok {
		return f.IndexInBounds
	}
	return false
}

// ---- 溯源 ----

// SetModuleConst 標記節點是否為「模組級常量」。
//
// ⚠️ 與 `SetIndexInBounds` 同款不對稱：`yes == false` 時只清既有條目、不新建。
func (t *NodeFacts) SetModuleConst(n Node, yes bool) {
	if t == nil || n == nil {
		return
	}
	if !yes {
		if f, ok := t.entries[n]; ok {
			f.ModuleConst = false
		}
		return
	}
	t.fact(n).ModuleConst = true
}

// ModuleConstOf 回傳節點是否被標記為模組級常量。nil 安全。
func (t *NodeFacts) ModuleConstOf(n Node) bool {
	if t == nil || n == nil {
		return false
	}
	if f, ok := t.entries[n]; ok {
		return f.ModuleConst
	}
	return false
}

// SetSourceFile 記錄節點的來源檔路徑。nil 安全。
func (t *NodeFacts) SetSourceFile(n Node, file string) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).SourceFile = file
}

// SourceFileOf 回傳節點的來源檔路徑；未記錄時回空字串。nil 安全。
func (t *NodeFacts) SourceFileOf(n Node) string {
	if t == nil || n == nil {
		return ""
	}
	if f, ok := t.entries[n]; ok {
		return f.SourceFile
	}
	return ""
}

// SetModuleOwner 記錄節點的來源模組短名。nil 安全。
func (t *NodeFacts) SetModuleOwner(n Node, owner string) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).ModuleOwner = owner
}

// ModuleOwnerOf 回傳節點的來源模組短名；未記錄（主程式自身語句）回空字串。
// nil 安全。
func (t *NodeFacts) ModuleOwnerOf(n Node) string {
	if t == nil || n == nil {
		return ""
	}
	if f, ok := t.entries[n]; ok {
		return f.ModuleOwner
	}
	return ""
}

// ---- union ----

// SetVariadicUnion 記錄函式的 union variadic 標記
// （checker.ValidateUnionTypes 寫入）。nil 安全。
func (t *NodeFacts) SetVariadicUnion(n Node, union string) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).VariadicUnion = union
}

// VariadicUnionOf 回傳函式的 union variadic 標記；未記錄回空字串。nil 安全。
func (t *NodeFacts) VariadicUnionOf(n Node) string {
	if t == nil || n == nil {
		return ""
	}
	if f, ok := t.entries[n]; ok {
		return f.VariadicUnion
	}
	return ""
}

// SetGenericUnion 記錄函式的單一 union 標記
// （checker.ValidateUnionTypes 寫入）。nil 安全。
func (t *NodeFacts) SetGenericUnion(n Node, union string) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).GenericUnion = union
}

// GenericUnionOf 回傳函式的單一 union 標記；未記錄回空字串。nil 安全。
func (t *NodeFacts) GenericUnionOf(n Node) string {
	if t == nil || n == nil {
		return ""
	}
	if f, ok := t.entries[n]; ok {
		return f.GenericUnion
	}
	return ""
}

// ---- 整表操作 ----

// Len 回傳條目數（測試用：反空洞斷言需要證明表真的被填過）。
func (t *NodeFacts) Len() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Clone 深拷貝條目並把鍵經 remap 換到副本節點（CloneProgram 用）。
// remap 未收錄的鍵原樣保留 —— 與 `remapSem` 對另兩張表的處理同口徑
// （指向未被拷貝的孤立節點）。nil receiver 安全，返回 nil。
//
// ⚠️ `EmbedFiles` 逐條深拷貝：遷移前 nodeSem 的克隆是**共享** `*NodeSemantics`
// 指針（連條目都不拷），這裡多拷一層 map 是刻意收緊——沒有任何寫入點就地
// 改這個 map（`SetEmbedFiles` 是整批替換），故不改變可觀測行為。
func (t *NodeFacts) Clone(remap func(Node) Node) *NodeFacts {
	if t == nil || len(t.entries) == 0 {
		return nil
	}
	out := &NodeFacts{entries: make(map[Node]*NodeFact, len(t.entries))}
	for n, f := range t.entries {
		key := n
		if remap != nil {
			if nn := remap(n); nn != nil {
				key = nn
			}
		}
		out.entries[key] = f.clone()
	}
	return out
}

// clone 複製單一條目（含 EmbedFiles 對映）。nil receiver 安全，返回 nil。
func (f *NodeFact) clone() *NodeFact {
	if f == nil {
		return nil
	}
	cp := *f
	if f.EmbedFiles != nil {
		cp.EmbedFiles = make(map[string][]byte, len(f.EmbedFiles))
		for k, v := range f.EmbedFiles {
			cp.EmbedFiles[k] = v
		}
	}
	return &cp
}

// Merge 把另一張表的條目併入本表（模塊合併點用）。既有鍵不被覆蓋 —— 與
// `SemanticContext.Merge` 對另兩張表的語義一致。nil 安全。
func (t *NodeFacts) Merge(other *NodeFacts) {
	if t == nil || other == nil {
		return
	}
	if t.entries == nil {
		t.entries = make(map[Node]*NodeFact)
	}
	for n, f := range other.entries {
		if _, exists := t.entries[n]; !exists {
			t.entries[n] = f.clone()
		}
	}
}
