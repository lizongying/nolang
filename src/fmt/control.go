package fmt

import (
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
	ifExpr, ok := es.Expression.(*parser.IfExpression)
	if !ok {
		return false
	}
	return f.hasRT(ifExpr, parser.RTBareMatch)
}

func (f *formatter) formatIfExpression(e *parser.IfExpression) {
	// Standalone if-then: `cond -> body` (without enclosing { })
	if f.hasRT(e, parser.RTStandalone) {
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

	// 裸 match 表達式 `{ cond -> body }` 輸出新式語法
	if f.hasRT(e, parser.RTBareMatch) {
		f.formatBareMatchExpression(e)
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

// formatBareMatchExpression 將裸 match 鏈（IfExpression desugar）格式化為
// 新式語法 `{ cond -> body }`。
// 鏈的結構由 buildBareMatchDesugar 產生：
//   - 若最後一個 arm 是 wildcard（else），頂層 IfExpression 可能會被包裝在
//     ExpressionStatement 內。
//   - 對於非 wildcard arm，Alternative 為 BlockStatement{ExpressionStatement{next IfExpression}}
//   - 對於 wildcard arm，Alternative 為直接的 BlockStatement

// formatBareMatchExpression 將裸 match 鏈（IfExpression desugar）格式化為
// 新式語法 `{ cond -> body }`。
// 鏈的結構由 buildBareMatchDesugar 產生：
//   - 若最後一個 arm 是 wildcard（else），頂層 IfExpression 可能會被包裝在
//     ExpressionStatement 內。
//   - 對於非 wildcard arm，Alternative 為 BlockStatement{ExpressionStatement{next IfExpression}}
//   - 對於 wildcard arm，Alternative 為直接的 BlockStatement
func (f *formatter) formatBareMatchExpression(e *parser.IfExpression) {
	// RTMatchWrapper: rawCond 包裝層 `if 1 { it = matched; <if-chain> }`
	// 跳過包裝層，直接格式化內部的 if-chain。
	if f.hasRT(e, parser.RTMatchWrapper) {
		for _, stmt := range e.Consequence.Statements {
			if es, ok := stmt.(*parser.ExpressionStatement); ok {
				if inner, ok := es.Expression.(*parser.IfExpression); ok && f.hasRT(inner, parser.RTBareMatch) {
					f.formatBareMatchExpression(inner)
					return
				}
			}
		}
	}
	if e.MatchedExpr != nil {
		f.formatExpression(e.MatchedExpr)
		f.write(": {")
	} else {
		f.write("{")
	}
	// Output opening brace comment on the same line as {
	if obc := f.obcOf(e); obc != nil && len(obc.List) > 0 {
		f.write("; ")
		for _, c := range obc.List {
			f.write(strings.TrimSpace(c.Text))
		}
	}
f.indent++
	// 輸出當前 arm
	f.writeBareMatchArm(e)
	// 處理後續 arm（Alternative 鏈）
	for e.Alternative != nil {
		if len(e.Alternative.Statements) == 1 {
			if es, ok := e.Alternative.Statements[0].(*parser.ExpressionStatement); ok {
				if next, ok := es.Expression.(*parser.IfExpression); ok && f.hasRT(next, parser.RTBareMatch) {
					e = next
					f.write("\n")
					f.writeBareMatchArm(e)
					continue
				}
			}
		}
		// Wildcard arm 的 Alternative 是直接的 BlockStatement
		// 模擬 IfExpression 包裝後調用 writeBareMatchArm；
		// 標誌記入 formatter 本地合成表，不污染 program.Sem。
		wildcardIf := &parser.IfExpression{
			Token:       e.Token,
			Condition:   &parser.IntegerLiteral{Token: e.Token, Value: 1},
			Consequence: e.Alternative,
		}
		if f.synthRT == nil {
			f.synthRT = make(map[*parser.IfExpression]parser.RTFlag)
		}
		f.synthRT[wildcardIf] = parser.RTBareMatch | parser.RTMatchWildcard
		if e.DotValBody == e.Alternative {
			wildcardIf.DotValBody = e.Alternative
		}
		f.write("\n")
		f.writeBareMatchArm(wildcardIf)
		break
	}
	f.indent--
	f.newline()
	f.write("}")
}

// writeBareMatchArm 輸出單個 arm。
// 對於非 wildcard：cond -> body
// 對於 wildcard：-> body
// 若 body 只有一個簡單語句（ExpressionStatement / LetStatement / ReturnStatement 等）且無註釋，
// 內聯輸出在同一行；若 body 有多個語句，用 { } 大括號包裹。

// writeBareMatchArm 輸出單個 arm。
// 對於非 wildcard：cond -> body
// 對於 wildcard：-> body
// 若 body 只有一個簡單語句（ExpressionStatement / LetStatement / ReturnStatement 等）且無註釋，
// 內聯輸出在同一行；若 body 有多個語句，用 { } 大括號包裹。
func (f *formatter) writeBareMatchArm(e *parser.IfExpression) {
	// 過濾 compiler 注入的合成語句（如 `it = matched`）
	statements := make([]parser.Statement, 0, len(e.Consequence.Statements))
	for _, stmt := range e.Consequence.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsSynthetic {
			continue
		}
		statements = append(statements, stmt)
	}

	// 判斷是否為可內聯的簡單 body：單語句、無塊級註釋、類型為表達式/let/return/break/continue。
	// 注意：body 語句的 doc comment（arm 前的註釋，如 `; F grade`）不阻止內聯，
	// 該註釋會在 arm 條件之前獨立行輸出。
	// 使用 IsInline 標誌（由 parser 設置）區分用戶寫的 block `{ }` 與 inline 單語句。
	// 例外：若 body 是 bare match 表達式（本身就是 `{ }` 形式），內聯不改變其輸出形式。
	canInline := (e.Consequence.IsInline || f.isBareMatchBody(statements)) &&
		len(statements) == 1 &&
		e.Consequence.TrailingComments == nil &&
		e.Consequence.ClosingBraceComment == nil &&
		f.obcOf(e.Consequence) == nil
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
		if e.Consequence.IsInline {
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

	// If body has TrailingComments that are OUTSIDE the block (collected
	// before the opening brace), they are arm-level doc comments. Output
	// them on their own lines BEFORE the condition.
	// Comments INSIDE the block (between { and }) are real block content
	// and must stay in the block.
	if e.Consequence.TrailingComments != nil &&
		e.Consequence.ClosingBraceComment == nil &&
		f.obcOf(e.Consequence) == nil {
		openLine := e.Consequence.Token.Line
		var outside, inside []*parser.Comment
		for _, c := range e.Consequence.TrailingComments.List {
			if c.Pos.Line < openLine {
				outside = append(outside, c)
			} else {
				inside = append(inside, c)
			}
		}
		// Output outside comments before the condition
		for _, c := range outside {
			f.writeCommentBody(c)
			f.newline()
		}
		// If there are inside comments, keep them in the block
		if len(inside) > 0 {
			e.Consequence.TrailingComments = &parser.CommentGroup{List: inside}
		} else {
			e.Consequence.TrailingComments = nil
		}
	}

	if f.hasRT(e, parser.RTMatchWildcard) {
		if e.DotValBody != nil && e.DotValBody == e.Consequence {
			f.write("ok ->")
		} else {
			f.write("->")
		}
	} else if e.MatchedExpr != nil {
		// 按 pattern 類型依序檢查顯式欄位，避免對 desugar 後的 Condition 做啟發式推斷
		switch {
		case e.RangePattern != nil:
			// [a..b) 等 range pattern
			f.formatRangeBrackets(e.RangePattern)
			f.write(" ->")
		case len(e.OptionPatterns) > 0:
			// nil || err 等 option pattern 列表
			f.write(strings.Join(e.OptionPatterns, " || "))
			f.write(" ->")
		case len(e.ValuePatterns) > 0:
			// 1 || 3 || 5 等 multi-value pattern 列表
			for i, vp := range e.ValuePatterns {
				if i > 0 {
					f.write(" || ")
				}
				f.formatExpression(vp)
			}
			f.write(" ->")
		case e.RawCond != nil:
			// ok(cond) raw cond arm
			f.write("ok(")
			f.formatExpression(e.RawCond)
			f.write(") ->")
		case e.EqualityPattern != nil:
			// matched == X 等值 pattern，直接輸出 X
			f.formatExpression(e.EqualityPattern)
			f.write(" ->")
		default:
			// 無顯式欄位：回退到格式化 Condition（向後相容）
			f.formatExpression(e.Condition)
			f.write(" ->")
		}
	} else {
		f.formatExpression(e.Condition)
		f.write(" ->")
	}

	// 空 body（如 wildcard `->`）：不輸出任何內容。
	// 但當 body 是顯式 block（IsInline=false，如 `cond -> {}`）時，
	// 保留空 {} 輸出以保持源碼形式。
	if len(statements) == 0 &&
		e.Consequence.IsInline &&
		e.Consequence.TrailingComments == nil &&
		e.Consequence.ClosingBraceComment == nil &&
		f.obcOf(e.Consequence) == nil {
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
	if obc := f.obcOf(e.Consequence); obc != nil && len(obc.List) > 0 {
		f.write("; ")
		for _, c := range obc.List {
			f.write(strings.TrimSpace(c.Text))
		}
	}
	f.indent++
	f.formatBlockInner(e.Consequence, 0)
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

	// Counted loop: { body } * N（新式語法；Token 非 FOR）
	if s.CountExpr != nil && s.Token.Type != lexer.FOR {
		f.write("{")
		f.indent++
		f.formatBlockInner(s.Body, s.Body.Token.Line)
		f.indent--
		f.newline()
		f.write("} * ")
		f.formatExpression(s.CountExpr)
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
	// BooleanLiteral{Value: false} → 空括號 ()（不執行）
	if bl, ok := cond.(*parser.BooleanLiteral); ok && !bl.Value {
		f.writeLoopBodyBlock(s)
		f.write(" ()")
		return
	}
	if cond != nil {
		f.writeLoopBodyBlock(s)
		f.write(" (")
		f.formatExpression(cond)
		f.write(")")
		return
	}

	// 無限循環：{ body } (true)
	// 涵蓋舊式 !! { } / for { }（Condition 保持 nil 的歷史路徑，現已改為 BooleanLiteral{true}）。
	// 空括號 () 代表 false（不執行），因此無限循環必須顯式輸出 (true)。
	f.writeLoopBodyBlock(s)
	f.write(" (true)")
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
