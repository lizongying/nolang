package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// ============================================================
// 无栈协程（状态机变换）实现
//
// 设计概要：
//   - 含 awy 的函数被变换为 coro_resume.N 函数
//   - 局部变量提升到 %coro_state.N 结构体
//   - awy 挂起点将函数体分为若干段，通过 switch state 跳转
//   - 当前限制：awy 只能在函数体顶层语句中出现（不在 if/for/match 内）
// ============================================================

// containsAwait 检测语句列表是否含 awy 表达式。
func containsAwait(stmts []parser.Statement) bool {
	for _, stmt := range stmts {
		if containsAwaitInStmt(stmt) {
			return true
		}
	}
	return false
}

// containsAwaitInStmt 检测单条语句是否含 awy。
func containsAwaitInStmt(stmt parser.Statement) bool {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return containsAwaitInExpr(s.Value)
	case *parser.ExpressionStatement:
		return containsAwaitInExpr(s.Expression)
	}
	return false
}

// containsAwaitInExpr 递归检测表达式中是否含 awy。
func containsAwaitInExpr(expr parser.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *parser.AwaitExpression:
		return true
	case *parser.CallExpression:
		if containsAwaitInExpr(e.Function) {
			return true
		}
		for _, arg := range e.Arguments {
			if containsAwaitInExpr(arg) {
				return true
			}
		}
		return false
	case *parser.InfixExpression:
		return containsAwaitInExpr(e.Left) || containsAwaitInExpr(e.Right)
	case *parser.PrefixExpression:
		return containsAwaitInExpr(e.Right)
	}
	return false
}

// topLevelAwaitIndex 记录一个顶层 awy 语句的位置及其结果变量。
type topLevelAwaitIndex struct {
	stmtIdx    int    // 语句在函数体中的索引
	resultVar  string // 结果变量名（如 r = awy ... 中的 r），空表示无结果
}

// collectTopLevelAwaitIndices 收集函数体顶层 awy 语句的索引及结果变量。
// 只收集「整条语句是 awy」的情况（如 r = awy ... 或 awy ...）。
// 不收集 awy 嵌套在复杂表达式中的情况（暂不支持）。
func collectTopLevelAwaitIndices(stmts []parser.Statement) []topLevelAwaitIndex {
	var indices []topLevelAwaitIndex
	for i, stmt := range stmts {
		if rv, ok := topLevelAwaitInfo(stmt); ok {
			indices = append(indices, topLevelAwaitIndex{stmtIdx: i, resultVar: rv})
		}
	}
	return indices
}

// topLevelAwaitInfo 返回 (resultVar, true) 如果 stmt 是顶层 awy 语句。
func topLevelAwaitInfo(stmt parser.Statement) (string, bool) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if _, ok := s.Value.(*parser.AwaitExpression); ok {
			return s.Name.Value, true
		}
	case *parser.ExpressionStatement:
		if _, ok := s.Expression.(*parser.AwaitExpression); ok {
			return "", true
		}
	}
	return "", false
}

// isTopLevelAwaitStmt 检测语句是否是顶层 awy 语句。
func isTopLevelAwaitStmt(stmt parser.Statement) bool {
	_, ok := topLevelAwaitInfo(stmt)
	return ok
}

// coroStateName 返回 coro_state 的 LLVM 类型名。
func (g *Generator) coroStateName(num int) string {
	return fmt.Sprintf("%%coro_state.%d", num)
}

// coroResumeName 返回 coro_resume 函数的 LLVM 名。
func (g *Generator) coroResumeName(num int) string {
	return fmt.Sprintf("coro_resume.%d", num)
}

// coroInitName 返回 coro_init 函数的 LLVM 名。
func (g *Generator) coroInitName(num int) string {
	return fmt.Sprintf("coro_init.%d", num)
}

// transformAsyncFunction 将含 awy 的函数变换为无栈协程状态机。
// 生成：
//   1. %coro_state.N 结构体定义
//   2. @coro_init.N 初始化函数
//   3. @coro_resume.N 状态机 resume 函数
//
// 原函数不再生成（被 coro_resume.N 替代）。
func (g *Generator) transformAsyncFunction(sb *strings.Builder, fd *parser.FunctionDefinition) bool {
	stmts := fd.Body.Statements
	awaitIndices := collectTopLevelAwaitIndices(stmts)
	if len(awaitIndices) == 0 {
		return false
	}

	g.coroFuncNum++
	num := g.coroFuncNum
	stateType := g.coroStateName(num)
	// 记录 async 函数名 → coro 编号映射，供 prepareAsyncCall/generateRunExpression 使用
	if g.asyncFuncCoroNum == nil {
		g.asyncFuncCoroNum = make(map[string]int)
	}
	g.asyncFuncCoroNum[fd.Name] = num

	// 1. 收集局部变量类型
	localVarTypes := make(map[string]string)
	g.collectVarDeclsFromStmt(fd.Body, localVarTypes)
	// 排除参数和结果参数（它们作为结构体字段单独处理）
	for _, p := range fd.Parameters {
		delete(localVarTypes, p.Name)
	}
	for _, r := range fd.Results {
		delete(localVarTypes, r.Name)
	}
	// 模块级 async 包装：模块级变量不提升到 coro_state。
	// 它们在 coro_resume 入口由 generateLet alloca（与 @main 一致）。
	// 仅嵌套作用域内的变量（if/for 内声明的）才提升到 coro_state。
	// 原因：模块级变量数量庞大（含 stdlib），全部提升会导致 coro_state 结构体过大。
	// 模块级变量若跨 awy 段使用，其值在 yield 后丢失 — 目前模块级 async 测试用例
	// 不依赖跨段变量（awy 结果通过 task 加载，不依赖 alloca 持久化）。
	if g.isModuleAsyncWrap {
		if g.funcLocalNames == nil {
			g.funcLocalNames = make(map[string]bool)
		}
		for name := range localVarTypes {
			// 排除所有 moduleVarTypes 中的变量（模块级），不放入 coro_state
			// 也不设置 funcLocalNames — generateLet 会处理 alloca 和 funcLocalNames 设置
			if g.moduleVarTypes != nil {
				if _, isModule := g.moduleVarTypes[name]; isModule {
					delete(localVarTypes, name)
					continue
				}
			}
			// 排除全局变量和函数引用
			if (g.globalVars != nil && g.globalVars[name]) || (g.funcRefVars != nil && g.funcRefVars[name]) {
				delete(localVarTypes, name)
				continue
			}
			// 嵌套作用域变量：提升到 coro_state
			g.funcLocalNames[name] = true
		}
	} else {
		// 普通函数：排除全局变量和函数引用，其余提升到 coro_state
		for name := range localVarTypes {
			if (g.globalVars != nil && g.globalVars[name]) || (g.funcRefVars != nil && g.funcRefVars[name]) {
				delete(localVarTypes, name)
			}
		}
		if g.funcLocalNames == nil {
			g.funcLocalNames = make(map[string]bool)
		}
		for name := range localVarTypes {
			g.funcLocalNames[name] = true
		}
	}

	// 2. 构建 coro_state 字段列表
	// field 0: state (i32)
	// field 1: __task (i8*) — 当前协程自身的 task 指针（trampoline 写入，s_end 设 done=true）
	// field 2: __awaited (i8*) — 当前等待的 task 指针（yield 时存，check 时读 done）
	// field 3..: 参数
	// field K..: 局部变量
	// field M..: 结果参数
	fields := []coroField{
		{name: "__state", llvmTy: "i32"},
		{name: "__task", llvmTy: "i8*"},
		{name: "__awaited", llvmTy: "i8*"},
	}
	for _, p := range fd.Parameters {
		llvmTy := g.resolveParamLLVMType(p.Type)
		fields = append(fields, coroField{name: p.Name, llvmTy: llvmTy, isParam: true})
	}
	// 按 sorted 顺序添加局部变量，保证确定性
	sortedLocals := make([]string, 0, len(localVarTypes))
	for name := range localVarTypes {
		sortedLocals = append(sortedLocals, name)
	}
	sortStrings(sortedLocals)
	for _, name := range sortedLocals {
		fields = append(fields, coroField{name: name, llvmTy: localVarTypes[name]})
	}
	for _, r := range fd.Results {
		if r.Name != "" {
			llvmTy := g.resolveParamLLVMType(r.Type)
			fields = append(fields, coroField{name: r.Name, llvmTy: llvmTy, isResult: true})
		}
	}

	g.coroStateFields = fields
	g.coroFieldIdx = make(map[string]int)
	for i, f := range fields {
		g.coroFieldIdx[f.name] = i
	}

	// 3. 生成结构体定义（直接写入 sb，确保在使用它的 coro_init.N / coro_resume.N 之前定义）
	fieldTypes := make([]string, len(fields))
	for i, f := range fields {
		fieldTypes[i] = f.llvmTy
	}
	structDef := fmt.Sprintf("%s = type { %s }\n", stateType, strings.Join(fieldTypes, ", "))
	sb.WriteString(structDef)

	// 4. 生成 coro_init.N 函数
	g.generateCoroInit(sb, fd, num, stateType, fields)

	// 5. 生成 coro_resume.N 函数
	g.generateCoroResume(sb, fd, num, stateType, fields, awaitIndices, localVarTypes)

	return true
}

// generateCoroInit 生成 coro_init.N 函数，用于初始化 coro_state 的参数。
// 调用方在创建协程时调用 coro_init.N(cs, args...) 设置参数和 state=0。
func (g *Generator) generateCoroInit(sb *strings.Builder, fd *parser.FunctionDefinition, num int, stateType string, fields []coroField) {
	initName := g.coroInitName(num)
	// 函数签名：void @coro_init.N(%coro_state.N* %cs, <param types>...)
	var paramStrs []string
	paramStrs = append(paramStrs, stateType+"* %cs")
	for _, p := range fd.Parameters {
		llvmTy := g.resolveParamLLVMType(p.Type)
		paramStrs = append(paramStrs, fmt.Sprintf("%s %s", llvmTy, llvmVarRef(p.Name)))
	}

	sb.WriteString(fmt.Sprintf("define void @%s(%s) {\n", sanitizeLLVMName(initName), strings.Join(paramStrs, ", ")))
	sb.WriteString("entry:\n")
	// state = 0
	sb.WriteString(fmt.Sprintf("\t%%init.state.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 0\n", stateType, stateType))
	sb.WriteString("\tstore i32 0, i32* %init.state.gep\n")
	// task = null
	sb.WriteString(fmt.Sprintf("\t%%init.task.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 1\n", stateType, stateType))
	sb.WriteString("\tstore i8* null, i8** %init.task.gep\n")
	// awaited = null
	sb.WriteString(fmt.Sprintf("\t%%init.awaited.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 2\n", stateType, stateType))
	sb.WriteString("\tstore i8* null, i8** %init.awaited.gep\n")
	// 存储参数
	for _, p := range fd.Parameters {
		idx := g.coroFieldIdx[p.Name]
		sb.WriteString(fmt.Sprintf("\t%%init.%s.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 %d\n",
			sanitizeLLVMName(p.Name), stateType, stateType, idx))
		llvmTy := g.resolveParamLLVMType(p.Type)
		sb.WriteString(fmt.Sprintf("\tstore %s %s, %s* %%init.%s.gep\n",
			llvmTy, llvmVarRef(p.Name), llvmTy, sanitizeLLVMName(p.Name)))
	}
	// 零初始化局部变量和结果参数
	for _, f := range fields {
		if f.isParam || f.name == "__state" || f.name == "__task" || f.name == "__awaited" {
			continue
		}
		idx := g.coroFieldIdx[f.name]
		sb.WriteString(fmt.Sprintf("\t%%init.%s.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 %d\n",
			sanitizeLLVMName(f.name), stateType, stateType, idx))
		sb.WriteString(fmt.Sprintf("\tstore %s zeroinitializer, %s* %%init.%s.gep\n",
			f.llvmTy, f.llvmTy, sanitizeLLVMName(f.name)))
	}
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")
}

// generateCoroResume 生成 coro_resume.N 状态机函数。
func (g *Generator) generateCoroResume(sb *strings.Builder, fd *parser.FunctionDefinition, num int, stateType string, fields []coroField, awaitIndices []topLevelAwaitIndex, localVarTypes map[string]string) {
	resumeName := g.coroResumeName(num)
	// coro_resume 的段体生成时，临时变量 alloca 直接写入 sb（内联）。
	// 每个段每次 resume 最多进入一次，不会因循环导致栈增长。
	g.entryAllocaBuf = nil
	sb.WriteString(fmt.Sprintf("define void @%s(%s* %%cs) {\n", sanitizeLLVMName(resumeName), stateType))
	sb.WriteString("entry:\n")
	// load state
	sb.WriteString(fmt.Sprintf("\t%%r.state.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 0\n", stateType, stateType))
	sb.WriteString("\t%r.state = load i32, i32* %r.state.gep\n")
	// load awaited_task
	sb.WriteString(fmt.Sprintf("\t%%r.task.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 1\n", stateType, stateType))
	sb.WriteString("\t%r.task = load i8*, i8** %r.task.gep\n")
	// load awaited (field 2) — check 块检查它的 done
	sb.WriteString(fmt.Sprintf("\t%%r.awaited.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 2\n", stateType, stateType))
	sb.WriteString("\t%r.awaited = load i8*, i8** %r.awaited.gep\n")

	// 为所有局部变量和参数创建 alloca，并从 coro_state load
	// 这样函数体生成逻辑不需要修改，varAddr 仍然使用 %name
	for _, f := range fields {
		if f.name == "__state" || f.name == "__task" || f.name == "__awaited" {
			continue
		}
		// alloca
		sb.WriteString(fmt.Sprintf("\t%s = alloca %s\n", llvmVarRef(f.name), f.llvmTy))
		// load from coro_state
		idx := g.coroFieldIdx[f.name]
		sb.WriteString(fmt.Sprintf("\t%%r.ld.%s.gep = getelementptr inbounds %s, %s* %%cs, i32 0, i32 %d\n",
			sanitizeLLVMName(f.name), stateType, stateType, idx))
		sb.WriteString(fmt.Sprintf("\t%%r.ld.%s = load %s, %s* %%r.ld.%s.gep\n",
			sanitizeLLVMName(f.name), f.llvmTy, f.llvmTy, sanitizeLLVMName(f.name)))
		sb.WriteString(fmt.Sprintf("\tstore %s %%r.ld.%s, %s* %s\n",
			f.llvmTy, sanitizeLLVMName(f.name), f.llvmTy, llvmVarRef(f.name)))
	}

	// 模块级 async 包装：预分配模块级变量（与 generateMainFunction 一致）。
	// 预分配所有标量和 % 类型变量（%task/%str-long/%arr/%vec/struct 等），
	// generateLet 在段体内仅做初始化（malloc data、store len/cap 等），不重复 alloca。
	// 预分配的原因：CallExpression 可能在使用变量地址作为输出参数时，
	// 对应的 generateLet 尚未执行（前向引用）。
	if g.isModuleAsyncWrap && g.moduleVarTypes != nil {
		sortedNames := make([]string, 0, len(g.moduleVarTypes))
		for name := range g.moduleVarTypes {
			sortedNames = append(sortedNames, name)
		}
		sortStrings(sortedNames)
		for _, name := range sortedNames {
			if g.globalVars != nil && g.globalVars[name] {
				continue
			}
			if g.funcRefVars != nil && g.funcRefVars[name] {
				continue
			}
			varType := g.moduleVarTypes[name]
			// 与 generateMainFunction 的预分配条件一致：
			// 跳过空类型，以及非 % 前缀且非简单标量的类型
			if varType == "" || (strings.HasPrefix(varType, "%") == false && varType != "i64" && varType != "double" && varType != "i1" && varType != "i8" && varType != "i32") {
				continue
			}
			if !g.funcLocalNames[name] {
				g.funcLocalNames[name] = true
				sb.WriteString(fmt.Sprintf("\t%s = alloca %s\n", llvmVarRef(name), varType))
			}
		}
	}

	// switch state
	numSegments := len(awaitIndices) + 1
	sb.WriteString("\tswitch i32 %r.state, label %s0 [\n")
	for i := 1; i < numSegments; i++ {
		sb.WriteString(fmt.Sprintf("\t\ti32 %d, label %%s%d_check\n", i, i))
	}
	sb.WriteString("\t]\n")

	// 生成各段
	stmts := fd.Body.Statements
	prevIdx := 0
	for segIdx := 0; segIdx < numSegments; segIdx++ {
		var segEnd int
		if segIdx < len(awaitIndices) {
			segEnd = awaitIndices[segIdx].stmtIdx
		} else {
			segEnd = len(stmts)
		}

		// 如果 segIdx > 0，先生成 check 块（检查上一个 awy 的 task 是否完成）
		if segIdx > 0 {
			prevAwait := awaitIndices[segIdx-1]
			sb.WriteString(fmt.Sprintf("s%d_check:\n", segIdx))
			// 检查 %r.awaited 的 done 字段（上次 yield 时等待的 task）
			// %task = { void (i8*)*, i64, i1 }
			// done 是 field 2
			g.tmpIdx++
			doneGEP := fmt.Sprintf("%%r.done.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %%r.awaited to %%task*\n", doneGEP))
			g.tmpIdx++
			doneFieldGEP := fmt.Sprintf("%%r.done.field.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", doneFieldGEP, doneGEP))
			g.tmpIdx++
			doneVal := fmt.Sprintf("%%r.done.val.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("\t%s = load i1, i1* %s\n", doneVal, doneFieldGEP))
			// done=true → 加载结果（若有 resultVar）→ 跳到段体
			// done=false → yield（ret void）
			sb.WriteString(fmt.Sprintf("\tbr i1 %s, label %%s%d_cont, label %%s%d_yield\n", doneVal, segIdx, segIdx))
			// cont 块：task 已完成，加载结果到 resultVar（慢路径专用）
			sb.WriteString(fmt.Sprintf("s%d_cont:\n", segIdx))
			if prevAwait.resultVar != "" {
				g.loadTaskResult(sb, "%r.awaited", prevAwait.resultVar, fields)
			}
			sb.WriteString(fmt.Sprintf("\tbr label %%s%d\n", segIdx))
			// yield 块：未完成，ret void
			sb.WriteString(fmt.Sprintf("s%d_yield:\n", segIdx))
			sb.WriteString(fmt.Sprintf("\tcall void @nolang_async_wait(i8* %%r.task)\n"))
			g.emitCoroSaveLocals(sb, stateType, fields)
			sb.WriteString("\tret void\n")
		}

		// 生成段体
		sb.WriteString(fmt.Sprintf("s%d:\n", segIdx))
		// 设置 coroInAsyncFunc 标志，使 awy 语句生成 yield 逻辑
		g.coroInAsyncFunc = true
		g.coroAwaitPoints = nil
		g.indentLevel = 1
		g.blockTerminated = false
		// 生成段内语句（prevIdx 到 segEnd）
		for i := prevIdx; i < segEnd; i++ {
			g.generateStatement(sb, stmts[i])
		}
		// 如果当前段是 awy 段（segIdx < len(awaitIndices)），生成 awy yield 逻辑
		if segIdx < len(awaitIndices) {
			awyStmt := stmts[awaitIndices[segIdx].stmtIdx]
			g.generateAwaitYield(sb, awyStmt, segIdx+1, stateType, fields)
		} else {
			// 最终段：若段体未以 terminator 结束（如 return/break），补 br 到 s_end
			if !g.blockTerminated {
				sb.WriteString("\tbr label %s_end\n")
			}
		}
		g.coroInAsyncFunc = false
		prevIdx = segEnd + 1
	}

	// 最终段结束：设 state=-1，设 task.done=true，ret void
	// 注意：不在此处调用 @nolang_async_done — 事件循环检测到 done=true 后会自动唤醒等待者。
	sb.WriteString("s_end:\n")
	sb.WriteString("\tstore i32 -1, i32* %r.state.gep\n")
	// 保存结果参数回 coro_state
	g.emitCoroSaveResults(sb, stateType, fields)
	// 设 task.done = true（task 从 coro_state.__task 字段加载）
	g.tmpIdx++
	doneTaskCast := fmt.Sprintf("%%r.end.taskcast.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %%r.task to %%task*\n", doneTaskCast))
	g.tmpIdx++
	doneFieldGEP := fmt.Sprintf("%%r.end.done.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", doneFieldGEP, doneTaskCast))
	sb.WriteString(fmt.Sprintf("\tstore i1 true, i1* %s\n", doneFieldGEP))
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")
}

// generateAwaitYield 在 awy 语句处生成 yield 逻辑。
// awy 语句可能是：
//   - r = awy f-async(args) — 创建 task，入队，检查 done，yield 或继续
//   - r = awy h — 检查 h.done，yield 或继续
func (g *Generator) generateAwaitYield(sb *strings.Builder, stmt parser.Statement, nextState int, stateType string, fields []coroField) {
	var awyExpr *parser.AwaitExpression
	var resultVar string

	switch s := stmt.(type) {
	case *parser.LetStatement:
		awyExpr, _ = s.Value.(*parser.AwaitExpression)
		resultVar = s.Name.Value
	case *parser.ExpressionStatement:
		awyExpr, _ = s.Expression.(*parser.AwaitExpression)
	}

	if awyExpr == nil {
		return
	}

	// 生成 awy 表达式，获取 task 指针
	taskPtr := g.generateAwaitForCoro(sb, awyExpr, resultVar)
	if taskPtr == "" {
		taskPtr = "null"
	}

	// 检查 task.done
	g.tmpIdx++
	doneTaskCast := fmt.Sprintf("%%awy.done.cast.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %s to %%task*\n", doneTaskCast, taskPtr))
	g.tmpIdx++
	doneGEP := fmt.Sprintf("%%awy.done.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", doneGEP, doneTaskCast))
	g.tmpIdx++
	doneVal := fmt.Sprintf("%%awy.done.val.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = load i1, i1* %s\n", doneVal, doneGEP))
	sb.WriteString(fmt.Sprintf("\tbr i1 %s, label %%s_cont_%d, label %%s_yield_%d\n", doneVal, nextState-1, nextState-1))

	// yield：未完成
	sb.WriteString(fmt.Sprintf("s_yield_%d:\n", nextState-1))
	// store state
	sb.WriteString(fmt.Sprintf("\tstore i32 %d, i32* %%r.state.gep\n", nextState))
	// store awaited task (field 2 = __awaited)，供下次 resume 的 check 块读取
	sb.WriteString(fmt.Sprintf("\tstore i8* %s, i8** %%r.awaited.gep\n", taskPtr))
	// 保存局部变量回 coro_state
	g.emitCoroSaveLocals(sb, stateType, fields)
	// call wait
	sb.WriteString(fmt.Sprintf("\tcall void @nolang_async_wait(i8* %s)\n", taskPtr))
	sb.WriteString("\tret void\n")

	// continue：完成，加载结果
	sb.WriteString(fmt.Sprintf("s_cont_%d:\n", nextState-1))
	// 如果有结果变量，从 task 加载结果
	if resultVar != "" {
		g.loadTaskResult(sb, taskPtr, resultVar, fields)
	}
	// 快速路径：task 已完成，跳过 check 块，直接进入下一段体。
	// （check 块仅供 resume 路径使用：协程被重新调度时检查 awaited task 是否完成）
	sb.WriteString(fmt.Sprintf("\tbr label %%s%d\n", nextState))
}

// generateAwaitForCoro 在协程上下文中生成 awy 表达式，返回 task 指针（i8*）。
func (g *Generator) generateAwaitForCoro(sb *strings.Builder, expr *parser.AwaitExpression, resultVar string) string {
	// Case 1: awy f-async(args) — 创建 task 并入队
	if call, ok := expr.Right.(*parser.CallExpression); ok {
		if g.isAsyncCall(call) {
			return g.createTaskAndEnqueue(sb, call, resultVar)
		}
	}
	// Case 2: awy <identifier> — 已有 task 变量
	// 在 coro 上下文中，task 变量类型为 %task*（指向堆任务的指针），
	// 需从 alloca 加载 %task* 再 bitcast 为 i8*。
	// 直接使用 varAddr 会得到 alloca 地址（值拷贝），其 done 字段
	// 不会被事件循环更新（事件循环操作的是堆任务）。
	if ident, ok := expr.Right.(*parser.Identifier); ok {
		taskVarAddr := g.varAddr(ident.Value)
		g.tmpIdx++
		taskLoaded := fmt.Sprintf("%%awy.task.load.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("\t%s = load %%task*, %%task** %s\n", taskLoaded, taskVarAddr))
		g.tmpIdx++
		cast := fmt.Sprintf("%%awy.task.cast.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("\t%s = bitcast %%task* %s to i8*\n", cast, taskLoaded))
		return cast
	}
	return ""
}

// createTaskAndEnqueue 创建 task 并入队，返回 task 指针（i8*）。
func (g *Generator) createTaskAndEnqueue(sb *strings.Builder, call *parser.CallExpression, resultVar string) string {
	// 复用 prepareAsyncCall 生成 wrapper 和 args
	wrapperName, argsBitcast, _, resultType := g.prepareAsyncCall(sb, call)
	if wrapperName == "" {
		return ""
	}

	// 创建 task 结构（堆分配，因为 task 入队后需要在 yield 后存活）
	// %task = { void (i8*)*, i64, i1 }
	// field 0: resume_fn (wrapper)
	// field 1: coro_state_ptr (args)
	// field 2: done (false)
	g.tmpIdx++
	memReg := fmt.Sprintf("%%coro.task.mem.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = call i8* @malloc(i64 24)\n", memReg))
	g.tmpIdx++
	taskAddr := fmt.Sprintf("%%coro.task.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %s to %%task*\n", taskAddr, memReg))

	// store resume_fn (field 0)
	g.tmpIdx++
	f0GEP := fmt.Sprintf("%%coro.task.f0.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", f0GEP, taskAddr))
	sb.WriteString(fmt.Sprintf("\tstore void (i8*)* @%s, void (i8*)** %s\n", wrapperName, f0GEP))

	// store coro_state_ptr (field 1) — 这里用 args 作为 state
	g.tmpIdx++
	f1GEP := fmt.Sprintf("%%coro.task.f1.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", f1GEP, taskAddr))
	// field 1 是 i8*，但存储的是 coro_state 指针
	// 简化：直接存储 args bitcast
	g.storeDataPtrField(sb, argsBitcast, f1GEP)

	// store done = false (field 2)
	g.tmpIdx++
	f2GEP := fmt.Sprintf("%%coro.task.f2.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", f2GEP, taskAddr))
	sb.WriteString(fmt.Sprintf("\tstore i1 false, i1* %s\n", f2GEP))

	// 入队
	g.tmpIdx++
	taskCast := fmt.Sprintf("%%coro.task.cast.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast %%task* %s to i8*\n", taskCast, taskAddr))
	sb.WriteString(fmt.Sprintf("\tcall void @nolang_async_enqueue(i8* %s)\n", taskCast))

	// 如果有结果变量，记录结果类型
	if resultVar != "" && g.taskResultTypes != nil {
		g.taskResultTypes[resultVar] = resultType
	}

	return taskCast
}

// loadTaskResult 从 task 加载结果到结果变量。
func (g *Generator) loadTaskResult(sb *strings.Builder, taskPtr string, resultVar string, fields []coroField) {
	// 获取结果类型
	resultType := "i64"
	if g.taskResultTypes != nil {
		if t, ok := g.taskResultTypes[resultVar]; ok {
			resultType = t
		}
	}
	// task.field1 (i64) 存储的是 args 结构指针。
	// prepareAsyncCall 生成的 args 结构：{ i8* result_ptr, i8* arg1, ... }
	// result_ptr 指向结果缓冲区，需先加载 result_ptr 再加载结果值。
	g.tmpIdx++
	taskTypedCast := fmt.Sprintf("%%coro.lr.task.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %s to %%task*\n", taskTypedCast, taskPtr))
	g.tmpIdx++
	statePtrGEP := fmt.Sprintf("%%coro.lr.stateptr.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", statePtrGEP, taskTypedCast))
	g.tmpIdx++
	statePtr := fmt.Sprintf("%%coro.lr.state.%d", g.tmpIdx)
	statePtr = g.loadDataPtrField(sb, statePtrGEP)
	// args 结构 field 0 = result_ptr (i8*)，bitcast args 为 { i8* }* 并加载 result_ptr
	g.tmpIdx++
	argsTyped := fmt.Sprintf("%%coro.lr.args.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %s to { i8* }*\n", argsTyped, statePtr))
	g.tmpIdx++
	resultPtrGEP := fmt.Sprintf("%%coro.lr.resptr.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", resultPtrGEP, argsTyped))
	g.tmpIdx++
	resultPtr := fmt.Sprintf("%%coro.lr.rptr.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = load i8*, i8** %s\n", resultPtr, resultPtrGEP))
	// bitcast result_ptr to resultType*
	g.tmpIdx++
	resultTypedPtr := fmt.Sprintf("%%coro.lr.restyped.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = bitcast i8* %s to %s*\n", resultTypedPtr, resultPtr, resultType))
	// load result
	g.tmpIdx++
	resultVal := fmt.Sprintf("%%coro.lr.resval.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("\t%s = load %s, %s* %s\n", resultVal, resultType, resultType, resultTypedPtr))
	// store to result variable
	sb.WriteString(fmt.Sprintf("\tstore %s %s, %s* %s\n", resultType, resultVal, resultType, llvmVarRef(resultVar)))
}

// emitCoroSaveLocals 将所有局部变量和参数从 alloca store 回 coro_state。
func (g *Generator) emitCoroSaveLocals(sb *strings.Builder, stateType string, fields []coroField) {
	for _, f := range fields {
		if f.name == "__state" || f.name == "__task" || f.name == "__awaited" {
			continue
		}
		idx := g.coroFieldIdx[f.name]
		g.tmpIdx++
		gep := fmt.Sprintf("%%coro.sv.%s.%d", sanitizeLLVMName(f.name), g.tmpIdx)
		sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %s, %s* %%cs, i32 0, i32 %d\n",
			gep, stateType, stateType, idx))
		g.tmpIdx++
		val := fmt.Sprintf("%%coro.sv.%s.val.%d", sanitizeLLVMName(f.name), g.tmpIdx)
		sb.WriteString(fmt.Sprintf("\t%s = load %s, %s* %s\n", val, f.llvmTy, f.llvmTy, llvmVarRef(f.name)))
		sb.WriteString(fmt.Sprintf("\tstore %s %s, %s* %s\n", f.llvmTy, val, f.llvmTy, gep))
	}
}

// emitCoroSaveResults 将结果参数 store 回 coro_state（函数结束时调用）。
func (g *Generator) emitCoroSaveResults(sb *strings.Builder, stateType string, fields []coroField) {
	for _, f := range fields {
		if !f.isResult {
			continue
		}
		idx := g.coroFieldIdx[f.name]
		g.tmpIdx++
		gep := fmt.Sprintf("%%coro.sr.%s.%d", sanitizeLLVMName(f.name), g.tmpIdx)
		sb.WriteString(fmt.Sprintf("\t%s = getelementptr inbounds %s, %s* %%cs, i32 0, i32 %d\n",
			gep, stateType, stateType, idx))
		g.tmpIdx++
		val := fmt.Sprintf("%%coro.sr.%s.val.%d", sanitizeLLVMName(f.name), g.tmpIdx)
		sb.WriteString(fmt.Sprintf("\t%s = load %s, %s* %s\n", val, f.llvmTy, f.llvmTy, llvmVarRef(f.name)))
		sb.WriteString(fmt.Sprintf("\tstore %s %s, %s* %s\n", f.llvmTy, val, f.llvmTy, gep))
	}
}

// sortStrings 对字符串切片进行排序（简单插入排序，避免引入 sort 包）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// transformModuleAsync 将模块级代码（含 awy）包装为匿名 async 函数（coro）。
// 生成 coro_state.N / coro_init.N / coro_resume.N，以及 @main 入口（创建 task 并启动事件循环）。
// 模块级局部变量（非 globalVars）提升到 coro_state；全局变量通过 @global 访问。
//
// 设计：nolang 没有显式 main，模块级代码即入口。当模块级代码含 awy 时，
// 视为异步实现：将模块级语句作为匿名 async 函数体进行状态机变换，
// @main 仅负责创建 coro_state + task 并启动 @nolang_async_run 事件循环。
//
// 返回 true 表示已走异步路径（调用方应跳过普通 @main 生成）；false 表示无顶层 awy，未做任何变换。
func (g *Generator) transformModuleAsync(sb *strings.Builder, program *parser.Program) bool {
	// 收集模块级语句，应用与 generateMainFunction 相同的过滤逻辑：
	// - 排除函数/结构体/类型别名定义
	// - 跳过无需运行时初始化的全局变量 LetStatement（纯标量全局常量）
	// - 跳过函数引用变量
	var moduleStmts []parser.Statement
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition, *parser.StructDefinition, *parser.TypeAlias,
			*parser.EnumDefinition, *parser.TaggedEnumDefinition:
			// 这些定义在各自的专用 pass 中发射（全局常量/结构体类型），
			// 不应作为可执行语句进入 async 函数体。
			continue
		case *parser.LetStatement:
			// 跳过已作为全局发射的 LetStatement（非 str/arr/vec 且非重赋值）
			if g.globalVars != nil && g.globalVars[s.Name.Value] {
				isReassigned := g.reassignedVars != nil && g.reassignedVars[s.Name.Value]
				if !isReassigned {
					lt := g.varLLVMType(s)
					if s.Type == nil && g.varTypes != nil {
						if t, ok := g.varTypes[s.Name.Value]; ok {
							lt = t
						}
					}
					if lt != "%str-long" && lt != "%arr" && lt != "%vec" {
						continue
					}
				}
			}
			// 跳过函数引用变量
			if g.funcRefVars != nil && g.funcRefVars[s.Name.Value] {
				continue
			}
			moduleStmts = append(moduleStmts, stmt)
		default:
			moduleStmts = append(moduleStmts, stmt)
		}
	}

	// 快速检查：模块级顶层若无 awy，则不走异步路径，回退到普通 @main 生成。
	if len(collectTopLevelAwaitIndices(moduleStmts)) == 0 {
		return false
	}

	// 补充 generateMainFunction 未重置的状态（与 generateFunctionDefinition 对齐），
	// 确保 coro_resume 段体生成时各 map 已初始化。
	g.ssaTypes = make(map[string]string)
	g.varFnTypes = make(map[string]*parser.FunctionType)
	g.arraySizes = make(map[string]int64)
	g.taskResultTypes = make(map[string]string)
	g.futureResultTypes = make(map[string]string)
	if g.paramNames == nil {
		g.paramNames = make(map[string]bool)
	}
	g.funcParams = make(map[string]bool)
	g.ssaVersion = make(map[string]int)
	g.outputParamOrder = nil
	g.outBindState = nil

	// 构造合成 FunctionDefinition（匿名 async 入口，无参数无结果）
	fd := &parser.FunctionDefinition{
		Name: "__nolang_main_async",
		Body: &parser.BlockStatement{Statements: moduleStmts},
	}

	// 设置 coro 上下文状态
	g.curFuncName = "__nolang_main_async"
	g.inMainFunction = false
	g.isModuleAsyncWrap = true

	// 变换为状态机（生成 coro_state.N / coro_init.N / coro_resume.N）
	g.transformAsyncFunction(sb, fd)

	g.isModuleAsyncWrap = false

	// 生成 coro_trampoline.N（包装 coro_resume.N 为 i8*(i8*)* 签名，并写回 __task）
	num := g.asyncFuncCoroNum["__nolang_main_async"]
	g.generateCoroTrampoline(sb, num)

	// 生成 @main 入口：创建 coro_state + task，启动事件循环
	g.generateAsyncMainEntry(sb, num)
	return true
}

// generateCoroTrampoline 生成 coro_trampoline.N 函数。
// 签名：void @coro_trampoline.N(i8* %task_ptr)
// 职责：从 task.data (field 1, i64) 取 coro_state 指针，写回 task_ptr 到 coro_state.__task (field 1)，
//
//	然后调用 coro_resume.N(cs)。事件循环通过 resume_fn 调用此 trampoline。
func (g *Generator) generateCoroTrampoline(sb *strings.Builder, num int) {
	if g.coroTrampolineEmitted != nil && g.coroTrampolineEmitted[num] {
		return
	}
	if g.coroTrampolineEmitted == nil {
		g.coroTrampolineEmitted = make(map[int]bool)
	}
	g.coroTrampolineEmitted[num] = true

	stateType := g.coroStateName(num)
	resumeName := g.coroResumeName(num)
	trampName := fmt.Sprintf("coro_trampoline.%d", num)

	sb.WriteString(fmt.Sprintf("define void @%s(i8* %%task_ptr) {\n", sanitizeLLVMName(trampName)))
	sb.WriteString("entry:\n")
	// task = bitcast i8* %task_ptr to %task*
	sb.WriteString("\t%tr.task = bitcast i8* %task_ptr to %task*\n")
	// load data (field 1, i64) = coro_state pointer
	sb.WriteString("\t%tr.data.gep = getelementptr inbounds %task, %task* %tr.task, i32 0, i32 1\n")
	sb.WriteString("\t%tr.data.i64 = load i64, i64* %tr.data.gep\n")
	sb.WriteString("\t%tr.cs.i8 = inttoptr i64 %tr.data.i64 to i8*\n")
	sb.WriteString(fmt.Sprintf("\t%%tr.cs = bitcast i8* %%tr.cs.i8 to %s*\n", stateType))
	// 写回 task_ptr 到 coro_state.__task (field 1)，供 s_end 设 done=true
	sb.WriteString(fmt.Sprintf("\t%%tr.taskfld.gep = getelementptr inbounds %s, %s* %%tr.cs, i32 0, i32 1\n", stateType, stateType))
	sb.WriteString("\tstore i8* %task_ptr, i8** %tr.taskfld.gep\n")
	// call coro_resume.N(cs)
	sb.WriteString(fmt.Sprintf("\tcall void @%s(%s* %%tr.cs)\n", sanitizeLLVMName(resumeName), stateType))
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")
}

// generateAsyncMainEntry 生成 @main 函数，创建 coro_state + task 并启动事件循环。
func (g *Generator) generateAsyncMainEntry(sb *strings.Builder, num int) {
	stateType := g.coroStateName(num)
	initName := g.coroInitName(num)
	trampName := fmt.Sprintf("coro_trampoline.%d", num)

	g.inMainFunction = true
	g.curFuncRetType = "void"
	g.curFuncRetName = ""
	g.indentLevel = 0

	sb.WriteString("define i32 @main(i32 %c-argc, i8** %c-argv) {\n")
	g.indentLevel = 1
	g.emitLabel(sb, "entry")
	g.indentLevel = 2

	// store argc/argv
	sb.WriteString(fmt.Sprintf("%sstore i32 %%c-argc, i32* @.argc.addr\n", g.indent()))
	sb.WriteString(fmt.Sprintf("%sstore i8** %%c-argv, i8*** @.argv.addr\n", g.indent()))

	// alloca coro_state.N
	g.tmpIdx++
	csAddr := fmt.Sprintf("%%main.cs.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), csAddr, stateType))

	// call coro_init.N(cs) — 无参数
	sb.WriteString(fmt.Sprintf("%scall void @%s(%s* %s)\n", g.indent(), sanitizeLLVMName(initName), stateType, csAddr))

	// alloca %task
	g.tmpIdx++
	taskAddr := fmt.Sprintf("%%main.task.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%task\n", g.indent(), taskAddr))

	// store resume_fn (field 0) = @coro_trampoline.N
	g.tmpIdx++
	taskF0 := fmt.Sprintf("%%main.task.f0.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", g.indent(), taskF0, taskAddr))
	sb.WriteString(fmt.Sprintf("%sstore void (i8*)* @%s, void (i8*)** %s\n", g.indent(), sanitizeLLVMName(trampName), taskF0))

	// store data (field 1) = cs bitcast to i8*（storeDataPtrField: ptrtoint + store i64）
	g.tmpIdx++
	csCast := fmt.Sprintf("%%main.cs.cast.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), csCast, stateType, csAddr))
	g.tmpIdx++
	taskF1 := fmt.Sprintf("%%main.task.f1.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", g.indent(), taskF1, taskAddr))
	g.storeDataPtrField(sb, csCast, taskF1)

	// store done (field 2) = false
	g.tmpIdx++
	taskF2 := fmt.Sprintf("%%main.task.f2.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", g.indent(), taskF2, taskAddr))
	sb.WriteString(fmt.Sprintf("%sstore i1 false, i1* %s\n", g.indent(), taskF2))

	// bitcast task* to i8*
	g.tmpIdx++
	taskI8 := fmt.Sprintf("%%main.task.i8.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), taskI8, taskAddr))

	// 启动事件循环
	sb.WriteString(fmt.Sprintf("%scall void @nolang_async_run(i8* %s)\n", g.indent(), taskI8))

	sb.WriteString(fmt.Sprintf("%sret i32 0\n", g.indent()))
	g.indentLevel = 0
	sb.WriteString("}\n\n")
	g.inMainFunction = false
}
