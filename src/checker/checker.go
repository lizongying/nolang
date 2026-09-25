package checker

import (
	"fmt"
	fs "io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// identIndexReadOnLine 報告該行是否含「直接變數基底」索引讀取 `base[i]`：base 結尾為
// 識別符 / `)` / `]`，且 `[` 之後不是範圍 `[0..`、不是 `[?]`、也不是空括號
// （切片型別 `[]T`）。DotExpression 基底 `.[i]` / `.field[i]` 因 `[` 前為 `.`
// 而不匹配，故不被當作可處理的直接索引讀取。
//
// 注意：使用手動掃描而非 regexp，因 Go 的 RE2 引擎不支援 `(?!...)` 負向先行斷言。
func identIndexReadOnLine(s string) bool {
	isBase := func(c byte) bool {
		return c == ')' || c == ']' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_'
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		if i == 0 || !isBase(s[i-1]) {
			continue
		}
		after := s[i+1:]
		if len(after) == 0 || after[0] == ']' { // []T 切片型別
			continue
		}
		if after[0] == '?' { // [?]
			continue
		}
		// 範圍 [0..n)：base 後緊跟數字且隨後出現 ".."
		if after[0] >= '0' && after[0] <= '9' {
			if end := strings.IndexByte(after, ']'); end >= 2 && after[1] == '.' && after[2] == '.' {
				continue
			}
		}
		if strings.IndexByte(after, ']') >= 0 {
			return true
		}
	}
	return false
}

// Migrated from build/transpiler.go: semantic-checker subsystem
// (validators + module/type resolution helpers).

var validationFuncTypes map[string]string

// validationStructNames holds the set of struct type names defined in the
// current program. Used by ValidateUndefinedVars to distinguish struct type
// names (e.g. `s` in `s { x i64 }`) from struct instances (e.g. `s0 = s {}`).
// When a DotExpression like `s.x = 7` appears, if the receiver `s` is a struct
// type name but not a defined variable (instance), it's an error.
var validationStructNames map[string]bool

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
		// Implicit receiver: `.len` parses to DotExpression{Receiver: self}
		// (parser/expr.go), and an explicit `self.len` reaches here as the
		// bare Identifier "self". `self` is never a local, so without this
		// case its type resolves to "" and every self-field access looks like
		// an unknown receiver. The two DotExpression branches below already
		// special-case `self`; this makes the base case agree with them.
		if e.Value == "self" && selfType != "" {
			return selfType
		}
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
			} else if rt := inferExprType(e.Receiver, varTypes, funcTypes, selfType); rt != "" {
				// 巢狀路徑：`p.nodes[i].str-val` 的 Receiver 是 IndexExpression，
				// 不是純識別符。遞迴推斷它的型別（[4]json-value 的元素 → json-value），
				// 才能查到 `str-val` 這個欄位。沒有這一步，任何「陣列元素再取欄位」
				// 的路徑都推不出型別。
				// （舊註解說這會讓 checkReadOnlyLenAssign 在巢狀路徑失效、需要補洞 ——
				//  那是錯的：欄位的 `X.len = n` 本來就是支援的增長操作，見
				//  checkReadOnlyLenAssign 上的說明與 tests/len-assign-grow.no。）
				typeName = rt
			}
			if typeName != "" {
				// Unwrap optional `?T` to its inner struct type so field access
				// on an option value (`opt.field`) resolves against T's fields.
				// Handles both Nolang-style `?test-conn` and LLVM-style `%test-conn`.
				unwrapped := typeName
				if strings.HasPrefix(unwrapped, "?") {
					unwrapped = unwrapped[1:]
				}
				unwrapped = strings.TrimPrefix(unwrapped, "%")
				if fields, ok := validationStructFields[unwrapped]; ok {
					if fieldType, ok := fields[e.Property]; ok {
						return fieldType
					}
				}
			}
		}
		// Array/slice/str .len and .cap property access returns i64.
		// str .len-bytes is the byte-length property (compiler builtin, mirrors old .len).
		if recv, ok := e.Receiver.(*parser.Identifier); ok {
			t := ""
			if recv.Value == "self" {
				t = selfType
			} else if tt, exists := varTypes[recv.Value]; exists {
				t = tt
			}
			if t != "" && (strings.HasPrefix(t, "[]") || (strings.HasPrefix(t, "[") && strings.Contains(t, "]")) || t == "str") {
				switch e.Property {
				case "len", "cap", "len-bytes":
					return "i64"
				}
			}
		}
		return ""
	case *parser.IndexExpression:
		// Array/slice element access: str 下标返回 char（2026-09-06）。
		// txt 下标自 code-point 化後與 str 一致：`t[i]` 回傳碼點 char（越界 -1），
		// `t[i] = v` 亦以碼點寫入（重編碼＋搬移，255 位元組上限）。
		if e.Left != nil {
			lt := inferExprType(e.Left, varTypes, funcTypes, selfType)
			if lt == "str" || lt == "txt" {
				return "char"
			}
			// 陣列 / 切片元素型別：`[4]json-value` → `json-value`、`[]i64` → `i64`、
			// `[2][3]i64` → `[3]i64`（只剝最外層）。
			// 之前這裡一律回傳 ""，所以 `p.nodes[i].str-val` 這種「陣列元素再取欄位」
			// 的巢狀路徑完全推不出型別，任何依賴推斷的檢查（唯讀容器欄位寫入等）
			// 在巢狀路徑上都無從下手。
			if strings.HasPrefix(lt, "[") {
				if elem := extractArrayElemType(lt); elem != "" {
					return elem
				}
			}
		}
		// 其他下标无法在此可靠推断元素类型
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
	case *parser.IfExpression:
		// `x = cond -> v` (short-circuit pipeline) and `x = subject: { arms }`
		// (a match used as a value) both lower to an if-chain whose VALUE is an
		// arm's trailing expression. Falling through to the `default` returned
		// "i64" for every one of them, so `s = cond -> 'new'` failed with
		// "cannot assign i64 value to str variable" — and so did the
		// pre-existing match-as-value form. The else-chain is itself an
		// IfExpression, so recursing into the alternative walks to the last arm.
		if t := blockTrailingExprType(e.Consequence, varTypes, funcTypes, selfType); t != "" {
			return t
		}
		return blockTrailingExprType(e.Alternative, varTypes, funcTypes, selfType)
	default:
		return "i64"
	}
}

// blockTrailingExprType reports the type of the value a block yields when it is
// used as an expression: its last statement, when that statement is a bare
// expression (MIR captures exactly that; see captureArmValue). A block ending in
// an assignment or a call-with-no-value yields nothing, hence "".
func blockTrailingExprType(b *parser.BlockStatement, varTypes map[string]string, funcTypes map[string]string, selfType string) string {
	if b == nil || len(b.Statements) == 0 {
		return ""
	}
	last := b.Statements[len(b.Statements)-1]
	es, ok := last.(*parser.ExpressionStatement)
	if !ok {
		return ""
	}
	return inferExprType(es.Expression, varTypes, funcTypes, selfType)
}

type ValidateResult struct {
	Line      int
	Column    int
	EndColumn int
	File      string // 来源文件（节点级，用于合併模式下跳过 std）；空则由 RunAllLints 回退归因
	Message   string
	TraceID   string
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
				TraceID: "6qca9xu4",
				Line:    line,
				Column:  col,
				Message: "embed: cannot combine with explicit value",
			})
		}

		// 規則 1：必須是 []byte / [N]byte / fs.embed 類型
		if ls.Type == nil {
			results = append(results, ValidateResult{
				TraceID: "238u1xr7",
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
					TraceID: "4y6hz2la",
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
					TraceID: "14ki3f1w",
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
						TraceID: "nwtpvei9",
						Line:    line,
						Column:  col,
						Message: fmt.Sprintf("embed: fs.embed requires a directory, got file: %s (resolved: %s)", embedPath, resolvedPath),
					})
				}
				if !isFsEmbedType && info.IsDir() {
					results = append(results, ValidateResult{
						TraceID: "vr0eh0yz",
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

// declaredResults returns the declared result parameters of a function/method
// definition, with the implicit `self` receiver stripped for method
// definitions. It is a thin alias for parser.DeclaredResults, which owns the
// rationale and the method-semantics contract.
//
// Within this package every read of `Results[0]` as "the return type" must go
// through it, otherwise a method gets misread as returning its receiver type
// (e.g. `sym = ht.decode-symbol(br)` -> "cannot assign bz-huffman value to
// i64 variable 'sym'").
func declaredResults(fd *parser.FunctionDefinition) []*parser.Parameter {
	return parser.DeclaredResults(fd)
}

// ValidateDeprecatedLen reports deprecated bare `.len` property reads on
// str / array / slice / vec receivers. The property form is being phased out
// in favor of the `.len()` method (containers) and `.len-bytes()` / `.len()`
// (str). vec.no and byte.no are exempt because they implement the container
// types and legitimately read the backing `len` field.
//
// The results are collected like other ValidateTypes results: `no vet`
// surfaces them as diagnostics (non-fatal), while `no build` treats them as
// errors. This lets the diagnostic drive the migration and, once the codebase
// is fully migrated, enforce the deprecation.
func ValidateDeprecatedLen(program *parser.Program) []ValidateResult {
	var results []ValidateResult

	funcTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			// declaredResults drops the implicit `self` receiver of methods, so
			// rs[0] is the real return type (absent for void methods).
			if rs := declaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				funcTypes[fd.Name] = rs[0].Type.String()
			}
		}
	}

	for _, stmt := range program.Statements {
		// Skip monomorphized instances (generated, not source).
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if strings.Contains(fd.Name, "__") {
				continue
			}
		}
		// Exempt the builtin type implementations: they read the backing `len`
		// field. str.no's `str.len-bytes` reads it for byte length; vec.no /
		// byte.no's `[...].len` / `[]byte.len` read it for element count.
		// Match by full path under src/std, NOT just basename — otherwise test
		// files that reuse the impl names (e.g. test/std/str.no,
		// test/std/vec.no) would be wrongly exempted and left un-migrated.
		srcFile := parser.GetSourceFile(stmt)
		if strings.Contains(srcFile, "src/std/") {
			base := srcFile
			if i := strings.LastIndex(base, "/"); i >= 0 {
				base = base[i+1:]
			}
			if base == "vec.no" || base == "byte.no" || base == "str.no" {
				continue
			}
		}

		var selfType string
		varTypes := make(map[string]string)
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			// The implicit receiver sits at Results[0] (parser/decl.go); its
			// type is the struct this function is a method of. Needed so that
			// `self` / `.field` receivers can be resolved below.
			if fd.IsMethodDef && len(fd.Results) > 0 && fd.Results[0].Name == "self" && fd.Results[0].Type != nil {
				selfType = fd.Results[0].Type.String()
			}
			for _, p := range fd.Parameters {
				if p.Type != nil && p.Type.String() != "" {
					varTypes[p.Name] = p.Type.String()
				}
			}
			if fd.Body != nil {
				walkStmtForLen(fd.Body, varTypes, funcTypes, selfType, &results)
			}
			continue
		}
		walkStmtForLen(stmt, varTypes, funcTypes, selfType, &results)
	}
	return results
}

// lenKind classifies a receiver type for the `.len` deprecation.
//
//	"str"       → str / str-long  (use .len-bytes() / .len())
//	"container" → array / slice / vec (use .len())
//	"skip"      → not a builtin container/str (e.g. a struct with a len field)
//	""          → unknown (still reported; default to container migration)
func lenKind(t string) string {
	switch t {
	case "str", "str-long":
		return "str"
	}
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "[") || strings.HasPrefix(t, "vec") || strings.HasPrefix(t, "[]") {
		return "container"
	}
	return "skip"
}

// isStructLenField reports whether `t` names a user struct that declares a real
// `len` FIELD. A bare `recv.len` on such a struct is a legitimate field access,
// not the deprecated container/str `.len` property, and must not be flagged.
func isStructLenField(t string) bool {
	if t == "" {
		return false
	}
	unwrapped := strings.TrimPrefix(t, "?")
	unwrapped = strings.TrimPrefix(unwrapped, "%")
	if fields, ok := validationStructFields[unwrapped]; ok {
		if _, has := fields["len"]; has {
			return true
		}
	}
	return false
}

func walkStmtForLen(stmt parser.Statement, varTypes, funcTypes map[string]string, selfType string, results *[]ValidateResult) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Value != nil {
			if t := inferExprType(s.Value, varTypes, funcTypes, selfType); t != "" {
				if _, exists := varTypes[s.Name.Value]; !exists {
					varTypes[s.Name.Value] = t
				}
			}
			walkExprForLen(s.Value, varTypes, funcTypes, selfType, results)
		}
	case *parser.MultiAssignStatement:
		// Targets are identifiers/index expressions (assignment, not a read);
		// only the RHS is a value read.
		walkExprForLen(s.Value, varTypes, funcTypes, selfType, results)
	case *parser.UnwrapAssignStatement:
		walkExprForLen(s.Value, varTypes, funcTypes, selfType, results)
	case *parser.ReturnStatement:
		walkExprForLen(s.ReturnValue, varTypes, funcTypes, selfType, results)
	case *parser.ExpressionStatement:
		walkExprForLen(s.Expression, varTypes, funcTypes, selfType, results)
	case *parser.BlockStatement:
		for _, sub := range s.Statements {
			walkStmtForLen(sub, varTypes, funcTypes, selfType, results)
		}
	case *parser.ForStatement:
		walkStmtForLen(s.Init, varTypes, funcTypes, selfType, results)
		walkExprForLen(s.Condition, varTypes, funcTypes, selfType, results)
		walkStmtForLen(s.Update, varTypes, funcTypes, selfType, results)
		if s.IterRange != nil {
			walkExprForLen(s.IterRange.Range, varTypes, funcTypes, selfType, results)
			walkExprForLen(s.IterRange.RangeExpr, varTypes, funcTypes, selfType, results)
		}
		if s.Body != nil {
			walkStmtForLen(s.Body, varTypes, funcTypes, selfType, results)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			walkStmtForLen(s.Body, varTypes, funcTypes, selfType, results)
		}
	default:
		// Other statement kinds carry no nested `.len` reads we care about.
	}
}

func walkBlockForLen(b *parser.BlockStatement, varTypes, funcTypes map[string]string, selfType string, results *[]ValidateResult) {
	if b == nil {
		return
	}
	for _, sub := range b.Statements {
		walkStmtForLen(sub, varTypes, funcTypes, selfType, results)
	}
}

func walkExprForLen(expr parser.Expression, varTypes, funcTypes map[string]string, selfType string, results *[]ValidateResult) {
	if expr == nil {
		return
	}
	// A nil *parser.X stored inside an Expression interface is a *typed-nil*
	// value: it evades the `expr == nil` check above and still matches a case
	// in the type switch below, where dereferencing e.Field then panics
	// (e.g. a RangeExpression with a missing bound holds a typed-nil child).
	// Guard against it so `no vet <dir>` / `no build` never crash on such ASTs.
	if rv := reflect.ValueOf(expr); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	switch e := expr.(type) {
	case *parser.DotExpression:
		if e.Property == "len" {
			recvType := inferExprType(e.Receiver, varTypes, funcTypes, selfType)
			// Flag the deprecated `.len` *property* read on every builtin
			// container / str receiver, PLUS unknown-typed receivers (we
			// cannot always statically resolve `awy`/function-return types,
			// but a bare `.len` there is still the deprecated property).
			// The only legitimate bare `.len` is a real field access on a
			// user struct that actually declares a `len` field — skip those.
			if !isStructLenField(recvType) {
				kind := lenKind(recvType)
				if kind == "" {
					// Unknown receiver: default to the container form
					// (recv.len() is valid for both containers and str
					// codepoint count; str.byte-length callers are detected
					// when the type is statically known as str below).
					kind = "container"
				}
				msg := ""
				switch kind {
				case "str":
					msg = "deprecated: str.len property is removed; use s.len-bytes() for byte length or s.len() for codepoint count"
				default:
					msg = "deprecated: recv.len property is removed; use recv.len() for element count"
				}
				*results = append(*results, ValidateResult{
					Line:    e.Token.Line,
					Column:  e.Token.Column,
					Message: msg,
					TraceID: "len-depr",
				})
			}
		}
		// Always recurse into receiver (e.g. a.b.len → walk a.b).
		walkExprForLen(e.Receiver, varTypes, funcTypes, selfType, results)
	case *parser.CallExpression:
		// A method call `recv.len()` must NOT be flagged: only walk the
		// receiver. A bare `.len` passed as an argument IS a read and is
		// reached through the argument walk below.
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			walkExprForLen(dot.Receiver, varTypes, funcTypes, selfType, results)
		} else {
			walkExprForLen(e.Function, varTypes, funcTypes, selfType, results)
		}
		for _, a := range e.Arguments {
			walkExprForLen(a, varTypes, funcTypes, selfType, results)
		}
		for _, a := range e.GenericArgs {
			walkExprForLen(a, varTypes, funcTypes, selfType, results)
		}
	case *parser.InfixExpression:
		walkExprForLen(e.Left, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.Right, varTypes, funcTypes, selfType, results)
	case *parser.PrefixExpression:
		walkExprForLen(e.Right, varTypes, funcTypes, selfType, results)
	case *parser.IndexExpression:
		walkExprForLen(e.Left, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.Index, varTypes, funcTypes, selfType, results)
	case *parser.SliceExpression:
		walkExprForLen(e.Left, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.Range, varTypes, funcTypes, selfType, results)
	case *parser.RangeExpression:
		walkExprForLen(e.Start, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.End, varTypes, funcTypes, selfType, results)
	case *parser.ConditionalExpression:
		walkExprForLen(e.Condition, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.Consequence, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.Alternative, varTypes, funcTypes, selfType, results)
	case *parser.CastExpression:
		walkExprForLen(e.Expr, varTypes, funcTypes, selfType, results)
	case *parser.RunExpression:
		walkExprForLen(e.Call, varTypes, funcTypes, selfType, results)
	case *parser.AwaitExpression:
		walkExprForLen(e.Right, varTypes, funcTypes, selfType, results)
	case *parser.ArrayLiteral:
		for _, el := range e.Elements {
			walkExprForLen(el, varTypes, funcTypes, selfType, results)
		}
	case *parser.AssignExpression:
		// Left is a DotExpression write target; only walk the value.
		walkExprForLen(e.Value, varTypes, funcTypes, selfType, results)
	case *parser.IfExpression:
		walkExprForLen(e.Condition, varTypes, funcTypes, selfType, results)
		walkExprForLen(e.MatchedExpr, varTypes, funcTypes, selfType, results)
		walkBlockForLen(e.Consequence, varTypes, funcTypes, selfType, results)
		walkBlockForLen(e.Alternative, varTypes, funcTypes, selfType, results)
	default:
		// literals, identifiers, function literals, etc.: nothing to recurse
	}
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
			// declaredResults drops the implicit `self` receiver of methods, so
			// rs[0] is the real return type (absent for void methods).
			if rs := declaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				funcTypes[fd.Name] = rs[0].Type.String()
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
					TraceID: "uvws00l4",
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
	for i, stmt := range program.Statements {
		// 判斷是否為 struct 方法
		selfType := ""
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if len(fd.Results) > 0 && fd.Results[0].Name == "self" {
				selfType = fd.Results[0].Type.String()
			}
			// 跳過單態化生成的函式（函式名含 '__'），因為這些函式
			// 由編譯器自動生成，其型別檢查應在泛型模板層面完成。
			// 單態化後的函式體中 i64 字面量賦值給特化類型（如 f64、u8）
			// 是合法的，代碼生成器會正確處理轉換。
			if strings.Contains(fd.Name, "__") {
				continue
			}
			// 跳過泛型模板函式：有 GenericParams 或函式名以 '[' 開頭
			// （如 []t.to-str），這些函式體中使用了泛型型別參數（如 t），
			// 在特化前無法精確檢查型別相容性（如 t 傳給期望 i64 的參數）。
			if len(fd.GenericParams) > 0 || strings.HasPrefix(fd.Name, "[") {
				continue
			}
		}
		// 使用預填的頂層變數型別，避免跨語句型別推斷失敗
		localVarTypes := make(map[string]string)
		for k, v := range topLevelVarTypes {
			localVarTypes[k] = v
		}
		errs := validateStmtTypes(stmt, funcNames, funcTypes, selfType, localVarTypes, i == len(program.Statements)-1, program.Sem, "")
		results = append(results, errs...)
	}

	// Deprecated `.len` property pass (drives + enforces the .len() migration).
	results = append(results, ValidateDeprecatedLen(program)...)

	// View (`&T`) placement rules: methods' result lists only, borrowing the
	// receiver's own type.
	results = append(results, ValidateViewTypes(program)...)

	// Field-level tag annotations (`#{inline}`): placement and acyclicity.
	results = append(results, ValidateFieldTags(program)...)

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
				TraceID: "2trc49mk",
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
	case *parser.ViewType:
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
		"bool", "byte", "char", "str", "txt":
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

// isAllCapsConst returns true if the name looks like an ALL-CAPS constant
// (e.g. SQLITE-OK, MYSQL-RECV-BUF, CLIENT-PROTOCOL-41). Used to skip
// undefined-variable checks for constants from externally imported modules
// that were filtered out by lib.no during module merging.
func isAllCapsConst(name string) bool {
	if name == "" {
		return false
	}
	hasUpper := false
	for _, ch := range name {
		switch {
		case ch >= 'A' && ch <= 'Z':
			hasUpper = true
		case ch >= '0' && ch <= '9':
			// digits allowed
		case ch == '-':
			// hyphens allowed
		default:
			return false
		}
	}
	return hasUpper
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
		// Skip naming-convention checks for functions explicitly marked
		// (e.g. compiler-generated monomorphized or overload-mangled functions
		// whose names contain underscores). The IsSkipNamingCheck flag is set
		// by compiler passes; currently no pass sets it, reserving the hook
		// for future stdlib-wide filtering.
		if s.IsSkipNamingCheck || strings.Contains(s.Name, "_") {
			if s.Body != nil {
				for _, bStmt := range s.Body.Statements {
					results = append(results, checkNaming(bStmt, globalVars)...)
				}
			}
			return results
		}
		// For methods like "[]t.sort-desc", only validate the method name part (after the last '.')
		nameToCheck := s.Name
		if lastDot := strings.LastIndex(s.Name, "."); lastDot >= 0 {
			nameToCheck = s.Name[lastDot+1:]
		}
		if !isValidVarName(nameToCheck) {
			results = append(results, ValidateResult{
				TraceID: "x6swm2kk",
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
		// 跳過編譯器合成變數（雙底線前綴 `__` 慣例，如 index-out 降級產生的
		// `__idx_out_L_C`、unwrap 降級的 `__unwrap_N`、回填位圖 `__ret_init_bitmap`）。
		// 這些名稱含底線/數字會違反命名規範，但屬內部產物，不應對使用者報警。
		if s.Name != nil && strings.HasPrefix(s.Name.Value, "__") {
			return results
		}
		// 註：點號名字（`recv.field`）不可能出現在這裡——產生它的點號欄位型別
		// 宣告已被 parser 移除（見 parser/stmt.go 的相關註解），而模組前綴改名
		// 只作用於頂層 LetStatement，本函式已在上方跳過。故不需要為點號名字
		// 開任何豁免（曾誤以 IsFieldDecl 旗標處理，該旗標已隨語法一併移除）。
		if s.Name != nil && !isValidVarName(s.Name.Value) {
			results = append(results, ValidateResult{
				TraceID: "2dhoris2",
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
			TraceID: "y7964ox1",
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
func ValidateUnusedVars(program *parser.Program, mainVarNames map[string]bool) []ValidateResult {
	var results []ValidateResult

	// Collect top-level LetStatement names
	topLevelVars := make(map[string]struct{ line, column int })
	var varOrder []string

	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Name != nil && ls.Name.Value != "_" {
				// When mainVarNames is provided (no vet path with merged program),
				// skip variables that belong to imported modules — only check
				// variables defined in the main source file.
				if mainVarNames != nil && !mainVarNames[ls.Name.Value] {
					continue
				}
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
				TraceID:   "6kryrbsq",
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
		// Collect interface method names as "InterfaceName.methodName"
		// (e.g. "db.query", "rows.scan-int") so that vet doesn't flag
		// calls to interface methods as undefined after module merging
		// rewrites them to "module.InterfaceName.methodName".
		if id, ok := stmt.(*parser.InterfaceDefinition); ok {
			for _, m := range id.Methods {
				definedVars[id.Name+"."+m.Name] = true
			}
		}
		// MultiAssignStatement targets (e.g. `a, b = func()`) define
		// variables at the top level. Collect them so that after the
		// transpiler rewrites MultiAssign to nested-call syntax
		// (bar(a, b)), the targets are still recognized as defined.
		if mas, ok := stmt.(*parser.MultiAssignStatement); ok {
			for _, target := range mas.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					definedVars[ident.Value] = true
				}
			}
		}
	}
	// Also pick up variables registered in the semantic context's
	// DeclaredVars map. The transpiler registers MultiAssignStatement
	// targets here before converting them to nested-call syntax, so
	// the merged program (where MultiAssignStatement no longer exists)
	// still has the variable names available.
	if program.Sem != nil {
		for name := range program.Sem.DeclaredVars {
			definedVars[name] = true
		}
	}
	return definedVars
}
func ValidateUndefinedVars(program *parser.Program, rootDir string) []ValidateResult {
	validationMu.Lock()
	defer validationMu.Unlock()
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
	//
	//     Transitive imports are handled via a worklist: when a module is
	//     loaded, its own UseStatements are added to the worklist so that
	//     transitively imported modules are also processed. This ensures
	//     that entry files can use functions from transitively imported
	//     modules without explicitly importing them (bug08 fix).
	if rootDir != "" {
		pkg, _ := pkg.LoadPackage(rootDir)

		// collectModuleSymbols processes a single module's statements,
		// adding defined names.
		var collectModuleSymbols func(modProg *parser.Program)
		visitedModules := make(map[string]bool) // dedup by module path

		collectModuleSymbols = func(modProg *parser.Program) {
			if modProg == nil {
				return
			}
			for _, ms := range modProg.Statements {
				if es, ok := ms.(*parser.ExternStatement); ok && es.Name != nil {
					definedVars[es.Name.Value] = true
					funcNames[es.Name.Value] = true
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

		// collectRawModuleSymbols parses the raw module file directly
		// (bypassing lib.no export filtering) to collect ALL symbols
		// including unexported ones (e.g. _sqlite3-open, SQLITE-OK).
		// This is needed because the merged program includes full method
		// bodies that reference internal FFI functions and constants.
		collectRawModuleSymbols := func(filePath string) {
			if filePath == "" {
				return
			}
			rawProg := parseProgramFile(filePath)
			if rawProg == nil {
				return
			}
			for _, ms := range rawProg.Statements {
				if es, ok := ms.(*parser.ExternStatement); ok && es.Name != nil {
					definedVars[es.Name.Value] = true
					funcNames[es.Name.Value] = true
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

		// Build initial worklist from the entry program's UseStatements.
		var worklist []*parser.UseStatement
		for _, stmt := range program.Statements {
			use, ok := stmt.(*parser.UseStatement)
			if !ok {
				continue
			}
			worklist = append(worklist, use)
		}

		// Process the worklist, draining transitively.
		for len(worklist) > 0 {
			use := worklist[0]
			worklist = worklist[1:]

			// Dedup: skip modules we've already processed
			if visitedModules[use.Path] {
				continue
			}
			visitedModules[use.Path] = true

			modProg := resolveUseModule(use, pkg, rootDir)
			if modProg == nil {
				continue
			}

			// Collect this module's symbols (from filtered exports)
			collectModuleSymbols(modProg)

			// Also collect ALL symbols (including unexported) from the
			// raw module file, because the merged program includes full
			// method bodies that reference internal FFI functions and
			// constants not in the lib.no export list.
			rawFilePath := resolveModuleFilePath(use, pkg, rootDir)
			collectRawModuleSymbols(rawFilePath)

			// Collect nested UseStatements for transitive processing.
			for _, ms := range modProg.Statements {
				if nestedUse, ok := ms.(*parser.UseStatement); ok {
					if !visitedModules[nestedUse.Path] {
						worklist = append(worklist, nestedUse)
					}
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

	// Collect struct type names so we can distinguish struct type names
	// (e.g. `s` in `s { x i64 }`) from struct instances (e.g. `s0 = s {}`).
	// Struct type names are NOT added to definedVars: they are type names,
	// not variables. Assigning a field on a type name (e.g. `s.x = 7`) is
	// an error — the user must instantiate first (`s0 = s {}` then `s0.x = 7`).
	validationStructNames = make(map[string]bool)
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			validationStructNames[sd.Name] = true
		}
	}

	// 4. Walk statements and check for undefined references
	for _, stmt := range program.Statements {
		results = append(results, checkUndefinedVarsInStmt(stmt, definedVars, funcNames)...)
	}

	return results
}

// 註：此處原本有一個 methodDefNames()，以「被呼叫者的名字是否撞上某個方法
// 定義」反推「攤平後的首實參曾是接收者」。該做法已廢除，改走語義副表：
//
//   - 會漏：衝突改名的自由函式 `module.fn` 與某方法定義同名時，會被誤認成
//     方法呼叫（見 build/module_prefix.go 的命名空間共用問題）。
//   - 會誤：自由函式的首實參是按值傳入的，本就不該算「已賦值」。
//   - 根因：攤平的結果在 AST 上不可自證，只能靠名字字串猜。
//
// 現在改由攤平點（build/transpiler.go 的 resolveMethodCall）把
// 「這是一次被攤平的方法呼叫」寫進 parser.SemanticContext（parser.CalleeKind），
// 檢查側直接讀 `sem.CalleeOf(call) == parser.CalleeMethod`——事實顯式記錄，
// 不再依賴名字能否撞上某個定義。

func ValidateUninitOutputParams(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	// 預計算應跳過校驗的內建樁（含同名多載延續定義）。
	suppressed := builtinSuppressedDefs(program)
	// 語義副表：攤平後的方法呼叫（`Type.m(recv, ...)`）第一個實參是被改寫的
	// 接收者，需與 DotExpression 形式一視同仁（理由見上方註釋）。
	sem := program.Sem
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		// #{buildin=NAME} / #{intrinsic} 內建樁函式及其同名多載：真實實作位於
		// Go runtime，nolang 函式體不參與校驗，其 ?T 返回參數的讀取/賦值亦由
		// runtime 保證，故跳過檢查。
		if suppressed[fd] {
			continue
		}
		// Collect ?T output parameter names
		type nullableParam struct {
			name string
			line int
			col  int
		}
		var nullableParams []nullableParam
		// declaredResults 剔除方法定義的隱式 `self` 接收者，理由同
		// ValidateUnassignedReturns：接收者由呼叫方指標別名提供。
		for _, r := range declaredResults(fd) {
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
		collectAssignedNames(fd.Body.Statements, assigned, sem)
		// Collect all read variable names in the body
		read := make(map[string]bool)
		collectReadNames(fd.Body.Statements, read)
		// Check each ?T output param: read but not assigned → error
		for _, p := range nullableParams {
			if read[p.name] && !assigned[p.name] {
				results = append(results, ValidateResult{
					TraceID: "wdk3k728",
					Line:    p.line,
					Column:  p.col,
					Message: fmt.Sprintf("output parameter '%s' (?T) is read but never assigned in function body — uninitialized use of nullable output parameter", p.name),
				})
			}
		}
	}
	return results
}

// ValidateUnassignedReturns checks for named result parameters (output
// parameters) that are never explicitly assigned in the function body.
// The ret_init mechanism silently zero-fills such parameters, which can
// mask bugs where a function forgets to set a return value.  This check
// reports a warning (not an error) to make the oversight visible without
// blocking compilation, since zero-default returns are sometimes intentional.
func ValidateUnassignedReturns(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	// 預計算應跳過校驗的內建樁（含同名多載延續定義）。
	suppressed := builtinSuppressedDefs(program)
	// 語義副表，理由同 ValidateUninitOutputParams。
	sem := program.Sem
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		// #{intrinsic} / #{buildin=NAME} 註解標記的函式（及其同名多載延續定義）：
		// 命名返回參數由 codegen / 内建 / 出參引用在 nolang 源碼之外賦值，
		// 靜態檢查無法追蹤，故跳過「未賦值」檢查，避免對空體內建樁誤報
		// "result parameter 'X' is never assigned" (i3k422u3)。
		if suppressed[fd] {
			continue
		}
		// Collect named result parameters (non-nullable only; nullable
		// ones are already handled by ValidateUninitOutputParams).
		type retParam struct {
			name string
			typ  string
			line int
			col  int
		}
		var retParams []retParam
		// declaredResults 剔除方法定義的隱式 `self` 接收者：self 由呼叫方
		// 指標別名提供，函式體無須「賦值」，不應報未賦值。
		for _, r := range declaredResults(fd) {
			if r.Name == "" || r.Type == nil {
				continue
			}
			if _, ok := r.Type.(*parser.NullableType); ok {
				continue // nullable already checked by ValidateUninitOutputParams
			}
			retParams = append(retParams, retParam{
				name: r.Name,
				typ:  r.Type.String(),
				line: r.Token.Line,
				col:  r.Token.Column,
			})
		}
		if len(retParams) == 0 {
			continue
		}
		// Collect all directly-assigned variable names in the body
		assigned := make(map[string]bool)
		collectAssignedNames(fd.Body.Statements, assigned, sem)
		// Report any result parameter that is never assigned
		for _, p := range retParams {
			if !assigned[p.name] {
				results = append(results, ValidateResult{
					TraceID: "i3k422u3",
					Line:    p.line,
					Column:  p.col,
					Message: fmt.Sprintf("result parameter '%s' (%s) is never assigned in function body — will be zero-filled on return", p.name, p.typ),
				})
			}
		}
	}
	return results
}

// functionIsIntrinsic 回報函式是否被 #{intrinsic} 註解標記。
// 優先讀取 FunctionDefinition.Intrinsic 節點欄位（解析期由 attachAnnotations
// 填寫，與 OverflowMode 同機制）；若該欄位因單態化/HIR 重建遺失，再以
// program.Sem 註解副表作為權威來源兜底。
func functionIsIntrinsic(program *parser.Program, fd *parser.FunctionDefinition) bool {
	if fd.Intrinsic {
		return true
	}
	if program.Sem != nil {
		for _, e := range program.Sem.AnnotationsOf(fd) {
			if e.Key == "intrinsic" {
				return true
			}
		}
	}
	return false
}

// functionIsBuiltin 回報函式是否被 #{buildin=NAME} 註解標記（內建樁函式）。
// 優先讀取 FunctionDefinition.BuiltinStub 節點欄位（解析期由 attachAnnotations
// 填寫）；若該欄位因單態化/HIR 重建遺失，再以 program.Sem 註解副表兜底。
// 內建樁的真實實作位於 Go runtime，nolang 函式體不參與校驗與 codegen，故其
// 命名返回參數在 nolang 源碼中必然「未賦值」，相關返回值校驗應跳過。
func functionIsBuiltin(program *parser.Program, fd *parser.FunctionDefinition) bool {
	if fd.BuiltinStub {
		return true
	}
	if program.Sem != nil {
		for _, e := range program.Sem.AnnotationsOf(fd) {
			if e.Key == "buildin" {
				return true
			}
		}
	}
	return false
}

// builtinSuppressedDefs 預計算一組「應跳過返回值校驗」的函式定義指標集合。
// 包含兩類：
//  1. 自身被 #{buildin=...} / #{intrinsic} 註解標記的內建樁函式；
//  2. 與上述函式「同名」的其餘多載定義。nolang 以連續同名 `name = ...`
//     表達 arity 多載，註解（#{buildin=...} / #{intrinsic}）只掛在首個
//     定義上，其餘多載體同為空樁、命名返回參數同樣「未賦值」，故一併跳過
//     校驗，避免對內建多載（如 global.no 的 format 第二個多載
//     `format = (s str) (out str) { }`）誤報
//     "result parameter 'X' is never assigned" (i3k422u3)。
//
// 採用「同名群組」而非「緊鄰序列」判斷：no vet / lsp vet 在 std 重整或
// 多模組合併（merged program）後，多載陳述未必仍相鄰，但同名關係不變，
// 故以名稱聚類可穩定覆蓋整個多載群組。
//
// 重要：此集合僅用於校驗跳過，完全不影響 codegen。多載延續定義仍照常
// 編譯為一般函式（不會被標記為 builtin stub 而缺失符號定義），因此不會
// 引入「@format() 未定義」之類的 codegen 回歸。
func builtinSuppressedDefs(program *parser.Program) map[*parser.FunctionDefinition]bool {
	// 第一遍：收集所有「自身為 builtin/intrinsic」的函式名稱。
	// 來源有二：(a) 直接標記的函式定義（未經 strip 的場景，如單元測試）；
	// (b) program.BuiltinFuncNames，由 stripBuiltinStubs 在剝離內建樁時記錄，
	// 覆蓋「樁函式已被剝離、但同名多載延續定義仍在」的編譯/vet 管線場景。
	builtinNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		if functionIsBuiltin(program, fd) || functionIsIntrinsic(program, fd) || fd.BuiltinGroup {
			builtinNames[fd.Name] = true
		}
	}
	for name := range program.BuiltinFuncNames {
		builtinNames[name] = true
	}
	// 第二遍：同名的全部定義（含多載延續）一併跳過校驗。nolang 以連續同名
	// `name = ...` 表達 arity 多載，註解只掛在首個定義上；std 重整 / 多模組
	// 合併（merged program）後多載陳述未必仍相鄰，故以名稱聚類穩定覆蓋整個
	// 多載群組，避免對內建多載誤報 "result parameter 'X' is never assigned"。
	suppressed := make(map[*parser.FunctionDefinition]bool)
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		if builtinNames[fd.Name] {
			suppressed[fd] = true
		}
	}
	return suppressed
}

// collectAssignedNames 收集 stmts 中所有「被寫入」的變數名（見
// assignTargetBaseName），以及「作為方法呼叫接收者而被改寫」的變數名。
//
// sem 是語義副表（program.Sem）：merged 程式中 `recv.m(args)` 已被攤平成
// `Type.m(recv, args)`，接收者（第一個實參）已無法從語法自證，只能靠攤平點
// 寫入的 `CalleeOf(call) == parser.CalleeMethod` 認出；否則 `ec.init(c, key)`
// 這類「以方法初始化出參」的函式會被誤報為「返回參數從未賦值」（i3k422u3）。
// sem 為 nil 時等同於「全部未記錄」（安全降級，只是不再認得攤平形態）。
func collectAssignedNames(stmts []parser.Statement, assigned map[string]bool, sem *parser.SemanticContext) {
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
				collectAssignedNamesInExpr(s.Value, assigned, sem)
			}
		case *parser.MultiAssignStatement:
			for _, target := range s.Targets {
				if name := assignTargetBaseName(target); name != "" {
					assigned[name] = true
				}
			}
			if s.Value != nil {
				collectAssignedNamesInExpr(s.Value, assigned, sem)
			}
		case *parser.BlockStatement:
			collectAssignedNames(s.Statements, assigned, sem)
		case *parser.ForStatement:
			if s.Init != nil {
				collectAssignedNames([]parser.Statement{s.Init}, assigned, sem)
			}
			// A range-for loop variable (i <- [a..b): {...}) is always
			// assigned by the iteration, so it must not be flagged as
			// unassigned. This also covers the common idiom of using the
			// named result parameter itself as the loop variable
			// (e.g. `p <- [pos..n): {...}` populating result param `p`).
			if s.IterRange != nil && s.IterRange.Variable != "" {
				assigned[s.IterRange.Variable] = true
			}
			if s.Body != nil {
				collectAssignedNames(s.Body.Statements, assigned, sem)
			}
		case *parser.ExpressionStatement:
			if s.Expression != nil {
				collectAssignedNamesInExpr(s.Expression, assigned, sem)
			}
		case *parser.ReturnStatement:
			if s.ReturnValue != nil {
				collectAssignedNamesInExpr(s.ReturnValue, assigned, sem)
			}
		}
	}
}
func collectAssignedNamesInExpr(expr parser.Expression, assigned map[string]bool, sem *parser.SemanticContext) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			collectAssignedNames(e.Consequence.Statements, assigned, sem)
		}
		if e.Alternative != nil {
			collectAssignedNames(e.Alternative.Statements, assigned, sem)
		}
	case *parser.ConditionalExpression:
		// ternary cond ? a : b — no statements, just expressions
	case *parser.AssignExpression:
		// Nested assignment used as an expression (e.g. inside a loop body:
		// `dst[i] = i`, `dst.len = n`). The left side is the write target.
		if name := assignTargetBaseName(e.Left); name != "" {
			assigned[name] = true
		}
		if e.Value != nil {
			collectAssignedNamesInExpr(e.Value, assigned, sem)
		}
	case *parser.CallExpression:
		// A method call mutates its receiver: `out.init(c, key)`,
		// `entries.push(name)`, `p.init(...)`. If the receiver is a result
		// parameter, that parameter is effectively assigned, so it must not
		// be flagged as "never assigned". This is the same mutating-assignment
		// class as element/field writes (recall assignTargetBaseName resolves
		// `out.field` / `out[i]` to the base identifier `out`).
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			if name := assignTargetBaseName(dot.Receiver); name != "" {
				assigned[name] = true
			}
		}
		// 同一件事的「攤平」形態：`Type.m(recv, args...)`。
		//
		// RunAllLints 跑在 merged 程式上，而合併 std 模組會觸發整程式
		// pass，把方法呼叫攤平成帶顯式 self 的自由函式呼叫
		// （build/transpiler.go：`ce.Function = &parser.Identifier{...}` +
		// 把接收者 unshift 進 Arguments）。攤平後語法上的 DotExpression
		// 已不存在，接收者變成**第一個實參**：
		//     enc-accept = (c conn, key [32]byte) (ec enc-conn) {
		//         ec.init(c, key)          ; 原始語法 → DotExpression 分支
		//     }
		//     ; merged：CallExpression{fn: Identifier{enc-conn.init},
		//     ;                        args: [ec, c, key]}   → 這裡
		// 少了這一支，`ec` 就會被誤報 "never assigned"（i3k422u3）。
		//
		// 判據來自**語義副表**，不再靠名字反推：攤平點
		// （build/transpiler.go 的 resolveMethodCall）在改寫當下會於
		// program.Sem 上記一筆 parser.CalleeMethod，這裡直接讀那個事實。
		//
		// 舊做法是「被呼叫者的名字是否撞上某個方法定義」，已廢除：
		// 它既會漏（衝突改名的自由函式 module.fn 與某方法同名時被誤認成
		// 方法，見 build/module_prefix.go 的命名空間共用問題），也會誤
		// （自由函式首實參按值傳入，本就不該算已賦值）。而且它依賴的是
		// 名字字串能否撞上某個定義這種脆弱推理，而不是一次顯式記錄。
		if sem.CalleeOf(e) == parser.CalleeMethod && len(e.Arguments) > 0 {
			if name := assignTargetBaseName(e.Arguments[0]); name != "" {
				assigned[name] = true
			}
		}
		collectAssignedNamesInExpr(e.Function, assigned, sem)
		for _, a := range e.Arguments {
			collectAssignedNamesInExpr(a, assigned, sem)
		}
	}
}

// assignTargetBaseName returns the root variable name an assignment target
// writes to. For `x = ...` it is "x"; for `x[i] = ...` / `x.field = ...` it
// resolves to "x" (the element/field write still mutates x, so x is assigned);
// nested forms (e.g. `a.b[i] = ...`) resolve to the leftmost identifier "a".
// Returns "" when the target has no identifiable base identifier.
//
// This matters for ValidateUnassignedReturns: a named result parameter that is
// populated via element or field assignment (out[i] = ..., out.field = ...) is
// genuinely assigned, so it must not be flagged as "never assigned".
func assignTargetBaseName(target parser.Expression) string {
	switch t := target.(type) {
	case *parser.Identifier:
		return t.Value
	case *parser.IndexExpression:
		return assignTargetBaseName(t.Left)
	case *parser.DotExpression:
		return assignTargetBaseName(t.Receiver)
	}
	return ""
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
		// Dotted methods have self as the first output parameter (Results[0]),
		// inserted by parseMethodDefinition. Skip it for signature comparison.
		implParams := fd.Parameters
		implResults := fd.Results
		if len(implResults) > 0 && implResults[0].Name == "self" {
			implResults = implResults[1:]
		}
		for _, methods := range ifaces {
			for _, m := range methods {
				if m.Name != implMethod {
					continue
				}
				if len(implParams) != len(m.Params) {
					results = append(results, ValidateResult{
						TraceID:   "m5klw1rq",
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
							TraceID:   "i933a48e",
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
						TraceID:   "iky4xsx4",
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
								TraceID:   "9741sawd",
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
				TraceID: "8yiio0ut",
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
				TraceID: "tgutu5g0",
				Line:    us.Token.Line,
				Column:  us.Token.Column,
				Message: fmt.Sprintf("use '# %s.%s %s' instead of '# %s.%s as %s'", us.Path, us.Function, us.Alias, us.Path, us.Function, us.Alias),
			})
		}
	}
	return results
}
func ValidateRedundantTypeAnnotation(program *parser.Program) []ValidateResult {
	validationMu.Lock()
	defer validationMu.Unlock()
	var results []ValidateResult
	// Build funcTypes for inferExprType
	validationFuncTypes = make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			// declaredResults drops the implicit `self` receiver of methods, so
			// rs[0] is the real return type (absent for void methods).
			if rs := declaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				validationFuncTypes[fd.Name] = rs[0].Type.String()
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
					TraceID: "tcpoxtfd",
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
						TraceID: "379z4njd",
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
				TraceID: "tmqnqq9x",
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
				TraceID: "8dbrd5fk",
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
				TraceID: "lr3c6kg5",
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

// ---------------------------------------------------------------------------
// 整數四則運算溢出 lint（#{overflow} 註解提示）
//
// 自 nolang「永不 panic」的設計約束出發，整數四則運算（有號與無號的 + - *，以及有號
// /）預設在溢出時回傳 option<int>（溢出 → err，正常 → ok(value)），與 codegen 的
// option-on-overflow 預設語意一致（見 src/build/llvm/expr.go 的 emitOverflowArith /
// emitOverflowDivOption）。本 lint 在 LSP / no vet 路徑對「未標註 #{overflow} 的整數
// 運算」發出 Hint，提示使用者可加：
//
//	#{overflow = wrap}    無聲 2's complement 回繞，回傳普通 int
//	#{overflow = clamp0}  下溢歸零，回傳普通 int
//	#{overflow = min}     飽和箝位到型別最小值
//	#{overflow = max}     飽和箝位到型別最大值
//	#{overflow = saturate} 飽和箝位到型別區間（min/max 取決於方向）
//
// 亦可用型別前綴形式指定具體型別，如 `#{overflow = u8-max}`、`#{overflow = i8-min}`，
// 精確控制某個窄型別的飽和界限。LSP 據此 Hint 的 Code（"overflow-wrap"）提供對應
// quickfix 插入註解。
//
// 為避免誤報，只有當左右運算元都可能為整數時才提示：整數字面量，或宣告型別為
// i*/u*/int 的識別字；確定為非整數（float / str / char / bool / byte / rune）的運算
// 元直接跳過。無法確定的運算元（如函式呼叫結果）保守視為有號整數（可能多提示，但不
// 會漏標註導致運行期 err 未被預期）。已標註的函式（FunctionDefinition.OverflowMode !=
// ""）或語句（語義副表 overflow 註解）整體跳過。
// ---------------------------------------------------------------------------

var signedIntTypeNames = map[string]bool{
	"int": true, "i8": true, "i16": true, "i32": true, "i64": true, "i128": true,
}

// unsignedIntTypeNames 是確定為「無號整數」的型別集合；這些型別的 + - * 同樣會因
// 溢出而（預設）回傳 option<int>，故也應提示 #{overflow} 註解。
var unsignedIntTypeNames = map[string]bool{
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true,
}

// nonIntTypeNames 是確定「非整數」（float / str / txt / char / bool / byte / rune）的
// 型別集合；這些型別的算術不回傳 option，故不應提示 #{overflow} 註解（避免誤報）。
// "float" 是 number.no 中定義的型別別名（float = f32 | f64），必須在此註冊，
// 否則 operandIntKind 對 float 型變數會保守回退為 "signed"，導致 float.div
// （`q = . / b`）被誤報為整數溢出。
var nonIntTypeNames = map[string]bool{
	"float": true, "num": true, "number.float": true, "number.num": true,
	"f32": true, "f64": true,
	"str": true, "txt": true, "char": true, "bool": true, "byte": true, "rune": true,
}

// overflowAnnotatedNode 報告節點是否攜帶 #{overflow = ...} 註解。
//
// 與編譯期檢查 stmtOverflowAnnotated（ValidateUnhandledOverflow 使用）嚴格一致：
// 同時查詢 AnnotationsOf（ResolveProgram 後）與 RawAnnotationsOf（解析期 side-table），
// 並回退到節點的 OverflowMode 欄位。原因：活躍的「行註解」路徑
// （parser.applyLineOverflowAnnotations）只把溢出模式寫入陳述節點的 OverflowMode
// 欄位、不寫入 side-table；而 std 函式庫正是以「每條陳述前一行 #{overflow=wrap}」的
// 寫法逐站修復沉默泄漏。若本檢查只讀 AnnotationsOf，就會把這些已正確標註的程式
// 誤報為 ovf-int-default，與編譯期檢查（讀欄位）自相矛盾。兩者統一後，lint 與
// 編譯對「已標註」的判定一致，已帶 #{overflow} 的程式不再被重複報錯。
func overflowAnnotatedNode(sem *parser.SemanticContext, n parser.Node) bool {
	if sem == nil || n == nil {
		return false
	}
	hasOverflow := func(entries []*parser.AnnotationEntry) bool {
		for _, e := range entries {
			if e != nil && e.Key == "overflow" {
				return true
			}
		}
		return false
	}
	if hasOverflow(sem.AnnotationsOf(n)) || hasOverflow(sem.RawAnnotationsOf(n)) {
		return true
	}
	switch s := n.(type) {
	case *parser.ForStatement:
		return s.OverflowMode != ""
	case *parser.ExpressionStatement:
		return s.OverflowMode != ""
	case *parser.LetStatement:
		return s.OverflowMode != ""
	case *parser.ReturnStatement:
		return s.OverflowMode != ""
	case *parser.MultiAssignStatement:
		return s.OverflowMode != ""
	}
	return false
}

// operandIntKind 啟發式判斷運算元「是否為整數」，回傳三者之一：
//   - "signed"  確定為有號整數，或無法確定（保守假定為有號整數）；
//   - "unsigned" 確定為無號整數；
//   - ""        確定為非整數（float / str / char / bool / byte / rune 等），
//     這些算術不回傳 option，不應提示 #{overflow}。
//
// 僅當運算元確定為非整數型別時傳回 ""，避免對 float / str 算術誤報（它們不回傳
// option，無 #{overflow} 之說）。
func operandIntKind(e parser.Expression, declared map[string]string) string {
	switch x := e.(type) {
	case *parser.IntegerLiteral:
		return "signed" // 整數字面量預設為有號
	case *parser.FloatLiteral:
		return "" // float 算術不回傳 option
	case *parser.StringLiteral, *parser.CharLiteral, *parser.BooleanLiteral, *parser.RegexLiteral:
		// str/txt 字面量（同一 StringLiteral 節點）、char、bool、regex 皆為確定
		// 非整數運算元：str/txt 的 `-` 是字串拼接、不產生 option<int>，不應提示
		// #{overflow}。此前這些字面量掉到尾部保守 `return "signed"`，導致
		// `'got=' - x.to-str()` 這類拼接被誤報為整數溢出。
		return ""
	case *parser.PrefixExpression:
		// 一元負號修飾整數字面量，如 -1
		if x.Operator == "-" {
			if _, ok := x.Right.(*parser.IntegerLiteral); ok {
				return "signed"
			}
		}
		return ""
	}
	if id, ok := e.(*parser.Identifier); ok {
		t, ok := declared[id.Value]
		if !ok {
			return "signed" // 未知型別：保守視為有號整數
		}
		if signedIntTypeNames[t] {
			return "signed"
		}
		if unsignedIntTypeNames[t] {
			return "unsigned"
		}
		if nonIntTypeNames[t] {
			return ""
		}
		return "signed" // 其他已宣告型別（如 struct 別名）：保守視為有號
	}
	return "signed" // 呼叫 / 索引 / 成員等：保守視為可能是有號整數
}

// overflowArithOps 是會因整數溢出而（預設）回傳 option<int> 的二元運算子。
// 對應 codegen 的 option-on-overflow 預設語意：+ - * 對有號與無號皆適用；
// 有號 / 只有 INT_MIN / -1 會溢出，無號 / 因 a/b <= a 永不溢出。
var overflowArithOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true,
}

// walkExprForIntOverflow 遞迴收集表達式內所有「未標註且可能溢出的整數運算」InfixExpression。
// 規則（與 codegen 預設 option-on-overflow 語意一致）：
//   - + - *：左右運算元皆為整數（有號或無號，含未知型別）時提示；任一侧確定為
//     非整數（str / float 等）則跳過（該運算非整數算術）。
//   - /：僅當至少一側為有號整數時提示（無號除法永不溢出；有號除法 INT_MIN / -1 溢出）。
//
// line 為包容此表達式的「葉」語句所在行（插入註解的位置）；sem 用於遞迴進入 if
// 分支區塊時對其中的子語句做註解感知掃描。
func walkExprForIntOverflow(e parser.Expression, file string, line int, sem *parser.SemanticContext, declared map[string]string, emit func(file string, line, col int)) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *parser.InfixExpression:
		if overflowArithOps[x.Operator] {
			lk := operandIntKind(x.Left, declared)
			rk := operandIntKind(x.Right, declared)
			if lk != "" && rk != "" {
				prompt := false
				if x.Operator == "/" {
					// 無號除法永不溢出：僅有號（含未知型別）一側才提示。
					if lk == "signed" || rk == "signed" {
						prompt = true
					}
				} else {
					prompt = true
				}
				if prompt {
					emit(file, line, x.Token.Column)
				}
			}
		}
		walkExprForIntOverflow(x.Left, file, line, sem, declared, emit)
		walkExprForIntOverflow(x.Right, file, line, sem, declared, emit)
	case *parser.PrefixExpression:
		walkExprForIntOverflow(x.Right, file, line, sem, declared, emit)
	case *parser.CallExpression:
		walkExprForIntOverflow(x.Function, file, line, sem, declared, emit)
		for _, a := range x.Arguments {
			walkExprForIntOverflow(a, file, line, sem, declared, emit)
		}
	case *parser.IfExpression:
		walkExprForIntOverflow(x.Condition, file, line, sem, declared, emit)
		if x.Consequence != nil {
			for _, s := range x.Consequence.Statements {
				walkStmtForOverflow(s, file, sem, declared, emit)
			}
		}
		if x.Alternative != nil {
			for _, s := range x.Alternative.Statements {
				walkStmtForOverflow(s, file, sem, declared, emit)
			}
		}
	case *parser.IndexExpression:
		walkExprForIntOverflow(x.Left, file, line, sem, declared, emit)
		walkExprForIntOverflow(x.Index, file, line, sem, declared, emit)
	case *parser.AssignExpression:
		walkExprForIntOverflow(x.Left, file, line, sem, declared, emit)
		walkExprForIntOverflow(x.Value, file, line, sem, declared, emit)
	case *parser.ArrayLiteral:
		for _, el := range x.Elements {
			walkExprForIntOverflow(el, file, line, sem, declared, emit)
		}
	}
}

// walkStmtForOverflow 註解感知地遍歷語句樹，對每個未標註的「葉」語句掃描其表達式
// 內的有號整數相減，並以該語句所在行（插入註解的位置）呼叫 emit(file, line, col)。
// col 為運算子的欄位，供 LSP 高亮；file 為語句所屬來源檔。
//
// file 由外層（頂層函式 / 語句）逐層傳入，而非取節點自身的 SourceFile：合併模式下
// 只有頂層語句被 SetSourceFile 標記，嵌套語句的 SourceFile 為空；若在此處取空值，
// RunAllLints 的行號範圍回退歸因會把不同模組的同名行號張冠李戴（例如把 str.no 的
// 整數運算算到 byte.no 頭上）。節點若確有自身 SourceFile，則以該值覆蓋。
func walkStmtForOverflow(stmt parser.Statement, file string, sem *parser.SemanticContext, declared map[string]string, emit func(file string, line, col int)) {
	if stmt == nil {
		return
	}
	// 函式定義：受函式級註解管轄；以本函數自身作用域的宣告型別掃描。
	if fn, ok := stmt.(*parser.FunctionDefinition); ok {
		if fn.OverflowMode == "" && fn.Body != nil {
			fnFile := parser.GetSourceFile(fn)
			if fnFile == "" {
				fnFile = file
			}
			fnDeclared := collectFuncDeclared(fn)
			for _, b := range fn.Body.Statements {
				walkStmtForOverflow(b, fnFile, sem, fnDeclared, emit)
			}
		}
		return
	}
	// 語句級註解：整條語句內的相減都受此註解管轄，跳過
	if overflowAnnotatedNode(sem, stmt) {
		return
	}
	if f := parser.GetSourceFile(stmt); f != "" {
		file = f
	}
	emitSubs := func(e parser.Expression) {
		if e != nil {
			walkExprForIntOverflow(e, file, stmt.Pos().Line, sem, declared, emit)
		}
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// 顯式宣告成 option 型別（`d ?i64 = x - 1`）是訊息明列的合法方案②
		// 「用 ?T 接收，再以 match / ?= 消費」，不該再報。lowering 亦會把
		// `t ?= a - b` 合成為 `__unwrap_N ?i64 = a - b`（見 lowerUnwrapAssign），
		// 故同一豁免同時覆蓋 `?=` 形式。此處與編譯期檢查 ValidateUnhandledOverflow
		// 的 declaredOption 豁免保持一致，否則 lint 會誤報、而 `no build` 卻通過。
		if s.Type != nil && strings.HasPrefix(s.Type.String(), "?") {
			return
		}
		// When s.Type is nil the variable has no explicit annotation in the source
		// (e.g. `q = . / b` where `q` is a function result param declared as `?int`).
		// If the target is a known option-type result param, the overflow IS being
		// handled — suppress the lint to avoid a false positive.
		//
		// 同一豁免也涵蓋 lowering 捕獲 pass 的產物：`a = b + c / d` 展開成
		// `a ?T = nil` 預宣告 + ok 臂 `a = b + __cap_1`（後者不帶型別節點），
		// `a` 由 collectLetsInStmts 以 `?T` 登記，故這裡讀得到。
		if s.Type == nil && s.Name != nil {
			if t := declared[s.Name.Value]; strings.HasPrefix(t, "?") {
				return
			}
		}
		// `_ = expr`：顯式捨棄，求值本身在執行期安全（捕獲推斷走 option 路徑），
		// 使用者已表明不要這個值與其錯誤 → 與 ValidateUnhandledOverflow 一致地豁免。
		if s.Name != nil && s.Name.Value == "_" {
			return
		}
		emitSubs(s.Value)
	case *parser.ReturnStatement:
		emitSubs(s.ReturnValue)
	case *parser.ExpressionStatement:
		emitSubs(s.Expression)
	case *parser.MultiAssignStatement:
		emitSubs(s.Value)
	case *parser.UnwrapAssignStatement:
		// 顯式 `?=` 已主動處理溢出（將 option 上拋給呼叫者），是 ovf-int-default
		// 訊息明列的合法替代方案，不再重複提示。
	case *parser.ForStatement:
		emitSubs(s.Condition)
		if s.Init != nil {
			walkStmtForOverflow(s.Init, file, sem, declared, emit)
		}
		if s.Update != nil {
			walkStmtForOverflow(s.Update, file, sem, declared, emit)
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				walkStmtForOverflow(b, file, sem, declared, emit)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			walkStmtForOverflow(b, file, sem, declared, emit)
		}
	}
}

// collectFuncDeclared 收集單一函數體內可見的參數與 let 顯式型別，作用域限定在該
// 函數（不與其他函數 / std 模組的參數名撞名）。nolang vet 會把 std 模組合併進同一
// 個 program；若用全域 name→type map，使用者函數的 `a`/`b` 會被 std 中同名參數（多為
// u8/byte 等無號型別）覆寫，導致誤判（如把有號相減誤當無號、或把 float 相減誤報 /
// 漏報）。故必須按函數作用域收集。
func collectFuncDeclared(fn *parser.FunctionDefinition) map[string]string {
	types := map[string]string{}
	for _, p := range fn.FuncSignature.Parameters {
		if p.Type != nil {
			if nt, ok := p.Type.(*parser.NamedType); ok {
				types[p.Name] = nt.Value
			}
		}
	}
	// Also register result params (including the implicit `self` receiver added
	// by the parser for method definitions, e.g. float.div → self has type "float").
	// Using Type.String() so that option result params like `q ?int` appear as "?int",
	// which walkStmtForOverflow uses to suppress the ovf-int-default false positive
	// when the overflow-aware result is being assigned to an already-option target.
	for _, p := range fn.FuncSignature.Results {
		if p.Type != nil {
			types[p.Name] = p.Type.String()
		}
	}
	if fn.Body != nil {
		collectLetsInStmts(fn.Body.Statements, types)
	}
	return types
}

// collectLetsInStmts 遞迴收集區塊 / for / 巢狀函數體內的 let 顯式型別，寫入 types。
// 巢狀函數的參數作用域獨立，不寫入外層 types（由各自 collectFuncDeclared 處理）。
func collectLetsInStmts(stmts []parser.Statement, types map[string]string) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *parser.LetStatement:
			if s.Name != nil && s.Type != nil {
				if nt, ok := s.Type.(*parser.NamedType); ok {
					types[s.Name.Value] = nt.Value
				} else {
					// `?T` 等非具名型別：以字串形式登記（`?i64`），供
					// walkStmtForOverflow 的 declared-option 豁免判定。lowering 的
					// 捕獲 pass 會合成 `a ?T = nil` 預宣告，其 ok 臂的最終賦值
					// （`a = b + __cap_1`）不帶型別節點，只能靠這裡登記的 `?T`
					// 認出「目標是 option → 溢出已就地捕獲」，否則該賦值會被
					// 誤報為 ovf-int-default（訊息卻正是推薦這種寫法）。
					types[s.Name.Value] = s.Type.String()
				}
			}
		case *parser.FunctionDefinition:
			if s.Body != nil {
				collectLetsInStmts(s.Body.Statements, types)
			}
		case *parser.ForStatement:
			if s.Init != nil {
				collectLetsInStmts([]parser.Statement{s.Init}, types)
			}
			// 迭代變數（`for ch <- s`）不是 let，原本從未登記型別；字串迭代的
			// 元素是 char，不登記會被保守當成整數，把 `ch - 32` 這類字元算術
			// 誤報為未處理的整數溢位（見 iterElemType）。
			if et := iterElemType(s.IterRange, types, ""); et != "" {
				types[s.IterRange.Variable] = et
			}
			if s.Body != nil {
				collectLetsInStmts(s.Body.Statements, types)
			}
		case *parser.BlockStatement:
			collectLetsInStmts(s.Statements, types)
		}
	}
}

// iterElemType 推斷 `for x <- src` 迭代變數 x 的元素型別，供溢位校驗的型別表登記。
//
// 只對「確定非整數家族」的迭代源回傳型別——str 字面量 / 字串切片 / 型別為 str 的
// 識別字，其元素皆是 char；數值區間（`for i <- [0..3]`）與推斷不出的來源一律回
// ""，呼叫方據此不登記，維持既有的「型別未知 → 保守視為整數」語意。因此本函式
// 只會消除誤報，不會放寬整數溢位（+ - * / 預設 option<int>）的檢查。
//
// 動機：迭代變數原本從未進入型別表，於是
//
//	for ch <- s { u = ch - 32 }   // s str
//
// 裡的 ch 型別未知 → 被當成整數 → 字元減法被誤報 [ovfhndld]。char 本身不在
// isIntType 之內（其算術不產生 option），登記為 char 後即不再誤報。
func iterElemType(ie *parser.IterationExpr, varTypes map[string]string, selfType string) string {
	if ie == nil || ie.Variable == "" {
		return ""
	}
	// `for ch <- 'abc'`：迭代字串字面量，元素是 char。
	if ie.RangeStr != "" {
		return "char"
	}
	if ie.RangeExpr == nil {
		return ""
	}
	if _, ok := ie.RangeExpr.(*parser.StringLiteral); ok {
		return "char"
	}
	// `for ch <- s` / `for ch <- s[a..b]`：迭代源為 str 時元素是 char。
	// 陣列 / 切片 / 未知型別的迭代源一律回 "" → 不登記（保守路徑不變）。
	if inferExprType(ie.RangeExpr, varTypes, nil, selfType) == "str" {
		return "char"
	}
	return ""
}

// collectTopLevelLets 收集模組頂層 let 的顯式型別，供頂層（非函數）語句掃描使用。
// 非 let 的頂層陳述（for / 區塊）另行遞迴收集其內部型別，使頂層 `for ch <- s`
// 的迭代變數也能帶上 char 型別（見 iterElemType），避免字元算術被誤報。
func collectTopLevelLets(program *parser.Program) map[string]string {
	types := map[string]string{}
	if program == nil {
		return types
	}
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Name != nil && ls.Type != nil {
				if nt, ok := ls.Type.(*parser.NamedType); ok {
					types[ls.Name.Value] = nt.Value
				} else {
					// 與 collectLetsInStmts 同：`?T` 以字串形式登記，讓頂層指令稿
					// 的捕獲賦值（`a = b + c / d` → `a ?T = nil` 預宣告）同樣豁免
					// ovf-int-default。
					types[ls.Name.Value] = ls.Type.String()
				}
			}
			continue
		}
		collectLetsInStmts([]parser.Statement{stmt}, types)
	}
	return types
}

// ValidateIntOverflow 對未標註 #{overflow} 的整數四則運算（有號與無號的 + - *，
// 以及有號 /）發出 Hint，提示可加 #{overflow = wrap|clamp0|min|max|saturate} 註解
// （亦支援型別前綴形式如 u8-max / i8-min）。
func ValidateIntOverflow(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	sem := program.Sem
	var results []ValidateResult
	emit := func(file string, line, col int) {
		results = append(results, ValidateResult{
			File:    file,
			Line:    line,
			Column:  col,
			Message: "整數運算 `a OP b` 預設在溢出時回傳 option<int>，現已列為錯誤。請二選一：①加註解 `#{overflow = wrap}`（無聲回繞）/`clamp0`（下溢歸零）/`min`/`max`/`saturate`（飽和箝位），或用型別前綴形式如 `#{overflow = u8-max}`、`#{overflow = i8-min}` 回普通 int；②以 `?T` 接收（`a ?i64 = x + y`）或直接 `a = x + y` 讓編譯器推斷成 option 就地捕獲錯誤，再用 `?=` / match 處理；③`_ = x + y` 顯式丟棄。",
			TraceID: "ovf-int-default",
		})
	}
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*parser.FunctionDefinition); ok {
			if fn.OverflowMode == "" && fn.Body != nil {
				// 作用域限定：僅用本函數自身的參數 / let 型別，避免與 std 模組
				// 同名參數互相污染（nolang vet 會合併 std 進同一 program）。
				declared := collectFuncDeclared(fn)
				file := parser.GetSourceFile(fn)
				for _, b := range fn.Body.Statements {
					walkStmtForOverflow(b, file, sem, declared, emit)
				}
			}
			continue
		}
		declared := collectTopLevelLets(program)
		walkStmtForOverflow(stmt, parser.GetSourceFile(stmt), sem, declared, emit)
	}
	return results
}

// StatementsWithIntOverflow 回傳「包含至少一個會溢出的整數運算」的陳述集合
// （含巢狀 for / 區塊 / 函式體內的葉陳述，以及包容它們的容器陳述）。
//
// 用途：判斷某條 #{overflow = ...} 註解是否「實際生效」。若被標註的陳述不在本集合
// 中，該註解對整數溢出毫無作用（例如整行只是字串拼接，中劃線 `-` 是拼接而非減法），
// 應提示刪除、並在 `no fmt` 時移除。
//
// 與 ValidateIntOverflow / ValidateUnhandledOverflow 共用同一套型別推斷（inferExprType /
// isIntExpr / isDirectOverflowValue），且對「型別未知」的運算元保守視為整數（寧可多報，
// 因為這樣只會讓我們「保留」註解、不會「誤刪」有效的註解）。因此本集合是超集性質：
// 落在集合中的陳述未必真的需要註解，但不在集合中的陳述註解必定無效。
func StatementsWithIntOverflow(program *parser.Program) map[parser.Statement]bool {
	if program == nil {
		return nil
	}
	relevant := map[parser.Statement]bool{}

	// 先宣告兩個遞迴閉包變數（互相參考），再於下方指派，避免前向參考未定義。
	var exprHasIntOverflow func(e parser.Expression, declared map[string]string, selfType string) bool
	var stmtHasIntOverflow func(stmt parser.Statement, declared map[string]string, selfType string) bool

	// exprHasIntOverflow 遞迴判斷表達式（含 if 分支陳述）是否含整數溢出運算。
	// 對 InfixExpression 直接取用 isDirectOverflowValue（頂層運算元皆為整數時回 true，
	// 未知型別保守視為整數）；對其它結構遞迴進子表達式 / 子陳述。
	// 注意：必須「窮盡遍歷」不得提前 return true——relevant 集合是在 stmtHasIntOverflow
	// 递归过程中副作用登记的，短路会遗漏后续兄弟语句（如 match 第二臂的 `x = x - 1`），
	// 使其永远不在集合中，`no fmt` 就会把管辖真实整数运算的 #{overflow} 注解误删。
	exprHasIntOverflow = func(e parser.Expression, declared map[string]string, selfType string) bool {
		if e == nil {
			return false
		}
		found := false
		switch x := e.(type) {
		case *parser.InfixExpression:
			if isDirectOverflowValue(x, declared, selfType) ||
				(overflowArithOps[x.Operator] &&
					looseOperandIsInt(x.Left, declared, selfType) &&
					looseOperandIsInt(x.Right, declared, selfType)) {
				found = true
			}
			if exprHasIntOverflow(x.Left, declared, selfType) {
				found = true
			}
			if exprHasIntOverflow(x.Right, declared, selfType) {
				found = true
			}
		case *parser.PrefixExpression:
			if exprHasIntOverflow(x.Right, declared, selfType) {
				found = true
			}
		case *parser.CallExpression:
			if exprHasIntOverflow(x.Function, declared, selfType) {
				found = true
			}
			for _, a := range x.Arguments {
				if exprHasIntOverflow(a, declared, selfType) {
					found = true
				}
			}
		case *parser.IfExpression:
			if exprHasIntOverflow(x.Condition, declared, selfType) {
				found = true
			}
			if x.Consequence != nil {
				for _, s := range x.Consequence.Statements {
					if stmtHasIntOverflow(s, declared, selfType) {
						found = true
					}
				}
			}
			if x.Alternative != nil {
				for _, s := range x.Alternative.Statements {
					if stmtHasIntOverflow(s, declared, selfType) {
						found = true
					}
				}
			}
		case *parser.IndexExpression:
			if exprHasIntOverflow(x.Left, declared, selfType) {
				found = true
			}
			if exprHasIntOverflow(x.Index, declared, selfType) {
				found = true
			}
		case *parser.AssignExpression:
			if exprHasIntOverflow(x.Left, declared, selfType) {
				found = true
			}
			if exprHasIntOverflow(x.Value, declared, selfType) {
				found = true
			}
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				if exprHasIntOverflow(el, declared, selfType) {
					found = true
				}
			}
		}
		return found
	}

	// stmtHasIntOverflow 遞迴判斷陳述（含其巢狀子陳述）是否含整數溢出運算；
	// 含溢出則標記該陳述與所有包容它的容器陳述為 relevant。
	stmtHasIntOverflow = func(stmt parser.Statement, declared map[string]string, selfType string) bool {
		if stmt == nil {
			return false
		}
		found := false
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			fd := collectFuncDeclared(s)
			ft := methodSelfType(s)
			if s.Body != nil {
				for _, b := range s.Body.Statements {
					if stmtHasIntOverflow(b, fd, ft) {
						found = true
					}
				}
			}
		case *parser.ForStatement:
			if exprHasIntOverflow(s.Condition, declared, selfType) {
				found = true
			}
			if s.Init != nil && stmtHasIntOverflow(s.Init, declared, selfType) {
				found = true
			}
			if s.Update != nil && stmtHasIntOverflow(s.Update, declared, selfType) {
				found = true
			}
			if s.Body != nil {
				for _, b := range s.Body.Statements {
					if stmtHasIntOverflow(b, declared, selfType) {
						found = true
					}
				}
			}
		case *parser.BlockStatement:
			for _, b := range s.Statements {
				if stmtHasIntOverflow(b, declared, selfType) {
					found = true
				}
			}
		case *parser.LetStatement:
			if exprHasIntOverflow(s.Value, declared, selfType) {
				found = true
			}
		case *parser.ReturnStatement:
			if exprHasIntOverflow(s.ReturnValue, declared, selfType) {
				found = true
			}
		case *parser.ExpressionStatement:
			if exprHasIntOverflow(s.Expression, declared, selfType) {
				found = true
			}
		case *parser.MultiAssignStatement:
			if exprHasIntOverflow(s.Value, declared, selfType) {
				found = true
			}
		case *parser.UnwrapAssignStatement:
			if exprHasIntOverflow(s.Value, declared, selfType) {
				found = true
			}
		}
		if found {
			relevant[stmt] = true
		}
		return found
	}

	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*parser.FunctionDefinition); ok {
			fd := collectFuncDeclared(fn)
			ft := methodSelfType(fn)
			if fn.Body != nil {
				for _, b := range fn.Body.Statements {
					if stmtHasIntOverflow(b, fd, ft) {
						relevant[stmt] = true
					}
				}
			}
			continue
		}
		declared := collectTopLevelLets(program)
		if stmtHasIntOverflow(stmt, declared, "") {
			relevant[stmt] = true
		}
	}
	return relevant
}

// OverflowAnnotationRelevance 回傳兩組資訊，供 `no fmt` 與 LSP 判斷並移除「無效」的
// #{overflow = ...} 註解：
//
//   - relevant：與 StatementsWithIntOverflow 相同，是「含整數溢出運算」的陳述集合
//     （超集性質：落在集合中的陳述未必真的需要註解，但不在其中的註解必定無效）。
//   - governed：把「獨立成行」的 #{overflow = ...} 註解節點（*parser.AnnotationStatement）
//     對應到它「行注解」語意下所管轄的下一條陳述（*parser.Statement；若後方沒有任何
//     被管轄的陳述則為 nil）。語意與 parser.applyLineOverflowAnnotations 完全一致：
//     連續的非 overflow 註解（如緊跟的 #{index-out=0}）會被略過，直到遇到第一條非
//     註解陳述（即被管轄者）；若下一條是另一個含 overflow 的註解，則本註解不轄制任何
//     陳述（gov 為 nil）。
//
// 使用方式：formatter 在輸出 overflow 註解時，
//   - 對「附加」路徑（IDENT 起始陳述被 parser 直接附加的 overflow）以被標註陳述本身
//     查 relevant；
//   - 對「獨立節點」路徑以 governed 查 relevant。
//
// 兩組皆為 nil 時退回「保留所有 overflow 註解」的舊行為（不刪除），確保無型別資訊的
// 場景（如 LSP 純格式化）不會誤刪。
func OverflowAnnotationRelevance(program *parser.Program) (relevant map[parser.Statement]bool, governed map[*parser.AnnotationStatement]parser.Statement) {
	if program == nil {
		return nil, nil
	}
	relevant = StatementsWithIntOverflow(program)
	governed = map[*parser.AnnotationStatement]parser.Statement{}

	// scanList 在「同一個陳述列表」內掃描獨立 overflow 註解，並依行注解語意計算其
	// 管轄陳述（僅限同一列表內的下一條陳述——與 parser 逐區塊處理的語意一致）。
	scanList := func(stmts []parser.Statement) {
		for i, s := range stmts {
			as, ok := s.(*parser.AnnotationStatement)
			if !ok {
				continue
			}
			if !hasOverflowEntry(as.Entries) {
				continue
			}
			for j := i + 1; j < len(stmts); j++ {
				next := stmts[j]
				if next == nil {
					continue
				}
				if as2, isAnn := next.(*parser.AnnotationStatement); isAnn {
					if hasOverflowEntry(as2.Entries) {
						break
					}
					continue
				}
				governed[as] = next
				break
			}
		}
	}

	// collect 遞迴走遍所有陳述列表（頂層、函式體、區塊體、for 迴圈體），每個列表
	// 各自獨立掃描（與 parser 對每個 BlockStatement 呼叫 applyLineOverflowAnnotations
	// 的語意一致）。另必須下鑽 if/match 臂體（ExpressionStatement 或 Let 的值為
	// IfExpression）：被管辖语句以 `.` 开头时（如 `.len = cur + 1`）parser 不会把行
	// 注解附加到语句上，注解以独立 AnnotationStatement 形式留在臂块内；若此处不
	// 扫描，governed 查不到 → gov=nil → formatter 误删有效的 #{overflow} 注解。
	var collect func(stmts []parser.Statement)
	collectIf := func(ie *parser.IfExpression) {
		if ie == nil {
			return
		}
		if ie.Consequence != nil {
			collect(ie.Consequence.Statements)
		}
		if ie.Alternative != nil {
			collect(ie.Alternative.Statements)
		}
	}
	collect = func(stmts []parser.Statement) {
		scanList(stmts)
		for _, s := range stmts {
			switch v := s.(type) {
			case *parser.FunctionDefinition:
				if v.Body != nil {
					collect(v.Body.Statements)
				}
			case *parser.BlockStatement:
				collect(v.Statements)
			case *parser.ForStatement:
				if v.Body != nil {
					collect(v.Body.Statements)
				}
			case *parser.ExpressionStatement:
				if ie, ok := v.Expression.(*parser.IfExpression); ok {
					collectIf(ie)
				}
			case *parser.LetStatement:
				if ie, ok := v.Value.(*parser.IfExpression); ok {
					collectIf(ie)
				}
			}
		}
	}
	collect(program.Statements)
	return relevant, governed
}

// hasOverflowEntry 報告註解條目中是否含有 overflow 鍵。
func hasOverflowEntry(entries []*parser.AnnotationEntry) bool {
	for _, e := range entries {
		if e != nil && e.Key == "overflow" {
			return true
		}
	}
	return false
}

// methodSelfType 回傳方法定義接收者 self 的型別字串（如 "str"），供
// inferExprType 推斷 self.x 成員型別；非方法定義回傳 ""。
func methodSelfType(fn *parser.FunctionDefinition) string {
	if fn == nil || !fn.IsMethodDef {
		return ""
	}
	if len(fn.FuncSignature.Parameters) > 0 && fn.FuncSignature.Parameters[0].Type != nil {
		return fn.FuncSignature.Parameters[0].Type.String()
	}
	return ""
}

// ValidateDivByZero 對整數除法 / 與取模 % 的「字面常數零除數」發出編譯期錯誤。
//
// 背景：Nolang 的整數除法/取模在 MIR 後端直接發射裸 sdiv/srem/udiv/urem，對零除數
// 沒有任何執行時期檢查。LLVM 語言參考明定「除以零是未定義行為（UB）」，除數為 0 時
// 產生 poison，且最佳化器會假設除數永遠非 0（如將 X/X 折疊成常數 1，前提正是 X≠0）。
// 這會造成靜默的錯誤結果甚至 trace/BPT trap，且同一支程式在 --js 後端語意完全不同
// （Infinity/NaN）。src/ 內搜尋不到任何 div-by-zero 防護，src/std/number.no 亦自註
// 「b 為 0 時行為未定義」。
//
// 本規則只攔截「除數為整數字面量 0」這種可在編譯期靜態判定的情形（如 `10 / 0`、`a % 0`、
// `x / 0`），與溢出檢查無關（不受 #{overflow} 註解豁免）。浮點 / 0.0 屬 IEEE-754 定義
// （±inf / nan），不在管轄範圍；執行時期才確定的零除數（如 `a / b` 且 b 來自外部輸入）
// 無法靜態偵測，留待未來的執行時期檢查處理。
func ValidateDivByZero(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	emit := func(file string, line, col int) {
		results = append(results, ValidateResult{
			File:    file,
			Line:    line,
			Column:  col,
			Message: "整數除法 / 取模的除數是字面常數 0，會觸發 LLVM 未定義行為（sdiv/srem 除以零），產生不可預測的結果。請改以執行時期零檢查，或讓函式以 option 回傳錯誤。",
			TraceID: "div-by-zero",
		})
	}
	for _, stmt := range program.Statements {
		walkStmtDivByZero(stmt, "", emit)
	}
	return results
}

// walkStmtDivByZero 遞迴走訪陳述，對每個表達式呼叫 walkExprDivByZero。
// file 由頂層逐層傳入（同 walkStmtForOverflow 的歸因邏輯），故不取節點自身 SourceFile。
func walkStmtDivByZero(stmt parser.Statement, file string, emit func(file string, line, col int)) {
	if stmt == nil {
		return
	}
	if f := parser.GetSourceFile(stmt); f != "" {
		file = f
	}
	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				walkStmtDivByZero(b, file, emit)
			}
		}
	case *parser.LetStatement:
		walkExprDivByZero(s.Value, file, emit)
	case *parser.ReturnStatement:
		walkExprDivByZero(s.ReturnValue, file, emit)
	case *parser.ExpressionStatement:
		walkExprDivByZero(s.Expression, file, emit)
	case *parser.MultiAssignStatement:
		walkExprDivByZero(s.Value, file, emit)
	case *parser.UnwrapAssignStatement:
		walkExprDivByZero(s.Value, file, emit)
	case *parser.ForStatement:
		walkExprDivByZero(s.Condition, file, emit)
		if s.Init != nil {
			walkStmtDivByZero(s.Init, file, emit)
		}
		if s.Update != nil {
			walkStmtDivByZero(s.Update, file, emit)
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				walkStmtDivByZero(b, file, emit)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			walkStmtDivByZero(b, file, emit)
		}
	}
}

// walkExprDivByZero 遞迴走訪表達式；對 / 與 % 且右運算元為整數字面量 0 的節點報錯。
func walkExprDivByZero(e parser.Expression, file string, emit func(file string, line, col int)) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *parser.InfixExpression:
		if x.Operator == "/" || x.Operator == "%" {
			if isLiteralZeroDivisor(x.Right) {
				emit(file, x.Token.Line, x.Token.Column)
			}
		}
		walkExprDivByZero(x.Left, file, emit)
		walkExprDivByZero(x.Right, file, emit)
	case *parser.PrefixExpression:
		walkExprDivByZero(x.Right, file, emit)
	case *parser.GroupedExpression:
		walkExprDivByZero(x.Expression, file, emit)
	case *parser.CallExpression:
		walkExprDivByZero(x.Function, file, emit)
		for _, a := range x.Arguments {
			walkExprDivByZero(a, file, emit)
		}
	case *parser.IfExpression:
		walkExprDivByZero(x.Condition, file, emit)
		if x.Consequence != nil {
			for _, st := range x.Consequence.Statements {
				walkStmtDivByZero(st, file, emit)
			}
		}
		if x.Alternative != nil {
			for _, st := range x.Alternative.Statements {
				walkStmtDivByZero(st, file, emit)
			}
		}
	case *parser.IndexExpression:
		walkExprDivByZero(x.Index, file, emit)
	case *parser.AssignExpression:
		walkExprDivByZero(x.Left, file, emit)
		walkExprDivByZero(x.Value, file, emit)
	case *parser.ArrayLiteral:
		for _, el := range x.Elements {
			walkExprDivByZero(el, file, emit)
		}
	}
}

// isLiteralZeroDivisor 報告除數表達式是否為整數字面量 0（或前置負號包住的 0）。
// 浮點字面量 0.0 不算（IEEE-754 定義除法為 inf/nan，非 UB）。
func isLiteralZeroDivisor(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.IntegerLiteral:
		return v.Value == 0
	case *parser.PrefixExpression:
		if v.Operator == "-" {
			return isLiteralZeroDivisor(v.Right)
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────
// 未處理的溢出 option（編譯硬錯誤）
//
// 背景：未標註 #{overflow = ...} 的整數運算（+ - * /，有號/無號）在溢出時預設回傳
// option<int>（永不 panic）。這個 option 若不被處理，就會「沉默泄漏」：變數被推斷為
// option 但程式其實把它當普通 int 用，運行期語意漂移（且 codegen 的 option 是
// {tag,data} 結構，直接當 int 解引用會錯）。本規則在編譯期攔截這種「產生 option 卻
// 沒處理」的寫法，迫使程式設計師顯式二選一：
//   1. 加 #{overflow = wrap|clamp0|min|max|saturate} 註解 → 回普通 int；
//   2. 用 `x ?= a + b` 上拋（錯誤傳給呼叫者），或 match 解構，或顯式宣告 `x ?i64 = ...`。
//
// 與 overflowModeFromNode（codegen）保持一致：一個陳述的溢出模式若被 codegen 視為
// 「已標註」（wrap 等），本規則即視為「已處理」；反之預設 option 模式即視為「未處理」。
// 這保證檢查與實際 codegen 語意嚴格一致，不會誤報已正確標註的程式。

const unhandledOverflowTraceID = "ovfhndld"

// stmtOverflowAnnotated 報告陳述是否已被 #{overflow = ...} 註解處理（與 codegen 的
// overflowModeFromNode 判定一致）：函式級註解不再視為作用域，故只看陳述自身。
// 同時查詢 Annotations（ResolveProgram 後）與 RawAnnotations（解析期），因為某些
// 校驗路徑（單檔 / 依賴模組 vet）只設定了 RawAnnotations 而未執行 ResolveProgram，
// 若只查 Annotations 會漏掉合法的 #{overflow} 註解，誤報未處理的溢出 option。
func stmtOverflowAnnotated(sem *parser.SemanticContext, stmt parser.Statement) bool {
	hasOverflow := func(entries []*parser.AnnotationEntry) bool {
		for _, e := range entries {
			if e != nil && e.Key == "overflow" {
				return true
			}
		}
		return false
	}
	if sem != nil {
		if hasOverflow(sem.AnnotationsOf(stmt)) || hasOverflow(sem.RawAnnotationsOf(stmt)) {
			return true
		}
	}
	switch s := stmt.(type) {
	case *parser.ForStatement:
		return s.OverflowMode != ""
	case *parser.ExpressionStatement:
		return s.OverflowMode != ""
	case *parser.LetStatement:
		return s.OverflowMode != ""
	case *parser.ReturnStatement:
		return s.OverflowMode != ""
	case *parser.MultiAssignStatement:
		return s.OverflowMode != ""
	}
	return false
}

// isIntType 報告型別名是否屬整數家族（有號 / 無號 / byte）。這些型別的 + - * / 在
// 溢出時預設回傳 option<int>；非整數（str / char / bool / f64 / ptr / fn 等）的
// 同名運算子（如 str 的 `-` 是字串拼接）不回傳 option，不應被本規則提示。
func isIntType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "byte", "int":
		return true
	}
	return false
}

// isDirectOverflowValue 報告表達式 v 的「結果本質」是否是一個未標註的整數溢出運算
// （+ - * /，運算元皆為整數）。即：v 去掉分組/括號包裹後，頂層是這類 InfixExpression，
// 且左右運算元經型別推斷皆為整數。若任一運算元確定為非整數（如 str 的 `-` 是字串
// 拼接），則回傳 false——那些語境下不會產生 option，誤報會污染 str.no / txt.no 等。
// varTypes 為當前函式作用域內「具型別標註的區域變數 / 參數」對照表；selfType 為方法
// 的接收者型別（用於 self.x 成員型別推斷）。兩者皆可能為空（未知變數保守視為整數）。
func isDirectOverflowValue(v parser.Expression, varTypes map[string]string, selfType string) bool {
	if v == nil {
		return false
	}
	if g, ok := v.(*parser.GroupedExpression); ok {
		return isDirectOverflowValue(g.Expression, varTypes, selfType)
	}
	inf, ok := v.(*parser.InfixExpression)
	if !ok {
		return false
	}
	if !overflowArithOps[inf.Operator] {
		return false
	}
	// 運算元必須都是整數（含未知型別，保守視為整數）；任一確定非整數則跳過。
	if !isIntExpr(inf.Left, varTypes, selfType) || !isIntExpr(inf.Right, varTypes, selfType) {
		return false
	}
	// 無號除法永不溢出：僅有號（含未知型別）一側才提示。
	if inf.Operator == "/" {
		lk := operandIntKind(inf.Left, varTypes)
		rk := operandIntKind(inf.Right, varTypes)
		if lk != "signed" && rk != "signed" {
			return false
		}
	}
	// i128 因 %option 無法容納，codegen 退化為回繞（普通 int），不產生 option。
	if inferExprType(inf.Left, varTypes, nil, selfType) == "i128" ||
		inferExprType(inf.Right, varTypes, nil, selfType) == "i128" {
		return false
	}
	return true
}

// looseOperandIsInt 是 relevant 集合（StatementsWithIntOverflow）专用的宽口径分类：
// 在 isIntExpr 之外，额外把「泛型型别参数」视为整数。动机：`[]t.sum` 里的
// `acc = acc + .[i]`，`.[i]` 推断为元素型别 "t"（型别参数名），isIntType("t")
// 回 false 会被当成「确定非整数」，导致管辖它的 #{overflow} 註解被 `no fmt`
// 误删（单态化为 i64 vec 后该运算确实可能溢出，wrap 是作者显式选择的语义）。
// relevant 集合是超集语义（宁可多保留註解，绝不误删），故只在「确定非整数」
// （内建非整型 / 容器 / option / 限定名 / 已知 struct / 具体型别别名且非整族）
// 时回 false；其余无法归类的裸标识符（型别参数 / 未解析别名）一律保守回 true。
// 仅供 relevant 计算使用，不影响编译期硬错误（ValidateUnhandledOverflow 仍用
// 原口径的 isIntExpr）。
func looseOperandIsInt(e parser.Expression, declared map[string]string, selfType string) bool {
	t := inferExprType(e, declared, nil, selfType)
	if t == "" {
		return true // 推断失败：保守视为整数（与 isIntExpr 一致）
	}
	if isIntType(t) {
		return true
	}
	if nonIntTypeNames[t] {
		return false
	}
	// 容器 / option / 函数型别 / 限定名 / 联合：非整数家族（与原口径一致）。
	if strings.ContainsAny(t, "[]?().| ") {
		return false
	}
	// 已知 struct：struct 算术不产生 option。
	if validationStructFields != nil {
		if _, ok := validationStructFields[t]; ok {
			return false
		}
	}
	if at, ok := validationConcreteTypeAliases[t]; ok {
		return isIntType(at)
	}
	// 裸未知标识符：视为泛型型别参数 → 保守整数。
	return true
}

// isIntExpr 推斷表達式 e 是否為整數型別（用於判定 + - * / 是否會產生 option<int>）。
// 確定型別為整數家族 → true；確定非整數（str / char / bool / f64 / ptr / fn 等）→ false；
// 型別未知（無標註的區域變數 / 動態呼叫）→ 保守傳回 true（視為整數，寧可多報，
// 因為未標註整數運算預設就是 option，多報可由使用者加註解消除；而漏報會沉默泄漏）。
func isIntExpr(e parser.Expression, varTypes map[string]string, selfType string) bool {
	t := inferExprType(e, varTypes, nil, selfType)
	if t == "" {
		return true // 未知型別：保守視為整數
	}
	return isIntType(t)
}

// ValidateUnhandledOverflow 報告所有「可能溢出但未被處理」的整數運算，作為編譯
// 硬錯誤（亦由 RunAllLints 納入 no vet / LSP 診斷）。
func ValidateUnhandledOverflow(program *parser.Program, mainFile string) []ValidateResult {
	if program == nil {
		return nil
	}
	sem := program.Sem
	var results []ValidateResult

	// 同位置重複結果去重（no vet 會對同一模組做多次校驗傳遞，避免同一處被
	// 報告多次；同時防禦性過濾 walk 中可能的重複訪問）。
	seen := map[string]bool{}

	// 本檢查對使用者程式碼與標準函式庫（std）一體適用：merged/lowered 路徑下 std
	// 的型別歸因已透過「節點欄位攜帶 OverflowMode + 解析期 side-table」修正為可靠，
	// 故不再對 std 設豁免。std 自身的沉默泄漏已以 `#{overflow=wrap}` 逐站修復（見
	// src/std 各模組），撤豁免後 no build / no run / no vet 對 std 與使用者碼同標準。
	//
	// curFile 追蹤「當前所处函式來源檔」，取值依序為：陳述自身的 SourceFile
	// （模組合併後由 transpiler 填入）→ 外層傳入的 curFile → mainFile（目前
	// 被編譯/檢查的主檔案）。三層回退不可少：單檔 vet / LSP 路徑下主程式自身
	// 的陳述其 SourceFile 為空（見 ast.go CommentedNode 註解），若無 mainFile
	// 回退，直接 vet std/str.no 之類的標準庫檔案就會漏報。
	//
	// 合併程式中同一份 std 原始檔可能因載入路徑不同而帶兩種 SourceFile 字串
	// （如嵌入的絕對路徑 `/.../src/std/str.no` 與工作區磁碟的相對路徑
	// `src/std/str.no`），導致同一處被重複計入。去重鍵對路徑做 filepath.Abs
	// 正規化，使兩種形式歸併為同一處。

	// canonPath 正規化 SourceFile 以便跨載入路徑去重（見上）。
	canonPath := func(f string) string {
		if f == "" {
			return ""
		}
		// 標準庫同一份原始檔可能因載入路徑不同而帶兩種 SourceFile 字串
		// （嵌入的絕對路徑 /.../src/std/X.no 與工作區磁碟的 std/X.no /
		// src/std/X.no 等），統一為「std/X.no」模組相對形式以便去重。
		if i := strings.Index(f, "/std/"); i >= 0 {
			return "std" + f[i+len("/std/"):]
		}
		if strings.HasPrefix(f, "std/") {
			return f
		}
		if a, err := filepath.Abs(f); err == nil {
			return a
		}
		return f
	}

	walkStmt := func(stmt parser.Statement, fnReturnsOption, enclosingOverflow bool, varTypes map[string]string, selfType, curFile string) {
	}
	// valueCtx 標記該表達式處於「值上下文」（綁定右值 / 回傳值 / 呼叫實參），
	// 而非「陳述上下文」（值被丟棄）。match 作表達式使用時
	// （`result = x: { 2 -> 2 + 1 }`），臂末表達式就是臂的值，不是被丟棄的
	// 表達式陳述；若一律當丟棄處理會對這種寫法誤報「未處理溢出」。
	var walkExpr func(e parser.Expression, fnReturnsOption, enclosingOverflow, valueCtx bool, varTypes map[string]string, selfType, curFile string)

	// seedVarTypes 由函式參數（含方法 self 接收者）建立區域型別對照表，
	// 供 isDirectOverflowValue 判斷運算元是否為整數（排除 str - str 等字串拼接）。
	seedVarTypes := func(params []*parser.Parameter, isMethod bool) (map[string]string, string) {
		vt := map[string]string{}
		st := ""
		for i, p := range params {
			if p == nil || p.Name == "" || p.Type == nil {
				continue
			}
			vt[p.Name] = p.Type.String()
			if isMethod && i == 0 {
				st = p.Type.String()
			}
		}
		return vt, st
	}

	report := func(stmt parser.Statement, msg string, curFile string) {
		// key 必須含 curFile（正規化後）：合併後的 program 彙集了多個模組的陳述，
		// 不同檔案完全可能出現相同的 (line, col)，若不含檔案會互相吃掉而漏報；
		// 正規化則使同一檔案的絕對/相對兩種 SourceFile 歸併為同一處。
		key := fmt.Sprintf("%s|%d:%d:%s", canonPath(curFile), stmt.Pos().Line, stmt.Pos().Column, msg)
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, ValidateResult{
			Line:    stmt.Pos().Line,
			Column:  stmt.Pos().Column,
			Message: msg,
			TraceID: unhandledOverflowTraceID,
		})
	}

	walkExpr = func(e parser.Expression, fnReturnsOption, enclosingOverflow, valueCtx bool, varTypes map[string]string, selfType, curFile string) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.IfExpression:
			// 條件 if / match 解構：遞迴其分支體。enclosingOverflow 透過本參數向臂體
			// 內陳述傳遞——若外層陳述已被 #{overflow=...} 註解，臂體內的整數運算
			// 一併視為已處理（否則 `cond -> body` 臂體內的 `cp = cp + 1` 會被孤立上報）。
			// walkArm 逐條走臂體；在值上下文下，臂的最後一條「表達式陳述」即為
			// 該臂的值（不是被丟棄），跳過以免誤報。
			walkArm := func(stmts []parser.Statement) {
				for i, b := range stmts {
					if valueCtx && i == len(stmts)-1 {
						if _, isExprStmt := b.(*parser.ExpressionStatement); isExprStmt {
							continue
						}
					}
					walkStmt(b, fnReturnsOption, enclosingOverflow, varTypes, selfType, curFile)
				}
			}
			if x.Consequence != nil {
				walkArm(x.Consequence.Statements)
			}
			if x.Alternative != nil {
				walkArm(x.Alternative.Statements)
			}
		case *parser.GroupedExpression:
			walkExpr(x.Expression, fnReturnsOption, enclosingOverflow, valueCtx, varTypes, selfType, curFile)
		case *parser.PrefixExpression:
			walkExpr(x.Right, fnReturnsOption, enclosingOverflow, valueCtx, varTypes, selfType, curFile)
		}
	}

	walkStmt = func(stmt parser.Statement, fnReturnsOption, enclosingOverflow bool, varTypes map[string]string, selfType, curFile string) {
		if stmt == nil {
			return
		}
		// 若本陳述自身已被 #{overflow=...} 註解，或其外層陳述（如緊鄰前置註解的
		// `cond -> body` 條件陳述）已被註解，則其整棵子樹（含 if/match 臂體、迴圈
		// 體內的整數運算）視同已處理，不再逐條陳述上報「未處理溢出」。這讓「行注解
		// 只作用下一條陳述」的語意正確涵蓋 `cond -> body` 這類單條件陳述內的整數
		// 運算，也使 275 個既有行注解能真正作用於其分支體內的溢出处。
		effOverflow := enclosingOverflow || stmtOverflowAnnotated(sem, stmt)
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			fnOpt := false
			// declaredResults drops the implicit `self` receiver of methods, so
			// rs[0] is the real return type.
			if rs := declaredResults(s); len(rs) > 0 && rs[0] != nil && rs[0].Type != nil {
				if strings.HasPrefix(rs[0].Type.String(), "?") {
					fnOpt = true
				}
			}
			cf := s.SourceFile
			if cf == "" {
				cf = curFile
			}
			if cf == "" {
				cf = mainFile
			}
			vt, st := seedVarTypes(s.Parameters, s.IsMethodDef)
			if s.Body != nil {
				// 函式體是獨立作用域，只受其自身註解約束；不繼承外層 enclosingOverflow，
				// 以免外層註解靜默掩蓋巢狀函式體內的真實泄漏。
				for _, b := range s.Body.Statements {
					walkStmt(b, fnOpt, false, vt, st, cf)
				}
			}
			return
		case *parser.LetStatement:
			// `name = (...) (...) {}` 方法定義：遞迴其函式體，自身非運算綁定。
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok {
				fnOpt := false
				if len(fl.Results) > 0 && fl.Results[0] != nil && fl.Results[0].Type != nil {
					if strings.HasPrefix(fl.Results[0].Type.String(), "?") {
						fnOpt = true
					}
				}
				cf := s.SourceFile
				if cf == "" {
					cf = curFile
				}
				if cf == "" {
					cf = mainFile
				}
				vt, st := seedVarTypes(fl.Parameters, false)
				if fl.Body != nil {
					for _, b := range fl.Body.Statements {
						walkStmt(b, fnOpt, false, vt, st, cf)
					}
				}
				return
			}
			// 具型別標註的綁定就地登記進 varTypes（順序語句共享同一 map），使後續
			// 語句能據此判斷運算元型別（如 r str = ... 之後的 r - seg 才會被排除
			// 為字串拼接而非誤報整數溢出）。
			if s.Type != nil && s.Name != nil {
				varTypes[s.Name.Value] = s.Type.String()
			} else if s.Type == nil && s.Name != nil && s.Value != nil {
				// 未標註型別的綁定（`a = 'foo'`）也由初值推斷並登記：否則後續
				// 陳述中該變數型別「未知」會被保守視為整數，把字串拼接
				// （`c = a - b`，str 的 `-` 是拼接）誤報成未處理的整數溢出。
				// 推斷不出型別（如跨模組呼叫結果）時保持未知，仍走保守路徑。
				if t := inferExprType(s.Value, varTypes, nil, selfType); t != "" {
					varTypes[s.Name.Value] = t
				}
			}
			// 普通 `=`：未被註解處理且 LHS 未顯式宣告 ?T，且其結果本質是未標註溢出
			// 運算 → 沉默泄漏。函式回傳 ?T 時，區域 option 中間值是合約內預期行為 → 不報。
			//
			// LHS 為 `_`（`_ = expr`）是顯式捨棄：使用者已表明不要這個值與其錯誤，
			// 求值本身在執行期是安全的（捕獲推斷會讓除零/越界走 option 路徑，不 UB），
			// 故豁免。這與「裸表達式陳述」（無 `_ =`，下方 ExpressionStatement 分支）
			// 不同：後者多半是誤寫，仍報錯。
			if !fnReturnsOption && !effOverflow {
				declaredOption := s.Type != nil && strings.HasPrefix(s.Type.String(), "?")
				discarded := s.Name != nil && s.Name.Value == "_"
				if !declaredOption && !discarded && isDirectOverflowValue(s.Value, varTypes, selfType) {
					report(s, "整数运算 `a OP b` 默认在溢出时返回 option<int>。此 option 未被处理：请加 `#{overflow = wrap}` 注解回普通 int，或用 `?=` 上抛（如 `x ?= a + b`），或显式声明为 option 类型（如 `x ?i64 = a + b`）以就地捕获错误，或用 `_ = a + b` 显式丢弃。", curFile)
				}
			}
			if s.Value != nil {
				walkExpr(s.Value, fnReturnsOption, effOverflow, true, varTypes, selfType, curFile)
			}
		case *parser.ReturnStatement:
			if !effOverflow {
				// 函式回傳 ?T 時，回傳 option 是合約內預期行為 → 不報。
				if isDirectOverflowValue(s.ReturnValue, varTypes, selfType) && !fnReturnsOption {
					report(s, "返回的整数运算默认返回 option<int>，但本函数不返回 option 类型。请加 `#{overflow = wrap}` 注解回普通 int，或让函数返回 ?T 并用 `result ?= expr` 上抛（或用 `result = expr` 就地捕获错误）。", curFile)
				}
			}
			if s.ReturnValue != nil {
				walkExpr(s.ReturnValue, fnReturnsOption, effOverflow, true, varTypes, selfType, curFile)
			}
		case *parser.ExpressionStatement:
			if !fnReturnsOption && !effOverflow {
				if isDirectOverflowValue(s.Expression, varTypes, selfType) {
					report(s, "整数运算结果默认是 option<int>，作为表达式语句被丢弃（未处理）。请加 `#{overflow = wrap}` 注解回普通 int，或用 `?=` 上抛，或用 `_ = expr` 显式丢弃。", curFile)
				}
			}
			if s.Expression != nil {
				walkExpr(s.Expression, fnReturnsOption, effOverflow, false, varTypes, selfType, curFile)
			}
		case *parser.UnwrapAssignStatement:
			// `?=` 上拋：已處理，跳過；但仍遞迴其體內巢狀陳述。
			if s.Value != nil {
				walkExpr(s.Value, fnReturnsOption, effOverflow, true, varTypes, selfType, curFile)
			}
		case *parser.ForStatement:
			// for 的 init 可能綁定迴圈變數（如 i i64 = 0），登記後續可據此判斷型別。
			if s.Init != nil {
				if ls, ok := s.Init.(*parser.LetStatement); ok && ls.Type != nil && ls.Name != nil {
					varTypes[ls.Name.Value] = ls.Type.String()
				}
				walkStmt(s.Init, fnReturnsOption, effOverflow, varTypes, selfType, curFile)
			}
			// 迭代變數（`for ch <- s`）同樣要登記：字串迭代的元素是 char，
			// 不登記其型別未知 → 保守當成整數 → `ch - 32` 被誤報為未處理的
			// 整數溢位 [ovfhndld]。char 的算術不產生 option，見 iterElemType。
			if et := iterElemType(s.IterRange, varTypes, selfType); et != "" {
				varTypes[s.IterRange.Variable] = et
			}
			if s.Condition != nil {
				walkExpr(s.Condition, fnReturnsOption, effOverflow, true, varTypes, selfType, curFile)
			}
			if s.Update != nil {
				walkStmt(s.Update, fnReturnsOption, effOverflow, varTypes, selfType, curFile)
			}
			if s.Body != nil {
				for _, b := range s.Body.Statements {
					walkStmt(b, fnReturnsOption, effOverflow, varTypes, selfType, curFile)
				}
			}
		case *parser.BlockStatement:
			for _, b := range s.Statements {
				walkStmt(b, fnReturnsOption, effOverflow, varTypes, selfType, curFile)
			}
		case *parser.MultiAssignStatement:
			// a, b = f()：名稱型別由呼叫回傳值決定，難靜態得知，僅遞迴 RHS。
			if s.Value != nil {
				walkExpr(s.Value, fnReturnsOption, effOverflow, true, varTypes, selfType, curFile)
			}
		}
	}

	// 頂層順序陳述必須共享同一份 varTypes：此前每條頂層陳述都新建一個空 map，
	// 前一條陳述註冊的型別標註（如 `a f64 = arr[0]`）在後續陳述（`c f64 = a * b`）
	// 中不可見，運算元被當成「型別未知」而保守視為整數 → 對浮點/字串運算誤報
	// 「未處理整數溢出」（tmp-arr-test / test-str-concat 等）。函式體仍各自
	// 建立獨立作用域（seedVarTypes），不受影響。
	topTypes := map[string]string{}
	for _, stmt := range program.Statements {
		walkStmt(stmt, false, false, topTypes, "", mainFile)
	}
	return results
}

const unhandledIndexTraceID = "idxhndld"

// ValidateUnhandledIndex 報告所有「可能越界但未被處理」的 arr/vec/slice 索引
// （b[i]），作為編譯錯誤（亦由 RunAllLints 納入 no vet / LSP 診斷）。
//
// 背景：arr/vec/slice 的索引 b[i] 在越界時預設回傳 option<elem>（永不 panic），
// 與整數溢出同理。這個 option 若不被處理，就會「沉默泄漏」：變數被推斷為 option
// 但程式其實把它當普通 elem 用，運行期語意漂移（且 codegen 的 option 是
// {tag,data} 結構，直接當 elem 解引用會錯）。本規則在編譯期攔截這種「產生 option
// 卻沒處理」的寫法，迫使程式設計師顯式二選一：
//  1. 用 `a ?= b[i]` 上拋（錯誤傳給呼叫者）；在返回 ?T 的函式中 `a = b[i]`
//     會被 lowering 捕獲：`a` 推斷為 `?elem`，越界時就地存 `nil`（不提前 return）；
//  2. 加 `#{index-out = DEF}` 註解（DEF 為字面量），越界時取預設值 DEF。
//
// 與 parser 的 isSafeIndexBase / maybeIndexOutAssign
// 保持一致：str/txt 索引回傳字元（非 option，不報）；struct field 索引（receiver.field[i]）
// 走既有 bounds_check 路徑（不報）；只有直接變數基底的 arr/vec/slice 索引會產生
// option 並需被處理。
func ValidateUnhandledIndex(program *parser.Program, mainFile string) []ValidateResult {
	if program == nil {
		return nil
	}
	sem := program.Sem
	var results []ValidateResult
	seen := map[string]bool{}

	// 合併程式中同一份 std 原始檔可能因載入路徑不同而帶兩種 SourceFile 字串，
	// 去重鍵對路徑做 filepath.Abs 正規化（見 ValidateUnhandledOverflow）。
	canonPath := func(f string) string {
		if f == "" {
			return ""
		}
		if i := strings.Index(f, "/std/"); i >= 0 {
			return "std" + f[i+len("/std/"):]
		}
		if strings.HasPrefix(f, "std/") {
			return f
		}
		if a, err := filepath.Abs(f); err == nil {
			return a
		}
		return f
	}

	// lineHasIdentIndexRead 確認 file 的 line 行確實含一個「直接變數基底」索引讀取
	//（`name[i]`，name 為識別符 / `)` / `]`；排除切片型別 `[]T`、範圍 `[0..n)`、
	// `[?]`、以及 DotExpression 基底 `.[i]` / `.field[i]`）。合併 std 的 vet 會把
	// 跨模組索引誤歸因到錯誤檔案的某一行（該行實際無索引讀取，如方法呼叫、註解、
	// `}`），此過濾剔除這類誤報，只保留真正落在含索引讀取行上的診斷。
	srcLines := map[string][]string{}
	lineHasIdentIndexRead := func(file string, line int) bool {
		abs, err := filepath.Abs(file)
		if err != nil {
			abs = file
		}
		lines, ok := srcLines[abs]
		if !ok {
			data, e := os.ReadFile(abs)
			if e != nil {
				return true // 無法讀取則不過濾（保留診斷）
			}
			lines = strings.Split(string(data), "\n")
			srcLines[abs] = lines
		}
		if line < 1 || line > len(lines) {
			return false
		}
		return identIndexReadOnLine(lines[line-1])
	}

	report := func(idx *parser.IndexExpression, curFile string) {
		// 標準庫（std）不再豁免（自 2026-09-11 起，與 overflow 規則一致）。未處理
		// 越界索引必須逐站以 `#{index-out = DEF}` 或 `?=` 修復，使 `no vet` 與
		// 合併程式的 lint 對 std 保持乾淨。
		line, col := idx.Pos().Line, idx.Pos().Column
		// 剔除合併 std vet 的跨模組誤歸因：報告行必須確實含直接變數基底索引讀取。
		if !lineHasIdentIndexRead(curFile, line) {
			return
		}
		msg := "数组/切片索引 `arr[i]` 默认在越界时返回 option<elem>。此 option 未被处理：请用 `a ?= arr[i]` 上抛（在返回 ?T 的函数中可直接 `a = arr[i]` 就地捕获错误），或加 `#{index-out = DEF}` 注解以越界时取默认值 DEF，或显式声明为 option 类型（如 `a ?elem = arr[i]`）以就地捕获错误，或用 `_ = arr[i]` 显式丢弃。"
		key := fmt.Sprintf("%s|%d:%d", canonPath(curFile), line, col)
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, ValidateResult{
			Line:      line,
			Column:    col,
			EndColumn: idx.EndPos().Column,
			File:      curFile,
			Message:   msg,
			TraceID:   unhandledIndexTraceID,
		})
	}

	// isSafeBase 複製 parser.isSafeIndexBase 的判定（僅直接變數基底、arr/vec/slice）。
	isSafeBase := func(curFunc string, idx *parser.IndexExpression) bool {
		if idx == nil || idx.Left == nil {
			return false
		}
		ident, ok := idx.Left.(*parser.Identifier)
		if !ok {
			return false
		}
		// 合併 std vet 時，checker 的 curFunc 可能帶多餘的模組前綴
		//（如 "path.path.join"），而 IdxLocalTypes 的鍵是 lower 期記錄的
		// 單前綴名（"path.join"）。逐層去掉前綴嘗試，對齊鍵名。
		// 只查 IdxLocalTypes（lower 預掃描所得，權威且無跨函數污染）；
		// 不使用 FuncVarType / 全域 VarType 回退——那些在合併模式下會被
		// 同名參數（其他函數的 `b []byte`）污染，導致 str 索引 b[0] 被誤判。
		funcKeys := []string{curFunc}
		if i := strings.Index(curFunc, "."); i >= 0 {
			funcKeys = append(funcKeys, curFunc[i+1:])
		}
		lt := ""
		if sem != nil && sem.IdxLocalTypes != nil {
			for _, cf := range funcKeys {
				if t, ok := sem.IdxLocalTypes[cf][ident.Value]; ok && t != "" {
					lt = strings.TrimPrefix(t, "?")
					break
				}
			}
		}
		return parser.ContainerElemType(lt) != ""
	}

	// lhsIsOption 報告指派目標（既存變數）的靜態型別是否為 ?T：option 目標會
	// 自動接納越界 option（不報）。
	lhsIsOption := func(curFunc, name string) bool {
		if name == "" || sem == nil {
			return false
		}
		if t, ok := sem.FuncVarType(curFunc, name); ok && strings.HasPrefix(t, "?") {
			return true
		}
		if t, ok := sem.VarType(name); ok && strings.HasPrefix(t, "?") {
			return true
		}
		return false
	}

	// Pass 1：收錄「已處理」的索引表達式指標。
	//   - `a ?= b[i]`（UnwrapAssignStatement）
	//   - option 回傳函式內裸 `a = b[i]` 的就地捕獲（lowering 捕獲 pass）
	//   - `#{index-out = DEF}` 降級產生的合成 tmp：`__idx_out_L_C = b[i]`（IsSynthetic）
	handled := map[*parser.IndexExpression]bool{}

	var walkStmt func(stmt parser.Statement, curFunc string, fnOpt bool, curFile string)
	var walkExpr func(e parser.Expression, curFunc string, curFile string)

	walkExpr = func(e parser.Expression, curFunc, curFile string) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.UnwrapAssignStatement:
			if idx, ok := x.Value.(*parser.IndexExpression); ok {
				handled[idx] = true
			}
			if x.Value != nil {
				walkExpr(x.Value, curFunc, curFile)
			}
		case *parser.IfExpression:
			if x.Consequence != nil {
				for _, b := range x.Consequence.Statements {
					walkStmt(b, curFunc, false, curFile)
				}
			}
			if x.Alternative != nil {
				for _, b := range x.Alternative.Statements {
					walkStmt(b, curFunc, false, curFile)
				}
			}
		case *parser.GroupedExpression:
			walkExpr(x.Expression, curFunc, curFile)
		case *parser.PrefixExpression:
			walkExpr(x.Right, curFunc, curFile)
		}
	}

	walkStmt = func(stmt parser.Statement, curFunc string, fnOpt bool, curFile string) {
		if stmt == nil {
			return
		}
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			fcf := s.SourceFile
			if fcf == "" {
				fcf = curFile
			}
			if fcf == "" {
				fcf = mainFile
			}
			fnOpt = false
			// declaredResults drops the implicit `self` receiver of methods, so
			// rs[0] is the real return type.
			if rs := declaredResults(s); len(rs) > 0 && rs[0] != nil && rs[0].Type != nil {
				if strings.HasPrefix(rs[0].Type.String(), "?") {
					fnOpt = true
				}
			}
			if s.Body != nil {
				for _, b := range s.Body.Statements {
					walkStmt(b, s.Name, fnOpt, fcf)
				}
			}
			return
		case *parser.LetStatement:
			// `#{index-out}` 降級產生的合成 tmp：`__idx_out_L_C = b[i]`。
			if s.IsSynthetic && s.Name != nil && strings.HasPrefix(s.Name.Value, "__idx_out_") {
				if idx, ok := s.Value.(*parser.IndexExpression); ok {
					handled[idx] = true
				}
			}
			// 非合成 `x = b[i]`：若 LHS 顯式宣告 ?T（option 本地）則已接納
			// 越界 option；否則（非 option 函式，或結果參數非 ?T）越界 option
			// 未被處理 → 報錯。LHS 為 `_`（`_ = b[i]`）是顯式捨棄 → 豁免。
			if !s.IsSynthetic {
				if idx, ok := s.Value.(*parser.IndexExpression); ok {
					discarded := s.Name != nil && s.Name.Value == "_"
					if isSafeBase(curFunc, idx) && !discarded && !(s.Type != nil && strings.HasPrefix(s.Type.String(), "?")) {
						cf := s.SourceFile
						if cf == "" {
							cf = curFile
						}
						if cf == "" {
							cf = mainFile
						}
						report(idx, cf)
					}
				}
			}
			if s.Value != nil {
				walkExpr(s.Value, curFunc, curFile)
			}
		case *parser.ExpressionStatement:
			// `b = arr[i]`（既有變數的裸賦值）以 ExpressionStatement 包裹
			// AssignExpression 出現：LHS 為 ?T 或處於 option 函式（codegen 自動
			// wrap）時已接納；否則越界 option 未被處理 → 報錯。LHS 為 `_` 時豁免。
			if ae, ok := s.Expression.(*parser.AssignExpression); ok {
				if idx, ok := ae.Value.(*parser.IndexExpression); ok {
					if isSafeBase(curFunc, idx) {
						handledByType := false
						if id, ok := ae.Left.(*parser.Identifier); ok {
							handledByType = id.Value == "_" || lhsIsOption(curFunc, id.Value)
						}
						if !handledByType && !fnOpt {
							cf := s.SourceFile
							if cf == "" {
								cf = curFile
							}
							if cf == "" {
								cf = mainFile
							}
							report(idx, cf)
						}
					}
				}
			}
			if s.Expression != nil {
				walkExpr(s.Expression, curFunc, curFile)
			}
		case *parser.UnwrapAssignStatement:
			if idx, ok := s.Value.(*parser.IndexExpression); ok {
				handled[idx] = true
			}
			if s.Value != nil {
				walkExpr(s.Value, curFunc, curFile)
			}
		case *parser.ForStatement:
			if s.Init != nil {
				walkStmt(s.Init, curFunc, fnOpt, curFile)
			}
			if s.Condition != nil {
				walkExpr(s.Condition, curFunc, curFile)
			}
			if s.Update != nil {
				walkStmt(s.Update, curFunc, fnOpt, curFile)
			}
			if s.Body != nil {
				for _, b := range s.Body.Statements {
					walkStmt(b, curFunc, fnOpt, curFile)
				}
			}
		case *parser.BlockStatement:
			for _, b := range s.Statements {
				walkStmt(b, curFunc, fnOpt, curFile)
			}
		case *parser.MultiAssignStatement:
			if s.Value != nil {
				walkExpr(s.Value, curFunc, curFile)
			}
		}
	}

	for _, stmt := range program.Statements {
		walkStmt(stmt, "", false, mainFile)
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
					TraceID: "a3yrogp1",
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

// ─────────────────────────────────────────────────────────────
// s[i] 性能告警（str/txt 码点下标 O(n)）
// ─────────────────────────────────────────────────────────────

// ValidateStrIndexComplexity 针对 str/txt 的 s[i] 码点下标给出性能告警。
//
// nolang 中 s[i] 的语义是「第 i 个码点（字符）」：当编译器无法证明 s 为纯 ASCII
// （0-127）时，每次下标都必须从串首前向迭代 UTF-8 才能定位第 i 个码点，时间复杂度
// O(n)。只有当 s 可被证明为纯 ASCII（字节数 == 码点数）时，下标退化为一次直接定址 O(1)。
//
// 本 lint 不禁止语法（warning 而非 error），仅提示：
//   - 若该串确实只会是 ASCII，声明时加 #{ascii} 或赋值为 ASCII 字面量即可获 O(1)；
//   - 若实际需要的是「第 i 个字节」，请用 s.byte(i)（永远 O(1)，供 UTF-8 编解码等使用）；
//   - 若需要遍历每个字符，直接 for c in s 即可（s[i] 本身已是码点）。
//
// 与 build/llvm/strchar_at.go 的 collectAsciiVars 证明规则保持一致，
// 使告警与实际生成的 O(1)/O(n) 路径一致。
// ValidateStrIndexComplexity 检查「在 for 循环体内对未证明为纯 ASCII 的 str/txt
// 使用 s[i] 下标」这一 O(n²) 反模式，给出 WARNING 级静态告警（不禁止语法）。
//
// 设计要点：
//   - 仅在循环体内告警。单独一次 s[i]（如 s[5]）只是 O(n)，不是性能灾难；真正反模式
//     是手动按码点下标遍历（for i <- [0..s.count()): { s[i] }），每次下标都从串首前向
//     迭代 UTF-8，整体退化为 O(n²)。
//   - 用节点级 file（从顶层语句的 SourceFile 继承、进入函数体时更新为该函数 SourceFile）
//     判定是否位于标准库 src/std/*，从而跳过 std 内部代码。合併模式下 std 与用户代码
//     行号都以 1 为基准相互重叠，行号→文件单映射会误判，故一律走节点级判定。
//   - 与 build/llvm/strchar_at.go 的 collectAsciiVars 证明规则保持一致，使告警与实际
//     生成的 O(1)/O(n) 路径一致：可证明 ASCII 时 s[i] 为 O(1)，不告警。
func ValidateStrIndexComplexity(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	if program == nil {
		return nil
	}
	sem := program.Sem

	// 第一遍：收集可证明为纯 ASCII 的变量名，规则与 codegen 一致；同时自建
	// 类型表（scopeTypes[funcName][varName]）与函数返回类型表。自建类型表避免依赖
	// SemanticContext.VarTypes / FuncVarTypes —— 那些 map 会被全程序（含 std）污染，
	// 在 no vet 合併模式下查到的类型不可靠。
	asciiVars := map[string]bool{}
	scopeTypes := map[string]map[string]string{}
	funcReturns := map[string]string{}
	collectStrIndexTypes(program.Statements, sem, "", scopeTypes, funcReturns, asciiVars)

	for _, stmt := range program.Statements {
		results = append(results, checkStrIndexInStmt(stmt, sem, asciiVars, scopeTypes, "", false, parser.GetSourceFile(stmt))...)
	}
	return results
}

// collectStrIndexAscii 收集可证明为纯 ASCII 的字符串变量，规则镜像 codegen 的
// collectAsciiVars：显式 #{ascii} 注解、字符串字面量、标识符传播、- 拼接两侧均 ASCII。
// funcName 用于 asciiVars 的作用域限定键（见 strIndexAsciiKey）。
func collectStrIndexAscii(stmts []parser.Statement, sem *parser.SemanticContext, asciiVars map[string]bool, funcName string) {
	for _, s := range stmts {
		switch st := s.(type) {
		case *parser.LetStatement:
			noteStrIndexASCIILet(st, sem, asciiVars, funcName)
			scanStrIndexExpr(st.Value, sem, asciiVars, funcName)
		case *parser.FunctionDefinition:
			if st.Body != nil {
				collectStrIndexAscii(st.Body.Statements, sem, asciiVars, st.Name)
			}
		case *parser.BlockStatement:
			collectStrIndexAscii(st.Statements, sem, asciiVars, funcName)
		case *parser.ExpressionStatement:
			scanStrIndexExpr(st.Expression, sem, asciiVars, funcName)
		case *parser.ReturnStatement:
			scanStrIndexExpr(st.ReturnValue, sem, asciiVars, funcName)
		}
	}
}

// collectStrIndexTypes 自建类型表（scopeTypes）与函数返回类型表（funcReturns），
// 从 AST 直接抽取，绕过被全程序污染的 SemanticContext.VarTypes。
//   - 显式类型注解 let x: str = ... → "str"
//   - 字符串字面量 let x = '...' → "str"
//   - 标识符传播 let x = y → 沿用 y 的类型
//   - 函数调用 let x = f() → 沿用 f 的返回类型
//   - 函数参数与返回类型也一并记录
//
// 同时顺带收集 asciiVars（调用 noteStrIndexASCIILet / scanStrIndexExpr）。
func collectStrIndexTypes(stmts []parser.Statement, sem *parser.SemanticContext, funcName string, scopeTypes map[string]map[string]string, funcReturns map[string]string, asciiVars map[string]bool) {
	ensureScope := func(fn string) map[string]string {
		if scopeTypes[fn] == nil {
			scopeTypes[fn] = map[string]string{}
		}
		return scopeTypes[fn]
	}
	for _, s := range stmts {
		// std 檔案不參與收集。no vet 合併模式會把整個標準庫併入 program，而
		// asciiVars/scopeTypes 是以「變數名」為鍵（非作用域限定）。std 裡有大量
		// 名為 s / n / i / out / pos / val / target / limit … 的區域變數被賦予 ASCII
		// 字面量，一旦進入 asciiVars，使用者程式碼中的同名變數就會被誤判為
		// 「已證明純 ASCII」，使 STR_INDEX_COMPLEXITY 告警完全失效（實測 s 被 std 污染）。
		// std 本來也在回報端被 isStdSourceFile 跳過，此處提前排除可同時避免污染。
		if isStdSourceFile(parser.GetSourceFile(s)) {
			continue
		}
		switch st := s.(type) {
		case *parser.LetStatement:
			if st.Name != nil {
				scope := ensureScope(funcName)
				scope[st.Name.Value] = exprTypeString(st.Type, st.Value, scopeTypes, funcName, funcReturns)
			}
			noteStrIndexASCIILet(st, sem, asciiVars, funcName)
			scanStrIndexExpr(st.Value, sem, asciiVars, funcName)
		case *parser.FunctionDefinition:
			scope := ensureScope(st.Name)
			for _, p := range st.FuncSignature.Parameters {
				scope[p.Name] = typeNodeString(p.Type)
			}
			// declaredResults drops the implicit `self` receiver of methods.
			if rs := declaredResults(st); len(rs) > 0 {
				funcReturns[st.Name] = typeNodeString(rs[0].Type)
			}
			if st.Body != nil {
				collectStrIndexTypes(st.Body.Statements, sem, st.Name, scopeTypes, funcReturns, asciiVars)
			}
		case *parser.BlockStatement:
			collectStrIndexTypes(st.Statements, sem, funcName, scopeTypes, funcReturns, asciiVars)
		case *parser.ExpressionStatement:
			scanStrIndexExpr(st.Expression, sem, asciiVars, funcName)
		case *parser.ReturnStatement:
			scanStrIndexExpr(st.ReturnValue, sem, asciiVars, funcName)
		}
	}
}

// exprTypeString 推断一个 let 绑定的类型字符串。
func exprTypeString(typ parser.Type, val parser.Expression, scopeTypes map[string]map[string]string, funcName string, funcReturns map[string]string) string {
	if typ != nil {
		return typeNodeString(typ)
	}
	switch v := val.(type) {
	case *parser.StringLiteral:
		return "str"
	case *parser.Identifier:
		if t := lookupVarType(scopeTypes, funcName, v.Value); t != "" {
			return t
		}
	case *parser.CallExpression:
		if fname := callName(v.Function); fname != "" {
			if r, ok := funcReturns[fname]; ok {
				return r
			}
		}
	}
	return ""
}

// typeNodeString 将 AST 类型节点转为规范类型名（如 "str" / "txt"）。
func typeNodeString(t parser.Type) string {
	if t == nil {
		return ""
	}
	if nt, ok := t.(*parser.NamedType); ok {
		return nt.Value
	}
	return t.String()
}

// callName 提取调用表达式的函数名（Identifier 或 DotExpression 的 property）。
func callName(fn parser.Expression) string {
	switch f := fn.(type) {
	case *parser.Identifier:
		return f.Value
	case *parser.DotExpression:
		return f.Property
	}
	return ""
}

// lookupVarType 在自建类型表中按 (funcName, name) 查找变量类型，回退到全局作用域。
// 返回 "" 表示无法确定类型（调用方应跳过告警以避免误报）。
func lookupVarType(scopeTypes map[string]map[string]string, funcName, name string) string {
	if funcName != "" {
		if m, ok := scopeTypes[funcName]; ok {
			if t, ok := m[name]; ok {
				return t
			}
		}
	}
	if m, ok := scopeTypes[""]; ok {
		if t, ok := m[name]; ok {
			return t
		}
	}
	return ""
}

// strIndexAsciiKey 是 asciiVars 的作用域限定鍵。
//
// 「已證明純 ASCII」是**每個變數宣告**的性質，不是名稱的性質。原本 asciiVars 以裸
// 變數名為鍵，於是不同函式裡同名的區域變數（s / n / i / out / pos / val / target …）
// 會互相污染：只要任一處賦了 ASCII 字面量，其他所有同名變數都被誤判為「已證明
// ASCII」，使 STR_INDEX_COMPLEXITY 對這些名字**靜默失效**。以 funcName + NUL + name
// 為鍵即可消除同名跨作用域污染。頂層語句的 funcName 為 ""。
func strIndexAsciiKey(funcName, name string) string {
	return funcName + "\x00" + name
}

func noteStrIndexASCIILet(st *parser.LetStatement, sem *parser.SemanticContext, asciiVars map[string]bool, funcName string) {
	if st == nil || st.Name == nil {
		return
	}
	proven := hasStrIndexASCIIAnnotation(sem, st)
	if !proven {
		switch v := st.Value.(type) {
		case *parser.StringLiteral:
			proven = isASCIIString(v.Value)
		case *parser.Identifier:
			proven = asciiVars[strIndexAsciiKey(funcName, v.Value)]
		case *parser.InfixExpression:
			if v.Operator == "-" {
				l, lok := v.Left.(*parser.Identifier)
				r, rok := v.Right.(*parser.Identifier)
				if lok && rok {
					proven = asciiVars[strIndexAsciiKey(funcName, l.Value)] && asciiVars[strIndexAsciiKey(funcName, r.Value)]
				}
				if lr, ok := v.Left.(*parser.StringLiteral); ok {
					lok = isASCIIString(lr.Value)
				}
				if rr, ok := v.Right.(*parser.StringLiteral); ok {
					rok = isASCIIString(rr.Value)
				}
				if lok && rok {
					proven = true
				}
			}
		}
	}
	if proven {
		asciiVars[strIndexAsciiKey(funcName, st.Name.Value)] = true
	}
}

func hasStrIndexASCIIAnnotation(sem *parser.SemanticContext, n parser.Node) bool {
	if sem == nil || n == nil {
		return false
	}
	for _, e := range sem.AnnotationsOf(n) {
		if e != nil && e.Key == "ascii" {
			return true
		}
	}
	return false
}

func scanStrIndexExpr(e parser.Expression, sem *parser.SemanticContext, asciiVars map[string]bool, funcName string) {
	switch x := e.(type) {
	case *parser.IfExpression:
		if x.Consequence != nil {
			collectStrIndexAscii(x.Consequence.Statements, sem, asciiVars, funcName)
		}
		if x.Alternative != nil {
			collectStrIndexAscii(x.Alternative.Statements, sem, asciiVars, funcName)
		}
	case *parser.FunctionLiteral:
		if x.Body != nil {
			collectStrIndexAscii(x.Body.Statements, sem, asciiVars, funcName)
		}
	case *parser.CallExpression:
		scanStrIndexExpr(x.Function, sem, asciiVars, funcName)
		for _, a := range x.Arguments {
			scanStrIndexExpr(a, sem, asciiVars, funcName)
		}
	}
}

func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// isStrLikeType 报告 surface 类型是否为 str/txt 系列。
// 同时兼容未来 str.ascii / txt.ascii 声明类型（含 ascii 字样）。
func isStrLikeType(t string) bool {
	switch t {
	case "str", "txt":
		return true
	}
	return strings.Contains(t, "ascii")
}

// isStdSourceFile 报告路径是否属于标准库（src/std/*）。no vet 合併模式会把 std 语句
// 并入 program，为避免对 std 内部代码刷屏告警，遇到 std 文件直接跳过。
func isStdSourceFile(path string) bool {
	if path == "" {
		return false
	}
	clean := filepath.ToSlash(path)
	for _, seg := range strings.Split(clean, "/") {
		if seg == "std" {
			return true
		}
	}
	return false
}

// checkStrIndexInStmt 递归遍历语句，收集其中的 IndexExpression 进行告警判定。
// funcName 为当前所在函数名（用于 scopeTypes 解析函数局部变量类型），顶层为 ""。
// scopeTypes 为自建类型表（见 collectStrIndexTypes）；sem 仍保留以兼容调用方。
// inLoop 表示当前是否处于某个 for 循环体内部 —— 仅在循环体里对 str/txt 做 s[i]
// 下标才告警（即手动按码点下标遍历的 O(n²) 反模式），单独一次下标（如 s[5]）或
// 直接 for c <- s 遍历不告警。
// checkStrIndexInStmt 递归遍历语句，收集其中的 IndexExpression 进行告警判定。
// funcName 为当前所在函数名（用于 scopeTypes 解析函数局部变量类型），顶层为 ""。
// scopeTypes 为自建类型表（见 collectStrIndexTypes）；sem 仍保留以兼容调用方。
// inLoop 表示当前是否处于某个 for 循环体内部 —— 仅在循环体里对 str/txt 做 s[i]
// 下标才告警（即手动按码点下标遍历的 O(n²) 反模式），单独一次下标（如 s[5]）或
// 直接 for c <- s 遍历不告警。
// file 为当前节点所属来源文件（从顶层语句的 SourceFile 继承，进入函数体时更新为该
// 函数的 SourceFile）。合併模式下 std 与用户代码的行号都以 1 为基准相互重叠，无法用
// 「行号→文件」单映射区分，故改用节点级 file 判定 —— std 文件（src/std/*）直接跳过。
func checkStrIndexInStmt(stmt parser.Statement, sem *parser.SemanticContext, asciiVars map[string]bool, scopeTypes map[string]map[string]string, funcName string, inLoop bool, file string) []ValidateResult {
	var results []ValidateResult
	if stmt == nil {
		return results
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			results = append(results, checkStrIndexInExpr(s.Expression, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			results = append(results, checkStrIndexInExpr(s.Value, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.MultiAssignStatement:
		for _, t := range s.Targets {
			results = append(results, checkStrIndexInExpr(t, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
		if s.Value != nil {
			results = append(results, checkStrIndexInExpr(s.Value, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.UnwrapAssignStatement:
		if s.Value != nil {
			results = append(results, checkStrIndexInExpr(s.Value, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			results = append(results, checkStrIndexInExpr(s.ReturnValue, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.FunctionDefinition:
		// 函数体沿用该函数自身的来源文件（std 函数 → src/std/*，据此跳过其内部代码）。
		fnFile := parser.GetSourceFile(s)
		if fnFile == "" {
			fnFile = file
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				results = append(results, checkStrIndexInStmt(b, sem, asciiVars, scopeTypes, s.Name, false, fnFile)...)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			results = append(results, checkStrIndexInStmt(b, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.ForStatement:
		// 循环头（Init/Condition/Update）不算「遍历体」，沿用当前 inLoop；
		// 仅循环体进入 inLoop=true，使 s[i] 在循环体内才触发告警。
		if s.Init != nil {
			results = append(results, checkStrIndexInStmt(s.Init, sem, asciiVars, scopeTypes, funcName, false, file)...)
		}
		if s.Condition != nil {
			results = append(results, checkStrIndexInExpr(s.Condition, sem, asciiVars, scopeTypes, funcName, false, file)...)
		}
		if s.Update != nil {
			results = append(results, checkStrIndexInStmt(s.Update, sem, asciiVars, scopeTypes, funcName, false, file)...)
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				results = append(results, checkStrIndexInStmt(b, sem, asciiVars, scopeTypes, funcName, true, file)...)
			}
		}
	}
	return results
}

// checkStrIndexInExpr 递归遍历表达式，对循环体内 str/txt 的 s[i] 下标（未证明 ASCII）
// 发出告警 —— 即手动按码点下标遍历的 O(n²) 反模式。
// checkStrIndexInExpr 递归遍历表达式，对循环体内 str/txt 的 s[i] 下标（未证明 ASCII）
// 发出告警 —— 即手动按码点下标遍历的 O(n²) 反模式。
// funcName 含义同 checkStrIndexInStmt；scopeTypes 为自建类型表；file 为当前节点所属
// 来源文件（节点级，避开合併模式下 std/用户行号重叠）；inLoop 表示当前是否处于 for
// 循环体内部，仅此时才告警。
func checkStrIndexInExpr(e parser.Expression, sem *parser.SemanticContext, asciiVars map[string]bool, scopeTypes map[string]map[string]string, funcName string, inLoop bool, file string) []ValidateResult {
	var results []ValidateResult
	if e == nil {
		return results
	}
	switch x := e.(type) {
	case *parser.IndexExpression:
		if inLoop && !isStdSourceFile(file) {
			if id, ok := x.Left.(*parser.Identifier); ok {
				dt := lookupVarType(scopeTypes, funcName, id.Value)
				dok := dt != ""
				if dok && isStrLikeType(dt) && !asciiVars[strIndexAsciiKey(funcName, id.Value)] {
					results = append(results, ValidateResult{
						Line:    x.Token.Line,
						Column:  x.Token.Column,
						File:    file,
						Message: "在循环里对 str/txt 使用 s[i] 取下标为 O(n²)：每次下标都要从串首前向迭代 UTF-8 取第 i 个码点，循环会被重复执行。建议：(1) 若需遍历字符，直接用 c <- s（内部按码点迭代，O(n)）；(2) 若只需字节访问，用 s.byte(i)；(3) 若该串可证明为纯 ASCII（声明加 #{ascii} 或赋 ASCII 字面量），s[i] 为 O(1)。",
						TraceID: "STR_INDEX_COMPLEXITY",
					})
				}
			}
		}
		results = append(results, checkStrIndexInExpr(x.Left, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		results = append(results, checkStrIndexInExpr(x.Index, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
	case *parser.InfixExpression:
		results = append(results, checkStrIndexInExpr(x.Left, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		results = append(results, checkStrIndexInExpr(x.Right, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
	case *parser.PrefixExpression:
		results = append(results, checkStrIndexInExpr(x.Right, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
	case *parser.GroupedExpression:
		results = append(results, checkStrIndexInExpr(x.Expression, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
	case *parser.DotExpression:
		results = append(results, checkStrIndexInExpr(x.Receiver, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
	case *parser.AssignExpression:
		results = append(results, checkStrIndexInExpr(x.Left, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		results = append(results, checkStrIndexInExpr(x.Value, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
	case *parser.CallExpression:
		results = append(results, checkStrIndexInExpr(x.Function, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		for _, a := range x.Arguments {
			results = append(results, checkStrIndexInExpr(a, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	case *parser.IfExpression:
		if x.Condition != nil {
			results = append(results, checkStrIndexInExpr(x.Condition, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
		if x.Consequence != nil {
			for _, b := range x.Consequence.Statements {
				results = append(results, checkStrIndexInStmt(b, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
			}
		}
		if x.Alternative != nil {
			for _, b := range x.Alternative.Statements {
				results = append(results, checkStrIndexInStmt(b, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
			}
		}
	case *parser.FunctionLiteral:
		// 闭包体是独立作用域，不再视为「循环遍历体内」，重置 inLoop；
		// 闭包体是独立作用域，不再视为「循环遍历体内」，重置 inLoop；
		// 来源文件沿用外层 file（闭包无独立 SourceFile）。
		if x.Body != nil {
			for _, b := range x.Body.Statements {
				results = append(results, checkStrIndexInStmt(b, sem, asciiVars, scopeTypes, funcName, false, file)...)
			}
		}
	case *parser.ArrayLiteral:
		for _, el := range x.Elements {
			results = append(results, checkStrIndexInExpr(el, sem, asciiVars, scopeTypes, funcName, inLoop, file)...)
		}
	}
	return results
}

// ---------------------------------------------------------------------------
// 關係運算子（< <= > >=）的字串運算元校驗
// ---------------------------------------------------------------------------

// strOrderingOps 是 nolang「對字串沒有定義」的關係運算子。== / != 刻意不在此列：
// 它們由 runtime 的 @str_eq 實作真正的字串相等比較，多字元字串完全合法。
var strOrderingOps = map[string]bool{"<": true, "<=": true, ">": true, ">=": true}

// strOrderingRuneCount 回報字串字面量的碼點數。AST 的字面量在不同階段可能帶引號
// （'a' / "a"），統一先去掉引號再數。
func strOrderingRuneCount(v string) int {
	return len([]rune(strings.Trim(v, "'\"")))
}

// strOrderingBadOperand 回報運算元是否為關係運算子無法處理的字串。
//
// 合法（回 false）：
//   - 單字元字串字面量 'a'：隱式 char（碼點），hir2mir 會折成整數比較。
//   - char 字面量 "a"：本身就是碼點整數。
//   - 數值型別（i64 / f64 / byte / …）。
//   - 型別推斷不出來的運算元：保守放行（維持既有行為，由 LLVM 端把關）。
//
// 非法（回 true）：型別為 str / txt 的運算元 —— 多字元字面量、str 變數、回傳 str
// 的呼叫。nolang 沒有字串字典序比較，codegen 的 str 分支只有 @str_eq（相等），
// 關係運算子會退化成「恆假」。與其執行期靜默給錯答案，這裡在編譯期拒絕。
func strOrderingBadOperand(e parser.Expression, funcTypes, varTypes map[string]string, selfType string) bool {
	switch x := e.(type) {
	case nil:
		return false
	case *parser.GroupedExpression:
		return strOrderingBadOperand(x.Expression, funcTypes, varTypes, selfType)
	case *parser.StringLiteral:
		return strOrderingRuneCount(x.Value) != 1
	case *parser.CharLiteral:
		return false
	}
	return isStrLikeType(inferExprType(e, varTypes, funcTypes, selfType))
}

// strOrderingMessage 是關係運算子遇到多字元字串時回報的訊息。
const strOrderingMessage = "relational operator on a multi-character string: nolang has no lexicographic " +
	"string comparison, so the result would always be false. Compare single-character strings " +
	"('a', implicitly a char), chars (\"a\") or numbers; use == / != for whole strings."

// walkStrOrderingExpr 遞迴掃描表達式：對 < <= > >= 的可疑字串運算元回報錯誤，並在
// 遇到 if / 閉包時下潛其區塊。file 為當前節點所屬來源檔（節點級，避開合併模式下
// std 與使用者程式碼行號重疊）。
func walkStrOrderingExpr(e parser.Expression, funcTypes, varTypes map[string]string, selfType, file string, results *[]ValidateResult) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *parser.InfixExpression:
		if strOrderingOps[x.Operator] &&
			(strOrderingBadOperand(x.Left, funcTypes, varTypes, selfType) ||
				strOrderingBadOperand(x.Right, funcTypes, varTypes, selfType)) {
			*results = append(*results, ValidateResult{
				TraceID: "go7p9knv",
				Line:    x.Token.Line,
				Column:  x.Token.Column,
				File:    file,
				Message: strOrderingMessage,
			})
		}
		walkStrOrderingExpr(x.Left, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(x.Right, funcTypes, varTypes, selfType, file, results)
	case *parser.PrefixExpression:
		walkStrOrderingExpr(x.Right, funcTypes, varTypes, selfType, file, results)
	case *parser.GroupedExpression:
		walkStrOrderingExpr(x.Expression, funcTypes, varTypes, selfType, file, results)
	case *parser.DotExpression:
		walkStrOrderingExpr(x.Receiver, funcTypes, varTypes, selfType, file, results)
	case *parser.IndexExpression:
		walkStrOrderingExpr(x.Left, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(x.Index, funcTypes, varTypes, selfType, file, results)
	case *parser.SliceExpression:
		walkStrOrderingExpr(x.Left, funcTypes, varTypes, selfType, file, results)
		if x.Range != nil {
			walkStrOrderingExpr(x.Range.Start, funcTypes, varTypes, selfType, file, results)
			walkStrOrderingExpr(x.Range.End, funcTypes, varTypes, selfType, file, results)
		}
	case *parser.AssignExpression:
		walkStrOrderingExpr(x.Left, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(x.Value, funcTypes, varTypes, selfType, file, results)
	case *parser.CallExpression:
		walkStrOrderingExpr(x.Function, funcTypes, varTypes, selfType, file, results)
		for _, a := range x.Arguments {
			walkStrOrderingExpr(a, funcTypes, varTypes, selfType, file, results)
		}
	case *parser.ConditionalExpression:
		walkStrOrderingExpr(x.Condition, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(x.Consequence, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(x.Alternative, funcTypes, varTypes, selfType, file, results)
	case *parser.CastExpression:
		walkStrOrderingExpr(x.Expr, funcTypes, varTypes, selfType, file, results)
	case *parser.RunExpression:
		walkStrOrderingExpr(x.Call, funcTypes, varTypes, selfType, file, results)
	case *parser.AwaitExpression:
		walkStrOrderingExpr(x.Right, funcTypes, varTypes, selfType, file, results)
	case *parser.ArrayLiteral:
		walkStrOrderingExpr(x.Size, funcTypes, varTypes, selfType, file, results)
		for _, el := range x.Elements {
			walkStrOrderingExpr(el, funcTypes, varTypes, selfType, file, results)
		}
	case *parser.SliceLiteral:
		for _, el := range x.Elements {
			walkStrOrderingExpr(el, funcTypes, varTypes, selfType, file, results)
		}
	case *parser.MapLiteral:
		for _, p := range x.Pairs {
			walkStrOrderingExpr(p.Key, funcTypes, varTypes, selfType, file, results)
			walkStrOrderingExpr(p.Value, funcTypes, varTypes, selfType, file, results)
		}
	case *parser.StructLiteral:
		for _, f := range x.Fields {
			walkStrOrderingExpr(f.Value, funcTypes, varTypes, selfType, file, results)
		}
	case *parser.IfExpression:
		// 只走 Condition（match 臂的 desugar 結果已在其中），不走 EqualityPattern /
		// ValuePatterns / RawCond —— 它們與 Condition 共用同一批子運算式，會重複回報。
		walkStrOrderingExpr(x.Condition, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingBlock(x.Consequence, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingBlock(x.Alternative, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingBlock(x.DotValBody, funcTypes, varTypes, selfType, file, results)
	case *parser.FunctionLiteral:
		// 閉包體是獨立作用域：參數 + 自身 let 各自登記。
		local := make(map[string]string)
		for _, p := range x.Parameters {
			if p.Type != nil {
				local[p.Name] = typeNodeString(p.Type)
			}
		}
		for _, p := range x.Results {
			if p.Type != nil {
				local[p.Name] = typeNodeString(p.Type)
			}
		}
		if x.Body != nil {
			for _, b := range x.Body.Statements {
				walkStrOrderingStmt(b, funcTypes, local, selfType, file, results)
			}
		}
	}
}

// walkStrOrderingBlock 掃描一個區塊（可能為 nil）。
func walkStrOrderingBlock(b *parser.BlockStatement, funcTypes, varTypes map[string]string, selfType, file string, results *[]ValidateResult) {
	if b == nil {
		return
	}
	for _, stmt := range b.Statements {
		walkStrOrderingStmt(stmt, funcTypes, varTypes, selfType, file, results)
	}
}

// walkStrOrderingStmt 依序掃描語句，並把 let 綁定的型別登記進 varTypes（顯式標註
// 優先，其次由右值推斷），使後續語句裡的 `s < t` 能判定 s 是否為 str。
func walkStrOrderingStmt(stmt parser.Statement, funcTypes, varTypes map[string]string, selfType, file string, results *[]ValidateResult) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		walkStrOrderingExpr(s.Expression, funcTypes, varTypes, selfType, file, results)
	case *parser.LetStatement:
		// 編譯器合成語句（match 臂 `it` 綁定、`?=` 傳播賦值）不是使用者寫的，跳過。
		if s.IsSynthetic || s.IsPropagation {
			return
		}
		walkStrOrderingExpr(s.Value, funcTypes, varTypes, selfType, file, results)
		if s.Name != nil {
			t := typeNodeString(s.Type)
			if t == "" {
				t = inferExprType(s.Value, varTypes, funcTypes, selfType)
			}
			if t != "" {
				varTypes[s.Name.Value] = t
			}
		}
	case *parser.MultiAssignStatement:
		for _, tgt := range s.Targets {
			walkStrOrderingExpr(tgt, funcTypes, varTypes, selfType, file, results)
		}
		walkStrOrderingExpr(s.Value, funcTypes, varTypes, selfType, file, results)
	case *parser.UnwrapAssignStatement:
		walkStrOrderingExpr(s.Target, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(s.Value, funcTypes, varTypes, selfType, file, results)
	case *parser.ReturnStatement:
		walkStrOrderingExpr(s.ReturnValue, funcTypes, varTypes, selfType, file, results)
	case *parser.FunctionDefinition:
		// 跳過單態化副本（名稱含 `__`）與泛型模板：型別參數未特化，推斷不可靠。
		if strings.Contains(s.Name, "__") || len(s.GenericParams) > 0 || strings.HasPrefix(s.Name, "[") {
			return
		}
		fnFile := parser.GetSourceFile(s)
		if fnFile == "" {
			fnFile = file
		}
		local := make(map[string]string)
		for _, p := range s.Parameters {
			if p.Type != nil {
				local[p.Name] = typeNodeString(p.Type)
			}
		}
		for _, p := range s.Results {
			if p.Type != nil {
				local[p.Name] = typeNodeString(p.Type)
			}
		}
		bodySelf := selfType
		if len(s.Results) > 0 && s.Results[0].Name == "self" {
			bodySelf = typeNodeString(s.Results[0].Type)
		}
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				walkStrOrderingStmt(b, funcTypes, local, bodySelf, fnFile, results)
			}
		}
	case *parser.BlockStatement:
		walkStrOrderingBlock(s, funcTypes, varTypes, selfType, file, results)
	case *parser.ForStatement:
		walkStrOrderingStmt(s.Init, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(s.Condition, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingExpr(s.CountExpr, funcTypes, varTypes, selfType, file, results)
		walkStrOrderingStmt(s.Update, funcTypes, varTypes, selfType, file, results)
		if s.IterRange != nil {
			walkStrOrderingExpr(s.IterRange.RangeExpr, funcTypes, varTypes, selfType, file, results)
			if r := s.IterRange.Range; r != nil {
				walkStrOrderingExpr(r.Start, funcTypes, varTypes, selfType, file, results)
				walkStrOrderingExpr(r.End, funcTypes, varTypes, selfType, file, results)
			}
			// `for ch <- s` 的迭代變數是 char（見 iterElemType）。
			if et := iterElemType(s.IterRange, varTypes, selfType); et != "" {
				varTypes[s.IterRange.Variable] = et
			}
		}
		walkStrOrderingBlock(s.Body, funcTypes, varTypes, selfType, file, results)
	}
}

// collectStrOrderingFuncTypes 收集「回傳型別」表（使用者函式 + extern 宣告 +
// 常用 stdlib 方法），供 inferExprType 判定呼叫結果是否為 str。
func collectStrOrderingFuncTypes(program *parser.Program) map[string]string {
	funcTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if rs := declaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				funcTypes[fd.Name] = rs[0].Type.String()
			}
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			if len(es.Results) > 0 && es.Results[0].Type != nil {
				funcTypes[es.Name.Value] = es.Results[0].Type.String()
			}
		}
	}
	// std 方法定義在 src/std/*.no，校驗階段尚未合併，回傳型別先預填。
	for k, v := range map[string]string{
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
	} {
		if _, exists := funcTypes[k]; !exists {
			funcTypes[k] = v
		}
	}
	return funcTypes
}

// ValidateStrOrdering 檢查關係運算子（< <= > >=）的字串運算元。
//
// nolang 沒有字串的字典序比較：str 在 codegen 走的是 @str_eq（只做相等），關係
// 運算子會退化成「恆假」。規則：
//   - 單字元字串字面量 'a' → 合法，隱式視為 char（碼點）。
//   - 單字元字串字面量與 char（"a"）混合比較 → 合法，同樣按 char。
//   - 數值型別 → 合法。
//   - 其他 str / txt（多字元字面量、str 變數、回傳 str 的呼叫）→ 報錯。
func ValidateStrOrdering(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	funcTypes := collectStrOrderingFuncTypes(program)
	varTypes := make(map[string]string)
	var results []ValidateResult
	for _, stmt := range program.Statements {
		walkStrOrderingStmt(stmt, funcTypes, varTypes, "", parser.GetSourceFile(stmt), &results)
	}
	// 去重：一個 match 的區間臂 `['a'..'m')` 會被 desugar 成
	// `(s >= 'a') && (s < 'm')`，兩個 InfixExpression 落在同一行同一列，
	// 不去重會對同一個臂報兩條完全相同的錯誤。
	if len(results) < 2 {
		return results
	}
	seen := make(map[string]bool, len(results))
	deduped := results[:0]
	for _, r := range results {
		key := fmt.Sprintf("%d:%d:%s:%s", r.Line, r.Column, r.File, r.Message)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, r)
	}
	return deduped
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
				TraceID: "lkiy53ow",
				Line:    e.Token.Line,
				Column:  e.Token.Column,
				Message: fmt.Sprintf("hex literal '%s' uses uppercase; format will convert to lowercase (e.g. 0xff)", e.Token.Literal),
			})
		}
	case *parser.ByteLiteral:
		if hasUpperHex(e.Token.Literal) {
			results = append(results, ValidateResult{
				TraceID: "ucj09vyi",
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
	validationMu.Lock()
	defer validationMu.Unlock()
	var results []ValidateResult

	// 收集 struct 欄位型別資訊，用於解析結構欄位存取
	structFields := collectStructFields(program)

	// 構建函數返回類型映射，供 inferExprType 推導用戶定義函數呼叫的返回類型
	validationFuncTypes = make(map[string]string)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			// declaredResults drops the implicit `self` receiver of methods, so
			// rs[0] is the real return type (absent for void methods).
			if rs := declaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				validationFuncTypes[fd.Name] = rs[0].Type.String()
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
		"str.len-bytes":    "i64",
		"str.index":        "i64",
		"str.index-from":   "i64",
		"str.slice":        "str",
		"str.contains":     "bool",
		"str.starts-with":  "bool",
		"str.ends-with":    "bool",
		"str.to-upper":     "str",
		"str.to-lower":     "str",
		"str.trim":         "str",
		"str.trim-left":    "str",
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
			TraceID: "dpcwdbng",
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
				TraceID: "mzwb28br",
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
				TraceID: "xr3x7p5q",
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
	case 'b', 'c', 'd', 'o', 'p', 'x', 'X':
		// 整數類型（p = 指針地址，亦為整數）
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
	case 't':
		// 数据类型名：任何类型皆可（编译期输出类型名）
	case 'v':
		// 字面量值：任何类型皆可（按类型自动选择格式）
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
	// Route through the cached HIR arena: one lower, zero re-parse, and the
	// arena is reused across every export query for the same module. This is the
	// HIR migration's stage-2 redirect of the module-export collection that
	// ValidateUndefinedVars performs on every build. Falls back to the legacy
	// AST walk if the HIR path cannot produce a result.
	if pkg := stdHirForSource(source); pkg != nil {
		var exports []ModuleExport
		for _, e := range hir.ExportedSymbols(pkg) {
			exports = append(exports, ModuleExport{Name: e.Name, Value: e.Value, Type: e.Type})
		}
		return exports
	}
	return parseModuleExportsFromSourceAST(source)
}

// parseModuleExportsFromSourceAST is the legacy AST-walk implementation of
// parseModuleExportsFromSource, retained as a fallback.
func parseModuleExportsFromSourceAST(source []byte) []ModuleExport {
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
			// 內建樁函式（#{buildin=...}）僅為宣告，真實實作位於 Go runtime，
			// 不應作為模組匯出符號（否則會改變內建解析路徑）。
			if fd.BuiltinStub {
				continue
			}
			exports = append(exports, ModuleExport{Name: fd.Name, Value: ""})
		}
		if es, ok := stmt.(*parser.ExternStatement); ok && es.Name != nil {
			// Skip private FFI declarations (underscore-prefixed)
			if strings.HasPrefix(es.Name.Value, "_") {
				continue
			}
			exports = append(exports, ModuleExport{Name: es.Name.Value, Value: ""})
		}
		// Collect enum variant names as exported constants so that
		// ValidateUndefinedVars recognizes cross-module enum variants
		// (e.g. file-mode.read-write from fs.no).
		if ed, ok := stmt.(*parser.EnumDefinition); ok {
			for _, v := range ed.Values {
				exports = append(exports, ModuleExport{Name: v.Name, Value: ""})
			}
		}
		if ted, ok := stmt.(*parser.TaggedEnumDefinition); ok {
			for _, v := range ted.Variants {
				exports = append(exports, ModuleExport{Name: v.Name, Value: ""})
			}
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
	//    - "hmac"   → FullPath: "crypto/hmac" → std/crypto/hmac.no
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
			// Module-prefixed names (e.g. "number.gcd"): check the last
			// segment — LetStatement function assignments like
			// `gcd = (a int, b int) (r int) { ... }` are collected by
			// CollectDefinedVars under the bare name ("gcd"), but
			// ResolveModuleCalls rewrites call sites to "number.gcd".
			if idx := strings.LastIndex(e.Value, "."); idx > 0 {
				shortName := e.Value[idx+1:]
				if definedVars[shortName] || funcNames[shortName] {
					return nil
				}
				// For method calls with module prefix (e.g.
				// "sqlite.db-sqlite.query"), also try the last two
				// segments ("db-sqlite.query") as the method name,
				// since methods are registered as "Type.method".
				if idx2 := strings.LastIndex(e.Value[:idx], "."); idx2 >= 0 {
					twoSeg := e.Value[idx2+1:]
					if definedVars[twoSeg] || funcNames[twoSeg] {
						return nil
					}
				}
				// If the first segment is a known imported module name
				// (e.g. "sqlite" in "sqlite._sqlite3-open"), skip the
				// check. vet cannot resolve internal symbols (FFI
				// functions, constants) from externally imported modules
				// that are filtered by lib.no export declarations.
				firstSeg := e.Value[:idx]
				if idx2 := strings.Index(firstSeg, "."); idx2 > 0 {
					firstSeg = firstSeg[:idx2]
				}
				if definedVars[firstSeg] {
					return nil
				}
			}
			if builtin.FindBuiltinMethod(e.Value) != nil {
				return nil
			}
			// Skip private FFI functions (underscore-prefixed like
			// _sqlite3-open) and ALL-CAPS constants (like SQLITE-OK,
			// MYSQL-RECV-BUF) that are defined in externally imported
			// modules but filtered out by lib.no. These symbols are
			// used inside method bodies that get merged into the
			// program, but their definitions don't survive the
			// lib.no export filtering applied during module merging.
			if strings.HasPrefix(e.Value, "_") || isAllCapsConst(e.Value) {
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
					TraceID: "iyn3rgtm",
					Line:    e.Token.Line,
					Column:  e.Token.Column,
					Message: "'self' (.) can only be used inside struct methods; if you meant the match value, use 'it'",
				})
			} else {
				results = append(results, ValidateResult{
					TraceID: "4bek3xc6",
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
		// Check that the left side's receiver (for DotExpression like s.x = 7)
		// is a defined variable (instance), not a struct type name.
		// e.g. `s { x i64 }` defines type `s`; `s.x = 7` is illegal because
		// `s` is a type, not an instance. Use `s0 = s {}` first, then `s0.x = 7`.
		// Note: built-in type names like `i8`, `u32`, `f64`, `byte` etc. are
		// NOT in validationStructNames, so `i8.MIN = -128` is not flagged here.
		if dot, ok := e.Left.(*parser.DotExpression); ok {
			if recv, ok := dot.Receiver.(*parser.Identifier); ok {
				if recv.Value == "self" {
					// `self` is valid in struct method bodies — skip
				} else if validationStructNames[recv.Value] {
					// Receiver is a struct type name, not an instance.
					results = append(results, ValidateResult{
						TraceID: "fxdzclp6",
						Line:    recv.Token.Line,
						Column:  recv.Token.Column,
						Message: fmt.Sprintf("cannot assign field '%s' on struct type '%s': instantiate first (e.g. `%s0 = %s {}`)", dot.Property, recv.Value, recv.Value, recv.Value),
					})
				}
			}
		}
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

// checkBareExprStatement rejects expression statements that have no observable
// effect — a function/builtin used as a bare value without being called, or a
// literal used as a statement. For example `print 'hi'` parses as two
// statements: the bare identifier `print` then the bare string literal `'hi'`;
// both are meaningless on their own and previously leaked broken IR to the
// backend. A bare *variable* (e.g. `x`) is left to other checks.
func checkBareExprStatement(expr parser.Expression, funcNames map[string]bool) *ValidateResult {
	switch e := expr.(type) {
	case *parser.Identifier:
		if funcNames[e.Value] || builtin.FindBuiltinMethod(e.Value) != nil {
			return &ValidateResult{
				TraceID: "k9179drd",
				Line:    e.Token.Line,
				Column:  e.Token.Column,
				Message: fmt.Sprintf("'%s' is a function and must be called; did you mean %s(...)?", e.Value, e.Value),
			}
		}
		return nil
	case *parser.StringLiteral, *parser.IntegerLiteral, *parser.FloatLiteral,
		*parser.CharLiteral, *parser.BooleanLiteral, *parser.ByteLiteral, *parser.NilLiteral:
		pos := expr.Pos()
		return &ValidateResult{
			TraceID: "yf6kxdmz",
			Line:    pos.Line,
			Column:  pos.Column,
			Message: "a statement cannot be just a literal value; call a function or assign it to a variable",
		}
	}
	return nil
}

// isSignedIntType 判斷內部型別字串是否為有符號整數（i8/i16/i32/i64/i128）。
func isSignedIntType(t string) bool {
	switch t {
	case "i8", "i16", "i32", "i64", "i128":
		return true
	}
	return false
}

// overflowModeFromSem 讀取節點的 #{overflow = wrap|clamp0} 註解，回傳處理模式。
// "" = 未標註（預設有符號相減回傳 option<int>）；"wrap" 靜默回繞；"clamp0" 溢出歸零。
// sem 為 nil 時安全回傳 ""。
func overflowModeFromSem(sem *parser.SemanticContext, n parser.Node) string {
	if sem == nil || n == nil {
		return ""
	}
	for _, e := range sem.AnnotationsOf(n) {
		if e.Key != "overflow" || e.Value == nil {
			continue
		}
		switch v := e.Value.(type) {
		case *parser.AnnotationIdentValue:
			if v.Value == "wrap" || v.Value == "clamp0" {
				return v.Value
			}
		case *parser.AnnotationStringValue:
			if v.Value == "wrap" || v.Value == "clamp0" {
				return v.Value
			}
		}
	}
	return ""
}

// typeNamesEquivalent 判斷兩個型別名是否指涉同一個型別，容許「裸名 ↔ 模組限定名」
// 的拼寫差異（如 `frame` 與 `http2.frame`）。
//
// 模組合併後，struct/enum 型別會同時以裸名與 module.name 兩種鍵註冊
// （見 build/module_prefix.go 的 typeOwner 與 build/llvm 的雙鍵註冊），
// 因此兩種拼寫在語意上等價。但 AST 改寫 pass 可能只改寫其中一側
// （例如把區域變數的型別註解寫成 http2.frame，卻沒改寫同一函式方法的結果型別
// ?frame），導致純字串比較誤報。
//
// 只比較「整串」的裸名/限定名對應，不做子字串比對：
//
//	frame          vs http2.frame   → true
//	server.conn    vs tls.conn      → false（兩個都是限定名，後綴雖同但不互為前綴）
//	?T             vs ?U             → 遞迴比較內層
//
// isViewBorrow reports whether assigning a value of type src to a variable of
// type dst is a VIEW BINDING: `v &T = x` (or `v ?&T = x`) where x is a T.
//
// A view BORROWS its source instead of copying it, so `T -> &T` is legal even
// though the type strings differ — this is exactly the rule that makes
// `json.get = (key str) (child ?&json) { child = self ... }` compile.
func isViewBorrow(dst, src string) bool {
	if !strings.HasPrefix(dst, "&") && !strings.HasPrefix(dst, "?&") {
		return false
	}
	inner := strings.TrimPrefix(strings.TrimPrefix(dst, "?"), "&")
	return typeNamesEquivalent(src, inner)
}

// ValidateViewTypes enforces the rules of the VIEW type (`&T`):
//
//  1. `&T` may only appear in the RESULT list of a METHOD. A borrow has no
//     lifetime of its own; the only place the language can attach one is the
//     method receiver (`self`), so a view in a parameter, in a plain
//     function's result, or in a local declaration is rejected.
//  2. The borrowed type must be the receiver's own type — `json.get` may
//     return `?&json`, never `?&something-else`.
//
// Rule 3 ("only bind to self") is enforced at MIR lowering time, where the
// binding's source expression is actually known.
func ValidateViewTypes(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	if program == nil {
		return results
	}
	// viewInner returns the view type string ("&T") when t is a view or an
	// option of one, and "" otherwise.
	viewInner := func(t parser.Type) string {
		if t == nil {
			return ""
		}
		raw := strings.TrimPrefix(t.String(), "?")
		if !strings.HasPrefix(raw, "&") {
			return ""
		}
		return raw
	}
	add := func(pos lexer.Position, msg string) {
		results = append(results, ValidateResult{
			TraceID: "viewtyp01",
			Line:    pos.Line,
			Column:  pos.Column,
			Message: msg,
		})
	}
	// `&T` is a LIFETIME binding to a method's receiver, not a type: it says
	// "this value borrows from `self`". A struct field has no receiver, so a
	// field can never borrow — `holder { p &point }` is rejected. (An earlier
	// iteration modelled `&T` as a pointer and allowed it in struct fields;
	// that is wrong and was removed.)
	for _, stmt := range program.Statements {
		sd, ok := stmt.(*parser.StructDefinition)
		if !ok {
			continue
		}
		for _, f := range sd.Fields {
			raw := viewInner(f.Type)
			if raw == "" {
				continue
			}
			add(f.Pos(), "view type "+raw+" is not allowed as a struct field: "+
				"a view borrows its lifetime from a method receiver (`self`), and a "+
				"field has no receiver to borrow from")
		}
	}
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		for _, p := range fd.Parameters {
			if raw := viewInner(p.Type); raw != "" {
				p := p.Pos()
				add(p, "view type "+raw+" is only allowed in a method's result list (a borrow has no lifetime outside a receiver)")
			}
		}
		recvType := ""
		if fd.IsMethodDef && len(fd.Results) > 0 && fd.Results[0].Name == "self" && fd.Results[0].Type != nil {
			recvType = fd.Results[0].Type.String()
		}
		for _, r := range fd.Results {
			if r.Name == "self" {
				continue
			}
			raw := viewInner(r.Type)
			if raw == "" {
				continue
			}
			if !fd.IsMethodDef {
				p := r.Pos()
				add(p, "view type "+raw+" is only allowed in a method's result list (a borrow has no lifetime outside a receiver)")
				continue
			}
			if strings.TrimPrefix(raw, "&") != recvType {
				p := r.Pos()
				add(p, "view result "+raw+" must borrow the receiver type ("+recvType+")")
			}
		}
	}
	return results
}

// ValidateFieldTags enforces the rules of the field-level semantic tag
// annotations. Today the only such annotation is `#{inline}`, which opts a
// STRUCT-typed field out of the default pointer layout and back into the
// by-value layout:
//
//	holder {
//	    #{inline=true} p pt   ; stored as %pt, inside the host
//	    q pt                  ; default: stored as %pt*, on the heap
//	}
//
// Three spellings are accepted:
//
//	#{inline}        the shorthand for `#{inline=true}`
//	#{inline=true}   by-value layout: the field's bytes live inside the host
//	#{inline=false}  pointer layout: the slot holds a `%T*`, the pointee is
//	                 heap-allocated and owned by the field
//
// `#{inline=false}` is NOT a synonym for "no annotation". Both values OVERRIDE
// the module-wide default that `NOLANG_FIELD_PTR` selects, so `#{inline=false}`
// makes a field a pointer even when the rest of the program stores struct
// fields by value — which is also how a recursive type is written
// (`node { next node #{inline=false} }`) without flipping the global switch.
//
// The explicit forms are preferred precisely because they say which layout is
// meant; `#{inline=false}` exists so a reader never has to work out whether a
// missing annotation was deliberate.
//
// Rules:
//
//  1. `#{inline}` must sit on a field whose declared type can actually be a
//     struct. On a scalar / str / vec / array / slice / map / option / pointer
//     it is a no-op that reads as if it did something; the language rejects it
//     rather than hiding the ambiguity. This holds for `=false` too: a scalar
//     field is ALWAYS stored inline, so `#{inline=false}` on one asserts the
//     opposite of what the compiler does.
//  2. The value must be a boolean. `#{inline=foo}` is rejected rather than
//     silently read as true.
//  3. Inline fields must not form a cycle. An inlined struct is stored INSIDE
//     its host, so `a { #{inline} b b }` together with `b { #{inline} a a }`
//     has no finite size. Direct self-reference is the degenerate case.
//
// A struct-typed field WITHOUT the annotation is always valid: that is the
// default pointer layout, which is what makes recursive types possible.
func ValidateFieldTags(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	if program == nil {
		return results
	}
	add := func(pos lexer.Position, msg string) {
		results = append(results, ValidateResult{
			TraceID: "fieldtag1",
			Line:    pos.Line,
			Column:  pos.Column,
			Message: msg,
		})
	}

	var structs []*parser.StructDefinition
	known := map[string]bool{}
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			known[sd.Name] = true
			structs = append(structs, sd)
		}
	}

	// inlineEdges[a] lists the struct types `a` embeds BY VALUE: its inline
	// fields, plus fixed-size arrays of a struct (which also store the element
	// inline). Slices, maps, options and pointers embed only a handle.
	inlineEdges := map[string][]string{}
	for _, sd := range structs {
		for _, f := range sd.Fields {
			if f == nil {
				continue
			}
			present, value, ok := inlineAnnotationOf(program, f)
			if !present {
				continue
			}
			if !ok {
				add(f.Pos(), "#{inline} takes a boolean: write `#{inline=true}` (or the "+
					"shorthand `#{inline}`) for the by-value layout, or `#{inline=false}` "+
					"for the default pointer layout; got a non-boolean value")
				continue
			}
			raw := structFieldTypeString(f)
			if raw == "" {
				continue
			}
			if !canBeStructType(raw) {
				add(f.Pos(), "#{inline} is only meaningful on a struct-typed field, but "+
					sd.Name+"."+f.Name+" is declared "+raw+"; scalars, str/vec, arrays, "+
					"slices, maps, options and pointers are always stored inline")
				continue
			}
			// Only `=true` embeds by value. `=false` is a pointer, so it adds
			// no embedding edge and can never close a cycle — that is what
			// makes `node { next node #{inline=false} }` legal.
			if value {
				inlineEdges[sd.Name] = append(inlineEdges[sd.Name], raw)
			}
		}
	}

	// Enum definitions carry `#{inline}` too (the enum-level annotation above
	// the whole enum). Nothing embeds by value there, but the value must still
	// be a boolean, otherwise `#{inline=foo}` would silently mean "true".
	for _, stmt := range program.Statements {
		var entries []*parser.AnnotationEntry
		switch d := stmt.(type) {
		case *parser.TaggedEnumDefinition:
			entries = annotationEntriesOf(program, d)
		case *parser.EnumDefinition:
			entries = annotationEntriesOf(program, d)
		default:
			continue
		}
		if _, _, ok := parser.InlineAnnotationValue(entries); !ok {
			add(stmt.Pos(), "#{inline} takes a boolean: write `#{inline=true}` (or the "+
				"shorthand `#{inline}`), or `#{inline=false}`; got a non-boolean value")
		}
	}

	// Cycle detection over the by-value embedding graph. A type that is not a
	// struct declared in this program (an alias, or a qualified std struct) is
	// not resolved here and simply ends the walk.
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var cyclePath []string
	var walk func(name string, path []string) bool
	walk = func(name string, path []string) bool {
		state[name] = onStack
		path = append(path, name)
		for _, next := range inlineEdges[name] {
			if !known[next] {
				continue
			}
			switch state[next] {
			case onStack:
				cyclePath = append(path, next)
				return true
			case unvisited:
				if walk(next, path) {
					return true
				}
			}
		}
		state[name] = done
		return false
	}
	for _, sd := range structs {
		if state[sd.Name] != unvisited {
			continue
		}
		if walk(sd.Name, nil) {
			add(sd.Pos(), "inline fields form a cycle: "+strings.Join(cyclePath, " -> ")+
				" — an inlined struct is stored inside its host, so the size would be "+
				"infinite; write #{inline=false} on one of these fields (or drop the "+
				"annotation) to break the cycle")
			break
		}
	}
	return results
}

// annotationEntriesOf returns the `#{...}` entries attached to a node.
// Both tables are consulted: ResolveProgram copies RawAnnotations into
// Annotations, and the single-file / dependency-vet paths never run it.
func annotationEntriesOf(program *parser.Program, n parser.Node) []*parser.AnnotationEntry {
	if program == nil || program.Sem == nil || n == nil {
		return nil
	}
	entries := program.Sem.AnnotationsOf(n)
	if len(entries) == 0 {
		entries = program.Sem.RawAnnotationsOf(n)
	}
	return entries
}

// inlineAnnotationOf reads the `#{inline}` annotation off a struct field.
// present=false means no annotation; ok=false means it carried a non-boolean
// value. See parser.InlineAnnotationValue for the accepted spellings.
func inlineAnnotationOf(program *parser.Program, f *parser.StructField) (present, value, ok bool) {
	return parser.InlineAnnotationValue(annotationEntriesOf(program, f))
}

// canBeStructType reports whether a declared field type could name a struct.
// It is a blacklist on purpose: anything it does not positively rule out (a
// user struct name, a type alias, a module-qualified name) is left for the
// layout pass to resolve, so an alias to a struct is not misreported here.
func canBeStructType(raw string) bool {
	if raw == "" {
		return false
	}
	switch raw {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"f32", "f64", "bool", "byte", "char", "int", "str", "vec", "txt":
		return false
	}
	switch raw[0] {
	case '[', '*', '&', '?', '%':
		// [N]T and [K]V (fixed array / map), []T (slice), *T, &T (view),
		// ?T (option) and %T all embed a handle, not the value.
		return false
	}
	return true
}

func typeNamesEquivalent(a, b string) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	// 指標/可空/view 前綴需一致，再比較其餘部分。
	for _, prefix := range []string{"?", "*", "%", "&"} {
		pa := strings.HasPrefix(a, prefix)
		pb := strings.HasPrefix(b, prefix)
		if pa != pb {
			return false
		}
		if pa {
			return typeNamesEquivalent(strings.TrimPrefix(a, prefix), strings.TrimPrefix(b, prefix))
		}
	}
	return strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

// optionTypesCompatible 判斷兩個 option 型別（?T / ?U）是否可互相賦值：
// 內部型別相同（容許模組限定名差異）或皆為整數型別（允許窄化）。
func optionTypesCompatible(inferred, existing string) bool {
	if !strings.HasPrefix(inferred, "?") || !strings.HasPrefix(existing, "?") {
		return false
	}
	it := inferred[1:]
	et := existing[1:]
	if typeNamesEquivalent(it, et) {
		return true
	}
	if _, _, ok1 := intTypeRange(it); ok1 {
		if _, _, ok2 := intTypeRange(et); ok2 {
			return true
		}
	}
	return false
}

// isContainerLenOwner reports whether a value of type t owns a read-only `len`
// (and `cap` / `len-bytes`) field that must never be written directly: strings,
// arrays, slices and vec. Everything else — including a user struct that happens
// to declare a real `len` field (e.g. `bag { items []i64; len i64 }`) or the
// builtin `txt` — is a normal assignable field and must stay legal.
func isContainerLenOwner(t string) bool {
	switch {
	case t == "str":
		return true
	case strings.HasPrefix(t, "["):
		return true
	case t == "vec" || t == "%vec":
		return true
	}
	return false
}

// checkReadOnlyLenAssign rejects writing the read-only length field of a
// container (`x.len = n`, `x.cap = n`, `x.len-bytes = n`).
//
// ⚠️ INTENTIONALLY NOT CALLED (2026-09-21). See the call-site note in
// validateStmtTypes: writing `.len` on a container **struct field** (including
// nested ones like `b.arr[1].len = 2`) is a supported grow/shrink operation —
// codegen allocates the buffer when data == null, and
// tests/len-assign-grow.no covers it. Wiring this in would reject those
// and break that test. Plain container variables are already rejected by the
// transpiler's own guard ("cannot modify read-only field 'len' of ...").
//
// Original rationale (kept for context): the transpiler's guard only fires for
// bare identifiers present in arraySizes / sliceSizes / stringSizes, so nested
// paths slipped through and were miscompiled into a null-pointer store. That
// hole was since fixed on the CODEGEN side (allocation on null), not by
// rejecting the write — so this type-inference based check is redundant.
func checkReadOnlyLenAssign(a *parser.AssignExpression, varTypes, funcTypes map[string]string, selfType string) *ValidateResult {
	if a == nil || a.Left == nil {
		return nil
	}
	dot, ok := a.Left.(*parser.DotExpression)
	if !ok {
		return nil
	}
	switch dot.Property {
	case "len", "cap", "len-bytes":
	default:
		return nil
	}
	rt := inferExprType(dot.Receiver, varTypes, funcTypes, selfType)
	if !isContainerLenOwner(rt) {
		return nil
	}
	pos := a.Left.Pos()
	return &ValidateResult{
		TraceID: "rdonlylen",
		Line:    pos.Line,
		Column:  pos.Column,
		Message: fmt.Sprintf("cannot modify read-only field '%s' of %s; the length is derived from the buffer — use truncate(n) instead", dot.Property, rt),
	}
}

func validateStmtTypes(stmt parser.Statement, funcNames map[string]bool, funcTypes map[string]string, selfType string, varTypes map[string]string, isBlockValue bool, sem *parser.SemanticContext, overflowMode string) []ValidateResult {
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
		if len(s.Results) > 0 && s.Results[0].Name == "self" {
			methodSelfType = s.Results[0].Type.String()
		}
		// 建立局部 funcNames 副本，排除當前函式的參數和輸出參數名，
		// 避免與全域函式同名時（如 io.out）對輸出參數賦值被誤報為
		// "cannot reassign function name"。
		localFuncNames := make(map[string]bool, len(funcNames))
		for k, v := range funcNames {
			localFuncNames[k] = v
		}
		for _, p := range s.Parameters {
			delete(localFuncNames, p.Name)
		}
		for _, p := range s.Results {
			delete(localFuncNames, p.Name)
		}
		if s.Body != nil {
			// 函數級溢出處理模式：#{overflow = ...} 註解優先，否則繼承外層模式。
			fdMode := overflowModeFromSem(sem, s)
			if fdMode == "" {
				fdMode = overflowMode
			}
			for i, bStmt := range s.Body.Statements {
				errs := validateStmtTypes(bStmt, localFuncNames, funcTypes, methodSelfType, localTypes, i == len(s.Body.Statements)-1, sem, fdMode)
				results = append(results, errs...)
			}
		}

	case *parser.LetStatement:
		// Skip compiler-injected synthetic let statements (e.g. match arm `it` bindings)
		if s.IsSynthetic {
			// Still record its declared type so later synthetic references can resolve it
			if s.Type != nil && s.Type.String() != "" {
				varTypes[s.Name.Value] = s.Type.String()
			} else if s.Name != nil && s.Name.Value == "it" {
				// Untyped shared `it` binding（ok/wildcard 臂在 parse 期無法確定
				// matched 型別時不帶 Type 標注）。清掉前一個 arm 的 typed binding
				// 殘留在 varTypes["it"] 的型別（如 err 臂的 "err"），否則本臂的
				// `v = it` 會把 v 推斷成 "err"，後續 `x ?= ...` + `x = v` 誤報
				// "cannot assign err value to ?i64 variable"（trace 15w45dqk）。
				delete(varTypes, "it")
			}
			break
		}
		// Compiler-generated `result = __unwrap_N` propagation assignment
		// (lowering's `?=` / safe-index error paths). It copies the whole
		// option value into an option-typed result parameter to forward a
		// nil/err outcome; at runtime every option is `{i64 tag, i64 data}`,
		// so the copy is representation-compatible even when the inner types
		// differ (`size ?= fstat-size(.fd)` in a function returning `?[]byte`).
		// Type compatibility here is guaranteed by lowering, so skip the check.
		if s.IsPropagation {
			break
		}
		// 宣告局部變數後，從 funcNames 中移除該名稱，
		// 使後續 AssignExpression 對同名局部變數賦值不會誤報。
		// funcNames 在 FunctionDefinition 分支中已是 localFuncNames 副本，
		// 修改不影響全域 funcNames。
		// 注意：不在 LetStatement 中檢查 funcNames，因為在函式體內
		// 聲明與全域函式同名的局部變數（如 sha3-224 中的 out）是合法的遮蔽。
		delete(funcNames, s.Name.Value)

		// 檢查 nil 賦值到非可空變數
		if _, isNil := s.Value.(*parser.NilLiteral); isNil {
			// 有顯式型別註記
			if s.Type != nil && s.Type.String() != "" && s.Type.String() != s.Name.Value {
				_, isOption := s.Type.(*parser.NullableType)
				if !isOption {
					results = append(results, ValidateResult{
						TraceID: "aih7e3j0",
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
						TraceID: "p2epl19k",
						Line:    s.Token.Line,
						Column:  s.Token.Column,
						Message: fmt.Sprintf("cannot assign nil to non-option variable '%s'", s.Name.Value),
					})
				}
				break
			}
			// 新變數從 nil 推斷不出型別
			results = append(results, ValidateResult{
				TraceID: "170bewkb",
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
							TraceID: "irjygycv",
							Line:    callPos.Line,
							Column:  callPos.Column,
							Message: "val() constructor is deprecated; use ok(x) for explicit construction or `name = expr` for implicit assignment",
						})
					}
				}
			}
			// 型別推斷
			inferredType := inferExprType(s.Value, varTypes, funcTypes, selfType)
			// `err` 是變體標記，不是真型別：match 的 `err ->` 臂會為隱式綁定 `it`
			// 合成一條 `let it t=err`。而 err 載荷**永遠是 str**
			// （`option { ok(v t), nil, err(e str) }`，且統一佈局後標量 option
			// 也真的存得下整條訊息），所以 err 臂的 `it` 就是一個字串值。
			// 把它正規化成 "str"，否則 `msg = it` 會誤報
			// 「cannot assign err value to str variable」(trace 15w45dqk)，
			// 而且首次指派時變數會被推斷成根本不存在的型別 "err"。
			if inferredType == "err" {
				inferredType = "str"
			}
			// 有符號整數相減溢位：未標註 #{overflow} 時預設回傳 option<int>
			// （溢出 err，永不 panic）；標註 wrap/clamp0 時回傳 int。
			// i128 因 %option 無法容納，維持回傳 i128（codegen 退化為回繞）。
			if inferredType != "" {
				if inf, ok := s.Value.(*parser.InfixExpression); ok && inf.Operator == "-" &&
					isSignedIntType(inferredType) && inferredType != "i128" {
					effMode := overflowModeFromSem(sem, s)
					if effMode == "" {
						effMode = overflowMode
					}
					// 預設有符號相減維持 plain int（2's 補數回繞）；option<int> 回傳路徑暫停用（codegen 已改為 plain sub）。

				}
			}
			if inferredType == "" {
				// Type inference failed. Check if the RHS is a LHS-inferred
				// builtin (with-len, with-cap, with-cap-len) whose result
				// type depends on the assignment LHS. Without an explicit
				// type annotation, the type cannot be determined and
				// defaults to []i64 (8 bytes/element), which is almost
				// never the intended type. Report an error so the user
				// adds a type annotation (e.g. `buf []byte = with-len(n)`).
				if fnName := isLHSInferredBuiltinCall(s.Value); fnName != "" {
					// Only report if the variable has no real type annotation
					// and no existing type from a prior declaration (e.g.
					// function parameter or output parameter).
					_, hasExistingType := varTypes[s.Name.Value]
					if !hasRealTypeAnnotation(s) && !hasExistingType {
						valPos := s.Value.Pos()
						results = append(results, ValidateResult{
							TraceID: "nnqq67ko",
							Line:    valPos.Line,
							Column:  valPos.Column,
							Message: fmt.Sprintf("cannot infer type for '%s': %s() requires an explicit type annotation on the left side (e.g. `name []byte = %s(n)`)", s.Name.Value, fnName, fnName),
						})
					}
				}
			}
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
										TraceID: "os0j8ix4",
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
									TraceID: "f0p0dfha",
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
					if inferredType != "" && !typeNamesEquivalent(inferredType, existingType) && isConcreteType(existingType) && !isArrayAssign && !isOptionCtor &&
						!isArgTypeCompatible(existingType, inferredType, s.Value) && !optionTypesCompatible(inferredType, existingType) &&
						!isViewBorrow(existingType, inferredType) &&
						// 整數字面量指派給已宣告型別的變數：常數轉換（如 -1 → byte == 255），不報窄化
						!isIntLiteralNarrowingToDeclared(s.Value, existingType) {
						valPos := s.Value.Pos()
						results = append(results, ValidateResult{
							TraceID: "15w45dqk",
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
		// Reject bare expression statements with no effect (e.g. `print 'hi'`
		// parses as the bare identifier `print` then the bare literal `'hi'`).
		// A bare expression that is the LAST statement of its block is the
		// block's value (e.g. `r = if 1 { 'hello' }`), so it is allowed.
		if !isBlockValue {
			if res := checkBareExprStatement(s.Expression, funcNames); res != nil {
				results = append(results, *res)
				break
			}
		}
		// val(x) 作為構造器已廢棄：檢查語句表達式中的 val() 呼叫
		if call, ok := s.Expression.(*parser.CallExpression); ok {
			if cid, ok2 := call.Function.(*parser.Identifier); ok2 && cid.Value == "val" {
				callPos := call.Pos()
				results = append(results, ValidateResult{
					TraceID: "7c5fbvp4",
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
				for i, bStmt := range ifExpr.Consequence.Statements {
					errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes, i == len(ifExpr.Consequence.Statements)-1, sem, overflowMode)
					results = append(results, errs...)
				}
			}
			if ifExpr.Alternative != nil {
				for i, bStmt := range ifExpr.Alternative.Statements {
					errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes, i == len(ifExpr.Alternative.Statements)-1, sem, overflowMode)
					results = append(results, errs...)
				}
			}
			break
		}
		if assign, ok := s.Expression.(*parser.AssignExpression); ok {
			// 唯讀容器欄位寫入：`X.len = n` / `X.cap = n` / `X.len-bytes = n`。
			//
			// ⚠️ 實際契約（2026-09-21 實測確認，別再照舊註解去「補洞」）：
			//   - **純變數**容器（`s = 'abc'; s.len = 2`）→ **拒絕**。由 transpiler 那層
			//     的表（arraySizes / sliceSizes / stringSizes）攔下，錯誤訊息
			//     "cannot modify read-only field 'len' of string 's'"。
			//   - **結構體欄位**容器（`b.s.len = 3`，含巢狀 `b.arr[1].len = 2`）
			//     → **放行，而且是刻意支援的功能**：寫入時若 data == null 會先分配
			//     緩衝區（codegen 的 ensureContainerStorageForLen），所以「從零值增長」
			//     是合法的，`tests/len-assign-grow.no` 五個案例全靠這個行為。
			//     早年 `p.nodes[i].str-val.len = 1` 對 null 做 GEP + store 會 SIGSEGV，
			//     那個洞已經由上面的分配邏輯補掉（不是靠拒絕）。
			//
			// 結論：這裡**不要**接 checkReadOnlyLenAssign —— 那會把欄位寫入也擋掉，
			// 直接打破 len-assign-grow.no。該函式刻意保持不被呼叫。
			if ident, ok := assign.Left.(*parser.Identifier); ok {
				// 檢查是否對函式名稱賦值
				if funcNames[ident.Value] {
					results = append(results, ValidateResult{
						TraceID: "2ls7kxqp",
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
								TraceID: "b6fjj9ka",
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
						// 有符號整數相減溢位：未標註時預設回傳 option<int>。
						subDefaultOpt := false
						if valType != "" {
							if inf, ok := assign.Value.(*parser.InfixExpression); ok && inf.Operator == "-" &&
								isSignedIntType(valType) && valType != "i128" {
								effMode := overflowModeFromSem(sem, s)
								if effMode == "" {
									effMode = overflowMode
								}
								// 預設有符號相減維持 plain int（2's 補數回繞）；option<int> 回傳路徑暫停用。

							}
						}
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
										TraceID: "96o13ee6",
										Line:    callPos.Line,
										Column:  callPos.Column,
										Message: "val() constructor is deprecated; use ok(x) for explicit construction or `name = expr` for implicit assignment",
									})
									isOptionCtor = true
								}
							}
						}
						if valType != "" && !typeNamesEquivalent(valType, existingType) && isConcreteType(existingType) && !isOptionCtor &&
							!isArgTypeCompatible(existingType, valType, assign.Value) && !optionTypesCompatible(valType, existingType) &&
							// 整數字面量指派給已宣告型別的變數：常數轉換（如 -1 → byte == 255），不報窄化
							!isIntLiteralNarrowingToDeclared(assign.Value, existingType) {
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
												TraceID: "3b9bxfnt",
												Line:    assign.Token.Line,
												Column:  assign.Token.Column,
												Message: fmt.Sprintf("cannot assign %s value to %s element in array '%s'%s", elemInferred, elemType, ident.Value, narrowingHint(elemInferred, elemType)),
											})
										}
									}
								}
							} else {
								msg := fmt.Sprintf("cannot assign %s value to %s variable '%s'%s", valType, existingType, ident.Value, narrowingHint(valType, existingType))
								if subDefaultOpt && !strings.HasPrefix(existingType, "?") {
									msg = fmt.Sprintf("signed subtraction may overflow: result is option<%s> but '%s' is declared %s. Add `#{overflow = wrap}` or `#{overflow = clamp0}` (e.g. above this statement), or declare '%s' as ?%s", strings.TrimPrefix(valType, "?"), ident.Value, existingType, ident.Value, strings.TrimPrefix(valType, "?"))
								}
								results = append(results, ValidateResult{
									TraceID: "1mf3x79l",
									Line:    assign.Token.Line,
									Column:  assign.Token.Column,
									Message: msg,
								})
							}
						}
					} else if !exists {
						// 首次賦值，記錄推斷型別
						valType := inferExprType(assign.Value, varTypes, funcTypes, selfType)
						// 有符號整數相減溢位：未標註時預設回傳 option<int>。
						if valType != "" {
							if inf, ok := assign.Value.(*parser.InfixExpression); ok && inf.Operator == "-" &&
								isSignedIntType(valType) && valType != "i128" {
								effMode := overflowModeFromSem(sem, s)
								if effMode == "" {
									effMode = overflowMode
								}
								// 預設有符號相減維持 plain int（2's 補數回繞）；option<int> 回傳路徑暫停用。

							}
						}
						if valType != "" {
							varTypes[ident.Value] = valType
						}
					}
				}
			}
		}

	case *parser.ForStatement:
		if s.Body != nil {
			for i, bStmt := range s.Body.Statements {
				errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes, i == len(s.Body.Statements)-1, sem, overflowMode)
				results = append(results, errs...)
			}
		}

	case *parser.BlockStatement:
		for i, bStmt := range s.Statements {
			errs := validateStmtTypes(bStmt, funcNames, funcTypes, selfType, varTypes, i == len(s.Statements)-1, sem, overflowMode)
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
									TraceID: "35gh0yw4",
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
	stdSigsOnce        sync.Once
	stdSigsCache       map[string][]string // 模塊函數簽名（鍵：module.fn 或裸名 fn）
	stdMethodSigsCache map[string][]string // 結構體方法簽名（鍵：module.struct.method）
	// 參數型別表，與上面兩張「回傳型別表」同鍵。有了它 checker 才能檢查
	// std 呼叫的引數；沒有它，方法引數從來沒被走訪過（見 checkCallArgsInExpr）。
	stdFuncParamsCache   map[string][]string // 模塊函數參數型別（鍵同 stdSigsCache）
	stdMethodParamsCache map[string][]string // 方法參數型別（鍵同 stdMethodSigsCache）
	stdFieldsCache       map[string]map[string]string
	stdAliasesCache      map[string]string   // 單具體型別別名快取（如 "fd" → "i64"）
	stdStructModCache    map[string]string   // struct name → module short name（如 "conn" → "tls"）
	stdEnumVariantsCache map[string][]string // enum type name → variant names (如 "file-mode" → ["read","write","append","read-write"])
	// stdProgramsCache 緩存 CollectStdModuleSignatures PASS1 已解析的 std 模組
	// Program，按「內容哈希」(cache.ContentKey) 索引。no vet src/std 的第二遍
	// 可直接復用，跳過磁盤文件的重新 parse；內容與 embed 不一致時自然不命中，
	// 安全回退到正常 parse。僅在 NOLANG_REUSE_STD_AST 開關開啟時由 build 端查詢。
	stdProgramsCache map[string]*parser.Program
)

// stdHirCache caches the immutable HIR arena of each std module, keyed by
// content hash. Signature collection (collectStdSigsFromFS PASS 1) and export
// collection (parseModuleExportsFromSource) both route through it, so a module
// is parsed and lowered to HIR exactly once per process instead of being
// re-parsed on every build. It is the HIR twin of stdProgramsCache (which keeps
// the AST); see the HIR migration plan (stage 2: signature/export extraction
// consumes HIR, AST path retained as the proven oracle + equivalence oracle).
var (
	stdHirCacheMu sync.Mutex
	stdHirCache   map[string]*hir.Package
)

func stdHirStore(ck string, pkg *hir.Package) {
	stdHirCacheMu.Lock()
	if stdHirCache == nil {
		stdHirCache = make(map[string]*hir.Package)
	}
	stdHirCache[ck] = pkg
	stdHirCacheMu.Unlock()
}

// stdHirForSource parses and lowers source into its HIR arena, caching the
// result by content hash. A nil return means the source failed to parse.
func stdHirForSource(source []byte) *hir.Package {
	ck := cache.ContentKey(string(source))
	stdHirCacheMu.Lock()
	if p, ok := stdHirCache[ck]; ok {
		stdHirCacheMu.Unlock()
		return p
	}
	stdHirCacheMu.Unlock()

	l := lexer.New(string(source))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil
	}
	// 移除標準庫內建樁函式（#{buildin=...}）：其真實實作位於 Go runtime，
	// 不應作為模組匯出符號（否則會改變內建解析路徑）。
	kept := prog.Statements[:0]
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.BuiltinStub {
			continue
		}
		kept = append(kept, stmt)
	}
	prog.Statements = kept
	pkg := parser.ASTToHIR(prog)
	stdHirStore(ck, pkg)
	return pkg
}

type StdModuleInfo struct {
	ShortName string // last path segment of FullPath, e.g. "rand", "math"
	FullPath  string // relative to std/, e.g. "crypto/rand", "net/net", "math"
	ShortPath string // FullPath with redundant dir omitted when dir==file, e.g. "net", "crypto/hmac", "math"
}

func debugCountHashFns(stage string, merged *parser.Program) {
	_ = stage
	_ = merged
}

// listStdModules enumerates every std module under "std/" of the given fs.FS.
// It is the FS-parameterized core of knownStdModules: the runtime uses
// nolang.StdFS (embedded), while the signature-table generator uses an
// os.DirFS over src/ on disk. Both yield identical module lists because the
// embed source and the disk source are the same files.
func listStdModules(fsys fs.FS) []StdModuleInfo {
	var infos []StdModuleInfo
	seen := make(map[string]bool)

	var walkDir func(dir string)
	walkDir = func(dir string) {
		entries, err := fs.ReadDir(fsys, dir)
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
	return infos
}

func knownStdModules() []StdModuleInfo {
	knownStdModulesOnce.Do(func() {
		knownStdModulesList = listStdModules(nolang.StdFS)
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

//go:generate go run ./genstdsig

func setStdSigCaches(funcSigs map[string][]string, methodSigs map[string][]string, funcParams map[string][]string, methodParams map[string][]string, structFields map[string]map[string]string, aliases map[string]string, structMod map[string]string, enumVariants map[string][]string) {
	stdSigsCache = funcSigs
	stdMethodSigsCache = methodSigs
	stdFuncParamsCache = funcParams
	stdMethodParamsCache = methodParams
	stdFieldsCache = structFields
	stdAliasesCache = aliases
	stdStructModCache = structMod
	stdEnumVariantsCache = enumVariants
}

func CollectStdModuleSignatures() (map[string][]string, map[string]map[string]string) {
	stdSigsOnce.Do(func() {
		// ---- embedded signature table (compiled-in Go literals) ----
		// Preferred path: stdsig_gen.go bakes the five signature tables into
		// the binary as Go source literals, so PASS1 is skipped on EVERY build.
		// Only used when the embedded key matches the current embedded std
		// content; if src/std changed without regenerating stdsig_gen.go, the
		// keys differ and we fall through to full collection.
		if embeddedStdSigReady {
			if key, err := computeStdSigKey(); err == nil && embeddedStdSigKey == key {
				setStdSigCaches(embeddedStdFuncSigs, embeddedStdMethodSigs, embeddedStdFuncParams, embeddedStdMethodParams, embeddedStdStructFields, embeddedStdAliases, embeddedStdStructMod, embeddedStdEnumVariants)
				warmStdTokenCache()
				return
			}
		}

		// ---- full collection from embedded StdFS ----
		funcSigs, methodSigs, funcParams, methodParams, structFields, aliases, structMod, enumVariants, _ := collectStdSigsFromFS(nolang.StdFS)
		setStdSigCaches(funcSigs, methodSigs, funcParams, methodParams, structFields, aliases, structMod, enumVariants)
		warmStdTokenCache()
	})
	return stdSigsCache, stdFieldsCache
}

// CollectStdSigsFromFS is the exported, FS-parameterized entry point used by
// the signature-table generator (genstdsig) to collect the five signature
// tables from the on-disk src/ tree at `no` build time.
func CollectStdSigsFromFS(fsys fs.FS) (map[string][]string, map[string][]string, map[string][]string, map[string][]string, map[string]map[string]string, map[string]string, map[string]string, map[string][]string, error) {
	return collectStdSigsFromFS(fsys)
}

// collectStdSigsFromFS parses every std module under fsys and collects the
// five signature tables (func return types, method return types, struct fields,
// type aliases, struct→module map) needed by the parser's type inference. It is
// the FS-parameterized core of CollectStdModuleSignatures: the runtime passes
// nolang.StdFS, the generator passes an os.DirFS over src/ on disk.
// Returns (funcSigs, methodSigs, funcParams, methodParams, structFields,
// aliases, structMod, enumVariants, error). The two *Params tables are keyed
// exactly like their result-type counterparts and carry the declared
// PARAMETER types, so callers can type-check arguments.
func collectStdSigsFromFS(fsys fs.FS) (map[string][]string, map[string][]string, map[string][]string, map[string][]string, map[string]map[string]string, map[string]string, map[string]string, map[string][]string, error) {
	// PASS 1: 解析所有模組並暫存，同時統計裸 struct 名的跨模組定義數。
	// 多模組同名結構體（如 server-conn 定義於 server/tls/sse/ws）的裸名
	// 有歧義：函數簽名快照若記錄裸名（?server-conn），解析期 it 綁定會
	// 標注錯誤型別，合併後 codegen 解析到錯誤模組的結構體。
	type parsedMod struct {
		info   StdModuleInfo
		prog   *parser.Program
		hirPkg *hir.Package
	}
	var mods []parsedMod
	// 並行解析：各模組的 lex+parse 相互獨立，僅共用有鎖的 token LRU。
	// 結果按模組原順序寫回，保持 last-wins 合併語義不變。
	known := listStdModules(fsys)
	parsed := make([]*parser.Program, len(known))
	parsedCK := make([]string, len(known))
	hirPkgs := make([]*hir.Package, len(known))
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
				source, err := fs.ReadFile(fsys, embedPath)
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
				// Build and cache the HIR arena for this module so export
				// collection (parseModuleExportsFromSource) and signature
				// collection (PASS 2 below) reuse it instead of re-parsing.
				// This is the entry point of the HIR pipeline: every std
				// module is lowered exactly once per process.
				hp := parser.ASTToHIR(prog)
				hirPkgs[i] = hp
				stdHirStore(parsedCK[i], hp)
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
		mods = append(mods, parsedMod{info, prog, hirPkgs[i]})
	}

	// PASS 2: 收集簽名/欄位/別名 —— 直接走 HIR。每個模組的 HIR arena 已在
	// PASS 1 生成並緩存，hir.CollectModuleSignatures 只遍歷已構建的 HIR 切片，
	// 不再 lex/parse 任何源碼。其產出與上方 AST 遍歷 byte 級一致
	// （見 hir/golden_test.go 的 TestSignatureTablesMatchChecker）。
	modulePkgs := make([]hir.ModulePackage, 0, len(mods))
	for _, m := range mods {
		if m.hirPkg == nil {
			// Fall back to lowering on demand; should not happen because PASS 1
			// always builds the arena, but keeps the function total-safe.
			m.hirPkg = parser.ASTToHIR(m.prog)
		}
		modulePkgs = append(modulePkgs, hir.ModulePackage{Short: m.info.ShortName, Pkg: m.hirPkg})
	}
	st := hir.CollectModuleSignatures(modulePkgs)

	return st.Funcs, st.Methods, st.FuncParams, st.MethodParams, st.Structs, st.Aliases, st.StructMod, st.Enums, nil
}
func CollectStdConcreteAliases() map[string]string {
	CollectStdModuleSignatures() // 觸發 sync.Once 填充快取
	return stdAliasesCache
}
func CollectStdStructModules() map[string]string {
	CollectStdModuleSignatures() // 觸發 sync.Once 填充快取
	return stdStructModCache
}

// CollectStdMethodSigs 返回結構體方法簽名表（鍵：module.struct.method）。
// 與 CollectStdModuleSignatures（模組函數簽名）分離存放，避免同名衝突。
func CollectStdMethodSigs() map[string][]string {
	CollectStdModuleSignatures() // 觸發 sync.Once 填充快取
	return stdMethodSigsCache
}

// CollectStdEnumVariants 返回 std 模組中所有枚舉型別的變體名列表。
// 鍵為枚舉型別名（如 "file-mode"），值為變體名列表（如 ["read","write","append","read-write"]）。
// 供 parser 的 match desugar 使用，使跨模組枚舉 match 能正確識別型別。
func CollectStdEnumVariants() map[string][]string {
	CollectStdModuleSignatures() // 觸發 sync.Once 填充快取
	return stdEnumVariantsCache
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
	case *parser.ViewType:
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

// lhsInferredBuiltins lists builtin functions whose result type is inferred
// from the assignment LHS (e.g. `buf []byte = with-len(n)`).
// When the LHS has no type annotation, these functions cannot determine the
// element type and default to i64 (8 bytes), which is almost never what the
// user wants. The checker must report an error in this case.
var lhsInferredBuiltins = map[string]bool{
	"with-len":     true,
	"with-cap":     true,
	"with-cap-len": true,
}

// isLHSInferredBuiltinCall checks whether expr is a call to a builtin
// whose type is inferred from the assignment LHS (with-len, with-cap,
// with-cap-len). Returns the function name if true, "" otherwise.
func isLHSInferredBuiltinCall(expr parser.Expression) string {
	call, ok := expr.(*parser.CallExpression)
	if !ok {
		return ""
	}
	ident, ok := call.Function.(*parser.Identifier)
	if !ok {
		return ""
	}
	if lhsInferredBuiltins[ident.Value] {
		return ident.Value
	}
	return ""
}

// hasRealTypeAnnotation reports whether a LetStatement has an explicit,
// user-written type annotation (not a parser-inferred placeholder).
func hasRealTypeAnnotation(s *parser.LetStatement) bool {
	return s.Type != nil && s.Type.String() != "" &&
		s.Type.String() != s.Name.Value && !isInferredType(s.Type)
}

// ValidateLHSInferredBuiltins checks all statements (including function bodies)
// for with-len/with-cap/with-cap-len calls without an explicit type annotation
// on the assignment LHS. This is used as a post-merge validation pass to catch
// cases that single-file ValidateTypes/ValidateFuncArgs would miss — i.e. when
// the offending code lives in an imported module file.
//
// The function recurses into:
//   - FunctionDefinition bodies (including method bodies)
//   - BlockStatement sub-blocks
//   - IfExpression branches
func ValidateLHSInferredBuiltins(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	varTypes := make(map[string]string)
	for _, stmt := range program.Statements {
		results = append(results, checkLHSInferredInStmt(stmt, varTypes)...)
	}
	return results
}

// checkLHSInferredInStmt recursively walks a statement and checks for
// LHS-inferred builtin calls (with-len, with-cap, with-cap-len) without
// an explicit type annotation.
func checkLHSInferredInStmt(stmt parser.Statement, varTypes map[string]string) []ValidateResult {
	if stmt == nil {
		return nil
	}
	var results []ValidateResult
	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		// Build local var types from parameters and result params
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
			for _, bs := range s.Body.Statements {
				results = append(results, checkLHSInferredInStmt(bs, localTypes)...)
			}
		}
	case *parser.BlockStatement:
		for _, bs := range s.Statements {
			results = append(results, checkLHSInferredInStmt(bs, varTypes)...)
		}
	case *parser.LetStatement:
		if s.IsSynthetic {
			break
		}
		// Record explicit type annotation
		if s.Type != nil && s.Type.String() != "" && s.Type.String() != s.Name.Value {
			if _, exists := varTypes[s.Name.Value]; !exists {
				varTypes[s.Name.Value] = s.Type.String()
			}
		}
		if s.Value != nil {
			if fnName := isLHSInferredBuiltinCall(s.Value); fnName != "" {
				_, hasExistingType := varTypes[s.Name.Value]
				if !hasRealTypeAnnotation(s) && !hasExistingType {
					valPos := s.Value.Pos()
					results = append(results, ValidateResult{
						TraceID: "p56zjfhi",
						Line:    valPos.Line,
						Column:  valPos.Column,
						Message: fmt.Sprintf("cannot infer type for '%s': %s() requires an explicit type annotation on the left side (e.g. `name []byte = %s(n)`)", s.Name.Value, fnName, fnName),
					})
				}
			}
		}
	}
	return results
}

func isBuiltinType(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64", "i128",
		"u8", "u16", "u32", "u64", "u128",
		"f32", "f64",
		"byte", "bool", "str", "txt":
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
			TraceID: "ol0l3ova",
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
			// 方法定義的合成 receiver `self` 由 parser/decl.go 在解析
			// `t.method = …` 時注入到 **Results 的最前面**（而非 Parameters），
			// 其型別即接收者名稱（如 `json.parse` 的 `json`）。它並非使用者
			// 撰寫的型別標註，套用跨模組前綴檢查會產生誤報：例如 JS 平台宣告
			// `json.parse = …` / `timer.x = …`，其 receiver 名稱 `json` /
			// `timer` 恰好與 std 模組中的 struct 同名，於是噴出
			// "type 'json' not found; did you mean 'json.json'?"——但這裡的
			// `json` 是 JS 全域命名空間而非型別。故跳過 Results[0] 的合成
			// receiver，只檢查使用者實際撰寫的參數 / 回傳型別。
			resStart := 0
			if s.IsMethodDef && len(s.Results) > 0 {
				resStart = 1
			}
			// 既有行為：方法定義的輸入參數從首個起全檢（receiver 不在
			// Parameters 中），此處保留 paramStart 以不變動既有輸入檢查語意。
			paramStart := 0
			if s.IsMethodDef && len(s.Parameters) > 0 {
				paramStart = 1
			}
			for _, p := range s.Parameters[paramStart:] {
				if p.Type != nil {
					checkType(p.Type, p.Token.Line, p.Token.Column)
				}
			}
			for _, r := range s.Results[resStart:] {
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
	// The value records the SET OF OWNING MODULES ("" = the main program) that
	// define each bare name: `module.fn()` may only be flattened to the bare
	// `fn()` when `fn` actually belongs to `module`. Without the owner check a
	// qualified std call like `fs.is-file()` was hijacked by any same-named
	// user function `is-file` from an unrelated module — std `#{buildin}` stubs
	// are never merged into the program (transpiler skips BuiltinStub), so `fs`
	// owns no definition here and only the user's bare `is-file` exists. When
	// that user function's body itself calls `fs.is-file()`, the flattening made
	// it call ITSELF: infinite recursion → stack-overflow SIGSEGV at runtime.
	// Keeping the DotExpression lets codegen route the call to the builtin.
	moduleFns := make(map[string]map[string]bool)
	addModFn := func(name, owner string) {
		if moduleFns[name] == nil {
			moduleFns[name] = map[string]bool{}
		}
		moduleFns[name][owner] = true
	}
	// Collect top-level constant names (LetStatement) — these are module-level
	// constants like `BASE64-STD` (from encoding/base64.no), used to rewrite
	// module.CONST dotted accesses to bare constant references.
	moduleConsts := make(map[string]bool)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if !fd.IsMethodDef {
				addModFn(fd.Name, parser.GetModuleOwner(stmt))
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
					addModFn(ls.Name.Value, parser.GetModuleOwner(stmt))
				}
			}
		}
	}
	for _, stmt := range program.Statements {
		resolveModuleCallsInStmt(stmt, program.Sem, modSet, moduleFns, moduleConsts, prefixedFns)
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
func resolveModuleCallsInStmt(stmt parser.Statement, sem *parser.SemanticContext, modSet map[string]bool, moduleFns map[string]map[string]bool, moduleConsts map[string]bool, prefixedFns map[string]bool) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveModuleCallsInExpr(s.Expression, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			s.Value = resolveModuleCallsInExpr(s.Value, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, sem, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveModuleCallsInStmt(bodyStmt, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveModuleCallsInExpr(s.Condition, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if s.Init != nil {
			resolveModuleCallsInStmt(s.Init, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if s.Update != nil {
			resolveModuleCallsInStmt(s.Update, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveModuleCallsInStmt(bodyStmt, sem, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
	}
}
func resolveModuleCallsInExpr(expr parser.Expression, sem *parser.SemanticContext, modSet map[string]bool, moduleFns map[string]map[string]bool, moduleConsts map[string]bool, prefixedFns map[string]bool) parser.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		// For curried calls (e.g. `mod.fn(args)(out1, out2)`), the outer
		// CallExpression's Function is itself a CallExpression. Recurse into
		// it first so the inner module-qualified name gets resolved.
		if _, isCall := e.Function.(*parser.CallExpression); isCall {
			e.Function = resolveModuleCallsInExpr(e.Function, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		// Check if this is a module.fn() call (single or multi-level).
		// Only rewrite when the function property is a known module-level function
		// and the receiver chain matches a known module ShortName.
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			modPath, fnName := extractModulePathAndFunc(dot)
			if modPath != "" && modSet[modPath] {
				// 模組短名：多層路徑（crypto/sha256）取最後一段
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
					// 留一筆「這是模組限定自由函式呼叫」。它與攤平後的方法呼叫
					// 形狀完全相同（Arguments[0] 是普通實參，不是接收者），
					// 兩者共用同一個「帶點 Identifier」命名空間；記錄下來才能
					// 區分，否則模組自由函式的首實參會被誤算成「被改寫」。
					if sem != nil {
						sem.SetCallee(e, parser.CalleeModuleFn)
					}
				} else if owners := moduleFns[fnName]; owners[short] {
					// We are in this branch because fnName is a real top-level
					// module function OWNED BY THIS MODULE (the module `short`
					// appears in its owner set). For a
					// module.fn() call the module function is the correct target
					// even when fnName also names a builtin method — e.g.
					// math.degrees is the std function (def @degrees), NOT the
					// f64 value method. Rewriting to the bare name lets the call
					// resolve to the (unprefixed) definition. The previous
					// builtin-method short-circuit here wrongly kept
					// math.degrees as a DotExpression, which codegen emitted as
					// @math.degrees while the def was @degrees → undefined symbol.
					// Before rewriting module.fn() → fn(), still check if the
					// module also defines a std struct method named
					// module.module.fn (e.g. json.json.parse); if so keep the
					// DotExpression so codegen routes it to module.module.fn.
					stdMethodKey := short + "." + short + "." + fnName
					methodSigs := CollectStdMethodSigs()
					if _, isStdMethod := methodSigs[stdMethodKey]; isStdMethod {
						// Keep as module.fn() DotExpression — codegen will
						// resolve it to module.module.fn via the std struct
						// method dispatch path.
					} else {
						// Rewrite to direct function call
						e.Function = &parser.Identifier{
							Token: lexer.Token{Type: lexer.IDENT, Literal: fnName},
							Value: fnName,
						}
					}
				}
			}
		}
		// Recurse into arguments
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveModuleCallsInExpr(arg, sem, modSet, moduleFns, moduleConsts, prefixedFns)
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
		e.Receiver = resolveModuleCallsInExpr(e.Receiver, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		return e

	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveModuleCallsInExpr(e.Right, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Consequence != nil {
			e.Consequence = resolveModuleCallsInExpr(e.Consequence, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Alternative != nil {
			e.Alternative = resolveModuleCallsInExpr(e.Alternative, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveModuleCallsInExpr(e.Condition, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Consequence != nil {
			for _, bodyStmt := range e.Consequence.Statements {
				resolveModuleCallsInStmt(bodyStmt, sem, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
		if e.Alternative != nil {
			for _, bodyStmt := range e.Alternative.Statements {
				resolveModuleCallsInStmt(bodyStmt, sem, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
		return e

	case *parser.GroupedExpression:
		if e.Expression != nil {
			e.Expression = resolveModuleCallsInExpr(e.Expression, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Index != nil {
			e.Index = resolveModuleCallsInExpr(e.Index, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		return e

	case *parser.SliceExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Range != nil {
			if e.Range.Start != nil {
				e.Range.Start = resolveModuleCallsInExpr(e.Range.Start, sem, modSet, moduleFns, moduleConsts, prefixedFns)
			}
			if e.Range.End != nil {
				e.Range.End = resolveModuleCallsInExpr(e.Range.End, sem, modSet, moduleFns, moduleConsts, prefixedFns)
			}
		}
		return e

	case *parser.AssignExpression:
		if e.Left != nil {
			e.Left = resolveModuleCallsInExpr(e.Left, sem, modSet, moduleFns, moduleConsts, prefixedFns)
		}
		if e.Value != nil {
			e.Value = resolveModuleCallsInExpr(e.Value, sem, modSet, moduleFns, moduleConsts, prefixedFns)
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
		// Only method definitions carry a `self` receiver that needs the
		// `self.method(args)` → `Type.method(self, args)` desugaring below.
		if !fd.IsMethodDef {
			continue
		}
		// Derive the receiver type.
		//
		//   • Explicit-self methods declare `self` as their first parameter:
		//       txt.to-hex = (self txt, ...) (...)  → selfType = "txt"
		//   • Implicit-self methods omit the parameter but encode the receiver
		//     type in the method-name prefix (Type.method = ()):
		//       txt.from-hex = () (...)  → selfType = "txt"
		//       mypkg.mystruct.do = () (...)  → selfType = "mypkg.mystruct"
		//
		// Historically only the explicit-self form was handled, so every
		// `Type.method = ()` method (the dominant std style) left its bare
		// `.method()` calls as `DotExpression{self, prop}`. Codegen dispatches
		// those correctly *only* when the call is a direct CallExpression; when
		// a bare `.method()` is nested (e.g. the LHS of a binary op, or after
		// the method is inlined into a non-method function) the dispatch is
		// lost and codegen emits a malformed `call void @self.method()` with an
		// empty receiver — producing invalid IR such as `sdiv i64 , 2`.
		// Desugaring here converts them to explicit `Type.method(self, args)`
		// calls, which survive inlining and every expression context uniformly.
		var selfType string
		if len(fd.Results) > 0 && fd.Results[0].Name == "self" && fd.Results[0].Type != nil {
			selfType = fd.Results[0].Type.String()
		} else if parts := strings.Split(fd.Name, "."); len(parts) >= 2 {
			// Receiver type is every segment except the final method name.
			selfType = strings.Join(parts[:len(parts)-1], ".")
		}
		if selfType == "" {
			continue
		}
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
		resolveMethodCallsInStmt(stmt, program.Sem, typeOwner, definedMethods)
	}
}
func resolveMethodCallsInStmt(stmt parser.Statement, sem *parser.SemanticContext, typeOwner map[string]string, definedMethods map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			s.Expression = resolveMethodCallsInExpr(s.Expression, sem, typeOwner, definedMethods)
		}
	case *parser.LetStatement:
		if s.Value != nil {
			s.Value = resolveMethodCallsInExpr(s.Value, sem, typeOwner, definedMethods)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			s.Value = resolveMethodCallsInExpr(s.Value, sem, typeOwner, definedMethods)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveMethodCallsInStmt(bodyStmt, sem, typeOwner, definedMethods)
			}
		}
	case *parser.BlockStatement:
		for _, bodyStmt := range s.Statements {
			resolveMethodCallsInStmt(bodyStmt, sem, typeOwner, definedMethods)
		}
	case *parser.ForStatement:
		if s.Condition != nil {
			s.Condition = resolveMethodCallsInExpr(s.Condition, sem, typeOwner, definedMethods)
		}
		if s.Init != nil {
			resolveMethodCallsInStmt(s.Init, sem, typeOwner, definedMethods)
		}
		if s.Update != nil {
			resolveMethodCallsInStmt(s.Update, sem, typeOwner, definedMethods)
		}
		if s.Body != nil {
			for _, bodyStmt := range s.Body.Statements {
				resolveMethodCallsInStmt(bodyStmt, sem, typeOwner, definedMethods)
			}
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			s.ReturnValue = resolveMethodCallsInExpr(s.ReturnValue, sem, typeOwner, definedMethods)
		}
	}
}
func resolveMethodCallsInExpr(expr parser.Expression, sem *parser.SemanticContext, typeOwner map[string]string, definedMethods map[string]bool) parser.Expression {
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
							// 這也是**方法呼叫**：源碼寫 `Type.method(recv, …)` 時，
							// 接收者是使用者自己寫在實參表首位的，改寫只是把被呼叫
							// 者補成完整限定名，實參不動 ⇒ Arguments[0] 仍是接收者，
							// 與攤平形態語意相同（故同樣記 CalleeMethod）。
							if sem != nil {
								sem.SetCallee(e, parser.CalleeMethod)
							}
							break
						}
					}
				}
			}
		}
		// Recurse into arguments and nested calls
		if innerCall, ok := e.Function.(*parser.CallExpression); ok {
			e.Function = resolveMethodCallsInExpr(innerCall, sem, typeOwner, definedMethods)
		}
		for i, arg := range e.Arguments {
			e.Arguments[i] = resolveMethodCallsInExpr(arg, sem, typeOwner, definedMethods)
		}
		return e
	case *parser.DotExpression:
		e.Receiver = resolveMethodCallsInExpr(e.Receiver, sem, typeOwner, definedMethods)
		return e
	case *parser.InfixExpression:
		if e.Left != nil {
			e.Left = resolveMethodCallsInExpr(e.Left, sem, typeOwner, definedMethods)
		}
		if e.Right != nil {
			e.Right = resolveMethodCallsInExpr(e.Right, sem, typeOwner, definedMethods)
		}
		return e
	case *parser.PrefixExpression:
		if e.Right != nil {
			e.Right = resolveMethodCallsInExpr(e.Right, sem, typeOwner, definedMethods)
		}
		return e
	case *parser.IndexExpression:
		if e.Left != nil {
			e.Left = resolveMethodCallsInExpr(e.Left, sem, typeOwner, definedMethods)
		}
		if e.Index != nil {
			e.Index = resolveMethodCallsInExpr(e.Index, sem, typeOwner, definedMethods)
		}
		return e
	case *parser.IfExpression:
		if e.Condition != nil {
			e.Condition = resolveMethodCallsInExpr(e.Condition, sem, typeOwner, definedMethods)
		}
		if e.Consequence != nil {
			for _, bs := range e.Consequence.Statements {
				resolveMethodCallsInStmt(bs, sem, typeOwner, definedMethods)
			}
		}
		if e.Alternative != nil {
			for _, bs := range e.Alternative.Statements {
				resolveMethodCallsInStmt(bs, sem, typeOwner, definedMethods)
			}
		}
		return e
	case *parser.ConditionalExpression:
		if e.Condition != nil {
			e.Condition = resolveMethodCallsInExpr(e.Condition, sem, typeOwner, definedMethods)
		}
		if e.Consequence != nil {
			e.Consequence = resolveMethodCallsInExpr(e.Consequence, sem, typeOwner, definedMethods)
		}
		if e.Alternative != nil {
			e.Alternative = resolveMethodCallsInExpr(e.Alternative, sem, typeOwner, definedMethods)
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
	// If f.Type is already an ArrayType or SliceType, its String() already
	// includes the [N] or [] prefix. Only apply the prefix for legacy fields
	// where Type is a plain named type with separate ArraySize/IsSlice flags.
	switch f.Type.(type) {
	case *parser.ArrayType, *parser.SliceType:
		return typeStr
	}
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
				// Don't rewrite builtin methods on builtin types (e.g. []byte.len,
				// []i64.len, str.len). Builtins are handled by the codegen via
				// DotExpression dispatch + ForwardFunc. Rewriting them to
				// Type.method(self, ...) would create undefined function calls
				// (e.g. _LB__RB_byte.len) or bypass the inline field-access path.
				isBuiltinMethodOnBuiltinType := false
				if strings.HasPrefix(selfType, "[]") {
					if builtin.FindBuiltinMethod("vec."+dot.Property) != nil ||
						builtin.FindBuiltinMethod(dot.Property) != nil ||
						builtin.FindBuiltinMethod(selfType+"."+dot.Property) != nil {
						isBuiltinMethodOnBuiltinType = true
					}
				} else if strings.HasPrefix(selfType, "[") {
					if builtin.FindBuiltinMethod("arr."+dot.Property) != nil ||
						builtin.FindBuiltinMethod(selfType+"."+dot.Property) != nil {
						isBuiltinMethodOnBuiltinType = true
					}
				} else if selfType == "str" || selfType == "byte" || selfType == "char" || selfType == "bool" ||
					selfType == "i64" || selfType == "u64" || selfType == "i32" || selfType == "u32" ||
					selfType == "i16" || selfType == "u16" || selfType == "i8" || selfType == "u8" ||
					selfType == "f64" || selfType == "f32" {
					if builtin.FindBuiltinMethod(selfType+"."+dot.Property) != nil ||
						builtin.FindBuiltinMethod(dot.Property) != nil {
						isBuiltinMethodOnBuiltinType = true
					}
				}
				if !isBuiltinMethodOnBuiltinType {
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
	// Method definitions have self as the first output parameter (Results[0]).
	// Exclude it from ResultTypes so return-count validation (lookupReturnCount)
	// sees the real number of return values, not including the implicit self.
	resultParams := fd.Results
	if fd.IsMethodDef && len(resultParams) > 0 && resultParams[0].Name == "self" {
		resultParams = resultParams[1:]
	}
	results := make([]paramInfo, len(resultParams))
	for i, r := range resultParams {
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

// validationMu protects validationStructFields, validationConcreteTypeAliases,
// validationFuncTypes, and validationStructNames from concurrent writes during
// parallel vet/build (e.g. LSP server processes multiple documents concurrently).
// Without the mutex, parallel goroutines racing on the map writes cause
// "concurrent map writes" fatal panics.
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
