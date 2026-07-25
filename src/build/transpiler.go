package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	nolang "github.com/lizongying/nolang"
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
	// 記錄需要從 program.Statements 中刪除的重複函數
	toRemove := make(map[*parser.FunctionDefinition]bool)
	for name, fns := range overloads {
		if len(fns) <= 1 {
			continue // 無重載，不改名
		}
		// 去重：對於相同名稱+簽名+平台組合的重複定義（多模組同函數），只保留第一個。
		// 不同平台標註（#{mac-arm64} vs #{wasi-wasm32}）的定義視為不同函數，不去重。
		seenSigs := make(map[string]bool)
		uniqueFns := make([]*parser.FunctionDefinition, 0, len(fns))
		for _, fd := range fns {
			sig := platformAwareCallSignature(name, fd.Parameters, fd.PlatformKeys)
			if !seenSigs[sig] {
				seenSigs[sig] = true
				uniqueFns = append(uniqueFns, fd)
			} else {
				toRemove[fd] = true
			}
		}
		// 用 uniqueFns 取代 fns 進行後續處理
		fns = uniqueFns
		overloads[name] = uniqueFns
		// 去重後若僅剩單一函數（同簽名重複定義），無需改名
		if len(uniqueFns) <= 1 {
			continue
		}
		for _, fd := range fns {
			parts := []string{name}
			for _, p := range fd.Parameters {
				parts = append(parts, sanitizeTypeForName(p.Type.String()))
			}
			mangledName := strings.Join(parts, "_")
			fd.Name = mangledName // 直接修改 AST
			sig := callSignature(name, fd.Parameters)
			mangled[sig] = mangledName
		}
	}

	// 從 program.Statements 中刪除重複的函數定義
	if len(toRemove) > 0 {
		filtered := make([]parser.Statement, 0, len(program.Statements))
		for _, stmt := range program.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if toRemove[fd] {
					continue
				}
			}
			filtered = append(filtered, stmt)
		}
		program.Statements = filtered
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
				if s.Condition != nil {
					updateCallNames(s.Condition, overloads, mangled, varTypes)
				}
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.MultiAssignStatement:
				if s.Value != nil {
					updateCallNames(s.Value, overloads, mangled, varTypes)
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
		parts = append(parts, sanitizeTypeForName(p.Type.String()))
	}
	return strings.Join(parts, "_")
}

// platformAwareCallSignature 生成包含平台標註的調用簽名 key。
// 用於 mangleOverloads 去重：不同平台標註（#{mac-arm64} vs #{wasi-wasm32}）
// 的同名同參數函數視為不同函數，不應被去重。
// platformKeys 為空表示平台通用宣告。
func platformAwareCallSignature(name string, params []*parser.Parameter, platformKeys []string) string {
	sig := callSignature(name, params)
	if len(platformKeys) == 0 {
		return sig + "\x00" // 通用平台用 \x00 後綴與特定平台區分
	}
	// 排序平台 key 以確保不同順序的相同組合產生相同簽名
	sorted := make([]string, len(platformKeys))
	copy(sorted, platformKeys)
	sort.Strings(sorted)
	return sig + "\x00" + strings.Join(sorted, ",")
}

// sanitizeTypeForName 將型別字串轉成 LLVM 識別符安全的形式：
// - "[]byte"   → "slice.byte"
// - "?i64"     → "opt.i64"
// - "[4]i64"   → "arr4.i64"
// - "ptr i64"  → "ptr.i64"
func sanitizeTypeForName(s string) string {
	r := strings.NewReplacer(
		"[]", "slice.",
		"?", "opt.",
		"ptr ", "ptr.",
		"[", "arr",
		"]", ".",
		" ", "_",
		"|", "-",
	)
	return r.Replace(s)
}

// isConcreteType 檢查型別名稱是否為已知具體型別
func isConcreteType(typeName string) bool {
	switch typeName {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"byte", "f64", "str", "bool", "char", "void":
		return true
	}
	// 複合型別：切片、陣列、可空、指針
	if strings.HasPrefix(typeName, "[]") || strings.HasPrefix(typeName, "[") ||
		strings.HasPrefix(typeName, "?") || strings.HasPrefix(typeName, "ptr ") {
		return true
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

// validationStructFields holds struct name → field name → field type string,
// populated by ValidateTypes for use in inferExprType when resolving
// self.field method calls (e.g. .recv-buf.slice()).
var validationStructFields map[string]map[string]string

func inferExprType(expr parser.Expression, varTypes map[string]string, funcTypes map[string]string, selfType string) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		// 十六進位字面量（0xNN）優先推斷為 byte
		raw := e.Token.Literal
		if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
			return "byte"
		}
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.CharLiteral:
		return "char"
	case *parser.ByteLiteral:
		return "byte"
	case *parser.RegexLiteral:
		return "regexp"
	case *parser.Identifier:
		if t, ok := varTypes[e.Value]; ok {
			// Function-type variable: return simplified "fn" marker
			if strings.HasPrefix(t, "fn(") {
				return "fn"
			}
			return t
		}
		return "" // 未知變數
	case *parser.CallExpression:
		// 1. 檢查內建函數
		if ident, ok := e.Function.(*parser.Identifier); ok {
			for _, m := range builtin.BuiltinMethodList {
				if m.MethodName == ident.Value {
					if len(m.Return) > 0 {
						return m.Return[0].String()
					}
				}
			}
			// 2. 檢查用戶定義的函數（含 extern）
			if retType, exists := funcTypes[ident.Value]; exists {
				// FFI ptr 型別在 Nolang 層以 i64 儲存（透過 ptrtoint/inttoptr 轉換）
				// ptr 可能是 "ptr <nil>"（不透明指標）或 "ptr T"（具型別指標）
				if strings.HasPrefix(retType, "ptr") {
					return "i64"
				}
				return retType
			}
			// 跨模組函數呼叫（定義在 std 模組中，vet 階段尚未 merge），
			// 對已知回傳 str 的函數直接推斷，避免變數型別缺失
			switch ident.Value {
			case "char-to-str", "i64-to-str", "f64-to-str", "bool-to-str", "byte-to-str":
				return "str"
			}
			return ""
		}
		// 4. 檢查 struct 方法調用（DotExpression）
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			var typeName string
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recv.Value == "self" {
					// 從當前方法的 self 參數獲取類型
					typeName = selfType
				} else if recvType, exists := varTypes[recv.Value]; exists {
					typeName = recvType
				}
			} else if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				// 字串字面量接收者（如 '123'.to-i64()）→ str 型別
				typeName = "str"
			} else if innerDot, ok := dot.Receiver.(*parser.DotExpression); ok {
				// struct field 方法調用（如 .field.method() 即 self.field.method()）
				// 遞迴推斷接收者型別
				if innerRecv, ok := innerDot.Receiver.(*parser.Identifier); ok && innerRecv.Value == "self" {
					// self.field → 查 struct 定義取得 field 型別
					if validationStructFields != nil {
						if fields, ok := validationStructFields[selfType]; ok {
							if fieldType, ok := fields[innerDot.Property]; ok {
								typeName = fieldType
							}
						}
					}
				}
			} else if _, ok := dot.Receiver.(*parser.IndexExpression); ok {
				// 陣列元素接收者（如 arr[i].slice(...)）— 元素型別無法靜態推斷，
				// 返回空字串跳過型別檢查，由 LLVM 端驗證
				return ""
			}
			if typeName != "" {
				methodName := typeName + "." + dot.Property
				if retType, exists := funcTypes[methodName]; exists {
					return retType
				}
				// 查詢內建方法（如 i64.to-str, str.to-i64, str.to-bool 等）
				for _, m := range builtin.BuiltinMethodList {
					if m.MethodName == methodName && len(m.Return) > 0 {
						return m.Return[0].String()
					}
				}
				// typeName 已知但方法定義在 std 模組中（vet 階段尚未 merge），
				// 無法推斷回傳型別；返回空字串跳過型別檢查，由 LLVM 端驗證
				return ""
			}
		}
		// 接收者型別未知（如跨模組函數返回的變數、struct field 存取結果等），
		// 無法推斷回傳型別；返回空字串跳過型別檢查，由 LLVM 端驗證
		return ""
	case *parser.InfixExpression:
		// 簡單推斷：比較與邏輯運算返回 bool，算術返回左運算元型別，
		// 位元/移位運算僅在左運算元為具體整數型別時返回該型別（避免泛型型別參數回傳非整數型別）
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool"
		case "+", "-", "*", "/":
			// 根據左運算元推斷型別
			leftType := inferExprType(e.Left, varTypes, funcTypes, selfType)
			if leftType != "" {
				return leftType
			}
			return "i64"
		case "&", "|", "^", "<<", ">>":
			// 位元/移位運算：僅當左運算元為具體整數型別時回傳該型別，
			// 否則回退為 i64（保留舊行為，避免泛型型別參數如 k 造成型別不匹配）
			leftType := inferExprType(e.Left, varTypes, funcTypes, selfType)
			if leftType != "" && intTypeBits(leftType) > 0 {
				return leftType
			}
			return "i64"
		default:
			return "i64"
		}
	case *parser.CastExpression:
		// 強轉表達式的型別即目標型別
		if e.Type != nil {
			return e.Type.String()
		}
		return "i64"
	case *parser.PrefixExpression:
		if e.Operator == "!" {
			return "bool"
		}
		// 前綴正負號傳遞內層表達式的型別
		return inferExprType(e.Right, varTypes, funcTypes, selfType)
	case *parser.DotExpression:
		// Struct field access: look up receiver type in varTypes,
		// then resolve field type from validationStructFields.
		if validationStructFields != nil {
			var typeName string
			if recv, ok := e.Receiver.(*parser.Identifier); ok {
				if recv.Value == "self" {
					typeName = selfType
				} else if t, exists := varTypes[recv.Value]; exists {
					typeName = t
				}
			}
			if typeName != "" {
				if fields, ok := validationStructFields[typeName]; ok {
					if fieldType, ok := fields[e.Property]; ok {
						return fieldType
					}
				}
			}
		}
		return ""
	case *parser.IndexExpression:
		// Array/slice element access: cannot reliably infer element type here
		return ""
	case *parser.SliceExpression:
		// Slicing [N]T returns []T; slicing str returns str
		if e.Left != nil {
			leftType := inferExprType(e.Left, varTypes, funcTypes, selfType)
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
	case *parser.GroupedExpression:
		return inferExprType(e.Expression, varTypes, funcTypes, selfType)
	case *parser.ConditionalExpression:
		// 三元運算子：從兩分支推斷型別
		consequenceType := inferExprType(e.Consequence, varTypes, funcTypes, selfType)
		alternativeType := inferExprType(e.Alternative, varTypes, funcTypes, selfType)
		if consequenceType == alternativeType && consequenceType != "" {
			return consequenceType
		}
		if consequenceType != "" {
			return consequenceType
		}
		return "i64"
	case *parser.StructLiteral:
		// A struct literal `name{}` has the type of the struct itself.
		if e.Type != "" {
			return e.Type
		}
		return "i64"
	case *parser.ArrayLiteral:
		// Array literal v[1, 2, ...] → infer type from elements
		if len(e.Elements) > 0 {
			elemType := inferExprType(e.Elements[0], varTypes, funcTypes, selfType)
			if elemType != "" {
				return fmt.Sprintf("[%d]%s", len(e.Elements), elemType)
			}
		}
		return "i64"
	case *parser.SliceLiteral:
		// Slice literal [1, 2, ...] → infer type from elements
		if len(e.Elements) > 0 {
			elemType := inferExprType(e.Elements[0], varTypes, funcTypes, selfType)
			if elemType != "" {
				return fmt.Sprintf("[]%s", elemType)
			}
		}
		return "i64"
	case *parser.FunctionLiteral:
		// Phase 1: anonymous function literals are typed with the simplified "fn" marker.
		// Phase 2 may derive the precise FunctionType signature.
		return "fn"
	case *parser.MapLiteral:
		// Map literal { k:v, ... } → infer type from associated MapType if present
		if e.MapType != nil {
			return e.MapType.String()
		}
		// Fallback: infer from first pair's key/value types
		if len(e.Pairs) > 0 {
			keyType := inferExprType(e.Pairs[0].Key, varTypes, funcTypes, selfType)
			valType := inferExprType(e.Pairs[0].Value, varTypes, funcTypes, selfType)
			if keyType != "" && valType != "" {
				return "[" + keyType + "]" + valType
			}
		}
		return ""
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
			if fns, has := overloads[name]; has && len(fns) >= 1 {
				// 收集實參類型
				argTypes := make([]string, len(e.Arguments))
				for i, arg := range e.Arguments {
					t := inferExprType(arg, varTypes, nil, "")
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
				parts := []string{name}
				for _, t := range argTypes {
					parts = append(parts, sanitizeTypeForName(t))
				}
				sig := strings.Join(parts, "_")
				if mangledName, ok := mangled[sig]; ok {
					ident.Value = mangledName
				} else {
					// 找不到精確匹配，嘗試最接近的重載（取第一個）
					if len(fns) > 0 {
						ident.Value = fns[0].Name
					}
				}
			}
		} else {
			// 方法調用 (receiver.method(args))：e.Function 是 DotExpression
			// 遞迴處理 receiver 中的嵌套調用（如 count(0).to-str()）
			updateCallNames(e.Function, overloads, mangled, varTypes)
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

	case *parser.DotExpression:
		// receiver.property：遞迴處理 receiver 中的嵌套調用
		updateCallNames(e.Receiver, overloads, mangled, varTypes)

	case *parser.IndexExpression:
		// arr[idx]：遞迴處理索引表達式中的嵌套調用
		updateCallNames(e.Left, overloads, mangled, varTypes)
		updateCallNames(e.Index, overloads, mangled, varTypes)

	case *parser.ConditionalExpression:
		// cond ? a : b：遞迴處理所有子表達式
		updateCallNames(e.Condition, overloads, mangled, varTypes)
		updateCallNames(e.Consequence, overloads, mangled, varTypes)
		updateCallNames(e.Alternative, overloads, mangled, varTypes)

	case *parser.IfExpression:
		if e.Condition != nil {
			updateCallNames(e.Condition, overloads, mangled, varTypes)
		}
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
	case *parser.MultiAssignStatement:
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
	llvmGenerator    *llvm.Generator
	pkg              *Package // 當前套件（用於路徑解析）
	sourcePath       string   // 當前編譯的源碼檔案路徑（用於 std 庫檢測）
	allowAnonymousFn bool     // 是否允許匿名函式型別參數（來自 mod.jsonc）

	// externFuncSigs/externStructFields: 預載入的跨文件函數簽名和 struct 欄位型別，
	// 注入到所有 parser 實例中以支援 let 型別推斷
	externFuncSigs     map[string][]string
	externStructFields map[string]map[string]string

	// targetGoos/targetGoarch: 編譯目標平台，用於平台變體過濾。
	// 空字串表示 fallback 到 runtime.GOOS/GOARCH（編譯主機平台）。
	targetGoos    string
	targetGoarch  string
	noBoundsCheck bool // skip bounds checks in generated code (unsafe mode)
}

func NewTranspiler(pkg *Package) *Transpiler {
	t := &Transpiler{
		llvmGenerator: llvm.NewGenerator(),
		pkg:           pkg,
	}
	if pkg != nil {
		t.allowAnonymousFn = pkg.Compiler.AnonymousFnType
	}
	return t
}

// SetTargetPlatform sets the target (GOOS, GOARCH) for platform-variant filtering
// during code generation. Empty strings fall back to the host runtime platform.
// This is propagated to the underlying LLVM generator before Generate is called.
func (t *Transpiler) SetTargetPlatform(goos, goarch string) {
	t.targetGoos = goos
	t.targetGoarch = goarch
}

// SetNoBoundsCheck configures whether bounds checks are skipped in generated code.
// When true (unsafe mode), array/slice/string indexing does not emit bounds checks.
func (t *Transpiler) SetNoBoundsCheck(skip bool) {
	t.noBoundsCheck = skip
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
	p.AllowAnonymousFnType = t.allowAnonymousFn
	p.Filename = filepath.Base(filePath)
	// 注入預載入的跨文件簽名，支援 let 型別推斷
	if t.externFuncSigs != nil || t.externStructFields != nil {
		p.SetExternSignatures(t.externFuncSigs, t.externStructFields)
	}
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
			return t.resolveFile(fullPath)
		}
		// 沒有套件配置，相對於當前目錄
		filePath := relPath + ".no"
		return t.resolveFile(filePath)
	}

	// std/ 開頭 → 標準庫路徑
	if strings.HasPrefix(path, "std/") || path == "std" {
		// 相對於語言根目錄的 src/std/
		// 使用硬編碼路徑或透過套件 alias
		if t.pkg != nil {
			// 嘗試透過 alias 解析（mod.jsonc 中的 @std）
			resolved := t.pkg.ResolvePath(path)
			if !strings.HasSuffix(resolved, ".no") {
				resolved = resolved + ".no"
			}
			if _, err := os.Stat(resolved); err == nil {
				return t.resolveFile(resolved)
			}
		}
		// 嘗試透過 GetStdSourceDir（讀取 NOLANG_STD_SRC 環境變量）
		// strip "std/" prefix to get module path relative to std/
		relPath := strings.TrimPrefix(path, "std/")
		if path == "std" {
			relPath = ""
		}
		stdFile := GetStdSourceFile(relPath)
		if _, err := os.Stat(stdFile); err == nil {
			return t.resolveFile(stdFile)
		}
		// Lookup table: match ShortPath to FullPath (e.g. "net" → "net/net")
		for _, info := range knownStdModules() {
			if info.ShortPath == relPath {
				stdFile := GetStdSourceFile(info.FullPath)
				if _, err := os.Stat(stdFile); err == nil {
					return t.resolveFile(stdFile)
				}
			}
		}
		// fallback: std/<module>.no 相對於執行目錄
		fallback := path + ".no"
		if _, err := os.Stat(fallback); err == nil {
			return t.resolveFile(fallback)
		}
		// 再試 src/std/<module>.no 相對於執行目錄
		srcPath := "src/" + path + ".no"
		if _, err := os.Stat(srcPath); err == nil {
			return t.resolveFile(srcPath)
		}
		return t.resolveFile(srcPath)
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
					return t.resolveFile(modPath)
				}
			}
		}
		// URL 風格的導入路徑但未在 dependencies 中宣告
		return nil, fmt.Errorf("dependency not found: %q is not declared in mod.jsonc dependencies", path)
	}

	// 非 std 路徑 → 透過 alias 解析
	if t.pkg != nil {
		modulePath := t.pkg.ResolvePath(path)
		if !strings.HasSuffix(modulePath, ".no") {
			modulePath = modulePath + ".no"
		}
		return t.resolveFile(modulePath)
	}

	// 沒有套件配置，直接嘗試
	filePath := path + ".no"
	return t.resolveFile(filePath)
}

// resolveFile parses a .no file and applies lib.no export filtering if present.
func (t *Transpiler) resolveFile(filePath string) (*parser.Program, error) {
	prog, err := t.parseFile(filePath)
	if err != nil {
		return nil, err
	}
	// Apply lib.no export filtering
	pkgRoot := findPackageRootFromFile(filePath)
	if pkgRoot != "" {
		libPath := filepath.Join(pkgRoot, "lib.no")
		if _, err := os.Stat(libPath); err == nil {
			prog = filterByExports(prog, libPath)
		}
	}
	return prog, nil
}

func (t *Transpiler) Compile(source string) (string, error) {
	return t.CompileTarget(source, TargetLLVM)
}

// preloadModuleSignatures 掃描源碼中的 use 語句，預載入模組的函數簽名和 struct 欄位型別。
// 這些簽名會注入到 parser 中，使 let 型別推斷能處理跨文件方法調用。
// 也預載入所有已知 std 模組的簽名，因為 transpiler 會自動載入這些模組。
func (t *Transpiler) preloadModuleSignatures(source string) (map[string][]string, map[string]map[string]string) {
	funcSigs := make(map[string][]string)
	structFields := make(map[string]map[string]string)
	loadedPaths := make(map[string]bool) // 避免重複載入同一模組

	// collectSignaturesFromProg 從已解析的 Program 中收集函數簽名和 struct 欄位
	collectSignaturesFromProg := func(modProg *parser.Program) {
		for _, stmt := range modProg.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if len(fd.Results) > 0 {
					rets := make([]string, len(fd.Results))
					for i, r := range fd.Results {
						rets[i] = r.Type.String()
					}
					funcSigs[fd.Name] = rets
				}
			}
			if sd, ok := stmt.(*parser.StructDefinition); ok {
				fields := make(map[string]string)
				for _, f := range sd.Fields {
					if typeStr := structFieldTypeString(f); typeStr != "" {
						fields[f.Name] = typeStr
					}
				}
				structFields[sd.Name] = fields
			}
		}
	}

	// 1. 掃描顯式 use/# 語句
	useRe := regexp.MustCompile(`(?m)^\s*(?:use|#)\s+([\w/.\-]+)`)
	matches := useRe.FindAllStringSubmatch(source, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawPath := m[1]
		// 嘗試解析模組路徑：先嘗試完整路徑，再嘗試去掉最後 .function 部分
		candidates := []string{rawPath}
		if idx := strings.LastIndex(rawPath, "."); idx > 0 {
			candidates = append(candidates, rawPath[:idx])
		}
		var modProg *parser.Program
		for _, usePath := range candidates {
			if loadedPaths[usePath] {
				modProg = nil
				break
			}
			fakeUse := &parser.UseStatement{Path: usePath}
			prog, err := t.resolveUse(fakeUse)
			if err == nil && prog != nil {
				modProg = prog
				loadedPaths[usePath] = true
				break
			}
		}
		if modProg != nil {
			collectSignaturesFromProg(modProg)
		}
	}

	// 2. 預載入所有已知 std 模組的簽名（transpiler 會自動載入這些模組）
	for _, info := range knownStdModules() {
		path := "std/" + info.ShortPath
		if loadedPaths[path] {
			continue
		}
		fakeUse := &parser.UseStatement{Path: path}
		prog, err := t.resolveUse(fakeUse)
		if err != nil || prog == nil {
			continue
		}
		loadedPaths[path] = true
		collectSignaturesFromProg(prog)
	}

	return funcSigs, structFields
}

func (t *Transpiler) CompileTarget(source string, _ Target) (string, error) {
	// 預載入跨文件模組簽名，供 parser 型別推斷使用
	externFuncSigs, externStructFields := t.preloadModuleSignatures(source)
	// 存儲到 Transpiler 中，使 parseFile（用於解析自動載入的模組）也能注入簽名
	t.externFuncSigs = externFuncSigs
	t.externStructFields = externStructFields

	l := lexer.New(source)
	p := parser.New(l)
	p.AllowAnonymousFnType = t.allowAnonymousFn
	if t.sourcePath != "" {
		p.Filename = filepath.Base(t.sourcePath)
	}
	// 注入外部簽名
	if len(externFuncSigs) > 0 || len(externStructFields) > 0 {
		p.SetExternSignatures(externFuncSigs, externStructFields)
	}
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
	// 如果 pkg 為 nil，檢查源碼檔案路徑是否為標準庫
	if isUserCode && t.sourcePath != "" {
		if strings.Contains(t.sourcePath, "src/std") || strings.Contains(t.sourcePath, "/std/") {
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
			// if len(fd.Results) > 0 {
			// 	return "", fmt.Errorf("result parameters fn()() are only allowed in standard library, not in user code (function: %s)", fd.Name)
			// }
		}
	}

	// 構建變數類型表
	// globalVarTypes 僅包含頂層 LetStatement（全域變數），用於泛型呼叫掃描時的
	// per-function varTypes 初始化，避免其他函數體內的同名局部變數污染型別查找。
	varTypes := make(map[string]string)
	globalVarTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
				globalVarTypes[ls.Name.Value] = ls.Type.String()
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
					globalVarTypes[ls.Name.Value] = t
				}
			}
		}
		// Also collect variable types from function bodies
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			// Add function parameter and result types so method resolution
			// can dispatch builtins correctly (e.g. `out.zero()` where out
			// is a `[32]byte` result parameter needs to find []byte.zero).
			for _, p := range fd.Parameters {
				if p.Type != nil {
					varTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fd.Results {
				if r.Type != nil {
					varTypes[r.Name] = r.Type.String()
				}
			}
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

	// ?T 輸出參數未初始化檢查（case6）
	if uninitErrs := ValidateUninitOutputParams(program); len(uninitErrs) > 0 {
		var msgs []string
		for _, e := range uninitErrs {
			msgs = append(msgs, fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message))
		}
		return "", fmt.Errorf("uninitialized output parameter errors: %s", strings.Join(msgs, "; "))
	}

	// 名稱修飾 pass：處理方法重載
	mangleOverloads(program, varTypes)

	// 自動 enter/leave：插入作用域生命週期調用
	injectEnterLeave(program)

	// 收集導入的模塊路徑（ShortName），用於後續的 module.fn() → fn() 重寫
	var importedModules []string
	// 預填充已知 std 模塊的 ShortName，允許 math.degrees()、base64.encode-std() 等呼叫無需顯式導入
	// 使用 ShortName（路徑最後一段）作為調用前綴
	for _, info := range knownStdModules() {
		importedModules = append(importedModules, info.ShortName)
	}
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			importedModules = append(importedModules, moduleShortName(use.Path))
		}
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
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
	// typeOwner 記錄跨模組型別定義的歸屬（bareName → moduleShortName），
	// 用於將導入模組的 struct/interface/enum 等型別定義加上模組前綴
	// （如 result → sql.result），避免與主檔案變數或型別衝突。
	typeOwner := make(map[string]string)
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			modProg, err := t.resolveUse(use)
			if err != nil {
				return "", fmt.Errorf("loading module %s: %w", use.Path, err)
			}
			if strings.HasPrefix(use.Path, "std/") || use.Path == "std" {
				explicitStdModules[use.Path] = true
			}
			// 為導入模組的型別定義加上模組前綴（如 result → sql.result）
			prefixModuleStatements(modProg.Statements, moduleShortName(use.Path), typeOwner)
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
								if isConstantExpr(ls.Value) && matchesTargetPlatform(ls.PlatformKeys, t.targetGoos, t.targetGoarch) {
									moduleConstants[ls.Name.Value] = ls.Value
								}
							}
						}
						// Skip other lets when alias is used
					} else {
						// 如果主程序已有同名變量，跳過以避免衝突
						if !mainVarNames[ls.Name.Value] {
							merged.Statements = append(merged.Statements, ls)
							if isConstantExpr(ls.Value) && matchesTargetPlatform(ls.PlatformKeys, t.targetGoos, t.targetGoarch) {
								moduleConstants[ls.Name.Value] = ls.Value
							}
						}
					}
				}
				if use.Alias == "" {
					if sd, ok := ms.(*parser.StructDefinition); ok {
						merged.Statements = append(merged.Statements, sd)
					}
					if id, ok := ms.(*parser.InterfaceDefinition); ok {
						merged.Statements = append(merged.Statements, id)
					}
					if ta, ok := ms.(*parser.TypeAlias); ok {
						merged.Statements = append(merged.Statements, ta)
					}
					if ed, ok := ms.(*parser.EnumDefinition); ok {
						merged.Statements = append(merged.Statements, ed)
					}
					if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
						merged.Statements = append(merged.Statements, ted)
					}
					// FFI extern 宣告必須隨模組一起合併，否則 codegen 的 externFuncs
					// 會缺少條目，導致 extern 呼叫走 Nolang by-reference 路徑而非 FFI 路徑。
					if es, ok := ms.(*parser.ExternStatement); ok {
						merged.Statements = append(merged.Statements, es)
					}
				}
			}
			continue
		}
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			merged.Statements = append(merged.Statements, stmt)
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			// FFI extern 宣告 — 收集至 merged 供後續 codegen 使用（目前尚未實作）
			merged.Statements = append(merged.Statements, es)
		}
	}

	// 自動載入已知 std 模組（允許無需顯式導入的 module.fn() 呼叫）
	for _, info := range knownStdModules() {
		// 如果變量名與模塊名衝突，跳過自動載入
		if _, isVar := varTypes[info.ShortName]; isVar {
			continue
		}
		path := "std/" + info.ShortPath
		if explicitStdModules[path] {
			continue
		}
		use := &parser.UseStatement{Path: path, Function: ""}
		modProg, err := t.resolveUse(use)
		if err != nil {
			return "", fmt.Errorf("auto-loading module %s: %w", path, err)
		}
		// 為自動載入模組的型別定義加上模組前綴（如 result → sql.result）
		prefixModuleStatements(modProg.Statements, info.ShortName, typeOwner)
		for _, ms := range modProg.Statements {
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				merged.Statements = append(merged.Statements, fd)
			}
			if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				// 如果主程序已有同名變量，跳過以避免衝突
				if !mainVarNames[ls.Name.Value] {
					merged.Statements = append(merged.Statements, ls)
					if isConstantExpr(ls.Value) && matchesTargetPlatform(ls.PlatformKeys, t.targetGoos, t.targetGoarch) {
						moduleConstants[ls.Name.Value] = ls.Value
					}
				}
			}
			if sd, ok := ms.(*parser.StructDefinition); ok {
				merged.Statements = append(merged.Statements, sd)
			}
			if id, ok := ms.(*parser.InterfaceDefinition); ok {
				merged.Statements = append(merged.Statements, id)
			}
			if ta, ok := ms.(*parser.TypeAlias); ok {
				merged.Statements = append(merged.Statements, ta)
			}
			if ed, ok := ms.(*parser.EnumDefinition); ok {
				merged.Statements = append(merged.Statements, ed)
			}
			if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
				merged.Statements = append(merged.Statements, ted)
			}
		}
	}

	// 第一階段型別參考改寫：將導入模組語句中的裸型別名改寫為 module.type 形式。
	// 必須在 resolveSelfMethodCalls 之前執行，因為 resolveSelfMethodCalls 透過
	// collectStructFields 以 struct 名稱為 key 查找，若型別定義已重命名為
	// module.name 但參考仍為裸名，會無法匹配。
	// 但先要完成跨模組方法名稱重命名：一個方法可能定義在 B 模組但接收者型別
	// 定義在 A 模組（如 bufio.no 的 reader.fill，reader 定義在 io.no）。
	// 此時 typeOwner 已包含所有模組的型別歸屬，可以正確重命名。
	prefixMethodNames(merged.Statements, typeOwner)
	rewriteTypeRefs(merged.Statements, typeOwner)

	// 常量傳播：將模組常量替換為字面值，使 module functions 可以直接使用常量
	resolveModuleConstants(merged, moduleConstants)

	// 解析 module.fn() 呼叫：將 DotExpression 重寫為 Identifier
	// 必須在 monomorphizeGenerics 之前執行，以便泛型模組函數也能被正確處理
	resolveModuleCalls(merged, importedModules)

	// 解析 self.method() 呼叫：將方法體內的 self.method(args) 重寫為 Type.method(self, args)
	resolveSelfMethodCalls(merged)

	// 泛型單態化：掃描泛型函數呼叫，生成具體版本
	// 使用 globalVarTypes（僅頂層變數）避免其他函數的局部變數型別洩漏到 method resolution
	monomorphizeGenerics(merged, globalVarTypes)

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
	// 必須在 monomorphizeUnions/rewriteUnionCalls 之前添加，
	// 否則頂層呼叫（如 pow(2, 10, r)）不會被重寫為具體版本
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			continue
		}
		if _, ok := stmt.(*parser.UseStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExternStatement); ok {
			// extern 為宣告，非頂層可執行語句（已於前述步驟收集至 merged）
			continue
		}
		// Convert MultiAssignStatement to old nested-call syntax for codegen
		if mas, ok := stmt.(*parser.MultiAssignStatement); ok {
			if innerCall, ok := mas.Value.(*parser.CallExpression); ok {
				// Create: innerCall(outerArgs) with outerArgs being the target expressions
				outerCall := &parser.CallExpression{
					Token:     innerCall.Token,
					Function:  innerCall,
					Arguments: mas.Targets,
				}
				merged.Statements = append(merged.Statements, &parser.ExpressionStatement{Expression: outerCall})
			}
			continue
		}
		merged.Statements = append(merged.Statements, stmt)
	}

	// 第二階段型別參考改寫：主檔案的頂層語句（struct 定義、let 宣告等）
	// 在此時才加入 merged，需要再次改寫以處理主檔案中對導入模組型別的引用。
	// 已改寫過的型別名（含 "."）會被 prefixTypeName 自動跳過，安全無副作用。
	rewriteTypeRefs(merged.Statements, typeOwner)

	// 解析頂層代碼中的 module.fn() 呼叫
	resolveModuleCalls(merged, importedModules)

	// 解析 Type.method(args) 靜態方法呼叫：將 bigint.cmp(d, d2) 重寫為
	// bigint.bigint.cmp(d, d2)，與 prefixMethodNames 重命名後的方法定義對齊。
	// 必須在 resolveSelfMethodCalls 之後執行（該 pass 已用正確的 module 前綴
	// 生成 Type.method(self, args)），並在所有頂層代碼加入 merged 之後執行。
	resolveMethodCalls(merged, typeOwner)

	// 泛型結構體單態化：掃描 map[K]V 使用點，自 hashmap-*-tmpl 模板生成具體結構與方法。
	// 必須在 monomorphizeGenerics 之後（避免與 [n]t 泛型衝突）、monomorphizeUnions 之前執行。
	monomorphizeGenericStructs(merged)

	// 聯合型別單態化：對帶 ..T（T 為 union alias）的函數，
	// 為 union 的每個具體型別生成一個函數版本。生成函數的命名
	// 採用 "<原名>__<成員型別>" 的形式；對函數體內對自己的呼叫也
	// 一併替換。
	monomorphizeUnions(merged)

	// 重寫對聯合型別泛型函數的呼叫：將 max(args) 改為 max__i64(args)
	rewriteUnionCalls(merged, varTypes)

	// 在合併所有 std 模組後再做一次名稱修飾，
	// 處理跨模組的重載衝突（如 bigint.div-mod vs number.div-mod）
	mangleOverloads(merged, nil)

	// 編譯期未初始化變數檢查：循環體內聲明的變數在循環外使用
	// 必須在模組合併後執行，才能檢查到導入模組（如 md5.no）中的問題
	if err := validateLoopScopedVars(merged); err != nil {
		return "", err
	}

	// 傳播目標平台到 LLVM generator，讓 Generate 內部的平台過濾使用目標平台
	// 而非編譯主機平台（支援交叉編譯）。
	t.llvmGenerator.SetTargetPlatform(t.targetGoos, t.targetGoarch)
	t.llvmGenerator.SetNoBoundsCheck(t.noBoundsCheck)
	// 傳遞主檔案名稱集合，讓 generator 能區分主檔案全域變數的合法重新賦值
	// 與導入模組函數中的同名局部變數（如 bigint.cmp 中的 result 不應誤寫到 @result）
	t.llvmGenerator.SetMainFileNames(mainVarNames)
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
		// Also handle IfExpression (e.g. `if cond { ... }` as a statement),
		// whose Condition may contain method calls (e.g. `elif path.starts-with(x)`).
		if ie, ok := s.Expression.(*parser.IfExpression); ok {
			scanIfExpressionForGenericCalls(ie, genericFns, varTypes, program, newStmts)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			// Build per-function varTypes to avoid cross-function name pollution.
			// The global varTypes is shared across all functions, so a local variable
			// named `resp` of type `str` in one function would pollute the lookup for
			// a parameter named `resp` of type `http-response` in another function.
			funcVarTypes := make(map[string]string)
			for k, v := range varTypes {
				funcVarTypes[k] = v
			}
			for _, p := range s.Parameters {
				if p.Type != nil {
					funcVarTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range s.Results {
				if r.Name != "" && r.Type != nil {
					funcVarTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(s.Body, funcVarTypes)
			for _, bodyStmt := range s.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, funcVarTypes, program, newStmts)
			}
		}
	case *parser.LetStatement:
		// Method definitions: type.method = (params) { ... }
		// These are LetStatements with FunctionLiteral values; scan their bodies
		// for method calls that need resolution.
		if fl, ok := s.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
			funcVarTypes := make(map[string]string)
			for k, v := range varTypes {
				funcVarTypes[k] = v
			}
			for _, p := range fl.Parameters {
				if p.Type != nil {
					funcVarTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fl.Results {
				if r.Name != "" && r.Type != nil {
					funcVarTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fl.Body, funcVarTypes)
			for _, bodyStmt := range fl.Body.Statements {
				scanStmtForGenericCalls(bodyStmt, genericFns, funcVarTypes, program, newStmts)
			}
		} else if s.Value != nil {
			// Regular variable assignment: scan the value expression
			// for method calls that need resolution (e.g., s = "A".to-str())
			scanExprForGenericCalls(s.Value, genericFns, varTypes, program, newStmts)
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

// scanIfExpressionForGenericCalls recursively scans an IfExpression's Condition,
// Consequence, and Alternative for method calls that need resolution.
func scanIfExpressionForGenericCalls(ie *parser.IfExpression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement) {
	if ie.Condition != nil {
		scanExprForGenericCalls(ie.Condition, genericFns, varTypes, program, newStmts)
	}
	if ie.Consequence != nil {
		for _, s := range ie.Consequence.Statements {
			scanStmtForGenericCalls(s, genericFns, varTypes, program, newStmts)
		}
	}
	if ie.Alternative != nil {
		for _, s := range ie.Alternative.Statements {
			scanStmtForGenericCalls(s, genericFns, varTypes, program, newStmts)
		}
	}
}

// scanExprForGenericCalls recursively walks an expression tree to find
// CallExpressions (including method calls) that need generic/method resolution.
func scanExprForGenericCalls(expr parser.Expression, genericFns map[string]*parser.FunctionDefinition,
	varTypes map[string]string, program *parser.Program, newStmts *[]parser.Statement) {
	switch e := expr.(type) {
	case *parser.CallExpression:
		processCallExpression(e, genericFns, varTypes, program, newStmts)
		for _, arg := range e.Arguments {
			scanExprForGenericCalls(arg, genericFns, varTypes, program, newStmts)
		}
	case *parser.InfixExpression:
		scanExprForGenericCalls(e.Left, genericFns, varTypes, program, newStmts)
		scanExprForGenericCalls(e.Right, genericFns, varTypes, program, newStmts)
	case *parser.PrefixExpression:
		scanExprForGenericCalls(e.Right, genericFns, varTypes, program, newStmts)
	case *parser.IfExpression:
		scanIfExpressionForGenericCalls(e, genericFns, varTypes, program, newStmts)
	case *parser.GroupedExpression:
		scanExprForGenericCalls(e.Expression, genericFns, varTypes, program, newStmts)
	}
}

// monomorphizeUnions 對聯合型別（union type alias）進行單態化。
// 對每個帶有 ..T（T 為 union alias）的函數（variadic），或參數/結果
// 使用 union alias 的非 variadic 函數，生成 N 個函數（每個 union
// 成員一個），函數名為 "<原名>__<成員>"。原函數定義保留作為「範本」
// 供後續步驟識別用途，但會在 codegen 階段被跳過（靠 IsVariadic &&
// VariadicUnion != "" 判斷；或 GenericUnion != "" 判斷）。
func monomorphizeUnions(program *parser.Program) {
	aliases, _ := ValidateUnionTypes(program)
	if os.Getenv("NOLANG_UNION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[union-debug] monomorphizeUnions: %d aliases, %d statements\n", len(aliases), len(program.Statements))
		typeAliasCount := 0
		for _, stmt := range program.Statements {
			if ta, ok := stmt.(*parser.TypeAlias); ok {
				typeAliasCount++
				fmt.Fprintf(os.Stderr, "[union-debug]   TypeAlias: name=%s isUnion=%v\n", ta.Name, ta.IsUnion())
			}
		}
		fmt.Fprintf(os.Stderr, "[union-debug]   total TypeAlias statements: %d\n", typeAliasCount)
		for name, ta := range aliases {
			fmt.Fprintf(os.Stderr, "[union-debug]   alias %s: isUnion=%v\n", name, ta.IsUnion())
		}
		for _, stmt := range program.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				fmt.Fprintf(os.Stderr, "[union-debug]   func %s: IsVariadic=%v VariadicUnion=%q GenericUnion=%q\n",
					fd.Name, fd.IsVariadic, fd.VariadicUnion, fd.GenericUnion)
			}
		}
	}
	if len(aliases) == 0 {
		return
	}

	// 收集所有需要單態化的函數
	type pending struct {
		fd        *parser.FunctionDefinition
		unionName string
		members   []parser.Type
	}
	var pendingFns []pending
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var unionName string
		if fd.IsVariadic && fd.VariadicUnion != "" {
			unionName = fd.VariadicUnion
		} else if !fd.IsVariadic && fd.GenericUnion != "" {
			unionName = fd.GenericUnion
		}
		if unionName == "" {
			if os.Getenv("NOLANG_UNION_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[union-debug] skip %s: IsVariadic=%v VariadicUnion=%q GenericUnion=%q\n",
					fd.Name, fd.IsVariadic, fd.VariadicUnion, fd.GenericUnion)
			}
			continue
		}
		if os.Getenv("NOLANG_UNION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[union-debug] monomorphize %s: union=%s\n", fd.Name, unionName)
		}
		members := FlattenUnion(unionName, aliases)
		if len(members) == 0 {
			continue
		}
		pendingFns = append(pendingFns, pending{fd: fd, unionName: unionName, members: members})
	}

	if len(pendingFns) == 0 {
		return
	}

	var newStmts []parser.Statement
	for _, p := range pendingFns {
		for _, mem := range p.members {
			nt, ok := mem.(*parser.NamedType)
			if !ok {
				continue
			}
			concrete := cloneUnionVariant(p.fd, nt.Value, aliases)
			newStmts = append(newStmts, concrete)
		}
		// 標記原函數為「範本」：在 name 末尾加 __TEMPLATE 使其不與生成版本衝突
		p.fd.Name = p.fd.Name + "__" + p.unionName + "_TEMPLATE"
	}
	program.Statements = append(program.Statements, newStmts...)
}

// rewriteUnionCalls 重寫對聯合型別泛型函數的呼叫。
// 在 monomorphizeUnions 之後，原函數被改名為 "<name>__<union>_TEMPLATE"，
// 具體版本為 "<name>__<memberType>"。此函數遍歷所有呼叫點，
// 根據引數型別推斷應使用的具體版本，並將呼叫名改寫為 "<name>__<memberType>"。
func rewriteUnionCalls(program *parser.Program, varTypes map[string]string) {
	// 收集所有模板函數：原名 → templateInfo
	// 模板名格式：<origName>__<unionName>_TEMPLATE
	templates := make(map[string]*unionTemplateInfo)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		if strings.HasSuffix(fd.Name, "_TEMPLATE") {
			base := strings.TrimSuffix(fd.Name, "_TEMPLATE")
			parts := strings.SplitN(base, "__", 2)
			if len(parts) != 2 {
				continue
			}
			origName := parts[0]
			unionName := parts[1]
			aliases, _ := ValidateUnionTypes(program)
			members := FlattenUnion(unionName, aliases)
			memberNames := make([]string, 0, len(members))
			for _, m := range members {
				if nt, ok := m.(*parser.NamedType); ok {
					memberNames = append(memberNames, nt.Value)
				}
			}
			templates[origName] = &unionTemplateInfo{origName: origName, unionName: unionName, members: memberNames}
		}
	}

	if len(templates) == 0 {
		if os.Getenv("NOLANG_UNION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[union-debug] rewriteUnionCalls: no templates found\n")
		}
		return
	}
	if os.Getenv("NOLANG_UNION_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[union-debug] rewriteUnionCalls: %d templates\n", len(templates))
		for name, tpl := range templates {
			fmt.Fprintf(os.Stderr, "[union-debug]   template %s: union=%s members=%v\n", name, tpl.unionName, tpl.members)
		}
	}

	// 遍歷所有語句，重寫呼叫
	rewriteUnionCallStmts(program.Statements, templates, varTypes)
}

// rewriteUnionCallStmts 遍歷語句列表，對每個語句中的聯合型別泛型呼叫進行重寫。
// 此函數與 rewriteUnionCallExpr 互相遞迴：rewriteUnionCallExpr 處理 IfExpression
// 的 Consequence/Alternative 時會呼叫本函數，以正確走訪所有語句類型
// （包括 LetStatement、ReturnStatement 等，而不僅是 ExpressionStatement）。
func rewriteUnionCallStmts(stmts []parser.Statement, templates map[string]*unionTemplateInfo, vt map[string]string) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.ExpressionStatement:
			rewriteUnionCallExpr(s.Expression, templates, vt)
		case *parser.LetStatement:
			if s.Value != nil {
				rewriteUnionCallExpr(s.Value, templates, vt)
			}
		case *parser.FunctionDefinition:
			if os.Getenv("NOLANG_UNION_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[union-debug] walk FunctionDefinition: %s, body=%d stmts\n", s.Name, len(s.Body.Statements))
			}
			if s.Body != nil {
				// Augment varTypes with the function's parameter types to
				// correctly infer argument types for identifier expressions.
				// This prevents cross-module template matching (e.g. bigint.gcd
				// should not be rewritten by the number.gcd template).
				localVt := make(map[string]string)
				for k, v := range vt {
					localVt[k] = v
				}
				for _, param := range s.Parameters {
					if nt, ok := param.Type.(*parser.NamedType); ok {
						localVt[param.Name] = nt.Value
					}
				}
				for _, result := range s.Results {
					if nt, ok := result.Type.(*parser.NamedType); ok {
						localVt[result.Name] = nt.Value
					}
				}
				// Also collect local LetStatement types so that variables shadowing
				// globals or same-name locals from other functions (e.g. `c` in
				// dns-parse-records vs `c []str` elsewhere) resolve to the correct
				// union member type. Without this, a single global varTypes entry
				// would leak across functions and produce wrong memberType inference.
				collectVarTypesFromBody(s.Body, localVt)
				rewriteUnionCallStmts(s.Body.Statements, templates, localVt)
			}
		case *parser.BlockStatement:
			rewriteUnionCallStmts(s.Statements, templates, vt)
		case *parser.ForStatement:
			if s.Condition != nil {
				rewriteUnionCallExpr(s.Condition, templates, vt)
			}
			if s.Body != nil {
				rewriteUnionCallStmts(s.Body.Statements, templates, vt)
			}
		case *parser.MultiAssignStatement:
			if s.Value != nil {
				rewriteUnionCallExpr(s.Value, templates, vt)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				rewriteUnionCallExpr(s.ReturnValue, templates, vt)
			}
		}
	}
}

// unionTemplateInfo 記錄聯合型別模板函數的資訊
type unionTemplateInfo struct {
	origName  string
	unionName string
	members   []string
}

// rewriteUnionCallExpr 遞迴重寫表達式中的聯合型別呼叫
func rewriteUnionCallExpr(expr parser.Expression, templates map[string]*unionTemplateInfo, varTypes map[string]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// 先遞迴處理引數中的呼叫
		for _, arg := range e.Arguments {
			rewriteUnionCallExpr(arg, templates, varTypes)
		}
		// 處理 curried 呼叫：(innerCall)(args)
		if _, ok := e.Function.(*parser.CallExpression); ok {
			rewriteUnionCallExpr(e.Function, templates, varTypes)
			return
		}
		// 檢查是否為聯合型別泛型呼叫
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if tpl, exists := templates[ident.Value]; exists {
				memberType := inferArgMemberType(e, tpl, varTypes)
				if os.Getenv("NOLANG_UNION_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[union-debug] rewrite call %s: memberType=%s\n", ident.Value, memberType)
				}
				if memberType != "" {
					// Verify memberType is a valid member of the template's union.
					// This prevents incorrectly rewriting calls from different
					// modules that share the same function name (e.g. bigint.gcd
					// should not be rewritten by the number.gcd template).
					isValid := false
					for _, m := range tpl.members {
						if m == memberType {
							isValid = true
							break
						}
					}
					if isValid {
						ident.Value = ident.Value + "__" + memberType
						if os.Getenv("NOLANG_UNION_DEBUG") != "" {
							fmt.Fprintf(os.Stderr, "[union-debug]   rewritten to %s\n", ident.Value)
						}
					}
				}
			} else {
				// Not found by plain name; try method-style resolution
				// e.g., "sign" → "num.sign"
				for tplName, tpl := range templates {
					if strings.HasSuffix(tplName, "."+ident.Value) {
						// Found a method template, rewrite the identifier to use it
						memberType := inferArgMemberType(e, tpl, varTypes)
						if memberType != "" {
							isValid := false
							for _, m := range tpl.members {
								if m == memberType {
									isValid = true
									break
								}
							}
							if isValid {
								ident.Value = tplName + "__" + memberType
								if os.Getenv("NOLANG_UNION_DEBUG") != "" {
									fmt.Fprintf(os.Stderr, "[union-debug]   rewritten (method) %s → %s\n", ident.Value, ident.Value)
								}
								break
							}
						}
					}
				}
			}
		}
	case *parser.InfixExpression:
		rewriteUnionCallExpr(e.Left, templates, varTypes)
		rewriteUnionCallExpr(e.Right, templates, varTypes)
	case *parser.PrefixExpression:
		rewriteUnionCallExpr(e.Right, templates, varTypes)
	case *parser.IfExpression:
		if e.Condition != nil {
			rewriteUnionCallExpr(e.Condition, templates, varTypes)
		}
		// 走訪 Consequence/Alternative 中的所有語句類型（不僅 ExpressionStatement），
		// 以正確重寫 LetStatement（如 `blen-str = body-len.to-str()`）等內部的呼叫。
		// 僅走 ExpressionStatement 會導致 standalone if-then (`cond -> { let = call() }`)
		// 內的聯合型別呼叫未被重寫，產生 undefined `@int.to-str` 錯誤。
		if e.Consequence != nil {
			rewriteUnionCallStmts(e.Consequence.Statements, templates, varTypes)
		}
		if e.Alternative != nil {
			rewriteUnionCallStmts(e.Alternative.Statements, templates, varTypes)
		}
	case *parser.AssignExpression:
		rewriteUnionCallExpr(e.Value, templates, varTypes)
	}
}

// inferArgMemberType 從呼叫引數推斷應使用的聯合成員型別
func inferArgMemberType(call *parser.CallExpression, tpl *unionTemplateInfo, varTypes map[string]string) string {
	if len(call.Arguments) == 0 {
		return ""
	}
	// 對於 variadic 函數（..num），使用第一個引數的型別
	// 對於非 variadic 函數（abs(a num)），也使用第一個引數的型別
	firstArg := call.Arguments[0]
	return inferExprMemberType(firstArg, varTypes)
}

// inferExprMemberType 從表達式推斷聯合成員型別
func inferExprMemberType(expr parser.Expression, varTypes map[string]string) string {
	switch v := expr.(type) {
	case *parser.IntegerLiteral:
		return "i64" // 整數字面常量預設為 i64
	case *parser.FloatLiteral:
		return "f64"
	case *parser.Identifier:
		// Look up the variable's actual type, default to i64
		if varTypes != nil {
			if t, ok := varTypes[v.Value]; ok {
				return t
			}
		}
		return "i64"
	case *parser.PrefixExpression:
		return inferExprMemberType(v.Right, varTypes)
	case *parser.GroupedExpression:
		return inferExprMemberType(v.Expression, varTypes)
	}
	return "i64"
}

// inferTypeFromExpr 嘗試從值表達式推斷變數型別。無法推斷時返回空白字串。
func inferTypeFromExpr(expr parser.Expression) string {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		// 十六進位字面量（0xNN）推斷為 byte，十進位整數推斷為 i64
		raw := e.Token.Literal
		if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
			return "byte"
		}
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.RegexLiteral:
		return "regexp"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.StructLiteral:
		return e.Type
	case *parser.PrefixExpression:
		if e.Operator == "-" || e.Operator == "+" {
			return inferTypeFromExpr(e.Right)
		}
		if e.Operator == "!" {
			return "bool"
		}
	case *parser.InfixExpression:
		// Comparison and logical operators always produce bool
		switch e.Operator {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "bool"
		}
		if t := inferTypeFromExpr(e.Left); t != "" {
			return t
		}
		return inferTypeFromExpr(e.Right)
	case *parser.GroupedExpression:
		return inferTypeFromExpr(e.Expression)
	case *parser.ConditionalExpression:
		// `cond ? a : b` — type is the type of the consequence (or alternative)
		if t := inferTypeFromExpr(e.Consequence); t != "" {
			return t
		}
		return inferTypeFromExpr(e.Alternative)
	case *parser.MapLiteral:
		if e.MapType != nil {
			return e.MapType.String()
		}
		if len(e.Pairs) > 0 {
			keyType := inferTypeFromExpr(e.Pairs[0].Key)
			valType := inferTypeFromExpr(e.Pairs[0].Value)
			if keyType != "" && valType != "" {
				return "[" + keyType + "]" + valType
			}
		}
		return ""
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "char-to-str":
				return "str"
			case "i64-to-str":
				return "str"
			case "f64-to-str":
				return "str"
			case "bool-to-str":
				return "str"
			case "byte-to-str":
				return "str"
			case "load-le-u16":
				return "u16"
			case "load-le-u32":
				return "u32"
			case "load-le-u64":
				return "u64"
			}
		}
		// Method call: receiver.load-le-u32(off) → DotExpression
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			switch dot.Property {
			case "load-le-u16":
				return "u16"
			case "load-le-u32":
				return "u32"
			case "load-le-u64":
				return "u64"
			}
		}
	}
	return ""
}

// isValidType 檢查是否為有效的 Nolang 型別名
func isValidType(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64":
		return true
	}
	return false
}

// cloneUnionVariant 為 union 函數的某個成員型別複製一份具體實例。
// 替換函數簽名中的 variadic 元素型別為該成員（若為 variadic），
// 並將所有 union 別名的參數/結果型別替換為具體成員型別。
// 對於函數體內所有對「自己」的遞迴呼叫改名為具體版本。
func cloneUnionVariant(fd *parser.FunctionDefinition, memberType string, aliases map[string]*parser.TypeAlias) *parser.FunctionDefinition {
	// 簡單深拷貝：先淺拷貝結構體，再逐欄位拷貝容器。
	clone := *fd
	clone.Parameters = make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		pCopy := *p
		if i == len(fd.Parameters)-1 && fd.IsVariadic {
			// 最後一個參數是 variadic；元素型別改為具體成員
			var tok lexer.Token
			// Use the underlying type's token if available, otherwise the param token
			if p.Type != nil {
				if st, ok := p.Type.(*parser.SliceType); ok {
					tok = st.Token
				}
			}
			if tok.Type == 0 {
				tok = p.Token
			}
			pCopy.Type = &parser.SliceType{
				Token: tok,
				Elem:  &parser.NamedType{Value: memberType},
			}
		} else {
			// 非 variadic 參數：若型別是 union 別名，替換為具體成員
			if nt, ok := p.Type.(*parser.NamedType); ok {
				if _, isUnion := aliases[nt.Value]; isUnion {
					pCopy.Type = &parser.NamedType{Value: memberType}
				}
			}
		}
		clone.Parameters[i] = &pCopy
	}
	clone.Results = make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		rCopy := *r
		// 若結果型別是 union 別名，替換為具體成員型別
		if nt, ok := r.Type.(*parser.NamedType); ok {
			if _, isUnion := aliases[nt.Value]; isUnion {
				rCopy.Type = &parser.NamedType{Value: memberType}
			}
		}
		clone.Results[i] = &rCopy
	}
	clone.Name = fd.Name + "__" + memberType
	// 重設 union 標記：實例化後該函數就是具體的
	clone.VariadicUnion = ""
	clone.GenericUnion = ""
	// 深拷貝 Body
	clone.Body = cloneBlockForUnion(fd.Body, fd.Name, clone.Name, memberType)
	return &clone
}

// cloneBlockForUnion 深拷貝一個 block，遞迴地把對 <oldName> 的呼叫
// 改名為 <newName>。<memberType> 是當前單態化的具體型別。
func cloneBlockForUnion(bs *parser.BlockStatement, oldName, newName, memberType string) *parser.BlockStatement {
	if bs == nil {
		return nil
	}
	out := &parser.BlockStatement{Token: bs.Token, RBrace: bs.RBrace}
	for _, s := range bs.Statements {
		out.Statements = append(out.Statements, cloneStmtForUnion(s, oldName, newName, memberType))
	}
	return out
}

func cloneStmtForUnion(stmt parser.Statement, oldName, newName, memberType string) parser.Statement {
	if stmt == nil {
		return nil
	}
	// IfExpression 在源碼中是 *parser.ExpressionStatement 包裝的
	// IfExpression（因為 IfExpression 實現了 Expression 而非 Statement）。
	// 我們在 ExpressionStatement case 內處理遞歸；不再單獨 case *IfExpression。
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		// shallow-copy the wrapper and rewrite its expression
		es := *s
		es.Expression = cloneExprForUnion(s.Expression, oldName, newName, memberType)
		return &es
	case *parser.LetStatement:
		ls := *s
		if s.Name != nil {
			n := *s.Name
			ls.Name = &n
		}
		ls.Type = s.Type
		ls.Value = cloneExprForUnion(s.Value, oldName, newName, memberType)
		return &ls
	case *parser.BlockStatement:
		return cloneBlockForUnion(s, oldName, newName, memberType)
	case *parser.ForStatement:
		fs := *s
		if s.IterRange != nil {
			fs.IterRange = cloneIterForUnion(s.IterRange, oldName, newName, memberType)
		}
		if s.Condition != nil {
			fs.Condition = cloneExprForUnion(s.Condition, oldName, newName, memberType)
		}
		fs.Body = cloneBlockForUnion(s.Body, oldName, newName, memberType)
		return &fs
	case *parser.ReturnStatement:
		rs := *s
		rs.ReturnValue = cloneExprForUnion(s.ReturnValue, oldName, newName, memberType)
		return &rs
	case *parser.MultiAssignStatement:
		mas := *s
		mas.Targets = append([]parser.Expression{}, s.Targets...)
		mas.Value = cloneExprForUnion(s.Value, oldName, newName, memberType)
		return &mas
	}
	// Fallback: shallow copy via type assertion to the concrete type
	return stmt
}

func cloneIterForUnion(it *parser.IterationExpr, oldName, newName, memberType string) *parser.IterationExpr {
	if it == nil {
		return nil
	}
	cp := *it
	if it.Range != nil {
		// RangeExpression has Start and End
		cp.Range = cloneRangeForUnion(it.Range, oldName, newName, memberType)
	}
	if it.RangeExpr != nil {
		cp.RangeExpr = cloneExprForUnion(it.RangeExpr, oldName, newName, memberType)
	}
	return &cp
}

func cloneRangeForUnion(r *parser.RangeExpression, oldName, newName, memberType string) *parser.RangeExpression {
	if r == nil {
		return nil
	}
	cp := *r
	if r.Start != nil {
		cp.Start = cloneExprForUnion(r.Start, oldName, newName, memberType)
	}
	if r.End != nil {
		cp.End = cloneExprForUnion(r.End, oldName, newName, memberType)
	}
	return &cp
}

func cloneExprForUnion(expr parser.Expression, oldName, newName, memberType string) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		ce := *e
		ce.Function = cloneExprForUnion(e.Function, oldName, newName, memberType)
		ce.Arguments = make([]parser.Expression, len(e.Arguments))
		for i, a := range e.Arguments {
			ce.Arguments[i] = cloneExprForUnion(a, oldName, newName, memberType)
		}
		return &ce
	case *parser.Identifier:
		cp := *e
		if cp.Value == oldName {
			cp.Value = newName
		}
		return &cp
	case *parser.DotExpression:
		de := *e
		de.Receiver = cloneExprForUnion(e.Receiver, oldName, newName, memberType)
		return &de
	case *parser.IfExpression:
		ie := *e
		ie.Condition = cloneExprForUnion(e.Condition, oldName, newName, memberType)
		ie.Consequence = cloneBlockForUnion(e.Consequence, oldName, newName, memberType)
		if e.Alternative != nil {
			ie.Alternative = cloneBlockForUnion(e.Alternative, oldName, newName, memberType)
		}
		return &ie
	case *parser.InfixExpression:
		ie := *e
		ie.Left = cloneExprForUnion(e.Left, oldName, newName, memberType)
		ie.Right = cloneExprForUnion(e.Right, oldName, newName, memberType)
		return &ie
	case *parser.PrefixExpression:
		pe := *e
		pe.Right = cloneExprForUnion(e.Right, oldName, newName, memberType)
		return &pe
	}
	return expr
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
		resolveMethodCall(dot, ce, genericFns, varTypes, newStmts, program)
	}

	// Recurse into arguments
	for _, arg := range ce.Arguments {
		if innerCe, ok := arg.(*parser.CallExpression); ok {
			processCallExpression(innerCe, genericFns, varTypes, program, newStmts)
		}
	}
}

// fnExistsInProgram checks if a function or method with the given name exists
// in the program's top-level statements. Method definitions (e.g. f64.to-str,
// int.to-str) are stored as *parser.FunctionDefinition with the full dotted
// name, so a simple Name match suffices.
func fnExistsInProgram(program *parser.Program, name string) bool {
	if program == nil {
		return false
	}
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == name {
			return true
		}
	}
	return false
}

// resolveMethodCall resolves a DotExpression-based method call.
// Returns true if the call was resolved and rewritten.
func resolveMethodCall(dot *parser.DotExpression, ce *parser.CallExpression,
	genericFns map[string]*parser.FunctionDefinition, varTypes map[string]string,
	newStmts *[]parser.Statement, program *parser.Program) bool {

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
	// Map types use "hashmap-K-V" naming convention (not "[K]V")
	// Option type (?T): method is defined on the inner type T, not on option.
	// Strip the leading "?" so concreteName becomes "T.method" (e.g. ?str.to-lower → str.to-lower).
	recvTypeForMethod := recvType
	if strings.HasPrefix(recvTypeForMethod, "?") {
		recvTypeForMethod = recvTypeForMethod[1:]
	}
	concreteName := recvTypeForMethod + "." + methodName
	if hmName := mapTypeToHashmapName(recvTypeForMethod); hmName != "" {
		concreteName = hmName + "." + methodName
	}

	// Don't rewrite if this is a builtin method (e.g. vec.push, vec.clear).
	// Builtins are registered with LLVM type prefixes (vec, arr, str, etc.),
	// not Nolang type names ([]str, []i64, etc.). The LLVM code generator
	// handles builtin method calls via DotExpression dispatch + ForwardFunc.
	// Rewriting them to []str.push(out, ...) would create undefined function calls.
	if strings.HasPrefix(recvType, "[]") {
		// Slice types ([]str, []i64, []byte, etc.) map to "vec" builtins.
		// Check both "vec.<method>" and the concrete type prefix.
		if builtin.FindBuiltinMethod("vec."+methodName) != nil {
			return false
		}
		// Also check the concrete name (e.g. []byte.slice is registered directly)
		if builtin.FindBuiltinMethod(concreteName) != nil {
			return false
		}
	}
	if strings.HasPrefix(recvType, "[") && !strings.HasPrefix(recvType, "[]") {
		// Array types ([N]T) map to "arr" builtins
		if builtin.FindBuiltinMethod("arr."+methodName) != nil {
			return false
		}
		// Some array builtins are registered under the slice name (e.g. []byte.zero
		// via ForwardFunc "arr-zero"); the codegen dispatches them for [N]T too.
		// Strip the size prefix so [32]byte.zero matches []byte.zero registration.
		if closeIdx := strings.IndexByte(recvType, ']'); closeIdx > 0 && closeIdx+1 < len(recvType) {
			elemType := recvType[closeIdx+1:]
			sliceBuiltinName := "[]" + elemType + "." + methodName
			if builtin.FindBuiltinMethod(sliceBuiltinName) != nil {
				return false
			}
		}
	}
	// Check concrete name for other types (str, i64, etc.)
	if builtin.FindBuiltinMethod(concreteName) != nil {
		return false
	}

	// Check if recvType is a member of a union type alias
	// If so, use the union alias prefix instead of the concrete type —
	// BUT only when the union method actually exists. When the union method
	// is not defined (e.g. float.to-str was removed in favor of f64.to-str),
	// keep the concrete type name so codegen can dispatch correctly.
	if program != nil {
		for _, stmt := range program.Statements {
			ta, ok := stmt.(*parser.TypeAlias)
			if !ok || ta.Union == nil {
				continue
			}
			for _, member := range ta.Union.Types {
				if nt, ok := member.(*parser.NamedType); ok && nt.Value == recvType {
					unionMethodName := ta.Name + "." + methodName
					if fnExistsInProgram(program, unionMethodName) {
						concreteName = unionMethodName
					}
					break
				}
			}
			if concreteName != recvType+"."+methodName {
				break
			}
		}
	}

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
	case *parser.RegexLiteral:
		return "regexp"
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
			Type:  substituteType(s.Type, subst),
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
			// 整數替換值（如陣列大小泛型 [n]t）：轉為 IntegerLiteral
			if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
				return &parser.IntegerLiteral{
					Token: e.Token,
					Value: intVal,
				}
			}
			// 非整數替換值（如方法名重寫 hashmap-*-tmpl.hash → hashmap-K-V.hash）：保留為 Identifier
			return &parser.Identifier{
				Token: e.Token,
				Value: val,
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
	case *parser.FunctionType:
		// Function types are not subject to generic substitution in Phase 1.
		return t
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
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
				} else {
					// Can't infer type (e.g., method call result) — delete any
					// stale entry inherited from another scope (e.g., a global
					// variable with the same name) to prevent wrong method resolution.
					delete(varTypes, ls.Name.Value)
				}
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
		// ExpressionStatement may wrap an IfExpression (e.g. `cond -> { body }`
		// match arms desugared to if/else). Recurse into its consequence and
		// alternative blocks so that local LetStatements inside match arms
		// shadow globals / outer-locals correctly during method resolution.
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			collectVarTypesFromExpr(es.Expression, varTypes)
		}
	}
}

// collectVarTypesFromExpr recurses into expression nodes that contain
// BlockStatement bodies (IfExpression, etc.) and collects local variable types.
func collectVarTypesFromExpr(expr parser.Expression, varTypes map[string]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.IfExpression:
		collectVarTypesFromBody(e.Consequence, varTypes)
		collectVarTypesFromBody(e.Alternative, varTypes)
		collectVarTypesFromBody(e.DotValBody, varTypes)
	case *parser.GroupedExpression:
		collectVarTypesFromExpr(e.Expression, varTypes)
	case *parser.ConditionalExpression:
		collectVarTypesFromExpr(e.Consequence, varTypes)
		collectVarTypesFromExpr(e.Alternative, varTypes)
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
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectArraySizesFromStmt(ss, sizes)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectArraySizesFromStmt(ss, sizes)
				}
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
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectSliceSizeMapFromStmt(ss, slices)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectSliceSizeMapFromStmt(ss, slices)
				}
			}
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
		if s.Type != nil && (s.Type.String() == "str") {
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		} else if isStringExprForCollect(s.Value, strSizes) {
			// Also detect string from inferred expression (StringLiteral, string concatenation,
			// string method calls like slice/repeat, char-to-str, copy from known string var)
			if sl, ok := s.Value.(*parser.StringLiteral); ok {
				strSizes[s.Name.Value] = int64(len(sl.Value))
			} else {
				strSizes[s.Name.Value] = 0 // unknown size, mark as string but no bound check
			}
		}
	case *parser.FunctionDefinition:
		// Add str parameters and results to stringSizes so they're recognized as strings
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				strSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				strSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, strSizes)
			}
		}
	case *parser.ExpressionStatement:
		// if/else 表達式中的局部变量也需收集
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				for _, ss := range ifExpr.Consequence.Statements {
					collectStringSizeMapFromStmt(ss, strSizes)
				}
			}
			if ifExpr.Alternative != nil {
				for _, ss := range ifExpr.Alternative.Statements {
					collectStringSizeMapFromStmt(ss, strSizes)
				}
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
// isStringExprForCollect is a stricter version of isStringExpr used during the
// string size map collection phase. Unlike isStringExpr (which defers unknown
// types to LLVM), this function only returns true for expressions that are
// DEFINITELY strings, avoiding false positives like struct field access (DotExpression)
// or array element access (IndexExpression) which may be non-string types.
func isStringExprForCollect(expr parser.Expression, strSizes map[string]int64) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.Identifier:
		_, exists := strSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExprForCollect(e.Expression, strSizes)
	case *parser.InfixExpression:
		// String concatenation: when both sides are strings, the result is a string
		if e.Operator == "-" {
			return isStringExprForCollect(e.Left, strSizes) && isStringExprForCollect(e.Right, strSizes)
		}
	case *parser.CallExpression:
		// Check if it's a method call on a known string receiver (e.g., s.slice(), s.repeat())
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if ident, ok := dot.Receiver.(*parser.Identifier); ok {
				if _, exists := strSizes[ident.Value]; exists {
					return true // method call on known string variable
				}
			}
			// 'literal'.method() — method call on string literal
			if _, ok := dot.Receiver.(*parser.StringLiteral); ok {
				return true
			}
		}
		// Check if it's a global builtin function call that returns str (e.g., char-to-str)
		if ident, ok := e.Function.(*parser.Identifier); ok {
			switch ident.Value {
			case "char-to-str":
				return true
			}
		}
	}
	return false
}

// isStringExpr checks if an expression is a string type
func isStringExpr(expr parser.Expression, stringSizes map[string]int64) bool {
	switch e := expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.NilLiteral:
		// nil can be assigned to ?str (option string) variables
		return true
	case *parser.Identifier:
		_, exists := stringSizes[e.Value]
		return exists
	case *parser.GroupedExpression:
		return isStringExpr(e.Expression, stringSizes)
	case *parser.InfixExpression:
		// String concatenation: when both sides are strings, the result is a string
		if e.Operator == "-" {
			return isStringExpr(e.Left, stringSizes) && isStringExpr(e.Right, stringSizes)
		}
	case *parser.CallExpression:
		// Function/method calls (e.g., i64-to-str, s.to-upper) may return strings.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.DotExpression:
		// Struct field access (e.g., fp.path) may return a string.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
	case *parser.IndexExpression:
		// Array element access (e.g., req.headers[i], arr[i]) may return a string.
		// Return type cannot be determined at validation time; defer to LLVM type checking.
		return true
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
		// In nolang, first assignment is definition, subsequent are reassignments
		// Check for duplicates when:
		//   1. There's an explicit type annotation in source (e.g., `i i64 = 0`), OR
		//   2. The name follows uppercase constant convention (e.g., `A = 0`, `SBOX = 1`)
		// Parser-inferred types (Type.Token position == Name.Token position) are treated as
		// "no explicit annotation" — but uppercase constants still trigger duplicate detection.
		hasExplicitType := s.Type != nil
		if hasExplicitType {
			if nt, ok := s.Type.(*parser.NamedType); ok {
				if nt.Token.Line == s.Name.Token.Line && nt.Token.Column == s.Name.Token.Column {
					// Parser-inferred type, not explicit annotation
					hasExplicitType = false
				}
			}
			// SliceType/ArrayType：parser 自動推導時 Token 與 nameToken 相同（同行同列），
			// 用戶顯式標註時 Token 是 '[' 位置。比較 Token 來區分推導 vs 顯式標註。
			// 這允許 slice/array 重新賦值（如 `local = [1, 2, 3]` 在已宣告後）。
			if st, ok := s.Type.(*parser.SliceType); ok {
				if st.Token.Line == s.Name.Token.Line && st.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			if at, ok := s.Type.(*parser.ArrayType); ok {
				if at.Token.Line == s.Name.Token.Line && at.Token.Column == s.Name.Token.Column {
					hasExplicitType = false
				}
			}
			if s.Type.String() == s.Name.Value {
				// Parser artifact: Type.String() == Name.Value
				hasExplicitType = false
			}
		}
		isConst := isConstantName(s.Name.Value)
		if !hasExplicitType && !isConst {
			return nil
		}
		// 使用複合 key：name + "\x00" + platformKey（無平台註解則 suffix 為空）
		// 同名 + 同平台才算衝突；不同平台或通用 vs 平台特定不衝突
		var dupKeys []string
		if len(s.PlatformKeys) == 0 {
			dupKeys = []string{s.Name.Value + "\x00"}
		} else {
			dupKeys = make([]string, 0, len(s.PlatformKeys))
			for _, pk := range s.PlatformKeys {
				dupKeys = append(dupKeys, s.Name.Value+"\x00"+pk)
			}
		}
		for _, k := range dupKeys {
			if seen[k] {
				return fmt.Errorf("duplicate variable '%s'", s.Name.Value)
			}
		}
		for _, k := range dupKeys {
			seen[k] = true
		}
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

// validateLoopScopedVars detects variables that are first declared inside a
// ForStatement body and then used after the loop exits. Because the loop might
// execute zero iterations, such variables would be undef at the point of use,
// leading to undefined behavior (e.g. infinite loops after LLVM optimization).
func validateLoopScopedVars(program *parser.Program) error {
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Body != nil {
			// Pre-populate definedBeforeLoop with all function parameters and
			// result parameters, since they are always initialized by the caller
			// or at function entry.
			preDefined := make(map[string]bool)
			for _, p := range fd.Parameters {
				preDefined[p.Name] = true
			}
			for _, r := range fd.Results {
				if r.Name != "" {
					preDefined[r.Name] = true
				}
			}
			if err := validateLoopScopedStmts(fd.Body.Statements, preDefined); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateLoopScopedStmts walks a statement list sequentially, tracking which
// variables are first declared inside a ForStatement body. After a ForStatement,
// it checks ALL subsequent statements for uses of those loop-only variables.
func validateLoopScopedStmts(stmts []parser.Statement, preDefined map[string]bool) error {
	// definedBeforeLoop: variables defined at top level before any loop
	definedBeforeLoop := make(map[string]bool)
	// Pre-populate with function parameters and results
	for k, v := range preDefined {
		definedBeforeLoop[k] = v
	}
	// loopOnlyVars: variables first declared inside a preceding loop body.
	// These might be undef if the loop executed zero iterations.
	loopOnlyVars := make(map[string]bool)

	for _, stmt := range stmts {
		// Check if this statement reads a loop-only variable in an expression
		usedNames := collectExprIdentifiers(stmt)
		for name := range usedNames {
			if loopOnlyVars[name] && !definedBeforeLoop[name] {
				return fmt.Errorf("line %d: variable '%s' is declared inside a loop body and may be uninitialized when used here (loop might execute zero iterations)",
					stmtPosLine(stmt), name)
			}
		}
		// Check if a ForStatement's loop variable reuses a loop-only variable name.
		// This is the exact pattern that caused the md5 hang: a variable declared
		// inside a loop body (c u32 = h2) was reused as a range-for loop variable
		// (c <- (dremain..56)), causing LLVM to generate undef PHI nodes.
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.IterRange != nil && fs.IterRange.Variable != "" {
				v := fs.IterRange.Variable
				if loopOnlyVars[v] && !definedBeforeLoop[v] {
					return fmt.Errorf("line %d: variable '%s' is declared inside a loop body and may be uninitialized when reused as loop variable here (loop might execute zero iterations)",
						fs.Token.Line, v)
				}
			}
		}

		// Track top-level variable declarations
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			definedBeforeLoop[ls.Name.Value] = true
			// Once re-declared at top level, it's no longer "loop-only"
			delete(loopOnlyVars, ls.Name.Value)
		}
		// ForStatement: loop variable is always initialized
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.IterRange != nil && fs.IterRange.Variable != "" {
				definedBeforeLoop[fs.IterRange.Variable] = true
				delete(loopOnlyVars, fs.IterRange.Variable)
			}
			// Collect variables declared inside the loop body
			bodyVars := collectLoopBodyVarDecls(fs)
			for v := range bodyVars {
				if !definedBeforeLoop[v] {
					loopOnlyVars[v] = true
				}
			}
			// Recursively validate nested statements inside the loop body.
			// Pass current definedBeforeLoop as preDefined so that variables
			// defined before this loop are still recognized inside the body.
			if fs.Body != nil {
				if err := validateLoopScopedStmts(fs.Body.Statements, definedBeforeLoop); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func collectLoopBodyVarDecls(fs *parser.ForStatement) map[string]bool {
	vars := make(map[string]bool)
	if fs.Body == nil {
		return vars
	}
	collectVarDeclsFromStmts(fs.Body.Statements, vars)
	if fs.IterRange != nil && fs.IterRange.Variable != "" {
		delete(vars, fs.IterRange.Variable)
	}
	return vars
}

func collectVarDeclsFromStmts(stmts []parser.Statement, vars map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name != nil {
				vars[s.Name.Value] = true
			}
		case *parser.BlockStatement:
			collectVarDeclsFromStmts(s.Statements, vars)
		case *parser.ForStatement:
			if s.Body != nil {
				collectVarDeclsFromStmts(s.Body.Statements, vars)
			}
		}
	}
}

func collectExprIdentifiers(stmt parser.Statement) map[string]bool {
	idents := make(map[string]bool)
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Value != nil {
			collectIdentsFromExpr(s.Value, idents)
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			collectIdentsFromExpr(s.Expression, idents)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			collectIdentsFromExpr(s.ReturnValue, idents)
		}
	case *parser.ForStatement:
		if s.IterRange != nil {
			if s.IterRange.Range != nil {
				if s.IterRange.Range.Start != nil {
					collectIdentsFromExpr(s.IterRange.Range.Start, idents)
				}
				if s.IterRange.Range.End != nil {
					collectIdentsFromExpr(s.IterRange.Range.End, idents)
				}
			}
			if s.IterRange.RangeExpr != nil {
				collectIdentsFromExpr(s.IterRange.RangeExpr, idents)
			}
		}
		if s.Condition != nil {
			collectIdentsFromExpr(s.Condition, idents)
		}
		if s.CountExpr != nil {
			collectIdentsFromExpr(s.CountExpr, idents)
		}
	case *parser.MultiAssignStatement:
		for _, target := range s.Targets {
			collectIdentsFromExpr(target, idents)
		}
		if s.Value != nil {
			collectIdentsFromExpr(s.Value, idents)
		}
	}
	return idents
}

func collectIdentsFromExpr(expr parser.Expression, idents map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		idents[e.Value] = true
	case *parser.PrefixExpression:
		collectIdentsFromExpr(e.Right, idents)
	case *parser.InfixExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Right, idents)
	case *parser.CallExpression:
		collectIdentsFromExpr(e.Function, idents)
		for _, arg := range e.Arguments {
			collectIdentsFromExpr(arg, idents)
		}
		for _, ga := range e.GenericArgs {
			collectIdentsFromExpr(ga, idents)
		}
	case *parser.DotExpression:
		collectIdentsFromExpr(e.Receiver, idents)
	case *parser.IndexExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Index, idents)
	case *parser.SliceExpression:
		collectIdentsFromExpr(e.Left, idents)
		if e.Range != nil {
			collectIdentsFromExpr(e.Range.Start, idents)
			collectIdentsFromExpr(e.Range.End, idents)
		}
	case *parser.AssignExpression:
		collectIdentsFromExpr(e.Left, idents)
		collectIdentsFromExpr(e.Value, idents)
	case *parser.ConditionalExpression:
		collectIdentsFromExpr(e.Condition, idents)
		collectIdentsFromExpr(e.Consequence, idents)
		collectIdentsFromExpr(e.Alternative, idents)
	case *parser.IfExpression:
		collectIdentsFromExpr(e.Condition, idents)
	case *parser.CastExpression:
		collectIdentsFromExpr(e.Expr, idents)
	case *parser.GroupedExpression:
		collectIdentsFromExpr(e.Expression, idents)
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			collectIdentsFromExpr(elem, idents)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			collectIdentsFromExpr(elem, idents)
		}
	case *parser.MapLiteral:
		for _, pair := range e.Pairs {
			collectIdentsFromExpr(pair.Key, idents)
			collectIdentsFromExpr(pair.Value, idents)
		}
	case *parser.StructLiteral:
		for _, field := range e.Fields {
			collectIdentsFromExpr(field.Value, idents)
		}
	case *parser.RunExpression:
		collectIdentsFromExpr(e.Call, idents)
	case *parser.AwaitExpression:
		collectIdentsFromExpr(e.Right, idents)
	case *parser.RangeExpression:
		collectIdentsFromExpr(e.Start, idents)
		collectIdentsFromExpr(e.End, idents)
	}
}

func stmtPosLine(stmt parser.Statement) int {
	if stmt == nil {
		return 0
	}
	return stmt.Pos().Line
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
			// Skip validation for synthetic `it` bindings injected by match desugar.
			// These have sentinel types ("err", "nil") or element types ("str", "i64")
			// but their value is an option variable, not a direct string/integer.
			if s.IsSynthetic {
				return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes)
			}
			// Skip string type check for array/slice variables
			isArrayVar := false
			if at, ok := s.Type.(*parser.ArrayType); ok {
				isArrayVar = at != nil
			}
			if !isArrayVar {
				// Only check explicitly str-typed variables (not inferred ones).
				// Inferred string variables (from StringLiteral, etc.) may later be
				// assigned from struct field access or cross-module calls whose
				// return type is unknown at vet time; deferring to LLVM is safer.
				isExplicitStr := s.Type != nil && (s.Type.String() == "str")
				if isExplicitStr {
					if !isStringExpr(s.Value, stringSizes) {
						return fmt.Errorf("cannot assign non-string value to string variable '%s'", s.Name.Value)
					}
				}
			}
			return validateExprArrayBounds(s.Value, arraySizes, sliceSizes, stringSizes, varTypes)
		}
	case *parser.FunctionDefinition:
		// Build a fresh per-function stringSizes map to avoid variable name
		// collisions between functions (e.g., 'pos' may be str in one function
		// and i64 in another). Only include this function's parameters, results,
		// and local variables — not global constants or other functions' variables.
		funcStringSizes := make(map[string]int64)
		for _, p := range s.Parameters {
			if p.Type != nil && (p.Type.String() == "str") {
				funcStringSizes[p.Name] = 0
			}
		}
		for _, p := range s.Results {
			if p.Type != nil && (p.Type.String() == "str") {
				funcStringSizes[p.Name] = 0
			}
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				collectStringSizeMapFromStmt(ss, funcStringSizes)
			}
			for _, ss := range s.Body.Statements {
				if err := validateStmtArrayBounds(ss, arraySizes, sliceSizes, funcStringSizes, varTypes); err != nil {
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

// tryEvalConstInt 嘗試在編譯期求值整數常數表達式。
// 支援：IntegerLiteral、PrefixExpression(-)、InfixExpression(+,-,*)、GroupedExpression。
// 若成功求值回傳 (value, true)，否則回傳 (0, false)。
func tryEvalConstInt(expr parser.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return e.Value, true
	case *parser.PrefixExpression:
		if e.Operator == "-" {
			if v, ok := tryEvalConstInt(e.Right); ok {
				return -v, true
			}
		}
		if e.Operator == "+" {
			return tryEvalConstInt(e.Right)
		}
	case *parser.InfixExpression:
		lv, lok := tryEvalConstInt(e.Left)
		rv, rok := tryEvalConstInt(e.Right)
		if lok && rok {
			switch e.Operator {
			case "+":
				return lv + rv, true
			case "-":
				return lv - rv, true
			case "*":
				return lv * rv, true
			}
		}
	case *parser.GroupedExpression:
		return tryEvalConstInt(e.Expression)
	}
	return 0, false
}

// checkConstIndexBounds 檢查常數索引是否越界（負數或 >= size）。
// 若索引非常數或 size <= 0 則跳過。回傳錯誤或 nil。
func checkConstIndexBounds(idxExpr parser.Expression, size int64, varName string, typeName string) error {
	if size <= 0 {
		return nil
	}
	idx, ok := tryEvalConstInt(idxExpr)
	if !ok {
		return nil
	}
	if idx < 0 {
		return fmt.Errorf("index %d out of bounds for %s '%s' of size %d", idx, typeName, varName, size)
	}
	if idx >= size {
		return fmt.Errorf("index %d out of bounds for %s '%s' of size %d", idx, typeName, varName, size)
	}
	return nil
}

func validateExprArrayBounds(expr parser.Expression, arraySizes map[string]int64, sliceSizes map[string]int64, stringSizes map[string]int64, varTypes map[string]string) error {
	switch e := expr.(type) {
	case *parser.IndexExpression:
		// 檢查索引是否為常數且超出陣列長度
		if ident, ok := e.Left.(*parser.Identifier); ok {
			if size, exists := arraySizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "array"); err != nil {
					return err
				}
			}
			// Also check slice bounds
			if size, exists := sliceSizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "slice"); err != nil {
					return err
				}
			}
			// Also check string index bounds
			if size, exists := stringSizes[ident.Value]; exists {
				if err := checkConstIndexBounds(e.Index, size, ident.Value, "string"); err != nil {
					return err
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
		// Note: string type check for reassignments is intentionally omitted.
		// Inferred string variables may be reassigned from struct field access or
		// cross-module calls whose return type is unknown at vet time.
		// The LetStatement check for explicitly str-typed variables is sufficient.
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
					// self.len() inside method bodies is valid — resolveSelfMethodCalls
					// will rewrite it to Type.len(self), which the codegen handles as
					// a builtin field access. Skip validation for the implicit receiver.
					if ident.Value != "self" {
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
						// Exception: map types (hashmap-K-V or [K]V) have a legitimate len() method
						if typeName, exists := varTypes[ident.Value]; exists {
							if strings.Contains(typeName, "hashmap-") || isMapTypeString(typeName) {
								// map types have a len() method — skip rejection
							} else {
								return fmt.Errorf("%s '%s' has no method 'len', use '%s.len' instead", typeName, ident.Value, ident.Value)
							}
						}
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

	// 1.5 構建函數返回類型映射（含 extern 宣告）
	funcTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.Results) > 0 && fd.Results[0].Type != nil {
				funcTypes[fd.Name] = fd.Results[0].Type.String()
			}
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			if len(es.Results) > 0 && es.Results[0].Type != nil {
				funcTypes[es.Name.Value] = es.Results[0].Type.String()
			}
		}
	}

	// 預填 stdlib 方法回傳型別（定義在 src/std/*.no，在 ValidateTypes 之後才合併）
	stdlibMethodTypes := map[string]string{
		"str.index":       "i64",
		"str.slice":       "str",
		"str.contains":    "bool",
		"str.starts-with": "bool",
		"str.ends-with":   "bool",
		"str.to-upper":    "str",
		"str.to-lower":    "str",
		"str.trim":        "str",
		"str.repeat":      "str",
		"str.copy":        "str",
	}
	for k, v := range stdlibMethodTypes {
		if _, exists := funcTypes[k]; !exists {
			funcTypes[k] = v
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
			// 使用複合 key：sig + "\x00" + platformKey（無平台註解則 suffix 為空）
			// 同簽名 + 同平台才算衝突；不同平台或通用 vs 平台特定不衝突
			var sigKeys []string
			if len(fd.PlatformKeys) == 0 {
				sigKeys = []string{sig + "\x00"}
			} else {
				sigKeys = make([]string, 0, len(fd.PlatformKeys))
				for _, pk := range fd.PlatformKeys {
					sigKeys = append(sigKeys, sig+"\x00"+pk)
				}
			}
			firstConflictLine := -1
			for _, k := range sigKeys {
				if firstLine, exists := sigSeen[k]; exists {
					firstConflictLine = firstLine
					break
				}
			}
			if firstConflictLine >= 0 {
				results = append(results, ValidateResult{
					Line:    fd.Token.Line,
					Column:  fd.Token.Column,
					Message: fmt.Sprintf("duplicate function definition '%s' (first defined at line %d)", sig, firstConflictLine),
				})
			} else {
				for _, k := range sigKeys {
					sigSeen[k] = fd.Token.Line
				}
			}
		}
	}

	// 3. 遍歷頂層語句做型別檢查
	// 收集 struct 定義的欄位型別，供 inferExprType 解析 self.field.method() 接收者型別
	validationStructFields = collectStructFields(program)
	// 先收集所有頂層變數的顯式型別，供跨語句型別推斷使用
	// （如 `z f64 = 2.0` 之後 `a = z * z` 需知道 z 是 f64）
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
	for _, stmt := range program.Statements {
		// 判斷是否為 struct 方法
		selfType := ""
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.Parameters) > 0 && fd.Parameters[0].Name == "self" {
				selfType = fd.Parameters[0].Type.String()
			}
		}
		// 使用預填的頂層變數型別，避免跨語句型別推斷失敗
		localVarTypes := make(map[string]string)
		for k, v := range topLevelVarTypes {
			localVarTypes[k] = v
		}
		errs := validateStmtTypes(stmt, funcNames, funcTypes, selfType, localVarTypes)
		results = append(results, errs...)
	}

	return results
}

// ValidateUnionTypes 收集 type alias（單型別和 union）並對函數的
// 聯合型別/泛型變體做檢查：
//   - 記錄每個 alias 名稱 -> 解析後的具體型別列表
//   - 對帶 ..T 形式的函數，若 T 是 union alias，記錄到 FunctionDefinition.VariadicUnion
//   - 對每個 type alias，遞迴展開成扁平化的 []Type，供 codegen 使用
//
// 不在這裡阻擋編譯；錯誤一律以 ValidateResult 報告（warning 級別）。
func ValidateUnionTypes(program *parser.Program) (map[string]*parser.TypeAlias, []ValidateResult) {
	aliases := make(map[string]*parser.TypeAlias)
	var results []ValidateResult

	// Pass 1: 收集所有 type alias
	for _, stmt := range program.Statements {
		ta, ok := stmt.(*parser.TypeAlias)
		if !ok {
			continue
		}
		if _, exists := aliases[ta.Name]; exists {
			results = append(results, ValidateResult{
				Line:    ta.Token.Line,
				Column:  ta.Token.Column,
				Message: fmt.Sprintf("duplicate type alias %q", ta.Name),
			})
			continue
		}
		aliases[ta.Name] = ta
	}

	// Pass 2: 對函數的 variadic 參數，若類型是 union alias 名稱，
	// 設到 FunctionDefinition.VariadicUnion，供 codegen 單態化。
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || !fd.IsVariadic {
			continue
		}
		if len(fd.Parameters) == 0 {
			continue
		}
		last := fd.Parameters[len(fd.Parameters)-1]
		if last.Type == nil {
			continue
		}
		// variadic 參數型別以 []t 表示（"切片"）；內部元素名稱就是 union 名
		typeName := strings.TrimPrefix(last.Type.String(), "[]")
		if typeName == "" {
			continue
		}
		if ta, ok := aliases[typeName]; ok && ta.IsUnion() {
			fd.VariadicUnion = typeName
		}
	}

	// Pass 3: 對函數的參數或結果型別，若整個函數只使用同一個 union alias
	// （非 variadic 情況，例如 abs = (a num) (r num)），標記為 GenericUnion
	// 供 codegen 單態化。
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.IsVariadic {
			continue
		}
		if fd.GenericUnion != "" {
			continue
		}
		unionName := findSingleUnionName(fd, aliases)
		if unionName != "" {
			fd.GenericUnion = unionName
		}
	}

	return aliases, results
}

// findSingleUnionName 檢查函數的參數與結果型別，找出唯一使用的 union alias。
// 若函數只涉及一個 union alias，則返回該名稱；否則返回空字串。
// 對於 variadic 函數，返回空字串（variadic 由 VariadicUnion 處理）。
func findSingleUnionName(fd *parser.FunctionDefinition, aliases map[string]*parser.TypeAlias) string {
	unionNames := make(map[string]bool)
	for _, p := range fd.Parameters {
		name := collectUnionNamesFromType(p.Type, aliases)
		for n := range name {
			unionNames[n] = true
		}
	}
	for _, r := range fd.Results {
		name := collectUnionNamesFromType(r.Type, aliases)
		for n := range name {
			unionNames[n] = true
		}
	}
	if len(unionNames) == 1 {
		for n := range unionNames {
			return n
		}
	}
	return ""
}

// collectUnionNamesFromType 從型別中收集所有 union alias 名稱。
func collectUnionNamesFromType(t parser.Type, aliases map[string]*parser.TypeAlias) map[string]bool {
	out := make(map[string]bool)
	if t == nil {
		return out
	}
	switch ty := t.(type) {
	case *parser.NamedType:
		if ta, ok := aliases[ty.Value]; ok && ta.IsUnion() {
			out[ty.Value] = true
		}
	case *parser.SliceType:
		sub := collectUnionNamesFromType(ty.Elem, aliases)
		for n := range sub {
			out[n] = true
		}
	case *parser.ArrayType:
		sub := collectUnionNamesFromType(ty.Elem, aliases)
		for n := range sub {
			out[n] = true
		}
	case *parser.PointerType:
		sub := collectUnionNamesFromType(ty.Type, aliases)
		for n := range sub {
			out[n] = true
		}
	case *parser.NullableType:
		sub := collectUnionNamesFromType(ty.Type, aliases)
		for n := range sub {
			out[n] = true
		}
	case *parser.FunctionType:
		// Function types do not contribute union alias names.
	}
	return out
}

// FlattenUnion 將一個 union alias（或單型別 alias）扁平化為具體型別列表。
// 對於 union：對每個成員遞迴展開（若成員是另一個 union alias 會被展開）。
// 對於單型別 alias：返回 [Type]（長度 1）。
// 對於已知的 builtin（i8/i16/.../f64/bool/byte/char/str）：原樣返回。
func FlattenUnion(name string, aliases map[string]*parser.TypeAlias) []parser.Type {
	// 內建類型（不可遞迴展開，視為葉節點）
	switch name {
	case "i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f32", "f64",
		"bool", "byte", "char", "str":
		return []parser.Type{&parser.NamedType{Value: name}}
	}
	ta, ok := aliases[name]
	if !ok {
		// 未知型別，當作泛型變量返回
		return []parser.Type{&parser.NamedType{Value: name}}
	}
	if ta.Union != nil {
		var out []parser.Type
		for _, t := range ta.Union.Types {
			if nt, ok := t.(*parser.NamedType); ok {
				// 對 union 的成員再做遞迴展開
				out = append(out, FlattenUnion(nt.Value, aliases)...)
			} else {
				out = append(out, t)
			}
		}
		return out
	}
	if ta.Type != nil {
		if nt, ok := ta.Type.(*parser.NamedType); ok {
			return FlattenUnion(nt.Value, aliases)
		}
		return []parser.Type{ta.Type}
	}
	return nil
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
		// Global constants/variables at top level allow uppercase names (e.g., SBOX)
		if _, ok := stmt.(*parser.LetStatement); ok {
			continue
		}
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

// ValidateAsyncNaming 檢查所有由 'run' 調用的函數名稱是否以 '-async' 結尾
func ValidateAsyncNaming(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	walkStatementsForAsync(program.Statements, &results)
	return results
}

func walkStatementsForAsync(stmts []parser.Statement, results *[]ValidateResult) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if s.Body != nil {
				walkStatementsForAsync(s.Body.Statements, results)
			}
		case *parser.LetStatement:
			if s.Value != nil {
				checkRunAsyncNaming(s.Value, results)
				if fnLit, ok := s.Value.(*parser.FunctionLiteral); ok {
					if fnLit.Body != nil {
						walkStatementsForAsync(fnLit.Body.Statements, results)
					}
				}
			}
		case *parser.BlockStatement:
			walkStatementsForAsync(s.Statements, results)
		case *parser.ExpressionStatement:
			if s.Expression != nil {
				checkRunAsyncNaming(s.Expression, results)
				if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
					if ifExpr.Consequence != nil {
						walkStatementsForAsync(ifExpr.Consequence.Statements, results)
					}
					if ifExpr.Alternative != nil {
						walkStatementsForAsync(ifExpr.Alternative.Statements, results)
					}
				}
			}
		case *parser.ForStatement:
			if s.Body != nil {
				walkStatementsForAsync(s.Body.Statements, results)
			}
		}
	}
}

// checkRunAsyncNaming 檢查單個表達式是否為 RunExpression，並驗證其調用的函數名稱
func checkRunAsyncNaming(expr parser.Expression, results *[]ValidateResult) {
	runExpr, ok := expr.(*parser.RunExpression)
	if !ok {
		return
	}
	call, ok := runExpr.Call.(*parser.CallExpression)
	if !ok {
		return
	}
	fnName := asyncCallFunctionName(call)
	if fnName == "" {
		return
	}
	if !strings.HasSuffix(fnName, "-async") {
		*results = append(*results, ValidateResult{
			Line:    runExpr.Token.Line,
			Column:  runExpr.Token.Column,
			Message: fmt.Sprintf("function '%s' called by 'run' should end with '-async'", fnName),
		})
	}
}

func asyncCallFunctionName(call *parser.CallExpression) string {
	if ident, ok := call.Function.(*parser.Identifier); ok {
		return ident.Value
	}
	if dot, ok := call.Function.(*parser.DotExpression); ok {
		return dot.Property
	}
	return ""
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
		// Walk IterRange (e.g., `i <- [0..N)` — the range expression may reference vars)
		if s.IterRange != nil {
			if s.IterRange.Range != nil {
				if s.IterRange.Range.Start != nil {
					markReferencesInExpr(s.IterRange.Range.Start, varSet, usedVars)
				}
				if s.IterRange.Range.End != nil {
					markReferencesInExpr(s.IterRange.Range.End, varSet, usedVars)
				}
			}
			if s.IterRange.RangeExpr != nil {
				markReferencesInExpr(s.IterRange.RangeExpr, varSet, usedVars)
			}
		}
		// Walk CountExpr (for `{ } * N` counted loops)
		if s.CountExpr != nil {
			markReferencesInExpr(s.CountExpr, varSet, usedVars)
		}
		if s.Body != nil {
			for _, inner := range s.Body.Statements {
				markReferencesInStatement(inner, varSet, usedVars)
			}
		}

	case *parser.MultiAssignStatement:
		// Multi-assignment: targets, value = func(args)
		// Walk targets (they may reference vars via index expressions)
		for _, target := range s.Targets {
			markReferencesInExpr(target, varSet, usedVars)
		}
		if s.Value != nil {
			markReferencesInExpr(s.Value, varSet, usedVars)
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
func ValidateUndefinedVars(program *parser.Program, rootDir string) []ValidateResult {
	var results []ValidateResult

	// 1. Collect all defined names
	definedVars := make(map[string]bool) // name → true
	funcNames := make(map[string]bool)   // function names

	// Top-level LetStatements, FunctionDefinitions, and ExternStatements
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			definedVars[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			definedVars[fd.Name] = true
			funcNames[fd.Name] = true
		}
		if es, ok := stmt.(*parser.ExternStatement); ok && es.Name != nil {
			definedVars[es.Name.Value] = true
			funcNames[es.Name.Value] = true
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
		// Union methods (e.g. "num.sign") can be called by short name ("sign")
		// because rewriteUnionCalls dispatches by argument type.
		// Register the short name so the validator doesn't flag it as undefined.
		if idx := strings.LastIndex(n, "."); idx > 0 {
			shortName := n[idx+1:]
			definedVars[shortName] = true
		}
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
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
		}
	}

	// 3b. Collect symbols from local module imports (paths starting with /)
	//     These include FFI declarations (#c), functions, and constants from
	//     imported files like `# /sqlite-driver/sqlite`.
	if rootDir != "" {
		pkg, _ := LoadPackage(rootDir)
		for _, stmt := range program.Statements {
			use, ok := stmt.(*parser.UseStatement)
			if !ok {
				continue
			}
			// Only handle module-level imports (no specific function)
			// Function-specific imports are already handled in step 3.
			if use.Function != "" {
				continue
			}
			modProg := resolveUseModule(use, pkg)
			if modProg == nil {
				continue
			}
			for _, ms := range modProg.Statements {
				if es, ok := ms.(*parser.ExternStatement); ok && es.Name != nil {
					// Skip private FFI declarations (underscore-prefixed)
					if !strings.HasPrefix(es.Name.Value, "_") {
						definedVars[es.Name.Value] = true
						funcNames[es.Name.Value] = true
					}
				}
				if fd, ok := ms.(*parser.FunctionDefinition); ok {
					definedVars[fd.Name] = true
					funcNames[fd.Name] = true
				}
				if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
					definedVars[ls.Name.Value] = true
				}
			}
		}
	}

	// Collect enum variant names from EnumDefinition and TaggedEnumDefinition
	for _, stmt := range program.Statements {
		if ed, ok := stmt.(*parser.EnumDefinition); ok {
			for _, v := range ed.Values {
				definedVars[v.Name] = true
			}
		}
		if ted, ok := stmt.(*parser.TaggedEnumDefinition); ok {
			for _, v := range ted.Variants {
				definedVars[v.Name] = true
			}
		}
	}

	// 4. Walk statements and check for undefined references
	for _, stmt := range program.Statements {
		results = append(results, checkUndefinedVarsInStmt(stmt, definedVars, funcNames)...)
	}

	return results
}

// ValidateUninitOutputParams checks that ?T (nullable) output parameters are
// directly assigned in the function body before being read. A ?T output
// parameter that is read (used in an expression) but never assigned via '='
// is flagged as an error — reading an uninitialized nullable value is unsafe
// and almost certainly a bug (case6: ?T 未初始化使用 → 編譯器報錯).
//
// Covered scenarios:
//   - Case 7 (?T 先賦值再用 → 允許): param IS assigned → no error.
//   - Case 8 (?T 空函數體 → 返回 nil): param NOT read → no error.
//   - Case 6 (?T 未初始化使用 → 報錯): param read but NOT assigned → error.
func ValidateUninitOutputParams(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		// Collect ?T output parameter names
		type nullableParam struct {
			name string
			line int
			col  int
		}
		var nullableParams []nullableParam
		for _, r := range fd.Results {
			if r.Name == "" {
				continue
			}
			if _, ok := r.Type.(*parser.NullableType); ok {
				nullableParams = append(nullableParams, nullableParam{
					name: r.Name,
					line: r.Token.Line,
					col:  r.Token.Column,
				})
			}
		}
		if len(nullableParams) == 0 {
			continue
		}
		// Collect all directly-assigned variable names in the body
		assigned := make(map[string]bool)
		collectAssignedNames(fd.Body.Statements, assigned)
		// Collect all read variable names in the body
		read := make(map[string]bool)
		collectReadNames(fd.Body.Statements, read)
		// Check each ?T output param: read but not assigned → error
		for _, p := range nullableParams {
			if read[p.name] && !assigned[p.name] {
				results = append(results, ValidateResult{
					Line:    p.line,
					Column:  p.col,
					Message: fmt.Sprintf("output parameter '%s' (?T) is read but never assigned in function body — uninitialized use of nullable output parameter", p.name),
				})
			}
		}
	}
	return results
}

// collectAssignedNames walks statements recursively and collects variable names
// that are directly assigned via '=' (LetStatement.Name or MultiAssignStatement
// Identifier targets). IndexExpression/DotExpression targets are NOT direct
// assignments — they read the base variable to write to a field/element.
func collectAssignedNames(stmts []parser.Statement, assigned map[string]bool) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name != nil {
				assigned[s.Name.Value] = true
			}
			if s.Value != nil {
				collectAssignedNamesInExpr(s.Value, assigned)
			}
		case *parser.MultiAssignStatement:
			for _, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					assigned[ident.Value] = true
				}
				// IndexExpression/DotExpression targets: not direct assignments
			}
			if s.Value != nil {
				collectAssignedNamesInExpr(s.Value, assigned)
			}
		case *parser.BlockStatement:
			collectAssignedNames(s.Statements, assigned)
		case *parser.ForStatement:
			if s.Init != nil {
				collectAssignedNames([]parser.Statement{s.Init}, assigned)
			}
			if s.Body != nil {
				collectAssignedNames(s.Body.Statements, assigned)
			}
		case *parser.ExpressionStatement:
			if s.Expression != nil {
				collectAssignedNamesInExpr(s.Expression, assigned)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				collectAssignedNamesInExpr(s.ReturnValue, assigned)
			}
		}
	}
}

// collectAssignedNamesInExpr walks expressions for nested statements (if/else)
// that may contain assignments. Does NOT recurse into nested function literals.
func collectAssignedNamesInExpr(expr parser.Expression, assigned map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			collectAssignedNames(e.Consequence.Statements, assigned)
		}
		if e.Alternative != nil {
			collectAssignedNames(e.Alternative.Statements, assigned)
		}
	case *parser.ConditionalExpression:
		// ternary cond ? a : b — no statements, just expressions
	}
}

// collectReadNames walks statements recursively and collects variable names
// that are read (used in expressions). Direct assignment targets (LetStatement.Name,
// MultiAssignStatement Identifier targets) are NOT reads. However, IndexExpression.Left
// and DotExpression.Receiver ARE reads — out[i]=val reads 'out' to get the data ptr.
func collectReadNames(stmts []parser.Statement, read map[string]bool) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Value != nil {
				collectReadNamesInExpr(s.Value, read)
			}
		case *parser.MultiAssignStatement:
			for _, target := range s.Targets {
				switch t := target.(type) {
				case *parser.IndexExpression:
					// out[i] = val → reads out (to get data pointer) and i
					collectReadNamesInExpr(t.Left, read)
					collectReadNamesInExpr(t.Index, read)
				case *parser.DotExpression:
					// out.field = val → reads out (to get struct pointer)
					collectReadNamesInExpr(t.Receiver, read)
				}
				// Identifier targets are pure writes — not reads
			}
			if s.Value != nil {
				collectReadNamesInExpr(s.Value, read)
			}
		case *parser.BlockStatement:
			collectReadNames(s.Statements, read)
		case *parser.ForStatement:
			if s.Init != nil {
				collectReadNames([]parser.Statement{s.Init}, read)
			}
			if s.Condition != nil {
				collectReadNamesInExpr(s.Condition, read)
			}
			if s.Update != nil {
				collectReadNames([]parser.Statement{s.Update}, read)
			}
			if s.IterRange != nil {
				if s.IterRange.RangeExpr != nil {
					collectReadNamesInExpr(s.IterRange.RangeExpr, read)
				}
				if s.IterRange.Range != nil {
					if s.IterRange.Range.Start != nil {
						collectReadNamesInExpr(s.IterRange.Range.Start, read)
					}
					if s.IterRange.Range.End != nil {
						collectReadNamesInExpr(s.IterRange.Range.End, read)
					}
				}
			}
			if s.Body != nil {
				collectReadNames(s.Body.Statements, read)
			}
		case *parser.ExpressionStatement:
			if s.Expression != nil {
				collectReadNamesInExpr(s.Expression, read)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				collectReadNamesInExpr(s.ReturnValue, read)
			}
		}
	}
}

// collectReadNamesInExpr walks an expression tree and collects all variable names
// that are read. Handles all expression types including DotExpression.Receiver,
// IndexExpression.Left, CallExpression args, etc.
func collectReadNamesInExpr(expr parser.Expression, read map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		read[e.Value] = true
	case *parser.CallExpression:
		collectReadNamesInExpr(e.Function, read)
		for _, arg := range e.Arguments {
			collectReadNamesInExpr(arg, read)
		}
	case *parser.DotExpression:
		// out.field → reads out (the receiver)
		collectReadNamesInExpr(e.Receiver, read)
	case *parser.IndexExpression:
		// out[i] → reads out and i
		collectReadNamesInExpr(e.Left, read)
		collectReadNamesInExpr(e.Index, read)
	case *parser.SliceExpression:
		collectReadNamesInExpr(e.Left, read)
		if e.Range != nil {
			if e.Range.Start != nil {
				collectReadNamesInExpr(e.Range.Start, read)
			}
			if e.Range.End != nil {
				collectReadNamesInExpr(e.Range.End, read)
			}
		}
	case *parser.InfixExpression:
		if e.Left != nil {
			collectReadNamesInExpr(e.Left, read)
		}
		if e.Right != nil {
			collectReadNamesInExpr(e.Right, read)
		}
	case *parser.PrefixExpression:
		if e.Right != nil {
			collectReadNamesInExpr(e.Right, read)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			collectReadNamesInExpr(e.Expression, read)
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			collectReadNamesInExpr(e.Condition, read)
		}
		if e.Consequence != nil {
			collectReadNames(e.Consequence.Statements, read)
		}
		if e.Alternative != nil {
			collectReadNames(e.Alternative.Statements, read)
		}
	case *parser.AssignExpression:
		// out.field = val → reads out (via DotExpression.Receiver)
		if dot, ok := e.Left.(*parser.DotExpression); ok {
			collectReadNamesInExpr(dot.Receiver, read)
		}
		if idx, ok := e.Left.(*parser.IndexExpression); ok {
			collectReadNamesInExpr(idx.Left, read)
			collectReadNamesInExpr(idx.Index, read)
		}
		if e.Value != nil {
			collectReadNamesInExpr(e.Value, read)
		}
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			collectReadNamesInExpr(e.Condition, read)
		}
		if e.Consequence != nil {
			collectReadNamesInExpr(e.Consequence, read)
		}
		if e.Alternative != nil {
			collectReadNamesInExpr(e.Alternative, read)
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			collectReadNamesInExpr(elem, read)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			collectReadNamesInExpr(elem, read)
		}
	case *parser.StructLiteral:
		for _, f := range e.Fields {
			if f.Value != nil {
				collectReadNamesInExpr(f.Value, read)
			}
		}
	case *parser.MapLiteral:
		for _, pair := range e.Pairs {
			collectReadNamesInExpr(pair.Key, read)
			collectReadNamesInExpr(pair.Value, read)
		}
	case *parser.RunExpression:
		if e.Call != nil {
			collectReadNamesInExpr(e.Call, read)
		}
	case *parser.AwaitExpression:
		if e.Right != nil {
			collectReadNamesInExpr(e.Right, read)
		}
	case *parser.CastExpression:
		if e.Expr != nil {
			collectReadNamesInExpr(e.Expr, read)
		}
	// Literals (Integer, String, Float, Char, Boolean, Byte, Nil, Regex) don't read variables
	// FunctionLiteral: don't recurse (nested function has its own scope)
	}
}

// ValidateInterfaceImplementation matches dotted-name function definitions
// (e.g. `i8.gt = ...`) against generic-receiver interface method
// declarations (e.g. `ord { t.gt(b t) (res bool) }`). Emits a warning
// when an implementing type is missing or its method signature does not
// match the interface constraint.
func ValidateInterfaceImplementation(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	type ifaceMethod struct {
		Receiver string
		Name     string
		Params   []string // canonical type strings
		Results  []string
		Token    lexer.Token
	}
	ifaces := map[string][]ifaceMethod{} // interface name → methods
	for _, stmt := range program.Statements {
		id, ok := stmt.(*parser.InterfaceDefinition)
		if !ok {
			continue
		}
		var methods []ifaceMethod
		for _, m := range id.Methods {
			if !m.IsGenericReceiver {
				continue
			}
			im := ifaceMethod{Receiver: m.Receiver, Name: m.Name, Token: m.Token}
			for _, p := range m.Parameters {
				if p.Type != nil {
					im.Params = append(im.Params, p.Type.String())
				}
			}
			for _, r := range m.Results {
				if r.Type != nil {
					im.Results = append(im.Results, r.Type.String())
				}
			}
			methods = append(methods, im)
		}
		ifaces[id.Name] = methods
	}

	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || !strings.Contains(fd.Name, ".") {
			continue
		}
		implType, implMethod, ok := splitDottedMethodName(fd.Name)
		if !ok {
			continue
		}
		// Dotted methods have a hidden self parameter prepended by
		// parseMethodDefinition. Skip it for signature comparison.
		implParams := fd.Parameters
		if len(implParams) > 0 && implParams[0].Name == "self" {
			implParams = implParams[1:]
		}
		implResults := fd.Results
		for _, methods := range ifaces {
			for _, m := range methods {
				if m.Name != implMethod {
					continue
				}
				if len(implParams) != len(m.Params) {
					results = append(results, ValidateResult{
						Line:      fd.Token.Line,
						Column:    fd.Token.Column,
						EndColumn: fd.Token.Column + len(fd.Name),
						Message: fmt.Sprintf("method '%s.%s' has %d parameter(s), interface expects %d",
							implType, implMethod, len(implParams), len(m.Params)),
					})
					continue
				}
				for i, p := range implParams {
					if p.Type == nil {
						continue
					}
					paramType := p.Type.String()
					expected := strings.ReplaceAll(m.Params[i], m.Receiver, implType)
					if paramType != expected {
						results = append(results, ValidateResult{
							Line:      p.Token.Line,
							Column:    p.Token.Column,
							EndColumn: p.Token.Column + len(p.Name),
							Message: fmt.Sprintf("parameter %d of '%s.%s': expected '%s', got '%s'",
								i+1, implType, implMethod, expected, paramType),
						})
					}
				}
				if len(implResults) != len(m.Results) {
					results = append(results, ValidateResult{
						Line:      fd.Token.Line,
						Column:    fd.Token.Column,
						EndColumn: fd.Token.Column + len(fd.Name),
						Message: fmt.Sprintf("method '%s.%s' has %d result(s), interface expects %d",
							implType, implMethod, len(implResults), len(m.Results)),
					})
				} else {
					for i, r := range implResults {
						if r.Type == nil {
							continue
						}
						resType := r.Type.String()
						expected := strings.ReplaceAll(m.Results[i], m.Receiver, implType)
						if resType != expected {
							results = append(results, ValidateResult{
								Line:      r.Token.Line,
								Column:    r.Token.Column,
								EndColumn: r.Token.Column + len(r.Name),
								Message: fmt.Sprintf("result %d of '%s.%s': expected '%s', got '%s'",
									i+1, implType, implMethod, expected, resType),
							})
						}
					}
				}
			}
		}
	}
	return results
}

// splitDottedMethodName splits a function name like "i8.gt" or
// "[]ord.ast" or "[?]ord.desc" into (implType, methodName). Returns
// false if the name does not contain a dotted-method form.
func splitDottedMethodName(name string) (string, string, bool) {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return "", "", false
	}
	implType := name[:idx]
	methodName := name[idx+1:]
	if implType == "" || methodName == "" {
		return "", "", false
	}
	return implType, methodName, true
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

// ValidateDuplicateVars checks for duplicate variable declarations and returns diagnostics.
func ValidateDuplicateVars(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	seen := make(map[string]struct{})
	for _, stmt := range program.Statements {
		results = append(results, checkStmtDuplicateVars(stmt, seen)...)
	}
	return results
}

func checkStmtDuplicateVars(stmt parser.Statement, seen map[string]struct{}) []ValidateResult {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name == nil {
			return nil
		}

		// Detect parser-inferred types (Type.Token at same position as Name.Token)
		isInferred := false
		if nt, ok := s.Type.(*parser.NamedType); ok && s.Name != nil {
			isInferred = nt.Token.Line == s.Name.Token.Line &&
				nt.Token.Column == s.Name.Token.Column
		}
		// SliceType/ArrayType：parser 自動推導時 Token 與 nameToken 相同（同行同列），
		// 用戶顯式標註時 Token 是 '[' 位置。允許 slice/array 重新賦值。
		if st, ok := s.Type.(*parser.SliceType); ok && s.Name != nil {
			if st.Token.Line == s.Name.Token.Line && st.Token.Column == s.Name.Token.Column {
				isInferred = true
			}
		}
		if at, ok := s.Type.(*parser.ArrayType); ok && s.Name != nil {
			if at.Token.Line == s.Name.Token.Line && at.Token.Column == s.Name.Token.Column {
				isInferred = true
			}
		}

		// 計算複合 key：name + "\x00" + platformKey（無平台註解則 suffix 為空）
		// 同名 + 同平台才算衝突；不同平台或通用 vs 平台特定不衝突
		var compositeKeys []string
		if len(s.PlatformKeys) == 0 {
			compositeKeys = []string{s.Name.Value + "\x00"}
		} else {
			compositeKeys = make([]string, 0, len(s.PlatformKeys))
			for _, pk := range s.PlatformKeys {
				compositeKeys = append(compositeKeys, s.Name.Value+"\x00"+pk)
			}
		}
		// Check for duplicate when:
		//   1. Real type annotation (not parser artifact where Type == Name, and not inferred), OR
		//   2. Uppercase constant name (e.g., `A = 0`, `SBOX = 1`) — even without type annotation
		hasRealTypeAnnotation := s.Type != nil && s.Type.String() != s.Name.Value && !isInferred
		isConst := isConstantName(s.Name.Value)
		if hasRealTypeAnnotation || isConst {
			for _, k := range compositeKeys {
				if _, exists := seen[k]; exists {
					return []ValidateResult{{
						Line:    s.Token.Line,
						Column:  s.Token.Column,
						Message: fmt.Sprintf("'%s' already declared in this scope", s.Name.Value),
					}}
				}
			}
			// Real type annotation or uppercase constant — always register
			for _, k := range compositeKeys {
				seen[k] = struct{}{}
			}
		} else if isInferred {
			// Inferred type (e.g. `i = 0`): register only the first declaration,
			// allow subsequent re-assignments like `i = 4`
			anyExists := false
			for _, k := range compositeKeys {
				if _, exists := seen[k]; exists {
					anyExists = true
					break
				}
			}
			if !anyExists {
				for _, k := range compositeKeys {
					seen[k] = struct{}{}
				}
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			bodySeen := make(map[string]struct{})
			for _, bStmt := range s.Body.Statements {
				results := checkStmtDuplicateVars(bStmt, bodySeen)
				if len(results) > 0 {
					return results
				}
			}
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			results := checkStmtDuplicateVars(bStmt, seen)
			if len(results) > 0 {
				return results
			}
		}
	}
	return nil
}

// ValidateDependencyImports checks that URL-style import paths (e.g., github.com/...)
// are declared in mod.jsonc dependencies. rootDir is the directory to search upward
// from for the project's mod.jsonc.
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
				Message: fmt.Sprintf("dependency not found: %q is not declared in mod.jsonc dependencies", path),
			})
		}
	}
	return results
}

// ValidateExportSymbols checks that all export declarations in lib.no reference
// symbols that actually exist in the corresponding module source files.
func ValidateExportSymbols(program *parser.Program, docPath string) []ValidateResult {
	// Only validate lib.no files
	if !strings.HasSuffix(docPath, "lib.no") {
		return nil
	}

	docDir := filepath.Dir(docPath)

	var results []ValidateResult
	for _, stmt := range program.Statements {
		es, ok := stmt.(*parser.ExportStatement)
		if !ok {
			continue
		}
		if es.Function == "" {
			continue
		}

		// Resolve the module file path
		modFile := ""
		path := es.Path
		if strings.HasPrefix(path, "/") {
			modFile = filepath.Join(docDir, strings.TrimPrefix(path, "/")) + ".no"
		} else if strings.HasPrefix(path, "std/") || path == "std" {
			modFile = path + ".no"
		}

		if modFile == "" {
			continue
		}

		// Parse the module file
		source, err := os.ReadFile(modFile)
		if err != nil {
			results = append(results, ValidateResult{
				Line:    es.Token.Line,
				Column:  es.Token.Column,
				Message: fmt.Sprintf("module file not found: %s", modFile),
			})
			continue
		}

		l := lexer.New(string(source))
		p := parser.New(l)
		modProg := p.ParseProgram()
		if len(p.Errors()) > 0 {
			continue
		}

		// Check if the function exists in the module
		found := false
		for _, modStmt := range modProg.Statements {
			switch s := modStmt.(type) {
			case *parser.FunctionDefinition:
				if s.Name == es.Function {
					found = true
				}
			case *parser.StructDefinition:
				if s.Name == es.Function {
					found = true
				}
			case *parser.EnumDefinition:
				if s.Name == es.Function {
					found = true
				}
			case *parser.TaggedEnumDefinition:
				if s.Name == es.Function {
					found = true
				}
			case *parser.InterfaceDefinition:
				if s.Name == es.Function {
					found = true
				}
			case *parser.LetStatement:
				if s.Name != nil && s.Name.Value == es.Function {
					found = true
				}
			}
		}

		if !found {
			results = append(results, ValidateResult{
				Line:    es.Token.Line,
				Column:  es.Token.Column,
				Message: fmt.Sprintf("export references undefined symbol %q (not found in %s)", es.Function, modFile),
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

// ValidatePrintFormat 檢查 print/printf/eprint/eprintf/sprintf 呼叫中的具名格式字串。
// 對於第一個參數為 StringLiteral 的呼叫，解析 {name:spec} 欄位並驗證：
//   - 欄位名稱在當前作用域內已定義（否則 "undefined variable '<name>' in format string"）
//   - 規格字串可被 ParseFormatSpec 解析（否則 "invalid format spec"）
//   - 規格類型字元與變數型別相容（整數類型對應 b/c/d/o/x/X；
//     浮點數對應 e/E/f/F/g/G/%；str/bool 對應 s）
func ValidatePrintFormat(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// 收集 struct 欄位型別資訊，用於解析結構欄位存取
	structFields := collectStructFields(program)

	// 走訪頂層敘述，追蹤變數作用域
	for _, stmt := range program.Statements {
		results = append(results, checkPrintFormatInStmt(stmt, make(map[string]string), structFields)...)
	}
	return results
}

// isPrintFormatCall 判斷呼叫是否為 print/printf/eprint/eprintf/sprintf（含 fmt. 前綴）
func isPrintFormatCall(fnName string) bool {
	switch fnName {
	case "print", "printf", "eprint", "eprintf", "sprintf",
		"fmt.print", "fmt.printf", "fmt.eprint", "fmt.eprintf", "fmt.sprintf":
		return true
	}
	return false
}

// checkPrintFormatInStmt 走訪敘述並驗證 print 格式字串，同時追蹤變數作用域。
func checkPrintFormatInStmt(stmt parser.Statement, varTypes map[string]string, structFields map[string]map[string]string) []ValidateResult {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			return checkPrintFormatInExpr(s.Expression, varTypes, structFields)
		}
	case *parser.LetStatement:
		var results []ValidateResult
		if s.Value != nil {
			results = append(results, checkPrintFormatInExpr(s.Value, varTypes, structFields)...)
			// 註冊變數型別：優先使用顯式型別標註，否則從值推導
			if s.Name != nil {
				if s.Type != nil && s.Type.String() != "" {
					varTypes[s.Name.Value] = s.Type.String()
				} else if _, exists := varTypes[s.Name.Value]; !exists {
					if inferred := inferExprType(s.Value, varTypes, nil, ""); inferred != "" {
						varTypes[s.Name.Value] = inferred
					}
				}
			}
			return results
		}
		// 僅型別宣告（無 = value）
		if s.Name != nil && s.Type != nil {
			varTypes[s.Name.Value] = s.Type.String()
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			return checkPrintFormatInExpr(s.Value, varTypes, structFields)
		}
	case *parser.FunctionDefinition:
		// 為函數體建立本地作用域，包含參數與結果參數
		localTypes := make(map[string]string)
		for k, v := range varTypes {
			localTypes[k] = v
		}
		for _, p := range s.Parameters {
			if p.Type != nil {
				localTypes[p.Name] = p.Type.String()
			}
		}
		for _, r := range s.Results {
			if r.Name != "" && r.Type != nil {
				localTypes[r.Name] = r.Type.String()
			}
		}
		if s.Body != nil {
			var results []ValidateResult
			for _, bs := range s.Body.Statements {
				results = append(results, checkPrintFormatInStmt(bs, localTypes, structFields)...)
			}
			return results
		}
	case *parser.BlockStatement:
		var results []ValidateResult
		for _, bs := range s.Statements {
			results = append(results, checkPrintFormatInStmt(bs, varTypes, structFields)...)
		}
		return results
	case *parser.ForStatement:
		var results []ValidateResult
		if s.Init != nil {
			results = append(results, checkPrintFormatInStmt(s.Init, varTypes, structFields)...)
		}
		if s.Condition != nil {
			results = append(results, checkPrintFormatInExpr(s.Condition, varTypes, structFields)...)
		}
		if s.Update != nil {
			results = append(results, checkPrintFormatInStmt(s.Update, varTypes, structFields)...)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				results = append(results, checkPrintFormatInStmt(bs, varTypes, structFields)...)
			}
		}
		return results
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			return checkPrintFormatInExpr(s.ReturnValue, varTypes, structFields)
		}
	}
	return nil
}

// checkPrintFormatInExpr 走訪表達式並驗證 print 格式字串。
func checkPrintFormatInExpr(expr parser.Expression, varTypes map[string]string, structFields map[string]map[string]string) []ValidateResult {
	if expr == nil {
		return nil
	}
	var results []ValidateResult
	switch e := expr.(type) {
	case *parser.CallExpression:
		// 識別 print/printf/eprint/eprintf/sprintf 呼叫
		if ident, ok := e.Function.(*parser.Identifier); ok {
			if isPrintFormatCall(ident.Value) && len(e.Arguments) > 0 {
				results = append(results, validatePrintFormatCall(e, varTypes, structFields)...)
			}
		}
		// 遞迴檢查引數中的巢狀呼叫
		for _, arg := range e.Arguments {
			results = append(results, checkPrintFormatInExpr(arg, varTypes, structFields)...)
		}
	case *parser.InfixExpression:
		if e.Left != nil {
			results = append(results, checkPrintFormatInExpr(e.Left, varTypes, structFields)...)
		}
		if e.Right != nil {
			results = append(results, checkPrintFormatInExpr(e.Right, varTypes, structFields)...)
		}
	case *parser.PrefixExpression:
		if e.Right != nil {
			results = append(results, checkPrintFormatInExpr(e.Right, varTypes, structFields)...)
		}
	case *parser.GroupedExpression:
		if e.Expression != nil {
			results = append(results, checkPrintFormatInExpr(e.Expression, varTypes, structFields)...)
		}
	case *parser.IfExpression:
		if e.Condition != nil {
			results = append(results, checkPrintFormatInExpr(e.Condition, varTypes, structFields)...)
		}
		if e.Consequence != nil {
			for _, is := range e.Consequence.Statements {
				results = append(results, checkPrintFormatInStmt(is, varTypes, structFields)...)
			}
		}
		if e.Alternative != nil {
			for _, is := range e.Alternative.Statements {
				results = append(results, checkPrintFormatInStmt(is, varTypes, structFields)...)
			}
		}
	case *parser.IndexExpression:
		if e.Left != nil {
			results = append(results, checkPrintFormatInExpr(e.Left, varTypes, structFields)...)
		}
		if e.Index != nil {
			results = append(results, checkPrintFormatInExpr(e.Index, varTypes, structFields)...)
		}
	case *parser.AssignExpression:
		if e.Value != nil {
			results = append(results, checkPrintFormatInExpr(e.Value, varTypes, structFields)...)
		}
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			results = append(results, checkPrintFormatInExpr(e.Condition, varTypes, structFields)...)
		}
		if e.Consequence != nil {
			results = append(results, checkPrintFormatInExpr(e.Consequence, varTypes, structFields)...)
		}
		if e.Alternative != nil {
			results = append(results, checkPrintFormatInExpr(e.Alternative, varTypes, structFields)...)
		}
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			results = append(results, checkPrintFormatInExpr(elem, varTypes, structFields)...)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			results = append(results, checkPrintFormatInExpr(elem, varTypes, structFields)...)
		}
	case *parser.StructLiteral:
		for _, f := range e.Fields {
			if f.Value != nil {
				results = append(results, checkPrintFormatInExpr(f.Value, varTypes, structFields)...)
			}
		}
	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, is := range e.Body.Statements {
				results = append(results, checkPrintFormatInStmt(is, varTypes, structFields)...)
			}
		}
	}
	return results
}

// validatePrintFormatCall 驗證單個 print/printf/eprint/eprintf/sprintf 呼叫的格式字串。
// 只驗證含 '{' 的具名格式字串；C-style printf('...%d...', args) 不含 '{' 時跳過，
// 保留向後相容性。
func validatePrintFormatCall(e *parser.CallExpression, varTypes map[string]string, structFields map[string]map[string]string) []ValidateResult {
	strLit, ok := e.Arguments[0].(*parser.StringLiteral)
	if !ok {
		// 第一個引數不是字串字面量：無法在編譯期檢查，跳過
		return nil
	}
	// 只對含 '{' 的格式字串進行具名格式驗證。
	// 不含 '{' 的字串視為 C-style 格式（如 printf('%d', x)），不做驗證。
	if !strings.Contains(strLit.Value, "{") {
		return nil
	}
	segments, err := parser.ParseFormatString(strLit.Value)
	if err != nil {
		return []ValidateResult{{
			Line:    strLit.Token.Line,
			Column:  strLit.Token.Column,
			Message: fmt.Sprintf("format string error: %v", err),
		}}
	}
	var results []ValidateResult
	for _, seg := range segments {
		if seg.Field == nil {
			continue
		}
		field := seg.Field
		// 1. 檢查變數是否在作用域內
		varType, inScope := varTypes[field.Name]
		if !inScope {
			results = append(results, ValidateResult{
				Line:    strLit.Token.Line,
				Column:  strLit.Token.Column,
				Message: fmt.Sprintf("undefined variable '%s' in format string", field.Name),
			})
			continue
		}
		// 2. 規格已由 ParseFormatString 解析；若 Parsed 為 nil 表示無規格
		if field.Parsed == nil {
			continue
		}
		// 3. 檢查規格類型字元與變數型別相容性
		if msg := checkFormatSpecTypeCompat(field.Parsed.Type, varType, field.Spec); msg != "" {
			results = append(results, ValidateResult{
				Line:    strLit.Token.Line,
				Column:  strLit.Token.Column,
				Message: msg,
			})
		}
	}
	return results
}

// checkFormatSpecTypeCompat 檢查規格類型字元與變數型別是否相容。
// 回傳非空字串表示錯誤訊息。
func checkFormatSpecTypeCompat(typeChar byte, varType, specStr string) string {
	if typeChar == 0 {
		// 無類型字元：任何變數型別皆可
		return ""
	}
	switch typeChar {
	case 'b', 'c', 'd', 'o', 'x', 'X':
		// 整數類型
		if !isIntegerTypeStr(varType) {
			return fmt.Sprintf("format spec '%c' requires integer type, got '%s' (spec: %q)", typeChar, varType, specStr)
		}
	case 'e', 'E', 'f', 'F', 'g', 'G', '%':
		// 浮點數類型
		if !isFloatTypeStr(varType) {
			return fmt.Sprintf("format spec '%c' requires float type, got '%s' (spec: %q)", typeChar, varType, specStr)
		}
	case 's':
		// 字串或布林值
		if varType != "str" && varType != "bool" && !isIntegerTypeStr(varType) {
			return fmt.Sprintf("format spec 's' requires str/bool/integer type, got '%s' (spec: %q)", varType, specStr)
		}
	}
	return ""
}

// isIntegerTypeStr 判斷型別字串是否為整數類型
func isIntegerTypeStr(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "byte", "char":
		return true
	}
	return false
}

// isFloatTypeStr 判斷型別字串是否為浮點數類型
func isFloatTypeStr(t string) bool {
	return t == "f32" || t == "f64"
}

// collectModuleNames returns all known module ShortNames (from #use + auto-imported std modules).
func collectModuleNames(program *parser.Program) []string {
	seen := make(map[string]bool)
	var names []string

	for _, info := range knownStdModules() {
		if !seen[info.ShortName] {
			seen[info.ShortName] = true
			names = append(names, info.ShortName)
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
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
		}
	}

	return names
}

// ModuleExport holds an exported name and its string value from a module file.
type ModuleExport struct {
	Name  string
	Value string
	Type  string
}

// Per-module export cache: parsing a module's .no file to extract its exports
// is expensive, and the std modules are identical across all vet calls in a
// process. Cache the parsed exports keyed by module name.
var (
	moduleExportsCacheMu sync.Mutex
	moduleExportsCache   = make(map[string][]ModuleExport)
)

// GetModuleExports resolves module .no files and extracts their top-level
// LetStatement names with values (for hover) and function names.
// Results are cached per-module-name for the lifetime of the process.
func GetModuleExports(moduleNames []string) []ModuleExport {
	seen := make(map[string]bool)
	var exports []ModuleExport

	for _, m := range moduleNames {
		// Fast path: use cached exports for this module name.
		moduleExportsCacheMu.Lock()
		cached, ok := moduleExportsCache[m]
		moduleExportsCacheMu.Unlock()

		if !ok {
			// Parse the module once and cache its exports.
			cached = parseModuleExports(m)
			moduleExportsCacheMu.Lock()
			moduleExportsCache[m] = cached
			moduleExportsCacheMu.Unlock()
		}

		for _, e := range cached {
			if seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			exports = append(exports, e)
		}
	}

	return exports
}

// parseModuleExports resolves a single module's .no file and extracts its
// top-level exports (constants, functions, externs).
func parseModuleExports(moduleName string) []ModuleExport {
	filePath := resolveModulePath(moduleName)
	if filePath == "" {
		return nil
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	l := lexer.New(string(source))
	p := parser.New(l)
	modProg := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil
	}

	var exports []ModuleExport
	for _, stmt := range modProg.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			val := moduleExprValue(ls.Value)
			typeStr := ""
			if ls.Type != nil {
				typeStr = ls.Type.String()
			}
			exports = append(exports, ModuleExport{Name: ls.Name.Value, Value: val, Type: typeStr})
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			exports = append(exports, ModuleExport{Name: fd.Name, Value: ""})
		}
		if es, ok := stmt.(*parser.ExternStatement); ok && es.Name != nil {
			// Skip private FFI declarations (underscore-prefixed)
			if strings.HasPrefix(es.Name.Value, "_") {
				continue
			}
			exports = append(exports, ModuleExport{Name: es.Name.Value, Value: ""})
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
		// Use the token literal so values that overflow int64 (e.g. 18446744073709551615)
		// display correctly instead of showing the wrapped int64 value (e.g. -1).
		if e.Token.Literal != "" {
			return e.Token.Literal
		}
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

// resolveModulePath tries to locate a .no file for the given module name.
// It consults the knownStdModules() lookup table, matching by ShortPath
// (which omits the redundant directory name when dir==file), then uses
// FullPath to resolve the actual file.
func resolveModulePath(moduleName string) string {
	// 1. Consult knownStdModules lookup table.
	//    Match by ShortPath (or FullPath as fallback), resolve via FullPath.
	//    - "math"   → FullPath: "math"      → std/math.no
	//    - "net"    → FullPath: "net/net"   → std/net/net.no
	//    - "client" → FullPath: "net/client"→ std/net/client.no
	//    - "hmac"   → FullPath: "hash/hmac" → std/hash/hmac.no
	for _, info := range knownStdModules() {
		if info.ShortPath == moduleName || info.FullPath == moduleName || info.ShortName == moduleName {
			stdFile := GetStdSourceFile(info.FullPath)
			if _, err := os.Stat(stdFile); err == nil {
				return stdFile
			}
		}
	}

	// 2. Try direct path via GetStdSourceDir (respects NOLANG_STD_SRC env var)
	stdFile := GetStdSourceFile(moduleName)
	if _, err := os.Stat(stdFile); err == nil {
		return stdFile
	}

	// 3. Fallback: try relative to CWD
	candidates := []string{
		"std/" + moduleName + ".no",
		"src/std/" + moduleName + ".no",
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}

// ResolveStdModulePath is the exported version of resolveModulePath,
// for use by the LSP server to locate std module source files.
func ResolveStdModulePath(moduleName string) string {
	return resolveModulePath(moduleName)
}

func checkUndefinedVarsInStmt(stmt parser.Statement, definedVars, funcNames map[string]bool) []ValidateResult {
	var results []ValidateResult
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			results = append(results, checkUndefinedVarsInExpr(s.Expression, definedVars, funcNames, false)...)
		}
	case *parser.LetStatement:
		// Name is a definition — register it so it can be referenced later
		if s.Value != nil {
			results = append(results, checkUndefinedVarsInExpr(s.Value, definedVars, funcNames, false)...)
		}
		if s.Name != nil {
			definedVars[s.Name.Value] = true
		}
	case *parser.MultiAssignStatement:
		// Register all left-side variables as defined
		for _, target := range s.Targets {
			if ident, ok := target.(*parser.Identifier); ok {
				definedVars[ident.Value] = true
			}
		}
		if s.Value != nil {
			results = append(results, checkUndefinedVarsInExpr(s.Value, definedVars, funcNames, false)...)
		}
	case *parser.FunctionDefinition:
		// Parameters, generic params, and result params are defined vars at BOTH
		// the function scope (localDefs) AND the outer scope (definedVars), so
		// result/output parameters like 'ek' are visible at module level.
		localDefs := make(map[string]bool)
		for k, v := range definedVars {
			localDefs[k] = v
		}
		for _, p := range s.Parameters {
			definedVars[p.Name] = true
			localDefs[p.Name] = true
		}
		for _, gp := range s.GenericParams {
			definedVars[gp.Value] = true
			localDefs[gp.Value] = true
		}
		for _, r := range s.Results {
			if r.Name != "" {
				definedVars[r.Name] = true
				localDefs[r.Name] = true
			}
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
		// Labeled-conditional wrapper: `#2 val: { ... }` is encoded by
		// parseLabeledStatement as ForStatement{Condition: *IfExpression,
		// Body: Consequence}. Skip the synthetic Condition check and let
		// the Body be processed instead.
		if ifExpr, ok := s.Condition.(*parser.IfExpression); ok && s.Body == ifExpr.Consequence {
			if ifExpr.Condition != nil {
				if id, ok := ifExpr.Condition.(*parser.Identifier); ok {
					localDefs[id.Value] = true
				}
			}
		} else {
			if s.Init != nil {
				results = append(results, checkUndefinedVarsInStmt(s.Init, localDefs, funcNames)...)
			}
			if s.Condition != nil {
				results = append(results, checkUndefinedVarsInExpr(s.Condition, localDefs, funcNames, false)...)
			}
			if s.Update != nil {
				results = append(results, checkUndefinedVarsInStmt(s.Update, localDefs, funcNames)...)
			}
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
	case *parser.ExternStatement:
		if s.Name != nil {
			definedVars[s.Name.Value] = true
			funcNames[s.Name.Value] = true
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
			// Option constructors: val, err, ok are not real functions
			// nil/it are option-pattern keywords and match-binding variables
			if e.Value == "val" || e.Value == "err" || e.Value == "ok" ||
				e.Value == "nil" || e.Value == "it" {
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
func validateStmtTypes(stmt parser.Statement, funcNames map[string]bool, funcTypes map[string]string, selfType string, varTypes map[string]string) []ValidateResult {
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
		// 進入方法體時，更新 selfType
		methodSelfType := selfType
		if len(s.Parameters) > 0 && s.Parameters[0].Name == "self" {
			methodSelfType = s.Parameters[0].Type.String()
		}
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				errs := validateStmtTypes(bStmt, funcNames, funcTypes, methodSelfType, localTypes)
				results = append(results, errs...)
			}
		}

	case *parser.LetStatement:
		// Skip compiler-injected synthetic let statements (e.g. match arm `it` bindings)
		if s.IsSynthetic {
			// Still record its declared type so later synthetic references can resolve it
			if s.Type != nil && s.Type.String() != "" {
				varTypes[s.Name.Value] = s.Type.String()
			}
			break
		}
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
			// 只有新變數才記錄顯式型別；已存在的變數（如函式結果參數）不覆寫
			if _, exists := varTypes[s.Name.Value]; !exists {
				varTypes[s.Name.Value] = s.Type.String()
			}
		}
		if s.Value != nil {
			// 型別推斷
			inferredType := inferExprType(s.Value, varTypes, funcTypes, selfType)
			if inferredType != "" {
				if existingType, exists := varTypes[s.Name.Value]; exists {
					// 變數已有型別，檢查是否相容
					// 集合字面量 (ArrayLiteral/SliceLiteral) 可初始化陣列變數，跳過型別不匹配檢查
					_, isSlice := s.Value.(*parser.SliceLiteral)
					_, isArrayLit := s.Value.(*parser.ArrayLiteral)
					isArrayAssign := (isSlice || isArrayLit) && strings.HasPrefix(existingType, "[")
					// Per-element type checking for array/slice literals:
					// Instead of only skipping the overall type check, verify each
					// element is compatible with the declared element type.
					if isArrayAssign {
						elemType := extractArrayElemType(existingType)
						if elemType != "" {
							var elements []parser.Expression
							if isSlice {
								elements = s.Value.(*parser.SliceLiteral).Elements
							} else {
								elements = s.Value.(*parser.ArrayLiteral).Elements
							}
							for _, elem := range elements {
								elemInferred := inferExprType(elem, varTypes, funcTypes, selfType)
								if elemInferred != "" && elemInferred != elemType &&
									!isArgTypeCompatible(elemType, elemInferred, elem) {
									results = append(results, ValidateResult{
										Line:    s.Token.Line,
										Column:  s.Token.Column,
										Message: fmt.Sprintf("cannot assign %s value to %s element in array '%s'%s", elemInferred, elemType, s.Name.Value, narrowingHint(elemInferred, elemType)),
									})
								}
							}
						}
					}
					// Option 建構子：err(x) / val(x) / nil 可指派給 ?T 變數
					isOptionCtor := false
					if _, isNil := s.Value.(*parser.NilLiteral); isNil {
						if strings.HasPrefix(existingType, "?") {
							isOptionCtor = true
						}
					}
					if call, ok := s.Value.(*parser.CallExpression); ok {
						if cid, ok2 := call.Function.(*parser.Identifier); ok2 {
							if cid.Value == "err" || cid.Value == "val" || cid.Value == "ok" {
								if strings.HasPrefix(existingType, "?") {
									isOptionCtor = true
								}
							}
						}
					}
					// 隱式值賦值：val = n 可直接賦值給 ?T 變數（tag 自動設為 0）
					if strings.HasPrefix(existingType, "?") && !isOptionCtor {
						// 檢查推斷型別是否與 Option 內部型別相符
						innerType := existingType[1:]
						if inferredType == innerType || isArgTypeCompatible(innerType, inferredType, s.Value) {
							isOptionCtor = true
						}
					}
					if inferredType != existingType && isConcreteType(existingType) && !isArrayAssign && !isOptionCtor &&
						!isArgTypeCompatible(existingType, inferredType, s.Value) {
						valPos := s.Value.Pos()
						results = append(results, ValidateResult{
							Line:    valPos.Line,
							Column:  valPos.Column,
							Message: fmt.Sprintf("cannot assign %s value to %s variable '%s'%s", inferredType, existingType, s.Name.Value, narrowingHint(inferredType, existingType)),
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
					errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes)
					results = append(results, errs...)
				}
			}
			if ifExpr.Alternative != nil {
				for _, bStmt := range ifExpr.Alternative.Statements {
					errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes)
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
						valType := inferExprType(assign.Value, varTypes, funcTypes, selfType)
						// Option 建構子：err(x) / val(x) 可指派給任何 ?T 變數
						isOptionCtor := false
						if call, ok := assign.Value.(*parser.CallExpression); ok {
							if cid, ok2 := call.Function.(*parser.Identifier); ok2 {
								if cid.Value == "err" || cid.Value == "val" || cid.Value == "ok" {
									if strings.HasPrefix(existingType, "?") {
										isOptionCtor = true
									}
								}
							}
						}
						if valType != "" && valType != existingType && isConcreteType(existingType) && !isOptionCtor &&
							!isArgTypeCompatible(existingType, valType, assign.Value) {
							// Check if this is an array/slice literal assignment to a typed array variable
							_, isSlice := assign.Value.(*parser.SliceLiteral)
							_, isArrayLit := assign.Value.(*parser.ArrayLiteral)
							isArrayAssign := (isSlice || isArrayLit) && strings.HasPrefix(existingType, "[")
							if isArrayAssign {
								elemType := extractArrayElemType(existingType)
								if elemType != "" {
									var elements []parser.Expression
									if isSlice {
										elements = assign.Value.(*parser.SliceLiteral).Elements
									} else {
										elements = assign.Value.(*parser.ArrayLiteral).Elements
									}
									for _, elem := range elements {
										elemInferred := inferExprType(elem, varTypes, funcTypes, selfType)
										if elemInferred != "" && elemInferred != elemType &&
											!isArgTypeCompatible(elemType, elemInferred, elem) {
											results = append(results, ValidateResult{
												Line:    assign.Token.Line,
												Column:  assign.Token.Column,
												Message: fmt.Sprintf("cannot assign %s value to %s element in array '%s'%s", elemInferred, elemType, ident.Value, narrowingHint(elemInferred, elemType)),
											})
										}
									}
								}
							} else {
								results = append(results, ValidateResult{
									Line:    assign.Token.Line,
									Column:  assign.Token.Column,
									Message: fmt.Sprintf("cannot assign %s value to %s variable '%s'%s", valType, existingType, ident.Value, narrowingHint(valType, existingType)),
								})
							}
						}
					} else if !exists {
						// 首次賦值，記錄推斷型別
						valType := inferExprType(assign.Value, varTypes, funcTypes, selfType)
						if valType != "" {
							varTypes[ident.Value] = valType
						}
					}
				}
			}
		}

	case *parser.ForStatement:
		if s.Body != nil {
			for _, bStmt := range s.Body.Statements {
				errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes)
				results = append(results, errs...)
			}
		}

	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes)
			results = append(results, errs...)
		}

	case *parser.MultiAssignStatement:
		// Type-check multi-return assignment: fields[n], pos = parse-field(s, pos)
		// For each Identifier target, infer the type from the call's return types
		// and check compatibility with any existing type.
		if s.Value != nil {
			// Determine the return types of the call expression
			var returnTypes []string
			if callExpr, ok := s.Value.(*parser.CallExpression); ok {
				fnName := ""
				if ident, ok := callExpr.Function.(*parser.Identifier); ok {
					fnName = ident.Value
				} else if dot, ok := callExpr.Function.(*parser.DotExpression); ok {
					fnName = dot.Property
					// Try full method name (e.g., str.to-upper)
					if recv, ok := dot.Receiver.(*parser.Identifier); ok {
						fnName = recv.Value + "." + fnName
					}
				}
				if fnName != "" {
					// Look up function return types from funcTypes (only first return type is stored)
					// For multi-return, we need to look at the program's function definitions.
					// Since funcTypes only stores the first return type, we infer each target
					// from the call's return type signature.
					if rt, ok := funcTypes[fnName]; ok && len(s.Targets) == 1 {
						returnTypes = []string{rt}
					}
				}
			}
			// For each target, check type compatibility
			for i, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					// Only check if we know the return type for this position
					if i < len(returnTypes) {
						inferredType := returnTypes[i]
						if existingType, exists := varTypes[ident.Value]; exists {
							if inferredType != "" && existingType != "" &&
								inferredType != existingType &&
								isConcreteType(existingType) &&
								!isArgTypeCompatible(existingType, inferredType, nil) {
								results = append(results, ValidateResult{
									Line:    s.Token.Line,
									Column:  s.Token.Column,
									Message: fmt.Sprintf("cannot assign %s value to %s variable '%s'", inferredType, existingType, ident.Value),
								})
							}
						} else {
							// First assignment: record the inferred type
							if inferredType != "" {
								varTypes[ident.Value] = inferredType
							}
						}
					}
				}
				// IndexExpression targets (e.g., fields[n]) are assignments to
				// existing array elements — no new variable definition or type check needed.
			}
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

var (
	knownStdModulesOnce sync.Once
	knownStdModulesList []StdModuleInfo

	// Cache for CollectStdModuleSignatures: parsing all std modules is
	// expensive (~0.5s). VetFile is called once per .no file, so without
	// caching a full std vet spends ~95% of its time re-parsing modules.
	stdSigsOnce    sync.Once
	stdSigsCache   map[string][]string
	stdFieldsCache map[string]map[string]string
)

// StdModuleInfo holds information about a standard library module.
type StdModuleInfo struct {
	ShortName string // last path segment of FullPath, e.g. "rand", "math"
	FullPath  string // relative to std/, e.g. "hash/rand", "net/net", "math"
	ShortPath string // FullPath with redundant dir omitted when dir==file, e.g. "net", "hash/hmac", "math"
}

// knownStdModules returns all embedded standard library modules.
// Uses //go:embed to discover all .no files in src/std/ at compile time.
func knownStdModules() []StdModuleInfo {
	knownStdModulesOnce.Do(func() {
		var infos []StdModuleInfo
		seen := make(map[string]bool)

		var walkDir func(dir string)
		walkDir = func(dir string) {
			entries, err := nolang.StdFS.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range entries {
				path := dir + "/" + e.Name()
				if e.IsDir() {
					walkDir(path)
				} else if strings.HasSuffix(e.Name(), ".no") {
					rel := strings.TrimPrefix(path, "std/")
					fullPath := strings.TrimSuffix(rel, ".no")
					if !seen[fullPath] {
						seen[fullPath] = true
						shortName := fullPath
						if idx := strings.LastIndex(fullPath, "/"); idx >= 0 {
							shortName = fullPath[idx+1:]
						}
						// ShortPath: omit the redundant directory name when
						// file name equals directory name (e.g. "net/net" → "net").
						shortPath := fullPath
						if idx := strings.LastIndex(fullPath, "/"); idx >= 0 {
							dir := fullPath[:idx]
							file := fullPath[idx+1:]
							if dir == file {
								shortPath = file
							}
						}
						infos = append(infos, StdModuleInfo{
							ShortName: shortName,
							FullPath:  fullPath,
							ShortPath: shortPath,
						})
					}
				}
			}
		}
		walkDir("std")
		knownStdModulesList = infos
	})
	return knownStdModulesList
}

// GetStdModules returns StdModuleInfo for all embedded standard library modules.
func GetStdModules() []StdModuleInfo {
	return knownStdModules()
}

// CollectStdModuleSignatures parses all std module source files and returns
// function signatures (funcName → return types) and struct field types
// (structName → field name → field type). This is used by the LSP to inject
// extern signatures into the parser so that type inference (e.g. option match
// `it` binding) works correctly for cross-module method calls.
func CollectStdModuleSignatures() (map[string][]string, map[string]map[string]string) {
	stdSigsOnce.Do(func() {
		funcSigs := make(map[string][]string)
		structFields := make(map[string]map[string]string)

		for _, info := range knownStdModules() {
			modFilePath := ResolveStdModulePath(info.ShortPath)
			if modFilePath == "" {
				continue
			}
			source, err := os.ReadFile(modFilePath)
			if err != nil {
				continue
			}
			l := lexer.New(string(source))
			p := parser.New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				continue
			}
			for _, stmt := range prog.Statements {
				if fd, ok := stmt.(*parser.FunctionDefinition); ok {
					if len(fd.Results) > 0 {
						rets := make([]string, len(fd.Results))
						for i, r := range fd.Results {
							rets[i] = r.Type.String()
						}
						funcSigs[fd.Name] = rets
					}
				}
				if sd, ok := stmt.(*parser.StructDefinition); ok {
					fields := make(map[string]string)
					for _, f := range sd.Fields {
						if typeStr := structFieldTypeString(f); typeStr != "" {
							fields[f.Name] = typeStr
						}
					}
					structFields[sd.Name] = fields
				}
			}
		}

		stdSigsCache = funcSigs
		stdFieldsCache = structFields
	})
	return stdSigsCache, stdFieldsCache
}

// GetStdModuleShortNames returns the short names of all embedded standard library
// modules (for use in definedVars and module name registration).
func GetStdModuleShortNames() []string {
	infos := knownStdModules()
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.ShortName
	}
	return names
}

// GetStdModuleFullPaths returns the full paths of all embedded standard library
// modules (for use in file resolution and auto-loading).
func GetStdModuleFullPaths() []string {
	infos := knownStdModules()
	paths := make([]string, len(infos))
	for i, info := range infos {
		paths[i] = info.FullPath
	}
	return paths
}

// resolveModuleCalls walks the program and rewrites module.fn() calls
// where the DotExpression receiver chain matches an imported module ShortName.
// Supports single-level (base64.encode-std → encode-std) module paths.
// Also rewrites module.CONST constant accesses (e.g. base64.BASE64-STD → BASE64-STD).
func resolveModuleCalls(program *parser.Program, importedModules []string) {
	if len(importedModules) == 0 {
		return
	}
	modSet := make(map[string]bool)
	for _, m := range importedModules {
		modSet[m] = true
	}
	// Collect simple (non-dotted) function names — these are module-level
	// functions like `degrees` (from math.no). Method definitions like
	// `str.starts-with` or `path.exists` have dots and are NOT module functions.
	moduleFns := make(map[string]bool)
	// Collect top-level constant names (LetStatement) — these are module-level
	// constants like `BASE64-STD` (from encoding/base64.no), used to rewrite
	// module.CONST dotted accesses to bare constant references.
	moduleConsts := make(map[string]bool)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if !strings.Contains(fd.Name, ".") {
				moduleFns[fd.Name] = true
			}
		}
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			// 僅收集符合大寫常數命名規範的名稱（如 SEP、DOT、BASE64-STD、FNV-OFFSET）。
			// 小寫變數（如 len、i、result、path）不應視為模組常量，否則會造成
			// `path.len`（path 為函數參數）被錯誤改寫為 `len`（Identifier），
			// 導致 cookie.no 的 parse-response 在與其他模組連結時 IR 出現 `%len undefined`。
			// 函數定義（FunctionLiteral 值）另由 moduleFns 收集，不受此篩選影響。
			if isConstantName(ls.Name.Value) {
				moduleConsts[ls.Name.Value] = true
			}
			// Also collect functions defined as LetStatement with FunctionLiteral
			// value (e.g. `list-dir = (dirpath str) (entries []str) { ... }`).
			// Without this, module.fn() calls to these functions are not rewritten
			// to fn(), causing varLLVMType to fail type inference for the result
			// (it doesn't handle DotExpression function calls for module-prefixed
			// names, so variables assigned from them default to i64).
			if _, isFn := ls.Value.(*parser.FunctionLiteral); isFn {
				if !strings.Contains(ls.Name.Value, ".") {
					moduleFns[ls.Name.Value] = true
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		resolveModuleCallsInStmt(stmt, modSet, moduleFns, moduleConsts)
	}
}

// extractModulePathAndFunc walks a DotExpression chain to extract the
// module path (joined with "/") and the final property (function name).
// For example:
//
//	DotExpression{Identifier("math"), "sqrt"}     → ("math", "sqrt")
//	DotExpression{DotExpression{Identifier("hash"), "sha256"}, "sha256"}
//	                                              → ("hash/sha256", "sha256")
//
// Returns ("", "") if the chain contains non-Identifier nodes.
func extractModulePathAndFunc(dot *parser.DotExpression) (path, fnName string) {
	fnName = dot.Property
	var segments []string
	cur := dot.Receiver
	for {
		if d, ok := cur.(*parser.DotExpression); ok {
			segments = append([]string{d.Property}, segments...)
			cur = d.Receiver
		} else if ident, ok := cur.(*parser.Identifier); ok {
			segments = append([]string{ident.Value}, segments...)
			break
		} else {
			return "", ""
		}
	}
	path = strings.Join(segments, "/")
	return path, fnName
}

func resolveModuleCallsInStmt(stmt parser.Statement, modSet map[string]bool, moduleFns map[string]bool, moduleConsts map[string]bool) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveModuleCallsInExpr(s.Expression, modSet, moduleFns, moduleConsts)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, modSet, moduleFns, moduleConsts)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, modSet, moduleFns, moduleConsts)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveModuleCallsInExpr(s.Condition, modSet, moduleFns, moduleConsts)
		}
		if s.Init != nil {
			resolveModuleCallsInStmt(s.Init, modSet, moduleFns, moduleConsts)
		}
		if s.Update != nil {
			resolveModuleCallsInStmt(s.Update, modSet, moduleFns, moduleConsts)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts)
			}
		}
	}
}

func resolveModuleCallsInExpr(expr parser.Expression, modSet map[string]bool, moduleFns map[string]bool, moduleConsts map[string]bool) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// For curried calls (e.g. `mod.fn(args)(out1, out2)`), the outer
		// CallExpression's Function is itself a CallExpression. Recurse into
		// it first so the inner module-qualified name gets resolved.
		if _, isCall := e.Function.(*parser.CallExpression); isCall {
			e.Function = resolveModuleCallsInExpr(e.Function, modSet, moduleFns, moduleConsts)
		}
		// Check if this is a module.fn() call (single or multi-level).
		// Only rewrite when the function property is a known module-level function
		// and the receiver chain matches a known module ShortName.
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			modPath, fnName := extractModulePathAndFunc(dot)
			if modPath != "" && modSet[modPath] && moduleFns[fnName] {
				// Rewrite to direct function call
				e.Function = &parser.Identifier{
					Token: lexer.Token{Type: lexer.IDENT, Literal: fnName},
					Value: fnName,
				}
			}
		}
		// Recurse into arguments
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveModuleCallsInExpr(arg, modSet, moduleFns, moduleConsts)
		}
		return e

	case *parser.DotExpression:
		// 處理 module.CONST 常量存取（非呼叫），如 base64.BASE64-STD → BASE64-STD。
		// 僅當 receiver 鏈比對到已知模組 ShortName（modSet）且 property 為已知模組常量時改寫。
		// struct 欄位存取（f.read、p.path）的 receiver 變數名不在 modSet，不受影響。
		modPath, propName := extractModulePathAndFunc(e)
		if modPath != "" && modSet[modPath] && moduleConsts[propName] {
			return &parser.Identifier{
				Token: lexer.Token{Type: lexer.IDENT, Literal: propName},
				Value: propName,
			}
		}
		// 遞迴處理 receiver（鏈式存取如 a.b.c 的 struct 欄位）
		e.Receiver = resolveModuleCallsInExpr(e.Receiver, modSet, moduleFns, moduleConsts)
		return e

	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts)
		}
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, modSet, moduleFns, moduleConsts)
		}
		return e

	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, modSet, moduleFns, moduleConsts)
		}
		return e

	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, modSet, moduleFns, moduleConsts)
		}
		if e.Consequence != nil {
			e.Consequence = resolveModuleCallsInExpr(e.Consequence, modSet, moduleFns, moduleConsts)
		}
		if e.Alternative != nil {
			e.Alternative = resolveModuleCallsInExpr(e.Alternative, modSet, moduleFns, moduleConsts)
		}
		return e

	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, modSet, moduleFns, moduleConsts)
		}
		if e.Consequence != nil {
			for _, bodyStmt := range e.Consequence.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts)
			}
		}
		if e.Alternative != nil {
			for _, bodyStmt := range e.Alternative.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts)
			}
		}
		return e

	case *parser.GroupedExpression:
		if e.Expression != nil {
			e.Expression = resolveModuleCallsInExpr(e.Expression, modSet, moduleFns, moduleConsts)
		}
		return e

	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts)
		}
		if e.Index != nil {
			e.Index = resolveModuleCallsInExpr(e.Index, modSet, moduleFns, moduleConsts)
		}
		return e

	case *parser.SliceExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				e.Range.Start = resolveModuleCallsInExpr(e.Range.Start, modSet, moduleFns, moduleConsts)
			}
			if e.Range.End != nil {
				e.Range.End = resolveModuleCallsInExpr(e.Range.End, modSet, moduleFns, moduleConsts)
			}
		}
		return e

	case *parser.AssignExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts)
		}
		if e.Value != nil {
			e.Value = resolveModuleCallsInExpr(e.Value, modSet, moduleFns, moduleConsts)
		}
		return e

	default:
		return e
	}
}

// resolveSelfMethodCalls rewrites self.method(args) calls inside method bodies
// to StructType.method(self, args), where StructType is derived from the
// function's implicit self parameter.
//
// Also rewrites .field.method(args) calls (where .field is self.field) to
// FieldType.method(self.field, args), so that method calls on struct fields
// are dispatched to the field's type method. This mirrors the self.method()
// rewrite and is required because the LLVM generator only handles Identifier
// receivers, not DotExpression receivers.
func resolveSelfMethodCalls(program *parser.Program) {
	structFields := collectStructFields(program)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		if len(fd.Parameters) == 0 || fd.Parameters[0].Name != "self" {
			continue
		}
		selfType := fd.Parameters[0].Type.String()
		if fd.Body != nil {
			for _, bodyStmt := range fd.Body.Statements {
				resolveSelfInStmt(bodyStmt, selfType, structFields)
			}
		}
	}
}

// resolveMethodCalls rewrites user-written `Type.method(args)` static method
// calls to `module.Type.method(args)` using the typeOwner registry.
//
// prefixMethodNames() renames method definitions from `Type.method` to
// `module.Type.method` (e.g. bigint.cmp → bigint.bigint.cmp).  However,
// resolveModuleCalls() only rewrites simple `module.fn()` calls — it does
// NOT touch `Type.method()` calls because the method name contains a dot
// and is not collected into moduleFns.
//
// As a result, user-written `bigint.cmp(d, d2)` would keep fnName="bigint.cmp"
// at codegen time, but funcRetTypes has key "bigint.bigint.cmp", causing the
// call to generate undefined `@bigint.cmp`.
//
// This pass is the symmetric counterpart to prefixMethodNames: it rewrites
// call sites so they match the renamed definitions.  Only CallExpressions
// whose receiver is a bare Identifier matching a typeOwner key are rewritten;
// instance method calls (var.method()) and already-prefixed calls are left
// untouched.
func resolveMethodCalls(program *parser.Program, typeOwner map[string]string) {
	if len(typeOwner) == 0 {
		return
	}
	// Collect all defined method names (with dots) so we only rewrite calls
	// that actually target a known method.  This avoids rewriting field-access
	// patterns that happen to share a name with a type.
	definedMethods := make(map[string]bool)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if strings.Contains(fd.Name, ".") {
				definedMethods[fd.Name] = true
			}
		}
	}
	for _, stmt := range program.Statements {
		resolveMethodCallsInStmt(stmt, typeOwner, definedMethods)
	}
}

func resolveMethodCallsInStmt(stmt parser.Statement, typeOwner map[string]string, definedMethods map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveMethodCallsInExpr(s.Expression, typeOwner, definedMethods)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveMethodCallsInExpr(s.Value, typeOwner, definedMethods)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			s.Value = resolveMethodCallsInExpr(s.Value, typeOwner, definedMethods)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveMethodCallsInStmt(bodyStmt, typeOwner, definedMethods)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveMethodCallsInStmt(bodyStmt, typeOwner, definedMethods)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveMethodCallsInExpr(s.Condition, typeOwner, definedMethods)
		}
		if s.Init != nil {
			resolveMethodCallsInStmt(s.Init, typeOwner, definedMethods)
		}
		if s.Update != nil {
			resolveMethodCallsInStmt(s.Update, typeOwner, definedMethods)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveMethodCallsInStmt(bodyStmt, typeOwner, definedMethods)
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			s.ReturnValue = resolveMethodCallsInExpr(s.ReturnValue, typeOwner, definedMethods)
		}
	}
}

func resolveMethodCallsInExpr(expr parser.Expression, typeOwner map[string]string, definedMethods map[string]bool) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// Rewrite Type.method(args) → module.Type.method(args) when Type is
		// a known user-defined struct type (in typeOwner) and the method
		// exists (module.Type.method is in definedMethods).
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				typeName := recv.Value
				if mod, ok := typeOwner[typeName]; ok && mod != "" {
					fullName := mod + "." + typeName + "." + dot.Property
					if definedMethods[fullName] {
						e.Function = &parser.Identifier{
							Token: lexer.Token{Type: lexer.IDENT, Literal: fullName},
							Value: fullName,
						}
					}
				}
			}
		}
		// Recurse into arguments and nested calls
		if innerCall, ok := e.Function.(*parser.CallExpression); ok {
			e.Function = resolveMethodCallsInExpr(innerCall, typeOwner, definedMethods)
		}
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveMethodCallsInExpr(arg, typeOwner, definedMethods)
		}
		return e
	case *parser.DotExpression:
		e.Receiver = resolveMethodCallsInExpr(e.Receiver, typeOwner, definedMethods)
		return e
	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveMethodCallsInExpr(e.Left, typeOwner, definedMethods)
		}
		if e.Right != nil {
			e.Right = resolveMethodCallsInExpr(e.Right, typeOwner, definedMethods)
		}
		return e
	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveMethodCallsInExpr(e.Right, typeOwner, definedMethods)
		}
		return e
	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveMethodCallsInExpr(e.Left, typeOwner, definedMethods)
		}
		if e.Index != nil {
			e.Index = resolveMethodCallsInExpr(e.Index, typeOwner, definedMethods)
		}
		return e
	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveMethodCallsInExpr(e.Condition, typeOwner, definedMethods)
		}
		if e.Consequence != nil {
			for _, bs := range e.Consequence.Statements {
				resolveMethodCallsInStmt(bs, typeOwner, definedMethods)
			}
		}
		if e.Alternative != nil {
			for _, bs := range e.Alternative.Statements {
				resolveMethodCallsInStmt(bs, typeOwner, definedMethods)
			}
		}
		return e
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveMethodCallsInExpr(e.Condition, typeOwner, definedMethods)
		}
		if e.Consequence != nil {
			e.Consequence = resolveMethodCallsInExpr(e.Consequence, typeOwner, definedMethods)
		}
		if e.Alternative != nil {
			e.Alternative = resolveMethodCallsInExpr(e.Alternative, typeOwner, definedMethods)
		}
		return e
	}
	return expr
}

// collectStructFields builds a map from struct name to field name → field type
// string. Used by resolveSelfInExpr to look up field types when rewriting
// .field.method(args) calls.
// structFieldTypeString returns the full type string of a struct field,
// taking into account ArraySize and IsSlice flags that are stored separately
// from f.Type (which only holds the element type).
func structFieldTypeString(f *parser.StructField) string {
	if f.Type == nil {
		return ""
	}
	typeStr := f.Type.String()
	if f.ArraySize > 0 {
		return fmt.Sprintf("[%d]%s", f.ArraySize, typeStr)
	}
	if f.IsSlice {
		return "[]" + typeStr
	}
	return typeStr
}

func collectStructFields(program *parser.Program) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, stmt := range program.Statements {
		sd, ok := stmt.(*parser.StructDefinition)
		if !ok {
			continue
		}
		fields := make(map[string]string)
		for _, f := range sd.Fields {
			if typeStr := structFieldTypeString(f); typeStr != "" {
				fields[f.Name] = typeStr
			}
		}
		result[sd.Name] = fields
	}
	return result
}

func resolveSelfInStmt(stmt parser.Statement, selfType string, structFields map[string]map[string]string) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			resolveSelfInExpr(s.Expression, selfType, structFields)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			resolveSelfInExpr(s.Value, selfType, structFields)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			resolveSelfInExpr(s.Value, selfType, structFields)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveSelfInStmt(bodyStmt, selfType, structFields)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveSelfInStmt(bodyStmt, selfType, structFields)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			resolveSelfInExpr(s.Condition, selfType, structFields)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveSelfInStmt(bodyStmt, selfType, structFields)
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			resolveSelfInExpr(s.ReturnValue, selfType, structFields)
		}
	}
}

func resolveSelfInExpr(expr parser.Expression, selfType string, structFields map[string]map[string]string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			// Case 1: self.method(args) → StructType.method(self, args)
			if recv, ok := dot.Receiver.(*parser.Identifier); ok && recv.Value == "self" {
				concreteName := selfType + "." + dot.Property
				e.Function = &parser.Identifier{
					Token: lexer.Token{Type: lexer.IDENT, Literal: concreteName},
					Value: concreteName,
				}
				receiverArg := &parser.Identifier{
					Token: recv.Token,
					Value: "self",
				}
				e.Arguments = append([]parser.Expression{receiverArg}, e.Arguments...)
			}
			// Case 2: .field.method(args) → FieldType.method(.field, args)
			// where .field is self.field (DotExpression with Receiver=Identifier{"self"})
			if innerDot, ok := dot.Receiver.(*parser.DotExpression); ok {
				if innerRecv, ok := innerDot.Receiver.(*parser.Identifier); ok && innerRecv.Value == "self" {
					fieldName := innerDot.Property
					if fields, ok := structFields[selfType]; ok {
						if fieldType, ok := fields[fieldName]; ok {
							concreteName := fieldType + "." + dot.Property
							e.Function = &parser.Identifier{
								Token: lexer.Token{Type: lexer.IDENT, Literal: concreteName},
								Value: concreteName,
							}
							// receiver arg: .field (= self.field)
							receiverArg := &parser.DotExpression{
								Token:    innerDot.Token,
								Property: fieldName,
								Receiver: &parser.Identifier{
									Token: lexer.Token{Type: lexer.IDENT, Literal: "self"},
									Value: "self",
								},
							}
							e.Arguments = append([]parser.Expression{receiverArg}, e.Arguments...)
						}
					}
				}
			}
			// Case 3: .field[i].method(args) → ElementType.method(.field[i], args)
			// where .field is self.field and field is an array type [N]T
			if idxExpr, ok := dot.Receiver.(*parser.IndexExpression); ok {
				if innerDot, ok := idxExpr.Left.(*parser.DotExpression); ok {
					if innerRecv, ok := innerDot.Receiver.(*parser.Identifier); ok && innerRecv.Value == "self" {
						fieldName := innerDot.Property
						if fields, ok := structFields[selfType]; ok {
							if fieldType, ok := fields[fieldName]; ok {
								// fieldType 形如 "[N]T" — 提取元素型別 T
								closingIdx := strings.LastIndex(fieldType, "]")
								if closingIdx >= 0 && closingIdx+1 < len(fieldType) {
									elemType := fieldType[closingIdx+1:]
									concreteName := elemType + "." + dot.Property
									e.Function = &parser.Identifier{
										Token: lexer.Token{Type: lexer.IDENT, Literal: concreteName},
										Value: concreteName,
									}
									// receiver arg: .field[i] (= self.field[i])
									receiverArg := &parser.IndexExpression{
										Token: idxExpr.Token,
										Left: &parser.DotExpression{
											Token:    innerDot.Token,
											Property: fieldName,
											Receiver: &parser.Identifier{
												Token: lexer.Token{Type: lexer.IDENT, Literal: "self"},
												Value: "self",
											},
										},
										Index: idxExpr.Index,
									}
									e.Arguments = append([]parser.Expression{receiverArg}, e.Arguments...)
								}
							}
						}
					}
				}
			}
		}
		if innerCall, ok := e.Function.(*parser.CallExpression); ok {
			resolveSelfInExpr(innerCall, selfType, structFields)
		}
		for _, arg := range e.Arguments {
			resolveSelfInExpr(arg, selfType, structFields)
		}
	case *parser.InfixExpression:
		resolveSelfInExpr(e.Left, selfType, structFields)
		resolveSelfInExpr(e.Right, selfType, structFields)
	case *parser.PrefixExpression:
		resolveSelfInExpr(e.Right, selfType, structFields)
	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, s := range e.Consequence.Statements {
				resolveSelfInStmt(s, selfType, structFields)
			}
		}
		if e.Alternative != nil {
			for _, s := range e.Alternative.Statements {
				resolveSelfInStmt(s, selfType, structFields)
			}
		}
	case *parser.GroupedExpression:
		resolveSelfInExpr(e.Expression, selfType, structFields)
	case *parser.ConditionalExpression:
		resolveSelfInExpr(e.Condition, selfType, structFields)
		resolveSelfInExpr(e.Consequence, selfType, structFields)
		resolveSelfInExpr(e.Alternative, selfType, structFields)
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

// isConstantName 判斷名稱是否符合 Nolang 大寫常數命名規範。
// 規則：首字元為大寫 ASCII 字母（A-Z），且名稱中不含小寫 ASCII 字母（a-z）。
// 例：SBOX, FNV-OFFSET, O-EXCL, A, MAX-LEN 為常數；sum, i, myVar, Foo 不為常數。
// 用於重複定義偵測——大寫常數即使無顯式型別註記也應禁止重複賦值。
func isConstantName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' {
			return false
		}
	}
	return true
}

// matchesTargetPlatform 檢查宣告的 PlatformKeys 是否符合目標平台。
// 無 PlatformKeys（平台通用宣告）永遠回傳 true。
// goos/goarch 為空時（未設定目標平台），也回傳 true（向後相容）。
func matchesTargetPlatform(platformKeys []string, goos, goarch string) bool {
	if len(platformKeys) == 0 {
		return true
	}
	if goos == "" || goarch == "" {
		return true // 未設定目標平台，接受所有（向後相容）
	}
	targetKey := llvm.PlatformKeyFor(goos, goarch)
	if targetKey == "" {
		return true // 不支援的平台，接受所有
	}
	for _, pk := range platformKeys {
		if pk == targetKey {
			return true
		}
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
		resolveModuleConstantsInStmt(stmt, constants, nil)
	}
}

func resolveModuleConstantsInStmt(stmt parser.Statement, constants map[string]parser.Expression, locals map[string]bool) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveModuleConstantsInExpr(s.Expression, constants, locals)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveModuleConstantsInExpr(s.Value, constants, locals)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			s.Value = resolveModuleConstantsInExpr(s.Value, constants, locals)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			funcLocals := make(map[string]bool)
			if locals != nil {
				for k, v := range locals {
					funcLocals[k] = v
				}
			}
			for _, p := range s.Parameters {
				funcLocals[p.Name] = true
			}
			for _, r := range s.Results {
				if r.Name != "" {
					funcLocals[r.Name] = true
				}
			}
			collectLocalNames(s.Body, funcLocals)
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants, funcLocals)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveModuleConstantsInStmt(bodyStmt, constants, locals)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveModuleConstantsInExpr(s.Condition, constants, locals)
		}
		if s.Init != nil {
			resolveModuleConstantsInStmt(s.Init, constants, locals)
		}
		if s.Update != nil {
			resolveModuleConstantsInStmt(s.Update, constants, locals)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants, locals)
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			s.ReturnValue = resolveModuleConstantsInExpr(s.ReturnValue, constants, locals)
		}
	}
}

func collectLocalNames(block *parser.BlockStatement, locals map[string]bool) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			locals[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if fd.Body != nil {
				for _, p := range fd.Parameters {
					locals[p.Name] = true
				}
				for _, r := range fd.Results {
					if r.Name != "" {
						locals[r.Name] = true
					}
				}
				collectLocalNames(fd.Body, locals)
			}
		}
		if fs, ok := stmt.(*parser.ForStatement); ok {
			if fs.Init != nil {
				if ls, ok := fs.Init.(*parser.LetStatement); ok && ls.Name != nil {
					locals[ls.Name.Value] = true
				}
			}
			if fs.Body != nil {
				if fs.IterRange != nil && fs.IterRange.Variable != "" {
					locals[fs.IterRange.Variable] = true
				}
				collectLocalNames(fs.Body, locals)
			}
		}
		if bs, ok := stmt.(*parser.BlockStatement); ok {
			collectLocalNames(bs, locals)
		}
	}
}

func resolveModuleConstantsInExpr(expr parser.Expression, constants map[string]parser.Expression, locals map[string]bool) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		// Skip option type variant names (ok/nil/err) — these are built-in
		// keywords used in match patterns and must never be replaced by
		// module constants, even if a module happens to define a top-level
		// or local variable with the same name.
		if e.Value == "ok" || e.Value == "nil" || e.Value == "err" {
			return e
		}
		// Skip local variables — they shadow module constants
		if locals != nil && locals[e.Value] {
			return e
		}
		if lit, ok := constants[e.Value]; ok {
			return lit
		}
		return e
	case *parser.CallExpression:
		e.Function = resolveModuleConstantsInExpr(e.Function, constants, locals)
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveModuleConstantsInExpr(arg, constants, locals)
		}
		return e
	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants, locals)
		}
		if e.Right != nil {
			e.Right = resolveModuleConstantsInExpr(e.Right, constants, locals)
		}
		return e
	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveModuleConstantsInExpr(e.Right, constants, locals)
		}
		return e
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleConstantsInExpr(e.Condition, constants, locals)
		}
		if e.Consequence != nil {
			e.Consequence = resolveModuleConstantsInExpr(e.Consequence, constants, locals)
		}
		if e.Alternative != nil {
			e.Alternative = resolveModuleConstantsInExpr(e.Alternative, constants, locals)
		}
		return e
	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleConstantsInExpr(e.Condition, constants, locals)
		}
		if e.Consequence != nil {
			for _, bodyStmt := range e.Consequence.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants, locals)
			}
		}
		if e.Alternative != nil {
			for _, bodyStmt := range e.Alternative.Statements {
				resolveModuleConstantsInStmt(bodyStmt, constants, locals)
			}
		}
		return e
	case *parser.GroupedExpression:
		if e.Expression != nil {
			e.Expression = resolveModuleConstantsInExpr(e.Expression, constants, locals)
		}
		return e
	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants, locals)
		}
		if e.Index != nil {
			e.Index = resolveModuleConstantsInExpr(e.Index, constants, locals)
		}
		return e
	case *parser.SliceExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants, locals)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				e.Range.Start = resolveModuleConstantsInExpr(e.Range.Start, constants, locals)
			}
			if e.Range.End != nil {
				e.Range.End = resolveModuleConstantsInExpr(e.Range.End, constants, locals)
			}
		}
		return e
	case *parser.AssignExpression:
		if e.Left != nil {
			e.Left = resolveModuleConstantsInExpr(e.Left, constants, locals)
		}
		if e.Value != nil {
			e.Value = resolveModuleConstantsInExpr(e.Value, constants, locals)
		}
		return e
	default:
		return e
	}
}

// ValidateFuncArgs checks that function call argument types match the function signature.
// rootDir is optional — if empty, only locally defined function signatures are checked.
// If provided, imported function signatures from module files are also resolved.

// funcSigFromDef extracts the parameter and result type info from a function
// definition, used to build the signature table for type inference.
func funcSigFromDef(fd *parser.FunctionDefinition) *funcSig {
	params := make([]paramInfo, len(fd.Parameters))
	for i, p := range fd.Parameters {
		t := ""
		if p != nil && p.Type != nil {
			t = p.Type.String()
		}
		params[i] = paramInfo{Name: p.Name, Type: t, HasDefault: p.DefaultExpr != nil}
	}
	results := make([]paramInfo, len(fd.Results))
	for i, r := range fd.Results {
		t := ""
		if r != nil && r.Type != nil {
			t = r.Type.String()
		}
		results[i] = paramInfo{Name: r.Name, Type: t}
	}
	return &funcSig{ParamTypes: params, ResultTypes: results}
}

// funcSigFirstReturnType returns the type of the function's first result
// parameter, or "" if the function has no results.
func funcSigFirstReturnType(sig *funcSig) string {
	if sig == nil || len(sig.ResultTypes) == 0 {
		return ""
	}
	return sig.ResultTypes[0].Type
}

func ValidateFuncArgs(program *parser.Program, rootDir string) []ValidateResult {
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

	// 4. Validate call expressions
	for _, stmt := range program.Statements {
		results = append(results, checkCallArgsInStmt(stmt, sigs, make(map[string]string), structFields)...)
	}

	return results
}

// resolveUseModule resolves a UseStatement to its module program.
// It handles local paths (/path), std paths, and dependency paths (domain/...).
func resolveUseModule(use *parser.UseStatement, pkg *Package) *parser.Program {
	path := use.Path
	var prog *parser.Program
	var filePath string

	// Local module paths (starting with /)
	if strings.HasPrefix(path, "/") {
		if pkg == nil {
			return nil
		}
		relPath := strings.TrimPrefix(path, "/")
		filePath = filepath.Join(pkg.RootDir, relPath) + ".no"
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
	ParamTypes  []paramInfo
	ResultTypes []paramInfo
}

type paramInfo struct {
	Name       string
	Type       string
	HasDefault bool // 參數是否有默認值
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
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64":
		return true
	}
	return false
}

// intTypeRange returns the (min, max) range for a given integer type.
// byte is treated as u8 (0–255).
// char is treated as a Unicode code point (0–0x10FFFF).
func intTypeRange(t string) (min, max int64, ok bool) {
	switch t {
	case "i8":
		return -128, 127, true
	case "i16":
		return -32768, 32767, true
	case "i32":
		return -2147483648, 2147483647, true
	case "i64":
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
	}
	return 0, 0, false
}

// intTypeBits returns the bit width of an integer type.
// byte is treated as u8 (8 bits). Returns 0 for unknown types.
func intTypeBits(t string) int {
	switch t {
	case "i8", "u8", "byte":
		return 8
	case "i16", "u16":
		return 16
	case "i32", "u32":
		return 32
	case "i64", "u64":
		return 64
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
	case "u8", "u16", "u32", "u64", "byte":
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
func isArgTypeCompatible(expectedType, argType string, arg parser.Expression) bool {
	if argType == expectedType {
		return true
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
						if expectedType == "u64" {
							return true
						}
						return uval <= uint64(max)
					}
				}
				return false
			}
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

// findPackageRootFromFile walks up from a file path to find the directory containing mod.jsonc.
func findPackageRootFromFile(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		cfgFile := filepath.Join(dir, "mod.jsonc")
		if _, err := os.Stat(cfgFile); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// filterByExports filters a module's program to only include exported items declared in lib.no.
// It also auto-exports structs/enums/interfaces referenced by exported functions.
func filterByExports(prog *parser.Program, libPath string) *parser.Program {
	libSource, err := os.ReadFile(libPath)
	if err != nil {
		return prog
	}

	l := lexer.New(string(libSource))
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

	// Validate that all exported names exist in the module
	for _, e := range exports {
		if !defined[e.fn] {
			fmt.Fprintf(os.Stderr, "warning: export references undefined symbol %q (not found in module)\n", e.fn)
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

	// Filter statements
	filtered := &parser.Program{Statements: []parser.Statement{}}
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			if alias, ok := exported[s.Name]; ok {
				if alias != "" {
					s.Name = alias
				}
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
