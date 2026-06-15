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
	buf          strings.Builder
	indent       int
	sourceLines  []string
	lineTypes    []lineType
	stmtLineCnt  []int // per-statement formatted line count
	stmtOrigLine []int // per-statement original 1-based line number
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

func (f *formatter) formatProgram(p *parser.Program) {
	f.stmtLineCnt = nil
	f.stmtOrigLine = nil
	lineCount := func() int {
		s := f.buf.String()
		if len(s) == 0 {
			return 0
		}
		return strings.Count(s, "\n") + 1
	}
	for i, stmt := range p.Statements {
		before := lineCount()

		if i > 0 {
			// 保留原始碼中的空行（最多合併為一行）
			// 函數定義之間始終插入空行
			_, prevIsFunc := p.Statements[i-1].(*parser.FunctionDefinition)
			_, currIsFunc := stmt.(*parser.FunctionDefinition)
			if f.hasBlankLineBetween(p.Statements[i-1], stmt) || (prevIsFunc && currIsFunc) {
				f.newline()
			}
			f.newline()
		}
		f.formatStatement(stmt)

		after := lineCount()
		f.stmtLineCnt = append(f.stmtLineCnt, after-before)
		f.stmtOrigLine = append(f.stmtOrigLine, stmtTokenLine(stmt))
	}
}

// hasBlankLineBetween 檢查原始碼中兩個陳述句之間是否有空行
// 只考慮程式碼行之間的空白行，註釋行/空白行之間的空白行不計（section header 與函數之間的空白）
func (f *formatter) hasBlankLineBetween(prev, curr parser.Statement) bool {
	prevLine := stmtTokenLine(prev)
	currLine := stmtTokenLine(curr)
	if prevLine == 0 || currLine == 0 || currLine <= prevLine {
		return false
	}
	// 檢查 prevLine 到 currLine 之間是否有程式碼行之間的空白行
	for line := prevLine; line < currLine; line++ {
		idx := line - 1
		if idx >= len(f.sourceLines) {
			continue
		}
		if strings.TrimSpace(f.sourceLines[idx]) == "" {
			// 空白行只有在前一行是程式碼行時才視為 statement 之間的空行
			// 註釋行或 section header 前的空白不計
			if idx > 0 && idx-1 < len(f.lineTypes) && f.lineTypes[idx-1] == lineCode {
				return true
			}
		}
	}
	return false
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
		f.formatExpression(e.Type)
	case *parser.PointerType:
		f.write("ptr ")
		f.formatExpression(e.Type)
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
	if s.ArraySize > 0 {
		f.writef(" [%d]", s.ArraySize)
		if s.ElemType != "" {
			f.write(s.ElemType)
		}
	} else if s.IsSlice {
		f.write(" []")
		if s.ElemType != "" {
			f.write(s.ElemType)
		}
	} else if s.Type != nil && s.Type.Value != "" && !isInferredType(s) {
		if s.IsOption {
			f.write(" ?")
			f.write(s.Type.Value)
		} else {
			f.write(" ")
			f.write(s.Type.Value)
		}
	}
	if s.Value != nil {
		f.write(" = ")
		// 當 ArraySize > 0 且值為 ArrayLiteral（由 [1, 2, 3] 轉換而來）
		// 以切片風格輸出 [1, 2, 3]，避免重複 size
		if s.ArraySize > 0 {
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

// isInferredType checks if the type was inferred by the parser (not written in source).
// The parser sets Type.Token to the same position as Name.Token for inferred types.
func isInferredType(s *parser.LetStatement) bool {
	if s.Type == nil || s.Name == nil {
		return false
	}
	return s.Type.Token.Line == s.Name.Token.Line &&
		s.Type.Token.Column == s.Name.Token.Column
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
	f.write(" = (")
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
	f.indent++
	for _, stmt := range s.Body.Statements {
		f.newline()
		f.formatStatement(stmt)
	}
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
func filterExplicitGenericParams(params []string) []string {
	var result []string
	for _, p := range params {
		if len(p) != 1 || p[0] < 'a' || p[0] > 'z' {
			result = append(result, p)
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
		if p.Type != "" {
			f.write(" ")
			f.write(p.Type)
		}
	}
}

func (f *formatter) formatBlockStatement(s *parser.BlockStatement) {
	f.write("{")
	f.indent++
	for _, stmt := range s.Statements {
		f.newline()
		f.formatStatement(stmt)
	}
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
	f.write(" ")
	f.write(e.Operator)
	f.write(" ")
	f.formatExpression(e.Right)
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
	} else if s.Variable != "" && (s.Range != nil || s.RangeStr != "" || s.RangeIdent != "" || s.RangeSliceLit != nil) {
		// range for: for i <- [a..b]
		f.write(" ")
		f.write(s.Variable)
		f.write(" <- ")
		if s.RangeStr != "" {
			f.write("'")
			f.write(s.RangeStr)
			f.write("'")
		} else if s.RangeIdent != "" {
			f.write(s.RangeIdent)
		} else if s.RangeSliceLit != nil {
			f.formatSliceLiteral(s.RangeSliceLit)
		} else {
			f.formatRangeBrackets(s.Range)
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
	for _, stmt := range s.Body.Statements {
		f.newline()
		f.formatStatement(stmt)
	}
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
			f.write(field.Type)
		} else if field.ArraySize > 0 {
			f.writef("[%d]", field.ArraySize)
			f.write(field.Type)
		} else {
			f.write(field.Type)
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
		f.write(v.Type)
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
	f.indent--
	f.newline()
	f.write("}")
}

// lineType 表示原始行類型
// linePreserved: 純註釋行或空白行（保留原樣）
// lineCode: 程式碼行（需要格式化）
type lineType int

const (
	linePreserved lineType = iota
	lineCode
)

// lineComment 記錄原始碼中的行尾註釋資訊
type lineComment struct {
	comment  string // 註釋內容（不含 //）
	codeLine string // 去除註釋後的原始程式碼
}

func Format(code string) string {
	if strings.TrimSpace(code) == "" {
		return ""
	}

	// 從原始碼中移除 // 註釋，純註釋行保留原樣
	cleanCode, comments, lineTypes := stripAndClassify(code)

	l := lexer.New(cleanCode)
	p := parser.New(l)
	program := p.ParseProgram()

	// 如果解析失敗，返回原始碼，不修改
	if len(p.Errors()) > 0 {
		return code
	}

	if program == nil || len(program.Statements) == 0 {
		return cleanCode
	}

	cleanLines := strings.Split(cleanCode, "\n")
	f := &formatter{
		sourceLines: cleanLines,
		lineTypes:   lineTypes,
	}
	f.formatProgram(program)
	formattedCode := f.buf.String()
	formattedLines := strings.Split(formattedCode, "\n")

	// 使用 statement 行數追蹤重建：遍歷原始 cleanLines，
	// 保留行（純註釋/空白）原樣輸出，程式碼行替換為下一段格式化行的內容。
	// 同一 statement 可能跨越多行原始碼（例如多行函數定義），
	// 後續連續的程式碼行（如 }）屬同一 statement，不產生額外格式化輸出。
	stmtStartLines := make(map[int]bool)
	for _, l := range f.stmtOrigLine {
		if l > 0 {
			stmtStartLines[l-1] = true
		}
	}

	// 预处理：将缩进的纯注释行插入到所属语句的格式化输出中
	// 对于函数体内的注释（在 } 之前），插入到格式化输出的 } 之前
	// 对于 } 之后的注释，在合并循环中缩进会被归零处理
	stmtFormattedStart := make([]int, len(f.stmtLineCnt))
	pos := 0
	for i, cnt := range f.stmtLineCnt {
		stmtFormattedStart[i] = pos
		pos += cnt
	}

	// 建立行索引→语句索引的映射
	lineToStmt := make([]int, len(lineTypes))
	currentStmt := -1
	for i, lt := range lineTypes {
		if lt == lineCode && stmtStartLines[i] {
			currentStmt++
		}
		lineToStmt[i] = currentStmt
	}

	// 收集所有代码行位置
	codeLinePositions := make([]int, 0, len(lineTypes))
	for i, lt := range lineTypes {
		if lt == lineCode {
			codeLinePositions = append(codeLinePositions, i)
		}
	}

	type commentEntry struct {
		lineIdx int
		content string
	}

	// 记录已被预插入的注释行，合并循环中跳过它们
	handledComments := make(map[int]bool)

	// 将缩进注释按语句分组（仅在 } 之前的内部注释）
	commentsByStmt := make(map[int][]commentEntry)
	codePosIdx := 0
	for i, lt := range lineTypes {
		if lt != linePreserved {
			continue
		}
		line := cleanLines[i]
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "//") {
			// 缩进的空白行，夹在缩进注释之间时也收集到预插入中
			if strings.TrimSpace(line) == "" {
				// 检查相邻行是否为缩进的注释（缩进 > 0 表示在函数体内部）
				nextIsIndentedComment := i+1 < len(cleanLines) &&
					strings.HasPrefix(strings.TrimLeft(cleanLines[i+1], " \t"), "//") &&
					len(cleanLines[i+1])-len(strings.TrimLeft(cleanLines[i+1], " \t")) > 0
				prevIsIndentedComment := i > 0 &&
					strings.HasPrefix(strings.TrimLeft(cleanLines[i-1], " \t"), "//") &&
					len(cleanLines[i-1])-len(strings.TrimLeft(cleanLines[i-1], " \t")) > 0
				if nextIsIndentedComment || prevIsIndentedComment {
					stmt := lineToStmt[i]
					if stmt >= 0 {
						commentsByStmt[stmt] = append(commentsByStmt[stmt], commentEntry{lineIdx: i, content: cleanLines[i]})
						handledComments[i] = true
					}
				}
			}
			continue
		}
		indent := len(line) - len(trimmed)
		if indent == 0 {
			continue
		}

		// 查找前一行代码
		var prevCodeLine string
		for prev := i - 1; prev >= 0; prev-- {
			if lineTypes[prev] == lineCode {
				prevCodeLine = cleanLines[prev]
				break
			}
		}

		// 处理 } 之后的注释：函数级 }（缩进 0）由合并循环处理，嵌套 }（缩进 > 0）预插入并调整缩进
		if strings.TrimSpace(prevCodeLine) == "}" {
			prevIndent := len(prevCodeLine) - len(strings.TrimLeft(prevCodeLine, " \t"))
			if prevIndent == 0 {
				// 函数级 }：跳过，由合并循环归零缩进
				continue
			}
			// 嵌套 }：缩进调整到 } 的同级，然后预插入
			trimmed := strings.TrimLeft(cleanLines[i], " \t")
			adjustedLine := strings.Repeat(" ", prevIndent) + trimmed
			stmt := lineToStmt[i]
			if stmt >= 0 {
				commentsByStmt[stmt] = append(commentsByStmt[stmt], commentEntry{lineIdx: i, content: adjustedLine})
				handledComments[i] = true
			}
			continue
		}

		// 推进到当前行之后的代码行
		for codePosIdx < len(codeLinePositions) && codeLinePositions[codePosIdx] <= i {
			codePosIdx++
		}

		// 判断是否在语句体内部：下一代码行不是语句起始行，或没有更多代码行（尾部）
		isInsideBody := false
		if codePosIdx < len(codeLinePositions) {
			nextCodeLine := codeLinePositions[codePosIdx]
			if !stmtStartLines[nextCodeLine] {
				isInsideBody = true
			}
		} else {
			isInsideBody = true
		}

		if isInsideBody {
			stmt := lineToStmt[i]
			if stmt >= 0 {
				commentsByStmt[stmt] = append(commentsByStmt[stmt], commentEntry{lineIdx: i, content: cleanLines[i]})
				handledComments[i] = true
			}
		}
	}

	// 将注释插入到格式化行中：通过内容匹配找到每条注释的插入位置
	// 对于函数体内部的注释（非 } 之后的），找到下一条代码行在格式化输出中的位置，插入在其之前
	if len(commentsByStmt) > 0 {
		newFormattedLines := make([]string, 0, len(formattedLines))
		newStmtLineCnt := make([]int, len(f.stmtLineCnt))
		copy(newStmtLineCnt, f.stmtLineCnt)

		for stmtIdx := 0; stmtIdx < len(f.stmtLineCnt); stmtIdx++ {
			start := stmtFormattedStart[stmtIdx]
			end := start + f.stmtLineCnt[stmtIdx]
			stmtLines := formattedLines[start:end]

			if entries, ok := commentsByStmt[stmtIdx]; ok && len(entries) > 0 {
				offset := 0
				for _, entry := range entries {
					// 先按 lineIdx 排序，确保按原始顺序处理
					_ = entry.lineIdx

					// 找到注释之后的下一条代码行
					nextCodeLine := ""
					for j := entry.lineIdx + 1; j < len(cleanLines); j++ {
						if lineTypes[j] == lineCode {
							nextCodeLine = strings.TrimSpace(cleanLines[j])
							break
						}
					}

					if nextCodeLine == "" {
						// 没有后续代码行，追加到末尾
						stmtLines = append(stmtLines, entry.content)
						offset++
						continue
					}

					// 当下一条代码行是 } 时，插入到语句末尾（最后一个 } 之前）
					// 避免 } 在格式化输出中出现多次导致错误定位
					insertPos := -1
					if nextCodeLine == "}" {
						for k := len(stmtLines) - 1; k >= 0; k-- {
							if strings.TrimSpace(stmtLines[k]) == "}" {
								insertPos = k
								break
							}
						}
						goto doInsert
					}

					// 在 stmtLines 中找到匹配 nextCodeLine 的位置
					// 优先精确匹配（避免变量名相同但内容不同的误匹配），再回退到模糊匹配
					// 第一遍：精确匹配
					for k, sl := range stmtLines {
						trimmed := strings.TrimSpace(sl)
						if trimmed == nextCodeLine {
							insertPos = k
							break
						}
					}
					// 第二遍：模糊匹配（仅当精确匹配失败时）
					if insertPos < 0 {
						for k, sl := range stmtLines {
							trimmed := strings.TrimSpace(sl)
							if matchFormattedLine(trimmed, nextCodeLine) {
								insertPos = k
								break
							}
						}
					}

				doInsert:
					if insertPos >= 0 {
						// 在找到的位置之前插入
						// 注意：stmtLines 每次插入後都會更新，insertPos 已反映之前的偏移
						if insertPos < 0 {
							insertPos = 0
						}
						before := make([]string, insertPos)
						copy(before, stmtLines[:insertPos])
						after := stmtLines[insertPos:]
						combined := make([]string, 0, len(stmtLines)+1)
						combined = append(combined, before...)
						combined = append(combined, entry.content)
						combined = append(combined, after...)
						stmtLines = combined
						offset++
					} else {
						// 找不到匹配，追加到末尾
						stmtLines = append(stmtLines, entry.content)
						offset++
					}
				}

				newFormattedLines = append(newFormattedLines, stmtLines...)
				newStmtLineCnt[stmtIdx] += offset
			} else {
				newFormattedLines = append(newFormattedLines, stmtLines...)
			}
		}

		formattedLines = newFormattedLines
		f.stmtLineCnt = newStmtLineCnt
	}

	stmtIdx := 0
	formLineIdx := 0
	var result []string
	for origLineIdx, lt := range lineTypes {
		if lt == linePreserved {
			line := cleanLines[origLineIdx]
			isComment := strings.HasPrefix(strings.TrimLeft(line, " \t"), "//")
			if isComment {
				// 当前一行不是注释时，检查下一个语句输出是否以空行开头
				// 若是，在注释前插入该空行（让空行在 } 与注释之间，而非注释与函数之间）
				if len(result) > 0 {
					prevTrim := strings.TrimSpace(result[len(result)-1])
					prevIsCode := prevTrim != "" && !strings.HasPrefix(prevTrim, "//")
					if prevIsCode && formLineIdx < len(formattedLines) {
						// 向后查找下一个语句起始行
						for next := origLineIdx + 1; next < len(lineTypes); next++ {
							if lineTypes[next] == linePreserved {
								continue
							}
							if lineTypes[next] == lineCode {
								if stmtStartLines[next] && strings.TrimSpace(formattedLines[formLineIdx]) == "" {
									result = append(result, "")
									formLineIdx++
									if stmtIdx < len(f.stmtLineCnt) {
										f.stmtLineCnt[stmtIdx]--
									}
								}
							}
							break
						}
					}
				}
				// 跳过已被预插入的注释（已进入 formattedLines）
				if handledComments[origLineIdx] {
					continue
				}
				// 处理 } 之后的缩进注释：调整缩进匹配 } 的层级
				var prevCodeLine string
				for prev := origLineIdx - 1; prev >= 0; prev-- {
					if lineTypes[prev] == lineCode {
						prevCodeLine = cleanLines[prev]
						break
					}
				}
				if strings.TrimSpace(prevCodeLine) == "}" {
					// 前一代码行为 }：注释在 } 之后，缩进调整到 } 的同级
					prevIndent := len(prevCodeLine) - len(strings.TrimLeft(prevCodeLine, " \t"))
					line = strings.Repeat(" ", prevIndent) + strings.TrimLeft(line, " \t")
				} else if formLineIdx >= len(formattedLines) {
					// 没有更多格式化输出（文件末尾的注释），完全归零缩进
					line = strings.TrimLeft(line, " \t")
				}
				result = append(result, line)
			} else {
				// 空白行：前后都是注释行时才保留
				prevIsComment := origLineIdx == 0 || strings.HasPrefix(strings.TrimLeft(cleanLines[origLineIdx-1], " \t"), "//")
				nextIsComment := origLineIdx == len(cleanLines)-1 ||
					(origLineIdx+1 < len(lineTypes) && lineTypes[origLineIdx+1] == linePreserved &&
						strings.HasPrefix(strings.TrimLeft(cleanLines[origLineIdx+1], " \t"), "//"))
				if prevIsComment && nextIsComment {
					result = append(result, line)
				}
			}
			continue
		}

		// 代码行：只有当此行为语句起始行时才输出格式化内容
		if stmtStartLines[origLineIdx] && stmtIdx < len(f.stmtLineCnt) {
			cnt := f.stmtLineCnt[stmtIdx]
			for j := 0; j < cnt && formLineIdx+j < len(formattedLines); j++ {
				result = append(result, formattedLines[formLineIdx+j])
			}
			formLineIdx += cnt
			stmtIdx++
		}
		// 非起始行的代码行（如 }）隶属于上一个 statement，不输出
	}

	// 將行尾註釋匹配到格式化輸出
	if len(comments) > 0 {
		for _, comment := range comments {
			ident := extractFirstIdentifier(comment.codeLine)
			if ident == "" {
				continue
			}
			for j, line := range result {
				if extractFirstIdentifier(line) == ident && !strings.Contains(result[j], "//"+comment.comment) {
					result[j] = line + "  // " + comment.comment
					break
				}
			}
		}
	}

	return strings.Join(result, "\n")
}

// extractFirstIdentifier 從一行程式碼中提取首個有效的 Nolang 標識符
func extractFirstIdentifier(line string) string {
	// 跳過開頭的空白
	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return ""
	}
	// 逐字元提取：字母/數字/底線/連接號
	var buf strings.Builder
	for _, ch := range line {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' {
			buf.WriteRune(ch)
		} else {
			break
		}
	}
	return buf.String()
}

// matchFormattedLine 检查格式化行是否匹配原始代码行的内容。
// 使用逐步宽松的匹配策略：精确 → 前缀 → 变量名匹配。
func matchFormattedLine(trimmedFormatted, codeLine string) bool {
	if trimmedFormatted == "" || codeLine == "" {
		return false
	}
	// 1. 精确匹配
	if trimmedFormatted == codeLine {
		return true
	}
	// 2. 前缀匹配（因格式化可能增减括号或展开行内块）
	if strings.HasPrefix(codeLine, trimmedFormatted) || strings.HasPrefix(trimmedFormatted, codeLine) {
		return true
	}
	// 3. 匹配到第一个 '='（处理赋值语句）
	if eqIdx := strings.Index(codeLine, "="); eqIdx > 0 {
		prefix := strings.TrimSpace(codeLine[:eqIdx])
		if prefix != "" && strings.HasPrefix(strings.TrimSpace(trimmedFormatted), prefix) {
			return true
		}
	}
	// 4. 匹配到第一个 '{'（处理行内块展开）
	if braceIdx := strings.Index(codeLine, "{"); braceIdx > 0 {
		prefix := strings.TrimSpace(codeLine[:braceIdx])
		if prefix != "" && strings.HasPrefix(strings.TrimSpace(trimmedFormatted), prefix) {
			return true
		}
	}
	return false
}

// stripAndClassify 從原始碼中移除行尾註釋，保留純註釋行和空白行。
// 返回：移除行尾註釋後的程式碼，行尾註釋列表，每行的類型
func stripAndClassify(code string) (string, []lineComment, []lineType) {
	lines := strings.Split(code, "\n")
	var comments []lineComment
	types := make([]lineType, len(lines))
	inStr := false

	for lineIdx, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")

		// 純註釋行或空白行 → 保留原樣
		if strings.HasPrefix(trimmed, "//") || strings.TrimSpace(line) == "" {
			types[lineIdx] = linePreserved
			continue
		}

		types[lineIdx] = lineCode

		// 程式碼行 → 檢查是否有行尾註釋
		for i := 0; i < len(line); i++ {
			ch := line[i]
			if ch == '\'' && (i == 0 || line[i-1] != '\\') {
				inStr = !inStr
			}
			if !inStr && ch == '/' && i+1 < len(line) && line[i+1] == '/' {
				comment := strings.TrimSpace(line[i+2:])
				beforeComment := strings.TrimRight(line[:i], " \t")

				if comment != "" {
					comments = append(comments, lineComment{
						comment:  comment,
						codeLine: beforeComment,
					})
				}
				lines[lineIdx] = beforeComment
				break
			}
		}
	}

	return strings.Join(lines, "\n"), comments, types
}

func (f *Formatter) Format(code string) string {
	return Format(code)
}
