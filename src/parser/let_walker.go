package parser

// let_walker.go — 依型別標註位置定位 LetStatement 的單一 walk 口徑。
//
// `no fmt` 的冗餘標註移除（cmd/no/main.go）與 LSP format-on-save 的主動改寫
// （src/lsp/fmt_rewrite.go）都從 checker 的報告拿到「型別標註的 Line/Column」，
// 再回到 AST 找節點。過去兩邊各寫了一份逐字相同的遞迴 walk，任何一方補了
// 新的語句形態（例如 SurfaceMatch 臂、函式字面值）另一方就會靜默漏配。
// 這裡收成單一來源：下鑽形狀改一次，兩條管道同時生效。

// FindLetByTypePos 回傳型別標註 Pos() 等於 (line, col) 的 LetStatement。
// 只認顯式標註（ls.Type != nil）；program 為 nil 或找不到時回傳 nil。
func FindLetByTypePos(program *Program, line, col int) *LetStatement {
	if program == nil {
		return nil
	}
	var found *LetStatement
	var walk func(stmts []Statement)
	walk = func(stmts []Statement) {
		for _, s := range stmts {
			if s == nil || found != nil {
				continue
			}
			if ls, ok := s.(*LetStatement); ok {
				if ls.Type != nil && ls.Type.Pos().Line == line && ls.Type.Pos().Column == col {
					found = ls
					return
				}
			}
			switch v := s.(type) {
			case *FunctionDefinition:
				if v.Body != nil {
					walk(v.Body.Statements)
				}
			case *ForStatement:
				if v.Init != nil {
					walk([]Statement{v.Init})
				}
				if v.Update != nil {
					walk([]Statement{v.Update})
				}
				if v.Body != nil {
					walk(v.Body.Statements)
				}
			case *BlockStatement:
				walk(v.Statements)
			case *ExpressionStatement:
				if ie, ok := v.Expression.(*IfExpression); ok {
					if ie.Consequence != nil {
						walk(ie.Consequence.Statements)
					}
					if ie.Alternative != nil {
						walk(ie.Alternative.Statements)
					}
				}
				if sm, ok := v.Expression.(*SurfaceMatch); ok {
					WalkSurfaceMatchArmBodies(sm, walk)
				}
			case *LetStatement:
				if fl, ok := v.Value.(*FunctionLiteral); ok && fl.Body != nil {
					walk(fl.Body.Statements)
				}
				if sm, ok := v.Value.(*SurfaceMatch); ok {
					WalkSurfaceMatchArmBodies(sm, walk)
				}
			}
		}
	}
	walk(program.Statements)
	return found
}
