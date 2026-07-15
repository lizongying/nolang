package llvm

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

func (g *Generator) mapToLLVMType(nolangType string) string {
	// Function type: fn(...) -> function pointer (void + by-reference convention)
	// All Nolang functions use void + by-reference, so function pointers are
	// uniformly typed as void (...)* to allow indirect calls without exact signatures.
	if strings.HasPrefix(nolangType, "fn(") {
		return "void (...)*"
	}
	// ?type → option type
	if strings.HasPrefix(nolangType, "?") {
		return "%option"
	}
	// *type → pointer type
	if strings.HasPrefix(nolangType, "*") {
		elemType := nolangType[1:]
		return g.mapToLLVMType(elemType) + "*"
	}
	// Union type: "A | B" → use the first non-err/non-nil type.
	// Synthetic `it` bindings from match desugar use union type strings
	// (e.g. "http2-frame | err", "i64 | err | nil") to represent which
	// option variants a wildcard arm covers. For codegen we only care
	// about the concrete (ok) element type, so extract it.
	if strings.Contains(nolangType, "|") {
		parts := strings.Split(nolangType, "|")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "err" || p == "nil" || p == "" {
				continue
			}
			mapped := g.mapToLLVMType(p)
			if mapped != "i64" {
				return mapped
			}
		}
		return "i64"
	}
	// hashmap-K-V → %hashmap-K-V (callers pass MapType.LLVMName() which returns
	// "hashmap-{key}-{value}", matching the struct name defined in src/std/map/map.no).
	// Kept as a safety net: String() now returns the spec-mandated [K]V form which
	// is ambiguous with [N]T arrays, so codegen must route through LLVMName().
	if strings.HasPrefix(nolangType, "hashmap-") {
		return "%" + nolangType
	}
	// map[K]V → %hashmap-K-V (explicit map[K]V syntax; construct struct name)
	if strings.HasPrefix(nolangType, "map[") {
		closeBracket := strings.IndexByte(nolangType, ']')
		if closeBracket > 0 {
			keyType := nolangType[4:closeBracket]
			valueType := nolangType[closeBracket+1:]
			return "%hashmap-" + keyType + "-" + valueType
		}
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

// arrayTypeToLLVM converts a (possibly nested) ArrayType AST node to its LLVM
// array type string. For [12][16]i64 it returns "[12 x [16 x i64]]".
func (g *Generator) arrayTypeToLLVM(at *parser.ArrayType) string {
	size := int64(0)
	if v, ok := g.constFoldInt(at.Size); ok {
		size = v
	} else if intLit, ok := at.Size.(*parser.IntegerLiteral); ok {
		size = intLit.Value
	}
	var elemLLVMType string
	if inner, ok := at.Elem.(*parser.ArrayType); ok {
		elemLLVMType = g.arrayTypeToLLVM(inner)
	} else {
		elemLLVMType = g.mapToLLVMType(at.Elem.String())
	}
	return fmt.Sprintf("[%d x %s]", size, elemLLVMType)
}

// constFoldInt evaluates a constant integer expression (IntegerLiteral,
// negative IntegerLiteral, CharLiteral, or InfixExpression with +/-/* on
// constants) and returns the folded value. Used for array sizes like [16 + 160].
func (g *Generator) constFoldInt(expr parser.Expression) (int64, bool) {
	if expr == nil {
		return 0, false
	}
	if v, ok := intConstValue(expr); ok {
		return v, true
	}
	if ie, ok := expr.(*parser.InfixExpression); ok {
		left, ok1 := g.constFoldInt(ie.Left)
		right, ok2 := g.constFoldInt(ie.Right)
		if !ok1 || !ok2 {
			return 0, false
		}
		switch ie.Operator {
		case "+":
			return left + right, true
		case "-":
			return left - right, true
		case "*":
			return left * right, true
		}
	}
	return 0, false
}

// extractArrayElemType parses an LLVM array type string like "[12 x [16 x i64]]"
// and returns the element type "[16 x i64]". Returns "" if parsing fails.
func extractArrayElemType(llvmType string) string {
	if !strings.HasPrefix(llvmType, "[") {
		return ""
	}
	// Find the matching closing bracket for the outermost [
	depth := 0
	closeB := -1
	for i, c := range llvmType {
		if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				closeB = i
				break
			}
		}
	}
	if closeB < 0 {
		return ""
	}
	inner := llvmType[1:closeB] // e.g. "12 x [16 x i64]"
	// Find the FIRST " x " at depth 0 (the outermost separator)
	depth = 0
	xIdx := -1
	for i := 0; i < len(inner)-2; i++ {
		if inner[i] == '[' {
			depth++
		} else if inner[i] == ']' {
			depth--
		} else if depth == 0 && inner[i] == ' ' && inner[i+1] == 'x' && inner[i+2] == ' ' {
			xIdx = i
			break
		}
	}
	if xIdx < 0 {
		return ""
	}
	return strings.TrimSpace(inner[xIdx+3:])
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
	"open":        true,
	"read":        true,
	"write":       true,
	"close":       true,
	"mkdir":       true,
	"chmod":       true,
	"unlink":      true,
	"rename":      true,
	"stat":        true,
	"chdir":       true,
	"getcwd":      true,
	"getenv":      true,
	"setenv":      true,
	"getpid":      true,
	"gethostname": true,
	"malloc":      true,
	"free":        true,
	"memcpy":      true,
	"memset":      true,
	"memcmp":      true,
	"printf":      true,
	"sprintf":     true,
	"strcmp":      true,
	"strlen":      true,
	"time":        true,
	"sleep":       true,
	"fopen":       true,
	"fgets":       true,
	"fclose":      true,
	"atoi":        true,
	"strtoull":    true,
	"strtod":      true,
	"fmod":        true,
	"hypot":       true,
	"cbrt":        true,
	"exit":        true,
}

// variadicFuncSigs 記錄變參 C 函數的完整 LLVM 函數類型簽名。
// LLVM IR 中呼叫變參函數時必須帶上函數類型簽名，否則變參部分
// 可能無法正確傳遞（macOS arm64 上會導致參數錯位，例如 @open
// 的 mode 參數被路徑字串首字節覆蓋）。
var variadicFuncSigs = map[string]string{
	"open":    "(i8*, i32, ...)",
	"printf":  "(i8*, ...)",
	"sprintf": "(i8*, i8*, ...)",
	"execlp":  "(i8*, ...)",
}

// clibCallSig 返回呼叫指定 C 函數時應使用的函數類型前綴。
// 對於變參函數返回其完整簽名（如 "(i8*, i32, ...)"），
// 對於非變參函數返回空字串（LLVM 可從 declare 推斷，無需顯式簽名）。
func clibCallSig(fnName string) string {
	return variadicFuncSigs[fnName]
}

// llvmTypeSize returns the size in bytes of a primitive or built-in struct LLVM type.
// Used by vec-push / arr-zero to compute malloc and memcpy sizes — returning a
// too-small value here causes heap buffer overflows (e.g. SIGSEGV when indexing
// a []str slice past ~170 elements, because %str-long is 24 bytes, not 8).
func llvmTypeSize(llvmType string) int64 {
	switch llvmType {
	case "i1", "i8", "u8":
		return 1
	case "i16", "u16":
		return 2
	case "i32", "u32", "float":
		return 4
	case "i64", "u64", "double", "i8*", "i8**", "i8***":
		return 8
	case "%str-long", "%vec":
		// { i64, i64, i8* } = 8 + 8 + 8 = 24 bytes
		return 24
	case "%option":
		// { i64, [16 x i8] } = 8 + 16 = 24 bytes
		return 24
	case "%arr":
		// { i64, i8* } = 8 + 8 = 16 bytes
		return 16
	case "%str-short":
		// { i8, [127 x i8] } = 1 + 127 = 128 bytes
		return 128
	default:
		return 8 // 預設 i64
	}
}
