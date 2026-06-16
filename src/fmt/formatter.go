package fmt

import (
	"fmt"
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
}

func (f *formatter) writeIndent() {
	f.buf.WriteString(strings.Repeat("    ", f.indent))
}

func (f *formatter) write(s string) {
	f.buf.WriteString(s)
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
		f.write("//")
		f.write(c.Text)
	}
}

// formatInlineComment outputs a comment that appears on the same line as code.
func (f *formatter) formatInlineComment(comment *parser.CommentGroup) {
	if comment == nil || len(comment.List) == 0 {
		return
	}
	c := comment.List[0]
	f.write("  // ")
	f.write(strings.TrimSpace(c.Text))
}

// formatTrailingComments outputs comments that appear before a closing brace.
func (f *formatter) formatTrailingComments(tc *parser.CommentGroup) {
	if tc == nil {
		return
	}
	for _, c := range tc.List {
		f.newline()
		f.write("//")
		f.write(c.Text)
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

func (f *formatter) formatProgram(p *parser.Program) {
	for i, stmt := range p.Statements {
		if i > 0 {
			prevEndLine := stmtTokenEndLine(p.Statements[i-1])
			currStartLine := stmtFirstLine(stmt)
			// 函數定義之間始終插入空行
			_, prevIsFunc := p.Statements[i-1].(*parser.FunctionDefinition)
			_, currIsFunc := stmt.(*parser.FunctionDefinition)
			if f.hasBlankLineBetween(prevEndLine, currStartLine) || (prevIsFunc && currIsFunc) {
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
			f.write("//")
			f.write(c.Text)
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
	}
	return 0
}

func (f *formatter) formatStatement(stmt parser.Statement) {
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

	switch s := stmt.(type) {
	case *parser.UseStatement:
		f.formatUseStatement(s)
	case *parser.LetStatement:
		f.formatLetStatement(s)
	case *parser.ReturnStatement:
		f.formatReturnStatement(s)
	case *parser.ExpressionStatement:
		f.formatExpression(s.Expression)
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
		if e.Value == "self" || e.Value == "it" {
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
		f.write("'")
		f.write(e.Value)
		f.write("'")
	case *parser.CharLiteral:
		f.write("'")
		f.write(e.Value)
		f.write("'")
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
		f.write(e.Type.String())
	case *parser.PointerType:
		f.write("ptr ")
		f.write(e.Type.String())
	case *parser.GroupedExpression:
		f.write("(")
		f.formatExpression(e.Expression)
		f.write(")")
	}
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

func (f *formatter) formatLetStatement(s *parser.LetStatement) {
	f.formatExpression(s.Name)
	// Render array/slice type: a [3]u16, v []u8
	if at, ok := s.Type.(*parser.ArrayType); ok {
		f.write(" [")
		if at.Size != nil {
			f.formatExpression(at.Size)
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
	}
	if s.Value != nil {
		f.write(" = ")
		// 當 ArraySize > 0 且值為 ArrayLiteral（由 [1, 2, 3] 轉換而來）
		// 以切片風格輸出 [1, 2, 3]，避免重複 size
		if at, ok := s.Type.(*parser.ArrayType); ok {
			if intLit, ok := at.Size.(*parser.IntegerLiteral); ok && intLit.Value > 0 {
				if arr, ok := s.Value.(*parser.ArrayLiteral); ok && isSliceConverted(arr) {
					f.write("[")
					for i, el := range arr.Elements {
						if i > 0 {
							f.write(", ")
						}
						f.formatExpression(el)
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
	f.formatParameters(params)
	f.write(")")
	if len(s.Results) > 0 {
		f.write(" (")
		f.formatParameters(s.Results)
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
				// Same line: use semicolon separator
				f.write("; ")
			} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) {
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

func (f *formatter) formatParameters(params []*parser.Parameter) {
	for i, p := range params {
		if i > 0 {
			f.write(", ")
		}
		f.write(p.Name)
		if p.Type != nil {
			f.write(" ")
			f.write(p.Type.String())
		}
	}
}

func (f *formatter) formatBlockStatement(s *parser.BlockStatement) {
	f.write("{")
	f.indent++

	// 過濾掉 ; 分隔符產生的空表達式語句
	statements := make([]parser.Statement, 0, len(s.Statements))
	for _, stmt := range s.Statements {
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
				// Same line: use semicolon separator
				f.write("; ")
			} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) {
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
		f.write("\n")
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
		case "self", "it":
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

func (f *formatter) formatIfExpression(e *parser.IfExpression) {
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
		f.write(s.Label)
		f.write(" ")
	}

	// ! { } 無限循環
	if s.Token.Type == lexer.NOT {
		f.write("!")
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
		// C-style for: for init; cond; update { }
		f.write(" ")
		f.formatStatement(s.Init)
		f.write("; ")
		f.formatExpression(s.Condition)
		f.write("; ")
		f.formatStatement(s.Update)
	} else if s.Condition != nil {
		// while-style: for cond { }
		f.write(" ")
		f.formatExpression(s.Condition)
	}
	// else: infinite loop: for { }

	f.write(" {")
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
				// Same line: use semicolon separator
				f.write("; ")
			} else {
				prevEndLine := stmtTokenEndLine(statements[i-1])
				currStartLine := stmtFirstLine(stmt)
				if f.hasBlankLineBetween(prevEndLine, currStartLine) {
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
	f.write("break")
	if s.Label != "" {
		f.write(" ")
		f.write(s.Label)
	}
}

func (f *formatter) formatContinueStatement(s *parser.ContinueStatement) {
	f.write("continue")
	if s.Label != "" {
		f.write(" ")
		f.write(s.Label)
	}
}

func (f *formatter) formatAssignExpression(e *parser.AssignExpression) {
	f.formatExpression(e.Left)
	f.write(" = ")
	f.formatExpression(e.Value)
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
	f.write("{")
	for i, field := range e.Fields {
		if i > 0 {
			f.write(", ")
		}
		f.write(field.Name)
		if field.Value != nil {
			f.write(": ")
			f.formatExpression(field.Value)
		}
	}
	f.write("}")
}

func (f *formatter) formatStructDefinition(s *parser.StructDefinition) {
	f.write(s.Name)
	if len(s.Implements) > 0 {
		f.write(" : ")
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
			f.write(field.Type.String())
		} else if field.ArraySize > 0 {
			f.writef("[%d]", field.ArraySize)
			f.write(field.Type.String())
		} else {
			f.write(field.Type.String())
		}
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatEnumDefinition(s *parser.EnumDefinition) {
	f.write(s.Name)
	f.write(" {")
	for i, v := range s.Values {
		if i > 0 {
			f.write(", ")
		}
		f.write(v.Name)
	}
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
	f.write(" {")
	f.indent++
	for _, m := range s.Methods {
		f.newline()
		f.write(m.Name)
		f.write("()")
	}
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatFunctionLiteral(e *parser.FunctionLiteral) {
	f.write("(")
	f.formatParameters(e.Parameters)
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

func Format(code string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()

	// 如果解析失敗，返回原始碼，不修改
	if len(p.Errors()) > 0 {
		return code
	}

	if program == nil || len(program.Statements) == 0 {
		return code
	}

	sourceLines := strings.Split(code, "\n")
	f := &formatter{
		sourceLines: sourceLines,
	}
	f.formatProgram(program)

	return strings.TrimRight(f.buf.String(), "\n")
}

func (f *Formatter) Format(code string) string {
	return Format(code)
}
