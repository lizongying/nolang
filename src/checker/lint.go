package checker

// 本檔案提供 RunAllLints 共享入口，集中所有 Validate* 校驗器的調用
// 與嚴重性映射。被以下三條路徑共用：
//   1. build/transpiler.go vetMode 分支（no vet CLI 命令）
//   2. lsp/vet.go VetFile（nolang-lsp vet CLI 命令）
//   3. lsp/server.go publishDiagnostics（編輯器實時診斷）
//
// 新增 lint 時只需在這裡加一行，三條路徑同時生效，避免漂移。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// LintSeverity 表示 lint 結果的嚴重程度。
type LintSeverity string

const (
	LintError   LintSeverity = "error"
	LintWarning LintSeverity = "warning"
	LintHint    LintSeverity = "hint"
)

// LintResult 是一條 lint 診斷結果，包含位置、嚴重性、來源和消息。
type LintResult struct {
	Line      int
	Column    int
	EndColumn int
	Severity  LintSeverity
	Source    string
	Message   string
	TraceID   string
}

// LintOptions 控制 RunAllLints 的行為。
type LintOptions struct {
	// SourcePath 是當前編譯/驗證的源碼檔案路徑，
	// 用於 ValidateEmbedAnnotations 和 ValidateExportSymbols。
	SourcePath string
	// RootDir 是源碼所在目錄，用於 ValidateUndefinedVars、
	// ValidateDependencyImports 和 ValidateFuncArgs 的路徑解析。
	RootDir string
	// Strict 為 true 時，所有 warning/hint 升級為 error。
	Strict bool
	// LightweightMode 為 true 時，額外執行 CheckUnresolvedModuleCalls
	// （輕量版未解析模組函數呼叫檢查）。LSP 路徑（不做模組合併）
	// 應設為 true；no vet 路徑已有 build/module_check.go 的完整版，
	// 應設為 false（預設）。
	LightweightMode bool
	// MainFileVarNames 記錄主程式（非導入模組）的頂層變數名集合。
	// 用於 ValidateUnusedVars：當 program 是合併了導入模組的 merged
	// program 時（no vet 路徑），只檢查主程式的變數是否未使用，
	// 跳過導入模組的常量（如 std/log 的 LEVEL-ERROR）。
	// 為 nil 時（LSP 路徑），program 未合併模組，檢查所有頂層變數。
	MainFileVarNames map[string]bool
	// SkipTypeChecks 為 true 時跳過 ValidateTypes 和 ValidateFuncArgs。
	// 用於 no vet 路徑：這兩項檢查已在模組合併前作為 hard error 執行過
	// （transpiler.go line 1641/1647），合併後對 merged program 重複執行
	// 會因標準庫模組函數體內的局部型別推斷不完整而產生誤報（如
	// rand(kp-state) 被誤判為 []byte 而非 i64）。
	// LSP 路徑不做模組合併，program 是原始的，需要執行這兩項檢查。
	SkipTypeChecks bool
}

// RunAllLints 對 program 執行全部 lint 校驗，返回匯總結果。
// 調用者可根據 LintOptions.Strict 決定是否將 warning/hint 視為錯誤。
//
// 注意：ValidateTypes、ValidateFuncArgs、ValidateUninitOutputParams 在
// build/transpiler.go Compile 流程中已被調用並可能作為 hard error 阻止編譯，
// 但在 vet 模式（不走 Compile 的 LSP 路徑）下需要在此重新執行。
// 為保證三條路徑一致，這裡不遺漏地全部執行——同一 program 重複調用
// Validate* 是安全的（純只讀，不改 AST）。
func RunAllLints(program *parser.Program, opts LintOptions) []LintResult {
	if program == nil {
		return nil
	}

	var results []LintResult

	// 1. 型別錯誤（no vet 路徑已在合併前執行，跳過以避免 merged program 誤報）
	if !opts.SkipTypeChecks {
		for _, e := range ValidateTypes(program) {
			results = append(results, LintResult{
				Line: e.Line, Column: e.Column,
				Severity: LintError, Source: "nolang-type-checker",
				Message: e.Message, TraceID: e.TraceID,
			})
		}
	}

	// 2. 命名規範
	for _, w := range ValidateNaming(program) {
		results = append(results, LintResult{
			Line: w.Line, Column: w.Column,
			Severity: LintWarning, Source: "nolang-lint",
			Message: w.Message, TraceID: w.TraceID,
		})
	}

	// 3. async 命名規範
	for _, w := range ValidateAsyncNaming(program) {
		results = append(results, LintResult{
			Line: w.Line, Column: w.Column,
			Severity: LintWarning, Source: "nolang-lint",
			Message: w.Message, TraceID: w.TraceID,
		})
	}

	// 4. 未使用變數
	for _, u := range ValidateUnusedVars(program, opts.MainFileVarNames) {
		endCol := u.Column
		if u.EndColumn > 0 {
			endCol = u.EndColumn
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, EndColumn: endCol,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 5. 未定義變數
	for _, u := range ValidateUndefinedVars(program, opts.RootDir) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 6. 未初始化的 ?T 輸出參數
	for _, u := range ValidateUninitOutputParams(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 7. 未賦值的命名返回參數
	for _, w := range ValidateUnassignedReturns(program) {
		results = append(results, LintResult{
			Line: w.Line, Column: w.Column,
			Severity: LintWarning, Source: "nolang-type-checker",
			Message: w.Message, TraceID: w.TraceID,
		})
	}

	// 8. embed 註解校驗
	for _, e := range ValidateEmbedAnnotations(program, opts.SourcePath) {
		results = append(results, LintResult{
			Line: e.Line, Column: e.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: e.Message, TraceID: e.TraceID,
		})
	}

	// 9. 接口實現
	for _, u := range ValidateInterfaceImplementation(program) {
		endCol := u.Column
		if u.EndColumn > 0 {
			endCol = u.EndColumn
		}
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column, EndColumn: endCol,
			Severity: LintWarning, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 10. use 關鍵字提示
	for _, u := range ValidateUseKeyword(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 11. use as 別名提示
	for _, u := range ValidateUseAlias(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 12. 冗餘型別標註
	for _, u := range ValidateRedundantTypeAnnotation(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 13. 重複變數
	for _, u := range ValidateDuplicateVars(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 14. 依賴導入校驗
	for _, u := range ValidateDependencyImports(program, opts.RootDir) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 15. 導出符號校驗
	for _, u := range ValidateExportSymbols(program, opts.SourcePath) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 16. 字串拼接提示（建議用 - 代替 +）
	for _, u := range ValidateStringConcat(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 17. hex 字面量大寫提示
	for _, u := range ValidateHexCase(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintHint, Source: "nolang-lint",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 18. 函數引數型別校驗（no vet 路徑已在合併前執行，跳過以避免 merged program 誤報）
	if !opts.SkipTypeChecks {
		for _, u := range ValidateFuncArgs(program, opts.RootDir) {
			results = append(results, LintResult{
				Line: u.Line, Column: u.Column,
				Severity: LintError, Source: "nolang-type-checker",
				Message: u.Message, TraceID: u.TraceID,
			})
		}
	}

	// 19. print 格式字串校驗
	for _, u := range ValidatePrintFormat(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-format-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 20. 跨模組型別前綴校驗
	for _, u := range ValidateCrossModuleTypeRefs(program) {
		results = append(results, LintResult{
			Line: u.Line, Column: u.Column,
			Severity: LintError, Source: "nolang-type-checker",
			Message: u.Message, TraceID: u.TraceID,
		})
	}

	// 20b. 輕量版未解析模組函數呼叫檢查（僅 LSP 路徑）
	// no vet 路徑已有 build/module_check.go 的完整版（依賴 merged 上下文），
	// 不需要重複執行。
	if opts.LightweightMode {
		for _, u := range CheckUnresolvedModuleCalls(program) {
			results = append(results, LintResult{
				Line: u.Line, Column: u.Column,
				Severity: LintError, Source: "nolang-lint",
				Message: u.Message, TraceID: u.TraceID,
			})
		}
	}

	// 21. parser 警告
	for _, warnMsg := range program.Warnings {
		var line, col int
		// Strip optional "filename:" prefix so Sscanf can match "line N, column M:"
		stripped := warnMsg
		if idx := strings.Index(stripped, "line "); idx > 0 {
			stripped = stripped[idx:]
		}
		fmt.Sscanf(stripped, "line %d, column %d:", &line, &col)
		// Extract message after "line N, column M: " prefix
		msg := stripped
		if prefix := fmt.Sprintf("line %d, column %d:", line, col); strings.HasPrefix(msg, prefix) {
			msg = strings.TrimPrefix(msg, prefix)
			msg = strings.TrimPrefix(msg, " ")
		}
		// Strip "[W_CODE] " tag prefix for cleaner display
		if strings.HasPrefix(msg, "[W_") {
			if endBracket := strings.Index(msg, "] "); endBracket >= 0 {
				msg = msg[endBracket+2:]
			}
		}
		results = append(results, LintResult{
			Line: line, Column: col,
			Severity: LintHint, Source: "nolang-parser",
			Message: msg,
		})
	}

	// --strict：升級 warning/hint 為 error
	if opts.Strict {
		for i := range results {
			if results[i].Severity != LintError {
				results[i].Severity = LintError
			}
		}
	}

	// 按 line, column 排序，方便閱讀
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Line != results[j].Line {
			return results[i].Line < results[j].Line
		}
		return results[i].Column < results[j].Column
	})

	return results
}

// CountBySeverity 統計指定嚴重性的結果數量。
func CountBySeverity(results []LintResult, sev LintSeverity) int {
	count := 0
	for _, r := range results {
		if r.Severity == sev {
			count++
		}
	}
	return count
}

// HasErrors 判斷結果中是否存在 error 級別的診斷。
func HasErrors(results []LintResult) bool {
	for _, r := range results {
		if r.Severity == LintError {
			return true
		}
	}
	return false
}
