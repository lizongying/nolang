package llvm

import (
	"fmt"
	"os"
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

	// === C-style format string path (multi-arg) ===
	// Handle sprintf/printf/eprintf/format with C-style format specifiers
	// (e.g., sprintf('%d', x), printf('%s=%d', s, n)).
	// Converts each %d/%s/%x/etc. to emitArgAsStrLong and concatenates.
	if sb != nil && hasArgs && len(expr.Arguments) >= 2 {
		if strLit, ok := expr.Arguments[0].(*parser.StringLiteral); ok {
			if strings.Contains(strLit.Value, "%") && !strings.Contains(strLit.Value, "{") {
				switch fnName {
				case "printf", "fmt.printf", "eprintf", "fmt.eprintf", "sprintf", "fmt.sprintf",
					"format", "fmt.format":
					useStderr := strings.HasPrefix(fnName, "eprint") || strings.HasPrefix(fnName, "fmt.eprint")
					isReturnStr := fnName == "sprintf" || fnName == "fmt.sprintf" ||
						fnName == "format" || fnName == "fmt.format"

					fmtStr := strLit.Value
					var segPtrs []string
					argIdx := 1
					litStart := 0
					for i := 0; i < len(fmtStr); i++ {
						if fmtStr[i] != '%' || i+1 >= len(fmtStr) {
							continue
						}
						// Literal segment before %
						if i > litStart {
							segPtrs = append(segPtrs, g.buildStrLongFromValue(sb, fmtStr[litStart:i]))
						}
						j := i + 1
						if fmtStr[j] == '%' {
							segPtrs = append(segPtrs, g.buildStrLongFromValue(sb, "%"))
							i = j
							litStart = j + 1
							continue
						}
						// Scan spec: flags, width, precision, conversion char
						for j < len(fmtStr) {
							c := fmtStr[j]
							if c == 'd' || c == 'i' || c == 'u' || c == 's' ||
								c == 'x' || c == 'X' || c == 'b' || c == 'o' ||
								c == 'c' || c == 'f' || c == 'e' || c == 'g' {
								j++
								break
							}
							if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
								(c >= '0' && c <= '9') || c == '.' || c == '-' ||
								c == '+' || c == '#' || c == ' ' || c == '0' {
								j++
							} else {
								break
							}
						}
						spec := fmtStr[i+1 : j]
						if argIdx < len(expr.Arguments) {
							argPtr := g.emitArgAsStrLong(sb, expr.Arguments[argIdx], spec)
							if argPtr != "" {
								segPtrs = append(segPtrs, argPtr)
							}
							argIdx++
						}
						i = j - 1
						litStart = j
					}
					if litStart < len(fmtStr) {
						segPtrs = append(segPtrs, g.buildStrLongFromValue(sb, fmtStr[litStart:]))
					}

					if isReturnStr {
						if len(segPtrs) == 0 {
							ptr := g.buildStrLongFromValue(sb, "")
							loadReg := g.tmpReg("sprintf.result")
							sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, ptr))
							return loadReg
						}
						result := segPtrs[0]
						for k := 1; k < len(segPtrs); k++ {
							result = g.concatStrLongPtrs(sb, result, segPtrs[k])
						}
						// Load %str-long value from pointer so assignment
						// codegen can store it directly (not the pointer).
						loadReg := g.tmpReg("sprintf.result")
						sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), loadReg, result))
						return loadReg
					}
					outFn := "out"
					if useStderr {
						outFn = "err"
					}
					for _, segPtr := range segPtrs {
						discardedN := g.tmpReg("vso.tmp")
						sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), discardedN))
						sb.WriteString(fmt.Sprintf("%scall void @%s(%%str-long* %s, i64* %s)\n",
							g.indent(), outFn, segPtr, discardedN))
					}
					return "0"
				}
			}
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
			// If emitArgAsStrLong returns "" (e.g. void-call argument that could
			// not be materialized into a value), skip emitting @out — otherwise
			// emitOutCall would produce malformed IR `call void @out(%str-long* , ...)`
			// with an empty first operand, which `opt` rejects as "expected value token".
			argPtr := g.emitArgAsStrLong(sb, arg, "")
			if argPtr != "" {
				g.emitOutCall(sb, useStderr, argPtr)
			}
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
		// Skip @out if argPtr is empty (void-call argument) to avoid
		// malformed IR with missing first operand.
		if argPtr != "" {
			g.emitOutCall(sb, false, argPtr)
		}
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
	nAlloca := g.tmpReg("vso.n")
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
	if v == "" {
		// 表达式求值返回空值（通常是 void 函数被当作表达式使用）
		// 输出编译错误而非生成空操作数 IR
		fmt.Fprintf(os.Stderr, "codegen error: expression produced empty value in emitArgAsStrLong (expr: %T)\n", expr)
		return ""
	}

	// String-returning CallExpression (e.g. energy().to-str()):
	// exprResultLLVMType may fail for chained calls; after generation,
	// ssaTypes[v] holds the actual return type — if %str-long, materialize
	// into a temp alloca and return it like other string expressions.
	if _, isCall := expr.(*parser.CallExpression); isCall && g.ssaTypes != nil {
		if t, ok := g.ssaTypes[v]; ok && t == "%str-long" {
			tmpAlloca := g.tmpReg("str-long.arg")
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
	outBuf := g.tmpReg("vso.tmp")
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
			extReg := g.tmpReg("arg.ext")
			sb.WriteString(fmt.Sprintf("%s%s = zext %s %s to i64\n", g.indent(), extReg, srcType, v))
			v = extReg
		}
		valAlloca := g.tmpReg("fmtval")
		sb.WriteString(fmt.Sprintf("%s%s = alloca i64\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-uint(i64* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	case srcType == "double":
		// fmt-f64(double* x, %str-long* spec, %str-long* out)
		valAlloca := g.tmpReg("fmtval")
		sb.WriteString(fmt.Sprintf("%s%s = alloca double\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore double %s, double* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-f64(double* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	case srcType == "i1":
		// fmt-bool(i1* b, %str-long* spec, %str-long* out)
		valAlloca := g.tmpReg("fmtval")
		sb.WriteString(fmt.Sprintf("%s%s = alloca i1\n", g.indent(), valAlloca))
		sb.WriteString(fmt.Sprintf("%sstore i1 %s, i1* %s\n", g.indent(), v, valAlloca))
		sb.WriteString(fmt.Sprintf("%scall void @fmt-bool(i1* %s, %%str-long* %s, %%str-long* %s)\n",
			g.indent(), valAlloca, specPtr, outBuf))
	default:
		// Integer — fmt-int(i64* n, %str-long* spec, %str-long* out).
		// Coerce narrow integers to i64 with sext (signed semantics, matches
		// the previous printVariadic behavior for %lld).
		if srcType == "i8" || srcType == "i16" || srcType == "i32" {
			extReg := g.tmpReg("arg.ext")
			sb.WriteString(fmt.Sprintf("%s%s = sext %s %s to i64\n", g.indent(), extReg, srcType, v))
			v = extReg
		}
		valAlloca := g.tmpReg("fmtval")
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
		cmpReg := g.tmpReg("boolcmp.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @nolang.memcmp(i8* %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i64 5)\n",
				g.indent(), cmpReg, a[0]))
		}
		eqReg := g.tmpReg("booleq.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), eqReg, cmpReg))
		}
		zextReg := g.tmpReg("boolzext.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, eqReg))
		}
		return zextReg
	}

	// bool.to-str: select + malloc + memcpy + 构造 %str-long
	// Must heap-allocate the data buffer so emitHeapFree can safely free it.
	if fnName == "bool.to-str" && hasArgs {
		a := evalArgs()
		selectReg := g.tmpReg("boolstr.tmp")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.true, i64 0, i64 0), i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.str.false, i64 0, i64 0)\n",
				g.indent(), selectReg, a[0]))
		}
		// "true" = 4, "false" = 5; use select directly
		lenReg := g.tmpReg("boolstr.len")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 4, i64 5\n", g.indent(), lenReg, a[0]))
		}
		// Allocate heap buffer (6 bytes, enough for "false\0")
		bufReg := g.tmpReg("boolstr.buf")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 6)\n", g.indent(), bufReg))
		}
		// Copy length including null terminator: "true\0" = 5, "false\0" = 6
		copyLenReg := g.tmpReg("boolstr.copylen")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 5, i64 6\n", g.indent(), copyLenReg, a[0]))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
				g.indent(), bufReg, selectReg, copyLenReg))
		}
		// Construct %str-long { len, cap, data } with heap-allocated data
		strReg1 := g.tmpReg("boolstr.val")
		strReg2 := g.tmpReg("boolstr.val")
		strReg3 := g.tmpReg("boolstr.val")
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
				tmpAlloca := g.tmpReg("str-long.nt")
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
	sizeReg := g.tmpReg("nt.size")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), sizeReg, strLen))
	}
	buf := g.tmpReg("nt.buf")
	if sb != nil {
		sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), buf, sizeReg))
		// Null-terminate
		nullEnd := g.tmpReg("nt.end")
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
						lenGEP := g.tmpReg("builtin.len.gep")
						lenReg := g.tmpReg("builtin.len")
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
					if t, ok := g.varTypes[ident.Value]; ok && g.isStructLLVMType(t) {
						structName := strings.TrimPrefix(t, "%")
						if fields, ok := g.structTypes[structName]; ok {
							for _, f := range fields {
								if f.name == dot.Property && (f.typ == "%vec" || f.typ == "%arr") {
									a := evalArgs()
									lenReg := g.tmpReg("builtin.len")
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
		lenReg := g.tmpReg("builtin.len")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenReg, arg))
		}
		return lenReg
	}

	if fnName == "cap" && hasArgs {
		a := evalArgs()
		arg := a[0]
		capGEP := g.tmpReg("builtin.cap.gep")
		capReg := g.tmpReg("builtin.cap")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr i64, i64* %s, i64 1\n", g.indent(), capGEP, arg))
			sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), capReg, capGEP))
		}
		return capReg
	}

	// args-count: 返回命令行參數數量
	if fnName == "args-count" {
		loadReg := g.tmpReg("argc")
		extReg := g.tmpReg("argc.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load i32, i32* @.argc.addr\n", g.indent(), loadReg))
			sb.WriteString(fmt.Sprintf("%s%s = zext i32 %s to i64\n", g.indent(), extReg, loadReg))
		}
		return extReg
	}

	// args-get: 返回第 idx 個命令行參數
	if fnName == "args-get" && hasArgs {
		a := evalArgs()
		argvReg := g.tmpReg("argv.load")
		gepReg := g.tmpReg("argv.gep")
		ptrReg := g.tmpReg("argv.ptr")
		lenReg := g.tmpReg("argv.len")
		strReg := g.tmpReg("argv.str")
		bufReg := g.tmpReg("argv.buf")
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
			lenGEP := g.tmpReg("str-long.len")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			// Store cap (field 1) = lenReg
			capGEP := g.tmpReg("str-long.cap")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, capGEP))
			// Allocate heap buffer and memcpy (must be heap-allocated so emitHeapFree can safely free it)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, lenReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), bufReg, ptrReg, lenReg))
			// Store data pointer (field 2)
			dataGEP := g.tmpReg("str-long.data")
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
		statBuf := g.tmpReg("statbuf")
		statRet := g.tmpReg("stat.ret")
		cmpReg := g.tmpReg("stat.cmp")
		modeGEP := g.tmpReg("stat.mode")
		modeLoad := g.tmpReg("stat.mode.ld")
		andReg := g.tmpReg("stat.and")
		cmp2 := g.tmpReg("stat.cmp2")
		extReg := g.tmpReg("stat.ext")
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
			zextReg := g.tmpReg("stat.zext")
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, extReg))
			return zextReg
		}
	}

	// is-file: 判斷路徑是否為普通檔案
	// Mirrors is-dir but masks with S_IFREG (0100000 = 0x8000 = 32768)
	if (fnName == "is-file" || fnName == "stat-file") && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		statBuf := g.tmpReg("statbuf.sf")
		statRet := g.tmpReg("stat.ret.sf")
		cmpReg := g.tmpReg("stat.cmp.sf")
		modeGEP := g.tmpReg("stat.mode.sf")
		modeLoad := g.tmpReg("stat.mode.ld.sf")
		andReg := g.tmpReg("stat.and.sf")
		cmp2 := g.tmpReg("stat.cmp2.sf")
		extReg := g.tmpReg("stat.ext.sf")
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
			zextReg := g.tmpReg("stat.zext.sf")
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), zextReg, extReg))
			return zextReg
		}
	}

	// stat-size / file-size: 獲取文件大小
	if (fnName == "stat-size" || fnName == "file-size") && hasArgs {
		a := evalArgs()
		// 從 %str-long 參數提取 i8* 資料指針
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		statBuf := g.tmpReg("statbuf")
		statRet := g.tmpReg("stat.ret")
		cmpReg := g.tmpReg("stat.cmp")
		sizeGEP := g.tmpReg("stat.size")
		sizeLoad := g.tmpReg("stat.size.ld")
		selReg := g.tmpReg("stat.sel")
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
		statBuf := g.tmpReg("statbuf.sm")
		statRet := g.tmpReg("stat.ret.sm")
		cmpReg := g.tmpReg("stat.cmp.sm")
		modeGEP := g.tmpReg("stat.mode.gep")
		modeLoad := g.tmpReg("stat.mode.ld")
		modeZext := g.tmpReg("stat.mode.zext")
		selReg := g.tmpReg("stat.mode.sel")
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
		statBuf := g.tmpReg("statbuf.su")
		statRet := g.tmpReg("stat.ret.su")
		cmpReg := g.tmpReg("stat.cmp.su")
		uidGEP := g.tmpReg("stat.uid.gep")
		uidLoad := g.tmpReg("stat.uid.ld")
		uidZext := g.tmpReg("stat.uid.zext")
		selReg := g.tmpReg("stat.uid.sel")
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
		statBuf := g.tmpReg("statbuf.sg")
		statRet := g.tmpReg("stat.ret.sg")
		cmpReg := g.tmpReg("stat.cmp.sg")
		gidGEP := g.tmpReg("stat.gid.gep")
		gidLoad := g.tmpReg("stat.gid.ld")
		gidZext := g.tmpReg("stat.gid.zext")
		selReg := g.tmpReg("stat.gid.sel")
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
		statBuf := g.tmpReg("statbuf.smt")
		statRet := g.tmpReg("stat.ret.smt")
		cmpReg := g.tmpReg("stat.cmp.smt")
		mtimeGEP := g.tmpReg("stat.mtime.gep")
		mtimeLoad := g.tmpReg("stat.mtime.ld")
		selReg := g.tmpReg("stat.mtime.sel")
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
		statBuf := g.tmpReg("rf.statbuf")
		statRet := g.tmpReg("rf.statret")
		statCmp := g.tmpReg("rf.statcmp")
		sizeGEP := g.tmpReg("rf.sizegep")
		sizeLoad := g.tmpReg("rf.sizeld")
		sizeSel := g.tmpReg("rf.size")
		openRet := g.tmpReg("rf.open")
		openCmp := g.tmpReg("rf.opencmp")
		bufReg := g.tmpReg("rf.buf")
		readRet := g.tmpReg("rf.read")
		readSel := g.tmpReg("rf.readsel")
		strReg := g.tmpReg("rf.str")
		lenGEP := g.tmpReg("rf.len.gep")
		dataGEP := g.tmpReg("rf.data.gep")
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
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufReg, sizeSel))
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
			capGEP := g.tmpReg("readfile.cap")
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
		wfLenGEP := g.tmpReg("wf.datalen.gep")
		wfDataLen := g.tmpReg("wf.datalen")
		wfDataGEP := g.tmpReg("wf.dataptr.gep")
		wfDataPtr := g.tmpReg("wf.dataptr")
		wfOpen := g.tmpReg("wf.open")
		wfOpenCmp := g.tmpReg("wf.opencmp")
		wfWrite := g.tmpReg("wf.write")
		wfWriteSel := g.tmpReg("wf.writesel")
		wfCmp := g.tmpReg("wf.cmp")
		wfZext := g.tmpReg("wf.zext")
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
		bufReg := g.tmpReg("getline.buf")
		stdinReg := g.tmpReg("getline.stdin")
		fgetsReg := g.tmpReg("getline.fgets")
		cmpReg := g.tmpReg("getline.cmp")
		lenReg := g.tmpReg("getline.len")
		strReg := g.tmpReg("getline.str")
		if sb != nil {
			// Allocate 4096 byte heap buffer (must be heap-allocated so emitHeapFree can safely free it)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 4096)\n", g.indent(), bufReg))
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
			lenGEP := g.tmpReg("str-long.len")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), nlLenReg, lenGEP))
			capGEP := g.tmpReg("getline.cap")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), nlLenReg, capGEP))
			dataGEP := g.tmpReg("str-long.data")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, bufReg, dataGEP)
		}
		// 記錄 ok 值（cmpReg）供 curried 呼叫使用 — zext i1 → i64 (Nolang bools are i64)
		okZext := g.tmpReg("getline.ok.zext")
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
		dirpReg := g.tmpReg("opendir.ret")
		extReg := g.tmpReg("opendir.ext")
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
		dirpPtr := g.tmpReg("readdir.dirp")
		entryReg := g.tmpReg("readdir.entry")
		cmpReg := g.tmpReg("readdir.cmp")
		nameGep := g.tmpReg("readdir.namegep")
		safeName := g.tmpReg("readdir.safename")
		lenReg := g.tmpReg("readdir.len")
		strReg := g.tmpReg("readdir.str")
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
			bufSize := g.tmpReg("readdir.bufsize")
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), bufSize, lenReg))
			nameBuf := g.tmpReg("readdir.namebuf")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), nameBuf, bufSize))
			// 使用 @llvm.memcpy 替代 @strcpy（避免 libc 依賴），bufSize = len + 1 包含 null terminator
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), nameBuf, safeName, bufSize))
			// Create %str-long struct { len, cap, data } pointing to the heap copy
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), strReg))
			lenGEP := g.tmpReg("readdir.lengep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, lenGEP))
			capGEP := g.tmpReg("readdir.capgep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, strReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), lenReg, capGEP))
			dataGEP := g.tmpReg("readdir.datagep")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, strReg))
			g.storeDataPtrField(sb, nameBuf, dataGEP)
		}
		// Store ok flag for curried call — zext i1 → i64 (Nolang bools are i64)
		okZext := g.tmpReg("readdir.ok.zext")
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
		dirpPtr := g.tmpReg("closedir.dirp")
		retReg := g.tmpReg("closedir.ret")
		cmpReg := g.tmpReg("closedir.cmp")
		extReg := g.tmpReg("closedir.ext")
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

		bufReg := g.tmpReg("winff.buf")
		findDataReg := g.tmpReg("winff.finddata")
		handleReg := g.tmpReg("winff.handle")
		handleStoreReg := g.tmpReg("winff.handlestore")
		flagPtrReg := g.tmpReg("winff.flagptr")
		flagPtrI32Reg := g.tmpReg("winff.flagptr.i32")
		invalidCmpReg := g.tmpReg("winff.invalid")
		bufI64Reg := g.tmpReg("winff.buf.i64")
		resultReg := g.tmpReg("winff.result")

		failLabel := fmt.Sprintf("winff.fail.%d", g.tmpIdx)
		okLabel := fmt.Sprintf("winff.ok.%d", g.tmpIdx)
		mergeLabel := fmt.Sprintf("winff.merge.%d", g.tmpIdx)

		if sb != nil {
			// buf = malloc(332)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 332)\n", g.indent(), bufReg))
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

		bufReg := g.tmpReg("winfnf.buf")
		flagPtrReg := g.tmpReg("winfnf.flagptr")
		flagPtrI32Reg := g.tmpReg("winfnf.flagptr.i32")
		findDataReg := g.tmpReg("winfnf.finddata")
		namePtrReg := g.tmpReg("winfnf.nameptr")
		flagReg := g.tmpReg("winfnf.flag")
		flagCmpReg := g.tmpReg("winfnf.flagcmp")

		// "next" block registers
		handlePtrReg := g.tmpReg("winfnf.handleptr")
		handleReg := g.tmpReg("winfnf.handle")
		nextRetReg := g.tmpReg("winfnf.nextret")
		nextOkReg := g.tmpReg("winfnf.nextok")

		// "build" block registers
		lenReg := g.tmpReg("winfnf.len")
		bufSizeReg := g.tmpReg("winfnf.bufsize")
		nameBufReg := g.tmpReg("winfnf.namebuf")
		strReg := g.tmpReg("winfnf.str")
		lenGEP := g.tmpReg("winfnf.lengep")
		capGEP := g.tmpReg("winfnf.capgep")
		dataGEP := g.tmpReg("winfnf.datagep")

		// "empty" block registers
		emptyStrReg := g.tmpReg("winfnf.emptystr")
		emptyLenGEP := g.tmpReg("winfnf.emptylengep")
		emptyCapGEP := g.tmpReg("winfnf.emptycapgep")
		emptyDataGEP := g.tmpReg("winfnf.emptydatagep")

		// "merge" block registers
		finalStrReg := g.tmpReg("winfnf.finalstr")
		okReg := g.tmpReg("winfnf.ok")
		okZextReg := g.tmpReg("winfnf.okzext")

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
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), nameBufReg, bufSizeReg))
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

		bufReg := g.tmpReg("winfc.buf")
		handlePtrReg := g.tmpReg("winfc.handleptr")
		handleReg := g.tmpReg("winfc.handle")
		retReg := g.tmpReg("winfc.ret")
		cmpReg := g.tmpReg("winfc.cmp")
		extReg := g.tmpReg("winfc.ext")

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
		wsaDataBuf := g.tmpReg("winwsa.data")
		retReg := g.tmpReg("winwsa.ret")
		cmpReg := g.tmpReg("winwsa.cmp")
		extReg := g.tmpReg("winwsa.ext")
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
		retReg := g.tmpReg("touch.ret")
		cmpReg := g.tmpReg("touch.cmp")
		extReg := g.tmpReg("touch.ext")
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
		allocaReg := g.tmpReg("archstr")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), allocaReg))
			lenGEP := g.tmpReg("archstr.len")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, allocaReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, lenGEP))
			capGEP := g.tmpReg("archstr.cap")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, allocaReg))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), strLen, capGEP))
			dataGEP := g.tmpReg("archstr.data")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, allocaReg))
			// malloc a heap buffer and copy the constant string (must be heap-allocated so emitHeapFree can safely free it)
			archBuf := g.tmpReg("archstr.buf")
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %d)\n", g.indent(), archBuf, strLen+1))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.str.%d, i64 0, i64 0), i64 %d, i1 false)\n",
				g.indent(), archBuf, strLen, strLen, idx, strLen+1))
			g.storeDataPtrField(sb, archBuf, dataGEP)
		}
		return allocaReg
	}

	// ═══════════════════════════════════════════════
	// notools Unix 工具集擴展：fs / os 新增 ForwardFunc 實作
	// ═══════════════════════════════════════════════

	// realpath: resolve absolute path (POSIX realpath(3))
	// Returns: %str-long* (empty string on failure)
	if fnName == "realpath" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		rpRet := g.tmpReg("rp.ptr")
		rpCmp := g.tmpReg("rp.cmp")
		rpSafe := g.tmpReg("rp.safe")
		rpLen := g.tmpReg("rp.len")
		rpBufSize := g.tmpReg("rp.bufsize")
		rpBuf := g.tmpReg("rp.buf")
		rpStr := g.tmpReg("rp.str")
		rpLenGEP := g.tmpReg("rp.lengep")
		rpCapGEP := g.tmpReg("rp.capgep")
		rpDataGEP := g.tmpReg("rp.datagep")
		if sb != nil {
			// call realpath(p, NULL) — NULL means libc allocates the result buffer
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @realpath(i8* %s, i8* null)\n", g.indent(), rpRet, pathPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i8* %s, null\n", g.indent(), rpCmp, rpRet))
			// If NULL, use empty string global (avoids passing NULL to strlen/memcpy)
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* %s, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.str.empty, i64 0, i64 0)\n",
				g.indent(), rpSafe, rpCmp, rpRet))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), rpLen, rpSafe))
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), rpBufSize, rpLen))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), rpBuf, rpBufSize))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
				g.indent(), rpBuf, rpSafe, rpBufSize))
			// Construct %str-long { len, cap, data }
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), rpStr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), rpLenGEP, rpStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), rpLen, rpLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), rpCapGEP, rpStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), rpLen, rpCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), rpDataGEP, rpStr))
			g.storeDataPtrField(sb, rpBuf, rpDataGEP)
		}
		// Load %str-long value from pointer so assignment codegen can store it directly.
		rpLoad := g.tmpReg("rp.load")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), rpLoad, rpStr))
		}
		return rpLoad
	}

	// readlink: read symbolic link target (POSIX readlink(2))
	// Returns: (target str, ok bool)
	if fnName == "readlink" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		rlBuf := g.tmpReg("rl.buf")
		rlRet := g.tmpReg("rl.ret")
		rlCmp := g.tmpReg("rl.cmp")
		rlLen := g.tmpReg("rl.len")
		rlStr := g.tmpReg("rl.str")
		rlLenGEP := g.tmpReg("rl.lengep")
		rlCapGEP := g.tmpReg("rl.capgep")
		rlDataGEP := g.tmpReg("rl.datagep")
		rlOkZext := g.tmpReg("rl.ok")
		if sb != nil {
			// Allocate 4096-byte buffer for readlink
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 4096)\n", g.indent(), rlBuf))
			// readlink(path, buf, 4096) → number of bytes read (or -1 on error)
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @readlink(i8* %s, i8* %s, i64 4096)\n",
				g.indent(), rlRet, pathPtr, rlBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i64 %s, 0\n", g.indent(), rlCmp, rlRet))
			// If error, use 0 for length
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), rlLen, rlCmp, rlRet))
			// On error, use empty string; on success, use rlBuf
			// We keep the malloc'd buffer even on error (it'll be freed by emitHeapFree)
			// Construct %str-long { len, cap, data }
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), rlStr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), rlLenGEP, rlStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), rlLen, rlLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), rlCapGEP, rlStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 4096, i64* %s\n", g.indent(), rlCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), rlDataGEP, rlStr))
			g.storeDataPtrField(sb, rlBuf, rlDataGEP)
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), rlOkZext, rlCmp))
		}
		g.lastBuiltinExtra = rlOkZext
		return rlStr
	}

	// mkstemp: create and open a unique temporary file (POSIX mkstemp(3))
	// Returns: (fd, name). fd=-1 and empty name on failure.
	if fnName == "mkstemp" && hasArgs {
		a := evalArgs()
		tmplPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		// Need to copy template into mutable buffer (mkstemp modifies in place)
		msLen := g.tmpReg("ms.len")
		msBufSize := g.tmpReg("ms.bufsize")
		msBuf := g.tmpReg("ms.buf")
		msRet := g.tmpReg("ms.ret")
		msCmp := g.tmpReg("ms.cmp")
		msSafe := g.tmpReg("ms.safe")
		msActualLen := g.tmpReg("ms.alen")
		msSelLen := g.tmpReg("ms.sellen")
		msStr := g.tmpReg("ms.str")
		msLenGEP := g.tmpReg("ms.lengep")
		msCapGEP := g.tmpReg("ms.capgep")
		msDataGEP := g.tmpReg("ms.datagep")
		msFd := g.tmpReg("ms.fd")
		if sb != nil {
			// strlen(tmpl) → len
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), msLen, tmplPtr))
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), msBufSize, msLen))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), msBuf, msBufSize))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
				g.indent(), msBuf, tmplPtr, msBufSize))
			// mkstemp(buf) → fd
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @mkstemp(i8* %s)\n", g.indent(), msRet, msBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp sge i32 %s, 0\n", g.indent(), msCmp, msRet))
			// If fd < 0, use empty string global for name; else use msBuf
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* %s, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.str.empty, i64 0, i64 0)\n",
				g.indent(), msSafe, msCmp, msBuf))
			// strlen of actual name (after mkstemp modifies XXXXXX)
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), msActualLen, msSafe))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), msSelLen, msCmp, msActualLen))
			// Construct %str-long { len, cap, data }
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), msStr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), msLenGEP, msStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), msSelLen, msLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), msCapGEP, msStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), msSelLen, msCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), msDataGEP, msStr))
			g.storeDataPtrField(sb, msBuf, msDataGEP)
			// sext fd to i64
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), msFd, msRet))
		}
		// mkstemp returns (name str, fd fd): first return = name str (pointer, loaded by
		// multi-assignment codegen), lastBuiltinExtra = fd i64.
		// Return order is (name, fd) — not (fd, name) — because lastBuiltinExtra is
		// hardcoded to i64 in the multi-assignment path; str must be the primary return.
		g.lastBuiltinExtra = msFd
		return msStr
	}

	// mkdtemp: create a unique temporary directory (POSIX mkdtemp(3))
	// Returns: (name str, ok bool)
	if fnName == "mkdtemp" && hasArgs {
		a := evalArgs()
		tmplPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		mdLen := g.tmpReg("md.len")
		mdBufSize := g.tmpReg("md.bufsize")
		mdBuf := g.tmpReg("md.buf")
		mdRet := g.tmpReg("md.ret")
		mdCmp := g.tmpReg("md.cmp")
		mdSafe := g.tmpReg("md.safe")
		mdActualLen := g.tmpReg("md.alen")
		mdSelLen := g.tmpReg("md.sellen")
		mdStr := g.tmpReg("md.str")
		mdLenGEP := g.tmpReg("md.lengep")
		mdCapGEP := g.tmpReg("md.capgep")
		mdDataGEP := g.tmpReg("md.datagep")
		mdOkZext := g.tmpReg("md.ok")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), mdLen, tmplPtr))
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), mdBufSize, mdLen))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), mdBuf, mdBufSize))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
				g.indent(), mdBuf, tmplPtr, mdBufSize))
			// mkdtemp(buf) → i8* (NULL on failure)
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @mkdtemp(i8* %s)\n", g.indent(), mdRet, mdBuf))
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i8* %s, null\n", g.indent(), mdCmp, mdRet))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* %s, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.str.empty, i64 0, i64 0)\n",
				g.indent(), mdSafe, mdCmp, mdBuf))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), mdActualLen, mdSafe))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 0\n", g.indent(), mdSelLen, mdCmp, mdActualLen))
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), mdStr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), mdLenGEP, mdStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), mdSelLen, mdLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), mdCapGEP, mdStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), mdSelLen, mdCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), mdDataGEP, mdStr))
			g.storeDataPtrField(sb, mdBuf, mdDataGEP)
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), mdOkZext, mdCmp))
		}
		g.lastBuiltinExtra = mdOkZext
		return mdStr
	}

	// utime: set file access and modification times (POSIX utimes(2))
	// Returns: ok bool
	if fnName == "utime" && hasArgs {
		a := evalArgs()
		pathPtr := g.nullTerminateStrArg(sb, a[0], expr.Arguments[0])
		utBuf := g.tmpReg("ut.buf")
		utAtimeGEP := g.tmpReg("ut.atime")
		utMtimeGEP := g.tmpReg("ut.mtime")
		utRet := g.tmpReg("ut.ret")
		utCmp := g.tmpReg("ut.cmp")
		utExt := g.tmpReg("ut.ext")
		if sb != nil {
			// alloca [32 x i8] for two struct timeval (each 16 bytes on macOS/Linux 64-bit)
			// Layout: times[0].tv_sec@0(i64), times[0].tv_usec@8(i64, upper 4 bytes are padding on macOS),
			//         times[1].tv_sec@16(i64), times[1].tv_usec@24(i64)
			sb.WriteString(fmt.Sprintf("%s%s = alloca [32 x i8]\n", g.indent(), utBuf))
			// Store atime at offset 0
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [32 x i8], [32 x i8]* %s, i64 0, i64 0\n", g.indent(), utAtimeGEP, utBuf))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), a[1], utAtimeGEP))
			// Store mtime at offset 16
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [32 x i8], [32 x i8]* %s, i64 0, i64 16\n", g.indent(), utMtimeGEP, utBuf))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), a[2], utMtimeGEP))
			// utimes(path, times) — tv_usec fields at offsets 8 and 24 are zero (from alloca zero-init is NOT guaranteed,
			// but utimes only reads tv_sec and tv_usec; the padding bytes don't matter)
			// Actually alloca doesn't zero-initialize, so let's explicitly zero tv_usec fields
			utUsec0GEP := g.tmpReg("ut.usec0")
			utUsec1GEP := g.tmpReg("ut.usec1")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [32 x i8], [32 x i8]* %s, i64 0, i64 8\n", g.indent(), utUsec0GEP, utBuf))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), utUsec0GEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [32 x i8], [32 x i8]* %s, i64 0, i64 24\n", g.indent(), utUsec1GEP, utBuf))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), utUsec1GEP))
			// Cast [32 x i8]* to i8* for the utimes call
			utTimesPtr := g.tmpReg("ut.timesptr")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [32 x i8], [32 x i8]* %s, i64 0, i64 0\n", g.indent(), utTimesPtr, utBuf))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @utimes(i8* %s, i8* %s)\n", g.indent(), utRet, pathPtr, utTimesPtr))
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i32 %s, 0\n", g.indent(), utCmp, utRet))
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), utExt, utCmp))
		}
		return utExt
	}

	// getgroups: get supplementary group IDs (POSIX getgroups(2))
	// Returns: (gids []i64, n i64)
	// Note: Returns count via the second return value; the slice is currently empty
	// because dynamic element-wise i32→i64 conversion requires loop infrastructure
	// that is complex to generate inside callBuiltin. The Nolang wrapper can use
	// the count for validation; full group ID retrieval will be added when loop
	// generation inside builtins is available.
	if fnName == "getgroups" {
		ggN := g.tmpReg("gg.n")
		ggVec := g.tmpReg("gg.vec")
		ggLenGEP := g.tmpReg("gg.lengep")
		ggCapGEP := g.tmpReg("gg.capgep")
		ggDataGEP := g.tmpReg("gg.datagep")
		ggNExt := g.tmpReg("gg.next")
		if sb != nil {
			// getgroups(0, NULL) → count of groups (or -1 on error)
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @getgroups(i32 0, i32* null)\n", g.indent(), ggN))
			// Construct empty %vec { len=0, cap=0, data=null }
			// The group IDs themselves are not retrieved here; callers needing the
			// actual IDs should use a different mechanism (e.g., parsing /proc/$$/gid
			// on Linux or calling getgroups via FFI on macOS).
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), ggVec))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 0\n", g.indent(), ggLenGEP, ggVec))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), ggLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 1\n", g.indent(), ggCapGEP, ggVec))
			sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), ggCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), ggDataGEP, ggVec))
			g.storeDataPtrField(sb, "null", ggDataGEP)
			// n = sext ggN to i64 (returns -1 on error, count on success)
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), ggNExt, ggN))
		}
		g.lastBuiltinExtra = ggNExt
		return ggVec
	}

	// num-cpu: get number of online CPUs (wraps sysconf(_SC_NPROCESSORS_ONLN))
	// _SC_NPROCESSORS_ONLN is platform-specific:
	//   darwin (macOS): 58
	//   linux:          84
	//   windows/wasi:   sysconf unavailable — return 1 as a conservative fallback
	// Returns: i64
	if fnName == "num-cpu" {
		goos := g.goos()
		if goos == "windows" || goos == "wasi" {
			// No sysconf on these targets; return 1 (single CPU fallback).
			// Avoids referencing @sysconf which is not declared on windows/wasi.
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s; num-cpu: sysconf unavailable on %s, returning 1\n", g.indent(), goos))
			}
			return "1"
		}
		scNprocOnln := int64(58) // macOS _SC_NPROCESSORS_ONLN
		if goos == "linux" {
			scNprocOnln = 84 // Linux _SC_NPROCESSORS_ONLN
		}
		ncRet := g.tmpReg("nc.ret")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @sysconf(i32 %d)\n", g.indent(), ncRet, scNprocOnln))
		}
		return ncRet
	}

	// uname: get kernel/architecture info (POSIX uname(2))
	// Returns: %utsname* (struct with 5 %str-long fields)
	if fnName == "uname" {
		// Resolve the utsname struct name — it may be registered as "utsname"
		// or "os.utsname" (after prefixModuleStatements adds the module prefix).
		utsnameKey := "utsname"
		if _, ok := g.structTypes[utsnameKey]; !ok {
			for name := range g.structTypes {
				if strings.HasSuffix(name, ".utsname") {
					utsnameKey = name
					break
				}
			}
		}
		utsnameLLVMType := "%" + utsnameKey
		// struct utsname field offsets:
		// macOS: _UTSNAME_LENGTH = 256, fields at 0, 256, 512, 768, 1024
		// Linux: _UTSNAME_LENGTH = 65, fields at 0, 65, 130, 195, 260
		fieldLen := int64(256)
		if g.goos() == "linux" {
			fieldLen = 65
		}
		totalSize := fieldLen * 5
		unBuf := g.tmpReg("un.buf")
		unRet := g.tmpReg("un.ret")
		// Allocate the utsname result struct (%utsname = { %str-long, %str-long, %str-long, %str-long, %str-long })
		unResult := g.tmpReg("un.result")
		// Field field offsets in the C struct utsname buffer
		offsets := [5]int64{0, fieldLen, fieldLen * 2, fieldLen * 3, fieldLen * 4}
		var fieldRegs [5]string
		if sb != nil {
			// alloca [totalSize x i8] for C struct utsname
			sb.WriteString(fmt.Sprintf("%s%s = alloca [%d x i8]\n", g.indent(), unBuf, totalSize))
			// call uname(buf)
			unBufPtr := g.tmpReg("un.bufptr")
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr [%d x i8], [%d x i8]* %s, i64 0, i64 0\n",
				g.indent(), unBufPtr, totalSize, totalSize, unBuf))
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @uname(i8* %s)\n", g.indent(), unRet, unBufPtr))
			// alloca %utsname for the Nolang result
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), unResult, utsnameLLVMType))
		}
		// For each of the 5 fields (sysname, nodename, release, version, machine):
		fieldNames := [5]string{"sysname", "nodename", "release", "version", "machine"}
		for i := 0; i < 5; i++ {
			g.tmpIdx++
			fldGEP := fmt.Sprintf("%%un.%s.gep.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldLen := fmt.Sprintf("%%un.%s.len.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldBufSize := fmt.Sprintf("%%un.%s.bufsize.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldBuf := fmt.Sprintf("%%un.%s.buf.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldStr := fmt.Sprintf("%%un.%s.str.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldLenGEP := fmt.Sprintf("%%un.%s.lengep.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldCapGEP := fmt.Sprintf("%%un.%s.capgep.%d", fieldNames[i], g.tmpIdx)
			g.tmpIdx++
			fldDataGEP := fmt.Sprintf("%%un.%s.datagep.%d", fieldNames[i], g.tmpIdx)
			fieldRegs[i] = fldStr
			if sb != nil {
				// GEP to the field in the C buffer
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr [%d x i8], [%d x i8]* %s, i64 0, i64 %d\n",
					g.indent(), fldGEP, totalSize, totalSize, unBuf, offsets[i]))
				// strlen of the field
				sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), fldLen, fldGEP))
				sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), fldBufSize, fldLen))
				sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), fldBuf, fldBufSize))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
					g.indent(), fldBuf, fldGEP, fldBufSize))
				// Construct %str-long for this field
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), fldStr))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), fldLenGEP, fldStr))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), fldLen, fldLenGEP))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), fldCapGEP, fldStr))
				sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), fldLen, fldCapGEP))
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), fldDataGEP, fldStr))
				g.storeDataPtrField(sb, fldBuf, fldDataGEP)
			}
		}
		// Now we need to store each %str-long field into the utsname struct.
		// The struct type is resolved dynamically (may be "utsname" or "os.utsname").
		// Since it is an alloca, we can use getelementptr to get each field
		// and store the %str-long values directly.
		if sb != nil {
			for i := 0; i < 5; i++ {
				g.tmpIdx++
				dstGEP := fmt.Sprintf("%%un.dst.%s.%d", fieldNames[i], g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), dstGEP, utsnameLLVMType, utsnameLLVMType, unResult, i))
				// Load the %str-long from the field alloca
				g.tmpIdx++
				fldLoad := fmt.Sprintf("%%un.%s.load.%d", fieldNames[i], g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), fldLoad, fieldRegs[i]))
				sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), fldLoad, dstGEP))
			}
		}
		// Load the %utsname value from the alloca so the Let statement can
		// store it directly into the LHS variable (which must be allocated
		// as %utsname, not i64). Returning the pointer causes a type mismatch
		// because the Let statement expects a value, not a pointer.
		unLoad := g.tmpReg("un.load")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), unLoad, utsnameLLVMType, utsnameLLVMType, unResult))
		}
		return unLoad
	}

	// ttyname: get terminal name (POSIX ttyname(3))
	// Returns: %str-long* (empty string on failure)
	if fnName == "ttyname" && hasArgs {
		a := evalArgs()
		// fd is i64; need to trunc to i32 for the C call
		tnFd := g.tmpReg("tn.fd")
		tnRet := g.tmpReg("tn.ret")
		tnCmp := g.tmpReg("tn.cmp")
		tnSafe := g.tmpReg("tn.safe")
		tnLen := g.tmpReg("tn.len")
		tnBufSize := g.tmpReg("tn.bufsize")
		tnBuf := g.tmpReg("tn.buf")
		tnStr := g.tmpReg("tn.str")
		tnLenGEP := g.tmpReg("tn.lengep")
		tnCapGEP := g.tmpReg("tn.capgep")
		tnDataGEP := g.tmpReg("tn.datagep")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to i32\n", g.indent(), tnFd, a[0]))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @ttyname(i32 %s)\n", g.indent(), tnRet, tnFd))
			sb.WriteString(fmt.Sprintf("%s%s = icmp ne i8* %s, null\n", g.indent(), tnCmp, tnRet))
			sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i8* %s, i8* getelementptr inbounds ([1 x i8], [1 x i8]* @.str.empty, i64 0, i64 0)\n",
				g.indent(), tnSafe, tnCmp, tnRet))
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), tnLen, tnSafe))
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), tnBufSize, tnLen))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), tnBuf, tnBufSize))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
				g.indent(), tnBuf, tnSafe, tnBufSize))
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tnStr))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), tnLenGEP, tnStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), tnLen, tnLenGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), tnCapGEP, tnStr))
			sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), tnLen, tnCapGEP))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), tnDataGEP, tnStr))
			g.storeDataPtrField(sb, tnBuf, tnDataGEP)
		}
		// Load %str-long value from pointer so assignment codegen can store it directly.
		tnLoad := g.tmpReg("tn.load")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = load %%str-long, %%str-long* %s\n", g.indent(), tnLoad, tnStr))
		}
		return tnLoad
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
		forkRet := g.tmpReg("proc.fork.ret")
		forkExt := g.tmpReg("proc.fork.ext")
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call i32 @fork()\n", g.indent(), forkRet))
			sb.WriteString(fmt.Sprintf("%s%s = sext i32 %s to i64\n", g.indent(), forkExt, forkRet))
		}
		return forkExt
	}

	// process-pipe: create a pipe
	// Returns packed i64: (read_fd << 32) | write_fd
	if fnName == "process-pipe" {
		pipeFds := g.tmpReg("proc.pipe.fds")
		pipeRet := g.tmpReg("proc.pipe.ret")
		pipeGep0 := g.tmpReg("proc.pipe.gep0")
		pipeFd0 := g.tmpReg("proc.pipe.fd0")
		pipeGep1 := g.tmpReg("proc.pipe.gep1")
		pipeFd1 := g.tmpReg("proc.pipe.fd1")
		pipeExt0 := g.tmpReg("proc.pipe.ext0")
		pipeExt1 := g.tmpReg("proc.pipe.ext1")
		pipeShl := g.tmpReg("proc.pipe.shl")
		pipePack := g.tmpReg("proc.pipe.pack")
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
		waitStatus := g.tmpReg("proc.wait.status")
		waitPidTrunc := g.tmpReg("proc.wait.pid")
		waitOptTrunc := g.tmpReg("proc.wait.opt")
		waitRet := g.tmpReg("proc.wait.ret")
		waitLd := g.tmpReg("proc.wait.ld")
		waitShift := g.tmpReg("proc.wait.shift")
		waitCode := g.tmpReg("proc.wait.code")
		waitExt := g.tmpReg("proc.wait.ext")
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
		execRet := g.tmpReg("proc.exec.ret")
		execExt := g.tmpReg("proc.exec.ext")
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
		addrReg := g.tmpReg("net.l.addr")
		addrPtr := g.tmpReg("net.l.addrptr")

		// sin_family at offset 1 (macOS: sin_len=0, sin_family=1)
		famGep := g.tmpReg("net.l.fam")

		// htons(port): (port & 0xFF) << 8 | (port >> 8) & 0xFF
		portLo := g.tmpReg("net.l.plo")
		portHi := g.tmpReg("net.l.phi")
		portHiM := g.tmpReg("net.l.phm")
		portSl := g.tmpReg("net.l.psl")
		portNet := g.tmpReg("net.l.pnet")
		portI16 := g.tmpReg("net.l.pi16")

		// sin_port at offset 2
		portGep := g.tmpReg("net.l.portgep")
		portCast := g.tmpReg("net.l.portcast")

		// inet_pton(AF_INET=2, host, &sin_addr at offset 4)
		addrInGep := g.tmpReg("net.l.addringe")
		ptonRet := g.tmpReg("net.l.pton")
		ptonOk := g.tmpReg("net.l.ptonok")

		// socket(AF_INET=2, SOCK_STREAM=1, 0)
		sockFd := g.tmpReg("net.l.sock")
		sockOk := g.tmpReg("net.l.sockok")

		// setsockopt(fd, SOL_SOCKET=65535, SO_REUSEADDR=4, &val, 4)
		reuseAlloca := g.tmpReg("net.l.reuse")
		reusePtr := g.tmpReg("net.l.reuseptr")

		// bind(fd, &addr, 16)
		bindRet := g.tmpReg("net.l.bind")
		bindOk := g.tmpReg("net.l.bindok")

		// listen(fd, 128)
		listenRet := g.tmpReg("net.l.listen")
		listenOk := g.tmpReg("net.l.listenok")

		// fd as i64
		fdExt := g.tmpReg("net.l.fdext")

		// all.ok = pton.ok AND sock.ok AND bind.ok AND listen.ok
		ok1 := g.tmpReg("net.l.ok1")
		ok2 := g.tmpReg("net.l.ok2")
		ok3 := g.tmpReg("net.l.ok3")

		// result = select all.ok ? fd : -1
		resultReg := g.tmpReg("net.l.result")

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
		addrReg := g.tmpReg("net.d.addr")
		addrPtr := g.tmpReg("net.d.addrptr")

		// sin_family at offset 1
		famGep := g.tmpReg("net.d.fam")

		// htons(port)
		portLo := g.tmpReg("net.d.plo")
		portHi := g.tmpReg("net.d.phi")
		portHiM := g.tmpReg("net.d.phm")
		portSl := g.tmpReg("net.d.psl")
		portNet := g.tmpReg("net.d.pnet")
		portI16 := g.tmpReg("net.d.pi16")

		// sin_port at offset 2
		portGep := g.tmpReg("net.d.portgep")
		portCast := g.tmpReg("net.d.portcast")

		// inet_pton
		addrInGep := g.tmpReg("net.d.addringe")
		ptonRet := g.tmpReg("net.d.pton")
		ptonOk := g.tmpReg("net.d.ptonok")

		// getaddrinfo fallback registers
		hintsReg := g.tmpReg("net.d.hints")
		hintsPtr := g.tmpReg("net.d.hintsptr")
		hintsFamGep := g.tmpReg("net.d.hintsfam")
		hintsFamCast := g.tmpReg("net.d.hintsfamc")
		hintsStGep := g.tmpReg("net.d.hintsst")
		hintsStCast := g.tmpReg("net.d.hintsstc")
		resReg := g.tmpReg("net.d.res")
		gaiRet := g.tmpReg("net.d.gai")
		gaiOk := g.tmpReg("net.d.gaiok")
		resVal := g.tmpReg("net.d.resval")
		aiAddrGep := g.tmpReg("net.d.aiaddrgep")
		aiAddrCast := g.tmpReg("net.d.aiaddrcast")
		aiAddr := g.tmpReg("net.d.aiaddr")

		// socket
		sockFd := g.tmpReg("net.d.sock")
		sockOk := g.tmpReg("net.d.sockok")

		// connect
		connRet := g.tmpReg("net.d.conn")
		connOk := g.tmpReg("net.d.connok")

		// fd as i64
		fdExt := g.tmpReg("net.d.fdext")

		// sock.ok AND conn.ok
		sockConnOk := g.tmpReg("net.d.sockconnok")

		// phi nodes at merge
		finalOk := g.tmpReg("net.d.finalok")
		finalFd := g.tmpReg("net.d.finalfd")

		// result
		resultReg := g.tmpReg("net.d.result")

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
			addrSinAddrGep := g.tmpReg("net.d.sinaddr")
			aiAddrSinAddrGep := g.tmpReg("net.d.aiaddrsin")
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

		addrBuf := g.tmpReg("net.a.addr")
		addrPtr := g.tmpReg("net.a.addrptr")
		lenAlloca := g.tmpReg("net.a.len")
		fdTrunc := g.tmpReg("net.a.fdtrunc")
		acceptRet := g.tmpReg("net.a.ret")
		acceptExt := g.tmpReg("net.a.ext")

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
			dataGEP := g.tmpReg("net.s.datagep")
			dataLoad := g.tmpReg("net.s.dataptr")
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
				tmpAlloca := g.tmpReg("net.s.arrtmp")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			dataGEP := g.tmpReg("net.s.arrgep")
			dataLoad := g.tmpReg("net.s.arrptr")
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

		fdTrunc := g.tmpReg("net.s.fdtrunc")
		sendRet := g.tmpReg("net.s.ret")

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
			bufGEP := g.tmpReg("net.r.datagep")
			bufLoad := g.tmpReg("net.r.dataptr")
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
				tmpAlloca := g.tmpReg("net.r.arrtmp")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			bufGEP := g.tmpReg("net.r.arrgep")
			bufLoad := g.tmpReg("net.r.arrptr")
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

		fdTrunc := g.tmpReg("net.r.fdtrunc")
		recvRet := g.tmpReg("net.r.ret")

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
		sockFd := g.tmpReg("net.udp.sock")
		fdExt := g.tmpReg("net.udp.fdext")
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
			dataGEP := g.tmpReg("net.us.datagep")
			dataLoad := g.tmpReg("net.us.dataptr")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), dataGEP, vecPtr))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}
			dataPtr = dataLoad
		} else if dataArgType == "%arr" {
			arrEval := a[1]
			arrPtr := arrEval
			if idx := strings.Index(arrEval, ".val."); idx > 0 {
				tmpAlloca := g.tmpReg("net.us.arrtmp")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			dataGEP := g.tmpReg("net.us.arrgep")
			dataLoad := g.tmpReg("net.us.arrptr")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %s, i32 0, i32 1\n", g.indent(), dataGEP, arrPtr))
				dataLoad = g.loadDataPtrField(sb, dataGEP)
			}
			dataPtr = dataLoad
		} else {
			dataPtr = g.extractStrFromEvalArg(sb, a[1])
		}

		// Build sockaddr_in (16 bytes)
		addrReg := g.tmpReg("net.us.addr")
		addrPtr := g.tmpReg("net.us.addrptr")
		famGep := g.tmpReg("net.us.fam")
		portLo := g.tmpReg("net.us.plo")
		portHi := g.tmpReg("net.us.phi")
		portHiM := g.tmpReg("net.us.phm")
		portSl := g.tmpReg("net.us.psl")
		portNet := g.tmpReg("net.us.pnet")
		portI16 := g.tmpReg("net.us.pi16")
		portGep := g.tmpReg("net.us.portgep")
		portCast := g.tmpReg("net.us.portcast")
		addrInGep := g.tmpReg("net.us.addringe")
		ptonRet := g.tmpReg("net.us.pton")
		ptonOk := g.tmpReg("net.us.ptonok")
		// getaddrinfo fallback registers
		hintsReg := g.tmpReg("net.us.hints")
		hintsPtr := g.tmpReg("net.us.hintsptr")
		hintsFamGep := g.tmpReg("net.us.hintsfam")
		hintsFamCast := g.tmpReg("net.us.hintsfamc")
		hintsStGep := g.tmpReg("net.us.hintsst")
		hintsStCast := g.tmpReg("net.us.hintsstc")
		resReg := g.tmpReg("net.us.res")
		gaiRet := g.tmpReg("net.us.gai")
		gaiOk := g.tmpReg("net.us.gaiok")
		resVal := g.tmpReg("net.us.resval")
		aiAddrGep := g.tmpReg("net.us.aiaddrgep")
		aiAddrCast := g.tmpReg("net.us.aiaddrcast")
		fdTrunc := g.tmpReg("net.us.fdtrunc")
		sendRet := g.tmpReg("net.us.ret")

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
			bufGEP := g.tmpReg("net.ur.datagep")
			bufLoad := g.tmpReg("net.ur.dataptr")
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n", g.indent(), bufGEP, vecPtr))
				bufLoad = g.loadDataPtrField(sb, bufGEP)
			}
			bufPtr = bufLoad
		} else if bufArgType == "%arr" {
			arrEval := a[1]
			arrPtr := arrEval
			if idx := strings.Index(arrEval, ".val."); idx > 0 {
				tmpAlloca := g.tmpReg("net.ur.arrtmp")
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%arr\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%arr %s, %%arr* %s\n", g.indent(), arrEval, tmpAlloca))
				}
				arrPtr = tmpAlloca
			}
			bufGEP := g.tmpReg("net.ur.arrgep")
			bufLoad := g.tmpReg("net.ur.arrptr")
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
		addrReg := g.tmpReg("net.ur.addr")
		addrPtr := g.tmpReg("net.ur.addrptr")
		lenAlloca := g.tmpReg("net.ur.addrlen")
		fdTrunc := g.tmpReg("net.ur.fdtrunc")
		recvRet := g.tmpReg("net.ur.ret")

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

		tvAlloca := g.tmpReg("net.to.tv")
		tvPtr := g.tmpReg("net.to.tvptr")
		secVal := g.tmpReg("net.to.sec")
		remainMs := g.tmpReg("net.to.remain")
		usecVal := g.tmpReg("net.to.usec")
		tvSecGep := g.tmpReg("net.to.tvsec")
		tvSecCast := g.tmpReg("net.to.tvsecc")
		tvUsecGep := g.tmpReg("net.to.tvusec")
		tvUsecCast := g.tmpReg("net.to.tvusecc")
		fdTrunc := g.tmpReg("net.to.fdtrunc")
		setRet := g.tmpReg("net.to.ret")
		retExt := g.tmpReg("net.to.ext")

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

		addrReg := g.tmpReg("unix.l.addr")
		addrPtr := g.tmpReg("unix.l.addrptr")

		// sun_family at offset 1 (macOS: sun_len=0, sun_family=1)
		famGep := g.tmpReg("unix.l.fam")

		// sun_path at offset 2
		pathGep := g.tmpReg("unix.l.path")

		// cap path length at 103
		capCmp := g.tmpReg("unix.l.capcmp")
		cappedLen := g.tmpReg("unix.l.caplen")

		// socket(AF_UNIX=1, SOCK_STREAM=1, 0)
		sockFd := g.tmpReg("unix.l.sock")
		sockOk := g.tmpReg("unix.l.sockok")

		// bind(fd, &addr, 106)
		bindRet := g.tmpReg("unix.l.bind")
		bindOk := g.tmpReg("unix.l.bindok")

		// listen(fd, 128)
		listenRet := g.tmpReg("unix.l.listen")
		listenOk := g.tmpReg("unix.l.listenok")

		fdExt := g.tmpReg("unix.l.fdext")
		ok1 := g.tmpReg("unix.l.ok1")
		ok2 := g.tmpReg("unix.l.ok2")
		resultReg := g.tmpReg("unix.l.result")

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

		addrReg := g.tmpReg("unix.d.addr")
		addrPtr := g.tmpReg("unix.d.addrptr")

		// sun_family at offset 1 (macOS: sun_len=0, sun_family=1)
		famGep := g.tmpReg("unix.d.fam")

		// sun_path at offset 2
		pathGep := g.tmpReg("unix.d.path")

		// cap path length at 103
		capCmp := g.tmpReg("unix.d.capcmp")
		cappedLen := g.tmpReg("unix.d.caplen")

		// socket(AF_UNIX=1, SOCK_STREAM=1, 0)
		sockFd := g.tmpReg("unix.d.sock")
		sockOk := g.tmpReg("unix.d.sockok")

		// connect(fd, &addr, 106)
		connRet := g.tmpReg("unix.d.conn")
		connOk := g.tmpReg("unix.d.connok")

		fdExt := g.tmpReg("unix.d.fdext")
		ok1 := g.tmpReg("unix.d.ok1")
		resultReg := g.tmpReg("unix.d.result")

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
		sockFd := g.tmpReg("net.icmp.sock")
		fdExt := g.tmpReg("net.icmp.fdext")
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
			tmpAlloca := g.tmpReg("str-long.dot")
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
				tmpAlloca := g.tmpReg("str-long.idx")
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
				sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), ptr, tmpAlloca))
				dataPtr = g.extractStrDataPtr(sb, tmpAlloca)
			}
		}
	}

	if dataPtr == "" {
		return dataPtr
	}

	sizeReg := g.tmpReg("str-longnull.size")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), sizeReg, strLen))

	buf := g.tmpReg("str-longnull.buf")
	sb.WriteString(fmt.Sprintf("%s%s = alloca i8, i64 %s\n", g.indent(), buf, sizeReg))

	nullEnd := g.tmpReg("str-longnull.end")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), nullEnd, buf, strLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullEnd))

	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), buf, dataPtr, strLen))

	return buf
}

// loadStderr loads the FILE* for stderr and returns the register name.
// macOS: @__stderrp, Linux/Windows/WASI: @stderr
// 使用編譯目標平台（g.goos()）而非宿主平台，與 decl.go 宣告分派一致。
func (g *Generator) loadStderr(sb *strings.Builder) string {
	reg := g.tmpReg("stderr.ptr")
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
		discardedN := g.tmpReg("vso.tmp")
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
	outBuf := g.tmpReg("vso.tmp")
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
	tmpAlloca := g.tmpReg("fmtarg")
	loadReg := g.tmpReg("fmtarg.val")
	convReg := g.tmpReg("fmtarg.conv")

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

	totalLen := g.tmpReg("nfmt.concat.total")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, %s\n", g.indent(), totalLen, leftLen, rightLen))

	allocSize := g.tmpReg("nfmt.concat.alloc")
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), allocSize, totalLen))

	bufPtr := g.tmpReg("nfmt.concat.buf")
	sb.WriteString(fmt.Sprintf("%s%s = call i8* @nolang.malloc(i64 %s)\n", g.indent(), bufPtr, allocSize))

	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), bufPtr, leftData, leftLen))

	dstOffset := g.tmpReg("nfmt.concat.dst")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), dstOffset, bufPtr, leftLen))
	sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		g.indent(), dstOffset, rightData, rightLen))

	nullPos := g.tmpReg("nfmt.concat.null")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr i8, i8* %s, i64 %s\n", g.indent(), nullPos, bufPtr, totalLen))
	sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPos))

	resultAlloca := g.tmpReg("nfmt.concat.result")
	sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), resultAlloca))

	lenGEP := g.tmpReg("nfmt.concat.len.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", g.indent(), lenGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, lenGEP))

	capGEP := g.tmpReg("nfmt.concat.cap.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", g.indent(), capGEP, resultAlloca))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), totalLen, capGEP))

	dataGEP := g.tmpReg("nfmt.concat.data.gep")
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", g.indent(), dataGEP, resultAlloca))
	g.storeDataPtrField(sb, bufPtr, dataGEP)

	// 注册为语句级临时堆对象：若未绑定变量，由 generateStatement 在语句结束前 free data。
	g.stmtTemporaries = append(g.stmtTemporaries, resultAlloca)
	return resultAlloca
}
