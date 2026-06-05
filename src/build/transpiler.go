package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/build/llvm"
	"github.com/lizongying/nolang/build/no"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// mangleOverloads 對同名函數進行名稱修飾，並更新調用點
func mangleOverloads(program *parser.Program, varTypes map[string]string) {
	// 1. 構建重載表
	overloads := make(map[string][]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			overloads[fd.Name] = append(overloads[fd.Name], fd)
		}
	}

	// 2. 對需要修飾的函數生成新名稱
	mangled := make(map[string]string) // 原始調用簽名 → 修飾後名稱
	for name, fns := range overloads {
		if len(fns) <= 1 {
			continue // 無重載，不改名
		}
		for _, fd := range fns {
			parts := []string{name}
			for _, p := range fd.Parameters {
				parts = append(parts, p.Type)
			}
			mangledName := strings.Join(parts, "_")
			fd.Name = mangledName // 直接修改 AST
			sig := callSignature(name, fd.Parameters)
			mangled[sig] = mangledName
		}
	}

	if len(mangled) == 0 {
		return // 沒有重載，無需遍歷
	}

	// 3. 遍歷所有語句，更新 CallExpression 的函數名
	var walk func(stmts []parser.Statement)
	walk = func(stmts []parser.Statement) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *parser.ExpressionStatement:
				updateCallNames(s.Expression, overloads, mangled, varTypes)
			case *parser.LetStatement:
				if s.Value != nil {
					updateCallNames(s.Value, overloads, mangled, varTypes)
				}
			case *parser.FunctionDefinition:
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.BlockStatement:
				walk(s.Statements)
			case *parser.ForStatement:
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.ReturnStatement:
				if s.ReturnValue != nil {
					updateCallNames(s.ReturnValue, overloads, mangled, varTypes)
				}
			}
		}
	}
	walk(program.Statements)

	// 也用於回退查找（無參數類型匹配時的前端保底）
	_ = varTypes
}

// callSignature 生成調用簽名 key，用於查找
func callSignature(name string, params []*parser.Parameter) string {
	parts := []string{name}
	for _, p := range params {
		parts = append(parts, p.Type)
	}
	return strings.Join(parts, "_")
}

// inferExprType 推斷表達式的類型字串
func inferExprType(expr parser.Expression, varTypes map[string]string) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.CharLiteral:
		return "char"
	case *parser.Identifier:
		if t, ok := varTypes[e.Value]; ok {
			return t
		}
		return "" // 未知變數
	case *parser.CallExpression:
		// 函數調用的返回類型 — 目前只能返回 i64
		return "i64"
	case *parser.InfixExpression:
		// 簡單推斷：比較運算返回 bool，算術返回 i64/f64
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=":
			return "bool"
		case "+", "-", "*", "/":
			// 根據類型推斷
			leftType := inferExprType(e.Left, varTypes)
			if leftType != "" {
				return leftType
			}
			return "i64"
		default:
			return "i64"
		}
	case *parser.PrefixExpression:
		if e.Operator == "!" {
			return "bool"
		}
		return "i64"
	case *parser.DotExpression:
		return "i64"
	default:
		return "i64"
	}
}

// updateCallNames 遞迴更新 CallExpression 中的函數名
func updateCallNames(expr parser.Expression, overloads map[string][]*parser.FunctionDefinition,
	mangled map[string]string, varTypes map[string]string) {

	switch e := expr.(type) {
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			name := ident.Value
			if fns, has := overloads[name]; has && len(fns) > 1 {
				// 收集實參類型
				argTypes := make([]string, len(e.Arguments))
				for i, arg := range e.Arguments {
					t := inferExprType(arg, varTypes)
					if t == "" {
						// 無法推斷類型，使用第一個重載
						if i < len(fns[0].Parameters) {
							t = fns[0].Parameters[i].Type
						} else {
							t = "i64"
						}
					}
					argTypes[i] = t
				}
				// 查找匹配的重載
				sig := name + "_" + strings.Join(argTypes, "_")
				if mangledName, ok := mangled[sig]; ok {
					ident.Value = mangledName
				} else {
					// 找不到精確匹配，嘗試最接近的重載（取第一個）
					if len(fns) > 0 {
						ident.Value = fns[0].Name
					}
				}
			}
		}
		// 遞迴處理參數中的嵌套調用
		for _, arg := range e.Arguments {
			updateCallNames(arg, overloads, mangled, varTypes)
		}

	case *parser.InfixExpression:
		updateCallNames(e.Left, overloads, mangled, varTypes)
		updateCallNames(e.Right, overloads, mangled, varTypes)

	case *parser.PrefixExpression:
		updateCallNames(e.Right, overloads, mangled, varTypes)

	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, s := range e.Consequence.Statements {
				updateCallNamesInStmt(s, overloads, mangled, varTypes)
			}
		}
		if e.Alternative != nil {
			for _, s := range e.Alternative.Statements {
				updateCallNamesInStmt(s, overloads, mangled, varTypes)
			}
		}
	}
}

func updateCallNamesInStmt(stmt parser.Statement, overloads map[string][]*parser.FunctionDefinition,
	mangled map[string]string, varTypes map[string]string) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		updateCallNames(s.Expression, overloads, mangled, varTypes)
	case *parser.LetStatement:
		if s.Value != nil {
			updateCallNames(s.Value, overloads, mangled, varTypes)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			updateCallNames(s.ReturnValue, overloads, mangled, varTypes)
		}
	}
}

type Transpiler struct {
	llvmGenerator *llvm.Generator
	noGenerator   *no.Generator
	pkg           *Package // 當前套件（用於路徑解析）
}

func NewTranspiler() *Transpiler {
	return &Transpiler{
		llvmGenerator: llvm.NewGenerator(),
		noGenerator:   no.NewGenerator(),
	}
}

type Target int

const (
	TargetUnknown Target = iota
	TargetLLVM
	TargetNo
)

func (t *Transpiler) parseFile(filePath string) (*parser.Program, error) {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	l := lexer.New(string(source))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("%s: %v", filePath, p.Errors())
	}
	return prog, nil
}

func (t *Transpiler) resolveUse(use *parser.UseStatement) (*parser.Program, error) {
	// use path.fn → 載入 path.no 並取出 fn 函數
	path := use.Path

	// std/ 開頭 → 標準庫路徑
	if strings.HasPrefix(path, "std/") || path == "std" {
		// 相對於語言根目錄的 src/std/
		// 使用硬編碼路徑或透過套件 alias
		if t.pkg != nil {
			// 嘗試透過 alias 解析（nolang.jsonc 中的 @std）
			resolved := t.pkg.ResolvePath(path)
			if !strings.HasSuffix(resolved, ".no") {
				resolved = resolved + ".no"
			}
			if _, err := os.Stat(resolved); err == nil {
				return t.parseFile(resolved)
			}
		}
		// fallback: src/std/ 相對於執行目錄
		fallback := path + ".no"
		if _, err := os.Stat(fallback); err == nil {
			return t.parseFile(fallback)
		}
		// 再試 src/std/<module>.no
		srcPath := "src/" + path + ".no"
		return t.parseFile(srcPath)
	}

	// 非 std 路徑 → 透過 alias 解析
	if t.pkg != nil {
		modulePath := t.pkg.ResolvePath(path)
		if !strings.HasSuffix(modulePath, ".no") {
			modulePath = modulePath + ".no"
		}
		return t.parseFile(modulePath)
	}

	// 沒有套件配置，直接嘗試
	filePath := path + ".no"
	return t.parseFile(filePath)
}

func (t *Transpiler) Compile(source string) (string, error) {
	return t.CompileTarget(source, TargetLLVM)
}

func (t *Transpiler) CompileTarget(source string, target Target) (string, error) {
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", fmt.Errorf("parser errors: %v", p.Errors())
	}

	// 驗證：僅標準庫能使用的功能
	isUserCode := true
	if t.pkg != nil {
		root := t.pkg.RootDir
		if strings.Contains(root, "src/std") || strings.Contains(root, "std") {
			isUserCode = false
		}
	}
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && isUserCode {
			// ..any 僅標準庫可用
			if fd.IsVariadic {
				for _, p := range fd.Parameters {
					if p.Type == "[]any" {
						return "", fmt.Errorf("..any is only allowed in standard library, not in user code (function: %s)", fd.Name)
					}
				}
			}
			// 結果參數 fn(params)(results) 僅標準庫可用
			if len(fd.Results) > 0 {
				return "", fmt.Errorf("result parameters fn()() are only allowed in standard library, not in user code (function: %s)", fd.Name)
			}
		}
	}

	// 構建變數類型表
	varTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.Value
			}
		}
	}

	// 名稱修飾 pass：處理方法重載
	mangleOverloads(program, varTypes)

	// 自動 enter/leave：插入作用域生命週期調用
	injectEnterLeave(program)

	// 處理 use 陳述句：載入模組並合併函數定義
	merged := &parser.Program{Statements: []parser.Statement{}}
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			modProg, err := t.resolveUse(use)
			if err != nil {
				return "", fmt.Errorf("loading module %s: %w", use.Path, err)
			}
			// 將模組中的 FunctionDefinition 加入 merged
			for _, ms := range modProg.Statements {
				if _, ok := ms.(*parser.FunctionDefinition); ok {
					merged.Statements = append(merged.Statements, ms)
				}
			}
			continue
		}
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			merged.Statements = append(merged.Statements, stmt)
		}
	}

	// 泛型單態化：掃描泛型函數呼叫，生成具體版本
	monomorphizeGenerics(merged)

	// 非函數定義的陳述句（頂層呼叫）放到最後
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			continue
		}
		if _, ok := stmt.(*parser.UseStatement); ok {
			continue
		}
		merged.Statements = append(merged.Statements, stmt)
	}

	switch target {
	case TargetLLVM:
		return t.llvmGenerator.Generate(merged), nil
	case TargetNo:
		return t.noGenerator.Generate(merged), nil
	default:
		return "", fmt.Errorf("unknown target: %v", target)
	}
}

// monomorphizeGenerics 對泛型函數進行單態化
// 掃描所有 CallExpression.GenericArgs 或可推斷的呼叫，
// 找到對應的泛型 FunctionDefinition，為每個具體化組合產生具體版本
func monomorphizeGenerics(program *parser.Program) {
	// 收集所有泛型函數定義
	genericFns := make(map[string]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.GenericParams) > 0 {
				genericFns[fd.Name] = fd
			}
		}
	}

	if len(genericFns) == 0 {
		return
	}

	// 掃描所有陳述句尋找泛型呼叫
	var newStmts []parser.Statement
	for _, stmt := range program.Statements {
		// 尋找 CallExpression
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			if ce, ok := es.Expression.(*parser.CallExpression); ok {
				if fnName, ok := ce.Function.(*parser.Identifier); ok {
					if fd, exists := genericFns[fnName.Value]; exists {
						// 嘗試從引數型別推斷泛型參數
						genericArgs := ce.GenericArgs
						if len(genericArgs) == 0 {
							genericArgs = inferGenericArgs(fd, ce, program)
						}
						if len(genericArgs) > 0 {
							// 建立具體化版本
							concrete := cloneAndSubstitute(fd, genericArgs)
							newStmts = append(newStmts, concrete)
							// 更新呼叫名稱
							fnName.Value = concrete.Name
							ce.GenericArgs = nil
						}
					}
				}
			}
		}
	}

	program.Statements = append(program.Statements, newStmts...)
}

// inferGenericArgs 從函數呼叫的引數型別推斷泛型參數
// 例如 fn(arr [n]t) 被以 [8]byte 引數呼叫 → n=8, t=byte
func inferGenericArgs(fd *parser.FunctionDefinition, call *parser.CallExpression, program *parser.Program) []parser.Expression {
	if len(fd.Parameters) == 0 || len(call.Arguments) == 0 {
		return nil
	}

	var args []parser.Expression

	for pi, param := range fd.Parameters {
		if pi >= len(call.Arguments) {
			break
		}
		arg := call.Arguments[pi]
		argType := inferArgType(arg, program)

		// 匹配泛型型別：t 與具體型別 i64
		if len(param.Type) == 1 && param.Type[0] >= 'a' && param.Type[0] <= 'z' {
			if isLowerLetter(param.Type) && argType != "" {
				args = append(args, &parser.StringLiteral{Value: argType})
			}
		}

		// 匹配參數型別 [n]t 與引數型別 [8]byte
		if len(param.Type) > 3 && param.Type[0] == '[' {
			closeBracket := strings.IndexByte(param.Type, ']')
			if closeBracket > 0 && closeBracket+1 < len(param.Type) {
				sizeParam := param.Type[1:closeBracket]    // n
				elemParam := param.Type[closeBracket+1:]   // t

				// 從引數型別中提取具體值
				if len(argType) > 2 && argType[0] == '[' {
					argClose := strings.IndexByte(argType, ']')
					if argClose > 0 {
						argSize := argType[1:argClose]       // 8
						argElem := argType[argClose+1:]      // byte

						if isLowerLetter(sizeParam) {
							if val, err := strconv.ParseInt(argSize, 10, 64); err == nil {
								args = append(args, &parser.IntegerLiteral{Value: val})
							}
						}
						if isLowerLetter(elemParam) {
							// 型別引數目前用字串表示
							args = append(args, &parser.StringLiteral{Value: argElem})
						}
					}
				}
			}
		}
	}
	return args
}

func isLowerLetter(s string) bool {
	return len(s) == 1 && s[0] >= 'a' && s[0] <= 'z'
}

func inferArgType(expr parser.Expression, program *parser.Program) string {
	switch e := expr.(type) {
	case *parser.Identifier:
		for _, stmt := range program.Statements {
			if ls, ok := stmt.(*parser.LetStatement); ok {
				if ls.Name != nil && ls.Name.Value == e.Value && ls.Type != nil {
					return ls.Type.Value
				}
			}
		}
	case *parser.IntegerLiteral:
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.BooleanLiteral:
		return "bool"
	}
	return ""
}

// cloneAndSubstitute 複製泛型函數並以具體值替換泛型參數
func cloneAndSubstitute(fd *parser.FunctionDefinition, genericArgs []parser.Expression) *parser.FunctionDefinition {
	if len(genericArgs) == 0 {
		return fd
	}

	// 建立名稱修飾：arr_to_vec → arr_to_vec.8.byte
	mangledName := fd.Name
	for _, arg := range genericArgs {
		if lit, ok := arg.(*parser.IntegerLiteral); ok {
			mangledName += fmt.Sprintf(".%d", lit.Value)
		} else if lit, ok := arg.(*parser.StringLiteral); ok {
			mangledName += "." + lit.Value
		}
	}

	// 複製並替換參數類型中的泛型標記
	subst := make(map[string]string) // 泛型參數名 → 具體值字串
	for i, gp := range fd.GenericParams {
		if i < len(genericArgs) {
			if lit, ok := genericArgs[i].(*parser.IntegerLiteral); ok {
				subst[gp] = fmt.Sprintf("%d", lit.Value)
			} else if lit, ok := genericArgs[i].(*parser.StringLiteral); ok {
				subst[gp] = lit.Value
			}
		}
	}

	// 複製參數
	newParams := make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		newParams[i] = &parser.Parameter{
			Token: p.Token,
			Name:  p.Name,
			Type:  substituteType(p.Type, subst),
		}
	}

	// 複製回傳值
	newResults := make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		newResults[i] = &parser.Parameter{
			Token: r.Token,
			Name:  r.Name,
			Type:  substituteType(r.Type, subst),
		}
	}

	// 複製並替換函數體
	newBody := substituteBody(fd.Body, subst)

	return &parser.FunctionDefinition{
		Token:         fd.Token,
		Name:          mangledName,
		GenericParams: nil, // 具體化後無泛型參數
		Parameters:    newParams,
		Results:       newResults,
		Body:          newBody,
	}
}

// substituteBody 遞迴替換函數體中的泛型參數
func substituteBody(body *parser.BlockStatement, subst map[string]string) *parser.BlockStatement {
	if body == nil || len(subst) == 0 {
		return body
	}
	newStmts := make([]parser.Statement, len(body.Statements))
	for i, stmt := range body.Statements {
		newStmts[i] = substituteStmt(stmt, subst)
	}
	return &parser.BlockStatement{
		Token:      body.Token,
		Statements: newStmts,
	}
}

func substituteStmt(stmt parser.Statement, subst map[string]string) parser.Statement {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		return &parser.ExpressionStatement{
			Token:      s.Token,
			Expression: substituteExpr(s.Expression, subst),
		}
	case *parser.LetStatement:
		return &parser.LetStatement{
			Token: s.Token,
			Name:  s.Name,
			Value: substituteExpr(s.Value, subst),
			Type:  s.Type,
		}
	case *parser.ForStatement:
		newFor := &parser.ForStatement{
			Token:    s.Token,
			Variable: s.Variable,
			Range:    substituteRange(s.Range, subst),
			Body:     substituteBody(s.Body, subst),
			Label:    s.Label,
		}
		// 也替換 for i < n 條件中的 n
		if s.Condition != nil {
			newFor.Condition = substituteExpr(s.Condition, subst)
		}
		return newFor
	case *parser.BlockStatement:
		return substituteBody(s, subst)
	default:
		return stmt
	}
}

func substituteExpr(expr parser.Expression, subst map[string]string) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		if val, ok := subst[e.Value]; ok {
			// 將泛型參數替換為具體整數值
			intVal, _ := strconv.ParseInt(val, 10, 64)
			return &parser.IntegerLiteral{
				Token: e.Token,
				Value: intVal,
			}
		}
		return e
	case *parser.IntegerLiteral:
		return e
	case *parser.InfixExpression:
		return &parser.InfixExpression{
			Token:    e.Token,
			Left:     substituteExpr(e.Left, subst),
			Operator: e.Operator,
			Right:    substituteExpr(e.Right, subst),
		}
	case *parser.PrefixExpression:
		return &parser.PrefixExpression{
			Token:    e.Token,
			Operator: e.Operator,
			Right:    substituteExpr(e.Right, subst),
		}
	case *parser.CallExpression:
		newCe := &parser.CallExpression{
			Token:     e.Token,
			Function:  substituteExpr(e.Function, subst),
			Arguments: make([]parser.Expression, len(e.Arguments)),
		}
		for i, arg := range e.Arguments {
			newCe.Arguments[i] = substituteExpr(arg, subst)
		}
		return newCe
	case *parser.IndexExpression:
		return &parser.IndexExpression{
			Token: e.Token,
			Left:  substituteExpr(e.Left, subst),
			Index: substituteExpr(e.Index, subst),
		}
	default:
		return e
	}
}

func substituteRange(r *parser.RangeExpression, subst map[string]string) *parser.RangeExpression {
	if r == nil {
		return nil
	}
	return &parser.RangeExpression{
		Token:    r.Token,
		LeftInc:  r.LeftInc,
		RightInc: r.RightInc,
		Start:    substituteExpr(r.Start, subst),
		End:      substituteExpr(r.End, subst),
	}
}

// substituteType 替換類型字串中的泛型參數
// 例如 "[n]t" + n=8,t=byte → "[8]byte"
func substituteType(typeStr string, subst map[string]string) string {
	if len(subst) == 0 {
		return typeStr
	}
	result := typeStr
	for k, v := range subst {
		// 替換 [N] → [v]（陣列大小）
		old := "[" + k + "]"
		new := "[" + v + "]"
		result = strings.ReplaceAll(result, old, new)
		// 替換元素型別：型別字串結尾的單字母
		// 例如 "[]t" → "[]byte" 或 "[8]t" → "[8]byte"
		if strings.HasSuffix(result, k) {
			idx := len(result) - len(k)
			// 確保 k 是獨立的（前面是 ] 或前面沒有字元）
			if idx == 0 || result[idx-1] == ']' {
				result = result[:idx] + v
			}
		}
	}
	return result
}

// makeIdent 建立 Identifier AST 節點
func makeIdent(name string) *parser.Identifier {
	return &parser.Identifier{
		Token: lexer.Token{Type: lexer.IDENT, Literal: name},
		Value: name,
	}
}

// makeMethodCall 建立 varName.methodName() 的 ExpressionStatement
func makeMethodCall(varName, method string) *parser.ExpressionStatement {
	return &parser.ExpressionStatement{
		Token: lexer.Token{Type: lexer.IDENT, Literal: varName},
		Expression: &parser.CallExpression{
			Token: lexer.Token{Type: lexer.LPAREN, Literal: "("},
			Function: &parser.DotExpression{
				Token:    lexer.Token{Type: lexer.DOT, Literal: "."},
				Receiver: makeIdent(varName),
				Property: method,
			},
			Arguments: []parser.Expression{},
		},
	}
}

// injectEnterLeave 為實現了 enter()/leave() 的類型自動插入作用域調用
func injectEnterLeave(program *parser.Program) {
	// 1. 收集實現了 enter/leave 的類型
	hasEnter := make(map[string]bool)
	hasLeave := make(map[string]bool)

	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		// 方法名格式：TypeName.methodName
		dotIdx := -1
		for i := len(fd.Name) - 1; i >= 0; i-- {
			if fd.Name[i] == '.' {
				dotIdx = i
				break
			}
		}
		if dotIdx < 0 {
			continue
		}
		typeName := fd.Name[:dotIdx]
		methodName := fd.Name[dotIdx+1:]

		if methodName == "enter" {
			hasEnter[typeName] = true
		} else if methodName == "leave" {
			hasLeave[typeName] = true
		}
	}

	if len(hasEnter) == 0 && len(hasLeave) == 0 {
		return // 沒有類型需要處理
	}

	// 找出既有 enter 又有 leave 的類型
	lifecycleTypes := make(map[string]bool)
	for t := range hasEnter {
		lifecycleTypes[t] = true
	}
	for t := range hasLeave {
		lifecycleTypes[t] = true
	}

	// 2. 遍歷所有函數體，注入 enter/leave
	var walkBlock func(block *parser.BlockStatement, inScope []string)
	walkBlock = func(block *parser.BlockStatement, inScope []string) {
		var newStmts []parser.Statement
		scopeVars := make([]string, len(inScope))
		copy(scopeVars, inScope)

		for _, stmt := range block.Statements {
			newStmts = append(newStmts, stmt)

			switch s := stmt.(type) {
			case *parser.LetStatement:
				typeName := ""
				if s.Type != nil {
					typeName = s.Type.Value
				}
				if lifecycleTypes[typeName] {
					varName := s.Name.Value
					// 插入 varName.enter()
					newStmts = append(newStmts, makeMethodCall(varName, "enter"))
					scopeVars = append(scopeVars, varName)
				}

			case *parser.ReturnStatement:
				// 在 return 前插入 leave()
				for i := len(scopeVars) - 1; i >= 0; i-- {
					if hasLeave[findTypeForVar(scopeVars[i], block, lifecycleTypes)] {
						newStmts = append(newStmts, makeMethodCall(scopeVars[i], "leave"))
					}
				}

			case *parser.ForStatement:
				if s.Body != nil {
					walkBlock(s.Body, scopeVars)
				}

			case *parser.ExpressionStatement:
				if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
					if ifExpr.Consequence != nil {
						walkBlock(ifExpr.Consequence, scopeVars)
					}
					if ifExpr.Alternative != nil {
						walkBlock(ifExpr.Alternative, scopeVars)
					}
				}
			}

		}

		// 區塊結尾插入 leave()（反向）
		if len(scopeVars) > len(inScope) {
			for i := len(scopeVars) - 1; i >= len(inScope); i-- {
				if hasLeave[findTypeForVar(scopeVars[i], block, lifecycleTypes)] {
					newStmts = append(newStmts, makeMethodCall(scopeVars[i], "leave"))
				}
			}
		}

		block.Statements = newStmts
	}

	// 遍歷頂層函數和區塊
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if s.Body != nil {
				walkBlock(s.Body, nil)
			}
		}
	}
}

// findTypeForVar 從區塊語句中查找變數的類型（簡化版）
func findTypeForVar(varName string, block *parser.BlockStatement, lifecycleTypes map[string]bool) string {
	for _, stmt := range block.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name.Value == varName {
			if ls.Type != nil {
				return ls.Type.Value
			}
		}
	}
	// 默認返回空
	for t := range lifecycleTypes {
		return t
	}
	return ""
}
