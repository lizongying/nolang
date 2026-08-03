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
			return maybeParen(g.generateExpression(args[0]), args[0]) + ".length", true
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

	// recv0 is argStrs[0] with conditional parentheses for use as a method receiver.
	// Simple identifiers don't need parentheses; complex expressions do.
	recv0 := ""
	if len(args) >= 1 {
		recv0 = maybeParen(argStrs[0], args[0])
	}

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
		if g.targetEnv == "browser" {
			switch method {
			case "exit":
				return "throw new Error(\"__nolang_exit:\" + " + joinedArgs + ")", true
			case "get-env":
				return "(window.__nolang_env || {})[" + joinedArgs + "]", true
			case "set-env":
				if len(args) >= 2 {
					return "(window.__nolang_env = window.__nolang_env || {})[" + argStrs[0] + "] = " + argStrs[1], true
				}
				return "", false
			case "get-wd":
				return "location.href", true
			case "get-pid":
				return "0", true
			case "args":
				return "(window.__nolang_args || [])", true
			}
			return "", false
		}
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
				return recv0 + ".toUpperCase()", true
			}
		case "lower", "to-lower":
			if len(args) == 1 {
				return recv0 + ".toLowerCase()", true
			}
		case "trim":
			if len(args) == 1 {
				return recv0 + ".trim()", true
			}
		case "split":
			if len(args) == 2 {
				return recv0 + ".split(" + argStrs[1] + ")", true
			}
		case "contains":
			if len(args) == 2 {
				return recv0 + ".includes(" + argStrs[1] + ")", true
			}
		case "reverse":
			if len(args) == 1 {
				return recv0 + ".split('').reverse().join('')", true
			}
		case "repeat":
			if len(args) == 2 {
				return recv0 + ".repeat(" + argStrs[1] + ")", true
			}
		case "starts-with":
			if len(args) == 2 {
				return recv0 + ".startsWith(" + argStrs[1] + ")", true
			}
		case "ends-with":
			if len(args) == 2 {
				return recv0 + ".endsWith(" + argStrs[1] + ")", true
			}
		case "index":
			if len(args) == 2 {
				return recv0 + ".indexOf(" + argStrs[1] + ")", true
			}
		case "last-index":
			if len(args) == 2 {
				return recv0 + ".lastIndexOf(" + argStrs[1] + ")", true
			}
		case "replace":
			if len(args) == 3 {
				return recv0 + ".replaceAll(" + argStrs[1] + ", " + argStrs[2] + ")", true
			}
		case "slice":
			if len(args) == 3 {
				return recv0 + ".slice(" + argStrs[1] + ", " + argStrs[2] + ")", true
			}
			if len(args) == 2 {
				return recv0 + ".slice(" + argStrs[1] + ")", true
			}
		case "char-at":
			if len(args) == 2 {
				return recv0 + ".charCodeAt(" + argStrs[1] + ")", true
			}
		case "char-to-str":
			if len(args) == 2 {
				return "String.fromCharCode(" + argStrs[1] + ")", true
			}
		case "empty":
			if len(args) == 1 {
				return recv0 + ".length === 0", true
			}
		}
		return "", false

	case "vec", "arr":
		// vec/arr module functions — v1: best-effort for common operations.
		switch method {
		case "len":
			if len(args) == 1 {
				return recv0 + ".length", true
			}
		case "push":
			if len(args) == 2 {
				return recv0 + ".push(" + argStrs[1] + ")", true
			}
		case "pop":
			if len(args) == 1 {
				return recv0 + ".pop()", true
			}
		case "contains":
			if len(args) == 2 {
				return recv0 + ".includes(" + argStrs[1] + ")", true
			}
		case "reverse":
			if len(args) == 1 {
				return recv0 + ".reverse()", true
			}
		case "clone":
			if len(args) == 1 {
				return recv0 + ".slice()", true
			}
		case "clear":
			if len(args) == 1 {
				return recv0 + ".length = 0", true
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
		case "char-to-str":
			if len(args) == 1 {
				return "String.fromCharCode(" + argStrs[0] + ")", true
			}
		}
		return "", false

	case "dom":
		switch method {
		case "get-element-by-id":
			if len(args) >= 1 {
				return "document.getElementById(" + argStrs[0] + ")", true
			}
		case "query-selector":
			if len(args) >= 1 {
				return "document.querySelector(" + argStrs[0] + ")", true
			}
		case "create-element":
			if len(args) >= 1 {
				return "document.createElement(" + argStrs[0] + ")", true
			}
		case "query-all":
			if len(args) >= 1 {
				return "document.querySelectorAll(" + argStrs[0] + ")", true
			}
		case "create-text-node":
			if len(args) >= 1 {
				return "document.createTextNode(" + argStrs[0] + ")", true
			}
		case "body":
			return "document.body", true
		case "head":
			return "document.head", true
		case "load-script":
			if len(args) >= 1 {
				return "(function() { var s = document.createElement('script'); s.src = " + argStrs[0] + "; document.head.appendChild(s); })()", true
			}
		case "load-script-callback":
			if len(args) >= 2 {
				return "(function() { var s = document.createElement('script'); s.src = " + argStrs[0] + "; s.onload = " + argStrs[1] + "; document.head.appendChild(s); })()", true
			}
		case "load-style":
			if len(args) >= 1 {
				return "(function() { var l = document.createElement('link'); l.rel = 'stylesheet'; l.href = " + argStrs[0] + "; document.head.appendChild(l); })()", true
			}
		}
		return "", false

	case "events":
		switch method {
		case "on-click":
			if len(args) >= 2 {
				return recv0 + ".addEventListener('click', " + argStrs[1] + ")", true
			}
		case "on-load":
			if len(args) >= 1 {
				return "window.addEventListener('load', " + argStrs[0] + ")", true
			}
		}
		return "", false

	case "canvas":
		switch method {
		case "get-context-2d":
			if len(args) >= 1 {
				return recv0 + ".getContext('2d')", true
			}
		case "get-width":
			if len(args) >= 1 {
				return recv0 + ".width", true
			}
		case "get-height":
			if len(args) >= 1 {
				return recv0 + ".height", true
			}
		}
		return "", false

	case "storage":
		switch method {
		case "set-item":
			if len(args) >= 2 {
				return "localStorage.setItem(" + argStrs[0] + ", " + argStrs[1] + ")", true
			}
		case "get-item":
			if len(args) >= 1 {
				return "localStorage.getItem(" + argStrs[0] + ")", true
			}
		case "remove-item":
			if len(args) >= 1 {
				return "localStorage.removeItem(" + argStrs[0] + ")", true
			}
		case "clear":
			return "localStorage.clear()", true
		}
		return "", false

	case "location":
		switch method {
		case "href":
			return "location.href", true
		case "search":
			return "location.search", true
		case "path":
			return "location.pathname", true
		case "host":
			return "location.host", true
		case "redirect":
			if len(args) >= 1 {
				return "location.href = " + argStrs[0], true
			}
		}
		return "", false

	case "history":
		switch method {
		case "back":
			return "history.back()", true
		case "forward":
			return "history.forward()", true
		case "push":
			if len(args) >= 1 {
				return "history.pushState({}, '', " + argStrs[0] + ")", true
			}
		case "length":
			return "history.length", true
		}
		return "", false

	case "animation":
		switch method {
		case "request-frame":
			if len(args) >= 1 {
				return "requestAnimationFrame(" + argStrs[0] + ")", true
			}
		case "cancel-frame":
			if len(args) >= 1 {
				return "cancelAnimationFrame(" + argStrs[0] + ")", true
			}
		}
		return "", false

	case "fetch":
		switch method {
		case "async":
			if len(args) >= 1 {
				return "fetch(" + argStrs[0] + ").then(function(r) { return r.text(); })", true
			}
		case "json-async":
			if len(args) >= 1 {
				return "fetch(" + argStrs[0] + ").then(function(r) { return r.json(); })", true
			}
		case "post-async":
			if len(args) >= 2 {
				return "fetch(" + argStrs[0] + ", { method: 'POST', body: " + argStrs[1] + " }).then(function(r) { return r.text(); })", true
			}
		case "post-json-async":
			if len(args) >= 2 {
				return "fetch(" + argStrs[0] + ", { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(" + argStrs[1] + ") }).then(function(r) { return r.json(); })", true
			}
		}
		return "", false

	case "ws-browser":
		switch method {
		case "connect":
			if len(args) >= 1 {
				return "new WebSocket(" + argStrs[0] + ")", true
			}
		case "on-open":
			if len(args) >= 2 {
				return recv0 + ".onopen = " + argStrs[1], true
			}
		case "on-message":
			if len(args) >= 2 {
				return recv0 + ".onmessage = function(e) { (" + argStrs[1] + ")(e.data); }", true
			}
		case "on-close":
			if len(args) >= 2 {
				return recv0 + ".onclose = " + argStrs[1], true
			}
		case "on-error":
			if len(args) >= 2 {
				return recv0 + ".onerror = " + argStrs[1], true
			}
		case "send":
			if len(args) >= 2 {
				return recv0 + ".send(" + argStrs[1] + ")", true
			}
		case "send-json":
			if len(args) >= 2 {
				return recv0 + ".send(JSON.stringify(" + argStrs[1] + "))", true
			}
		case "close":
			if len(args) >= 1 {
				return recv0 + ".close()", true
			}
		case "ready-state":
			if len(args) >= 1 {
				return recv0 + ".readyState", true
			}
		}
		return "", false

	case "json":
		switch method {
		case "parse":
			if len(args) >= 1 {
				return "JSON.parse(" + argStrs[0] + ")", true
			}
		case "stringify":
			if len(args) >= 1 {
				return "JSON.stringify(" + argStrs[0] + ")", true
			}
		case "stringify-pretty":
			if len(args) >= 1 {
				return "JSON.stringify(" + argStrs[0] + ", null, 2)", true
			}
		}
		return "", false

	case "timer":
		switch method {
		case "set-interval":
			if len(args) >= 2 {
				return "setInterval(" + argStrs[0] + ", " + argStrs[1] + ")", true
			}
		case "clear-interval":
			if len(args) >= 1 {
				return "clearInterval(" + argStrs[0] + ")", true
			}
		case "set-timeout":
			if len(args) >= 2 {
				return "setTimeout(" + argStrs[0] + ", " + argStrs[1] + ")", true
			}
		case "clear-timeout":
			if len(args) >= 1 {
				return "clearTimeout(" + argStrs[0] + ")", true
			}
		}
		return "", false

	case "monaco":
		switch method {
		case "init":
			if len(args) >= 2 {
				return "(function() { var s = document.createElement('script'); s.src = " + argStrs[0] + " + '/loader.js'; s.onload = function() { require.config({ paths: { vs: " + argStrs[0] + " + '/vs' } }); require(['vs/editor/editor.main'], " + argStrs[1] + "); }; document.head.appendChild(s); })()", true
			}
		case "create":
			if len(args) >= 3 {
				return "monaco.editor.create(" + argStrs[0] + ", { value: " + argStrs[1] + ", language: " + argStrs[2] + " })", true
			}
		case "create-opts":
			if len(args) >= 2 {
				return "monaco.editor.create(" + argStrs[0] + ", " + argStrs[1] + ")", true
			}
		case "get-value":
			if len(args) >= 1 {
				return recv0 + ".getValue()", true
			}
		case "set-value":
			if len(args) >= 2 {
				return recv0 + ".setValue(" + argStrs[1] + ")", true
			}
		case "on-change":
			if len(args) >= 2 {
				return recv0 + ".onDidChangeModelContent(" + argStrs[1] + ")", true
			}
		case "set-language":
			if len(args) >= 2 {
				return "monaco.editor.setModelLanguage(" + recv0 + ".getModel(), " + argStrs[1] + ")", true
			}
		case "create-model":
			if len(args) >= 2 {
				return "monaco.editor.createModel(" + argStrs[0] + ", " + argStrs[1] + ")", true
			}
		case "set-model":
			if len(args) >= 2 {
				return recv0 + ".setModel(" + argStrs[1] + ")", true
			}
		case "dispose":
			if len(args) >= 1 {
				return recv0 + ".dispose()", true
			}
		case "define-theme":
			if len(args) >= 2 {
				return "monaco.editor.defineTheme(" + argStrs[0] + ", " + argStrs[1] + ")", true
			}
		case "set-theme":
			if len(args) >= 1 {
				return "monaco.editor.setTheme(" + argStrs[0] + ")", true
			}
		case "get-model":
			if len(args) >= 1 {
				return recv0 + ".getModel()", true
			}
		case "set-readonly":
			if len(args) >= 2 {
				return recv0 + ".updateOptions({ readOnly: " + argStrs[1] + " })", true
			}
		case "layout":
			if len(args) >= 1 {
				return recv0 + ".layout()", true
			}
		}
		return "", false
	}

	return "", false
}
