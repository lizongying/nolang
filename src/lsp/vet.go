package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// VetResult holds a single diagnostic found during vet.
type VetResult struct {
	File     string
	Line     int
	Column   int
	Severity string
	Source   string
	Message  string
}

// VetFile runs the full LSP validation pipeline on a single .no file
// and returns all diagnostics found.
func VetFile(filePath string) []VetResult {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return []VetResult{{
			File:     filePath,
			Severity: "error",
			Source:   "lsp-vet",
			Message:  fmt.Sprintf("cannot read file: %v", err),
		}}
	}

	absPath, _ := filepath.Abs(filePath)
	docDir := filepath.Dir(absPath)

	l := lexer.New(string(source))
	p := parser.New(l)
	p.Filename = filepath.Base(filePath)

	// Inject std module signatures so the parser can infer cross-module types
	funcSigs, structFields := checker.CollectStdModuleSignatures()
	p.SetExternSignatures(funcSigs, structFields)

	prog := p.ParseProgram()

	var results []VetResult

	// 1. Parse errors
	for _, errMsg := range p.Errors() {
		var line, col int
		fmt.Sscanf(errMsg, "line %d, column %d:", &line, &col)
		results = append(results, VetResult{
			File:     filePath,
			Line:     line,
			Column:   col,
			Severity: "error",
			Source:   "nolang-parser",
			Message:  errMsg,
		})
	}

	if prog == nil {
		return results
	}

	// 2. Run all lints via shared entry point (naming, unused, embed,
	// interface impl, string concat, hex case, print format, func args,
	// types, undefined vars, etc.) — same as LSP real-time diagnostics.
	// LightweightMode=true: also check unresolved module.fn() calls
	// (no vet doesn't need this — it has build/module_check.go's full
	// version that works on the merged program).
	lints := checker.RunAllLints(prog, checker.LintOptions{
		SourcePath:      absPath,
		RootDir:         docDir,
		LightweightMode: true,
	})
	for _, l := range lints {
		results = append(results, VetResult{
			File:     filePath,
			Line:     l.Line,
			Column:   l.Column,
			Severity: string(l.Severity),
			Source:   l.Source,
			Message:  l.Message,
		})
	}

	return results
}

// VetDir runs VetFile on every .no file in a directory (recursively).
func VetDir(dirPath string) []VetResult {
	return VetDirVerbose(dirPath, nil)
}

// VetDirVerbose is like VetDir but reports per-file progress to the given
// logger. If progress is nil, no progress is reported. Each callback receives
// the file path, the number of diagnostics found in that file, and the elapsed
// duration — allowing callers to spot slow files.
type VetProgressFunc func(path string, diagCount int, elapsed time.Duration)

func VetDirVerbose(dirPath string, progress VetProgressFunc) []VetResult {
	// 載入 package.jsonc 以套用 ignore 列表 + 解析依賴
	// （與 no vet CLI 的目錄模式行為對齊）
	vetPkg, _ := pkg.LoadPackage(dirPath)
	if vetPkg != nil {
		if _, err := vetPkg.EnsureDependencies(10); err != nil {
			return []VetResult{{
				File:     dirPath,
				Severity: "error",
				Source:   "lsp-vet",
				Message:  fmt.Sprintf("dependency resolution failed: %v", err),
			}}
		}
	}

	var results []VetResult
	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".no") {
			return nil
		}
		// 跳過 ignore 列表中匹配的檔案（與 no vet CLI 一致）
		if vetPkg != nil && vetPkg.IsIgnored(path) {
			return nil
		}
		start := time.Now()
		fileRes := VetFile(path)
		if progress != nil {
			progress(path, len(fileRes), time.Since(start))
		}
		results = append(results, fileRes...)
		return nil
	})
	return results
}

// VetPath dispatches to VetFile or VetDir based on whether the path
// is a file or directory.
func VetPath(path string) []VetResult {
	return VetPathVerbose(path, nil)
}

// VetPathVerbose is like VetPath but accepts a progress callback for
// directory walks.
func VetPathVerbose(path string, progress VetProgressFunc) []VetResult {
	info, err := os.Stat(path)
	if err != nil {
		return []VetResult{{
			File:     path,
			Severity: "error",
			Source:   "lsp-vet",
			Message:  fmt.Sprintf("cannot access path: %v", err),
		}}
	}
	if info.IsDir() {
		return VetDirVerbose(path, progress)
	}
	return VetFile(path)
}

// FormatVetResults produces a human-readable report from vet results.
// Returns the number of errors (excluding warnings and hints).
func FormatVetResults(results []VetResult) int {
	// Sort by file, then line, then column
	sort.Slice(results, func(i, j int) bool {
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		if results[i].Line != results[j].Line {
			return results[i].Line < results[j].Line
		}
		return results[i].Column < results[j].Column
	})

	errorCount := 0
	for _, r := range results {
		sev := strings.ToUpper(r.Severity)
		if r.Severity == "error" {
			errorCount++
		}
		if r.Line > 0 {
			fmt.Printf("%s:%d:%d: [%s] %s: %s\n", r.File, r.Line, r.Column, sev, r.Source, r.Message)
		} else {
			fmt.Printf("%s: [%s] %s: %s\n", r.File, sev, r.Source, r.Message)
		}
	}
	return errorCount
}
