package checker

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// Migrated from build/transpiler.go: semantic-checker subsystem
// (validators + module/type resolution helpers).

var validationFuncTypes map[string]string
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
		// run <expr> 返回不透明的 task 句柄（LLVM i8*），供 async-cancel 使用。
		// 显式推断为 "i8*" 使 `h = run ...` 的变量获得一致类型。
		if _, ok := expr.(*parser.RunExpression); ok {
			return "i8*"
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
				// 整數家族方法（to-str/to-i64 等）在 builtin/stdlib 以 int.* 統一註冊，
				// transpiler 也會將 i64.to-str() 重寫為 int.to-str()。校驗器同步歸一化，
				// 才能正確推斷回傳型別（例如 dns.no 的 a.to-str()）。
				if isValidationIntType(typeName) {
					switch dot.Property {
					case "to-str":
						return "str"
					case "to-i64":
						return "i64"
					case "to-u64":
						return "u64"
					case "to-bool":
						return "bool"
					case "to-f64":
						return "f64"
					case "to-f32":
						return "f32"
					}
				}
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
			// 例外：slice/array/str 的常用方法直接推斷，避免格式字串檢查誤報
			if strings.HasPrefix(typeName, "[]") || (strings.HasPrefix(typeName, "[") && strings.Contains(typeName, "]")) {
				switch dot.Property {
				case "len", "cap", "index", "index-from":
					return "i64"
				case "slice", "copy", "repeat":
					return typeName
				case "to-str":
					return "str"
				}
			}
			return ""
			}
			// typeName 為空：可能是模組限定的內建呼叫（如 number.char-to-str(13)）。
			// 此時接收者是模組名（非本作用域變數），對應裸內建回傳 str。
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if _, isVar := varTypes[recv.Value]; !isVar {
					// 模組限定函數呼叫（如 math.sin、sha1.sha1）：
					// 接收者為模組名而非變數，直接以裸函數名查 funcTypes。
					if retType, exists := funcTypes[dot.Property]; exists {
						return retType
					}
					switch recv.Value + "." + dot.Property {
					case "number.char-to-str":
						return "str"
					}
					switch dot.Property {
					case "char-to-str", "i64-to-str", "f64-to-str", "bool-to-str", "byte-to-str":
						return "str"
					}
				}
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
		return ""
	case "&", "|", "^", "<<", ">>":
		// 位元/移位運算：僅當左運算元為具體整數型別時回傳該型別，
		// 否則返回空字串（未知型別），跳過型別檢查
		leftType := inferExprType(e.Left, varTypes, funcTypes, selfType)
		if leftType != "" && intTypeBits(leftType) > 0 {
			return leftType
		}
		return ""
	default:
		return ""
	}
	case *parser.CastExpression:
		// 強轉表達式的型別即目標型別
		if e.Type != nil {
			return e.Type.String()
		}
		return ""
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
		return ""
	case *parser.StructLiteral:
		// A struct literal `name{}` has the type of the struct itself.
		if e.Type != "" {
			return e.Type
		}
		// 無顯式型別名：左側賦值目標已標示型別，返回空字串由型別檢查器跳過
		return ""
	case *parser.ArrayLiteral:
		// Array literal v[1, 2, ...] → infer type from elements
		if len(e.Elements) > 0 {
			elemType := inferExprType(e.Elements[0], varTypes, funcTypes, selfType)
		if elemType != "" {
			return fmt.Sprintf("[%d]%s", len(e.Elements), elemType)
		}
	}
		return ""
	case *parser.SliceLiteral:
		// Slice literal [1, 2, ...] → infer type from elements
		if len(e.Elements) > 0 {
			elemType := inferExprType(e.Elements[0], varTypes, funcTypes, selfType)
		if elemType != "" {
			return fmt.Sprintf("[]%s", elemType)
		}
	}
		return ""
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
type ValidateResult struct {
	Line      int
	Column    int
	EndColumn int
	Message   string
}
func ValidateEmbedAnnotations(program *parser.Program, sourcePath string) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		ls, ok := stmt.(*parser.LetStatement)
		if !ok {
			continue
		}
		// 查找 embed 註解（經由 side-table）
		var embedPath string
		var embedEntry *parser.AnnotationEntry
		for _, annot := range program.Sem.AnnotationsOf(ls) {
			if annot.Key != "embed" {
				continue
			}
			embedEntry = annot
			if sv, ok := annot.Value.(*parser.AnnotationStringValue); ok {
				embedPath = sv.Value
			} else if iv, ok := annot.Value.(*parser.AnnotationIdentValue); ok {
				embedPath = iv.Value
			}
		}
		if embedEntry == nil {
			continue
		}

		line := ls.Token.Line
		col := ls.Token.Column

		// 規則 2：不能與顯式 Value 共存
		if ls.Value != nil {
			results = append(results, ValidateResult{
				Line:    line,
				Column:  col,
				Message: "embed: cannot combine with explicit value",
			})
		}

		// 規則 1：必須是 []byte / [N]byte / fs.embed 類型
		if ls.Type == nil {
			results = append(results, ValidateResult{
				Line:    line,
				Column:  col,
				Message: "embed: only []byte / [N]byte / fs.embed declarations are supported, got untyped declaration",
			})
		} else {
			typeStr := ls.Type.String()
			isByteSlice := false
			isFsEmbed := false
			// 檢查是否為 []byte 或 [N]byte
			if st, ok := ls.Type.(*parser.SliceType); ok {
				if et, ok := st.Elem.(*parser.NamedType); ok {
					if et.Value == "byte" || et.Value == "u8" {
						isByteSlice = true
					}
				}
			}
			if at, ok := ls.Type.(*parser.ArrayType); ok {
				if et, ok := at.Elem.(*parser.NamedType); ok {
					if et.Value == "byte" || et.Value == "u8" {
						isByteSlice = true
					}
				}
			}
			// 檢查是否為 fs.embed（目錄嵌入）
			if nt, ok := ls.Type.(*parser.NamedType); ok {
				if nt.Value == "fs.embed" {
					isFsEmbed = true
				}
			}
			if !isByteSlice && !isFsEmbed {
				results = append(results, ValidateResult{
					Line:    line,
					Column:  col,
					Message: fmt.Sprintf("embed: only []byte / [N]byte / fs.embed declarations are supported, got %s", typeStr),
				})
			}
		}

		// 規則 3：文件/目錄必須存在
		if embedPath != "" {
			// 與 import 路徑一致：前置 "/" 表示相對於工作區根目錄（不是文件系統絕對路徑）
			embedRel := strings.TrimPrefix(embedPath, "/")
			resolvedPath := filepath.Join(pkg.ResolveEmbedBase(sourcePath), embedRel)
			info, err := os.Stat(resolvedPath)
			if err != nil {
				results = append(results, ValidateResult{
					Line:    line,
					Column:  col,
					Message: fmt.Sprintf("embed: file not found: %s (resolved: %s)", embedPath, resolvedPath),
				})
			} else if ls.Type != nil {
				// 規則 3b：fs.embed 類型必須指向目錄；[]byte/[N]byte 類型必須指向文件
				isFsEmbedType := false
				if nt, ok := ls.Type.(*parser.NamedType); ok && nt.Value == "fs.embed" {
					isFsEmbedType = true
				}
				if isFsEmbedType && !info.IsDir() {
					results = append(results, ValidateResult{
						Line:    line,
						Column:  col,
						Message: fmt.Sprintf("embed: fs.embed requires a directory, got file: %s (resolved: %s)", embedPath, resolvedPath),
					})
				}
				if !isFsEmbedType && info.IsDir() {
					results = append(results, ValidateResult{
						Line:    line,
						Column:  col,
						Message: fmt.Sprintf("embed: []byte requires a file, got directory: %s (resolved: %s). Use fs.embed type for directory embedding", embedPath, resolvedPath),
					})
				}
			}
		}
	}
	return results
}
func ValidateTypes(program *parser.Program) []ValidateResult {
	validationMu.Lock()
	defer validationMu.Unlock()
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
			fdPlatformKeys := program.Sem.PlatformKeysOf(fd)
			var sigKeys []string
			if len(fdPlatformKeys) == 0 {
				sigKeys = []string{sig + "\x00"}
			} else {
				sigKeys = make([]string, 0, len(fdPlatformKeys))
				for _, pk := range fdPlatformKeys {
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
	// 收集單具體型別別名（name = known-type，非 union、非 function-type），
	// 供 isConcreteType / isArgTypeCompatible 實施 newtype 語義。
	validationConcreteTypeAliases = collectConcreteTypeAliases(program)
	// 合併預載入的 std 模組別名（如 fs.no 的 fd=i64），使跨模組 newtype 檢查生效。
	// std 模組別名由 sync.Once 快取提供（CollectStdConcreteAliases），
	// 避免並行構建時的資料競爭。
	for k, v := range CollectStdConcreteAliases() {
		if _, exists := validationConcreteTypeAliases[k]; !exists {
			validationConcreteTypeAliases[k] = v
		}
	}
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
func FlattenUnion(name string, aliases map[string]*parser.TypeAlias) []parser.Type {
	// 內建類型（不可遞迴展開，視為葉節點）
	switch name {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "u128",
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
func ValidateNaming(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// Reuse the shared defined-vars collection (same first pass as
	// ValidateUndefinedVars) to know which names are global variables.
	definedVars := CollectDefinedVars(program)

	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.LetStatement); ok {
			continue
		}
		results = append(results, checkNaming(stmt, definedVars)...)
	}
	return results
}
func checkNaming(stmt parser.Statement, globalVars map[string]bool) []ValidateResult {
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
				results = append(results, checkNaming(bStmt, globalVars)...)
			}
		}
	case *parser.LetStatement:
		// Skip reassignments of known global variables (e.g., RAND-COUNTER).
		// Local variables with uppercase names (e.g., C) are still flagged.
		if s.Name != nil && globalVars[s.Name.Value] {
			return results
		}
		if s.Name != nil && !isValidVarName(s.Name.Value) {
			results = append(results, ValidateResult{
				Line:    s.Name.Token.Line,
				Column:  s.Name.Token.Column,
				Message: fmt.Sprintf("'%s' should use only lowercase letters and hyphens", s.Name.Value),
			})
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			results = append(results, checkNaming(bStmt, globalVars)...)
		}
	case *parser.ExpressionStatement:
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			if ifExpr.Consequence != nil {
				results = append(results, checkNaming(ifExpr.Consequence, globalVars)...)
			}
			if ifExpr.Alternative != nil {
				results = append(results, checkNaming(ifExpr.Alternative, globalVars)...)
			}
		}
	}
	return results
}
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

	case *parser.StringLiteral:
		// Named format strings like 'pi = {pi:.2f}' reference variables
		// via {name[:spec]} fields. Parse the literal and mark each
		// referenced name as used so ValidateUnusedVars does not flag
		// variables that are only consumed inside a format string.
		segments, err := parser.ParseFormatString(e.Value)
		if err != nil {
			return
		}
		for _, seg := range segments {
			if seg.Field == nil {
				continue
			}
			if seg.Field.IsExpr {
				// Expression field: parse and mark all identifiers as used
				l := lexer.New(seg.Field.Name)
				p := parser.New(l)
				prog := p.ParseProgram()
				if len(p.Errors()) > 0 {
					continue
				}
				for _, s := range prog.Statements {
					markReferencesInStatement(s, varSet, usedVars)
				}
				continue
			}
			if _, exists := varSet[seg.Field.Name]; exists {
				usedVars[seg.Field.Name] = true
			}
		}
	}
}
func CollectDefinedVars(program *parser.Program) map[string]bool {
	definedVars := make(map[string]bool)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			definedVars[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			definedVars[fd.Name] = true
		}
		if es, ok := stmt.(*parser.ExternStatement); ok && es.Name != nil {
			definedVars[es.Name.Value] = true
		}
		if ta, ok := stmt.(*parser.TypeAlias); ok {
			definedVars[ta.Name] = true
		}
	}
	return definedVars
}
func ValidateUndefinedVars(program *parser.Program, rootDir string) []ValidateResult {
	var results []ValidateResult

	// 1. Collect all defined names (shared first pass)
	definedVars := CollectDefinedVars(program)
	funcNames := make(map[string]bool) // function names

	// Collect function names (LetStatements and ExternStatements already
	// collected into definedVars by CollectDefinedVars above)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			funcNames[fd.Name] = true
		}
		if es, ok := stmt.(*parser.ExternStatement); ok && es.Name != nil {
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
		pkg, _ := pkg.LoadPackage(rootDir)
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
		if !ok || !fd.IsMethodDef {
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
func ValidateRedundantTypeAnnotation(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	// Build funcTypes for inferExprType
	validationFuncTypes = make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.Results) > 0 && fd.Results[0].Type != nil {
				validationFuncTypes[fd.Name] = fd.Results[0].Type.String()
			}
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			if len(es.Results) > 0 && es.Results[0].Type != nil {
				validationFuncTypes[es.Name.Value] = es.Results[0].Type.String()
			}
		}
	}
	varTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		results = append(results, checkRedundantTypeInStmt(stmt, varTypes)...)
	}
	return results
}
func checkRedundantTypeInStmt(stmt parser.Statement, varTypes map[string]string) []ValidateResult {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		var results []ValidateResult
		if s.Type != nil && !isInferredType(s.Type) && s.Value != nil && s.Name != nil {
			annotatedType := s.Type.String()
			inferredType := inferExprType(s.Value, varTypes, validationFuncTypes, "")
			if inferredType != "" && inferredType == annotatedType {
				results = append(results, ValidateResult{
					Line:    s.Type.Pos().Line,
					Column:  s.Type.Pos().Column,
					Message: fmt.Sprintf("type annotation '%s' can be omitted (inferred from value)", annotatedType),
				})
			}
			// Register the variable for subsequent checks
			varTypes[s.Name.Value] = annotatedType
		}
		return results
	case *parser.FunctionDefinition:
		if s.Body != nil {
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
			var results []ValidateResult
			for _, bs := range s.Body.Statements {
				results = append(results, checkRedundantTypeInStmt(bs, localTypes)...)
			}
			return results
		}
	case *parser.BlockStatement:
		var results []ValidateResult
		for _, bs := range s.Statements {
			results = append(results, checkRedundantTypeInStmt(bs, varTypes)...)
		}
		return results
	}
	return nil
}
func ValidateDuplicateVars(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	seen := make(map[string]struct{})
	for _, stmt := range program.Statements {
		results = append(results, checkStmtDuplicateVars(program.Sem, stmt, seen)...)
	}
	return results
}
func checkStmtDuplicateVars(sem *parser.SemanticContext, stmt parser.Statement, seen map[string]struct{}) []ValidateResult {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name == nil {
			return nil
		}

		// Detect parser-inferred types using the IsInferred flag.
		// Handles NamedType, NullableType (?T), SliceType, ArrayType, etc.
		isInferred := isInferredType(s.Type)

		// 計算複合 key：name + "\x00" + platformKey（無平台註解則 suffix 為空）
		// 同名 + 同平台才算衝突；不同平台或通用 vs 平台特定不衝突
		sPlatformKeys := sem.PlatformKeysOf(s)
		var compositeKeys []string
		if len(sPlatformKeys) == 0 {
			compositeKeys = []string{s.Name.Value + "\x00"}
		} else {
			compositeKeys = make([]string, 0, len(sPlatformKeys))
			for _, pk := range sPlatformKeys {
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
				results := checkStmtDuplicateVars(sem, bStmt, bodySeen)
				if len(results) > 0 {
					return results
				}
			}
		}
	case *parser.BlockStatement:
		for _, bStmt := range s.Statements {
			results := checkStmtDuplicateVars(sem, bStmt, seen)
			if len(results) > 0 {
				return results
			}
		}
	}
	return nil
}
func ValidateDependencyImports(program *parser.Program, rootDir string) []ValidateResult {
	if rootDir == "" {
		return nil
	}
	pkg, _ := pkg.LoadPackage(rootDir)
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
		if _, _, matched := pkg.MatchDependency(path); !matched {
			results = append(results, ValidateResult{
				Line:    us.Token.Line,
				Column:  us.Token.Column,
				Message: fmt.Sprintf("dependency not found: %q is not declared in package.jsonc dependencies", path),
			})
		}
	}
	return results
}
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

		l := lexer.NewCached(modFile, string(source))
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
func ValidateHexCase(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		results = append(results, checkHexCaseInStmt(stmt)...)
	}
	return results
}
func checkHexCaseInStmt(stmt parser.Statement) []ValidateResult {
	var results []ValidateResult
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			results = append(results, checkHexCaseInExpr(s.Expression)...)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			results = append(results, checkHexCaseInExpr(s.Value)...)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				results = append(results, checkHexCaseInStmt(bodyStmt)...)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			results = append(results, checkHexCaseInStmt(bodyStmt)...)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			results = append(results, checkHexCaseInExpr(s.ReturnValue)...)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			results = append(results, checkHexCaseInStmt(s.Init)...)
		}
		if s.Condition != nil {
			results = append(results, checkHexCaseInExpr(s.Condition)...)
		}
		if s.Update != nil {
			results = append(results, checkHexCaseInStmt(s.Update)...)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				results = append(results, checkHexCaseInStmt(bodyStmt)...)
			}
		}
	}
	return results
}
func hasUpperHex(literal string) bool {
	if len(literal) >= 2 && literal[0] == '0' && literal[1] == 'X' {
		return true
	}
	if len(literal) >= 2 && literal[0] == '0' && literal[1] == 'x' {
		for _, c := range literal[2:] {
			if c >= 'A' && c <= 'F' {
				return true
			}
		}
	}
	if len(literal) == 3 && literal[0] == 'x' {
		for _, c := range literal[1:] {
			if c >= 'A' && c <= 'F' {
				return true
			}
		}
	}
	return false
}
func checkHexCaseInExpr(expr parser.Expression) []ValidateResult {
	var results []ValidateResult
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		if hasUpperHex(e.Token.Literal) {
			results = append(results, ValidateResult{
				Line:    e.Token.Line,
				Column:  e.Token.Column,
				Message: fmt.Sprintf("hex literal '%s' uses uppercase; format will convert to lowercase (e.g. 0xff)", e.Token.Literal),
			})
		}
	case *parser.ByteLiteral:
		if hasUpperHex(e.Token.Literal) {
			results = append(results, ValidateResult{
				Line:    e.Token.Line,
				Column:  e.Token.Column,
				Message: fmt.Sprintf("byte literal '%s' uses uppercase hex; format will convert to lowercase (e.g. xff)", e.Token.Literal),
			})
		}
	case *parser.InfixExpression:
		results = append(results, checkHexCaseInExpr(e.Left)...)
		results = append(results, checkHexCaseInExpr(e.Right)...)
	case *parser.PrefixExpression:
		results = append(results, checkHexCaseInExpr(e.Right)...)
	case *parser.GroupedExpression:
		results = append(results, checkHexCaseInExpr(e.Expression)...)
	case *parser.CallExpression:
		for _, arg := range e.Arguments {
			results = append(results, checkHexCaseInExpr(arg)...)
		}
	case *parser.DotExpression:
		results = append(results, checkHexCaseInExpr(e.Receiver)...)
	case *parser.IndexExpression:
		results = append(results, checkHexCaseInExpr(e.Left)...)
		results = append(results, checkHexCaseInExpr(e.Index)...)
	case *parser.AssignExpression:
		results = append(results, checkHexCaseInExpr(e.Value)...)
	case *parser.ConditionalExpression:
		results = append(results, checkHexCaseInExpr(e.Condition)...)
		results = append(results, checkHexCaseInExpr(e.Consequence)...)
		results = append(results, checkHexCaseInExpr(e.Alternative)...)
	case *parser.ArrayLiteral:
		for _, elem := range e.Elements {
			results = append(results, checkHexCaseInExpr(elem)...)
		}
	case *parser.SliceLiteral:
		for _, elem := range e.Elements {
			results = append(results, checkHexCaseInExpr(elem)...)
		}
	case *parser.StructLiteral:
		for _, field := range e.Fields {
			if field.Value != nil {
				results = append(results, checkHexCaseInExpr(field.Value)...)
			}
		}
	case *parser.MapLiteral:
		for _, pair := range e.Pairs {
			results = append(results, checkHexCaseInExpr(pair.Key)...)
			results = append(results, checkHexCaseInExpr(pair.Value)...)
		}
	case *parser.CastExpression:
		results = append(results, checkHexCaseInExpr(e.Expr)...)
	}
	return results
}
func ValidatePrintFormat(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// 收集 struct 欄位型別資訊，用於解析結構欄位存取
	structFields := collectStructFields(program)

	// 構建函數返回類型映射，供 inferExprType 推導用戶定義函數呼叫的返回類型
	validationFuncTypes = make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.Results) > 0 && fd.Results[0].Type != nil {
				validationFuncTypes[fd.Name] = fd.Results[0].Type.String()
			}
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			if len(es.Results) > 0 && es.Results[0].Type != nil {
				validationFuncTypes[es.Name.Value] = es.Results[0].Type.String()
			}
		}
	}
	// 預填 stdlib 方法回傳型別（定義在 src/std/*.no，在 vet 階段尚未合併）
	// 覆蓋 str、slice/array 的常用方法，避免 inferExprType 回傳空字串
	stdlibMethodTypes := map[string]string{
		"str.len":          "i64",
		"str.index":        "i64",
		"str.index-from":   "i64",
		"str.slice":        "str",
		"str.contains":     "bool",
		"str.starts-with":  "bool",
		"str.ends-with":    "bool",
		"str.to-upper":     "str",
		"str.to-lower":     "str",
		"str.trim":         "str",
		"str.trim-left":   "str",
		"str.trim-right":   "str",
		"str.repeat":       "str",
		"str.copy":         "str",
		"str.to-i64":       "i64",
		"str.to-bool":      "bool",
		"str.to-f64":       "f64",
		"str.split":        "[][]byte",
		"str.replace":      "str",
		"str.to-hex":       "str",
		"str.to-hex-lower": "str",
	}
	for k, v := range stdlibMethodTypes {
		if _, exists := validationFuncTypes[k]; !exists {
			validationFuncTypes[k] = v
		}
	}

	// 走訪頂層敘述，追蹤變數作用域
	// 頂層變數共用同一作用域（模組級），故使用單一 varTypes map
	varTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		results = append(results, checkPrintFormatInStmt(stmt, varTypes, structFields)...)
	}
	return results
}
func isPrintFormatCall(fnName string) bool {
	switch fnName {
	case "print", "eprint", "format", "printf", "eprintf", "sprintf",
		"fmt.print", "fmt.eprint", "fmt.format",
		"fmt.printf", "fmt.eprintf", "fmt.sprintf":
		return true
	}
	return false
}
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
			// 註冊變數：始終註冊變數名稱，即使型別無法推導。
			// 型別未知時設為空字串 ""，validatePrintFormatCall 會跳過型別相容性檢查。
			// 這避免了對已賦值變數誤報 "undefined variable"。
			if s.Name != nil {
				if s.Type != nil && s.Type.String() != "" {
					varTypes[s.Name.Value] = s.Type.String()
				} else if _, exists := varTypes[s.Name.Value]; !exists {
					varTypes[s.Name.Value] = inferExprType(s.Value, varTypes, validationFuncTypes, "")
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
			var results []ValidateResult
			results = append(results, checkPrintFormatInExpr(s.Value, varTypes, structFields)...)
			// 註冊所有賦值目標變數名稱
			for _, target := range s.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					if _, exists := varTypes[ident.Value]; !exists {
						varTypes[ident.Value] = ""
					}
				}
			}
			return results
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
		// 註冊迴圈迭代變數（如 i <- [0..n): 中的 i）
		if s.IterRange != nil && s.IterRange.Variable != "" {
			if _, exists := varTypes[s.IterRange.Variable]; !exists {
				varTypes[s.IterRange.Variable] = "i64"
			}
		}
		// 註冊計次迴圈變數（{ } * N 語法）
		if s.CountExpr != nil {
			results = append(results, checkPrintFormatInExpr(s.CountExpr, varTypes, structFields)...)
		}
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
		// Expression fields (e.g. {hash[i] & 255:02x}) skip variable
		// scope/type validation here — the expression is validated when
		// parsed by the code generator.
		if field.IsExpr {
			continue
		}
		// 1. 檢查變數是否在作用域內
		// 支援點表達式欄位名（如 content.len）：先查全名，再查基礎變數名
		varType, inScope := varTypes[field.Name]
		if !inScope {
			if dotIdx := strings.Index(field.Name, "."); dotIdx > 0 {
				baseName := field.Name[:dotIdx]
				varType, inScope = varTypes[baseName]
			}
		}
		if !inScope {
			results = append(results, ValidateResult{
				Line:    strLit.Token.Line,
				Column:  strLit.Token.Column,
				Message: fmt.Sprintf("undefined variable '%s' in format string", field.Name),
			})
			continue
		}
		// 型別未知（推導失敗）：跳過型別相容性檢查，由 LLVM 端驗證
		if varType == "" {
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
func isIntegerTypeStr(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128", "byte", "char":
		return true
	}
	return false
}
func isFloatTypeStr(t string) bool {
	return t == "f32" || t == "f64"
}
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
type ModuleExport struct {
	Name  string
	Value string
	Type  string
}
var (
	moduleExportsCacheMu sync.Mutex
	moduleExportsCache   = make(map[string][]ModuleExport)
)
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
func parseModuleExports(moduleName string) []ModuleExport {
	for _, info := range knownStdModules() {
		if info.ShortPath == moduleName || info.FullPath == moduleName || info.ShortName == moduleName {
			embedPath := "std/" + info.FullPath + ".no"
			if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
				return parseModuleExportsFromSource(data)
			}
		}
	}
	return nil
}
func parseModuleExportsFromSource(source []byte) []ModuleExport {
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
func collectModuleExports(program *parser.Program, moduleNames []string) []string {
	exports := GetModuleExports(moduleNames)
	var names []string
	for _, e := range exports {
		names = append(names, e.Name)
	}
	return names
}
func resolveModulePath(moduleName string) string {
	// 1. Consult knownStdModules lookup table.
	//    Match by ShortPath (or FullPath as fallback), resolve via FullPath.
	//    - "math"   → FullPath: "math"      → std/math.no
	//    - "net"    → FullPath: "net/net"   → std/net/net.no
	//    - "client" → FullPath: "net/client"→ std/net/client.no
	//    - "hmac"   → FullPath: "hash/hmac" → std/hash/hmac.no
	for _, info := range knownStdModules() {
		if info.ShortPath == moduleName || info.FullPath == moduleName || info.ShortName == moduleName {
			stdFile := pkg.GetStdSourceFile(info.FullPath)
			if _, err := os.Stat(stdFile); err == nil {
				return stdFile
			}
		}
	}

	// 2. Try direct path via GetStdSourceDir (respects NOLANG_STD_SRC env var)
	stdFile := pkg.GetStdSourceFile(moduleName)
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
		// Body: Consequence, IsCondWrapper: true}. Skip the synthetic
		// Condition check and let the Body be processed instead.
		// 直接讀取 IsCondWrapper 欄位（parser 在合成位置顯式設置），
		// 避免依賴 `s.Body == ifExpr.Consequence` 指標相等啟發式。
		if s.IsCondWrapper {
			if ifExpr, ok := s.Condition.(*parser.IfExpression); ok && ifExpr.Condition != nil {
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
			// Special hint for 'self' (.) used outside struct methods
			if e.Value == "self" {
				results = append(results, ValidateResult{
					Line:    e.Token.Line,
					Column:  e.Token.Column,
					Message: "'self' (.) can only be used inside struct methods; if you meant the match value, use 'it'",
				})
			} else {
				results = append(results, ValidateResult{
					Line:    e.Token.Line,
					Column:  e.Token.Column,
					Message: fmt.Sprintf("'%s' is not defined", e.Value),
				})
			}
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
			// val(x) 作為構造器已廢棄：應改用 ok(x) 或隱式賦值 val = n
			if call, ok := s.Value.(*parser.CallExpression); ok {
				if cid, ok2 := call.Function.(*parser.Identifier); ok2 {
					if cid.Value == "val" {
						callPos := call.Pos()
						results = append(results, ValidateResult{
							Line:    callPos.Line,
							Column:  callPos.Column,
							Message: "val() constructor is deprecated; use ok(x) for explicit construction or `name = expr` for implicit assignment",
						})
					}
				}
			}
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
					// Option 建構子：err(x) / ok(x) / nil 可指派給 ?T 變數
					// 注意：val(x) 已廢棄作為構造器，應改用 ok(x)；隱式賦值請用 val = n
					isOptionCtor := false
					if _, isNil := s.Value.(*parser.NilLiteral); isNil {
						if strings.HasPrefix(existingType, "?") {
							isOptionCtor = true
						}
					}
					if call, ok := s.Value.(*parser.CallExpression); ok {
						if cid, ok2 := call.Function.(*parser.Identifier); ok2 {
							if cid.Value == "err" || cid.Value == "ok" {
								if strings.HasPrefix(existingType, "?") {
									isOptionCtor = true
								}
							}
							if cid.Value == "val" {
								// val(x) 作為構造器已廢棄：應改用 ok(x) 或隱式賦值 val = n
								callPos := call.Pos()
								results = append(results, ValidateResult{
									Line:    callPos.Line,
									Column:  callPos.Column,
									Message: "val() constructor is deprecated; use ok(x) for explicit construction or `name = expr` for implicit assignment",
								})
								isOptionCtor = true
							}
						}
					}
					// 隱式值賦值：val = n 可直接賦值給 ?T 變數（tag 自動設為 0）
					if strings.HasPrefix(existingType, "?") && !isOptionCtor {
						// 檢查推斷型別是否與 Option 內部型別相符
						innerType := existingType[1:]
						if inferredType == innerType || isArgTypeCompatible(innerType, inferredType, s.Value) {
							isOptionCtor = true
						} else if _, _, ok1 := intTypeRange(innerType); ok1 {
							if _, _, ok2 := intTypeRange(inferredType); ok2 {
								// 整數型別之間的隱式賦值給 Option 變數允許窄化：
								// generator 在 copyToData 中將所有整數存為 i64（8 位元組），
								// 不區分 i8/u8/i16 等寬度；窄化安全性由程式碼邏輯（如範圍檢查）保證。
								isOptionCtor = true
							}
						}
					}
					if inferredType != "" && inferredType != existingType && isConcreteType(existingType) && !isArrayAssign && !isOptionCtor &&
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
		// val(x) 作為構造器已廢棄：檢查語句表達式中的 val() 呼叫
		if call, ok := s.Expression.(*parser.CallExpression); ok {
			if cid, ok2 := call.Function.(*parser.Identifier); ok2 && cid.Value == "val" {
				callPos := call.Pos()
				results = append(results, ValidateResult{
					Line:    callPos.Line,
					Column:  callPos.Column,
					Message: "val() constructor is deprecated; use ok(x) for explicit construction or `name = expr` for implicit assignment",
				})
			}
		}
		// 處理 if 表示式
		if ifExpr, ok := s.Expression.(*parser.IfExpression); ok {
			// 注意：不在 if 條件中檢查 val() 構造器。
			// match 脫糖後 if 條件可能含 `matched == val(v)`，此處 val 可能是
			// 用戶自定義枚舉的 variant，不能簡單當作廢棄的 Option 構造器報錯。
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
						// Option 建構子：err(x) / ok(x) 可指派給任何 ?T 變數
						// 注意：val(x) 已廢棄作為構造器，應改用 ok(x)
						isOptionCtor := false
						if call, ok := assign.Value.(*parser.CallExpression); ok {
							if cid, ok2 := call.Function.(*parser.Identifier); ok2 {
								if cid.Value == "err" || cid.Value == "ok" {
									if strings.HasPrefix(existingType, "?") {
										isOptionCtor = true
									}
								}
								if cid.Value == "val" {
									callPos := call.Pos()
									results = append(results, ValidateResult{
										Line:    callPos.Line,
										Column:  callPos.Column,
										Message: "val() constructor is deprecated; use ok(x) for explicit construction or `name = expr` for implicit assignment",
									})
									isOptionCtor = true
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
	stdSigsOnce       sync.Once
	stdSigsCache      map[string][]string
	stdFieldsCache    map[string]map[string]string
	stdAliasesCache   map[string]string // 單具體型別別名快取（如 "fd" → "i64"）
	stdStructModCache map[string]string // struct name → module short name（如 "conn" → "tls"）
	// stdProgramsCache 緩存 CollectStdModuleSignatures PASS1 已解析的 std 模組
	// Program，按「內容哈希」(cache.ContentKey) 索引。no vet src/std 的第二遍
	// 可直接復用，跳過磁盤文件的重新 parse；內容與 embed 不一致時自然不命中，
	// 安全回退到正常 parse。僅在 NOLANG_REUSE_STD_AST 開關開啟時由 build 端查詢。
	stdProgramsCache map[string]*parser.Program
)
type StdModuleInfo struct {
	ShortName string // last path segment of FullPath, e.g. "rand", "math"
	FullPath  string // relative to std/, e.g. "hash/rand", "net/net", "math"
	ShortPath string // FullPath with redundant dir omitted when dir==file, e.g. "net", "hash/hmac", "math"
}
func debugCountHashFns(stage string, merged *parser.Program) {
	_ = stage
	_ = merged
}
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
func GetStdModules() []StdModuleInfo {
	return knownStdModules()
}
type JsModuleInfo struct {
	ShortName string // 檔名去掉 .no 與目錄前綴，如 "console-log"
	FullPath  string // 相對於 js/，如 "console-log"
	ShortPath string // 與 FullPath 相同（JS 相容層目前無子目錄，dir==file 的情況不適用）
}
var (
	knownJsModulesOnce sync.Once
	knownJsModulesList []JsModuleInfo
)
func knownJsModules() []JsModuleInfo {
	knownJsModulesOnce.Do(func() {
		var infos []JsModuleInfo
		seen := make(map[string]bool)

		var walkDir func(dir string)
		walkDir = func(dir string) {
			entries, err := nolang.JsFS.ReadDir(dir)
			if err != nil {
				return
			}
			for _, e := range entries {
				path := dir + "/" + e.Name()
				if e.IsDir() {
					walkDir(path)
				} else if strings.HasSuffix(e.Name(), ".no") {
					rel := strings.TrimPrefix(path, "js/")
					fullPath := strings.TrimSuffix(rel, ".no")
					if !seen[fullPath] {
						seen[fullPath] = true
						shortName := fullPath
						if idx := strings.LastIndex(fullPath, "/"); idx >= 0 {
							shortName = fullPath[idx+1:]
						}
						shortPath := fullPath
						if idx := strings.LastIndex(fullPath, "/"); idx >= 0 {
							dirName := fullPath[:idx]
							file := fullPath[idx+1:]
							if dirName == file {
								shortPath = file
							}
						}
						infos = append(infos, JsModuleInfo{
							ShortName: shortName,
							FullPath:  fullPath,
							ShortPath: shortPath,
						})
					}
				}
			}
		}
		walkDir("js")
		knownJsModulesList = infos
	})
	return knownJsModulesList
}
func GetJsModules() []JsModuleInfo {
	return knownJsModules()
}
func CollectStdModuleSignatures() (map[string][]string, map[string]map[string]string) {
	stdSigsOnce.Do(func() {
		funcSigs := make(map[string][]string)
		structFields := make(map[string]map[string]string)
		aliases := make(map[string]string)
		structMod := make(map[string]string) // struct name → module short name

		// PASS 1: 解析所有模組並暫存，同時統計裸 struct 名的跨模組定義數。
		// 多模組同名結構體（如 server-conn 定義於 server/tls/sse/ws）的裸名
		// 有歧義：函數簽名快照若記錄裸名（?server-conn），解析期 it 綁定會
		// 標注錯誤型別，合併後 codegen 解析到錯誤模組的結構體。
		type parsedMod struct {
			info StdModuleInfo
			prog *parser.Program
		}
		var mods []parsedMod
		structCount := make(map[string]int)
		funcCount := make(map[string]int)
		// 並行解析：各模組的 lex+parse 相互獨立，僅共用有鎖的 token LRU。
		// 結果按 knownStdModules() 原順序寫回，保持 last-wins 合併語義不變。
		known := knownStdModules()
		parsed := make([]*parser.Program, len(known))
		parsedCK := make([]string, len(known))
		{
			var wg sync.WaitGroup
			sem := make(chan struct{}, runtime.GOMAXPROCS(0))
			for i, info := range known {
				wg.Add(1)
				go func(i int, info StdModuleInfo) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					embedPath := "std/" + info.FullPath + ".no"
					source, err := nolang.StdFS.ReadFile(embedPath)
					if err != nil {
						return
					}
					l := lexer.NewCached(embedPath, string(source))
					p := parser.New(l)
					prog := p.ParseProgram()
					if len(p.Errors()) > 0 {
						return
					}
					parsed[i] = prog
					parsedCK[i] = cache.ContentKey(string(source))
				}(i, info)
			}
			wg.Wait()
		}
		stdProgramsCache = make(map[string]*parser.Program)
		for i, info := range known {
			prog := parsed[i]
			if prog == nil {
				continue
			}
			stdProgramsCache[parsedCK[i]] = prog
			mods = append(mods, parsedMod{info, prog})
			for _, stmt := range prog.Statements {
				if sd, ok := stmt.(*parser.StructDefinition); ok {
					structCount[sd.Name]++
				}
				if fd, ok := stmt.(*parser.FunctionDefinition); ok && !fd.IsMethodDef && !strings.Contains(fd.Name, ".") {
					funcCount[fd.Name]++
				}
			}
		}

		// qualifyRet: 函數結果型別若引用「本模組定義且跨模組同名歧義」的
		// struct 裸名，改記為 module.name（與 build 合併後 prefixModuleStatements
		// 的重命名世界一致）。唯一裸名保持不變（解析期 structFields 查找仍用裸名）。
		qualifyRet := func(typeStr, modShort string, ownStructs map[string]bool) string {
			bare := strings.TrimPrefix(typeStr, "?")
			if bare == typeStr {
				// 非 option 形式：僅處理裸名
				if ownStructs[bare] && structCount[bare] > 1 {
					return modShort + "." + bare
				}
				return typeStr
			}
			if ownStructs[bare] && structCount[bare] > 1 {
				return "?" + modShort + "." + bare
			}
			return typeStr
		}

		// PASS 2: 收集簽名/欄位/別名
		for _, m := range mods {
			ownStructs := make(map[string]bool)
			for _, stmt := range m.prog.Statements {
				if sd, ok := stmt.(*parser.StructDefinition); ok {
					ownStructs[sd.Name] = true
				}
			}
			for _, stmt := range m.prog.Statements {
				if fd, ok := stmt.(*parser.FunctionDefinition); ok {
					if len(fd.Results) > 0 {
						rets := make([]string, len(fd.Results))
						for i, r := range fd.Results {
							rets[i] = qualifyRet(r.Type.String(), m.info.ShortName, ownStructs)
						}
						// 跨模組同名頂層函數（dial 定義於 quic/dns/proxy/tls）：
						// 裸名鍵按載入順序 last-wins，會令 dns.dial() 呼叫在
						// 解析期被標注成其他模組 dial 的返回型別。歧義名以
						// "module.fn" 為鍵註冊，裸名鍵不寫入（避免錯誤匹配）；
						// inferTypeFromCallExpr 對 module.fn() 呼叫優先查
						// "module.fn" 鍵。唯一名維持裸名鍵。
						if !fd.IsMethodDef && !strings.Contains(fd.Name, ".") && funcCount[fd.Name] > 1 {
							funcSigs[m.info.ShortName+"."+fd.Name] = rets
						} else {
							funcSigs[fd.Name] = rets
						}
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
					// 歧義結構體另以 module.name 為 key 註冊一份，使解析期
					// it 綁定標注 "tls.server-conn" 後仍能解析欄位/方法。
					if structCount[sd.Name] > 1 {
						structFields[m.info.ShortName+"."+sd.Name] = fields
					}
					// Map struct name → module short name for cross-module prefix validation
					if _, exists := structMod[sd.Name]; !exists {
						structMod[sd.Name] = m.info.ShortName
					}
				}
				// 收集單具體型別別名（name = known-type），使 newtype 語義
				// 在跨模組場景下也能生效（如 fs.no 定義 fd=i64，io.no 使用 fd）
				if ta, ok := stmt.(*parser.TypeAlias); ok && ta.Type != nil && ta.Union == nil {
					if _, ok := ta.Type.(*parser.FunctionType); !ok {
						if _, exists := aliases[ta.Name]; !exists {
							aliases[ta.Name] = ta.Type.String()
						}
					}
				}
			}
		}

		stdSigsCache = funcSigs
		stdFieldsCache = structFields
		stdAliasesCache = aliases
		stdStructModCache = structMod
	})
	return stdSigsCache, stdFieldsCache
}
func CollectStdConcreteAliases() map[string]string {
	CollectStdModuleSignatures() // 觸發 sync.Once 填充快取
	return stdAliasesCache
}
func CollectStdStructModules() map[string]string {
	CollectStdModuleSignatures() // 觸發 sync.Once 填充快取
	return stdStructModCache
}

// StdProgramForContent 返回與內容哈希相對應、在 CollectStdModuleSignatures
// PASS1 已解析的 std 模組 Program；未命中（非 std 模組或內容與 embed 不一致）
// 返回 nil。僅在 NOLANG_REUSE_STD_AST 開關開啟時由 build 端查詢，用於
// no vet src/std 跳過磁盤文件重新 parse。
func StdProgramForContent(contentKey string) *parser.Program {
	return stdProgramsCache[contentKey]
}
func extractBaseTypeName(t parser.Type) string {
	if t == nil {
		return ""
	}
	switch tt := t.(type) {
	case *parser.NamedType:
		return tt.Value
	case *parser.NullableType:
		return extractBaseTypeName(tt.Type)
	case *parser.PointerType:
		return extractBaseTypeName(tt.Type)
	case *parser.ArrayType:
		return extractBaseTypeName(tt.Elem)
	case *parser.SliceType:
		return extractBaseTypeName(tt.Elem)
	}
	return ""
}
func isInferredType(t parser.Type) bool {
	if t == nil {
		return false
	}
	switch tt := t.(type) {
	case *parser.NamedType:
		return tt.IsInferred
	case *parser.NullableType:
		return tt.IsInferred || isInferredType(tt.Type)
	case *parser.PointerType:
		return isInferredType(tt.Type)
	case *parser.ArrayType:
		return tt.IsInferred || isInferredType(tt.Elem)
	case *parser.SliceType:
		return tt.IsInferred || isInferredType(tt.Elem)
	}
	return false
}
func isBuiltinType(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "u128",
		"f32", "f64",
		"byte", "bool", "str":
		return true
	}
	return false
}
func ValidateCrossModuleTypeRefs(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	// 1. Build the struct-name → module-short-name map from std modules.
	structMod := CollectStdStructModules()
	if len(structMod) == 0 {
		return results
	}

	// 2. Collect locally defined type names (structs + type aliases).
	localTypes := make(map[string]bool)
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			localTypes[sd.Name] = true
		}
		if ta, ok := stmt.(*parser.TypeAlias); ok {
			localTypes[ta.Name] = true
		}
	}
	// Also include std concrete aliases (e.g. "fd") — these are intentionally
	// shared without module prefix.
	for k := range CollectStdConcreteAliases() {
		localTypes[k] = true
	}

	// 3. Helper: check a single Type and emit a result if it's a cross-module
	//    struct used without prefix.
	checkType := func(t parser.Type, fallbackLine, fallbackCol int) {
		baseName := extractBaseTypeName(t)
		if baseName == "" {
			return
		}
		// Already has a module prefix (contains a dot) — correct.
		if strings.Contains(baseName, ".") {
			return
		}
		// Builtin primitive type — no prefix needed.
		if isBuiltinType(baseName) {
			return
		}
		// Locally defined type — no prefix needed.
		if localTypes[baseName] {
			return
		}
		// Check if this type is a struct from another std module.
		modName, isCrossModule := structMod[baseName]
		if !isCrossModule {
			return
		}
		// Report error: should use module.type
		line := fallbackLine
		col := fallbackCol
		if t != nil {
			pos := t.Pos()
			if pos.Line > 0 {
				line = pos.Line
				col = pos.Column
			}
		}
		results = append(results, ValidateResult{
			Line:    line,
			Column:  col,
			Message: fmt.Sprintf("type '%s' not found; did you mean '%s.%s'?", baseName, modName, baseName),
		})
	}

	// 4. Walk all top-level statements.
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.StructDefinition:
			for _, f := range s.Fields {
				if f.Type != nil {
					checkType(f.Type, f.Token.Line, f.Token.Column)
				}
			}
		case *parser.LetStatement:
			if s.Type != nil && !isInferredType(s.Type) {
				checkType(s.Type, s.Token.Line, s.Token.Column)
			}
		case *parser.FunctionDefinition:
			for _, p := range s.Parameters {
				if p.Type != nil {
					checkType(p.Type, p.Token.Line, p.Token.Column)
				}
			}
			for _, r := range s.Results {
				if r.Type != nil {
					checkType(r.Type, r.Token.Line, r.Token.Column)
				}
			}
			// Also check local variable declarations inside function bodies.
			// Skip inferred types — they are auto-derived by the compiler
			// from function calls, not written by the user, so flagging them
			// for missing module prefix would be a false positive.
			if s.Body != nil {
				for _, bodyStmt := range s.Body.Statements {
					if ls, ok := bodyStmt.(*parser.LetStatement); ok && ls.Type != nil && !isInferredType(ls.Type) {
						checkType(ls.Type, ls.Token.Line, ls.Token.Column)
					}
				}
			}
		case *parser.ExternStatement:
			for _, p := range s.Parameters {
				if p.Type != nil {
					checkType(p.Type, p.Token.Line, p.Token.Column)
				}
			}
			for _, r := range s.Results {
				if r.Type != nil {
					checkType(r.Type, r.Token.Line, r.Token.Column)
				}
			}
		}
	}

	return results
}
func GetStdModuleShortNames() []string {
	infos := knownStdModules()
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.ShortName
	}
	return names
}
func GetStdModuleFullPaths() []string {
	infos := knownStdModules()
	paths := make([]string, len(infos))
	for i, info := range infos {
		paths[i] = info.FullPath
	}
	return paths
}
func resolveModuleCalls(program *parser.Program, importedModules []string, prefixedFns map[string]bool) {
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
			if !fd.IsMethodDef {
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
		resolveModuleCallsInStmt(stmt, modSet, moduleFns, moduleConsts, prefixedFns)
	}
}
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
func resolveModuleCallsInStmt(stmt parser.Statement, modSet map[string]bool, moduleFns map[string]bool, moduleConsts map[string]bool, prefixedFns map[string]bool) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveModuleCallsInExpr(s.Expression, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveModuleCallsInExpr(s.Condition, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if s.Init != nil {
			resolveModuleCallsInStmt(s.Init, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if s.Update != nil {
			resolveModuleCallsInStmt(s.Update, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
	}
}
func resolveModuleCallsInExpr(expr parser.Expression, modSet map[string]bool, moduleFns map[string]bool, moduleConsts map[string]bool, prefixedFns map[string]bool) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// For curried calls (e.g. `mod.fn(args)(out1, out2)`), the outer
		// CallExpression's Function is itself a CallExpression. Recurse into
		// it first so the inner module-qualified name gets resolved.
		if _, isCall := e.Function.(*parser.CallExpression); isCall {
			e.Function = resolveModuleCallsInExpr(e.Function, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		// Check if this is a module.fn() call (single or multi-level).
		// Only rewrite when the function property is a known module-level function
		// and the receiver chain matches a known module ShortName.
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			modPath, fnName := extractModulePathAndFunc(dot)
			if modPath != "" && modSet[modPath] {
				// 模組短名：多層路徑（hash/sha256）取最後一段
				short := modPath
				if idx := strings.LastIndex(short, "/"); idx >= 0 {
					short = short[idx+1:]
				}
				if full := short + "." + fnName; prefixedFns[full] {
					// 衝突函數已改名為 module.fn：改寫為扁平帶點 Identifier
					// （與方法呼叫同通道），保持與定義名精確對齊。
					e.Function = &parser.Identifier{
						Token: lexer.Token{Type: lexer.IDENT, Literal: full},
						Value: full,
					}
				} else if moduleFns[fnName] {
					// Rewrite to direct function call
					e.Function = &parser.Identifier{
						Token: lexer.Token{Type: lexer.IDENT, Literal: fnName},
						Value: fnName,
					}
				}
			}
		}
		// Recurse into arguments
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveModuleCallsInExpr(arg, modSet, moduleFns, moduleConsts, prefixedFns)
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
		e.Receiver = resolveModuleCallsInExpr(e.Receiver, modSet, moduleFns, moduleConsts, prefixedFns)
		return e

	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Consequence != nil {
			e.Consequence = resolveModuleCallsInExpr(e.Consequence, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Alternative != nil {
			e.Alternative = resolveModuleCallsInExpr(e.Alternative, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Consequence != nil {
			for _, bodyStmt := range e.Consequence.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
		if e.Alternative != nil {
			for _, bodyStmt := range e.Alternative.Statements {
				resolveModuleCallsInStmt(bodyStmt, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
		return e

	case *parser.GroupedExpression:
		if e.Expression != nil {
			e.Expression = resolveModuleCallsInExpr(e.Expression, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Index != nil {
			e.Index = resolveModuleCallsInExpr(e.Index, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.SliceExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				e.Range.Start = resolveModuleCallsInExpr(e.Range.Start, modSet, moduleFns, moduleConsts, prefixedFns)
			}
			if e.Range.End != nil {
				e.Range.End = resolveModuleCallsInExpr(e.Range.End, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
		return e

	case *parser.AssignExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Value != nil {
			e.Value = resolveModuleCallsInExpr(e.Value, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	default:
		return e
	}
}
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
			if fd.IsMethodDef {
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
		// typeOwner 的 key 為 "module.name"，value 為 bareName。遍歷查找
		// value == typeName 的所有鍵，對每個候選 "module.name" 檢查方法是否存在。
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				typeName := recv.Value
				if !strings.Contains(typeName, ".") {
					for k, v := range typeOwner {
						if v != typeName {
							continue
						}
						fullName := k + "." + dot.Property
						if definedMethods[fullName] {
							e.Function = &parser.Identifier{
								Token: lexer.Token{Type: lexer.IDENT, Literal: fullName},
								Value: fullName,
							}
							break
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
func collectConcreteTypeAliases(program *parser.Program) map[string]string {
	result := make(map[string]string)
	for _, stmt := range program.Statements {
		ta, ok := stmt.(*parser.TypeAlias)
		if !ok {
			continue
		}
		// Skip union aliases (ta.Union != nil) and function-type aliases
		// (ta.Type is *parser.FunctionType, handled separately as fnTypeAliases).
		if ta.Type == nil || ta.Union != nil {
			continue
		}
		if _, ok := ta.Type.(*parser.FunctionType); ok {
			continue
		}
		result[ta.Name] = ta.Type.String()
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
							// Don't rewrite builtin methods on slice/array types
							// (e.g. []byte.push, []i64.len, [N]byte.zero).
							// Builtins are handled by the codegen via DotExpression
							// dispatch + ForwardFunc. Rewriting them to
							// []T.method(field, ...) would create undefined function calls.
							isBuiltinMethod := false
							if strings.HasPrefix(fieldType, "[]") {
								if builtin.FindBuiltinMethod("vec."+dot.Property) != nil ||
									builtin.FindBuiltinMethod(dot.Property) != nil ||
									builtin.FindBuiltinMethod(fieldType+"."+dot.Property) != nil {
									isBuiltinMethod = true
								}
							} else if strings.HasPrefix(fieldType, "[") {
								if builtin.FindBuiltinMethod("arr."+dot.Property) != nil ||
									builtin.FindBuiltinMethod(fieldType+"."+dot.Property) != nil {
									isBuiltinMethod = true
								}
							}
							if !isBuiltinMethod {
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
func matchesTargetPlatform(platformKeys []string, goos, goarch string) bool {
	if len(platformKeys) == 0 {
		return true
	}
	if goos == "" || goarch == "" {
		return true // 未設定目標平台，接受所有（向後相容）
	}
	targetKey := pkg.PlatformKeyFor(goos, goarch)
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
func funcSigFirstReturnType(sig *funcSig) string {
	if sig == nil || len(sig.ResultTypes) == 0 {
		return ""
	}
	return sig.ResultTypes[0].Type
}
type funcSig struct {
	ParamTypes  []paramInfo
	ResultTypes []paramInfo
}
var validationStructFields map[string]map[string]string

// validationMu protects validationStructFields and validationConcreteTypeAliases
// from concurrent writes during parallel vet/build. ValidateTypes writes these
// globals; ValidateFuncArgs reads them. Without the mutex, parallel goroutines
// racing on the map writes cause "concurrent map writes" fatal panics.
var validationMu sync.Mutex
func isValidationIntType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128":
		return true
	}
	return false
}
type paramInfo struct {
	Name       string
	Type       string
	HasDefault bool // 參數是否有默認值
}
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
	case "i128", "u128":
		return 128
	}
	return 0
}
func findPackageRootFromFile(filePath string) string {
	dir := filepath.Dir(filePath)
	for {
		cfgFile := filepath.Join(dir, "package.jsonc")
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
