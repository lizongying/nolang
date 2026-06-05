package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cd src/build && go run . -target llvm -cc clang -o ../../dist/test_1 ../../tests/test_1.no
func main() {
	// os.Args = append(os.Args, "../../tests/test_let.no")
	outputFile := flag.String("o", "/Users/lizongying/IdeaProjects/no/dist/test_let.no", "Output file path")
	targetStr := flag.String("target", "llvm", "Target: llvm, no")
	cc := flag.String("cc", "clang", "C compiler for llvm target: clang, zig")
	testMode := flag.Bool("test", false, "Build test module (test.no)")
	exportMode := flag.Bool("export", false, "Build library module (lib.no)")
	verbose := flag.Bool("v", false, "Verbose mode")
	help := flag.Bool("h", false, "Show help")
	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	if len(flag.Args()) != 1 {
		fmt.Println("Error: Missing input file")
		flag.Usage()
		os.Exit(1)
	}

	target := TargetLLVM
	switch *targetStr {
	case "llvm":
		target = TargetLLVM
	case "no":
		target = TargetNo
	default:
		fmt.Printf("Error: Unknown target %q (use: llvm, no)\n", *targetStr)
		os.Exit(1)
	}

	inputFile := flag.Args()[0]

	// 若指定的是目錄，先找目錄內的 nolang.jsonc
	info, err := os.Stat(inputFile)
	isDir := err == nil && info.IsDir()

	var pkgDir string
	if isDir {
		pkgDir = inputFile
	} else {
		pkgDir = filepath.Dir(inputFile)
	}

	pkg, _ := LoadPackage(pkgDir)
	if pkg != nil && isDir {
		var mainFile string
		if *testMode {
			mainFile = "test.no"
		} else if *exportMode {
			mainFile = "lib.no"
		} else {
			mainFile = pkg.Main
			if mainFile == "" {
				mainFile = "main.no"
			}
		}
		inputFile = pkg.ResolvePath(mainFile)
	}

	source, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading input file: %v\n", err)
		os.Exit(1)
	}

	compiler := NewTranspiler()
	compiler.pkg = pkg

	code, err := compiler.CompileTarget(string(source), target)
	if err != nil {
		fmt.Printf("Compilation error: %v\n", err)
		os.Exit(1)
	}

	fileName := strings.TrimSuffix(filepath.Base(inputFile), ".no")

	var outPath string
	if *outputFile != "" {
		outPath = *outputFile
	} else {
		// 套件模式：dist 放在套件根目錄
		rootDir := "."
		if pkg != nil {
			rootDir = pkg.RootDir
		}
		distDir := filepath.Join(rootDir, "dist")
		err = os.MkdirAll(distDir, 0755)
		if err != nil {
			fmt.Printf("Error creating dist directory: %v\n", err)
			os.Exit(1)
		}
		outPath = filepath.Join(distDir, fileName)
	}

	switch target {
	case TargetLLVM:
		err = buildLLVM(code, fileName, outPath, *cc, *verbose)
	case TargetNo:
		err = buildNo(code, fileName, outPath, *verbose)
	}

	if err != nil {
		fmt.Printf("Build error: %v\n", err)
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("Build successful: %s\n", outPath)
	}
}

func buildLLVM(code string, fileName string, outPath string, cc string, verbose bool) error {
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
		// opt 不可用時 fallback 到原始 IR
		if verbose {
			fmt.Printf("opt not available, using unoptimized IR: %v\n", err)
		}
		optPath = llPath
	}
	// 保留最佳化後的 IR
	optFinal := optPath
	if optFinal != llPath { // 只有在 opt 成功時才為分離檔案
		raw, _ := os.ReadFile(optPath)
		os.WriteFile(outPath+"_opt.ll", raw, 0644)
	}
	llPath = optPath

	// Step 2: llc → .s (assembly)
	sPath := filepath.Join(tempDir, fileName+".s")
	llcCmd := exec.Command("llc", llPath, "-o", sPath)
	llcCmd.Stdout = os.Stdout
	llcCmd.Stderr = os.Stderr

	if verbose {
		fmt.Printf("Running: llc %s -o %s\n", llPath, sPath)
	}

	err = llcCmd.Run()
	if err != nil {
		return fmt.Errorf("LLVM assembly failed: %w", err)
	}

	// Step 2: cc → executable (assemble + link), e.g. clang, zig cc
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

	err = clangCmd.Run()
	if err != nil {
		return fmt.Errorf("linking failed: %w", err)
	}

	return nil
}

func buildNo(code string, fileName string, outPath string, verbose bool) error {
	if verbose {
		fmt.Printf("Generated nolang code saved to %s\n", outPath)
	}
	return os.WriteFile(outPath+".no", []byte(code), 0644)
}
