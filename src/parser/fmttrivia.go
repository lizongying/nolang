package parser

// fmttrivia.go — formatter 專用的表層 trivia 表（Stage 2 of the THIR plan,
// docs/design/nolang-parser-lowering-invariants.md §3.5）。
//
// 背景：RTFlags（`RTFlag` 位集合）與 OpeningBraceComment（`{` 同行註釋）原是
// `NodeSemantics`（已於 Stage 7 刪除）的兩個欄位，存放於 `SemanticContext.nodeSem`。
// 但它們與語義無關
// ——只有 formatter / LSP 讀，編譯管線（build / checker / codegen）完全不讀
// （resolver.go 原註解即如此聲明）。把它們留在以 AST 指針為鍵的 nodeSem 裡，
// 會讓「語義副表」與「排版 trivia」兩件事共用一張表、共用一套序列化，並使
// Stage 7 刪除 nodeSem 時要把 formatter 需求一起拖下水（該階段已完成，
// trivia 從頭到尾都是獨立表，未被拖下水）。
//
// 故本輪把它們遷到獨立的 `FmtTrivia` 表，掛在 `Program.FmtTrivia`（Program 級
// 輸出表）：
//
//   - 寫入者：parser 解析期（stmt.go / expr.go / match.go / decl.go）與
//     lowering（lowering.go）；
//   - 讀取者：只有 src/fmt（LSP 經 fmt 間接消費）；
//   - 不進 THIR / HIR（hir.Package 硬約束；見 hir/hir.go Package 註解）；
//   - 不參與磁碟 blob 序列化（見下方 NotSerialized 說明）。
//
// NotSerialized：本表刻意不入 `detProgram` 影子，因此 gob 往返後為 nil。這是
// 安全的：fmt / LSP 全部走 fresh parse（`SkipLowering = true`）而從不讀磁碟
// 快取；build / checker 對這兩個欄位零讀取。程式碼從不直接 gob 編碼
// `*Program`（兩條編碼路徑 stdast.go / mmast.go 編的都是 `*CachedProgram`，
// 經 detProgram 影子），故也不會觸發 map 迭代序非確定性。

// FmtTrivia 存放一批節點的排版 trivia，以 AST 節點指針為鍵。
//
// entries 刻意未導出：這樣所有以反射遍歷 Program 欄位的 walker
// （astcache.go 的 occurrence 走查、sugar_seam_test / sugar_kinds_test 的
// 普查）都看不見它，無需在三處跳過名單裡各加一次。`Program.FmtTrivia` 本身
// 必須導出（src/fmt 要讀），但其值是沒有導出欄位的結構體。
type FmtTrivia struct {
	entries map[Node]*FmtTriviaEntry
}

// FmtTriviaEntry 是單一節點的 trivia 條目。
type FmtTriviaEntry struct {
	// RTFlags 是 fmt 往返（round-trip）專用的表層語法標誌位
	//（裸 match / wildcard arm / standalone if / rawCond 包裝層 / elif
	// / pipeline value / 空臂零值）。僅 formatter 讀取。
	RTFlags RTFlag

	// OpeningBraceComment 保存 `{` 同行註釋（BlockStatement 或裸 match 的
	// IfExpression）。僅 formatter 讀取。
	OpeningBraceComment *CommentGroup
}

// NewFmtTrivia 建立一張空的 trivia 表。
func NewFmtTrivia() *FmtTrivia {
	return &FmtTrivia{entries: make(map[Node]*FmtTriviaEntry)}
}

// GobEncode / GobDecode 讓本表在 gob 面前是「什麼都不帶」的空殼。
//
// 為什麼需要這兩個空方法：本表**沒有導出欄位**（entries 刻意未導出，好讓所有
// 反射 walker 看不見它），而 gob 對「無導出欄位的型別」會直接報錯
// `gob: type parser.FmtTrivia has no exported fields`。正常編碼路徑（stdast /
// mmast）編的是 `*CachedProgram`，經 detProgram 影子、根本碰不到本表；但
// `mmast_test.go` 為模擬上一代格式而**直接** gob 編碼 `*parser.Program`，就撞上
// 了這個錯誤。
//
// 與其為了遷就編碼器把 entries 導出（那會讓三處反射 walker 跳過名單全部失效），
// 不如在此明確宣告：本表**不參與序列化**——編碼寫空、解碼讀空，語義與
// 「不入 detProgram 影子」完全一致（fmt/LSP 一律 fresh parse，本就讀不到 blob）。
func (t *FmtTrivia) GobEncode() ([]byte, error) { return nil, nil }

func (t *FmtTrivia) GobDecode([]byte) error { return nil }

// ensure 取出 n 的條目（不存在則建立）。呼叫前必須已排除 nil。
func (t *FmtTrivia) ensure(n Node) *FmtTriviaEntry {
	if t.entries == nil {
		t.entries = make(map[Node]*FmtTriviaEntry)
	}
	e, ok := t.entries[n]
	if !ok {
		e = &FmtTriviaEntry{}
		t.entries[n] = e
	}
	return e
}

// SetRTFlag 為節點**疊加** fmt 往返標誌位（OR，非覆蓋）——原
// `SemanticContext.SetRTFlag` 的語義必須逐字保留：多處寫入同一節點的
// 不同標誌位（如 `RTStandalone | RTMatchWildcard`）依賴此疊加性。
// nil receiver / nil 節點安全。
func (t *FmtTrivia) SetRTFlag(n Node, fl RTFlag) {
	if t == nil || n == nil {
		return
	}
	t.ensure(n).RTFlags |= fl
}

// HasRTFlag 報告節點是否帶指定往返標誌。nil receiver / nil 節點安全。
func (t *FmtTrivia) HasRTFlag(n Node, fl RTFlag) bool {
	if t == nil || n == nil {
		return false
	}
	if e, ok := t.entries[n]; ok {
		return e.RTFlags&fl != 0
	}
	return false
}

// SetOpeningBraceComment 設定節點的 `{` 同行註釋；cg 為 nil 時清除。
//
// ⚠️ nil 分支的語義是「**只清除已有條目、不新建條目**」——這與
// `decl.go` 遷走函式體註釋後的「清空」呼叫直接相關。若退化成
// `ensure(n).OpeningBraceComment = nil`，原本沒有語義的節點會被建出一個空
// 條目，改變條目集合（進而影響 Blob 版面與合併結果）。
// nil receiver / nil 節點安全。
func (t *FmtTrivia) SetOpeningBraceComment(n Node, cg *CommentGroup) {
	if t == nil || n == nil {
		return
	}
	if cg == nil {
		if e, ok := t.entries[n]; ok {
			e.OpeningBraceComment = nil
		}
		return
	}
	t.ensure(n).OpeningBraceComment = cg
}

// OpeningBraceCommentOf 返回節點的 `{` 同行註釋（無則 nil）。
// nil receiver / nil 節點安全。
func (t *FmtTrivia) OpeningBraceCommentOf(n Node) *CommentGroup {
	if t == nil || n == nil {
		return nil
	}
	if e, ok := t.entries[n]; ok {
		return e.OpeningBraceComment
	}
	return nil
}

// Len 回傳條目數（測試用：反空洞斷言需要證明表真的被填過）。
func (t *FmtTrivia) Len() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Clone 深拷貝條目並把鍵經 remap 換到副本節點（CloneProgram 用）。
// remap 未收錄的鍵原樣保留——與 `remapSem` 對三張語義表的處理同口徑
// （指向未被拷貝的孤立節點）。
// nil receiver 安全，返回 nil。
func (t *FmtTrivia) Clone(remap func(Node) Node) *FmtTrivia {
	if t == nil || len(t.entries) == 0 {
		return nil
	}
	out := &FmtTrivia{entries: make(map[Node]*FmtTriviaEntry, len(t.entries))}
	for n, e := range t.entries {
		key := n
		if remap != nil {
			if nn := remap(n); nn != nil {
				key = nn
			}
		}
		cp := *e
		out.entries[key] = &cp
	}
	return out
}

// Merge 把另一張表的條目併入本表（模塊合併點用）。既有鍵不被覆蓋——與
// `SemanticContext.Merge` 對三張語義表的語義一致。nil 安全。
func (t *FmtTrivia) Merge(other *FmtTrivia) {
	if t == nil || other == nil {
		return
	}
	for n, e := range other.entries {
		if _, exists := t.entries[n]; !exists {
			t.ensure(n)
			t.entries[n] = e
		}
	}
}
