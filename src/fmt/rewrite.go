package fmt

// rewrite.go — `no fmt` 與 LSP 共用的主動改寫渲染層（編排見 fmtflow 套件）。
//
// 早期 CLI 的做法是「先手術式移除冗餘型別標註 → 用改後的字串重新 parse →
// 渲染」，LSP 則無法照做（會讓 doc.AST 與來源字串短暫不一致）。現在兩邊都
// 走同一條路：把「哪些 LetStatement 的標註該省略」當成一張節點表傳進
// formatter。冗餘與否仍由 checker.ValidateRedundantTypeAnnotationWithExtern
// 判定、再由 fmt.RedundantTypeAnnotationSafe 過濾，渲染時由 stmt.go 把該節點
// 的 s.Type 臨時視為 nil；不再需要文字手術，也不必複製那套區間計算。
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
// 循環風格固定為 prefix；需要指定風格用 FormatProgramWithRewritesAndLoopStyle。
func FormatProgramWithRewrites(
	program *parser.Program,
	source string,
	redundant map[*parser.LetStatement]bool,
	relevant map[parser.Statement]bool,
	governed map[*parser.AnnotationStatement]parser.Statement,
) string {
	return FormatProgramWithRewritesAndLoopStyle(program, source, LoopStylePrefix, redundant, relevant, governed)
}

// FormatProgramWithRewritesAndLoopStyle 同 FormatProgramWithRewrites，但可指定循環
// 輸出風格。與 FormatProgramWithOverflowAndLoopStyle 對稱，避免工具鏈的兩條
// 主動改寫入口一個吃 style、一個硬編碼 prefix。
func FormatProgramWithRewritesAndLoopStyle(
	program *parser.Program,
	source string,
	style LoopStyle,
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
		loopStyle:        style,
		redundantTypes:   redundant,
		overflowRelevant: relevant,
		overflowGoverned: governed,
	}
	f.formatProgram(program)
	return ensureTrailingNewline(f.buf.String())
}
