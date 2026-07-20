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
	CC      string // C compiler: "clang" or "zig"
	Target  string // target triple (e.g. "x86_64-linux-gnu", "" = auto)
	Verbose bool
	Output  string // optional output path ("" = auto)
}

// parseTargetPlatform extracts (goos, goarch) from a target triple.
// Returns ("", "") when target is empty, signaling the caller to fall back
// to the host runtime platform.
//
// Recognized architectures (first hyphen-separated component):
//   - x86_64    → amd64
//   - aarch64   → arm64
//
// Recognized operating systems (any subsequent component):
//   - linux     → linux
//   - windows   → windows
//   - macos     → darwin
//   - darwin    → darwin
//
// Examples:
//   - "x86_64-linux-gnu"      → ("linux", "amd64")
//   - "aarch64-linux-gnu"     → ("linux", "arm64")
//   - "x86_64-windows-gnu"    → ("windows", "amd64")
//   - "aarch64-darwin"        → ("darwin", "arm64")
//   - "x86_64-apple-macos"    → ("darwin", "amd64")
//   - ""                      → ("", "")
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
	}
	for _, p := range parts[1:] {
		switch p {
		case "linux":
			goos = "linux"
		case "windows":
			goos = "windows"
		case "macos", "darwin", "apple":
			goos = "darwin"
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
	sPath := filepath.Join(tempDir, fileName+".s")
	llcArgs := []string{llPath, "-o", sPath}
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

	// cc → executable (assemble + link)
	var clangArgs []string
	if target != "" {
		clangArgs = append(clangArgs, "--target="+target)
	}
	clangArgs = append(clangArgs, sPath, "-o", outPath)
	for _, lib := range linkLibs {
		clangArgs = append(clangArgs, "-l"+lib)
	}
	// Windows 平台需要連結 ws2_32（Winsock，供 net-* 內建使用）與
	// winpthread（pthread，供 run/awy async 使用）。涵蓋原生 Windows 編譯
	// 與從其他平台交叉編譯到 Windows 兩種情境。
	if runtime.GOOS == "windows" || strings.Contains(target, "windows") {
		clangArgs = append(clangArgs, "-lws2_32", "-lwinpthread")
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
