package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
	pkg "github.com/lizongying/nolang/package"
)

// fixEntry 是一條「可套用」修復與其歸屬診斷/檔案的綁定，便於分組與預覽。
type fixEntry struct {
	file string // 實際要被修改的源檔路徑
	lint checker.LintResult
	fix  parser.DiagnosticFix
}

// fixCommand 實作 `no fix`：收集 vet 能修的診斷（Fixes 含 Snippet+span），
// 預設 dry-run 預覽差異，只有顯式 --apply 才真正改寫源檔。
//
// 設計要點（與 no fmt --fix 正交）：
//   - 僅處理「有 Snippet 且 span 有效」的修復；Description-only 的建議仍由 `no vet` 展示。
//   - 預設絕不寫盤；--apply 才套用，可選帶 source rule / traceID 過濾。
//   - 同檔多修復從後往前套用，並拒絕相交（overlap）的 span，避免毀損源碼。
func fixCommand(args []string) {
	nbuild.ClearCaches()
	fs := flag.NewFlagSet("fix", flag.ExitOnError)
	apply := fs.Bool("apply", false, "apply fixes to source files (default: dry-run preview, no files modified)")
	filter := fs.String("filter", "", "only apply fixes whose source rule or traceID matches this value, e.g. --filter=overflow")
	strict := fs.Bool("strict", false, "treat lint warnings/hints as errors")
	fs.Usage = func() {
		fmt.Println("Usage: no fix [--apply] [--filter=<source|traceID>] [--strict] [<file|dir>]")
		fmt.Println("")
		fmt.Println("Collect diagnostics that carry an applicable fix and preview (or apply) them.")
		fmt.Println("Flags:")
		fmt.Println("  --apply    apply fixes; default is a non-destructive preview")
		fmt.Println("  --filter   only consider fixes whose source rule or traceID matches (e.g. overflow)")
		fmt.Println("  --strict   treat lint warnings/hints as errors")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  no fix                     preview all applicable fixes in the current directory")
		fmt.Println("  no fix main.no             preview fixes for main.no")
		fmt.Println("  no fix --apply             apply all applicable fixes")
		fmt.Println("  no fix --apply --filter=overflow  apply only overflow-related fixes")
	}
	_ = fs.Parse(args)

	opts := nbuild.BuildOptions{
		Verbose: verbose,
		Strict:  *strict,
	}

	inputPath := "."
	if fs.NArg() > 0 {
		inputPath = fs.Args()[0]
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 載入 package 配置以套用 ignore 列表 + 解析依賴（與 vet 一致）。
	vetPkg, _ := nbuild.LoadPackage(inputPath)
	if vetPkg != nil && info.IsDir() {
		if _, derr := vetPkg.EnsureDependencies(10); derr != nil {
			fmt.Fprintf(os.Stderr, "Error: dependency resolution failed: %v\n", derr)
			os.Exit(1)
		}
	}

	var allLints []nbuild.LintResult

	if info.IsDir() {
		// 目錄模式：遞迴收集所有 .no 檔（與 vet 相同的 WalkDir + worker pool）。
		var files []string
		werr := filepath.WalkDir(inputPath, func(path string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() && pkg.IsIgnoredSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(path, ".no") {
				if pkg.IsIgnoredSourcePath(path) {
					return nil
				}
				if vetPkg != nil && vetPkg.IsIgnored(path) {
					return nil
				}
				if verr := pkg.ValidateSourcePathName(path); verr != nil {
					return nil
				}
				files = append(files, path)
			}
			return nil
		})
		if werr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", werr)
			os.Exit(1)
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, runtime.NumCPU())
		var errMu sync.Mutex
		var firstErr error
		var lintMu sync.Mutex
		for _, file := range files {
			wg.Add(1)
			sem <- struct{}{}
			go func(f string) {
				defer wg.Done()
				defer func() { <-sem }()
				lints, e := nbuild.VetFileWithLints(f, opts)
				if e != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = i18n.Errorf("ddutfwr8", "Error in %s: %w", f, e)
					}
					errMu.Unlock()
					return
				}
				if len(lints) > 0 {
					lintMu.Lock()
					allLints = append(allLints, nbuild.LintResult{File: f, Lints: lints})
					lintMu.Unlock()
				}
			}(file)
		}
		wg.Wait()
		if firstErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", firstErr)
			os.Exit(1)
		}
	} else {
		// 檔案模式：驗證單個檔案。
		if pkg.IsIgnoredSourcePath(inputPath) {
			fmt.Println("source file is ignored (leading underscore); nothing to fix")
			return
		}
		if verr := pkg.ValidateSourcePathName(inputPath); verr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", verr)
			os.Exit(1)
		}
		lints, e := nbuild.VetFileWithLints(inputPath, opts)
		if e != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", e)
			os.Exit(1)
		}
		if len(lints) > 0 {
			allLints = append(allLints, nbuild.LintResult{File: inputPath, Lints: lints})
		}
	}

	// 收集可套用修復（Snippet 非空且 span 有效），按實際目標檔分組。
	byFile := map[string][]fixEntry{}
	for _, fr := range allLints {
		baseFile := fr.File
		for _, l := range fr.Lints {
			if len(l.Fixes) == 0 {
				continue
			}
			targetFile := l.File
			if targetFile == "" {
				targetFile = baseFile
			}
			for _, f := range l.Fixes {
				if f.Snippet == "" || f.StartLine <= 0 {
					continue
				}
				if *apply && *filter != "" {
					if l.Source != *filter && l.TraceID != *filter {
						continue
					}
				}
				editFile := f.File
				if editFile == "" {
					editFile = targetFile
				}
				byFile[editFile] = append(byFile[editFile], fixEntry{file: editFile, lint: l, fix: f})
			}
		}
	}

	if len(byFile) == 0 {
		fmt.Println("No applicable fixes found.")
		return
	}

	if !*apply {
		printFixPreview(byFile)
		return
	}

	// 套用模式：對每個檔案一次性套用其全部修復（含重疊檢測）。
	hadError := false
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		entries := byFile[file]
		fixes := make([]parser.DiagnosticFix, len(entries))
		for i, e := range entries {
			fixes[i] = e.fix
		}
		data, rerr := os.ReadFile(file)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", file, rerr)
			hadError = true
			continue
		}
		newContent, aerr := applyFixesToSource(string(data), fixes)
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", aerr)
			hadError = true
			continue
		}
		if newContent == string(data) {
			continue
		}
		if werr := os.WriteFile(file, []byte(newContent), 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", file, werr)
			hadError = true
			continue
		}
		fmt.Printf("%s: applied %d fix(es)\n", file, len(fixes))
	}
	if hadError {
		os.Exit(1)
	}
}

// printFixPreview 以 dry-run 預覽方式印出每個可套用修復：檔案、位置、來源規則、
// 說明，以及 old → new 的差異（old 與 new 相同時只印 new）。
func printFixPreview(byFile map[string][]fixEntry) {
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		es := byFile[file]
		fmt.Printf("%s\n", file)
		data, rerr := os.ReadFile(file)
		content := ""
		if rerr == nil {
			content = string(data)
		}
		starts := computeLineStarts(content)
		for _, e := range es {
			f := e.fix
			label := ""
			if f.Description != "" {
				label = ": " + f.Description
			}
			fmt.Printf("  %d:%d [%s]%s\n", f.StartLine, f.StartCol, e.lint.Source, label)
			old := ""
			if content != "" {
				old = extractSpan(content, starts, f)
			}
			if old != "" && old != f.Snippet {
				for _, ol := range strings.Split(old, "\n") {
					fmt.Printf("    - %s\n", ol)
				}
			}
			for _, nl := range strings.Split(f.Snippet, "\n") {
				fmt.Printf("      | %s\n", nl)
			}
		}
		fmt.Println("")
	}
	fmt.Fprintln(os.Stderr, "Dry-run preview only; no files were modified. Re-run with --apply to apply these fixes.")
}

// computeLineStarts 回傳每個 0-based 行首的位元組偏移（含第 0 行 = 0）。
// Nolang lexer 的 Column 是 1-based 位元組列，故可直接加 (col-1) 得到位元組偏移。
func computeLineStarts(content string) []int {
	starts := []int{0}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// extractSpan 回傳修復 span 在 content 中覆蓋的舊文本（用於預覽的差異顯示）。
func extractSpan(content string, starts []int, f parser.DiagnosticFix) string {
	sl, sc := f.StartLine-1, f.StartCol-1
	el, ec := f.EndLine-1, f.EndCol-1
	if f.EndLine == 0 {
		el = sl
	}
	if f.EndCol == 0 {
		ec = f.StartCol // 預設單一字元
	}
	if sl < 0 || el < 0 || sc < 0 || ec < 0 {
		return ""
	}
	if el >= len(starts) {
		return ""
	}
	s := starts[sl] + sc
	e := starts[el] + ec
	if s < 0 || e > len(content) || s > e {
		return ""
	}
	return content[s:e]
}

type fixEdit struct {
	start, end int
	snippet    string
}

// applyFixesToSource 將修復套用到源碼：依 1-based 位元組列計算位元組偏移，從後往前
// 替換，並拒絕相交（overlap）的 span。回傳新內容；任何衝突都報錯而非毀損源碼。
func applyFixesToSource(content string, fixes []parser.DiagnosticFix) (string, error) {
	if len(fixes) == 0 {
		return content, nil
	}
	starts := computeLineStarts(content)

	type editKey struct {
		s, e  int
		snip  string
	}
	seen := map[editKey]bool{}
	var edits []fixEdit
	for _, f := range fixes {
		if f.Snippet == "" || f.StartLine <= 0 {
			continue
		}
		sl, sc := f.StartLine-1, f.StartCol-1
		el, ec := f.EndLine-1, f.EndCol-1
		if f.EndLine == 0 {
			el = sl
		}
		if f.EndCol == 0 {
			ec = f.StartCol // 預設單一字元
		}
		if sl < 0 || el < 0 || sc < 0 || ec < 0 {
			return "", fmt.Errorf("fix %q has invalid span (line %d col %d)", f.Description, f.StartLine, f.StartCol)
		}
		if el >= len(starts) {
			return "", fmt.Errorf("fix %q: line %d out of range", f.Description, f.EndLine)
		}
		s := starts[sl] + sc
		e := starts[el] + ec
		if s < 0 || e > len(content) || s > e {
			return "", fmt.Errorf("fix %q: span out of range (%d-%d)", f.Description, s, e)
		}
		k := editKey{s, e, f.Snippet}
		if seen[k] {
			continue // 去重：相同 span+snippet 只套用一次
		}
		seen[k] = true
		edits = append(edits, fixEdit{start: s, end: e, snippet: f.Snippet})
	}

	// 從檔案尾端往前排序，保證先套用後面的編輯不影響前面的位元組偏移。
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start > edits[j].start
		}
		return edits[i].end > edits[j].end
	})

	// 重疊檢測：任一新 span 與已排程 span 相交即報錯退出。
	type interval struct{ s, e int }
	var applied []interval
	for _, ed := range edits {
		for _, a := range applied {
			if ed.start < a.e && a.s < ed.end {
				return "", fmt.Errorf("overlapping fix spans at bytes %d-%d and %d-%d; aborting to avoid corrupting source", a.s, a.e, ed.start, ed.end)
			}
		}
		applied = append(applied, interval{ed.start, ed.end})
	}

	out := content
	for _, ed := range edits {
		out = out[:ed.start] + ed.snippet + out[ed.end:]
	}
	return out, nil
}
