package fmt

// rewrite.go — 讓工具鏈（LSP / editor）在不改寫來源字串的前提下，做與
// `no fmt` 相同的主動改寫。
//
// CLI 的做法是「先手術式移除冗餘型別標註 → 用改後的字串重新 parse → 渲染」。
// LSP 若照做，就必須在每次 format-on-save 重複 cmd/no 裡那一整套文字區間
// 計算與防禦邏輯（約 250 行），且會讓 doc.AST 與來源字串短暫不一致。
// 這裡改成把「哪些 LetStatement 的標註該省略」當成一張節點表傳進 formatter：
// 冗餘與否仍由 checker.ValidateRedundantTypeAnnotationWithExtern 判定，
// 渲染時由 stmt.go 把該節點的 s.Type 臨時視為 nil，效果與 CLI 重新 parse
// 後的樹一致，但不需要動來源字串，也不複製文字手術。
//
// overflow 註解的移除沿用既有 overflowRelevant / overflowGoverned 通道，
// 因此這個 API 是 FormatProgramWithOverflow 的超集：三張表全為 nil 時
// 與 FormatProgram 逐位元組一致。

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// FormatProgramWithRewrites 以給定的 AST 渲染 source，並套用兩類主動改寫：
//
//   - redundant：這些 LetStatement 的顯式型別標註不輸出（nil / 空 = 不改寫）；
//   - relevant / governed：移除無效的 #{overflow = ...} 註解（nil = 不移除）。
//
// 呼叫端負責在渲染前跑 checker 判定，並對產物做再次解析回驗；本函式只做
// 渲染，不改動 program 的任何欄位（見 stmt.go 的還原）。
func FormatProgramWithRewrites(
	program *parser.Program,
	source string,
	redundant map[*parser.LetStatement]bool,
	relevant map[parser.Statement]bool,
	governed map[*parser.AnnotationStatement]parser.Statement,
) string {
	if program == nil || len(program.Statements) == 0 {
		return source
	}
	f := &formatter{
		sourceLines:      strings.Split(source, "\n"),
		sem:              program.Sem,
		trivia:           program.FmtTrivia,
		loopStyle:        LoopStylePrefix,
		redundantTypes:   redundant,
		overflowRelevant: relevant,
		overflowGoverned: governed,
	}
	f.formatProgram(program)
	return ensureTrailingNewline(f.buf.String())
}
