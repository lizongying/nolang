package parser

// 糖節點遍歷助手（S1/S4）——給 parser 包**之外**的消費方用。
//
// 背景與 checker/sugar_walk.go 同一理由：每條規則都是手寫的
// `switch node.(type)`，switch 裡沒有的型別會被靜默跳過，整棵子樹對該規則
// 不可見。SurfaceMatch 是**容器**（Matched + N 個 arm，每個 arm 帶
// Condition / MultiValuePatterns / Body），把它的枚舉內聯進每個 walker
// （checker 的 6 個、cmd/no 的 3 個……）等於把維護面乘上 walker 的數量。
//
// checker 自己有一份同語義的未匯出副本（checker/sugar_walk.go，S1 時立的），
// 那份暫時保留：把它改成委派本檔是純等價重構，不併入本次翻轉（最小 diff 原則）。
// 新的消費方（cmd/no 的 fixer walker）直接用這裡的匯出版本。
//
// 回調只收到**直接**子節點；遞迴是 walker 自己的事——正因如此，嵌套的糖無需
// 額外管線：walker 遞進 arm 體後又會撞上內層的 *SurfaceMatch，再次呼叫本助手。

// WalkSurfaceMatchArmBodies 以「臂體語句列表」為單位回調，而不是逐條語句。
//
// 為什麼要有這個版本：有些規則的判定依賴**列表上下文**——最典型的是
// 「這條 #{overflow} 註解管轄它後面的第一條陳述」。逐條回調會把「後面是誰」
// 這個資訊切碎。另一個理由是作用域累積（optunuse 的 predeclared 判定要按
// 語句順序走訪），同樣需要整份列表。
func WalkSurfaceMatchArmBodies(sm *SurfaceMatch, onBody func([]Statement)) {
	if sm == nil || onBody == nil {
		return
	}
	for i := range sm.Arms {
		if sm.Arms[i].Body != nil {
			onBody(sm.Arms[i].Body.Statements)
		}
	}
}

// WalkSurfaceMatchChildrenRW 是 WalkSurfaceMatchChildren 的「可替換」版：
// onExpr 的返回值會**寫回原槽位**（回傳 nil 視為「保持不變」）。
//
// 為什麼單獨立一個而不是把 WalkSurfaceMatchChildren 的 onExpr 改成回傳型：
// 只讀版在 checker 裡有 12 個呼叫點，改簽名會把無關的 diff 攤大。改寫型消費方
// 目前只有兩個（`tools/std_qualify` / `tools/std_methodify`——要把臂內的
// Identifier 換成 DotExpression），它們需要「看見臂內節點」**並且**「改得動它」：
// 沒有這個版本，翻到糖形態只會變成靜默漏改。
func WalkSurfaceMatchChildrenRW(
	sm *SurfaceMatch,
	onExpr func(Expression) Expression,
	onStmt func(Statement),
) {
	if sm == nil {
		return
	}
	if sm.Matched != nil && onExpr != nil {
		sm.Matched = onExpr(sm.Matched)
	}
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		if onExpr != nil {
			if arm.Condition != nil {
				arm.Condition = onExpr(arm.Condition)
			}
			for j, p := range arm.MultiValuePatterns {
				if p != nil {
					arm.MultiValuePatterns[j] = onExpr(p)
				}
			}
		}
		if arm.Body != nil && onStmt != nil {
			for _, inner := range arm.Body.Statements {
				onStmt(inner)
			}
		}
	}
}

// WalkSurfaceMatchChildren 以直接子節點回調遍歷 SurfaceMatch（只讀）。
// onExpr / onStmt 可為 nil，nil 回調即跳過該類別。
//
// 注意本助手**不**枚舉 MultiOptionPatterns：那是 []string（`nil || err` 這種
// 模式名），不是運算式，不承載變數引用。arm 的 Condition 算子節點：糖形態下
// 條件尚未被合併進 `matched == X` 的比較運算式（那是 buildMatchDesugar 的產物），
// 條件裡引用的變數只能從這裡看見。
func WalkSurfaceMatchChildren(
	sm *SurfaceMatch,
	onExpr func(Expression),
	onStmt func(Statement),
) {
	if onExpr == nil {
		WalkSurfaceMatchChildrenRW(sm, nil, onStmt)
		return
	}
	WalkSurfaceMatchChildrenRW(sm, func(e Expression) Expression { onExpr(e); return e }, onStmt)
}
