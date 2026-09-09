package llvm

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// TestOverflowModeFromNodeForStatement 驗證 HIR 重建後的 ForStatement.OverflowMode
// 能被 overflowModeFromNode 讀出（HIR 模式下 g.sem 為 nil，不能依賴 side-table）。
// 這是修復「迴圈體整數運算在 HIR 模式退回 option 模式 → %option→trunc 報錯」的關鍵路徑。
func TestOverflowModeFromNodeForStatement(t *testing.T) {
	cases := []string{"wrap", "clamp0", "min", "max", "saturate"}
	for _, mode := range cases {
		var g Generator
		fs := &parser.ForStatement{OverflowMode: mode}
		got := g.overflowModeFromNode(fs)
		if got != mode {
			t.Errorf("overflowModeFromNode(ForStatement{OverflowMode:%q}) = %q, want %q", mode, got, mode)
		}
	}
}

// TestOverflowModeFromNodeForStatementEmpty 確認無標註時退回預設（""）。
func TestOverflowModeFromNodeForStatementEmpty(t *testing.T) {
	var g Generator
	fs := &parser.ForStatement{} // OverflowMode 空
	if got := g.overflowModeFromNode(fs); got != "" {
		t.Errorf("overflowModeFromNode(ForStatement{}) = %q, want empty (default option mode)", got)
	}
}

// TestOverflowModeFromNodeExpressionStatement 驗證 if/match 臂體（ExpressionStatement）的
// OverflowMode 在 HIR 重建後能被 overflowModeFromNode 讀出（arm 条件註解在註解后 的修復路徑）。
func TestOverflowModeFromNodeExpressionStatement(t *testing.T) {
	cases := []string{"wrap", "clamp0", "min", "max", "saturate"}
	for _, mode := range cases {
		var g Generator
		es := &parser.ExpressionStatement{OverflowMode: mode}
		if got := g.overflowModeFromNode(es); got != mode {
			t.Errorf("overflowModeFromNode(ExpressionStatement{OverflowMode:%q}) = %q, want %q", mode, got, mode)
		}
	}
}

// TestOverflowModeFromNodeExpressionStatementEmpty 確認無標註時退回預設（""）。
func TestOverflowModeFromNodeExpressionStatementEmpty(t *testing.T) {
	var g Generator
	es := &parser.ExpressionStatement{}
	if got := g.overflowModeFromNode(es); got != "" {
		t.Errorf("overflowModeFromNode(ExpressionStatement{}) = %q, want empty (default option mode)", got)
	}
}
