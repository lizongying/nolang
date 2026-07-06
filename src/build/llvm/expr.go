package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

func (g *Generator) generateExpression(expr parser.Expression) string {
	return g.generateExprWithSB(nil, expr)
}

// inferSSAType 從 SSA 暫存器名稱或字面量推斷 LLVM 型別
// 用於 if 表達式 phi 節點的型別推斷（當函數返回型別為 void 時）
func (g *Generator) inferSSAType(val string) string {
	if val == "" || val == "0" {
		return ""
	}
	// 字串字面量暫存器：%str-longlit.* 或 %str-shortlit.*
	if strings.Contains(val, "str-longlit") || strings.Contains(val, "str-shortlit") {
		return "ptr"
	}
	// 字串相關暫存器：%str.* (concat, repeat 等)
	if strings.Contains(val, "str-concat") || strings.Contains(val, "str-repeat") ||
		strings.Contains(val, "str-slice") || strings.Contains(val, "str-trim") {
		return "ptr"
	}
	// option 暫存器：%opt.* 或 %option.*
	if strings.Contains(val, "opt.") || strings.Contains(val, "option.") {
		return "%option"
	}
	// option data pointer (struct inner type, e.g. ?str): %var.data.ptr.*
	if strings.Contains(val, ".data.ptr.") {
		return "ptr"
	}
	// 浮點數字面量（含小數點或 e+）
	if strings.Contains(val, ".") && !strings.HasPrefix(val, "%") {
		return "double"
	}
	// 純數字 → i64
	if !strings.HasPrefix(val, "%") {
		return "i64"
	}
	// 其他 SSA 暫存器：查 ssaTypes（phi 節點等已記錄型別）
	if g.ssaTypes != nil {
		if t, ok := g.ssaTypes[val]; ok && t != "" {
			return t
		}
	}
	return ""
}

func (g *Generator) generateExprWithSB(sb *strings.Builder, expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.FloatLiteral:
		return fmt.Sprintf("%f", e.Value)
	case *parser.ByteLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.NilLiteral:
		return "0" // placeholder; nil is handled at assignment level
	case *parser.BooleanLiteral:
		if e.Value {
			return "1"
		}
		return "0"
	case *parser.Identifier:
		// Local variables and parameters shadow enum variants:
		// if a function has a parameter or local named "ok"/"err"/"nil",
		// it must be treated as the variable, not the option-enum variant.
		isLocalVar := g.funcLocalNames != nil && g.funcLocalNames[e.Value]
		// Enum variant: return tag index as constant integer
		if !isLocalVar && g.enumVariantIndex != nil {
			if tagIdx, ok := g.enumVariantIndex[e.Value]; ok {
				return fmt.Sprintf("%d", tagIdx)
			}
		}
		// Option type variable: extract data from data field (field 1)
		if g.varTypes != nil {
			if t, ok := g.varTypes[e.Value]; ok && t == "%option" {
				// Determine inner type from optionInnerTypes
				innerType := "i64"
				if g.optionInnerTypes != nil {
					if it, ok := g.optionInnerTypes[e.Value]; ok && it != "" {
						innerType = it
					}
				}
				g.tmpIdx++
				dataGEP := llvmSSAReg(e.Value, fmt.Sprintf(".data.gep.%d", g.tmpIdx))
				// For struct types (e.g. %str-long), return a pointer to the data
				// field (bitcast to innerType*) instead of loading the struct value.
				// This matches string literal behavior (which returns ptr to alloca).
				if strings.HasPrefix(innerType, "%") {
					g.tmpIdx++
					dataPtr := llvmSSAReg(e.Value, fmt.Sprintf(".data.ptr.%d", g.tmpIdx))
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, llvmVarRef(e.Value)))
						sb.WriteString(fmt.Sprintf("%s%s = bitcast [16 x i8]* %s to %s*\n", g.indent(), dataPtr, dataGEP, innerType))
					}
					return dataPtr
				}
				// For primitive types (i64, double, etc.), load the value
				g.tmpIdx++
				dataPtr := llvmSSAReg(e.Value, fmt.Sprintf(".data.ptr.%d", g.tmpIdx))
				g.tmpIdx++
				dataLoad := llvmSSAReg(e.Value, fmt.Sprintf(".data.val.%d", g.tmpIdx))
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, llvmVarRef(e.Value)))
					sb.WriteString(fmt.Sprintf("%s%s = bitcast [16 x i8]* %s to %s*\n", g.indent(), dataPtr, dataGEP, innerType))
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), dataLoad, innerType, innerType, dataPtr))
				}
				return dataLoad
			}
		}
		g.tmpIdx++
		reg := llvmSSAReg(e.Value, fmt.Sprintf(".val.%d", g.tmpIdx))
		if sb != nil {
			llvmType := "i64"
			if g.varTypes != nil {
				if t, ok := g.varTypes[e.Value]; ok {
					llvmType = t
				}
			}
			ptrType := llvmType + "*"
			varAddr := g.varAddr(e.Value)
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s %s\n", g.indent(), reg, llvmType, ptrType, varAddr))
		}
		return reg
	case *parser.StringLiteral:
		idx := g.stringIdx
		g.stringIdx++
		escaped := g.escapeLLVMString(e.Value)
		strLen := len(e.Value)
		g.fmtGlobals = append(g.fmtGlobals,
			fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", idx, strLen, escaped))
		dataPtr := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0)",
			strLen, strLen, idx)

		if sb != nil {
			if strLen <= 127 {
				// SSO: use %str-short (stack-allocated small string)
				g.tmpIdx++
				allocaReg := fmt.Sprintf("%%str-longlit.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-short\n", g.indent(), allocaReg))
				// Store len | 0x80 (field 0, i8)
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%str-longlit.len.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
				sb.WriteString(fmt.Sprintf("%sstore i8 %d, i8* %s\n", g.indent(), strLen|0x80, lenGEP))
				// Copy string data into field 1 ([127 x i8])
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%str-longlit.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n", g.indent(), dataGEP, allocaReg))
				// Zero the data field first to ensure null-termination for C functions (strtod, atoi, etc.)
				sb.WriteString(fmt.Sprintf("%sstore [127 x i8] zeroinitializer, [127 x i8]* %s\n", g.indent(), dataGEP))
				// Bitcast [127 x i8]* to i8* for memcpy
				g.tmpIdx++
				dstPtr := fmt.Sprintf("%%str-longlit.dst.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dstPtr, dataGEP))
				sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, %s, i64 %d)\n", g.indent(), dstPtr, dataPtr, strLen))
				return allocaReg
			} else {
				// Large string: use %str-long (heap pointer)
				g.tmpIdx++
				allocaReg := fmt.Sprintf("%%str-longlit.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), allocaReg))
				// Store len (field 0)
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%str-longlit.len.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
				// Store data (field 1)
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%str-longlit.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, allocaReg))
				sb.WriteString(fmt.Sprintf("%sstore %s, i8** %s\n", g.indent(), dataPtr, dataGEP))
				return allocaReg
			}
		}
		if strLen <= 127 {
			return fmt.Sprintf("%%str-longlit.%d", g.tmpIdx)
		}
		return fmt.Sprintf("%%str-longlit.%d", g.tmpIdx)
	case *parser.PrefixExpression:
		right := g.generateExprWithSB(sb, e.Right)
		if e.Operator == "-" {
			if strings.HasPrefix(right, "%") {
				g.tmpIdx++
				reg := fmt.Sprintf("%%neg.tmp.%d", g.tmpIdx)
				if sb != nil {
					// 判斷浮點型別：支援 float (f32) 與 double (f64)
					if ft := g.floatLLVMType(e.Right); ft != "" {
						sb.WriteString(fmt.Sprintf("%s%s = fneg %s %s\n", g.indent(), reg, ft, right))
					} else {
						negType := g.intExprLLVMType(e.Right)
						rc := g.coerceToInt(sb, right, e.Right, negType)
						sb.WriteString(fmt.Sprintf("%s%s = sub %s 0, %s\n", g.indent(), reg, negType, rc))
					}
				}
				return reg
			}
			return "-" + right
		}
		return right
	case *parser.CallExpression:
		result := g.generateCallExpression(sb, e)
		if strings.HasPrefix(result, "call ") {
			if strings.HasPrefix(result, "call void") {
				if sb != nil {
					sb.WriteString(g.indent() + result + "\n")
				}
				return ""
			}
			g.tmpIdx++
			reg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), reg, result))
			}
			// Track SSA type for phi node inference
			if g.ssaTypes != nil {
				if strings.HasPrefix(result, "call ") {
					parts := strings.SplitN(result[5:], " ", 2)
					if len(parts) >= 1 {
						retType := parts[0]
						g.ssaTypes[reg] = retType
					}
				}
			}
			// If call returns i32 (printf, etc.), zext to i64 for consistency
			if strings.Contains(result, "call i32") {
				g.tmpIdx++
				zextReg := fmt.Sprintf("%%call.zext.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), zextReg, reg))
				}
				return zextReg
			}
			return reg
		}
		return result
	case *parser.DotExpression:
		return g.generateDotExpression(sb, e)
	case *parser.AssignExpression:
		return g.generateAssignExpression(sb, e)
	case *parser.StructLiteral:
		return g.generateStructLiteral(sb, e)
	case *parser.IfExpression:
		return g.generateIfExpression(sb, e)
	case *parser.ConditionalExpression:
		return g.generateConditionalExpression(sb, e)
	case *parser.ArrayLiteral:
		return g.generateArrayLiteral(sb, e)
	case *parser.SliceLiteral:
		return g.generateSliceLiteral(sb, e)
	case *parser.SliceExpression:
		return g.generateSliceExpression(sb, e)
	case *parser.PointerType:
		if e.Type != nil {
			return g.mapToLLVMType(e.Type.String()) + "*"
		}
		return "i64*"
	case *parser.IndexExpression:
		return g.generateIndexExpression(sb, e)
	case *parser.InfixExpression:
		return g.generateInfix(sb, e)
	case *parser.GroupedExpression:
		return g.generateExprWithSB(sb, e.Expression)
	default:
		return "0"
	}
}

// generateConditionAsI1 generates LLVM IR for a condition expression,
// ensuring the result is of type i1. If the expression already produces i1
// (e.g. bool variable, bool struct field, bool method call), no trunc is needed.
func (g *Generator) generateConditionAsI1(sb *strings.Builder, cond parser.Expression) string {
	// Use intExprLLVMType to detect i1 conditions across all expression kinds:
	// Identifier (bool var), DotExpression (bool field, incl. chained access like
	// self.value.b), CallExpression (bool-returning function), etc.
	if g.intExprLLVMType(cond) == "i1" {
		return g.generateExprWithSB(sb, cond)
	}
	// Default: assume i64, need trunc to i1
	g.tmpIdx++
	reg := fmt.Sprintf("%%if.trunc.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), reg, g.generateExprWithSB(sb, cond)))
	return reg
}

func (g *Generator) generateIfExpression(sb *strings.Builder, expr *parser.IfExpression) string {
	g.tmpIdx++
	labelId := g.tmpIdx

	// 若條件是 InfixExpression（比較運算），直接取 i1
	cond := ""
	if infix, ok := expr.Condition.(*parser.InfixExpression); ok {
		isCmp := infix.Operator == "==" || infix.Operator == "!=" ||
			infix.Operator == "<" || infix.Operator == ">" ||
			infix.Operator == "<=" || infix.Operator == ">="
		if isCmp {
			cond = g.generateInfixI1(sb, infix)
		} else {
			// 非比較運算（如 && / ||）返回 i64，需 trunc 到 i1
			cond = g.generateConditionAsI1(sb, expr.Condition)
		}
	} else {
		cond = g.generateConditionAsI1(sb, expr.Condition)
	}

	// branch
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%if.then.%d, label %%if.else.%d\n",
		g.indent(), cond, labelId, labelId))

	// then
	thenLabel := fmt.Sprintf("if.then.%d", labelId)
	g.emitLabel(sb, thenLabel)
	g.indentLevel++
	// 預設 phi 值：對 struct 用 zeroinitializer，對 pointer 用 null，對 float/double 用 0.0
	defaultZero := "0"
	if strings.HasPrefix(g.curFuncRetType, "%") {
		defaultZero = "zeroinitializer"
	} else if strings.HasSuffix(g.curFuncRetType, "*") {
		defaultZero = "null"
	} else if g.curFuncRetType == "float" || g.curFuncRetType == "double" {
		defaultZero = "0.000000e+00"
	}
	thenVal := defaultZero
	if expr.Consequence != nil && len(expr.Consequence.Statements) > 0 {
		for i := 0; i < len(expr.Consequence.Statements)-1; i++ {
			g.generateStatement(sb, expr.Consequence.Statements[i])
		}
		last := expr.Consequence.Statements[len(expr.Consequence.Statements)-1]
		if es, ok := last.(*parser.ExpressionStatement); ok {
			thenVal = g.generateExprWithSB(sb, es.Expression)
		} else {
			g.generateStatement(sb, last)
		}
	}
	// 若 then 分支的最後一個表達式是 void 函數呼叫（用結果參數），
	// 則 thenVal 為空，需要從結果參數載入作為 phi 值。
	if thenVal == "" && g.curFuncRetName != "" && !g.blockTerminated {
		g.tmpIdx++
		thenLoad := fmt.Sprintf("%%if.then.load.%d", g.tmpIdx)
		retType := g.curFuncRetType
		if retType == "" || retType == "void" {
			retType = "i64"
		}
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %%%s\n", g.indent(), thenLoad, retType, retType, g.curFuncRetName))
		thenVal = thenLoad
	}
	// 若 then 沒有產生有效值（return 等），回退到 defaultZero
	if thenVal == "" {
		thenVal = defaultZero
	}
	thenTerminated := g.blockTerminated
	thenPredecessor := g.currentBlock
	if !thenTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%if.end.%d\n", g.indent(), labelId))
	}
	g.indentLevel--

	// else
	elseLabel := fmt.Sprintf("if.else.%d", labelId)
	g.emitLabel(sb, elseLabel)
	g.indentLevel++
	elseVal := defaultZero
	if expr.Alternative != nil && len(expr.Alternative.Statements) > 0 {
		for i := 0; i < len(expr.Alternative.Statements)-1; i++ {
			g.generateStatement(sb, expr.Alternative.Statements[i])
		}
		last := expr.Alternative.Statements[len(expr.Alternative.Statements)-1]
		if es, ok := last.(*parser.ExpressionStatement); ok {
			elseVal = g.generateExprWithSB(sb, es.Expression)
		} else {
			g.generateStatement(sb, last)
		}
	}
	if elseVal == "" && g.curFuncRetName != "" && !g.blockTerminated {
		g.tmpIdx++
		elseLoad := fmt.Sprintf("%%if.else.load.%d", g.tmpIdx)
		retType := g.curFuncRetType
		if retType == "" || retType == "void" {
			retType = "i64"
		}
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %%%s\n", g.indent(), elseLoad, retType, retType, g.curFuncRetName))
		elseVal = elseLoad
	}
	if elseVal == "" {
		elseVal = defaultZero
	}
	elseTerminated := g.blockTerminated
	elsePredecessor := g.currentBlock
	if !elseTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%if.end.%d\n", g.indent(), labelId))
	}
	g.indentLevel--

	// end
	endLabel := fmt.Sprintf("if.end.%d", labelId)
	g.emitLabel(sb, endLabel)
	g.tmpIdx++
	phiReg := fmt.Sprintf("%%if.phi.%d", g.tmpIdx)
	// phi type matches current function's return type
	phiType := g.curFuncRetType
	if phiType == "" || phiType == "void" {
		phiType = "i64"
	}
	// 從實際值推斷 phi 型別（處理 if 表達式在 void 函數中返回 string/option 的情況）
	if thenVal != "" && thenVal != "0" {
		if inferred := g.inferSSAType(thenVal); inferred != "" {
			phiType = inferred
		}
	} else if elseVal != "" && elseVal != "0" {
		if inferred := g.inferSSAType(elseVal); inferred != "" {
			phiType = inferred
		}
	}
	// 若 then/else 一方為 default zero，但另一方推斷出具體型別，沿用該型別
	if thenVal == defaultZero || thenVal == "0" {
		if inferred := g.inferSSAType(elseVal); inferred != "" {
			phiType = inferred
		}
	} else if elseVal == defaultZero || elseVal == "0" {
		if inferred := g.inferSSAType(thenVal); inferred != "" {
			phiType = inferred
		}
	}
	// 記錄 phi 節點型別，供後續 inferSSAType 查詢
	if g.ssaTypes != nil {
		g.ssaTypes[phiReg] = phiType
	}
	// For struct types, use zeroinitializer instead of integer 0
	zeroVal := "0"
	if strings.HasPrefix(phiType, "%") {
		zeroVal = "zeroinitializer"
	} else if phiType == "ptr" {
		zeroVal = "null"
	} else if phiType == "float" || phiType == "double" {
		zeroVal = "0.000000e+00"
	}
	if thenVal == "" || thenVal == "0" {
		thenVal = zeroVal
	}
	if elseVal == "" || elseVal == "0" {
		elseVal = zeroVal
	}
	// Build phi entries based on which branches are terminated
	thenPred := fmt.Sprintf("%%%s", thenPredecessor)
	elsePred := fmt.Sprintf("%%%s", elsePredecessor)
	if thenTerminated && elseTerminated {
		// Both branches return — if.end is unreachable; emit a dummy value
		sb.WriteString(fmt.Sprintf("%s%s = add %s 0, 0\n", g.indent(), phiReg, phiType))
	} else if thenTerminated {
		sb.WriteString(fmt.Sprintf("%s%s = phi %s [%s, %s]\n",
			g.indent(), phiReg, phiType, elseVal, elsePred))
	} else if elseTerminated {
		sb.WriteString(fmt.Sprintf("%s%s = phi %s [%s, %s]\n",
			g.indent(), phiReg, phiType, thenVal, thenPred))
	} else {
		sb.WriteString(fmt.Sprintf("%s%s = phi %s [%s, %s], [%s, %s]\n",
			g.indent(), phiReg, phiType, thenVal, thenPred, elseVal, elsePred))
	}

	return phiReg
}

// generateConditionalExpression 產生三元運算子的 LLVM IR
// 支持 condition ? consequence : alternative
func (g *Generator) generateConditionalExpression(sb *strings.Builder, expr *parser.ConditionalExpression) string {
	g.tmpIdx++
	labelId := g.tmpIdx

	// 判斷結果型別
	phiType := g.conditionalResultType(expr)

	// 生成條件（若為比較運算，直接取 i1）
	cond := ""
	if infix, ok := expr.Condition.(*parser.InfixExpression); ok {
		isCmp := infix.Operator == "==" || infix.Operator == "!=" ||
			infix.Operator == "<" || infix.Operator == ">" ||
			infix.Operator == "<=" || infix.Operator == ">="
		if isCmp {
			cond = g.generateInfixI1(sb, infix)
		} else {
			g.tmpIdx++
			reg := fmt.Sprintf("%%cond.trunc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), reg, g.generateExprWithSB(sb, expr.Condition)))
			cond = reg
		}
	} else {
		g.tmpIdx++
		reg := fmt.Sprintf("%%cond.trunc.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), reg, g.generateExprWithSB(sb, expr.Condition)))
		cond = reg
	}

	// branch
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%cond.then.%d, label %%cond.else.%d\n",
		g.indent(), cond, labelId, labelId))

	// then (consequence)
	g.emitLabel(sb, fmt.Sprintf("cond.then.%d", labelId))
	g.indentLevel++
	thenVal := g.generateExprWithSB(sb, expr.Consequence)
	thenPredecessor := g.currentBlock
	sb.WriteString(fmt.Sprintf("%sbr label %%cond.end.%d\n", g.indent(), labelId))
	g.indentLevel--

	// else (alternative)
	g.emitLabel(sb, fmt.Sprintf("cond.else.%d", labelId))
	g.indentLevel++
	elseVal := g.generateExprWithSB(sb, expr.Alternative)
	elsePredecessor := g.currentBlock
	sb.WriteString(fmt.Sprintf("%sbr label %%cond.end.%d\n", g.indent(), labelId))
	g.indentLevel--

	// end: phi
	g.emitLabel(sb, fmt.Sprintf("cond.end.%d", labelId))
	g.tmpIdx++
	phiReg := fmt.Sprintf("%%cond.phi.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = phi %s [%s, %%%s], [%s, %%%s]\n",
		g.indent(), phiReg, phiType, thenVal, thenPredecessor, elseVal, elsePredecessor))

	return phiReg
}

// conditionalResultType 推導三元運算式的結果型別
func (g *Generator) conditionalResultType(expr *parser.ConditionalExpression) string {
	if isFloatExpr(expr.Consequence) || isFloatExpr(expr.Alternative) {
		return "f64"
	}
	// 檢查是否為泛型（由變數名判斷）
	if ident, ok := expr.Consequence.(*parser.Identifier); ok {
		if t, ok := g.varTypes[ident.Value]; ok {
			return t
		}
	}
	if ident, ok := expr.Alternative.(*parser.Identifier); ok {
		if t, ok := g.varTypes[ident.Value]; ok {
			return t
		}
	}
	return "i64"
}

// isFloatExpr 判斷表達式是否為 f64 類型
func isFloatExpr(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.FloatLiteral:
		return true
	case *parser.InfixExpression:
		return isFloatExpr(v.Left) || isFloatExpr(v.Right)
	case *parser.PrefixExpression:
		return isFloatExpr(v.Right)
	case *parser.GroupedExpression:
		return isFloatExpr(v.Expression)
	}
	return false
}

// isDoubleExpr 判斷表達式是否為浮點型別（float 或 double）。
// 保留原名以維持向下相容；現在同時涵蓋 f32 (float) 與 f64 (double)。
func (g *Generator) isDoubleExpr(expr parser.Expression) bool {
	return g.floatLLVMType(expr) != ""
}

// floatLLVMType 推斷表達式的 LLVM 浮點型別。
// 回傳 "float"（f32）、"double"（f64）或 ""（非浮點）。
// 當運算元混合 float 與 double 時，較寬者勝出（double）。
// 注意：比較運算（==, !=, <, >, <=, >=）的結果是 i1/bool，不是浮點，
// 因此對比較運算永遠回傳 ""。
func (g *Generator) floatLLVMType(expr parser.Expression) string {
	switch v := expr.(type) {
	case *parser.FloatLiteral:
		return "double"
	case *parser.InfixExpression:
		// 比較運算的結果是 i1/bool，不是浮點型別
		switch v.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return ""
		}
		lt := g.floatLLVMType(v.Left)
		rt := g.floatLLVMType(v.Right)
		if lt == "double" || rt == "double" {
			return "double"
		}
		if lt == "float" || rt == "float" {
			return "float"
		}
	case *parser.PrefixExpression:
		return g.floatLLVMType(v.Right)
	case *parser.GroupedExpression:
		return g.floatLLVMType(v.Expression)
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				switch t {
				case "float":
					return "float"
				case "double":
					return "double"
				}
			}
		}
	case *parser.DotExpression:
		// 支援鏈式存取：非 Identifier receiver 透過 exprResultLLVMType 推導
		if g.structTypes != nil {
			recvType := g.exprResultLLVMType(v.Receiver)
			if strings.HasPrefix(recvType, "%") {
				structName := strings.TrimPrefix(recvType, "%")
				if fields, ok := g.structTypes[structName]; ok {
					for _, f := range fields {
						if f.name == v.Property {
							switch f.typ {
							case "float":
								return "float"
							case "double":
								return "double"
							}
						}
					}
				}
			}
		}
	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			m := builtin.FindBuiltinMethod(ident.Value)
			if m != nil && len(m.Return) > 0 {
				switch m.Return[0] {
				case parser.TypeF32:
					return "float"
				case parser.TypeF64:
					return "double"
				}
			}
		}
	}
	return ""
}

// intExprLLVMType 推斷表達式的 LLVM 整數型別（i8/i16/i32/i64）。
// 用於算術與比較運算時選擇正確的型別，避免單態化後 i8/i16/i32 變數
// 與硬編碼 i64 指令之間的型別不匹配。
// 注意：IndexExpression 預設回傳 i64，因為 generateIndexExpression
// 會將 i8 元素 zext 到 i64。
func (g *Generator) intExprLLVMType(expr parser.Expression) string {
	switch v := expr.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				switch t {
				case "i1", "i8", "i16", "i32", "i64":
					return t
				}
			}
		}
	case *parser.DotExpression:
		// Field access on a struct variable: look up field's LLVM type.
		// e.g. .connected (where self.connected is bool) → i1
		// 支援鏈式存取：非 Identifier receiver 透過 exprResultLLVMType 推導
		recvType := g.exprResultLLVMType(v.Receiver)
		if strings.HasPrefix(recvType, "%") {
			structName := strings.TrimPrefix(recvType, "%")
			if fields, ok := g.structTypes[structName]; ok {
				for _, f := range fields {
					if f.name == v.Property {
						switch f.typ {
						case "i1", "i8", "i16", "i32", "i64":
							return f.typ
						case "double":
							return "double"
						}
						// Non-integer field (struct/str/etc.) — default to i64
						return "i64"
					}
				}
			}
		}
	case *parser.InfixExpression:
		// 比較運算與邏輯運算的結果是 i1（已 zext 後為 i64）
		switch v.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "i64"
		}
		// 與 arithLLVMType 相同的策略：偏好非字面量運算元的型別
		_, leftIsLit := v.Left.(*parser.IntegerLiteral)
		_, rightIsLit := v.Right.(*parser.IntegerLiteral)
		if !leftIsLit {
			if t := g.intExprLLVMType(v.Left); t != "i64" {
				return t
			}
		}
		if !rightIsLit {
			if t := g.intExprLLVMType(v.Right); t != "i64" {
				return t
			}
		}
		return widerIntType(g.intExprLLVMType(v.Left), g.intExprLLVMType(v.Right))
	case *parser.PrefixExpression:
		return g.intExprLLVMType(v.Right)
	case *parser.GroupedExpression:
		return g.intExprLLVMType(v.Expression)
	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			if g.funcRetTypes != nil {
				if t, ok := g.funcRetTypes[ident.Value]; ok {
					if t != "void" {
						return t
					}
					// void + 單輸出函數：使用 funcResultLLVMType 中的輸出型別
					if g.funcResultLLVMType != nil {
						if ts, ok := g.funcResultLLVMType[ident.Value]; ok && len(ts) == 1 {
							return ts[0]
						}
					}
					return "i64"
				}
			}
		}
		// DotExpression receiver call: look up str.<method>
		if dot, ok := v.Function.(*parser.DotExpression); ok {
			// StringLiteral receiver: always str type
			if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				shortName := "str." + dot.Property
				if g.funcRetTypes != nil {
					if t, ok := g.funcRetTypes[shortName]; ok {
						if t != "void" {
							return t
						}
						if g.funcResultLLVMType != nil {
							if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
								return ts[0]
							}
						}
					}
				}
			}
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					if srcType == "str-short" || srcType == "str-long" {
						candidates = append(candidates, "str")
					}
					// 基本型別可能對應多個 nolang 型別名稱（如 i32 → char, i32, u32）
					if primAliases, ok := llvmTypeToNolang[srcType]; ok {
						candidates = append(candidates, primAliases...)
					}
					for _, cand := range candidates {
						shortName := cand + "." + dot.Property
						if g.funcRetTypes != nil {
							if t, ok := g.funcRetTypes[shortName]; ok {
								if t != "void" {
									return t
								}
								// void + 單輸出函數（如 str.empty 返回 i1）：
								// 使用 funcResultLLVMType 中的輸出型別
								if g.funcResultLLVMType != nil {
									if ts, ok := g.funcResultLLVMType[shortName]; ok && len(ts) == 1 {
										return ts[0]
									}
								}
							}
						}
					}
				}
			}
		}
		return "i64"
	}
	return "i64"
}

// widerIntType 回傳兩個 LLVM 整數型別中較寬者。
func widerIntType(a, b string) string {
	order := map[string]int{"i8": 8, "i16": 16, "i32": 32, "i64": 64}
	if order[a] >= order[b] {
		return a
	}
	return b
}

// arithLLVMType 推斷算術/比較運算的 LLVM 整數型別。
// 當一個運算元是整數字面常量（預設為 i64）而另一個是變數時，
// 優先使用變數的型別，避免字面常量的預設型別主導型別推斷。
func (g *Generator) arithLLVMType(left, right parser.Expression) string {
	_, leftIsLit := left.(*parser.IntegerLiteral)
	_, rightIsLit := right.(*parser.IntegerLiteral)
	if !leftIsLit {
		if t := g.intExprLLVMType(left); t != "i64" {
			return t
		}
	}
	if !rightIsLit {
		if t := g.intExprLLVMType(right); t != "i64" {
			return t
		}
	}
	return widerIntType(g.intExprLLVMType(left), g.intExprLLVMType(right))
}

// coerceToInt 將 SSA 值轉換為目標整數型別。
// 當值是較窄的整數型別時，進行 zext 擴展；當值是 i64 而目標較窄時，進行 trunc。
// 當值是整數字面常量時保持原樣（LLVM 會自動處理）。
func (g *Generator) coerceToInt(sb *strings.Builder, v string, exprForType parser.Expression, targetType string) string {
	if v == "" || targetType == "i64" {
		return v
	}
	// 整數字面常量：直接使用，LLVM 會自動處理
	if _, err := fmt.Sscanf(v, "%d", new(int64)); err == nil && !strings.HasPrefix(v, "%") {
		return v
	}
	// SSA 暫存器：若來源型別與目標型別不同，進行轉換
	if strings.HasPrefix(v, "%") {
		srcType := g.intExprLLVMType(exprForType)
		if srcType == targetType {
			return v
		}
		if sb == nil {
			return v
		}
		g.tmpIdx++
		cvtReg := fmt.Sprintf("%%cvt.%d", g.tmpIdx)
		if srcType == "i64" {
			// i64 → 較窄型別：trunc
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), cvtReg, v, targetType))
		} else {
			order := map[string]int{"i8": 8, "i16": 16, "i32": 32, "i64": 64}
			if order[srcType] > order[targetType] {
				// 來源比目標寬：trunc
				sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), cvtReg, srcType, v, targetType))
			} else {
				// 來源比目標窄：zext
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to %s\n", g.indent(), cvtReg, srcType, v, targetType))
			}
		}
		return cvtReg
	}
	return v
}

// zextBoolToI64 將 i1 運算元 zext 到 i64，以用於 && / || 等邏輯運算。
// 若運算元已是 i64（如比較結果已 zext、或布林字面常量），則保持不變。
// 這解決了 void+單輸出函數（如 str.empty() 返回 i1）與 i64 運算元混合的型別不匹配問題。
func (g *Generator) zextBoolToI64(sb *strings.Builder, v string, expr parser.Expression) string {
	if v == "" {
		return v
	}
	// 整數字面常量（含布林字面 "0"/"1"）：保持原樣
	if _, err := fmt.Sscanf(v, "%d", new(int64)); err == nil && !strings.HasPrefix(v, "%") {
		return v
	}
	// SSA 暫存器：若來源型別為 i1，zext 到 i64
	if strings.HasPrefix(v, "%") {
		if g.intExprLLVMType(expr) == "i1" {
			if sb == nil {
				return v
			}
			g.tmpIdx++
			extReg := fmt.Sprintf("%%lzext.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, v))
			return extReg
		}
	}
	return v
}

// generateInfixI1 回傳 i1 比較結果（無 zext），用於 for/if 條件

// exprResultLLVMType 推導任意表達式的結果 LLVM 型別。
// 用於鏈式 DotExpression / IndexExpression 的型別推導，
// 例如 .nodes[idx].str-val 中 .nodes[idx] 回傳 %json-value*，
// 需由此函式推導出 "json-value" 以解析後續 .str-val 欄位。
func (g *Generator) exprResultLLVMType(expr parser.Expression) string {
	switch v := expr.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				return t
			}
		}
	case *parser.DotExpression:
		recvType := g.exprResultLLVMType(v.Receiver)
		if strings.HasPrefix(recvType, "%") {
			structName := strings.TrimPrefix(recvType, "%")
			if fields, ok := g.structTypes[structName]; ok {
				for _, f := range fields {
					if f.name == v.Property {
						return f.typ
					}
				}
			}
		}
		// str-long/str-short .len → i64
		if v.Property == "len" && (recvType == "%str-long" || recvType == "%str-short") {
			return "i64"
		}
	case *parser.IndexExpression:
		leftType := g.exprResultLLVMType(v.Left)
		if strings.HasPrefix(leftType, "[") {
			closeB := strings.IndexByte(leftType, ']')
			if closeB > 0 {
				inner := leftType[1:closeB]
				xIdx := strings.LastIndex(inner, " x ")
				if xIdx >= 0 {
					return inner[xIdx+3:]
				}
			}
		}
		// %vec / %arr: element type tracked separately
		if ident, ok := v.Left.(*parser.Identifier); ok {
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[ident.Value]; ok {
					return et
				}
			}
		}
	case *parser.CallExpression:
		if g.ssaTypes != nil {
			if ident, ok := v.Function.(*parser.Identifier); ok {
				if g.funcRetTypes != nil {
					if t, ok := g.funcRetTypes[ident.Value]; ok && t != "void" {
						return t
					}
				}
			}
		}
	}
	return ""
}

func (g *Generator) generateDotExpression(sb *strings.Builder, expr *parser.DotExpression) string {
	varName := ""
	if ident, ok := expr.Receiver.(*parser.Identifier); ok {
		varName = ident.Value
	}
	fieldName := expr.Property

	// 命名空間 enum 變體存取：FileMode.WRITE、FilePerm.PERM_600
	if g.enumVariants != nil && varName != "" {
		if variants, ok := g.enumVariants[varName]; ok {
			if val, ok := variants[fieldName]; ok {
				return fmt.Sprintf("%d", val)
			}
		}
	}
	// Fallback: 命名空間風格存取，property 為模組級整數常量（如 FileMode.WRITE 中的 WRITE）
	if g.enumVariantIndex != nil && varName == "" {
		if val, ok := g.enumVariantIndex[fieldName]; ok {
			return fmt.Sprintf("%d", val)
		}
	}

	// 判定 receiver 的 struct 名稱與基底指標
	// - Identifier receiver: 使用變數名稱（%%%s）
	// - 非 Identifier receiver（IndexExpression / DotExpression）: 遞迴生成取得 SSA 指標
	structName := ""
	basePtr := "" // 非空時表示 receiver 是非 Identifier，需用 SSA 指標而非變數名

	if varName != "" {
		if t, ok := g.varTypes[varName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
	} else {
		// 先推導型別（不生成 IR），確認為 struct 後再生成 IR
		recvType := g.exprResultLLVMType(expr.Receiver)
		if strings.HasPrefix(recvType, "%") {
			structName = strings.TrimPrefix(recvType, "%")
		}
	}

	// Built-in str/str-short .len access
	// .len 需要字串的指標（%str-long* / %str-short*），而非載入後的值。
	// 因此對鏈式 receiver 使用 generateExprPtr 取得指標，避免 load。
	if fieldName == "len" && sb != nil {
		if structName == "str-long" || structName == "str-short" {
			ptr := ""
			if varName != "" {
				ptr = g.varAddr(varName)
			} else {
				ptr = g.generateExprPtr(sb, expr.Receiver)
			}
			if structName == "str-long" {
				return g.extractStrLen(sb, ptr)
			}
			return g.extractStrShortLen(sb, ptr)
		}
	}

	// 生成 receiver 程式碼取得 SSA 指標（用於一般欄位存取）
	// 非 Identifier receiver 必須取得指標（%T*），而非載入後的值，
	// 否則後續 GEP 會把 struct 值當作指標使用（如 self.buf.len 的 %vec 值）。
	if varName == "" && sb != nil {
		basePtr = g.generateExprPtr(sb, expr.Receiver)
		// Fallback: generateExprPtr 不支援的表達式類型（如 CallExpression）
		// 透過 generateExprWithSB 取得值後存入臨時 alloca 以獲得指標。
		if basePtr == "" {
			val := g.generateExprWithSB(sb, expr.Receiver)
			if val != "" && val != "0" {
				recvType := g.exprResultLLVMType(expr.Receiver)
				if recvType != "" {
					g.tmpIdx++
					tmpAlloca := fmt.Sprintf("%%recv.tmp.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, recvType))
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), recvType, val, recvType, tmpAlloca))
					basePtr = tmpAlloca
				}
			}
		}
	}

	if fields, ok := g.structTypes[structName]; ok {
		fieldIdx := -1
		var fieldType string
		for i, f := range fields {
			if f.name == fieldName {
				fieldIdx = i
				fieldType = f.typ
				break
			}
		}
		if fieldIdx >= 0 && sb != nil {
			structTy := "%" + structName
			g.tmpIdx++
			reg := fmt.Sprintf("%%dot.gep.%d", g.tmpIdx)
			if basePtr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), reg, structTy, structTy, basePtr, fieldIdx))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
					g.indent(), reg, structTy, structTy, varName, fieldIdx))
			}
			// 一律 load 欄位值。對 struct 型別的鏈式存取由 generateExprPtr 處理（需指標的情況）。
			g.tmpIdx++
			loadReg := fmt.Sprintf("%%dot.val.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, fieldType, fieldType, reg))
			return loadReg
		}
	}
	return "0"
}

// generateExprPtr 生成表達式的 LLVM IR 並回傳指向結果的指標（%T*），
// 而非載入後的值。用於需要指標的場景，例如 .len 存取需 %str-long*。
// 對 Identifier 回傳變數地址；對 DotExpression 回傳欄位 GEP 指標；
// 對 IndexExpression 回傳元素指標（struct 元素）。
func (g *Generator) generateExprPtr(sb *strings.Builder, expr parser.Expression) string {
	switch v := expr.(type) {
	case *parser.Identifier:
		return g.varAddr(v.Value)
	case *parser.DotExpression:
		// 取得 receiver 指標，GEP 到欄位，回傳指標（不 load）
		recvName := ""
		if ident, ok := v.Receiver.(*parser.Identifier); ok {
			recvName = ident.Value
		}
		structName := ""
		basePtr := ""
		if recvName != "" {
			if t, ok := g.varTypes[recvName]; ok {
				structName = strings.TrimPrefix(t, "%")
			}
		} else {
			recvType := g.exprResultLLVMType(v.Receiver)
			if strings.HasPrefix(recvType, "%") {
				structName = strings.TrimPrefix(recvType, "%")
			}
			if sb != nil {
				basePtr = g.generateExprPtr(sb, v.Receiver)
			}
		}
		if fields, ok := g.structTypes[structName]; ok {
			for i, f := range fields {
				if f.name == v.Property {
					g.tmpIdx++
					reg := fmt.Sprintf("%%dot.ptr.gep.%d", g.tmpIdx)
					structTy := "%" + structName
					if sb != nil {
						if basePtr != "" {
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
								g.indent(), reg, structTy, structTy, basePtr, i))
						} else {
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
								g.indent(), reg, structTy, structTy, recvName, i))
						}
					}
					return reg
				}
			}
		}
	case *parser.IndexExpression:
		// 對陣列/切片元素，需回傳元素指標（而非載入的值）
		// 否則 arr[i].field = val 會把 struct value 當作指標使用
		return g.generateIndexExprPtr(sb, v)
	}
	return ""
}

// generateIndexExprPtr 生成陣列/切片元素的指標（GEP），不載入值
func (g *Generator) generateIndexExprPtr(sb *strings.Builder, v *parser.IndexExpression) string {
	varName := ""
	if ident, ok := v.Left.(*parser.Identifier); ok {
		varName = ident.Value
	} else if dot, ok := v.Left.(*parser.DotExpression); ok {
		// struct.field[i] 讀取：委託給 generateStructFieldIndexRead
		// 但這裡需要指標，所以用 generateExprPtr 取得欄位指標
		return g.generateExprPtr(sb, dot)
	}
	if varName == "" {
		// 無法取得指標，回退到載入值
		return ""
	}

	idx := g.generateExprWithSB(sb, v.Index)

	if t, ok := g.varTypes[varName]; ok {
		if t == "%arr" {
			llvmElemType := "i64"
			if et, ok := g.arrayElemTypes[varName]; ok {
				llvmElemType = et
			}
			arrRef := llvmVarRef(varName)
			if g.globalVars != nil && g.globalVars[varName] {
				arrRef = llvmGlobalRef(varName)
			}
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%arr.ptr.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%arr.ptr.data.%d", g.tmpIdx)
			g.tmpIdx++
			dataTyped := fmt.Sprintf("%%arr.ptr.typed.%d", g.tmpIdx)
			g.tmpIdx++
			elemGEP := fmt.Sprintf("%%arr.ptr.elem.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
					g.indent(), dataGEP, arrRef))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}
			return elemGEP
		}
		if t == "%vec" {
			llvmElemType := "i64"
			if et, ok := g.arrayElemTypes[varName]; ok {
				llvmElemType = et
			}
			vecRef := llvmVarRef(varName)
			if g.globalVars != nil && g.globalVars[varName] {
				vecRef = llvmGlobalRef(varName)
			}
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%vec.ptr.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%vec.ptr.data.%d", g.tmpIdx)
			g.tmpIdx++
			dataTyped := fmt.Sprintf("%%vec.ptr.typed.%d", g.tmpIdx)
			g.tmpIdx++
			elemGEP := fmt.Sprintf("%%vec.ptr.elem.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, vecRef))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}
			return elemGEP
		}
	}
	// 其他情況回退到載入值
	return ""
}

// extractStrDataPtr extracts the i8* data pointer (field 1) from a %str-long* pointer.
// Returns the register name holding the i8*.
func (g *Generator) extractStrDataPtr(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%str-long.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataLoad := fmt.Sprintf("%%str-long.data.val.%d", g.tmpIdx)
	if sb != nil {
		// Handle both @global and %local references
		if strings.HasPrefix(strPtr, "@") {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, strPtr))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, strPtr))
		}
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), dataLoad, dataGEP))
	}
	return dataLoad
}

// extractStrLen extracts the i64 len (field 0) from a %str-long* pointer.
func (g *Generator) extractStrLen(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%str-long.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	lenLoad := fmt.Sprintf("%%str-long.len.val.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
	}
	return lenLoad
}

// extractStrShortLen extracts the i64 len from a %str-short* pointer.
// Loads field 0 (i8), ANDs with 0x7F to clear the SSO tag bit, then zero-extends to i64.
func (g *Generator) extractStrShortLen(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%str-longsm.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	lenLoad := fmt.Sprintf("%%str-longsm.len.raw.%d", g.tmpIdx)
	g.tmpIdx++
	lenMasked := fmt.Sprintf("%%str-longsm.len.mask.%d", g.tmpIdx)
	g.tmpIdx++
	lenExt := fmt.Sprintf("%%str-longsm.len.ext.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 0\n", g.indent(), lenGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), lenLoad, lenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = and i8 %s, 127\n", g.indent(), lenMasked, lenLoad))
		sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), lenExt, lenMasked))
	}
	return lenExt
}

// extractStrShortDataPtr extracts the i8* data pointer from a %str-short* pointer.
// Returns a pointer to field 1 (the inline [127 x i8] array), bitcast to i8*.
func (g *Generator) extractStrShortDataPtr(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%str-longsm.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataPtr := fmt.Sprintf("%%str-longsm.data.ptr.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n", g.indent(), dataGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dataPtr, dataGEP))
	}
	return dataPtr
}

// extractLenDispatch extracts len from either %str-long or %str-short based on known variable type.
func (g *Generator) extractLenDispatch(sb *strings.Builder, varName string) string {
	if t, ok := g.varTypes[varName]; ok {
		if t == "%str-short" {
			return g.extractStrShortLen(sb, "%"+varName)
		}
		return g.extractStrLen(sb, "%"+varName)
	}
	return g.extractStrLen(sb, "%"+varName)
}

// extractDataPtrDispatch extracts data ptr from either %str-long or %str-short based on known variable type.
func (g *Generator) extractDataPtrDispatch(sb *strings.Builder, varName string) string {
	if t, ok := g.varTypes[varName]; ok {
		if t == "%str-short" {
			return g.extractStrShortDataPtr(sb, "%"+varName)
		}
		return g.extractStrDataPtr(sb, "%"+varName)
	}
	return g.extractStrDataPtr(sb, "%"+varName)
}

// resolveStrPtr resolves a value to a %str-long* pointer.
// If the value is a register starting with %, it's already a %str-long*.
// Otherwise, it returns the value as-is.
func (g *Generator) resolveStrPtr(val string) string {
	if strings.HasPrefix(val, "%") {
		return val
	}
	return val
}

// generateStructFieldIndexAssign 處理 struct.field[i] = val 賦值
func (g *Generator) generateStructFieldIndexAssign(sb *strings.Builder, dot *parser.DotExpression, index parser.Expression, value parser.Expression) string {
	recvName := ""
	if ident, ok := dot.Receiver.(*parser.Identifier); ok {
		recvName = ident.Value
	}
	fieldName := dot.Property
	idx := g.generateExprWithSB(sb, index)
	val := g.generateExprWithSB(sb, value)

	// 判定 struct 名稱與基底指標
	// - Identifier receiver: 使用變數名稱（%%%s）
	// - 非 Identifier receiver: 使用 generateExprPtr 取得指標
	structName := ""
	basePtr := ""
	if recvName != "" {
		if t, ok := g.varTypes[recvName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
	} else {
		recvType := g.exprResultLLVMType(dot.Receiver)
		if strings.HasPrefix(recvType, "%") {
			structName = strings.TrimPrefix(recvType, "%")
		}
		if sb != nil {
			basePtr = g.generateExprPtr(sb, dot.Receiver)
		}
	}

	if fields, ok := g.structTypes[structName]; ok {
		fieldIdx := -1
		var fieldType string
		for i, f := range fields {
			if f.name == fieldName {
				fieldIdx = i
				fieldType = f.typ
				break
			}
		}
		if fieldIdx >= 0 && sb != nil {
			// GEP to field in struct
			g.tmpIdx++
			fieldGEP := fmt.Sprintf("%%set.field.gep.%d", g.tmpIdx)
			structTy := "%" + structName
			if basePtr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, basePtr, fieldIdx))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, recvName, fieldIdx))
			}

			if fieldType == "%vec" {
				// Slice field: load data pointer (field 2), bitcast, GEP, store
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%set.vec.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%set.vec.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))

				// Bitcast to i64*
				g.tmpIdx++
				dataTyped := fmt.Sprintf("%%set.vec.typed.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i64*\n",
					g.indent(), dataTyped, dataLoad))

				// GEP to element index and store
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%set.vec.elem.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i64, i64* %s, i64 %s\n",
					g.indent(), elemGEP, dataTyped, idx))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n",
					g.indent(), val, elemGEP))
				return "0"
			}

			if fieldType == "%str-long" {
				// str-long field: load data pointer (field 1), GEP, store
				// Also auto-update len (field 0) to max(len, idx+1)
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%set.strf.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%set.strf.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
					g.indent(), dataGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))

				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%set.strf.elem.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
					g.indent(), elemGEP, dataLoad, idx))
				storeVal := val
				if strings.HasPrefix(val, "%") {
					valType := g.intExprLLVMType(value)
					if strings.HasPrefix(valType, "i") && valType != "i8" {
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%set.strf.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
						storeVal = truncReg
					}
				}
				sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n",
					g.indent(), storeVal, elemGEP))

				// Auto-update len: load cur len, compute idx+1, store max
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%set.strf.len.gep.%d", g.tmpIdx)
				g.tmpIdx++
				curLen := fmt.Sprintf("%%set.strf.cur-len.%d", g.tmpIdx)
				g.tmpIdx++
				newLen := fmt.Sprintf("%%set.strf.new-len.%d", g.tmpIdx)
				g.tmpIdx++
				cmpReg := fmt.Sprintf("%%set.strf.cmp.%d", g.tmpIdx)
				g.tmpIdx++
				finalLen := fmt.Sprintf("%%set.strf.final-len.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
					g.indent(), lenGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
				sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
				return "0"
			}

			if fieldType == "%str-short" {
				// str-short field: GEP to field 1 (data array), GEP into array, store
				// Also auto-update len (field 0, i8) to max(len, idx+1)
				g.tmpIdx++
				arrFieldGEP := fmt.Sprintf("%%set.strsf.arr.gep.%d", g.tmpIdx)
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%set.strsf.elem.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n",
					g.indent(), arrFieldGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [127 x i8], [127 x i8]* %s, i64 0, i64 %s\n",
					g.indent(), elemGEP, arrFieldGEP, idx))
				storeVal := val
				if strings.HasPrefix(val, "%") {
					valType := g.intExprLLVMType(value)
					if strings.HasPrefix(valType, "i") && valType != "i8" {
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%set.strsf.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
						storeVal = truncReg
					}
				}
				sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n",
					g.indent(), storeVal, elemGEP))

				// Auto-update len: load cur len (i8), zext to i64, compute idx+1, store max
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%set.strsf.len.gep.%d", g.tmpIdx)
				g.tmpIdx++
				curLen8 := fmt.Sprintf("%%set.strsf.cur-len8.%d", g.tmpIdx)
				g.tmpIdx++
				curLen := fmt.Sprintf("%%set.strsf.cur-len.%d", g.tmpIdx)
				g.tmpIdx++
				newLen := fmt.Sprintf("%%set.strsf.new-len.%d", g.tmpIdx)
				g.tmpIdx++
				cmpReg := fmt.Sprintf("%%set.strsf.cmp.%d", g.tmpIdx)
				g.tmpIdx++
				finalLen64 := fmt.Sprintf("%%set.strsf.final-len64.%d", g.tmpIdx)
				g.tmpIdx++
				finalLen8 := fmt.Sprintf("%%set.strsf.final-len8.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 0\n",
					g.indent(), lenGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), curLen8, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), curLen, curLen8))
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
				sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen64, cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i8\n", g.indent(), finalLen8, finalLen64))
				sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n", g.indent(), finalLen8, lenGEP))
				return "0"
			}

			if strings.HasPrefix(fieldType, "[") {
				// Inline array field: GEP into the array directly
				closeB := strings.IndexByte(fieldType, ']')
				if closeB > 0 {
					inner := fieldType[1:closeB]
					xIdx := strings.LastIndex(inner, " x ")
					elemType := "i64"
					if xIdx >= 0 {
						elemType = inner[xIdx+3:]
					}
					g.tmpIdx++
					elemGEP := fmt.Sprintf("%%set.arr.elem.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
						g.indent(), elemGEP, fieldType, fieldType, fieldGEP, idx))
					// Truncate val to elemType if needed (e.g., i64 → i8 for byte arrays)
					// Only truncate for integer types; struct types (e.g. %str-long) need conversion
					storeVal := val
					if elemType != "i64" && !strings.HasPrefix(elemType, "%") && strings.HasPrefix(val, "%") {
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%set.arr.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, val, elemType))
						storeVal = truncReg
					}
					// s2s conversion: StringLiteral (%str-short* alloca) → %str-long value
					// when assigning to a %str-long array element (e.g., keys[i] = 'foo')
					if elemType == "%str-long" && strings.HasPrefix(val, "%str-longlit.") {
						storeVal = g.convertStrLongLitToLongValue(sb, val)
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
						g.indent(), elemType, storeVal, elemType, elemGEP))
					return "0"
				}
			}
		}
	}
	return "0"
}

// generateStructFieldIndexRead 處理 struct.field[i] 讀取
func (g *Generator) generateStructFieldIndexRead(sb *strings.Builder, dot *parser.DotExpression, index parser.Expression) string {
	recvName := ""
	if ident, ok := dot.Receiver.(*parser.Identifier); ok {
		recvName = ident.Value
	}
	fieldName := dot.Property
	idx := g.generateExprWithSB(sb, index)

	// 判定 receiver 的 struct 名稱與基底指標
	// - Identifier receiver: 使用變數名稱（%%%s）
	// - 非 Identifier receiver（IndexExpression / DotExpression）: 遞迴生成取得 SSA 指標
	structName := ""
	basePtr := "" // 非空時表示 receiver 是非 Identifier，需用 SSA 指標而非變數名

	if recvName != "" {
		if t, ok := g.varTypes[recvName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
	} else {
		recvType := g.exprResultLLVMType(dot.Receiver)
		if strings.HasPrefix(recvType, "%") {
			structName = strings.TrimPrefix(recvType, "%")
		}
		if sb != nil {
			// 非 Identifier receiver 必須取得指標（%T*），而非載入後的值，
			// 否則後續 GEP 會把 struct 值當作指標使用。
			basePtr = g.generateExprPtr(sb, dot.Receiver)
			// Fallback: generateExprPtr 不支援的表達式類型
			if basePtr == "" {
				val := g.generateExprWithSB(sb, dot.Receiver)
				if val != "" && val != "0" {
					if recvType != "" {
						g.tmpIdx++
						tmpAlloca := fmt.Sprintf("%%recv.tmp.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, recvType))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), recvType, val, recvType, tmpAlloca))
						basePtr = tmpAlloca
					}
				}
			}
		}
	}

	if fields, ok := g.structTypes[structName]; ok {
		fieldIdx := -1
		var fieldType string
		for i, f := range fields {
			if f.name == fieldName {
				fieldIdx = i
				fieldType = f.typ
				break
			}
		}
		if fieldIdx >= 0 && sb != nil {
			// GEP to field in struct
			g.tmpIdx++
			fieldGEP := fmt.Sprintf("%%idx.field.gep.%d", g.tmpIdx)
			structTy := "%" + structName
			if basePtr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, basePtr, fieldIdx))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, recvName, fieldIdx))
			}

			if fieldType == "%vec" {
				// Slice field: load data pointer, bitcast, GEP, load
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%idx.vec.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%idx.vec.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))

				// Bitcast to i64*
				g.tmpIdx++
				dataTyped := fmt.Sprintf("%%idx.vec.typed.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i64*\n",
					g.indent(), dataTyped, dataLoad))

				// GEP to element and load
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%idx.vec.elem.%d", g.tmpIdx)
				g.tmpIdx++
				elemLoad := fmt.Sprintf("%%idx.vec.val.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i64, i64* %s, i64 %s\n",
					g.indent(), elemGEP, dataTyped, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
					g.indent(), elemLoad, elemGEP))
				return elemLoad
			}

			if fieldType == "%str-long" {
				// str-long field: load data pointer (field 1), GEP to byte, load, zext to i64
				// 對應 generateStructFieldIndexAssign 的 %str-long case（讀取版本）
				strIdx := idx
				if strings.HasPrefix(idx, "%") {
					idxType := g.intExprLLVMType(index)
					if idxType != "i64" {
						g.tmpIdx++
						zextReg := fmt.Sprintf("%%idx.strf.zext.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
						strIdx = zextReg
					}
				}
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%idx.strf.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%idx.strf.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
					g.indent(), dataGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))
				g.tmpIdx++
				charGEP := fmt.Sprintf("%%idx.strf.char.gep.%d", g.tmpIdx)
				g.tmpIdx++
				charLoad := fmt.Sprintf("%%idx.strf.char.val.%d", g.tmpIdx)
				g.tmpIdx++
				charZext := fmt.Sprintf("%%idx.strf.char.zext.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
					g.indent(), charGEP, dataLoad, strIdx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
					g.indent(), charZext, charLoad))
				return charZext
			}

			if fieldType == "%str-short" {
				// str-short field: GEP to data array (field 1), GEP to byte, load, zext to i64
				strIdx := idx
				if strings.HasPrefix(idx, "%") {
					idxType := g.intExprLLVMType(index)
					if idxType != "i64" {
						g.tmpIdx++
						zextReg := fmt.Sprintf("%%idx.strsf.zext.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
						strIdx = zextReg
					}
				}
				g.tmpIdx++
				arrGEP := fmt.Sprintf("%%idx.strsf.arr.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n",
					g.indent(), arrGEP, fieldGEP))
				g.tmpIdx++
				charGEP := fmt.Sprintf("%%idx.strsf.char.gep.%d", g.tmpIdx)
				g.tmpIdx++
				charLoad := fmt.Sprintf("%%idx.strsf.char.val.%d", g.tmpIdx)
				g.tmpIdx++
				charZext := fmt.Sprintf("%%idx.strsf.char.zext.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [127 x i8], [127 x i8]* %s, i64 0, i64 %s\n",
					g.indent(), charGEP, arrGEP, strIdx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
					g.indent(), charZext, charLoad))
				return charZext
			}

			if strings.HasPrefix(fieldType, "[") {
				// Inline array field
				closeB := strings.IndexByte(fieldType, ']')
				if closeB > 0 {
					inner := fieldType[1:closeB]
					xIdx := strings.LastIndex(inner, " x ")
					elemType := "i64"
					if xIdx >= 0 {
						elemType = inner[xIdx+3:]
					}
					g.tmpIdx++
					elemGEP := fmt.Sprintf("%%idx.arr.elem.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
						g.indent(), elemGEP, fieldType, fieldType, fieldGEP, idx))
					// Struct element: return pointer (by-reference), no load needed
					if strings.HasPrefix(elemType, "%") {
						return elemGEP
					}
					// Integer element: load value
					g.tmpIdx++
					elemLoad := fmt.Sprintf("%%idx.arr.val.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
						g.indent(), elemLoad, elemType, elemType, elemGEP))
					if elemType != "i64" {
						g.tmpIdx++
						zextReg := fmt.Sprintf("%%idx.arr.zext.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, elemType, elemLoad))
						return zextReg
					}
					return elemLoad
				}
			}
		}
	}
	return "0"
}

// generateNestedStrIndexRead 處理巢狀索引讀取 names[i][j] 或 .vals[idx][i]：
// 內層 names[i] / .vals[idx] 回傳 %str-long* 指標，外層 [j] 取出 data 指標後 GEP 到第 j 個位元組。
func (g *Generator) generateNestedStrIndexRead(sb *strings.Builder, innerIdx *parser.IndexExpression, index parser.Expression) string {
	// 評估內層索引表達式，取得 %str-long* 指標
	// 使用 generateExprPtr 取得元素指標（而非載入的 struct value），
	// 否則後續 GEP 會把 struct 值當作指標使用。
	strPtr := g.generateExprPtr(sb, innerIdx)
	if strPtr == "" || strPtr == "0" {
		// Fallback: generateExprPtr 無法取得指標時，嘗試載入值後存入臨時 alloca
		val := g.generateExprWithSB(sb, innerIdx)
		if val == "" || val == "0" {
			return "0"
		}
		recvType := g.exprResultLLVMType(innerIdx)
		if sb != nil && recvType != "" {
			g.tmpIdx++
			tmpAlloca := fmt.Sprintf("%%nestidx.tmp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, recvType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), recvType, val, recvType, tmpAlloca))
			strPtr = tmpAlloca
		} else {
			return "0"
		}
	}
	idx := g.generateExprWithSB(sb, index)
	// GEP 索引必須是 i64；若索引為 i8/i16/i32 SSA 值則 zext 到 i64
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(index)
		if idxType != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%nestidx.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}
	if sb == nil {
		return "0"
	}
	// 從 %str-long* 取出 data 指標（field 1）
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%nestidx.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataLoad := fmt.Sprintf("%%nestidx.data.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
		g.indent(), dataGEP, strPtr))
	sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
		g.indent(), dataLoad, dataGEP))
	// GEP 到第 i 個位元組並 load
	g.tmpIdx++
	charGEP := fmt.Sprintf("%%nestidx.char.gep.%d", g.tmpIdx)
	g.tmpIdx++
	charLoad := fmt.Sprintf("%%nestidx.char.val.%d", g.tmpIdx)
	g.tmpIdx++
	charZext := fmt.Sprintf("%%nestidx.char.zext.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
		g.indent(), charGEP, dataLoad, idx))
	sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
		g.indent(), charLoad, charGEP))
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
		g.indent(), charZext, charLoad))
	return charZext
}

// generateNestedStrIndexAssign 處理巢狀索引賦值 .vals[idx][i] = val：
// 內層 .vals[idx] 回傳 %str-long* 指標，外層 [i] 取出 data 指標後 GEP 到第 i 個位元組並 store。
func (g *Generator) generateNestedStrIndexAssign(sb *strings.Builder, innerIdx *parser.IndexExpression, index parser.Expression, value parser.Expression) string {
	// 評估內層索引表達式，取得 %str-long* 指標
	// 使用 generateIndexExprPtr 取得元素指標（而非載入的 str-long value）
	strPtr := g.generateIndexExprPtr(sb, innerIdx)
	if strPtr == "" || strPtr == "0" {
		return "0"
	}
	idx := g.generateExprWithSB(sb, index)
	// GEP 索引必須是 i64
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(index)
		if idxType != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%nestset.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}
	val := g.generateExprWithSB(sb, value)
	val = g.stripLLVMType(val)
	// 值需截斷為 i8（位元組存儲）
	if sb != nil {
		// 從 %str-long* 取出 data 指標（field 1）
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%nestset.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLoad := fmt.Sprintf("%%nestset.data.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
			g.indent(), dataGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
			g.indent(), dataLoad, dataGEP))
		// GEP 到第 i 個位元組
		g.tmpIdx++
		charGEP := fmt.Sprintf("%%nestset.char.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
			g.indent(), charGEP, dataLoad, idx))
		// 截斷值為 i8 並 store
		g.tmpIdx++
		truncReg := fmt.Sprintf("%%nestset.trunc.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i8\n", g.indent(), truncReg, val))
		sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n", g.indent(), truncReg, charGEP))
	}
	return "0"
}

func (g *Generator) generateAssignExpression(sb *strings.Builder, expr *parser.AssignExpression) string {
	// 巢狀欄位賦值: struct.field.subfield = value (e.g., self.p.len = val)
	if dot, ok := expr.Left.(*parser.DotExpression); ok {
		if innerDot, ok := dot.Receiver.(*parser.DotExpression); ok {
			if innerIdent, ok := innerDot.Receiver.(*parser.Identifier); ok {
				recvName := innerIdent.Value
				outerField := innerDot.Property
				innerField := dot.Property
				structName := ""
				if t, ok := g.varTypes[recvName]; ok {
					structName = strings.TrimPrefix(t, "%")
				}
				val := g.generateExprWithSB(sb, expr.Value)
				if fields, ok := g.structTypes[structName]; ok {
					outerIdx := -1
					var outerType string
					for i, f := range fields {
						if f.name == outerField {
							outerIdx = i
							outerType = f.typ
							break
						}
					}
					if outerIdx >= 0 && sb != nil {
						g.tmpIdx++
						outerGEP := fmt.Sprintf("%%set.nested.outer.gep.%d", g.tmpIdx)
						structTy := "%" + structName
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
							g.indent(), outerGEP, structTy, structTy, recvName, outerIdx))
						// Now GEP into the sub-struct (e.g., str-long has len at 0, data at 1)
						subFields, ok2 := g.structTypes[strings.TrimPrefix(outerType, "%")]
						if ok2 {
							subIdx := -1
							var subType string
							for i, f := range subFields {
								if f.name == innerField {
									subIdx = i
									subType = f.typ
									break
								}
							}
							if subIdx >= 0 {
								g.tmpIdx++
								subGEP := fmt.Sprintf("%%set.nested.sub.gep.%d", g.tmpIdx)
								sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
									g.indent(), subGEP, outerType, outerType, outerGEP, subIdx))
								sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), subType, val, subType, subGEP))
							}
						}
					}
				}
				return "0"
			}
		}
	}

	// 欄位賦值: u.name = value → GEP + store
	// 支援鏈式存取：.nodes[i].field = value, .nodes[i].str-val.len = value
	if dot, ok := expr.Left.(*parser.DotExpression); ok {
		varName := ""
		if ident, ok := dot.Receiver.(*parser.Identifier); ok {
			varName = ident.Value
		}
		fieldName := dot.Property
		val := g.generateExprWithSB(sb, expr.Value)

		// 判定 struct 名稱與基底指標
		// - Identifier receiver: 使用變數名稱（%%%s）
		// - 非 Identifier receiver: 使用 generateExprPtr 取得指標
		structName := ""
		basePtr := ""
		if varName != "" {
			if t, ok := g.varTypes[varName]; ok {
				structName = strings.TrimPrefix(t, "%")
			}
		} else {
			recvType := g.exprResultLLVMType(dot.Receiver)
			if strings.HasPrefix(recvType, "%") {
				structName = strings.TrimPrefix(recvType, "%")
			}
			if sb != nil {
				basePtr = g.generateExprPtr(sb, dot.Receiver)
				if basePtr == "" {
					// Fallback: 生成值後存入臨時 alloca
					val2 := g.generateExprWithSB(sb, dot.Receiver)
					if val2 != "" && val2 != "0" && recvType != "" {
						g.tmpIdx++
						tmpAlloca := fmt.Sprintf("%%assign.tmp.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, recvType))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), recvType, val2, recvType, tmpAlloca))
						basePtr = tmpAlloca
					}
				}
			}
		}

		g.tmpIdx++
		reg := fmt.Sprintf("%%set.gep.%d", g.tmpIdx)

		if fields, ok := g.structTypes[structName]; ok {
			fieldIdx := -1
			var fieldType string
			for i, f := range fields {
				if f.name == fieldName {
					fieldIdx = i
					fieldType = f.typ
					break
				}
			}
			if fieldIdx >= 0 && sb != nil {
				// 當欄位型別為 %str-long 但值是短字串字面量（%str-short*）時，
				// 需用 malloc 配置 heap buffer 並轉換為 %str-long value，
				// 因為 convertShortToLong 會指向 str-short 的 stack buffer，函數返回後失效。
				if fieldType == "%str-long" {
					if strLit, ok := expr.Value.(*parser.StringLiteral); ok && len(strLit.Value) <= 127 {
						val = g.convertStrLongLitToLongValue(sb, val)
					}
				}
				structTy := "%" + structName
				if basePtr != "" {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), reg, structTy, structTy, basePtr, fieldIdx))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
						g.indent(), reg, structTy, structTy, varName, fieldIdx))
				}
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, val, fieldType, reg))
			}
		}
		return "0"
	}

	// 索引賦值: arr[i] = val → GEP + store
	// 也支援 struct.field[i] = val（如 out.limbs[0] = v）
	if idxExpr, ok := expr.Left.(*parser.IndexExpression); ok {
		varName := ""
		if ident, ok := idxExpr.Left.(*parser.Identifier); ok {
			varName = ident.Value
		} else if dot, ok := idxExpr.Left.(*parser.DotExpression); ok {
			// struct.field[i] = val 模式
			return g.generateStructFieldIndexAssign(sb, dot, idxExpr.Index, expr.Value)
		} else if innerIdx, ok := idxExpr.Left.(*parser.IndexExpression); ok {
			// 巢狀索引賦值: .vals[idx][i] = val[i]
			return g.generateNestedStrIndexAssign(sb, innerIdx, idxExpr.Index, expr.Value)
		}
		idx := g.generateExprWithSB(sb, idxExpr.Index)
		val := g.generateExprWithSB(sb, expr.Value)

		// 取得陣列 LLVM 型別
		var llvmElemType string
		var arrayLLVMType string
		if t, ok := g.varTypes[varName]; ok {
			if t == "%arr" {
				// %arr type: load data pointer, bitcast, GEP, store
				llvmElemType = "i64"
				if et, ok := g.arrayElemTypes[varName]; ok {
					llvmElemType = et
				}

				// Load data pointer from arr struct
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%arr.set.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%arr.set.data.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
						g.indent(), dataGEP, g.varAddr(varName)))
					sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
						g.indent(), dataLoad, dataGEP))
				}

				// Bitcast to element type pointer
				g.tmpIdx++
				dataTyped := fmt.Sprintf("%%arr.set.typed.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
						g.indent(), dataTyped, dataLoad, llvmElemType))
				}

				// GEP to element index and store
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%arr.set.elem.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
						g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
					storeVal := val
					// 處理字串值儲存到陣列元素：
					// - 元素型別為 %str-long：字串字面量（str-short*）需轉為 str-long value；
					//   str-long 變數需 load 出 struct value
					// - 元素型別為 i64（str 以指標儲存）：字串指標需 ptrtoint
					// 注意：字串拼接（InfixExpression）結果是 %str-long value，不是指標，不在此處理。
					if llvmElemType == "%str-long" {
						if strLit, ok := expr.Value.(*parser.StringLiteral); ok {
							if len(strLit.Value) <= 127 {
								storeVal = g.convertStrLongLitToLongValue(sb, val)
							}
						} else if ident, ok := expr.Value.(*parser.Identifier); ok {
							if t, ok := g.varTypes[ident.Value]; ok && t == "%str-long" {
								// val is already a loaded %str-long value from generateExprWithSB,
								// so we can use it directly without another load.
								storeVal = val
							}
						}
					} else if llvmElemType == "i64" {
						needPtrToInt := false
						strLLVMType := ""
						switch e := expr.Value.(type) {
						case *parser.StringLiteral:
							needPtrToInt = true
							if len(e.Value) <= 127 {
								strLLVMType = "%str-short*"
							} else {
								strLLVMType = "%str-long*"
							}
						case *parser.Identifier:
							if t, ok := g.varTypes[e.Value]; ok {
								if t == "%str-long" {
									needPtrToInt = true
									strLLVMType = "%str-long*"
								} else if t == "%str-short" {
									needPtrToInt = true
									strLLVMType = "%str-short*"
								}
							}
						}
						if needPtrToInt {
							g.tmpIdx++
							convReg := fmt.Sprintf("%%arr.set.conv.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %s %s to i64\n", g.indent(), convReg, strLLVMType, val))
							storeVal = convReg
						}
					}
					if storeVal == val && strings.HasPrefix(val, "%") {
						srcType := g.intExprLLVMType(expr.Value)
						// Only convert between integer types; skip for struct types (e.g. %str-long)
						if srcType != "" && srcType != llvmElemType && g.isIntegerLLVMType(srcType) && g.isIntegerLLVMType(llvmElemType) {
							g.tmpIdx++
							convReg := fmt.Sprintf("%%arr.set.conv.%d", g.tmpIdx)
							if srcType == "i64" {
								// i64 → smaller type: trunc
								sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, srcType, val, llvmElemType))
							} else if llvmElemType == "i64" {
								// smaller type → i64: zext
								sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), convReg, srcType, val))
							} else {
								// smaller → smaller (both non-i64): trunc to target
								sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, srcType, val, llvmElemType))
							}
							storeVal = convReg
						}
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
						g.indent(), llvmElemType, storeVal, llvmElemType, elemGEP))
				}
				return "0"
			}

			if t == "%vec" {
				// %vec type: load data pointer (field 2), bitcast, GEP, store
				llvmElemType = "i64"
				if et, ok := g.arrayElemTypes[varName]; ok {
					llvmElemType = et
				}

				// Load data pointer from vec struct (field 2)
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%vec.set.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%vec.set.data.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %%%s, i32 0, i32 2\n",
						g.indent(), dataGEP, varName))
					sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
						g.indent(), dataLoad, dataGEP))
				}

				// Bitcast to element type pointer
				g.tmpIdx++
				dataTyped := fmt.Sprintf("%%vec.set.typed.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
						g.indent(), dataTyped, dataLoad, llvmElemType))
				}

				// Coerce val to element type if needed (e.g., i64 → i32)
				storeVal := val
				if llvmElemType != "i64" && strings.HasPrefix(val, "%") {
					g.tmpIdx++
					truncReg := fmt.Sprintf("%%vec.set.trunc.%d", g.tmpIdx)
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n",
							g.indent(), truncReg, val, llvmElemType))
					}
					storeVal = truncReg
				}

				// GEP to element index and store
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%vec.set.elem.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
						g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
						g.indent(), llvmElemType, storeVal, llvmElemType, elemGEP))

					// Auto-update len (field 0) to max(len, idx+1). Without this,
					// sha256/hmac-sha256/tls-prf receive vec.len == 0 even after
					// elements were written via vec[i] = val, producing wrong outputs.
					g.tmpIdx++
					lenGEP := fmt.Sprintf("%%vec.set.len.gep.%d", g.tmpIdx)
					g.tmpIdx++
					curLen := fmt.Sprintf("%%vec.set.cur-len.%d", g.tmpIdx)
					g.tmpIdx++
					newLen := fmt.Sprintf("%%vec.set.new-len.%d", g.tmpIdx)
					g.tmpIdx++
					cmpReg := fmt.Sprintf("%%vec.set.cmp.%d", g.tmpIdx)
					g.tmpIdx++
					finalLen := fmt.Sprintf("%%vec.set.final-len.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %%%s, i32 0, i32 0\n",
						g.indent(), lenGEP, varName))
					sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
					sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
					sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
					sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
				}
				return "0"
			}

			if t == "%str-long" {
				// %str-long type: load data pointer (field 1), GEP, store
				// Also auto-update len (field 0) to max(len, idx+1)
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%str-long.set.data.gep.%d", g.tmpIdx)
				g.tmpIdx++
				dataLoad := fmt.Sprintf("%%str-long.set.data.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
						g.indent(), dataGEP, g.varAddr(varName)))
					sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
						g.indent(), dataLoad, dataGEP))
				}

				// GEP into data with index
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%str-long.set.elem.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
						g.indent(), elemGEP, dataLoad, idx))
					storeVal := val
					if strings.HasPrefix(val, "%") {
						valType := g.intExprLLVMType(expr.Value)
						if strings.HasPrefix(valType, "i") && valType != "i8" {
							g.tmpIdx++
							truncReg := fmt.Sprintf("%%trunc.i8.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
							storeVal = truncReg
						}
					}
					sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n",
						g.indent(), storeVal, elemGEP))

					// Auto-update len: load cur len, compute idx+1, store max
					g.tmpIdx++
					lenGEP := fmt.Sprintf("%%str-long.set.len.gep.%d", g.tmpIdx)
					g.tmpIdx++
					curLen := fmt.Sprintf("%%str-long.set.cur-len.%d", g.tmpIdx)
					g.tmpIdx++
					newLen := fmt.Sprintf("%%str-long.set.new-len.%d", g.tmpIdx)
					g.tmpIdx++
					cmpReg := fmt.Sprintf("%%str-long.set.cmp.%d", g.tmpIdx)
					g.tmpIdx++
					finalLen := fmt.Sprintf("%%str-long.set.final-len.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
						g.indent(), lenGEP, g.varAddr(varName)))
					sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
					sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
					sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
					sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
				}
				return "0"
			}

			if t == "%str-short" {
				// %str-short type: GEP to field 1 (data array), GEP into array, store
				// Also auto-update len (field 0) to max(len, idx+1)
				g.tmpIdx++
				fieldGEP := fmt.Sprintf("%%str-longsm.set.field.%d", g.tmpIdx)
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%str-longsm.set.elem.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n",
						g.indent(), fieldGEP, g.varAddr(varName)))
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [127 x i8], [127 x i8]* %s, i64 0, i64 %s\n",
						g.indent(), elemGEP, fieldGEP, idx))
					storeVal := val
					if strings.HasPrefix(val, "%") {
						valType := g.intExprLLVMType(expr.Value)
						if strings.HasPrefix(valType, "i") && valType != "i8" {
							g.tmpIdx++
							truncReg := fmt.Sprintf("%%trunc.i8.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
							storeVal = truncReg
						}
					}
					sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n",
						g.indent(), storeVal, elemGEP))

					// Auto-update len: load cur len (i8), zext to i64, compute idx+1, store max
					g.tmpIdx++
					lenGEP := fmt.Sprintf("%%str-short.set.len.gep.%d", g.tmpIdx)
					g.tmpIdx++
					curLen8 := fmt.Sprintf("%%str-short.set.cur-len8.%d", g.tmpIdx)
					g.tmpIdx++
					curLen := fmt.Sprintf("%%str-short.set.cur-len.%d", g.tmpIdx)
					g.tmpIdx++
					newLen := fmt.Sprintf("%%str-short.set.new-len.%d", g.tmpIdx)
					g.tmpIdx++
					cmpReg := fmt.Sprintf("%%str-short.set.cmp.%d", g.tmpIdx)
					g.tmpIdx++
					finalLen64 := fmt.Sprintf("%%str-short.set.final-len64.%d", g.tmpIdx)
					g.tmpIdx++
					finalLen8 := fmt.Sprintf("%%str-short.set.final-len8.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 0\n",
						g.indent(), lenGEP, g.varAddr(varName)))
					sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), curLen8, lenGEP))
					sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), curLen, curLen8))
					sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
					sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
					sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen64, cmpReg, newLen, curLen))
					sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i8\n", g.indent(), finalLen8, finalLen64))
					sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n", g.indent(), finalLen8, lenGEP))
				}
				return "0"
			}

			if strings.HasPrefix(t, "[") {
				closeB := strings.IndexByte(t, ']')
				if closeB > 0 {
					// t is LLVM type like "[4 x i64]", parse element directly
					inner := t[1:closeB]
					xIdx := strings.LastIndex(inner, " x ")
					if xIdx >= 0 {
						llvmElemType = inner[xIdx+3:]
					} else {
						llvmElemType = "i64"
					}
					arrayLLVMType = t
				}
			}
		}
		if llvmElemType == "" {
			llvmElemType = "i8"
			arrayLLVMType = "[8 x i8]"
		}

		// GEP + store
		g.tmpIdx++
		gepReg := fmt.Sprintf("%%set.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i64 0, i64 %s\n",
				g.indent(), gepReg, arrayLLVMType, arrayLLVMType, varName, idx))
			storeVal := val
			if llvmElemType == "i8" && strings.HasPrefix(val, "%") {
				valType := g.intExprLLVMType(expr.Value)
				if strings.HasPrefix(valType, "i") && valType != "i8" {
					g.tmpIdx++
					truncReg := fmt.Sprintf("%%trunc.i8.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
					storeVal = truncReg
				}
			}
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
				g.indent(), llvmElemType, storeVal, llvmElemType, gepReg))
		}
		return "0"
	}

	return "0"
}

// generateIndexExpression 處理 arr[i] 讀取
// 也支援 struct.field[i] 讀取（如 out.limbs[i]）
func (g *Generator) generateIndexExpression(sb *strings.Builder, expr *parser.IndexExpression) string {
	// 直接使用 alloca 名稱（而非 loaded value）
	varName := ""
	if ident, ok := expr.Left.(*parser.Identifier); ok {
		varName = ident.Value
	} else if dot, ok := expr.Left.(*parser.DotExpression); ok {
		// struct.field[i] 讀取模式
		return g.generateStructFieldIndexRead(sb, dot, expr.Index)
	} else if lit, ok := expr.Left.(*parser.StringLiteral); ok {
		// 模組字串常量傳播後的情況：HEX-LOWER[b>>4] 中的 Left 變成 StringLiteral
		// 為此我們需要將字串常量分配到 stack 上，然後 GEP 索引
		return g.generateStringLiteralIndex(sb, lit, expr.Index)
	} else if innerIdx, ok := expr.Left.(*parser.IndexExpression); ok {
		// 巢狀索引讀取：例如 .vals[idx][i]
		// 內層 .vals[idx] 由 generateStructFieldIndexRead 回傳 %str-long* 指標，
		// 外層 [i] 需取出 str-long 的 data 指標後 GEP 到第 i 個位元組。
		return g.generateNestedStrIndexRead(sb, innerIdx, expr.Index)
	}
	idx := g.generateExprWithSB(sb, expr.Index)
	// GEP 索引必須是 i64；若索引為 i8/i16/i32 SSA 值則 zext 到 i64
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(expr.Index)
		if idxType != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%idx.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}

	// String indexing: s[i] → extract data ptr from %str-long, then GEP into it
	if varName != "" {
		if t, ok := g.varTypes[varName]; ok && t == "%str-long" {
			strPtr := g.varAddr(varName)
			dataPtr := g.extractStrDataPtr(sb, strPtr)
			g.tmpIdx++
			charGEP := fmt.Sprintf("%%str-longidx.gep.%d", g.tmpIdx)
			g.tmpIdx++
			charLoad := fmt.Sprintf("%%str-longidx.val.%d", g.tmpIdx)
			g.tmpIdx++
			charZext := fmt.Sprintf("%%str-longidx.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
					g.indent(), charGEP, dataPtr, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
					g.indent(), charZext, charLoad))
			}
			return charZext
		}
		// str-short indexing: GEP into field 1 ([127 x i8]) directly
		if t, ok := g.varTypes[varName]; ok && t == "%str-short" {
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str-longsm.idx.gep.%d", g.tmpIdx)
			g.tmpIdx++
			charGEP := fmt.Sprintf("%%str-longsm.idx.elem.%d", g.tmpIdx)
			g.tmpIdx++
			charLoad := fmt.Sprintf("%%str-longsm.idx.val.%d", g.tmpIdx)
			g.tmpIdx++
			charZext := fmt.Sprintf("%%str-longsm.idx.zext.%d", g.tmpIdx)
			if sb != nil {
				// GEP to field 1 (the [127 x i8] array)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %%%s, i32 0, i32 1\n",
					g.indent(), dataGEP, varName))
				// GEP into the array at index
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [127 x i8], [127 x i8]* %s, i64 0, i64 %s\n",
					g.indent(), charGEP, dataGEP, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
					g.indent(), charZext, charLoad))
			}
			return charZext
		}
	}

	// 取得變數的 LLVM 型別
	var llvmElemType string
	var arrayLLVMType string
	if t, ok := g.varTypes[varName]; ok {
		if t == "%arr" {
			// %arr type: load data pointer, bitcast, GEP, load
			llvmElemType = "i64"
			if et, ok := g.arrayElemTypes[varName]; ok {
				llvmElemType = et
			}

			// Determine the base reference: @name for globals, %name for local allocas.
			arrRef := llvmVarRef(varName)
			if g.globalVars != nil && g.globalVars[varName] {
				arrRef = llvmGlobalRef(varName)
			}

			// Load data pointer from arr struct
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%arr.idx.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%arr.idx.data.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
					g.indent(), dataGEP, arrRef))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))
			}

			// Bitcast to element type pointer
			g.tmpIdx++
			dataTyped := fmt.Sprintf("%%arr.idx.typed.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
			}

			// GEP to element
			g.tmpIdx++
			elemGEP := fmt.Sprintf("%%arr.idx.elem.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}

			// Load element
			g.tmpIdx++
			elemLoad := fmt.Sprintf("%%arr.idx.val.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
					g.indent(), elemLoad, llvmElemType, llvmElemType, elemGEP))
			}
			// 統一回傳 i64：若元素為 i8 則 zext 到 i64
			if llvmElemType == "i8" {
				g.tmpIdx++
				zextReg := fmt.Sprintf("%%arr.idx.zext.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, elemLoad))
				}
				return zextReg
			}
			return elemLoad
		}

		if t == "%vec" {
			// %vec type: load data pointer (field 2), bitcast, GEP, load
			llvmElemType = "i64"
			if et, ok := g.arrayElemTypes[varName]; ok {
				llvmElemType = et
			}

			// Determine the base reference: @name for globals, %name for local allocas.
			vecRef := llvmVarRef(varName)
			if g.globalVars != nil && g.globalVars[varName] {
				vecRef = llvmGlobalRef(varName)
			}

			// Load data pointer from vec struct (field 2)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%vec.idx.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%vec.idx.data.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, vecRef))
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
					g.indent(), dataLoad, dataGEP))
			}

			// Bitcast to element type pointer
			g.tmpIdx++
			dataTyped := fmt.Sprintf("%%vec.idx.typed.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
			}

			// GEP to element
			g.tmpIdx++
			elemGEP := fmt.Sprintf("%%vec.idx.elem.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}

			// Load element
			g.tmpIdx++
			elemLoad := fmt.Sprintf("%%vec.idx.val.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
					g.indent(), elemLoad, llvmElemType, llvmElemType, elemGEP))
			}
			// 當元素型別為整數且小於 i64 時，零擴展至 i64 以與下游消費端（運算、print 等）一致。
			// 注意：struct 型別（如 %str-long）不應 zext。
			if llvmElemType == "i1" || llvmElemType == "i8" || llvmElemType == "i16" || llvmElemType == "i32" {
				g.tmpIdx++
				zextReg := fmt.Sprintf("%%vec.idx.zext.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n",
						g.indent(), zextReg, llvmElemType, elemLoad))
				}
				return zextReg
			}
			// float（32-bit）→ i64：先 bitcast 至 i32 再 zext 至 i64
			if llvmElemType == "float" {
				g.tmpIdx++
				bcReg := fmt.Sprintf("%%vec.idx.bc.%d", g.tmpIdx)
				g.tmpIdx++
				zextReg := fmt.Sprintf("%%vec.idx.zext.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast float %s to i32\n",
						g.indent(), bcReg, elemLoad))
					sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n",
						g.indent(), zextReg, bcReg))
				}
				return zextReg
			}
			// double（64-bit）→ i64：直接 bitcast
			if llvmElemType == "double" {
				g.tmpIdx++
				bcReg := fmt.Sprintf("%%vec.idx.bc.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast double %s to i64\n",
						g.indent(), bcReg, elemLoad))
				}
				return bcReg
			}
			return elemLoad
		}

		// t is LLVM type like "[4 x i64]" (g.varTypes stores LLVM types)
		if strings.HasPrefix(t, "[") {
			closeB := strings.IndexByte(t, ']')
			if closeB > 0 {
				// Parse LLVM array format: [4 x i64] → element is "i64"
				inner := t[1:closeB] // "4 x i64"
				xIdx := strings.LastIndex(inner, " x ")
				if xIdx >= 0 {
					llvmElemType = inner[xIdx+3:] // "i64"
				} else {
					llvmElemType = "i64"
				}
				arrayLLVMType = t
			}
		}
	}
	if llvmElemType == "" {
		// Check if this is a []byte (i8*) type
		if t, ok := g.varTypes[varName]; ok && t == "i8*" {
			// []byte parameter: load data pointer from i8** parameter, then GEP
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%idx.data.load.%d", g.tmpIdx)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%idx.gep.%d", g.tmpIdx)
			g.tmpIdx++
			loadReg := fmt.Sprintf("%%idx.load.%d", g.tmpIdx)
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%idx.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %%%s\n", g.indent(), dataLoad, varName))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), gepReg, dataLoad, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), loadReg, gepReg))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, loadReg))
			}
			return zextReg
		}
		// []str / []T (any T whose LLVM type ends in *): 載入資料指標、GEP、return %T*（不 load，str 為 struct）
		if t, ok := g.varTypes[varName]; ok && strings.HasPrefix(t, "%") && strings.HasSuffix(t, "*") {
			elemType := strings.TrimSuffix(t, "*")
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%idx.data.load.%d", g.tmpIdx)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%idx.gep.%d", g.tmpIdx)
			if sb != nil {
				// 載入 slice 的資料指標（%T** → %T*）
				sb.WriteString(fmt.Sprintf("%s%s = load %s*, %s** %%%s\n", g.indent(), dataLoad, elemType, elemType, varName))
				// GEP 到第 idx 個元素（%T*，不 load）
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), gepReg, elemType, elemType, dataLoad, idx))
			}
			return gepReg
		}
		llvmElemType = "i8"
		arrayLLVMType = "[8 x i8]"
	}

	// GEP 取得元素指標：使用 %varName (alloca) 而非 loaded value
	g.tmpIdx++
	gepReg := fmt.Sprintf("%%idx.gep.%d", g.tmpIdx)
	// Determine the base reference: @name for globals, %name for local allocas.
	arrRef := llvmVarRef(varName)
	if g.globalVars != nil && g.globalVars[varName] {
		arrRef = llvmGlobalRef(varName)
	}
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
			g.indent(), gepReg, arrayLLVMType, arrayLLVMType, arrRef, idx))
	}

	// Load 元素值（非 i8* 型別的 fallback，如 str 的 i8 元素）
	g.tmpIdx++
	loadReg := fmt.Sprintf("%%idx.load.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
			g.indent(), loadReg, llvmElemType, llvmElemType, gepReg))
	}
	// 統一回傳 i64：若元素為 i8 則 zext 到 i64，與其他索引路徑一致
	if llvmElemType == "i8" {
		g.tmpIdx++
		zextReg := fmt.Sprintf("%%idx.zext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, loadReg))
		}
		return zextReg
	}
	return loadReg
}

// generateStringLiteralIndex 處理字串常量的索引運算（用於模組字串常量傳播後的場景）
// 例如：HEX-LOWER[b >> 4] 在 resolveModuleConstants 後，Left 變成 StringLiteral。
// 對於短字串（≤127 bytes），分配 %str-short；對於長字串，分配 %str-long。
func (g *Generator) generateStringLiteralIndex(sb *strings.Builder, lit *parser.StringLiteral, index parser.Expression) string {
	idx := g.generateExprWithSB(sb, index)
	// GEP 索引必須是 i64；若索引為 i8/i16/i32 SSA 值則 zext 到 i64
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(index)
		if idxType != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%str-longlit.idx.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}
	strLen := len(lit.Value)
	g.tmpIdx++
	strAlloca := fmt.Sprintf("%%str-longlit.idx.%d", g.tmpIdx)
	g.tmpIdx++
	dataPtr := fmt.Sprintf("%%str-longlit.idx.ptr.%d", g.tmpIdx)
	g.tmpIdx++
	charGEP := fmt.Sprintf("%%str-longlit.idx.gep.%d", g.tmpIdx)
	g.tmpIdx++
	charLoad := fmt.Sprintf("%%str-longlit.idx.val.%d", g.tmpIdx)
	g.tmpIdx++
	zextReg := fmt.Sprintf("%%str-longlit.idx.zext.%d", g.tmpIdx)

	if sb == nil {
		return zextReg
	}

	if strLen <= 127 {
		// SSO: %str-short = { i8, [127 x i8] }
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-short\n", g.indent(), strAlloca))
		// field 0: i8 = strLen | 0x80
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%str-longlit.idx.len.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i8 %d, i8* %s\n", g.indent(), strLen|0x80, lenGEP))
		// field 1: [127 x i8] - copy literal data
		g.tmpIdx++
		dataFieldGEP := fmt.Sprintf("%%str-longlit.idx.datafield.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n",
			g.indent(), dataFieldGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dataPtr, dataFieldGEP))
		// Emit the literal as a global string
		litIdx := g.stringIdx
		g.stringIdx++
		escaped := g.escapeLLVMString(lit.Value)
		g.fmtGlobals = append(g.fmtGlobals,
			fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", litIdx, strLen, escaped))
		srcPtr := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0)",
			strLen, strLen, litIdx)
		sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, %s, i64 %d)\n", g.indent(), dataPtr, srcPtr, strLen))
		// GEP into the array
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [127 x i8], [127 x i8]* %s, i64 0, i64 %s\n",
			g.indent(), charGEP, dataFieldGEP, idx))
	} else {
		// Long string: %str-long = { i64, i8* }
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strAlloca))
		// field 0: i64 = strLen
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%str-longlit.idx.len.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
		// field 1: i8* = data pointer
		g.tmpIdx++
		dataFieldGEP := fmt.Sprintf("%%str-longlit.idx.datafield.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
			g.indent(), dataFieldGEP, strAlloca))
		// Emit the literal as a global string
		litIdx := g.stringIdx
		g.stringIdx++
		escaped := g.escapeLLVMString(lit.Value)
		g.fmtGlobals = append(g.fmtGlobals,
			fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", litIdx, strLen, escaped))
		srcPtr := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0)",
			strLen, strLen, litIdx)
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), srcPtr, dataFieldGEP))
		// GEP into the data array
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
			g.indent(), charGEP, srcPtr, idx))
	}

	// Load the byte and zext to i64
	sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), charLoad, charGEP))
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, charLoad))
	return zextReg
}

// generateStringCmp 使用 strcmp 進行字串比較，回傳 zext 後的 i64 結果。
// 適用於 ==, !=, <, >, <=, >= 等比較運算子。
func (g *Generator) generateStringCmp(sb *strings.Builder, expr *parser.InfixExpression) string {
	leftPtr := g.getStrPtr(sb, expr.Left)
	rightPtr := g.getStrPtr(sb, expr.Right)
	leftData := g.extractDataFromExpr(sb, expr.Left, leftPtr)
	rightData := g.extractDataFromExpr(sb, expr.Right, rightPtr)

	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%str-longcmp.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call i32 @strcmp(i8* %s, i8* %s)\n", g.indent(), cmpReg, leftData, rightData))
	}

	// strcmp 回傳 0=相等, <0=a<b, >0=a>b
	var cmpOp string
	switch expr.Operator {
	case "==":
		cmpOp = "eq"
	case "!=":
		cmpOp = "ne"
	case "<":
		cmpOp = "slt"
	case ">":
		cmpOp = "sgt"
	case "<=":
		cmpOp = "sle"
	case ">=":
		cmpOp = "sge"
	default:
		cmpOp = "eq"
	}

	g.tmpIdx++
	resultReg := fmt.Sprintf("%%str-longcmpres.%d", g.tmpIdx)
	g.tmpIdx++
	extReg := fmt.Sprintf("%%str-longcmpext.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = icmp %s i32 %s, 0\n", g.indent(), resultReg, cmpOp, cmpReg))
		sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, resultReg))
	}
	return extReg
}

// generateStringCmpI1 使用 strcmp 進行字串比較，直接回傳 i1 結果。
// 用於 if/while 條件式中。
func (g *Generator) generateStringCmpI1(sb *strings.Builder, expr *parser.InfixExpression) string {
	leftPtr := g.getStrPtr(sb, expr.Left)
	rightPtr := g.getStrPtr(sb, expr.Right)
	leftData := g.extractDataFromExpr(sb, expr.Left, leftPtr)
	rightData := g.extractDataFromExpr(sb, expr.Right, rightPtr)

	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%str-longcmp.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call i32 @strcmp(i8* %s, i8* %s)\n", g.indent(), cmpReg, leftData, rightData))
	}

	var cmpOp string
	switch expr.Operator {
	case "==":
		cmpOp = "eq"
	case "!=":
		cmpOp = "ne"
	case "<":
		cmpOp = "slt"
	case ">":
		cmpOp = "sgt"
	case "<=":
		cmpOp = "sle"
	case ">=":
		cmpOp = "sge"
	default:
		cmpOp = "eq"
	}

	g.tmpIdx++
	resultReg := fmt.Sprintf("%%str-longcmpres.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = icmp %s i32 %s, 0\n", g.indent(), resultReg, cmpOp, cmpReg))
	}
	return resultReg
}

func (g *Generator) generateStructLiteral(sb *strings.Builder, expr *parser.StructLiteral) string {
	// struct literal: user { name: 'abc', age: 20 }
	// 在 generateLet 中處理（varLLVMType 已回傳 struct type）
	// 這裡只產生一個 placeholder
	return "{ }"
}

func (g *Generator) generateInfixI1(sb *strings.Builder, expr *parser.InfixExpression) string {
	// Option tag comparison: x == err/nil/ok or x != err/nil/ok for %option typed variables
	// Also handles tagged enum variants: x == status1, x == status2, etc.
	if expr.Operator == "==" || expr.Operator == "!=" {
		if leftIdent, ok := expr.Left.(*parser.Identifier); ok {
			// Check if right side is Identifier (err/nil/ok/enum-variant) or NilLiteral (nil)
			var tag int64 = -1
			if rightIdent, ok := expr.Right.(*parser.Identifier); ok {
				if rightIdent.Value == "err" {
					tag = 2
				} else if rightIdent.Value == "nil" {
					tag = 1
				} else if rightIdent.Value == "ok" {
					tag = 0
				} else if g.enumVariantIndex != nil {
					// Check if it's a tagged enum variant
					if idx, ok := g.enumVariantIndex[rightIdent.Value]; ok {
						tag = idx
					}
				}
			} else if _, ok := expr.Right.(*parser.NilLiteral); ok {
				// nil is parsed as NilLiteral, not Identifier
				tag = 1
			}
			if tag >= 0 {
				if t, ok := g.varTypes[leftIdent.Value]; ok && t == "%option" {
					g.tmpIdx++
					tagGEP := fmt.Sprintf("%%opt.cmp.gep.%d", g.tmpIdx)
					g.tmpIdx++
					tagLoad := fmt.Sprintf("%%opt.cmp.load.%d", g.tmpIdx)
					g.tmpIdx++
					cmpReg := fmt.Sprintf("%%cmp.i1.%d", g.tmpIdx)
					cmpOp := "eq"
					if expr.Operator == "!=" {
						cmpOp = "ne"
					}
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 0\n", g.indent(), tagGEP, leftIdent.Value))
						sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), tagLoad, tagGEP))
						sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %d\n", g.indent(), cmpReg, cmpOp, tagLoad, tag))
					}
					return cmpReg
				}
			}
		}
	}

	// 字串比較：使用 strcmp 直接回傳 i1
	if g.isStringExpr(expr.Left) || g.isStringExpr(expr.Right) {
		switch expr.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return g.generateStringCmpI1(sb, expr)
		}
	}

	left := g.generateExprWithSB(sb, expr.Left)
	right := g.generateExprWithSB(sb, expr.Right)
	g.tmpIdx++
	reg := fmt.Sprintf("%%cmp.i1.%d", g.tmpIdx)
	cmpOp := ""
	switch expr.Operator {
	case "==":
		cmpOp = "eq"
	case "!=":
		cmpOp = "ne"
	case "<":
		cmpOp = "olt"
	case ">":
		cmpOp = "ogt"
	case "<=":
		cmpOp = "ole"
	case ">=":
		cmpOp = "oge"
	default:
		return g.generateInfix(sb, expr) // fallback
	}
	// 浮點比較：使用 fcmp
	if ft := g.floatLLVMType(expr.Left); ft != "" || g.floatLLVMType(expr.Right) != "" {
		ft := "double"
		if g.floatLLVMType(expr.Left) == "float" || g.floatLLVMType(expr.Right) == "float" {
			ft = "float"
		}
		if g.floatLLVMType(expr.Left) == "double" || g.floatLLVMType(expr.Right) == "double" {
			ft = "double"
		}
		lc := g.coerceToFloatReg(sb, left, expr.Left, ft)
		rc := g.coerceToFloatReg(sb, right, expr.Right, ft)
		// fcmp 的 eq/ne 需要加 o 前綴
		fcmpOp := cmpOp
		if cmpOp == "eq" {
			fcmpOp = "oeq"
		} else if cmpOp == "ne" {
			fcmpOp = "one"
		}
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = fcmp %s %s %s, %s\n", g.indent(), reg, fcmpOp, ft, lc, rc))
		}
		return reg
	}
	cmpType := g.arithLLVMType(expr.Left, expr.Right)
	lc := g.coerceToInt(sb, left, expr.Left, cmpType)
	rc := g.coerceToInt(sb, right, expr.Right, cmpType)
	// 整數比較的 icmp 操作名稱
	intCmpOp := cmpOp
	switch cmpOp {
	case "olt":
		intCmpOp = "slt"
	case "ogt":
		intCmpOp = "sgt"
	case "ole":
		intCmpOp = "sle"
	case "oge":
		intCmpOp = "sge"
	}
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = icmp %s %s %s, %s\n", g.indent(), reg, intCmpOp, cmpType, lc, rc))
	}
	return reg
}

// coerceToFloatReg 將 SSA 值或字面常量轉換為目標浮點型別（float/double）。
// 這是 generateInfixI1 用的輔助函式，因為 generateInfix 中的 coerceToFloat 是閉包。
func (g *Generator) coerceToFloatReg(sb *strings.Builder, v string, exprForType parser.Expression, targetType string) string {
	if v == "" || targetType == "" {
		return v
	}
	// 浮點字面常量（含 . 或 e/E）→ 保持原樣
	if _, err := fmt.Sscanf(v, "%f", new(float64)); err == nil && strings.ContainsAny(v, ".eE") {
		return v
	}
	// SSA 暫存器
	if strings.HasPrefix(v, "%") {
		srcType := g.floatLLVMType(exprForType)
		if srcType == targetType {
			return v
		}
		if srcType != "" {
			if sb != nil {
				g.tmpIdx++
				cvtReg := fmt.Sprintf("%%fpcvt.%d", g.tmpIdx)
				if targetType == "double" && srcType == "float" {
					sb.WriteString(fmt.Sprintf("%s%s = fpext float %s to double\n", g.indent(), cvtReg, v))
				} else if targetType == "float" && srcType == "double" {
					sb.WriteString(fmt.Sprintf("%s%s = fptrunc double %s to float\n", g.indent(), cvtReg, v))
				} else {
					return v
				}
				return cvtReg
			}
			return v
		}
		// 整數 → 浮點
		if sb != nil {
			intType := g.intExprLLVMType(exprForType)
			g.tmpIdx++
			cvtReg := fmt.Sprintf("%%sitofp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to %s\n", g.indent(), cvtReg, intType, v, targetType))
			return cvtReg
		}
		return v
	}
	// 整數字面常量
	if _, err := fmt.Sscanf(v, "%d", new(int64)); err == nil {
		return v + ".0"
	}
	return v
}

func (g *Generator) generateArrayLiteral(sb *strings.Builder, arr *parser.ArrayLiteral) string {
	// Array literals for fixed-size arrays are handled directly in generateLet.
	// This function is kept for potential standalone use (e.g. in expressions).
	return "0"
}

func (g *Generator) generateSliceExpression(sb *strings.Builder, expr *parser.SliceExpression) string {
	r := expr.Range
	leftVal := g.generateExprWithSB(sb, expr.Left)

	// Determine if the left expression is a vec, arr, or str by resolving its name
	varName := ""
	if ident, ok := expr.Left.(*parser.Identifier); ok {
		varName = ident.Value
	}

	isVec := false
	isArr := false
	isStr := false
	isStrShort := false
	if varName != "" && g.varTypes != nil {
		if t, ok := g.varTypes[varName]; ok {
			isVec = t == "%vec"
			isArr = t == "%arr"
			isStr = t == "%str-long"
			isStrShort = t == "%str-short"
		}
	}

	if !isVec && !isArr && !isStr && !isStrShort {
		sb.WriteString(fmt.Sprintf("%s; slice expression (non-vec/arr/str): %s\n", g.indent(), leftVal))
		return "0"
	}

	g.tmpIdx++
	tid := g.tmpIdx
	resultType := "%vec"
	if isStr || isStrShort {
		resultType = "%str-long"
	}
	resultReg := fmt.Sprintf("%%slic.%d", tid)

	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), resultReg, resultType))
	}

	// Variables for source fields
	var srcLen, srcData, srcCap string

	if isStrShort {
		strPtr := fmt.Sprintf("%%%s", varName)
		srcLen = g.extractStrShortLen(sb, strPtr)
		srcData = g.extractStrShortDataPtr(sb, strPtr)
		srcCap = srcLen
	} else {
		// Determine struct type name for source GEPs
		structType := "%arr"
		if isVec {
			structType = "%vec"
		} else if isStr {
			structType = "%str-long"
		}

		// Data field index: %arr → field 1, %vec → field 2, %str-long → field 1
		dataField := uint32(1)
		if isVec {
			dataField = 2
		}

		// Load source len (field 0 for both %arr and %vec)
		g.tmpIdx++
		srcLenGEP := fmt.Sprintf("%%slice.srclen.gep.%d", g.tmpIdx)
		g.tmpIdx++
		srcLen = fmt.Sprintf("%%slice.srclen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 0\n",
				g.indent(), srcLenGEP, structType, structType, varName))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
				g.indent(), srcLen, srcLenGEP))
		}

		// Load source data pointer
		g.tmpIdx++
		srcDataGEP := fmt.Sprintf("%%slice.srcdata.gep.%d", g.tmpIdx)
		g.tmpIdx++
		srcData = fmt.Sprintf("%%slice.srcdata.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
				g.indent(), srcDataGEP, structType, structType, varName, dataField))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n",
				g.indent(), srcData, srcDataGEP))
		}

		// Source capacity: for %vec load from field 1, for %arr/%str-long use len
		g.tmpIdx++
		srcCap = fmt.Sprintf("%%slice.srccap.%d", g.tmpIdx)
		if isVec {
			g.tmpIdx++
			srcCapGEP := fmt.Sprintf("%%slice.srccap.gep.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 1\n",
					g.indent(), srcCapGEP, structType, structType, varName))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
					g.indent(), srcCap, srcCapGEP))
			}
		} else {
			// %arr/%str-long has no cap field; use len as cap
			srcCap = srcLen
		}
	}

	// Compute start: 0 if no start, else compile(start) + (1 if ( exclusive)
	startReg := "0"
	if r != nil && r.Start != nil {
		startVal := g.generateExprWithSB(sb, r.Start)
		if !r.LeftInc {
			// ( exclusive: start = start + 1
			g.tmpIdx++
			startPlus := fmt.Sprintf("%%vec.start.plus.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), startPlus, startVal))
			}
			startReg = startPlus
		} else {
			startReg = startVal
		}
	}

	// Compute new len
	var newLenReg string
	if r == nil || (r.Start == nil && r.End == nil) {
		// [..] full slice: new_len = src_len
		newLenReg = srcLen
	} else if r.Start == nil && r.End != nil {
		// [..end]: new_len = end + (1 if ] else 0)
		endVal := g.generateExprWithSB(sb, r.End)
		if r.RightInc {
			g.tmpIdx++
			newLenReg = fmt.Sprintf("%%vec.newlen.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), newLenReg, endVal))
			}
		} else {
			newLenReg = endVal
		}
	} else if r.Start != nil && r.End == nil {
		// [start..]: new_len = src_len - start
		g.tmpIdx++
		newLenReg = fmt.Sprintf("%%vec.newlen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
				g.indent(), newLenReg, srcLen, startReg))
		}
	} else {
		// [start..end]: new_len = end - start + (1 if ] else 0)
		endVal := g.generateExprWithSB(sb, r.End)
		g.tmpIdx++
		subReg := fmt.Sprintf("%%vec.sublen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
				g.indent(), subReg, endVal, startReg))
		}
		if r.RightInc {
			g.tmpIdx++
			newLenReg = fmt.Sprintf("%%vec.newlen.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), newLenReg, subReg))
			}
		} else {
			newLenReg = subReg
		}
	}

	// Compute new cap: src_cap - start
	g.tmpIdx++
	newCapReg := fmt.Sprintf("%%vec.newcap.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
			g.indent(), newCapReg, srcCap, startReg))
	}

	// Compute new data pointer: GEP on i8* with byte offset
	// byte offset = start * elem_size (default 8 for i64, 1 for str/str-short)
	elemSize := int64(8)
	if isStr || isStrShort {
		elemSize = 1
	} else if elemType, ok := g.arrayElemTypes[varName]; ok {
		switch elemType {
		case "i8", "i16", "i32", "i64":
			if s := g.llvmTypeSize(elemType); s > 0 {
				elemSize = s
			}
		}
	}
	g.tmpIdx++
	offsetReg := fmt.Sprintf("%%vec.offset.%d", g.tmpIdx)
	if sb != nil {
		if startReg == "0" {
			offsetReg = "0"
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n",
				g.indent(), offsetReg, startReg, elemSize))
		}
	}

	g.tmpIdx++
	newDataReg := fmt.Sprintf("%%vec.newdata.%d", g.tmpIdx)
	if sb != nil {
		if offsetReg == "0" {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8*\n",
				g.indent(), newDataReg, srcData))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
				g.indent(), newDataReg, srcData, offsetReg))
		}
	}

	// Store new len (field 0)
	g.tmpIdx++
	dstLenGEP := fmt.Sprintf("%%slic.dstlen.gep.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
			g.indent(), dstLenGEP, resultType, resultType, resultReg))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n",
			g.indent(), newLenReg, dstLenGEP))
	}

	if resultType == "%vec" {
		// Store new cap (field 1) — only %vec has cap
		g.tmpIdx++
		dstCapGEP := fmt.Sprintf("%%slic.dstcap.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
				g.indent(), dstCapGEP, resultType, resultType, resultReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n",
				g.indent(), newCapReg, dstCapGEP))
		}
	}

	// Store new data
	// %vec: field 2, %str-long: field 1
	dstDataField := uint32(2)
	if isStr || isStrShort {
		dstDataField = 1
	}
	g.tmpIdx++
	dstDataGEP := fmt.Sprintf("%%slic.dstdata.gep.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
			g.indent(), dstDataGEP, resultType, resultType, resultReg, dstDataField))
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n",
			g.indent(), newDataReg, dstDataGEP))
	}

	return resultReg
}

func (g *Generator) rangeBoundStr(expr parser.Expression) string {
	if expr == nil {
		return ""
	}
	return g.generateExpression(expr)
}

func (g *Generator) generateSliceLiteral(sb *strings.Builder, slice *parser.SliceLiteral) string {
	elemType := "i64"
	var sb2 strings.Builder
	sb2.WriteString("[")
	for i, elem := range slice.Elements {
		if i > 0 {
			sb2.WriteString(", ")
		}
		ev := g.generateExprWithSB(sb, elem)
		ev = g.stripLLVMType(ev)
		sb2.WriteString(fmt.Sprintf("%s %s", elemType, ev))
	}
	sb2.WriteString("]")
	// 返回未定型別的陣列值，由呼叫端決定型別
	return sb2.String()
}

func (g *Generator) generateInfix(sb *strings.Builder, expr *parser.InfixExpression) string {
	// 檢查是否為條件語境（for/if 的條件表達式），是則直接輸出 i1
	// 由調用方負責在 generateForStatement / generateIfExpression 中處理

	// Option tag comparison: x == err/nil/ok for %option typed variables
	// Delegate to generateInfixI1 and zext result to i64
	// (needed when comparison appears inside && or ||, not just as if condition)
	if expr.Operator == "==" || expr.Operator == "!=" {
		if leftIdent, ok := expr.Left.(*parser.Identifier); ok {
			var tag int64 = -1
			if rightIdent, ok := expr.Right.(*parser.Identifier); ok {
				if rightIdent.Value == "err" {
					tag = 2
				} else if rightIdent.Value == "nil" {
					tag = 1
				} else if rightIdent.Value == "ok" {
					tag = 0
				}
			} else if _, ok := expr.Right.(*parser.NilLiteral); ok {
				tag = 1
			}
			if tag >= 0 {
				if t, ok := g.varTypes[leftIdent.Value]; ok && t == "%option" {
					i1Result := g.generateInfixI1(sb, expr)
					g.tmpIdx++
					reg := fmt.Sprintf("%%optcmp.zext.%d", g.tmpIdx)
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), reg, i1Result))
					}
					return reg
				}
			}
		}
	}

	left := g.generateExprWithSB(sb, expr.Left)
	right := g.generateExprWithSB(sb, expr.Right)

	// coerceToFloat 將值轉換為目標浮點型別（"float" 或 "double"）。
	// - 浮點字面常量保持原樣（LLVM 會在上下文中自動處理）
	// - 已是目標型別的 SSA 暫存器保持原樣
	// - 其他浮點 SSA 暫存器用 fpext/fptrunc 轉換
	// - 整數 SSA 暫存器用 sitofp 轉換
	// - 整數字面常量附加 ".0"
	coerceToFloat := func(v string, exprForType parser.Expression, targetType string) string {
		if v == "" || targetType == "" {
			return v
		}
		// 浮點字面常量（含 . 或 e/E）→ 保持原樣
		if _, err := fmt.Sscanf(v, "%f", new(float64)); err == nil && strings.ContainsAny(v, ".eE") {
			return v
		}
		// SSA 暫存器（% 開頭）
		if strings.HasPrefix(v, "%") {
			srcType := g.floatLLVMType(exprForType)
			if srcType == targetType {
				return v
			}
			if srcType != "" {
				// float ↔ double 轉換
				if sb != nil {
					g.tmpIdx++
					cvtReg := fmt.Sprintf("%%fpcvt.%d", g.tmpIdx)
					if targetType == "double" && srcType == "float" {
						sb.WriteString(fmt.Sprintf("%s%s = fpext float %s to double\n", g.indent(), cvtReg, v))
					} else if targetType == "float" && srcType == "double" {
						sb.WriteString(fmt.Sprintf("%s%s = fptrunc double %s to float\n", g.indent(), cvtReg, v))
					} else {
						return v
					}
					return cvtReg
				}
				return v
			}
			// 整數 → 浮點
			if sb != nil {
				intType := g.intExprLLVMType(exprForType)
				g.tmpIdx++
				cvtReg := fmt.Sprintf("%%sitofp.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = sitofp %s %s to %s\n", g.indent(), cvtReg, intType, v, targetType))
				return cvtReg
			}
			return v
		}
		// 整數字面常量
		if _, err := fmt.Sscanf(v, "%d", new(int64)); err == nil {
			return v + ".0"
		}
		return v
	}

	// floatArithType 回傳算術/比較運算的目標浮點型別。
	// 當任一運算元為浮點時回傳較寬者；否則回傳 ""。
	floatArithType := func(left, right parser.Expression) string {
		lt := g.floatLLVMType(left)
		rt := g.floatLLVMType(right)
		if lt == "double" || rt == "double" {
			return "double"
		}
		if lt == "float" || rt == "float" {
			return "float"
		}
		return ""
	}

	// 字串比較：使用 strcmp 而非整數比較指令
	if g.isStringExpr(expr.Left) || g.isStringExpr(expr.Right) {
		switch expr.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return g.generateStringCmp(sb, expr)
		}
	}

	switch expr.Operator {
	case "++":
		if sb != nil {
			if ident, ok := expr.Left.(*parser.Identifier); ok {
				g.tmpIdx++
				lReg := fmt.Sprintf("%%inc.ld.%d", g.tmpIdx)
				g.tmpIdx++
				rReg := fmt.Sprintf("%%inc.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), lReg, ident.Value))
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), rReg, lReg))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), rReg, ident.Value))
				return rReg
			}
		}
		return "0"
	case "--":
		if sb != nil {
			if ident, ok := expr.Left.(*parser.Identifier); ok {
				g.tmpIdx++
				lReg := fmt.Sprintf("%%dec.ld.%d", g.tmpIdx)
				g.tmpIdx++
				rReg := fmt.Sprintf("%%dec.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), lReg, ident.Value))
				sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), rReg, lReg))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), rReg, ident.Value))
				return rReg
			}
		}
		return "0"
	case "+":
		// String concatenation: detect if either operand is a string
		if g.isStringExpr(expr.Left) || g.isStringExpr(expr.Right) {
			if sb == nil {
				return "%str-longconcat.null"
			}
			return g.generateStrConcat(sb, expr.Left, expr.Right)
		}
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			reg := fmt.Sprintf("%%fadd.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fadd %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%add.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = add %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "-":
		// String concatenation: detect if either operand is a string
		if g.isStringExpr(expr.Left) || g.isStringExpr(expr.Right) {
			if sb == nil {
				return "%str-longconcat.null"
			}
			return g.generateStrConcat(sb, expr.Left, expr.Right)
		}
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			reg := fmt.Sprintf("%%fsub.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fsub %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%sub.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sub %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "*":
		// String repetition: 'str' * n
		if g.isStringExpr(expr.Left) {
			if sb == nil {
				return "%str-longrepeat.null"
			}
			return g.generateStrRepeat(sb, expr.Left, expr.Right)
		}
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			reg := fmt.Sprintf("%%fmul.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fmul %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%mul.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = mul %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "/":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			reg := fmt.Sprintf("%%fdiv.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fdiv %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%div.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sdiv %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "%":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			reg := fmt.Sprintf("%%fmod.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = frem %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%mod.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = srem %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "==":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%eq.cmp.%d", g.tmpIdx)
			g.tmpIdx++
			extReg := fmt.Sprintf("%%eq.ext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp oeq %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%eq.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%eq.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "!=":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%ne.cmp.%d", g.tmpIdx)
			g.tmpIdx++
			extReg := fmt.Sprintf("%%ne.ext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp one %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%ne.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%ne.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "<":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%lt.cmp.%d", g.tmpIdx)
			g.tmpIdx++
			extReg := fmt.Sprintf("%%lt.ext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp olt %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%lt.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%lt.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case ">":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%gt.cmp.%d", g.tmpIdx)
			g.tmpIdx++
			extReg := fmt.Sprintf("%%gt.ext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp ogt %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%gt.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%gt.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "<=":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%le.cmp.%d", g.tmpIdx)
			g.tmpIdx++
			extReg := fmt.Sprintf("%%le.ext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp ole %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%le.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%le.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sle %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case ">=":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			g.tmpIdx++
			cmpReg := fmt.Sprintf("%%ge.cmp.%d", g.tmpIdx)
			g.tmpIdx++
			extReg := fmt.Sprintf("%%ge.ext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp oge %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%ge.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%ge.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "|":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%or.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = or %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "&":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%and.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = and %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "^":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%xor.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = xor %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "<<":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%shl.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = shl %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case ">>":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		g.tmpIdx++
		reg := fmt.Sprintf("%%shr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = lshr %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "&&":
		// 邏輯 AND：將 i1 運算元 zext 到 i64（如 str.empty() 返回 i1），
		// 比較結果已是 i64，保持不變。然後使用 and 指令。
		leftI64 := g.zextBoolToI64(sb, left, expr.Left)
		rightI64 := g.zextBoolToI64(sb, right, expr.Right)
		g.tmpIdx++
		reg := fmt.Sprintf("%%land.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %s\n", g.indent(), reg, leftI64, rightI64))
		}
		return reg
	case "||":
		// 邏輯 OR：同上，將 i1 運算元 zext 到 i64 後使用 or 指令。
		leftI64 := g.zextBoolToI64(sb, left, expr.Left)
		rightI64 := g.zextBoolToI64(sb, right, expr.Right)
		g.tmpIdx++
		reg := fmt.Sprintf("%%lor.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %s\n", g.indent(), reg, leftI64, rightI64))
		}
		return reg
	default:
		return "0"
	}
}

// isStringExpr checks if an expression is of string type.
func (g *Generator) isStringExpr(expr parser.Expression) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[e.Value]; ok && (t == "%str-long" || t == "%str-short") {
				return true
			}
		}
	case *parser.InfixExpression:
		if e.Operator == "-" || e.Operator == "+" || e.Operator == "*" {
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	}
	return false
}

// getStrPtr returns the %str-long* or %str-short* pointer for a string expression.
func (g *Generator) getStrPtr(sb *strings.Builder, expr parser.Expression) string {
	if ident, ok := expr.(*parser.Identifier); ok {
		return "%" + ident.Value
	}
	// DotExpression 會回傳 %str-long SSA value（loaded from struct field），
	// 但 extractStrLen/extractStrDataPtr 需要的是 %str-long* 指標。
	// 將 value 物化到臨時 alloca，再傳回指標。
	if dot, ok := expr.(*parser.DotExpression); ok {
		val := g.generateExprWithSB(sb, expr)
		if val == "" {
			return val
		}
		if strings.HasPrefix(val, "@") {
			return val
		}
		et := g.exprResultLLVMType(dot)
		if et == "%str-long" {
			g.tmpIdx++
			tmpAlloca := fmt.Sprintf("%%strptr.tmp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, tmpAlloca))
			return tmpAlloca
		}
		return val
	}
	return g.generateExprWithSB(sb, expr)
}

// getStrType returns the LLVM type string for a string expression.
func (g *Generator) getStrType(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		if len(e.Value) <= 127 {
			return "%str-short"
		}
		return "%str-long"
	case *parser.Identifier:
		if t, ok := g.varTypes[e.Value]; ok {
			return t
		}
	case *parser.InfixExpression:
		if e.Operator == "-" || e.Operator == "+" {
			return "%str-long" // concat results are always %str-long
		}
	}
	return "%str-long"
}

// extractLenFromExpr extracts i64 len from a string expression (either %str-long or %str-short).
func (g *Generator) extractLenFromExpr(sb *strings.Builder, expr parser.Expression, ptr string) string {
	stype := g.getStrType(expr)
	if stype == "%str-short" {
		return g.extractStrShortLen(sb, ptr)
	}
	return g.extractStrLen(sb, ptr)
}

// extractDataFromExpr extracts i8* data pointer from a string expression (either %str-long or %str-short).
func (g *Generator) extractDataFromExpr(sb *strings.Builder, expr parser.Expression, ptr string) string {
	stype := g.getStrType(expr)
	if stype == "%str-short" {
		return g.extractStrShortDataPtr(sb, ptr)
	}
	return g.extractStrDataPtr(sb, ptr)
}

// strLenFromExpr generates LLVM IR to extract the string length from a string expression.
// Returns the register name holding the i64 length.
func (g *Generator) strLenFromExpr(sb *strings.Builder, expr parser.Expression) string {
	switch a := expr.(type) {
	case *parser.StringLiteral:
		// Compile-time constant length
		return fmt.Sprintf("%d", len(a.Value))
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[a.Value]; ok {
				if t == "%str-long" {
					return g.extractStrLen(sb, "%"+a.Value)
				}
				if t == "%str-short" {
					return g.extractStrShortLen(sb, "%"+a.Value)
				}
			}
		}
		return "0"
	case *parser.InfixExpression:
		if (a.Operator == "-" || a.Operator == "+" || a.Operator == "*") && (g.isStringExpr(a.Left) || g.isStringExpr(a.Right)) {
			if a.Operator == "*" {
				ptr := g.generateStrRepeat(sb, a.Left, a.Right)
				return g.extractStrLen(sb, ptr)
			}
			ptr := g.generateStrConcat(sb, a.Left, a.Right)
			return g.extractStrLen(sb, ptr)
		}
	case *parser.DotExpression:
		// .field 或 obj.field：generateDotExpression 會載入 struct 值到 SSA register
		// 對於 str 欄位，返回的是 %str-long SSA value。需先 alloca 再 store 以取得指標。
		ptr := g.generateExprWithSB(sb, a)
		et := g.exprResultLLVMType(a)
		if et == "%str-long" {
			g.tmpIdx++
			tmpAlloca := fmt.Sprintf("%%strlen.dot.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
			return g.extractStrLen(sb, tmpAlloca)
		} else if et == "%str-short" {
			return g.extractStrShortLen(sb, ptr)
		}
	}
	return "0"
}

// generateStrConcat generates LLVM IR for string concatenation using `-` operator.
func (g *Generator) generateStrConcat(sb *strings.Builder, leftExpr, rightExpr parser.Expression) string {
	if sb == nil {
		return "%str-longconcat.null"
	}

	leftPtr := g.getStrPtr(sb, leftExpr)
	rightPtr := g.getStrPtr(sb, rightExpr)

	leftLen := g.extractLenFromExpr(sb, leftExpr, leftPtr)
	rightLen := g.extractLenFromExpr(sb, rightExpr, rightPtr)
	leftData := g.extractDataFromExpr(sb, leftExpr, leftPtr)
	rightData := g.extractDataFromExpr(sb, rightExpr, rightPtr)

	g.tmpIdx++
	totalLen := fmt.Sprintf("%%concat.total.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), totalLen, leftLen, rightLen))

	g.tmpIdx++
	allocSize := fmt.Sprintf("%%concat.alloc.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), allocSize, totalLen))

	g.tmpIdx++
	bufPtr := fmt.Sprintf("%%concat.buf.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufPtr, allocSize))

	sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n",
		g.indent(), bufPtr, leftData, leftLen))

	g.tmpIdx++
	dstOffset := fmt.Sprintf("%%concat.dst.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), dstOffset, bufPtr, leftLen))
	sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n",
		g.indent(), dstOffset, rightData, rightLen))

	g.tmpIdx++
	nullPos := fmt.Sprintf("%%concat.null.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nullPos, bufPtr, totalLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))

	g.tmpIdx++
	resultAlloca := fmt.Sprintf("%%concat.result.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), resultAlloca))

	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%concat.len.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%concat.data.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufPtr, dataGEP))

	return resultAlloca
}

// generateStrRepeat generates LLVM IR for string repetition using `*` operator.
// Example: 'Hello' * 3 → 'HelloHelloHello'
func (g *Generator) generateStrRepeat(sb *strings.Builder, strExpr, countExpr parser.Expression) string {
	if sb == nil {
		return "%str-longrepeat.null"
	}

	// Get string pointer and length
	strPtr := g.getStrPtr(sb, strExpr)
	strLen := g.extractLenFromExpr(sb, strExpr, strPtr)
	strData := g.extractDataFromExpr(sb, strExpr, strPtr)

	// Get count (right operand should be an integer)
	countReg := g.generateExprWithSB(sb, countExpr)

	// Calculate total length = strLen * count
	g.tmpIdx++
	totalLen := fmt.Sprintf("%%repeat.total.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %s\n", g.indent(), totalLen, strLen, countReg))

	// Allocate memory for result (totalLen + 1 for null terminator)
	g.tmpIdx++
	allocSize := fmt.Sprintf("%%repeat.alloc.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), allocSize, totalLen))

	g.tmpIdx++
	bufPtr := fmt.Sprintf("%%repeat.buf.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufPtr, allocSize))

	// Loop to copy string data count times
	// We'll use a simple loop: for i in 0..count, memcpy(strData, buf + i*strLen, strLen)
	g.tmpIdx++
	loopStart := fmt.Sprintf("%%repeat.loop.start.%d", g.tmpIdx)
	loopBody := fmt.Sprintf("%%repeat.loop.body.%d", g.tmpIdx)
	loopEnd := fmt.Sprintf("%%repeat.loop.end.%d", g.tmpIdx)

	// Initialize counter i = 0
	g.tmpIdx++
	iReg := fmt.Sprintf("%%repeat.i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), iReg))
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iReg))

	// Jump to loop start
	sb.WriteString(fmt.Sprintf("%sbr label %s\n", g.indent(), loopStart))

	// Loop start: check if i < count
	sb.WriteString(fmt.Sprintf("%s:\n", loopStart))
	g.tmpIdx++
	iVal := fmt.Sprintf("%%repeat.i.val.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iReg))
	g.tmpIdx++
	cmp := fmt.Sprintf("%%repeat.cmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmp, iVal, countReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %s, label %s\n", g.indent(), cmp, loopBody, loopEnd))

	// Loop body: copy string data to buf + i*strLen
	sb.WriteString(fmt.Sprintf("%s:\n", loopBody))
	g.tmpIdx++
	offset := fmt.Sprintf("%%repeat.offset.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %s\n", g.indent(), offset, iVal, strLen))
	g.tmpIdx++
	dstPtr := fmt.Sprintf("%%repeat.dst.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), dstPtr, bufPtr, offset))
	sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n",
		g.indent(), dstPtr, strData, strLen))

	// Increment i
	g.tmpIdx++
	iNext := fmt.Sprintf("%%repeat.i.next.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iReg))
	sb.WriteString(fmt.Sprintf("%sbr label %s\n", g.indent(), loopStart))

	// Loop end
	sb.WriteString(fmt.Sprintf("%s:\n", loopEnd))

	// Add null terminator at buf[totalLen]
	g.tmpIdx++
	nullPos := fmt.Sprintf("%%repeat.null.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nullPos, bufPtr, totalLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))

	// Create result %str-long
	g.tmpIdx++
	resultAlloca := fmt.Sprintf("%%repeat.result.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), resultAlloca))

	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%repeat.len.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%repeat.data.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufPtr, dataGEP))

	return resultAlloca
}
