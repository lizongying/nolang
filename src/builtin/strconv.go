package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// str-to-i8: string to i8
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i8",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI8},
		Doc:          "Parse a string to i8",
		ForwardFunc:  "atoi_i8",
	})

	// str-to-i16: string to i16
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i16",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI16},
		Doc:          "Parse a string to i16",
		ForwardFunc:  "atoi_i16",
	})

	// str-to-i32: string to i32
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i32",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI32},
		Doc:          "Parse a string to i32",
		ForwardFunc:  "atoi_i32",
	})

	// str-to-i64: string to i64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-i64",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Parse a string to i64",
		ForwardFunc:  "atoi_i64",
	})

	// str-to-u8: string to u8
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u8",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU8},
		Doc:          "Parse a string to u8",
		ForwardFunc:  "strtoul_u8",
	})

	// str-to-u16: string to u16
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u16",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU16},
		Doc:          "Parse a string to u16",
		ForwardFunc:  "strtoul_u16",
	})

	// str-to-u32: string to u32
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u32",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU32},
		Doc:          "Parse a string to u32",
		ForwardFunc:  "strtoul_u32",
	})

	// str-to-u64: string to u64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-u64",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Parse a string to u64",
		ForwardFunc:  "strtoul_u64",
	})

	// str-to-f32: string to f32
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-f32",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeF32},
		Doc:          "Parse a string to f32",
		ForwardFunc:  "strtod_f32",
	})

	// str-to-f64: string to f64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-f64",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Parse a string to f64",
		ForwardFunc:  "strtod_f64",
	})

	// str-to-bool: string to bool
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-bool",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeBool},
		Doc:          "Parse a string to bool (\"true\" or \"false\")",
		ForwardFunc:  "str_to_bool",
	})

	// str-to-byte: string to byte
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "str-to-byte",
		Params:       []parser.Type{parser.TypeStr},
		Return:       []parser.Type{parser.TypeByte},
		Doc:          "Parse a string to byte",
		ForwardFunc:  "atoi_byte",
	})

	// i8-to-str: i8 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i8-to-str",
		Params:       []parser.Type{parser.TypeI8},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i8 as a string",
		ForwardFunc:  "sprintf_i8",
	})

	// i16-to-str: i16 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i16-to-str",
		Params:       []parser.Type{parser.TypeI16},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i16 as a string",
		ForwardFunc:  "sprintf_i16",
	})

	// i32-to-str: i32 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i32-to-str",
		Params:       []parser.Type{parser.TypeI32},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i32 as a string",
		ForwardFunc:  "sprintf_i32",
	})

	// i64-to-str: i64 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i64-to-str",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i64 as a string",
		ForwardFunc:  "sprintf_i64",
	})

	// u8-to-str: u8 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u8-to-str",
		Params:       []parser.Type{parser.TypeU8},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u8 as a string",
		ForwardFunc:  "sprintf_u8",
	})

	// u16-to-str: u16 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u16-to-str",
		Params:       []parser.Type{parser.TypeU16},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u16 as a string",
		ForwardFunc:  "sprintf_u16",
	})

	// u32-to-str: u32 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u32-to-str",
		Params:       []parser.Type{parser.TypeU32},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u32 as a string",
		ForwardFunc:  "sprintf_u32",
	})

	// u64-to-str: u64 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u64-to-str",
		Params:       []parser.Type{parser.TypeU64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a u64 as a string",
		ForwardFunc:  "sprintf_u64",
	})

	// f32-to-str: f32 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f32-to-str",
		Params:       []parser.Type{parser.TypeF32},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f32 as a string",
		ForwardFunc:  "sprintf_f32",
	})

	// f64-to-str: f64 to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-str",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a f64 as a string",
		ForwardFunc:  "sprintf_f64",
	})

	// bool-to-str: bool to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "bool-to-str",
		Params:       []parser.Type{parser.TypeBool},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a bool as \"true\" or \"false\"",
		ForwardFunc:  "bool_to_str",
	})

	// byte-to-str: byte to string
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "byte-to-str",
		Params:       []parser.Type{parser.TypeByte},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format a byte as a string",
		ForwardFunc:  "sprintf_byte",
	})
}
