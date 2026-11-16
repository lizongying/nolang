package fmt

// redundant_guard.go — 「這個型別標註到底能不能省」的單一判定。
//
// 兩條路徑過去各寫一份：
//   - `no fmt` / `no fmt --fix`：cmd/no 的 computeRedundantTypeRemoval（算 byte 區間）
//   - LSP format-on-save：lsp 的 redundantTypeSafe（只判 AST）
//
// 兩份的防禦條件並不一致（LSP 多擋了 slice / map 型別），於是同一份檔案
// 「存檔自動格式化」與「命令列 no fmt -w」的結果可以不同 —— 直接違反
// AGENTS.md §5.3 的單一來源契約。本檔把 AST 層的判定抽出來，兩邊共用。
//
// 這裡刻意只依賴 parser（不 import checker），維持 fmt 套件的分層。

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// RedundantTypeAnnotationSafe 回傳 `n` 的型別標註是否可以安全省略。
//
// 「不安全」的四類（寧可保守，不可改壞語義）：
//  1. 定長陣列 `[N]T`：標註本身決定「固定棧陣列 vs 堆切片」的表示形式，
//     移除後 push 會改成增長語義（`[3]char` 的 len 由 3 變 4）。
//     checker 讀到的 ArrayLiteral 正是這個標註觸發轉換才形成的，屬循環論證。
//  2. map 字面值 `m [str]i64 = {..}`：移除後 parser 會把 `{..}` 讀成程式區塊。
//  3. 值含十六進位字面量：checker 的 `0xNN -> byte` 啟發式與編譯器
//     「無標註時整數字面量預設 i64」不一致，移除會把 byte 退化成 i64。
//  4. 位置不成立：推斷/合成型別的 Pos() 與變數名重合（從未寫入原始碼），
//     照常往後掃描會把變數名本身刪掉。
//
// slice `[]T` 與「非字面值的 map 標註」（如 `m [str]i64 = make-map()`）不在
// 禁止之列：它們只是型別提示，移除後 parser 仍能從右值推回來。這也正是
// cmd/no 既有行為；空字面量等推不回來的形態由呼叫端的「移除後再解析」關卡兜住。
func RedundantTypeAnnotationSafe(n *parser.LetStatement) bool {
	if n == nil || n.Type == nil || n.Value == nil || n.Name == nil {
		return false
	}
	// 定長陣列：[N]T 是表示形式開關，不是型別提示。
	if _, ok := n.Type.(*parser.ArrayType); ok {
		return false
	}
	// map 字面值：`m = {..}` 會被讀成程式區塊。
	if _, ok := n.Value.(*parser.MapLiteral); ok {
		return false
	}
	if ExpressionHasHexLiteral(n.Value) {
		return false
	}
	tp := n.Type.Pos()
	np := n.Name.Pos()
	if tp.Line != np.Line || tp.Column <= np.Column+len(n.Name.Value) {
		return false
	}
	return true
}

// ExpressionHasHexLiteral 走查字面量樹，回報是否含 0x/0X 開頭的整數字面量。
//
// 只看 AST：`no fmt` 的固定路徑另外還有一段源碼 span 文字掃描兜底
// （cmd/no 的 containsHexLiteral），能捕捉二元 / 呼叫運算元裡的 hex；
// 這裡保持 AST-only，是兩邊都能用的最小共同判準。
func ExpressionHasHexLiteral(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.IntegerLiteral:
		r := strings.ToLower(v.Raw)
		return strings.HasPrefix(r, "0x") || strings.HasPrefix(r, "-0x") || strings.HasPrefix(r, "+0x")
	case *parser.ArrayLiteral:
		if v.Size != nil && ExpressionHasHexLiteral(v.Size) {
			return true
		}
		for _, el := range v.Elements {
			if ExpressionHasHexLiteral(el) {
				return true
			}
		}
	case *parser.SliceLiteral:
		for _, el := range v.Elements {
			if ExpressionHasHexLiteral(el) {
				return true
			}
		}
	case *parser.MapLiteral:
		for _, p := range v.Pairs {
			if ExpressionHasHexLiteral(p.Key) || ExpressionHasHexLiteral(p.Value) {
				return true
			}
		}
	}
	return false
}
