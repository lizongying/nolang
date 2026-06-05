package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

func (g *Generator) generateExpression(expr parser.Expression) string {
	return g.generateExprWithSB(nil, expr)
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
		g.tmpIdx++
		reg := fmt.Sprintf("%%%s.val.%d", e.Value, g.tmpIdx)
		if sb != nil {
			llvmType := "i64"
			if g.varTypes != nil {
				if t, ok := g.varTypes[e.Value]; ok {
					llvmType = t
				}
			}
			ptrType := llvmType + "*"
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s %%%s\n", g.indent(), reg, llvmType, ptrType, e.Value))
		}
		return reg
	case *parser.StringLiteral:
		idx := g.stringIdx
		g.stringIdx++
		escaped := g.escapeLLVMString(e.Value)
		strLen := len(e.Value)
		g.fmtGlobals = append(g.fmtGlobals,
			fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\\00\"", idx, strLen+1, escaped))
		dataPtr := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0)",
			strLen+1, strLen+1, idx)

		if sb != nil {
			if strLen <= 127 {
				// SSO: use %str-smail (stack-allocated small string)
				g.tmpIdx++
				allocaReg := fmt.Sprintf("%%strlit.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-smail\n", g.indent(), allocaReg))
				// Store len | 0x80 (field 0, i8)
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%strlit.len.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
				sb.WriteString(fmt.Sprintf("%sstore i8 %d, i8* %s\n", g.indent(), strLen|0x80, lenGEP))
				// Copy string data into field 1 ([127 x i8])
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%strlit.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %s, i32 0, i32 1\n", g.indent(), dataGEP, allocaReg))
				// Bitcast [127 x i8]* to i8* for memcpy
				g.tmpIdx++
				dstPtr := fmt.Sprintf("%%strlit.dst.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dstPtr, dataGEP))
				sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, %s, i64 %d)\n", g.indent(), dstPtr, dataPtr, strLen))
				return allocaReg
			} else {
				// Large string: use %str (heap pointer)
				g.tmpIdx++
				allocaReg := fmt.Sprintf("%%strlit.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str\n", g.indent(), allocaReg))
				// Store len (field 0)
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%strlit.len.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
				// Store data (field 1)
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%strlit.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 1\n", g.indent(), dataGEP, allocaReg))
				sb.WriteString(fmt.Sprintf("%sstore %s, i8** %s\n", g.indent(), dataPtr, dataGEP))
				return allocaReg
			}
		}
		if strLen <= 127 {
			return fmt.Sprintf("%%strlit.%d", g.tmpIdx)
		}
		return fmt.Sprintf("%%strlit.%d", g.tmpIdx)
	case *parser.PrefixExpression:
		right := g.generateExprWithSB(sb, e.Right)
		if e.Operator == "-" {
			if strings.HasPrefix(right, "%") {
				g.tmpIdx++
				reg := fmt.Sprintf("%%neg.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = fneg double %s\n", g.indent(), reg, right))
				}
				return reg
			}
			return "-" + right
		}
		return right
	case *parser.CallExpression:
		result := g.generateCallExpression(sb, e)
		if strings.HasPrefix(result, "call ") {
			g.tmpIdx++
			reg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), reg, result))
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
	case *parser.ArrayLiteral:
		return g.generateArrayLiteral(sb, e)
	case *parser.SliceLiteral:
		return g.generateSliceLiteral(sb, e)
	case *parser.SliceExpression:
		return g.generateSliceExpression(sb, e)
	case *parser.PointerType:
		if e.Type != nil {
			return g.generateExprWithSB(sb, e.Type) + "*"
		}
		return "i64*"
	case *parser.IndexExpression:
		return g.generateIndexExpression(sb, e)
	case *parser.InfixExpression:
		return g.generateInfix(sb, e)
	default:
		return "0"
	}
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
			g.tmpIdx++
			reg := fmt.Sprintf("%%if.trunc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), reg, g.generateExprWithSB(sb, expr.Condition)))
			cond = reg
		}
	} else {
		g.tmpIdx++
		reg := fmt.Sprintf("%%if.trunc.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), reg, g.generateExprWithSB(sb, expr.Condition)))
		cond = reg
	}

	// branch
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%if.then.%d, label %%if.else.%d\n",
		g.indent(), cond, labelId, labelId))

	// then
	sb.WriteString(fmt.Sprintf("if.then.%d:\n", labelId))
	g.indentLevel++
	g.tmpIdx++
	thenReg := fmt.Sprintf("%%if.val.%d", g.tmpIdx)
	if expr.Consequence != nil && len(expr.Consequence.Statements) > 0 {
		last := expr.Consequence.Statements[len(expr.Consequence.Statements)-1]
		if es, ok := last.(*parser.ExpressionStatement); ok {
			sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), thenReg, g.generateExpression(es.Expression)))
		} else {
			g.generateStatement(sb, last)
			sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), thenReg))
		}
	} else {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), thenReg))
	}
	sb.WriteString(fmt.Sprintf("%sbr label %%if.end.%d\n", g.indent(), labelId))
	g.indentLevel--

	// else
	sb.WriteString(fmt.Sprintf("if.else.%d:\n", labelId))
	g.indentLevel++
	g.tmpIdx++
	elseReg := fmt.Sprintf("%%if.val.%d", g.tmpIdx)
	if expr.Alternative != nil && len(expr.Alternative.Statements) > 0 {
		last := expr.Alternative.Statements[len(expr.Alternative.Statements)-1]
		if es, ok := last.(*parser.ExpressionStatement); ok {
			sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), elseReg, g.generateExpression(es.Expression)))
		} else {
			g.generateStatement(sb, last)
			sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), elseReg))
		}
	} else {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), elseReg))
	}
	sb.WriteString(fmt.Sprintf("%sbr label %%if.end.%d\n", g.indent(), labelId))
	g.indentLevel--

	// end
	sb.WriteString(fmt.Sprintf("if.end.%d:\n", labelId))
	g.tmpIdx++
	phiReg := fmt.Sprintf("%%if.phi.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = phi i64 [%s, %%if.then.%d], [%s, %%if.else.%d]\n",
		g.indent(), phiReg, thenReg, labelId, elseReg, labelId))
	return phiReg
}

// generateInfixI1 回傳 i1 比較結果（無 zext），用於 for/if 條件
func (g *Generator) generateDotExpression(sb *strings.Builder, expr *parser.DotExpression) string {
	varName := ""
	if ident, ok := expr.Receiver.(*parser.Identifier); ok {
		varName = ident.Value
	}
	fieldName := expr.Property
	g.tmpIdx++
	reg := fmt.Sprintf("%%dot.gep.%d", g.tmpIdx)
	g.tmpIdx++
	loadReg := fmt.Sprintf("%%dot.val.%d", g.tmpIdx)

	structName := ""
	if t, ok := g.varTypes[varName]; ok {
		structName = strings.TrimPrefix(t, "%")
	}

	// Built-in str/str-smail .len access
	if fieldName == "len" && sb != nil {
		if structName == "str" {
			// %str: field 0 is i64 len
			return g.extractStrLen(sb, "%"+varName)
		}
		if structName == "str-smail" {
			// %str-smail: field 0 is i8 len (with high bit tag), mask and zext
			return g.extractStrSmailLen(sb, "%"+varName)
		}
	}

	if fields, ok := g.structTypes[structName]; ok {
		fieldIdx := -1
		for i, f := range fields {
			if f.name == fieldName {
				fieldIdx = i
				break
			}
		}
		if fieldIdx >= 0 && sb != nil {
			structTy := "%" + structName
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
				g.indent(), reg, structTy, structTy, varName, fieldIdx))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), loadReg, reg))
			return loadReg
		}
	}
	return "0"
}

// extractStrDataPtr extracts the i8* data pointer (field 1) from a %str* pointer.
// Returns the register name holding the i8*.
func (g *Generator) extractStrDataPtr(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%str.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataLoad := fmt.Sprintf("%%str.data.val.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 1\n", g.indent(), dataGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), dataLoad, dataGEP))
	}
	return dataLoad
}

// extractStrLen extracts the i64 len (field 0) from a %str* pointer.
func (g *Generator) extractStrLen(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%str.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	lenLoad := fmt.Sprintf("%%str.len.val.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 0\n", g.indent(), lenGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
	}
	return lenLoad
}

// extractStrSmailLen extracts the i64 len from a %str-smail* pointer.
// Loads field 0 (i8), ANDs with 0x7F to clear the SSO tag bit, then zero-extends to i64.
func (g *Generator) extractStrSmailLen(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%strsm.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	lenLoad := fmt.Sprintf("%%strsm.len.raw.%d", g.tmpIdx)
	g.tmpIdx++
	lenMasked := fmt.Sprintf("%%strsm.len.mask.%d", g.tmpIdx)
	g.tmpIdx++
	lenExt := fmt.Sprintf("%%strsm.len.ext.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %s, i32 0, i32 0\n", g.indent(), lenGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), lenLoad, lenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = and i8 %s, 127\n", g.indent(), lenMasked, lenLoad))
		sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), lenExt, lenMasked))
	}
	return lenExt
}

// extractStrSmailDataPtr extracts the i8* data pointer from a %str-smail* pointer.
// Returns a pointer to field 1 (the inline [127 x i8] array), bitcast to i8*.
func (g *Generator) extractStrSmailDataPtr(sb *strings.Builder, strPtr string) string {
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%strsm.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataPtr := fmt.Sprintf("%%strsm.data.ptr.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %s, i32 0, i32 1\n", g.indent(), dataGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dataPtr, dataGEP))
	}
	return dataPtr
}

// extractLenDispatch extracts len from either %str or %str-smail based on known variable type.
func (g *Generator) extractLenDispatch(sb *strings.Builder, varName string) string {
	if t, ok := g.varTypes[varName]; ok {
		if t == "%str-smail" {
			return g.extractStrSmailLen(sb, "%"+varName)
		}
		return g.extractStrLen(sb, "%"+varName)
	}
	return g.extractStrLen(sb, "%"+varName)
}

// extractDataPtrDispatch extracts data ptr from either %str or %str-smail based on known variable type.
func (g *Generator) extractDataPtrDispatch(sb *strings.Builder, varName string) string {
	if t, ok := g.varTypes[varName]; ok {
		if t == "%str-smail" {
			return g.extractStrSmailDataPtr(sb, "%"+varName)
		}
		return g.extractStrDataPtr(sb, "%"+varName)
	}
	return g.extractStrDataPtr(sb, "%"+varName)
}

// resolveStrPtr resolves a value to a %str* pointer.
// If the value is a register starting with %, it's already a %str*.
// Otherwise, it returns the value as-is.
func (g *Generator) resolveStrPtr(val string) string {
	if strings.HasPrefix(val, "%") {
		return val
	}
	return val
}

func (g *Generator) generateAssignExpression(sb *strings.Builder, expr *parser.AssignExpression) string {
	// 欄位賦值: u.name = value → GEP + store
	if dot, ok := expr.Left.(*parser.DotExpression); ok {
		varName := ""
		if ident, ok := dot.Receiver.(*parser.Identifier); ok {
			varName = ident.Value
		}
		structName := ""
		if t, ok := g.varTypes[varName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
		fieldName := dot.Property
		val := g.generateExprWithSB(sb, expr.Value)
		g.tmpIdx++
		reg := fmt.Sprintf("%%set.gep.%d", g.tmpIdx)

		if fields, ok := g.structTypes[structName]; ok {
			fieldIdx := -1
			for i, f := range fields {
				if f.name == fieldName {
					fieldIdx = i
					break
				}
			}
			if fieldIdx >= 0 && sb != nil {
				structTy := "%" + structName
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
					g.indent(), reg, structTy, structTy, structName, fieldIdx))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), val, reg))
			}
		}
		return "0"
	}

	// 索引賦值: arr[i] = val → GEP + store
	if idxExpr, ok := expr.Left.(*parser.IndexExpression); ok {
		varName := ""
		if ident, ok := idxExpr.Left.(*parser.Identifier); ok {
			varName = ident.Value
		}
		idx := g.generateExprWithSB(sb, idxExpr.Index)
		val := g.generateExprWithSB(sb, expr.Value)

		// 取得陣列 LLVM 型別
		var llvmElemType string
		var arrayLLVMType string
		if t, ok := g.varTypes[varName]; ok {
			if strings.HasPrefix(t, "[") {
				closeB := strings.IndexByte(t, ']')
				if closeB > 0 {
					elemType := t[closeB+1:]
					llvmElemType = g.mapToLLVMType(elemType)
					arrayLLVMType = g.mapToLLVMType(t)
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
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
				g.indent(), llvmElemType, val, llvmElemType, gepReg))
		}
		return "0"
	}

	return "0"
}

// generateIndexExpression 處理 arr[i] 讀取
func (g *Generator) generateIndexExpression(sb *strings.Builder, expr *parser.IndexExpression) string {
	// 直接使用 alloca 名稱（而非 loaded value）
	varName := ""
	if ident, ok := expr.Left.(*parser.Identifier); ok {
		varName = ident.Value
	}
	idx := g.generateExprWithSB(sb, expr.Index)

	// String indexing: s[i] → extract data ptr from %str, then GEP into it
	if varName != "" {
		if t, ok := g.varTypes[varName]; ok && t == "%str" {
			dataPtr := g.extractStrDataPtr(sb, "%"+varName)
			g.tmpIdx++
			charGEP := fmt.Sprintf("%%stridx.gep.%d", g.tmpIdx)
			g.tmpIdx++
			charLoad := fmt.Sprintf("%%stridx.val.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
					g.indent(), charGEP, dataPtr, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
			}
			return charLoad
		}
		// str-smail indexing: GEP into field 1 ([127 x i8]) directly
		if t, ok := g.varTypes[varName]; ok && t == "%str-smail" {
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%strsm.idx.gep.%d", g.tmpIdx)
			g.tmpIdx++
			charGEP := fmt.Sprintf("%%strsm.idx.elem.%d", g.tmpIdx)
			g.tmpIdx++
			charLoad := fmt.Sprintf("%%strsm.idx.val.%d", g.tmpIdx)
			if sb != nil {
				// GEP to field 1 (the [127 x i8] array)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %%%s, i32 0, i32 1\n",
					g.indent(), dataGEP, varName))
				// GEP into the array at index
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [127 x i8], [127 x i8]* %s, i64 0, i64 %s\n",
					g.indent(), charGEP, dataGEP, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
			}
			return charLoad
		}
	}

	// 取得變數的 LLVM 型別
	var llvmElemType string
	var arrayLLVMType string
	if t, ok := g.varTypes[varName]; ok {
		// t = "[8]byte"
		if strings.HasPrefix(t, "[") {
			closeB := strings.IndexByte(t, ']')
			if closeB > 0 {
				elemType := t[closeB+1:]
				llvmElemType = g.mapToLLVMType(elemType)
				arrayLLVMType = g.mapToLLVMType(t)
			}
		}
	}
	if llvmElemType == "" {
		llvmElemType = "i8"
		arrayLLVMType = "[8 x i8]"
	}

	// GEP 取得元素指標：使用 %varName (alloca) 而非 loaded value
	g.tmpIdx++
	gepReg := fmt.Sprintf("%%idx.gep.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i64 0, i64 %s\n",
			g.indent(), gepReg, arrayLLVMType, arrayLLVMType, varName, idx))
	}

	// Load 元素值
	g.tmpIdx++
	loadReg := fmt.Sprintf("%%idx.load.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
			g.indent(), loadReg, llvmElemType, llvmElemType, gepReg))
	}
	return loadReg
}

func (g *Generator) generateStructLiteral(sb *strings.Builder, expr *parser.StructLiteral) string {
	// struct literal: user { name: 'abc', age: 20 }
	// 在 generateLet 中處理（varLLVMType 已回傳 struct type）
	// 這裡只產生一個 placeholder
	return "{ }"
}

func (g *Generator) generateInfixI1(sb *strings.Builder, expr *parser.InfixExpression) string {
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
		cmpOp = "slt"
	case ">":
		cmpOp = "sgt"
	case "<=":
		cmpOp = "sle"
	case ">=":
		cmpOp = "sge"
	default:
		return g.generateInfix(sb, expr) // fallback
	}
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), reg, cmpOp, left, right))
	}
	return reg
}

func (g *Generator) generateArrayLiteral(sb *strings.Builder, arr *parser.ArrayLiteral) string {
	elemType := "i64"
	var sb2 strings.Builder
	sb2.WriteString("[")
	for i, elem := range arr.Elements {
		if i > 0 {
			sb2.WriteString(", ")
		}
		ev := g.generateExprWithSB(sb, elem)
		ev = g.stripLLVMType(ev)
		sb2.WriteString(fmt.Sprintf("%s %s", elemType, ev))
	}
	sb2.WriteString("]")
	return fmt.Sprintf("[%d x %s] %s", len(arr.Elements), elemType, sb2.String())
}

func (g *Generator) generateSliceExpression(sb *strings.Builder, expr *parser.SliceExpression) string {
	// TODO: implement LLVM slice expression
	// For now, generate a comment and return a placeholder
	r := expr.Range
	leftVal := g.generateExprWithSB(sb, expr.Left)
	sb.WriteString(fmt.Sprintf("%s; slice expression: %s[%s..%s]\n", g.indent(), leftVal,
		g.rangeBoundStr(r.Start), g.rangeBoundStr(r.End)))
	return "0"
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

	left := g.generateExprWithSB(sb, expr.Left)
	right := g.generateExprWithSB(sb, expr.Right)

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
		g.tmpIdx++
		reg := fmt.Sprintf("%%add.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "-":
		// String concatenation: detect if either operand is a string
		if g.isStringExpr(expr.Left) || g.isStringExpr(expr.Right) {
			if sb == nil {
				return "%strconcat.null"
			}
			return g.generateStrConcat(sb, expr.Left, expr.Right)
		}
		g.tmpIdx++
		reg := fmt.Sprintf("%%sub.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "*":
		g.tmpIdx++
		reg := fmt.Sprintf("%%mul.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "/":
		g.tmpIdx++
		reg := fmt.Sprintf("%%div.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sdiv i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "%":
		g.tmpIdx++
		reg := fmt.Sprintf("%%mod.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = srem i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "==":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%eq.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%eq.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, %s\n", g.indent(), cmpReg, left, right))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "!=":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%ne.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%ne.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i64 %s, %s\n", g.indent(), cmpReg, left, right))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "<":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%lt.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%lt.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, left, right))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case ">":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%gt.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%gt.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, left, right))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "<=":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%le.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%le.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sle i64 %s, %s\n", g.indent(), cmpReg, left, right))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case ">=":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%ge.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%ge.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i64 %s, %s\n", g.indent(), cmpReg, left, right))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "|":
		g.tmpIdx++
		reg := fmt.Sprintf("%%or.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "&":
		g.tmpIdx++
		reg := fmt.Sprintf("%%and.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "^":
		g.tmpIdx++
		reg := fmt.Sprintf("%%xor.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = xor i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case "<<":
		g.tmpIdx++
		reg := fmt.Sprintf("%%shl.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = shl i64 %s, %s\n", g.indent(), reg, left, right))
		}
		return reg
	case ">>":
		g.tmpIdx++
		reg := fmt.Sprintf("%%shr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = lshr i64 %s, %s\n", g.indent(), reg, left, right))
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
			if t, ok := g.varTypes[e.Value]; ok && (t == "%str" || t == "%str-smail") {
				return true
			}
		}
	case *parser.InfixExpression:
		if e.Operator == "-" {
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	}
	return false
}

// getStrPtr returns the %str* or %str-smail* pointer for a string expression.
func (g *Generator) getStrPtr(sb *strings.Builder, expr parser.Expression) string {
	if ident, ok := expr.(*parser.Identifier); ok {
		return "%" + ident.Value
	}
	return g.generateExprWithSB(sb, expr)
}

// getStrType returns the LLVM type string for a string expression.
func (g *Generator) getStrType(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		if len(e.Value) <= 127 {
			return "%str-smail"
		}
		return "%str"
	case *parser.Identifier:
		if t, ok := g.varTypes[e.Value]; ok {
			return t
		}
	case *parser.InfixExpression:
		if e.Operator == "-" {
			return "%str" // concat results are always %str
		}
	}
	return "%str"
}

// extractLenFromExpr extracts i64 len from a string expression (either %str or %str-smail).
func (g *Generator) extractLenFromExpr(sb *strings.Builder, expr parser.Expression, ptr string) string {
	stype := g.getStrType(expr)
	if stype == "%str-smail" {
		return g.extractStrSmailLen(sb, ptr)
	}
	return g.extractStrLen(sb, ptr)
}

// extractDataFromExpr extracts i8* data pointer from a string expression (either %str or %str-smail).
func (g *Generator) extractDataFromExpr(sb *strings.Builder, expr parser.Expression, ptr string) string {
	stype := g.getStrType(expr)
	if stype == "%str-smail" {
		return g.extractStrSmailDataPtr(sb, ptr)
	}
	return g.extractStrDataPtr(sb, ptr)
}

// generateStrConcat generates LLVM IR for string concatenation using `-` operator.
func (g *Generator) generateStrConcat(sb *strings.Builder, leftExpr, rightExpr parser.Expression) string {
	if sb == nil {
		return "%strconcat.null"
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
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str\n", g.indent(), resultAlloca))

	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%concat.len.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%concat.data.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 1\n", g.indent(), dataGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufPtr, dataGEP))

	return resultAlloca
}
