package build

// 本檔案定義 vet 模式下文件級 lint 結果聚合類型，以及從 checker 包
// 重新導出的嚴重性常量。供 no vet CLI 命令使用。
//
// checker.LintResult 是單條診斷（不包含文件路徑）；
// LintResult（此處定義）將同一文件的所有 lint 結果與文件路徑綁定。

import "github.com/lizongying/nolang/checker"

// LintResult 是一個文件的全部 lint 結果（文件級聚合）。
type LintResult struct {
	File  string               // 源碼檔案路徑
	Lints []checker.LintResult // 該文件的所有 lint 診斷
}

// 嚴重性類型，從 checker 重新導出。
type LintSeverity = checker.LintSeverity

const (
	LintError   = checker.LintError
	LintWarning = checker.LintWarning
	LintHint    = checker.LintHint
)
