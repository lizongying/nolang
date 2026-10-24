package build

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/checker"
)

// DiagnosticError 是編譯期結構化診斷錯誤。它攜帶已結構化的 []checker.LintResult，
// 供 CLI / LSP 直接消費，取代舊路徑「把 []checker.ValidateResult 拍平成
// fmt.Errorf 字符串 → parseCompileErrorToLints 正則反解析」的脆弱往返（見
// builder.go:parseCompileErrorToLints）。
//
// Error() 渲染與舊拍平路徑完全一致的字符串（"validation errors: line L, column C:
// msg [tid]; ..."），因此任何只印 err.Error() 的既有呼叫點（含 buildWithPkg 的
// "compilation error: %w" 包裝、cmd 的錯誤印出）行為不變——這是行為保持重構：
// 結構化資料在邊界可用（errors.As 可取），但扁平字串表示被精確保留。
//
// Prefix 區分 "validation errors"（預設）與 "function argument errors"，對應舊
// 拍平路徑的兩種錯誤前綴。
type DiagnosticError struct {
	Lints  []checker.LintResult
	Prefix string
}

// Error 渲染與舊拍平路徑逐字一致的字符串，保證所有只消費 err.Error() 的呼叫點
// 輸出不變。
func (e *DiagnosticError) Error() string {
	if len(e.Lints) == 0 {
		return ""
	}
	prefix := e.Prefix
	if prefix == "" {
		prefix = "validation errors"
	}
	parts := make([]string, 0, len(e.Lints))
	for _, l := range e.Lints {
		parts = append(parts, fmt.Sprintf("line %d, column %d: %s [%s]", l.Line, l.Column, l.Message, l.TraceID))
	}
	return prefix + ": " + strings.Join(parts, "; ")
}

// validateResultToLint 把 checker.ValidateResult 轉為結構化 checker.LintResult。
//
// 欄位刻意與 parseCompileErrorToLints 的反解析結果對齊：Severity=error、
// Source="nolang-compile"、File=""、EndColumn=0。這樣 errors.As 取到的結構化
// LintResult 與「Error() 字串經 parseCompileErrorToLints 反解析」得到的 LintResult
// 逐欄位全等——保證 no vet 的指紋門禁（VetFileWithLints 回傳 lint 的規範化
// sha256）在 #6 前後零漂移。
//
// 註：checker.ValidateResult 在合併模式下本帶 File / EndColumn 一手歸因資訊，
// 此處主動丟棄以匹配現有行為；未來若要讓合併模組的驗證錯誤帶正確 File 歸因，
// 需同步調整 reparse 前綴並重新基線化指紋（獨立 follow-up）。
func validateResultToLint(e checker.ValidateResult) checker.LintResult {
	return checker.LintResult{
		Line:     e.Line,
		Column:   e.Column,
		Severity: checker.LintError,
		Source:   "nolang-compile",
		Message:  e.Message,
		TraceID:  e.TraceID,
	}
}
