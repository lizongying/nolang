package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

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

// convertSmailToStr converts a %%str-smail* to a %%str* for use as function argument.
// Returns the %%str* register name.
func (g *Generator) convertSmailToStr(sb *strings.Builder, smailReg string) string {
	g.tmpIdx++
	strAlloca := fmt.Sprintf("%%str.s2s.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%str\n", g.indent(), strAlloca))

		// Extract length: load i8, mask 0x7F, zext to i64
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%s2s.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		lenRaw := fmt.Sprintf("%%s2s.len.raw.%d", g.tmpIdx)
		g.tmpIdx++
		lenMask := fmt.Sprintf("%%s2s.len.mask.%d", g.tmpIdx)
		g.tmpIdx++
		lenExt := fmt.Sprintf("%%s2s.len.ext.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %s, i32 0, i32 0\n", g.indent(), lenGEP, smailReg))
		sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), lenRaw, lenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = and i8 %s, 127\n", g.indent(), lenMask, lenRaw))
		sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), lenExt, lenMask))

		// Extract data pointer: bitcast [127 x i8]* field to i8*
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%s2s.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataCast := fmt.Sprintf("%%s2s.data.cast.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-smail, %%str-smail* %s, i32 0, i32 1\n", g.indent(), dataGEP, smailReg))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dataCast, dataGEP))

		// Store into %%str struct
		g.tmpIdx++
		dstLenGEP := fmt.Sprintf("%%s2s.dst.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dstDataGEP := fmt.Sprintf("%%s2s.dst.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 0\n", g.indent(), dstLenGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenExt, dstLenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str, %%str* %s, i32 0, i32 1\n", g.indent(), dstDataGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), dataCast, dstDataGEP))
	}
	return strAlloca
}

// generateCallArg 生成單個函數調用參數的 LLVM 表示
func (g *Generator) generateCallArg(sb *strings.Builder, arg parser.Expression) string {
	switch a := arg.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[a.Value]; ok && t == "%str" {
				return "%str* " + g.varAddr(a.Value)
			}
			if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
				return t + "* " + g.varAddr(a.Value)
			}
			if t, ok := g.varTypes[a.Value]; ok && t == "double" {
				return "double* " + g.varAddr(a.Value)
			}
			// %vec / %arr / 任何 struct 指標型別 → 變數本身已是指標
			if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "%") {
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
		if len(a.Value) <= 127 {
			ev = g.convertSmailToStr(sb, ev)
		}
		return "%str* " + ev
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
		// %idx.gep.*, %arr.idx.elem.*, %vec.idx.elem.*, %stridx.gep.* 等都是 GEP 結果（指標）
		// %idx.zext.*, %arr.idx.val.*, %vec.idx.val.* 等是載入值（i64）
		if strings.Contains(ev, ".gep.") || strings.Contains(ev, ".elem.") {
			// GEP result is a pointer; need its LLVM type
			// Determine element type from source variable
			ptrType := "i64*"
			if ident, ok := a.Left.(*parser.Identifier); ok {
				if g.varTypes != nil {
					if t, ok := g.varTypes[ident.Value]; ok {
						if strings.HasPrefix(t, "%") && strings.HasSuffix(t, "*") {
							ptrType = t // %str* etc.
						}
					}
				}
			}
			return ptrType + " " + ev
		}
		// SSA value (e.g., %idx.zext.* for []byte) — wrap in temp
		g.tmpIdx++
		tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
		}
		return "i64* " + tmpName
	case *parser.SliceExpression:
		// 切片表達式回傳 %vec 或 %str（已分配在 stack 上）
		ev := g.generateExprWithSB(sb, arg)
		// 從變數型別推斷指標型別
		ptrType := "%vec*"
		if ident, ok := a.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					if t == "%str" || t == "%str-smail" {
						ptrType = "%str*"
					}
				}
			}
		}
		return ptrType + " " + ev
	default:
		ev := g.generateExprWithSB(sb, arg)
		if strings.HasPrefix(ev, "%strlit") {
			return "%str* " + ev
		} else if strings.HasPrefix(ev, "%") {
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
					} else if t == "%str" {
						ptrType = "%str*"
					} else if t == "i8*" {
						ptrType = "i8**"
					}
				}
				if idx := strings.IndexByte(baseName, '.'); idx > 0 {
					if t, ok := g.varTypes[baseName[:idx]]; ok {
						if t == "double" {
							ptrType = "double*"
						} else if t == "%str" {
							ptrType = "%str*"
						}
					}
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

func (g *Generator) generateCallExpression(sb *strings.Builder, expr *parser.CallExpression) string {
	// 處理 func(args)(output) 模式：
	// 當 Function 是 CallExpression 時，表示內層調用 + 輸出參數捕獲
	// 例如：str-index(s, sn, target, tn)(pos)
	if innerCall, ok := expr.Function.(*parser.CallExpression); ok {
		// 確定內層調用的返回型別
		retType := "void"
		innerFnName := ""
		if ident, ok := innerCall.Function.(*parser.Identifier); ok {
			innerFnName = ident.Value
		} else if dot, ok := innerCall.Function.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				innerFnName = recv.Value + "." + dot.Property
			}
		}
		if g.funcRetTypes != nil {
			if t, ok := g.funcRetTypes[innerFnName]; ok {
				retType = t
			}
		}

		// 生成內層調用的參數
		innerArgs := make([]string, len(innerCall.Arguments))
		for i, arg := range innerCall.Arguments {
			innerArgs[i] = g.generateCallArg(sb, arg)
		}

		if retType == "void" {
			// void 返回：直接調用
			// 檢查是否為多結果函數（curried 呼叫 → 單次呼叫，附加輸出參數）
			numResults := 0
			if g.funcNumResults != nil {
				// 嘗試多個名稱變體（可能已被 mangleOverloads 修飾）
				for _, name := range []string{innerFnName, innerFnName + "_i64_i64_i64_i64"} {
					if n, ok := g.funcNumResults[name]; ok && n > numResults {
						numResults = n
					}
				}
			}
			if numResults > 1 {
				// 多結果：將輸出參數附加到呼叫，傳遞指標
				allArgs := make([]string, 0, len(innerArgs)+len(expr.Arguments))
				allArgs = append(allArgs, innerArgs...)
				for _, outArg := range expr.Arguments {
					allArgs = append(allArgs, g.generateCallArg(sb, outArg))
				}
				sb.WriteString(fmt.Sprintf("%scall void @%s(%s)\n", g.indent(), sanitizeLLVMName(innerFnName), strings.Join(allArgs, ", ")))
				return ""
			}
			// void 返回：直接調用，然後為每個輸出參數分配空間
			for _, outArg := range expr.Arguments {
				if ident, ok := outArg.(*parser.Identifier); ok {
					varName := ident.Value
					if _, exists := g.varTypes[varName]; !exists {
						g.varTypes[varName] = "i64"
						g.tmpIdx++
						g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: "i64", Size: 8})
						sb.WriteString(fmt.Sprintf("%s%%%s = alloca i64\n", g.indent(), varName))
						sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 8, i8* %%%s)\n", g.indent(), varName))
					}
				}
			}
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
		if recv, ok := dot.Receiver.(*parser.Identifier); ok {
			fnName = recv.Value + "." + dot.Property
		}
	}

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

	// 通過 BuiltinMethodList 分派（LLVMIntrinsic / CLibCall / LLVMConv）
	if m := builtin.FindBuiltinMethod(fnName); m != nil {
		if m.LLVMIntrinsic != "" {
			a := evalArgs()
			argStr := ""
			for i, v := range a {
				if i > 0 {
					argStr += ", "
				}
				argStr += "double " + v
			}
			return fmt.Sprintf("call double @%s(%s)", m.LLVMIntrinsic, argStr)
		}
		if m.CLibCall != nil {
			return g.genCLibCall(sb, m, evalArgs)
		}
		if m.LLVMConv != nil {
			return g.genLLVMConv(sb, m, evalArgs)
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

	// val() and err() are handled at the assignment level
	if fnName == "val" || fnName == "err" {
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s; %s() is only valid in assignment context\n", g.indent(), fnName))
		}
		return "0"
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
	// if it's an Identifier (a variable to store the result into).
	hasOutputParam := false
	if g.funcNumResults != nil {
		if n, ok := g.funcNumResults[fnName]; ok && n == 1 && retType != "void" {
			if len(expr.Arguments) > 0 {
				if _, ok := expr.Arguments[len(expr.Arguments)-1].(*parser.Identifier); ok {
					hasOutputParam = true
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

	// genTypedArg generates a typed pointer argument for a single expression
	genTypedArg := func(arg parser.Expression) string {
		switch a := arg.(type) {
		case *parser.Identifier:
			// str 型別用 %str* 指標
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && t == "%str" {
					return "%str* " + g.varAddr(a.Value)
				}
			}
			// 陣列型別用正確的指標型別
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
					return t + "* " + g.varAddr(a.Value)
				}
			}
			// double 型別用 double* 指標
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && t == "double" {
					return "double* " + g.varAddr(a.Value)
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
			if len(a.Value) <= 127 {
				ev = g.convertSmailToStr(sb, ev)
			}
			return "%str* " + ev
		case *parser.IntegerLiteral:
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), a.Value, tmpName))
			}
			return "i64* " + tmpName
		case *parser.IndexExpression:
			ev := g.generateExprWithSB(sb, arg)
			if strings.Contains(ev, ".gep.") || strings.Contains(ev, ".elem.") {
				ptrType := "i64*"
				if ident, ok := a.Left.(*parser.Identifier); ok {
					if g.varTypes != nil {
						if t, ok := g.varTypes[ident.Value]; ok {
							if strings.HasPrefix(t, "%") && strings.HasSuffix(t, "*") {
								ptrType = t
							}
						}
					}
				}
				return ptrType + " " + ev
			}
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
			}
			return "i64* " + tmpName
		case *parser.SliceExpression:
			ev := g.generateExprWithSB(sb, arg)
			ptrType := "%vec*"
			if ident, ok := a.Left.(*parser.Identifier); ok {
				if g.varTypes != nil {
					if t, ok := g.varTypes[ident.Value]; ok {
						if t == "%str" || t == "%str-smail" {
							ptrType = "%str*"
						}
					}
				}
			}
			return ptrType + " " + ev
		default:
			ev := g.generateExprWithSB(sb, arg)
			if strings.HasPrefix(ev, "%strlit") {
				return "%str* " + ev
			} else if strings.HasPrefix(ev, "%") && strings.Contains(ev, ".") {
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
	for _, arg := range nonVariadicArgs {
		typedArgs = append(typedArgs, genTypedArg(arg))
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
				sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), dataCast, dataGEP))
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

	// Make the call
	callStr := fmt.Sprintf("call %s @%s(%s)", retType, sanitizeLLVMName(fnName), strings.Join(typedArgs, ", "))

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

	return callStr
}
