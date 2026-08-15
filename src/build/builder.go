package build

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/build/wasm"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

//go:embed runtime
var processRuntimeC embed.FS

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

// CheckToolchain verifies that LLVM toolchain (llvm-config) and the chosen
// C compiler (clang or zig) are available.
// Returns an error with OS-specific install instructions if not found.
func CheckToolchain(cc string) error {
	// WASM 建構下無法呼叫本機 LLVM 工具鏈（opt/llc/clang）；
	// 應改用 --wasm-direct 直接產生 WASM 的路徑。
	if runtime.GOOS == "wasip1" {
		return fmt.Errorf("LLVM toolchain not available in WASM build; use --wasm-direct flag")
	}
	if _, err := exec.LookPath("llvm-config"); err != nil {
		return fmt.Errorf(`未檢測到 LLVM 工具鏈
  macOS: brew install llvm
  Ubuntu: sudo apt install llvm-dev clang
  Windows: winget install LLVM.LLVM`)
	}

	// 讀取版本僅用於顯示，不強制特定版本
	out, _ := exec.Command("llvm-config", "--version").Output()
	if verStr := strings.TrimSpace(string(out)); verStr != "" {
		fmt.Fprintf(os.Stderr, "llvm-config version: %s\n", verStr)
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
	CC              string // C compiler: "clang" or "zig"
	Target          string // target triple (e.g. "x86_64-linux-gnu", "" = auto)
	Verbose         bool
	Output          string // optional output path ("" = auto)
	NoBoundsCheck   bool   // skip bounds checks (unsafe mode, for max performance)
	UseDirectWasm   bool   // use Direct WASM backend (no LLVM toolchain required)
	UseJS           bool   // use JS backend (emit JavaScript source, no LLVM toolchain)
	BrowserMode     bool   // when true with UseJS, generate browser-targeted JS + HTML wrapper
	CompilerVersion string // current compiler version (for package.jsonc compatibility check)
}

// versionCompatible 檢查 package.jsonc 中聲明的編譯器版本是否與當前編譯器版本兼容。
// 採用語義化版本規則：
//   - major >= 1 時，major 版本相同即視為兼容（1.2.0 與 1.3.1 兼容）
//   - major == 0 時（初始開發階段），API 不穩定，以 minor 作為兼容性判斷依據
//     （0.1.0 與 0.1.5 兼容，但 0.1.0 與 0.2.0 不兼容）
//   - 若無法解析為 semver，則要求精確匹配
func versionCompatible(required, current string) bool {
	required = strings.TrimSpace(required)
	current = strings.TrimSpace(current)
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
//                        保守地將 unknown wasm 目標視為 wasi)
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
func BuildFile(inputPath string, opts BuildOptions) error {
	// 工具鏈檢查
	if err := CheckToolchain(opts.CC); err != nil {
		return err
	}

	// 若指定的是目錄，先找目錄內的 package.jsonc
	info, err := os.Stat(inputPath)
	isDir := err == nil && info.IsDir()

	var pkg *Package
	if isDir {
		pkg, _ = LoadPackage(inputPath)
		if pkg != nil {
			if _, err := pkg.EnsureDependencies(10); err != nil {
				return fmt.Errorf("dependency resolution failed: %w", err)
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
			return fmt.Errorf("main.no not found in %s", inputPath)
		}
		inputPath = mainPath
	}

	// 若尚未載入 package（非目錄模式），從檔案目錄載入
	if pkg == nil {
		pkgDir := filepath.Dir(inputPath)
		pkg, _ = LoadPackage(pkgDir)
		if pkg != nil {
			if _, err := pkg.EnsureDependencies(10); err != nil {
				return fmt.Errorf("dependency resolution failed: %w", err)
			}
		}
	}

	return buildWithPkg(inputPath, pkg, opts, false)
}

// VetFile performs syntax and semantic validation on a .no source file without
// producing any compilation artifacts (no LLVM IR, no binary).
func VetFile(inputPath string, opts BuildOptions) error {
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
			return fmt.Errorf("dependency resolution failed: %w", err)
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
			return fmt.Errorf("main.no not found in %s", inputPath)
		}
		inputPath = mainPath
	}

	source, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
	}

	compiler := NewTranspiler(pkg)
	compiler.sourcePath = inputPath
	goos, goarch := parseTargetPlatform(opts.Target)
	compiler.SetTargetPlatform(goos, goarch)
	// vet 模式：跳過 LLVM IR 生成，只做前端驗證（語法+型別+模組合併+單態化）
	compiler.SetVetMode(true)
	_, err = compiler.Compile(string(source))
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	return nil
}

// buildWithPkg 是核心編譯邏輯，使用已載入的 Package（可為 nil）。
// BuildFile 和 BuildWorkspace 共用此函數，避免重複載入 Package。
func buildWithPkg(inputPath string, pkg *Package, opts BuildOptions, buffered bool) error {
	// 檢查 package.jsonc 中聲明的編譯器版本是否與當前編譯器版本匹配
	if pkg != nil && pkg.Compiler.Version != "" && opts.CompilerVersion != "" {
		if !versionCompatible(pkg.Compiler.Version, opts.CompilerVersion) {
			msg := fmt.Sprintf("warning: compiler version mismatch: package.jsonc requires %q, current compiler is %q\n",
				pkg.Compiler.Version, opts.CompilerVersion)
			if buffered {
				fmt.Fprint(os.Stderr, msg)
			} else {
				fmt.Fprint(os.Stderr, msg)
			}
		}
	}

	source, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
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
			return fmt.Errorf("creating dist directory: %w", err)
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
		return buildJSPkg(source, inputPath, pkg, opts, outPath, sink)
	}

	// LLVM 路徑
	compiler := NewTranspiler(pkg)
	compiler.sourcePath = inputPath
	goos, goarch := parseTargetPlatform(opts.Target)
	compiler.SetTargetPlatform(goos, goarch)
	compiler.SetNoBoundsCheck(opts.NoBoundsCheck)
	code, err := compiler.Compile(string(source))
	if err != nil {
		return fmt.Errorf("compilation error: %w", err)
	}

	var linkLibs []string
	if pkg != nil {
		linkLibs = pkg.Compiler.LinkLibs
	}

	err = buildLLVMInternal(code, fileName, outPath, opts.CC, opts.Target, opts.Verbose, linkLibs, sink)
	if err != nil {
		if sink != nil && sink.Len() > 0 {
			fmt.Fprint(os.Stderr, sink.String())
		}
		return fmt.Errorf("build error: %w", err)
	}

	if sink != nil && sink.Len() > 0 {
		fmt.Fprint(os.Stderr, sink.String())
	}

	if opts.Verbose {
		fmt.Printf("Build successful: %s\n", outPath)
	}

	return nil
}

// vprintf 輸出 verbose 訊息到 sink（若 nil 則寫 os.Stderr）
func vprintf(sink *bytes.Buffer, format string, args ...interface{}) {
	if sink != nil {
		fmt.Fprintf(sink, format, args...)
	} else {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// buildLLVMInternal writes LLVM IR and compiles it to an executable via opt + llc + cc.
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
	if os.Getenv("NOLANG_KEEP_IR") == "" {
		defer os.RemoveAll(tempDir)
	} else {
		fmt.Fprintf(os.Stderr, "[debug] keep build tmp dir: %s\n", tempDir)
	}

	llPath := filepath.Join(tempDir, fileName+".ll")
	err = os.WriteFile(llPath, []byte(code), 0644)
	if err != nil {
		return fmt.Errorf("writing LLVM IR file: %w", err)
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

	// opt -O3 最佳化
	if verbose {
		vprintf(sink, "Generated LLVM IR: %s\n", llPath)
	}
	optPath := filepath.Join(tempDir, fileName+"_opt.ll")
	optLevel := os.Getenv("NOLANG_OPT_LEVEL")
	if optLevel == "" {
		optLevel = "-O3"
	}
	optCmd := exec.Command("opt", optLevel, llPath, "-S", "-o", optPath)
	if sink != nil {
		optCmd.Stdout = sink
		optCmd.Stderr = sink
	} else {
		optCmd.Stdout = os.Stdout
		optCmd.Stderr = os.Stderr
	}
	if verbose {
		vprintf(sink, "Running: opt -O3 %s -o %s\n", llPath, optPath)
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

	// llc → .s (assembly)
	// --fp-contract=fast enables FMA synthesis (e.g. fmul+fadd → fmadd).
	// This matches the default behavior of gcc -O2 / clang -O2 on C code,
	// where FP_CONTRACT is permitted by the standard. Only the last bit of
	// rounding may differ, which is acceptable for Nolang programs and
	// yields significant performance gains on FP-intensive workloads
	// (e.g. mandelbrot inner loop: fmul+fadd → single fmadd instruction).
	//
	// WASI 目標跳過 llc：clang/zig cc 可直接編譯 LLVM IR，且 WASM 組譯器
	// 不支援 llc 生成的引號識別符（如 @"HEX-UPPER"），直接傳 .ll 給 cc 避免
	// 組譯階段的符號解析問題。
	sPath := filepath.Join(tempDir, fileName+".s")
	if !isWasiTarget {
		llcArgs := []string{"--fp-contract=fast", llPath, "-o", sPath}
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

	// cc → executable (assemble + link)
	var clangArgs []string
	if isWasiTarget {
		// WASI 目標：跳過所有原生平台 -l<lib> 連結（包含 -lws2_32 等 Windows 專屬函式庫）。
		// zig cc 自帶 wasi-libc，無需 $WASI_SYSROOT；clang 則需要 wasi-sysroot。
		// 直接傳 .ll 給 cc（跳過 llc），避免 WASM 組譯器不支援引號識別符。
		clangArgs = append(clangArgs, "--target="+target)
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
		clangArgs = append(clangArgs, llPath, "-o", outPath)
	} else {
		if target != "" {
			clangArgs = append(clangArgs, "--target="+target)
		}
		isWindowsTarget := runtime.GOOS == "windows"
		if !isWindowsTarget {
			// Also check the target triple for Windows/Mingw components.
			tGoos, _ := parseTargetPlatform(target)
			isWindowsTarget = tGoos == "windows"
		}
		// Link the cross-platform process runtime (provides @nolang.process_run).
		// Read from the embedded FS and written next to the generated
		// assembly so clang compiles+links it.
		// Windows no longer needs the C runtime — process.cmd is implemented
		// in pure Nolang using Win32 API ForwardFunc builtins (CreateProcessA,
		// CreatePipe, etc.). The C runtime is only linked for POSIX targets.
		if !isWindowsTarget {
			procCPath := filepath.Join(tempDir, "process_runtime.c")
			cBytes, rErr := processRuntimeC.ReadFile("runtime/process.c")
			if rErr != nil {
				return fmt.Errorf("read process runtime C: %w", rErr)
			}
			if wErr := os.WriteFile(procCPath, cBytes, 0644); wErr != nil {
				return fmt.Errorf("write process runtime C: %w", wErr)
			}
			clangArgs = append(clangArgs, procCPath)
		}
		clangArgs = append(clangArgs, sPath, "-o", outPath)
		for _, lib := range linkLibs {
			clangArgs = append(clangArgs, "-l"+lib)
		}
		// Windows 平台需要連結 ws2_32（Winsock，供 net-* 內建使用）。
		// 無棧協程不需要 pthread，事件循環運行時由 src/runtime/async_runtime.c 提供。
		if isWindowsTarget {
			clangArgs = append(clangArgs, "-lws2_32")
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
		vprintf(sink, "Running: %s %s -o %s\n", cmdStr, sPath, outPath)
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

// buildJSPkg 使用 JS 後端從 Nolang 源碼發射 JavaScript 並寫入輸出檔案。
// 當 package.jsonc 的 compiler.emit 為 "js" 時由 buildWithPkg 呼叫，
// 使 workspace 模式下的單個 JS 目標也能自動走 JS 後端。
func buildJSPkg(source []byte, inputPath string, pkg *Package, opts BuildOptions, outPath string, sink *bytes.Buffer) error {
	// 輸出路徑加上 .js 副檔名
	if !strings.HasSuffix(outPath, ".js") {
		outPath = outPath + ".js"
	}

	// 解析為 AST
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return fmt.Errorf("%s: %v", inputPath, errs)
	}

	// 解析並合併本地模組導入
	program, err := resolveAndMergeJSModules(program, inputPath, pkg)
	if err != nil {
		return fmt.Errorf("module resolution: %w", err)
	}

	// JS 後端 codegen（型別擦除）
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	envMode := "node"
	if opts.BrowserMode {
		g.SetTargetEnv("browser")
		envMode = "browser"
	}
	if opts.Verbose {
		vprintf(sink, "[js] emit: js (type erasure), env: %s\n", envMode)
	}
	jsCode, err := g.Generate(program)
	if err != nil {
		return fmt.Errorf("JS generation error: %w", err)
	}

	// 確保輸出目錄存在
	if dir := filepath.Dir(outPath); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(outPath, []byte(jsCode), 0644); err != nil {
		return fmt.Errorf("writing JS output: %w", err)
	}

	vprintf(sink, "Built: %s\n", outPath)
	return nil
}

// resolveAndMergeJSModules resolves local module imports (UseStatements with
// paths starting with "/") and merges their statements into the main program.
// This is necessary because the JS backend, unlike the LLVM backend, does not
// go through the full Transpiler.Compile pipeline that handles module merging.
//
// Without this step, global variables and functions defined in imported local
// modules (e.g. state.no, editor.no) would be missing from the generated JS,
// causing runtime ReferenceErrors (e.g. "EDITOR is not defined").
//
// js/ modules are skipped — their function calls are mapped to JS builtins by
// builtin.go. std/ modules are also skipped — common std functions are mapped
// by builtin.go, and merging full std source would bloat the output.
func resolveAndMergeJSModules(program *parser.Program, inputPath string, pkgConfig *Package) (*parser.Program, error) {
	if program == nil {
		return program, nil
	}

	// Determine workspace root for resolving "/"-prefixed import paths.
	var wsRoot string
	if pkgConfig != nil {
		wsRoot = pkgConfig.WorkspaceRoot()
		if wsRoot == "" {
			wsRoot = pkgConfig.RootDir
		}
	}
	if wsRoot == "" {
		if ws, ok := FindWorkspaceRoot(inputPath); ok {
			wsRoot = ws
		}
	}

	// Collect main program's top-level variable and function names to avoid
	// duplicating them when merging module statements.
	mainVarNames := make(map[string]bool)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			mainVarNames[ls.Name.Value] = true
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			mainVarNames[fd.Name] = true
		}
	}

	// Build the merged program: start with a fresh statement list and merge
	// semantic contexts from the main program and all imported modules.
	merged := &parser.Program{Statements: []parser.Statement{}}
	merged.Sem = parser.NewSemanticContext()
	merged.Sem.Merge(program.Sem)

	loadedModules := make(map[string]bool) // track resolved file paths to avoid duplicate loading

	// parseModuleFile reads and parses a .no file, applying lib.no export
	// filtering if the module belongs to a package with exports.
	parseModuleFile := func(filePath string) (*parser.Program, error) {
		source, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		l := lexer.NewCached(filePath, string(source))
		p := parser.New(l)
		p.Filename = filepath.Base(filePath)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil, fmt.Errorf("%s: %v", filePath, p.Errors())
		}
		// Apply lib.no export filtering if the module's package has exports.
		pkgRoot := FindPackageRoot(filePath)
		if pkgRoot != "" {
			libPath := filepath.Join(pkgRoot, "lib.no")
			if _, err := os.Stat(libPath); err == nil {
				prog = checker.FilterByExports(prog, libPath, filePath)
			}
		}
		return prog, nil
	}

	// resolveModulePath converts a UseStatement path to a file path.
	// Only local paths (starting with "/") are handled; std/ and js/ paths
	// are skipped by the caller.
	resolveModulePath := func(usePath string) string {
		relPath := strings.TrimPrefix(usePath, "/")
		fullPath := ResolveToWorkspaceRoot(wsRoot, relPath)
		if !strings.HasSuffix(fullPath, ".no") {
			fullPath = fullPath + ".no"
		}
		return filepath.Clean(fullPath)
	}

	// processUseAndMerge loads a module from a UseStatement, merges its
	// non-UseStatement statements into merged, and collects its UseStatements
	// for transitive processing.
	var pendingUses []*parser.UseStatement
	processUseAndMerge := func(use *parser.UseStatement) error {
		// Only resolve local module imports (path starts with "/").
		// Skip js/ modules (handled by builtin.go) and std/ modules
		// (common functions handled by builtin.go).
		if !strings.HasPrefix(use.Path, "/") {
			return nil
		}

		modulePath := resolveModulePath(use.Path)
		if loadedModules[modulePath] {
			return nil
		}
		loadedModules[modulePath] = true

		modProg, err := parseModuleFile(modulePath)
		if err != nil {
			return fmt.Errorf("loading module %s: %w", use.Path, err)
		}
		merged.Sem.Merge(modProg.Sem)

		for _, ms := range modProg.Statements {
			// Collect transitive UseStatements for recursive processing.
			// Only local module imports (path starts with "/") are processed;
			// std/ and js/ modules are handled by builtin.go.
			if nestedUse, ok := ms.(*parser.UseStatement); ok {
				if strings.HasPrefix(nestedUse.Path, "/") {
					pendingUses = append(pendingUses, nestedUse)
				}
				continue
			}

			// When alias is specified, only import the specific function/constant.
			if use.Alias != "" {
				if fd, ok := ms.(*parser.FunctionDefinition); ok {
					if use.Function != "" && fd.Name == use.Function {
						fd.Name = use.Alias
						merged.Statements = append(merged.Statements, fd)
					}
				}
				if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
					if use.Function != "" && ls.Name.Value == use.Function {
						ls.Name.Value = use.Alias
						if !mainVarNames[ls.Name.Value] {
							merged.Statements = append(merged.Statements, ls)
						}
					}
				}
				continue
			}

			// No alias: merge all non-UseStatement statements.
			// Skip variables/functions that already exist in the main program.
			if fd, ok := ms.(*parser.FunctionDefinition); ok {
				if !mainVarNames[fd.Name] {
					merged.Statements = append(merged.Statements, fd)
				}
			} else if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
				if !mainVarNames[ls.Name.Value] {
					merged.Statements = append(merged.Statements, ls)
				}
			} else if sd, ok := ms.(*parser.StructDefinition); ok {
				merged.Statements = append(merged.Statements, sd)
			} else if id, ok := ms.(*parser.InterfaceDefinition); ok {
				merged.Statements = append(merged.Statements, id)
			} else if ta, ok := ms.(*parser.TypeAlias); ok {
				merged.Statements = append(merged.Statements, ta)
			} else if ed, ok := ms.(*parser.EnumDefinition); ok {
				merged.Statements = append(merged.Statements, ed)
			} else if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
				merged.Statements = append(merged.Statements, ted)
			} else if es, ok := ms.(*parser.ExternStatement); ok {
				merged.Statements = append(merged.Statements, es)
			}
		}
		return nil
	}

	// Process main program's UseStatements first, then drain transitive imports.
	for _, stmt := range program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			if err := processUseAndMerge(use); err != nil {
				return nil, err
			}
			continue
		}
		// Add main program's own statements to merged (except UseStatements).
		merged.Statements = append(merged.Statements, stmt)
	}

	// Process transitive imports: drain the pendingUses worklist.
	for len(pendingUses) > 0 {
		use := pendingUses[0]
		pendingUses = pendingUses[1:]
		if err := processUseAndMerge(use); err != nil {
			return nil, err
		}
	}

	return merged, nil
}

// BuildJS 使用 JS 後端從 Nolang 源碼直接產生 JavaScript 原始碼字串。
// 當 opts.UseJS 為 true 時呼叫。輸出的 JS 可由 node 直接執行。
//
// 流程：
//  1. 讀取源碼檔案
//  2. 以 lexer + parser 解析為 *parser.Program
//  3. 解析並合併本地模組導入（resolveAndMergeJSModules）
//  4. 由 js.Generator.Generate(program) 直接發射 JS 原始碼（型別擦除）
//  5. 回傳 JS 原始碼字串
//
// 此路徑不經過 LLVM 工具鏈（opt/llc/clang），適用於 Node.js 與瀏覽器環境。
func BuildJS(inputPath string, opts BuildOptions) (string, error) {
	// 1. 讀取源碼
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return "", fmt.Errorf("reading input file: %w", err)
	}

	// 2. 解析為 AST（與 BuildDirectWasm 一致的最小流程）
	l := lexer.NewCached(inputPath, string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return "", fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return "", fmt.Errorf("%s: %v", inputPath, errs)
	}

	// 3. 解析並合併本地模組導入
	pkgConfig, _ := LoadPackage(filepath.Dir(inputPath))
	program, err = resolveAndMergeJSModules(program, inputPath, pkgConfig)
	if err != nil {
		return "", fmt.Errorf("module resolution: %w", err)
	}

	// 4. JS 後端 codegen（型別擦除）
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	envMode := "node"
	if opts.BrowserMode {
		g.SetTargetEnv("browser")
		envMode = "browser"
	}
	if opts.Verbose {
		fmt.Fprintf(os.Stderr, "[js] target: js (type erasure), env: %s\n", envMode)
	}
	return g.Generate(program)
}

// BuildJSBrowser compiles Nolang source to browser-targeted JS and an HTML wrapper.
// Returns (jsCode, htmlCode, error). The HTML references the JS file by name (basename only).
func BuildJSBrowser(inputPath string, opts BuildOptions, jsFileName string) (jsCode, htmlCode string, err error) {
	opts.BrowserMode = true
	jsCode, err = BuildJS(inputPath, opts)
	if err != nil {
		return "", "", err
	}

	// Determine title from package name or filename
	title := strings.TrimSuffix(filepath.Base(inputPath), ".no")
	if pkg, perr := LoadPackage(filepath.Dir(inputPath)); perr == nil && pkg != nil && pkg.Name != "" {
		title = pkg.Name
	}

	htmlCode = js.RenderHTML(title, jsFileName)
	return jsCode, htmlCode, nil
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
			err := buildWithPkg(t.filePath, t.pkg, projectOpts, true)
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
