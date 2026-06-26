package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

// BuildFile compiles a .no source file and produces the output binary/file.
func BuildFile(inputPath string, opts BuildOptions) error {
	// 工具鏈檢查
	if err := CheckToolchain(opts.CC); err != nil {
		return err
	}

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
		// 確保所有傳遞依賴已解析
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
	code, err := compiler.Compile(string(source))
	if err != nil {
		return fmt.Errorf("compilation error: %w", err)
	}

	fileName := strings.TrimSuffix(filepath.Base(inputPath), ".no")

	var outPath string
	if opts.Output != "" {
		outPath = opts.Output
	} else {
		rootDir := "."
		if pkg != nil {
			rootDir = pkg.RootDir
		}
		distDir := filepath.Join(rootDir, "dist")
		if err = os.MkdirAll(distDir, 0755); err != nil {
			return fmt.Errorf("creating dist directory: %w", err)
		}
		outPath = filepath.Join(distDir, fileName)
	}

	err = BuildLLVM(code, fileName, outPath, opts.CC, opts.Target, opts.Verbose)
	if err != nil {
		return fmt.Errorf("build error: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Build successful: %s\n", outPath)
	}

	return nil
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
		// 確保所有傳遞依賴已解析
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
	compiler.sourcePath = inputPath  // 設定源碼路徑用於 std 庫檢測
	// Compile 會進行解析、型別檢查和 LLVM IR 產生
	// 我們只關心是否有錯誤，丟棄產生的 LLVM IR
	_, err = compiler.Compile(string(source))
	if err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	return nil
}

// BuildLLVM writes LLVM IR and compiles it to an executable via opt + llc + cc.
func BuildLLVM(code string, fileName string, outPath string, cc string, target string, verbose bool) error {
	tempDir, err := os.MkdirTemp("", "nolang")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	llPath := filepath.Join(tempDir, fileName+".ll")
	err = os.WriteFile(llPath, []byte(code), 0644)
	if err != nil {
		return fmt.Errorf("writing LLVM IR file: %w", err)
	}

	// 保留 .ll 供分析（確保目錄存在）
	llOut := outPath + ".ll"
	if err := os.MkdirAll(filepath.Dir(llOut), 0755); err == nil {
		os.WriteFile(llOut, []byte(code), 0644)
	}

	// opt -O2 最佳化
	if verbose {
		fmt.Printf("Generated LLVM IR: %s\n", llPath)
	}
	optPath := filepath.Join(tempDir, fileName+"_opt.ll")
	optCmd := exec.Command("opt", "-O2", llPath, "-S", "-o", optPath)
	optCmd.Stdout = os.Stdout
	optCmd.Stderr = os.Stderr
	if verbose {
		fmt.Printf("Running: opt -O2 %s -o %s\n", llPath, optPath)
	}
	if err := optCmd.Run(); err != nil {
		if verbose {
			fmt.Printf("opt not available, using unoptimized IR: %v\n", err)
		}
		optPath = llPath
	}
	if optPath != llPath {
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
	llcCmd.Stdout = os.Stdout
	llcCmd.Stderr = os.Stderr
	if verbose {
		fmt.Printf("Running: llc %s -o %s\n", llPath, sPath)
		if target != "" {
			fmt.Printf("  target: %s\n", target)
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
	var clangCmd *exec.Cmd
	if cc == "zig" {
		clangArgs = append([]string{"cc"}, clangArgs...)
		clangCmd = exec.Command("zig", clangArgs...)
	} else {
		clangCmd = exec.Command(cc, clangArgs...)
	}
	clangCmd.Stdout = os.Stdout
	clangCmd.Stderr = os.Stderr
	if verbose {
		cmdStr := cc
		if cc == "zig" {
			cmdStr = "zig cc"
		}
		fmt.Printf("Running: %s %s -o %s\n", cmdStr, sPath, outPath)
	}
	if err = clangCmd.Run(); err != nil {
		return fmt.Errorf("linking failed: %w", err)
	}

	return nil
}
