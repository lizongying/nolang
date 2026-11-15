package build

// fmt_signatures.go — `no fmt` 的跨模組簽名載入器。
//
// `no build` / `no test` 走 CompileTarget → preloadModuleSignatures →
// SemRebuildConfig.Apply；`no fmt` 過去是裸 parser.New + SkipLowering，
// 拿不到任何跨模組簽名（fmt/expr.go 多處註解逐字承認這件事）。本檔案把
//「簽名表」這一段抽成可獨立使用的元件：
//
//   - std 內嵌簽名表（stdsig_gen.go）在 NewFmtSigLoader 取一次；
//   - 原始碼裡 use / # 指向的本地模組，按路徑快取並真正 resolveUse 載入；
//   - 三張表按「排序後的 use 集合」快取，目錄批次格式化不會逐檔重掃。
//
// 產出是 parser.SemRebuildConfig，可直接 cfg.Apply(p)，與 build / vet 共用
// 同一份注入協定（見 parser/semviews.go）。fmt 包本身不 import checker /
// build，只接收這份 cfg，維持既有分層與「fmt 不碰 checker」的約束。
//
// ⚠️ 這只解決語義判定，不解決 AST 全量重渲染的排版漂移；AGENTS.md §6.6
// 「不要對 src/std 跑 no fmt -w」的規約仍然成立。

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// collectModuleSignatures 從已解析的 Program 收集函數簽名 / 方法簽名 / struct
// 欄位型別，寫入傳入的三張表。這是 preloadModuleSignatures 與 FmtSigLoader
// 共用的單一來源，兩邊對「方法鍵、陣列方法裸名、struct 欄位」的判定不會漂移。
//
// modShort 是模組短名（如 /src/tail.tail → tail），用來組成 mod.fn 全名鍵。
func collectModuleSignatures(modProg *parser.Program, modShort string,
	funcSigs, methodSigs map[string][]string,
	structFields map[string]map[string]string) {
	if modProg == nil {
		return
	}
	for _, stmt := range modProg.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			// DeclaredResults 剔除方法定義的隱式 `self` 接收者：這張表供 let
			// 型別推斷使用（parser/type.go 的 len(rets)==1 判定），留著 self
			// 會讓無回傳值的方法被推成「回傳接收者型別」。
			if decl := parser.DeclaredResults(fd); len(decl) > 0 {
				rets := make([]string, 0, len(decl))
				for _, r := range decl {
					rets = append(rets, r.Type.String())
				}
				if fd.IsMethodDef && len(fd.Name) > 0 && fd.Name[0] != '[' {
					fullKey := modShort + "." + fd.Name
					methodSigs[fullKey] = rets
					// 同時以 fd.Name 原樣登記（如 "str.starts-with"），讓
					// receiverType + "." + dot.Property 也能命中。
					if strings.Contains(fd.Name, ".") {
						methodSigs[fd.Name] = rets
					}
				} else if len(fd.Name) > 0 && fd.Name[0] != '[' {
					funcSigs[modShort+"."+fd.Name] = rets
				} else {
					// 陣列 / 切片型別方法（如 []t.method）：以裸名存入 funcSigs。
					funcSigs[fd.Name] = rets
				}
			}
		}
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			fields := make(map[string]string)
			for _, f := range sd.Fields {
				if typeStr := checker.StructFieldTypeString(f); typeStr != "" {
					fields[f.Name] = typeStr
				}
			}
			structFields[sd.Name] = fields
		}
	}
}

// fmtUseRe 與 preloadModuleSignatures 的掃描式逐字一致，確保兩條管道看到
// 同一組 use / # 陳述。
var fmtUseRe = regexp.MustCompile(`(?m)^\s*(?:use|#)\s+([\w/.\-]+)`)

type fmtSigTables struct {
	funcSigs     map[string][]string
	methodSigs   map[string][]string
	structFields map[string]map[string]string
}

type fmtModuleSigs struct {
	funcSigs     map[string][]string
	methodSigs   map[string][]string
	structFields map[string]map[string]string
}

func newFmtModuleSigs() *fmtModuleSigs {
	return &fmtModuleSigs{
		funcSigs:     make(map[string][]string),
		methodSigs:   make(map[string][]string),
		structFields: make(map[string]map[string]string),
	}
}

// FmtSigLoader 為一次 `no fmt` 執行（單檔或整個目錄）提供跨模組簽名配置。
// 它持有一個專用於模組解析的 Transpiler，但**不跑任何 codegen**；重複的
// resolveUse 結果落在 fileCache / moduleCache，目錄批次只載一次。
//
// 所有公開方法都可在 nil receiver 上呼叫（退化成零配置 = 舊單檔行為）。
type FmtSigLoader struct {
	mu sync.Mutex
	t  *Transpiler

	// disabled：非 make 建構的二進位（StdSigKey 為空）沒有內嵌簽名表，
	// CollectStdModuleSignatures 會 os.Exit(1)。此時優雅退化，絕不讓
	// `no fmt` 因為拿不到簽名就整支程式被殺掉。
	disabled bool

	stdFunc      map[string][]string
	stdMethod    map[string][]string
	stdStruct    map[string]map[string]string
	enumVariants map[string][]string

	moduleCache map[string]*fmtModuleSigs
	tablesCache map[string]*fmtSigTables
}

// NewFmtSigLoader 建立載入器。p 可為 nil（單檔不在任何 package 內），
// 此時本地模組路徑解析仍可用 workspaceRoot 的預設值。
func NewFmtSigLoader(p *Package) *FmtSigLoader {
	l := &FmtSigLoader{
		t:           NewTranspiler(p),
		moduleCache: make(map[string]*fmtModuleSigs),
		tablesCache: make(map[string]*fmtSigTables),
	}
	l.t.fileCache = make(map[string]*parser.Program)
	if checker.StdSigKey == "" {
		l.disabled = true
		return l
	}
	stdSigs, stdFields := checker.CollectStdModuleSignatures()
	l.stdFunc = stdSigs
	l.stdMethod = checker.CollectStdMethodSigs()
	l.stdStruct = stdFields
	l.enumVariants = checker.CollectStdEnumVariants()
	// 讓 resolveUse 解析子模組時，子模組內部的 let 推斷也看得到 std 簽名
	// （與 preloadModuleSignatures 步驟 0 掛 t.externFuncSigs 同動機）。
	l.t.externFuncSigs = l.stdFunc
	l.t.externMethodSigs = l.stdMethod
	l.t.externStructFields = l.stdStruct
	return l
}

// ConfigFor 回傳格式化 source 時應注入的解析配置。filePath 非空時會填
// cfg.Filename（影響診斷文字）；`no fmt` 預設傳空字串以保持既有的錯誤
// 輸出逐字不變。回傳的 map 是共用只讀表，呼叫端不應修改。
func (l *FmtSigLoader) ConfigFor(source, filePath string) parser.SemRebuildConfig {
	if l == nil || l.disabled {
		return parser.SemRebuildConfig{}
	}
	tables := l.tablesFor(source)
	name := ""
	if filePath != "" {
		name = filepath.Base(filePath)
	}
	return parser.SemRebuildConfig{
		Filename:     name,
		FuncSigs:     tables.funcSigs,
		MethodSigs:   tables.methodSigs,
		StructFields: tables.structFields,
		EnumVariants: l.enumVariants,
	}
}

// tablesFor 依「排序後的 use 集合」快取合併後的三張表。std 簽名一律由內嵌表
// 全量提供（標準庫隱式可用，不能按是否引用跳過），本地模組簽名後寫入、
// 覆蓋同名鍵，維持 build 的「本地優先」語義。
func (l *FmtSigLoader) tablesFor(source string) *fmtSigTables {
	uses := fmtUseRe.FindAllStringSubmatch(source, -1)
	paths := make([]string, 0, len(uses))
	seen := make(map[string]bool, len(uses))
	for _, m := range uses {
		if len(m) < 2 {
			continue
		}
		p := m[1]
		if p == "std" || strings.HasPrefix(p, "std/") {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	sort.Strings(paths)
	key := strings.Join(paths, "\x00")

	l.mu.Lock()
	defer l.mu.Unlock()
	if cached, ok := l.tablesCache[key]; ok {
		return cached
	}

	tables := &fmtSigTables{
		funcSigs:     make(map[string][]string, len(l.stdFunc)+16),
		methodSigs:   make(map[string][]string, len(l.stdMethod)+16),
		structFields: make(map[string]map[string]string, len(l.stdStruct)+16),
	}
	for k, v := range l.stdFunc {
		tables.funcSigs[k] = v
	}
	for k, v := range l.stdMethod {
		tables.methodSigs[k] = v
	}
	for k, v := range l.stdStruct {
		tables.structFields[k] = v
	}
	for _, p := range paths {
		ms := l.loadModule(p)
		for k, v := range ms.funcSigs {
			tables.funcSigs[k] = v
		}
		for k, v := range ms.methodSigs {
			tables.methodSigs[k] = v
		}
		for k, v := range ms.structFields {
			tables.structFields[k] = v
		}
	}
	l.tablesCache[key] = tables
	return tables
}

// loadModule 載入單一 use 路徑並收集簽名。必須由已持有 l.mu 的 tablesFor 呼叫。
// 解析失敗也快取空結果，避免目錄批次裡對同一個壞路徑反覆做磁碟 I/O。
func (l *FmtSigLoader) loadModule(rawPath string) *fmtModuleSigs {
	empty := newFmtModuleSigs()
	candidates := []string{rawPath}
	if idx := strings.LastIndex(rawPath, "."); idx > 0 {
		candidates = append(candidates, rawPath[:idx])
	}
	for _, usePath := range candidates {
		if cached, ok := l.moduleCache[usePath]; ok {
			return cached
		}
		prog, err := l.t.resolveUse(&parser.UseStatement{Path: usePath})
		if err != nil || prog == nil {
			l.moduleCache[usePath] = empty
			continue
		}
		modShort := usePath
		if idx := strings.LastIndex(modShort, "/"); idx >= 0 {
			modShort = modShort[idx+1:]
		}
		if idx := strings.LastIndex(modShort, "."); idx > 0 {
			modShort = modShort[:idx]
		}
		ms := newFmtModuleSigs()
		collectModuleSignatures(prog, modShort, ms.funcSigs, ms.methodSigs, ms.structFields)
		l.moduleCache[usePath] = ms
		return ms
	}
	return empty
}
