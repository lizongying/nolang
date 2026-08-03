package wasm

import (
	"bytes"
	"fmt"
	"strings"

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

// globalEntry 記錄一筆已定義 global。
// globalType 為 ValType；mutable 為是否可變；init 為初始值表達式的位元組序列
// （不含結尾的 OpEnd，serialize 時會補上）。
type globalEntry struct {
	valType ValType
	mutable bool
	init    []byte
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
// Task 7：實作基礎型別與表達式 codegen（i32/i64/f32/f64 局部變數、算術/比較/
// 邏輯運算子、print 內建、if/else、for range、函數定義與呼叫）。
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
	globals   []globalEntry // 已定義 global（Task 8：heapPtr）

	// ---- Task 7 codegen 狀態 ----

	// 函數映射：name → function index（含 _start 與用戶函數）。
	funcTable map[string]uint32
	// 函數簽名：name → 參數型別列表（與 type section 一致）。
	funcParams map[string][]ValType
	// 函數簽名：name → 參數名稱列表（用於 local index 對應）。
	funcParamNames map[string][]string
	// 函數簽名：name → 結果型別列表。
	funcResults map[string][]ValType
	// 函數簽名：name → 結果變數名稱列表（用於結尾 local.get + return）。
	funcResultNames map[string][]string

	// 局部變數映射：currentFunc → 變數名 → local index（含參數；參數為 0..n-1）。
	locals map[string]map[string]int
	// 已宣告 local 型別（不含參數），按宣告順序；用於 LocalGroup 編碼。
	localDecls map[string][]ValType
	// 局部變數型別映射：currentFunc → 變數名 → ValType（含參數）。
	localTypeMap map[string]map[string]ValType

	// 字串常數池：string → 記憶體 offset。
	stringPool map[string]uint32
	// 按加入順序的字串，用於 data section。
	stringData []string
	// 下一個可用記憶體位置（字串池配置起點 = 1024）。
	nextMemOffset uint32

	// data section 區段（字串常數 + 靜態緩衝）。
	dataSegments []DataSegment

	// 當前正在發射的函數名稱（_start 或用戶函數名）。
	currentFunc string

	// 控制流深度追蹤：目前開啟的 block/loop/if 數量，用於 br 深度計算。
	ctrlDepth int
	// 迴圈框架堆疊：用於 break/continue 的 br 目標深度計算。
	loopStack []loopFrame

	// ---- Task 8 codegen 狀態 ----

	// heapOffset 為編譯期靜態配置的游標（從 HeapBase 起）。
	// 運行期 malloc 的起點（heapPtr global 初始值）= codegen 結束時的 heapOffset。
	heapOffset uint32

	// structDefs 記錄用戶定義的 struct 佈局（名稱 → StructLayout）。
	structDefs map[string]*StructLayout

	// strDescriptorPool 為 str literal descriptor 的去重池（字串 → 描述符 offset）。
	strDescriptorPool map[string]uint32

	// staticDataSegments 為編譯期產生的靜態資料區段（str descriptor 等）。
	// 與 stringData（字串位元組）合併後寫入 data section。
	staticDataSegments []DataSegment

	// needsMalloc 標記是否需要 runtime malloc/free 函數與 heapPtr global。
	needsMalloc bool

	// localKindMap 記錄當前函數中變數的 ValKind（純量/str/vec/arr/struct）。
	localKindMap map[string]map[string]ValKind

	// localElemTypeMap 記錄當前函數中 vec/arr 變數的元素 ValType。
	localElemTypeMap map[string]map[string]ValType

	// localStructTypeMap 記錄當前函數中 struct 變數的型別名稱（對應 structDefs）。
	localStructTypeMap map[string]map[string]string
}

// loopFrame 記錄一個 for 迴圈的 break/continue 目標深度。
// blockIndex = 開啟 break 目標 block 後的 ctrlDepth；
// loopIndex  = 開啟 continue 目標 loop 後的 ctrlDepth。
// br 深度 = 當前 ctrlDepth - 目標 index。
type loopFrame struct {
	blockIndex int
	loopIndex  int
}

// 記憶體佈局常數（避開 WASI runtime 使用的低位）。
// 字串池從 StringPoolBase 開始向上成長；固定緩衝區位於更高位址。
// 注意：Task 7 假設字串池不會成長至 IovecBase（測試字串皆短）。
const (
	StringPoolBase uint32 = 1024 // 字串常數起點
	IovecBase      uint32 = 4096 // fd_write 用 iovec（8 bytes: ptr+len）
	NwrittenPtr    uint32 = 4104 // fd_write nwritten 寫入位置（4 bytes）
	ItoaBufferEnd  uint32 = 8224 // itoa 緩衝結尾（digits 由 8223 往前寫）
	ItoaBufferSize uint32 = 32   // itoa 緩衝大小（8192..8224）
	FmtBufferStart uint32 = 4108 // 格式化輸出緩衝區起點（8192..8224 為 itoa）
	FmtBufferEnd   uint32 = 8192 // 格式化輸出緩衝區終點（避免與 ItoaBuffer 衝突）
)

// fd_write / proc_exit 的 function index（import 後固定）。
const (
	fdWriteIdx  uint32 = 0
	procExitIdx uint32 = 1
)

// SetTargetPlatform 設定編譯目標平台（GOOS, GOARCH），用於平台變體過濾。
// 空字串時回退到宿主 runtime（與 llvm.Generator.SetTargetPlatform 行為一致）。
func (g *Generator) SetTargetPlatform(goos, goarch string) {
	g.targetGoos = goos
	g.targetGoarch = goarch
}

// Generate 從給定的 Nolang 程式產生完整的 WebAssembly 二進制。
//
// 流程：
//  1. 重置狀態、加入 WASI imports（fd_write、proc_exit）與匯出的 memory。
//  2. 宣告 _start（function index 2）與所有用戶函數（index 3+）。
//  3. 發射 _start 主體（頂層語句 + proc_exit(0)）與各用戶函數主體。
//     字串常數在 codegen 過程中自動加入字串池。
//  4. serialize() 將字串池寫入 data section 並組裝所有 section。
//
// 當 program 為空（或 nil）時，僅發射最小骨架（_start 呼叫 proc_exit(0)）。
func (g *Generator) Generate(program *parser.Program) ([]byte, error) {
	g.reset()
	g.setupWASIImports()
	g.emitDefaultMemory()

	if program != nil {
		if err := g.emitProgram(program); err != nil {
			return nil, err
		}
	} else {
		g.emitSkeletonStart()
	}

	return g.serialize()
}

// emitProgram 處理完整的 Nolang AST，發射 _start 與用戶函數。
func (g *Generator) emitProgram(program *parser.Program) error {
	// 0. 掃描所有 StructDefinition，計算佈局並存入 structDefs。
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			layout := computeStructLayout(sd)
			g.structDefs[sd.Name] = layout
		}
	}

	// 1. 宣告 _start（function index 2，第一個已定義函數）。
	startType := g.internType(nil, nil) // () -> ()
	startIdx := g.addFunction(startType)
	g.addExport("_start", FuncExport, startIdx)
	g.funcTable["_start"] = startIdx
	g.funcParams["_start"] = nil
	g.funcParamNames["_start"] = nil
	g.funcResults["_start"] = nil
	g.funcResultNames["_start"] = nil

	// 2. 宣告 runtime malloc/free（function index 3, 4）。
	//    固定發射以簡化索引管理；實際程式碼主體稍後發射以保持 codes 順序一致。
	g.needsMalloc = true
	g.declareMallocFree()

	// 3. 宣告所有用戶函數（function index 5, 6, ...）。
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			g.declareFunction(fd)
		}
	}

	// 4. 發射 _start 主體（codes[0]，對應 functions[0] = _start）。
	g.currentFunc = "_start"
	g.locals["_start"] = make(map[string]int)
	g.localDecls["_start"] = nil
	g.localTypeMap["_start"] = make(map[string]ValType)
	g.localKindMap["_start"] = make(map[string]ValKind)
	g.localElemTypeMap["_start"] = make(map[string]ValType)
	g.localStructTypeMap["_start"] = make(map[string]string)
	g.emitStartBody(program)

	// 5. 發射 malloc/free 程式碼主體（codes[1,2]，對應 functions[1,2]）。
	g.emitMallocFreeCode()

	// 6. 發射各用戶函數主體（codes[3+]，對應 functions[3+]）。
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			g.emitFunctionBody(fd)
		}
	}

	return nil
}

// emitSkeletonStart 發射骨架 _start（無 AST）：i32.const 0; call proc_exit。
// 保留給未傳入 program 的最小模組（Task 6 相容行為）。
func (g *Generator) emitSkeletonStart() {
	startType := g.internType(nil, nil)
	startIdx := g.addFunction(startType)
	g.addExport("_start", FuncExport, startIdx)
	g.funcTable["_start"] = startIdx
	// i32.const 0; call proc_exit(1)
	code := []byte{OpI32Const, 0x00, OpCall, byte(procExitIdx)}
	g.addCode(nil, code)
}

// declareFunction 掃描 FunctionDefinition 並將其簽名註冊到 funcTable 與型別表。
// 不發射程式碼主體（主體由 emitFunctionBody 負責）。
func (g *Generator) declareFunction(fd *parser.FunctionDefinition) {
	params := make([]ValType, len(fd.Parameters))
	paramNames := make([]string, len(fd.Parameters))
	for i, p := range fd.Parameters {
		params[i] = ValTypeFromName(p.Type)
		paramNames[i] = p.Name
	}
	results := make([]ValType, len(fd.Results))
	resultNames := make([]string, len(fd.Results))
	for i, r := range fd.Results {
		results[i] = ValTypeFromName(r.Type)
		resultNames[i] = r.Name
	}
	typeIdx := g.internType(params, results)
	funcIdx := g.addFunction(typeIdx)
	g.funcTable[fd.Name] = funcIdx
	g.funcParams[fd.Name] = params
	g.funcParamNames[fd.Name] = paramNames
	g.funcResults[fd.Name] = results
	g.funcResultNames[fd.Name] = resultNames
}

// emitFunctionBody 發射單一用戶函數的程式碼主體。
// 結尾自動 local.get 所有結果變數，使堆疊符合函數結果型別。
func (g *Generator) emitFunctionBody(fd *parser.FunctionDefinition) {
	g.currentFunc = fd.Name
	// 建立局部變數映射：參數為 local 0..n-1。
	lmap := make(map[string]int)
	tmap := make(map[string]ValType)
	kmap := make(map[string]ValKind)
	emap := make(map[string]ValType)
	smap := make(map[string]string)
	paramNames := g.funcParamNames[fd.Name]
	paramTypes := g.funcParams[fd.Name]
	for i, name := range paramNames {
		lmap[name] = i
		tmap[name] = paramTypes[i]
		kmap[name] = KindScalar
		// 方法定義的首個參數為 self（struct 指標）。
		// 註冊其 struct 型別名稱，供方法體內 .field 脫糖為 self.field 時查找。
		// 函數名形如 "user.birthday"，取 "." 前的部分作為 struct 名稱。
		if i == 0 && name == "self" {
			if dotIdx := strings.Index(fd.Name, "."); dotIdx > 0 {
				structName := fd.Name[:dotIdx]
				smap[name] = structName
				kmap[name] = KindStruct
			}
		}
	}
	// 結果變數亦為 local（需宣告），自參數數量之後開始。
	g.locals[fd.Name] = lmap
	g.localDecls[fd.Name] = nil
	g.localTypeMap[fd.Name] = tmap
	g.localKindMap[fd.Name] = kmap
	g.localElemTypeMap[fd.Name] = emap
	g.localStructTypeMap[fd.Name] = smap
	resultNames := g.funcResultNames[fd.Name]
	resultTypes := g.funcResults[fd.Name]
	for i, r := range resultNames {
		if _, exists := lmap[r]; !exists {
			lmap[r] = len(paramNames) + len(g.localDecls[fd.Name])
			g.localDecls[fd.Name] = append(g.localDecls[fd.Name], resultTypes[i])
			tmap[r] = resultTypes[i]
			kmap[r] = KindScalar
		}
	}

	var sb bytes.Buffer
	if fd.Body != nil {
		for _, s := range fd.Body.Statements {
			g.emitStmt(&sb, s)
		}
	}
	// 結尾：依序 local.get 所有結果變數，使其留在堆疊上。
	for _, r := range resultNames {
		idx := lmap[r]
		sb.WriteByte(OpLocalGet)
		writeU32LEB(&sb, uint32(idx))
	}

	g.addCode(groupLocals(g.localDecls[fd.Name]), sb.Bytes())
}

// emitStartBody 發射 _start 主體：所有非函數定義的頂層語句 + proc_exit(0)。
func (g *Generator) emitStartBody(program *parser.Program) {
	var sb bytes.Buffer
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			continue
		}
		g.emitStmt(&sb, stmt)
	}
	// i32.const 0; call proc_exit
	writeI32ConstU(&sb, 0)
	sb.WriteByte(OpCall)
	writeU32LEB(&sb, procExitIdx)
	g.addCode(groupLocals(g.localDecls["_start"]), sb.Bytes())
}

// groupLocals 將一串 local 型別（按宣告順序）壓縮為 WASM LocalGroup vec。
// 連續相同型別的 local 會被合併為單一 group。
func groupLocals(types []ValType) []LocalGroup {
	if len(types) == 0 {
		return nil
	}
	var groups []LocalGroup
	cur := types[0]
	count := uint32(1)
	for i := 1; i < len(types); i++ {
		if types[i] == cur {
			count++
		} else {
			groups = append(groups, LocalGroup{Count: count, Type: cur})
			cur = types[i]
			count = 1
		}
	}
	groups = append(groups, LocalGroup{Count: count, Type: cur})
	return groups
}

// writeU32LEB 將無號 LEB128 寫入 bytes.Buffer（codegen 便利函式）。
func writeU32LEB(sb *bytes.Buffer, v uint32) {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
			sb.WriteByte(b)
		} else {
			sb.WriteByte(b)
			return
		}
	}
}

// writeSLEB 將有號 LEB128 寫入 bytes.Buffer（codegen 便利函式）。
func writeSLEB(sb *bytes.Buffer, v int64) {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if (b&0x40 != 0 && v == -1) || (b&0x40 == 0 && v == 0) {
			sb.WriteByte(b)
			return
		}
		b |= 0x80
		sb.WriteByte(b)
	}
}

// ValTypeFromName 將 Nolang 型別名稱（parser.Type）映射為 WASM ValType。
// Nolang 整數預設為 i64；bool 以 i32 表示；
// str / vec / arr / 用戶 struct 均以描述符指標表示（i32）。
func ValTypeFromName(t parser.Type) ValType {
	if t == nil {
		return I64
	}
	switch t.String() {
	case "i32", "u32", "bool":
		return I32
	case "i64", "u64", "int", "i8", "u8", "i16", "u16", "byte", "char", "i128", "u128":
		return I64
	case "f32":
		return F32
	case "f64":
		return F64
	case "str":
		// str 變數持有描述符指標（i32）
		return I32
	}
	// SliceType / ArrayType / MapType / 用戶自訂 struct 型別均為描述符指標
	switch t.(type) {
	case *parser.SliceType, *parser.ArrayType, *parser.MapType, *parser.NamedType:
		return I32
	}
	return I64
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
	g.globals = nil

	g.funcTable = make(map[string]uint32)
	g.funcParams = make(map[string][]ValType)
	g.funcParamNames = make(map[string][]string)
	g.funcResults = make(map[string][]ValType)
	g.funcResultNames = make(map[string][]string)
	g.locals = make(map[string]map[string]int)
	g.localDecls = make(map[string][]ValType)
	g.localTypeMap = make(map[string]map[string]ValType)
	g.stringPool = make(map[string]uint32)
	g.stringData = nil
	g.nextMemOffset = StringPoolBase
	g.dataSegments = nil
	g.currentFunc = ""
	g.ctrlDepth = 0
	g.loopStack = nil

	// Task 8 狀態
	g.heapOffset = HeapBase
	g.structDefs = make(map[string]*StructLayout)
	g.strDescriptorPool = make(map[string]uint32)
	g.staticDataSegments = nil
	g.needsMalloc = false
	g.localKindMap = make(map[string]map[string]ValKind)
	g.localElemTypeMap = make(map[string]map[string]ValType)
	g.localStructTypeMap = make(map[string]map[string]string)
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

// emitStartFunction 已被 emitProgram（含 _start）與 emitSkeletonStart 取代。
// 保留註解標記 Task 6 骨架的歷史位置；實際 _start 發射邏輯見 emitProgram / emitSkeletonStart。

// finalizeDataSegments 將字串池（stringData）與靜態資料區段（staticDataSegments）
// 合併轉為 data section 區段。必須在 serialize 前呼叫。
func (g *Generator) finalizeDataSegments() {
	g.dataSegments = g.dataSegments[:0]
	for _, s := range g.stringData {
		off := g.stringPool[s]
		g.dataSegments = append(g.dataSegments, DataSegment{
			offset: off,
			bytes:  []byte(s),
		})
	}
	// 合併編譯期靜態資料（str descriptor 等）
	g.dataSegments = append(g.dataSegments, g.staticDataSegments...)
}

// finalizeHeapGlobal 在所有 codegen 完成後，加入 heapPtr global。
// 初始值 = g.heapOffset（所有編譯期靜態配置之後的位址）。
func (g *Generator) finalizeHeapGlobal() {
	if !g.needsMalloc {
		return
	}
	// heapPtr global：mutable i32，初始值 = g.heapOffset
	initExpr := []byte{OpI32Const}
	// 用 SLEB128 編碼初始值
	var tmp bytes.Buffer
	writeSLEB(&tmp, int64(g.heapOffset))
	initExpr = append(initExpr, tmp.Bytes()...)
	g.globals = append(g.globals, globalEntry{
		valType: I32,
		mutable: true,
		init:    initExpr,
	})
}

// serialize 將所有已累積的 section 組裝為完整的 WASM 二進制。
// section 順序依 ID 遞增（Type < Import < Function < Memory < Global < Export < Code < Data）。
func (g *Generator) serialize() ([]byte, error) {
	if err := g.validateState(); err != nil {
		return nil, err
	}

	// 在組裝前完成所有延遲決策：
	//   1. heapPtr global 初始值（依賴 codegen 完成後的 heapOffset）
	//   2. data section（字串池 + 靜態描述符）
	g.finalizeHeapGlobal()
	g.finalizeDataSegments()

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

	// Global section (6) — Task 8：heapPtr
	if len(g.globals) > 0 {
		WriteSection(w, GlobalSection, g.encodeGlobalSection())
	}

	// Export section (7)
	if len(g.exports) > 0 {
		WriteSection(w, ExportSection, g.encodeExportSection())
	}

	// Code section (10)
	if len(g.codes) > 0 {
		WriteSection(w, CodeSection, g.encodeCodeSection())
	}

	// Data section (11)
	if len(g.dataSegments) > 0 {
		WriteSection(w, DataSection, g.encodeDataSection())
	}

	return w.Bytes(), nil
}

// encodeGlobalSection 編碼 global section：vec of global entries。
// 每個 entry：[valtype][mutability][init_expr][end]。
func (g *Generator) encodeGlobalSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.globals)))
	for _, gl := range g.globals {
		w.WriteByte(byte(gl.valType))
		if gl.mutable {
			w.WriteByte(0x01)
		} else {
			w.WriteByte(0x00)
		}
		w.WriteBytes(gl.init)
		w.WriteByte(OpEnd)
	}
	return w.Bytes()
}

// encodeDataSection 編碼 data section：vec of active data segments。
func (g *Generator) encodeDataSection() []byte {
	w := NewWriter()
	w.WriteLEB128(uint32(len(g.dataSegments)))
	for _, d := range g.dataSegments {
		w.WriteBytes(ActiveData(d.offset, d.bytes))
	}
	return w.Bytes()
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
