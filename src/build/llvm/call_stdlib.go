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
					if t == "%str" {
						return g.extractStrDataPtr(sb, "%"+a.Value)
					}
					if t == "%str-smail" {
						return g.extractStrSmailDataPtr(sb, "%"+a.Value)
					}
				}
			}
		case *parser.StringLiteral:
			ptr := g.generateExprWithSB(sb, a)
			if len(a.Value) <= 127 {
				return g.extractStrSmailDataPtr(sb, ptr)
			}
			return g.extractStrDataPtr(sb, ptr)
		case *parser.InfixExpression:
			if a.Operator == "-" && (g.isStringExpr(a.Left) || g.isStringExpr(a.Right)) {
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
			if i > 0 {
				fg := g.getFormatGlobal(" ")
				sb2.WriteString(fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0))\n%s",
					len(" ")+1, len(" ")+1, fg, g.indent()))
			}
			dataPtr := strDataPtr(arg)
			if dataPtr != "" {
				strLen := g.strLenFromExpr(sb, arg)
				g.tmpIdx++
				lenI32 := fmt.Sprintf("%%strpr.len.i32.%d", g.tmpIdx)
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
					fmtSpec = "%lld"
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
		return printVariadic(false)
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

	// str-to-i8/i16/i32/u8/u16/u32/byte: atoi + trunc + zext
	truncFmts := map[string]string{
		"str-to-i8": "i8", "str-to-i16": "i16", "str-to-i32": "i32",
		"str-to-u8": "i8", "str-to-u16": "i16", "str-to-u32": "i32",
		"str-to-byte": "i8",
	}
	if trunc, ok := truncFmts[fnName]; ok && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		aReg := fmt.Sprintf("%%st.a.%d", g.tmpIdx)
		g.tmpIdx++
		tReg := fmt.Sprintf("%%st.t.%d", g.tmpIdx)
		g.tmpIdx++
		zReg := fmt.Sprintf("%%st.z.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @atoi(%s)\n", g.indent(), aReg, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), tReg, aReg, trunc))
			sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), zReg, trunc, tReg))
		}
		return zReg
	}

	// str-to-char: load i8 + zext
	if fnName == "str-to-char" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		cReg := fmt.Sprintf("%%st.c.%d", g.tmpIdx)
		g.tmpIdx++
		zReg := fmt.Sprintf("%%st.cz.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i8, %s\n", g.indent(), cReg, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), zReg, cReg))
		}
		return zReg
	}

	// str-to-bool: strcmp + cmp + zext
	if fnName == "str-to-bool" && hasArgs {
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

	// bool-to-str: select
	if fnName == "bool-to-str" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		reg := fmt.Sprintf("%%boolstr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.str.false, i64 0, i64 0)\n",
				g.indent(), reg, a[0]))
		}
		return reg
	}

	return ""
}

// callBuiltin — 內建函數（len, cap, args-count, args-get, is-dir, stat-size, get-line）
func (g *Generator) callBuiltin(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string, expr *parser.CallExpression) string {

	if fnName == "len" && hasArgs {
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
			// Allocate %str struct { i64 len, i8* data }
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str\n", g.indent(), strReg))
			// Store length (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%str.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str, %%str* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			// Allocate buffer and memcpy
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), bufReg, lenReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 0, i8* %s)\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n", g.indent(), bufReg, ptrReg, lenReg))
			// Store data pointer (field 1)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str, %%str* %s, i32 0, i32 1\n", g.indent(), dataGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, dataGEP))
		}
		return strReg
	}

	// is-dir: 判斷路徑是否為目錄
	if fnName == "is-dir" && hasArgs {
		a := evalArgs()
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
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @stat(i8* %s, i8* %s)\n", g.indent(), statRet, a[0], statBuf))
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
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @stat(i8* %s, i8* %s)\n", g.indent(), statRet, a[0], statBuf))
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
			// Create %str struct
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str\n", g.indent(), strReg))
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%str.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str, %%str* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str, %%str* %s, i32 0, i32 1\n", g.indent(), dataGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), bufReg, dataGEP))
			// fclose
			sb.WriteString(fmt.Sprintf("%scall i32 @fclose(i8* %s)\n", g.indent(), stdinReg))
		}
		return strReg
	}

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
				if t == "%str" {
					dataPtr = g.extractStrDataPtr(sb, "%"+a.Value)
				} else if t == "%str-smail" {
					dataPtr = g.extractStrSmailDataPtr(sb, "%"+a.Value)
				}
			}
		}
	case *parser.StringLiteral:
		ptr := g.generateExprWithSB(sb, a)
		if len(a.Value) <= 127 {
			dataPtr = g.extractStrSmailDataPtr(sb, ptr)
		} else {
			dataPtr = g.extractStrDataPtr(sb, ptr)
		}
	case *parser.InfixExpression:
		if a.Operator == "-" && (g.isStringExpr(a.Left) || g.isStringExpr(a.Right)) {
			ptr := g.generateStrConcat(sb, a.Left, a.Right)
			dataPtr = g.extractStrDataPtr(sb, ptr)
		}
	}

	if dataPtr == "" {
		return dataPtr
	}

	g.tmpIdx++
	sizeReg := fmt.Sprintf("%%strnull.size.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), sizeReg, strLen))

	g.tmpIdx++
	buf := fmt.Sprintf("%%strnull.buf.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), buf, sizeReg))

	g.tmpIdx++
	nullEnd := fmt.Sprintf("%%strnull.end.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), nullEnd, buf, strLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullEnd))

	sb.WriteString(fmt.Sprintf("%scall void @memcpy(i8* %s, i8* %s, i64 %s)\n", g.indent(), buf, dataPtr, strLen))

	return buf
}
