package checker

import (
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// thirPool is the resolver-free structural pool used by the THIR side tables.
// A plain structural parse is enough here: an unresolved alias stays Named and
// keeps its raw spelling, which is all ExprTypeOf promises.
var thirPool = ty.NewPool(nil)

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
func (v *THIRView) ExprTypeRef(n parser.Node) *ty.Type {
	if v == nil || n == nil {
		return nil
	}
	// 構建期間的重入呼叫直接回 nil（sync.Once 不可重入）。collectExprTypes 的
	// 走查會透過 collectFuncDeclared → declTypeOf → TypeRefOf 對不在 IdMap 的
	// 運算式節點（例如運算符重載方法未 remember 的 *Parameter 結果參數）再次
	// 進入本方法；此時最終表尚未成形，回 nil 讓 declTypeOf 退回 legacy AST
	// 宣告值兜底，語義與構建完成後查表對宣告節點的結論一致（nodeType 直讀
	// .Type.String()），且避免了 Do 套 Do 的死鎖。
	if v.exprBuilding {
		return nil
	}
	v.exprOnce.Do(func() {
		v.exprBuilding = true
		v.exprTypes = collectExprTypes(v.prog, v)
		v.exprBuilding = false
	})
	return v.exprTypes[n]
}

// ExprTypeOf returns the expression-level inferred type as a raw spelling (the
// diagnostic form). Use ExprTypeRef for structural questions.
func (v *THIRView) ExprTypeOf(n parser.Node) string {
	if t := v.ExprTypeRef(n); t != nil {
		return t.Raw()
	}
	return ""
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
// Sem.FuncVarTypes 补缺），函數回傳型別表則以 localFuncTypes 就地從本
// program 構造（登記規則與 validator 入口重建 validationFuncTypes 逐位元
// 一致），使本表與 validator 走查時呼叫 inferExprType 得到的答案同源。
//
// ⚠️ 已知限制（本批刻意接受，消費方遷移前需重新談判）：varTypes 用的是
// 「整函數扁平表」，而部分 validator 用「逐語句推進的作用域表」。函數內同名
// 變數在不同語句被賦不同型別時兩者會分叉。本表當下無生產消費方，分叉不產生
// 行為差異；TestExprTypesFaithful 統計覆蓋率供後續批次決策。
func collectExprTypes(program *parser.Program, view *THIRView) map[parser.Node]*ty.Type {
	out := make(map[parser.Node]*ty.Type)
	if program == nil {
		return out
	}
	funcTypes := localFuncTypes(program)
	top := &exprTypeCtx{
		varTypes:  collectTopLevelLets(program, funcTypes, view),
		funcTypes: funcTypes,
		visited:   make(map[parser.Expression]bool),
	}
	top.selfType = top.varTypes["self"]
	collectStmtsExprTypes(program.Statements, top, view, out)
	return out
}

// localFuncTypes 從本 program 的 AST 就地構造「函數名 → 回傳型別」表，
// 登記規則與 ValidateFuncArgs / ValidateRedundantTypeAnnotation 入口重建
// validationFuncTypes 的方式一致（fd 取 declaredResults[0] 原始 Type.String，
// extern 取 Results[0]）。
//
// 為何不用 snapshotValidationFuncTypes()：global validationFuncTypes 由
// 各 validator 在 validationMu 临界区内重建；而本收集表经 ExprTypeOf 的
// sync.Once **惰性**构建，触发点恰在持锁路径（runTypesCore 锁内 →
// declTypeOf → TypeOf → ExprTypeOf → Once.Do）内。snapshot 会再次
// validationMu.Lock，非可重入互斥锁自锁 → 全进程死锁（fatal error: all
// goroutines are asleep）。就地读 AST 不碰全局、不需锁，语义与锁内快照
// 等价（同一 program 同一登记规则），并消除整类重入死锁。
func localFuncTypes(program *parser.Program) map[string]string {
	m := make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if rs := declaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				m[fd.Name] = rs[0].Type.String()
			}
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			if len(es.Results) > 0 && es.Results[0].Type != nil {
				m[es.Name.Value] = es.Results[0].Type.String()
			}
		}
	}
	return m
}

// collectStmtsExprTypes 走查语句列表。函数定义切换独立作用域（参数 + 该函数的
// let），与 collectFuncDeclared 的「巢状函数不写外层」口径一致。
func collectStmtsExprTypes(stmts []parser.Statement, c *exprTypeCtx, view *THIRView, out map[parser.Node]*ty.Type) {
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
						sub.varTypes[k] = v.Raw()
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
func recordExprType(e parser.Expression, c *exprTypeCtx, out map[parser.Node]*ty.Type) {
	if e == nil {
		return
	}
	if c.visited[e] {
		return
	}
	c.visited[e] = true
	if t := inferExprType(e, c.varTypes, c.funcTypes, c.selfType); t != "" {
		out[e] = thirPool.Parse(t)
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
