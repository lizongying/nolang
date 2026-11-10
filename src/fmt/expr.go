package fmt

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

func (f *formatter) formatExpression(expr parser.Expression) {
	switch e := expr.(type) {
	case *parser.Identifier:
		if e.Value == "self" {
			// 前一個字節是 `.`（區間 `[0..` 之後）時不能寫 `.`，否則拼成三點 token。
			if f.lastByte() == '.' {
				f.write("self")
			} else {
				f.write(".")
			}
		} else {
			f.write(e.Value)
		}
	case *parser.IntegerLiteral:
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
	case *parser.SurfaceMatch:
		f.formatSurfaceMatch(e)
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
	case *parser.ViewType:
		f.write("&")
		if e.Type != nil {
			f.write(e.Type.String())
		}
	case *parser.GroupedExpression:
		f.write("(")
		f.formatExpression(e.Expression)
		f.write(")")
	case *parser.RunExpression:
		f.write("run ")
		f.formatExpression(e.Call)
	case *parser.CoExpression:
		f.write("co ")
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

// maxInlineInfixWidth 是中缀表达式「摊平到单行」时允许的最大宽度。超过它的
// 多行中缀（典型是长串接链，如 INVSBOX）保留逐行拆分；低于它的（如
// `d = 1 + 2`）则合并为单行。该值小于最短的一行字符串拼接对（~135 列），
// 故不影响既有串接对齐行为。
const maxInlineInfixWidth = 76

// inlineWidth 用一个临时的 forceInline formatter 把整个表达式完全摊平，返回其
// 单行宽度。若摊平结果仍含换行（内含函数体 / 块等必须跨行的构造），ok 为 false，
// 表示不可折叠为单行。
func (f *formatter) inlineWidth(e parser.Expression) (int, bool) {
	tmp := &formatter{
		sem:         f.sem,
		trivia:      f.trivia,
		loopStyle:   f.loopStyle,
		forceInline: true,
	}
	tmp.formatExpression(e)
	out := tmp.buf.String()
	if strings.ContainsRune(out, '\n') {
		return 0, false
	}
	return tmp.column, true
}

func (f *formatter) formatInfixExpression(e *parser.InfixExpression) {
	// Boolean-comparison simplification (`x == true` -> `x`, `x == false` ->
	// `! x`) used to be performed here. It was removed on purpose: the rewrite
	// is unsound for operands whose type the formatter cannot observe - most
	// notably an option<?T> such as the result of process.wait-nohang().
	// `exited == true` lowers to a valid option/bool comparison, but the
	// collapsed `exited` / `! exited` used as a control-flow guard triggers a
	// MIR "option-as-i1" codegen crash. Always emit the comparison verbatim
	// through the normal infix path below.
	f.formatExpression(e.Left)

	// Detect multi-line expressions (right operand starts on a different line)
	rightLine := stmtExprEndLine(e.Right)
	leftLine := stmtExprEndLine(e.Left)
	multiLine := rightLine > leftLine

	// A multi-line infix is only worth keeping split when the operands are too
	// wide to sit on one line (the long string-concat idiom). A short one — e.g.
	// `d = 1 +\n2` — was just an incidental break in the source and must collapse
	// to `d = 1 + 2`. Measure the fully flattened width; if it fits within
	// maxInlineInfixWidth (and contains no block that would force a newline),
	// treat it as single-line. forceInline short-circuits so measuring (which
	// re-enters this function) never recurses infinitely.
	if multiLine {
		if f.forceInline {
			multiLine = false
		} else if w, ok := f.inlineWidth(e); ok && w <= maxInlineInfixWidth {
			multiLine = false
		}
	}

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
			// 前一字節是 `.`（區間 `[0..` 之後）時必須寫全 `self.`，否則 `..` + `.` 拼成
			// 三點 token `...`（佔位陳述），輸出不再是合法區間。
			if f.lastByte() == '.' {
				f.write("self.")
			} else {
				f.write(".")
			}
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
	scalar := allScalarLiterals(e.Elements)

	// 純量字面量（num/byte/char/bool 等）一律每行 8 個，避免過長
	if len(e.Elements) > 8 && scalar {
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

	// 字串陣列一律每行一個元素（2026-11-10 定案）：不看元素個數，也不看原始碼
	// 是單行還是同行塞了好幾個，全部拆開。字串不是純量、長度差異大，又常拿來
	// 當查找表，擠在同一行既難讀也難 diff。此規則優先於下面「保留原分行」的路徑；
	// 空陣列沒有元素，維持 []。
	if allStringLiterals(e.Elements) {
		f.write("[")
		f.indent++
		for _, el := range e.Elements {
			f.newline()
			f.formatExpression(el)
			f.write(",")
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

	// 過去只有 >8 個元素才會保留分行：像 `t.evk = [\n 'code',\n 'file-mode',\n
	// 'file-perm'\n]` 這種 3 個字串的表格會被壓成單行，evt 那種長字串甚至
	// 會擠成 200+ 字元的一行。非純量元素（字串 / 複合式）一個就是一個邏輯項，
	// 作者刻意分行時不該合併；純量仍走既有的 ≤8 單行規則，不受影響。
	if len(e.Elements) > 8 || (!scalar && sourceMultiLine) {
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
// allStringLiterals 判斷是否為「字串陣列」：至少一個元素，且每一個都是字串
// 字面量（'...' 或反引號原始串）。混合型別、含運算式或註解的陣列不算，
// 仍走「原始碼跨多行才保留分行」的規則。
func allStringLiterals(elements []parser.Expression) bool {
	if len(elements) == 0 {
		return false
	}
	for _, el := range elements {
		if _, ok := el.(*parser.StringLiteral); !ok {
			return false
		}
	}
	return true
}

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

// formatStructLiteral 輸出結構體**實例化**：`{` 必須緊貼型別名（`zip{}`、
// `net.conn{ ... }`），不插入空白。這與結構體**定義**（`zip { field i64 }`，
// 由 typedef.go 輸出）刻意區分：`x { ... }` 在 parser 裡同時是舊式 match 區塊
// 的候選形狀（W_DEPRECATED_BRACE），型別名後補空格會讓實例化看起來像定義或
// 區塊；緊貼寫法是唯一的規範形態。
func (f *formatter) formatStructLiteral(e *parser.StructLiteral) {
	f.write(e.Type)
	f.write("{")
	if len(e.Fields) == 0 {
		f.write("}")
		return
	}
	f.indent++
	for _, field := range e.Fields {
		// 欄位的 Doc 必須印在該欄位正上方。以前 formatStructLiteral 只輸出
		// `name: value`，欄位註解因而被排到字面量 `}` 之後、變成下一條陳述的
		// Doc（compiler/src/h2m.no 的 s99j 釋放清單本該依附 `rls: []`）。
		// 這裡比照 formatStructDefinition 的寫法保留註解與欄位間的空行。
		if doc := field.GetDoc(); doc != nil && len(doc.List) > 0 {
			f.newline()
			f.formatDocComments(doc)
			lastDocLine := doc.List[len(doc.List)-1].Pos.Line
			if lastDocLine > 0 && field.Token.Line > 0 &&
				f.hasBlankLineBetween(lastDocLine, field.Token.Line) {
				f.write("\n") // blank line (no indent)
			}
		}
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
