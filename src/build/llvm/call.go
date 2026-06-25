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
				if g.globalVars != nil && g.globalVars[a.Value] {
					return "%str* @" + a.Value
				}
				return "%str* %" + a.Value
			}
			if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
				return t + "* %" + a.Value
			}
			if t, ok := g.varTypes[a.Value]; ok && t == "double" {
				return "double* %" + a.Value
			}
		}
		varAddr := "%" + a.Value
		if g.globalVars != nil && g.globalVars[a.Value] {
			varAddr = "@" + a.Value
		}
		return "i64* " + varAddr
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
	default:
		ev := g.generateExprWithSB(sb, arg)
		if strings.HasPrefix(ev, "%strlit") {
			return "%str* " + ev
		} else if strings.HasPrefix(ev, "%") {
			// SSA register (value, not pointer) — keep the full register name
			// and infer its pointer type from varTypes[baseName]
			parts := strings.SplitN(ev, ".", 2)
			baseName := strings.TrimPrefix(parts[0], "%")
			// strip trailing "gep" or other suffixes from baseName for varTypes lookup
			lookupName := baseName
			// Use baseName (which may include suffixes) directly; varTypes only has plain var names,
			// so fall back to plain when suffix-bearing lookup misses.
			if g.varTypes != nil {
				if t, ok := g.varTypes[lookupName]; ok {
					if t == "double" {
						return "double* " + ev
					}
					if t == "%str" {
						return "%str* " + ev
					}
				}
				// try without suffix
				if idx := strings.IndexByte(baseName, '.'); idx > 0 {
					if t, ok := g.varTypes[baseName[:idx]]; ok {
						if t == "double" {
							return "double* " + ev
						}
						if t == "%str" {
							return "%str* " + ev
						}
					}
				}
			}
			return "i64* " + ev
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
			sb.WriteString(fmt.Sprintf("%scall void @%s(%s)\n", g.indent(), innerFnName, strings.Join(innerArgs, ", ")))
			return ""
		}

		// 有返回值：生成 call 並捕獲結果
		g.tmpIdx++
		retReg := fmt.Sprintf("%%callret.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = call %s @%s(%s)\n", g.indent(), retReg, retType, innerFnName, strings.Join(innerArgs, ", ")))

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
	typedArgs := make([]string, len(expr.Arguments))
	for i, arg := range expr.Arguments {
		switch a := arg.(type) {
		case *parser.Identifier:
			// str 型別用 %str* 指標
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && t == "%str" {
					if g.globalVars != nil && g.globalVars[a.Value] {
						typedArgs[i] = "%str* @" + a.Value
					} else {
						typedArgs[i] = "%str* %" + a.Value
					}
					break
				}
			}
			// 陣列型別用正確的指標型別
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
					// t is already LLVM type (e.g. "[4 x i64]"), don't call mapToLLVMType again
					typedArgs[i] = t + "* %" + a.Value
					break
				}
			}
			// double 型別用 double* 指標
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && t == "double" {
					typedArgs[i] = "double* %" + a.Value
					break
				}
			}
			varAddr := "%" + a.Value
			if g.globalVars != nil && g.globalVars[a.Value] {
				varAddr = "@" + a.Value
			}
			typedArgs[i] = "i64* " + varAddr
		case *parser.FloatLiteral:
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), fmt.Sprintf("%f", a.Value), tmpName))
			}
			typedArgs[i] = "double* " + tmpName
		case *parser.StringLiteral:
			// String literal: generate %str struct and pass as %str*
			ev := g.generateExprWithSB(sb, arg)
			if len(a.Value) <= 127 {
				// SSO string: convert %str-smail to %str for function argument
				ev = g.convertSmailToStr(sb, ev)
			}
			typedArgs[i] = "%str* " + ev
		case *parser.IntegerLiteral:
			g.tmpIdx++
			tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), a.Value, tmpName))
			}
			typedArgs[i] = "i64* " + tmpName
		default:
			ev := g.generateExprWithSB(sb, arg)
			if strings.HasPrefix(ev, "%strlit") {
				// String literal alloca
				typedArgs[i] = "%str* " + ev
			} else if strings.HasPrefix(ev, "%") && strings.Contains(ev, ".") {
				// SSA register (value, not pointer): store to temp alloca and pass pointer
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					// Determine type from the SSA register prefix
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
						typedArgs[i] = "double* " + tmpName
					} else {
						sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
						sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
						typedArgs[i] = "i64* " + tmpName
					}
				} else {
					typedArgs[i] = "i64* " + tmpName
				}
			} else if strings.HasPrefix(ev, "%") {
				parts := strings.Split(ev, ".")
				varName := strings.TrimPrefix(parts[0], "%")
				// double 型別用 double* 指標
				if g.varTypes != nil {
					if t, ok := g.varTypes[varName]; ok && t == "double" {
						typedArgs[i] = "double* %" + varName
						break
					}
				}
				typedArgs[i] = "i64* %" + varName
			} else if strings.Contains(ev, ".") {
				// float literal value (e.g. "180.000000")
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), ev, tmpName))
				}
				typedArgs[i] = "double* " + tmpName
			} else if _, err := fmt.Sscanf(ev, "%d", new(int)); err == nil {
				g.tmpIdx++
				tmpName := fmt.Sprintf("%%ref.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), tmpName))
					sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), ev, tmpName))
				}
				typedArgs[i] = "i64* " + tmpName
			} else {
				typedArgs[i] = ev
			}
		}
	}
	return fmt.Sprintf("call %s @%s(%s)", retType, fnName, strings.Join(typedArgs, ", "))
}
