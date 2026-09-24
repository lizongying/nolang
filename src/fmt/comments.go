package fmt

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// docStartLine returns the first line of the Doc comment before a statement, or 0.
func docStartLine(stmt parser.Statement) int {
	if d, ok := stmt.(interface{ GetDoc() *parser.CommentGroup }); ok {
		doc := d.GetDoc()
		if doc != nil && len(doc.List) > 0 {
			return doc.List[0].Pos.Line
		}
	}
	return 0
}

// stmtFirstLine returns the first source line of a statement (including Doc comments).

// stmtFirstLine returns the first source line of a statement (including Doc comments).
// For a standalone AnnotationStatement, we deliberately ignore its Doc comment when
// computing the gap: the parser re-attaches a preceding line comment as the
// annotation's Doc on every re-parse (so the comment survives a round-trip), but the
// comment is emitted *above* the `#{...}` token. Treating the Doc as the statement's
// start would shift the gap calculation upward and inject a spurious blank line
// between the previous statement and the annotation — breaking idempotency. The gap
// is therefore measured to the `#{...}` token line, which is stable across passes.
func stmtFirstLine(stmt parser.Statement) int {
	if _, ok := stmt.(*parser.AnnotationStatement); ok {
		return stmtTokenLine(stmt)
	}
	if l := docStartLine(stmt); l > 0 {
		return l
	}
	return stmtTokenLine(stmt)
}

// stmtTokenEndLine returns the line of the last source token in a statement (1-based).
// It delegates to the AST node's EndPos(), which already accounts for multi-line
// constructs (blocks, multi-line calls, struct literals, ...). This replaces the
// previous hand-maintained per-type switch, which could drift from the parser and
// silently mis-handle blank-line detection after multi-line statements.

// stmtTokenEndLine returns the line of the last source token in a statement (1-based).
// It delegates to the AST node's EndPos(), which already accounts for multi-line
// constructs (blocks, multi-line calls, struct literals, ...). This replaces the
// previous hand-maintained per-type switch, which could drift from the parser and
// silently mis-handle blank-line detection after multi-line statements.
func stmtTokenEndLine(stmt parser.Statement) int {
	if stmt == nil {
		return 0
	}
	return stmt.EndPos().Line
}

// stmtExprEndLine returns the end line of an expression, delegating to EndPos().
// This replaces the previous per-type switch; EndPos() already recurses into the
// correct sub-expression (last argument, alternative branch, value, ...) so the
// result is accurate for every multi-line shape.

// stmtExprEndLine returns the end line of an expression, delegating to EndPos().
// This replaces the previous per-type switch; EndPos() already recurses into the
// correct sub-expression (last argument, alternative branch, value, ...) so the
// result is accurate for every multi-line shape.
func stmtExprEndLine(expr parser.Expression) int {
	if expr == nil {
		return 0
	}
	return expr.EndPos().Line
}

// formatDocComments outputs comment lines that serve as Doc for a statement.

// formatDocComments outputs comment lines that serve as Doc for a statement.
func (f *formatter) formatDocComments(doc *parser.CommentGroup) {
	if doc == nil {
		return
	}
	for i, c := range doc.List {
		if i > 0 {
			prevLine := doc.List[i-1].Pos.Line
			if c.Pos.Line > prevLine+1 {
				// Blank line between comment groups: output blank line then indent
				f.write("\n")
				f.newline()
			} else {
				f.newline()
			}
		}
		f.writeCommentBody(c)
	}
}

// commentMarker returns the comment start symbol for a comment.
// `//` comments are normalized to `;` on output.

// commentMarker returns the comment start symbol for a comment.
// `//` comments are normalized to `;` on output.
func (f *formatter) commentMarker(c *parser.Comment) string {
	if c.Marker == "" || c.Marker == "//" {
		return ";"
	}
	return c.Marker
}

// writeCommentBody writes a single comment with its marker(s).
// Block comments (Marker ";;block") use strict format: opening ";;" and closing ";;"
// each on their own line (opening ;; must be followed by a newline to trigger
// multiline mode). Line comments (Marker "//", ";", or ";;" without newline)
// normalize the space after the marker.

// writeCommentBody writes a single comment with its marker(s).
// Block comments (Marker ";;block") use strict format: opening ";;" and closing ";;"
// each on their own line (opening ;; must be followed by a newline to trigger
// multiline mode). Line comments (Marker "//", ";", or ";;" without newline)
// normalize the space after the marker.
func (f *formatter) writeCommentBody(c *parser.Comment) {
	if c.Marker == ";;block" {
		f.write(";;")
		// Ensure text starts on a new line
		text := c.Text
		if len(text) == 0 || text[0] != '\n' {
			f.write("\n")
		}
		f.write(text)
		// Ensure closing ;; is on its own line
		if len(text) == 0 || text[len(text)-1] != '\n' {
			f.write("\n")
		}
		f.write(";;")
		return
	}
	m := f.commentMarker(c)
	text := c.Text
	if strings.TrimSpace(text) == "" {
		f.write(m)
	} else if len(text) > 0 && text[0] != ' ' {
		// Missing space after marker — add one
		f.write(m + " ")
		f.write(text)
	} else {
		// Already has space — preserve as-is
		f.write(m)
		f.write(text)
	}
}

// formatInlineComment outputs a comment that appears on the same line as code.

// formatInlineComment outputs a comment that appears on the same line as code.
func (f *formatter) formatInlineComment(comment *parser.CommentGroup) {
	if comment == nil || len(comment.List) == 0 {
		return
	}
	c := comment.List[0]
	if c.Marker == ";;block" {
		// 塊註釋不應出現在行內（行內 ;; 必為單行）；
		// 但防禦性處理：按塊格式輸出
		f.write(" ;;")
		f.write(c.Text)
		f.write(";;")
		return
	}
	if c.Marker == ";" || c.Marker == "" || c.Marker == "//" {
		// `;` 單行註釋緊貼代碼：a = 1; comment
		// `//` comments are normalized to `;` on output.
		f.write("; ")
	} else if c.Marker == ";;" {
		// `;;` 單行註釋緊貼代碼：a = 1;; comment
		f.write(";; ")
	} else {
		f.write("  // ")
	}
	f.write(strings.TrimSpace(c.Text))
}

// formatTrailingComments outputs comments that appear before a closing brace.
// prevEndLine is the source line of the last emitted statement (or the block's
// opening brace when the block has no statements beforehand); it anchors
// blank-line detection so a separating blank between the preceding code and a
// trailing comment (and between consecutive trailing comments) is preserved.
// Without it the blank would always collapse, making it impossible to keep
// `stmt\n\n; comment\n}`.
func (f *formatter) formatTrailingComments(tc *parser.CommentGroup, prevEndLine int) {
	if tc == nil {
		return
	}
	prevLine := prevEndLine
	for _, c := range tc.List {
		// 保留源碼中「上一段程式碼與本註釋之間」以及「兩個註釋之間」的空行。
		// 用 hasBlankLineBetween 偵測真實空白行（並略過 overflow 註解行），
		// 而非單純行號落差 —— 後者會在 formatter 自身輸出推移行號後誤判、破壞冪等。
		if prevLine > 0 && c.Pos.Line > 0 && f.hasBlankLineBetween(prevLine, c.Pos.Line) {
			f.write("\n") // blank line (no indent)
		}
		f.newline()
		f.writeCommentBody(c)
		prevLine = c.Pos.Line
	}
}

// hasBlankLineBetween checks if there is a blank line between two source positions.

// hasBlankLineBetween checks if there is a blank line between two source positions.
// A blank line that immediately follows an overflow annotation (e.g. the line left
// after `#{overflow=wrap}`) is part of the formatter's own emitted layout, not a
// genuine separative blank between two statements: the formatter re-emits the
// annotation and such a following blank on every pass, so counting it would make
// blank detection non-idempotent (a blank would be added, then re-detected, then
// added again). We therefore skip blank lines directly after an overflow annotation.
func (f *formatter) hasBlankLineBetween(prevEndLine, currStartLine int) bool {
	if prevEndLine <= 0 || currStartLine <= 0 || currStartLine <= prevEndLine+1 {
		return false
	}
	prevWasOverflow := false
	for lineNum := prevEndLine + 1; lineNum < currStartLine; lineNum++ {
		idx := lineNum - 1
		if idx >= len(f.sourceLines) {
			continue
		}
		trimmed := strings.TrimSpace(f.sourceLines[idx])
		// overflow 註解行（#{...overflow...}）本身不算分隔空白，且其後緊鄰的空白行
		// 屬 formatter 自身輸出佈局，亦不計入。
		if strings.HasPrefix(trimmed, "#{") && strings.Contains(trimmed, "overflow") {
			prevWasOverflow = true
			continue
		}
		if trimmed == "" {
			if prevWasOverflow {
				prevWasOverflow = false
				continue
			}
			return true
		}
		prevWasOverflow = false
	}
	return false
}

func (f *formatter) hasDocComment(stmt parser.Statement) bool {
	if d, ok := stmt.(interface{ GetDoc() *parser.CommentGroup }); ok {
		doc := d.GetDoc()
		return doc != nil && len(doc.List) > 0
	}
	return false
}

// stmtTokenLine returns the first source line of a statement (1-based).
// Every statement's Pos() is its leading token line, so delegating to Pos()
// reproduces the previous per-type switch exactly without the maintenance cost.
func stmtTokenLine(stmt parser.Statement) int {
	if stmt == nil {
		return 0
	}
	return stmt.Pos().Line
}

// attachedAnnotations returns annotations attached to a statement by the parser
// (e.g. platform annotations #{mac-arm64}, #{linux-amd64} attached via attachAnnotations),
// read from the semantic side-table.
func (f *formatter) attachedAnnotations(stmt parser.Statement) []*parser.AnnotationEntry {
	switch stmt.(type) {
	case *parser.LetStatement, *parser.FunctionDefinition, *parser.StructDefinition, *parser.ExpressionStatement,
		*parser.ForStatement, *parser.MultiAssignStatement, *parser.ReturnStatement,
		*parser.TaggedEnumDefinition, *parser.EnumDefinition, *parser.InterfaceDefinition:
		all := f.sem.AnnotationsOf(stmt)
		// `#{index-out = ...}` 有兩種來源：
		//  1) 獨立成行寫在本陳述正上方：parser 對 IDENT 起始的陳述會把註解直接
		//     attachAnnotations 到本陳述（Propagated=false、無獨立節點），此時**必須**
		//     由附加註解路徑輸出，否則 `no fmt -w` 會整條遺失越界保護。
		//  2) 由上一行獨立 AnnotationStatement 節點、或 for 迴圈表頭，經
		//     applyLineIndexOutAnnotations 複製進本陳述 side-table 的副本
		//     （Propagated=true）：其顯示已由該節點/表頭負責，此处再印一次會雙印、
		//     非冪等，故只過濾掉 Propagated 副本。
		// 尾隨寫法（`stmt #{index-out=0}`）無獨立節點，永遠保留。
		if len(all) == 0 {
			return nil
		}
		filtered := all[:0:0]
		for _, e := range all {
			if e != nil && e.Key == "index-out" && !e.Trailing && e.Propagated {
				continue
			}
			filtered = append(filtered, e)
		}
		return filtered
	}
	return nil
}

// hasAttachedAnnotations reports whether the statement has attached annotations
// that will be rendered as a #{...} line before the statement body.
func (f *formatter) hasAttachedAnnotations(stmt parser.Statement) bool {
	return len(f.attachedAnnotations(stmt)) > 0
}

// attachedAnnotationsWillEmit reports whether formatting this statement will emit
// a `#{...}` line before its body — any non-trailing entry (platform / generic /
// overflow / index-out). It is used by the gap logic to decide whether to insert
// a separating blank line.
//
// 只要陳述上方會印出獨立一行註解，就必須與上方程式碼以一個空行分隔：整組
// 「註釋 → 註解 → 陳述」是一個 node（見 nodeHeadWillEmit），node 之間以空行分隔。
// overflow / index-out 也是行注解、同樣佔用陳述上方的一行，故一併計入。
func (f *formatter) attachedAnnotationsWillEmit(stmt parser.Statement) bool {
	for _, e := range f.attachedAnnotations(stmt) {
		if e.Trailing {
			// 尾隨註解輸出在陳述同一行後方，不佔用上方獨立行，故不觸發間隙
			// （否則會在陳述前插入空行、破壞冪等）。
			continue
		}
		if e.Key == "overflow" && !f.overflowEntryEffective(stmt) {
			// 無效 overflow（管轄陳述不含整數運算）不會真正輸出，故不觸發間隙，
			// 避免在陳述前留下空行（否則 formatStatement 實際未印出、產生空白行）。
			continue
		}
		return true
	}
	return false
}

// annotationNodeWillEmit reports whether formatting stmt starts with a `#{...}`
// line of its own — either a standalone AnnotationStatement node or one emitted
// from the statement's attached annotations.
func (f *formatter) annotationNodeWillEmit(stmt parser.Statement) bool {
	if as, ok := stmt.(*parser.AnnotationStatement); ok {
		return f.annotationStatementEmits(as)
	}
	return f.attachedAnnotationsWillEmit(stmt)
}

// nodeHeadWillEmit reports whether formatting stmt writes anything *above* its own
// body — a doc comment and/or an annotation line. Such a statement is the
// 「註釋 + 註解 + 陳述」node the layout keeps separated by a blank line from what
// precedes it (see the gap logic in formatBlockInner / formatProgram).
func (f *formatter) nodeHeadWillEmit(stmt parser.Statement) bool {
	return f.hasDocComment(stmt) || f.annotationNodeWillEmit(stmt)
}

// attachedAnnotationsWillEmitAny 保留為 attachedAnnotationsWillEmit 的別名：內聯
// 守衛（isStandaloneInline / writeBareMatchArm 的 canInline）要求「只要上方會有一行
// 註解就不得內聯」。
//
// 內聯會把註解寫成 `cond -> #{overflow=wrap}`，而解析器會把 `#{...}` 當成 arm
// 本體、二次格式化再產生 `overflow = wrap` 之類的錯亂（非冪等）。因此只要該陳述
// 上方會有一行註解，就必須強制區塊形式 `cond -> { #{...} ... }`。
func (f *formatter) attachedAnnotationsWillEmitAny(stmt parser.Statement) bool {
	return f.attachedAnnotationsWillEmit(stmt)
}

// overflowModeStringOf 從一個 overflow 註解條目取出正規化模式字串
// （wrap/clamp0/min/max/saturate）；非 overflow 條目、無值或無法識別時回傳 ""。
func overflowModeStringOf(e *parser.AnnotationEntry) string {
	if e == nil || e.Key != "overflow" || e.Value == nil {
		return ""
	}
	switch v := e.Value.(type) {
	case *parser.AnnotationIdentValue:
		return parser.NormalizeOverflowMode(v.Value)
	case *parser.AnnotationStringValue:
		return parser.NormalizeOverflowMode(v.Value)
	}
	return ""
}

// emitOverflowAnnotation 輸出一個 overflow 註解行。`#{overflow=...}` 現為行注解
// （見 parser.applyLineOverflowAnnotations），與其下方陳述一一對應，因此**不再**
// 去重折疊——每一處顯式注解都原樣輸出，避免 formatter 靜默刪除注解造成整數
// 語意漂移。回傳是否實際輸出。overflow 與平台/泛型註解分屬不同行：本函式只負責
// overflow 行，呼叫方處理 others 行。
//
// trailingNewline 控制是否在註解行末輸出換行：
//   - 附加路徑（formatStatement 中已掛載到某陳述的 overflow）：設 true，
//     使該陳述自身的內容（如 `key = .[i]`）落在下一行。
//   - 獨立註解陳述路徑（formatAnnotationStatement）：設 false，使註解以
//     「結尾即內容」形式結束（不帶尾隨換行），由後續的間隙邏輯統一負責換行。
func (f *formatter) emitOverflowAnnotation(mode string, trailingNewline bool) bool {
	if mode == "" {
		return false
	}
	f.write("#{overflow=")
	f.write(mode)
	f.write("}")
	if trailingNewline {
		f.newline()
	}
	return true
}

// lowerHexLiteral converts an uppercase hex literal to lowercase.
// Handles two forms:
//   - "0xFF" / "0XFF" → "0xff"  (integer hex literal)
//   - "xFF"            → "xff"   (byte literal)
//
// Non-hex literals are returned unchanged.
