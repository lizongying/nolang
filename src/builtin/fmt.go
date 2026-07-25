package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// printf: formatted print (no newline) via io.out
	// Accepts a single str argument which may contain {name:spec} replacement fields.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "printf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print formatted string to stdout without newline. Supports {name:spec} named format fields.",
		ForwardFunc:  "printf",
	})

	// sprintf: format string and return it
	// Accepts a single str argument which may contain {name:spec} replacement fields.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format string with {name:spec} named fields and return the result.",
		ForwardFunc:  "sprintf",
	})

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

	// eprintf: formatted print to stderr (no newline) via io.err
	// Accepts a single str argument which may contain {name:spec} replacement fields.
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "eprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print formatted string to stderr without newline. Supports {name:spec} named format fields.",
		ForwardFunc:  "eprintf",
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
}
