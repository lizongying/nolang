package wasm

import (
	"fmt"

	"github.com/lizongying/nolang/parser"
)

// ---- 內部結構：模組建構狀態 ----

// funcType 記錄一個 functype（參數與結果型別列表）。
type funcType struct {
	params  []ValType
	results []ValType
}

// importEntry 記錄一筆 import。對 FuncImport，typeIndex 指向 g.types。
type importEntry struct {
	module    string
	name      string
	kind      ImportKind
	typeIndex uint32 // 僅 FuncImport 使用
}

// funcIndex 記錄一個「已定義函數」的 type index（不含 import）。
// 已定義函數的 function index = (import 函數數量) + (在 g.functions 中的位置)。
type funcIndex struct {
	typeIndex uint32
}

// exportEntry 記錄一筆 export。
type exportEntry struct {
	name  string
	kind  ExportKind
	index uint32
}

// memoryLimit 記錄一個線性記憶體的 limits。
type memoryLimit struct {
	min    uint32
	max    uint32
	hasMax bool
}

// codeEntry 記錄一個已定義函數的程式碼主體（locals + instructions）。
type codeEntry struct {
	locals []LocalGroup
	code   []byte
}

// Generator 直接從 Nolang AST 發射 WebAssembly 字節碼（Direct WASM Backend）。
// 當 no 以 wasm 形式運行於瀏覽器（無 LLVM 工具鏈可用）時使用此後端，
// 或在原生 no 下透過 --wasm-direct 旗標觸發（用於回歸測試）。
//
// 目前（Task 6 骨架）僅發射最小合法 WASM 模組：WASI imports + _start。
// 完整 AST -> WASM codegen 見 Task 7-9。
type Generator struct {
	// targetGoos / targetGoarch 用於平台變體過濾（#{linux-amd64}、#{wasi-wasm32} 等），
	// 與 llvm.Generator 的對應欄位語義一致。
	targetGoos   string
	targetGoarch string

	// 模組建構狀態
	types    []funcType
	typeKeys map[string]uint32 // (params+results) -> type index，用於去重

	imports   []importEntry
	functions []funcIndex // 已定義函數（不含 import）
	exports   []exportEntry
	memories  []memoryLimit
	codes     []codeEntry
}

// SetTargetPlatform 設定編譯目標平台（GOOS, GOARCH），用於平台變體過濾。
// 空字串時回退到宿主 runtime（與 llvm.Generator.SetTargetPlatform 行為一致）。
func (g *Generator) SetTargetPlatform(goos, goarch string) {
	g.targetGoos = goos
	g.targetGoarch = goarch
}

// Generate 從給定的 Nolang 程式產生完整的 WebAssembly 二進制。
//
// 目前（Task 6 骨架）僅發射最小合法 WASM 模組：
//   - WASI imports: fd_write, proc_exit
//   - 一個匯出的 _start 函數，呼叫 proc_exit(0)
//   - 一個匯出的 memory（min 1 page）
//
// Task 7-9 將加入完整 AST -> WASM codegen。
func (g *Generator) Generate(program *parser.Program) ([]byte, error) {
	g.reset()
	g.setupWASIImports()
	g.emitStartFunction()
	g.emitDefaultMemory()
	return g.serialize()
}

// emitDefaultMemory 加入一個匯出的線性記憶體（min 1 page, 無 max）。
// 記憶體 index 為 0，並以 "memory" 名稱匯出，供 WASI runtime 與
// Direct WASM 後端的堆/字串資料使用（Task 8 會使用此記憶體）。
func (g *Generator) emitDefaultMemory() {
	memIdx := g.addMemory(1, 0, false)
	g.addExport("memory", MemoryExport, memIdx)
}

// reset 清空所有模組建構狀態，使 Generate 可重複呼叫。
func (g *Generator) reset() {
	g.types = nil
	g.typeKeys = make(map[string]uint32)
	g.imports = nil
	g.functions = nil
	g.exports = nil
	g.memories = nil
	g.codes = nil
}

// internType 加入（或重用）一個 functype，回傳其 type index。
// 內容相同的 functype 會被去重，符合 WASM 模組的常見慣例。
func (g *Generator) internType(params, results []ValType) uint32 {
	key := funcTypeKey(params, results)
	if idx, ok := g.typeKeys[key]; ok {
		return idx
	}
	idx := uint32(len(g.types))
	g.types = append(g.types, funcType{params: params, results: results})
	g.typeKeys[key] = idx
	return idx
}

// funcTypeKey 由參數與結果型別列表產生去重用的字串鍵。
func funcTypeKey(params, results []ValType) string {
	buf := make([]byte, 0, len(params)+len(results)+1)
	buf = append(buf, 'p')
	for _, v := range params {
		buf = append(buf, byte(v))
	}
	buf = append(buf, 'r')
	for _, v := range results {
		buf = append(buf, byte(v))
	}
	return string(buf)
}

// addFuncImport 加入一筆函數 import，回傳該 import 的 function index。
// import 的 function index 從 0 開始依加入順序編號。
func (g *Generator) addFuncImport(module, name string, typeIndex uint32) uint32 {
	idx := uint32(len(g.imports))
	g.imports = append(g.imports, importEntry{
		module:    module,
		name:      name,
		kind:      FuncImport,
		typeIndex: typeIndex,
	})
	return idx
}

// addFunction 加入一筆已定義函數，回傳其 function index。
// 已定義函數的 function index 接在所有 import 函數之後。
func (g *Generator) addFunction(typeIndex uint32) uint32 {
	idx := uint32(len(g.imports) + len(g.functions))
	g.functions = append(g.functions, funcIndex{typeIndex: typeIndex})
	return idx
}

// addExport 加入一筆 export。
func (g *Generator) addExport(name string, kind ExportKind, index uint32) {
	g.exports = append(g.exports, exportEntry{name: name, kind: kind, index: index})
}

// addMemory 加入一個線性記憶體，回傳其 memory index。
func (g *Generator) addMemory(min, max uint32, hasMax bool) uint32 {
	idx := uint32(len(g.memories))
	g.memories = append(g.memories, memoryLimit{min: min, max: max, hasMax: hasMax})
	return idx
}

// addCode 加入一筆已定義函數的程式碼主體。
// functions 與 codes 的數量必須一致且一一對應。
func (g *Generator) addCode(locals []LocalGroup, code []byte) {
	g.codes = append(g.codes, codeEntry{locals: locals, code: code})
}

// setupWASIImports 加入 Direct WASM 後端需要的 WASI imports：
//   - wasi_snapshot_preview1.fd_write: (i32, i32, i32, i32) -> i32  (func index 0)
//   - wasi_snapshot_preview1.proc_exit: (i32) -> ()                 (func index 1)
//
// fd_write / proc_exit 的 type index 透過 internType 取得並自動去重。
func (g *Generator) setupWASIImports() {
	fdWriteType := g.internType([]ValType{I32, I32, I32, I32}, []ValType{I32})
	procExitType := g.internType([]ValType{I32}, nil)
	g.addFuncImport("wasi_snapshot_preview1", "fd_write", fdWriteType)
	g.addFuncImport("wasi_snapshot_preview1", "proc_exit", procExitType)
}

// emitStartFunction 發射 _start 函數（WASI 進入點）。
// _start 的型別為 () -> ()，function index 為 2（接在 fd_write=0、proc_exit=1 之後）。
// 主體為：i32.const 0; call 1 (proc_exit)。
// proc_exit 在型別上為 (i32)->()，呼叫後堆疊為空，符合 _start 的 () 結果型別。
func (g *Generator) emitStartFunction() {
	startType := g.internType(nil, nil) // () -> ()
	startIdx := g.addFunction(startType)
	g.addExport("_start", FuncExport, startIdx)

	// i32.const 0  → opcode 0x41 + LEB128(0) = 0x00
	// call 1       → opcode 0x10 + LEB128(1) = 0x01
	code := []byte{OpI32Const, 0x00, OpCall, 0x01}
	g.addCode(nil, code)
}

// serialize 將所有已累積的 section 組裝為完整的 WASM 二進制。
// section 順序依 ID 遞增（Type < Import < Function < Memory < Export < Code）。
func (g *Generator) serialize() ([]byte, error) {
	if err := g.validateState(); err != nil {
		return nil, err
	}

	w := NewWriter()

	// Magic: \0asm
	w.WriteBytes([]byte{0x00, 0x61, 0x73, 0x6d})
	// Version: 1
	w.WriteBytes([]byte{0x01, 0x00, 0x00, 0x00})

	// Type section (1)
	typeBody := g.encodeTypeSection()
	WriteSection(w, TypeSection, typeBody)

	// Import section (2)
	if len(g.imports) > 0 {
		WriteSection(w, ImportSection, g.encodeImportSection())
	}

	// Function section (3): 已定義函數的 type index vec
	if len(g.functions) > 0 {
		WriteSection(w, FunctionSection, g.encodeFunctionSection())
	}

	// Memory section (5)
	if len(g.memories) > 0 {
		WriteSection(w, MemorySection, g.encodeMemorySection())
	}

	// Export section (7)
	if len(g.exports) > 0 {
		WriteSection(w, ExportSection, g.encodeExportSection())
	}

	// Code section (10)
	if len(g.codes) > 0 {
		WriteSection(w, CodeSection, g.encodeCodeSection())
	}

	return w.Bytes(), nil
}

// validateState 檢查模組狀態的一致性。
func (g *Generator) validateState() error {
	if len(g.functions) != len(g.codes) {
		return fmt.Errorf("wasm: function/code count mismatch: %d functions vs %d codes",
			len(g.functions), len(g.codes))
	}
	return nil
}

// encodeTypeSection 編碼 type section 的內容（不含 section header）。
func (g *Generator) encodeTypeSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.types)))
	for _, ft := range g.types {
		w.WriteBytes(FuncType(ft.params, ft.results))
	}
	return w.Bytes()
}

// encodeImportSection 編碼 import section 的內容。
func (g *Generator) encodeImportSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.imports)))
	for _, imp := range g.imports {
		var desc []byte
		switch imp.kind {
		case FuncImport:
			dw := NewWriter()
			dw.WriteLEB128(imp.typeIndex)
			desc = dw.Bytes()
		default:
			// 骨架僅支援 func import；其他 kind 由 Task 7+ 補上。
			desc = nil
		}
		w.WriteBytes(Import(imp.module, imp.name, imp.kind, desc))
	}
	return w.Bytes()
}

// encodeFunctionSection 編碼 function section（已定義函數的 type index vec）。
func (g *Generator) encodeFunctionSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.functions)))
	for _, f := range g.functions {
		w.WriteLEB128(f.typeIndex)
	}
	return w.Bytes()
}

// encodeMemorySection 編碼 memory section。
func (g *Generator) encodeMemorySection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.memories)))
	for _, m := range g.memories {
		w.WriteBytes(Memory(m.min, m.max, m.hasMax))
	}
	return w.Bytes()
}

// encodeExportSection 編碼 export section。
func (g *Generator) encodeExportSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.exports)))
	for _, e := range g.exports {
		w.WriteBytes(Export(e.name, e.kind, e.index))
	}
	return w.Bytes()
}

// encodeCodeSection 編碼 code section。
// 每個 entry 為 [u32 size][body]，body 由 FunctionBody 產生。
func (g *Generator) encodeCodeSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.codes)))
	for _, c := range g.codes {
		body := FunctionBody(c.locals, c.code)
		w.WriteBytes(CodeEntry(body))
	}
	return w.Bytes()
}
