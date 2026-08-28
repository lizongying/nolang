package llvm

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// llvmTypeAlign 返回 LLVM 類型的對齊字節數（與 LLVM ABI 對齊規則一致）。
// 透過 classifyTypeKind 區分內聯陣列、結構體與純量，取代字串前綴判斷。
// 注意：%vec、%str-long、%arr 雖然是內建型別，但也在 structTypes 中註冊了欄位定義，
// 需要走結構體對齊計算路徑。
func (g *Generator) llvmTypeAlign(llvmType string) int64 {
	desc := g.classifyTypeKind(llvmType)
	switch desc.Kind {
	case KindInlineArray:
		// [N x T] 的對齊 = 元素對齊
		return g.llvmTypeAlign(desc.ElemType)
	case KindUserStruct, KindVec, KindStr, KindArr:
		// 用戶結構體與內建容器結構體（%vec/%str-long/%arr 均在 structTypes 中註冊）：
		// 對齊 = 所有欄位中最大的對齊
		structName := desc.StructName
		if structName == "" {
			structName = strings.TrimPrefix(llvmType, "%")
		}
		if g.structTypes != nil {
			if fields, ok := g.structTypes[structName]; ok {
				var maxAlign int64 = 1
				for _, f := range fields {
					a := g.llvmTypeAlign(f.typ)
					if a > maxAlign {
						maxAlign = a
					}
				}
				return maxAlign
			}
		}
		return 8
	}
	// 純量與其他內建型別（%option 等）
	switch llvmType {
	case "i1", "i8":
		return 1
	case "i16":
		return 2
	case "i32", "float":
		return 4
	case "i64", "i8*", "double":
		return 8
	case "i128":
		return 16
	default:
		return 8
	}
}

// llvmTypeSize 計算 LLVM 類型的字節大小。
// 透過 classifyTypeKind 區分內聯陣列、結構體與純量，取代字串前綴判斷。
// 注意：%vec、%str-long、%arr 雖然是內建型別，但也在 structTypes 中註冊了欄位定義，
// 需要走結構體大小計算路徑。
func (g *Generator) llvmTypeSize(llvmType string) int64 {
	desc := g.classifyTypeKind(llvmType)
	switch desc.Kind {
	case KindInlineArray:
		// [N x T] → N * sizeof(T)
		return desc.ArrayN * g.llvmTypeSize(desc.ElemType)
	case KindUserStruct, KindVec, KindStr, KindArr:
		// 用戶結構體與內建容器結構體（%vec/%str-long/%arr 均在 structTypes 中註冊）：
		// 依 LLVM ABI 對齊規則計算每個欄位的大小，末尾補齊到最大對齊值。
		structName := desc.StructName
		if structName == "" {
			structName = strings.TrimPrefix(llvmType, "%")
		}
		if g.structTypes != nil {
			if fields, ok := g.structTypes[structName]; ok {
				var offset int64
				var maxAlign int64 = 1
				for _, f := range fields {
					fa := g.llvmTypeAlign(f.typ)
					if fa > maxAlign {
						maxAlign = fa
					}
					// 將 offset 向上對齊到欄位對齊值
					offset = (offset + fa - 1) / fa * fa
					offset += g.llvmTypeSize(f.typ)
				}
				// 結構體總大小補齊到最大對齊值
				if maxAlign > 0 {
					offset = (offset + maxAlign - 1) / maxAlign * maxAlign
				}
				if offset > 0 {
					return offset
				}
			}
		}
	}
	// 純量與其他內建型別（%option 等）
	switch llvmType {
	case "i1":
		return 1
	case "i8":
		return 1
	case "i16":
		return 2
	case "i32":
		return 4
	case "i64", "i8*", "double":
		return 8
	case "i128":
		return 16
	case "%option":
		return 24
	case "float":
		return 4
	default:
		return 8
	}
}

// shouldStackAllocArray 判定局部固定陣列是否適合用 alloca（棧分配）而非 malloc（堆分配）。
// 條件：元素為基礎定寬型別（i1/i8/i16/i32/i64/double/float）且總尺寸 ≤ 閾值。
// 超大陣列、vec、含動態類型元素（struct/指標）的陣列仍用堆分配。
func shouldStackAllocArray(llvmElemType string, totalSize int64) bool {
	const maxStackArrayBytes = int64(256) // 64 * i8 = 64B, 足夠 buf[64]byte 等常見小陣列
	switch llvmElemType {
	case "i1", "i8", "i16", "i32", "i64", "i128", "double", "float":
		return totalSize > 0 && totalSize <= maxStackArrayBytes
	default:
		return false
	}
}

func (g *Generator) emitLifetimeEnd(sb *strings.Builder) {
	for _, v := range g.funcVars {
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.end.p0i8(i64 %d, i8* %s)\n", g.indent(), v.Size, llvmVarRef(v.Name)))
	}
}

// flushOutputBindings 在函數返回前，按當前 SSA 版本查表，
// 將延遲綁定的輸出參數值實際寫入輸出參數指標。
// SSA 版本由 if 分支前後的 save/restore 隔離：
// - 分支內的賦值遞增版本，return 時 flush 用遞增後的版本查表 → 正確
// - 分支後版本恢復，隱式返回 flush 用恢復後的版本查表 → 不會命中分支內的綁定
// - 無綁定的版本（如常量賦值的立即 store）跳過 flush
func (g *Generator) flushOutputBindings(sb *strings.Builder) {
	if g.outputBindings == nil || len(g.outputBindings) == 0 {
		return
	}
	// 排序確保輸出順序確定（與 emitHeapFree/emitGlobalHeapFree 一致）。
	sortedOutNames := make([]string, 0, len(g.outputBindings))
	for name := range g.outputBindings {
		sortedOutNames = append(sortedOutNames, name)
	}
	sort.Strings(sortedOutNames)
	for _, name := range sortedOutNames {
		versions := g.outputBindings[name]
		if len(versions) == 0 {
			continue
		}
		version := 0
		if g.ssaVersion != nil {
			version = g.ssaVersion[name]
		}
		binding, ok := versions[version]
		if !ok {
			continue // 該版本無延遲綁定，立即 store 已處理
		}
		paramPtr := g.varAddr(name)
		paramType, ok := g.varTypes[name]
		if !ok {
			paramType = binding.llvmType
		}
		srcLoad := g.tmpReg("outmove.src")
		srcPtr := g.varAddr(binding.sourceVar)
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
			g.indent(), srcLoad, toLLVMType(binding.llvmType), toLLVMType(binding.llvmType), srcPtr))
		// 型別轉換：源型別與參數型別可能不同（如 i64 → i8, i64 → float）
		storeVal := srcLoad
		if binding.llvmType != paramType {
			convReg := g.tmpReg("outmove.conv")
			converted := true
			switch {
			case g.isIntegerLLVMType(binding.llvmType) && g.isIntegerLLVMType(paramType):
				// 整數 → 整數：trunc 或 zext/sext
				if llvmIntBitWidth(binding.llvmType) > llvmIntBitWidth(paramType) {
					sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, toLLVMType(binding.llvmType), srcLoad, toLLVMType(paramType)))
				} else {
					op := widenExtOp(binding.llvmType)
					sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to %s\n", g.indent(), convReg, op, toLLVMType(binding.llvmType), srcLoad, toLLVMType(paramType)))
				}
			case g.isIntegerLLVMType(binding.llvmType) && (paramType == "float" || paramType == "double"):
				// 整數 → 浮點：sitofp
				sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to %s\n", g.indent(), convReg, toLLVMType(binding.llvmType), srcLoad, paramType))
			case (binding.llvmType == "float" || binding.llvmType == "double") && g.isIntegerLLVMType(paramType):
				// 浮點 → 整數：fptosi
				sb.WriteString(fmt.Sprintf("%s%s = fptosi %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, toLLVMType(paramType)))
			case binding.llvmType == "float" && paramType == "double":
				// float → double：fpext
				sb.WriteString(fmt.Sprintf("%s%s = fpext %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
			case binding.llvmType == "double" && paramType == "float":
				// double → float：fptrunc
				sb.WriteString(fmt.Sprintf("%s%s = fptrunc %s %s to %s\n", g.indent(), convReg, binding.llvmType, srcLoad, paramType))
			default:
				// 其他（如 struct 型別相同但名稱不同）：直接 store
				converted = false
			}
			if converted {
				storeVal = convReg
			}
		}
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
			g.indent(), toLLVMType(paramType), storeVal, toLLVMType(paramType), paramPtr))
	}
}

// emitHeapFree 在函數返回前（move 之後），釋放未被 move 的局部堆變數的資料緩衝區。
// heapVars 記錄所有有堆分配的局部變數，movedVars 記錄已 move 到輸出參數的變數（不應 free）。
// 各型別的 data 欄位索引（data 為 i64 地址值）：
//
//	%vec:       field 2 (i64 data)
//	%str-long:  field 2 (i64 data)
//	%arr:       field 1 (i64 data)

// ---- Liveness 预分析：判定 b=a 中源变量 a 是否在后续被引用 ----
//
// 改进版 liveness 分析（替代旧的 flattenStmts + stmtContainsVarRef 线性扫描）：
//
// 1. 读写区分：只有「读引用」才阻止 move。纯赋值目标（如 a = [1,2,3]）不阻止 move，
//    因为 freeOldHeapValue 会正确检测 moved 状态并跳过 free。但 a = f(a) 这种 RHS
//    引用源变量的情况仍会阻止 move（通过 exprContainsVarRead 检查 RHS）。
//
// 2. 分支感知：当 out=a 在某个 if/else 分支内（move 到输出参数）时，
//    互斥分支中的读引用不阻止 move。输出参数的 move 有运行时位图追踪
//    （detectBranchMoveToOut + hasBranchMove），可以安全跳过互斥分支。
//    对局部变量间 b=a（move-to-local），无运行时位图追踪，
//    互斥分支中的读引用仍阻止 move（保守，正确）。
//
// 3. 循环体引用：for 循环体内的读引用阻止 move（循环体可能多次执行，
//    move 后源变量不再拥有 data，读取会 use-after-free）。

// branchNode 标识语句所在的分支上下文节点。
// 多个 branchNode 组成 chain，从外到内表示嵌套的分支路径。
type branchNode struct {
	nodeID    int // 分支节点的唯一 ID（对应 IfExpression/ConditionalExpression）
	branchIdx int // 0 = then/consequence, 1 = else/alternative
}

// flatStmtEntry 是展平后的语句条目，携带分支上下文信息。
type flatStmtEntry struct {
	stmt      parser.Statement
	branchCtx []branchNode // nil = 顶层；非空 = 在分支内
}

// computeMoveEligibility 遍历函数体，对每个 b=a（LetStatement，RHS 为 Identifier）
// 判定源变量 a 是否在后续语句中被**读取**引用。
// 若未被读取引用 → moveEligible[stmt]=true（可 move）。
//
// 三项改进（相比旧版 flattenStmts + stmtContainsVarRef）：
// 1. 读写区分：纯赋值目标（a = expr）不视为读引用
// 2. 分支感知：互斥分支中的读引用不阻止 move
// 3. 循环体：循环体内的读引用阻止 move（保守，正确）
func (g *Generator) computeMoveEligibility(body *parser.BlockStatement) {
	g.moveEligible = make(map[*parser.LetStatement]bool)
	if body == nil {
		return
	}
	branchNodeCounter = 0 // 重置分支节点计数器，确保每个函数的分支 ID 从 0 开始
	entries := flattenStmtsWithCtx(body.Statements, nil)
	for i, entry := range entries {
		ls, ok := entry.stmt.(*parser.LetStatement)
		if !ok || ls.IsSynthetic {
			continue
		}
		ident, ok := ls.Value.(*parser.Identifier)
		if !ok {
			continue
		}
		// 分支感知改进仅对 move-to-out（输出参数）安全：
		// - move-to-out 有运行时位图追踪（detectBranchMoveToOut + hasBranchMove），
		//   可以安全地跳过互斥分支中的读引用。
		// - move-to-local（局部变量间 b=a）无运行时位图追踪，
		//   编译期 movedVarBitset 无法区分运行时分支，
		//   互斥分支中的读引用必须仍然阻止 move（保守）。
		canUseBranchAware := g.outputParamNames != nil && ls.Name != nil && g.outputParamNames[ls.Name.Value]
		usedAfter := false
		for j := i + 1; j < len(entries); j++ {
			// 仅对 move-to-out 跳过互斥分支
			if canUseBranchAware && branchesMutuallyExclusive(entry.branchCtx, entries[j].branchCtx) {
				continue
			}
			if stmtContainsVarRead(entries[j].stmt, ident.Value) {
				usedAfter = true
				break
			}
		}
		g.moveEligible[ls] = !usedAfter
	}
}

// branchesMutuallyExclusive 判定两个分支上下文是否互斥。
// 互斥条件：两条 chain 共享公共前缀，但在相同分支节点处分叉到不同分支。
// 例：[{1,0}]（then）和 [{1,1}]（else）互斥；
//      [{1,0}] 和 nil（顶层）不互斥（then 汇合后回到顶层）。
func branchesMutuallyExclusive(ctx1, ctx2 []branchNode) bool {
	minLen := len(ctx1)
	if len(ctx2) < minLen {
		minLen = len(ctx2)
	}
	for k := 0; k < minLen; k++ {
		if ctx1[k].nodeID == ctx2[k].nodeID && ctx1[k].branchIdx != ctx2[k].branchIdx {
			// 在同一分支节点处分叉到不同分支 → 互斥
			return true
		}
		if ctx1[k].nodeID != ctx2[k].nodeID {
			// 分支节点不同 → 不是同一分支层次，不互斥
			break
		}
	}
	return false
}

// flattenStmtsWithCtx 将语句列表递归展开为一维列表，同时追踪分支上下文。
// branchCtx 是当前语句列表所在的分支上下文链（nil = 顶层）。
func flattenStmtsWithCtx(stmts []parser.Statement, branchCtx []branchNode) []flatStmtEntry {
	var result []flatStmtEntry
	for _, stmt := range stmts {
		result = append(result, flatStmtEntry{stmt: stmt, branchCtx: branchCtx})
		switch s := stmt.(type) {
		case *parser.ExpressionStatement:
			result = append(result, flattenExprStmtsWithCtx(s.Expression, branchCtx)...)
		case *parser.ForStatement:
			if s.Init != nil {
				result = append(result, flattenStmtsWithCtx([]parser.Statement{s.Init}, branchCtx)...)
			}
			if s.Update != nil {
				result = append(result, flattenStmtsWithCtx([]parser.Statement{s.Update}, branchCtx)...)
			}
			if s.Body != nil {
				// 循环体不是分支互斥的（循环体在循环条件成立时执行），
				// 用同一 branchCtx（不加新分支节点）
				result = append(result, flattenStmtsWithCtx(s.Body.Statements, branchCtx)...)
			}
		case *parser.MultiAssignStatement:
			// MultiAssign targets are expressions, not statements
		}
	}
	return result
}

// flattenExprStmtsWithCtx 展开表达式语句中嵌套的块（if 表达式的 then/else 体），
// 同时追踪分支上下文。
func flattenExprStmtsWithCtx(expr parser.Expression, branchCtx []branchNode) []flatStmtEntry {
	var result []flatStmtEntry
	collectNestedStmtsWithCtx(expr, branchCtx, &result)
	return result
}

var branchNodeCounter int // 用于生成唯一分支节点 ID

func collectNestedStmtsWithCtx(expr parser.Expression, branchCtx []branchNode, result *[]flatStmtEntry) {
	switch e := expr.(type) {
	case *parser.IfExpression:
		// 为此 IfExpression 分配唯一 nodeID
		nodeID := branchNodeCounter
		branchNodeCounter++
		// condition 在分支之前，沿用当前 branchCtx
		if e.Condition != nil {
			collectNestedStmtsWithCtx(e.Condition, branchCtx, result)
		}
		if e.Consequence != nil {
			thenCtx := append([]branchNode(nil), branchCtx...)
			thenCtx = append(thenCtx, branchNode{nodeID: nodeID, branchIdx: 0})
			*result = append(*result, flattenStmtsWithCtx(e.Consequence.Statements, thenCtx)...)
		}
		if e.Alternative != nil {
			elseCtx := append([]branchNode(nil), branchCtx...)
			elseCtx = append(elseCtx, branchNode{nodeID: nodeID, branchIdx: 1})
			*result = append(*result, flattenStmtsWithCtx(e.Alternative.Statements, elseCtx)...)
		}
	case *parser.InfixExpression:
		collectNestedStmtsWithCtx(e.Left, branchCtx, result)
		collectNestedStmtsWithCtx(e.Right, branchCtx, result)
	case *parser.PrefixExpression:
		collectNestedStmtsWithCtx(e.Right, branchCtx, result)
	case *parser.CallExpression:
		collectNestedStmtsWithCtx(e.Function, branchCtx, result)
		for _, arg := range e.Arguments {
			collectNestedStmtsWithCtx(arg, branchCtx, result)
		}
	case *parser.GroupedExpression:
		collectNestedStmtsWithCtx(e.Expression, branchCtx, result)
	case *parser.ConditionalExpression:
		// ConditionalExpression（三元条件）也是互斥分支
		nodeID := branchNodeCounter
		branchNodeCounter++
		collectNestedStmtsWithCtx(e.Condition, branchCtx, result)
		thenCtx := append([]branchNode(nil), branchCtx...)
		thenCtx = append(thenCtx, branchNode{nodeID: nodeID, branchIdx: 0})
		collectNestedStmtsWithCtx(e.Consequence, thenCtx, result)
		elseCtx := append([]branchNode(nil), branchCtx...)
		elseCtx = append(elseCtx, branchNode{nodeID: nodeID, branchIdx: 1})
		collectNestedStmtsWithCtx(e.Alternative, elseCtx, result)
	case *parser.AssignExpression:
		if e.Left != nil {
			collectNestedStmtsWithCtx(e.Left, branchCtx, result)
		}
		if e.Value != nil {
			collectNestedStmtsWithCtx(e.Value, branchCtx, result)
		}
	}
}

// stmtContainsVarRead 检查语句中是否**读取**引用了指定变量名。
// 与旧版 stmtContainsVarRef 的区别：纯赋值目标（如 a = [1,2,3]）不视为读引用，
// 因为 freeOldHeapValue 会正确检测 moved 状态并跳过 free，move 是安全的。
// 但 a = f(a) 这种 RHS 引用源变量的情况仍会返回 true（通过 exprContainsVarRead 检查 RHS）。
func stmtContainsVarRead(stmt parser.Statement, varName string) bool {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// 赋值目标（LHS）不视为读引用：
		// freeOldHeapValue 在 move 后会检测 moved 状态并跳过 free，
		// 所以 a = [1,2,3] 这种纯重赋值不会 use-after-move。
		// 但 RHS 引用源变量（如 a = f(a)）仍然是读引用。
		if s.Value != nil {
			return exprContainsVarRead(s.Value, varName)
		}
	case *parser.ExpressionStatement:
		return exprContainsVarRead(s.Expression, varName)
	case *parser.ForStatement:
		if s.Condition != nil && exprContainsVarRead(s.Condition, varName) {
			return true
		}
		if s.CountExpr != nil && exprContainsVarRead(s.CountExpr, varName) {
			return true
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				if stmtContainsVarRead(ss, varName) {
					return true
				}
			}
		}
	case *parser.ReturnStatement:
		// ReturnValue 始终为 nil
	case *parser.MultiAssignStatement:
		// MultiAssign targets（如 a, b = f()）不视为读引用，
		// 但 targets 中的表达式可能引用变量（如 a[i], x = f()）
		for _, t := range s.Targets {
			if exprContainsVarRead(t, varName) {
				return true
			}
		}
		if s.Value != nil && exprContainsVarRead(s.Value, varName) {
			return true
		}
	case *parser.BreakStatement:
	case *parser.ContinueStatement:
	}
	return false
}

// exprContainsVarRead 递归检查表达式中是否**读取**引用了指定变量名。
// 与旧版 exprContainsVarRef 的区别：在 AssignExpression 中，
// 左值（赋值目标）不视为读引用。
func exprContainsVarRead(expr parser.Expression, varName string) bool {
	switch e := expr.(type) {
	case *parser.Identifier:
		return e.Value == varName
	case *parser.InfixExpression:
		return exprContainsVarRead(e.Left, varName) || exprContainsVarRead(e.Right, varName)
	case *parser.PrefixExpression:
		return exprContainsVarRead(e.Right, varName)
	case *parser.CallExpression:
		if exprContainsVarRead(e.Function, varName) {
			return true
		}
		for _, arg := range e.Arguments {
			if exprContainsVarRead(arg, varName) {
				return true
			}
		}
	case *parser.DotExpression:
		return exprContainsVarRead(e.Receiver, varName)
	case *parser.IndexExpression:
		return exprContainsVarRead(e.Left, varName) || exprContainsVarRead(e.Index, varName)
	case *parser.IfExpression:
		if e.Condition != nil && exprContainsVarRead(e.Condition, varName) {
			return true
		}
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				if stmtContainsVarRead(ss, varName) {
					return true
				}
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				if stmtContainsVarRead(ss, varName) {
					return true
				}
			}
		}
	case *parser.SliceExpression:
		if exprContainsVarRead(e.Left, varName) {
			return true
		}
		if e.Range != nil {
			if e.Range.Start != nil && exprContainsVarRead(e.Range.Start, varName) {
				return true
			}
			if e.Range.End != nil && exprContainsVarRead(e.Range.End, varName) {
				return true
			}
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			if exprContainsVarRead(elem, varName) {
				return true
			}
		}
	case *parser.GroupedExpression:
		return exprContainsVarRead(e.Expression, varName)
	case *parser.ConditionalExpression:
		return exprContainsVarRead(e.Condition, varName) ||
			exprContainsVarRead(e.Consequence, varName) ||
			exprContainsVarRead(e.Alternative, varName)
	case *parser.AssignExpression:
		// 左值（赋值目标）不视为读引用
		// 但右值（Value）中的引用仍然是读引用
		if e.Value != nil && exprContainsVarRead(e.Value, varName) {
			return true
		}
	case *parser.RangeExpression:
		if e.Start != nil && exprContainsVarRead(e.Start, varName) {
			return true
		}
		if e.End != nil && exprContainsVarRead(e.End, varName) {
			return true
		}
	// Literals (Integer, Float, String, Char, Byte, Boolean, Nil) — no refs
	}
	return false
}

// trackLocalHeapVar 將局部變數加入 heapVars 追蹤，用於函數結束時深層 free。
// 跳過參數（paramNames）和輸出參數（outputParamNames，由呼叫者管理）。
// 同時分配 varIdx（堆變數下標），用於 bitmap bit 定位。
func (g *Generator) trackLocalHeapVar(name, llvmType string) {
	if g.heapVars == nil {
		return
	}
	if _, isLocal := g.funcLocalNames[name]; !isLocal {
		return
	}
	if _, isParam := g.paramNames[name]; isParam {
		return
	}
	if g.outputParamNames != nil && g.outputParamNames[name] {
		return
	}
	g.heapVars[name] = llvmType
	if g.heapVarIndex != nil {
		if _, exists := g.heapVarIndex[name]; !exists {
			g.heapVarIndex[name] = g.nextHeapVarIdx
			g.nextHeapVarIdx++
		}
	}
}

// computeAssignedSeed 計算 AssignedFact 的入口初值（entry IN）：所有「被寫過
// （賦值 LHS）/ vec·arr」的局部堆變數在函數入口即標記為 assigned。這保證此類變量
// 恆為 triMust，絕不會被 Phase 2 誤判為「從未持有堆」而跳過 free。只有「從不寫、
// 又非 vec·arr」的局部（其 heap data 指針恆為 memset 的 NULL）才可能被判 triMustNot，
// 而該類變量的 free 本就是 NULL 守衛的 no-op，跳過無洩漏、無崩潰。
func (g *Generator) computeAssignedSeed(fd *parser.FunctionDefinition) bitsetFact {
	seed := newBitsetFact(g.nextHeapVarIdx)
	names := make(map[string]bool)
	if fd != nil && fd.Body != nil {
		g.collectAssignLHS(fd.Body.Statements, names)
	}
	// 所有被寫過的局部（LHS 名 → varIdx）：
	for name := range names {
		if idx, ok := g.heapVarIndex[name]; ok {
			seed.set(idx)
		}
	}
	// vec·arr 局部（prologue 已 malloc buffer，入口即持有堆；與 entry effAssign 互補）：
	if g.heapVars != nil {
		for name, t := range g.heapVars {
			if t == "%vec" || t == "%arr" {
				if idx, ok := g.heapVarIndex[name]; ok {
					seed.set(idx)
				}
			}
		}
	}
	return seed
}

// collectAssignLHS 遞歸收集函數體內所有「賦值 LHS」變量名：LetStatement.Name、
// MultiAssignStatement.Targets、ForStatement.IterRange.Variable。對表達式位置的
// 賦值語句（if/match/while 表達式內嵌套語句塊，match 已 desugar 為 IfExpression）
// 透過 collectAssignLHSExpr 觸達。
func (g *Generator) collectAssignLHS(stmts []parser.Statement, names map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name != nil {
				names[s.Name.Value] = true
			}
			if s.Value != nil {
				g.collectAssignLHSExpr(s.Value, names)
			}
		case *parser.MultiAssignStatement:
			for _, t := range s.Targets {
				if id, ok := t.(*parser.Identifier); ok {
					names[id.Value] = true
				}
			}
			if s.Value != nil {
				g.collectAssignLHSExpr(s.Value, names)
			}
		case *parser.ExpressionStatement:
			g.collectAssignLHSExpr(s.Expression, names)
		case *parser.ForStatement:
			if s.Init != nil {
				g.collectAssignLHS([]parser.Statement{s.Init}, names)
			}
			if s.Update != nil {
				g.collectAssignLHS([]parser.Statement{s.Update}, names)
			}
			if s.Condition != nil {
				g.collectAssignLHSExpr(s.Condition, names)
			}
			if s.IterRange != nil && s.IterRange.Variable != "" {
				names[s.IterRange.Variable] = true
			}
			if s.Body != nil {
				g.collectAssignLHS(s.Body.Statements, names)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				g.collectAssignLHSExpr(s.ReturnValue, names)
			}
		}
	}
}

// collectAssignLHSExpr 收集表達式內嵌套語句塊中的賦值 LHS，並對 AssignExpression
// 的左值一併記錄（表達式形式的賦值）。遞歸結構與 collectNestedStmts 對齊，覆蓋
// if/match/while 表達式內的語句塊（match → IfExpression）。
func (g *Generator) collectAssignLHSExpr(expr parser.Expression, names map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.AssignExpression:
		if id, ok := e.Left.(*parser.Identifier); ok {
			names[id.Value] = true
		}
		if e.Left != nil {
			g.collectAssignLHSExpr(e.Left, names)
		}
		if e.Value != nil {
			g.collectAssignLHSExpr(e.Value, names)
		}
	case *parser.IfExpression:
		if e.Consequence != nil {
			g.collectAssignLHS(e.Consequence.Statements, names)
		}
		if e.Alternative != nil {
			g.collectAssignLHS(e.Alternative.Statements, names)
		}
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			g.collectAssignLHSExpr(e.Condition, names)
		}
		if e.Consequence != nil {
			g.collectAssignLHSExpr(e.Consequence, names)
		}
		if e.Alternative != nil {
			g.collectAssignLHSExpr(e.Alternative, names)
		}
	case *parser.InfixExpression:
		if e.Left != nil {
			g.collectAssignLHSExpr(e.Left, names)
		}
		if e.Right != nil {
			g.collectAssignLHSExpr(e.Right, names)
		}
	case *parser.PrefixExpression:
		if e.Right != nil {
			g.collectAssignLHSExpr(e.Right, names)
		}
	case *parser.CallExpression:
		if e.Function != nil {
			g.collectAssignLHSExpr(e.Function, names)
		}
		for _, arg := range e.Arguments {
			g.collectAssignLHSExpr(arg, names)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			g.collectAssignLHSExpr(e.Expression, names)
		}
	}
}

func (g *Generator) emitHeapFree(sb *strings.Builder) {
	if g.heapVars == nil || len(g.heapVars) == 0 {
		return
	}
	// 排序確保輸出順序確定（別名場景下先 free 誰隨機會增加 double-free 調試難度，
	// 且編譯不可復現）。與 emitGlobalHeapFree 保持一致。
	sortedNames := make([]string, 0, len(g.heapVars))
	for name := range g.heapVars {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		llvmType := g.heapVars[name]
		// 輸出參數本身不 free（由呼叫者管理）
		if g.outputParamNames != nil && g.outputParamNames[name] {
			continue
		}
		elemType := ""
		if g.arrayElemTypes != nil {
			elemType = g.arrayElemTypes[name]
		}
		varIdx, hasIdx := g.heapVarIndex[name]
		if !hasIdx {
			// 無 varIdx（非局部堆變數或未追蹤），直接 free
			g.emitVarHeapFree(sb, g.varAddr(name), llvmType, elemType, name)
			continue
		}
		// 數據流優化：若數據流分析確認變數在所有路徑都已 moved（triMust），
		// 則靜態跳過 free，無需運行時 bitmap 檢查。
		// 對於 triMay/triMustNot，回退到舊邏輯（movedVarBitset 或 bitmap）。
		if g.cfgMovedFacts != nil && g.curCFG != nil {
			reachable := g.computeReachableBlocks()
			allMeet := newBitsetFact(g.nextHeapVarIdx)
			allJoin := newBitsetFact(g.nextHeapVarIdx)
			aMeet := newBitsetFact(g.nextHeapVarIdx)
			aJoin := newBitsetFact(g.nextHeapVarIdx)
			first := true
			aFirst := true
		for _, label := range g.curCFG.Order {
			if !reachable[label] {
				continue
			}
			bf := g.cfgMovedFacts[label]
			if bf == nil {
				continue
			}
			// 只取函数出口块（无后继的 block = return 终止块）的 OUT 作为函数结尾真实状态，
			// 而非对全部 block 取 meet（中间块会把结果稀释成全 0，导致 triMust 恒不触发）。
			if b := g.curCFG.Blocks[label]; b != nil && len(b.Succs) == 0 {
				if first {
					allMeet = bf.outMeet.copy()
					allJoin = bf.outJoin.copy()
					first = false
				} else {
					allMeet = allMeet.meet(bf.outMeet)
					allJoin = allJoin.join(bf.outJoin)
				}
				// 同步聚合 AssignedFact 出口狀態（仅当已求解）
				if g.cfgAssignedFacts != nil {
					if abf := g.cfgAssignedFacts[label]; abf != nil {
						if aFirst {
							aMeet = abf.outMeet.copy()
							aJoin = abf.outJoin.copy()
							aFirst = false
						} else {
							aMeet = aMeet.meet(abf.outMeet)
							aJoin = aJoin.join(abf.outJoin)
						}
					}
				}
			}
		}
			if !first {
				tri := classifyMoved(allMeet, allJoin, varIdx)
				dfStat(tri, len(g.curCFG.Order), len(reachable))
				if tri == triMust {
					// 所有路徑 moved → 靜態跳過 free
					continue
				}
				// Safety net: if the compiler already knows the var is moved
				// (via markMovedVar in handleMoveToOut/handleMoveLocal), trust
				// that over the CFG dataflow result. The CFG may be incomplete
				// because internal codegen paths (vec.push, etc.) create basic
				// blocks without registering CFG edges, making reachable blocks
				// miss the block where the move effect was recorded. This causes
				// the dataflow solver to incorrectly conclude triMustNot for a
				// variable that is actually moved, leading to double-free.
				if tri == triMustNot && g.isMovedVar(varIdx) {
					continue
				}
				// AssignedFact 優化：僅當「肯定從不 moved 且 肯定持有堆」時直接 free，
				// 繞開運行時 NULL 守衛（@free(NULL) 為 안전 no-op，即便誤判亦無害）。
				// 與 moved 正交：moved=triMay 時不優化（交給 bitmap/NULL 守衛處理，防雙重釋放）。
				if tri == triMustNot && !aFirst {
					aTri := classifyMoved(aMeet, aJoin, varIdx)
				if aTri == triMust {
					g.emitVarHeapFreeDirect(sb, g.varAddr(name), llvmType, elemType, name)
					continue
				}
				// Phase 2：變量在所有路徑都未持有堆數據（data 恆為 memset 的 NULL）
				// → 整段 free 都不發。安全依據：入口 seed 已確保所有「被寫過（LHS）
				// / vec·arr」的局部恆為 assigned=triMust，絕不落入此分支；能落入的
				// 只有「從不寫、又非 vec·arr」的局部，其 heap data 指針恆為 NULL，
				// free(NULL) 本就是 no-op，跳過無洩漏、無崩潰。
				if aTri == triMustNot {
					continue
				}
				}
			} else {
				dfStatNoBlock()
			}
		}
		// 回退到舊邏輯
		if g.hasBranchMove && g.movedBitmapBase != "" {
			// 有運行時 bitmap：生成 IR 檢查 bit
			g.emitBitCheckFree(sb, name, varIdx, llvmType, elemType)
		} else {
			// 無 bitmap：編譯期檢查 movedVarBitset
			if g.isMovedVar(varIdx) {
				continue // 跳過 free（所有權轉移）
			}
			g.emitVarHeapFree(sb, g.varAddr(name), llvmType, elemType, name)
		}
	}
}

// trackLocalTask 记录函数内通过 `task = run ...` 创建的本地 task 变量名，
// 用于函数返回前清理未 awy 的 task（SubTask 2.3）。
// 仅在非 coro 上下文追踪（coro 上下文的 task 生命周期由状态机管理）。
func (g *Generator) trackLocalTask(varName string) {
	if g.coroInAsyncFunc {
		return
	}
	g.localTasks = append(g.localTasks, varName)
}

// untrackLocalTask 将已 awy 的 task 变量从 localTasks 移除，
// 避免函数返回前重复 free（SubTask 2.2 已在 awaitTaskVar 中 free）。
func (g *Generator) untrackLocalTask(varName string) {
	for i, name := range g.localTasks {
		if name == varName {
			g.localTasks = append(g.localTasks[:i], g.localTasks[i+1:]...)
			return
		}
	}
}

// emitLocalTasksFree 在函数返回前清理未 awy 的本地 task（SubTask 2.3）。
// 对每个未 awy 的 task：
//   - 检查 done 标志（field 2）
//   - 若 done=false：同步调用 resume_fn(task) 驱动 task 到完成（与 awaitTaskVar
//     not_done 路径一致），resume_fn 执行完毕后设置 done=true。
//   - 释放 args struct 和 result buffer 容器（仅容器，不释放 data）。
//   - 不释放 task 结构体本身：task 通过 @nolang_async_enqueue 入队后，
//     就绪队列 @nolang_ready_q 仍持有 task 指针。释放 task 会导致事件循环
//     调度到该 task 时读取已释放内存（UAF）。task 结构体（24 字节）作为
//     已知泄漏保留——事件循环取出 task 后，wrapper 检查 done=true 直接返回，
//     done_handler 调用 nolang_async_done（无等待者时直接返回），安全跳过。
//
// 释放的容器：
//   - result buffer：data 由调用端结果变量接管（经 trackLocalHeapVar 追踪）
//   - args struct：参数容器（cloneBuf 等）由 wrapper 在目标函数执行后释放，
//     此处仅释放 args struct 容器本身（{ i8*, ... } 结构体）
func (g *Generator) emitLocalTasksFree(sb *strings.Builder) {
	if len(g.localTasks) == 0 {
		return
	}
	for _, varName := range g.localTasks {
		// task 句柄为 i8*（run 返回不透明指针），bitcast 到 %task* 供 GEP 访问。
		taskVarAddr := g.varAddr(varName)
		taskPtrCast := g.tmpReg("ltask.cast")
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), taskPtrCast, taskVarAddr))
		taskPtr := g.tmpReg("ltask.ptr")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %%task*\n", g.indent(), taskPtr, taskPtrCast))

		// 检查 done (field 2)
		doneGEP := g.tmpReg("ltask.dgep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", g.indent(), doneGEP, taskPtr))
		doneVal := g.tmpReg("ltask.dval")
		sb.WriteString(fmt.Sprintf("%s%s = load i1, i1* %s\n", g.indent(), doneVal, doneGEP))

		// 标签名使用不同前缀避免与寄存器名冲突（LLVM 寄存器和标签共享命名空间）
		doneLabel := fmt.Sprintf("ltask.dlbl.%d", g.tmpIdx)
		notDoneLabel := fmt.Sprintf("ltask.ndlbl.%d", g.tmpIdx)
		freeLabel := fmt.Sprintf("ltask.flbl.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), doneVal, doneLabel, notDoneLabel))

		// not_done: 同步调用 resume_fn(task) 驱动到完成（与 awaitTaskVar 一致）
		sb.WriteString(notDoneLabel + ":\n")
		// load resume_fn (field 0)
		fnGEP := g.tmpReg("ltask.fngep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", g.indent(), fnGEP, taskPtr))
		fnVal := g.tmpReg("ltask.fn")
		sb.WriteString(fmt.Sprintf("%s%s = load void (i8*)*, void (i8*)** %s\n", g.indent(), fnVal, fnGEP))
		// bitcast task* to i8*
		resumeTaskI8 := g.tmpReg("ltask.resume.i8")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), resumeTaskI8, taskPtr))
		// call resume_fn(task) — 同步驱动 task 到完成
		sb.WriteString(fmt.Sprintf("%scall void %s(i8* %s)\n", g.indent(), fnVal, resumeTaskI8))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), doneLabel))

		// done: free args/result 容器（不 free task 本身，避免就绪队列 UAF）
		sb.WriteString(doneLabel + ":\n")
		// 加载 args_ptr (field 1)
		argsGEP := g.tmpReg("ltask.agep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", g.indent(), argsGEP, taskPtr))
		argsPtr := g.loadDataPtrField(sb, argsGEP)

		// 加载 result_ptr (args field 0)
		argsTyped := g.tmpReg("ltask.atyped")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to { i8* }*\n", g.indent(), argsTyped, argsPtr))
		resultPtrGEP := g.tmpReg("ltask.rpgep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", g.indent(), resultPtrGEP, argsTyped))
		resultPtr := g.tmpReg("ltask.rptr")
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), resultPtr, resultPtrGEP))

		// free result buffer (仅容器)
		sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), resultPtr))

		// free args struct (仅容器)
		sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), argsPtr))

		// 注意：不 free task 结构体本身。
		// task 通过 @nolang_async_enqueue 入队后，就绪队列仍持有 task 指针。
		// 释放 task 会导致事件循环调度到该 task 时 UAF。
		// task（24 字节）作为已知泄漏保留，事件循环取出后 wrapper 检查 done=true
		// 直接返回，安全跳过。

		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), freeLabel))

		// free: 继续
		sb.WriteString(freeLabel + ":\n")
	}
}

// emitGlobalHeapFree 釋放模組級堆變數（LLVM globals，如 @vec、@str）。
// 這些變數由 generateMainFunction 的 top-level 語句初始化（malloc data），
// 但不在 heapVars 中（因 trackLocalHeapVar 跳過 globalVars），
// 需單獨釋放以避免長期運行服務的記憶體泄漏。
func (g *Generator) emitGlobalHeapFree(sb *strings.Builder) {
	if g.globalVars == nil || g.moduleVarTypes == nil {
		return
	}
	// 排序確保輸出順序確定
	sortedNames := make([]string, 0, len(g.moduleVarTypes))
	for name := range g.moduleVarTypes {
		if g.globalVars[name] {
			sortedNames = append(sortedNames, name)
		}
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		llvmType := g.moduleVarTypes[name]
		var elemType string
		if g.moduleArrayElemTypes != nil {
			elemType = g.moduleArrayElemTypes[name]
		}
		// 跳過 embed 變量（只讀常量數據，不參與堆釋放）
		if g.embedVars != nil && g.embedVars[name] {
			continue
		}
		// 只釋放堆擁有型別（%vec/%str-long/%arr/%option/用戶結構體）
		// %option 需進一步檢查 inner type 是否為堆型別（在 emitOptionHeapFree 內處理）
		if llvmType != "%vec" && llvmType != "%str-long" && llvmType != "%arr" &&
			llvmType != "%option" && !g.isUserStructType(llvmType) {
			continue
		}
		g.emitVarHeapFree(sb, llvmGlobalRef(name), llvmType, elemType, name)
	}
}

// fieldHeapKind 描述一個 LLVM 型別的堆數據分類，供 free/clone 共用分派。
type fieldHeapKind int

const (
	fieldHeapNone       fieldHeapKind = iota // 純量型別，不持有堆數據
	fieldHeapContainer                       // %vec/%str-long/%arr：含 data 指標欄位
	fieldHeapUserStruct                      // 用戶結構體：需遞迴釋放/clone 欄位
	fieldHeapOption                          // %option：含 boxed heap 指標
)

// fieldHeapInfo 為 free/clone 提供統一的欄位堆佈局描述。
// dataFieldIdx 僅對 fieldHeapContainer 有效（%vec/%str-long=2, %arr=1）。
// containerType 保存原始 LLVM 型別字串，供後續 emitShallowDataFree/emitContainerClone 使用。
type fieldHeapInfo struct {
	kind          fieldHeapKind
	dataFieldIdx  int    // container 的 data 欄位索引
	containerType string // 原始 LLVM 型別字串（如 "%vec"、"%user.foo"）
}

// classifyFieldHeap 根據 LLVM 型別字串返回其堆分類資訊。
// 此函數為 free/clone 的單一真實來源（single source of truth）：
// 新增 struct 欄位型別只需修改此處，無需同步改動 emitVarHeapFree/emitStructFieldsFree/emitStructClone/emitElementFree/emitDeepElementClone/emitDeepClone。
// 底層透過 classifyTypeKind（TypeKind 枚舉）進行型別分類，消除字串前綴判斷。
func (g *Generator) classifyFieldHeap(llvmType, elemType string) fieldHeapInfo {
	desc := g.classifyTypeKind(llvmType)
	switch desc.Kind {
	case KindVec, KindStr:
		return fieldHeapInfo{kind: fieldHeapContainer, dataFieldIdx: 2, containerType: llvmType}
	case KindArr:
		return fieldHeapInfo{kind: fieldHeapContainer, dataFieldIdx: 1, containerType: llvmType}
	case KindOption:
		return fieldHeapInfo{kind: fieldHeapOption, containerType: llvmType}
	case KindUserStruct:
		return fieldHeapInfo{kind: fieldHeapUserStruct, containerType: llvmType}
	}
	return fieldHeapInfo{kind: fieldHeapNone}
}

// emitVarHeapFree 釋放單一變數的堆數據。
// 透過 classifyFieldHeap 統一分派，避免與 clone 路徑重複維護型別 switch。
// name 為變數名，用於查詢 optionInnerTypes（option 釋放時需要）；
// 遞迴場景（釋放 option 內部結構）可傳 ""，此時 %option 分支會走兜底路徑。
// emitVarHeapFreeViaLocalCopy 先將變數當前值拷貝進一個局部 alloca，再釋放局部副本。
// 用途：變數重賦值前的「釋放舊值」。若直接從變數位址 load 再釋放，opt -O3 會把後續
// 賦值的 store 經 SROA/GVN 前向傳播到釋放點的 load，並據此消除釋放迴圈的 NULL 檢查，
// 導致以 len!=0 但 data=NULL（或尚未寫入元素）的狀態解引用而崩潰。
// 改從獨立的局部 alloca 釋放，opt 無法把變數的賦值 store 與 alloca 的初始 load 等同，
// 從而保全「釋放的是賦值前的舊值」語意（含 NULL 檢查）。
func (g *Generator) emitVarHeapFreeViaLocalCopy(sb *strings.Builder, varPtr, llvmType, elemType, name string) {
	oldVal := g.tmpReg("freeold.val")
	sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), oldVal, llvmType, llvmType, varPtr))
	oldLocal := g.tmpReg("freeold")
	sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), oldLocal, llvmType))
	sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), llvmType, oldVal, llvmType, oldLocal))
	g.emitVarHeapFree(sb, oldLocal, llvmType, elemType, name)
}

func (g *Generator) emitVarHeapFree(sb *strings.Builder, varPtr, llvmType, elemType, name string) {
	info := g.classifyFieldHeap(llvmType, elemType)
	switch info.kind {
	case fieldHeapOption:
		g.emitOptionHeapFree(sb, varPtr, name)
		return
	case fieldHeapUserStruct:
		g.emitStructFieldsFree(sb, varPtr, info.containerType)
		return
	case fieldHeapNone:
		return
	}
	// fieldHeapContainer：%str-long 淺釋放 data；%vec/%arr 視 elemType 決定深淺
	if llvmType == "%str-long" {
		g.emitShallowDataFree(sb, varPtr, llvmType, info.dataFieldIdx)
		return
	}
	if g.isHeapOwningType(elemType) {
		// 嵌套容器（[][]T）：查詢內層元素型別，傳遞給 emitDeepContainerFree 做遞迴釋放
		innerType := ""
		if g.elemElemTypes != nil && name != "" {
			innerType = g.elemElemTypes[name]
		}
		if innerType != "" {
			g.emitDeepContainerFree(sb, varPtr, llvmType, info.dataFieldIdx, elemType, innerType)
		} else {
			g.emitDeepContainerFree(sb, varPtr, llvmType, info.dataFieldIdx, elemType)
		}
		return
	}
	g.emitShallowDataFree(sb, varPtr, llvmType, info.dataFieldIdx)
}

// emitVarHeapFreeDirect 与 emitVarHeapFree 類似，但對淺容器（%str-long /
// 非堆元素 %vec/%arr）跳過運行時 NULL 守衛，直接 call @free。
// 僅在數據流確認「變量肯定持有堆」(triMust) 時使用：data 必為非 NULL；
// 即便誤判為 triMust（實際 data=NULL，如賦值 nil），@free(NULL) 亦為安全 no-op。
// 深容器（堆元素 %vec/%arr）仍走 emitDeepContainerFree 保留長度/data 守衛。
func (g *Generator) emitVarHeapFreeDirect(sb *strings.Builder, varPtr, llvmType, elemType, name string) {
	info := g.classifyFieldHeap(llvmType, elemType)
	switch info.kind {
	case fieldHeapOption:
		g.emitOptionHeapFree(sb, varPtr, name)
		return
	case fieldHeapUserStruct:
		g.emitStructFieldsFree(sb, varPtr, info.containerType)
		return
	case fieldHeapNone:
		return
	}
	// fieldHeapContainer
	if llvmType == "%str-long" {
		g.emitShallowDataFreeDirect(sb, varPtr, llvmType, info.dataFieldIdx)
		return
	}
	if g.isHeapOwningType(elemType) {
		// 深容器保留守衛（長度=0 / data=NULL 的未初始化語義較複雜，不在本優化範圍）
		// 嵌套容器（[][]T）：查詢內層元素型別，傳遞給 emitDeepContainerFree 做遞迴釋放
		innerType := ""
		if g.elemElemTypes != nil && name != "" {
			innerType = g.elemElemTypes[name]
		}
		if innerType != "" {
			g.emitDeepContainerFree(sb, varPtr, llvmType, info.dataFieldIdx, elemType, innerType)
		} else {
			g.emitDeepContainerFree(sb, varPtr, llvmType, info.dataFieldIdx, elemType)
		}
		return
	}
	g.emitShallowDataFreeDirect(sb, varPtr, llvmType, info.dataFieldIdx)
}

// emitShallowDataFreeDirect 載入 data 指針並直接 call @free，不發出 NULL 守衛分支。
func (g *Generator) emitShallowDataFreeDirect(sb *strings.Builder, containerPtr, containerType string, dataFieldIdx int) {
	dataGEP := g.tmpReg("heapfree.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, containerType, containerType, containerPtr, dataFieldIdx))
	dataLoad := g.loadDataPtrField(sb, dataGEP)
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), dataLoad))
}

// emitOptionHeapFree 釋放 %option 變數持有的堆 box。
// 僅當 inner type 為堆型別（%str-long/%vec/%arr/用戶結構體）且 tag != nil 時才釋放：
//  1. 從 optionInnerTypes[name] 取得 inner type；若無記錄或為純量，直接返回（安全但洩漏）
//  2. load tag (field 0)；若 == 1 (nil)，data 是 0，跳過（避免 inttoptr i64 0）
//  3. load data (field 1) 為 i64，inttoptr 回 i8*
//  4. NULL check（data 可能為 0 即使 tag != 1）
//  5. 若 inner 為堆型別：bitcast 到 inner*，呼叫 emitVarHeapFree 遞迴釋放 inner 的 data
//     （inner 是 %str-long → 釋放字串 data；是 %vec/%arr → 釋放容器 data；
//     是用戶結構體 → emitStructFieldsFree 遞迴釋放欄位）
//  6. call @free(i8* boxPtr) 釋放 box 本身
//
// 順序：先釋放 inner 的 data，再 free box，避免懸垂。
func (g *Generator) emitOptionHeapFree(sb *strings.Builder, optPtr, name string) {
	innerType := ""
	if g.optionInnerTypes != nil {
		innerType = g.optionInnerTypes[name]
	}
	// 全域 option 變數的 inner type 不在函數級 optionInnerTypes 中，
	// 嘗試從模組級備份讀取（emitGlobalHeapFree 場景）
	if innerType == "" && g.moduleOptionInnerTypes != nil {
		innerType = g.moduleOptionInnerTypes[name]
	}
	if innerType == "" || !g.isHeapOwningType(innerType) {
		return // 純量 option 或無 inner 資訊：不持有堆，跳過
	}

	g.tmpIdx++
	tid := g.tmpIdx

	// 1. load tag (field 0)
	tagGEP := fmt.Sprintf("%%optfree.tag.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
		g.indent(), tagGEP, optPtr))
	tagReg := fmt.Sprintf("%%optfree.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), tagReg, tagGEP))

	// 2. icmp eq i64 tag, 1 → skip（tag==1 表示 nil，data 為 0）
	nilCmp := fmt.Sprintf("%%optfree.nil.%d", tid)
	skipLabel := fmt.Sprintf("optfree.skip.%d", tid)
	dataLabel := fmt.Sprintf("optfree.data.%d", tid)
	fromBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 1\n", g.indent(), nilCmp, tagReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nilCmp, skipLabel, dataLabel))
	g.cfgTerm(fromBlock, termCondBr)

	// 3. 非 nil：load data (field 1) i64, inttoptr to i8*
	g.emitLabel(sb, dataLabel)
	g.cfgEdge(fromBlock, skipLabel)
	g.cfgEdge(fromBlock, dataLabel)
	dataGEP := fmt.Sprintf("%%optfree.data.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
		g.indent(), dataGEP, optPtr))
	dataIntReg := fmt.Sprintf("%%optfree.data.i64.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataIntReg, dataGEP))
	boxPtrReg := fmt.Sprintf("%%optfree.box.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), boxPtrReg, dataIntReg))

	// 4. NULL check
	nullCmp := fmt.Sprintf("%%optfree.null.%d", tid)
	freeLabel := fmt.Sprintf("optfree.free.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, boxPtrReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, freeLabel))
	g.cfgTerm(dataLabel, termCondBr)

	// 5. 非 NULL：先遞迴釋放 inner 的 data
	g.emitLabel(sb, freeLabel)
	g.cfgEdge(dataLabel, skipLabel)
	g.cfgEdge(dataLabel, freeLabel)
	innerPtrReg := fmt.Sprintf("%%optfree.inner.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), innerPtrReg, boxPtrReg, innerType))
	// 遞迴釋放 inner 的 data（不傳 name，inner 不是 option 變數本身）
	// inner 是 %str-long → 釋放字串 data；%vec/%arr → 釋放容器 data；用戶結構體 → 遞迴釋放欄位
	g.emitVarHeapFree(sb, innerPtrReg, innerType, "", "")

	// 6. 釋放 box 本身
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), boxPtrReg))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.cfgTerm(freeLabel, termBr)

	g.emitLabel(sb, skipLabel)
	g.cfgEdge(freeLabel, skipLabel)
}

// emitOptionDeepClone 深層 clone option 變數 b = a：
//   - malloc 新 box 並遞迴 clone inner 的堆數據，使 a/b 各自獨立擁有 box
//   - 與 vec/struct 的 b = a 一致，避免 a/b 共享 box 導致 double-free
//
// 流程：
//  1. load tag from src；store tag to dst
//  2. 若 tag == 1 (nil)：dst.data = 0，return
//  3. 若 inner 為堆型別：load src.data (i64) → inttoptr to innerType*；
//     malloc 新 box（inner struct size）→ bitcast to innerType*；
//     呼叫 emitDeepClone(srcInnerPtr, dstInnerPtr, innerType, "")；
//     ptrtoint 新 box to i64 → store to dst.data
//  4. 若 inner 為純量：直接 load src.data i64 → store to dst.data
//
// 若 inner type 未知（optionInnerTypes 缺失），退化為淺拷貝（共享 box）作為兜底。
func (g *Generator) emitOptionDeepClone(sb *strings.Builder, srcName, dstName string) {
	innerType := ""
	if g.optionInnerTypes != nil {
		innerType = g.optionInnerTypes[srcName]
	}
	// 也嘗試從 dst 取（option-to-option 推導可能寫到 dst）
	if innerType == "" && g.optionInnerTypes != nil {
		innerType = g.optionInnerTypes[dstName]
	}

	g.tmpIdx++
	tid := g.tmpIdx

	// 1. load tag from src
	srcTagGEP := fmt.Sprintf("%%optclone.src.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
		g.indent(), srcTagGEP, llvmVarRef(srcName)))
	srcTagReg := fmt.Sprintf("%%optclone.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcTagReg, srcTagGEP))

	// store tag to dst
	dstTagGEP := fmt.Sprintf("%%optclone.dst.tag.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
		g.indent(), dstTagGEP, llvmVarRef(dstName)))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcTagReg, dstTagGEP))

	// 2. 若 tag == 1 (nil)：dst.data = 0，return
	nilCmp := fmt.Sprintf("%%optclone.nil.%d", tid)
	skipLabel := fmt.Sprintf("optclone.skip.%d", tid)
	cloneLabel := fmt.Sprintf("optclone.do.%d", tid)
	// CFG: record block before conditional branch
	optFromBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 1\n", g.indent(), nilCmp, srcTagReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nilCmp, skipLabel, cloneLabel))
	// CFG: conditional branch → skipLabel / cloneLabel
	g.cfgTerm(optFromBlock, termCondBr)
	g.cfgEdge(optFromBlock, skipLabel)
	g.cfgEdge(optFromBlock, cloneLabel)

	g.emitLabel(sb, cloneLabel)

	// 3. 非 nil：根據 inner 決定 clone 策略
	if innerType == "" {
		// 兜底：無 inner 資訊，退化為淺拷貝 data（共享 box）
		// 此分支不應常見，僅作防禦
		srcDataGEP := fmt.Sprintf("%%optclone.src.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), srcDataGEP, llvmVarRef(srcName)))
		srcDataReg := fmt.Sprintf("%%optclone.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcDataReg, srcDataGEP))
		dstDataGEP := fmt.Sprintf("%%optclone.dst.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), dstDataGEP, llvmVarRef(dstName)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcDataReg, dstDataGEP))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		// CFG: cloneLabel → skipLabel (unconditional branch)
		g.cfgTerm(cloneLabel, termBr)
		g.cfgEdge(cloneLabel, skipLabel)
		g.emitLabel(sb, skipLabel)
		return
	}

	if g.isHeapOwningType(innerType) {
		// inner 為堆型別：malloc 新 box + emitDeepClone
		// load src.data i64
		srcDataGEP := fmt.Sprintf("%%optclone.src.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), srcDataGEP, llvmVarRef(srcName)))
		srcDataIntReg := fmt.Sprintf("%%optclone.src.i64.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcDataIntReg, srcDataGEP))
		// inttoptr to innerType*
		srcInnerPtrReg := fmt.Sprintf("%%optclone.src.inner.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to %s*\n", g.indent(), srcInnerPtrReg, srcDataIntReg, innerType))

		// malloc 新 box（inner struct size）
		structSize := g.llvmTypeSize(innerType)
		if structSize == 0 {
			structSize = 8
		}
		newBoxI8Reg := fmt.Sprintf("%%optclone.newbox.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), newBoxI8Reg, structSize))
		// bitcast to innerType*
		newInnerPtrReg := fmt.Sprintf("%%optclone.dst.inner.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), newInnerPtrReg, newBoxI8Reg, innerType))

		// NULL check src inner ptr（src 可能是 nil option 但 tag 非 1 的邊界情況）
		srcNullCmp := fmt.Sprintf("%%optclone.srcnull.%d", tid)
		deepCloneLabel := fmt.Sprintf("optclone.deep.%d", tid)
		// CFG: cloneLabel is the current block for this conditional branch
		deepFromBlock := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq %s* %s, null\n", g.indent(), srcNullCmp, innerType, srcInnerPtrReg))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), srcNullCmp, skipLabel, deepCloneLabel))
		// CFG: conditional branch → skipLabel / deepCloneLabel
		g.cfgTerm(deepFromBlock, termCondBr)
		g.cfgEdge(deepFromBlock, skipLabel)
		g.cfgEdge(deepFromBlock, deepCloneLabel)

		// 深層 clone inner
		g.emitLabel(sb, deepCloneLabel)
		g.emitDeepClone(sb, srcInnerPtrReg, newInnerPtrReg, innerType, "")

		// ptrtoint 新 box to i64, store to dst.data
		newBoxIntReg := fmt.Sprintf("%%optclone.newbox.i64.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), newBoxIntReg, newBoxI8Reg))
		dstDataGEP := fmt.Sprintf("%%optclone.dst.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), dstDataGEP, llvmVarRef(dstName)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), newBoxIntReg, dstDataGEP))
	} else {
		// inner 為純量：直接 copy i64 data
		srcDataGEP := fmt.Sprintf("%%optclone.src.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), srcDataGEP, llvmVarRef(srcName)))
		srcDataReg := fmt.Sprintf("%%optclone.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcDataReg, srcDataGEP))
		dstDataGEP := fmt.Sprintf("%%optclone.dst.data.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n",
			g.indent(), dstDataGEP, llvmVarRef(dstName)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcDataReg, dstDataGEP))
	}

	// CFG: current block (deepCloneLabel or cloneLabel) → skipLabel
	deepEndFromBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.cfgTerm(deepEndFromBlock, termBr)
	g.cfgEdge(deepEndFromBlock, skipLabel)
	g.emitLabel(sb, skipLabel)
}

// outputParamBitIndex 返回 out 參數在 outputParamOrder 中的索引（即位圖 bit index）。
// 若不是 out 參數，返回 -1。
func (g *Generator) outputParamBitIndex(outName string) int {
	for i, n := range g.outputParamOrder {
		if n == outName {
			return i
		}
	}
	return -1
}

// detectBranchMoveToOut 預掃描函數體語句，檢測是否存在「分支內 move 到 out 參數」。
// move 定義：LetStatement 將局部變數（Identifier）賦值給 out 參數。
// 分支定義：IfExpression（Consequence/Alternative）、ForStatement（Body）、ConditionalExpression。
// 僅當此類模式存在時返回 true，用於決定是否分配函數級 u64 位圖變數。
//   - 無 move → 返回 false → 不分配位圖，全部 free
//   - move 不在分支 → 返回 false → 不分配位圖，編譯期 movedVarBitset 確定性跳過 free
//   - move 在分支 → 返回 true → 分配位圖，運行時雙重校驗
func detectBranchMoveToOut(stmts []parser.Statement, outNames map[string]bool) bool {
	found := false
	var scanExpr func(expr parser.Expression, inBranch bool)
	var scanStmts func(stmts []parser.Statement, inBranch bool)
	scanStmts = func(ss []parser.Statement, inBranch bool) {
		for _, st := range ss {
			if found {
				return
			}
			switch s := st.(type) {
			case *parser.LetStatement:
				if inBranch && s.Name != nil && outNames[s.Name.Value] {
					if _, ok := s.Value.(*parser.Identifier); ok {
						found = true
						return
					}
				}
				if s.Value != nil {
					scanExpr(s.Value, inBranch)
				}
			case *parser.ExpressionStatement:
				if s.Expression != nil {
					scanExpr(s.Expression, inBranch)
				}
			case *parser.ForStatement:
				if s.Init != nil {
					scanStmts([]parser.Statement{s.Init}, inBranch)
				}
				if s.Condition != nil {
					scanExpr(s.Condition, inBranch)
				}
				if s.Update != nil {
					scanStmts([]parser.Statement{s.Update}, inBranch)
				}
				if s.Body != nil {
					scanStmts(s.Body.Statements, true)
				}
				if s.IterRange != nil {
					scanExpr(s.IterRange, inBranch)
				}
				if s.CountExpr != nil {
					scanExpr(s.CountExpr, inBranch)
				}
			case *parser.BlockStatement:
				scanStmts(s.Statements, inBranch)
		case *parser.ReturnStatement:
			// ReturnValue 始终为 nil（Nolang 禁止 return <值>），无需扫描
		case *parser.MultiAssignStatement:
				for _, t := range s.Targets {
					scanExpr(t, inBranch)
				}
				if s.Value != nil {
					scanExpr(s.Value, inBranch)
				}
			}
		}
	}
	scanExpr = func(expr parser.Expression, inBranch bool) {
		if expr == nil || found {
			return
		}
		switch e := expr.(type) {
		case *parser.IfExpression:
			if e.Condition != nil {
				scanExpr(e.Condition, inBranch)
			}
			if e.Consequence != nil {
				scanStmts(e.Consequence.Statements, true)
			}
			if e.Alternative != nil {
				scanStmts(e.Alternative.Statements, true)
			}
		case *parser.ConditionalExpression:
			if e.Condition != nil {
				scanExpr(e.Condition, inBranch)
			}
			if e.Consequence != nil {
				scanExpr(e.Consequence, true)
			}
			if e.Alternative != nil {
				scanExpr(e.Alternative, true)
			}
		case *parser.InfixExpression:
			scanExpr(e.Left, inBranch)
			scanExpr(e.Right, inBranch)
		case *parser.PrefixExpression:
			scanExpr(e.Right, inBranch)
		case *parser.CallExpression:
			scanExpr(e.Function, inBranch)
			for _, a := range e.Arguments {
				scanExpr(a, inBranch)
			}
		case *parser.GroupedExpression:
			scanExpr(e.Expression, inBranch)
		case *parser.IndexExpression:
			scanExpr(e.Left, inBranch)
			if e.Index != nil {
				scanExpr(e.Index, inBranch)
			}
		case *parser.DotExpression:
			scanExpr(e.Receiver, inBranch)
		case *parser.SliceExpression:
			scanExpr(e.Left, inBranch)
			if e.Range != nil {
				scanExpr(e.Range.Start, inBranch)
				scanExpr(e.Range.End, inBranch)
			}
		case *parser.AssignExpression:
			scanExpr(e.Left, inBranch)
			scanExpr(e.Value, inBranch)
		case *parser.CastExpression:
			scanExpr(e.Expr, inBranch)
		case *parser.RunExpression:
			scanExpr(e.Call, inBranch)
		case *parser.AwaitExpression:
			scanExpr(e.Right, inBranch)
		case *parser.IterationExpr:
			if e.RangeExpr != nil {
				scanExpr(e.RangeExpr, inBranch)
			}
			if e.Range != nil {
				scanExpr(e.Range.Start, inBranch)
				scanExpr(e.Range.End, inBranch)
			}
		}
	}
	scanStmts(stmts, false)
	return found
}

// ---- 編譯期位圖操作（無運行時 bitmap 時用）----

// markMovedVar 編譯期標記堆變數為 moved（無 bitmap 時用）。
// 所有權已轉移，函數結束時跳過 free。
func (g *Generator) markMovedVar(varIdx int) {
	block := varIdx / 64
	offset := uint(varIdx % 64)
	for len(g.movedVarBitset) <= block {
		g.movedVarBitset = append(g.movedVarBitset, 0)
	}
	g.movedVarBitset[block] |= 1 << offset
}

// unmarkMovedVar 編譯期清除堆變數 moved 標記（覆蓋清舊）。
// out 參數重新綁定到別的變數時，舊變數恢復所有權，函數結束時需 free。
func (g *Generator) unmarkMovedVar(varIdx int) {
	block := varIdx / 64
	offset := uint(varIdx % 64)
	if block < len(g.movedVarBitset) {
		g.movedVarBitset[block] &^= 1 << offset
	}
}

// isMovedVar 編譯期檢查堆變數是否 moved（無 bitmap 時用）。
func (g *Generator) isMovedVar(varIdx int) bool {
	block := varIdx / 64
	offset := uint(varIdx % 64)
	if block >= len(g.movedVarBitset) {
		return false
	}
	return g.movedVarBitset[block]&(1<<offset) != 0
}

// ---- 運行時 bitmap IR 生成（有 bitmap 時用）----

// emitSetMovedBitIR 生成 IR：設置堆變數對應的 bitmap bit=1（表示 move 已發生）。
// IR: %old = load bitmap[block]; %mask = or %old, (1<<offset); store %mask, bitmap[block]
func (g *Generator) emitSetMovedBitIR(sb *strings.Builder, varIdx int) {
	if g.movedBitmapBase == "" {
		return
	}
	block := varIdx / 64
	offset := varIdx % 64
	bvName := fmt.Sprintf("%s%d", g.movedBitmapBase, block)
	oldVal := g.tmpReg("mb.old")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldVal, bvName))
	maskVal := g.tmpReg("mb.mask")
	sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %d\n", g.indent(), maskVal, oldVal, 1<<offset))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), maskVal, bvName))
}

// emitClearMovedBitIR 生成 IR：清除堆變數對應的 bitmap bit=0（覆蓋清舊）。
// IR: %old = load bitmap[block]; %mask = and %old, ~(1<<offset); store %mask, bitmap[block]
func (g *Generator) emitClearMovedBitIR(sb *strings.Builder, varIdx int) {
	if g.movedBitmapBase == "" {
		return
	}
	block := varIdx / 64
	offset := varIdx % 64
	bvName := fmt.Sprintf("%s%d", g.movedBitmapBase, block)
	oldVal := g.tmpReg("mb.clr")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldVal, bvName))
	maskVal := g.tmpReg("mb.clrm")
	sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %d\n", g.indent(), maskVal, oldVal, ^(1 << offset)))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), maskVal, bvName))
}

// emitSetRetInitBit 生成 IR：標記 out 參數已顯式賦值（設置 %__ret_init_bitmap 對應 bit=1）。
// 與 emitSetMovedBitIR 對稱，但 retInitBitmapVar 為單一 i64（out 參數 ≤ 64）。
// IR: %ri.old = load i64, i64* %__ret_init_bitmap; %ri.mask = or %ri.old, (1<<bitIdx); store %ri.mask
func (g *Generator) emitSetRetInitBit(sb *strings.Builder, outName string) {
	if g.retInitBitmapVar == "" {
		return
	}
	bitIdx, ok := g.retInitBits[outName]
	if !ok {
		return
	}
	oldVal := g.tmpReg("ri.old")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldVal, g.retInitBitmapVar))
	maskVal := g.tmpReg("ri.mask")
	sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %d\n", g.indent(), maskVal, oldVal, 1<<bitIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), maskVal, g.retInitBitmapVar))
}

// emitRetInitZeroFill 在 ReturnStatement 路徑（flushOutputBindings 之前）對所有
// 未被顯式賦值的 out 參數補發零值 store。
// 與 emitBitCheckFree 對稱：bit=0 → 未賦值，需補零；bit=1 → 已賦值，跳過。
// 按宣告順序（outputParamOrder）處理每個 out 參數，避免依賴 map 迭代順序。
// 每個 out 參數生成：load bitmap → and mask → icmp eq 0 → br → 補零區塊 / 跳過區塊。
// 補零值依型別選擇（emitRetInitZeroStore）：option 補 nil（tag=1, data=0）、
// 整數補 0、浮點補 0.0、struct（%str-long/%vec/%arr/用戶結構）補 zeroinitializer。
func (g *Generator) emitRetInitZeroFill(sb *strings.Builder) {
	if g.retInitBitmapVar == "" || len(g.retInitBits) == 0 {
		return
	}
	for _, name := range g.outputParamOrder {
		bitIdx, ok := g.retInitBits[name]
		if !ok {
			continue
		}
		llvmType, hasType := g.varTypes[name]
		if !hasType {
			llvmType = "i64"
		}
		// 載入 bitmap 並檢查對應 bit
		bv := g.tmpReg("ri.zf.bv")
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), bv, g.retInitBitmapVar))
		masked := g.tmpReg("ri.zf.masked")
		sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %d\n", g.indent(), masked, bv, 1<<bitIdx))
		unassigned := g.tmpReg("ri.zf.unassigned")
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), unassigned, masked))
		g.tmpIdx++
		zfLabel := fmt.Sprintf("ri.zf.fill.%d", g.tmpIdx)
		g.tmpIdx++
		skipLabel := fmt.Sprintf("ri.zf.skip.%d", g.tmpIdx)
		// CFG: record block before conditional branch
		zfFromBlock := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n",
			g.indent(), unassigned, zfLabel, skipLabel))
		// CFG: conditional branch → zfLabel / skipLabel
		g.cfgTerm(zfFromBlock, termCondBr)
		g.cfgEdge(zfFromBlock, zfLabel)
		g.cfgEdge(zfFromBlock, skipLabel)
		// 補零區塊：bit=0（未賦值），補發零值 store
		g.emitLabel(sb, zfLabel)
		g.emitRetInitZeroStore(sb, name, llvmType)
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		// CFG: zfLabel → skipLabel
		g.cfgTerm(zfLabel, termBr)
		g.cfgEdge(zfLabel, skipLabel)
		// 跳過區塊：bit=1（已賦值），直接繼續
		g.emitLabel(sb, skipLabel)
	}
}

// emitRetInitZeroStore 依 LLVM 型別選擇零值 store 到 out 參數指標。
// 透過 classifyTypeKind 統一判斷型別種類，取代字串前綴檢查。
// option 型別補 nil（tag=1, data=0），與 generateOptionAssign 的 NilLiteral 分支一致；
// 整數補 0、浮點補 0.0、struct（%str-long/%vec/%arr/用戶結構）補 zeroinitializer。
func (g *Generator) emitRetInitZeroStore(sb *strings.Builder, name, llvmType string) {
	ptr := g.varAddr(name)
	desc := g.classifyTypeKind(llvmType)
	switch desc.Kind {
	case KindOption:
		// option 預設為 nil：tag=1（nil 標記）、data=0
		tagGEP := g.tmpReg("ri.zf.tag")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n", g.indent(), tagGEP, ptr))
		sb.WriteString(fmt.Sprintf("%sstore i64 1, i64* %s\n", g.indent(), tagGEP))
		dataGEP := g.tmpReg("ri.zf.data")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, ptr))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dataGEP))
	case KindInlineArray:
		// 原始陣列型別 [N x T]（out 參數為 [N x T]*）
		sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(llvmType), toLLVMType(llvmType), ptr))
	case KindVec, KindStr, KindArr, KindUserStruct, KindTask, KindFuture:
		// struct 類型：%str-long、%vec、%arr、用戶結構體、%task、%future
		sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(llvmType), toLLVMType(llvmType), ptr))
	case KindUnknown:
		// % 開頭但未在 structTypes 中註冊的型別（如生成的 %coro_state.N）：
		// 視為結構體，發出 zeroinitializer（與原始 strings.HasPrefix(llvmType, "%") 行為一致）
		if g.isStructLLVMType(llvmType) {
			sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(llvmType), toLLVMType(llvmType), ptr))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), ptr))
		}
	case KindScalar:
		if g.isIntegerLLVMType(llvmType) {
			irType := toLLVMType(llvmType)
			sb.WriteString(fmt.Sprintf("%sstore %s 0, %s* %s\n", g.indent(), irType, irType, ptr))
		} else if llvmType == "float" || llvmType == "double" {
			sb.WriteString(fmt.Sprintf("%sstore %s 0.0, %s* %s\n", g.indent(), llvmType, llvmType, ptr))
		} else {
			// 兜底：視為 i64
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), ptr))
		}
	default:
		// 兜底：視為 i64
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), ptr))
	}
}

// emitBitCheckFree 生成 IR：檢查堆變數對應的 bitmap bit。
// bit=1 → move 已發生，所有權轉移，跳過 free；
// bit=0 → move 未發生（分支未執行），仍擁有數據，需 free。
func (g *Generator) emitBitCheckFree(sb *strings.Builder, name string, varIdx int, llvmType, elemType string) {
	block := varIdx / 64
	offset := varIdx % 64
	bvName := fmt.Sprintf("%s%d", g.movedBitmapBase, block)
	bv := g.tmpReg("dc.bv")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), bv, bvName))
	masked := g.tmpReg("dc.masked")
	sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %d\n", g.indent(), masked, bv, 1<<offset))
	moved := g.tmpReg("dc.moved")
	sb.WriteString(fmt.Sprintf("%s%s = icmp ne i64 %s, 0\n", g.indent(), moved, masked))
	g.tmpIdx++
	freeLabel := fmt.Sprintf("dc.free.%d", g.tmpIdx)
	g.tmpIdx++
	skipLabel := fmt.Sprintf("dc.skip.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n",
		g.indent(), moved, skipLabel, freeLabel))
	fromBlock := g.cfgBlockLabel()
	g.cfgTerm(fromBlock, termCondBr)
	// free block: move 未發生（bit=0），仍擁有數據，需 free
	g.emitLabel(sb, freeLabel)
	g.cfgEdge(fromBlock, freeLabel)
	g.cfgEdge(fromBlock, skipLabel)
	g.emitVarHeapFree(sb, g.varAddr(name), llvmType, elemType, name)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.cfgTerm(freeLabel, termBr)
	// skip block: move 已發生（bit=1），所有權已轉移，跳過 free
	g.emitLabel(sb, skipLabel)
	g.cfgEdge(freeLabel, skipLabel)
}

// ---- move 賦值處理 ----

// handleMoveToOut 處理 move 到 out 參數：清舊 bit + 設新 bit + 更新 outBindState。
// 適用於 `out = x`（x 是局部堆變數，out 是輸出參數）。
func (g *Generator) handleMoveToOut(sb *strings.Builder, srcName, outName string) {
	srcVarIdx, ok := g.heapVarIndex[srcName]
	if !ok {
		return // 源不是局部堆變數
	}
	outIdx := g.outputParamBitIndex(outName)
	if outIdx < 0 {
		return
	}
	// 1. 清舊：若 out 參數之前綁定了別的變數，清除舊變數的 bit
	if outIdx < len(g.outBindState) {
		oldVarIdx := g.outBindState[outIdx]
		if oldVarIdx >= 0 {
			if g.hasBranchMove {
				g.emitClearMovedBitIR(sb, oldVarIdx) // 運行時清 bit
			} else {
				g.unmarkMovedVar(oldVarIdx) // 編譯期清 bit
			}
			// CFG: 舊綁定變數恢復所有權（effRemove）
			g.cfgAddEffect(effect{Kind: effRemove, VarIdx: oldVarIdx})
		}
	}
	// 2. 設新：設置當前變數的 bit
	if g.hasBranchMove {
		g.emitSetMovedBitIR(sb, srcVarIdx) // 運行時設 bit
	} else {
		g.markMovedVar(srcVarIdx) // 編譯期設 bit
	}
	// CFG: 源變數 moved（effAdd）+ out 參數綁定（effBind）
	g.cfgAddEffect(effect{Kind: effAdd, VarIdx: srcVarIdx})
	g.cfgAddEffect(effect{Kind: effBind, OutIdx: outIdx, VarIdx: srcVarIdx})
	// 3. 更新 outBindState
	if outIdx < len(g.outBindState) {
		g.outBindState[outIdx] = srcVarIdx
	}
}

// handleMoveLocal 處理局部間 move（b = a，不能深拷貝時）：僅設 bit。
// 所有權從 a 轉移到 b，a 不再擁有數據，函數結束時跳過 free。
func (g *Generator) handleMoveLocal(sb *strings.Builder, srcName string) {
	srcVarIdx, ok := g.heapVarIndex[srcName]
	if !ok {
		return
	}
	if g.hasBranchMove {
		g.emitSetMovedBitIR(sb, srcVarIdx) // 運行時設 bit
	} else {
		g.markMovedVar(srcVarIdx) // 編譯期設 bit
	}
	// CFG: 記錄 effAdd（源變數 moved，所有權轉移）
	g.cfgAddEffect(effect{Kind: effAdd, VarIdx: srcVarIdx})
}

// emitShallowDataFree releases a container's data buffer without iterating elements.
func (g *Generator) emitShallowDataFree(sb *strings.Builder, containerPtr, containerType string, dataFieldIdx int) {
	dataGEP := g.tmpReg("heapfree.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, containerType, containerType, containerPtr, dataFieldIdx))
	dataLoad := g.loadDataPtrField(sb, dataGEP)
	g.emitNullCheckFree(sb, dataLoad)
}

// emitNullCheckFree frees an i8* pointer with NULL check.
func (g *Generator) emitNullCheckFree(sb *strings.Builder, dataPtr string) {
	nullCmp := g.tmpReg("heapfree.null")
	g.tmpIdx++
	freeLabel := fmt.Sprintf("heapfree.free.%d", g.tmpIdx)
	g.tmpIdx++
	skipLabel := fmt.Sprintf("heapfree.skip.%d", g.tmpIdx)
	// 記錄當前 block（branch 前的 block），用於 CFG 邊追蹤
	fromBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, dataPtr))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, freeLabel))
	// CFG: branch 前設置 terminator（條件分支：兩個後繼）
	g.cfgTerm(fromBlock, termCondBr)
	g.emitLabel(sb, freeLabel)
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), dataPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	// CFG: freeLabel → skipLabel（單後繼 branch）
	g.cfgTerm(freeLabel, termBr)
	g.emitLabel(sb, skipLabel)
	// CFG: fromBlock → freeLabel, fromBlock → skipLabel, freeLabel → skipLabel
	g.cfgEdge(fromBlock, freeLabel)
	g.cfgEdge(fromBlock, skipLabel)
	g.cfgEdge(freeLabel, skipLabel)
}

// emitDeepContainerFree deep-frees a %vec/%arr: iterates elements to free their heap data, then frees the data buffer.
// elemElemType 為嵌套容器的內層元素型別（如 [][]i64 的 elemType="%vec", elemElemType="i64"），
// 為空時表示無嵌套，對 %vec/%arr 元素走淺層 free（與原行為一致）。
func (g *Generator) emitDeepContainerFree(sb *strings.Builder, containerPtr, containerType string, dataFieldIdx int, elemType string, elemElemType ...string) {
	g.tmpIdx++
	tid := g.tmpIdx
	lenGEP := fmt.Sprintf("%%df.len.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, containerType, containerType, containerPtr))
	lenReg := fmt.Sprintf("%%df.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))

	// Empty container (len == 0) owns no elements and no heap buffer, even if
	// its data pointer is still NULL — e.g. a global sitting in its
	// zeroinitializer state *before* its first assignment. This guard is
	// load-bearing: opt -O3 can prove the data field is non-null from the
	// `inbounds` field GEP and delete the NULL check below, turning a
	// not-yet-initialized global container into a NULL-deref crash.
	// Short-circuiting on len makes the free of such a container a no-op and
	// cannot be eliminated by the optimizer.
	lenZero := fmt.Sprintf("%%df.len.zero.%d", tid)
	skipLabel := fmt.Sprintf("df.skip.%d", tid)
	dataLoadLabel := fmt.Sprintf("df.dataload.%d", tid)
	fromBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), lenZero, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), lenZero, skipLabel, dataLoadLabel))
	g.cfgTerm(fromBlock, termCondBr)
	g.emitLabel(sb, dataLoadLabel)
	g.cfgEdge(fromBlock, skipLabel)
	g.cfgEdge(fromBlock, dataLoadLabel)

	dataGEP := fmt.Sprintf("%%df.data.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, containerType, containerType, containerPtr, dataFieldIdx))
	dataLoad := g.loadDataPtrField(sb, dataGEP)
	nullCmp := fmt.Sprintf("%%df.null.%d", tid)
	loopStartLabel := fmt.Sprintf("df.loop.start.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, dataLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, loopStartLabel))
	g.cfgTerm(dataLoadLabel, termCondBr)
	g.emitLabel(sb, loopStartLabel)
	g.cfgEdge(dataLoadLabel, skipLabel)
	g.cfgEdge(dataLoadLabel, loopStartLabel)
	iPtr := fmt.Sprintf("%%df.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	loopCondLabel := fmt.Sprintf("df.loop.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.cfgTerm(loopStartLabel, termBr)
	g.emitLabel(sb, loopCondLabel)
	g.cfgEdge(loopStartLabel, loopCondLabel)
	iVal := fmt.Sprintf("%%df.i.val.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	loopCmp := fmt.Sprintf("%%df.loop.cmp.%d", tid)
	loopBodyLabel := fmt.Sprintf("df.loop.body.%d", tid)
	loopEndLabel := fmt.Sprintf("df.loop.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), loopCmp, iVal, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), loopCmp, loopBodyLabel, loopEndLabel))
	g.cfgTerm(loopCondLabel, termCondBr)
	g.emitLabel(sb, loopBodyLabel)
	g.cfgEdge(loopCondLabel, loopBodyLabel)
	g.cfgEdge(loopCondLabel, loopEndLabel)
	elemArr := fmt.Sprintf("%%df.elemarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), elemArr, dataLoad, elemType))
	elemGEP := fmt.Sprintf("%%df.elem.gep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), elemGEP, elemType, elemType, elemArr, iVal))
	g.emitElementFree(sb, elemGEP, elemType, elemElemType...)
	// emitElementFree may create sub-blocks (e.g. heapfree.free.X / heapfree.skip.X
	// via emitNullCheckFree). After it returns, g.currentBlock is the last sub-block
	// (e.g. heapfree.skip.X), NOT loopBodyLabel. The back edge and terminator must
	// be recorded from the actual current block to keep the CFG consistent with
	// the emitted IR. Using loopBodyLabel here would create phantom edges
	// (loopBodyLabel → loopCondLabel) that don't exist in the IR, causing the
	// dataflow solver to produce incorrect moved-facts (e.g. triMustNot for a
	// variable that is actually moved), which in turn leads to double-free or
	// infinite-loop bugs under -O3.
	backEdgeFrom := g.cfgBlockLabel()
	iNext := fmt.Sprintf("%%df.i.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.cfgTerm(backEdgeFrom, termBr)
	g.emitLabel(sb, loopEndLabel)
	g.cfgEdge(backEdgeFrom, loopCondLabel) // back edge
	sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), dataLoad))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
	g.cfgTerm(loopEndLabel, termBr)
	g.emitLabel(sb, skipLabel)
	g.cfgEdge(loopEndLabel, skipLabel)
}

// emitElementFree frees heap data of a single container element.
// 透過 classifyFieldHeap 統一分派，與 emitDeepElementClone 共用型別判斷。
// elemElemType 為嵌套容器的內層元素型別（如 [][]i64 的 elemType="%vec", elemElemType="i64"），
// 為空時表示無嵌套（或未知），對 %vec/%arr 元素走淺層 free。
func (g *Generator) emitElementFree(sb *strings.Builder, elemPtr, elemType string, elemElemType ...string) {
	info := g.classifyFieldHeap(elemType, "")
	switch info.kind {
	case fieldHeapContainer:
		// 嵌套容器（%vec/%arr 元素為 %vec/%arr）：若有內層元素型別，遞迴釋放內層元素再 free data。
		innerType := ""
		if len(elemElemType) > 0 {
			innerType = elemElemType[0]
		}
		if innerType != "" && g.isHeapOwningType(innerType) {
			g.emitDeepContainerFree(sb, elemPtr, elemType, info.dataFieldIdx, innerType)
		} else {
			dataGEP := g.tmpReg("df.elem.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), dataGEP, elemType, elemType, elemPtr, info.dataFieldIdx))
			dataLoad := g.loadDataPtrField(sb, dataGEP)
			g.emitNullCheckFree(sb, dataLoad)
		}
	case fieldHeapUserStruct:
		g.emitStructFieldsFree(sb, elemPtr, info.containerType)
	}
}

// freeOldHeapValue 釋放重新賦值變數的舊堆數據。
// 跳過合成 let（IsSynthetic）。
// 輸出參數也釋放舊值（函數結束時由 emitHeapFree 跳過最終值，歸呼叫者）。
// 對已 move 的變數執行雙重校驗：bit=0 仍需 free，bit=1 跳過（所有權已轉移）。
// 釋放決策後清除 moved bit：變數即將獲得新值，不再處於 moved 狀態。
// 全局变量也釋放舊值（不在 heapVars 中，用 moduleVarTypes 查型別）。
func (g *Generator) freeOldHeapValue(sb *strings.Builder, stmt *parser.LetStatement, name string) {
	if stmt.IsSynthetic {
		return
	}
	// 全局變數首次（初始化）賦值：舊值是 zeroinitializer（容器）或字符串字面量（rodata），
	// 絕非堆數據。釋放它會 free rodata 崩潰（如 @HEX-UPPER 等 std 字符串常量全局），
	// 或讓 opt -O3 把後續賦值的 len/data 前向傳播到釋放點 load 而 NULL 解引用。
	// 因此全局首次賦值一律跳過釋放舊值；後續重賦值才釋放舊堆值（下方全局路徑處理）。
	if g.globalVars != nil && g.globalVars[name] && (g.funcLocalNames == nil || !g.funcLocalNames[name]) {
		if !g.globalFirstAssigned[name] {
			g.globalFirstAssigned[name] = true
			return
		}
	}
	// 局部堆變數路徑
	if g.heapVars != nil {
		oldType, isHeap := g.heapVars[name]
		if isHeap {
			elemType := ""
			if g.arrayElemTypes != nil {
				elemType = g.arrayElemTypes[name]
			}
		varIdx, hasIdx := g.heapVarIndex[name]
		if !hasIdx {
			g.emitVarHeapFree(sb, g.varAddr(name), oldType, elemType, name)
			return
		}
		// CFG: 局部堆變數（重）賦值，獲得本函數擁有的堆數據 → 標記 assigned。
		// 覆盖首次賦值、重賦值、以及 moved 後重新賦值三條路徑（下方分支前統一記錄）。
		g.cfgAddEffect(effect{Kind: effAssign, VarIdx: varIdx})
		if g.hasBranchMove && g.movedBitmapBase != "" {
				// 有運行時 bitmap：生成 IR 檢查 bit
				// bit=1 → 所有權已轉移，跳過 free（舊 data 不屬於此變數）
				// bit=0 → 仍擁有數據，free 舊值
				g.emitBitCheckFree(sb, name, varIdx, oldType, elemType)
				// 清除 moved bit：變數即將獲得新值，不再處於 moved 狀態。
				// 若不清除，函數結束時 emitHeapFree 會誤跳過釋放新值（記憶體洩漏）。
				g.emitClearMovedBitIR(sb, varIdx)
			} else {
				// 無 bitmap：編譯期檢查 movedVarBitset
				if g.isMovedVar(varIdx) {
					// 跳過 free（所有權轉移）
					// 清除 moved bit：變數即將獲得新值
					g.unmarkMovedVar(varIdx)
					// CFG: 變數重賦值，清除 moved 狀態（effRemove）
					g.cfgAddEffect(effect{Kind: effRemove, VarIdx: varIdx})
					return
				}
				g.emitVarHeapFree(sb, g.varAddr(name), oldType, elemType, name)
			}
			// CFG: 變數重賦值，清除 moved 狀態（effRemove）
			// 覆蓋 bitmap 和直接 free 兩條路徑（上面 return 的路徑已單獨記錄）
			g.cfgAddEffect(effect{Kind: effRemove, VarIdx: varIdx})
			return
		}
	}
	// 全局變數路徑（重賦值，非首次）：釋放「賦值前」舊值（全局無 moved 追蹤）。
	// 關鍵：先把舊值拷貝進一個局部 alloca，再釋放局部副本，而不是直接從 @g 重新讀取。
	// 否則 opt -O3 會把後續賦值的 store 經 SROA/GVN 前向傳播到釋放點的 load，並消除
	// 釋放迴圈的 NULL 檢查，導致以 len!=0 但 data=NULL 的狀態解引用而崩潰。
	if g.globalVars != nil && g.globalVars[name] && (g.funcLocalNames == nil || !g.funcLocalNames[name]) {
		oldType := ""
		if g.moduleVarTypes != nil {
			oldType = g.moduleVarTypes[name]
		}
		if oldType == "" && g.varTypes != nil {
			oldType = g.varTypes[name]
		}
		if oldType != "" && g.isHeapOwningType(oldType) {
			elemType := ""
			if g.moduleArrayElemTypes != nil {
				elemType = g.moduleArrayElemTypes[name]
			}
			if elemType == "" && g.arrayElemTypes != nil {
				elemType = g.arrayElemTypes[name]
			}
			g.emitVarHeapFreeViaLocalCopy(sb, g.varAddr(name), oldType, elemType, name)
		}
	}
}

// emitStructFieldsFree 遞迴釋放用戶結構體中所有含堆數據的欄位。
// 透過 classifyFieldHeap 統一判斷每個欄位的堆類型，與 emitStructClone 共用分派邏輯。
func (g *Generator) emitStructFieldsFree(sb *strings.Builder, structPtr, structType string) {
	structName := strings.TrimPrefix(structType, "%")
	fields, ok := g.structTypes[structName]
	if !ok {
		return
	}
	for i, f := range fields {
		info := g.classifyFieldHeap(f.typ, f.elemType)
		switch info.kind {
		case fieldHeapContainer:
			g.emitStructFieldFree(sb, structPtr, structType, i, info.dataFieldIdx, info.containerType, f.elemType, f.elemElemType)
		case fieldHeapUserStruct:
			fieldGEP := g.tmpReg("structfield.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), fieldGEP, structType, structType, structPtr, i))
			g.emitStructFieldsFree(sb, fieldGEP, info.containerType)
		case fieldHeapNone:
			// 內聯固定陣列字段 [N x T]（如 hashmap 的 keys [256]str）：
			// 遍歷 N 個元素遞迴釋放其堆數據。純量元素（i64/i8/...）無需釋放。
			if n, elemType, ok := parseInlineArrayType(f.typ); ok && g.isHeapOwningType(elemType) {
				g.emitInlineArrayFieldFree(sb, structPtr, structType, i, n, elemType, f.typ, f.elemElemType)
			}
		}
	}
}

// parseInlineArrayType 解析 LLVM 內聯固定陣列類型字串 "[N x T]"。
// 返回 (N, elemType, true)；非內聯陣列或解析失敗返回 (0, "", false)。
// 例："[256 x %str-long]" → (256, "%str-long", true)。
func parseInlineArrayType(s string) (int64, string, bool) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return 0, "", false
	}
	inner := s[1 : len(s)-1] // "256 x %str-long"
	parts := strings.SplitN(inner, " x ", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	var n int64
	if _, err := fmt.Sscanf(parts[0], "%d", &n); err != nil {
		return 0, "", false
	}
	elem := strings.TrimSpace(parts[1])
	if n <= 0 || elem == "" {
		return 0, "", false
	}
	return n, elem, true
}

// emitInlineArrayFieldFree 釋放結構體內聯固定陣列字段 [N x T] 中每個元素的堆數據。
// 用於 hashmap 等含 [N]str 鍵字段的場景：遍歷 N 個 slot，對每個非空元素遞迴釋放。
// fieldIdx 為字段在結構體中的索引，arrayType 為字段的 LLVM 類型字串（如 "[256 x %str-long]"）。
// elemElemType 為嵌套容器的內層元素型別（如 [N][]str → elemType="%vec", elemElemType="%str-long"）。
func (g *Generator) emitInlineArrayFieldFree(sb *strings.Builder, structPtr, structType string, fieldIdx int, n int64, elemType, arrayType, elemElemType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	fieldGEP := fmt.Sprintf("%%inlarr.fgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), fieldGEP, structType, structType, structPtr, fieldIdx))
	// 循環計數器
	iPtr := fmt.Sprintf("%%inlarr.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	condLabel := fmt.Sprintf("inlarr.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, condLabel)
	iVal := fmt.Sprintf("%%inlarr.iv.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	cmp := fmt.Sprintf("%%inlarr.cmp.%d", tid)
	bodyLabel := fmt.Sprintf("inlarr.body.%d", tid)
	endLabel := fmt.Sprintf("inlarr.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmp, iVal, n))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), cmp, bodyLabel, endLabel))
	g.emitLabel(sb, bodyLabel)
	// GEP 第 i 個元素：結果類型為 elemType*，可直接傳給 emitElementFree
	// 注意：[N x T]* 的 GEP 需要兩個索引 (0, i)：
	//   - 第一個索引 0：不穿過 [N x T] 陣列本身（步長 = sizeof([N x T])）
	//   - 第二個索引 i：存取陣列內部第 i 個元素（步長 = sizeof(T)）
	// 若只用一個索引 i，步長會變成 sizeof([N x T])，導致越界存取。
	elemGEP := fmt.Sprintf("%%inlarr.elem.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
		g.indent(), elemGEP, arrayType, arrayType, fieldGEP, iVal))
	// 對 %str-long 元素加 len==0 跳過檢查：
	// hashmap 等 std 容器的 init 只清零 len 未清零 data，未使用 slot 的 data 為垃圾值。
	// len==0 表示該 slot 未持有堆數據，跳過 free 避免 free 垃圾指標。
	// 對 %vec/%arr 同理（len=0 表示無元素）。三者 len 均為 field 0。
	if elemType == "%str-long" || elemType == "%vec" || elemType == "%arr" {
		lenGEP := fmt.Sprintf("%%inlarr.len.gep.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, elemType, elemType, elemGEP))
		lenLoad := fmt.Sprintf("%%inlarr.len.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
		lenCmp := fmt.Sprintf("%%inlarr.lencmp.%d", tid)
		skipLabel := fmt.Sprintf("inlarr.skip.%d", tid)
		freeLabel := fmt.Sprintf("inlarr.free.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), lenCmp, lenLoad))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), lenCmp, skipLabel, freeLabel))
		g.emitLabel(sb, freeLabel)
		g.emitElementFree(sb, elemGEP, elemType, elemElemType)
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		g.emitLabel(sb, skipLabel)
	} else {
		g.emitElementFree(sb, elemGEP, elemType, elemElemType)
	}
	next := fmt.Sprintf("%%inlarr.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), next, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), next, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, endLabel)
}

// emitStructFieldFree frees heap data of struct field (deep free for vec/arr with heap-owning elements).
func (g *Generator) emitStructFieldFree(sb *strings.Builder, structPtr, structType string, fieldIdx, dataFieldIdx int, fieldType, fieldElemType, fieldElemElemType string) {
	fieldGEP := g.tmpReg("structfield.fgep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), fieldGEP, structType, structType, structPtr, fieldIdx))
	if fieldType == "%str-long" {
		// len==0 skip: struct fields initialized with string literals (e.g.
		// json-pool.init sets str-val = ' ') have non-null data pointers
		// pointing to non-heap (alloca/rodata) memory. Freeing them crashes.
		// len==0 means the field holds no heap data; skip free to match
		// emitInlineArrayFieldFree's behavior for the same scenario.
		g.tmpIdx++
		tid := g.tmpIdx
		lenGEP := fmt.Sprintf("%%sf.len.gep.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, fieldGEP))
		lenLoad := fmt.Sprintf("%%sf.len.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
		lenCmp := fmt.Sprintf("%%sf.lencmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), lenCmp, lenLoad))
		skipLabel := fmt.Sprintf("sf.skip.%d", tid)
		freeLabel := fmt.Sprintf("sf.free.%d", tid)
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), lenCmp, skipLabel, freeLabel))
		g.emitLabel(sb, freeLabel)
		g.emitShallowDataFree(sb, fieldGEP, fieldType, dataFieldIdx)
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		g.emitLabel(sb, skipLabel)
		return
	}
	if g.isHeapOwningType(fieldElemType) {
		g.emitDeepContainerFree(sb, fieldGEP, fieldType, dataFieldIdx, fieldElemType, fieldElemElemType)
		return
	}
	g.emitShallowDataFree(sb, fieldGEP, fieldType, dataFieldIdx)
}

// canDeepCloneStruct 遞迴檢查用戶結構體是否可以安全深層 clone。
// 在 Nolang 中所有型別都是可 clone 的：嵌套容器（[][]T）透過 elemElemType
// 機制支持遞迴深拷貝，用戶結構體透過 emitStructClone 遞迴處理。
// 此函數始終返回 true，保留介面供呼叫端使用。
func (g *Generator) canDeepCloneStruct(structType string) bool {
	return true
}

// emitDeepClone 生成深層 clone 代碼：從 srcPtr 深層複製到 dstPtr。
// 透過 classifyFieldHeap 統一分派，與 emitVarHeapFree 共用型別判斷。
// 對於 %vec/%arr：malloc 新 data 緩衝區，memcpy，遞迴 clone 元素。
// 對於 %str-long：malloc 新 data 緩衝區，memcpy（元素為 i8，無需遞迴）。
// 對於用戶結構體：memcpy 整個結構體，遞迴 clone 含堆數據的欄位。
// elemElemType 為嵌套容器的內層元素型別（如 [][]i64 的 elemType="%vec", elemElemType="i64"）。
func (g *Generator) emitDeepClone(sb *strings.Builder, srcPtr, dstPtr, llvmType, elemType string, elemElemType ...string) {
	info := g.classifyFieldHeap(llvmType, elemType)
	switch info.kind {
	case fieldHeapContainer:
		// %str-long 的 elemType 固定為 "i8"
		cloneElem := elemType
		if llvmType == "%str-long" {
			cloneElem = "i8"
		}
		g.emitContainerClone(sb, srcPtr, dstPtr, info.containerType, info.dataFieldIdx, cloneElem, elemElemType...)
	case fieldHeapUserStruct:
		g.emitStructClone(sb, srcPtr, dstPtr, info.containerType)
	}
}

// emitContainerClone 深層 clone %vec/%arr/%str-long：
// 先 store zeros 到 dst（處理 NULL 源資料），再 malloc+memcpy+遞迴 clone 元素。
// elemElemType 為嵌套容器的內層元素型別，傳遞給 emitDeepElementClone 做遞迴 clone。
func (g *Generator) emitContainerClone(sb *strings.Builder, srcPtr, dstPtr, containerType string, dataFieldIdx int, elemType string, elemElemType ...string) {
	g.tmpIdx++
	tid := g.tmpIdx

	// 先 store zeros 到 dst（處理源 data 為 NULL 的情況）
	dstLenGEP := fmt.Sprintf("%%clone.dst.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), dstLenGEP, containerType, containerType, dstPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dstLenGEP))
	if containerType == "%vec" || containerType == "%str-long" {
		dstCapGEP := fmt.Sprintf("%%clone.dst.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
			g.indent(), dstCapGEP, containerType, containerType, dstPtr))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dstCapGEP))
	}
	// 運行時 move 標記改用函數級位圖變數，結構體內無 is_moved 欄位，深拷貝無需重置。
	dstDataGEP := fmt.Sprintf("%%clone.dst.data.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstDataGEP, containerType, containerType, dstPtr, dataFieldIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dstDataGEP))

	// load source len
	srcLenGEP := fmt.Sprintf("%%clone.src.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), srcLenGEP, containerType, containerType, srcPtr))
	srcLenReg := fmt.Sprintf("%%clone.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcLenReg, srcLenGEP))

	// load source cap（%vec/%str-long 有 cap 欄位；%arr 用 len 作為 cap）
	var srcCapReg string
	if containerType == "%vec" || containerType == "%str-long" {
		srcCapGEP := fmt.Sprintf("%%clone.src.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
			g.indent(), srcCapGEP, containerType, containerType, srcPtr))
		srcCapReg = fmt.Sprintf("%%clone.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcCapReg, srcCapGEP))
	} else {
		srcCapReg = srcLenReg
	}

	// load source data
	srcDataGEP := fmt.Sprintf("%%clone.src.data.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcDataGEP, containerType, containerType, srcPtr, dataFieldIdx))
	srcDataLoad := g.loadDataPtrField(sb, srcDataGEP)

	// NULL check
	nullCmp := fmt.Sprintf("%%clone.null.%d", tid)
	skipLabel := fmt.Sprintf("clone.skip.%d", tid)
	doLabel := fmt.Sprintf("clone.do.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, srcDataLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, doLabel))

	// clone.do: malloc + memcpy + deep clone elements + overwrite dst
	g.emitLabel(sb, doLabel)
	elemSize := g.llvmTypeSize(elemType)
	if elemSize == 0 {
		elemSize = 8
	}
	bufSizeReg := fmt.Sprintf("%%clone.bufsize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), bufSizeReg, srcCapReg, elemSize))
	cloneBuf := fmt.Sprintf("%%clone.buf.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), cloneBuf, bufSizeReg))
	copySizeReg := fmt.Sprintf("%%clone.copysize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), copySizeReg, srcLenReg, elemSize))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), cloneBuf, srcDataLoad, copySizeReg))

	// 若元素是堆擁有型別，遞迴 clone 每個元素的 data
	if g.isHeapOwningType(elemType) {
		g.emitDeepElementClone(sb, cloneBuf, srcDataLoad, srcLenReg, elemType, elemElemType...)
	}

	// overwrite dst with actual values
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcLenReg, dstLenGEP))
	if containerType == "%vec" || containerType == "%str-long" {
		dstCapGEP := fmt.Sprintf("%%clone.dst.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), srcCapReg, dstCapGEP))
	}
	g.storeDataPtrField(sb, cloneBuf, dstDataGEP)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))

	// clone.skip: dst 已有 zeros，無需操作
	g.emitLabel(sb, skipLabel)
}

// emitDeepElementClone 遍歷容器元素，clone 每個元素的堆 data。
// 透過 classifyFieldHeap 統一分派，與 emitElementFree 共用型別判斷。
// 用於 %str-long 元素（clone 每個字串的 data）、用戶結構體元素（遞迴 clone 欄位）、
// 以及嵌套容器元素（%vec/%arr 元素為 %vec/%arr，需遞迴 clone 內層元素）。
// elemElemType 為嵌套容器的內層元素型別（如 [][]i64 的 elemType="%vec", elemElemType="i64"）。
func (g *Generator) emitDeepElementClone(sb *strings.Builder, dstBuf, srcBuf, lenReg, elemType string, elemElemType ...string) {
	info := g.classifyFieldHeap(elemType, "")
	// 用戶結構體元素：遞迴 clone 每個元素的欄位
	if info.kind == fieldHeapUserStruct {
		g.emitStructElementsClone(sb, dstBuf, srcBuf, lenReg, info.containerType)
		return
	}
	if info.kind != fieldHeapContainer {
		return // 純量元素，已由 memcpy 複製
	}
	dataFieldIdx := info.dataFieldIdx

	// 確定內層元素型別（用於嵌套容器遞迴 clone）
	innerType := ""
	if len(elemElemType) > 0 {
		innerType = elemElemType[0]
	}

	g.tmpIdx++
	tid := g.tmpIdx
	elemSize := g.llvmTypeSize(elemType)
	if elemSize == 0 {
		elemSize = 8
	}

	// 迴圈: for i = 0; i < len; i++
	iPtr := fmt.Sprintf("%%clonec.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	loopCondLabel := fmt.Sprintf("clonec.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopCondLabel)
	iVal := fmt.Sprintf("%%clonec.i.val.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	loopCmp := fmt.Sprintf("%%clonec.cmp.%d", tid)
	loopBodyLabel := fmt.Sprintf("clonec.body.%d", tid)
	loopEndLabel := fmt.Sprintf("clonec.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), loopCmp, iVal, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), loopCmp, loopBodyLabel, loopEndLabel))
	g.emitLabel(sb, loopBodyLabel)

	// 取得 src 和 dst 元素指標
	srcElemArr := fmt.Sprintf("%%clonec.srcarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), srcElemArr, srcBuf, elemType))
	srcElemGEP := fmt.Sprintf("%%clonec.srcgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), srcElemGEP, elemType, elemType, srcElemArr, iVal))
	dstElemArr := fmt.Sprintf("%%clonec.dstarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dstElemArr, dstBuf, elemType))
	dstElemGEP := fmt.Sprintf("%%clonec.dstgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), dstElemGEP, elemType, elemType, dstElemArr, iVal))

	// load src 元素的 data
	srcElemDataGEP := fmt.Sprintf("%%clonec.srcdata.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcElemDataGEP, elemType, elemType, srcElemGEP, dataFieldIdx))
	srcElemData := g.loadDataPtrField(sb, srcElemDataGEP)

	// NULL check
	elemNullCmp := fmt.Sprintf("%%clonec.null.%d", tid)
	elemSkipLabel := fmt.Sprintf("clonec.skip.%d", tid)
	elemDoLabel := fmt.Sprintf("clonec.do.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), elemNullCmp, srcElemData))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), elemNullCmp, elemSkipLabel, elemDoLabel))

	// clonec.do: malloc + memcpy + store to dst
	g.emitLabel(sb, elemDoLabel)
	// load src 元素的 len（用於 memcpy 大小）
	srcElemLenGEP := fmt.Sprintf("%%clonec.srclen.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), srcElemLenGEP, elemType, elemType, srcElemGEP))
	srcElemLen := fmt.Sprintf("%%clonec.len.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcElemLen, srcElemLenGEP))
	// load src 元素的 cap（用於 malloc 大小）
	var srcElemCap string
	if elemType == "%vec" || elemType == "%str-long" {
		srcElemCapGEP := fmt.Sprintf("%%clonec.srccap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
			g.indent(), srcElemCapGEP, elemType, elemType, srcElemGEP))
		srcElemCap = fmt.Sprintf("%%clonec.cap.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), srcElemCap, srcElemCapGEP))
	} else {
		srcElemCap = srcElemLen
	}
	// 子元素大小：%str-long=1 byte；嵌套容器的內層元素用 llvmTypeSize 計算
	subElemSize := int64(1)
	if innerType != "" {
		if s := g.llvmTypeSize(innerType); s > 0 {
			subElemSize = s
		} else {
			subElemSize = 8 // fallback for unknown types
		}
	} else if elemType == "%vec" || elemType == "%arr" {
		subElemSize = 8 // fallback（無內層型別資訊，不應到達此處）
	}
	elemBufSize := fmt.Sprintf("%%clonec.bufsize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), elemBufSize, srcElemCap, subElemSize))
	elemCloneBuf := fmt.Sprintf("%%clonec.buf.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), elemCloneBuf, elemBufSize))
	elemCopySize := fmt.Sprintf("%%clonec.copysize.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), elemCopySize, srcElemLen, subElemSize))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), elemCloneBuf, srcElemData, elemCopySize))
	// 嵌套容器：遞迴 clone 內層元素的堆 data
	if innerType != "" && g.isHeapOwningType(innerType) {
		if os.Getenv("NOLANG_DEBUG_CLONE") != "" {
			fmt.Fprintf(os.Stderr, "[debug-clone] emitDeepElementClone recurse: elemType=%q innerType=%q elemElemType=%v\n", elemType, innerType, elemElemType)
		}
		g.emitDeepElementClone(sb, elemCloneBuf, srcElemData, srcElemLen, innerType)
	}
	// store new data to dst 元素
	dstElemDataGEP := fmt.Sprintf("%%clonec.dstdata.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstElemDataGEP, elemType, elemType, dstElemGEP, dataFieldIdx))
	g.storeDataPtrField(sb, elemCloneBuf, dstElemDataGEP)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), elemSkipLabel))

	// clonec.skip: 繼續下一個元素
	g.emitLabel(sb, elemSkipLabel)
	iNext := fmt.Sprintf("%%clonec.i.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopEndLabel)
}

// emitStructElementsClone 遍歷用戶結構體元素陣列，遞迴 clone 每個元素的欄位。
func (g *Generator) emitStructElementsClone(sb *strings.Builder, dstBuf, srcBuf, lenReg, structType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	elemSize := g.llvmTypeSize(structType)
	if elemSize == 0 {
		elemSize = 8
	}
	iPtr := fmt.Sprintf("%%clones.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	loopCondLabel := fmt.Sprintf("clones.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopCondLabel)
	iVal := fmt.Sprintf("%%clones.i.val.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	loopCmp := fmt.Sprintf("%%clones.cmp.%d", tid)
	loopBodyLabel := fmt.Sprintf("clones.body.%d", tid)
	loopEndLabel := fmt.Sprintf("clones.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), loopCmp, iVal, lenReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), loopCmp, loopBodyLabel, loopEndLabel))
	g.emitLabel(sb, loopBodyLabel)
	srcElemArr := fmt.Sprintf("%%clones.srcarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), srcElemArr, srcBuf, structType))
	srcElemGEP := fmt.Sprintf("%%clones.srcgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), srcElemGEP, structType, structType, srcElemArr, iVal))
	dstElemArr := fmt.Sprintf("%%clones.dstarr.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dstElemArr, dstBuf, structType))
	dstElemGEP := fmt.Sprintf("%%clones.dstgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
		g.indent(), dstElemGEP, structType, structType, dstElemArr, iVal))
	// 遞迴 clone 結構體欄位
	g.emitStructClone(sb, srcElemGEP, dstElemGEP, structType)
	iNext := fmt.Sprintf("%%clones.i.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopCondLabel))
	g.emitLabel(sb, loopEndLabel)
}

// emitStructClone 深層 clone 用戶結構體：先 memcpy 整個結構體，再遞迴 clone 含堆數據的欄位。
// 透過 classifyFieldHeap 統一判斷每個欄位的堆類型，與 emitStructFieldsFree 共用分派邏輯。
func (g *Generator) emitStructClone(sb *strings.Builder, srcPtr, dstPtr, structType string) {
	// 先 memcpy 整個結構體（複製所有欄位，包括 data 指標）
	structSize := g.llvmTypeSize(structType)
	if structSize == 0 {
		structSize = 8
	}
	srcI8 := g.tmpReg("structclone.src.i8")
	sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), srcI8, structType, srcPtr))
	dstI8 := g.tmpReg("structclone.dst.i8")
	sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), dstI8, structType, dstPtr))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %d, i1 false)\n",
		g.indent(), dstI8, srcI8, structSize))

	// 遞迴 clone 含堆數據的欄位（覆寫 data 指標為獨立的 clone）
	structName := strings.TrimPrefix(structType, "%")
	fields, ok := g.structTypes[structName]
	if !ok {
		return
	}
	for i, f := range fields {
		info := g.classifyFieldHeap(f.typ, f.elemType)
		if os.Getenv("NOLANG_DEBUG_CLONE") != "" && info.kind == fieldHeapContainer {
			fmt.Fprintf(os.Stderr, "[debug-clone] emitStructClone %s field[%d] %q: typ=%q elemType=%q elemElemType=%q\n", structName, i, f.name, f.typ, f.elemType, f.elemElemType)
		}
		switch info.kind {
		case fieldHeapContainer:
			g.emitStructFieldClone(sb, srcPtr, dstPtr, structType, i, info.dataFieldIdx, info.containerType, f.elemType, f.elemElemType)
		case fieldHeapUserStruct:
			srcFieldGEP := g.tmpReg("structclone.src.fgep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), srcFieldGEP, structType, structType, srcPtr, i))
			dstFieldGEP := g.tmpReg("structclone.dst.fgep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), dstFieldGEP, structType, structType, dstPtr, i))
			g.emitStructClone(sb, srcFieldGEP, dstFieldGEP, info.containerType)
		case fieldHeapNone:
			// 內聯固定陣列字段 [N x T]（如 hashmap 的 keys [256]str）：
			// memcpy 已淺拷貝所有元素的 data 指標，需遍歷 N 個元素遞迴 clone，
			// 覆寫 dst 元素的 data 為獨立 clone，避免 a/b 共享 data 導致 double-free。
			if n, elemType, ok := parseInlineArrayType(f.typ); ok && g.isHeapOwningType(elemType) {
				g.emitInlineArrayFieldClone(sb, srcPtr, dstPtr, structType, i, n, elemType, f.typ, f.elemElemType)
			}
		}
	}
}

// emitInlineArrayFieldClone 深層 clone 結構體內聯固定陣列字段 [N x T] 的每個元素。
// 用於 hashmap 等含 [N]str 鍵字段的場景：遍歷 N 個 slot，對每個元素遞迴 clone，
// 使 dst 擁有獨立的堆數據，避免與 src 共享 data 指標。
// elemElemType 為嵌套容器的內層元素型別（如 [N][]str → elemType="%vec", elemElemType="%str-long"）。
func (g *Generator) emitInlineArrayFieldClone(sb *strings.Builder, srcPtr, dstPtr, structType string, fieldIdx int, n int64, elemType, arrayType, elemElemType string) {
	g.tmpIdx++
	tid := g.tmpIdx
	srcFieldGEP := fmt.Sprintf("%%inlarrc.src.fgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcFieldGEP, structType, structType, srcPtr, fieldIdx))
	dstFieldGEP := fmt.Sprintf("%%inlarrc.dst.fgep.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstFieldGEP, structType, structType, dstPtr, fieldIdx))
	// 循環計數器
	iPtr := fmt.Sprintf("%%inlarrc.i.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iPtr))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iPtr))
	condLabel := fmt.Sprintf("inlarrc.cond.%d", tid)
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, condLabel)
	iVal := fmt.Sprintf("%%inlarrc.iv.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iPtr))
	cmp := fmt.Sprintf("%%inlarrc.cmp.%d", tid)
	bodyLabel := fmt.Sprintf("inlarrc.body.%d", tid)
	endLabel := fmt.Sprintf("inlarrc.end.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmp, iVal, n))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), cmp, bodyLabel, endLabel))
	g.emitLabel(sb, bodyLabel)
	// GEP src/dst 第 i 個元素
	// 注意：[N x T]* 的 GEP 需要兩個索引 (0, i)，詳見 emitInlineArrayFieldFree 的註解。
	srcElemGEP := fmt.Sprintf("%%inlarrc.src.elem.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
		g.indent(), srcElemGEP, arrayType, arrayType, srcFieldGEP, iVal))
	dstElemGEP := fmt.Sprintf("%%inlarrc.dst.elem.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
		g.indent(), dstElemGEP, arrayType, arrayType, dstFieldGEP, iVal))
	// 遞迴 clone 元素（%str-long → emitContainerClone；用戶結構體 → emitStructClone）
	g.emitDeepClone(sb, srcElemGEP, dstElemGEP, elemType, "", elemElemType)
	next := fmt.Sprintf("%%inlarrc.next.%d", tid)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), next, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), next, iPtr))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), condLabel))
	g.emitLabel(sb, endLabel)
}

// emitStructFieldClone clone 結構體欄位的堆 data。
func (g *Generator) emitStructFieldClone(sb *strings.Builder, srcPtr, dstPtr, structType string, fieldIdx, dataFieldIdx int, fieldType, fieldElemType, fieldElemElemType string) {
	srcFieldGEP := g.tmpReg("structclone.src.flg")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), srcFieldGEP, structType, structType, srcPtr, fieldIdx))
	dstFieldGEP := g.tmpReg("structclone.dst.flg")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dstFieldGEP, structType, structType, dstPtr, fieldIdx))
	if fieldType == "%str-long" {
		g.emitContainerClone(sb, srcFieldGEP, dstFieldGEP, "%str-long", 2, "i8")
		return
	}
	g.emitContainerClone(sb, srcFieldGEP, dstFieldGEP, fieldType, dataFieldIdx, fieldElemType, fieldElemElemType)
}

// initVecFieldFromSliceLiteral 在 struct literal 中將 SliceLiteral 初始化到 %vec 欄位。
// 使用 malloc 分配堆內存（而非 alloca），確保 struct 通過 out 參數返回後 data 仍有效。
func (g *Generator) initVecFieldFromSliceLiteral(sb *strings.Builder, structVar, structTy string, fieldIdx int, slice *parser.SliceLiteral, fieldElemType string) {
	elemType := fieldElemType
	if elemType == "" {
		elemType = "i64"
	}
	n := int64(len(slice.Elements))
	g.tmpIdx++
	tid := g.tmpIdx
	tmpArr := fmt.Sprintf("%%st.slice.tmp.%d", tid)
	arrType := fmt.Sprintf("[%d x %s]", n, toLLVMType(elemType))
	elemSize := g.llvmTypeSize(elemType)
	if elemSize == 0 {
		elemSize = 8
	}
	bufSize := n * elemSize

	// malloc 分配堆內存（避免函數返回後棧幀銷毀導致懸垂指針）
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), tmpArr, bufSize))
	// bitcast to element array pointer for GEP store
	arrPtrReg := g.tmpReg("st.slice.arrptr")
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), arrPtrReg, tmpArr, arrType))

	// 逐元素 store
	for i, elem := range slice.Elements {
		ev := g.generateExprWithSB(sb, elem)
		ev = g.stripLLVMType(ev)
		gepReg := g.tmpReg("st.slice.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
			g.indent(), gepReg, arrType, arrType, arrPtrReg, i))
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(elemType), ev, toLLVMType(elemType), gepReg))
	}

	// data 指針即 malloc 返回的 i8*
	ptrReg := tmpArr

	// store len（欄位的 field 0）
	lenGEP := g.tmpReg("st.slice.len")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 0\n",
		g.indent(), lenGEP, structTy, structTy, g.varAddr(structVar), fieldIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

	// store cap（欄位的 field 1）
	capGEP := g.tmpReg("st.slice.cap")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 1\n",
		g.indent(), capGEP, structTy, structTy, g.varAddr(structVar), fieldIdx))
	sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

	// store data（欄位的 field 2）
	dataGEP := g.tmpReg("st.slice.data")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 2\n",
		g.indent(), dataGEP, structTy, structTy, g.varAddr(structVar), fieldIdx))
	g.storeDataPtrField(sb, ptrReg, dataGEP)
}

// resolveParamLLVMType 計算參數的 LLVM 型別字串。
// 當參數型別為具名型別且對應至具名函式型別別名時，解析為函式指標型別 void (...)*。
func (g *Generator) resolveParamLLVMType(t parser.Type) string {
	if nt, ok := t.(*parser.NamedType); ok && g.fnTypeAliases != nil {
		if ft, ok := g.fnTypeAliases[nt.Value]; ok {
			return g.mapToLLVMType(ft.String())
		}
	}
	// 單具體型別別名解析：若 NamedType 是已註冊的具體型別別名，用底層 Type 遞迴解析
	// 使 SliceType/ArrayType 等特殊路徑也能正確套用到底層型別
	if nt, ok := t.(*parser.NamedType); ok && g.concreteTypeAliases != nil {
		if underlying, ok := g.concreteTypeAliases[nt.Value]; ok {
			return g.resolveParamLLVMType(underlying)
		}
	}
	// MapType: String() returns [K]V which is ambiguous with [N]T arrays in
	// string-based dispatch; use LLVMName() to get the hashmap-K-V struct name.
	if mt, ok := t.(*parser.MapType); ok {
		return g.mapToLLVMType(mt.LLVMName())
	}
	// Fixed-size ArrayType (e.g. [32]byte) → raw LLVM array [32 x i8] to match
	// struct field type (collectStructTypeFields uses the same raw array form).
	// Without this, [32]byte params would be typed as %arr (struct with len/data
	// pointer), which is incompatible with [32 x i8] struct fields.
	if at, ok := t.(*parser.ArrayType); ok && at.Size != nil {
		return g.arrayTypeToLLVM(at)
	}
	// SliceType (e.g. []str, []byte) → %vec (built-in struct: vec { len, cap, data }).
	// Without this, String() returns "[]str" which mapToLLVMType would route through
	// its "[" prefix branch and correctly return "%vec" — but only when called
	// directly. The dedicated handling here makes the intent explicit and ensures
	// funcResultLLVMType registers "%vec" for slice-returning functions (e.g.
	// list-dir returns []str), so downstream varLLVMType can infer %vec for
	// `entries = fs.list-dir(dir)` instead of defaulting to i64.
	if st, ok := t.(*parser.SliceType); ok {
		_ = st
		return "%vec"
	}
	return g.mapToLLVMType(t.String())
}

// hasInlineArrayStructParam checks if a function definition has parameters or
// local struct types that contain inline array fields with heap-owning elements
// (e.g., json.json with [64]json-value where json-value has %str-long fields).
// Such functions use deep clone/free patterns that can trigger LLVM -O3
// optimizer miscompilation, so optnone is added to prevent it.
func (g *Generator) hasInlineArrayStructParam(fd *parser.FunctionDefinition) bool {
	checkType := func(llvmType string) bool {
		if !g.isStructLLVMType(llvmType) {
			return false
		}
		structName := strings.TrimPrefix(llvmType, "%")
		return g.structHasInlineArrayHeap(structName, map[string]bool{})
	}
	// Check parameters
	for _, p := range fd.Parameters {
		llvmType := g.resolveParamLLVMType(p.Type)
		if checkType(llvmType) {
			return true
		}
	}
	// Check results
	for _, r := range fd.Results {
		llvmType := g.resolveParamLLVMType(r.Type)
		if checkType(llvmType) {
			return true
		}
	}
	return false
}

// hasMultiStrResults checks if a function has 2 or more str (%str-long) output
// parameters. When such functions are inlined by the LLVM optimizer, the
// caller's alloca slots for str outputs can be mis-optimized (constant
// propagation corrupts the first str output). Adding optnone+noinline
// prevents inlining and optimization, working around the bug.
func (g *Generator) hasMultiStrResults(fd *parser.FunctionDefinition) bool {
	strCount := 0
	for _, r := range fd.Results {
		llvmType := g.resolveParamLLVMType(r.Type)
		if llvmType == "%str-long" {
			strCount++
		}
	}
	return strCount >= 2
}

// structHasInlineArrayHeap recursively checks if a struct type contains
// inline array fields ([N x T]) where T is a heap-owning type.
func (g *Generator) structHasInlineArrayHeap(structName string, visited map[string]bool) bool {
	if visited[structName] {
		return false
	}
	visited[structName] = true
	fields, ok := g.structTypes[structName]
	if !ok {
		return false
	}
	for _, f := range fields {
		// Check if field is an inline array [N x T]
		if n, elemType, ok := parseInlineArrayType(f.typ); ok && n > 0 {
			if g.isHeapOwningType(elemType) {
				return true
			}
			// Recursively check if elemType is a struct with inline arrays
			if g.isStructLLVMType(elemType) {
				innerName := strings.TrimPrefix(elemType, "%")
				if g.structHasInlineArrayHeap(innerName, visited) {
					return true
				}
			}
		}
		// Recursively check struct fields
		if g.isStructLLVMType(f.typ) && !strings.HasPrefix(f.typ, "%str-long") &&
			!strings.HasPrefix(f.typ, "%vec") && !strings.HasPrefix(f.typ, "%arr") {
			innerName := strings.TrimPrefix(f.typ, "%")
			if g.structHasInlineArrayHeap(innerName, visited) {
				return true
			}
		}
	}
	return false
}

func (g *Generator) generateFunctionDefinition(sb *strings.Builder, fd *parser.FunctionDefinition) {
	// 創建全新 funcState 實例：消除 40+ 手動重置，防止遺漏導致跨函數污染
	g.resetFuncState()
	g.curFuncName = fd.Name
	g.inMainFunction = false
	// 无栈协程：检测含 awy 的函数，变换为状态机。
	// 含 awy 的函数不再生成原始函数体，而是生成 coro_resume.N 状态机函数。
	if fd.Body != nil && len(fd.Body.Statements) > 0 {
		if len(collectTopLevelAwaitIndices(fd.Body.Statements)) > 0 {
			g.transformAsyncFunction(sb, fd)
			return
		}
	}
	for _, p := range fd.Parameters {
		g.paramNames[p.Name] = true
		g.funcLocalNames[p.Name] = true
		g.funcParams[p.Name] = true
		g.varTypes[p.Name] = g.resolveParamLLVMType(p.Type)
		// Track FunctionType parameters for indirect call codegen
		if ft, ok := p.Type.(*parser.FunctionType); ok {
			g.varFnTypes[p.Name] = ft
		}
		// 參數型別為具名型別且對應至具名函式型別別名時，亦登錄為間接呼叫目標
		if nt, ok := p.Type.(*parser.NamedType); ok && g.fnTypeAliases != nil {
			if ft, ok := g.fnTypeAliases[nt.Value]; ok {
				g.varFnTypes[p.Name] = ft
			}
		}
		// 陣列型輸入參數需註冊元素型別與大小，供後續索引賦值/讀取及
		// genTypedArg 中 %arr → [N x T]* 參數轉換使用（缺 arraySizes 會 fallback
		// 為 %arr* 直接傳遞，導致被調用函數寫入 struct 本身而非 data 緩衝區）。
		if at, ok := p.Type.(*parser.ArrayType); ok && at.Elem != nil {
			g.arrayElemTypes[p.Name] = g.mapToLLVMType(at.Elem.String())
			if v, ok := g.constFoldInt(at.Size); ok {
				g.arraySizes[p.Name] = v
			} else if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
				g.arraySizes[p.Name] = intLit.Value
			}
		}
		// 切片型輸入參數也需註冊元素型別，供 IndexExpression 使用正確型別
		if st, ok := p.Type.(*parser.SliceType); ok && st.Elem != nil {
			g.arrayElemTypes[p.Name] = g.mapToLLVMType(st.Elem.String())
		}
		// 泛型替換後的切片型別：substituteType 將 NamedType{Value:"v"} 替換為
		// NamedType{Value:"[]str"}，但此時 p.Type 是 NamedType 而非 SliceType。
		// 需從字串表示中解析切片元素型別，否則 arrayElemTypes 不會被註冊，
		// 導致 put 方法深拷貝 val 時用預設 8 字節元素大小（而非 24），
		// 造成資料截斷、get 找不到 key → 回傳 nil → match ok arm 空指標崩潰。
		if nt, ok := p.Type.(*parser.NamedType); ok && strings.HasPrefix(nt.Value, "[]") {
			elemStr := nt.Value[2:]
			g.arrayElemTypes[p.Name] = g.mapToLLVMType(elemStr)
		}
	}
	// 結果參數（無論單結果或多結果）皆以 by-reference 形式傳遞，
	// 與 call.go 的 hasOutputParam / voidSingleOutput 約定保持一致。
	for _, r := range fd.Results {
		if r.Name != "" {
			g.paramNames[r.Name] = true
			g.funcLocalNames[r.Name] = true
			g.funcParams[r.Name] = true
			g.outputParamNames[r.Name] = true
			g.outputParamOrder = append(g.outputParamOrder, r.Name)
			// MapType: use LLVMName() to avoid [K]V matching the array branch.
			var typeStr string
			if mt, ok := r.Type.(*parser.MapType); ok {
				typeStr = mt.LLVMName()
			} else {
			typeStr = r.Type.String()
		}
			g.varTypes[r.Name] = g.resolveOutputParamLLVMType(r.Type)
			if strings.HasPrefix(typeStr, "?") {
				innerTypeStr := typeStr[1:] // strip '?'
				g.optionInnerTypes[r.Name] = g.mapToLLVMType(innerTypeStr)
				// ?[]T: option inner type is %vec; also register the vec element type
				// so that copyToData can correctly deep-clone the vec when boxing
				// (e.g. result = .vals[idx] where result is ?[]str).
				if strings.HasPrefix(innerTypeStr, "[]") {
					elemTy := g.mapToLLVMType(innerTypeStr[2:])
					if g.arrayElemTypes == nil {
						g.arrayElemTypes = make(map[string]string)
					}
					g.arrayElemTypes[r.Name] = elemTy
				}
			}
			// 陣列型結果參數的 LLVM 簽名為 [N x T]*（resolveParamLLVMType 回傳 [N x T]）。
			// 註冊元素型別與大小，供索引賦值/讀取及 IndexExpression 使用正確型別。
			// 注意：out 參數為 [N x T]*（原始陣列指標），而非 %arr*（struct 指標），
			// 故不可覆蓋 varTypes 為 %arr。
			if at, ok := r.Type.(*parser.ArrayType); ok && at.Elem != nil {
				g.arrayElemTypes[r.Name] = g.mapToLLVMType(at.Elem.String())
				if v, ok := g.constFoldInt(at.Size); ok {
					g.arraySizes[r.Name] = v
				} else if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					g.arraySizes[r.Name] = intLit.Value
				}
			}
			// 切片型結果參數也需註冊元素型別，供 IndexExpression 使用正確型別
			if st, ok := r.Type.(*parser.SliceType); ok && st.Elem != nil {
				g.arrayElemTypes[r.Name] = g.mapToLLVMType(st.Elem.String())
			}
			// out 參數加入 heapVars（用於 freeOldHeapValue 釋放賦值時的舊值）。
			// 不加入 heapVarIndex（不參與 move bitmap），函數結束 emitHeapFree 仍跳過 out。
			if llvmType := g.varTypes[r.Name]; g.isHeapOwningType(llvmType) || llvmType == "%option" {
				if g.heapVars == nil {
					g.heapVars = make(map[string]string)
				}
				g.heapVars[r.Name] = llvmType
			}
		}
	}

	// 初始化返回值延遲零值追蹤：每個 out 參數名 → bit index（與 outputParamOrder 對齊）。
	// 僅當存在 out 參數時啟用 hasRetInitCheck，後續 prologue 配置 bitmap、return 時補零。
	if len(g.outputParamOrder) > 0 {
		g.retInitBits = make(map[string]int, len(g.outputParamOrder))
		for i, name := range g.outputParamOrder {
			g.retInitBits[name] = i
		}
		g.hasRetInitCheck = true
	}

	returnType := "void"
	// All Nolang functions with results use void + pointer-passing convention:
	// multi-result: each result passed by reference as an additional parameter
	// single-result: also passed by reference (for consistency with multi-result
	// and to support struct types like %option that can't be returned by value
	// in all contexts)
	if len(fd.Results) == 1 {
		_ = g.mapToLLVMType(fd.Results[0].Type.String()) // result type used in pointer param
	}

	g.curFuncRetType = returnType
	g.curFuncRetName = ""
	if len(fd.Results) == 1 && fd.Results[0].Name != "" {
		g.curFuncRetName = fd.Results[0].Name
	}

	// Rename user-defined main to _nolang_main to avoid conflict with the
	// C entry point wrapper main(i32, i8**) generated by generateMainFunction.
	// 為避免與 clib 系統調用（@open、@read、@write、@close、@mkdir、@unlink、
	// @rename、@stat、@chdir 等）衝突，僅在用戶函數名稱命中 clibFuncNames 時
	// 加 "n." 前綴；其他情況保留原名以維持與 builtin 的 dispatch 優先級。
	llvmFnName := fd.Name
	if clibFuncNames[fd.Name] {
		llvmFnName = "n." + fd.Name
	}
	if fd.Name == "main" {
		llvmFnName = "_nolang_main"
	}

	// Use internal linkage + alwaysinline for user-defined functions so LLVM
	// can inline them at -O2/-O3. This is critical for performance: without
	// inlining, every function call has overhead and prevents cross-function
	// optimizations (e.g., constant propagation, DCE).
	// Skip for main (renamed to _nolang_main, called from C wrapper).
	isMain := fd.Name == "main"
	linkage := "internal"
	if isMain {
		linkage = ""
	}
	if linkage != "" {
		sb.WriteString(fmt.Sprintf("define %s %s @%s(", linkage, returnType, sanitizeLLVMName(llvmFnName)))
	} else {
		sb.WriteString(fmt.Sprintf("define %s @%s(", returnType, sanitizeLLVMName(llvmFnName)))
	}

	for i, param := range fd.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		// 引用傳遞：參數為指標 i64* %n（函式型別參數為 void (...)** %n）
		llvmType := toLLVMType(g.resolveParamLLVMType(param.Type)) + "*"
		sb.WriteString(fmt.Sprintf("%s %s", llvmType, llvmVarRef(param.Name)))
	}
	// 結果參數（單結果或多結果）以指標形式附加到參數列表
	firstResult := true
	for _, r := range fd.Results {
		if r.Name != "" {
			llvmType := toLLVMType(g.resolveOutputParamLLVMType(r.Type)) + "*"
			sep := ", "
			if firstResult && len(fd.Parameters) == 0 {
				sep = "" // 第一個參數前不需逗號
			}
			sb.WriteString(fmt.Sprintf("%s%s %s", sep, llvmType, llvmVarRef(r.Name)))
			firstResult = false
		}
	}

	sb.WriteString(")")
	// Add optnone noinline for functions with large struct types containing inline arrays
	// (e.g., json.json.parse with [64]json-value). The deep clone/free patterns in these
	// functions trigger LLVM -O3 constant propagation bugs that produce invalid free
	// targets (e.g., free(-16)). optnone disables optimization for the function body,
	// while llc still generates optimized machine code.
	if g.hasInlineArrayStructParam(fd) {
		sb.WriteString(" noinline optnone")
	}
	sb.WriteString(" {\n")
	g.indentLevel++
	g.emitLabel(sb, "entry")
	g.indentLevel++

	// 初始化 CFG 用于数据流分析（MovedFact must/may 分析）。
	// entry block 已由 emitLabel 注册，设置 Entry 指向它。
	g.curCFG = newFuncCFG()
	g.curCFG.Entry = "entry"
	g.curCFG.getOrCreateBlock("entry")
	g.cfgMovedFacts = nil
	g.cfgAssignedFacts = nil

	// 收集所有變數（一次分配），排除參數（已是指標）
	localVarTypes := make(map[string]string)
	// Reset itAllocTypes per function to prevent type leakage from prior functions
	g.itAllocTypes = make(map[string]string)
	g.collectVarDeclsFromStmt(fd.Body, localVarTypes)
	for k, v := range localVarTypes {
		g.varTypes[k] = v
		g.funcLocalNames[k] = true
	}
	// 確保 range 變數有型別
	g.collectRangeVarTypes(fd.Body, localVarTypes)
	for k, v := range localVarTypes {
		g.varTypes[k] = v
		g.funcLocalNames[k] = true
	}
	// 結果參數為 passed by reference（單結果與多結果皆同），不分配本地 alloca。
	for _, r := range fd.Results {
		if r.Name != "" {
			g.varTypes[r.Name] = g.resolveOutputParamLLVMType(r.Type)
			g.funcLocalNames[r.Name] = true
			// Register slice element type for []T result params (e.g. []str → "%str-long").
			// Without this, vec-push defaults to i64 (8 bytes) instead of %str-long (24 bytes),
			// causing heap corruption and segfaults in functions like str.split that push to []str results.
			if st, ok := r.Type.(*parser.SliceType); ok && st.Elem != nil && g.arrayElemTypes != nil {
				g.arrayElemTypes[r.Name] = g.mapToLLVMType(st.Elem.String())
				// 嵌套容器（[][]T）：注册内层元素型别
				if g.elemElemTypes != nil {
					if innerType := g.nestedElemLLVMType(st.Elem); innerType != "" {
						g.elemElemTypes[r.Name] = innerType
					}
				}
			}
		}
	}

	for _, p := range fd.Parameters {
		delete(localVarTypes, p.Name)
	}
	// 結果參數：均為 by-reference 形式（單結果與多結果），不應作為本地變量分配。
	for _, r := range fd.Results {
		delete(localVarTypes, r.Name)
	}

	// 零初始化 out 參數：out 參數由呼叫者傳入指標，呼叫者可能未初始化
	// （如 resp = http.do(...) 中 resp 為未初始化的棧變數）。
	// out 參數已加入 heapVars，函數內首次賦值（如 resp = nil）會觸發
	// freeOldHeapValue 釋放舊值。若舊值為呼叫者的棧殘值，free 會釋放垃圾指標
	// 導致 heap corruption，後續 malloc 偵測到 corruption 時觸發 trace/BPT trap。
	for _, r := range fd.Results {
		if r.Name == "" {
			continue
		}
		llvmType := g.varTypes[r.Name]
		if llvmType == "%option" {
			// option: tag=1 (nil), data=0
			tagGEP := g.tmpReg("outzero.tag.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n", g.indent(), tagGEP, llvmVarRef(r.Name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 1, i64* %s\n", g.indent(), tagGEP))
			dataGEP := g.tmpReg("outzero.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, llvmVarRef(r.Name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dataGEP))
		} else if g.isHeapOwningType(llvmType) {
			// 用戶結構體/%vec/%str-long/%arr: memset 確保所有 data 指標為 NULL
			sz := g.llvmTypeSize(llvmType)
			if sz > 0 {
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0.i64(ptr %s, i8 0, i64 %d, i1 false)\n", g.indent(), llvmVarRef(r.Name), sz))
			}
		}
	}

	// 分配 + lifetime.start
	// 排序確保輸出順序確定（與 emitHeapFree/emitGlobalHeapFree 一致），
	// 避免 IR 因 map 遍歷順序隨機而不可復現。
	sortedVarNames := make([]string, 0, len(localVarTypes))
	for name := range localVarTypes {
		sortedVarNames = append(sortedVarNames, name)
	}
	sort.Strings(sortedVarNames)
	for _, varName := range sortedVarNames {
		varType := localVarTypes[varName]
		sz := g.llvmTypeSize(varType)
		g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: varType, Size: sz})
		// Record allocated type for synthetic `it` variables so that
		// bitcasts can be generated when the actual type differs (e.g.
		// allocated as %http2-frame but storing/loading i64).
		if varName == "it" && g.itAllocTypes != nil {
			g.itAllocTypes[varName] = varType
		}
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), llvmVarRef(varName), toLLVMType(varType)))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), sz, llvmVarRef(varName)))
		// %str-long 局部變數零初始化：確保 data 指標為 NULL。
		// 若變數在循環/條件塊內賦值但運行時未執行，emitHeapFree 的 NULL 檢查能安全跳過 free。
		// 使用 llvm.memset 而非 store zeroinitializer：後者會被 LLVM opt 的 DSE 移除
		// （opt 看到後續的 = '' 初始化就判定零初始化為死存儲）。但提前 return 路徑
		// 未執行 = '' 賦值時，freeOldHeapValue 仍會讀取 data 指標並嘗試 free。
		if varType == "%str-long" {
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0.i64(ptr %s, i8 0, i64 24, i1 false)\n", g.indent(), llvmVarRef(varName)))
		}
		// 用戶自定義結構體局部變數零初始化：確保所有堆擁有欄位（%str-long、%vec、
		// [N]%str-long 內聯陣列、嵌套結構體）的 data 指標為 NULL。
		// 若函數在所有欄位被明確初始化前提前 return（如 HTTPS 回應解析中狀態碼
		// 解析失敗觸發 return，parse-headers 尚未執行），emitHeapFree 的 NULL
		// 檢查能安全跳過 free，避免釋放 stack 殘值導致 exit 133 崩潰。
		if g.isUserStructType(varType) {
			// 使用 llvm.memset 而非 store zeroinitializer：後者會被 LLVM opt 的
			// DSE（Dead Store Elimination）移除，因為 opt 看到後續對各欄位的 store
			// 就判定零初始化為死存儲。但提前 return 路徑（如狀態碼解析失敗）
			// 未寫入所有欄位，emitHeapFree 仍會讀取並嘗試 free 未初始化的 data
			// 指標。llvm.memset 是 intrinsic call，opt 不會輕易移除，確保所有
			// 堆擁有欄位在函數返回時 data 指標為 NULL，emitHeapFree 的 NULL 檢查
			// 能安全跳過 free。
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0.i64(ptr %s, i8 0, i64 %d, i1 false)\n", g.indent(), llvmVarRef(varName), sz))
		}
		// %vec (slice) 局部變數需要 malloc 資料緩衝區，否則 buf[i] = val 會因 data 為 null 而崩潰。
		// 使用 malloc（而非 alloca）使得資料在函數返回後仍然有效（例如函數輸出 []byte 給呼叫者）。
		// 預分配容量為 4（而非 256）：減少記憶體浪費。push 時自動擴容（cap==0→4, cap<1024→cap*2）。
		// 若變數隨後被 SliceLiteral 賦值（如 v = [1,2,3]），舊 buffer 由 freeOldHeapValue 釋放，
		// 每次函數調用僅浪費 4*elemSize 字節（而非 256*elemSize）。
		if varType == "%vec" {
			vecCap := int64(4)
			elemSize := int64(8) // default i64
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[varName]; ok {
					elemSize = llvmTypeSize(et)
				}
			}
			vecBufSize := vecCap * elemSize
			dataBuf := g.tmpReg("local.vecdata")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
			lenGEP := g.tmpReg("local.veclen.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, llvmVarRef(varName)))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
			capGEP := g.tmpReg("local.veccap.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, llvmVarRef(varName)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
			dataGEP := g.tmpReg("local.vecdata.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, llvmVarRef(varName)))
			g.storeDataPtrField(sb, dataBuf, dataGEP)
			// 追蹤 prologue buffer 為堆變數，使首次賦值（如 v = [1,2,3]）時
			// freeOldHeapValue 能釋放 prologue buffer，避免每次函數調用洩漏 256*elemSize 字節。
			// 若變數從未被賦值，emitHeapFree 會在函數結束時釋放 prologue buffer。
			// trackLocalHeapVar 跳過參數和輸出參數，所以這裡只追蹤真正的局部變數。
			g.trackLocalHeapVar(varName, "%vec")
			// CFG: prologue 已 malloc buffer → 入口即持有堆數據（assigned=true）
			if idx, ok := g.heapVarIndex[varName]; ok {
				g.cfgAddEffect(effect{Kind: effAssign, VarIdx: idx})
			}
		}
		// [N]T 局部陣列需分配資料緩衝區並初始化 len/data，否則 arr[i] = val
		// 會因 data 為未初始化（stack 殘值）而寫入垃圾地址，造成堆損壞。
		// 小尺寸/定寬元素陣列用 alloca（棧分配），避免 malloc/free 開銷與記憶體洩漏。
		if varType == "%arr" {
			arrSize := int64(0)
			if g.arraySizes != nil {
				if sz, ok := g.arraySizes[varName]; ok {
					arrSize = sz
				}
			}
			elemSize := int64(8)
			llvmElemType := "i64"
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[varName]; ok {
					elemSize = g.llvmTypeSize(et)
					llvmElemType = toLLVMType(et)
				}
			}
			totalSize := arrSize * elemSize
			if totalSize <= 0 {
				totalSize = 64
			}
			arrDataBuf := g.tmpReg("local.arrdata")
			if shouldStackAllocArray(llvmElemType, totalSize) {
				// 棧分配：entry block 中的 alloca 不會隨迴圈增長棧，且無需 free
				sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), arrDataBuf, totalSize))
				g.stackArrVars[varName] = true
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), arrDataBuf, totalSize))
				// 僅 malloc 路徑需要追蹤；棧分配的 arr 無需 free。
				// 追蹤目的同 %vec：使首次賦值時 freeOldHeapValue 能釋放 prologue buffer。
				g.trackLocalHeapVar(varName, "%arr")
				// CFG: prologue 已 malloc buffer → 入口即持有堆數據（assigned=true）
				if idx, ok := g.heapVarIndex[varName]; ok {
					g.cfgAddEffect(effect{Kind: effAssign, VarIdx: idx})
				}
			}
			arrLenGEP := g.tmpReg("local.arrlen.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n", g.indent(), arrLenGEP, llvmVarRef(varName)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arrSize, arrLenGEP))
			arrDataGEP := g.tmpReg("local.arrdata.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), arrDataGEP, llvmVarRef(varName)))
			g.storeDataPtrField(sb, arrDataBuf, arrDataGEP)
		}
	}

	// 輸出參數（單結果或多結果）的緩衝區合法性由調用方負責。
	// 函數 prologue 不再自動 malloc 兜底緩衝區，避免：
	//   1. 越權替調用方處理參數生命週期
	//   2. 覆蓋調用方傳入的有效緩衝區指標（導致洩漏）
	//   3. 單/多輸出參數邏輯割裂
	// 調用方在 call.go voidSingleOutput 路徑中分配緩衝區。

	// 參數化為指標（引用傳遞模型）
	for _, param := range fd.Parameters {
		llvmType := g.mapToLLVMType(param.Type.String())
		// 已是指標型別，不需 alloca/store
		// %n 是 i64*，可直接 load
		_ = llvmType
	}

	// 初始化 outBindState：每個 out 參數初始無綁定（-1）。
	if len(g.outputParamOrder) > 0 {
		g.outBindState = make([]int, len(g.outputParamOrder))
		for i := range g.outBindState {
			g.outBindState[i] = -1
		}
	}

	// 預掃描函數體：檢測是否存在分支內 move-to-out（決定是否需要分配位圖變數）。
	// 僅當 move 存在且在分支中（IfExpression/ForStatement/ConditionalExpression）時，
	// 才需要位圖變數做運行時雙重校驗。
	if fd.Body != nil && len(g.outputParamOrder) > 0 {
		g.hasBranchMove = detectBranchMoveToOut(fd.Body.Statements, g.outputParamNames)
	}

	// 預先設置 bitmap 變數名前綴，使函數體生成期間的 emitSetMovedBitIR/emitClearMovedBitIR
	// 能正確生成 IR。實際的 alloca + store 0 在後面根據最終 nextHeapVarIdx 一次性發出。
	// bitmapCount 在函數體生成後根據實際堆變數數量計算。
	if g.hasBranchMove {
		g.movedBitmapBase = "%__mb"
	}

	// 返回值延遲零值追蹤：配置 %__ret_init_bitmap 並初始化為 0。
	// 僅當存在 out 參數時啟用。函數體生成期間的 emitSetRetInitBit 透過此名稱生成 IR。
	// out 參數數量 ≤ 64（函數參數/返回值上限），單一 i64 即可容納所有 bit。
	// 註：out 參數由呼叫方傳入指標並初始化緩衝區，prologue 不對其 store zeroinitializer；
	//     未賦值的 out 參數在 ReturnStatement 由 emitRetInitZeroFill 補零。
	if g.hasRetInitCheck {
		g.retInitBitmapVar = "%__ret_init_bitmap"
		sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), g.retInitBitmapVar))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), g.retInitBitmapVar))
	}

	// Liveness 预分析：判定 b=a 中源变量是否在后续被引用，用于选择 clone/move。
	g.computeMoveEligibility(fd.Body)

	// 生成函數體到獨立緩衝區，同時收集 entry-block alloca（來自字面量參數的臨時變量）。
	// 將 alloca 提升到 entry block 可避免循環體內的 call 參數每次迭代都增長棧，
	// 導致長循環（如 n-body 1000000 次 advance()）棧溢出。
	// 生成過程中 trackLocalHeapVar 會分配 varIdx，結束後 nextHeapVarIdx 為最終值。
	g.entryAllocaBuf = &strings.Builder{}
	bodyBuf := &strings.Builder{}
	if fd.Body != nil {
		for _, stmt := range fd.Body.Statements {
			g.generateStatement(bodyBuf, stmt)
		}
	}

	// 求解數據流分析（MovedFact must/may）。
	// CFG 在函數體生成過程中已通過 cfgEdge/cfgTerm/cfgAddEffect 構建完成。
	// 求解結果存入 cfgMovedFacts，供 emitHeapFree 做三態決策：
	//   triMust → 所有路徑 moved → 靜態跳過 free
	//   triMustNot → 無路徑 moved → 靜態 free
	//   triMay → 部分路徑 moved → 運行時 bitmap 檢查
	if g.curCFG != nil && g.nextHeapVarIdx > 0 {
		g.cfgMovedFacts = solveBitsetForward(
			g.curCFG, g.nextHeapVarIdx,
			newBitsetFact(g.nextHeapVarIdx),
			movedTransfer,
		)
		// 求解 AssignedFact：局部堆變數在出口是否持有本函數擁有的堆數據。
		// 入口 seed：所有「被寫過（LHS）/ vec·arr」的局部在入口即標記為 assigned，
		// 確保它們恆為 triMust，絕不會被 Phase 2 誤判為「從未持有堆」而跳過 free。
		// vec·arr 由 prologue entry effAssign 覆蓋，此處一併 seed 以求穩健。
		assignedEntry := g.computeAssignedSeed(fd)
		g.cfgAssignedFacts = solveBitsetForward(
			g.curCFG, g.nextHeapVarIdx,
			assignedEntry,
			assignedTransfer,
		)
	}

	// 分配運行時 bitmap（僅當存在分支內 move 且有局部堆變數時）：
	// bitmap 的每個 bit 對應一個局部堆變數的 varIdx。
	// 一塊 u64 存 64 個標記位；塊號 = varIdx / 64，塊內偏移 = varIdx % 64。
	// 僅當 move 在分支中（無法靜態推斷）時才分配：
	//   - 無 move → 全部 free，不需位圖
	//   - move 不在分支 → move 綁定確定，編譯期 movedVarBitset 跳過 free，不需位圖
	//   - move 在分支 → 需位圖運行時判斷 move 是否實際發生
	if g.hasBranchMove && g.nextHeapVarIdx > 0 {
		g.bitmapCount = (g.nextHeapVarIdx + 63) / 64
		// movedBitmapBase 已在函數體生成前預先設置
		for i := 0; i < g.bitmapCount; i++ {
			sb.WriteString(fmt.Sprintf("%s%s%d = alloca i64\n", g.indent(), g.movedBitmapBase, i))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s%d\n", g.indent(), g.movedBitmapBase, i))
		}
	}

	// 先寫入 entry-block alloca（在所有局部變量 alloca 之後、函數體之前）
	sb.WriteString(g.entryAllocaBuf.String())
	// 再寫入函數體
	sb.WriteString(bodyBuf.String())

	// 所有 Nolang 函數均採用 void 返回 + 指針傳參約定（returnType 恆為 "void"）。
	// 函數體無顯式 return 時自動銷毀局部堆變數並返回。
	g.emitRetInitZeroFill(sb)
	g.flushOutputBindings(sb)
	g.emitLocalTasksFree(sb)
	g.emitHeapFree(sb)
	g.emitLifetimeEnd(sb)
	sb.WriteString(g.indent() + "ret void\n")
	g.indentLevel--
	g.indentLevel--
	sb.WriteString("}\n\n")
}

func (g *Generator) generateMainFunction(sb *strings.Builder, program *parser.Program) {
	// 創建全新 funcState 實例：消除手動重置，防止遺漏導致跨函數污染
	g.resetFuncState()
	// main returns i32 but has no named result parameter. We keep
	// curFuncRetType = "void" so if-expression PHI nodes default to i64
	// (the standard integer type). The inMainFunction flag lets the
	// ReturnStatement handler emit `ret i32 0` for bare returns in main.
	// Without resetting curFuncRetName, it leaks from the last processed
	// function (e.g. "yes"), causing conditional expressions at module
	// level to emit `load i64, i64* %yes` for a non-existent variable.
	g.inMainFunction = true
	g.curFuncRetName = ""
	g.curFuncRetType = "void"

	// 恢復模組級 option 變數的 inner type 備份，
	// 讓 main 函數內 top-level option 變數的 inner type 可被查詢
	// （expr.go / call.go 等多處讀取 optionInnerTypes）。
	if g.moduleOptionInnerTypes != nil {
		for k, v := range g.moduleOptionInnerTypes {
			g.optionInnerTypes[k] = v
		}
	}

	hasTopLevel := false
	for _, stmt := range program.Statements {
		switch stmt.(type) {
		case *parser.FunctionDefinition, *parser.StructDefinition, *parser.TypeAlias:
			continue
		default:
			hasTopLevel = true
			break
		}
		if hasTopLevel {
			break
		}
	}

	if !hasTopLevel {
		return
	}

	// 模块级代码含顶层 awy 时，包装为匿名 async 函数（无栈协程状态机变换），
	// @main 仅创建 coro_state + task 并启动 @nolang_async_run 事件循环。
	// 此路径下不再生成普通 @main 函数体。
	if g.transformModuleAsync(sb, program) {
		return
	}

	// Check if user-defined main function exists (renamed to _nolang_main)
	hasUserMain := false
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == "main" {
			hasUserMain = true
			break
		}
	}

	// WASI 平台入口函數名必須為 __main_argc_argv：wasi-libc 的 _start 呼叫
	// __main_void（crt1-command.o），後者初始化 argv 後呼叫 weak symbol
	// __main_argc_argv。若用戶 main 仍命名為 @main，__main_argc_argv 為未定義
	// weak symbol，呼叫會跳轉到 unreachable trap。原生平台仍使用 @main。
	mainName := "main"
	if g.goos() == "wasi" {
		mainName = "__main_argc_argv"
	}
	sb.WriteString(fmt.Sprintf("define i32 @%s(i32 %%c-argc, i8** %%c-argv) {\n", mainName))
	g.indentLevel++
	g.emitLabel(sb, "entry")
	g.indentLevel++

	// Store argc/argv into globals for use by args-count / args-get builtins.
	// These globals are declared in decl.go and accessible from any function.
	sb.WriteString(fmt.Sprintf("%sstore i32 %%c-argc, i32* @.argc.addr\n", g.indent()))
	sb.WriteString(fmt.Sprintf("%sstore i8** %%c-argv, i8*** @.argv.addr\n", g.indent()))

	g.emitLifetimeEnd(sb)

	// Pre-allocate all top-level local variables (not globals, not functions)
	// This is needed because CallExpression values use the variable's address
	// as an output parameter before generateLet would allocate it.
	if g.moduleVarTypes != nil {
		// Sort keys for deterministic alloca ordering (Go map iteration is randomized).
		// Non-deterministic alloca order causes LLVM opt -O3 to make different
		// optimization decisions across builds, leading to intermittent issues.
		sortedNames := make([]string, 0, len(g.moduleVarTypes))
		for name := range g.moduleVarTypes {
			sortedNames = append(sortedNames, name)
		}
		sort.Strings(sortedNames)
		for _, name := range sortedNames {
			varType := g.moduleVarTypes[name]
			if g.globalVars != nil && g.globalVars[name] {
				continue
			}
			if g.funcRefVars != nil && g.funcRefVars[name] {
				continue
			}
			if varType == "" || (!g.isStructLLVMType(varType) && !g.isIntegerLLVMType(varType) && varType != "double" && varType != "i1" && !strings.HasSuffix(varType, "*")) {
				// Skip complex types that need special allocation (handled by generateLet)
				continue
			}
			if !g.funcLocalNames[name] {
				g.funcLocalNames[name] = true
				sz := g.llvmTypeSize(varType)
				if sz == 0 {
					sz = 8
				}
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), llvmVarRef(name), toLLVMType(varType)))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), sz, llvmVarRef(name)))
				// Zero-initialize heap-owning types (str-long, vec, arr, option,
				// user structs) so that emitHeapFree's null-check on the data
				// pointer works correctly even when the variable is never
				// assigned (e.g. in an untaken match/if branch). Without this,
				// the data field contains stack garbage, and free(garbage)
				// causes SIGABRT.
				// Use llvm.memset (not store zeroinitializer) to avoid LLVM opt's
				// DSE removing the zero-init as a dead store.
				if g.isHeapOwningType(varType) && sz > 0 {
					sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0.i64(ptr %s, i8 0, i64 %d, i1 false)\n", g.indent(), llvmVarRef(name), sz))
				}
			}
		}
	}

	// 生成 top-level 語句到獨立緩衝區，同時收集 entry-block alloca。
	// 與 generateFunctionDefinition 相同的修復：避免循環體內 call 參數的 alloca 每次迭代增長棧。
	g.entryAllocaBuf = &strings.Builder{}
	bodyBuf := &strings.Builder{}
	// Generate top-level statements (e.g. h = crc-32('', 0), test-str-len(), print(0))
	// Skip calls to user-defined main() when hasUserMain is true, since _nolang_main()
	// already calls the user's main. Otherwise we get infinite recursion.
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			// Embed vars are emitted as statically initialized globals; skip runtime init.
			if g.sem.EmbedDataOf(ls) != nil {
			// Directory embed vars are also statically initialized globals; skip runtime init.
			if g.sem.EmbedFilesOf(ls) != nil {
				continue
			}
				continue
			}
			// Skip LetStatements already emitted as globals, EXCEPT for:
			// - string/array types (need runtime init via generateLet)
			// - reassigned variables (need store instruction for new value)
			if g.globalVars != nil && g.globalVars[ls.Name.Value] {
				// Reassigned global variables (e.g. h0 in SHA tests) must generate
				// a store instruction — don't skip them.
				if g.reassignedVars != nil && g.reassignedVars[ls.Name.Value] {
					// Fall through to generateLet
				} else {
					lt := g.varLLVMType(ls)
					// For assignments (Type=nil), also check the variable's declared type
					if ls.Type == nil && g.varTypes != nil {
						if t, ok := g.varTypes[ls.Name.Value]; ok {
							lt = t
						}
					}
					// 原始陣列類型（如 [3 x i64]）也需要生成初始化代碼
					// （如 b = a.clone() 返回 [N]T 時，b 的類型是 [N x T] 而非 %arr）
					if lt != "%str-long" && lt != "%arr" && lt != "%vec" && !strings.HasPrefix(lt, "[") {
						continue
					}
				}
			}
			// Skip function-typed LetStatements (already collected as functions)
			if g.funcRefVars != nil && g.funcRefVars[ls.Name.Value] {
				continue
			}
			g.generateLet(bodyBuf, ls)
		}
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			if hasUserMain {
				if call, ok := es.Expression.(*parser.CallExpression); ok {
					if ident, ok := call.Function.(*parser.Identifier); ok && ident.Value == "main" {
						continue
					}
				}
			}
			g.generateExpressionStmt(bodyBuf, es)
		}
		if fs, ok := stmt.(*parser.ForStatement); ok {
			g.generateForStatement(bodyBuf, fs)
		}
	}
	// 先寫入 entry-block alloca，再寫入函數體（模組級語句）。
	// 模組級語句包含所有導入模組的初始化（如 aes.no 的 SBOX），
	// 必須在使用者 main 函數之前執行，否則 main 內呼叫的函數會存取未初始化的全域變數。
	sb.WriteString(g.entryAllocaBuf.String())
	sb.WriteString(bodyBuf.String())

	// 使用者 main 函數必須在所有模組級初始化完成後才呼叫，
	// 以確保 main 內呼叫的函數能正確存取已初始化的全域變數（如 SBOX）。
	if hasUserMain {
		// _nolang_main 的輸出參數（如 main = () (out i64)）以指標形式附加到參數列表。
		// @main 必須為每個輸出參數分配棧空間並傳遞指標，否則 LLVM -O3 在 inline 後
		// 會將缺少的參數視為 null（UB），導致 store i64 0, ptr null → unreachable →
		// 全局變數釋放和 ret i32 0 被優化器刪除，程序在 _nolang_main 返回後立即崩潰。
		mainArgs := []string{}
		if g.funcResultLLVMType != nil {
			if retTypes, ok := g.funcResultLLVMType["main"]; ok && len(retTypes) > 0 {
				for _, rt := range retTypes {
					g.tmpIdx++
					tmpName := fmt.Sprintf("%%main.out.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, toLLVMType(rt)))
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(rt), toLLVMType(rt), tmpName))
					mainArgs = append(mainArgs, toLLVMType(rt)+"* "+tmpName)
				}
			}
		}
		if len(mainArgs) > 0 {
			sb.WriteString(fmt.Sprintf("%scall void @_nolang_main(%s)\n", g.indent(), strings.Join(mainArgs, ", ")))
		} else {
			sb.WriteString(fmt.Sprintf("%scall void @_nolang_main()\n", g.indent()))
		}
	}
	// 釋放 top-level 堆變數（模組級 vec/str/arr 等），避免長期運行服務的記憶體泄漏。
	// 同時釋放 top-level 局部堆變數（非 globalVars 的局部）。
	g.emitLocalTasksFree(sb)
	g.emitHeapFree(sb)
	g.emitGlobalHeapFree(sb)
	sb.WriteString(g.indent() + "ret i32 0\n")
	g.indentLevel--
	g.indentLevel--
	sb.WriteString("}\n\n")
}

// lookupMethodReturnType returns the LLVM return type for a method call,
// looking up both user-defined methods (g.funcRetTypes + g.funcResultLLVMType
// for void + by-reference output) and builtin methods. Returns "" if the
// method is not found or has no determinable return type.
//
// This is used for literal receivers (IntegerLiteral/FloatLiteral/etc.) where
// the method may be either a user-defined Nolang function (e.g. i64.to-str
// implemented in number.no) or a builtin (e.g. f64.to-str).
func (g *Generator) lookupMethodReturnType(shortName string) string {
	if g.funcRetTypes != nil {
		if t, ok := g.funcRetTypes[shortName]; ok {
			if t != "void" {
				return t
			}
			// void + 單輸出函數（如 i64.to-str = () (out str)）：
			// 使用 funcResultLLVMType 中的輸出型別
			if g.funcResultLLVMType != nil {
				if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
					retType := ts[0]
					if retType == "i1" {
						retType = "i64"
					}
					return retType
				}
			}
		}
	}
	if m := builtin.FindBuiltinMethod(shortName); m != nil && len(m.Return) > 0 {
		return g.mapToLLVMType(m.Return[0].String())
	}
	return ""
}

// builtinStructReturnType returns the LLVM struct type (e.g. "%utsname") if the
// builtin method's first return type is a registered struct, otherwise "".
// Used by varLLVMType to infer LHS variable type for calls like `uts = os.uname()`.
// Resolves both the raw name (e.g. "utsname") and module-prefixed variants
// (e.g. "os.utsname") added by prefixModuleStatements.
func (g *Generator) builtinStructReturnType(m *builtin.BuiltinMethod) string {
	if m == nil || len(m.Return) == 0 || g.structTypes == nil {
		return ""
	}
	nt, ok := m.Return[0].(*parser.NamedType)
	if !ok {
		return ""
	}
	// Direct match
	if _, ok := g.structTypes[nt.Value]; ok {
		return "%" + nt.Value
	}
	// Try module-prefixed variants (e.g. "os.utsname")
	suffix := "." + nt.Value
	for name := range g.structTypes {
		if strings.HasSuffix(name, suffix) {
			return "%" + name
		}
	}
	return ""
}

func (g *Generator) varLLVMType(stmt *parser.LetStatement) string {
	// 單具體型別別名解析：若顯式型別為已註冊的具體型別別名，用底層 Type 遞迴解析
	// 使 ArrayType/SliceType 等特殊路徑也能正確套用到底層型別
	if nt, ok := stmt.Type.(*parser.NamedType); ok && g.concreteTypeAliases != nil {
		if underlying, ok := g.concreteTypeAliases[nt.Value]; ok {
			unwrapped := *stmt
			unwrapped.Type = underlying
			return g.varLLVMType(&unwrapped)
		}
	}
	// Option type: ?type
	if _, ok := stmt.Type.(*parser.NullableType); ok {
		return "%option"
	}
	// 結構體
	if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
		if t, ok := g.varTypes[sl.Type]; ok {
			return t
		}
		// Direct match in structTypes
		if _, ok := g.structTypes[sl.Type]; ok {
			return "%" + sl.Type
		}
		// Try module-prefixed variants (e.g. "server" → "server.server")
		// when the bare type name is not directly registered but a
		// module-prefixed version exists. This fixes "unsized type" errors
		// when struct literals use bare names that were prefixed by
		// prefixModuleStatements.
		suffix := "." + sl.Type
		for name := range g.structTypes {
			if strings.HasSuffix(name, suffix) {
				return "%" + name
			}
		}
		return "%" + sl.Type
	}
	// struct 欄位讀取 (e.g. p-local = fp.path)：依 receiver 型別與欄位名稱查詢 LLVM 型別
	// 支援鏈式存取：非 Identifier receiver 透過 exprResultLLVMType 推導
	if dot, ok := stmt.Value.(*parser.DotExpression); ok {
		recvType := g.exprResultLLVMType(dot.Receiver)
		// .len on str → i64 (must check before struct field lookup,
		// but .len should always return i64)
		if dot.Property == "len" && (recvType == "%str-long") {
			return "i64"
		}
		if g.isStructLLVMType(recvType) {
			structName := strings.TrimPrefix(recvType, "%")
			if fields, ok := g.structTypes[structName]; ok {
				for _, f := range fields {
					if f.name == dot.Property {
						return f.typ
					}
				}
			}
		}
		return "i64"
	}
	// 陣列/切片
	if at, ok := stmt.Type.(*parser.ArrayType); ok {
		// Nested array type (e.g. [12][16]i64): use raw LLVM array type [12 x [16 x i64]]
		if _, isNested := at.Elem.(*parser.ArrayType); isNested {
			return g.arrayTypeToLLVM(at)
		}
		return "%arr"
	}
	if _, ok := stmt.Type.(*parser.SliceType); ok {
		return "%vec"
	}
	// 映射表：m [K]V — use LLVMName() because String() returns [K]V which is
	// indistinguishable from [N]T arrays in string-based dispatch.
	if mt, ok := stmt.Type.(*parser.MapType); ok {
		return g.mapToLLVMType(mt.LLVMName())
	}
	// 顯式型別註釋（如 n i32 = 0 或 a i8）：優先使用型別而非從值推斷
	if stmt.Type != nil {
		ts := stmt.Type.String()
		// 對於基本型別別名（如 i8 同時是 byte/char），使用最精確的 LLVM 型別
		mapped := g.mapToLLVMType(ts)
		if mapped != "" {
			return mapped
		}
	}
	switch v := stmt.Value.(type) {
	case *parser.Identifier:
		// Look up the type of the source variable
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				// When the source is an %option (e.g. `it` from a match block),
				// prefer the target variable's existing type, since the option's
				// inner value has already been extracted by generateExprWithSB.
				if t == "%option" && stmt.Name != nil {
					// 合成 it 綁定：優先用 source option 的 inner 型別。
					// 共享 it alloca 的型別（varTypes[it]）可能被其它 match 的
					// err 臂（%str-long）或更大 struct 污染；ok 臂真正提取的值
					// 型別由 optionInnerTypes[source] 決定（如 ?i64 → i64），
					// 用 alloca 型別會生成 `load %str-long, %str-long* <i64值>`
					// 之類的非法 IR。指標型別差異由 itAllocTypes bitcast 機制吸收。
					if stmt.IsSynthetic && g.optionInnerTypes != nil {
						if inner, ok := g.optionInnerTypes[v.Value]; ok && inner != "" {
							return inner
						}
						// optionInnerTypes is not populated for the source variable.
						// This can happen when the source was assigned from a function
						// call whose return type couldn't be resolved at collectVarDecls
						// time (e.g. cross-module function name mismatch).
						//
						// Do NOT fall back to g.varTypes[stmt.Name.Value] (i.e. the
						// shared `it` variable's existing type). That type may have been
						// set by a previous match arm with a different element type
						// (e.g. %ws.server-conn from a ?ws.server-conn match), leading
						// to type contamination: the ok arm generates i64 extraction
						// (matching generateExprWithSB's default for unknown options),
						// but the alloca is typed as the wrong struct, producing
						// "use of undefined value '%it'" or type-mismatched IR.
						//
						// Instead, return "i64" — the same default that
						// generateExprWithSB uses when optionInnerTypes is not
						// populated. The itAllocTypes bitcast mechanism handles any
						// alloca-type vs. actual-type difference. And the collectVarDecls
						// max-size logic ensures the alloca is large enough if a
						// previous match already set it to a larger type.
						return "i64"
					}
				}
				return t
			}
		}
		return "i64"
	case *parser.StringLiteral:
		return "%str-long"
	case *parser.CharLiteral:
		return "i32"
	case *parser.RegexLiteral:
		return "%regexp"
	case *parser.InfixExpression:
		// 位元組算術（單字元 StringLiteral 配非字串運算元，如 c - 'A'）應推導為整數
		// 型別，而非 %str-long。需在字串相接檢查之前判斷。
		if v.Operator == "-" || v.Operator == "+" {
			if (isSingleCharStringLit(v.Left) && !g.isStringExpr(v.Right)) ||
				(isSingleCharStringLit(v.Right) && !g.isStringExpr(v.Left)) {
				// 落入整數算術推導路徑（intExprLLVMType 回傳 i64）
				if ft := g.floatLLVMType(v); ft != "" {
					return ft
				}
				return g.intExprLLVMType(v)
			}
		}
		if (v.Operator == "-" || v.Operator == "+" || v.Operator == "*") && (g.isStringExpr(v.Left) || g.isStringExpr(v.Right)) {
			return "%str-long"
		}
		// floatLLVMType 已處理混合 float/double 的型別提升
		if ft := g.floatLLVMType(v); ft != "" {
			return ft
		}
		// 整數算術：使用 intExprLLVMType 推斷正確的整數寬度
		return g.intExprLLVMType(v)
	case *parser.SliceLiteral:
		return "%vec"
	case *parser.ArrayLiteral:
		return "%arr"
	case *parser.IndexExpression:
		// s = arr2d[i] — when arr2d has a raw LLVM array type like [12 x [16 x i64]],
		// the result is a pointer to the element row: [16 x i64]*
		if ident, ok := v.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok && strings.HasPrefix(t, "[") {
					elemType := extractArrayElemType(t)
					if elemType != "" {
						// Only return a pointer for nested array elements
						// (e.g. arr2d[i] where arr2d is [12 x [16 x i64]] → [16 x i64]*).
						// For scalar elements (e.g. [4 x i64] → i64), the value is
						// loaded, not a pointer.
						if strings.HasPrefix(elemType, "[") {
							return elemType + "*"
						}
						return elemType
					}
				}
				// 切片/陣列元素讀取（如 r = records[i] 其中 records []dns-record）：
				// 依 arrayElemTypes 推導元素型別。struct 元素返回值型別
				// （generateIndexExpression 對 vec/arr 載入元素值而非指標）；
				// 基本型別（i8 等）保持 i64（generateIndexExpression 會 zext i8 → i64）。
				// 注意：必須先檢查變數實際型別是否為 str-long（字串），避免被
				// moduleArrayElemTypes 中同名變數（如 spectral-norm 的 u []f64）污染。
				// 例如 http2-do 中 u = url 使 u 成為 str-long 區域變數，但
				// moduleArrayElemTypes["u"] 可能是 "double"（來自 spectral-norm main），
				// 導致 u[i] 被誤判為 double 比較，實際應為 i64（字串索引 zext 結果）。
				if actualType, ok := g.varTypes[ident.Value]; ok && actualType == "%str-long" {
					return "i64"
				}
				if g.arrayElemTypes != nil {
					if elemType, ok := g.arrayElemTypes[ident.Value]; ok {
						if g.isStructLLVMType(elemType) {
							return elemType
						}
						// 浮點數陣列元素（如 [15]f64 → double）必須返回 double/float，
						// 否則 generateLet 會誤判為 i64 並套用 sitofp 於已是 double 的值
						if elemType == "double" || elemType == "float" {
							return elemType
						}
						// 基本型別仍回 i64（byte 讀取在 codegen 中 zext 為 i64）
						return "i64"
					}
				}
			}
		}
		// struct.field[i] — 推導欄位陣列/切片的元素型別
		// （如 .vals[idx] 其中 vals 為 [256 x %str-long] 或 []str）
		if dot, ok := v.Left.(*parser.DotExpression); ok {
			recvName := ""
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				recvName = ident.Value
			}
			if recvName != "" && g.varTypes != nil {
				if t, ok := g.varTypes[recvName]; ok {
					structName := strings.TrimPrefix(t, "%")
					if fields, ok := g.structTypes[structName]; ok {
						for _, f := range fields {
							if f.name != dot.Property {
								continue
							}
							// Inline array field: [N x T]
							if strings.HasPrefix(f.typ, "[") {
								closeB := strings.IndexByte(f.typ, ']')
								if closeB > 0 {
									inner := f.typ[1:closeB]
									xIdx := strings.LastIndex(inner, " x ")
									if xIdx >= 0 {
										elemType := inner[xIdx+3:]
										if g.isStructLLVMType(elemType) {
											return elemType
										}
										return "i64"
									}
								}
								continue
							}
							// Slice field: %vec with element type recorded in elemType.
							// generateIndexExpression loads the actual element value
							// (e.g. %str-long for []str), so varLLVMType must match
							// to avoid `store i64 %str-long-val, i64* %var` mismatches.
							if f.typ == "%vec" && f.elemType != "" {
								if g.isStructLLVMType(f.elemType) {
									return f.elemType
								}
								if f.elemType == "double" || f.elemType == "float" {
									return f.elemType
								}
								return "i64"
							}
						}
					}
				}
			}
		}
		return "i64"
	case *parser.SliceExpression:
		// Check source type: slicing %str-long produces %str-long, otherwise %vec
		if ident, ok := v.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok && (t == "%str-long") {
					return "%str-long"
				}
			}
		}
		return "%vec"
	case *parser.CastExpression:
		// `expr as Type`: use the target type's LLVM mapping
		if v.Type != nil {
			t := g.mapToLLVMType(v.Type.String())
			if t != "" {
				return t
			}
		}
		return "i64"
	case *parser.CallExpression:
		// -async 函数调用返回 %future（惰性，未执行）
		if g.isAsyncCall(v) {
			if _, _, resultType := g.resolveAsyncCallInfo(v); resultType != "" {
				if g.futureResultTypes != nil && stmt != nil && stmt.Name != nil {
					g.futureResultTypes[stmt.Name.Value] = resultType
				}
			}
			return "%future"
		}
		if ident, ok := v.Function.(*parser.Identifier); ok {
			name := ident.Value
			// FFI extern 函式：依 extern 宣告的 result 型別推斷 Nolang 儲存型別。
			// callExtern 會將 str 構造為 %str-long、ptr/pptr/ppptr/i32/bool 轉為 i64、f64 保持 double。
			if g.externFuncs != nil {
				if ext, ok := g.externFuncs[name]; ok && len(ext.ResultTypes) > 0 {
					return ffiTypeToNolangStorage(ext.ResultTypes[0])
				}
			}
			// Option constructors: val(...), err(...), ok(...) return %option
			if name == "val" || name == "err" || name == "ok" {
				return "%option"
			}
			// with-cap / with-len / with-cap-len: type inferred from LHS type annotation
			if name == "with-cap" || name == "with-len" || name == "with-cap-len" {
				if stmt.Type != nil {
					ts := stmt.Type.String()
					if ts == "str" {
						return "%str-long"
					}
					// Slice types ([]i64, []byte, etc.) → %vec
					return "%vec"
				}
				// Reassignment: infer from existing var type
				if g.varTypes != nil && stmt.Name != nil {
					if t, ok := g.varTypes[stmt.Name.Value]; ok {
						if t == "%str-long" {
							return "%str-long"
						}
						return t
					}
				}
				return "%vec"
			}
			strFns := map[string]bool{
				"int.to-str": true,
				"i64.to-str": true, "i32.to-str": true, "i16.to-str": true, "i8.to-str": true,
				"u64.to-str": true, "u32.to-str": true, "u16.to-str": true, "u8.to-str": true,
				"f64.to-str": true, "f32.to-str": true,
				"bool.to-str": true, "byte.to-str": true, "char-to-str": true,
				"get-env": true, "get-wd": true, "host-name": true,
			}
			if strFns[name] {
				return "%str-long"
			}
			// Check if the function is a builtin that returns f64 or str.
			// Note: bool-returning builtins intentionally fall through to "i64"
			// because their IR emits `zext i1 to i64`, producing an i64 SSA
			// value that the int-coercion path (trunc i64 to i1) handles correctly.
			if m := builtin.FindBuiltinMethod(name); m != nil && len(m.Return) > 0 {
				if m.Return[0] == parser.TypeF64 {
					return "double"
				}
				if m.Return[0] == parser.TypeStr {
					return "%str-long"
				}
			// Slice-returning builtins (e.g. read-file → []byte)
			if _, isSlice := m.Return[0].(*parser.SliceType); isSlice {
				return "%vec"
			}
				// Struct-returning builtins (e.g. uname → utsname)
				if structTy := g.builtinStructReturnType(m); structTy != "" {
					return structTy
				}
			}
			// Check funcRetTypes for non-builtin functions (e.g. module functions like degrees)
			if g.funcRetTypes != nil {
				if t, ok := g.funcRetTypes[name]; ok && t != "void" {
					return t
				}
				// Fallback: try short name without module prefix (e.g. "http.do-req" → "do-req")
				if idx := strings.Index(name, "."); idx >= 0 {
					shortName := name[idx+1:]
					if t, ok := g.funcRetTypes[shortName]; ok && t != "void" {
						return t
					}
				}
			}
			// 用戶自定義函數使用 void + by-reference 輸出約定，
			// funcRetTypes 為 "void" 但 funcResultLLVMType 仍保留語意型別。
			// 對於單結果函數，從 funcResultLLVMType 取得輸出型別。
			if g.funcNumResults != nil {
				// Try full name and short name (without module prefix)
				lookupNames := []string{name}
				if idx := strings.Index(name, "."); idx >= 0 {
					lookupNames = append(lookupNames, name[idx+1:])
				}
				for _, lookupName := range lookupNames {
					if n, ok := g.funcNumResults[lookupName]; ok && n == 1 {
						if g.funcResultLLVMType != nil {
							if ts, ok := g.funcResultLLVMType[lookupName]; ok && len(ts) == 1 {
								retType := ts[0]
								if retType == "i1" {
									retType = "i64"
								}
								return retType
							}
						}
					}
				}
			}
		}
		// DotExpression receiver call (e.g. s.contains, s.index): look up str.<method>
		if dot, ok := v.Function.(*parser.DotExpression); ok {
			// Module-prefixed builtin call (e.g. fs.get-line, fs.is-file):
			// if the receiver is an Identifier not in varTypes (i.e., a module name),
			// look up the property as a builtin method for return type inference.
			if recvIdent, ok := dot.Receiver.(*parser.Identifier); ok {
				_, isVar := g.varTypes[recvIdent.Value]
				if !isVar {
					// First check user-defined functions with module prefix
					fullName := recvIdent.Value + "." + dot.Property
					// Also try short name without module prefix (e.g. "http.do-req" → "do-req")
					shortName := dot.Property
					if g.funcRetTypes != nil {
						if t, ok := g.funcRetTypes[fullName]; ok && t != "void" {
							return t
						}
						if t, ok := g.funcRetTypes[shortName]; ok && t != "void" {
							return t
						}
					}
					// User-defined functions use void + by-reference convention:
					// funcRetTypes is "void" but funcResultLLVMType holds the semantic type.
					// For single-result functions (e.g. tls.server-accept → ?server-conn),
					// resolve the LLVM return type from funcResultLLVMType.
					// Without this, module-prefixed option-returning calls default to i64,
					// causing type mismatches when the option value is used.
					if g.funcNumResults != nil {
						// Try full name first, then short name
						for _, lookupName := range []string{fullName, shortName} {
							if n, ok := g.funcNumResults[lookupName]; ok && n == 1 {
								if g.funcResultLLVMType != nil {
									if ts, ok := g.funcResultLLVMType[lookupName]; ok && len(ts) == 1 {
										retType := ts[0]
										if retType == "i1" {
											retType = "i64"
										}
										return retType
									}
								}
							}
						}
					}
				// Then check builtins (strip module prefix)
				if m := builtin.FindBuiltinMethod(dot.Property); m != nil && len(m.Return) > 0 {
					if m.Return[0] == parser.TypeF64 {
						return "double"
					}
					if m.Return[0] == parser.TypeStr {
						return "%str-long"
					}
					// Slice-returning builtins (e.g. fs.read-file → []byte)
					if _, isSlice := m.Return[0].(*parser.SliceType); isSlice {
						return "%vec"
					}
					// Struct-returning builtins (e.g. os.uname → utsname)
					if structTy := g.builtinStructReturnType(m); structTy != "" {
						return structTy
					}
				}
				}
			}
			recvExpr := dot.Receiver
			// Unwrap GroupedExpression: (123).to-str() → 123.to-str()
			if ge, ok := recvExpr.(*parser.GroupedExpression); ok {
				recvExpr = ge.Expression
			}
		if recv, ok := recvExpr.(*parser.Identifier); ok {
			if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					// 基本型別可能對應多個 nolang 型別名稱（如 i32 → char, i32, u32）
					if primAliases, ok := llvmTypeToNolang[srcType]; ok {
						candidates = append(candidates, primAliases...)
					}
					// 切片/陣列 receiver（如 query []byte）：依元素型別構造 []T 候選名稱，
					// 使 []byte.to-str 等方法能被查找到。
					if (srcType == "vec" || srcType == "arr") && g.arrayElemTypes != nil {
						if elemLLVMType, ok := g.arrayElemTypes[recv.Value]; ok {
							elemLLVMType = strings.TrimPrefix(elemLLVMType, "%")
							if elemAliases, ok := llvmTypeToNolang[elemLLVMType]; ok {
								for _, alias := range elemAliases {
									candidates = append(candidates, "[]"+alias)
								}
								// For arr receivers, also construct [n]t mangled name
								// candidates (e.g. _3xi64.clone) to match transpiler's
								// cloneAndSubstitute output. This is critical for varLLVMType
								// to infer the return type of [n]t methods (e.g. b = a.clone()
								// where a is [3]i64 → returns [3 x i64], not default i64).
								// vec receivers don't need this because []T candidates
								// already cover slice methods.
								if srcType == "arr" && g.arraySizes != nil {
									if arrSize, ok := g.arraySizes[recv.Value]; ok {
										for _, alias := range elemAliases {
											candidates = append(candidates, fmt.Sprintf("_%dx%s", arrSize, alias))
										}
									}
								}
								// For vec (slice) receivers, construct _x<elem> mangled name
								// candidates (e.g. _xi64.max) to match transpiler's
								// cloneAndSubstitute output for slice generics.
								if srcType == "vec" {
									for _, alias := range elemAliases {
										candidates = append(candidates, "_x"+alias)
									}
								}
							}
						}
					}
					// Option type (?T): try the inner type as a candidate
					// (e.g. conn-val is ?str, conn-val.to-lower() → str.to-lower)
					if srcType == "option" && g.optionInnerTypes != nil {
						if innerType, ok := g.optionInnerTypes[recv.Value]; ok && innerType != "" {
							innerSrc := strings.TrimPrefix(innerType, "%")
							candidates = append(candidates, innerSrc)
							if primAliases, ok := llvmTypeToNolang[innerSrc]; ok {
								candidates = append(candidates, primAliases...)
							}
						}
					}
				for _, cand := range candidates {
					shortName := cand + "." + dot.Property
					// Try both the raw name and the sanitized name (e.g. []byte.to-str → _LB__RB_byte.to-str)
					lookupNames := []string{shortName, sanitizeLLVMName(shortName)}
					for _, ln := range lookupNames {
						if g.funcRetTypes != nil {
							if t, ok := g.funcRetTypes[ln]; ok {
								if t != "void" {
									return t
								}
								// void + 單輸出函數（如 str.empty 返回 i1）：
								// 使用 funcResultLLVMType 中的輸出型別
								// Nolang bools are stored as i64, not i1
								if g.funcResultLLVMType != nil {
									if ts, ok := g.funcResultLLVMType[ln]; ok && len(ts) == 1 {
										retType := ts[0]
										if retType == "i1" {
											retType = "i64"
										}
										return retType
									}
								}
							}
						}
					}
					// Also check build-in methods (e.g., str.eq, str.copy, i64.to-str)
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						if len(m.Return) > 0 {
							return g.mapToLLVMType(m.Return[0].String())
						}
						// ForwardFunc builtins with empty Return: infer from ForwardFunc name
						if m.ForwardFunc != "" {
							switch m.ForwardFunc {
							case "with-cap", "with-len", "with-cap-len":
								return "%vec"
							case "bool-to-str", "ffi-cstr-at":
								return "%str-long"
							}
						}
					}
					// Fallback: try module-prefixed versions of the sanitized name.
					// User-defined functions in imported modules may be registered with
					// a module prefix (e.g. "byte._LB__RB_byte.to-str").
					sanitizedSuffix := "." + sanitizeLLVMName(shortName)
					if g.funcRetTypes != nil {
						for fullName, rt := range g.funcRetTypes {
							if !strings.HasSuffix(fullName, sanitizedSuffix) {
								continue
							}
							if rt != "void" {
								return rt
							}
							if g.funcResultLLVMType != nil {
								if ts, ok := g.funcResultLLVMType[fullName]; ok && len(ts) == 1 {
									retType := ts[0]
									if retType == "i1" {
										retType = "i64"
									}
									return retType
								}
							}
						}
					}
				}
					// Fallback: ReceiverGlobal builtins (sqrt, abs, ...) not prefixed by type;
					// look up by method name alone. e.g. r2.sqrt() -> sqrt -> double.
					if m := builtin.FindBuiltinMethod(dot.Property); m != nil && len(m.Return) > 0 {
						return g.mapToLLVMType(m.Return[0].String())
					}
				} else {
					// Module-prefixed function call (e.g., str.with-cap, vec.with-cap)
					// where receiver is a module name, not a variable.
					fullName := recv.Value + "." + dot.Property
					if g.funcRetTypes != nil {
						if t, ok := g.funcRetTypes[fullName]; ok && t != "void" {
							return t
						}
					}
					if m := builtin.FindBuiltinMethod(fullName); m != nil {
						if len(m.Return) > 0 {
							return g.mapToLLVMType(m.Return[0].String())
						}
						if m.ForwardFunc != "" {
							switch m.ForwardFunc {
							case "with-cap", "with-len", "with-cap-len":
								return "%vec"
							case "bool-to-str", "ffi-cstr-at":
								return "%str-long"
							}
						}
					}
					// Fallback: user-defined functions are registered in the maps
					// under their SIMPLE name (e.g. "list-dir"), not the module-prefixed
					// name (e.g. "fs.list-dir"). When the receiver is a module name
					// (not a variable), try looking up the property directly as a
					// user-defined function name. This makes `entries = fs.list-dir(dir)`
					// resolve to "%vec" (the LLVM type of list-dir's []str result)
					// instead of defaulting to "i64", which is critical for downstream
					// arrayElemTypes inference and correct slice indexing codegen.
					if g.funcNumResults != nil {
						if n, ok := g.funcNumResults[dot.Property]; ok {
							if n == 1 {
								if g.funcResultLLVMType != nil {
									if ts, ok2 := g.funcResultLLVMType[dot.Property]; ok2 && len(ts) == 1 {
										retType := ts[0]
										if retType == "i1" {
											retType = "i64"
										}
										return retType
									}
								}
							}
						}
					}
				}
			}
			// IntegerLiteral receiver (e.g., (123).to-str())
			if _, ok := recvExpr.(*parser.IntegerLiteral); ok {
				shortName := "i64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// FloatLiteral receiver (e.g., (3.14).to-str())
			if _, ok := recvExpr.(*parser.FloatLiteral); ok {
				shortName := "f64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// BooleanLiteral receiver (e.g., true.to-str())
			if _, ok := recvExpr.(*parser.BooleanLiteral); ok {
				shortName := "bool." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// PrefixExpression receiver (e.g., (-42).to-str())
			if _, ok := recvExpr.(*parser.PrefixExpression); ok {
				shortName := "i64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// InfixExpression receiver (e.g., (-9223372036854775807 - 1).to-str()，
			// 已被上方 Unwrap GroupedExpression 處理後為 InfixExpression)
			// 算術表達式視為 i64
			if _, ok := recvExpr.(*parser.InfixExpression); ok {
				shortName := "i64." + dot.Property
				if t := g.lookupMethodReturnType(shortName); t != "" {
					return t
				}
			}
			// StringLiteral receiver (e.g., 'hello'.eq(b, n))
			if _, ok := recvExpr.(*parser.StringLiteral); ok {
				shortName := "str." + dot.Property
				if g.funcRetTypes != nil {
					if t, ok := g.funcRetTypes[shortName]; ok {
						if t != "void" {
							return t
						}
						// void + 單輸出函數（如 str.repeat 返回 str）：使用 funcResultLLVMType
						// Nolang bools are stored as i64, not i1
						if g.funcResultLLVMType != nil {
							if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
								retType := ts[0]
								if retType == "i1" {
									retType = "i64"
								}
								return retType
							}
						}
					}
				}
				if m := builtin.FindBuiltinMethod(shortName); m != nil && len(m.Return) > 0 {
					return g.mapToLLVMType(m.Return[0].String())
				}
			}
			// 結構欄位 / 陣列元素 / 切片結果 / 函數呼叫結果接收者
			// （如 c.name.trim()、names[i].slice()、buf[..].to-str()、foo().trim()）
			// 透過 exprResultLLVMType 推導接收者型別，再映射到 nolang 型別名查找方法返回型別
			switch recvExpr.(type) {
			case *parser.DotExpression, *parser.IndexExpression, *parser.SliceExpression, *parser.CallExpression:
				elemType := g.exprResultLLVMType(recvExpr)
				srcType := strings.TrimPrefix(elemType, "%")
				candidates := []string{srcType}
				if primAliases, ok := llvmTypeToNolang[srcType]; ok {
					candidates = append(candidates, primAliases...)
				}
				// vec/arr 切片結果：依元素型別構造 []T 候選（如 []byte.to-str）
				if srcType == "vec" || srcType == "arr" {
					if sliceExpr, ok := recvExpr.(*parser.SliceExpression); ok {
						if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
							if g.arrayElemTypes != nil {
								if et, ok := g.arrayElemTypes[ident.Value]; ok {
									et = strings.TrimPrefix(et, "%")
									if elemAliases, ok := llvmTypeToNolang[et]; ok {
										for _, alias := range elemAliases {
											candidates = append(candidates, "[]"+alias)
										}
									}
								}
							}
						}
					}
				}
				for _, cand := range candidates {
					shortName := cand + "." + dot.Property
					if g.funcRetTypes != nil {
						if t, ok := g.funcRetTypes[shortName]; ok {
							if t != "void" {
								return t
							}
							if g.funcResultLLVMType != nil {
								if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
									retType := ts[0]
									if retType == "i1" {
										retType = "i64"
									}
									return retType
								}
							}
						}
					}
					if m := builtin.FindBuiltinMethod(shortName); m != nil && len(m.Return) > 0 {
						return g.mapToLLVMType(m.Return[0].String())
					}
				}
			}
		}
		return "i64"
	case *parser.FloatLiteral:
		return "double"
	case *parser.PrefixExpression:
		// 負號前置運算：根據運算元型別推導
		// -1.0 → double, -1 → i64, -x → 視 x 型別而定
		if v.Operator == "-" {
			return g.varLLVMType(&parser.LetStatement{Value: v.Right})
		}
		return "i64"
	case *parser.GroupedExpression:
		return g.varLLVMType(&parser.LetStatement{Value: v.Expression})
	case *parser.IfExpression:
		// if 表達式：從 consequence 推斷型別
		if v.Consequence != nil && len(v.Consequence.Statements) > 0 {
			last := v.Consequence.Statements[len(v.Consequence.Statements)-1]
			if es, ok := last.(*parser.ExpressionStatement); ok {
				return g.varLLVMType(&parser.LetStatement{Value: es.Expression})
			}
		}
		// 從 alternative 推斷
		if v.Alternative != nil && len(v.Alternative.Statements) > 0 {
			last := v.Alternative.Statements[len(v.Alternative.Statements)-1]
			if es, ok := last.(*parser.ExpressionStatement); ok {
				return g.varLLVMType(&parser.LetStatement{Value: es.Expression})
			}
		}
		return "i64"
	case *parser.RunExpression:
		// Track result type for awy type inference
		switch c := v.Call.(type) {
		case *parser.CallExpression:
			_, _, resultType := g.resolveAsyncCallInfo(c)
			if resultType != "" && g.taskResultTypes != nil && stmt != nil && stmt.Name != nil {
				g.taskResultTypes[stmt.Name.Value] = resultType
			}
		case *parser.Identifier:
			// run future_var — 从 futureResultTypes 传播到 taskResultTypes
			if g.futureResultTypes != nil && g.taskResultTypes != nil && stmt != nil && stmt.Name != nil {
				if t, ok := g.futureResultTypes[c.Value]; ok {
					g.taskResultTypes[stmt.Name.Value] = t
				}
			}
		}
		return "i8*"
	case *parser.AwaitExpression:
		// awy f-async(args) — 直接调用 -async 函数
		if call, ok := v.Right.(*parser.CallExpression); ok {
			if g.isAsyncCall(call) {
				if _, _, resultType := g.resolveAsyncCallInfo(call); resultType != "" {
					return resultType
				}
			}
		}
		// awy <identifier> — future 变量或 task 变量
		if ident, ok := v.Right.(*parser.Identifier); ok {
			if g.futureResultTypes != nil {
				if t, ok := g.futureResultTypes[ident.Value]; ok {
					return t
				}
			}
			if g.taskResultTypes != nil {
				if t, ok := g.taskResultTypes[ident.Value]; ok {
					return t
				}
			}
		}
		return "i64"
	case *parser.IntegerLiteral:
		// Integer literal without explicit type annotation defaults to i64
		return "i64"
	default:
		return "i64"
	}
}

// inferOptionInnerType determines the inner LLVM type of a ?T option variable
// from its LetStatement. Used during var collection (before codegen) so that
// subsequent varLLVMType calls can resolve method calls on option variables
// (e.g. cl = conn-val.to-lower() where conn-val is ?str).
func (g *Generator) inferOptionInnerType(stmt *parser.LetStatement) string {
	// From explicit type annotation (e.g. val ?f64)
	if nt, ok := stmt.Type.(*parser.NullableType); ok {
		return g.mapToLLVMType(nt.Type.String())
	}
	// From option variable assignment (e.g. it = n where n is ?i64)
	if ident, ok := stmt.Value.(*parser.Identifier); ok {
		if g.optionInnerTypes != nil {
			if srcInner, ok := g.optionInnerTypes[ident.Value]; ok && srcInner != "" {
				return srcInner
			}
		}
	}
	// From function call return type (e.g. f = .get-header(...) returning ?str)
	if call, ok := stmt.Value.(*parser.CallExpression); ok {
		fnName := ""
		if ident, ok := call.Function.(*parser.Identifier); ok {
			fnName = ident.Value
		} else if dot, ok := call.Function.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					fnName = srcType + "." + dot.Property
				} else {
					// Receiver is not a variable → module-prefixed call
					// (e.g. tls.server-accept → fnName = "tls.server-accept")
					fnName = recv.Value + "." + dot.Property
				}
			}
			if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				fnName = "str." + dot.Property
			}
		}
	if fnName != "" && g.funcResultInnerTypes != nil {
		if innerTypes, ok := g.funcResultInnerTypes[fnName]; ok && len(innerTypes) >= 1 && innerTypes[0] != "" {
			return innerTypes[0]
		}
		// Fallback: try short name without module prefix (e.g. "http.do-req" → "do-req")
		if idx := strings.Index(fnName, "."); idx >= 0 {
			shortName := fnName[idx+1:]
			if innerTypes, ok := g.funcResultInnerTypes[shortName]; ok && len(innerTypes) >= 1 && innerTypes[0] != "" {
				return innerTypes[0]
			}
		}
	}
	}
	return ""
}

// inferOptionVecElemType determines the element type of a ?[]T option variable
// (where the inner type is %vec). Used during var collection so that method calls
// like v.push() inside match ok arms can determine the correct element type.
// Returns the LLVM element type (e.g. "%str-long" for ?[]str, "i64" for ?[]i64).
func (g *Generator) inferOptionVecElemType(stmt *parser.LetStatement) string {
	// From explicit type annotation (e.g. val ?[]str)
	if nt, ok := stmt.Type.(*parser.NullableType); ok {
		if st, ok := nt.Type.(*parser.SliceType); ok && st.Elem != nil {
			return g.mapToLLVMType(st.Elem.String())
		}
	}
	// From function call return type (e.g. v = m.get(...) returning ?[]str)
	// We need to check funcResultNolangTypes to get the full ?[]str type string,
	// then extract the element type from it.
	if call, ok := stmt.Value.(*parser.CallExpression); ok {
		fnName := ""
		if ident, ok := call.Function.(*parser.Identifier); ok {
			fnName = ident.Value
		} else if dot, ok := call.Function.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					fnName = srcType + "." + dot.Property
				} else {
					fnName = recv.Value + "." + dot.Property
				}
			}
		}
		// Check funcResultNolangTypes for the full return type (e.g. "?[]str")
		if fnName != "" && g.funcResultNolangTypes != nil {
			if nolangTypes, ok := g.funcResultNolangTypes[fnName]; ok && len(nolangTypes) >= 1 {
				typeStr := nolangTypes[0]
				if strings.HasPrefix(typeStr, "?") {
					typeStr = typeStr[1:]
				}
				if strings.HasPrefix(typeStr, "[]") {
					return g.mapToLLVMType(typeStr[2:])
				}
			}
			// Fallback: try short name
			if idx := strings.Index(fnName, "."); idx >= 0 {
				shortName := fnName[idx+1:]
				if nolangTypes, ok := g.funcResultNolangTypes[shortName]; ok && len(nolangTypes) >= 1 {
					typeStr := nolangTypes[0]
					if strings.HasPrefix(typeStr, "?") {
						typeStr = typeStr[1:]
					}
					if strings.HasPrefix(typeStr, "[]") {
						return g.mapToLLVMType(typeStr[2:])
					}
				}
			}
		}
	}
	// From option variable assignment (e.g. it = v where v is ?[]str)
	if ident, ok := stmt.Value.(*parser.Identifier); ok {
		if g.arrayElemTypes != nil {
			if et, ok := g.arrayElemTypes[ident.Value]; ok && et != "" {
				return et
			}
		}
	}
	return ""
}

func (g *Generator) collectRangeVarTypes(stmt parser.Statement, vars map[string]string) {
	switch s := stmt.(type) {
	case *parser.ForStatement:
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, ok := vars[s.IterRange.Variable]; !ok {
				vars[s.IterRange.Variable] = "i64"
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.collectRangeVarTypes(ss, vars)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.collectRangeVarTypes(ss, vars)
		}
	}
}

func (g *Generator) collectVarDecls(program *parser.Program) map[string]string {
	vars := make(map[string]string)
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			// Don't overwrite existing type — first declaration wins (e.g., a i8; a = 2)
			if _, exists := vars[s.Name.Value]; !exists {
				t := g.varLLVMType(s)
				vars[s.Name.Value] = t
				g.varTypes[s.Name.Value] = t // register immediately for later varLLVMType calls
				// Register array element type for module-level [N]T globals (e.g. SBOX [256]byte)
				// so that IndexExpression codegen uses the correct element type instead of
				// defaulting to i64.
				if at, ok := s.Type.(*parser.ArrayType); ok && at.Size != nil && at.Elem != nil && g.arrayElemTypes != nil {
					// Nested arrays (e.g. [12][16]i64) must use raw LLVM array type [16 x i64]
					// for the element, not %arr struct or [0 x i64] from mapToLLVMType.
					if inner, ok := at.Elem.(*parser.ArrayType); ok {
						g.arrayElemTypes[s.Name.Value] = g.arrayTypeToLLVM(inner)
					} else {
						g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(at.Elem.String())
					}
				}
				// Register slice element type for module-level []T globals
				if st, ok := s.Type.(*parser.SliceType); ok && st.Elem != nil && g.arrayElemTypes != nil {
					g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(st.Elem.String())
				}
			}
			// Recurse into value expression to collect inner variables
			// (e.g. synthetic `it` injected by match desugar inside if-expression branches)
			if s.Value != nil {
				g.collectVarDeclsFromExpr(s.Value, vars)
			}
		case *parser.FunctionDefinition:
		// Skip function bodies - variables inside functions are collected
		// in generateFunctionDefinition via collectVarDeclsFromStmt.
		// This prevents synthetic `it` variables from one function (e.g., %file type)
		// from polluting module-level variable types and leaking into other functions.
		default:
			g.collectVarDeclsFromStmtInner(s, vars, true)
		}
	}
	return vars
}

func (g *Generator) collectVarDeclsFromStmt(stmt parser.Statement, vars map[string]string) {
	g.collectVarDeclsFromStmtInner(stmt, vars, false)
}

func (g *Generator) collectVarDeclsFromStmtInner(stmt parser.Statement, vars map[string]string, isModuleLevel bool) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// Skip synthetic let statements with "err"/"nil" type sentinels.
		// 這些是 match 對應 err/nil arm 注入的 `it = matched`，
		// 變數型別語意上是 err/nil（無值），LLVM 端以 i64 佔位即可。
		// 不註冊為 option 型別，否則後續查找會誤用 matched 的型別。
		if s.IsSynthetic {
			// Note: cross-function type pollution is already prevented because
			// generateFunctionDefinition resets g.varTypes per function (line 1599)
			// and collectVarDecls skips function bodies. Module-level synthetic `it`
			// must be collected so that %it gets allocated for top-level match.
			if nt, ok := s.Type.(*parser.NamedType); ok {
				if nt.Value == "err" {
					// err 臂的 it = 錯誤消息字符串（%str-long），非佔位符。
					// 走 max-size 覆寫邏輯：僅在不存在 / 現有為 i64 / %str-long 更大時更新，
					// 避免縮小已按更大 struct（如 %http.response）分配的共享 it alloca。
					if existing, exists := vars[s.Name.Value]; !exists || existing == "i64" ||
						g.llvmTypeSize("%str-long") > g.llvmTypeSize(existing) {
						vars[s.Name.Value] = "%str-long"
						if g.varTypes != nil {
							g.varTypes[s.Name.Value] = "%str-long"
						}
					}
					return
				}
				if nt.Value == "nil" {
					if _, exists := vars[s.Name.Value]; !exists {
						vars[s.Name.Value] = "i64"
						if g.varTypes != nil {
							g.varTypes[s.Name.Value] = "i64"
						}
					}
					return
				}
			}
			// Synthetic let with non-err/non-nil type (e.g. ok arm with elemType "file"):
			// 若之前已有 placeholder（i64 from err/nil arm），需覆寫以使用真實型別。
			// 否則註冊新變數。
			// `it` 變數跨多個 match 共用：每個 match 的 ok arm 可能有不同的元素型別
			// （如 ?str → %str-long, ?client → %client），必須每次更新以確保
			// 方法呼叫解析（it.close()）能正確找到型別前綴。
			vt := g.varLLVMType(s)
			// 診斷輸出走 NOLANG_DEBUG_IT 環境變量（默認關閉）
			// Fallback: if the source is an option variable with a struct inner type,
			// use the struct inner type as vt. This handles cases where:
			// - mapToLLVMType couldn't resolve a bare struct name (e.g. "server-conn")
			// - varLLVMType returned an existing type from a previous match's it binding
			//   (e.g. %str-long from ?str match), which is wrong for ?tls.server-conn
			if ident, ok := s.Value.(*parser.Identifier); ok {
				if g.varTypes != nil {
					if srcType, ok := g.varTypes[ident.Value]; ok && srcType == "%option" {
						if g.optionInnerTypes != nil {
							if innerType, ok := g.optionInnerTypes[ident.Value]; ok {
								if g.isStructLLVMType(innerType) {
									vt = innerType
								} else if g.isStructLLVMType(vt) {
									// vt is a struct (leaked from previous match arm),
									// but inner type is not (e.g. vt=%str-long, innerType=i64).
									// Use the inner type to avoid type mismatch.
									vt = innerType
								}
						}
					}
				}
			}
		}
		if existing, exists := vars[s.Name.Value]; !exists || existing == "i64" || (g.isStructLLVMType(existing) && existing != vt) {
			// 選擇較大的型別以避免 overflow：struct 型別（%）通常比 i64 大，
			// 多個 struct 型別則保留先到的（因為 option data field 固定 16 bytes）。
			// 例外：當新舊型別同為 struct 且大小相同時，仍需更新為 ok arm 的正確型別。
			// 否則 varTypes["it"] 會保留 err arm 的型別（如 %str-long），
			// 導致後續 c = it 被推導為錯誤型別，c.path 找不到欄位而回退為 i64，
			// 造成 alloca 過小（8 bytes 而非 24），str 欄位截斷為空字串。
			// 安全性：alloca 大小不變，其他 arm 透過 itAllocTypes bitcast 機制適配。
			if !exists || existing == "i64" || g.llvmTypeSize(vt) > g.llvmTypeSize(existing) ||
				(g.isStructLLVMType(vt) && g.llvmTypeSize(vt) == g.llvmTypeSize(existing)) {
				vars[s.Name.Value] = vt
				if g.varTypes != nil {
					g.varTypes[s.Name.Value] = vt
				}
			}
		}
		// Propagate arrayElemTypes and elemElemTypes from the source option
		// variable to `it` so that emitDeepClone in generateLet uses the
		// correct element size (e.g. 24 for %str-long instead of 8 for i64).
		// Without this, the synthetic path returns early and skips the
		// propagation at line ~4495, causing deep clone to use the default
		// i64 element size for ?[]T option unwrapping.
		if ident, ok := s.Value.(*parser.Identifier); ok {
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[ident.Value]; ok && et != "" {
					g.arrayElemTypes[s.Name.Value] = et
				}
			}
			if g.elemElemTypes != nil {
				if eet, ok := g.elemElemTypes[ident.Value]; ok && eet != "" {
					g.elemElemTypes[s.Name.Value] = eet
				}
			}
		}
			return
		}
		// Don't overwrite existing type (e.g. %option declared with ?type)
		if _, exists := vars[s.Name.Value]; !exists {
			// 對模組級全域變數的重新賦值（Type==nil 表示賦值而非宣告），
			// 不應創建本地 alloca 覆蓋全域變數。例如 gen-random 中的
			// `LAST = (LAST * IA + IC) % IM` 必須更新全域 @LAST，
			// 否則每次呼叫都從未初始化的本地 LAST 開始，破壞 RNG 狀態。
			//
			// 修正跨模組全局變量賦值：只有當函數與全局變量來自同一模組時，
			// 函數內的賦值才應寫入全局 @name。否則應創建局部變數。
			// 例如：mod_globals 模組定義了全局變量 COUNTER 和函數 inc，
			// inc 中的 COUNTER = COUNTER + 1 應寫入 @COUNTER。
			// 但 bigint.cmp 中的 result = .abs-cmp(b) 不應寫到主檔案的 @result。
			//
			// 判斷邏輯：
			// 1. globalVarOwner 記錄全局變量屬於哪個模組（"" = 主檔案）
			// 2. funcOwner 記錄當前函數屬於哪個模組（"" = 主檔案）
			// 3. 若兩者歸屬相同，則允許寫入全局變量
			// 4. 若 funcOwner 為空（主檔案函數），也允許寫入（向後相容）
			// 例外：若同名變數是當前函數的參數（如 make-repeat-fasta 的參數 n
			// 與模組級 n i64 = 1000 同名），參數應遮蔽全域變數，不可刪除
			// funcLocalNames 中的記錄，否則 varAddr 會錯誤返回 @n 而非 %n。
			if s.Type == nil && g.globalVars != nil && g.globalVars[s.Name.Value] &&
				g.curFuncName != "" && (g.funcParams == nil || !g.funcParams[s.Name.Value]) {
				// 檢查函數與全局變量是否來自同一模組
				sameModule := true
				if g.globalVarOwner != nil && g.funcOwner != nil {
					varOwner := g.globalVarOwner[s.Name.Value]
					// 模組函數名可能帶模組前綴（如 bigint.cmp），也可能不帶（無衝突時）
					// 先嘗試裸名查找，再嘗試帶前綴的變體
					fo, ok := g.funcOwner[g.curFuncName]
					if !ok {
						// 函數名可能帶模組前綴，嘗試提取前綴
						// 嘗試用點號分割查找
						if idx := strings.LastIndex(g.curFuncName, "."); idx >= 0 {
							fo, ok = g.funcOwner[g.curFuncName[:idx]]
						}
					}
					if ok {
						sameModule = (varOwner == fo)
					}
					// 若 funcOwner 中找不到該函數，默認為主檔案函數（sameModule = true）
				}
				if sameModule {
					if g.funcLocalNames != nil {
						delete(g.funcLocalNames, s.Name.Value)
					}
					if s.Value != nil {
						g.collectVarDeclsFromExpr(s.Value, vars)
					}
					return
				}
			}
			// 切片表達式（view = arr[0..4]）總是走 clone 路徑（malloc + memcpy），
			// 變量需要獨立的 alloca 存儲空間。不再跳過 alloca。
			vt := g.varLLVMType(s)
			// bool (i1) 局部變量擴展為 i64：Nolang 的引用傳遞模型中，函數參數使用
			// resolveOutputParamLLVMType 將 i1 映射為 i64。若局部變量使用 i1 alloca
			// （1 byte），但函數寫入 i64（8 bytes），會覆蓋相鄰變量的內存。
			// 將所有 bool 局部變量統一使用 i64 存儲，避免此問題。
			if vt == "i1" {
				vt = "i64"
			}
			vars[s.Name.Value] = vt
			// Update g.varTypes immediately so subsequent lookups work
			if g.varTypes != nil {
			g.varTypes[s.Name.Value] = vt
		}
		// 變數間賦值（b = a）時傳播 arrayElemTypes 和 elemElemTypes，
		// 使後續 varLLVMType 能正確推導嵌套容器元素的型別（如 b0 = b[0]）。
		if ident, ok := s.Value.(*parser.Identifier); ok {
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[ident.Value]; ok {
					g.arrayElemTypes[s.Name.Value] = et
				}
			}
			if g.elemElemTypes != nil {
				if eet, ok := g.elemElemTypes[ident.Value]; ok {
					g.elemElemTypes[s.Name.Value] = eet
				}
			}
		}
		// Populate optionInnerTypes for ?T variables during collection so that
			// subsequent varLLVMType calls can resolve method calls on option
			// variables (e.g. cl = conn-val.to-lower() where conn-val is ?str).
			if vt == "%option" && g.optionInnerTypes != nil {
			if _, exists := g.optionInnerTypes[s.Name.Value]; !exists {
				if inner := g.inferOptionInnerType(s); inner != "" {
					g.optionInnerTypes[s.Name.Value] = inner
				}
			}
			// For ?[]T option variables (inner type %vec), also register the
			// element type in arrayElemTypes so that method calls like v.push()
			// inside match ok arms can correctly determine the element type.
			// Without this, push defaults to i64, causing type mismatches when
			// pushing str/struct elements.
			if inner, ok := g.optionInnerTypes[s.Name.Value]; ok && inner == "%vec" {
				if g.arrayElemTypes != nil {
					if _, exists := g.arrayElemTypes[s.Name.Value]; !exists {
						if et := g.inferOptionVecElemType(s); et != "" {
							g.arrayElemTypes[s.Name.Value] = et
						}
					}
				}
			}
			// 若 inner 為堆型別，將 option 變數加入 heapVars 追蹤，
				// 確保 v = m.get(...) 這類經 collectVarDecls 推導型別的 option 變數
				// 也在函數結束時釋放其持有的 box。
				if g.heapVars != nil {
					if inner, ok := g.optionInnerTypes[s.Name.Value]; ok && g.isHeapOwningType(inner) {
						if _, isLocal := g.funcLocalNames[s.Name.Value]; isLocal {
							if _, isParam := g.paramNames[s.Name.Value]; !isParam {
								if g.outputParamNames == nil || !g.outputParamNames[s.Name.Value] {
									g.trackLocalHeapVar(s.Name.Value, "%option")
								}
							}
						}
					}
				}
			}
			// Track array size for [N]T locals so we can malloc the data buffer
			if at, ok := s.Type.(*parser.ArrayType); ok && at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					if g.arraySizes != nil {
						g.arraySizes[s.Name.Value] = intLit.Value
					}
					// Also register element type for IndexExpression
					if at.Elem != nil && g.arrayElemTypes != nil {
						g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(at.Elem.String())
					}
				}
			}
			// Register slice element type for []T locals (e.g. []byte → "i8").
			// This must happen during collection (not just in generateLet) so that
			// subsequent varLLVMType calls can resolve []T.method calls (e.g.
			// packet-str = packet.to-str() needs arrayElemTypes["packet"]="i8"
			// to find []byte.to-str and infer %str-long instead of defaulting to i64).
			if st, ok := s.Type.(*parser.SliceType); ok && st.Elem != nil && g.arrayElemTypes != nil {
				g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(st.Elem.String())
				// 嵌套容器（[][]T）：注册内层元素型别
				if g.elemElemTypes != nil {
					if innerType := g.nestedElemLLVMType(st.Elem); innerType != "" {
						g.elemElemTypes[s.Name.Value] = innerType
					}
				}
			}
			// Propagate element type when assigning a struct field of %vec/%arr type
			// to a local variable (e.g. `old-keys = .keys` in hashmap.rehash).
			// This must happen during collection so that subsequent varLLVMType calls
			// for IndexExpressions on this variable (e.g. `v = old-vals[i]`) infer
			// the correct element type instead of defaulting to i64 — which would
			// cause the alloca to be i64 while the actual stored value is %str-long.
			if dot, ok := s.Value.(*parser.DotExpression); ok && g.arrayElemTypes != nil {
				if recvIdent, ok := dot.Receiver.(*parser.Identifier); ok {
					if recvType, ok := g.varTypes[recvIdent.Value]; ok {
						structName := strings.TrimPrefix(recvType, "%")
						if fields, ok := g.structTypes[structName]; ok {
							for _, f := range fields {
								if f.name == dot.Property && f.typ == "%vec" && f.elemType != "" {
									g.arrayElemTypes[s.Name.Value] = f.elemType
									break
								}
							}
						}
					}
				}
			}
			// Infer array/slice element type and size from function call return type
			// (e.g. inner-hash = sha256(...) where sha256 returns [32]byte,
			// or raw = list-dir(...) where list-dir returns []str)
			if (vt == "%arr" || vt == "%vec") && g.funcResultNolangTypes != nil {
				fnName := ""
				if call, ok := s.Value.(*parser.CallExpression); ok {
					if ident, ok := call.Function.(*parser.Identifier); ok {
						fnName = ident.Value
					} else if dot, ok := call.Function.(*parser.DotExpression); ok {
						// Resolve method name (e.g. content.split → str.split)
						if recv, ok := dot.Receiver.(*parser.Identifier); ok {
							if recvType, ok := g.varTypes[recv.Value]; ok {
								srcType := strings.TrimPrefix(recvType, "%")
								candidates := []string{srcType}
								if primAliases, ok := llvmTypeToNolang[srcType]; ok {
									candidates = append(candidates, primAliases...)
								}
								for _, cand := range candidates {
									fullName := cand + "." + dot.Property
									if _, ok := g.funcResultNolangTypes[fullName]; ok {
										fnName = fullName
										break
									}
								}
							} else {
								// Fallback: receiver is a module name (e.g. "fs"),
								// not a variable. User-defined functions may be
								// registered under their full qualified name (e.g.
								// "process.list-all") or their simple name (e.g.
								// "list-dir"). Try the full name first, then the
								// simple name. This makes `all = process.list-all()`
								// infer the []proc-info element type and register
								// g.arrayElemTypes["all"], fixing downstream struct
								// field access type inference.
								fullName := recv.Value + "." + dot.Property
								if _, ok := g.funcResultNolangTypes[fullName]; ok {
									fnName = fullName
								} else if _, ok := g.funcResultNolangTypes[dot.Property]; ok {
									fnName = dot.Property
								}
							}
						}
					}
				}
				if fnName != "" {
					if nolangRets, ok := g.funcResultNolangTypes[fnName]; ok && len(nolangRets) == 1 {
						// Parse [N]T or []T format
						nolangType := nolangRets[0]
						if strings.HasPrefix(nolangType, "[") {
							// Extract element type: [N]T → T or []T → T
							if rbracket := strings.Index(nolangType, "]"); rbracket > 0 {
								elemType := nolangType[rbracket+1:]
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[s.Name.Value] = g.mapToLLVMType(elemType)
								}
								// Extract size: [N]T → N (empty for slices []T)
								sizeStr := nolangType[1:rbracket]
								if n, err := fmt.Sscanf(sizeStr, "%d", new(int64)); err == nil && n == 1 {
									if g.arraySizes != nil {
										var sz int64
										fmt.Sscanf(sizeStr, "%d", &sz)
										g.arraySizes[s.Name.Value] = sz
									}
								}
							}
						}
					}
				}
			}
		}
		// Recurse into value expression to collect inner variables
		// (e.g. `it` injected by match desugar inside if-expression branches)
		if s.Value != nil {
			g.collectVarDeclsFromExpr(s.Value, vars)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			g.collectVarDeclsFromStmt(s.Init, vars)
		}
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, ok := vars[s.IterRange.Variable]; !ok {
				vars[s.IterRange.Variable] = "i64"
				// Update g.varTypes immediately so that subsequent varLLVMType
				// calls for variables referencing this range variable (e.g. nx = y)
				// see the local i64 type, not a stale module-level type (e.g. y = 1.0
				// at module level would leave g.varTypes["y"] = "double").
				if g.varTypes != nil {
					g.varTypes[s.IterRange.Variable] = "i64"
				}
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
	case *parser.ExpressionStatement:
		g.collectVarDeclsFromExpr(s.Expression, vars)
	case *parser.MultiAssignStatement:
		// 註冊多賦值左側的所有變數
		// 對於多結果函數調用，需要根據函數的輸出參數型別推斷每個變數的型別
		if callExpr, ok := s.Value.(*parser.CallExpression); ok {
			fnName := ""
			if ident, ok := callExpr.Function.(*parser.Identifier); ok {
				fnName = ident.Value
			} else if dot, ok := callExpr.Function.(*parser.DotExpression); ok {
				fnName = dot.Property
				// 嘗試解析完整方法名（如 str.to-upper）以查詢 funcResultLLVMType
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					recvType := ""
					if t, ok := vars[recv.Value]; ok {
						recvType = strings.TrimPrefix(t, "%")
					} else if g.varTypes != nil {
						if t, ok := g.varTypes[recv.Value]; ok {
							recvType = strings.TrimPrefix(t, "%")
						}
					}
					if recvType != "" {
						fullName := recvType + "." + dot.Property
						if g.funcResultLLVMType != nil {
							if _, ok := g.funcResultLLVMType[fullName]; ok {
								fnName = fullName
							}
						}
					}
				}
			}
			// Determine the return types for each target position
			var retTypes []string
			if fnName != "" {
				if g.funcResultLLVMType != nil {
					if rets, ok := g.funcResultLLVMType[fnName]; ok && len(rets) >= len(s.Targets) {
						for _, t := range rets {
							if t == "i1" {
								t = "i64"
							}
							retTypes = append(retTypes, t)
						}
					} else if m := builtin.FindBuiltinMethod(fnName); m != nil && len(m.Return) >= len(s.Targets) {
						for _, r := range m.Return {
							t := g.mapToLLVMType(r.String())
							if t == "i1" {
								t = "i64"
							}
							retTypes = append(retTypes, t)
						}
					}
				} else if m := builtin.FindBuiltinMethod(fnName); m != nil && len(m.Return) >= len(s.Targets) {
					for _, r := range m.Return {
						t := g.mapToLLVMType(r.String())
						if t == "i1" {
							t = "i64"
						}
						retTypes = append(retTypes, t)
					}
				}
			}
		// Register each Identifier target with the corresponding return type
		for i, target := range s.Targets {
			if ident, ok := target.(*parser.Identifier); ok {
				// 多賦值左側變數屬於當前作用域（main 或函數體），必須註冊為
				// 局部名，否則會與同名全局函數（如標準庫的 out/err 打印函數）
				// 衝突：作為返回槽指標傳遞時被誤判成函數指標 void(...)**，
				// 導致呼叫方傳參型別錯亂、opt 把形參表重排、運行時空指標崩潰。
				if g.funcLocalNames != nil {
					g.funcLocalNames[ident.Value] = true
				}
				if _, exists := vars[ident.Value]; !exists {
						var vt string
						if i < len(retTypes) {
							vt = retTypes[i]
						} else {
							vt = "i64"
						}
						vars[ident.Value] = vt
						if g.varTypes != nil {
							g.varTypes[ident.Value] = vt
						}
					}
				}
				// IndexExpression targets (e.g., fields[n]) are not new variables
			}
		} else {
			for _, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					if _, exists := vars[ident.Value]; !exists {
						vars[ident.Value] = "i64"
						if g.varTypes != nil {
							g.varTypes[ident.Value] = "i64"
						}
					}
				}
			}
		}
		// 遞迴收集值中的變數宣告
		if s.Value != nil {
			g.collectVarDeclsFromExpr(s.Value, vars)
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.collectVarDeclsFromStmt(ss, vars)
		}
	}
}

func (g *Generator) collectVarDeclsFromExpr(expr parser.Expression, vars map[string]string) {
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
	case *parser.CallExpression:
		// 註冊輸出參數變數（函調用的最後一個參數為 Identifier 時）
		if g.funcRetTypes != nil && len(e.Arguments) > 0 {
			fnName := ""
			if ident, ok := e.Function.(*parser.Identifier); ok {
				fnName = ident.Value
			} else if dot, ok := e.Function.(*parser.DotExpression); ok {
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					fnName = recv.Value + "." + dot.Property
				}
			}
			if fnName != "" {
				if retType, ok := g.funcRetTypes[fnName]; ok && retType != "void" {
					if n, ok := g.funcNumResults[fnName]; ok && n == 1 {
						// Single named result: last arg might be output param
						lastArgIdent, isIdent := e.Arguments[len(e.Arguments)-1].(*parser.Identifier)
						if isIdent {
							paramCount := len(e.Arguments)
							if g.funcParamCount != nil {
								if pc, ok := g.funcParamCount[fnName]; ok {
									paramCount = pc
								}
							}
							if len(e.Arguments) > paramCount {
								varName := lastArgIdent.Value
								if _, exists := vars[varName]; !exists {
									vars[varName] = retType
									if g.varTypes != nil {
										g.varTypes[varName] = retType
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func (g *Generator) collectStructType(sd *parser.StructDefinition) {
	g.collectStructTypeFields(sd)
	// 註冊 struct type 名稱
	g.varTypes[sd.Name] = "%" + sd.Name
}

// collectStructTypeFields 只收集結構體欄位型別到 g.structTypes，不寫入 g.varTypes。
// 適用於需要 structTypes 已被填充但不希望汙染 varTypes 的早期階段
// （如函數參數/回傳型別預掃描）。
func (g *Generator) collectStructTypeFields(sd *parser.StructDefinition) {
	var fields []structField
	for _, f := range sd.Fields {
		llvmType := "i64"
		elemTy := "" // element type for %vec fields
		elemElemTy := "" // inner element type for nested container fields (e.g. [][]str)
	if f.ArraySize > 0 {
		elemType := "i64"
		if f.Type != nil {
			// f.Type is the full ArrayType (e.g. [16]session), but we need the
			// element type. Extract it from ArrayType.Elem to avoid calling
			// mapToLLVMType on the full "[16]session" string (which returns %arr).
			elemTypeStr := f.Type.String()
			if at, ok := f.Type.(*parser.ArrayType); ok && at.Elem != nil {
				elemTypeStr = at.Elem.String()
			}
			elemType = toLLVMType(g.mapToLLVMType(elemTypeStr))
		}
			llvmType = fmt.Sprintf("[%d x %s]", f.ArraySize, elemType)
			// 嵌套容器（[N][]T）：記錄內層元素型別
			if elemType == "%vec" || elemType == "%arr" {
				elemElemTy = g.nestedElemLLVMType(f.Type)
			}
		} else if f.IsSlice {
			// 切片用 %vec 型別
			llvmType = "%vec"
			// 記錄元素型別（byte → i8, i64 → i64, 等）
			// 從 SliceType 中提取元素型別
			if st, ok := f.Type.(*parser.SliceType); ok {
				elemTy = g.mapToLLVMType(st.Elem.String())
			} else if f.Type != nil {
				elemTy = g.mapToLLVMType(f.Type.String())
			}
			if elemTy == "" {
				elemTy = "i64"
			}
			// 嵌套容器（[][]T）：記錄內層元素型別
			if elemTy == "%vec" || elemTy == "%arr" {
				elemElemTy = g.nestedElemLLVMType(f.Type)
			}
		} else if f.Type != nil {
			llvmType = g.mapToLLVMType(f.Type.String())
		}
		fields = append(fields, structField{name: f.Name, typ: llvmType, elemType: elemTy, elemElemType: elemElemTy})
	}
	g.structTypes[sd.Name] = fields
}

func (g *Generator) generateStatement(sb *strings.Builder, stmt parser.Statement) {
	// 清空语句级临时堆对象列表：每个语句独立管理自己的临时对象，
	// 循环体内每次迭代的语句都会清空+注册+释放，不会累积。
	g.stmtTemporaries = nil
	g.stmtTempRawPtrs = nil
	switch s := stmt.(type) {
	case *parser.LetStatement:
		g.generateLet(sb, s)
	case *parser.ExpressionStatement:
		g.generateExpressionStmt(sb, s)
	case *parser.ForStatement:
		g.generateForStatement(sb, s)
	case *parser.BreakStatement:
		// Do NOT emit lifetime.end here — break only exits the innermost loop,
		// not the function. Calling emitLifetimeEnd would mark all function
		// locals as dead, causing -O3 to treat subsequent accesses (e.g. outer
		// loop's `i`, `matched`, `limit`) as undefined behavior.
		target := g.findLoopTarget(s.Label, true)
		if target != "" {
			curBlock := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), target))
			g.blockTerminated = true
			// CFG: current block → loop exit target
			g.cfgEdge(curBlock, target)
			g.cfgTerm(curBlock, termBr)
		}

	case *parser.ContinueStatement:
		target := g.findLoopTarget(s.Label, false)
		if target != "" {
			curBlock := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), target))
			g.blockTerminated = true
			// CFG: current block → loop continue target (step/cond)
			g.cfgEdge(curBlock, target)
			g.cfgTerm(curBlock, termBr)
		}

	case *parser.InterfaceDefinition:
		// 介面定義不生成 LLVM IR

	case *parser.EnumDefinition:
		g.generateEnumDefinition(sb, s)

	case *parser.StructDefinition:
		// type already emitted in Generate()
	// struct definition 本身不生成 IR（type 已由 Generate 發出）
	case *parser.MultiAssignStatement:
		if innerCall, ok := s.Value.(*parser.CallExpression); ok {
			// 註冊左側目標變數為局部名，避免與同名全局函數（如標準庫的 out/err
			// 打印函數）衝突：否則在將其作為返回槽指標傳遞時會被誤判成函數指標
			// void(...)**，導致呼叫方傳參型別錯亂、opt 把形參表重排、運行時空指標崩潰。
			for _, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					if g.funcLocalNames != nil {
						g.funcLocalNames[ident.Value] = true
					}
				}
			}
			outerCall := &parser.CallExpression{
				Token:     innerCall.Token,
				Function:  innerCall,
				Arguments: s.Targets,
			}
			g.generateExpressionStmt(sb, &parser.ExpressionStatement{Expression: outerCall})
		}
		// 返回值延遲零值追蹤：多賦值 a, b = f() 完成後，標記每個 out 參數目標已賦值。
		// 目標可能是 Identifier（result）、IndexExpression（fields[n]）或 DotExpression（obj.field），
		// 取其基底變數名檢查是否為 out 參數。
		if g.retInitBitmapVar != "" {
			for _, target := range s.Targets {
				baseName := ""
				switch t := target.(type) {
				case *parser.Identifier:
					baseName = t.Value
				case *parser.IndexExpression:
					if ident, ok := t.Left.(*parser.Identifier); ok {
						baseName = ident.Value
					}
				case *parser.DotExpression:
					if ident, ok := t.Receiver.(*parser.Identifier); ok {
						baseName = ident.Value
					}
				}
				if baseName != "" && g.outputParamNames != nil && g.outputParamNames[baseName] {
					g.emitSetRetInitBit(sb, baseName)
				}
			}
		}

	case *parser.ReturnStatement:
		// Nolang 禁止 return <值>，结果通过具名输出参数（out-param）传递。
		// ReturnValue 始终为 nil，此处只处理裸 return。
		g.emitRetInitZeroFill(sb)
		g.flushOutputBindings(sb)
		g.emitLocalTasksFree(sb)
		g.emitHeapFree(sb)
		g.emitLifetimeEnd(sb)
		if g.curFuncRetType != "void" && g.curFuncRetName != "" {
			// 有輸出參數的裸 return：載入輸出參數並返回
			resultLoad := g.tmpReg("ret.val")
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %%%s\n",
				g.indent(), resultLoad, toLLVMType(g.curFuncRetType), toLLVMType(g.curFuncRetType), g.curFuncRetName))
			sb.WriteString(fmt.Sprintf("%sret %s %s\n", g.indent(), toLLVMType(g.curFuncRetType), resultLoad))
		} else if g.curFuncRetType != "void" && g.curFuncRetType != "" {
			// 無輸出參數但有回傳型別：回傳零值
			sb.WriteString(fmt.Sprintf("%sret %s 0\n", g.indent(), toLLVMType(g.curFuncRetType)))
		} else if g.inMainFunction {
			// main 函數的裸 return：回傳 i32 0（C 入口點慣例）
			sb.WriteString(g.indent() + "ret i32 0\n")
		} else {
			sb.WriteString(g.indent() + "ret void\n")
		}
		g.blockTerminated = true
		// CFG: return terminator (no successors)
		g.cfgTerm(g.cfgBlockLabel(), termRet)
	}
	// 语句结束前释放未绑定变量的临时堆对象（如 str 拼接结果）。
	// 只 free data buffer，不 free 结构体本身（alloca 栈分配）。
	// 若 block 已终止（ret/br），无法继续发射指令，跳过释放；
	// 被返回值消费的临时对象所有权已转移给调用者，不应释放。
	g.emitStmtTemporariesFree(sb)
}

// emitStmtTemporariesFree 释放当前语句注册的临时堆对象。
// 对每个 %str-long* 临时指针，load 其 data 字段（field 2），NULL check 后 call @free。
// 对每个 raw i8* 临时指针，NULL check 后 call @free。
// 不 free 结构体本身（alloca 栈分配）。
// 若 block 已终止，跳过释放（避免在 ret/br 后发射指令）。
func (g *Generator) emitStmtTemporariesFree(sb *strings.Builder) {
	if len(g.stmtTemporaries) == 0 && len(g.stmtTempRawPtrs) == 0 {
		g.stmtTemporaries = nil
		g.stmtTempRawPtrs = nil
		return
	}
	// block 已终止（如 return/break/continue），无法继续发射指令。
	// 被返回值消费的临时对象所有权已转移；break/continue 语句本身不产生临时对象。
	if g.blockTerminated {
		g.stmtTemporaries = nil
		g.stmtTempRawPtrs = nil
		return
	}
	for _, tmp := range g.stmtTemporaries {
		// %vec = { i64 len, i64 cap, i64 data }，data 在 field 2
		// emitShallowDataFree 会 GEP field 2 → load i64 → inttoptr → NULL check → free
		// %str-long 和 %vec 記憶體佈局相同，統一用 %vec
		g.emitShallowDataFree(sb, tmp, "%vec", 2)
	}
	for _, rawPtr := range g.stmtTempRawPtrs {
		// raw i8* pointer: NULL check → free
		// Labels must NOT start with '%' — LLVM IR label definitions are "name:"
		// without the '%' prefix (only br references use "%name").
		nullCmp := g.tmpReg("heapfree.rawnull")
		g.tmpIdx++
		freeLabel := fmt.Sprintf("heapfree.rawfree.%d", g.tmpIdx)
		g.tmpIdx++
		skipLabel := fmt.Sprintf("heapfree.rawskip.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmp, rawPtr))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmp, skipLabel, freeLabel))
		g.emitLabel(sb, freeLabel)
		sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), rawPtr))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipLabel))
		g.emitLabel(sb, skipLabel)
	}
	g.stmtTemporaries = nil
	g.stmtTempRawPtrs = nil
}

// untrackStmtTemporary 从 stmtTemporaries 列表移除指定临时指针。
// 用于 generateLet 中 RHS 求值结果绑定到变量时：变量通过 heapVars/trackLocalHeapVar
// 接管 data 所有权，临时对象不再由语句级释放机制 free（避免 double-free）。
func (g *Generator) untrackStmtTemporary(val string) {
	for i, t := range g.stmtTemporaries {
		if t == val {
			g.stmtTemporaries = append(g.stmtTemporaries[:i], g.stmtTemporaries[i+1:]...)
			return
		}
	}
}

// trackStrTemporary 将一个 %str-long* 临时指针注册到 stmtTemporaries，
// 若已存在则跳过（避免 double-free）。
// 用于 builtin 结果（如 read-file）和 SSA 值物化的临时 alloca，
// 确保其 malloc'd data 在语句结束时被释放。
func (g *Generator) trackStrTemporary(ptr string) {
	for _, t := range g.stmtTemporaries {
		if t == ptr {
			return
		}
	}
	g.stmtTemporaries = append(g.stmtTemporaries, ptr)
}

func (g *Generator) generateForStatement(sb *strings.Builder, stmt *parser.ForStatement) {
	// range for: for i in [a..b] — push/pop handled in generateRangeFor
	if stmt.IterRange != nil {
		g.generateRangeFor(sb, stmt)
		return
	}

	// Push loop exit target
	g.tmpIdx++
	labelId := g.tmpIdx

	// 次數循環：{ } * N
	if stmt.CountExpr != nil {
		// 編譯期優化：若 N 為常數且 ≤ 0，循環體不執行，直接跳過
		if intLit, ok := stmt.CountExpr.(*parser.IntegerLiteral); ok && intLit.Value <= 0 {
			return
		}
		counterVar := fmt.Sprintf("__lc_%d", labelId)
		// init: %__lc_N = alloca i64, store 0
		sb.WriteString(fmt.Sprintf("%s%%%s = alloca i64\n", g.indent(), counterVar))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), counterVar))

		g.loopExits = append(g.loopExits, loopExit{
			name: stmt.Label,
			cond: fmt.Sprintf("for.step.%d", labelId),
			exit: fmt.Sprintf("for.end.%d", labelId),
		})
		defer func() {
			g.loopExits = g.loopExits[:len(g.loopExits)-1]
		}()

		// br → cond
		preheaderBlock := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
		// CFG: preheader → cond
		g.cfgEdge(preheaderBlock, fmt.Sprintf("for.cond.%d", labelId))
		g.cfgTerm(preheaderBlock, termBr)

		// cond block
		condLabel := fmt.Sprintf("for.cond.%d", labelId)
		g.emitLabel(sb, condLabel)
		g.indentLevel++
		counterLoad := fmt.Sprintf("%%%s.val", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), counterLoad, counterVar))
		cmpVal := g.generateExprWithSB(sb, stmt.CountExpr)
		cmpResult := fmt.Sprintf("%%%s.cmp", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpResult, counterLoad, cmpVal))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%for.body.%d, label %%for.end.%d\n",
			g.indent(), cmpResult, labelId, labelId))
		// CFG: cond → body / end
		g.cfgEdge(condLabel, fmt.Sprintf("for.body.%d", labelId))
		g.cfgEdge(condLabel, fmt.Sprintf("for.end.%d", labelId))
		g.cfgTerm(condLabel, termCondBr)
		g.indentLevel--

		// body block
		bodyLabel := fmt.Sprintf("for.body.%d", labelId)
		g.emitLabel(sb, bodyLabel)
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		// body 未終止時跳到 step 執行更新
		if !g.blockTerminated {
			bodyTail := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%for.step.%d\n", g.indent(), labelId))
			// CFG: body tail → step
			g.cfgEdge(bodyTail, fmt.Sprintf("for.step.%d", labelId))
			g.cfgTerm(bodyTail, termBr)
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: counter++（continue 跳轉目標）
		stepLabel := fmt.Sprintf("for.step.%d", labelId)
		g.emitLabel(sb, stepLabel)
		g.indentLevel++
		// update: %val = load i64, %cnt; %inc = add i64 %val, 1; store i64 %inc, %cnt
		updateLoad := fmt.Sprintf("%%%s.val2", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), updateLoad, counterVar))
		updateInc := fmt.Sprintf("%%%s.inc", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), updateInc, updateLoad))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), updateInc, counterVar))
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
		// CFG: step → cond (back edge)
		g.cfgEdge(stepLabel, condLabel)
		g.cfgTerm(stepLabel, termBr)
		g.indentLevel--

		// end block
		g.emitLabel(sb, fmt.Sprintf("for.end.%d", labelId))
		return
	}

	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("for.step.%d", labelId),
		exit: fmt.Sprintf("for.end.%d", labelId),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// init
	if stmt.Init != nil {
		g.generateStatement(sb, stmt.Init)
	}

	// br → cond
	preheaderBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
	// CFG: preheader → cond
	g.cfgEdge(preheaderBlock, fmt.Sprintf("for.cond.%d", labelId))
	g.cfgTerm(preheaderBlock, termBr)

	// cond block
	condLabel := fmt.Sprintf("for.cond.%d", labelId)
	g.emitLabel(sb, condLabel)
	g.indentLevel++
	condVal := ""
	isInfiniteLoop := false
	skipBody := false
	if stmt.Condition != nil {
		// 無限循環 { body } (true)：條件是 BooleanLiteral{true}，直接生成 br label %for.body
		// 不創建 for.cond → for.end 的不可達邊，避免 LLVM 優化器在多輸出參數 + break
		// 場景下錯誤地常量傳播輸出參數值（如 ok=true 被優化為 false）。
		if boolLit, ok := stmt.Condition.(*parser.BooleanLiteral); ok && boolLit.Value {
			isInfiniteLoop = true
			sb.WriteString(fmt.Sprintf("%sbr label %%for.body.%d\n", g.indent(), labelId))
			// CFG: cond → body only (no edge to end)
			g.cfgEdge(condLabel, fmt.Sprintf("for.body.%d", labelId))
			g.cfgTerm(condLabel, termBr)
			g.indentLevel--
		} else if boolLit, ok := stmt.Condition.(*parser.BooleanLiteral); ok && !boolLit.Value {
			// { body } (false) 或 { body } () — 條件永遠為 false，直接跳到 for.end
			skipBody = true
			sb.WriteString(fmt.Sprintf("%sbr label %%for.end.%d\n", g.indent(), labelId))
			// CFG: cond → end only
			g.cfgEdge(condLabel, fmt.Sprintf("for.end.%d", labelId))
			g.cfgTerm(condLabel, termBr)
			g.indentLevel--
		} else if infix, ok := stmt.Condition.(*parser.InfixExpression); ok {
			isCmp := infix.Operator == "==" || infix.Operator == "!=" ||
				infix.Operator == "<" || infix.Operator == ">" ||
				infix.Operator == "<=" || infix.Operator == ">="
			if isCmp {
				condVal = g.generateInfixI1(sb, infix)
			} else {
				// 非比較運算（如 && / ||）：使用 generateConditionAsI1
				// 它會檢查表達式的 LLVM 型別，若已是 i1 則直接使用，
				// 否則才 trunc i64 to i1。避免對 i1 值生成 trunc i64。
				condVal = g.generateConditionAsI1(sb, stmt.Condition)
			}
		} else {
			// Option variable as boolean condition (e.g. for cond = recv-f where
			// recv-f is ?T): check tag != 1 (nil) instead of truncating the data
			// pointer to i1. Without this, struct inner types (e.g. ?http2-frame)
			// would return a %T* data pointer and fail to truncate.
			if ident, ok := stmt.Condition.(*parser.Identifier); ok {
				if t, ok := g.varTypes[ident.Value]; ok && t == "%option" {
					tagGEP := g.tmpReg("for.opt.tag.gep")
					tagLoad := g.tmpReg("for.opt.tag")
					cmpReg := g.tmpReg("for.opt.cmp")
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 0\n",
						g.indent(), tagGEP, llvmVarRef(ident.Value)))
					sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), tagLoad, tagGEP))
					sb.WriteString(fmt.Sprintf("%s%s = icmp ne i64 %s, 1\n", g.indent(), cmpReg, tagLoad))
					condVal = cmpReg
				}
			}
			if condVal == "" {
				rawVal := g.generateExprWithSB(sb, stmt.Condition)
				if strings.HasPrefix(rawVal, "%") {
					// 判斷條件表達式的 LLVM 型別。若已是 i1（如 bool 返回的用戶函數 rows.next()），
					// 直接使用；否則為 i64，需 trunc 到 i1。
					condType := g.intExprLLVMType(stmt.Condition)
					if condType == "i1" {
						condVal = rawVal
					} else {
						truncReg := g.tmpReg("for.trunc")
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), truncReg, rawVal))
						condVal = truncReg
					}
				} else {
					condVal = rawVal
				}
			}
		}
	} else {
		// Condition is nil — treat as infinite loop (historical path)
		isInfiniteLoop = true
		sb.WriteString(fmt.Sprintf("%sbr label %%for.body.%d\n", g.indent(), labelId))
		// CFG: cond → body only (no edge to end)
		g.cfgEdge(condLabel, fmt.Sprintf("for.body.%d", labelId))
		g.cfgTerm(condLabel, termBr)
		g.indentLevel--
	}
	if !isInfiniteLoop && condVal != "" {
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%for.body.%d, label %%for.end.%d\n",
			g.indent(), condVal, labelId, labelId))
		// CFG: cond → body / end
		g.cfgEdge(condLabel, fmt.Sprintf("for.body.%d", labelId))
		g.cfgEdge(condLabel, fmt.Sprintf("for.end.%d", labelId))
		g.cfgTerm(condLabel, termCondBr)
		g.indentLevel--
	}

	// body block (skip when condition is constant false)
	if !skipBody {
		bodyLabel := fmt.Sprintf("for.body.%d", labelId)
		g.emitLabel(sb, bodyLabel)
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		// body 未終止時跳到 step 執行更新
		if !g.blockTerminated {
			bodyTail := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%for.step.%d\n", g.indent(), labelId))
			// CFG: body tail → step
			g.cfgEdge(bodyTail, fmt.Sprintf("for.step.%d", labelId))
			g.cfgTerm(bodyTail, termBr)
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: 執行 update（continue 跳轉目標）
		stepLabel := fmt.Sprintf("for.step.%d", labelId)
		g.emitLabel(sb, stepLabel)
		g.indentLevel++
		// update
		if stmt.Update != nil {
			g.generateStatement(sb, stmt.Update)
		}
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
		// CFG: step → cond (back edge)
		g.cfgEdge(stepLabel, condLabel)
		g.cfgTerm(stepLabel, termBr)
		g.indentLevel--
	}

	// end block
	g.emitLabel(sb, fmt.Sprintf("for.end.%d", labelId))
}

func (g *Generator) generateStringRange(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	varName := ir.Variable
	str := ir.RangeStr
	g.tmpIdx++
	lbl := g.tmpIdx

	// 建立字串常數
	idx := g.stringIdx
	g.stringIdx++
	escaped := g.escapeLLVMString(str)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", idx, len(str), escaped))

	idxReg := g.tmpReg("str-longidx")
	ptrReg := g.tmpReg("str-longptr")

	// init: idx = 0
	sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), idxReg))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), idxReg, varName))
	sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), ptrReg))

	// br → cond
	preheaderBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%sbr label %%str-long.cond.%d\n", g.indent(), lbl))
	// CFG: preheader → cond
	g.cfgEdge(preheaderBlock, fmt.Sprintf("str.cond.%d", lbl))
	g.cfgTerm(preheaderBlock, termBr)

	// cond block
	condLabel := fmt.Sprintf("str.cond.%d", lbl)
	g.emitLabel(sb, condLabel)
	g.indentLevel++
	iLoad := g.tmpReg("str-longi")
	cmpReg := g.tmpReg("str-longcmp")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmpReg, iLoad, len(str)))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%str-long.body.%d, label %%str-long.end.%d\n", g.indent(), cmpReg, lbl, lbl))
	// CFG: cond → body / end
	g.cfgEdge(condLabel, fmt.Sprintf("str.body.%d", lbl))
	g.cfgEdge(condLabel, fmt.Sprintf("str.end.%d", lbl))
	g.cfgTerm(condLabel, termCondBr)
	g.indentLevel--

	// body: char = str[i]; varName = char
	bodyLabel := fmt.Sprintf("str.body.%d", lbl)
	g.emitLabel(sb, bodyLabel)
	g.indentLevel++
	chReg := g.tmpReg("str-longch")
	chZext := g.tmpReg("str-longchz")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [%d x i8], [%d x i8]* @.str.%d, i64 0, i64 %s\n",
		g.indent(), chReg, len(str)+1, len(str)+1, idx, iLoad))
	sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), chZext, chReg))
	charVal := g.tmpReg("str-longcv")
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), charVal, chZext))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), charVal, varName))

	// body statements
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}

	// update + back-edge: idx++; br → cond
	if !g.blockTerminated {
		bodyTail := g.cfgBlockLabel()
		iNext := g.tmpReg("str-longnext")
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%str-long.cond.%d\n", g.indent(), lbl))
		// CFG: body tail → cond (back edge)
		g.cfgEdge(bodyTail, condLabel)
		g.cfgTerm(bodyTail, termBr)
	}
	g.blockTerminated = false
	g.indentLevel--

	g.emitLabel(sb, fmt.Sprintf("str.end.%d", lbl))
}

func (g *Generator) generateArrayRange(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	varName := ir.Variable
	var structPtr string // "%arr* %%identName" or "%vec* %vec.tmp.N"
	var structType string
	var isVec bool
	var elemType string

	// Handle inline slice expression: for x in arr[1..3]
	// Generate the slice to a temporary %slic.N (alloca %vec/%str-long), then iterate.
	if sliceExpr, ok := ir.RangeExpr.(*parser.SliceExpression); ok {
		structPtr = g.generateSliceExpression(sb, sliceExpr)
		// Determine result type and element type from the slice expression
		recvType := g.exprResultLLVMType(sliceExpr.Left)
		if recvType == "%str-long" {
			structType = "%str-long"
			isVec = false
			elemType = "i8"
		} else {
			structType = "%vec"
			isVec = true
			elemType = "i64"
			// Try to get more specific element type
			if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
				if et, ok := g.arrayElemTypes[ident.Value]; ok {
					elemType = toLLVMType(et)
				}
			}
		}
	} else if ident, ok := ir.RangeExpr.(*parser.Identifier); ok {
		// Named variable: for i in a
		identName := ident.Value

		// Slice view: use adjusted data pointer + view length directly
		if g.isSliceViewVar(identName) {
			view := g.sliceViews[identName]
			elemType = toLLVMType(view.elemType)
			structType = view.baseType
			isVec = !view.isStr

			g.tmpIdx++
			lbl := g.tmpIdx
			g.loopExits = append(g.loopExits, loopExit{
				name: stmt.Label,
				cond: fmt.Sprintf("arr.step.%d", lbl),
				exit: fmt.Sprintf("arr.end.%d", lbl),
			})
			defer func() {
				g.loopExits = g.loopExits[:len(g.loopExits)-1]
			}()

			// Initialize i = 0
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))

			// br → cond
			preheaderBlock := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
			// CFG: preheader → cond
			g.cfgEdge(preheaderBlock, fmt.Sprintf("arr.cond.%d", lbl))
			g.cfgTerm(preheaderBlock, termBr)

			// cond block: i < viewLen
			condLabel := fmt.Sprintf("arr.cond.%d", lbl)
			g.emitLabel(sb, condLabel)
			g.indentLevel++
			iLoad := g.tmpReg("arr.i")
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
			cmpReg := g.tmpReg("arr.cmp")
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, view.viewLen))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%arr.body.%d, label %%arr.end.%d\n", g.indent(), cmpReg, lbl, lbl))
			// CFG: cond → body / end
			g.cfgEdge(condLabel, fmt.Sprintf("arr.body.%d", lbl))
			g.cfgEdge(condLabel, fmt.Sprintf("arr.end.%d", lbl))
			g.cfgTerm(condLabel, termCondBr)
			g.indentLevel--

			// body block
			g.emitLabel(sb, fmt.Sprintf("arr.body.%d", lbl))
			g.indentLevel++

			// Load element from view data[i] using adjusted data pointer
			castReg := g.tmpReg("arr.cast")
			ptrType := elemType + "*"
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s\n", g.indent(), castReg, view.dataPtrReg, ptrType))

			elemGEP := g.tmpReg("arr.elem.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s %s, i64 %s\n",
				g.indent(), elemGEP, elemType, ptrType, castReg, iLoad))

			elemLoad := g.tmpReg("arr.elem")
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), elemLoad, toLLVMType(elemType), toLLVMType(elemType)+"*", elemGEP))

			// Store element into loop variable
			g.varTypes[varName] = elemType
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %%%s\n", g.indent(), toLLVMType(elemType), elemLoad, toLLVMType(elemType)+"*", varName))

			// Generate body statements
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
			// body 未終止時跳到 step 執行更新
			if !g.blockTerminated {
				bodyTail := g.cfgBlockLabel()
				sb.WriteString(fmt.Sprintf("%sbr label %%arr.step.%d\n", g.indent(), lbl))
				// CFG: body tail → step
				g.cfgEdge(bodyTail, fmt.Sprintf("arr.step.%d", lbl))
				g.cfgTerm(bodyTail, termBr)
			}
			g.blockTerminated = false
			g.indentLevel--

			// step block: i++（continue 跳轉目標）
			stepLabel := fmt.Sprintf("arr.step.%d", lbl)
			g.emitLabel(sb, stepLabel)
			g.indentLevel++
			// Increment i
			iLoad2 := g.tmpReg("arr.i2")
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad2, varName))
			iInc := g.tmpReg("arr.inc")
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iInc, iLoad2))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInc, varName))
			sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
			// CFG: step → cond (back edge)
			g.cfgEdge(stepLabel, condLabel)
			g.cfgTerm(stepLabel, termBr)
			g.indentLevel--

			// end block
			g.emitLabel(sb, fmt.Sprintf("arr.end.%d", lbl))
			return
		}

		structType = g.varTypes[identName]
		isVec = structType == "%vec"
		if structType == "" {
			structType = "%arr"
		}
		// 使用 varAddr 以正確處理全域變數（@name）vs 局部變數（%name）。
		// 直接拼 "%%%s" 會把全域變數誤當作局部，導致 LLVM「undefined value」錯誤。
		structPtr = g.varAddr(identName)

		// Get element type
		elemType = "i64"
		if et, ok := g.arrayElemTypes[identName]; ok {
			elemType = toLLVMType(et)
		}
	} else if sliceLit, ok := ir.RangeExpr.(*parser.SliceLiteral); ok {
		// Inline slice literal: for i in [1, 2, 3]
		structType = "%vec"
		isVec = true
		elemType = "i64"

		g.tmpIdx++
		tid := g.tmpIdx
		tmpVec := fmt.Sprintf("%%vec.tmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), tmpVec))

		n := int64(len(sliceLit.Elements))

		// alloca temp array
		arrType := fmt.Sprintf("[%d x %s]", n, elemType)
		tmpArr := fmt.Sprintf("%%slice.tmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))

		// store elements via GEP
		for i, elem := range sliceLit.Elements {
			ev := g.generateExprWithSB(sb, elem)
			ev = g.stripLLVMType(ev)
			// When elemType is %str-long and ev is a %str-long* pointer (e.g. from
			// a StringLiteral alloca), load the struct value before storing.
			if elemType == "%str-long" {
				ev = g.loadStrValueIfNeeded(sb, ev)
			}
			gepReg := g.tmpReg("slice.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), gepReg, arrType, arrType, tmpArr, i))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(elemType), ev, toLLVMType(elemType), gepReg))
		}

		// bitcast to i8*
		ptrReg := g.tmpReg("slice.ptr")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), ptrReg, arrType, tmpArr))

		// store len (field 0)
		lenGEP := g.tmpReg("vec.len.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

		// store cap (field 1)
		capGEP := g.tmpReg("vec.cap.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
			g.indent(), capGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

		// store data (field 2)
		dataGEP := g.tmpReg("vec.data.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
			g.indent(), dataGEP, tmpVec))
		g.storeDataPtrField(sb, ptrReg, dataGEP)

		structPtr = tmpVec
	}

	g.tmpIdx++
	lbl := g.tmpIdx
	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("arr.step.%d", lbl),
		exit: fmt.Sprintf("arr.end.%d", lbl),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// Load len (field 0 for both %arr and %vec)
	lenGEP := g.tmpReg("arr.len.gep")
	lenLoad := g.tmpReg("arr.len")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, structType, structType, structPtr))
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))

	// Initialize i = 0
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))

	// br → cond
	preheaderBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
	// CFG: preheader → cond
	g.cfgEdge(preheaderBlock, fmt.Sprintf("arr.cond.%d", lbl))
	g.cfgTerm(preheaderBlock, termBr)

	// cond block: i < len
	condLabel := fmt.Sprintf("arr.cond.%d", lbl)
	g.emitLabel(sb, condLabel)
	g.indentLevel++
	iLoad := g.tmpReg("arr.i")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	cmpReg := g.tmpReg("arr.cmp")
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, lenLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%arr.body.%d, label %%arr.end.%d\n", g.indent(), cmpReg, lbl, lbl))
	// CFG: cond → body / end
	g.cfgEdge(condLabel, fmt.Sprintf("arr.body.%d", lbl))
	g.cfgEdge(condLabel, fmt.Sprintf("arr.end.%d", lbl))
	g.cfgTerm(condLabel, termCondBr)
	g.indentLevel--

	// body block
	g.emitLabel(sb, fmt.Sprintf("arr.body.%d", lbl))
	g.indentLevel++

	// Load element from data[i]
	// Data field index: %arr → field 1, %vec/%str-long → field 2
	// (%arr = {i64 len, i64 data}; %vec/%str-long = {i64 len, i64 cap, i64 data})
	dataField := uint32(1)
	if isVec || structType == "%str-long" {
		dataField = 2
	}
	dataGEP := g.tmpReg("arr.data.gep")
	dataLoad := g.tmpReg("arr.data")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, structType, structType, structPtr, dataField))
	dataLoad = g.loadDataPtrField(sb, dataGEP)

	// Bitcast data to element type pointer
	castReg := g.tmpReg("arr.cast")
	ptrType := toLLVMType(elemType) + "*"
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s\n", g.indent(), castReg, dataLoad, ptrType))

	// GEP into element array by index
	elemGEP := g.tmpReg("arr.elem.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s %s, i64 %s\n",
		g.indent(), elemGEP, toLLVMType(elemType), ptrType, castReg, iLoad))

	// Load element value
	elemLoad := g.tmpReg("arr.elem")
	sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), elemLoad, toLLVMType(elemType), ptrType, elemGEP))

	// 小整數類型（i8/u8/i16/u16/i32/u32，如 str 的 char）拓寬到 i64 再存入循環變數
	storeVal := elemLoad
	storeType := elemType
	if elemType == "i1" || elemType == "i8" || elemType == "u8" || elemType == "i16" || elemType == "u16" || elemType == "i32" || elemType == "u32" {
		zextReg := g.tmpReg("arr.zext")
		op := widenExtOp(elemType)
		sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to i64\n", g.indent(), zextReg, op, toLLVMType(elemType), elemLoad))
		storeVal = zextReg
		storeType = "i64"
	}

	// Store element into loop variable
	g.varTypes[varName] = storeType
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %%%s\n", g.indent(), toLLVMType(storeType), storeVal, toLLVMType(storeType), varName))

	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// body 未終止時跳到 step 執行更新
	if !g.blockTerminated {
		bodyTail := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%sbr label %%arr.step.%d\n", g.indent(), lbl))
		// CFG: body tail → step
		g.cfgEdge(bodyTail, fmt.Sprintf("arr.step.%d", lbl))
		g.cfgTerm(bodyTail, termBr)
	}
	g.blockTerminated = false
	g.indentLevel--

	// step block: i++（continue 跳轉目標）
	stepLabel := fmt.Sprintf("arr.step.%d", lbl)
	g.emitLabel(sb, stepLabel)
	g.indentLevel++
	// Update: i++
	iNext := g.tmpReg("arr.next")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
	// CFG: step → cond (back edge)
	g.cfgEdge(stepLabel, condLabel)
	g.cfgTerm(stepLabel, termBr)
	g.indentLevel--

	// end block
	g.emitLabel(sb, fmt.Sprintf("arr.end.%d", lbl))
}

func (g *Generator) generateRangeFor(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	// 字串遍歷: for i in 'hello'
	if ir.RangeStr != "" {
		g.generateStringRange(sb, stmt)
		return
	}

	// 陣列/切片遍歷: for i in a
	if ir.RangeExpr != nil {
		g.generateArrayRange(sb, stmt)
		return
	}

	r := ir.Range
	varName := ir.Variable
	g.tmpIdx++
	lbl := g.tmpIdx
	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("rng.step.%d", lbl),
		exit: fmt.Sprintf("rng.end.%d", lbl),
	})
	// Track the loop variable's upper bound for bounds check elimination.
	// Save the old bound and restore it after the loop body (handles nested loops
	// and variable name reuse across functions).
	oldBound, hadOldBound := int64(0), false
	if g.rangeLoopBounds != nil {
		if old, ok := g.rangeLoopBounds[varName]; ok {
			oldBound = old
			hadOldBound = true
		}
		// Record the new bound if the end is a constant.
		if endLit, ok := r.End.(*parser.IntegerLiteral); ok {
			if r.RightInc {
				g.rangeLoopBounds[varName] = endLit.Value + 1
			} else {
				g.rangeLoopBounds[varName] = endLit.Value
			}
		} else {
			// Non-constant end: remove any stale bound so we don't make
			// incorrect assumptions.
			delete(g.rangeLoopBounds, varName)
		}
	}
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
		// Restore the old bound
		if g.rangeLoopBounds != nil {
			if hadOldBound {
				g.rangeLoopBounds[varName] = oldBound
			} else {
				delete(g.rangeLoopBounds, varName)
			}
		}
	}()

	// 計算 start 和 end 值
	startVal := g.generateExprWithSB(sb, r.Start)
	endVal := g.generateExprWithSB(sb, r.End)

	// Check if both start and end are compile-time constants.
	// If so, the loop direction is known at compile time and we can
	// generate a simple loop with NO per-iteration select instructions.
	startLit, startIsLit := r.Start.(*parser.IntegerLiteral)
	endLit, endIsLit := r.End.(*parser.IntegerLiteral)

	if startIsLit && endIsLit {
		// ================================================================
		// Fast path: both bounds are constants — direction known at compile time.
		// Generates a simple loop with NO select instructions:
		//   cond: single icmp (no select)
		//   step: single add/sub (no select)
		// ================================================================
		ascending := startLit.Value <= endLit.Value

		// Compute initial value: start (left-closed) or start±1 (left-open)
		var iInitVal string
		if r.LeftInc {
			iInitVal = startVal
		} else {
			iInitReg := g.tmpReg("rng.init")
			if ascending {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iInitReg, startVal))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), iInitReg, startVal))
			}
			iInitVal = iInitReg
		}
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInitVal, varName))
		preheaderBlock := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
		// CFG: preheader → cond
		g.cfgEdge(preheaderBlock, fmt.Sprintf("rng.cond.%d", lbl))
		g.cfgTerm(preheaderBlock, termBr)

		// cond block: single comparison (no select!)
		condLabel := fmt.Sprintf("rng.cond.%d", lbl)
		g.emitLabel(sb, condLabel)
		g.indentLevel++
		iLoad := g.tmpReg("rng.i")
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
		cmpReg := g.tmpReg("rng.cmp")
		if ascending {
			cmpOp := "sle"
			if !r.RightInc {
				cmpOp = "slt"
			}
			sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), cmpReg, cmpOp, iLoad, endVal))
		} else {
			cmpOp := "sge"
			if !r.RightInc {
				cmpOp = "sgt"
			}
			sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), cmpReg, cmpOp, iLoad, endVal))
		}
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), cmpReg, lbl, lbl))
		// CFG: cond → body / end
		g.cfgEdge(condLabel, fmt.Sprintf("rng.body.%d", lbl))
		g.cfgEdge(condLabel, fmt.Sprintf("rng.end.%d", lbl))
		g.cfgTerm(condLabel, termCondBr)
		g.indentLevel--

		// body block
		g.emitLabel(sb, fmt.Sprintf("rng.body.%d", lbl))
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		if !g.blockTerminated {
			bodyTail := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%rng.step.%d\n", g.indent(), lbl))
			// CFG: body tail → step
			g.cfgEdge(bodyTail, fmt.Sprintf("rng.step.%d", lbl))
			g.cfgTerm(bodyTail, termBr)
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: simple increment or decrement (no select!)
		stepLabel := fmt.Sprintf("rng.step.%d", lbl)
		g.emitLabel(sb, stepLabel)
		g.indentLevel++
		iLoad2 := g.tmpReg("rng.i2")
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad2, varName))
		iNext := g.tmpReg("rng.next")
		if ascending {
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad2))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), iNext, iLoad2))
		}
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
		// CFG: step → cond (back edge)
		g.cfgEdge(stepLabel, condLabel)
		g.cfgTerm(stepLabel, termBr)
		g.indentLevel--

		// end block
		g.emitLabel(sb, fmt.Sprintf("rng.end.%d", lbl))
		return
	}

	// ================================================================
	// Fast path: start is literal 0, end is non-constant.
	// This is the most common pattern: `i <- [0..n)` for array iteration.
	//
	// When start=0 and the range is right-exclusive `[0..end)`:
	//   - If end >= 0: ascending (0, 1, ..., end-1) — correct
	//   - If end < 0: condition `0 < end` is false, loop doesn't execute
	//     (intuitive behavior — no iterations for negative count)
	//
	// This generates a simple ascending-only loop with NO select and NO
	// per-iteration branch on direction, matching C/Rust for-loop performance.
	// ================================================================
	if startIsLit && startLit.Value == 0 && r.LeftInc && !r.RightInc {
		// i = 0 (left-inclusive)
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))
		preheaderBlock := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
		// CFG: preheader → cond
		g.cfgEdge(preheaderBlock, fmt.Sprintf("rng.cond.%d", lbl))
		g.cfgTerm(preheaderBlock, termBr)

		// cond block: i < end (ascending only, single icmp)
		condLabel := fmt.Sprintf("rng.cond.%d", lbl)
		g.emitLabel(sb, condLabel)
		g.indentLevel++
		iLoad := g.tmpReg("rng.i")
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
		cmpReg := g.tmpReg("rng.cmp")
		sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, endVal))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), cmpReg, lbl, lbl))
		// CFG: cond → body / end
		g.cfgEdge(condLabel, fmt.Sprintf("rng.body.%d", lbl))
		g.cfgEdge(condLabel, fmt.Sprintf("rng.end.%d", lbl))
		g.cfgTerm(condLabel, termCondBr)
		g.indentLevel--

		// body block
		g.emitLabel(sb, fmt.Sprintf("rng.body.%d", lbl))
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		if !g.blockTerminated {
			bodyTail := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%sbr label %%rng.step.%d\n", g.indent(), lbl))
			// CFG: body tail → step
			g.cfgEdge(bodyTail, fmt.Sprintf("rng.step.%d", lbl))
			g.cfgTerm(bodyTail, termBr)
		}
		g.blockTerminated = false
		g.indentLevel--

		// step block: i = i + 1 (simple increment)
		stepLabel := fmt.Sprintf("rng.step.%d", lbl)
		g.emitLabel(sb, stepLabel)
		g.indentLevel++
		iLoad2 := g.tmpReg("rng.i2")
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad2, varName))
		iNext := g.tmpReg("rng.next")
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad2))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
		// CFG: step → cond (back edge)
		g.cfgEdge(stepLabel, condLabel)
		g.cfgTerm(stepLabel, termBr)
		g.indentLevel--

		// end block
		g.emitLabel(sb, fmt.Sprintf("rng.end.%d", lbl))
		return
	}

	// ================================================================
	// General path: at least one bound is non-constant.
	//
	// Uses a single condition block with `select` for the comparison
	// instead of separate ascending/descending blocks with a per-iteration
	// branch. This reduces the loop to 3 blocks (cond, body, step) and
	// eliminates the loop-invariant `br dirCmp` back-edge branch.
	//
	// On ARM64, `select` compiles to a single `CSEL` instruction, and the
	// two `icmp` instructions can be computed in parallel. The eliminated
	// branch — even though perfectly predicted — was preventing LLVM from
	// performing loop unswitching and other loop optimizations because the
	// loop had multiple exits (break statements in the body).
	//
	// Per-iteration cost: 1 load + 2 icmp + 1 select + 1 br (cond)
	//                       1 load + 1 add + 1 store + 1 br (back-edge)
	// ================================================================

	// 計算方向標誌：start <= end 為遞增 (true)，start > end 為遞減 (false)
	dirCmp := g.tmpReg("rng.dircmp")
	sb.WriteString(fmt.Sprintf("%s%s = icmp sle i64 %s, %s\n", g.indent(), dirCmp, startVal, endVal))

	// Pre-compute step: 1 for ascending, -1 for descending (computed once)
	stepReg := g.tmpReg("rng.stepval")
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 1, i64 -1\n", g.indent(), stepReg, dirCmp))

	// 計算初始值 i = start（左閉）或 start+step（左開）
	iInit := g.tmpReg("rng.init")
	if r.LeftInc {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 0\n", g.indent(), iInit, startVal))
	} else {
		// 左開：i = start + step (step is 1 for ascending, -1 for descending)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), iInit, startVal, stepReg))
	}
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInit, varName))

	// Single condition block: uses select to choose asc/desc comparison.
	// No per-iteration branch on dirCmp needed.
	preheaderBlock := g.cfgBlockLabel()
	sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
	// CFG: preheader → cond
	g.cfgEdge(preheaderBlock, fmt.Sprintf("rng.cond.%d", lbl))
	g.cfgTerm(preheaderBlock, termBr)

	condLabel := fmt.Sprintf("rng.cond.%d", lbl)
	g.emitLabel(sb, condLabel)
	g.indentLevel++
	condILoad := g.tmpReg("rng.cond.i")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), condILoad, varName))
	// Ascending comparison: i < end (or i <= end if right-inclusive)
	ascCmp := g.tmpReg("rng.asc.cmp")
	ascOp := "sle"
	if !r.RightInc {
		ascOp = "slt"
	}
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), ascCmp, ascOp, condILoad, endVal))
	// Descending comparison: i > end (or i >= end if right-inclusive)
	descCmp := g.tmpReg("rng.desc.cmp")
	descOp := "sge"
	if !r.RightInc {
		descOp = "sgt"
	}
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), descCmp, descOp, condILoad, endVal))
	// Select the appropriate comparison result based on direction
	condResult := g.tmpReg("rng.cond.result")
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i1 %s, i1 %s\n", g.indent(), condResult, dirCmp, ascCmp, descCmp))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), condResult, lbl, lbl))
	// CFG: cond → body / end
	g.cfgEdge(condLabel, fmt.Sprintf("rng.body.%d", lbl))
	g.cfgEdge(condLabel, fmt.Sprintf("rng.end.%d", lbl))
	g.cfgTerm(condLabel, termCondBr)
	g.indentLevel--

	// body block
	g.emitLabel(sb, fmt.Sprintf("rng.body.%d", lbl))
	g.indentLevel++
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// body 未終止時跳到 step 執行更新
	if !g.blockTerminated {
		bodyTail := g.cfgBlockLabel()
		sb.WriteString(fmt.Sprintf("%sbr label %%rng.step.%d\n", g.indent(), lbl))
		// CFG: body tail → step
		g.cfgEdge(bodyTail, fmt.Sprintf("rng.step.%d", lbl))
		g.cfgTerm(bodyTail, termBr)
	}
	g.blockTerminated = false
	g.indentLevel--

	// step block: i = i + step (single add, no branch on dirCmp!)
	// continue 跳轉目標
	stepLabel := fmt.Sprintf("rng.step.%d", lbl)
	g.emitLabel(sb, stepLabel)
	g.indentLevel++
	stepILoad := g.tmpReg("rng.step.i")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), stepILoad, varName))
	iNext := g.tmpReg("rng.next")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), iNext, stepILoad, stepReg))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	// Back-edge: directly to cond block (no dirCmp branch!)
	sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
	// CFG: step → cond (back edge)
	g.cfgEdge(stepLabel, condLabel)
	g.cfgTerm(stepLabel, termBr)
	g.indentLevel--

	// end block
	g.emitLabel(sb, fmt.Sprintf("rng.end.%d", lbl))
}

func (g *Generator) generateLet(sb *strings.Builder, stmt *parser.LetStatement) {
	name := stmt.Name.Value

	// 追踪通过 `task = run ...` 创建的本地 task 变量（SubTask 2.3）。
	// 仅追踪非合成 let（合成为 match arm 注入，不含 run）。
	if !stmt.IsSynthetic {
		if _, isRun := stmt.Value.(*parser.RunExpression); isRun {
			g.trackLocalTask(name)
		}
	}

	// 處理 match 對應 err/nil arm 注入的合成 let 陳述句（`it = matched`）。
	// 這些 let 的 Type 為 "err" / "nil" / "err | nil" 哨兵字串。
	// - "err" 臂：it = 錯誤消息字符串（%str-long）。err('msg') 裝箱時把
	//   %str-long box 指標 ptrtoint 存入 option data 欄位，此處逆向解箱：
	//   data → load i64 → inttoptr → %str-long* → load → store 到 it。
	//   it 僅為借用視圖（淺拷貝，不 track heap），box 所有權仍屬 option。
	// - "nil" / "err | nil"：語意上無確定值，維持 i64 佔位符。
	if stmt.IsSynthetic {
		if nt, ok := stmt.Type.(*parser.NamedType); ok {
			// 判斷型別字串是否僅由 err/nil 組成（如 "err"、"nil"、"err | nil"），
			// 不含任何具體元素型別（如 "[]byte | err" 仍需綁定 it = inner value）。
			onlyErrNil := true
			for _, p := range strings.Split(nt.Value, "|") {
				p = strings.TrimSpace(p)
				if p == "err" || p == "nil" || p == "" {
					continue
				}
				onlyErrNil = false
				break
			}
			if onlyErrNil {
				// 純 err 臂且 matched 是 option 變數：解箱錯誤消息到 it。
				if nt.Value == "err" {
					if srcIdent, ok := stmt.Value.(*parser.Identifier); ok && g.varTypes != nil {
						if srcType, ok := g.varTypes[srcIdent.Value]; ok && srcType == "%option" {
							// 確保 it 有 alloca（模組級/函數級皆由 collectVarDecls 註冊；
							// 若缺失則就地分配 %str-long）。
							if _, exists := g.varTypes[name]; !exists {
								g.varTypes[name] = "%str-long"
								g.funcLocalNames[name] = true
								sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), llvmVarRef(name)))
								sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 24, i8* %s)\n", g.indent(), llvmVarRef(name)))
								if g.itAllocTypes != nil && name == "it" {
									g.itAllocTypes[name] = "%str-long"
								}
							}
							// 解箱：option data(i64) → inttoptr %str-long* → load
							srcAddr := g.varAddr(srcIdent.Value)
							dataGEP := g.tmpReg("it.err.data.gep")
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, srcAddr))
							dataInt := g.tmpReg("it.err.data")
							sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataInt, dataGEP))
							boxPtr := g.tmpReg("it.err.box")
							sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to %%str-long*\n", g.indent(), boxPtr, dataInt))
							msgVal := g.tmpReg("it.err.msg")
							sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), msgVal, boxPtr))
							// store 到 it（alloca 型別不同時 bitcast；alloca 保證 ≥24B，
							// 由 collectVarDecls 的 max-size 邏輯保障）。
							storeAddr := g.varAddr(name)
							allocType := ""
							if g.itAllocTypes != nil {
								if at, ok := g.itAllocTypes[name]; ok {
									allocType = at
								}
							}
							if allocType == "" {
								allocType = g.varTypes[name]
							}
							if allocType != "" && allocType != "%str-long" {
								castReg := g.tmpReg("it.err.cast")
								sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to %%str-long*\n", g.indent(), castReg, allocType, storeAddr))
								storeAddr = castReg
							}
							sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), msgVal, storeAddr))
							// 後續此臂內讀 it 一律按 %str-long 處理
							// （expr.go 的 itAllocTypes bitcast 機制負責指標型別匹配）。
							g.varTypes[name] = "%str-long"
							return
						}
					}
				}
				if g.varTypes != nil {
					if _, exists := g.varTypes[name]; !exists {
						g.varTypes[name] = "i64"
						g.funcLocalNames[name] = true
						sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), llvmVarRef(name)))
						sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 8, i8* %s)\n", g.indent(), llvmVarRef(name)))
						sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), llvmVarRef(name)))
					}
				}
				return
			}
		}
	}

	// oldValFreed 標記舊值是否已釋放。
	// 對於 RHS 安全（不引用舊值）的型別特定分支（SliceLiteral），在進入分支前立即釋放舊值。
	// 對於一般情況，在 RHS 求值後釋放舊值，避免 `x = f(x)` 造成 use-after-free。
	oldValFreed := false

	// 返回值延遲零值追蹤：顯式賦值到 out 參數時設定對應 bit。
	// 在此統一設定，涵蓋後續所有路徑（move、slice view clone、option assign、延遲綁定、立即 store）。
	// 合成 let（match err/nil arm）不標記，因其語意上無真實值綁定。
	if g.outputParamNames != nil && g.outputParamNames[name] && !stmt.IsSynthetic {
		g.emitSetRetInitBit(sb, name)
	}

	// 堆變數 moved 追蹤：若目標是輸出參數且源是局部堆變數，根據 liveness 決定 clone/move。
	// 此处仅设置 oldValFreed 标志（防止通用路径对 out 参数 freeOldHeapValue），
	// 实际的 move/clone 决策和执行由下方统一赋值逻辑处理。
	// - 源是全局变量 → 禁止 move，一律 clone（全局变量持久存在，不能转移所有权）
	// - 源在后续被引用 → clone（源仍需拥有 data）
	// - 源在后续未被引用 → move（所有权转移，源跳过 free）
	if g.heapVars != nil && g.outputParamNames != nil && g.outputParamNames[name] && g.heapVarIndex != nil && !stmt.IsSynthetic {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if _, isHeap := g.heapVars[ident.Value]; isHeap {
				// move 時不 free out 舊值（舊值可能與前一個 source 共享 data）
				canMove := g.moveEligible != nil && g.moveEligible[stmt]
				if canMove {
					oldValFreed = true
				}
				// clone 時由統一路徑 freeOldHeapValue 釋放舊值
			}
		}
	}

	// 切片視圖賦值：view = arr[0..4]
	// generateSliceViewAssignment 現在總是 clone（needClone=true），新值獨立擁有 data，
	// 不引用舊值。因此釋放目標變數的舊 prologue buffer 是安全的，避免內存洩漏。
	// 注意：舊設計中 generateSliceViewAssignment 可能建立引用舊值的視圖（自切片），
	// 故不釋放舊值。但新設計下自切片也走 clone 路徑，此擔憂已不適用。
	if _, isSliceExpr := stmt.Value.(*parser.SliceExpression); isSliceExpr {
		if !oldValFreed {
			g.freeOldHeapValue(sb, stmt, name)
			oldValFreed = true
		}
		if g.generateSliceViewAssignment(sb, stmt, name) {
			return
		}
		// generateSliceViewAssignment 無法處理（如 base 不是 Identifier）。
		// slice 結果共享原始 data（不 clone），若原始 data 在後續被釋放
		// （如結構體在函數退出時被釋放），目標變數會懸空（use-after-free）。
		// 因此對輸出參數和局部變數目標，都必須 clone slice 的 data。
		if g.outputParamNames != nil && g.outputParamNames[name] {
			g.cloneSliceExprResult(sb, stmt, name)
			return
		}
		if g.funcLocalNames != nil && g.funcLocalNames[name] {
			g.cloneSliceExprResult(sb, stmt, name)
			return
		}
		// 其他情況：回退到原本的 generateSliceExpression 路徑
	}

	// 切片視圖變數賦值給輸出參數或顯式 []T 型別：out = view
	// view 是 slice view alias（RHS 是 Identifier 而非 SliceExpression），
	// 繞過 generateSliceViewAssignment 的 clone 保護（該函數只處理 SliceExpression RHS）。
	// 若目標會逃逸函數作用域，必須 clone view 的 data，
	// 避免共享原數組 data 導致原數組 free 後 out 懸空（use-after-free）。
	if ident, ok := stmt.Value.(*parser.Identifier); ok {
		if g.isSliceViewVar(ident.Value) {
			needClone := false
			if g.outputParamNames != nil && g.outputParamNames[name] {
				needClone = true
			}
			if _, isSliceType := stmt.Type.(*parser.SliceType); isSliceType {
				needClone = true
			}
			if needClone {
				view := g.sliceViews[ident.Value]
				// 釋放目標變數的舊堆值（如有）
				g.freeOldHeapValue(sb, stmt, name)
				// 計算 elemSize
				elemSize := int64(8)
				if view.isStr {
					elemSize = 1
				} else {
					if s := g.llvmTypeSize(view.elemType); s > 0 {
						elemSize = s
					}
				}
				// clone view data 到目標變數（malloc + memcpy + store len/cap/data）
				g.emitSliceClone(sb, name, view.dataPtrReg, view.viewLen, view.elemType, elemSize, view.isStr, "0")
				// 追蹤目標為堆變數（僅局部變數，非輸出參數；輸出參數由呼叫者管理）
				resultType := "%vec"
				if view.isStr {
					resultType = "%str-long"
				}
				if g.outputParamNames == nil || !g.outputParamNames[name] {
					g.trackLocalHeapVar(name, resultType)
				}
				return
			}
		}
	}

	// 切片儲存：使用 %vec 結構體
	_, isSliceLit := stmt.Value.(*parser.SliceLiteral)
	_, isSliceExpr2 := stmt.Value.(*parser.SliceExpression)
	_, isSliceType := stmt.Type.(*parser.SliceType)
	// Skip default slice init when using with-cap/with-len/with-cap-len builtin (type-inferred allocation)
	isWithCapCall := false
	if call, ok := stmt.Value.(*parser.CallExpression); ok {
		if ident, ok := call.Function.(*parser.Identifier); ok && (ident.Value == "with-cap" || ident.Value == "with-len" || ident.Value == "with-cap-len") {
			isWithCapCall = true
		}
	}
	if (isSliceType || isSliceLit) && !isSliceExpr2 && !isWithCapCall {
		// SliceLiteral 的 RHS 是字面量，不引用舊值，可安全在求值前釋放舊值
		if !oldValFreed && isSliceLit {
			g.freeOldHeapValue(sb, stmt, name)
			oldValFreed = true
		}
		// 若變數原本宣告為 %arr（固定大小陣列，alloca 16 字節），但被 SliceLiteral
		// 重新賦值為 %vec（24 字節，3 個欄位），原本的 alloca 空間不足，寫入 field 2
		// 會越界。此時 alloca 一個新的 %vec 變數並通過 varAlias 重定向所有後續存取。
		if g.varTypes != nil && g.varTypes[name] == "%arr" {
			g.tmpIdx++
			vecVarName := fmt.Sprintf("%s.vec.%d", name, g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), llvmVarRef(vecVarName)))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 24, i8* %s)\n", g.indent(), llvmVarRef(vecVarName)))
			g.varAlias[name] = vecVarName
			g.funcLocalNames[vecVarName] = true
			// 從 stackArrVars 移除（不再使用棧陣列路徑）
			if g.stackArrVars != nil {
				delete(g.stackArrVars, name)
			}
		}
		// 註冊變數型別為 %vec，供後續索引賦值/讀取/指標取得使用
		g.varTypes[name] = "%vec"
		// Only mark as local if not already a global variable
		if g.globalVars == nil || !g.globalVars[name] {
			g.funcLocalNames[name] = true
		}
		// 記錄切片元素型別，供 IndexExpression 使用正確型別讀取
		if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
			g.arrayElemTypes[name] = g.mapToLLVMType(st.Elem.String())
			// 嵌套容器（[][]T）：注册内层元素型别
			if g.elemElemTypes != nil {
				if innerType := g.nestedElemLLVMType(st.Elem); innerType != "" {
					g.elemElemTypes[name] = innerType
				}
			}
		} else {
			g.arrayElemTypes[name] = "i64"
		}
		if isSliceLit {
			slice := stmt.Value.(*parser.SliceLiteral)
			elemType := "i64"
			if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
				elemType = g.mapToLLVMType(st.Elem.String())
			}
			n := int64(len(slice.Elements))
			g.tmpIdx++
			tid := g.tmpIdx
			tmpArr := fmt.Sprintf("%%slice.tmp.%d", tid)
			arrType := fmt.Sprintf("[%d x %s]", n, elemType)

			// malloc temp array on heap（而非 alloca 棧分配），
			// 因為 %vec 擁有 data 並在函數結束時 free，棧指標會導致 crash。
			elemSize := g.llvmTypeSize(elemType)
			if elemSize == 0 {
				elemSize = 8
			}
			bufSize := n * elemSize
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), tmpArr, bufSize))

			// bitcast i8* to array type pointer for element GEP
			arrPtr := g.tmpReg("slice.arrptr")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), arrPtr, tmpArr, toLLVMType(arrType)))

			// store each element via GEP
			for i, elem := range slice.Elements {
				ev := g.generateExprWithSB(sb, elem)
				ev = g.stripLLVMType(ev)
				// When elemType is %str-long and ev is a %str-long* pointer (e.g. from
				// a StringLiteral alloca), load the struct value before storing.
				if elemType == "%str-long" {
					ev = g.loadStrValueIfNeeded(sb, ev)
				}
				// When elemType is i64 (untyped slice literal) but the element is a
				// string expression (str-long* pointer), convert via ptrtoint to avoid
				// storing a ptr value into an i64 slot (LLVM type error).
				if elemType == "i64" && g.isStringExpr(elem) && strings.HasPrefix(ev, "%") {
					p2i := g.tmpReg("slice.p2i")
					sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %%str-long* %s to i64\n", g.indent(), p2i, ev))
					ev = p2i
				}
				gepReg := g.tmpReg("slice.gep")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), gepReg, toLLVMType(arrType), toLLVMType(arrType), arrPtr, i))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(elemType), ev, toLLVMType(elemType), gepReg))
			}

			// tmpArr is already i8* (from malloc), use directly as vec data
			ptrReg := tmpArr

			// store len (field 0)
			lenGEP := g.tmpReg("vec.len.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
				g.indent(), lenGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

			// store cap (field 1)
			capGEP := g.tmpReg("vec.cap.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
				g.indent(), capGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

			// store data (field 2)
			dataGEP := g.tmpReg("vec.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
				g.indent(), dataGEP, g.varAddr(name)))
			g.storeDataPtrField(sb, ptrReg, dataGEP)
			// 追蹤局部 vec 變數，用於函數結束時深層 free
			g.trackLocalHeapVar(name, "%vec")
			return
		}

		// Non-literal slice declaration (e.g. `lines []str`): initialize %vec
		// with a default data buffer so push() and index assignment work.
		// 僅當沒有 RHS 值時才執行預設初始化。若有 RHS 值（如 `v []i64 = view`），
		// 必須 fall through 到深層 clone / 一般賦值路徑，避免忽略 RHS 導致 len=0。
		if stmt.Value == nil {
			elemType := "i64"
			if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
				elemType = g.mapToLLVMType(st.Elem.String())
			}
			elemSize := g.llvmTypeSize(elemType)
			if elemSize == 0 {
				elemSize = 8
			}
			defaultCap := int64(1024)
			bufSize := defaultCap * elemSize
			vecBuf := g.tmpReg("vec.init.buf")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), vecBuf, bufSize))
			// store len = 0 (field 0)
			vecLenGEP := g.tmpReg("vec.init.len")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), vecLenGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), vecLenGEP))
			// store cap = defaultCap (field 1)
			vecCapGEP := g.tmpReg("vec.init.cap")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), vecCapGEP, g.varAddr(name)))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), defaultCap, vecCapGEP))
			// store data = buf (field 2)
			vecDataGEP := g.tmpReg("vec.init.data")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), vecDataGEP, g.varAddr(name)))
			g.storeDataPtrField(sb, vecBuf, vecDataGEP)
			// 追蹤局部 vec 變數，用於函數結束時深層 free
			g.trackLocalHeapVar(name, "%vec")
			return
		}
		// 有 RHS 值但非 SliceLiteral：型別註冊已在上方完成，
		// fall through 到深層 clone / 一般賦值路徑評估 RHS。
	}

	// Option type assignment: handle nil, val(), err(), and implicit values
	llvmTypeCheck := g.varLLVMType(stmt)
	// Also check if variable already has %option type (reassignment)
	if llvmTypeCheck != "%option" && g.varTypes != nil {
		if t, ok := g.varTypes[stmt.Name.Value]; ok && t == "%option" {
			llvmTypeCheck = "%option"
		}
	}
	// 若變數有明確非 option 型別（如函數輸出參數 fd i64），
	// 優先使用變數型別而非從 funcRetTypes 推斷的 %option。
	// 解決 fs.no 中 open-write-raw 內 fd = open-write(p) 場景：
	// varLLVMType 從 funcRetTypes 推斷為 %option，但 fd 是 i64，實際 dispatch 走 builtin 返回 i64。
	// 但 synthetic `it` 綁定（來自 match desugar）不在此列：
	// it 的值來自 option 變數，必須走 option 賦值路徑，不能被 nil arm 的 i64 佔位符覆蓋。
	if llvmTypeCheck == "%option" && g.varTypes != nil && !stmt.IsSynthetic {
		if t, ok := g.varTypes[stmt.Name.Value]; ok && t != "" && t != "%option" {
			llvmTypeCheck = t
		}
	}
	if llvmTypeCheck == "%option" {
		// Ensure variable is allocated (needed for `it = x` injected in match arm bodies)
		if g.varTypes != nil {
			if _, exists := g.varTypes[name]; !exists {
				g.varTypes[name] = "%option"
				g.funcLocalNames[name] = true
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%option\n", g.indent(), llvmVarRef(name)))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 16, i8* %s)\n", g.indent(), llvmVarRef(name)))
			}
		}
		// Track inner type for ?T option variables
		if g.optionInnerTypes != nil {
			if _, exists := g.optionInnerTypes[name]; !exists {
				// From explicit type annotation (e.g. val ?f64)
				if nt, ok := stmt.Type.(*parser.NullableType); ok {
					g.optionInnerTypes[name] = g.mapToLLVMType(nt.Type.String())
				}
			}
			if _, exists := g.optionInnerTypes[name]; !exists {
				// From option variable assignment (e.g. it = n where n is ?i64)
				if ident, ok := stmt.Value.(*parser.Identifier); ok {
					if srcInner, ok := g.optionInnerTypes[ident.Value]; ok && srcInner != "" {
						g.optionInnerTypes[name] = srcInner
					}
				}
			}
			if _, exists := g.optionInnerTypes[name]; !exists {
				// From function call return type (e.g. f = '3.14'.to-f64())
				if call, ok := stmt.Value.(*parser.CallExpression); ok {
					fnName := ""
					if ident, ok := call.Function.(*parser.Identifier); ok {
						fnName = ident.Value
					} else if dot, ok := call.Function.(*parser.DotExpression); ok {
						if recv, ok := dot.Receiver.(*parser.Identifier); ok {
							if recvType, ok := g.varTypes[recv.Value]; ok {
								srcType := strings.TrimPrefix(recvType, "%")
								fnName = srcType + "." + dot.Property
							} else {
								// Receiver is not a variable → module-prefixed call
								// (e.g. tls.server-accept → fnName = "tls.server-accept")
								fnName = recv.Value + "." + dot.Property
							}
						}
						// Also try string literal receiver
						if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
							fnName = "str." + dot.Property
						}
					}
				if fnName != "" && g.funcResultInnerTypes != nil {
					if innerTypes, ok := g.funcResultInnerTypes[fnName]; ok && len(innerTypes) >= 1 && innerTypes[0] != "" {
						g.optionInnerTypes[name] = innerTypes[0]
					} else if idx := strings.Index(fnName, "."); idx >= 0 {
						// Fallback: try short name without module prefix (e.g. "http.do-req" → "do-req")
						shortName := fnName[idx+1:]
						if innerTypes, ok := g.funcResultInnerTypes[shortName]; ok && len(innerTypes) >= 1 && innerTypes[0] != "" {
							g.optionInnerTypes[name] = innerTypes[0]
						}
					}
				}
				}
			}
		}
		g.generateOptionAssign(sb, stmt)
		return
	}

	// Determine target type for type-inferred builtins (e.g. with-cap)
	g.currentTargetType = ""
	g.currentTargetElemType = ""
	if stmt.Type != nil {
		g.currentTargetType = g.mapToLLVMType(stmt.Type.String())
		if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
			g.currentTargetElemType = g.mapToLLVMType(st.Elem.String())
		}
	} else if g.varTypes != nil {
		if t, ok := g.varTypes[name]; ok {
			g.currentTargetType = t
		}
	}
	// Fallback: look up element type from arrayElemTypes (for inferred assignments)
	if g.currentTargetElemType == "" && g.arrayElemTypes != nil {
		if et, ok := g.arrayElemTypes[name]; ok {
			g.currentTargetElemType = et
		}
	}

	// For with-cap/with-len/with-cap-len with slice type: register element type
	// (skipped the default slice init path, so need manual element type registration)
	// Note: alloca is already done during variable declaration collection phase.
	if isWithCapCall && isSliceType {
		if g.currentTargetElemType != "" {
			g.arrayElemTypes[name] = g.currentTargetElemType
		} else {
			g.arrayElemTypes[name] = "i64"
		}
	}

	// 统一赋值逻辑：b = a，其中 a 是堆拥有型变量。
	// - 源是全局变量 → 禁止 move，一律深層 clone
	// - 源是局部变量且后续未被引用 → move（浅拷贝 + 标记 moved，源跳过 free）
	// - 源是局部变量且后续被引用 → 深層 clone（两者各自独立拥有 data）
	// - 目标是全局变量 → 也走 clone/move 路径（freeOldHeapValue 已支持全局）
	// 巢狀容器（%vec/%arr 元素為 %vec/%arr）因子元素型別未知，無法安全深層 clone，
	// 但 move 仍然安全（move 只是浅拷贝 + 标记，不涉及递归）。
	// canClone==false 时退化路径：forced move（浅拷贝 + 标记 moved），防止 double-free。
	//
	// Synthetic `it = matched` bindings (injected by match expression desugar)
	// are also included when the source is NOT an %option variable. For option
	// sources, the `it` binding only borrows the inner value (shallow copy);
	// the option box retains data ownership, so clone/move must be skipped
	// (handled by the option-specific synthetic paths above). For non-option
	// heap-owning sources (e.g. `cmd: { 'x' -> ... }` where cmd is %str-long),
	// the synthetic `it = cmd` must deep-clone to give `it` an independent data
	// buffer — otherwise both `it` and `cmd` share the same data pointer and
	// emitHeapFree double-frees it at function exit.
	if g.heapVars != nil {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if ident.Value != name {
				// Skip synthetic bindings when source is an option variable:
				// option `it` extraction borrows the value (shallow copy),
				// the option box owns the data — clone would double-free.
				skipSynthetic := false
				if stmt.IsSynthetic {
					if srcType, hasType := g.varTypes[ident.Value]; hasType && srcType == "%option" {
						skipSynthetic = true
					}
				}
				if !skipSynthetic {
				_, isLocal := g.funcLocalNames[name]
				isOutput := g.outputParamNames != nil && g.outputParamNames[name]
				isGlobal := g.globalVars != nil && g.globalVars[name] && (g.funcLocalNames == nil || !g.funcLocalNames[name])

				// 情况 1：源是全局堆拥有型变量 → 一律 clone
				// 注意：必须排除局部变量（参数/局部），因为局部变量可能与全局变量同名。
				if g.globalVars != nil && g.globalVars[ident.Value] && g.funcLocalNames != nil && !g.funcLocalNames[ident.Value] {
					if srcType, hasType := g.varTypes[ident.Value]; hasType && g.isHeapOwningType(srcType) {
						srcElemType := ""
						if g.arrayElemTypes != nil {
							srcElemType = g.arrayElemTypes[ident.Value]
						}
						canClone := true
						srcElemElemType := ""
						if g.elemElemTypes != nil {
							srcElemElemType = g.elemElemTypes[ident.Value]
						}
						// 所有型別均可深層 clone：嵌套容器透過 elemElemType 機制遞迴處理，
						// 用戶結構體透過 emitStructClone 遞迴處理。
						if canClone && (isLocal || isOutput || isGlobal) {
							g.freeOldHeapValue(sb, stmt, name)
							g.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcType, srcElemType, srcElemElemType)
							if !isOutput && !isGlobal {
								g.trackLocalHeapVar(name, srcType)
							}
							// 設定變數型別，使後續方法呼叫能正確解析
							if g.varTypes == nil {
								g.varTypes = make(map[string]string)
							}
							g.varTypes[name] = srcType
							if srcElemType != "" {
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[name] = srcElemType
								}
								if isGlobal && g.moduleArrayElemTypes != nil {
									g.moduleArrayElemTypes[name] = srcElemType
								}
							}
							return
						}
					}
				}

				// 情况 1.5：源是函数参数（堆拥有型别）→ 一律 clone
				// 参数通过指针传递（指向调用者的变量），调用者仍拥有原始数据。
				// 不能 move：move 需要修改源的数据字段（置零），这会影响调用者的变量，
				// 导致调用者在函数返回后使用该变量时崩溃（use-after-free）。
				// 因此参数→输出参数/局部变量 赋值时，必须深拷贝，使目标拥有独立的 data。
				if g.funcParams != nil && g.funcParams[ident.Value] {
					if srcType, hasType := g.varTypes[ident.Value]; hasType && g.isHeapOwningType(srcType) {
						srcElemType := ""
						if g.arrayElemTypes != nil {
							srcElemType = g.arrayElemTypes[ident.Value]
						}
						canClone := true
						srcElemElemType := ""
						if g.elemElemTypes != nil {
							srcElemElemType = g.elemElemTypes[ident.Value]
						}
						// 所有型別均可深層 clone：嵌套容器透過 elemElemType 機制遞迴處理，
						// 用戶結構體透過 emitStructClone 遞迴處理。
						if canClone && (isLocal || isOutput || isGlobal) {
							g.freeOldHeapValue(sb, stmt, name)
							g.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcType, srcElemType, srcElemElemType)
							if !isOutput && !isGlobal {
								g.trackLocalHeapVar(name, srcType)
							}
							// 設定變數型別
							if g.varTypes == nil {
								g.varTypes = make(map[string]string)
							}
							g.varTypes[name] = srcType
							if srcElemType != "" {
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[name] = srcElemType
								}
								if isGlobal && g.moduleArrayElemTypes != nil {
									g.moduleArrayElemTypes[name] = srcElemType
								}
							}
							return
						}
					}
				}

				// 情况 2：源是局部堆拥有型变量
				if srcHeapType, isHeap := g.heapVars[ident.Value]; isHeap {
					if isLocal || isOutput || isGlobal {
						srcElemType := ""
						if g.arrayElemTypes != nil {
							srcElemType = g.arrayElemTypes[ident.Value]
						}
						// 检查是否可以 move（源未被后续引用）
						canMove := g.moveEligible != nil && g.moveEligible[stmt]
						if canMove {
							// move：浅拷贝结构体 + 标记源为 moved
							// 对输出参数不 free 旧值（旧值可能与前一个 source 共享 data）
							if !isOutput {
								g.freeOldHeapValue(sb, stmt, name)
							}
							moveReg := g.tmpReg("move.val")
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
								g.indent(), moveReg, srcHeapType, srcHeapType, g.varAddr(ident.Value)))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
								g.indent(), srcHeapType, moveReg, srcHeapType, g.varAddr(name)))
							if isOutput {
								// move 到输出参数：用 handleMoveToOut（含 outBindState 管理）
								g.handleMoveToOut(sb, ident.Value, name)
							} else {
								// move 到局部变量/全局变量：用 handleMoveLocal（仅设 bit）
								g.handleMoveLocal(sb, ident.Value)
								if !isGlobal {
									g.trackLocalHeapVar(name, srcHeapType)
								}
							}
							if srcElemType != "" {
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[name] = srcElemType
								}
								if isGlobal && g.moduleArrayElemTypes != nil {
									g.moduleArrayElemTypes[name] = srcElemType
								}
							}
							return
						}
						// clone：深層 clone（源仍需拥有 data）
						canClone := true
						srcElemElemType := ""
						if g.elemElemTypes != nil {
							srcElemElemType = g.elemElemTypes[ident.Value]
						}
						// 所有型別均可深層 clone：嵌套容器透過 elemElemType 機制遞迴處理，
						// 用戶結構體透過 emitStructClone 遞迴處理。
						if canClone {
							g.freeOldHeapValue(sb, stmt, name)
							g.emitDeepClone(sb, g.varAddr(ident.Value), g.varAddr(name), srcHeapType, srcElemType, srcElemElemType)
							if !isOutput && !isGlobal {
								g.trackLocalHeapVar(name, srcHeapType)
							}
							// 設定變數型別
							if g.varTypes == nil {
								g.varTypes = make(map[string]string)
							}
							g.varTypes[name] = srcHeapType
							if srcElemType != "" {
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[name] = srcElemType
								}
								if isGlobal && g.moduleArrayElemTypes != nil {
									g.moduleArrayElemTypes[name] = srcElemType
								}
							}
							if srcElemElemType != "" {
								if g.elemElemTypes != nil {
									g.elemElemTypes[name] = srcElemElemType
								}
								if isGlobal && g.moduleElemElemTypes != nil {
									g.moduleElemElemTypes[name] = srcElemElemType
								}
							}
							return
						}
					}
			}
			} // close if !skipSynthetic
		}
	}
	// 深層 clone 路徑：x = vec[i] / arr[i]，其中元素為堆擁有型別（如 %str-long）。
		// vec[i] 的 codegen 會 load 出 %str-long 值（淺拷貝 {len, cap, data}），
		// 使 x.data 與 vec[i].data 指向同一塊堆記憶體。函數結束時 emitHeapFree 會
		// 同時 free x（釋放 x.data）和 vec（深層 free 釋放每個元素的 data），導致
		// vec[i].data 被 double-free → heap corruption → SIGABRT/SIGTRAP。
		// 修復：對堆擁有型別元素進行深層 clone，使 x 擁有獨立的 data 緩衝區。
		if idxExpr, ok := stmt.Value.(*parser.IndexExpression); ok {
			if ident, ok := idxExpr.Left.(*parser.Identifier); ok {
				if srcType, ok := g.varTypes[ident.Value]; ok && (srcType == "%vec" || srcType == "%arr") {
					srcElemType := "i64"
					if g.arrayElemTypes != nil {
						if et, ok := g.arrayElemTypes[ident.Value]; ok {
							srcElemType = et
						}
					}
					// 僅對堆擁有型別元素深層 clone（%str-long/用戶結構體）；
					// 純量元素（i64/i8/...）淺拷貝即可，無所有權問題。
					if g.isHeapOwningType(srcElemType) {
						_, isLocal := g.funcLocalNames[name]
						isOutput := g.outputParamNames != nil && g.outputParamNames[name]
					if isLocal && !isOutput && ident.Value != name {
						// All types are cloneable (including nested containers like [][]i64)
						canClone := true
						if canClone {
								// 釋放目標變數的舊堆值（如有）
								g.freeOldHeapValue(sb, stmt, name)
								// 取得 vec/arr 元素指標（含 bounds check）
								elemPtr := g.generateIndexExprPtr(sb, idxExpr)
								// 深層 clone 元素到目標變數（malloc 新 data + memcpy）
								// 若元素本身是容器（如 [][]str 的 c[0] → %vec），
								// 需傳遞其元素型別（elemElemType）作為 elemType，
								// 使 emitContainerClone 正確計算元素大小並遞迴 clone。
								innerElemType := ""
								if g.elemElemTypes != nil {
									if eet, ok := g.elemElemTypes[ident.Value]; ok {
										innerElemType = eet
									}
								}
								g.emitDeepClone(sb, elemPtr, g.varAddr(name), srcElemType, innerElemType)
							// 追蹤目標變數為堆變數
							g.trackLocalHeapVar(name, srcElemType)
							// 設定變數型別，使後續方法呼叫能正確解析
							if g.varTypes == nil {
								g.varTypes = make(map[string]string)
							}
							g.varTypes[name] = srcElemType
							// 傳播 elemType: 如果源是嵌套容器（如 [][]i64），
							// 目標變數的元素型別應為源的 elemElemType（如 i64）
							if g.arrayElemTypes != nil {
								// 查找源的 elemElemType（嵌套容器的內層元素型別）
								if ident, ok := idxExpr.Left.(*parser.Identifier); ok {
									if g.elemElemTypes != nil {
										if eet, ok := g.elemElemTypes[ident.Value]; ok {
											g.arrayElemTypes[name] = eet
										}
									}
								}
							}
							return
							}
						}
					}
				}
	}
}

	// 深層 clone 路徑：x = struct.field[i]，其中 field 是 %vec/%arr 且元素為堆擁有型別。
	// 例如 r0 = m2.rows[0]，其中 m2.rows 是 [][]str，rows[0] 是 []str（%vec）。
	// 需深層 clone 以避免 data 指標共享，並傳播 elemType/elemElemType。
	if idxExpr, ok := stmt.Value.(*parser.IndexExpression); ok {
		if dot, ok := idxExpr.Left.(*parser.DotExpression); ok {
			fieldType := g.exprResultLLVMType(dot)
			if fieldType == "%vec" || fieldType == "%arr" {
				// 從 struct 定義中取得 field 的 elemType 和 elemElemType
				srcElemType := ""
				srcElemElemType := ""
				recvType := g.exprResultLLVMType(dot.Receiver)
				if g.isStructLLVMType(recvType) {
					structName := strings.TrimPrefix(recvType, "%")
					if fields, _ := g.resolveStructFields(structName); fields != nil {
						for _, f := range fields {
							if f.name == dot.Property && (f.typ == "%vec" || f.typ == "%arr") {
								srcElemType = f.elemType
								srcElemElemType = f.elemElemType
								break
							}
						}
					}
				}
				if srcElemType == "" {
					srcElemType = "i64"
				}
				// 僅對堆擁有型別元素深層 clone
				if g.isHeapOwningType(srcElemType) {
					_, isLocal := g.funcLocalNames[name]
					isOutput := g.outputParamNames != nil && g.outputParamNames[name]
					if isLocal && !isOutput {
						g.freeOldHeapValue(sb, stmt, name)
						elemPtr := g.generateIndexExprPtr(sb, idxExpr)
						g.emitDeepClone(sb, elemPtr, g.varAddr(name), srcElemType, srcElemElemType)
						g.trackLocalHeapVar(name, srcElemType)
						if g.varTypes == nil {
							g.varTypes = make(map[string]string)
						}
						g.varTypes[name] = srcElemType
						// 傳播 elemType/elemElemType
						if g.arrayElemTypes != nil {
							g.arrayElemTypes[name] = srcElemElemType
						}
						return
					}
				}
			}
		}
	}

	// Deep clone path for struct field read: x = s.field  /  out = s.field
	// where field is a heap-owning type (%str-long, %vec, user struct).
	// generateDotExpression loads the field value (shallow copy of {len, cap, data}),
	// making x.data share the same buffer as s.field.data. At function exit,
	// emitHeapFree would free x.data, corrupting s.field.data → use-after-free.
	// Fix: deep clone the field so the target owns an independent data buffer.
	//
	// This applies to local variables, output parameters, and global variables.
	// For output parameters (out = s.field), shallow copy would share the data
	// pointer with the struct field. When the struct is later freed or the field
	// is reassigned, the output parameter's data pointer becomes dangling
	// (use-after-free). Deep clone ensures the output parameter owns independent
	// data, which the caller then manages.
	if dotExpr, ok := stmt.Value.(*parser.DotExpression); ok {
		fieldType := g.exprResultLLVMType(dotExpr)
		if fieldType != "" && g.isHeapOwningType(fieldType) {
			_, isLocal := g.funcLocalNames[name]
			isOutput := g.outputParamNames != nil && g.outputParamNames[name]
			isGlobal := g.globalVars != nil && g.globalVars[name] && (g.funcLocalNames == nil || !g.funcLocalNames[name])
			if isLocal || isOutput || isGlobal {
				canClone := true
				if canClone {
					g.freeOldHeapValue(sb, stmt, name)
					fieldPtr := g.generateExprPtr(sb, dotExpr)
					g.emitDeepClone(sb, fieldPtr, g.varAddr(name), fieldType, "")
					// Only track local (non-output, non-global) variables as heap vars.
					// Output parameters are managed by the caller; globals are freed
					// by emitGlobalHeapFree.
					if !isOutput && !isGlobal {
						g.trackLocalHeapVar(name, fieldType)
					}
				// 設定變數型別，使後續方法呼叫能正確解析
				if g.varTypes == nil {
					g.varTypes = make(map[string]string)
				}
				g.varTypes[name] = fieldType
					return
				}
			}
		}
	}

		// Deep clone path for struct field array element: s = .sessions[idx]
		// where .sessions is [N]session (inline array field of a struct).
		// generateStructFieldIndexRead returns a GEP pointer for struct elements,
		// not a loaded value. Without this path, generateLet would try to
		// store the pointer as a struct value (type mismatch: ptr vs %session).
		// For heap-owning element types (struct with str/vec fields), use deep
		// clone to avoid double-free. For non-heap-owning struct types, load
		// the value from the pointer before storing.
		if idxExpr, ok := stmt.Value.(*parser.IndexExpression); ok {
			if dot, ok := idxExpr.Left.(*parser.DotExpression); ok {
				// Determine the receiver struct name
				recvName := ""
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					recvName = ident.Value
				}
				structName := ""
				if recvName != "" {
					if t, ok := g.varTypes[recvName]; ok {
						structName = strings.TrimPrefix(t, "%")
					}
				} else {
					recvType := g.exprResultLLVMType(dot.Receiver)
					if g.isStructLLVMType(recvType) {
						structName = strings.TrimPrefix(recvType, "%")
					}
				}
				// Find the field's array type and extract element type
				fieldElemType := ""
				if structName != "" {
					if fields, ok := g.structTypes[structName]; ok {
						for _, f := range fields {
							if f.name == dot.Property && strings.HasPrefix(f.typ, "[") {
								closeB := strings.IndexByte(f.typ, ']')
								if closeB > 0 {
									inner := f.typ[1:closeB]
									xIdx := strings.LastIndex(inner, " x ")
									if xIdx >= 0 {
										fieldElemType = inner[xIdx+3:]
									}
								}
								break
							}
						}
					}
				}
				if fieldElemType != "" && g.isHeapOwningType(fieldElemType) {
					_, isLocal := g.funcLocalNames[name]
					isOutput := g.outputParamNames != nil && g.outputParamNames[name]
					isGlobal := g.globalVars != nil && g.globalVars[name] && (g.funcLocalNames == nil || !g.funcLocalNames[name])
					if isLocal || isOutput || isGlobal {
						// All types are cloneable
						canClone := true
						if canClone {
							g.freeOldHeapValue(sb, stmt, name)
							// Get element pointer via generateStructFieldIndexRead
							// (returns GEP pointer for struct elements)
							elemPtr := g.generateExprWithSB(sb, stmt.Value)
							if elemPtr != "" && elemPtr != "0" {
								g.emitDeepClone(sb, elemPtr, g.varAddr(name), fieldElemType, "")
						if !isOutput && !isGlobal {
							g.trackLocalHeapVar(name, fieldElemType)
						}
						// 設定變數型別，使後續方法呼叫能正確解析
						if g.varTypes == nil {
							g.varTypes = make(map[string]string)
						}
						g.varTypes[name] = fieldElemType
								if g.arrayElemTypes != nil {
									g.arrayElemTypes[name] = fieldElemType
								}
							}
							return
						}
						// Fallback: can't deep clone (nested containers), use load+store
						// This creates a shallow copy, which may cause double-free for
						// heap-owning types, but is better than type-mismatch crash.
						g.freeOldHeapValue(sb, stmt, name)
						elemPtr := g.generateExprWithSB(sb, stmt.Value)
						if elemPtr != "" && elemPtr != "0" {
							loadReg := g.tmpReg("sf.arr.val")
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
								g.indent(), loadReg, toLLVMType(fieldElemType), toLLVMType(fieldElemType), elemPtr))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
								g.indent(), toLLVMType(fieldElemType), loadReg, toLLVMType(fieldElemType), g.varAddr(name)))
						}
						return
					}
				}
			}
		}
	}

	// Propagate element type when assigning a struct field of %vec/%arr type
	// to a local variable (e.g. `old-keys = .keys` in hashmap.rehash).
	// Without this, g.arrayElemTypes[name] is unset and subsequent element
	// access (old-keys[i]) defaults to i64, causing LLVM type mismatch when
	// the field's element type is %str-long or another heap-owning type.
	if dot, ok := stmt.Value.(*parser.DotExpression); ok && g.arrayElemTypes != nil {
		if recvIdent, ok := dot.Receiver.(*parser.Identifier); ok {
			if recvType, ok := g.varTypes[recvIdent.Value]; ok {
				structName := strings.TrimPrefix(recvType, "%")
				if fields, ok := g.structTypes[structName]; ok {
					for _, f := range fields {
						if f.name == dot.Property && f.typ == "%vec" {
							g.arrayElemTypes[name] = f.elemType
							break
						}
					}
				}
			}
		}
	}

	val := g.generateExprWithSB(sb, stmt.Value)
	val = g.stripLLVMType(val)

	// 若 RHS 求值产生了语句级临时堆对象（如 str 拼接结果），
	// 变量将通过 trackLocalHeapVar/heapVars 接管 data 所有权，
	// 从 stmtTemporaries 移除以避免语句结束时的 double-free。
	// 非临时对象（字符串字面量、已有变量等）不在列表中，移除为 no-op。
	g.untrackStmtTemporary(val)

	// 一般情況：在 RHS 求值後釋放舊值，避免 `x = f(x)` 造成 use-after-free。
	// 型別特定分支（SliceLiteral）已在進入分支前釋放並設置 oldValFreed。
	if !oldValFreed {
		g.freeOldHeapValue(sb, stmt, name)
	}
	// 在清除 currentTargetType 前保存值的實際型別，
	// 供後續賦值時的型別轉換使用（zext/trunc）。
	valActualType := ""
	if strings.HasPrefix(val, "%") {
		valActualType = g.intExprLLVMType(stmt.Value)
	}
	llvmType := g.varLLVMType(stmt)
	g.currentTargetType = ""     // clear after use
	g.currentTargetElemType = "" // clear after use
	// For assignments (Type=nil) to %arr variables, varLLVMType may return
	// the function's raw result type (e.g. [16 x i8]) instead of %arr.
	// Override with the variable's declared type so the switch matches %arr.
	if stmt.Type == nil && g.varTypes != nil {
		if t, ok := g.varTypes[name]; ok && t == "%arr" {
			llvmType = "%arr"
		}
	}

	// Empty string assignment optimization: s = "" → set len=0 in-place, no storage switch
	// str-long: store i64 0 to field 0, cap/ptr unchanged
	if llvmType == "%str-long" {
		if strLit, ok := stmt.Value.(*parser.StringLiteral); ok && len(strLit.Value) == 0 {
			storeAddr := g.varAddr(name)
			lenGEP := g.tmpReg("sc.len.gep")
			// str-long: field 0 is i64 len
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, storeAddr))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
			}
			return
		}
	}

	// Synthetic `it = matched` for `ok` arm of a match on ?T:
	// matched is an option variable whose inner type is a struct
	// (e.g. ?file), and the target `it` is the inner struct type.
	// generateExprWithSB returns a pointer to the option's data field
	// (bitcast to inner struct pointer) for struct inner types.
	// We need to load the struct value before storing into `it`.
	// Additionally, `it` may be allocated with a different struct type
	// (e.g. %tls.server-conn) when shared across multiple matches with different
	// element types (e.g. ?str → %str-long, ?tls.server-conn → %tls.server-conn).
	// In that case, use the source option's inner type for the load, and bitcast
	// the `it` alloca to the inner type for the store.
	if stmt.IsSynthetic && g.isStructLLVMType(llvmType) {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			// Detect whether the source is an option variable whose inner
			// value was extracted as a struct pointer by generateExprWithSB.
			isOptionSrc := false
			if g.varTypes != nil {
				if srcType, ok := g.varTypes[ident.Value]; ok && srcType == "%option" {
					isOptionSrc = true
				}
			}
			if !isOptionSrc && g.ssaTypes != nil {
				if ssaType, ok := g.ssaTypes[val]; ok && ssaType == llvmType+"*" {
					isOptionSrc = true
				}
			}
			if isOptionSrc && strings.HasPrefix(val, "%") {
				// Determine the source option's inner type.
				// When the source is ?T where T is a struct (e.g. ?str), generateExprWithSB
				// returned a pointer (via inttoptr) we can load from. When the source is
				// ?T where T is primitive (e.g. ?i64), generateExprWithSB returned the raw
				// i64 value, NOT a pointer — loading from it triggers LLVM type errors.
				srcInnerType := ""
				if g.optionInnerTypes != nil {
					srcInnerType = g.optionInnerTypes[ident.Value]
				}
				// Only treat as struct when srcInnerType is a known non-empty struct type.
					// When srcInnerType is "" (unknown, e.g. tls.conn.send returns ?i64 but
					// optionInnerTypes is not populated), treat as primitive to avoid loading
					// from a raw i64 value as if it were a pointer (which causes trace/BPT trap).
			if srcInnerType != "" && g.isStructLLVMType(srcInnerType) {
				// Source option holds a struct pointer — load the struct value,
				// then DEEP CLONE it into `it` so `it` owns independent heap data.
				// Previously this was a shallow copy (store struct value directly),
				// which shared data pointers with the option's heap box. When `it`
				// was freed at function exit, the option box's data became dangling,
				// causing use-after-free / double-free / SIGSEGV.
				// Deep clone allocates new data buffers, so `it` and the option box
				// each own independent heap data and can be freed independently.
				loadType := srcInnerType
				loadReg := g.tmpReg("it.syn.load")
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, loadType, loadType, val))
				// If the load type differs from llvmType (it's allocated type),
				// bitcast the alloca pointer so the store uses the correct type.
				dstPtr := g.varAddr(name)
				if loadType != llvmType {
					castReg := g.tmpReg("it.syn.cast")
					sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to %s*\n", g.indent(), castReg, llvmType, dstPtr, loadType))
					dstPtr = castReg
				}
				// Stage loaded value into a temp alloca for emitDeepClone
				// (which requires pointers, not SSA values).
				srcTmp := g.tmpReg("it.syn.src")
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), srcTmp, loadType))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), loadType, loadReg, loadType, srcTmp))
				// Determine element type for container types (%vec/%arr/%str-long)
				// so emitDeepClone recursively clones elements.
				elemType := ""
				if loadType == "%vec" || loadType == "%arr" {
					if g.arrayElemTypes != nil {
						if et, ok := g.arrayElemTypes[name]; ok && et != "" {
							elemType = et
						}
					}
				} else if loadType == "%str-long" {
					elemType = "i8"
				}
				g.emitDeepClone(sb, srcTmp, dstPtr, loadType, elemType)
				// Update varTypes so reads of `it` in this arm use the correct type
				if g.varTypes != nil {
					g.varTypes[name] = loadType
				}
				// Track `it` as a heap variable (it now owns independent heap data)
				g.trackLocalHeapVar(name, loadType)
				// Propagate element type for container types
				if elemType != "" && g.arrayElemTypes != nil {
					g.arrayElemTypes[name] = elemType
				}
				return
			} else {
					// Source option holds a primitive (e.g. ?i64). `val` is the raw i64 value,
					// not a pointer. `it` may have been pre-allocated as a struct type
					// (e.g. %str-long) from a previous match with a different element type;
					// in that case the arm body does not use `it` (types would not match),
					// so we store the i64 via a bitcast instead of treating the value as a
					// pointer. Without this, LLVM reports:
					//   '%w.data.val.N' defined with type 'i64' but expected 'ptr'
					if llvmType != "i64" {
						castReg := g.tmpReg("it.syn.cast")
						sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i64*\n", g.indent(), castReg, llvmType, g.varAddr(name)))
						sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), val, castReg))
						return
					}
				}
			}
		}
	}

	// Synthetic `it = matched` where `it` was pre-allocated as i64 (from a
	// nil/err arm) but the matched value is a struct pointer extracted from
	// an option (generateExprWithSB returns %var.data.ptr.N via inttoptr).
	// The pointer must be converted back to i64 via ptrtoint before storing
	// into the i64 alloca. Without this, LLVM reports:
	//   '%var.data.ptr.N' defined with type 'ptr' but expected 'i64'
	// Additionally, varTypes is updated to the struct type so that subsequent
	// field access (it.field) and method resolution (it.field.method()) work.
	// The i64 alloca holds a ptrtoint'd struct pointer; field access codegen
	// detects the i64-alloca-with-struct-type case and generates load+inttoptr.
	syntheticStructPtrType := ""
	if stmt.IsSynthetic && llvmType == "i64" {
		if ident, ok := stmt.Value.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if srcType, ok := g.varTypes[ident.Value]; ok && srcType == "%option" {
					if g.optionInnerTypes != nil {
						if innerType, ok := g.optionInnerTypes[ident.Value]; ok && g.isStructLLVMType(innerType) {
							if strings.HasPrefix(val, "%") && strings.Contains(val, ".data.ptr.") {
								ptrToIntReg := g.tmpReg("it.p2i")
								sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %s* %s to i64\n", g.indent(), ptrToIntReg, innerType, val))
								val = ptrToIntReg
								// Remember struct type for varTypes update below
								// (the generic update at line ~5666 would overwrite
								// to i64, so we re-apply after it).
								syntheticStructPtrType = innerType
							}
						}
					}
				}
			}
		}
	}

	// 若變數已有整數型別（如函數輸出參數 yes bool 為 i1），
	// 但值是字面常量（如 true → "1"），使用變數的型別以避免型別不匹配
	// （例如 store i64 1, i64* %yes 但 %yes 是 alloca i1）。
	if existingType, ok := g.varTypes[name]; ok && g.isIntegerLLVMType(existingType) && g.isIntegerLLVMType(llvmType) {
		if !strings.HasPrefix(val, "%") {
			llvmType = existingType
		}
	}

	// 函數呼叫（用結果參數模式）會被 generateExpr 寫成 call void @...，
	// 並返回空字串表示沒有 SSA 值。對於這種情況，result param
	// 已經被函數就地修改，不需要再 store。
	if _, isCall := stmt.Value.(*parser.CallExpression); isCall && val == "" {
		return
	}

	// 結構體儲存
	if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
		structName := sl.Type
		fields := g.structTypes[structName]
		// If bare type name not found, try module-prefixed variant
		// (e.g. "server" → "server.server") to avoid unsized type errors.
		if fields == nil {
			suffix := "." + structName
			for name := range g.structTypes {
				if strings.HasSuffix(name, suffix) {
					structName = name
					fields = g.structTypes[name]
					break
				}
			}
		}
		structTy := "%" + structName
		// Ensure variable is allocated (needed for top-level LetStatements in main function)
		if _, exists := g.funcLocalNames[name]; !exists {
			if _, isGlobal := g.globalVars[name]; !isGlobal {
				g.varTypes[name] = structTy
				g.funcLocalNames[name] = true
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), llvmVarRef(name), structTy))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 0, i8* %s)\n", g.indent(), llvmVarRef(name)))
			}
		}
		// 先將整個結構體初始化為 zeroinitializer，避免未指定的欄位帶有 stack 殘值
		sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), structTy, structTy, g.varAddr(name)))
		// 建立欄位名稱 → 索引映射
		fieldIndexByName := make(map[string]int)
		for i, f := range fields {
			fieldIndexByName[f.name] = i
		}
		// 記錄哪些欄位已由 literal 明確設定
		setFields := make(map[int]bool)
		// 逐欄位 store（按欄位名稱查找定義中的索引）
		for _, f := range sl.Fields {
			fieldIdx := -1
			fieldType := "i64"
			if idx, ok := fieldIndexByName[f.Name]; ok {
				fieldIdx = idx
				fieldType = fields[idx].typ
			}
			if fieldIdx < 0 {
				continue
			}
			setFields[fieldIdx] = true
			// %vec 欄位且值為 SliceLiteral：執行完整切片初始化
			//（alloca 臨時數組 + 逐元素 store + 構造 %vec {len, cap, data}）
			if fieldType == "%vec" {
				if sliceLit, ok := f.Value.(*parser.SliceLiteral); ok {
					g.initVecFieldFromSliceLiteral(sb, name, structTy, fieldIdx, sliceLit, fields[fieldIdx].elemType)
					continue
				}
			}
			fieldVal := g.generateExprWithSB(sb, f.Value)
			fieldVal = g.stripLLVMType(fieldVal)
			gepReg := g.tmpReg("st.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), gepReg, structTy, structTy, g.varAddr(name), fieldIdx))
			// 處理欄位型別與值的型別不一致：
			//   1. 欄位型別為 struct/string 但值是純量（如整數字面量）：使用 zeroinitializer
			//   2. 欄位型別為 struct/string 且值是指標（如 StringLiteral 回傳的 alloca 指針）：
			//      需要先 load 出 struct 值再 store
			//   3. 欄位型別為 %str-long 但值是不同型別：先轉換為 %str-long 再 store
			if g.isStructLLVMType(fieldType) {
				if nestedSL, ok := f.Value.(*parser.StructLiteral); ok {
					// 嵌套 struct literal：先 zeroinitializer 清零，再递归设置子字段
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(fieldType), toLLVMType(fieldType), gepReg))
					nestedStructName := strings.TrimPrefix(fieldType, "%")
					nestedFields := g.structTypes[nestedStructName]
					if nestedFields != nil {
						nestedFieldIdxMap := make(map[string]int)
						for i, nf := range nestedFields {
							nestedFieldIdxMap[nf.name] = i
						}
						for _, nsf := range nestedSL.Fields {
							if nfIdx, ok := nestedFieldIdxMap[nsf.Name]; ok {
								nfType := nestedFields[nfIdx].typ
								nfGEP := g.tmpReg("st.ngep")
								sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
									g.indent(), nfGEP, toLLVMType(fieldType), toLLVMType(fieldType), gepReg, nfIdx))
								// StringLiteral 和某些表达式返回 %str-long* 指针，需要 load
								// Identifier 返回 %str-long 值（已 load），直接 store
								if _, isStrLit := nsf.Value.(*parser.StringLiteral); isStrLit {
									nfVal := g.generateExprWithSB(sb, nsf.Value)
									nfVal = g.stripLLVMType(nfVal)
									loadReg := g.tmpReg("st.nfload")
									sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(nfType), toLLVMType(nfType), nfVal))
									sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(nfType), loadReg, toLLVMType(nfType), nfGEP))
								} else {
									nfVal := g.generateExprWithSB(sb, nsf.Value)
									nfVal = g.stripLLVMType(nfVal)
									sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(nfType), nfVal, toLLVMType(nfType), nfGEP))
								}
							}
						}
					}
				} else if !strings.HasPrefix(fieldVal, "%") {
					// 純量值，無法轉為 struct：使用 zeroinitializer
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(fieldType), toLLVMType(fieldType), gepReg))
				} else {
					// String literals and str pointer regs are %str-long* pointers (alloca),
					// need to load the %str-long value before storing.
					if _, isStrLit := f.Value.(*parser.StringLiteral); isStrLit || g.isStrPtrReg(fieldVal) {
						loadReg := g.tmpReg("st.fload")
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(fieldType), toLLVMType(fieldType), fieldVal))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), loadReg, toLLVMType(fieldType), gepReg))
					} else {
						// 決定實際的 source str 型別
						sourceStrType := g.inferSourceStrType(f.Value)
						if sourceStrType == "" {
							// 非 str 值，直接 store（已是 struct 值）
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
					} else if sourceStrType == fieldType {
						// 同型別。若源是函數參數或局部堆變數（如 Identifier
						// 指向 str 參數），直接 store 只做淺拷貝——結構體欄位
						// 與源共享同一 data 指標。當結構體被傳遞給另一個函數
						// 且該函數從欄位讀取 str 並賦值給輸出參數時，輸出參數
						// 與原始 str 共享 data，導致 double-free（bug19）。
						// 修復：對來自參數/局部堆變數的 str 值執行深層 clone。
						needClone := false
						if ident, ok := f.Value.(*parser.Identifier); ok {
							if g.funcParams != nil && g.funcParams[ident.Value] {
								needClone = true
							}
							if g.heapVars != nil {
								if _, isHeap := g.heapVars[ident.Value]; isHeap {
									needClone = true
								}
							}
						}
						if needClone {
							// 深層 clone：malloc 新 data + memcpy
							// srcPtr = 源變數的地址（如函數參數 %raw）
							// dstPtr = 結構體欄位的 GEP 指標
							// containerType = %str-long, dataFieldIdx = 2（data 在欄位 2）
							srcIdent := f.Value.(*parser.Identifier)
							dataFieldIdx := 2
							if toLLVMType(fieldType) == "%arr" {
								dataFieldIdx = 1
							}
							g.emitContainerClone(sb, g.varAddr(srcIdent.Value), gepReg, toLLVMType(fieldType), dataFieldIdx, "")
						} else {
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
						}
						} else {
							// 不同型別：先取得 source 指標，轉換為目標型別的指標，再 load + store
							sourcePtr := g.materializeStrPtr(sb, f.Value, sourceStrType, fieldVal)
							convertedPtr := sourcePtr
							loadReg := g.tmpReg("st.fload")
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(fieldType), toLLVMType(fieldType), convertedPtr))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), loadReg, toLLVMType(fieldType), gepReg))
						}
					}
				}
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
		}
	}
	// 為未明確設定的 %vec 欄位分配 data 緩衝區
		for i, f := range fields {
			if setFields[i] {
				continue
			}
			if f.typ != "%vec" {
				continue
			}
			vecCap := int64(256)
			elemSize := int64(8)
			if f.elemType != "" {
				elemSize = llvmTypeSize(f.elemType)
			}
			vecBufSize := vecCap * elemSize
			dataBuf := g.tmpReg("st.let.vecdata")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
			capGEP := g.tmpReg("st.let.veccap")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 1\n",
				g.indent(), capGEP, structTy, structTy, g.varAddr(name), i))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
			dataGEP := g.tmpReg("st.let.vecdataptr")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 2\n",
				g.indent(), dataGEP, structTy, structTy, g.varAddr(name), i))
			g.storeDataPtrField(sb, dataBuf, dataGEP)
		}
		// Track struct as heap variable if any field is heap-owning
		// (str/vec/arr/user struct). This ensures emitHeapFree releases
		// the struct's heap fields when the function exits.
		// Without this, t.raw.data would be leaked and any str-range
		// pointing into t.raw.data+offset would crash on free (bug19).
		if g.heapVars != nil {
			for _, f := range fields {
				if g.isHeapOwningType(f.typ) || g.isHeapOwningType(f.elemType) {
					g.trackLocalHeapVar(name, structTy)
					break
				}
			}
		}
		return
	}

	// MapType: m [K]V = { k1:v1, k2:v2 } → alloca %hashmap-K-V + init() + put() calls
	// The alloca itself is emitted by generateFunctionDefinition's collection phase
	// (collectVarDeclsFromStmt → varLLVMType → mapToLLVMType(mt.LLVMName()) → "%hashmap-K-V").
	// Here we only emit the init() call and put() calls for each MapLiteral pair.
	if mt, ok := stmt.Type.(*parser.MapType); ok {
		llvmType := g.mapToLLVMType(mt.LLVMName()) // e.g. %hashmap-str-i64
		g.varTypes[name] = llvmType
		// 區分全局變數和局部變數：全局變數的 varAddr 需返回 @name（透過 globalVars），
		// 不能設 funcLocalNames（否則 varAddr 會返回未 alloca 的 %name）。
		if g.globalVars == nil || !g.globalVars[name] {
			g.funcLocalNames[name] = true
		}
		structName := strings.TrimPrefix(llvmType, "%")
		recvArg := llvmType + "* " + g.varAddr(name)
		// Zero-initialize the struct before init(). Without this, fields not
		// touched by init() (e.g., hashmap vals[i].len/data when init() only
		// zeroes occ and keys.len) would contain stack garbage, causing the
		// scope-exit free loop to call free() on non-heap pointers.
		sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), llvmType, llvmType, g.varAddr(name)))
		// init() call: @hashmap-str-i64.init(%hashmap-str-i64* %m)
		sb.WriteString(fmt.Sprintf("%scall void @%s.init(%s)\n", g.indent(), sanitizeLLVMName(structName), recvArg))
		// If value is MapLiteral, generate put() calls for each pair
		if ml, ok := stmt.Value.(*parser.MapLiteral); ok {
			for _, pair := range ml.Pairs {
				keyArg := g.generateCallArg(sb, pair.Key)
				valArg := g.generateCallArg(sb, pair.Value)
				// is-new bool result param (discarded); map.no declares is-new as bool result
				isNewTmp := g.tmpReg("map.isnew")
				sb.WriteString(fmt.Sprintf("%s%s = alloca i1\n", g.indent(), isNewTmp))
				sb.WriteString(fmt.Sprintf("%scall void @%s.put(%s, %s, %s, i1* %s)\n",
					g.indent(), sanitizeLLVMName(structName), recvArg, keyArg, valArg, isNewTmp))
			}
		}
		// 追蹤為堆變數：hashmap 內聯 [N]str 鍵等欄位持有堆數據，
		// 函數結束時由 emitHeapFree → emitVarHeapFree → emitStructFieldsFree 遞迴釋放。
		g.trackLocalHeapVar(name, llvmType)
		return
	}

	if at, ok := stmt.Type.(*parser.ArrayType); ok {
		// 註冊變數型別為 %arr，供後續索引賦值/讀取/指標取得使用
		g.varTypes[name] = "%arr"
		// 區分全局變數和局部變數：全局變數的 varAddr 需返回 @name（透過 globalVars），
		// 不能設 funcLocalNames（否則 varAddr 會返回未 alloca 的 %name）
		if g.globalVars == nil || !g.globalVars[name] {
			g.funcLocalNames[name] = true
		}
		var arraySize int64
		if at.Size != nil {
			if v, ok := g.constFoldInt(at.Size); ok {
				arraySize = v
			} else if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
				arraySize = intLit.Value
			}
		} else if arrLit, ok := stmt.Value.(*parser.ArrayLiteral); ok {
			if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
				arraySize = intLit.Value
			}
		}
		// 嵌套陣列元素（如 [12][16]i64 的元素 [16]i64）必須使用原始 LLVM 陣列類型
		// [16 x i64]，而非 %arr 結構體 {i64, i64}，否則 store 時型別不匹配。
		var llvmElemType string
		if inner, ok := at.Elem.(*parser.ArrayType); ok {
			llvmElemType = g.arrayTypeToLLVM(inner)
		} else {
			elemType := "i64"
			if at.Elem != nil {
				elemType = at.Elem.String()
			}
			llvmElemType = g.mapToLLVMType(elemType)
		}
		elemSize := g.llvmTypeSize(llvmElemType)

		// Register element type and size for later index resolution and
		// %arr → [N x T]* argument conversion (genTypedArg / generateCallArg)
		g.arrayElemTypes[name] = llvmElemType
		if g.arraySizes != nil {
			g.arraySizes[name] = arraySize
		}

		// Store len field
		lenGEP := g.tmpReg("arr.len.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, g.varAddr(name)))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arraySize, lenGEP))

		// Allocate data buffer: arraySize * elemSize
		totalSize := arraySize * elemSize
		var dataReg string
		if g.stackArrVars != nil && g.stackArrVars[name] {
			// 棧分配陣列：prologue 已用 alloca 分配並存入 data 欄位，直接載入重用
			reuseGEP := g.tmpReg("arr.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), reuseGEP, g.varAddr(name)))
			dataReg = g.loadDataPtrField(sb, reuseGEP)
		} else {
			g.tmpIdx++
			dataReg = fmt.Sprintf("%%arr.data.malloc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), dataReg, totalSize))

			// Store data pointer in struct
			dataGEP := g.tmpReg("arr.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), dataGEP, g.varAddr(name)))
			g.storeDataPtrField(sb, dataReg, dataGEP)
		}

		// Store elements from array literal (if any)
		if arrLit, ok := stmt.Value.(*parser.ArrayLiteral); ok && len(arrLit.Elements) > 0 {
			dataCast := g.tmpReg("arr.data.cast")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataReg, toLLVMType(llvmElemType)))

			for i, elem := range arrLit.Elements {
				ev := g.generateExprWithSB(sb, elem)
				ev = g.stripLLVMType(ev)
				elemGEP := g.tmpReg("arr.elem.gep")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %d\n",
					g.indent(), elemGEP, toLLVMType(llvmElemType), toLLVMType(llvmElemType), dataCast, i))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), toLLVMType(llvmElemType), ev, toLLVMType(llvmElemType), elemGEP))
			}
		} else if stmt.Value != nil {
			// Non-ArrayLiteral value (e.g., function call returning [N]T):
			// val holds the raw LLVM array value (e.g., [20 x i8] %call.tmp.N).
			// Store it into the data buffer via bitcast to [N x T]*.
			rawArrType := fmt.Sprintf("[%d x %s]", arraySize, toLLVMType(llvmElemType))
			dataCast := g.tmpReg("arr.data.cast")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataReg, rawArrType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
				g.indent(), rawArrType, val, rawArrType, dataCast))
		}
		return
	}

	// Coerce value to declared type if variable was already typed (e.g., a i8; a = 2)
	alreadyCoerced := false
	if existingType, ok := g.varTypes[name]; ok && existingType != llvmType {
		if strings.HasPrefix(val, "%") {
			if g.isIntegerLLVMType(existingType) && g.isIntegerLLVMType(llvmType) {
				if llvmIntBitWidth(existingType) == llvmIntBitWidth(llvmType) {
					// Same bit width (e.g. i64 → u64) — no conversion needed
					llvmType = existingType
					alreadyCoerced = true
				} else {
					// int → int: trunc/zext
					// Bug13 fix: when llvmType=i1 (bool) but existingType=i64
					// (because collectVarDeclsFromStmtInner expands bool→i64),
					// the val from voidSingleOutput is already i64. Check SSA type
					// to avoid generating zext i1 (which would be a type error
					// since val is actually i64, not i1).
					actualValType := llvmType
					if g.ssaTypes != nil {
						if ssaT, ok := g.ssaTypes[val]; ok {
							actualValType = ssaT
						}
					}
					if llvmType == "i1" && existingType == "i64" && actualValType == "i64" {
						// val is already i64 (e.g. from voidSingleOutput path
						// where bool output params are mapped to i64).
						// No conversion needed, just use existingType.
						llvmType = existingType
						alreadyCoerced = true
					} else {
						convReg := g.tmpReg("conv")
						if llvmIntBitWidth(llvmType) > llvmIntBitWidth(existingType) {
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, toLLVMType(llvmType), val, toLLVMType(existingType)))
						} else {
							op := widenExtOp(llvmType)
							sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to %s\n", g.indent(), convReg, op, toLLVMType(llvmType), val, toLLVMType(existingType)))
						}
						val = convReg
						llvmType = existingType
						alreadyCoerced = true
					}
				}
			} else if existingType == "double" && g.isIntegerLLVMType(llvmType) {
				// int → double: sitofp
				convReg := g.tmpReg("sitofp")
				sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to double\n", g.indent(), convReg, llvmType, val))
				val = convReg
				llvmType = "double"
				alreadyCoerced = true
			} else if g.isIntegerLLVMType(existingType) && llvmType == "double" {
				// double → int: fptosi
				convReg := g.tmpReg("fptosi")
				sb.WriteString(fmt.Sprintf("%s%s = fptosi double %s to %s\n", g.indent(), convReg, val, existingType))
				val = convReg
				llvmType = existingType
				alreadyCoerced = true
			}
		}
	}

	// int → float/double conversion when the declared type is float/double but
	// the actual value is an integer SSA register (e.g., `f f64 = val` where val
	// is i64). The coercion block above misses this because both existingType and
	// llvmType are "double" (both derived from the f64 type annotation), so
	// existingType != llvmType is false.
	// Guard: skip if the expression is already a float expression (e.g., frac * 0.1
	// produces fmul double), since intExprLLVMType defaults to "i64" for non-integer
	// types including floats, which would cause a spurious sitofp on an already-double value.
	if !alreadyCoerced && (llvmType == "double" || llvmType == "float") && strings.HasPrefix(val, "%") &&
		g.floatLLVMType(stmt.Value) == "" {
		actualType := ""
		if g.ssaTypes != nil {
			if t, ok := g.ssaTypes[val]; ok {
				actualType = t
			}
		}
		if actualType == "" {
			actualType = valActualType
		}
		if actualType != "" && g.isIntegerLLVMType(actualType) {
			convReg := g.tmpReg("sitofp")
			sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to %s\n", g.indent(), convReg, toLLVMType(actualType), val, llvmType))
			val = convReg
			alreadyCoerced = true
		}
	}

	// 對單態化後的小整數型別（i8/u8/i16/u16/i32/u32），若值來自 i64 上下文
	// （如陣列索引 zext 或字面常量運算），需要 trunc 到變數型別
	// 注意：若上面的型別強制轉換已經處理過，則跳過，避免重複 trunc
	// 字串字面量 → byte (u8)：從 str-long 資料欄位載入第一個 byte
	if !alreadyCoerced && (llvmType == "i8" || llvmType == "u8") && strings.HasPrefix(val, "%str-longlit.") {
		if newVal, ok := g.coerceStrLitToByte(sb, val, stmt.Value); ok {
			val = newVal
			alreadyCoerced = true
		}
	}
if !alreadyCoerced && g.isIntegerLLVMType(llvmType) && llvmType != "i64" && strings.HasPrefix(val, "%") {
				// Check actual SSA type first (e.g. option data extraction may
				// already have truncated i64 → i8). Only trunc if the value
				// is genuinely i64.
				actualType := "i64"
				if g.ssaTypes != nil {
					if t, ok := g.ssaTypes[val]; ok {
						actualType = t
					}
				}
				if actualType == "i64" {
					valType := g.intExprLLVMType(stmt.Value)
					if valType == "i64" && toLLVMType(llvmType) != "i64" {
						truncReg := g.tmpReg("trunc")
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, val, toLLVMType(llvmType)))
						val = truncReg
					}
				}
	}
	// 寬變數賦值窄整數值（如 u64 = u32 | u32, u64 = u32 + u32）：
	// 兩個窄型別運算的結果仍是窄型別，需要 zext/sext 到變數的寬型別。
	// 使用 valActualType（在 currentTargetType 清除前保存），因為
	// 目標型別傳播可能已使值成為 i64/u64，此時不需要再拓寬。
	if !alreadyCoerced && llvmIntBitWidth(llvmType) == 64 && valActualType != "" {
		if g.isIntegerLLVMType(valActualType) && llvmIntBitWidth(valActualType) < 64 {
			zextReg := g.tmpReg("zext")
			op := widenExtOp(valActualType)
			sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to %s\n", g.indent(), zextReg, op, toLLVMType(valActualType), val, toLLVMType(llvmType)))
			val = zextReg
		}
	}

	// Compute the store address for `it` when it's shared across matches
	// with different element types (e.g. allocated as %http2-frame but storing i64).
	// Also update g.varTypes so subsequent reads of `it` in this match arm
	// use the correct type (not the allocated type from a different match).
	storeAddr := g.varAddr(name)
	if stmt.IsSynthetic {
		allocType := ""
		if g.itAllocTypes != nil {
			if at, ok := g.itAllocTypes[name]; ok {
				allocType = at
			}
		}
		if allocType == "" {
			if at, ok := g.varTypes[name]; ok {
				allocType = at
			}
		}
		if allocType != "" && allocType != llvmType {
			// Bitcast pointer when allocated type differs from actual type
			// (e.g. allocated as %http2-frame* but storing/loading i64)
			castReg := g.tmpReg("it.cast")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to %s*\n", g.indent(), castReg, allocType, storeAddr, llvmType))
			storeAddr = castReg
		}
		// Update varTypes so reads of `it` in this arm use the correct type
		if g.varTypes != nil {
			g.varTypes[name] = llvmType
		}
		// Re-apply struct pointer type for synthetic `it` that holds a
		// ptrtoint'd struct pointer (the generic update above sets i64).
		if syntheticStructPtrType != "" && g.varTypes != nil {
			g.varTypes[name] = syntheticStructPtrType
		}
	}

	// 輸出參數賦值：立即 store（不再使用延遲綁定）。
	// 原延遲綁定機制（SSA 版本化 + flushOutputBindings）在 if/break 場景下
	// 會丟失賦值：分支內的賦值遞增 SSA 版本，但分支後版本被 restore，
	// 導致函數返回時 flush 查不到綁定，out 參數保留舊值。
	// 立即 store 確保 `out = x` 在執行時即刻生效，語意正確且無副作用。
	// SSA 版本遞增保留以維持與 expr.go save/restore 的相容性，但不再記錄綁定。
	if g.outputParamNames != nil && g.outputParamNames[name] && !stmt.IsSynthetic {
		g.ssaVersion[name]++
		// 不記錄延遲綁定，落入後續立即 store 路徑
	}

	// 堆變數追蹤：記錄有堆分配資料的局部變數，用於函數結束時 free。
	// 同時，若目標從局部變數接收堆資料（淺拷貝），標記源變數為 moved（不 free），
	// 以避免 emitHeapFree 同時 free 兩個指向同一塊堆內存的變數（double-free）。
	// 適用於輸出參數和局部變數間的賦值（如 ls.no 中 `dir = a`）。
	if g.heapVars != nil {
		// 僅追蹤局部變數（非參數、非輸出參數）
		if _, isLocal := g.funcLocalNames[name]; isLocal {
			if _, isParam := g.paramNames[name]; !isParam {
				if g.outputParamNames == nil || !g.outputParamNames[name] {
					switch llvmType {
					case "%vec", "%str-long":
						g.trackLocalHeapVar(name, llvmType)
					case "%arr":
						// [N]byte 等固定大小陣列若以 alloca 棧分配（見 shouldStackAllocArray），
						// 則 data 指向棧記憶體，不可在函數退出時 free。
						// 僅當變數不在 stackArrVars 中（即 malloc 路徑）才追蹤為 heap var。
						if g.stackArrVars == nil || !g.stackArrVars[name] {
							g.trackLocalHeapVar(name, llvmType)
						}
					case "%option":
						// option 是否持有堆數據由 inner type 決定。
						// 僅當 inner 為堆型別（%str-long/%vec/%arr/用戶結構體）時才追蹤，
						// 純量 option（?i64/?f64/...）不 malloc，無需釋放。
						if inner, ok := g.optionInnerTypes[name]; ok && g.isHeapOwningType(inner) {
							g.trackLocalHeapVar(name, llvmType)
						}
					default:
						// 用戶自定義結構體：若含堆數據欄位（%vec/%str-long/%arr 或嵌套結構體），
						// 加入追蹤以便函數結束時遞迴釋放。
						if g.isUserStructType(llvmType) {
							g.trackLocalHeapVar(name, llvmType)
						}
					}
				}
			}
		}
		// b = a 是深層 clone（malloc 新 data + memcpy），源變數仍擁有獨立 data，不需 move 標記。
		// out = ident 的 move 由觸發點 1（handleMoveToOut）統一處理。
	}

	switch llvmType {
	case "%str-long":
		// Copy %str-long struct: load from source, store to dest
		// String literal produces %str-long* pointer (alloca).
		// Function call results are already %%str-long values and can be stored directly.
		isGlobal := g.globalVars != nil && g.globalVars[name] && !(g.funcLocalNames != nil && g.funcLocalNames[name])
		if strings.HasPrefix(val, "%str-longlit.") {
			// All string literals are now %str-long* alloca pointers.
			// Load the %str-long value for storing into the target variable.
			loadReg := g.tmpReg("str-long.load")
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
			val = loadReg
		} else if g.isStrPtrReg(val) {
			// val is a %str-long* pointer (from generateStrConcat or convertShortToLong).
			// Load the %str-long value from the pointer before storing.
			loadReg := g.tmpReg("str-long.load")
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
			val = loadReg
		} else if g.isVecPtrReg(val) {
			// val is a %vec* pointer (e.g. from read-file builtin returning %rf.vec.N).
			// %vec and %str-long have the same LLVM struct layout {i64, i64, i64},
			// so load %vec and store as %str-long (bug12: cross-module str return corruption).
			loadReg := g.tmpReg("str-long.load")
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%vec* %s\n", g.indent(), loadReg, val))
			val = loadReg
		} else if !strings.HasPrefix(val, "%") {
			// 宣告但無初值（如 `dummy str`）：val 為 "0"，使用 zeroinitializer
			if isGlobal {
				sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), llvmGlobalRef(name)))
			} else {
				sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), storeAddr))
			}
			return
		}
		if isGlobal {
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, llvmGlobalRef(name)))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, storeAddr))
		}
	case "%vec":
		// Copy %vec struct: load from source, store to dest
		// val may be either an SSA value (already loaded) or an alloca pointer.
		// Identifiers produce loaded values; slice expressions produce alloca pointers.
		isGlobal := g.globalVars != nil && g.globalVars[name] && !(g.funcLocalNames != nil && g.funcLocalNames[name])
		vecPtrPrefixes := []string{
			"%slic.",    // generateSliceExpression
			"%vec.tmp.", // for-range with slice literal
			"%vvec.",    // variadic call args
			"%rf.vec.",  // read-file ForwardFunc returns %vec*
		}
		isPtr := false
		for _, p := range vecPtrPrefixes {
			if strings.HasPrefix(val, p) {
				isPtr = true
				break
			}
		}
		if isPtr {
			copyReg := g.tmpReg("vec.copy")
			sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), copyReg, val))
			val = copyReg
		}
		// Propagate element type from voidSingleOutput path (e.g. parts = 'a-b-c'.split('-')
		// returns []str, so arrayElemTypes["parts"] must be %str-long for correct indexing).
		// Also check if arrayElemTypes[name] is already set (e.g. from type annotation `parts []str`).
		if g.arrayElemTypes != nil {
			if _, hasElemType := g.arrayElemTypes[name]; !hasElemType {
				if g.lastVoidSingleOutputElemType != "" {
					g.arrayElemTypes[name] = g.lastVoidSingleOutputElemType
				}
			}
		}
		g.lastVoidSingleOutputElemType = "" // clear after use
		if isGlobal {
			sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %s\n", g.indent(), val, llvmGlobalRef(name)))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %s\n", g.indent(), val, llvmVarRef(name)))
		}
	case "i8*":
		g.storeDataPtrField(sb, val, g.varAddr(name))
	case "%arr":
		// Assignment to an [N]T variable (e.g., `out = func()` where out was
		// declared as `out [16]byte`). val is a raw LLVM array value (e.g.,
		// [16 x i8] %call.tmp.N from voidSingleOutput). Store it into the
		// %arr struct's data buffer via bitcast.
		//
		// IMPORTANT: freeOldHeapValue may have already freed the old data buffer.
		// We must allocate a NEW buffer via malloc rather than reusing the old
		// (potentially freed) data pointer. Using the freed pointer causes
		// use-after-free, manifesting as trace/BPT trap on macOS.
		if val == "0" || val == "" {
			sb.WriteString(fmt.Sprintf("%sstore %%arr zeroinitializer, %%arr* %s\n", g.indent(), storeAddr))
		} else {
			arraySize := int64(0)
			llvmElemType := "i64"
			if s, ok := g.arraySizes[name]; ok {
				arraySize = s
			}
			if et, ok := g.arrayElemTypes[name]; ok {
				llvmElemType = et
			}
			rawArrType := fmt.Sprintf("[%d x %s]", arraySize, toLLVMType(llvmElemType))
			// Allocate new data buffer (old was freed by freeOldHeapValue if it existed)
			elemSize := g.llvmTypeSize(llvmElemType)
			if elemSize == 0 {
				elemSize = 8
			}
			totalSize := arraySize * elemSize
			dataMallocReg := g.tmpReg("arr.let.data.malloc")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), dataMallocReg, totalSize))
			// Store new data pointer in %arr struct (field 1)
			dataGEP := g.tmpReg("arr.let.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), dataGEP, storeAddr))
			g.storeDataPtrField(sb, dataMallocReg, dataGEP)
			// Store len field (field 0)
			lenGEP := g.tmpReg("arr.let.len.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n",
				g.indent(), lenGEP, storeAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arraySize, lenGEP))
			// Bitcast and store the value into the new data buffer
			dataCast := g.tmpReg("arr.let.cast")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataMallocReg, rawArrType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
				g.indent(), rawArrType, val, rawArrType, dataCast))
		}
	case "float", "double":
		// Convert integer literal to float format (e.g. "1" → "1.0")
		// This is needed when monomorphized union-type functions substitute
		// integer values into float-typed contexts.
		if !strings.HasPrefix(val, "%") && !strings.ContainsAny(val, ".eE") {
			if _, err := fmt.Sscanf(val, "%d", new(int64)); err == nil {
				val = val + ".0"
			}
		}
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(llvmType), val, toLLVMType(llvmType), g.varAddr(name)))
	default:
		irType := toLLVMType(llvmType)
		ptrType := irType + "*"
		// 宣告但無初值（如 `f http2-frame`）：val 為 "0"，struct 需用 zeroinitializer
		if g.isStructLLVMType(llvmType) && !strings.HasPrefix(val, "%") {
			sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s %s\n", g.indent(), irType, ptrType, storeAddr))
		} else {
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %s\n", g.indent(), irType, val, ptrType, storeAddr))
		}
	}
}

func (g *Generator) isIntegerLLVMType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128", "i1", "u8", "u16", "u32", "u64", "u128":
		return true
	}
	return false
}

// isStrPtrReg checks if a register name is a %str-long* pointer (from alloca).
// In LLVM 21 with opaque pointers, both values and pointers are SSA registers
// starting with '%', so we identify pointers by naming convention.
func (g *Generator) isStrPtrReg(val string) bool {
	if !strings.HasPrefix(val, "%") {
		return false
	}
	// Known alloca patterns that produce a %str-long* pointer
	ptrPatterns := []string{
		"%str-longlit.",        // string literal alloca
		"%str-long.s2s.",       // str conversion alloca
		"%s2s.result.",         // s2s conversion in stmt.go
		"%concat.result.",      // generateStrConcat
		"%nfmt.concat.result.", // callNamedFormat sprintf concatenation
		"%nfmt.field.",         // generateFieldStr single-field format() result
		"%repeat.result.",      // generateStrRepeat
		"%argv.str.",           // args-get in call_stdlib.go
		"%sprintf.val.",        // sprintf-based str returns (to-str etc.)
		"%str-long.s2s.",       // duplicate, keep
		"%idx.arr.elem.",       // generateStructFieldIndexRead: [N x %str-long] element GEP
		"%getline.str.",        // get-line builtin in call_stdlib.go
		"%readdir.str.",        // read-dir builtin in call_stdlib.go
		"%rf.str.",             // read-file builtin in call_stdlib.go
		"%archstr.",            // get-arch builtin in call_stdlib.go
		"%gl.str.",             // getlogin builtin in call_stdlib.go
		"%gd.str.",             // getdomainname builtin in call_stdlib.go
		"%sc.str.",             // sysctl builtin in call_stdlib.go
		"%slic.",               // generateSliceExpression (string slice → %str-long*)
		"%vso.tmp.",            // voidSingleOutput temporary buffer
	}
	for _, p := range ptrPatterns {
		if strings.HasPrefix(val, p) {
			return true
		}
	}
	return false
}

// isVecPtrReg checks if a register name is a %vec* pointer (from alloca or builtin).
// Used when a %vec* pointer needs to be loaded as %str-long (bug12: read-file returns
// %vec* but target variable is %str-long; both have identical {i64, i64, i64} layout).
func (g *Generator) isVecPtrReg(val string) bool {
	if !strings.HasPrefix(val, "%") {
		return false
	}
	vecPtrPatterns := []string{
		"%rf.vec.",  // read-file builtin returns %vec*
		"%slic.",    // generateSliceExpression returns %vec*
		"%vec.tmp.", // for-range with slice literal
		"%vvec.",    // variadic call args
	}
	for _, p := range vecPtrPatterns {
		if strings.HasPrefix(val, p) {
			return true
		}
	}
	return false
}

// loadStrValueIfNeeded checks if val is a %str-long* pointer (alloca from
// string literal, concat, repeat, etc.) and, if so, emits a load to obtain
// the %str-long struct value. Returns the loaded register name or val as-is.
// This is necessary when storing string expressions into struct/slice fields
// that expect a %str-long value, not a pointer to one.
func (g *Generator) loadStrValueIfNeeded(sb *strings.Builder, val string) string {
	if sb == nil || val == "" {
		return val
	}
	if g.isStrPtrReg(val) {
		loadReg := g.tmpReg("str-long.load")
		sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
		return loadReg
	}
	return val
}

func (g *Generator) stripLLVMType(val string) string {
	prefixes := []string{"i8* ", "i64 ", "i32 ", "i16 ", "i8 ", "double ", "float ", "i1 "}
	for _, p := range prefixes {
		if strings.HasPrefix(val, p) {
			return val[len(p):]
		}
	}
	return val
}

// inferSourceStrType 從 AST 表達式推斷 str 值的 LLVM 型別（%str-long）。
// 用於 struct literal 欄位賦值時判斷 source 與 target 型別是否一致。
// 回傳 "" 表示該表達式不是 str 值。
func (g *Generator) inferSourceStrType(expr parser.Expression) string {
	switch v := expr.(type) {
	case *parser.StringLiteral:
		return "%str-long"
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				if t == "%str-long" {
					return t
				}
			}
		}
	case *parser.DotExpression:
		// 結構體欄位存取：欄位型別即為 source 型別
		// 簡化處理：若是 file.path（type = str → %str-long），返回 %str-long
		// 更精確的處理需要查 struct definition，這裡先返回 fieldType
		if ident, ok := v.Receiver.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					structName := strings.TrimPrefix(t, "%")
					if fields, ok := g.structTypes[structName]; ok {
						for _, f := range fields {
							if f.name == v.Property {
								if f.typ == "%str-long" {
									return f.typ
								}
							}
						}
					}
				}
			}
		}
	}
	return ""
}

// materializeStrPtr 將 str 表達式物化為指標（%str-long*）。
// 對於 Identifier，使用其 varAddr；對於 StringLiteral，值本身已是指標；對於其他情況，
// 將值存入臨時 alloca 後回傳其地址。
func (g *Generator) materializeStrPtr(sb *strings.Builder, expr parser.Expression, strType, fieldVal string) string {
	switch expr.(type) {
	case *parser.StringLiteral:
		// StringLiteral 回傳的 fieldVal 本身就是 alloca 指標
		return fieldVal
	case *parser.Identifier:
		ident := expr.(*parser.Identifier)
		// Identifier 的 alloca 指標就是 varAddr
		return g.varAddr(ident.Value)
	case *parser.DotExpression:
		// DotExpression：值是 loaded struct，需要物化為指標
		tmpAlloca := g.tmpReg("st.fmat")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, strType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), strType, fieldVal, strType, tmpAlloca))
		}
		return tmpAlloca
	}
	// Fallback：將 fieldVal 存入臨時 alloca
	tmpAlloca := g.tmpReg("st.fmat")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, strType))
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), strType, fieldVal, strType, tmpAlloca))
	}
	return tmpAlloca
}

func (g *Generator) generateExpressionStmt(sb *strings.Builder, stmt *parser.ExpressionStatement) {
	if stmt.Expression == nil {
		return
	}

	switch e := stmt.Expression.(type) {
	case *parser.CallExpression:
		result := g.generateCallExpression(sb, e)
		if strings.HasPrefix(result, "call ") {
			sb.WriteString(g.indent() + result + "\n")
		}
	case *parser.AssignExpression:
		// 欄位賦值: u.name = value
		g.generateAssignExpression(sb, e)
		// 結果已由 generateAssignExpression 寫入 sb
	default:
		// 用 sb 確保 side effect（如 ++/--）被發出
		_ = g.generateExprWithSB(sb, e)
	}
}

// generateOptionAssign handles assignment to %option typed variables.
// Cases: nil, val(x), err(x), implicit value (tag=0).
func (g *Generator) generateOptionAssign(sb *strings.Builder, stmt *parser.LetStatement) {
	name := stmt.Name.Value

	// 釋放目標變數的舊堆 box（若有）。
	// 場景：v = f1(); v = f2() — 第二次賦值前需先釋放 f1 返回的 box，否則洩漏。
	// freeOldHeapFree 內部會處理：
	//   - 若 name 不在 heapVars 中（純量 option 或未追蹤），直接 return
	//   - 若 name 是 out 參數，return（呼叫者管理）
	//   - 透過 emitVarHeapFree 的 %option 分支正確釋放 option box
	//   - 透過 bitmap 機制處理 move 後的雙重釋放
	g.freeOldHeapValue(sb, stmt, name)

	// Helper: store tag value
	storeTag := func(tag int64) {
		tagGEP := g.tmpReg("opt.tag.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 0\n", g.indent(), tagGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), tag, tagGEP))
	}

	// Helper: zero data field
	zeroData := func() {
		dataGEP := g.tmpReg("opt.data.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dataGEP))
	}

	// Helper: copy a %str-long struct into data field via heap allocation.
	// malloc the struct, DEEP clone (so the string bytes are independently
	// owned, not shared with the source), then ptrtoint the pointer to i64
	// and store that i64 in the data field.
	// A shallow load+store would share the data pointer; when the source
	// str-long is freed at scope exit, the box would hold a dangling pointer.
	copyStrToData := func(srcPtr string) {
		heapPtr := g.tmpReg("opt.heap")
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 32)\n", g.indent(), heapPtr))
		heapCast := g.tmpReg("opt.heap.cast")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %%str-long*\n", g.indent(), heapCast, heapPtr))
		// emitDeepClone for %str-long: memcpy struct (len/cap/data) then
		// malloc a new buffer and memcpy the string bytes, so the box owns
		// an independent copy of the bytes.
		g.emitDeepClone(sb, srcPtr, heapCast, "%str-long", "")
		ptrInt := g.tmpReg("opt.ptr.int")
		sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
		dataGEP := g.tmpReg("opt.data.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
	}

	// Helper: store an i64 value directly into data field (fits in 8 bytes)
	copyI64ToData := func(val string) {
		dataGEP := g.tmpReg("opt.data.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), val, dataGEP))
	}

	// Helper: store a double value directly into data field (fits in 8 bytes)
	copyF64ToData := func(val string) {
		dataGEP := g.tmpReg("opt.data.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), val, dataGEP))
	}

	// Helper: 取得變數的 %str-long* 指標。
	// 共享合成 it 的 alloca 可能按更大的 struct 型別分配（如 %http.response），
	// varTypes 已收窄為 %str-long 時需 bitcast 指標，避免 emitDeepClone 型別錯配。
	strPtrOf := func(varName string) string {
		ptr := g.varAddr(varName)
		allocType := ""
		if g.itAllocTypes != nil {
			if at, ok := g.itAllocTypes[varName]; ok {
				allocType = at
			}
		}
		if allocType != "" && allocType != "%str-long" {
			castReg := g.tmpReg("opt.src.cast")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to %%str-long*\n", g.indent(), castReg, allocType, ptr))
			ptr = castReg
		}
		return ptr
	}

	// Helper: store a float (f32) value directly into data field (fits in 8 bytes)
	copyF32ToData := func(val string) {
		dataGEP := g.tmpReg("opt.data.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore float %s, float* %s\n", g.indent(), val, dataGEP))
	}

	// copyToData dispatches to the correct copy function based on the option's inner type.
	// For struct types: malloc on heap, DEEP clone struct (including heap-owned fields
	// like vec.data/str-long.data), ptrtoint pointer to i64, store i64.
	// A shallow store would share heap data between source and option box; when the
	// source is freed at scope exit, the box would hold dangling pointers, causing
	// use-after-free when the option is later unwrapped.
	// For i64/double/float: store directly in the data field (8 bytes, no malloc needed).
	copyToData := func(val string) {
		innerType := "i64"
		if g.optionInnerTypes != nil {
			if it, ok := g.optionInnerTypes[name]; ok && it != "" {
				innerType = it
			}
		}
		if os.Getenv("NOLANG_DEBUG_CLONE") != "" {
			fmt.Fprintf(os.Stderr, "[debug-clone] copyToData ENTER name=%q val=%q innerType=%q\n", name, val, innerType)
		}
		if innerType == "double" {
			copyF64ToData(val)
		} else if innerType == "float" {
			copyF32ToData(val)
		} else if g.isStructLLVMType(innerType) {
			// Struct types (e.g. %str-long, %client, %conn):
			// malloc heap box, store struct value to a temp alloca, then
			// emitDeepClone from temp to box so heap-owned fields are independently
			// allocated (not shared with the source).
			structSize := g.llvmTypeSize(innerType)
			if structSize == 0 {
				structSize = 8
			}
			heapPtr := g.tmpReg("opt.heap")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), heapPtr, structSize))
			heapCast := g.tmpReg("opt.heap.cast")
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), heapCast, heapPtr, innerType))
			// Stage the loaded value into a temp alloca so emitDeepClone has a
			// source pointer to read from (it requires pointers, not SSA values).
			srcTmp := g.tmpReg("opt.src.tmp")
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), srcTmp, innerType))
			// 防禦：val 非 SSA 寄存器（如整數字面量 "0" 佔位）時，
			// 不能 `store %str-long 0`（非法 IR），改存 zeroinitializer。
			storeVal := val
			if !strings.HasPrefix(val, "%") && !strings.HasPrefix(val, "@") {
				storeVal = "zeroinitializer"
			}
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), innerType, storeVal, innerType, srcTmp))
			// emitDeepClone does a memcpy of the whole struct (copying scalar fields
			// like fd/i64 directly) then recursively clones heap-owned fields
			// (vec.data → malloc+memcpy, str-long.data → malloc+memcpy, nested
			// structs → emitStructClone) so the box owns independent heap data.
			// For %vec inner type, pass the element type (from arrayElemTypes) so
			// emitContainerClone correctly calculates element sizes and recursively
			// clones elements (e.g. []str → elements are %str-long, need 24-byte
			// malloc per element, not 8-byte i64 default).
			vecElemType := ""
			if innerType == "%vec" && g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[name]; ok && et != "" {
					vecElemType = et
				}
			}
			if os.Getenv("NOLANG_DEBUG_CLONE") != "" {
				fmt.Fprintf(os.Stderr, "[debug-clone] copyToData name=%q innerType=%q vecElemType=%q arrayElemTypes=%v\n", name, innerType, vecElemType, g.arrayElemTypes)
			}
			g.emitDeepClone(sb, srcTmp, heapCast, innerType, vecElemType)
			ptrInt := g.tmpReg("opt.ptr.int")
			sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
			dataGEP := g.tmpReg("opt.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
		} else {
			// All integer types (i8/u8/i16/u16/i32/u32/i64/u64/bool) stored as i64
			copyI64ToData(val)
		}
	}

	// 純宣告（x ?T，無 RHS）：預設初始化為 nil（tag=1, data=0）。
	// 不可走 default 分支的 storeTag(0)+copyToData("0")——對 struct inner
	// 型別會生成 `store %str-long 0`（整數常量存入結構體型別，非法 IR）。
	if stmt.Value == nil {
		storeTag(1)
		zeroData()
		return
	}

	switch v := stmt.Value.(type) {
	case *parser.NilLiteral:
		// x = nil → tag=1, zero data
		storeTag(1)
		zeroData()

	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			if (ident.Value == "val" || ident.Value == "ok") && len(v.Arguments) == 1 {
				// x = val(expr) / ok(expr) → tag=0, copy expr to data
				storeTag(0)
				arg := v.Arguments[0]
				if _, isStr := arg.(*parser.StringLiteral); isStr {
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else if argIdent, isIdent := arg.(*parser.Identifier); isIdent {
					if t, ok := g.varTypes[argIdent.Value]; ok && (t == "%str-long") {
						// String variable: load and copy %str-long struct
						// （共享 it alloca 型別不同時由 strPtrOf bitcast）
						copyStrToData(strPtrOf(argIdent.Value))
					} else {
						// i64/f64 variable
						val := g.generateExprWithSB(sb, arg)
						copyToData(val)
					}
				} else if g.isStringExpr(arg) {
					// String expression (e.g. concat 'a' - b): result is %str-long*
					// alloca, copy as str struct regardless of option inner type
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else {
					val := g.generateExprWithSB(sb, arg)
					copyToData(val)
				}
				return
			}
			if ident.Value == "err" && len(v.Arguments) == 1 {
				// x = err(expr) → tag=2, copy expr to data
				storeTag(2)
				arg := v.Arguments[0]
				if _, isStr := arg.(*parser.StringLiteral); isStr {
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else if argIdent, isIdent := arg.(*parser.Identifier); isIdent {
					if t, ok := g.varTypes[argIdent.Value]; ok && t == "%str-long" {
						// （共享 it alloca 型別不同時由 strPtrOf bitcast）
						copyStrToData(strPtrOf(argIdent.Value))
					} else {
						val := g.generateExprWithSB(sb, arg)
						copyToData(val)
					}
				} else if g.isStringExpr(arg) {
					// String expression (e.g. concat 'a' - b): result is %str-long*
					// alloca, copy as str struct regardless of option inner type
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else {
					val := g.generateExprWithSB(sb, arg)
					copyToData(val)
				}
				return
			}
		}
		// Fallback: 處理函數呼叫返回值
		// Nolang 函數呼叫（如 .to-i64()）透過 voidSingleOutput 路徑會回傳 %option 結構的 load 暫存器，
		// 此時需複製整個結構到 LHS 變數；其他情況（內建函數、純量運算）視為隱含 val 並存成 i64。
		// 判斷依據：call 的函式名稱是否在 funcResultLLVMType 中（即 Nolang 函數）且 LLVM 型別為 %option。
		isNolangOptionCall := false
		if g.funcResultLLVMType != nil {
			if dot, isDot := v.Function.(*parser.DotExpression); isDot {
				// 重組方法名稱：依 varTypes/內建別名表推導型別前綴
				recv := dot.Receiver
				if recvIdent, ok := recv.(*parser.Identifier); ok {
					if recvType, ok := g.varTypes[recvIdent.Value]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						// Map LLVM struct names to Nolang type names for function lookup
						if srcType == "str-long" {
							candidates = append(candidates, "str")
						}
						// vec/arr receivers: add []T and _x<T> mangled name candidates
						// so monomorphized slice methods (e.g. _xi64.max) are found.
						if (srcType == "vec" || srcType == "arr") && g.arrayElemTypes != nil {
							if et, ok := g.arrayElemTypes[recvIdent.Value]; ok {
								et = strings.TrimPrefix(et, "%")
								if elemAliases, ok := llvmTypeToNolang[et]; ok {
									for _, alias := range elemAliases {
										candidates = append(candidates, "[]"+alias)
										candidates = append(candidates, "_x"+alias)
									}
								}
							}
						}
						for _, cand := range candidates {
							candName := cand + "." + dot.Property
							if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
								isNolangOptionCall = true
								break
							}
						}
					} else {
						// Receiver is a module name (not a variable), e.g.
						// json-util.extract-str(msg, 'content').
						// Check the full qualified name in funcResultLLVMType.
						fullName := recvIdent.Value + "." + dot.Property
						if ts, ok := g.funcResultLLVMType[fullName]; ok && len(ts) == 1 && ts[0] == "%option" {
							isNolangOptionCall = true
						}
					}
				} else if _, isStrLit := recv.(*parser.StringLiteral); isStrLit {
					candName := "str." + dot.Property
					if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
						isNolangOptionCall = true
					}
				} else {
					// Receiver is CallExpression / DotExpression / etc. (e.g. arg(1).to-i64()):
					// derive receiver type via exprResultLLVMType and look up method.
					recvType := g.exprResultLLVMType(recv)
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					if srcType == "str-long" {
						candidates = append(candidates, "str")
					}
					for _, cand := range candidates {
						candName := cand + "." + dot.Property
						if ts, ok := g.funcResultLLVMType[candName]; ok && len(ts) == 1 && ts[0] == "%option" {
							isNolangOptionCall = true
							break
						}
					}
				}
			} else if ident, isIdent := v.Function.(*parser.Identifier); isIdent {
				// 已被 transpiler 改寫為 str.to-i64() 形式
				if ts, ok := g.funcResultLLVMType[ident.Value]; ok && len(ts) == 1 && ts[0] == "%option" {
					isNolangOptionCall = true
				}
			}
		}
		if isNolangOptionCall {
			// 呼叫函數並複製 %option 結構
			val := g.generateExprWithSB(sb, v)
			// val 是 %option 的 load 暫存器，需儲存到 name
			sb.WriteString(fmt.Sprintf("%sstore %%option %s, %%option* %%%s\n", g.indent(), val, name))
			return
		}
		storeTag(0)
		val := g.generateExprWithSB(sb, v)
		copyToData(val)

	case *parser.Identifier:
		// option = option：區分 move（out 參數）vs 深層 clone（局部）
		if t, ok := g.varTypes[v.Value]; ok && t == "%option" {
			isOutput := g.outputParamNames != nil && g.outputParamNames[name]
			if isOutput {
				// out = a：走 move（與 vec/struct 一致）。
				// 淺拷貝 %option 結構（共享 box），由 handleMoveToOut 標記 source moved，
				// 函數結束時跳過 source 的 free，由呼叫者管理 out。
				copyReg := g.tmpReg("opt.copy")
				sb.WriteString(fmt.Sprintf("%s%s = load %%option, %%option* %%%s\n", g.indent(), copyReg, v.Value))
				sb.WriteString(fmt.Sprintf("%sstore %%option %s, %%option* %%%s\n", g.indent(), copyReg, name))
				g.handleMoveToOut(sb, v.Value, name)
			} else {
				// b = a：走深層 clone（與 vec/struct 一致）。
				// malloc 新 box + 遞迴 clone inner 的堆數據，
				// 使 a 和 b 各自獨立擁有 box，函數結束時各自 free。
				g.emitOptionDeepClone(sb, v.Value, name)
				// 追蹤目標為堆變數（若 inner 為堆型別）
				if inner, ok := g.optionInnerTypes[name]; ok && g.isHeapOwningType(inner) {
					if _, isLocal := g.funcLocalNames[name]; isLocal {
						if _, isParam := g.paramNames[name]; !isParam {
							if g.outputParamNames == nil || !g.outputParamNames[name] {
								g.trackLocalHeapVar(name, "%option")
							}
						}
					}
				}
			}
		} else {
			// Implicit value: val = n (plain value → ?T, tag=0)
			storeTag(0)
			// If option inner type is a struct (e.g. %str-long) but source
			// variable was alloca'd as i64 (type inference gap), bitcast
			// the i64* to the struct pointer and load the struct value.
			if g.optionInnerTypes != nil {
				if innerType, ok := g.optionInnerTypes[name]; ok && g.isStructLLVMType(innerType) {
					// Move-semantics fast path: when source is a local heap variable
					// and target is an output parameter, use shallow copy (memcpy)
					// + move (handleMoveToOut) instead of deep clone.
					// This avoids the emitDeepClone pattern (malloc+memcpy+NULL check)
					// that triggers LLVM -O3 optimizer miscompilation (free(-16)).
					isOutput := g.outputParamNames != nil && g.outputParamNames[name]
					_, isHeapVar := g.heapVars[v.Value]
					isLocal := g.funcLocalNames != nil && g.funcLocalNames[v.Value]
					srcIsStruct := false
					if srcType, ok := g.varTypes[v.Value]; ok {
						srcIsStruct = g.isStructLLVMType(srcType)
					}
					if isOutput && isHeapVar && isLocal && srcIsStruct && v.Value != name {
						structSize := g.llvmTypeSize(innerType)
						if structSize == 0 {
							structSize = 8
						}
						heapPtr := g.tmpReg("opt.heap")
						sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), heapPtr, structSize))
						// Shallow copy: memcpy from source to heap box
						srcAddr := g.varAddr(v.Value)
						sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0.p0.i64(i8* %s, i8* %s, i64 %d, i1 false)\n",
							g.indent(), heapPtr, srcAddr, structSize))
						// Mark source as moved: skip free at function end.
						// The heap box takes ownership of the source's data.
						g.handleMoveToOut(sb, v.Value, name)
						// Store heap pointer in option data field
						ptrInt := g.tmpReg("opt.ptr.int")
						sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
						dataGEP := g.tmpReg("opt.data.gep")
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
						sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
						return
					}
					if srcType, ok := g.varTypes[v.Value]; !ok || (srcType != innerType && !g.isStructLLVMType(srcType)) {
						castReg := g.tmpReg("opt.src.cast")
						sb.WriteString(fmt.Sprintf("%s%s = bitcast i64* %%%s to %s*\n", g.indent(), castReg, v.Value, innerType))
						loadReg := g.tmpReg("opt.struct.load")
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, innerType, innerType, castReg))
						copyToData(loadReg)
						return
					}
				}
			}
			val := g.generateExprWithSB(sb, stmt.Value)
			// option 接管 data 所有权（copyToData 会 load 并存储到 heap box），移除临时追踪
			g.untrackStmtTemporary(val)
			copyToData(val)
		}

	default:
		// 隱含值：x = expr → tag=0, 將 expr 寫入 data 欄位
		// 對於 struct literal（如 ?file = file{...}），將 option data
		// 欄位 bitcast 為 inner struct pointer，逐欄位直接 store。
		if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
			innerName := strings.TrimPrefix(g.optionInnerTypes[name], "%")
			innerTy := g.mapToLLVMType(innerName)
			fields := g.structTypes[innerName]
			if innerTy != "" && innerTy != "%option" && len(fields) > 0 {
				storeTag(0)
				// malloc struct on heap, store fields, ptrtoint to i64
				structSize := g.llvmTypeSize(innerTy)
				if structSize == 0 {
					structSize = 8
				}
				heapPtr := g.tmpReg("opt.heap")
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), heapPtr, structSize))
				heapCast := g.tmpReg("opt.heap.cast")
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), heapCast, heapPtr, innerTy))
				fieldIndexByName := make(map[string]int)
				for i, f := range fields {
					fieldIndexByName[f.name] = i
				}
				for _, f := range sl.Fields {
					fieldIdx, ok := fieldIndexByName[f.Name]
					if !ok {
						continue
					}
					fieldType := fields[fieldIdx].typ
					gepReg := g.tmpReg("opt.fld.gep")
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), gepReg, innerTy, innerTy, heapCast, fieldIdx))
					fieldVal := g.generateExprWithSB(sb, f.Value)
					fieldVal = g.stripLLVMType(fieldVal)
				if g.isStructLLVMType(fieldType) && !strings.HasPrefix(fieldVal, "%") {
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(fieldType), toLLVMType(fieldType), gepReg))
				} else if g.isStructLLVMType(fieldType) && strings.HasPrefix(fieldVal, "%") {
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
				} else {
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
				}
				}
				// ptrtoint heap pointer to i64 and store in option data field
				ptrInt := g.tmpReg("opt.ptr.int")
				sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), ptrInt, heapPtr))
				dataGEP := g.tmpReg("opt.data.gep")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ptrInt, dataGEP))
				return
			}
		}
		storeTag(0)
		if _, isStr := stmt.Value.(*parser.StringLiteral); isStr {
			srcPtr := g.generateExprWithSB(sb, stmt.Value)
			// copyStrToData 会 load %str-long 并存入 heap box（共享 data 指针），移除临时追踪
			g.untrackStmtTemporary(srcPtr)
			copyStrToData(srcPtr)
		} else if g.isStringExpr(stmt.Value) {
			// String expression: obtain a pointer to the %str-long value and let
			// copyStrToData load + store it. generateExprPtr covers Identifier,
			// DotExpression (field GEP), and IndexExpression (element GEP).
			// For expressions where generateExprPtr is unsupported (e.g. concat
			// returning an alloca pointer), fall back to generateExprWithSB.
			srcPtr := g.generateExprPtr(sb, stmt.Value)
			if srcPtr == "" {
				srcPtr = g.generateExprWithSB(sb, stmt.Value)
			}
			// copyStrToData 会 load %str-long 并存入 heap box（共享 data 指针），移除临时追踪
			g.untrackStmtTemporary(srcPtr)
			copyStrToData(srcPtr)
		} else {
			val := g.generateExprWithSB(sb, stmt.Value)
			val = g.stripLLVMType(val)
			// option 接管值（含 str data 指针），移除临时追踪避免 double-free
			g.untrackStmtTemporary(val)
			// For struct inner types (e.g. ?conn), generateExprWithSB may return a
			// pointer (e.g. %conn* from .conns[i]) rather than a loaded value.
			// Detect this: if val starts with "%" and the option's inner type is a
			// struct, treat val as a pointer and load the struct value first.
			// But skip loading if ssaTypes indicates val is already a loaded
			// struct value (not a pointer) — e.g. from vec element access.
			if g.optionInnerTypes != nil {
				if innerType, ok := g.optionInnerTypes[name]; ok && g.isStructLLVMType(innerType) {
					if strings.HasPrefix(val, "%") {
						// Check ssaTypes: if val is registered as the inner type
						// (without "*"), it's already a loaded value — skip the
						// redundant load which would cause a type mismatch.
						ssaType, hasSSA := g.ssaTypes[val]
						isAlreadyValue := hasSSA && ssaType == innerType
						if !isAlreadyValue {
							// val is a pointer to the struct; load the value
							loadReg := g.tmpReg("opt.struct.load")
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, innerType, innerType, val))
							val = loadReg
						}
					}
				}
			}
			copyToData(val)
		}
	}
}

func (g *Generator) generateEnumDefinition(sb *strings.Builder, ed *parser.EnumDefinition) {
	for _, v := range ed.Values {
		g.tmpIdx++
		sb.WriteString(fmt.Sprintf("%s@%s = constant i64 %d\n", g.indent(), v.Name, v.Value))
	}
}
