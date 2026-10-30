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
	"sync"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
	pkg "github.com/lizongying/nolang/package"
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

// validationCurModule holds the std module ShortName that owns the top-level
// statement currently being walked by ValidateFuncArgs (set per statement in
// the loop below, before the recursive argument walk begins). It lets the
// per-call-site private-builtin check (checkPrivateBuiltinMethodCall) attribute
// a method call to its enclosing module WITHOUT threading the module through
// every recursive checkCallArgs* signature.
//
// Safe as package state because ValidateFuncArgs is the sole driver of this
// walk and is serialized by validationMu; the recursive walk of one statement
// completes (synchronously) before the loop advances to the next statement, so
// validationCurModule is always correct for the call site being checked.
var validationCurModule string

// isConcreteType 檢查型別名稱是否為已知具體型別
func isConcreteType(typeName string) bool {
	switch typeName {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128",
		"byte", "f32", "f64", "str", "txt", "bool", "char", "void":
		return true
	}
	// 複合型別：切片、陣列、可空、指針、view（借用）
	if ty.IsContainerRaw(typeName) ||
		ty.IsOptionRaw(typeName) || ty.IsPtrRaw(typeName) ||
		ty.IsViewRaw(typeName) {
		return true
	}
	// 已註冊的單具體型別別名（如 fd）視為具體型別，不再跳過型別檢查。
	// 別名由 Resolver 提供，故 `fs.fd` 這種帶前綴的拼寫一併命中。
	if checkerType(typeName).Underlying() != nil {
		return true
	}
	return false
}

// arithmeticOps are infix operators whose result is option-bearing in nolang
// (arithmetic/bitwise operations can raise overflow, modeled as ?T). Comparison
// and logical operators produce bool and are intentionally excluded.
var arithmeticOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"&": true, "|": true, "^": true, "<<": true, ">>": true,
}

// isArithmeticExpr reports whether expr is a bare arithmetic/bitwise expression
// (option-producing). Grouped expressions are unwrapped; prefix negation (`-x`)
// and bitwise-not (`~x`) are also treated as arithmetic.
func isArithmeticExpr(expr parser.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *parser.GroupedExpression:
		return isArithmeticExpr(e.Expression)
	case *parser.InfixExpression:
		return arithmeticOps[e.Operator]
	case *parser.PrefixExpression:
		return e.Operator == "-" || e.Operator == "~"
	}
	return false
}

// funcSigReturnsOption reports whether a function signature has at least one
// result parameter of option type (?T). Such functions must have their result
// (or, per the Phase-2 rule, their bare arithmetic arguments) unwrapped with `?=`.
func funcSigReturnsOption(sig *funcSig) bool {
	if sig == nil {
		return false
	}
	for _, r := range sig.ResultTypes {
		if ty.IsOptionRaw(r.Type) {
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

// validationUnionTypeAliases holds union type alias name → alias AST
// (e.g. "num" → `int | float`, "string" → `str | txt`), populated by
// ValidateTypes from the current program. Used by isArgTypeCompatible to
// accept a concrete member (or any type compatible with a member) where the
// union alias is expected. Nested members are flattened recursively via
// FlattenUnion, which also resolves members that are themselves aliases.
// Std-module aliases are merged in via CollectStdUnionAliases (sync.Once),
// so cross-module calls with union-alias parameters validate correctly.
var validationUnionTypeAliases map[string]*parser.TypeAlias

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
	case *parser.BlockStatement:
		// 入口程序頂層 `{ ... }` 區塊（如 `no run` 的腳本式入口）也是一條
		// 頂層陳述句，必須遞迴其內部陳述，否則塊內對 std 函式（如
		// sha1.sha1-hex() 漏參）的呼叫會被合併後的數量檢查漏掉——完整型別
		// 檢查 checkCallArgsInStmtWithResultParams 已有此分支，此處對齊。
		var results []ValidateResult
		for _, bs := range s.Statements {
			results = append(results, checkArgCountInStmt(bs, sigs)...)
		}
		return results
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
				// Allow more args than params (could be output param binding or
				// method-call receiver rewritten to type.method(receiver, ...)).
				// This runs on the MERGED program where method calls have already
				// been rewritten, so extra args are expected and legitimate.
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
	case *parser.SurfaceMatch:
		var results []ValidateResult
		// S4：糖形態下 match 是 SurfaceMatch，臂體對本 walker 不可見。見 sugar_walk.go。
		walkSurfaceMatchChildren(e, nil,
			func(x parser.Expression) { results = append(results, checkArgCountInExpr(x, sigs)...) },
			func(s parser.Statement) { results = append(results, checkArgCountInStmt(s, sigs)...) })
		return results
	}
	return nil
}

// ValidateFuncArgs 是旧版入口（view=nil）；THIR 侧变体见 ValidateFuncArgsTHIR。
func ValidateFuncArgs(program *parser.Program, rootDir string) []ValidateResult {
	return runFuncArgsCore(program, rootDir, nil)
}

// runFuncArgsCore 是 ValidateFuncArgs / THIR 两变体共享的收集 + 走查主体。
// view 作用面：
//   - sigs 签名表：view 非 nil 时 FuncSigs（HIR 提取，与 funcSigFromDef 逐字段
//     对齐——见 thir_stage.go FuncSigs 注释）优先，AST/导入收集只补缺（导入模块
//     是独立 parse 树、不在 HIR，必须兜底）；
//   - topLevelVarTypes 与 walker 内声明登记值（let / param / result）经 declTypeOf。
//
// 影子期刻意保留 AST 的面：
//   - structFields：legacy 值是 structFieldTypeString（折叠 ArraySize/IsSlice
//     修饰），而 TypeOf 回裸 .Type 字段（Inferred 优先）——口径分叉、换源会改
//     值（`[]byte` vs `byte`），禁；
//   - 推断面（inferExprType / funcSigFirstReturnType 回填）保持原路径——sigs
//     值等价前提下行为不变，且登记只进本地 varTypes（不写 sem 副表）。
func runFuncArgsCore(program *parser.Program, rootDir string, view *THIRView) []ValidateResult {
	validationMu.Lock()
	defer validationMu.Unlock()
	var results []ValidateResult
	// 確保 union 別名表可用（isArgTypeCompatible 的 union 分支依賴它）。
	// ValidateTypes 正常會先填充；此處冪等重填以防單獨調用本函數。
	if validationUnionTypeAliases == nil {
		validationUnionTypeAliases, _ = ValidateUnionTypes(program)
		for k, v := range CollectStdUnionAliases() {
			if _, exists := validationUnionTypeAliases[k]; !exists {
				validationUnionTypeAliases[k] = v
			}
		}
	}
	// 1. Collect local function signatures (including from resolved imports
	//    which are already merged into the program at build time)
	sigs := make(map[string]*funcSig)
	if view != nil {
		for k, s := range view.FuncSigs() {
			sigs[k] = s
		}
	}
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if _, exists := sigs[fd.Name]; exists && view != nil {
				continue // THIR 侧 FuncSigs 已收录（HIR 优先）
			}
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
	// 3. Collect struct field type info from struct definitions.
	//    刻意不走 declTypeOf：legacy 值 structFieldTypeString 折叠了
	//    ArraySize/IsSlice 修饰（`[]byte`），TypeOf 回裸 .Type（`byte`）——
	//    口径分叉，影子期保留 AST 来源（见 runFuncArgsCore 注释）。
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
					topLevelVarTypes[ls.Name.Value] = declTypeOfRaw(view, ls, ls.Type.String())
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
		// Attribute the statement to its owning std module so the private
		// builtin-method check can reject underscore-prefixed str/txt field
		// accesses called from outside their own module.
		validationCurModule = stdModuleForSourceFile(program.Sem.SourceFileOf(stmt))
		results = append(results, checkCallArgsInStmt(stmt, sigs, topLevelVarTypes, structFields, view)...)
	}
	return results
}

// resolveUseModule resolves a UseStatement to its module program.
// It handles local paths (/path), std paths, and dependency paths (domain/...).
// rootDir is the directory of the entry source file, used as a fallback
// for workspace root resolution when pkg is nil (no package.noml found).
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
	// Tier 1: on-disk content-addressed AST cache. This standalone parse injects
	// no cross-file signatures, so the only non-source parse input is the std
	// signature key — fold it into the salt. The rebuild config is bare for the
	// same reason: the fresh parse below leaves Filename and the extern tables
	// empty, and the blob's semantic views must be regenerated under exactly
	// those inputs or the rebuilt AST would differ from the parsed one.
	if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
		dkey := dc.KeyFor(string(source), StdSigKey)
		if cached, ok := dc.Get(dkey, parser.SemRebuildConfig{}); ok {
			parseProgramFileCache.Put(ck, cached)
			return cached
		}
	}
	l := lexer.NewCached(filePath, string(source))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil
	}
	parseProgramFileCache.Put(ck, prog)
	// Tier 1: store for the next process (LSP restart / next build).
	if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
		dc.Put(dc.KeyFor(string(source), StdSigKey), prog)
	}
	return prog
}

func checkCallArgsInStmt(stmt parser.Statement, sigs map[string]*funcSig, varTypes map[string]string, structFields map[string]map[string]string, view *THIRView) []ValidateResult {
	return checkCallArgsInStmtWithResultParams(stmt, sigs, varTypes, nil, structFields, false, view)
}

// checkCallArgsInIfExpr descends into an IfExpression's branches.
func checkCallArgsInIfExpr(s *parser.IfExpression, sigs map[string]*funcSig, varTypes map[string]string, resultParamNames map[string]bool, structFields map[string]map[string]string, underUnwrap bool, view *THIRView) []ValidateResult {
	var results []ValidateResult
	if s.Condition != nil {
		results = append(results, checkCallArgsInExpr(s.Condition, sigs, varTypes, structFields, underUnwrap, view)...)
	}
	if s.Consequence != nil {
		for _, bs := range s.Consequence.Statements {
			results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
		}
	}
	if s.Alternative != nil {
		for _, bs := range s.Alternative.Statements {
			results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
		}
	}
	return results
}
func checkCallArgsInStmtWithResultParams(stmt parser.Statement, sigs map[string]*funcSig, varTypes map[string]string, resultParamNames map[string]bool, structFields map[string]map[string]string, underUnwrap bool, view *THIRView) []ValidateResult {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			// If-as-statement: ExpressionStatement wraps an IfExpression.
			// Descend into both branches so result-param types propagate.
			if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
				return checkCallArgsInIfExpr(ifExpr, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)
			}
			// S1：糖形態 match 是 SurfaceMatch；逐臂條件/臂體下鑽 call-arg 檢查。
			if sm, ok := s.Expression.(*parser.SurfaceMatch); ok {
				var res []ValidateResult
				for _, arm := range sm.Arms {
					if arm.Condition != nil {
						res = append(res, checkCallArgsInExpr(arm.Condition, sigs, varTypes, structFields, underUnwrap, view)...)
					}
					if arm.Body != nil {
						for _, bs := range arm.Body.Statements {
							res = append(res, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
						}
					}
				}
				return res
			}
			return checkCallArgsInExpr(s.Expression, sigs, varTypes, structFields, underUnwrap, view)
		}
	case *parser.LetStatement:
		// Compiler-injected synthetic let statements (e.g. match arm `it` bindings).
		// If the synthetic binding has an explicit Type, record it directly.
		// If not (fallback path when parser couldn't resolve the matched variable's
		// type at parse time), infer from value but unwrap Optional types (?T → T),
		// because `it` in a match arm always represents the unwrapped inner value.
		if s.IsSynthetic {
			if s.Value != nil {
				results := checkCallArgsInExpr(s.Value, sigs, varTypes, structFields, underUnwrap, view)
				// Compiler-generated `target = __cap_N` propagation assignment on the
				// nil/err arm of a safe-index / `?=` capture (lowering.go:1230-1235).
				// It copies the WHOLE option into the target, so the value's type is
				// `?T` while the target's real type is whatever the ok arm assigns —
				// recording the unwrapped `T` here (the branch below) poisons
				// varTypes: the ok arm's real let is then skipped by the
				// `!exists` guard, and every later use of the target resolves to the
				// wrong type (`xs str = shs-sub(ln, i + 1, rb)` → varTypes["xs"]=="i64"
				// → spurious 6fgg3htw on `shs-byte(xs, k)`).
				// checker.go:6830 (str-ordering) and :8766 (type-compat) already skip
				// IsPropagation lets; do the same here so the ok arm's let wins.
				if s.Name != nil && !resultParamNames[s.Name.Value] && !s.IsPropagation {
					if s.Type != nil && s.Type.String() != "" {
						varTypes[s.Name.Value] = declTypeOfRaw(view, s, s.Type.String())
					} else {
						// Untyped shared `it` binding (ok/wildcard arm whose matched
						// type is unknown at parse time). Arms share one varTypes map,
						// so a preceding sentinel (nil/err) arm's typed `it` binding may
						// have recorded "err"/"nil" into varTypes["it"]; that stale type
						// must NOT leak into this arm, or `x = it` here infers the wrong
						// type (spurious "expected str, got err", trace 6fgg3htw). Always
						// re-derive the unwrapped payload from the matched value; if it
						// cannot be resolved, clear `it` so it stays unknown - mirroring
						// the equivalent guard in ValidateTypes (trace 15w45dqk).
						inferred := inferExprType(s.Value, varTypes, nil, "")
						if ty.IsOptionRaw(inferred) {
							inferred = ty.StripOptionRaw(inferred)
						}
						if inferred != "" {
							varTypes[s.Name.Value] = inferred
						} else if s.Name.Value == "it" {
							delete(varTypes, "it")
						}
					}
				}
				return results
			}
			if s.Name != nil && s.Type != nil {
				varTypes[s.Name.Value] = declTypeOfRaw(view, s, s.Type.String())
			}
			return nil
		}
		if s.Value != nil {
			// A `?=` unwrap desugars to `__unwrap_N = <expr>`; calls within that
			// RHS are exempt from the bare-arithmetic-argument rule because the
			// `?=` itself is the required explicit unwrap.
			uw := underUnwrap
			if s.Name != nil && strings.HasPrefix(s.Name.Value, "__unwrap_") {
				uw = true
			}
			results := checkCallArgsInExpr(s.Value, sigs, varTypes, structFields, uw, view)
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
					varTypes[s.Name.Value] = declTypeOfRaw(view, s, s.Type.String())
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
			varTypes[s.Name.Value] = declTypeOfRaw(view, s, s.Type.String())
		}
	case *parser.MultiAssignStatement:
		var results []ValidateResult
		if s.Value != nil {
			results = append(results, checkCallArgsInExpr(s.Value, sigs, varTypes, structFields, underUnwrap, view)...)
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
				localTypes[p.Name] = declTypeOfRaw(view, p, p.Type.String())
			}
		}
		for _, r := range s.Results {
			if r.Name != "" && r.Type != nil {
				rt := r.Type.String()
				// 運算符重載方法（_add 等）書寫為 `?T`，但編譯器視作 payload T：
				// 結果參數的生效型別去掉 `?`，使方法體內 `c = <T>` 合法、
				// 不觸發 option 包裝／解包，對齊內建數值型別（int.div 先例）。
				if s.IsMethodDef && parser.IsOperatorOverloadName(parser.OperatorOverloadBaseName(s.Name)) {
					rt = ty.StripOptionRaw(rt)
				}
				localTypes[r.Name] = declTypeOfRaw(view, r, rt)
				innerResultParams[r.Name] = true
			}
		}
		if s.Body != nil {
			var results []ValidateResult
			for _, bs := range s.Body.Statements {
				results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, localTypes, innerResultParams, structFields, false, view)...)
			}
			return results
		}
	case *parser.BlockStatement:
		var results []ValidateResult
		for _, bs := range s.Statements {
			results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.ForStatement:
		var results []ValidateResult
		if s.Init != nil {
			results = append(results, checkCallArgsInStmtWithResultParams(s.Init, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
		}
		if s.Condition != nil {
			results = append(results, checkCallArgsInExpr(s.Condition, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		if s.Update != nil {
			results = append(results, checkCallArgsInStmtWithResultParams(s.Update, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				results = append(results, checkCallArgsInStmtWithResultParams(bs, sigs, varTypes, resultParamNames, structFields, underUnwrap, view)...)
			}
		}
		return results
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return checkCallArgsInExpr(s.ReturnValue, sigs, varTypes, structFields, underUnwrap, view)
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
		// str 下标返回 char（2026-09-06，应需求方要求：str[i] 返回字符而非字节）。
		// txt 自 code-point 索引化后与 str 相同：`t[i]` 返回码点 char，写值亦以
		// 码点计（raw byte 存取改用 .byte / set-byte 内建）。
		// 值仍走编译器统一的 i64 整数表示（char 在代码生成层以 i64 传递，与 .eq/.cmp
		// 等对比一致），仅类型层面标注为 char，使字符方法分发与 `c = s[i]` 类型推断生效。
		if leftType == "str" || leftType == "txt" {
			return "char"
		}
		if ty.IsContainerRaw(leftType) {
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
			if ty.IsContainerRaw(leftType) {
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
	// 沿別名鏈解析到底層型別再取整數範圍，使整數字面量例外可正確範圍檢查。
	// 別名鏈交給 ty.Pool 的 Resolver（Canonical 走到底，且含模組前綴剝除）。
	if c := checkerCanonicalRaw(t); c != t {
		return intTypeRange(c)
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

// intToCharAssignOK 報告「把一個整數型別的運算式（i64/byte/u*/i* 等）指派給
// char 變數」是否合法。char 本質就是 Unicode 碼點（i32），用一個執行期整數
// 值（例如 buf 裡讀出的位元組、算術得到的碼點）初始化 char 變數是正當用法，
// 等價於 std 的 char-to-str(x) 對執行期值的處理，因此放行。
//
// 僅在目標為 char、來源為整數型別時回傳 true；str/bool/f64 等非整數來源仍由
// 既有型別檢查回報錯誤。char→char 不會走到這裡（型別相同，走既有捷徑）。
func intToCharAssignOK(targetType, srcType string) bool {
	if targetType != "char" {
		return false
	}
	_, _, ok := intTypeRange(srcType)
	return ok
}

// charLiteralOutOfRange 在 expr 為整數字面量（含負號字面量）時，檢查其值是否
// 落在合法 Unicode 碼點視窗 [0, 0x10FFFF] 之外。intToCharAssignOK 放行「整數值
// → char」是針對執行期值（如從 buf 讀出的碼點）；但一個寫死超出 Unicode 範圍
// 的字面量（如 1114112）是確定的邏輯錯誤，必須拒絕，否則 char 會承載一個
// 永遠無法編碼的「碼點」，在 char-to-str / UTF-8 編碼階段產生非法位元組。
func charLiteralOutOfRange(expr parser.Expression) bool {
	val, ok := integerLiteralValue(expr)
	if !ok {
		return false
	}
	return val < 0 || val > 1114111 // 0x10FFFF
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

// isIntLiteralNarrowingToDeclared 報告「整數字面量指派給已宣告整數型別的變數」
// 是否應視為合法的常數轉換（不報窄化錯誤）。
//
// 適用情境（僅限指派語句，不含呼叫實參）：
//
//	x byte = 0        ; 已在範圍內，走一般範圍檢查（此函式回傳 false）
//	x byte = -1       ; 負字面量 → 無號目標：二補數位模式轉換（0xFF == 255），放行
//	x byte = 300      ; 正值超出上界 = 真實數值溢位，仍報錯
//	x i8   = -200     ; 負值超出有號下界 = 真實數值溢位，仍報錯
//
// 語義界線：負字面量寫入無號變數是「位模式指派」（sentinel 慣用法，如 -1 表示
// 未知/全 1），截斷結果由目標寬度唯一決定；正值超出上界則是「數值溢位」，屬於
// 真實的邏輯錯誤，必須繼續報錯。
//
// 只在截斷無歧義時放行：字面量需落在目標寬度的 [-2^(w-1), 2^w-1] 視窗內
// （w = 目標位寬）；例如 byte 的視窗是 -128..255，故 -200 落在窗外仍報錯。
func isIntLiteralNarrowingToDeclared(expr parser.Expression, targetType string) bool {
	val, ok := integerLiteralValue(expr)
	if !ok {
		return false
	}
	min, max, ok := intTypeRange(targetType)
	if !ok {
		return false
	}
	// 落在目標範圍內的走既有路徑，這裡只處理越界的情形。
	if val >= min && val <= max {
		return false
	}
	// 只放行「負字面量 → 無號目標」這一檔。
	if val >= 0 || min != 0 {
		return false
	}
	w := integerBitWidth(targetType)
	if w == 0 {
		return false
	}
	if w >= 64 {
		// 任何 int64 都有唯一確定的 64 位元（或更寬）二補數位模式。
		return true
	}
	return val >= -(int64(1) << uint(w-1))
}

// isFloatLiteralToDeclared 放行「浮點常量 → 已宣告浮點變數」的常量轉換。
// 小數字面量一律推斷為 f64，但 `v f32 = 4.0`、`wn f32 = 0.0 - 8.0` 這類
// 寫法是编译器可折叠的常量表达式（与整数字面量窄化同口径），不应被当成
// 执行期 f64→f32 丢精度而拒绝。真正的 f64→f32 窄化限「f64 变量/执行期值」，
// 必须显式 .to-f32()。
func isFloatLiteralToDeclared(expr parser.Expression, targetType string) bool {
	if targetType != "f32" && targetType != "f64" {
		return false
	}
	return isConstFloatExpr(expr)
}

// isConstFloatExpr reports whether expr is a compile-time constant built only
// from float literals and float arithmetic (+ - * / % and unary -). An
// identifier (a runtime f64 value) is intentionally NOT a constant, so a real
// f64→f32 narrowing still gets rejected.
func isConstFloatExpr(expr parser.Expression) bool {
	switch e := expr.(type) {
	case *parser.FloatLiteral:
		return true
	case *parser.PrefixExpression:
		if e.Operator == "-" || e.Operator == "+" {
			return isConstFloatExpr(e.Right)
		}
		return false
	case *parser.InfixExpression:
		switch e.Operator {
		case "+", "-", "*", "/", "%":
			return isConstFloatExpr(e.Left) && isConstFloatExpr(e.Right)
		}
		return false
	}
	return false
}

// narrowingHint 回傳一個可操作的窄化修復提示。當 fromType 的值需要賦值給
// 更窄的 toType 時，提示使用者如何用位元運算安全窄化。
//
// 對無號目標型別：建議使用 mask（如 `& 4294967295`）或位移（如 `>> 32`）。
// 對有號目標型別：提示位元窄化不適用（符號位元截斷語義不明確）。
// 非整數型別或非窄化場景回傳空字串。
func narrowingHint(fromType, toType string) string {
	// char 的約束是「值域」而不是「位寬」：合法值是 Unicode 純量碼點
	// 0..=0x10FFFF（21 bit），超出該範圍的值無法表示，位元遮罩也救不回來
	// （`0x110000 & 0xFFFFF` 只會得到一個不相關的碼點）。所以這裡先用值域
	// 口徑給出提示，而不是走下面的位寬/遮罩邏輯。
	if toType == "char" {
		if _, _, ok := intTypeRange(fromType); ok {
			return "; hint: char holds a Unicode code point in 0..=0x10FFFF (1114111 is the last valid value)"
		}
	}
	// B5: f64 → f32 是唯一的隐式浮点窄化（f32 → f64 已隐式放行），
	// 会丢精度，提示显式转换 API。
	if toType == "f32" && fromType == "f64" {
		return "; hint: f64 -> f32 loses precision and must be explicit (e.g. `x.to-f32()`, std/number.no)"
	}
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
	// B5: 浮點隱式拓寬 — f32 → f64 放行。f32 的值域與精度都是 f64 的子集，
	// 拓宽无损（底层 MIR 以 double 承载所有浮点），与整数的隐式拓宽同一口径。
	// 反向 f64 → f32 刻意**不**放行：会丢精度，必须显式 .to-f32()（std/number.no，
	// 底层真实 fptrunc+fpext 往返）。
	if expectedType == "f64" && argType == "f32" {
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
	// txt 和 str 互通：txt 是固定 256 字節字串，str 字面量可賦值給 txt
	// （如 t txt = 'abc'），代碼生成器處理轉換。
	if (expectedType == "txt" && argType == "str") ||
		(expectedType == "str" && argType == "txt") {
		return true
	}
	// char → str 隱式轉換：char 是單個 Unicode 碼點，按 UTF-8 編碼成一個 str。
	// `s[i]` / `s[a..b]` / `for c <- s` 都產生 char，這個轉換讓
	// `a str = s[0]`、`f(str)` 傳 char 這類寫法可用
	// （codegen 走 @str_from_cp；見 docs/docs/lang/str.md）。
	// 反向 str → char 不開放：str 可能是多碼點，需要顯式 s[i] / to-chars()。
	if expectedType == "str" && argType == "char" {
		return true
	}
	// ?T 到 T 的隱式解包：在 ok 分支中，?T 變數已確認為有值，
	// 傳給期望 T 的參數是安全的（代碼生成器會自動取值）。
	if ty.IsOptionRaw(argType) && argType[1:] == expectedType {
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
	// 聯合別名反向相容：當 expectedType 是 union alias（如 "num"、"string"），
	// argType 是其中一個（遞迴展平後的）成員時允許。單態化管線
	// （ValidateUnionTypes + FlattenUnion + codegen clone）本就為每個成員
	// 生成特化版本，此處只是讓參數檢查與之一致。
	if validationUnionTypeAliases != nil {
		for _, name := range []string{expectedType, stripModulePrefix(expectedType)} {
			if ta, ok := validationUnionTypeAliases[name]; ok && ta != nil && ta.IsUnion() {
				for _, m := range FlattenUnion(name, validationUnionTypeAliases) {
					if nt, ok := m.(*parser.NamedType); ok {
						if nt.Value == argType || isArgTypeCompatible(nt.Value, argType, arg) {
							return true
						}
					}
				}
				break
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
	if ty.IsContainerRaw(argType) && !ty.IsSliceRaw(argType) {
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
	if ty.IsSliceRaw(argType) && ty.IsContainerRaw(expectedType) && !ty.IsSliceRaw(expectedType) {
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

// genericElemOfKey returns the type variable of a receiver-generic std method
// key, e.g. "[]t.eq" -> "t". It returns "" for every concrete key ("[]byte.index",
// "str.starts-with", "json.get-str"), which is what marks those as checkable.
//
// A type variable is a single lowercase letter; a concrete element is a real
// type name ("byte", "str", "char", "ord"), so the length test separates them.
func genericElemOfKey(key string) string {
	dot := strings.Index(key, ".")
	if dot < 0 {
		return ""
	}
	// A slice type is written `[]T` — empty brackets FOLLOWED by the element
	// (NOT `[T]`) — so the element starts at index 2 of the receiver part.
	recv := key[:dot]
	if !strings.HasPrefix(recv, "[]") {
		return ""
	}
	elem := recv[2:]
	if len(elem) != 1 || elem[0] < 'a' || elem[0] > 'z' {
		return ""
	}
	return elem
}

// mentionsTypeVar reports whether a type string still references a bare
// generic type variable — a token that is exactly one ASCII lowercase letter
// ("t", "v", "k", ...), whether bare (`t`) or wrapped in a container (`[]t`,
// `?t`, `[n]t`, `&t`). Concrete type names are always longer or contain
// non-letters, so scanning for such a standalone token separates generic
// (un-checkable, must be skipped) from concrete parameters.
func mentionsTypeVar(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLetter := c >= 'a' && c <= 'z'
		if !isLetter {
			continue
		}
		prevIdent := i > 0 && isTypeIdentByte(s[i-1])
		nextIdent := i+1 < len(s) && isTypeIdentByte(s[i+1])
		if !prevIdent && !nextIdent {
			return true
		}
	}
	return false
}

// isTypeIdentByte reports whether c can appear inside a nolang type/identifier
// name. Type names may contain '-' (e.g. "server-conn"), so it counts too.
func isTypeIdentByte(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// stdStructMethodMods indexes "struct.method" -> the set of std modules that
// define it, built from the MODULE-QUALIFIED keys ("tls.server-conn.send").
//
// Several std modules define the same bare struct name (server-conn lives in
// tls, ws and sse; conn lives in net and tls). The signature table registers a
// bare "server-conn.send" too, but it is last-wins over those modules, so its
// parameter types belong to whichever module happened to be merged last.
// Resolving a call through such a key reports the wrong expected type — a
// `str` argument flagged against the `i64` parameter of another module's
// send(). Anything with more than one owning module is therefore treated as
// unresolvable rather than guessed at.
var (
	stdStructMethodMods     map[string]map[string]bool
	stdStructMethodModsOnce sync.Once
)

func initStdStructMethodMods() {
	stdStructMethodModsOnce.Do(func() {
		stdStructMethodMods = make(map[string]map[string]bool)
		record := func(key string) {
			dot := strings.Index(key, ".")
			if dot < 0 {
				return
			}
			rest := key[dot+1:]
			// Only "module.struct.method" shape qualifies; "module.method"
			// has no second dot and is not a struct method.
			if !strings.Contains(rest, ".") {
				return
			}
			if stdStructMethodMods[rest] == nil {
				stdStructMethodMods[rest] = make(map[string]bool)
			}
			stdStructMethodMods[rest][key[:dot]] = true
		}
		CollectStdModuleSignatures() // warm the caches (sync.Once)
		for k := range stdMethodSigsCache {
			record(k)
		}
		for k := range stdSigsCache {
			record(k)
		}
	})
}

// structMethodIsAmbiguous reports whether "struct.method" is defined by more
// than one std module, which makes its bare table entry untrustworthy.
func structMethodIsAmbiguous(key string) bool {
	initStdStructMethodMods()
	return len(stdStructMethodMods[key]) > 1
}

// sliceElemOf returns the element type of an array/slice type: "[]i64" -> "i64",
// "[3]i64" -> "i64".
//
// It deliberately returns "" for anything whose element is not knowable:
//
//   - a MAP is written "[K]V" — it also starts with '[', but a non-numeric key
//     means there is no element type (and no generic slice method applies);
//   - the builtin "vec" has no element at all. THIS is the case that must not
//     be inferred: a slice read as `vec` carries no element information, so any
//     parameter check built on it would be a guess.
func sliceElemOf(t string) string {
	if strings.HasPrefix(t, "[]") {
		return t[2:]
	}
	if !strings.HasPrefix(t, "[") {
		return ""
	}
	idx := strings.Index(t, "]")
	if idx <= 1 {
		return ""
	}
	key := t[1:idx]
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return "" // not "[N]T" — a map key, so no element type
		}
	}
	return t[idx+1:]
}

// substituteTypeVar replaces every whole-token occurrence of the type variable
// `tv` in `t` with `elem`: with tv="t" and elem="i64", "t" -> "i64",
// "[]t" -> "[]i64", "?t" -> "?i64", and "txt"/"i64" are left alone.
func substituteTypeVar(t, tv, elem string) string {
	var b strings.Builder
	start := -1
	for i := 0; i <= len(t); i++ {
		if i < len(t) && isTypeIdentByte(t[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if tok := t[start:i]; tok == tv {
				b.WriteString(elem)
			} else {
				b.WriteString(tok)
			}
			start = -1
		}
		if i < len(t) {
			b.WriteByte(t[i])
		}
	}
	return b.String()
}

// stdMethodParamTypes resolves the declared parameter types of a std call
// `recv.method(...)`. It returns the parameter list, the key it was found
// under (the key decides whether a parameter is still generic), and whether the
// types came from the builtin registry rather than a std declaration.
//
// `storage` matters because a builtin-only method has no declared signature:
// its parameter types describe the STORAGE SHAPE the codegen writes, so an
// integer argument is simply narrowed/widened to the element stride and is not
// a type error (see builtinSliceMethodParamTypes).
func stdMethodParamTypes(recvType, method string) ([]string, string, bool) {
	if recvType == "" || method == "" {
		return nil, "", false
	}
	CollectStdModuleSignatures() // warm the caches (sync.Once)
	keys := make([]string, 0, 3)
	keys = append(keys, recvType+"."+method)
	// Receiver-generic fallback: `[]t.eq` is registered under the literal
	// element name `t`, so a concrete slice receiver (`[]i64`) resolves here.
	//
	// Must test for the TWO characters "[]": a slice is empty brackets followed
	// by the element. A map is written `[K]V`, which also starts with '[' but
	// is NOT a slice — routing `[str]i64.remove` through `[]t.remove` picked
	// up the slice index parameter (i64) and flagged the map's str KEY.
	if strings.HasPrefix(recvType, "[]") {
		keys = append(keys, "[]t."+method)
	}
	for _, key := range keys {
		if structMethodIsAmbiguous(key) {
			continue // several modules define it: parameters are a coin flip
		}
		if entry, ok := stdMethodSigsCache[key]; ok {
			return entry.Params, key, false
		}
		if entry, ok := stdSigsCache[key]; ok {
			return entry.Params, key, false
		}
	}
	params, key := builtinSliceMethodParamTypes(recvType, method)
	return params, key, len(params) > 0
}

// stdMethodOptionResult 解析 `recv.method(...)` 的**回傳型別**，且只在「確定是
// option」時回傳 `?T`，其餘一律回 ""。
//
// 🔴 為什麼只回 option：本函式的唯一呼叫者是「顯式 plain 標註 + option 呼叫」的
// 嚴格模式（checker.go 的 optionResultOfCallTarget → 15w45dqk）。把 plain 結果也
// 一併餵進去只會擴大誤報面（例如型別變數沒綁定的泛型方法、跨模組同名列），對「不
// 允許靜默拆包」這個目標沒有任何幫助。option 結果則是可判定的事實：目標是 plain
// 而值是 `?T` 時，tag 一定被丟掉。
//
// 解析順序與 stdMethodParamTypes 逐字對稱（同鍵、同跳過條件），避免「引數查得到、
// 結果查不到」的第二次不對稱：
//  1. std 簽名表（stdSigsCache / stdMethodSigsCache）；
//  2. Go builtin 表的 **str/txt 裸名回落**（`lookupBuiltin` 的 bare-name fallback：
//     `str.byte` / `txt.byte` 在 Go 表裡只以裸名 "byte" 註冊，與 std 聲明是同一
//     個內建的兩面）。
//
// 兩條「不猜」的邊界（寧可漏報，不可誤報）：
//   - 結構體方法被多個 std 模組定義（structMethodIsAmbiguous）→ 鍵不可信；
//   - 結果仍帶型別變數（泛型 `?v`，如 hashmap 的 get）→ mentionsTypeVar 擋掉。
func stdMethodOptionResult(recvType, method string) string {
	if recvType == "" || method == "" {
		return ""
	}
	// 簽名表未暖表時（`go test` 沒帶 StdSigKey 的二進位）必須直接安靜返回：
	// CollectStdModuleSignatures 在 StdSigKey 為空時會 os.Exit(1)，而 structMethodIsAmbiguous
	// → initStdStructMethodMods 也會走同一條路。
	if stdSigsCache == nil && stdMethodSigsCache == nil {
		if StdSigKey == "" {
			return ""
		}
		CollectStdModuleSignatures()
		if stdSigsCache == nil && stdMethodSigsCache == nil {
			return ""
		}
	}
	keys := make([]string, 0, 2)
	keys = append(keys, recvType+"."+method)
	if strings.HasPrefix(recvType, "[]") {
		keys = append(keys, "[]t."+method)
	}
	for _, key := range keys {
		if structMethodIsAmbiguous(key) {
			continue
		}
		if entry, ok := stdMethodSigsCache[key]; ok {
			if r := optionResultOfSigEntry(entry); r != "" {
				return r
			}
		}
		if entry, ok := stdSigsCache[key]; ok {
			if r := optionResultOfSigEntry(entry); r != "" {
				return r
			}
		}
	}
	// Go builtin 表的裸名回落：只有 str/txt 兩種接收者（與 builtin/str.go 的
	// 註釋一致），且只認 option 結果，因此不可能影響其他型別的方法解析。
	if recvType == "str" || recvType == "txt" {
		for _, m := range builtin.BuiltinMethodList {
			if m.MethodName != method || len(m.Return) == 0 {
				continue
			}
			if r := m.Return[0].String(); ty.IsOptionRaw(r) && !mentionsTypeVar(r) {
				return r
			}
		}
	}
	return ""
}

// optionResultOfSigEntry 從一條 std 簽名表條目取出「確定為 option」的第一個結果型別；
// 非 option、無結果、或仍帶型別變數（泛型）時回 ""。
func optionResultOfSigEntry(entry StdSigEntry) string {
	if len(entry.Results) == 0 {
		return ""
	}
	r := entry.Results[0]
	if !ty.IsOptionRaw(r) || mentionsTypeVar(r) {
		return ""
	}
	return r
}

// builtinSliceMethodParamTypes resolves a slice method that exists ONLY in the
// builtin registry (src/builtin/vec.go) and therefore has no std declaration
// and never reaches the signature tables.
//
// The std tables already cover every slice method that takes a value EXCEPT
// push: std/vec.no declares []t.pop / []t.insert / []t.remove / []t.truncate
// itself, and there the index-vs-element distinction is written literally
// (`[]t.insert = (i i64, val t)`), so `[]str.remove(0)` is checked as an i64
// index and passes. push is NOT declared in std — it is a global `#{buildin}`
// stub (`vec-push = (val t)`) precisely so that no `[]t.push` method body is
// created — and clear/asc/desc take no arguments, so push is the
// only builtin-only method with anything to check.
//
// The types come from BuiltinMethod.ElemParams, NOT from BuiltinMethod.Params:
// Params records i64 for every such parameter because the registry is
// element-type-agnostic, so substituting the element type into it would reject
// a legal `[]str.remove(0)` — that is precisely the reason this table used to
// be considered unhookable. ElemParams marks which positions are the
// receiver's element; unmarked positions keep their scalar type.
//
// The types describe the STORAGE SHAPE, not a declared signature: codegen
// derives the push stride from the receiver's element type
// (emitBuiltinVecPush / elemTypeOfReceiver, pinned by
// tests/push-narrow.no), so pushing an i64 into a []byte truncates to one
// byte by design. src/std relies on that (zip-writer.put takes `b i64` and
// pushes it into a []byte), so integer-vs-integer is not reported as an error
// here — only a mismatch of KIND is (see checkStdMethodCallArgs).
func builtinSliceMethodParamTypes(recvType, method string) ([]string, string) {
	if !strings.HasPrefix(recvType, "[]") {
		return nil, "" // not a slice: no element to substitute
	}
	key := "[]t." + method
	bm := builtin.FindBuiltinMethod(key)
	if bm == nil || len(bm.Params) == 0 {
		return nil, ""
	}
	params := make([]string, len(bm.Params))
	for i := range params {
		if i < len(bm.ElemParams) && bm.ElemParams[i] {
			params[i] = "t" // the receiver's element type
			continue
		}
		params[i] = bm.Params[i].String()
	}
	// Returning the receiver-generic key is what makes the caller substitute
	// the element type from the receiver (see genericElemOfKey).
	return params, key
}

// isIntegerTypeName reports whether t is one of the integer types (including
// the aliases byte/char and u8..u64). It is used to tell "the same kind stored
// at a different stride" apart from a genuine type mismatch.
func isIntegerTypeName(t string) bool {
	_, _, ok := intTypeRange(t)
	return ok
}

// checkStdMethodCallArgs type-checks the arguments of `recv.method(args)`
// against the std signature tables.
//
// The deliberate rule here is DO NOT INFER. A receiver-generic method such as
// `[]t.eq` declares its parameter as `[]t`; at a `[]i64` call site the element
// type is only recoverable by substituting into the receiver, and any guess
// (notably reading a slice as the builtin `vec`) produces false diagnostics.
// Parameters that still mention the type variable are therefore skipped rather
// than checked. Only fully concrete parameters — `[]byte.index` expecting
// `str`, `[]str.join` expecting `str` — are verified.
func checkStdMethodCallArgs(e *parser.CallExpression, dot *parser.DotExpression, varTypes map[string]string, structFields map[string]map[string]string) []ValidateResult {
	if dot == nil || len(e.Arguments) == 0 {
		return nil
	}
	recvType := resolveExprType(dot.Receiver, varTypes, structFields)
	params, key, storage := stdMethodParamTypes(recvType, dot.Property)
	if len(params) == 0 {
		return nil
	}
	var results []ValidateResult
	for i, arg := range e.Arguments {
		if i >= len(params) {
			break
		}
		paramType := params[i]
		if paramType == "" {
			continue // unnamed / untyped parameter in the std source
		}
		if tv := genericElemOfKey(key); tv != "" {
			// Receiver-generic method (`[]t.eq`). The element type is DERIVED
			// from the receiver, not guessed: "[]i64" pins t = i64. When the
			// receiver carries no element — a map, or the builtin `vec` — there
			// is nothing to substitute and the parameter is skipped. Reading a
			// slice as `vec` and then checking against it is exactly the
			// mis-inference this must never do.
			elem := sliceElemOf(recvType)
			if elem == "" {
				continue
			}
			paramType = substituteTypeVar(paramType, tv, elem)
			if paramType == "" {
				continue
			}
		}
		// A parameter that STILL mentions a bare single-lowercase-letter type
		// variable (`t`, or a container of it like `[]t`, `?t`, `[n]t`) is generic
		// in a way the checker cannot bind here. Slice-field generic structs
		// (`heap { data []t }`, `set { data []t }`) declare methods whose element
		// type `t` is specialized by the FACTORY argument (`set.init(buf)`), not
		// by the receiver name (`set`), so `recvType` carries no element to
		// substitute. Per the "DO NOT INFER" rule above we SKIP such parameters
		// rather than mis-report `expected '[]t', got '[8]i64'`.
		if mentionsTypeVar(paramType) {
			continue
		}
		argType := resolveExprType(arg, varTypes, structFields)
		if argType == "" {
			continue // unknown argument type: nothing to assert
		}
		// Union-alias parameters dispatch per MEMBER at monomorphization time, and that
		// pass matches by type NAME only (see build.inferExprMemberType). A merely
		// "compatible" argument — `byte` into `bigint | i64` via integer widening —
		// therefore picks no member and leaves the template callee dangling. Bail out
		// here with a source position instead (`unionarg`). Placed BEFORE the option
		// guard on purpose: `?i64` must resolve through its payload, while `?byte`
		// still has to be rejected.
		if us := checkUnionMemberArg(arg, paramType, argType, recvType+"."+dot.Property, i+1); len(us) > 0 {
			results = append(results, us...)
			continue
		}
		// `storage` types describe the builtin's STORAGE SHAPE, not a declared
		// signature, and the codegen stores the value at the receiver's element
		// stride — an i64 pushed into a []byte is truncated to one byte by
		// design. Integer-vs-integer is therefore never an error there; only a
		// mismatch of KIND is reported (a str pushed into a []i64 and friends).
		if storage && isIntegerTypeName(paramType) && isIntegerTypeName(argType) {
			continue
		}
		// An option-typed argument is also skipped. varTypes does not always
		// track that a `?T` was already unwrapped by an enclosing `ok ->` arm,
		// so asserting on it here reports real std calls as errors (net.no's
		// `e.init(c, key)` was flagged this way). The dedicated option rule
		// (fxxoptarg) already covers user-defined calls, where the flow is
		// known precisely.
		if ty.IsOptionRaw(argType) {
			continue
		}
		if typeNamesEquivalent(paramType, argType) {
			continue
		}
		if isArgTypeCompatible(paramType, argType, arg) {
			continue
		}
		line, col := e.Token.Line, e.Token.Column
		if p := arg.Pos(); p.Line > 0 {
			line, col = p.Line, p.Column
		}
		results = append(results, ValidateResult{
			TraceID: "stdargtyp",
			Line:    line,
			Column:  col,
			Message: fmt.Sprintf("argument %d of '%s.%s': expected '%s', got '%s'",
				i+1, recvType, dot.Property, paramType, argType),
		})
	}
	return results
}

// checkPrivateBuiltinMethodCall enforces that the underscore-prefixed builtin
// methods on str/txt — PRIVATE field accessors/mutators registered in
// src/builtin (str._len / str._cap / str._data / str._set-len / str._set-cap /
// str._set-data / txt._len / txt._data / txt._set-len) — are ONLY callable
// from within their OWNING std module:
//
//   - str._*  may only be called from src/std/str.no
//   - txt._*  may only be called from src/std/txt.no
//
// Any call from user code (or from any other std module) is a COMPILE-TIME
// error. The owner module is derived from the receiver type (str/txt); curModule
// is the std module ShortName of the enclosing top-level statement (set by
// ValidateFuncArgs from parser.GetSourceFile + stdModuleForSourceFile).
//
// Only REGISTERED private builtins are enforced — a user-defined underscore
// method on some other type is unrelated and left to the normal checks.
func checkPrivateBuiltinMethodCall(e *parser.CallExpression, dot *parser.DotExpression, recvType, curModule string) []ValidateResult {
	if dot == nil || dot.Property == "" {
		return nil
	}
	if !strings.HasPrefix(dot.Property, "_") {
		return nil
	}
	if recvType != "str" && recvType != "txt" {
		return nil
	}
	// Only enforce for a builtin actually registered under this
	// receiver.method (str._len, txt._data, ...). Bare-name matches are
	// impossible here because the key is always receiver-qualified.
	if builtin.FindBuiltinMethod(recvType+"."+dot.Property) == nil {
		return nil
	}
	if curModule == recvType {
		return nil // allowed: call site is inside the owning module
	}
	line, col := e.Token.Line, e.Token.Column
	if p := dot.Pos(); p.Line > 0 {
		line, col = p.Line, p.Column
	}
	where := curModule
	if where == "" {
		where = "user code"
	}
	return []ValidateResult{{
		TraceID: "privbuiltin",
		Line:    line,
		Column:  col,
		Message: fmt.Sprintf("private builtin method '%s.%s' may only be called from within module %s, not from %s", recvType, dot.Property, recvType, where),
	}}
}

// isLoweringTempArg reports whether e is an identifier naming a
// lowering-synthesized temporary.
//
// Every such name starts with `__`, and a `__`-prefixed name CANNOT be written
// in Nolang source — `__x = 1` and `print(__x)` both fail to parse — so any
// identifier carrying that prefix was produced by the lowering passes in
// `src/parser/lowering.go` (`__cap_N` capture temps, `__unwrap_N` / `__opt_N`
// `?=` temporaries, `__match_subj_L_C` safe-index match subjects).
//
// They are all typed `?T` by construction, but their own `ok` arm uses them as
// the unwrapped payload, and codegen lowers them that way. The argument option
// check (fxxoptarg) must therefore skip them; see the call site for the
// measured evidence.
func isLoweringTempArg(e parser.Expression) bool {
	id, ok := e.(*parser.Identifier)
	return ok && strings.HasPrefix(id.Value, "__")
}

func checkCallArgsInExpr(expr parser.Expression, sigs map[string]*funcSig, varTypes map[string]string, structFields map[string]map[string]string, underUnwrap bool, view *THIRView) []ValidateResult {
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
					results = append(results, checkCallArgsInExpr(arg, sigs, varTypes, structFields, underUnwrap, view)...)
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
				// Method calls have implicit self in signature but not in expr.Arguments
				maxArgs := len(sig.ParamTypes)
				if len(sig.ParamTypes) > 0 && sig.ParamTypes[0].Name == "self" {
					maxArgs--
				}
				// A `f (a ..T)` spread parameter absorbs any number of trailing
				// arguments, so it puts no upper bound on the count. Without this the
				// language's own variadic functions were unusable:
				// `mx = (a ..i64) (r i64)` + `mx(3, 1, 7)` reported
				// "expects at most 1 input argument(s), got 3".
				if !sig.Variadic && len(e.Arguments) > maxArgs {
					results = append(results, ValidateResult{
						TraceID: "oarg2",
						Line:    e.Token.Line,
						Column:  e.Token.Column,
						Message: fmt.Sprintf("function '%s' expects at most %d input argument(s), got %d — output parameters should be bound via assignment, e.g. result = %s(...)", ident.Value, maxArgs, len(e.Arguments), ident.Value),
					})
				} else if len(e.Arguments) < minArgs {
					results = append(results, ValidateResult{
						TraceID: "haahhq7o",
						Line:    e.Token.Line,
						Column:  e.Token.Column,
						Message: fmt.Sprintf("function '%s' expects at least %d argument(s), got %d", ident.Value, minArgs, len(e.Arguments)),
					})
				} else {
					// Check argument types using resolveExprType (handles struct fields, arrays, etc.)
					// For a variadic callee the LAST parameter is the spread: every
					// argument at or past it is an ELEMENT of that parameter, so the
					// expected type is `T`, never the `[]T` the parameter is declared as
					// (parser lowers `..T` to a slice type).
					variadicAt := -1
					if sig.Variadic && len(sig.ParamTypes) > 0 {
						variadicAt = len(sig.ParamTypes) - 1
					}
					for i, arg := range e.Arguments {
						pi := i
						if variadicAt >= 0 && pi > variadicAt {
							pi = variadicAt
						}
						if pi >= len(sig.ParamTypes) || (i >= len(sig.ParamTypes) && variadicAt < 0) {
							break
						}
						argType := resolveExprType(arg, varTypes, structFields)
						expectedType := sig.ParamTypes[pi].Type
						if variadicAt >= 0 && pi == variadicAt {
							expectedType = strings.TrimPrefix(expectedType, "[]")
						}
						if expectedType == "" || argType == "" {
							continue
						}
						// An argument whose resolved type STILL mentions a bare
						// generic type variable (`t`, `?t`, `[]t`, `[n]t`) could
						// not be specialized at check time. Slice-field generic std
						// structs (`heap { data []t }`, `set { data []t }`) return
						// `?t`/`[]t` from their methods, and the element type `t` is
						// bound only by the factory argument during
						// monomorphizeSliceStructs, which runs AFTER the checker. Per
						// the "DO NOT INFER" rule we skip the mismatch rather than
						// mis-report `expected '?i64', got '?t'`.
						if mentionsTypeVar(argType) {
							continue
						}
						// Phase-2 rule: a bare option argument (?T passed where T is
						// expected) must be rejected. Implicit unwrap of option
						// arguments is not allowed. Provably-non-option expressions
						// (e.g. a constant like `10+20`, or an `it` binding inside a
						// match arm) already resolve to the plain T type and never
						// reach this branch, so they pass. Genuine ?T values (a ?i64
						// variable, a ?i64-returning call, etc.) cannot be proven
						// non-option at compile time and are rejected here, forcing
						// the caller to unwrap explicitly (match on the value and call
						// inside the `-> it` arm, or force-unwrap).
						//
						// Exemption: when the call is the RHS of a `?=` unwrap
						// (underUnwrap == true, set by the desugared `__unwrap_N` let
						// in the statement checker), the `?=` itself is the required
						// explicit unwrap — the same exemption already granted to the
						// bare-arithmetic-argument rule (f10x20nf) below. Inside a `?=`
						// RHS, a ?T argument is auto-unwrapped and any none propagates
						// upward, which is exactly `?=`'s purpose.
						// Lowering-synthesized temporaries (`__cap_N`, `__unwrap_N`,
						// `__opt_N`, `__match_subj_L_C`) are typed `?T`, but inside
						// their own `ok` arm they denote the UNWRAPPED payload —
						// the same convention as an arm's `it` binding, and the one
						// codegen implements (the argument reaches the callee as a
						// plain `T`; verified on `r0 = g(3, j - 1)`, whose desugared
						// ok arm emits `call void @g(i64 %lv80, …)` and prints the
						// correct value).
						//
						// The checker cannot track arm context for these temps, so the
						// option test must be skipped for them — otherwise every
						// desugared `x = f(a, b - 1)` (a call whose argument carries
						// an option source) reports a spurious `fxxoptarg`. Exempting
						// them cannot mask a user's mistake: a `__`-prefixed name
						// cannot be written in Nolang source (`__x = …` and
						// `print(__x)` both fail to parse).
						if isLoweringTempArg(arg) {
							continue
						}
						if !underUnwrap && ty.IsOptionRaw(argType) && ty.StripOptionRaw(argType) == expectedType {
							results = append(results, ValidateResult{
								TraceID: "fxxoptarg",
								Line:    e.Token.Line,
								Column:  e.Token.Column,
								Message: fmt.Sprintf("argument %d of '%s': passing a bare option value of type '%s' where '%s' is expected requires explicit unwrap (match on the value and call inside the `-> it` arm); implicit unwrap of option arguments is not allowed", i+1, ident.Value, argType, expectedType),
							})
							continue
						}
						if !isArgTypeCompatible(expectedType, argType, arg) {
							results = append(results, ValidateResult{
								TraceID: "6fgg3htw",
								Line:    e.Token.Line,
								Column:  e.Token.Column,
								Message: fmt.Sprintf("argument %d of '%s': expected '%s', got '%s'", i+1, ident.Value, expectedType, argType),
							})
						}
					}
				}
				// Phase-2 rule: an option-returning function called with a bare
				// arithmetic argument must be explicitly unwrapped with `?=`.
				// A bare arithmetic expression (e.g. `10+20`) is option-bearing in
				// nolang (overflow can be raised), so passing it directly to a
				// function that returns an option (?T) without `?=` is rejected to
				// force the caller to acknowledge the option. The call is exempt
				// when it is the RHS of a `?=` unwrap (underUnwrap == true, set by
				// the desugared `__unwrap_N` let in the statement checker).
				if !underUnwrap && funcSigReturnsOption(sig) {
					for _, arg := range e.Arguments {
						if isArithmeticExpr(arg) {
							results = append(results, ValidateResult{
								TraceID: "f10x20nf",
								Line:    e.Token.Line,
								Column:  e.Token.Column,
								Message: fmt.Sprintf("call to option-returning function '%s' with a bare arithmetic argument requires explicit unwrap: use 'x ?= %s(...)' to unwrap the argument first, or unwrap the result (e.g. 'y ?= %s(...)')", ident.Value, ident.Value, ident.Value),
							})
							break
						}
					}
				}
			}
		} else if dot, ok := e.Function.(*parser.DotExpression); ok {
			// `recv.method(args)`: the callee is a DotExpression, which the
			// branch above does not handle, so method arguments used to go
			// completely unchecked. The std signature tables now carry
			// parameter types, so they can be checked here.
			results = append(results, checkStdMethodCallArgs(e, dot, varTypes, structFields)...)
			// Compile-time privacy enforcement for underscore-prefixed str/txt
			// builtin field accessors (see checkPrivateBuiltinMethodCall).
			recvType := resolveExprType(dot.Receiver, varTypes, structFields)
			results = append(results, checkPrivateBuiltinMethodCall(e, dot, recvType, validationCurModule)...)
			// ... and for underscore-prefixed std symbols owned by a DIFFERENT
			// module (see checkPrivateStdCall): `_` in std means module-internal,
			// so a cross-module call is an ERROR, not a convention.
			results = append(results, checkPrivateStdCall(e, dot, recvType, validationCurModule)...)
		}
		// Recurse into arguments for nested calls
		for _, arg := range e.Arguments {
			results = append(results, checkCallArgsInExpr(arg, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.InfixExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkCallArgsInExpr(e.Left, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		if e.Right != nil {
			results = append(results, checkCallArgsInExpr(e.Right, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.PrefixExpression:
		if e.Right != nil {
			return checkCallArgsInExpr(e.Right, sigs, varTypes, structFields, underUnwrap, view)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			return checkCallArgsInExpr(e.Expression, sigs, varTypes, structFields, underUnwrap, view)
		}
	case *parser.IfExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkCallArgsInExpr(e.Condition, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		if e.Consequence != nil {
			for _, is := range e.Consequence.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs, varTypes, structFields, view)...)
			}
		}
		if e.Alternative != nil {
			for _, is := range e.Alternative.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs, varTypes, structFields, view)...)
			}
		}
		return results
	case *parser.SurfaceMatch:
		var results []ValidateResult
		// S4：糖形態下 match 是 SurfaceMatch，臂體對本 walker 不可見。見 sugar_walk.go。
		walkSurfaceMatchChildren(e, varTypes,
			func(x parser.Expression) {
				results = append(results, checkCallArgsInExpr(x, sigs, varTypes, structFields, underUnwrap, view)...)
			},
			func(s parser.Statement) {
				results = append(results, checkCallArgsInStmt(s, sigs, varTypes, structFields, view)...)
			})
		return results
	case *parser.IndexExpression:
		var results []ValidateResult
		if e.Left != nil {
			results = append(results, checkCallArgsInExpr(e.Left, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		if e.Index != nil {
			results = append(results, checkCallArgsInExpr(e.Index, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.AssignExpression:
		if e.Value != nil {
			return checkCallArgsInExpr(e.Value, sigs, varTypes, structFields, underUnwrap, view)
		}
	case *parser.ConditionalExpression:
		var results []ValidateResult
		if e.Condition != nil {
			results = append(results, checkCallArgsInExpr(e.Condition, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		if e.Consequence != nil {
			results = append(results, checkCallArgsInExpr(e.Consequence, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		if e.Alternative != nil {
			results = append(results, checkCallArgsInExpr(e.Alternative, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.ArrayLiteral:
		var results []ValidateResult
		for _, elem := range e.Elements {
			results = append(results, checkCallArgsInExpr(elem, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.SliceLiteral:
		var results []ValidateResult
		for _, elem := range e.Elements {
			results = append(results, checkCallArgsInExpr(elem, sigs, varTypes, structFields, underUnwrap, view)...)
		}
		return results
	case *parser.StructLiteral:
		var results []ValidateResult
		for _, f := range e.Fields {
			if f.Value != nil {
				results = append(results, checkCallArgsInExpr(f.Value, sigs, varTypes, structFields, underUnwrap, view)...)
			}
		}
		return results
	case *parser.FunctionLiteral:
		if e.Body != nil {
			var results []ValidateResult
			for _, is := range e.Body.Statements {
				results = append(results, checkCallArgsInStmt(is, sigs, varTypes, structFields, view)...)
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
		case *parser.ViewType:
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
	// 同模組的全大寫常量（nolang 常量命名約定，如 SQLITE-OK / SQLITE-ROW）。
	// 驅動的方法體會直接引用它們做結果碼比較；若不同步導出，呼叫端攤平後
	// MIR 解析不到該名字，會退化成 undef（`icmp ne i64 %rc, undef`），
	// 比較結果恆為真、錯誤分支永遠走不到。
	definedConstants := make(map[string]bool)
	for _, stmt := range prog.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil && isConstantName(ls.Name.Value) {
			definedConstants[ls.Name.Value] = true
		}
	}
	calledFuncs := make(map[string]bool)
	// referencedConsts：被保留函式／方法體「直接引用」（非呼叫）的同模組常量名。
	referencedConsts := make(map[string]bool)
	var collectCalledFuncs func(body *parser.BlockStatement)
	var walkExpr func(expr parser.Expression)
	var walkStmts func(stmts []parser.Statement)
	walkExpr = func(expr parser.Expression) {
		if expr == nil {
			return
		}
		switch e := expr.(type) {
		case *parser.Identifier:
			// 裸識別字引用（常量）。只收同模組的全大寫常量，避免誤抓區域變數
			// 或 std 名，把不相關的 LetStatement 一併帶進導出集。
			if definedConstants[e.Value] {
				referencedConsts[e.Value] = true
			}
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
		case *parser.SurfaceMatch:
			// S4：糖形態下 match 是 SurfaceMatch，臂體對本 walker 不可見。見 sugar_walk.go。
			walkSurfaceMatchChildren(e, nil,
				func(x parser.Expression) { walkExpr(x) },
				func(s parser.Statement) { walkStmts([]parser.Statement{s}) })
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
	// 導出型別時自動導出其方法（Type.method）。
	// 驅動套件（如 example/sqlite-driver）在 lib.no 中只導出型別：
	//   @ /src/sqlite.db-sqlite
	//   @ /src/sqlite.open
	// 但介面方法的實作定義在同檔案的 `db-sqlite.exec = (...) {...}` 形式
	// （AST 上是名為 "db-sqlite.exec" 的 FunctionDefinition）。這些方法既不在
	// 導出清單中、也不被任何已導出函式呼叫，會被本函式過濾掉。呼叫端把
	// `d.exec(...)` 攤平為 `sqlite.db-sqlite.exec(d, ...)` 後找不到函式定義，
	// MIR codegen 報 "unknown callee sqlite.db-sqlite.exec"。
	// 型別一旦導出，其方法就屬於該型別的公開 ABI，必須一併導出。
	// 必須放在下方 worklist 傳遞閉包「之前」：方法體會呼叫 FFI 宣告與其他
	// 同模組 helper，那些依賴要靠閉包一併帶出。
	for _, stmt := range prog.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		dot := strings.Index(fd.Name, ".")
		if dot <= 0 {
			continue
		}
		if _, ok := exported[fd.Name[:dot]]; ok {
			exported[fd.Name] = ""
		}
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
		// 方法體直接引用的同模組常量一併導出（見 referencedConsts 的說明）。
		for c := range referencedConsts {
			if _, alreadyKept := exported[c]; !alreadyKept {
				exported[c] = ""
				worklist = append(worklist, c)
			}
		}
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
	//
	// 必須帶著 prog.Sem 回傳：Sem 是以「節點指標」為鍵的語義側表（#{overflow} /
	// #{index-out} / #{ascii} / platform keys / embed / 推斷型別…），過濾只動頂層
	// 語句清單、不動節本體，兩者的節點是同一批指標。若回傳 `&parser.Program{Statements: …}`
	// 把 Sem 丟掉，導入模組的標註在 merged 程式裡就整套查不到（transpiler 的
	// `merged.Sem.Merge(modProg.Sem)` 對 nil 是 no-op），於是
	//   - 安全索引寫入 desugar（parser/lowering.go）讀不到原陳述的 overflow 條目、
	//     無法把模式轉移到取代節點；
	//   - 下游 checker 遂把已標註的 `out[20 + i]` 報成 ovf-int-default 硬錯誤，
	//     而同一份檔案「直接當主檔 vet」（不經本過濾）零錯誤——同一原始碼兩條
	//     管道自相矛盾（實報 example/mysql-driver/src/mysql.no:234）。
	// BuiltinFuncNames 同理：它是「按名稱」跳過內建樁返回值校驗的集合，遺失會讓
	// 導入模組的 #{buildin} 樁被 i3k422u3 誤報。
	// Warnings 屬文字訊息且可能指向被過濾掉的語句，不帶（維持原行為）。
	filtered := &parser.Program{
		Statements:       []parser.Statement{},
		Sem:              prog.Sem,
		BuiltinFuncNames: prog.BuiltinFuncNames,
	}
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.UseStatement:
			// Always keep UseStatements so that transitive imports are processed.
			// Without this, a lib.no's `# /path/to/impl` would be filtered out,
			// preventing the actual function definitions from being loaded.
			filtered.Statements = append(filtered.Statements, s)
		case *parser.ExternStatement:
			// #{c} FFI 宣告（如 _sqlite3-open）必須無條件保留。它們只是簽名
			// 宣告、不產生任何程式碼，只有被呼叫時才需要對應的 C 符號；而被
			// 保留的函式／方法體正是唯一可能呼叫它們的地方。把它們按導出清單
			// 過濾會讓驅動套件（lib.no 只導出型別與 open）的所有 FFI 呼叫在
			// 連結期變成 undefined symbol：
			//   "_sqlite3_open", referenced from: _sqlite.open in ...
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
	typeStr = ty.StripOptionRaw(typeStr)
	if ty.IsSliceRaw(typeStr) {
		return "slice"
	}
	if ty.IsContainerRaw(typeStr) {
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
	st := ty.Parse(mapType)
	if st.Kind != ty.Map || st.Key == nil || st.Elem == nil {
		return ""
	}
	keyType := st.Key.Raw()
	if keyType == "" || checkerKeyCategory(keyType) == "" {
		return ""
	}
	return "hashmap-" + keyType + "-" + parser.SanitizeLLVMTypeName(st.Elem.Raw())
}
