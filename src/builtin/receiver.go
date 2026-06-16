package builtin

// ReceiverKind 區分內建方法接收者類別，替換原始 string ReceiverType
type ReceiverKind uint8

const (
	// ReceiverGlobal 全域頂層函數，無接收者 (max, min, abs 全域呼叫)
	ReceiverGlobal ReceiverKind = iota

	// 內建原生類型
	ReceiverStr
	ReceiverF32
	ReceiverF64
	ReceiverI8
	ReceiverI16
	ReceiverI32
	ReceiverI64
	ReceiverU8
	ReceiverU16
	ReceiverU32
	ReceiverU64
	ReceiverVec
	ReceiverArr
)

// String 實現，方便日誌、LSP、錯誤打印轉文字
func (k ReceiverKind) String() string {
	switch k {
	case ReceiverGlobal:
		return ""
	case ReceiverStr:
		return "str"
	case ReceiverF32:
		return "f32"
	case ReceiverF64:
		return "f64"
	case ReceiverI8:
		return "i8"
	case ReceiverI16:
		return "i16"
	case ReceiverI32:
		return "i32"
	case ReceiverI64:
		return "i64"
	case ReceiverU8:
		return "u8"
	case ReceiverU16:
		return "u16"
	case ReceiverU32:
		return "u32"
	case ReceiverU64:
		return "u64"
	case ReceiverVec:
		return "[]t"
	case ReceiverArr:
		return "[n]t"
	default:
		return "unknown"
	}
}
