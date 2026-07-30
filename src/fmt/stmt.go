package fmt

import (
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

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
	if anns := f.attachedAnnotations(stmt); len(anns) > 0 {
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
		if at.Elem != nil && !at.IsInferred && !elemTypeInferred(at.Elem) {
			f.write(at.Elem.String())
		}
	} else if st, ok := s.Type.(*parser.SliceType); ok && !st.IsInferred {
		f.write(" []")
		// Only output element type if explicitly written (not inferred default i64)
		if st.Elem != nil && !elemTypeInferred(st.Elem) {
			f.write(st.Elem.String())
		}
	} else if nt, ok := s.Type.(*parser.NamedType); ok && nt.Value != "" && !nt.IsInferred {
		f.write(" ")
		f.write(nt.Value)
	} else if nt, ok := s.Type.(*parser.NullableType); ok && !nt.IsInferred {
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

// isSliceConverted checks if ArrayLiteral was converted from SliceLiteral.
// 直接讀取 ArrayLiteral.WasSliceLiteral 欄位（由 parser 在轉換時設置），
// 避免依賴 Size.Token.Literal == "[" 的隱式 token 約定。

// isSliceConverted checks if ArrayLiteral was converted from SliceLiteral.
// 直接讀取 ArrayLiteral.WasSliceLiteral 欄位（由 parser 在轉換時設置），
// 避免依賴 Size.Token.Literal == "[" 的隱式 token 約定。
func isSliceConverted(arr *parser.ArrayLiteral) bool {
	return arr.WasSliceLiteral
}

// elemTypeInformed 判斷陣列/切片的元素型別是否為 parser 推斷。
// 直接讀取 IsInferred 欄位（parser 在推斷位置顯式設置），
// 避免依賴 token 位置相等啟發式。

// elemTypeInformed 判斷陣列/切片的元素型別是否為 parser 推斷。
// 直接讀取 IsInferred 欄位（parser 在推斷位置顯式設置），
// 避免依賴 token 位置相等啟發式。
func elemTypeInferred(t parser.Type) bool {
	switch typ := t.(type) {
	case *parser.NamedType:
		return typ.IsInferred
	case *parser.SliceType:
		return typ.IsInferred
	case *parser.ArrayType:
		return typ.IsInferred
	case *parser.NullableType:
		return typ.IsInferred
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
	f.formatBlockInner(s.Body, 0) // pass 0 to avoid preserving blank lines after { in function bodies
	f.indent--
	f.newline()
	f.write("}")
}

// isMethodDef reports whether a function definition is a method.
// 直接讀取 IsMethodDef 欄位（parser 在方法定義位置顯式設置），
// 避免依賴 `strings.Contains(Name, ".")` 的字串子串啟發式。

// isMethodDef reports whether a function definition is a method.
// 直接讀取 IsMethodDef 欄位（parser 在方法定義位置顯式設置），
// 避免依賴 `strings.Contains(Name, ".")` 的字串子串啟發式。
func isMethodDef(s *parser.FunctionDefinition) bool {
	return s.IsMethodDef
}

// filterExplicitGenericParams 過濾隱式推斷的泛型參數，只保留明確聲明的泛型參數。
// 直接讀取 Identifier.IsImplicitGeneric 欄位（由 addImplicitGeneric 設置），
// 避免依賴「單字母小寫 a-z 視為隱式」的命名規則啟發式。

// filterExplicitGenericParams 過濾隱式推斷的泛型參數，只保留明確聲明的泛型參數。
// 直接讀取 Identifier.IsImplicitGeneric 欄位（由 addImplicitGeneric 設置），
// 避免依賴「單字母小寫 a-z 視為隱式」的命名規則啟發式。
func filterExplicitGenericParams(params []*parser.Identifier) []string {
	var result []string
	for _, p := range params {
		if !p.IsImplicitGeneric {
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

// formatBlockInner formats the statements inside a block body (without writing
// the enclosing braces). It handles statement filtering, blank-line preservation,
// and doc-comment spacing. The caller is responsible for writing braces and
// managing indent. openBraceLine is the source line of '{' (0 if unknown).

// formatBlockInner formats the statements inside a block body (without writing
// the enclosing braces). It handles statement filtering, blank-line preservation,
// and doc-comment spacing. The caller is responsible for writing braces and
// managing indent. openBraceLine is the source line of '{' (0 if unknown).
func (f *formatter) formatBlockInner(body *parser.BlockStatement, openBraceLine int) {
	// 過濾掉 ; 分隔符產生的空表達式語句及 compiler 注入的合成語句
	statements := make([]parser.Statement, 0, len(body.Statements))
	for _, stmt := range body.Statements {
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
			firstDocStartLine := stmtFirstLine(stmt)
			if (openBraceLine > 0 && firstDocStartLine > openBraceLine+1) || f.hasDocComment(stmt) {
				f.write("\n") // blank line (no indent)
			}
			f.newline()
		}
		f.formatStatement(stmt)
	}

	// 輸出尾隨註釋
	f.formatTrailingComments(body.TrailingComments)
}

func (f *formatter) formatBlockStatement(s *parser.BlockStatement) {
	f.write("{")
	// Output opening brace comment on the same line as {
	if obc := f.obcOf(s); obc != nil && len(obc.List) > 0 {
		f.write("  //")
		for _, c := range obc.List {
			f.write(c.Text)
		}
	}
	f.indent++
	f.formatBlockInner(s, s.Token.Line)
	f.indent--
	f.newline()
	f.write("}")
}

func (f *formatter) formatBreakStatement(s *parser.BreakStatement) {
	// Preserve `*` shorthand if it was the original source.
	if s.Token.Type == lexer.MUL {
		f.write("*")
	} else {
		f.write("break")
	}
	// 直接讀取 LabelKind 枚舉（parser 在 LABEL/IDENT token 位置顯式設置），
	// 避免依賴 isNumericLabel 字串內容啟發式。
	// `*` 簡寫形式固定使用 `#` 前綴（如 `* #1`），與 LabelKind 無關。
	if s.Label != "" {
		if s.Token.Type == lexer.MUL || s.LabelKind == parser.LabelNumeric {
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
	// 直接讀取 LabelKind 枚舉（parser 在 LABEL/IDENT token 位置顯式設置），
	// 避免依賴 isNumericLabel 字串內容啟發式。
	// `**` 簡寫形式固定使用 `#` 前綴（如 `** #1`），與 LabelKind 無關。
	if s.Label != "" {
		if s.Token.Type == lexer.STAR_STAR || s.LabelKind == parser.LabelNumeric {
			f.write(" #")
		} else {
			f.write(" ")
		}
		f.write(s.Label)
	}
}

func (f *formatter) formatExternStatement(s *parser.ExternStatement) {
	// 輸出 #{c} 或 #{c, extra=...} 格式
	f.write("#{")
	f.write(s.Lang)
	for _, a := range f.sem.AnnotationsOf(s) {
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
// (e.g. platform annotations #{mac-arm64}, #{linux-amd64} attached via attachAnnotations),
// read from the semantic side-table.
