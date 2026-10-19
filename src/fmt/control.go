package fmt

import (
	"fmt"
	"os"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// formatStandaloneBody formats the body of a standalone if-then (cond -> body).
// If the body was written inline (single simple statement, no braces), it outputs
// inline without braces. If the body was written as a block `{ ... }`, it keeps
// the block form.
func (f *formatter) formatStandaloneBody(body *parser.BlockStatement) {
	if f.isStandaloneInline(body) {
		switch body.Statements[0].(type) {
		case *parser.ExpressionStatement:
			f.formatExpression(body.Statements[0].(*parser.ExpressionStatement).Expression)
			return
		case *parser.LetStatement, *parser.MultiAssignStatement,
			*parser.ReturnStatement, *parser.BreakStatement, *parser.ContinueStatement:
			f.formatStatement(body.Statements[0])
			return
		}
	}
	f.formatBlockStatement(body)
}

// isStandaloneInline reports whether a standalone body should be formatted inline
// (single simple statement without braces) rather than as a block `{ ... }`.
// Uses the IsInline flag set by the parser: true = inline single-statement body
// (e.g. `cond -> return`, `cond -> x = 1`), false = explicit block `{ ... }`.
// Also checks for comments/obc that would force block output.
// Used to decide whether `->` for the else arm goes on the same line or a new line:
// inline bodies keep `->` on the same line, block bodies put `->` on a new line.
func (f *formatter) isStandaloneInline(body *parser.BlockStatement) bool {
	if !body.IsInline || len(body.Statements) != 1 ||
		body.TrailingComments != nil ||
		body.ClosingBraceComment != nil ||
		f.obcOf(body) != nil ||
		f.hasDocComment(body.Statements[0]) {
		return false
	}
	// 若唯一陳述帶有「會實際輸出」的附加註解（如區塊級 #{overflow=...} 經
	// side-table 合併、或平台註解 #{mac-arm64}），內聯為 `cond -> #{...}` 形式無法
	// 被解析器還原（註解會被誤判為 arm 本體，或二次格式化產生 `overflow = wrap`
	// 之類的錯亂）。此時強制區塊形式 `cond -> { #{...} ... }` 以保證冪等與可重解析。
	// 此處用「含 overflow」的版本：overflow 同樣會印出獨立一行，同樣不能內聯。
	if f.attachedAnnotationsWillEmitAny(body.Statements[0]) {
		return false
	}
	switch body.Statements[0].(type) {
	case *parser.ExpressionStatement, *parser.LetStatement, *parser.MultiAssignStatement,
		*parser.ReturnStatement, *parser.BreakStatement, *parser.ContinueStatement:
		return true
	}
	return false
}

// isBareMatchBody reports whether the body contains a single bare match expression.
// Bare match bodies (e.g. `-> { cond -> body }`) output as `{ ... }` themselves,
// so inlining them in writeBareMatchArm doesn't change their visual form.
// This allows `{ }` block bodies containing a bare match to be inlined.
func (f *formatter) isBareMatchBody(statements []parser.Statement) bool {
	if len(statements) != 1 {
		return false
	}
	es, ok := statements[0].(*parser.ExpressionStatement)
	if !ok {
		return false
	}
	// 糖形態下裸 match 是 *SurfaceMatch，本身就印成 `{ ... }`，內聯不改變視覺形式，故可直接內聯。
	if _, ok := es.Expression.(*parser.SurfaceMatch); ok {
		return true
	}
	return false
}

func (f *formatter) formatIfExpression(e *parser.IfExpression) {
	// Standalone if-then: `cond -> body` (without enclosing { })
	//
	// RTPipelineValue（`x = cond -> v` 的管道值節點，parser 刻意不設
	// RTStandalone）與 standalone 共用同一套 `->` 輸出：管體在值語境下
	// 同樣由 parseStandaloneBody/wrapStandaloneChain 構造（內鏈自帶
	// RTStandalone），else 續寫規則也一致。若不加此分支，通用打印會輸出
	// `cond: { body }`——該寫法重新解析為 match-as-value，語義已被改變。
	if f.hasRT(e, parser.RTStandalone) || f.hasRT(e, parser.RTPipelineValue) {
		// Wildcard standalone: -> body
		if f.hasRT(e, parser.RTMatchWildcard) {
			// Empty body: just output -> (no trailing space)
			if e.Consequence == nil || len(e.Consequence.Statements) == 0 {
				f.write("->")
				return
			}
			f.write("-> ")
		} else {
			f.formatExpression(e.Condition)
			f.write(" -> ")
		}
		f.formatStandaloneBody(e.Consequence)
		if e.Alternative != nil {
			// Else `->` placement:
			// - If consequence is a block (not inline), `->` must go on a new line
			//   to avoid `} -> {` on the same line.
			// - If RTElseNewline is set (else was on a new line in source),
			//   `->` must go on a new line even when consequence is inline.
			// - Otherwise (inline consequence, same-line else), `->` stays inline.
			if !f.isStandaloneInline(e.Consequence) || f.hasRT(e, parser.RTElseNewline) {
				f.write("\n")
				f.newline()
				f.write("-> ")
			} else {
				f.write(" -> ")
			}
			f.formatStandaloneBody(e.Alternative)
		}
		return
	}

	// 區分 `if cond { } else { }`（deprecated，Token 為 IF 關鍵字）
	// 與 `cond: { } else: { }`（向後相容舊式，Token 為條件首 token，非 IF）
	isDeprecatedIf := e.Token.Type == lexer.IF
	if isDeprecatedIf {
		f.write("if ")
	}
	f.formatExpression(e.Condition)
	if isDeprecatedIf {
		f.write(" {")
	} else {
		f.write(": {")
	}
	f.indent++
	f.formatBlockInner(e.Consequence, e.Consequence.Token.Line)
	f.indent--
	f.newline()
	f.write("}")

	if e.Alternative != nil {
		// Check if alternative contains a single if expression (elif desugaring)
		if f.isElifBlock(e.Alternative) {
			ifExpr := e.Alternative.Statements[0].(*parser.ExpressionStatement).Expression.(*parser.IfExpression)
			f.write(" elif ")
			f.formatExpression(ifExpr.Condition)
			f.write(" {")
			f.indent++
			f.formatBlockInner(ifExpr.Consequence, ifExpr.Consequence.Token.Line)
			f.indent--
			f.newline()
			f.write("}")
			// Handle nested alternative
			if ifExpr.Alternative != nil {
				f.formatElifChain(ifExpr.Alternative)
			}
		} else {
			if isDeprecatedIf {
				f.write(" else {")
			} else {
				f.write(" else: {")
			}
			f.indent++
			f.formatBlockInner(e.Alternative, e.Alternative.Token.Line)
			f.indent--
			f.newline()
			f.write("}")
		}
	}
}

// bareMatchArm 是一條「已備妥、可輸出」的 match 臂：主體語句已濾掉解析器為空臂
// 留下的 nil 佔位，臂模式（pattern）由 writeCond 自行輸出（不含結尾的 ` ->`）。
//
// 它的唯一生產者是 armFromMatchArm：S-d 後 match 只以糖形態 SurfaceMatch 存在，
// 不再有降糖 IfExpression 形態，故臂模式直接照 parser.MatchArm 的表面資訊讀出，
// 不需要任何反推。
type bareMatchArm struct {
	body      *parser.BlockStatement
	stmts     []parser.Statement // body 語句，已濾掉合成腳手架
	wildcard  bool
	valBranch bool   // 通配臂但是 val/ok 分支（`ok` / `ok(v)`）
	binding   string // 析構綁定後綴，如 `(v)` / `(w, h)`
	writeCond func(*formatter)
}

// formatSurfaceMatch 直接從糖節點 SurfaceMatch 輸出 match——不經降糖、不讀痕跡
// 欄位、不反推 if 鏈形狀。臂模式照語法原樣讀出（見 armFromMatchArm），主體排版
// 由 writeArm 統一處理。
//
// 糖形態是 S-d 後 match 的唯一非編譯形態（no fmt 只見 SurfaceMatch），由
// src/fmt/sugar_fmt_ab_test.go 的 TestSugarFmtCorpusSmoke 等把守（reparse + 冪等）。
//
// 臂之間的空行：writeArm 每條臂都以 f.newline() 起頭，而臂與臂之間再寫一個裸
// "\n"，所以 match 的臂永遠隔一行輸出，與源碼是否留白無關。
func (f *formatter) formatSurfaceMatch(sm *parser.SurfaceMatch) {
	if sm.Matched != nil {
		f.formatExpression(sm.Matched)
		f.write(": {")
	} else {
		f.write("{")
	}
	// Output opening brace comment on the same line as {
	if obc := f.matchOpeningBraceComment(sm); obc != nil && len(obc.List) > 0 {
		f.write("; ")
		for _, c := range obc.List {
			f.write(strings.TrimSpace(c.Text))
		}
	}
	f.indent++
	for i := range sm.Arms {
		if i > 0 {
			f.write("\n")
		}
		f.writeMatchArm(&sm.Arms[i])
	}
	f.indent--
	f.newline()
	f.write("}")
}

// matchOpeningBraceComment 取 match 外層 `{` 的同行註釋。糖節點自己帶著它
// （SurfaceMatch.OpeningBraceComment）；side-table 那一邊是給降糖形態用的。
// 兩邊理論上只會有一邊非空，先查副表是為了讓「註釋已搬到 IfExpression」的
// 混合情形不會掉註釋。
func (f *formatter) matchOpeningBraceComment(sm *parser.SurfaceMatch) *parser.CommentGroup {
	if f.sem != nil {
		if g := f.sem.OpeningBraceCommentOf(sm); g != nil && len(g.List) > 0 {
			return g
		}
	}
	return sm.OpeningBraceComment
}

// writeMatchArm 輸出糖形態的單條 match 臂（parser.MatchArm）。與
// writeBareMatchArm 走同一條排版路徑，差別只在臂模式的來源。
func (f *formatter) writeMatchArm(ma *parser.MatchArm) {
	f.writeArm(f.armFromMatchArm(ma))
}

// armFromMatchArm 從糖節點 MatchArm 直接讀出臂模式——沒有反推、沒有痕跡欄位。
//
// 順序對應 parser 的臂首判定（parser/expr.go parseMatchExprFrom、
// parser/match.go parseBareMatchExpr）：多值 / 多 option pattern 在解析時就清空
// 了 Condition，故必須先於 Condition 檢查；`ok(v)` / `err(e)` 這類析構綁定與
// `v T ->` 類型臂則各有自己的欄位。
func (f *formatter) armFromMatchArm(ma *parser.MatchArm) bareMatchArm {
	// IsDotVal 的臂（`ok ->` / `. ->` / `ok(v) ->`）在語法上是通配拼寫，但語義上
	// 是 val 分支、不是 catch-all —— 與降糖形態的「RTMatchWildcard + DotValBody」
	// 同構，故兩者都設、由 writeArm 輸出 `ok` 前綴。
	a := bareMatchArm{
		body:      ma.Body,
		wildcard:  ma.IsWildcard,
		valBranch: ma.IsDotVal,
	}
	if ma.Body != nil {
		a.stmts = make([]parser.Statement, 0, len(ma.Body.Statements))
		for _, stmt := range ma.Body.Statements {
			// 糖形態沒有降糖合成的 `it` 綁定與零值語句；這裡只濾掉解析器為
			// 空臂留下的 nil 佔位，與降糖路徑保持同一口徑。
			if es, ok := stmt.(*parser.ExpressionStatement); ok && es.Expression == nil {
				continue
			}
			a.stmts = append(a.stmts, stmt)
		}
	}
	if ma.IsDotVal && ma.BindingName != "" {
		a.binding = "(" + ma.BindingName + ")"
	}
	a.writeCond = func(f *formatter) {
		switch {
		case len(ma.MultiOptionPatterns) > 0:
			// nil || err 等 option pattern 列表
			f.write(strings.Join(ma.MultiOptionPatterns, " || "))
		case len(ma.MultiValuePatterns) > 0:
			// 1 || 3 || 5 等 multi-value pattern 列表
			for i, vp := range ma.MultiValuePatterns {
				if i > 0 {
					f.write(" || ")
				}
				f.formatExpression(vp)
			}
		case ma.IsRawCond:
			// ok(cond) raw cond arm
			f.write("ok(")
			f.formatExpression(ma.Condition)
			f.write(")")
		case ma.IsTypeArm:
			// v T -> 帶綁定的類型臂（union / type-set 解構）
			f.write(ma.TypeBindingName)
			f.write(" ")
			f.write(ma.TypePattern)
		case ma.BindingName != "" && ma.BindingVariant != "":
			// err(e) 析構綁定（ok(v) 走 valBranch 分支）
			f.write(ma.BindingVariant)
			f.write("(")
			f.write(ma.BindingName)
			f.write(")")
		default:
			f.formatExpression(ma.Condition)
		}
	}
	return a
}

// writeArm 是 match 臂排版的唯一實現。參數已由兩個生產者備妥，本函式不再關心
// 臂模式是讀來的還是反推出來的。
func (f *formatter) writeArm(a bareMatchArm) {
	statements := a.stmts

	// outsideComments collects arm-level doc comments that were originally
	// before the arm body's opening brace. They are output before the condition.
	var outsideComments []*parser.Comment

	// If body has TrailingComments that are OUTSIDE the block (collected
	// before the opening brace), they are arm-level doc comments. Output
	// them on their own lines BEFORE the condition.
	// Comments INSIDE the block (between { and }) are real block content
	// and must stay in the block.
	// This must happen BEFORE the canInline check, because outside comments
	// should not prevent inlining — only inside comments do.
	if a.body.TrailingComments != nil &&
		a.body.ClosingBraceComment == nil &&
		f.obcOf(a.body) == nil {
		openLine := a.body.Token.Line
		var outside, inside []*parser.Comment
		for _, c := range a.body.TrailingComments.List {
			if c.Pos.Line < openLine {
				outside = append(outside, c)
			} else {
				inside = append(inside, c)
			}
		}
		// Stash outside comments to output before the condition later.
		// We can't output them yet because we haven't written the newline.
		a.body.TrailingComments = nil
		if len(inside) > 0 {
			a.body.TrailingComments = &parser.CommentGroup{List: inside}
		}
		// Store outside comments for output after the newline.
		outsideComments = outside
	}

	// 判斷是否為可內聯的簡單 body：單語句、無塊級註釋、類型為表達式/let/return/break/continue。
	// 注意：body 語句的 doc comment（arm 前的註釋，如 `; F grade`）不阻止內聯，
	// 該註釋會在 arm 條件之前獨立行輸出。
	// 使用 IsInline 標誌（由 parser 設置）區分用戶寫的 block `{ }` 與 inline 單語句。
	// 例外：若 body 是 bare match 表達式（本身就是 `{ }` 形式），內聯不改變其輸出形式。
	//
	// 關鍵守衛（與 isStandaloneInline 一致）：若唯一語句帶有「會實際輸出」的附加註解
	// （如區塊級 #{overflow=wrap} 經 side-table 合併到該語句、或平台註解 #{mac-arm64}），
	// 內聯成 `cond -> #{...}` 形式無法被解析器還原（註解會被誤判為 arm 本體，或二次格式化
	// 產生 `overflow = wrap` 之類的錯亂）。此時強制區塊形式 `cond -> { #{...} ... }`，
	// 保證冪等與可重解析。
	// 注意：此處必須用「含 overflow」的版本。overflow 也是會在該語句上方印出獨立一行的
	// 行注解，內聯同樣無法還原；早期誤用不含 overflow 的版本，導致帶 overflow 的 arm
	// body 被印成 `cond -> #{overflow=wrap}` 而二次格式化錯亂。
	canInline := (a.body.IsInline || f.isBareMatchBody(statements)) &&
		len(statements) == 1 &&
		a.body.TrailingComments == nil &&
		a.body.ClosingBraceComment == nil &&
		f.obcOf(a.body) == nil &&
		!f.attachedAnnotationsWillEmitAny(statements[0])
	if canInline {
		switch statements[0].(type) {
		case *parser.ExpressionStatement, *parser.LetStatement,
			*parser.ReturnStatement, *parser.BreakStatement, *parser.ContinueStatement:
		default:
			canInline = false
		}
	}

	// 若可內聯且 body 語句有 doc comment：
	//   - body 原始為 inline（IsInline=true）：doc comment 是 arm 前的註釋
	//     （如 `; F grade`），先輸出註釋再內聯 body。
	//   - body 原始為顯式 block（IsInline=false）：doc comment 是 block 內註釋
	//     （如 `// 部分區塊`），不內聯，走 block 路徑讓註釋在 block 內輸出。
	if canInline && f.hasDocComment(statements[0]) {
		if a.body.IsInline {
			f.newline()
			var doc *parser.CommentGroup
			if d, ok := statements[0].(interface{ GetDoc() *parser.CommentGroup }); ok {
				doc = d.GetDoc()
			}
			f.formatDocComments(doc)
			f.newline()
		} else {
			// 顯式 block 內的註釋：不內聯，保持 block 形式
			canInline = false
			f.newline()
		}
	} else {
		f.newline()
	}

	// Output outside comments (arm-level doc comments collected before the
	// arm body's opening brace) on their own lines before the condition.
	for _, c := range outsideComments {
		f.writeCommentBody(c)
		f.newline()
	}

	if a.wildcard {
		if a.valBranch {
			// `ok` / `ok(v)`：val 分支不是 catch-all，模式要印出來。
			f.write("ok" + a.binding + " ->")
		} else {
			f.write("->")
		}
	} else {
		a.writeCond(f)
		f.write(" ->")
	}

	// 空 body（如 wildcard `->`）：不輸出任何內容。
	// 但當 body 是顯式 block（IsInline=false，如 `cond -> {}`）時，
	// 保留空 {} 輸出以保持源碼形式。
	if len(statements) == 0 &&
		a.body.IsInline &&
		a.body.TrailingComments == nil &&
		a.body.ClosingBraceComment == nil &&
		f.obcOf(a.body) == nil {
		return
	}
	// 內聯簡單 body：輸出在同一行
	if canInline {
		f.write(" ")
		// 臨時清除 doc comment 以避免 formatStatement 再次輸出（已在上文輸出過），
		// 格式化後恢復以保持 AST 不變（避免影響後續格式化）。
		stmt := statements[0]
		var origDoc *parser.CommentGroup
		if d, ok := stmt.(interface {
			GetDoc() *parser.CommentGroup
			SetDoc(*parser.CommentGroup)
		}); ok {
			origDoc = d.GetDoc()
			d.SetDoc(nil)
		}
		f.formatStatement(stmt)
		if d, ok := stmt.(interface {
			GetDoc() *parser.CommentGroup
			SetDoc(*parser.CommentGroup)
		}); ok && origDoc != nil {
			d.SetDoc(origDoc)
		}
		return
	}
	// 多語句 body：用 { } 大括號包裹
	f.write(" {")
	// Output opening brace comment on the same line as {
	if obc := f.obcOf(a.body); obc != nil && len(obc.List) > 0 {
		if os.Getenv("NOLANG_FMTDBG") != "" {
			fmt.Fprintf(os.Stderr, "[DBG] writeArm obcOf(a.body)=%q\n", obc.List[0].Text)
		}
		f.write("; ")
		for _, c := range obc.List {
			f.write(strings.TrimSpace(c.Text))
		}
	}
	f.indent++
	f.formatBlockInner(a.body, 0)
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatElifChain(alt *parser.BlockStatement) {
	if f.isElifBlock(alt) {
		ifExpr := alt.Statements[0].(*parser.ExpressionStatement).Expression.(*parser.IfExpression)
		f.write(" elif ")
		f.formatExpression(ifExpr.Condition)
		f.write(" {")
		f.indent++
		f.formatBlockInner(ifExpr.Consequence, ifExpr.Consequence.Token.Line)
		f.indent--
		f.newline()
		f.write("}")
		if ifExpr.Alternative != nil {
			f.formatElifChain(ifExpr.Alternative)
		}
	} else {
		f.write(" else {")
		f.indent++
		f.formatBlockInner(alt, alt.Token.Line)
		f.indent--
		f.newline()
		f.write("}")
	}
}

func (f *formatter) isElifBlock(bs *parser.BlockStatement) bool {
	if len(bs.Statements) != 1 {
		return false
	}
	es, ok := bs.Statements[0].(*parser.ExpressionStatement)
	if !ok {
		return false
	}
	ifExpr, ok := es.Expression.(*parser.IfExpression)
	if !ok {
		return false
	}
	// 顯式標記：parser 在 parseElifBlock 中寫入 RTElif（語義副表），
	// 直接讀取此標誌避免啟發式推斷。
	return f.hasRT(ifExpr, parser.RTElif)
}

// writeLoopBodyBlock 輸出循環主體 "{\n  stmts\n}"，保留語句間的空行與文檔註釋。

// writeLoopBodyBlock 輸出循環主體 "{\n  stmts\n}"，保留語句間的空行與文檔註釋。
func (f *formatter) writeLoopBodyBlock(s *parser.ForStatement) {
	f.write("{")
	f.indent++
	f.formatBlockInner(s.Body, s.Body.Token.Line)
	f.indent--
	f.newline()
	f.write("}")
}

// writeLoopBodyAfterColon 輸出循環主體（冒號之後的部分）。
// 當 body 是單條 inline 語句（IsInline=true，無 trailing comments）時，
// 輸出 " stmt"（單行，不加 {}）；否則輸出 " {\n  stmts\n}"（block 形式）。

// writeLoopBodyAfterColon 輸出循環主體（冒號之後的部分）。
// 當 body 是單條 inline 語句（IsInline=true，無 trailing comments）時，
// 輸出 " stmt"（單行，不加 {}）；否則輸出 " {\n  stmts\n}"（block 形式）。
func (f *formatter) writeLoopBodyAfterColon(s *parser.ForStatement) {
	if s.Body != nil && s.Body.IsInline && len(s.Body.Statements) == 1 && s.Body.TrailingComments == nil {
		f.write(" ")
		f.formatStatement(s.Body.Statements[0])
		return
	}
	f.write(" {")
	f.indent++
	f.formatBlockInner(s.Body, s.Body.Token.Line)
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatForStatement(s *parser.ForStatement) {
	if s.Label != "" {
		f.write("#")
		f.write(s.Label)
		f.write(" ")
	}

	// Bare range-for: i <- [a..b]: { body } — when token type is not FOR and IterRange set
	if s.Token.Type != lexer.FOR && s.IterRange != nil && s.IterRange.Variable != "" {
		f.write(s.IterRange.Variable)
		f.write(" <- ")
		if s.IterRange.RangeStr != "" {
			f.write(s.IterRange.RangeStr)
		} else if s.IterRange.Range != nil {
			if s.IterRange.Range.LeftInc {
				f.write("[")
			} else {
				f.write("(")
			}
			f.formatExpression(s.IterRange.Range.Start)
			f.write("..")
			f.formatExpression(s.IterRange.Range.End)
			if s.IterRange.Range.RightInc {
				f.write("]")
			} else {
				f.write(")")
			}
		} else if s.IterRange.RangeExpr != nil {
			f.formatExpression(s.IterRange.RangeExpr)
		} else {
			f.write("?")
		}
		f.write(":")
		f.writeLoopBodyAfterColon(s)
		return
	}

	// Counted loop: N * { body }（前置式，預設）或 { body } * N（後置式）
	if s.CountExpr != nil && s.Token.Type != lexer.FOR {
		if f.loopStyle == LoopStyleSuffix {
			f.writeLoopBodyBlock(s)
			f.write(" * ")
			f.formatExpression(s.CountExpr)
			return
		}
		f.formatExpression(s.CountExpr)
		f.write(" * ")
		f.writeLoopBodyBlock(s)
		return
	}

	// for 關鍵字形式的 range-for（已廢棄，向後相容輸出）：for i <- [a..b]: { body }
	if s.Token.Type == lexer.FOR && s.IterRange != nil && s.IterRange.Variable != "" {
		f.write("for ")
		f.write(s.IterRange.Variable)
		f.write(" <- ")
		if s.IterRange.RangeStr != "" {
			f.write("'")
			f.write(s.IterRange.RangeStr)
			f.write("'")
		} else if ident, ok := s.IterRange.RangeExpr.(*parser.Identifier); ok {
			f.write(ident.Value)
		} else if sliceLit, ok := s.IterRange.RangeExpr.(*parser.SliceLiteral); ok {
			f.formatSliceLiteral(sliceLit)
		} else {
			f.formatRangeBrackets(s.IterRange.Range)
		}
		f.write(":")
		f.writeLoopBodyAfterColon(s)
		return
	}

	// C-style for（已廢棄，向後相容輸出）：for init, cond, update { body }
	if s.Token.Type == lexer.FOR && s.Init != nil {
		f.write("for ")
		f.formatStatement(s.Init)
		f.write(", ")
		f.formatExpression(s.Condition)
		f.write(", ")
		f.formatStatement(s.Update)
		f.write(" {")
		f.indent++
		f.formatBlockInner(s.Body, s.Body.Token.Line)
		f.indent--
		f.newline()
		f.write("}")
		return
	}

	// 條件循環（while-style）：{ body } (cond)
	// 涵蓋新式 { } (cond)、舊式 for cond { }、以及標籤條件包裝器
	// (#N cond: { } → ForStatement{Condition: *IfExpression, Body: Consequence})。
	// 解開合成的 IfExpression 以輸出原始條件。
	// 直接讀取 IsCondWrapper 欄位（parser 在合成位置顯式設置），
	// 避免依賴 `s.Body == ifExpr.Consequence` 指標相等啟發式。
	cond := s.Condition
	if s.IsCondWrapper {
		if ifExpr, ok := cond.(*parser.IfExpression); ok {
			cond = ifExpr.Condition
		}
	}
	// 布林字面量條件：前置式輸出 `!! { }`（恆真）/ `! { }`（恆假，不執行）；
	// 後置式輸出 `{ } (true)` / `{ } ()`（空括號代表 false，不執行）。
	if bl, ok := cond.(*parser.BooleanLiteral); ok {
		if f.loopStyle == LoopStyleSuffix {
			f.writeLoopBodyBlock(s)
			if bl.Value {
				f.write(" (true)")
			} else {
				f.write(" ()")
			}
			return
		}
		if bl.Value {
			f.write("!! ")
		} else {
			f.write("! ")
		}
		f.writeLoopBodyBlock(s)
		return
	}
	// 一般條件：前置式 `(cond) { }`；後置式 `{ } (cond)`。
	if cond != nil {
		if f.loopStyle == LoopStyleSuffix {
			f.writeLoopBodyBlock(s)
			f.write(" (")
			f.formatExpression(cond)
			f.write(")")
			return
		}
		f.write("(")
		f.formatExpression(cond)
		f.write(") ")
		f.writeLoopBodyBlock(s)
		return
	}

	// 無條件 → 無限循環。前置式 `!! { }`；後置式 `{ } (true)`
	// 涵蓋舊式 !! { } / for { }（Condition 保持 nil 的歷史路徑，現已改為 BooleanLiteral{true}）。
	// 空括號 () 代表 false（不執行），因此無限循環必須顯式輸出 (true)。
	if f.loopStyle == LoopStyleSuffix {
		f.writeLoopBodyBlock(s)
		f.write(" (true)")
		return
	}
	f.write("!! ")
	f.writeLoopBodyBlock(s)
}

func (f *formatter) formatRangeBrackets(re *parser.RangeExpression) {
	if re.LeftInc {
		f.write("[")
	} else {
		f.write("(")
	}
	f.formatExpression(re.Start)
	f.write("..")
	f.formatExpression(re.End)
	if re.RightInc {
		f.write("]")
	} else {
		f.write(")")
	}
}
