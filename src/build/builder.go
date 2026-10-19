package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/build/llvmapi"
	"github.com/lizongying/nolang/build/wasm"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// DetectTarget 根据当前运行平台返回对应的 target triple。
// 用于 no build/run/test 未指定 -target 时的默认值。
func DetectTarget() string {
	arch := runtime.GOARCH
	osName := runtime.GOOS

	// 映射 GOARCH到LLVM架构名
	var llvmArch string
	switch arch {
	case "amd64":
		llvmArch = "x86_64"
	case "arm64":
		llvmArch = "aarch64"
	default:
		// 不支持的架构返回空字符串，让llc使用默认值
		return ""
	}

	// 映射GOOS到LLVM系统名
	//
	// 注意：macOS 必须使用 Apple 的 darwin 环境（apple-darwin / apple-macosx），
	// 不能用 "macos-gnu"。gnu 环境会让 clang 走 Linux/ELF 风格的标头与 sysroot
	// 搜索路径，从而跳过 macOS SDK，导致 <stdlib.h> 等系统标头找不到
	// （process_runtime.c 等任何内嵌 C 在链接阶段都会编译失败）。
	// Linux 用 linux-gnu、Windows 用 windows-gnu（MinGW）均正确。
	var llvmOS string
	switch osName {
	case "linux":
		llvmOS = "linux-gnu"
	case "darwin":
		llvmOS = "apple-darwin"
	case "windows":
		llvmOS = "windows-gnu"
	default:
		// 不支持的系统返回空字符串
		return ""
	}

	return llvmArch + "-" + llvmOS
}

// zigTargetTriple 把 Apple 的 LLVM triple（如 "aarch64-apple-darwin"）轉成
// zig 接受的長相（"aarch64-macos"）。zig 連結已編譯好的 object 時，若
// --target 仍用 "apple-darwin" 會報
// "unable to parse target query 'aarch64-apple-darwin': UnknownOperatingSystem"
// —— zig 的 OS 名是 "macos" 而非 LLVM 的 "darwin"。clang 原生接受 apple-darwin，
// 不走此轉換。非 Apple 目標原樣返回（冪等）。
func zigTargetTriple(t string) string {
	if t == "" {
		return t
	}
	parts := strings.Split(t, "-")
	if len(parts) < 2 {
		return t
	}
	for _, p := range parts[1:] {
		// 任何 darwin 組件（darwin / darwin25.0.0 / apple-darwin）都視為 Apple 平台。
		if p == "darwin" || p == "apple-darwin" || strings.HasPrefix(p, "darwin") {
			return parts[0] + "-macos"
		}
	}
	return t
}

// llvmCodegenTriple 把使用者傳入的 -target（可能是 zig 風格方言，如
// "x86_64-macos-none" / "aarch64-windows-gnu"）映射成 LLVM 規範 triple，供
// 進程內 LLVM C API 後端（no-llvm）決定正確的 object 格式（Mach-O / COFF / ELF）。
//
// 根因：LLVM 的 triple parser 依「arch-vendor-os-env」的位置解析。zig 方言把 OS
// 名稱放在 vendor 槽位（如 "x86_64-macos-none" → vendor="macos"、os="none"；
// "x86_64-windows-gnu" → vendor="windows"、os="gnu"），LLVM 據此把 OS 判成
// UnknownOS，逆退回「宿主」object 格式——在 Linux runner 上就是 ELF。結接階段
// zig 用 darwin 的 TBD 解析器讀 ELF 會報 "failed to parse TBD file"，用 lld-link
// 讀 ELF 會報 "unknown file type"。把 OS 顯式放進對的位置（apple-macosx /
// pc-windows-gnu）即可讓 LLVM 產出正確格式。
//
// 只用於 LLVM codegen 的 triple；結接仍用 zigTargetTriple(target) 頲給 zig 方言
// （兩者拼寫不同，必須分開）。無法辨識的平台（如 wasm32／空 target）原樣返回，
// 維持既有行為。
func llvmCodegenTriple(target string) string {
	goos, goarch := parseTargetPlatform(target)
	var arch string
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	default:
		// wasm32 或未知架構：不改寫，退回呼叫端（pickTriple）以模式／宿主 triple 處理。
		return target
	}
	switch goos {
	case "darwin":
		return arch + "-apple-macosx"
	case "windows":
		return arch + "-pc-windows-gnu"
	case "linux":
		return arch + "-unknown-linux-gnu"
	default:
		return target
	}
}

// CheckToolchain verifies that the tools the chosen build path actually needs
// are available, returning OS-specific install instructions otherwise.
//
// 註：llvm-config 在整個「.no 程式构建」流程中從不被 exec（build 期只用
// opt/llc/clang/zig，llvm-config 僅在 make 階段為 no 二進位取 cflags/ldflags），
// 這裡只把它當成「系統是否裝了 LLVM」的哨兵。有兩條路徑根本不需要系統 LLVM，
// 故跳過該哨兵，否則「零外部工具鏈」的 CI smoke test 會因 runner 只裝了
// llvm-config-22（沒有同名 llvm-config）而誤報「未檢測到 LLVM 工具鏈」：
//  1. 內置 LLVM 的 no-llvm 變體（Enabled && Available）：optimize+codegen 全在進程內，
//     cc 只做連結，完全不碰外部 LLVM；
//  2. cc == "zig"：zig 自帶 clang+lld+libc，單進程即可完成最佳化/程式碼生成/連結。
func CheckToolchain(cc string) error {
	// WASM 建構下無法呼叫本機 LLVM 工具鏈（opt/llc/clang）；
	// 應改用 --wasm-direct 直接產生 WASM 的路徑。
	if runtime.GOOS == "wasip1" {
		return fmt.Errorf("LLVM toolchain not available in WASM build; use --wasm-direct flag")
	}
	if capiActive := llvmapi.Enabled() && llvmapi.Available(); !capiActive && cc != "zig" {
		if _, err := exec.LookPath("llvm-config"); err != nil {
			return fmt.Errorf(`未檢測到 LLVM 工具鏈
  macOS: brew install llvm
  Ubuntu: sudo apt install llvm-dev clang
  Windows: winget install LLVM.LLVM`)
		}
	}

	switch cc {
	case "zig":
		if _, err := exec.LookPath("zig"); err != nil {
			return fmt.Errorf(`未檢測到 Zig 編譯器
  macOS: brew install zig
  Ubuntu: sudo snap install zig --classic
  Windows: winget install zig.zig`)
		}
	default:
		if _, err := exec.LookPath("clang"); err != nil {
			return fmt.Errorf(`未檢測到 clang，請確認 LLVM 工具鏈完整安裝
  macOS: brew install llvm
  Ubuntu: sudo apt install llvm-dev clang
  Windows: winget install LLVM.LLVM`)
		}
	}

	return nil
}

// BuildOptions holds all options for a build operation.
type BuildOptions struct {
	CC            string // C compiler: "clang" or "zig"
	Target        string // target triple (e.g. "x86_64-linux-gnu", "" = auto)
	Verbose       bool
	Output        string // optional output path ("" = auto)
	NoBoundsCheck bool   // skip bounds checks (unsafe mode, for max performance)
	UseDirectWasm bool   // use Direct WASM backend (no LLVM toolchain required)
	UseJS         bool   // use JS backend (emit JavaScript source, no LLVM toolchain)
	BrowserMode   bool   // when true with UseJS, generate browser-targeted JS + HTML wrapper
	// OptionInlineThreshold 覆寫 `?T` 載荷內聯的字節閾值（0 = 沿用 package.jsonc
	// / NOLANG_OPTION_INLINE_THRESHOLD 的解析結果）。見 transpiler.SetOptionInlineThreshold。
	OptionInlineThreshold int
	CompilerVersion       string            // current compiler version (for package.jsonc compatibility check)
	Strict                bool              // vet mode: treat all lint warnings/hints as errors
	LDFlags               map[string]string // -ld-KEY=VALUE pairs injected as compile-time global constants
	// ESMCDN overrides the import-map CDN base for browser ESM output
	// ("" = package.jsonc compiler.esm-cdn, else the built-in default).
	ESMCDN string
}

// versionCompatible 檢查 package.jsonc 中聲明的編譯器版本是否與當前編譯器版本兼容。
// package.jsonc 的 version 約定帶前導 v（如 "v0.1.0"），但解析時前後綴皆相容：
// 一律先剝除前導 "v" 再比較，因此 "v0.1.0" 與 "0.1.0" 視為同一版本。
// 採用語義化版本規則：
//   - major >= 1 時，major 版本相同即視為兼容（1.2.0 與 1.3.1 兼容）
//   - major == 0 時（初始開發階段），API 不穩定，以 minor 作為兼容性判斷依據
//     （0.1.0 與 0.1.5 兼容，但 0.1.0 與 0.2.0 不兼容）
//   - 若無法解析為 semver，則要求精確匹配
func versionCompatible(required, current string) bool {
	required = strings.TrimPrefix(strings.TrimSpace(required), "v")
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	if required == "" || current == "" {
		return true
	}
	if required == current {
		return true
	}
	// 嘗試 semver 解析：major.minor.patch
	parseSemver := func(v string) (major, minor int, ok bool) {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, ".", 3)
		if len(parts) < 2 {
			return 0, 0, false
		}
		m, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, false
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
		return m, n, true
	}
	reqMajor, reqMinor, ok1 := parseSemver(required)
	curMajor, curMinor, ok2 := parseSemver(current)
	if ok1 && ok2 {
		if reqMajor == 0 && curMajor == 0 {
			// 0.x.y 階段：minor 必須相同才算兼容
			return reqMinor == curMinor
		}
		// 1.x.y 及以上：major 相同即兼容
		return reqMajor == curMajor
	}
	// 無法解析為 semver，要求精確匹配
	return required == current
}

// ClearCaches 清空全局 token 緩存和模組 AST 緩存。
// 應在命令級別（no build / no test / no vet 等）開始時調用一次，
// 之後所有文件共享同一份緩存，避免重複詞法分析和解析。
// 不在每次 Compile 調用中清空，使 no test 等多文件場景能跨文件復用。
func ClearCaches() {
	lexer.ClearTokenCache()
	checker.ClearModuleCache()
}

// parseTargetPlatform extracts (goos, goarch) from a target triple.
// Returns ("", "") when target is empty, signaling the caller to fall back
// to the host runtime platform.
//
// Recognized architectures (first hyphen-separated component):
//   - x86_64    → amd64
//   - aarch64   → arm64
//   - wasm32    → wasm32
//
// Recognized operating systems (any subsequent component):
//   - linux     → linux
//   - windows   → windows
//   - macos     → darwin
//   - darwin    → darwin
//   - wasi      → wasi   (wasm32-wasi, wasm32-wasi-threads)
//   - unknown   → wasi   (wasm32-unknown-wasi / wasm32-unknown-unknown;
//     保守地將 unknown wasm 目標視為 wasi)
//
// Examples:
//   - "x86_64-linux-gnu"           → ("linux", "amd64")
//   - "aarch64-linux-gnu"          → ("linux", "arm64")
//   - "x86_64-windows-gnu"         → ("windows", "amd64")
//   - "aarch64-darwin"             → ("darwin", "arm64")
//   - "x86_64-apple-macos"         → ("darwin", "amd64")
//   - "wasm32-wasi"                → ("wasi", "wasm32")
//   - "wasm32-wasi-threads"        → ("wasi", "wasm32")
//   - "wasm32-unknown-wasi"        → ("wasi", "wasm32")
//   - "wasm32-unknown-unknown"     → ("wasi", "wasm32")
//   - ""                           → ("", "")
func parseTargetPlatform(target string) (goos, goarch string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", ""
	}
	parts := strings.Split(target, "-")
	if len(parts) == 0 {
		return "", ""
	}
	switch parts[0] {
	case "x86_64":
		goarch = "amd64"
	case "aarch64":
		goarch = "arm64"
	case "wasm32":
		goarch = "wasm32"
	}
	for _, p := range parts[1:] {
		switch p {
		case "linux":
			goos = "linux"
		case "windows", "mingw32", "mingw64", "w64":
			goos = "windows"
		case "macos", "darwin", "apple":
			goos = "darwin"
		case "wasi":
			goos = "wasi"
		case "unknown":
			// wasm32-unknown-wasi / wasm32-unknown-unknown：保守地視為 wasi。
			// 僅在 wasm32 目標且尚未偵測到其他 OS 時覆蓋，避免影響
			// x86_64-unknown-linux-gnu 等原生 triple 的既有行為。
			if goarch == "wasm32" && goos == "" {
				goos = "wasi"
			}
		}
	}
	return goos, goarch
}

// BuildFile compiles a .no source file and produces the output binary/file.
func BuildFile(inputPath string, opts BuildOptions) (string, error) {
	// 工具鏈檢查
	if err := CheckToolchain(opts.CC); err != nil {
		return "", err
	}

	// 若指定的是目錄，先找目錄內的 package.jsonc
	info, err := os.Stat(inputPath)
	isDir := err == nil && info.IsDir()

	var pkg *Package
	if isDir {
		pkg, _ = LoadPackage(inputPath)
		if pkg != nil {
			if _, err := pkg.EnsureDependencies(10); err != nil {
				return "", fmt.Errorf("dependency resolution failed: %w", err)
			}
			mainFile := pkg.Main
			if mainFile == "" {
				mainFile = "main.no"
			}
			inputPath = pkg.ResolvePath(mainFile)
		}
	}

	// 如果仍然是指向目錄（無 package config 的情況），預設使用 main.no
	if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
		mainPath := filepath.Join(inputPath, "main.no")
		if _, err := os.Stat(mainPath); err != nil {
			return "", fmt.Errorf("main.no not found in %s", inputPath)
		}
		inputPath = mainPath
	}

	// 若尚未載入 package（非目錄模式），從檔案目錄載入
	if pkg == nil {
		pkgDir := filepath.Dir(inputPath)
		pkg, _ = LoadPackage(pkgDir)
		if pkg != nil {
			if _, err := pkg.EnsureDependencies(10); err != nil {
				return "", fmt.Errorf("dependency resolution failed: %w", err)
			}
		}
	}

	return buildWithPkg(inputPath, pkg, opts, false)
}

// ─── 後端選擇（集中化） ──────────────────────────────────────────────────────
// 三個後端（LLVM / JS / Direct-WASM）的「選哪個」決策原本散落在 buildCommand /
// runCommand / testCommand 三處命令層，外加 buildWithPkg 一處編排層（共 4 份重複的
// 判斷邏輯）。任何「新增後端 / 改預設行為」都要同時改多處，極易遺漏。這裡把它
// 收斂成單一決策 ResolveBackend + 單一分發 BuildBackend，命令層只做調用。
type BackendKind int

const (
	BackendLLVM BackendKind = iota
	BackendJS
	BackendWASMDirect
)

// ResolveBackend 是唯一的後端決策點。優先級：Direct-WASM > JS > LLVM。
//   - Direct-WASM：明確 --wasm-direct，或 wasip1 平台下目標含 wasm32（無 LLVM 可用）。
//   - JS：命令列 -js，或 package.jsonc 的 compiler.emit == "js"。
//   - 其餘皆為 LLVM（MIR 後端）。
func ResolveBackend(opts BuildOptions, pkg *Package) BackendKind {
	if opts.UseDirectWasm || (runtime.GOOS == "wasip1" && strings.Contains(opts.Target, "wasm32")) {
		return BackendWASMDirect
	}
	if opts.UseJS || (pkg != nil && pkg.Compiler.Emit == "js") {
		return BackendJS
	}
	return BackendLLVM
}

// BuildBackend 是唯一的後端分發點：依 ResolveBackend 的結果呼叫對應的具體建構
// 路徑，並把產物寫入 opts.Output（若為空則由各後端使用預設路徑）。返回產物路徑，
// 供 run/test 執行、或供 build 提示使用者產物位置（見下方「預設輸出路徑不直觀」修正）。
// 函式本身不打印任何訊息——路徑的提示由呼叫方統一處理，避免 run/test 被多餘輸出干擾。
func BuildBackend(kind BackendKind, inputPath string, opts BuildOptions) (outPath string, err error) {
	switch kind {
	case BackendWASMDirect:
		return buildWasmDirect(inputPath, opts)
	case BackendJS:
		return buildJSBackend(inputPath, opts)
	default: // BackendLLVM
		return BuildFile(inputPath, opts)
	}
}

// buildWasmDirect 對應原 main.go 的 --wasm-direct 分支：繞過 LLVM 工具鏈，直接
// 發射 WASM 字節碼並寫入 opts.Output（預設 a.wasm）。
func buildWasmDirect(inputPath string, opts BuildOptions) (string, error) {
	if !opts.UseDirectWasm && runtime.GOOS == "wasip1" {
		fmt.Fprintln(os.Stderr, "[warn] wasip1 runtime: auto-switching to Direct WASM backend (LLVM toolchain unavailable)")
	}
	out := opts.Output
	if out == "" {
		out = "a.wasm"
	}
	if inputPath == "." {
		return "", fmt.Errorf("--wasm-direct requires an explicit input file (workspace mode not supported)")
	}
	wasmBytes, err := BuildDirectWasm(inputPath, opts)
	if err != nil {
		return "", fmt.Errorf("WASM build error: %w", err)
	}
	if err := os.WriteFile(out, wasmBytes, 0755); err != nil {
		return "", fmt.Errorf("writing WASM output %s: %w", out, err)
	}
	return out, nil
}

// VetFile performs syntax and semantic validation on a .no source file without
// producing any compilation artifacts (no LLVM IR, no binary).
func VetFile(inputPath string, opts BuildOptions) error {
	_, err := VetFileWithLints(inputPath, opts)
	return err
}

// VetFileWithLints 與 VetFile 相同，但額外返回 vet 模式下收集的 lint 結果。
// lint 結果包含命名規範、未用變數、embed 校驗、接口實現、字串拼接等
// 非阻塞性診斷——調用者可選擇輸出為 warning/hint 或在 --strict 模式下
// 升級為 error。
func VetFileWithLints(inputPath string, opts BuildOptions) ([]checker.LintResult, error) {
	// 若指定的是目錄，先找目錄內的 package.jsonc
	info, err := os.Stat(inputPath)
	isDir := err == nil && info.IsDir()

	var pkgDir string
	if isDir {
		pkgDir = inputPath
	} else {
		pkgDir = filepath.Dir(inputPath)
	}

	pkg, _ := LoadPackage(pkgDir)
	if pkg != nil && isDir {
		if _, err := pkg.EnsureDependencies(10); err != nil {
			return nil, fmt.Errorf("dependency resolution failed: %w", err)
		}
		mainFile := pkg.Main
		if mainFile == "" {
			mainFile = "main.no"
		}
		inputPath = pkg.ResolvePath(mainFile)
	}

	// 如果仍然是指向目錄（無 package config 的情況），預設使用 main.no
	if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
		mainPath := filepath.Join(inputPath, "main.no")
		if _, err := os.Stat(mainPath); err != nil {
			return nil, fmt.Errorf("main.no not found in %s", inputPath)
		}
		inputPath = mainPath
	}

	source, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	compiler := NewTranspiler(pkg)
	compiler.sourcePath = inputPath
	goos, goarch := parseTargetPlatform(opts.Target)
	compiler.SetTargetPlatform(goos, goarch)
	compiler.SetLDFlags(opts.LDFlags)
	// vet 模式：跳過 LLVM IR 生成，只做前端驗證（語法+型別+模組合併+單態化）
	compiler.SetVetMode(true)
	compiler.SetVetStrict(opts.Strict)
	_, err = compiler.Compile(string(source))
	if err != nil {
		// 將 compile error 解析為結構化 LintResult，避免塌縮成一行
		// （保留行號/列號/來源），使 no vet 輸出格式與 LSP 一致。
		structured := parseCompileErrorToLints(err)
		lints := compiler.VetLints()
		lints = append(lints, structured...)
		return lints, nil
	}

	return compiler.VetLints(), nil
}

// compileErrLocRe matches the location prefix of ONE diagnostic inside a joined
// compile-error string: an optional "<file>: " prefix followed by the canonical
// location, either the full "line L, column C:" or the column-less "line L:"
// used by checkUnresolvedModuleCalls. It is used both to split the joined
// string at real diagnostic boundaries and to pull the position out of a part.
//
// The file group is deliberately `[^:\n]+` (no colon) so it cannot swallow a
// previous diagnostic's location, and `\s*` absorbs the single space that
// fmt.Errorf("%s: %s", path, body) inserts after the path's colon.
var compileErrLocRe = regexp.MustCompile(`(?:(?P<file>[^:\n]+):\s*)?line (?P<line>\d+)(?:, column (?P<col>\d+))?:`)

// splitCompileErrParts splits a joined compile-error string into one string per
// diagnostic. Empty/blank fragments are dropped, so a caller can range over the
// result unconditionally.
//
// A plain strings.Split(msg, "; ") is wrong: message text legitimately contains
// a semicolon (e.g. "export alias 'x' is the same as the function name; the
// alias can be omitted"), which would be cut in half and the tail would become a
// location-less lint. Splitting only where a location prefix follows the "; "
// keeps such a message whole.
func splitCompileErrParts(msg string) []string {
	locs := compileErrLocRe.FindAllStringIndex(msg, -1)
	if len(locs) == 0 {
		if p := strings.TrimSpace(msg); p != "" {
			return []string{p}
		}
		return nil
	}
	// A non-final fragment ends with the "; " separator, which must go; the last
	// one has no separator, so its trailing text is message content and is left
	// alone (a message could legitimately end in ';').
	trimSep := func(s string) string {
		s = strings.TrimSpace(s)
		if strings.HasSuffix(s, ";") {
			s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
		}
		return s
	}
	var parts []string
	// Anything before the first location is its own (location-less) diagnostic.
	if head := trimSep(msg[:locs[0][0]]); head != "" {
		parts = append(parts, head)
	}
	for i, loc := range locs {
		if i+1 < len(locs) {
			if p := trimSep(msg[loc[0]:locs[i+1][0]]); p != "" {
				parts = append(parts, p)
			}
			continue
		}
		if p := strings.TrimSpace(msg[loc[0]:]); p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// parseCompileErrorToLints 把 Compile() 返回的 error 字串解析為結構化
// LintResult 條目。Compile 內部的錯誤格式有三種：
//   - "validation errors: line L, column C: msg1 [id]; line L, column C: msg2 [id]"
//   - "parser errors: line L, column C: msg1 [E_XXX]; line L, column C: msg2 [E_XXX]"
//     （由 parser.FormatDiagnostics 產生，見 transpiler.go）
//   - "check error: line L: msg"（來自 checkUnresolvedModuleCalls）
//
// 可選的 "<file>: " 前綴（parseFile / parseEmbeddedProgram 為被匯入模組加上）
// 會被提取到 LintResult.File，使診斷歸屬到真正的來源檔而非被 vet 的主檔。
//
// 每條提取行號/列號，統一標記為 error 級別。行號/列號一旦被提取，就從 Message
// 中移除：呼叫方（printLintResults / LSP）會用自己的欄位重新渲染位置，留在
// Message 裡只會讓同一條診斷印兩次 "line L, column C:"。
func parseCompileErrorToLints(err error) []checker.LintResult {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var results []checker.LintResult

	// 去掉外層包裝前綴，保留含行號的原始消息
	for _, prefix := range []string{
		"validation error: ", "validation errors: ",
		"parser error: ", "parser errors: ",
		"check error: ",
	} {
		msg = strings.TrimPrefix(msg, prefix)
	}

	for _, part := range splitCompileErrParts(msg) {
		// 提取並移除 [xxx] 後綴（由 transpiler.go / parser 格式化注入）
		// suffix 形如 " [traceid]"：suffix[0]=' '、suffix[1]='['，故 traceid 為
		// suffix[2 : len-1]。注意勿寫死偏移量（曾誤用 8，把所有 trace id 截成末
		// 兩字元，如 "ovfhndld"→"ld"）。len>=3 防護避免 " []" 之類切片越界。
		tid := ""
		if idx := strings.LastIndex(part, " ["); idx >= 0 {
			suffix := part[idx:]
			if strings.HasSuffix(suffix, "]") && len(suffix) >= 3 {
				tid = suffix[2 : len(suffix)-1]
				part = strings.TrimSpace(part[:idx])
			}
		}
		lr := checker.LintResult{
			Severity: checker.LintError,
			Source:   "nolang-compile",
			Message:  part,
			TraceID:  tid,
		}
		// 提取位置（可選 "<file>: " 前綴 + "line L, column C:" / "line L:"），
		// 並把位置前綴從消息正文中移除。
		if m := compileErrLocRe.FindStringSubmatchIndex(part); m != nil && m[0] == 0 {
			sub := func(name string) string {
				i := compileErrLocRe.SubexpIndex(name)
				if i < 0 || m[2*i] < 0 {
					return ""
				}
				return part[m[2*i]:m[2*i+1]]
			}
			lr.File = sub("file")
			lr.Line, _ = strconv.Atoi(sub("line"))
			lr.Column, _ = strconv.Atoi(sub("col"))
			if rest := strings.TrimSpace(part[m[1]:]); rest != "" {
				lr.Message = rest
			}
		}
		results = append(results, lr)
	}
	return results
}

// buildWithPkg 是核心編譯邏輯，使用已載入的 Package（可為 nil）。
// BuildFile 和 BuildWorkspace 共用此函數，避免重複載入 Package。
func buildWithPkg(inputPath string, pkg *Package, opts BuildOptions, buffered bool) (string, error) {
	// 檢查 package.jsonc 中聲明的編譯器版本是否與當前編譯器版本匹配
	if pkg != nil && pkg.Compiler.Version != "" && opts.CompilerVersion != "" {
		if !versionCompatible(pkg.Compiler.Version, opts.CompilerVersion) {
			msg := fmt.Sprintf("warning: compiler version mismatch: package.jsonc requires %q, current compiler is %q\n",
				pkg.Compiler.Version, opts.CompilerVersion)
			fmt.Fprint(os.Stderr, msg)
		}
	}

	source, err := os.ReadFile(inputPath)
	if err != nil {
		return "", fmt.Errorf("reading input file: %w", err)
	}

	// 決定輸出名稱：main 檔 → 套件名稱，lib 檔 → lib-套件名稱
	baseName := strings.TrimSuffix(filepath.Base(inputPath), ".no")
	fileName := baseName
	if pkg != nil && pkg.Name != "" {
		mainBase := "main"
		if pkg.Main != "" {
			mainBase = strings.TrimSuffix(filepath.Base(pkg.Main), ".no")
		}
		if baseName == mainBase {
			fileName = pkg.Name
		} else if baseName == "lib" {
			fileName = "lib-" + pkg.Name
		}
	}

	var outPath string
	if opts.Output != "" {
		outPath = opts.Output
	} else {
		rootDir := "."
		outDir := "dist" // 預設輸出目錄
		if pkg != nil {
			rootDir = pkg.RootDir
			if pkg.Output != "" {
				outDir = strings.TrimPrefix(pkg.Output, "./")
			}
		}
		distDir := filepath.Join(rootDir, outDir)
		if err = os.MkdirAll(distDir, 0755); err != nil {
			return "", fmt.Errorf("creating dist directory: %w", err)
		}
		outPath = filepath.Join(distDir, fileName)
	}

	// 並行構建時使用緩衝區避免輸出交錯
	var sink *bytes.Buffer
	if buffered {
		sink = &bytes.Buffer{}
	}

	// 如果 package.jsonc 指定 emit: js，使用 JS 後端發射 JavaScript
	if pkg != nil && pkg.Compiler.Emit == "js" {
		if err := buildJSPkg(source, inputPath, pkg, opts, outPath, sink); err != nil {
			return "", err
		}
		return outPath, nil
	}

	// LLVM 路徑
	compiler := NewTranspiler(pkg)
	compiler.sourcePath = inputPath
	goos, goarch := parseTargetPlatform(opts.Target)
	compiler.SetTargetPlatform(goos, goarch)
	compiler.SetNoBoundsCheck(opts.NoBoundsCheck)
	compiler.SetVerbose(opts.Verbose)
	compiler.SetOptionInlineThreshold(opts.OptionInlineThreshold)
	compiler.SetLDFlags(opts.LDFlags)
	code, err := compiler.Compile(string(source))

	// 打印主程序中链式 -> 条件的 lint 提示（仅主程序，不含 std 库，避免噪音）。
	// 即便后续 codegen/链接失败，parse 阶段已收集的提示仍有价值，故在 err 检查前打印。
	for _, hint := range compiler.ChainedIfHints() {
		fmt.Fprintln(os.Stderr, hint)
	}

	// 打印 `+=` / `-=` / `*=` / `/=` / `%=` 的廢棄提示。與上面同理在 err 檢查前：
	// 這幾條由 parser 期診斷帶來，與編譯成敗無關；格式走 Diagnostic.Error()，
	// 與 W_CHAIN_IF 的輸出一致。
	for _, d := range compiler.CompoundAssignHints() {
		fmt.Fprintln(os.Stderr, d.Error())
	}

	if err != nil {
		return "", fmt.Errorf("compilation error: %w", err)
	}

	var linkLibs []string
	if pkg != nil {
		linkLibs = pkg.Compiler.LinkLibs
	}
	// 編譯單元內每個被載入套件自己宣告的 link-libs（見 Transpiler.LinkLibs）。
	// `no run <file>` / `# <path>` 直引源檔時 pkg == nil，這是唯一的連結庫來源：
	// tests/test-ffi-sqlite.no 直接引用 example/sqlite-driver/src/sqlite.no，
	// 驅動的 #{c} 外部函式需要 -lsqlite3。
	for _, lib := range compiler.LinkLibs() {
		dup := false
		for _, have := range linkLibs {
			if have == lib {
				dup = true
				break
			}
		}
		if !dup {
			linkLibs = append(linkLibs, lib)
		}
	}
	// NOLANG_LINK_LIBS 為顯式覆寫／診斷開關（逗號分隔的庫名，不含 -l）。
	if env := os.Getenv("NOLANG_LINK_LIBS"); env != "" {
		for _, l := range strings.Split(env, ",") {
			if l = strings.TrimSpace(l); l != "" {
				linkLibs = append(linkLibs, l)
			}
		}
	}

	if err := buildLLVMInternal(code, fileName, outPath, opts.CC, opts.Target, opts.Verbose, linkLibs, sink); err != nil {
		if sink != nil && sink.Len() > 0 {
			fmt.Fprint(os.Stderr, sink.String())
		}
		return "", fmt.Errorf("build error: %w", err)
	}

	if sink != nil && sink.Len() > 0 {
		fmt.Fprint(os.Stderr, sink.String())
	}

	return outPath, nil
}

// vprintf 輸出 verbose 訊息到 sink（若 nil 則寫 os.Stderr）
func vprintf(sink *bytes.Buffer, format string, args ...any) {
	if sink != nil {
		fmt.Fprintf(sink, format, args...)
	} else {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// ─── 型別指標（typed pointer）的處理 ───────────────────────────────────────────
// codegen 仍輸出 LLVM <17 風格的型別化指標 IR（i8*、%str-long*、[5 x i8]* 等）。
// 早期版本在此做一次「純文字」重寫把它轉成 opaque pointer（ptr），但實測證明 LLVM 17+
// 的 IR 解析器會自動把型別化指標升級為 opaque `ptr`（opt/llc 靜默接受、無 warning），
// 故該 toOpaquePointers 重寫層是死代碼，已於 2026-10-09 刪除——不要再加回來。

// buildLLVMInternal writes LLVM IR and compiles it to an executable.
// Default (non-verbose): a single clang/zig cc process compiles the .ll
// directly — opt + llc + link all happen inside clang (one spawn, minimal
// toolchain dependency: only clang, not opt/llc). With -v, the standalone
// opt + llc stages are kept so intermediate artifacts (<out>_opt.ll / <out>.s)
// are emitted for inspection.
// If sink is non-nil, output is buffered to avoid interleaving in parallel builds.
func buildLLVMInternal(code string, fileName string, outPath string, cc string, target string, verbose bool, linkLibs []string, sink *bytes.Buffer) error {
	// WASM 建構下無法呼叫本機 LLVM 工具鏈（opt/llc/clang）。
	// 瀏覽器 playground 應改用 --wasm-direct 路徑（直接產生 WASM，不經過 LLVM IR）。
	if runtime.GOOS == "wasip1" {
		return fmt.Errorf("LLVM toolchain not available in WASM build; use --wasm-direct flag")
	}

	// 偵測 WASI 目標：wasm32-wasi / wasm32-wasi-threads / wasm32-unknown-wasi 等。
	// WASI 目標需要 --sysroot=$WASI_SYSROOT，且跳過 -lws2_32 等原生平台連結旗標。
	isWasiTarget := strings.Contains(target, "wasm32-wasi") || strings.Contains(target, "wasm32-unknown-wasi") || strings.Contains(target, "wasm32-wasi-threads")

	tempDir, err := os.MkdirTemp("", "nolang")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	// 中間產物（.ll/.s）每次建構可達數百 MB，必須清理，否則會撐爆系統暫存目錄。
	// 需要保留 IR 供分析時設 NOLANG_KEEP_IR（與 cmd/no run 路徑同一約定）。
	keepIR := os.Getenv("NOLANG_KEEP_IR") != ""
	if !keepIR {
		defer os.RemoveAll(tempDir)
	} else {
		fmt.Fprintf(os.Stderr, "[debug] keep build tmp dir: %s\n", tempDir)
	}

	// 後端管線（native / WASI 共用）：
	//   預設（非 verbose）＝ 單進程：直接把 .ll 交給 clang / zig cc，
	//     由 clang 在內部完成 optimize + lower + link（等同 `clang -x ir`），
	//     不再分別 spawn opt / llc。這是 MIR 後端的標準路徑，依賴面最小
	//     （只需 clang，不須 Homebrew LLVM 的 opt / llc）。
	//   verbose ＝ 保留 opt -O2 + llc 兩段，產出可檢視的中間檔
	//     （<out>_opt.ll 與 <out>.s），方便除錯。
	optLevel := os.Getenv("NOLANG_OPT_LEVEL")
	if optLevel == "" {
		optLevel = "-O2"
	}
	singleShot := !verbose

	// MIR IR 關閉 FP contraction（避免 fmul+fadd 融合成 FMA，與 MIR 驗證基準一致）；
	// 其餘（理論上已移除的 legacy）才用 fast。
	fpContract := "fast"
	if strings.Contains(code, "!nolang.mir.backend") {
		fpContract = "off"
	}

	// ─── LLVM C API 進程內編譯（僅非 debug 模式；開關默認關閉）───────────────
	// 非 verbose（singleShot）時，原本的路徑是把 .ll 寫盤後 spawn `clang -x ir`，
	// 由 clang 在內部完成 optimize + codegen + link。開關開啟（NOLANG_LLVM_CAPI=1
	// 或 --llvm-capi）且本二進位帶有 llvm_capi build tag 時，改成在進程內用 LLVM C API
	// 把 IR 直接編成 .o，後續只讓 cc 做連結。
	// debug（-v）模式不受本開關影響：仍走 opt + llc + clang，保留可檢視的中間產物。
	useCAPI := false
	if singleShot && llvmapi.Enabled() {
		if !llvmapi.Available() {
			vprintf(sink, "[warn] %s=1 but this binary has no LLVM C API backend; falling back to clang (rebuild with: make no-capi)\n", llvmapi.EnvSwitch)
		} else {
			useCAPI = true
		}
	}

	// .ll 是否落盤：
	//   - verbose（debug）：必須寫（opt 要吃它）
	//   - 走 LLVM C API：IR 直接從內存餵給 C API，磁碟上那份沒人讀 → 跳過
	//     （但 NOLANG_KEEP_IR 是顯式的調試意圖，此時仍寫，保留可分析的產物）
	//   - 其餘（clang 單進程 / 開關開但無 CAPI 後端而回退）：必須寫
	llPath := filepath.Join(tempDir, fileName+".ll")
	if !useCAPI || keepIR {
		// LLVM 17+ 的 IR 解析器會自動把 typed pointer（i8*、%vec* 等）升級為 opaque
		// `ptr`，故不再需要 toOpaquePointers 文本重寫層（死代碼，2026-10-09 刪除）。
		if err = os.WriteFile(llPath, []byte(code), 0644); err != nil {
			return fmt.Errorf("writing LLVM IR file: %w", err)
		}
	}

	// 保留 .ll 供分析（僅 -v 時輸出）
	if verbose {
		llOut := outPath + ".ll"
		if mkErr := os.MkdirAll(filepath.Dir(llOut), 0755); mkErr == nil {
			if wErr := os.WriteFile(llOut, []byte(code), 0644); wErr != nil {
				fmt.Fprintf(os.Stderr, "[debug] failed to write %s: %v\n", llOut, wErr)
			} else {
				fmt.Fprintf(os.Stderr, "[debug] wrote %s (%d bytes)\n", llOut, len(code))
			}
		}
	}

	// ─── LLVM C API：真正執行編譯（是否走這條路的決策已在寫 .ll 之前做完）─────
	var capiObject string
	if useCAPI {
		objPath := filepath.Join(tempDir, fileName+".o")
		capiOpts := llvmapi.Options{
			IR:         code,
			Triple:     llvmCodegenTriple(target),
			OptLevel:   llvmapi.ParseOptLevel(optLevel),
			FPContract: fpContract,
		}
		if cerr := llvmapi.CompileToObject(capiOpts, objPath); cerr != nil {
			return fmt.Errorf("in-process LLVM C API compile failed (disable with %s=0): %w", llvmapi.EnvSwitch, cerr)
		}
		capiObject = objPath
		if verbose {
			vprintf(sink, "Compiled in-process via LLVM C API: %s\n", objPath)
		}
	}

	var sPath string
	if !singleShot {
		// ---- verbose：保留 opt + llc，供檢視中間產物 ----
		if verbose {
			vprintf(sink, "Generated LLVM IR: %s\n", llPath)
		}
		optPath := filepath.Join(tempDir, fileName+"_opt.ll")
		optCmd := exec.Command("opt", optLevel, llPath, "-S", "-o", optPath)
		if sink != nil {
			optCmd.Stdout = sink
			optCmd.Stderr = sink
		} else {
			optCmd.Stdout = os.Stdout
			optCmd.Stderr = os.Stderr
		}
		if verbose {
			vprintf(sink, "Running: opt %s %s -o %s\n", optLevel, llPath, optPath)
		}
		if err := optCmd.Run(); err != nil {
			// 區分 "opt 不存在" 和 "IR 錯誤"
			if _, lookErr := exec.LookPath("opt"); lookErr != nil {
				if verbose {
					vprintf(sink, "opt not available, using unoptimized IR\n")
				}
				optPath = llPath
			} else {
				return fmt.Errorf("LLVM optimization failed: %w", err)
			}
		}
		if optPath != llPath && verbose {
			raw, _ := os.ReadFile(optPath)
			os.WriteFile(outPath+"_opt.ll", raw, 0644)
		}
		llPath = optPath

		sPath = filepath.Join(tempDir, fileName+".s")
		if !isWasiTarget {
			llcArgs := []string{"--fp-contract=" + fpContract, llPath, "-o", sPath}
			if target != "" {
				llcArgs = append([]string{"-mtriple=" + target}, llcArgs...)
			}
			llcCmd := exec.Command("llc", llcArgs...)
			if sink != nil {
				llcCmd.Stdout = sink
				llcCmd.Stderr = sink
			} else {
				llcCmd.Stdout = os.Stdout
				llcCmd.Stderr = os.Stderr
			}
			if verbose {
				vprintf(sink, "Running: llc %s -o %s\n", llPath, sPath)
				if target != "" {
					vprintf(sink, "  target: %s\n", target)
				}
			}
			if err = llcCmd.Run(); err != nil {
				return fmt.Errorf("LLVM assembly failed: %w", err)
			}
		} else if verbose {
			vprintf(sink, "Skipping llc for WASI target (clang reads .ll directly)\n")
		}
	}

	// ---- clang / zig cc：組譯 + 連結 ----
	// 單進程路徑（非 verbose）：clang 直接吃 .ll（需 -x ir + 優化等級 + fp-contract）。
	// verbose 路徑：非 WASI 吃 llc 產出的 .s；WASI 仍跳 llc、吃 .ll。
	var clangIn string
	if capiObject != "" {
		// LLVM C API 已產出 .o，cc 只需連結。
		clangIn = capiObject
	} else if singleShot {
		clangIn = llPath
	} else if isWasiTarget {
		clangIn = llPath
	} else {
		clangIn = sPath
	}
	var clangArgs []string
	// zig 的 Apple OS 名是 "macos" 而非 LLVM 的 "darwin"：用 apple-darwin 作
	// --target 連結已編譯好的 object 會報
	// "unable to parse target query '...': UnknownOperatingSystem"。故 zig 時
	// 把 darwin triple 轉成 <arch>-macos；clang 原生接受 apple-darwin，不轉。
	linkTarget := target
	if cc == "zig" {
		linkTarget = zigTargetTriple(target)
	}
	if isWasiTarget {
		// WASI 目標：跳過所有原生平台 -l<lib> 連結（包含 -lws2_32 等 Windows 專屬函式庫）。
		// zig cc 自帶 wasi-libc，無需 $WASI_SYSROOT；clang 則需要 wasi-sysroot。
		// 直接傳 .ll 給 cc（跳過 llc），避免 WASM 組譯器不支援引號識別符。
		clangArgs = append(clangArgs, "--target="+linkTarget)
		if cc != "zig" {
			sysroot := os.Getenv("WASI_SYSROOT")
			if sysroot == "" {
				return fmt.Errorf(`WASI sysroot not found. Set $WASI_SYSROOT to point to your wasi-sysroot.
  macOS: brew install wasi-libc  (then export WASI_SYSROOT=$(brew --prefix wasi-libc)/share/wasi-sysroot)
  Ubuntu: download from https://github.com/WebAssembly/wasi-sdk/releases and extract
  Or build from source: git clone https://github.com/WebAssembly/wasi-libc && cd wasi-libc && make
  Alternatively, use Zig: no build -cc zig -target wasm32-wasi ...
  Then: export WASI_SYSROOT=/path/to/wasi-sysroot`)
			}
			clangArgs = append(clangArgs, "--sysroot="+sysroot)
		}
		clangArgs = append(clangArgs, clangIn, "-o", outPath)
	} else {
		if target != "" {
			clangArgs = append(clangArgs, "--target="+linkTarget)
		}
		isWindowsTarget := runtime.GOOS == "windows"
		if !isWindowsTarget {
			tGoos, _ := parseTargetPlatform(target)
			isWindowsTarget = tGoos == "windows"
		}
		clangArgs = append(clangArgs, clangIn, "-o", outPath)
		// Linux 發行版預設以 PIE 連結；本後端經 llc 產出的物件使用
		// R_X86_64_32 絕對重定位（如 .rodata 中的常數查表），無法連結成
		// PIE 可執行檔。對 Linux 目標改用 -no-pie 產生非 PIE 二進位。
		// macOS / Windows 不適用此旗標（macOS 會報 unsupported option）。
		tGoos, _ := parseTargetPlatform(target)
		if tGoos == "" {
			tGoos = runtime.GOOS
		}
		if tGoos == "linux" {
			clangArgs = append(clangArgs, "-no-pie")
		}
		for _, lib := range linkLibs {
			clangArgs = append(clangArgs, "-l"+lib)
		}
		// Windows 平台需要連結 ws2_32（Winsock，供 net-* 內建使用）。
		if isWindowsTarget {
			clangArgs = append(clangArgs, "-lws2_32")
		}
	}
	if singleShot && capiObject == "" {
		// 單進程：在內部做優化 + 程式碼生成 + 連結。
		// clang 用 -x ir 標註輸入是 LLVM IR；zig 不認 -x ir（報
		// "language not recognized: \"ir\""），但能透過 .ll 副檔名自動識別 IR
		// （見 TestBuildLLVMInternalZigDarwinLinksSingleShot），故 zig 不加 -x ir，
		// 僅保留 -O2 / -ffp-contract 優化參數。
		// （走 LLVM C API 時 optimize + codegen 已在本進程完成，此處只連結 .o，不會走到這裡。）
		if cc == "zig" {
			clangArgs = append([]string{optLevel, "-ffp-contract=" + fpContract}, clangArgs...)
		} else {
			clangArgs = append([]string{optLevel, "-ffp-contract=" + fpContract, "-x", "ir"}, clangArgs...)
		}
	}
	var clangCmd *exec.Cmd
	if cc == "zig" {
		clangArgs = append([]string{"cc"}, clangArgs...)
		clangCmd = exec.Command("zig", clangArgs...)
	} else {
		clangCmd = exec.Command(cc, clangArgs...)
	}
	if sink != nil {
		clangCmd.Stdout = sink
		clangCmd.Stderr = sink
	} else {
		clangCmd.Stdout = os.Stdout
		clangCmd.Stderr = os.Stderr
	}
	if verbose {
		cmdStr := cc
		if cc == "zig" {
			cmdStr = "zig cc"
		}
		vprintf(sink, "Running: %s %s -o %s\n", cmdStr, clangIn, outPath)
	}
	if err = clangCmd.Run(); err != nil {
		return fmt.Errorf("linking failed: %w", err)
	}

	// Ensure the output binary has the executable bit.
	// clang/zig normally produce 0755, but some environments (cross-compilation,
	// unusual umask, copied artifacts) may strip the +x bit. Set it explicitly.
	if err = os.Chmod(outPath, 0755); err != nil {
		vprintf(sink, "warning: chmod 0755 %s failed: %v\n", outPath, err)
	}

	return nil
}

// BuildLLVM is the public API for standalone LLVM compilation.
// Kept for backward compatibility.
func BuildLLVM(code string, fileName string, outPath string, cc string, target string, verbose bool, linkLibs []string) error {
	return buildLLVMInternal(code, fileName, outPath, cc, target, verbose, linkLibs, nil)
}

// BuildDirectWasm 使用 Direct WASM 後端從 Nolang 源碼直接產生 WebAssembly 二進制。
// 當 opts.Target 為 wasm32-wasi 且 opts.UseDirectWasm 為 true 時呼叫。
//
// 流程：
//  1. 讀取源碼檔案
//  2. 以 lexer + parser 解析為 *parser.Program（重用 Transpiler.parseFile 的解析邏輯）
//  3. 由 opts.Target 解析目標平台（goos/goarch），用於平台變體過濾
//  4. 呼叫 wasm.Generator.Generate(program) 直接發射 WASM 字節碼
//  5. 回傳 WASM 二進制 bytes
//
// 此路徑不經過 LLVM 工具鏈（opt/llc/clang），適用於瀏覽器 playground（wasip1）
// 或透過 --wasm-direct 旗標觸發的原機回歸測試。
func BuildDirectWasm(inputPath string, opts BuildOptions) ([]byte, error) {
	// 1. 讀取源碼
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	// 2. 解析為 AST（與 Transpiler.parseFile 一致的最小流程；
	//    Direct WASM 後端目前不需跨文件簽名預載入）。
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return nil, fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%s: %v", inputPath, errs)
	}

	// S1：js/wasm 後端為實驗性後端，仍走降糖形態（保留 desugar pass）。
	// 主編譯管線（checker/transpiler/H0）已原生消費 SurfaceMatch，不走此處。
	p.DesugarSurfaceMatches(program)

	// Inject -ld-KEY=VALUE compile-time global constants
	injectLDFlags(program, opts.LDFlags)

	// Third-party ESM imports (# npm/…) are JS-backend only: the Direct WASM
	// backend cannot link JavaScript modules.
	if imports, err := js.ExtractESMImports(program); err != nil {
		return nil, err
	} else if len(imports) > 0 {
		return nil, fmt.Errorf("ESM (npm) imports are not supported by the Direct WASM backend; use the JS backend (no build --js)")
	}

	// 型別校驗（與 LLVM 路徑一致，避免型別錯誤延遲到執行期才炸）
	if err := validateProgram(program, inputPath); err != nil {
		return nil, err
	}

	// 3. 解析目標平台。未指定時預設為 wasi/wasm32（Direct WASM 後端的主要目標）。
	goos, goarch := parseTargetPlatform(opts.Target)
	if goos == "" {
		goos = "wasi"
	}
	if goarch == "" {
		goarch = "wasm32"
	}

	// 4. Direct WASM codegen
	g := &wasm.Generator{}
	g.SetTargetPlatform(goos, goarch)
	if opts.Verbose {
		fmt.Fprintf(os.Stderr, "[direct-wasm] target: %s-%s\n", goarch, goos)
	}
	return g.Generate(program)
}

// validateProgram runs the same four semantic-validation passes that the LLVM
// backend runs inside CompileTarget, so that the JS and Direct-WASM backends
// are held to the same type-checking standard instead of deferring every error
// to runtime. Without this, type errors the LLVM path rejects at compile time
// would only surface as opaque JS/WASM runtime failures.
func validateProgram(program *parser.Program, inputPath string) error {
	var errs []checker.ValidateResult
	errs = append(errs, checker.ValidateTypes(program)...)
	errs = append(errs, checker.ValidateFuncArgs(program, filepath.Dir(inputPath))...)
	errs = append(errs, checker.ValidateUndefinedVars(program, filepath.Dir(inputPath), inputPath)...)
	errs = append(errs, checker.ValidateUninitOutputParams(program)...)
	if len(errs) == 0 {
		return nil
	}
	var b strings.Builder
	for _, e := range errs {
		fmt.Fprintf(&b, "  %s:%d:%d: %s [%s]\n", filepath.Base(inputPath), e.Line, e.Column, e.Message, e.TraceID)
	}
	return fmt.Errorf("%s: validation failed:\n%s", inputPath, b.String())
}

// LoadWorkspaceFile reads and parses a standalone workspace.jsonc file from the given directory.
// Returns a map of project name -> relative path.
// Returns nil, nil if workspace.jsonc does not exist (it is optional).
func LoadWorkspaceFile(dir string) (map[string]string, error) {
	wsFile := filepath.Join(dir, "workspace.jsonc")
	raw, err := os.ReadFile(wsFile)
	if err != nil {
		return nil, nil // workspace.jsonc is optional
	}

	cleaned := stripJSONC(raw)
	var ws map[string]string
	if err := json.Unmarshal(cleaned, &ws); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", wsFile, err)
	}
	return ws, nil
}

// BuildWorkspace reads workspace.jsonc from workspaceDir and builds all listed
// projects in parallel. For each project, it builds main.no (if exists), lib.no
// (if exists), and all .no files in tests/ (if exists).
// At least one of main.no or lib.no must exist; otherwise the project is skipped.
// The -o (Output) option is ignored in workspace mode; each target outputs to
// its own dist/ directory.
func BuildWorkspace(workspaceDir string, opts BuildOptions) error {
	// Toolchain check once for all projects
	if err := CheckToolchain(opts.CC); err != nil {
		return err
	}

	ws, err := LoadWorkspaceFile(workspaceDir)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace.jsonc not found in %s", workspaceDir)
	}
	if len(ws) == 0 {
		return fmt.Errorf("workspace.jsonc is empty")
	}

	type buildTarget struct {
		projectName string
		filePath    string
		pkg         *Package // 已載入的 Package，避免重複載入
	}

	type buildResult struct {
		target buildTarget
		err    error
	}

	var targets []buildTarget

	for name, relPath := range ws {
		projectDir := filepath.Join(workspaceDir, relPath)

		// Load package once per project (cache workspace map, resolve deps)
		pkg, _ := LoadPackage(projectDir)
		if pkg != nil {
			if _, err := pkg.EnsureDependencies(10); err != nil {
				fmt.Fprintf(os.Stderr, "SKIP: %s: dependency resolution failed: %v\n", name, err)
				continue
			}
		}

		mainFile := "main.no"
		if pkg != nil && pkg.Main != "" {
			mainFile = pkg.Main
		}

		mainPath := filepath.Join(projectDir, mainFile)
		libPath := filepath.Join(projectDir, "lib.no")
		testDir := filepath.Join(projectDir, "tests")

		hasMain := false
		hasLib := false

		// Check main.no
		if _, err := os.Stat(mainPath); err == nil {
			if pkg == nil || !pkg.IsIgnored(mainPath) {
				targets = append(targets, buildTarget{projectName: name, filePath: mainPath, pkg: pkg})
				hasMain = true
			} else {
				fmt.Fprintf(os.Stderr, "SKIP (ignored): %s: %s\n", name, mainPath)
				hasMain = true
			}
		}

		// Check lib.no
		if _, err := os.Stat(libPath); err == nil {
			if pkg == nil || !pkg.IsIgnored(libPath) {
				targets = append(targets, buildTarget{projectName: name, filePath: libPath, pkg: pkg})
				hasLib = true
			} else {
				fmt.Fprintf(os.Stderr, "SKIP (ignored): %s: %s\n", name, libPath)
				hasLib = true
			}
		}

		// At least one of main.no or lib.no must exist
		if !hasMain && !hasLib {
			fmt.Fprintf(os.Stderr, "SKIP: %s: no main.no or lib.no found in %s\n", name, projectDir)
			continue
		}

		// Scan tests/ directory for .no files (excluding main.no and lib.no)
		if info, err := os.Stat(testDir); err == nil && info.IsDir() {
			_ = filepath.WalkDir(testDir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				fname := d.Name()
				if !strings.HasSuffix(fname, ".no") {
					return nil
				}
				if fname == "main.no" || fname == "lib.no" {
					return nil
				}
				// 跳過 ignore 列表中匹配的檔案
				if pkg != nil && pkg.IsIgnored(path) {
					return nil
				}
				targets = append(targets, buildTarget{projectName: name, filePath: path, pkg: pkg})
				return nil
			})
		}
	}

	if len(targets) == 0 {
		return fmt.Errorf("no build targets found in workspace")
	}

	// Build all targets in parallel, with concurrency limited to NumCPU
	// to avoid spawning too many LLVM subprocess (opt/llc/cc) under load.
	// Each target spawns up to 3 subprocesses; without a semaphore, 50 targets
	// would start 150 LLVM subprocesses simultaneously, causing memory/CPU thrash.
	var wg sync.WaitGroup
	results := make(chan buildResult, len(targets))
	// semaphore: 緩衝 channel 作為信號量，容量為並發度上限。
	// 預設為 runtime.NumCPU()，每個 worker 啟動前 acquire（寫入），完成後 release（讀出）。
	concurrency := runtime.NumCPU()
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	for _, t := range targets {
		wg.Add(1)
		go func(t buildTarget) {
			defer wg.Done()
			// Acquire semaphore slot (blocks if at concurrency limit)
			sem <- struct{}{}
			defer func() { <-sem }()

			projectOpts := opts
			projectOpts.Output = "" // use default per-project output

		fmt.Fprintf(os.Stderr, "Building %s: %s\n", t.projectName, t.filePath)
		_, err := buildWithPkg(t.filePath, t.pkg, projectOpts, true)
			results <- buildResult{target: t, err: err}
		}(t)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var hasError bool
	for r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s: %s: %v\n", r.target.projectName, r.target.filePath, r.err)
			hasError = true
		} else {
			fmt.Fprintf(os.Stderr, "OK:   %s: %s\n", r.target.projectName, r.target.filePath)
		}
	}

	if hasError {
		return fmt.Errorf("one or more workspace targets failed to build")
	}
	return nil
}
