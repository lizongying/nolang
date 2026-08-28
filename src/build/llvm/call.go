package llvm

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
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
	"i8":       {"i8"},
	"u8":       {"byte", "u8"},
	"i16":      {"i16", "u16"},
	"u16":      {"u16"},
	"i32":      {"char", "i32", "u32"},
	"u32":      {"u32"},
	"i64":      {"i64", "u64"},
	"i128":     {"i128", "u128"},
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
				return toLLVMType(t) + "* " + g.varAddr(a.Value)
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
						rawArrType := fmt.Sprintf("[%d x %s]", arrSize, toLLVMType(elemType))
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
			if t, ok := g.varTypes[a.Value]; ok && g.isStructLLVMType(t) {
				return t + "* " + g.varAddr(a.Value)
			}
			// bool (i1) 變數 → 返回 i64* (not i1*)，因為 Nolang 的引用傳遞模型中
			// 所有整數類型（包括 bool）都以 i64 存儲。函數參數使用 resolveOutputParamLLVMType
			// 將 i1 映射為 i64，若呼叫端傳遞 i1* 會導致類型不匹配（UB），使 LLVM 優化器
			// 在內聯後錯誤地常量傳播 bool 輸出參數的值。
			if t, ok := g.varTypes[a.Value]; ok && t == "i1" {
				return "i64* " + g.varAddr(a.Value)
			}
			// i32 / i16 / i8 等純量型別 — 使用實際型別而非預設 i64*
			if t, ok := g.varTypes[a.Value]; ok {
				return toLLVMType(t) + "* " + g.varAddr(a.Value)
			}
		}
		return "i64* " + g.varAddr(a.Value)
	case *parser.FloatLiteral:
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
		if sb != nil {
			// 协程上下文中使用堆分配（malloc），因为参数指针会被存入 args 结构，
			// async_wrapper 在 yield 后执行时需要访问 — 栈 alloca 在 ret void 后失效。
			if g.coroInAsyncFunc {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 8)\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%s%s.cast = bitcast i8* %s to double*\n", g.indent(), tmpName, tmpName))
				sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s.cast\n", g.indent(), fmt.Sprintf("%f", a.Value), tmpName))
				return "double* " + tmpName + ".cast"
			}
			// alloca 提升至 entry block，避免循環體內每次迭代增長棧
			g.emitEntryAlloca(sb, "%s = alloca double\n", tmpName)
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
			// 协程上下文中使用堆分配（malloc），因为参数指针会被存入 args 结构，
			// async_wrapper 在 yield 后执行时需要访问 — 栈 alloca 在 ret void 后失效。
			if g.coroInAsyncFunc {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 8)\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%s%s.cast = bitcast i8* %s to i64*\n", g.indent(), tmpName, tmpName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s.cast\n", g.indent(), a.Value, tmpName))
				return "i64* " + tmpName + ".cast"
			}
			// alloca 提升至 entry block，避免循環體內每次迭代增長棧
			g.emitEntryAlloca(sb, "%s = alloca i64\n", tmpName)
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
			return toLLVMType(elemLLVMType) + "* " + ev
		}
		// generateIndexExpression always zexts/sexts narrow integer elements
		// (i8/i16/i32) to i64, so the SSA value type is i64 regardless of
		// elemLLVMType. Use i64 for the alloca and store to ensure the
		// pointer type is i64*. This is safe because:
		//   - Callee loads i64 from i64 alloca → correct full value
		//   - Callee loads i8 from i64 alloca → reads low byte = correct value
		//     (zext ensures high bytes are 0)
		//   - Callee stores i8 to i64 alloca → modifies low byte only,
		//     high bytes stay 0 → caller reads correct i64
		storeVal := ev
		argType := elemLLVMType
		if strings.HasPrefix(ev, "%") && g.isIntegerLLVMType(elemLLVMType) {
			argType = "i64"
		}
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, toLLVMType(argType)))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(argType), storeVal, toLLVMType(argType), tmpName))
		}
		return toLLVMType(argType) + "* " + tmpName
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
			if g.isStructLLVMType(fieldType) {
				if !strings.HasPrefix(fieldVal, "%") {
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(fieldType), toLLVMType(fieldType), gepReg))
					}
				} else if _, isStrLit := f.Value.(*parser.StringLiteral); isStrLit || g.isStrPtrReg(fieldVal) {
					// String literals and str pointer regs are %str-long* pointers (alloca),
					// need to load the value before storing.
					if sb != nil {
						g.tmpIdx++
						loadReg := fmt.Sprintf("%%ref.st.fload.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(fieldType), toLLVMType(fieldType), fieldVal))
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), loadReg, toLLVMType(fieldType), gepReg))
					}
				} else if sb != nil {
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
				}
			} else if sb != nil {
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
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
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
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
		// Infer element type from the actual elements (parameter type info
		// unavailable here). Default to i64 for integer elements.
		elemType := "i64"
		if len(a.Elements) > 0 && g.isStringExpr(a.Elements[0]) {
			elemType = "%str-long"
		}
		n := int64(len(a.Elements))
		g.tmpIdx++
		vecName := fmt.Sprintf("%%callvec.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecName))
			// Initialize len/cap/data to 0 so empty slices (n==0) have valid
			// zero values instead of stack garbage (causes segfault when the
			// callee reads .len and gets a huge garbage value).
			g.tmpIdx++
			zeroLenGEP := fmt.Sprintf("%%callvec.zlen.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), zeroLenGEP, vecName))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), zeroLenGEP))
			g.tmpIdx++
			zeroCapGEP := fmt.Sprintf("%%callvec.zcap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), zeroCapGEP, vecName))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), zeroCapGEP))
			g.tmpIdx++
			zeroDataGEP := fmt.Sprintf("%%callvec.zdata.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), zeroDataGEP, vecName))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), zeroDataGEP))
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
					// If generateExprWithSB returned empty (e.g. void function call
					// from an imported module whose definition is not in the AST),
					// use 0 as a fallback to avoid generating invalid IR like
					// "store i64 , i64* ...".
					if ev == "" {
						ev = "0"
					}
					// For struct types (e.g. %str-long), strip the type prefix
					// if present (generateExprWithSB may return "%str-long %reg").
					if g.isStructLLVMType(elemType) && strings.HasPrefix(ev, elemType+" ") {
						ev = ev[len(elemType)+1:]
					}
					g.tmpIdx++
					gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
						g.indent(), gepReg, arrType, arrType, tmpArr, i))
					// For struct types, generateExprWithSB may return a pointer
					// (e.g. StringLiteral returns %str-longlit.N which is a %str-long*).
					// Load the struct value before storing into the array.
					storeVal := ev
					if g.isStructLLVMType(elemType) {
						// StringLiteral returns an alloca pointer; load the value.
						if strings.HasPrefix(ev, "%str-longlit") {
							g.tmpIdx++
							loadReg := fmt.Sprintf("%%callvec.load.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(elemType), toLLVMType(elemType), ev))
							storeVal = loadReg
						}
					} else if g.isIntegerLLVMType(elemType) && elemType != "i64" && strings.HasPrefix(ev, "%") {
						// Truncate i64 to smaller integer types
						g.tmpIdx++
						truncReg := fmt.Sprintf("%%callvec.trunc.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, ev, toLLVMType(elemType)))
						storeVal = truncReg
					}
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(elemType), storeVal, toLLVMType(elemType), gepReg))
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
		// Cross-module string stride safety: detect %str-long* pointers from
		// builtins (fs.read-file, get-line, read-dir, etc.) via isStrPtrReg.
		// Also track for stmt-level free to prevent memory leak.
		if strings.HasPrefix(ev, "%") && g.isStrPtrReg(ev) {
			g.trackStrTemporary(ev)
			return "%str-long* " + ev
		}
		// Cross-module string stride safety: detect %str-long SSA values
		// (from with-len, with-cap, etc.) via ssaTypes.
		// Track the temp alloca for stmt-level free to prevent memory leak.
		if strings.HasPrefix(ev, "%") && g.ssaTypes != nil {
			if ssaType, ok := g.ssaTypes[ev]; ok && ssaType == "%str-long" {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ev, tmpName))
				}
				g.trackStrTemporary(tmpName)
				return "%str-long* " + tmpName
			}
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
			// 1. Try varTypes lookup by variable name (for simple Identifier args)
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
					} else if g.isStructLLVMType(t) {
						ptrType = t + "*"
					} else if g.isIntegerLLVMType(t) {
						ptrType = t + "*"
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
			// 2. DotExpression (e.g. .field): look up field type from structTypes.
			//    Handles Identifier receivers (cfg.field) and chained access
			//    (a.b.field) by using exprResultLLVMType as fallback.
			if dot, ok := arg.(*parser.DotExpression); ok {
				// For struct-typed fields, generate the GEP pointer directly
				// instead of load+alloca+store. This is critical for method
				// call receivers (e.g. j.pool.parse()): the method modifies the
				// struct in-place via the self pointer. A value copy would
				// discard all mutations and may contain uninitialized data.
				fieldElemType := g.exprResultLLVMType(arg)
				if fieldElemType != "" && fieldElemType != "i64" && g.isStructLLVMType(fieldElemType) {
					ptrType = fieldElemType + "*"
					if sb != nil {
						ptrAddr := g.generateExprPtr(sb, arg)
						if ptrAddr != "" {
							return ptrType + " " + ptrAddr
						}
					}
				}
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					if g.varTypes != nil {
						if t, ok := g.varTypes[ident.Value]; ok && g.isStructLLVMType(t) {
							structName := strings.TrimPrefix(t, "%")
							// D3 fix: use resolveStructFields to handle module-prefixed struct names
							if fields, _ := g.resolveStructFields(structName); fields != nil {
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
				// For chained DotExpression (a.b.field) or non-Identifier receivers,
				// exprResultLLVMType can resolve the full chain to the field type.
				if ptrType == "i64*" {
					if et := g.exprResultLLVMType(arg); et != "" && et != "i64" {
						ptrType = et + "*"
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
			// 3. Fallback: check ssaTypes for the SSA register's type.
			//    generateDotExpression and other generators record the loaded
			//    value's type in ssaTypes. This catches cases where varTypes
			//    and structTypes lookups fail (e.g. module-prefixed struct names,
			//    complex chained access).
			if ptrType == "i64*" && g.ssaTypes != nil {
				if ssaType, ok := g.ssaTypes[ev]; ok && ssaType != "" && ssaType != "i64" {
					ptrType = ssaType + "*"
				}
			}
			elemType := strings.TrimSuffix(ptrType, "*")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, toLLVMType(elemType)))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %s\n", g.indent(), toLLVMType(elemType), ev, toLLVMType(elemType)+"*", tmpName))
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

// idxLenUpdateInfo records info needed to auto-update vec/arr len after a
// multi-assign call that wrote elements via IndexExpression output targets.
type idxLenUpdateInfo struct {
	varName string
	idxReg  string
	isConst bool
}

// generateOutputIdxPtr generates a write pointer for an IndexExpression used as
// a multi-assign output target (e.g. `fields[n], pos = f(...)`).
// Unlike generateCallArg (which READS the element and triggers len-based bounds
// check), this generates a GEP pointer with cap-based bounds check (matching the
// regular vec[i] = val write path), allowing writes to freshly with-cap'd containers
// where len == 0. Returns ("", nil) if the expression is not a supported vec/arr index.
func (g *Generator) generateOutputIdxPtr(sb *strings.Builder, v *parser.IndexExpression) (string, *idxLenUpdateInfo) {
	varName := ""
	if ident, ok := v.Left.(*parser.Identifier); ok {
		varName = ident.Value
	} else {
		return "", nil
	}
	if varName == "" {
		return "", nil
	}
	t, ok := g.varTypes[varName]
	if !ok {
		return "", nil
	}
	idx := g.generateExprWithSB(sb, v.Index)
	if strings.HasPrefix(idx, "%") {
		idxType := g.intExprLLVMType(v.Index)
		if idxType != "" && toLLVMType(idxType) != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%outidx.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zextReg, toLLVMType(idxType), idx))
			}
			idx = zextReg
		}
	}
	_, isConstIdx := v.Index.(*parser.IntegerLiteral)
	varRef := llvmVarRef(varName)
	if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
		varRef = llvmGlobalRef(varName)
	}
	if t == "%vec" {
		llvmElemType := "i64"
		if et, ok := g.arrayElemTypes[varName]; ok {
			llvmElemType = toLLVMType(et)
		}
		// Cap-based bounds check (matching vec[i] = val write path), skip for const idx
		if !isConstIdx {
			vecCap := g.emitVecCapLoad(sb, varRef)
			g.emitBoundsCheck(sb, idx, vecCap)
		}
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%outvec.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLoad := fmt.Sprintf("%%outvec.data.%d", g.tmpIdx)
		g.tmpIdx++
		dataTyped := fmt.Sprintf("%%outvec.typed.%d", g.tmpIdx)
		g.tmpIdx++
		elemGEP := fmt.Sprintf("%%outvec.elem.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
				g.indent(), dataGEP, varRef))
			dataLoad = g.loadDataPtrField(sb, dataGEP)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataTyped, dataLoad, llvmElemType))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
				g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
		}
		return llvmElemType + "* " + elemGEP, &idxLenUpdateInfo{varName: varName, idxReg: idx, isConst: isConstIdx}
	}
	if t == "%arr" {
		llvmElemType := "i64"
		if et, ok := g.arrayElemTypes[varName]; ok {
			llvmElemType = toLLVMType(et)
		}
		if !isConstIdx {
			arrLen := g.emitArrLenLoad(sb, varRef)
			g.emitBoundsCheck(sb, idx, arrLen)
		}
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%outarr.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLoad := fmt.Sprintf("%%outarr.data.%d", g.tmpIdx)
		g.tmpIdx++
		dataTyped := fmt.Sprintf("%%outarr.typed.%d", g.tmpIdx)
		g.tmpIdx++
		elemGEP := fmt.Sprintf("%%outarr.elem.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
				g.indent(), dataGEP, varRef))
			dataLoad = g.loadDataPtrField(sb, dataGEP)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataTyped, dataLoad, llvmElemType))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %s\n",
				g.indent(), elemGEP, llvmElemType, llvmElemType, dataTyped, idx))
		}
		return llvmElemType + "* " + elemGEP, nil
	}
	if t == "%str-long" {
		// str[i] as output target: cap-based bounds check
		if !isConstIdx {
			strCap := g.emitStrCapLoad(sb, varRef)
			g.emitBoundsCheck(sb, idx, strCap)
		}
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%outstr.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLoad := fmt.Sprintf("%%outstr.data.%d", g.tmpIdx)
		g.tmpIdx++
		elemGEP := fmt.Sprintf("%%outstr.elem.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n",
				g.indent(), dataGEP, varRef))
			dataLoad = g.loadDataPtrField(sb, dataGEP)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
				g.indent(), elemGEP, dataLoad, idx))
		}
		return "i8* " + elemGEP, &idxLenUpdateInfo{varName: varName, idxReg: idx, isConst: isConstIdx}
	}
	return "", nil
}

// emitVecLenAutoUpdate updates vec.len to max(len, idx+1) after a multi-assign
// call wrote an element via IndexExpression output target. Without this, subsequent
// reads of the written element would fail bounds check (len still 0 after with-cap).
func (g *Generator) emitVecLenAutoUpdate(sb *strings.Builder, varName string, idx string, isConst bool) {
	if sb == nil {
		return
	}
	varRef := llvmVarRef(varName)
	if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
		varRef = llvmGlobalRef(varName)
	}
	// For constant indices, directly set len = max(len, idx+1) unconditionally
	// (the write already happened via the output pointer).
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%ma.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	curLen := fmt.Sprintf("%%ma.cur-len.%d", g.tmpIdx)
	g.tmpIdx++
	newLen := fmt.Sprintf("%%ma.new-len.%d", g.tmpIdx)
	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%ma.cmp.%d", g.tmpIdx)
	g.tmpIdx++
	finalLen := fmt.Sprintf("%%ma.final-len.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, varRef))
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), newLen, idx))
	sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, newLen, curLen))
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, newLen, curLen))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
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
		// 含 awy 的函数被变换为无栈协程（coro_resume.N），原始 @fn 不再存在。
		// 直接调用此类函数时，创建 coro_state + task 并启动事件循环驱动其完成。
		// （否则会生成 call void @fn() 引用未定义符号。）
		// 仅处理非协程上下文中的直接调用；协程内部应使用 run+awy。
		if g.asyncFuncCoroNum != nil {
			if num, isCoro := g.asyncFuncCoroNum[ident.Value]; isCoro && !g.coroInAsyncFunc {
				return g.generateCoroCall(sb, expr, ident.Value, num)
			}
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
			} else if _, ok := dot.Receiver.(*parser.DotExpression); ok {
				// 結構欄位接收者（如 j.pool.parse(s, 0)）
				// 透過 exprResultLLVMType 推導欄位型別，再映射到 nolang 型別名查找方法
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
				// Slice return types (e.g. []i64 from getgroups) use %vec.
				// The builtin returns a %vec* (alloca); we load it to %vec below.
				if _, isSlice := m.Return[0].(*parser.SliceType); isSlice {
					storeType = "%vec"
				}
			}
			// CLibCall builtins (如 i64.to-str) 透過 sprintf alloca 返回 %str-long* pointer。
			// ForwardFunc builtins (如 get-line) 透過 alloca 返回 %str-long* pointer。
			// 兩者都需要 load 成 %str-long value。
			// Slice-returning builtins (如 getgroups) 同樣透過 alloca 返回 %vec* pointer，需 load 成 %vec value。
			// ForwardFunc builtins (如 net-dial) 返回 i64 value。
			actualVal := retReg
			if storeType == "%str-long" {
				// retReg 是 %str-long* (alloca)，需 load 成 %str-long value
				g.tmpIdx++
				loadReg := fmt.Sprintf("%%builtin.load.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, retReg))
				actualVal = loadReg
			} else if storeType == "%vec" {
				// retReg 是 %vec* (alloca)，需 load 成 %vec value
				g.tmpIdx++
				loadReg := fmt.Sprintf("%%builtin.load.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), loadReg, retReg))
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
					// 当第一个返回值是字符串/切片类型并存储到变量时，
					// 从 stmtTemporaries 中移除临时指针，避免语句结束时
					// 释放变量引用的堆内存（use-after-free）。
					// 变量通过 heapVars/trackLocalHeapVar 接管 data 所有权。
					if outIdx == 0 && (curStoreType == "%str-long" || curStoreType == "%vec") {
						g.untrackStmtTemporary(retReg)
						// 标记变量为堆变量，以便后续赋值时释放旧值
						g.trackLocalHeapVar(varName, curStoreType)
					}
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

		// Module-function receiver fix (nested call dispatch):
		// Functions declared inside a struct module namespace (e.g. process.cmd)
		// carry a hidden `%self` receiver as their first LLVM parameter. When called
		// via a module-qualified name with no instance (always the case for these
		// functions), no receiver is passed by the caller. Without a dummy self, every
		// argument shifts right by one and the trailing output parameter (e.g. %err)
		// is left undefined → null → memset crash inside the callee (opt/llc mis-schedule).
		// Detect it via funcParamLLVMTypes (includes the receiver) vs funcParamCount
		// (excludes it): a +1 difference means a receiver exists but was not passed.
		if innerMethodRecv == nil && innerFnName != "" && g.funcParamCount != nil && g.funcParamLLVMTypes != nil && sb != nil && !g.funcIsVariadic[innerFnName] {
			if pt, ok := g.funcParamLLVMTypes[innerFnName]; ok && len(pt) > 0 {
				// funcParamCount includes the (unpassed) self receiver for module
				// functions; a +1 gap vs the supplied input args means self is missing.
				if pc, ok := g.funcParamCount[innerFnName]; ok && pc == len(innerArgs)+1 {
					// funcParamLLVMTypes stores element types (no trailing '*');
					// the receiver is always passed by reference, matching the
					// callee's `%T* %self` first parameter.
					recvElemType := strings.TrimSuffix(pt[0], "*")
					if recvElemType == "" {
						recvElemType = "i64"
					}
					g.tmpIdx++
					dummySelf := fmt.Sprintf("%%dummy.self.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), dummySelf, recvElemType))
					sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), recvElemType, recvElemType, dummySelf))
					// Prepend pointer to dummy self as the FIRST argument.
					innerArgs = append([]string{recvElemType + "* " + dummySelf}, innerArgs...)
				}
			}
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
				// Track IndexExpression output args on vec/arr for post-call len update.
				// When fields[n] is an output target, the called function writes via pointer;
				// after return, vec.len must be updated to max(len, n+1) so subsequent reads pass bounds check.
				var lenUpdates []idxLenUpdateInfo
				for outIdx, outArg := range expr.Arguments {
					// Auto-allocate undeclared output variables (e.g. `total` in `.c.recv-all()(response, total)`)
					if ident, ok := outArg.(*parser.Identifier); ok {
						// Output targets are the LHS of a multi-assignment / curried
						// call — they are ALWAYS variables, never function references.
						// Register them as local names so generateCallArg emits a typed
						// pointer (e.g. %str-long*) instead of a function pointer
						// (void(...)**). This is critical when the target name collides
						// with a global function of the same name (e.g. the std print
						// functions `out`/`err`): without this, `out, code, err = f()`
						// would pass a `void(...)**` slot holding @out/@err, leaving the
						// real %out/%err locals unwritten and corrupting the call's
						// output parameters (opt -O2/-O3 then mis-schedules and crashes).
						if g.funcLocalNames == nil {
							g.funcLocalNames = make(map[string]bool)
						}
						g.funcLocalNames[ident.Value] = true
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
						allArgs = append(allArgs, g.generateCallArg(sb, outArg))
						continue
					}
					// IndexExpression output target (e.g. fields[n] = f()): generate write
					// pointer with cap-based bounds check (matching vec[i] = val write path).
					// Using generateCallArg here would READ the element (len-based bounds check),
					// crashing on freshly with-cap'd containers where len == 0.
					if idxExpr, ok := outArg.(*parser.IndexExpression); ok {
						if ptrArg, lu := g.generateOutputIdxPtr(sb, idxExpr); ptrArg != "" {
							allArgs = append(allArgs, ptrArg)
							if lu != nil {
								lenUpdates = append(lenUpdates, *lu)
							}
							continue
						}
					}
					allArgs = append(allArgs, g.generateCallArg(sb, outArg))
				}
				// Guard against empty function name: if innerFnName is empty,
				// skip the call to avoid generating malformed IR `call void @(...)`.
				if innerFnName != "" {
					sb.WriteString(fmt.Sprintf("%scall void @%s(%s)\n", g.indent(), sanitizeLLVMName(innerFnName), strings.Join(allArgs, ", ")))
					// The called function wrote elements via pointer; without updating len,
					// subsequent reads (fields[0]) would fail bounds check (len still 0).
					for _, lu := range lenUpdates {
						g.emitVecLenAutoUpdate(sb, lu.varName, lu.idxReg, lu.isConst)
					}
				}
				return ""
			}
			// 純 void（無輸出參數）：直接調用
			if innerFnName != "" {
				sb.WriteString(fmt.Sprintf("%scall void @%s(%s)\n", g.indent(), sanitizeLLVMName(innerFnName), strings.Join(innerArgs, ", ")))
			}
			return ""
		}

		// 有返回值：生成 call 並捕獲結果
		g.tmpIdx++
		retReg := fmt.Sprintf("%%callret.%d", g.tmpIdx)
		if innerFnName != "" {
			sb.WriteString(fmt.Sprintf("%s%s = call %s @%s(%s)\n", g.indent(), retReg, retType, sanitizeLLVMName(innerFnName), strings.Join(innerArgs, ", ")))
		} else {
			fmt.Fprintf(os.Stderr, "codegen warning: skipping call with empty function name\n")
			return ""
		}

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
	// isModuleQualified indicates that expr.Function is a DotExpression
	// whose receiver is a module name (not a variable in varTypes).
	// In this case, the receiver must NOT be prepended to the arguments
	// (e.g. number.rotate-left(t, 7) → rotate-left(t, 7), not rotate-left(number, t, 7)).
	isModuleQualified := false
	if ident, ok := expr.Function.(*parser.Identifier); ok {
		fnName = ident.Value
	} else if dot, ok := expr.Function.(*parser.DotExpression); ok {
		// 支援多段限定名（如 net.quic.varint-encode）：
		// 鏈式 DotExpression 的 Receiver 本身也是 DotExpression，
		// 需遞迴展開為完整名稱。
		fnName = flattenDottedExpr(dot)
		// For non-Identifier receivers (e.g. IndexExpression, CallExpression),
		// flattenDottedExpr returns "". Use just the method name so that
		// the method receiver resolution at ~L1424 can handle it.
		if fnName == "" {
			fnName = dot.Property
		}
		// Detect module-qualified calls: if the first segment is not a
		// variable in varTypes, treat it as a module name. Strip the prefix
		// when the short name is a user function (registered without the
		// module prefix in funcRetTypes, e.g. char-to-str, i64-to-str).
		if strings.Contains(fnName, ".") && g.varTypes != nil {
			if idx := strings.Index(fnName, "."); idx >= 0 {
				firstSegment := fnName[:idx]
				if _, isVar := g.varTypes[firstSegment]; !isVar {
					shortName := fnName[idx+1:]
					// Check if shortName is a user function (without module prefix)
					if g.funcRetTypes != nil {
						if _, hasUserFn := g.funcRetTypes[shortName]; hasUserFn {
							fnName = shortName
							isModuleQualified = true
						} else if _, hasFullFn := g.funcRetTypes[fnName]; !hasFullFn {
							// Full name not registered either — could be a
							// module-qualified builtin (e.g. number.rotate-left).
							// Mark as module-qualified so receiver is not prepended.
							isModuleQualified = true
						}
					}
				} else if _, isIdentReceiver := dot.Receiver.(*parser.Identifier); !isIdentReceiver {
					// Chained method call (e.g. data.len.to-str()):
					// The first segment IS a variable, but the receiver is
					// NOT a simple Identifier (it's a DotExpression, IndexExpression,
					// etc.). Don't use the flattened chain as a function name;
					// use just the method name and let the method receiver
					// resolution at ~L1697 resolve the correct type-prefixed name.
					fnName = dot.Property
				}
			}
		}
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
				//
				// ⚠️ Only check the FULL function name (e.g. "gzip-decompress") as a
				// builtin. Do NOT strip the prefix and check the short name (e.g. "max"
				// from "[]t.max"), because a user-defined method like []t.max has a
				// full Nolang body and shares its short name "max" with the unrelated
				// math-max ForwardFunc builtin. Stripping would incorrectly classify
				// []t.max as a ForwardFunc, leaving skipBuiltin=false and causing the
				// prefix-stripping at ~L1295 to mangle fnName to "max", which then
				// fails all funcRetTypes/funcNumResults/funcResultLLVMType lookups,
				// so voidSingleOutput is never triggered and the call loses its
				// output parameter.
				isForwardBuiltin := false
				m := builtin.FindBuiltinMethod(fnName)
				if m == nil && strings.Contains(fnName, ".") {
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
				// Same ForwardFunc check as above for non-dotted names.
				// For dotted names (e.g. "str.len"), strip the prefix to find
				// the builtin by its short name ("len"), matching the logic
				// in the first skipBuiltin check above. Without this, the
				// second check would override the first check's decision for
				// dotted ForwardFunc builtins like str.len.
				isForwardBuiltin := false
				m := builtin.FindBuiltinMethod(fnName)
				if m == nil && strings.Contains(fnName, ".") {
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
	}
	if !skipBuiltin {
		m := builtin.FindBuiltinMethod(fnName)
		if m == nil && strings.Contains(fnName, ".") {
			// Try stripping module prefix (e.g. "fs.is-dir" → "is-dir").
			// Safe because skipBuiltin already verified no user function exists with the full name.
			// BUT: if the first segment is a variable name (e.g. "data.load-le-u32"),
			// this is a method call, not a module-qualified call. Don't strip —
			// let the method resolution code at line ~1094 handle it.
			//
			// 例外：若 stripped shortName 是 ReceiverGlobal builtin（如 sqrt、abs、sin），
			// 此類「全域方法」不依賴接收者型別，receiver 會從 DotExpression 補上作為
			// 第一個參數。即使 first segment 是變數（如 r2.sqrt() → sqrt(r2)），
			// 仍可安全剝離前綴，交由 LLVMIntrinsic 路徑處理。
			if idx := strings.Index(fnName, "."); idx >= 0 {
				firstSegment := fnName[:idx]
				_, isVar := g.varTypes[firstSegment]
				shortName := fnName[idx+1:]
				m2 := builtin.FindBuiltinMethod(shortName)
				if m2 != nil {
					if !isVar {
						// Don't strip if fnName itself is a registered user function
						// (e.g. []t.max is a user-defined generic method, not the
						// global math.max builtin). Stripping would incorrectly
						// dispatch to the builtin, ignoring the user's definition.
						if g.funcRetTypes != nil {
							if _, isUserFn := g.funcRetTypes[fnName]; isUserFn {
								// fnName is a user function; skip builtin dispatch
							} else {
								m = m2
								fnName = shortName
							}
						} else {
							m = m2
							fnName = shortName
						}
					} else if m2.ReceiverType == builtin.ReceiverGlobal {
						// For ReceiverGlobal builtins (e.g. sqrt, abs), stripping
						// the prefix is normally safe. BUT: if the variable has a
						// type-specific builtin (e.g. vec.truncate), prefer that
						// to avoid dispatching to the wrong builtin (e.g. POSIX
						// truncate instead of vec.truncate). Let the method
						// resolution code at ~L1564 handle the dispatch instead.
						shouldStrip := true
						if recvType, ok := g.varTypes[firstSegment]; ok {
							recvTypeName := strings.TrimPrefix(recvType, "%")
							// Build candidate type names to check for type-specific
							// builtins (e.g. vec.truncate, str.truncate).
							candTypes := []string{recvTypeName}
							// nolang type aliases (e.g. str-long → str)
							if aliases, ok2 := llvmTypeToNolang[recvTypeName]; ok2 {
								candTypes = append(candTypes, aliases...)
							}
							// Option (?T): also try the inner type
							if recvTypeName == "option" && g.optionInnerTypes != nil {
								if innerType, ok3 := g.optionInnerTypes[firstSegment]; ok3 {
									innerSrc := strings.TrimPrefix(innerType, "%")
									candTypes = append(candTypes, innerSrc)
									if innerAliases, ok4 := llvmTypeToNolang[innerSrc]; ok4 {
										candTypes = append(candTypes, innerAliases...)
									}
								}
							}
							for _, ct := range candTypes {
								if builtin.FindBuiltinMethod(ct+"."+shortName) != nil {
									shouldStrip = false
									break
								}
							}
						}
						if shouldStrip {
							m = m2
							fnName = shortName
						}
					}
				}
			}
		}
		// Chained method calls (e.g. .data.truncate(1), .name.clear()) had their
		// fnName reduced to the bare method name by the code at ~L1259 above. If
		// the receiver expression's type has a type-specific builtin
		// (vec.truncate, str.clear, ...), prefer it over a same-named global C
		// lib call (e.g. POSIX truncate(path, len)): otherwise the CLibCall would
		// receive garbage arguments and produce invalid IR. This mirrors the
		// ReceiverGlobal guard at ~L1462, which only protects the case where
		// fnName still carries a "var." prefix.
		if m != nil && m.CLibCall != nil {
			if dot, isDot := expr.Function.(*parser.DotExpression); isDot {
				recv := dot.Receiver
				if ge, ok := recv.(*parser.GroupedExpression); ok {
					recv = ge.Expression
				}
				if _, isIdent := recv.(*parser.Identifier); !isIdent {
					if elemType := g.exprResultLLVMType(recv); elemType != "" {
						srcType := strings.TrimPrefix(elemType, "%")
						candTypes := []string{srcType}
						if aliases, ok2 := llvmTypeToNolang[srcType]; ok2 {
							candTypes = append(candTypes, aliases...)
						}
						for _, ct := range candTypes {
							if m2 := builtin.FindBuiltinMethod(ct + "." + fnName); m2 != nil {
								m = m2
								fnName = m2.MethodName
								maybeRenableLLVMName()
								break
							}
						}
					}
				}
			}
		}
		if m != nil {
			if m.LLVMIntrinsic != "" {
				// 對 method call（如 r2.sqrt()），receiver 是第一個參數
				// expr.Arguments 不含 receiver，需從 DotExpression 補上
				// 但模組限定呼叫（如 number.sqrt(x)）不應 prepend receiver
				methodArgs := expr.Arguments
				if dot, isDot := expr.Function.(*parser.DotExpression); isDot && !isModuleQualified {
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
				// LLVM intrinsics like @llvm.log.f64 require at least one
				// double argument. When called with zero arguments (e.g.
				// `log()` in test code), emit 0.0 as a placeholder to avoid
				// generating invalid IR that causes "Intrinsic called with
				// incompatible signature" during LLVM optimization.
				if len(args) == 0 {
					argStr = "double 0.0"
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
				if os.Getenv("NOLANG_DEBUG_PUSH") != "" && m.ForwardFunc == "vec-push" {
					fmt.Fprintf(os.Stderr, "[debug-push] reached ForwardFunc dispatch, fnName=%q, ForwardFunc=%q, args=%d\n", fnName, m.ForwardFunc, len(expr.Arguments))
				}
				// When a method call (e.g. buf.load-le-u32(0)) is dispatched via
				// the global builtin form (prefix stripped at line ~1014), the
				// receiver must be extracted from the DotExpression and passed to
				// genForwardFunc so it can be prepended to the args list.
				// 但模組限定呼叫（如 number.rotate-left(t, 7)）不應 prepend receiver。
				var fwdReceiver parser.Expression = nil
				if dot, isDot := expr.Function.(*parser.DotExpression); isDot && !isModuleQualified {
					fwdReceiver = dot.Receiver
					// Unwrap GroupedExpression: (self.data).push(x) → self.data.push(x),
					// matching the unwrap the method-resolution path performs below.
					if ge, ok := fwdReceiver.(*parser.GroupedExpression); ok {
						fwdReceiver = ge.Expression
					}
				}
				// vec-push writes the push expansion directly into sb and returns ""
				// on success. For receiver forms the expansion supports (a variable or
				// a struct field), "" means "handled": return early so the
				// method-resolution dispatch site below does not emit the expansion
				// a SECOND time. Bug 07 (struct-field-push-broken): without this,
				// .data.push(val) on a struct field appended every element twice
				// (len grew by 2 per push). Unsupported receiver forms (index/call/
				// slice results) still fall through to method resolution as before.
				vecPushHandled := false
				if m.ForwardFunc == "vec-push" {
					recv := fwdReceiver
					if recv == nil && len(expr.Arguments) > 0 {
						recv = expr.Arguments[0]
					}
					if ge, ok := recv.(*parser.GroupedExpression); ok {
						recv = ge.Expression
					}
					switch recv.(type) {
					case *parser.Identifier, *parser.DotExpression:
						vecPushHandled = true
					}
				}
				// vec-clear/str-clear/vec-truncate/str-truncate write their expansion
				// directly into sb and return "" on success — same as vec-push on the
				// receiver forms it supports (vecPushHandled). Return early so the
				// method-resolution dispatch site below does not emit the expansion
				// a SECOND time (bug 07 family: struct-field push/clear/truncate were
				// either silently no-ops, emitted twice, or got mis-dispatched to a
				// same-named POSIX call).
				fwdHandled := vecPushHandled || m.ForwardFunc == "memcpy" || m.ForwardFunc == "memset" || m.ForwardFunc == "str-clear" || m.ForwardFunc == "str-truncate" || m.ForwardFunc == "vec-clear" || m.ForwardFunc == "vec-truncate"
				if r := g.genForwardFunc(sb, m.ForwardFunc, expr, fwdReceiver); r != "" || fwdHandled {
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
		// Debug: log receiver type for push calls
		if dot.Property == "push" && os.Getenv("NOLANG_DEBUG_PUSH") != "" {
			fmt.Fprintf(os.Stderr, "[debug-push] receiverExpr type=%T value=%v property=%q\n", receiverExpr, receiverExpr, dot.Property)
		}
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
								et = strings.TrimPrefix(et, "%")
								if elemAliases, ok := llvmTypeToNolang[et]; ok {
									for _, alias := range elemAliases {
										candidates = append(candidates, "[]"+alias)
									}
									// For arr receivers, also construct [n]t mangled name
									// candidates (e.g. _3xi64.clone) to match transpiler's
									// cloneAndSubstitute output. vec receivers don't need this
									// because []T candidates already cover slice methods.
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
							// For vec types (e.g. []byte.push), also check just the method name
							// because vec.go registers push without the []T prefix
							if srcType == "vec" || srcType == "arr" {
								if m := builtin.FindBuiltinMethod(dot.Property); m != nil {
									fnName = dot.Property
									methodReceiver = recv
									break
								}
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
			// 結構欄位接收者（如 c.name.trim()、self.buf.len、it.status-code.to-str()）
			// 透過 exprResultLLVMType 推導欄位型別，再映射到 nolang 型別名查找方法
			elemType := g.exprResultLLVMType(receiverExpr)
			srcType := strings.TrimPrefix(elemType, "%")
			// 先嘗試聯合型別別名（如 i64 → int.to-str），與 Identifier 接收者路徑保持一致
			if g.unionAliases != nil {
				unionSrcType := srcType
				if unionSrcType == "double" {
					unionSrcType = "f64"
				} else if unionSrcType == "float" {
					unionSrcType = "f32"
				}
				for aliasName := range g.unionAliases {
					if !g.isMemberOfUnionTransitive(unionSrcType, aliasName, make(map[string]bool)) {
						continue
					}
					// Try monomorphized name first: unionAlias.methodName__memberType
					monoName := aliasName + "." + dot.Property + "__" + unionSrcType
					if _, exists := g.funcRetTypes[monoName]; exists {
						fnName = monoName
						methodReceiver = receiverExpr
						break
					}
					// Try non-monomorphized name: unionAlias.methodName
					unionName := aliasName + "." + dot.Property
					if _, exists := g.funcRetTypes[unionName]; exists {
						fnName = unionName
						methodReceiver = receiverExpr
						break
					}
				}
			}
			candidates := []string{srcType}
			if primAliases, ok := llvmTypeToNolang[srcType]; ok {
				candidates = append(candidates, primAliases...)
			}
			// vec/arr fields: construct []T candidates (e.g., decoder.out.push)
			if srcType == "vec" || srcType == "arr" {
				// For DotExpression receivers, we already have elemType from exprResultLLVMType
				// which returns the field's LLVM type. We need to construct []T candidates.
				// But we need the element type of the vec/arr, not the vec/arr itself.
				// Skip this for now - the vec.push builtin will handle it directly
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
				// For vec types, also check just the method name
				if srcType == "vec" || srcType == "arr" {
					if methodReceiver == nil {
						if m := builtin.FindBuiltinMethod(dot.Property); m != nil {
							fnName = dot.Property
							methodReceiver = receiverExpr
							break
						}
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
					if r := g.genForwardFunc(sb, m.ForwardFunc, expr, methodReceiver); r != "" || m.ForwardFunc == "memcpy" || m.ForwardFunc == "memset" || m.ForwardFunc == "arr-zero" || m.ForwardFunc == "str-clear" || m.ForwardFunc == "str-truncate" || m.ForwardFunc == "vec-truncate" || m.ForwardFunc == "vec-clear" || m.ForwardFunc == "vec-push" {
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
					// Only treat as output param if caller provided all params
					// (including the output param) as arguments.
					// funcParamCount includes the implicit self receiver for methods,
					// but expr.Arguments does not. For method calls, the caller has
					// provided the output param when effectiveArgs (len+1 for self)
					// >= paramCount. For plain calls, use > (original logic).
					paramCount := len(expr.Arguments)
					if g.funcParamCount != nil {
						if pc, ok := g.funcParamCount[fnName]; ok {
							paramCount = pc
						}
					}
					if methodReceiver != nil {
						// Method call: self is implicit, so effective args = len+1.
						// Use > (not >=): when effective args == paramCount, the caller
						// provided exactly the input params (self + args), NOT the output.
						// The output param is only present when effective args > paramCount.
						// Using >= would misidentify the last input arg (e.g. `end` in
						// s.slice(0, end)) as the output param, causing the call to be
						// treated as statement-form (void return) instead of expression-form
						// (voidSingleOutput), producing empty store values in codegen.
						if len(expr.Arguments)+1 > paramCount {
							hasOutputParam = true
						}
					} else {
						if len(expr.Arguments) > paramCount {
							hasOutputParam = true
						}
					}
				}
			}
		}
	}

	// 單輸出參數：調用方未顯式傳遞輸出變數（如 v1 = s.to-i64()）。
	// Nolang 函數的 funcRetTypes 為語意回傳型別（如 %option），但實際 LLVM 簽名是 void + 輸出指標。
	// 此類函數需分配臨時空間、傳遞指標、調用後載入結果作為返回值。
	// 注意：啟發式檢測的輸出參數（funcHeuristicOutput）已存在於 fd.Parameters 中，
	// 函數定義已將其作為常規 LLVM 參數生成。當調用方已傳遞所有參數（語句形式）時不加返回槽；
	// 但當調用方未傳遞輸出參數（表達式形式，如 resp = http.get(url)）時，
	// 仍需分配臨時 buffer 並作為最後一個參數傳遞，否則會生成缺少參數的 void call。
	voidSingleOutput := false
	voidSingleOutputType := ""
	triggerVoidSingle := (retType == "void" || isNolangSingleResult) && g.funcNumResults != nil && g.funcResultLLVMType != nil
	if triggerVoidSingle {
		if n, ok := g.funcNumResults[fnName]; ok && n == 1 {
			if ts, ok := g.funcResultLLVMType[fnName]; ok && len(ts) == 1 {
				if h, ok := g.funcHeuristicOutput[fnName]; ok && h {
					// 啟發式輸出：輸出參數已在 fd.Parameters 中。
					// 區分語句形式（調用方已傳遞所有參數）和表達式形式（缺少輸出參數）。
					heuristicParamCount := len(expr.Arguments)
					if g.funcParamCount != nil {
						if pc, ok := g.funcParamCount[fnName]; ok {
							heuristicParamCount = pc
						}
					}
					// 考慮 methodReceiver：方法調用時 receiver 會被 prepend 到參數列表
					effectiveArgs := len(expr.Arguments)
					if methodReceiver != nil {
						effectiveArgs++
					}
					if effectiveArgs < heuristicParamCount {
						// 表達式形式：調用方未傳遞輸出參數，需分配臨時 buffer
						voidSingleOutput = true
						voidSingleOutputType = ts[0]
					}
					// 否則：語句形式，調用方已傳遞所有參數，不加返回槽
					// 但僅當 hasOutputParam 已捕獲輸出參數時跳過；
					// 否則仍需分配臨時 buffer（表達式形式，如 v = f(x)）。
				} else {
					if !hasOutputParam {
						voidSingleOutput = true
						voidSingleOutputType = ts[0]
					}
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
			// 若引數為當前函數的 out 參數，標記為已賦值（bit=1）。
			// 被調用函數可能通過指標寫入 out（如 fe-tobytes(out, h)），
			// 若不標記，emitRetInitZeroFill 會在 return 時以零值覆蓋已寫入的數據。
			if g.outputParamNames != nil && g.outputParamNames[a.Value] && sb != nil {
				g.emitSetRetInitBit(sb, a.Value)
			}
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
							rawArrType := fmt.Sprintf("[%d x %s]", arrSize, toLLVMType(elemType))
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
			// bool (i1) 變數作為函數參數傳遞時，使用 i64* 而非 i1*。
			// 原因：resolveOutputParamLLVMType 將 bool 映射為 i64，函數簽名使用 i64*。
			// 若呼叫端傳遞 i1* 會導致類型不匹配（UB），使 LLVM 優化器錯誤地常量傳播。
			if argType == "i1" {
				argType = "i64"
			}
			// 對 by-reference 函數呼叫，先將引數值存到暫存變數，
			// 避免被呼叫函數修改原始變數（例如 gcd 會修改 a, b）
			// Nolang 帶 result parameter 的函數 retType 也是 "void"，需額外檢查 isNolangSingleResult
			if (retType != "void" || isNolangSingleResult) && g.isIntegerLLVMType(argType) {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%arg.save.%d", g.tmpIdx)
				g.tmpIdx++
				tmpVal := fmt.Sprintf("%%arg.val.%d", g.tmpIdx)
				irArgType := toLLVMType(argType)
				if sb != nil {
					// alloca 提升至 entry block，避免循環體內每次迭代增長棧（與 FloatLiteral 相同策略）
					g.emitEntryAlloca(sb, "%s = alloca %s\n", tmpName, irArgType)
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), tmpVal, irArgType, irArgType, g.varAddr(a.Value)))
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), irArgType, tmpVal, irArgType, tmpName))
				}
				return irArgType + "* " + tmpName
			}
			return toLLVMType(argType) + "* " + g.varAddr(a.Value)
		case *parser.FloatLiteral:
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				// alloca 提升至 entry block，避免循環體內每次迭代增長棧
				g.emitEntryAlloca(sb, "%s = alloca double\n", tmpName)
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
				// alloca 提升至 entry block，避免循環體內每次迭代增長棧
				g.emitEntryAlloca(sb, "%s = alloca %s\n", tmpName, elemType)
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
				return toLLVMType(elemLLVMType) + "* " + ev
			}
			// generateIndexExpression always zexts/sexts narrow integer elements
			// (i8/i16/i32) to i64, so the SSA value type is i64 regardless of
			// elemLLVMType. Use i64 for the alloca and store to ensure the
			// pointer type is i64*. This is safe because:
			//   - Callee loads i64 from i64 alloca → correct full value
			//   - Callee loads i8 from i64 alloca → reads low byte = correct value
			//     (zext ensures high bytes are 0)
			//   - Callee stores i8 to i64 alloca → modifies low byte only,
			//     high bytes stay 0 → caller reads correct i64
			storeVal := ev
			argType := elemLLVMType
			if strings.HasPrefix(ev, "%") && g.isIntegerLLVMType(elemLLVMType) {
				argType = "i64"
			}
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, toLLVMType(argType)))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(argType), storeVal, toLLVMType(argType), tmpName))
			}
			return toLLVMType(argType) + "* " + tmpName
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
				if g.isStructLLVMType(fieldType) {
					if !strings.HasPrefix(fieldVal, "%") {
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(fieldType), toLLVMType(fieldType), gepReg))
						}
					} else if _, isStrLit := f.Value.(*parser.StringLiteral); isStrLit || g.isStrPtrReg(fieldVal) {
						// String literals and str pointer regs are %str-long* pointers (alloca),
						// need to load the value before storing.
						if sb != nil {
							g.tmpIdx++
							loadReg := fmt.Sprintf("%%ref.st.fload.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(fieldType), toLLVMType(fieldType), fieldVal))
							sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), loadReg, toLLVMType(fieldType), gepReg))
						}
					} else if sb != nil {
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
					}
				} else if sb != nil {
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), fieldVal, toLLVMType(fieldType), gepReg))
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
					sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), dataBuf, vecBufSize))
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
						if g.isIntegerLLVMType(mapped) || g.isStructLLVMType(mapped) {
							elemType = mapped
						}
					}
				}
			}
			// Fallback: infer from actual elements if param type didn't resolve
			if elemType == "i64" && len(a.Elements) > 0 && g.isStringExpr(a.Elements[0]) {
				elemType = "%str-long"
			}
			n := int64(len(a.Elements))
			g.tmpIdx++
			vecName := fmt.Sprintf("%%callvec.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecName))
				// Initialize len/cap/data to 0 so empty slices (n==0) have valid
				// zero values instead of stack garbage (causes segfault when the
				// callee reads .len and gets a huge garbage value).
				g.tmpIdx++
				zeroLenGEP := fmt.Sprintf("%%callvec.zlen.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), zeroLenGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), zeroLenGEP))
				g.tmpIdx++
				zeroCapGEP := fmt.Sprintf("%%callvec.zcap.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), zeroCapGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), zeroCapGEP))
				g.tmpIdx++
				zeroDataGEP := fmt.Sprintf("%%callvec.zdata.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), zeroDataGEP, vecName))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), zeroDataGEP))
			}
			if n > 0 {
				g.tmpIdx++
				tmpArr := fmt.Sprintf("%%callvec.arr.%d", g.tmpIdx)
				arrType := fmt.Sprintf("[%d x %s]", n, toLLVMType(elemType))
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))
					for i, elem := range a.Elements {
						ev := g.generateExprWithSB(sb, elem)
						ev = g.stripLLVMType(ev)
						// Defensive fallback: if generateExprWithSB returned empty
						// (e.g. void function call), use 0 to avoid invalid IR
						// "store i64 , i64* ...".
						if ev == "" {
							ev = "0"
						}
						g.tmpIdx++
						gepReg := fmt.Sprintf("%%callvec.gep.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
							g.indent(), gepReg, arrType, arrType, tmpArr, i))
						storeVal := ev
						if g.isStructLLVMType(elemType) {
							// StringLiteral returns an alloca pointer; load the value.
							if strings.HasPrefix(ev, "%str-longlit") {
								g.tmpIdx++
								loadReg := fmt.Sprintf("%%callvec.load.%d", g.tmpIdx)
								sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(elemType), toLLVMType(elemType), ev))
								storeVal = loadReg
							}
						} else if g.isIntegerLLVMType(elemType) && elemType != "i64" && strings.HasPrefix(ev, "%") {
							g.tmpIdx++
							truncReg := fmt.Sprintf("%%callvec.trunc.%d", g.tmpIdx)
							sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, ev, toLLVMType(elemType)))
							storeVal = truncReg
						}
						sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(elemType), storeVal, toLLVMType(elemType), gepReg))
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
			// Cross-module string stride safety: detect %str-long* pointers from
			// builtins (fs.read-file, get-line, read-dir, etc.) via isStrPtrReg.
			// Without this, builtin results passed as function arguments fall
			// through to i64* handling, causing the callee to index the string
			// with stride=8 instead of stride=1 (reading garbage data).
			// Also track for stmt-level free to prevent memory leak when builtin
			// results are used directly as function arguments (not stored in vars).
			if strings.HasPrefix(ev, "%") && g.isStrPtrReg(ev) {
				g.trackStrTemporary(ev)
				return "%str-long* " + ev
			}
			// Cross-module string stride safety: detect %str-long SSA values
			// (from with-len, with-cap, etc.) via ssaTypes. Without this,
			// with-len results passed as function arguments fall through to
			// i64* handling, causing stride mismatch.
			// Track the temp alloca for stmt-level free to prevent memory leak.
			if strings.HasPrefix(ev, "%") && g.ssaTypes != nil {
				if ssaType, ok := g.ssaTypes[ev]; ok && ssaType == "%str-long" {
					g.tmpIdx++
					tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
					if sb != nil {
						sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpName))
						sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ev, tmpName))
					}
					g.trackStrTemporary(tmpName)
					return "%str-long* " + tmpName
				}
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
								if t, ok := g.varTypes[ident.Value]; ok && g.isStructLLVMType(t) {
									structName := strings.TrimPrefix(t, "%")
									if fields, ok := g.structTypes[structName]; ok {
										for _, f := range fields {
											if f.name == dot.Property {
												fieldType := f.typ
												if fieldType != "i64" {
													sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpName, toLLVMType(fieldType)))
													sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(fieldType), ev, toLLVMType(fieldType), tmpName))
													return toLLVMType(fieldType) + "* " + tmpName
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
	// Propagate parameter type to currentTargetType so that type-inferred
	// builtins (with-len, with-cap, with-cap-len) produce the correct LLVM
	// type (e.g. %str-long for str params, %vec for []T params).
	// Without this, with-len used as a direct function argument defaults to
	// %vec (stride=8) instead of %str-long (stride=1), causing cross-module
	// string stride corruption.
	savedTargetType := g.currentTargetType
	savedTargetElemType := g.currentTargetElemType
	typedArgs := make([]string, 0, len(nonVariadicArgs)+1)
	paramLLVMTypes := g.funcParamLLVMTypes[fnName]
	paramNolangTypes := g.funcParamTypes[fnName]
	for i, arg := range nonVariadicArgs {
		g.currentTargetType = ""
		g.currentTargetElemType = ""
		if i < len(paramLLVMTypes) {
			g.currentTargetType = paramLLVMTypes[i]
		}
		if i < len(paramNolangTypes) {
			nt := paramNolangTypes[i]
			if strings.HasPrefix(nt, "[]") {
				elemNolang := strings.TrimPrefix(nt, "[]")
				g.currentTargetElemType = g.mapToLLVMType(elemNolang)
			}
		}
		// For DotExpression-based calls (e.g. os.read-file), funcParamLLVMTypes
		// is keyed by the full name (e.g. "fs.read-file"). Try the full name
		// if the short name didn't match.
		if i >= len(paramLLVMTypes) && g.funcParamLLVMTypes != nil {
			if pts, ok := g.funcParamLLVMTypes[fnName]; ok && i < len(pts) {
				g.currentTargetType = pts[i]
			}
		}
		typedArgs = append(typedArgs, genTypedArg(arg, i))
	}
	g.currentTargetType = savedTargetType
	g.currentTargetElemType = savedTargetElemType

	// Static method call fix: when a user-defined type method (Type.method())
	// is called without an instance receiver, the function definition still
	// includes self as the first parameter (from parseMethodDefinition), but
	// methodReceiver is nil and self is not prepended. Detect this mismatch
	// by comparing funcParamCount (includes self) with the number of provided
	// args. If exactly 1 arg is missing (self), add a dummy null self pointer.
	if methodReceiver == nil && g.funcParamCount != nil && sb != nil && !isVariadic {
		if pc, ok := g.funcParamCount[fnName]; ok && pc == len(nonVariadicArgs)+1 {
			g.tmpIdx++
			dummySelf := fmt.Sprintf("%%dummy.self.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), dummySelf))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), dummySelf))
			typedArgs = append([]string{"i64* " + dummySelf}, typedArgs...)
		}
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
			arrType := fmt.Sprintf("[%d x %s]", n, toLLVMType(elemType))
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
					sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), toLLVMType(elemType), ev, toLLVMType(elemType), gepReg))
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
				sb.WriteString(fmt.Sprintf("%s%s = bitcast [%d x %s]* %s to i8*\n", g.indent(), dataCast, n, toLLVMType(elemType), arrName))
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
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), voidSingleTmp, toLLVMType(voidSingleOutputType)))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), g.llvmTypeSize(voidSingleOutputType), voidSingleTmp))
			if voidSingleOutputType == "%arr" {
				// 固定數組 [N]T：調用方分配 N*elemSize 空間並設置 len/data。
				// 固定數組大小已知，不同於可變切片，需預分配空間供 out[i] = val 寫入。
				// %arr 類型（如 [8]u8）需初始化 len 與 data 指標，否則方法體 out[i] = val 會崩潰。
				// 緩衝區由調用方負責分配，被調用函數 prologue 不再預分配。
				arrSize := int64(0)
				elemSize := int64(1)
				if g.funcResultNolangTypes != nil {
					if nolangRets, ok := g.funcResultNolangTypes[fnName]; ok && len(nolangRets) == 1 {
						nt := nolangRets[0]
						if strings.HasPrefix(nt, "[") {
							if rb := strings.Index(nt, "]"); rb > 0 {
								sizeStr := nt[1:rb]
								if v, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
									arrSize = v
								}
								elemSize = llvmTypeSize(g.mapToLLVMType(nt[rb+1:]))
							}
						}
					}
				}
				totalSize := arrSize * elemSize
				g.tmpIdx++
				arrDataBuf := fmt.Sprintf("%%vso.arrdata.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), arrDataBuf, totalSize))
				g.tmpIdx++
				arrLenGEP := fmt.Sprintf("%%vso.arrlen.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 0\n", g.indent(), arrLenGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arrSize, arrLenGEP))
				g.tmpIdx++
				arrDataGEP := fmt.Sprintf("%%vso.arrdata.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), arrDataGEP, voidSingleTmp))
				g.storeDataPtrField(sb, arrDataBuf, arrDataGEP)
			} else {
				// 其他類型（i64/[]i64/str/?T/struct）零初始化：
				//   i64 → 0
				//   []i64 (%vec) → 空容器 {len=0, cap=0, data=null}
				//   str (%str-long) → 空容器 {len=0, cap=0, data=null}
				//   ?T (%option) → nil
				//   struct → {} 零值
				// 函數內需自行用 with-len/with-cap/[] 初始化後才能 out[i] = val
				sb.WriteString(fmt.Sprintf("%sstore %s zeroinitializer, %s* %s\n", g.indent(), toLLVMType(voidSingleOutputType), toLLVMType(voidSingleOutputType), voidSingleTmp))
			}
		}
		typedArgs = append(typedArgs, toLLVMType(voidSingleOutputType)+"* "+voidSingleTmp)
		// Propagate element type from the function's Nolang return type (e.g. []str → %str-long)
		// so that generateLet's %vec store path can set arrayElemTypes[name] correctly.
		// Without this, parts = 'a-b-c'.split('-') would store a %vec but leave
		// arrayElemTypes["parts"] unset, causing parts[0] to be read as i64 instead of %str-long.
		g.lastVoidSingleOutputElemType = ""
		if voidSingleOutputType == "%vec" && g.funcResultNolangTypes != nil {
			if nolangRets, ok := g.funcResultNolangTypes[fnName]; ok && len(nolangRets) == 1 {
				nt := nolangRets[0]
				if strings.HasPrefix(nt, "[]") {
					elemNolang := nt[2:]
					g.lastVoidSingleOutputElemType = g.mapToLLVMType(elemNolang)
				}
			}
		}
	}

	// Check if this is a mangled push method call (e.g., @_LB__RB_byte.push)
	// If so, handle it specially to avoid undefined function errors.
	mangledName := strings.TrimPrefix(llvmFnName, "@")
	if strings.HasPrefix(mangledName, "_LB__RB_") && strings.HasSuffix(mangledName, ".push)") {
		// This is a mangled push call - let it fall through to normal call generation
		// The actual function should be generated by the type system
	}

	// Append output parameter pointer to typedArgs when hasOutputParam is true.
	// The function signature includes the output param as the last LLVM parameter,
	// so we must pass it even in statement form (e.g., sm.get-session(0, s)).
	if hasOutputParam && outputArg != nil {
		if ident, ok := outputArg.(*parser.Identifier); ok {
			if ts, ok := g.funcResultLLVMType[fnName]; ok && len(ts) == 1 {
				typedArgs = append(typedArgs, toLLVMType(ts[0])+"* "+g.varAddr(ident.Value))
			}
		}
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
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, toLLVMType(voidSingleOutputType), toLLVMType(voidSingleOutputType), voidSingleTmp))
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

// byteArrDataPtr extracts the i8* data pointer from a []byte / [N]byte array argument.
// For %arr: load field 1 (data pointer).
// For %vec: load field 2 (data pointer).
// For %str-long: load field 2 (data pointer).
// For slice views: use the view's dataPtrReg directly.
func (g *Generator) byteArrDataPtr(sb *strings.Builder, arg parser.Expression) string {
	ident, ok := arg.(*parser.Identifier)
	if !ok {
		// Fallback: evaluate the expression and try to use it as i8*
		return g.generateExprWithSB(sb, arg)
	}
	varName := ident.Value
	// Slice view: use the pre-computed data pointer
	if g.isSliceViewVar(varName) {
		view := g.sliceViews[varName]
		return view.dataPtrReg
	}
	if g.varTypes != nil {
		if t, ok := g.varTypes[varName]; ok {
			arrRef := g.varAddr(varName)
			if g.globalVars != nil && g.globalVars[varName] && !(g.funcLocalNames != nil && g.funcLocalNames[varName]) {
				arrRef = llvmGlobalRef(varName)
			}
			switch t {
			case "%arr":
				// %arr = { i64, i8* } — field 1 is data pointer
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%bp.arr.gep.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n",
						g.indent(), dataGEP, arrRef))
				}
				return g.loadDataPtrField(sb, dataGEP)
			case "%vec":
				// %vec = { i64, i64, i8* } — field 2 is data pointer
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%bp.vec.gep.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
						g.indent(), dataGEP, arrRef))
				}
				return g.loadDataPtrField(sb, dataGEP)
			case "%str-long":
				return g.extractStrDataPtr(sb, arrRef)
			}
		}
	}
	// Fallback
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
			if t, ok := g.varTypes[ident.Value]; ok && (t == "i64" || t == "i32" || t == "i16" || t == "i8" || t == "i1" ||
				t == "u64" || t == "u32" || t == "u16" || t == "u8" || t == "u128") {
				g.tmpIdx++
				loadReg := fmt.Sprintf("%%fwd.i64.%d", g.tmpIdx)
				llvmType := toLLVMType(t)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, llvmType, llvmType, g.varAddr(ident.Value)))
				}
				if llvmType != "i64" {
					g.tmpIdx++
					extReg := fmt.Sprintf("%%fwd.i64.ext.%d", g.tmpIdx)
					if sb != nil {
						// Unsigned types use zext, signed types use sext
						extOp := "zext"
						if !isUnsignedIntType(t) && llvmType != "i1" {
							extOp = "sext"
						}
						sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to i64\n", g.indent(), extReg, extOp, llvmType, loadReg))
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

// inferFieldElemType infers the element type of a []T slice from a DotExpression
// receiver (e.g. self.out where out is []byte). It looks up the struct type
// and field definition to find the element type.
// Returns the Nolang type name (e.g. "byte", "i64") or "" if not found.
func (g *Generator) inferFieldElemType(dot *parser.DotExpression) string {
	if dot == nil {
		return ""
	}
	// Try to get the struct name from the receiver
	recvName := ""
	if ident, ok := dot.Receiver.(*parser.Identifier); ok {
		recvName = ident.Value
	}
	if recvName == "" {
		return ""
	}
	// "self" is the implicit receiver in method bodies
	structName := ""
	if t, ok := g.varTypes[recvName]; ok {
		structName = strings.TrimPrefix(t, "%")
	}
	if structName == "" {
		return ""
	}
	// Look up the field in the struct definition
	fields, ok := g.structTypes[structName]
	if !ok {
		return ""
	}
	for _, f := range fields {
		if f.name == dot.Property && f.typ == "%vec" {
			// The field is a vec ([]T). Return the element type from struct field info.
			if f.elemType != "" {
				return f.elemType
			}
			// Fallback: try arrayElemTypes with the field key "self.field" or "recvName.field"
			fieldKey := recvName + "." + dot.Property
			if g.arrayElemTypes != nil {
				if et, ok := g.arrayElemTypes[fieldKey]; ok {
					return et
				}
			}
			return ""
		}
	}
	return ""
}

// inferFieldElemElemType looks up the inner element type (elemElemType) for
// nested container fields like [][]str (where elemType="%vec" and elemElemType="%str-long")
// from the struct field definition in structTypes.
// Returns the LLVM type string (e.g. "%str-long", "i64") or "" if not found.
func (g *Generator) inferFieldElemElemType(dot *parser.DotExpression) string {
	if dot == nil {
		return ""
	}
	// Try to get the struct name from the receiver
	recvName := ""
	if ident, ok := dot.Receiver.(*parser.Identifier); ok {
		recvName = ident.Value
	}
	if recvName == "" {
		return ""
	}
	structName := ""
	if t, ok := g.varTypes[recvName]; ok {
		structName = strings.TrimPrefix(t, "%")
	}
	if structName == "" {
		return ""
	}
	fields, ok := g.structTypes[structName]
	if !ok {
		return ""
	}
	for _, f := range fields {
		if f.name == dot.Property && (f.typ == "%vec" || f.typ == "%arr") {
			return f.elemElemType
		}
	}
	return ""
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
		// Register the SSA type as i64 so that downstream type coercion
		// (generateLet's int→int path) knows the value is already i64,
		// not i1. Without this, generateLet sees llvmType="i1" (from the
		// bool return type) and generates a spurious `zext i1 %eqzext to i64`
		// on an already-i64 value, causing an LLVM type mismatch error.
		if g.ssaTypes != nil {
			g.ssaTypes[zextReg] = "i64"
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
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 6)\n", g.indent(), bufReg))
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
		var recvAddr string
		if ident, ok := args[0].(*parser.Identifier); ok {
			recvAddr = g.varAddr(ident.Value)
		} else if dot, ok := args[0].(*parser.DotExpression); ok {
			// Struct field receiver (e.g. self.data): generate the field address.
			// Bug 07 follow-up: without this, .data.clear() inside a struct method
			// was a silent no-op (only Identifier receivers were supported).
			recvAddr = g.generateExprPtr(sb, dot)
			if recvAddr == "" {
				return ""
			}
		} else {
			return ""
		}
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
		// Handle both Identifier and DotExpression receivers.
		// The transpiler rewrites .field.push(val) into []T.push(self.field, val),
		// so args[0] can be a DotExpression (e.g. self.out).
		var recvAddr string
		var recvName string
		if ident, ok := args[0].(*parser.Identifier); ok {
			recvName = ident.Value
			recvAddr = g.varAddr(recvName)
		} else if dot, ok := args[0].(*parser.DotExpression); ok {
			// For struct field receivers (e.g. self.out), generate the field address
			recvAddr = g.generateExprPtr(sb, dot)
			if os.Getenv("NOLANG_DEBUG_PUSH") != "" {
				fmt.Fprintf(os.Stderr, "[debug-push] DotExpression receiver, recvAddr=%q, receiver=%v\n", recvAddr, dot)
			}
			if recvAddr == "" {
				return ""
			}
			// Try to get the receiver name for output param tracking
			if innerIdent, ok := dot.Receiver.(*parser.Identifier); ok {
				recvName = innerIdent.Value + "." + dot.Property
			}
		} else {
			return ""
		}

		// If the receiver is an output parameter, mark it as initialized.
		// Without this, emitRetInitZeroFill at function end would overwrite
		// the pushed data with zeroinitializer (clobbering the result).
		if recvName != "" && g.outputParamNames != nil && g.outputParamNames[recvName] {
			g.emitSetRetInitBit(sb, recvName)
		}

		// Option receiver unwrap: when the receiver is an %option variable whose
		// inner type is %vec (e.g. v = m.get(...) returns ?[]str), we need to
		// load the heap-allocated %vec pointer from the option's data field.
		// Without this, the push code would operate on the option alloca directly
		// (treating %option* as %vec*), causing type mismatches and corrupt data.
		if ident, ok := args[0].(*parser.Identifier); ok {
			if vt, ok := g.varTypes[ident.Value]; ok && vt == "%option" {
				innerType := "i64"
				if g.optionInnerTypes != nil {
					if it, ok := g.optionInnerTypes[ident.Value]; ok && it != "" {
						innerType = it
					}
				}
				if g.isStructLLVMType(innerType) {
					// Load the struct pointer from the option's data field
					g.tmpIdx++
					optDataGEP := fmt.Sprintf("%%vp.optdata.gep.%d", g.tmpIdx)
					g.tmpIdx++
					optDataLoad := fmt.Sprintf("%%vp.optdata.val.%d", g.tmpIdx)
					g.tmpIdx++
					optDataPtr := fmt.Sprintf("%%vp.optdata.ptr.%d", g.tmpIdx)
					sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %s, i32 0, i32 1\n", g.indent(), optDataGEP, recvAddr))
					sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), optDataLoad, optDataGEP))
					sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to %s*\n", g.indent(), optDataPtr, optDataLoad, innerType))
					recvAddr = optDataPtr
				}
			}
		}

		// Get element type and size
		elemType := "i64"
		if g.arrayElemTypes != nil && recvName != "" {
			if et, ok := g.arrayElemTypes[recvName]; ok {
				elemType = toLLVMType(et)
			}
		}
		// For DotExpression receivers, try to infer element type from struct field
		if dot, ok := args[0].(*parser.DotExpression); ok {
			if et := g.inferFieldElemType(dot); et != "" {
				elemType = toLLVMType(et)
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
			if valType != "" && toLLVMType(valType) != toLLVMType(elemType) && g.isIntegerLLVMType(valType) && g.isIntegerLLVMType(elemType) {
				g.tmpIdx++
				convReg := fmt.Sprintf("%%vp.conv.%d", g.tmpIdx)
				if sb != nil {
					if valType == "i64" {
						sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), convReg, val, toLLVMType(elemType)))
					} else if elemType == "i64" {
						op := widenExtOp(valType)
						sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to i64\n", g.indent(), convReg, op, toLLVMType(valType), val))
					} else {
						sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), convReg, toLLVMType(valType), val, toLLVMType(elemType)))
					}
				}
				storeVal = convReg
			}
		}

		// For struct element types (e.g. %str-long, %vec, user structs), ForwardFunc
		// builtin call results (like arg(i)) are returned as *pointers* to the struct,
		// while identifiers, index expressions, and regular method calls return struct
		// *values* (already loaded). Load the struct value only for ForwardFunc calls.
		if g.isStructLLVMType(elemType) {
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
			// CFG: record the block before the conditional branch
			vpFromBlock := g.cfgBlockLabel()
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), spaceCmp, curLen, curCap))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), spaceCmp, fastLabel, expandLabel))
			// CFG: conditional branch → two successors
			g.cfgTerm(vpFromBlock, termCondBr)
			g.cfgEdge(vpFromBlock, fastLabel)
			g.cfgEdge(vpFromBlock, expandLabel)
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
			// Store val: for heap-owning types, deep-clone to give vec its own independent copy
			// (avoids double-free when both source var and vec element are freed at exit)
			if g.isHeapOwningType(elemType) {
				g.tmpIdx++
				cloneSrc := fmt.Sprintf("%%vp.csrc.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), cloneSrc, elemType))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, cloneSrc))
			// 嵌套容器（[][]T）：emitDeepClone 的 elemType 參數是容器的元素型別。
			// 對於 %vec 元素，其元素型別由 elemElemTypes[recvName] 決定。
			// 對於 struct field 接收者（如 m1.rows），elemElemTypes 不含 "m1.rows" 鍵，
			// 需從 structTypes 的欄位定義中推導（inferFieldElemElemType）。
			// elemElemType 參數僅用於三層嵌套（如 [][][]T），此處為空。
			innerElemType := ""
			if elemType == "%vec" || elemType == "%arr" {
				if g.elemElemTypes != nil && recvName != "" {
					innerElemType = g.elemElemTypes[recvName]
				}
				// Fallback for DotExpression receivers: look up from struct field definition
				if innerElemType == "" {
					if dot, ok := args[0].(*parser.DotExpression); ok {
						innerElemType = g.inferFieldElemElemType(dot)
					}
				}
			}
			g.emitDeepClone(sb, cloneSrc, elemGEP, elemType, innerElemType)
			} else {
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, elemGEP))
			}
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
			// CFG: fastLabel → endLabel (unconditional branch)
			g.cfgTerm(fastLabel, termBr)
			g.cfgEdge(fastLabel, endLabel)
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
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), newBuf, totalBytes))

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
			// Deep-clone heap-owning types to give vec its own independent copy
			if g.isHeapOwningType(elemType) {
				g.tmpIdx++
				cloneSrc2 := fmt.Sprintf("%%vp.csrc2.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), cloneSrc2, elemType))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, cloneSrc2))
			// 嵌套容器（[][]T）：emitDeepClone 的 elemType 參數是容器的元素型別。
			innerElemType2 := ""
			if elemType == "%vec" || elemType == "%arr" {
				if g.elemElemTypes != nil && recvName != "" {
					innerElemType2 = g.elemElemTypes[recvName]
				}
				// Fallback for DotExpression receivers: look up from struct field definition
				if innerElemType2 == "" {
					if dot, ok := args[0].(*parser.DotExpression); ok {
						innerElemType2 = g.inferFieldElemElemType(dot)
					}
				}
			}
			g.emitDeepClone(sb, cloneSrc2, newElemGEP, elemType, innerElemType2)
			} else {
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, storeVal, elemType, newElemGEP))
			}

			// Update cap (field 1) = newCap
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%vp.cap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), newCap, capGEP))

			// Free old data buffer before overwriting the data field.
			// Without this, every vec.push grow leaks the previous buffer
			// (cap 0→4→8→16→... each leaks the prior allocation).
			// oldData is already an i8* register from loadDataPtrField above.
			// emitNullCheckFree guards free(NULL) anyway (no-op when cap was 0).
			g.emitNullCheckFree(sb, oldData)

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
			// CFG: expandLabel → endLabel (unconditional branch)
			g.cfgTerm(expandLabel, termBr)
			g.cfgEdge(expandLabel, endLabel)
		}

		// End label
		if sb != nil {
			g.emitLabel(sb, endLabel)
		}
		// Note: heap-owning elements are deep-cloned during push (fast/expand paths),
		// so no movedVars marking is needed — source and vec have independent copies.
		return ""

	case "str-clear":
		// str.clear() — set len=0 in-place, no storage switch
		// str-long: store i64 0 to field 0, cap/ptr unchanged
		if len(args) < 1 {
			return ""
		}
		var recvAddr string
		if ident, ok := args[0].(*parser.Identifier); ok {
			recvAddr = g.varAddr(ident.Value)
		} else if dot, ok := args[0].(*parser.DotExpression); ok {
			// Struct field receiver (e.g. self.name): generate the field address.
			// Bug 07 follow-up: without this, .name.clear() was a silent no-op.
			recvAddr = g.generateExprPtr(sb, dot)
			if recvAddr == "" {
				return ""
			}
		} else {
			return ""
		}
		// str-long: field 0 is i64 len
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%sc.len.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
		}
		return ""

	case "str-truncate":
		// str.truncate(n) — set len = max(0, min(len, n)) in-place, cap/ptr unchanged
		// str-long: field 0 is i64 len
		if len(args) < 2 {
			return ""
		}
		var recvAddr string
		if ident, ok := args[0].(*parser.Identifier); ok {
			recvAddr = g.varAddr(ident.Value)
		} else if dot, ok := args[0].(*parser.DotExpression); ok {
			// Struct field receiver (e.g. self.name): generate the field address.
			// Bug 07 follow-up: without this, .name.truncate(n) was a silent
			// no-op — or worse, got mis-dispatched to the POSIX truncate CLibCall.
			recvAddr = g.generateExprPtr(sb, dot)
			if recvAddr == "" {
				return ""
			}
		} else {
			return ""
		}
		nVal := g.evalI64Arg(sb, args[1])
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%st.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		curLen := fmt.Sprintf("%%st.cur-len.%d", g.tmpIdx)
		g.tmpIdx++
		negCmp := fmt.Sprintf("%%st.neg-cmp.%d", g.tmpIdx)
		g.tmpIdx++
		clampedN := fmt.Sprintf("%%st.clamped-n.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%st.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		finalLen := fmt.Sprintf("%%st.final-len.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
			// Clamp n to [0, ∞): if n < 0, use 0
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, 0\n", g.indent(), negCmp, nVal))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 0, i64 %s\n", g.indent(), clampedN, negCmp, nVal))
			// len = min(clampedN, curLen)
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, clampedN, curLen))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, clampedN, curLen))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
		}
		return ""

	case "vec-truncate":
		// vec.truncate(n) — set len = max(0, min(len, n)) in-place, cap/ptr unchanged
		// vec: field 0 is i64 len (same layout as str-long)
		if len(args) < 2 {
			return ""
		}
		var recvAddr string
		if ident, ok := args[0].(*parser.Identifier); ok {
			recvAddr = g.varAddr(ident.Value)
		} else if dot, ok := args[0].(*parser.DotExpression); ok {
			// Struct field receiver (e.g. self.data): generate the field address.
			// Bug 07 follow-up: without this, .data.truncate(n) was a silent
			// no-op — or worse, got mis-dispatched to the POSIX truncate CLibCall.
			recvAddr = g.generateExprPtr(sb, dot)
			if recvAddr == "" {
				return ""
			}
		} else {
			return ""
		}
		nVal := g.evalI64Arg(sb, args[1])
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%vt.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		curLen := fmt.Sprintf("%%vt.cur-len.%d", g.tmpIdx)
		g.tmpIdx++
		negCmp := fmt.Sprintf("%%vt.neg-cmp.%d", g.tmpIdx)
		g.tmpIdx++
		clampedN := fmt.Sprintf("%%vt.clamped-n.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%vt.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		finalLen := fmt.Sprintf("%%vt.final-len.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, recvAddr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), curLen, lenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, 0\n", g.indent(), negCmp, nVal))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 0, i64 %s\n", g.indent(), clampedN, negCmp, nVal))
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, clampedN, curLen))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), finalLen, cmpReg, clampedN, curLen))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), finalLen, lenGEP))
		}
		return ""

	case "with-cap":
		// with-cap(cap) — builtin syntax, type inferred from assignment LHS
		//   s str = with-cap(256)   → %str-long { len=0, cap=256, data=malloc(256) }
		//   v []i64 = with-cap(100) → %vec { len=0, cap=100, data=malloc(100*8) }
		// Method form (v.with-cap(100)): receiver is args[0], cap is args[1].
		argIdx := 0
		if len(args) > 1 {
			argIdx = 1 // skip receiver
		}
		if len(args) < argIdx+1 {
			return ""
		}
		capVal := g.evalI64Arg(sb, args[argIdx])
		targetType := g.currentTargetType
		switch targetType {
		case "%str-long", "str":
			// malloc(cap) — stride-1 (each element is 1 byte, i8)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wc.sbuf.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, capVal))
				// Zero-initialize the buffer to prevent garbage data.
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), bufReg, capVal))
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
			if g.currentTargetElemType != "" {
				if s := g.llvmTypeSize(g.currentTargetElemType); s > 0 {
					elemSize = s
				}
			}
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
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, bytesReg))
				// Zero the element array so element-assignment's "free old value" path
				// loads defined len=0/data=null (not undef) and never calls free(undef).
				// (load undef -> icmp -> free(undef) is UB that SCCP deletes the whole fn.)
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), bufReg, bytesReg))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec zeroinitializer, i64 0, 0\n", g.indent(), v1))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 1\n", g.indent(), v2, v1, capVal))
				_p2i_v3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 2\n", g.indent(), v3, v2, _p2i_v3))
			}
			g.ssaTypes[v3] = "%vec"
			return v3
		}

	case "with-len":
		// with-len(len) — builtin syntax, type inferred from assignment LHS
		//   v []i64 = with-len(100) → %vec { len=100, cap=100, data=malloc(100*8) }
		// Like with-cap, but also sets len=cap so that direct index reads/writes
		// pass bounds checks without needing push() to grow the length.
		// Method form (v.with-len(100)): receiver is args[0], len is args[1].
		argIdx := 0
		if len(args) > 1 {
			argIdx = 1 // skip receiver
		}
		if len(args) < argIdx+1 {
			return ""
		}
		lenVal := g.evalI64Arg(sb, args[argIdx])
		targetType := g.currentTargetType
		switch targetType {
		case "%str-long", "str":
			// malloc(len) — stride-1 (each element is 1 byte, i8)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wl.sbuf.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, lenVal))
				// Zero-initialize the buffer to prevent garbage data when
				// the string is returned cross-module before all positions
				// are explicitly written. Matches with-cap/vec behavior.
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), bufReg, lenVal))
			}
			// Build %str-long { len=len, cap=len, data=buf }
			g.tmpIdx++
			s1 := fmt.Sprintf("%%wl.s1.%d", g.tmpIdx)
			g.tmpIdx++
			s2 := fmt.Sprintf("%%wl.s2.%d", g.tmpIdx)
			g.tmpIdx++
			s3 := fmt.Sprintf("%%wl.s3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), s1, lenVal))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), s2, s1, lenVal))
				_p2i_s3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), s3, s2, _p2i_s3))
			}
			g.ssaTypes[s3] = "%str-long"
			return s3

		default: // %vec, []i64, etc.
			elemSize := int64(8) // default i64
			if g.currentTargetElemType != "" {
				if s := g.llvmTypeSize(g.currentTargetElemType); s > 0 {
					elemSize = s
				}
			}
			g.tmpIdx++
			bytesReg := fmt.Sprintf("%%wl.vbytes.%d", g.tmpIdx)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wl.vbuf.%d", g.tmpIdx)
			g.tmpIdx++
			v1 := fmt.Sprintf("%%wl.v1.%d", g.tmpIdx)
			g.tmpIdx++
			v2 := fmt.Sprintf("%%wl.v2.%d", g.tmpIdx)
			g.tmpIdx++
			v3 := fmt.Sprintf("%%wl.v3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), bytesReg, lenVal, elemSize))
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, bytesReg))
				// Zero the element array so element-assignment's "free old value" path
				// loads defined len=0/data=null (not undef) and never calls free(undef).
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), bufReg, bytesReg))
				// len=len, cap=len (both set to the argument value)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec zeroinitializer, i64 %s, 0\n", g.indent(), v1, lenVal))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 1\n", g.indent(), v2, v1, lenVal))
				_p2i_v3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 2\n", g.indent(), v3, v2, _p2i_v3))
			}
			g.ssaTypes[v3] = "%vec"
			return v3
		}

	case "with-cap-len":
		// with-cap-len(cap, len) — builtin syntax, type inferred from assignment LHS
		//   v []i64 = with-cap-len(200, 100) → %vec { len=100, cap=200, data=malloc(200*8) }
		// Combines with-cap and with-len: allocates cap elements, sets len to the
		// given length (len <= cap), enabling direct index access within [0,len)
		// while reserving extra capacity for future growth.
		// Method form (v.with-len-cap(len, cap)): receiver is args[0].
		argIdx := 0
		if len(args) > 2 {
			argIdx = 1 // skip receiver
		}
		if len(args) < argIdx+2 {
			return ""
		}
		capVal := g.evalI64Arg(sb, args[argIdx])
		lenVal := g.evalI64Arg(sb, args[argIdx+1])
		targetType := g.currentTargetType
		switch targetType {
		case "%str-long", "str":
			// malloc(cap) — stride-1 (each element is 1 byte, i8)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wcl.sbuf.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, capVal))
				// Zero-initialize the buffer to prevent garbage data.
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), bufReg, capVal))
			}
			// Build %str-long { len=len, cap=cap, data=buf }
			g.tmpIdx++
			s1 := fmt.Sprintf("%%wcl.s1.%d", g.tmpIdx)
			g.tmpIdx++
			s2 := fmt.Sprintf("%%wcl.s2.%d", g.tmpIdx)
			g.tmpIdx++
			s3 := fmt.Sprintf("%%wcl.s3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), s1, lenVal))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), s2, s1, capVal))
				_p2i_s3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), s3, s2, _p2i_s3))
			}
			g.ssaTypes[s3] = "%str-long"
			return s3

		default: // %vec, []i64, etc.
			elemSize := int64(8) // default i64
			if g.currentTargetElemType != "" {
				if s := g.llvmTypeSize(g.currentTargetElemType); s > 0 {
					elemSize = s
				}
			}
			g.tmpIdx++
			bytesReg := fmt.Sprintf("%%wcl.vbytes.%d", g.tmpIdx)
			g.tmpIdx++
			bufReg := fmt.Sprintf("%%wcl.vbuf.%d", g.tmpIdx)
			g.tmpIdx++
			v1 := fmt.Sprintf("%%wcl.v1.%d", g.tmpIdx)
			g.tmpIdx++
			v2 := fmt.Sprintf("%%wcl.v2.%d", g.tmpIdx)
			g.tmpIdx++
			v3 := fmt.Sprintf("%%wcl.v3.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, %d\n", g.indent(), bytesReg, capVal, elemSize))
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, bytesReg))
				// Zero the element array so element-assignment's "free old value" path
				// loads defined len=0/data=null (not undef) and never calls free(undef).
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", g.indent(), bufReg, bytesReg))
				// len=len, cap=cap (independent values)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec zeroinitializer, i64 %s, 0\n", g.indent(), v1, lenVal))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 1\n", g.indent(), v2, v1, capVal))
				_p2i_v3 := g.ptrToIntVal(sb, bufReg)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%vec %s, i64 %s, 2\n", g.indent(), v3, v2, _p2i_v3))
			}
			g.ssaTypes[v3] = "%vec"
			return v3
		}

	case "get-errno":
		// get-errno() — get the last errno value from C library
		// macOS: @__error(), Linux/WASI: @__errno_location(), Windows: @_errno()
		// 使用編譯目標平台（g.targetGoos）而非宿主平台（runtime.GOOS），
		// 與 decl.go 的 errno 宣告分派保持一致。
		goos := g.targetGoos
		if goos == "" {
			goos = runtime.GOOS
		}
		g.tmpIdx++
		errnoPtrReg := fmt.Sprintf("%%errno.ptr.%d", g.tmpIdx)
		g.tmpIdx++
		errnoReg := fmt.Sprintf("%%errno.val.%d", g.tmpIdx)
		if sb != nil {
			// Get errno pointer
			if goos == "darwin" {
				sb.WriteString(fmt.Sprintf("%s%s = call i32* @__error()\n", g.indent(), errnoPtrReg))
			} else if goos == "windows" {
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

	case "rotate-left", "rotate-right":
		// rotate-left(x, n): llvm.fshl(x, x, n) → (x << n) | (x >> (bits-n))
		// rotate-right(x, n): llvm.fshr(x, x, n) → (x >> n) | (x << (bits-n))
		// LLVM lowers these to ARM64 ROR / x86 ROL/ROR instructions.
		if len(args) < 2 {
			return "0"
		}
		xVal := g.evalI64Arg(sb, args[0])
		nVal := g.evalI64Arg(sb, args[1])
		// Determine the integer width from the first argument's type
		argType := g.intExprLLVMType(args[0])
		if argType == "" {
			// intExprLLVMType intentionally does not handle IndexExpression
			// (because generateIndexExpression zexts to i64). For rotate-left
			// on u32 array elements (e.g. rotate-left(w[i-15], 25) where w is
			// []u32), look up the element type directly from arrayElemTypes so
			// the correct llvm.fshl.i32 intrinsic is selected.
			if idx, ok := args[0].(*parser.IndexExpression); ok {
				if ident, ok := idx.Left.(*parser.Identifier); ok && g.arrayElemTypes != nil {
					if et, ok := g.arrayElemTypes[ident.Value]; ok {
						switch toLLVMType(et) {
						case "i32":
							argType = "i32"
						case "i16":
							argType = "i16"
						}
					}
				}
			}
			if argType == "" {
				argType = "i64"
			}
		}
		intrinsic := "llvm.fshl.i64"
		retType := "i64"
		if toLLVMType(argType) == "i32" {
			intrinsic = "llvm.fshl.i32"
			retType = "i32"
			// For i32 operands, truncate x and n to i32
			if strings.HasPrefix(xVal, "%") {
				g.tmpIdx++
				truncX := fmt.Sprintf("%%rotl.truncx.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), truncX, xVal))
				}
				xVal = truncX
			} else {
				xVal = fmt.Sprintf("i32 %s", xVal)
			}
			if strings.HasPrefix(nVal, "%") {
				g.tmpIdx++
				truncN := fmt.Sprintf("%%rotl.truncn.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), truncN, nVal))
				}
				nVal = truncN
			} else {
				nVal = fmt.Sprintf("i32 %s", nVal)
			}
			if forwardFunc == "rotate-right" {
				intrinsic = "llvm.fshr.i32"
			}
		} else {
			// i64 path
			if !strings.HasPrefix(xVal, "%") {
				xVal = fmt.Sprintf("i64 %s", xVal)
			}
			if !strings.HasPrefix(nVal, "%") {
				nVal = fmt.Sprintf("i64 %s", nVal)
			}
			if forwardFunc == "rotate-right" {
				intrinsic = "llvm.fshr.i64"
			}
		}
		g.tmpIdx++
		rotReg := fmt.Sprintf("%%rotl.res.%d", g.tmpIdx)
		if sb != nil {
			// llvm.fshl(retType, retType, retType) — pass x twice (concatenated funnel)
			xArg := xVal
			if !strings.HasPrefix(xVal, retType+" ") && !strings.HasPrefix(xVal, "%") {
				xArg = retType + " " + xVal
			}
			if strings.HasPrefix(xVal, "%") {
				xArg = retType + " " + xVal
			}
			nArg := nVal
			if !strings.HasPrefix(nVal, retType+" ") && !strings.HasPrefix(nVal, "%") {
				nArg = retType + " " + nVal
			}
			if strings.HasPrefix(nVal, "%") {
				nArg = retType + " " + nVal
			}
			sb.WriteString(fmt.Sprintf("%s%s = call %s @%s(%s, %s, %s)\n",
				g.indent(), rotReg, retType, intrinsic, xArg, xArg, nArg))
		}
		// For i32 result, zext to i64 for Nolang's i64-based value flow
		if retType == "i32" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%rotl.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), zextReg, rotReg))
			}
			return zextReg
		}
		return rotReg

	case "load-le-u16", "load-le-u32", "load-le-u64":
		// load-le-uXX(arr, offset): load little-endian uXX from byte array data.
		// Replaces: buf[off] | (buf[off+1] << 8) | ... → single load instruction.
		// Args: arr ([]byte or [N]byte), offset (i64)
		if len(args) < 2 {
			return "0"
		}
		// Get the data pointer from the array/slice argument
		dataPtr := g.byteArrDataPtr(sb, args[0])
		offsetVal := g.evalI64Arg(sb, args[1])
		// Determine the load type
		loadType := "i64"
		switch forwardFunc {
		case "load-le-u16":
			loadType = "i16"
		case "load-le-u32":
			loadType = "i32"
		case "load-le-u64":
			loadType = "i64"
		}
		// GEP to offset: getelementptr i8, i8* dataPtr, i64 offset
		g.tmpIdx++
		gepReg := fmt.Sprintf("%%leload.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
				g.indent(), gepReg, dataPtr, offsetVal))
		}
		// Bitcast to loadType*
		g.tmpIdx++
		typedPtr := fmt.Sprintf("%%leload.typed.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), typedPtr, gepReg, loadType))
		}
		// Load the value
		g.tmpIdx++
		loadReg := fmt.Sprintf("%%leload.val.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n",
				g.indent(), loadReg, loadType, loadType, typedPtr))
		}
		// ZExt to i64 for Nolang's value flow
		if loadType != "i64" {
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%leload.zext.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n",
					g.indent(), zextReg, loadType, loadReg))
			}
			return zextReg
		}
		return loadReg

	case "store-le-u32":
		// store-le-u32(arr, offset, value): store u32 value to byte array at offset in little-endian.
		// Replaces: buf[off]=v&255; buf[off+1]=(v>>8)&255; ... → single store i32.
		if len(args) < 3 {
			return "0"
		}
		// Get the data pointer - handle both %arr struct and raw [N x i8]* output params
		var dataPtr string
		var storeOutParam string // out 參數名（寫入後需標記 __ret_init_bitmap，否則返回時被零填充覆蓋）
		if ident, ok := args[0].(*parser.Identifier); ok {
			varName := ident.Value
			// Check if this is an output parameter with raw array type (e.g. [16]byte)
			if g.outputParamNames != nil && g.outputParamNames[varName] {
				storeOutParam = varName
				// Output params of [N]byte type are passed as [N x i8]* directly
				dataPtr = g.varAddr(varName)
				g.tmpIdx++
				castReg := fmt.Sprintf("%%lestore.cast.%d", g.tmpIdx)
				// Determine array size from arraySizes
				arrSize := int64(16)
				if g.arraySizes != nil {
					if sz, ok := g.arraySizes[varName]; ok {
						arrSize = sz
					}
				}
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = bitcast [%d x i8]* %s to i8*\n",
						g.indent(), castReg, arrSize, dataPtr))
				}
				dataPtr = castReg
			} else {
				dataPtr = g.byteArrDataPtr(sb, args[0])
			}
		} else {
			dataPtr = g.byteArrDataPtr(sb, args[0])
		}
		offsetVal := g.evalI64Arg(sb, args[1])
		// Get the value as i32 (truncate from i64 if needed)
		valI64 := g.evalI64Arg(sb, args[2])
		g.tmpIdx++
		valI32 := fmt.Sprintf("%%lestore.trunc.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), valI32, valI64))
		}
		// GEP to offset
		g.tmpIdx++
		gepReg := fmt.Sprintf("%%lestore.gep.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n",
				g.indent(), gepReg, dataPtr, offsetVal))
		}
		// Bitcast to i32*
		g.tmpIdx++
		typedPtr := fmt.Sprintf("%%lestore.typed.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n",
				g.indent(), typedPtr, gepReg))
		}
		// Store i32
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%sstore i32 %s, i32* %s\n",
				g.indent(), valI32, typedPtr))
		}
		// 寫入 out 參數後標記已初始化，避免 return 路徑的延遲零填充覆蓋寫入內容
		if storeOutParam != "" && sb != nil {
			g.emitSetRetInitBit(sb, storeOutParam)
		}
		return "0"

	case "math-max":
		// math.max(a, b) → icmp sgt a, b → select
		if len(args) < 2 {
			return "0"
		}
		aVal := g.evalI64Arg(sb, args[0])
		bVal := g.evalI64Arg(sb, args[1])
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%maxcmp.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%maxsel.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpReg, aVal, bVal))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), selReg, cmpReg, aVal, bVal))
		}
		return selReg

	case "math-min":
		// math.min(a, b) → icmp slt a, b → select
		if len(args) < 2 {
			return "0"
		}
		aVal := g.evalI64Arg(sb, args[0])
		bVal := g.evalI64Arg(sb, args[1])
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%mincmp.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%minsel.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, aVal, bVal))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), selReg, cmpReg, aVal, bVal))
		}
		return selReg

	case "math-abs":
		// math.abs(a) → sub 0, a if negative → select
		if len(args) < 1 {
			return "0"
		}
		aVal := g.evalI64Arg(sb, args[0])
		g.tmpIdx++
		subReg := fmt.Sprintf("%%abssub.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%abscmp.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%abssel.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, 0\n", g.indent(), cmpReg, aVal))
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 0, %s\n", g.indent(), subReg, aVal))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), selReg, cmpReg, subReg, aVal))
		}
		return selReg

	case "math-clamp":
		// math.clamp(val, min, max) → clamp(val, min, max)
		// result = max(min, min(val, max))
		if len(args) < 3 {
			return "0"
		}
		valArg := g.evalI64Arg(sb, args[0])
		minArg := g.evalI64Arg(sb, args[1])
		maxArg := g.evalI64Arg(sb, args[2])
		g.tmpIdx++
		cmpHi := fmt.Sprintf("%%clamphi.%d", g.tmpIdx)
		g.tmpIdx++
		selHi := fmt.Sprintf("%%clampselhi.%d", g.tmpIdx)
		g.tmpIdx++
		cmpLo := fmt.Sprintf("%%clamplo.%d", g.tmpIdx)
		g.tmpIdx++
		selLo := fmt.Sprintf("%%clampsello.%d", g.tmpIdx)
		if sb != nil {
			// min(val, max)
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, %s\n", g.indent(), cmpHi, valArg, maxArg))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), selHi, cmpHi, maxArg, valArg))
			// max(result, min)
			sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpLo, selHi, minArg))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), selLo, cmpLo, minArg, selHi))
		}
		return selLo
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
		// FFI extern 返回的 C 字串指標可能指向靜態記憶體（getenv/strerror 等），
		// 直接包裝進 %str-long 會在 emitHeapFree 時 free 非堆記憶體 → UB。
		// 改為 malloc + memcpy 複製到獨立緩衝區，與 RetCStrToStr 路徑一致。
		return g.emitFFIExternStrClone(sb, callReg)
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
