package fmt

// 「全臂同一目標賦值」的 match 折疊（no fmt 常規規則，非 --fix 子類）。
//
// 形態：語句位置的 match，每個臂都只寫一條「給同一個變數賦值」的陳述——
//
//	m: {
//	    read -> flags = O-RDONLY
//	    -> flags = 0
//	}
//
// 這種寫法把「求值結果」這件事藏進臂裡：match 本身被當成語句、其值被丟棄，而
// 使用者真正想要的「用匹配結果給 flags 賦值」只能靠每個臂各自重複一次賦值來表達。
// 折疊後值語境直接浮到面上：
//
//	flags = m: {
//	    read -> O-RDONLY
//	    -> 0
//	}
//
// 這也是 `?=` 脫糖的目標形態，對齊後同一段邏輯不會出現兩種寫法。
//
// ── 為什麼只在「語句位置」折疊 ──────────────────────────────
// 折疊會把 match 從陳述語境搬到值語境（賦值右值），而 parser 的 ValueCtx 會影響
// 空臂的降糖（值語境的空臂合成零值、陳述語境是 no-op）。只有「整條陳述恰好是
// 這個 match」（`ExpressionStatement.Expression == *SurfaceMatch`）時才動手；
// `x = a: {…}`（已經是目標形態）不會觸發，因為那時陳述是 AssignExpression/
// LetStatement 而非裸 match，故本規則天然冪等。
//
// ── 安全性判據（全部必要） ──────────────────────────────────
//  1. 必須有 catch-all 臂。無 catch-all 的 match 在「沒有臂命中」時不會執行任何
//     臂；折疊後 `v = subj: {…}` 變成對 v 的一次賦值，值語境的空結果會被合成
//     零值覆蓋 v——這是行為改變。有 catch-all ⇒ match 恆有值，折疊恆等價。
//  2. 每臂恰好一條、且是對**同一**簡單識別字的賦值（`v = …`）。臂內若還有別的
//     陳述（副作用、return、嵌套 match），折疊就必須搬移語句，超出排版範疇。
//  3. 每臂的 body 必須是 inline 形態（`cond -> v = x`，非 `cond -> { … }`）。
//     區塊形態折疊後要不要留大括號是有爭議的排版決定，保守跳過。
//  4. 臂陳述不得帶任何註解（`#{overflow=…}` 等）與 doc 註釋、
//     無 `{` 同行註釋、無尾隨註釋。這些註釋在臂內的位置資訊折疊後無處安放。
//  5. 目標不得帶顯式型別標註（`v i64 = …`）。折疊會把標註搬到 match 外層，
//     但各臂標註可能不一致，無法在純排版層裁決。
//  6. 主體不得是隱含 self（`.: { … }`）。fmt 把 `self` 印成 `.`，折疊後成
//     `v = .: { … }`——實測 `no` 會把它按「self 型別」解讀並報
//     cannot assign code value to str（見 2026-11-06 復現），語法不可用。
//  7. 類型解構臂（`v i64 -> …`）跳過：糖形態下這類臂的迴印/推斷本就不穩
//     （docs/design/nolang-parser-lowering-invariants.md §21.7 #5）。
//  8. 任一臂的值是 option 構造（`ok(x)` / `err(x)` / `nil`）則放棄：賦值點的隱式
//     包裝語義會在折疊後消失，混合裸值與包裝值會讓 match 產出型別自相矛盾。

import (
	"github.com/lizongying/nolang/parser"
)

// foldableMatchAssign 判定 sm 是否可折疊為 `name = sm: { … }`。
// 可折疊時回傳目標變數名與「每臂應產出的值表達式」（鍵為臂指標，供
// writeMatchArm 取用），否則 ok=false。
func (f *formatter) foldableMatchAssign(sm *parser.SurfaceMatch) (string, map[*parser.MatchArm]parser.Expression, bool) {
	if sm == nil || sm.Matched == nil || len(sm.Arms) < 2 {
		return "", nil, false
	}
	// (6) 隱含 self 主體不可折疊。
	if id, ok := sm.Matched.(*parser.Identifier); ok && id.Value == "self" {
		return "", nil, false
	}
	// (1) 必須有 catch-all 臂（`-> …` / `_ -> …`），否則折疊會引入「無臂命中即
	// 賦零值」的行為差異。
	hasCatchAll := false
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		if arm.IsWildcard && !arm.IsDotVal && arm.Condition == nil &&
			len(arm.MultiOptionPatterns) == 0 && len(arm.MultiValuePatterns) == 0 &&
			!arm.IsTypeArm {
			hasCatchAll = true
			break
		}
	}
	if !hasCatchAll {
		return "", nil, false
	}
	name := ""
	values := make(map[*parser.MatchArm]parser.Expression, len(sm.Arms))
	for i := range sm.Arms {
		arm := &sm.Arms[i]
		if arm.IsTypeArm { // (7)
			return "", nil, false
		}
		if arm.Body == nil || arm.IsBlockBody || !arm.Body.IsInline || len(arm.Body.Statements) != 1 {
			return "", nil, false // (2)(3)
		}
		if arm.Body.TrailingComments != nil || arm.Body.ClosingBraceComment != nil || f.obcOf(arm.Body) != nil {
			return "", nil, false // (4)
		}
		stmt := arm.Body.Statements[0]
		if f.hasDocComment(stmt) || len(f.attachedAnnotations(stmt)) > 0 {
			return "", nil, false // (4)
		}
		armName, val, ok := foldArmAssign(stmt)
		if !ok {
			return "", nil, false // (2)(5)
		}
		if isOptionCtorValue(val) {
			return "", nil, false // (8)
		}
		if name == "" {
			name = armName
		} else if name != armName {
			return "", nil, false
		}
		values[arm] = val
	}
	if name == "" {
		return "", nil, false
	}
	return name, values, true
}

// foldArmAssign 從臂內的唯一陳述取出「目標名 + 產出值」。接受兩種拼寫：
//
//	v = expr     → LetStatement（nolang 的賦值在 AST 上就是 LetStatement）
//	v = expr     → ExpressionStatement{AssignExpression{Left: Identifier}}
//
// 目標必須是簡單識別字（不求值 `self.x` / `a[i]`——折疊後寫不回原位），且不得
// 帶顯式型別標註（判據 5）。
func foldArmAssign(stmt parser.Statement) (string, parser.Expression, bool) {
	var name string
	var value parser.Expression
	switch v := stmt.(type) {
	case *parser.LetStatement:
		if v.IsSynthetic || v.Name == nil || v.Value == nil {
			return "", nil, false
		}
		if !typeIsInferred(v.Type) {
			return "", nil, false
		}
		name, value = v.Name.Value, v.Value
	case *parser.ExpressionStatement:
		as, ok := v.Expression.(*parser.AssignExpression)
		if !ok || as == nil || as.IsSynthetic {
			return "", nil, false
		}
		id, ok := as.Left.(*parser.Identifier)
		if !ok || id == nil || as.Value == nil {
			return "", nil, false
		}
		name, value = id.Value, as.Value
	default:
		return "", nil, false
	}
	if name == "" || value == nil {
		return "", nil, false
	}
	// 嵌套賦值（`v = w = x`）不是單一產出值，折疊會把它拆壞。
	if _, isAssign := value.(*parser.AssignExpression); isAssign {
		return "", nil, false
	}
	return name, value, true
}

// typeIsInferred 報告型別標註是否為「parser 推斷」（而非源碼顯式寫下）。
// nil 型別、NamedType.IsInferred 都算推斷。
func typeIsInferred(t parser.Type) bool {
	if t == nil {
		return true
	}
	nt, ok := t.(*parser.NamedType)
	if !ok || nt == nil {
		return false // 切片 / 指標等顯式標註：不折疊
	}
	return nt.IsInferred
}

// isOptionCtorValue 報告臂的產出值是否為「option 包裝」形態（`ok(x)` / `err(x)` / `nil`）。
//
// 判據 (8)：賦值目標若是 option 型別（`out ?bool`），臂內的 `out = true` 依賴的是
// **賦值點的隱式 ok() 包裝**，而 `out = err(it)` 寫的是已包裝的值。折疊後每個臂的
// 值是 match 的**產出型別**，包裝不再發生，兩臂就變成 `?bool` vs `bool` 而報
// 「match arm has inconsistent return types: i64 vs str」（2026-11-06 在
// tests/err-structured.no、match-err-msg-scalar.no、match-it-scope.no 實測）。
//
// formatter 沒有型別資訊，無法判斷目標是不是 option；但「有臂寫了包裝值」正是這類
// 混用的**必要條件**——全部臂都是裸值時型別由值本身決定，折疊後外層賦值仍會包裝，
// 行為不變。故只要有一臂是選項構造就整組放棄折疊（保守、可解釋）。
func isOptionCtorValue(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.CallExpression:
		if id, ok := v.Function.(*parser.Identifier); ok && id != nil {
			return id.Value == "ok" || id.Value == "err"
		}
	case *parser.Identifier:
		// `nil` 在 AST 裡是識別字（部分路徑是 NilLiteral），兩種都認。
		return v != nil && v.Value == "nil"
	case *parser.NilLiteral:
		return true
	}
	return false
}
