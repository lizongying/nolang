package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "regexp-match",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Check if text matches the regular expression pattern",
		ForwardFunc:  "regexp-match",
	})

	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "regexp-find",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Find the first match of pattern in text",
		ForwardFunc:  "regexp-find",
	})
}
