package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildOptions holds all options for a build operation.
type BuildOptions struct {
	CC      string // C compiler: "clang" or "zig"
	Verbose bool
	Output  string // optional output path ("" = auto)
}

// BuildFile compiles a .no source file and produces the output binary/file.
func BuildFile(inputPath string, opts BuildOptions) error {
	// 若指定的是目錄，先找目錄內的 nolang.jsonc
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
		mainFile := pkg.Main
		if mainFile == "" {
			mainFile = "main.no"
		}
		inputPath = pkg.ResolvePath(mainFile)
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

	err = BuildLLVM(code, fileName, outPath, opts.CC, opts.Verbose)
	if err != nil {
		return fmt.Errorf("build error: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Build successful: %s\n", outPath)
	}

	return nil
}

// BuildLLVM writes LLVM IR and compiles it to an executable via opt + llc + cc.
func BuildLLVM(code string, fileName string, outPath string, cc string, verbose bool) error {
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
	llcCmd := exec.Command("llc", llPath, "-o", sPath)
	llcCmd.Stdout = os.Stdout
	llcCmd.Stderr = os.Stderr
	if verbose {
		fmt.Printf("Running: llc %s -o %s\n", llPath, sPath)
	}
	if err = llcCmd.Run(); err != nil {
		return fmt.Errorf("LLVM assembly failed: %w", err)
	}

	// cc → executable (assemble + link)
	var clangCmd *exec.Cmd
	if cc == "zig" {
		clangCmd = exec.Command("zig", "cc", sPath, "-o", outPath)
	} else {
		clangCmd = exec.Command(cc, sPath, "-o", outPath)
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
