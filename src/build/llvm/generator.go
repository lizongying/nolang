package llvm

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

type varInfo struct {
	Name string
	Type string
	Size int64
}

type structField struct {
	name     string
	typ      string // LLVM type string
	elemType string // for %vec fields: LLVM element type (e.g. "i8" for []byte, "i64" for []i64)
}

// ExternFuncInfo 描述一個 FFI extern 宣告的型別資訊。
// ParamTypes / ResultTypes 為 FFI 型別名稱（"i64","i32","f64","str","ptr","pptr","ppptr","bool"）。
type ExternFuncInfo struct {
	Name        string
	ParamTypes  []string
	ResultTypes []string
}

type loopExit struct {
	name string // 循環名稱（空 = 未命名）
	cond string // LLVM 條件塊標籤（continue 跳轉目標）
	exit string // LLVM 退出塊標籤（break 跳轉目標）
}

type Generator struct {
	indentLevel            int
	fmtStrIdx              int
	stringIdx              int
	fmtGlobals             []string
	tmpIdx                 int
	funcVars               []varInfo                        // current function's variables for lifetime.end
	varTypes               map[string]string                // variable name → LLVM type
	varSSA                 map[string]int                   // variable name → current SSA version
	ssaMode                bool                             // true = 使用 SSA 暫存器
	paramNames             map[string]bool                  // 函數參數名稱（使用 .addr 存取）
	funcRetTypes           map[string]string                // 函數名 → 回傳型別
	funcNumResults         map[string]int                   // 函數名 → 結果數（單結果=1，多結果=N>1，void=0）
	funcResultLLVMType     map[string][]string              // 函數名 → 各輸出參數的 LLVM 型別列表
	funcResultNolangTypes  map[string][]string              // 函數名 → 各輸出參數的 Nolang 型別字串列表
	funcIsVariadic         map[string]bool                  // 函數名 → 是否為 variadic 函數
	funcParamCount         map[string]int                   // 函數名 → 非 variadic 參數數量
	funcHeuristicOutput    map[string]bool                  // 函數名 → 是否為啟發式檢測的輸出參數（非顯式 fd.Results）
	funcParamDefaults      map[string][]parser.Expression   // 函數名 → 各參數的默認值表達式（nil 表示無默認值）
	funcParamLLVMTypes     map[string][]string              // 函數名 → 各參數的 LLVM 型別列表（含 receiver）
	funcParamTypes         map[string][]string              // 函數名 → 各參數的 Nolang 型別字串列表（含 receiver）
	structTypes            map[string][]structField         // struct name → fields
	structTypeLLVM         string                           // 當前正在生成的 struct LLVM type name
	loopExits              []loopExit                       // 活躍循環退出目標棧
	currentBlock           string                           // current basic block label (for PHI predecessor tracking)
	arrayElemTypes         map[string]string                // variable name → element LLVM type for %arr variables
	arraySizes             map[string]int64                 // variable name → declared array size for [N]T locals
	curFuncRetType         string                           // 當前函數回傳型別（void/i64/...）
	curFuncRetName         string                           // 當前函數輸出參數名稱（為空表示 void）
	curFuncName            string                           // 當前函數名稱（debug 用）
	inMainFunction         bool                             // true when generating the synthetic @main wrapper
	outputParamNames       map[string]bool                  // 當前函數的輸出參數名稱集合
	outputBindings         map[string]map[int]outputBinding // 輸出參數名 → {SSA版本 → 延遲綁定}
	ssaVersion             map[string]int                   // 輸出參數的 SSA 版本計數器（每次賦值遞增）
	heapVars               map[string]string                // 堆分配變數名 → LLVM 型別（%vec/%str-long/%arr），用於函數結束時 free
	stackArrVars           map[string]bool                  // 棧分配的局部固定陣列名（小尺寸/定寬元素），用於 generateLet 跳過 malloc
	heapVarIndex           map[string]int                   // 堆變數名 → varIdx（僅局部堆變數，用於 bitmap 定位）
	globalVars             map[string]bool                  // module-level vars that should be LLVM globals
	mainFileNames          map[string]bool                  // names (vars+funcs) from the main file being compiled (not imported modules)
	reassignedVars         map[string]bool                  // module-level vars that are reassigned (not constants)
	rangeLoopVars          map[string]bool                  // top-level vars used as range loop variables (must be locals)
	rangeLoopBounds        map[string]int64                 // range loop variable name → upper bound (for bounds check elimination)
	multiAssignVars        map[string]bool                  // top-level vars used as multi-assign targets (must be locals)
	funcRefVars            map[string]bool                  // top-level vars that are function references (value is an Identifier referring to a function)
	moduleVarTypes         map[string]string                // module-level variable types (preserved across functions)
	moduleArrayElemTypes   map[string]string                // module-level array/slice element types (preserved across functions)
	ssaTypes               map[string]string                // SSA register name → LLVM type (i64/double/%str-long/%str-long*/...)
	blockTerminated        bool                             // true if current basic block ends with a terminator (ret/br)
	funcLocalNames         map[string]bool                  // local variable names in current function (params + allocas)
	funcParams             map[string]bool                  // current function's parameter names (to distinguish from globals with same name)
	unionAliases           map[string][]string              // union type alias name → member type names (e.g. "float"→["f32","f64"])
	optionInnerTypes       map[string]string                // option variable name → inner LLVM type (e.g. "f"→"double" for ?f64)
	moduleOptionInnerTypes map[string]string                // 模組級 option 變數 inner type 備份（避免函數級 map reset 後丟失）
	itAllocTypes           map[string]string                // synthetic `it` variable name → allocated LLVM type (for bitcast when shared across matches with different types)
	funcResultInnerTypes   map[string][]string              // function name → inner LLVM types of ?T results
	enumVariantIndex       map[string]int64                 // enum variant name → tag index (e.g. status1→0, status2→1)
	enumVariants           map[string]map[string]int64      // enum type name → variant name → value (e.g. "FileMode"→{"WRITE":1,"CREATE":64})
	varFnTypes             map[string]*parser.FunctionType  // variable name → FunctionType (for indirect calls)
	fnTypeAliases          map[string]*parser.FunctionType  // named function type alias name → FunctionType
	externFuncs            map[string]*ExternFuncInfo       // extern function name → FFI type info
	lastBuiltinExtra       string                           // extra return value from multi-result builtin (e.g. get-line ok)
	currentTargetType      string                           // target type for type-inferred builtins (e.g. with-cap)
	currentTargetElemType  string                           // element type for slice builtins (e.g. %str-long for []str)
	sliceViews             map[string]*sliceViewInfo        // variable name → slice view metadata (alias, no independent struct)
	varAlias               map[string]string                // variable name → actual LLVM variable name (用於 %arr 重新賦值為 %vec 時重定向)
	taskResultTypes        map[string]string                // task variable name → result LLVM type (for awy type inference)
	futureResultTypes      map[string]string                // future variable name → result LLVM type (for awy type inference)
	asyncWrappers          strings.Builder                  // wrapper functions for run expressions
	debugCallCount         int                              // debug counter for tracing function generation calls
	entryAllocaBuf         *strings.Builder                 // entry-block alloca buffer for literal-arg temporaries (hoisted out of loops to prevent stack overflow)
	targetGoos             string                           // target GOOS for platform filtering ("" = fallback to runtime.GOOS)
	targetGoarch           string                           // target GOARCH for platform filtering ("" = fallback to runtime.GOARCH)
	targetDatalayout       string                           // LLVM target datalayout ("" = fallback to historical macOS arm64 default)
	targetTriple           string                           // LLVM target triple ("" = fallback to historical macOS arm64 default)
	noBoundsCheck          bool                             // true = skip emitting bounds checks (unsafe mode)
	outputParamOrder       []string                         // output param names in declaration order (for outBindState index)
	hasBranchMove          bool                             // true = function has move-to-out inside a branch (needs bitmap)
	nextHeapVarIdx         int                              // next available varIdx for local heap vars
	outBindState           []int                            // out param → bound heap var idx (-1=none, -2=uncertain)
	movedVarBitset         []uint64                         // compile-time moved bitmap (used when no runtime bitmap)
	movedBitmapBase        string                           // LLVM bitmap var name prefix (e.g. "%__mb", "" = not allocated)
	bitmapCount            int                              // number of u64 bitmap blocks (= maxVarIdx/64 + 1)

	// === 无栈协程（状态机变换）相关字段 ===
	// async 函数变换为 coro_resume.N 函数，局部变量提升到 %coro_state.N 结构体。
	coroStateBuilders     []strings.Builder // coro_state 结构体定义缓冲区
	coroInAsyncFunc       bool              // 当前正在生成 async 函数的状态机
	coroFuncNum           int               // async 函数编号（用于 %coro_state.N 唯一命名）
	coroAwaitPoints       []awaitPoint      // 当前 async 函数的 awy 挂起点列表
	coroStateFields       []coroField       // coro_state 结构体字段（state + 参数 + 局部变量 + 结果）
	coroFieldIdx          map[string]int    // 变量名 → coro_state 字段索引
	asyncFuncCoroNum      map[string]int    // async 函数名 → coro 编号（用于 run/awy 生成 coro_state task）
	coroTrampolineEmitted map[int]bool      // coro 编号 → trampoline 是否已生成
	isModuleAsyncWrap     bool              // 当前正在生成模块级 async 包装（收集 localVarTypes 时排除 globalVars/funcRefVars）
}

// awaitPoint 描述一个 awy 挂起点的信息。
type awaitPoint struct {
	id       int    // 挂起点编号（1..M）
	taskVar  string // 等待的 task 变量名（LLVM SSA 寄存器或变量名）
	stateVal int    // 挂起时写入 coro_state.state 的值
}

// coroField 描述 coro_state 结构体的一个字段。
type coroField struct {
	name     string // Nolang 变量名（或 "__state"/"__result"）
	llvmTy   string // LLVM 类型字符串
	isParam  bool   // 是否为参数
	isResult bool   // 是否为结果参数
}

// emitEntryAlloca writes an alloca instruction to the entry-block buffer if available,
// otherwise to sb. This hoists literal-argument allocas out of loop bodies to prevent
// stack overflow when a call inside a loop would otherwise allocate new stack on each iteration.
func (g *Generator) emitEntryAlloca(sb *strings.Builder, format string, args ...interface{}) {
	if g.entryAllocaBuf != nil {
		g.entryAllocaBuf.WriteString(fmt.Sprintf(format, args...))
	} else {
		sb.WriteString(fmt.Sprintf(format, args...))
	}
}

// sliceViewInfo tracks a slice view alias: a variable that references a portion
// of an underlying arr/vec/str without allocating an independent struct.
// The view is described by an adjusted data pointer + computed length,
// enabling zero-copy, zero-allocation slicing.
type sliceViewInfo struct {
	baseVar    string // original variable name (e.g. "arr")
	baseType   string // %arr, %vec, %str-long
	startOff   string // LLVM i64 value for start offset (constant or register)
	viewLen    string // LLVM i64 value for computed view length
	dataPtrReg string // LLVM i8* register for adjusted data pointer (base_data + start * elemSize)
	elemType   string // element LLVM type (e.g. "i64", "i8")
	isStr      bool   // is this a string slice?
}

// outputBinding 記錄輸出參數的延遲綁定資訊。
// 當 res = x 時，不立即 store 到輸出參數指標，也不 load 源值，
// 僅記錄源變數名稱和型別。在 flushOutputBindings 時按 SSA 版本查表，
// 找到綁定則 load 源變數並 store 到輸出參數；找不到則跳過（立即 store 已處理）。
// SSA 版本由 if 分支前後的 save/restore 隔離，不同分支的綁定互不覆蓋。
type outputBinding struct {
	sourceVar string // 源變數名稱（如 "i", "from"）
	llvmType  string // 源變數的 LLVM 型別（如 i64, i8）
}

func NewGenerator() *Generator {
	return &Generator{
		sliceViews: make(map[string]*sliceViewInfo),
	}
}

func (g *Generator) indent() string {
	return strings.Repeat("\t", g.indentLevel)
}

// emitLabel writes a basic block label and updates currentBlock tracking.
func (g *Generator) emitLabel(sb *strings.Builder, label string) {
	sb.WriteString(label + ":\n")
	g.currentBlock = label
	g.blockTerminated = false
}

func (g *Generator) getFormatGlobal(fmtStr string) string {
	name := fmt.Sprintf("@.pfmt.%d", g.fmtStrIdx)
	g.fmtStrIdx++
	size := len(fmtStr) + 1
	escaped := g.escapeLLVMString(fmtStr)
	g.fmtGlobals = append(g.fmtGlobals,
		fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"", name, size, escaped))
	return name
}

func (g *Generator) escapeLLVMString(s string) string {
	r := strings.NewReplacer(
		"\\", "\\5C",
		"\n", "\\0A",
		"\r", "\\0D",
		"\t", "\\09",
		"\"", "\\22",
	)
	return r.Replace(s)
}

// llvmVarRef returns an LLVM variable reference for the given name.
// If the name contains special characters like '-', it wraps it in quotes
// to prevent LLVM from parsing e.g. %bl-1 as (%bl) - 1.
func llvmVarRef(name string) string {
	if strings.ContainsAny(name, "-") {
		return "%\"" + name + "\""
	}
	return "%" + name
}

// llvmGlobalRef returns an LLVM global variable reference for the given name.
// If the name contains special characters like '-', it wraps it in quotes
// to prevent LLVM from parsing e.g. @INV-SBOX as (@INV) - SBOX.
func llvmGlobalRef(name string) string {
	if strings.ContainsAny(name, "-") {
		return "@\"" + name + "\""
	}
	return "@" + name
}

// varAddr returns the LLVM variable reference (local or global) for the given name.
// It checks globalVars to determine whether to use @ (global) or % (local) prefix.
// Local variables (parameters and allocas in the current function) take precedence
// over globals with the same name.
func (g *Generator) varAddr(name string) string {
	// 檢查別名（變數從 %arr 重新賦值為 %vec 時，alloca 新的 %vec 並重定向）
	if g.varAlias != nil {
		if alias, ok := g.varAlias[name]; ok {
			name = alias
		}
	}
	if g.funcLocalNames != nil && g.funcLocalNames[name] {
		return llvmVarRef(name)
	}
	if g.globalVars != nil && g.globalVars[name] {
		return llvmGlobalRef(name)
	}
	return llvmVarRef(name)
}

// llvmSSAReg returns an LLVM SSA register name for the given base name and suffix.
// For names with special chars like '-', the entire name is quoted.
// e.g. llvmSSAReg("bl-1", ".val.434") → %"bl-1.val.434"
func llvmSSAReg(base, suffix string) string {
	if strings.ContainsAny(base, "-") {
		return "%\"" + base + suffix + "\""
	}
	return "%" + base + suffix
}

// intConstValue extracts an int64 value from an expression that is either an
// IntegerLiteral (positive) or a PrefixExpression with "-" operator applied to
// an IntegerLiteral (negative, e.g. FNV-OFFSET = -3750763034362895579).
// Returns (value, true) on success.
func intConstValue(expr parser.Expression) (int64, bool) {
	if intLit, ok := expr.(*parser.IntegerLiteral); ok {
		return intLit.Value, true
	}
	if pe, ok := expr.(*parser.PrefixExpression); ok && pe.Operator == "-" {
		if intLit, ok := pe.Right.(*parser.IntegerLiteral); ok {
			return -intLit.Value, true
		}
	}
	// CharLiteral: convert Unicode codepoint to int64
	if charLit, ok := expr.(*parser.CharLiteral); ok {
		runes := []rune(charLit.Value)
		if len(runes) == 1 {
			return int64(runes[0]), true
		}
	}
	return 0, false
}

// ffiTypeName 從 parser.Type 抽出 FFI 型別名稱。
// 指針型別依間接層數返回 "ptr"（*T）、"pptr"（**T）、"ppptr"（***T）；
// 其餘（NamedType 等）直接取 String()。
func ffiTypeName(t parser.Type) string {
	if t == nil {
		return ""
	}
	// 計算指針間接層數
	level := 0
	for {
		pt, ok := t.(*parser.PointerType)
		if !ok {
			break
		}
		level++
		if pt.Type == nil {
			// 不透明指標（舊式 ptr，向後相容）
			break
		}
		t = pt.Type
	}
	switch level {
	case 0:
		return t.String()
	case 1:
		return "ptr"
	case 2:
		return "pptr"
	case 3:
		return "ppptr"
	default:
		return "ptr"
	}
}

// ffiTypeToLLVM 將 FFI 型別名稱對應到 LLVM IR 型別字串。
func ffiTypeToLLVM(t string) string {
	switch t {
	case "i64":
		return "i64"
	case "i32", "bool":
		return "i32"
	case "f64":
		return "double"
	case "str", "ptr":
		return "i8*"
	case "pptr":
		return "i8**"
	case "ppptr":
		return "i8***"
	default:
		return "i64"
	}
}

// ffiTypeToNolangStorage 將 FFI 型別名稱對應到 Nolang 端的儲存型別
// （即 callExtern 回傳值的 LLVM 型別）。與 ffiTypeToLLVM 不同：
// str 在 C 端為 i8*，但 callExtern 會構造 %str-long 結構後回傳；
// ptr / pptr / ppptr / i32 / bool 皆以 i64 儲存（ptrtoint / sext / zext）。
func ffiTypeToNolangStorage(t string) string {
	switch t {
	case "i64", "i32", "ptr", "pptr", "ppptr", "bool":
		return "i64"
	case "f64":
		return "double"
	case "str":
		return "%str-long"
	default:
		return "i64"
	}
}

// externSymbolRef returns the LLVM symbol reference for an extern (C) function.
// Nolang source uses hyphens (-) in identifiers, but C ABI symbols use underscores (_).
// A leading underscore in the Nolang name marks the declaration as private (not
// exported from the module); it is stripped before generating the C ABI symbol.
// So "_sqlite3-open" → @sqlite3_open, matching the actual libsqlite3 symbol.
func externSymbolRef(name string) string {
	cName := name
	// Strip leading underscore (private marker) for C ABI symbol
	if strings.HasPrefix(cName, "_") {
		cName = cName[1:]
	}
	cName = strings.ReplaceAll(cName, "-", "_")
	return "@" + cName
}

// formatExternDeclare 產生單一 extern 函式的 LLVM declare 敘述。
// 回傳型別取自第一個 result FFI 型別（無 result 為 void）；
// 參數型別中 pptr 對應 i8**、ppptr 對應 i8***，其餘依 ffiTypeToLLVM。
// 符號名稱將連字號轉為底線以匹配 C ABI 命名。
func (g *Generator) formatExternDeclare(name string, info *ExternFuncInfo) string {
	retType := "void"
	if len(info.ResultTypes) > 0 {
		retType = ffiTypeToLLVM(info.ResultTypes[0])
	}
	paramTypes := make([]string, 0, len(info.ParamTypes))
	for _, pt := range info.ParamTypes {
		paramTypes = append(paramTypes, ffiTypeToLLVM(pt))
	}
	return fmt.Sprintf("declare %s %s(%s)\n", retType, externSymbolRef(name), strings.Join(paramTypes, ", "))
}

// sortedExternNames 回傳 g.externFuncs 鍵的排序切片，確保輸出順序確定。
func (g *Generator) sortedExternNames() []string {
	names := make([]string, 0, len(g.externFuncs))
	for n := range g.externFuncs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// platformKeys maps flattened platform annotation keys to (goos, goarch) pairs.
// Each key unambiguously specifies both OS and architecture — no combination
// logic (OR/AND) is needed. Multiple keys are simply OR'd together.
var platformKeys = map[string]struct{ goos, goarch string }{
	"linux-amd64": {"linux", "amd64"},
	"linux-arm64": {"linux", "arm64"},
	"win-amd64":   {"windows", "amd64"},
	"win-arm64":   {"windows", "arm64"},
	"mac-amd64":   {"darwin", "amd64"},
	"mac-arm64":   {"darwin", "arm64"},
	"wasi-wasm32": {"wasi", "wasm32"},
}

// targetDatalayoutAndTriple returns the LLVM target datalayout and triple
// for the given (goos, goarch) platform. Empty strings fall back to the
// host runtime platform (backward-compatible with the previous hardcoded
// macOS arm64 default).
func targetDatalayoutAndTriple(goos, goarch string) (layout, triple string) {
	// If both empty, return the historical default (macOS arm64).
	if goos == "" && goarch == "" {
		return "e-m:o-i64:64-i128:128-n32:64-S128", "arm64-apple-macosx15.0.0"
	}
	// Use targetGoos/targetGoarch if set; otherwise fall back to runtime.
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	switch goos + "/" + goarch {
	case "wasi/wasm32":
		return "e-m:e-p:32:32-i64:64-n32:64-S128", "wasm32-wasi"
	case "darwin/arm64":
		return "e-m:o-i64:64-i128:128-n32:64-S128", "arm64-apple-macosx15.0.0"
	case "darwin/amd64":
		return "e-m:o-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128", "x86_64-apple-macosx15.0.0"
	case "linux/amd64":
		return "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128", "x86_64-unknown-linux-gnu"
	case "linux/arm64":
		return "e-m:e-i8:8:32-i16:16:32-i64:64-i128:128-n32:64-S128-Fn32", "aarch64-unknown-linux-gnu"
	case "windows/amd64":
		return "e-m:w-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128", "x86_64-pc-windows-gnu"
	case "windows/arm64":
		return "e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-i128:128-f80:128-n8:16:32:64-S128", "aarch64-pc-windows-gnu"
	}
	// Final fallback: historical default.
	return "e-m:o-i64:64-i128:128-n32:64-S128", "arm64-apple-macosx15.0.0"
}

// stmtAnnotations extracts platform annotations from a statement.
func stmtAnnotations(stmt parser.Statement) []*parser.AnnotationEntry {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return s.Annotations
	case *parser.FunctionDefinition:
		return s.Annotations
	case *parser.StructDefinition:
		return s.Annotations
	case *parser.ExpressionStatement:
		return s.Annotations
	}
	return nil
}

// matchesPlatform returns true if any platform annotation key matches the
// target (goos, goarch). Multiple keys are OR'd — any match includes the code.
//
//	#{mac-arm64}                     → macOS ARM64 only
//	#{mac-amd64, mac-arm64}          → macOS on any arch
//	#{linux-amd64, win-amd64}        → Linux x86_64 OR Windows x86_64
//
// Non-platform annotations (e.g. #{debug}, #{range=...}) are ignored.
// If there are no platform annotations, returns true (always include).
func matchesPlatform(annotations []*parser.AnnotationEntry, goos, goarch string) bool {
	if len(annotations) == 0 {
		return true
	}
	hasPlatform := false
	for _, entry := range annotations {
		// Only boolean entries (no value) can be platform annotations
		if entry.Value != nil {
			continue
		}
		matcher, isPlatform := platformKeys[entry.Key]
		if !isPlatform {
			continue
		}
		hasPlatform = true
		if goos == matcher.goos && goarch == matcher.goarch {
			return true
		}
	}
	return !hasPlatform
}

// FilterByPlatform removes statements whose platform annotations don't match
// the target (goos, goarch). Exported so the transpiler can apply the filter
// before code generation.
func FilterByPlatform(stmts []parser.Statement, goos, goarch string) []parser.Statement {
	filtered := make([]parser.Statement, 0, len(stmts))
	for _, stmt := range stmts {
		if matchesPlatform(stmtAnnotations(stmt), goos, goarch) {
			filtered = append(filtered, stmt)
		}
	}
	return filtered
}

// PlatformKeyFor performs a reverse lookup on platformKeys: given a (goos, goarch)
// pair, it returns the corresponding platform annotation key (e.g. ("darwin","arm64")
// → "mac-arm64"). Returns "" if no key matches.
func PlatformKeyFor(goos, goarch string) string {
	for key, matcher := range platformKeys {
		if matcher.goos == goos && matcher.goarch == goarch {
			return key
		}
	}
	return ""
}

// SetTargetPlatform configures the target (GOOS, GOARCH) used by Generate's
// platform filter. Empty strings cause Generate to fall back to the host
// runtime's GOOS/GOARCH (backward-compatible behavior).
//
// 此函式亦透過 targetDatalayoutAndTriple 計算並填充 targetDatalayout /
// targetTriple 欄位，供 Generate 發射對應的 LLVM target header。
func (g *Generator) SetTargetPlatform(goos, goarch string) {
	g.targetGoos = goos
	g.targetGoarch = goarch
	g.targetDatalayout, g.targetTriple = targetDatalayoutAndTriple(goos, goarch)
}

// SetNoBoundsCheck configures whether bounds checks are emitted for array/slice/string
// indexing. When true (unsafe mode), no bounds check calls are generated, trading safety
// for performance. This is propagated from BuildOptions via the Transpiler.
func (g *Generator) SetNoBoundsCheck(skip bool) {
	g.noBoundsCheck = skip
}

// SetMainFileNames 設定主檔案（正在編譯的檔案，非導入模組）的變數和函數名稱集合。
// 用於區分主檔案全域變數的合法重新賦值與導入模組函數中的同名局部變數。
// 例如：主檔案的 result [16]byte 是全域變數，但 bigint.cmp 中的 result = .abs-cmp(b)
// 應為局部變數，不應誤寫到全域 @result。
func (g *Generator) SetMainFileNames(names map[string]bool) {
	g.mainFileNames = names
}

// goos 返回當前編譯目標的 GOOS（如 "darwin"/"linux"/"windows"/"wasi"）。
// 若 SetTargetPlatform 未設定目標，回退到宿主 runtime.GOOS（向後相容）。
// 用於所有平台分派邏輯，避免使用 runtime.GOOS 而誤用宿主平台。
func (g *Generator) goos() string {
	if g.targetGoos != "" {
		return g.targetGoos
	}
	return runtime.GOOS
}

// goarch 返回當前編譯目標的 GOARCH。
// 若 SetTargetPlatform 未設定目標，回退到宿主 runtime.GOARCH（向後相容）。
func (g *Generator) goarch() string {
	if g.targetGoarch != "" {
		return g.targetGoarch
	}
	return runtime.GOARCH
}

func (g *Generator) Generate(program *parser.Program) string {
	// Filter out statements that don't match the target platform
	// (#{mac-arm64}, #{linux-amd64}, etc.). If SetTargetPlatform was not
	// called (empty fields), fall back to the host runtime platform.
	goos, goarch := g.targetGoos, g.targetGoarch
	if goos == "" || goarch == "" {
		goos, goarch = runtime.GOOS, runtime.GOARCH
	}
	program.Statements = FilterByPlatform(program.Statements, goos, goarch)

	g.fmtGlobals = nil
	g.fmtStrIdx = 0
	g.stringIdx = 0
	g.tmpIdx = 0
	g.asyncWrappers.Reset()
	g.coroStateBuilders = nil
	g.coroFuncNum = 0
	g.coroInAsyncFunc = false
	g.asyncFuncCoroNum = make(map[string]int)
	g.coroTrampolineEmitted = make(map[int]bool)
	g.varTypes = make(map[string]string)
	g.paramNames = make(map[string]bool)
	g.funcRetTypes = make(map[string]string)
	g.funcNumResults = make(map[string]int)
	g.funcResultLLVMType = make(map[string][]string)
	g.funcResultNolangTypes = make(map[string][]string)
	g.funcIsVariadic = make(map[string]bool)
	g.funcParamCount = make(map[string]int)
	g.funcHeuristicOutput = make(map[string]bool)
	g.funcParamDefaults = make(map[string][]parser.Expression)
	g.funcParamLLVMTypes = make(map[string][]string)
	g.funcParamTypes = make(map[string][]string)
	g.structTypes = make(map[string][]structField)
	g.arrayElemTypes = make(map[string]string)
	g.globalVars = make(map[string]bool)
	g.ssaTypes = make(map[string]string)
	g.unionAliases = make(map[string][]string)
	g.optionInnerTypes = make(map[string]string)
	g.itAllocTypes = make(map[string]string)
	g.funcResultInnerTypes = make(map[string][]string)
	g.enumVariantIndex = make(map[string]int64)
	g.enumVariants = make(map[string]map[string]int64)
	g.fnTypeAliases = make(map[string]*parser.FunctionType)
	g.externFuncs = make(map[string]*ExternFuncInfo)
	g.reassignedVars = make(map[string]bool)

	// 掃描所有頂層 LetStatement，標記被重新賦值的變數（出現多次同名 LetStatement）
	// 這些變數不應被常量摺疊（enumVariantIndex 機制），必須從全局變數載入實際值
	letCount := make(map[string]int)
	g.multiAssignVars = make(map[string]bool)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			letCount[ls.Name.Value]++
		}
		if mas, ok := stmt.(*parser.MultiAssignStatement); ok {
			for _, t := range mas.Targets {
				if ident, ok := t.(*parser.Identifier); ok {
					letCount[ident.Value]++
					g.multiAssignVars[ident.Value] = true
				}
			}
		}
		// The transpiler converts MultiAssignStatement to a nested CallExpression:
		//   innerCall(targets...)  =>  ExpressionStatement{CallExpression{Function: innerCall, Arguments: targets}}
		// Detect this converted form so multi-assign targets are still recognized.
		if es, ok := stmt.(*parser.ExpressionStatement); ok {
			if ce, ok := es.Expression.(*parser.CallExpression); ok {
				if _, ok := ce.Function.(*parser.CallExpression); ok {
					for _, arg := range ce.Arguments {
						if ident, ok := arg.(*parser.Identifier); ok {
							letCount[ident.Value]++
							g.multiAssignVars[ident.Value] = true
						}
					}
				}
			}
		}
	}
	for name, count := range letCount {
		if count > 1 {
			g.reassignedVars[name] = true
		}
	}
	// 掃描所有 ForStatement (含巢狀)，標記 range loop 變數
	// 這些變數不應被視為常量全局變數，必須是局部變數以便 range loop 寫入
	g.funcRefVars = make(map[string]bool)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ident, ok := ls.Value.(*parser.Identifier); ok {
				if _, isFn := g.funcRetTypes[ident.Value]; isFn {
					g.funcRefVars[ls.Name.Value] = true
				}
			}
		}
	}
	g.rangeLoopVars = make(map[string]bool)
	g.rangeLoopBounds = make(map[string]int64)
	var collectRangeVars func(stmts []parser.Statement)
	collectRangeVars = func(stmts []parser.Statement) {
		for _, s := range stmts {
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				// Recurse into function bodies to find range loop variables
				if st.Body != nil {
					collectRangeVars(st.Body.Statements)
				}
			case *parser.ForStatement:
				if st.IterRange != nil && st.IterRange.Variable != "" {
					g.rangeLoopVars[st.IterRange.Variable] = true
					// Track the upper bound for bounds check elimination.
					// For `for i in [0..N)` where N is a constant, record N.
					if st.IterRange.Range != nil {
						if endLit, ok := st.IterRange.Range.End.(*parser.IntegerLiteral); ok {
							if st.IterRange.Range.RightInc {
								// [0..N] (inclusive): bound = N+1
								g.rangeLoopBounds[st.IterRange.Variable] = endLit.Value + 1
							} else {
								// [0..N) (exclusive): bound = N
								g.rangeLoopBounds[st.IterRange.Variable] = endLit.Value
							}
						}
					}
				}
				if st.Body != nil {
					collectRangeVars(st.Body.Statements)
				}
			case *parser.ExpressionStatement:
				// ExpressionStatement may wrap an IfExpression (match arm desugar).
				if ie, ok := st.Expression.(*parser.IfExpression); ok {
					if ie.Consequence != nil {
						collectRangeVars(ie.Consequence.Statements)
					}
					if ie.Alternative != nil {
						collectRangeVars(ie.Alternative.Statements)
					}
				}
			}
		}
	}
	collectRangeVars(program.Statements)

	// 收集聯合型別別名，用於解析 receiver method call
	for _, stmt := range program.Statements {
		if ta, ok := stmt.(*parser.TypeAlias); ok && ta.Union != nil {
			members := make([]string, 0, len(ta.Union.Types))
			for _, m := range ta.Union.Types {
				if nt, ok := m.(*parser.NamedType); ok {
					members = append(members, nt.Value)
				}
			}
			g.unionAliases[ta.Name] = members
		}
		// 收集具名函式型別別名（name = (params)(results)?），用於解析
		// 參數型別為 NamedType 時的間接呼叫登錄
		if ta, ok := stmt.(*parser.TypeAlias); ok && ta.Type != nil {
			if ft, ok := ta.Type.(*parser.FunctionType); ok {
				g.fnTypeAliases[ta.Name] = ft
			}
		}
	}

	// 收集標籤枚舉 & 簡單枚舉變體名稱 → 索引
	for _, stmt := range program.Statements {
		if ted, ok := stmt.(*parser.TaggedEnumDefinition); ok {
			for i, v := range ted.Variants {
				g.enumVariantIndex[v.Name] = int64(i)
				if g.varTypes != nil {
					g.varTypes[v.Name] = "i64"
				}
			}
		}
		if ed, ok := stmt.(*parser.EnumDefinition); ok {
			if g.enumVariants[ed.Name] == nil {
				g.enumVariants[ed.Name] = make(map[string]int64)
			}
			for _, v := range ed.Values {
				g.enumVariantIndex[v.Name] = v.Value
				g.enumVariants[ed.Name][v.Name] = v.Value
				if g.varTypes != nil {
					g.varTypes[v.Name] = "i64"
				}
			}
		}
	}
	// 同時收集模組級 i64 整數常量，支援命名空間風格存取（如 FileMode.WRITE）
	// 含負整數常量（如 FNV-OFFSET = -3750763034362895579）
	// 只收集大寫命名的識別符（常量命名慣例），小寫命名為變數不應被常量摺疊
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if v, ok := intConstValue(ls.Value); ok {
				name := ls.Name.Value
				if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
					g.enumVariantIndex[name] = v
				}
			}
		}
	}

	var sb strings.Builder

	sb.WriteString("; ModuleID = 'nolang'\n")
	sb.WriteString("source_filename = \"nolang\"\n")
	// 目標 datalayout / triple 由 SetTargetPlatform 填充；未設定時回退到
	// 歷史預設（macOS arm64），與舊版硬編碼行為一致。
	layout, triple := g.targetDatalayout, g.targetTriple
	if layout == "" || triple == "" {
		layout, triple = targetDatalayoutAndTriple("", "")
	}
	sb.WriteString("target datalayout = \"" + layout + "\"\n")
	sb.WriteString("target triple = \"" + triple + "\"\n\n")

	// 預掃描 FFI extern 宣告，收集型別資訊。
	// 必須在 emit declare 之前完成（declare 緊隨 writeDeclarations 之後發出）。
	for _, stmt := range program.Statements {
		if es, ok := stmt.(*parser.ExternStatement); ok && es.Name != nil {
			name := es.Name.Value
			paramTypes := make([]string, 0, len(es.Parameters))
			for _, p := range es.Parameters {
				paramTypes = append(paramTypes, ffiTypeName(p.Type))
			}
			resultTypes := make([]string, 0, len(es.Results))
			for _, r := range es.Results {
				resultTypes = append(resultTypes, ffiTypeName(r.Type))
			}
			g.externFuncs[name] = &ExternFuncInfo{
				Name:        name,
				ParamTypes:  paramTypes,
				ResultTypes: resultTypes,
			}
		}
	}

	// %task type for async/await (run/awy): { resume_fn, data, done }
	// 無棧協程：resume_fn 是 wrapper 函數指針，data 是 i64（存指針整數值，與 vec/arr/str 的 data 字段一致）
	sb.WriteString("%task = type { void (i8*)*, i64, i1 }\n")
	// %future type for lazy async futures: { wrapper_fn_ptr, args_ptr, result_ptr }
	// args_ptr/result_ptr 均為 i64（存指針整數值），通過 storeDataPtrField/loadDataPtrField 轉換
	sb.WriteString("%future = type { void (i8*)*, i64, i64 }\n")

	g.writeDeclarations(&sb)

	// Emit extern function declarations
	for _, name := range g.sortedExternNames() {
		sb.WriteString(g.formatExternDeclare(name, g.externFuncs[name]))
	}

	// 預掃描結構體欄位型別，確保函數回傳型別預掃描時 mapToLLVMType
	// 能正確解析用戶自定義結構體（如 open() (d db) 的 db 型別）。
	// 僅填充 g.structTypes，不汙染 g.varTypes（後者由 collectStructType 負責）。
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			g.collectStructTypeFields(sd)
		}
	}

	// 預掃描：收集所有函數的回傳型別和函數名
	funcNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			// Skip union monomorphization templates (e.g. max__num_TEMPLATE)
			if strings.HasSuffix(s.Name, "_TEMPLATE") {
				continue
			}
			fd := s
			// 用戶自定義函數一律使用 void + by-reference 輸出，
			// 因此 funcRetTypes 在 Nolang 函數的場景下也應為 "void"。
			// funcResultLLVMType 仍保留語意型別（如 %option / i64）用於型別推斷。
			retType := "void"
			if len(fd.Results) == 1 && fd.Results[0].Name == "" {
				retType = g.mapToLLVMType(fd.Results[0].Type.String())
			}
			g.funcRetTypes[fd.Name] = retType
			g.funcNumResults[fd.Name] = len(fd.Results)
			// 收集每個輸出參數的 LLVM 型別，供多賦值推斷變數型別使用
			if len(fd.Results) > 0 {
				rets := make([]string, len(fd.Results))
				nolangRets := make([]string, len(fd.Results))
				innerRets := make([]string, len(fd.Results))
				for i, r := range fd.Results {
					typeStr := r.Type.String()
					rets[i] = g.resolveParamLLVMType(r.Type)
					nolangRets[i] = typeStr
					if nt, ok := r.Type.(*parser.NullableType); ok {
						innerRets[i] = g.resolveParamLLVMType(nt.Type)
					} else if strings.HasPrefix(typeStr, "?") {
						innerRets[i] = g.mapToLLVMType(typeStr[1:])
					}
				}
				g.funcResultLLVMType[fd.Name] = rets
				g.funcResultNolangTypes[fd.Name] = nolangRets
				g.funcResultInnerTypes[fd.Name] = innerRets
			}
			g.funcIsVariadic[fd.Name] = fd.IsVariadic
			if fd.IsVariadic && len(fd.Parameters) > 0 {
				g.funcParamCount[fd.Name] = len(fd.Parameters) - 1 // exclude the variadic slice param
			} else {
				g.funcParamCount[fd.Name] = len(fd.Parameters)
			}
			funcNames[fd.Name] = true
			// 收集參數 LLVM 型別（fd.Parameters 已含 self，無需再從名稱前綴 prepend）
			paramTypes := make([]string, 0, len(fd.Parameters))
			paramNolangTypes := make([]string, 0, len(fd.Parameters))
			for _, p := range fd.Parameters {
				paramTypes = append(paramTypes, g.mapToLLVMType(p.Type.String()))
				paramNolangTypes = append(paramNolangTypes, p.Type.String())
			}
			g.funcParamLLVMTypes[fd.Name] = paramTypes
			g.funcParamTypes[fd.Name] = paramNolangTypes
			// 收集參數默認值
			defaults := make([]parser.Expression, len(fd.Parameters))
			for i, p := range fd.Parameters {
				defaults[i] = p.DefaultExpr
			}
			g.funcParamDefaults[fd.Name] = defaults
		case *parser.LetStatement:
			// 收集 str.to-upper = (out str, out-n i64) { ... } 等帶點名的方法定義
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok {
				if s.Name != nil && strings.Contains(s.Name.Value, ".") {
					name := s.Name.Value
					retType := "void"
					if len(fl.Results) == 1 && fl.Results[0].Name == "" {
						retType = g.mapToLLVMType(fl.Results[0].Type.String())
					}
					// 用完整名稱作為 key（含點），便於方法呼叫查找
					g.funcRetTypes[name] = retType
					g.funcNumResults[name] = len(fl.Results)
					if len(fl.Results) > 0 {
						rets := make([]string, len(fl.Results))
						nolangRets := make([]string, len(fl.Results))
						innerRets := make([]string, len(fl.Results))
						for i, r := range fl.Results {
							typeStr := r.Type.String()
							rets[i] = g.resolveParamLLVMType(r.Type)
							nolangRets[i] = typeStr
							if nt, ok := r.Type.(*parser.NullableType); ok {
								innerRets[i] = g.resolveParamLLVMType(nt.Type)
							} else if strings.HasPrefix(typeStr, "?") {
								innerRets[i] = g.mapToLLVMType(typeStr[1:])
							}
						}
						g.funcResultLLVMType[name] = rets
						g.funcResultNolangTypes[name] = nolangRets
						g.funcResultInnerTypes[name] = innerRets
					}
					g.funcIsVariadic[name] = fl.IsVariadic
					if fl.IsVariadic && len(fl.Parameters) > 0 {
						g.funcParamCount[name] = len(fl.Parameters) - 1
					} else {
						g.funcParamCount[name] = len(fl.Parameters)
					}
					funcNames[name] = true
					// 收集參數 LLVM 型別（含 receiver for methods）
					paramTypes := make([]string, 0, len(fl.Parameters)+1)
					if idx := strings.IndexByte(name, '.'); idx > 0 {
						paramTypes = append(paramTypes, g.mapToLLVMType(name[:idx]))
					}
					for _, p := range fl.Parameters {
						paramTypes = append(paramTypes, g.mapToLLVMType(p.Type.String()))
					}
					g.funcParamLLVMTypes[name] = paramTypes
					// 收集參數默認值（含 receiver slot）
					defaults := make([]parser.Expression, len(fl.Parameters)+1) // +1 for receiver
					for i, p := range fl.Parameters {
						defaults[i+1] = p.DefaultExpr
					}
					g.funcParamDefaults[name] = defaults
				} else if s.Name != nil {
					// 收集 open = (p str, opts file-opts) { ... } 等普通函數的參數型別
					name := s.Name.Value
					retType := "void"
					if len(fl.Results) == 1 && fl.Results[0].Name == "" {
						retType = g.mapToLLVMType(fl.Results[0].Type.String())
					}
					g.funcRetTypes[name] = retType
					g.funcNumResults[name] = len(fl.Results)
					if len(fl.Results) > 0 {
						rets := make([]string, len(fl.Results))
						nolangRets := make([]string, len(fl.Results))
						innerRets := make([]string, len(fl.Results))
						for i, r := range fl.Results {
							typeStr := r.Type.String()
							rets[i] = g.resolveParamLLVMType(r.Type)
							nolangRets[i] = typeStr
							if nt, ok := r.Type.(*parser.NullableType); ok {
								innerRets[i] = g.resolveParamLLVMType(nt.Type)
							} else if strings.HasPrefix(typeStr, "?") {
								innerRets[i] = g.mapToLLVMType(typeStr[1:])
							}
						}
						g.funcResultLLVMType[name] = rets
						g.funcResultNolangTypes[name] = nolangRets
						g.funcResultInnerTypes[name] = innerRets
					}
					g.funcIsVariadic[name] = fl.IsVariadic
					if fl.IsVariadic && len(fl.Parameters) > 0 {
						g.funcParamCount[name] = len(fl.Parameters) - 1
					} else {
						g.funcParamCount[name] = len(fl.Parameters)
					}
					funcNames[name] = true
					paramTypes := make([]string, 0, len(fl.Parameters))
					paramNolangTypes := make([]string, 0, len(fl.Parameters))
					for _, p := range fl.Parameters {
						paramTypes = append(paramTypes, g.mapToLLVMType(p.Type.String()))
						paramNolangTypes = append(paramNolangTypes, p.Type.String())
					}
					g.funcParamLLVMTypes[name] = paramTypes
					g.funcParamTypes[name] = paramNolangTypes
					// 收集參數默認值
					defaults := make([]parser.Expression, len(fl.Parameters))
					for i, p := range fl.Parameters {
						defaults[i] = p.DefaultExpr
					}
					g.funcParamDefaults[name] = defaults
				}
			}
		}
	}

	// 對於 0 個顯式結果的函數，掃描 body 找出被賦值的參數，這些參數實際上是輸出參數。
	// 例如 str.to-i64 = (val i64) { val = 0; ... } 中的 val 是輸出。
	g.detectOutputParamsFromBody(program, funcNames)

	// Pre-register built-in arr type (used for all fixed-size arrays)
	// data is i64 (address value) instead of i8* to keep IR type-uniform.
	g.structTypes["arr"] = []structField{
		{name: "len", typ: "i64"},
		{name: "data", typ: "i64"},
	}

	// Pre-register built-in vec type (used for all slices)
	g.structTypes["vec"] = []structField{
		{name: "len", typ: "i64"},
		{name: "cap", typ: "i64"},
		{name: "data", typ: "i64"},
	}

	// Pre-register built-in str-long type (heap-only string type)
	// { len, cap, data } — data is i64 (address value) for type-uniform IR.
	g.structTypes["str-long"] = []structField{
		{name: "len", typ: "i64"},
		{name: "cap", typ: "i64"},
		{name: "data", typ: "i64"},
	}

	// 收集結構體定義並生成 LLVM struct type
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			g.collectStructType(sd)
		}
	}

	// 發出 struct type 宣告
	// Always emit built-in string types
	// 運行時 move 標記改用函數級 u64 位圖變數（%__move_bitmap），不佔用結構體欄位。
	sb.WriteString("%str-long = type { i64, i64, i64 }\n")
	sb.WriteString("%option = type { i64, i64 }\n")
	sb.WriteString("%arr = type { i64, i64 }\n")
	sb.WriteString("%vec = type { i64, i64, i64 }\n")
	// Sort struct type names for deterministic IR output (Go map iteration is randomized).
	sortedStructs := make([]string, 0, len(g.structTypes))
	for name := range g.structTypes {
		if name == "str-long" || name == "arr" || name == "vec" {
			continue
		}
		sortedStructs = append(sortedStructs, name)
	}
	sort.Strings(sortedStructs)
	for _, name := range sortedStructs {
		fields := g.structTypes[name]
		sb.WriteString(fmt.Sprintf("%%%s = type { ", name))
		for i, f := range fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(f.typ)
		}
		sb.WriteString(" }\n")
	}
	sb.WriteString("\n")

	// 註冊結構體型別名稱到 varTypes，使得 bigint{} 等結構體字面量能正確識別型別
	// 必須在 collectVarDecls 之前執行
	for name := range g.structTypes {
		if name == "str-long" || name == "arr" || name == "vec" {
			continue
		}
		if g.varTypes == nil {
			g.varTypes = make(map[string]string)
		}
		g.varTypes[name] = "%" + name
	}

	// 預先收集所有變數型別（包括模組級常量）
	// 必須在生成函數定義之前執行，以便函數內的變數引用（如 SBOX）能正確識別型別
	varDecls := g.collectVarDecls(program)
	for k, v := range varDecls {
		g.varTypes[k] = v
	}
	// 備份模組級 option 變數的 inner type（collectVarDecls 已推導），
	// 避免後續 generateFunctionDefinition reset optionInnerTypes 時丟失，
	// 供 emitGlobalHeapFree 釋放全域 option 變數時查詢。
	g.moduleOptionInnerTypes = make(map[string]string)
	for k, v := range g.optionInnerTypes {
		g.moduleOptionInnerTypes[k] = v
	}
	// 發出模組級全局變數定義（在函數定義之前，以便所有函數都能訪問）
	// 只對以下類型的變數發出全局定義：
	// 1. i64 整數常量（如 MASK = 4294967295）
	// 2. %str-long 字串變數（如 SBOX 表）
	// 先收集所有 i64 整數常量的值，以便別名（如 o-append = FileMode.APPEND）能解析
	// 含負整數常量（如 FNV-OFFSET = -3750763034362895579）
	moduleIntConsts := make(map[string]int64)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if v, ok := intConstValue(ls.Value); ok {
				moduleIntConsts[ls.Name.Value] = v
			}
		}
	}
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			name := ls.Name.Value
			// Skip if already emitted as global (e.g., multiple let stmts with same name)
			if g.globalVars[name] {
				continue
			}
			// Skip if name conflicts with a function definition (e.g., module function
			// with same name as a top-level variable in the test file)
			if funcNames[name] {
				continue
			}
			// Skip if variable is used as a range loop variable or multi-assign
			// target. These must be local variables so the codegen (which uses
			// %var refs) can write to them. Constants defined in multiple modules
			// (like FNV-OFFSET, which only appears in LetStatements) are NOT
			// skipped — they remain globals.
			if g.rangeLoopVars != nil && g.rangeLoopVars[name] {
				continue
			}
			if g.multiAssignVars != nil && g.multiAssignVars[name] {
				continue
			}
			llvmType := g.varLLVMType(ls)
			if llvmType == "%str-long" {
				// Only emit as global for string literal constants, uninitialized
				// declarations, or with-cap/with-len allocations (which need runtime
				// init in main but must be accessible from functions).
				// Other runtime-computed strings (e.g. cmd = arg(1)) must be
				// local variables allocated in generateMainFunction.
				_, isStrLit := ls.Value.(*parser.StringLiteral)
				isWithAlloc := false
				if call, ok := ls.Value.(*parser.CallExpression); ok {
					if ident, ok := call.Function.(*parser.Identifier); ok {
						if ident.Value == "with-cap" || ident.Value == "with-len" {
							isWithAlloc = true
						}
					}
				}
				if ls.Value == nil || isStrLit || isWithAlloc {
					sb.WriteString(fmt.Sprintf("%s = global %s zeroinitializer\n", llvmGlobalRef(name), llvmType))
					g.globalVars[name] = true
				}
			} else if llvmType == "%arr" {
				sb.WriteString(fmt.Sprintf("%s = global %s zeroinitializer\n", llvmGlobalRef(name), llvmType))
				g.globalVars[name] = true
			} else if llvmType == "%vec" {
				// Top-level slice (vec) variables are emitted as globals so that
				// functions can reference them via @name (e.g. binary-trees arena).
				sb.WriteString(fmt.Sprintf("%s = global %s zeroinitializer\n", llvmGlobalRef(name), llvmType))
				g.globalVars[name] = true
			} else if strings.HasPrefix(llvmType, "[") {
				// Raw LLVM array type (e.g. [12 x [16 x i64]] for 2D array constants)
				sb.WriteString(fmt.Sprintf("%s = global %s zeroinitializer\n", llvmGlobalRef(name), llvmType))
				g.globalVars[name] = true
			} else if llvmType == "i32" && ls.Value != nil {
				// Char constant: emit as global i32
				if v, ok := intConstValue(ls.Value); ok {
					sb.WriteString(fmt.Sprintf("%s = global i32 %d\n", llvmGlobalRef(name), v))
					g.globalVars[name] = true
				}
			} else if llvmType == "i64" && ls.Value == nil {
				// i64 module-level declaration without initial value (e.g. `e2e-recv-total i64`).
				// Emit as global zero-initialized so functions can share state via @name.
				sb.WriteString(fmt.Sprintf("%s = global i64 0\n", llvmGlobalRef(name)))
				g.globalVars[name] = true
			} else if llvmType == "i64" && ls.Value != nil {
				if v, ok := intConstValue(ls.Value); ok {
					initVal := fmt.Sprintf("%d", v)
					sb.WriteString(fmt.Sprintf("%s = global i64 %s\n", llvmGlobalRef(name), initVal))
					g.globalVars[name] = true
				} else if dot, ok := ls.Value.(*parser.DotExpression); ok {
					// i64 模組級常量 = EnumName.Variant（如 o-excl = FileMode.EXCL）
					resolved := false
					if g.enumVariants != nil {
						if ident, ok := dot.Receiver.(*parser.Identifier); ok {
							if variants, ok := g.enumVariants[ident.Value]; ok {
								if val, ok := variants[dot.Property]; ok {
									sb.WriteString(fmt.Sprintf("%s = global i64 %d\n", llvmGlobalRef(name), val))
									g.globalVars[name] = true
									resolved = true
								}
							}
						}
					}
					// 若 enum 命名空間解析失敗，嘗試將 property 視為已收集的整數常量
					if !resolved {
						if val, ok := moduleIntConsts[dot.Property]; ok {
							sb.WriteString(fmt.Sprintf("%s = global i64 %d\n", llvmGlobalRef(name), val))
							g.globalVars[name] = true
						}
					}
				} else if ident, ok := ls.Value.(*parser.Identifier); ok {
					// i64 模組級常量 = 另一個整數常量（如 o-append = APPEND）
					if val, ok := moduleIntConsts[ident.Value]; ok {
						sb.WriteString(fmt.Sprintf("%s = global i64 %d\n", llvmGlobalRef(name), val))
						g.globalVars[name] = true
					}
				}
			} else if llvmType == "double" && ls.Value != nil {
				// float/double 模組級常量（如 E = 2.718282, PI = 3.141593）
				if fl, ok := ls.Value.(*parser.FloatLiteral); ok {
					floatStr := fl.Raw
					if floatStr == "" {
						// %v would produce "1" for 1.0, which is invalid LLVM IR;
						// %f always includes a decimal point (e.g., "1.000000")
						floatStr = fmt.Sprintf("%f", fl.Value)
					}
					sb.WriteString(fmt.Sprintf("%s = global double %s\n", llvmGlobalRef(name), floatStr))
					g.globalVars[name] = true
				}
			} else if strings.HasPrefix(llvmType, "%") && ls.Value == nil {
				// User-defined struct types (e.g. %tls.tls-conn, %server.https-server)
				// Emit as global when declared without an initial value (e.g. `e2e-tc tls-conn`).
				// This allows functions to reference the variable via @name.
				sb.WriteString(fmt.Sprintf("%s = global %s zeroinitializer\n", llvmGlobalRef(name), llvmType))
				g.globalVars[name] = true
			}
		}
	}
	sb.WriteString("\n")

	// 保存模組級變數型別備份，防止 generateFunctionDefinition 重置時丟失
	g.moduleVarTypes = make(map[string]string)
	for k, v := range varDecls {
		g.moduleVarTypes[k] = v
	}
	// 保存模組級陣列/切片元素型別備份，防止 generateFunctionDefinition 中
	// 函數參數（如 rsa.no bn-add 的 c []i64）覆蓋模組級同名變數（如 main.no 的 c []str）
	g.moduleArrayElemTypes = make(map[string]string)
	for k, v := range g.arrayElemTypes {
		g.moduleArrayElemTypes[k] = v
	}
	// 保存結構體型別到 moduleVarTypes（確保函數內也能識別 struct literal 型別）
	for name := range g.structTypes {
		if name == "str-long" || name == "arr" || name == "vec" {
			continue
		}
		g.moduleVarTypes[name] = "%" + name
	}

	// 掃描所有函數體，找出對模組級全域變數的重新賦值（Type==nil 的 LetStatement），
	// 將這些變數加入 reassignedVars 以避免被常量摺疊。
	// 例：fasta 的 `LAST i64 = 42` 在 gen-random 中被 `LAST = (LAST * IA + IC) % IM`
	// 重新賦值；若不標記為 reassigned，generateExprWithSB 會從 enumVariantIndex
	// 摺疊為常量 42，破壞 RNG 狀態。
	// 限制：僅主檔案函數的賦值才標記為 reassigned。
	// 導入模組的函數（如 bigint.cmp、abs-add）若有同名局部變數（如 result），
	// 不應誤標為全域 reassign，否則全域 @result 會被誤認為非常量，
	// 且局部 result 會被誤寫到全域。
	var scanGlobalReassigns func(stmts []parser.Statement, curFunc string)
	scanGlobalReassigns = func(stmts []parser.Statement, curFunc string) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *parser.LetStatement:
				// Type==nil 表示賦值（非宣告），若目標是全域變數則標記為 reassigned。
				// 但僅限主檔案函數。
				if s.Type == nil && g.globalVars != nil && g.globalVars[s.Name.Value] &&
					curFunc != "" && g.mainFileNames != nil && g.mainFileNames[curFunc] {
					g.reassignedVars[s.Name.Value] = true
				}
				// 遞迴走訪 RHS 表達式中的內嵌語句（如 IfExpression、區塊表達式）
				if s.Value != nil {
					scanGlobalReassignsExpr(s.Value, func(stmts2 []parser.Statement) {
						scanGlobalReassigns(stmts2, curFunc)
					})
				}
			case *parser.FunctionDefinition:
				if s.Body != nil {
					scanGlobalReassigns(s.Body.Statements, s.Name)
				}
			case *parser.ForStatement:
				if s.Body != nil {
					scanGlobalReassigns(s.Body.Statements, curFunc)
				}
			case *parser.ExpressionStatement:
				scanGlobalReassignsExpr(s.Expression, func(stmts2 []parser.Statement) {
					scanGlobalReassigns(stmts2, curFunc)
				})
			case *parser.MultiAssignStatement:
				if curFunc != "" && g.mainFileNames != nil && g.mainFileNames[curFunc] {
					for _, t := range s.Targets {
						if ident, ok := t.(*parser.Identifier); ok {
							if g.globalVars != nil && g.globalVars[ident.Value] {
								g.reassignedVars[ident.Value] = true
							}
						}
					}
				}
			}
		}
	}
	scanGlobalReassigns(program.Statements, "")

	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.EnumDefinition:
			// Enums are emitted via their own pass; nothing to do at module-level generation.
		case *parser.FunctionDefinition:
			// Skip union monomorphization templates (e.g. max__num_TEMPLATE)
			if strings.HasSuffix(s.Name, "_TEMPLATE") {
				continue
			}
			g.generateFunctionDefinition(&sb, s)
		case *parser.LetStatement:
			// 處理 open = (p str, opts file-opts) (f ?file) { ... } 形式的頂層函數定義
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok && s.Name != nil {
				llvmFnName := s.Name.Value
				if clibFuncNames[llvmFnName] {
					llvmFnName = "n." + llvmFnName
				}
				// 構造一個臨時 FunctionDefinition 用於 generateFunctionDefinition
				tmpFD := &parser.FunctionDefinition{
					Token: s.Token,
					Name:  llvmFnName,
					FuncSignature: parser.FuncSignature{
						Parameters: fl.Parameters,
						Results:    fl.Results,
						IsVariadic: fl.IsVariadic,
					},
					Body: fl.Body,
				}
				g.generateFunctionDefinition(&sb, tmpFD)
			}
		case *parser.ExternStatement:
			// FFI extern 宣告：型別資訊已於預掃描階段收集至 g.externFuncs，
			// declare 已緊隨 writeDeclarations 之後發出，此處無需再產生 IR。
		}
	}

	g.generateMainFunction(&sb, program)

	if len(g.fmtGlobals) > 0 {
		sb.WriteString("\n; Format string constants\n")
		for _, fg := range g.fmtGlobals {
			sb.WriteString(fg + "\n")
		}
	}

	// Async wrapper functions generated by run expressions
	sb.WriteString(g.asyncWrappers.String())

	// 无栈协程：coro_state 结构体定义已由 transformAsyncFunction 直接写入 sb（在使用前定义），
	// 此处无需再统一输出。

	// 將所有內部產生的 @malloc(i64 ...) 呼叫重定向至 @nolang.malloc(i64) wrapper。
	// wrapper 內部依目標平台決定是否需要 trunc 至 i32（WASI/wasm32 平台 wasi-libc
	// 的 malloc 簽名為 i32）。這避免了 45+ 處呼叫端個別處理平台差異。
	// 注意：字串模式 `@malloc(i64 ` (含尾部空格) 不會匹配 `declare i8* @malloc(i64)`
	// 宣告（後者為 `(i64)` 無空格），因此宣告不會被誤改。
	// 非 WASI 平台 nolang.malloc wrapper 內部使用佔位符 @nolang.real_malloc 呼叫真實
	// malloc，避免被此處的 ReplaceAll 改寫成 @nolang.malloc 造成無限遞迴；替換後還原。
	ir := sb.String()
	ir = strings.ReplaceAll(ir, "@malloc(i64 ", "@nolang.malloc(i64 ")
	ir = strings.ReplaceAll(ir, "@nolang.real_malloc(", "@malloc(")
	// WASI 平台：將所有 call i64 @read(...) / call i64 @write(...) 呼叫重定向至
	// @nolang.read / @nolang.write wrapper。wrapper 內部將 i64 count 截斷為 i32，
	// 並將 i32 返回值符號擴展回 i64，匹配 wasi-libc 的 (i32, i8*, i32) -> i32 簽名。
	// 使用 "call i64 @read(" / "call i64 @write(" 模式（含 i64），不會匹配 wrapper
	// 內部的 "call i32 @read(" / "call i32 @write("（i32），避免無限遞迴替換。
	if g.goos() == "wasi" {
		ir = strings.ReplaceAll(ir, "call i64 @read(", "call i64 @nolang.read(")
		ir = strings.ReplaceAll(ir, "call i64 @write(", "call i64 @nolang.write(")
	}
	return ir
}

// scanGlobalReassignsExpr 遞迴走訪表達式中內嵌的語句區塊（如 IfExpression 的
// Consequence/Alternative），用於偵測函數體內對模組級全域變數的重新賦值。
func scanGlobalReassignsExpr(expr parser.Expression, scan func([]parser.Statement)) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.IfExpression:
		if e.Consequence != nil {
			scan(e.Consequence.Statements)
		}
		if e.Alternative != nil {
			scan(e.Alternative.Statements)
		}
		if e.DotValBody != nil {
			scan(e.DotValBody.Statements)
		}
	case *parser.CallExpression:
		for _, arg := range e.Arguments {
			scanGlobalReassignsExpr(arg, scan)
		}
		scanGlobalReassignsExpr(e.Function, scan)
	case *parser.InfixExpression:
		scanGlobalReassignsExpr(e.Left, scan)
		scanGlobalReassignsExpr(e.Right, scan)
	case *parser.PrefixExpression:
		scanGlobalReassignsExpr(e.Right, scan)
	}
}

func llvmLLVMType(t builtin.LLVMArgType) string {
	switch t {
	case builtin.LLVMI64:
		return "i64"
	case builtin.LLVMF64:
		return "double"
	case builtin.LLVMI8Ptr:
		return "i8*"
	case builtin.LLVMI32:
		return "i32"
	case builtin.LLVMStrPtr:
		return "i8*"
	default:
		return "i64"
	}
}

// detectOutputParamsFromBody 對於 0 個顯式結果的函數，掃描 body 找出被賦值的參數，
// 將其視為輸出參數，補充 funcRetTypes / funcNumResults / funcResultLLVMType。
// 這樣 str.to-i64 = (val i64) { val = 0; ... } 中的 val 才能被識別為輸出。
func (g *Generator) detectOutputParamsFromBody(program *parser.Program, funcNames map[string]bool) {
	for _, stmt := range program.Statements {
		var fd *parser.FunctionDefinition
		var params []*parser.Parameter
		var body *parser.BlockStatement
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			fd = s
			params = s.Parameters
			body = s.Body
		case *parser.LetStatement:
			fl, ok := s.Value.(*parser.FunctionLiteral)
			if !ok || s.Name == nil {
				continue
			}
			if !strings.Contains(s.Name.Value, ".") {
				continue
			}
			// Name 包含 "." 表示這是方法定義（如 str.to-upper = (...) { }），
			// 設置 IsMethodDef=true 以與 parseMethodDefinition 產出的節點一致。
			fd = &parser.FunctionDefinition{Name: s.Name.Value, IsMethodDef: true}
			params = fl.Parameters
			body = fl.Body
		default:
			continue
		}
		if fd == nil || body == nil {
			continue
		}
		// 只處理目前 0 個結果的函數
		if n, ok := g.funcNumResults[fd.Name]; ok && n > 0 {
			continue
		}
		// 對於方法定義（IsMethodDef=true），第一個參數是 receiver（self），
		// 它是指標傳遞、原地修改，不應視為輸出參數。跳過 self 進行分析。
		// 直接讀取 IsMethodDef 欄位（parser 在方法定義位置顯式設置），
		// 避免依賴 `strings.Contains(fd.Name, ".")` 字串子串啟發式。
		analyzeParams := params
		if fd.IsMethodDef && len(params) > 0 {
			analyzeParams = params[1:]
		}
		// 使用源碼順序遍歷分析參數使用情況，區分輸入和輸出參數。
		// 核心啟發式：若參數在賦值前被讀取（如 str.slice 的 start 在 `if start < 0` 中先讀，
		// 然後才 `start = 0`），則為輸入參數；若參數先被賦值才被讀取（如 str.slice 的 out
		// 先 `out.len = 0` 再 `for i < out.len`），則為輸出參數。
		outputs := g.analyzeParamUsage(body, analyzeParams)
		if len(outputs) == 0 {
			continue
		}
		// 更新預掃描的表
		// 始終保持 retType 為 void — 即使是單輸出參數也通過指標傳遞，
		// 與多輸出函數保持一致，避免 detectOutputParamsFromBody 與 generateFunctionDefinition
		// 之間的簽名不一致（前者可能設為 i64，但後者基於原始 fd.Results=void 生成 void 函數）。
		retType := "void"
		g.funcRetTypes[fd.Name] = retType
		g.funcNumResults[fd.Name] = len(outputs)
		// 標記為啟發式檢測的輸出：這些參數已存在於 fd.Parameters 中，
		// 函數定義已將其作為常規 LLVM 參數生成，調用方不需額外返回槽。
		g.funcHeuristicOutput[fd.Name] = true
		rets := make([]string, len(outputs))
		nolangRets := make([]string, len(outputs))
		innerRets := make([]string, len(outputs))
		for i, p := range outputs {
			typeStr := p.Type.String()
			rets[i] = g.resolveParamLLVMType(p.Type)
			nolangRets[i] = typeStr
			if nt, ok := p.Type.(*parser.NullableType); ok {
				innerRets[i] = g.resolveParamLLVMType(nt.Type)
			} else if strings.HasPrefix(typeStr, "?") {
				innerRets[i] = g.mapToLLVMType(typeStr[1:])
			}
		}
		g.funcResultLLVMType[fd.Name] = rets
		g.funcResultNolangTypes[fd.Name] = nolangRets
		g.funcResultInnerTypes[fd.Name] = innerRets
	}
}

// analyzeParamUsage 通過源碼順序遍歷函數體，判斷每個參數是輸入還是輸出。
// 狀態機：0=未讀, 1=賦值前已讀（輸入）, 2=已賦值（可能為輸出）。
// 輸出參數 = 最終狀態為 2（被賦值且 從未在賦值前被讀取）。
func (g *Generator) analyzeParamUsage(body *parser.BlockStatement, params []*parser.Parameter) []*parser.Parameter {
	state := make(map[string]int)
	for _, p := range params {
		state[p.Name] = 0
	}
	g.walkStmtForAnalysis(body, state)
	var outputs []*parser.Parameter
	for _, p := range params {
		if state[p.Name] == 2 {
			outputs = append(outputs, p)
		}
	}
	return outputs
}

// walkStmtForAnalysis 按源碼順序走訪語句，更新參數狀態。
func (g *Generator) walkStmtForAnalysis(stmt parser.Statement, state map[string]int) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		// RHS 先讀取，再賦值給 LHS
		if s.Value != nil {
			g.walkExprReadsForAnalysis(s.Value, state)
		}
		if s.Name != nil {
			g.markParamAssigned(s.Name.Value, state)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			g.walkExprReadsForAnalysis(s.Value, state)
		}
		for _, target := range s.Targets {
			if ident, ok := target.(*parser.Identifier); ok {
				g.markParamAssigned(ident.Value, state)
			}
		}
	case *parser.ForStatement:
		// for init; cond; update { body }
		if s.Init != nil {
			g.walkStmtForAnalysis(s.Init, state)
		}
		if s.IterRange != nil {
			if s.IterRange.Variable != "" {
				g.markParamAssigned(s.IterRange.Variable, state)
			}
			if s.IterRange.Range != nil {
				if s.IterRange.Range.Start != nil {
					g.walkExprReadsForAnalysis(s.IterRange.Range.Start, state)
				}
				if s.IterRange.Range.End != nil {
					g.walkExprReadsForAnalysis(s.IterRange.Range.End, state)
				}
			}
			if s.IterRange.RangeExpr != nil {
				g.walkExprReadsForAnalysis(s.IterRange.RangeExpr, state)
			}
		}
		if s.Condition != nil {
			g.walkExprReadsForAnalysis(s.Condition, state)
		}
		if s.CountExpr != nil {
			g.walkExprReadsForAnalysis(s.CountExpr, state)
		}
		if s.Body != nil {
			for _, ss := range s.Body.Statements {
				g.walkStmtForAnalysis(ss, state)
			}
		}
		if s.Update != nil {
			g.walkStmtForAnalysis(s.Update, state)
		}
	case *parser.BlockStatement:
		for _, ss := range s.Statements {
			g.walkStmtForAnalysis(ss, state)
		}
	case *parser.ExpressionStatement:
		g.walkExprForAnalysis(s.Expression, state)
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			g.walkExprReadsForAnalysis(s.ReturnValue, state)
		}
	}
}

// walkExprForAnalysis 處理表達式語句，識別賦值表達式並按正確順序處理讀寫。
func (g *Generator) walkExprForAnalysis(expr parser.Expression, state map[string]int) {
	if expr == nil {
		return
	}
	if assign, ok := expr.(*parser.AssignExpression); ok {
		// RHS 先讀取
		if assign.Value != nil {
			g.walkExprReadsForAnalysis(assign.Value, state)
		}
		// 然後處理 LHS 的賦值
		switch lhs := assign.Left.(type) {
		case *parser.Identifier:
			g.markParamAssigned(lhs.Value, state)
		case *parser.DotExpression:
			// out.len = v → out 被賦值（透過欄位）
			if recv, ok := lhs.Receiver.(*parser.Identifier); ok {
				g.markParamAssigned(recv.Value, state)
			} else {
				// 嵌套的 DotExpression，如 a.b.c = v，保守地不處理
				g.walkExprReadsForAnalysis(lhs.Receiver, state)
			}
		case *parser.IndexExpression:
			// out[i] = v → out 被賦值（透過索引），i 被讀取
			if id, ok := lhs.Left.(*parser.Identifier); ok {
				g.markParamAssigned(id.Value, state)
			} else {
				g.walkExprReadsForAnalysis(lhs.Left, state)
			}
			g.walkExprReadsForAnalysis(lhs.Index, state)
		default:
			// 其他 LHS 類型，保守地讀取
			g.walkExprReadsForAnalysis(assign.Left, state)
		}
		return
	}
	// 非賦值表達式：全部視為讀取
	g.walkExprReadsForAnalysis(expr, state)
}

// walkExprReadsForAnalysis 走訪表達式中的所有變數讀取。
func (g *Generator) walkExprReadsForAnalysis(expr parser.Expression, state map[string]int) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		g.markParamRead(e.Value, state)
	case *parser.AssignExpression:
		// 嵌套賦值表達式：按完整賦值處理
		g.walkExprForAnalysis(e, state)
	case *parser.InfixExpression:
		g.walkExprReadsForAnalysis(e.Left, state)
		g.walkExprReadsForAnalysis(e.Right, state)
	case *parser.PrefixExpression:
		g.walkExprReadsForAnalysis(e.Right, state)
	case *parser.CallExpression:
		g.walkExprReadsForAnalysis(e.Function, state)
		for _, arg := range e.Arguments {
			g.walkExprReadsForAnalysis(arg, state)
		}
	case *parser.DotExpression:
		// s.len → s 被讀取
		g.walkExprReadsForAnalysis(e.Receiver, state)
	case *parser.IndexExpression:
		// a[i] → a 和 i 都被讀取
		g.walkExprReadsForAnalysis(e.Left, state)
		g.walkExprReadsForAnalysis(e.Index, state)
	case *parser.IfExpression:
		if e.Condition != nil {
			g.walkExprReadsForAnalysis(e.Condition, state)
		}
		if e.Consequence != nil {
			for _, ss := range e.Consequence.Statements {
				g.walkStmtForAnalysis(ss, state)
			}
		}
		if e.Alternative != nil {
			for _, ss := range e.Alternative.Statements {
				g.walkStmtForAnalysis(ss, state)
			}
		}
	case *parser.GroupedExpression:
		g.walkExprReadsForAnalysis(e.Expression, state)
	case *parser.SliceExpression:
		g.walkExprReadsForAnalysis(e.Left, state)
		if e.Range != nil {
			if e.Range.Start != nil {
				g.walkExprReadsForAnalysis(e.Range.Start, state)
			}
			if e.Range.End != nil {
				g.walkExprReadsForAnalysis(e.Range.End, state)
			}
		}
	case *parser.ConditionalExpression:
		g.walkExprReadsForAnalysis(e.Condition, state)
		g.walkExprReadsForAnalysis(e.Consequence, state)
		g.walkExprReadsForAnalysis(e.Alternative, state)
	case *parser.RangeExpression:
		if e.Start != nil {
			g.walkExprReadsForAnalysis(e.Start, state)
		}
		if e.End != nil {
			g.walkExprReadsForAnalysis(e.End, state)
		}
		// Literals (Integer, Float, String, Char, Byte, Boolean, Nil) — no reads
	}
}

// markParamRead 標記參數被讀取。若參數尚未被賦值（狀態 0），則轉為「賦值前已讀」（狀態 1）。
func (g *Generator) markParamRead(name string, state map[string]int) {
	if s, ok := state[name]; ok && s == 0 {
		state[name] = 1
	}
}

// markParamAssigned 標記參數被賦值。僅當參數尚未被讀取（狀態 0）時才轉為「已賦值」（狀態 2）。
// 若參數已被讀取（狀態 1），則保持不變 — 這表示它是輸入參數（在賦值前被讀取）。
func (g *Generator) markParamAssigned(name string, state map[string]int) {
	if s, ok := state[name]; ok && s == 0 {
		state[name] = 2
	}
}

func (g *Generator) genCLibCall(sb *strings.Builder, m *builtin.BuiltinMethod, evalArgs func() []string, origExprs []parser.Expression) string {
	a := evalArgs()
	clib := m.CLibCall

	// 變參函數需帶上完整函數類型簽名，否則變參部分可能傳遞錯誤
	sigStr := ""
	if sig := clibCallSig(clib.FuncName); sig != "" {
		sigStr = " " + sig
	}

	// Build argument string
	evIdx := 0
	argStr := ""
	for i := 0; i < len(clib.ArgTypes); i++ {
		if i > 0 {
			argStr += ", "
		}
		argType := clib.ArgTypes[i]

		// RetBuf 模式：第一個 i8* 參數使用 BufGlobal 作為緩衝區指針
		if clib.RetBuf && i == 0 && argType == builtin.LLVMI8Ptr && clib.BufGlobal != "" {
			argStr += "i8* getelementptr inbounds ([1024 x i8], [1024 x i8]* " + clib.BufGlobal + ", i64 0, i64 0)"
			evIdx++
			continue
		}

		if fixedVal, ok := clib.FixedArgs[i]; ok {
			argStr += llvmLLVMType(argType) + " " + fixedVal
			evIdx++
			continue
		}

		if fixedGlobal, ok := clib.FixedArgGlobals[i]; ok {
			// value is a full LLVM expression including the type prefix
			argStr += fixedGlobal
			evIdx++
			continue
		}

		if truncTo, ok := clib.TruncArgs[i]; ok {
			if evIdx >= len(a) {
				argStr += llvmLLVMType(truncTo) + " 0"
				evIdx++
				continue
			}
			g.tmpIdx++
			truncReg := fmt.Sprintf("%%clib.trunc.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = trunc i64 %s to %s\n", g.indent(), truncReg, a[evIdx], llvmLLVMType(truncTo)))
			}
			argStr += llvmLLVMType(truncTo) + " " + truncReg
			evIdx++
			continue
		}

		if clib.StrDataArg != nil && clib.StrDataArg[i] {
			if evIdx < len(a) {
				dataPtr := g.extractStrFromEvalArg(sb, a[evIdx])
				argStr += "i8* " + dataPtr
			} else {
				argStr += "i8* null"
			}
			evIdx++
			continue
		}

		if argType == builtin.LLVMStrPtr {
			if evIdx < len(a) {
				// 使用 makeNullTerminatedStr 确保传递给 C 函数的字符串有 null 终止符
				if origExprs != nil && evIdx < len(origExprs) {
					nullTermPtr := g.makeNullTerminatedStr(sb, origExprs[evIdx])
					if nullTermPtr != "" {
						argStr += "i8* " + nullTermPtr
					} else {
						dataPtr := g.extractStrFromEvalArg(sb, a[evIdx])
						argStr += "i8* " + dataPtr
					}
				} else {
					dataPtr := g.extractStrFromEvalArg(sb, a[evIdx])
					argStr += "i8* " + dataPtr
				}
			} else {
				argStr += "i8* null"
			}
		} else {
			if evIdx < len(a) {
				argStr += llvmLLVMType(argType) + " " + a[evIdx]
			} else {
				argStr += llvmLLVMType(argType) + " 0"
			}
		}
		evIdx++
	}

	// RetBuf: return the buffer pointer instead of C return value
	// 同時需要把 C 字串（null 結尾的 i8*）轉換為 Nolang %str-long
	if clib.RetBuf {
		bufExpr := fmt.Sprintf("getelementptr inbounds ([1024 x i8], [1024 x i8]* %s, i64 0, i64 0)", clib.BufGlobal)
		buf := "i8* " + bufExpr
		cRetType := llvmLLVMType(clib.RetType)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%scall %s%s @%s(%s)\n", g.indent(), cRetType, sigStr, clib.FuncName, argStr))
		}
		// 如果返回型別是 str，需把 buf 中的 C 字串包裝成 %str-long
		// 通過 strlen 計算長度，並把 (len, ptr) 寫入新的 %str-long 值
		returnsStr := false
		for _, t := range m.Return {
			if t == parser.TypeStr {
				returnsStr = true
				break
			}
		}
		if clib.RetType == builtin.LLVMI8Ptr || returnsStr {
			g.tmpIdx++
			lenReg := fmt.Sprintf("%%retbuf.len.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(%s)\n", g.indent(), lenReg, buf))
			}
			g.tmpIdx++
			strReg1 := fmt.Sprintf("%%retbuf.val.%d", g.tmpIdx)
			g.tmpIdx++
			strReg2 := fmt.Sprintf("%%retbuf.val.%d", g.tmpIdx)
			g.tmpIdx++
			strReg3 := fmt.Sprintf("%%retbuf.val.%d", g.tmpIdx)
			if sb != nil {
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), strReg1, lenReg))
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), strReg2, strReg1, lenReg))
				// Convert i8* buf to i64 for data field
				g.tmpIdx++
				bufPtrReg := g.ptrToIntVal(sb, buf)
				sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), strReg3, strReg2, bufPtrReg))
			}
			return strReg3
		}
		return buf
	}

	// RetCStrToStr: C 函數返回 i8* (C 字串)，需包裝為 Nolang %str-long
	// 1) 調用 C 函數取得 i8* 指針（可能為 NULL，如 getenv 找不到變數時）
	// 2) 若為 NULL：返回 nil %str-long（data=0），使 `v == nil` 成立
	// 3) 若非 NULL：複製 C 字串到 malloc'd 緩衝區（避免 emitHeapFree 釋放 libc 靜態記憶體）
	// 4) 構造 %str-long 並通過 insertvalue 設定 (len, len, ptr)
	// 5) 返回 %str-long 結構體值（不是 i8*）
	if clib.RetCStrToStr {
		// 使用單一 tmpIdx 作為所有暫存器與標籤的後綴，確保同函數內
		// 多次呼叫 RetCStrToStr 路徑時標籤（cstr.nil/cstr.copy/cstr.merge）
		// 不會衝突。LLVM 要求同一函數內基本塊標籤唯一。
		g.tmpIdx++
		id := g.tmpIdx
		cstrReg := fmt.Sprintf("%%cstr.ptr.%d", id)
		nullCmpReg := fmt.Sprintf("%%cstr.null.%d", id)
		lenReg := fmt.Sprintf("%%cstr.len.%d", id)
		bufSizeReg := fmt.Sprintf("%%cstr.bufsize.%d", id)
		bufReg := fmt.Sprintf("%%cstr.buf.%d", id)
		nullPosReg := fmt.Sprintf("%%cstr.nullpos.%d", id)
		mergeLenReg := fmt.Sprintf("%%cstr.mlen.%d", id)
		mergeDataReg := fmt.Sprintf("%%cstr.mdata.%d", id)
		mergeStr1 := fmt.Sprintf("%%cstr.mval1.%d", id)
		mergeStr2 := fmt.Sprintf("%%cstr.mval2.%d", id)
		mergeStr3 := fmt.Sprintf("%%cstr.mval3.%d", id)
		nilLabel := fmt.Sprintf("cstr.nil.%d", id)
		copyLabel := fmt.Sprintf("cstr.copy.%d", id)
		mergeLabel := fmt.Sprintf("cstr.merge.%d", id)
		if sb != nil {
			// 1) 調用 C 函數取得 i8*
			sb.WriteString(fmt.Sprintf("%s%s = call i8*%s @%s(%s)\n", g.indent(), cstrReg, sigStr, clib.FuncName, argStr))
			// 2) 檢查 NULL：若為 NULL 則 data=null/len=0，使 `v == nil` 成立
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmpReg, cstrReg))
			sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmpReg, nilLabel, copyLabel))
			// nil block: jump directly to merge (PHI picks 0/null)
			sb.WriteString(nilLabel + ":\n")
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))
			// copy block: strlen + malloc + memcpy + null-terminate
			sb.WriteString(copyLabel + ":\n")
			sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, cstrReg))
			sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), bufSizeReg, lenReg))
			sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, bufSizeReg))
			sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), bufReg, cstrReg, lenReg))
			sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), nullPosReg, bufReg, lenReg))
			sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPosReg))
			sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))
			// merge block: PHI for len and data pointer
			sb.WriteString(mergeLabel + ":\n")
			sb.WriteString(fmt.Sprintf("%s%s = phi i64 [0, %%%s], [%s, %%%s]\n", g.indent(), mergeLenReg, nilLabel, lenReg, copyLabel))
			sb.WriteString(fmt.Sprintf("%s%s = phi i8* [null, %%%s], [%s, %%%s]\n", g.indent(), mergeDataReg, nilLabel, bufReg, copyLabel))
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), mergeStr1, mergeLenReg))
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), mergeStr2, mergeStr1, mergeLenReg))
			_p2i_mergeStr3 := g.ptrToIntVal(sb, mergeDataReg)
			sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), mergeStr3, mergeStr2, _p2i_mergeStr3))
		}
		return mergeStr3
	}

	cRetType := llvmLLVMType(clib.RetType)
	if clib.RetType == builtin.LLVMStrPtr {
		cRetType = "i8*"
	}

	if clib.RetExt != nil {
		g.tmpIdx++
		callReg := fmt.Sprintf("%%clib.ret.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call %s%s @%s(%s)\n", g.indent(), callReg, cRetType, sigStr, clib.FuncName, argStr))
		}
		g.tmpIdx++
		extReg := fmt.Sprintf("%%clib.ext.%d", g.tmpIdx)
		extInstr := "zext"
		if clib.RetType == builtin.LLVMI32 {
			extInstr = "sext"
		}
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = %s %s %s to i64\n", g.indent(), extReg, extInstr, cRetType, callReg))
		}
		return extReg
	}

	if clib.CmpRet {
		g.tmpIdx++
		callReg := fmt.Sprintf("%%clib.ret.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = call %s%s @%s(%s)\n", g.indent(), callReg, cRetType, sigStr, clib.FuncName, argStr))
		}
		g.tmpIdx++
		cmpReg := fmt.Sprintf("%%clib.cmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = icmp eq %s %s, 0\n", g.indent(), cmpReg, cRetType, callReg))
		}
		g.tmpIdx++
		extReg := fmt.Sprintf("%%clib.ext.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = zext i1 %s to i64\n", g.indent(), extReg, cmpReg))
		}
		return extReg
	}

	return fmt.Sprintf("call %s%s @%s(%s)", cRetType, sigStr, clib.FuncName, argStr)
}

func (g *Generator) extractStrFromEvalArg(sb *strings.Builder, evalResult string) string {
	if strings.HasPrefix(evalResult, "%") {
		// evalResult 可能是以下形式：
		//   1. %key           — 直接是 %str-long* 指針
		//   2. %key.val.N     — load 出來的 %str-long 值
		//   3. %dot.val.N     — 從 struct 欄位載入的值（%dot 不是有效指針）
		// extractStrDataPtr 需要 %str-long* 指針。
		baseRef := evalResult
		loadedValue := ""
		if idx := strings.Index(evalResult, ".val."); idx > 0 {
			loadedValue = evalResult
			baseRef = evalResult[:idx]
		}
		parts := strings.Split(baseRef, ".")
		varName := strings.TrimPrefix(parts[0], "%")
		// 若有 loaded value 且 varName 不在 varTypes（例如來自 DotExpression 的 %dot）
		// 則需先將值存入臨時變量，再從臨時變量指針提取資料指針。
		if loadedValue != "" {
			_, known := func() (string, bool) {
				if g.varTypes == nil {
					return "", false
				}
				t, ok := g.varTypes[varName]
				return t, ok
			}()
			if !known {
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%str.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%str-long\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%str-long %s, %%str-long* %s\n", g.indent(), loadedValue, tmpAlloca))
				}
				return g.extractStrDataPtr(sb, tmpAlloca)
			}
		}
		if g.varTypes != nil {
			if _, ok := g.varTypes[varName]; ok {
				return g.extractStrDataPtr(sb, baseRef)
			}
		}
		return g.extractStrDataPtr(sb, baseRef)
	}
	return evalResult
}

// sliceEvalArgToPtr resolves an eval result (from generateExprWithSB) to a %vec* pointer.
// If the eval result is a loaded value (%var.val.N), store it into a temporary alloca
// and return the alloca pointer. If it's already a pointer, return as-is.
func (g *Generator) sliceEvalArgToPtr(sb *strings.Builder, evalResult string) string {
	if !strings.HasPrefix(evalResult, "%") {
		return evalResult
	}
	// Check if it's a loaded value (contains ".val.")
	if idx := strings.Index(evalResult, ".val."); idx > 0 {
		baseRef := evalResult[:idx]
		varName := strings.TrimPrefix(baseRef, "%")
		// If the variable is a known %vec, the loaded value is the struct value;
		// store it into a temp alloca to get a pointer.
		if g.varTypes != nil {
			if t, ok := g.varTypes[varName]; ok && t == "%vec" {
				g.tmpIdx++
				tmpAlloca := fmt.Sprintf("%%vec.tmp.%d", g.tmpIdx)
				if sb != nil {
					sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), tmpAlloca))
					sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %s\n", g.indent(), evalResult, tmpAlloca))
				}
				return tmpAlloca
			}
		}
		// Unknown variable type — still store into temp
		g.tmpIdx++
		tmpAlloca := fmt.Sprintf("%%vec.tmp.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = alloca %%vec\n", g.indent(), tmpAlloca))
			sb.WriteString(fmt.Sprintf("%sstore %%vec %s, %%vec* %s\n", g.indent(), evalResult, tmpAlloca))
		}
		return tmpAlloca
	}
	// Already a pointer (e.g., %varname or %vec.tmp.N)
	return evalResult
}

func (g *Generator) genLLVMConv(sb *strings.Builder, m *builtin.BuiltinMethod, evalArgs func() []string) string {
	a := evalArgs()
	conv := *m.LLVMConv
	switch conv {
	case builtin.LLVMConvI64ToFP:
		g.tmpIdx++
		reg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = sitofp i64 %s to double\n", g.indent(), reg, a[0]))
		}
		return reg
	case builtin.LLVMConvFPToI64:
		g.tmpIdx++
		reg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = fptosi double %s to i64\n", g.indent(), reg, a[0]))
		}
		return reg
	case builtin.LLVMConvF64ToF32:
		g.tmpIdx++
		reg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = fptrunc double %s to float\n", g.indent(), reg, a[0]))
		}
		return reg
	case builtin.LLVMConvF32ToF64:
		g.tmpIdx++
		reg := fmt.Sprintf("%%conv.%d", g.tmpIdx)
		if sb != nil {
			sb.WriteString(fmt.Sprintf("%s%s = fpext float %s to double\n", g.indent(), reg, a[0]))
		}
		return reg
	}
	return ""
}

// storeDataPtrField stores an i8* value into a struct's data field (which is i64).
// Emits ptrtoint i8* → i64, then store i64.
// ptrVal may include an "i8* " type prefix (e.g. from GEP expressions), which is stripped.
func (g *Generator) storeDataPtrField(sb *strings.Builder, ptrVal string, fieldGEP string) {
	if ptrVal == "null" {
		sb.WriteString(fmt.Sprintf("%sstore i64 0, i64* %s\n", g.indent(), fieldGEP))
		return
	}
	// Strip "i8* " prefix if present (e.g. "i8* getelementptr ...")
	ptrVal = strings.TrimPrefix(ptrVal, "i8* ")
	g.tmpIdx++
	intReg := fmt.Sprintf("%%d2i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), intReg, ptrVal))
	sb.WriteString(fmt.Sprintf("%sstore i64 %s, i64* %s\n", g.indent(), intReg, fieldGEP))
}

// loadDataPtrField loads an i8* value from a struct's data field (which is i64).
// Emits load i64, then inttoptr i64 → i8*. Returns the i8* register name.
func (g *Generator) loadDataPtrField(sb *strings.Builder, fieldGEP string) string {
	g.tmpIdx++
	intReg := fmt.Sprintf("%%i2d.%d", g.tmpIdx)
	g.tmpIdx++
	ptrReg := fmt.Sprintf("%%dptr.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = load i64, i64* %s\n", g.indent(), intReg, fieldGEP))
	sb.WriteString(fmt.Sprintf("%s%s = inttoptr i64 %s to i8*\n", g.indent(), ptrReg, intReg))
	return ptrReg
}

// ptrToIntVal converts an i8* value to i64 for use in insertvalue.
// Returns the i64 value (register name or "0" for null).
// Strips any leading "i8* " prefix from ptrVal to avoid duplicate type annotations.
func (g *Generator) ptrToIntVal(sb *strings.Builder, ptrVal string) string {
	if ptrVal == "null" {
		return "0"
	}
	ptrVal = strings.TrimPrefix(ptrVal, "i8* ")
	g.tmpIdx++
	intReg := fmt.Sprintf("%%p2i.%d", g.tmpIdx)
	sb.WriteString(fmt.Sprintf("%s%s = ptrtoint i8* %s to i64\n", g.indent(), intReg, ptrVal))
	return intReg
}

// emitFFIExternStrClone 將 FFI extern 函數返回的 C 字串指標（i8*）複製到
// malloc'd 緩衝區，構造為獨立擁有的 %str-long。
// 與 RetCStrToStr 路徑（generator.go:1763）邏輯一致，避免 emitHeapFree
// 釋放 C 靜態字串（getenv/strerror 等）導致未定義行為。
//
// 流程：
//  1. NULL 檢查：若 C 返回 NULL，構造 nil %str-long（data=0）
//  2. 非 NULL：strlen + malloc + memcpy + null 終止
//  3. PHI 合併兩條路徑，構造 %str-long 返回
func (g *Generator) emitFFIExternStrClone(sb *strings.Builder, cstrPtr string) string {
	g.tmpIdx++
	id := g.tmpIdx
	nullCmpReg := fmt.Sprintf("%%fstr.null.%d", id)
	nilLabel := fmt.Sprintf("fstr.nil.%d", id)
	copyLabel := fmt.Sprintf("fstr.copy.%d", id)
	mergeLabel := fmt.Sprintf("fstr.merge.%d", id)
	lenReg := fmt.Sprintf("%%fstr.len.%d", id)
	bufSizeReg := fmt.Sprintf("%%fstr.bufsize.%d", id)
	bufReg := fmt.Sprintf("%%fstr.buf.%d", id)
	nullPosReg := fmt.Sprintf("%%fstr.nullpos.%d", id)
	mergeLenReg := fmt.Sprintf("%%fstr.mlen.%d", id)
	mergeDataReg := fmt.Sprintf("%%fstr.mdata.%d", id)
	mergeStr1 := fmt.Sprintf("%%fstr.mval1.%d", id)
	mergeStr2 := fmt.Sprintf("%%fstr.mval2.%d", id)
	mergeStr3 := fmt.Sprintf("%%fstr.mval3.%d", id)

	if sb != nil {
		// NULL 檢查
		sb.WriteString(fmt.Sprintf("%s%s = icmp eq i8* %s, null\n", g.indent(), nullCmpReg, cstrPtr))
		sb.WriteString(fmt.Sprintf("%sbr i1 %s, label %%%s, label %%%s\n", g.indent(), nullCmpReg, nilLabel, copyLabel))
		// nil block: data=null, len=0（使 `s == nil` 成立）
		g.emitLabel(sb, nilLabel)
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))
		// copy block: strlen + malloc + memcpy + null 終止
		g.emitLabel(sb, copyLabel)
		sb.WriteString(fmt.Sprintf("%s%s = call i64 @nolang.strlen(i8* %s)\n", g.indent(), lenReg, cstrPtr))
		sb.WriteString(fmt.Sprintf("%s%s = add i64 %s, 1\n", g.indent(), bufSizeReg, lenReg))
		sb.WriteString(fmt.Sprintf("%s%s = call i8* @malloc(i64 %s)\n", g.indent(), bufReg, bufSizeReg))
		sb.WriteString(fmt.Sprintf("%scall void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", g.indent(), bufReg, cstrPtr, lenReg))
		sb.WriteString(fmt.Sprintf("%s%s = getelementptr inbounds i8, i8* %s, i64 %s\n", g.indent(), nullPosReg, bufReg, lenReg))
		sb.WriteString(fmt.Sprintf("%sstore i8 0, i8* %s\n", g.indent(), nullPosReg))
		sb.WriteString(fmt.Sprintf("%sbr label %%%s\n", g.indent(), mergeLabel))
		// merge block: PHI 合併
		g.emitLabel(sb, mergeLabel)
		sb.WriteString(fmt.Sprintf("%s%s = phi i64 [0, %%%s], [%s, %%%s]\n", g.indent(), mergeLenReg, nilLabel, lenReg, copyLabel))
		sb.WriteString(fmt.Sprintf("%s%s = phi i8* [null, %%%s], [%s, %%%s]\n", g.indent(), mergeDataReg, nilLabel, bufReg, copyLabel))
		sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long zeroinitializer, i64 %s, 0\n", g.indent(), mergeStr1, mergeLenReg))
		sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 1\n", g.indent(), mergeStr2, mergeStr1, mergeLenReg))
		_p2i_mergeStr3 := g.ptrToIntVal(sb, mergeDataReg)
		sb.WriteString(fmt.Sprintf("%s%s = insertvalue %%str-long %s, i64 %s, 2\n", g.indent(), mergeStr3, mergeStr2, _p2i_mergeStr3))
	}
	return mergeStr3
}

// isUserStructType 判斷 LLVM 型別字串是否為用戶自定義結構體。
// 用戶結構體以 '%' 開頭，但排除內建型別（%vec, %str-long, %arr, %option, %task, %future）。
// 且該名稱必須存在於 g.structTypes 中。
func (g *Generator) isUserStructType(llvmType string) bool {
	if !strings.HasPrefix(llvmType, "%") {
		return false
	}
	switch llvmType {
	case "%vec", "%str-long", "%arr", "%option", "%task", "%future":
		return false
	}
	structName := strings.TrimPrefix(llvmType, "%")
	if g.structTypes == nil {
		return false
	}
	_, ok := g.structTypes[structName]
	return ok
}

// isHeapOwningType 判斷 LLVM 型別是否擁有堆數據（需要深層 free）。
// %vec、%str-long、%arr 以及含堆欄位的用戶結構體都屬於此類。
func (g *Generator) isHeapOwningType(llvmType string) bool {
	switch llvmType {
	case "%vec", "%str-long", "%arr":
		return true
	default:
		return g.isUserStructType(llvmType)
	}
}

func (g *Generator) findLoopTarget(label string, isBreak bool) string {
	if label != "" {
		for i := len(g.loopExits) - 1; i >= 0; i-- {
			if g.loopExits[i].name == label {
				if isBreak {
					return g.loopExits[i].exit
				}
				return g.loopExits[i].cond
			}
		}
	}
	// 未命名或标签未找到：使用最近循环
	if len(g.loopExits) > 0 {
		last := g.loopExits[len(g.loopExits)-1]
		if isBreak {
			return last.exit
		}
		return last.cond
	}
	return ""
}
