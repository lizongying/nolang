package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestRunAllLints_OptUnuseCarriesFix 鎖定 checker 層 optunuse（gubuo5cb）在
// RunAllLints 路徑（no vet 的軟 lint 通道，與 build 層的硬錯誤通道並行）必須攜帶
// DiagnosticFix。runLintUnhandledOverflow 在轉換 ValidateResult → LintResult 時
// 必須透傳 Fixes，否則 no vet 的 fix: 提示行會消失。
func TestRunAllLints_OptUnuseCarriesFix(t *testing.T) {
	const src = `b i64 = 6
c i64 = 2
a = b + c / 2
`
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	lints := RunAllLints(prog, LintOptions{SourcePath: "m.no"})
	var found *LintResult
	for i := range lints {
		if lints[i].TraceID == optUnuseTraceID {
			found = &lints[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected optunuse lint from RunAllLints, got: %+v", lints)
	}
	if len(found.Fixes) == 0 {
		t.Errorf("optunuse lint has no Fixes; no 'fix:' will surface in no vet (RunAllLints path)")
	}
	if found.Fixes[0].Description == "" {
		t.Errorf("optunuse Fix[0] has empty Description")
	}
}
