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
func stmtFirstLine(stmt parser.Statement) int {
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

// formatTrailingComments outputs comments that appear before a closing brace.
func (f *formatter) formatTrailingComments(tc *parser.CommentGroup) {
	if tc == nil {
		return
	}
	for _, c := range tc.List {
		f.newline()
		f.writeCommentBody(c)
	}
}

// hasBlankLineBetween checks if there is a blank line between two source positions.

// hasBlankLineBetween checks if there is a blank line between two source positions.
func (f *formatter) hasBlankLineBetween(prevEndLine, currStartLine int) bool {
	if prevEndLine <= 0 || currStartLine <= 0 || currStartLine <= prevEndLine+1 {
		return false
	}
	for lineNum := prevEndLine + 1; lineNum < currStartLine; lineNum++ {
		idx := lineNum - 1
		if idx < len(f.sourceLines) && strings.TrimSpace(f.sourceLines[idx]) == "" {
			return true
		}
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
	case *parser.LetStatement, *parser.FunctionDefinition, *parser.StructDefinition, *parser.ExpressionStatement:
		return f.sem.AnnotationsOf(stmt)
	}
	return nil
}

// lowerHexLiteral converts an uppercase hex literal to lowercase.
// Handles two forms:
//   - "0xFF" / "0XFF" → "0xff"  (integer hex literal)
//   - "xFF"            → "xff"   (byte literal)
//
// Non-hex literals are returned unchanged.
