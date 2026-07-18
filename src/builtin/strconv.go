package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// str.to-i64 / str.to-u64 are implemented in Nolang (src/std/str.no) and
	// return ?i64 / ?u64 Option types. The old CLibCall (atoi / strtoull)
	// builtins have been removed in favour of the Nolang implementation.

	// str.to-f64 / str.to-f32 are implemented in Nolang (src/std/str.no)
	// using the str-to-f64 function. The old CLibCall (strtod) has been
	// removed in favour of the Nolang implementation.

	// str.to-bool: string to bool (method: s.to-bool())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.to-bool",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Parse a string to bool (\"true\" or \"false\") (method)",
		ForwardFunc:  "str-to-bool",
	})

	// Integer to-str methods (i8/i16/i32/i64/u8/u16/u32/u64/byte.to-str) and
	// the char-to-str global are now implemented in Nolang (src/std/number.no),
	// replacing the previous sprintf CLibCall builtins. f64.to-str is also
	// implemented in Nolang (f64-to-str function in number.no). Only f32.to-str
	// remains as a builtin (f32→f64 conversion not yet available in Nolang).

	// f32.to-str: f32 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF32,
		MethodName:   "f32.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f32 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%g", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMF64}, RetType: LLVMI32},
	})

	// bool.to-str: bool to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverBool,
		MethodName:   "bool.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a bool as \"true\" or \"false\" (method)",
		ForwardFunc:  "bool-to-str",
	})

	// i64-to-f64: integer to float conversion
	convI64ToFP := LLVMConvI64ToFP
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i64-to-f64",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert i64 to f64",
		LLVMConv:     &convI64ToFP,
	})

	// f64-to-i64: float to integer conversion
	convFPToI64 := LLVMConvFPToI64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-i64",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Convert f64 to i64",
		LLVMConv:     &convFPToI64,
	})

	// f64-to-f32: double to float conversion (fptrunc)
	convF64ToF32 := LLVMConvF64ToF32
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-f32",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeF32},
		Doc:          "Convert f64 to f32",
		LLVMConv:     &convF64ToF32,
	})
}
