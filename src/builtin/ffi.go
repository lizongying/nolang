package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// ffi-cstr-at: 讀取 C 字串陣列元素 ((char**)arr)[idx] 為 Nolang str
	// 用於 MySQL mysql_fetch_row 等回傳 char** 的 C API
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ffi-cstr-at",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Read C string array element ((char**)ptr)[idx] as Nolang str",
		ForwardFunc:  "ffi-cstr-at",
	})

	// ffi-cstr-at-int: 讀取 C 字串陣列元素並解析為 i64（via strtoll）
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ffi-cstr-at-int",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Read C string array element ((char**)ptr)[idx] and parse as i64",
		ForwardFunc:  "ffi-cstr-at-int",
	})

	// ffi-cstr-at-float: 讀取 C 字串陣列元素並解析為 f64（via strtod）
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "ffi-cstr-at-float",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Read C string array element ((char**)ptr)[idx] and parse as f64",
		ForwardFunc:  "ffi-cstr-at-float",
	})
}
