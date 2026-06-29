package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// str.eq: compare two strings for equality (method: a.eq(b, n))
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.eq",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Compare two strings for equality (method)",
		ForwardFunc:  "eq-raw",
	})

	// str.len: get string length
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "len",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the length of the string",
		ForwardFunc:  "str-len",
	})

	// str.copy: copy n bytes from src to dst (method: src.copy(dst, n))
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.copy",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Copy n bytes from src to dst (method)",
		ForwardFunc:  "memcpy",
	})

	// str-fill: fill first n bytes of s with val
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-fill",
		Params:       []parser.Type{parser.TypeStr, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{},
		Doc:          "Fill the first n bytes of a string with val",
		ForwardFunc:  "memset",
	})
}
