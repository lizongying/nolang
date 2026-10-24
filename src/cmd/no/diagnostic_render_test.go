package main

import (
	"fmt"
	"testing"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
)

// TestAsBuildDiagnosticUnwrapsCompilationWrap 鎖定 #6 follow-up 的契約：
// BuildBackend 的錯誤以 "compilation error: %w" 包裝（build/builder.go），
// asBuildDiagnostic 必須能穿透該包裝取回 *build.DiagnosticError 的結構化 Lints。
func TestAsBuildDiagnosticUnwrapsCompilationWrap(t *testing.T) {
	want := &nbuild.DiagnosticError{
		Lints: []checker.LintResult{
			{
				Line:     1,
				Column:   10,
				Severity: checker.LintError,
				Source:   "nolang-compile",
				Message:  "'undefinedVar' is not defined",
				TraceID:  "4bek3xc6",
			},
		},
	}
	wrapped := fmt.Errorf("compilation error: %w", want)

	de, ok := asBuildDiagnostic(wrapped)
	if !ok {
		t.Fatal("asBuildDiagnostic 應穿透 'compilation error: %w' 取回 *build.DiagnosticError")
	}
	if len(de.Lints) != 1 {
		t.Fatalf("期望 1 條 lint，實際 %d 條: %+v", len(de.Lints), de.Lints)
	}
	if de.Lints[0].Message != "'undefinedVar' is not defined" || de.Lints[0].Line != 1 || de.Lints[0].Column != 10 {
		t.Fatalf("取回的 lint 欄位不符: %+v", de.Lints[0])
	}

	// 非結構化錯誤應回傳 false。
	if _, ok := asBuildDiagnostic(fmt.Errorf("some internal error")); ok {
		t.Fatal("非結構化錯誤不應被誤判為 DiagnosticError")
	}

	// Lints 為空的 DiagnosticError 視為無結構化診斷。
	if _, ok := asBuildDiagnostic(&nbuild.DiagnosticError{}); ok {
		t.Fatal("Lints 為空的 DiagnosticError 不應被視為有結構化診斷")
	}
}
