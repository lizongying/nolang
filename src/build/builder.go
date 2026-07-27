package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/lizongying/nolang/build/js"
	"github.com/lizongying/nolang/build/wasm"
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
	var llvmOS string
	switch osName {
	case "linux":
		llvmOS = "linux-gnu"
	case "darwin":
		llvmOS = "macos-gnu"
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
	CC            string // C compiler: "clang" or "zig"
	Target        string // target triple (e.g. "x86_64-linux-gnu", "" = auto)
	Verbose       bool
	Output        string // optional output path ("" = auto)
	NoBoundsCheck bool   // skip bounds checks (unsafe mode, for max performance)
	UseDirectWasm bool   // use Direct WASM backend (no LLVM toolchain required)
	UseJS         bool   // use JS backend (emit JavaScript source, no LLVM toolchain)
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
		case "windows":
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

	// 若指定的是目錄，先找目錄內的 mod.jsonc
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
	// 若指定的是目錄，先找目錄內的 mod.jsonc
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
	_, err = compiler.Compile(string(source))
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	return nil
}

// buildWithPkg 是核心編譯邏輯，使用已載入的 Package（可為 nil）。
// BuildFile 和 BuildWorkspace 共用此函數，避免重複載入 Package。
func buildWithPkg(inputPath string, pkg *Package, opts BuildOptions, buffered bool) error {
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
	}

	compiler := NewTranspiler(pkg)
	compiler.sourcePath = inputPath
	goos, goarch := parseTargetPlatform(opts.Target)
	compiler.SetTargetPlatform(goos, goarch)
	compiler.SetNoBoundsCheck(opts.NoBoundsCheck)
	code, err := compiler.Compile(string(source))
	if err != nil {
		return fmt.Errorf("compilation error: %w", err)
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

	var linkLibs []string
	if pkg != nil {
		linkLibs = pkg.Compiler.LinkLibs
	}

	// 並行構建時使用緩衝區避免輸出交錯
	var sink *bytes.Buffer
	if buffered {
		sink = &bytes.Buffer{}
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
	optCmd := exec.Command("opt", "-O3", llPath, "-S", "-o", optPath)
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
		clangArgs = append(clangArgs, sPath, "-o", outPath)
		for _, lib := range linkLibs {
			clangArgs = append(clangArgs, "-l"+lib)
		}
		// Windows 平台需要連結 ws2_32（Winsock，供 net-* 內建使用）。
		// 無棧協程不需要 pthread，事件循環運行時由 src/runtime/async_runtime.c 提供。
		if runtime.GOOS == "windows" || strings.Contains(target, "windows") {
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
	l := lexer.New(string(source))
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

// BuildJS 使用 JS 後端從 Nolang 源碼直接產生 JavaScript 原始碼字串。
// 當 opts.UseJS 為 true 時呼叫。輸出的 JS 可由 node 直接執行。
//
// 流程：
//  1. 讀取源碼檔案
//  2. 以 lexer + parser 解析為 *parser.Program
//  3. 由 js.Generator.Generate(program) 直接發射 JS 原始碼（型別擦除）
//  4. 回傳 JS 原始碼字串
//
// 此路徑不經過 LLVM 工具鏈（opt/llc/clang），適用於 Node.js 與瀏覽器環境。
func BuildJS(inputPath string, opts BuildOptions) (string, error) {
	// 1. 讀取源碼
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return "", fmt.Errorf("reading input file: %w", err)
	}

	// 2. 解析為 AST（與 BuildDirectWasm 一致的最小流程）
	l := lexer.New(string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(inputPath)
	program := p.ParseProgram()
	if program == nil {
		return "", fmt.Errorf("%s: parser returned nil program", inputPath)
	}
	if errs := p.Errors(); len(errs) > 0 {
		return "", fmt.Errorf("%s: %v", inputPath, errs)
	}

	// 3. JS 後端 codegen（型別擦除）
	g := js.NewGenerator()
	g.SetTargetPlatform("js", "")
	if opts.Verbose {
		fmt.Fprintf(os.Stderr, "[js] target: js (type erasure)\n")
	}
	return g.Generate(program)
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
			targets = append(targets, buildTarget{projectName: name, filePath: mainPath, pkg: pkg})
			hasMain = true
		}

		// Check lib.no
		if _, err := os.Stat(libPath); err == nil {
			targets = append(targets, buildTarget{projectName: name, filePath: libPath, pkg: pkg})
			hasLib = true
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
				targets = append(targets, buildTarget{projectName: name, filePath: path, pkg: pkg})
				return nil
			})
		}
	}

	if len(targets) == 0 {
		return fmt.Errorf("no build targets found in workspace")
	}

	// Build all targets in parallel
	var wg sync.WaitGroup
	results := make(chan buildResult, len(targets))

	for _, t := range targets {
		wg.Add(1)
		go func(t buildTarget) {
			defer wg.Done()
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
