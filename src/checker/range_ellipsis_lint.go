package checker

import (
	"fmt"
	"reflect"

	"github.com/lizongying/nolang/parser"
)

// rngEllipsisTraceID 是「區間上界用隱式 self 的 `.field`，與 `..` 連寫成 `...`」的
// 診斷碼。見 src/std/arr.no 頂部說明：`..`（範圍運算子）與 `.{field}`（self 成員存取）
// 連寫時會產生 `...` 歧義（`...` 亦是 return/終止運算子）。
const rngEllipsisTraceID = "rng-ellipsis"

// implicitSelfHead 沿表達式最左側（head）下降，回傳「隱式 self 的 `.` token」與其緊接的
// 欄位/方法名。命中條件：最左側是一個 Receiver 為隱式 self（Token.Type==DOT 的
// Identifier）的 DotExpression——即原始碼以 `.` 開頭（`.count`、`.len()`、`.p.len-bytes()`
// 等）。顯式寫的 `self.count` 其 self 是 IDENT token，不算命中，因為那正是建議的寫法。
func implicitSelfHead(e parser.Expression) (*parser.Identifier, string) {
	switch x := e.(type) {
	case *parser.CallExpression:
		return implicitSelfHead(x.Function)
	case *parser.IndexExpression:
		return implicitSelfHead(x.Left)
	case *parser.GroupedExpression:
		return implicitSelfHead(x.Expression)
	case *parser.DotExpression:
		if id, ok := x.Receiver.(*parser.Identifier); ok && isImplicitSelfIdent(id) {
			return id, x.Property
		}
		return implicitSelfHead(x.Receiver)
	}
	return nil, ""
}

// ValidateRangeSelfEllipsis 對 `i <- [0...count)` 這類「區間上界以隱式 self 的 `.` 開頭、
// 緊接範圍運算子 `..`」的寫法發一條 HINT，建議顯式寫出接收者 `self`（如 `[0..self.count)`），
// 以避免 `..` + `.field` 連寫形成的 `...` 與 return/終止運算子歧義。
//
// 為什麼是 HINT 而非 ERROR：現行詞法器會把 `...` 拆成 `..`（ELLIPSIS）+ `.`（DOT），
// 語意仍能正確解析，只是可讀性差、易踩坑；提示不改寫、交由人工決定（與 arr.no 說明一致）。
//
// 只檢查區間的**上界**（End）——下界（Start）緊接的是 `[`/`(` 而非 `..`，不會形成 `...`。
func ValidateRangeSelfEllipsis(program *parser.Program, mainFile string) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult

	checkEnd := func(rng *parser.RangeExpression, file string) {
		if rng == nil || rng.End == nil {
			return
		}
		// 上界必須是「`..` 之後立即出現的頭一個 `.`」——即 RangeExpression 的 End。
		id, prop := implicitSelfHead(rng.End)
		if id == nil {
			return
		}
		results = append(results, ValidateResult{
			Line:    id.Token.Line,
			Column:  id.Token.Column,
			File:    file,
			TraceID: rngEllipsisTraceID,
			Message: fmt.Sprintf(
				"區間上界以隱式 self 的 `.%s` 開頭，與範圍運算子 `..` 連寫會形成 `...%s`（歧義：`...` 亦是 return/終止運算子）。建議顯式寫出接收者，改寫為 `..self.%s`（例：`[a..self.%s)`）。",
				prop, prop, prop, prop),
		})
	}

	// walkStmt 與 walkStmtSingle 互相遞迴（for 的 Init/Update 是單條陳述，需回呼 walkStmt），
	// 故全部先宣告為變數再指派閉包，避免前向參考編譯錯誤。
	var walkExpr func(e parser.Expression, file string)
	var walkBlock func(b *parser.BlockStatement, file string)
	var walkStmt func(stmts []parser.Statement, file string)
	var walkStmtSingle func(s parser.Statement, file string)

	walkExpr = func(e parser.Expression, file string) {
		if e == nil {
			return
		}
		// 型別化 nil（介面內含 nil 指標）會繞過上面的 `e == nil`，取用子欄位時 panic；
		// 與 walkExprForLen 一致地攔下（缺界的 RangeExpression 可能持有 typed-nil 子節點）。
		if rv := reflect.ValueOf(e); rv.Kind() == reflect.Ptr && rv.IsNil() {
			return
		}
		switch x := e.(type) {
		case *parser.RangeExpression:
			checkEnd(x, file)
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
		case *parser.RunExpression:
			walkExpr(x.Call, file)
		case *parser.AwaitExpression:
			walkExpr(x.Right, file)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(el, file)
			}
		case *parser.AssignExpression:
			walkExpr(x.Value, file)
		case *parser.GroupedExpression:
			walkExpr(x.Expression, file)
		case *parser.IfExpression:
			walkExpr(x.Condition, file)
			walkBlock(x.Consequence, file)
			walkBlock(x.Alternative, file)
		case *parser.SurfaceMatch:
			// S4：糖形態下 match 是 SurfaceMatch（表達式節點），經 ExpressionStatement
			// 抵達本 walker；臂體對本 walker 不可見。見 sugar_walk.go。
		walkSurfaceMatchChildren(x, nil,
			func(e parser.Expression) { walkExpr(e, file) },
			func(s parser.Statement) { walkStmtSingle(s, file) })
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
			if f := program.Sem.SourceFileOf(s); f != "" {
				file = f
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				walkBlock(st.Body, file)
			case *parser.BlockStatement:
				walkStmt(st.Statements, file)
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
