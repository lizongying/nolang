package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

func (g *Generator) llvmTypeSize(llvmType string) int64 {
	if strings.HasPrefix(llvmType, "[") {
		// [N x i64] → N * 8
		var n int64
		var elem string
		if _, err := fmt.Sscanf(llvmType, "[%d x %s]", &n, &elem); err == nil {
			return n * g.llvmTypeSize(elem)
		}
		return 64 // fallback
	}
	switch llvmType {
	case "i1":
		return 1
	case "i8":
		return 1
	case "i16":
		return 2
	case "i32":
		return 4
	case "i64", "i8*", "double":
		return 8
	case "%str":
		return 16
	case "%arr":
		return 16
	case "%vec":
		return 24
	case "%option":
		return 24
	case "%str-smail":
		return 128
	case "float":
		return 4
	default:
		return 8
	}
}

func (g *Generator) emitLifetimeEnd(sb *strings.Builder) {
	for _, v := range g.funcVars {
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.end.p0i8(i64 %d, i8* %%%s)\n", g.indent(), v.Size, v.Name))
	}
}

func (g *Generator) generateFunctionDefinition(sb *strings.Builder, fd *parser.FunctionDefinition) {
	g.funcVars = nil
	g.varTypes = make(map[string]string) // reset varTypes for each function
	if g.paramNames == nil {
		g.paramNames = make(map[string]bool)
	}
	for _, p := range fd.Parameters {
		g.paramNames[p.Name] = true
		g.varTypes[p.Name] = g.mapToLLVMType(p.Type.String())
	}

	returnType := "void"
	if len(fd.Results) > 0 {
		returnType = g.mapToLLVMType(fd.Results[0].Type.String())
	}

	sb.WriteString(fmt.Sprintf("define %s @%s(", returnType, fd.Name))

	for i, param := range fd.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		// 引用傳遞：參數為指標 i64* %n
		llvmType := g.mapToLLVMType(param.Type.String()) + "*"
		sb.WriteString(fmt.Sprintf("%s %%%s", llvmType, param.Name))
	}

	sb.WriteString(") {\n")
	g.indentLevel++
	sb.WriteString(g.indent() + "entry:\n")
	g.indentLevel++

	// 收集所有變數（一次分配），排除參數（已是指標）
	localVarTypes := make(map[string]string)
	g.collectVarDeclsFromStmt(fd.Body, localVarTypes)
	for k, v := range localVarTypes {
		g.varTypes[k] = v
	}
	// 確保 range 變數有型別
	g.collectRangeVarTypes(fd.Body, localVarTypes)
	for k, v := range localVarTypes {
		g.varTypes[k] = v
	}
	// 結果參數也要分配空間
	for _, r := range fd.Results {
		if r.Name != "" {
			llvmType := g.mapToLLVMType(r.Type.String())
			localVarTypes[r.Name] = llvmType
			g.varTypes[r.Name] = llvmType
		}
	}

	for _, p := range fd.Parameters {
		delete(localVarTypes, p.Name)
	}

	// 分配 + lifetime.start
	for varName, varType := range localVarTypes {
		sz := g.llvmTypeSize(varType)
		g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: varType, Size: sz})
		sb.WriteString(fmt.Sprintf("%s%%%s = alloca %s\n", g.indent(), varName, varType))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %%%s)\n", g.indent(), sz, varName))
	}

	// 參數化為指標（引用傳遞模型）
	for _, param := range fd.Parameters {
		llvmType := g.mapToLLVMType(param.Type.String())
		// 已是指標型別，不需 alloca/store
		// %n 是 i64*，可直接 load
		_ = llvmType
	}

	if fd.Body != nil {
		for _, stmt := range fd.Body.Statements {
			g.generateStatement(sb, stmt)
		}
	}

	// 若函數無 return 陳述句（void），自動銷毀 + return
	if returnType == "void" {
		g.emitLifetimeEnd(sb)
		sb.WriteString(g.indent() + "ret void\n")
	}
	g.indentLevel--
	g.indentLevel--
	sb.WriteString("}\n\n")
}

func (g *Generator) generateMainFunction(sb *strings.Builder, program *parser.Program) {
	g.funcVars = nil

	hasTopLevel := false
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); !ok {
			if _, ok := stmt.(*parser.StructDefinition); !ok {
				hasTopLevel = true
				break
			}
		}
	}

	if !hasTopLevel {
		return
	}

	sb.WriteString("define i32 @main() {\n")
	g.indentLevel++
	sb.WriteString(g.indent() + "entry:\n")
	g.indentLevel++

	varDecls := g.collectVarDecls(program)
	for k, v := range varDecls {
		g.varTypes[k] = v
	}
	for varName, varType := range varDecls {
		sz := g.llvmTypeSize(varType)
		g.funcVars = append(g.funcVars, varInfo{Name: varName, Type: varType, Size: sz})
		sb.WriteString(fmt.Sprintf("%s%%%s = alloca %s\n", g.indent(), varName, varType))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 %d, i8* %%%s)\n", g.indent(), sz, varName))
	}

	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); !ok {
			if _, ok := stmt.(*parser.StructDefinition); !ok {
				g.generateStatement(sb, stmt)
			}
		}
	}

	g.emitLifetimeEnd(sb)
	sb.WriteString(g.indent() + "ret i32 0\n")
	g.indentLevel--
	g.indentLevel--
	sb.WriteString("}\n\n")
}

func (g *Generator) varLLVMType(stmt *parser.LetStatement) string {
	// Option type: ?type
	if _, ok := stmt.Type.(*parser.NullableType); ok {
		return "%option"
	}
	// 結構體
	if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
		if t, ok := g.varTypes[sl.Type]; ok {
			return t
		}
		return "%" + sl.Type
	}
	// 陣列/切片
	if _, ok := stmt.Type.(*parser.ArrayType); ok {
		return "%arr"
	}
	if _, ok := stmt.Type.(*parser.SliceType); ok {
		return "%vec"
	}
	// Type-only declaration: a i8 (no initializer)
	if stmt.Value == nil && stmt.Type != nil {
		return g.mapToLLVMType(stmt.Type.String())
	}
	switch v := stmt.Value.(type) {
	case *parser.Identifier:
		// Look up the type of the source variable
		if g.varTypes != nil {
			if t, ok := g.varTypes[v.Value]; ok {
				return t
			}
		}
		return "i64"
	case *parser.StringLiteral:
		if len(v.Value) <= 127 {
			return "%str-smail"
		}
		return "%str"
	case *parser.InfixExpression:
		if v.Operator == "-" && (g.isStringExpr(v.Left) || g.isStringExpr(v.Right)) {
			return "%str"
		}
		return "i64"
	case *parser.SliceLiteral:
		return "%vec"
	case *parser.SliceExpression:
		// Check source type: slicing %str/%str-smail produces %str, otherwise %vec
		if ident, ok := v.Left.(*parser.Identifier); ok {
			if g.varTypes != nil {
				if t, ok := g.varTypes[ident.Value]; ok && (t == "%str" || t == "%str-smail") {
					return "%str"
				}
			}
		}
		return "%vec"
	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			name := ident.Value
			strFns := map[string]bool{
				"i64-to-str": true, "i32-to-str": true, "i16-to-str": true, "i8-to-str": true,
				"u64-to-str": true, "u32-to-str": true, "u16-to-str": true, "u8-to-str": true,
				"f64-to-str": true, "f32-to-str": true,
				"bool-to-str": true, "byte-to-str": true, "char-to-str": true,
				"get-env": true, "get-wd": true, "host-name": true,
			}
			if strFns[name] {
				return "%str"
			}
			f64Fns := map[string]bool{
				"str-to-f64": true, "str-to-f32": true,
			}
			if f64Fns[name] {
				return "double"
			}
		}
		return "i64"
	case *parser.GroupedExpression:
		return g.varLLVMType(&parser.LetStatement{Value: v.Expression})
	default:
		return "i64"
	}
}

func (g *Generator) collectRangeVarTypes(stmt parser.Statement, vars map[string]string) {
	switch s := stmt.(type) {
	case *parser.ForStatement:
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, ok := vars[s.IterRange.Variable]; !ok {
				vars[s.IterRange.Variable] = "i64"
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.collectRangeVarTypes(ss, vars)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.collectRangeVarTypes(ss, vars)
		}
	}
}

func (g *Generator) collectVarDecls(program *parser.Program) map[string]string {
	vars := make(map[string]string)
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			// Don't overwrite existing type — first declaration wins (e.g., a i8; a = 2)
			if _, exists := vars[s.Name.Value]; !exists {
				t := g.varLLVMType(s)
				vars[s.Name.Value] = t
				g.varTypes[s.Name.Value] = t // register immediately for later varLLVMType calls
			}
		case *parser.FunctionDefinition:
			if s.Body != nil {
				for _, bodyStmt := range s.Body.Statements {
					g.collectVarDeclsFromStmt(bodyStmt, vars)
				}
			}
		default:
			g.collectVarDeclsFromStmt(s, vars)
		}
	}
	return vars
}

func (g *Generator) collectVarDeclsFromStmt(stmt parser.Statement, vars map[string]string) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// Don't overwrite existing type (e.g. %option declared with ?type)
		if _, exists := vars[s.Name.Value]; !exists {
			vt := g.varLLVMType(s)
			vars[s.Name.Value] = vt
			// Update g.varTypes immediately so subsequent lookups work
			if g.varTypes != nil {
				g.varTypes[s.Name.Value] = vt
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			g.collectVarDeclsFromStmt(s.Init, vars)
		}
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, ok := vars[s.IterRange.Variable]; !ok {
				vars[s.IterRange.Variable] = "i64"
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
	case *parser.ExpressionStatement:
		g.collectVarDeclsFromExpr(s.Expression, vars)
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.collectVarDeclsFromStmt(ss, vars)
		}
	}
}

func (g *Generator) collectVarDeclsFromExpr(expr parser.Expression, vars map[string]string) {
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				g.collectVarDeclsFromStmt(ss, vars)
			}
		}
	}
}

func (g *Generator) collectStructType(sd *parser.StructDefinition) {
	var fields []structField
	for _, f := range sd.Fields {
		llvmType := "i64"
		if f.ArraySize > 0 {
			elemType := "i64"
			if f.Type != nil {
				elemType = g.mapToLLVMType(f.Type.String())
			}
			llvmType = fmt.Sprintf("[%d x %s]", f.ArraySize, elemType)
		} else if f.IsSlice {
			// 切片用 %vec 型別
			llvmType = "%vec"
		} else if f.Type != nil {
			llvmType = g.mapToLLVMType(f.Type.String())
		}
		fields = append(fields, structField{name: f.Name, typ: llvmType})
	}
	g.structTypes[sd.Name] = fields
	// 註冊 struct type 名稱
	g.varTypes[sd.Name] = "%" + sd.Name
}

func (g *Generator) generateStatement(sb *strings.Builder, stmt parser.Statement) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		g.generateLet(sb, s)
	case *parser.ExpressionStatement:
		g.generateExpressionStmt(sb, s)
	case *parser.ForStatement:
		g.generateForStatement(sb, s)
	case *parser.BreakStatement:
		g.emitLifetimeEnd(sb)
		target := g.findLoopTarget(s.Label, true)
		if target != "" {
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), target))
		}

	case *parser.ContinueStatement:
		target := g.findLoopTarget(s.Label, false)
		if target != "" {
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), target))
		}

	case *parser.InterfaceDefinition:
		// 介面定義不生成 LLVM IR

	case *parser.EnumDefinition:
		g.generateEnumDefinition(sb, s)

	case *parser.StructDefinition:
		// type already emitted in Generate()
	// struct definition 本身不生成 IR（type 已由 Generate 發出）
	case *parser.ReturnStatement:
		g.emitLifetimeEnd(sb)
		if s.ReturnValue != nil {
			val := g.generateExprWithSB(sb, s.ReturnValue)
			sb.WriteString(fmt.Sprintf("%sret i64 %s\n", g.indent(), val))
		} else {
			sb.WriteString(g.indent() + "ret void\n")
		}
	}
}

func (g *Generator) generateForStatement(sb *strings.Builder, stmt *parser.ForStatement) {
	// range for: for i in [a..b] — push/pop handled in generateRangeFor
	if stmt.IterRange != nil {
		g.generateRangeFor(sb, stmt)
		return
	}

	// Push loop exit target
	g.tmpIdx++
	labelId := g.tmpIdx

	// 次數循環：N * { }
	if stmt.CountExpr != nil {
		counterVar := fmt.Sprintf("__lc_%d", labelId)
		// init: %__lc_N = alloca i64, store 0
		sb.WriteString(fmt.Sprintf("%s%%%s = alloca i64\n", g.indent(), counterVar))
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), counterVar))

		g.loopExits = append(g.loopExits, loopExit{
			name: stmt.Label,
			cond: fmt.Sprintf("for.cond.%d", labelId),
			exit: fmt.Sprintf("for.end.%d", labelId),
		})
		defer func() {
			g.loopExits = g.loopExits[:len(g.loopExits)-1]
		}()

		// br → cond
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))

		// cond block
		sb.WriteString(fmt.Sprintf("for.cond.%d:\n", labelId))
		g.indentLevel++
		counterLoad := fmt.Sprintf("%%%s.val", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), counterLoad, counterVar))
		cmpVal := g.generateExprWithSB(sb, stmt.CountExpr)
		cmpResult := fmt.Sprintf("%%%s.cmp", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpResult, counterLoad, cmpVal))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%for.body.%d, label %%for.end.%d\n",
			g.indent(), cmpResult, labelId, labelId))
		g.indentLevel--

		// body block
		sb.WriteString(fmt.Sprintf("for.body.%d:\n", labelId))
		g.indentLevel++
		if stmt.Body != nil {
			for _, s := range stmt.Body.Statements {
				g.generateStatement(sb, s)
			}
		}
		// update: %val = load i64, %cnt; %inc = add i64 %val, 1; store i64 %inc, %cnt
		updateLoad := fmt.Sprintf("%%%s.val2", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), updateLoad, counterVar))
		updateInc := fmt.Sprintf("%%%s.inc", counterVar)
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), updateInc, updateLoad))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), updateInc, counterVar))
		sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
		g.indentLevel--

		// end block
		sb.WriteString(fmt.Sprintf("for.end.%d:\n", labelId))
		return
	}

	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("for.cond.%d", labelId),
		exit: fmt.Sprintf("for.end.%d", labelId),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// init
	if stmt.Init != nil {
		g.generateStatement(sb, stmt.Init)
	}

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))

	// cond block
	sb.WriteString(fmt.Sprintf("for.cond.%d:\n", labelId))
	g.indentLevel++
	condVal := ""
	if stmt.Condition != nil {
		// 若條件是 InfixExpression（比較運算），直接取 i1
		if infix, ok := stmt.Condition.(*parser.InfixExpression); ok {
			isCmp := infix.Operator == "==" || infix.Operator == "!=" ||
				infix.Operator == "<" || infix.Operator == ">" ||
				infix.Operator == "<=" || infix.Operator == ">="
			if isCmp {
				condVal = g.generateInfixI1(sb, infix)
			} else {
				condVal = g.generateExprWithSB(sb, stmt.Condition)
			}
		} else {
			condVal = g.generateExprWithSB(sb, stmt.Condition)
		}
	} else {
		condVal = "1" // infinite loop
	}
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%for.body.%d, label %%for.end.%d\n",
		g.indent(), condVal, labelId, labelId))
	g.indentLevel--

	// body block
	sb.WriteString(fmt.Sprintf("for.body.%d:\n", labelId))
	g.indentLevel++
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// update
	if stmt.Update != nil {
		g.generateStatement(sb, stmt.Update)
	}
	sb.WriteString(fmt.Sprintf("%sbr label %%for.cond.%d\n", g.indent(), labelId))
	g.indentLevel--

	// end block
	sb.WriteString(fmt.Sprintf("for.end.%d:\n", labelId))
}

func (g *Generator) generateStringRange(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	varName := ir.Variable
	str := ir.RangeStr
	g.tmpIdx++
	lbl := g.tmpIdx

	// 建立字串常數
	idx := g.stringIdx
	g.stringIdx++
	escaped := g.escapeLLVMString(str)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\"", idx, len(str), escaped))

	g.tmpIdx++
	idxReg := fmt.Sprintf("%%stridx.%d", g.tmpIdx)
	g.tmpIdx++
	ptrReg := fmt.Sprintf("%%strptr.%d", g.tmpIdx)

	// init: idx = 0
	sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), idxReg))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), idxReg, varName))
	sb.WriteString(fmt.Sprintf("%s%s = add i64 0, 0\n", g.indent(), ptrReg))

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%str.cond.%d\n", g.indent(), lbl))

	// cond block
	sb.WriteString(fmt.Sprintf("str.cond.%d:\n", lbl))
	g.indentLevel++
	g.tmpIdx++
	iLoad := fmt.Sprintf("%%stri.%d", g.tmpIdx)
	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%strcmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %d\n", g.indent(), cmpReg, iLoad, len(str)))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%str.body.%d, label %%str.end.%d\n", g.indent(), cmpReg, lbl, lbl))
	g.indentLevel--

	// body: char = str[i]; varName = char
	sb.WriteString(fmt.Sprintf("str.body.%d:\n", lbl))
	g.indentLevel++
	g.tmpIdx++
	chReg := fmt.Sprintf("%%strch.%d", g.tmpIdx)
	g.tmpIdx++
	chZext := fmt.Sprintf("%%strchz.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds [%d x i8], [%d x i8]* @.str.%d, i64 0, i64 %s\n",
		g.indent(), chReg, len(str)+1, len(str)+1, idx, iLoad))
	sb.WriteString(fmt.Sprintf("%s%s = load i8, i8* %s\n", g.indent(), chZext, chReg))
	g.tmpIdx++
	charVal := fmt.Sprintf("%%strcv.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = zext i8 %s to i64\n", g.indent(), charVal, chZext))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), charVal, varName))

	// body statements
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}

	// update: idx++
	g.tmpIdx++
	iNext := fmt.Sprintf("%%strnext.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	sb.WriteString(fmt.Sprintf("%sbr label %%str.cond.%d\n", g.indent(), lbl))
	g.indentLevel--

	sb.WriteString(fmt.Sprintf("str.end.%d:\n", lbl))
}

func (g *Generator) generateArrayRange(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	varName := ir.Variable
	var structPtr string // "%arr* %%identName" or "%vec* %vec.tmp.N"
	var structType string
	var isVec bool
	var elemType string

	// Determine the source: named variable or inline slice literal
	if ident, ok := ir.RangeExpr.(*parser.Identifier); ok {
		// Named variable: for i in a
		identName := ident.Value
		structType = g.varTypes[identName]
		isVec = structType == "%vec"
		if structType == "" {
			structType = "%arr"
		}
		structPtr = fmt.Sprintf("%%%s", identName)

		// Get element type
		elemType = "i64"
		if et, ok := g.arrayElemTypes[identName]; ok {
			elemType = et
		}
	} else if sliceLit, ok := ir.RangeExpr.(*parser.SliceLiteral); ok {
		// Inline slice literal: for i in [1, 2, 3]
		structType = "%vec"
		isVec = true
		elemType = "i64"

		g.tmpIdx++
		tid := g.tmpIdx
		tmpVec := fmt.Sprintf("%%vec.tmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), tmpVec))

		n := int64(len(sliceLit.Elements))

		// alloca temp array
		arrType := fmt.Sprintf("[%d x %s]", n, elemType)
		tmpArr := fmt.Sprintf("%%slice.tmp.%d", tid)
		sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))

		// store elements via GEP
		for i, elem := range sliceLit.Elements {
			ev := g.generateExprWithSB(sb, elem)
			ev = g.stripLLVMType(ev)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%slice.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
				g.indent(), gepReg, arrType, arrType, tmpArr, i))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, ev, elemType, gepReg))
		}

		// bitcast to i8*
		g.tmpIdx++
		ptrReg := fmt.Sprintf("%%slice.ptr.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), ptrReg, arrType, tmpArr))

		// store len (field 0)
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%vec.len.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 0\n",
			g.indent(), lenGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

		// store cap (field 1)
		g.tmpIdx++
		capGEP := fmt.Sprintf("%%vec.cap.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 1\n",
			g.indent(), capGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

		// store data (field 2)
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%vec.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %s, i32 0, i32 2\n",
			g.indent(), dataGEP, tmpVec))
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), ptrReg, dataGEP))

		structPtr = tmpVec
	}

	g.tmpIdx++
	lbl := g.tmpIdx
	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("arr.cond.%d", lbl),
		exit: fmt.Sprintf("arr.end.%d", lbl),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// Load len (field 0 for both %arr and %vec)
	g.tmpIdx++
	lenGEP := fmt.Sprintf("%%arr.len.gep.%d", g.tmpIdx)
	g.tmpIdx++
	lenLoad := fmt.Sprintf("%%arr.len.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 0\n",
		g.indent(), lenGEP, structType, structType, structPtr))
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), lenLoad, lenGEP))

	// Initialize i = 0
	sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %%%s\n", g.indent(), varName))

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))

	// cond block: i < len
	sb.WriteString(fmt.Sprintf("arr.cond.%d:\n", lbl))
	g.indentLevel++
	g.tmpIdx++
	iLoad := fmt.Sprintf("%%arr.i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%arr.cmp.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = icmp slt i64 %s, %s\n", g.indent(), cmpReg, iLoad, lenLoad))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%arr.body.%d, label %%arr.end.%d\n", g.indent(), cmpReg, lbl, lbl))
	g.indentLevel--

	// body block
	sb.WriteString(fmt.Sprintf("arr.body.%d:\n", lbl))
	g.indentLevel++

	// Load element from data[i]
	// Data field index: %arr → field 1, %vec → field 2
	dataField := uint32(1)
	if isVec {
		dataField = 2
	}
	g.tmpIdx++
	dataGEP := fmt.Sprintf("%%arr.data.gep.%d", g.tmpIdx)
	g.tmpIdx++
	dataLoad := fmt.Sprintf("%%arr.data.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
		g.indent(), dataGEP, structType, structType, structPtr, dataField))
	sb.WriteString(fmt.Sprintf("%s%s = load i8*, i8** %s\n", g.indent(), dataLoad, dataGEP))

	// Bitcast data to element type pointer
	g.tmpIdx++
	castReg := fmt.Sprintf("%%arr.cast.%d", g.tmpIdx)
	ptrType := elemType + "*"
	sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s\n", g.indent(), castReg, dataLoad, ptrType))

	// GEP into element array by index
	g.tmpIdx++
	elemGEP := fmt.Sprintf("%%arr.elem.gep.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s %s, i64 %s\n",
		g.indent(), elemGEP, elemType, ptrType, castReg, iLoad))

	// Load element value
	g.tmpIdx++
	elemLoad := fmt.Sprintf("%%arr.elem.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load %s, %s* %s\n", g.indent(), elemLoad, elemType, ptrType, elemGEP))

	// Store element into loop variable
	g.varTypes[varName] = elemType
	ptr2 := elemType + "*"
	sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %%%s\n", g.indent(), elemType, elemLoad, ptr2, varName))

	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}

	// Update: i++
	g.tmpIdx++
	iNext := fmt.Sprintf("%%arr.next.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iNext, iLoad))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	sb.WriteString(fmt.Sprintf("%sbr label %%arr.cond.%d\n", g.indent(), lbl))
	g.indentLevel--

	// end block
	sb.WriteString(fmt.Sprintf("arr.end.%d:\n", lbl))
}

func (g *Generator) generateRangeFor(sb *strings.Builder, stmt *parser.ForStatement) {
	ir := stmt.IterRange
	// 字串遍歷: for i in 'hello'
	if ir.RangeStr != "" {
		g.generateStringRange(sb, stmt)
		return
	}

	// 陣列/切片遍歷: for i in a
	if ir.RangeExpr != nil {
		g.generateArrayRange(sb, stmt)
		return
	}

	r := ir.Range
	varName := ir.Variable
	g.tmpIdx++
	lbl := g.tmpIdx
	g.loopExits = append(g.loopExits, loopExit{
		name: stmt.Label,
		cond: fmt.Sprintf("rng.cond.%d", lbl),
		exit: fmt.Sprintf("rng.end.%d", lbl),
	})
	defer func() {
		g.loopExits = g.loopExits[:len(g.loopExits)-1]
	}()

	// 計算 start 和 end 值
	startVal := g.generateExprWithSB(sb, r.Start)
	endVal := g.generateExprWithSB(sb, r.End)

	// 計算初始值 i = start (或 start+1 若左開)
	g.tmpIdx++
	iInit := fmt.Sprintf("%%rng.init.%d", g.tmpIdx)
	if r.LeftInc {
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 0\n", g.indent(), iInit, startVal))
	} else {
		g.tmpIdx++
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iInit, startVal))
	}
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iInit, varName))

	// br → cond
	sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))

	// cond block: 判斷方向並比較
	sb.WriteString(fmt.Sprintf("rng.cond.%d:\n", lbl))
	g.indentLevel++
	g.tmpIdx++
	cmpReg := fmt.Sprintf("%%rng.cmp.%d", g.tmpIdx)
	g.tmpIdx++
	selReg := fmt.Sprintf("%%rng.sel.%d", g.tmpIdx)
	// 先載入目前 i 值
	g.tmpIdx++
	iLoad := fmt.Sprintf("%%rng.i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %%%s\n", g.indent(), iLoad, varName))
	// 判斷方向並選擇比較結果
	// 若 a <= b 則用 i <= b (ascending)，否則 i >= b (descending)
	g.tmpIdx++
	ascCmp := fmt.Sprintf("%%rng.asc.%d", g.tmpIdx)
	g.tmpIdx++
	descCmp := fmt.Sprintf("%%rng.desc.%d", g.tmpIdx)
	cmpOp := "sle"
	if !r.RightInc {
		cmpOp = "slt"
	}
	// ascending: i <= end
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), ascCmp, cmpOp, iLoad, endVal))
	// descending: i >= end
	descOp := "sge"
	if !r.RightInc {
		descOp = "sgt"
	}
	sb.WriteString(fmt.Sprintf("%s%s = icmp %s i64 %s, %s\n", g.indent(), descCmp, descOp, iLoad, endVal))
	// 選擇 ascending vs descending
	sb.WriteString(fmt.Sprintf("%s%s = icmp sle i64 %s, %s\n", g.indent(), cmpReg, startVal, endVal))
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i1 %s, i1 %s\n", g.indent(), selReg, cmpReg, ascCmp, descCmp))
	sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%rng.body.%d, label %%rng.end.%d\n", g.indent(), selReg, lbl, lbl))
	g.indentLevel--

	// body block
	sb.WriteString(fmt.Sprintf("rng.body.%d:\n", lbl))
	g.indentLevel++
	if stmt.Body != nil {
		for _, s := range stmt.Body.Statements {
			g.generateStatement(sb, s)
		}
	}
	// update: 依方向 i = i ± 1
	g.tmpIdx++
	iUp := fmt.Sprintf("%%rng.up.%d", g.tmpIdx)
	g.tmpIdx++
	iDown := fmt.Sprintf("%%rng.down.%d", g.tmpIdx)
	g.tmpIdx++
	iNext := fmt.Sprintf("%%rng.next.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), iUp, iLoad))
	sb.WriteString(fmt.Sprintf("%s%s = sub i64 %s, 1\n", g.indent(), iDown, iLoad))
	sb.WriteString(fmt.Sprintf("%s%s = select i1 %s, i64 %s, i64 %s\n", g.indent(), iNext, cmpReg, iUp, iDown))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), iNext, varName))
	sb.WriteString(fmt.Sprintf("%sbr label %%rng.cond.%d\n", g.indent(), lbl))
	g.indentLevel--

	// end block
	sb.WriteString(fmt.Sprintf("rng.end.%d:\n", lbl))
}

func (g *Generator) generateLet(sb *strings.Builder, stmt *parser.LetStatement) {
	name := stmt.Name.Value

	// 切片儲存：使用 %vec 結構體
	_, isSliceLit := stmt.Value.(*parser.SliceLiteral)
	_, isSliceExpr := stmt.Value.(*parser.SliceExpression)
	_, isSliceType := stmt.Type.(*parser.SliceType)
	if (isSliceType || isSliceLit) && !isSliceExpr {
		if isSliceLit {
			slice := stmt.Value.(*parser.SliceLiteral)
			elemType := "i64"
			if st, ok := stmt.Type.(*parser.SliceType); ok && st.Elem != nil {
				elemType = g.mapToLLVMType(st.Elem.String())
			}
			n := int64(len(slice.Elements))
			g.tmpIdx++
			tid := g.tmpIdx
			tmpArr := fmt.Sprintf("%%slice.tmp.%d", tid)
			arrType := fmt.Sprintf("[%d x %s]", n, elemType)

			// alloca temp array on stack
			sb.WriteString(fmt.Sprintf("%s%s = alloca %s\n", g.indent(), tmpArr, arrType))

			// store each element via GEP
			for i, elem := range slice.Elements {
				ev := g.generateExprWithSB(sb, elem)
				ev = g.stripLLVMType(ev)
				g.tmpIdx++
				gepReg := fmt.Sprintf("%%slice.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d\n",
					g.indent(), gepReg, arrType, arrType, tmpArr, i))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), elemType, ev, elemType, gepReg))
			}

			// bitcast array pointer to i8* (matches %vec.data field type)
			g.tmpIdx++
			ptrReg := fmt.Sprintf("%%slice.ptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i8*\n", g.indent(), ptrReg, arrType, tmpArr))

			// store len (field 0)
			g.tmpIdx++
			lenGEP := fmt.Sprintf("%%vec.len.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %%%s, i32 0, i32 0\n",
				g.indent(), lenGEP, name))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, lenGEP))

			// store cap (field 1)
			g.tmpIdx++
			capGEP := fmt.Sprintf("%%vec.cap.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %%%s, i32 0, i32 1\n",
				g.indent(), capGEP, name))
			sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), n, capGEP))

			// store data (field 2)
			g.tmpIdx++
			dataGEP := fmt.Sprintf("%%vec.data.gep.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%vec, %%vec* %%%s, i32 0, i32 2\n",
				g.indent(), dataGEP, name))
			sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), ptrReg, dataGEP))
			return
		}

		// Non-literal slice assignment — not yet supported
		sb.WriteString(fmt.Sprintf("%s; slice non-literal store not yet implemented\n", g.indent()))
		return
	}

	// Option type assignment: handle nil, val(), err(), and implicit values
	llvmTypeCheck := g.varLLVMType(stmt)
	// Also check if variable already has %option type (reassignment)
	if llvmTypeCheck != "%option" && g.varTypes != nil {
		if t, ok := g.varTypes[stmt.Name.Value]; ok && t == "%option" {
			llvmTypeCheck = "%option"
		}
	}
	if llvmTypeCheck == "%option" {
		// Ensure variable is allocated (needed for `it = x` injected in match arm bodies)
		if g.varTypes != nil {
			if _, exists := g.varTypes[name]; !exists {
				g.varTypes[name] = "%option"
				sb.WriteString(fmt.Sprintf("%s%s = alloca %%option\n", g.indent(), name))
				sb.WriteString(fmt.Sprintf("%scall void @llvm.lifetime.start.p0i8(i64 24, i8* %%%s)\n", g.indent(), name))
			}
		}
		g.generateOptionAssign(sb, stmt)
		return
	}

	val := g.generateExprWithSB(sb, stmt.Value)
	val = g.stripLLVMType(val)
	llvmType := g.varLLVMType(stmt)

	// 結構體儲存
	if sl, ok := stmt.Value.(*parser.StructLiteral); ok {
		structName := sl.Type
		fields := g.structTypes[structName]
		// 逐欄位 store
		for i, f := range sl.Fields {
			fieldType := "i64"
			if i < len(fields) {
				fieldType = fields[i].typ
			}
			fieldVal := g.generateExprWithSB(sb, f.Value)
			fieldVal = g.stripLLVMType(fieldVal)
			g.tmpIdx++
			gepReg := fmt.Sprintf("%%st.gep.%d", g.tmpIdx)
			structTy := "%" + structName
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %%%s, i32 0, i32 %d\n",
				g.indent(), gepReg, structTy, structTy, name, i))
			sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n", g.indent(), fieldType, fieldVal, fieldType, gepReg))
		}
		return
	}

	if at, ok := stmt.Type.(*parser.ArrayType); ok && at.Size != nil {
		var arraySize int64
		if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
			arraySize = intLit.Value
		}
		elemType := "i64"
		if at.Elem != nil {
			elemType = at.Elem.String()
		}
		llvmElemType := g.mapToLLVMType(elemType)
		elemSize := g.llvmTypeSize(llvmElemType)

		// Register element type for later index resolution
		g.arrayElemTypes[name] = llvmElemType

		// Store len field
		g.tmpIdx++
		lenGEP := fmt.Sprintf("%%arr.len.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %%%s, i32 0, i32 0\n",
			g.indent(), lenGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), arraySize, lenGEP))

		// Allocate data buffer: arraySize * elemSize
		totalSize := arraySize * elemSize
		g.tmpIdx++
		dataReg := fmt.Sprintf("%%arr.data.malloc.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %d)\n", g.indent(), dataReg, totalSize))

		// Store data pointer in struct
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%arr.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%arr, %%arr* %%%s, i32 0, i32 1\n",
			g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %s\n", g.indent(), dataReg, dataGEP))

		// Store elements from array literal (if any)
		if arrLit, ok := stmt.Value.(*parser.ArrayLiteral); ok && len(arrLit.Elements) > 0 {
			g.tmpIdx++
			dataCast := fmt.Sprintf("%%arr.data.cast.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast i8* %s to %s*\n",
				g.indent(), dataCast, dataReg, llvmElemType))

			for i, elem := range arrLit.Elements {
				ev := g.generateExprWithSB(sb, elem)
				ev = g.stripLLVMType(ev)
				g.tmpIdx++
				elemGEP := fmt.Sprintf("%%arr.elem.gep.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %s, %s* %s, i64 %d\n",
					g.indent(), elemGEP, llvmElemType, llvmElemType, dataCast, i))
				sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %s\n",
					g.indent(), llvmElemType, ev, llvmElemType, elemGEP))
			}
		}
		return
	}

	// Coerce value to declared type if variable was already typed (e.g., a i8; a = 2)
	if existingType, ok := g.varTypes[name]; ok && existingType != llvmType {
		if g.isIntegerLLVMType(existingType) && g.isIntegerLLVMType(llvmType) {
			if strings.HasPrefix(val, "%") {
				// Register value — insert trunc instruction
				g.tmpIdx++
				truncReg := fmt.Sprintf("%%trunc.%d", g.tmpIdx)
				sb.WriteString(fmt.Sprintf("%s%s = trunc %s %s to %s\n", g.indent(), truncReg, llvmType, val, existingType))
				val = truncReg
			}
			llvmType = existingType
		}
	}

	switch llvmType {
	case "%str":
		// Copy %str struct: load from source, store to dest
		g.tmpIdx++
		copyReg := fmt.Sprintf("%%str.copy.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %%str, %%str* %s\n", g.indent(), copyReg, val))
		sb.WriteString(fmt.Sprintf("%sstore %%str %s, %%str* %%%s\n", g.indent(), copyReg, name))
	case "%str-smail":
		// Copy %str-smail struct: load from source, store to dest
		g.tmpIdx++
		copyReg := fmt.Sprintf("%%strsm.copy.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %%str-smail, %%str-smail* %s\n", g.indent(), copyReg, val))
		sb.WriteString(fmt.Sprintf("%sstore %%str-smail %s, %%str-smail* %%%s\n", g.indent(), copyReg, name))
	case "%vec":
		// Copy %vec struct: load from source, store to dest
		g.tmpIdx++
		copyReg := fmt.Sprintf("%%vec.copy.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %%vec, %%vec* %s\n", g.indent(), copyReg, val))
		sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %%%s\n", g.indent(), copyReg, name))
	case "i8*":
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %%%s\n", g.indent(), val, name))
	case "double":
		sb.WriteString(fmt.Sprintf("%sstore double %s, double* %%%s\n", g.indent(), val, name))
	default:
		ptrType := llvmType + "*"
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s %%%s\n", g.indent(), llvmType, val, ptrType, name))
	}
}

func (g *Generator) isIntegerLLVMType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i1":
		return true
	}
	return false
}

func (g *Generator) stripLLVMType(val string) string {
	prefixes := []string{"i8* ", "i64 ", "i32 ", "i16 ", "i8 ", "double ", "float ", "i1 "}
	for _, p := range prefixes {
		if strings.HasPrefix(val, p) {
			return val[len(p):]
		}
	}
	return val
}

func (g *Generator) generateExpressionStmt(sb *strings.Builder, stmt *parser.ExpressionStatement) {
	if stmt.Expression == nil {
		return
	}

	switch e := stmt.Expression.(type) {
	case *parser.CallExpression:
		result := g.generateCallExpression(sb, e)
		if strings.HasPrefix(result, "call ") {
			sb.WriteString(g.indent() + result + "\n")
		}
	case *parser.AssignExpression:
		// 欄位賦值: u.name = value
		g.generateAssignExpression(sb, e)
		// 結果已由 generateAssignExpression 寫入 sb
	default:
		// 用 sb 確保 side effect（如 ++/--）被發出
		_ = g.generateExprWithSB(sb, e)
	}
}

// generateOptionAssign handles assignment to %option typed variables.
// Cases: nil, val(x), err(x), implicit value (tag=0).
func (g *Generator) generateOptionAssign(sb *strings.Builder, stmt *parser.LetStatement) {
	name := stmt.Name.Value

	// Helper: store tag value
	storeTag := func(tag int64) {
		g.tmpIdx++
		tagGEP := fmt.Sprintf("%%opt.tag.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 0\n", g.indent(), tagGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore i64 %d, i64* %s\n", g.indent(), tag, tagGEP))
	}

	// Helper: zero data field
	zeroData := func() {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%sstore [16 x i8] zeroinitializer, [16 x i8]* %s\n", g.indent(), dataGEP))
	}

	// Helper: copy a %str struct into data field (16 bytes)
	copyStrToData := func(srcPtr string) {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtr := fmt.Sprintf("%%opt.data.ptr.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [16 x i8]* %s to %%str*\n", g.indent(), dataPtr, dataGEP))
		g.tmpIdx++
		copyReg := fmt.Sprintf("%%opt.str.copy.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = load %%str, %%str* %s\n", g.indent(), copyReg, srcPtr))
		sb.WriteString(fmt.Sprintf("%sstore %%str %s, %%str* %s\n", g.indent(), copyReg, dataPtr))
	}

	// Helper: copy an i64 value into data field
	copyI64ToData := func(val string) {
		g.tmpIdx++
		dataGEP := fmt.Sprintf("%%opt.data.gep.%d", g.tmpIdx)
		g.tmpIdx++
		dataPtr := fmt.Sprintf("%%opt.data.ptr.%d", g.tmpIdx)
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds %%option, %%option* %%%s, i32 0, i32 1\n", g.indent(), dataGEP, name))
		sb.WriteString(fmt.Sprintf("%s%s = bitcast [16 x i8]* %s to i64*\n", g.indent(), dataPtr, dataGEP))
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), val, dataPtr))
	}

	switch v := stmt.Value.(type) {
	case *parser.NilLiteral:
		// x = nil → tag=1, zero data
		storeTag(1)
		zeroData()

	case *parser.CallExpression:
		if ident, ok := v.Function.(*parser.Identifier); ok {
			if ident.Value == "val" && len(v.Arguments) == 1 {
				// x = val(expr) → tag=0, copy expr to data
				storeTag(0)
				arg := v.Arguments[0]
				if _, isStr := arg.(*parser.StringLiteral); isStr {
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else if argIdent, isIdent := arg.(*parser.Identifier); isIdent {
					if t, ok := g.varTypes[argIdent.Value]; ok && (t == "%str" || t == "%str-smail") {
						// String variable: load and copy %str struct
						// For %str, copy directly
						if t == "%str" {
							copyStrToData("%" + argIdent.Value)
						} else {
							// %str-smail: need to convert to %str first (not yet supported, store as i64 placeholder)
							zeroData()
						}
					} else {
						// i64 variable
						val := g.generateExprWithSB(sb, arg)
						copyI64ToData(val)
					}
				} else {
					val := g.generateExprWithSB(sb, arg)
					copyI64ToData(val)
				}
				return
			}
			if ident.Value == "err" && len(v.Arguments) == 1 {
				// x = err(expr) → tag=2, copy expr to data
				storeTag(2)
				arg := v.Arguments[0]
				if _, isStr := arg.(*parser.StringLiteral); isStr {
					srcPtr := g.generateExprWithSB(sb, arg)
					copyStrToData(srcPtr)
				} else if argIdent, isIdent := arg.(*parser.Identifier); isIdent {
					if t, ok := g.varTypes[argIdent.Value]; ok && t == "%str" {
						copyStrToData("%" + argIdent.Value)
					} else {
						val := g.generateExprWithSB(sb, arg)
						copyI64ToData(val)
					}
				} else {
					val := g.generateExprWithSB(sb, arg)
					copyI64ToData(val)
				}
				return
			}
		}
		// Fallback: unknown call, treat as implicit val
		storeTag(0)
		val := g.generateExprWithSB(sb, v)
		copyI64ToData(val)

	case *parser.Identifier:
		// Copy %option struct from source variable
		if t, ok := g.varTypes[v.Value]; ok && t == "%option" {
			g.tmpIdx++
			copyReg := fmt.Sprintf("%%opt.copy.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = load %%option, %%option* %%%s\n", g.indent(), copyReg, v.Value))
			sb.WriteString(fmt.Sprintf("%sstore %%option %s, %%option* %%%s\n", g.indent(), copyReg, name))
		}

	default:
		// Implicit value: x = 'test' or x = 42 → tag=0, copy value
		storeTag(0)
		if _, isStr := stmt.Value.(*parser.StringLiteral); isStr {
			srcPtr := g.generateExprWithSB(sb, stmt.Value)
			copyStrToData(srcPtr)
		} else {
			val := g.generateExprWithSB(sb, stmt.Value)
			val = g.stripLLVMType(val)
			copyI64ToData(val)
		}
	}
}

func (g *Generator) generateEnumDefinition(sb *strings.Builder, ed *parser.EnumDefinition) {
	for _, v := range ed.Values {
		g.tmpIdx++
		sb.WriteString(fmt.Sprintf("%s@%s = constant i64 %d\n", g.indent(), v.Name, v.Value))
	}
}
