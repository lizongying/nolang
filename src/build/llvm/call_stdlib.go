package llvm

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// callFmt — print/println/printf 家族
func (g *Generator) callFmt(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string, expr *parser.CallExpression) string {

	// === Named format string path ===
	// For printf/eprintf/sprintf with a string literal first arg, parse {name:spec}
	// fields and dispatch to Nolang fmt-* helpers + io.out/io.err.
	// For print/eprint with a single string literal arg containing '{', also use
	// named format (with auto-appended newline).
	if sb != nil && hasArgs && len(expr.Arguments) >= 1 {
		intercept, autoNewline := g.shouldInterceptNamedFormat(fnName, expr)
		if intercept {
			strLit := expr.Arguments[0].(*parser.StringLiteral)
			segments, err := parser.ParseFormatString(strLit.Value)
			if err == nil {
				return g.callNamedFormat(sb, fnName, segments, autoNewline)
			}
			// Parse error: fall through to existing code path
		}
	}

	// === Plain string printf/eprintf/sprintf/format path ===
	// libc @printf/@sprintf/@fprintf declarations have been removed; all output
	// goes through Nolang's @out/@err. When printf/eprintf/sprintf/format is
	// called with a single string literal arg (no '{' fields, no '%'
	// conversions, no additional args), emit it directly via @out/@err (no
	// trailing newline) or return the string for sprintf/format. This covers
	// common label-style usage like printf('crc32("")=') and
	// format('label='). Multi-arg C-style printf('...%d...', x) is no longer
	// supported — migrate to named format printf('{x}') or format('{x}').
	// Note: printf/eprintf/sprintf are deprecated; prefer print/eprint/format
	// + io.out/io.err for no-newline output.
	if sb != nil && hasArgs && len(expr.Arguments) == 1 {
		if strLit, ok := expr.Arguments[0].(*parser.StringLiteral); ok {
			switch fnName {
			case "printf", "fmt.printf", "eprintf", "fmt.eprintf", "sprintf", "fmt.sprintf",
				"format", "fmt.format":
				useStderr := strings.HasPrefix(fnName, "eprint") || strings.HasPrefix(fnName, "fmt.eprint")
				isReturnStr := fnName == "sprintf" || fnName == "fmt.sprintf" ||
					fnName == "format" || fnName == "fmt.format"
				strPtr := g.buildStrLongFromValue(sb, strLit.Value)
				if isReturnStr {
					return strPtr
				}
				g.emitOutCall(sb, useStderr, strPtr)
				return "0"
			}
		}
	}

	// Note: C-style printf/eprintf/sprintf paths (using libc @printf/@sprintf/@fprintf)
	// have been removed. All output now goes through Nolang's @out/@err functions.
	// Use named format strings (e.g. printf('{x}')) or print/println instead.

	// printVariadic handles print/eprint/println with arbitrary args.
	// All output goes through Nolang's @out/@err (no libc @printf/@fprintf).
	// Writes calls directly to sb; returns a non-"call " sentinel so the
	// statement-level caller (generateExpressionStmt) doesn't double-emit.
	printVariadic := func(newline bool, useStderr bool) string {
		if !hasArgs {
			if newline {
				nlPtr := g.buildStrLongFromValue(sb, "\n")
				g.emitOutCall(sb, useStderr, nlPtr)
			}
			return "0"
		}
		for i, arg := range expr.Arguments {
			// Skip void function call arguments (call for side effects, don't print)
			if callExpr, ok := arg.(*parser.CallExpression); ok && !g.isNonVoidCall(callExpr) {
				g.generateExprWithSB(sb, arg)
				continue
			}
			// Space separator between args.
			if i > 0 {
				spacePtr := g.buildStrLongFromValue(sb, " ")
				g.emitOutCall(sb, useStderr, spacePtr)
			}
			// Convert arg to %str-long* and write via @out/@err.
			argPtr := g.emitArgAsStrLong(sb, arg, "")
			g.emitOutCall(sb, useStderr, argPtr)
		}
		// Trailing newline (print/eprint always append \n).
		if newline {
			nlPtr := g.buildStrLongFromValue(sb, "\n")
			g.emitOutCall(sb, useStderr, nlPtr)
		}
		return "0"
	}

	if fnName == "print" || fnName == "fmt.print" {
		return printVariadic(true, false)
	}
	if fnName == "println" || fnName == "fmt.println" {
		return printVariadic(true, false)
	}
	if fnName == "eprint" || fnName == "fmt.eprint" {
		return printVariadic(true, true)
	}
	if fnName == "println-empty" {
		nlPtr := g.buildStrLongFromValue(sb, "\n")
		g.emitOutCall(sb, false, nlPtr)
		return "0"
	}

	// Type-specific print functions: each converts its single arg to a
	// %str-long* via emitArgAsStrLong (using the appropriate fmt-* helper
	// and spec), then writes it via @out. Variants with "println" prefix
	// additionally write a trailing newline via @out.
	emitTypedPrint := func(targetFn, spec string, addNewline bool) string {
		if fnName != targetFn || !hasArgs {
			return ""
		}
		arg := expr.Arguments[0]
		argPtr := g.emitArgAsStrLong(sb, arg, spec)
		g.emitOutCall(sb, false, argPtr)
		if addNewline {
			nlPtr := g.buildStrLongFromValue(sb, "\n")
			g.emitOutCall(sb, false, nlPtr)
		}
		return "0"
	}

	// print-i64 / println-i64: fmt-int with empty spec.
	if r := emitTypedPrint("print-i64", "", false); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.print-i64", "", false); r != "" {
		return r
	}
	if r := emitTypedPrint("println-i64", "", true); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.println-i64", "", true); r != "" {
		return r
	}
	// print-byte: fmt-int with empty spec (byte is i64 from zext).
	if r := emitTypedPrint("print-byte", "", false); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.print-byte", "", false); r != "" {
		return r
	}
	if r := emitTypedPrint("println-byte", "", true); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.println-byte", "", true); r != "" {
		return r
	}
	// print-char: fmt-int with 'c' spec (converts integer to single char).
	if r := emitTypedPrint("print-char", "c", false); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.print-char", "c", false); r != "" {
		return r
	}
	if r := emitTypedPrint("println-char", "c", true); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.println-char", "c", true); r != "" {
		return r
	}
	// print-hex*: fmt-uint (dispatched by emitArgAsStrLong when spec contains 'x').
	if r := emitTypedPrint("print-hex32", "08x", false); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.print-hex32", "08x", false); r != "" {
		return r
	}
	if r := emitTypedPrint("print-hex64", "016x", false); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.print-hex64", "016x", false); r != "" {
		return r
	}
	if r := emitTypedPrint("print-hex8", "02x", false); r != "" {
		return r
	}
	if r := emitTypedPrint("fmt.print-hex8", "02x", false); r != "" {
		return r
	}
	// print-f64: fmt-f64 with empty spec (defaults to 'g').
	if r := emitTypedPrint("print-f64", "", false); r != "" {
		return r
	}
	if r := emitTypedPrint("println-f64", "", true); r != "" {
		return r
	}
	// print-bool: fmt-bool with empty spec.
	if r := emitTypedPrint("print-bool", "", false); r != "" {
		return r
	}
	if r := emitTypedPrint("println-bool", "", true); r != "" {
		return r
	}

	return ""
}

// emitOutCall emits a call to @out or @err with the given %str-long*.
// The i64* output parameter (bytes written) is allocated and discarded.
// Writes the call directly to sb.
func (g *Generator) emitOutCall(sb *strings.Builder, useStderr bool, strPtr string) {
	if sb == nil {
		return
	}
	outFn := "out"
	if useStderr {
		outFn = "err"
	}
	g.tmpIdx++
	nAlloca := fmt.Sprintf("%%vso.n.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), nAlloca))
	sb.WriteString(fmt.Sprintf("%scall void @%s(%%str-long* %s, i64* %s)\n",
		g.indent(), outFn, strPtr, nAlloca))
}

// emitArgAsStrLong converts any expression to a %str-long* alloca.
// For string expressions, returns the existing %str-long* directly.
// For integer/float/bool expressions, calls the appropriate fmt-* helper
// with the given spec string and a zero-initialized output buffer.
// Pattern follows generateFieldStr but works with ANY expression
// (uses generateExprWithSB to obtain SSA values for non-identifier exprs).
func (g *Generator) emitArgAsStrLong(sb *strings.Builder, expr parser.Expression, spec string) string {
	// Determine the source LLVM type first (mirrors printVariadic's logic).
	srcType := "i64"
	isOptionStr := false
	if ident, ok := expr.(*parser.Identifier); ok && g.varTypes != nil {
		if t, ok := g.varTypes[ident.Value]; ok {
			srcType = t
			if t == "%option" && g.optionInnerTypes != nil {
				if it, ok := g.optionInnerTypes[ident.Value]; ok {
					srcType = it
					isOptionStr = it == "%str-long"
				}
			}
		}
	} else {
		t := g.intExprLLVMType(expr)
		if t != "" {
			srcType = t
		}
	}

	// String expression: return %str-long* directly.
	// For option variables (?str), generateExprWithSB extracts the inner %str-long*.
	if srcType == "%str-long" {
		if isOptionStr {
			// Option ?str: generateExprWithSB returns the inner %str-long* via inttoptr.
			return g.generateExprWithSB(sb, expr)
		}
		return g.getStrPtr(sb, expr)
	}
	if g.isStringExpr(expr) {
		return g.getStrPtr(sb, expr)
	}

	// Generate the expression's SSA value.
	v := g.generateExprWithSB(sb, expr)

	// String-returning CallExpression (e.g. energy().to-str()):
	// exprResultLLVMType may fail for chained calls; after generation,
	// ssaTypes[v] holds the actual return type — if %str-long, materialize
	// into a temp alloca and return it like other string expressions.
	if _, isCall := expr.(*parser.CallExpression); isCall && g.ssaTypes != nil {
		if t, ok := g.ssaTypes[v]; ok && t == "%str-long" {
			g.tmpIdx++
			tmpAlloca := fmt.Sprintf("%%str-long.arg.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), v, tmpAlloca))
			return tmpAlloca
		}
	}

	// Fallback: detect double literal (e.g. "3.14") by content.
	if srcType == "i64" && strings.Contains(v, ".") && !strings.HasPrefix(v, "%") && !strings.HasPrefix(v, "i8*") {
		srcType = "double"
	}

	// Build spec %str-long*
	specPtr := g.buildStrLongFromValue(sb, spec)

	// Allocate output buffer and zero-initialize it.
	// fmt-* helpers perform move-assignment to `out` (out = fmt-apply-spec(...)),
	// which frees the old value of `out` before assigning. An uninitialized
	// buffer would contain stack garbage, causing the free of a bogus data
	// pointer to crash (SIGABRT). Must zero-initialize to {len=0, cap=0, data=null}.
	g.tmpIdx++
	outBuf := fmt.Sprintf("%%vso.tmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), outBuf))
	sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), outBuf))

	// Determine spec type character (last alphabetic char in spec).
	// Mirrors generateFieldStr's specType-based dispatch.
	specType := byte(0)
	for i := len(spec) - 1; i >= 0; i-- {
		c := spec[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			specType = c
			break
		}
	}

	// Dispatch based on spec type and source type (mirrors generateFieldStr).
	switch {
	case specType == 'b' || specType == 'o' || specType == 'x' || specType == 'X':
		// Unsigned format — use fmt-uint (expects i64*).
		// Coerce narrow integers to i64 with zext (unsigned semantics).
		if srcType == "i8" || srcType == "i16" || srcType == "i32" {
			g.tmpIdx++
			extReg := fmt.Sprintf("%%arg.ext.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), extReg, srcType, v))
			v = extReg
		}
		g.tmpIdx++
		valAlloca := fmt.Sprintf("%%fmtval.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-uint(i64* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	case srcType == "double":
		// fmt-f64(double* x, %str-long* spec, %str-long* out)
		g.tmpIdx++
		valAlloca := fmt.Sprintf("%%fmtval.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-f64(double* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	case srcType == "i1":
		// fmt-bool(i1* b, %str-long* spec, %str-long* out)
		g.tmpIdx++
		valAlloca := fmt.Sprintf("%%fmtval.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = alloca i1\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i1 %s, i1* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-bool(i1* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	default:
		// Integer — fmt-int(i64* n, %str-long* spec, %str-long* out).
		// Coerce narrow integers to i64 with sext (signed semantics, matches
		// the previous printVariadic behavior for %lld).
		if srcType == "i8" || srcType == "i16" || srcType == "i32" {
			g.tmpIdx++
			extReg := fmt.Sprintf("%%arg.ext.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = sext %s %s to i64\n", g.indent(), extReg, srcType, v))
			v = extReg
		}
		g.tmpIdx++
		valAlloca := fmt.Sprintf("%%fmtval.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-int(i64* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	}
	return outBuf
}

// callStrconv — 仍需 ForwardFunc 的特殊轉換
func (g *Generator) callStrconv(sb *strings.Builder, fnName string, hasArgs bool, nArgs int,
	evalArgs func() []string, strArg, llvmArg func(string) string) string {

	// str.to-bool: memcmp("true", 5 bytes incl null) + cmp + zext
	// 使用 @llvm.memcmp 替代 @strcmp（避免 libc 依賴）
	if fnName == "str.to-bool" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%boolcmp.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.memcmp(i8* %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i64 5)\n",
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

	// bool.to-str: select + malloc + memcpy + 构造 %str-long
	// Must heap-allocate the data buffer so emitHeapFree can safely free it.
	if fnName == "bool.to-str" && hasArgs {
		a := evalArgs()
		g.tmpIdx++
		selectReg := fmt.Sprintf("%%boolstr.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.str.false, i64 0, i64 0)\n",
				g.indent(), selectReg, a[0]))
		}
		// "true" = 4, "false" = 5; use select directly
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%boolstr.len.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 4, i64 5\n", g.indent(), lenReg, a[0]))
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
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 5, i64 6\n", g.indent(), copyLenReg, a[0]))
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
	}

	return ""
}

// isTempStrDataPtr 判断表达式是否产生临时堆 data 指针（需要在使用后 free）。
// 字符串拼接（InfixExpression +）和函数调用（CallExpression 返回 %str-long）
// 会 malloc 新 data；StringLiteral 的 data 是全局常量，Identifier 引用变量，
// 均不需 free。
func (g *Generator) isTempStrDataPtr(arg parser.Expression) bool {
	switch a := arg.(type) {
	case *parser.InfixExpression:
		if a.Operator == "+" || a.Operator == "-" {
			return g.isStringExpr(a.Left) || g.isStringExpr(a.Right)
		}
	case *parser.CallExpression:
		return true
	case *parser.GroupedExpression:
		return g.isTempStrDataPtr(a.Expression)
	}
	return false
}

// nullTerminateStrArg ensures a string argument is null-terminated for C function calls.
// Tries makeNullTerminatedStr first (handles Identifier, StringLiteral, InfixExpression, DotExpression).
// Falls back to manual null-termination from the eval result for other expression types
// (e.g. CallExpression like arg(i)).
func (g *Generator) nullTerminateStrArg(sb *strings.Builder, evalResult string, expr parser.Expression) string {
	// Try makeNullTerminatedStr for known expression types
	if ptr := g.makeNullTerminatedStr(sb, expr); ptr != "" {
		return ptr
	}
	// Fallback: manually null-terminate from the eval result.
	// Resolve evalResult to a stable %str-long* pointer for BOTH data and length
	// extraction. When evalResult is a loaded value (contains ".val."), stripping
	// ".val.N" only yields a valid pointer for simple variable loads (e.g.
	// %x.val.N → %x). For complex expressions like IndexExpression results
	// (e.g. %vec.idx.val.N), the stripped prefix (%vec.idx) is NOT a valid
	// register, so we materialize the value into a temp alloca instead.
	strPtr := evalResult
	if strings.HasPrefix(evalResult, "%") {
		if idx := strings.Index(evalResult, ".val."); idx > 0 {
			baseRef := evalResult[:idx]
			// baseRef is a valid alloca pointer only if it is a simple variable
			// reference (%varName with no extra dots). For complex expressions
			// like %vec.idx.val.N, baseRef (%vec.idx) is NOT a valid register,
			// so we materialize the value into a temp alloca instead.
			simpleVar := !strings.Contains(strings.TrimPrefix(baseRef, "%"), ".")
			known := false
			if simpleVar && g.varTypes != nil {
				varName := strings.TrimPrefix(baseRef, "%")
				_, known = g.varTypes[varName]
			}
			if known {
				// Simple variable load: %var.val.N → %var is the alloca pointer
				strPtr = baseRef
			} else {
				// Complex expression result: materialize value into temp alloca
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%str-long.nt.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), evalResult, tmpAlloca))
				}
				strPtr = tmpAlloca
			}
		}
	}
	dataPtr := g.extractStrDataPtr(sb, strPtr)
	strLen := g.extractStrLen(sb, strPtr)
	// Allocate buffer of len+1
	g.tmpIdx++
	sizeReg := fmt.Sprintf("%%nt.size.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), sizeReg, strLen))
	}
	g.tmpIdx++
	buf := fmt.Sprintf("%%nt.buf.%d", g.tmpIdx)
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), buf, sizeReg))
		// Null-terminate
		g.tmpIdx++
		nullEnd := fmt.Sprintf("%%nt.end.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), nullEnd, buf, strLen))
		sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullEnd))
		// Copy string data
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), buf, dataPtr, strLen))
	}
	return buf
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
		// Handle string variables: use extractLenDispatch for %str-long
		if ident, ok := arg0.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok {
					if t == "%str-long" {
						return g.extractLenDispatch(sb, ident.Value)
					}
					// Handle %arr and %vec: load field 0 (i64 len) from the struct pointer
					if t == "%arr" || t == "%vec" {
						g.tmpIdx++
						lenGEP := fmt.Sprintf("%%builtin.len.gep.%d", g.tmpIdx)
						g.tmpIdx++
						lenReg := fmt.Sprintf("%%builtin.len.%d", g.tmpIdx)
						if sb != nil {
							sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n", g.indent(), lenGEP, t, t, g.varAddr(ident.Value)))
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
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* @.argc.addr\n", g.indent(), loadReg))
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
			sb.WriteString(fmt.Sprintf("%s%s = load i8**, i8*** @.argv.addr\n", g.indent(), argvReg))
			// idx is already i64; use directly for GEP
			// GEP to get argv[idx] (i8*)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8*, i8** %s, i64 %s\n", g.indent(), gepReg, argvReg, a[0]))
			ptrReg = g.loadDataPtrField(sb, gepReg)
			// strlen to get length
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, ptrReg))
			// Allocate %str-long struct { i64 len, i64 cap, i8* data }
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			// Store length (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%str-long.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			// Store cap (field 1) = lenReg
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%str-long.cap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, capGEP))
			// Allocate heap buffer and memcpy (must be heap-allocated so emitHeapFree can safely free it)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, lenReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), bufReg, ptrReg, lenReg))
			// Store data pointer (field 2)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str-long.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, bufReg, dataGEP)
		}
		return strReg
	}

	// is-dir: 判斷路徑是否為目錄
	if fnName == "is-dir" && hasArgs {
		a := evalArgs()
		// 從 %str-long 參數提取 i8* 資料指針
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
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
		statL := g.statLayout()
		if sb != nil {
			// Allocate stat buffer
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			// stat(path, &statbuf)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			// Check stat return == 0
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			// Load st_mode
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), modeGEP, statBuf, statL.ModeOff))
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

	// is-file: 判斷路徑是否為普通檔案
	// Mirrors is-dir but masks with S_IFREG (0100000 = 0x8000 = 32768)
	if (fnName == "is-file" || fnName == "stat-file") && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.sf.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.sf.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.sf.%d", g.tmpIdx)
		g.tmpIdx++
		modeGEP := fmt.Sprintf("%%stat.mode.sf.%d", g.tmpIdx)
		g.tmpIdx++
		modeLoad := fmt.Sprintf("%%stat.mode.ld.sf.%d", g.tmpIdx)
		g.tmpIdx++
		andReg := fmt.Sprintf("%%stat.and.sf.%d", g.tmpIdx)
		g.tmpIdx++
		cmp2 := fmt.Sprintf("%%stat.cmp2.sf.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%stat.ext.sf.%d", g.tmpIdx)
		statL := g.statLayout()
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), modeGEP, statBuf, statL.ModeOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i16, i16* %s\n", g.indent(), modeLoad, modeGEP))
			// S_IFREG = 0100000 = 0x8000 = 32768
			sb.WriteString(fmt.Sprintf("%s%s = and i16 %s, 32768\n", g.indent(), andReg, modeLoad))
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i16 %s, 0\n", g.indent(), cmp2, andReg))
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), extReg, cmpReg, cmp2))
			g.tmpIdx++
			zextReg := fmt.Sprintf("%%stat.zext.sf.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, extReg))
			return zextReg
		}
	}

	// stat-size / file-size: 獲取文件大小
	if (fnName == "stat-size" || fnName == "file-size") && hasArgs {
		a := evalArgs()
		// 從 %str-long 參數提取 i8* 資料指針
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
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
		statL := g.statLayout()
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), sizeGEP, statBuf, statL.SizeOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), sizeLoad, sizeGEP))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), selReg, cmpReg, sizeLoad))
		}
		return selReg
	}

	// stat-mode: 獲取文件模式 (st_mode)
	if (fnName == "stat-mode") && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.sm.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.sm.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.sm.%d", g.tmpIdx)
		g.tmpIdx++
		modeGEP := fmt.Sprintf("%%stat.mode.gep.%d", g.tmpIdx)
		g.tmpIdx++
		modeLoad := fmt.Sprintf("%%stat.mode.ld.%d", g.tmpIdx)
		g.tmpIdx++
		modeZext := fmt.Sprintf("%%stat.mode.zext.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%stat.mode.sel.%d", g.tmpIdx)
		statL := g.statLayout()
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), modeGEP, statBuf, statL.ModeOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i16, i16* %s\n", g.indent(), modeLoad, modeGEP))
			sb.WriteString(fmt.Sprintf("%s%s = zext i16 %s to i64\n", g.indent(), modeZext, modeLoad))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), selReg, cmpReg, modeZext))
		}
		return selReg
	}

	// stat-uid: 獲取文件 owner uid
	if (fnName == "stat-uid") && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.su.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.su.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.su.%d", g.tmpIdx)
		g.tmpIdx++
		uidGEP := fmt.Sprintf("%%stat.uid.gep.%d", g.tmpIdx)
		g.tmpIdx++
		uidLoad := fmt.Sprintf("%%stat.uid.ld.%d", g.tmpIdx)
		g.tmpIdx++
		uidZext := fmt.Sprintf("%%stat.uid.zext.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%stat.uid.sel.%d", g.tmpIdx)
		statL := g.statLayout()
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), uidGEP, statBuf, statL.UidOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), uidLoad, uidGEP))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), uidZext, uidLoad))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), selReg, cmpReg, uidZext))
		}
		return selReg
	}

	// stat-gid: 獲取文件 group gid
	if (fnName == "stat-gid") && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.sg.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.sg.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.sg.%d", g.tmpIdx)
		g.tmpIdx++
		gidGEP := fmt.Sprintf("%%stat.gid.gep.%d", g.tmpIdx)
		g.tmpIdx++
		gidLoad := fmt.Sprintf("%%stat.gid.ld.%d", g.tmpIdx)
		g.tmpIdx++
		gidZext := fmt.Sprintf("%%stat.gid.zext.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%stat.gid.sel.%d", g.tmpIdx)
		statL := g.statLayout()
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), gidGEP, statBuf, statL.GidOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), gidLoad, gidGEP))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), gidZext, gidLoad))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), selReg, cmpReg, gidZext))
		}
		return selReg
	}

	// stat-mtime: 獲取文件修改時間 (st_mtimespec.tv_sec)
	if (fnName == "stat-mtime") && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%statbuf.smt.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%stat.ret.smt.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%stat.cmp.smt.%d", g.tmpIdx)
		g.tmpIdx++
		mtimeGEP := fmt.Sprintf("%%stat.mtime.gep.%d", g.tmpIdx)
		g.tmpIdx++
		mtimeLoad := fmt.Sprintf("%%stat.mtime.ld.%d", g.tmpIdx)
		g.tmpIdx++
		selReg := fmt.Sprintf("%%stat.mtime.sel.%d", g.tmpIdx)
		statL := g.statLayout()
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), mtimeGEP, statBuf, statL.MtimeOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), mtimeLoad, mtimeGEP))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), selReg, cmpReg, mtimeLoad))
		}
		return selReg
	}

	// read-file: read entire file into a string
	// Returns a %str-long* alloca; empty string on error.
	if fnName == "read-file" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		statBuf := fmt.Sprintf("%%rf.statbuf.%d", g.tmpIdx)
		g.tmpIdx++
		statRet := fmt.Sprintf("%%rf.statret.%d", g.tmpIdx)
		g.tmpIdx++
		statCmp := fmt.Sprintf("%%rf.statcmp.%d", g.tmpIdx)
		g.tmpIdx++
		sizeGEP := fmt.Sprintf("%%rf.sizegep.%d", g.tmpIdx)
		g.tmpIdx++
		sizeLoad := fmt.Sprintf("%%rf.sizeld.%d", g.tmpIdx)
		g.tmpIdx++
		sizeSel := fmt.Sprintf("%%rf.size.%d", g.tmpIdx)
		g.tmpIdx++
		openRet := fmt.Sprintf("%%rf.open.%d", g.tmpIdx)
		g.tmpIdx++
		openCmp := fmt.Sprintf("%%rf.opencmp.%d", g.tmpIdx)
		g.tmpIdx++
		bufReg := fmt.Sprintf("%%rf.buf.%d", g.tmpIdx)
		g.tmpIdx++
		readRet := fmt.Sprintf("%%rf.read.%d", g.tmpIdx)
		g.tmpIdx++
		readSel := fmt.Sprintf("%%rf.readsel.%d", g.tmpIdx)
		g.tmpIdx++
		strReg := fmt.Sprintf("%%rf.str.%d", g.tmpIdx)
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%rf.len.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%rf.data.gep.%d", g.tmpIdx)
		statL := g.statLayout()
		if sb != nil {
			// stat(path) → file size (0 on failure)
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %d\n", g.indent(), statBuf, statL.Size))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i8* %s, i8* %s)\n", g.indent(), statRet, g.libcFn("stat"), pathPtr, statBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), statCmp, statRet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %d\n", g.indent(), sizeGEP, statBuf, statL.SizeOff))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), sizeLoad, sizeGEP))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), sizeSel, statCmp, sizeLoad))
			// open(path, O_RDONLY=0, 0)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 (i8*, i32, ...) @%s(i8* %s, i32 0, i32 0)\n", g.indent(), openRet, g.libcFn("open"), pathPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), openCmp, openRet))
			// malloc(size)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, sizeSel))
			// read(fd, buf, size)
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @%s(i32 %s, i8* %s, i64 %s)\n", g.indent(), readRet, g.libcFn("read"), openRet, bufReg, sizeSel))
			// close(fd)
			sb.WriteString(fmt.Sprintf("%scall i32 @%s(i32 %s)\n", g.indent(), g.libcFn("close"), openRet))
			// If open failed, use 0 for read count
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), readSel, openCmp, readRet))
			// Construct %str-long {len, cap, data}
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), readSel, lenGEP))
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%readfile.cap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), readSel, capGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, bufReg, dataGEP)
		}
		return strReg
	}

	// write-file: write []byte data to a file (overwrite)
	// Returns i64 bool (0 or 1).
	if fnName == "write-file" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		// Extract data pointer and length from []byte (%vec) argument
		// %vec = { i64 len, i64 cap, i8* data }
		vecPtr := g.sliceEvalArgToPtr(sb, a[1])
		g.tmpIdx++
		wfLenGEP := fmt.Sprintf("%%wf.datalen.gep.%d", g.tmpIdx)
		g.tmpIdx++
		wfDataLen := fmt.Sprintf("%%wf.datalen.%d", g.tmpIdx)
		g.tmpIdx++
		wfDataGEP := fmt.Sprintf("%%wf.dataptr.gep.%d", g.tmpIdx)
		g.tmpIdx++
		wfDataPtr := fmt.Sprintf("%%wf.dataptr.%d", g.tmpIdx)
		g.tmpIdx++
		wfOpen := fmt.Sprintf("%%wf.open.%d", g.tmpIdx)
		g.tmpIdx++
		wfOpenCmp := fmt.Sprintf("%%wf.opencmp.%d", g.tmpIdx)
		g.tmpIdx++
		wfWrite := fmt.Sprintf("%%wf.write.%d", g.tmpIdx)
		g.tmpIdx++
		wfWriteSel := fmt.Sprintf("%%wf.writesel.%d", g.tmpIdx)
		g.tmpIdx++
		wfCmp := fmt.Sprintf("%%wf.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		wfZext := fmt.Sprintf("%%wf.zext.%d", g.tmpIdx)
		openFlags := g.openWriteFlags()
		if sb != nil {
			// Extract len and data ptr from %vec
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), wfLenGEP, vecPtr))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), wfDataLen, wfLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), wfDataGEP, vecPtr))
			wfDataPtr = g.loadDataPtrField(sb, wfDataGEP)
			// open(path, O_WRONLY|O_CREAT|O_TRUNC, 0644=420)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 (i8*, i32, ...) @%s(i8* %s, i32 %d, i32 420)\n", g.indent(), wfOpen, g.libcFn("open"), pathPtr, openFlags))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), wfOpenCmp, wfOpen))
			// write(fd, data, len)
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @%s(i32 %s, i8* %s, i64 %s)\n", g.indent(), wfWrite, g.libcFn("write"), wfOpen, wfDataPtr, wfDataLen))
			// If open failed, use -1 for write result
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 -1\n", g.indent(), wfWriteSel, wfOpenCmp, wfWrite))
			// close(fd)
			sb.WriteString(fmt.Sprintf("%scall i32 @%s(i32 %s)\n", g.indent(), g.libcFn("close"), wfOpen))
			// ok = (written == len)
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i64 %s, %s\n", g.indent(), wfCmp, wfWriteSel, wfDataLen))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), wfZext, wfCmp))
		}
		return wfZext
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
			// Allocate 4096 byte heap buffer (must be heap-allocated so emitHeapFree can safely free it)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 4096)\n", g.indent(), bufReg))
			// 使用 C 的 stdin 全域變數（macOS: __stdinp, Linux/WASI: stdin）
			// 避免在 macOS 上 fopen("/dev/stdin") 對 pipe/重定向不穩定的問題
			// 使用編譯目標平台（g.goos()）而非宿主平台，與 decl.go 宣告分派一致。
			stdinSym := "@stdin"
			if g.goos() == "darwin" {
				stdinSym = "@__stdinp"
			}
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), stdinReg, stdinSym))
			// fgets(buf, 4096, stdin)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @fgets(i8* %s, i32 4096, i8* %s)\n", g.indent(), fgetsReg, bufReg, stdinReg))
			// Check if fgets returned NULL
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i8* %s, null\n", g.indent(), cmpReg, fgetsReg))
			// strlen of buffer
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, bufReg))
			// Strip trailing newline: if last byte is '\n' (10), replace with '\0' and decrement len
			nlIdx := g.tmpIdx
			g.tmpIdx++
			nlCheckLab := fmt.Sprintf("getline.nlcheck.%d", nlIdx)
			nlStripLab := fmt.Sprintf("getline.nlstrip.%d", nlIdx)
			nlEndLab := fmt.Sprintf("getline.nlend.%d", nlIdx)
			nlCmpReg := fmt.Sprintf("%%getline.nlcmp.%d", nlIdx)
			nlLastIdxReg := fmt.Sprintf("%%getline.nlidx.%d", nlIdx)
			nlPtrReg := fmt.Sprintf("%%getline.nlptr.%d", nlIdx)
			nlByteReg := fmt.Sprintf("%%getline.nlbyte.%d", nlIdx)
			nlIsNlReg := fmt.Sprintf("%%getline.nlisnl.%d", nlIdx)
			nlSubReg := fmt.Sprintf("%%getline.nlsub.%d", nlIdx)
			nlLenReg := fmt.Sprintf("%%getline.nllen.%d", nlIdx)
			// 記錄分支前的基本塊（用於 PHI predecessor）
			prevBlock := g.currentBlock
			// 只有當 len > 0 時才檢查
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, 0\n", g.indent(), nlCmpReg, lenReg))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nlCmpReg, nlCheckLab, nlEndLab))
			// nlcheck: 檢查最後一個字節是否為 '\n'
			g.emitLabel(sb, nlCheckLab)
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), nlLastIdxReg, lenReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nlPtrReg, bufReg, nlLastIdxReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), nlByteReg, nlPtrReg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8 %s, 10\n", g.indent(), nlIsNlReg, nlByteReg))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nlIsNlReg, nlStripLab, nlEndLab))
			// nlstrip: 替換 '\n' 為 '\0' 並 len--
			g.emitLabel(sb, nlStripLab)
			sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nlPtrReg))
			sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), nlSubReg, lenReg))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), nlEndLab))
			// nlend: PHI 合併 len
			g.emitLabel(sb, nlEndLab)
			sb.WriteString(fmt.Sprintf("%s%s = phi i64 [ %s, %%%s ], [ %s, %%%s ], [ %s, %%%s ]\n", g.indent(), nlLenReg, lenReg, prevBlock, lenReg, nlCheckLab, nlSubReg, nlStripLab))
			// Create %str-long struct（使用已 strip 的長度）
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%str-long.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), nlLenReg, lenGEP))
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%getline.cap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), nlLenReg, capGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%str-long.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, bufReg, dataGEP)
		}
		// 記錄 ok 值（cmpReg）供 curried 呼叫使用 — zext i1 → i64 (Nolang bools are i64)
		g.tmpIdx++
		okZext := fmt.Sprintf("%%getline.ok.zext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), okZext, cmpReg))
		}
		g.lastBuiltinExtra = okZext
		return strReg
	}

	// ═══════════════════════════════════════════════
	// directory — 目錄操作
	// ═══════════════════════════════════════════════

	// open-dir: open a directory for reading entries
	// Returns: dirp i64 (0 on failure)
	if fnName == "open-dir" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		dirpReg := fmt.Sprintf("%%opendir.ret.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%opendir.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @opendir(i8* %s)\n", g.indent(), dirpReg, pathPtr))
			sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), extReg, dirpReg))
		}
		return extReg
	}

	// read-dir: read next directory entry name
	// Args: dirp i64 (from open-dir)
	// Returns: name str, ok bool (ok=false when no more entries)
	// macOS struct dirent: d_name at offset 12
	if fnName == "read-dir" && hasArgs {
		a := evalArgs()
		dirpVal := a[0]
		g.tmpIdx++
		dirpPtr := fmt.Sprintf("%%readdir.dirp.%d", g.tmpIdx)
		g.tmpIdx++
		entryReg := fmt.Sprintf("%%readdir.entry.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%readdir.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		nameGep := fmt.Sprintf("%%readdir.namegep.%d", g.tmpIdx)
		g.tmpIdx++
		safeName := fmt.Sprintf("%%readdir.safename.%d", g.tmpIdx)
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%readdir.len.%d", g.tmpIdx)
		g.tmpIdx++
		strReg := fmt.Sprintf("%%readdir.str.%d", g.tmpIdx)
		if sb != nil {
			// inttoptr i64 to i8* (DIR*)
			sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), dirpPtr, dirpVal))
			// readdir(dirp)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @readdir(i8* %s)\n", g.indent(), entryReg, dirpPtr))
			// Check if NULL (no more entries)
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i8* %s, null\n", g.indent(), cmpReg, entryReg))
			// d_name at offset 21 (macOS 64-bit struct dirent:
			//   d_ino(8) + d_seekoff(8) + d_reclen(2) + d_namlen(2) + d_type(1) = 21)
			// GEP on NULL is safe in LLVM IR (just pointer arithmetic, no memory access)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 21\n", g.indent(), nameGep, entryReg))
			// Select: if not NULL, use d_name pointer; otherwise use empty string global
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* %s, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.str.empty, i64 0, i64 0)\n",
				g.indent(), safeName, cmpReg, nameGep))
			// strlen on the safe pointer
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, safeName))
			// Copy d_name into a heap buffer (readdir returns static memory that
			// gets overwritten on the next call)
			g.tmpIdx++
			bufSize := fmt.Sprintf("%%readdir.bufsize.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), bufSize, lenReg))
			g.tmpIdx++
			nameBuf := fmt.Sprintf("%%readdir.namebuf.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), nameBuf, bufSize))
			// 使用 @llvm.memcpy 替代 @strcpy（避免 libc 依賴），bufSize = len + 1 包含 null terminator
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), nameBuf, safeName, bufSize))
			// Create %str-long struct { len, cap, data } pointing to the heap copy
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%readdir.lengep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%readdir.capgep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, capGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%readdir.datagep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, nameBuf, dataGEP)
		}
		// Store ok flag for curried call — zext i1 → i64 (Nolang bools are i64)
		g.tmpIdx++
		okZext := fmt.Sprintf("%%readdir.ok.zext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), okZext, cmpReg))
		}
		g.lastBuiltinExtra = okZext
		return strReg
	}

	// close-dir: close a directory handle
	// Returns: ok bool
	if fnName == "close-dir" && hasArgs {
		a := evalArgs()
		dirpVal := a[0]
		g.tmpIdx++
		dirpPtr := fmt.Sprintf("%%closedir.dirp.%d", g.tmpIdx)
		g.tmpIdx++
		retReg := fmt.Sprintf("%%closedir.ret.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%closedir.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%closedir.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), dirpPtr, dirpVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @closedir(i8* %s)\n", g.indent(), retReg, dirpPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, retReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	}

	// win-find-first-file: open a directory search on Windows (FindFirstFileA)
	// Args: path str (caller must append "\\*" wildcard in Nolang layer)
	// Returns: bufPtr i64 (0 on failure — INVALID_HANDLE_VALUE)
	// Buffer layout (332 bytes):
	//   [0..8)   HANDLE (i8*)
	//   [8..328) WIN32_FIND_DATAA (320 bytes, cFileName at offset 44)
	//   [328..332) consumed flag (i32: 0=first file not yet returned, 1=consumed)
	if fnName == "win-find-first-file" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])

		g.tmpIdx++
		bufReg := fmt.Sprintf("%%winff.buf.%d", g.tmpIdx)
		g.tmpIdx++
		findDataReg := fmt.Sprintf("%%winff.finddata.%d", g.tmpIdx)
		g.tmpIdx++
		handleReg := fmt.Sprintf("%%winff.handle.%d", g.tmpIdx)
		g.tmpIdx++
		handleStoreReg := fmt.Sprintf("%%winff.handlestore.%d", g.tmpIdx)
		g.tmpIdx++
		flagPtrReg := fmt.Sprintf("%%winff.flagptr.%d", g.tmpIdx)
		g.tmpIdx++
		flagPtrI32Reg := fmt.Sprintf("%%winff.flagptr.i32.%d", g.tmpIdx)
		g.tmpIdx++
		invalidCmpReg := fmt.Sprintf("%%winff.invalid.%d", g.tmpIdx)
		g.tmpIdx++
		bufI64Reg := fmt.Sprintf("%%winff.buf.i64.%d", g.tmpIdx)
		g.tmpIdx++
		resultReg := fmt.Sprintf("%%winff.result.%d", g.tmpIdx)

		failLabel := fmt.Sprintf("winff.fail.%d", g.tmpIdx)
		okLabel := fmt.Sprintf("winff.ok.%d", g.tmpIdx)
		mergeLabel := fmt.Sprintf("winff.merge.%d", g.tmpIdx)

		if sb != nil {
			// buf = malloc(332)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 332)\n", g.indent(), bufReg))
			// findData = buf + 8
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 8\n", g.indent(), findDataReg, bufReg))
			// handle = FindFirstFileA(path, findData)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @FindFirstFileA(i8* %s, i8* %s)\n", g.indent(), handleReg, pathPtr, findDataReg))
			// store handle at buf[0..8)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8**\n", g.indent(), handleStoreReg, bufReg))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), handleReg, handleStoreReg))
			// flagPtr = buf + 328; store 0 (first file not yet consumed)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 328\n", g.indent(), flagPtrReg, bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n", g.indent(), flagPtrI32Reg, flagPtrReg))
			sb.WriteString(fmt.Sprintf("%sstore i32 0, i32* %s\n", g.indent(), flagPtrI32Reg))
			// check INVALID_HANDLE_VALUE (i8* -1)
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, inttoptr (i64 -1 to i8*)\n", g.indent(), invalidCmpReg, handleReg))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), invalidCmpReg, failLabel, okLabel))

			// fail: free(buf), result = 0
			g.emitLabel(sb, failLabel)
			sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), bufReg))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// ok: result = ptrtoint(buf, i64)
			g.emitLabel(sb, okLabel)
			sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), bufI64Reg, bufReg))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// merge: phi result
			g.emitLabel(sb, mergeLabel)
			sb.WriteString(fmt.Sprintf("%s%s = phi i64 [ 0, %%%s ], [ %s, %%%s ]\n", g.indent(), resultReg, failLabel, bufI64Reg, okLabel))
		}
		return resultReg
	}

	// win-find-next-file: read next directory entry name on Windows (FindNextFileA)
	// Args: bufPtr i64 (from win-find-first-file)
	// Returns: name str, ok bool (ok=false when no more entries)
	// Uses consumed flag at buf+328: first call returns the file found by
	// FindFirstFileA (already in WIN32_FIND_DATAA); subsequent calls invoke
	// FindNextFileA.
	if fnName == "win-find-next-file" && hasArgs {
		a := evalArgs()
		bufPtrVal := a[0]

		g.tmpIdx++
		bufReg := fmt.Sprintf("%%winfnf.buf.%d", g.tmpIdx)
		g.tmpIdx++
		flagPtrReg := fmt.Sprintf("%%winfnf.flagptr.%d", g.tmpIdx)
		g.tmpIdx++
		flagPtrI32Reg := fmt.Sprintf("%%winfnf.flagptr.i32.%d", g.tmpIdx)
		g.tmpIdx++
		findDataReg := fmt.Sprintf("%%winfnf.finddata.%d", g.tmpIdx)
		g.tmpIdx++
		namePtrReg := fmt.Sprintf("%%winfnf.nameptr.%d", g.tmpIdx)
		g.tmpIdx++
		flagReg := fmt.Sprintf("%%winfnf.flag.%d", g.tmpIdx)
		g.tmpIdx++
		flagCmpReg := fmt.Sprintf("%%winfnf.flagcmp.%d", g.tmpIdx)

		// "next" block registers
		g.tmpIdx++
		handlePtrReg := fmt.Sprintf("%%winfnf.handleptr.%d", g.tmpIdx)
		g.tmpIdx++
		handleReg := fmt.Sprintf("%%winfnf.handle.%d", g.tmpIdx)
		g.tmpIdx++
		nextRetReg := fmt.Sprintf("%%winfnf.nextret.%d", g.tmpIdx)
		g.tmpIdx++
		nextOkReg := fmt.Sprintf("%%winfnf.nextok.%d", g.tmpIdx)

		// "build" block registers
		g.tmpIdx++
		lenReg := fmt.Sprintf("%%winfnf.len.%d", g.tmpIdx)
		g.tmpIdx++
		bufSizeReg := fmt.Sprintf("%%winfnf.bufsize.%d", g.tmpIdx)
		g.tmpIdx++
		nameBufReg := fmt.Sprintf("%%winfnf.namebuf.%d", g.tmpIdx)
		g.tmpIdx++
		strReg := fmt.Sprintf("%%winfnf.str.%d", g.tmpIdx)
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%winfnf.lengep.%d", g.tmpIdx)
		g.tmpIdx++
		capGEP := fmt.Sprintf("%%winfnf.capgep.%d", g.tmpIdx)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%winfnf.datagep.%d", g.tmpIdx)

		// "empty" block registers
		g.tmpIdx++
		emptyStrReg := fmt.Sprintf("%%winfnf.emptystr.%d", g.tmpIdx)
		g.tmpIdx++
		emptyLenGEP := fmt.Sprintf("%%winfnf.emptylengep.%d", g.tmpIdx)
		g.tmpIdx++
		emptyCapGEP := fmt.Sprintf("%%winfnf.emptycapgep.%d", g.tmpIdx)
		g.tmpIdx++
		emptyDataGEP := fmt.Sprintf("%%winfnf.emptydatagep.%d", g.tmpIdx)

		// "merge" block registers
		g.tmpIdx++
		finalStrReg := fmt.Sprintf("%%winfnf.finalstr.%d", g.tmpIdx)
		g.tmpIdx++
		okReg := fmt.Sprintf("%%winfnf.ok.%d", g.tmpIdx)
		g.tmpIdx++
		okZextReg := fmt.Sprintf("%%winfnf.okzext.%d", g.tmpIdx)

		firstLabel := fmt.Sprintf("winfnf.first.%d", g.tmpIdx)
		nextLabel := fmt.Sprintf("winfnf.next.%d", g.tmpIdx)
		buildLabel := fmt.Sprintf("winfnf.build.%d", g.tmpIdx)
		emptyLabel := fmt.Sprintf("winfnf.empty.%d", g.tmpIdx)
		mergeLabel := fmt.Sprintf("winfnf.merge.%d", g.tmpIdx)

		if sb != nil {
			// entry: load flag, branch on it
			sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), bufReg, bufPtrVal))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 328\n", g.indent(), flagPtrReg, bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n", g.indent(), flagPtrI32Reg, flagPtrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 8\n", g.indent(), findDataReg, bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 44\n", g.indent(), namePtrReg, findDataReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* %s\n", g.indent(), flagReg, flagPtrI32Reg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), flagCmpReg, flagReg))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), flagCmpReg, firstLabel, nextLabel))

			// first: first file from FindFirstFileA, mark consumed, go to build
			g.emitLabel(sb, firstLabel)
			sb.WriteString(fmt.Sprintf("%sstore i32 1, i32* %s\n", g.indent(), flagPtrI32Reg))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), buildLabel))

			// next: call FindNextFileA, branch on result
			g.emitLabel(sb, nextLabel)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8**\n", g.indent(), handlePtrReg, bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), handleReg, handlePtrReg))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @FindNextFileA(i8* %s, i8* %s)\n", g.indent(), nextRetReg, handleReg, findDataReg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i32 %s, 0\n", g.indent(), nextOkReg, nextRetReg))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nextOkReg, buildLabel, emptyLabel))

			// build: construct %str-long from cFileName (namePtr)
			g.emitLabel(sb, buildLabel)
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, namePtrReg))
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), bufSizeReg, lenReg))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), nameBufReg, bufSizeReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), nameBufReg, namePtrReg, bufSizeReg))
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, capGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, nameBufReg, dataGEP)
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// empty: construct empty %str-long (len=0, cap=0, data=0)
			g.emitLabel(sb, emptyLabel)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), emptyStrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), emptyLenGEP, emptyStrReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), emptyLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), emptyCapGEP, emptyStrReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), emptyCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), emptyDataGEP, emptyStrReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), emptyDataGEP))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// merge: PHI for str pointer and ok flag
			g.emitLabel(sb, mergeLabel)
			sb.WriteString(fmt.Sprintf("%s%s = phi %%str-long* [ %s, %%%s ], [ %s, %%%s ]\n", g.indent(), finalStrReg, strReg, buildLabel, emptyStrReg, emptyLabel))
			sb.WriteString(fmt.Sprintf("%s%s = phi i1 [ true, %%%s ], [ false, %%%s ]\n", g.indent(), okReg, buildLabel, emptyLabel))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), okZextReg, okReg))
		}
		g.lastBuiltinExtra = okZextReg
		return finalStrReg
	}

	// win-find-close: close a Windows directory search handle (FindClose + free)
	// Args: bufPtr i64 (from win-find-first-file)
	// Returns: ok bool (true on success — FindClose returns nonzero on success)
	if fnName == "win-find-close" && hasArgs {
		a := evalArgs()
		bufPtrVal := a[0]

		g.tmpIdx++
		bufReg := fmt.Sprintf("%%winfc.buf.%d", g.tmpIdx)
		g.tmpIdx++
		handlePtrReg := fmt.Sprintf("%%winfc.handleptr.%d", g.tmpIdx)
		g.tmpIdx++
		handleReg := fmt.Sprintf("%%winfc.handle.%d", g.tmpIdx)
		g.tmpIdx++
		retReg := fmt.Sprintf("%%winfc.ret.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%winfc.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%winfc.ext.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), bufReg, bufPtrVal))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8**\n", g.indent(), handlePtrReg, bufReg))
			sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), handleReg, handlePtrReg))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @FindClose(i8* %s)\n", g.indent(), retReg, handleReg))
			sb.WriteString(fmt.Sprintf("%scall void @free(i8* %s)\n", g.indent(), bufReg))
			// FindClose returns nonzero on success
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i32 %s, 0\n", g.indent(), cmpReg, retReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	}

	// win-wsa-startup: initialize Winsock 2.2 on Windows (WSAStartup)
	// Args: none
	// Returns: ok bool (true on success — WSAStartup returns 0 on success)
	// WSADATA structure is 408 bytes on Windows; allocated on stack via alloca.
	if fnName == "win-wsa-startup" {
		g.tmpIdx++
		wsaDataBuf := fmt.Sprintf("%%winwsa.data.%d", g.tmpIdx)
		g.tmpIdx++
		retReg := fmt.Sprintf("%%winwsa.ret.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%winwsa.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%winwsa.ext.%d", g.tmpIdx)
		if sb != nil {
			// WSADATA is 408 bytes; allocate on stack (only needed during the call).
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 408\n", g.indent(), wsaDataBuf))
			// WSAStartup(0x0202 /* Winsock 2.2 */, &wsaData)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @WSAStartup(i16 514, i8* %s)\n", g.indent(), retReg, wsaDataBuf))
			// WSAStartup returns 0 on success
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, retReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	}

	// touch-file: update file access and modification times to current time
	// Returns: ok bool
	// Windows: routed to @nolang.win_utimensat stub (no-op success).
	if fnName == "touch-file" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		g.tmpIdx++
		retReg := fmt.Sprintf("%%touch.ret.%d", g.tmpIdx)
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%touch.cmp.%d", g.tmpIdx)
		g.tmpIdx++
		extReg := fmt.Sprintf("%%touch.ext.%d", g.tmpIdx)
		// Windows: route to nolang.win_utimensat stub (no-op success).
		utimensatFn := "utimensat"
		if g.goos() == "windows" {
			utimensatFn = "nolang.win_utimensat"
		}
		if sb != nil {
			// utimensat(AT_FDCWD=-2, path, NULL, 0)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i32 -2, i8* %s, i8* null, i32 0)\n", g.indent(), retReg, utimensatFn, pathPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), cmpReg, retReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	}

	// get-arch: return compile-time architecture string (e.g. "arm64", "amd64")
	if fnName == "get-arch" {
		arch := runtime.GOARCH
		idx := g.stringIdx
		g.stringIdx++
		escaped := g.escapeLLVMString(arch)
		strLen := len(arch)
		g.fmtGlobals = append(g.fmtGlobals,
			fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", idx, strLen, escaped))
		g.tmpIdx++
		allocaReg := fmt.Sprintf("%%archstr.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), allocaReg))
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%archstr.len.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%archstr.cap.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, allocaReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, capGEP))
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%archstr.data.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, allocaReg))
			// malloc a heap buffer and copy the constant string (must be heap-allocated so emitHeapFree can safely free it)
			g.tmpIdx++
			archBuf := fmt.Sprintf("%%archstr.buf.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), archBuf, strLen+1))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0), i64 %d, i1 false)\n",
				g.indent(), archBuf, strLen, strLen, idx, strLen+1))
			g.storeDataPtrField(sb, archBuf, dataGEP)
		}
		return allocaReg
	}

	// gzip-compress / gzip-decompress / inflate-decompress 已改為純 Nolang 實現
	// （src/std/archive/gzip.no），不再使用 zlib 內置函數。舊的 ForwardFunc LLVM
	// 代碼生成（呼叫 @compress2 / @uncompress / @nolang.inflate_raw）已全部移除。

	// ═══════════════════════════════════════════════
	// process — 進程操作
	// ═══════════════════════════════════════════════

	// process-fork: fork current process
	// Returns: 0=child, >0=parent(child_pid), -1=error
	// Windows has no fork() equivalent; report a compile-time error directing
	// users to process-spawn (which wraps _execlp + _cwait on Windows).
	if fnName == "process-fork" {
		// Windows/WASI 不支援 fork()。decl.go 在這些目標下不會宣告 @fork，
		// 因此連結階段會產生 "undefined symbol: fork" 錯誤（符合 spec 行為）。
		// 使用者應使用 #{wasi-wasm32} 或 #{win-amd64,win-arm64} 標註提供替代方案。
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
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i32* %s)\n", g.indent(), pipeRet, g.libcFn("pipe"), pipeFds))
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
	// Windows: routed to @nolang.win_waitpid stub (reorders _cwait params and
	// normalizes status encoding; see decl.go writeWindowsStubs).
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
		// Windows: route to nolang.win_waitpid wrapper (parameter order + status
		// encoding normalized). Non-Windows uses libc waitpid directly.
		waitpidFn := libcFnFor(g.goos(), "waitpid")
		if g.goos() == "windows" {
			waitpidFn = "nolang.win_waitpid"
		}
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca i32\n", g.indent(), waitStatus))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), waitPidTrunc, pidVal))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), waitOptTrunc, optVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @%s(i32 %s, i32* %s, i32 %s)\n", g.indent(), waitRet, waitpidFn, waitPidTrunc, waitStatus, waitOptTrunc))
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
			sb.WriteString(fmt.Sprintf("%s%s = call i32 (i8*, ...) @%s(i8* %s, i8* %s, i8* %s, i8* null)\n", g.indent(), execRet, g.libcFn("execlp"), progPtr, progPtr, argPtr))
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
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", g.indent(), addrPtr))

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
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", g.indent(), addrPtr))

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
			g.emitLabel(sb, tryResolve)
			sb.WriteString(fmt.Sprintf("%s%s = alloca [48 x i8]\n", g.indent(), hintsReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [48 x i8], [48 x i8]* %s, i64 0, i64 0\n", g.indent(), hintsPtr, hintsReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 48, i1 false)\n", g.indent(), hintsPtr))
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

			// use_resolved: copy only sin_addr from ai_addr to our sockaddr_in
			// We copy only 4 bytes at offset 4 (sin_addr), NOT the full 16 bytes,
			// because getaddrinfo was called with NULL service so sin_port=0 in ai_addr,
			// and we already set sin_port correctly before the inet_pton branch.
			// Copying all 16 bytes would overwrite sin_port with 0, connecting to port 0.
			g.emitLabel(sb, useResolved)
			resVal = g.loadDataPtrField(sb, resReg)
			// macOS addrinfo layout: ai_addr at offset 32
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 32\n", g.indent(), aiAddrGep, resVal))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8**\n", g.indent(), aiAddrCast, aiAddrGep))
			aiAddr = g.loadDataPtrField(sb, aiAddrCast)
			// copy only sin_addr (4 bytes at offset 4) from ai_addr to our buffer
			g.tmpIdx++
			addrSinAddrGep := fmt.Sprintf("%%net.d.sinaddr.%d", g.tmpIdx)
			g.tmpIdx++
			aiAddrSinAddrGep := fmt.Sprintf("%%net.d.aiaddrsin.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), addrSinAddrGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), aiAddrSinAddrGep, aiAddr))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 4, i1 false)\n", g.indent(), addrSinAddrGep, aiAddrSinAddrGep))
			// freeaddrinfo
			sb.WriteString(fmt.Sprintf("%scall void @freeaddrinfo(i8* %s)\n", g.indent(), resVal))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), doSocket))

			// do_socket: socket + connect
			g.emitLabel(sb, doSocket)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 2, i32 1, i32 0)\n", g.indent(), sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), sockOk, sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @connect(i32 %s, i8* %s, i32 16)\n", g.indent(), connRet, sockFd, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), connOk, connRet))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), sockConnOk, sockOk, connOk))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// fail: result = -1
			g.emitLabel(sb, failLabel)
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))

			// merge: phi to determine final result
			g.emitLabel(sb, mergeLabel)
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
	// Args: fd i64, data str|[]byte, n i64
	// Returns: written i64 (-1 on error)
	if fnName == "net-send" && hasArgs && nArgs >= 3 {
		a := evalArgs()
		fdVal := a[0]

		var dataPtr string
		dataArgType := g.exprResultLLVMType(expr.Arguments[1])
		if dataArgType == "%vec" {
			// []byte: extract data pointer from vec field 2
			vecPtr := g.sliceEvalArgToPtr(sb, a[1])
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%net.s.datagep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%net.s.dataptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, vecPtr))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}
			dataPtr = dataLoad
		} else if dataArgType == "%arr" {
			// [n]byte: extract data pointer from arr field 1
			// resolve eval result to %arr* pointer (may need temp alloca for loaded values)
			arrEval := a[1]
			arrPtr := arrEval
			if idx := strings.Index(arrEval, ".val."); idx > 0 {
				// loaded value: store into temp %arr alloca to get a pointer
				baseRef := arrEval[:idx]
				varName := strings.TrimPrefix(baseRef, "%")
				_ = varName
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%net.s.arrtmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%net.s.arrgep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%net.s.arrptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, arrPtr))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}
			dataPtr = dataLoad
		} else {
			// str: use existing path
			dataPtr = g.extractStrFromEvalArg(sb, a[1])
		}

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
	// Args: fd i64, buf str|[]byte, n i64
	// Returns: read-n i64 (-1 on error, 0 on connection closed)
	if fnName == "net-recv" && hasArgs && nArgs >= 3 {
		a := evalArgs()
		fdVal := a[0]

		var bufPtr string
		bufArgType := g.exprResultLLVMType(expr.Arguments[1])
		if bufArgType == "%vec" {
			// []byte: extract data pointer from vec field 2
			vecPtr := g.sliceEvalArgToPtr(sb, a[1])
			g.tmpIdx++
			bufGEP := fmt.Sprintf("%%net.r.datagep.%d", g.tmpIdx)
			g.tmpIdx++
			bufLoad := fmt.Sprintf("%%net.r.dataptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), bufGEP, vecPtr))
				bufLoad = g.loadDataPtrField(sb, bufGEP)
			}
			bufPtr = bufLoad
		} else if bufArgType == "%arr" {
			// [n]byte: extract data pointer from arr field 1
			arrEval := a[1]
			arrPtr := arrEval
			if idx := strings.Index(arrEval, ".val."); idx > 0 {
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%net.r.arrtmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			g.tmpIdx++
			bufGEP := fmt.Sprintf("%%net.r.arrgep.%d", g.tmpIdx)
			g.tmpIdx++
			bufLoad := fmt.Sprintf("%%net.r.arrptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), bufGEP, arrPtr))
				bufLoad = g.loadDataPtrField(sb, bufGEP)
			}
			bufPtr = bufLoad
		} else {
			// str: use existing path
			bufPtr = g.extractStrFromEvalArg(sb, a[1])
		}

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

	// net-udp-open: create a UDP socket
	// Performs: socket(AF_INET, SOCK_DGRAM, 0)
	// Returns: fd i64 (-1 on error)
	if fnName == "net-udp-open" {
		g.tmpIdx++
		sockFd := fmt.Sprintf("%%net.udp.sock.%d", g.tmpIdx)
		g.tmpIdx++
		fdExt := fmt.Sprintf("%%net.udp.fdext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 2, i32 2, i32 0)\n", g.indent(), sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))
		}
		return fdExt
	}

	// net-udp-sendto: send UDP datagram
	// Args: fd i64, data str|[]byte, n i64, host str, port i64
	// Returns: written i64 (-1 on error)
	if fnName == "net-udp-sendto" && hasArgs && nArgs >= 5 {
		a := evalArgs()
		fdVal := a[0]
		nVal := a[2]
		hostPtr := g.makeNullTerminatedStr(sb, expr.Arguments[3])
		portVal := a[4]

		// extract data pointer (same logic as net-send)
		var dataPtr string
		dataArgType := g.exprResultLLVMType(expr.Arguments[1])
		if dataArgType == "%vec" {
			vecPtr := g.sliceEvalArgToPtr(sb, a[1])
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%net.us.datagep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%net.us.dataptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, vecPtr))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}
			dataPtr = dataLoad
		} else if dataArgType == "%arr" {
			arrEval := a[1]
			arrPtr := arrEval
			if idx := strings.Index(arrEval, ".val."); idx > 0 {
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%net.us.arrtmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%net.us.arrgep.%d", g.tmpIdx)
			g.tmpIdx++
			dataLoad := fmt.Sprintf("%%net.us.arrptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, arrPtr))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}
			dataPtr = dataLoad
		} else {
			dataPtr = g.extractStrFromEvalArg(sb, a[1])
		}

		// Build sockaddr_in (16 bytes)
		g.tmpIdx++
		addrReg := fmt.Sprintf("%%net.us.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%net.us.addrptr.%d", g.tmpIdx)
		g.tmpIdx++
		famGep := fmt.Sprintf("%%net.us.fam.%d", g.tmpIdx)
		g.tmpIdx++
		portLo := fmt.Sprintf("%%net.us.plo.%d", g.tmpIdx)
		g.tmpIdx++
		portHi := fmt.Sprintf("%%net.us.phi.%d", g.tmpIdx)
		g.tmpIdx++
		portHiM := fmt.Sprintf("%%net.us.phm.%d", g.tmpIdx)
		g.tmpIdx++
		portSl := fmt.Sprintf("%%net.us.psl.%d", g.tmpIdx)
		g.tmpIdx++
		portNet := fmt.Sprintf("%%net.us.pnet.%d", g.tmpIdx)
		g.tmpIdx++
		portI16 := fmt.Sprintf("%%net.us.pi16.%d", g.tmpIdx)
		g.tmpIdx++
		portGep := fmt.Sprintf("%%net.us.portgep.%d", g.tmpIdx)
		g.tmpIdx++
		portCast := fmt.Sprintf("%%net.us.portcast.%d", g.tmpIdx)
		g.tmpIdx++
		addrInGep := fmt.Sprintf("%%net.us.addringe.%d", g.tmpIdx)
		g.tmpIdx++
		ptonRet := fmt.Sprintf("%%net.us.pton.%d", g.tmpIdx)
		g.tmpIdx++
		ptonOk := fmt.Sprintf("%%net.us.ptonok.%d", g.tmpIdx)
		// getaddrinfo fallback registers
		g.tmpIdx++
		hintsReg := fmt.Sprintf("%%net.us.hints.%d", g.tmpIdx)
		g.tmpIdx++
		hintsPtr := fmt.Sprintf("%%net.us.hintsptr.%d", g.tmpIdx)
		g.tmpIdx++
		hintsFamGep := fmt.Sprintf("%%net.us.hintsfam.%d", g.tmpIdx)
		g.tmpIdx++
		hintsFamCast := fmt.Sprintf("%%net.us.hintsfamc.%d", g.tmpIdx)
		g.tmpIdx++
		hintsStGep := fmt.Sprintf("%%net.us.hintsst.%d", g.tmpIdx)
		g.tmpIdx++
		hintsStCast := fmt.Sprintf("%%net.us.hintsstc.%d", g.tmpIdx)
		g.tmpIdx++
		resReg := fmt.Sprintf("%%net.us.res.%d", g.tmpIdx)
		g.tmpIdx++
		gaiRet := fmt.Sprintf("%%net.us.gai.%d", g.tmpIdx)
		g.tmpIdx++
		gaiOk := fmt.Sprintf("%%net.us.gaiok.%d", g.tmpIdx)
		g.tmpIdx++
		resVal := fmt.Sprintf("%%net.us.resval.%d", g.tmpIdx)
		g.tmpIdx++
		aiAddrGep := fmt.Sprintf("%%net.us.aiaddrgep.%d", g.tmpIdx)
		g.tmpIdx++
		aiAddrCast := fmt.Sprintf("%%net.us.aiaddrcast.%d", g.tmpIdx)
		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%net.us.fdtrunc.%d", g.tmpIdx)
		g.tmpIdx++
		sendRet := fmt.Sprintf("%%net.us.ret.%d", g.tmpIdx)

		if sb != nil {
			// Basic block labels
			tryResolve := fmt.Sprintf("net.us.tryresolve.%d", g.tmpIdx)
			useResolved := fmt.Sprintf("net.us.useresolved.%d", g.tmpIdx)
			doSendto := fmt.Sprintf("net.us.dosendto.%d", g.tmpIdx)

			sb.WriteString(fmt.Sprintf("%s%s = alloca [16 x i8]\n", g.indent(), addrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", g.indent(), addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 1\n", g.indent(), famGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%sstore i8 2, i8* %s\n", g.indent(), famGep))
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, 255\n", g.indent(), portLo, portVal))
			sb.WriteString(fmt.Sprintf("%s%s = lshr i64 %s, 8\n", g.indent(), portHi, portVal))
			sb.WriteString(fmt.Sprintf("%s%s = and i64 %s, 255\n", g.indent(), portHiM, portHi))
			sb.WriteString(fmt.Sprintf("%s%s = shl i64 %s, 8\n", g.indent(), portSl, portLo))
			sb.WriteString(fmt.Sprintf("%s%s = or i64 %s, %s\n", g.indent(), portNet, portSl, portHiM))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i16\n", g.indent(), portI16, portNet))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 2\n", g.indent(), portGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i16*\n", g.indent(), portCast, portGep))
			sb.WriteString(fmt.Sprintf("%sstore i16 %s, i16* %s\n", g.indent(), portI16, portCast))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), addrInGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @inet_pton(i32 2, i8* %s, i8* %s)\n", g.indent(), ptonRet, hostPtr, addrInGep))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 1\n", g.indent(), ptonOk, ptonRet))
			// Branch: if inet_pton succeeded, go to do_sendto; else try DNS resolve
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), ptonOk, doSendto, tryResolve))

			// try_resolve: call getaddrinfo
			g.emitLabel(sb, tryResolve)
			sb.WriteString(fmt.Sprintf("%s%s = alloca [48 x i8]\n", g.indent(), hintsReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [48 x i8], [48 x i8]* %s, i64 0, i64 0\n", g.indent(), hintsPtr, hintsReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 48, i1 false)\n", g.indent(), hintsPtr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 4\n", g.indent(), hintsFamGep, hintsPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n", g.indent(), hintsFamCast, hintsFamGep))
			sb.WriteString(fmt.Sprintf("%sstore i32 2, i32* %s\n", g.indent(), hintsFamCast))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 8\n", g.indent(), hintsStGep, hintsPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i32*\n", g.indent(), hintsStCast, hintsStGep))
			sb.WriteString(fmt.Sprintf("%sstore i32 2, i32* %s\n", g.indent(), hintsStCast))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i8*\n", g.indent(), resReg))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @getaddrinfo(i8* %s, i8* null, i8* %s, i8** %s)\n", g.indent(), gaiRet, hostPtr, hintsPtr, resReg))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), gaiOk, gaiRet))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), gaiOk, useResolved, doSendto))

			// use_resolved: copy ai_addr to our sockaddr_in
			g.emitLabel(sb, useResolved)
			resVal = g.loadDataPtrField(sb, resReg)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 32\n", g.indent(), aiAddrGep, resVal))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i8**\n", g.indent(), aiAddrCast, aiAddrGep))
			aiAddr := g.loadDataPtrField(sb, aiAddrCast)
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 16, i1 false)\n", g.indent(), addrPtr, aiAddr))
			sb.WriteString(fmt.Sprintf("%scall void @freeaddrinfo(i8* %s)\n", g.indent(), resVal))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), doSendto))

			// do_sendto: call sendto
			g.emitLabel(sb, doSendto)
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, fdVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @sendto(i32 %s, i8* %s, i64 %s, i32 0, i8* %s, i32 16)\n", g.indent(), sendRet, fdTrunc, dataPtr, nVal, addrPtr))
		}
		return sendRet
	}

	// net-udp-recvfrom: receive UDP datagram
	// Args: fd i64, buf str|[]byte, n i64
	// Returns: read-n i64 (-1 on error, 0 on timeout)
	if fnName == "net-udp-recvfrom" && hasArgs && nArgs >= 3 {
		a := evalArgs()
		fdVal := a[0]

		var bufPtr string
		bufArgType := g.exprResultLLVMType(expr.Arguments[1])
		if bufArgType == "%vec" {
			vecPtr := g.sliceEvalArgToPtr(sb, a[1])
			g.tmpIdx++
			bufGEP := fmt.Sprintf("%%net.ur.datagep.%d", g.tmpIdx)
			g.tmpIdx++
			bufLoad := fmt.Sprintf("%%net.ur.dataptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), bufGEP, vecPtr))
				bufLoad = g.loadDataPtrField(sb, bufGEP)
			}
			bufPtr = bufLoad
		} else if bufArgType == "%arr" {
			arrEval := a[1]
			arrPtr := arrEval
			if idx := strings.Index(arrEval, ".val."); idx > 0 {
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%net.ur.arrtmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			g.tmpIdx++
			bufGEP := fmt.Sprintf("%%net.ur.arrgep.%d", g.tmpIdx)
			g.tmpIdx++
			bufLoad := fmt.Sprintf("%%net.ur.arrptr.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), bufGEP, arrPtr))
				bufLoad = g.loadDataPtrField(sb, bufGEP)
			}
			bufPtr = bufLoad
		} else {
			bufPtr = g.extractStrFromEvalArg(sb, a[1])
		}

		nVal := a[2]

		// Allocate sockaddr_in for source address (we don't use it, but recvfrom needs it)
		g.tmpIdx++
		addrReg := fmt.Sprintf("%%net.ur.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%net.ur.addrptr.%d", g.tmpIdx)
		g.tmpIdx++
		lenAlloca := fmt.Sprintf("%%net.ur.addrlen.%d", g.tmpIdx)
		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%net.ur.fdtrunc.%d", g.tmpIdx)
		g.tmpIdx++
		recvRet := fmt.Sprintf("%%net.ur.ret.%d", g.tmpIdx)

		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca [16 x i8]\n", g.indent(), addrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", g.indent(), addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = alloca i32\n", g.indent(), lenAlloca))
			sb.WriteString(fmt.Sprintf("%sstore i32 16, i32* %s\n", g.indent(), lenAlloca))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, fdVal))
			// recvfrom(fd, buf, n, 0, &addr, &addrlen)
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @recvfrom(i32 %s, i8* %s, i64 %s, i32 0, i8* %s, i32* %s)\n", g.indent(), recvRet, fdTrunc, bufPtr, nVal, addrPtr, lenAlloca))
		}
		return recvRet
	}

	// net-set-recv-timeout: set socket recv timeout (SO_RCVTIMEO)
	// Args: fd i64, timeout-ms i64
	// Returns: ok i64 (0=success, -1=error)
	if fnName == "net-set-recv-timeout" && hasArgs && nArgs >= 2 {
		a := evalArgs()
		fdVal := a[0]
		timeoutMs := a[1]

		g.tmpIdx++
		tvAlloca := fmt.Sprintf("%%net.to.tv.%d", g.tmpIdx)
		g.tmpIdx++
		tvPtr := fmt.Sprintf("%%net.to.tvptr.%d", g.tmpIdx)
		g.tmpIdx++
		secVal := fmt.Sprintf("%%net.to.sec.%d", g.tmpIdx)
		g.tmpIdx++
		remainMs := fmt.Sprintf("%%net.to.remain.%d", g.tmpIdx)
		g.tmpIdx++
		usecVal := fmt.Sprintf("%%net.to.usec.%d", g.tmpIdx)
		g.tmpIdx++
		tvSecGep := fmt.Sprintf("%%net.to.tvsec.%d", g.tmpIdx)
		g.tmpIdx++
		tvSecCast := fmt.Sprintf("%%net.to.tvsecc.%d", g.tmpIdx)
		g.tmpIdx++
		tvUsecGep := fmt.Sprintf("%%net.to.tvusec.%d", g.tmpIdx)
		g.tmpIdx++
		tvUsecCast := fmt.Sprintf("%%net.to.tvusecc.%d", g.tmpIdx)
		g.tmpIdx++
		fdTrunc := fmt.Sprintf("%%net.to.fdtrunc.%d", g.tmpIdx)
		g.tmpIdx++
		setRet := fmt.Sprintf("%%net.to.ret.%d", g.tmpIdx)
		g.tmpIdx++
		retExt := fmt.Sprintf("%%net.to.ext.%d", g.tmpIdx)

		if sb != nil {
			// struct timeval { long tv_sec, long tv_usec } = 16 bytes on 64-bit
			sb.WriteString(fmt.Sprintf("%s%s = alloca [16 x i8]\n", g.indent(), tvAlloca))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [16 x i8], [16 x i8]* %s, i64 0, i64 0\n", g.indent(), tvPtr, tvAlloca))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 16, i1 false)\n", g.indent(), tvPtr))
			// tv_sec = timeout_ms / 1000
			sb.WriteString(fmt.Sprintf("%s%s = sdiv i64 %s, 1000\n", g.indent(), secVal, timeoutMs))
			// tv_usec = (timeout_ms % 1000) * 1000
			sb.WriteString(fmt.Sprintf("%s%s = srem i64 %s, 1000\n", g.indent(), remainMs, timeoutMs))
			sb.WriteString(fmt.Sprintf("%s%s = mul i64 %s, 1000\n", g.indent(), usecVal, remainMs))
			// store tv_sec at offset 0
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 0\n", g.indent(), tvSecGep, tvPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i64*\n", g.indent(), tvSecCast, tvSecGep))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), secVal, tvSecCast))
			// store tv_usec at offset 8
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 8\n", g.indent(), tvUsecGep, tvPtr))
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to i64*\n", g.indent(), tvUsecCast, tvUsecGep))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), usecVal, tvUsecCast))
			// setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv))
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), fdTrunc, fdVal))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @setsockopt(i32 %s, i32 65535, i32 20, i8* %s, i32 16)\n", g.indent(), setRet, fdTrunc, tvPtr))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), retExt, setRet))
		}
		return retExt
	}

	// unix-listen: create Unix domain listening socket
	// Performs: unlink(path) + socket(AF_UNIX, SOCK_STREAM, 0) + bind + listen
	// Args: path str
	// Returns: fd i64 (-1 on error)
	if fnName == "unix-listen" && hasArgs && nArgs >= 1 {
		evalArgs()
		pathPtr := g.makeNullTerminatedStr(sb, expr.Arguments[0])
		pathLen := g.strLenFromExpr(sb, expr.Arguments[0])

		g.tmpIdx++
		addrReg := fmt.Sprintf("%%unix.l.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%unix.l.addrptr.%d", g.tmpIdx)

		// sun_family at offset 1 (macOS: sun_len=0, sun_family=1)
		g.tmpIdx++
		famGep := fmt.Sprintf("%%unix.l.fam.%d", g.tmpIdx)

		// sun_path at offset 2
		g.tmpIdx++
		pathGep := fmt.Sprintf("%%unix.l.path.%d", g.tmpIdx)

		// cap path length at 103
		g.tmpIdx++
		capCmp := fmt.Sprintf("%%unix.l.capcmp.%d", g.tmpIdx)
		g.tmpIdx++
		cappedLen := fmt.Sprintf("%%unix.l.caplen.%d", g.tmpIdx)

		// socket(AF_UNIX=1, SOCK_STREAM=1, 0)
		g.tmpIdx++
		sockFd := fmt.Sprintf("%%unix.l.sock.%d", g.tmpIdx)
		g.tmpIdx++
		sockOk := fmt.Sprintf("%%unix.l.sockok.%d", g.tmpIdx)

		// bind(fd, &addr, 106)
		g.tmpIdx++
		bindRet := fmt.Sprintf("%%unix.l.bind.%d", g.tmpIdx)
		g.tmpIdx++
		bindOk := fmt.Sprintf("%%unix.l.bindok.%d", g.tmpIdx)

		// listen(fd, 128)
		g.tmpIdx++
		listenRet := fmt.Sprintf("%%unix.l.listen.%d", g.tmpIdx)
		g.tmpIdx++
		listenOk := fmt.Sprintf("%%unix.l.listenok.%d", g.tmpIdx)

		g.tmpIdx++
		fdExt := fmt.Sprintf("%%unix.l.fdext.%d", g.tmpIdx)
		g.tmpIdx++
		ok1 := fmt.Sprintf("%%unix.l.ok1.%d", g.tmpIdx)
		g.tmpIdx++
		ok2 := fmt.Sprintf("%%unix.l.ok2.%d", g.tmpIdx)
		g.tmpIdx++
		resultReg := fmt.Sprintf("%%unix.l.result.%d", g.tmpIdx)

		if sb != nil {
			// unlink(path) — remove existing socket file, ignore result
			sb.WriteString(fmt.Sprintf("%scall i32 @%s(i8* %s)\n", g.indent(), g.libcFn("unlink"), pathPtr))

			// allocate sockaddr_un (110 bytes), zero it
			sb.WriteString(fmt.Sprintf("%s%s = alloca [110 x i8]\n", g.indent(), addrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [110 x i8], [110 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 110, i1 false)\n", g.indent(), addrPtr))

			// sun_len = 106 at offset 0 (macOS)
			sb.WriteString(fmt.Sprintf("%sstore i8 106, i8* %s\n", g.indent(), addrPtr))

			// sun_family = AF_UNIX (1) at offset 1 (macOS)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 1\n", g.indent(), famGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%sstore i8 1, i8* %s\n", g.indent(), famGep))

			// copy path to offset 2, capped at 103 bytes
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 2\n", g.indent(), pathGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, 103\n", g.indent(), capCmp, pathLen))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 103, i64 %s\n", g.indent(), cappedLen, capCmp, pathLen))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), pathGep, pathPtr, cappedLen))

			// socket(AF_UNIX=1, SOCK_STREAM=1, 0)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 1, i32 1, i32 0)\n", g.indent(), sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), sockOk, sockFd))

			// bind(fd, &addr, 106)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @bind(i32 %s, i8* %s, i32 106)\n", g.indent(), bindRet, sockFd, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), bindOk, bindRet))

			// listen(fd, 128)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @listen(i32 %s, i32 128)\n", g.indent(), listenRet, sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), listenOk, listenRet))

			// fd as i64
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))

			// all.ok
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), ok1, sockOk, bindOk))
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), ok2, ok1, listenOk))

			// result
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 -1\n", g.indent(), resultReg, ok2, fdExt))
		}
		return resultReg
	}

	// unix-dial: connect to Unix domain socket
	// Performs: socket(AF_UNIX, SOCK_STREAM, 0) + connect
	// Args: path str
	// Returns: fd i64 (-1 on error)
	if fnName == "unix-dial" && hasArgs && nArgs >= 1 {
		evalArgs()
		pathPtr := g.makeNullTerminatedStr(sb, expr.Arguments[0])
		pathLen := g.strLenFromExpr(sb, expr.Arguments[0])

		g.tmpIdx++
		addrReg := fmt.Sprintf("%%unix.d.addr.%d", g.tmpIdx)
		g.tmpIdx++
		addrPtr := fmt.Sprintf("%%unix.d.addrptr.%d", g.tmpIdx)

		// sun_family at offset 1 (macOS: sun_len=0, sun_family=1)
		g.tmpIdx++
		famGep := fmt.Sprintf("%%unix.d.fam.%d", g.tmpIdx)

		// sun_path at offset 2
		g.tmpIdx++
		pathGep := fmt.Sprintf("%%unix.d.path.%d", g.tmpIdx)

		// cap path length at 103
		g.tmpIdx++
		capCmp := fmt.Sprintf("%%unix.d.capcmp.%d", g.tmpIdx)
		g.tmpIdx++
		cappedLen := fmt.Sprintf("%%unix.d.caplen.%d", g.tmpIdx)

		// socket(AF_UNIX=1, SOCK_STREAM=1, 0)
		g.tmpIdx++
		sockFd := fmt.Sprintf("%%unix.d.sock.%d", g.tmpIdx)
		g.tmpIdx++
		sockOk := fmt.Sprintf("%%unix.d.sockok.%d", g.tmpIdx)

		// connect(fd, &addr, 106)
		g.tmpIdx++
		connRet := fmt.Sprintf("%%unix.d.conn.%d", g.tmpIdx)
		g.tmpIdx++
		connOk := fmt.Sprintf("%%unix.d.connok.%d", g.tmpIdx)

		g.tmpIdx++
		fdExt := fmt.Sprintf("%%unix.d.fdext.%d", g.tmpIdx)
		g.tmpIdx++
		ok1 := fmt.Sprintf("%%unix.d.ok1.%d", g.tmpIdx)
		g.tmpIdx++
		resultReg := fmt.Sprintf("%%unix.d.result.%d", g.tmpIdx)

		if sb != nil {
			// allocate sockaddr_un (110 bytes), zero it
			sb.WriteString(fmt.Sprintf("%s%s = alloca [110 x i8]\n", g.indent(), addrReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [110 x i8], [110 x i8]* %s, i64 0, i64 0\n", g.indent(), addrPtr, addrReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 110, i1 false)\n", g.indent(), addrPtr))

			// sun_len = 106 at offset 0 (macOS)
			sb.WriteString(fmt.Sprintf("%sstore i8 106, i8* %s\n", g.indent(), addrPtr))

			// sun_family = AF_UNIX (1) at offset 1 (macOS)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 1\n", g.indent(), famGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%sstore i8 1, i8* %s\n", g.indent(), famGep))

			// copy path to offset 2, capped at 103 bytes
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 2\n", g.indent(), pathGep, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sgt i64 %s, 103\n", g.indent(), capCmp, pathLen))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 103, i64 %s\n", g.indent(), cappedLen, capCmp, pathLen))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), pathGep, pathPtr, cappedLen))

			// socket(AF_UNIX=1, SOCK_STREAM=1, 0)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 1, i32 1, i32 0)\n", g.indent(), sockFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), sockOk, sockFd))

			// connect(fd, &addr, 106)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @connect(i32 %s, i8* %s, i32 106)\n", g.indent(), connRet, sockFd, addrPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), connOk, connRet))

			// fd as i64
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))

			// all.ok
			sb.WriteString(fmt.Sprintf("%s%s = and i1 %s, %s\n", g.indent(), ok1, sockOk, connOk))

			// result
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 -1\n", g.indent(), resultReg, ok1, fdExt))
		}
		return resultReg
	}

	// net-icmp-open: create an ICMP socket for ping
	// macOS:  socket(AF_INET, SOCK_DGRAM, IPPROTO_ICMP)  — unprivileged
	// Linux:  socket(AF_INET, SOCK_RAW,  IPPROTO_ICMP)  — requires root or CAP_NET_RAW
	// Returns: fd i64 (-1 on error)
	if fnName == "net-icmp-open" {
		if sb == nil {
			return ""
		}
		// SOCK_DGRAM=2 on macOS, SOCK_RAW=3 on Linux
		sockType := int32(3) // SOCK_RAW (Linux)
		if g.goos() == "darwin" {
			sockType = 2 // SOCK_DGRAM (macOS unprivileged ICMP)
		}
		g.tmpIdx++
		sockFd := fmt.Sprintf("%%net.icmp.sock.%d", g.tmpIdx)
		g.tmpIdx++
		fdExt := fmt.Sprintf("%%net.icmp.fdext.%d", g.tmpIdx)
		// socket(AF_INET=2, sockType, IPPROTO_ICMP=1)
		sb.WriteString(fmt.Sprintf("%s%s = call i32 @socket(i32 2, i32 %d, i32 1)\n", g.indent(), sockFd, sockType))
		sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), fdExt, sockFd))
		return fdExt
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
				if t == "%str-long" {
					dataPtr = g.extractStrDataPtr(sb, g.varAddr(a.Value))
				} else if t == "%option" {
					// ?str variable: generateExprWithSB extracts the option data
					// field as a %str-long* pointer (for struct inner types).
					innerType := "i64"
					if g.optionInnerTypes != nil {
						if it, ok := g.optionInnerTypes[a.Value]; ok {
							innerType = it
						}
					}
					if innerType == "%str-long" {
						ptr := g.generateExprWithSB(sb, a)
						dataPtr = g.extractStrDataPtr(sb, ptr)
					}
				}
			}
		}
	case *parser.StringLiteral:
		ptr := g.generateExprWithSB(sb, a)
		dataPtr = g.extractStrDataPtr(sb, ptr)
	case *parser.InfixExpression:
		if (a.Operator == "-" || a.Operator == "+") && (g.isStringExpr(a.Left) || g.isStringExpr(a.Right)) {
			ptr := g.generateStrConcat(sb, a.Left, a.Right)
			dataPtr = g.extractStrDataPtr(sb, ptr)
		}
	case *parser.DotExpression:
		// .field 或 obj.field：generateDotExpression 會載入 struct 值到 SSA register
		// 對於 str 欄位，返回的是 %str-long SSA value。需先 alloca 再 store 以取得指標。
		ptr := g.generateExprWithSB(sb, a)
		et := g.exprResultLLVMType(a)
		if et == "%str-long" {
			g.tmpIdx++
			tmpAlloca := fmt.Sprintf("%%str-long.dot.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
			dataPtr = g.extractStrDataPtr(sb, tmpAlloca)
		}
	case *parser.IndexExpression:
		// String element from a []str slice (e.g. files[i]).
		// generateIndexExpression loads the %str-long value; materialize it
		// into a temp alloca to obtain a %str-long* for data pointer extraction.
		if ident, ok := a.Left.(*parser.Identifier); ok {
			if et, ok := g.arrayElemTypes[ident.Value]; ok && et == "%str-long" {
				ptr := g.generateExprWithSB(sb, a)
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%str-long.idx.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
				sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
				dataPtr = g.extractStrDataPtr(sb, tmpAlloca)
			}
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

	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), buf, dataPtr, strLen))

	return buf
}

// loadStderr loads the FILE* for stderr and returns the register name.
// macOS: @__stderrp, Linux/Windows/WASI: @stderr
// 使用編譯目標平台（g.goos()）而非宿主平台，與 decl.go 宣告分派一致。
func (g *Generator) loadStderr(sb *strings.Builder) string {
	g.tmpIdx++
	reg := fmt.Sprintf("%%stderr.ptr.%d", g.tmpIdx)
	var globalName string
	if g.goos() == "darwin" {
		globalName = "@__stderrp"
	} else {
		globalName = "@stderr"
	}
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), reg, globalName))
	}
	return reg
}

// shouldInterceptNamedFormat determines whether callFmt should intercept the
// call for named format string processing. Returns (intercept, autoNewline).
//
// Interception rule: only when the first arg is a StringLiteral containing
// '{' (a potential {name:spec} field). This preserves backward compatibility
// with C-style printf('...%d...', args) whose format string has no '{'.
//
// - printf/eprintf/sprintf/format: intercept when first arg is a StringLiteral
//   containing '{'. autoNewline=false (user includes \n explicitly, or returns
//   the string for sprintf/format).
// - print/eprint: intercept only when there is exactly 1 StringLiteral arg
//   whose value contains '{'. autoNewline=true (print adds trailing \n).
//
// printf/eprintf/sprintf are deprecated; prefer print/eprint/format + io.out/io.err.
func (g *Generator) shouldInterceptNamedFormat(fnName string, expr *parser.CallExpression) (bool, bool) {
	strLit, ok := expr.Arguments[0].(*parser.StringLiteral)
	if !ok {
		return false, false
	}
	if !strings.Contains(strLit.Value, "{") {
		return false, false
	}
	switch fnName {
	case "printf", "fmt.printf", "eprintf", "fmt.eprintf", "sprintf", "fmt.sprintf",
		"format", "fmt.format":
		return true, false
	case "print", "fmt.print", "eprint", "fmt.eprint":
		// Only intercept single-arg calls whose format string contains '{'
		// (potential field). Multi-arg calls go through variadic path.
		if len(expr.Arguments) != 1 {
			return false, false
		}
		return true, true
	}
	return false, false
}

// callNamedFormat generates LLVM IR for a named format string call.
// Dispatches each {name:spec} field to the appropriate fmt-* helper
// (fmt-int/fmt-uint/fmt-f64/fmt-str/fmt-bool) and outputs via io.out/io.err
// (for printf/eprintf/print/eprint) or returns the concatenated string
// (for sprintf/format).
//
// segments: parsed format segments (literals and fields)
// autoNewline: if true, append a trailing "\n" (for print/eprint)
func (g *Generator) callNamedFormat(sb *strings.Builder, fnName string, segments []parser.FormatSegment, autoNewline bool) string {
	if autoNewline {
		segments = append(segments, parser.FormatSegment{Literal: "\n"})
	}

	useStderr := strings.HasPrefix(fnName, "eprint") || strings.HasPrefix(fnName, "fmt.eprint")
	isReturnStr := fnName == "sprintf" || fnName == "fmt.sprintf" ||
		fnName == "format" || fnName == "fmt.format"

	// Build each segment as a %str-long* alloca
	segPtrs := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg.Field != nil {
			segPtr := g.generateFieldStr(sb, seg.Field)
			segPtrs = append(segPtrs, segPtr)
		} else if seg.Literal != "" {
			segPtr := g.buildStrLongFromValue(sb, seg.Literal)
			segPtrs = append(segPtrs, segPtr)
		}
	}

	if isReturnStr {
		// Concatenate all segments into a single %str-long* (sprintf/format)
		if len(segPtrs) == 0 {
			return g.buildStrLongFromValue(sb, "")
		}
		result := segPtrs[0]
		for i := 1; i < len(segPtrs); i++ {
			result = g.concatStrLongPtrs(sb, result, segPtrs[i])
		}
		return result
	}

	// printf/eprintf/print/eprint: write each segment via io.out/io.err
	outFn := "out"
	if useStderr {
		outFn = "err"
	}
	for _, segPtr := range segPtrs {
		g.tmpIdx++
		discardedN := fmt.Sprintf("%%vso.tmp.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), discardedN))
		sb.WriteString(fmt.Sprintf("%scall void @%s(%%str-long* %s, i64* %s)\n",
			g.indent(), outFn, segPtr, discardedN))
	}
	// Return "0" (valid in phi nodes) to prevent fallthrough to other
	// handlers. The actual calls are written to sb.
	return "0"
}

// buildStrLongFromValue creates a %str-long* alloca from a string value.
// Uses the StringLiteral generation path (malloc + memcpy + struct init).
func (g *Generator) buildStrLongFromValue(sb *strings.Builder, s string) string {
	return g.generateExprWithSB(sb, &parser.StringLiteral{Value: s})
}

// generateFieldStr generates code to format a single {name:spec} field.
// Looks up the variable's LLVM type, dispatches to the appropriate fmt-*
// helper, and returns a %str-long* alloca holding the formatted result.
func (g *Generator) generateFieldStr(sb *strings.Builder, field *parser.FormatField) string {
	varType, ok := g.varTypes[field.Name]
	if !ok {
		// Variable not in scope — ValidatePrintFormat should have caught this.
		// Emit an empty string as fallback.
		return g.buildStrLongFromValue(sb, "")
	}

	// Build spec %str-long*
	specPtr := g.buildStrLongFromValue(sb, field.Spec)

	// Allocate output buffer and zero-initialize it.
	// fmt-* helpers perform move-assignment to `out` (out = fmt-apply-spec(...)),
	// which frees the old value of `out` before assigning. An uninitialized
	// buffer would contain stack garbage, causing the free of a bogus data
	// pointer to crash (SIGABRT). Per project convention, the caller must
	// initialize output parameters: str → {len=0, cap=0, data=null}.
	g.tmpIdx++
	outBuf := fmt.Sprintf("%%vso.tmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), outBuf))
	sb.WriteString(fmt.Sprintf("%sstore %%str-long zeroinitializer, %%str-long* %s\n", g.indent(), outBuf))

	// Determine spec type character
	specType := byte(0)
	if field.Parsed != nil {
		specType = field.Parsed.Type
	}

	// Dispatch based on spec type and variable LLVM type
	switch {
	case specType == 'b' || specType == 'o' || specType == 'x' || specType == 'X':
		// Unsigned format — use fmt-uint (expects i64*)
		argPtr := g.emitFmtArgPtr(sb, field.Name, varType, "i64")
		sb.WriteString(fmt.Sprintf("%scall void @fmt-uint(i64* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), argPtr, specPtr, outBuf))
	case varType == "double":
		// fmt-f64(double* x, %str-long* spec, %str-long* out)
		argPtr := g.emitFmtArgPtr(sb, field.Name, varType, "double")
		sb.WriteString(fmt.Sprintf("%scall void @fmt-f64(double* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), argPtr, specPtr, outBuf))
	case varType == "%str-long":
		// fmt-str(%str-long* s, %str-long* spec, %str-long* out)
		argPtr := g.varAddr(field.Name)
		sb.WriteString(fmt.Sprintf("%scall void @fmt-str(%%str-long* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), argPtr, specPtr, outBuf))
	case varType == "i1":
		// fmt-bool(i1* b, %str-long* spec, %str-long* out)
		argPtr := g.varAddr(field.Name)
		sb.WriteString(fmt.Sprintf("%scall void @fmt-bool(i1* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), argPtr, specPtr, outBuf))
	default:
		// Integer — fmt-int(i64* n, %str-long* spec, %str-long* out)
		argPtr := g.emitFmtArgPtr(sb, field.Name, varType, "i64")
		sb.WriteString(fmt.Sprintf("%scall void @fmt-int(i64* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), argPtr, specPtr, outBuf))
	}
	return outBuf
}

// emitFmtArgPtr returns a pointer to a value of targetType, coercing if necessary.
// targetType is the LLVM type string (e.g., "i64", "double").
// If the variable's LLVM type matches targetType, returns its address directly.
// Otherwise, loads the value, converts (zext/sext), and stores to a temp alloca.
func (g *Generator) emitFmtArgPtr(sb *strings.Builder, varName, varType, targetType string) string {
	addr := g.varAddr(varName)
	if varType == targetType {
		return addr
	}
	// Load value, convert, store to temp alloca of targetType
	g.tmpIdx++
	tmpAlloca := fmt.Sprintf("%%fmtarg.%d", g.tmpIdx)
	g.tmpIdx++
	loadReg := fmt.Sprintf("%%fmtarg.val.%d", g.tmpIdx)
	g.tmpIdx++
	convReg := fmt.Sprintf("%%fmtarg.conv.%d", g.tmpIdx)

	sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpAlloca, targetType))
	sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), loadReg, varType, varType, addr))

	switch {
	case varType == "i8" && targetType == "i64":
		sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), convReg, loadReg))
	case varType == "i16" && targetType == "i64":
		sb.WriteString(fmt.Sprintf("%s%s = sext i16 %s to i64\n", g.indent(), convReg, loadReg))
	case varType == "i32" && targetType == "i64":
		sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), convReg, loadReg))
	case varType == "i1" && targetType == "i64":
		sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), convReg, loadReg))
	case varType == "i64" && targetType == "double":
		// Integer to double conversion (sitofp)
		sb.WriteString(fmt.Sprintf("%s%s = sitofp i64 %s to double\n", g.indent(), convReg, loadReg))
	default:
		// No conversion needed — store directly (type matches)
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), targetType, loadReg, targetType, tmpAlloca))
		return tmpAlloca
	}
	sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), targetType, convReg, targetType, tmpAlloca))
	return tmpAlloca
}

// concatStrLongPtrs concatenates two %str-long* allocas and returns a new
// %str-long* alloca holding the result. Uses malloc + memcpy.
func (g *Generator) concatStrLongPtrs(sb *strings.Builder, leftPtr, rightPtr string) string {
	leftLen := g.extractStrLen(sb, leftPtr)
	rightLen := g.extractStrLen(sb, rightPtr)
	leftData := g.extractStrDataPtr(sb, leftPtr)
	rightData := g.extractStrDataPtr(sb, rightPtr)

	g.tmpIdx++
	totalLen := fmt.Sprintf("%%nfmt.concat.total.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), totalLen, leftLen, rightLen))

	g.tmpIdx++
	allocSize := fmt.Sprintf("%%nfmt.concat.alloc.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), allocSize, totalLen))

	g.tmpIdx++
	bufPtr := fmt.Sprintf("%%nfmt.concat.buf.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufPtr, allocSize))

	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), bufPtr, leftData, leftLen))

	g.tmpIdx++
	dstOffset := fmt.Sprintf("%%nfmt.concat.dst.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), dstOffset, bufPtr, leftLen))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), dstOffset, rightData, rightLen))

	g.tmpIdx++
	nullPos := fmt.Sprintf("%%nfmt.concat.null.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nullPos, bufPtr, totalLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))

	g.tmpIdx++
	resultAlloca := fmt.Sprintf("%%nfmt.concat.result.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), resultAlloca))

	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%nfmt.concat.len.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	g.tmpIdx++
	capGEP := fmt.Sprintf("%%nfmt.concat.cap.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, capGEP))

	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%nfmt.concat.data.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, resultAlloca))
	g.storeDataPtrField(sb, bufPtr, dataGEP)

	return resultAlloca
}
