package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// llvmTypeToNolang maps LLVM primitive types to all nolang type names that
// could have produced them. This is needed for method-call resolution, where
// the receiver's variable has an LLVM type (e.g. i32) but the method is
// registered under its nolang type name (e.g. char.is-alpha).
var llvmTypeToNolang = map[string][]string{
	"i1":        {"bool"},
	"i8":        {"byte", "i8", "u8"},
	"i16":       {"i16", "u16"},
	"i32":       {"char", "i32", "u32"},
	"i64":       {"i64", "u64"},
	"float":     {"f32"},
	"double":    {"f64"},
	"%str-long": {"str"},
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

// convertShortToLong converts a %%str-short* to a %%str-long* for use as function argument.
// Returns the %%str-long* register name.
func (g *Generator) convertShortToLong(sb *strings.Builder, shortReg string) string {
	g.tmpIdx++
	strAlloca := fmt.Sprintf("%%str-long.s2s.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strAlloca))

		// Extract length: load i8, mask 0x7F, zext to i64
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%s2s.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		lenRaw := fmt.Sprintf("%%s2s.len.raw.%d", g.tmpIdx)
		g.tmpIdx++
		lenMask := fmt.Sprintf("%%s2s.len.mask.%d", g.tmpIdx)
		g.tmpIdx++
		lenExt := fmt.Sprintf("%%s2s.len.ext.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 0\n", g.indent(), lenGEP, shortReg))
		sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), lenRaw, lenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = and i8 %s, 127\n", g.indent(), lenMask, lenRaw))
		sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), lenExt, lenMask))

		// Extract data pointer: bitcast [127 x i8]* field to i8*
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%s2s.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataCast := fmt.Sprintf("%%s2s.data.cast.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-short, %%str-short* %s, i32 0, i32 1\n", g.indent(), dataGEP, shortReg))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [127 x i8]* %s to i8*\n", g.indent(), dataCast, dataGEP))

		// Store into %%str-long struct
		g.tmpIdx++
		dstLenGEP := fmt.Sprintf("%%s2s.dst.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dstDataGEP := fmt.Sprintf("%%s2s.dst.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), dstLenGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenExt, dstLenGEP))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dstDataGEP, strAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), dataCast, dstDataGEP))
	}
	return strAlloca
}

// generateCallArg 生成單個函數調用參數的 LLVM 表示
func (g *Generator) generateCallArg(sb *strings.Builder, arg parser.Expression) string {
	switch a := arg.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[a.Value]; ok && t == "%str-long" {
				return "%str-long* " + g.varAddr(a.Value)
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
			ev = g.convertShortToLong(sb, ev)
		}
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
		// %idx.zext.*, %arr.idx.val.*, %vec.idx.val.* 等是載入值（i64）
		if strings.Contains(ev, ".gep.") || strings.Contains(ev, ".elem.") {
			// GEP result is a pointer; need its LLVM type
			// Determine element type from source variable
			ptrType := "i64*"
			if ident, ok := a.Left.(*parser.Identifier); ok {
				if g.varTypes != nil {
					if t, ok := g.varTypes[ident.Value]; ok {
						if strings.HasPrefix(t, "%") && strings.HasSuffix(t, "*") {
							ptrType = t // %str-long* etc.
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
		// 切片表達式回傳 %vec 或 %str-long（已分配在 stack 上）
		ev := g.generateExprWithSB(sb, arg)
		// 從變數型別推斷指標型別
		ptrType := "%vec*"
		if ident, ok := a.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					if t == "%str-long" || t == "%str-short" {
						ptrType = "%str-long*"
					}
				}
			}
		}
		return ptrType + " " + ev
	default:
		ev := g.generateExprWithSB(sb, arg)
		if strings.HasPrefix(ev, "%str-longlit") {
			return "%str-long* " + ev
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
		var innerMethodRecv *parser.Identifier // method receiver if inner call is s.method(...)
		if ident, ok := innerCall.Function.(*parser.Identifier); ok {
			innerFnName = ident.Value
		} else if dot, ok := innerCall.Function.(*parser.DotExpression); ok {
			// 解析 receiver method call：s.to-bytes → str.to-bytes
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				innerMethodRecv = recv
				candidate := recv.Value + "." + dot.Property
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
				// str/str-short/arr/vec 方法解析
				if innerFnName == "" {
					if recvType, ok := g.varTypes[recv.Value]; ok {
						srcType := strings.TrimPrefix(recvType, "%")
						candidates := []string{srcType}
						if srcType == "str-short" {
							candidates = append(candidates, "str")
						}
						if primAliases, ok := llvmTypeToNolang[srcType]; ok {
							candidates = append(candidates, primAliases...)
						}
						for _, cand := range candidates {
							shortName := cand + "." + dot.Property
							if g.funcRetTypes != nil {
								if t, ok := g.funcRetTypes[shortName]; ok {
									isMultiResult := false
									if g.funcNumResults != nil {
										if n, ok := g.funcNumResults[shortName]; ok && n > 1 {
											isMultiResult = true
										}
									}
									if t != "void" || isMultiResult {
										innerFnName = shortName
										break
									}
								}
							}
						}
					}
				}
				// fallback
				if innerFnName == "" {
					innerFnName = candidate
				}
			}
		}
		if g.funcRetTypes != nil {
			if t, ok := g.funcRetTypes[innerFnName]; ok {
				retType = t
			}
		}

		// 生成內層調用的參數（receiver 作為第一個參數）
		innerArgs := make([]string, 0)
		if innerMethodRecv != nil {
			// str-short 接收者呼叫 str.* 方法時，需轉換為 %str-long
			if strings.HasPrefix(innerFnName, "str.") {
				if t, ok := g.varTypes[innerMethodRecv.Value]; ok && t == "%str-short" {
					shortPtr := g.varAddr(innerMethodRecv.Value)
					strPtr := g.convertShortToLong(sb, shortPtr)
					innerArgs = append(innerArgs, "%str-long* "+strPtr)
				} else {
					innerArgs = append(innerArgs, g.generateCallArg(sb, innerMethodRecv))
				}
			} else {
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
				allArgs := make([]string, 0, len(innerArgs)+len(expr.Arguments))
				allArgs = append(allArgs, innerArgs...)
				for _, outArg := range expr.Arguments {
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

	// 通過 BuiltinMethodList 分派（LLVMIntrinsic / CLibCall / LLVMConv / ForwardFunc）
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
		// ForwardFunc: str-copy→memcpy, str-eq→memcmp, str-fill→memset
		if m.ForwardFunc != "" {
			if r := g.genForwardFunc(sb, m.ForwardFunc, expr, nil); r != "" || m.ForwardFunc == "memcpy" || m.ForwardFunc == "memset" {
				return r
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

	// val() and err() are handled at the assignment level
	if fnName == "val" || fnName == "err" {
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
			// Also resolve str/str-short/arr/vec/char/byte/bool receiver methods (e.g. s.index → str.index, c.is-alpha → char.is-alpha)
			if methodReceiver == nil {
				if recvType, ok := g.varTypes[recv.Value]; ok {
					srcType := strings.TrimPrefix(recvType, "%")
					candidates := []string{srcType}
					if srcType == "str-short" || srcType == "str-long" {
						candidates = append(candidates, "str")
					}
					// Primitive LLVM types may correspond to multiple nolang type names.
					// For example, i32 can be char, i32, or u32. Try all candidates.
					if primAliases, ok := llvmTypeToNolang[srcType]; ok {
						candidates = append(candidates, primAliases...)
					}
					for _, cand := range candidates {
						shortName := cand + "." + dot.Property
						if g.funcRetTypes != nil {
							// 接受單結果（非 void）或帶輸出參數的 void 函數（funcNumResults >= 1）。
							// str.repeat / str.trim 等多結果函數的 funcRetTypes 為 void，
							// 但 funcNumResults 為 2；str.to-i64 等單輸出函數 funcNumResults 為 1。
							if t, ok := g.funcRetTypes[shortName]; ok {
								hasOutput := false
								if g.funcNumResults != nil {
									if n, ok := g.funcNumResults[shortName]; ok && n >= 1 {
										hasOutput = true
									}
								}
								if t != "void" || hasOutput {
									fnName = shortName
									methodReceiver = recv
									break
								}
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
				if t, ok := g.funcRetTypes[shortName]; ok {
					hasOutput := false
					if g.funcNumResults != nil {
						if n, ok := g.funcNumResults[shortName]; ok && n >= 1 {
							hasOutput = true
						}
					}
					if t != "void" || hasOutput {
						fnName = shortName
						methodReceiver = receiverExpr
					}
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
			if m := builtin.FindBuiltinMethod(shortName); m != nil {
				fnName = shortName
				methodReceiver = receiverExpr
			}
		} else if _, ok := receiverExpr.(*parser.FloatLiteral); ok {
			// 浮點字面量接收者（如 3.14.to-str()）
			// 浮點字面量預設為 f64 型別
			shortName := "f64." + dot.Property
			if m := builtin.FindBuiltinMethod(shortName); m != nil {
				fnName = shortName
				methodReceiver = receiverExpr
			}
		} else if _, ok := receiverExpr.(*parser.PrefixExpression); ok {
			// 前綴表達式接收者（如 (-42).to-str()）
			// 負整數字面量被解析為 PrefixExpression(-, IntegerLiteral)
			// 視為 i64 型別
			shortName := "i64." + dot.Property
			if m := builtin.FindBuiltinMethod(shortName); m != nil {
				fnName = shortName
				methodReceiver = receiverExpr
			}
		}
	}

	// 方法解析後，檢查是否為 build-in 方法（如 str.eq、str.copy、i64.to-str、f64.to-str）
	// 此時 fnName 已解析為型別名 + 屬性（如 "str.eq"），methodReceiver 為接收者表達式。
	// build-in 方法不在 funcRetTypes 中，需透過 FindBuiltinMethod 查找並分派。
	if methodReceiver != nil {
		if m := builtin.FindBuiltinMethod(fnName); m != nil {
			if m.ForwardFunc != "" {
				if r := g.genForwardFunc(sb, m.ForwardFunc, expr, methodReceiver); r != "" || m.ForwardFunc == "memcpy" || m.ForwardFunc == "memset" {
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
				return g.genCLibCall(sb, m, methodEvalArgs)
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
	hasOutputParam := false
	if g.funcNumResults != nil {
		if n, ok := g.funcNumResults[fnName]; ok && n == 1 && retType != "void" {
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

	// void + 單輸出參數，調用方未顯式傳遞輸出變數（如 v1 = s.to-i64()）。
	// 此類函數（如 str.to-i64）的 funcRetTypes 為 void，但 funcNumResults 為 1，
	// 輸出通過指標傳遞。需分配臨時空間、傳遞指標、調用後載入結果作為返回值。
	voidSingleOutput := false
	voidSingleOutputType := ""
	if retType == "void" && g.funcNumResults != nil && g.funcResultLLVMType != nil {
		if n, ok := g.funcNumResults[fnName]; ok && n == 1 {
			if ts, ok := g.funcResultLLVMType[fnName]; ok && len(ts) == 1 {
				voidSingleOutput = true
				voidSingleOutputType = ts[0]
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

	// genTypedArg generates a typed pointer argument for a single expression
	genTypedArg := func(arg parser.Expression, argIdx int) string {
		switch a := arg.(type) {
		case *parser.Identifier:
			// str 型別用 %str-long* 指標
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && t == "%str-long" {
					return "%str-long* " + g.varAddr(a.Value)
				}
				// str-short 接收者呼叫 str.* 方法時，需轉換為 %str-long
				if t, ok := g.varTypes[a.Value]; ok && t == "%str-short" && strings.HasPrefix(fnName, "str.") {
					shortPtr := g.varAddr(a.Value)
					strPtr := g.convertShortToLong(sb, shortPtr)
					return "%str-long* " + strPtr
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
			// 使用實際型別（不再硬編碼為 i64*）
			argType := "i64"
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok {
					argType = t
				}
			}
			// 對 by-reference 函數呼叫，先將引數值存到暫存變數，
			// 避免被呼叫函數修改原始變數（例如 gcd 會修改 a, b）
			if retType != "void" && g.isIntegerLLVMType(argType) {
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
			if len(a.Value) <= 127 {
				ev = g.convertShortToLong(sb, ev)
			}
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
						if t == "%str-long" || t == "%str-short" {
							ptrType = "%str-long*"
						}
					}
				}
			}
			return ptrType + " " + ev
		default:
			ev := g.generateExprWithSB(sb, arg)
			if strings.HasPrefix(ev, "%str-longlit") {
				return "%str-long* " + ev
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

	// void + 單輸出：分配臨時輸出空間並附加指標到參數列表
	voidSingleTmp := ""
	if voidSingleOutput {
		g.tmpIdx++
		voidSingleTmp = fmt.Sprintf("%%vso.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), voidSingleTmp, voidSingleOutputType))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), g.llvmTypeSize(voidSingleOutputType), voidSingleTmp))
			// %str-long 類型需要初始化 data 指標，否則方法體 out[i] = val 會因 data 為 null 而崩潰
			if voidSingleOutputType == "%str-long" {
				g.tmpIdx++
				dataBuf := fmt.Sprintf("%%vso.data.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca [128 x i8]\n", g.indent(), dataBuf))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 128, i8* %s)\n", g.indent(), dataBuf))
				// 初始化 len = 0
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%vso.len.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
				// 設置 data 指標指向緩衝區
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%vso.data.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), dataBuf, dataGEP))
			} else if voidSingleOutputType == "%vec" {
				// %vec 類型需要初始化 data 指標，否則方法體 out[i] = val 會因 data 為 null 而崩潰
				vecBufSize := 256
				g.tmpIdx++
				dataBuf := fmt.Sprintf("%%vso.vecdata.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca [%d x i8]\n", g.indent(), dataBuf, vecBufSize))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %s)\n", g.indent(), vecBufSize, dataBuf))
				// 初始化 len = 0（field 0）
				g.tmpIdx++
				lenGEP := fmt.Sprintf("%%vso.veclen.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), lenGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), lenGEP))
				// 設置 cap = vecBufSize（field 1）
				g.tmpIdx++
				capGEP := fmt.Sprintf("%%vso.veccap.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), capGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), vecBufSize, capGEP))
				// 設置 data 指標指向緩衝區（field 2）
				g.tmpIdx++
				dataGEP := fmt.Sprintf("%%vso.vecdata.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, voidSingleTmp))
				sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), dataBuf, dataGEP))
			}
		}
		typedArgs = append(typedArgs, voidSingleOutputType+"* "+voidSingleTmp)
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

	// void + 單輸出：調用後載入結果作為返回值
	if voidSingleOutput {
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s\n", g.indent(), callStr))
			g.tmpIdx++
			loadReg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, voidSingleOutputType, voidSingleOutputType, voidSingleTmp))
			return loadReg
		}
		return ""
	}

	// For non-void returns without output param: capture return value as expression
	if retType != "void" && sb != nil {
		g.tmpIdx++
		callReg := fmt.Sprintf("%%call.tmp.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = %s\n", g.indent(), callReg, callStr))
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
// Handles Identifier (str/str-short variables) and StringLiteral.
func (g *Generator) strExprDataPtr(sb *strings.Builder, arg parser.Expression) string {
	switch a := arg.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[a.Value]; ok {
				ptr := g.varAddr(a.Value)
				if t == "%str-short" {
					return g.extractStrShortDataPtr(sb, ptr)
				}
				if t == "%str-long" {
					return g.extractStrDataPtr(sb, ptr)
				}
			}
		}
	case *parser.StringLiteral:
		ptr := g.generateExprWithSB(sb, arg)
		if len(a.Value) <= 127 {
			return g.extractStrShortDataPtr(sb, ptr)
		}
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
		// str.copy(dst, n) [method] or str-copy(src, dst, n) [global] → memcpy(dst_data, src_data, n)
		if len(args) < 3 {
			return ""
		}
		srcPtr := g.strExprDataPtr(sb, args[0])
		dstPtr := g.strExprDataPtr(sb, args[1])
		nVal := g.evalI64Arg(sb, args[2])
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n", g.indent(), dstPtr, srcPtr, nVal))
		}
		return ""

	case "eq-raw":
		// str.eq(b, n) [method] or str-eq(a, b, n) [global] → memcmp(a_data, b_data, n) == 0 → zext to i64
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
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @memcmp(i8* %s, i8* %s, i64 %s)\n", g.indent(), cmpReg, aPtr, bPtr, nVal))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), eqReg, cmpReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, eqReg))
		}
		return zextReg

	case "memset":
		// str-fill(s, n, val) → memset(s_data, val, n)
		// Note: C memset signature is void* memset(void*, int, size_t)
		if len(args) < 3 {
			return ""
		}
		sPtr := g.strExprDataPtr(sb, args[0])
		nVal := g.evalI64Arg(sb, args[1])
		valVal := g.evalI64Arg(sb, args[2])
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i8* @memset(i8* %s, i32 %s, i64 %s)\n", g.indent(), sPtr, valVal, nVal))
		}
		return ""
	}
	return ""
}
