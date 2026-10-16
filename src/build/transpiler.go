package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	nolang "github.com/lizongying/nolang"
	"github.com/lizongying/nolang/cache"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/mir"
	"github.com/lizongying/nolang/mmast"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

type Transpiler struct {
	pkg              *Package // 當前套件（用於路徑解析）
	sourcePath       string   // 當前編譯的源碼檔案路徑（用於 std 庫檢測）
	allowAnonymousFn bool     // 是否允許匿名函式型別參數（來自 package.jsonc）
	// optionInlineThreshold 是 `?T` 載荷內聯的字節閾值（0 = 用 MIR 預設值 24）。
	// 來源依序為 NOLANG_OPTION_INLINE_THRESHOLD 環境變數、package.jsonc 的
	// compiler.option-inline-threshold。它決定 MIR 的
	// `%option = { i64 tag, [N x i64] slot }` 裡 slot 的寬度：載荷 sizeof 不
	// 大於它則內聯，大於它則堆裝箱、slot[0] 存指針。
	optionInlineThreshold int
	vetMode               bool                 // vet 模式：只做語法+型別檢查，跳過 LLVM IR 生成
	vetStrict             bool                 // vet 模式下是否將 warning/hint 升級為 error
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
	// fileCache: per-Transpiler AST 解析快取，消除單次 CompileTarget 內的重复解析。
	// 同一 std 模組檔案在一次編譯中最多被解析 4 次（preload/checker.ValidateFuncArgs/merge/auto-load），
	// 快取後降至 1 次。安全性：preload 和 checker.ValidateFuncArgs 只讀不修改 AST，
	// merge 步驟的 prefixModuleStatements/alias 改名是冪等的（已帶前綴的名稱會被跳過）。
	fileCache map[string]*parser.Program
}

func NewTranspiler(pkg *Package) *Transpiler {
	t := &Transpiler{
		pkg: pkg,
	}
	if pkg != nil {
		t.allowAnonymousFn = pkg.Compiler.AnonymousFnType
		t.optionInlineThreshold = pkg.Compiler.OptionInlineThreshold
	}
	// 環境變數覆寫 package.jsonc，方便不改專案檔就能做 A/B。
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
// 優先序：命令列 > NOLANG_OPTION_INLINE_THRESHOLD > package.jsonc；0 表示「未指定」，
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

type Target int

const (
	TargetUnknown Target = iota
	TargetLLVM
)

func (t *Transpiler) parseFile(filePath string) (*parser.Program, error) {
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
	// Tier 1: on-disk content-addressed AST cache. Skips lex+parse for every
	// unchanged user file across builds. The salt captures the non-source
	// ParseProgram inputs (injected cross-file signatures, the anonymous-fn
	// flag, std key) so a restored AST is always identical to a fresh parse.
	if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
		salt := t.astCacheSalt()
		key := dc.KeyFor(string(source), salt)
		if cached, ok := dc.Get(key); ok {
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
	p.AllowAnonymousFnType = t.allowAnonymousFn
	p.Filename = filepath.Base(filePath)
	// 注入預載入的跨文件簽名，支援 let 型別推斷
	if t.externFuncSigs != nil || t.externStructFields != nil {
		p.SetExternSignatures(t.externFuncSigs, t.externMethodSigs, t.externStructFields)
	}
	// 注入跨模組枚舉變體，使 match desugar 能識別枚舉型別
	stdEnumVariants := checker.CollectStdEnumVariants()
	if len(stdEnumVariants) > 0 {
		p.SetExternEnumVariants(stdEnumVariants)
	}
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
		FuncSigs     map[string][]string         `json:"f"`
		MethodSigs   map[string][]string         `json:"m"`
		StructFields map[string]map[string]string `json:"s"`
		AllowAnon    bool                        `json:"a"`
		StdKey       string                      `json:"k"`
	}{t.externFuncSigs, t.externMethodSigs, t.externStructFields, t.allowAnonymousFn, checker.StdSigKey})
	return string(b)
}

// parseEmbeddedProgram 從內嵌 FS 的位元組資料解析 Nolang 程式。
// 與 parseFile 平行，但來源為 embed.FS.ReadFile 的結果而非磁碟檔案。
// 用於 js/ 相容層模組解析（# js/<module>）。
func (t *Transpiler) parseEmbeddedProgram(filename string, data []byte) (*parser.Program, error) {
	// 快取命中：同一內嵌檔案在單次編譯中可能被 resolveUse 調用多次
	if t.fileCache != nil {
		if cached, ok := t.fileCache[filename]; ok {
			return cached, nil
		}
	}
	l := lexer.NewCached(filename, string(data))
	p := parser.New(l)
	p.AllowAnonymousFnType = t.allowAnonymousFn
	p.Filename = filepath.Base(filename)
	if t.externFuncSigs != nil || t.externStructFields != nil {
		p.SetExternSignatures(t.externFuncSigs, t.externMethodSigs, t.externStructFields)
	}
	// 注入跨模組枚舉變體，使 match desugar 能識別枚舉型別
	stdEnumVariants := checker.CollectStdEnumVariants()
	if len(stdEnumVariants) > 0 {
		p.SetExternEnumVariants(stdEnumVariants)
	}
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

// workspaceRoot 回傳當前編譯的工作區根目錄（workspace.jsonc 所在目錄）。
// 優先使用套件宣告的 WorkspaceRoot，否則從 sourcePath 向上查找 workspace.jsonc。
// 這是所有本地導入路徑解析的單一基準，編譯器不再以當前源碼檔目錄作為相對基準。
func (t *Transpiler) workspaceRoot() string {
	if t.pkg != nil {
		if ws := t.pkg.WorkspaceRoot(); ws != "" {
			return ws
		}
		if t.pkg.RootDir != "" {
			return t.pkg.RootDir
		}
	}
	if t.sourcePath != "" {
		if ws, ok := pkg.FindWorkspaceRoot(t.sourcePath); ok {
			return ws
		}
	}
	return ""
}

// resolveModuleFile resolves a module path (e.g. "std/byte", "/example/test1/src/utils")
// to its source file path on disk. For std/ modules it uses GetStdSourceFile;
// for user modules it resolves relative to the workspace root.
// Returns empty string if the path cannot be resolved (best-effort).
func resolveModuleFile(modPath, workspaceRoot string) string {
	if strings.HasPrefix(modPath, "std/") || modPath == "std" {
		relPath := strings.TrimPrefix(modPath, "std/")
		if modPath == "std" {
			relPath = ""
		}
		// Try direct path first
		if relPath != "" {
			if p := GetStdSourceFile(relPath); p != "" {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		// Try lookup table (e.g. "net" → "net/net")
		if full, ok := checker.KnownStdModules()[relPath]; ok {
			if p := GetStdSourceFile(full); p != "" {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		// Disk source unavailable (production embeds std via StdFS, and the
		// gob AST cache carries no on-disk path). Return the *logical* std path
		// so merged std statements still carry a std-recognizable SourceFile.
		// This lets `no vet` attribute and filter std-internal diagnostics
		// correctly instead of leaking them as empty-File lints at unrelated
		// line numbers (the "位置不可靠（报在无关语句 / 函数定义行）" symptom,
		// e.g. a std-internal lint at line 266 mis-attributed to a user module
		// because that module happens to have a function spanning that line).
		if relPath == "" {
			return "std"
		}
		if full, ok := checker.KnownStdModules()[relPath]; ok {
			return "std/" + full
		}
		return "std/" + relPath
	}
	// User module: resolve relative to workspace root
	relPath := strings.TrimPrefix(modPath, "/")
	fullPath := filepath.Join(workspaceRoot, relPath)
	if !strings.HasSuffix(fullPath, ".no") {
		fullPath = fullPath + ".no"
	}
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath
	}
	return ""
}

// resolveASTDependency 嘗試把已匹配的依賴鍵當成「預編 AST 依賴」處理：
// 鍵形如 nogit@github.com/lizongying/notools，發布產物是 <tool>.ast.zip。
//
// 這類依賴沒有原始碼可下載（zip 裡就是 AST），所以不能走
// ResolveDependencyModule → DownloadPackage 那條路；命中時直接回傳
// mmast 解出的 *parser.Program。
//
// 回傳 (prog, true, err) 表示這是 AST 依賴（成功或失敗都由它定奪）；
// (nil, false, nil) 表示不是 AST 依賴，呼叫方續走原本的磁碟/下載解析。
func (t *Transpiler) resolveASTDependency(key, version, importPath string) (*parser.Program, bool, error) {
	if !pkg.IsASTDependencyKey(key) {
		return nil, false, nil
	}
	// 一個 zip = 一個模組。只接受 `use nogit`（工具名）與
	// `use <host/owner/repo>/nogit`（倉庫路徑 + 工具名）兩種寫法；
	// 指向更深的模組路徑時明確報錯，不靜默回退成「找不到檔案」。
	at := strings.Index(key, "@")
	tool, repo := key[:at], key[at+1:]
	rest, hit := subPathAfter(importPath, tool)
	if !hit {
		rest, hit = subPathAfter(importPath, repo)
		if !hit {
			return nil, true, fmt.Errorf(
				"import %q does not match AST dependency %s (use %q or %q)",
				importPath, key, tool, repo+"/"+tool)
		}
	}
	if rest != "" && rest != tool {
		return nil, true, fmt.Errorf(
			"dependency %s ships a single prebuilt module %q; cannot import subpath %q",
			key, tool, importPath)
	}
	prog, err := mmast.EnsureModuleAST(key, version)
	if err != nil {
		return nil, true, err
	}
	return prog, true, nil
}

// subPathAfter 回傳 importPath 去掉前綴 p 之後的剩餘段（已去掉前導 "/"）。
// hit 為 false 表示 importPath 不以 p 開頭（該前綴沒命中）。
func subPathAfter(importPath, p string) (rest string, hit bool) {
	if !strings.HasPrefix(importPath, p) {
		return "", false
	}
	return strings.TrimPrefix(strings.TrimPrefix(importPath, p), "/"), true
}

func (t *Transpiler) resolveUse(use *parser.UseStatement) (*parser.Program, error) {
	// use path.fn → 載入 path.no 並取出 fn 函數
	path := use.Path

	// 源碼導入約束：禁止 "./" 和 "../" 相對跳轉
	// 所有外部代碼引用只能通過 workspace 別名或遠端包標識間接引入
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return nil, fmt.Errorf("import %q: relative paths (./ ../) are not allowed in source code; use workspace alias or remote package identifier instead", path)
	}

	// 本地模塊：/path → 先嘗試依賴匹配，再回退到相對於 workspace 根目錄的路徑解析
	if strings.HasPrefix(path, "/") {
		// 先嘗試作為依賴鍵匹配（如 "/example/test2/utils.greet" 匹配依賴 "/example/test2"）
		if t.pkg != nil && len(t.pkg.Dependencies) > 0 {
			if key, ver, matched := t.pkg.MatchDependency(path); matched {
				// 預編 AST 依賴（nogit@host/owner/repo）不落原始碼，直接從
				// <tool>.ast.zip 取回解碼後的 AST。
				if prog, ok, err := t.resolveASTDependency(key, ver, path); ok {
					return prog, err
				}
				modPath, err := t.pkg.ResolveDependencyModule(path)
				if err != nil {
					return nil, err
				}
				if modPath != "" {
					return t.resolveFile(modPath)
				}
			}
		}
		// 回退：相對於 workspace 根目錄的直接路徑
		relPath := strings.TrimPrefix(path, "/")
		fullPath := pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), relPath)
		if !strings.HasSuffix(fullPath, ".no") {
			fullPath = fullPath + ".no"
		}
		return t.resolveFile(fullPath)
	}
	// std/ 開頭 → 標準庫路徑（預設只從內嵌 StdFS 載入，支援單二進制分發）
	if strings.HasPrefix(path, "std/") || path == "std" {
		// strip "std/" prefix to get module path relative to std/
		relPath := strings.TrimPrefix(path, "std/")
		if path == "std" {
			relPath = ""
		}
		// 0. 開發期覆寫：當 $NOLANG_STD_SRC 設定時，優先從磁碟原始碼載入，
		//    讓對 std/ 模組的原始碼修改（含偵錯插樁）立即生效，免於每次重編譯編譯器。
		//    生產路徑（未設定環境變數）不受影響，仍走內嵌 StdFS。
		if env := os.Getenv(pkg.NOLANG_STD_SRC); env != "" {
			tryDisk := func(diskRel string) (*parser.Program, error) {
				diskPath := pkg.GetStdSourceFile(diskRel)
				if diskPath == "" {
					return nil, nil
				}
				data, err := os.ReadFile(diskPath)
				if err != nil {
					return nil, nil
				}
				return t.parseEmbeddedProgram(diskPath, data)
			}
			if relPath != "" {
				if prog, err := tryDisk(relPath); prog != nil {
					return prog, nil
				} else if err != nil {
					return nil, err
				}
			}
			if full, ok := checker.KnownStdModules()[relPath]; ok {
				if prog, err := tryDisk(full); prog != nil {
					return prog, nil
				} else if err != nil {
					return nil, err
				}
			}
		}
		// 1. 直接路徑：std/<relPath>.no
		if relPath != "" {
			embedPath := "std/" + relPath + ".no"
			// Tier 0: 优先用烤进二进制的 std AST（gzip+gob），省掉 lex+parse。
			if prog, ok := checker.StdASTFor(embedPath); ok {
				return prog, nil
			}
			if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
			}
		}
		// 2. Lookup table: match ShortName to FullPath (e.g. "net" → "net/net")
		if full, ok := checker.KnownStdModules()[relPath]; ok {
			embedPath := "std/" + full + ".no"
			if prog, ok := checker.StdASTFor(embedPath); ok {
				return prog, nil
			}
			if data, err := nolang.StdFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
			}
		}
		// 找不到模組 → 返回語意化錯誤（不再回退到磁碟）
		return nil, fmt.Errorf("std module not found in embedded StdFS: %s", path)
	}
	// js/ 開頭 → JS 相容層路徑（內建第三方包，與 std/ 機制平行）
	// 模組名稱遵循 Nolang 的小寫中劃線風格（如 js/console-log、js/fs-read-file）
	if strings.HasPrefix(path, "js/") || path == "js" {
		relPath := strings.TrimPrefix(path, "js/")
		if path == "js" {
			relPath = ""
		}
		// 1. 嘗試從內嵌的 JsFS 載入（src/js/<module>.no）
		if relPath != "" {
			embedPath := "js/" + relPath + ".no"
			if data, err := nolang.JsFS.ReadFile(embedPath); err == nil {
				return t.parseEmbeddedProgram(embedPath, data)
			}
		}
		// 2. Lookup table: match ShortName to FullPath（與 std/ 的 checker.knownStdModules 機制平行）
		for _, info := range checker.KnownJsModules() {
			if info.FullPath == relPath || info.ShortName == relPath {
				embedPath := "js/" + info.FullPath + ".no"
				if data, err := nolang.JsFS.ReadFile(embedPath); err == nil {
					return t.parseEmbeddedProgram(embedPath, data)
				}
			}
		}
		// 3. fallback: src/js/<module>.no 相對於執行目錄
		if relPath != "" {
			fallback := "src/" + path + ".no"
			if _, err := os.Stat(fallback); err == nil {
				return t.resolveFile(fallback)
			}
		}
		return nil, fmt.Errorf("js compatibility module not found: %s", relPath)
	}
	// 依賴解析：嘗試所有導入路徑匹配 package.jsonc 中宣告的依賴
	// 支援 URL 風格（github.com/...）、路徑風格（/example/pkg、./pkg）、短名稱風格（test2）
	first := strings.SplitN(path, "/", 2)[0]
	isURLStyle := strings.Contains(first, ".")
	if t.pkg != nil && len(t.pkg.Dependencies) > 0 {
		if key, ver, matched := t.pkg.MatchDependency(path); matched {
			// 同上：預編 AST 依賴走 .ast.zip，不下載原始碼。
			if prog, ok, err := t.resolveASTDependency(key, ver, path); ok {
				return prog, err
			}
			modPath, err := t.pkg.ResolveDependencyModule(path)
			if err != nil {
				return nil, err
			}
			if modPath != "" {
				return t.resolveFile(modPath)
			}
		}
	}
	if isURLStyle {
		// URL 風格的導入路徑但未在 dependencies 中宣告
		return nil, fmt.Errorf("dependency not found: %q is not declared in package.jsonc dependencies", path)
	}
	// 非 std 路徑 → 透過 alias 解析（一律相對於工作區根目錄，不再以當前源碼檔目錄為基準）
	modulePath := pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)
	if !strings.HasSuffix(modulePath, ".no") {
		modulePath = modulePath + ".no"
	}
	return t.resolveFile(filepath.Clean(modulePath))
}

// resolveFile parses a .no file and applies lib.no export filtering if present.
func (t *Transpiler) resolveFile(filePath string) (*parser.Program, error) {
	prog, err := t.parseFile(filePath)
	if err != nil {
		return nil, err
	}
	// Apply lib.no export filtering
	pkgRoot := findPackageRootFromFile(filePath)
	if pkgRoot != "" {
		libPath := filepath.Join(pkgRoot, "lib.no")
		if _, err := os.Stat(libPath); err == nil {
			prog = checker.FilterByExports(prog, libPath, filePath)
		}
	}
	return prog, nil
}

// LinkLibs 收集本次編譯「實際載入」的每個源檔所屬套件在 package.jsonc 中宣告的
// compiler.link-libs，去重後回傳（套件根排序，保證結果穩定）。
//
// 為什麼需要：link-libs 原本只從「被建置的套件」（opts 的 pkg）取得。但
//
//	no run tests/test-ffi-sqlite.no
//
// 這類直接編譯單一 .no 檔的入口 pkg == nil，而該檔以 `# <path>` 直接引用驅動
// 源檔（example/sqlite-driver/src/sqlite.no）。驅動的 #{c} 外部函式需要
// -lsqlite3，卻因為只有驅動自己的 package.jsonc 宣告過這件事而拿不到連結參數，
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

// std 模組按需載入（on-demand reachability），不再有 alwaysAutoLoadStd 這種
// 「無條件整模組載入」的概念。是否引入某個 std 模組完全由推斷決定：
//   - 顯式引用：程式（含已合併模組）中的 module.fn() DotExpression 呼叫，
//     以及 use std/... 語句（collectReferencedStdModules 掃描）。
//   - 型別隱含：[]T → vec、[N]T → arr、[K]V → map、txt、regexp 等（同函數內
//     的 walkType 掃描，見 collectReferencedStdModules）。
//   - 內建隱含：print/格式化內建（legacy codegen 路徑會發出 @fmt-int/@out/@err
//     等裸呼叫）隱含需要 fmt/io/str 模組——此為「根據用法推斷」，不是「總是引入」。
//     MIR 的 print builtin 是自包含的（直接發 @print_*，不依賴 fmt 模組），
//     故對 MIR 路徑而言載入 fmt/io/str 只是 LowerHIR 會過濾掉的死代碼，無副作用。
// 泛型實例化動態新增的依賴不在此層處理，而是由 monomorphizeGenerics 之後的
// HIR 內部待處理佇列迭代收斂（見下方 auto-load 區塊末尾的 pending 循環）。

// stdModuleMethodPrefixes lists std module short names whose generic [n]t
// methods are called via variable.method() form (e.g. buf.queue-init()).
// collectReferencedStdModules cannot detect these because the receiver is
// a variable, not a module name. When a method call's property starts with
// one of these prefixes followed by '-', the corresponding std module is
// auto-loaded so its [n]t generic methods can be monomorphized.
var stdModuleMethodPrefixes = []string{"queue", "arr-stack"}

// stdPrimitiveTypeNames lists the scalar / union-alias receiver types whose
// std methods are invoked through VALUES (`x.degrees()`, `i.to-str()`). The
// static reference scan cannot see a module prefix on those calls, so the
// owning module body would never auto-load and MIR codegen dies with
// `unknown callee f64.degrees` in a script that never spells out `math.`.
var stdPrimitiveTypeNames = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true, "i128": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true,
	"f32": true, "f64": true, "str": true, "char": true, "byte": true,
	"bool": true, "int": true, "uint": true, "num": true, "float": true,
}

var (
	stdPrimMethodModsOnce sync.Once
	stdPrimMethodMods     map[string][]string
)

func (t *Transpiler) CompileTarget(source string, _ Target) (string, error) {
	// 預載入跨文件模組簽名，供 parser 型別推斷使用
	externFuncSigs, externMethodSigs, externStructFields := t.preloadModuleSignatures(source)
	// 存儲到 Transpiler 中，使 parseFile（用於解析自動載入的模組）也能注入簽名
	t.externFuncSigs = externFuncSigs
	t.externMethodSigs = externMethodSigs
	t.externStructFields = externStructFields

	// B（開關 NOLANG_REUSE_STD_AST）：no vet src/std 時，target 即某個 std 模組，
	// 其 Program 已在 CollectStdModuleSignatures PASS1 解析過。若磁盤文件內容與
	// embed 解析內容一致（內容哈希命中），直接復用該 Program，跳過磁盤重新 parse。
	// 限定標準庫：非 std 文件內容不會命中任何 std 模組鍵，自動走正常 parse 路徑。
	var program *parser.Program
	if os.Getenv("NOLANG_REUSE_STD_AST") == "1" {
		if reused := checker.StdProgramForContent(cache.ContentKey(source)); reused != nil {
			program = reused
		}
	}
	if program == nil {
		// Tier 1: on-disk content-addressed AST cache for the entry file. The
		// salt captures the non-source ParseProgram inputs (preloaded cross-file
		// signatures, anonym-fntype flag, std key) so a restored AST is identical
		// to a fresh parse under the same inputs.
		if dc, derr := parser.DefaultUserASTCache(); derr == nil && dc != nil {
			if cached, ok := dc.Get(dc.KeyFor(source, t.astCacheSalt())); ok {
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
		p.AllowAnonymousFnType = t.allowAnonymousFn
		if t.sourcePath != "" {
			p.Filename = filepath.Base(t.sourcePath)
		}
		// 注入外部簽名
		if len(externFuncSigs) > 0 || len(externStructFields) > 0 {
			p.SetExternSignatures(externFuncSigs, externMethodSigs, externStructFields)
		}
		// 注入跨模組枚舉變體，使 match desugar 能識別枚舉型別
		stdEnumVariants := checker.CollectStdEnumVariants()
		if len(stdEnumVariants) > 0 {
			p.SetExternEnumVariants(stdEnumVariants)
		}
		program = p.ParseProgram()
		if len(p.Errors()) > 0 {
			return "", fmt.Errorf("parser errors: %s", parser.FormatDiagnostics(p.Diagnostics(), parser.SeverityError))
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
	// 保留字檢查：'main' 是程式入口的保留名。Nolang 的入口由頂層語句自動
	// 合成（見 hir2mir.synthesizeMainForTopLevel），不需要也不允許使用者顯式
	// 定義名為 main 的函式（含方法 .main、嵌套函式）。顯式定義 main 直接報錯。
	// 遞迴走訪所有函式定義（含嵌套在其它函式 / 區塊內的），確保任何層級的
	// main 都被攔截，而不只是頂層。
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
					mainErr = fmt.Errorf("line %d, column %d: 'main' is a reserved name and cannot be used as a function name; use top-level statements instead (the entry point is synthesized automatically)", s.Token.Line, s.Token.Column)
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
	if mainErr != nil {
		return "", mainErr
	}
	// 移除標準庫內建樁函式（#{buildin=...}）：其真實實作位於 Go runtime，
	// nolang 函式體僅為聲明，編譯器不處理（不校驗、不 codegen）。幂等，
	// 缓存命中與全新解析都執行一次。
	stripBuiltinStubs(program)
	// 注入 -ld-KEY=VALUE 全域常量（在 embed 處理之前，使注入變數可被後續 pass 使用）
	if t.ldFlags != nil {
		injectLDFlags(program, t.ldFlags)
	}
	// 處理 #{embed=...} 註解：編譯期讀取嵌入文件
	if err := t.processEmbeds(program, t.sourcePath); err != nil {
		return "", err
	}
	// 驗證：僅標準庫能使用的功能
	isUserCode := true
	if t.pkg != nil && pathHasComponent(t.pkg.RootDir, "std") {
		isUserCode = false
	}
	// 如果 pkg 為 nil，檢查源碼檔案路徑是否為標準庫
	if isUserCode && t.sourcePath != "" && pathHasComponent(t.sourcePath, "std") {
		isUserCode = false
	}
	// 合併 PASS 1+2+3+4：單次遍歷同時執行：
	// (1) ..any 標準庫限制檢查
	// (2) 構建 varTypes / globalVarTypes
	// (3) 收集 importedModules（UseStatement 的 ShortName）
	// (4) 收集 mainVarNames（頂層 Let + FunctionDefinition 名）
	// 原本為 4 次獨立遍歷，合併後降至 1 次。
	varTypes := make(map[string]string)
	globalVarTypes := make(map[string]string)
	var importedModules []string
	mainVarNames := make(map[string]bool)
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
						return "", fmt.Errorf("..any is only allowed in standard library, not in user code (function: %s)", fd.Name)
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
			collectVarTypesFromBody(fd.Body, varTypes)
			// (4) 收集主程序函數名
			mainVarNames[fd.Name] = true
		}
		// (3) 收集導入的模塊路徑
		if use, ok := stmt.(*parser.UseStatement); ok {
			importedModules = append(importedModules, checker.ModuleShortName(use.Path))
		}
	}
	// 編譯期陣列邊界檢查（單次遍歷同時收集陣列、切片、字串大小映射）
	sizeMap := buildSizeMaps(program)
	validateFuncStrReturns = sizeMap.funcStrReturns
	if err := validateArrayBounds(program, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes); err != nil {
		return "", err
	}
	// 編譯期重複變數檢查
	if err := validateDuplicates(program); err != nil {
		return "", err
	}
	// Tag top-level statements with their source path BEFORE type-checking so
	// validation passes (e.g. the .len deprecation check) can exempt builtin
	// implementation files (vec.no / byte.no / str.no) by filename. SourceFile
	// is otherwise only set later (after module merge), which is too late for
	// the validation phase.
	for _, stmt := range program.Statements {
		parser.SetSourceFile(stmt, t.sourcePath)
	}
	// 型別檢查（收集所有錯誤後統一報告，而非遇錯即返）
	// 這樣用戶在 no build 時能一次看到所有型別錯誤，而非逐個修復後才能看到下一個。
	var allValidateErrs []string
	if typeErrs := checker.ValidateTypes(program); len(typeErrs) > 0 {
		for _, e := range typeErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	// 函數呼叫引數型別檢查（包括 newtype 語義：fd 變量不能傳給 i64 參數）
	if funcArgErrs := checker.ValidateFuncArgs(program, filepath.Dir(t.sourcePath)); len(funcArgErrs) > 0 {
		for _, e := range funcArgErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	// 未定義變數檢查（包括 struct 類型名直接賦值欄位，如 s.x = 7 而非 s0.x = 7）
	if undefErrs := checker.ValidateUndefinedVars(program, filepath.Dir(t.sourcePath), t.sourcePath); len(undefErrs) > 0 {
		for _, e := range undefErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	// 關係運算子（< <= > >=）的多字元字串運算元（編譯硬錯誤）：nolang 沒有字串
	// 字典序比較，codegen 的 str 分支只有 @str_eq（相等），`a >= b`（a/b 為 str）
	// 會靜默退化成恆假。單字元字串字面量（'a'）隱式當 char，仍合法。
	if strOrderErrs := checker.ValidateStrOrdering(program); len(strOrderErrs) > 0 {
		for _, e := range strOrderErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	// option 生命週期檢查（D2 分級）。同一趟 ValidateUnhandledOverflow 產出兩種 trace：
	//   - ovfhndld（HINT）：整數運算產生 option 並綁定到變數。統一 option 規則（D6）下
	//     這是常態，只提示「該變數是 ?T，記得 match 或 ?=」——**不是**硬錯誤。
	//   - optunuse（ERROR）：該 option 既沒上拋（?=）也沒解包（match / `_ =`），錯誤被
	//     靜默丟棄 —— 這才是唯一該讓 no build / no run / no test 失敗的診斷。
	// 因此這裡必須逐條依 TraceID 過濾，不能整批當硬錯誤（否則每個 `x = a + b` 都會
	// 讓建置失敗，而它其實是合法且常見的寫法）。
	if leakErrs := checker.ValidateUnhandledOverflow(program, t.sourcePath); len(leakErrs) > 0 {
		for _, e := range leakErrs {
			if e.TraceID != checker.OptUnuseTraceID {
				continue
			}
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	// 未處理的越界索引（D2：降為 HINT，不再阻斷建置）。arr/vec/slice 索引 b[i] 越界時
	// 預設回傳 option<elem>，這是統一規則的常態；「沒解包」由上面的 optunuse 兜底。
	// 診斷仍會經 RunAllLints 出現在 no vet / LSP（灰色提示），故此處不再呼叫
	// checker.ValidateUnhandledIndex。
	//
	// 下標直寫 `a[i] = v`（D9）→ ERROR `idxwrite`，**阻斷建置**。
	// 與上面「讀側只提示」相反：寫側走 emitIndexStore 的原始 GEP+store，
	// **沒有任何邊界檢查**，越界即 raw OOB write，是記憶體安全洞而非風格問題。
	// 必須先用 place-match 解包（多行形式；match 臂以换行分隔）或 `a[i] ?= v`
	// 上拋，故在此逐條過濾 TraceID 後匯入硬錯誤。
	if writeErrs := checker.ValidateIndexWrite(program, t.sourcePath); len(writeErrs) > 0 {
		for _, e := range writeErrs {
			if e.TraceID != checker.IndexWriteTraceID {
				continue
			}
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	// ?T 輸出參數未初始化檢查（case6）
	if uninitErrs := checker.ValidateUninitOutputParams(program); len(uninitErrs) > 0 {
		for _, e := range uninitErrs {
			allValidateErrs = append(allValidateErrs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
	}
	if len(allValidateErrs) > 0 {
		return "", fmt.Errorf("validation errors: %s", strings.Join(allValidateErrs, "; "))
	}
	// 名稱修飾 pass：處理方法重載
	mangleOverloads(program, varTypes)
	// 自動 enter/leave：插入作用域生命週期調用
	injectEnterLeave(program)
	// 處理 use 陳述句：載入模組並合併函數定義和常量
	// 註：importedModules 和 mainVarNames 已在上方合併 PASS 中收集完成
	merged := &parser.Program{Statements: []parser.Statement{}}
	// 合併語義副表：merged 匯集主程序與各模塊的 AST 節點，
	// 各自 side-table 中的節點語義（註解/平台鍵/embed 等）也必須匯集。
	merged.Sem = parser.NewSemanticContext()
	merged.Sem.Merge(program.Sem)
	// 記錄已顯式導入的模組路徑，避免重複載入
	explicitStdModules := make(map[string]bool)
	// moduleConstants 的功能已下沉到 LetStatement.IsModuleConst 欄位。
	// 合併模組時直接在 LetStatement 上標記 IsModuleConst = true，
	// resolveModuleConstants 改為遍歷 program 語句從帶標記的節點收集常量。
	// mergedGlobalConsts tracks uppercase constant names already added to
	// merged from any module (std or user). Prevents duplicate-declaration
	// errors when multiple std modules define the same constant (e.g.
	// FNV-OFFSET/FNV-PRIME in both collection/map.no and
	// collection/static-hashmap.no). Only the first occurrence is kept.
	mergedGlobalConsts := make(map[string]bool)
	// typeOwner 記錄跨模組型別定義的歸屬（bareName → moduleShortName），
	// 用於將導入模組的 struct/interface/enum 等型別定義加上模組前綴
	// （如 result → sql.result），避免與主檔案變數或型別衝突。
	typeOwner := make(map[string]string)
	// stmtOwner 的功能已下沉到 CommentedNode.ModuleOwner 欄位。
	// 語句的來源模組短名直接存儲在 AST 節點上（parser.SetModuleOwner），
	// 供 prefixCollidingFunctions 判定：同名頂層函數衝突時，只有模組側定義
	// 改名為 module.fn，主程序定義永不改名。
	// stmtFileMap 的功能已下沉到 CommentedNode.SourceFile 欄位。
	// 語句的源碼檔案路徑直接存儲在 AST 節點上（parser.SetSourceFile），
	// 供 vet 模式的 RunAllLints 填充 LintResult.File，使 lint 診斷歸因到
	// 正確的源檔而非一律使用輸入檔路徑。
	// loadedUserModules tracks non-std module paths that have already been
	// imported (when use.Alias == ""). Prevents duplicate FunctionDefinition
	// pointers from being appended when the same module is referenced by
	// multiple `# /path/to/module.fn` directives — which would cause
	// mangleOverloads to remove ALL copies (pointer-keyed dedup removes
	// every occurrence of the same pointer).
	loadedUserModules := make(map[string]bool)
	// pendingUses is a worklist of UseStatements from imported modules that
	// need to be processed (transitive imports). When a module is loaded, its
	// UseStatements are added to this worklist so that their target modules
	// are also loaded and merged.
	var pendingUses []*parser.UseStatement
	// processUseAndMerge loads a module from a UseStatement, merges its
	// non-UseStatement statements into merged, and collects its UseStatements
	// into pendingUses for recursive processing.
	processUseAndMerge := func(use *parser.UseStatement) error {
		if use.Alias == "" && loadedUserModules[use.Path] {
			return nil
		}
		modProg, err := t.resolveUse(use)
		if err != nil {
			return fmt.Errorf("loading module %s: %w", use.Path, err)
		}
		// 解析模組的源碼檔案路徑，供 vet lint 歸因使用。
		// std/ 模組 → GetStdSourceFile；用戶模組 → workspace 根目錄解析。
		modFile := resolveModuleFile(use.Path, t.workspaceRoot())
		merged.Sem.Merge(modProg.Sem)
		if use.Alias == "" {
			loadedUserModules[use.Path] = true
		}
		if strings.HasPrefix(use.Path, "std/") || use.Path == "std" {
			explicitStdModules[use.Path] = true
		}
		// 為導入模組的型別定義加上模組前綴（如 result → sql.result）
		useModShort := checker.ModuleShortName(use.Path)
		prefixModuleStatements(modProg.Statements, useModShort, typeOwner)
		// 將模組中的 FunctionDefinition 和 LetStatement（常量）加入 merged
		for _, ms := range modProg.Statements {
			// Collect transitive UseStatements for recursive processing
			if nestedUse, ok := ms.(*parser.UseStatement); ok {
				if nestedUse.Alias == "" && loadedUserModules[nestedUse.Path] {
					continue
				}
				pendingUses = append(pendingUses, nestedUse)
				continue
			}
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				// 🔴 內建樁函式（#{buildin=...}）必須在此跳過，與 stripBuiltinStubs 及
				// std 自動載入路徑（loadStdModuleBodyForce）保持一致。
				//
				// 這條 `# std/xxx` 顯式導入路徑原本漏了這個檢查，後果不是「多一個沒用的
				// 函式」而是**靜默錯誤答案**：空體樁（`f64.sqrt = () (res f64) {}`）被
				// 當成普通函式 codegen 成一個讀未初始化 alloca 的 `define void @f64_sqrt`，
				// 而呼叫點改走這個真實符號，不再走 Go 內建表：
				//
				//   # std/os  →  os.get-env('HOME') 回傳 ''，os.get-pid() 回傳 0
				//   # std/number → f64.sqrt() 回傳堆疊垃圾（連帶 f64.asin/acos/cbrt 全錯）
				//
				// 沒有顯式導入時 std 走自動載入路徑，所以這些程式「看起來」是好的；
				// 全語料只有 8 個檔案寫了 `# std/...`，且沒有一個導入了帶樁的模組，
				// 因此 golden sweep 也照不到。src/std 共有 153 個 #{buildin} 樁，
				// 散在 os/fs/process/net/math/number/vec/fmt/global/time/async/… 15 個模組。
				if fd.BuiltinStub {
					if merged.BuiltinFuncNames == nil {
						merged.BuiltinFuncNames = make(map[string]bool)
					}
					merged.BuiltinFuncNames[fd.Name] = true
					continue
				}
				// 來源檔必須 Deep 標記（與 std 自動載入路徑一致）：只標頂層語句時，
				// 函式體內的語句 SourceFile 留空，lint 端拿不到來源檔就回退成主檔路徑，
				// 而 Line/Column 取自節點本身（模組座標）——報出 `main.no:1098` 這種
				// 主檔根本沒有的行號（實報 tests/ffi-mysql.no:1098:5，該檔僅 126 行，
				// 1098 行在 example/mysql-driver/src/mysql.no）。
				// If alias is specified, only import the specific function under the alias name
				if use.Alias != "" {
					if use.Function != "" && fd.Name == use.Function {
						fd.Name = use.Alias
						merged.Statements = append(merged.Statements, fd)
						parser.SetSourceFileDeep(fd, modFile)
						// alias 導入按用戶指定名，不參與衝突前綴
					}
					// Skip other functions when alias is used
				} else {
					merged.Statements = append(merged.Statements, fd)
					parser.SetModuleOwner(fd, useModShort)
					parser.SetSourceFileDeep(fd, modFile)
				}
			}
			if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				// If alias is specified, only import the specific function under the alias name
				if use.Alias != "" {
					if use.Function != "" && ls.Name.Value == use.Function {
						if _, ok := ls.Value.(*parser.FunctionLiteral); ok {
							ls.Name.Value = use.Alias
						}
						if !mainVarNames[ls.Name.Value] {
							merged.Statements = append(merged.Statements, ls)
							parser.SetSourceFileDeep(ls, modFile)
							if checker.IsConstantExpr(ls.Value) && checker.MatchesTargetPlatform(modProg.Sem.PlatformKeysOf(ls), t.targetGoos, t.targetGoarch) {
								ls.IsModuleConst = true
							}
						}
					}
					// Skip other lets when alias is used
				} else {
					// 如果主程序已有同名變量，跳過以避免衝突
					if !mainVarNames[ls.Name.Value] {
						isConst := checker.IsConstantExpr(ls.Value)
						// 跨模組同名常量去重（與自動載入路徑同理）
						// 例外：帶平台註解的常量不參與去重，由 FilterByPlatform 選取。
						platformKeys := modProg.Sem.PlatformKeysOf(ls)
						hasPlatformAnnotation := len(platformKeys) > 0
						if isConst && mergedGlobalConsts[ls.Name.Value] && !hasPlatformAnnotation {
							continue
						}
						if isConst && !hasPlatformAnnotation {
							mergedGlobalConsts[ls.Name.Value] = true
						}
						merged.Statements = append(merged.Statements, ls)
						parser.SetModuleOwner(ls, useModShort)
						parser.SetSourceFileDeep(ls, modFile)
						if isConst && checker.MatchesTargetPlatform(platformKeys, t.targetGoos, t.targetGoarch) {
							ls.IsModuleConst = true
						}
					}
				}
			}
			if use.Alias == "" {
				if sd, ok := ms.(*parser.StructDefinition); ok {
					merged.Statements = append(merged.Statements, sd)
					parser.SetSourceFile(sd, modFile)
				}
				if id, ok := ms.(*parser.InterfaceDefinition); ok {
					merged.Statements = append(merged.Statements, id)
					parser.SetSourceFile(id, modFile)
				}
				if ta, ok := ms.(*parser.TypeAlias); ok {
					merged.Statements = append(merged.Statements, ta)
					parser.SetSourceFile(ta, modFile)
				}
				if ed, ok := ms.(*parser.EnumDefinition); ok {
					merged.Statements = append(merged.Statements, ed)
					parser.SetSourceFile(ed, modFile)
				}
				if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
					merged.Statements = append(merged.Statements, ted)
					parser.SetSourceFile(ted, modFile)
				}
				// FFI extern 宣告必須隨模組一起合併，否則 codegen 的 externFuncs
				// 會缺少條目，導致 extern 呼叫走 Nolang by-reference 路徑而非 FFI 路徑。
				if es, ok := ms.(*parser.ExternStatement); ok {
					merged.Statements = append(merged.Statements, es)
					parser.SetSourceFile(es, modFile)
				}
			}
		}
		return nil
	}

	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			if err := processUseAndMerge(use); err != nil {
				return "", err
			}
			continue
		}
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			merged.Statements = append(merged.Statements, stmt)
			parser.SetSourceFile(stmt, t.sourcePath)
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			// FFI extern 宣告 — 收集至 merged 供後續 codegen 使用（目前尚未實作）
			merged.Statements = append(merged.Statements, es)
			parser.SetSourceFile(es, t.sourcePath)
		}
	}
	// Process transitive imports: drain the pendingUses worklist.
	// Each imported module may itself import other modules; those must also
	// be loaded and merged to make their function definitions available.
	for len(pendingUses) > 0 {
		use := pendingUses[0]
		pendingUses = pendingUses[1:]
		if err := processUseAndMerge(use); err != nil {
			return "", err
		}
	}
	// 第二遍按需載入 std 模組體（對應審查點 ②）：
	// 第一遍（簽名預收集 CollectStdModuleSignatures）已全量解析所有 std ——
	// 因為 nolang 標準庫隱式可用（無需顯式 use 即可呼叫 module.fn()），parser
	// 做 let 型別推斷需要全部 std 簽名，故第一遍必須全量、無法跳過。
	// 但 codegen 只需程式「實際引用到」的 std 模組。此處掃描 merged（含主程式
	// 與本地 use 模組）中的 module.fn()/module.CONST/use std/... 引用，取 std
	// 模組間的可達閉包，僅載入這些模組體，其餘略過，避免為未使用的 std 付出
	// 解析 + 合併成本。與全量載入相比正確性等價：隱式分發走 builtin，不依賴
	// 此處載入；std 函數僅能經顯式 module.fn() 觸達，閉包已覆蓋。
	loadedStd := make(map[string]bool)
	var stdWorklist []string
	// 掃描 merged（含已載入模組）和原始 program（含頂層語句）中的 std 模組引用。
	// 原始 program 的頂層 LetStatement / ExpressionStatement 此時尚未加入 merged，
	// 但其中可能引用 module.fn()（如 sha1.sha1(data)），必須在此處一併檢測，
	// 否則這些 std 模組不會被自動載入，導致標準庫函數無法在頂層代碼中使用。
	refs := t.collectReferencedStdModules(merged)
	for sp := range t.collectReferencedStdModules(program) {
		refs[sp] = true
	}
	for sp := range refs {
		if !loadedStd[sp] {
			loadedStd[sp] = true
			stdWorklist = append(stdWorklist, sp)
		}
	}
	// 內建隱含的 std 模組：print/格式化內建（legacy codegen 路徑會發出
	// @fmt-int/@out/@err 等裸呼叫）隱含需要 fmt/io/str 模組。這是「根據用法推斷」
	// （只有用到 print 才引入），不再是 alwaysAutoLoadStd 的「總是引入」。
	// MIR 的 print builtin 自包含、不依賴 fmt 模組，故對 MIR 路徑只是被 LowerHIR
	// 過濾掉的死代碼，無副作用。
	// 此外：io 模組的 out/outln/err/errln 內部依賴 fmt/str/byte（同樣發出裸
	// @fmt-int/@out/@err 呼叫），故只要引用 io 就視同 print 家族推斷，一併引入
	// fmt/io/str/byte，與 alwaysAutoLoadStd 時期行為對齊（避免 io.errln 等呼叫
	// 缺 fmt/str/byte 本體而編譯失敗）。
	usesPrint := t.programUsesPrint(merged) || t.programUsesPrint(program)
	usesIO := refs["io"]
	// printInferredStd：標記由 print/io 用法推斷引入的基礎模組。這些模組的
	// 函數體（fmt-str/fmt-int 等）是 MIR 具名格式欄位的 codegen 硬依賴，
	// 即使與頂層變量同名也不能被衝突跳過（見 loadStdModuleBodyForce）。
	printInferredStd := make(map[string]bool)
	if usesPrint || usesIO {
		for _, name := range []string{"fmt", "io", "str", "byte"} {
			printInferredStd[name] = true
			if _, ok := stdModuleLookup()[name]; ok && !loadedStd[name] {
				loadedStd[name] = true
				stdWorklist = append(stdWorklist, name)
			}
		}
	}
	// 決定性載入順序：輕量型別/標量模組（tier 0）必須先於重型模組（tier 1）
	// 載入。legacy codegen 的簽名解析依賴型別模組（str 等）先就位，否則會把
	// str 回傳誤判為 i64（test-tcp-fork.no 回歸）。Go map 迭代順序不確定，故
	// 顯式排序 worklist 以固定順序。
	sort.Slice(stdWorklist, func(i, j int) bool {
		pi, pj := stdLoadTier(stdWorklist[i]), stdLoadTier(stdWorklist[j])
		if pi != pj {
			return pi < pj
		}
		return stdWorklist[i] < stdWorklist[j]
	})
	for len(stdWorklist) > 0 {
		sp := stdWorklist[0]
		stdWorklist = stdWorklist[1:]
		newDeps, err := t.loadStdModuleBodyForce(sp, merged, typeOwner, mainVarNames, mergedGlobalConsts, globalVarTypes, loadedStd, explicitStdModules, printInferredStd)
		if err != nil {
			return "", err
		}
		for _, dep := range newDeps {
			if !loadedStd[dep] {
				loadedStd[dep] = true
				stdWorklist = append(stdWorklist, dep)
			}
		}
	}
	// 第一階段型別參考改寫：將導入模組語句中的裸型別名改寫為 module.type 形式。
	// 必須在 resolveSelfMethodCalls 之前執行，因為 resolveSelfMethodCalls 透過
	// collectStructFields 以 struct 名稱為 key 查找，若型別定義已重命名為
	// module.name 但參考仍為裸名，會無法匹配。
	// 但先要完成跨模組方法名稱重命名：一個方法可能定義在 B 模組但接收者型別
	// 定義在 A 模組（如 bufio.no 的 reader.fill，reader 定義在 io.no）。
	// 此時 typeOwner 已包含所有模組的型別歸屬，可以正確重命名。
	// mainLocalTypes：主程序自行定義的型別裸名。主程序型別與 std 模組同名時
	//（如自定義 tree-set vs std/collection/tree-set），主程序方法不得加模組前綴。
	mainLocalTypes := make(map[string]bool)
	for _, mainStmt := range program.Statements {
		if name := getTypeDefName(mainStmt); name != "" {
			mainLocalTypes[name] = true
		}
	}
	prefixMethodNames(merged.Statements, typeOwner, mainLocalTypes)
	// 型別參考改寫必須區分歸屬：
	//   - 模組語句用完整 typeOwner（模組內的裸名引用要跟隨定義端一起改寫成
	//     module.type，否則模組自身的方法簽名與型別定義對不上）。
	//   - 主程序語句用剔除本地型別後的表：主程序自定義的 struct 與 std 模組
	//     同名時（tests/test-bare-match.no 的 tree-set vs std/collection/tree-set），
	//     主程序的裸名引用必須繼續指向本地定義，否則方法簽名被改成
	//     `%tree-set.tree-set*` 而定義名仍是 `tree-set.test-match`，呼叫端與
	//     定義端錯位，產生 "use of undefined value" 的無效 IR。
	mainLocalOwner := excludeLocalTypes(typeOwner, mainLocalTypes)
	if len(typeOwner) > 0 {
		mainStmtSet := make(map[parser.Statement]bool, len(program.Statements))
		for _, mainStmt := range program.Statements {
			mainStmtSet[mainStmt] = true
		}
		for _, mergedStmt := range merged.Statements {
			if mainStmtSet[mergedStmt] {
				rewriteTypeRefsInStmt(mergedStmt, mainLocalOwner)
			} else {
				rewriteTypeRefsInStmt(mergedStmt, typeOwner)
			}
		}
	}
	checker.DebugCountHashFns("after-prefixMethodNames", merged)
	// 函數衝突前綴：多模組同名頂層函數（connect×4、get×5 …）改名為
	// module.fn 並改寫模組內裸呼叫。必須在 prefixMethodNames 之後執行，
	// 否則 path.join 之類的新名字會被誤認成 struct path 的方法。
	prefixedFns := prefixCollidingFunctions(merged)
	// 掃描合併後的程序，找出被重新賦值的全局變量（Type==nil 的 LetStatement），
	// 從常量標記中移除可變全局變量，避免被常量傳播替換為字面量。
	// 這些可變全局變量在 codegen 中應從 @VAR 載入實際值，而非使用初始常量。
	// 必須在第一次 ResolveModuleConstants 之前執行，否則函數體中的裸名 COUNTER
	// 會被替換為字面量 0，破壞可變全局變量的語意。
	reassignedGlobals := make(map[string]bool)
	for _, stmt := range merged.Statements {
		collectReassignedGlobals(stmt, reassignedGlobals)
	}
	for _, stmt := range merged.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			if reassignedGlobals[ls.Name.Value] {
				ls.IsModuleConst = false
			}
		}
	}
	// 常量傳播：將模組常量替換為字面值，使 module functions 可以直接使用常量
	checker.ResolveModuleConstants(merged)
	checker.DebugCountHashFns("after-resolveModuleConstants", merged)
	// 解析 module.fn() 呼叫：將 DotExpression 重寫為 Identifier
	// 必須在 monomorphizeGenerics 之前執行，以便泛型模組函數也能被正確處理
	checker.ResolveModuleCalls(merged, importedModules, prefixedFns)
	checker.DebugCountHashFns("after-resolveModuleCalls", merged)
	// 解析 self.method() 呼叫：將方法體內的 self.method(args) 重寫為 Type.method(self, args)
	checker.ResolveSelfMethodCalls(merged)
	checker.DebugCountHashFns("after-resolveSelfMethodCalls", merged)
	// 介面具象化：把「宣告型別是介面、且具體實作可靜態唯一確定」的變數換成實作
	// 型別。必須在 resolveMethodCalls（方法呼叫攤平）之前執行，否則攤平會依介面
	// 名生成不存在的介面方法名（sql.db.exec）。
	devirtualizeInterfaceVars(program, merged, varTypes, typeOwner)
	// 非函數定義的陳述句（頂層呼叫）加入 merged
	// 必須在 monomorphizeGenerics 之前添加，否則頂層的方法呼叫（如 a.clone()）
	// 不會被解析與單態化；也必須在 monomorphizeUnions/rewriteUnionCalls 之前添加，
	// 否則頂層呼叫（如 pow(2, 10, r)）不會被重寫為具體版本
	for _, stmt := range program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			continue
		}
		if _, ok := stmt.(*parser.UseStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExternStatement); ok {
			// extern 為宣告，非頂層可執行語句（已於前述步驟收集至 merged）
			continue
		}
		// Convert MultiAssignStatement to old nested-call syntax for codegen
		if mas, ok := stmt.(*parser.MultiAssignStatement); ok {
			// Before converting, register all Identifier targets as declared
			// in the merged program's semantic context. After conversion to
			// nested-call syntax (bar(a, b)), the targets become call arguments
			// and would otherwise be reported as undefined by CollectDefinedVars.
			for _, target := range mas.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					merged.Sem.SetDeclared(ident.Value)
				}
			}
			if innerCall, ok := mas.Value.(*parser.CallExpression); ok {
				// Create: innerCall(outerArgs) with outerArgs being the target expressions
				outerCall := &parser.CallExpression{
					Token:     innerCall.Token,
					Function:  innerCall,
					Arguments: mas.Targets,
				}
				merged.Statements = append(merged.Statements, &parser.ExpressionStatement{Expression: outerCall})
			}
			continue
		}
		merged.Statements = append(merged.Statements, stmt)
	}
	// 切片欄位泛型結構體單態化：由工廠呼叫 `Type.init(concreteBuffer)` 推斷元素
	// 型別，生成具現化結構體 + 方法，標註工廠結果變數型別，並移除模板。
	// 必須在 monomorphizeGenerics 之前執行：實例方法呼叫 `h.push` 需保持為接收者
	// 為變數的 DotExpression，交由 resolveMethodCall 依 varTypes[h]=具現化名自然攤平。
	monomorphizeSliceStructs(merged, globalVarTypes)
	checker.DebugCountHashFns("after-monomorphizeSliceStructs", merged)
	// 使用 globalVarTypes（僅頂層變數）避免其他函數的局部變數型別洩漏到 method resolution
	// 傳入 typeOwner 以便 resolveMethodCall 為跨模組型別補上模組前綴
	// typeOwner 用剔除主程序本地型別的版本：globalVarTypes 只含主程序頂層變數，
	// 其裸名型別若由主程序自行定義，方法呼叫不得被加上模組前綴。
	// 泛型實例化待處理佇列（HIR 側收斂）：monomorphizeGenerics 動態新增的具體
	// 函數體可能帶入先前未載入的 std 模組依賴（例如泛型 T 實例化為 map<K,V>
	// 後，參數型別註解暴露 collection/map 依賴；或實例化後引用 vec/str 內部函數）。
	// 這類依賴不交給 MIR 處理（mir.LowerHIR 是純一次性 lowering，不回碰 HIR），
	// 而是在 HIR（build）側維護待處理佇列：反覆掃描合併後的 merged、批次合併新
	// 引用的 std 模組體、重新執行必要的前綴/解析/單態化步驟，直到不再出現新的
	// std 模組引用（迭代收斂）。
	for {
		monomorphizeGenerics(merged, globalVarTypes, mainLocalOwner)
		checker.DebugCountHashFns("after-monomorphizeGenerics", merged)
		// 過濾：移除尚未具現化的泛型函數定義（只有具體版本才能產生 LLVM IR）
		filtered := make([]parser.Statement, 0, len(merged.Statements))
		for _, stmt := range merged.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if len(fd.GenericParams) > 0 {
					continue // 跳過泛型函數（GenericParams 未被清空說明尚未具現化）
				}
			}
			filtered = append(filtered, stmt)
		}
		merged.Statements = filtered
		checker.DebugCountHashFns("after-filter-generic", merged)
		// 掃描合併後程式，找出泛型實例化新引入的 std 模組引用
		pendingRefs := t.collectReferencedStdModules(merged)
		if t.programUsesPrint(merged) || t.programUsesPrint(program) || pendingRefs["io"] {
			for _, name := range []string{"fmt", "io", "str", "byte"} {
				if _, ok := stdModuleLookup()[name]; ok {
					pendingRefs[name] = true
				}
			}
		}
		var newRefs []string
		for ref := range pendingRefs {
			if !loadedStd[ref] {
				newRefs = append(newRefs, ref)
			}
		}
		if len(newRefs) == 0 {
			break
		}
		// 決定性載入順序（與主 worklist 一致）：輕量型別模組先於重型模組，
		// 避免泛型實例化新引入的依賴因 map 迭代順序不確定而打亂 str 等型別
		// 模組的就位次序，導致 legacy codegen 簽名解析錯誤。
		sort.Slice(newRefs, func(i, j int) bool {
			pi, pj := stdLoadTier(newRefs[i]), stdLoadTier(newRefs[j])
			if pi != pj {
				return pi < pj
			}
			return newRefs[i] < newRefs[j]
		})
		// 批次合併新引用的 std 模組體（含其傳遞閉包）
		before := len(merged.Statements)
		for _, ref := range newRefs {
			loadedStd[ref] = true
			if _, err := t.loadStdModuleBody(ref, merged, typeOwner, mainVarNames, mergedGlobalConsts, globalVarTypes, loadedStd, explicitStdModules); err != nil {
				return "", err
			}
		}
		newStmts := merged.Statements[before:]
		// 僅對「新合併」的陳述句重跑前綴/型別參考重寫 pass。對整個 merged 程式
		// 重跑 prefixMethodNames 並非安全冪等：已處理的函數（如 process.exec 回傳
		// exec-result 的 str 欄位）會被錯誤重命名/重寫，導致 str 回傳的 sret
		// 暫存器型別被誤判為 i64（test-tcp-fork.no 回歸）。只處理新增陳述句可避免
		// 污染既有程式，同時仍讓泛型實例化新引入的 std 模組函數正確接入。
		if len(typeOwner) > 0 && len(newStmts) > 0 {
			localMainSet := make(map[parser.Statement]bool, len(program.Statements))
			for _, mainStmt := range program.Statements {
				localMainSet[mainStmt] = true
			}
			for _, ns := range newStmts {
				if localMainSet[ns] {
					rewriteTypeRefsInStmt(ns, mainLocalOwner)
				} else {
					rewriteTypeRefsInStmt(ns, typeOwner)
				}
			}
			prefixMethodNames(newStmts, typeOwner, mainLocalTypes)
		}
		// 注意：下面的整程式（whole-merged）解析 pass（prefixCollidingFunctions /
		// ResolveModuleConstants / ResolveModuleCalls / ResolveSelfMethodCalls /
		// rewriteTypeRefs / ResolveMethodCalls）刻意「不」在迴圈內每輪重跑。
		// 對整個 merged 程式重跑這些 pass 並非安全冪等：已處理的函數會被錯誤
		// 重命名 / 重寫（如 process.exec 回傳 exec-result 的 str 欄位 sret 暫存器
		// 被誤判為 i64，見 test-tcp-fork.no；以及 static-hashmap.put 的 insert-idx
		// 局部變數被誤併入 first-tomb，見 test-map.no）。正確做法是：迴圈內只對
		// 「新合併」的陳述句做前綴 / 型別參考重寫（上方 newStmts 區塊），待待處理
		// 佇列收斂、所有 std 模組就位後，由下方第二階段（rewriteTypeRefs /
		// ResolveModuleCalls / ResolveMethodCalls）在「完整程式」上統一解析一次。
		// 重新過濾已具現化的泛型定義，避免下一輪 monomorphizeGenerics 重複處理
		filtered2 := make([]parser.Statement, 0, len(merged.Statements))
		for _, stmt := range merged.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if len(fd.GenericParams) > 0 {
					continue
				}
			}
			filtered2 = append(filtered2, stmt)
		}
		merged.Statements = filtered2
	}
	// 第二階段型別參考改寫：主檔案的頂層語句（struct 定義、let 宣告等）
	// 在此時才加入 merged，需要再次改寫以處理主檔案中對導入模組型別的引用。
	// 已改寫過的型別名（含 "."）會被 prefixTypeName 自動跳過，安全無副作用。
	// 使用剔除主程序本地型別後的表：主程序自定義型別與模組同名時，裸名引用
	// 必須保持指向本地定義（與 prefixMethodNames 的本地優先語義一致）。
	// 待處理佇列收斂、所有 std 模組就位後，在「完整程式」上重跑一次名稱修飾
	// （僅此一次，不在迴圈內每輪重跑，避免對已處理函數的非冪等破壞）。
	prefixedFns = prefixCollidingFunctions(merged)
	rewriteTypeRefs(merged.Statements, mainLocalOwner)
	// 解析頂層代碼中的 module.fn() 呼叫
	checker.ResolveModuleCalls(merged, importedModules, prefixedFns)
	// 再次傳播模組常量：頂層語句此時已加入 merged，且 module.CONST 已被
	// ResolveModuleCalls 改寫為裸名（如 math.PI → PI），需要再次將裸名
	// 常量替換為字面值。否則頂層代碼中的模組常量會殘留為變數引用。
	// reassignedGlobals 已在前面處理（清除 IsModuleConst 標記），
	// 可變全局變量不會被常量傳播替換。
	checker.ResolveModuleConstants(merged)
	// 解析 Type.method(args) 靜態方法呼叫：將 bigint.cmp(d, d2) 重寫為
	// bigint.bigint.cmp(d, d2)，與 prefixMethodNames 重命名後的方法定義對齊。
	// 必須在 resolveSelfMethodCalls 之後執行（該 pass 已用正確的 module 前綴
	// 生成 Type.method(self, args)），並在所有頂層代碼加入 merged 之後執行。
	checker.ResolveMethodCalls(merged, typeOwner)
	checker.DebugCountHashFns("after-resolveMethodCalls", merged)
	// 檢查期診斷：主程式中呼叫不存在的 module.fn（如 bigint.from-int）。
	// 以前這類錯誤會落到 LLVM opt 才報 `use of undefined value`，難以定位。
	// 必須在 ResolveModuleCalls + ResolveMethodCalls 之後執行：合法的
	// module.fn / Type.method 呼叫此時都已被改寫，殘留的模組點呼叫即非法。
	if err := checkUnresolvedModuleCalls(merged, importedModules); err != nil {
		return "", fmt.Errorf("check error: %v", err)
	}
	// 泛型結構體單態化：掃描 map[K]V 使用點，自 hashmap-*-tmpl 模板生成具體結構與方法。
	// 必須在 monomorphizeGenerics 之後（避免與 [n]t 泛型衝突）、monomorphizeUnions 之前執行。
	monomorphizeGenericStructs(merged)
	checker.DebugCountHashFns("after-monomorphizeGenericStructs", merged)
	// 聯合型別單態化：對帶 ..T（T 為 union alias）的函數，
	// 為 union 的每個具體型別生成一個函數版本。生成函數的命名
	// 採用 "<原名>__<成員型別>" 的形式；對函數體內對自己的呼叫也
	// 一併替換。
	monomorphizeUnions(merged)
	checker.DebugCountHashFns("after-monomorphizeUnions", merged)
	// 重寫對聯合型別泛型函數的呼叫：將 max(args) 改為 max__i64(args)
	rewriteUnionCalls(merged, varTypes)
	checker.DebugCountHashFns("after-rewriteUnionCalls", merged)
	// 在合併所有 std 模組後再做一次名稱修飾，
	// 處理跨模組的重載衝突（如 bigint.div-mod vs number.div-mod）
	mangleOverloads(merged, nil)
	checker.DebugCountHashFns("after-mangleOverloads", merged)
	// D16: 模組合併後重新執行 validateArrayBounds。
	// 原本僅在主程式合併前執行（line 1397），導入模組內的錯誤（如 out.len = 1
	// 違反 str.len 只讀約束）被靜默吞掉，直到連結期才以 undefined symbol 報錯。
	// 必須在所有轉換 pass 完成後執行，此時 merged 包含全部主程式與模組陳述句。
	mergedVarTypes := buildVarTypes(merged)
	mergedSizeMap := buildSizeMaps(merged)
	validateFuncStrReturns = mergedSizeMap.funcStrReturns
	if err := validateArrayBounds(merged, mergedSizeMap.arraySizes, mergedSizeMap.sliceSizes, mergedSizeMap.stringSizes, mergedVarTypes); err != nil {
		return "", err
	}
	// 編譯期未初始化變數檢查：循環體內聲明的變數在循環外使用
	// 必須在模組合併後執行，才能檢查到導入模組（如 md5.no）中的問題
	if err := validateLoopScopedVars(merged); err != nil {
		return "", err
	}
	// Bug 22: 跨模組 with-len/with-cap/with-cap-len 無類型標註漏檢。
	// 單文件時 ValidateTypes/ValidateFuncArgs 會檢查，但模組合併發生在驗證之後，
	// 導入模組函數體內的 with-len 無類型標註不會被檢查到。
	// 在模組合併後對 merged 程式執行專門的 LHS-inferred builtin 檢查。
	if lhsInfErrs := checker.ValidateLHSInferredBuiltins(merged); len(lhsInfErrs) > 0 {
		var msgs []string
		for _, e := range lhsInfErrs {
			msgs = append(msgs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
		return "", fmt.Errorf("validation errors: %s", strings.Join(msgs, "; "))
	}
	// 問題 8: 模組合併後重新執行函數引數數量檢查。
	// ValidateFuncArgs 在模組合併前（line 1654）執行，此時標準庫函數（如
	// sha1.sha1-hex）的函數定義尚未被合併到 program 中，sigs 中沒有它們的
	// 簽名，因此參數數量不匹配（如 sha1-hex() 缺少參數）不會被檢測到。
	// 在模組合併後對 merged 程式重新執行參數數量檢查，使標準庫函數的
	// 參數數量也能被檢查。只檢查數量不檢查型別，避免標準庫內部複雜型別
	// 推斷（泛型、單態化）產生的誤報。
	if mergedArgCountErrs := checker.ValidateFuncArgCount(merged); len(mergedArgCountErrs) > 0 {
		var msgs []string
		for _, e := range mergedArgCountErrs {
			msgs = append(msgs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
		return "", fmt.Errorf("function argument errors: %s", strings.Join(msgs, "; "))
	}
	// 字面零除數檢查：整數 / 與 % 的除數為字面常數 0 會觸發 LLVM UB，編譯期攔截。
	if divZeroErrs := checker.ValidateDivByZero(merged); len(divZeroErrs) > 0 {
		var msgs []string
		for _, e := range divZeroErrs {
			msgs = append(msgs, fmt.Sprintf("line %d, column %d: %s [%s]", e.Line, e.Column, e.Message, e.TraceID))
		}
		return "", fmt.Errorf("validation errors: %s", strings.Join(msgs, "; "))
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
		t.vetLints = append(t.vetLints, checker.RunAllLints(merged, checker.LintOptions{
			SourcePath:       t.sourcePath,
			RootDir:          filepath.Dir(t.sourcePath),
			Strict:           t.vetStrict,
			MainFileVarNames: mainVarNames,
			SkipTypeChecks:   true,
		})...)
		return "", nil
	}
	// The checked AST is lowered to HIR; the MIR backend consumes the HIR package
	// (restoring inferred types from the side-table). This is the ONLY codegen
	// path. The legacy surface-AST generator (selected with NOLANG_HIR=0) and the
	// legacy HIR-based generator both lived in src/build/llvm/ and were removed
	// once the full-corpus gate was clean (docs/MIR_DESIGN.md §13.3.13).
	//
	// targetGoos/targetGoarch are honoured in two places: the merge pass above
	// (checker.MatchesTargetPlatform at the std-module import sites) and the MIR
	// backend (mir.SetTargetPlatform, set from SetTargetPlatform), which filters
	// MAIN-file #{platform} variants and picks target-specific C symbols. The
	// backend still falls back to the host when the triple did not resolve to a
	// complete (goos, goarch) pair — see src/mir/platform.go.
	var ir string
	{
		if os.Getenv("NOLANG_DEBUG_SELF") != "" {
			f, _ := os.Create("/tmp/merged_dump.txt")
			for i, st := range merged.Statements {
				nm := ""
				blen := -1
				var bfirst string
				switch v := st.(type) {
				case *parser.FunctionDefinition:
					nm = v.Name
					if v.Body != nil {
						blen = len(v.Body.Statements)
						if blen > 0 {
							bfirst = fmt.Sprintf("%T", v.Body.Statements[0])
						}
					}
				case *parser.LetStatement:
					if v.Name != nil {
						nm = v.Name.Value
					}
				}
				fmt.Fprintf(f, "MERGED[%d] %T name=%q bodyLen=%d firstBody=%q\n", i, st, nm, blen, bfirst)
			}
			f.Close()
		}
		hirPkg, idMap := parser.ASTToHIRWithMap(merged)
		parser.PopulateInferredTypes(hirPkg, idMap)
		if os.Getenv("NOLANG_DEBUG_SELF") != "" {
			for _, id := range hirPkg.Top {
				n := hirPkg.Node(id)
				if n == nil {
					continue
				}
				if n.Kind == hir.KLet || n.Kind == hir.KFuncDef {
					fmt.Fprintf(os.Stderr, "[debug-self] pkg.Top[%d] kind=%v name=%q\n", id, n.Kind, hirPkg.Str(n.S))
				}
			}
		}
		// MIR is the only codegen path. The strangler-fig machinery that used to
		// live here is gone (docs/MIR_DESIGN.md §13.3.13): there is no NOLANG_MIR=2
		// fallback to a legacy HIR generator, no NOLANG_MIR=0 legacy backend, and
		// no NOLANG_HIR=0 surface-AST generator left to degrade to. A construct the
		// MIR backend cannot handle now fails the build loudly instead of silently
		// producing output from a second implementation.
		//
		// NOLANG_MIR=1 is retained as a pure DIAGNOSTIC: it dumps the lowered
		// module + memory report to a temp file and then emits MIR exactly as
		// normal. (It used to dump and then hand back to legacy, which no longer
		// exists.) 0 and 2 are rejected rather than silently ignored so an old
		// invocation cannot be mistaken for a working legacy run.
		mirMode := os.Getenv("NOLANG_MIR")
		if mirMode == "0" || mirMode == "2" {
			return "", fmt.Errorf("NOLANG_MIR=%s: the legacy codegen backend was removed and MIR is the only backend; unset NOLANG_MIR (or use NOLANG_MIR=1 for the lowering dump)", mirMode)
		}
		if mirMode == "1" {
			func() {
				defer func() { recover() }()
				mod, rep, ldiags := mir.LowerHIR(hirPkg, checker.CollectStdEnumVariants())
				t.applyOptionInlineThreshold(mod)
				if mod != nil {
					if f, err := os.CreateTemp("", "nolang-mir-*.txt"); err == nil {
						fmt.Fprintf(f, "=== NOLANG_MIR verification for %s ===\n", t.sourcePath)
						fmt.Fprintf(f, "--- lowered MIR ---\n%s\n", mod.String())
						fmt.Fprintf(f, "--- memory analysis ---\n")
						fmt.Fprintf(f, "drops inserted: %d\n", rep.DropsInserted)
						for _, d := range rep.Diagnostics {
							fmt.Fprintf(f, "[mem-diag] %s: %s (func=%s)\n", d.Kind, d.Msg, d.Func)
						}
						for _, d := range ldiags {
							fmt.Fprintf(f, "[lower-gap] %s/%s: %s\n", d.Func, d.Kind, d.Msg)
						}
						f.Close()
					}
				}
			}()
		}
		mout, mreason := t.emitMIR(hirPkg)
		if mout == "" {
			return "", fmt.Errorf("MIR codegen could not handle %s: %s", t.sourcePath, mreason)
		}
		ir = mout
	}
	return ir, nil
}

// verifyMIRIRViaOpt pre-flights MIR-emitted IR through the LLVM toolchain so a
// broken IR is reported with the MIR-specific message ("MIR IR failed LLVM
// verification/assembly") instead of surfacing several seconds later as a
// generic optimizer error from the builder.
//
// MIR is still maturing; some constructs produce IR that only the optimizer's
// verifier rejects — e.g. register type mismatches, undefined callees, or a void
// value used where a result is expected.
//
// DEFAULT = CHEAP GATE. Only `opt -passes=verify` runs (~30 ms). This is exactly
// the check this stage exists for: the IR verifier is the first thing any `opt`
// pipeline runs. The previous default — a full `opt -O3` *plus* `llc` — did not
// just check the IR, it duplicated the builder's entire backend on every single
// build; combined with an IR shape LLVM's SROA cannot handle cheaply, that is
// what turned a 5-line json program into a 62 s compile (§13.3.15). Note the
// duplicated stages cannot even be kept for free: `llc` on *unoptimized* MIR IR
// is far slower than on the optimized IR (>100 s vs ~14 s), so a "verify-only"
// pre-flight that still assembled would be a pessimization.
//
// NOLANG_MIR_PREFLIGHT=full restores the old behaviour (full pipeline + llc) for
// hunting an IR shape that survives verification but breaks a transformation
// pass or the assembler. It changes only WHERE a build fails, never WHETHER it
// fails: the builder runs the identical command over the identical bytes, so an
// IR the full pre-flight rejects is rejected by the builder too.
//
// If `opt` is unavailable we cannot verify and assume the IR is sound (mirroring
// buildLLVMInternal's own opt-unavailable handling, which skips optimization).
func verifyMIRIRViaOpt(ll string) error {
	dir, err := os.MkdirTemp("", "nolang-mir-opt")
	if err != nil {
		return nil // cannot verify; do not break the build
	}
	if os.Getenv("NOLANG_MIR_KEEP_IR") == "" {
		defer os.RemoveAll(dir)
	}
	inPath := filepath.Join(dir, "m.ll")
	// LLVM 17+ 解析器自動升級 typed pointer → opaque `ptr`，無需 toOpaquePointers 重寫層（死代碼，2026-10-09 刪除）。
	if err := os.WriteFile(inPath, []byte(ll), 0644); err != nil {
		return nil
	}
	// Stage 1: IR verification. See the function comment: the default runs only
	// the verifier pass, because the full pipeline here duplicated the builder's
	// opt on every build.
	fullPreflight := os.Getenv("NOLANG_MIR_PREFLIGHT") == "full"
	if _, err := exec.LookPath("opt"); err == nil {
		outPath := filepath.Join(dir, "m_opt.ll")
		args := []string{"-passes=verify", inPath, "-S", "-o", outPath}
		if fullPreflight {
			optLevel := os.Getenv("NOLANG_OPT_LEVEL")
			if optLevel == "" {
				optLevel = "-O2"
			}
			args = []string{optLevel, inPath, "-S", "-o", outPath}
		}
		cmd := exec.Command("opt", args...)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			if os.Getenv("NOLANG_MIR_DUMP_BAD") != "" {
				_ = os.WriteFile("/tmp/mirb/failed.ll", []byte(ll), 0644)
			}
			return fmt.Errorf("MIR IR failed LLVM verification: %v: %s", err, buf.String())
		}
		inPath = outPath
	}
	// Stage 2: assembly. opt can pass while llc still rejects the IR (e.g. an
	// instruction the codegen emitted that the assembler lowers incorrectly).
	// Skipped by default: assembling costs ~14 s on optimized json IR (>100 s on
	// unoptimized IR, see the function comment) and the builder assembles the
	// identical IR immediately after this returns.
	if _, err := exec.LookPath("llc"); err == nil && fullPreflight {
		sPath := filepath.Join(dir, "m.s")
		cmd := exec.Command("llc", "--fp-contract=fast", inPath, "-o", sPath)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("MIR IR failed LLVM assembly: %v: %s", err, buf.String())
		}
	}
	return nil
}

// emitMIR is the codegen path. It lowers HIR -> MIR, analyzes memory, emits LLVM
// IR, and pre-flights it past the LLVM verifier (see verifyMIRIRViaOpt). It
// returns MIR-emitted IR only when every stage succeeds; on any failure it
// returns empty IR plus a reason and the caller rejects the build.
//
// There is deliberately NO fallback to a second backend. Until §13.3.13 this
// function took an `allowFallback` flag that, under NOLANG_MIR=2, degraded to the
// legacy HIR generator on ANY failure (unsupported construct, optimizer
// verification error, or panic). That made MIR unbreakable but also invisible: a
// gap silently produced legacy output, so nothing failed and nothing was
// reported. Failing loudly is the point — see §13.3.13 for the deletion.
//
// The deferred recover() is retained (panics during lowering are still bugs, not
// user errors) but now always propagates: it catches panics that neither
// EmitLLVM's own recovery nor opt verification would.
func (t *Transpiler) emitMIR(hirPkg *hir.Package) (out string, reason string) {
	debug := os.Getenv("NOLANG_MIR_DEBUG") != ""
	defer func() {
		if r := recover(); r != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "[MIR] recovered panic: %v\n", r)
			}
			out = "" // propagate empty; caller rejects the build
			reason = fmt.Sprintf("panic: %v", r)
		}
	}()
	mod, rep, diags := mir.LowerHIR(hirPkg, checker.CollectStdEnumVariants())
	t.applyOptionInlineThreshold(mod)
	if os.Getenv("NOLANG_MIR_DUMP_HIR") != "" {
		fmt.Fprintf(os.Stderr, "[HIR-DUMP]\n%s\n", hirPkg.Dump())
	}
	if mod == nil {
		return "", "lowering produced nil module"
	}
	// Lowering gaps that would produce WRONG-but-exit-0 output (a shape the v1 MIR
	// backend cannot lower and would emit unsubstituted) are hard errors.
	// Shipping silently-wrong output is the one outcome worse than refusing to
	// compile, so these fatal lower diagnostics (kind "interp") stop the build.
	//
	// kind "deprecated" joins them: it is the backstop for a call to a REMOVED
	// builtin (printf / eprintf) that reached lowering. The checker rejects
	// those first (ValidateDeprecatedPrintf) with a better, source-located
	// message, so this only fires if a call site somehow bypassed it — and
	// exactly then it must refuse the build rather than emit a call whose
	// target does not exist.
	if d, ok := firstFatalLowerDiag(diags); ok {
		// Surface the concrete lowering diagnostic (field name / type / shape),
		// not just the generic bucket. A bare "unsupported construct" forces a
		// re-run with debugger every single time — the diag already carries the
		// answer, so print it.
		return "", "unsupported construct in MIR backend: " + d.Kind + ": " + d.Msg
	}
	if os.Getenv("NOLANG_MIR_DUMP_MIR") != "" {
		fmt.Fprintf(os.Stderr, "[MIR-DUMP]\n%s\n", mod.DumpAnnotated())
	}
	// Memory-safety gate: only emit when the analysis is CLEAN. If the analyzer
	// found any unsafe construct (use-after-move, missing/duplicate drop,
	// borrow-escape, unsupported) the codegen would produce IR with a
	// double-free / UAF / leak — exactly the "lots of memory problems" class.
	// Those always surface as rep.Diagnostics, so a non-empty report means "MIR
	// cannot guarantee safety here" and the build is refused rather than emitting
	// a binary that aborts at runtime.
	if rep != nil && rep.HasErrors() {
		if debug {
			fmt.Fprintf(os.Stderr, "[MIR] memory analysis found unsafe constructs: %v\n", rep.Error())
		}
		return "", rep.Error()
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "[MIR] EmitLLVM failed: %v\n", err)
		}
		return "", fmt.Sprintf("EmitLLVM: %v", err)
	}
	if os.Getenv("NOLANG_MIR_DUMP_LL") != "" {
		if f, e := os.CreateTemp("", "nolang-mir-emit-*.ll"); e == nil {
			_ = os.WriteFile(f.Name(), []byte(ll), 0644)
			fmt.Fprintf(os.Stderr, "[MIR] emitted LLVM -> %s\n", f.Name())
		}
	}
	// verify 門禁（opt -passes=verify）僅 verbose（或 NOLANG_MIR_PREFLIGHT=full）
	// 時保留：非 verbose 走單進程 clang -x ir，嚴格 1 spawn，不再 spawn opt。
	// IR 若有錯，clang 自身會在 parse 期執行同一套 LLVM verifier 並報錯
	//（僅訊息不如 MIR 專屬錯誤具體）。
	runVerify := t.verbose || os.Getenv("NOLANG_MIR_PREFLIGHT") == "full"
	if runVerify {
		if verr := verifyMIRIRViaOpt(ll); verr != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "[MIR] opt verification failed: %v\n", verr)
			}
			return "", fmt.Sprintf("opt-verify: %v", verr)
		}
	}
	return ll, ""
}

// firstFatalLowerDiag returns the first diagnostic that represents a construct
// the MIR backend cannot emit correctly, together with true. Benign lower gaps
// (kind "ident"/"dot") are intentionally NOT fatal — they emit a best-effort
// form that works for the v1 subset.
// applyOptionInlineThreshold 把配置的 `?T` 載荷內聯閾值交給 MIR。0（未配置）
// 交給 MIR 自己取預設值 24，讓 MIR 單獨使用時也有正確行為。
func (t *Transpiler) applyOptionInlineThreshold(mod *mir.Module) {
	if mod == nil || t.optionInlineThreshold == 0 {
		return
	}
	mod.OptionInlineThreshold = t.optionInlineThreshold
}

func firstFatalLowerDiag(diags []mir.LowerDiag) (mir.LowerDiag, bool) {
	for _, d := range diags {
		// "interp": a named-format shape the backend would emit unsubstituted.
		// "deprecated": a call to a removed builtin (printf / eprintf) that got
		//   past the checker — the call target does not exist, so the build must
		//   stop instead of emitting it.
		if d.Kind == "interp" || d.Kind == "deprecated" {
			return d, true
		}
	}
	return mir.LowerDiag{}, false
}

// collectReassignedGlobals 掃描語句，找出 Type==nil 的 LetStatement（賦值），
// 記錄被重新賦值的變量名到 reassignedGlobals。
// 這些變量是可變全局變量，不應被常量傳播替換為初始值。
func collectReassignedGlobals(stmt parser.Statement, reassignedGlobals map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Type == nil && s.Name != nil {
			reassignedGlobals[s.Name.Value] = true
		}
		if s.Value != nil {
			collectReassignedGlobalsInExpr(s.Value, reassignedGlobals)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			collectReassignedGlobalsInExpr(s.Expression, reassignedGlobals)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectReassignedGlobals(s.Init, reassignedGlobals)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.BlockStatement:
		for _, bs := range s.Statements {
			collectReassignedGlobals(bs, reassignedGlobals)
		}
	}
}

func collectReassignedGlobalsInExpr(expr parser.Expression, reassignedGlobals map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.FunctionLiteral:
		// 遞迴掃描函數體內的賦值語句
		if e.Body != nil {
			for _, bs := range e.Body.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, bs := range e.Consequence.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
		if e.Alternative != nil {
			for _, bs := range e.Alternative.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.CallExpression:
		for _, arg := range e.Arguments {
			collectReassignedGlobalsInExpr(arg, reassignedGlobals)
		}
	case *parser.InfixExpression:
		collectReassignedGlobalsInExpr(e.Left, reassignedGlobals)
		collectReassignedGlobalsInExpr(e.Right, reassignedGlobals)
	}
}

// unionTemplateInfo 記錄聯合型別模板函數的資訊
type unionTemplateInfo struct {
	origName  string
	unionName string
	members   []string
}

// devirtualizeInterfaceVars 把「宣告型別是介面、而具體實作可靜態唯一確定」的
// 變數具象化（devirtualization）：`ls.Type` 與 `varTypes` 一併換成具體實作型別。
//
// nolang 是單態化編譯器，介面宣告（`db enter, leave { ... }`，見
// src/std/database/sql.no）表達的是「一組方法簽名的約束」，不是執行期型別。
// `d db = sqlite.open(':memory:')` 之後 d 的實際型別就是驅動回傳的 `db-sqlite`。
// 若不具象化，方法攤平時會依介面名生成 `sql.db.exec` —— 一個不存在的函式，
// MIR 端報：
//
//	EmitLLVM: unknown callee sql.db.exec in func main
//
// 而且 `d.handle` 這類具體欄位存取也無從解析（介面本身沒有欄位）。
//
// 判據刻意保守：只有在「該介面在整個編譯單元裡恰好只有一個實作者」時才具象化。
// 多個實作者（例如同時 import sqlite 與 mysql 驅動）時保持原樣 —— 寧可讓下游
// 給出明確診斷，也不要猜一個實作而產生錯誤分派。
// ifaceImplTable 記錄「介面 -> 實作者型別」映射，由 devirtualizeInterfaceVars
// 在每次編譯時寫入 **該次編譯的 merged Program 的語義副表**（SemanticContext.
// IfaceImpls）；resolveMethodCall 在接收者的靜態型別是介面名時靠它回退到
// 唯一實作者 —— 介面本身只有方法簽名、沒有方法體可呼叫。
//
// 為什麼必須是 per-Program 而不能是全域變數：它是「單一編譯單元」的狀態。
// 放在全域時，`no vet <dir>` 的平行處理會讓先編譯的檔案（例如引入 sqlite 驅動
// 的 database-sql.no）把 `db -> [db-sqlite]` 留在表裡，後編譯的 ffi-mysql.no
// 於是把自己的介面呼叫攤平成 `sqlite.db-sqlite.query` —— 該定義在這個程式裡
// 並不存在，最終報成 `'sqlite.db-sqlite.query' is not defined` 假警報。
//
// 為什麼不能只靠 varTypes：合併後 buildVarTypes(merged) 會從 AST 重建一份
// 變數型別表，重建結果與 CompileTarget 早期那張表並非同一個 map，時序上無法
// 保證具象化寫入一定活到攤平那一刻。這張表按「介面名」查詢，與 varTypes 的
// 生命周期解耦。key 同時含完整名（sql.db）與去掉模組前綴的裸名（db），因為
// 不同 pass 拿到的名字形態不一致。

// buildArraySizeMap 構建變數名 → 陣列大小的映射
// 從所有 LetStatement 中收集 ArraySize
// sizeMaps 儲存陣列、切片、字串的大小映射，由 buildSizeMaps 單次遍歷填充。
// 合併三個原本各自獨立遍歷 AST 的函數（buildArraySizeMap/buildSliceSizeMap/buildStringSizeMap），
// 將遍歷次數從 3 次降至 1 次，顯著減少大型程序的校驗開銷。
type sizeMaps struct {
	arraySizes     map[string]int64
	sliceSizes     map[string]int64
	stringSizes    map[string]int64
	funcStrReturns map[string]bool // user-defined functions returning str
}

// validateFuncStrReturns is a package-level variable that holds the
// funcStrReturns map from buildSizeMaps. It is set before validateArrayBounds
// runs so that collectStringSizeMapFromStmt (which is called from the
// FunctionDefinition case of validateStmtArrayBounds with a fresh per-function
// stringSizes map) can access funcStrReturns without a signature change.
var validateFuncStrReturns map[string]bool

// ── 型別檢查 ──────────────────────────────────────────────
// checker.ValidateResult 型別檢查結果

// ValidateEmbedAnnotations 校驗 #{embed=...} 註解的用法是否正確。
// sourcePath 用於解析相對路徑（相對於包根目錄）。

// checker.ValidateTypes 對 Program 進行型別檢查，回傳錯誤列表（包含行號）

// checker.ValidateUnionTypes 收集 type alias（單型別和 union）並對函數的
// 聯合型別/泛型變體做檢查：
//   - 記錄每個 alias 名稱 -> 解析後的具體型別列表
//   - 對帶 ..T 形式的函數，若 T 是 union alias，記錄到 FunctionDefinition.VariadicUnion
//   - 對每個 type alias，遞迴展開成扁平化的 []Type，供 codegen 使用
//
// 不在這裡阻擋編譯；錯誤一律以 checker.ValidateResult 報告（warning 級別）。

// findSingleUnionName 檢查函數的參數與結果型別，找出唯一使用的 union alias。
// 若函數只涉及一個 union alias，則返回該名稱；否則返回空字串。
// 對於 variadic 函數，返回空字串（variadic 由 VariadicUnion 處理）。

// collectUnionNamesFromType 從型別中收集所有 union alias 名稱。

// FlattenUnion 將一個 union alias（或單型別 alias）扁平化為具體型別列表。
// 對於 union：對每個成員遞迴展開（若成員是另一個 union alias 會被展開）。
// 對於單型別 alias：返回 [Type]（長度 1）。
// 對於已知的 builtin（i8/i16/.../f64/bool/byte/char/str）：原樣返回。

// isValidVarName 檢查名稱是否只包含小寫字母（a-z）、中連接符（-）和數字，且不能以數字開頭

// checker.ValidateNaming 檢查所有變數/函數名稱是否符合命名規範（只用小寫和中劃線）

// checker.ValidateAsyncNaming 檢查所有由 'run' 調用的函數名稱是否以 '-async' 結尾

// checkRunAsyncNaming 檢查單個表達式是否為 RunExpression，並驗證其調用的函數名稱

// checker.ValidateUnusedVars detects top-level variables that are defined but never used.

// markReferencesInStatement walks a statement tree, finding Identifier references to top-level vars.

// markReferencesInExpr walks an expression tree, marking Identifiers found in varSet as used.

// CollectDefinedVars performs the first pass: collects all top-level defined
// names (LetStatements, FunctionDefinitions, ExternStatements) from the program.
// Shared by checker.ValidateNaming (to skip global variable reassignments) and
// checker.ValidateUndefinedVars (as the base for its more comprehensive collection).

// checker.ValidateUndefinedVars detects references to variables that are not defined.

// checker.ValidateUninitOutputParams checks that ?T (nullable) output parameters are
// directly assigned in the function body before being read. A ?T output
// parameter that is read (used in an expression) but never assigned via '='
// is flagged as an error — reading an uninitialized nullable value is unsafe
// and almost certainly a bug (case6: ?T 未初始化使用 → 編譯器報錯).
//
// Covered scenarios:
//   - Case 7 (?T 先賦值再用 → 允許): param IS assigned → no error.
//   - Case 8 (?T 空函數體 → 返回 nil): param NOT read → no error.
//   - Case 6 (?T 未初始化使用 → 報錯): param read but NOT assigned → error.

// collectAssignedNames walks statements recursively and collects variable names
// that are directly assigned via '=' (LetStatement.Name or MultiAssignStatement
// Identifier targets). IndexExpression/DotExpression targets are NOT direct
// assignments — they read the base variable to write to a field/element.

// collectAssignedNamesInExpr walks expressions for nested statements (if/else)
// that may contain assignments. Does NOT recurse into nested function literals.

// collectReadNames walks statements recursively and collects variable names
// that are read (used in expressions). Direct assignment targets (LetStatement.Name,
// MultiAssignStatement Identifier targets) are NOT reads. However, IndexExpression.Left
// and DotExpression.Receiver ARE reads — out[i]=val reads 'out' to get the data ptr.

// collectReadNamesInExpr walks an expression tree and collects all variable names
// that are read. Handles all expression types including DotExpression.Receiver,
// IndexExpression.Left, CallExpression args, etc.

// checker.ValidateInterfaceImplementation matches dotted-name function definitions
// (e.g. `i8.gt = ...`) against generic-receiver interface method
// declarations (e.g. `ord { t.gt(b t) (res bool) }`). Emits a warning
// when an implementing type is missing or its method signature does not
// match the interface constraint.

// splitDottedMethodName splits a function name like "i8.gt" or
// "[]ord.ast" or "[?]ord.desc" into (implType, methodName). Returns
// false if the name does not contain a dotted-method form.

// checker.ValidateUseKeyword warns when "use" keyword is used instead of "#".

// checker.ValidateUseAlias warns when 'as' keyword is used for import aliasing and suggests direct alias style.

// checker.ValidateRedundantTypeAnnotation produces hints when a variable's explicit type
// annotation is redundant — the type can be inferred from the value (e.g. `m i64 = 100`).

// checker.ValidateDuplicateVars checks for duplicate variable declarations and returns diagnostics.

// checker.ValidateDependencyImports checks that URL-style import paths (e.g., github.com/...)
// are declared in package.jsonc dependencies. rootDir is the directory to search upward
// from for the project's package.jsonc.

// checker.ValidateExportSymbols checks that all export declarations in lib.no reference
// symbols that actually exist in the corresponding module source files.

// checker.ValidateStringConcat warns when "+" is used with string operands and suggests "-" instead.

// checker.ValidateHexCase 檢查十六進位字面量是否使用了大寫字母。
// Nolang 慣例：hex 一律小寫（0xff 而非 0xFF）。
// 格式化工具會自動將大寫轉為小寫，此檢查產生 hint 提醒。

// hasUpperHex returns true if a hex literal contains uppercase hex digits
// or an uppercase '0X' prefix.

// checker.ValidatePrintFormat 檢查 print/printf/eprint/eprintf/sprintf 呼叫中的具名格式字串。
// 對於第一個參數為 StringLiteral 的呼叫，解析 {name:spec} 欄位並驗證：
//   - 欄位名稱在當前作用域內已定義（否則 "undefined variable '<name>' in format string"）
//   - 規格字串可被 ParseFormatSpec 解析（否則 "invalid format spec"）
//   - 規格類型字元與變數型別相容（整數類型對應 b/c/d/o/x/X；
//     浮點數對應 e/E/f/F/g/G/%；str/bool 對應 s）

// isPrintFormatCall 判斷呼叫是否為 print/eprint/format/printf/eprintf/sprintf（含 fmt. 前綴）
// printf/eprintf/sprintf 為已廢棄的別名，仍保留以維持向後相容。

// checkPrintFormatInStmt 走訪敘述並驗證 print 格式字串，同時追蹤變數作用域。

// checkPrintFormatInExpr 走訪表達式並驗證 print 格式字串。

// validatePrintFormatCall 驗證單個 print/printf/eprint/eprintf/sprintf 呼叫的格式字串。
// 只驗證含 '{' 的具名格式字串；C-style printf('...%d...', args) 不含 '{' 時跳過，
// 保留向後相容性。

// checkFormatSpecTypeCompat 檢查規格類型字元與變數型別是否相容。
// 回傳非空字串表示錯誤訊息。

// isIntegerTypeStr 判斷型別字串是否為整數類型

// isFloatTypeStr 判斷型別字串是否為浮點數類型

// collectModuleNames returns all known module ShortNames (from #use + auto-imported std modules).

// checker.ModuleExport holds an exported name and its string value from a module file.

// Per-module export cache: parsing a module's .no file to extract its exports
// is expensive, and the std modules are identical across all vet calls in a
// process. Cache the parsed exports keyed by module name.

// checker.GetModuleExports resolves module .no files and extracts their top-level
// LetStatement names with values (for hover) and function names.
// Results are cached per-module-name for the lifetime of the process.

// parseModuleExports resolves a single module's .no file and extracts its
// top-level exports (constants, functions, externs).
// 只從內嵌 StdFS 讀取,支援單二進制分發。

// parseModuleExportsFromSource 從原始碼位元組解析模組導出列表。
// 被 parseModuleExports 調用，支援磁碟和內嵌 StdFS 兩種來源。

// moduleExprValue extracts the string representation of a module-level expression value.

// collectModuleExports tries to resolve each module's .no file and extract its
// top-level LetStatement names (constants) and function names.

// resolveModulePath tries to locate a .no file for the given module name.
// It consults the checker.KnownStdModules() lookup table, matching by ShortName/FullPath
// (which omits the redundant directory name when dir==file), then uses
// FullPath to resolve the actual file.

// checker.ResolveStdModulePath is the exported version of resolveModulePath,
// for use by the LSP server to locate std module source files.

// validateStmtTypes 檢查單個語句的型別問題

// moduleShortName extracts the last path segment as the module name.
// "std/math" → "math", "fmt" → "fmt", "crypto/md5" → "md5"

// checker.StdModuleInfo holds information about a standard library module and is
// still used by the FS-walk path (ListStdModules). The consumer-facing API is
// now the KV map (ShortName -> FullPath) returned by checker.KnownStdModules /
// checker.GetStdModules — no []StdModuleInfo materialization.

// debugCountHashFns is a no-op placeholder retained for call-site compatibility.
// All temporary debugging output has been removed.

// checker.knownStdModules returns the embedded std module KV map
// (ShortName -> FullPath). When an injected sig key is present it returns the
// baked embeddedStdModules directly; otherwise it falls back to an FS walk.

// checker.GetStdModules returns the embedded std module KV map
// (ShortName -> FullPath) for all embedded standard library modules.

// checker.JsModuleInfo 描述一個 JS 相容層模組（src/js/ 下的 .no 檔案）。
// 與 checker.StdModuleInfo 結構相同，但用於 js/ 命名空間。

// knownJsModules 回傳所有內嵌的 JS 相容層模組。
// 使用 //go:embed js 在編譯時發現 src/js/ 下的所有 .no 檔案。
// 與 checker.knownStdModules 機制平行。

// checker.GetJsModules returns checker.JsModuleInfo for all embedded JS compatibility modules.

// checker.CollectStdModuleSignatures parses all std module source files and returns
// function signatures (funcName → return types) and struct field types
// (structName → field name → field type). This is used by the LSP to inject
// extern signatures into the parser so that type inference (e.g. option match
// `it` binding) works correctly for cross-module method calls.

// checker.CollectStdConcreteAliases returns the cached single concrete type aliases
// collected from std modules (e.g. "fd" → "i64" from fs.no). Triggers
// checker.CollectStdModuleSignatures via sync.Once to populate the cache.

// checker.CollectStdStructModules returns a map from struct name to the short name
// of the module that defines it (e.g. "conn" → "tls"). Used by
// checker.ValidateCrossModuleTypeRefs to enforce module-prefix on cross-module type
// references. Triggers checker.CollectStdModuleSignatures via sync.Once.

// extractBaseTypeName unwraps NullableType, PointerType, ArrayType, and
// SliceType wrappers to find the innermost NamedType value string.
// Returns "" if the type is nil or not a NamedType (after unwrapping).

// isInferredType recursively checks if a type node was inferred by the parser
// (not explicitly written by the user). Used by checker.ValidateCrossModuleTypeRefs to
// skip inferred types — they are auto-derived from function call return types,
// so flagging them for missing module prefix would be a false positive.

// isBuiltinType returns true for primitive type names that don't require
// a module prefix.

// checker.ValidateCrossModuleTypeRefs checks that struct field types, variable
// declaration types, and function parameter/result types use the proper
// module prefix when referencing a struct defined in another module.
//
// Per the language spec, cross-module type references must use the
// "module.type" form (e.g. `tls.conn`). Using the bare struct name
// (e.g. `conn`) without the module prefix is an error.
//
// Exceptions:
//   - Builtin primitive types (i64, str, bool, ...)
//   - Types already using a module prefix (contain a dot)
//   - Types defined locally in the current file (structs, type aliases)
//   - Type aliases collected via checker.CollectStdConcreteAliases (e.g. "fd" → "i64")

// checker.GetStdModuleShortNames returns the short names of all embedded standard library
// modules (for use in definedVars and module name registration).

// GetStdModuleFullPaths returns the full paths of all embedded standard library
// modules (for use in file resolution and auto-loading).

// resolveModuleCalls walks the program and rewrites module.fn() calls
// where the DotExpression receiver chain matches an imported module ShortName.
// Supports single-level (base64.encode-std → encode-std) module paths.
// Also rewrites module.CONST constant accesses (e.g. base64.BASE64-STD → BASE64-STD).

// extractModulePathAndFunc walks a DotExpression chain to extract the
// module path (joined with "/") and the final property (function name).
// For example:
//
//	DotExpression{Identifier("math"), "sqrt"}     → ("math", "sqrt")
//	DotExpression{DotExpression{Identifier("hash"), "sha256"}, "sha256"}
//	                                              → ("crypto/sha256", "sha256")
//
// Returns ("", "") if the chain contains non-Identifier nodes.

// resolveSelfMethodCalls rewrites self.method(args) calls inside method bodies
// to StructType.method(self, args), where StructType is derived from the
// function's implicit self parameter.
//
// Also rewrites .field.method(args) calls (where .field is self.field) to
// FieldType.method(self.field, args), so that method calls on struct fields
// are dispatched to the field's type method. This mirrors the self.method()
// rewrite and is required because the LLVM generator only handles Identifier
// receivers, not DotExpression receivers.

// resolveMethodCalls rewrites user-written `Type.method(args)` static method
// calls to `module.Type.method(args)` using the typeOwner registry.
//
// prefixMethodNames() renames method definitions from `Type.method` to
// `module.Type.method` (e.g. bigint.cmp → bigint.bigint.cmp).  However,
// checker.ResolveModuleCalls() only rewrites simple `module.fn()` calls — it does
// NOT touch `Type.method()` calls because the method name contains a dot
// and is not collected into moduleFns.
//
// As a result, user-written `bigint.cmp(d, d2)` would keep fnName="bigint.cmp"
// at codegen time, but funcRetTypes has key "bigint.bigint.cmp", causing the
// call to generate undefined `@bigint.cmp`.
//
// This pass is the symmetric counterpart to prefixMethodNames: it rewrites
// call sites so they match the renamed definitions.  Only CallExpressions
// whose receiver is a bare Identifier matching a typeOwner key are rewritten;
// instance method calls (var.method()) and already-prefixed calls are left
// untouched.

// collectStructFields builds a map from struct name to field name → field type
// string. Used by resolveSelfInExpr to look up field types when rewriting
// .field.method(args) calls.
// structFieldTypeString returns the full type string of a struct field,
// taking into account ArraySize and IsSlice flags that are stored separately
// from f.Type (which only holds the element type).

// collectConcreteTypeAliases builds a map from single concrete type alias
// name to its underlying type string (e.g. "fd" → "i64"). Only aliases of
// the form `name = known-type` (non-union, non-function-type) are collected.
// Used by isConcreteType / isArgTypeCompatible to enforce newtype semantics.

// isConstantExpr returns true if the expression is a compile-time constant literal.

// isConstantName 判斷名稱是否符合 Nolang 大寫常數命名規範。
// 規則：首字元為大寫 ASCII 字母（A-Z），且名稱中不含小寫 ASCII 字母（a-z）。
// 例：SBOX, FNV-OFFSET, O-EXCL, A, MAX-LEN 為常數；sum, i, myVar, Foo 不為常數。
// 用於重複定義偵測——大寫常數即使無顯式型別註記也應禁止重複賦值。

// matchesTargetPlatform 檢查宣告的 PlatformKeys 是否符合目標平台。
// 無 PlatformKeys（平台通用宣告）永遠回傳 true。
// goos/goarch 為空時（未設定目標平台），也回傳 true（向後相容）。

// resolveModuleConstants walks the program and replaces Identifier references to
// module constants with their literal values, allowing module functions like
// degrees() to reference pi/e directly.

// checker.ValidateFuncArgs checks that function call argument types match the function signature.
// rootDir is optional — if empty, only locally defined function signatures are checked.
// If provided, imported function signatures from module files are also resolved.
// funcSigFromDef extracts the parameter and result type info from a function
// definition, used to build the signature table for type inference.

// funcSigFirstReturnType returns the type of the function's first result
// parameter, or "" if the function has no results.
