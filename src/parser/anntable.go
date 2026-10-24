package parser

// anntable.go — Program 級註解表（Stage 3 of the THIR plan,
// docs/design/nolang-parser-lowering-invariants.md §3.5）。
//
// 背景：`#{...}` 註解條目原本是 `NodeSemantics` 的四個欄位 ——
// `RawAnnotations`（解析期收集的原始條目）、`Annotations`（ResolveProgram 拷貝
// 的成品）、`PlatformKeys`、`GenericParams`。後三者都可從前者純函數算出，
// 卻被物化成欄位，造成兩個長期問題：
//
//  1. `Annotations` 與 `RawAnnotations` 內容恆等（ResolveProgram 只是
//     `ns.Annotations = ns.RawAnnotations`），但「誰在什麼時候可見」取決於
//     ResolveProgram 是否跑過，於是下游被迫寫成
//     `hasOverflow(sem.AnnotationsOf(n)) || hasOverflow(sem.RawAnnotationsOf(n))`
//     這種「雙查」——同一個事實問兩遍，只為了覆蓋兩種填充時機；
//  2. 註解是 nodeSem 裡條目數最多的一類（每個帶 `#{...}` 的節點都有一筆），
//     把它留在以 AST 指針為鍵的 nodeSem 裡，會讓 Stage 7 刪除 nodeSem 時要
//     一次性搬走最大的一塊（該階段已完成，註解全程獨立成表）。
//
// 故本輪把它們遷到獨立的 `AnnTable`：只有**一份**存儲（原始條目），
// `AnnotationsOf` / `RawAnnotationsOf` 都讀它，`PlatformKeysOf` /
// `GenericParamsOf` 降為純函數（從條目即時算出，不再物化）。
//
// 掛載：`SemanticContext.anns`（與 VarTypes 等同為 Program 級副表），
// 經 `Program.Anns()` 暴露給需要整表操作的呼叫方（blob 序列化、clone、合併）。
// 刻意**不做**成 `Program` 的獨立字段 + `SemanticContext` 持有別名：那會出現
// 「兩個字段必須手工同步」的脆弱狀態，而合併期（transpiler 建 merged program）
// 與反序列化期（`detToSem` 重建 Sem）各有一次重建，漏一處就靜默丟註解。
//
// Serialization：本表**必須**跨進程保留（平台過濾 `FilterByPlatform`、`overflow`
// 溢出處理、`index-out` 索引守衛、`embed` 全都讀它），故與 `FmtTrivia` 的
// 「不進 blob」相反 —— 經 `CachedProgram.Anns`（occurrence 編號鍵）編碼，見
// astcache.go 的 SerializeAnns / RestoreAnns。
type AnnTable struct {
	// entries 刻意未導出：這樣所有以反射遍歷 Program 欄位的 walker
	// （astcache.go 的 occurrence 走查、sugar_seam_test / sugar_kinds_test 的
	// 普查）都看不見它，無需在三處跳過名單裡各加一次。
	entries map[Node][]*AnnotationEntry
}

// NewAnnTable 建立一張空的註解表。
func NewAnnTable() *AnnTable {
	return &AnnTable{entries: make(map[Node][]*AnnotationEntry)}
}

// GobEncode / GobDecode 讓本表在 gob 面前是「什麼都不帶」的空殼。
//
// 為什麼需要這兩個空方法：本表**沒有導出欄位**（entries 刻意未導出，好讓反射
// walker 看不見它），而 gob 對「無導出欄位的型別」會直接報錯
// `gob: type parser.AnnTable has no exported fields`。
//
// ⚠️ 這**不代表**註解不進 blob —— 那樣平台過濾／overflow／index-out 會在快取
// 命中時全部靜默失效。註解走的是 `CachedProgram.Anns`（occurrence 編號鍵，
// 見 astcache.go）。這對空方法只服務「直接 gob 編碼 `*parser.Program`」的路徑
// （實測只有 mmast_test.go 為模擬上一代格式而這麼做，且該測試只關心解碼失敗）。
// 兩條生產編碼路徑（checker/stdast.go、mmast/mmast.go）編的都是
// `*CachedProgram`，註解照常帶上。
func (t *AnnTable) GobEncode() ([]byte, error) { return nil, nil }

func (t *AnnTable) GobDecode([]byte) error { return nil }

// Set 為節點**覆蓋**寫入註解條目（原 `SemanticContext.SetRawAnnotations` 的
// 語義：整批替換，不是追加）。ents 為 nil 時等同刪除該鍵下的條目但保留鍵
// （與原 `s.ensure(n).RawAnnotations = entries` 一致 —— 原實現總是先 ensure
// 建出條目再賦值）。
//
// ⚠️ 覆蓋語義是被依賴的：lowering 的 `moveOverflowAnnotations` 類改寫會把
// 外層陳述的註解搬去內層，若變成追加就會重複觸發溢位處理。
// nil receiver / nil 節點安全。
func (t *AnnTable) Set(n Node, ents []*AnnotationEntry) {
	if t == nil || n == nil {
		return
	}
	if t.entries == nil {
		t.entries = make(map[Node][]*AnnotationEntry)
	}
	t.entries[n] = ents
}

// Of 返回節點的註解條目（無則 nil）。nil receiver / nil 節點安全。
//
// 它同時是 `SemanticContext.AnnotationsOf` 與 `.RawAnnotationsOf` 的後端：
// 遷移後兩者讀的是同一份存儲，故「ResolveProgram 是否跑過」不再影響可見性
// （下游那些 `AnnotationsOf || RawAnnotationsOf` 雙查從此永遠等價）。
func (t *AnnTable) Of(n Node) []*AnnotationEntry {
	if t == nil || n == nil {
		return nil
	}
	return t.entries[n]
}

// Len 回傳條目數（測試用：反空洞斷言需要證明表真的被填過）。
func (t *AnnTable) Len() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Clone 深拷貝條目並把鍵經 remap 換到副本節點（CloneProgram 用）。
// remap 未收錄的鍵原樣保留——與 `remapSem` 對另兩張表的處理同口徑
// （指向未被拷貝的孤立節點）。nil receiver 安全，返回 nil。
func (t *AnnTable) Clone(remap func(Node) Node) *AnnTable {
	if t == nil || len(t.entries) == 0 {
		return nil
	}
	out := &AnnTable{entries: make(map[Node][]*AnnotationEntry, len(t.entries))}
	for n, ents := range t.entries {
		key := n
		if remap != nil {
			if nn := remap(n); nn != nil {
				key = nn
			}
		}
		out.entries[key] = ents
	}
	return out
}

// Merge 把另一張表的條目併入本表（模塊合併點用）。既有鍵不被覆蓋——與
// `SemanticContext.Merge` 對另兩張表的語義一致。nil 安全。
func (t *AnnTable) Merge(other *AnnTable) {
	if t == nil || other == nil {
		return
	}
	if t.entries == nil {
		t.entries = make(map[Node][]*AnnotationEntry)
	}
	for n, ents := range other.entries {
		if _, exists := t.entries[n]; !exists {
			t.entries[n] = ents
		}
	}
}

// MergeInto 是為 `SemanticContext.Merge` 提供的入口：other 可能為 nil
// （反序列化重建的 Sem 不帶表），而本表可能尚未建立。
func (t *AnnTable) ensure() *AnnTable {
	if t == nil {
		return NewAnnTable()
	}
	if t.entries == nil {
		t.entries = make(map[Node][]*AnnotationEntry)
	}
	return t
}
