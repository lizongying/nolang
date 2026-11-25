package checker

import (
	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// ValueMatchExprTraceID 是「value-match 出現在非法表達式位置」診斷碼。
// build 路徑（build/transpiler_validate.go）據此 TraceID 匯入硬錯誤，vet 則由
// lint 步驟匯入；兩者共用同一碼，故必須在原始碼裡給出穩定可讀的字面值。
const ValueMatchExprTraceID = "matchexpr"

// ValidateValueMatchPosition 報告出現在「賦值右值頂層」之外的 value-match。
//
// 背景：matched value match（`subject: { arms }`，AST 為 *SurfaceMatch）是
// 語句級構造。MIR 只在帶目標名的賦值語句右值上用 exprCapture 捕獲各臂的
// 最終值（見 mir/hir2mir.go lowerStmtInner 的 `cn.Kind == hir.KIf &&
// name != ""` 分支）；括號包裹的賦值右值合法，因為 HIR 會剝掉
// GroupedExpression，孩子仍是 KIf。任何其它位置——運算子操作數、函式實參、
// 比較、return 值、字面量元素等——lowerExpr 拒絕控制流、直接返回 NoVal，
// 最終在 codegen 報 `consumer reads value 0 (NoVal) [zodppsnm]`。
//
// 與其讓壞 MIR 流到 codegen，checker 在這些位置直接給出硬錯誤，並提示把
// match 提取到賦值右值、先存進區域變數再用。
//
// 合法位置（isAssignRHS=true 傳下去）：
//   - LetStatement.Value
//   - AssignExpression.Value
//   - MultiAssignStatement.Value
//   - UnwrapAssignStatement.Value
//
// 以及它們值表達式外任意層 GroupedExpression（括號透明）。
//
// 檔名歸因：SetSourceFileDeep 只標語句節點，嵌在 InfixExpression 等操作數
// 裡的 SurfaceMatch 本身沒有 SourceFile/ModuleOwner。walker 因此在遍历语句
// 时把该语句的溯源当作 file 上下文往下传，report 优先用节点自身归因、缺时
// 回退到上下文， merged 模式下才不会把模块行号贴到入口文件上。
func ValidateValueMatchPosition(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	if program == nil {
		return results
	}

	// sourceFileOf 返回节点的一手溯源，缺时返回空串。
	sourceFileOf := func(n parser.Node) string {
		if program.Sem == nil || n == nil {
			return ""
		}
		if f := program.Sem.SourceFileOf(n); f != "" {
			return f
		}
		return program.Sem.ModuleOwnerOf(n)
	}

	report := func(sm *parser.SurfaceMatch, stmtFile string) {
		pos := sm.Pos()
		file := sourceFileOf(sm)
		if file == "" {
			file = stmtFile
		}
		results = append(results, ValidateResult{
			Line:   pos.Line,
			Column: pos.Column,
			File:   file,
			Message: i18n.T("vmp8s7tq",
				"a value match (`subject: { arms }`) is a statement-level construct and may only appear as the right-hand side of an assignment (optionally parenthesized). It cannot be used inside an expression such as an operand, function argument, comparison, return value or literal. Bind its result to a variable first: `v = subject: { ok -> it ... }`, then use `v`."),
			TraceID: ValueMatchExprTraceID,
		})
	}

	// stripParens 剝掉括號包裹，返回內層表達式與剝掉的層數。match 值外的括號
	// 是透明的（HIR 會剝掉），故賦值右值經任意層括號包裹仍算合法位置。
	stripParens := func(e parser.Expression) parser.Expression {
		for {
			g, ok := e.(*parser.GroupedExpression)
			if !ok || g.Expression == nil {
				return e
			}
			e = g.Expression
		}
	}

	var walkExpr func(e parser.Expression, isAssignRHS bool, file string)
	var walkStmts func(stmts []parser.Statement, fallback string)

	// walkExpr 遍歷表達式。isAssignRHS 表示當前表達式是某賦值語句的直接右值
	// （可能隔著括號）——只有在這種上下文裡 SurfaceMatch 才合法。
	// file 是外層語句的溯源，供深層表達式節點缺少自身歸因時回退。
	walkExpr = func(e parser.Expression, isAssignRHS bool, file string) {
		if e == nil {
			return
		}
		// 先剝括號判定 match：括號不改變「是否賦值右值頂層」。
		inner := stripParens(e)
		if sm, ok := inner.(*parser.SurfaceMatch); ok {
			if !isAssignRHS {
				report(sm, file)
			}
			// match 的主語與臂條件/臂體都是普通表達式/語句上下文：即使本 match
			// 合法，其內部的巢狀 match 仍要按各自位置判定（內部一律非 RHS 頂層，
			// 除臂體內自己的賦值語句外——由 walkStmts 處理）。
			if sm.Matched != nil {
				walkExpr(sm.Matched, false, file)
			}
			for i := range sm.Arms {
				arm := &sm.Arms[i]
				if arm.Condition != nil {
					walkExpr(arm.Condition, false, file)
				}
				for _, mv := range arm.MultiValuePatterns {
					walkExpr(mv, false, file)
				}
				if arm.Body != nil {
					walkStmts(arm.Body.Statements, file)
				}
			}
			return
		}

		switch x := e.(type) {
		case *parser.GroupedExpression:
			// 括號透明：把 isAssignRHS 原樣傳下去。
			walkExpr(x.Expression, isAssignRHS, file)
		case *parser.CallExpression:
			// 函式本身與實參都是普通表達式位置——match 作實參非法。
			walkExpr(x.Function, false, file)
			for _, a := range x.Arguments {
				walkExpr(a, false, file)
			}
			for _, g := range x.GenericArgs {
				walkExpr(g, false, file)
			}
		case *parser.InfixExpression:
			walkExpr(x.Left, false, file)
			walkExpr(x.Right, false, file)
		case *parser.PrefixExpression:
			walkExpr(x.Right, false, file)
		case *parser.IndexExpression:
			walkExpr(x.Left, false, file)
			walkExpr(x.Index, false, file)
		case *parser.SliceExpression:
			walkExpr(x.Left, false, file)
			if x.Range != nil {
				walkExpr(x.Range.Start, false, file)
				walkExpr(x.Range.End, false, file)
			}
		case *parser.DotExpression:
			walkExpr(x.Receiver, false, file)
		case *parser.ConditionalExpression:
			walkExpr(x.Condition, false, file)
			walkExpr(x.Consequence, false, file)
			walkExpr(x.Alternative, false, file)
		case *parser.IfExpression:
			walkExpr(x.Condition, false, file)
			if x.Consequence != nil {
				walkStmts(x.Consequence.Statements, file)
			}
			if x.Alternative != nil {
				walkStmts(x.Alternative.Statements, file)
			}
		case *parser.AssignExpression:
			// 賦值表達式自身的右值是合法的 match 位置（欄位/下標賦值）。
			walkExpr(x.Left, false, file)
			walkExpr(x.Value, true, file)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(el, false, file)
			}
		case *parser.SliceLiteral:
			for _, el := range x.Elements {
				walkExpr(el, false, file)
			}
		case *parser.StructLiteral:
			for _, f := range x.Fields {
				walkExpr(f.Value, false, file)
			}
		case *parser.MapLiteral:
			for _, p := range x.Pairs {
				walkExpr(p.Key, false, file)
				walkExpr(p.Value, false, file)
			}
		case *parser.CastExpression:
			walkExpr(x.Expr, false, file)
		case *parser.BlockMatchStatement:
			// 塊 match `{ body }: { arms }` 是語句，其内部語句照常遍歷。
			for _, s := range blockMatchChildStatements(x) {
				walkStmts([]parser.Statement{s}, file)
			}
		}
		// 字面量 / Identifier / FunctionLiteral（獨立作用域）無需深入。
	}

	walkStmts = func(stmts []parser.Statement, fallback string) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			// 語句自身通常已被 SetSourceFileDeep 標記；深層表達式沒有，
			// 所以用語句溯源當作整棵子樹的 file 上下文。
			file := sourceFileOf(s)
			if file == "" {
				file = fallback
			}
			switch x := s.(type) {
			case *parser.FunctionDefinition:
				if x.Body != nil {
					walkStmts(x.Body.Statements, file)
				}
			case *parser.LetStatement:
				walkExpr(x.Value, true, file)
			case *parser.MultiAssignStatement:
				walkExpr(x.Value, true, file)
				for _, t := range x.Targets {
					walkExpr(t, false, file)
				}
			case *parser.UnwrapAssignStatement:
				walkExpr(x.Value, true, file)
				if x.Target != nil {
					walkExpr(x.Target, false, file)
				}
			case *parser.ExpressionStatement:
				// 獨立成句的 match（值被丟棄）是語句級 match 的正常用法，合法；
				// 但若語句是 print(match) 這類呼叫，match 在實參位置（walkExpr
				// 進 CallExpression 後降為 false）仍會被判非法。
				walkExpr(x.Expression, true, file)
			case *parser.ReturnStatement:
				walkExpr(x.ReturnValue, false, file)
			case *parser.BlockStatement:
				walkStmts(x.Statements, file)
			case *parser.BlockMatchStatement:
				for _, inner := range blockMatchChildStatements(x) {
					walkStmts([]parser.Statement{inner}, file)
				}
			case *parser.ForStatement:
				if x.Init != nil {
					walkStmts([]parser.Statement{x.Init}, file)
				}
				walkExpr(x.Condition, false, file)
				if x.Update != nil {
					walkStmts([]parser.Statement{x.Update}, file)
				}
				if x.IterRange != nil {
					walkExpr(x.IterRange.RangeExpr, false, file)
					if x.IterRange.Range != nil {
						walkExpr(x.IterRange.Range.Start, false, file)
						walkExpr(x.IterRange.Range.End, false, file)
					}
				}
				if x.Body != nil {
					walkStmts(x.Body.Statements, file)
				}
			}
		}
	}

	walkStmts(program.Statements, "")
	return results
}
