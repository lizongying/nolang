package checker

import "github.com/lizongying/nolang/parser"

// ---- S1：糖節點遍歷助手 ----------------------------------------------------
//
// checker 今天跑在兩種 AST 形態上，S1 會加上第三種：
//
//	lowered  ParseProgram 默認路徑：所有糖已展開。`no build` / `no vet`。
//	surface  SkipUnwrapLowering + SkipSafeIndexLowering。LSP / `no fmt` 今天就用。
//	sugar    SkipLowering：連 SurfaceMatch 都存活。S1 的目標形態。
//
// 每條規則都是手寫的 `switch node.(type)`。**switch 裡沒有的型別會被靜默跳過**，
// 於是整棵子樹對該規則不可見——不是報錯，是沉默。S1 之前存活的糖是
// GroupedExpression / UnwrapAssignStatement / MultiAssignStatement，各規則
// 各自長了一個 case（見 checker/index_write_test.go:14 立的規矩：
// 「surface AST 與降級 AST 兩端口徑一致」）。
//
// SurfaceMatch 不一樣：它是**容器**（Matched + N 個 arm，每個 arm 帶
// Condition / MultiValuePatterns / Body）。把它的枚舉內聯進每個 walker，等於把
// 維護面乘上 walker 的數量。故集中在這裡：**新增一個糖節點、或 MatchArm 新增
// 一個欄位，都只改這一個檔案。**
//
// 回調只收到**直接**子節點；遞迴是 walker 自己的事——正因如此，嵌套的糖無需
// 額外管線：walker 遞進 arm 體後又會撞上內層的 *SurfaceMatch，再次呼叫本助手。
//
// 為什麼 arm 的 Condition 也算子節點：糖形態下條件尚未被合併進 `matched == X`
// 的比較運算式（那是 buildMatchDesugar 的產物），條件裡引用的變數只能從這裡看見。
//
// 注意本助手**不**枚舉 MultiOptionPatterns：那是 `[]string`（`nil || err` 這種
// 模式名），不是運算式，不承載變數引用。

// walkSurfaceMatchChildren 以直接子節點回調遍歷 SurfaceMatch（只讀）。
// onExpr / onStmt 可為 nil，nil 回調即跳過該類別。
//
// 本函式是 parser.WalkSurfaceMatchChildren 的薄委派。S4 期間把權威實作收進
// parser/sugar_walk.go（給 parser 包之外的消費方用），這份 checker 內的副本改成
// 轉發，行為逐位元相同——保留函式名是為了不動 12 個呼叫點。
func walkSurfaceMatchChildren(
	sm *parser.SurfaceMatch,
	onExpr func(parser.Expression),
	onStmt func(parser.Statement),
) {
	parser.WalkSurfaceMatchChildren(sm, onExpr, onStmt)
}

// walkSurfaceMatchArmBodies 以「臂體語句列表」為單位回調，而不是逐條語句。
//
// 為什麼要有這個版本：有些規則的判定依賴**列表上下文**——最典型的是
// OverflowAnnotationRelevance 的「這條 #{overflow} 註解管轄它後面的第一條陳述」。
// 逐條回調（walkSurfaceMatchChildren 的 onStmt）會把「後面是誰」這個資訊切碎，
// 使註解查不到被管轄者，`no fmt` 便把有效的 overflow 註解誤刪。
func walkSurfaceMatchArmBodies(sm *parser.SurfaceMatch, onBody func([]parser.Statement)) {
	if sm == nil || onBody == nil {
		return
	}
	for i := range sm.Arms {
		if sm.Arms[i].Body != nil {
			onBody(sm.Arms[i].Body.Statements)
		}
	}
}

// surfaceMatchArmBindings 回報一個 arm 在其臂體內**引入**的綁定名
// （`ok(v) ->` 的 v、`rect(w, h) ->` 的 w/h、類型臂 `v i32 ->` 的 v）。
//
// 為什麼需要它：糖形態下這些名字只存在於 MatchArm 的元資料裡；降糖後它們變成
// 臂體開頭的合成 `v = __unwrap_N`（真正寫進 AST 的 let），walker 才看得見。
// 因此「綁定名算不算一次定義 / 算不算已消費」在兩種形態下由不同機制回答，
// 規則若要跨形態一致，必須自己把綁定名考慮進去。
//
// 回傳 nil 表示該臂沿用隱含的 `it`（沒有顯式綁定名）。
func surfaceMatchArmBindings(arm *parser.MatchArm) []string {
	if arm == nil {
		return nil
	}
	var out []string
	if arm.BindingName != "" {
		out = append(out, arm.BindingName)
	}
	out = append(out, arm.BindingNames...)
	if arm.TypeBindingName != "" {
		out = append(out, arm.TypeBindingName)
	}
	return out
}
