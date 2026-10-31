package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/build/astaudit"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/mir"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

type Transpiler struct {
	pkg              *Package // 當前套件（用於路徑解析）
	sourcePath       string   // 當前編譯的源碼檔案路徑（用於 std 庫檢測）
	allowAnonymousFn bool     // 是否允許匿名函式型別參數（來自 package.noml）
	// optionInlineThreshold 是 `?T` 載荷內聯的字節閾值（0 = 用 MIR 預設值 24）。
	// 來源依序為 NOLANG_OPTION_INLINE_THRESHOLD 環境變數、package.noml 的
	// compiler.option-inline-threshold。它決定 MIR 的
	// `%option = { i64 tag, [N x i64] slot }` 裡 slot 的寬度：載荷 sizeof 不
	// 大於它則內聯，大於它則堆裝箱、slot[0] 存指針。
	optionInlineThreshold int
	vetMode               bool                 // vet 模式：只做語法+型別檢查，跳過 LLVM IR 生成
	vetStrict             bool                 // vet 模式下是否將 warning/hint 升級為 error
	// jsBackend 標記本次前端組裝服務於 JS 後端（--js 或 package.noml 的
	// compiler.emit = "js"）。LLVM 無法連結 JS 模組，故 `# npm/<spec>` 這類
	// ESM 導入在 JS 後端下**不解析為 .no 原始碼**，而是與 js_backend.go 同口徑
	// 原樣保留 use 行：JS 生成器據此發射 import 頭，checker 據此登記導入名。
	// 沒有這個標記時 `# npm/...` 走 resolveModule 的硬錯分支（見
	// transpiler_resolve.go），`no vet example/esm-imports` 因此誤報 ERROR。
	jsBackend bool
	vetLints              []checker.LintResult // vet 模式下收集的 lint 結果
	// chainedIfHints 收集主程序（非 std 库）中链式 -> 条件的 lint 提示，
	// 由 buildWithPkg 在编译成功后打印到 stderr，提醒用户改用合并条件或 { } 块。
	chainedIfHints []string
	// compoundAssignHints 收集主程序中 `+=` / `-=` / `*=` / `/=` / `%=` 的廢棄提示。
	// 這五個運算符在 parser 就降糖成 `a ?= a + 1`（UnwrapAssignStatement），
	// lowering 之後 surface AST 消失，checker 的 deprecated lint 不再看得到，
	// 故 CLI 管線靠這條 parser warning 補上（LSP 實時診斷走 surface AST，
	// 由 checker 負責，兩邊不會重複）。
	compoundAssignHints []parser.Diagnostic
	// externFuncSigs/externStructFields: 預載入的跨文件函數簽名和 struct 欄位型別，
	// 注入到所有 parser 實例中以支援 let 型別推斷
	externFuncSigs     map[string][]string
	externMethodSigs   map[string][]string
	externStructFields map[string]map[string]string
	// targetGoos/targetGoarch: 編譯目標平台，用於平台變體過濾。
	// 空字串表示 fallback 到 runtime.GOOS/GOARCH（編譯主機平台）。
	targetGoos    string
	targetGoarch  string
	noBoundsCheck bool // skip bounds checks in generated code (unsafe mode)
	// verbose 對應 build/run 的 -v：非 verbose 時後端走單進程 clang -x ir，
	// 且跳過 MIR IR 的 opt 驗證門禁（verifyMIRIRViaOpt）以做到嚴格 1 spawn；
	// verbose 時才保留 opt 驗證 + opt/llc 中間檔。NOLANG_MIR_PREFLIGHT=full
	// 也強制跑驗證門禁（保留該除錯開關的語意）。
	verbose bool
	// ldFlags: -ld-KEY=VALUE pairs injected as compile-time global constants.
	// Set via SetLDFlags before Compile; the synthetic LetStatements are
	// prepended to the parsed program in CompileTarget.
	ldFlags map[string]string
	// fileCache: per-Transpiler AST 解析快取，消除單次 CompileTarget 内的重复解析。
	// 同一 std 模組檔案在一次編譯中最多被解析 4 次（preload/checker.ValidateFuncArgs/merge/auto-load），
	// 快取後降至 1 次。安全性：preload 和 checker.ValidateFuncArgs 只讀不修改 AST，
	// merge 步驟的 prefixModuleStatements/alias 改名是冪等的（已帶前綴的名稱會被跳過）。
	fileCache map[string]*parser.Program
	// astAuditDelta / astAuditByRoot: S5 門禁的觀測結果。設
	// NOLANG_AUDIT_AST_WRITES=1 時，每個進入編譯器的 Program（新解析或快取
	// 還原）都會在那一刻拍一份值快照（auditRegister），CompileTarget 返回時
	// 再拍一次，差集即「ParseProgram 之後被寫入的 AST 節點欄位」——嚴格範圍
	// 不變量（docs/design/nolang-parser-lowering-invariants.md §1.4）要求它
	// 為空。涵蓋每個 root 而不只是 entry：模組合併寫的是模組自己的節點，
	// 只看 entry 程式會把 §13.5 清单裡 transpiler_stdload.go 的寫點全漏掉。
	// 未設環境變數時完全不拍，成本為零。
	astAuditDelta  astaudit.Delta
	astAuditByRoot map[string]astaudit.Delta
	astAuditRoots  []astAuditRoot
	astAuditSeen   map[*parser.Program]bool
}

// astAuditRoot pairs a compiled Program with the pristine value snapshot taken
// the moment it entered the compiler.
type astAuditRoot struct {
	label    string
	prog     *parser.Program
	snapshot *astaudit.Snapshot
}

// ASTAuditDelta returns the merged AST-write audit of the most recent Compile.
// Only populated when NOLANG_AUDIT_AST_WRITES was set; otherwise zero-valued.
func (t *Transpiler) ASTAuditDelta() astaudit.Delta { return t.astAuditDelta }

// ASTAuditByRoot returns the AST-write audit split per compiled Program root,
// keyed by the label passed to auditRegister (a file path).
func (t *Transpiler) ASTAuditByRoot() map[string]astaudit.Delta { return t.astAuditByRoot }

// astAuditEnabled reports whether the AST-write gate should run.
func astAuditEnabled() bool { return os.Getenv("NOLANG_AUDIT_AST_WRITES") != "" }

// auditRegister records prog's pristine snapshot. It must be called at every
// point a Program enters the compiler — fresh parse or cache restore — i.e.
// before any assembly pass can touch it. Idempotent per pointer.
func (t *Transpiler) auditRegister(label string, prog *parser.Program) {
	if prog == nil || !astAuditEnabled() {
		return
	}
	if t.astAuditSeen == nil {
		t.astAuditSeen = make(map[*parser.Program]bool)
	}
	if t.astAuditSeen[prog] {
		return
	}
	t.astAuditSeen[prog] = true
	t.astAuditRoots = append(t.astAuditRoots, astAuditRoot{
		label: label, prog: prog, snapshot: astaudit.Capture(prog),
	})
}

// collectASTAudit diffs every registered root against its pristine snapshot.
// Called via defer at the end of CompileTarget so that every exit path
// (including the errors returned mid-assembly) is covered.
func (t *Transpiler) collectASTAudit() {
	if !astAuditEnabled() {
		return
	}
	t.astAuditByRoot = make(map[string]astaudit.Delta, len(t.astAuditRoots))
	for _, r := range t.astAuditRoots {
		d := astaudit.Diff(r.snapshot, astaudit.Capture(r.prog))
		// A file may legitimately be reachable through more than one label
		// (aliased use); fold those together rather than dropping one.
		if prev, ok := t.astAuditByRoot[r.label]; ok {
			prev.Merge(d)
			t.astAuditByRoot[r.label] = prev
			continue
		}
		t.astAuditByRoot[r.label] = d
		t.astAuditDelta.Merge(d)
	}
	// The report is opt-in separately: CI turns the gate on for every build and
	// does not want ~50k lines of diff per file on stderr.
	if os.Getenv("NOLANG_AUDIT_AST_WRITES_VERBOSE") == "" {
		return
	}
	for label, d := range t.astAuditByRoot {
		if s := d.Summary(); s != "" {
			fmt.Fprintf(os.Stderr, "[ast-audit] %s: %d field write(s), %d slice delta(s), %d added / %d removed node(s)\n%s",
				label, len(d.Writes), len(d.Slices), len(d.AddedNodes), len(d.RemovedNodes), s)
		} else {
			fmt.Fprintf(os.Stderr, "[ast-audit] %s: clean (no AST writes after ParseProgram)\n", label)
		}
	}
}

func NewTranspiler(pkg *Package) *Transpiler {
	t := &Transpiler{
		pkg: pkg,
	}
	if pkg != nil {
		t.allowAnonymousFn = pkg.Compiler.AnonymousFnType
		t.optionInlineThreshold = pkg.Compiler.OptionInlineThreshold
	}
	// 環境變數覆寫 package.noml，方便不改專案檔就能做 A/B。
	if v := os.Getenv("NOLANG_OPTION_INLINE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			t.optionInlineThreshold = n
		} else {
			fmt.Fprintf(os.Stderr, "warning: NOLANG_OPTION_INLINE_THRESHOLD=%q is not an integer, ignored\n", v)
		}
	}
	return t
}

// SetTargetPlatform sets the target (GOOS, GOARCH) for platform-variant filtering
// during code generation. Empty strings fall back to the host runtime platform.
// This is propagated to the underlying LLVM generator before Generate is called,
// and to the MIR backend (mir.SetTargetPlatform) so `-target` drives BOTH the
// #{mac-*}/#{linux-*} annotation filter (src/mir/platform.go) and the
// target-specific C symbols in the built-in shims (errno, stdin, struct stat
// layout, ...). Without the MIR side, a darwin binary built on a Linux host
// linked against glibc symbols (___errno_location, _stdin) and failed to link.
func (t *Transpiler) SetTargetPlatform(goos, goarch string) {
	t.targetGoos = goos
	t.targetGoarch = goarch
	mir.SetTargetPlatform(goos, goarch)
}

// SetNoBoundsCheck configures whether bounds checks are skipped in generated code.
// When true (unsafe mode), array/slice/string indexing does not emit bounds checks.
// SetOptionInlineThreshold 以命令列 `--option-inline-threshold=N` 覆寫閾值。
// 優先序：命令列 > NOLANG_OPTION_INLINE_THRESHOLD > package.noml；0 表示「未指定」，
// 保留 NewTranspiler 已經解析好的值。
func (t *Transpiler) SetOptionInlineThreshold(n int) {
	if n > 0 {
		t.optionInlineThreshold = n
	}
}

func (t *Transpiler) SetNoBoundsCheck(skip bool) {
	t.noBoundsCheck = skip
}

// SetVerbose mirrors the build/run -v flag into the transpiler so the MIR IR
// verification gate (verifyMIRIRViaOpt) only runs when the user opts into
// verbose output; non-verbose builds keep the backend at a strict single
// clang -x ir spawn.
func (t *Transpiler) SetVerbose(v bool) {
	t.verbose = v
}

// SetLDFlags sets the -ld-KEY=VALUE pairs to inject as compile-time global constants.
// Must be called before Compile/CompileTarget. The synthetic LetStatements are
// prepended to the parsed program so they participate in all subsequent passes
// (type inference, validation, module merge, codegen).
func (t *Transpiler) SetLDFlags(ld map[string]string) {
	t.ldFlags = ld
}

// SetVetMode configures the transpiler to skip LLVM IR generation.
// When true, Compile/CompileTarget performs full front-end validation
// (parse, type-check, module merge, monomorphization) but returns before
// the expensive LLVM codegen step. Used by `no vet`.
func (t *Transpiler) SetVetMode(vet bool) {
	t.vetMode = vet
}

// SetVetStrict 在 vet 模式下啟用嚴格模式：將所有 warning/hint 升級為 error。
// 必須在 SetVetMode(true) 之後、Compile 之前調用。
func (t *Transpiler) SetVetStrict(strict bool) {
	t.vetStrict = strict
}

// SetJSBackend 標記本次組裝服務於 JS 後端（emit = "js"）。啟用後 `# npm/...`
// 導入不再嘗試解析成 .no 原始碼，改為原樣保留 use 行（與 js_backend.go 的
// resolveAndMergeJSModules 同口徑）。必須在 Compile 之前調用。
func (t *Transpiler) SetJSBackend(v bool) {
	t.jsBackend = v
}

// VetLints 返回 vet 模式下收集的 lint 結果。
// 僅在 vetMode=true 且 Compile 已執行後有效。
func (t *Transpiler) VetLints() []checker.LintResult {
	return t.vetLints
}

// ChainedIfHints 返回主程序中链式 -> 条件的 lint 提示（编译成功后有效）。
// 仅包含用户主程序（不含自动载入的 std 库），由 build 命令打印到 stderr。
func (t *Transpiler) ChainedIfHints() []string {
	return t.chainedIfHints
}

// CompoundAssignHints 返回主程序中「複合賦算運算符已廢棄」的解析期警告
// （parser 診斷，含位置）。vet 模式會把它們轉成 LintResult 輸出；
// build 模式由 buildWithPkg 打印到 stderr。
func (t *Transpiler) CompoundAssignHints() []parser.Diagnostic {
	return t.compoundAssignHints
}

type Target int

const (
	TargetUnknown Target = iota
	TargetLLVM
)

// LinkLibs 收集本次編譯「實際載入」的每個源檔所屬套件在 package.noml 中宣告的
// compiler.link-libs，去重後回傳（套件根排序，保證結果穩定）。
//
// 為什麼需要：link-libs 原本只從「被建置的套件」（opts 的 pkg）取得。但
//
//	no run tests/test-ffi-sqlite.no
//
// 這類直接編譯單一 .no 檔的入口 pkg == nil，而該檔以 `# <path>` 直接引用驅動
// 源檔（example/sqlite-driver/src/sqlite.no）。驅動的 #{c} 外部函式需要
// -lsqlite3，卻因為只有驅動自己的 package.noml 宣告過這件事而拿不到連結參數，
// 連結期報：
//
//	"_sqlite3_open", referenced from: _sqlite.open in ...
//
// 這裡沿著實際載入的源檔回溯其套件根，把鏈路上每個套件的 link-libs 一併帶上。
func (t *Transpiler) LinkLibs() []string {
	if len(t.fileCache) == 0 {
		return nil
	}
	seenRoot := make(map[string]bool)
	roots := make([]string, 0, len(t.fileCache))
	for path := range t.fileCache {
		root := findPackageRootFromFile(path)
		if root == "" || seenRoot[root] {
			continue
		}
		seenRoot[root] = true
		roots = append(roots, root)
	}
	sort.Strings(roots)
	seenLib := make(map[string]bool)
	var out []string
	for _, root := range roots {
		p, err := pkg.LoadPackage(root)
		if err != nil || p == nil {
			continue
		}
		for _, lib := range p.Compiler.LinkLibs {
			lib = strings.TrimSpace(lib)
			if lib == "" || seenLib[lib] {
				continue
			}
			seenLib[lib] = true
			out = append(out, lib)
		}
	}
	return out
}

func (t *Transpiler) Compile(source string) (string, error) {
	// 初始化 per-Transpiler AST 解析快取，消除單次編譯內的重复解析。
	// 同一 std 模組在 preloadModuleSignatures、checker.ValidateFuncArgs、merge 步驟中
	// 會被重複載入，快取後僅解析一次。
	t.fileCache = make(map[string]*parser.Program)
	// 保留全域模組解析快取（parseProgramFileCache + tokenCache），
	// 使 no test / no build workspace 等多文件場景可跨文件復用。
	// 快取在命令級別清空（BuildFile / BuildWorkspace / VetFile 入口），
	// 而非每次 Compile 調用都清空。
	return t.CompileTarget(source, TargetLLVM)
}

// CompileTarget 編譯單一 Nolang 源文件，是整個前端 + 裝配 + codegen 的編排器
// （orchestrator）。階段依序：
//
//	preload → parse → 入口驗證 → 裝配（merge/stdload/prefix/append/monomorph/
//	second-stage）→ 合併後驗證 → vet mode | codegen
//
// 裝配階段的跨階段狀態由 compileAssembly 承載（transpiler_merge.go），各階段
// 實作落在 transpiler_entry/validate/merge/stdload/monomorph/codegen.go 的階段
// 方法上；本函式只負責編排與錯誤傳播。
func (t *Transpiler) CompileTarget(source string, _ Target) (string, error) {
	// 預載入跨文件模組簽名，供 parser 型別推斷使用
	externFuncSigs, externMethodSigs, externStructFields := t.preloadModuleSignatures(source)
	// 存儲到 Transpiler 中，使 parseFile（用於解析自動載入的模組）也能注入簽名
	t.externFuncSigs = externFuncSigs
	t.externMethodSigs = externMethodSigs
	t.externStructFields = externStructFields

	// 入口程序 AST：std 內容復用 / Tier-1 磁盤快取 / fresh parse 三路徑
	program, err := t.parseEntryProgram(source)
	if err != nil {
		return "", err
	}
	// AST 寫入稽核（S5 門禁，設計文檔 §1.4）：在 program 進入編譯器的第一時間
	// 拍下快照。快照的 root 是**解析輸出本體**——S5 stage 6 起裝配只碰工作
	// 副本（newCompileAssembly 內 CloneProgram 重綁定），此 root 必須保持乾淨，
	// 門禁釘住零寫入。涵蓋每個 root 而不只是 entry：模組合併寫的是模組自己的
	// 節點，只看 entry 程式會把 §13.5 清单裡 transpiler_stdload.go 的寫點全漏掉。
	// 未設環境變數時完全不拍，成本為零。
	t.auditRegister(t.sourcePath, program)
	defer t.collectASTAudit()
	// 保留字檢查：'main' 是程式入口的保留名。Nolang 的入口由頂層語句自動
	// 合成（見 hir2mir.synthesizeMainForTopLevel），不需要也不允許使用者顯式
	// 定義名為 main 的函式（含方法 .main、嵌套函式）。顯式定義 main 直接報錯。
	if err := checkReservedMainName(program); err != nil {
		return "", err
	}
	// 裝配前的表面準備：移除內建樁 / 注入 -ldFlags / 處理 embeds
	if err := t.prepareEntryProgram(program); err != nil {
		return "", err
	}
	// 入口事實收集（PASS 1-4 合併）：..any 標準庫限制 / varTypes / importedModules /
	// mainVarNames。isUserCode 判定（pkg 根目錄或源碼路徑含 "std" 成分）抽到
	// isStdLibraryCode，此處取反。
	varTypes, globalVarTypes, importedModules, mainVarNames, err := collectEntryProgramFacts(program, !t.isStdLibraryCode())
	if err != nil {
		return "", err
	}
	// 入口驗證（模組合併前）：陣列邊界 / 重複變數 / 型別檢查七驗證器彙總
	if err := validateEntryProgram(program, t.sourcePath, varTypes); err != nil {
		return "", err
	}
	// S5 stage 6：進入裝配——取工作副本，裝配狀態由 compileAssembly 承載
	asm := newCompileAssembly(t, program, varTypes, globalVarTypes, importedModules, mainVarNames)
	// use 模組合併（主程序 + transitive drain）
	if err := asm.mergeModules(); err != nil {
		return "", err
	}
	// 第二遍按需載入 std 模組體（可達閉包，避免為未使用的 std 付出解析成本）
	if err := asm.loadStdModulesOnDemand(); err != nil {
		return "", err
	}
	// 前綴 + 型別參考改寫 + 模組呼叫解析
	asm.prefixMethodsAndTypes()
	// 頂層語句加入 merged（必須在單態化之前）
	asm.appendTopLevelStatements()
	// 單態化迭代收斂迴圈（切片欄位泛型結構體 → 泛型具現化 → 新 std 依賴批次合併）
	if err := asm.runMonomorphizationLoop(); err != nil {
		return "", err
	}
	// 第二階段解析（完整程式上統一解析一次）+ checkUnresolvedModuleCalls
	if err := asm.secondStageResolution(); err != nil {
		return "", err
	}
	// 收斂後的最終單態化（泛型結構體 / 聯合型別）+ 跨模組重載名稱修飾
	asm.finalizeMonomorphization()
	// 合併後驗證（D16 邊界 / loop-scoped / LHS-inferred / arg-count / div-zero）
	if err := validateMergedProgram(asm.merged); err != nil {
		return "", err
	}
	// vet 模式：前端驗證（語法+型別+模組合併+單態化）已全部完成，
	// 跳過 LLVM IR 生成以大幅加速 `no vet`。
	// 在返回前對 merged 執行全部 lint 校驗（命名、未用變數、embed、
	// 接口實現、字串拼接等），使 CI 路徑與編輯器診斷保持一致。
	if t.vetMode {
		// `+=` 一族的廢棄提示：parser 期診斷，轉成 LintResult 讓 `no vet`
		// 以 warning 輸出（位置/文案與 LSP 一致：W_COMPOUND_ASSIGN 碼）。
		for _, d := range t.compoundAssignHints {
			t.vetLints = append(t.vetLints, checker.LintResult{
				Line: d.Pos.Line, Column: d.Pos.Column, File: d.Filename,
				Severity: checker.LintWarning, Source: "nolang-lint",
				Message: d.Message, TraceID: d.Code,
			})
		}
		t.vetLints = append(t.vetLints, checker.RunAllLints(asm.merged, checker.LintOptions{
			SourcePath:       t.sourcePath,
			RootDir:          filepath.Dir(t.sourcePath),
			Strict:           t.vetStrict,
			MainFileVarNames: mainVarNames,
			SkipTypeChecks:   true,
		})...)
		return "", nil
	}
	// codegen：HIR lowering + MIR emit（唯一 codegen 路徑，見 lowerToIR）
	return t.lowerToIR(asm.merged)
}
