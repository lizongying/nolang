package llvm

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// llvmTypeToNolang maps LLVM primitive types (with leading '%' stripped) to all
// nolang type names that could have produced them. This is needed for method-call
// resolution, where the receiver's variable has an LLVM type (e.g. i32) but the
// method is registered under its nolang type name (e.g. char.is-alpha).
var llvmTypeToNolang = map[string][]string{
	"i1":       {"bool"},
	"i8":       {"byte", "i8", "u8"},
	"i16":      {"i16", "u16"},
	"i32":      {"char", "i32", "u32"},
	"i64":      {"i64", "u64"},
	"float":    {"f32"},
	"double":   {"f64"},
	"str-long": {"str"},
}

// flattenDottedExpr 將鏈式 DotExpression（如 net.quic.fn）展開為完整名稱字串。
// 僅處理 Identifier 與 DotExpression 兩種節點；遇到其他節點返回 ""。
func flattenDottedExpr(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.Identifier:
		return e.Value
	case *parser.DotExpression:
		left := flattenDottedExpr(e.Receiver)
		if left == "" {
			return ""
		}
		return left + "." + e.Property
	}
	return ""
}

// isNonVoidCall checks if a CallExpression returns a non-void type.
func (g *Generator) isNonVoidCall(expr *parser.CallExpression) bool {
	if ident, ok := expr.Function.(*parser.Identifier); ok {
		if g.funcRetTypes != nil {
			if t, ok := g.funcRetTypes[ident.Value]; ok {
				return t != "void"
			}
		}
		// Builtin methods are always non-void
		if m := builtin.FindBuiltinMethod(ident.Value); m != nil {
			return true
		}
	}
	return true // default to non-void for unknown calls
}

// generateCallArg 生成單個函數調用參數的 LLVM 表示
func (g *Generator) generateCallArg(sb *strings.Builder, arg parser.Expression) string {
	switch a := arg.(type) {
	case *parser.Identifier:
		// Enum variant: allocate temp i64 and store the constant tag index
		// 但若同名變數已存在（如 curried call 的輸出參數 ok/err），優先當作變數
		if g.enumVariantIndex != nil {
			if _, isVar := g.varTypes[a.Value]; !isVar {
				if tagIdx, ok := g.enumVariantIndex[a.Value]; ok {
					g.tmpIdx++
					tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
						sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), tagIdx, tmpName))
					}
					return "i64* " + tmpName
				}
			}
		}
		// Function reference: when an Identifier refers to a known user-defined
		// function, pass it as a function pointer. Allocate a temp void (...)* slot
		// and store @funcName into it, so the callee receives a by-reference pointer.
		// Function names may also appear in g.varTypes as "i64" (a placeholder from
		// collectVarDecls), so we check funcRetTypes first to distinguish functions
		// from real variables.
		// But local variables/parameters (in funcLocalNames) shadow global function
		// names — e.g. test-f32-to-str has local `f f32` which must not be confused
		// with the global `f` function from des.no.
		isLocalVar := g.funcLocalNames != nil && g.funcLocalNames[a.Value]
		if !isLocalVar && g.funcRetTypes != nil && g.funcRetTypes[a.Value] != "" {
			if _, isFn := g.funcRetTypes[a.Value]; isFn {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%fnptr.arg.%d", g.tmpIdx)
				fnLLVMName := a.Value
				if clibFuncNames[a.Value] {
					fnLLVMName = "n." + a.Value
				}
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca void (...)*\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore void (...)* @%s, void (...)** %s\n", g.indent(), sanitizeLLVMName(fnLLVMName), tmpName))
				}
				return "void (...)** " + tmpName
			}
		}
		if g.varTypes != nil {
			// Slice view variable passed as function argument:
			// materialize a temporary struct with shared data pointer (no clone).
			// This preserves the reference semantics of Nolang function parameters.
			if g.isSliceViewVar(a.Value) {
				view := g.sliceViews[a.Value]
				matPtr := g.materializeSliceView(sb, a.Value)
				if view.isStr {
					return "%str-long* " + matPtr
				}
				return "%vec* " + matPtr
			}
			if t, ok := g.varTypes[a.Value]; ok && t == "%str-long" {
				return "%str-long* " + g.varAddr(a.Value)
			}
			if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
				return t + "* " + g.varAddr(a.Value)
			}
			if t, ok := g.varTypes[a.Value]; ok && t == "double" {
				return "double* " + g.varAddr(a.Value)
			}
			if t, ok := g.varTypes[a.Value]; ok && t == "float" {
				return "float* " + g.varAddr(a.Value)
			}
			// %arr (fixed-size array struct) → extract data pointer and bitcast to [N x T]*
			// Function parameters with [N]T type use raw LLVM array [N x T], but local
			// variables are stored as %arr struct { len, data* }. Need to extract data
			// pointer and bitcast to match the expected parameter type.
			if t, ok := g.varTypes[a.Value]; ok && t == "%arr" {
				if elemType, ok := g.arrayElemTypes[a.Value]; ok {
					if arrSize, ok := g.arraySizes[a.Value]; ok {
						rawArrType := fmt.Sprintf("[%d x %s]", arrSize, elemType)
						if sb != nil {
							g.tmpIdx++
							dataGEP := fmt.Sprintf("%%arr.arg.gep.%d", g.tmpIdx)
							g.tmpIdx++
							dataLoad := fmt.Sprintf("%%arr.arg.data.%d", g.tmpIdx)
							g.tmpIdx++
							dataCast := fmt.Sprintf("%%arr.arg.cast.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, g.varAddr(a.Value)))
							dataLoad = g.loadDataPtrField(sb, dataGEP)
							sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dataCast, dataLoad, rawArrType))
							return rawArrType + "* " + dataCast
						}
						return rawArrType + "* " + g.varAddr(a.Value)
					}
				}
				// Fallback: pass %arr* directly
				return "%arr* " + g.varAddr(a.Value)
			}
			// %vec / 任何 struct 指標型別 → 變數本身已是指標
			if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "%") {
				return t + "* " + g.varAddr(a.Value)
			}
			// bool (i1) 變數 → 返回 i1*
			if t, ok := g.varTypes[a.Value]; ok && t == "i1" {
				return "i1* " + g.varAddr(a.Value)
			}
			// i32 / i16 / i8 等純量型別 — 使用實際型別而非預設 i64*
			if t, ok := g.varTypes[a.Value]; ok {
				return t + "* " + g.varAddr(a.Value)
			}
		}
		return "i64* " + g.varAddr(a.Value)
	case *parser.FloatLiteral:
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
			sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), fmt.Sprintf("%f", a.Value), tmpName))
		}
		return "double* " + tmpName
	case *parser.StringLiteral:
		ev := g.generateExprWithSB(sb, arg)

		return "%str-long* " + ev
	case *parser.IntegerLiteral:
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), a.Value, tmpName))
		}
		return "i64* " + tmpName
	case *parser.IndexExpression:
		// 索引表達式可能回傳 %T* (slice/array of structs)、i64 (數字元素) 或 i8* (byte 元素)
		// 對於 struct 切片，SSA 值已經是指標，直接傳遞即可
		ev := g.generateExprWithSB(sb, arg)
		// 從 SSA 寄存器名稱推斷型別：GEP for struct slice → %T*；load → i64
		// %idx.gep.*, %arr.idx.elem.*, %vec.idx.elem.*, %str-longidx.gep.* 等都是 GEP 結果（指標）
		// %idx.zext.*, %arr.idx.val.*, %vec.idx.val.* 等是載入值（i64 或 %str-long）
		// 使用 exprResultLLVMType 推導元素型別（支援 Identifier 和 DotExpression receiver）
		elemLLVMType := g.exprResultLLVMType(arg)
		if elemLLVMType == "" {
			elemLLVMType = "i64"
		}
		if strings.Contains(ev, ".gep.") || strings.Contains(ev, ".elem.") {
			// GEP result is a pointer
			return elemLLVMType + "* " + ev
		}
		// SSA value (e.g., %idx.zext.* for []byte, %arr.idx.val.* for []str)
		// 需根據元素型別選擇正確的 alloca/store 型別
		// 若 SSA 值為 i64（如 zext 後的 byte/int32）但元素型別為 i8/i16/i32，需 trunc
		storeVal := ev
		if strings.HasPrefix(ev, "%") && g.isIntegerLLVMType(elemLLVMType) {
			// generateIndexExpression always zexts narrow integer elements to i64,
			// so the SSA value type is i64 regardless of elemLLVMType.
			// When elemLLVMType is a narrow integer (i8/i16/i32), trunc to elemLLVMType.
			srcType := "i64"
			if srcType != elemLLVMType {
				g.tmpIdx++
				convReg := fmt.Sprintf("%%ref.conv.%d", g.tmpIdx)
				if sb != nil {
					if srcType == "i64" {
						// i64 → smaller type: trunc
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, srcType, ev, elemLLVMType))
					} else if elemLLVMType == "i64" {
						// smaller type → i64: zext
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), convReg, srcType, ev))
					} else {
						// smaller → smaller (both non-i64): trunc to target
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, srcType, ev, elemLLVMType))
					}
				}
				storeVal = convReg
			}
		}
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, elemLLVMType))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemLLVMType, storeVal, elemLLVMType, tmpName))
		}
		return elemLLVMType + "* " + tmpName
	case *parser.SliceExpression:
		// 切片表達式回傳 %vec 或 %str-long（已分配在 stack 上）
		ev := g.generateExprWithSB(sb, arg)
		// 從變數型別推斷指標型別
		ptrType := "%vec*"
		if ident, ok := a.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					if t == "%str-long" {
						ptrType = "%str-long*"
					}
				}
			}
		} else {
			// Non-Identifier receiver (e.g. .field[..]): use exprResultLLVMType
			recvType := g.exprResultLLVMType(a.Left)
			if recvType == "%str-long" {
				ptrType = "%str-long*"
			}
		}
		return ptrType + " " + ev
	case *parser.StructLiteral:
		// 結構體字面量：分配 temp slot 並依序 store 欄位
		structName := a.Type
		structTy := "%" + structName
		fields := g.structTypes[structName]
		fieldIndexByName := make(map[string]int)
		for i, f := range fields {
			fieldIndexByName[f.name] = i
		}
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.st.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, structTy))
			// 先將整個結構體初始化為 zeroinitializer，避免未指定的欄位帶有 stack 殘值
			sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), structTy, structTy, tmpName))
		}
		// 記錄哪些欄位已由 literal 明確設定
		setFields := make(map[int]bool)
		for _, f := range a.Fields {
			fieldIdx, ok := fieldIndexByName[f.Name]
			if !ok {
				continue
			}
			setFields[fieldIdx] = true
			fieldType := fields[fieldIdx].typ
			fieldVal := g.generateExprWithSB(sb, f.Value)
			fieldVal = g.stripLLVMType(fieldVal)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%ref.st.gep.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), gepReg, structTy, structTy, tmpName, fieldIdx))
			}
			if strings.HasPrefix(fieldType, "%") {
				if !strings.HasPrefix(fieldVal, "%") {
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), fieldType, fieldType, gepReg))
					}
				} else if _, isStrLit := f.Value.(*parser.StringLiteral); isStrLit || g.isStrPtrReg(fieldVal) {
					// String literals and str pointer regs are %str-long* pointers (alloca),
					// need to load the value before storing.
					if sb != nil {
						g.tmpIdx++
						loadReg := fmt.Sprintf("%%ref.st.fload.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, fieldType, fieldType, fieldVal))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, loadReg, fieldType, gepReg))
					}
				} else if sb != nil {
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
				}
			} else if sb != nil {
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
			}
		}
		// 為未明確設定的 %vec 欄位分配 data 緩衝區，設定 cap=256
		// 否則 vec[i] = val 會因 data=null 而 SIGBUS
		if sb != nil {
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
				g.tmpIdx++
				dataBuf := fmt.Sprintf("%%ref.st.vecdata.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%ref.st.veccap.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 1\n",
					g.indent(), capGEP, structTy, structTy, tmpName, i))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%ref.st.vecdataptr.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 2\n",
					g.indent(), dataGEP, structTy, structTy, tmpName, i))
				g.storeDataPtrField(sb, dataBuf, dataGEP)
			}
		}
		return structTy + "* " + tmpName
	case *parser.SliceLiteral:
		// Slice literal passed as function argument in indirect/curried calls.
		// Default to i64 element type (parameter type info unavailable here).
		n := int64(len(a.Elements))
		g.tmpIdx++
		vecName := fmt.Sprintf("%%callvec.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecName))
		}
		if n > 0 {
			g.tmpIdx++
			tmpArr := fmt.Sprintf("%%callvec.arr.%d", g.tmpIdx)
			arrType := fmt.Sprintf("[%d x i64]", n)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))
				for i, elem := range a.Elements {
					ev := g.generateExprWithSB(sb, elem)
					ev = g.stripLLVMType(ev)
					g.tmpIdx++
					gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), gepReg, arrType, arrType, tmpArr, i))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, gepReg))
				}
				g.tmpIdx++
				ptrReg := fmt.Sprintf("%%callvec.ptr.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), ptrReg, arrType, tmpArr))
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%callvec.len.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%callvec.cap.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%callvec.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, vecName))
				g.storeDataPtrField(sb, ptrReg, dataGEP)
			}
		}
		return "%vec* " + vecName
	default:
		ev := g.generateExprWithSB(sb, arg)
		if strings.HasPrefix(ev, "%str-longlit") {
			return "%str-long* " + ev
		}
		// String concat / string method call results: ev is a %str-long* SSA register.
		// Detect via isStringExpr so InfixExpression (- for concat) and other string
		// expressions are passed as %str-long* instead of being truncated to i64.
		// But DotExpression and regular CallExpression load a %str-long VALUE
		// (not a pointer), so they must fall through to the alloca+store path.
		// ForwardFunc built-ins (like arg(i)) return %str-long* pointers directly,
		// so they can use the direct return path.
		if g.isStringExpr(arg) && strings.HasPrefix(ev, "%") {
			if _, isDot := arg.(*parser.DotExpression); !isDot {
				isRegularCall := false
				if call, ok := arg.(*parser.CallExpression); ok {
					isForward := false
					if ident, ok := call.Function.(*parser.Identifier); ok {
						if m := builtin.FindBuiltinMethod(ident.Value); m != nil && m.ForwardFunc != "" {
							isForward = true
						}
					}
					if !isForward {
						isRegularCall = true
					}
				}
				if !isRegularCall {
					return "%str-long* " + ev
				}
			}
		}
		if strings.HasPrefix(ev, "%") {
			// SSA register (value, not pointer) — allocate a temp slot and store
			// the value, so the function can take a pointer to it.
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			ptrType := "i64*"
			parts := strings.SplitN(ev, ".", 2)
			baseName := strings.TrimPrefix(parts[0], "%")
			if g.varTypes != nil {
				if t, ok := g.varTypes[baseName]; ok {
					if t == "double" {
						ptrType = "double*"
					} else if t == "%str-long" {
						ptrType = "%str-long*"
					} else if t == "i8*" {
						ptrType = "i8**"
					}
				}
				if idx := strings.IndexByte(baseName, '.'); idx > 0 {
					if t, ok := g.varTypes[baseName[:idx]]; ok {
						if t == "double" {
							ptrType = "double*"
						} else if t == "%str-long" {
							ptrType = "%str-long*"
						}
					}
				}
			}
			// DotExpression (e.g. .field) loads a struct value into an SSA register.
			// The baseName "dot" is not a real variable, so varTypes lookup fails and
			// ptrType defaults to i64*. Determine the field type from structTypes so
			// %vec / %arr / %str-long fields get the correct pointer type.
			if dot, ok := arg.(*parser.DotExpression); ok {
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					if g.varTypes != nil {
						if t, ok := g.varTypes[ident.Value]; ok && strings.HasPrefix(t, "%") {
							structName := strings.TrimPrefix(t, "%")
							if fields, ok := g.structTypes[structName]; ok {
								for _, f := range fields {
									if f.name == dot.Property {
										ptrType = f.typ + "*"
										break
									}
								}
							}
						}
					}
				}
			}
			// CallExpression: the baseName (e.g. "call") is not a real variable,
			// so varTypes lookup fails. Use exprResultLLVMType to determine the
			// function's return type for correct alloca/store.
			if call, ok := arg.(*parser.CallExpression); ok {
				if et := g.exprResultLLVMType(call); et != "" {
					ptrType = et + "*"
				}
			}
			elemType := strings.TrimSuffix(ptrType, "*")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, elemType))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %s\n", g.indent(), elemType, ev, ptrType, tmpName))
			}
			return ptrType + " " + tmpName
		} else if strings.Contains(ev, ".") {
			// float literal value (e.g. "180.000000")
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), ev, tmpName))
			}
			return "double* " + tmpName
		} else if _, err := fmt.Sscanf(ev, "%d", new(int)); err == nil {
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
			}
			return "i64* " + tmpName
		}
		return ev
	}
}

// generateIndirectCall emits a call through a function-pointer variable.
// varName is the variable holding the function pointer; ft is its FunctionType,
// used to determine the number of by-reference output parameters.
func (g *Generator) generateIndirectCall(sb *strings.Builder, expr *parser.CallExpression, varName string, ft *parser.FunctionType) string {
	// Load the function pointer: %fnPtr = load void (...)*, void (...)** %var
	g.tmpIdx++
	fnPtrReg := fmt.Sprintf("%%fnptr.load.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load void (...)*, void (...)** %s\n", g.indent(), fnPtrReg, g.varAddr(varName)))
	}
	// Generate input arguments (by-reference, same convention as direct calls)
	args := make([]string, 0, len(expr.Arguments)+len(ft.Results))
	for _, arg := range expr.Arguments {
		args = append(args, g.generateCallArg(sb, arg))
	}
	numResults := len(ft.Results)
	if numResults == 0 {
		// Pure void call
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall void %s(%s)\n", g.indent(), fnPtrReg, strings.Join(args, ", ")))
		}
		return ""
	}
	// One or more results: allocate temp output slots and append as by-reference args
	resultTypes := make([]string, numResults)
	resultTemps := make([]string, numResults)
	// Save stack pointer to prevent stack growth when called inside loops
	g.tmpIdx++
	fncallSp := fmt.Sprintf("%%fncall.sp.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = call ptr @llvm.stacksave.p0()\n", g.indent(), fncallSp))
	}
	for i, r := range ft.Results {
		llvmType := g.mapToLLVMType(r.Type.String())
		resultTypes[i] = llvmType
		g.tmpIdx++
		tmp := fmt.Sprintf("%%fncall.out.%d", g.tmpIdx)
		resultTemps[i] = tmp
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmp, llvmType))
		}
		args = append(args, llvmType+"* "+tmp)
	}
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%scall void %s(%s)\n", g.indent(), fnPtrReg, strings.Join(args, ", ")))
	}
	// Load the first result to return as the call's SSA value (Phase 1: single result)
	if numResults >= 1 {
		g.tmpIdx++
		loadReg := fmt.Sprintf("%%fncall.ret.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, resultTypes[0], resultTypes[0], resultTemps[0]))
			// Restore stack pointer to prevent stack growth when called inside loops
			sb.WriteString(fmt.Sprintf("%scall void @llvm.stackrestore.p0(ptr %s)\n", g.indent(), fncallSp))
		}
		return loadReg
	}
	return ""
}

func (g *Generator) generateCallExpression(sb *strings.Builder, expr *parser.CallExpression) string {
	// -async 函数调用：返回 %future（惰性，不执行）
	if g.isAsyncCall(expr) {
		return g.generateFutureCreation(sb, expr)
	}
	// Indirect call through a function-type variable (e.g. fn() where fn is a param of type fn (...)).
	// Detect before any other dispatch: if the callee identifier is registered in
	// g.varFnTypes, emit a call through a loaded function pointer.
	if ident, ok := expr.Function.(*parser.Identifier); ok {
		if ft, ok := g.varFnTypes[ident.Value]; ok {
			return g.generateIndirectCall(sb, expr, ident.Value, ft)
		}
	}
	// 例如：str-index(s, sn, target, tn)(pos)
	if innerCall, ok := expr.Function.(*parser.CallExpression); ok {
		// 確定內層調用的返回型別
		retType := "void"
		innerFnName := ""
		var innerMethodRecv parser.Expression // method receiver if inner call is s.method(...)
		if ident, ok := innerCall.Function.(*parser.Identifier); ok {
			innerFnName = ident.Value
		} else if dot, ok := innerCall.Function.(*parser.DotExpression); ok {
			// 解析 receiver method call：s.to-bytes → str.to-bytes
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				candidate := recv.Value + "." + dot.Property
				// 僅當 receiver 是已註冊變數時才視為方法呼叫（需傳 receiver 作為首參）。
				// 若 receiver 不在 varTypes 中（如模組名 rsa.rsa-modpow），
				// 則為模組限定呼叫，不設定 innerMethodRecv。
				if _, isVar := g.varTypes[recv.Value]; isVar {
					innerMethodRecv = recv
				}
				// 嘗試 union alias 解析
				if recvType, ok := g.varTypes[recv.Value]; ok && g.unionAliases != nil {
					srcType := recvType
					if srcType == "double" {
						srcType = "f64"
					} else if srcType == "float" {
						srcType = "f32"
					}
					for aliasName := range g.unionAliases {
						if !g.isMemberOfUnionTransitive(srcType, aliasName, make(map[string]bool)) {
							continue
						}
						monoName := aliasName + "." + dot.Property + "__" + srcType
						if _, exists := g.funcRetTypes[monoName]; exists {
							innerFnName = monoName
							innerMethodRecv = nil // monomorphized 名字已含型別
							break
						}
						unionName := aliasName + "." + dot.Property
						if _, exists := g.funcRetTypes[unionName]; exists {
							innerFnName = unionName
							innerMethodRecv = nil
							break
						}
					}
				}
				// str/arr/vec 方法解析
				if innerFnName == "" {
					if recvType, ok := g.varTypes[recv.Value]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						if primAliases, ok := llvmTypeToNolang[srcType]; ok {
							candidates = append(candidates, primAliases...)
						}
						for _, cand := range candidates {
							shortName := cand + "." + dot.Property
							if g.funcRetTypes != nil {
								if _, ok := g.funcRetTypes[shortName]; ok {
									innerFnName = shortName
									break
								}
							}
						}
					}
				}
				// fallback
				if innerFnName == "" {
					innerFnName = candidate
				}
			} else if _, ok := dot.Receiver.(*parser.IndexExpression); ok {
				// 陣列元素接收者（如 names[i].slice(0, nlen)）
				// 透過 exprResultLLVMType 推導元素型別，再映射到 nolang 型別名查找方法
				elemType := g.exprResultLLVMType(dot.Receiver)
				srcType := strings.TrimPrefix(elemType, "%")
				candidates := []string{srcType}
				if primAliases, ok := llvmTypeToNolang[srcType]; ok {
					candidates = append(candidates, primAliases...)
				}
				for _, cand := range candidates {
					shortName := cand + "." + dot.Property
					if g.funcRetTypes != nil {
						if _, ok := g.funcRetTypes[shortName]; ok {
							innerFnName = shortName
							innerMethodRecv = dot.Receiver
							break
						}
					}
				}
			}
		}
		if g.funcRetTypes != nil {
			if t, ok := g.funcRetTypes[innerFnName]; ok {
				retType = t
				// 用戶自定義函數加 "n." 前綴以避免與 clib 系統調用衝突（僅限 clibFuncNames 內名稱）
				if clibFuncNames[innerFnName] {
					innerFnName = "n." + innerFnName
				}
			}
		}

		// builtin 方法（如 i64.to-str, net-dial, get-line）不走用戶函數的輸出參數慣例，
		// 返回值透過 LLVM return register。用 generateCallExpression 分派 builtin，
		// 並將返回值存入輸出變數。
		// For module-prefixed calls (e.g. fs.get-line), strip the prefix to find the builtin.
		if m := builtin.FindBuiltinMethod(innerFnName); m == nil && strings.Contains(innerFnName, ".") {
			if idx := strings.Index(innerFnName, "."); idx >= 0 {
				shortName := innerFnName[idx+1:]
				if m2 := builtin.FindBuiltinMethod(shortName); m2 != nil {
					m = m2
					innerFnName = shortName
				}
			}
		}
		if m := builtin.FindBuiltinMethod(innerFnName); m != nil && innerMethodRecv == nil {
			// 清空 lastBuiltinExtra（get-line 會設定它）
			g.lastBuiltinExtra = ""
			retReg := g.generateCallExpression(sb, innerCall)
			if retReg == "" {
				return ""
			}
			// 決定 store 型別
			storeType := "i64"
			if len(m.Return) > 0 {
				switch m.Return[0].String() {
				case "str":
					storeType = "%str-long"
				case "f64", "f32":
					storeType = "double"
				case "bool":
					storeType = "i1"
				}
			}
			// CLibCall builtins (如 i64.to-str) 透過 sprintf alloca 返回 %str-long* pointer。
			// ForwardFunc builtins (如 get-line) 透過 alloca 返回 %str-long* pointer。
			// 兩者都需要 load 成 %str-long value。
			// ForwardFunc builtins (如 net-dial) 返回 i64 value。
			actualVal := retReg
			if storeType == "%str-long" {
				// retReg 是 %str-long* (alloca)，需 load 成 %str-long value
				g.tmpIdx++
				loadReg := fmt.Sprintf("%%builtin.load.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, retReg))
				actualVal = loadReg
			}
			outIdx := 0
			for _, outArg := range expr.Arguments {
				if ident, ok := outArg.(*parser.Identifier); ok {
					varName := ident.Value
					curStoreType := storeType
					curVal := actualVal
					// 多返回值 builtin: 第二個輸出使用 lastBuiltinExtra (如 get-line 的 ok)
					// lastBuiltinExtra is already zext'd to i64 (Nolang bools are i64)
					if outIdx == 1 && g.lastBuiltinExtra != "" {
						curStoreType = "i64"
						curVal = g.lastBuiltinExtra
					}
					if _, exists := g.varTypes[varName]; !exists {
						g.varTypes[varName] = curStoreType
						g.tmpIdx++
						g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: curStoreType, Size: 8})
						sb.WriteString(fmt.Sprintf("%s%%%s = alloca %s\n", g.indent(), varName, curStoreType))
						sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 8, i8* %%%s)\n", g.indent(), varName))
					} else if existingType, ok := g.varTypes[varName]; ok && existingType != curStoreType {
						// 變數已存在但型別不同（如 ok 已宣告為 i1 但 builtin 回傳 i64）
						// 需將值轉換為變數的型別以避免 LLVM IR 型別不匹配（UB）
						if existingType == "i1" && curStoreType == "i64" {
							// i64 → i1: trunc
							g.tmpIdx++
							truncReg := fmt.Sprintf("%%builtin.trunc.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i1\n", g.indent(), truncReg, curVal))
							curVal = truncReg
							curStoreType = "i1"
						}
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %%%s\n", g.indent(), curStoreType, curVal, curStoreType, varName))
				}
				outIdx++
			}
			g.lastBuiltinExtra = ""
			return retReg
		}

		// 生成內層調用的參數（receiver 作為第一個參數）
		innerArgs := make([]string, 0)
		if innerMethodRecv != nil {
			if innerMethodRecv != nil {
				innerArgs = append(innerArgs, g.generateCallArg(sb, innerMethodRecv))
			}
		}
		for _, arg := range innerCall.Arguments {
			innerArgs = append(innerArgs, g.generateCallArg(sb, arg))
		}

		if retType == "void" {
			// void 返回：直接調用
			// 檢查是否為帶輸出參數的函數（curried 呼叫 → 單次呼叫，附加輸出參數）
			numResults := 0
			if g.funcNumResults != nil {
				// 嘗試多個名稱變體（可能已被 mangleOverloads 修飾）
				for _, name := range []string{innerFnName, innerFnName + "_i64_i64_i64_i64"} {
					if n, ok := g.funcNumResults[name]; ok && n > numResults {
						numResults = n
					}
				}
			}
			if numResults >= 1 {
				// 帶輸出參數：將輸出參數附加到呼叫，傳遞指標
				// 先取得輸出型別（用於 auto-allocate 未宣告的輸出變數）
				var outTypes []string
				if g.funcResultLLVMType != nil {
					for _, name := range []string{innerFnName, innerFnName + "_i64_i64_i64_i64"} {
						if ts, ok := g.funcResultLLVMType[name]; ok && len(ts) > 0 {
							outTypes = ts
							break
						}
					}
				}
				allArgs := make([]string, 0, len(innerArgs)+len(expr.Arguments))
				allArgs = append(allArgs, innerArgs...)
				for outIdx, outArg := range expr.Arguments {
					// Auto-allocate undeclared output variables (e.g. `total` in `.c.recv-all()(response, total)`)
					if ident, ok := outArg.(*parser.Identifier); ok {
						_, exists := g.varTypes[ident.Value]
						if !exists {
							outType := "i64"
							if outIdx < len(outTypes) {
								outType = outTypes[outIdx]
							}
							g.varTypes[ident.Value] = outType
							g.tmpIdx++
							g.funcVars = append(g.funcVars, varInfo{Name: ident.Value, Type: outType, Size: 8})
							if sb != nil {
								sb.WriteString(fmt.Sprintf("%s%%%s = alloca %s\n", g.indent(), ident.Value, outType))
								sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 8, i8* %%%s)\n", g.indent(), ident.Value))
							}
						}
					}
					allArgs = append(allArgs, g.generateCallArg(sb, outArg))
				}
				sb.WriteString(fmt.Sprintf("%scall void @%s(%s)\n", g.indent(), sanitizeLLVMName(innerFnName), strings.Join(allArgs, ", ")))
				return ""
			}
			// 純 void（無輸出參數）：直接調用
			sb.WriteString(fmt.Sprintf("%scall void @%s(%s)\n", g.indent(), sanitizeLLVMName(innerFnName), strings.Join(innerArgs, ", ")))
			return ""
		}

		// 有返回值：生成 call 並捕獲結果
		g.tmpIdx++
		retReg := fmt.Sprintf("%%callret.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = call %s @%s(%s)\n", g.indent(), retReg, retType, sanitizeLLVMName(innerFnName), strings.Join(innerArgs, ", ")))

		// 將返回值存入輸出參數變數
		for _, outArg := range expr.Arguments {
			if ident, ok := outArg.(*parser.Identifier); ok {
				varName := ident.Value
				if _, exists := g.varTypes[varName]; !exists {
					g.varTypes[varName] = retType
					g.tmpIdx++
					g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: retType, Size: 8})
					sb.WriteString(fmt.Sprintf("%s%%%s = alloca %s\n", g.indent(), varName, retType))
					sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 8, i8* %%%s)\n", g.indent(), varName))
				}
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %%%s\n", g.indent(), retType, retReg, retType, varName))
			}
		}
		return retReg
	}

	fnName := ""
	if ident, ok := expr.Function.(*parser.Identifier); ok {
		fnName = ident.Value
	} else if dot, ok := expr.Function.(*parser.DotExpression); ok {
		// 支援多段限定名（如 net.quic.quic-varint-encode）：
		// 鏈式 DotExpression 的 Receiver 本身也是 DotExpression，
		// 需遞迴展開為完整名稱。
		fnName = flattenDottedExpr(dot)
	}

	// 僅在用戶函數名稱與 C 系統調用（@open / @read / @write / @close / @mkdir /
	// @unlink / @rename / @stat / @chdir / @getcwd / @getenv / @getpid / @gethostname
	// / @malloc / @free / @memcpy / @memset / @printf / @sprintf / @strcmp
	// / @nolang.strlen / @fopen / @fgets / @fclose / @exit）衝突時，才加 "n."
	// 前綴以避免 redefinition。其他情況不前綴，保留與 builtin 的 dispatch 優先級。
	// 註：@atoi / @strtoull / @strtod / @time / @sleep 已移除——
	//   - str.to-i64 / str.to-u64 由 str.no Nolang 實作提供（返回 ?i64 / ?u64）
	//   - ffi-cstr-at-float / ffi-cstr-at-int 改用內部 @nolang.strtod / @nolang.strtoll
	//   - now 內建改用內部 @nolang.now_s（gettimeofday）
	//   - sleep 內建改用內部 @nolang.sleep_s（nanosleep）
	llvmFnName := fnName
	maybeRenableLLVMName := func() {
		if g.funcRetTypes != nil {
			if _, isUser := g.funcRetTypes[fnName]; isUser {
				if clibFuncNames[fnName] {
					llvmFnName = "n." + fnName
				} else {
					llvmFnName = fnName
				}
			} else {
				llvmFnName = fnName
			}
		} else {
			llvmFnName = fnName
		}
	}
	maybeRenableLLVMName()

	hasArgs := len(expr.Arguments) > 0

	// 共用閉包
	evalArgs := func() []string {
		result := make([]string, len(expr.Arguments))
		for i, arg := range expr.Arguments {
			result[i] = g.generateExprWithSB(sb, arg)
		}
		return result
	}
	strArg := func(a string) string {
		if strings.HasPrefix(a, "%") {
			return "i8* " + a
		}
		return a
	}
	llvmArg := func(val string) string {
		if strings.HasPrefix(val, "%") {
			return "i64 " + val
		}
		return "i64 " + val
	}

	// FFI extern 函式分派：在 builtin / 方法解析之前檢查，避免與同名項目衝突。
	// extern 函式名稱為使用者自選的 FFI 名稱（如 c-strlen、sqlite3-open），與
	// Nolang 方法名稱（如 str.to-upper）不會碰撞。
	if g.externFuncs != nil {
		if ext, ok := g.externFuncs[fnName]; ok {
			return g.callExtern(sb, ext, expr)
		}
	}

	// 通過 BuiltinMethodList 分派（LLVMIntrinsic / CLibCall / LLVMConv / ForwardFunc）
	// 注意：方法呼叫且 funcRetTypes 已有 Nolang 實作者，必須走後面的方法解析路徑，
	// 否則 CLibCall 會丟失輸出參數（如 .to-i64(n) 變成 atoi(...) 結果未存回 n）。
	// 處理兩種情況：
	//   1) expr.Function 仍是 DotExpression（如使用者直接呼叫 obj.method()）
	//   2) expr.Function 已被 transpiler 改寫為 Identifier，但 fnName 含 "." 且有 Nolang 實作
	skipBuiltin := false
	if dot, isDot := expr.Function.(*parser.DotExpression); isDot {
		if g.funcRetTypes != nil {
			if _, hasNolang := g.funcRetTypes[fnName]; hasNolang {
				skipBuiltin = true
			} else if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				// 根據 varTypes 取得 receiver 的實際型別前綴查找方法
				// 例如 archive.read(i) → archive 型別為 %tar → 查找 tar.read
				if g.varTypes != nil {
					recvVarName := recv.Value
					if recvType, ok := g.varTypes[recvVarName]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						if primAliases, ok := llvmTypeToNolang[srcType]; ok {
							candidates = append(candidates, primAliases...)
						}
						for _, cand := range candidates {
							candName := cand + "." + dot.Property
							if _, hasNolang := g.funcRetTypes[candName]; hasNolang {
								// ForwardFunc built-ins (如 fs.read) 的 Nolang 函數體為空，
								// 真正實作在 LLVM intrinsic 中。struct 方法（如 tar.read）有
								// 完整 Nolang 實作，不是 ForwardFunc，應跳過 builtin 路徑。
								isForwardBuiltin := false
								if m := builtin.FindBuiltinMethod(candName); m != nil && m.ForwardFunc != "" {
									isForwardBuiltin = true
								}
								if !isForwardBuiltin {
									skipBuiltin = true
									// Update fnName and llvmFnName to the resolved method
									// name (e.g. archive.type → tar.type) so the regular
									// call path generates @tar.type, not @archive.type.
									fnName = candName
									llvmFnName = candName
									break
								}
							}
						}
					}
				}
			}
		}
	} else if _, isIdent := expr.Function.(*parser.Identifier); isIdent {
		// transpiler 已將 .method() 改寫為 method()，fnName 含 "." 時視為方法呼叫
		if strings.Contains(fnName, ".") && g.funcRetTypes != nil {
			if _, hasNolang := g.funcRetTypes[fnName]; hasNolang {
				// ForwardFunc built-ins (e.g. gzip-decompress, gzip-compress) have
				// special LLVM code generation in callBuiltin that must be used.
				// The Nolang function definition has an empty body — the real
				// implementation is the built-in LLVM intrinsic.
				isForwardBuiltin := false
				m := builtin.FindBuiltinMethod(fnName)
				if m == nil {
					if idx := strings.Index(fnName, "."); idx >= 0 {
						m = builtin.FindBuiltinMethod(fnName[idx+1:])
					}
				}
				if m != nil && m.ForwardFunc != "" {
					isForwardBuiltin = true
				}
				if !isForwardBuiltin {
					skipBuiltin = true
				}
			}
		}
		// 用戶自定義頂層函數（包含 std 模組內的 fs.open / fs.open-write 等）
		// 優先於同名 builtin：使用者呼叫 `open(path)` 時若存在用戶自定義函數，
		// 應優先使用，否則 fs 構造函數會被 clib 系統調用遮蔽。
		if !skipBuiltin && g.funcRetTypes != nil {
			if _, hasNolang := g.funcRetTypes[fnName]; hasNolang {
				// Same ForwardFunc check as above for non-dotted names
				isForwardBuiltin := false
				m := builtin.FindBuiltinMethod(fnName)
				if m != nil && m.ForwardFunc != "" {
					isForwardBuiltin = true
				}
				if !isForwardBuiltin {
					skipBuiltin = true
				}
			}
		}
	}
	if !skipBuiltin {
		m := builtin.FindBuiltinMethod(fnName)
		if m == nil && strings.Contains(fnName, ".") {
			// Try stripping module prefix (e.g. "fs.is-dir" → "is-dir").
			// Safe because skipBuiltin already verified no user function exists with the full name.
			if idx := strings.Index(fnName, "."); idx >= 0 {
				shortName := fnName[idx+1:]
				if m2 := builtin.FindBuiltinMethod(shortName); m2 != nil {
					m = m2
					fnName = shortName
				}
			}
		}
		if m != nil {
			if m.LLVMIntrinsic != "" {
				// 對 method call（如 r2.sqrt()），receiver 是第一個參數
				// expr.Arguments 不含 receiver，需從 DotExpression 補上
				methodArgs := expr.Arguments
				if dot, isDot := expr.Function.(*parser.DotExpression); isDot {
					methodArgs = append([]parser.Expression{dot.Receiver}, expr.Arguments...)
				}
				args := make([]string, len(methodArgs))
				for i, arg := range methodArgs {
					v := g.generateExprWithSB(sb, arg)
					// Only coerce integer types to double; leave non-integer (string, etc.) as 0.0
					if g.intExprLLVMType(arg) != "" && !g.isStringExpr(arg) {
						v = g.coerceToFloatReg(sb, v, arg, "double")
					} else if g.isStringExpr(arg) {
						// String argument to log() intrinsic: use 0.0 placeholder
						v = "0.0"
					}
					args[i] = v
				}
				argStr := ""
				for i, v := range args {
					if i > 0 {
						argStr += ", "
					}
					argStr += "double " + v
				}
				return fmt.Sprintf("call double @%s(%s)", m.LLVMIntrinsic, argStr)
			}
			if m.CLibCall != nil {
				return g.genCLibCall(sb, m, evalArgs, expr.Arguments)
			}
			if m.LLVMConv != nil {
				return g.genLLVMConv(sb, m, evalArgs)
			}
			// ForwardFunc: str-copy→memcpy, str-eq→memcmp, str-fill→memset
			if m.ForwardFunc != "" {
				if r := g.genForwardFunc(sb, m.ForwardFunc, expr, nil); r != "" || m.ForwardFunc == "memcpy" || m.ForwardFunc == "memset" || m.ForwardFunc == "str-clear" {
					return r
				}
				// If genForwardFunc didn't handle it, try callBuiltin with the
				// ForwardFunc name (e.g. arg→args-get, args→args-count are
				// implemented in callBuiltin, not genForwardFunc).
				if r := g.callBuiltin(sb, m.ForwardFunc, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg, expr); r != "" {
					return r
				}
			}
		}
	}

	// 嘗試各 domain handler
	if r := g.callFmt(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg, expr); r != "" {
		return r
	}
	if r := g.callStrconv(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg); r != "" {
		return r
	}
	if r := g.callBuiltin(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg, expr); r != "" {
		return r
	}
	// sort-asc / sort-desc 直接在 call.go 處理（無需 call_stdlib 函數）
	if (fnName == "sort-asc" || fnName == "sort-desc") && hasArgs && len(expr.Arguments) >= 2 {
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s; %s not yet implemented for LLVM target\n", g.indent(), fnName))
		}
		return ""
	}

	// val(), ok() and err() are handled at the assignment level
	if fnName == "val" || fnName == "err" || fnName == "ok" {
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s; %s() is only valid in assignment context\n", g.indent(), fnName))
		}
		return "0"
	}

	// For DotExpression-based receiver calls (e.g. a.is-inf), try to resolve
	// as a union type method call. The receiver variable's type is looked up
	// in g.varTypes and matched against union type aliases.
	// If resolution succeeds, track the receiver so it can be passed as self.
	var methodReceiver parser.Expression = nil
	if dot, ok := expr.Function.(*parser.DotExpression); ok {
		receiverExpr := dot.Receiver
		// Unwrap GroupedExpression: (123).to-str() → 123.to-str()
		if ge, ok := receiverExpr.(*parser.GroupedExpression); ok {
			receiverExpr = ge.Expression
		}
		if recv, ok := receiverExpr.(*parser.Identifier); ok {
			if recvType, ok := g.varTypes[recv.Value]; ok && g.unionAliases != nil {
				// Map LLVM type name back to source type name
				srcType := recvType
				if srcType == "double" {
					srcType = "f64"
				} else if srcType == "float" {
					srcType = "f32"
				}
				for aliasName := range g.unionAliases {
					if !g.isMemberOfUnionTransitive(srcType, aliasName, make(map[string]bool)) {
						continue
					}
					// Try monomorphized name first: unionAlias.methodName__memberType
					monoName := aliasName + "." + dot.Property + "__" + srcType
					if _, exists := g.funcRetTypes[monoName]; exists {
						fnName = monoName
						methodReceiver = recv
						break
					}
					// Try non-monomorphized name: unionAlias.methodName
					unionName := aliasName + "." + dot.Property
					if _, exists := g.funcRetTypes[unionName]; exists {
						fnName = unionName
						methodReceiver = recv
						break
					}
				}
			}
			// Also resolve str/arr/vec/char/byte/bool receiver methods (e.g. s.index → str.index, c.is-alpha → char.is-alpha)
			if methodReceiver == nil {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					// Option type (?T): try the inner type as a candidate
					// (e.g. conn-val is ?str → try str.to-lower)
					if srcType == "option" && g.optionInnerTypes != nil {
						if innerType, ok := g.optionInnerTypes[recv.Value]; ok {
							innerSrc := strings.TrimPrefix(innerType, "%")
							candidates = append(candidates, innerSrc)
							if primAliases, ok := llvmTypeToNolang[innerSrc]; ok {
								candidates = append(candidates, primAliases...)
							}
						}
					}
					// Primitive LLVM types may correspond to multiple nolang type names.
					// For example, i32 can be char, i32, or u32. Try all candidates.
					if primAliases, ok := llvmTypeToNolang[srcType]; ok {
						candidates = append(candidates, primAliases...)
					}
					// vec/arr 變數：依元素型別構造 []T 候選（如 opened.to-str → []byte.to-str）
					// Also handle raw LLVM array types like "[32 x i8]" (fixed-size array
					// result parameters typed directly as [N x T] rather than %arr struct).
					if srcType == "vec" || srcType == "arr" || strings.HasPrefix(srcType, "[") {
						if g.arrayElemTypes != nil {
							if et, ok := g.arrayElemTypes[recv.Value]; ok {
								if elemAliases, ok := llvmTypeToNolang[et]; ok {
									for _, alias := range elemAliases {
										candidates = append(candidates, "[]"+alias)
									}
								}
							}
						}
					}
					for _, cand := range candidates {
						shortName := cand + "." + dot.Property
						if g.funcRetTypes != nil {
							// 接受任何已註冊的用戶方法（含 void 無輸出參數的方法，如 process.close）。
							if _, ok := g.funcRetTypes[shortName]; ok {
								fnName = shortName
								methodReceiver = recv
								break
							}
							// Fallback: try mangled name from mangleOverloads.
							// When duplicate method definitions exist (e.g. user code + auto-imported
							// std module), mangleOverloads appends parameter type suffixes:
							// "json-pool.alloc" → "json-pool.alloc_json-pool"
							mangledName := shortName + "_" + cand
							if _, ok := g.funcRetTypes[mangledName]; ok {
								fnName = mangledName
								methodReceiver = recv
								break
							}
						}
						// Also check build-in methods (e.g., str.eq, str.copy, i64.to-str)
						if methodReceiver == nil {
							if m := builtin.FindBuiltinMethod(shortName); m != nil {
								fnName = shortName
								methodReceiver = recv
								break
							}
						}
					}
				}
			}
		} else if _, ok := receiverExpr.(*parser.StringLiteral); ok {
			// 字符串字面量接收者（如 'abc'.compare('abc')）
			// 字符串字面量永遠是 str 型別，直接嘗試 str.<property>
			shortName := "str." + dot.Property
			if g.funcRetTypes != nil {
				if _, ok := g.funcRetTypes[shortName]; ok {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
			// Also check build-in methods (e.g., 'hello'.eq(b, n))
			if methodReceiver == nil {
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
		} else if _, ok := receiverExpr.(*parser.IntegerLiteral); ok {
			// 整數字面量接收者（如 123.to-str()）
			// 整數字面量預設為 i64 型別
			shortName := "i64." + dot.Property
			if g.funcRetTypes != nil {
				if _, ok := g.funcRetTypes[shortName]; ok {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
			if methodReceiver == nil {
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
		} else if _, ok := receiverExpr.(*parser.FloatLiteral); ok {
			// 浮點字面量接收者（如 3.14.to-str()）
			// 浮點字面量預設為 f64 型別
			shortName := "f64." + dot.Property
			if g.funcRetTypes != nil {
				if _, ok := g.funcRetTypes[shortName]; ok {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
			if methodReceiver == nil {
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
		} else if _, ok := receiverExpr.(*parser.BooleanLiteral); ok {
			// 布爾字面量接收者（如 true.to-str()）
			// 布爾字面量預設為 bool 型別
			shortName := "bool." + dot.Property
			if g.funcRetTypes != nil {
				if _, ok := g.funcRetTypes[shortName]; ok {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
			if methodReceiver == nil {
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
		} else if group, ok := receiverExpr.(*parser.GroupedExpression); ok {
			// 帶括號的表達式接收者（如 (-42).to-str()、(-9223372036854775807 - 1).to-str()）
			// 嘗試解析內部表達式的型別
			if infix, ok := group.Expression.(*parser.InfixExpression); ok {
				// 算術表達式視為 i64
				shortName := "i64." + dot.Property
				if g.funcRetTypes != nil {
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						methodReceiver = receiverExpr
					}
				}
				if methodReceiver == nil {
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						fnName = shortName
						methodReceiver = receiverExpr
					}
				}
				_ = infix
			} else if _, ok := group.Expression.(*parser.IntegerLiteral); ok {
				shortName := "i64." + dot.Property
				if g.funcRetTypes != nil {
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						methodReceiver = receiverExpr
					}
				}
				if methodReceiver == nil {
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						fnName = shortName
						methodReceiver = receiverExpr
					}
				}
			}
		} else if infix, ok := receiverExpr.(*parser.InfixExpression); ok {
			// 帶括號的算術表達式接收者（已被 L478-479 unwrap），
			// 如 (-9223372036854775807 - 1).to-str() 在 L478 unwrap 後變為 InfixExpression。
			// 算術表達式視為 i64。
			_ = infix
			shortName := "i64." + dot.Property
			if g.funcRetTypes != nil {
				if _, ok := g.funcRetTypes[shortName]; ok {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
			if methodReceiver == nil {
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
		} else if _, ok := receiverExpr.(*parser.PrefixExpression); ok {
			// 前綴表達式接收者（如 -42.to-str()，無括號）
			// 負整數字面量被解析為 PrefixExpression(-, IntegerLiteral)
			// 視為 i64 型別
			shortName := "i64." + dot.Property
			if g.funcRetTypes != nil {
				if _, ok := g.funcRetTypes[shortName]; ok {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
			if methodReceiver == nil {
				if m := builtin.FindBuiltinMethod(shortName); m != nil {
					fnName = shortName
					methodReceiver = receiverExpr
				}
			}
		} else if _, ok := receiverExpr.(*parser.IndexExpression); ok {
			// 陣列元素接收者（如 names[i].slice(0, nlen)）
			// 透過 exprResultLLVMType 推導元素型別，再映射到 nolang 型別名查找方法
			elemType := g.exprResultLLVMType(receiverExpr)
			srcType := strings.TrimPrefix(elemType, "%")
			candidates := []string{srcType}
			if primAliases, ok := llvmTypeToNolang[srcType]; ok {
				candidates = append(candidates, primAliases...)
			}
			for _, cand := range candidates {
				shortName := cand + "." + dot.Property
				if g.funcRetTypes != nil {
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
				if methodReceiver == nil {
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
			}
		} else if _, ok := receiverExpr.(*parser.DotExpression); ok {
			// 結構欄位接收者（如 c.name.trim()、self.buf.len）
			// 透過 exprResultLLVMType 推導欄位型別，再映射到 nolang 型別名查找方法
			elemType := g.exprResultLLVMType(receiverExpr)
			srcType := strings.TrimPrefix(elemType, "%")
			candidates := []string{srcType}
			if primAliases, ok := llvmTypeToNolang[srcType]; ok {
				candidates = append(candidates, primAliases...)
			}
			for _, cand := range candidates {
				shortName := cand + "." + dot.Property
				if g.funcRetTypes != nil {
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
				if methodReceiver == nil {
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
			}
		} else if _, ok := receiverExpr.(*parser.SliceExpression); ok {
			// 切片結果接收者（如 buf[pos..end].to-str()）
			// 透過 exprResultLLVMType 推導切片結果型別，再映射到 nolang 型別名查找方法
			elemType := g.exprResultLLVMType(receiverExpr)
			srcType := strings.TrimPrefix(elemType, "%")
			candidates := []string{srcType}
			if primAliases, ok := llvmTypeToNolang[srcType]; ok {
				candidates = append(candidates, primAliases...)
			}
			// vec/arr 切片：依元素型別構造 []T 候選（如 []byte.to-str）
			if srcType == "vec" || srcType == "arr" {
				if sliceExpr, ok := receiverExpr.(*parser.SliceExpression); ok {
					if ident, ok := sliceExpr.Left.(*parser.Identifier); ok {
						if g.arrayElemTypes != nil {
							if et, ok := g.arrayElemTypes[ident.Value]; ok {
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
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
				if methodReceiver == nil {
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
			}
		} else if _, ok := receiverExpr.(*parser.CallExpression); ok {
			// 函數呼叫結果接收者（如 foo().trim()）
			// 透過 exprResultLLVMType 推導返回型別，再映射到 nolang 型別名查找方法
			elemType := g.exprResultLLVMType(receiverExpr)
			srcType := strings.TrimPrefix(elemType, "%")
			candidates := []string{srcType}
			if primAliases, ok := llvmTypeToNolang[srcType]; ok {
				candidates = append(candidates, primAliases...)
			}
			for _, cand := range candidates {
				shortName := cand + "." + dot.Property
				if g.funcRetTypes != nil {
					if _, ok := g.funcRetTypes[shortName]; ok {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
				if methodReceiver == nil {
					if m := builtin.FindBuiltinMethod(shortName); m != nil {
						fnName = shortName
						methodReceiver = receiverExpr
						break
					}
				}
			}
		}
	}

	// 方法解析後，檢查是否為 build-in 方法（如 str.eq、str.copy、i64.to-str、f64.to-str）
	// 此時 fnName 已解析為型別名 + 屬性（如 "str.eq"），methodReceiver 為接收者表達式。
	// build-in 方法不在 funcRetTypes 中，需透過 FindBuiltinMethod 查找並分派。
	// 若 Nolang 中已有同名方法（funcRetTypes 中存在），優先使用 Nolang 實作。
	if methodReceiver != nil {
		// 方法解析後，fnName 可能已被更新為型別前綴名稱（如 char.is-alpha），
		// 重新計算 llvmFnName 以確保與最終 fnName 一致。
		maybeRenableLLVMName()
		_, hasNolangImpl := g.funcRetTypes[fnName]
		if !hasNolangImpl {
			if m := builtin.FindBuiltinMethod(fnName); m != nil {
				if m.ForwardFunc != "" {
					if r := g.genForwardFunc(sb, m.ForwardFunc, expr, methodReceiver); r != "" || m.ForwardFunc == "memcpy" || m.ForwardFunc == "memset" || m.ForwardFunc == "arr-zero" || m.ForwardFunc == "str-clear" || m.ForwardFunc == "vec-clear" || m.ForwardFunc == "vec-push" {
						return r
					}
				}
				if m.CLibCall != nil {
					// 構建包含 receiver 的參數列表
					methodEvalArgs := func() []string {
						allArgs := append([]parser.Expression{methodReceiver}, expr.Arguments...)
						result := make([]string, len(allArgs))
						for i, arg := range allArgs {
							result[i] = g.generateExprWithSB(sb, arg)
						}
						return result
					}
					allExprs := append([]parser.Expression{methodReceiver}, expr.Arguments...)
					return g.genCLibCall(sb, m, methodEvalArgs, allExprs)
				}
				if m.LLVMConv != nil {
					methodEvalArgs := func() []string {
						allArgs := append([]parser.Expression{methodReceiver}, expr.Arguments...)
						result := make([]string, len(allArgs))
						for i, arg := range allArgs {
							result[i] = g.generateExprWithSB(sb, arg)
						}
						return result
					}
					return g.genLLVMConv(sb, m, methodEvalArgs)
				}
			}
		}
	}

	// Intercept .zero() calls that were rewritten by the transpiler
	// (e.g. [4]i64.zero(data) → _LB_4_RB_i64.zero). If the function doesn't
	// exist in funcRetTypes, generate llvm.memset directly.
	if strings.HasSuffix(fnName, ".zero") && g.funcRetTypes != nil {
		if _, exists := g.funcRetTypes[fnName]; !exists {
			if len(expr.Arguments) > 0 {
				if r := g.genArrZero(sb, expr.Arguments[0]); r {
					return ""
				}
			}
		}
	}

	// Default: call @funcName(args) — 引用傳遞模式
	// 每個參數傳遞指標（不 eval，避免輸出參數產生多餘 load）
	retType := "void"
	if g.funcRetTypes != nil {
		if t, ok := g.funcRetTypes[fnName]; ok {
			retType = t
		}
	}
	// Determine if the function has a single named result (output parameter passed as last arg)
	// Convention: for single-result functions, the last argument is the output parameter
	// if it's an Identifier (a variable to store the result into) AND there are more args
	// than the function's declared parameter count.
	// Nolang functions (those in funcResultLLVMType) always use void + output param,
	// so they also have hasOutputParam even when retType != "void".
	isNolangSingleResult := false
	if g.funcResultLLVMType != nil {
		if _, ok := g.funcResultLLVMType[fnName]; ok {
			if g.funcNumResults != nil {
				if n, ok := g.funcNumResults[fnName]; ok && n == 1 {
					isNolangSingleResult = true
				}
			}
		}
	}
	// 用戶自定義函數（has funcResultLLVMType）一律使用 void + by-reference 輸出，
	// 即使 funcRetTypes 中存的是語意回傳型別（如 %option / i64），LLVM 簽名仍是 void。
	// 避免 call i64 @funcname(...) 與 void 函數簽名不一致。
	if isNolangSingleResult {
		retType = "void"
	}
	hasOutputParam := false
	if g.funcNumResults != nil {
		triggerHasOutput := retType != "void" || isNolangSingleResult
		if n, ok := g.funcNumResults[fnName]; ok && n == 1 && triggerHasOutput {
			if len(expr.Arguments) > 0 {
				if _, ok := expr.Arguments[len(expr.Arguments)-1].(*parser.Identifier); ok {
					// Only treat as output param if args > function params
					paramCount := len(expr.Arguments)
					if g.funcParamCount != nil {
						if pc, ok := g.funcParamCount[fnName]; ok {
							paramCount = pc
						}
					}
					if len(expr.Arguments) > paramCount {
						hasOutputParam = true
					}
				}
			}
		}
	}

	// 單輸出參數：調用方未顯式傳遞輸出變數（如 v1 = s.to-i64()）。
	// Nolang 函數的 funcRetTypes 為語意回傳型別（如 %option），但實際 LLVM 簽名是 void + 輸出指標。
	// 此類函數需分配臨時空間、傳遞指標、調用後載入結果作為返回值。
	// 注意：啟發式檢測的輸出參數（funcHeuristicOutput）已存在於 fd.Parameters 中，
	// 函數定義已將其作為常規 LLVM 參數生成，調用方已傳遞，不應再加返回槽。
	voidSingleOutput := false
	voidSingleOutputType := ""
	triggerVoidSingle := (retType == "void" || isNolangSingleResult) && g.funcNumResults != nil && g.funcResultLLVMType != nil
	if triggerVoidSingle {
		if n, ok := g.funcNumResults[fnName]; ok && n == 1 {
			if ts, ok := g.funcResultLLVMType[fnName]; ok && len(ts) == 1 {
				if h, ok := g.funcHeuristicOutput[fnName]; ok && h {
					// 啟發式輸出：參數已在 fd.Parameters 中，調用方已傳遞，不加返回槽
				} else {
					voidSingleOutput = true
					voidSingleOutputType = ts[0]
				}
			}
		}
	}

	// Separate input args from output param
	var inputArgs []parser.Expression
	var outputArg parser.Expression
	if hasOutputParam && len(expr.Arguments) > 0 {
		inputArgs = expr.Arguments[:len(expr.Arguments)-1]
		outputArg = expr.Arguments[len(expr.Arguments)-1]
	} else {
		inputArgs = expr.Arguments
	}

	// For variadic functions, separate non-variadic and variadic args
	isVariadic := false
	nonVariadicCount := 0
	if g.funcIsVariadic != nil {
		isVariadic = g.funcIsVariadic[fnName]
		nonVariadicCount = g.funcParamCount[fnName]
	}

	var nonVariadicArgs []parser.Expression
	var variadicArgs []parser.Expression
	if isVariadic {
		if len(inputArgs) > nonVariadicCount {
			nonVariadicArgs = inputArgs[:nonVariadicCount]
			variadicArgs = inputArgs[nonVariadicCount:]
		} else {
			nonVariadicArgs = inputArgs
		}
	} else {
		nonVariadicArgs = inputArgs
	}

	// For resolved DotExpression method calls (union alias resolution),
	// prepend the receiver variable as the self parameter.
	if methodReceiver != nil {
		nonVariadicArgs = append([]parser.Expression{methodReceiver}, nonVariadicArgs...)
	}

	// 參數默認值填充：若提供的參數數量少於函數聲明的參數數量，
	// 使用默認值表達式補齊缺失的參數。
	if g.funcParamDefaults != nil && !isVariadic {
		if defaults, ok := g.funcParamDefaults[fnName]; ok {
			paramCount := len(defaults)
			if methodReceiver != nil {
				// receiver 已 prepend 到 nonVariadicArgs，對齊 defaults 的偏移
				providedCount := len(nonVariadicArgs) - 1 // 減去 receiver
				for i := providedCount; i < paramCount-1 && i >= 0; i++ {
					// defaults[i+1] 對應非 receiver 參數（defaults[0] 是 receiver slot）
					if i+1 < len(defaults) && defaults[i+1] != nil {
						nonVariadicArgs = append(nonVariadicArgs, defaults[i+1])
					}
				}
			} else {
				providedCount := len(nonVariadicArgs)
				for i := providedCount; i < paramCount && i >= 0; i++ {
					if defaults[i] != nil {
						nonVariadicArgs = append(nonVariadicArgs, defaults[i])
					}
				}
			}
		}
	}

	// genTypedArg generates a typed pointer argument for a single expression
	genTypedArg := func(arg parser.Expression, argIdx int) string {
		// DotExpression as method receiver (self): pass field pointer by-reference
		// so the method can modify the struct field directly (e.g. c.inner.set-value(100))
		if dot, ok := arg.(*parser.DotExpression); ok && argIdx == 0 && methodReceiver != nil {
			ptr := g.generateExprPtr(sb, dot)
			if ptr != "" {
				recvType := g.exprResultLLVMType(dot)
				if recvType != "" {
					return recvType + "* " + ptr
				}
				return ptr
			}
			// Fallback: generate value and store in temp alloca
		}
		switch a := arg.(type) {
		case *parser.Identifier:
			// Enum variant: allocate temp i64 and store the constant tag index
			// 局部變數/參數優先於枚舉變體（避免名稱遮蔽：如 json-kind 的 num 變體
			// 與局部變數 num 衝突）
			// 但被重新賦值的變數（如 SHA 測試中的 h0）必須作為變數載入，非常量
			if g.enumVariantIndex != nil && (g.funcLocalNames == nil || !g.funcLocalNames[a.Value]) {
				if g.reassignedVars == nil || !g.reassignedVars[a.Value] {
					if tagIdx, ok := g.enumVariantIndex[a.Value]; ok {
						g.tmpIdx++
						tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
							sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), tagIdx, tmpName))
						}
						return "i64* " + tmpName
					}
				}
			}
			// Function reference: when an Identifier refers to a known user-defined
			// function, pass it as a function pointer (by-reference convention).
			// Function names may also appear in g.varTypes as "i64" (a placeholder from
			// collectVarDecls), so we check funcRetTypes first to distinguish functions
			// from real variables.
			// But local variables/parameters (in funcLocalNames) shadow global function
			// names — e.g. test-f32-to-str has local `f f32` which must not be confused
			// with the global `f` function from des.no.
			isLocalVar := g.funcLocalNames != nil && g.funcLocalNames[a.Value]
			if !isLocalVar && g.funcRetTypes != nil {
				if _, isFn := g.funcRetTypes[a.Value]; isFn {
					g.tmpIdx++
					tmpName := fmt.Sprintf("%%fnptr.arg.%d", g.tmpIdx)
					fnLLVMName := a.Value
					if clibFuncNames[a.Value] {
						fnLLVMName = "n." + a.Value
					}
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = alloca void (...)*\n", g.indent(), tmpName))
						sb.WriteString(fmt.Sprintf("%sstore void (...)* @%s, void (...)** %s\n", g.indent(), sanitizeLLVMName(fnLLVMName), tmpName))
					}
					return "void (...)** " + tmpName
				}
			}
			// str 型別用 %str-long* 指標
			if g.varTypes != nil {
				// Slice view variable as method receiver or argument
				if g.isSliceViewVar(a.Value) {
					view := g.sliceViews[a.Value]
					matPtr := g.materializeSliceView(sb, a.Value)
					if view.isStr {
						return "%str-long* " + matPtr
					}
					return "%vec* " + matPtr
				}
				if t, ok := g.varTypes[a.Value]; ok && t == "%str-long" {
					return "%str-long* " + g.varAddr(a.Value)
				}
			}
			// 陣列型別用正確的指標型別
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
					return t + "* " + g.varAddr(a.Value)
				}
			}
			// 浮點型別
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && (t == "double" || t == "float") {
					return t + "* " + g.varAddr(a.Value)
				}
			}
			// %arr (fixed-size array struct) → extract data pointer and bitcast to [N x T]*
			// Function parameters with [N]T type use raw LLVM array [N x T], but local
			// variables are stored as %arr struct { len, data* }. Need to extract data
			// pointer and bitcast to match the expected parameter type.
			// When the parameter expects %vec (slice), convert %arr { len, data* }
			// to %vec { len, cap, data* } by setting cap = len.
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && t == "%arr" {
					// Check if parameter expects %vec (slice)
					paramLLVMType := ""
					if g.funcParamLLVMTypes != nil {
						if types, ok := g.funcParamLLVMTypes[fnName]; ok && argIdx < len(types) {
							paramLLVMType = types[argIdx]
						}
					}
					if paramLLVMType == "%vec" {
						// Convert %arr { len, data* } → %vec { len, cap=len, data* }
						if sb != nil {
							g.tmpIdx++
							vecTmp := fmt.Sprintf("%%arr.vec.tmp.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecTmp))
							// vec.len = arr.len
							g.tmpIdx++
							arrLenGEP := fmt.Sprintf("%%arr.len.gep.%d", g.tmpIdx)
							g.tmpIdx++
							arrLenLoad := fmt.Sprintf("%%arr.len.val.%d", g.tmpIdx)
							g.tmpIdx++
							vecLenGEP := fmt.Sprintf("%%vec.len.gep.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n", g.indent(), arrLenGEP, g.varAddr(a.Value)))
							sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), arrLenLoad, arrLenGEP))
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), vecLenGEP, vecTmp))
							sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), arrLenLoad, vecLenGEP))
							// vec.cap = arr.len
							g.tmpIdx++
							vecCapGEP := fmt.Sprintf("%%vec.cap.gep.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), vecCapGEP, vecTmp))
							sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), arrLenLoad, vecCapGEP))
							// vec.data = arr.data
							g.tmpIdx++
							arrDataGEP := fmt.Sprintf("%%arr.data.gep.%d", g.tmpIdx)
							g.tmpIdx++
							arrDataLoad := fmt.Sprintf("%%arr.data.val.%d", g.tmpIdx)
							g.tmpIdx++
							vecDataGEP := fmt.Sprintf("%%vec.data.gep.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), arrDataGEP, g.varAddr(a.Value)))
							arrDataLoad = g.loadDataPtrField(sb, arrDataGEP)
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), vecDataGEP, vecTmp))
							g.storeDataPtrField(sb, arrDataLoad, vecDataGEP)
							return "%vec* " + vecTmp
						}
						return "%vec* " + g.varAddr(a.Value)
					}
					if elemType, ok := g.arrayElemTypes[a.Value]; ok {
						if arrSize, ok := g.arraySizes[a.Value]; ok {
							rawArrType := fmt.Sprintf("[%d x %s]", arrSize, elemType)
							if sb != nil {
								g.tmpIdx++
								dataGEP := fmt.Sprintf("%%arr.arg.gep.%d", g.tmpIdx)
								g.tmpIdx++
								dataLoad := fmt.Sprintf("%%arr.arg.data.%d", g.tmpIdx)
								g.tmpIdx++
								dataCast := fmt.Sprintf("%%arr.arg.cast.%d", g.tmpIdx)
								sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, g.varAddr(a.Value)))
								dataLoad = g.loadDataPtrField(sb, dataGEP)
								sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dataCast, dataLoad, rawArrType))
								return rawArrType + "* " + dataCast
							}
							return rawArrType + "* " + g.varAddr(a.Value)
						}
					}
				}
			}
			// 使用實際型別（不再硬編碼為 i64*）
			argType := "i64"
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok {
					argType = t
				}
			}
			// 對 by-reference 函數呼叫，先將引數值存到暫存變數，
			// 避免被呼叫函數修改原始變數（例如 gcd 會修改 a, b）
			// Nolang 帶 result parameter 的函數 retType 也是 "void"，需額外檢查 isNolangSingleResult
			if (retType != "void" || isNolangSingleResult) && g.isIntegerLLVMType(argType) {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%arg.save.%d", g.tmpIdx)
				g.tmpIdx++
				tmpVal := fmt.Sprintf("%%arg.val.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, argType))
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), tmpVal, argType, argType, g.varAddr(a.Value)))
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), argType, tmpVal, argType, tmpName))
				}
				return argType + "* " + tmpName
			}
			return argType + "* " + g.varAddr(a.Value)
		case *parser.FloatLiteral:
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), fmt.Sprintf("%f", a.Value), tmpName))
			}
			return "double* " + tmpName
		case *parser.StringLiteral:
			ev := g.generateExprWithSB(sb, arg)

			return "%str-long* " + ev
		case *parser.IntegerLiteral:
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			elemType := "i64"
			if g.funcParamLLVMTypes != nil {
				if types, ok := g.funcParamLLVMTypes[fnName]; ok && argIdx < len(types) {
					if types[argIdx] == "i32" {
						elemType = "i32"
					}
				}
			}
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, elemType))
				sb.WriteString(fmt.Sprintf("%sstore %s %d, %s* %s\n", g.indent(), elemType, a.Value, elemType, tmpName))
			}
			return elemType + "* " + tmpName
		case *parser.IndexExpression:
			ev := g.generateExprWithSB(sb, arg)
			// 使用 exprResultLLVMType 推導元素型別（支援 Identifier 和 DotExpression receiver）
			elemLLVMType := g.exprResultLLVMType(arg)
			if elemLLVMType == "" {
				elemLLVMType = "i64"
			}
			if strings.Contains(ev, ".gep.") || strings.Contains(ev, ".elem.") {
				// GEP result is a pointer
				return elemLLVMType + "* " + ev
			}
			// 若 SSA 值為 i64（如 zext 後的 byte/int32）但元素型別為 i8/i16/i32，需 trunc
			storeVal := ev
			if strings.HasPrefix(ev, "%") && g.isIntegerLLVMType(elemLLVMType) {
				// generateIndexExpression always zexts narrow integer elements to i64,
				// so the SSA value type is i64 regardless of elemLLVMType.
				// When elemLLVMType is a narrow integer (i8/i16/i32), trunc to elemLLVMType.
				srcType := "i64"
				if srcType != elemLLVMType {
					g.tmpIdx++
					convReg := fmt.Sprintf("%%ref.conv.%d", g.tmpIdx)
					if sb != nil {
						if srcType == "i64" {
							// i64 → smaller type: trunc
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, srcType, ev, elemLLVMType))
						} else if elemLLVMType == "i64" {
							// smaller type → i64: zext
							sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), convReg, srcType, ev))
						} else {
							// smaller → smaller (both non-i64): trunc to target
							sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, srcType, ev, elemLLVMType))
						}
					}
					storeVal = convReg
				}
			}
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, elemLLVMType))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemLLVMType, storeVal, elemLLVMType, tmpName))
			}
			return elemLLVMType + "* " + tmpName
		case *parser.SliceExpression:
			ev := g.generateExprWithSB(sb, arg)
			ptrType := "%vec*"
			if ident, ok := a.Left.(*parser.Identifier); ok {
				if g.varTypes != nil {
					if t, ok := g.varTypes[ident.Value]; ok {
						if t == "%str-long" {
							ptrType = "%str-long*"
						}
					}
				}
			}
			return ptrType + " " + ev
		case *parser.StructLiteral:
			// 結構體字面量：分配 temp slot 並依序 store 欄位
			structName := a.Type
			if structName == "" {
				// 匿名結構體：從函數簽名推斷 struct 名稱
				if g.funcParamTypes != nil {
					// 嘗試 fnName 以及去除前綴（如 "n."）後的名稱
					candidates := []string{fnName}
					if idx := strings.LastIndex(fnName, "."); idx > 0 {
						candidates = append(candidates, fnName[idx+1:])
					}
					for _, cname := range candidates {
						if nolangTypes, ok := g.funcParamTypes[cname]; ok {
							if argIdx >= 0 && argIdx < len(nolangTypes) {
								structName = nolangTypes[argIdx]
								break
							}
						}
					}
				}
			}
			if structName == "" {
				// 無法推斷，fallback 為 i64
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), tmpName))
				}
				return "i64* " + tmpName
			}
			structTy := "%" + structName
			fields := g.structTypes[structName]
			fieldIndexByName := make(map[string]int)
			for i, f := range fields {
				fieldIndexByName[f.name] = i
			}
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.st.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, structTy))
				// 先將整個結構體初始化為 zeroinitializer，避免未指定的欄位帶有 stack 殘值
				sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), structTy, structTy, tmpName))
			}
			setFields := make(map[int]bool)
			for _, f := range a.Fields {
				fieldIdx, ok := fieldIndexByName[f.Name]
				if !ok {
					continue
				}
				setFields[fieldIdx] = true
				fieldType := fields[fieldIdx].typ
				fieldVal := g.generateExprWithSB(sb, f.Value)
				fieldVal = g.stripLLVMType(fieldVal)
				g.tmpIdx++
				gepReg := fmt.Sprintf("%%ref.st.gep.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), gepReg, structTy, structTy, tmpName, fieldIdx))
				}
				if strings.HasPrefix(fieldType, "%") {
					if !strings.HasPrefix(fieldVal, "%") {
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), fieldType, fieldType, gepReg))
						}
					} else if _, isStrLit := f.Value.(*parser.StringLiteral); isStrLit || g.isStrPtrReg(fieldVal) {
						// String literals and str pointer regs are %str-long* pointers (alloca),
						// need to load the value before storing.
						if sb != nil {
							g.tmpIdx++
							loadReg := fmt.Sprintf("%%ref.st.fload.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, fieldType, fieldType, fieldVal))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, loadReg, fieldType, gepReg))
						}
					} else if sb != nil {
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
					}
				} else if sb != nil {
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
				}
			}
			// 為未明確設定的 %vec 欄位分配 data 緩衝區
			if sb != nil {
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
					g.tmpIdx++
					dataBuf := fmt.Sprintf("%%ref.st.vecdata.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
					g.tmpIdx++
					capGEP := fmt.Sprintf("%%ref.st.veccap.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 1\n",
						g.indent(), capGEP, structTy, structTy, tmpName, i))
					sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
					g.tmpIdx++
					dataGEP := fmt.Sprintf("%%ref.st.vecdataptr.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d, i32 2\n",
						g.indent(), dataGEP, structTy, structTy, tmpName, i))
					g.storeDataPtrField(sb, dataBuf, dataGEP)
				}
			}
			return structTy + "* " + tmpName
		case *parser.SliceLiteral:
			// Slice literal as function argument (e.g. bn-sub(m, [2, 0, ...]))
			// Determine element type from the parameter's declared type.
			elemType := "i64"
			if g.funcParamTypes != nil {
				if types, ok := g.funcParamTypes[fnName]; ok && argIdx < len(types) {
					paramType := types[argIdx]
					if strings.HasPrefix(paramType, "[]") {
						mapped := g.mapToLLVMType(paramType[2:])
						if g.isIntegerLLVMType(mapped) {
							elemType = mapped
						}
					}
				}
			}
			n := int64(len(a.Elements))
			g.tmpIdx++
			vecName := fmt.Sprintf("%%callvec.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecName))
			}
			if n > 0 {
				g.tmpIdx++
				tmpArr := fmt.Sprintf("%%callvec.arr.%d", g.tmpIdx)
				arrType := fmt.Sprintf("[%d x %s]", n, elemType)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))
					for i, elem := range a.Elements {
						ev := g.generateExprWithSB(sb, elem)
						ev = g.stripLLVMType(ev)
						g.tmpIdx++
						gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
							g.indent(), gepReg, arrType, arrType, tmpArr, i))
						storeVal := ev
						if elemType != "i64" && strings.HasPrefix(ev, "%") {
							g.tmpIdx++
							truncReg := fmt.Sprintf("%%callvec.trunc.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, ev, elemType))
							storeVal = truncReg
						}
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, gepReg))
					}
					g.tmpIdx++
					ptrReg := fmt.Sprintf("%%callvec.ptr.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), ptrReg, arrType, tmpArr))
					g.tmpIdx++
					lenGEP := fmt.Sprintf("%%callvec.len.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, vecName))
					sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))
					g.tmpIdx++
					capGEP := fmt.Sprintf("%%callvec.cap.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, vecName))
					sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))
					g.tmpIdx++
					dataGEP := fmt.Sprintf("%%callvec.data.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, vecName))
					g.storeDataPtrField(sb, ptrReg, dataGEP)
				}
			}
			return "%vec* " + vecName
		default:
			ev := g.generateExprWithSB(sb, arg)
			if strings.HasPrefix(ev, "%str-longlit") {
				return "%str-long* " + ev
			}
			// String concat / string method call results: ev is a %str-long* SSA register.
			// Detect via isStringExpr so InfixExpression (- for concat) and other string
			// expressions are passed as %str-long* instead of being truncated to i64.
			// But DotExpression and regular CallExpression load a %str-long VALUE
			// (not a pointer), so they must fall through to the alloca+store path.
			// ForwardFunc built-ins (like arg(i)) return %str-long* pointers directly,
			// so they can use the direct return path.
			if g.isStringExpr(arg) && strings.HasPrefix(ev, "%") {
				if _, isDot := arg.(*parser.DotExpression); !isDot {
					isRegularCall := false
					if call, ok := arg.(*parser.CallExpression); ok {
						// Check if this is a ForwardFunc built-in (returns pointer)
						// vs a regular function call (returns value)
						isForward := false
						if ident, ok := call.Function.(*parser.Identifier); ok {
							if m := builtin.FindBuiltinMethod(ident.Value); m != nil && m.ForwardFunc != "" {
								isForward = true
							}
						}
						if !isForward {
							isRegularCall = true
						}
					}
					if !isRegularCall {
						return "%str-long* " + ev
					}
				}
			}
			if strings.HasPrefix(ev, "%") && strings.Contains(ev, ".") {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					parts := strings.SplitN(ev, ".", 2)
					baseName := strings.TrimPrefix(parts[0], "%")
					isDouble := false
					if g.varTypes != nil {
						if t, ok := g.varTypes[baseName]; ok && t == "double" {
							isDouble = true
						}
					}
					if _, ok := arg.(*parser.FloatLiteral); ok {
						isDouble = true
					}
					if isDouble {
						sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
						sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), ev, tmpName))
						return "double* " + tmpName
					}
					// DotExpression (e.g. .field) loads a struct value into an SSA register.
					// The baseName "dot" is not a real variable, so varTypes lookup fails and
					// ptrType defaults to i64*. Determine the field type from structTypes so
					// %vec / %arr / %str-long / [N x T] fields get the correct pointer type.
					if dot, ok := arg.(*parser.DotExpression); ok {
						if ident, ok := dot.Receiver.(*parser.Identifier); ok {
							if g.varTypes != nil {
								if t, ok := g.varTypes[ident.Value]; ok && strings.HasPrefix(t, "%") {
									structName := strings.TrimPrefix(t, "%")
									if fields, ok := g.structTypes[structName]; ok {
										for _, f := range fields {
											if f.name == dot.Property {
												fieldType := f.typ
												if fieldType != "i64" {
													sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, fieldType))
													sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, ev, fieldType, tmpName))
													return fieldType + "* " + tmpName
												}
												break
											}
										}
									}
								}
							}
						}
					}
					// CallExpression: the baseName (e.g. "call") is not a real variable,
					// so varTypes lookup fails. Use exprResultLLVMType to determine the
					// function's return type for correct alloca/store.
					if call, ok := arg.(*parser.CallExpression); ok {
						if et := g.exprResultLLVMType(call); et != "" && et != "i64" {
							sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, et))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), et, ev, et, tmpName))
							return et + "* " + tmpName
						}
					}
					sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
					return "i64* " + tmpName
				}
				return "i64* " + tmpName
			} else if strings.HasPrefix(ev, "%") {
				parts := strings.Split(ev, ".")
				varName := strings.TrimPrefix(parts[0], "%")
				if g.varTypes != nil {
					if t, ok := g.varTypes[varName]; ok && t == "double" {
						return "double* %" + varName
					}
				}
				return "i64* %" + varName
			} else if strings.Contains(ev, ".") {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), ev, tmpName))
				}
				return "double* " + tmpName
			} else if _, err := fmt.Sscanf(ev, "%d", new(int)); err == nil {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
				}
				return "i64* " + tmpName
			}
			return ev
		}
	}

	// Generate typed arguments for non-variadic params
	typedArgs := make([]string, 0, len(nonVariadicArgs)+1)
	for i, arg := range nonVariadicArgs {
		typedArgs = append(typedArgs, genTypedArg(arg, i))
	}

	// If variadic, pack variadic args into a %vec struct
	if isVariadic {
		n := len(variadicArgs)
		elemType := retType // element type matches return type for monomorphized functions
		if elemType == "void" || elemType == "" {
			elemType = "i64"
		}
		g.tmpIdx++
		vecName := fmt.Sprintf("%%vvec.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecName))
		}
		if n > 0 {
			g.tmpIdx++
			arrName := fmt.Sprintf("%%varr.%d", g.tmpIdx)
			arrType := fmt.Sprintf("[%d x %s]", n, elemType)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), arrName, arrType))
			}
			for i, arg := range variadicArgs {
				ev := g.generateExprWithSB(sb, arg)
				g.tmpIdx++
				gepReg := fmt.Sprintf("%%varr.gep.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), gepReg, arrType, arrType, arrName, i))
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, ev, elemType, gepReg))
				}
			}
			// Set len (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%vvec.len.%d", g.tmpIdx)
			// Set data (field 2) = bitcast arrName to i8*
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%vvec.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataCast := fmt.Sprintf("%%vvec.data.cast.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, vecName))
				sb.WriteString(fmt.Sprintf("%s%s = bitcast [%d x %s]* %s to i8*\n", g.indent(), dataCast, n, elemType, arrName))
				g.storeDataPtrField(sb, dataCast, dataGEP)
			}
		} else {
			// Empty variadic: set len=0, data=null
			if sb != nil {
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%vvec.len.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
			}
		}
		typedArgs = append(typedArgs, "%vec* "+vecName)
	}

	// void + 單輸出：分配臨時輸出空間並附加指標到參數列表
	voidSingleTmp := ""
	voidSingleSp := ""
	if voidSingleOutput {
		g.tmpIdx++
		voidSingleTmp = fmt.Sprintf("%%vso.tmp.%d", g.tmpIdx)
		if sb != nil {
			// Save stack pointer to prevent stack growth when called inside loops
			g.tmpIdx++
			voidSingleSp = fmt.Sprintf("%%vso.sp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call ptr @llvm.stacksave.p0()\n", g.indent(), voidSingleSp))
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), voidSingleTmp, voidSingleOutputType))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), g.llvmTypeSize(voidSingleOutputType), voidSingleTmp))
			// %str-long 類型需要初始化 data 指標，否則方法體 out[i] = val 會因 data 為 null 而崩潰
			if voidSingleOutputType == "%str-long" {
				g.tmpIdx++
				dataBuf := fmt.Sprintf("%%vso.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 256)\n", g.indent(), dataBuf))
				// 初始化 len = 0
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%vso.len.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
				// 設置 cap = 256
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%vso.cap.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 256, i64* %s\n", g.indent(), capGEP))
				// 設置 data 指標指向緩衝區
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%vso.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, voidSingleTmp))
				g.storeDataPtrField(sb, dataBuf, dataGEP)
			} else if voidSingleOutputType == "%vec" {
				// %vec 類型需要初始化 data 指標，否則方法體 out[i] = val 會因 data 為 null 而崩潰
				// 使用 malloc（而非 alloca）使得 []byte 輸出在函數返回後仍有效
				// 計算元素大小：vecCap * elemSize 才是正確的 malloc 位元組數，
				// 否則對 %str-long（24 bytes/elem）會以 16384 bytes 當 cap，
				// 導致 16384/24 ≈ 682 個元素後溢出（或更小初始值 4096 → 170 個元素崩潰）
				vecCap := int64(256)
				elemSize := int64(8) // 預設 i64
				if g.funcResultNolangTypes != nil {
					if nolangRets, ok := g.funcResultNolangTypes[fnName]; ok && len(nolangRets) == 1 {
						nt := nolangRets[0]
						if strings.HasPrefix(nt, "[") {
							if rb := strings.Index(nt, "]"); rb > 0 {
								elemSize = llvmTypeSize(g.mapToLLVMType(nt[rb+1:]))
							}
						}
					}
				}
				vecBufSize := vecCap * elemSize
				g.tmpIdx++
				dataBuf := fmt.Sprintf("%%vso.vecdata.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
				// 初始化 len = 0（field 0）
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%vso.veclen.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
				// 設置 cap = vecCap（field 1，元素計數而非位元組數）
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%vso.veccap.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecCap, capGEP))
				// 設置 data 指標指向緩衝區（field 2）
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%vso.vecdata.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, voidSingleTmp))
				g.storeDataPtrField(sb, dataBuf, dataGEP)
			}
		}
		typedArgs = append(typedArgs, voidSingleOutputType+"* "+voidSingleTmp)
	}

	// Make the call
	callStr := fmt.Sprintf("call %s @%s(%s)", retType, sanitizeLLVMName(llvmFnName), strings.Join(typedArgs, ", "))

	// If has output param, store return value into output variable
	if hasOutputParam && outputArg != nil {
		if ident, ok := outputArg.(*parser.Identifier); ok {
			if sb != nil {
				if retType == "void" {
					sb.WriteString(g.indent() + callStr + "\n")
				} else {
					g.tmpIdx++
					callReg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), callReg, callStr))
					outType := retType
					if outType == "" {
						outType = "i64"
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), outType, callReg, outType, g.varAddr(ident.Value)))
				}
			}
			return ""
		}
	}

	// void + 單輸出：調用後載入結果作為返回值
	if voidSingleOutput {
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s\n", g.indent(), callStr))
			g.tmpIdx++
			loadReg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, voidSingleOutputType, voidSingleOutputType, voidSingleTmp))
			// Restore stack pointer to prevent stack growth when called inside loops
			if voidSingleSp != "" {
				sb.WriteString(fmt.Sprintf("%scall void @llvm.stackrestore.p0(ptr %s)\n", g.indent(), voidSingleSp))
			}
			// 記錄 SSA 型別，供 inferSSAType 查詢（如 if 表達式 phi 節點推斷）
			if g.ssaTypes != nil {
				g.ssaTypes[loadReg] = voidSingleOutputType
			}
			return loadReg
		}
		return ""
	}

	// For non-void returns without output param: capture return value as expression
	if retType != "void" && sb != nil {
		g.tmpIdx++
		callReg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), callReg, callStr))
		// 記錄 SSA 型別，供 inferSSAType 查詢
		if g.ssaTypes != nil {
			g.ssaTypes[callReg] = retType
		}
		return callReg
	}

	return callStr
}

// isMemberOfUnionTransitive checks if typeName is a member of aliasName's union,
// following transitive union aliases (e.g., int → i64, num → int → i64).
func (g *Generator) isMemberOfUnionTransitive(typeName, aliasName string, visited map[string]bool) bool {
	members, ok := g.unionAliases[aliasName]
	if !ok {
		return false
	}
	for _, m := range members {
		if m == typeName {
			return true
		}
		if _, isUnion := g.unionAliases[m]; isUnion && !visited[m] {
			visited[m] = true
			if g.isMemberOfUnionTransitive(typeName, m, visited) {
				return true
			}
		}
	}
	return false
}

// strExprDataPtr extracts the i8* data pointer from a string expression argument.
// Handles Identifier (str variables) and StringLiteral.
func (g *Generator) strExprDataPtr(sb *strings.Builder, arg parser.Expression) string {
	switch a := arg.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[a.Value]; ok {
				ptr := g.varAddr(a.Value)
				if t == "%str-long" {
					return g.extractStrDataPtr(sb, ptr)
				}
			}
		}
	case *parser.StringLiteral:
		ptr := g.generateExprWithSB(sb, arg)
		return g.extractStrDataPtr(sb, ptr)
	}
	// Fallback: generate expression and hope it's a usable pointer
	return g.generateExprWithSB(sb, arg)
}

// evalI64Arg evaluates an expression to an i64 value string (for use as LLVM argument).
func (g *Generator) evalI64Arg(sb *strings.Builder, arg parser.Expression) string {
	if il, ok := arg.(*parser.IntegerLiteral); ok {
		return fmt.Sprintf("%d", il.Value)
	}
	val := g.generateExprWithSB(sb, arg)
	// If it's a pointer (starts with % and is a ref.tmp), load it
	if strings.HasPrefix(val, "%ref.tmp") {
		g.tmpIdx++
		loadReg := fmt.Sprintf("%%fwd.i64.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), loadReg, val))
		}
		return loadReg
	}
	// If it's a variable pointer, load from it
	if ident, ok := arg.(*parser.Identifier); ok {
		if g.varTypes != nil {
			if t, ok := g.varTypes[ident.Value]; ok && (t == "i64" || t == "i32" || t == "i8" || t == "i1") {
				g.tmpIdx++
				loadReg := fmt.Sprintf("%%fwd.i64.%d", g.tmpIdx)
				llvmType := t
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, llvmType, llvmType, g.varAddr(ident.Value)))
				}
				if llvmType != "i64" {
					g.tmpIdx++
					extReg := fmt.Sprintf("%%fwd.i64.ext.%d", g.tmpIdx)
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), extReg, llvmType, loadReg))
					}
					return extReg
				}
				return loadReg
			}
		}
	}
	return val
}

// genArrZero generates llvm.memset to zero an array/slice in-place.
// receiverArg is the first argument (the array/slice variable) passed to the
// rewritten .zero() call. Returns true if IR was generated.
func (g *Generator) genArrZero(sb *strings.Builder, receiverArg parser.Expression) bool {
	ident, ok := receiverArg.(*parser.Identifier)
	if !ok {
		return false
	}
	recvName := ident.Value
	recvType := ""
	if g.varTypes != nil {
		recvType = g.varTypes[recvName]
	}
	// 確定元素大小（位元組）
	elemSize := int64(8) // 預設 i64
	if g.arrayElemTypes != nil {
		if et, ok := g.arrayElemTypes[recvName]; ok {
			elemSize = llvmTypeSize(et)
		}
	}
	recvAddr := g.varAddr(recvName)
	if sb == nil {
		return true
	}
	if recvType == "%arr" {
		// 定長陣列：%arr = { i64, i8* }
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%az.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%az.len.%d", g.tmpIdx)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%az.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataReg := fmt.Sprintf("%%az.data.%d", g.tmpIdx)
		g.tmpIdx++
		totalReg := fmt.Sprintf("%%az.total.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, recvAddr))
		dataReg = g.loadDataPtrField(sb, dataGEP)
		sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), totalReg, lenReg, elemSize))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), dataReg, totalReg))
	} else {
		// 切片：%vec = { i64, i64, i8* }
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%az.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%az.len.%d", g.tmpIdx)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%az.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataReg := fmt.Sprintf("%%az.data.%d", g.tmpIdx)
		g.tmpIdx++
		totalReg := fmt.Sprintf("%%az.total.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, recvAddr))
		dataReg = g.loadDataPtrField(sb, dataGEP)
		sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), totalReg, lenReg, elemSize))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), dataReg, totalReg))
	}
	return true
}

// genForwardFunc handles ForwardFunc builtins: memcpy (str.copy), memcmp (str.eq), memset (str-fill).
// receiver is non-nil for method-style calls (e.g. a.eq(b, n)); nil for global function calls.
// Returns the SSA register for the result, or "" for void functions.
func (g *Generator) genForwardFunc(sb *strings.Builder, forwardFunc string, expr *parser.CallExpression, receiver parser.Expression) string {
	// 構建有效參數列表：receiver（若有）+ expr.Arguments
	var args []parser.Expression
	if receiver != nil {
		args = append(args, receiver)
	}
	args = append(args, expr.Arguments...)

	switch forwardFunc {
	case "memcpy":
		// str.copy(dst, n) [method] or str-copy(src, dst, n) [global] → llvm.memcpy(dst_data, src_data, n, false)
		if len(args) < 3 {
			return ""
		}
		srcPtr := g.strExprDataPtr(sb, args[0])
		dstPtr := g.strExprDataPtr(sb, args[1])
		nVal := g.evalI64Arg(sb, args[2])
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), dstPtr, srcPtr, nVal))
		}
		return ""

	case "eq-raw":
		// str.eq(b, n) [method] or str-eq(a, b, n) [global] → llvm.memcmp(a_data, b_data, n) == 0 → zext to i64
		if len(args) < 3 {
			return "0"
		}
		aPtr := g.strExprDataPtr(sb, args[0])
		bPtr := g.strExprDataPtr(sb, args[1])
		nVal := g.evalI64Arg(sb, args[2])
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%eqcmp.%d", g.tmpIdx)
		g.tmpIdx++
		eqReg := fmt.Sprintf("%%eqres.%d", g.tmpIdx)
		g.tmpIdx++
		zextReg := fmt.Sprintf("%%eqzext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.memcmp(i8* %s, i8* %s, i64 %s)\n", g.indent(), cmpReg, aPtr, bPtr, nVal))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), eqReg, cmpReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, eqReg))
		}
		return zextReg

	case "memset":
		// str-fill(s, n, val) → llvm.memset(s_data, val, n, false)
		// llvm.memset signature: void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)
		if len(args) < 3 {
			return ""
		}
		sPtr := g.strExprDataPtr(sb, args[0])
		nVal := g.evalI64Arg(sb, args[1])
		valVal := g.evalI64Arg(sb, args[2])
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 %s, i64 %s, i1 false)\n", g.indent(), sPtr, valVal, nVal))
		}
		return ""

	case "str-to-bool":
		// str.to-bool: memcmp("true", 5 bytes incl null) + cmp + zext
		// 使用 @llvm.memcmp 替代 @strcmp（避免 libc 依賴）
		if len(args) < 1 {
			return ""
		}
		sPtr := g.strExprDataPtr(sb, args[0])
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%boolcmp.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.memcmp(i8* %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i64 5)\n",
				g.indent(), cmpReg, sPtr))
		}
		g.tmpIdx++
		eqReg := fmt.Sprintf("%%booleq.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), eqReg, cmpReg))
		}
		g.tmpIdx++
		zextReg := fmt.Sprintf("%%boolzext.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, eqReg))
		}
		return zextReg

	case "bool-to-str":
		// bool.to-str: select + malloc + memcpy + 构造 %str-long
		// Must heap-allocate the data buffer so emitHeapFree can safely free it.
		// Using global constant pointers directly would cause abort when freed.
		if len(args) < 1 {
			return ""
		}
		var bVal string
		if bl, ok := args[0].(*parser.BooleanLiteral); ok {
			if bl.Value {
				bVal = "1"
			} else {
				bVal = "0"
			}
		} else {
			bVal = g.generateExprWithSB(sb, args[0])
		}
		g.tmpIdx++
		selectReg := fmt.Sprintf("%%boolstr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.str.false, i64 0, i64 0)\n",
				g.indent(), selectReg, bVal))
		}
		// "true" = 4, "false" = 5; use select directly
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%boolstr.len.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 4, i64 5\n", g.indent(), lenReg, bVal))
		}
		// Allocate heap buffer (6 bytes, enough for "false\0")
		g.tmpIdx++
		bufReg := fmt.Sprintf("%%boolstr.buf.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 6)\n", g.indent(), bufReg))
		}
		// Copy length including null terminator: "true\0" = 5, "false\0" = 6
		g.tmpIdx++
		copyLenReg := fmt.Sprintf("%%boolstr.copylen.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 5, i64 6\n", g.indent(), copyLenReg, bVal))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
				g.indent(), bufReg, selectReg, copyLenReg))
		}
		// Construct %str-long { len, cap, data } with heap-allocated data
		g.tmpIdx++
		strReg1 := fmt.Sprintf("%%boolstr.val.%d", g.tmpIdx)
		g.tmpIdx++
		strReg2 := fmt.Sprintf("%%boolstr.val.%d", g.tmpIdx)
		g.tmpIdx++
		strReg3 := fmt.Sprintf("%%boolstr.val.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), strReg1, lenReg))
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), strReg2, strReg1, lenReg))
			_p2i_strReg3 := g.ptrToIntVal(sb, bufReg)
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), strReg3, strReg2, _p2i_strReg3))
		}
		return strReg3

	case "ffi-cstr-at", "ffi-cstr-at-int", "ffi-cstr-at-float":
		// ffi-cstr-at*(arr i64, idx i64): 從 char** 陣列讀取第 idx 個 C 字串
		// NULL 安全：以 alloca i8 + store 0 建立空字串，select 避免 NULL 傳入 nolang.strlen/nolang.strtoll/nolang.strtod
		if len(args) < 2 {
			return ""
		}
		arrVal := g.evalI64Arg(sb, args[0])
		idxVal := g.evalI64Arg(sb, args[1])

		// 1. inttoptr i64 → i8**
		g.tmpIdx++
		ptrReg := fmt.Sprintf("%%cstr.ptr.%d", g.tmpIdx)
		// 2. getelementptr i8*, i8** %ptr, i64 %idx
		g.tmpIdx++
		gepReg := fmt.Sprintf("%%cstr.gep.%d", g.tmpIdx)
		// 3. load i8*, i8** %gep
		g.tmpIdx++
		fieldReg := fmt.Sprintf("%%cstr.field.%d", g.tmpIdx)
		// 4. icmp eq i8* %field, null
		g.tmpIdx++
		isnullReg := fmt.Sprintf("%%cstr.isnull.%d", g.tmpIdx)
		// 5. alloca i8 + store i8 0
		g.tmpIdx++
		emptyReg := fmt.Sprintf("%%cstr.empty.%d", g.tmpIdx)
		// 6. select i1 %isnull, i8* %empty, i8* %field
		g.tmpIdx++
		safeReg := fmt.Sprintf("%%cstr.safe.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8**\n", g.indent(), ptrReg, arrVal))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8*, i8** %s, i64 %s\n", g.indent(), gepReg, ptrReg, idxVal))
			fieldReg = g.loadDataPtrField(sb, gepReg)
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), isnullReg, fieldReg))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8\n", g.indent(), emptyReg))
			sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), emptyReg))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* %s, i8* %s\n", g.indent(), safeReg, isnullReg, emptyReg, fieldReg))
		}

		switch forwardFunc {
		case "ffi-cstr-at":
			// 7. strlen → 8. insertvalue %str-long
			g.tmpIdx++
			lenReg := fmt.Sprintf("%%cstr.len.%d", g.tmpIdx)
			g.tmpIdx++
			strReg1 := fmt.Sprintf("%%cstr.s1.%d", g.tmpIdx)
			g.tmpIdx++
			strReg2 := fmt.Sprintf("%%cstr.s2.%d", g.tmpIdx)
			g.tmpIdx++
			strReg3 := fmt.Sprintf("%%cstr.s3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, safeReg))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), strReg1, lenReg))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), strReg2, strReg1, lenReg))
				_p2i_strReg3 := g.ptrToIntVal(sb, safeReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), strReg3, strReg2, _p2i_strReg3))
			}
			return strReg3

		case "ffi-cstr-at-int":
			// 7. nolang.strtoll(safe) — internal base-10 parser replacing libc @strtoll
			g.tmpIdx++
			valReg := fmt.Sprintf("%%cstr.ival.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strtoll(i8* %s)\n", g.indent(), valReg, safeReg))
			}
			return valReg

		case "ffi-cstr-at-float":
			// 7. nolang.strtod(safe) — internal parser replacing libc @strtod
			g.tmpIdx++
			valReg := fmt.Sprintf("%%cstr.fval.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call double @nolang.strtod(i8* %s)\n", g.indent(), valReg, safeReg))
			}
			return valReg
		}
		return ""

	case "arr-zero":
		// []<type>.zero / [n]<type>.zero — 使用 llvm.memset 將所有元素置零
		// receiver 是陣列/切片變數；無額外參數。
		// %arr = type { i64, i8* }  (len, data)
		// %vec = type { i64, i64, i8* } (len, cap, data)
		if len(args) < 1 {
			return ""
		}
		ident, ok := args[0].(*parser.Identifier)
		if !ok {
			return ""
		}
		recvName := ident.Value
		recvType := ""
		if g.varTypes != nil {
			recvType = g.varTypes[recvName]
		}
		// 確定元素大小（位元組）
		elemSize := int64(8) // 預設 i64
		if g.arrayElemTypes != nil {
			if et, ok := g.arrayElemTypes[recvName]; ok {
				elemSize = llvmTypeSize(et)
			}
		}
		recvAddr := g.varAddr(recvName)
		if recvType == "%arr" {
			// 定長陣列：%arr = { i64, i8* }
			// field 0 = len, field 1 = data
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%az.len.gep.%d", g.tmpIdx)
			g.tmpIdx++
			lenReg := fmt.Sprintf("%%az.len.%d", g.tmpIdx)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%az.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataReg := fmt.Sprintf("%%az.data.%d", g.tmpIdx)
			g.tmpIdx++
			totalReg := fmt.Sprintf("%%az.total.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, recvAddr))
				dataReg = g.loadDataPtrField(sb, dataGEP)
				sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), totalReg, lenReg, elemSize))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), dataReg, totalReg))
			}
		} else {
			// 切片：%vec = { i64, i64, i8* }
			// field 0 = len, field 2 = data
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%az.len.gep.%d", g.tmpIdx)
			g.tmpIdx++
			lenReg := fmt.Sprintf("%%az.len.%d", g.tmpIdx)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%az.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataReg := fmt.Sprintf("%%az.data.%d", g.tmpIdx)
			g.tmpIdx++
			totalReg := fmt.Sprintf("%%az.total.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
				sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, recvAddr))
				dataReg = g.loadDataPtrField(sb, dataGEP)
				sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), totalReg, lenReg, elemSize))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), dataReg, totalReg))
			}
		}
		return ""

	case "vec-clear":
		// vec.clear() — set len=0 in-place, cap/data unchanged
		if len(args) < 1 {
			return ""
		}
		ident, ok := args[0].(*parser.Identifier)
		if !ok {
			return ""
		}
		recvAddr := g.varAddr(ident.Value)
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%vc.len.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
		}
		return ""

	case "vec-push":
		// vec.push(val) — append element with auto-grow
		// Expansion: cap==0→4, cap<1024→cap*2, cap>=1024→cap*5/4
		if len(args) < 2 {
			return ""
		}
		ident, ok := args[0].(*parser.Identifier)
		if !ok {
			return ""
		}
		recvName := ident.Value
		recvAddr := g.varAddr(recvName)

		// Get element type and size
		elemType := "i64"
		if g.arrayElemTypes != nil {
			if et, ok := g.arrayElemTypes[recvName]; ok {
				elemType = et
			}
		}
		elemSize := llvmTypeSize(elemType)

		// Load current len (field 0) and cap (field 1)
		curLen := g.emitVecLenLoad(sb, recvAddr)
		curCap := g.emitVecCapLoad(sb, recvAddr)

		// Evaluate the value to push
		val := g.generateExprWithSB(sb, args[1])

		// Coerce val to element type
		storeVal := val
		if strings.HasPrefix(val, "%") {
			valType := g.intExprLLVMType(args[1])
			if valType != "" && valType != elemType && g.isIntegerLLVMType(valType) && g.isIntegerLLVMType(elemType) {
				g.tmpIdx++
				convReg := fmt.Sprintf("%%vp.conv.%d", g.tmpIdx)
				if sb != nil {
					if valType == "i64" {
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), convReg, val, elemType))
					} else if elemType == "i64" {
						sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), convReg, valType, val))
					} else {
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, valType, val, elemType))
					}
				}
				storeVal = convReg
			}
		}

		// For struct element types (e.g. %str-long, %vec, user structs), ForwardFunc
		// builtin call results (like arg(i)) are returned as *pointers* to the struct,
		// while identifiers, index expressions, and regular method calls return struct
		// *values* (already loaded). Load the struct value only for ForwardFunc calls.
		if strings.HasPrefix(elemType, "%") {
			needLoad := false
			if call, ok := args[1].(*parser.CallExpression); ok {
				if ident, ok := call.Function.(*parser.Identifier); ok {
					if m := builtin.FindBuiltinMethod(ident.Value); m != nil && m.ForwardFunc != "" {
						needLoad = true
					}
				}
			}
			// StringLiteral returns a %str-longlit.N alloca pointer; load the value.
			if _, ok := args[1].(*parser.StringLiteral); ok {
				needLoad = true
			}
			if needLoad {
				g.tmpIdx++
				loadReg := fmt.Sprintf("%%vp.sload.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, elemType, elemType, val))
				}
				storeVal = loadReg
			}
		}

		// Generate labels for basic blocks
		lbl := g.tmpIdx
		g.tmpIdx += 3 // reserve 3 label numbers
		fastLabel := fmt.Sprintf("vp.fast.%d", lbl)
		expandLabel := fmt.Sprintf("vp.expand.%d", lbl)
		endLabel := fmt.Sprintf("vp.end.%d", lbl)

		// Compare len < cap
		g.tmpIdx++
		spaceCmp := fmt.Sprintf("%%vp.space.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), spaceCmp, curLen, curCap))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), spaceCmp, fastLabel, expandLabel))
		}

		// Fast path: len < cap, just store val and increment len
		if sb != nil {
			g.emitLabel(sb, fastLabel)
			// Load data pointer (field 2)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%vp.data.gep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%vp.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, recvAddr))
			dataLoad = g.loadDataPtrField(sb, dataGEP)
			// Bitcast to element type pointer
			g.tmpIdx++
			dataTyped := fmt.Sprintf("%%vp.typed.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), dataTyped, dataLoad, elemType))
			// GEP to index len
			g.tmpIdx++
			elemGEP := fmt.Sprintf("%%vp.elem.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n", g.indent(), elemGEP, elemType, elemType, dataTyped, curLen))
			// Store val
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, elemGEP))
			// Increment len: store len+1 to field 0
			g.tmpIdx++
			newLen := fmt.Sprintf("%%vp.newlen.%d", g.tmpIdx)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%vp.len.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, curLen))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), newLen, lenGEP))
			// Jump to end
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), endLabel))
		}

		// Expand path: len >= cap, need to grow
		if sb != nil {
			g.emitLabel(sb, expandLabel)
			// Calculate new-cap: cap==0→4, cap<1024→cap*2, cap>=1024→cap*5/4
			g.tmpIdx++
			capIsZero := fmt.Sprintf("%%vp.cz.%d", g.tmpIdx)
			g.tmpIdx++
			capIsSmall := fmt.Sprintf("%%vp.cs.%d", g.tmpIdx)
			// cap == 0 → new-cap = 4
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, 0\n", g.indent(), capIsZero, curCap))
			// cap < 1024 → new-cap = cap * 2
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, 1024\n", g.indent(), capIsSmall, curCap))
			// cap * 2
			g.tmpIdx++
			capDoubled := fmt.Sprintf("%%vp.cd.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = shl i64 %s, 1\n", g.indent(), capDoubled, curCap))
			// cap * 5 / 4
			g.tmpIdx++
			capMul5 := fmt.Sprintf("%%vp.cm5.%d", g.tmpIdx)
			g.tmpIdx++
			capQuarter := fmt.Sprintf("%%vp.cq.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, 5\n", g.indent(), capMul5, curCap))
			sb.WriteString(fmt.Sprintf("%s%s = sdiv i64 %s, 4\n", g.indent(), capQuarter, capMul5))
			// select small ? cap*2 : cap*5/4
			g.tmpIdx++
			growCap := fmt.Sprintf("%%vp.gc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), growCap, capIsSmall, capDoubled, capQuarter))
			// select zero ? 4 : growCap
			g.tmpIdx++
			newCap := fmt.Sprintf("%%vp.nc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 4, i64 %s\n", g.indent(), newCap, capIsZero, growCap))

			// malloc new buffer: new-cap * elemSize
			g.tmpIdx++
			totalBytes := fmt.Sprintf("%%vp.tb.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), totalBytes, newCap, elemSize))
			g.tmpIdx++
			newBuf := fmt.Sprintf("%%vp.buf.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), newBuf, totalBytes))

			// Copy old data if old cap > 0
			// Load old data pointer
			g.tmpIdx++
			oldDataGEP := fmt.Sprintf("%%vp.od.gep.%d", g.tmpIdx)
			g.tmpIdx++
			oldData := fmt.Sprintf("%%vp.od.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), oldDataGEP, recvAddr))
			oldData = g.loadDataPtrField(sb, oldDataGEP)

			// Copy old data: bytes = curLen * elemSize (only valid elements)
			g.tmpIdx++
			copyBytes := fmt.Sprintf("%%vp.cb.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), copyBytes, curLen, elemSize))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), newBuf, oldData, copyBytes))

			// Store val at newBuf[len]
			g.tmpIdx++
			newTyped := fmt.Sprintf("%%vp.nt.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n", g.indent(), newTyped, newBuf, elemType))
			g.tmpIdx++
			newElemGEP := fmt.Sprintf("%%vp.ne.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n", g.indent(), newElemGEP, elemType, elemType, newTyped, curLen))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, newElemGEP))

			// Update cap (field 1) = newCap
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%vp.cap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), newCap, capGEP))

			// Update data (field 2) = newBuf
			g.tmpIdx++
			dataStoreGEP := fmt.Sprintf("%%vp.ds.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataStoreGEP, recvAddr))
			g.storeDataPtrField(sb, newBuf, dataStoreGEP)

			// Increment len: store len+1 to field 0
			g.tmpIdx++
			newLen2 := fmt.Sprintf("%%vp.nl2.%d", g.tmpIdx)
			g.tmpIdx++
			lenGEP2 := fmt.Sprintf("%%vp.lg2.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen2, curLen))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP2, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), newLen2, lenGEP2))
			// Jump to end
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), endLabel))
		}

		// End label
		if sb != nil {
			g.emitLabel(sb, endLabel)
		}
		return ""

	case "str-clear":
		// str.clear() — set len=0 in-place, no storage switch
		// str-long: store i64 0 to field 0, cap/ptr unchanged
		if len(args) < 1 {
			return ""
		}
		ident, ok := args[0].(*parser.Identifier)
		if !ok {
			return ""
		}
		recvName := ident.Value
		recvAddr := g.varAddr(recvName)
		// str-long: field 0 is i64 len
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%sc.len.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
		}
		return ""

	case "with-cap":
		// with-cap(cap) — builtin syntax, type inferred from assignment LHS
		//   s str = with-cap(256)   → %str-long { len=0, cap=256, data=malloc(256) }
		//   v []i64 = with-cap(100) → %vec { len=0, cap=100, data=malloc(100*8) }
		if len(args) < 1 {
			return ""
		}
		capVal := g.evalI64Arg(sb, args[0])
		targetType := g.currentTargetType
		switch targetType {
		case "%str-long", "str":
			// malloc(cap)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wc.sbuf.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, capVal))
			}
			// Build %str-long { len=0, cap=cap, data=buf } via insertvalue
			g.tmpIdx++
			s1 := fmt.Sprintf("%%wc.s1.%d", g.tmpIdx)
			g.tmpIdx++
			s2 := fmt.Sprintf("%%wc.s2.%d", g.tmpIdx)
			g.tmpIdx++
			s3 := fmt.Sprintf("%%wc.s3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 0, 0\n", g.indent(), s1))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), s2, s1, capVal))
				_p2i_s3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), s3, s2, _p2i_s3))
			}
			g.ssaTypes[s3] = "%str-long"
			return s3

		default: // %vec, []i64, etc.
			elemSize := int64(8) // default i64
			// total bytes = cap * elemSize
			g.tmpIdx++
			bytesReg := fmt.Sprintf("%%wc.vbytes.%d", g.tmpIdx)
			// malloc(cap * elemSize)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wc.vbuf.%d", g.tmpIdx)
			// Build %vec { len=0, cap=cap, data=buf } via insertvalue
			g.tmpIdx++
			v1 := fmt.Sprintf("%%wc.v1.%d", g.tmpIdx)
			g.tmpIdx++
			v2 := fmt.Sprintf("%%wc.v2.%d", g.tmpIdx)
			g.tmpIdx++
			v3 := fmt.Sprintf("%%wc.v3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), bytesReg, capVal, elemSize))
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, bytesReg))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec zeroinitializer, i64 0, 0\n", g.indent(), v1))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 1\n", g.indent(), v2, v1, capVal))
				_p2i_v3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 2\n", g.indent(), v3, v2, _p2i_v3))
			}
			g.ssaTypes[v3] = "%vec"
			return v3
		}

	case "get-errno":
		// get-errno() — get the last errno value from C library
		// macOS: @__error(), Linux: @__errno_location(), Windows: @_errno()
		g.tmpIdx++
		errnoPtrReg := fmt.Sprintf("%%errno.ptr.%d", g.tmpIdx)
		g.tmpIdx++
		errnoReg := fmt.Sprintf("%%errno.val.%d", g.tmpIdx)
		if sb != nil {
			// Get errno pointer
			if runtime.GOOS == "darwin" {
				sb.WriteString(fmt.Sprintf("%s%s = call i32* @__error()\n", g.indent(), errnoPtrReg))
			} else if runtime.GOOS == "windows" {
				sb.WriteString(fmt.Sprintf("%s%s = call i32* @_errno()\n", g.indent(), errnoPtrReg))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s = call i32* @__errno_location()\n", g.indent(), errnoPtrReg))
			}
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), errnoReg, errnoPtrReg))
			g.tmpIdx++
			sextReg := fmt.Sprintf("%%errno.sext.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), sextReg, errnoReg))
			return sextReg
		}
		return "0"
	}

	return ""
}

// evalF64Arg 評估表達式為 double 值。
// FloatLiteral / double(或 float) 變數直接取值；其餘（整數表達式等）先取 i64 再 sitofp 成 double。
func (g *Generator) evalF64Arg(sb *strings.Builder, arg parser.Expression) string {
	if _, ok := arg.(*parser.FloatLiteral); ok {
		return g.generateExprWithSB(sb, arg)
	}
	if ident, ok := arg.(*parser.Identifier); ok && g.varTypes != nil {
		if t, ok := g.varTypes[ident.Value]; ok && (t == "double" || t == "float") {
			return g.generateExprWithSB(sb, arg)
		}
	}
	i64Val := g.evalI64Arg(sb, arg)
	g.tmpIdx++
	reg := fmt.Sprintf("%%ext.f64.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = sitofp i64 %s to double\n", g.indent(), reg, i64Val))
	return reg
}

// callExtern 產生對 FFI extern 函式的 LLVM call 指令，並依 FFI 型別對應表
// 自動處理輸入參數與回傳值的型別轉換。
//
// 輸入轉換（Nolang storage → C）：
//
//	i64  → i64 (none)
//	i32  → trunc i64→i32
//	f64  → double (none；整數表達式先 sitofp)
//	str  → makeNullTerminatedStr → i8*
//	ptr  → inttoptr i64→i8*
//	bool → trunc i64→i32
//	pptr → alloca i8*，傳 i8**；呼叫後 load+ptrtoint 存回呼叫端變數
//	ppptr → alloca i8**，傳 i8***；呼叫後 load+ptrtoint 存回呼叫端變數
//
// 輸出轉換（C → Nolang storage）：
//
//	i64  → i64 (none)
//	i32  → sext i32→i64
//	f64  → double (none)
//	str  → strlen + insertvalue %str-long
//	ptr  → ptrtoint i8*→i64
//	pptr → ptrtoint i8**→i64
//	ppptr → ptrtoint i8***→i64
//	bool → icmp ne 0 + zext i1→i64
//	void → "0"
func (g *Generator) callExtern(sb *strings.Builder, info *ExternFuncInfo, expr *parser.CallExpression) string {
	// 乾跑（sb == nil，例如 rangeBoundStr 型別推斷）：回傳佔位值，不發出 IR。
	if sb == nil {
		return "0"
	}

	type pptrSlot struct {
		slotReg  string
		varName  string // 呼叫端變數名；非 Identifier 時為 ""（跳過 store-back）
		llvmType string // alloca 的元素型別（如 "i8*" 或 "i8**"）
		ptrType  string // slot 的指針型別（如 "i8**" 或 "i8***"）
	}
	var pptrSlots []pptrSlot

	llvmArgs := make([]string, 0, len(info.ParamTypes))
	for i, ptype := range info.ParamTypes {
		if i >= len(expr.Arguments) {
			break
		}
		arg := expr.Arguments[i]
		switch ptype {
		case "i64":
			llvmArgs = append(llvmArgs, "i64 "+g.evalI64Arg(sb, arg))
		case "i32":
			val := g.evalI64Arg(sb, arg)
			g.tmpIdx++
			reg := fmt.Sprintf("%%ext.trunc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), reg, val))
			llvmArgs = append(llvmArgs, "i32 "+reg)
		case "bool":
			// 輸入：Nolang bool (i64) → C int (i32)
			val := g.evalI64Arg(sb, arg)
			g.tmpIdx++
			reg := fmt.Sprintf("%%ext.btrunc.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), reg, val))
			llvmArgs = append(llvmArgs, "i32 "+reg)
		case "f64":
			llvmArgs = append(llvmArgs, "double "+g.evalF64Arg(sb, arg))
		case "str":
			ptr := g.makeNullTerminatedStr(sb, arg)
			if ptr == "" {
				ptr = g.strExprDataPtr(sb, arg)
			}
			llvmArgs = append(llvmArgs, "i8* "+ptr)
		case "ptr":
			val := g.evalI64Arg(sb, arg)
			g.tmpIdx++
			reg := fmt.Sprintf("%%ext.ptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), reg, val))
			llvmArgs = append(llvmArgs, "i8* "+reg)
		case "pptr":
			g.tmpIdx++
			slot := fmt.Sprintf("%%ext.pptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8*\n", g.indent(), slot))
			llvmArgs = append(llvmArgs, "i8** "+slot)
			varName := ""
			if ident, ok := arg.(*parser.Identifier); ok {
				varName = ident.Value
			}
			pptrSlots = append(pptrSlots, pptrSlot{slotReg: slot, varName: varName, llvmType: "i8*", ptrType: "i8**"})
		case "ppptr":
			g.tmpIdx++
			slot := fmt.Sprintf("%%ext.ppptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8**\n", g.indent(), slot))
			llvmArgs = append(llvmArgs, "i8*** "+slot)
			varName := ""
			if ident, ok := arg.(*parser.Identifier); ok {
				varName = ident.Value
			}
			pptrSlots = append(pptrSlots, pptrSlot{slotReg: slot, varName: varName, llvmType: "i8**", ptrType: "i8***"})
		default:
			// 未知 FFI 型別：保守地當作 i64
			llvmArgs = append(llvmArgs, "i64 "+g.evalI64Arg(sb, arg))
		}
	}

	// 決定回傳型別（取第一個 result；無 result 為 void）
	retFFIType := ""
	if len(info.ResultTypes) > 0 {
		retFFIType = info.ResultTypes[0]
	}
	retLLVMType := "void"
	if retFFIType != "" {
		retLLVMType = ffiTypeToLLVM(retFFIType)
	}

	fnRef := externSymbolRef(info.Name)
	argStr := strings.Join(llvmArgs, ", ")

	var callReg string
	if retLLVMType == "void" {
		sb.WriteString(fmt.Sprintf("%scall void %s(%s)\n", g.indent(), fnRef, argStr))
	} else {
		g.tmpIdx++
		callReg = fmt.Sprintf("%%ext.call.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = call %s %s(%s)\n", g.indent(), callReg, retLLVMType, fnRef, argStr))
	}

	// pptr / ppptr 輸出參數：從 alloca 載入指針，ptrtoint 成 i64，存回呼叫端變數。
	// 呼叫端引數必須是簡單 Identifier；否則跳過 store-back。
	for _, ps := range pptrSlots {
		g.tmpIdx++
		loadReg := fmt.Sprintf("%%ext.pptr.load.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %s, %s %s\n", g.indent(), loadReg, ps.llvmType, ps.ptrType, ps.slotReg))
		g.tmpIdx++
		intReg := fmt.Sprintf("%%ext.pptr.int.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %s %s to i64\n", g.indent(), intReg, ps.llvmType, loadReg))
		if ps.varName != "" {
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), intReg, g.varAddr(ps.varName)))
		}
	}

	// 回傳值轉換
	if retLLVMType == "void" {
		return "0"
	}
	switch retFFIType {
	case "i64":
		return callReg
	case "i32":
		g.tmpIdx++
		reg := fmt.Sprintf("%%ext.sext.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), reg, callReg))
		return reg
	case "f64":
		return callReg
	case "str":
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%ext.strlen.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, callReg))
		g.tmpIdx++
		strReg1 := fmt.Sprintf("%%ext.str1.%d", g.tmpIdx)
		g.tmpIdx++
		strReg2 := fmt.Sprintf("%%ext.str2.%d", g.tmpIdx)
		g.tmpIdx++
		strReg3 := fmt.Sprintf("%%ext.str3.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), strReg1, lenReg))
		sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), strReg2, strReg1, lenReg))
		_p2i_strReg3 := g.ptrToIntVal(sb, callReg)
		sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), strReg3, strReg2, _p2i_strReg3))
		return strReg3
	case "ptr", "pptr", "ppptr":
		g.tmpIdx++
		reg := fmt.Sprintf("%%ext.ptrtoint.%d", g.tmpIdx)
		llvmRetType := ffiTypeToLLVM(retFFIType)
		sb.WriteString(fmt.Sprintf("%s%s = ptrtoint %s %s to i64\n", g.indent(), reg, llvmRetType, callReg))
		return reg
	case "bool":
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%ext.cmp.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = icmp ne i32 %s, 0\n", g.indent(), cmpReg, callReg))
		g.tmpIdx++
		reg := fmt.Sprintf("%%ext.zext.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), reg, cmpReg))
		return reg
	}
	return callReg
}
