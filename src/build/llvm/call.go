package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

func (g *Generator) generateCallExpression(sb *strings.Builder, expr *parser.CallExpression) string {
	fnName := ""
	if ident, ok := expr.Function.(*parser.Identifier); ok {
		fnName = ident.Value
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

	// 嘗試各 domain handler
	if r := g.callFmt(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg, expr); r != "" {
		return r
	}
	if r := g.callStrconv(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg); r != "" {
		return r
	}
	if r := g.callOs(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg); r != "" {
		return r
	}
	if r := g.callFileIO(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg); r != "" {
		return r
	}
	if r := g.callMath(sb, fnName, hasArgs, len(expr.Arguments), evalArgs, strArg, llvmArg); r != "" {
		return r
	}
	// sort-asc / sort-desc 直接在 call.go 處理（無需 call_stdlib 函數）
	if (fnName == "sort-asc" || fnName == "sort-desc") && hasArgs && len(expr.Arguments) >= 2 {
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s; %s not yet implemented for LLVM target\n", g.indent(), fnName))
		}
		return ""
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
					typedArgs[i] = "%str* %" + a.Value
					break
				}
			}
			// 陣列型別用正確的指標型別
			if g.varTypes != nil {
				if t, ok := g.varTypes[a.Value]; ok && strings.HasPrefix(t, "[") {
					llvmType := g.mapToLLVMType(t)
					typedArgs[i] = llvmType + "* %" + a.Value
					break
				}
			}
			typedArgs[i] = "i64* %" + a.Value
		case *parser.StringLiteral:
			// String literal: generate %str struct and pass as %str*
			ev := g.generateExprWithSB(sb, arg)
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
			} else if strings.HasPrefix(ev, "%") {
				parts := strings.Split(ev, ".")
				varName := strings.TrimPrefix(parts[0], "%")
				typedArgs[i] = "i64* %" + varName
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
