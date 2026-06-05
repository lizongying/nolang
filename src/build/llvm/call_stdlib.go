package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// callFmt — print/println/printf 家族
func (g *Generator) callFmt(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string, expr *parser.CallExpression) string {

	// 型別化 printf 參數
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

	// strDataPtr extracts the i8* data pointer from a %str* or %str-smail* for print/printf usage.
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
		}
		return ""
	}

	if fnName == "printf" && hasArgs {
		// Format string arg: extract i8* data from %str
		fmtData := strDataPtr(expr.Arguments[0])
		args := ""
		if fmtData != "" {
			args = "i8* " + fmtData
		} else {
			a := evalArgs()
			args = strArg(a[0])
		}
		// Remaining args
		for i := 1; i < len(expr.Arguments); i++ {
			data := strDataPtr(expr.Arguments[i])
			if data != "" {
				args += ", i8* " + data
			} else {
				a := evalArgs()
				args += ", " + typedArg(a[i])
			}
		}
		// 編譯期檢查格式字串參數個數
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

	// print / println variadic: 對每個參數依型別用對應格式
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
			// Check if this arg is a string type
			dataPtr := strDataPtr(arg)
			if dataPtr != "" {
				fmtSpec := "%s"
				if newline && i == len(expr.Arguments)-1 {
					fmtSpec += "\n"
				}
				fg := g.getFormatGlobal(fmtSpec)
				sb2.WriteString(fmt.Sprintf("call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), i8* %s)",
					len(fmtSpec)+1, len(fmtSpec)+1, fg, dataPtr))
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

	if fnName == "print" {
		return printVariadic(false)
	}
	if fnName == "println" {
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
		if fn == fnName && hasArgs {
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
	if r := printInt("%lld\n", "println-i64"); r != "" {
		return r
	}
	if r := printInt("%d", "print-byte"); r != "" {
		return r
	}
	if r := printInt("%d\n", "println-byte"); r != "" {
		return r
	}
	if r := printInt("%c", "print-char"); r != "" {
		return r
	}
	if r := printInt("%c\n", "println-char"); r != "" {
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

// callMath — LLVM intrinsic 數學函數
func (g *Generator) callMath(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string) string {

	// 1 參數 intrinsics
	u1 := map[string]string{
		"abs": "llvm.fabs.f64", "sqrt": "llvm.sqrt.f64",
		"sin": "llvm.sin.f64", "cos": "llvm.cos.f64",
		"ceil": "llvm.ceil.f64", "floor": "llvm.floor.f64",
		"round": "llvm.round.f64", "trunc": "llvm.trunc.f64",
		"exp": "llvm.exp.f64", "log": "llvm.log.f64",
		"log10": "llvm.log10.f64", "log2": "llvm.log2.f64",
		"atan": "llvm.atan.f64",
		"asin": "llvm.asin.f64", "acos": "llvm.acos.f64",
		"sinh": "llvm.sinh.f64", "cosh": "llvm.cosh.f64",
		"tanh": "llvm.tanh.f64",
	}
	if intr, ok := u1[fnName]; ok && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call double @%s(double %s)", intr, a[0])
	}

	// 2 參數 intrinsics
	pairs := map[string]string{
		"pow": "llvm.pow.f64", "atan2": "llvm.atan2.f64",
		"max": "llvm.maxnum.f64", "min": "llvm.minnum.f64",
		"hypot": "hypot",
	}
	if intr, ok := pairs[fnName]; ok && nArgs == 2 {
		a := evalArgs()
		return fmt.Sprintf("call double @%s(double %s, double %s)", intr, a[0], a[1])
	}

	if fnName == "fmod" && nArgs == 2 {
		a := evalArgs()
		return fmt.Sprintf("call double @fmod(double %s, double %s)", a[0], a[1])
	}
	if fnName == "cbrt" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call double @cbrt(double %s)", a[0])
	}
	// tan = sin(x) / cos(x)
	if fnName == "tan" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		sReg := fmt.Sprintf("%%tan.s.%d", g.tmpIdx)
		g.tmpIdx++
		cReg := fmt.Sprintf("%%tan.c.%d", g.tmpIdx)
		g.tmpIdx++
		rReg := fmt.Sprintf("%%tan.r.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call double @llvm.sin.f64(double %s)\n", g.indent(), sReg, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = call double @llvm.cos.f64(double %s)\n", g.indent(), cReg, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = fdiv double %s, %s\n", g.indent(), rReg, sReg, cReg))
		}
		return rReg
	}

	return ""
}

// callStrconv — 字串轉換家族
func (g *Generator) callStrconv(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string) string {

	if fnName == "str-to-i64" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call i64 @atoi(%s)", strArg(a[0]))
	}
	if fnName == "str-to-f64" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call double @strtod(%s, i8* null)", strArg(a[0]))
	}
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

	// str-to-XX: atoi + trunc + zext
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
	if fnName == "str-to-u64" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call i64 @strtoull(%s, i8* null, i32 10)", strArg(a[0]))
	}
	if fnName == "str-to-f32" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call double @strtod(%s, i8* null)", strArg(a[0]))
	}
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

	// XX-to-str: sprintf + format specifier
	sprintfFmts := map[string]string{
		"i8-to-str": "%hhd", "i16-to-str": "%hd", "i32-to-str": "%d",
		"u8-to-str": "%hhu", "u16-to-str": "%hu", "u32-to-str": "%u", "u64-to-str": "%llu",
		"byte-to-str": "%hhu", "char-to-str": "%c",
	}
	if fmtSpec, ok := sprintfFmts[fnName]; ok && hasArgs {
		a := evalArgs()
		fg := g.getFormatGlobal(fmtSpec)
		buf := "i8* getelementptr inbounds ([64 x i8], [64 x i8]* @.strconv_buf, i64 0, i64 0)"
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i32 (i8*, i8*, ...) @sprintf(%s, i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), %s)\n",
				g.indent(), buf, len(fmtSpec)+1, len(fmtSpec)+1, fg, llvmArg(a[0])))
		}
		return buf
	}
	if fnName == "i64-to-str" && hasArgs {
		a := evalArgs()
		fg := g.getFormatGlobal("%lld")
		buf := "i8* getelementptr inbounds ([64 x i8], [64 x i8]* @.strconv_buf, i64 0, i64 0)"
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i32 (i8*, i8*, ...) @sprintf(%s, i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), %s)\n",
				g.indent(), buf, len("%lld")+1, len("%lld")+1, fg, llvmArg(a[0])))
		}
		return buf
	}
	if fnName == "f64-to-str" && hasArgs {
		a := evalArgs()
		fg := g.getFormatGlobal("%g")
		buf := "i8* getelementptr inbounds ([64 x i8], [64 x i8]* @.strconv_buf, i64 0, i64 0)"
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i32 (i8*, i8*, ...) @sprintf(%s, i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), double %s)\n",
				g.indent(), buf, len("%g")+1, len("%g")+1, fg, a[0]))
		}
		return buf
	}
	if fnName == "f32-to-str" && hasArgs {
		a := evalArgs()
		fg := g.getFormatGlobal("%g")
		buf := "i8* getelementptr inbounds ([64 x i8], [64 x i8]* @.strconv_buf, i64 0, i64 0)"
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i32 (i8*, i8*, ...) @sprintf(%s, i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i64 0, i64 0), double %s)\n",
				g.indent(), buf, len("%g")+1, len("%g")+1, fg, a[0]))
		}
		return buf
	}
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

// callOs — 作業系統呼叫（環境變數、程序、時間等）
func (g *Generator) callOs(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string) string {

	if fnName == "get-env" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call i8* @getenv(%s)", strArg(a[0]))
	}
	if fnName == "set-env" && nArgs == 2 {
		a := evalArgs()
		return fmt.Sprintf("call i32 @setenv(%s, %s, i32 1)", strArg(a[0]), strArg(a[1]))
	}
	if fnName == "get-wd" {
		buf := "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* @.os_buf, i64 0, i64 0)"
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i8* @getcwd(%s, i64 1024)\n", g.indent(), buf))
		}
		return buf
	}
	if fnName == "ch-dir" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call i32 @chdir(%s)", strArg(a[0]))
	}
	if fnName == "exit" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call void @exit(i32 %s)", llvmArg(a[0]))
	}
	if fnName == "get-pid" {
		g.tmpIdx++
		pidReg := fmt.Sprintf("%%pid.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%pid.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @getpid()\n", g.indent(), pidReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), extReg, pidReg))
		}
		return extReg
	}
	if fnName == "host-name" {
		buf := "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* @.os_buf, i64 0, i64 0)"
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i32 @gethostname(%s, i64 1024)\n", g.indent(), buf))
		}
		return buf
	}
	if fnName == "mkdir" && nArgs == 2 {
		a := evalArgs()
		return fmt.Sprintf("call i32 @mkdir(%s, i32 %s)", strArg(a[0]), a[1])
	}
	if fnName == "remove" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		rReg := fmt.Sprintf("%%rm.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		rExt := fmt.Sprintf("%%rm.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @unlink(%s)\n", g.indent(), rReg, strArg(a[0])))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), rExt, rReg))
		}
		return rExt
	}
	if fnName == "rename" && nArgs == 2 {
		a := evalArgs()
		g.tmpIdx++
		rReg := fmt.Sprintf("%%rn.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		rExt := fmt.Sprintf("%%rn.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @rename(%s, %s)\n", g.indent(), rReg, strArg(a[0]), strArg(a[1])))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), rExt, rReg))
		}
		return rExt
	}
	if fnName == "is-file" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		rReg := fmt.Sprintf("%%st.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		rExt := fmt.Sprintf("%%st.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @stat(%s, i8* null)\n", g.indent(), rReg, strArg(a[0])))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), rExt, rReg))
		}
		g.tmpIdx++
		rExt2 := fmt.Sprintf("%%st.ext2.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), rExt2, rExt))
		}
		return rExt2
	}
	if fnName == "now" {
		g.tmpIdx++
		tReg := fmt.Sprintf("%%time.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @time(i8* null)\n", g.indent(), tReg))
		}
		return tReg
	}
	if fnName == "sleep" && hasArgs {
		a := evalArgs()
		return fmt.Sprintf("call i32 @sleep(i32 %s)", a[0])
	}

	return ""
}

// callFileIO — 檔案讀寫
func (g *Generator) callFileIO(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string) string {

	if fnName == "open-read" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		fdReg := fmt.Sprintf("%%fd.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		fdExt := fmt.Sprintf("%%fd.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @open(%s, i32 0, i32 0)\n", g.indent(), fdReg, strArg(a[0])))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, fdReg))
		}
		return fdExt
	}
	if fnName == "open-write" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		fdReg := fmt.Sprintf("%%fd.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		fdExt := fmt.Sprintf("%%fd.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @open(%s, i32 1537, i32 420)\n", g.indent(), fdReg, strArg(a[0])))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, fdReg))
		}
		return fdExt
	}
	if fnName == "read" && nArgs >= 3 {
		a := evalArgs()
		buf := "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* @.os_buf, i64 0, i64 0)"
		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%fd.trunc.%d", g.tmpIdx)
		g.tmpIdx++
		rReg := fmt.Sprintf("%%rd.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @read(i32 %s, %s, i64 %s)\n",
				g.indent(), rReg, fdTrunc, buf, a[2]))
		}
		return rReg
	}
	if fnName == "write" && nArgs >= 3 {
		a := evalArgs()
		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%fd.trunc.%d", g.tmpIdx)
		g.tmpIdx++
		wReg := fmt.Sprintf("%%wr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @write(i32 %s, %s, i64 %s)\n",
				g.indent(), wReg, fdTrunc, strArg(a[1]), a[2]))
		}
		return wReg
	}
	if fnName == "close" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		clTrunc := fmt.Sprintf("%%cl.trunc.%d", g.tmpIdx)
		g.tmpIdx++
		clReg := fmt.Sprintf("%%cl.tmp.%d", g.tmpIdx)
		g.tmpIdx++
		clExt := fmt.Sprintf("%%cl.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), clTrunc, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @close(i32 %s)\n", g.indent(), clReg, clTrunc))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), clExt, clReg))
		}
		return clExt
	}

	return ""
}
