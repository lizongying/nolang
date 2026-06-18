package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/build/llvm"
	"github.com/lizongying/nolang/builtin"
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
				parts = append(parts, p.Type.String())
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
		parts = append(parts, p.Type.String())
	}
	return strings.Join(parts, "_")
}

// isConcreteType 檢查型別名稱是否為已知具體型別
func isConcreteType(typeName string) bool {
	switch typeName {
	case "i64", "f64", "str", "bool", "char", "byte", "void":
		return true
	}
	// 複合型別：切片、陣列、可空、指針
	if strings.HasPrefix(typeName, "[]") || strings.HasPrefix(typeName, "[") ||
		strings.HasPrefix(typeName, "?") || strings.HasPrefix(typeName, "ptr ") {
		return true
	}
	return false
}

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
		// 函數調用的返回類型 — 查詢 builtin 回傳型別
		if ident, ok := e.Function.(*parser.Identifier); ok {
			for _, m := range builtin.BuiltinMethodList {
				if m.MethodName == ident.Value {
					if len(m.Return) > 0 {
						return m.Return[0].String()
					}
				}
			}
		}
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
		// 前綴正負號傳遞內層表達式的型別
		return inferExprType(e.Right, varTypes)
	case *parser.DotExpression:
		return "i64"
	case *parser.GroupedExpression:
		return inferExprType(e.Expression, varTypes)
	case *parser.ConditionalExpression:
		// 三元運算子：從兩分支推斷型別
		consequenceType := inferExprType(e.Consequence, varTypes)
		alternativeType := inferExprType(e.Alternative, varTypes)
		if consequenceType == alternativeType && consequenceType != "" {
			return consequenceType
		}
		if consequenceType != "" {
			return consequenceType
		}
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
							t = fns[0].Parameters[i].Type.String()
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
	case *parser.GroupedExpression:
		updateCallNames(e.Expression, overloads, mangled, varTypes)
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
	pkg           *Package // 當前套件（用於路徑解析）
}

func NewTranspiler(pkg *Package) *Transpiler {
	return &Transpiler{
		llvmGenerator: llvm.NewGenerator(),
		pkg:           pkg,
	}
}

type Target int

const (
	TargetUnknown Target = iota
	TargetLLVM
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

	// 本地模塊：/path → 相對於專案根目錄
	if strings.HasPrefix(path, "/") {
		relPath := strings.TrimPrefix(path, "/")
		if t.pkg != nil {
			fullPath := filepath.Join(t.pkg.RootDir, relPath) + ".no"
			return t.parseFile(fullPath)
		}
		// 沒有套件配置，相對於當前目錄
		filePath := relPath + ".no"
		return t.parseFile(filePath)
	}

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
		// fallback: std/<module>.no 相對於執行目錄
		fallback := path + ".no"
		if _, err := os.Stat(fallback); err == nil {
			return t.parseFile(fallback)
		}
		// 再試 src/std/<module>.no 相對於執行目錄
		srcPath := "src/" + path + ".no"
		if _, err := os.Stat(srcPath); err == nil {
			return t.parseFile(srcPath)
		}
		// 最後嘗試相對於 binary 所在路徑
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			binSrcPath := filepath.Join(exeDir, "..", "src", path+".no")
			if _, err := os.Stat(binSrcPath); err == nil {
				return t.parseFile(binSrcPath)
			}
		}
		return t.parseFile(srcPath)
	}

	// 依賴解析：domain/org/repo/... 風格的導入路徑
	if first := strings.SplitN(path, "/", 2)[0]; strings.Contains(first, ".") {
		if t.pkg != nil && len(t.pkg.Dependencies) > 0 {
			if _, _, matched := t.pkg.matchDependency(path); matched {
				modPath, err := t.pkg.ResolveDependencyModule(path)
				if err != nil {
					return nil, err
				}
				if modPath != "" {
					return t.parseFile(modPath)
				}
			}
		}
		// URL 風格的導入路徑但未在 dependencies 中宣告
		return nil, fmt.Errorf("dependency not found: %q is not declared in nolang.jsonc dependencies", path)
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

func (t *Transpiler) CompileTarget(source string, _ Target) (string, error) {
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
					if p.Type.String() == "[]any" {
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
				varTypes[ls.Name.Value] = ls.Type.String()
			}
		}
		// Also collect variable types from function bodies
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			collectVarTypesFromBody(fd.Body, varTypes)
		}
	}

	// 編譯期陣列邊界檢查
	arraySizes := buildArraySizeMap(program)
	sliceSizes := buildSliceSizeMap(program)
	stringSizes := buildStringSizeMap(program)
	if err := validateArrayBounds(program, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
		return "", err
	}

	// 編譯期重複變數檢查
	if err := validateDuplicates(program); err != nil {
		return "", err
	}

	// 型別檢查
	if typeErrs := ValidateTypes(program); len(typeErrs) > 0 {
		var msgs []string
		for _, e := range typeErrs {
			msgs = append(msgs, fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message))
		}
		return "", fmt.Errorf("type errors: %s", strings.Join(msgs, "; "))
	}

	// 名稱修飾 pass：處理方法重載
	mangleOverloads(program, varTypes)

	// 自動 enter/leave：插入作用域生命週期調用
	injectEnterLeave(program)

	// 收集導入的模塊短名稱，用於後續的 module.fn() → fn() 重寫
	var importedModules []string
	// 預填充已知 std 模塊短名稱，允許 math.degrees() 等呼叫無需顯式導入
	// 如果變量名與模塊名衝突，跳過自動添加以避免歧義
	for _, name := range knownStdModuleNames() {
		if _, isVar := varTypes[name]; !isVar {
			importedModules = append(importedModules, name)
		}
	}
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			importedModules = append(importedModules, moduleShortName(use.Path))
		}
	}

	// 收集主程序變量名，避免與合併的模塊定義衝突
	mainVarNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			mainVarNames[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			mainVarNames[fd.Name] = true
		}
	}

	// 處理 use 陳述句：載入模組並合併函數定義和常量
	merged := &parser.Program{Statements: []parser.Statement{}}
	// 記錄已顯式導入的模組路徑，避免重複載入
	explicitStdModules := make(map[string]bool)
	moduleConstants := make(map[string]parser.Expression)
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			modProg, err := t.resolveUse(use)
			if err != nil {
				return "", fmt.Errorf("loading module %s: %w", use.Path, err)
			}
			if strings.HasPrefix(use.Path, "std/") || use.Path == "std" {
				explicitStdModules[use.Path] = true
			}
			// 將模組中的 FunctionDefinition 和 LetStatement（常量）加入 merged
			for _, ms := range modProg.Statements {
				if fd, ok := ms.(*parser.FunctionDefinition); ok {
					// If alias is specified, only import the specific function under the alias name
					if use.Alias != "" {
						if use.Function != "" && fd.Name == use.Function {
							fd.Name = use.Alias
							merged.Statements = append(merged.Statements, fd)
						}
						// Skip other functions when alias is used
					} else {
						merged.Statements = append(merged.Statements, fd)
					}
				}
				if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
					// If alias is specified, only import the specific function under the alias name
					if use.Alias != "" {
						if use.Function != "" && ls.Name.Value == use.Function {
							if _, ok := ls.Value.(*parser.FunctionLiteral); ok {
								ls.Name.Value = use.Alias
							}
							if !mainVarNames[ls.Name.Value] {
								merged.Statements = append(merged.Statements, ls)
								if isConstantExpr(ls.Value) {
									moduleConstants[ls.Name.Value] = ls.Value
								}
							}
						}
						// Skip other lets when alias is used
					} else {
						// 如果主程序已有同名變量，跳過以避免衝突
						if !mainVarNames[ls.Name.Value] {
							merged.Statements = append(merged.Statements, ls)
							if isConstantExpr(ls.Value) {
								moduleConstants[ls.Name.Value] = ls.Value
							}
						}
					}
				}
			}
			continue
		}
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			merged.Statements = append(merged.Statements, stmt)
		}
	}

	// 自動載入已知 std 模組（允許無需顯式導入的 module.fn() 呼叫）
	for _, name := range knownStdModuleNames() {
		// 如果變量名與模塊名衝突，跳過自動載入
		if _, isVar := varTypes[name]; isVar {
			continue
		}
		path := "std/" + name
		if explicitStdModules[path] {
			continue
		}
		use := &parser.UseStatement{Path: path, Function: ""}
		modProg, err := t.resolveUse(use)
		if err != nil {
			return "", fmt.Errorf("auto-loading module %s: %w", path, err)
		}
		for _, ms := range modProg.Statements {
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				merged.Statements = append(merged.Statements, fd)
			}
			if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				// 如果主程序已有同名變量，跳過以避免衝突
				if !mainVarNames[ls.Name.Value] {
					merged.Statements = append(merged.Statements, ls)
					if isConstantExpr(ls.Value) {
						moduleConstants[ls.Name.Value] = ls.Value
					}
				}
			}
		}
	}

	// 常量傳播：將模組常量替換為字面值，使 module functions 可以直接使用常量
	resolveModuleConstants(merged, moduleConstants)

	// 解析 module.fn() 呼叫：將 DotExpression 重寫為 Identifier
	// 必須在 monomorphizeGenerics 之前執行，以便泛型模組函數也能被正確處理
	resolveModuleCalls(merged, importedModules)

	// 泛型單態化：掃描泛型函數呼叫，生成具體版本
	monomorphizeGenerics(merged, varTypes)

	// 過濾：移除尚未具現化的泛型函數定義（只有具體版本才能產生 LLVM IR）
	filtered := make([]parser.Statement, 0, len(merged.Statements))
	for _, stmt := range merged.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.GenericParams) > 0 {
				continue // 跳過泛型函數（GenericParams 未被清空說明尚未具現化）
			}
		}
		filtered = append(filtered, stmt)
	}
	merged.Statements = filtered

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

	// 再次解析 module.fn() 呼叫：處理剛剛添加的頂層代碼
	resolveModuleCalls(merged, importedModules)

	return t.llvmGenerator.Generate(merged), nil
}

// monomorphizeGenerics 對泛型函數進行單態化
func monomorphizeGenerics(program *parser.Program, varTypes map[string]string) {
	// 收集所有泛型函數定義
	genericFns := make(map[string]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.GenericParams) > 0 {
				genericFns[fd.Name] = fd
			} else if isGenericMethod(fd.Name) {
				// Method definitions like [n]t.fill have implicit generic params
				genericFns[fd.Name] = fd
			}
		}
	}

	if len(genericFns) == 0 {
		return
	}

	// 遞迴掃描所有陳述句尋找泛型呼叫（包括函數體內）
	var newStmts []parser.Statement
	for _, stmt := range program.Statements {
		scanStmtForGenericCalls(stmt, genericFns, varTypes, program, &newStmts)
	}

	program.Statements = append(program.Statements, newStmts...)
}

// isGenericMethod checks if a function name like "[n]t.method" has generic type params
func isGenericMethod(name string) bool {
	if len(name) > 3 && name[0] == '[' {
		closeB := strings.IndexByte(name, ']')
		if closeB > 0 && closeB+1 < len(name) {
			sizeParam := name[1:closeB]
			elemParam := name[closeB+1:]
			// Check for "." separator
			dotIdx := strings.IndexByte(elemParam, '.')
			if dotIdx > 0 {
				elem := elemParam[:dotIdx]
				return (isLowerLetter(sizeParam) || sizeParam == "") && isLowerLetter(elem)
			}
		}
	}
	if strings.HasPrefix(name, "[].") {
		return false // [].method - no generics
	}
	if len(name) > 2 && name[0] == '[' && name[1] == ']' {
		dotIdx := strings.IndexByte(name, '.')
		if dotIdx > 2 {
			elem := name[2:dotIdx]
			return isLowerLetter(elem)
		}
	}
	return false
}

// scanStmtForGenericCalls recursively scans statements for generic calls
func scanStmtForGenericCalls(stmt parser.Statement, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement) {

	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if ce, ok := s.Expression.(*parser.CallExpression); ok {
			processCallExpression(ce, genericFns, varTypes, program, newStmts)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts)
			}
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			scanStmtForGenericCalls(bodyStmt, genericFns, varTypes, program, newStmts)
		}
	}
}

// processCallExpression handles a single CallExpression for generic resolution
func processCallExpression(ce *parser.CallExpression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement) {

	// Regular function call: fn(args)
	if fnName, ok := ce.Function.(*parser.Identifier); ok {
		if fd, exists := genericFns[fnName.Value]; exists {
			genericArgs := ce.GenericArgs
			if len(genericArgs) == 0 {
				genericArgs = inferGenericArgs(fd, ce, program)
			}
			if len(genericArgs) > 0 {
				concrete := cloneAndSubstitute(fd, genericArgs)
				*newStmts = append(*newStmts, concrete)
				fnName.Value = concrete.Name
				ce.GenericArgs = nil
			}
		}
	}

	// Method call: receiver.method(args)
	if dot, ok := ce.Function.(*parser.DotExpression); ok {
		resolveMethodCall(dot, ce, genericFns, varTypes, newStmts)
	}

	// Recurse into arguments
	for _, arg := range ce.Arguments {
		if innerCe, ok := arg.(*parser.CallExpression); ok {
			processCallExpression(innerCe, genericFns, varTypes, program, newStmts)
		}
	}
}

// resolveMethodCall resolves a DotExpression-based method call.
// Returns true if the call was resolved and rewritten.
func resolveMethodCall(dot *parser.DotExpression, ce *parser.CallExpression,
	genericFns map[string]*parser.FunctionDefinition, varTypes map[string]string,
	newStmts *[]parser.Statement) bool {

	// Get receiver variable name and type
	recvIdent, ok := dot.Receiver.(*parser.Identifier)
	if !ok {
		return false
	}
	recvType, ok := varTypes[recvIdent.Value]
	if !ok {
		return false
	}

	methodName := dot.Property

	// Search for matching generic method
	for name, fd := range genericFns {
		dotIdx := strings.LastIndex(name, ".")
		if dotIdx < 0 {
			continue
		}
		typePrefix := name[:dotIdx]
		methodSuffix := name[dotIdx+1:]
		if methodSuffix != methodName {
			continue
		}

		// Try to match typePrefix (e.g., "[n]t") against recvType (e.g., "[4]i64")
		genericArgs := matchTypePattern(typePrefix, recvType, fd)
		if len(genericArgs) == 0 {
			continue
		}

		// Create concrete version
		concrete := cloneAndSubstitute(fd, genericArgs)
		*newStmts = append(*newStmts, concrete)

		// Rewrite call: replace DotExpression with Identifier, prepend receiver
		ce.Function = &parser.Identifier{
			Token: lexer.Token{Type: lexer.IDENT, Literal: concrete.Name},
			Value: concrete.Name,
		}
		// Prepend receiver as first argument
		receiverArg := &parser.Identifier{
			Token: recvIdent.Token,
			Value: recvIdent.Value,
		}
		ce.Arguments = append([]parser.Expression{receiverArg}, ce.Arguments...)
		return true
	}

	// Try non-generic method: type.method already exists
	// Rewrite to direct call with receiver prepended
	concreteName := recvType + "." + methodName
	ce.Function = &parser.Identifier{
		Token: lexer.Token{Type: lexer.IDENT, Literal: concreteName},
		Value: concreteName,
	}
	receiverArg := &parser.Identifier{
		Token: recvIdent.Token,
		Value: recvIdent.Value,
	}
	ce.Arguments = append([]parser.Expression{receiverArg}, ce.Arguments...)
	return true
}

// matchTypePattern matches a type pattern like "[n]t" against a concrete type like "[4]i64".
// Returns generic args (e.g., n=4, t=i64) or nil if no match.
func matchTypePattern(pattern, concrete string, fd *parser.FunctionDefinition) []parser.Expression {
	// Match [n]t against [4]i64
	if len(pattern) > 3 && pattern[0] == '[' {
		closeBracket := strings.IndexByte(pattern, ']')
		if closeBracket > 0 && closeBracket+1 < len(pattern) {
			sizeParam := pattern[1:closeBracket]
			elemParam := pattern[closeBracket+1:]

			if len(concrete) > 2 && concrete[0] == '[' {
				argClose := strings.IndexByte(concrete, ']')
				if argClose > 0 {
					argSize := concrete[1:argClose]
					argElem := concrete[argClose+1:]

					var args []parser.Expression
					if isLowerLetter(sizeParam) {
						if val, err := strconv.ParseInt(argSize, 10, 64); err == nil {
							args = append(args, &parser.IntegerLiteral{Value: val})
						}
					}
					if isLowerLetter(elemParam) {
						args = append(args, &parser.StringLiteral{Value: argElem})
					}
					if len(args) > 0 {
						return args
					}
				}
			}
		}
	}

	// Match []t against []i64 (slice pattern)
	if strings.HasPrefix(pattern, "[]") {
		elemParam := pattern[2:]
		if strings.HasPrefix(concrete, "[]") {
			argElem := concrete[2:]
			if isLowerLetter(elemParam) {
				return []parser.Expression{&parser.StringLiteral{Value: argElem}}
			}
		}
	}

	return nil
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
		paramType := param.Type.String()

		// 匹配泛型型別：t 與具體型別 i64
		if len(paramType) == 1 && paramType[0] >= 'a' && paramType[0] <= 'z' {
			if isLowerLetter(paramType) && argType != "" {
				args = append(args, &parser.StringLiteral{Value: argType})
			}
		}

		// 匹配參數型別 [n]t 與引數型別 [8]byte
		if len(paramType) > 3 && paramType[0] == '[' {
			closeBracket := strings.IndexByte(paramType, ']')
			if closeBracket > 0 && closeBracket+1 < len(paramType) {
				sizeParam := paramType[1:closeBracket]  // n
				elemParam := paramType[closeBracket+1:] // t

				// 從引數型別中提取具體值
				if len(argType) > 2 && argType[0] == '[' {
					argClose := strings.IndexByte(argType, ']')
					if argClose > 0 {
						argSize := argType[1:argClose]  // 8
						argElem := argType[argClose+1:] // byte

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
					return ls.Type.String()
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
	case *parser.GroupedExpression:
		return inferArgType(e.Expression, program)
	}
	return ""
}

// cloneAndSubstitute 複製泛型函數並以具體值替換泛型參數
func cloneAndSubstitute(fd *parser.FunctionDefinition, genericArgs []parser.Expression) *parser.FunctionDefinition {
	if len(genericArgs) == 0 {
		return fd
	}

	// 複製並替換參數類型中的泛型標記
	subst := make(map[string]string) // 泛型參數名 → 具體值字串

	// For explicit generic params (positional matching)
	// Skip for implicit generic methods like [n]t.method - use name-based matching below
	isImplicitGenericMethod := len(fd.Name) > 3 && fd.Name[0] == '['
	if !isImplicitGenericMethod {
		for i, gp := range fd.GenericParams {
			if i < len(genericArgs) {
				if lit, ok := genericArgs[i].(*parser.IntegerLiteral); ok {
					subst[gp.Value] = fmt.Sprintf("%d", lit.Value)
				} else if lit, ok := genericArgs[i].(*parser.StringLiteral); ok {
					subst[gp.Value] = lit.Value
				}
			}
		}
	}

	// For implicit generic methods like [n]t.method:
	// Extract size/elem param names from the method name and match by type (not position)
	var sizeVal string
	var elemVal string
	for _, arg := range genericArgs {
		if lit, ok := arg.(*parser.IntegerLiteral); ok {
			sizeVal = fmt.Sprintf("%d", lit.Value)
		} else if lit, ok := arg.(*parser.StringLiteral); ok {
			elemVal = lit.Value
		}
	}

	if isImplicitGenericMethod {
		closeB := strings.IndexByte(fd.Name, ']')
		if closeB > 0 && closeB+1 < len(fd.Name) {
			sizeParam := fd.Name[1:closeB]
			elemPart := fd.Name[closeB+1:]
			dotIdx := strings.IndexByte(elemPart, '.')
			var elemParam string
			if dotIdx > 0 {
				elemParam = elemPart[:dotIdx]
			}
			// Add to subst if not already set by positional matching
			if isLowerLetter(sizeParam) && sizeVal != "" {
				if _, exists := subst[sizeParam]; !exists {
					subst[sizeParam] = sizeVal
				}
			}
			if isLowerLetter(elemParam) && elemVal != "" {
				if _, exists := subst[elemParam]; !exists {
					subst[elemParam] = elemVal
				}
			}
		}
	}

	// Build mangled name
	mangledName := fd.Name
	if isImplicitGenericMethod {
		// Replace generic type prefix with LLVM-safe name: [n]t.fill → _4xi64.fill
		closeB := strings.IndexByte(mangledName, ']')
		dotIdx := strings.IndexByte(mangledName, '.')
		if closeB > 0 && dotIdx > closeB {
			sizeParam := mangledName[1:closeB]
			elemParam := mangledName[closeB+1 : dotIdx]
			_ = sizeParam // used implicitly via isLowerLetter check below
			_ = elemParam
			if isLowerLetter(string(mangledName[1])) && isLowerLetter(string(mangledName[closeB+1])) {
				mangledName = "_" + sizeVal + "x" + elemVal + mangledName[dotIdx:]
			}
		}
	} else {
		// Regular generic function: append args to name
		for _, arg := range genericArgs {
			if lit, ok := arg.(*parser.IntegerLiteral); ok {
				mangledName += fmt.Sprintf(".%d", lit.Value)
			} else if lit, ok := arg.(*parser.StringLiteral); ok {
				mangledName += "." + lit.Value
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
		Token: fd.Token,
		Name:  mangledName,
		FuncSignature: parser.FuncSignature{
			GenericParams: nil, // 具體化後無泛型參數
			Parameters:    newParams,
			Results:       newResults,
		},
		Body: newBody,
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
			Token: s.Token,
			Body:  substituteBody(s.Body, subst),
			Label: s.Label,
		}
		if s.IterRange != nil {
			newFor.IterRange = &parser.IterationExpr{
				Variable:  s.IterRange.Variable,
				Range:     substituteRange(s.IterRange.Range, subst),
				RangeStr:  s.IterRange.RangeStr,
				RangeExpr: s.IterRange.RangeExpr,
			}
			// Also copy RangeExpr (identifier/slice) - it may contain generic types too
			if ident, ok := s.IterRange.RangeExpr.(*parser.Identifier); ok {
				if val, ok2 := subst[ident.Value]; ok2 {
					newFor.IterRange.RangeExpr = &parser.Identifier{Token: ident.Token, Value: val}
				}
			}
		}

		// 也替換 for i < n 條件中的 n
		if s.Condition != nil {
			newFor.Condition = substituteExpr(s.Condition, subst)
		}
		if s.CountExpr != nil {
			newFor.CountExpr = substituteExpr(s.CountExpr, subst)
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
	case *parser.GroupedExpression:
		return &parser.GroupedExpression{
			Token:      e.Token,
			Expression: substituteExpr(e.Expression, subst),
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

// substituteType 替換類型中的泛型參數
// 遞迴處理所有 Type 節點
func substituteType(t parser.Type, subst map[string]string) parser.Type {
	if len(subst) == 0 || t == nil {
		return t
	}
	switch typ := t.(type) {
	case *parser.NamedType:
		if val, ok := subst[typ.Value]; ok {
			return &parser.NamedType{Token: typ.Token, Value: val}
		}
		return typ
	case *parser.ArrayType:
		newSize := typ.Size
		if ident, ok := typ.Size.(*parser.Identifier); ok {
			if val, ok := subst[ident.Value]; ok {
				if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
					newSize = &parser.IntegerLiteral{Token: ident.Token, Value: intVal}
				}
			}
		}
		newElem := substituteType(typ.Elem, subst)
		return &parser.ArrayType{Token: typ.Token, Size: newSize, Elem: newElem}
	case *parser.SliceType:
		newElem := substituteType(typ.Elem, subst)
		return &parser.SliceType{Token: typ.Token, Elem: newElem}
	case *parser.NullableType:
		newInner := substituteType(typ.Type, subst)
		return &parser.NullableType{Token: typ.Token, Type: newInner}
	case *parser.PointerType:
		newInner := substituteType(typ.Type, subst)
		return &parser.PointerType{Token: typ.Token, Type: newInner}
	default:
		return t
	}
}

// collectVarTypesFromBody recursively collects variable types from a function body
func collectVarTypesFromBody(body *parser.BlockStatement, varTypes map[string]string) {
	if body == nil {
		return
	}
	for _, stmt := range body.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
			}
		}
		if bs, ok := stmt.(*parser.BlockStatement); ok {
			collectVarTypesFromBody(bs, varTypes)
		}
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.Body != nil {
				collectVarTypesFromBody(fs.Body, varTypes)
			}
		}
	}
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
					typeName = s.Type.String()
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
				return ls.Type.String()
			}
		}
	}
	// 默認返回空
	for t := range lifecycleTypes {
		return t
	}
	return ""
}

// buildArraySizeMap 構建變數名 → 陣列大小的映射
// 從所有 LetStatement 中收集 ArraySize
func buildArraySizeMap(program *parser.Program) map[string]int64 {
	sizes := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectArraySizesFromStmt(stmt, sizes)
	}
	return sizes
}

func collectArraySizesFromStmt(stmt parser.Statement, sizes map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if at, ok := s.Type.(*parser.ArrayType); ok {
			var arraySize int64
			if at.Size != nil {
				if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
					arraySize = intLit.Value
				}
			} else if arrLit, ok := s.Value.(*parser.ArrayLiteral); ok {
				if intLit, ok := arrLit.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
					arraySize = intLit.Value
				}
			}
			if arraySize > 0 {
				sizes[s.Name.Value] = arraySize
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectArraySizesFromStmt(ss, sizes)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectArraySizesFromStmt(s.Init, sizes)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectArraySizesFromStmt(ss, sizes)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectArraySizesFromStmt(ss, sizes)
		}
	}
}

// buildSliceSizeMap collects names of slice variables and their initial element count
func buildSliceSizeMap(program *parser.Program) map[string]int64 {
	slices := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectSliceSizeMapFromStmt(stmt, slices)
	}
	return slices
}

func collectSliceSizeMapFromStmt(stmt parser.Statement, slices map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if _, ok := s.Type.(*parser.SliceType); ok {
			if sl, ok := s.Value.(*parser.SliceLiteral); ok {
				slices[s.Name.Value] = int64(len(sl.Elements))
			} else {
				slices[s.Name.Value] = 0 // unknown size
			}
		} else if sl, ok := s.Value.(*parser.SliceLiteral); ok {
			// Also detect slice from SliceLiteral value (inferred type, no [] annotation)
			slices[s.Name.Value] = int64(len(sl.Elements))
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSliceSizeMapFromStmt(ss, slices)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectSliceSizeMapFromStmt(s.Init, slices)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectSliceSizeMapFromStmt(ss, slices)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectSliceSizeMapFromStmt(ss, slices)
		}
	}
}

// buildStringSizeMap collects names of string variables and their literal length
func buildStringSizeMap(program *parser.Program) map[string]int64 {
	strSizes := make(map[string]int64)
	for _, stmt := range program.Statements {
		collectStringSizeMapFromStmt(stmt, strSizes)
	}
	return strSizes
}

func collectStringSizeMapFromStmt(stmt parser.Statement, strSizes map[string]int64) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Type != nil && (s.Type.String() == "str" || s.Type.String() == "str-smail") {
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		} else if sl, ok := s.Value.(*parser.StringLiteral); ok {
			// Also detect string from StringLiteral value (inferred type)
			strSizes[s.Name.Value] = int64(len(sl.Value))
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectStringSizeMapFromStmt(s.Init, strSizes)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			collectStringSizeMapFromStmt(ss, strSizes)
		}
	}
}

// validateArrayBounds 編譯期陣列邊界檢查
// 檢查所有 IndexExpression 中的常數索引是否超出陣列長度
// isStringExpr checks if an expression is a string type
func isStringExpr(expr parser.Expression, stringSizes map[string]int64) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.Identifier:
		_, exists := stringSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExpr(e.Expression, stringSizes)
	}
	return false
}

// validateDuplicates checks for duplicate variable declarations
func validateDuplicates(program *parser.Program) error {
	seen := make(map[string]bool)
	for _, stmt := range program.Statements {
		if err := validateStmtDuplicates(stmt, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateStmtDuplicates(stmt parser.Statement, seen map[string]bool) error {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// Only type-annotated declarations count as "definitions" (e.g., a i8)
		// The parser sets s.Type to the variable name for untyped assignments (a = 2),
		// so we check if Type.Value differs from Name.Value to detect real type annotations
		if s.Type == nil || s.Type.String() == s.Name.Value {
			return nil
		}
		if seen[s.Name.Value] {
			return fmt.Errorf("duplicate variable '%s'", s.Name.Value)
		}
		seen[s.Name.Value] = true
	case *parser.FunctionDefinition:
		if s.Body != nil {
			bodySeen := make(map[string]bool)
			for _, bStmt := range s.Body.Statements {
				if err := validateStmtDuplicates(bStmt, bodySeen); err != nil {
					return err
				}
			}
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			if err := validateStmtDuplicates(bStmt, seen); err != nil {
				return err
			}
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				if err := validateStmtDuplicates(bStmt, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateArrayBounds 編譯期陣列邊界檢查
// 檢查所有 IndexExpression 中的常數索引是否超出陣列長度
func validateArrayBounds(program *parser.Program, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	for _, stmt := range program.Statements {
		if err := validateStmtArrayBounds(stmt, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
			return err
		}
	}
	return nil
}

func validateStmtArrayBounds(stmt parser.Statement, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		return validateExprArrayBounds(s.Expression, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.LetStatement:
		if s.Value != nil {
			// Check type mismatch for string variables
			if _, exists := stringSizes[s.Name.Value]; exists {
				if !isStringExpr(s.Value, stringSizes) {
					return fmt.Errorf("cannot assign non-string value to string variable '%s'", s.Name.Value)
				}
			}
			return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
	case *parser.ForStatement:
		if s.Init != nil {
			if err := validateStmtArrayBounds(s.Init, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return validateExprArrayBounds(s.ReturnValue, arraySizes, sliceSizes, stringSizes, varTypes)
		}
	}
	return nil
}

func validateExprArrayBounds(expr parser.Expression, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	switch e := expr.(type) {
	case *parser.IndexExpression:
		// 檢查索引是否為常數且超出陣列長度
		if ident, ok := e.Left.(*parser.Identifier); ok {
			if size, exists := arraySizes[ident.Value]; exists && size > 0 {
				if lit, ok := e.Index.(*parser.IntegerLiteral); ok {
					if lit.Value >= size {
						return fmt.Errorf("index %d out of bounds for array '%s' of size %d", lit.Value, ident.Value, size)
					}
				}
			}
			// Also check slice bounds
			if size, exists := sliceSizes[ident.Value]; exists && size > 0 {
				if lit, ok := e.Index.(*parser.IntegerLiteral); ok {
					if lit.Value >= size {
						return fmt.Errorf("index %d out of bounds for slice '%s' of length %d", lit.Value, ident.Value, size)
					}
				}
			}
			// Also check string index bounds
			if size, exists := stringSizes[ident.Value]; exists && size > 0 {
				if lit, ok := e.Index.(*parser.IntegerLiteral); ok {
					if lit.Value >= size {
						return fmt.Errorf("index %d out of bounds for string '%s' of length %d", lit.Value, ident.Value, size)
					}
				}
			}
		}
		// 遞迴檢查 Left 和 Index（Index 自身也可能有巢狀索引）
		if err := validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
			return err
		}
		return validateExprArrayBounds(e.Index, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.AssignExpression:
		// array.len = val / slice.len = val / string.len = val → 不允許修改唯獨的 len 欄位
		if dot, ok := e.Left.(*parser.DotExpression); ok {
			if dot.Property == "len" {
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					if _, exists := arraySizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of array '%s'", ident.Value)
					}
					if _, exists := sliceSizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of slice '%s'", ident.Value)
					}
					if _, exists := stringSizes[ident.Value]; exists {
						return fmt.Errorf("cannot modify read-only field 'len' of string '%s'", ident.Value)
					}
				}
			}
		}
		// a = val type mismatch check
		if ident, ok := e.Left.(*parser.Identifier); ok {
			if _, exists := stringSizes[ident.Value]; exists {
				if !isStringExpr(e.Value, stringSizes) {
					return fmt.Errorf("cannot assign non-string value to string variable '%s'", ident.Value)
				}
			}
		}
		// a[i] = val → 檢查 Left 中的 IndexExpression
		// （slice 的索引檢查已在 IndexExpression case 中處理）
		return validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.InfixExpression:
		if err := validateExprArrayBounds(e.Left, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
			return err
		}
		return validateExprArrayBounds(e.Right, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.PrefixExpression:
		return validateExprArrayBounds(e.Right, arraySizes, sliceSizes, stringSizes, varTypes)
	case *parser.CallExpression:
		// array.len() / slice.len() / string.len() → 沒有 len() 方法
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if dot.Property == "len" {
				if ident, ok := dot.Receiver.(*parser.Identifier); ok {
					if _, exists := arraySizes[ident.Value]; exists {
						return fmt.Errorf("array '%s' has no method 'len', use '%s.len' instead", ident.Value, ident.Value)
					}
					if _, exists := sliceSizes[ident.Value]; exists {
						return fmt.Errorf("slice '%s' has no method 'len', use '%s.len' instead", ident.Value, ident.Value)
					}
					if _, exists := stringSizes[ident.Value]; exists {
						return fmt.Errorf("string '%s' has no method 'len', use '%s.len' instead", ident.Value, ident.Value)
					}
					// For any other typed variable, also reject .len() method
					if typeName, exists := varTypes[ident.Value]; exists {
						return fmt.Errorf("%s '%s' has no method 'len', use '%s.len' instead", typeName, ident.Value, ident.Value)
					}
				}
			}
		}
		if e.Function != nil {
			if err := validateExprArrayBounds(e.Function, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
		for _, arg := range e.Arguments {
			if err := validateExprArrayBounds(arg, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			if err := validateExprArrayBounds(elem, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			if err := validateExprArrayBounds(elem, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			if err := validateExprArrayBounds(e.Condition, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
				return err
			}
		}
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, stringSizes, varTypes); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ── 型別檢查 ──────────────────────────────────────────────

// ValidateResult 型別檢查結果
type ValidateResult struct {
	Line      int
	Column    int
	EndColumn int
	Message   string
}

// ValidateTypes 對 Program 進行型別檢查，回傳錯誤列表（包含行號）
func ValidateTypes(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// 1. 收集所有函式名稱
	funcNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			funcNames[fd.Name] = true
		}
	}

	// 2. 檢查重複函式簽名（允許重載，但簽名不能重複）
	sigSeen := make(map[string]int) // signature → first seen line
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			var paramTypes []string
			for _, p := range fd.Parameters {
				paramTypes = append(paramTypes, p.Type.String())
			}
			sig := fd.Name + "(" + strings.Join(paramTypes, ", ") + ")"
			if firstLine, exists := sigSeen[sig]; exists {
				results = append(results, ValidateResult{
					Line:    fd.Token.Line,
					Column:  fd.Token.Column,
					Message: fmt.Sprintf("duplicate function definition '%s' (first defined at line %d)", sig, firstLine),
				})
			} else {
				sigSeen[sig] = fd.Token.Line
			}
		}
	}

	// 3. 遍歷頂層語句做型別檢查
	for _, stmt := range program.Statements {
		errs := validateStmtTypes(stmt, funcNames, make(map[string]string))
		results = append(results, errs...)
	}

	return results
}

// isValidVarName 檢查名稱是否只包含小寫字母（a-z）、中連接符（-）和數字，且不能以數字開頭
func isValidVarName(name string) bool {
	if name == "" {
		return true
	}
	for i, ch := range name {
		if i == 0 {
			// 不能以數字開頭
			if ch >= '0' && ch <= '9' {
				return false
			}
		}
		if ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

// ValidateNaming 檢查所有變數/函數名稱是否符合命名規範（只用小寫和中劃線）
func ValidateNaming(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		results = append(results, checkNaming(stmt)...)
	}
	return results
}

func checkNaming(stmt parser.Statement) []ValidateResult {
	var results []ValidateResult
	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		// For methods like "[]t.sort-desc", only validate the method name part (after the last '.')
		nameToCheck := s.Name
		if lastDot := strings.LastIndex(s.Name, "."); lastDot >= 0 {
			nameToCheck = s.Name[lastDot+1:]
		}
		if !isValidVarName(nameToCheck) {
			results = append(results, ValidateResult{
				Line:    s.Token.Line,
				Column:  s.Token.Column,
				Message: fmt.Sprintf("'%s' should use only lowercase letters and hyphens", s.Name),
			})
		}
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				results = append(results, checkNaming(bStmt)...)
			}
		}
	case *parser.LetStatement:
		if s.Name != nil && !isValidVarName(s.Name.Value) {
			results = append(results, ValidateResult{
				Line:    s.Name.Token.Line,
				Column:  s.Name.Token.Column,
				Message: fmt.Sprintf("'%s' should use only lowercase letters and hyphens", s.Name.Value),
			})
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			results = append(results, checkNaming(bStmt)...)
		}
	case *parser.ExpressionStatement:
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				results = append(results, checkNaming(ifExpr.Consequence)...)
			}
			if ifExpr.Alternative != nil {
				results = append(results, checkNaming(ifExpr.Alternative)...)
			}
		}
	}
	return results
}

// ValidateUnusedVars detects top-level variables that are defined but never used.
func ValidateUnusedVars(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// Collect top-level LetStatement names
	topLevelVars := make(map[string]struct{ line, column int })
	var varOrder []string

	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Name != nil && ls.Name.Value != "_" {
				topLevelVars[ls.Name.Value] = struct{ line, column int }{
					line:   ls.Name.Token.Line,
					column: ls.Name.Token.Column,
				}
				varOrder = append(varOrder, ls.Name.Value)
			}
		}
	}

	if len(topLevelVars) == 0 {
		return nil
	}

	// Walk entire AST to find references
	usedVars := make(map[string]bool)
	for _, stmt := range program.Statements {
		markReferencesInStatement(stmt, topLevelVars, usedVars)
	}

	// Report unused top-level variables
	for _, name := range varOrder {
		if !usedVars[name] {
			def := topLevelVars[name]
			results = append(results, ValidateResult{
				Line:      def.line,
				Column:    def.column,
				EndColumn: def.column + len(name) - 1,
				Message:   fmt.Sprintf("'%s' is defined but never used", name),
			})
		}
	}

	return results
}

// markReferencesInStatement walks a statement tree, finding Identifier references to top-level vars.
func markReferencesInStatement(stmt parser.Statement, varSet map[string]struct{ line, column int }, usedVars map[string]bool) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// Don't count the variable name itself as a usage
		if s.Value != nil {
			markReferencesInExpr(s.Value, varSet, usedVars)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			markReferencesInExpr(s.Expression, varSet, usedVars)
		}

	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, inner := range s.Body.Statements {
				markReferencesInStatement(inner, varSet, usedVars)
			}
		}

	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			markReferencesInExpr(s.ReturnValue, varSet, usedVars)
		}

	case *parser.BlockStatement:
		for _, inner := range s.Statements {
			markReferencesInStatement(inner, varSet, usedVars)
		}

	case *parser.ForStatement:
		if s.Init != nil {
			markReferencesInStatement(s.Init, varSet, usedVars)
		}
		if s.Condition != nil {
			markReferencesInExpr(s.Condition, varSet, usedVars)
		}
		if s.Update != nil {
			markReferencesInStatement(s.Update, varSet, usedVars)
		}
		if s.Body != nil {
			for _, inner := range s.Body.Statements {
				markReferencesInStatement(inner, varSet, usedVars)
			}
		}
	}
}

// markReferencesInExpr walks an expression tree, marking Identifiers found in varSet as used.
func markReferencesInExpr(expr parser.Expression, varSet map[string]struct{ line, column int }, usedVars map[string]bool) {
	switch e := expr.(type) {
	case *parser.Identifier:
		if _, exists := varSet[e.Value]; exists {
			usedVars[e.Value] = true
		}

	case *parser.InfixExpression:
		if e.Left != nil {
			markReferencesInExpr(e.Left, varSet, usedVars)
		}
		if e.Right != nil {
			markReferencesInExpr(e.Right, varSet, usedVars)
		}

	case *parser.PrefixExpression:
		if e.Right != nil {
			markReferencesInExpr(e.Right, varSet, usedVars)
		}

	case *parser.CallExpression:
		if e.Function != nil {
			markReferencesInExpr(e.Function, varSet, usedVars)
		}
		for _, arg := range e.Arguments {
			markReferencesInExpr(arg, varSet, usedVars)
		}

	case *parser.DotExpression:
		if e.Receiver != nil {
			markReferencesInExpr(e.Receiver, varSet, usedVars)
		}

	case *parser.GroupedExpression:
		if e.Expression != nil {
			markReferencesInExpr(e.Expression, varSet, usedVars)
		}

	case *parser.IfExpression:
		if e.Condition != nil {
			markReferencesInExpr(e.Condition, varSet, usedVars)
		}
		if e.Consequence != nil {
			for _, inner := range e.Consequence.Statements {
				markReferencesInStatement(inner, varSet, usedVars)
			}
		}
		if e.Alternative != nil {
			for _, inner := range e.Alternative.Statements {
				markReferencesInStatement(inner, varSet, usedVars)
			}
		}

	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			markReferencesInExpr(elem, varSet, usedVars)
		}

	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			markReferencesInExpr(elem, varSet, usedVars)
		}

	case *parser.IndexExpression:
		if e.Left != nil {
			markReferencesInExpr(e.Left, varSet, usedVars)
		}
		if e.Index != nil {
			markReferencesInExpr(e.Index, varSet, usedVars)
		}

	case *parser.AssignExpression:
		if e.Left != nil {
			markReferencesInExpr(e.Left, varSet, usedVars)
		}
		if e.Value != nil {
			markReferencesInExpr(e.Value, varSet, usedVars)
		}

	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, inner := range e.Body.Statements {
				markReferencesInStatement(inner, varSet, usedVars)
			}
		}

	case *parser.SliceExpression:
		if e.Left != nil {
			markReferencesInExpr(e.Left, varSet, usedVars)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				markReferencesInExpr(e.Range.Start, varSet, usedVars)
			}
			if e.Range.End != nil {
				markReferencesInExpr(e.Range.End, varSet, usedVars)
			}
		}

	case *parser.ConditionalExpression:
		if e.Condition != nil {
			markReferencesInExpr(e.Condition, varSet, usedVars)
		}
		if e.Consequence != nil {
			markReferencesInExpr(e.Consequence, varSet, usedVars)
		}
		if e.Alternative != nil {
			markReferencesInExpr(e.Alternative, varSet, usedVars)
		}

	case *parser.StructLiteral:
		for _, f := range e.Fields {
			if f.Value != nil {
				markReferencesInExpr(f.Value, varSet, usedVars)
			}
		}
	}
}

// ValidateUndefinedVars detects references to variables that are not defined.
func ValidateUndefinedVars(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// 1. Collect all defined names
	definedVars := make(map[string]bool) // name → true
	funcNames := make(map[string]bool)   // function names

	// Top-level LetStatements and FunctionDefinitions
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			definedVars[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			definedVars[fd.Name] = true
			funcNames[fd.Name] = true
		}
	}

	// 2. Collect module names (from #use + auto-imported known std modules)
	//    and also parse those module files for exported constants/functions.
	moduleNames := collectModuleNames(program)
	for _, m := range moduleNames {
		definedVars[m] = true
	}
	exportedNames := collectModuleExports(program, moduleNames)
	for _, n := range exportedNames {
		definedVars[n] = true
	}

	// 3. Add explicitly imported function names from UseStatements
	//    (e.g., # /src/utils.greet → defines greet();
	//     # /src/utils.greet as myGreet → defines myGreet())
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok && use.Function != "" {
			if use.Alias != "" {
				definedVars[use.Alias] = true
			} else {
				definedVars[use.Function] = true
			}
		}
	}

	// 4. Walk statements and check for undefined references
	for _, stmt := range program.Statements {
		results = append(results, checkUndefinedVarsInStmt(stmt, definedVars, funcNames)...)
	}

	return results
}

// ValidateUseKeyword warns when "use" keyword is used instead of "#".
func ValidateUseKeyword(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		if us, ok := stmt.(*parser.UseStatement); ok && us.Token.Literal == "use" {
			results = append(results, ValidateResult{
				Line:    us.Token.Line,
				Column:  us.Token.Column,
				Message: "'use' keyword is deprecated, use '#' instead (e.g., '# " + us.Path + "')",
			})
		}
	}
	return results
}

// ValidateUseAlias warns when 'as' keyword is used for import aliasing and suggests direct alias style.
func ValidateUseAlias(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		if us, ok := stmt.(*parser.UseStatement); ok && us.Token.Literal == "#" && us.AsKeyword {
			results = append(results, ValidateResult{
				Line:    us.Token.Line,
				Column:  us.Token.Column,
				Message: fmt.Sprintf("use '# %s.%s %s' instead of '# %s.%s as %s'", us.Path, us.Function, us.Alias, us.Path, us.Function, us.Alias),
			})
		}
	}
	return results
}

// ValidateDependencyImports checks that URL-style import paths (e.g., github.com/...)
// are declared in nolang.jsonc dependencies. rootDir is the directory to search upward
// from for the project's nolang.jsonc.
func ValidateDependencyImports(program *parser.Program, rootDir string) []ValidateResult {
	if rootDir == "" {
		return nil
	}
	pkg, _ := LoadPackage(rootDir)
	if pkg == nil || len(pkg.Dependencies) == 0 {
		return nil
	}

	var results []ValidateResult
	for _, stmt := range program.Statements {
		us, ok := stmt.(*parser.UseStatement)
		if !ok {
			continue
		}
		path := us.Path
		// Check if this is a URL-style path (first segment contains ".")
		first := strings.SplitN(path, "/", 2)[0]
		if !strings.Contains(first, ".") {
			continue
		}
		// Check if declared in dependencies
		if _, _, matched := pkg.matchDependency(path); !matched {
			results = append(results, ValidateResult{
				Line:    us.Token.Line,
				Column:  us.Token.Column,
				Message: fmt.Sprintf("dependency not found: %q is not declared in nolang.jsonc dependencies", path),
			})
		}
	}
	return results
}

// ValidateStringConcat warns when "+" is used with string operands and suggests "-" instead.
func ValidateStringConcat(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		results = append(results, checkStringConcatInStmt(stmt)...)
	}
	return results
}

func checkStringConcatInStmt(stmt parser.Statement) []ValidateResult {
	var results []ValidateResult
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			results = append(results, checkStringConcatInExpr(s.Expression)...)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			results = append(results, checkStringConcatInExpr(s.Value)...)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				results = append(results, checkStringConcatInStmt(bodyStmt)...)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			results = append(results, checkStringConcatInStmt(bodyStmt)...)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			results = append(results, checkStringConcatInExpr(s.ReturnValue)...)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			results = append(results, checkStringConcatInStmt(s.Init)...)
		}
		if s.Condition != nil {
			results = append(results, checkStringConcatInExpr(s.Condition)...)
		}
		if s.Update != nil {
			results = append(results, checkStringConcatInStmt(s.Update)...)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				results = append(results, checkStringConcatInStmt(bodyStmt)...)
			}
		}
	}
	return results
}

func checkStringConcatInExpr(expr parser.Expression) []ValidateResult {
	var results []ValidateResult
	switch e := expr.(type) {
	case *parser.InfixExpression:
		if e.Operator == "+" {
			// Check if either operand is a string literal
			isStrConcat := false
			if _, ok := e.Left.(*parser.StringLiteral); ok {
				isStrConcat = true
			} else if _, ok := e.Right.(*parser.StringLiteral); ok {
				isStrConcat = true
			}
			if isStrConcat {
				results = append(results, ValidateResult{
					Line:    e.Token.Line,
					Column:  e.Token.Column,
					Message: "string concatenation: use '-' instead of '+'",
				})
			}
		}
		// Recurse into sub-expressions
		results = append(results, checkStringConcatInExpr(e.Left)...)
		results = append(results, checkStringConcatInExpr(e.Right)...)
	case *parser.CallExpression:
		for _, arg := range e.Arguments {
			results = append(results, checkStringConcatInExpr(arg)...)
		}
	case *parser.DotExpression:
		results = append(results, checkStringConcatInExpr(e.Receiver)...)
	case *parser.PrefixExpression:
		results = append(results, checkStringConcatInExpr(e.Right)...)
	case *parser.GroupedExpression:
		results = append(results, checkStringConcatInExpr(e.Expression)...)
	case *parser.IndexExpression:
		results = append(results, checkStringConcatInExpr(e.Left)...)
		results = append(results, checkStringConcatInExpr(e.Index)...)
	}
	return results
}

// collectModuleNames returns all known module short names (from #use + auto-imported std modules).
func collectModuleNames(program *parser.Program) []string {
	seen := make(map[string]bool)
	var names []string

	for _, name := range knownStdModuleNames() {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			short := moduleShortName(use.Path)
			if !seen[short] {
				seen[short] = true
				names = append(names, short)
			}
		}
	}

	return names
}

// ModuleExport holds an exported name and its string value from a module file.
type ModuleExport struct {
	Name  string
	Value string
}

// GetModuleExports resolves module .no files and extracts their top-level
// LetStatement names with values (for hover) and function names.
func GetModuleExports(moduleNames []string) []ModuleExport {
	seen := make(map[string]bool)
	var exports []ModuleExport

	for _, m := range moduleNames {
		filePath := resolveModulePath(m)
		if filePath == "" {
			continue
		}

		source, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		l := lexer.New(string(source))
		p := parser.New(l)
		modProg := p.ParseProgram()
		if len(p.Errors()) > 0 {
			continue
		}

		for _, stmt := range modProg.Statements {
			if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
				if seen[ls.Name.Value] {
					continue
				}
				seen[ls.Name.Value] = true
				val := moduleExprValue(ls.Value)
				exports = append(exports, ModuleExport{Name: ls.Name.Value, Value: val})
			}
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if seen[fd.Name] {
					continue
				}
				seen[fd.Name] = true
				exports = append(exports, ModuleExport{Name: fd.Name, Value: ""})
			}
		}
	}

	return exports
}

// moduleExprValue extracts the string representation of a module-level expression value.
func moduleExprValue(expr parser.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.FloatLiteral:
		if e.Raw != "" {
			return e.Raw
		}
		return fmt.Sprintf("%g", e.Value)
	case *parser.StringLiteral:
		return "\"" + e.Value + "\""
	case *parser.BooleanLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *parser.NilLiteral:
		return "nil"
	default:
		return ""
	}
}

// collectModuleExports tries to resolve each module's .no file and extract its
// top-level LetStatement names (constants) and function names.
func collectModuleExports(program *parser.Program, moduleNames []string) []string {
	exports := GetModuleExports(moduleNames)
	var names []string
	for _, e := range exports {
		names = append(names, e.Name)
	}
	return names
}

// resolveModulePath tries to locate a .no file for the given module short name
// (e.g., "math" → "std/math.no"), using the same fallback chain as resolveUse.
func resolveModulePath(moduleName string) string {
	// Try relative to CWD
	candidates := []string{
		"std/" + moduleName + ".no",
		"src/std/" + moduleName + ".no",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	// Try binary-relative paths
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		// Binary could be at bin/nolang or vscode-nolang/server/nolang-lsp
		relativePaths := []string{
			filepath.Join(exeDir, "..", "src", "std", moduleName+".no"),       // bin/ → src/
			filepath.Join(exeDir, "..", "..", "src", "std", moduleName+".no"), // vscode-nolang/server/ → src/
		}
		for _, c := range relativePaths {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}

	return ""
}

func checkUndefinedVarsInStmt(stmt parser.Statement, definedVars, funcNames map[string]bool) []ValidateResult {
	var results []ValidateResult
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			results = append(results, checkUndefinedVarsInExpr(s.Expression, definedVars, funcNames, false)...)
		}
	case *parser.LetStatement:
		// Name is a definition, not a reference — skip it
		if s.Value != nil {
			results = append(results, checkUndefinedVarsInExpr(s.Value, definedVars, funcNames, false)...)
		}
	case *parser.FunctionDefinition:
		// Parameters are defined vars within the function
		localDefs := make(map[string]bool)
		for k, v := range definedVars {
			localDefs[k] = v
		}
		for _, p := range s.Parameters {
			localDefs[p.Name] = true
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				results = append(results, checkUndefinedVarsInStmt(bodyStmt, localDefs, funcNames)...)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			results = append(results, checkUndefinedVarsInStmt(bodyStmt, definedVars, funcNames)...)
		}
	case *parser.ForStatement:
		localDefs := make(map[string]bool)
		for k, v := range definedVars {
			localDefs[k] = v
		}
		if s.IterRange != nil && s.IterRange.Variable != "" {
			localDefs[s.IterRange.Variable] = true
		}
		if s.Init != nil {
			results = append(results, checkUndefinedVarsInStmt(s.Init, localDefs, funcNames)...)
		}
		if s.Condition != nil {
			results = append(results, checkUndefinedVarsInExpr(s.Condition, localDefs, funcNames, false)...)
		}
		if s.Update != nil {
			results = append(results, checkUndefinedVarsInStmt(s.Update, localDefs, funcNames)...)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				results = append(results, checkUndefinedVarsInStmt(bodyStmt, localDefs, funcNames)...)
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			results = append(results, checkUndefinedVarsInExpr(s.ReturnValue, definedVars, funcNames, false)...)
		}
	}
	return results
}

func checkUndefinedVarsInExpr(expr parser.Expression, definedVars, funcNames map[string]bool, isFuncCallArg bool) []ValidateResult {
	var results []ValidateResult
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		// Skip function call names (checked via builtin + funcNames)
		if !definedVars[e.Value] {
			// Check if it's a known function or builtin
			if funcNames[e.Value] {
				return nil
			}
			if builtin.FindBuiltinMethod(e.Value) != nil {
				return nil
			}
			results = append(results, ValidateResult{
				Line:    e.Token.Line,
				Column:  e.Token.Column,
				Message: fmt.Sprintf("'%s' is not defined", e.Value),
			})
		}
	case *parser.CallExpression:
		// Function name: check as call target, not variable reference
		if e.Function != nil {
			// Don't pass isFuncCallArg=true for the function — the function name
			// is checked by the Identifier case's builtin/funcName check
			results = append(results, checkUndefinedVarsInExpr(e.Function, definedVars, funcNames, false)...)
		}
		for _, arg := range e.Arguments {
			results = append(results, checkUndefinedVarsInExpr(arg, definedVars, funcNames, true)...)
		}
	case *parser.DotExpression:
		// Receiver is a module/struct/type name, Property is a method/field name.
		// Neither is a plain variable reference — skip entirely.
	case *parser.InfixExpression:
		if e.Left != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Left, definedVars, funcNames, false)...)
		}
		if e.Right != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Right, definedVars, funcNames, false)...)
		}
	case *parser.PrefixExpression:
		if e.Right != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Right, definedVars, funcNames, false)...)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Expression, definedVars, funcNames, false)...)
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Condition, definedVars, funcNames, false)...)
		}
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				results = append(results, checkUndefinedVarsInStmt(innerStmt, definedVars, funcNames)...)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				results = append(results, checkUndefinedVarsInStmt(innerStmt, definedVars, funcNames)...)
			}
		}
	case *parser.IndexExpression:
		if e.Left != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Left, definedVars, funcNames, false)...)
		}
		if e.Index != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Index, definedVars, funcNames, false)...)
		}
	case *parser.SliceExpression:
		if e.Left != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Left, definedVars, funcNames, false)...)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				results = append(results, checkUndefinedVarsInExpr(e.Range.Start, definedVars, funcNames, false)...)
			}
			if e.Range.End != nil {
				results = append(results, checkUndefinedVarsInExpr(e.Range.End, definedVars, funcNames, false)...)
			}
		}
	case *parser.AssignExpression:
		// Left side is a target, not a reference — don't check it
		if e.Value != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Value, definedVars, funcNames, false)...)
		}
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Condition, definedVars, funcNames, false)...)
		}
		if e.Consequence != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Consequence, definedVars, funcNames, false)...)
		}
		if e.Alternative != nil {
			results = append(results, checkUndefinedVarsInExpr(e.Alternative, definedVars, funcNames, false)...)
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			results = append(results, checkUndefinedVarsInExpr(elem, definedVars, funcNames, false)...)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			results = append(results, checkUndefinedVarsInExpr(elem, definedVars, funcNames, false)...)
		}
	case *parser.StructLiteral:
		for _, f := range e.Fields {
			if f.Value != nil {
				results = append(results, checkUndefinedVarsInExpr(f.Value, definedVars, funcNames, false)...)
			}
		}
	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, innerStmt := range e.Body.Statements {
				results = append(results, checkUndefinedVarsInStmt(innerStmt, definedVars, funcNames)...)
			}
		}
	}
	return results
}

// validateStmtTypes 檢查單個語句的型別問題
func validateStmtTypes(stmt parser.Statement, funcNames map[string]bool, varTypes map[string]string) []ValidateResult {
	var results []ValidateResult

	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		// 進入函式體，用新的作用域
		localTypes := make(map[string]string)
		// 參數加入作用域
		for _, p := range s.Parameters {
			if p.Type != nil {
				localTypes[p.Name] = p.Type.String()
			}
		}
		// 結果參數加入作用域
		for _, p := range s.Results {
			if p.Type != nil {
				localTypes[p.Name] = p.Type.String()
			}
		}
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				errs := validateStmtTypes(bStmt, funcNames, localTypes)
				results = append(results, errs...)
			}
		}

	case *parser.LetStatement:
		// 檢查是否對函式名稱賦值
		if funcNames[s.Name.Value] {
			results = append(results, ValidateResult{
				Line:    s.Token.Line,
				Column:  s.Token.Column,
				Message: fmt.Sprintf("cannot reassign function name '%s'", s.Name.Value),
			})
		}

		// 檢查 nil 賦值到非可空變數
		if _, isNil := s.Value.(*parser.NilLiteral); isNil {
			// 有顯式型別註記
			if s.Type != nil && s.Type.String() != "" && s.Type.String() != s.Name.Value {
				_, isOption := s.Type.(*parser.NullableType)
				if !isOption {
					results = append(results, ValidateResult{
						Line:    s.Token.Line,
						Column:  s.Token.Column,
						Message: fmt.Sprintf("cannot assign nil to non-option variable '%s'", s.Name.Value),
					})
				}
				// 記錄型別
				varTypes[s.Name.Value] = s.Type.String()
				break
			}
			// 無顯式型別，檢查是否已有型別
			if existingType, exists := varTypes[s.Name.Value]; exists {
				if existingType != "" && !strings.HasPrefix(existingType, "?") {
					results = append(results, ValidateResult{
						Line:    s.Token.Line,
						Column:  s.Token.Column,
						Message: fmt.Sprintf("cannot assign nil to non-option variable '%s'", s.Name.Value),
					})
				}
				break
			}
			// 新變數從 nil 推斷不出型別
			results = append(results, ValidateResult{
				Line:    s.Token.Line,
				Column:  s.Token.Column,
				Message: fmt.Sprintf("cannot infer type from nil for variable '%s'", s.Name.Value),
			})
			break
		}

		// 記錄型別
		if s.Type != nil && s.Type.String() != "" && s.Type.String() != s.Name.Value {
			// 顯式型別註記
			varTypes[s.Name.Value] = s.Type.String()
		} else if s.Value != nil {
			// 型別推斷
			inferredType := inferExprType(s.Value, varTypes)
			if inferredType != "" {
				if existingType, exists := varTypes[s.Name.Value]; exists {
					// 變數已有型別，檢查是否相容
					if inferredType != existingType && isConcreteType(existingType) {
						results = append(results, ValidateResult{
							Line:    s.Token.Line,
							Column:  s.Token.Column,
							Message: fmt.Sprintf("cannot assign %s value to %s variable '%s'", inferredType, existingType, s.Name.Value),
						})
					}
				} else {
					// 首次賦值，記錄推斷型別
					varTypes[s.Name.Value] = inferredType
				}
			}
		}

	case *parser.ExpressionStatement:
		// 處理 if 表示式
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, bStmt := range ifExpr.Consequence.Statements {
					errs := validateStmtTypes(bStmt, funcNames, varTypes)
					results = append(results, errs...)
				}
			}
			if ifExpr.Alternative != nil {
				for _, bStmt := range ifExpr.Alternative.Statements {
					errs := validateStmtTypes(bStmt, funcNames, varTypes)
					results = append(results, errs...)
				}
			}
			break
		}
		if assign, ok := s.Expression.(*parser.AssignExpression); ok {
			if ident, ok := assign.Left.(*parser.Identifier); ok {
				// 檢查是否對函式名稱賦值
				if funcNames[ident.Value] {
					results = append(results, ValidateResult{
						Line:    ident.Token.Line,
						Column:  ident.Token.Column,
						Message: fmt.Sprintf("cannot reassign function name '%s'", ident.Value),
					})
				}
				// 檢查 nil 賦值到非可空變數
				isNilAssign := false
				if _, isNil := assign.Value.(*parser.NilLiteral); isNil {
					isNilAssign = true
					if existingType, exists := varTypes[ident.Value]; exists {
						if !strings.HasPrefix(existingType, "?") {
							results = append(results, ValidateResult{
								Line:    ident.Token.Line,
								Column:  ident.Token.Column,
								Message: fmt.Sprintf("cannot assign nil to non-option variable '%s'", ident.Value),
							})
						}
					}
				}
				// 型別不匹配檢查
				if !isNilAssign {
					if existingType, exists := varTypes[ident.Value]; exists {
						valType := inferExprType(assign.Value, varTypes)
						if valType != "" && valType != existingType && isConcreteType(existingType) {
							results = append(results, ValidateResult{
								Line:    ident.Token.Line,
								Column:  ident.Token.Column,
								Message: fmt.Sprintf("cannot assign %s value to %s variable '%s'", valType, existingType, ident.Value),
							})
						}
					}
				}
			}
		}

	case *parser.ForStatement:
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				errs := validateStmtTypes(bStmt, funcNames, varTypes)
				results = append(results, errs...)
			}
		}

	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			errs := validateStmtTypes(bStmt, funcNames, varTypes)
			results = append(results, errs...)
		}

	}

	return results
}

// moduleShortName extracts the last path segment as the module name.
// "std/math" → "math", "fmt" → "fmt", "hash/md5" → "md5"
func moduleShortName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// knownStdModuleNames returns standard library module short names that should
// be auto-recognized for module.function() calling style without explicit imports.
// These modules have ReceiverGlobal builtins whose MethodName matches the
// DotExpression property (e.g., math.degrees() → degrees()).
func knownStdModuleNames() []string {
	return []string{"math"}
}

// GetKnownStdModuleNames returns the list of auto-recognized standard library
// module short names (exported for LSP use).
func GetKnownStdModuleNames() []string {
	return knownStdModuleNames()
}

// resolveModuleCalls walks the program and rewrites module.fn() calls
// where the DotExpression receiver matches an imported module name.
func resolveModuleCalls(program *parser.Program, importedModules []string) {
	if len(importedModules) == 0 {
		return
	}
	modSet := make(map[string]bool)
	for _, m := range importedModules {
		modSet[m] = true
	}
	for _, stmt := range program.Statements {
		resolveModuleCallsInStmt(stmt, modSet)
	}
}

func resolveModuleCallsInStmt(stmt parser.Statement, modSet map[string]bool) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveModuleCallsInExpr(s.Expression, modSet)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, modSet)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveModuleCallsInStmt(bodyStmt, modSet)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveModuleCallsInExpr(s.Condition, modSet)
		}
		if s.Init != nil {
			resolveModuleCallsInStmt(s.Init, modSet)
		}
		if s.Update != nil {
			resolveModuleCallsInStmt(s.Update, modSet)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet)
			}
		}
	}
}

func resolveModuleCallsInExpr(expr parser.Expression, modSet map[string]bool) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// Check if this is a module.fn() call
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if recvIdent, ok := dot.Receiver.(*parser.Identifier); ok {
				if modSet[recvIdent.Value] {
					// Rewrite to direct function call
					e.Function = &parser.Identifier{
						Token: lexer.Token{Type: lexer.IDENT, Literal: dot.Property},
						Value: dot.Property,
					}
				}
			}
		}
		// Recurse into arguments
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveModuleCallsInExpr(arg, modSet)
		}
		return e

	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet)
		}
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, modSet)
		}
		return e

	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, modSet)
		}
		return e

	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, modSet)
		}
		if e.Consequence != nil {
			e.Consequence = resolveModuleCallsInExpr(e.Consequence, modSet)
		}
		if e.Alternative != nil {
			e.Alternative = resolveModuleCallsInExpr(e.Alternative, modSet)
		}
		return e

	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, modSet)
		}
		if e.Consequence != nil {
			for _, bodyStmt := range e.Consequence.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet)
			}
		}
		if e.Alternative != nil {
			for _, bodyStmt := range e.Alternative.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet)
			}
		}
		return e

	case *parser.GroupedExpression:
		if e.Expression != nil {
			e.Expression = resolveModuleCallsInExpr(e.Expression, modSet)
		}
		return e

	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet)
		}
		if e.Index != nil {
			e.Index = resolveModuleCallsInExpr(e.Index, modSet)
		}
		return e

	case *parser.SliceExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				e.Range.Start = resolveModuleCallsInExpr(e.Range.Start, modSet)
			}
			if e.Range.End != nil {
				e.Range.End = resolveModuleCallsInExpr(e.Range.End, modSet)
			}
		}
		return e

	case *parser.AssignExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet)
		}
		if e.Value != nil {
			e.Value = resolveModuleCallsInExpr(e.Value, modSet)
		}
		return e

	default:
		return e
	}
}

// isConstantExpr returns true if the expression is a compile-time constant literal.
func isConstantExpr(expr parser.Expression) bool {
	switch expr.(type) {
	case *parser.IntegerLiteral:
		return true
	case *parser.FloatLiteral:
		return true
	case *parser.StringLiteral:
		return true
	}
	return false
}

// resolveModuleConstants walks the program and replaces Identifier references to
// module constants with their literal values, allowing module functions like
// degrees() to reference pi/e directly.
func resolveModuleConstants(program *parser.Program, constants map[string]parser.Expression) {
	if len(constants) == 0 {
		return
	}
	for _, stmt := range program.Statements {
		resolveModuleConstantsInStmt(stmt, constants)
	}
}

func resolveModuleConstantsInStmt(stmt parser.Statement, constants map[string]parser.Expression) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveModuleConstantsInExpr(s.Expression, constants)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveModuleConstantsInExpr(s.Value, constants)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveModuleConstantsInStmt(bodyStmt, constants)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveModuleConstantsInExpr(s.Condition, constants)
		}
		if s.Init != nil {
			resolveModuleConstantsInStmt(s.Init, constants)
		}
		if s.Update != nil {
			resolveModuleConstantsInStmt(s.Update, constants)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants)
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			s.ReturnValue = resolveModuleConstantsInExpr(s.ReturnValue, constants)
		}
	}
}

func resolveModuleConstantsInExpr(expr parser.Expression, constants map[string]parser.Expression) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		if lit, ok := constants[e.Value]; ok {
			return lit
		}
		return e
	case *parser.CallExpression:
		e.Function = resolveModuleConstantsInExpr(e.Function, constants)
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveModuleConstantsInExpr(arg, constants)
		}
		return e
	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants)
		}
		if e.Right != nil {
			e.Right = resolveModuleConstantsInExpr(e.Right, constants)
		}
		return e
	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveModuleConstantsInExpr(e.Right, constants)
		}
		return e
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleConstantsInExpr(e.Condition, constants)
		}
		if e.Consequence != nil {
			e.Consequence = resolveModuleConstantsInExpr(e.Consequence, constants)
		}
		if e.Alternative != nil {
			e.Alternative = resolveModuleConstantsInExpr(e.Alternative, constants)
		}
		return e
	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleConstantsInExpr(e.Condition, constants)
		}
		if e.Consequence != nil {
			for _, bodyStmt := range e.Consequence.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants)
			}
		}
		if e.Alternative != nil {
			for _, bodyStmt := range e.Alternative.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants)
			}
		}
		return e
	case *parser.GroupedExpression:
		if e.Expression != nil {
			e.Expression = resolveModuleConstantsInExpr(e.Expression, constants)
		}
		return e
	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants)
		}
		if e.Index != nil {
			e.Index = resolveModuleConstantsInExpr(e.Index, constants)
		}
		return e
	case *parser.SliceExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				e.Range.Start = resolveModuleConstantsInExpr(e.Range.Start, constants)
			}
			if e.Range.End != nil {
				e.Range.End = resolveModuleConstantsInExpr(e.Range.End, constants)
			}
		}
		return e
	case *parser.AssignExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants)
		}
		if e.Value != nil {
			e.Value = resolveModuleConstantsInExpr(e.Value, constants)
		}
		return e
	default:
		return e
	}
}

// ValidateFuncArgs checks that function call argument types match the function signature.
// rootDir is optional — if empty, only locally defined function signatures are checked.
// If provided, imported function signatures from module files are also resolved.
func ValidateFuncArgs(program *parser.Program, rootDir string) []ValidateResult {
	var results []ValidateResult

	// 1. Collect local function signatures (including from resolved imports
	//    which are already merged into the program at build time)
	sigs := make(map[string]*funcSig)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			params := make([]paramInfo, len(fd.Parameters))
			for i, p := range fd.Parameters {
				t := ""
				if p.Type != nil {
					t = p.Type.String()
				}
				params[i] = paramInfo{Name: p.Name, Type: t}
			}
			sigs[fd.Name] = &funcSig{ParamTypes: params}
		}
	}

	// 2. Collect imported function signatures from UseStatements
	//    by parsing the referenced module files (when rootDir is available)
	if rootDir != "" {
		pkg, _ := LoadPackage(rootDir)
		for _, stmt := range program.Statements {
			use, ok := stmt.(*parser.UseStatement)
			if !ok || use.Function == "" {
				continue
			}
			funcName := use.Function
			if use.Alias != "" {
				funcName = use.Alias
			}
			if _, exists := sigs[funcName]; exists {
				continue // already defined locally or via another import
			}

			modProg := resolveUseModule(use, pkg)
			if modProg == nil {
				continue
			}
			for _, ms := range modProg.Statements {
				if fd, ok := ms.(*parser.FunctionDefinition); ok && fd.Name == use.Function {
					params := make([]paramInfo, len(fd.Parameters))
					for i, p := range fd.Parameters {
						t := ""
						if p.Type != nil {
							t = p.Type.String()
						}
						params[i] = paramInfo{Name: p.Name, Type: t}
					}
					sigs[funcName] = &funcSig{ParamTypes: params}
					break
				}
			}
		}
	}

	// 3. Validate call expressions
	for _, stmt := range program.Statements {
		results = append(results, checkCallArgsInStmt(stmt, sigs)...)
	}

	return results
}

// resolveUseModule resolves a UseStatement to its module program.
// It handles local paths (/path), std paths, and dependency paths (domain/...).
func resolveUseModule(use *parser.UseStatement, pkg *Package) *parser.Program {
	path := use.Path

	// Local module paths (starting with /)
	if strings.HasPrefix(path, "/") {
		if pkg == nil {
			return nil
		}
		relPath := strings.TrimPrefix(path, "/")
		fullPath := filepath.Join(pkg.RootDir, relPath) + ".no"
		return parseProgramFile(fullPath)
	}

	// std/ paths
	if strings.HasPrefix(path, "std/") || path == "std" {
		modPath := resolveModulePath(moduleShortName(path))
		if modPath == "" {
			return nil
		}
		return parseProgramFile(modPath)
	}

	// Dependency paths (domain/org/repo/...)
	if pkg != nil {
		modPath, err := pkg.ResolveDependencyModule(path)
		if err == nil && modPath != "" {
			return parseProgramFile(modPath)
		}
	}

	return nil
}

// parseProgramFile reads and parses a .no file into a Program.
func parseProgramFile(filePath string) *parser.Program {
	if _, err := os.Stat(filePath); err != nil {
		return nil
	}
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	l := lexer.New(string(source))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil
	}
	return prog
}

type funcSig struct {
	ParamTypes []paramInfo
}

type paramInfo struct {
	Name string
	Type string
}

func checkCallArgsInStmt(stmt parser.Statement, sigs map[string]*funcSig) []ValidateResult {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			return checkCallArgsInExpr(s.Expression, sigs)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			return checkCallArgsInExpr(s.Value, sigs)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			var results []ValidateResult
			for _, bs := range s.Body.Statements {
				results = append(results, checkCallArgsInStmt(bs, sigs)...)
			}
			return results
		}
	case *parser.BlockStatement:
		var results []ValidateResult
		for _, bs := range s.Statements {
			results = append(results, checkCallArgsInStmt(bs, sigs)...)
		}
		return results
	case *parser.ForStatement:
		var results []ValidateResult
		if s.Init != nil {
			results = append(results, checkCallArgsInStmt(s.Init, sigs)...)
		}
		if s.Condition != nil {
			results = append(results, checkCallArgsInExpr(s.Condition, sigs)...)
		}
		if s.Update != nil {
			results = append(results, checkCallArgsInStmt(s.Update, sigs)...)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				results = append(results, checkCallArgsInStmt(bs, sigs)...)
			}
		}
		return results
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return checkCallArgsInExpr(s.ReturnValue, sigs)
		}
	}
	return nil
}

func checkCallArgsInExpr(expr parser.Expression, sigs map[string]*funcSig) []ValidateResult {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		var results []ValidateResult
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if sig, ok := sigs[ident.Value]; ok {
				// Check argument count
				if len(e.Arguments) != len(sig.ParamTypes) {
					results = append(results, ValidateResult{
						Line:    e.Token.Line,
						Column:  e.Token.Column,
						Message: fmt.Sprintf("function '%s' expects %d argument(s), got %d", ident.Value, len(sig.ParamTypes), len(e.Arguments)),
					})
				} else {
					// Check argument types
					varTypes := make(map[string]string)
					for i, arg := range e.Arguments {
						argType := inferExprType(arg, varTypes)
						expectedType := sig.ParamTypes[i].Type
						if expectedType != "" && argType != "" && argType != expectedType {
							results = append(results, ValidateResult{
								Line:    e.Token.Line,
								Column:  e.Token.Column,
								Message: fmt.Sprintf("argument %d of '%s': expected '%s', got '%s'", i+1, ident.Value, expectedType, argType),
							})
						}
					}
				}
			}
		}
		// Recurse into arguments for nested calls
		for _, arg := range e.Arguments {
			results = append(results, checkCallArgsInExpr(arg, sigs)...)
		}
		return results

	case *parser.InfixExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkCallArgsInExpr(e.Left, sigs)...)
		}
		if e.Right != nil {
			results = append(results, checkCallArgsInExpr(e.Right, sigs)...)
		}
		return results

	case *parser.PrefixExpression:
		if e.Right != nil {
			return checkCallArgsInExpr(e.Right, sigs)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			return checkCallArgsInExpr(e.Expression, sigs)
		}
	case *parser.IfExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkCallArgsInExpr(e.Condition, sigs)...)
		}
		if e.Consequence != nil {
			for _, is := range e.Consequence.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs)...)
			}
		}
		if e.Alternative != nil {
			for _, is := range e.Alternative.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs)...)
			}
		}
		return results
	case *parser.IndexExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkCallArgsInExpr(e.Left, sigs)...)
		}
		if e.Index != nil {
			results = append(results, checkCallArgsInExpr(e.Index, sigs)...)
		}
		return results
	case *parser.AssignExpression:
		if e.Value != nil {
			return checkCallArgsInExpr(e.Value, sigs)
		}
	case *parser.ConditionalExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkCallArgsInExpr(e.Condition, sigs)...)
		}
		if e.Consequence != nil {
			results = append(results, checkCallArgsInExpr(e.Consequence, sigs)...)
		}
		if e.Alternative != nil {
			results = append(results, checkCallArgsInExpr(e.Alternative, sigs)...)
		}
		return results
	case *parser.ArrayLiteral:
		var results []ValidateResult
		for _, elem := range e.Elements {
			results = append(results, checkCallArgsInExpr(elem, sigs)...)
		}
		return results
	case *parser.SliceLiteral:
		var results []ValidateResult
		for _, elem := range e.Elements {
			results = append(results, checkCallArgsInExpr(elem, sigs)...)
		}
		return results
	case *parser.StructLiteral:
		var results []ValidateResult
		for _, f := range e.Fields {
			if f.Value != nil {
				results = append(results, checkCallArgsInExpr(f.Value, sigs)...)
			}
		}
		return results
	case *parser.FunctionLiteral:
		if e.Body != nil {
			var results []ValidateResult
			for _, is := range e.Body.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs)...)
			}
			return results
		}
	}
	return nil
}
