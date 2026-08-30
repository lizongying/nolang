package checker

// 本檔案承載 ValidateFuncArgs 校驗簇：函數呼叫實參型別檢查、newtype 別名
// 相容規則、use 模組解析與 lib.no 匯出過濾。原位於 build/transpiler.go，
// 遷出後 lsp 僅依賴 checker 即可完成 vet，不再拉入 transpiler 後端。

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// FilterByExports 依 lib.no 匯出聲明過濾模組 Program（供 build 代碼生成使用）。
// modFilePath is the file path of the module being filtered (used to match
// exports to the correct module). If empty, path-based filtering is skipped.
func FilterByExports(prog *parser.Program, libPath string, modFilePath string) *parser.Program {
	return filterByExports(prog, libPath, modFilePath)
}

// ClearModuleCache 清空模組 AST 解析快取（build 每次 Compile 前調用）。
func ClearModuleCache() {
	clearParseProgramFileCache()
}

// StructFieldTypeString 回傳 struct 欄位的完整型別字串（供 build 使用）。
func StructFieldTypeString(f *parser.StructField) string {
	return structFieldTypeString(f)
}

// isConcreteType 檢查型別名稱是否為已知具體型別
func isConcreteType(typeName string) bool {
	switch typeName {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128",
		"byte", "f64", "str", "bool", "char", "void":
		return true
	}
	// 複合型別：切片、陣列、可空、指針
	if strings.HasPrefix(typeName, "[]") || strings.HasPrefix(typeName, "[") ||
		strings.HasPrefix(typeName, "?") || strings.HasPrefix(typeName, "ptr ") {
		return true
	}
	// 已註冊的單具體型別別名（如 fd）視為具體型別，不再跳過型別檢查
	if validationConcreteTypeAliases != nil {
		if _, ok := validationConcreteTypeAliases[typeName]; ok {
			return true
		}
	}
	return false
}

// extractArrayElemType extracts the element type from an array/slice type string.
// e.g. "[]byte" → "byte", "[16]byte" → "byte", "[4]i64" → "i64".
// Returns "" if the type has no element type (e.g. "[4]").
func extractArrayElemType(typeStr string) string {
	idx := strings.Index(typeStr, "]")
	if idx >= 0 && idx+1 < len(typeStr) {
		return typeStr[idx+1:]
	}
	return ""
}

// validationConcreteTypeAliases holds single concrete type alias name →
// underlying type string (e.g. "fd" → "i64"), populated by ValidateTypes.
// Used to enforce newtype semantics: an alias of a primitive type is
// mutually exclusive with its underlying type (i64 var cannot be assigned
// to fd var, and vice versa), with an integer-literal exception
// (STDIN-FD fd = 0 is allowed). Composite aliases (e.g. bytes=[]byte)
// are also registered but treated as transparent (no newtype enforcement).
var validationConcreteTypeAliases map[string]string

// ValidateFuncArgCount checks only argument counts against function signatures.
// Unlike ValidateFuncArgs, it does NOT check argument types, making it safe
// to run on merged programs that include standard library functions with
// complex type inference (generics, monomorphization) that would cause
// false-positive type errors.
//
// This is called after module merging to catch missing-argument errors for
// standard library functions (e.g. sha1-hex() with 0 args) that were not
// visible to ValidateFuncArgs (which runs before module merging).
func ValidateFuncArgCount(program *parser.Program) []ValidateResult {
	validationMu.Lock()
	defer validationMu.Unlock()
	// Collect function signatures from the merged program
	sigs := make(map[string]*funcSig)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			sigs[fd.Name] = funcSigFromDef(fd)
		}
	}
	var results []ValidateResult
	for _, stmt := range program.Statements {
		results = append(results, checkArgCountInStmt(stmt, sigs)...)
	}
	return results
}

// checkArgCountInStmt walks a statement and checks CallExpression argument counts.
func checkArgCountInStmt(stmt parser.Statement, sigs map[string]*funcSig) []ValidateResult {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			return checkArgCountInExpr(s.Expression, sigs)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			return checkArgCountInExpr(s.Value, sigs)
		}
	case *parser.FunctionDefinition:
		var results []ValidateResult
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				results = append(results, checkArgCountInStmt(bs, sigs)...)
			}
		}
		return results
	}
	return nil
}

// checkArgCountInExpr walks an expression and checks CallExpression argument counts.
func checkArgCountInExpr(expr parser.Expression, sigs map[string]*funcSig) []ValidateResult {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		var results []ValidateResult
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if sig, ok := sigs[ident.Value]; ok {
				// Compute minimum required arguments (excluding params with defaults)
				minArgs := len(sig.ParamTypes)
				// Method calls (e.g. ec.init(c, key) rewritten to net.enc-conn.init(c, key))
				// have an implicit self receiver as the first parameter in the signature,
				// but it is NOT in expr.Arguments. Subtract 1 for the implicit self.
				if len(sig.ParamTypes) > 0 && sig.ParamTypes[0].Name == "self" {
					minArgs--
				}
				for j := len(sig.ParamTypes) - 1; j >= 0; j-- {
					if sig.ParamTypes[j].HasDefault {
						minArgs = j
					} else {
						break
					}
				}
				// Allow more args than params (could be output param), only flag too few
				if len(e.Arguments) < minArgs {
					results = append(results, ValidateResult{
						TraceID: "j7dzteja",
						Line:    e.Token.Line,
						Column:  e.Token.Column,
						Message: fmt.Sprintf("function '%s' expects at least %d argument(s), got %d", ident.Value, minArgs, len(e.Arguments)),
					})
				}
			}
		}
		// Recurse into arguments for nested calls
		for _, arg := range e.Arguments {
			results = append(results, checkArgCountInExpr(arg, sigs)...)
		}
		return results
	case *parser.InfixExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkArgCountInExpr(e.Left, sigs)...)
		}
		if e.Right != nil {
			results = append(results, checkArgCountInExpr(e.Right, sigs)...)
		}
		return results
	case *parser.PrefixExpression:
		if e.Right != nil {
			return checkArgCountInExpr(e.Right, sigs)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			return checkArgCountInExpr(e.Expression, sigs)
		}
	case *parser.IfExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkArgCountInExpr(e.Condition, sigs)...)
		}
		if e.Consequence != nil {
			for _, bs := range e.Consequence.Statements {
				results = append(results, checkArgCountInStmt(bs, sigs)...)
			}
		}
		if e.Alternative != nil {
			for _, bs := range e.Alternative.Statements {
				results = append(results, checkArgCountInStmt(bs, sigs)...)
			}
		}
		return results
	}
	return nil
}

func ValidateFuncArgs(program *parser.Program, rootDir string) []ValidateResult {
	validationMu.Lock()
	defer validationMu.Unlock()
	var results []ValidateResult
	// 1. Collect local function signatures (including from resolved imports
	//    which are already merged into the program at build time)
	sigs := make(map[string]*funcSig)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			sigs[fd.Name] = funcSigFromDef(fd)
		}
	}
	// 2. Collect imported function signatures from UseStatements
	//    by parsing the referenced module files (when rootDir is available)
	if rootDir != "" {
		pkg, _ := pkg.LoadPackage(rootDir)
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
			modProg := resolveUseModule(use, pkg, rootDir)
			if modProg == nil {
				continue
			}
			for _, ms := range modProg.Statements {
				if fd, ok := ms.(*parser.FunctionDefinition); ok && fd.Name == use.Function {
					sigs[funcName] = funcSigFromDef(fd)
					break
				}
			}
		}
	}
	// 3. Collect struct field type info from struct definitions
	structFields := make(map[string]map[string]string)
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			fields := make(map[string]string)
			for _, f := range sd.Fields {
				if typeStr := structFieldTypeString(f); typeStr != "" {
					fields[f.Name] = typeStr
				}
			}
			if len(fields) > 0 {
				structFields[sd.Name] = fields
			}
		}
	}
	// 4. Collect top-level variable types (explicit type annotations) so that
	//    call argument type checking can resolve variable types. This is needed
	//    for newtype enforcement (e.g. passing an `fd` variable to an `i64`
	//    parameter must be rejected).
	topLevelVarTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil && ls.Type.String() != "" && ls.Type.String() != ls.Name.Value {
				if _, exists := topLevelVarTypes[ls.Name.Value]; !exists {
					topLevelVarTypes[ls.Name.Value] = ls.Type.String()
				}
			}
		}
	}
	// 5. Validate call expressions
	for _, stmt := range program.Statements {
		// 跳過泛型模板函式和單態化函式：這些函式體中的型別在使用前
		// 無法精確檢查（泛型型別參數 t 在特化前不是具體型別）。
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if strings.Contains(fd.Name, "__") {
				continue
			}
			if len(fd.GenericParams) > 0 || strings.HasPrefix(fd.Name, "[") {
				continue
			}
		}
		results = append(results, checkCallArgsInStmt(stmt, sigs, topLevelVarTypes, structFields)...)
	}
	return results
}

// resolveUseModule resolves a UseStatement to its module program.
// It handles local paths (/path), std paths, and dependency paths (domain/...).
// rootDir is the directory of the entry source file, used as a fallback
// for workspace root resolution when pkg is nil (no package.jsonc found).
func resolveUseModule(use *parser.UseStatement, p *pkg.Package, rootDir string) *parser.Program {
	filePath := resolveModuleFilePath(use, p, rootDir)
	if filePath == "" {
		return nil
	}
	prog := parseProgramFile(filePath)
	if prog == nil {
		return nil
	}
	// Apply lib.no export filtering
	pkgRoot := findPackageRootFromFile(filePath)
	if pkgRoot != "" {
		libPath := filepath.Join(pkgRoot, "lib.no")
		if _, err := os.Stat(libPath); err == nil {
			prog = filterByExports(prog, libPath, filePath)
		}
	}
	return prog
}

// resolveModuleFilePath resolves a UseStatement to its source file path
// without parsing or filtering. Used by collectRawModuleSymbols to parse
// the raw module file directly (bypassing lib.no export filtering).
func resolveModuleFilePath(use *parser.UseStatement, p *pkg.Package, rootDir string) string {
	path := use.Path
	// Local module paths (starting with /)
	if strings.HasPrefix(path, "/") {
		var baseDir string
		if p != nil {
			baseDir = p.WorkspaceRoot()
		}
		if baseDir == "" && rootDir != "" {
			if ws, ok := pkg.FindWorkspaceRoot(rootDir); ok {
				baseDir = ws
			}
		}
		if baseDir == "" {
			return ""
		}
		relPath := strings.TrimPrefix(path, "/")
		return filepath.Join(baseDir, relPath) + ".no"
	} else if strings.HasPrefix(path, "std/") || path == "std" {
		relPath := strings.TrimPrefix(path, "std/")
		if path == "std" {
			relPath = ""
		}
		return resolveModulePath(relPath)
	} else if p != nil {
		filePath, err := p.ResolveDependencyModule(path)
		if err == nil && filePath != "" {
			return filePath
		}
	}
	return ""
}

// parseProgramFileCache: 包級 AST 解析快取，供 standalone 函數 parseProgramFile 使用。
// resolveUseModule（被 ValidateFuncArgs 調用）獨立於 Transpiler，無法存取 per-instance 快取。
// 每次 Compile 開始時由 clearParseProgramFileCache() 清空，避免跨編譯的陳舊資料。
//
// 緩存鍵為 (路徑, 內容哈希)（見 cache.Key），同路徑內容變化自動失效；
// LRU 有容量上限，常駐進程不會無限增長。
var parseProgramFileCache = cache.NewLRU[*parser.Program](2048)

func clearParseProgramFileCache() {
	parseProgramFileCache.Clear()
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
	// 內容尋址鍵：同路徑內容不同 → 不同鍵 → 不會命中過期 AST
	ck := cache.Key(filePath, string(source))
	if cached, ok := parseProgramFileCache.Get(ck); ok {
		return cached
	}
	l := lexer.NewCached(filePath, string(source))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil
	}
	parseProgramFileCache.Put(ck, prog)
	return prog
}

func checkCallArgsInStmt(stmt parser.Statement, sigs map[string]*funcSig, varTypes map[string]string, structFields map[string]map[string]string) []ValidateResult {
	return checkCallArgsInStmtWithResultParams(stmt, sigs, varTypes, nil, structFields)
}

// checkCallArgsInIfExpr descends into an IfExpression's branches.
func checkCallArgsInIfExpr(s *parser.IfExpression, sigs map[string]*funcSig, varTypes map[string]string, resultParamNames map[string]bool, structFields map[string]map[string]string) []ValidateResult {
	var results []ValidateResult
	if s.Condition != nil {
		results = append(results, checkCallArgsInExpr(s.Condition, sigs, varTypes, structFields)...)
	}
	if s.Consequence != nil {
		for _, bs := range s.Consequence.Statements {
			results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields)...)
		}
	}
	if s.Alternative != nil {
		for _, bs := range s.Alternative.Statements {
			results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields)...)
		}
	}
	return results
}
func checkCallArgsInStmtWithResultParams(stmt parser.Statement, sigs map[string]*funcSig, varTypes map[string]string, resultParamNames map[string]bool, structFields map[string]map[string]string) []ValidateResult {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			// If-as-statement: ExpressionStatement wraps an IfExpression.
			// Descend into both branches so result-param types propagate.
			if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
				return checkCallArgsInIfExpr(ifExpr, sigs, varTypes, resultParamNames, structFields)
			}
			return checkCallArgsInExpr(s.Expression, sigs, varTypes, structFields)
		}
	case *parser.LetStatement:
		// Compiler-injected synthetic let statements (e.g. match arm `it` bindings).
		// If the synthetic binding has an explicit Type, record it directly.
		// If not (fallback path when parser couldn't resolve the matched variable's
		// type at parse time), infer from value but unwrap Optional types (?T → T),
		// because `it` in a match arm always represents the unwrapped inner value.
		if s.IsSynthetic {
			if s.Value != nil {
				results := checkCallArgsInExpr(s.Value, sigs, varTypes, structFields)
				if s.Name != nil && !resultParamNames[s.Name.Value] {
					if s.Type != nil && s.Type.String() != "" {
						varTypes[s.Name.Value] = s.Type.String()
					} else if _, exists := varTypes[s.Name.Value]; !exists {
						inferred := inferExprType(s.Value, varTypes, nil, "")
						if strings.HasPrefix(inferred, "?") {
							inferred = strings.TrimPrefix(inferred, "?")
						}
						if inferred != "" {
							varTypes[s.Name.Value] = inferred
						}
					}
				}
				return results
			}
			if s.Name != nil && s.Type != nil {
				varTypes[s.Name.Value] = s.Type.String()
			}
			return nil
		}
		if s.Value != nil {
			results := checkCallArgsInExpr(s.Value, sigs, varTypes, structFields)
			// Register the variable type from assignment for subsequent checks.
			// Prefer the user-defined function's first return type over the
			// generic "i64" default of inferExprType for CallExpression.
			// Skip result parameters: their declared type is authoritative and
			// must not be overwritten.
			if s.Name != nil && !resultParamNames[s.Name.Value] {
				// If the variable has an explicit type annotation, that type
				// is authoritative — do not overwrite with inferred type.
				// e.g. `t u64 = 0` should register "u64", not "i64".
				if s.Type != nil && s.Type.String() != "" {
					varTypes[s.Name.Value] = s.Type.String()
				} else if _, exists := varTypes[s.Name.Value]; !exists {
					// No explicit type and variable not yet seen: infer from value.
					inferred := ""
					if call, ok := s.Value.(*parser.CallExpression); ok {
						if fn, ok := call.Function.(*parser.Identifier); ok {
							inferred = funcSigFirstReturnType(sigs[fn.Value])
						}
					}
					if inferred == "" {
						inferred = inferExprType(s.Value, varTypes, nil, "")
					}
					if inferred != "" {
						varTypes[s.Name.Value] = inferred
					} else if fnName := isLHSInferredBuiltinCall(s.Value); fnName != "" {
						// LHS-inferred builtin (with-len, with-cap, with-cap-len)
						// cannot determine its type without an explicit type
						// annotation. Report an error to prevent silent default
						// to []i64 (8 bytes/element).
						valPos := s.Value.Pos()
						results = append(results, ValidateResult{
							TraceID: "2e14k5et",
							Line:    valPos.Line,
							Column:  valPos.Column,
							Message: fmt.Sprintf("cannot infer type for '%s': %s() requires an explicit type annotation on the left side (e.g. `name []byte = %s(n)`)", s.Name.Value, fnName, fnName),
						})
					}
				}
				// If variable already exists in varTypes (e.g. declared with
				// explicit type earlier), keep the existing type — do not
				// overwrite with inferred type from reassignment.
			} else if s.Name != nil && resultParamNames[s.Name.Value] {
				// Result parameter: only seed the type if we can pin it
				// from a known function's first return type, otherwise
				// leave the declared type alone.
				if ident, ok := s.Value.(*parser.CallExpression); ok {
					if fn, ok := ident.Function.(*parser.Identifier); ok {
						if t := funcSigFirstReturnType(sigs[fn.Value]); t != "" {
							varTypes[s.Name.Value] = t
						}
					}
				}
			}
			return results
		}
		// Type-only declaration: name [N]type (no = value)
		if s.Name != nil && s.Type != nil {
			varTypes[s.Name.Value] = s.Type.String()
		}
	case *parser.MultiAssignStatement:
		var results []ValidateResult
		if s.Value != nil {
			results = append(results, checkCallArgsInExpr(s.Value, sigs, varTypes, structFields)...)
			// #5: Validate that the number of assignment targets matches the
			// number of return values of the called function.  A mismatch
			// causes silent memory corruption / SIGTRAP at runtime.
			if callExpr, ok := s.Value.(*parser.CallExpression); ok {
				expectedReturns := lookupReturnCount(callExpr, sigs, varTypes)
				if expectedReturns >= 0 && len(s.Targets) != expectedReturns {
					results = append(results, ValidateResult{
						TraceID: "rfzrw1nh",
						Line:    s.Token.Line,
						Column:  s.Token.Column,
						Message: fmt.Sprintf("function returns %d value(s) but %d target(s) provided", expectedReturns, len(s.Targets)),
					})
				}
			}
		}
		return results
	case *parser.FunctionDefinition:
		// Build local var types including function parameters and result params.
		// Result parameter types are FROZEN — they may not be re-inferred from
		// later assignments in the body (e.g. `q = zero()` must not turn `q`
		// from `bigint` into the default call return type `i64`).
		localTypes := make(map[string]string)
		for k, v := range varTypes {
			localTypes[k] = v
		}
		innerResultParams := make(map[string]bool)
		for _, p := range s.Parameters {
			if p.Type != nil {
				localTypes[p.Name] = p.Type.String()
			}
		}
		for _, r := range s.Results {
			if r.Name != "" && r.Type != nil {
				localTypes[r.Name] = r.Type.String()
				innerResultParams[r.Name] = true
			}
		}
		if s.Body != nil {
			var results []ValidateResult
			for _, bs := range s.Body.Statements {
				results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, localTypes, innerResultParams, structFields)...)
			}
			return results
		}
	case *parser.BlockStatement:
		var results []ValidateResult
		for _, bs := range s.Statements {
			results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields)...)
		}
		return results
	case *parser.ForStatement:
		var results []ValidateResult
		if s.Init != nil {
			results = append(results, checkCallArgsInStmtWithResultParams(s.Init, sigs, varTypes, resultParamNames, structFields)...)
		}
		if s.Condition != nil {
			results = append(results, checkCallArgsInExpr(s.Condition, sigs, varTypes, structFields)...)
		}
		if s.Update != nil {
			results = append(results, checkCallArgsInStmtWithResultParams(s.Update, sigs, varTypes, resultParamNames, structFields)...)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields)...)
			}
		}
		return results
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return checkCallArgsInExpr(s.ReturnValue, sigs, varTypes, structFields)
		}
	}
	return nil
}

// resolveExprType resolves the type of an expression, using struct field definitions
// and variable type info to correctly handle struct field access and array element types.
func resolveExprType(expr parser.Expression, varTypes map[string]string, structFields map[string]map[string]string) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IndexExpression:
		// Array/slice element access: extract element type from [N]type or []type
		leftType := resolveExprType(e.Left, varTypes, structFields)
		if strings.HasPrefix(leftType, "[") {
			if idx := strings.LastIndex(leftType, "]"); idx >= 0 && idx+1 < len(leftType) {
				elemType := leftType[idx+1:]
				if elemType != "" {
					return elemType
				}
			}
		}
		return ""
	case *parser.DotExpression:
		// Struct field access: resolve field type from struct definition
		if ident, ok := e.Receiver.(*parser.Identifier); ok {
			if receiverType, ok := varTypes[ident.Value]; ok {
				if fields, ok := structFields[receiverType]; ok {
					if t, ok := fields[e.Property]; ok {
						return t
					}
				}
			}
		}
		return ""
	case *parser.SliceExpression:
		// Slicing [N]T returns []T; slicing str returns str
		if e.Left != nil {
			leftType := resolveExprType(e.Left, varTypes, structFields)
			if strings.HasPrefix(leftType, "[") {
				if idx := strings.LastIndex(leftType, "]"); idx >= 0 && idx+1 < len(leftType) {
					return "[]" + leftType[idx+1:]
				}
			}
			if leftType == "str" {
				return "str"
			}
			return leftType
		}
		return ""
	case *parser.InfixExpression:
		// Comparison and logical operators always produce bool
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool"
		}
		// For arithmetic and bitwise operators, infer from operands.
		// Use resolveExprType (not inferExprType) so that IndexExpression
		// (e.g. v[d]) and DotExpression (e.g. .field) are resolved correctly.
		if t := resolveExprType(e.Left, varTypes, structFields); t != "" {
			return t
		}
		return resolveExprType(e.Right, varTypes, structFields)
	case *parser.PrefixExpression:
		if e.Operator == "!" {
			return "bool"
		}
		return resolveExprType(e.Right, varTypes, structFields)
	default:
		return inferExprType(expr, varTypes, nil, "")
	}
}

// isIntegerLiteral checks if an expression is an integer literal.
func isIntegerLiteral(expr parser.Expression) bool {
	_, ok := expr.(*parser.IntegerLiteral)
	return ok
}

// isNumericType returns true for all integer and float types.
func isNumericType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128", "f32", "f64":
		return true
	}
	return false
}

// intTypeRange returns the (min, max) range for a given integer type.
// byte is treated as u8 (0–255).
// char is treated as a Unicode code point (0–0x10FFFF).
func intTypeRange(t string) (min, max int64, ok bool) {
	// 單具體型別別名解析：若 t 是已註冊的整型基礎別名（如 fd → i64），
	// 遞迴解析到底層型別的整數範圍，使整數字面量例外可正確範圍檢查
	if validationConcreteTypeAliases != nil {
		if underlying, exists := validationConcreteTypeAliases[t]; exists {
			return intTypeRange(underlying)
		}
		// Try with module prefix stripped (e.g. "fs.fd" → "fd")
		if short := stripModulePrefix(t); short != t {
			if underlying, exists := validationConcreteTypeAliases[short]; exists {
				return intTypeRange(underlying)
			}
		}
	}
	switch t {
	case "i8":
		return -128, 127, true
	case "i16":
		return -32768, 32767, true
	case "i32":
		return -2147483648, 2147483647, true
	case "i64":
		return -9223372036854775808, 9223372036854775807, true
	case "i128":
		// i128 range exceeds int64; any int64 literal fits within i128.
		return -9223372036854775808, 9223372036854775807, true
	case "u8", "byte":
		return 0, 255, true
	case "char":
		return 0, 1114111, true // Unicode code points: 0 to 0x10FFFF
	case "u16":
		return 0, 65535, true
	case "u32":
		return 0, 4294967295, true
	case "u64":
		// u64 max (2^64-1) cannot be represented in int64.
		// We use int64 max as the upper bound for range comparisons
		// involving int64 values. Large unsigned literals (> int64 max)
		// are handled separately in isArgTypeCompatible via uint64FromLiteral.
		return 0, 9223372036854775807, true
	case "u128":
		// u128 range exceeds int64; any non-negative int64 literal fits within u128.
		// Large unsigned literals are handled via uint64FromLiteral.
		return 0, 9223372036854775807, true
	}
	return 0, 0, false
}

// integerBitWidth returns the bit width of an integer type (8, 16, 32, 64, 128).
// Returns 0 for non-integer types.
func integerBitWidth(t string) int {
	switch t {
	case "i8", "u8", "byte":
		return 8
	case "i16", "u16":
		return 16
	case "i32", "u32":
		return 32
	case "i64", "u64":
		return 64
	case "i128", "u128":
		return 128
	}
	return 0
}

// isSafeBitwiseNarrowing 報告一個表達式賦值給更窄的無號整數型別時是否安全。
//
// 當賦值的右側表達式僅由位元運算（& | ^ << >>）和整數字面量/byte 值構成，
// 且目標型別為無號整數（u8/u16/u32/u64/byte）時，截斷高位是位元操作的標準語義，
// 不會造成資料遺失（mask/位移已保證結果落在目標範圍內或刻意截斷高位）。
//
// 對有號目標型別從不適用，因為符號位元的截斷語義不明確。
func isSafeBitwiseNarrowing(expr parser.Expression, targetType string) bool {
	// 僅對無號整數目標型別放行
	switch targetType {
	case "u8", "u16", "u32", "u64", "u128", "byte":
	default:
		return false
	}
	return isBitwiseConstructExpr(expr)
}

// narrowingHint 回傳一個可操作的窄化修復提示。當 fromType 的值需要賦值給
// 更窄的 toType 時，提示使用者如何用位元運算安全窄化。
//
// 對無號目標型別：建議使用 mask（如 `& 4294967295`）或位移（如 `>> 32`）。
// 對有號目標型別：提示位元窄化不適用（符號位元截斷語義不明確）。
// 非整數型別或非窄化場景回傳空字串。
func narrowingHint(fromType, toType string) string {
	fromBits := intTypeBits(fromType)
	toBits := intTypeBits(toType)
	if fromBits == 0 || toBits == 0 || fromBits <= toBits {
		return ""
	}
	shift := fromBits - toBits
	switch toType {
	case "u8", "u16", "u32", "u64", "byte":
		mask := (uint64(1) << toBits) - 1
		return fmt.Sprintf("; hint: narrow safely with a bitwise mask (e.g. `& %d`) or right shift (e.g. `>> %d`)", mask, shift)
	default:
		// 有號目標型別：位元窄化不適用
		return fmt.Sprintf("; hint: bitwise narrowing is not safe for signed target %s (sign-bit truncation is ambiguous); use an explicit range check instead", toType)
	}
}

// isBitwiseConstructExpr 判斷表達式的頂層是否為位元運算（& | ^ << >>）。
//
// 當一個賦值的右側以位元運算為頂層運算子時，其語義是構造一個位元模式，
// 賦值給更窄的無號整數型別只是截斷高位，是位元操作的標準安全模式
// （常見於 ChaCha20/Poly1305 等密碼學/編解碼代碼）。
//
// 葉節點可以是任意子表達式（變數、陣列索引、字面量等）——只要頂層是位元運算即可，
// 因為截斷發生在賦值那一刻，與值的來源無關。但單純的直接賦值（如 u64_var → u32）
// 頂層不是位元運算，不會被此規則放行。
func isBitwiseConstructExpr(expr parser.Expression) bool {
	if expr == nil {
		return false
	}
	// 括號包裹：穿透到內層
	if g, ok := expr.(*parser.GroupedExpression); ok {
		return isBitwiseConstructExpr(g.Expression)
	}
	// 頂層必須是位元運算 InfixExpression
	ie, ok := expr.(*parser.InfixExpression)
	if !ok {
		return false
	}
	switch ie.Operator {
	case "&", "|", "^", "<<", ">>":
		return true
	}
	return false
}

// isArgTypeCompatible checks whether argType can be used where expectedType
// is required. This handles implicit coercions that the compiler allows:
//   - [N]T → []T  (fixed array passed where slice is expected)
//   - i64 literal → any integer type whose range includes the literal value
//     (e.g. 200 fits u8, but 300 does not; -1 does not fit any unsigned type)
//   - Implicit widening: a narrower integer type (e.g. byte/u8) can be
//     passed where a wider type (e.g. i64) is expected, as long as every
//     value of the narrower type fits within the wider type's range.
//   - Safe bitwise narrowing: an expression composed solely of bitwise
//     operators (& | ^ << >>) and integer literals may be assigned to a
//     narrower unsigned integer type, since the high-bit truncation is the
//     intended bit-pattern construction (common in crypto/codec code).
//   - Newtype enforcement: a single concrete type alias of a primitive
//     type (e.g. fd=i64) is mutually exclusive with its underlying type.
//     i64 var → fd and fd var → i64 are rejected. Integer literals cross
//     the boundary (STDIN-FD fd = 0 is allowed).
func isArgTypeCompatible(expectedType, argType string, arg parser.Expression) bool {
	if argType == expectedType {
		return true
	}
	// Map type ↔ hashmap specialization compatibility:
	// [K]V (map type syntax) and hashmap-K-V (specialized struct name)
	// refer to the same underlying type. The type checker sees the
	// variable type as [K]V but function signatures use hashmap-K-V.
	mapHashExpected := checkerMapTypeToHashmapName(expectedType)
	mapHashArg := checkerMapTypeToHashmapName(argType)
	if mapHashExpected != "" && mapHashExpected == mapHashArg {
		return true
	}
	if mapHashExpected != "" && mapHashExpected == argType {
		return true
	}
	if mapHashArg != "" && mapHashArg == expectedType {
		return true
	}
	// str 和 []byte 互通：底層都是位元組序列，標準庫中常將 str 字面量
	// 傳給 []byte 參數（如 sha256('') ），代碼生成器正確處理轉換。
	if (expectedType == "[]byte" && argType == "str") ||
		(expectedType == "str" && argType == "[]byte") {
		return true
	}
	// ?T 到 T 的隱式解包：在 ok 分支中，?T 變數已確認為有值，
	// 傳給期望 T 的參數是安全的（代碼生成器會自動取值）。
	if strings.HasPrefix(argType, "?") && argType[1:] == expectedType {
		return true
	}
	// 模組前綴類型相容：合併後類型可能被前綴化（如 bigint.bigint），
	// 但變數類型仍為未前綴的 bare name（如 bigint），允許互通。
	if expectedType == argType || strings.HasSuffix(expectedType, "."+argType) ||
		strings.HasSuffix(argType, "."+expectedType) {
		return true
	}
	// 聯合類型成員相容：當 argType 是聯合類型（如 "i64 | err"），
	// 檢查 expectedType 是否是其中一個成員。
	if strings.Contains(argType, " | ") {
		for _, part := range strings.Split(argType, " | ") {
			part = strings.TrimSpace(part)
			if part == expectedType || isArgTypeCompatible(expectedType, part, arg) {
				return true
			}
		}
	}
	// Newtype enforcement: a primitive single concrete type alias and its
	// underlying type are mutually exclusive. Block i64 var → fd and
	// fd var → i64 (non-literal cases). Integer literals are allowed to
	// cross the boundary (handled by the integer literal path below).
	if isAliasPair(expectedType, argType) {
		if _, isLit := integerLiteralValue(arg); !isLit {
			return false
		}
		// fall through to integer literal path for range check
	}
	// Safe bitwise narrowing (checked early so it takes precedence over
	// the widening range check below): an expression built solely from
	// bitwise operators may be assigned to a narrower unsigned integer
	// type — the high-bit truncation is the intended bit-pattern
	// construction (common in crypto/codec code like ChaCha20/Poly1305).
	if isSafeBitwiseNarrowing(arg, expectedType) {
		return true
	}
	// [N]T is compatible with []T (array-to-slice coercion)
	if strings.HasPrefix(argType, "[") && !strings.HasPrefix(argType, "[]") {
		if idx := strings.Index(argType, "]"); idx >= 0 {
			elemType := argType[idx+1:]
			if "[]"+elemType == expectedType {
				return true
			}
		}
	}
	// []T is compatible with [N]T (slice-to-array coercion): a SliceLiteral
	// can be assigned to a fixed-size array variable, following the left-hand
	// side type. e.g. BLAKE2B-SIGMA [12][16]i64 = [[0,1,...,15], ...]
	// where each inner [0,1,...,15] is a SliceLiteral inferred as []i64.
	if strings.HasPrefix(argType, "[]") && strings.HasPrefix(expectedType, "[") && !strings.HasPrefix(expectedType, "[]") {
		if idx := strings.Index(expectedType, "]"); idx >= 0 {
			expectedElemType := expectedType[idx+1:]
			if "[]"+expectedElemType == argType {
				return true
			}
		}
	}
	// Integer/char literals (inferred as i64, byte, or char) are compatible with integer types
	// whose range includes the literal value.
	if val, ok := integerLiteralValue(arg); ok {
		if argType == "i64" || argType == "byte" || argType == "char" {
			if min, max, ok := intTypeRange(expectedType); ok {
				// Fast path: the int64 value fits within the target range.
				if val >= min && val <= max {
					return true
				}
				// Fallback for large unsigned literals (e.g. 18446744073709551615
				// = 2^64-1) that overflow int64 and become negative. When the
				// target is an unsigned type and the literal's source text
				// represents a valid uint64 within the target's range, allow it.
				if val < 0 && min == 0 {
					if uval, ok := uint64FromLiteral(arg); ok {
						// For u64, any uint64 value is in range.
						if expectedType == "u64" || expectedType == "u128" {
							return true
						}
						return uval <= uint64(max)
					}
				}
				return false
			}
		}
	}
	// Transparent composite alias: a composite alias (e.g. bytes=[]byte)
	// is interchangeable with its underlying type — no newtype enforcement.
	if validationConcreteTypeAliases != nil {
		if underlying, ok := validationConcreteTypeAliases[expectedType]; ok && underlying == argType {
			return true
		}
		// Try with module prefix stripped (e.g. "fs.fd" → "fd")
		if expectedShort := stripModulePrefix(expectedType); expectedShort != expectedType {
			if underlying, ok := validationConcreteTypeAliases[expectedShort]; ok && underlying == argType {
				return true
			}
		}
		if underlying, ok := validationConcreteTypeAliases[argType]; ok && underlying == expectedType {
			return true
		}
		if argShort := stripModulePrefix(argType); argShort != argType {
			if underlying, ok := validationConcreteTypeAliases[argShort]; ok && underlying == expectedType {
				return true
			}
		}
	}
	// Implicit widening: if both types are integer types and the argType's
	// range is fully contained within the expectedType's range, allow the
	// conversion (e.g. byte → i64, u8 → i32, i8 → i64).
	if aMin, aMax, aOk := intTypeRange(argType); aOk {
		if eMin, eMax, eOk := intTypeRange(expectedType); eOk {
			// 同位寬有符号/無符號整數之間的位模式 reinterpret
			// （如 i64→u64, i32→u32）：標準庫中有合法用途
			// （如 hex 格式化將 i64 位模式轉為 u64）。
			if integerBitWidth(argType) == integerBitWidth(expectedType) &&
				integerBitWidth(argType) > 0 {
				return true
			}
			return aMin >= eMin && aMax <= eMax
		}
	}
	return false
}

// isSimplePrimitiveTypeName reports whether t is a simple primitive type
// name (i8/i16/i32/i64/u8/u16/u32/u64/byte/char/bool/f32/f64), as opposed
// to a composite type string like "[]byte" or "?i64".
func isSimplePrimitiveTypeName(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128",
		"byte", "char", "bool", "f32", "f64":
		return true
	}
	return false
}

// isAliasPair reports whether (a, b) form a newtype pair: one is a
// registered single concrete type alias of the other, AND the underlying
// type is a simple primitive. Only primitive aliases enforce newtype
// semantics (mutual exclusion with their underlying type); composite
// aliases (e.g. bytes=[]byte) are transparent and return false here.
func isAliasPair(a, b string) bool {
	if validationConcreteTypeAliases == nil {
		return false
	}
	// Strip module prefix for lookup (e.g. "fs.fd" → "fd")
	aShort := stripModulePrefix(a)
	bShort := stripModulePrefix(b)
	// a is alias, b is underlying (try both short and full forms)
	if underlying, ok := validationConcreteTypeAliases[a]; ok && underlying == b {
		return isSimplePrimitiveTypeName(underlying)
	}
	if aShort != a {
		if underlying, ok := validationConcreteTypeAliases[aShort]; ok && underlying == b {
			return isSimplePrimitiveTypeName(underlying)
		}
	}
	// b is alias, a is underlying (try both short and full forms)
	if underlying, ok := validationConcreteTypeAliases[b]; ok && underlying == a {
		return isSimplePrimitiveTypeName(underlying)
	}
	if bShort != b {
		if underlying, ok := validationConcreteTypeAliases[bShort]; ok && underlying == a {
			return isSimplePrimitiveTypeName(underlying)
		}
	}
	return false
}

// stripModulePrefix removes a module prefix from a type name.
// e.g. "fs.fd" → "fd", "http.request" → "request".
// If there is no prefix (no "." or only leading "."), returns the original.
func stripModulePrefix(typeName string) string {
	if idx := strings.LastIndex(typeName, "."); idx > 0 {
		return typeName[idx+1:]
	}
	return typeName
}

// integerLiteralValue extracts the int64 value from an integer literal,
// including negative literals expressed as PrefixExpression("-", IntegerLiteral).
// CharLiteral values are converted to their Unicode code point.
func integerLiteralValue(expr parser.Expression) (int64, bool) {
	if lit, ok := expr.(*parser.IntegerLiteral); ok {
		return lit.Value, true
	}
	// Handle negative literals: -100 is parsed as PrefixExpression("-", IntegerLiteral(100))
	if prefix, ok := expr.(*parser.PrefixExpression); ok && prefix.Operator == "-" {
		if lit, ok := prefix.Right.(*parser.IntegerLiteral); ok {
			return -lit.Value, true
		}
	}
	// CharLiteral: convert Unicode code point to int64
	if charLit, ok := expr.(*parser.CharLiteral); ok {
		runes := []rune(charLit.Value)
		if len(runes) == 1 {
			return int64(runes[0]), true
		}
	}
	return 0, false
}

// uint64FromLiteral extracts the uint64 value from an integer literal's Raw
// or Token.Literal string. This is needed for large unsigned literals (e.g.
// 18446744073709551615 = 2^64-1) whose int64 Value overflows to a negative
// number. The Raw/Token.Literal fields preserve the original source text.
func uint64FromLiteral(expr parser.Expression) (uint64, bool) {
	if lit, ok := expr.(*parser.IntegerLiteral); ok {
		raw := lit.Raw
		if raw == "" {
			raw = lit.Token.Literal
		}
		if raw != "" {
			if uval, err := strconv.ParseUint(raw, 10, 64); err == nil {
				return uval, true
			}
		}
		// Fall back to the int64 value reinterpreted as uint64.
		return uint64(lit.Value), true
	}
	return 0, false
}
func checkCallArgsInExpr(expr parser.Expression, sigs map[string]*funcSig, varTypes map[string]string, structFields map[string]map[string]string) []ValidateResult {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		var results []ValidateResult
		if ident, ok := e.Function.(*parser.Identifier); ok {
			// Skip type checking for built-in option constructors err(x) and ok(x).
			// These are language keywords that accept any type (str for error
			// messages, or any value for ok wrapping). Without this skip, when
			// the std library module io.no defines a function named `err` (writing
			// to stderr), the merged-module vet would incorrectly type-check
			// `err(it)` in match arms where `it` is an `err` variant, producing
			// false positives like "expected 'str', got 'err'".
			if ident.Value == "err" || ident.Value == "ok" {
				// Still recurse into arguments for nested call checking
				for _, arg := range e.Arguments {
					results = append(results, checkCallArgsInExpr(arg, sigs, varTypes, structFields)...)
				}
				return results
			}
			if sig, ok := sigs[ident.Value]; ok {
				// Check argument count (allow fewer args when default values exist)
				minArgs := len(sig.ParamTypes)
				// Method calls (e.g. ec.init(c, key) rewritten to net.enc-conn.init(c, key))
				// have an implicit self receiver as the first parameter in the signature,
				// but it is NOT in expr.Arguments. Subtract 1 for the implicit self.
				if len(sig.ParamTypes) > 0 && sig.ParamTypes[0].Name == "self" {
					minArgs--
				}
				for j := len(sig.ParamTypes) - 1; j >= 0; j-- {
					if sig.ParamTypes[j].HasDefault {
						minArgs = j
					} else {
						break
					}
				}
				if len(e.Arguments) > len(sig.ParamTypes) {
					// More args than params: last arg might be output param, check up to param count
				} else if len(e.Arguments) < minArgs {
					results = append(results, ValidateResult{
						TraceID: "haahhq7o",
						Line:    e.Token.Line,
						Column:  e.Token.Column,
						Message: fmt.Sprintf("function '%s' expects at least %d argument(s), got %d", ident.Value, minArgs, len(e.Arguments)),
					})
				} else {
					// Check argument types using resolveExprType (handles struct fields, arrays, etc.)
					for i, arg := range e.Arguments {
						if i >= len(sig.ParamTypes) {
							break
						}
						argType := resolveExprType(arg, varTypes, structFields)
						expectedType := sig.ParamTypes[i].Type
						if expectedType != "" && argType != "" && !isArgTypeCompatible(expectedType, argType, arg) {
							results = append(results, ValidateResult{
								TraceID: "6fgg3htw",
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
			results = append(results, checkCallArgsInExpr(arg, sigs, varTypes, structFields)...)
		}
		return results
	case *parser.InfixExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkCallArgsInExpr(e.Left, sigs, varTypes, structFields)...)
		}
		if e.Right != nil {
			results = append(results, checkCallArgsInExpr(e.Right, sigs, varTypes, structFields)...)
		}
		return results
	case *parser.PrefixExpression:
		if e.Right != nil {
			return checkCallArgsInExpr(e.Right, sigs, varTypes, structFields)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			return checkCallArgsInExpr(e.Expression, sigs, varTypes, structFields)
		}
	case *parser.IfExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkCallArgsInExpr(e.Condition, sigs, varTypes, structFields)...)
		}
		if e.Consequence != nil {
			for _, is := range e.Consequence.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs, varTypes, structFields)...)
			}
		}
		if e.Alternative != nil {
			for _, is := range e.Alternative.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs, varTypes, structFields)...)
			}
		}
		return results
	case *parser.IndexExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkCallArgsInExpr(e.Left, sigs, varTypes, structFields)...)
		}
		if e.Index != nil {
			results = append(results, checkCallArgsInExpr(e.Index, sigs, varTypes, structFields)...)
		}
		return results
	case *parser.AssignExpression:
		if e.Value != nil {
			return checkCallArgsInExpr(e.Value, sigs, varTypes, structFields)
		}
	case *parser.ConditionalExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkCallArgsInExpr(e.Condition, sigs, varTypes, structFields)...)
		}
		if e.Consequence != nil {
			results = append(results, checkCallArgsInExpr(e.Consequence, sigs, varTypes, structFields)...)
		}
		if e.Alternative != nil {
			results = append(results, checkCallArgsInExpr(e.Alternative, sigs, varTypes, structFields)...)
		}
		return results
	case *parser.ArrayLiteral:
		var results []ValidateResult
		for _, elem := range e.Elements {
			results = append(results, checkCallArgsInExpr(elem, sigs, varTypes, structFields)...)
		}
		return results
	case *parser.SliceLiteral:
		var results []ValidateResult
		for _, elem := range e.Elements {
			results = append(results, checkCallArgsInExpr(elem, sigs, varTypes, structFields)...)
		}
		return results
	case *parser.StructLiteral:
		var results []ValidateResult
		for _, f := range e.Fields {
			if f.Value != nil {
				results = append(results, checkCallArgsInExpr(f.Value, sigs, varTypes, structFields)...)
			}
		}
		return results
	case *parser.FunctionLiteral:
		if e.Body != nil {
			var results []ValidateResult
			for _, is := range e.Body.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs, varTypes, structFields)...)
			}
			return results
		}
	}
	return nil
}

// filterByExports filters a module's program to only include exported items declared in lib.no.
// It also auto-exports structs/enums/interfaces referenced by exported functions.
func filterByExports(prog *parser.Program, libPath string, modFilePath string) *parser.Program {
	libSource, err := os.ReadFile(libPath)
	if err != nil {
		return prog
	}
	l := lexer.NewCached(libPath, string(libSource))
	p := parser.New(l)
	libProg := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return prog
	}
	type exportEntry struct {
		path  string
		fn    string
		alias string
	}
	var exports []exportEntry
	for _, stmt := range libProg.Statements {
		if es, ok := stmt.(*parser.ExportStatement); ok {
			exports = append(exports, exportEntry{
				path:  es.Path,
				fn:    es.Function,
				alias: es.Alias,
			})
		}
	}
	if len(exports) == 0 {
		return &parser.Program{Statements: []parser.Statement{}}
	}
	// Build set of all defined names in the module
	defined := make(map[string]bool)
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			defined[s.Name] = true
		case *parser.StructDefinition:
			defined[s.Name] = true
		case *parser.EnumDefinition:
			defined[s.Name] = true
		case *parser.TaggedEnumDefinition:
			defined[s.Name] = true
		case *parser.InterfaceDefinition:
			defined[s.Name] = true
		case *parser.LetStatement:
			if s.Name != nil {
				defined[s.Name.Value] = true
			}
		}
	}
	// Validate that all exported names exist in the module.
	// Skip validation when the program has no function definitions (e.g. lib.no
	// itself only contains ExportStatement + UseStatement); the actual definitions
	// will be loaded from the UseStatements' target modules.
	hasFuncDefs := false
	for _, stmt := range prog.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			hasFuncDefs = true
			break
		}
	}
	if hasFuncDefs {
		// Compute the module's relative path (without .no extension) from the
		// file path and lib.no location. Only exports whose Path matches this
		// module are validated, avoiding false positives from other modules'
		// exports in the same lib.no file.
		modRelPath := ""
		if modFilePath != "" {
			pkgRoot := filepath.Dir(libPath)
			rel, err := filepath.Rel(pkgRoot, modFilePath)
			if err == nil {
				modRelPath = strings.TrimSuffix(rel, ".no")
			}
		}
		for _, e := range exports {
			// Skip validation for exports that don't belong to this module.
			if modRelPath != "" {
				if !strings.HasSuffix(e.path, "/"+modRelPath) && e.path != "/"+modRelPath && e.path != modRelPath {
					continue
				}
			}
			if !defined[e.fn] {
				fmt.Fprintf(os.Stderr, "warning: export references undefined symbol %q (not found in module)\n", e.fn)
			}
		}
	}
	exported := make(map[string]string)
	for _, e := range exports {
		name := e.fn
		if e.alias != "" {
			exported[name] = e.alias
		} else {
			exported[name] = ""
		}
	}
	// Collect type definitions in this module
	structs := make(map[string]bool)
	enums := make(map[string]bool)
	interfaces := make(map[string]bool)
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.StructDefinition:
			structs[s.Name] = true
		case *parser.EnumDefinition:
			enums[s.Name] = true
		case *parser.InterfaceDefinition:
			interfaces[s.Name] = true
		case *parser.TaggedEnumDefinition:
			enums[s.Name] = true
		}
	}
	// Auto-export types referenced by exported functions
	var collectTypes func(t parser.Type, result map[string]bool)
	collectTypes = func(t parser.Type, result map[string]bool) {
		if t == nil {
			return
		}
		switch tt := t.(type) {
		case *parser.NamedType:
			if structs[tt.Value] || enums[tt.Value] || interfaces[tt.Value] {
				result[tt.Value] = true
			}
		case *parser.ArrayType:
			collectTypes(tt.Elem, result)
		case *parser.SliceType:
			collectTypes(tt.Elem, result)
		case *parser.PointerType:
			collectTypes(tt.Type, result)
		case *parser.NullableType:
			collectTypes(tt.Type, result)
		}
	}
	autoResult := make(map[string]bool)
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if _, isExported := exported[fd.Name]; !isExported {
				continue
			}
			for _, param := range fd.Parameters {
				collectTypes(param.Type, autoResult)
			}
			for _, result := range fd.Results {
				collectTypes(result.Type, autoResult)
			}
		}
	}
	for name := range autoResult {
		exported[name] = ""
	}

	// Track call dependencies: if an exported function calls another function
	// defined in the same module, that function must also be kept (transitively).
	// Without this, non-exported helper functions called by exported functions
	// would be filtered out, leaving dangling call sites that cause linker errors.
	funcDefs := make(map[string]*parser.FunctionDefinition)
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			funcDefs[fd.Name] = fd
		}
	}
	calledFuncs := make(map[string]bool)
	var collectCalledFuncs func(body *parser.BlockStatement)
	var walkExpr func(expr parser.Expression)
	var walkStmts func(stmts []parser.Statement)
	walkExpr = func(expr parser.Expression) {
		if expr == nil {
			return
		}
		switch e := expr.(type) {
		case *parser.CallExpression:
			if ident, ok := e.Function.(*parser.Identifier); ok {
				if defined[ident.Value] {
					calledFuncs[ident.Value] = true
				}
			}
			walkExpr(e.Function)
			for _, arg := range e.Arguments {
				walkExpr(arg)
			}
		case *parser.InfixExpression:
			walkExpr(e.Left)
			walkExpr(e.Right)
		case *parser.PrefixExpression:
			walkExpr(e.Right)
		case *parser.DotExpression:
			walkExpr(e.Receiver)
		case *parser.IndexExpression:
			walkExpr(e.Left)
			walkExpr(e.Index)
		case *parser.SliceExpression:
			walkExpr(e.Left)
		case *parser.GroupedExpression:
			walkExpr(e.Expression)
		case *parser.FunctionLiteral:
			if e.Body != nil {
				walkStmts(e.Body.Statements)
			}
		case *parser.IfExpression:
			walkExpr(e.Condition)
			if e.Consequence != nil {
				walkStmts(e.Consequence.Statements)
			}
			if e.Alternative != nil {
				walkStmts(e.Alternative.Statements)
			}
		case *parser.ArrayLiteral:
			for _, elem := range e.Elements {
				walkExpr(elem)
			}
		case *parser.SliceLiteral:
			for _, elem := range e.Elements {
				walkExpr(elem)
			}
		case *parser.StructLiteral:
			for _, f := range e.Fields {
				walkExpr(f.Value)
			}
		case *parser.AssignExpression:
			walkExpr(e.Left)
			walkExpr(e.Value)
		case *parser.ConditionalExpression:
			walkExpr(e.Condition)
			walkExpr(e.Consequence)
			walkExpr(e.Alternative)
		case *parser.CastExpression:
			walkExpr(e.Expr)
		case *parser.AwaitExpression:
			walkExpr(e.Right)
		}
	}
	walkStmts = func(stmts []parser.Statement) {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			switch s := stmt.(type) {
			case *parser.LetStatement:
				walkExpr(s.Value)
			case *parser.ExpressionStatement:
				walkExpr(s.Expression)
			case *parser.MultiAssignStatement:
				walkExpr(s.Value)
			case *parser.ReturnStatement:
				walkExpr(s.ReturnValue)
			case *parser.ForStatement:
				walkExpr(s.Condition)
				if s.Body != nil {
					walkStmts(s.Body.Statements)
				}
				if s.IterRange != nil {
					if s.IterRange.Range != nil {
						walkExpr(s.IterRange.Range.Start)
						walkExpr(s.IterRange.Range.End)
					}
					walkExpr(s.IterRange.RangeExpr)
				}
				walkExpr(s.CountExpr)
			}
		}
	}
	collectCalledFuncs = func(body *parser.BlockStatement) {
		if body == nil {
			return
		}
		walkStmts(body.Statements)
	}
	// Iteratively expand the keep set with call dependencies (transitive closure)
	worklist := make([]string, 0, len(exported))
	for name := range exported {
		worklist = append(worklist, name)
	}
	for len(worklist) > 0 {
		name := worklist[0]
		worklist = worklist[1:]
		fd, ok := funcDefs[name]
		if !ok {
			continue
		}
		prevLen := len(calledFuncs)
		collectCalledFuncs(fd.Body)
		// Add newly discovered called functions to worklist and exported set
		if len(calledFuncs) > prevLen {
			for called := range calledFuncs {
				if _, alreadyKept := exported[called]; !alreadyKept {
					exported[called] = ""
					worklist = append(worklist, called)
				}
			}
		}
	}
	// Filter statements
	filtered := &parser.Program{Statements: []parser.Statement{}}
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.UseStatement:
			// Always keep UseStatements so that transitive imports are processed.
			// Without this, a lib.no's `# /path/to/impl` would be filtered out,
			// preventing the actual function definitions from being loaded.
			filtered.Statements = append(filtered.Statements, s)
		case *parser.FunctionDefinition:
			if _, ok := exported[s.Name]; ok {
				// Do NOT rename s.Name to the export alias here.
				// filterByExports may be called multiple times on the same
				// cached Program (preload + merge), and mutating s.Name
				// would cause subsequent calls to fail to find the original
				// function name. The merge step in the transpiler handles
				// renaming via the import alias (use.Alias).
				filtered.Statements = append(filtered.Statements, s)
			}
		case *parser.StructDefinition:
			if _, ok := exported[s.Name]; ok {
				filtered.Statements = append(filtered.Statements, s)
			}
		case *parser.EnumDefinition:
			if _, ok := exported[s.Name]; ok {
				filtered.Statements = append(filtered.Statements, s)
			}
		case *parser.TaggedEnumDefinition:
			if _, ok := exported[s.Name]; ok {
				filtered.Statements = append(filtered.Statements, s)
			}
		case *parser.InterfaceDefinition:
			if _, ok := exported[s.Name]; ok {
				filtered.Statements = append(filtered.Statements, s)
			}
		case *parser.LetStatement:
			if s.Name != nil {
				if _, ok := exported[s.Name.Value]; ok {
					filtered.Statements = append(filtered.Statements, s)
				}
			}
		}
	}
	return filtered
}

// lookupReturnCount determines how many values a call expression returns.
// It checks user-defined signatures (sigs), built-in functions, and std
// module signatures. Returns -1 if the return count cannot be determined
// (in which case the caller should skip the count check).
func lookupReturnCount(callExpr *parser.CallExpression, sigs map[string]*funcSig, varTypes map[string]string) int {
	// Direct function call: foo(args)
	if ident, ok := callExpr.Function.(*parser.Identifier); ok {
		name := ident.Value
		// 1. User-defined function
		if sig, ok := sigs[name]; ok {
			return len(sig.ResultTypes)
		}
		// 2. Built-in function
		if bi := builtin.FindBuiltinMethod(name); bi != nil {
			return len(bi.Return)
		}
		// 3. Std module function
		stdSigs, _ := CollectStdModuleSignatures()
		if rets, ok := stdSigs[name]; ok {
			return len(rets)
		}
		return -1
	}

	// Method call: receiver.method(args)
	if dot, ok := callExpr.Function.(*parser.DotExpression); ok {
		methodName := dot.Property
		// Determine receiver type to construct lookup key (e.g. "str.split")
		if recv, ok := dot.Receiver.(*parser.Identifier); ok {
			// If receiver is a known variable, use its type
			if varType, isVar := varTypes[recv.Value]; isVar {
				// Strip generic/array prefix to get base type name
				// e.g. "[]u8" → "slice", "str" → "str", "i64" → "i64"
				baseType := baseTypeNameForMethod(varType)
				fullName := baseType + "." + methodName
				// 1. User-defined method
				if sig, ok := sigs[fullName]; ok {
					return len(sig.ResultTypes)
				}
				// 2. Built-in method
				if bi := builtin.FindBuiltinMethod(fullName); bi != nil {
					return len(bi.Return)
				}
				// 3. Std module method
				stdSigs, _ := CollectStdModuleSignatures()
				if rets, ok := stdSigs[fullName]; ok {
					return len(rets)
				}
			} else {
				// Receiver is a module name (e.g. math.sin, os.stat-size, dep.resolve)
				// 0. User-defined function with module prefix (e.g. "dep.resolve")
				//    Must be checked BEFORE std sigs to avoid false positives when
				//    a user function name collides with a std bare name.
				fullName := recv.Value + "." + methodName
				if sig, ok := sigs[fullName]; ok {
					return len(sig.ResultTypes)
				}
				// 1. Look up by bare method name in builtins
				if bi := builtin.FindBuiltinMethod(methodName); bi != nil {
					return len(bi.Return)
				}
				stdSigs, _ := CollectStdModuleSignatures()
				if rets, ok := stdSigs[methodName]; ok {
					return len(rets)
				}
				// Also try module.method form
				if rets, ok := stdSigs[fullName]; ok {
					return len(rets)
				}
			}
		}
		return -1
	}

	return -1
}

// baseTypeNameForMethod extracts the base type name used for method lookup.
// e.g. "str" → "str", "i64" → "i64", "[]u8" → "slice", "[64]i64" → "array"
func baseTypeNameForMethod(typeStr string) string {
	typeStr = strings.TrimPrefix(typeStr, "?")
	if strings.HasPrefix(typeStr, "[]") {
		return "slice"
	}
	if strings.HasPrefix(typeStr, "[") && strings.Contains(typeStr, "]") {
		return "array"
	}
	return typeStr
}

// checkerKeyCategory 根據鍵型別名稱回傳其分類："str" / "int" / "bool"；
// 不支援的鍵型別回傳空字串。與 build.keyCategory 邏輯一致。
func checkerKeyCategory(keyType string) string {
	switch keyType {
	case "str":
		return "str"
	case "bool":
		return "bool"
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128":
		return "int"
	default:
		return ""
	}
}

// checkerMapTypeToHashmapName 將 map 型別字串（如 "[str]i64"）轉換為特化結構名稱
// （如 "hashmap-str-i64"）。若輸入不是 map 型別，回傳空字串。
// 與 build.mapTypeToHashmapName 邏輯一致，供 checker 包獨立使用（避免循環依賴）。
func checkerMapTypeToHashmapName(mapType string) string {
	if !strings.HasPrefix(mapType, "[") {
		return ""
	}
	closeBracket := strings.IndexByte(mapType, ']')
	if closeBracket <= 1 {
		return ""
	}
	keyType := mapType[1:closeBracket]
	if checkerKeyCategory(keyType) == "" {
		return ""
	}
	valueType := mapType[closeBracket+1:]
	return "hashmap-" + keyType + "-" + parser.SanitizeLLVMTypeName(valueType)
}
