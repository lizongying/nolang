package wasm

// Section IDs（WebAssembly binary format §5）。
//
// 參考規格：https://webassembly.github.io/spec/core/binary/modules.html
const (
	CustomSection  byte = 0
	TypeSection    byte = 1
	ImportSection  byte = 2
	FunctionSection byte = 3
	TableSection   byte = 4
	MemorySection  byte = 5
	GlobalSection  byte = 6
	ExportSection  byte = 7
	StartSection   byte = 8
	ElementSection byte = 9
	CodeSection    byte = 10
	DataSection    byte = 11
	DataCountSection byte = 12
)

// ValType 為 WASM 數值型別常數（binary format §2.2.1）。
type ValType byte

const (
	I32 ValType = 0x7F
	I64 ValType = 0x7E
	F32 ValType = 0x7D
	F64 ValType = 0x7C
)

// ImportKind 為 import descriptor 的 kind byte。
type ImportKind byte

const (
	FuncImport   ImportKind = 0
	TableImport  ImportKind = 1
	MemoryImport ImportKind = 2
	GlobalImport ImportKind = 3
)

// ExportKind 為 export descriptor 的 kind byte。
type ExportKind byte

const (
	FuncExport   ExportKind = 0
	TableExport  ExportKind = 1
	MemoryExport ExportKind = 2
	GlobalExport ExportKind = 3
)

// WASM 通用 opcode，保留給 codegen 使用（Task 7+）。
const (
	OpUnreachable byte = 0x00
	OpNop         byte = 0x01
	OpBlock       byte = 0x02
	OpLoop        byte = 0x03
	OpIf          byte = 0x04
	OpElse        byte = 0x05
	OpEnd         byte = 0x0B
	OpBr          byte = 0x0C
	OpBrIf        byte = 0x0D
	OpReturn      byte = 0x0F
	OpCall        byte = 0x10
	OpCallIndirect byte = 0x11
	OpDrop        byte = 0x1A
	OpSelect      byte = 0x1B
	OpLocalGet    byte = 0x20
	OpLocalSet    byte = 0x21
	OpLocalTee    byte = 0x22
	OpGlobalGet   byte = 0x23
	OpGlobalSet   byte = 0x24
	OpI32Load     byte = 0x28
	OpI64Load     byte = 0x29
	OpI32Load8U   byte = 0x2D
	OpI32Store    byte = 0x36
	OpI64Store    byte = 0x37
	OpI32Store8   byte = 0x3A
	OpI32Const    byte = 0x41
	OpI64Const    byte = 0x42
	OpF32Const    byte = 0x43
	OpF64Const    byte = 0x44
	OpI32Eqz      byte = 0x45
	OpI32Eq       byte = 0x46
	OpI32Ne       byte = 0x47
	OpI32LtS      byte = 0x48
	OpI32LtU      byte = 0x49
	OpI32GtS      byte = 0x4A
	OpI32GtU      byte = 0x4B
	OpI32LeS      byte = 0x4C
	OpI32LeU      byte = 0x4D
	OpI32GeS      byte = 0x4E
	OpI32GeU      byte = 0x4F
	OpI64Eqz      byte = 0x50
	OpI64Eq       byte = 0x51
	OpI64Ne       byte = 0x52
	OpI64LtS      byte = 0x53
	OpI64LtU      byte = 0x54
	OpI64GtS      byte = 0x55
	OpI64GtU      byte = 0x56
	OpI64LeS      byte = 0x57
	OpI64LeU      byte = 0x58
	OpI64GeS      byte = 0x59
	OpI64GeU      byte = 0x5A
	OpI32And      byte = 0x71
	OpI32Or       byte = 0x72
	OpI32Xor      byte = 0x73
	OpI32Add      byte = 0x6A
	OpI32Sub      byte = 0x6B
	OpI32Mul      byte = 0x6C
	OpI32Shl      byte = 0x74
	OpI32ShrS     byte = 0x75
	OpI32ShrU     byte = 0x76
	OpI64Add      byte = 0x7C
	OpI64Sub      byte = 0x7D
	OpI64Mul      byte = 0x7E
	OpI64DivS     byte = 0x7F
	OpI64DivU     byte = 0x80
	OpI64RemS     byte = 0x81
	OpI64RemU     byte = 0x82
	OpI32WrapI64       byte = 0xA7
	OpI64ExtendI32U    byte = 0xAD
	OpI64ExtendI32S    byte = 0xAC
	OpF64Add      byte = 0xA0
	OpF64Sub      byte = 0xA1
	OpF64Mul      byte = 0xA2
	OpF64Div      byte = 0xA3
)

// BlockType 常數（block/loop/if 的型別標籤）。
const (
	BlockTypeEmpty byte = 0x40 // void
)

// WriteSection 寫入一個完整的 section：[sectionID][u32 size][body...]。
// body 必須已經是編碼完成的 section 內容（不含長度前綴）。
func WriteSection(w *Writer, sectionID byte, body []byte) {
	w.WriteByte(sectionID)
	w.WriteLEB128(uint32(len(body)))
	w.WriteBytes(body)
}

// FuncType 編碼一個 functype：0x60 + 參數 vec + 結果 vec。
// 對應規格 §2.2.2。
func FuncType(params, results []ValType) []byte {
	w := NewWriter()
	w.WriteByte(0x60)
	w.WriteLEB128(uint32(len(params)))
	for _, p := range params {
		w.WriteByte(byte(p))
	}
	w.WriteLEB128(uint32(len(results)))
	for _, r := range results {
		w.WriteByte(byte(r))
	}
	return w.Bytes()
}

// Import 編碼一個 import：module name + field name + kind byte + descriptor。
// 對 FuncImport，desc 為單一 type index（u32）。
// 對其他 kind，desc 為對應的 table/memory/global descriptor。
func Import(module, name string, kind ImportKind, desc []byte) []byte {
	w := NewWriter()
	w.WriteName(module)
	w.WriteName(name)
	w.WriteByte(byte(kind))
	w.WriteBytes(desc)
	return w.Bytes()
}

// Memory 編碼一個 memory limits：flag + min + (optional max)。
// hasMax=false → flag=0x00, 僅 min。
// hasMax=true  → flag=0x01, min + max。
func Memory(min, max uint32, hasMax bool) []byte {
	w := NewWriter()
	if hasMax {
		w.WriteByte(0x01)
		w.WriteLEB128(min)
		w.WriteLEB128(max)
	} else {
		w.WriteByte(0x00)
		w.WriteLEB128(min)
	}
	return w.Bytes()
}

// Export 編碼一個 export：name + kind byte + index。
func Export(name string, kind ExportKind, index uint32) []byte {
	w := NewWriter()
	w.WriteName(name)
	w.WriteByte(byte(kind))
	w.WriteLEB128(index)
	return w.Bytes()
}

// DataSegment 描述一個 active data segment（綁定到 memory 0）。
// offset 為線性記憶體中的起始位址；bytes 為要寫入的資料。
type DataSegment struct {
	offset uint32
	bytes  []byte
}

// ActiveData 編碼一個 active data segment（memory 0）：
// 0x00 (flag) + offset 表達式 (i32.const <offset>; end) + u32 size + bytes。
// 對應規格 §2.5.2 中 active segment with memory index 0 的形式。
func ActiveData(offset uint32, data []byte) []byte {
	w := NewWriter()
	w.WriteByte(0x00) // active, memory index 0
	w.WriteByte(OpI32Const)
	w.WriteSLEB128(int64(offset))
	w.WriteByte(OpEnd)
	w.WriteLEB128(uint32(len(data)))
	w.WriteBytes(data)
	return w.Bytes()
}

// LocalGroup 描述一組連續、相同型別的局部變數。對應 WASM 的 local group
// 編碼：[count:u32][type:valtype]。
type LocalGroup struct {
	Count uint32
	Type  ValType
}

// FunctionBody 編碼一個函數主體：vec of local groups + code + 0x0B (end opcode)。
// 回傳的內容即為 code section 中單一 code entry 的「body」部分（不含外層的
// size 前綴；code section 的 vec 會在每個 entry 補上 size）。
func FunctionBody(locals []LocalGroup, code []byte) []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(locals)))
	for _, lg := range locals {
		w.WriteLEB128(lg.Count)
		w.WriteByte(byte(lg.Type))
	}
	w.WriteBytes(code)
	w.WriteByte(OpEnd) // 0x0B
	return w.Bytes()
}

// CodeEntry 編碼一個 code section 中的單一 entry：[u32 size][body]。
// body 由 FunctionBody 產生。
func CodeEntry(body []byte) []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(body)))
	w.WriteBytes(body)
	return w.Bytes()
}
