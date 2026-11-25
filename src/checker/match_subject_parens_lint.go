package checker

import (
	"reflect"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// matchSubjectParensTraceID 是「value-match 主語外面多餘括號」的 HINT 碼。
//
// `v = (expr): { arms }` 與 `v = expr: { arms }` 完全同義——括號是純語法，
// H0 對 GroupedExpression 一律透傳（見 parser/tohir.go 的 case），
// resolveSubjectMatch 與 optionSubjectElemType 也都剝掉括號再判主語型別。
// 但括號讓讀者誤以為它改變了 `: {` 的附著或求值順序，故給一條 HINT，
// 建議移除（no fmt 會自動移除，見 fmt/control.go 的 formatSurfaceMatch）。
//
// 只提示、不報錯：這是不影響語意的風格問題，交由 human / fmt 決定。
const matchSubjectParensTraceID = "msubjparen"

// ValidateMatchSubjectParens 對 `: { … }` 主語被一層或多層括號包住的位置
// 發 HINT（`msubjparen`），建議直接寫 `expr: { … }`。
func ValidateMatchSubjectParens(program *parser.Program, mainFile string) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult

	report := func(g *parser.GroupedExpression, file string) {
		pos := g.Pos()
		results = append(results, ValidateResult{
			Line:    pos.Line,
			Column:  pos.Column,
			File:    file,
			TraceID: matchSubjectParensTraceID,
			Message: i18n.T(matchSubjectParensTraceID,
				"the subject of a value match is wrapped in parentheses; they are redundant because parentheses carry no semantics here (the match binds to the whole expression, then the result is assigned). Write `expr: { ... }` instead — `no fmt` removes these parentheses automatically."),
		})
	}

	// walkExpr / walkBlock / walkStmt / walkStmtSingle 互相遞迴，先宣告再指派。
	// 結構與 range_ellipsis_lint.go 的 walker 一致：只在 SurfaceMatch 分支多
	// 檢查主語是否 GroupedExpression。
	var walkExpr func(e parser.Expression, file string)
	var walkBlock func(b *parser.BlockStatement, file string)
	var walkStmt func(stmts []parser.Statement, file string)
	var walkStmtSingle func(s parser.Statement, file string)

	walkExpr = func(e parser.Expression, file string) {
		if e == nil {
			return
		}
		if rv := reflect.ValueOf(e); rv.Kind() == reflect.Ptr && rv.IsNil() {
			return
		}
		switch x := e.(type) {
		case *parser.SurfaceMatch:
			if g, ok := x.Matched.(*parser.GroupedExpression); ok && g != nil {
				report(g, file)
			}
			walkSurfaceMatchChildren(x, nil,
				func(sub parser.Expression) { walkExpr(sub, file) },
				func(s parser.Statement) { walkStmtSingle(s, file) })
		case *parser.GroupedExpression:
			walkExpr(x.Expression, file)
		case *parser.RangeExpression:
			walkExpr(x.Start, file)
			walkExpr(x.End, file)
		case *parser.SliceExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Range, file)
		case *parser.CallExpression:
			walkExpr(x.Function, file)
			for _, a := range x.Arguments {
				walkExpr(a, file)
			}
			for _, a := range x.GenericArgs {
				walkExpr(a, file)
			}
		case *parser.InfixExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Right, file)
		case *parser.PrefixExpression:
			walkExpr(x.Right, file)
		case *parser.IndexExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Index, file)
		case *parser.ConditionalExpression:
			walkExpr(x.Condition, file)
			walkExpr(x.Consequence, file)
			walkExpr(x.Alternative, file)
		case *parser.CastExpression:
			walkExpr(x.Expr, file)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(el, file)
			}
		case *parser.SliceLiteral:
			for _, el := range x.Elements {
				walkExpr(el, file)
			}
		case *parser.StructLiteral:
			for _, f := range x.Fields {
				walkExpr(f.Value, file)
			}
		case *parser.MapLiteral:
			for _, p := range x.Pairs {
				walkExpr(p.Key, file)
				walkExpr(p.Value, file)
			}
		case *parser.AssignExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Value, file)
		case *parser.IfExpression:
			walkExpr(x.Condition, file)
			walkBlock(x.Consequence, file)
			walkBlock(x.Alternative, file)
		}
	}

	walkBlock = func(b *parser.BlockStatement, file string) {
		if b == nil {
			return
		}
		walkStmt(b.Statements, file)
	}

	walkStmt = func(stmts []parser.Statement, file string) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			if program.Sem != nil {
				if f := program.Sem.SourceFileOf(s); f != "" {
					file = f
				}
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				walkBlock(st.Body, file)
			case *parser.BlockStatement:
				walkStmt(st.Statements, file)
			case *parser.BlockMatchStatement:
				walkStmt(blockMatchChildStatements(st), file)
			case *parser.LetStatement:
				walkExpr(st.Value, file)
			case *parser.MultiAssignStatement:
				walkExpr(st.Value, file)
			case *parser.UnwrapAssignStatement:
				walkExpr(st.Value, file)
			case *parser.ReturnStatement:
				walkExpr(st.ReturnValue, file)
			case *parser.ExpressionStatement:
				walkExpr(st.Expression, file)
			case *parser.ForStatement:
				if st.IterRange != nil {
					walkExpr(st.IterRange.Range, file)
					walkExpr(st.IterRange.RangeExpr, file)
				}
				walkExpr(st.Condition, file)
				walkExpr(st.CountExpr, file)
				walkStmtSingle(st.Init, file)
				walkStmtSingle(st.Update, file)
				walkBlock(st.Body, file)
			}
		}
	}

	walkStmtSingle = func(s parser.Statement, file string) {
		if s == nil {
			return
		}
		walkStmt([]parser.Statement{s}, file)
	}

	walkStmt(program.Statements, mainFile)
	return results
}
