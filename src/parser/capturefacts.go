package parser

// capturefacts.go — 結構捕獲（option capture）判定結果的節點鍵副表。
//
// ── 為什麼要有這張表 ──
// 「結構捕獲」當前在 **AST 層注入合成節點**：H0 為 `a = b + c / d` 這類「可能
// 上拋 `?T`」的算式分配暫存變數 `__cap_N`，再改寫成 match 三臂（見
// lowering.go 的 maybeCaptureOptionAssign）。這是設計文檔 §5 的 **V4 違規**
// （注入合成節點），也是 S3 的 backlog。
//
// 消除它需要把「合成位置」從 AST 層移到 HIR 層（H0 在 AST→HIR 轉換時直接產
// 出 `KLet` + `KIf` 鏈）。但捕獲判定依賴 lowerer 走查期累積的**即時作用域
// 狀態**（`declaredLocals` / `localDeclaredType` / `localDefinitelyNonOption`），
// `tohir` 是獨立 walker，拿不到 ⇒ 必須先把判定結果**物化**成副表，讓 tohir 讀。
//
// ── 本表的定位（S3-b 第一步）──
// **只寫不讀**：lowerer 照舊執行捕獲降糖（行為完全不變），同時把判定結果記進
// 本表。當下沒有任何消費方 ⇒ 對生產路徑零影響（G7 HIR 逐字節一致 + 全倉門禁
// 為證）。等 tohir 側的產出邏輯就緒後，再切換消費並刪除 AST 合成路徑。
//
// ⚠️ 刻意**不進 blob**：本表是編譯期降糖的產物，反序列化（AST cache）後由
// lowerer 重新計算即可；與 `TypeOverride`（裝配期產物但仍鏡像）不同，不鏡像
// 不會造成行為差異。故不加入 `Serialize*` / `Restore*`，也不影響 `ASTVersion`。

// CaptureFacts 存放一批節點的捕獲判定結果，以 AST 節點指針為鍵。
//
// entries 刻意未導出（同 TypeFacts）：使以反射遍歷 Program 欄位的 walker
// （astcache.go 的 occurrence 走查、sugar_seam_test / sugar_kinds_test 的
// 普查）看不見它，無需在三處跳過名單裡各加一次。
type CaptureFacts struct {
	entries map[Node]*CaptureFact
}

// CaptureFact 是單一節點的捕獲判定結果。
type CaptureFact struct {
	// Inner 是捕獲的內層型別（option payload 的型別，如 "i64" / "f64" / "str"）。
	// 來源：`?T` 的 T，已含算術根補正（浮點 / str 拼接）與顯式標註覆寫。
	Inner string

	// Binds 是由內至外的捕獲綁定：每個元素對應一個被抽出成 `__cap_N` 的子表達式。
	//
	// ⚠️ Binds[i].Expr 一律指向**原始** AST 節點，絕不可以是副本：語義副表
	//（InferredType / TypeOverride）以原始指針為鍵，副本節點查不到型別，
	// 會讓 tohir 產出的 HIR 與今日不一致。替換成 `__cap_N` 識別符的動作由
	// tohir 在 lowering 時依本表**即席**完成（見 HIRCaptureEnabled 說明），
	// 不產生、也不改寫任何 AST。
	Binds []CaptureBind

	// WasDeclared 回報本陳述的目標變數在原始碼中**是否已宣告**
	//（lowerer 的 declaredLocals 判定）。
	//
	// 決定 H0 是否要產出預宣告 `x ?T = nil`：已宣告為 ?T 時只需臂內重賦值，
	// 多一條預宣告會改變 HIR（且語義上是把已初始化的變數清成 nil）。
	// 該狀態活在 lowerer 的走查期作用域裡，tohir 拿不到 ⇒ 必須隨表帶出。
	WasDeclared bool
}

// HIRCaptureEnabled 是 S3-b 的**雙軌開關**，也是 S3-c 切換後的**生產預設**。
//
//   - true（S3-c 後的生產預設，自 2026-10-25 起）：lowerer 只把捕獲判定寫進
//     本表、**不再改寫 AST**（V4 合規：用戶 AST 保持凍結）；結構由 H0
//     （tohir 的 lowerCaptureToHIR）讀本表產出等價的 `KLet` + `KIf` 鏈。
//   - false：lowerer 照舊在 AST 層注入 `__cap_N` 合成節點（S3-b 之前的舊路徑，
//     僅供雙軌等價測試 `TestHIRCaptureTrackMatchesASTTrack` 對照用）。
//
// 兩軌並存是為了逐檔比對（AST 降糖產物 vs H0 直產產物須 HIR 逐字節一致）；
// 雙軌測試會顯式設回 false 跑 AST 軌，故舊路徑仍被持續守護。S3-c 並未刪除
// 舊路徑——保留它使雙軌回歸閘不退化成 H0-vs-H0 的空比較。
var HIRCaptureEnabled = true

// CaptureBind 描述單一條捕獲綁定。
type CaptureBind struct {
	// Expr 是被捕獲的原始子表達式（AST 節點，跨進程不可序列化）。
	Expr Node
	// Inner 是該綁定的內層型別。
	Inner string
	// Name 是綁定用的暫存名（當前形態為 `__cap_N`）。
	Name string
}

func (t *CaptureFacts) ensure() *CaptureFacts {
	if t == nil {
		// 與 TypeFacts 同口徑：nil receiver 回新建表（呼叫方須負責接住——
		// 見 SemanticContext.Merge 的 `s.captureFacts = s.captureFacts.ensure()`）。
		return &CaptureFacts{}
	}
	if t.entries == nil {
		t.entries = make(map[Node]*CaptureFact)
	}
	return t
}

// Set 為節點 n 登記捕獲判定結果（f 為 nil 表示不捕獲）。
func (t *CaptureFacts) Set(n Node, f *CaptureFact) {
	if n == nil {
		return
	}
	t.ensure().entries[n] = f
}

// Of 回傳節點 n 的捕獲判定結果；無記錄時回傳 nil（「未判定」，與「判定不捕獲」
// 有別——後者會是條 f==nil 的記錄）。
func (t *CaptureFacts) Of(n Node) *CaptureFact {
	if t == nil || t.entries == nil || n == nil {
		return nil
	}
	return t.entries[n]
}

// Len 回傳已登記的條目數（測試用）。
func (t *CaptureFacts) Len() int {
	if t == nil || t.entries == nil {
		return 0
	}
	return len(t.entries)
}

// Merge 把另一張表的條目併入本表（模組合併點用）。既有鍵不被覆蓋——與
// anns / typeFacts / nodeFacts 同口徑。條目值可為 nil（「判定不捕獲」的
// 記錄），併入時原樣帶過，不做解引用。
func (t *CaptureFacts) Merge(other *CaptureFacts) {
	if t == nil || other == nil || len(other.entries) == 0 {
		return
	}
	t.ensure()
	for n, f := range other.entries {
		if _, exists := t.entries[n]; !exists {
			t.entries[n] = f
		}
	}
}

// Clone 複製本表並把鍵經 remap 換到副本節點（CloneProgram 的 remapSem 用；
// 與 typeFacts 等三表同口徑）。空表回 nil，讓呼叫方的 nil 檢查自然跳過。
//
// ⚠️ 條目值只做淺拷貝：CaptureFact.Binds[i].Expr 仍指向**原始** AST 節點
// （語義副表以原始指針為鍵，副本節點查不到型別——見 CaptureFact.Binds 的
// 註解）。當下唯一消費方（checker 的 optunuse/ovfhndld 護欄）只讀
// Inner/WasDeclared，不碰 Binds，故淺拷貝足夠；H0 軌若要在 merged 路徑
// 消費 Binds，須連同 Expr 一並換鍵（S3-c 切換時處理）。
func (t *CaptureFacts) Clone(remap func(Node) Node) *CaptureFacts {
	if t == nil || len(t.entries) == 0 {
		return nil
	}
	out := &CaptureFacts{entries: make(map[Node]*CaptureFact, len(t.entries))}
	for n, f := range t.entries {
		key := n
		if remap != nil {
			if nn := remap(n); nn != nil {
				key = nn
			}
		}
		if f != nil {
			cp := *f
			out.entries[key] = &cp
		} else {
			out.entries[key] = nil
		}
	}
	return out
}

// SetCaptureFact 為節點 n 登記捕獲判定結果（sem 可為 nil）。
func (s *SemanticContext) SetCaptureFact(n Node, f *CaptureFact) {
	if s == nil || n == nil {
		return
	}
	if s.captureFacts == nil {
		s.captureFacts = &CaptureFacts{}
	}
	s.captureFacts.ensure().Set(n, f)
}

// CaptureFactOf 回傳節點 n 的捕獲判定結果（sem 或表為 nil 時回 nil）。
func (s *SemanticContext) CaptureFactOf(n Node) *CaptureFact {
	if s == nil {
		return nil
	}
	return s.captureFacts.Of(n)
}

// CaptureFactsLen 回傳已登記條目數（測試用）。
func (s *SemanticContext) CaptureFactsLen() int {
	if s == nil {
		return 0
	}
	return s.captureFacts.Len()
}
