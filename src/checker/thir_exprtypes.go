package checker

import (
	"github.com/lizongying/nolang/parser"
)

// 本文件承载 6C-optional 第二步「显式 type map」：把 checker 的表达式级推断
// （inferExprType）结果持久化成一张节点键型别表，使 THIRView.TypeOf 可答
// 表达式节点（Identifier / InfixExpression / CallExpression / DotExpression /
// IndexExpression …）。
//
// ── 为什么是独立表而不是写进 pkg.Inferred ──
// pkg.Inferred 是 hir.Package 的 THIR 副表，MIR 有 4 处消费点
// （hir2mir.go：KCast 1540 / inferredTypeOf 1641 / letTypeRaw 2038 /
// lowerMapLit 7050）。inferredTypeOf 是「结构推导得 void 时的通用兜底」，
// 一旦往 pkg.Inferred 里塞表达式级型别，MIR 对大量表达式节点会从 void 翻成
// 具体型别 —— 那是 codegen 级行为变更，超出本批范围。故显式 type map 只挂在
// THIRView 上，TypeOf 第三档查询，MIR / codegen 完全不受影响。
//
// ── 时序桥接 ──
// inferExprType 在 checker（lint 步骤内）跑，PopulateInferredTypes 在 H0 跑，
// 而 BuildTHIRView 先于 lint 步骤完成 —— 视图构建时推断尚未发生。本表因此
// **惰性构建**（sync.Once）：只在真有调用方查询表达式节点时才付一次全树
// 收集代价。当下没有任何生产消费方查询表达式级 TypeOf，故本表在生产路径上
// 永不构建（零开销），能力先立、消费方迁移留待后续批次。

// ExprTypeOf 返回表达式节点的推断型别；非表达式节点恒为 ""。
//
// 首次调用触发一次全树收集（见 collectExprTypes），之后为 O(1) 查表。
// 表内只含 *表达式* 节点键，故声明类节点（LetStatement / Parameter /
// StructField …）即使触发构建也查不到自己，答案与构建前一致（零行为变化）。
func (v *THIRView) ExprTypeOf(n parser.Node) string {
	if v == nil || n == nil {
		return ""
	}
	v.exprOnce.Do(func() { v.exprTypes = collectExprTypes(v.prog, v) })
	return v.exprTypes[n]
}

// exprTypeCtx 是表达式型别收集走查的上下文：函数作用域内的名称→型别表、
// 函数回传型别表、方法 self 型别，以及跨作用域共享的已访问集。
type exprTypeCtx struct {
	varTypes  map[string]string
	funcTypes map[string]string
	selfType  string
	// visited 跨函数边界共享：inferExprType 的自递归与本走查的子节点递归
	// 会重复访问同一节点，不去重将指数级放大（std 合并的巨型 program 会
	// 直接卡死）。节点键是 AST 指针，可比较。
	visited map[parser.Expression]bool
}

// collectExprTypes 走查整棵程序，对每个表达式节点记录 inferExprType 的结果。
//
// 上下文口径刻意与 runIntOverflowCore 对齐（同一套 collectFuncDeclared +
// Sem.FuncVarTypes 补缺 + snapshotValidationFuncTypes），使本表与 validator
// 走查时调用 inferExprType 得到的答案同源。
//
// ⚠️ 已知限制（本批刻意接受，消费方迁移前需重新谈判）：varTypes 用的是
// 「整函数扁平表」，而部分 validator 用「逐语句推进的作用域表」。函数内同名
// 变数在不同语句被赋不同型别时两者会分叉。本表当下无生产消费方，分叉不产生
// 行为差异；TestExprTypesFaithful 统计覆盖率供后续批次决策。
func collectExprTypes(program *parser.Program, view *THIRView) map[parser.Node]string {
	out := make(map[parser.Node]string)
	if program == nil {
		return out
	}
	funcTypes := snapshotValidationFuncTypes()
	top := &exprTypeCtx{
		varTypes:  collectTopLevelLets(program, funcTypes, view),
		funcTypes: funcTypes,
		visited:   make(map[parser.Expression]bool),
	}
	top.selfType = top.varTypes["self"]
	collectStmtsExprTypes(program.Statements, top, view, out)
	return out
}

// collectStmtsExprTypes 走查语句列表。函数定义切换独立作用域（参数 + 该函数的
// let），与 collectFuncDeclared 的「巢状函数不写外层」口径一致。
func collectStmtsExprTypes(stmts []parser.Statement, c *exprTypeCtx, view *THIRView, out map[parser.Node]string) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if s.Body == nil {
				continue
			}
			sub := &exprTypeCtx{
				varTypes:  collectFuncDeclared(s, c.funcTypes, view),
				funcTypes: c.funcTypes,
				visited:   c.visited,
			}
			// 并入 parser 阶段的逐函数变数型别表：lowering 会把显式型别的 let
			// 改写成 match desugar（let 从 AST 消失），仅靠 AST 收集会漏登记。
			// 只补未登记键、不覆写既有条目 —— 与 runIntOverflowCore 同口径。
			if view != nil && view.prog != nil && view.prog.Sem != nil && s.Name != "" {
				for k, v := range view.prog.Sem.FuncVarTypes[s.Name] {
					if _, exists := sub.varTypes[k]; !exists {
						sub.varTypes[k] = v
					}
				}
			}
			sub.selfType = sub.varTypes["self"]
			collectStmtsExprTypes(s.Body.Statements, sub, view, out)
		case *parser.LetStatement:
			recordExprType(s.Value, c, out)
		case *parser.ExpressionStatement:
			recordExprType(s.Expression, c, out)
		case *parser.ReturnStatement:
			recordExprType(s.ReturnValue, c, out)
		case *parser.BlockStatement:
			collectStmtsExprTypes(s.Statements, c, view, out)
		case *parser.ForStatement:
			recordExprType(s.Condition, c, out)
			if s.Body != nil {
				collectStmtsExprTypes(s.Body.Statements, c, view, out)
			}
		}
	}
}

// recordExprType 记录单个表达式节点的推断型别，并对其子表达式递归。
func recordExprType(e parser.Expression, c *exprTypeCtx, out map[parser.Node]string) {
	if e == nil {
		return
	}
	if c.visited[e] {
		return
	}
	c.visited[e] = true
	if t := inferExprType(e, c.varTypes, c.funcTypes, c.selfType); t != "" {
		out[e] = t
	}
	switch x := e.(type) {
	case *parser.InfixExpression:
		recordExprType(x.Left, c, out)
		recordExprType(x.Right, c, out)
	case *parser.PrefixExpression:
		recordExprType(x.Right, c, out)
	case *parser.CallExpression:
		recordExprType(x.Function, c, out)
		for _, a := range x.Arguments {
			recordExprType(a, c, out)
		}
		for _, g := range x.GenericArgs {
			recordExprType(g, c, out)
		}
	case *parser.DotExpression:
		recordExprType(x.Receiver, c, out)
	case *parser.IndexExpression:
		recordExprType(x.Left, c, out)
		recordExprType(x.Index, c, out)
	case *parser.IfExpression:
		recordExprType(x.Condition, c, out)
	}
}
