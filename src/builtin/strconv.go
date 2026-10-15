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
	// replacing the previous sprintf CLibCall builtins. f64.to-str and f32.to-str
	// are also implemented in Nolang (f64-to-str function in number.no via
	// f32-to-f64 conversion).

	// f32-to-f64: f32 to f64 conversion (fpext)
	convF32ToF64 := LLVMConvF32ToF64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f32-to-f64",
		Params:       []parser.Type{parser.TypeF32},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert f32 to f64",
		LLVMConv:     &convF32ToF64,
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

	// ---------------------------------------------------------------------
	// 轉換矩陣補全（B6）：整數/浮點各寬度兩兩轉換，底層走 LLVMConv。
	// MIR 把 u64 摊平為 i64、u128 為 i128，符號性靠 Params/Return 判讀；
	// 浮點轉整數一律使用饱和 intrinsic（見 emitFptSat）。
	// ---------------------------------------------------------------------

	// u64-to-f64: 無號整數轉浮點（uitofp）
	convUIToFP := LLVMConvUIToFP
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u64-to-f64",
		Params:       []parser.Type{parser.TypeU64},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert u64 to f64",
		LLVMConv:     &convUIToFP,
	})

	// i128-to-f64: 128 位有號整數轉浮點（sitofp）
	convI128ToFP := LLVMConvI64ToFP
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i128-to-f64",
		Params:       []parser.Type{parser.TypeI128},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert i128 to f64",
		LLVMConv:     &convI128ToFP,
	})

	// u128-to-f64: 128 位無號整數轉浮點（uitofp）
	convU128ToFP := LLVMConvUIToFP
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "u128-to-f64",
		Params:       []parser.Type{parser.TypeU128},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert u128 to f64",
		LLVMConv:     &convU128ToFP,
	})

	// f64-to-u64: 浮點轉 64 位無號整數（llvm.fptoui.sat，饱和）
	convFPToUI64 := LLVMConvFPToUI
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-u64",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Convert f64 to u64 (saturating)",
		LLVMConv:     &convFPToUI64,
	})

	// f64-to-u32: 浮點轉 32 位無號整數（llvm.fptoui.sat）
	convFPToUI32 := LLVMConvFPToUI
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-u32",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeU32},
		Doc:          "Convert f64 to u32 (saturating)",
		LLVMConv:     &convFPToUI32,
	})

	// f64-to-i32: 浮點轉 32 位有號整數（llvm.fptosi.sat）
	convFPToI32 := LLVMConvFPToI64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "f64-to-i32",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeI32},
		Doc:          "Convert f64 to i32 (saturating)",
		LLVMConv:     &convFPToI32,
	})

	// i64-to-i32: 整數位寬收窄（trunc，低比特截斷）
	convI64ToI32 := LLVMConvTrunc
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "i64-to-i32",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI32},
		Doc:          "Truncate i64 to i32",
		LLVMConv:     &convI64ToI32,
	})
}
