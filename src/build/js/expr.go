package js

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// jsIdent converts a Nolang identifier to a valid JS identifier in camelCase.
// Nolang allows hyphens in identifiers (e.g. my-var, WS-CONN); JS does not.
// Hyphens and underscores are treated as word separators and converted to
// camelCase: first word lowercased, subsequent words capitalized.
// Single-word identifiers (no separators) preserve their original case
// (e.g. Point stays Point, sidebar stays sidebar).
// Leading underscores are preserved (e.g. __i → __i).
func jsIdent(name string) string {
	s := strings.ReplaceAll(name, "-", "_")
	// Single word (no underscores): preserve original case.
	if !strings.Contains(s, "_") {
		return s
	}
	parts := strings.Split(s, "_")
	var result strings.Builder
	firstWord := true
	for _, part := range parts {
		if part == "" {
			// Preserve leading/consecutive underscores
			result.WriteString("_")
			continue
		}
		if firstWord {
			result.WriteString(strings.ToLower(part))
			firstWord = false
		} else {
			result.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}
	return result.String()
}

// maybeParen wraps s in parentheses unless expr is a simple identifier,
// in which case the parentheses are unnecessary.
func maybeParen(s string, expr parser.Expression) string {
	if _, ok := expr.(*parser.Identifier); ok {
		return s
	}
	return "(" + s + ")"
}

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
		return jsIdent(e.Value)
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
// CRITICAL: Nolang uses `-` for STRING CONCATENATION. Since we do type erasure, we cannot
// reliably distinguish string concat from numeric subtraction at compile time.
// We use the __nsub runtime helper which checks types at runtime:
//   - both numbers → subtraction (a - b)
//   - otherwise    → string concatenation (String(a) + String(b))
func (g *Generator) generateInfixExpression(e *parser.InfixExpression) string {
	left := g.generateExpression(e.Left)
	right := g.generateExpression(e.Right)
	op := e.Operator

	// Nolang `-` → JS __nsub() runtime helper (handles both string concat and numeric subtraction)
	if op == "-" {
		return "__nsub(" + left + ", " + right + ")"
	}

	// &^ (bit clear) — JS has no direct equivalent; emit as a runtime expression: (left & ~right)
	if op == "&^" {
		return "(" + left + " & ~" + right + ")"
	}

	return "(" + left + " " + op + " " + right + ")"
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
		return jsIdent(ident.Value) + "(" + args + ")"
	}

	// DotExpression: method call (obj.method(args)) or module call (math.sin(x))
	if de, ok := ce.Function.(*parser.DotExpression); ok {
		// Check for module mappings (math.sin → Math.sin, time.now → Date.now, etc.)
		if js, handled := g.generateModuleCall(de, ce.Arguments); handled {
			return js
		}
		// Check for browser method calls on values (el.set-text(t), ctx.fill-rect(...), etc.)
		if js, handled := g.generateBrowserMethodCall(de, ce.Arguments); handled {
			return js
		}
		// Regular method/module call: receiver.property(args)
		receiver := g.generateExpression(de.Receiver)
		args := g.joinExpressions(ce.Arguments)
		return receiver + "." + jsIdent(de.Property) + "(" + args + ")"
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
	// Module constant mappings (math.PI → Math.PI, os.args → process.argv / window.__nolang_args)
	if ident, ok := de.Receiver.(*parser.Identifier); ok {
		switch ident.Value {
		case "math":
			return "Math." + de.Property
		case "os":
			if de.Property == "args" {
				if g.targetEnv == "browser" {
					return "(window.__nolang_args || [])"
				}
				return "process.argv"
			}
		}
	}
	// Special property: .len → .length (Nolang strings/arrays use .len, JS uses .length)
	if de.Property == "len" {
		return g.generateExpression(de.Receiver) + ".length"
	}
	return g.generateExpression(de.Receiver) + "." + jsIdent(de.Property)
}

// generateBrowserMethodCall maps browser-style method calls on values
// (el.set-text(t), ctx.fill-rect(...), etc.) to their JS equivalents.
// Returns (jsCode, true) when handled; ("", false) otherwise.
//
// Only property names in the known browser method set are intercepted; all
// other dot calls fall through to regular method emission.
func (g *Generator) generateBrowserMethodCall(de *parser.DotExpression, args []parser.Expression) (string, bool) {
	if de == nil {
		return "", false
	}
	receiver := maybeParen(g.generateExpression(de.Receiver), de.Receiver)
	argStrs := make([]string, 0, len(args))
	for _, a := range args {
		argStrs = append(argStrs, g.generateExpression(a))
	}
	joinedArgs := strings.Join(argStrs, ", ")

	switch de.Property {
	// DOM element methods
	case "set-text":
		if len(argStrs) >= 1 {
			return receiver + ".textContent = " + argStrs[0], true
		}
	case "get-text":
		return receiver + ".textContent", true
	case "set-html":
		if len(argStrs) >= 1 {
			return receiver + ".innerHTML = " + argStrs[0], true
		}
	case "append-child":
		if len(argStrs) >= 1 {
			return receiver + ".appendChild(" + argStrs[0] + ")", true
		}
	case "remove":
		return receiver + ".remove()", true
	case "set-style":
		if len(argStrs) >= 2 {
			return receiver + ".style[" + argStrs[0] + "] = " + argStrs[1], true
		}
	case "get-style":
		if len(argStrs) >= 1 {
			return "getComputedStyle(" + receiver + ")[" + argStrs[0] + "]", true
		}
	case "set-attr":
		if len(argStrs) >= 2 {
			return receiver + ".setAttribute(" + argStrs[0] + ", " + argStrs[1] + ")", true
		}
	case "get-attr":
		if len(argStrs) >= 1 {
			return receiver + ".getAttribute(" + argStrs[0] + ")", true
		}
	case "add-event-listener":
		if len(argStrs) >= 2 {
			return receiver + ".addEventListener(" + argStrs[0] + ", " + argStrs[1] + ")", true
		}
	case "remove-event-listener":
		if len(argStrs) >= 2 {
			return receiver + ".removeEventListener(" + argStrs[0] + ", " + argStrs[1] + ")", true
		}

	// String methods (on variables — Nolang uses method syntax, JS uses different names)
	case "index":
		if len(argStrs) >= 1 {
			return receiver + ".indexOf(" + argStrs[0] + ")", true
		}
	case "last-index":
		if len(argStrs) >= 1 {
			return receiver + ".lastIndexOf(" + argStrs[0] + ")", true
		}
	case "to-lower":
		return receiver + ".toLowerCase()", true
	case "to-upper":
		return receiver + ".toUpperCase()", true
	case "trim":
		return receiver + ".trim()", true
	case "contains":
		if len(argStrs) >= 1 {
			return receiver + ".includes(" + argStrs[0] + ")", true
		}
	case "starts-with":
		if len(argStrs) >= 1 {
			return receiver + ".startsWith(" + argStrs[0] + ")", true
		}
	case "ends-with":
		if len(argStrs) >= 1 {
			return receiver + ".endsWith(" + argStrs[0] + ")", true
		}
	case "replace":
		if len(argStrs) >= 2 {
			return receiver + ".replaceAll(" + argStrs[0] + ", " + argStrs[1] + ")", true
		}
	case "split":
		if len(argStrs) >= 1 {
			return receiver + ".split(" + argStrs[0] + ")", true
		}
	case "char-at":
		if len(argStrs) >= 1 {
			return receiver + ".charCodeAt(" + argStrs[0] + ")", true
		}
	case "char-to-str":
		if len(argStrs) >= 1 {
			return "String.fromCharCode(" + argStrs[0] + ")", true
		}
	case "slice":
		if len(argStrs) >= 2 {
			return receiver + ".slice(" + argStrs[0] + ", " + argStrs[1] + ")", true
		}
		if len(argStrs) >= 1 {
			return receiver + ".slice(" + argStrs[0] + ")", true
		}
	case "repeat":
		if len(argStrs) >= 1 {
			return receiver + ".repeat(" + argStrs[0] + ")", true
		}

	// Type conversion methods
	case "to-str":
		return "String(" + receiver + ")", true
	case "to-i64":
		return "parseInt(" + receiver + ")", true
	case "to-f64":
		return "parseFloat(" + receiver + ")", true
	case "to-bytes":
		return "new TextEncoder().encode(" + receiver + ")", true

	// Canvas context methods
	case "fill-rect":
		return receiver + ".fillRect(" + joinedArgs + ")", true
	case "clear-rect":
		return receiver + ".clearRect(" + joinedArgs + ")", true
	case "set-fill":
		if len(argStrs) >= 1 {
			return receiver + ".fillStyle = " + argStrs[0], true
		}
	case "set-stroke":
		if len(argStrs) >= 1 {
			return receiver + ".strokeStyle = " + argStrs[0], true
		}
	case "begin-path":
		return receiver + ".beginPath()", true
	case "move-to":
		return receiver + ".moveTo(" + joinedArgs + ")", true
	case "line-to":
		return receiver + ".lineTo(" + joinedArgs + ")", true
	case "stroke":
		return receiver + ".stroke()", true
	case "fill":
		return receiver + ".fill()", true
	case "arc":
		return receiver + ".arc(" + joinedArgs + ")", true

	// CSS class methods
	case "set-class":
		if len(argStrs) >= 1 {
			return receiver + ".className = " + argStrs[0], true
		}
	case "add-class":
		if len(argStrs) >= 1 {
			return receiver + ".classList.add(" + argStrs[0] + ")", true
		}
	case "remove-class":
		if len(argStrs) >= 1 {
			return receiver + ".classList.remove(" + argStrs[0] + ")", true
		}
	case "toggle-class":
		if len(argStrs) >= 1 {
			return receiver + ".classList.toggle(" + argStrs[0] + ")", true
		}

	// Element properties
	case "set-id":
		if len(argStrs) >= 1 {
			return receiver + ".id = " + argStrs[0], true
		}
	case "set-value":
		if len(argStrs) >= 1 {
			return receiver + ".value = " + argStrs[0], true
		}
	case "get-value":
		return receiver + ".value", true
	case "set-placeholder":
		if len(argStrs) >= 1 {
			return receiver + ".placeholder = " + argStrs[0], true
		}
	case "get-parent":
		return receiver + ".parentNode", true
	case "focus":
		return receiver + ".focus()", true
	case "blur":
		return receiver + ".blur()", true

	// Scrolling
	case "scroll-to-bottom":
		return receiver + ".scrollTop = " + receiver + ".scrollHeight", true
	case "get-scroll-height":
		return receiver + ".scrollHeight", true
	case "get-scroll-top":
		return receiver + ".scrollTop", true
	case "set-scroll-top":
		if len(argStrs) >= 1 {
			return receiver + ".scrollTop = " + argStrs[0], true
		}

	// DOM manipulation
	case "prepend":
		if len(argStrs) >= 1 {
			return receiver + ".prepend(" + argStrs[0] + ")", true
		}
	case "replace-with":
		if len(argStrs) >= 1 {
			return receiver + ".replaceWith(" + argStrs[0] + ")", true
		}
	case "insert-before":
		if len(argStrs) >= 2 {
			return receiver + ".insertBefore(" + argStrs[0] + ", " + argStrs[1] + ")", true
		}
	case "remove-all-children":
		return receiver + ".innerHTML = ''", true
	case "get-children":
		return receiver + ".children", true
	case "contains-child":
		if len(argStrs) >= 1 {
			return receiver + ".contains(" + argStrs[0] + ")", true
		}
	}
	return "", false
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
	return "new " + jsIdent(sl.Type) + "(" + strings.Join(values, ", ") + ")"
}

// generateFunctionLiteral emits an anonymous JS function.
func (g *Generator) generateFunctionLiteral(fl *parser.FunctionLiteral) string {
	if fl == nil {
		return ""
	}
	params := make([]string, 0, len(fl.Parameters))
	for _, p := range fl.Parameters {
		params = append(params, jsIdent(p.Name))
	}

	// Save declaredVars for local scope.
	savedVars := g.declaredVars
	g.declaredVars = make(map[string]bool)
	for _, p := range fl.Parameters {
		g.declaredVars[p.Name] = true // track original name for scope resolution
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
		return jsIdent(k.Value)
	default:
		return ""
	}
}
