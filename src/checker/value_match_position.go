package checker

import (
	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// ValueMatchExprTraceID 是「value-match 出现在非法表达式位置」诊断码。
// build 路径（build/transpiler_validate.go）据此 TraceID 汇入硬错误，vet 则由
// lint 步骤汇入；两者共用同一码，故必须在源码里给出稳定可读的字面值。
const ValueMatchExprTraceID = "matchexpr"

// ValidateValueMatchPosition 报告出现在「赋值右值顶层」之外的 value-match。
//
// 背景：matched value match（`subject: { arms }`，AST 为 *SurfaceMatch）是
// 语句级构造。MIR 只在带目标名的赋值语句右值上用 exprCapture 捕获各臂的
// 最终值（见 mir/hir2mir.go lowerStmtInner 的 `cn.Kind == hir.KIf &&
// name != ""` 分支）；括号包裹的赋值右值合法，因为 HIR 会剥掉
// GroupedExpression，孩子仍是 KIf。任何其它位置——运算符操作数、函数实参、
// 比较、return 值、字面量元素等——lowerExpr 拒绝控制流、直接返回 NoVal，
// 最终在 codegen 报 `consumer reads value 0 (NoVal) [zodppsnm]`。
//
// 与其让坏 MIR 流到 codegen，checker 在这些位置直接给出硬错误，并提示把
// match 提取到赋值右值、先存进局部变量再用。
//
// 合法位置（isAssignRHS=true 传下去）：
//   - LetStatement.Value
//   - AssignExpression.Value
//   - MultiAssignStatement.Value
//   - UnwrapAssignStatement.Value
// 以及它们值表达式外任意层 GroupedExpression（括号透明）。
func ValidateValueMatchPosition(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	if program == nil {
		return results
	}

	report := func(sm *parser.SurfaceMatch) {
		pos := sm.Pos()
		file := ""
		if program.Sem != nil {
			file = program.Sem.SourceFileOf(sm)
			// 合併後的 program 沒有逐節點 SourceFile 映射，退回模組短名，
			// 至少能定位是哪個 import 模組引入的非法寫法。
			if file == "" {
				file = program.Sem.ModuleOwnerOf(sm)
			}
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

	// stripParens 剥掉括号包裹，返回内层表达式与剥掉的层数。match 值外的括号
	// 是透明的（HIR 会剥掉），故赋值右值经任意层括号包裹仍算合法位置。
	stripParens := func(e parser.Expression) parser.Expression {
		for {
			g, ok := e.(*parser.GroupedExpression)
			if !ok || g.Expression == nil {
				return e
			}
			e = g.Expression
		}
	}

	var walkExpr func(e parser.Expression, isAssignRHS bool)
	var walkStmts func(stmts []parser.Statement)

	// walkExpr 遍历表达式。isAssignRHS 表示当前表达式是某赋值语句的直接右值
	// （可能隔着括号）——只有在这种上下文里 SurfaceMatch 才合法。
	walkExpr = func(e parser.Expression, isAssignRHS bool) {
		if e == nil {
			return
		}
		// 先剥括号判定 match：括号不改变「是否赋值右值顶层」。
		inner := stripParens(e)
		if sm, ok := inner.(*parser.SurfaceMatch); ok {
			if !isAssignRHS {
				report(sm)
			}
			// match 的主语与臂条件/臂体都是普通表达式/语句上下文：即使本 match
			// 合法，其内部的嵌套 match 仍要按各自位置判定（内部一律非 RHS 顶层，
			// 除臂体内自己的赋值语句外——由 walkStmts 处理）。
			if sm.Matched != nil {
				walkExpr(sm.Matched, false)
			}
			for i := range sm.Arms {
				arm := &sm.Arms[i]
				if arm.Condition != nil {
					walkExpr(arm.Condition, false)
				}
				for _, mv := range arm.MultiValuePatterns {
					walkExpr(mv, false)
				}
				if arm.Body != nil {
					walkStmts(arm.Body.Statements)
				}
			}
			return
		}

		switch x := e.(type) {
		case *parser.GroupedExpression:
			// 括号透明：把 isAssignRHS 原样传下去。
			walkExpr(x.Expression, isAssignRHS)
		case *parser.CallExpression:
			// 函数本身与实参都是普通表达式位置——match 作实参非法。
			walkExpr(x.Function, false)
			for _, a := range x.Arguments {
				walkExpr(a, false)
			}
			for _, g := range x.GenericArgs {
				walkExpr(g, false)
			}
		case *parser.InfixExpression:
			walkExpr(x.Left, false)
			walkExpr(x.Right, false)
		case *parser.PrefixExpression:
			walkExpr(x.Right, false)
		case *parser.IndexExpression:
			walkExpr(x.Left, false)
			walkExpr(x.Index, false)
		case *parser.SliceExpression:
			walkExpr(x.Left, false)
			if x.Range != nil {
				walkExpr(x.Range.Start, false)
				walkExpr(x.Range.End, false)
			}
		case *parser.DotExpression:
			walkExpr(x.Receiver, false)
		case *parser.ConditionalExpression:
			walkExpr(x.Condition, false)
			walkExpr(x.Consequence, false)
			walkExpr(x.Alternative, false)
		case *parser.IfExpression:
			walkExpr(x.Condition, false)
			if x.Consequence != nil {
				walkStmts(x.Consequence.Statements)
			}
			if x.Alternative != nil {
				walkStmts(x.Alternative.Statements)
			}
		case *parser.AssignExpression:
			// 赋值表达式自身的右值是合法的 match 位置（字段/下标赋值）。
			walkExpr(x.Left, false)
			walkExpr(x.Value, true)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(el, false)
			}
		case *parser.SliceLiteral:
			for _, el := range x.Elements {
				walkExpr(el, false)
			}
		case *parser.StructLiteral:
			for _, f := range x.Fields {
				walkExpr(f.Value, false)
			}
		case *parser.MapLiteral:
			for _, p := range x.Pairs {
				walkExpr(p.Key, false)
				walkExpr(p.Value, false)
			}
		case *parser.CastExpression:
			walkExpr(x.Expr, false)
		case *parser.RunExpression:
			walkExpr(x.Call, false)
		case *parser.AwaitExpression:
			walkExpr(x.Right, false)
		case *parser.BlockMatchStatement:
			// 块 match `{ body }: { arms }` 是语句，其内部语句照常遍历。
			for _, s := range blockMatchChildStatements(x) {
				walkStmts([]parser.Statement{s})
			}
		}
		// 字面量 / Identifier / FunctionLiteral（独立作用域）无需深入。
	}

	walkStmts = func(stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch x := s.(type) {
			case *parser.FunctionDefinition:
				if x.Body != nil {
					walkStmts(x.Body.Statements)
				}
			case *parser.LetStatement:
				walkExpr(x.Value, true)
			case *parser.MultiAssignStatement:
				walkExpr(x.Value, true)
				for _, t := range x.Targets {
					walkExpr(t, false)
				}
			case *parser.UnwrapAssignStatement:
				walkExpr(x.Value, true)
				if x.Target != nil {
					walkExpr(x.Target, false)
				}
			case *parser.ExpressionStatement:
				// 独立成句的 match（值被丢弃）是语句级 match 的正常用法，合法；
				// 但若语句是 print(match) 这类调用，match 在实参位置（walkExpr
				// 进 CallExpression 后降为 false）仍会被判非法。
				walkExpr(x.Expression, true)
			case *parser.ReturnStatement:
				walkExpr(x.ReturnValue, false)
			case *parser.BlockStatement:
				walkStmts(x.Statements)
			case *parser.BlockMatchStatement:
				for _, inner := range blockMatchChildStatements(x) {
					walkStmts([]parser.Statement{inner})
				}
			case *parser.ForStatement:
				if x.Init != nil {
					walkStmts([]parser.Statement{x.Init})
				}
				walkExpr(x.Condition, false)
				if x.Update != nil {
					walkStmts([]parser.Statement{x.Update})
				}
				if x.IterRange != nil {
					walkExpr(x.IterRange.RangeExpr, false)
					if x.IterRange.Range != nil {
						walkExpr(x.IterRange.Range.Start, false)
						walkExpr(x.IterRange.Range.End, false)
					}
				}
				if x.Body != nil {
					walkStmts(x.Body.Statements)
				}
			}
		}
	}

	walkStmts(program.Statements)
	return results
}
