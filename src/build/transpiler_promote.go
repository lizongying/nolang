package build

import (
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// transpiler_promote.go — 賦值位置上的「整數 → 可提升結構體」隱式提升。
//
// 政策（用戶定調）：`k i64 = 2` 之後 `x bigint = k` 安全且明確 —— bigint 只有
// 一個吃整數的無損構造子 `bigint.from-i64`，所以不要求使用者每次都手寫它。
// 型別表與判定條件全部住在 checker/scalar_promote.go，checker（structasg 要不要
// 報）與本檔（要不要改寫）讀同一張表；兩邊不一致就等於把「checker 放行、
// 後端靜默誤編譯」這條老路放回來。
//
// 落點：newCompileAssembly 在 parser.CloneProgram 之後的工作副本上跑，與
// mangleOverloads / injectEnterLeave 同期（那兩支也在此合成節點）。因此：
//   - ParseProgram 的輸出仍然不可變，S5 的零寫入 pin（ast_write_gate_test.go）
//     不受影響；
//   - 合成的 `bigint.from-i64(...)` 與用戶手寫的完全同形、同階段進入後續的
//     merge / 模組前綴 / 方法解析 / 單態化，不需要任何特例。
//
// 保守邊界（宁可漏改，不可誤改）：
//   - 只認**顯式標注**的裸結構體目標（`*parser.NamedType` 且非 IsInferred）；
//     `?bigint` / `&bigint` 目標不改寫 —— 那還要多打一次 tag，後端沒有這一步。
//   - 右值必須「可證明為整數」：整數字面量、其算術／位運算組合、或型別已在
//     本作用域標注為整數的識別字。呼叫、欄位存取、索引一律不動（推斷不出來）。
//   - 入口檔案自己宣告的同名結構體（如本地 `bigint`）不吃 std 的規則。

// promoteScalarAssignments 在裝配工作副本上做完全程的提升改寫。
func promoteScalarAssignments(program *parser.Program) {
	if program == nil {
		return
	}
	local := entryLocalStructNames(program)
	scope := map[string]string{}
	collectPromoteScope(program.Statements, scope)
	promoteStatements(program.Statements, scope, local)
}

// entryLocalStructNames 收集入口程式自己宣告的結構體名。提升規則以裸名為鍵，
// 同名不同家時必須讓本地宣告勝出（不改寫）。
func entryLocalStructNames(program *parser.Program) map[string]bool {
	names := map[string]bool{}
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok && sd.Name != "" {
			names[sd.Name] = true
		}
	}
	return names
}

// recordPromoteType 只在型別「對提升有訊號意義」時入表：
//   - 整數源型別：它可能是某個提升賦值的右值；
//   - 可提升結構體型別：它出現在右值時代表「已經是結構體值」，不得再包。
//
// 其餘型別不入表 —— 查不到即視為未知、不改寫。表因此幾乎是空的，pass 的代價
// 與既有裝配階段同階，不會拖慢自舉編譯。
func recordPromoteType(scope map[string]string, name, typ string) {
	if name == "" || typ == "" {
		return
	}
	if checker.IsPromotableSource(typ) || checker.IsPromotableTarget(typ) {
		scope[name] = typ
	}
}

// collectPromoteScope 收集作用域內的宣告型別（頂層與函式體各自成表，函式定義
// 節點本身不往下收 —— 它由 promoteStatements 用子作用域處理）。
func collectPromoteScope(stmts []parser.Statement, scope map[string]string) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name == nil || s.IsSynthetic {
				continue
			}
			if nt, ok := s.Type.(*parser.NamedType); ok && !nt.IsInferred {
				recordPromoteType(scope, s.Name.Value, nt.Value)
			}
		case *parser.FunctionDefinition:
			// 獨立作用域，由 promoteStatements 處理。
		case *parser.BlockStatement:
			collectPromoteScope(s.Statements, scope)
		case *parser.ForStatement:
			if s.Init != nil {
				collectPromoteScope([]parser.Statement{s.Init}, scope)
			}
			if s.Update != nil {
				collectPromoteScope([]parser.Statement{s.Update}, scope)
			}
			if s.Body != nil {
				collectPromoteScope(s.Body.Statements, scope)
			}
		case *parser.ExpressionStatement:
			collectPromoteScopeExpr(s.Expression, scope)
		}
	}
}

// collectPromoteScopeExpr 走遍表達式裡帶區塊的位置（if / 三元），讓區塊內的
// 宣告也進入同一個作用域。
func collectPromoteScopeExpr(expr parser.Expression, scope map[string]string) {
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			collectPromoteScope(e.Consequence.Statements, scope)
		}
		if e.Alternative != nil {
			collectPromoteScope(e.Alternative.Statements, scope)
		}
	case *parser.GroupedExpression:
		collectPromoteScopeExpr(e.Expression, scope)
	case *parser.ConditionalExpression:
		collectPromoteScopeExpr(e.Consequence, scope)
		collectPromoteScopeExpr(e.Alternative, scope)
	}
}

// promoteStatements 改寫 stmts 中的提升賦值，並遞迴進同作用域的巢狀區塊。
func promoteStatements(stmts []parser.Statement, scope map[string]string, local map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			inner := make(map[string]string, len(scope))
			for k, v := range scope {
				inner[k] = v
			}
			for _, p := range s.Parameters {
				if nt, ok := p.Type.(*parser.NamedType); ok && !nt.IsInferred {
					recordPromoteType(inner, p.Name, nt.Value)
				}
			}
			for _, r := range parser.DeclaredResults(s) {
				if nt, ok := r.Type.(*parser.NamedType); ok && !nt.IsInferred {
					recordPromoteType(inner, r.Name, nt.Value)
				}
			}
			if s.Body != nil {
				collectPromoteScope(s.Body.Statements, inner)
				promoteStatements(s.Body.Statements, inner, local)
			}
		case *parser.LetStatement:
			promoteScopeInExpr(s.Value, scope, local)
			if s.Name != nil && !s.IsSynthetic && s.Value != nil {
				if target := promoteTargetType(s, scope); target != "" {
					if ctor := promotionCtorFor(target, s.Value, scope, local); ctor != "" {
						s.Value = wrapPromotionCall(ctor, s.Value)
					}
				}
			}
		case *parser.MultiAssignStatement:
			promoteScopeInExpr(s.Value, scope, local)
			if len(s.Targets) == 1 && s.Value != nil {
				if ident, ok := s.Targets[0].(*parser.Identifier); ok {
					if declared := scope[ident.Value]; declared != "" {
						if ctor := promotionCtorFor(declared, s.Value, scope, local); ctor != "" {
							s.Value = wrapPromotionCall(ctor, s.Value)
						}
					}
				}
			}
		case *parser.BlockStatement:
			promoteStatements(s.Statements, scope, local)
		case *parser.ForStatement:
			if s.Init != nil {
				promoteStatements([]parser.Statement{s.Init}, scope, local)
			}
			if s.Update != nil {
				promoteStatements([]parser.Statement{s.Update}, scope, local)
			}
			if s.Body != nil {
				promoteStatements(s.Body.Statements, scope, local)
			}
		case *parser.ExpressionStatement:
			promoteScopeInExpr(s.Expression, scope, local)
		case *parser.UnwrapAssignStatement:
			promoteScopeInExpr(s.Value, scope, local)
		case *parser.ReturnStatement:
			promoteScopeInExpr(s.ReturnValue, scope, local)
		}
	}
}

func promoteScopeInExpr(expr parser.Expression, scope map[string]string, local map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			promoteStatements(e.Consequence.Statements, scope, local)
		}
		if e.Alternative != nil {
			promoteStatements(e.Alternative.Statements, scope, local)
		}
	case *parser.GroupedExpression:
		promoteScopeInExpr(e.Expression, scope, local)
	case *parser.ConditionalExpression:
		promoteScopeInExpr(e.Consequence, scope, local)
		promoteScopeInExpr(e.Alternative, scope, local)
	}
}

// promotionCtorFor 回報把右值隱式提升進目標結構體所需的構造子名（不需要提升
// 時回 ""）。三個條件缺一不可：目標是裸結構體且非本地同名宣告、源型別在規則
// 的 from 集合裡、且右值可證明為該源型別。
func promotionCtorFor(target string, value parser.Expression, scope map[string]string, local map[string]bool) string {
	if target == "" || local[target] {
		return ""
	}
	return checker.ScalarPromotionCtor(target, provableIntegerType(value, scope))
}

// promoteTargetType 取一條 let 的「目標結構體型別」：顯式標注的裸結構體名，或
// 無標注的**重新賦值**（parser 把 `x = k` 也parse成 LetStatement，Type 為 nil）
// 則沿用同作用域先前記錄的宣告型別。
//
// 只在 Type == nil 時回查表：Type 存在但不是裸 NamedType（`?bigint`、`[]i64`）
// 代表這次的宣告形態本身不吃提升，不能拿舊記錄頂替。
func promoteTargetType(s *parser.LetStatement, scope map[string]string) string {
	if s.Type == nil {
		return scope[s.Name.Value]
	}
	if nt, ok := s.Type.(*parser.NamedType); ok && !nt.IsInferred {
		return nt.Value
	}
	return ""
}

// provableIntegerType 回報 expr 可靜態證明為整數時的型別拼寫；證明不了回 ""。
//
// 與 checker 側的 provableScalarType 同一條紀律：只認不依賴推斷的形狀。識別字
// 查本作用域的標注（表裡只會有整數型別與可提升結構體型別，後者回 ""）；呼叫、
// 欄位存取、索引一律回 ""——它們的型別在此階段還不可知，誤包會把合法寫法打壞。
func provableIntegerType(expr parser.Expression, scope map[string]string) string {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		// 十六進位字面量按 byte、十進位按 i64 推斷（與 inferTypeFromExpr 同約定），
		// 兩者都在 bigint 的 from 集合裡，都能無損進 i64。
		raw := e.Token.Literal
		if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
			return "byte"
		}
		return "i64"
	case *parser.GroupedExpression:
		return provableIntegerType(e.Expression, scope)
	case *parser.PrefixExpression:
		switch e.Operator {
		case "-", "+", "~":
			return provableIntegerType(e.Right, scope)
		}
	case *parser.InfixExpression:
		// 比較／邏輯運算出 bool，不是整數；其餘算術／位運算兩側都必須已被證明。
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return ""
		}
		if l := provableIntegerType(e.Left, scope); l != "" && provableIntegerType(e.Right, scope) != "" {
			return l
		}
	case *parser.Identifier:
		if t := scope[e.Value]; t != "" && checker.IsPromotableSource(t) {
			return t
		}
	}
	return ""
}

// wrapPromotionCall 以源碼書寫形態合成 `<ctor>(arg)`。節點形狀與 parser 解析
// `bigint.from-i64(5)` 的結果一致（模組限定呼叫 = DotExpression 掛 Identifier
// 接收者），位置沿用原右值，診斷行號不會跳到別處。
func wrapPromotionCall(ctor string, arg parser.Expression) parser.Expression {
	mod, prop := splitQualified(ctor)
	pos := arg.Pos()
	return &parser.CallExpression{
		Token: lexer.Token{Type: lexer.LPAREN, Literal: "(", Line: pos.Line, Column: pos.Column},
		Function: &parser.DotExpression{
			Token: lexer.Token{Type: lexer.DOT, Literal: ".", Line: pos.Line, Column: pos.Column},
			Receiver: &parser.Identifier{
				Token: lexer.Token{Type: lexer.IDENT, Literal: mod, Line: pos.Line, Column: pos.Column},
				Value: mod,
			},
			Property: prop,
		},
		Arguments: []parser.Expression{arg},
	}
}

// splitQualified 把 "bigint.from-i64" 拆成模組短名與函式名。
func splitQualified(qualified string) (string, string) {
	for i := len(qualified) - 1; i >= 0; i-- {
		if qualified[i] == '.' {
			return qualified[:i], qualified[i+1:]
		}
	}
	return "", qualified
}
