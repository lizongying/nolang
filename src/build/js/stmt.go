package js

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// generateStatement dispatches a single statement to its JS codegen handler.
// Writes directly to g.out (does NOT return a string).
func (g *Generator) generateStatement(stmt parser.Statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		g.generateLetStatement(s)
	case *parser.ReturnStatement:
		g.generateReturnStatement(s)
	case *parser.ExpressionStatement:
		// IfExpression used as a statement (bare match/if) → handle as if-statement,
		// not as an IIFE expression.
		if ie, ok := s.Expression.(*parser.IfExpression); ok {
			g.generateIfStatement(ie)
		} else {
			val := g.generateExpression(s.Expression)
			g.writeLine(val + ";")
		}
	case *parser.BlockStatement:
		g.generateBlockStatement(s)
	case *parser.ForStatement:
		g.generateForStatement(s)
	case *parser.BreakStatement:
		// v1: ignore Label — JS does not support labeled break easily.
		g.writeLine("break;")
	case *parser.ContinueStatement:
		g.writeLine("continue;")
	case *parser.MultiAssignStatement:
		g.generateMultiAssignStatement(s)
	case *parser.FunctionDefinition:
		// Methods are emitted inside class bodies by generateStructDefinition.
		// Non-method functions are emitted in Generate phase 2 via this call.
		g.generateFunctionDefinition(s)
	case *parser.StructDefinition:
		g.generateStructDefinition(s)
	case *parser.UseStatement:
		// skip: JS compat layer functions called directly.
	case *parser.ExportStatement:
		// skip.
	case *parser.ExternStatement:
		// skip: FFI declarations not needed in JS.
	case *parser.EnumDefinition:
		g.generateEnumDefinition(s)
	case *parser.InterfaceDefinition:
		// skip: JS has no interfaces.
	case *parser.AnnotationStatement:
		// skip.
	case *parser.TypeAlias:
		// skip: JS has no type aliases.
	case *parser.TaggedEnumDefinition:
		// skip for v1.
	}
}

// generateLetStatement emits `let <name> = <expr>;` (type erasure: stmt.Type ignored).
// If Value is nil, emits `let <name>;`.
// If <name> is already declared (e.g. an out-param pre-declared at function entry),
// emits a plain assignment `<name> = <expr>;` to avoid JS redeclaration errors.
func (g *Generator) generateLetStatement(ls *parser.LetStatement) {
	if ls == nil || ls.Name == nil {
		return
	}
	name := jsIdent(ls.Name.Value)
	alreadyDeclared := g.declaredVars != nil && g.declaredVars[ls.Name.Value]
	// Only skip `let` for globals when inside a function body (not at top-level)
	isGlobal := g.inFunctionBody && g.globalVars != nil && g.globalVars[ls.Name.Value]
	if ls.Value == nil {
		if !alreadyDeclared && !isGlobal {
			g.writeLine("let " + name + ";")
		}
	} else {
		val := g.generateExpression(ls.Value)
		if alreadyDeclared || isGlobal {
			g.writeLine(name + " = " + val + ";")
		} else {
			g.writeLine("let " + name + " = " + val + ";")
		}
	}
	if g.declaredVars != nil {
		g.declaredVars[ls.Name.Value] = true
	}
}

// generateReturnStatement emits `return <expr>;` or `return;`.
// Bare return (ReturnValue == nil) returns the current function's out-params
// (single → `return r;`, multiple → `return [q, r];`), or `return;` if none.
func (g *Generator) generateReturnStatement(rs *parser.ReturnStatement) {
	if rs == nil {
		return
	}
	if rs.ReturnValue == nil {
		if len(g.currentResults) > 0 {
			g.writeLine("return " + g.formatResultsReturn() + ";")
			return
		}
		g.writeLine("return;")
		return
	}
	val := g.generateExpression(rs.ReturnValue)
	g.writeLine("return " + val + ";")
}

// formatResultsReturn builds the return expression for currentResults:
// single → "r"; multiple → "[q, r]".
func (g *Generator) formatResultsReturn() string {
	names := make([]string, 0, len(g.currentResults))
	for _, p := range g.currentResults {
		names = append(names, jsIdent(p.Name))
	}
	if len(names) == 1 {
		return names[0]
	}
	return "[" + strings.Join(names, ", ") + "]"
}

// generateBlockStatement emits `{ ... }` with indented body.
func (g *Generator) generateBlockStatement(bs *parser.BlockStatement) {
	if bs == nil {
		return
	}
	g.writeLine("{")
	g.indentLevel++
	for _, stmt := range bs.Statements {
		g.generateStatement(stmt)
	}
	g.indentLevel--
	g.writeLine("}")
}

// generateIfStatement emits `if (<cond>) { ... } else { ... }`.
// Alternative may be nil.
func (g *Generator) generateIfStatement(ie *parser.IfExpression) {
	if ie == nil {
		return
	}
	cond := g.generateExpression(ie.Condition)
	g.writeLine("if (" + cond + ") {")
	g.indentLevel++
	if ie.Consequence != nil {
		for _, stmt := range ie.Consequence.Statements {
			g.generateStatement(stmt)
		}
	}
	g.indentLevel--
	if ie.Alternative != nil && len(ie.Alternative.Statements) > 0 {
		g.writeLine("} else {")
		g.indentLevel++
		for _, stmt := range ie.Alternative.Statements {
			g.generateStatement(stmt)
		}
		g.indentLevel--
	}
	g.writeLine("}")
}

// generateMultiAssignStatement emits JS destructuring: `let [t1, t2] = <value>;`
// or `[t1, t2] = <value>;` if all targets are already declared.
func (g *Generator) generateMultiAssignStatement(mas *parser.MultiAssignStatement) {
	if mas == nil {
		return
	}
	val := g.generateExpression(mas.Value)
	targets := make([]string, 0, len(mas.Targets))
	anyUndeclared := false
	for _, t := range mas.Targets {
		if ident, ok := t.(*parser.Identifier); ok {
			if g.declaredVars == nil || !g.declaredVars[ident.Value] {
				anyUndeclared = true
			}
		}
		targets = append(targets, g.generateExpression(t))
	}
	joined := strings.Join(targets, ", ")
	if anyUndeclared {
		g.writeLine("let [" + joined + "] = " + val + ";")
		for _, t := range mas.Targets {
			if ident, ok := t.(*parser.Identifier); ok && g.declaredVars != nil {
				g.declaredVars[ident.Value] = true
			}
		}
	} else {
		g.writeLine("[" + joined + "] = " + val + ";")
	}
}

// generateEnumDefinition emits `const <Name> = { <value1>: 0, <value2>: 1, ... };`.
func (g *Generator) generateEnumDefinition(ed *parser.EnumDefinition) {
	if ed == nil {
		return
	}
	parts := make([]string, 0, len(ed.Values))
	for _, v := range ed.Values {
		parts = append(parts, v.Name+": "+fmt.Sprintf("%d", v.Value))
	}
	g.writeLine("const " + jsIdent(ed.Name) + " = { " + strings.Join(parts, ", ") + " };")
}

// generateStructDefinition emits a JS class with a constructor and inline methods.
//
//	class Point {
//	  constructor(x, y) { this.x = x; this.y = y; }
//	  distance() { let self = this; return ...; }
//	}
//
// Methods are looked up from g.methodsByReceiver[sd.Name].
func (g *Generator) generateStructDefinition(sd *parser.StructDefinition) {
	if sd == nil {
		return
	}
	g.writeLine("class " + jsIdent(sd.Name) + " {")
	g.indentLevel++

	// constructor(params...) { this.field = field; ... }
	fieldNames := make([]string, 0, len(sd.Fields))
	for _, f := range sd.Fields {
		fieldNames = append(fieldNames, jsIdent(f.Name))
	}
	if len(fieldNames) > 0 {
		g.writeLine("constructor(" + strings.Join(fieldNames, ", ") + ") {")
	} else {
		g.writeLine("constructor() {")
	}
	g.indentLevel++
	for _, f := range sd.Fields {
		g.writeLine("this." + jsIdent(f.Name) + " = " + jsIdent(f.Name) + ";")
	}
	g.indentLevel--
	g.writeLine("}")

	// methods (collected in Generate phase 1b)
	for _, fd := range g.methodsByReceiver[sd.Name] {
		g.generateMethod(fd)
	}

	g.indentLevel--
	g.writeLine("}")
}

// generateMethod emits a method inside a class body.
// The receiver (first parameter, named "self") becomes `this`.
// A local alias `let self = this;` is emitted so receiver references in the body resolve correctly.
func (g *Generator) generateMethod(fd *parser.FunctionDefinition) {
	if fd == nil {
		return
	}
	methodName := fd.Name
	if idx := strings.Index(fd.Name, "."); idx >= 0 {
		methodName = fd.Name[idx+1:]
	}
	methodName = jsIdent(methodName)
	// Skip the receiver (first parameter); remaining params become method params.
	params := make([]string, 0, len(fd.Parameters))
	for i, p := range fd.Parameters {
		if i == 0 {
			continue // skip receiver
		}
		params = append(params, jsIdent(p.Name))
	}
	g.writeLine(asyncPrefix(methodName) + methodName + "(" + strings.Join(params, ", ") + ") {")
	g.indentLevel++

	// Alias the receiver name to `this` so body references (self.field / self) map to this.
	if len(fd.Parameters) > 0 && fd.Parameters[0].Name != "" {
		g.writeLine("let " + jsIdent(fd.Parameters[0].Name) + " = this;")
	}

	// Declare out-params (Results) as local variables at method entry so that
	// assignments in the body resolve to them and a trailing return can read them.
	savedResults := g.currentResults
	g.currentResults = fd.Results
	for _, r := range fd.Results {
		g.writeLine("let " + r.Name + ";")
		if g.declaredVars != nil {
			g.declaredVars[r.Name] = true
		}
	}

	prevInFunc := g.inFunctionBody
	g.inFunctionBody = true
	needsTrailingReturn := true
	if fd.Body != nil {
		if n := len(fd.Body.Statements); n > 0 {
			if _, ok := fd.Body.Statements[n-1].(*parser.ReturnStatement); ok {
				needsTrailingReturn = false
			}
		}
		for _, stmt := range fd.Body.Statements {
			g.generateStatement(stmt)
		}
	}
	g.inFunctionBody = prevInFunc

	// Auto-emit return for out-params when the body doesn't already end with return.
	if needsTrailingReturn && len(g.currentResults) > 0 {
		g.writeLine("return " + g.formatResultsReturn() + ";")
	}

	g.currentResults = savedResults
	g.indentLevel--
	g.writeLine("}")
}

// generateFunctionDefinition emits a standalone JS function.
// Methods (IsMethodDef && name contains ".") are skipped here — they are emitted
// inside the class body by generateStructDefinition.
func (g *Generator) generateFunctionDefinition(fd *parser.FunctionDefinition) {
	if fd == nil {
		return
	}
	// Methods are emitted inside class bodies; skip standalone emission.
	if fd.IsMethodDef && strings.Contains(fd.Name, ".") {
		return
	}
	params := make([]string, 0, len(fd.Parameters))
	for _, p := range fd.Parameters {
		params = append(params, jsIdent(p.Name))
	}
	g.writeLine(asyncPrefix(fd.Name) + "function " + jsIdent(fd.Name) + "(" + strings.Join(params, ", ") + ") {")
	g.indentLevel++

	// Save and reset declaredVars for function-local scope tracking.
	savedVars := g.declaredVars
	g.declaredVars = make(map[string]bool)
	for _, p := range fd.Parameters {
		g.declaredVars[p.Name] = true
	}

	// Declare out-params (Results) as local variables at function entry so that
	// assignments in the body resolve to them and a trailing return can read them.
	savedResults := g.currentResults
	g.currentResults = fd.Results
	for _, r := range fd.Results {
		g.writeLine("let " + r.Name + ";")
		if g.declaredVars != nil {
			g.declaredVars[r.Name] = true
		}
	}

	prevInFunc := g.inFunctionBody
	g.inFunctionBody = true
	needsTrailingReturn := true
	if fd.Body != nil {
		if n := len(fd.Body.Statements); n > 0 {
			if _, ok := fd.Body.Statements[n-1].(*parser.ReturnStatement); ok {
				needsTrailingReturn = false
			}
		}
		for _, stmt := range fd.Body.Statements {
			g.generateStatement(stmt)
		}
	}
	g.inFunctionBody = prevInFunc

	// Auto-emit return for out-params when the body doesn't already end with return.
	if needsTrailingReturn && len(g.currentResults) > 0 {
		g.writeLine("return " + g.formatResultsReturn() + ";")
	}

	g.currentResults = savedResults
	g.declaredVars = savedVars
	g.indentLevel--
	g.writeLine("}")
}

// generateForStatement handles all ForStatement forms:
//   - IterRange != nil (range/iteration for)
//   - Condition != nil (conditional while)
//   - Init/Condition/Update all set (C-style for)
//   - Infinite loop
func (g *Generator) generateForStatement(fs *parser.ForStatement) {
	if fs == nil {
		return
	}

	// Counted loop: { body } * N
	if fs.CountExpr != nil {
		n := g.generateExpression(fs.CountExpr)
		g.writeLine("for (let __i = 0; __i < " + n + "; __i++) {")
		g.indentLevel++
		if fs.Body != nil {
			for _, stmt := range fs.Body.Statements {
				g.generateStatement(stmt)
			}
		}
		g.indentLevel--
		g.writeLine("}")
		return
	}

	// Range/iteration for
	if fs.IterRange != nil {
		g.generateForIterRange(fs)
		return
	}

	// C-style for: Init + Condition + Update
	if fs.Init != nil && fs.Condition != nil && fs.Update != nil {
		initStr := g.generateStmtInline(fs.Init)
		condStr := g.generateExpression(fs.Condition)
		updateStr := g.generateStmtInline(fs.Update)
		g.writeLine("for (" + initStr + "; " + condStr + "; " + updateStr + ") {")
		g.indentLevel++
		if fs.Body != nil {
			for _, stmt := range fs.Body.Statements {
				g.generateStatement(stmt)
			}
		}
		g.indentLevel--
		g.writeLine("}")
		return
	}

	// Conditional while: Condition != nil
	if fs.Condition != nil {
		condStr := g.generateExpression(fs.Condition)
		g.writeLine("while (" + condStr + ") {")
		g.indentLevel++
		if fs.Body != nil {
			for _, stmt := range fs.Body.Statements {
				g.generateStatement(stmt)
			}
		}
		g.indentLevel--
		g.writeLine("}")
		return
	}

	// Infinite loop
	g.writeLine("while (true) {")
	g.indentLevel++
	if fs.Body != nil {
		for _, stmt := range fs.Body.Statements {
			g.generateStatement(stmt)
		}
	}
	g.indentLevel--
	g.writeLine("}")
}

// generateForIterRange handles range/iteration for-loops (IterRange != nil).
func (g *Generator) generateForIterRange(fs *parser.ForStatement) {
	ir := fs.IterRange
	varName := ir.Variable

	// Numeric range: [a..b], (a..b], [a..b), (a..b)
	if ir.Range != nil {
		r := ir.Range
		startStr := ""
		if r.Start != nil {
			startStr = g.generateExpression(r.Start)
			if !r.LeftInc {
				// ( exclusive left: start = start + 1
				startStr = "(" + startStr + " + 1)"
			}
		}
		endStr := ""
		if r.End != nil {
			endStr = g.generateExpression(r.End)
		}

		// Determine ascending/descending at codegen time when both bounds are integer literals.
		ascending := true
		if startLit, ok := r.Start.(*parser.IntegerLiteral); ok {
			if endLit, ok := r.End.(*parser.IntegerLiteral); ok {
				ascending = startLit.Value <= endLit.Value
			}
		}

		if startStr == "" {
			startStr = "0"
		}

		var condStr string
		if endStr == "" {
			// no end bound — infinite (shouldn't happen in range-for, but handle gracefully)
			condStr = "true"
		} else if ascending {
			// RightInc=true (]) → <= (inclusive); RightInc=false ()) → < (exclusive)
			if r.RightInc {
				condStr = varName + " <= " + endStr
			} else {
				condStr = varName + " < " + endStr
			}
		} else {
			// descending: RightInc=true (]) → >= ; RightInc=false ()) → >
			if r.RightInc {
				condStr = varName + " >= " + endStr
			} else {
				condStr = varName + " > " + endStr
			}
		}

		step := "++"
		if !ascending {
			step = "--"
		}
		g.writeLine("for (let " + varName + " = " + startStr + "; " + condStr + "; " + varName + step + ") {")
		g.indentLevel++
		if fs.Body != nil {
			for _, stmt := range fs.Body.Statements {
				g.generateStatement(stmt)
			}
		}
		g.indentLevel--
		g.writeLine("}")
		return
	}

	// String iteration: for (const v of "string") { ... }
	if ir.RangeStr != "" {
		g.writeLine("for (const " + varName + " of " + g.generateStringLiteralValue(ir.RangeStr) + ") {")
		g.indentLevel++
		if fs.Body != nil {
			for _, stmt := range fs.Body.Statements {
				g.generateStatement(stmt)
			}
		}
		g.indentLevel--
		g.writeLine("}")
		return
	}

	// Slice/identifier iteration: for (const v of <expr>) { ... }
	if ir.RangeExpr != nil {
		exprStr := g.generateExpression(ir.RangeExpr)
		g.writeLine("for (const " + varName + " of " + exprStr + ") {")
		g.indentLevel++
		if fs.Body != nil {
			for _, stmt := range fs.Body.Statements {
				g.generateStatement(stmt)
			}
		}
		g.indentLevel--
		g.writeLine("}")
		return
	}
}

// generateStmtInline renders a statement as a single-line string (no trailing semicolon/newline).
// Used inside for-loop init/update slots where writeLine cannot be used.
func (g *Generator) generateStmtInline(stmt parser.Statement) string {
	if stmt == nil {
		return ""
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name == nil {
			return ""
		}
		if s.Value == nil {
			return "let " + s.Name.Value
		}
		return "let " + s.Name.Value + " = " + g.generateExpression(s.Value)
	case *parser.ExpressionStatement:
		// AssignExpression and other expressions are wrapped in ExpressionStatement.
		return g.generateExpression(s.Expression)
	default:
		return "/* unhandled stmt */"
	}
}

// generateStringLiteralValue converts a Nolang single-quoted string value to a JS double-quoted string literal.
// This is a convenience for IterRange.RangeStr which holds the raw string value.
func (g *Generator) generateStringLiteralValue(s string) string {
	return "\"" + escapeJSString(s) + "\""
}

// asyncPrefix 回傳 "async " 當函式/方法名以 "-async" 結尾，否則回傳空字串。
// 命名慣例：以 -async 結尾的函式會發射為 JS async function。
func asyncPrefix(name string) string {
	if strings.HasSuffix(name, "-async") {
		return "async "
	}
	return ""
}
