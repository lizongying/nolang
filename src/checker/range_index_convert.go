package checker

import (
	"github.com/lizongying/nolang/parser"
)

// 本文件回答一個問題：這個「`i <- [a..b): { … base[i] … }`」迴圈改寫成
// 「`x <- base: { … x … }`」是否**等價**。只有等價時 ValidateRangeIndexLoops
// 才發 HINT —— 否則提示就是一張兌現不了的支票（丟掉下標會改變行為）。
//
// 判定條件（全部滿足才算 convertible）：
//  1. 下標在迴圈體裡只當**同一個**容器的下標用；一旦它還索引別的容器
//     （`dst[i]: { ok -> it = src[i] }`）或被當值使用（`print(i)`、`i*16+j`、
//     `s[i..i+2]`），它擔負的是「對齊 / 定位」職責，容器迭代無法表達；
//  2. 沒有 `base[i] = v` 直寫（那需要 i 作為可寫位置）；
//  3. 基底是可迭代的裸變數：`.field[i]` 的接收者欄位不能出現在 `x <- …` 右側；
//  4. 迴圈體內沒有重新綁定同名下標（巢狀迴圈 / let / 閉包）；
//  5. 區間可證明**恰好覆蓋整個容器**：起點為 0、`[0..base.len())`，或上界字面量
//     等於宣告長度。前綴視窗（`[0..total-n)`）會少寫元素，不提示；
//  6. 遇到看不懂的 AST 結構一律保守放棄。

// rngidxScope 是判定所需的上下文：函式（或頂層）同一層的陳述集合，加上
// 函式參數的宣告型別（`f = (a [3]i64) …` 裡 `i <- [0..3)` 的長度就住在簽名上）。
type rngidxScope struct {
	stmts  []parser.Statement
	params map[string]parser.Type
}

// rngidxNewScope 組出函式體的作用域；params 為函式簽名的參數（頂層傳 nil）。
func rngidxNewScope(stmts []parser.Statement, params []*parser.Parameter) *rngidxScope {
	sc := &rngidxScope{stmts: stmts, params: map[string]parser.Type{}}
	for _, p := range params {
		if p != nil && p.Name != "" && p.Type != nil {
			sc.params[p.Name] = p.Type
		}
	}
	return sc
}

// rngidxConvertTarget 回傳「這個迴圈該迭代哪個容器」；ok=false 表示不該提示。
func rngidxConvertTarget(st *parser.ForStatement, scope *rngidxScope) (string, bool) {
	if st == nil || st.IterRange == nil || st.IterRange.Range == nil || st.Body == nil || scope == nil {
		return "", false
	}
	name := st.IterRange.Variable
	if name == "" {
		return "", false
	}
	u := &rngidxUse{name: name, bases: map[string]bool{}}
	u.stmts(st.Body.Statements)
	if u.bail {
		return "", false
	}
	if len(u.bases) != 1 {
		return "", false // 0 個：下標根本沒索引容器；多個：它在對齊容器
	}
	base := ""
	for b := range u.bases {
		base = b
	}
	if base == "" || base == name {
		return "", false
	}
	if !rngidxSweepCovers(base, st.IterRange.Range, scope) {
		return "", false
	}
	return base, true
}

// rngidxUse 累積一次掃描的結論；bail 一旦置位就不再關心細節。
type rngidxUse struct {
	name  string
	bases map[string]bool
	bail  bool
}

func (u *rngidxUse) markBail() { u.bail = true }

// expr 走查表達式。allowedIdx 表示當前位置是 `base[name]` 的下標槽（唯一允許
// 直接出現 name 的位置）。
func (u *rngidxUse) expr(e parser.Expression, allowedIdx bool) {
	if u.bail || e == nil {
		return
	}
	switch x := e.(type) {
	case *parser.Identifier:
		if x.Value == u.name && !allowedIdx {
			u.markBail() // 下標被當值用
		}
	case *parser.IndexExpression:
		if id, ok := x.Index.(*parser.Identifier); ok && id.Value == u.name {
			// name 作為下標：记下基底（非裸識別符 → 無法迭代）。
			if b, ok := x.Left.(*parser.Identifier); ok {
				if b.Value == u.name {
					u.markBail()
				} else {
					u.bases[b.Value] = true
				}
			} else {
				u.markBail() // `.field[i]` / `m[k][i]` 之類的基底不能出現在 `x <- …` 右側
				return
			}
			u.expr(x.Left, false)
			return
		}
		u.expr(x.Left, false)
		u.expr(x.Index, false)
	case *parser.AssignExpression:
		// `base[name] = v` 需要可寫位置，改寫會丟掉它。
		if idx, ok := x.Left.(*parser.IndexExpression); ok {
			if id, ok := idx.Index.(*parser.Identifier); ok && id.Value == u.name {
				u.markBail()
				return
			}
		}
		u.expr(x.Left, false)
		u.expr(x.Value, false)
	case *parser.InfixExpression:
		u.expr(x.Left, false)
		u.expr(x.Right, false)
	case *parser.PrefixExpression:
		u.expr(x.Right, false)
	case *parser.GroupedExpression:
		u.expr(x.Expression, false)
	case *parser.ConditionalExpression:
		u.expr(x.Condition, false)
		u.expr(x.Consequence, false)
		u.expr(x.Alternative, false)
	case *parser.CallExpression:
		u.expr(x.Function, false)
		for _, g := range x.GenericArgs {
			u.expr(g, false)
		}
		for _, a := range x.Arguments {
			u.expr(a, false)
		}
	case *parser.DotExpression:
		u.expr(x.Receiver, false)
	case *parser.AwaitExpression:
		u.expr(x.Right, false)
	case *parser.CoExpression:
		u.expr(x.Call, false)
	case *parser.RunExpression:
		u.expr(x.Call, false)
	case *parser.CastExpression:
		u.expr(x.Expr, false)
	case *parser.SliceExpression:
		u.expr(x.Left, false)
		if x.Range != nil {
			u.expr(x.Range.Start, false)
			u.expr(x.Range.End, false)
		}
	case *parser.RangeExpression:
		u.expr(x.Start, false)
		u.expr(x.End, false)
	case *parser.IterationExpr:
		u.expr(x.RangeExpr, false)
		if x.Range != nil {
			u.expr(x.Range.Start, false)
			u.expr(x.Range.End, false)
		}
	case *parser.ArrayLiteral:
		u.expr(x.Size, false)
		for _, el := range x.Elements {
			u.expr(el, false)
		}
	case *parser.SliceLiteral:
		for _, el := range x.Elements {
			u.expr(el, false)
		}
	case *parser.MapLiteral:
		for _, p := range x.Pairs {
			u.expr(p.Key, false)
			u.expr(p.Value, false)
		}
	case *parser.StructLiteral:
		for _, f := range x.Fields {
			if f != nil {
				u.expr(f.Value, false)
			}
		}
	case *parser.SurfaceMatch:
		u.expr(x.Matched, false)
		for i := range x.Arms {
			arm := &x.Arms[i]
			if arm.BindingName == u.name || arm.TypeBindingName == u.name {
				u.markBail() // 具名臂綁定蓋掉同名下標
			}
			for _, bn := range arm.BindingNames {
				if bn == u.name {
					u.markBail()
				}
			}
			u.expr(arm.Condition, false)
			for _, p := range arm.MultiValuePatterns {
				u.expr(p, false)
			}
			if arm.Body != nil {
				u.stmts(arm.Body.Statements)
			}
		}
	case *parser.IfExpression:
		u.expr(x.Condition, false)
		if x.Consequence != nil {
			u.stmts(x.Consequence.Statements)
		}
		if x.Alternative != nil {
			u.stmts(x.Alternative.Statements)
		}
	case *parser.UnwrapAssignStatement:
		// `v ?= expr`：v 若正是下標，改寫後沒有可寫位置；expr 裡的讀取照常走查。
		if x.Name != nil && x.Name.Value == u.name {
			u.markBail()
		}
		u.expr(x.Target, false)
		u.expr(x.Value, false)
	case *parser.FunctionLiteral:
		// 閉包內的讀取跨越了作用域邊界，保守放棄。
		u.markBail()
	case *parser.IntegerLiteral, *parser.FloatLiteral, *parser.StringLiteral,
		*parser.CharLiteral, *parser.BooleanLiteral, *parser.NilLiteral, *parser.RegexLiteral:
		// 字面量不含下標引用。
	default:
		// 其他結構（含未預期的型別節點）保守放棄，寧可不提示。
		u.markBail()
	}
}

func (u *rngidxUse) stmts(ss []parser.Statement) {
	for _, s := range ss {
		if u.bail {
			return
		}
		u.stmt(s)
	}
}

func (u *rngidxUse) stmt(s parser.Statement) {
	if u.bail || s == nil {
		return
	}
	switch st := s.(type) {
	case *parser.LetStatement:
		if st.Name != nil && st.Name.Value == u.name {
			u.markBail() // 同名重新綁定
		}
		u.expr(st.Value, false)
	case *parser.ExpressionStatement:
		u.expr(st.Expression, false)
	case *parser.MultiAssignStatement:
		for _, t := range st.Targets {
			if id, ok := t.(*parser.Identifier); ok && id.Value == u.name {
				u.markBail()
			}
			if idx, ok := t.(*parser.IndexExpression); ok {
				if id, ok := idx.Index.(*parser.Identifier); ok && id.Value == u.name {
					u.markBail()
				}
			}
		}
		u.expr(st.Value, false)
	case *parser.UnwrapAssignStatement:
		u.expr(st, false)
	case *parser.ReturnStatement:
		u.expr(st.ReturnValue, false)
	case *parser.BlockStatement:
		u.stmts(st.Statements)
	case *parser.ForStatement:
		if st.IterRange != nil && st.IterRange.Variable == u.name {
			u.markBail() // 巢狀迴圈重綁同名下標
		}
		if st.IterRange != nil {
			u.expr(st.IterRange.RangeExpr, false)
			if st.IterRange.Range != nil {
				u.expr(st.IterRange.Range.Start, false)
				u.expr(st.IterRange.Range.End, false)
			}
		}
		if st.Body != nil {
			u.stmts(st.Body.Statements)
		}
	case *parser.AnnotationStatement:
		// 純註解不引用下標。
	default:
		// 巢狀定義 / break / continue 等：保守放棄。
		u.markBail()
	}
}

// rngidxSweepCovers 判定 `[start..end]` 是否恰好覆蓋 base 的全部元素。
func rngidxSweepCovers(base string, r *parser.RangeExpression, scope *rngidxScope) bool {
	if !r.LeftInc {
		return false // `(0..N)` 少走第 0 個元素
	}
	if r.Start != nil {
		n, ok := rngidxInt(r.Start)
		if !ok || n != 0 {
			return false
		}
	}
	if r.End == nil {
		return false
	}
	if rngidxLenCallOf(r.End, base) {
		return !r.RightInc // `[0..base.len())` 就是整段迭代
	}
	end, ok := rngidxInt(r.End)
	if !ok {
		return false
	}
	declared, ok := rngidxDeclaredLen(scope, base)
	if !ok {
		return false
	}
	last := int(end)
	if r.RightInc {
		last++ // `[0..N]` 走 N+1 個元素
	}
	return last == declared
}

// rngidxLenCallOf 認出 `base.len()`（無引數、接收者正是 base）。
func rngidxLenCallOf(e parser.Expression, base string) bool {
	call, ok := e.(*parser.CallExpression)
	if !ok || len(call.Arguments) != 0 {
		return false
	}
	dot, ok := call.Function.(*parser.DotExpression)
	if !ok || dot.Property != "len" {
		return false
	}
	id, ok := dot.Receiver.(*parser.Identifier)
	return ok && id.Value == base
}

// rngidxInt 取整數字面量。
func rngidxInt(e parser.Expression) (int64, bool) {
	lit, ok := e.(*parser.IntegerLiteral)
	if !ok {
		return 0, false
	}
	return lit.Value, true
}

// rngidxDeclaredLen 極力量出 `base` 的宣告長度：先看函式參數型別，再看同層（函式體
// / 頂層）的 `base [N]t`、`base = [v0, v1, …]`、`base = with-len(N)` 宣告。量不出來
// 就回 false —— 寧可不提示，也不給一張會多寫 / 少寫元素的支票。
func rngidxDeclaredLen(scope *rngidxScope, base string) (int, bool) {
	if t, ok := scope.params[base]; ok {
		return arrayTypeLen(t)
	}
	found := 0
	length := 0
	for _, s := range scope.stmts {
		ls, ok := s.(*parser.LetStatement)
		if !ok || ls.Name == nil || ls.Name.Value != base {
			continue
		}
		found++
		if found > 1 {
			return 0, false // 重複宣告：長度不唯一
		}
		if ls.Type != nil {
			if at, ok := ls.Type.(*parser.ArrayType); ok && at.Size != nil {
				if n, ok2 := rngidxInt(at.Size); ok2 && n > 0 {
					length = int(n)
					continue
				}
				return 0, false // 宣告了但長度不是字面量
			}
		}
		switch v := ls.Value.(type) {
		case *parser.ArrayLiteral:
			if len(v.Elements) > 0 {
				length = len(v.Elements)
				continue
			}
			if n, ok2 := rngidxInt(v.Size); ok2 && n > 0 {
				length = int(n)
				continue
			}
		case *parser.CallExpression:
			if leaf, ok2 := rngidxCalleeLeaf(v); ok2 && (leaf == "with-len" || leaf == "with-cap-len") {
				if n, ok3 := rngidxInt(v.Arguments[0]); ok3 && n > 0 {
					length = int(n)
					continue
				}
			}
		}
		return 0, false
	}
	if found != 1 {
		return 0, false
	}
	return length, true
}

// arrayTypeLen 從參數型別 `[N]t` 取出行數；`[]t` / `[?]t` / vec / map 回 false。
func arrayTypeLen(t parser.Type) (int, bool) {
	at, ok := t.(*parser.ArrayType)
	if !ok || at.Size == nil {
		return 0, false
	}
	n, ok := rngidxInt(at.Size)
	if !ok || n <= 0 {
		return 0, false
	}
	return int(n), true
}

// rngidxCalleeLeaf 取呼叫目標的末段名稱（去掉模組前綴），供 `with-len(N)` 判定。
func rngidxCalleeLeaf(call *parser.CallExpression) (string, bool) {
	if len(call.Arguments) != 1 {
		return "", false
	}
	switch f := call.Function.(type) {
	case *parser.Identifier:
		name := f.Value
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '.' {
				name = name[i+1:]
				break
			}
		}
		return name, true
	}
	return "", false
}
