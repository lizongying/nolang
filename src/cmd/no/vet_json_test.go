package main

import (
	"encoding/json"
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
