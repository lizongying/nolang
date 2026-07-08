package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// printf: formatted print
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "printf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{},
		Doc:          "Print formatted output (variadic, format string + args)",
		ForwardFunc:  "printf",
	})

	// sprintf: formatted string (returns the formatted string)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "sprintf",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format and return a string (variadic, format string + args)",
		ForwardFunc:  "sprintf",
	})

	// print: print with newline
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "print",
		Params:       []parser.Type{},
		Return:       []parser.Type{},
		Doc:          "Print arguments followed by a newline (variadic)",
		ForwardFunc:  "println",
	})
}
