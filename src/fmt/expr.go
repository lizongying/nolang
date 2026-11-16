package fmt

import (
	"strings"

	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
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
		boolFacts:   f.boolFacts,
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
	// Redundant boolean comparison simplification (`x == true` -> `x`,
	// `x == false` -> `! x`) is a documented code-style rule (see
	// docs/docs/lang/code-style.md). It was reverted in v0.3.3 because an
	// option<?T> operand collapsed into a bare guard crashed MIR codegen
	// ("option-as-i1"). The rule now fires ONLY for operands proven to be
	// bool values by boolComparisonFacts (local facts plus any injected
	// cross-module signatures); option operands and every unknown operand
	// keep the comparison verbatim.
	if f.tryFormatBoolComparison(e) {
		return
	}
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

// tryFormatBoolComparison handles `x == true`, `x == false`, `x != true` and
// `x != false` (plus the mirrored `true == x` forms) by emitting the simplified
// boolean expression directly. It returns true when it produced output, false
// when the caller should fall back to the normal infix formatting. The rewrite
// only fires when *exactly one* operand is a boolean literal, so `true ==
// false` is left untouched. `x == true` -> `x`; `x == false` -> `! x`; `!=`
// flips the polarity. When the surviving operand is not atomic it is wrapped in
// parentheses so the emitted `!` cannot silently re-bind (e.g. `a + b == false`
// -> `! (a + b)`, not `! a + b`).
//
// Two independent guards, in this order:
//
//  1. Option hard gate — an operand known to be `?T` is NEVER simplified,
//     regardless of whether its annotation survives the CLI's
//     fixRedundantTypeSource pass and regardless of what the proof table
//     says. `exited == true` is a valid option/bool comparison, but the
//     collapsed `exited` / `! exited` used as a control-flow guard cannot be
//     lowered to i1 by MIR (the v0.3.3 revert). This check is deliberately
//     separate from the proof table so a future widening of the bool evidence
//     rules cannot reintroduce that crash.
//  2. Known-value guard — bool-ness must be *proven* by the evidence table
//     (local facts plus cross-module signatures when a ParseConfig is
//     injected; see boolComparisonFacts). Everything unproven — an unknown
//     cross-module call result, a bare name, a dot/call/infix operand — is
//     unknown and keeps its comparison.
func (f *formatter) tryFormatBoolComparison(e *parser.InfixExpression) bool {
	if e.Operator != "==" && e.Operator != "!=" {
		return false
	}
	lb, lok := e.Left.(*parser.BooleanLiteral)
	rb, rok := e.Right.(*parser.BooleanLiteral)
	if lok && rok {
		return false
	}
	var operand parser.Expression
	var expectTrue bool
	switch {
	case rok:
		operand, expectTrue = e.Left, rb.Value
	case lok:
		operand, expectTrue = e.Right, lb.Value
	default:
		return false
	}
	// option 是絕對禁令，與標註是否被移除無關；必須先於 bool 證據判定。
	if f.boolOperandIsOptionTyped(operand) {
		return false
	}
	// 已知的化簡，未知的不化簡：只有單檔可確證為 bool 值的運算元才重寫。
	if !f.boolOperandIsKnownValue(operand) {
		return false
	}
	if e.Operator == "!=" {
		expectTrue = !expectTrue
	}
	if expectTrue {
		f.formatExpression(operand)
	} else {
		f.write("! ")
		if boolOperandIsAtomic(operand) {
			f.formatExpression(operand)
		} else {
			f.write("(")
			f.formatExpression(operand)
			f.write(")")
		}
	}
	return true
}

// boolOperandIsKnownValue reports whether the surviving operand is *proven* to
// be a plain bool value by the evidence table. Anything else — an option<?T>,
// a cross-module call result with no visible signature, a bare name, or a
// dot/call/infix expression — is unknown and keeps its comparison verbatim.
func (f *formatter) boolOperandIsKnownValue(e parser.Expression) bool {
	for {
		g, ok := e.(*parser.GroupedExpression)
		if !ok || g.Expression == nil {
			break
		}
		e = g.Expression
	}
	id, ok := e.(*parser.Identifier)
	if !ok {
		return false
	}
	return f.boolFacts.isKnownValue(id.Value)
}

// boolOperandIsOptionTyped is the hard gate for option<?T> operands. It runs
// BEFORE boolOperandIsKnownValue so that "option never simplifies" is an
// absolute rule rather than a side effect of the proof table: even if the
// table ever gains new bool evidence, a `?T` sighting still wins.
//
// `no fmt` removes redundant type annotations (fixRedundantTypeSource) before
// formatting, so `ok ?bool = fs.write-str(...)` may arrive here as
// `ok = fs.write-str(...)`. That is fine and intentional: with the annotation
// gone the operand is unknown without a config, or classified as option from
// the injected signature with one — both keep the comparison. Both spellings
// are covered by bool_comparison_test.go. Type checks use the structured
// ty.Type.IsOption() API (never `?` prefix string parsing), so the verify-ty
// gate stays clean.
func (f *formatter) boolOperandIsOptionTyped(e parser.Expression) bool {
	for {
		g, ok := e.(*parser.GroupedExpression)
		if !ok || g.Expression == nil {
			break
		}
		e = g.Expression
	}
	id, ok := e.(*parser.Identifier)
	if !ok {
		return false
	}
	// Evidence collected from explicit `?T` annotations, `?T` parameters and
	// local functions declared to return `?T`.
	if f.boolFacts != nil && f.boolFacts.option[id.Value] {
		return true
	}
	// Semantic side-table from the fresh parse (explicit annotations and the
	// few locally inferred option types, e.g. `x = 1 + 2` -> ?i64).
	if f.sem == nil {
		return false
	}
	if t, ok := f.sem.VarTypeRef(id.Value); ok && t != nil && t.IsOption() {
		return true
	}
	for _, vars := range f.sem.FuncVarTypes {
		if t, ok := vars[id.Value]; ok && t != nil && t.IsOption() {
			return true
		}
	}
	return false
}

// boolComparisonFacts is the evidence table for the boolean comparison rule.
// Without an injected ParseConfig (bare single-file parse, fmt unit tests,
// LSP pure formatting) the only provable bool values come from three local
// sources:
//
//  1. an explicit type annotation (`flag bool = ...`, `(b bool)` parameter);
//  2. a boolean literal assignment (`flag = true`, `flag = false`);
//  3. a call to a function DEFINED IN THIS FILE whose declared result is bool.
//
// With a config injected (the `no fmt` CLI path), cross-module result types
// from sem.ExternFuncSigs / ExternMethodSigs join that table — see
// bool_extern.go. Everything still unproven is unknown and deliberately left
// alone: "known simplifies, unknown does not" holds in both modes. A std call
// such as `ok = fs.write-str(...)` returns ?bool, so classify marks it option
// and the hard gate keeps the comparison verbatim either way.
//
// `unknown` is a one-way downgrade: the same variable name may be a bool in
// one function and an option/unknown in another, and a name-keyed table must
// not let the bool sighting win. Once a name is downgraded it never comes back.
type boolComparisonFacts struct {
	value   map[string]bool // proven plain bool value
	option  map[string]bool // proven ?T
	unknown map[string]bool // ambiguous / unprovable sighting
}

func (b *boolComparisonFacts) isKnownValue(name string) bool {
	return b != nil && b.value[name] && !b.option[name] && !b.unknown[name]
}

func (b *boolComparisonFacts) classify(name string, t *ty.Type) {
	if name == "" || t == nil || !t.IsValid() {
		return
	}
	switch {
	case t.IsOption():
		b.option[name] = true
		delete(b.value, name)
	case t.IsBool():
		if !b.unknown[name] && !b.option[name] {
			b.value[name] = true
		}
	default:
		b.unknown[name] = true
		delete(b.value, name)
	}
}

func (b *boolComparisonFacts) markValue(name string) {
	if name != "" && !b.unknown[name] && !b.option[name] {
		b.value[name] = true
	}
}

func (b *boolComparisonFacts) markUnknown(name string) {
	if name != "" {
		b.unknown[name] = true
		delete(b.value, name)
	}
}

// collectBoolComparisonFacts builds the evidence table once per formatted
// program. The AST walk is intentionally best-effort: a statement shape it
// does not recognise simply contributes no evidence, which is the safe
// direction (unknown operands are never simplified).
func collectBoolComparisonFacts(p *parser.Program, sem *parser.SemanticContext) *boolComparisonFacts {
	b := &boolComparisonFacts{
		value:   make(map[string]bool),
		option:  make(map[string]bool),
		unknown: make(map[string]bool),
	}
	if p == nil {
		return b
	}

	// Result types for calls: definitions in THIS file first, then any
	// cross-module signatures injected via ParseSourceWithConfig. Local
	// definitions win, matching build's "local first" merge order. A qualified
	// method name (`str.starts-with`) is stored as written, so both bare and
	// qualified call spellings can be looked up.
	localResults := make(map[string]*ty.Type)
	for _, s := range p.Statements {
		if fd, ok := s.(*parser.FunctionDefinition); ok {
			if rs := parser.DeclaredResults(fd); len(rs) > 0 && rs[0].Type != nil {
				if t := ty.Parse(rs[0].Type.String()); t.IsValid() {
					localResults[fd.Name] = t
				}
			}
		}
	}
	// Without an injected config the extern tables are empty, so the
	// single-file "unknown wins" behaviour is preserved byte-for-byte.
	for k, t := range externResultTypes(sem) {
		if _, exists := localResults[k]; !exists {
			localResults[k] = t
		}
	}
	// Method calls keyed by a VARIABLE receiver (`s.starts-with`) need the
	// receiver's already-inferred type to reach the type-keyed method table.
	// Exact keys above win; this only fills the gap.
	for k, t := range externMethodResultsForVars(sem) {
		if _, exists := localResults[k]; !exists {
			localResults[k] = t
		}
	}

	// Semantic side-table from the fresh parse: declared annotations plus the
	// few types the parser infers locally (notably `x = 1 + 2` -> ?i64).
	if sem != nil {
		for name, t := range sem.VarTypes {
			b.classify(name, t)
		}
		for _, vars := range sem.FuncVarTypes {
			for name, t := range vars {
				b.classify(name, t)
			}
		}
	}

	b.walkStatements(p.Statements, localResults)
	return b
}

func (b *boolComparisonFacts) walkStatements(stmts []parser.Statement, local map[string]*ty.Type) {
	for _, s := range stmts {
		switch n := s.(type) {
		case *parser.LetStatement:
			b.recordLet(n, local)
			b.walkExpr(n.Value, local)
		case *parser.FunctionDefinition:
			for _, param := range n.Parameters {
				if param != nil && param.Type != nil {
					b.classify(param.Name, ty.Parse(param.Type.String()))
				}
			}
			if n.Body != nil {
				b.walkStatements(n.Body.Statements, local)
			}
		case *parser.BlockStatement:
			b.walkStatements(n.Statements, local)
		case *parser.ExpressionStatement:
			b.walkExpr(n.Expression, local)
		case *parser.ForStatement:
			if n.Init != nil {
				b.walkStatements([]parser.Statement{n.Init}, local)
			}
			if n.Update != nil {
				b.walkStatements([]parser.Statement{n.Update}, local)
			}
			if n.Body != nil {
				b.walkStatements(n.Body.Statements, local)
			}
		}
	}
}

func (b *boolComparisonFacts) recordLet(n *parser.LetStatement, local map[string]*ty.Type) {
	if n == nil || n.Name == nil {
		return
	}
	name := n.Name.Value
	if n.Type != nil {
		if t := ty.Parse(n.Type.String()); t.IsValid() {
			b.classify(name, t)
			return
		}
	}
	switch v := n.Value.(type) {
	case *parser.BooleanLiteral:
		b.markValue(name)
	case *parser.CallExpression:
		callee, ok := callResultName(v)
		if !ok {
			b.markUnknown(name)
			return
		}
		if t, exists := local[callee]; exists {
			b.classify(name, t)
			return
		}
		// No visible signature (no injected config, or module not loaded):
		// the result type is unknown — never assume bool.
		b.markUnknown(name)
	default:
		b.markUnknown(name)
	}
}

func (b *boolComparisonFacts) walkExpr(e parser.Expression, local map[string]*ty.Type) {
	switch n := e.(type) {
	case *parser.IfExpression:
		if n.Consequence != nil {
			b.walkStatements(n.Consequence.Statements, local)
		}
		if n.Alternative != nil {
			b.walkStatements(n.Alternative.Statements, local)
		}
	case *parser.SurfaceMatch:
		parser.WalkSurfaceMatchArmBodies(n, func(stmts []parser.Statement) {
			b.walkStatements(stmts, local)
		})
	case *parser.FunctionLiteral:
		if n.Body != nil {
			b.walkStatements(n.Body.Statements, local)
		}
	}
}

// boolOperandIsAtomic reports whether an expression can follow a prefix `!`
// without parentheses - i.e. it is a primary/postfix form whose own precedence
// is at least that of unary not.
func boolOperandIsAtomic(e parser.Expression) bool {
	switch e.(type) {
	case *parser.Identifier,
		*parser.IntegerLiteral,
		*parser.FloatLiteral,
		*parser.StringLiteral,
		*parser.CharLiteral,
		*parser.RegexLiteral,
		*parser.BooleanLiteral,
		*parser.NilLiteral,
		*parser.CallExpression,
		*parser.DotExpression,
		*parser.IndexExpression,
		*parser.SliceExpression,
		*parser.GroupedExpression,
		*parser.ArrayLiteral,
		*parser.SliceLiteral,
		*parser.StructLiteral,
		*parser.MapLiteral,
		*parser.PrefixExpression:
		return true
	}
	return false
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
