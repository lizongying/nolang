package fmt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

type Formatter struct{}

func NewFormatter() *Formatter {
	return &Formatter{}
}

type formatter struct {
	buf         strings.Builder
	indent      int
	sourceLines []string // original source lines (for blank line detection)
	column      int      // current output column (0-based)
	stringAlign int      // alignment column for multi-line string concat continuation lines
}

func (f *formatter) writeIndent() {
	indent := strings.Repeat("    ", f.indent)
	f.buf.WriteString(indent)
	f.column = len(indent)
}

func (f *formatter) write(s string) {
	f.buf.WriteString(s)
	f.column += len(s)
}

func (f *formatter) writef(format string, args ...interface{}) {
	f.buf.WriteString(fmt.Sprintf(format, args...))
}

func (f *formatter) newline() {
	f.buf.WriteString("\n")
	f.writeIndent()
}

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
func stmtFirstLine(stmt parser.Statement) int {
	if l := docStartLine(stmt); l > 0 {
		return l
	}
	return stmtTokenLine(stmt)
}

// stmtTokenEndLine returns the line of the last token in a statement (1-based).
func stmtTokenEndLine(stmt parser.Statement) int {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return s.Token.Line
	case *parser.UseStatement:
		return s.Token.Line
	case *parser.ExportStatement:
		return s.Token.Line
	case *parser.ReturnStatement:
		return s.Token.Line
	case *parser.ExpressionStatement:
		return stmtExprEndLine(s.Expression)
	case *parser.FunctionDefinition:
		return s.Body.Token.Line
	case *parser.ForStatement:
		return s.Body.Token.Line
	case *parser.BreakStatement:
		return s.Token.Line
	case *parser.ContinueStatement:
		return s.Token.Line
	case *parser.BlockStatement:
		return s.Token.Line
	case *parser.EnumDefinition:
		return s.Token.Line
	case *parser.TaggedEnumDefinition:
		return s.Token.Line
	case *parser.InterfaceDefinition:
		return s.Token.Line
	case *parser.StructDefinition:
		return s.Token.Line
	case *parser.TypeAlias:
		if s.Union != nil {
			return s.Union.EndPos().Line
		}
		if s.Type != nil {
			return s.Type.EndPos().Line
		}
		return s.Token.Line
	case *parser.ExternStatement:
		return s.EndPos().Line
	case *parser.AnnotationStatement:
		return s.Token.Line
	}
	return 0
}

// stmtExprEndLine returns the end line of an expression.
func stmtExprEndLine(expr parser.Expression) int {
	switch e := expr.(type) {
	case *parser.Identifier:
		return e.Token.Line
	case *parser.IntegerLiteral:
		return e.Token.Line
	case *parser.FloatLiteral:
		return e.Token.Line
	case *parser.BooleanLiteral:
		return e.Token.Line
	case *parser.ByteLiteral:
		return e.Token.Line
	case *parser.StringLiteral:
		return e.Token.Line
	case *parser.CharLiteral:
		return e.Token.Line
	case *parser.RegexLiteral:
		return e.Token.Line
	case *parser.NilLiteral:
		return e.Token.Line
	case *parser.PrefixExpression:
		return stmtExprEndLine(e.Right)
	case *parser.InfixExpression:
		return stmtExprEndLine(e.Right)
	case *parser.CallExpression:
		return e.Token.Line
	case *parser.DotExpression:
		return stmtExprEndLine(e.Receiver)
	case *parser.IfExpression:
		if e.Alternative != nil && e.Alternative.Token.Line > 0 {
			return e.Alternative.Token.Line
		}
		return e.Consequence.Token.Line
	case *parser.FunctionLiteral:
		return e.Body.Token.Line
	case *parser.IndexExpression:
		return e.Token.Line
	case *parser.SliceExpression:
		return e.Token.Line
	case *parser.RangeExpression:
		return e.Token.Line
	case *parser.ArrayLiteral:
		return e.Token.Line
	case *parser.SliceLiteral:
		return e.Token.Line
	case *parser.StructLiteral:
		return e.Token.Line
	case *parser.AssignExpression:
		return e.Token.Line
	case *parser.ConditionalExpression:
		return e.Token.Line
	case *parser.GroupedExpression:
		return e.Token.Line
	case *parser.RunExpression:
		return stmtExprEndLine(e.Call)
	case *parser.AwaitExpression:
		return stmtExprEndLine(e.Right)
	case *parser.CastExpression:
		return stmtExprEndLine(e.Expr)
	}
	return 0
}

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

// commentMarker returns the comment start symbol for a comment, defaulting to "//".
func (f *formatter) commentMarker(c *parser.Comment) string {
	if c.Marker == "" {
		return "//"
	}
	return c.Marker
}

// writeCommentBody writes a single comment with its marker(s).
// Block comments (Marker ";;") emit ";;" + Text + ";;" verbatim (Text may contain
// newlines). Line comments (Marker "//" or ";") normalize the space after the marker.
func (f *formatter) writeCommentBody(c *parser.Comment) {
	if c.Marker == ";;" {
		f.write(";;")
		f.write(c.Text)
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
func (f *formatter) formatInlineComment(comment *parser.CommentGroup) {
	if comment == nil || len(comment.List) == 0 {
		return
	}
	c := comment.List[0]
	if c.Marker == ";;" {
		// 塊註釋緊貼代碼：a = 1 ;; block ;;
		f.write(" ;;")
		f.write(c.Text)
		f.write(";;")
		return
	}
	if c.Marker == ";" {
		// `;` 註釋緊貼代碼：a = 1; comment
		f.write("; ")
	} else {
		f.write("  // ")
	}
	f.write(strings.TrimSpace(c.Text))
}

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

func (f *formatter) formatProgram(p *parser.Program) {
	for i, stmt := range p.Statements {
		if i > 0 {
			prevEndLine := stmtTokenEndLine(p.Statements[i-1])
			currStartLine := stmtFirstLine(stmt)
			_, prevIsFunc := p.Statements[i-1].(*parser.FunctionDefinition)
			_, currIsFunc := stmt.(*parser.FunctionDefinition)
			_, prevIsUse := p.Statements[i-1].(*parser.UseStatement)
			_, currIsUse := stmt.(*parser.UseStatement)
			// 導入語句之間不留空行（也不保留源文件的空白行）
			if prevIsUse && currIsUse {
				// no blank line between imports
			} else if prevIsUse || currIsUse {
				// 導入語句和其他語句之間保留空行
				f.newline()
			} else if f.hasBlankLineBetween(prevEndLine, currStartLine) || (prevIsFunc && currIsFunc) {
				f.newline()
			}
			f.newline()
		}
		f.formatStatement(stmt)
	}

	// 輸出尾隨註釋
	if p.TrailingComments != nil {
		f.newline()
		for _, c := range p.TrailingComments.List {
			f.writeCommentBody(c)
			f.newline()
		}
	}
}

// stmtTokenLine 取得陳述句的起始行號（1-based）
func stmtTokenLine(stmt parser.Statement) int {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return s.Token.Line
	case *parser.UseStatement:
		return s.Token.Line
	case *parser.ExportStatement:
		return s.Token.Line
	case *parser.ReturnStatement:
		return s.Token.Line
	case *parser.ExpressionStatement:
		return s.Token.Line
	case *parser.FunctionDefinition:
		return s.Token.Line
	case *parser.ForStatement:
		return s.Token.Line
	case *parser.BreakStatement:
		return s.Token.Line
	case *parser.ContinueStatement:
		return s.Token.Line
	case *parser.BlockStatement:
		return s.Token.Line
	case *parser.EnumDefinition:
		return s.Token.Line
	case *parser.TaggedEnumDefinition:
		return s.Token.Line
	case *parser.InterfaceDefinition:
		return s.Token.Line
	case *parser.StructDefinition:
		return s.Token.Line
	case *parser.TypeAlias:
		return s.Token.Line
	case *parser.ExternStatement:
		return s.Token.Line
	case *parser.AnnotationStatement:
		return s.Token.Line
	}
	return 0
}

func (f *formatter) formatStatement(stmt parser.Statement) {
	// Skip compiler-injected synthetic statements (e.g., `it = matched`)
	if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsSynthetic {
		return
	}
	// Use CommentedNode interface to get Doc comments
	var doc *parser.CommentGroup
	if d, ok := stmt.(interface{ GetDoc() *parser.CommentGroup }); ok {
		doc = d.GetDoc()
	}
	// 輸出語句前的註釋（Doc），保留註釋與語句之間的空行
	f.formatDocComments(doc)
	if doc != nil && len(doc.List) > 0 {
		lastDocLine := doc.List[len(doc.List)-1].Pos.Line
		stmtLine := stmtTokenLine(stmt)
		if lastDocLine > 0 && stmtLine > lastDocLine+1 {
			// Preserve blank line between last Doc comment and statement
			f.write("\n") // bare blank line (no indent)
		}
		f.newline() // indent for statement
	}

	// Output attached annotations (e.g. #{mac-arm64}, #{linux-amd64}) before the statement.
	// These are platform annotations or generic annotations attached by the parser.
	if anns := attachedAnnotations(stmt); len(anns) > 0 {
		f.write("#{")
		for i, e := range anns {
			if i > 0 {
				f.write(", ")
			}
			f.write(e.String())
		}
		f.write("}")
		f.newline()
	}

	switch s := stmt.(type) {
	case *parser.UseStatement:
		f.formatUseStatement(s)
	case *parser.ExportStatement:
		f.formatExportStatement(s)
	case *parser.LetStatement:
		f.formatLetStatement(s)
	case *parser.TypeAlias:
		f.formatTypeAlias(s)
	case *parser.ReturnStatement:
		f.formatReturnStatement(s)
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			f.formatExpression(s.Expression)
		}
		// nil expression = bare { from condition: { body } syntax — skip silently
	case *parser.FunctionDefinition:
		f.formatFunctionDefinition(s)
	case *parser.ForStatement:
		f.formatForStatement(s)
	case *parser.BreakStatement:
		f.formatBreakStatement(s)
	case *parser.ContinueStatement:
		f.formatContinueStatement(s)
	case *parser.BlockStatement:
		f.formatBlockStatement(s)
	case *parser.EnumDefinition:
		f.formatEnumDefinition(s)
	case *parser.TaggedEnumDefinition:
		f.formatTaggedEnumDefinition(s)
	case *parser.InterfaceDefinition:
		f.formatInterfaceDefinition(s)
	case *parser.StructDefinition:
		f.formatStructDefinition(s)
	case *parser.MultiAssignStatement:
		f.formatMultiAssignStatement(s)
	case *parser.ExternStatement:
		f.formatExternStatement(s)
	case *parser.AnnotationStatement:
		f.formatAnnotationStatement(s)
	}

	// For FunctionDefinition and ForStatement, inline comment is handled inside the specific formatter.
	// For other statement types, output inline comment here.
	if _, isFunc := stmt.(*parser.FunctionDefinition); !isFunc {
		var comment *parser.CommentGroup
		if c, ok := stmt.(interface{ GetComment() *parser.CommentGroup }); ok {
			comment = c.GetComment()
		}
		f.formatInlineComment(comment)
	}
}

func (f *formatter) formatExpression(expr parser.Expression) {
	switch e := expr.(type) {
	case *parser.Identifier:
		if e.Value == "self" {
			f.write(".")
		} else {
			f.write(e.Value)
		}
	case *parser.IntegerLiteral:
		f.write(e.Token.Literal)
	case *parser.ByteLiteral:
		f.write(e.Token.Literal)
	case *parser.FloatLiteral:
		f.write(e.Token.Literal)
	case *parser.StringLiteral:
		if e.Token.Raw != "" {
			f.write(e.Token.Raw)
		} else {
			f.write("'")
			f.write(e.Value)
			f.write("'")
		}
	case *parser.CharLiteral:
		if e.Token.Raw != "" {
			f.write(e.Token.Raw)
		} else {
			f.write("'")
			f.write(e.Value)
			f.write("'")
		}
	case *parser.RegexLiteral:
		f.write("/")
		f.write(e.Pattern)
		f.write("/")
		f.write(e.Flags)
	case *parser.BooleanLiteral:
		if e.Value {
			f.write("true")
		} else {
			f.write("false")
		}
	case *parser.NilLiteral:
		f.write("nil")
	case *parser.PrefixExpression:
		f.formatPrefixExpression(e)
	case *parser.InfixExpression:
		f.formatInfixExpression(e)
	case *parser.CallExpression:
		f.formatCallExpression(e)
	case *parser.DotExpression:
		f.formatDotExpression(e)
	case *parser.IfExpression:
		f.formatIfExpression(e)
	case *parser.FunctionLiteral:
		f.formatFunctionLiteral(e)
	case *parser.IndexExpression:
		f.formatIndexExpression(e)
	case *parser.SliceExpression:
		f.formatSliceExpression(e)
	case *parser.RangeExpression:
		f.formatRangeExpression(e)
	case *parser.ArrayLiteral:
		f.formatArrayLiteral(e)
	case *parser.SliceLiteral:
		f.formatSliceLiteral(e)
	case *parser.StructLiteral:
		f.formatStructLiteral(e)
	case *parser.AssignExpression:
		f.formatAssignExpression(e)
	case *parser.ConditionalExpression:
		f.formatConditionalExpression(e)
	case *parser.NullableType:
		f.write("?")
		if e.Type != nil {
			f.write(e.Type.String())
		}
	case *parser.PointerType:
		f.write("ptr")
		if e.Type != nil {
			f.write(" ")
			f.write(e.Type.String())
		}
	case *parser.GroupedExpression:
		f.write("(")
		f.formatExpression(e.Expression)
		f.write(")")
	case *parser.RunExpression:
		f.write("run ")
		f.formatExpression(e.Call)
	case *parser.AwaitExpression:
		f.write("awy ")
		f.formatExpression(e.Right)
	case *parser.CastExpression:
		f.formatExpression(e.Expr)
		f.write(" as ")
		if e.Type != nil {
			f.write(e.Type.String())
		} else {
			f.write("?")
		}
	case *parser.MapLiteral:
		f.formatMapLiteral(e)
	}
}

func (f *formatter) formatMapLiteral(ml *parser.MapLiteral) {
	if len(ml.Pairs) == 0 {
		f.write("{ }")
		return
	}
	f.write("{ ")
	for i, pair := range ml.Pairs {
		if i > 0 {
			f.write(", ")
		}
		f.formatExpression(pair.Key)
		f.write(": ")
		f.formatExpression(pair.Value)
	}
	f.write(" }")
}

func (f *formatter) formatUseStatement(s *parser.UseStatement) {
	f.write("# ")
	f.write(s.Path)
	if s.Function != "" {
		f.write(".")
		f.write(s.Function)
	}
	if s.Alias != "" {
		f.write(" ")
		f.write(s.Alias)
	}
}

func (f *formatter) formatExportStatement(s *parser.ExportStatement) {
	f.write("@ ")
	f.write(s.Path)
	if s.Function != "" {
		f.write(".")
		f.write(s.Function)
	}
	if s.Alias != "" {
		f.write(" ")
		f.write(s.Alias)
	}
}

func (f *formatter) formatTypeAlias(s *parser.TypeAlias) {
	f.write(s.Name)
	f.write(" = ")
	if s.IsUnion() {
		for i, t := range s.Union.Types {
			if i > 0 {
				f.write(" | ")
			}
			f.write(t.String())
		}
	} else if s.Type != nil {
		if ft, ok := s.Type.(*parser.FunctionType); ok {
			f.formatFunctionTypeAlias(ft)
		} else {
			f.write(s.Type.String())
		}
	}
}

// formatFunctionTypeAlias outputs a function type in alias syntax: (params)(results)?
func (f *formatter) formatFunctionTypeAlias(ft *parser.FunctionType) {
	f.write("(")
	for i, p := range ft.Params {
		if i > 0 {
			f.write(", ")
		}
		if p.Name != "" {
			f.write(p.Name)
			f.write(" ")
		}
		f.write(p.Type.String())
	}
	f.write(")")
	if len(ft.Results) > 0 {
		f.write(" (")
		for i, r := range ft.Results {
			if i > 0 {
				f.write(", ")
			}
			if r.Name != "" {
				f.write(r.Name)
				f.write(" ")
			}
			f.write(r.Type.String())
		}
		f.write(")")
	}
}

func (f *formatter) formatLetStatement(s *parser.LetStatement) {
	f.formatExpression(s.Name)
	// Render array/slice type: a [3]u16, v []u8, a [?]u16
	if at, ok := s.Type.(*parser.ArrayType); ok {
		f.write(" [")
		if at.Size != nil {
			f.formatExpression(at.Size)
		} else {
			f.write("?") // [?] — infer size from literal
		}
		f.write("]")
		// Only output element type if explicitly written (not inferred default i64)
		if at.Elem != nil && !typeTokenInferred(at.Token, at.Elem) {
			f.write(at.Elem.String())
		}
	} else if st, ok := s.Type.(*parser.SliceType); ok {
		f.write(" []")
		// Only output element type if explicitly written (not inferred default i64)
		if st.Elem != nil && !typeTokenInferred(st.Token, st.Elem) {
			f.write(st.Elem.String())
		}
	} else if nt, ok := s.Type.(*parser.NamedType); ok && nt.Value != "" && !isInferredType(s) {
		f.write(" ")
		f.write(nt.Value)
	} else if nt, ok := s.Type.(*parser.NullableType); ok && !isInferredNullableType(s) {
		f.write(" ?")
		f.write(nt.Type.String())
	} else if mt, ok := s.Type.(*parser.MapType); ok {
		// map 型別：輸出 " [K]V"（MapType.String() 已回傳 "[K]V" 形式）
		f.write(" ")
		f.write(mt.String())
	}
	if s.Value != nil {
		f.write(" = ")
		f.stringAlign = f.column
		// 當 ArraySize > 0 且值為 ArrayLiteral（由 [1, 2, 3] 轉換而來）
		// 以切片風格輸出 [1, 2, 3]，避免重複 size
		if at, ok := s.Type.(*parser.ArrayType); ok {
			if intLit, ok := at.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
				if arr, ok := s.Value.(*parser.ArrayLiteral); ok && isSliceConverted(arr) {
					f.write("[")
					multiLine := len(arr.Elements) > 8
					if multiLine {
						f.indent++
					}
					for i, el := range arr.Elements {
						if i > 0 && !(multiLine && i%8 == 0) {
							f.write(", ")
						}
						if multiLine && i%8 == 0 {
							f.newline()
						}
						f.formatExpression(el)
						if multiLine && (i%8 == 7 || i == len(arr.Elements)-1) {
							f.write(",")
						}
					}
					if multiLine {
						f.indent--
						f.newline()
					}
					f.write("]")
				} else {
					f.formatExpression(s.Value)
				}
			} else {
				f.formatExpression(s.Value)
			}
		} else {
			f.formatExpression(s.Value)
		}
		f.stringAlign = 0
	}
}

// formatMultiAssignStatement: q, r = func(args)
func (f *formatter) formatMultiAssignStatement(s *parser.MultiAssignStatement) {
	for i, target := range s.Targets {
		if i > 0 {
			f.write(", ")
		}
		f.formatExpression(target)
	}
	if s.Value != nil {
		f.write(" = ")
		f.formatExpression(s.Value)
	}
}

// isSliceConverted checks if ArrayLiteral was converted from SliceLiteral
// (Size.Token.Literal == "[" indicates the original LBRACKET token)
func isSliceConverted(arr *parser.ArrayLiteral) bool {
	if arr.Size == nil {
		return false
	}
	if intLit, ok := arr.Size.(*parser.IntegerLiteral); ok {
		return intLit.Token.Literal == "["
	}
	return false
}

// typeTokenInferred checks if a child type's token has the same position as the parent's token,
// indicating the child was inferred/defaulted by the parser.
func typeTokenInferred(parentToken lexer.Token, childType parser.Type) bool {
	pos := childType.Pos()
	return pos.Line == parentToken.Line && pos.Column == parentToken.Column
}

// isInferredType checks if the type was inferred by the parser (not written in source).
// The parser sets Type.Token to the same position as Name.Token for inferred types.
func isInferredType(s *parser.LetStatement) bool {
	if s.Type == nil || s.Name == nil {
		return false
	}
	nt, ok := s.Type.(*parser.NamedType)
	if !ok {
		return false
	}
	return nt.Token.Line == s.Name.Token.Line &&
		nt.Token.Column == s.Name.Token.Column
}

// isInferredNullableType 判斷 NullableType 是否為從推斷型別（如 i8.MIN）而來
// 推斷型別的位置會等於變數名位置
func isInferredNullableType(s *parser.LetStatement) bool {
	if s.Type == nil || s.Name == nil {
		return false
	}
	nt, ok := s.Type.(*parser.NullableType)
	if !ok {
		return false
	}
	if inner, ok := nt.Type.(*parser.NamedType); ok {
		return inner.Token.Line == s.Name.Token.Line &&
			inner.Token.Column == s.Name.Token.Column
	}
	return false
}

func (f *formatter) formatReturnStatement(s *parser.ReturnStatement) {
	f.write("return")
	if s.ReturnValue != nil {
		f.write(" ")
		f.formatExpression(s.ReturnValue)
	}
}

func (f *formatter) formatFunctionDefinition(s *parser.FunctionDefinition) {
	f.write(s.Name)
	// 只顯示明確泛型參數（大寫），跳過隱式推斷的單字母小寫泛型
	explicitGenericParams := filterExplicitGenericParams(s.GenericParams)
	if len(explicitGenericParams) > 0 {
		f.write("<")
		for i, gp := range explicitGenericParams {
			if i > 0 {
				f.write(", ")
			}
			f.write(gp)
		}
		f.write(">")
	}
	if s.ColonSyntax {
		f.write(": (")
	} else {
		f.write(" = (")
	}
	// Skip implicit self parameter for method definitions
	params := s.Parameters
	if isMethodDef(s) && len(params) > 0 && params[0].Name == "self" {
		params = params[1:]
	}
	f.formatParameters(params, s.IsVariadic)
	f.write(")")
	if len(s.Results) > 0 {
		f.write(" (")
		f.formatParameters(s.Results, false)
		f.write(")")
	}
	f.write(" {")
	// Output inline comment on the same line as the opening brace
	if s.Comment != nil && len(s.Comment.List) > 0 {
		c := s.Comment.List[0]
		f.writef("  // %s", strings.TrimSpace(c.Text))
	}
	f.indent++

	// 過濾掉 ; 分隔符產生的空表達式語句
	statements := make([]parser.Statement, 0, len(s.Body.Statements))
	for _, stmt := range s.Body.Statements {
		if es, ok := stmt.(*parser.ExpressionStatement); ok && es.Expression == nil {
			continue
		}
		statements = append(statements, stmt)
	}

	for i, stmt := range statements {
		if i > 0 {
			prevTokenLine := stmtTokenLine(statements[i-1])
			currTokenLine := stmtTokenLine(stmt)
		if prevTokenLine > 0 && prevTokenLine == currTokenLine {
			// Same line: never emit ';' (reserved for comments); split onto a new line.
			f.newline()
		} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) || f.hasDocComment(stmt) {
					f.write("\n") // blank line (no indent)
				}
				f.newline()
			}
		} else {
			// Check for blank line between '{' and first statement
			openBraceLine := s.Body.Token.Line
			firstDocStartLine := stmtFirstLine(stmt)
			if openBraceLine > 0 && firstDocStartLine > openBraceLine+1 {
				f.write("\n") // blank line (no indent)
			}
			f.newline()
		}
		f.formatStatement(stmt)
	}

	// 輸出函數體內的尾隨註釋
	f.formatTrailingComments(s.Body.TrailingComments)

	f.indent--
	f.newline()
	f.write("}")
}

// isMethodDef reports whether a function definition is a method (name contains '.').
func isMethodDef(s *parser.FunctionDefinition) bool {
	return strings.Contains(s.Name, ".")
}

// filterExplicitGenericParams 過濾隱式推斷的泛型參數，只保留明確聲明的泛型參數
// 隱式泛型為單字母小寫 a-z，由 detectImplicitGeneric 推斷
func filterExplicitGenericParams(params []*parser.Identifier) []string {
	var result []string
	for _, p := range params {
		if len(p.Value) != 1 || p.Value[0] < 'a' || p.Value[0] > 'z' {
			result = append(result, p.Value)
		}
	}
	return result
}

func (f *formatter) formatParameters(params []*parser.Parameter, isVariadic bool) {
	for i, p := range params {
		if i > 0 {
			f.write(", ")
		}
		f.write(p.Name)
		if p.Type != nil {
			f.write(" ")
			if isVariadic && i == len(params)-1 {
				f.write("..")
				if st, ok := p.Type.(*parser.SliceType); ok {
					f.write(st.Elem.String())
				} else {
					f.write(p.Type.String())
				}
			} else {
				f.write(p.Type.String())
			}
		}
		if p.DefaultExpr != nil {
			f.write(" = ")
			f.formatExpression(p.DefaultExpr)
		}
	}
}

func (f *formatter) formatBlockStatement(s *parser.BlockStatement) {
	f.write("{")
	// Output opening brace comment on the same line as {
	if s.OpeningBraceComment != nil && len(s.OpeningBraceComment.List) > 0 {
		f.write("  //")
		for _, c := range s.OpeningBraceComment.List {
			f.write(c.Text)
		}
	}
	f.indent++

	// 過濾掉 ; 分隔符產生的空表達式語句及 compiler 注入的合成語句
	statements := make([]parser.Statement, 0, len(s.Statements))
	for _, stmt := range s.Statements {
		if es, ok := stmt.(*parser.ExpressionStatement); ok && es.Expression == nil {
			continue
		}
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsSynthetic {
			continue
		}
		statements = append(statements, stmt)
	}

	for i, stmt := range statements {
		if i > 0 {
			prevTokenLine := stmtTokenLine(statements[i-1])
			currTokenLine := stmtTokenLine(stmt)
		if prevTokenLine > 0 && prevTokenLine == currTokenLine {
			// Same line: never emit ';' (reserved for comments); split onto a new line.
			f.newline()
		} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) || f.hasDocComment(stmt) {
					f.write("\n") // blank line (no indent)
				}
				f.newline()
			}
		} else {
			// Check for blank line between '{' and first statement
			openBraceLine := s.Token.Line
			firstDocStartLine := stmtFirstLine(stmt)
			if openBraceLine > 0 && firstDocStartLine > openBraceLine+1 {
				f.write("\n") // blank line (no indent)
			}
			f.newline()
		}
		f.formatStatement(stmt)
	}

	// 輸出尾隨註釋
	f.formatTrailingComments(s.TrailingComments)

	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatPrefixExpression(e *parser.PrefixExpression) {
	f.write(e.Operator)
	if e.Operator == "!" {
		f.write(" ")
	}
	f.formatExpression(e.Right)
}

func (f *formatter) formatInfixExpression(e *parser.InfixExpression) {
	f.formatExpression(e.Left)

	// Detect multi-line expressions (right operand starts on a different line)
	rightLine := stmtExprEndLine(e.Right)
	leftLine := stmtExprEndLine(e.Left)
	multiLine := rightLine > leftLine

	if multiLine {
		f.write(" ")
		f.write(e.Operator)
		if f.stringAlign > 0 && e.Operator == "+" {
			f.buf.WriteString("\n")
			f.buf.WriteString(strings.Repeat(" ", f.stringAlign))
			f.column = f.stringAlign
		} else {
			f.write("\n")
		}
		f.formatExpression(e.Right)
	} else {
		f.write(" ")
		f.write(e.Operator)
		f.write(" ")
		f.formatExpression(e.Right)
	}
}

func (f *formatter) formatCallExpression(e *parser.CallExpression) {
	f.formatExpression(e.Function)
	if len(e.GenericArgs) > 0 {
		f.write("<")
		for i, ga := range e.GenericArgs {
			if i > 0 {
				f.write(", ")
			}
			f.formatExpression(ga)
		}
		f.write(">")
	}
	f.write("(")
	for i, arg := range e.Arguments {
		if i > 0 {
			f.write(", ")
		}
		f.formatExpression(arg)
	}
	f.write(")")
}

func (f *formatter) formatDotExpression(e *parser.DotExpression) {
	if ident, ok := e.Receiver.(*parser.Identifier); ok {
		switch ident.Value {
		case "self":
			// .property (the dot serves as both self-reference and member access)
			f.write(".")
			f.write(e.Property)
			return
		case "super":
			// ..property (double dot for super)
			f.write("..")
			f.write(e.Property)
			return
		}
	}
	f.formatExpression(e.Receiver)
	f.write(".")
	f.write(e.Property)
}

// formatStandaloneBody formats the body of a standalone if-then (cond -> body).
// If the body is a single simple statement (expression, let, multi-assign,
// return, break, continue), it outputs inline without braces.
// If the body contains multiple statements, it outputs `{ stmts }`.
func (f *formatter) formatStandaloneBody(body *parser.BlockStatement) {
	if len(body.Statements) == 1 &&
		body.TrailingComments == nil &&
		body.ClosingBraceComment == nil &&
		body.OpeningBraceComment == nil &&
		!f.hasDocComment(body.Statements[0]) {
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

func (f *formatter) formatIfExpression(e *parser.IfExpression) {
	// Standalone if-then: `cond -> body` (without enclosing { })
	if e.IsStandalone {
		// Wildcard standalone: -> body (Condition is IntegerLiteral(1) marker)
		if intLit, ok := e.Condition.(*parser.IntegerLiteral); ok && intLit.Value == 1 {
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
			f.write(" -> ")
			f.formatStandaloneBody(e.Alternative)
		}
		return
	}

	// 裸 match 表達式 `{ cond -> body }` 輸出新式語法
	if e.IsBareMatch {
		f.formatBareMatchExpression(e)
		return
	}
	f.write("if ")
	f.formatExpression(e.Condition)
	f.write(" {")
	f.indent++
	for _, stmt := range e.Consequence.Statements {
		f.newline()
		f.formatStatement(stmt)
	}
	f.formatTrailingComments(e.Consequence.TrailingComments)
	f.indent--
	f.newline()
	f.write("}")

	if e.Alternative != nil {
		// Check if alternative contains a single if expression (elif desugaring)
		if isElifBlock(e.Alternative) {
			ifExpr := e.Alternative.Statements[0].(*parser.ExpressionStatement).Expression.(*parser.IfExpression)
			f.write(" elif ")
			f.formatExpression(ifExpr.Condition)
			f.write(" {")
			f.indent++
			for _, stmt := range ifExpr.Consequence.Statements {
				f.newline()
				f.formatStatement(stmt)
			}
			f.formatTrailingComments(ifExpr.Consequence.TrailingComments)
			f.indent--
			f.newline()
			f.write("}")
			// Handle nested alternative
			if ifExpr.Alternative != nil {
				f.formatElifChain(ifExpr.Alternative)
			}
		} else {
			f.write(" else {")
			f.indent++
			for _, stmt := range e.Alternative.Statements {
				f.newline()
				f.formatStatement(stmt)
			}
			f.formatTrailingComments(e.Alternative.TrailingComments)
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
func (f *formatter) formatBareMatchExpression(e *parser.IfExpression) {
	if e.MatchedExpr != nil {
		f.formatExpression(e.MatchedExpr)
		f.write(": {")
	} else {
		f.write("{")
	}
	// Output opening brace comment on the same line as {
	if e.OpeningBraceComment != nil && len(e.OpeningBraceComment.List) > 0 {
		f.write("  //")
		for _, c := range e.OpeningBraceComment.List {
			f.write(c.Text)
		}
	}
	f.indent++
	// 輸出當前 arm
	f.writeBareMatchArm(e)
	// 處理後續 arm（Alternative 鏈）
	for e.Alternative != nil {
		if len(e.Alternative.Statements) == 1 {
			if es, ok := e.Alternative.Statements[0].(*parser.ExpressionStatement); ok {
				if next, ok := es.Expression.(*parser.IfExpression); ok && next.IsBareMatch {
					e = next
					f.writeBareMatchArm(e)
					continue
				}
			}
		}
		// Wildcard arm 的 Alternative 是直接的 BlockStatement
		// 模擬 IfExpression 包裝後調用 writeBareMatchArm
		wildcardIf := &parser.IfExpression{
			Token:       e.Token,
			Condition:   &parser.IntegerLiteral{Token: e.Token, Value: 1},
			Consequence: e.Alternative,
			IsBareMatch: true,
		}
		if e.DotValBody == e.Alternative {
			wildcardIf.DotValBody = e.Alternative
		}
		f.writeBareMatchArm(wildcardIf)
		break
	}
	f.indent--
	f.newline()
	f.write("}")
}

// extractOptionPatterns extracts option pattern names from an || chain of
// (matched == pattern) comparisons. Returns nil if the chain doesn't match
// the expected form.
func extractOptionPatterns(expr *parser.InfixExpression, matched parser.Expression) []string {
	mid, ok := matched.(*parser.Identifier)
	if !ok {
		return nil
	}
	var patterns []string
	var collect func(e parser.Expression) bool
	collect = func(e parser.Expression) bool {
		if inf, ok := e.(*parser.InfixExpression); ok {
			if inf.Operator == "==" {
				if id, ok := inf.Left.(*parser.Identifier); ok && id.Value == mid.Value {
					if right, ok := inf.Right.(*parser.Identifier); ok {
						patterns = append(patterns, right.Value)
						return true
					}
				}
				return false
			}
			if inf.Operator == "||" {
				return collect(inf.Left) && collect(inf.Right)
			}
		}
		return false
	}
	if collect(expr) && len(patterns) >= 2 {
		return patterns
	}
	return nil
}

// writeBareMatchArm 輸出單個 arm。
// 對於非 wildcard：cond -> body
// 對於 wildcard：-> body
// 若 body 只有一個簡單語句（ExpressionStatement / LetStatement / ReturnStatement 等）且無註釋，
// 內聯輸出在同一行；若 body 有多個語句，用 { } 大括號包裹。
func (f *formatter) writeBareMatchArm(e *parser.IfExpression) {
	// 判斷是否為 wildcard（condition 為 IntegerLiteral(1) 標記）
	isWildcard := false
	if intLit, ok := e.Condition.(*parser.IntegerLiteral); ok && intLit.Value == 1 {
		isWildcard = true
	}
	f.newline()
	if isWildcard {
		if e.DotValBody != nil && e.DotValBody == e.Consequence {
			f.write("ok ->")
		} else {
			f.write("->")
		}
	} else {
		// For matched matches, strip `matched == ` from condition to show just pattern
		if e.MatchedExpr != nil {
			if infix, ok := e.Condition.(*parser.InfixExpression); ok && infix.Operator == "==" {
				if id, ok := infix.Left.(*parser.Identifier); ok {
					if mid, ok := e.MatchedExpr.(*parser.Identifier); ok && id.Value == mid.Value {
						f.formatExpression(infix.Right)
						f.write(" ->")
						goto writeBody
					}
				}
			}
			// Combined option patterns: (matched == nil) || (matched == err)
			if infix, ok := e.Condition.(*parser.InfixExpression); ok && infix.Operator == "||" {
				if patterns := extractOptionPatterns(infix, e.MatchedExpr); patterns != nil {
					f.write(strings.Join(patterns, " || "))
					f.write(" ->")
					goto writeBody
				}
			}
		}
		f.formatExpression(e.Condition)
		f.write(" ->")
	}
writeBody:
	// 過濾 compiler 注入的合成語句（如 `it = matched`）
	statements := make([]parser.Statement, 0, len(e.Consequence.Statements))
	for _, stmt := range e.Consequence.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsSynthetic {
			continue
		}
		statements = append(statements, stmt)
	}
	// 空 body（如 wildcard `->`）：不輸出任何內容
	if len(statements) == 0 &&
		e.Consequence.TrailingComments == nil &&
		e.Consequence.ClosingBraceComment == nil {
		return
	}
	// 內聯簡單 body：只一個語句且無註釋時，輸出在同一行
	if len(statements) == 1 &&
		e.Consequence.TrailingComments == nil &&
		e.Consequence.ClosingBraceComment == nil &&
		e.Consequence.OpeningBraceComment == nil &&
		!f.hasDocComment(statements[0]) {
		stmt := statements[0]
		switch stmt.(type) {
		case *parser.ExpressionStatement, *parser.LetStatement,
			*parser.ReturnStatement, *parser.BreakStatement, *parser.ContinueStatement:
			f.write(" ")
			f.formatStatement(stmt)
			return
		}
	}
	// 多語句 body：用 { } 大括號包裹
	f.write(" {")
	// Output opening brace comment on the same line as {
	if e.Consequence.OpeningBraceComment != nil && len(e.Consequence.OpeningBraceComment.List) > 0 {
		f.write("  //")
		for _, c := range e.Consequence.OpeningBraceComment.List {
			f.write(c.Text)
		}
	}
	f.indent++
	for i, stmt := range statements {
		if i > 0 {
			prevTokenLine := stmtTokenLine(statements[i-1])
			currTokenLine := stmtTokenLine(stmt)
		if prevTokenLine > 0 && prevTokenLine == currTokenLine {
			// Same line: never emit ';' (reserved for comments); split onto a new line.
			f.newline()
		} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) || f.hasDocComment(stmt) {
					f.write("\n") // blank line (no indent)
				}
				f.newline()
			}
		} else {
			f.newline()
		}
		f.formatStatement(stmt)
	}
	f.formatTrailingComments(e.Consequence.TrailingComments)
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatElifChain(alt *parser.BlockStatement) {
	if isElifBlock(alt) {
		ifExpr := alt.Statements[0].(*parser.ExpressionStatement).Expression.(*parser.IfExpression)
		f.write(" elif ")
		f.formatExpression(ifExpr.Condition)
		f.write(" {")
		f.indent++
		for _, stmt := range ifExpr.Consequence.Statements {
			f.newline()
			f.formatStatement(stmt)
		}
		f.formatTrailingComments(ifExpr.Consequence.TrailingComments)
		f.indent--
		f.newline()
		f.write("}")
		if ifExpr.Alternative != nil {
			f.formatElifChain(ifExpr.Alternative)
		}
	} else {
		f.write(" else {")
		f.indent++
		for _, stmt := range alt.Statements {
			f.newline()
			f.formatStatement(stmt)
		}
		f.formatTrailingComments(alt.TrailingComments)
		f.indent--
		f.newline()
		f.write("}")
	}
}

func isElifBlock(bs *parser.BlockStatement) bool {
	if len(bs.Statements) != 1 {
		return false
	}
	es, ok := bs.Statements[0].(*parser.ExpressionStatement)
	if !ok {
		return false
	}
	_, ok = es.Expression.(*parser.IfExpression)
	return ok
}

func (f *formatter) formatForStatement(s *parser.ForStatement) {
	if s.Label != "" {
		f.write("#")
		f.write(s.Label)
		// !!: no space between #N and !! (e.g. #1!! { ... })
		if s.Token.Type != lexer.BANG_BANG {
			f.write(" ")
		}
	}

	// !! { } 無限循環
	if s.Token.Type == lexer.BANG_BANG {
		f.write("!!")
		f.write(" {")
		f.indent++
		for _, stmt := range s.Body.Statements {
			f.newline()
			f.formatStatement(stmt)
		}
		f.formatTrailingComments(s.Body.TrailingComments)
		f.indent--
		f.newline()
		f.write("}")
		return
	}

	// Bare range-for: i <- [a..b]: { body } — when token type is IDENT and IterRange set
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
		f.write(": {")
		f.indent++
		for _, stmt := range s.Body.Statements {
			f.newline()
			f.formatStatement(stmt)
		}
		f.formatTrailingComments(s.Body.TrailingComments)
		f.indent--
		f.newline()
		f.write("}")
		return
	}

	// Bare counted loop: N * { body } — when token type is INT and CountExpr set
	if s.Token.Type != lexer.FOR && s.CountExpr != nil {
		f.formatExpression(s.CountExpr)
		f.write(" *")
		f.write(" {")
		f.indent++
		for _, stmt := range s.Body.Statements {
			f.newline()
			f.formatStatement(stmt)
		}
		f.formatTrailingComments(s.Body.TrailingComments)
		f.indent--
		f.newline()
		f.write("}")
		return
	}

	// 裸條件 for-loop：condition: { body }（不包含 for 關鍵字）
	if s.Token.Type != lexer.FOR {
		// Labeled-conditional wrapper: `#2 val: { ... }` is encoded by
		// parseLabeledStatement as ForStatement{Condition: *IfExpression,
		// Body: Consequence}. Unwrap the synthetic IfExpression so we render
		// the original `cond: { body }` shape and the body, not a nested
		// `if cond { ... }: { ... }`.
		if ifExpr, ok := s.Condition.(*parser.IfExpression); ok && s.Body == ifExpr.Consequence {
			if ifExpr.Condition != nil {
				f.formatExpression(ifExpr.Condition)
			}
			f.write(": {")
			f.indent++
			for _, stmt := range s.Body.Statements {
				f.newline()
				f.formatStatement(stmt)
			}
			f.formatTrailingComments(s.Body.TrailingComments)
			f.indent--
			f.newline()
			f.write("}")
			return
		}
		if s.Condition != nil {
			f.formatExpression(s.Condition)
		}
		f.write(": {")
		f.indent++
		statements := make([]parser.Statement, 0, len(s.Body.Statements))
		for _, stmt := range s.Body.Statements {
			if es, ok := stmt.(*parser.ExpressionStatement); ok && es.Expression == nil {
				continue
			}
			statements = append(statements, stmt)
		}
		for i, stmt := range statements {
			if i > 0 {
				prevTokenLine := stmtTokenLine(statements[i-1])
				currTokenLine := stmtTokenLine(stmt)
				if prevTokenLine > 0 && prevTokenLine == currTokenLine {
					f.write("; ")
				} else {
					prevEndLine := stmtTokenEndLine(statements[i-1])
					currStartLine := stmtFirstLine(stmt)
					if f.hasBlankLineBetween(prevEndLine, currStartLine) || f.hasDocComment(stmt) {
						f.write("\n")
					}
					f.newline()
				}
			} else {
				openBraceLine := s.Body.Token.Line
				firstDocStartLine := stmtFirstLine(stmt)
				if openBraceLine > 0 && firstDocStartLine > openBraceLine+1 {
					f.write("\n")
				}
				f.newline()
			}
			f.formatStatement(stmt)
		}
		f.formatTrailingComments(s.Body.TrailingComments)
		f.indent--
		f.newline()
		f.write("}")
		return
	}

	f.write(s.Token.Literal)

	// N * { } 次數循環
	if s.CountExpr != nil {
		f.write(" ")
		f.formatExpression(s.CountExpr)
		f.write(" *")
	} else if s.IterRange != nil && s.IterRange.Variable != "" {
		// range for: for i <- [a..b]
		f.write(" ")
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
	} else if s.Init != nil {
		// C-style for: for init, cond, update { }
		f.write(" ")
		f.formatStatement(s.Init)
		f.write(", ")
		f.formatExpression(s.Condition)
		f.write(", ")
		f.formatStatement(s.Update)
	} else if s.Condition != nil {
		// while-style: for cond { }
		f.write(" ")
		f.formatExpression(s.Condition)
	}
	// else: infinite loop: for { }

	if s.IterRange != nil {
		f.write(": {")
	} else {
		f.write(" {")
	}
	f.indent++
	// 過濾掉 ; 分隔符產生的空表達式語句
	statements := make([]parser.Statement, 0, len(s.Body.Statements))
	for _, stmt := range s.Body.Statements {
		if es, ok := stmt.(*parser.ExpressionStatement); ok && es.Expression == nil {
			continue
		}
		statements = append(statements, stmt)
	}
	for i, stmt := range statements {
		if i > 0 {
			prevTokenLine := stmtTokenLine(statements[i-1])
			currTokenLine := stmtTokenLine(stmt)
		if prevTokenLine > 0 && prevTokenLine == currTokenLine {
			// Same line: never emit ';' (reserved for comments); split onto a new line.
			f.newline()
		} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) || f.hasDocComment(stmt) {
					f.write("\n") // blank line (no indent)
				}
				f.newline()
			}
		} else {
			// Check for blank line between '{' and first statement
			openBraceLine := s.Body.Token.Line
			firstDocStartLine := stmtFirstLine(stmt)
			if openBraceLine > 0 && firstDocStartLine > openBraceLine+1 {
				f.write("\n") // blank line (no indent)
			}
			f.newline()
		}
		f.formatStatement(stmt)
	}
	f.formatTrailingComments(s.Body.TrailingComments)
	f.indent--
	f.newline()
	f.write("}")
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

func (f *formatter) formatBreakStatement(s *parser.BreakStatement) {
	// Preserve `*` shorthand if it was the original source.
	if s.Token.Type == lexer.MUL {
		f.write("*")
	} else {
		f.write("break")
	}
	if s.Label != "" {
		// Numeric label (`#1`) is encoded as `1` in s.Label; restore the `#`.
		if s.Token.Type == lexer.MUL || isNumericLabel(s.Label) {
			f.write(" #")
		} else {
			f.write(" ")
		}
		f.write(s.Label)
	}
}

func (f *formatter) formatContinueStatement(s *parser.ContinueStatement) {
	// Preserve `**` shorthand if it was the original source.
	if s.Token.Type == lexer.STAR_STAR {
		f.write("**")
	} else {
		f.write("continue")
	}
	if s.Label != "" {
		if s.Token.Type == lexer.STAR_STAR || isNumericLabel(s.Label) {
			f.write(" #")
		} else {
			f.write(" ")
		}
		f.write(s.Label)
	}
}

func isNumericLabel(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (f *formatter) formatAssignExpression(e *parser.AssignExpression) {
	f.formatExpression(e.Left)
	f.write(" = ")
	f.stringAlign = f.column
	f.formatExpression(e.Value)
	f.stringAlign = 0
}

func (f *formatter) formatConditionalExpression(e *parser.ConditionalExpression) {
	f.formatExpression(e.Condition)
	f.write(" ? ")
	f.formatExpression(e.Consequence)
	f.write(" : ")
	f.formatExpression(e.Alternative)
}

func (f *formatter) formatIndexExpression(e *parser.IndexExpression) {
	f.formatExpression(e.Left)
	f.write("[")
	f.formatExpression(e.Index)
	f.write("]")
}

func (f *formatter) formatSliceExpression(e *parser.SliceExpression) {
	f.formatExpression(e.Left)
	if e.Range != nil {
		f.formatRangeBrackets(e.Range)
	} else {
		f.write("[..]")
	}
}

func (f *formatter) formatRangeExpression(e *parser.RangeExpression) {
	f.formatRangeBrackets(e)
}

func (f *formatter) formatArrayLiteral(e *parser.ArrayLiteral) {
	f.write("[")
	if e.Size != nil {
		f.formatExpression(e.Size)
	}
	f.write("]{")
	for i, el := range e.Elements {
		if i > 0 {
			f.write(", ")
		}
		f.formatExpression(el)
	}
	f.write("}")
}

func (f *formatter) formatSliceLiteral(e *parser.SliceLiteral) {
	// Use multi-line formatting for arrays with many elements
	if len(e.Elements) > 8 {
		f.write("[")
		f.indent++
		for i, el := range e.Elements {
			f.newline()
			f.formatExpression(el)
			if i < len(e.Elements)-1 {
				f.write(",")
			} else {
				f.write(",") // trailing comma
			}
		}
		f.indent--
		f.newline()
		f.write("]")
		return
	}

	f.write("[")
	for i, el := range e.Elements {
		if i > 0 {
			f.write(", ")
		}
		f.formatExpression(el)
	}
	f.write("]")
}

func (f *formatter) formatStructLiteral(e *parser.StructLiteral) {
	f.write(e.Type)
	if len(e.Fields) == 0 {
		f.write("{}")
		return
	}
	f.write("{")
	f.indent++
	for _, field := range e.Fields {
		f.newline()
		f.write(field.Name)
		if field.Value != nil {
			f.write(": ")
			f.formatExpression(field.Value)
		}
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatStructDefinition(s *parser.StructDefinition) {
	f.write(s.Name)
	if len(s.Implements) > 0 {
		f.write(" ")
		f.write(strings.Join(s.Implements, ", "))
	}
	f.write(" {")
	f.indent++
	for _, field := range s.Fields {
		f.newline()
		f.write(field.Name)
		f.write(" ")
		if field.IsSlice {
			f.write("[]")
			if field.Type != nil {
				f.write(field.Type.String())
			}
		} else if field.ArraySize > 0 {
			f.writef("[%d]", field.ArraySize)
			if field.Type != nil {
				f.write(field.Type.String())
			}
		} else {
			if field.Type != nil {
				f.write(field.Type.String())
			}
		}
		if field.ReadOnly {
			f.write(" read-only")
		}
		if field.Sealed {
			f.write(" sealed")
		}
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatEnumDefinition(s *parser.EnumDefinition) {
	f.write(s.Name)
	f.write(" {")
	f.indent++
	for _, v := range s.Values {
		f.newline()
		f.write(v.Name)
		// 只在源碼確實寫了 `= <int>` 時輸出值；自動編號（red, green, blue）不輸出，
		// 以免 formatter 把簡單枚舉篡改成 red, green = 1, blue = 2。
		if v.Explicit {
			f.write(" = ")
			f.write(strconv.FormatInt(v.Value, 10))
		}
		f.write(",")
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatTaggedEnumDefinition(s *parser.TaggedEnumDefinition) {
	f.write(s.Name)
	f.write(" {")
	f.indent++
	for _, v := range s.Variants {
		f.newline()
		f.write(v.Name)
		f.write(" ")
		f.write(v.Type.String())
		f.write(",")
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatInterfaceDefinition(s *parser.InterfaceDefinition) {
	f.write(s.Name)
	if len(s.Implements) > 0 {
		f.write(" ")
		f.write(strings.Join(s.Implements, ", "))
	}
	f.write(" {")
	f.indent++
	for _, m := range s.Methods {
		f.newline()
		// Generic-receiver form: t.method(...)
		if m.IsGenericReceiver {
			f.write(m.Receiver)
			f.write(".")
		}
		f.write(m.Name)
		f.write("(")
		f.formatParameters(m.Parameters, m.IsVariadic)
		f.write(")")
		// Optional result declaration: (res type)
		if len(m.Results) > 0 {
			f.write(" (")
			f.formatParameters(m.Results, false)
			f.write(")")
		}
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatFunctionLiteral(e *parser.FunctionLiteral) {
	f.write("(")
	f.formatParameters(e.Parameters, e.IsVariadic)
	f.write(")")
	f.write(" {")
	f.indent++
	for _, stmt := range e.Body.Statements {
		f.newline()
		f.formatStatement(stmt)
	}
	f.formatTrailingComments(e.Body.TrailingComments)
	f.indent--
	f.newline()
	f.write("}")
}

// formatProgram parses and formats the given code, returning the formatted
// output (without any guarantee about a trailing newline), a bool indicating
// success, and any parser error messages. On parse error or
// empty/whitespace-only input the bool is false, out is empty, and errs holds
// the parser errors (nil for the empty-input case).
func formatProgram(code string) (out string, ok bool, errs []string) {
	if strings.TrimSpace(code) == "" {
		return "", false, nil
	}

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()

	// 如果解析失敗，返回原始碼，不修改；並透出錯誤訊息讓上層（如 `no fmt`）回報
	if len(p.Errors()) > 0 {
		return "", false, p.Errors()
	}

	if program == nil || len(program.Statements) == 0 {
		return "", false, nil
	}

	sourceLines := strings.Split(code, "\n")
	f := &formatter{
		sourceLines: sourceLines,
	}
	f.formatProgram(program)

	return f.buf.String(), true, nil
}

// Format reformats a code fragment. It does NOT add a trailing newline; callers
// that format a complete source file should use FormatFile instead. This keeps
// the fragment-level contract stable for unit tests and inline formatting.
// Parse errors are silently ignored (the original code is returned) — callers
// that need to surface errors should use FormatFileWithErrors.
func Format(code string) string {
	out, ok, _ := formatProgram(code)
	if !ok {
		return code
	}
	return strings.TrimRight(out, "\n")
}

// ensureTrailingNewline guarantees s ends with exactly one newline.
// Multiple trailing newlines are collapsed to one; a missing trailing newline
// is appended. Empty input is returned unchanged.
func ensureTrailingNewline(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// FormatFile formats a complete source file and guarantees the output ends
// with exactly one trailing newline (an empty line at EOF). Missing trailing
// newlines are appended; multiple trailing blank lines are collapsed to one.
// Unparseable or empty input is returned unchanged so the formatter never
// mangles a file it cannot understand. Parse errors are silently ignored
// (the original code is returned) — callers that need to surface errors should
// use FormatFileWithErrors.
func FormatFile(code string) string {
	out, ok, _ := formatProgram(code)
	if !ok {
		return code
	}
	return ensureTrailingNewline(out)
}

// FormatFileWithErrors behaves like FormatFile but also returns any parser
// error messages encountered while parsing the source. When parsing fails, out
// is the original (unchanged) code and errs holds the parser errors so callers
// such as `no fmt` can report them to the user instead of silently leaving the
// file untouched. errs is nil when formatting succeeded.
func FormatFileWithErrors(code string) (out string, errs []string) {
	o, ok, perrs := formatProgram(code)
	if !ok {
		return code, perrs
	}
	return ensureTrailingNewline(o), nil
}

func (f *Formatter) Format(code string) string {
	return Format(code)
}

func (f *Formatter) FormatFile(code string) string {
	return FormatFile(code)
}

func (f *formatter) formatExternStatement(s *parser.ExternStatement) {
	// 輸出 #{c} 或 #{c, extra=...} 格式
	f.write("#{")
	f.write(s.Lang)
	for _, a := range s.Annotations {
		f.write(", ")
		f.write(a.String())
	}
	f.write("}")
	f.newline()
	// 輸出函式宣告
	f.write(s.Name.Value)
	f.write(" = (")
	for i, p := range s.Parameters {
		if i > 0 {
			f.write(", ")
		}
		f.write(p.Name)
		if p.Type != nil {
			f.write(" ")
			f.write(p.Type.String())
		}
	}
	f.write(")")
	if len(s.Results) > 0 {
		f.write(" (")
		for i, r := range s.Results {
			if i > 0 {
				f.write(", ")
			}
			f.write(r.Name)
			if r.Type != nil {
				f.write(" ")
				f.write(r.Type.String())
			}
		}
		f.write(")")
	}
}

func (f *formatter) formatAnnotationStatement(s *parser.AnnotationStatement) {
	f.write("#{")
	for i, e := range s.Entries {
		if i > 0 {
			f.write(", ")
		}
		f.write(e.String())
	}
	f.write("}")
}

// attachedAnnotations returns annotations attached to a statement by the parser
// (e.g. platform annotations #{mac-arm64}, #{linux-amd64} attached via attachAnnotations).
func attachedAnnotations(stmt parser.Statement) []*parser.AnnotationEntry {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		return s.Annotations
	case *parser.FunctionDefinition:
		return s.Annotations
	case *parser.StructDefinition:
		return s.Annotations
	case *parser.ExpressionStatement:
		return s.Annotations
	}
	return nil
}
