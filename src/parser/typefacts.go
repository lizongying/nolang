package parser

// typefacts.go — Program 級型別事實表（Stage 4 of the THIR plan,
// docs/design/nolang-parser-lowering-invariants.md §3.5）。
//
// 背景：`InferredType` / `InferredByOptionArith` / `TypeOverride` 原本是
// `NodeSemantics`（已於 Stage 7 刪除）的三個欄位。它們是 nodeSem 裡唯一一組
// 「型別類」事實，
// 與剩下的改寫事實（Callee / IndexInBounds）、溯源（SourceFile / ModuleOwner）、
// union（VariadicUnion / GenericUnion）在寫入階段與消費者上都不相同：
//
//   - `InferredType` / `InferredByOptionArith`：**解析期**產出
//     （lowering.go:931-932 的 publishInferredType 唯一寫入點），消費者橫跨
//     tohir / fmt / checker / lowering 四個包；
//   - `TypeOverride`：**裝配期**產出（slice-structs 的 retypesFactoryLets、
//     devirtualizeInterfaceVars、cloneStmtForUnion），消費者是 tohir 與 build。
//
// 搬出 nodeSem 的理由與 Stage 2/3 一致：讓 nodeSem 逐類瘦身，Stage 7 才能整體
// 刪除（已完成）。設計文檔 §3.5 的目標管線已為本表預留名字 `TypeFacts`，與
// `AnnTable`（Stage 3）、`FmtTrivia`（Stage 2）、`NodeFacts`（Stage 5）四表並立。
//
// 掛載：`SemanticContext.typeFacts`（與 `anns` 並列），經 `Program.TypeFacts()`
// 暴露給整表操作的呼叫方（blob 序列化 / clone / 合併）。**不做**「Program 字段 +
// Sem 別名」—— 反序列化（`detToSem`）與合併期各有一次重建，兩處手工同步必漏。
//
// 🔴 本輪是**純重構**：`SemanticContext` 上 7 個訪問器的簽名全部不變、方法體改為
// 轉發，故下游 11 個讀寫點（tohir / fmt / checker / build / lowering）零改動。
//
// Serialization：三字段**全部**鏡像進 blob（`CachedProgram.TypeFacts`，
// occurrence 編號鍵）。`InferredType` 丟失會讓 KLet 的 `?T` 型別消失、checker
// 四處 option 豁免失效；`TypeOverride` 雖是裝配期產物（按 C9「下游」清單理論上
// 可不鏡像），但遷移前 `detNodeSem` 就是鏡像它的——不鏡像即行為變更，本輪保持原狀。

// TypeFacts 存放一批節點的型別事實，以 AST 節點指針為鍵。
//
// entries 刻意未導出：這樣所有以反射遍歷 Program 欄位的 walker
// （astcache.go 的 occurrence 走查、sugar_seam_test / sugar_kinds_test 的
// 普查）都看不見它，無需在三處跳過名單裡各加一次。
type TypeFacts struct {
	entries map[Node]*TypeFact
}

// TypeFact 是單一節點的型別事實條目。
//
// 與 `TypeFacts` 相反，本型別**必須導出**：它是 `detCached.TypeFacts` 的 gob
// 線值（`[]detU32Facts` 的 V），gob 要對它做反射編碼。
type TypeFact struct {
	// InferredType 是 lowering 為某個 LetStatement 推斷出的 `?T` 型別字串
	//（如 "?i64"）。原 AST 寫點 `s.Type = &NullableType{IsInferred: true}`
	// 在 §1.4 下被禁止，S3 起改記於此。
	//
	// 寫入者：lowering.go:931（publishInferredType，唯一）。
	// 讀取者：tohir 的 KLet（:449）、fmt 的 nullable 型別跳過渲染（:392）、
	// checker 的 inferredOption 判定（:3195 / :5317 / :5341 / :5736）。
	InferredType string

	// ByOptionArith 區分 InferredType 的寫入來源：true 表示由 `inferOptionArith`
	//（D6 算術根推斷）寫入，false 表示由 `synthesizeNullableType`（結構捕獲期間
	// 合成索引／捕獲根節點型別）寫入。兩者最終給同一個 HIR 型別，但對「根為
	// 算術的綁定是否還要進結構 desugar」的回答不同 —— `maybeCaptureOptionAssign`
	// 的短路檢查需區分兩者（lowering.go:1197）。語義層面此差異僅一 bit。
	//
	// ⚠️ 它是 parser 的**内部控制位**，不是程式事實：MIR / checker / fmt 都不讀，
	// 唯一消費點是 lowering 自己。故**不進 THIR**（不消耗 Flag 位）。
	ByOptionArith bool

	// TypeOverride 是整程式裝配為某個 LetStatement 定下的**有效型別覆蓋**
	//（具現化後的 `vec.i64`、介面具象化後的具體實作型別）。覆蓋語義：優先於
	// 節點宣告的 Type。
	//
	// 有效型別解析序：**TypeOverride > Type > InferredType**，實現在
	// tohir.go:430-452（KLet）。本表只負責存，優先序不由表層決定。
	//
	// 寫入者：build 的兩個裝配 pass（slice_structs.go:454 的 retypesFactoryLets、
	// transpiler_monomorph.go:1256 的 devirtualizeInterfaceVars）與
	// cloneStmtForUnion（:897，克隆體**繼承**——與 GenericUnion 的「不克隆」相反）。
	// 讀取者：tohir 的 PopulateInferredTypes（:247）與 KLet（:437）、
	// buildVarTypes（transpiler_validate.go:33）、
	// collectVarTypesFromBodyIn（transpiler_monomorph.go:2228）。
	TypeOverride string
}

// NewTypeFacts 建立一張空的型別事實表。
func NewTypeFacts() *TypeFacts {
	return &TypeFacts{entries: make(map[Node]*TypeFact)}
}

// GobEncode / GobDecode 讓本表在 gob 面前是「什麼都不帶」的空殼。
//
// 為什麼需要這兩個空方法：本表**沒有導出字段**（entries 刻意未導出，好讓反射
// walker 看不見它），而 gob 對「無導出字段的型別」會直接報錯
// `gob: type parser.TypeFacts has no exported fields`。
//
// ⚠️ 這**不代表**型別事實不進 blob —— 那樣 `?T` 推斷與裝配期覆寫會在快取命中時
// 全部靜默失效。真實數據走 `CachedProgram.TypeFacts`（occurrence 編號鍵，見
// astcache.go 的 SerializeTypeFacts / RestoreTypeFacts）。這對空方法只服務
// 「直接 gob 編碼 `*parser.Program`」的路徑（實測只有 mmast_test.go 為模擬上一代
// 格式而這麼做）。
func (t *TypeFacts) GobEncode() ([]byte, error) { return nil, nil }

func (t *TypeFacts) GobDecode([]byte) error { return nil }

// ensure 取得（必要時建立）本表；nil receiver 會建一張新表返回。
func (t *TypeFacts) ensure() *TypeFacts {
	if t == nil {
		return NewTypeFacts()
	}
	if t.entries == nil {
		t.entries = make(map[Node]*TypeFact)
	}
	return t
}

// fact 取出 n 的條目（不存在則建立）。呼叫前必須已排除 nil 表。
func (t *TypeFacts) fact(n Node) *TypeFact {
	if t.entries == nil {
		t.entries = make(map[Node]*TypeFact)
	}
	f, ok := t.entries[n]
	if !ok {
		f = &TypeFact{}
		t.entries[n] = f
	}
	return f
}

// SetInferred 記錄 lowering 推斷出的 `?T` 型別字串。
//
// ⚠️ 空字串**直接忽略、不建條目** —— 原 `SemanticContext.SetInferredType`
// 的守衛（`if typ == "" return`）必須逐字保留：條目集合會影響 blob 版面與合併
// 結果（Merge 視「鍵已存在」為「不覆蓋」）。
func (t *TypeFacts) SetInferred(n Node, typ string) {
	if t == nil || n == nil || typ == "" {
		return
	}
	t.fact(n).InferredType = typ
}

// InferredOf 回傳節點的推斷 `?T` 型別字串（無則 ""）。nil 安全。
func (t *TypeFacts) InferredOf(n Node) string {
	if t == nil || n == nil {
		return ""
	}
	if f, ok := t.entries[n]; ok {
		return f.InferredType
	}
	return ""
}

// SetByOptionArith 標記推斷來源是否為 `inferOptionArith`。
//
// ⚠️ 與 `SetInferred` / `SetOverride` 不同：本方法**總是建條目**，即使 `v == false`
// —— 原 `SemanticContext.SetInferredByOptionArith` 無條件 `ensure(n)` 後賦值。
// 這個不對稱是有意保留的（見 `TypeFact.ByOptionArith` 的註釋）。
func (t *TypeFacts) SetByOptionArith(n Node, v bool) {
	if t == nil || n == nil {
		return
	}
	t.fact(n).ByOptionArith = v
}

// ByOptionArithOf 回傳推斷來源標記（無條目時 false）。nil 安全。
func (t *TypeFacts) ByOptionArithOf(n Node) bool {
	if t == nil || n == nil {
		return false
	}
	if f, ok := t.entries[n]; ok {
		return f.ByOptionArith
	}
	return false
}

// SetOverride 記錄裝配期定下的有效型別覆蓋。
// ⚠️ 空字串直接忽略、不建條目（同 `SetInferred` 的守衛）。
func (t *TypeFacts) SetOverride(n Node, ov string) {
	if t == nil || n == nil || ov == "" {
		return
	}
	t.fact(n).TypeOverride = ov
}

// OverrideOf 回傳節點的有效型別覆蓋字串（無則 ""）。nil 安全。
func (t *TypeFacts) OverrideOf(n Node) string {
	if t == nil || n == nil {
		return ""
	}
	if f, ok := t.entries[n]; ok {
		return f.TypeOverride
	}
	return ""
}

// Len 回傳條目數（測試用：反空洞斷言需要證明表真的被填過）。
func (t *TypeFacts) Len() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Clone 深拷貝條目並把鍵經 remap 換到副本節點（CloneProgram 用）。
// remap 未收錄的鍵原樣保留——與 `remapSem` 對另兩張表的處理同口徑
// （指向未被拷貝的孤立節點）。nil receiver 安全，返回 nil。
func (t *TypeFacts) Clone(remap func(Node) Node) *TypeFacts {
	if t == nil || len(t.entries) == 0 {
		return nil
	}
	out := &TypeFacts{entries: make(map[Node]*TypeFact, len(t.entries))}
	for n, f := range t.entries {
		key := n
		if remap != nil {
			if nn := remap(n); nn != nil {
				key = nn
			}
		}
		cp := *f
		out.entries[key] = &cp
	}
	return out
}

// Merge 把另一張表的條目併入本表（模塊合併點用）。既有鍵不被覆蓋——與
// `SemanticContext.Merge` 對另兩張表的語義一致。nil 安全。
func (t *TypeFacts) Merge(other *TypeFacts) {
	if t == nil || other == nil {
		return
	}
	if t.entries == nil {
		t.entries = make(map[Node]*TypeFact)
	}
	for n, f := range other.entries {
		if _, exists := t.entries[n]; !exists {
			cp := *f
			t.entries[n] = &cp
		}
	}
}
