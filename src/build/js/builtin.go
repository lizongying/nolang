package js

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// generateBuiltinCall maps Nolang builtin function calls to their JS equivalents.
// Returns (jsCode, true) when the call is a recognized builtin; ("", false) otherwise.
//
// Recognized builtins (when ce.Function is *parser.Identifier):
//   - print/println  → console.log(args)
//   - eprint         → console.error(args)
//   - format         → string concatenation (v1 simplification; see comment below)
//   - len            → (arg).length
//   - ok             → arg (type erasure)
//   - nil            → null
func (g *Generator) generateBuiltinCall(ce *parser.CallExpression) (string, bool) {
	if ce == nil {
		return "", false
	}
	ident, ok := ce.Function.(*parser.Identifier)
	if !ok {
		return "", false
	}
	name := ident.Value
	args := ce.Arguments

	switch name {
	case "print", "println":
		// JS console.log space-separates args. Nolang print auto-appends newline;
		// JS console.log also appends newline.
		return "console.log(" + g.joinExpressions(args) + ")", true

	case "eprint":
		return "console.error(" + g.joinExpressions(args) + ")", true

	case "format":
		// v1 simplification: Nolang uses named format strings {name}.
		// Without runtime format support, we join args with string concatenation.
		// Single-arg: return that arg. Multi-arg: join with space via concatenation.
		if len(args) == 0 {
			return "\"\"", true
		}
		if len(args) == 1 {
			return "(\"\" + " + g.generateExpression(args[0]) + ")", true
		}
		parts := make([]string, 0, len(args))
		for _, a := range args {
			parts = append(parts, g.generateExpression(a))
		}
		return "(\"\" + " + strings.Join(parts, " + \" \" + ") + ")", true

	case "len":
		// v1 limitation: (arg).length works for strings and arrays.
		// For Map, JS uses .size (not .length). This is a known limitation.
		if len(args) == 1 {
			return "(" + g.generateExpression(args[0]) + ").length", true
		}
		return "", false

	case "ok":
		// ok(x) → x (type erasure)
		if len(args) == 1 {
			return g.generateExpression(args[0]), true
		}
		return "", false

	case "nil":
		return "null", true

	case "with-cap", "with-len", "with-cap-len":
		// Capacity/length constructors — for JS, just emit an empty array/string.
		// v1: emit [] (caller will reassign).
		if name == "with-cap" || name == "with-len" || name == "with-cap-len" {
			return "[]", true
		}
	}

	return "", false
}

// generateModuleCall maps Nolang module-qualified calls (math.sin, time.now, str.upper, etc.)
// to their JS equivalents.
// Returns (jsCode, true) when handled; ("", false) otherwise.
func (g *Generator) generateModuleCall(de *parser.DotExpression, args []parser.Expression) (string, bool) {
	if de == nil {
		return "", false
	}
	ident, ok := de.Receiver.(*parser.Identifier)
	if !ok {
		return "", false
	}
	module := ident.Value
	method := de.Property
	argStrs := make([]string, 0, len(args))
	for _, a := range args {
		argStrs = append(argStrs, g.generateExpression(a))
	}
	joinedArgs := strings.Join(argStrs, ", ")

	switch module {
	case "math":
		// math.<fn> → Math.<fn>; math.max/min are special (Math.max/min take varargs).
		return "Math." + method + "(" + joinedArgs + ")", true

	case "time":
		switch method {
		case "now", "now-s", "now-ms", "now-us", "now-ns":
			return "Date.now()", true
		case "sleep", "sleep-ms", "sleep-us", "sleep-ns":
			// v1: emit a no-op or comment; JS is single-threaded and has no blocking sleep.
			return "/* time." + method + " not supported in JS */", true
		}
		return "", false

	case "os":
		switch method {
		case "exit":
			return "process.exit(" + joinedArgs + ")", true
		case "get-env":
			return "process.env[" + joinedArgs + "]", true
		case "set-env":
			// v1: best-effort, 2 args expected.
			if len(args) >= 2 {
				return "process.env[" + argStrs[0] + "] = " + argStrs[1], true
			}
			return "", false
		case "get-wd":
			return "process.cwd()", true
		case "get-pid":
			return "process.pid", true
		case "args":
			return "process.argv", true
		}
		return "", false

	case "str":
		// str.<fn>(s) → (s).<jsMethod>
		// v1: best-effort mapping for common string methods.
		switch method {
		case "upper", "to-upper":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").toUpperCase()", true
			}
		case "lower", "to-lower":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").toLowerCase()", true
			}
		case "trim":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").trim()", true
			}
		case "split":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").split(" + argStrs[1] + ")", true
			}
		case "contains":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").includes(" + argStrs[1] + ")", true
			}
		case "reverse":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").split('').reverse().join('')", true
			}
		case "repeat":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").repeat(" + argStrs[1] + ")", true
			}
		case "starts-with":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").startsWith(" + argStrs[1] + ")", true
			}
		case "ends-with":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").endsWith(" + argStrs[1] + ")", true
			}
		case "index":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").indexOf(" + argStrs[1] + ")", true
			}
		case "empty":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").length === 0", true
			}
		}
		return "", false

	case "vec", "arr":
		// vec/arr module functions — v1: best-effort for common operations.
		switch method {
		case "len":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").length", true
			}
		case "push":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").push(" + argStrs[1] + ")", true
			}
		case "pop":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").pop()", true
			}
		case "contains":
			if len(args) == 2 {
				return "(" + argStrs[0] + ").includes(" + argStrs[1] + ")", true
			}
		case "reverse":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").reverse()", true
			}
		case "clone":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").slice()", true
			}
		case "clear":
			if len(args) == 1 {
				return "(" + argStrs[0] + ").length = 0", true
			}
		}
		return "", false

	case "number", "int", "float":
		// Numeric module functions — v1: best-effort.
		switch method {
		case "max":
			return "Math.max(" + joinedArgs + ")", true
		case "min":
			return "Math.min(" + joinedArgs + ")", true
		case "abs":
			if len(args) == 1 {
				return "Math.abs(" + argStrs[0] + ")", true
			}
		}
		return "", false
	}

	return "", false
}
