package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// []<type>.zero — 使用 llvm.memset 將陣列/切片所有元素置零
	// 註冊所有數值型別的變體，方法解析會依 receiver 元素型別匹配。
	elemTypes := []string{
		"i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f32", "f64",
		"byte",
	}
	for _, et := range elemTypes {
		BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
			ReceiverType: ReceiverVec,
			MethodName:   "[]" + et + ".zero",
			Params:       []parser.Type{},
			Return:       []parser.Type{},
			Doc:          "Zero all elements using llvm.memset",
			ForwardFunc:  "arr-zero",
		})
	}
}
