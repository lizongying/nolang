package parser

// annotations.go — 節點上的 `#{...}` 條目訪問（語法即真相）。
//
// 歷史：Stage 3 曾把註解遷進 `SemanticContext.anns`（`AnnTable`，節點指針鍵），
// 於是**尾隨註解**（`x = v[5] #{index-out=zero}`）只存在於表裡、AST 完全沒有
// 承載 —— 任何「從 AST 重建註解」的路徑都會靜默丟掉它們。現改為條目住在節點
// 上（`AnnotatedNode`），表整條刪除。
//
// 這一層的存在意義：讓 `SemanticContext.AnnotationsOf` 等既有訪問器維持原簽名，
// 從而 ~100 個呼叫點零改動，同時把存儲換到節點。

// annotated 由所有可攜帶註解的節點實作（即 `AnnotatedNode` 的兩個方法）。
type annotated interface {
	GetAnnotations() []*AnnotationEntry
	SetAnnotations([]*AnnotationEntry)
}

// annotationsOf 返回節點的註解條目；不支援註解的節點返回 nil。
func annotationsOf(n Node) []*AnnotationEntry {
	if n == nil {
		return nil
	}
	if a, ok := n.(annotated); ok {
		return a.GetAnnotations()
	}
	return nil
}

// setAnnotationsOf 整批替換節點的註解條目；不支援註解的節點為 no-op。
//
// 覆蓋語義（不是追加）見 `AnnotatedNode.SetAnnotations` 的說明。
func setAnnotationsOf(n Node, ents []*AnnotationEntry) {
	if n == nil {
		return
	}
	if a, ok := n.(annotated); ok {
		a.SetAnnotations(ents)
	}
}
