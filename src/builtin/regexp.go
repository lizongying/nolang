package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// regexp-match: 判断文本是否匹配正则表达式
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "regexp-match",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Check if text matches the regular expression pattern",
		ForwardFunc:  "regexp-match",
	})

	// regexp-find: 查找第一个匹配
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "regexp-find",
		Params:       []parser.Type{parser.TypeStr, parser.TypeStr},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Find the first match of pattern in text, returns the matched string",
		ForwardFunc:  "regexp-find",
	})
}
