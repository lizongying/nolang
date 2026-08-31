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
			return "%hashmap-" + keyType + "-" + parser.SanitizeLLVMTypeName(valueType)
		}
	}
	// [K]V → %hashmap-K-V (map type in [K]V form; sanitize value type for LLVM)
	if strings.HasPrefix(nolangType, "[") {
		closeBracket := strings.IndexByte(nolangType, ']')
		if closeBracket > 0 && closeBracket < len(nolangType)-1 {
			keyStr := nolangType[1:closeBracket]
			valStr := nolangType[closeBracket+1:]
			// Only treat as map if key is a builtin type name (not a number or empty)
			if keyStr != "" && keyStr != "?" && !isNumericStr(keyStr) && isLLVMBuiltinTypeName(keyStr) {
				return "%hashmap-" + keyStr + "-" + parser.SanitizeLLVMTypeName(valStr)
			}
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

	// 單具體型別別名解析：查詢 concreteTypeAliases，命中則遞歸解析底層型別
	// 例如 "fd" → 底層 "i64" → "i64"；"bytes" → 底層 "[]byte" → "%vec"
	if g.concreteTypeAliases != nil {
		if underlying, ok := g.concreteTypeAliases[nolangType]; ok {
			return g.mapToLLVMType(underlying.String())
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
	case "i128":
		return "i128"
	case "u8":
		return "u8"
	case "u16":
		return "u16"
	case "u32":
		return "u32"
	case "u64":
		return "u64"
	case "u128":
		return "u128"
	case "f32":
		return "float"
	case "f64":
		return "double"
	case "bool":
		return "i1"
	case "str":
		return "%str-long"
	case "ptr":
		return "i8*"
	case "byte":
		return "u8"
	case "char":
		return "i32"
	default:
		return "i64"
	}
}

// toLLVMType converts an internal type string (which may carry signedness info
// like "u8"/"u16"/"u32"/"u64") to the actual LLVM IR type string.
// LLVM IR does not distinguish signed/unsigned integers, so "u8" → "i8", etc.
// Use this when emitting LLVM IR instructions (load, store, zext, sext, GEP, etc.).
// Use mapToLLVMType() when storing into varTypes/arrayElemTypes to preserve
// signedness information for later widening decisions.
// Also handles array types like "[3 x u8]" → "[3 x i8]".
func toLLVMType(t string) string {
	// Handle array types: "[N x T]" → "[N x toLLVMType(T)]"
	if len(t) > 5 && strings.HasPrefix(t, "[") && strings.Contains(t, " x ") {
		closeBracket := strings.Index(t, "]")
		if closeBracket > 0 {
			spaceIdx := strings.Index(t, " x ")
			if spaceIdx > 0 && spaceIdx < closeBracket {
				size := t[1:spaceIdx]
				elemType := t[spaceIdx+3 : closeBracket]
				return "[" + size + " x " + toLLVMType(elemType) + "]"
			}
		}
	}
	switch t {
	case "u8":
		return "i8"
	case "u16":
		return "i16"
	case "u32":
		return "i32"
	case "u64":
		return "i64"
	case "u128":
		return "i128"
	default:
		return t
	}
}

// isUnsignedIntType reports whether the internal type string represents
// an unsigned integer type (u8/u16/u32/u64). These types must use zext
// for widening; signed types (i8/i16/i32) must use sext.
func isUnsignedIntType(t string) bool {
	switch t {
	case "u8", "u16", "u32", "u64", "u128":
		return true
	}
	return false
}

// widenExtOp returns the LLVM extension instruction ("zext" or "sext") for
// widening a narrow integer type to a wider one.
//
// - Unsigned types (u8/u16/u32/u64) → "zext" (zero-extend, preserves high bits as 0)
// - Signed types   (i8/i16/i32/i64) → "sext" (sign-extend, replicates sign bit)
// - i1 (bool)      → "zext" (boolean is always 0/1)
//
// This is the single source of truth for all widening decisions, replacing
// the previous scattered hardcoded checks that only covered i8 and missed
// i16/i32 (causing them to be incorrectly zero-extended).
func widenExtOp(valType string) string {
	if valType == "i1" {
		return "zext"
	}
	if isUnsignedIntType(valType) {
		return "zext"
	}
	return "sext"
}

// divOp returns the LLVM division instruction ("sdiv" or "udiv") based on
// the type's signedness. Unsigned types use "udiv", signed types use "sdiv".
func divOp(valType string) string {
	if isUnsignedIntType(valType) {
		return "udiv"
	}
	return "sdiv"
}

// remOp returns the LLVM remainder instruction ("srem" or "urem") based on
// the type's signedness. Unsigned types use "urem", signed types use "srem".
func remOp(valType string) string {
	if isUnsignedIntType(valType) {
		return "urem"
	}
	return "srem"
}

// icmpPred converts a signed comparison predicate to the correct LLVM icmp
// predicate based on the type's signedness. For unsigned types, signed
// predicates (slt/sgt/sle/sge) are converted to their unsigned equivalents
// (ult/ugt/ule/uge). Equality predicates (eq/ne) are returned unchanged.
//
// LLVM IR does not distinguish signed/unsigned types — the distinction is
// made by the comparison predicate. Using "slt" on an unsigned value would
// produce incorrect results when the high bit is set (e.g. u8 value 200
// would be treated as -56).
func icmpPred(op, llvmType string) string {
	if !isUnsignedIntType(llvmType) {
		return op // signed or eq/ne — no change needed
	}
	switch op {
	case "slt":
		return "ult"
	case "sgt":
		return "ugt"
	case "sle":
		return "ule"
	case "sge":
		return "uge"
	}
	return op // eq, ne, etc.
}

// llvmIntBitWidth returns the bit width of an integer type string (including
// unsigned variants). Returns 64 for unknown/non-integer types.
func llvmIntBitWidth(t string) int {
	switch t {
	case "i1":
		return 1
	case "i8", "u8":
		return 8
	case "i16", "u16":
		return 16
	case "i32", "u32":
		return 32
	case "i64", "u64":
		return 64
	case "i128", "u128":
		return 128
	default:
		return 64
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
		elemLLVMType = toLLVMType(g.mapToLLVMType(at.Elem.String()))
	}
	return fmt.Sprintf("[%d x %s]", size, elemLLVMType)
}

// resolveOutputParamLLVMType returns the LLVM type for an output (result) parameter.
// bool (i1) is widened to i64 to match the caller's convention: callers allocate
// i64 allocas for bool targets (see collectVarDeclsFromStmt i1→i64 conversion).
// Without this, the function signature would use i1* while the caller passes i64*,
// causing the function to write only 1 byte but the caller to read 8 bytes,
// resulting in garbage in the upper 7 bytes (bug15: multi-return bool corruption).
func (g *Generator) resolveOutputParamLLVMType(t parser.Type) string {
	llvmType := g.resolveParamLLVMType(t)
	if llvmType == "i1" {
		return "i64"
	}
	return llvmType
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
	"fopen":       true,
	"fgets":       true,
	"fclose":      true,
	"exit":        true,
}

// variadicFuncSigs 記錄變參 C 函數的完整 LLVM 函數類型簽名。
// LLVM IR 中呼叫變參函數時必須帶上函數類型簽名，否則變參部分
// 可能無法正確傳遞（macOS arm64 上會導致參數錯位，例如 @open
// 的 mode 參數被路徑字串首字節覆蓋）。
var variadicFuncSigs = map[string]string{
	"open":    "(i8*, i32, ...)",
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
	case "i128", "u128":
		return 16
	case "%str-long", "%vec":
		// { i64, i64, i64 } = 8 + 8 + 8 = 24 bytes
		return 24
	case "%option":
		// { i64, i64 } = 8 + 8 = 16 bytes
		return 16
	case "%arr":
		// { i64, i64 } = 8 + 8 = 16 bytes
		return 16
	default:
		return 8 // 預設 i64
	}
}

// isLLVMBuiltinTypeName reports whether name is a builtin type name
// usable as a map key (mirrors parser.isBuiltinTypeName).
func isLLVMBuiltinTypeName(name string) bool {
	switch name {
	case "str", "i64", "i32", "i16", "i8", "i128",
		"u64", "u32", "u16", "u8", "u128",
		"bool", "byte", "char",
		"f64", "f32":
		return true
	}
	return false
}

// isNumericStr reports whether s represents a valid integer.
func isNumericStr(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// llvmTypeToNolangName maps an internal LLVM type string (as stored in
// varTypes) back to a human-readable Nolang type name. Used by the 't'
// format spec (print('{x:t}') → prints the type name of x).
func llvmTypeToNolangName(llvmType string) string {
	switch llvmType {
	case "i64":
		return "i64"
	case "i32":
		return "i32"
	case "i16":
		return "i16"
	case "i8":
		return "i8"
	case "i128":
		return "i128"
	case "u64":
		return "u64"
	case "u32":
		return "u32"
	case "u16":
		return "u16"
	case "u8":
		return "u8"
	case "u128":
		return "u128"
	case "double":
		return "f64"
	case "float":
		return "f32"
	case "i1":
		return "bool"
	case "%str-long":
		return "str"
	case "%vec":
		return "[]T"
	case "%arr":
		return "[N]T"
	case "%option":
		return "?T"
	case "i8*":
		return "ptr"
	default:
		// Struct types: "%foo" → "foo"
		if strings.HasPrefix(llvmType, "%") {
			return llvmType[1:]
		}
		// HashMap types: "%hashmap-str-i64" → "[str]i64"
		if strings.HasPrefix(llvmType, "%hashmap-") {
			rest := llvmType[len("%hashmap-"):]
			parts := strings.SplitN(rest, "-", 2)
			if len(parts) == 2 {
				return "[" + parts[0] + "]" + parts[1]
			}
			return rest
		}
		return llvmType
	}
}
