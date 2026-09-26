package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// print: print to stdout, followed by a newline.
	//
	// Variadic: print(a, b, c) writes its arguments separated by single spaces
	// and appends ONE trailing newline. Each string-literal argument is its own
	// {name:spec} template, resolved from the scope at the call site — a literal
	// is NOT a C-style format string that describes/consumes the other
	// arguments (it is not printf('%d', 42)-style substitution). Non-literal
	// arguments are ordinary variadic values. Intercepted in
	// src/mir/hir2mir.go (lowerNamedFormat / lowerNamedFormatMulti); the Params
	// below only describe the declared signature arity, not the accepted count.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "print",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print string to stdout followed by a newline. Supports {name:spec} named format fields.",
		ForwardFunc:  "println",
	})

	// eprint: the stderr twin of print. Same variadic rules: arguments separated
	// by single spaces, one trailing newline, and each string-literal argument is
	// its own {name:spec} template resolved at the call site.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "eprint",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print string to stderr followed by a newline. Supports {name:spec} named format fields.",
		ForwardFunc:  "eprint",
	})

	// format: format string with {name:spec} named fields and return the result.
	// Replaces the deprecated sprintf. Accepts a SINGLE format string only —
	// there is no multi-argument form.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "format",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format string with {name:spec} named fields and return the result. Replaces sprintf.",
		ForwardFunc:  "format",
	})

	// ─── Deprecated (kept for backward compatibility; prefer print/eprint/format + io.out) ───

	// printf: deprecated. Use print (auto-newline) or io.out (no newline) instead.
	// Single format string only (no multi-argument form).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "printf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Deprecated: use print (auto-newline) or io.out (no newline). Formatted print to stdout without newline.",
		ForwardFunc:  "printf",
	})

	// sprintf: deprecated. Use format instead. Single format string only.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Deprecated: use format. Format string with {name:spec} named fields and return the result.",
		ForwardFunc:  "sprintf",
	})

	// eprintf: deprecated. Use eprint (auto-newline) or io.err (no newline) instead.
	// Single format string only (no multi-argument form).
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "eprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Deprecated: use eprint (auto-newline) or io.err (no newline). Formatted print to stderr without newline.",
		ForwardFunc:  "eprintf",
	})
}

