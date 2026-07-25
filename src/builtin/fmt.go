package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// print: print string to stdout with newline via io.outln
	// Accepts a single str argument which may contain {name:spec} replacement fields.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "print",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print string to stdout followed by a newline. Supports {name:spec} named format fields.",
		ForwardFunc:  "println",
	})

	// eprint: print string to stderr with newline via io.errln
	// Accepts a single str argument which may contain {name:spec} replacement fields.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "eprint",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print string to stderr followed by a newline. Supports {name:spec} named format fields.",
		ForwardFunc:  "eprint",
	})

	// format: format string with {name:spec} named fields and return the result.
	// Replaces the deprecated sprintf. Accepts a single str argument.
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
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "printf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Deprecated: use print (auto-newline) or io.out (no newline). Formatted print to stdout without newline.",
		ForwardFunc:  "printf",
	})

	// sprintf: deprecated. Use format instead.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Deprecated: use format. Format string with {name:spec} named fields and return the result.",
		ForwardFunc:  "sprintf",
	})

	// eprintf: deprecated. Use eprint (auto-newline) or io.err (no newline) instead.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "eprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Deprecated: use eprint (auto-newline) or io.err (no newline). Formatted print to stderr without newline.",
		ForwardFunc:  "eprintf",
	})
}

