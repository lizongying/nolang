package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	i64Type := LLVMI64

	// str-to-i64: string to i64 via atoi
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i64",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Parse a string to i64",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &i64Type},
	})

	// str-to-f64: string to f64 via strtod
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-f64",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Parse a string to f64",
		CLibCall:     &CLibCall{FuncName: "strtod", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr}, RetType: LLVMF64, FixedArgs: map[int]string{1: "null"}},
	})

	// str-to-f32: string to f32 via strtod
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-f32",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeF32},
		Doc:          "Parse a string to f32",
		CLibCall:     &CLibCall{FuncName: "strtod", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr}, RetType: LLVMF64, FixedArgs: map[int]string{1: "null"}},
	})

	// str-to-u64: string to u64 via strtoull
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u64",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Parse a string to u64",
		CLibCall:     &CLibCall{FuncName: "strtoull", ArgTypes: []LLVMArgType{LLVMStrPtr, LLVMI8Ptr, LLVMI32}, RetType: LLVMI64, FixedArgs: map[int]string{1: "null", 2: "10"}},
	})

	// str-to-i8: string to i8 (atoi + trunc + zext)
	truncI8 := LLVMI64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i8",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI8},
		Doc:          "Parse a string to i8",
		ForwardFunc:  "str-to-i8",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-i16: string to i16 (atoi + trunc + zext)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i16",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI16},
		Doc:          "Parse a string to i16",
		ForwardFunc:  "str-to-i16",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-i32: string to i32 (atoi)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i32",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI32},
		Doc:          "Parse a string to i32",
		ForwardFunc:  "str-to-i32",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-u8: string to u8
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u8",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU8},
		Doc:          "Parse a string to u8",
		ForwardFunc:  "str-to-u8",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-u16: string to u16
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u16",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU16},
		Doc:          "Parse a string to u16",
		ForwardFunc:  "str-to-u16",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-u32: string to u32
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u32",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU32},
		Doc:          "Parse a string to u32",
		ForwardFunc:  "str-to-u32",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-bool: string to bool (strcmp + cmp + zext)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-bool",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Parse a string to bool (\"true\" or \"false\")",
		ForwardFunc:  "str-to-bool",
	})

	// str-to-byte: string to byte
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-byte",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeByte},
		Doc:          "Parse a string to byte",
		ForwardFunc:  "str-to-byte",
		CLibCall:     &CLibCall{FuncName: "atoi", ArgTypes: []LLVMArgType{LLVMStrPtr}, RetType: LLVMI32, RetExt: &truncI8},
	})

	// str-to-char: string to char (load i8 + zext)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-char",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Get the first character of a string as i64",
		ForwardFunc:  "str-to-char",
	})

	// i8-to-str: i8 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i8-to-str",
		Params:       []parser.Type{parser.TypeI8},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i8 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hhd", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// i16-to-str: i16 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i16-to-str",
		Params:       []parser.Type{parser.TypeI16},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i16 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hd", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// i32-to-str: i32 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i32-to-str",
		Params:       []parser.Type{parser.TypeI32},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i32 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%d", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// i64-to-str: i64 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i64-to-str",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i64 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%lld", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u8-to-str: u8 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u8-to-str",
		Params:       []parser.Type{parser.TypeU8},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u8 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hhu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u16-to-str: u16 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u16-to-str",
		Params:       []parser.Type{parser.TypeU16},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u16 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u32-to-str: u32 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u32-to-str",
		Params:       []parser.Type{parser.TypeU32},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u32 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%u", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// u64-to-str: u64 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u64-to-str",
		Params:       []parser.Type{parser.TypeU64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u64 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%llu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// f32-to-str: f32 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f32-to-str",
		Params:       []parser.Type{parser.TypeF32},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f32 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%g", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMF64}, RetType: LLVMI32},
	})

	// f64-to-str: f64 to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-str",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f64 as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%g", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMF64}, RetType: LLVMI32},
	})

	// bool-to-str: bool to string (select)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "bool-to-str",
		Params:       []parser.Type{parser.TypeBool},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a bool as \"true\" or \"false\"",
		ForwardFunc:  "bool-to-str",
	})

	// byte-to-str: byte to string via sprintf
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "byte-to-str",
		Params:       []parser.Type{parser.TypeByte},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a byte as a string",
		CLibCall:     &CLibCall{FuncName: "sprintf", SprintfFmt: "%hhu", BufGlobal: "@.strconv_buf", ArgTypes: []LLVMArgType{LLVMI64}, RetType: LLVMI32},
	})

	// char-to-str: char to string via sprintf
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
