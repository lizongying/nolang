package llvm

import "strings"

func (g *Generator) mapToLLVMType(nolangType string) string {
	// ?type → option type
	if strings.HasPrefix(nolangType, "?") {
		return "%option"
	}
	// *type → pointer type
	if strings.HasPrefix(nolangType, "*") {
		elemType := nolangType[1:]
		return g.mapToLLVMType(elemType) + "*"
	}
	// [N]type → %arr (built-in struct: arr { len i64, data *any })
	if strings.HasPrefix(nolangType, "[") {
		closeBracket := strings.IndexByte(nolangType, ']')
		if closeBracket > 0 {
			sizeStr := nolangType[1:closeBracket]
			if sizeStr == "" {
				// []type → 切片，使用 %vec (built-in struct: vec { len i64, cap i64, data i8* })
				return "%vec"
			}
			return "%arr"
		}
	}

	// Check if it's a known struct type
	if g.structTypes != nil {
		if _, ok := g.structTypes[nolangType]; ok {
			return "%" + nolangType
		}
	}

	switch nolangType {
	case "i8":
		return "i8"
	case "i16":
		return "i16"
	case "i32":
		return "i32"
	case "i64":
		return "i64"
	case "u8":
		return "i8"
	case "u16":
		return "i16"
	case "u32":
		return "i32"
	case "u64":
		return "i64"
	case "f32":
		return "float"
	case "f64":
		return "double"
	case "bool":
		return "i1"
	case "str":
		return "%str-long"
	case "str-short":
		return "%str-short"
	case "ptr":
		return "i8*"
	case "byte":
		return "i8"
	case "char":
		return "i32"
	default:
		return "i64"
	}
}

// sanitizeLLVMName 將函式名稱中的非法字元替換為合法字元。
// LLVM IR 識別碼只允許字母、數字、_、.、-、$，不允許 [ ] ( ) 等。
// 例如 "[]ord.ast" → "_LB__RB_ord.ast"，"[n]ord.ast" → "_LB_n_RB_ord.ast"
func sanitizeLLVMName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch r {
		case '[':
			sb.WriteString("_LB_")
		case ']':
			sb.WriteString("_RB_")
		case '(':
			sb.WriteString("_LP_")
		case ')':
			sb.WriteString("_RP_")
		case ' ':
			sb.WriteString("_SP_")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// clibFuncNames 是 C 系統調用（decl.go 中 declare 的函式）名稱集合。
// 當用戶定義同名函數時，需要加 "n." 前綴以避免 LLVM IR 中 redefinition 錯誤。
// 其他與 builtin 同名的用戶函數（如 set.remove vs os.remove）不需前綴，
// 走原本的 dispatch 優先級。
var clibFuncNames = map[string]bool{
	"open":      true,
	"read":      true,
	"write":     true,
	"close":     true,
	"mkdir":     true,
	"unlink":    true,
	"rename":    true,
	"stat":      true,
	"chdir":     true,
	"getcwd":    true,
	"getenv":    true,
	"setenv":    true,
	"getpid":    true,
	"gethostname": true,
	"malloc":    true,
	"free":      true,
	"memcpy":    true,
	"memset":    true,
	"memcmp":    true,
	"printf":    true,
	"sprintf":   true,
	"strcmp":    true,
	"strlen":    true,
	"time":      true,
	"sleep":     true,
	"fopen":     true,
	"fgets":     true,
	"fclose":    true,
	"atoi":      true,
	"strtoull":  true,
	"strtod":    true,
	"fmod":      true,
	"hypot":     true,
	"cbrt":      true,
	"exit":      true,
}
