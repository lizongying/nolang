package lsp

// fmt_rewrite.go — LSP format-on-save 的入口（預設與 CLI `no fmt` 一致）。
//
// 主動改寫的實際編排只有一份：fmtflow.Format。`no fmt` 的三條 CLI 路徑
// （檔案 / stdin / 目錄檢查）與這裡呼叫的是同一個函式，因此「存檔自動格式化」
// 與 `no fmt -w` 對同一份檔案必然產生相同結果 —— 這是 AGENTS.md §5.3 的
// 單一來源契約，過去兩邊各有一套編排、防禦條件還不一致（存檔保留 `[]T`
// 標註、命令列卻移除），已由 fmtflow 收口。
//
// LSP 只負責三件本地決策：
//  1. 把 FmtConfig 的細粒度開關翻成 fmtflow.Options；
//  2. 決定要不要把 ParseDocument 發布的糖 AST 當作 base 交給 fmtflow（省一次
//     lex/parse；fmtflow 會深拷貝，索引樹本身不被 FixStringConcatOperators 污染）；
//  3. 純排版（rewrite 全關）時可完全跳過 fmtflow，直接複用 doc.AST。

import (
	nfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/fmtflow"
	"github.com/lizongying/nolang/parser"
)

// rewriteAndFormat 依 c 的開關對 src 做格式化。回傳值一定非空且可直接
// 作為 TextEdit.NewText；任何一步失敗都保守退回「只排版」的結果
// （fmtflow.Format 內部處理，錯誤不回報給編輯器）。
func rewriteAndFormat(src string, cfg nfmt.ParseConfig, c FmtConfig, base *parser.Program) string {
	opt := fmtflow.Options{
		LoopStyle:             nfmt.LoopStylePrefix,
		RedundantTypes:        c.Rewrite && c.RedundantTypes,
		StringConcat:          c.Rewrite && c.StringConcat,
		IneffectiveOverflow:   c.Rewrite && c.IneffectiveOverflow,
		CrossModuleSignatures: c.CrossModuleSignatures,
		Base:                  base,
	}
	out, _ := fmtflow.Format(src, cfg, opt)
	return out
}
