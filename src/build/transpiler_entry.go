package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"

	"github.com/lizongying/nolang/i18n"
)

// parserWarningsByCode 回傳 parser 診斷中指定碼的 WARNING 級 Diagnostic（含位置）。
// parser 只對外暴露格式化後的字串版 WarningsByCode；這裡需要行號/列號以轉成
// LintResult（vet 輸出）或在 stderr 印出，故自行過濾 Diagnostics()。
func parserWarningsByCode(p *parser.Parser, code string) []parser.Diagnostic {
	var out []parser.Diagnostic
	for _, d := range p.Diagnostics() {
		if d.Severity == parser.SeverityWarning && d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// parseFile resolves filePath to its AST. parseFileWrapped is the real body;
// this wrapper exists so the AST-write gate can snapshot every Program at the
// exact moment it enters the compiler (see auditRegister).
func (t *Transpiler) parseFile(filePath string) (*parser.Program, error) {
	prog, err := t.parseFileWrapped(filePath)
	if err == nil {
		t.auditRegister(filePath, prog)
	}
	return prog, err
}

func (t *Transpiler) parseFileWrapped(filePath string) (*parser.Program, error) {
	// 快取命中：同一檔案在單次編譯中可能被 resolveUse 調用多次
	if t.fileCache != nil {
		if cached, ok := t.fileCache[filePath]; ok {
			if os.Getenv("NOLANG_DEBUG_IT") != "" {
				fmt.Fprintf(os.Stderr, "[debug-it] parseFile CACHE HIT: %s\n", filePath)
			}
			return cached, nil
		}
	}
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		fmt.Fprintf(os.Stderr, "[debug-it] parseFile NEW PARSE: %s externFuncSigs=%v externMethodSigs=%v\n", filePath, t.externFuncSigs != nil, t.externMethodSigs != nil)
	}
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	// The parse configuration is expressed ONCE here and consumed by both the
	// fresh parse below and the Tier-1 rebuild, so a new parser input cannot be
	// added to one path only.
	cfg := t.astParseConfig(filePath)
	// Tier 1: on-disk content-addressed AST cache. Skips lex+parse for every
	// unchanged user file across builds. The salt captures the non-source
	// ParseProgram inputs (injected cross-file signatures, the anonymous-fn
	// flag, std key) so a restored AST is always identical to a fresh parse.
	if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
		salt := t.astCacheSalt()
		key := dc.KeyFor(string(source), salt)
		if cached, ok := dc.Get(key, cfg); ok {
			if os.Getenv("NOLANG_DEBUG_IT") != "" {
				fmt.Fprintf(os.Stderr, "[debug-it] parseFile DISK CACHE HIT: %s\n", filePath)
			}
			if t.fileCache != nil {
				t.fileCache[filePath] = cached
			}
			return cached, nil
		}
	}
	l := lexer.NewCached(filePath, string(source))
	p := parser.New(l)
	cfg.Apply(p)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("%s: %s", filePath, parser.FormatDiagnostics(p.Diagnostics(), parser.SeverityError))
	}
	// W_SEMI_EAT warnings are handled by LSP diagnostics; build path no longer
	// emits them to stderr to avoid noise during compilation.
	if t.fileCache != nil {
		t.fileCache[filePath] = prog
	}
	// Tier 1: store the freshly parsed AST so the next build reuses it.
	if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
		dc.Put(dc.KeyFor(string(source), t.astCacheSalt()), prog)
	}
	return prog, nil
}

// astCacheSalt summarizes every ParseProgram input that is not the raw source
// text, so the Tier 1 on-disk AST cache key stays correct. Two builds that
// differ only in these inputs must NOT share a cached AST, because the parser's
// semantic side-table (sem.VarTypes / sem.EnumVariants, consumed during
// lowering) is shaped by them.
func (t *Transpiler) astCacheSalt() string {
	b, _ := json.Marshal(struct {
		FuncSigs     map[string][]string          `json:"f"`
		MethodSigs   map[string][]string          `json:"m"`
		StructFields map[string]map[string]string `json:"s"`
		AllowAnon    bool                         `json:"a"`
		StdKey       string                       `json:"k"`
	}{t.externFuncSigs, t.externMethodSigs, t.externStructFields, t.allowAnonymousFn, checker.StdSigKey})
	return string(b)
}

// astParseConfig returns the source-independent inputs that shape a parse of
// filePath. It is consumed by BOTH the fresh-parse path and the Tier-1 rebuild
// path (`UserASTCache.Get`), which is the whole point: the AST blob carries the
// syntax tree only, so the semantic views have to be regenerated with the same
// configuration that produced the tree. Expressing that configuration once is
// what keeps the two paths from drifting — a new parser input added here reaches
// both, and one added by hand at a single call site reaches only that one.
//
// An empty filePath yields an empty Filename (not filepath.Base("") == "."),
// matching the old `if t.sourcePath != ""` guard in parseEntryProgram.
func (t *Transpiler) astParseConfig(filePath string) parser.SemRebuildConfig {
	name := ""
	if filePath != "" {
		name = filepath.Base(filePath)
	}
	return parser.SemRebuildConfig{
		Filename:             name,
		AllowAnonymousFnType: t.allowAnonymousFn,
		FuncSigs:             t.externFuncSigs,
		MethodSigs:           t.externMethodSigs,
		StructFields:         t.externStructFields,
		EnumVariants:         checker.CollectStdEnumVariants(),
	}
}

// parseEmbeddedProgram 從內嵌 FS 的位元組資料解析 Nolang 程式。
// 與 parseFile 平行，但來源為 embed.FS.ReadFile 的結果而非磁碟檔案。
// 用於 js/ 相容層模組解析（# js/<module>）。
// parseEmbeddedProgram parses embedded std source. Wrapped for the same
// reason as parseFile: auditRegister must see the Program before assembly.
func (t *Transpiler) parseEmbeddedProgram(filename string, data []byte) (*parser.Program, error) {
	prog, err := t.parseEmbeddedProgramWrapped(filename, data)
	if err == nil {
		t.auditRegister("embed:"+filename, prog)
	}
	return prog, err
}

func (t *Transpiler) parseEmbeddedProgramWrapped(filename string, data []byte) (*parser.Program, error) {
	// 快取命中：同一內嵌檔案在單次編譯中可能被 resolveUse 調用多次
	if t.fileCache != nil {
		if cached, ok := t.fileCache[filename]; ok {
			return cached, nil
		}
	}
	l := lexer.NewCached(filename, string(data))
	p := parser.New(l)
	t.astParseConfig(filename).Apply(p)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("%s: %s", filename, parser.FormatDiagnostics(p.Diagnostics(), parser.SeverityError))
	}
	// W_SEMI_EAT warnings are handled by LSP diagnostics; build path no longer
	// emits them to stderr to avoid noise during compilation.
	if t.fileCache != nil {
		t.fileCache[filename] = prog
	}
	return prog, nil
}

// preloadModuleSignatures 掃描源碼中的 use 語句，預載入模組的函數簽名和 struct 欄位型別。
// 這些簽名會注入到 parser 中，使 let 型別推斷能處理跨文件方法調用。
// 也預載入所有已知 std 模組的簽名，因為 transpiler 會自動載入這些模組。
func (t *Transpiler) preloadModuleSignatures(source string) (map[string][]string, map[string][]string, map[string]map[string]string) {
	funcSigs := make(map[string][]string)
	methodSigs := make(map[string][]string)
	structFields := make(map[string]map[string]string)
	loadedPaths := make(map[string]bool) // 避免重複載入同一模組
	// 0. 合併 std 模組簽名快取，並掛到 t.externFuncSigs：
	// 本地子模組（如 # /agent/agent）在下方步驟 1 就會被 parseFile 解析並
	// 寫入 fileCache，若此時 externFuncSigs 尚未設置，子模組內的
	// `resp = http.do-req(req)` 等 let 推斷拿不到任何 std 簽名，resp 無
	// 型別注解 → codegen 端 option inner 推斷錯誤（i64），字段訪問級聯崩潰。
	// 注意：本地模組簽名（步驟 1）寫入同一 map，同名鍵覆蓋 std 簽名，
	// 維持原「本地優先」語義。
	// 第一遍（簽名預收集）必須全量解析所有 std：nolang 的標準庫隱式可用
	// （無需顯式 `use std/...` 即可呼叫 module.fn()），parser 做 let 型別
	// 推斷時需要全部 std 簽名，故無法按「是否引用」跳過（對應審查點 ②）。
	// 真正可省略的 std 工作在第二遍（語義/IR）：見下方自動載入 std 迴圈，
	// 僅載入程式實際引用到的 std 模組體，其餘略過。
	stdSigs, stdFields := checker.CollectStdModuleSignatures()
	stdMethodSigs := checker.CollectStdMethodSigs()
	for name, sigs := range stdSigs {
		funcSigs[name] = sigs
	}
	for name, sigs := range stdMethodSigs {
		methodSigs[name] = sigs
	}
	for name, fields := range stdFields {
		structFields[name] = fields
	}
	t.externFuncSigs = funcSigs
	t.externMethodSigs = methodSigs
	t.externStructFields = structFields
	// collectSignaturesFromProg 從已解析的 Program 中收集函數簽名和 struct 欄位
	collectSignaturesFromProg := func(modProg *parser.Program, modShort string) {
		for _, stmt := range modProg.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				// DeclaredResults 剔除方法定義的隱式 `self` 接收者（見其
				// 註解）：這張表供 let 型別推斷使用（parser/type.go 的
				// len(rets)==1 判定），留著 self 會讓無回傳值的方法被推成
				// 「回傳接收者型別」，有回傳值的方法則因長度 2 查不到型別。
				if decl := parser.DeclaredResults(fd); len(decl) > 0 {
					rets := make([]string, 0, len(decl))
					for _, r := range decl {
						rets = append(rets, r.Type.String())
					}
					if fd.IsMethodDef && len(fd.Name) > 0 && fd.Name[0] != '[' {
						fullKey := modShort + "." + fd.Name
						methodSigs[fullKey] = rets
						// Also register with fd.Name as key (e.g., "str.starts-with")
						// so that type inference can find it via receiverType + "." + dot.Property
						// where receiverType is just the type name (e.g., "str"), not "module.struct".
						if strings.Contains(fd.Name, ".") {
							methodSigs[fd.Name] = rets
						}
					} else if len(fd.Name) > 0 && fd.Name[0] != '[' {
						funcSigs[modShort+"."+fd.Name] = rets
					} else {
						// 陣列/切片型別方法（如 []t.method）：以裸名存入 funcSigs
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
	// 1. 掃描顯式 use/# 語句（非 std 模組，std 模組由快取提供）
	useRe := regexp.MustCompile(`(?m)^\s*(?:use|#)\s+([\w/.\-]+)`)
	matches := useRe.FindAllStringSubmatch(source, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawPath := m[1]
		// std/ 模組簽名一律由 checker.CollectStdModuleSignatures 快取提供
		// （含跨模組同名 struct/函數的 module.name 限定鍵）。此處不得重複
		// 載入：直接解析會以裸名註冊簽名（如 do-req → ?response），在步驟 2
		// 的 !exists 合併中壓過快取的限定簽名，令 parser 推斷出未限定的
		// ?response 注解，codegen 端 mapToLLVMType 對裸名失敗回退 i64，
		// 造成 it 綁定型別錯誤與字段訪問級聯崩潰（status-code.to-str bug）。
		if rawPath == "std" || strings.HasPrefix(rawPath, "std/") {
			continue
		}
		// 嘗試解析模組路徑：先嘗試完整路徑，再嘗試去掉最後 .function 部分
		candidates := []string{rawPath}
		if idx := strings.LastIndex(rawPath, "."); idx > 0 {
			candidates = append(candidates, rawPath[:idx])
		}
		var modProg *parser.Program
		var matchedPath string
		for _, usePath := range candidates {
			if loadedPaths[usePath] {
				modProg = nil
				break
			}
			fakeUse := &parser.UseStatement{Path: usePath}
			prog, err := t.resolveUse(fakeUse)
			if err == nil && prog != nil {
				modProg = prog
				matchedPath = usePath
				loadedPaths[usePath] = true
				break
			}
		}
		if modProg != nil {
			// modShort = matchedPath 的最後一段（如 /src/tail.tail → tail）
			modShort := matchedPath
			if idx := strings.LastIndex(matchedPath, "/"); idx >= 0 {
				modShort = matchedPath[idx+1:]
			}
			if idx := strings.LastIndex(modShort, "."); idx > 0 {
				modShort = modShort[:idx]
			}
			collectSignaturesFromProg(modProg, modShort)
		}
	}
	// std 簽名已於步驟 0 合入（本地模組簽名在步驟 1 直接覆蓋同名鍵）。
	return funcSigs, methodSigs, structFields
}

// isStdLibraryCode 判斷本次編譯目標是否為標準庫代碼——僅標準庫能使用
// ..any 等受限功能。pkg 非空時看套件根目錄；pkg 為 nil（直接編譯單檔）
// 時看源碼檔案路徑。兩者都命中 "std" 路徑成分時視為標準庫。
func (t *Transpiler) isStdLibraryCode() bool {
	if t.pkg != nil && pathHasComponent(t.pkg.RootDir, "std") {
		return true
	}
	if t.sourcePath != "" && pathHasComponent(t.sourcePath, "std") {
		return true
	}
	return false
}

// parseEntryProgram 取得入口程序的 AST，依序嘗試三條路徑：
//
//  1. NOLANG_REUSE_STD_AST=1 時的 std 內容復用（no vet src/std 場景：
//     target 即某個 std 模組，其 Program 已在 PASS1 解析過，內容哈希命中
//     直接復用）。限定標準庫——非 std 文件不會命中任何 std 模組鍵。
//  2. Tier 1 on-disk content-addressed AST cache。salt 涵蓋全部非源碼
//     ParseProgram 輸入（預載入簽名、匿名函式型別旗標、std key），保證
//     還原的 AST 與同輸入下的 fresh parse 一致。
//  3. fresh parse（lexer + parser，注入外部簽名 / std 枚舉變體）。
//
// fresh parse 路徑還會收集主程序的 parser 期 lint 提示（chained-if 與
// `+=` 一族複合賦算廢棄提示，仅主程序、不含自動載入的 std 库），並把
// AST 寫回 Tier 1 快取供下次建置使用。
func (t *Transpiler) parseEntryProgram(source string) (*parser.Program, error) {
	// B（開關 NOLANG_REUSE_STD_AST）：no vet src/std 時，target 即某個 std 模組，
	// 其 Program 已在 CollectStdModuleSignatures PASS1 解析過。若磁盤文件內容與
	// embed 解析內容一致（內容哈希命中），直接復用該 Program，跳過磁盤重新 parse。
	var program *parser.Program
	if os.Getenv("NOLANG_REUSE_STD_AST") == "1" {
		if reused := checker.StdProgramForContent(cache.ContentKey(source)); reused != nil {
			program = reused
		}
	}
	if program == nil {
		// Tier 1: on-disk content-addressed AST cache for the entry file.
		if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
			if cached, ok := dc.Get(dc.KeyFor(source, t.astCacheSalt()), t.astParseConfig(t.sourcePath)); ok {
				if os.Getenv("NOLANG_DEBUG_IT") != "" {
					fmt.Fprintf(os.Stderr, "[debug-it] entry DISK CACHE HIT: %s\n", t.sourcePath)
				}
				program = cached
			}
		}
	}
	if program == nil {
		l := lexer.NewCached(t.sourcePath, source)
		p := parser.New(l)
		// 外部簽名由 preloadModuleSignatures 在本函式呼叫前填好；此處與 Tier-1
		// 重建路徑共用同一份配置（見 astParseConfig）。
		t.astParseConfig(t.sourcePath).Apply(p)
		program = p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil, i18n.Errorf("au6g7ji7", "parser errors: %s", parser.FormatDiagnostics(p.Diagnostics(), parser.SeverityError))
		}
		// 收集主程序中链式 -> 条件的 lint 提示（仅主程序，不含自动载入的 std 库，
		// 避免库代码触发噪音）。buildWithPkg 会在编译成功后打印。
		t.chainedIfHints = p.WarningsByCode(parser.WarnChainedIf)
		// 同理收集 `+=` 一族复合赋算的廢棄提示（parser 期診斷，含位置）。
		t.compoundAssignHints = parserWarningsByCode(p, parser.WarnCompoundAssign)
		// Tier 1: store the freshly parsed entry AST for the next build.
		if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
			dc.Put(dc.KeyFor(source, t.astCacheSalt()), program)
		}
	}
	return program, nil
}

// checkReservedMainName 遞迴走訪所有函式定義（含嵌套在其它函式 / 區塊內的），
// 攔截任何層級命名為 main 的函式（含方法 .main）。
//
// 'main' 是程式入口的保留名。Nolang 的入口由頂層語句自動合成（見
// hir2mir.synthesizeMainForTopLevel），不需要也不允許使用者顯式定義名為
// main 的函式。顯式定義 main 直接報錯。
func checkReservedMainName(program *parser.Program) error {
	var mainErr error
	var walkForMain func(stmts []parser.Statement)
	walkForMain = func(stmts []parser.Statement) {
		for _, stmt := range stmts {
			if mainErr != nil {
				return
			}
			switch s := stmt.(type) {
			case *parser.FunctionDefinition:
				if s.Name == "main" || strings.HasSuffix(s.Name, ".main") {
					mainErr = i18n.Errorf("xgs17cqx", "line %d, column %d: 'main' is a reserved name and cannot be used as a function name; use top-level statements instead (the entry point is synthesized automatically)", s.Token.Line, s.Token.Column)
					return
				}
				if s.Body != nil {
					walkForMain(s.Body.Statements)
				}
			case *parser.BlockStatement:
				walkForMain(s.Statements)
			}
		}
	}
	walkForMain(program.Statements)
	return mainErr
}

// prepareEntryProgram 對入口程序做裝配前的表面準備（就地修改 program）：
//
//  1. 移除標準庫內建樁函式（#{buildin=...}）：其真實實作位於 Go runtime，
//     nolang 函式體僅為聲明，編譯器不處理（不校驗、不 codegen）。幂等，
//     缓存命中與全新解析都執行一次。
//  2. 注入 -ld-KEY=VALUE 全域常量（在 embed 處理之前，使注入變數可被後續 pass 使用）。
//  3. 處理 #{embed=...} 註解：編譯期讀取嵌入文件。
func (t *Transpiler) prepareEntryProgram(program *parser.Program) error {
	stripBuiltinStubs(program)
	if t.ldFlags != nil {
		injectLDFlags(program, t.ldFlags)
	}
	if err := t.processEmbeds(program, t.sourcePath); err != nil {
		return err
	}
	return nil
}

// collectEntryProgramFacts 合併 PASS 1+2+3+4：單次遍歷 program.Statements 同時執行：
//
//	(1) ..any 標準庫限制檢查（非用戶代碼才允許 []any 變參）
//	(2) 構建 varTypes / globalVarTypes（頂層 let/推斷 + 函數參數/返回值 + 函數體收集）
//	(3) 收集 importedModules（UseStatement 的 ShortName + 全部已知 std 模塊預填充）
//	(4) 收集 mainVarNames（頂層 Let + FunctionDefinition 名）
//
// 原本為 4 次獨立遍歷，合併後降至 1 次。
func collectEntryProgramFacts(program *parser.Program, isUserCode bool) (varTypes, globalVarTypes map[string]string, importedModules []string, mainVarNames map[string]bool, err error) {
	varTypes = make(map[string]string)
	globalVarTypes = make(map[string]string)
	mainVarNames = make(map[string]bool)
	// 預填充已知 std 模塊的 ShortName，允許 math.degrees()、base64.encode-std() 等呼叫無需顯式導入
	for short := range checker.KnownStdModules() {
		importedModules = append(importedModules, short)
	}
	for _, stmt := range program.Statements {
		// (1) ..any 標準庫限制檢查
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && isUserCode {
			if fd.IsVariadic {
				for _, p := range fd.Parameters {
					if p.Type.String() == "[]any" {
						return nil, nil, nil, nil, i18n.Errorf("2243l8if", "..any is only allowed in standard library, not in user code (function: %s)", fd.Name)
					}
				}
			}
		}
		// (2) 構建變數類型表
		if ls, ok := stmt.(*parser.LetStatement); ok {
			if ls.Type != nil {
				varTypes[ls.Name.Value] = ls.Type.String()
				globalVarTypes[ls.Name.Value] = ls.Type.String()
			} else if ls.Value != nil {
				if t := inferTypeFromExpr(ls.Value); t != "" {
					varTypes[ls.Name.Value] = t
					globalVarTypes[ls.Name.Value] = t
				}
			}
			// (4) 收集主程序變量名
			if ls.Name != nil {
				mainVarNames[ls.Name.Value] = true
			}
		}
		// (2) 函數參數/返回值類型收集
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			for _, p := range fd.Parameters {
				if p.Type != nil {
					varTypes[p.Name] = p.Type.String()
				}
			}
			for _, r := range fd.Results {
				if r.Type != nil {
					varTypes[r.Name] = r.Type.String()
				}
			}
			collectVarTypesFromBody(fd.Body, varTypes, program.Sem)
			// (4) 收集主程序函數名
			mainVarNames[fd.Name] = true
		}
		// (3) 收集導入的模塊路徑
		if use, ok := stmt.(*parser.UseStatement); ok {
			importedModules = append(importedModules, checker.ModuleShortName(use.Path))
		}
	}
	return varTypes, globalVarTypes, importedModules, mainVarNames, nil
}
