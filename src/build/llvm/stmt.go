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
	if g.paramNames == nil {
		g.paramNames = make(map[string]bool)
	}
	for _, p := range fd.Parameters {
		g.paramNames[p.Name] = true
		g.varTypes[p.Name] = g.mapToLLVMType(p.Type)
	}

	returnType := "void"
	if len(fd.Results) > 0 {
		returnType = g.mapToLLVMType(fd.Results[0].Type)
	}

	sb.WriteString(fmt.Sprintf("define %s @%s(", returnType, fd.Name))

	for i, param := range fd.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		// 引用傳遞：參數為指標 i64* %n
		llvmType := g.mapToLLVMType(param.Type) + "*"
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
			llvmType := g.mapToLLVMType(r.Type)
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
		llvmType := g.mapToLLVMType(param.Type)
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
	if stmt.IsOption {
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
	if stmt.ArraySize > 0 {
		elemType := "i64"
		if stmt.ElemType != "" {
			elemType = g.mapToLLVMType(stmt.ElemType)
		}
		return fmt.Sprintf("[%d x %s]", stmt.ArraySize, elemType)
	}
	if stmt.IsSlice {
		return "{ i64*, i64 }"
	}
	switch v := stmt.Value.(type) {
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
	default:
		return "i64"
	}
}

func (g *Generator) collectRangeVarTypes(stmt parser.Statement, vars map[string]string) {
	switch s := stmt.(type) {
	case *parser.ForStatement:
		if s.Variable != "" {
			if _, ok := vars[s.Variable]; !ok {
				vars[s.Variable] = "i64"
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
			t := g.varLLVMType(s)
			vars[s.Name.Value] = t
			g.varTypes[s.Name.Value] = t // register immediately for later varLLVMType calls
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
		vars[s.Name.Value] = g.varLLVMType(s)
	case *parser.ForStatement:
		if s.Init != nil {
			g.collectVarDeclsFromStmt(s.Init, vars)
		}
		if s.Variable != "" {
			if _, ok := vars[s.Variable]; !ok {
				vars[s.Variable] = "i64"
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
			if f.Type != "" {
				elemType = g.mapToLLVMType(f.Type)
			}
			llvmType = fmt.Sprintf("[%d x %s]", f.ArraySize, elemType)
		} else if f.IsSlice {
			elemType := "i64"
			if f.Type != "" {
				elemType = g.mapToLLVMType(f.Type)
			}
			llvmType = fmt.Sprintf("{ %s*, i64 }", elemType)
		} else if f.Type != "" {
			llvmType = g.mapToLLVMType(f.Type)
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
	if stmt.Range != nil {
		g.generateRangeFor(sb, stmt)
		return
	}

	// Push loop exit target
	g.tmpIdx++
	labelId := g.tmpIdx
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
	varName := stmt.Variable
	str := stmt.RangeStr
	g.tmpIdx++
	lbl := g.tmpIdx

	// 建立字串常數
	idx := g.stringIdx
	g.stringIdx++
	escaped := g.escapeLLVMString(str)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\\00\"", idx, len(str)+1, escaped))

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

func (g *Generator) generateRangeFor(sb *strings.Builder, stmt *parser.ForStatement) {
	// 字串遍歷: for i in 'hello'
	if stmt.RangeStr != "" {
		g.generateStringRange(sb, stmt)
		return
	}

	r := stmt.Range
	varName := stmt.Variable
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

	// 切片儲存（在 generateExprWithSB 之前，避免重複生成元素）
	if stmt.IsSlice {
		if slice, ok := stmt.Value.(*parser.SliceLiteral); ok {
			elemType := "i64"
			if stmt.ElemType != "" {
				elemType = g.mapToLLVMType(stmt.ElemType)
			}
			n := len(slice.Elements)
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

			// bitcast array pointer to i64* (matches { i64*, i64 } struct)
			g.tmpIdx++
			ptrReg := fmt.Sprintf("%%slice.ptr.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = bitcast %s* %s to i64*\n", g.indent(), ptrReg, arrType, tmpArr))

			// build { i64*, i64 } struct via insertvalue
			g.tmpIdx++
			initReg := fmt.Sprintf("%%slice.init.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue { i64*, i64 } undef, i64* %s, 0\n",
				g.indent(), initReg, ptrReg))

			g.tmpIdx++
			initReg2 := fmt.Sprintf("%%slice.init2.%d", g.tmpIdx)
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue { i64*, i64 } %s, i64 %d, 1\n",
				g.indent(), initReg2, initReg, int64(n)))

			// store to the alloca'd variable
			sb.WriteString(fmt.Sprintf("%sstore { i64*, i64 } %s, { i64*, i64 }* %%%s\n",
				g.indent(), initReg2, name))
			return
		}

		// Non-literal slice assignment — not yet supported
		sb.WriteString(fmt.Sprintf("%s; slice non-literal store not yet implemented\n", g.indent()))
		return
	}

	// Option type assignment: handle nil, val(), err(), and implicit values
	llvmTypeCheck := g.varLLVMType(stmt)
	if llvmTypeCheck == "%option" {
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

	// 陣列儲存: [3 x i64] [i64 1, i64 2, i64 3]
	if stmt.ArraySize > 0 {
		// 預期 val 為 "[i64 1, i64 2, i64 3]"
		sb.WriteString(fmt.Sprintf("%sstore %s %s, %s* %%%s\n", g.indent(), llvmType, val, llvmType, name))
		return
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
	case "i8*":
		sb.WriteString(fmt.Sprintf("%sstore i8* %s, i8** %%%s\n", g.indent(), val, name))
	case "double":
		sb.WriteString(fmt.Sprintf("%sstore double %s, double* %%%s\n", g.indent(), val, name))
	default:
		sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %%%s\n", g.indent(), val, name))
	}
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
