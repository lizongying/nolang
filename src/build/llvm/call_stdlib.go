package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// callFmt — print/println/printf 家族
func (g *Generator) callFmt(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string, expr *parser.CallExpression) string {

	typedArg := func(v string) string {
		if strings.HasPrefix(v, "i8*") || strings.HasPrefix(v, "double") {
			return v
		}
		if strings.HasPrefix(v, "%") {
			parts := strings.SplitN(v, ".", 2)
			varName := strings.TrimPrefix(parts[0], "%")
			if g.varTypes != nil {
				if t, ok := g.varTypes[varName]; ok {
					if t == "double" {
						return "double " + v
					}
					if t == "%option" && g.optionInnerTypes != nil {
						if it, ok := g.optionInnerTypes[varName]; ok && it == "double" {
							return "double " + v
						}
					}
				}
			}
			return "i64 " + v
		}
		if strings.Contains(v, ".") {
			return "double " + v
		}
		return "i64 " + v
	}

	strDataPtr := func(arg parser.Expression) string {
		switch a := arg.(type) {
		case *parser.Identifier:
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok {
					if t == "%str-long" {
						return g.extractStrDataPtr(sb, "%"+a.Value)
					}
					if t == "%str-short" {
						return g.extractStrShortDataPtr(sb, "%"+a.Value)
					}
				}
			}
		case *parser.StringLiteral:
			ptr := g.generateExprWithSB(sb, a)
			if len(a.Value) <= 127 {
				return g.extractStrShortDataPtr(sb, ptr)
			}
			return g.extractStrDataPtr(sb, ptr)
		case *parser.InfixExpression:
			if (a.Operator == "-" || a.Operator == "+") && (g.isStringExpr(a.Left) || g.isStringExpr(a.Right)) {
				ptr := g.generateStrConcat(sb, a.Left, a.Right)
				return g.extractStrDataPtr(sb, ptr)
			}
		case *parser.GroupedExpression:
			if g.isStringExpr(a.Expression) {
				ptr := g.getStrPtr(sb, a.Expression)
				return g.extractStrDataPtr(sb, ptr)
			}
			return ""
		}
		return ""
	}

	if (fnName == "printf" || fnName == "fmt.printf") && hasArgs {
		var fmtArg string
		if strLit, ok := expr.Arguments[0].(*parser.StringLiteral); ok {
			fg := g.getFormatGlobal(strLit.Value)
			fmtArg = fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0)",
				len(strLit.Value)+1, len(strLit.Value)+1, fg)
		} else {
			fmtData := g.makeNullTerminatedStr(sb, expr.Arguments[0])
			if fmtData != "" {
				fmtArg = "i8* " + fmtData
			} else {
				a := evalArgs()
				fmtArg = strArg(a[0])
			}
		}
		args := fmtArg
		for i := 1; i < len(expr.Arguments); i++ {
			data := strDataPtr(expr.Arguments[i])
			if data != "" {
				nullStr := g.makeNullTerminatedStr(sb, expr.Arguments[i])
				if nullStr != "" {
					args += ", i8* " + nullStr
				} else {
					args += ", i8* " + data
				}
			} else {
				a := evalArgs()
				args += ", " + typedArg(a[i])
			}
		}
		if len(expr.Arguments) > 0 {
			if strLit, ok := expr.Arguments[0].(*parser.StringLiteral); ok {
				fmtStr := strLit.Value
				expected := 0
				for i := 0; i < len(fmtStr); i++ {
					if fmtStr[i] == '%' && i+1 < len(fmtStr) && fmtStr[i+1] != '%' {
						expected++
					}
				}
				got := len(expr.Arguments) - 1
				if got != expected {
					panic(fmt.Sprintf("printf: format string expects %d arguments, got %d\n  format: %q",
						expected, got, fmtStr))
				}
			}
		}
		return fmt.Sprintf("call i32 (i8*, ...) @printf(%s)", args)
	}

	printVariadic := func(newline bool) string {
		if !hasArgs {
			if newline {
				fg := g.getFormatGlobal("\n")
				return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0))",
					len("\n")+1, len("\n")+1, fg)
			}
			return ""
		}
		var sb2 strings.Builder
		for i, arg := range expr.Arguments {
			// Skip void function call arguments (call for side effects, don't print)
			if callExpr, ok := arg.(*parser.CallExpression); ok && !g.isNonVoidCall(callExpr) {
				g.generateExprWithSB(sb, arg)
				continue
			}
			if i > 0 {
				fg := g.getFormatGlobal(" ")
				sb2.WriteString(fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0))\n%s",
					len(" ")+1, len(" ")+1, fg, g.indent()))
			}
			dataPtr := strDataPtr(arg)
			if dataPtr != "" {
				strLen := g.strLenFromExpr(sb, arg)
				g.tmpIdx++
				lenI32 := fmt.Sprintf("%%str-longpr.len.i32.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), lenI32, strLen))
				fmtSpec := "%.*s"
				if newline && i == len(expr.Arguments)-1 {
					fmtSpec += "\n"
				}
				fg := g.getFormatGlobal(fmtSpec)
				sb2.WriteString(fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), i32 %s, i8* %s)",
					len(fmtSpec)+1, len(fmtSpec)+1, fg, lenI32, dataPtr))
			} else {
				v := g.generateExprWithSB(sb, arg)
				fmtSpec := ""
				if strings.HasPrefix(v, "i8*") {
					fmtSpec = "%s"
				} else if strings.HasPrefix(v, "%") {
					// Check variable type: double, bool, or default i64
					isDouble := false
					isBool := false
					isNarrow := false
					if ident, ok := arg.(*parser.Identifier); ok && g.varTypes != nil {
						if t, ok := g.varTypes[ident.Value]; ok {
							if t == "double" {
								isDouble = true
							}
							if t == "i1" {
								isBool = true
							}
							if t == "i8" || t == "i16" || t == "i32" {
								isNarrow = true
							}
							// Option variable: check inner type for double
							if t == "%option" && g.optionInnerTypes != nil {
								if it, ok := g.optionInnerTypes[ident.Value]; ok && it == "double" {
									isDouble = true
								}
							}
						}
					}
					if isDouble {
						fmtSpec = "%g"
					} else if isBool {
						// zext i1 to i64 for printf
						g.tmpIdx++
						zextReg := fmt.Sprintf("%%print.zext.%d", g.tmpIdx)
						sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, v))
						v = zextReg
						fmtSpec = "%lld"
					} else if isNarrow {
						// sign-extend i8/i16/i32 to i64 for printf (Nolang 中 i8/i16/i32 為有符號)
						if ident, ok := arg.(*parser.Identifier); ok && g.varTypes != nil {
							if t, ok := g.varTypes[ident.Value]; ok {
								// For option variables, use inner type for sext
								sextType := t
								if t == "%option" && g.optionInnerTypes != nil {
									if it, ok := g.optionInnerTypes[ident.Value]; ok {
										sextType = it
									}
								}
								if sextType != "%option" {
									g.tmpIdx++
									extReg := fmt.Sprintf("%%print.sext.%d", g.tmpIdx)
									sb.WriteString(fmt.Sprintf("%s%s = sext %s %s to i64\n", g.indent(), extReg, sextType, v))
									v = extReg
								}
							}
						}
						fmtSpec = "%lld"
					} else {
						fmtSpec = "%lld"
					}
				} else if strings.Contains(v, ".") {
					fmtSpec = "%g"
				} else {
					fmtSpec = "%lld"
				}
				if newline && i == len(expr.Arguments)-1 {
					fmtSpec += "\n"
				}
				fg := g.getFormatGlobal(fmtSpec)
				sb2.WriteString(fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), %s)",
					len(fmtSpec)+1, len(fmtSpec)+1, fg, typedArg(v)))
			}
		}
		return sb2.String()
	}

	if fnName == "print" || fnName == "fmt.print" {
		return printVariadic(true)
	}
	if fnName == "println" || fnName == "fmt.println" {
		if !hasArgs {
			fg := g.getFormatGlobal("\n")
			return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0))",
				len("\n")+1, len("\n")+1, fg)
		}
		return printVariadic(true)
	}
	if fnName == "println-empty" {
		fg := g.getFormatGlobal("\n")
		return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0))",
			len("\n")+1, len("\n")+1, fg)
	}

	printInt := func(fmtSpec, fn string) string {
		if fn == fnName {
			fg := g.getFormatGlobal(fmtSpec)
			a := evalArgs()
			return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), %s)",
				len(fmtSpec)+1, len(fmtSpec)+1, fg, llvmArg(a[0]))
		}
		return ""
	}
	if r := printInt("%lld", "print-i64"); r != "" {
		return r
	}
	if r := printInt("%lld", "fmt.print-i64"); r != "" {
		return r
	}
	if r := printInt("%lld\n", "println-i64"); r != "" {
		return r
	}
	if r := printInt("%lld\n", "fmt.println-i64"); r != "" {
		return r
	}
	if r := printInt("%d", "print-byte"); r != "" {
		return r
	}
	if r := printInt("%d", "fmt.print-byte"); r != "" {
		return r
	}
	if r := printInt("%d\n", "println-byte"); r != "" {
		return r
	}
	if r := printInt("%d\n", "fmt.println-byte"); r != "" {
		return r
	}
	if r := printInt("%c", "print-char"); r != "" {
		return r
	}
	if r := printInt("%c", "fmt.print-char"); r != "" {
		return r
	}
	if r := printInt("%c\n", "println-char"); r != "" {
		return r
	}
	if r := printInt("%c\n", "fmt.println-char"); r != "" {
		return r
	}

	printHex := func(fmtSpec, fn string) string {
		if fn == fnName && hasArgs {
			fg := g.getFormatGlobal(fmtSpec)
			a := evalArgs()
			return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), %s)",
				len(fmtSpec)+1, len(fmtSpec)+1, fg, typedArg(a[0]))
		}
		return ""
	}
	if r := printHex("%08llx", "print-hex32"); r != "" {
		return r
	}
	if r := printHex("%08llx", "fmt.print-hex32"); r != "" {
		return r
	}
	if r := printHex("%016llx", "print-hex64"); r != "" {
		return r
	}
	if r := printHex("%016llx", "fmt.print-hex64"); r != "" {
		return r
	}
	if r := printHex("%02llx", "print-hex8"); r != "" {
		return r
	}
	if r := printHex("%02llx", "fmt.print-hex8"); r != "" {
		return r
	}

	printFloat := func(fmtSpec, fn string) string {
		if fn == fnName && hasArgs {
			fg := g.getFormatGlobal(fmtSpec)
			a := evalArgs()
			return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), double %s)",
				len(fmtSpec)+1, len(fmtSpec)+1, fg, a[0])
		}
		return ""
	}
	if r := printFloat("%g", "print-f64"); r != "" {
		return r
	}
	if r := printFloat("%g\n", "println-f64"); r != "" {
		return r
	}

	printBool := func(fmtSpec, fn string) string {
		if fn == fnName && hasArgs {
			fg := g.getFormatGlobal(fmtSpec)
			a := evalArgs()
			g.tmpIdx++
			reg := fmt.Sprintf("%%boolpr.tmp.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.str.false, i64 0, i64 0)\n",
					g.indent(), reg, a[0]))
			}
			return fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), i8* %s)",
				len(fmtSpec)+1, len(fmtSpec)+1, fg, reg)
		}
		return ""
	}
	if r := printBool("%s", "print-bool"); r != "" {
		return r
	}
	if r := printBool("%s\n", "println-bool"); r != "" {
		return r
	}

	return ""
}

// callStrconv — 仍需 ForwardFunc 的特殊轉換
func (g *Generator) callStrconv(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string) string {

	// str.to-bool: strcmp + cmp + zext
	if fnName == "str.to-bool" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%boolcmp.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @strcmp(%s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0))\n",
				g.indent(), cmpReg, a[0]))
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
	}

	// bool.to-str: select + 构造 %str-long
	if fnName == "bool.to-str" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		selectReg := fmt.Sprintf("%%boolstr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.str.false, i64 0, i64 0)\n",
				g.indent(), selectReg, a[0]))
		}
		// strlen 计算长度（"true" = 4, "false" = 5）
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%boolstr.len.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @strlen(i8* %s)\n", g.indent(), lenReg, selectReg))
		}
		// 构造 %str-long { len, data }
		g.tmpIdx++
		strReg1 := fmt.Sprintf("%%boolstr.val.%d", g.tmpIdx)
		g.tmpIdx++
		strReg2 := fmt.Sprintf("%%boolstr.val.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), strReg1, lenReg))
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i8* %s, 1\n", g.indent(), strReg2, strReg1, selectReg))
		}
		return strReg2
	}

	return ""
}

// callBuiltin — 內建函數（len, cap, args-count, args-get, is-dir, stat-size, get-line）
func (g *Generator) callBuiltin(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string, expr *parser.CallExpression) string {

	// resolveSelfMethodCalls rewrites .len() inside method bodies to
	// Type.len(self) (e.g. []byte.len(self), str.len(self)).
	// If Type.len is not a user-defined function (not in funcRetTypes),
	// redirect to the builtin len handler.
	if strings.HasSuffix(fnName, ".len") && hasArgs {
		if _, exists := g.funcRetTypes[fnName]; !exists {
			fnName = "len"
		}
	}

	if fnName == "len" && hasArgs {
		arg0 := expr.Arguments[0]
		// Handle string variables: use extractLenDispatch for %str-long / %str-short
		if ident, ok := arg0.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					if t == "%str-long" || t == "%str-short" {
						return g.extractLenDispatch(sb, ident.Value)
					}
					// Handle %arr and %vec: load field 0 (i64 len) from the struct pointer
					if t == "%arr" || t == "%vec" {
						g.tmpIdx++
						lenGEP := fmt.Sprintf("%%builtin.len.gep.%d", g.tmpIdx)
						g.tmpIdx++
						lenReg := fmt.Sprintf("%%builtin.len.%d", g.tmpIdx)
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 0\n", g.indent(), lenGEP, t, t, ident.Value))
							sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, lenGEP))
						}
						return lenReg
					}
					// Handle fixed-size arrays [N x t]: return N as compile-time constant
					if strings.HasPrefix(t, "[") {
						closeB := strings.IndexByte(t, ']')
						if closeB > 0 {
							sizeStr := t[1:closeB]
							return sizeStr
						}
					}
				}
			}
		}
		// Handle string literals: compile-time known length
		if strLit, ok := arg0.(*parser.StringLiteral); ok {
			return fmt.Sprintf("%d", len(strLit.Value))
		}
		// Handle DotExpression on a slice field (e.g. len(.data) where .data is []byte).
		// generateDotExpression loads the %vec / %arr VALUE from the struct field, so the
		// argument is a struct value, not a pointer. Use extractvalue to get field 0 (len).
		if dot, ok := arg0.(*parser.DotExpression); ok {
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				if g.varTypes != nil {
					if t, ok := g.varTypes[ident.Value]; ok && strings.HasPrefix(t, "%") {
						structName := strings.TrimPrefix(t, "%")
						if fields, ok := g.structTypes[structName]; ok {
							for _, f := range fields {
								if f.name == dot.Property && (f.typ == "%vec" || f.typ == "%arr") {
									a := evalArgs()
									g.tmpIdx++
									lenReg := fmt.Sprintf("%%builtin.len.%d", g.tmpIdx)
									if sb != nil {
										sb.WriteString(fmt.Sprintf("%s%s = extractvalue %s %s, 0\n", g.indent(), lenReg, f.typ, a[0]))
									}
									return lenReg
								}
							}
						}
					}
				}
			}
		}
		// Default fallback: generic i64* load (for raw pointers)
		a := evalArgs()
		arg := a[0]
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%builtin.len.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, arg))
		}
		return lenReg
	}

	if fnName == "cap" && hasArgs {
		a := evalArgs()
		arg := a[0]
		g.tmpIdx++
		capGEP := fmt.Sprintf("%%builtin.cap.gep.%d", g.tmpIdx)
		g.tmpIdx++
		capReg := fmt.Sprintf("%%builtin.cap.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i64, i64* %s, i64 1\n", g.indent(), capGEP, arg))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), capReg, capGEP))
		}
		return capReg
	}

	// args-count: 返回命令行參數數量
	if fnName == "args-count" {
		g.tmpIdx++
		loadReg := fmt.Sprintf("%%argc.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%argc.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %%argc.addr\n", g.indent(), loadReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), extReg, loadReg))
		}
		return extReg
	}

	// args-get: 返回第 idx 個命令行參數
	if fnName == "args-get" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		argvReg := fmt.Sprintf("%%argv.load.%d", g.tmpIdx)
		g.tmpIdx++
		idxExt := fmt.Sprintf("%%argv.idx.%d", g.tmpIdx)
		g.tmpIdx++
		gepReg := fmt.Sprintf("%%argv.gep.%d", g.tmpIdx)
		g.tmpIdx++
		ptrReg := fmt.Sprintf("%%argv.ptr.%d", g.tmpIdx)
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%argv.len.%d", g.tmpIdx)
		g.tmpIdx++
		strReg := fmt.Sprintf("%%argv.str.%d", g.tmpIdx)
		g.tmpIdx++
		bufReg := fmt.Sprintf("%%argv.buf.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i8**, i8*** %%argv.addr\n", g.indent(), argvReg))
			// Extend idx to i64 for GEP
			sb.WriteString(fmt.Sprintf("%s%s = zext i64 %s to i64\n", g.indent(), idxExt, a[0]))
			// GEP to get argv[idx] (i8*)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8*, i8** %s, i64 %s\n", g.indent(), gepReg, argvReg, idxExt))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), ptrReg, gepReg))
			// strlen to get length
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @strlen(i8* %s)\n", g.indent(), lenReg, ptrReg))
			// Allocate %str-long struct { i64 len, i8* data }
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			// Store length (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%str-long.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			// Allocate buffer and memcpy
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), bufReg, lenReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 0, i8* %s)\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n", g.indent(), bufReg, ptrReg, lenReg))
			// Store data pointer (field 1)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str-long.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, dataGEP))
		}
		return strReg
	}

	// is-dir: 判斷路徑是否為目錄
	if fnName == "is-dir" && hasArgs {
		a := evalArgs()
		// 從 %str-long 參數提取 i8* 資料指針
		pathPtr := g.extractStrFromEvalArg(sb, a[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		modeGEP := fmt.Sprintf("%%stat.mode.%d", g.tmpIdx)
		g.tmpIdx++
		modeLoad := fmt.Sprintf("%%stat.mode.ld.%d", g.tmpIdx)
		g.tmpIdx++
		andReg := fmt.Sprintf("%%stat.and.%d", g.tmpIdx)
		g.tmpIdx++
		cmp2 := fmt.Sprintf("%%stat.cmp2.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%stat.ext.%d", g.tmpIdx)
		if sb != nil {
			// Allocate stat buffer (144 bytes on macOS arm64)
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 144\n", g.indent(), statBuf))
			// stat(path, &statbuf)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @stat(i8* %s, i8* %s)\n", g.indent(), statRet, pathPtr, statBuf))
			// Check stat return == 0
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			// Load st_mode (offset 16 on macOS arm64)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 16\n", g.indent(), modeGEP, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = load i16, i16* %s\n", g.indent(), modeLoad, modeGEP))
			// AND with S_IFDIR (0040000 = 0x4000)
			sb.WriteString(fmt.Sprintf("%s%s = and i16 %s, 16384\n", g.indent(), andReg, modeLoad))
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i16 %s, 0\n", g.indent(), cmp2, andReg))
			// AND with stat success check
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), extReg, cmpReg, cmp2))
			// zext to i64
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%stat.zext.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, extReg))
			return zextReg
		}
	}

	// stat-size / file-size: 獲取文件大小
	if (fnName == "stat-size" || fnName == "file-size") && hasArgs {
		a := evalArgs()
		// 從 %str-long 參數提取 i8* 資料指針
		pathPtr := g.extractStrFromEvalArg(sb, a[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		sizeGEP := fmt.Sprintf("%%stat.size.%d", g.tmpIdx)
		g.tmpIdx++
		sizeLoad := fmt.Sprintf("%%stat.size.ld.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%stat.sel.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 144\n", g.indent(), statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @stat(i8* %s, i8* %s)\n", g.indent(), statRet, pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 48\n", g.indent(), sizeGEP, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), sizeLoad, sizeGEP))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), selReg, cmpReg, sizeLoad))
		}
		return selReg
	}

	// get-line: 從標準輸入讀取一行
	if fnName == "get-line" {
		g.tmpIdx++
		bufReg := fmt.Sprintf("%%getline.buf.%d", g.tmpIdx)
		g.tmpIdx++
		stdinReg := fmt.Sprintf("%%getline.stdin.%d", g.tmpIdx)
		g.tmpIdx++
		fgetsReg := fmt.Sprintf("%%getline.fgets.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%getline.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%getline.len.%d", g.tmpIdx)
		g.tmpIdx++
		strReg := fmt.Sprintf("%%getline.str.%d", g.tmpIdx)
		if sb != nil {
			// Allocate 4096 byte buffer
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 4096\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 4096, i8* %s)\n", g.indent(), bufReg))
			// Get stdin (fopen with "r" on /dev/stdin or use stdin global)
			// On macOS, use fopen("/dev/stdin", "r")
			g.tmpIdx++
			stdinPath := fmt.Sprintf("%%getline.path.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca [11 x i8]\n", g.indent(), stdinPath))
			g.tmpIdx++
			pathGEP := fmt.Sprintf("%%getline.path.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [11 x i8], [11 x i8]* %s, i32 0, i32 0\n", g.indent(), pathGEP, stdinPath))
			// Store "/dev/stdin\0"
			sb.WriteString(fmt.Sprintf("%sstore [11 x i8] c\"/dev/stdin\\00\", [11 x i8]* %s\n", g.indent(), stdinPath))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @fopen(i8* %s, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.str.r, i64 0, i64 0))\n", g.indent(), stdinReg, pathGEP))
			// fgets(buf, 4096, stdin)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @fgets(i8* %s, i32 4096, i8* %s)\n", g.indent(), fgetsReg, bufReg, stdinReg))
			// Check if fgets returned NULL
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i8* %s, null\n", g.indent(), cmpReg, fgetsReg))
			// strlen of buffer
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @strlen(i8* %s)\n", g.indent(), lenReg, bufReg))
			// Create %str-long struct
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%str-long.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str-long.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), dataGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, dataGEP))
			// fclose
			sb.WriteString(fmt.Sprintf("%scall i32 @fclose(i8* %s)\n", g.indent(), stdinReg))
		}
		// 記錄 ok 值（cmpReg）供 curried 呼叫使用
		g.lastBuiltinExtra = cmpReg
		return strReg
	}

	if fnName == "gzip-compress" && hasArgs {
		a := evalArgs()

		// Extract data pointer and length from []byte (%vec) argument
		// %vec = { i64 len, i64 cap, i8* data }
		vecPtr := g.sliceEvalArgToPtr(sb, a[0])
		g.tmpIdx++
		dataLenGEP := fmt.Sprintf("%%gzip.c.datalen.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLen := fmt.Sprintf("%%gzip.c.datalen.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtrGEP := fmt.Sprintf("%%gzip.c.dataptr.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtr := fmt.Sprintf("%%gzip.c.dataptr.%d", g.tmpIdx)

		g.tmpIdx++
		bufReg := fmt.Sprintf("%%gzip.c.buf.%d", g.tmpIdx)
		g.tmpIdx++
		destLen := fmt.Sprintf("%%gzip.c.len.%d", g.tmpIdx)
		g.tmpIdx++
		destLenPtr := fmt.Sprintf("%%gzip.c.lenptr.%d", g.tmpIdx)
		g.tmpIdx++
		retReg := fmt.Sprintf("%%gzip.c.ret.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), dataLenGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataLen, dataLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataPtrGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), dataPtr, dataPtrGEP))

			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 4096\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 4096, i8* %s)\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), destLen))
			sb.WriteString(fmt.Sprintf("%sstore i64 4096, i64* %s\n", g.indent(), destLen))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i64* %s to i8*\n", g.indent(), destLenPtr, destLen))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @compress2(i8* %s, i8* %s, i8* %s, i64 %s, i32 9)\n",
				g.indent(), retReg, bufReg, destLenPtr, dataPtr, dataLen))

			// Build result %vec { len, cap, data }
			g.tmpIdx++
			vecReg := fmt.Sprintf("%%gzip.c.vec.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecReg))
			g.tmpIdx++
			vecLenGEP := fmt.Sprintf("%%gzip.c.veclen.gep.%d", g.tmpIdx)
			g.tmpIdx++
			vecLenLoad := fmt.Sprintf("%%gzip.c.veclen.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), vecLenGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), vecLenLoad, destLen))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), vecLenLoad, vecLenGEP))
			g.tmpIdx++
			vecCapGEP := fmt.Sprintf("%%gzip.c.veccap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), vecCapGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), vecLenLoad, vecCapGEP))
			g.tmpIdx++
			vecDataGEP := fmt.Sprintf("%%gzip.c.vecdata.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), vecDataGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, vecDataGEP))
			// Load the %vec value to return as SSA (callers expect a value, not a pointer)
			g.tmpIdx++
			vecVal := fmt.Sprintf("%%gzip.c.vec.val.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), vecVal, vecReg))
			if g.ssaTypes != nil {
				g.ssaTypes[vecVal] = "%vec"
			}
			return vecVal
		}
		return ""
	}

	if fnName == "gzip-decompress" && hasArgs {
		a := evalArgs()

		// Extract data pointer and length from []byte (%vec) argument
		// %vec = { i64 len, i64 cap, i8* data }
		vecPtr := g.sliceEvalArgToPtr(sb, a[0])
		g.tmpIdx++
		dataLenGEP := fmt.Sprintf("%%gzip.d.datalen.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLen := fmt.Sprintf("%%gzip.d.datalen.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtrGEP := fmt.Sprintf("%%gzip.d.dataptr.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtr := fmt.Sprintf("%%gzip.d.dataptr.%d", g.tmpIdx)

		g.tmpIdx++
		bufReg := fmt.Sprintf("%%gzip.d.buf.%d", g.tmpIdx)
		g.tmpIdx++
		destLen := fmt.Sprintf("%%gzip.d.len.%d", g.tmpIdx)
		g.tmpIdx++
		destLenPtr := fmt.Sprintf("%%gzip.d.lenptr.%d", g.tmpIdx)
		g.tmpIdx++
		retReg := fmt.Sprintf("%%gzip.d.ret.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), dataLenGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataLen, dataLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataPtrGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), dataPtr, dataPtrGEP))

			// Use malloc for output buffer (10 MB, large enough for typical tar.gz)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 10485760)\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), destLen))
			sb.WriteString(fmt.Sprintf("%sstore i64 10485760, i64* %s\n", g.indent(), destLen))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i64* %s to i8*\n", g.indent(), destLenPtr, destLen))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @uncompress(i8* %s, i8* %s, i8* %s, i64 %s)\n",
				g.indent(), retReg, bufReg, destLenPtr, dataPtr, dataLen))

			// Build result %vec { len, cap, data }
			g.tmpIdx++
			vecReg := fmt.Sprintf("%%gzip.d.vec.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecReg))
			g.tmpIdx++
			vecLenGEP := fmt.Sprintf("%%gzip.d.veclen.gep.%d", g.tmpIdx)
			g.tmpIdx++
			vecLenLoad := fmt.Sprintf("%%gzip.d.veclen.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), vecLenGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), vecLenLoad, destLen))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), vecLenLoad, vecLenGEP))
			g.tmpIdx++
			vecCapGEP := fmt.Sprintf("%%gzip.d.veccap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), vecCapGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 10485760, i64* %s\n", g.indent(), vecCapGEP))
			g.tmpIdx++
			vecDataGEP := fmt.Sprintf("%%gzip.d.vecdata.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), vecDataGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, vecDataGEP))
			// Load the %vec value to return as SSA (callers expect a value, not a pointer)
			g.tmpIdx++
			vecVal := fmt.Sprintf("%%gzip.d.vec.val.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), vecVal, vecReg))
			if g.ssaTypes != nil {
				g.ssaTypes[vecVal] = "%vec"
			}
			return vecVal
		}
		return ""
	}

	if fnName == "inflate-decompress" && hasArgs {
		a := evalArgs()

		// Extract data pointer and length from []byte (%vec) argument
		vecPtr := g.sliceEvalArgToPtr(sb, a[0])
		g.tmpIdx++
		dataLenGEP := fmt.Sprintf("%%inflate.d.datalen.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataLen := fmt.Sprintf("%%inflate.d.datalen.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtrGEP := fmt.Sprintf("%%inflate.d.dataptr.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtr := fmt.Sprintf("%%inflate.d.dataptr.%d", g.tmpIdx)

		// out_size is the second argument (i64)
		outSize := a[1]

		g.tmpIdx++
		bufReg := fmt.Sprintf("%%inflate.d.buf.%d", g.tmpIdx)
		g.tmpIdx++
		writtenAlloca := fmt.Sprintf("%%inflate.d.written.%d", g.tmpIdx)
		g.tmpIdx++
		retReg := fmt.Sprintf("%%inflate.d.ret.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), dataLenGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), dataLen, dataLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataPtrGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), dataPtr, dataPtrGEP))

			// Allocate output buffer via malloc (heap, since size can be large)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, outSize))
			// Allocate written counter
			sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), writtenAlloca))
			// Call nolang.inflate_raw(in, in_len, out, out_len, &written)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.inflate_raw(i8* %s, i64 %s, i8* %s, i64 %s, i64* %s)\n",
				g.indent(), retReg, dataPtr, dataLen, bufReg, outSize, writtenAlloca))

			// Build result %vec { len, cap, data }
			g.tmpIdx++
			vecReg := fmt.Sprintf("%%inflate.d.vec.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), vecReg))
			g.tmpIdx++
			vecLenGEP := fmt.Sprintf("%%inflate.d.veclen.gep.%d", g.tmpIdx)
			g.tmpIdx++
			vecLenLoad := fmt.Sprintf("%%inflate.d.veclen.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), vecLenGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), vecLenLoad, writtenAlloca))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), vecLenLoad, vecLenGEP))
			g.tmpIdx++
			vecCapGEP := fmt.Sprintf("%%inflate.d.veccap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), vecCapGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), outSize, vecCapGEP))
			g.tmpIdx++
			vecDataGEP := fmt.Sprintf("%%inflate.d.vecdata.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), vecDataGEP, vecReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, vecDataGEP))
			// Load the %vec value to return as SSA (callers expect a value, not a pointer)
			g.tmpIdx++
			vecVal := fmt.Sprintf("%%inflate.d.vec.val.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), vecVal, vecReg))
			if g.ssaTypes != nil {
				g.ssaTypes[vecVal] = "%vec"
			}
			return vecVal
		}
		return ""
	}

	if fnName == "regexp-match" && hasArgs {
		a := evalArgs()
		patternPtr := g.extractStrFromEvalArg(sb, a[0])
		textPtr := g.extractStrFromEvalArg(sb, a[1])

		g.tmpIdx++
		preg := fmt.Sprintf("%%regexp.m.preg.%d", g.tmpIdx)
		g.tmpIdx++
		execRet := fmt.Sprintf("%%regexp.m.exec.%d", g.tmpIdx)
		g.tmpIdx++
		matchCmp := fmt.Sprintf("%%regexp.m.match.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		match := fmt.Sprintf("%%regexp.m.match.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 72\n", g.indent(), preg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 72, i8* %s)\n", g.indent(), preg))
			sb.WriteString(fmt.Sprintf("%scall i32 @regcomp(i8* %s, i8* %s, i32 0)\n", g.indent(), preg, patternPtr))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @regexec(i8* %s, i8* %s, i32 0, i8* null, i32 0)\n", g.indent(), execRet, preg, textPtr))
			sb.WriteString(fmt.Sprintf("%scall void @regfree(i8* %s)\n", g.indent(), preg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), matchCmp, execRet))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), match, matchCmp))
		}
		return match
	}

	if fnName == "regexp-find" && hasArgs {
		a := evalArgs()
		patternPtr := g.extractStrFromEvalArg(sb, a[0])
		textPtr := g.extractStrFromEvalArg(sb, a[1])

		g.tmpIdx++
		preg := fmt.Sprintf("%%regexp.f.preg.%d", g.tmpIdx)
		g.tmpIdx++
		pmatch := fmt.Sprintf("%%regexp.f.pmatch.%d", g.tmpIdx)
		g.tmpIdx++
		execRet := fmt.Sprintf("%%regexp.f.exec.%d", g.tmpIdx)
		g.tmpIdx++
		start := fmt.Sprintf("%%regexp.f.start.%d", g.tmpIdx)
		g.tmpIdx++
		end := fmt.Sprintf("%%regexp.f.end.%d", g.tmpIdx)
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%regexp.f.len.%d", g.tmpIdx)
		g.tmpIdx++
		strReg := fmt.Sprintf("%%regexp.f.str.%d", g.tmpIdx)
		g.tmpIdx++
		bufReg := fmt.Sprintf("%%regexp.f.buf.%d", g.tmpIdx)
		g.tmpIdx++
		matchCmp := fmt.Sprintf("%%regexp.f.cmp.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 72\n", g.indent(), preg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 72, i8* %s)\n", g.indent(), preg))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 16\n", g.indent(), pmatch))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 16, i8* %s)\n", g.indent(), pmatch))
			sb.WriteString(fmt.Sprintf("%scall i32 @regcomp(i8* %s, i8* %s, i32 0)\n", g.indent(), preg, patternPtr))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @regexec(i8* %s, i8* %s, i32 1, i8* %s, i32 0)\n", g.indent(), execRet, preg, textPtr, pmatch))
			sb.WriteString(fmt.Sprintf("%scall void @regfree(i8* %s)\n", g.indent(), preg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), matchCmp, execRet))

			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			g.tmpIdx++
			strLenGEP := fmt.Sprintf("%%regexp.f.strlen.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), strLenGEP, strReg))
			g.tmpIdx++
			strDataGEP := fmt.Sprintf("%%regexp.f.strdata.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), strDataGEP, strReg))

			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%regexp.f.match, label %%regexp.f.no_match\n", g.indent(), matchCmp))
			sb.WriteString(fmt.Sprintf("regexp.f.match:\n"))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), start, pmatch))
			g.tmpIdx++
			endGEP := fmt.Sprintf("%%regexp.f.endgep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 8\n", g.indent(), endGEP, pmatch))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), end, endGEP))
			g.tmpIdx++
			lenI32 := fmt.Sprintf("%%regexp.f.leni32.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = sub nsw i32 %s, %s\n", g.indent(), lenI32, end, start))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), lenReg, lenI32))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, strLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), bufReg, lenReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 0, i8* %s)\n", g.indent(), bufReg))
			g.tmpIdx++
			startI64 := fmt.Sprintf("%%regexp.f.starti64.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), startI64, start))
			g.tmpIdx++
			textStart := fmt.Sprintf("%%regexp.f.txtstart.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), textStart, textPtr, startI64))
			sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n", g.indent(), bufReg, textStart, lenReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, strDataGEP))
			sb.WriteString(fmt.Sprintf("%sbr label %%regexp.f.end\n", g.indent()))

			sb.WriteString(fmt.Sprintf("regexp.f.no_match:\n"))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), strLenGEP))
			sb.WriteString(fmt.Sprintf("%sstore i8* null, i8** %s\n", g.indent(), strDataGEP))
			sb.WriteString(fmt.Sprintf("%sbr label %%regexp.f.end\n", g.indent()))

			sb.WriteString(fmt.Sprintf("regexp.f.end:\n"))
		}
		// strReg 是 %str-long* (alloca)，需 load 成 %str-long value 以便常規賦值路徑使用
		g.tmpIdx++
		strVal := fmt.Sprintf("%%regexp.f.strval.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), strVal, strReg))
		return strVal
	}

	// ═══════════════════════════════════════════════
	// process — 進程操作
	// ═══════════════════════════════════════════════

	// process-fork: fork current process
	// Returns: 0=child, >0=parent(child_pid), -1=error
	if fnName == "process-fork" {
		g.tmpIdx++
		forkRet := fmt.Sprintf("%%proc.fork.ret.%d", g.tmpIdx)
		g.tmpIdx++
		forkExt := fmt.Sprintf("%%proc.fork.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @fork()\n", g.indent(), forkRet))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), forkExt, forkRet))
		}
		return forkExt
	}

	// process-pipe: create a pipe
	// Returns packed i64: (read_fd << 32) | write_fd
	if fnName == "process-pipe" {
		g.tmpIdx++
		pipeFds := fmt.Sprintf("%%proc.pipe.fds.%d", g.tmpIdx)
		g.tmpIdx++
		pipeRet := fmt.Sprintf("%%proc.pipe.ret.%d", g.tmpIdx)
		g.tmpIdx++
		pipeGep0 := fmt.Sprintf("%%proc.pipe.gep0.%d", g.tmpIdx)
		g.tmpIdx++
		pipeFd0 := fmt.Sprintf("%%proc.pipe.fd0.%d", g.tmpIdx)
		g.tmpIdx++
		pipeGep1 := fmt.Sprintf("%%proc.pipe.gep1.%d", g.tmpIdx)
		g.tmpIdx++
		pipeFd1 := fmt.Sprintf("%%proc.pipe.fd1.%d", g.tmpIdx)
		g.tmpIdx++
		pipeExt0 := fmt.Sprintf("%%proc.pipe.ext0.%d", g.tmpIdx)
		g.tmpIdx++
		pipeExt1 := fmt.Sprintf("%%proc.pipe.ext1.%d", g.tmpIdx)
		g.tmpIdx++
		pipeShl := fmt.Sprintf("%%proc.pipe.shl.%d", g.tmpIdx)
		g.tmpIdx++
		pipePack := fmt.Sprintf("%%proc.pipe.pack.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca [2 x i32]\n", g.indent(), pipeFds))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @pipe(i32* %s)\n", g.indent(), pipeRet, pipeFds))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [2 x i32], [2 x i32]* %s, i64 0, i64 0\n", g.indent(), pipeGep0, pipeFds))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), pipeFd0, pipeGep0))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [2 x i32], [2 x i32]* %s, i64 0, i64 1\n", g.indent(), pipeGep1, pipeFds))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), pipeFd1, pipeGep1))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), pipeExt0, pipeFd0))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), pipeExt1, pipeFd1))
			sb.WriteString(fmt.Sprintf("%s%s = shl i64 %s, 32\n", g.indent(), pipeShl, pipeExt0))
			sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %s\n", g.indent(), pipePack, pipeShl, pipeExt1))
		}
		return pipePack
	}

	// process-waitpid: wait for child process
	// Args: pid i64, options i64
	// Returns exit code (WEXITSTATUS: (status >> 8) & 0xFF)
	if fnName == "process-waitpid" && hasArgs && nArgs >= 2 {
		a := evalArgs()
		pidVal := a[0]
		optVal := a[1]
		g.tmpIdx++
		waitStatus := fmt.Sprintf("%%proc.wait.status.%d", g.tmpIdx)
		g.tmpIdx++
		waitPidTrunc := fmt.Sprintf("%%proc.wait.pid.%d", g.tmpIdx)
		g.tmpIdx++
		waitOptTrunc := fmt.Sprintf("%%proc.wait.opt.%d", g.tmpIdx)
		g.tmpIdx++
		waitRet := fmt.Sprintf("%%proc.wait.ret.%d", g.tmpIdx)
		g.tmpIdx++
		waitLd := fmt.Sprintf("%%proc.wait.ld.%d", g.tmpIdx)
		g.tmpIdx++
		waitShift := fmt.Sprintf("%%proc.wait.shift.%d", g.tmpIdx)
		g.tmpIdx++
		waitCode := fmt.Sprintf("%%proc.wait.code.%d", g.tmpIdx)
		g.tmpIdx++
		waitExt := fmt.Sprintf("%%proc.wait.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i32\n", g.indent(), waitStatus))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), waitPidTrunc, pidVal))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), waitOptTrunc, optVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @waitpid(i32 %s, i32* %s, i32 %s)\n", g.indent(), waitRet, waitPidTrunc, waitStatus, waitOptTrunc))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), waitLd, waitStatus))
			sb.WriteString(fmt.Sprintf("%s%s = lshr i32 %s, 8\n", g.indent(), waitShift, waitLd))
			sb.WriteString(fmt.Sprintf("%s%s = and i32 %s, 255\n", g.indent(), waitCode, waitShift))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), waitExt, waitCode))
		}
		return waitExt
	}

	// process-exec: replace current process with new program
	// Args: program str, arg str
	// Calls execlp(program, program, arg, NULL)
	// Returns only on failure (errno via __errno or -1)
	if fnName == "process-exec" && hasArgs && nArgs >= 2 {
		progPtr := g.makeNullTerminatedStr(sb, expr.Arguments[0])
		argPtr := g.makeNullTerminatedStr(sb, expr.Arguments[1])
		g.tmpIdx++
		execRet := fmt.Sprintf("%%proc.exec.ret.%d", g.tmpIdx)
		g.tmpIdx++
		execExt := fmt.Sprintf("%%proc.exec.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 (i8*, ...) @execlp(i8* %s, i8* %s, i8* %s, i8* null)\n", g.indent(), execRet, progPtr, progPtr, argPtr))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), execExt, execRet))
		}
		return execExt
	}

	// ═══════════════════════════════════════════════
	// net — 網路操作
	// ═══════════════════════════════════════════════

	// net-listen: create TCP listening socket
	// Performs: socket + setsockopt(SO_REUSEADDR) + bind + listen
	// Args: host str, port i64
	// Returns: fd i64 (-1 on error)
	if fnName == "net-listen" && hasArgs && nArgs >= 2 {
		a := evalArgs()
		hostPtr := g.makeNullTerminatedStr(sb, expr.Arguments[0])
		portVal := a[1]

		// Allocate sockaddr_in (16 bytes) and zero it
		g.tmpIdx++
		addrReg := fmt.Sprintf("%%net.l.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%net.l.addrptr.%d", g.tmpIdx)

		// sin_family at offset 1 (macOS: sin_len=0, sin_family=1)
		g.tmpIdx++
		famGep := fmt.Sprintf("%%net.l.fam.%d", g.tmpIdx)

		// htons(port): (port & 0xFF) << 8 | (port >> 8) & 0xFF
		g.tmpIdx++
		portLo := fmt.Sprintf("%%net.l.plo.%d", g.tmpIdx)
		g.tmpIdx++
		portHi := fmt.Sprintf("%%net.l.phi.%d", g.tmpIdx)
		g.tmpIdx++
		portHiM := fmt.Sprintf("%%net.l.phm.%d", g.tmpIdx)
		g.tmpIdx++
		portSl := fmt.Sprintf("%%net.l.psl.%d", g.tmpIdx)
		g.tmpIdx++
		portNet := fmt.Sprintf("%%net.l.pnet.%d", g.tmpIdx)
		g.tmpIdx++
		portI16 := fmt.Sprintf("%%net.l.pi16.%d", g.tmpIdx)

		// sin_port at offset 2
		g.tmpIdx++
		portGep := fmt.Sprintf("%%net.l.portgep.%d", g.tmpIdx)
		g.tmpIdx++
		portCast := fmt.Sprintf("%%net.l.portcast.%d", g.tmpIdx)

		// inet_pton(AF_INET=2, host, &sin_addr at offset 4)
		g.tmpIdx++
		addrInGep := fmt.Sprintf("%%net.l.addringe.%d", g.tmpIdx)
		g.tmpIdx++
		ptonRet := fmt.Sprintf("%%net.l.pton.%d", g.tmpIdx)
		g.tmpIdx++
		ptonOk := fmt.Sprintf("%%net.l.ptonok.%d", g.tmpIdx)

		// socket(AF_INET=2, SOCK_STREAM=1, 0)
		g.tmpIdx++
		sockFd := fmt.Sprintf("%%net.l.sock.%d", g.tmpIdx)
		g.tmpIdx++
		sockOk := fmt.Sprintf("%%net.l.sockok.%d", g.tmpIdx)

		// setsockopt(fd, SOL_SOCKET=65535, SO_REUSEADDR=4, &val, 4)
		g.tmpIdx++
		reuseAlloca := fmt.Sprintf("%%net.l.reuse.%d", g.tmpIdx)
		g.tmpIdx++
		reusePtr := fmt.Sprintf("%%net.l.reuseptr.%d", g.tmpIdx)

		// bind(fd, &addr, 16)
		g.tmpIdx++
		bindRet := fmt.Sprintf("%%net.l.bind.%d", g.tmpIdx)
		g.tmpIdx++
		bindOk := fmt.Sprintf("%%net.l.bindok.%d", g.tmpIdx)

		// listen(fd, 128)
		g.tmpIdx++
		listenRet := fmt.Sprintf("%%net.l.listen.%d", g.tmpIdx)
		g.tmpIdx++
		listenOk := fmt.Sprintf("%%net.l.listenok.%d", g.tmpIdx)

		// fd as i64
		g.tmpIdx++
		fdExt := fmt.Sprintf("%%net.l.fdext.%d", g.tmpIdx)

		// all.ok = pton.ok AND sock.ok AND bind.ok AND listen.ok
		g.tmpIdx++
		ok1 := fmt.Sprintf("%%net.l.ok1.%d", g.tmpIdx)
		g.tmpIdx++
		ok2 := fmt.Sprintf("%%net.l.ok2.%d", g.tmpIdx)
		g.tmpIdx++
		ok3 := fmt.Sprintf("%%net.l.ok3.%d", g.tmpIdx)

		// result = select all.ok ? fd : -1
		g.tmpIdx++
		resultReg := fmt.Sprintf("%%net.l.result.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca [16 x i8]\n", g.indent(), addrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrReg))
			sb.WriteString(fmt.Sprintf("%scall i8* @memset(i8* %s, i32 0, i64 16)\n", g.indent(), addrPtr))

			// sin_family = AF_INET (2)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 1\n", g.indent(), famGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%sstore i8 2, i8* %s\n", g.indent(), famGep))

			// htons(port)
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, 255\n", g.indent(), portLo, portVal))
			sb.WriteString(fmt.Sprintf("%s%s = lshr i64 %s, 8\n", g.indent(), portHi, portVal))
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, 255\n", g.indent(), portHiM, portHi))
			sb.WriteString(fmt.Sprintf("%s%s = shl i64 %s, 8\n", g.indent(), portSl, portLo))
			sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %s\n", g.indent(), portNet, portSl, portHiM))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i16\n", g.indent(), portI16, portNet))

			// sin_port at offset 2
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 2\n", g.indent(), portGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i16*\n", g.indent(), portCast, portGep))
			sb.WriteString(fmt.Sprintf("%sstore i16 %s, i16* %s\n", g.indent(), portI16, portCast))

			// inet_pton(AF_INET=2, host, &sin_addr at offset 4)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), addrInGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @inet_pton(i32 2, i8* %s, i8* %s)\n", g.indent(), ptonRet, hostPtr, addrInGep))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 1\n", g.indent(), ptonOk, ptonRet))

			// socket(AF_INET=2, SOCK_STREAM=1, 0)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 2, i32 1, i32 0)\n", g.indent(), sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), sockOk, sockFd))

			// setsockopt(fd, SOL_SOCKET=65535, SO_REUSEADDR=4, &val=1, 4)
			sb.WriteString(fmt.Sprintf("%s%s = alloca i32\n", g.indent(), reuseAlloca))
			sb.WriteString(fmt.Sprintf("%sstore i32 1, i32* %s\n", g.indent(), reuseAlloca))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i32* %s to i8*\n", g.indent(), reusePtr, reuseAlloca))
			sb.WriteString(fmt.Sprintf("%scall i32 @setsockopt(i32 %s, i32 65535, i32 4, i8* %s, i32 4)\n", g.indent(), sockFd, reusePtr))

			// bind(fd, &addr, 16)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @bind(i32 %s, i8* %s, i32 16)\n", g.indent(), bindRet, sockFd, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), bindOk, bindRet))

			// listen(fd, 128)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @listen(i32 %s, i32 128)\n", g.indent(), listenRet, sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), listenOk, listenRet))

			// fd as i64
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))

			// all.ok
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), ok1, ptonOk, sockOk))
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), ok2, ok1, bindOk))
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), ok3, ok2, listenOk))

			// result
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 -1\n", g.indent(), resultReg, ok3, fdExt))
		}
		return resultReg
	}

	// net-dial: create TCP client connection
	// Performs: socket + connect
	// Args: host str (IP literal or hostname), port i64
	// Returns: fd i64 (-1 on error)
	// DNS fallback: if inet_pton fails (host is a hostname), use getaddrinfo
	if fnName == "net-dial" && hasArgs && nArgs >= 2 {
		a := evalArgs()
		hostPtr := g.makeNullTerminatedStr(sb, expr.Arguments[0])
		portVal := a[1]

		// Allocate sockaddr_in (16 bytes) and zero it
		g.tmpIdx++
		addrReg := fmt.Sprintf("%%net.d.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%net.d.addrptr.%d", g.tmpIdx)

		// sin_family at offset 1
		g.tmpIdx++
		famGep := fmt.Sprintf("%%net.d.fam.%d", g.tmpIdx)

		// htons(port)
		g.tmpIdx++
		portLo := fmt.Sprintf("%%net.d.plo.%d", g.tmpIdx)
		g.tmpIdx++
		portHi := fmt.Sprintf("%%net.d.phi.%d", g.tmpIdx)
		g.tmpIdx++
		portHiM := fmt.Sprintf("%%net.d.phm.%d", g.tmpIdx)
		g.tmpIdx++
		portSl := fmt.Sprintf("%%net.d.psl.%d", g.tmpIdx)
		g.tmpIdx++
		portNet := fmt.Sprintf("%%net.d.pnet.%d", g.tmpIdx)
		g.tmpIdx++
		portI16 := fmt.Sprintf("%%net.d.pi16.%d", g.tmpIdx)

		// sin_port at offset 2
		g.tmpIdx++
		portGep := fmt.Sprintf("%%net.d.portgep.%d", g.tmpIdx)
		g.tmpIdx++
		portCast := fmt.Sprintf("%%net.d.portcast.%d", g.tmpIdx)

		// inet_pton
		g.tmpIdx++
		addrInGep := fmt.Sprintf("%%net.d.addringe.%d", g.tmpIdx)
		g.tmpIdx++
		ptonRet := fmt.Sprintf("%%net.d.pton.%d", g.tmpIdx)
		g.tmpIdx++
		ptonOk := fmt.Sprintf("%%net.d.ptonok.%d", g.tmpIdx)

		// getaddrinfo fallback registers
		g.tmpIdx++
		hintsReg := fmt.Sprintf("%%net.d.hints.%d", g.tmpIdx)
		g.tmpIdx++
		hintsPtr := fmt.Sprintf("%%net.d.hintsptr.%d", g.tmpIdx)
		g.tmpIdx++
		hintsFamGep := fmt.Sprintf("%%net.d.hintsfam.%d", g.tmpIdx)
		g.tmpIdx++
		hintsFamCast := fmt.Sprintf("%%net.d.hintsfamc.%d", g.tmpIdx)
		g.tmpIdx++
		hintsStGep := fmt.Sprintf("%%net.d.hintsst.%d", g.tmpIdx)
		g.tmpIdx++
		hintsStCast := fmt.Sprintf("%%net.d.hintsstc.%d", g.tmpIdx)
		g.tmpIdx++
		resReg := fmt.Sprintf("%%net.d.res.%d", g.tmpIdx)
		g.tmpIdx++
		gaiRet := fmt.Sprintf("%%net.d.gai.%d", g.tmpIdx)
		g.tmpIdx++
		gaiOk := fmt.Sprintf("%%net.d.gaiok.%d", g.tmpIdx)
		g.tmpIdx++
		resVal := fmt.Sprintf("%%net.d.resval.%d", g.tmpIdx)
		g.tmpIdx++
		aiAddrGep := fmt.Sprintf("%%net.d.aiaddrgep.%d", g.tmpIdx)
		g.tmpIdx++
		aiAddrCast := fmt.Sprintf("%%net.d.aiaddrcast.%d", g.tmpIdx)
		g.tmpIdx++
		aiAddr := fmt.Sprintf("%%net.d.aiaddr.%d", g.tmpIdx)

		// socket
		g.tmpIdx++
		sockFd := fmt.Sprintf("%%net.d.sock.%d", g.tmpIdx)
		g.tmpIdx++
		sockOk := fmt.Sprintf("%%net.d.sockok.%d", g.tmpIdx)

		// connect
		g.tmpIdx++
		connRet := fmt.Sprintf("%%net.d.conn.%d", g.tmpIdx)
		g.tmpIdx++
		connOk := fmt.Sprintf("%%net.d.connok.%d", g.tmpIdx)

		// fd as i64
		g.tmpIdx++
		fdExt := fmt.Sprintf("%%net.d.fdext.%d", g.tmpIdx)

		// sock.ok AND conn.ok
		g.tmpIdx++
		sockConnOk := fmt.Sprintf("%%net.d.sockconnok.%d", g.tmpIdx)

		// phi nodes at merge
		g.tmpIdx++
		finalOk := fmt.Sprintf("%%net.d.finalok.%d", g.tmpIdx)
		g.tmpIdx++
		finalFd := fmt.Sprintf("%%net.d.finalfd.%d", g.tmpIdx)

		// result
		g.tmpIdx++
		resultReg := fmt.Sprintf("%%net.d.result.%d", g.tmpIdx)

		if sb != nil {
			// Basic block labels
			tryResolve := fmt.Sprintf("net.d.tryresolve.%d", g.tmpIdx)
			useResolved := fmt.Sprintf("net.d.useresolved.%d", g.tmpIdx)
			doSocket := fmt.Sprintf("net.d.dosocket.%d", g.tmpIdx)
			failLabel := fmt.Sprintf("net.d.fail.%d", g.tmpIdx)
			mergeLabel := fmt.Sprintf("net.d.merge.%d", g.tmpIdx)

			sb.WriteString(fmt.Sprintf("%s%s = alloca [16 x i8]\n", g.indent(), addrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrReg))
			sb.WriteString(fmt.Sprintf("%scall i8* @memset(i8* %s, i32 0, i64 16)\n", g.indent(), addrPtr))

			// sin_family = AF_INET (2)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 1\n", g.indent(), famGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%sstore i8 2, i8* %s\n", g.indent(), famGep))

			// htons(port)
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, 255\n", g.indent(), portLo, portVal))
			sb.WriteString(fmt.Sprintf("%s%s = lshr i64 %s, 8\n", g.indent(), portHi, portVal))
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, 255\n", g.indent(), portHiM, portHi))
			sb.WriteString(fmt.Sprintf("%s%s = shl i64 %s, 8\n", g.indent(), portSl, portLo))
			sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %s\n", g.indent(), portNet, portSl, portHiM))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i16\n", g.indent(), portI16, portNet))

			// sin_port at offset 2
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 2\n", g.indent(), portGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i16*\n", g.indent(), portCast, portGep))
			sb.WriteString(fmt.Sprintf("%sstore i16 %s, i16* %s\n", g.indent(), portI16, portCast))

			// inet_pton(AF_INET=2, host, &sin_addr at offset 4)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), addrInGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @inet_pton(i32 2, i8* %s, i8* %s)\n", g.indent(), ptonRet, hostPtr, addrInGep))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 1\n", g.indent(), ptonOk, ptonRet))

			// Branch: if inet_pton succeeded, go to do_socket; else try DNS resolve
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), ptonOk, doSocket, tryResolve))

			// try_resolve: call getaddrinfo
			sb.WriteString(fmt.Sprintf("%s:\n", tryResolve))
			sb.WriteString(fmt.Sprintf("%s%s = alloca [48 x i8]\n", g.indent(), hintsReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [48 x i8], [48 x i8]* %s, i64 0, i64 0\n", g.indent(), hintsPtr, hintsReg))
			sb.WriteString(fmt.Sprintf("%scall i8* @memset(i8* %s, i32 0, i64 48)\n", g.indent(), hintsPtr))
			// hints.ai_family = AF_INET (2) at offset 4
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), hintsFamGep, hintsPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n", g.indent(), hintsFamCast, hintsFamGep))
			sb.WriteString(fmt.Sprintf("%sstore i32 2, i32* %s\n", g.indent(), hintsFamCast))
			// hints.ai_socktype = SOCK_STREAM (1) at offset 8
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 8\n", g.indent(), hintsStGep, hintsPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n", g.indent(), hintsStCast, hintsStGep))
			sb.WriteString(fmt.Sprintf("%sstore i32 1, i32* %s\n", g.indent(), hintsStCast))
			// allocate result pointer
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8*\n", g.indent(), resReg))
			// getaddrinfo(host, NULL, &hints, &res)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @getaddrinfo(i8* %s, i8* null, i8* %s, i8** %s)\n", g.indent(), gaiRet, hostPtr, hintsPtr, resReg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), gaiOk, gaiRet))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), gaiOk, useResolved, failLabel))

			// use_resolved: copy ai_addr to our sockaddr_in
			sb.WriteString(fmt.Sprintf("%s:\n", useResolved))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), resVal, resReg))
			// macOS addrinfo layout: ai_addr at offset 32
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 32\n", g.indent(), aiAddrGep, resVal))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8**\n", g.indent(), aiAddrCast, aiAddrGep))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), aiAddr, aiAddrCast))
			// copy 16 bytes from ai_addr to our sockaddr_in
			sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 16)\n", g.indent(), addrPtr, aiAddr))
			// freeaddrinfo
			sb.WriteString(fmt.Sprintf("%scall void @freeaddrinfo(i8* %s)\n", g.indent(), resVal))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), doSocket))

			// do_socket: socket + connect
			sb.WriteString(fmt.Sprintf("%s:\n", doSocket))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 2, i32 1, i32 0)\n", g.indent(), sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), sockOk, sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @connect(i32 %s, i8* %s, i32 16)\n", g.indent(), connRet, sockFd, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), connOk, connRet))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), sockConnOk, sockOk, connOk))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// fail: result = -1
			sb.WriteString(fmt.Sprintf("%s:\n", failLabel))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// merge: phi to determine final result
			sb.WriteString(fmt.Sprintf("%s:\n", mergeLabel))
			sb.WriteString(fmt.Sprintf("%s%s = phi i1 [ false, %%%s ], [ %s, %%%s ]\n", g.indent(), finalOk, failLabel, sockConnOk, doSocket))
			sb.WriteString(fmt.Sprintf("%s%s = phi i64 [ -1, %%%s ], [ %s, %%%s ]\n", g.indent(), finalFd, failLabel, fdExt, doSocket))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 -1\n", g.indent(), resultReg, finalOk, finalFd))
		}
		return resultReg
	}

	// net-accept: accept TCP connection
	// Args: listen-fd i64
	// Returns: fd i64 (-1 on error)
	if fnName == "net-accept" && hasArgs && nArgs >= 1 {
		a := evalArgs()
		listenFd := a[0]

		g.tmpIdx++
		addrBuf := fmt.Sprintf("%%net.a.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%net.a.addrptr.%d", g.tmpIdx)
		g.tmpIdx++
		lenAlloca := fmt.Sprintf("%%net.a.len.%d", g.tmpIdx)
		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%net.a.fdtrunc.%d", g.tmpIdx)
		g.tmpIdx++
		acceptRet := fmt.Sprintf("%%net.a.ret.%d", g.tmpIdx)
		g.tmpIdx++
		acceptExt := fmt.Sprintf("%%net.a.ext.%d", g.tmpIdx)

		if sb != nil {
			// Allocate sockaddr storage (16 bytes) and addrlen
			sb.WriteString(fmt.Sprintf("%s%s = alloca [16 x i8]\n", g.indent(), addrBuf))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrBuf))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i32\n", g.indent(), lenAlloca))
			sb.WriteString(fmt.Sprintf("%sstore i32 16, i32* %s\n", g.indent(), lenAlloca))

			// trunc listen-fd to i32
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, listenFd))

			// accept(listen-fd, &addr, &addrlen)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @accept(i32 %s, i8* %s, i32* %s)\n", g.indent(), acceptRet, fdTrunc, addrPtr, lenAlloca))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), acceptExt, acceptRet))
		}
		return acceptExt
	}

	// net-send: send data on connected socket
	// Args: fd i64, data str, n i64
	// Returns: written i64 (-1 on error)
	if fnName == "net-send" && hasArgs && nArgs >= 3 {
		a := evalArgs()
		fdVal := a[0]
		dataPtr := g.extractStrFromEvalArg(sb, a[1])
		nVal := a[2]

		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%net.s.fdtrunc.%d", g.tmpIdx)
		g.tmpIdx++
		sendRet := fmt.Sprintf("%%net.s.ret.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, fdVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @send(i32 %s, i8* %s, i64 %s, i32 0)\n", g.indent(), sendRet, fdTrunc, dataPtr, nVal))
		}
		return sendRet
	}

	// net-recv: receive data on connected socket
	// Args: fd i64, buf str, n i64
	// Returns: read-n i64 (-1 on error, 0 on connection closed)
	if fnName == "net-recv" && hasArgs && nArgs >= 3 {
		a := evalArgs()
		fdVal := a[0]
		bufPtr := g.extractStrFromEvalArg(sb, a[1])
		nVal := a[2]

		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%net.r.fdtrunc.%d", g.tmpIdx)
		g.tmpIdx++
		recvRet := fmt.Sprintf("%%net.r.ret.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, fdVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @recv(i32 %s, i8* %s, i64 %s, i32 0)\n", g.indent(), recvRet, fdTrunc, bufPtr, nVal))
		}
		return recvRet
	}

	// TLS builtins removed — TLS is now implemented in pure Nolang (std/net/tls.no).
	// No OpenSSL dependency required.

	return ""
}

// makeNullTerminatedStr generates LLVM IR to create a null-terminated copy of a string expression.
func (g *Generator) makeNullTerminatedStr(sb *strings.Builder, expr parser.Expression) string {
	var dataPtr string
	strLen := g.strLenFromExpr(sb, expr)

	switch a := expr.(type) {
	case *parser.Identifier:
		if g.varTypes != nil {
			if t, ok := g.varTypes[a.Value]; ok {
				if t == "%str-long" {
					dataPtr = g.extractStrDataPtr(sb, "%"+a.Value)
				} else if t == "%str-short" {
					dataPtr = g.extractStrShortDataPtr(sb, "%"+a.Value)
				}
			}
		}
	case *parser.StringLiteral:
		ptr := g.generateExprWithSB(sb, a)
		if len(a.Value) <= 127 {
			dataPtr = g.extractStrShortDataPtr(sb, ptr)
		} else {
			dataPtr = g.extractStrDataPtr(sb, ptr)
		}
	case *parser.InfixExpression:
		if (a.Operator == "-" || a.Operator == "+") && (g.isStringExpr(a.Left) || g.isStringExpr(a.Right)) {
			ptr := g.generateStrConcat(sb, a.Left, a.Right)
			dataPtr = g.extractStrDataPtr(sb, ptr)
		}
	}

	if dataPtr == "" {
		return dataPtr
	}

	g.tmpIdx++
	sizeReg := fmt.Sprintf("%%str-longnull.size.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), sizeReg, strLen))

	g.tmpIdx++
	buf := fmt.Sprintf("%%str-longnull.buf.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), buf, sizeReg))

	g.tmpIdx++
	nullEnd := fmt.Sprintf("%%str-longnull.end.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), nullEnd, buf, strLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullEnd))

	sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n", g.indent(), buf, dataPtr, strLen))

	return buf
}
