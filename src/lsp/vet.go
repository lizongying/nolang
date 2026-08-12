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

	// 2. Type errors
	for _, e := range checker.ValidateTypes(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     e.Line,
			Column:   e.Column,
			Severity: "error",
			Source:   "nolang-type-checker",
			Message:  e.Message,
		})
	}

	// 3. Naming warnings
	for _, w := range checker.ValidateNaming(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     w.Line,
			Column:   w.Column,
			Severity: "warning",
			Source:   "nolang-lint",
			Message:  w.Message,
		})
	}

	// 3.5. Async naming warnings
	for _, w := range checker.ValidateAsyncNaming(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     w.Line,
			Column:   w.Column,
			Severity: "warning",
			Source:   "nolang-lint",
			Message:  w.Message,
		})
	}

	// 4. Unused variables (hint)
	for _, u := range checker.ValidateUnusedVars(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 5. Undefined variables
	for _, u := range checker.ValidateUndefinedVars(prog, docDir) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 5b. Uninitialized nullable output parameters (case6)
	for _, u := range checker.ValidateUninitOutputParams(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-type-checker",
			Message:  u.Message,
		})
	}

	// 5b2. Unassigned result parameters (warning) — named return params
	// that are never explicitly assigned will be silently zero-filled.
	for _, w := range checker.ValidateUnassignedReturns(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     w.Line,
			Column:   w.Column,
			Severity: "warning",
			Source:   "nolang-type-checker",
			Message:  w.Message,
		})
	}

	// 5c. Embed annotation validation
	for _, e := range checker.ValidateEmbedAnnotations(prog, filePath) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     e.Line,
			Column:   e.Column,
			Severity: "error",
			Source:   "nolang-lint",
			Message:  e.Message,
		})
	}

	// 6. Interface implementation warnings
	for _, u := range checker.ValidateInterfaceImplementation(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "warning",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 7. Use keyword hints
	for _, u := range checker.ValidateUseKeyword(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 8. Use alias hints
	for _, u := range checker.ValidateUseAlias(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 8b. Redundant type annotation hints
	for _, u := range checker.ValidateRedundantTypeAnnotation(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 9. Duplicate variables
	for _, u := range checker.ValidateDuplicateVars(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 10. Dependency import validation
	for _, u := range checker.ValidateDependencyImports(prog, docDir) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 11. Export symbol validation
	for _, u := range checker.ValidateExportSymbols(prog, absPath) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 12. String concatenation hints
	for _, u := range checker.ValidateStringConcat(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 12b. Uppercase hex literal hints
	for _, u := range checker.ValidateHexCase(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  u.Message,
		})
	}

	// 13. Function argument type checking
	for _, u := range checker.ValidateFuncArgs(prog, docDir) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-type-checker",
			Message:  u.Message,
		})
	}

	// 14. Print format string validation (named {name:spec} fields)
	for _, u := range checker.ValidatePrintFormat(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-format-checker",
			Message:  u.Message,
		})
	}

	// 15. Cross-module type prefix validation
	for _, u := range checker.ValidateCrossModuleTypeRefs(prog) {
		results = append(results, VetResult{
			File:     filePath,
			Line:     u.Line,
			Column:   u.Column,
			Severity: "error",
			Source:   "nolang-type-checker",
			Message:  u.Message,
		})
	}

	// 16. Parser warnings
	for _, warnMsg := range prog.Warnings {
		var line, col int
		fmt.Sscanf(warnMsg, "line %d, column %d:", &line, &col)
		results = append(results, VetResult{
			File:     filePath,
			Line:     line,
			Column:   col,
			Severity: "hint",
			Source:   "nolang-lint",
			Message:  warnMsg,
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
