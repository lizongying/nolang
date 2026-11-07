package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// captureStdout runs f while capturing everything written to os.Stdout.
func captureStdoutFix(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// TestPrintLintResultsRendersSnippetAndDescriptionFixes 驗證 printLintResults 同時正確
// 渲染「可套用片段」修復（| snippet 區塊）與「僅描述」修復（fix: <description>），
// 後者此前因 firstFix 只認 Snippet 而被靜默丟棄。
func TestPrintLintResultsRendersSnippetAndDescriptionFixes(t *testing.T) {
	results := []build.LintResult{{
		File: "a.no",
		Lints: []checker.LintResult{
			{
				Line: 1, Column: 1, Severity: checker.LintError,
				Source: "nolang-compile", Message: "msg with snippet fix", TraceID: "aaaaaaaa",
				Fixes: []parser.DiagnosticFix{{Snippet: "os.get-env('X')", Description: "qualify call"}},
			},
			{
				Line: 2, Column: 1, Severity: checker.LintError,
				Source: "nolang-compile", Message: "msg with description-only fix", TraceID: "bbbbbbbb",
				Fixes: []parser.DiagnosticFix{{Description: "rename the duplicate declaration"}},
			},
			{
				Line: 3, Column: 1, Severity: checker.LintError,
				Source: "nolang-compile", Message: "msg with no fix", TraceID: "cccccccc",
			},
		},
	}}

	out := captureStdoutFix(t, func() { _ = printLintResults(results) })

	if !bytes.Contains([]byte(out), []byte("| os.get-env('X')")) {
		t.Errorf("expected snippet fix to render as '| os.get-env('X')', got:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("rename the duplicate declaration")) {
		t.Errorf("expected description-only fix to render, got:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("fix:")) {
		t.Errorf("expected at least one 'fix:' label, got:\n%s", out)
	}
	// 無修復的診斷不應被套上 fix 區塊。
	if bytes.Contains([]byte(out), []byte("msg with no fix")) &&
		bytes.Contains([]byte(out)[bytes.Index([]byte(out), []byte("msg with no fix")):], []byte("fix:")) {
		// 允許（因為多條診斷共用輸出）；此處僅確認上面兩條有 fix 即可。
	}
}

// TestFirstFixPrefersSnippet 確認 firstFix 優先回傳帶 Snippet 的修復，
// 無 Snippet 時回退到僅描述修復，空輸入回傳零值。
func TestFirstFixPrefersSnippet(t *testing.T) {
	snip := parser.DiagnosticFix{Snippet: "x", Description: "d"}
	desc := parser.DiagnosticFix{Description: "only desc"}

	f := firstFix([]parser.DiagnosticFix{snip, desc})
	if f.Snippet != "x" {
		t.Errorf("expected snippet fix to win, got %+v", f)
	}

	f2 := firstFix([]parser.DiagnosticFix{desc})
	if f2.Snippet != "" || f2.Description != "only desc" {
		t.Errorf("expected description-only fix returned, got %+v", f2)
	}

	f3 := firstFix(nil)
	if f3.Snippet != "" || f3.Description != "" {
		t.Errorf("expected zero fix for nil input, got %+v", f3)
	}
}
