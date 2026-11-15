package lsp

// fmt_rewrite.go — LSP format-on-save 的主動改寫（預設與 CLI `no fmt` 一致）。
//
// CLI 的順序是：移除冗餘型別標註（改來源字串）→ 重新 parse → stringConcat
// 翻轉 → overflow 相關性 → FormatProgramWithOverflow。LSP 不能照抄前半段：
// doc.AST 是索引用的樹，改來源字串會讓它與緩衝區文字短暫不一致，而且
// cmd/no 那套文字區間計算（含 map / hex / 合成型別防禦）約 250 行，複製
// 一份到 LSP 只會帶來漂移。
//
// 這裡改用 fmt.FormatProgramWithRewrites：把「哪些 LetStatement 的標註該省略」
// 當成節點表交給渲染器，來源字串與 AST 都不必改。安全性由三道關卡保證：
//  1. 冗餘與否仍由 checker.ValidateRedundantTypeAnnotationWithExtern 判定；
//  2. redundantTypeSafe 擋掉定長陣列 / map / hex / 合成型別等「其實不可省」的標註；
//  3. 格式化產物再 parse 一次，若改寫讓程式變得不可解析，退回不省略標註的版本。
//
// 整個流程在一棵 fresh parse 的臨時樹上進行，doc.AST 完全不被碰——
// FixStringConcatOperators 會就地改 AST，直接複用索引樹會污染 hover / 補全。

import (
	"strings"

	"github.com/lizongying/nolang/checker"
	nfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/parser"
)

// rewriteAndFormat 依 c 的開關對 src 做格式化。回傳值一定非空且可直接
// 作為 TextEdit.NewText；任何一步失敗都保守退回「只排版」的結果。
func rewriteAndFormat(src string, cfg nfmt.ParseConfig, c FmtConfig) string {
	effective := cfg
	if !c.CrossModuleSignatures {
		// 退回單檔視野，但保留 Filename 以免診斷文字漂移。
		effective = nfmt.ParseConfig{Filename: cfg.Filename}
	}

	// 總開關關閉：純排版。呼叫端可選擇直接複用 doc.AST（見 server.go）。
	if !c.Rewrite {
		out, _ := nfmt.FormatFileWithConfig(src, effective, nfmt.LoopStylePrefix)
		return out
	}

	program, perrs := nfmt.ParseSourceWithConfig(src, effective)
	if program == nil || len(perrs) > 0 {
		out, _ := nfmt.FormatFileWithConfig(src, effective, nfmt.LoopStylePrefix)
		return out
	}

	ext := checker.ExternSigTablesFromConfig(effective)

	var redundant map[*parser.LetStatement]bool
	if c.RedundantTypes {
		redundant = redundantLetTypes(program, ext)
	}
	if c.StringConcat {
		checker.FixStringConcatOperators(program, checker.BuildTHIRView(program))
	}
	var relevant map[parser.Statement]bool
	var governed map[*parser.AnnotationStatement]parser.Statement
	if c.IneffectiveOverflow {
		relevant, governed = checker.OverflowAnnotationRelevanceWithExtern(program, ext)
	}

	out := nfmt.FormatProgramWithRewrites(program, src, redundant, relevant, governed)

	// 關卡 3：省略標註後的產物必須仍能解析。失敗就用同一棵樹重渲染一次，
	// 只保留 stringConcat / overflow 兩類不改變語法結構的改寫。
	if len(redundant) > 0 {
		if _, errs := nfmt.ParseSourceWithConfig(out, effective); len(errs) > 0 {
			return nfmt.FormatProgramWithRewrites(program, src, nil, relevant, governed)
		}
	}
	return out
}

// redundantLetTypes 把 checker 的報告（位置）換成節點表。
// 報告的 Line/Column 取自 s.Type.Pos()，與 findLetByTypePos 的定位同源。
func redundantLetTypes(program *parser.Program, ext checker.ExternSigTables) map[*parser.LetStatement]bool {
	results := checker.ValidateRedundantTypeAnnotationWithExtern(program, ext)
	if len(results) == 0 {
		return nil
	}
	out := make(map[*parser.LetStatement]bool, len(results))
	for _, r := range results {
		n := parser.FindLetByTypePos(program, r.Line, r.Column)
		if n == nil || !redundantTypeSafe(n) {
			continue
		}
		out[n] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// redundantTypeSafe 對應 cmd/no computeRedundantTypeRemoval 的防禦條件，
// 但只需在 AST 上判斷（不需要算文字區間）：
//   - 定長陣列 / slice / map 標註是「選表示形式」的開關，不是單純型別提示；
//   - map 字面值移除標註會被 parser 讀成程式區塊；
//   - hex 字面值移除標註會把 byte 退化成 i64（checker 啟發式與編譯器不一致）；
//   - 推斷/合成型別的 Pos() 與變數名重合，原始碼根本沒有可省略的文字。
func redundantTypeSafe(n *parser.LetStatement) bool {
	if n == nil || n.Type == nil || n.Value == nil || n.Name == nil {
		return false
	}
	switch n.Type.(type) {
	case *parser.ArrayType, *parser.SliceType, *parser.MapType:
		return false
	}
	if _, ok := n.Value.(*parser.MapLiteral); ok {
		return false
	}
	if walkHasHexLiteral(n.Value) {
		return false
	}
	tp := n.Type.Pos()
	np := n.Name.Pos()
	if tp.Line != np.Line || tp.Column <= np.Column+len(n.Name.Value) {
		return false
	}
	return true
}

// walkHasHexLiteral 走查字面量樹，尋找 0x/0X 開頭的整數字面量。
func walkHasHexLiteral(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.IntegerLiteral:
		r := strings.ToLower(v.Raw)
		return strings.HasPrefix(r, "0x") || strings.HasPrefix(r, "-0x") || strings.HasPrefix(r, "+0x")
	case *parser.ArrayLiteral:
		if v.Size != nil && walkHasHexLiteral(v.Size) {
			return true
		}
		for _, el := range v.Elements {
			if walkHasHexLiteral(el) {
				return true
			}
		}
	case *parser.SliceLiteral:
		for _, el := range v.Elements {
			if walkHasHexLiteral(el) {
				return true
			}
		}
	case *parser.MapLiteral:
		for _, p := range v.Pairs {
			if walkHasHexLiteral(p.Key) || walkHasHexLiteral(p.Value) {
				return true
			}
		}
	}
	return false
}
