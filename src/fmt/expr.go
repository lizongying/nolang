package fmt

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

func (f *formatter) formatExpression(expr parser.Expression) {
	switch e := expr.(type) {
	case *parser.Identifier:
		if e.Value == "self" {
			f.write(".")
		} else {
			f.write(e.Value)
		}
	case *parser.IntegerLiteral:
		f.write(lowerHexLiteral(e.Token.Literal))
	case *parser.ByteLiteral:
		f.write(lowerHexLiteral(e.Token.Literal))
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
		// 純量字面量（num/byte/char/bool 等）一律每行 8 個，避免過長
		if allScalarLiterals(e.Elements) {
			f.write("[")
			f.indent++
			for i, el := range e.Elements {
				if i > 0 && i%8 != 0 {
					f.write(", ")
				}
				if i%8 == 0 {
					f.newline()
				}
				f.formatExpression(el)
				if i%8 == 7 || i == len(e.Elements)-1 {
					f.write(",")
				}
			}
			f.indent--
			f.newline()
			f.write("]")
			return
		}

		// 偵測源碼是否已跨多行；若是，保留原始分行結構（例如 SHAPES 每行 8 個值）
		sourceMultiLine := false
		if len(e.Elements) >= 2 {
			firstLine := e.Elements[0].Pos().Line
			for i := 1; i < len(e.Elements); i++ {
				if e.Elements[i].Pos().Line != firstLine {
					sourceMultiLine = true
					break
				}
			}
		}

		f.write("[")
		f.indent++
		prevLine := 0
		for i, el := range e.Elements {
			curLine := el.Pos().Line
			if i == 0 {
				f.newline()
			} else if sourceMultiLine && curLine != prevLine {
				f.newline()
			} else if !sourceMultiLine {
				f.newline()
			} else {
				f.write(" ")
			}
			f.formatExpression(el)
			f.write(",")
			prevLine = curLine
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

// allScalarLiterals 判斷所有元素是否為純量字面量（num/byte/char/bool 等），
// 包含整數、浮點數、字元、布林字面量，以及其前綴運算（如負號）。
// 用於決定是否套用每行 8 個的緊湊格式。

// allScalarLiterals 判斷所有元素是否為純量字面量（num/byte/char/bool 等），
// 包含整數、浮點數、字元、布林字面量，以及其前綴運算（如負號）。
// 用於決定是否套用每行 8 個的緊湊格式。
func allScalarLiterals(elements []parser.Expression) bool {
	for _, el := range elements {
		if !isScalarLiteral(el) {
			return false
		}
	}
	return true
}

func isScalarLiteral(e parser.Expression) bool {
	switch v := e.(type) {
	case *parser.IntegerLiteral, *parser.FloatLiteral, *parser.CharLiteral, *parser.BooleanLiteral:
		return true
	case *parser.PrefixExpression:
		return isScalarLiteral(v.Right)
	}
	return false
}

func (f *formatter) formatStructLiteral(e *parser.StructLiteral) {
	f.write(e.Type)
	f.write(" {")
	if len(e.Fields) == 0 {
		f.write("}")
		return
	}
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

func (f *formatter) formatFunctionLiteral(e *parser.FunctionLiteral) {
	f.write("(")
	f.formatParameters(e.Parameters, e.IsVariadic)
	f.write(")")
	f.write(" {")
	f.indent++
	f.formatBlockInner(e.Body, e.Body.Token.Line)
	f.indent--
	f.newline()
	f.write("}")
}

// formatProgram parses and formats the given code, returning the formatted
// output (without any guarantee about a trailing newline), a bool indicating
// success, and any parser error messages. On parse error or
// empty/whitespace-only input the bool is false, out is empty, and errs holds
// the parser errors (nil for the empty-input case).

// lowerHexLiteral converts an uppercase hex literal to lowercase.
// Handles two forms:
//   - "0xFF" / "0XFF" → "0xff"  (integer hex literal)
//   - "xFF"            → "xff"   (byte literal)
//
// Non-hex literals are returned unchanged.
func lowerHexLiteral(literal string) string {
	if len(literal) >= 2 && literal[0] == '0' && (literal[1] == 'x' || literal[1] == 'X') {
		return "0x" + strings.ToLower(literal[2:])
	}
	if len(literal) == 3 && literal[0] == 'x' {
		return "x" + strings.ToLower(literal[1:])
	}
	return literal
}
