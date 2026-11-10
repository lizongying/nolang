package fmt

import (
	"sort"

	"github.com/lizongying/nolang/parser"
)

// 導入語句的位置歸一化：`no fmt` 只允許導入出現在**文件頂部的連續區段**，
// 其它位置（代碼之後的頂層導入、函式/塊/臂體內的導入）一律連同其上方的
// Doc 註釋搬到該區段末尾，再交由 normalizeImportRun 去重/排序。
//
// 為什麼要搬而不是報錯：checker 只從 `Program.Statements` 的頂層序列收集
// UseStatement（見 src/checker/checker.go 的 import 收集點），寫在塊內的導入
// 從未生效——它是一條「看起來像導入、實際無作用」的陳述。搬到頂部既滿足
// 「導入只在頂部」的排版契約，也讓那條被忽略的導入真正生效。
//
// 邊界：
//   - 搬移保持源碼出現順序（按 Pos().Line 穩定排序），多段中段導入的相對
//     次序不會被打亂。
//   - 註釋/`#{...}` 註解掛在 UseStatement 節點上（Doc / Comment / sem side-table），
//     隨節點一起搬，不落單。
//   - 塊內導入搬走後塊可能變空，格式化輸出 `{ }`——不刪塊本身（塊可能是
//     語法必需的位置，例如 `-> { }` 臂體）。
//   - 只走「會包含語句序列」的節點（塊、函式體、for 體、if/match 臂、
//     函數字面量）；純表達式容器不追，避免為理論情形引入大範圍 AST 走訪。

// hoistImports 把所有非頂部的導入語句搬到頂部導入區段末尾。
func hoistImports(p *parser.Program) {
	if p == nil || len(p.Statements) == 0 {
		return
	}
	// 頂部已有的連續導入區段：原地保留，新搬來的接在它後面。
	top := 0
	for top < len(p.Statements) {
		if _, ok := p.Statements[top].(*parser.UseStatement); !ok {
			break
		}
		top++
	}

	kept := make([]parser.Statement, 0, len(p.Statements))
	kept = append(kept, p.Statements[:top]...)
	var moved []*parser.UseStatement
	for _, stmt := range p.Statements[top:] {
		if u, ok := stmt.(*parser.UseStatement); ok {
			moved = append(moved, u)
			continue
		}
		moved = append(moved, takeImportsFromStmt(stmt)...)
		kept = append(kept, stmt)
	}
	if len(moved) == 0 {
		return
	}

	sort.SliceStable(moved, func(i, j int) bool {
		return moved[i].Pos().Line < moved[j].Pos().Line
	})

	out := make([]parser.Statement, 0, len(kept)+len(moved))
	out = append(out, kept[:top]...)
	for _, u := range moved {
		out = append(out, u)
	}
	out = append(out, kept[top:]...)
	p.Statements = out
}

// takeImportsFromBlock 摘除塊內所有導入語句並返回之（源碼順序）。
func takeImportsFromBlock(b *parser.BlockStatement) []*parser.UseStatement {
	if b == nil || len(b.Statements) == 0 {
		return nil
	}
	out := make([]*parser.UseStatement, 0)
	kept := make([]parser.Statement, 0, len(b.Statements))
	for _, s := range b.Statements {
		if u, ok := s.(*parser.UseStatement); ok {
			out = append(out, u)
			continue
		}
		out = append(out, takeImportsFromStmt(s)...)
		kept = append(kept, s)
	}
	b.Statements = kept
	return out
}

// takeImportsFromStmt 摘除語句（含其內部塊）中的導入語句。
func takeImportsFromStmt(s parser.Statement) []*parser.UseStatement {
	switch st := s.(type) {
	case nil:
		return nil
	case *parser.BlockStatement:
		return takeImportsFromBlock(st)
	case *parser.FunctionDefinition:
		return takeImportsFromBlock(st.Body)
	case *parser.ForStatement:
		out := takeImportsFromStmt(st.Init)
		out = append(out, takeImportsFromStmt(st.Update)...)
		return append(out, takeImportsFromBlock(st.Body)...)
	case *parser.BlockMatchStatement:
		out := takeImportsFromBlock(st.Body)
		return append(out, takeImportsFromExpr(st.Match)...)
	case *parser.ExpressionStatement:
		return takeImportsFromExpr(st.Expression)
	case *parser.LetStatement:
		return takeImportsFromExpr(st.Value)
	case *parser.MultiAssignStatement:
		return takeImportsFromExpr(st.Value)
	case *parser.UnwrapAssignStatement:
		return takeImportsFromExpr(st.Value)
	case *parser.ReturnStatement:
		return takeImportsFromExpr(st.ReturnValue)
	}
	return nil
}

// takeImportsFromExpr 摘除表達式內（函數字面量體、if/match 臂體）的導入語句。
func takeImportsFromExpr(e parser.Expression) []*parser.UseStatement {
	switch ex := e.(type) {
	case nil:
		return nil
	case *parser.FunctionLiteral:
		return takeImportsFromBlock(ex.Body)
	case *parser.IfExpression:
		out := takeImportsFromExpr(ex.Condition)
		out = append(out, takeImportsFromBlock(ex.Consequence)...)
		return append(out, takeImportsFromBlock(ex.Alternative)...)
	case *parser.SurfaceMatch:
		out := takeImportsFromExpr(ex.Matched)
		for i := range ex.Arms {
			out = append(out, takeImportsFromBlock(ex.Arms[i].Body)...)
		}
		return out
	case *parser.GroupedExpression:
		return takeImportsFromExpr(ex.Expression)
	case *parser.PrefixExpression:
		return takeImportsFromExpr(ex.Right)
	case *parser.InfixExpression:
		out := takeImportsFromExpr(ex.Left)
		return append(out, takeImportsFromExpr(ex.Right)...)
	case *parser.CallExpression:
		out := takeImportsFromExpr(ex.Function)
		for _, a := range ex.Arguments {
			out = append(out, takeImportsFromExpr(a)...)
		}
		return out
	case *parser.AssignExpression:
		out := takeImportsFromExpr(ex.Left)
		return append(out, takeImportsFromExpr(ex.Value)...)
	case *parser.ConditionalExpression:
		out := takeImportsFromExpr(ex.Condition)
		out = append(out, takeImportsFromExpr(ex.Consequence)...)
		return append(out, takeImportsFromExpr(ex.Alternative)...)
	case *parser.CastExpression:
		return takeImportsFromExpr(ex.Expr)
	case *parser.IndexExpression:
		out := takeImportsFromExpr(ex.Left)
		return append(out, takeImportsFromExpr(ex.Index)...)
	case *parser.SliceExpression:
		return takeImportsFromExpr(ex.Left)
	case *parser.ArrayLiteral:
		out := takeImportsFromExpr(ex.Size)
		for _, el := range ex.Elements {
			out = append(out, takeImportsFromExpr(el)...)
		}
		return out
	case *parser.SliceLiteral:
		out := make([]*parser.UseStatement, 0, len(ex.Elements))
		for _, el := range ex.Elements {
			out = append(out, takeImportsFromExpr(el)...)
		}
		return out
	case *parser.MapLiteral:
		out := make([]*parser.UseStatement, 0, len(ex.Pairs)*2)
		for _, pair := range ex.Pairs {
			out = append(out, takeImportsFromExpr(pair.Key)...)
			out = append(out, takeImportsFromExpr(pair.Value)...)
		}
		return out
	case *parser.StructLiteral:
		out := make([]*parser.UseStatement, 0, len(ex.Fields))
		for _, fld := range ex.Fields {
			if fld != nil {
				out = append(out, takeImportsFromExpr(fld.Value)...)
			}
		}
		return out
	}
	return nil
}
