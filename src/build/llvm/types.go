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
	// [N]type → [N x llvmType]
	if strings.HasPrefix(nolangType, "[") {
		closeBracket := strings.IndexByte(nolangType, ']')
		if closeBracket > 0 {
			sizeStr := nolangType[1:closeBracket]
			elemType := nolangType[closeBracket+1:]
			llvmElem := g.mapToLLVMType(elemType)
			if sizeStr == "" {
				// []type → 切片（用 i8* 表示）
				return llvmElem + "*"
			}
			return "[" + sizeStr + " x " + llvmElem + "]"
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
		return "%str"
	case "str-smail":
		return "%str-smail"
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
