package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	i64Type := LLVMI64

	// str.to-i64: string to i64 (method: s.to-i64())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.to-i64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Parse a string to i64 (method)",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// str.parse-f64-raw: internal string to f64 via strtod (used by str.to-f64 wrapper)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.parse-f64-raw",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Parse a string to f64 (internal, used by str.to-f64)",
		CLibCall:     &CLibCall{FuncName: "strtod", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr}, RetType: LLVMF64, FixedArgs: map[int]string{1: "null"}},
	})

	// str.to-f32: string to f32 (method: s.to-f32())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.to-f32",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF32},
		Doc:          "Parse a string to f32 (method)",
		CLibCall:     &CLibCall{FuncName: "strtod", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr}, RetType: LLVMF64, FixedArgs: map[int]string{1: "null"}},
	})

	// str.to-u64: string to u64 (method: s.to-u64())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.to-u64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Parse a string to u64 (method)",
		CLibCall:     &CLibCall{FuncName: "strtoull", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr, LLVMI32}, RetType: LLVMI64, FixedArgs: map[int]string{1: "null", 2: "10"}},
	})

	// str.to-bool: string to bool (method: s.to-bool())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverStr,
		MethodName:   "str.to-bool",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Parse a string to bool (\"true\" or \"false\") (method)",
		ForwardFunc:  "str-to-bool",
	})

	// i8.to-str: i8 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI8,
		MethodName:   "i8.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i8 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hhd", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// i16.to-str: i16 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI16,
		MethodName:   "i16.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i16 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hd", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// i32.to-str: i32 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI32,
		MethodName:   "i32.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i32 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%d", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// i64.to-str: i64 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI64,
		MethodName:   "i64.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i64 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%lld", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u8.to-str: u8 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverU8,
		MethodName:   "u8.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u8 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hhu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u16.to-str: u16 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverU16,
		MethodName:   "u16.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u16 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u32.to-str: u32 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverU32,
		MethodName:   "u32.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u32 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%u", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u64.to-str: u64 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverU64,
		MethodName:   "u64.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u64 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%llu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// f32.to-str: f32 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF32,
		MethodName:   "f32.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f32 as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%g", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMF64}, RetType: LLVMI32},
	})

	// f64.to-str: f64 to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f64 as a string (method)",
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

	// byte.to-str: byte to string (method: v.to-str())
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverByte,
		MethodName:   "byte.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a byte as a string (method)",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hhu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// char-to-str: char to string via sprintf (keep as global since char is i32 internally)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "char-to-str",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a char (as i64) as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%c", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
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
}
