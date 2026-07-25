package wasm

import (
	"bytes"
	"encoding/binary"

	"github.com/lizongying/nolang/parser"
)

// ---- Task 8：堆與複合型別 ----
//
// 記憶體佈局（線性記憶體）：
//
//	0            .. HeapBase-1        : WASI runtime + 字串常數池 + scratch
//	HeapBase     .. heapPtr-1          : 編譯期靜態配置（str descriptor 等）
//	heapPtr      ..                    : 運行期 bump allocator 起點
//
// 所有複合型別（str-long / vec / arr / struct）均以「描述符指標」表示，
// 變數本身持有單一 i32 local（描述符位址）。描述符格式：
//
//	str / vec / arr descriptor（12 bytes）:
//	  +0  : len  (i32)
//	  +4  : cap  (i32)
//	  +8  : data (i32, 指向位元組/元素緩衝區)
//
//	struct：欄位依自然對齊順序排列，無固定標頭。

// HeapBase 為堆配置起點。低於此位址的區域保留給字串常數池與 WASI scratch
// （IovecBase=4096、NwrittenPtr=4104、ItoaBuffer=[8192,8224)）。
// 靜態描述符（str literal descriptor）由 g.heapOffset 向上配置；
// 運行期 malloc 自 heapPtr global 開始向上配置。
const HeapBase uint32 = 8224

// DescriptorHeaderSize 為 str/vec/arr 描述符標頭大小（len + cap + data = 12 bytes）。
const DescriptorHeaderSize uint32 = 12

// DescriptorFieldLen / Cap / Data 為描述符欄位偏移。
const (
	DescriptorFieldLen  uint32 = 0
	DescriptorFieldCap  uint32 = 4
	DescriptorFieldData uint32 = 8
)

// mallocIdx / freeIdx 為 runtime malloc/free 的 function index。
// 佈局：fd_write=0, proc_exit=1, _start=2, malloc=3, free=4, 用戶函數=5+
const (
	mallocIdx uint32 = 3
	freeIdx   uint32 = 4
)

// heapPtrGlobalIdx 為 heapPtr global 的 index。
// heapPtr 是 mutable i32 global，初始值 = g.heapOffset（所有編譯期靜態配置之後）。
const heapPtrGlobalIdx uint32 = 0

// ValKind 標記複合型別的種類（與 ValType I32 一起使用，區分指標的指向內容）。
type ValKind int

const (
	KindScalar ValKind = iota // 純量（i32/i64/f32/f64）
	KindStr                   // str 描述符指標
	KindVec                   // vec 描述符指標
	KindArr                   // arr 描述符指標
	KindStruct                // struct 指標
)

// StructFieldLayout 描述單一 struct 欄位的記憶體佈局。
type StructFieldLayout struct {
	Name   string
	Type   ValType
	Kind   ValKind
	Size   uint32
	Offset uint32
}

// StructLayout 描述一個 struct 的完整欄位佈局。
type StructLayout struct {
	Name   string
	Fields []StructFieldLayout
	Size   uint32
}

// valTypeSize 回傳 ValType 的位元組大小。
func valTypeSize(t ValType) uint32 {
	switch t {
	case I32, F32:
		return 4
	case I64, F64:
		return 8
	}
	return 8
}

// alignTo 將 offset 向上對齊至 alignment 的倍數。
func alignTo(offset, alignment uint32) uint32 {
	return (offset + alignment - 1) & ^(alignment - 1)
}

// computeStructLayout 由 StructDefinition 計算欄位偏移與總大小。
// 所有指標型別（str/vec/struct 欄位）以 4 位元組對齊；純量依自然對齊。
func computeStructLayout(sd *parser.StructDefinition) *StructLayout {
	layout := &StructLayout{Name: sd.Name}
	var offset uint32 = 0
	var maxAlign uint32 = 8
	for _, f := range sd.Fields {
		t := ValTypeFromName(f.Type)
		kind := kindFromType(f.Type)
		size := valTypeSize(t)
		align := size
		if align > maxAlign {
			maxAlign = align
		}
		offset = alignTo(offset, align)
		layout.Fields = append(layout.Fields, StructFieldLayout{
			Name:   f.Name,
			Type:   t,
			Kind:   kind,
			Size:   size,
			Offset: offset,
		})
		offset += size
	}
	layout.Size = alignTo(offset, maxAlign)
	if layout.Size == 0 {
		layout.Size = 8 // 空結構體至少配置 8 位元組以避免零長度配置
	}
	return layout
}

// kindFromType 由 parser.Type 推斷 ValKind。
func kindFromType(t parser.Type) ValKind {
	if t == nil {
		return KindScalar
	}
	switch tt := t.(type) {
	case *parser.SliceType:
		return KindVec
	case *parser.ArrayType:
		return KindArr
	case *parser.NamedType:
		switch tt.Value {
		case "str":
			return KindStr
		}
		// 用戶自訂型別名稱視為 struct（由 structDefs 驗證）
		return KindStruct
	}
	return KindScalar
}

// ---- 編譯期靜態配置（用於 str literal descriptor） ----

// compileTimeAlloc 在編譯期配置 size 位元組的靜態記憶體，回傳其 offset。
// 靜態配置位於 HeapBase 起向上成長，不經過 runtime malloc。
// 配置內容透過 data section 寫入；呼叫端負責記錄寫入的位元組。
func (g *Generator) compileTimeAlloc(size uint32) uint32 {
	off := g.heapOffset
	g.heapOffset += size
	return off
}

// addStrDescriptor 為字串常數建立靜態 str 描述符，回傳描述符 offset。
// 描述符內容（len, cap, data）寫入 data section；data 指向字串位元組。
// 同一字串內容只會建立一份描述符。
func (g *Generator) addStrDescriptor(s string) uint32 {
	if off, ok := g.strDescriptorPool[s]; ok {
		return off
	}
	bytesOff := g.addStringLiteral(s)
	descOff := g.compileTimeAlloc(DescriptorHeaderSize)
	// 描述符內容：[len:u32 LE][cap:u32 LE][data:u32 LE]
	desc := make([]byte, DescriptorHeaderSize)
	binary.LittleEndian.PutUint32(desc[0:], uint32(len(s)))
	binary.LittleEndian.PutUint32(desc[4:], uint32(len(s)))
	binary.LittleEndian.PutUint32(desc[8:], bytesOff)
	g.staticDataSegments = append(g.staticDataSegments, DataSegment{
		offset: descOff,
		bytes:  desc,
	})
	g.strDescriptorPool[s] = descOff
	return descOff
}

// ---- 運行期 malloc/free runtime 函數 ----

// declareMallocFree 宣告 runtime malloc 與 free 的函數簽名（addFunction）。
// 必須在 _start 宣告後、用戶函數宣告前呼叫，以固定 function index。
// 實際程式碼主體由 emitMallocFreeCode 於所有宣告完成後發射，
// 確保 functions 與 codes 順序一致（function index = import_count + position）。
//
// 函數 index 固定為 mallocIdx=3、freeIdx=4，在 _start 之後、用戶函數之前。
func (g *Generator) declareMallocFree() {
	// malloc(i32 size) -> i32 ptr
	mallocType := g.internType([]ValType{I32}, []ValType{I32})
	g.addFunction(mallocType) // index 3
	g.funcTable["malloc"] = mallocIdx
	g.funcParams["malloc"] = []ValType{I32}
	g.funcParamNames["malloc"] = []string{"size"}
	g.funcResults["malloc"] = []ValType{I32}
	g.funcResultNames["malloc"] = []string{"ptr"}

	// free(i32 ptr) -> ()
	freeType := g.internType([]ValType{I32}, nil)
	g.addFunction(freeType) // index 4
	g.funcTable["free"] = freeIdx
	g.funcParams["free"] = []ValType{I32}
	g.funcParamNames["free"] = []string{"ptr"}
	g.funcResults["free"] = nil
	g.funcResultNames["free"] = nil
}

// emitMallocFreeCode 發射 runtime malloc 與 free 的程式碼主體（addCode）。
// 必須在 _start 主體發射後、用戶函數主體發射前呼叫，以保持 codes 與 functions 順序一致。
//
// malloc(size: i32) -> i32：bump allocator，自 heapPtr global 向上配置。
// free(ptr: i32)：no-op（最小可用實作；free-list 留待後續優化）。
func (g *Generator) emitMallocFreeCode() {
	// malloc 主體
	var mbuf bytes.Buffer
	// 對齊 size 至 8 位元組
	// size = (size + 7) & ~7
	mbuf.WriteByte(OpLocalGet)
	writeU32LEB(&mbuf, 0) // size (param 0)
	mbuf.WriteByte(OpI32Const)
	writeSLEB(&mbuf, 7)
	mbuf.WriteByte(OpI32Add)
	mbuf.WriteByte(OpI32Const)
	writeSLEB(&mbuf, -8)
	mbuf.WriteByte(OpI32And)
	mbuf.WriteByte(OpLocalSet)
	writeU32LEB(&mbuf, 0) // size = aligned

	// ptr = heapPtr; heapPtr = ptr + size; return ptr
	mbuf.WriteByte(OpGlobalGet)
	writeU32LEB(&mbuf, heapPtrGlobalIdx)
	mbuf.WriteByte(OpLocalTee)
	writeU32LEB(&mbuf, 1) // local 1 = $ptr（宣告的 local）
	mbuf.WriteByte(OpLocalGet)
	writeU32LEB(&mbuf, 0) // size
	mbuf.WriteByte(OpI32Add)
	mbuf.WriteByte(OpGlobalSet)
	writeU32LEB(&mbuf, heapPtrGlobalIdx)
	mbuf.WriteByte(OpLocalGet)
	writeU32LEB(&mbuf, 1) // return ptr

	g.addCode([]LocalGroup{{Count: 1, Type: I32}}, mbuf.Bytes())

	// free 主體：no-op（參數 ptr 自動由函數簽名消費）
	g.addCode(nil, []byte{})
}

// emitMallocCall 發射 call malloc。堆疊需求：[size: i32]。結果：[ptr: i32]。
func (g *Generator) emitMallocCall(sb *bytes.Buffer) {
	sb.WriteByte(OpCall)
	writeU32LEB(sb, mallocIdx)
}

// emitFreeCall 發射 call free。堆疊需求：[ptr: i32]。結果：無。
func (g *Generator) emitFreeCall(sb *bytes.Buffer) {
	sb.WriteByte(OpCall)
	writeU32LEB(sb, freeIdx)
}

// ---- 記憶體 load/store 輔助（i32/i64） ----

// emitI32Load 發射 i32.load align=2 offset=0。堆疊需求：[addr]。結果：[val: i32]。
func (g *Generator) emitI32Load(sb *bytes.Buffer) {
	sb.WriteByte(OpI32Load)
	sb.WriteByte(0x02) // align = log2(4)
	writeU32LEB(sb, 0)
}

// emitI64Load 發射 i64.load align=3 offset=0。堆疊需求：[addr]。結果：[val: i64]。
func (g *Generator) emitI64Load(sb *bytes.Buffer) {
	sb.WriteByte(OpI64Load)
	sb.WriteByte(0x03) // align = log2(8)
	writeU32LEB(sb, 0)
}

// emitI64Store 發射 i64.store align=3 offset=0。堆疊需求：[addr, val: i64]。
func (g *Generator) emitI64Store(sb *bytes.Buffer) {
	sb.WriteByte(OpI64Store)
	sb.WriteByte(0x03) // align = log2(8)
	writeU32LEB(sb, 0)
}

// emitI32Load8U 發射 i32.load8_u align=0 offset=0。堆疊需求：[addr]。結果：[val: i32]。
func (g *Generator) emitI32Load8U(sb *bytes.Buffer) {
	sb.WriteByte(OpI32Load8U)
	sb.WriteByte(0x00)
	writeU32LEB(sb, 0)
}

// emitI32ConstOffset 發射 i32.const offset（位址計算用）。
func (g *Generator) emitI32ConstOffset(sb *bytes.Buffer, offset uint32) {
	writeI32ConstU(sb, offset)
}

// emitDescriptorLoadField 發射從描述符載入指定欄位的指令。
// descPtr 在堆疊頂端；fieldOffset 為欄位偏移；loadI64=true 載入 i64，否則 i32。
// 堆疊：[descPtr] → [fieldValue]。
func (g *Generator) emitDescriptorLoadField(sb *bytes.Buffer, fieldOffset uint32, loadI64 bool) {
	// [descPtr] → [descPtr + fieldOffset] → load
	g.emitI32ConstOffset(sb, fieldOffset)
	sb.WriteByte(OpI32Add)
	if loadI64 {
		g.emitI64Load(sb)
	} else {
		g.emitI32Load(sb)
	}
}

// ---- bounds check ----

// emitBoundsCheck 發射邊界檢查：若 idx >= len，呼叫 proc_exit(1)。
// 堆疊需求（進入時）：[idx: i32, len: i32]。
// 堆疊離開時：空（idx 與 len 已被消費）。
//
// 結構：
//
//	if idx >= len:
//	    proc_exit(1)
//	    unreachable
//
// 使用 block + br_if 反轉條件：if (idx < len) br 0; else fall through to exit。
func (g *Generator) emitBoundsCheck(sb *bytes.Buffer) {
	// 堆疊：[idx, len] → i32.ge_u → [cond]
	sb.WriteByte(OpI32GeU)
	// 若 idx >= len 為真，跳出 block 並執行 proc_exit
	g.emitIfVoid(sb)
	writeI32ConstU(sb, 1)
	sb.WriteByte(OpCall)
	writeU32LEB(sb, procExitIdx)
	sb.WriteByte(OpUnreachable)
	g.emitEnd(sb)
}
