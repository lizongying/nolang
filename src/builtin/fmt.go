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
