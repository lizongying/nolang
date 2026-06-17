package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

type varInfo struct {
	Name string
	Type string
	Size int64
}

type structField struct {
	name string
	typ  string // LLVM type string
}

type loopExit struct {
	name string // 循環名稱（空 = 未命名）
	cond string // LLVM 條件塊標籤（continue 跳轉目標）
	exit string // LLVM 退出塊標籤（break 跳轉目標）
}

type Generator struct {
	indentLevel    int
	fmtStrIdx      int
	stringIdx      int
	fmtGlobals     []string
	tmpIdx         int
	funcVars       []varInfo                // current function's variables for lifetime.end
	varTypes       map[string]string        // variable name → LLVM type
	varSSA         map[string]int           // variable name → current SSA version
	ssaMode        bool                     // true = 使用 SSA 暫存器
	paramNames     map[string]bool          // 函數參數名稱（使用 .addr 存取）
	funcRetTypes   map[string]string        // 函數名 → 回傳型別
	structTypes    map[string][]structField // struct name → fields
	structTypeLLVM string                   // 當前正在生成的 struct LLVM type name
	loopExits      []loopExit               // 活躍循環退出目標棧
	nestedIfEndId  int                      // labelId of the most recently generated if expression's end block
	arrayElemTypes map[string]string        // variable name → element LLVM type for %arr variables
	curFuncRetType string                   // 當前函數回傳型別（void/i64/...）
	curFuncRetName string                   // 當前函數輸出參數名稱（為空表示 void）
}

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) indent() string {
	return strings.Repeat("\t", g.indentLevel)
}

func (g *Generator) getFormatGlobal(fmtStr string) string {
	name := fmt.Sprintf("@.pfmt.%d", g.fmtStrIdx)
	g.fmtStrIdx++
	size := len(fmtStr) + 1
	escaped := g.escapeLLVMString(fmtStr)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"", name, size, escaped))
	return name
}

func (g *Generator) escapeLLVMString(s string) string {
	r := strings.NewReplacer(
		"\\", "\\5C",
		"\n", "\\0A",
		"\r", "\\0D",
		"\t", "\\09",
		"\"", "\\22",
	)
	return r.Replace(s)
}

func (g *Generator) Generate(program *parser.Program) string {
	g.fmtGlobals = nil
	g.fmtStrIdx = 0
	g.stringIdx = 0
	g.tmpIdx = 0
	g.varTypes = make(map[string]string)
	g.paramNames = make(map[string]bool)
	g.funcRetTypes = make(map[string]string)
	g.structTypes = make(map[string][]structField)
	g.arrayElemTypes = make(map[string]string)

	var sb strings.Builder

	sb.WriteString("; ModuleID = 'nolang'\n")
	sb.WriteString("source_filename = \"nolang\"\n")
	sb.WriteString("target datalayout = \"e-m:o-i64:64-i128:128-n32:64-S128\"\n")
	sb.WriteString("target triple = \"arm64-apple-macosx15.0.0\"\n\n")

	g.writeDeclarations(&sb)

	// 預掃描：收集所有函數的回傳型別
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			retType := "void"
			if len(fd.Results) > 0 {
				retType = g.mapToLLVMType(fd.Results[0].Type.String())
			}
			g.funcRetTypes[fd.Name] = retType
		}
	}

	// Pre-register built-in arr type (used for all fixed-size arrays)
	g.structTypes["arr"] = []structField{
		{name: "len", typ: "i64"},
		{name: "data", typ: "i8*"},
	}

	// Pre-register built-in vec type (used for all slices)
	g.structTypes["vec"] = []structField{
		{name: "len", typ: "i64"},
		{name: "cap", typ: "i64"},
		{name: "data", typ: "i8*"},
	}

	// 收集結構體定義並生成 LLVM struct type
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			g.collectStructType(sd)
		}
	}

	// 發出 struct type 宣告
	// Always emit built-in string types
	sb.WriteString("%str-smail = type { i8, [127 x i8] }\n")
	sb.WriteString("%str = type { i64, i8* }\n")
	sb.WriteString("%option = type { i64, [16 x i8] }\n")
	sb.WriteString("%arr = type { i64, i8* }\n")
	sb.WriteString("%vec = type { i64, i64, i8* }\n")
	for name, fields := range g.structTypes {
		if name == "str" || name == "str-smail" || name == "arr" || name == "vec" {
			continue // built-in, already emitted
		}
		sb.WriteString(fmt.Sprintf("%%%s = type { ", name))
		for i, f := range fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(f.typ)
		}
		sb.WriteString(" }\n")
	}
	sb.WriteString("\n")

	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			g.generateFunctionDefinition(&sb, fd)
		}
	}

	g.generateMainFunction(&sb, program)

	if len(g.fmtGlobals) > 0 {
		sb.WriteString("\n; Format string constants\n")
		for _, fg := range g.fmtGlobals {
			sb.WriteString(fg + "\n")
		}
	}

	return sb.String()
}

func llvmLLVMType(t builtin.LLVMArgType) string {
	switch t {
	case builtin.LLVMI64:
		return "i64"
	case builtin.LLVMF64:
		return "double"
	case builtin.LLVMI8Ptr:
		return "i8*"
	case builtin.LLVMI32:
		return "i32"
	case builtin.LLVMStrPtr:
		return "i8*"
	default:
		return "i64"
	}
}

func (g *Generator) genCLibCall(sb *strings.Builder, m *builtin.BuiltinMethod, evalArgs func() []string) string {
	a := evalArgs()
	clib := m.CLibCall

	// Sprintf pattern: sprintf(buf, fmt, args...)
	if clib.SprintfFmt != "" {
		fg := g.getFormatGlobal(clib.SprintfFmt)
		buf := fmt.Sprintf("i8* getelementptr inbounds ([64 x i8], [64 x i8]* %s, i64 0, i64 0)", clib.BufGlobal)
		argStr := buf + ", i8* getelementptr inbounds ([" + fmt.Sprintf("%d", len(clib.SprintfFmt)+1) + " x i8], [" + fmt.Sprintf("%d", len(clib.SprintfFmt)+1) + " x i8]* " + fg + ", i64 0, i64 0)"
		for i, v := range a {
			argStr += ", " + llvmLLVMType(clib.ArgTypes[i]) + " " + v
		}
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall i32 (i8*, i8*, ...) @sprintf(%s)\n", g.indent(), argStr))
		}
		return buf
	}

	// Build argument string
	evIdx := 0
	argStr := ""
	for i := 0; i < len(clib.ArgTypes); i++ {
		if i > 0 {
			argStr += ", "
		}
		argType := clib.ArgTypes[i]

		if fixedVal, ok := clib.FixedArgs[i]; ok {
			argStr += llvmLLVMType(argType) + " " + fixedVal
			continue
		}

		if fixedGlobal, ok := clib.FixedArgGlobals[i]; ok {
			argStr += "i8* " + fixedGlobal
			continue
		}

		if truncTo, ok := clib.TruncArgs[i]; ok {
			g.tmpIdx++
			truncReg := fmt.Sprintf("%%clib.trunc.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, a[evIdx], llvmLLVMType(truncTo)))
			}
			argStr += llvmLLVMType(truncTo) + " " + truncReg
			evIdx++
			continue
		}

		if clib.StrDataArg != nil && clib.StrDataArg[i] {
			dataPtr := g.extractStrFromEvalArg(sb, a[evIdx])
			argStr += "i8* " + dataPtr
			evIdx++
			continue
		}

		if argType == builtin.LLVMStrPtr {
			dataPtr := g.extractStrFromEvalArg(sb, a[evIdx])
			argStr += "i8* " + dataPtr
		} else {
			argStr += llvmLLVMType(argType) + " " + a[evIdx]
		}
		evIdx++
	}

	// RetBuf: return the buffer pointer instead of C return value
	if clib.RetBuf {
		buf := fmt.Sprintf("i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* %s, i64 0, i64 0)", clib.BufGlobal)
		cRetType := llvmLLVMType(clib.RetType)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall %s @%s(%s)\n", g.indent(), cRetType, clib.FuncName, argStr))
		}
		return buf
	}

	cRetType := llvmLLVMType(clib.RetType)
	if clib.RetType == builtin.LLVMStrPtr {
		cRetType = "i8*"
	}

	if clib.RetExt != nil {
		g.tmpIdx++
		callReg := fmt.Sprintf("%%clib.ret.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call %s @%s(%s)\n", g.indent(), callReg, cRetType, clib.FuncName, argStr))
		}
		g.tmpIdx++
		extReg := fmt.Sprintf("%%clib.ext.%d", g.tmpIdx)
		extInstr := "zext"
		if clib.RetType == builtin.LLVMI32 {
			extInstr = "sext"
		}
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to i64\n", g.indent(), extReg, extInstr, cRetType, callReg))
		}
		return extReg
	}

	if clib.CmpRet {
		g.tmpIdx++
		callReg := fmt.Sprintf("%%clib.ret.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call %s @%s(%s)\n", g.indent(), callReg, cRetType, clib.FuncName, argStr))
		}
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%clib.cmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq %s %s, 0\n", g.indent(), cmpReg, cRetType, callReg))
		}
		g.tmpIdx++
		extReg := fmt.Sprintf("%%clib.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	}

	return fmt.Sprintf("call %s @%s(%s)", cRetType, clib.FuncName, argStr)
}

func (g *Generator) extractStrFromEvalArg(sb *strings.Builder, evalResult string) string {
	if strings.HasPrefix(evalResult, "%") {
		parts := strings.Split(evalResult, ".")
		varName := strings.TrimPrefix(parts[0], "%")
		if g.varTypes != nil {
			if t, ok := g.varTypes[varName]; ok {
				if t == "%str-smail" {
					return g.extractStrSmailDataPtr(sb, evalResult)
				}
				return g.extractStrDataPtr(sb, evalResult)
			}
		}
		return g.extractStrDataPtr(sb, evalResult)
	}
	return evalResult
}

func (g *Generator) genLLVMConv(sb *strings.Builder, m *builtin.BuiltinMethod, evalArgs func() []string) string {
	a := evalArgs()
	conv := *m.LLVMConv
	switch conv {
	case builtin.LLVMConvI64ToFP:
		g.tmpIdx++
		reg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sitofp i64 %s to double\n", g.indent(), reg, a[0]))
		}
		return reg
	case builtin.LLVMConvFPToI64:
		g.tmpIdx++
		reg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = fptosi double %s to i64\n", g.indent(), reg, a[0]))
		}
		return reg
	}
	return ""
}

func (g *Generator) findLoopTarget(label string, isBreak bool) string {
	if label != "" {
		for i := len(g.loopExits) - 1; i >= 0; i-- {
			if g.loopExits[i].name == label {
				if isBreak {
					return g.loopExits[i].exit
				}
				return g.loopExits[i].cond
			}
		}
	}
	// 未命名或标签未找到：使用最近循环
	if len(g.loopExits) > 0 {
		last := g.loopExits[len(g.loopExits)-1]
		if isBreak {
			return last.exit
		}
		return last.cond
	}
	return ""
}
