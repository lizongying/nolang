package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
)

// TestBuildVetReportJSON 驗證 `no vet --json` 的報告結構：拍平、去重、排序、摘要。
func TestBuildVetReportJSON(t *testing.T) {
	all := []nbuild.LintResult{
		{
			File: "a.no",
			Lints: []checker.LintResult{
				{Line: 12, Column: 3, Severity: checker.LintError, Source: "nolang-index", Message: "unhandled index", TraceID: "idxhndld"},
				{Line: 5, Column: 1, Severity: checker.LintWarning, Source: "nolang-overflow-ineffective", Message: "ineffective overflow annotation"},
			},
		},
		{
			File: "b.no",
			Lints: []checker.LintResult{
				{Line: 2, Column: 1, Severity: checker.LintHint, Source: "nolang-lint", Message: "unused var"},
			},
		},
	}

	report, errCount := buildVetReport(all)
	if errCount != 1 {
		t.Fatalf("expected 1 error, got %d", errCount)
	}
	if report.Summary.Errors != 1 || report.Summary.Warnings != 1 || report.Summary.Hints != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	if len(report.Diagnostics) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d", len(report.Diagnostics))
	}
	// 必須按 file 再 line 排序：a.no:5 在 a.no:12 之前，b.no 在最後。
	if report.Diagnostics[0].File != "a.no" || report.Diagnostics[0].Line != 5 {
		t.Fatalf("unexpected first diagnostic: %+v", report.Diagnostics[0])
	}
	if report.Diagnostics[1].File != "a.no" || report.Diagnostics[1].Line != 12 {
		t.Fatalf("unexpected second diagnostic: %+v", report.Diagnostics[1])
	}
	if report.Diagnostics[2].File != "b.no" {
		t.Fatalf("unexpected third diagnostic: %+v", report.Diagnostics[2])
	}
	// severity 為小寫枚舉，rule 攜帶規則代碼。
	if report.Diagnostics[0].Severity != "warning" || report.Diagnostics[0].Rule != "nolang-overflow-ineffective" {
		t.Fatalf("unexpected severity/rule: %+v", report.Diagnostics[0])
	}
	if report.Diagnostics[1].Rule != "nolang-index" || report.Diagnostics[1].TraceID != "idxhndld" {
		t.Fatalf("unexpected rule/traceId: %+v", report.Diagnostics[1])
	}

	// JSON 往返必須穩定。
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back vetReport
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Summary.Errors != 1 || len(back.Diagnostics) != 3 {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

// TestFlattenLintsDedup 驗證跨文件重複診斷被去重（merged 模式常見）。
func TestFlattenLintsDedup(t *testing.T) {
	all := []nbuild.LintResult{
		{File: "a.no", Lints: []checker.LintResult{
			{Line: 1, Column: 1, Severity: checker.LintError, Source: "s", Message: "m"},
		}},
		{File: "a.no", Lints: []checker.LintResult{
			{Line: 1, Column: 1, Severity: checker.LintError, Source: "s", Message: "m"},
		}},
	}
	diags, _, _, _ := flattenLints(all)
	if len(diags) != 1 {
		t.Fatalf("expected dedup to 1, got %d", len(diags))
	}
}

// TestFlattenLintsUniqueSourceLocation 釘住「按包去重 + 報告唯一源碼位置」：
// 同一源碼位置經不同 lint 鏈到達時可能帶絕對路徑（內嵌 blob）或相對路徑（磁碟），
// 二者必須合併為一條診斷，且輸出 File 只能是唯一的相對寫法。
func TestFlattenLintsUniqueSourceLocation(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absStd := filepath.Join(cwd, "src", "std", "str.no")
	relStd := filepath.Join("src", "std", "str.no")
	all := []nbuild.LintResult{
		{File: "main.no", Lints: []checker.LintResult{
			{Line: 42, Column: 9, Severity: checker.LintHint, Source: "nolang-overflow",
				Message: "same issue", File: absStd},
		}},
		{File: "main.no", Lints: []checker.LintResult{
			{Line: 42, Column: 9, Severity: checker.LintHint, Source: "nolang-overflow",
				Message: "same issue", File: relStd},
		}},
	}
	diags, _, _, hints := flattenLints(all)
	if len(diags) != 1 {
		t.Fatalf("expected 1 deduped diagnostic, got %d: %+v", len(diags), diags)
	}
	if hints != 1 {
		t.Fatalf("expected hints=1, got %d", hints)
	}
	want := filepath.ToSlash(relStd)
	if diags[0].File != want {
		t.Fatalf("unique source location: got %q, want %q", diags[0].File, want)
	}
}

// TestCanonicalVetPathBlobOutsideCwd 釘住 cwd 外(abs) 的內嵌 std blob 路徑被
// 歸一到 src/std/… 形式，與工作區磁碟的相對寫法一致。
func TestCanonicalVetPathBlobOutsideCwd(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/tmp/old-build/src/std/str.no", "src/std/str.no"},
		{"/home/runner/work/nolang/std/net/http.no", "src/std/net/http.no"},
		{"", ""},
		{"src/std/number.no", "src/std/number.no"},
	}
	for _, c := range cases {
		if got := canonicalVetPath(c.in); got != c.want {
			t.Errorf("canonicalVetPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuildVetReportEmpty 驗證無診斷時輸出空陣列 + 零摘要（JSON 仍為合法結構）。
func TestBuildVetReportEmpty(t *testing.T) {
	report, errCount := buildVetReport(nil)
	if errCount != 0 || len(report.Diagnostics) != 0 {
		t.Fatalf("expected empty report, got %+v", report)
	}
	if report.Summary.Errors != 0 || report.Summary.Warnings != 0 || report.Summary.Hints != 0 {
		t.Fatalf("expected zero summary, got %+v", report.Summary)
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if string(b) != `{"diagnostics":[],"summary":{"errors":0,"warnings":0,"hints":0}}` {
		t.Fatalf("unexpected empty JSON: %s", string(b))
	}
}
