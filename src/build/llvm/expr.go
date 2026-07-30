package llvm

import (
	"fmt"
	"os"
	"strconv"
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
	// 字串字面量暫存器：%str-longlit.* (alloca %str-long，指向 str-long struct)
	if strings.Contains(val, "str-longlit") {
		return "%str-long"
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
		s := strconv.FormatFloat(e.Value, 'g', -1, 64)
		// Ensure LLVM treats it as double: must contain '.', 'e', or 'E'
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s
	case *parser.ByteLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.CharLiteral:
		// Char literal: emit Unicode codepoint as i32
		runes := []rune(e.Value)
		if len(runes) == 1 {
			return fmt.Sprintf("%d", runes[0])
		}
		return "0"
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
		// But reassigned variables (e.g. h0 in SHA tests) must generate a load,
		// not use a stale constant from enumVariantIndex.
		if !isLocalVar && g.enumVariantIndex != nil {
			if g.reassignedVars == nil || !g.reassignedVars[e.Value] {
				if tagIdx, ok := g.enumVariantIndex[e.Value]; ok {
					return fmt.Sprintf("%d", tagIdx)
				}
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
				// For struct types: load i64 from data field, inttoptr to struct pointer.
				// Returns a pointer (consistent with how struct variables are referenced
				// by pointer throughout the codegen). Callers that need a value should
				// load from this pointer.
				if g.isStructLLVMType(innerType) {
					g.tmpIdx++
					dataLoad := llvmSSAReg(e.Value, fmt.Sprintf(".data.val.%d", g.tmpIdx))
					g.tmpIdx++
					dataPtr := llvmSSAReg(e.Value, fmt.Sprintf(".data.ptr.%d", g.tmpIdx))
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, llvmVarRef(e.Value)))
						sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataLoad, dataGEP))
						sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to %s*\n", g.indent(), dataPtr, dataLoad, innerType))
					}
					if g.ssaTypes != nil {
						g.ssaTypes[dataPtr] = innerType + "*"
					}
					return dataPtr
				}
				// For primitive types (i64, double, etc.), load the value directly
				g.tmpIdx++
				dataLoad := llvmSSAReg(e.Value, fmt.Sprintf(".data.val.%d", g.tmpIdx))
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), dataGEP, llvmVarRef(e.Value)))
					sb.WriteString(fmt.Sprintf("%s%s = load %s, i64* %s\n", g.indent(), dataLoad, innerType, dataGEP))
				}
				return dataLoad
			}
		}
		// Slice view variable used as expression value:
		// materialize to temporary struct (shared data), load and return value.
		// This handles cases like `out = view` or `result = view` where the
		// slice view needs to be converted to a concrete struct value.
		if g.isSliceViewVar(e.Value) {
			view := g.sliceViews[e.Value]
			llvmType := "%vec"
			if view.isStr {
				llvmType = "%str-long"
			}
			g.tmpIdx++
			reg := llvmSSAReg(e.Value, fmt.Sprintf(".svmat.val.%d", g.tmpIdx))
			if sb != nil {
				matPtr := g.materializeSliceView(sb, e.Value)
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
					g.indent(), reg, llvmType, llvmType, matPtr))
				if g.ssaTypes != nil {
					g.ssaTypes[reg] = llvmType
				}
			}
			return reg
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
			// For synthetic `it` shared across matches with different types,
			// the alloca may use a different (larger) type. Bitcast the pointer
			// before loading to match the current varTypes type.
			if g.itAllocTypes != nil {
				if allocType, ok := g.itAllocTypes[e.Value]; ok && allocType != llvmType {
					castReg := g.tmpReg("it.rcast")
					sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to %s*\n", g.indent(), castReg, allocType, varAddr, llvmType))
					varAddr = castReg
				}
			}
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s %s\n", g.indent(), reg, llvmType, ptrType, varAddr))
			// Record SSA type for phi type inference (e.g. bool/i1 variables
			// used as if-expression branch values).
			if g.ssaTypes != nil {
				g.ssaTypes[reg] = llvmType
			}
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
			// All strings use %str-long (heap pointer)
			allocaReg := g.tmpReg("str-longlit")
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), allocaReg))
			// Store len (field 0)
			lenGEP := g.tmpReg("str-longlit.len.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
			// Store cap (field 1) = strLen
			capGEP := g.tmpReg("str-longlit.cap.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, allocaReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, capGEP))
			// Store data (field 2) — data is i64 (address value)
			dataGEP := g.tmpReg("str-longlit.data.gep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, allocaReg))
			// Copy string data to writable heap memory so that index assignment
			// (e.g. input[2] = 99) does not write to read-only global constant.
			mallocSize := strLen
			if mallocSize == 0 {
				mallocSize = 1 // avoid implementation-defined malloc(0)
			}
			bufReg := g.tmpReg("str-longlit.buf")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), bufReg, mallocSize))
			if strLen > 0 {
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, %s, i64 %d, i1 false)\n",
					g.indent(), bufReg, dataPtr, strLen))
			}
			dataIntReg := g.tmpReg("str-longlit.data2int")
			sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), dataIntReg, bufReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), dataIntReg, dataGEP))
			return allocaReg
		}
		return fmt.Sprintf("%%str-longlit.%d", g.tmpIdx)
	case *parser.PrefixExpression:
		right := g.generateExprWithSB(sb, e.Right)
		if e.Operator == "-" {
			if strings.HasPrefix(right, "%") {
				reg := g.tmpReg("neg.tmp")
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
		if e.Operator == "!" {
			// Logical NOT: Nolang bools are i64 (0=false, non-zero=true)
			// !x  =>  icmp eq i64 x, 0  =>  zext i1 result to i64
			rc := right
			if !strings.HasPrefix(right, "%") {
				// Literal or constant — handle directly
				if right == "0" {
					return "1"
				}
				return "0"
			}
			// Ensure operand is i64
			operandType := g.intExprLLVMType(e.Right)
			if operandType == "" {
				operandType = "i64"
			}
			if operandType == "i1" {
				zextReg := g.tmpReg("not.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, right))
				}
				rc = zextReg
			} else if operandType != "i64" {
				extReg := g.tmpReg("not.ext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = sext %s %s to i64\n", g.indent(), extReg, operandType, right))
				}
				rc = extReg
			}
			cmpReg := g.tmpReg("not.cmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), cmpReg, rc))
			}
			reg := g.tmpReg("not.result")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), reg, cmpReg))
			}
			return reg
		}
		if e.Operator == "~" {
			// Bitwise NOT: ~x  =>  xor type, -1, x
			// -1 is all-ones in two's complement, so XOR flips all bits.
			notType := g.intExprLLVMType(e.Right)
			if notType == "" {
				// Fallback: derive from expression result type
				notType = g.exprResultLLVMType(e.Right)
				if notType == "" || notType == "i64" {
					notType = "i64"
				}
			}
			rc := g.coerceToInt(sb, right, e.Right, notType)
			if strings.HasPrefix(rc, "%") {
				reg := g.tmpReg("bnot.tmp")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = xor %s %s, -1\n", g.indent(), reg, notType, rc))
				}
				return reg
			}
			// Literal/constant operand — compute at compile time
			if v, err := strconv.ParseInt(rc, 10, 64); err == nil {
				return fmt.Sprintf("%d", ^v)
			}
			reg := g.tmpReg("bnot.tmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = xor %s %s, -1\n", g.indent(), reg, notType, rc))
			}
			return reg
		}
		return right
	case *parser.CallExpression:
		result := g.generateCallExpression(sb, e)
		if strings.HasPrefix(result, "call ") {
			if strings.HasPrefix(result, "call void") {
				if sb != nil {
					sb.WriteString(g.indent() + result + "\n")
				}
			// 检查是否有输出参数但未正确处理（防御性诊断）。
			// 仅當函數「源碼顯式聲明了回傳值」(funcDeclaredResults > 0) 卻生成了 void 呼叫時，
			// 才表示回傳值被遺失（真實 bug）。void 函數（如 fe-copy/fe-neg）即便被啟發式
			// 標記為單輸出，其 void 呼叫也是正確的，不應報錯。
			fnName := flattenDottedExpr(e.Function)
			if fnName == "" {
				if dot, ok := e.Function.(*parser.DotExpression); ok {
					fnName = dot.Property
				}
			}
			if fnName != "" && g.funcDeclaredResults != nil {
				if n, ok := g.funcDeclaredResults[fnName]; ok && n > 0 {
					if m, ok2 := g.funcNumResults[fnName]; ok2 && m > 0 {
						fmt.Fprintf(os.Stderr, "codegen error: function %q has %d output params but generateCallExpression returned void call (not handled as expression)\n", fnName, n)
					}
				}
			}
				return ""
			}
			reg := g.tmpReg("call.tmp")
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
				zextReg := g.tmpReg("call.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), zextReg, reg))
				}
				return zextReg
			}
			// If call returns i1 (bool), zext to i64 (Nolang bools are i64)
			if strings.Contains(result, "call i1 ") {
				zextReg := g.tmpReg("call.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, reg))
				}
				return zextReg
			}
			return reg
		}
		// result is a register name (from voidSingleOutput or hasOutputParam path)
		// If it's i1 (bool), zext to i64 (Nolang bools are i64)
		if strings.HasPrefix(result, "%") && g.ssaTypes != nil {
			if t, ok := g.ssaTypes[result]; ok && t == "i1" {
				zextReg := g.tmpReg("call.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, result))
				}
				return zextReg
			}
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
	case *parser.CastExpression:
		return g.generateCastExpression(sb, e)
	case *parser.RunExpression:
		return g.generateRunExpression(sb, e)
	case *parser.AwaitExpression:
		return g.generateAwaitExpression(sb, e)
	case *parser.RegexLiteral:
		// Desugar /pattern/flags into regexp-compile('pattern') call.
		// The regexp-compile function (defined in std/regexp.no) creates a
		// regexp struct, sets the pattern, and calls .compile() in one step.
		// Flags are currently stored on the node but not yet used by the
		// regexp engine; they are preserved for future flag support.
		callExpr := &parser.CallExpression{
			Token: e.Token,
			Function: &parser.Identifier{
				Token: e.Token,
				Value: "regexp-compile",
			},
			Arguments: []parser.Expression{
				&parser.StringLiteral{
					Token: e.Token,
					Value: e.Pattern,
				},
			},
		}
		return g.generateExprWithSB(sb, callExpr)
	default:
		return "0"
	}
}

// generateCastExpression handles `expr as Type` type casts.
//
// For integer-to-integer casts (e.g. i32 as u64, i64 as u32), emit the
// appropriate trunc/zext instruction so the LLVM type matches the target.
// Non-integer casts (struct/str/etc.) are currently not supported and
// return the underlying expression unchanged.
func (g *Generator) generateCastExpression(sb *strings.Builder, e *parser.CastExpression) string {
	val := g.generateExprWithSB(sb, e.Expr)
	if e.Type == nil || sb == nil {
		return val
	}
	srcType := g.intExprLLVMType(e.Expr)
	// generateIndexExpression always zexts narrow integer elements to i64,
	// so for IndexExpression the srcType is already i64 (intExprLLVMType default).
	if srcType == "" {
		srcType = "i64"
	}
	tgtType := g.mapToLLVMType(e.Type.String())
	if !g.isIntegerLLVMType(srcType) || !g.isIntegerLLVMType(tgtType) {
		return val
	}
	if srcType == tgtType {
		return val
	}
	if !strings.HasPrefix(val, "%") {
		return val
	}
	castReg := g.tmpReg("cast")
	if srcType == "i64" {
		// i64 → smaller: trunc
		sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), castReg, srcType, val, tgtType))
	} else if tgtType == "i64" {
		// smaller → i64: zext
		sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), castReg, srcType, val))
	} else {
		// smaller → smaller (both non-i64): trunc to target
		sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), castReg, srcType, val, tgtType))
	}
	return castReg
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
	reg := g.tmpReg("if.trunc")
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
	// Save SSA versions before entering branch: branch內的賦值遞增版本，
	// 分支結束後恢復，確保隱式返回不會命中分支內的延遲綁定。
	savedSSA := make(map[string]int)
	if g.ssaVersion != nil {
		for k, v := range g.ssaVersion {
			savedSSA[k] = v
		}
	}
	// Save outBindState before entering branch: branch 內的 move-to-out 不應污染其他分支。
	// 分支匯合時取並集：兩分支綁定相同 → 保持；不同 → -2（不確定）。
	savedOutBindState := make([]int, len(g.outBindState))
	copy(savedOutBindState, g.outBindState)
	// 預設 phi 值：對 struct 用 zeroinitializer，對 pointer 用 null，對 float/double 用 0.0
	defaultZero := "0"
	if g.isStructLLVMType(g.curFuncRetType) {
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
		thenLoad := g.tmpReg("if.then.load")
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
	// Load struct value from alloca pointer before br (must be before terminator).
	// String literals (%str-longlit.*) are alloca %str-long (pointers);
	// the PHI needs the %str-long struct value, so load it.
	if !g.blockTerminated && strings.Contains(thenVal, "str-longlit") {
		loadReg := g.tmpReg("if.then.strload")
		sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, thenVal))
		thenVal = loadReg
		if g.ssaTypes != nil {
			g.ssaTypes[loadReg] = "%str-long"
		}
	}
	thenTerminated := g.blockTerminated
	thenPredecessor := g.currentBlock
	if !thenTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%if.end.%d\n", g.indent(), labelId))
	}
	g.indentLevel--

	// 捕獲 then 分支的 outBindState 結果，供分支匯合時取並集
	thenOutBindState := make([]int, len(g.outBindState))
	copy(thenOutBindState, g.outBindState)

	// else
	elseLabel := fmt.Sprintf("if.else.%d", labelId)
	g.emitLabel(sb, elseLabel)
	g.indentLevel++
	// Restore SSA versions: else branch starts from pre-then state.
	if g.ssaVersion != nil {
		for k, v := range savedSSA {
			g.ssaVersion[k] = v
		}
	}
	// Restore outBindState: else 分支從進入 if 前的狀態開始，不受 then 分支 move 污染
	copy(g.outBindState, savedOutBindState)
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
		elseLoad := g.tmpReg("if.else.load")
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
	// Load struct value from alloca pointer before br (must be before terminator).
	if !g.blockTerminated && strings.Contains(elseVal, "str-longlit") {
		loadReg := g.tmpReg("if.else.strload")
		sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, elseVal))
		elseVal = loadReg
		if g.ssaTypes != nil {
			g.ssaTypes[loadReg] = "%str-long"
		}
	}
	elseTerminated := g.blockTerminated
	elsePredecessor := g.currentBlock
	if !elseTerminated {
		sb.WriteString(fmt.Sprintf("%sbr label %%if.end.%d\n", g.indent(), labelId))
	}
	g.indentLevel--

	// 分支匯合：outBindState 取並集。
	// - 兩分支綁定相同 → 保持（確定性綁定）
	// - 兩分支綁定不同 → -2（不確定，後續覆蓋不清舊 bit）
	// - 一分支綁定、另一分支無 → -2（不確定）
	// 這使得編譯期狀態能識別所有潛在 move 代碼路徑，配合運行時 bitmap 雙重校驗。
	for i := range g.outBindState {
		thenV := thenOutBindState[i]
		elseV := g.outBindState[i]
		if thenV != elseV {
			g.outBindState[i] = -2 // 不確定
		}
	}

	// end
	// Restore SSA versions after else branch: subsequent code uses pre-branch state.
	if g.ssaVersion != nil {
		for k, v := range savedSSA {
			g.ssaVersion[k] = v
		}
	}
	endLabel := fmt.Sprintf("if.end.%d", labelId)
	g.emitLabel(sb, endLabel)
	phiReg := g.tmpReg("if.phi")
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
	if g.isStructLLVMType(phiType) {
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
	// Coerce branch values to phiType to avoid type mismatches in phi nodes.
	// This handles cases where one branch returns i1 (bool) and the other i64.
	if !thenTerminated && strings.HasPrefix(thenVal, "%") {
		thenVal = g.coercePhiValue(sb, thenVal, phiType)
	}
	if !elseTerminated && strings.HasPrefix(elseVal, "%") {
		elseVal = g.coercePhiValue(sb, elseVal, phiType)
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

// coercePhiValue converts an SSA value to the target phi type if needed.
// Handles i1→i64 (zext), i8/i16/i32→i64 (zext), and other size mismatches.
func (g *Generator) coercePhiValue(sb *strings.Builder, val, targetType string) string {
	if val == "" || !strings.HasPrefix(val, "%") {
		return val
	}
	// Determine the actual type of the value
	valType := ""
	if g.ssaTypes != nil {
		if t, ok := g.ssaTypes[val]; ok && t != "" {
			valType = t
		}
	}
	if valType == "" {
		valType = g.inferSSAType(val)
	}
	if valType == "" || valType == targetType {
		return val
	}
	// Coerce small integer types to i64
	if targetType == "i64" && g.isIntegerLLVMType(valType) && valType != "i64" {
		zextReg := g.tmpReg("phi.zext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n",
				g.indent(), zextReg, valType, val))
		}
		return zextReg
	}
	// Coerce i64 to i1 (trunc) — less common but possible
	if targetType == "i1" && valType == "i64" {
		truncReg := g.tmpReg("phi.trunc")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i1\n",
				g.indent(), truncReg, valType, val))
		}
		return truncReg
	}
	return val
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
			reg := g.tmpReg("cond.trunc")
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), reg, g.generateExprWithSB(sb, expr.Condition)))
			cond = reg
		}
	} else {
		reg := g.tmpReg("cond.trunc")
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
	phiReg := g.tmpReg("cond.phi")
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
	case *parser.IndexExpression:
		// 陣列/切片元素：依 arrayElemTypes 推導浮點型別
		// （如 IUBP[k] 其中 IUBP 為 [15]f64 → double）
		if ident, ok := v.Left.(*parser.Identifier); ok {
			// 必須先檢查變數實際型別是否為 str-long（字串），避免被
			// moduleArrayElemTypes 中同名變數（如 spectral-norm 的 u []f64）污染。
			// 例如 http2-do 中 u = url 使 u 成為 str-long 區域變數，但
			// moduleArrayElemTypes["u"] 可能是 "double"（來自 spectral-norm main），
			// 導致 u[i] 被誤判為 double 比較。字串索引結果為 i64（zext i8），
			// 不是浮點數，應返回 ""。
			if g.varTypes != nil {
				if actualType, ok := g.varTypes[ident.Value]; ok && actualType == "%str-long" {
					return ""
				}
			}
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[ident.Value]; ok {
					switch et {
					case "float":
						return "float"
					case "double":
						return "double"
					}
				}
			}
		}
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
			if g.isStructLLVMType(recvType) {
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
		// method call（如 r2.sqrt()）：用方法名查 builtin 推導返回型別
		if dot, ok := v.Function.(*parser.DotExpression); ok {
			m := builtin.FindBuiltinMethod(dot.Property)
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
// llvmIntBitWidth returns the bit width of an LLVM integer type string.
// Returns 64 for unknown/non-integer types (defaulting to i64 behavior).
func llvmIntBitWidth(t string) int {
	switch t {
	case "i1":
		return 1
	case "i8":
		return 8
	case "i16":
		return 16
	case "i32":
		return 32
	case "i64":
		return 64
	default:
		return 64
	}
}

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
		if g.isStructLLVMType(recvType) {
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
		// 與 arithLLVMType 相同的策略：混合字面量與變數時偏好變數型別，
		// 兩者皆為變數時使用較寬型別（自動寬化），目標型別傳播
		_, leftIsLit := v.Left.(*parser.IntegerLiteral)
		_, rightIsLit := v.Right.(*parser.IntegerLiteral)
		lt := g.intExprLLVMType(v.Left)
		rt := g.intExprLLVMType(v.Right)
		if leftIsLit && !rightIsLit && rt != "i64" {
			return rt
		}
		if rightIsLit && !leftIsLit && lt != "i64" {
			return lt
		}
		resultType := widerIntType(lt, rt)
		if g.currentTargetType == "i64" && g.isIntegerLLVMType(resultType) && resultType != "i64" {
			return "i64"
		}
		return resultType
	case *parser.PrefixExpression:
		// !x always returns i64 (Nolang bools are i64)
		if v.Operator == "!" {
			return "i64"
		}
		return g.intExprLLVMType(v.Right)
	case *parser.GroupedExpression:
		return g.intExprLLVMType(v.Expression)
	case *parser.CastExpression:
		// `expr as Type`: the result type is the target type
		if v.Type != nil {
			t := g.mapToLLVMType(v.Type.String())
			if g.isIntegerLLVMType(t) {
				return t
			}
		}
		return g.intExprLLVMType(v.Expr)
	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			if g.funcRetTypes != nil {
				if t, ok := g.funcRetTypes[ident.Value]; ok {
					if t != "void" {
						return t
					}
					// void + 單輸出函數：使用 funcResultLLVMType 中的輸出型別
					// Nolang bools are stored as i64 (CallExpression handler zexts i1→i64)
					if g.funcResultLLVMType != nil {
						if ts, ok := g.funcResultLLVMType[ident.Value]; ok && len(ts) == 1 {
							retType := ts[0]
							if retType == "i1" {
								retType = "i64"
							}
							return retType
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
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
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
								// Nolang bools are stored as i64 (CallExpression handler zexts i1→i64)
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
// 當兩個運算元都是非字面量變數時，使用較寬的型別（自動寬化）。
// 目標型別傳播：若賦值目標是 i64（如 u64 = u32 | u32），
// 自動使用 i64 作為運算型別，避免窄型別溢出。
func (g *Generator) arithLLVMType(left, right parser.Expression) string {
	_, leftIsLit := left.(*parser.IntegerLiteral)
	_, rightIsLit := right.(*parser.IntegerLiteral)
	leftType := g.intExprLLVMType(left)
	rightType := g.intExprLLVMType(right)
	// 混合字面量與變數：優先使用變數的型別
	if leftIsLit && !rightIsLit && rightType != "i64" {
		return rightType
	}
	if rightIsLit && !leftIsLit && leftType != "i64" {
		return leftType
	}
	// 其他情況：使用較寬型別（自動寬化）
	resultType := widerIntType(leftType, rightType)
	// 目標型別傳播：若賦值目標是 i64 且運算元都是窄整數，使用 i64
	if g.currentTargetType == "i64" && g.isIntegerLLVMType(resultType) && resultType != "i64" {
		return "i64"
	}
	return resultType
}

// coerceToInt 將 SSA 值轉換為目標整數型別。
// 當值是較窄的整數型別時，進行 zext 擴展；當值是 i64 而目標較窄時，進行 trunc。
// 當值是整數字面常量時保持原樣（LLVM 會自動處理）。
func (g *Generator) coerceToInt(sb *strings.Builder, v string, exprForType parser.Expression, targetType string) string {
	if v == "" {
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
		cvtReg := g.tmpReg("cvt")
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
			extReg := g.tmpReg("lzext")
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
		if g.isStructLLVMType(recvType) {
			structName := strings.TrimPrefix(recvType, "%")
			if fields, ok := g.structTypes[structName]; ok {
				for _, f := range fields {
					if f.name == v.Property {
						// Inline array field (e.g. [32 x i8]) should be treated as
						// %arr for slicing/codegen purposes, so that
						// generateSliceExpression and generateCallArg can handle
						// struct field slices like .client-mac-key[..]
						if strings.HasPrefix(f.typ, "[") {
							return "%arr"
						}
						return f.typ
					}
				}
			}
		}
		// str-long .len → i64
		if v.Property == "len" && (recvType == "%str-long") {
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
		// struct.field[i] — when the field is an inline array (e.g. .domains[i]
		// where domains is [64 x %str-long]), exprResultLLVMType(v.Left) returns
		// "%arr" (per the DotExpression case above), which hides the element type.
		// Look up the raw array type from the struct definition and extract it.
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
							if f.name == dot.Property && strings.HasPrefix(f.typ, "[") {
								closeB := strings.IndexByte(f.typ, ']')
								if closeB > 0 {
									inner := f.typ[1:closeB]
									xIdx := strings.LastIndex(inner, " x ")
									if xIdx >= 0 {
										return inner[xIdx+3:]
									}
								}
							}
						}
					}
				}
			}
		}
		// %vec / %arr: element type tracked separately
		if ident, ok := v.Left.(*parser.Identifier); ok {
			// 必須先檢查變數實際型別是否為 str-long（字串），避免被
			// moduleArrayElemTypes 中同名變數（如 spectral-norm 的 u []f64）污染。
			// 例如 http2-do 中 u = url 使 u 成為 str-long 區域變數，但
			// moduleArrayElemTypes["u"] 可能是 "double"（來自 spectral-norm main），
			// 導致 u[i] 被誤判為 double。字串索引結果為 i64（zext i8）。
			if g.varTypes != nil {
				if actualType, ok := g.varTypes[ident.Value]; ok && actualType == "%str-long" {
					return "i64"
				}
			}
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[ident.Value]; ok {
					return et
				}
			}
		}
		// 切片結果索引：arr[1..3][0] — 切片結果為 %vec/%str-long，
		// 元素型別與原陣列相同。
		if sliceExpr, ok := v.Left.(*parser.SliceExpression); ok {
			recvType := g.exprResultLLVMType(sliceExpr.Left)
			if recvType == "%str-long" {
				return "i64"
			}
			if recvType == "%vec" || recvType == "%arr" {
				if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
					if g.arrayElemTypes != nil {
						if et, ok := g.arrayElemTypes[ident.Value]; ok {
							return et
						}
					}
				}
				return "i64"
			}
		}
	case *parser.SliceExpression:
		// 切片表達式的結果型別：
		// - str-long 切片 → %str-long
		// - vec/arr 切片 → %vec
		recvType := g.exprResultLLVMType(v.Left)
		if recvType == "%str-long" {
			return "%str-long"
		}
		if recvType == "%vec" || recvType == "%arr" {
			return "%vec"
		}
	case *parser.CallExpression:
		// -async 函数调用返回 %future（惰性，未执行）
		if g.isAsyncCall(v) {
			return "%future"
		}
		// Look up function return type. This works regardless of ssaTypes
		// because funcRetTypes/funcResultLLVMType are populated during
		// function declaration processing (before code generation).
		if ident, ok := v.Function.(*parser.Identifier); ok {
			fnName := ident.Value
			if g.funcRetTypes != nil {
				if t, ok := g.funcRetTypes[fnName]; ok && t != "void" {
					return t
				}
			}
			// For void functions with by-reference output parameters,
			// check funcResultLLVMType for the actual result type.
			if g.funcResultLLVMType != nil {
				if ts, ok := g.funcResultLLVMType[fnName]; ok && len(ts) == 1 {
					return ts[0]
				}
			}
			// ForwardFunc built-ins (e.g. arg, args, is-dir) are not in
			// funcRetTypes/funcResultLLVMType. Check builtin package for
			// their return type.
			if m := builtin.FindBuiltinMethod(fnName); m != nil {
				if len(m.Return) == 1 {
					return g.mapToLLVMType(m.Return[0].String())
				}
			}
		}
		// DotExpression calls: method calls (e.g. base.slice(0, n)) and
		// module function calls (e.g. os.get-env('HOSTNAME')).
		if dot, ok := v.Function.(*parser.DotExpression); ok {
			fullName := flattenDottedExpr(dot)
			// Module function calls (e.g. os.get-env): the receiver is a
			// module name, not a variable. Look up by full name first.
			if g.funcRetTypes != nil {
				if t, ok := g.funcRetTypes[fullName]; ok && t != "void" {
					return t
				}
			}
			if g.funcResultLLVMType != nil {
				if ts, ok := g.funcResultLLVMType[fullName]; ok && len(ts) == 1 {
					return ts[0]
				}
			}
			// Check builtin methods (CLibCall / ForwardFunc / etc.) by full
			// name and by short name (stripping module prefix).
			if m := builtin.FindBuiltinMethod(fullName); m != nil {
				if len(m.Return) == 1 {
					return g.mapToLLVMType(m.Return[0].String())
				}
			}
			if idx := strings.Index(fullName, "."); idx >= 0 {
				shortName := fullName[idx+1:]
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					if len(m.Return) == 1 {
						return g.mapToLLVMType(m.Return[0].String())
					}
				}
			}
			// Method calls on variables (e.g. base.slice(0, n)): resolve
			// receiver type and look up method return type.
			if g.ssaTypes != nil {
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					if g.varTypes != nil {
						if recvType, ok := g.varTypes[recv.Value]; ok {
							srcType := strings.TrimPrefix(recvType, "%")
							candidates := []string{srcType}
							if primAliases, ok := llvmTypeToNolang[srcType]; ok {
								candidates = append(candidates, primAliases...)
							}
							for _, cand := range candidates {
								shortName := cand + "." + dot.Property
								if g.funcRetTypes != nil {
									if t, ok := g.funcRetTypes[shortName]; ok && t != "void" {
										return t
									}
								}
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
	case *parser.RunExpression:
		return "%task"
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
	case *parser.RegexLiteral:
		// /pattern/ desugars to regexp-compile('pattern') which returns %regexp
		return "%regexp"
	}
	return ""
}

// structPtrForVar returns the LLVM register holding a %struct-type* pointer
// for the given variable. If the variable was allocated as i64 (e.g. synthetic
// `it` from a nil/err arm) but varTypes tracks it as a struct type (because a
// later arm assigned a struct pointer via ptrtoint), this generates a
// load i64 + inttoptr sequence to recover the struct pointer. Otherwise it
// returns the variable's address directly (suitable for GEP).
func (g *Generator) structPtrForVar(sb *strings.Builder, varName, structType string) string {
	if g.itAllocTypes != nil {
		if allocType, ok := g.itAllocTypes[varName]; ok && allocType == "i64" && g.isStructLLVMType(structType) {
			loadReg := g.tmpReg("it.i64load")
			ptrReg := g.tmpReg("it.ptrcast")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), loadReg, g.varAddr(varName)))
				sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to %s*\n", g.indent(), ptrReg, loadReg, structType))
			}
			return ptrReg
		}
	}
	return g.varAddr(varName)
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
		if g.isStructLLVMType(recvType) {
			structName = strings.TrimPrefix(recvType, "%")
		}
	}

	// Slice view .len: return computed view length directly (no struct access)
	if fieldName == "len" && varName != "" && g.isSliceViewVar(varName) {
		return g.sliceViewLen(varName)
	}

	// Built-in str .len access
	// .len 需要字串的指標（%str-long*），而非載入後的值。
	// 因此對鏈式 receiver 使用 generateExprPtr 取得指標，避免 load。
	if fieldName == "len" && sb != nil {
		if structName == "str-long" {
			ptr := ""
			if varName != "" {
				ptr = g.varAddr(varName)
			} else {
				ptr = g.generateExprPtr(sb, expr.Receiver)
				// generateExprPtr doesn't support CallExpression receivers (e.g. arg(i).len).
				// Fall back to materializing the value into a temp alloca.
				if ptr == "" {
					val := g.generateExprWithSB(sb, expr.Receiver)
					if val != "" && val != "0" {
						if g.isStrPtrReg(val) {
							ptr = val
						} else {
							tmpAlloca := g.tmpReg("strlen.recv")
							sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
							sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, tmpAlloca))
							ptr = tmpAlloca
						}
					}
				}
			}
			return g.extractStrLen(sb, ptr)
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
					tmpAlloca := g.tmpReg("recv.tmp")
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
			reg := g.tmpReg("dot.gep")
			if basePtr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), reg, structTy, structTy, basePtr, fieldIdx))
			} else {
				// Use structPtrForVar to handle i64-alloca-with-struct-type case
				// (e.g. `it` allocated as i64 but holding a struct pointer)
				baseAddr := g.structPtrForVar(sb, varName, structTy)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), reg, structTy, structTy, baseAddr, fieldIdx))
			}
			// 一律 load 欄位值。對 struct 型別的鏈式存取由 generateExprPtr 處理（需指標的情況）。
			loadReg := g.tmpReg("dot.val")
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
		// Slice view: materialize to temporary struct and return pointer
		if g.isSliceViewVar(v.Value) && sb != nil {
			return g.materializeSliceView(sb, v.Value)
		}
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
			if g.isStructLLVMType(recvType) {
				structName = strings.TrimPrefix(recvType, "%")
			}
			if sb != nil {
				basePtr = g.generateExprPtr(sb, v.Receiver)
			}
		}
		if fields, ok := g.structTypes[structName]; ok {
			for i, f := range fields {
				if f.name == v.Property {
					reg := g.tmpReg("dot.ptr.gep")
					structTy := "%" + structName
					if sb != nil {
						if basePtr != "" {
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
								g.indent(), reg, structTy, structTy, basePtr, i))
						} else {
							// Use structPtrForVar to handle i64-alloca-with-struct-type case
							baseAddr := g.structPtrForVar(sb, recvName, structTy)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
								g.indent(), reg, structTy, structTy, baseAddr, i))
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
		// struct.field[i]: get field pointer, then GEP into array with index
		basePtr := g.generateExprPtr(sb, dot)
		if basePtr == "" {
			return ""
		}
		// Determine the field's LLVM array type by looking up the struct definition
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
		fieldArrType := ""
		if structName != "" {
			if fields, ok := g.structTypes[structName]; ok {
				for _, f := range fields {
					if f.name == dot.Property {
						fieldArrType = f.typ
						break
					}
				}
			}
		}
		if fieldArrType == "" || !strings.HasPrefix(fieldArrType, "[") {
			// Not an array field, return field pointer as fallback
			return basePtr
		}
		idx := g.generateExprWithSB(sb, v.Index)
		// Ensure idx is i64
		if strings.HasPrefix(idx, "%") {
			idxType := g.intExprLLVMType(v.Index)
			if idxType != "i64" {
				zextReg := g.tmpReg("dotarr.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
				}
				idx = zextReg
			}
		}
		elemGEP := g.tmpReg("dotarr.ptr.elem")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
				g.indent(), elemGEP, fieldArrType, fieldArrType, basePtr, idx))
		}
		return elemGEP
	}
	if varName == "" {
		// 無法取得指標，回退到載入值
		return ""
	}

	idx := g.generateExprWithSB(sb, v.Index)

	// Slice view element pointer: view[i] = val → use adjusted data pointer
	if g.isSliceViewVar(varName) {
		view := g.sliceViews[varName]
		// Bounds check: use view length
		g.emitBoundsCheck(sb, idx, view.viewLen)
		dataTyped := g.tmpReg("sv.ptr.typed")
		elemGEP := g.tmpReg("sv.ptr.elem")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataTyped, view.dataPtrReg, view.elemType))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
				g.indent(), elemGEP, view.elemType, view.elemType, dataTyped, idx))
		}
		return elemGEP
	}

	if t, ok := g.varTypes[varName]; ok {
		if t == "%arr" {
			llvmElemType := "i64"
			if et, ok := g.arrayElemTypes[varName]; ok {
				llvmElemType = et
			}
			arrRef := llvmVarRef(varName)
			if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
				arrRef = llvmGlobalRef(varName)
			}
			// Bounds check: load arr len and verify idx
			// Skip if the array size is known at compile time and the index is provably in bounds.
			if !g.canSkipBoundsCheck(varName, v.Index) {
				arrLen := g.emitArrLenLoad(sb, arrRef)
				g.emitBoundsCheck(sb, idx, arrLen)
			}
			dataGEP := g.tmpReg("arr.ptr.data.gep")
			dataLoad := g.tmpReg("arr.ptr.data")
			dataTyped := g.tmpReg("arr.ptr.typed")
			elemGEP := g.tmpReg("arr.ptr.elem")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
					g.indent(), dataGEP, arrRef))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
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
			if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
				vecRef = llvmGlobalRef(varName)
			}
			// Bounds check: load vec len and verify idx
			// Skip if the array size is known at compile time and the index is provably in bounds.
			if !g.canSkipBoundsCheck(varName, v.Index) {
				vecLen := g.emitVecLenLoad(sb, vecRef)
				g.emitBoundsCheck(sb, idx, vecLen)
			}
			dataGEP := g.tmpReg("vec.ptr.data.gep")
			dataLoad := g.tmpReg("vec.ptr.data")
			dataTyped := g.tmpReg("vec.ptr.typed")
			elemGEP := g.tmpReg("vec.ptr.elem")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, vecRef))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}
			return elemGEP
		}
		// Fixed-size array parameter (e.g. [64 x %str-long]): varTypes[name] is the
		// raw LLVM array type. Generate a direct GEP on the array variable.
		// Without this branch, names[i].len on a [64]str parameter returns "" and
		// produces `getelementptr %str-long, %str-long* , ...` with an empty pointer.
		if strings.HasPrefix(t, "[") {
			llvmElemType := extractArrayElemType(t)
			if llvmElemType == "" {
				llvmElemType = "i64"
			}
			arrRef := llvmVarRef(varName)
			if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
				arrRef = llvmGlobalRef(varName)
			}
			elemGEP := g.tmpReg("fixarr.ptr.elem")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
					g.indent(), elemGEP, t, t, arrRef, idx))
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
	if strPtr == "" {
		fmt.Fprintf(os.Stderr, "codegen error: extractStrDataPtr called with empty strPtr\n")
		return ""
	}
	dataGEP := g.tmpReg("str-long.data.gep")
	dataLoad := g.tmpReg("str-long.data.val")
	if sb != nil {
		// Handle both @global and %local references
		if strings.HasPrefix(strPtr, "@") {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strPtr))
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strPtr))
		}
		dataLoad = g.loadDataPtrField(sb, dataGEP)
	}
	return dataLoad
}

// extractStrLen extracts the i64 len (field 0) from a %str-long* pointer.
func (g *Generator) extractStrLen(sb *strings.Builder, strPtr string) string {
	if strPtr == "" {
		fmt.Fprintf(os.Stderr, "codegen error: extractStrLen called with empty strPtr\n")
		return ""
	}
	lenGEP := g.tmpReg("str-long.len.gep")
	lenLoad := g.tmpReg("str-long.len.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
	}
	return lenLoad
}

// extractStrCap extracts the i64 cap (field 1) from a %str-long* pointer.
func (g *Generator) extractStrCap(sb *strings.Builder, strPtr string) string {
	if strPtr == "" {
		fmt.Fprintf(os.Stderr, "codegen error: extractStrCap called with empty strPtr\n")
		return ""
	}
	capGEP := g.tmpReg("str-long.cap.gep")
	capLoad := g.tmpReg("str-long.cap.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strPtr))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), capLoad, capGEP))
	}
	return capLoad
}

// emitBoundsCheck emits a call to @nolang.bounds_check(idx, len).
// idx and len must be i64 SSA registers or literals.
// Skipped entirely when g.noBoundsCheck is true (unsafe mode).
func (g *Generator) emitBoundsCheck(sb *strings.Builder, idx string, lenExpr string) {
	if sb == nil || g.noBoundsCheck {
		return
	}
	sb.WriteString(fmt.Sprintf("%scall void @nolang.bounds_check(i64 %s, i64 %s)\n",
		g.indent(), idx, lenExpr))
}

// canSkipBoundsCheck returns true if the bounds check can be provably eliminated.
// This is possible when:
//  1. The array variable has a compile-time known size (via g.arraySizes).
//  2. The index expression is a compile-time constant within bounds, OR
//     the index is a range-loop variable whose range upper bound is within the array size.
//
// Known limitations: only handles the common hot-loop patterns:
//   - Constant index: arr[5] where size >= 6
//   - Range loop variable: `for j in [0..N)` where N <= arraySize
func (g *Generator) canSkipBoundsCheck(varName string, indexExpr parser.Expression) bool {
	if g.arraySizes == nil || varName == "" || indexExpr == nil {
		return false
	}
	arrSize, ok := g.arraySizes[varName]
	if !ok || arrSize <= 0 {
		if os.Getenv("NOLANG_DEBUG_BC") != "" {
			fmt.Fprintf(os.Stderr, "[debug-bc] SKIP: varName=%s not in arraySizes\n", varName)
		}
		return false
	}

	// Case 1: Constant integer index
	if intLit, ok := indexExpr.(*parser.IntegerLiteral); ok {
		result := intLit.Value >= 0 && intLit.Value < arrSize
		if os.Getenv("NOLANG_DEBUG_BC") != "" {
			fmt.Fprintf(os.Stderr, "[debug-bc] const: varName=%s arrSize=%d idx=%d -> %v\n", varName, arrSize, intLit.Value, result)
		}
		return result
	}

	// Case 2: Range loop variable — check if it's tracked as a range loop var
	// with a known upper bound <= arraySize.
	if ident, ok := indexExpr.(*parser.Identifier); ok {
		// Check if this identifier is a range loop variable with a known bound.
		if g.rangeLoopBounds != nil {
			if bound, ok := g.rangeLoopBounds[ident.Value]; ok {
				result := bound >= 0 && bound <= arrSize
				if os.Getenv("NOLANG_DEBUG_BC") != "" {
					fmt.Fprintf(os.Stderr, "[debug-bc] loop: varName=%s arrSize=%d idxVar=%s bound=%d -> %v\n", varName, arrSize, ident.Value, bound, result)
				}
				return result
			}
			if os.Getenv("NOLANG_DEBUG_BC") != "" {
				fmt.Fprintf(os.Stderr, "[debug-bc] loop: varName=%s arrSize=%d idxVar=%s NOT in rangeLoopBounds\n", varName, arrSize, ident.Value)
			}
		}
	}

	return false
}

// emitArrLenLoad loads the i64 len (field 0) from a %arr* pointer.
func (g *Generator) emitArrLenLoad(sb *strings.Builder, arrRef string) string {
	lenGEP := g.tmpReg("arr.len.gep")
	lenLoad := g.tmpReg("arr.len.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, arrRef))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
	}
	return lenLoad
}

// emitVecLenLoad loads the i64 len (field 0) from a %vec* pointer.
func (g *Generator) emitVecLenLoad(sb *strings.Builder, vecRef string) string {
	lenGEP := g.tmpReg("vec.len.gep")
	lenLoad := g.tmpReg("vec.len.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, vecRef))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))
	}
	return lenLoad
}

// emitVecCapLoad loads the i64 cap (field 1) from a %vec* pointer.
func (g *Generator) emitVecCapLoad(sb *strings.Builder, vecRef string) string {
	capGEP := g.tmpReg("vec.cap.gep")
	capLoad := g.tmpReg("vec.cap.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
			g.indent(), capGEP, vecRef))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), capLoad, capGEP))
	}
	return capLoad
}

// emitStrCapLoad loads the i64 cap (field 1) from a %str-long* pointer.
func (g *Generator) emitStrCapLoad(sb *strings.Builder, strRef string) string {
	capGEP := g.tmpReg("str.cap.gep")
	capLoad := g.tmpReg("str.cap.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
			g.indent(), capGEP, strRef))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), capLoad, capGEP))
	}
	return capLoad
}

// extractLenDispatch extracts len from %str-long based on known variable type.
func (g *Generator) extractLenDispatch(sb *strings.Builder, varName string) string {
	return g.extractStrLen(sb, "%"+varName)
}

// extractDataPtrDispatch extracts data ptr from %str-long based on known variable type.
func (g *Generator) extractDataPtrDispatch(sb *strings.Builder, varName string) string {
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
		if g.isStructLLVMType(recvType) {
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
			fieldGEP := g.tmpReg("set.field.gep")
			structTy := "%" + structName
			if basePtr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, basePtr, fieldIdx))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, g.varAddr(recvName), fieldIdx))
			}

			if fieldType == "%vec" {
				// Determine element type from field definition
				vecElemType := "i64"
				if fields[fieldIdx].elemType != "" {
					vecElemType = fields[fieldIdx].elemType
				}

				// Bounds check for writes: use cap (field 1), not len (field 0)
				capGEP := g.tmpReg("set.vec.cap.gep")
				capLoad := g.tmpReg("set.vec.cap.val")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
					g.indent(), capGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
					g.indent(), capLoad, capGEP))
				g.emitBoundsCheck(sb, idx, capLoad)

				// Slice field: load data pointer (field 2), bitcast, GEP, store
				dataGEP := g.tmpReg("set.vec.data.gep")
				dataLoad := g.tmpReg("set.vec.data")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, fieldGEP))
				dataLoad = g.loadDataPtrField(sb, dataGEP)

				// Bitcast to element type pointer
				dataTyped := g.tmpReg("set.vec.typed")
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, vecElemType))

				// Coerce val to element type if needed
				storeVal := val
				if strings.HasPrefix(val, "%") {
					valType := g.intExprLLVMType(value)
					if valType != "" && valType != vecElemType && g.isIntegerLLVMType(valType) && g.isIntegerLLVMType(vecElemType) {
						convReg := g.tmpReg("set.vec.conv")
						if valType == "i64" {
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, valType, val, vecElemType))
						} else if vecElemType == "i64" {
							sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), convReg, valType, val))
						} else {
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, valType, val, vecElemType))
						}
						storeVal = convReg
					}
				}

				// GEP to element index and store
				elemGEP := g.tmpReg("set.vec.elem")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, vecElemType, vecElemType, dataTyped, idx))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), vecElemType, storeVal, vecElemType, elemGEP))

				// Auto-update len (field 0) to max(len, idx+1)
				lenGEP := g.tmpReg("set.vec.len.gep")
				curLen := g.tmpReg("set.vec.cur-len")
				newLen := g.tmpReg("set.vec.new-len")
				cmpReg := g.tmpReg("set.vec.cmp")
				finalLen := g.tmpReg("set.vec.final-len")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
					g.indent(), lenGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
				sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
				return "0"
			}

			if fieldType == "%str-long" {
				// str-long field: load data pointer (field 1), GEP, store
				dataGEP := g.tmpReg("set.strf.data.gep")
				dataLoad := g.tmpReg("set.strf.data")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, fieldGEP))
				dataLoad = g.loadDataPtrField(sb, dataGEP)

				elemGEP := g.tmpReg("set.strf.elem")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
					g.indent(), elemGEP, dataLoad, idx))
				storeVal := val
				if newVal, ok := g.coerceStrLitToByte(sb, val, value); ok {
					storeVal = newVal
				} else if strings.HasPrefix(val, "%") {
					valType := g.intExprLLVMType(value)
					if strings.HasPrefix(valType, "i") && valType != "i8" {
						truncReg := g.tmpReg("set.strf.trunc")
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
						storeVal = truncReg
					}
				}
				sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n",
					g.indent(), storeVal, elemGEP))

				// Auto-update len: load cur len, compute idx+1, store max
				lenGEP := g.tmpReg("set.strf.len.gep")
				curLen := g.tmpReg("set.strf.cur-len")
				newLen := g.tmpReg("set.strf.new-len")
				cmpReg := g.tmpReg("set.strf.cmp")
				finalLen := g.tmpReg("set.strf.final-len")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
					g.indent(), lenGEP, fieldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
				sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
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
					elemGEP := g.tmpReg("set.arr.elem")
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
						g.indent(), elemGEP, fieldType, fieldType, fieldGEP, idx))
					// Truncate/extend val to elemType if needed (e.g., i64 → i8 for byte arrays)
					// Only convert for integer types; struct types (e.g. %str-long) need different handling
					storeVal := val
					if elemType != "i64" && !g.isStructLLVMType(elemType) && strings.HasPrefix(val, "%") {
						valType := g.intExprLLVMType(value)
						if valType == "" {
							valType = "i64" // default assumption
						}
						if valType != elemType {
							convReg := g.tmpReg("set.arr.trunc")
							// Determine if we need trunc (wider→narrower) or zext (narrower→wider)
							valW := llvmIntBitWidth(valType)
							elemW := llvmIntBitWidth(elemType)
							if valW >= elemW {
								sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, valType, val, elemType))
							} else {
								sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to %s\n", g.indent(), convReg, valType, val, elemType))
							}
							storeVal = convReg
						}
					}
					// s2s conversion: StringLiteral → %str-long value
					// when assigning to a %str-long array element (e.g., keys[i] = 'foo')
					// All string literals are %str-long* pointers (alloca), need to load the value.
					if elemType == "%str-long" && strings.HasPrefix(val, "%str-longlit.") {
						if _, ok := value.(*parser.StringLiteral); ok {
							loadReg := g.tmpReg("str-long.load")
							sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
							storeVal = loadReg
						}
					}
					// Struct element copy (e.g., .names[i] = .names[last]):
					// generateExprWithSB returns a %str-long* pointer (from
					// generateStructFieldIndexRead for IndexExpression, or alloca from
					// concat/repeat), which must be loaded before storing as a struct value.
					// StringLiteral is handled above; Identifier returns a loaded value.
					if g.isStructLLVMType(elemType) {
						// Integer literal 0 assigned to a struct-type array element
						// (e.g., .vals[i] = 0 where vals is [256]str): use zeroinitializer.
						if intLit, ok := value.(*parser.IntegerLiteral); ok && intLit.Value == 0 {
							storeVal = "zeroinitializer"
						} else {
							_, isStrLit := value.(*parser.StringLiteral)
							_, isIdent := value.(*parser.Identifier)
							if !isStrLit && !isIdent {
								_, isIdx := value.(*parser.IndexExpression)
								if isIdx || g.isStringExpr(value) {
									loadReg := g.tmpReg("set.arr.load")
									sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
										g.indent(), loadReg, elemType, elemType, val))
									storeVal = loadReg
								}
							}
						}
					}
					// Heap-owning element types (%str-long/%vec/%arr/user struct):
					// shallow store shares data pointer between source and slot →
					// double-free at scope exit. Deep clone gives the slot its own
					// independent heap data. Before cloning, free the slot's old value
					// (with len==0 guard for containers to avoid freeing garbage
					// pointers in uninitialized slots).
					if g.isHeapOwningType(elemType) {
						if elemType == "%str-long" || elemType == "%vec" || elemType == "%arr" {
							oldLenGEP := g.tmpReg("set.arr.oldlen.gep")
							oldLenReg := g.tmpReg("set.arr.oldlen")
							lenCmpReg := g.tmpReg("set.arr.lencmp")
							g.tmpIdx++
							skipFreeLabel := fmt.Sprintf("set.arr.skipfree.%d", g.tmpIdx)
							g.tmpIdx++
							doFreeLabel := fmt.Sprintf("set.arr.dofree.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
								g.indent(), oldLenGEP, elemType, elemType, elemGEP))
							sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldLenReg, oldLenGEP))
							sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), lenCmpReg, oldLenReg))
							sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n",
								g.indent(), lenCmpReg, skipFreeLabel, doFreeLabel))
							g.emitLabel(sb, doFreeLabel)
							g.emitElementFree(sb, elemGEP, elemType)
							sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipFreeLabel))
							g.emitLabel(sb, skipFreeLabel)
						} else {
							// User struct: emitElementFree walks fields with NULL checks.
							g.emitElementFree(sb, elemGEP, elemType)
						}
						// Build srcPtr: alloca + store the value, then deep clone to slot.
						cloneSrc := g.tmpReg("set.arr.csrc")
						sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), cloneSrc, elemType))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
							g.indent(), elemType, storeVal, elemType, cloneSrc))
						g.emitDeepClone(sb, cloneSrc, elemGEP, elemType, "")
					} else {
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
							g.indent(), elemType, storeVal, elemType, elemGEP))
					}
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
		if g.isStructLLVMType(recvType) {
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
						tmpAlloca := g.tmpReg("recv.tmp")
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
			fieldGEP := g.tmpReg("idx.field.gep")
			structTy := "%" + structName
			if basePtr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, basePtr, fieldIdx))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), fieldGEP, structTy, structTy, g.varAddr(recvName), fieldIdx))
			}

			if fieldType == "%vec" {
				// Determine element type from field definition
				vecElemType := "i64"
				if fields[fieldIdx].elemType != "" {
					vecElemType = fields[fieldIdx].elemType
				}

				// Bounds check for reads: use len (field 0)
				vecLen := g.emitVecLenLoad(sb, fieldGEP)
				g.emitBoundsCheck(sb, idx, vecLen)

				// Slice field: load data pointer, bitcast, GEP, load
				dataGEP := g.tmpReg("idx.vec.data.gep")
				dataLoad := g.tmpReg("idx.vec.data")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, fieldGEP))
				dataLoad = g.loadDataPtrField(sb, dataGEP)

				// Bitcast to element type pointer
				dataTyped := g.tmpReg("idx.vec.typed")
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, vecElemType))

				// GEP to element and load
				elemGEP := g.tmpReg("idx.vec.elem")
				elemLoad := g.tmpReg("idx.vec.val")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, vecElemType, vecElemType, dataTyped, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
					g.indent(), elemLoad, vecElemType, vecElemType, elemGEP))
				// Track the SSA type so downstream consumers (e.g. option assignment)
				// can distinguish loaded struct values from pointers.
				if g.ssaTypes != nil && g.isStructLLVMType(vecElemType) {
					g.ssaTypes[elemLoad] = vecElemType
				}
				// Zext to i64 if element type is smaller (callers expect i64)
				if vecElemType != "i64" && g.isIntegerLLVMType(vecElemType) {
					zextReg := g.tmpReg("idx.vec.zext")
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n",
						g.indent(), zextReg, vecElemType, elemLoad))
					return zextReg
				}
				return elemLoad
			}

			if fieldType == "%str-long" {
				// str-long field: load data pointer (field 1), GEP to byte, load, zext to i64
				// 對應 generateStructFieldIndexAssign 的 %str-long case（讀取版本）
				strIdx := idx
				if strings.HasPrefix(idx, "%") {
					idxType := g.intExprLLVMType(index)
					if idxType != "i64" {
						zextReg := g.tmpReg("idx.strf.zext")
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
						strIdx = zextReg
					}
				}
				dataGEP := g.tmpReg("idx.strf.data.gep")
				dataLoad := g.tmpReg("idx.strf.data")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, fieldGEP))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
				charGEP := g.tmpReg("idx.strf.char.gep")
				charLoad := g.tmpReg("idx.strf.char.val")
				charZext := g.tmpReg("idx.strf.char.zext")
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
					g.indent(), charGEP, dataLoad, strIdx))
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
					elemGEP := g.tmpReg("idx.arr.elem")
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
						g.indent(), elemGEP, fieldType, fieldType, fieldGEP, idx))
					// Struct element: return pointer (by-reference), no load needed
					if g.isStructLLVMType(elemType) {
						return elemGEP
					}
					// Integer element: load value
					elemLoad := g.tmpReg("idx.arr.val")
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
						g.indent(), elemLoad, elemType, elemType, elemGEP))
					if elemType != "i64" {
						zextReg := g.tmpReg("idx.arr.zext")
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
			tmpAlloca := g.tmpReg("nestidx.tmp")
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
			zextReg := g.tmpReg("nestidx.zext")
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
	dataGEP := g.tmpReg("nestidx.data.gep")
	dataLoad := g.tmpReg("nestidx.data")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
		g.indent(), dataGEP, strPtr))
	dataLoad = g.loadDataPtrField(sb, dataGEP)
	// GEP 到第 i 個位元組並 load
	charGEP := g.tmpReg("nestidx.char.gep")
	charLoad := g.tmpReg("nestidx.char.val")
	charZext := g.tmpReg("nestidx.char.zext")
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
			zextReg := g.tmpReg("nestset.zext")
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
		dataGEP := g.tmpReg("nestset.data.gep")
		dataLoad := g.tmpReg("nestset.data")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
			g.indent(), dataGEP, strPtr))
		dataLoad = g.loadDataPtrField(sb, dataGEP)
		// GEP 到第 i 個位元組
		charGEP := g.tmpReg("nestset.char.gep")
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
			g.indent(), charGEP, dataLoad, idx))
		// 截斷值為 i8 並 store
		truncReg := g.tmpReg("nestset.trunc")
		sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i8\n", g.indent(), truncReg, val))
		sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n", g.indent(), truncReg, charGEP))
	}
	return "0"
}

// coerceStrLitToByte loads the first byte from a %str-longlit.N pointer (string
// literal) when it needs to be used as an i8 value (e.g., out[pos] = '=').
// Returns (byteReg, true) if coercion was applied, or (originalVal, false) otherwise.
func (g *Generator) coerceStrLitToByte(sb *strings.Builder, val string, expr parser.Expression) (string, bool) {
	if !strings.HasPrefix(val, "%str-longlit.") {
		return val, false
	}
	if _, ok := expr.(*parser.StringLiteral); !ok {
		return val, false
	}
	if sb == nil {
		return "0", true
	}
	// %str-long*: field 2 is i64 data pointer
	dataPtrGEP := g.tmpReg("strlit.dataptr.gep")
	dataPtrLoad := g.tmpReg("strlit.dataptr")
	byteLoad := g.tmpReg("strlit.byte")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataPtrGEP, val))
	dataPtrLoad = g.loadDataPtrField(sb, dataPtrGEP)
	sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), byteLoad, dataPtrLoad))
	return byteLoad, true
}

func (g *Generator) generateAssignExpression(sb *strings.Builder, expr *parser.AssignExpression) string {
	// 巢狀欄位賦值: struct.field.subfield = value (e.g., self.p.len = val)
	if dot, ok := expr.Left.(*parser.DotExpression); ok {
		if innerDot, ok := dot.Receiver.(*parser.DotExpression); ok {
			if innerIdent, ok := innerDot.Receiver.(*parser.Identifier); ok {
				recvName := innerIdent.Value
				outerField := innerDot.Property
				innerField := dot.Property
				// 返回值延遲零值追蹤：out.field.subfield = expr 時標記 out 參數已賦值
				if sb != nil && g.outputParamNames != nil && g.outputParamNames[recvName] {
					g.emitSetRetInitBit(sb, recvName)
				}
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
						outerGEP := g.tmpReg("set.nested.outer.gep")
						structTy := "%" + structName
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
							g.indent(), outerGEP, structTy, structTy, g.varAddr(recvName), outerIdx))
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
								subGEP := g.tmpReg("set.nested.sub.gep")
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

		// 返回值延遲零值追蹤：out.field = expr 時標記 out 參數已賦值
		if varName != "" && sb != nil && g.outputParamNames != nil && g.outputParamNames[varName] {
			g.emitSetRetInitBit(sb, varName)
		}

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
			if g.isStructLLVMType(recvType) {
				structName = strings.TrimPrefix(recvType, "%")
			}
			if sb != nil {
				basePtr = g.generateExprPtr(sb, dot.Receiver)
				if basePtr == "" {
					// Fallback: 生成值後存入臨時 alloca
					val2 := g.generateExprWithSB(sb, dot.Receiver)
					if val2 != "" && val2 != "0" && recvType != "" {
						tmpAlloca := g.tmpReg("assign.tmp")
						sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, recvType))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), recvType, val2, recvType, tmpAlloca))
						basePtr = tmpAlloca
					}
				}
			}
		}

		// 當值為 StructLiteral 時，直接在目標欄位上設定每個子欄位，
		// 因為 generateStructLiteral 只返回佔位符 "{ }"。
		if structLit, ok := expr.Value.(*parser.StructLiteral); ok {
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
					reg := g.tmpReg("set.gep")
					if basePtr != "" {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
							g.indent(), reg, structTy, structTy, basePtr, fieldIdx))
					} else {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
							g.indent(), reg, structTy, structTy, g.varAddr(varName), fieldIdx))
					}
					// 先用 zeroinitializer 清零
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), fieldType, fieldType, reg))
					// 逐欄位設定
					if structLitFields, ok := g.structTypes[strings.TrimPrefix(fieldType, "%")]; ok {
						fieldIdxMap := make(map[string]int)
						for i, f := range structLitFields {
							fieldIdxMap[f.name] = i
						}
						for _, sf := range structLit.Fields {
							if sfi, ok := fieldIdxMap[sf.Name]; ok {
								sfType := structLitFields[sfi].typ
								sfVal := g.generateExprWithSB(sb, sf.Value)
								sfGEP := g.tmpReg("set.st.fld")
								sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
									g.indent(), sfGEP, fieldType, fieldType, reg, sfi))
								sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), sfType, sfVal, sfType, sfGEP))
							}
						}
					}
				}
				return "0"
			}
		}

		// Set target type info for type-inferred builtins (e.g. with-len)
	// before generating the RHS value. Without this, with-len defaults
	// to i64 (8 bytes) element size, causing undersized allocations for
	// slice fields with larger element types (e.g. []str needs 24 bytes
	// per %str-long element). This leads to buffer overflows when the
	// field is subsequently indexed (e.g. .keys[idx] = key writes past
	// the allocated buffer).
	prevTargetType := g.currentTargetType
	prevTargetElemType := g.currentTargetElemType
	g.currentTargetType = ""
	g.currentTargetElemType = ""
	if structName != "" {
		if fields, ok := g.structTypes[structName]; ok {
			for _, f := range fields {
				if f.name == fieldName {
					g.currentTargetType = f.typ
					if f.typ == "%vec" && f.elemType != "" {
						g.currentTargetElemType = f.elemType
					}
					break
				}
			}
		}
	}

	val := g.generateExprWithSB(sb, expr.Value)

	g.currentTargetType = prevTargetType
	g.currentTargetElemType = prevTargetElemType

	// structName 和 basePtr 已在上方 StructLiteral 處理中宣告
	if varName != "" {
		if t, ok := g.varTypes[varName]; ok {
			structName = strings.TrimPrefix(t, "%")
		}
	} else {
			recvType := g.exprResultLLVMType(dot.Receiver)
			if g.isStructLLVMType(recvType) {
				structName = strings.TrimPrefix(recvType, "%")
			}
			if sb != nil {
				basePtr = g.generateExprPtr(sb, dot.Receiver)
				if basePtr == "" {
					// Fallback: 生成值後存入臨時 alloca
					val2 := g.generateExprWithSB(sb, dot.Receiver)
					if val2 != "" && val2 != "0" && recvType != "" {
						tmpAlloca := g.tmpReg("assign.tmp")
						sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, recvType))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), recvType, val2, recvType, tmpAlloca))
						basePtr = tmpAlloca
					}
				}
			}
		}

		reg := g.tmpReg("set.gep")

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
				// String literal is %str-long* pointer (alloca), load the value.
				if fieldType == "%str-long" {
					if _, ok := expr.Value.(*parser.StringLiteral); ok {
						loadReg := g.tmpReg("set.fld.strload")
						sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
						val = loadReg
					}
				}
				// Struct field assignment from a pointer-producing expression (e.g.,
				// rec.value = a - b, rec.field = .names[i]): generateExprWithSB returns
				// a %str-long* pointer (alloca from concat/repeat, or GEP from array
				// element read), which must be loaded before storing as a struct value.
				// StringLiteral is handled above; Identifier returns a loaded value.
				if g.isStructLLVMType(fieldType) {
					// Integer literal 0 assigned to a struct-type field (e.g.,
					// rec.field = 0 where field is str): use zeroinitializer.
					if intLit, ok := expr.Value.(*parser.IntegerLiteral); ok && intLit.Value == 0 {
						val = "zeroinitializer"
					} else {
						_, isStrLit := expr.Value.(*parser.StringLiteral)
						_, isIdent := expr.Value.(*parser.Identifier)
						if !isStrLit && !isIdent {
							_, isIdx := expr.Value.(*parser.IndexExpression)
							_, isInfix := expr.Value.(*parser.InfixExpression)
							// Only load for pointer-producing expressions:
							//   - IndexExpression (GEP → pointer)
							//   - InfixExpression (string concat/repeat → alloca pointer)
							// CallExpression and DotExpression return loaded values, not pointers.
							if isIdx || (isInfix && g.isStringExpr(expr.Value)) {
								loadReg := g.tmpReg("set.fld.load")
								sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
									g.indent(), loadReg, fieldType, fieldType, val))
								val = loadReg
							}
						}
					}
				}
				structTy := "%" + structName
				if basePtr != "" {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), reg, structTy, structTy, basePtr, fieldIdx))
				} else {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), reg, structTy, structTy, g.varAddr(varName), fieldIdx))
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
		// 返回值延遲零值追蹤：out[i] = expr 時標記 out 參數已賦值
		if varName != "" && sb != nil && g.outputParamNames != nil && g.outputParamNames[varName] {
			g.emitSetRetInitBit(sb, varName)
		}
		idx := g.generateExprWithSB(sb, idxExpr.Index)
		// GEP 索引必須是 i64；若索引為 i8/i16/i32 SSA 值則 zext 到 i64
		if strings.HasPrefix(idx, "%") {
			idxType := g.intExprLLVMType(idxExpr.Index)
			if idxType != "i64" {
				zextReg := g.tmpReg("idx.set.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
				}
				idx = zextReg
			}
		}
		val := g.generateExprWithSB(sb, expr.Value)

		// Slice view index assignment: view[i] = val → use adjusted data pointer
		if varName != "" && g.isSliceViewVar(varName) {
			view := g.sliceViews[varName]
			// Bounds check: use view length
			g.emitBoundsCheck(sb, idx, view.viewLen)
			dataTyped := g.tmpReg("sv.set.typed")
			elemGEP := g.tmpReg("sv.set.elem")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, view.dataPtrReg, view.elemType))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, view.elemType, view.elemType, dataTyped, idx))
				// Truncate i64 to smaller element type if needed
				storeVal := val
				if view.elemType != "i64" && !strings.HasPrefix(val, "%") {
					storeVal = val // constant, LLVM will handle truncation
				} else if view.elemType != "i64" && strings.HasPrefix(val, "%") {
					truncReg := g.tmpReg("sv.set.trunc")
					sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n",
						g.indent(), truncReg, val, view.elemType))
					storeVal = truncReg
				}
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), view.elemType, storeVal, view.elemType, elemGEP))
			}
			return "0"
		}

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

				// Bounds check: load arr len and verify idx
				arrLen := g.emitArrLenLoad(sb, g.varAddr(varName))
				g.emitBoundsCheck(sb, idx, arrLen)

				// Load data pointer from arr struct
				dataGEP := g.tmpReg("arr.set.data.gep")
				dataLoad := g.tmpReg("arr.set.data")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
						g.indent(), dataGEP, g.varAddr(varName)))
					dataLoad = g.loadDataPtrField(sb, dataGEP)
				}

				// Bitcast to element type pointer
				dataTyped := g.tmpReg("arr.set.typed")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
						g.indent(), dataTyped, dataLoad, llvmElemType))
				}

				// GEP to element index and store
				elemGEP := g.tmpReg("arr.set.elem")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
						g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
					storeVal := val
					// 處理字串值儲存到陣列元素：
					// - 元素型別為 %str-long：字串字面量需轉為 str-long value；
					//   str-long 變數需 load 出 struct value
					// - 元素型別為 i64（str 以指標儲存）：字串指標需 ptrtoint
					// 注意：字串拼接（InfixExpression）結果是 %str-long value，不是指標，不在此處理。
					if llvmElemType == "%str-long" {
						if ident, ok := expr.Value.(*parser.Identifier); ok {
							if t, ok := g.varTypes[ident.Value]; ok && t == "%str-long" {
								// val is already a loaded %str-long value from generateExprWithSB,
								// so we can use it directly without another load.
								storeVal = val
							}
						} else if g.isStrPtrReg(val) {
							// val is a %str-long* pointer (e.g. sprintf-based to-str result).
							// Load the %str-long value from the pointer before storing.
							loadReg := g.tmpReg("str-long.load")
							if sb != nil {
								sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
							}
							storeVal = loadReg
						}
					} else if llvmElemType == "i64" {
						needPtrToInt := false
						strLLVMType := ""
						switch e := expr.Value.(type) {
						case *parser.StringLiteral:
							needPtrToInt = true
							strLLVMType = "%str-long*"
						case *parser.Identifier:
							if t, ok := g.varTypes[e.Value]; ok {
								if t == "%str-long" {
									needPtrToInt = true
									strLLVMType = "%str-long*"
								}
							}
						}
						if needPtrToInt {
							convReg := g.tmpReg("arr.set.conv")
							sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %s %s to i64\n", g.indent(), convReg, strLLVMType, val))
							storeVal = convReg
						}
					}
					if storeVal == val && strings.HasPrefix(val, "%") {
						srcType := g.intExprLLVMType(expr.Value)
						// Only convert between integer types; skip for struct types (e.g. %str-long)
						if srcType != "" && srcType != llvmElemType && g.isIntegerLLVMType(srcType) && g.isIntegerLLVMType(llvmElemType) {
							convReg := g.tmpReg("arr.set.conv")
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

				// Bounds check for writes: use cap (field 1), not len (field 0).
				// This allows vec[i] = val for i in [0..cap) even when len == 0,
				// which is required for patterns like `data[next] = value` on
				// freshly declared []byte locals and struct fields.
				// Skip bounds check for constant indices — LLVM cannot eliminate
				// the inlined check across loop iterations, and a constant index
				// into a slice literal is always within bounds (otherwise the
				// program would have crashed on first iteration).
				varIdxExpr2, _ := expr.Left.(*parser.IndexExpression)
				_, isConstIdx2 := varIdxExpr2.Index.(*parser.IntegerLiteral)
				if !isConstIdx2 {
					vecCap := g.emitVecCapLoad(sb, g.varAddr(varName))
					g.emitBoundsCheck(sb, idx, vecCap)
				}

				// Load data pointer from vec struct (field 2)
				dataGEP := g.tmpReg("vec.set.data.gep")
				dataLoad := g.tmpReg("vec.set.data")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
						g.indent(), dataGEP, g.varAddr(varName)))
					dataLoad = g.loadDataPtrField(sb, dataGEP)
				}

				// Bitcast to element type pointer
				dataTyped := g.tmpReg("vec.set.typed")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
						g.indent(), dataTyped, dataLoad, llvmElemType))
				}

				// Coerce val to element type if needed (e.g., i64 → i32, i32 → i64, i32 → i32)
				storeVal := val
				if strings.HasPrefix(val, "%") {
					srcType := g.intExprLLVMType(expr.Value)
					// Only convert between integer types; skip for struct types (e.g. %str-long)
					if srcType != "" && srcType != llvmElemType && g.isIntegerLLVMType(srcType) && g.isIntegerLLVMType(llvmElemType) {
						convReg := g.tmpReg("vec.set.conv")
						if sb != nil {
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
						}
						storeVal = convReg
					}
				}

				// GEP to element index and store
				elemGEP := g.tmpReg("vec.set.elem")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
						g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
						g.indent(), llvmElemType, storeVal, llvmElemType, elemGEP))

					// Auto-update len (field 0) to max(len, idx+1). Without this,
					// sha256/hmac-sha256/prf receive vec.len == 0 even after
					// elements were written via vec[i] = val, producing wrong outputs.
					// Optimization: skip the load+add+icmp+select+store when the index
					// is a compile-time constant. For constant indices, either:
					//  (a) idx+1 <= len → max(len, idx+1) == len → store is redundant
					//  (b) idx+1 > len  → bounds check on cap already passed, but
					//      updating len for a constant index past len is not a
					//      pattern that appears in correct code (the slice would
					//      have been sized appropriately at initialization).
					varIdxExpr, _ := expr.Left.(*parser.IndexExpression)
					_, isConstIdx := varIdxExpr.Index.(*parser.IntegerLiteral)
					if !isConstIdx {
						lenGEP := g.tmpReg("vec.set.len.gep")
						curLen := g.tmpReg("vec.set.cur-len")
						newLen := g.tmpReg("vec.set.new-len")
						cmpReg := g.tmpReg("vec.set.cmp")
						finalLen := g.tmpReg("vec.set.final-len")
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
							g.indent(), lenGEP, g.varAddr(varName)))
						sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
						sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
						sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
						sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
						sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
					}
				}
				return "0"
			}

			if t == "%str-long" {
				// %str-long type: load data pointer (field 1), GEP, store
				// Also auto-update len (field 0) to max(len, idx+1)

				// Bounds check for writes: use cap (field 1), not len (field 0).
				// This allows str[i] = val for i in [0..cap) even when len == 0,
				// which is required for patterns like `out[i] = c` on str return
				// values (e.g. []byte.to-str) that start with len=0 but cap>0.
				strCap := g.extractStrCap(sb, g.varAddr(varName))
				g.emitBoundsCheck(sb, idx, strCap)

				dataGEP := g.tmpReg("str-long.set.data.gep")
				dataLoad := g.tmpReg("str-long.set.data")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
						g.indent(), dataGEP, g.varAddr(varName)))
					dataLoad = g.loadDataPtrField(sb, dataGEP)
				}

				// GEP into data with index
				elemGEP := g.tmpReg("str-long.set.elem")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
						g.indent(), elemGEP, dataLoad, idx))
					storeVal := val
					if newVal, ok := g.coerceStrLitToByte(sb, val, expr.Value); ok {
						storeVal = newVal
					} else if strings.HasPrefix(val, "%") {
						valType := g.intExprLLVMType(expr.Value)
						if strings.HasPrefix(valType, "i") && valType != "i8" {
							truncReg := g.tmpReg("trunc.i8")
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
							storeVal = truncReg
						}
					}
					sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n",
						g.indent(), storeVal, elemGEP))

					// Auto-update len: load cur len, compute idx+1, store max
					lenGEP := g.tmpReg("str-long.set.len.gep")
					curLen := g.tmpReg("str-long.set.cur-len")
					newLen := g.tmpReg("str-long.set.new-len")
					cmpReg := g.tmpReg("str-long.set.cmp")
					finalLen := g.tmpReg("str-long.set.final-len")
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
		gepReg := g.tmpReg("set.gep")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
				g.indent(), gepReg, arrayLLVMType, arrayLLVMType, g.varAddr(varName), idx))
			storeVal := val
			if llvmElemType == "i8" {
				if newVal, ok := g.coerceStrLitToByte(sb, val, expr.Value); ok {
					storeVal = newVal
				} else if strings.HasPrefix(val, "%") {
					valType := g.intExprLLVMType(expr.Value)
					if strings.HasPrefix(valType, "i") && valType != "i8" {
						truncReg := g.tmpReg("trunc.i8")
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to i8\n", g.indent(), truncReg, valType, val))
						storeVal = truncReg
					}
				}
			}
			// %str-long array element: string literals are %str-long*
			// pointers (alloca results), need to load the %str-long value.
			if llvmElemType == "%str-long" {
				if _, ok := expr.Value.(*parser.StringLiteral); ok {
					loadReg := g.tmpReg("set.arr.strload")
					sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
					storeVal = loadReg
				}
			}
			// Heap-owning element types (%str-long/%vec/%arr/user struct):
			// shallow store shares data pointer between source and slot →
			// double-free at scope exit. Deep clone gives the slot its own
			// independent heap data. Before cloning, free the slot's old value
			// (with len==0 guard for containers to avoid freeing garbage
			// pointers in uninitialized slots).
			if g.isHeapOwningType(llvmElemType) {
				if llvmElemType == "%str-long" || llvmElemType == "%vec" || llvmElemType == "%arr" {
					oldLenGEP := g.tmpReg("set.ga.oldlen.gep")
					oldLenReg := g.tmpReg("set.ga.oldlen")
					lenCmpReg := g.tmpReg("set.ga.lencmp")
					g.tmpIdx++
					skipFreeLabel := fmt.Sprintf("set.ga.skipfree.%d", g.tmpIdx)
					g.tmpIdx++
					doFreeLabel := fmt.Sprintf("set.ga.dofree.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
						g.indent(), oldLenGEP, llvmElemType, llvmElemType, gepReg))
					sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), oldLenReg, oldLenGEP))
					sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), lenCmpReg, oldLenReg))
					sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n",
						g.indent(), lenCmpReg, skipFreeLabel, doFreeLabel))
					g.emitLabel(sb, doFreeLabel)
					g.emitElementFree(sb, gepReg, llvmElemType)
					sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), skipFreeLabel))
					g.emitLabel(sb, skipFreeLabel)
				} else {
					// User struct: emitElementFree walks fields with NULL checks.
					g.emitElementFree(sb, gepReg, llvmElemType)
				}
				// Build srcPtr: alloca + store the value, then deep clone to slot.
				cloneSrc := g.tmpReg("set.ga.csrc")
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), cloneSrc, llvmElemType))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), llvmElemType, storeVal, llvmElemType, cloneSrc))
				g.emitDeepClone(sb, cloneSrc, gepReg, llvmElemType, "")
			} else {
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), llvmElemType, storeVal, llvmElemType, gepReg))
			}
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
	} else if sliceExpr, ok := expr.Left.(*parser.SliceExpression); ok {
		// 巢狀切片索引讀取：例如 arr[1..3][0] 或 s[1..3][2]
		// 內層 arr[1..3] 由 generateSliceExpression 回傳 %slic.N (alloca %vec/%str-long)，
		// 外層 [i] 需取出該結構的 data 指標後 GEP 到第 i 個元素。
		return g.generateSliceExprIndexRead(sb, sliceExpr, expr.Index)
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
			zextReg := g.tmpReg("idx.zext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}

	// Slice view indexing: view[i] → use adjusted data pointer + offset, no struct access
	if varName != "" && g.isSliceViewVar(varName) {
		view := g.sliceViews[varName]
		// Bounds check: use view length
		g.emitBoundsCheck(sb, idx, view.viewLen)
		if view.isStr {
			// String slice: GEP into i8* data pointer, load byte, zext to i64
			charGEP := g.tmpReg("sv.idx.gep")
			charLoad := g.tmpReg("sv.idx.val")
			charZext := g.tmpReg("sv.idx.zext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
					g.indent(), charGEP, view.dataPtrReg, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
					g.indent(), charLoad, charGEP))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
					g.indent(), charZext, charLoad))
			}
			return charZext
		}
		// Vec/arr slice: GEP into typed data pointer, load element
		dataTyped := g.tmpReg("sv.idx.typed")
		elemGEP := g.tmpReg("sv.idx.elem")
		elemLoad := g.tmpReg("sv.idx.val")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataTyped, view.dataPtrReg, view.elemType))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
				g.indent(), elemGEP, view.elemType, view.elemType, dataTyped, idx))
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
				g.indent(), elemLoad, view.elemType, view.elemType, elemGEP))
		}
		// Type-extend small integers to i64 for uniform handling
		if view.elemType != "i64" {
			zextReg := g.tmpReg("sv.idx.zext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n",
					g.indent(), zextReg, view.elemType, elemLoad))
			}
			return zextReg
		}
		return elemLoad
	}

	// String indexing: s[i] → extract data ptr from %str-long, then GEP into it
	if varName != "" {
		if t, ok := g.varTypes[varName]; ok && t == "%str-long" {
			strPtr := g.varAddr(varName)
			// Bounds check: load str len and verify idx
			strLen := g.extractStrLen(sb, strPtr)
			g.emitBoundsCheck(sb, idx, strLen)
			dataPtr := g.extractStrDataPtr(sb, strPtr)
			charGEP := g.tmpReg("str-longidx.gep")
			charLoad := g.tmpReg("str-longidx.val")
			charZext := g.tmpReg("str-longidx.zext")
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
			if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
				arrRef = llvmGlobalRef(varName)
			}

			// Bounds check: load arr len and verify idx
			// Skip if the array size is known at compile time and the index is provably in bounds.
			if !g.canSkipBoundsCheck(varName, expr.Index) {
				arrLen := g.emitArrLenLoad(sb, arrRef)
				g.emitBoundsCheck(sb, idx, arrLen)
			}
			dataGEP := g.tmpReg("arr.idx.data.gep")
			dataLoad := g.tmpReg("arr.idx.data")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
					g.indent(), dataGEP, arrRef))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}

			// Bitcast to element type pointer
			dataTyped := g.tmpReg("arr.idx.typed")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
			}

			// GEP to element
			elemGEP := g.tmpReg("arr.idx.elem")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}

			// Load element
			elemLoad := g.tmpReg("arr.idx.val")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
					g.indent(), elemLoad, llvmElemType, llvmElemType, elemGEP))
			}
			// 統一回傳 i64：若元素為較窄整數型別則 zext 到 i64（與 %vec 路徑一致）
			if llvmElemType == "i1" || llvmElemType == "i8" || llvmElemType == "i16" || llvmElemType == "i32" {
				zextReg := g.tmpReg("arr.idx.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, llvmElemType, elemLoad))
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
			if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
				vecRef = llvmGlobalRef(varName)
			}

			// Bounds check: load vec len and verify idx
			// Skip if the array size is known at compile time and the index is provably in bounds.
			if !g.canSkipBoundsCheck(varName, expr.Index) {
				vecLen := g.emitVecLenLoad(sb, vecRef)
				g.emitBoundsCheck(sb, idx, vecLen)
			}

			// Load data pointer from vec struct (field 2)
			dataGEP := g.tmpReg("vec.idx.data.gep")
			dataLoad := g.tmpReg("vec.idx.data")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
					g.indent(), dataGEP, vecRef))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}

			// Bitcast to element type pointer
			dataTyped := g.tmpReg("vec.idx.typed")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
					g.indent(), dataTyped, dataLoad, llvmElemType))
			}

			// GEP to element
			elemGEP := g.tmpReg("vec.idx.elem")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
			}

			// Load element
			elemLoad := g.tmpReg("vec.idx.val")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
					g.indent(), elemLoad, llvmElemType, llvmElemType, elemGEP))
			}
			// Track the SSA type so downstream consumers (e.g. option assignment)
			// can distinguish loaded struct values from pointers.
			if g.ssaTypes != nil && g.isStructLLVMType(llvmElemType) {
				g.ssaTypes[elemLoad] = llvmElemType
			}
			// 當元素型別為整數且小於 i64 時，零擴展至 i64 以與下游消費端（運算、print 等）一致。
			// 注意：struct 型別（如 %str-long）不應 zext。
			if llvmElemType == "i1" || llvmElemType == "i8" || llvmElemType == "i16" || llvmElemType == "i32" {
				zextReg := g.tmpReg("vec.idx.zext")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n",
						g.indent(), zextReg, llvmElemType, elemLoad))
				}
				return zextReg
			}
			// float/double：直接返回元素值（與 %arr 路徑一致）。
			// 下游 varLLVMType 會正確推導為 float/double，避免 bitcast 至 i64
			// 後造成 store float <i64-val>, float* %r 的型別不匹配。
			return elemLoad
		}

		// t is LLVM type like "[4 x i64]" (g.varTypes stores LLVM types)
		// Also handles "[16 x i64]*" (pointer to array, from s = arr2d[i])
		if strings.HasPrefix(t, "[") {
			// Check if it's a pointer to array (e.g. [16 x i64]*)
			if strings.HasSuffix(t, "*") {
				// s is a pointer to an array: load the pointer, then GEP
				arrayType := strings.TrimSuffix(t, "*") // "[16 x i64]"
				elemType := extractArrayElemType(arrayType)
				if elemType != "" {
					llvmElemType = elemType
					arrayLLVMType = arrayType
					// Load the pointer from the alloca
					ptrLoad := g.tmpReg("idx.ptr.load")
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = load %s*, %s** %s\n",
							g.indent(), ptrLoad, arrayType, arrayType, g.varAddr(varName)))
					}
					// GEP into the loaded pointer
					gepReg := g.tmpReg("idx.gep")
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
							g.indent(), gepReg, arrayType, arrayType, ptrLoad, idx))
					}
					// Load element value
					loadReg := g.tmpReg("idx.load")
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
							g.indent(), loadReg, llvmElemType, llvmElemType, gepReg))
					}
					if llvmElemType == "i8" {
						zextReg := g.tmpReg("idx.zext")
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, loadReg))
						}
						return zextReg
					}
					return loadReg
				}
			} else {
				// Raw array type (e.g. [12 x [16 x i64]] for 2D array constants)
				elemType := extractArrayElemType(t)
				if elemType != "" {
					llvmElemType = elemType
					arrayLLVMType = t
					// If the element type is itself an array (nested 2D array),
					// return the GEP pointer without loading (e.g. s = arr2d[i])
					if strings.HasPrefix(elemType, "[") {
						gepReg := g.tmpReg("idx.gep")
						arrRef := llvmVarRef(varName)
						if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
							arrRef = llvmGlobalRef(varName)
						}
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
								g.indent(), gepReg, arrayLLVMType, arrayLLVMType, arrRef, idx))
						}
						return gepReg
					}
				}
			}
		}
	}
	if llvmElemType == "" {
		// Check if this is a []byte (i8*) type
		if t, ok := g.varTypes[varName]; ok && t == "i8*" {
			// []byte parameter: load data pointer from i8** parameter, then GEP
			dataLoad := g.tmpReg("idx.data.load")
			gepReg := g.tmpReg("idx.gep")
			loadReg := g.tmpReg("idx.load")
			zextReg := g.tmpReg("idx.zext")
			if sb != nil {
				dataLoad = g.loadDataPtrField(sb, "%"+varName)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), gepReg, dataLoad, idx))
				sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), loadReg, gepReg))
				sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, loadReg))
			}
			return zextReg
		}
		// []str / []T (any T whose LLVM type ends in *): 載入資料指標、GEP、return %T*（不 load，str 為 struct）
		if t, ok := g.varTypes[varName]; ok && g.isStructLLVMType(t) && strings.HasSuffix(t, "*") {
			elemType := strings.TrimSuffix(t, "*")
			dataLoad := g.tmpReg("idx.data.load")
			gepReg := g.tmpReg("idx.gep")
			if sb != nil {
				// 載入 slice 的資料指標（%T** → %T*）
				sb.WriteString(fmt.Sprintf("%s%s = load %s*, %s** %s\n", g.indent(), dataLoad, elemType, elemType, g.varAddr(varName)))
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
	gepReg := g.tmpReg("idx.gep")
	// Determine the base reference: @name for globals, %name for local allocas.
	arrRef := llvmVarRef(varName)
	if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
		arrRef = llvmGlobalRef(varName)
	}
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
			g.indent(), gepReg, arrayLLVMType, arrayLLVMType, arrRef, idx))
	}

	// Load 元素值（非 i8* 型別的 fallback，如 str 的 i8 元素）
	loadReg := g.tmpReg("idx.load")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
			g.indent(), loadReg, llvmElemType, llvmElemType, gepReg))
	}
	// 統一回傳 i64：若元素為 i8 則 zext 到 i64，與其他索引路徑一致
	if llvmElemType == "i8" {
		zextReg := g.tmpReg("idx.zext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zextReg, loadReg))
		}
		return zextReg
	}
	return loadReg
}

// generateSliceExprIndexRead 處理 slice[start..end][i] 的巢狀索引讀取。
// 內層 slice[start..end] 由 generateSliceExpression 生成為 %slic.N (alloca %vec/%str-long)，
// 外層 [i] 從該結構取出 data 指標後 GEP 到第 i 個元素並 load。
func (g *Generator) generateSliceExprIndexRead(sb *strings.Builder, sliceExpr *parser.SliceExpression, index parser.Expression) string {
	// 生成內層切片表達式，得到一個臨時的 %slic.N (alloca %vec 或 %str-long)
	sliceReg := g.generateSliceExpression(sb, sliceExpr)
	if sliceReg == "" || sliceReg == "0" {
		return "0"
	}

	// 判斷切片結果類型與元素類型
	isStr := false
	elemType := "i64"
	if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
		if t, ok := g.varTypes[ident.Value]; ok {
			if t == "%str-long" {
				isStr = true
				elemType = "i8"
			} else if et, ok := g.arrayElemTypes[ident.Value]; ok {
				elemType = et
			}
		}
	}

	structType := "%vec"
	dataField := uint32(2)
	if isStr {
		structType = "%str-long"
		// %str-long 结构: field 0=len, field 1=cap, field 2=data 指针
		dataField = 2
	}

	// 載入切片長度用於 bounds check
	lenGEP := g.tmpReg("slicidx.len.gep")
	lenReg := g.tmpReg("slicidx.len")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, structType, structType, sliceReg))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
			g.indent(), lenReg, lenGEP))
	}

	// 生成索引並確保為 i64
	idx := g.generateExprWithSB(sb, index)
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(index)
		if idxType != "i64" {
			zextReg := g.tmpReg("slicidx.idx.zext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}

	// bounds check
	g.emitBoundsCheck(sb, idx, lenReg)

	// 載入 data 指標
	dataGEP := g.tmpReg("slicidx.data.gep")
	dataReg := ""
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
			g.indent(), dataGEP, structType, structType, sliceReg, dataField))
		dataReg = g.loadDataPtrField(sb, dataGEP)
	}

	if isStr {
		// 字串：GEP i8*，load i8，zext 到 i64
		charGEP := g.tmpReg("slicidx.char.gep")
		charLoad := g.tmpReg("slicidx.char")
		charZext := g.tmpReg("slicidx.zext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
				g.indent(), charGEP, dataReg, idx))
			sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n",
				g.indent(), charLoad, charGEP))
			sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n",
				g.indent(), charZext, charLoad))
		}
		return charZext
	}

	// vec/arr：bitcast 到元素類型，GEP，load
	dataTyped := g.tmpReg("slicidx.typed")
	elemGEP := g.tmpReg("slicidx.elem")
	elemLoad := g.tmpReg("slicidx.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
			g.indent(), dataTyped, dataReg, elemType))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
			g.indent(), elemGEP, elemType, elemType, dataTyped, idx))
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
			g.indent(), elemLoad, elemType, elemType, elemGEP))
	}
	// 小整數類型 zext 到 i64
	if elemType == "i1" || elemType == "i8" || elemType == "i16" || elemType == "i32" {
		zextReg := g.tmpReg("slicidx.zext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, elemType, elemLoad))
		}
		return zextReg
	}
	return elemLoad
}

// generateStringLiteralIndex 處理字串常量的索引運算（用於模組字串常量傳播後的場景）
// 例如：HEX-LOWER[b >> 4] 在 resolveModuleConstants 後，Left 變成 StringLiteral。
// All strings use %str-long.
func (g *Generator) generateStringLiteralIndex(sb *strings.Builder, lit *parser.StringLiteral, index parser.Expression) string {
	idx := g.generateExprWithSB(sb, index)
	// GEP 索引必須是 i64；若索引為 i8/i16/i32 SSA 值則 zext 到 i64
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(index)
		if idxType != "i64" {
			zextReg := g.tmpReg("str-longlit.idx.zext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, idxType, idx))
			}
			idx = zextReg
		}
	}
	strLen := len(lit.Value)
	strAlloca := g.tmpReg("str-longlit.idx")
	charGEP := g.tmpReg("str-longlit.idx.gep")
	charLoad := g.tmpReg("str-longlit.idx.val")
	zextReg := g.tmpReg("str-longlit.idx.zext")

	if sb == nil {
		return zextReg
	}

	// All strings use %str-long
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strAlloca))
	// field 0: i64 = strLen
	lenGEP := g.tmpReg("str-longlit.idx.len.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, strAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
	// field 1: i64 = cap (strLen)
	capGEP := g.tmpReg("str-longlit.idx.cap.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n",
		g.indent(), capGEP, strAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, capGEP))
	// field 2: i64 = data pointer (address value)
	dataFieldGEP := g.tmpReg("str-longlit.idx.datafield.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
		g.indent(), dataFieldGEP, strAlloca))
	// Emit the literal as a global string
	litIdx := g.stringIdx
	g.stringIdx++
	escaped := g.escapeLLVMString(lit.Value)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", litIdx, strLen, escaped))
	srcPtr := fmt.Sprintf("getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0)",
		strLen, strLen, litIdx)
	g.storeDataPtrField(sb, srcPtr, dataFieldGEP)
	// GEP into the data array
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n",
		g.indent(), charGEP, srcPtr, idx))

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
	leftLen := g.extractLenFromExpr(sb, expr.Left, leftPtr)
	rightLen := g.extractLenFromExpr(sb, expr.Right, rightPtr)

	// Null-terminate both strings: alloca len+1, memcpy data, store 0 at end
	leftBuf := g.tmpReg("strcmp.lbuf")
	leftSize := g.tmpReg("strcmp.lsize")
	leftEnd := g.tmpReg("strcmp.lend")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), leftSize, leftLen))
		sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), leftBuf, leftSize))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), leftBuf, leftData, leftLen))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), leftEnd, leftBuf, leftLen))
		sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), leftEnd))
	}

	rightBuf := g.tmpReg("strcmp.rbuf")
	rightSize := g.tmpReg("strcmp.rsize")
	rightEnd := g.tmpReg("strcmp.rend")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), rightSize, rightLen))
		sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), rightBuf, rightSize))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), rightBuf, rightData, rightLen))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), rightEnd, rightBuf, rightLen))
		sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), rightEnd))
	}

	cmpReg := g.tmpReg("str-longcmp")
	if sb != nil {
		// 使用 @llvm.memcmp 替代 @strcmp（避免 libc 依賴）
		// 兩個 buffer 都已 null-terminated，比較 min(leftLen, rightLen) + 1 bytes
		// （+1 包含 null terminator，等同 strcmp 的行為）
		lenLt := g.tmpReg("strcmp.lenlt")
		sb.WriteString(fmt.Sprintf("%s%s = icmp ult i64 %s, %s\n", g.indent(), lenLt, leftLen, rightLen))
		minLen := g.tmpReg("strcmp.min")
		sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n",
			g.indent(), minLen, lenLt, leftLen, rightLen))
		cmpLen := g.tmpReg("strcmp.cmplen")
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), cmpLen, minLen))
		sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.memcmp(i8* %s, i8* %s, i64 %s)\n",
			g.indent(), cmpReg, leftBuf, rightBuf, cmpLen))
	}

	// memcmp 回傳 0=相等, <0=a<b, >0=a>b（與 strcmp 相同）
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

	resultReg := g.tmpReg("str-longcmpres")
	extReg := g.tmpReg("str-longcmpext")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = icmp %s i32 %s, 0\n", g.indent(), resultReg, cmpOp, cmpReg))
		sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, resultReg))
	}
	return extReg
}

// generateByteCmp 使用整數 icmp 進行 byte 值比較，回傳 zext 後的 i64 結果。
// 適用於 byte 值（如 s[i]）與單字元 StringLiteral（如 '='）的比較，
// 避免誤用 strcmp 將 byte 值當作 %str-long* 指標。
func (g *Generator) generateByteCmp(sb *strings.Builder, expr *parser.InfixExpression) string {
	if sb == nil {
		return "0"
	}

	// 判斷 byte 端與 StringLiteral 端，並記住原始左右順序以正確處理 <, >, <=, >=
	// 透過 isSingleCharStringLit 找出 StringLiteral 端，另一側即為 byte 端。
	var byteSide, strLitSide parser.Expression
	byteOnLeft := false
	if isSingleCharStringLit(expr.Right) {
		byteSide = expr.Left
		strLitSide = expr.Right
		byteOnLeft = true
	} else {
		byteSide = expr.Right
		strLitSide = expr.Left
	}

	// byte 端：產生 i64 值（str 索引已 zext i8 to i64）
	byteVal := g.generateExprWithSB(sb, byteSide)
	// StringLiteral 端：產生 %str-longlit.N 指標，再取出首 byte
	strLitVal := g.generateExprWithSB(sb, strLitSide)
	strLitByte, _ := g.coerceStrLitToByte(sb, strLitVal, strLitSide)
	strLitByteZext := g.tmpReg("bytecmp.zext")
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), strLitByteZext, strLitByte))

	// operator → icmp predicate（與 generateStringCmp 相同語意）
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

	// 依照原始左右順序放置運算元，確保 <, >, <=, >= 語意正確
	var lhs, rhs string
	if byteOnLeft {
		lhs = byteVal
		rhs = strLitByteZext
	} else {
		lhs = strLitByteZext
		rhs = byteVal
	}

	cmpReg := g.tmpReg("bytecmp.cmp")
	extReg := g.tmpReg("bytecmp.ext")
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), cmpReg, cmpOp, lhs, rhs))
	sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
	return extReg
}

// generateStringCmpI1 使用 strcmp 進行字串比較，直接回傳 i1 結果。
// 用於 if/while 條件式中。
func (g *Generator) generateStringCmpI1(sb *strings.Builder, expr *parser.InfixExpression) string {
	leftPtr := g.getStrPtr(sb, expr.Left)
	rightPtr := g.getStrPtr(sb, expr.Right)
	leftData := g.extractDataFromExpr(sb, expr.Left, leftPtr)
	rightData := g.extractDataFromExpr(sb, expr.Right, rightPtr)
	leftLen := g.extractLenFromExpr(sb, expr.Left, leftPtr)
	rightLen := g.extractLenFromExpr(sb, expr.Right, rightPtr)

	// Null-terminate both strings: alloca len+1, memcpy data, store 0 at end
	leftBuf := g.tmpReg("strcmp.lbuf")
	leftSize := g.tmpReg("strcmp.lsize")
	leftEnd := g.tmpReg("strcmp.lend")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), leftSize, leftLen))
		sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), leftBuf, leftSize))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), leftBuf, leftData, leftLen))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), leftEnd, leftBuf, leftLen))
		sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), leftEnd))
	}

	rightBuf := g.tmpReg("strcmp.rbuf")
	rightSize := g.tmpReg("strcmp.rsize")
	rightEnd := g.tmpReg("strcmp.rend")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), rightSize, rightLen))
		sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), rightBuf, rightSize))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), rightBuf, rightData, rightLen))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), rightEnd, rightBuf, rightLen))
		sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), rightEnd))
	}

	cmpReg := g.tmpReg("str-longcmp")
	if sb != nil {
		// 使用 @llvm.memcmp 替代 @strcmp（避免 libc 依賴）
		lenLt := g.tmpReg("strcmp.lenlt")
		sb.WriteString(fmt.Sprintf("%s%s = icmp ult i64 %s, %s\n", g.indent(), lenLt, leftLen, rightLen))
		minLen := g.tmpReg("strcmp.min")
		sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n",
			g.indent(), minLen, lenLt, leftLen, rightLen))
		cmpLen := g.tmpReg("strcmp.cmplen")
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), cmpLen, minLen))
		sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.memcmp(i8* %s, i8* %s, i64 %s)\n",
			g.indent(), cmpReg, leftBuf, rightBuf, cmpLen))
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

	resultReg := g.tmpReg("str-longcmpres")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = icmp %s i32 %s, 0\n", g.indent(), resultReg, cmpOp, cmpReg))
	}
	return resultReg
}

// generateByteCmpI1 使用整數 icmp 進行 byte 值比較，直接回傳 i1 結果。
// 用於 if/while 條件式中 byte vs single-char-string-literal 的比較。
func (g *Generator) generateByteCmpI1(sb *strings.Builder, expr *parser.InfixExpression) string {
	if sb == nil {
		return "0"
	}

	var byteSide, strLitSide parser.Expression
	byteOnLeft := false
	if isSingleCharStringLit(expr.Right) {
		byteSide = expr.Left
		strLitSide = expr.Right
		byteOnLeft = true
	} else {
		byteSide = expr.Right
		strLitSide = expr.Left
	}

	byteVal := g.generateExprWithSB(sb, byteSide)
	strLitVal := g.generateExprWithSB(sb, strLitSide)
	strLitByte, _ := g.coerceStrLitToByte(sb, strLitVal, strLitSide)
	strLitByteZext := g.tmpReg("bytecmp.zext")
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), strLitByteZext, strLitByte))

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

	var lhs, rhs string
	if byteOnLeft {
		lhs = byteVal
		rhs = strLitByteZext
	} else {
		lhs = strLitByteZext
		rhs = byteVal
	}

	cmpReg := g.tmpReg("bytecmp.i1")
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), cmpReg, cmpOp, lhs, rhs))
	return cmpReg
}

// generateByteArith 使用整數 add/sub 進行 byte 值與單字元 StringLiteral 的算術。
// 適用於 c - 'A' 或 c + '0' 等表達式，避免誤用字串拼接。
func (g *Generator) generateByteArith(sb *strings.Builder, expr *parser.InfixExpression) string {
	if sb == nil {
		return "0"
	}

	var byteSide, strLitSide parser.Expression
	byteOnLeft := false
	if isSingleCharStringLit(expr.Right) {
		byteSide = expr.Left
		strLitSide = expr.Right
		byteOnLeft = true
	} else {
		byteSide = expr.Right
		strLitSide = expr.Left
	}

	byteVal := g.generateExprWithSB(sb, byteSide)
	strLitVal := g.generateExprWithSB(sb, strLitSide)
	strLitByte, _ := g.coerceStrLitToByte(sb, strLitVal, strLitSide)
	strLitByteZext := g.tmpReg("bytarith.zext")
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), strLitByteZext, strLitByte))

	var lhs, rhs string
	if byteOnLeft {
		lhs = byteVal
		rhs = strLitByteZext
	} else {
		lhs = strLitByteZext
		rhs = byteVal
	}

	g.tmpIdx++
	switch expr.Operator {
	case "+":
		reg := fmt.Sprintf("%%bytarith.add.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), reg, lhs, rhs))
		return reg
	case "-":
		reg := fmt.Sprintf("%%bytarith.sub.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n", g.indent(), reg, lhs, rhs))
		return reg
	}
	return "0"
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
					tagGEP := g.tmpReg("opt.cmp.gep")
					tagLoad := g.tmpReg("opt.cmp.load")
					cmpReg := g.tmpReg("cmp.i1")
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
				// str-long == nil: compare data pointer (field 2) to 0 (null).
				// A %str-long with data == 0 is considered "nil" (e.g. getenv
				// returning NULL, or an uninitialized string).
				if t, ok := g.varTypes[leftIdent.Value]; ok && t == "%str-long" && tag == 1 {
					dataGEP := g.tmpReg("strnil.data.gep")
					dataLoad := g.tmpReg("strnil.data.load")
					cmpReg := g.tmpReg("cmp.i1")
					cmpOp := "eq"
					if expr.Operator == "!=" {
						cmpOp = "ne"
					}
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, g.varAddr(leftIdent.Value)))
						sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataLoad, dataGEP))
						sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, 0\n", g.indent(), cmpReg, cmpOp, dataLoad))
					}
					return cmpReg
				}
			}
		}
	}

	// byte vs single-char-string-literal: 使用整數比較（非 strcmp），回傳 i1
	if (isSingleCharStringLit(expr.Left) && !g.isStringExpr(expr.Right)) ||
		(isSingleCharStringLit(expr.Right) && !g.isStringExpr(expr.Left)) {
		switch expr.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return g.generateByteCmpI1(sb, expr)
		}
	}

	// 字串比較：使用 strcmp 直接回傳 i1
	// 注意：str == nil 已在前面處理，不會走到這裡
	if (g.isStringExpr(expr.Left) || g.isStringExpr(expr.Right)) &&
		!isNilLiteral(expr.Left) && !isNilLiteral(expr.Right) {
		switch expr.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return g.generateStringCmpI1(sb, expr)
		}
	}

	left := g.generateExprWithSB(sb, expr.Left)
	right := g.generateExprWithSB(sb, expr.Right)
	reg := g.tmpReg("cmp.i1")
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
				cvtReg := g.tmpReg("fpcvt")
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
			cvtReg := g.tmpReg("sitofp")
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
	// or, for non-Identifier receivers (DotExpression/IndexExpression/...),
	// by deriving the LLVM type via exprResultLLVMType and the pointer via generateExprPtr.
	varName := ""
	recvType := ""
	recvPtr := ""
	if ident, ok := expr.Left.(*parser.Identifier); ok {
		varName = ident.Value
		if g.varTypes != nil {
			if t, ok := g.varTypes[varName]; ok {
				recvType = t
			}
		}
		// Slice view as base for inline slicing: materialize to temporary struct
		if g.isSliceViewVar(varName) && sb != nil {
			recvPtr = g.materializeSliceView(sb, varName)
		} else {
			recvPtr = g.varAddr(varName)
		}
	} else {
		// Non-Identifier receiver: derive type and pointer generically.
		recvType = g.exprResultLLVMType(expr.Left)
		recvPtr = g.generateExprPtr(sb, expr.Left)
	}

	isVec := recvType == "%vec"
	isArr := recvType == "%arr"
	isStr := recvType == "%str-long"

	// Detect inline array field (e.g. .client-mac-key is [32 x i8], not %arr struct).
	// exprResultLLVMType maps these to "%arr", so we check the raw field type.
	inlineArrayType := ""
	if dot, ok := expr.Left.(*parser.DotExpression); ok {
		if recvType2 := g.exprResultLLVMType(dot.Receiver); g.isStructLLVMType(recvType2) {
			structName2 := strings.TrimPrefix(recvType2, "%")
			if fields, ok := g.structTypes[structName2]; ok {
				for _, f := range fields {
					if f.name == dot.Property {
						if strings.HasPrefix(f.typ, "[") {
							inlineArrayType = f.typ
						}
						break
					}
				}
			}
		}
	}
	// Result parameters and locals with raw LLVM array type (e.g. [16 x i8])
	// are not %arr structs — treat them like inline arrays for slicing.
	if inlineArrayType == "" && strings.HasPrefix(recvType, "[") {
		inlineArrayType = recvType
	}

	if !isVec && !isArr && !isStr && inlineArrayType == "" {
		sb.WriteString(fmt.Sprintf("%s; slice expression (non-vec/arr/str): %s\n", g.indent(), leftVal))
		return "0"
	}

	g.tmpIdx++
	tid := g.tmpIdx
	resultType := "%vec"
	if isStr {
		resultType = "%str-long"
	}
	resultReg := fmt.Sprintf("%%slic.%d", tid)

	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), resultReg, resultType))
	}

	// Variables for source fields
	var srcLen, srcData, srcCap string

	if inlineArrayType != "" {
		// Inline array field (e.g. [32 x i8]): recvPtr points directly to [N x T],
		// not to an %arr struct. Extract N from the type string and bitcast to i8*.
		// Parse "[32 x i8]" → N=32, elemType=i8
		closeBracket := strings.IndexByte(inlineArrayType, ']')
		if closeBracket < 0 {
			sb.WriteString(fmt.Sprintf("%s; ERROR: cannot parse inline array type %s\n", g.indent(), inlineArrayType))
			return "0"
		}
		inner := inlineArrayType[1:closeBracket] // "32 x i8"
		sepIdx := strings.Index(inner, " x ")
		if sepIdx < 0 {
			return "0"
		}
		sizeStr := inner[:sepIdx]          // "32"
		inlineElemType := inner[sepIdx+3:] // "i8"

		// srcLen = N (constant)
		srcLen = sizeStr
		srcCap = sizeStr

		// srcData = bitcast [N x T]* to i8*
		g.tmpIdx++
		srcData = fmt.Sprintf("%%slice.inlinedata.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n",
				g.indent(), srcData, inlineArrayType, recvPtr))
		}

		// Track elem type for offset calculation
		if g.arrayElemTypes != nil {
			// Use a synthetic key to propagate elem type into the elemSize logic below
			// We'll set varName to a sentinel that arrayElemTypes can resolve
			if _, ok := g.arrayElemTypes["__inline_slice__"]; !ok {
				g.arrayElemTypes["__inline_slice__"] = inlineElemType
			}
			varName = "__inline_slice__"
		}
	} else {
		// Determine struct type name for source GEPs
		structType := "%arr"
		if isVec {
			structType = "%vec"
		} else if isStr {
			structType = "%str-long"
		}

		// Data field index: %arr → field 1, %vec → field 2, %str-long → field 2
		// (%arr = {i64 len, i64 data}; %vec/%str-long = {i64 len, i64 cap, i64 data})
		dataField := uint32(1)
		if isVec || isStr {
			dataField = 2
		}

		// Load source len (field 0 for both %arr and %vec)
		srcLenGEP := g.tmpReg("slice.srclen.gep")
		g.tmpIdx++
		srcLen = fmt.Sprintf("%%slice.srclen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
				g.indent(), srcLenGEP, structType, structType, recvPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
				g.indent(), srcLen, srcLenGEP))
		}

		// Load source data pointer
		srcDataGEP := g.tmpReg("slice.srcdata.gep")
		g.tmpIdx++
		srcData = fmt.Sprintf("%%slice.srcdata.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), srcDataGEP, structType, structType, recvPtr, dataField))
			srcData = g.loadDataPtrField(sb, srcDataGEP)
		}

		// Source capacity: for %vec/%str-long load from field 1, for %arr use len
		g.tmpIdx++
		srcCap = fmt.Sprintf("%%slice.srccap.%d", g.tmpIdx)
		if isVec || isStr {
			srcCapGEP := g.tmpReg("slice.srccap.gep")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
					g.indent(), srcCapGEP, structType, structType, recvPtr))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n",
					g.indent(), srcCap, srcCapGEP))
			}
		} else {
			// %arr has no cap field; use len as cap
			srcCap = srcLen
		}
	}

	// Compute start: 0 if no start, else compile(start) + (1 if ( exclusive)
	startReg := "0"
	if r != nil && r.Start != nil {
		startVal := g.generateExprWithSB(sb, r.Start)
		if !r.LeftInc {
			// ( exclusive: start = start + 1
			startPlus := g.tmpReg("vec.start.plus")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n",
					g.indent(), startPlus, startVal))
			}
			startReg = startPlus
		} else {
			startReg = startVal
		}
	} else if r != nil && r.Start == nil && !r.LeftInc {
		// (..end / (..] / (..) / (.. : 左開無下限，起始偏移 +1
		startReg = "1"
	}

	// Compute new len
	var newLenReg string
	if r == nil || (r.Start == nil && r.End == nil) {
		// Full slice: [..] / [..) / (..] / (..)
		// right-inclusive (]) → newLen = srcLen - start
		// right-exclusive ()) → newLen = srcLen - start - 1 (排除最後一個元素)
		rightExcl := r != nil && !r.RightInc
		if startReg == "0" && !rightExcl {
			newLenReg = srcLen
		} else {
			g.tmpIdx++
			newLenReg = fmt.Sprintf("%%vec.newlen.%d", g.tmpIdx)
			if sb != nil {
				if startReg == "0" {
					// right-exclusive only: newLen = srcLen - 1
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n",
						g.indent(), newLenReg, srcLen))
				} else if rightExcl {
					// left-exclusive + right-exclusive: newLen = srcLen - start - 1
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
						g.indent(), newLenReg, srcLen, startReg))
					extraSub := g.tmpReg("vec.newlen")
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n",
						g.indent(), extraSub, newLenReg))
					newLenReg = extraSub
				} else {
					// left-exclusive only: newLen = srcLen - start
					sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
						g.indent(), newLenReg, srcLen, startReg))
				}
			}
		}
	} else if r.Start == nil && r.End != nil {
		// [..end] / (..end]: new_len = end - start + (1 if ] else 0)
		endVal := g.generateExprWithSB(sb, r.End)
		subReg := g.tmpReg("vec.sublen")
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
		subReg := g.tmpReg("vec.sublen")
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
	newCapReg := g.tmpReg("vec.newcap")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, %s\n",
			g.indent(), newCapReg, srcCap, startReg))
	}

	// Compute new data pointer: GEP on i8* with byte offset
	// byte offset = start * elem_size (default 8 for i64, 1 for str)
	elemSize := int64(8)
	if isStr {
		elemSize = 1
	} else if varName != "" {
		if elemType, ok := g.arrayElemTypes[varName]; ok {
			switch elemType {
			case "i8", "i16", "i32", "i64":
				if s := g.llvmTypeSize(elemType); s > 0 {
					elemSize = s
				}
			}
		}
	}
	offsetReg := g.tmpReg("vec.offset")
	if sb != nil {
		if startReg == "0" {
			offsetReg = "0"
		} else {
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n",
				g.indent(), offsetReg, startReg, elemSize))
		}
	}

	newDataReg := g.tmpReg("vec.newdata")
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
	dstLenGEP := g.tmpReg("slic.dstlen.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
			g.indent(), dstLenGEP, resultType, resultType, resultReg))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n",
			g.indent(), newLenReg, dstLenGEP))
	}

	if resultType == "%vec" || resultType == "%str-long" {
		// Store new cap (field 1) — %vec 和 %str-long 都有 cap 字段
		dstCapGEP := g.tmpReg("slic.dstcap.gep")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 1\n",
				g.indent(), dstCapGEP, resultType, resultType, resultReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n",
				g.indent(), newCapReg, dstCapGEP))
		}
	}

	// Store new data
	// %arr: field 1, %vec: field 2, %str-long: field 2
	// (%arr = {i64 len, i64 data}; %vec/%str-long = {i64 len, i64 cap, i64 data})
	// 注意：inline array 切片結果為 %vec，data 也應寫入 field 2，
	// 不能用 isVec/isStr 判斷（它們對 inline array 為 false）。
	dstDataField := uint32(1)
	if resultType == "%vec" || resultType == "%str-long" {
		dstDataField = 2
	}
	dstDataGEP := g.tmpReg("slic.dstdata.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
			g.indent(), dstDataGEP, resultType, resultType, resultReg, dstDataField))
		g.storeDataPtrField(sb, newDataReg, dstDataGEP)
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
					reg := g.tmpReg("optcmp.zext")
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), reg, i1Result))
					}
					return reg
				}
				// str-long == nil: delegate to generateInfixI1 and zext to i64
				// (needed when comparison appears inside && or ||, not just as if condition)
				if t, ok := g.varTypes[leftIdent.Value]; ok && t == "%str-long" && tag == 1 {
					i1Result := g.generateInfixI1(sb, expr)
					reg := g.tmpReg("strnilcmp.zext")
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
					cvtReg := g.tmpReg("fpcvt")
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
				cvtReg := g.tmpReg("sitofp")
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

	// byte vs single-char-string-literal: 使用整數比較（非 strcmp）
	// 例如 s[i] == '=' 其中 s[i] 是 byte 值，'=' 是單字元 StringLiteral
	// 也涵蓋 c <= 'Z' 其中 c 是 i64 變數（持有 byte 值）的情況。
	if (isSingleCharStringLit(expr.Left) && !g.isStringExpr(expr.Right)) ||
		(isSingleCharStringLit(expr.Right) && !g.isStringExpr(expr.Left)) {
		switch expr.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return g.generateByteCmp(sb, expr)
		case "+", "-":
			return g.generateByteArith(sb, expr)
		}
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
				lReg := g.tmpReg("inc.ld")
				rReg := g.tmpReg("inc")
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
				lReg := g.tmpReg("dec.ld")
				rReg := g.tmpReg("dec")
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
			reg := g.tmpReg("fadd.tmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fadd %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("add.tmp")
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
			reg := g.tmpReg("fsub.tmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fsub %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("sub.tmp")
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
			reg := g.tmpReg("fmul.tmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fmul %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("mul.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = mul %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "/":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			reg := g.tmpReg("fdiv.tmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fdiv %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("div.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sdiv %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "%":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			ld := coerceToFloat(left, expr.Left, ft)
			rd := coerceToFloat(right, expr.Right, ft)
			reg := g.tmpReg("fmod.tmp")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = frem %s %s, %s\n", g.indent(), reg, ft, ld, rd))
			}
			return reg
		}
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("mod.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = srem %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "==":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			cmpReg := g.tmpReg("eq.cmp")
			extReg := g.tmpReg("eq.ext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp oeq %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		cmpReg := g.tmpReg("eq.cmp")
		extReg := g.tmpReg("eq.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "!=":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			cmpReg := g.tmpReg("ne.cmp")
			extReg := g.tmpReg("ne.ext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp one %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		cmpReg := g.tmpReg("ne.cmp")
		extReg := g.tmpReg("ne.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "<":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			cmpReg := g.tmpReg("lt.cmp")
			extReg := g.tmpReg("lt.ext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp olt %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		cmpReg := g.tmpReg("lt.cmp")
		extReg := g.tmpReg("lt.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case ">":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			cmpReg := g.tmpReg("gt.cmp")
			extReg := g.tmpReg("gt.ext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp ogt %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		cmpReg := g.tmpReg("gt.cmp")
		extReg := g.tmpReg("gt.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "<=":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			cmpReg := g.tmpReg("le.cmp")
			extReg := g.tmpReg("le.ext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp ole %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		cmpReg := g.tmpReg("le.cmp")
		extReg := g.tmpReg("le.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sle %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case ">=":
		if ft := floatArithType(expr.Left, expr.Right); ft != "" {
			lc := coerceToFloat(left, expr.Left, ft)
			rc := coerceToFloat(right, expr.Right, ft)
			cmpReg := g.tmpReg("ge.cmp")
			extReg := g.tmpReg("ge.ext")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = fcmp oge %s %s, %s\n", g.indent(), cmpReg, ft, lc, rc))
				sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
			}
			return extReg
		}
		cmpType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, cmpType)
		rc := g.coerceToInt(sb, right, expr.Right, cmpType)
		cmpReg := g.tmpReg("ge.cmp")
		extReg := g.tmpReg("ge.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge %s %s, %s\n", g.indent(), cmpReg, cmpType, lc, rc))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	case "|":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("or.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = or %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "&":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("and.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = and %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "^":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("xor.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = xor %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "<<":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("shl.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = shl %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case ">>":
		arithType := g.arithLLVMType(expr.Left, expr.Right)
		lc := g.coerceToInt(sb, left, expr.Left, arithType)
		rc := g.coerceToInt(sb, right, expr.Right, arithType)
		reg := g.tmpReg("shr.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = lshr %s %s, %s\n", g.indent(), reg, arithType, lc, rc))
		}
		return reg
	case "&&":
		// 邏輯 AND：將 i1 運算元 zext 到 i64（如 str.empty() 返回 i1），
		// 比較結果已是 i64，保持不變。然後使用 and 指令。
		leftI64 := g.zextBoolToI64(sb, left, expr.Left)
		rightI64 := g.zextBoolToI64(sb, right, expr.Right)
		reg := g.tmpReg("land.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, %s\n", g.indent(), reg, leftI64, rightI64))
		}
		return reg
	case "||":
		// 邏輯 OR：同上，將 i1 運算元 zext 到 i64 後使用 or 指令。
		leftI64 := g.zextBoolToI64(sb, left, expr.Left)
		rightI64 := g.zextBoolToI64(sb, right, expr.Right)
		reg := g.tmpReg("lor.tmp")
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
			if t, ok := g.varTypes[e.Value]; ok && (t == "%str-long") {
				return true
			}
		}
	case *parser.InfixExpression:
		if e.Operator == "*" {
			// str repeat: 'str' * n — no byte arithmetic semantics for *
			return g.isStringExpr(e.Left)
		}
		if e.Operator == "-" || e.Operator == "+" {
			// A single-char StringLiteral paired with a non-string operand is
			// byte arithmetic (e.g., c - 'A'), not string concatenation.
			if isSingleCharStringLit(e.Left) && !g.isStringExpr(e.Right) {
				return false
			}
			if isSingleCharStringLit(e.Right) && !g.isStringExpr(e.Left) {
				return false
			}
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	}
	// Fallback: use exprResultLLVMType for DotExpression and other complex expressions
	t := g.exprResultLLVMType(expr)
	return t == "%str-long"
}

// isByteValueExpr checks if an expression produces a byte value (i8/i64 from str
// indexing or a byte-typed variable). Used to dispatch byte vs single-char-string
// comparisons (e.g. s[i] == '=') to integer icmp instead of strcmp.
func (g *Generator) isByteValueExpr(expr parser.Expression) bool {
	switch e := expr.(type) {
	case *parser.CharLiteral:
		return true
	case *parser.IndexExpression:
		if ident, ok := e.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok && (t == "%str-long") {
					return true
				}
			}
		}
		return false
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[e.Value]; ok && t == "i8" {
				return true
			}
		}
		return false
	}
	return false
}

// isSingleCharStringLit checks if an expression is a single-character string literal.
func isSingleCharStringLit(expr parser.Expression) bool {
	if lit, ok := expr.(*parser.StringLiteral); ok {
		return len(lit.Value) == 1
	}
	return false
}

// isNilLiteral reports whether expr is a *parser.NilLiteral.
func isNilLiteral(expr parser.Expression) bool {
	_, ok := expr.(*parser.NilLiteral)
	return ok
}

// getStrPtr returns the %str-long* pointer for a string expression.
func (g *Generator) getStrPtr(sb *strings.Builder, expr parser.Expression) string {
	if ident, ok := expr.(*parser.Identifier); ok {
		return g.varAddr(ident.Value)
	}
	// IndexExpression（如 names[i]）：使用 generateExprPtr 取得元素指標，
	// 避免 load 出 %str-long value 後無法 GEP 存取 len/data 欄位。
	if _, ok := expr.(*parser.IndexExpression); ok {
		if ptr := g.generateExprPtr(sb, expr); ptr != "" {
			return ptr
		}
	}
	// DotExpression / 其他表達式：生成值後物化到臨時 alloca 取得指標。
	// extractStrLen/extractStrDataPtr 需要的是 %str-long* 指標，而非載入的值。
	val := g.generateExprWithSB(sb, expr)
	if val == "" {
		if os.Getenv("NOLANG_DEBUG_STRPTR") != "" {
			// Debug: identify which expression produces empty value in string context
			exprStr := ""
			if ce, ok := expr.(*parser.CallExpression); ok {
				exprStr = fmt.Sprintf("CallExpression fn=%v args=%d", ce.Function, len(ce.Arguments))
			} else {
				exprStr = fmt.Sprintf("%T: %v", expr, expr)
			}
			fmt.Fprintf(os.Stderr, "[debug-getStrPtr] empty val from expr: %s\n", exprStr)
		}
		return val
	}
	if strings.HasPrefix(val, "@") {
		return val
	}
	et := g.exprResultLLVMType(expr)
	// 若 exprResultLLVMType 無法推導型別（如 counts[i].to-str() 這類
	// receiver 為 IndexExpression 的方法呼叫，flattenDottedExpr 返回 ""），
	// 改查 ssaTypes — call.go 的 voidSingleOutput 路徑會記錄返回型別。
	if et == "" && g.ssaTypes != nil {
		if ssaType, ok := g.ssaTypes[val]; ok {
			et = ssaType
		}
	}
	if et == "%str-long" {
		// If val is a %str-long* pointer (from slice expression, concat, etc.),
		// load the %str-long value before storing to temp alloca.
		if g.isStrPtrReg(val) {
			loadReg := g.tmpReg("str-long.load")
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, val))
			val = loadReg
		}
		tmpAlloca := g.tmpReg("strptr.tmp")
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
		sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), val, tmpAlloca))
		return tmpAlloca
	}
	return val
}

// getStrType returns the LLVM type string for a string expression.
func (g *Generator) getStrType(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return "%str-long"
	case *parser.Identifier:
		if t, ok := g.varTypes[e.Value]; ok {
			return t
		}
	case *parser.InfixExpression:
		if e.Operator == "-" || e.Operator == "+" || e.Operator == "*" {
			return "%str-long" // concat/repeat results are always %str-long
		}
	}
	return "%str-long"
}

// extractLenFromExpr extracts i64 len from a string expression.
func (g *Generator) extractLenFromExpr(sb *strings.Builder, expr parser.Expression, ptr string) string {
	return g.extractStrLen(sb, ptr)
}

// extractDataFromExpr extracts i8* data pointer from a string expression.
func (g *Generator) extractDataFromExpr(sb *strings.Builder, expr parser.Expression, ptr string) string {
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
					return g.extractStrLen(sb, g.varAddr(a.Value))
				}
				// Option variable with string inner type (e.g. ?str):
				// extract the inner %str-long* pointer from
				// the option's data field, then get the string length.
				if t == "%option" && g.optionInnerTypes != nil {
					if innerType, ok := g.optionInnerTypes[a.Value]; ok {
						if innerType == "%str-long" {
							ptr := g.generateExprWithSB(sb, a)
							return g.extractStrLen(sb, ptr)
						}

					}
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
			tmpAlloca := g.tmpReg("strlen.dot")
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
			return g.extractStrLen(sb, tmpAlloca)
		}
	case *parser.IndexExpression:
		// String element from a []str slice (e.g. fields[i]).
		// generateIndexExpression loads the %str-long value; materialize it
		// into a temp alloca to obtain a %str-long* for length extraction.
		if ident, ok := a.Left.(*parser.Identifier); ok {
			if et, ok := g.arrayElemTypes[ident.Value]; ok && et == "%str-long" {
				ptr := g.generateExprWithSB(sb, a)
				tmpAlloca := g.tmpReg("strlen.idx")
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
				sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
				return g.extractStrLen(sb, tmpAlloca)
			}
		}
	case *parser.CallExpression:
		// Function/method call returning %str-long (e.g. n.to-str()).
		// exprResultLLVMType determines the return type; if it's %str-long,
		// materialize the call result into a temp alloca and extract len.
		et := g.exprResultLLVMType(a)
		if et == "%str-long" {
			ptr := g.generateExprWithSB(sb, a)
			// If ptr is already a %str-long* (e.g. from args-get alloca),
			// use it directly instead of trying to store a value.
			if g.isStrPtrReg(ptr) {
				return g.extractStrLen(sb, ptr)
			}
			tmpAlloca := g.tmpReg("strlen.call")
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
			return g.extractStrLen(sb, tmpAlloca)
		}
	}
	return "0"
}

// byteToSingleCharStr converts a byte value expression (e.g. from str indexing
// like line[i]) to a single-character %str-long* string. The byte value from
// generateIndexExpression is i64 (zext'd from i8); we trunc it back, store it
// into a 2-byte buffer (char + null), and build a %str-long struct {len=1, data}.
func (g *Generator) byteToSingleCharStr(sb *strings.Builder, expr parser.Expression) string {
	byteVal := g.generateExprWithSB(sb, expr)
	// Truncate i64 (zext'd byte) back to i8.
	byteI8 := g.tmpReg("concat.byte.i8")
	sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i8\n", g.indent(), byteI8, byteVal))
	// Allocate 2-byte buffer: char + null terminator.
	bufPtr := g.tmpReg("concat.byte.buf")
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 2)\n", g.indent(), bufPtr))
	sb.WriteString(fmt.Sprintf("%sstore i8 %s, i8* %s\n", g.indent(), byteI8, bufPtr))
	nullPos := g.tmpReg("concat.byte.null")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 1\n", g.indent(), nullPos, bufPtr))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))
	// Build %str-long struct: { len=1, cap=2, data=buf }.
	// alloca hoisted to entry block to avoid stack growth in tight loops;
	// safe vs dirty data: all 3 fields (len/cap/data) are fully overwritten below.
	resultAlloca := g.tmpReg("concat.byte.str")
	g.emitEntryAlloca(sb, "%s = alloca %%str-long\n", resultAlloca)
	lenGEP := g.tmpReg("concat.byte.len")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 1, i64* %s\n", g.indent(), lenGEP))
	capGEP := g.tmpReg("concat.byte.cap")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 2, i64* %s\n", g.indent(), capGEP))
	dataGEP := g.tmpReg("concat.byte.data")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, resultAlloca))
	g.storeDataPtrField(sb, bufPtr, dataGEP)
	return resultAlloca
}

// generateStrConcat generates LLVM IR for string concatenation using `-` operator.
func (g *Generator) generateStrConcat(sb *strings.Builder, leftExpr, rightExpr parser.Expression) string {
	if sb == nil {
		return "%str-longconcat.null"
	}

	// Byte operands (e.g. from str indexing like line[i]) must be converted to
	// single-character %str-long strings before concatenation, otherwise
	// getStrPtr would return the raw i64 byte value which extractLen/extractData
	// would mistakenly treat as a %str-long* pointer.
	var leftPtr, rightPtr string
	if g.isByteValueExpr(leftExpr) {
		leftPtr = g.byteToSingleCharStr(sb, leftExpr)
	} else {
		leftPtr = g.getStrPtr(sb, leftExpr)
	}
	if g.isByteValueExpr(rightExpr) {
		rightPtr = g.byteToSingleCharStr(sb, rightExpr)
	} else {
		rightPtr = g.getStrPtr(sb, rightExpr)
	}

	leftLen := g.extractLenFromExpr(sb, leftExpr, leftPtr)
	rightLen := g.extractLenFromExpr(sb, rightExpr, rightPtr)
	leftData := g.extractDataFromExpr(sb, leftExpr, leftPtr)
	rightData := g.extractDataFromExpr(sb, rightExpr, rightPtr)

	totalLen := g.tmpReg("concat.total")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), totalLen, leftLen, rightLen))

	allocSize := g.tmpReg("concat.alloc")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), allocSize, totalLen))

	bufPtr := g.tmpReg("concat.buf")
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufPtr, allocSize))

	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), bufPtr, leftData, leftLen))

	dstOffset := g.tmpReg("concat.dst")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), dstOffset, bufPtr, leftLen))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), dstOffset, rightData, rightLen))

	nullPos := g.tmpReg("concat.null")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nullPos, bufPtr, totalLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))

	// alloca hoisted to entry block to avoid stack growth in tight loops;
	// safe vs dirty data: all 3 fields (len/cap/data) are fully overwritten below.
	resultAlloca := g.tmpReg("concat.result")
	g.emitEntryAlloca(sb, "%s = alloca %%str-long\n", resultAlloca)

	lenGEP := g.tmpReg("concat.len.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	capGEP := g.tmpReg("concat.cap.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, capGEP))

	dataGEP := g.tmpReg("concat.data.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, resultAlloca))
	g.storeDataPtrField(sb, bufPtr, dataGEP)

	// 注册为语句级临时堆对象：若未绑定变量，由 generateStatement 在语句结束前 free data。
	// 若被 generateLet 绑定到变量，untrackStmtTemporary 会移除（由 heapVars 接管）。
	g.stmtTemporaries = append(g.stmtTemporaries, resultAlloca)
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
	totalLen := g.tmpReg("repeat.total")
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %s\n", g.indent(), totalLen, strLen, countReg))

	// Allocate memory for result (totalLen + 1 for null terminator)
	allocSize := g.tmpReg("repeat.alloc")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), allocSize, totalLen))

	bufPtr := g.tmpReg("repeat.buf")
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufPtr, allocSize))

	// Loop to copy string data count times
	// We'll use a simple loop: for i in 0..count, memcpy(strData, buf + i*strLen, strLen)
	g.tmpIdx++
	loopStart := fmt.Sprintf("repeat.loop.start.%d", g.tmpIdx)
	loopBody := fmt.Sprintf("repeat.loop.body.%d", g.tmpIdx)
	loopEnd := fmt.Sprintf("repeat.loop.end.%d", g.tmpIdx)

	// Initialize counter i = 0
	// alloca hoisted to entry block to avoid stack growth in tight loops;
	// safe vs dirty data: store i64 0 below fully overwrites the i64 slot.
	iReg := g.tmpReg("repeat.i")
	g.emitEntryAlloca(sb, "%s = alloca i64\n", iReg)
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), iReg))

	// Jump to loop start
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopStart))

	// Loop start: check if i < count
	g.emitLabel(sb, loopStart)
	iVal := g.tmpReg("repeat.i.val")
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), iVal, iReg))
	cmp := g.tmpReg("repeat.cmp")
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmp, iVal, countReg))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), cmp, loopBody, loopEnd))

	// Loop body: copy string data to buf + i*strLen
	g.emitLabel(sb, loopBody)
	offset := g.tmpReg("repeat.offset")
	sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %s\n", g.indent(), offset, iVal, strLen))
	dstPtr := g.tmpReg("repeat.dst")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), dstPtr, bufPtr, offset))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), dstPtr, strData, strLen))

	// Increment i
	iNext := g.tmpReg("repeat.i.next")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), iNext, iReg))
	sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), loopStart))

	// Loop end
	g.emitLabel(sb, loopEnd)

	// Add null terminator at buf[totalLen]
	nullPos := g.tmpReg("repeat.null")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nullPos, bufPtr, totalLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))

	// Create result %str-long
	resultAlloca := g.tmpReg("repeat.result")
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), resultAlloca))

	lenGEP := g.tmpReg("repeat.len.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	capGEP := g.tmpReg("repeat.cap.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, capGEP))

	dataGEP := g.tmpReg("repeat.data.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, resultAlloca))
	g.storeDataPtrField(sb, bufPtr, dataGEP)

	// 注册为语句级临时堆对象：若未绑定变量，由 generateStatement 在语句结束前 free data。
	g.stmtTemporaries = append(g.stmtTemporaries, resultAlloca)
	return resultAlloca
}

// resolveAsyncCallInfo extracts the LLVM function name, argument LLVM types,
// and result LLVM type from a CallExpression used in a `run` expression.
func (g *Generator) resolveAsyncCallInfo(call *parser.CallExpression) (fnName string, argTypes []string, resultType string) {
	// Direct function call: fetch-async(url)
	if ident, ok := call.Function.(*parser.Identifier); ok {
		fnName = ident.Value
	}
	// Method call: conn.query-async(sql) — resolve receiver type
	if dot, ok := call.Function.(*parser.DotExpression); ok {
		if recv, ok := dot.Receiver.(*parser.Identifier); ok {
			if recvType, ok := g.varTypes[recv.Value]; ok {
				srcType := strings.TrimPrefix(recvType, "%")
				candidates := []string{srcType}
				if primAliases, ok := llvmTypeToNolang[srcType]; ok {
					candidates = append(candidates, primAliases...)
				}
				for _, cand := range candidates {
					shortName := cand + "." + dot.Property
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						break
					}
				}
			}
		}
	}
	if fnName == "" {
		return
	}
	// Get argument types (includes receiver for methods)
	if types, ok := g.funcParamLLVMTypes[fnName]; ok {
		argTypes = types
	}
	// Get result type (single-result functions)
	if results, ok := g.funcResultLLVMType[fnName]; ok && len(results) >= 1 {
		resultType = results[0]
	}
	if resultType == "" {
		resultType = "i64"
	}
	return
}

// isAsyncCall checks if a CallExpression calls an -async function.
func (g *Generator) isAsyncCall(expr *parser.CallExpression) bool {
	fnName := ""
	if ident, ok := expr.Function.(*parser.Identifier); ok {
		fnName = ident.Value
	} else if dot, ok := expr.Function.(*parser.DotExpression); ok {
		fnName = dot.Property
	}
	if fnName == "" {
		return false
	}
	return strings.HasSuffix(fnName, "-async")
}

// isFutureVar checks if a variable holds a %future value.
func (g *Generator) isFutureVar(varName string) bool {
	if g.futureResultTypes == nil {
		return false
	}
	_, ok := g.futureResultTypes[varName]
	return ok
}

// isTaskVar checks if a variable holds a %task value.
func (g *Generator) isTaskVar(varName string) bool {
	if g.taskResultTypes == nil {
		return false
	}
	_, ok := g.taskResultTypes[varName]
	return ok
}

// prepareAsyncCall generates the wrapper function, allocates result buffer and args struct,
// and packs arguments. Shared by generateFutureCreation and generateRunExpression.
// Returns: (wrapperName, argsBitcast, resultPtrCast, resultType)
func (g *Generator) prepareAsyncCall(sb *strings.Builder, call *parser.CallExpression) (string, string, string, string) {
	fnName, argTypes, resultType := g.resolveAsyncCallInfo(call)
	if fnName == "" {
		return "", "", "", ""
	}

	// Build the list of argument expressions (receiver + explicit args for methods)
	var argExprs []parser.Expression
	if dot, ok := call.Function.(*parser.DotExpression); ok {
		argExprs = append(argExprs, dot.Receiver)
	}
	argExprs = append(argExprs, call.Arguments...)

	// Build the args struct type string: { i8*, i8*, ... } (result_ptr + all arg ptrs)
	numFields := len(argExprs) + 1
	argsTypeStr := "{ i8*"
	for i := 1; i < numFields; i++ {
		argsTypeStr += ", i8*"
	}
	argsTypeStr += " }"

	// Generate unique wrapper number
	g.tmpIdx++
	wrapperNum := g.tmpIdx
	wrapperName := fmt.Sprintf("async_wrapper.%d", wrapperNum)

	// Resolve LLVM function name (handle clib prefix)
	llvmFnName := fnName
	if clibFuncNames[fnName] {
		llvmFnName = "n." + fnName
	}
	sanitizedFnName := sanitizeLLVMName(llvmFnName)

	// === Generate wrapper function (to g.asyncWrappers) ===
	// wrapper 签名：void @wrapper(i8* %task_ptr)
	// 从 task_ptr 获取 args (field 1)，执行目标函数，设置 done=true (field 2)
	// 开头检查 done：若已完成则直接返回，避免事件循环重复调度时二次执行目标函数。
	w := &g.asyncWrappers
	w.WriteString(fmt.Sprintf("define void @%s(i8* %%task_ptr) {\n", wrapperName))
	w.WriteString("entry:\n")
	w.WriteString(fmt.Sprintf("\t%%w.task.%d = bitcast i8* %%task_ptr to %%task*\n", wrapperNum))
	// 检查 done (field 2)，已完成则跳过执行
	w.WriteString(fmt.Sprintf("\t%%w.done.gep.%d = getelementptr inbounds %%task, %%task* %%w.task.%d, i32 0, i32 2\n", wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("\t%%w.done.val.%d = load i1, i1* %%w.done.gep.%d\n", wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("\tbr i1 %%w.done.val.%d, label %%w_exit.%d, label %%w_exec.%d\n", wrapperNum, wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("w_exec.%d:\n", wrapperNum))
	// 从 task_ptr 取 args (field 1)
	w.WriteString(fmt.Sprintf("\t%%w.args.gep.%d = getelementptr inbounds %%task, %%task* %%w.task.%d, i32 0, i32 1\n", wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("\t%%w.args.i64.%d = load i64, i64* %%w.args.gep.%d\n", wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("\t%%w.args.ptr.%d = inttoptr i64 %%w.args.i64.%d to i8*\n", wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("\t%%args.typed.%d = bitcast i8* %%w.args.ptr.%d to %s*\n", wrapperNum, wrapperNum, argsTypeStr))
	// Load result_ptr (field 0) and bitcast to resultType*
	w.WriteString(fmt.Sprintf("\t%%result.ptr.gep.%d = getelementptr inbounds %s, %s* %%args.typed.%d, i32 0, i32 0\n", wrapperNum, argsTypeStr, argsTypeStr, wrapperNum))
	w.WriteString(fmt.Sprintf("\t%%result.ptr.%d = load i8*, i8** %%result.ptr.gep.%d\n", wrapperNum, wrapperNum))
	w.WriteString(fmt.Sprintf("\t%%result.typed.%d = bitcast i8* %%result.ptr.%d to %s*\n", wrapperNum, wrapperNum, resultType))
	// Load and bitcast each arg
	callArgStrs := make([]string, 0, len(argTypes)+1)
	for i, argType := range argTypes {
		fieldIdx := i + 1
		w.WriteString(fmt.Sprintf("\t%%warg.%d.gep.%d = getelementptr inbounds %s, %s* %%args.typed.%d, i32 0, i32 %d\n", i, wrapperNum, argsTypeStr, argsTypeStr, wrapperNum, fieldIdx))
		w.WriteString(fmt.Sprintf("\t%%warg.%d.ptr.%d = load i8*, i8** %%warg.%d.gep.%d\n", i, wrapperNum, i, wrapperNum))
		w.WriteString(fmt.Sprintf("\t%%warg.%d.typed.%d = bitcast i8* %%warg.%d.ptr.%d to %s*\n", i, wrapperNum, i, wrapperNum, argType))
		callArgStrs = append(callArgStrs, fmt.Sprintf("%s* %%warg.%d.typed.%d", argType, i, wrapperNum))
	}
	// Add result as last argument
	callArgStrs = append(callArgStrs, fmt.Sprintf("%s* %%result.typed.%d", resultType, wrapperNum))
	// Call target function
	w.WriteString(fmt.Sprintf("\tcall void @%s(%s)\n", sanitizedFnName, strings.Join(callArgStrs, ", ")))
	// 设置 done = true (field 2) — 复用 entry 块中已定义的 %w.done.gep.N（SSA 全函数可见）
	w.WriteString(fmt.Sprintf("\tstore i1 true, i1* %%w.done.gep.%d\n", wrapperNum))
	w.WriteString(fmt.Sprintf("\tbr label %%w_exit.%d\n", wrapperNum))
	w.WriteString(fmt.Sprintf("w_exit.%d:\n", wrapperNum))
	w.WriteString("\tret void\n")
	w.WriteString("}\n\n")

	// === Generate caller code (to sb) ===
	// Allocate result buffer.
	// In coroutine context, use malloc (heap) because the result must survive
	// across yield points — the wrapper writes to it after coro_resume returns.
	resultSize := g.llvmTypeSize(resultType)
	if resultSize == 0 {
		resultSize = 8
	}
	resultAddr := g.allocForCoro(sb, "async.result", resultType, resultSize)
	if sb != nil {
		// zeroinitializer：避免 out 參數賦值時 freeOldHeapValue 讀取棧垃圾 data 指針
		sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), resultType, resultType, resultAddr))
	}

	// Allocate args struct.
	// In coroutine context, use malloc (heap) for the same reason.
	argsSize := int64(8 * numFields) // each field is i8* (8 bytes)
	argsAddr := g.allocForCoro(sb, "async.args", argsTypeStr, argsSize)

	// Bitcast result.addr to i8* and store into field 0
	resultPtrCast := g.tmpReg("async.rptr")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), resultPtrCast, resultType, resultAddr))
	}
	f0GEP := g.tmpReg("async.f0")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", g.indent(), f0GEP, argsTypeStr, argsTypeStr, argsAddr))
		g.storeDataPtrField(sb, resultPtrCast, f0GEP)
	}

	// For each argument: generateCallArg, extract pointer, bitcast to i8*, store into field
	for i, argExpr := range argExprs {
		argStr := g.generateCallArg(sb, argExpr)
		// Parse argStr: "<type> <pointer>" — extract type and pointer via last space
		argTypeStr := ""
		argPtr := argStr
		if idx := strings.LastIndex(argStr, " "); idx >= 0 {
			argTypeStr = argStr[:idx]
			argPtr = argStr[idx+1:]
		}
		// Identifier + 堆拥有类型参数（如 shared []i64）：深拷贝到独立缓冲区，
		// 避免调用端与异步任务共享同一 data 指针造成数据竞争。
		// 非 Identifier 参数（字面量/表达式）由 generateCallArg 分配独立 temp 缓冲区，无共享。
		// 克隆副本不在 wrapper 内释放：async 函数可能将其 move 到 out 参数，
		// wrapper 释放会导致 out 参数堆数据 UAF。
		if ident, ok := argExpr.(*parser.Identifier); ok && sb != nil {
			if varType, hasType := g.varTypes[ident.Value]; hasType && g.isHeapOwningType(varType) {
				elemType := ""
				if g.arrayElemTypes != nil {
					elemType = g.arrayElemTypes[ident.Value]
				}
				// 检查是否可安全深拷贝（与 b=a 深拷贝路径一致的 canClone 判断）
				canClone := true
				if (varType == "%vec" || varType == "%arr") &&
					(elemType == "%vec" || elemType == "%arr") {
					canClone = false // 巢狀容器，子元素型別未知
				}
				if varType != "%vec" && varType != "%arr" && varType != "%str-long" {
					if !g.canDeepCloneStruct(varType) {
						canClone = false
					}
				}
				if canClone {
					cloneSize := g.llvmTypeSize(varType)
					if cloneSize == 0 {
						cloneSize = 8
					}
					cloneBuf := g.allocForCoro(sb, "async.argclone", varType, cloneSize)
					g.emitDeepClone(sb, g.varAddr(ident.Value), cloneBuf, varType, elemType)
					argTypeStr = varType + "*"
					argPtr = cloneBuf
				}
			}
		}
		g.tmpIdx++
		argCast := fmt.Sprintf("%%async.arg.%d.cast.%d", i, g.tmpIdx)
		if sb != nil {
			if argTypeStr != "" {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast %s %s to i8*\n", g.indent(), argCast, argTypeStr, argPtr))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8*\n", g.indent(), argCast, argPtr))
			}
		}
		g.tmpIdx++
		argGEP := fmt.Sprintf("%%async.arg.%d.gep.%d", i, g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n", g.indent(), argGEP, argsTypeStr, argsTypeStr, argsAddr, i+1))
			g.storeDataPtrField(sb, argCast, argGEP)
		}
	}

	// Bitcast args struct to i8*
	argsBitcast := g.tmpReg("async.args.bc")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), argsBitcast, argsTypeStr, argsAddr))
	}

	return wrapperName, argsBitcast, resultPtrCast, resultType
}

// generateFutureCreation constructs a %future value (lazy, not executed).
// Called when an -async function is called directly (without run/awy).
func (g *Generator) generateFutureCreation(sb *strings.Builder, call *parser.CallExpression) string {
	wrapperName, argsBitcast, resultPtrCast, _ := g.prepareAsyncCall(sb, call)
	if wrapperName == "" {
		return "0"
	}

	// Build %future = { void (i8*)* @wrapper, i8* %args.bc, i8* %result.ptr }
	futureAddr := g.tmpReg("async.future")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%future\n", g.indent(), futureAddr))
	}
	// Store wrapper_fn_ptr (field 0)
	futF0GEP := g.tmpReg("async.fut.f0")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 0\n", g.indent(), futF0GEP, futureAddr))
		sb.WriteString(fmt.Sprintf("%sstore void (i8*)* @%s, void (i8*)** %s\n", g.indent(), wrapperName, futF0GEP))
	}
	// Store args_ptr (field 1)
	futF1GEP := g.tmpReg("async.fut.f1")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 1\n", g.indent(), futF1GEP, futureAddr))
		g.storeDataPtrField(sb, argsBitcast, futF1GEP)
	}
	// Store result_ptr (field 2)
	futF2GEP := g.tmpReg("async.fut.f2")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 2\n", g.indent(), futF2GEP, futureAddr))
		g.storeDataPtrField(sb, resultPtrCast, futF2GEP)
	}
	// Load %future value
	futureVal := g.tmpReg("async.future.val")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %%future, %%future* %s\n", g.indent(), futureVal, futureAddr))
	}

	// Track SSA type
	if g.ssaTypes != nil {
		g.ssaTypes[futureVal] = "%future"
	}

	return futureVal
}

// allocForCoro allocates memory for a value of the given LLVM type.
// 始终使用 malloc（堆分配）：task/args/result 入队后必须在函数返回后存活，
// 否则函数返回后栈帧销毁，task 指针留在全局就绪队列成为悬垂指针（UAF）。
// 即使在非 async 上下文（coroInAsyncFunc=false）也必须用 malloc，
// awy 取回结果后或函数返回前由 emitLocalTasksFree / awaitTaskVar 释放。
// Returns the LLVM register name for the allocated pointer.
func (g *Generator) allocForCoro(sb *strings.Builder, namePrefix, llvmType string, size int64) string {
	g.tmpIdx++
	memReg := fmt.Sprintf("%%heap.%s.%d", sanitizeLLVMName(namePrefix), g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), memReg, size))
	}
	g.tmpIdx++
	ptrReg := fmt.Sprintf("%%heap.%s.cast.%d", sanitizeLLVMName(namePrefix), g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), ptrReg, memReg, llvmType))
	}
	return ptrReg
}

// launchThread creates a %task for the async wrapper and enqueues it into the event loop.
// %task = { void (i8*)* resume_fn, i8* data, i1 done }
// resume_fn = @wrapper, data = args struct pointer (field 0 of args = result_ptr), done = false.
func (g *Generator) launchThread(sb *strings.Builder, wrapperName, argsBitcast, resultPtrCast, resultType string) string {
	// Allocate %task.
	// In coroutine context, use malloc (heap) because the task is enqueued in
	// the event loop and must survive across yield points.
	// %task = { void (i8*)*, i64, i1 } → 24 bytes with alignment padding.
	taskAddr := g.allocForCoro(sb, "async.task", "%task", 24)
	// Store resume_fn (field 0) = @wrapper
	taskF0GEP := g.tmpReg("async.task.f0")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", g.indent(), taskF0GEP, taskAddr))
		sb.WriteString(fmt.Sprintf("%sstore void (i8*)* @%s, void (i8*)** %s\n", g.indent(), wrapperName, taskF0GEP))
	}
	// Store data (field 1) = argsBitcast (args struct; field 0 of args = result_ptr)
	taskF1GEP := g.tmpReg("async.task.f1")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", g.indent(), taskF1GEP, taskAddr))
		g.storeDataPtrField(sb, argsBitcast, taskF1GEP)
	}
	// Store done = false (field 2)
	taskF2GEP := g.tmpReg("async.task.f2")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", g.indent(), taskF2GEP, taskAddr))
		sb.WriteString(fmt.Sprintf("%sstore i1 false, i1* %s\n", g.indent(), taskF2GEP))
	}
	// Enqueue task into the event loop ready queue
	taskCast := g.tmpReg("async.task.cast")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), taskCast, taskAddr))
		sb.WriteString(fmt.Sprintf("%scall void @nolang_async_enqueue(i8* %s)\n", g.indent(), taskCast))
	}
	// 返回堆 task 指针（%task*），而非值拷贝。
	// 原因：事件循环更新的是堆 task 的 done 字段，值拷贝不会被更新。
	// awy 需要通过指针访问最新的 done 字段。
	// Track SSA type
	if g.ssaTypes != nil {
		g.ssaTypes[taskAddr] = "%task*"
	}

	return taskAddr
}

// launchThreadFromFuture extracts data from a %future variable and creates a %task
// for the event loop. The future's wrapper_fn_ptr becomes resume_fn, args_ptr becomes data.
func (g *Generator) launchThreadFromFuture(sb *strings.Builder, varName string) string {
	futurePtr := g.varAddr(varName)

	// Extract wrapper_fn_ptr (field 0)
	wfnGEP := g.tmpReg("run.fut.wfn.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 0\n", g.indent(), wfnGEP, futurePtr))
	}
	wfnReg := g.tmpReg("run.fut.wfn")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load void (i8*)*, void (i8*)** %s\n", g.indent(), wfnReg, wfnGEP))
	}

	// Extract args_ptr (field 1) — becomes task.data
	argsGEP := g.tmpReg("run.fut.args.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 1\n", g.indent(), argsGEP, futurePtr))
	}
	argsReg := g.tmpReg("run.fut.args")
	if sb != nil {
		argsReg = g.loadDataPtrField(sb, argsGEP)
	}

	// Determine result type
	resultType := "i64"
	if g.futureResultTypes != nil {
		if t, ok := g.futureResultTypes[varName]; ok {
			resultType = t
		}
	}

	// Build %task value: { resume_fn=wrapper_fn_ptr, data=args_ptr, done=false }
	// 在协程上下文中使用 malloc（堆分配），因为 task 入队后需要在 yield 后存活。
	taskAddr := g.allocForCoro(sb, "run.fut.task", "%task", 24)
	// Store resume_fn (field 0)
	taskF0GEP := g.tmpReg("run.fut.task.f0")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", g.indent(), taskF0GEP, taskAddr))
		sb.WriteString(fmt.Sprintf("%sstore void (i8*)* %s, void (i8*)** %s\n", g.indent(), wfnReg, taskF0GEP))
	}
	// Store data (field 1) = args_ptr
	taskF1GEP := g.tmpReg("run.fut.task.f1")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", g.indent(), taskF1GEP, taskAddr))
		g.storeDataPtrField(sb, argsReg, taskF1GEP)
	}
	// Store done = false (field 2)
	taskF2GEP := g.tmpReg("run.fut.task.f2")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", g.indent(), taskF2GEP, taskAddr))
		sb.WriteString(fmt.Sprintf("%sstore i1 false, i1* %s\n", g.indent(), taskF2GEP))
	}
	// Enqueue
	taskCast := g.tmpReg("run.fut.task.cast")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), taskCast, taskAddr))
		sb.WriteString(fmt.Sprintf("%scall void @nolang_async_enqueue(i8* %s)\n", g.indent(), taskCast))
	}
	// 返回堆 task 指针（%task*），与 launchThread 保持一致。
	// Track SSA type and result type
	if g.ssaTypes != nil {
		g.ssaTypes[taskAddr] = "%task*"
	}
	// Propagate result type to task variable
	if g.taskResultTypes != nil {
		g.taskResultTypes[varName] = resultType
	}

	return taskAddr
}

// generateRunExpression generates LLVM IR for `run <expression>`.
// Accepts: CallExpression (run f-async(args)) or Identifier (run future_var).
// Creates a %task and enqueues it into the event loop, returning the %task handle.
func (g *Generator) generateRunExpression(sb *strings.Builder, expr *parser.RunExpression) string {
	switch c := expr.Call.(type) {
	case *parser.CallExpression:
		// run f-async(args) or run non-async-func(args)
		wrapperName, argsBitcast, resultPtrCast, resultType := g.prepareAsyncCall(sb, c)
		if wrapperName == "" {
			return "0"
		}
		return g.launchThread(sb, wrapperName, argsBitcast, resultPtrCast, resultType)
	case *parser.Identifier:
		// run future_var — launch thread from existing future
		return g.launchThreadFromFuture(sb, c.Value)
	}
	return "0"
}

// awaitFutureCall creates a future and immediately executes it (synchronous, no thread).
func (g *Generator) awaitFutureCall(sb *strings.Builder, call *parser.CallExpression) string {
	wrapperName, argsBitcast, _, resultType := g.prepareAsyncCall(sb, call)
	if wrapperName == "" {
		return "0"
	}

	// Directly call wrapper_fn(args) — synchronous execution, no pthread needed
	callReg := g.tmpReg("awy.fut.call")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @%s(i8* %s)\n", g.indent(), callReg, wrapperName, argsBitcast))
	}

	// Bitcast result_ptr to correct type
	resultTyped := g.tmpReg("awy.fut.typed")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), resultTyped, callReg, resultType))
	}

	// Load the result value
	resultVal := g.tmpReg("awy.fut.result")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), resultVal, resultType, resultType, resultTyped))
	}

	// Track SSA type
	if g.ssaTypes != nil {
		g.ssaTypes[resultVal] = resultType
	}

	return resultVal
}

// awaitFutureVar executes a %future variable directly (synchronous, no thread).
func (g *Generator) awaitFutureVar(sb *strings.Builder, varName string) string {
	futurePtr := g.varAddr(varName)

	// Extract wrapper_fn_ptr (field 0)
	wfnGEP := g.tmpReg("awy.fv.wfn.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 0\n", g.indent(), wfnGEP, futurePtr))
	}
	wfnReg := g.tmpReg("awy.fv.wfn")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load void (i8*)*, void (i8*)** %s\n", g.indent(), wfnReg, wfnGEP))
	}

	// Extract args_ptr (field 1)
	argsGEP := g.tmpReg("awy.fv.args.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 1\n", g.indent(), argsGEP, futurePtr))
	}
	argsReg := g.tmpReg("awy.fv.args")
	if sb != nil {
		argsReg = g.loadDataPtrField(sb, argsGEP)
	}

	// Extract result_ptr (field 2)
	rptrGEP := g.tmpReg("awy.fv.rptr.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%future, %%future* %s, i32 0, i32 2\n", g.indent(), rptrGEP, futurePtr))
	}
	rptrReg := g.tmpReg("awy.fv.rptr")
	if sb != nil {
		rptrReg = g.loadDataPtrField(sb, rptrGEP)
	}

	// Determine result type
	resultType := "i64"
	if g.futureResultTypes != nil {
		if t, ok := g.futureResultTypes[varName]; ok {
			resultType = t
		}
	}

	// Directly call wrapper_fn(args)
	callReg := g.tmpReg("awy.fv.call")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call i8* %s(i8* %s)\n", g.indent(), callReg, wfnReg, argsReg))
	}

	// Bitcast result_ptr to correct type (use the result_ptr from the future)
	resultTyped := g.tmpReg("awy.fv.typed")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), resultTyped, rptrReg, resultType))
	}

	// Load the result value
	resultVal := g.tmpReg("awy.fv.result")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), resultVal, resultType, resultType, resultTyped))
	}

	// Track SSA type
	if g.ssaTypes != nil {
		g.ssaTypes[resultVal] = resultType
	}

	return resultVal
}

// awaitTaskVar waits for a %task variable to complete and loads the result.
// In non-async functions (no event loop running), this synchronously calls resume_fn(task)
// if the task is not yet done. In async functions, awy is handled by the state machine
// transform (coro.go), so this path is only reached for misuse.
func (g *Generator) awaitTaskVar(sb *strings.Builder, varName string) string {
	// task 变量存储在 alloca 中（alloca %task*），需先 load %task* 再使用。
	// 直接使用 varAddr 会得到 alloca 地址（%task**），将其作为 %task* 会导致
	// GEP 越界访问（field 2 偏移 16 字节超出 8 字节 alloca），读取到栈垃圾。
	// 与 generateAwaitForCoro Case 2 保持一致的 load 模式。
	taskVarAddr := g.varAddr(varName)
	taskPtr := g.tmpReg("awy.task.ptr")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %%task*, %%task** %s\n", g.indent(), taskPtr, taskVarAddr))
	}

	// Check task.done (field 2)
	doneGEP := g.tmpReg("awy.done.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", g.indent(), doneGEP, taskPtr))
	}
	doneVal := g.tmpReg("awy.done")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load i1, i1* %s\n", g.indent(), doneVal, doneGEP))
	}
	// br i1 %done, label %done_lbl, label %not_done
	notDoneLabel := fmt.Sprintf("awy.not_done.%d", g.tmpIdx)
	doneLabel := fmt.Sprintf("awy.done_lbl.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), doneVal, doneLabel, notDoneLabel))
	}

	// not_done: synchronously call resume_fn(task) to drive it to completion
	if sb != nil {
		sb.WriteString(notDoneLabel + ":\n")
	}
	// load resume_fn (field 0)
	fnGEP := g.tmpReg("awy.fn.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", g.indent(), fnGEP, taskPtr))
	}
	fnVal := g.tmpReg("awy.fn")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load void (i8*)*, void (i8*)** %s\n", g.indent(), fnVal, fnGEP))
	}
	// bitcast task* to i8*
	taskI8 := g.tmpReg("awy.task.i8")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), taskI8, taskPtr))
	}
	// call resume_fn(task)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%scall void %s(i8* %s)\n", g.indent(), fnVal, taskI8))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), doneLabel))
	}

	// done: load result
	if sb != nil {
		sb.WriteString(doneLabel + ":\n")
	}

	// Extract data (field 1) — args struct whose field 0 is result_ptr
	dataGEP := g.tmpReg("awy.data.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", g.indent(), dataGEP, taskPtr))
	}
	dataVal := g.tmpReg("awy.data")
	if sb != nil {
		dataVal = g.loadDataPtrField(sb, dataGEP)
	}

	// Determine result type
	resultType := "i64"
	if g.taskResultTypes != nil {
		if t, ok := g.taskResultTypes[varName]; ok {
			resultType = t
		}
	}

	// args struct field 0 = result_ptr; bitcast data to { i8* }* and load result_ptr
	argsTyped := g.tmpReg("awy.args.typed")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to { i8* }*\n", g.indent(), argsTyped, dataVal))
	}
	resultPtrGEP := g.tmpReg("awy.resptr.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", g.indent(), resultPtrGEP, argsTyped))
	}
	resultPtr := g.tmpReg("awy.resptr")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), resultPtr, resultPtrGEP))
	}

	// Bitcast result_ptr to correct type and load
	resultTyped := g.tmpReg("awy.result.typed")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), resultTyped, resultPtr, resultType))
	}
	resultVal := g.tmpReg("awy.result")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), resultVal, resultType, resultType, resultTyped))
	}

	// Track SSA type
	if g.ssaTypes != nil {
		g.ssaTypes[resultVal] = resultType
	}

	// 释放 task/args/result 堆容器（SubTask 2.2）。
	// 仅 free 容器结构体本身（malloc 的 %task / args struct / result buffer），
	// 不 free 容器内 data 指针指向的缓冲区：
	//   - result buffer 内的 data 由调用端结果变量（如 s = awy task 中的 s）接管，
	//     经 trackLocalHeapVar 追踪，函数结束由 emitHeapFree 释放。
	//   - args struct 内的 arg 指针指向 cloneBuf（Identifier 堆参数深拷贝）或
	//     generateCallArg 分配的 temp。cloneBuf 可能被 async 函数 move 到 out 参数，
	//     释放会导致 out 参数堆数据 UAF（与 prepareAsyncCall 注释一致）。
	//     故 cloneBuf 不在此释放，作为已知泄漏留待后续处理。
	// 释放顺序：result buffer → args struct → task struct（先释放被引用者）。
	if sb != nil {
		// 1. free result buffer (result_ptr，仅容器)
		freeResultReg := g.tmpReg("awy.free.result")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8*\n", g.indent(), freeResultReg, resultPtr))
		sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), freeResultReg))

		// 2. free args struct (dataVal 即 args_ptr i8*，仅容器)
		freeArgsReg := g.tmpReg("awy.free.args")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8*\n", g.indent(), freeArgsReg, dataVal))
		sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), freeArgsReg))

		// 3. free task struct (taskPtr，仅容器)
		freeTaskReg := g.tmpReg("awy.free.task")
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), freeTaskReg, taskPtr))
		sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), freeTaskReg))
	}

	// 标记该 task 变量已 awy，从 localTasks 移除（SubTask 2.3 追踪用）
	g.untrackLocalTask(varName)

	return resultVal
}

// generateAwaitExpression generates LLVM IR for `awy <expression>`.
// Accepts: CallExpression (awy f-async(args)), future variable (awy f), or task variable (awy h).
func (g *Generator) generateAwaitExpression(sb *strings.Builder, expr *parser.AwaitExpression) string {
	// Case 1: awy f-async(args) — create future + execute directly
	if call, ok := expr.Right.(*parser.CallExpression); ok {
		if g.isAsyncCall(call) {
			return g.awaitFutureCall(sb, call)
		}
	}

	// Case 2 & 3: awy <identifier> — check if future or task
	if ident, ok := expr.Right.(*parser.Identifier); ok {
		if g.isFutureVar(ident.Value) {
			// awy <future_var> — execute directly
			return g.awaitFutureVar(sb, ident.Value)
		}
		// awy <task_var> — wait for completion and load result
		return g.awaitTaskVar(sb, ident.Value)
	}

	// Fallback: evaluate expression, store to a %task alloca, then await it
	taskVal := g.generateExprWithSB(sb, expr.Right)
	taskAlloca := g.tmpReg("awy.task")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%task\n", g.indent(), taskAlloca))
		sb.WriteString(fmt.Sprintf("%sstore %%task %s, %%task* %s\n", g.indent(), taskVal, taskAlloca))
	}

	// Determine result type from SSA types
	resultType := "i64"
	if g.ssaTypes != nil {
		if t, ok := g.ssaTypes[taskVal]; ok {
			resultType = t
		}
	}

	// Check done (field 2); if not done, synchronously call resume_fn
	doneGEP := g.tmpReg("awy.fb.done.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 2\n", g.indent(), doneGEP, taskAlloca))
	}
	doneVal := g.tmpReg("awy.fb.done")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load i1, i1* %s\n", g.indent(), doneVal, doneGEP))
	}
	notDoneLabel := fmt.Sprintf("awy.fb.not_done.%d", g.tmpIdx)
	doneLabel := fmt.Sprintf("awy.fb.done_lbl.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), doneVal, doneLabel, notDoneLabel))
	}
	// not_done: call resume_fn(task)
	if sb != nil {
		sb.WriteString(notDoneLabel + ":\n")
	}
	fnGEP := g.tmpReg("awy.fb.fn.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 0\n", g.indent(), fnGEP, taskAlloca))
	}
	fnVal := g.tmpReg("awy.fb.fn")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load void (i8*)*, void (i8*)** %s\n", g.indent(), fnVal, fnGEP))
	}
	taskI8 := g.tmpReg("awy.fb.task.i8")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %%task* %s to i8*\n", g.indent(), taskI8, taskAlloca))
		sb.WriteString(fmt.Sprintf("%scall void %s(i8* %s)\n", g.indent(), fnVal, taskI8))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), doneLabel))
	}
	if sb != nil {
		sb.WriteString(doneLabel + ":\n")
	}

	// Extract data (field 1) → args struct field 0 = result_ptr
	dataGEP := g.tmpReg("awy.fb.data.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%task, %%task* %s, i32 0, i32 1\n", g.indent(), dataGEP, taskAlloca))
	}
	dataVal := g.tmpReg("awy.fb.data")
	if sb != nil {
		dataVal = g.loadDataPtrField(sb, dataGEP)
	}
	argsTyped := g.tmpReg("awy.fb.args.typed")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to { i8* }*\n", g.indent(), argsTyped, dataVal))
	}
	resultPtrGEP := g.tmpReg("awy.fb.resptr.gep")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds { i8* }, { i8* }* %s, i32 0, i32 0\n", g.indent(), resultPtrGEP, argsTyped))
	}
	resultPtr := g.tmpReg("awy.fb.resptr")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), resultPtr, resultPtrGEP))
	}
	resultTyped := g.tmpReg("awy.fb.result.typed")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), resultTyped, resultPtr, resultType))
	}
	resultVal := g.tmpReg("awy.fb.result")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), resultVal, resultType, resultType, resultTyped))
	}
	if g.ssaTypes != nil {
		g.ssaTypes[resultVal] = resultType
	}

	return resultVal
}
