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

	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// FilterByExports 依 lib.no 匯出聲明過濾模組 Program（供 build 代碼生成使用）。
func FilterByExports(prog *parser.Program, libPath string) *parser.Program {
	return filterByExports(prog, libPath)
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
			modProg := resolveUseModule(use, pkg)
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
		results = append(results, checkCallArgsInStmt(stmt, sigs, topLevelVarTypes, structFields)...)
	}
	return results
}

// resolveUseModule resolves a UseStatement to its module program.
// It handles local paths (/path), std paths, and dependency paths (domain/...).
func resolveUseModule(use *parser.UseStatement, pkg *pkg.Package) *parser.Program {
	path := use.Path
	var prog *parser.Program
	var filePath string
	// Local module paths (starting with /)
	if strings.HasPrefix(path, "/") {
		if pkg == nil {
			return nil
		}
		relPath := strings.TrimPrefix(path, "/")
		baseDir := pkg.WorkspaceRoot()
		filePath = filepath.Join(baseDir, relPath) + ".no"
		prog = parseProgramFile(filePath)
	} else if strings.HasPrefix(path, "std/") || path == "std" {
		// std/ paths — strip "std/" prefix to get module path relative to std/
		relPath := strings.TrimPrefix(path, "std/")
		if path == "std" {
			relPath = ""
		}
		filePath = resolveModulePath(relPath)
		if filePath == "" {
			return nil
		}
		prog = parseProgramFile(filePath)
	} else if pkg != nil {
		// Dependency paths (domain/org/repo/...)
		var err error
		filePath, err = pkg.ResolveDependencyModule(path)
		if err == nil && filePath != "" {
			prog = parseProgramFile(filePath)
		}
	}
	if prog == nil {
		return nil
	}
	// Apply lib.no export filtering
	if filePath != "" {
		pkgRoot := findPackageRootFromFile(filePath)
		if pkgRoot != "" {
			libPath := filepath.Join(pkgRoot, "lib.no")
			if _, err := os.Stat(libPath); err == nil {
				prog = filterByExports(prog, libPath)
			}
		}
	}
	return prog
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
		if s.Value != nil {
			return checkCallArgsInExpr(s.Value, sigs, varTypes, structFields)
		}
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
		if underlying, ok := validationConcreteTypeAliases[argType]; ok && underlying == expectedType {
			return true
		}
	}
	// Implicit widening: if both types are integer types and the argType's
	// range is fully contained within the expectedType's range, allow the
	// conversion (e.g. byte → i64, u8 → i32, i8 → i64).
	if aMin, aMax, aOk := intTypeRange(argType); aOk {
		if eMin, eMax, eOk := intTypeRange(expectedType); eOk {
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
	// a is alias, b is underlying
	if underlying, ok := validationConcreteTypeAliases[a]; ok && underlying == b {
		return isSimplePrimitiveTypeName(underlying)
	}
	// b is alias, a is underlying
	if underlying, ok := validationConcreteTypeAliases[b]; ok && underlying == a {
		return isSimplePrimitiveTypeName(underlying)
	}
	return false
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
			if sig, ok := sigs[ident.Value]; ok {
				// Check argument count (allow fewer args when default values exist)
				minArgs := len(sig.ParamTypes)
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
func filterByExports(prog *parser.Program, libPath string) *parser.Program {
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
		fn    string
		alias string
	}
	var exports []exportEntry
	for _, stmt := range libProg.Statements {
		if es, ok := stmt.(*parser.ExportStatement); ok {
			exports = append(exports, exportEntry{
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
		for _, e := range exports {
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
