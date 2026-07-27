package js

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// generateExpression dispatches an expression to its JS codegen handler.
// Returns the generated JS string; does NOT write to g.out.
func (g *Generator) generateExpression(expr parser.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.ByteLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.FloatLiteral:
		return fmt.Sprintf("%g", e.Value)
	case *parser.StringLiteral:
		return "\"" + escapeJSString(e.Value) + "\""
	case *parser.CharLiteral:
		return "\"" + escapeJSString(e.Value) + "\""
	case *parser.BooleanLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *parser.NilLiteral:
		return "null"
	case *parser.RegexLiteral:
		return "/" + e.Pattern + "/" + e.Flags
	case *parser.Identifier:
		return e.Value
	case *parser.GroupedExpression:
		return "(" + g.generateExpression(e.Expression) + ")"
	case *parser.PrefixExpression:
		return "(" + e.Operator + g.generateExpression(e.Right) + ")"
	case *parser.InfixExpression:
		return g.generateInfixExpression(e)
	case *parser.ConditionalExpression:
		return "(" + g.generateExpression(e.Condition) + " ? " +
			g.generateExpression(e.Consequence) + " : " +
			g.generateExpression(e.Alternative) + ")"
	case *parser.CallExpression:
		return g.generateCallExpression(e)
	case *parser.DotExpression:
		return g.generateDotExpression(e)
	case *parser.IndexExpression:
		return g.generateExpression(e.Left) + "[" + g.generateExpression(e.Index) + "]"
	case *parser.SliceExpression:
		return g.generateSliceExpression(e)
	case *parser.SliceLiteral:
		return "[" + g.joinExpressions(e.Elements) + "]"
	case *parser.ArrayLiteral:
		return "[" + g.joinExpressions(e.Elements) + "]"
	case *parser.MapLiteral:
		return g.generateMapLiteral(e)
	case *parser.StructLiteral:
		return g.generateStructLiteral(e)
	case *parser.RangeExpression:
		// Standalone range (not in for): emit as array via helper.
		// v1: emit a best-effort comment + array comprehension.
		startStr := g.generateExpression(e.Start)
		endStr := g.generateExpression(e.End)
		// rightInclusive? add +1 to end for inclusive
		endExpr := endStr
		if e.RightInc {
			endExpr = "(" + endStr + " + 1)"
		}
		startExpr := startStr
		if !e.LeftInc {
			startExpr = "(" + startStr + " + 1)"
		}
		return "Array.from({length: " + endExpr + " - " + startExpr +
			"}, (_, i) => i + " + startExpr + ")"
	case *parser.FunctionLiteral:
		return g.generateFunctionLiteral(e)
	case *parser.IfExpression:
		// IfExpression used as an expression → IIFE.
		return g.generateIfExpressionIIFE(e)
	case *parser.CastExpression:
		// Type erasure: `expr as Type` just becomes `expr`.
		return g.generateExpression(e.Expr)
	case *parser.AssignExpression:
		return g.generateExpression(e.Left) + " = " + g.generateExpression(e.Value)
	case *parser.NullableType:
		// Type expression — not meaningful at runtime. Return inner type name or empty.
		return ""
	case *parser.PointerType:
		// Type expression — not meaningful at runtime.
		return ""
	case *parser.RunExpression:
		// JS has no async tasks like Nolang's run; treat as direct call for v1.
		return g.generateExpression(e.Call)
	case *parser.AwaitExpression:
		return "await " + g.generateExpression(e.Right)
	}
	return "/* unhandled: " + fmt.Sprintf("%T", expr) + " */"
}

// generateInfixExpression handles infix operators with type-erasure string concat detection.
//
// CRITICAL: Nolang uses `-` for STRING CONCATENATION. Since we do type erasure, we detect
// string operands (StringLiteral/CharLiteral) at codegen time and emit `+` instead of `-`.
// For variables, we can't tell, so we keep `-`. This is a known v1 limitation.
func (g *Generator) generateInfixExpression(e *parser.InfixExpression) string {
	left := g.generateExpression(e.Left)
	right := g.generateExpression(e.Right)
	op := e.Operator

	// String concatenation detection: Nolang `-` becomes JS `+` when either side is a string/char literal.
	if op == "-" {
		if isStringLikeExpr(e.Left) || isStringLikeExpr(e.Right) {
			op = "+"
		}
	}

	// &^ (bit clear) — JS has no direct equivalent; emit as a runtime expression: (left & ~right)
	if op == "&^" {
		return "(" + left + " & ~" + right + ")"
	}

	return "(" + left + " " + op + " " + right + ")"
}

// isStringLikeExpr reports whether the expression is a StringLiteral or CharLiteral.
func isStringLikeExpr(e parser.Expression) bool {
	switch e.(type) {
	case *parser.StringLiteral, *parser.CharLiteral:
		return true
	}
	return false
}

// generateCallExpression handles function/method/module calls.
func (g *Generator) generateCallExpression(ce *parser.CallExpression) string {
	if ce == nil {
		return ""
	}
	// Check for builtins (print, println, len, etc.) when Function is an Identifier.
	if ident, ok := ce.Function.(*parser.Identifier); ok {
		if js, handled := g.generateBuiltinCall(ce); handled {
			return js
		}
		// Regular function call: name(args)
		args := g.joinExpressions(ce.Arguments)
		return ident.Value + "(" + args + ")"
	}

	// DotExpression: method call (obj.method(args)) or module call (math.sin(x))
	if de, ok := ce.Function.(*parser.DotExpression); ok {
		// Check for module mappings (math.sin → Math.sin, time.now → Date.now, etc.)
		if js, handled := g.generateModuleCall(de, ce.Arguments); handled {
			return js
		}
		// Regular method/module call: receiver.property(args)
		receiver := g.generateExpression(de.Receiver)
		args := g.joinExpressions(ce.Arguments)
		return receiver + "." + de.Property + "(" + args + ")"
	}

	// Generic call: function(args)
	fn := g.generateExpression(ce.Function)
	args := g.joinExpressions(ce.Arguments)
	// ce.GenericArgs — IGNORE (type erasure)
	return fn + "(" + args + ")"
}

// generateDotExpression handles dot access: receiver.property or module constant (math.PI).
func (g *Generator) generateDotExpression(de *parser.DotExpression) string {
	if de == nil {
		return ""
	}
	// Module constant mappings (math.PI → Math.PI, os.args → process.argv)
	if ident, ok := de.Receiver.(*parser.Identifier); ok {
		switch ident.Value {
		case "math":
			return "Math." + de.Property
		case "os":
			if de.Property == "args" {
				return "process.argv"
			}
		}
	}
	return g.generateExpression(de.Receiver) + "." + de.Property
}

// generateSliceExpression handles arr[a..b], arr[..b], arr[a..], arr[..], with bracket variants.
//
// Semantics follow the Nolang range-for convention (verified against llvm/expr.go):
//   - LeftInc=false (open left `(`): start = start + 1
//   - RightInc=true (closed right `]`): end is inclusive → JS slice end = end + 1
//   - RightInc=false (open right `)`): end is exclusive → JS slice end = end
func (g *Generator) generateSliceExpression(se *parser.SliceExpression) string {
	if se == nil {
		return ""
	}
	left := g.generateExpression(se.Left)
	r := se.Range

	if r == nil {
		return left + ".slice()"
	}

	// Compute start
	startStr := ""
	if r.Start == nil {
		startStr = "0"
	} else {
		s := g.generateExpression(r.Start)
		if !r.LeftInc {
			// ( exclusive: start = start + 1
			s = "(" + s + " + 1)"
		}
		startStr = s
	}

	// Compute end
	if r.End == nil {
		// no end bound: slice from start to end
		return left + ".slice(" + startStr + ")"
	}
	endStr := g.generateExpression(r.End)
	if r.RightInc {
		// ] inclusive: JS slice end is exclusive, so add 1 to include the end element
		endStr = "(" + endStr + " + 1)"
	}
	// ) exclusive: end stays as-is (JS slice end is exclusive)

	return left + ".slice(" + startStr + ", " + endStr + ")"
}

// generateMapLiteral emits a JS object literal or new Map(...) depending on key types.
func (g *Generator) generateMapLiteral(ml *parser.MapLiteral) string {
	if ml == nil {
		return "{}"
	}
	// Use plain object when all keys are StringLiteral or Identifier; else Map.
	allSimple := true
	for _, p := range ml.Pairs {
		if !isSimpleMapKey(p.Key) {
			allSimple = false
			break
		}
	}
	if allSimple {
		parts := make([]string, 0, len(ml.Pairs))
		for _, p := range ml.Pairs {
			keyStr := mapKeyToJS(p.Key)
			valStr := g.generateExpression(p.Value)
			parts = append(parts, keyStr+": "+valStr)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	// Fallback: new Map([[k, v], ...])
	parts := make([]string, 0, len(ml.Pairs))
	for _, p := range ml.Pairs {
		keyStr := g.generateExpression(p.Key)
		valStr := g.generateExpression(p.Value)
		parts = append(parts, "["+keyStr+", "+valStr+"]")
	}
	return "new Map([" + strings.Join(parts, ", ") + "])"
}

// generateStructLiteral emits `new <Type>(<field-values>)`.
func (g *Generator) generateStructLiteral(sl *parser.StructLiteral) string {
	if sl == nil {
		return ""
	}
	values := make([]string, 0, len(sl.Fields))
	for _, f := range sl.Fields {
		if f.Value != nil {
			values = append(values, g.generateExpression(f.Value))
		} else {
			values = append(values, "undefined")
		}
	}
	return "new " + sl.Type + "(" + strings.Join(values, ", ") + ")"
}

// generateFunctionLiteral emits an anonymous JS function.
func (g *Generator) generateFunctionLiteral(fl *parser.FunctionLiteral) string {
	if fl == nil {
		return ""
	}
	params := make([]string, 0, len(fl.Parameters))
	for _, p := range fl.Parameters {
		params = append(params, p.Name)
	}

	// Save declaredVars for local scope.
	savedVars := g.declaredVars
	g.declaredVars = make(map[string]bool)
	for _, p := range fl.Parameters {
		g.declaredVars[p.Name] = true
	}

	var sb strings.Builder
	sb.WriteString("function(")
	sb.WriteString(strings.Join(params, ", "))
	sb.WriteString(") {\n")

	// Temporarily redirect output to capture body.
	savedOut := g.out
	g.out = &sb
	savedIndent := g.indentLevel
	savedInFunc := g.inFunctionBody
	g.indentLevel = 1
	g.inFunctionBody = true

	if fl.Body != nil {
		for _, stmt := range fl.Body.Statements {
			g.generateStatement(stmt)
		}
	}

	g.indentLevel = 0
	g.writeLine("}")
	g.out = savedOut
	g.indentLevel = savedIndent
	g.inFunctionBody = savedInFunc
	g.declaredVars = savedVars

	return sb.String()
}

// generateIfExpressionIIFE wraps an IfExpression in an IIFE for use as an expression.
func (g *Generator) generateIfExpressionIIFE(ie *parser.IfExpression) string {
	if ie == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("(() => {\n")

	savedOut := g.out
	g.out = &sb
	savedIndent := g.indentLevel
	g.indentLevel = 1

	g.generateIfStatement(ie)

	g.indentLevel = 0
	g.writeLine("})()")
	g.out = savedOut
	g.indentLevel = savedIndent

	return sb.String()
}

// joinExpressions generates a comma-joined string of all expressions.
func (g *Generator) joinExpressions(exprs []parser.Expression) string {
	parts := make([]string, 0, len(exprs))
	for _, e := range exprs {
		parts = append(parts, g.generateExpression(e))
	}
	return strings.Join(parts, ", ")
}

// escapeJSString escapes a string for use inside JS double-quoted string literals.
func escapeJSString(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		case '\t':
			sb.WriteString("\\t")
		case '\r':
			sb.WriteString("\\r")
		case 0:
			sb.WriteString("\\0")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// isSimpleMapKey reports whether the key is suitable as a plain JS object key.
func isSimpleMapKey(e parser.Expression) bool {
	switch e.(type) {
	case *parser.StringLiteral, *parser.Identifier:
		return true
	}
	return false
}

// mapKeyToJS renders a map key as a JS object key string (without quotes for identifiers, quoted for strings).
func mapKeyToJS(e parser.Expression) string {
	switch k := e.(type) {
	case *parser.StringLiteral:
		return "\"" + escapeJSString(k.Value) + "\""
	case *parser.Identifier:
		return k.Value
	default:
		return ""
	}
}
