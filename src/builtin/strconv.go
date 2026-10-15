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

	// Integer to-str methods: i64.to-str 走原生 @str_from_i64（本檔下方
	// ForwardFunc "i64-to-str" 註冊，由 codegen 按名 interception 命中原生內聯）。
	// u64/i32/i16/i8/u32/u16/u8/byte.to-str 與 f64/f32.to-str 皆在 std/number.no
	// 以 Nolang 直接實現（窄整型拓寬複用 i64/u64；u64 走十進位迴圈、f64 走 %g
	// 例程、f32 拓寬複用 f64）——後端僅有 i64 原生格式化器，故其餘不設 #{buildin}。
	// char.to-str 見 std/char.no。

	// i64.to-str: i64 轉字串（方法，底層走原生 @str_from_i64）
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI64,
		MethodName:   "i64.to-str",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeStr},
		Doc:          "Format an i64 as a decimal string (method)",
		ForwardFunc:  "i64-to-str",
	})

	// f32-to-f64: f32 to f64 conversion (fpext)
	convF32ToF64 := LLVMConvF32ToF64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF32,
		MethodName:   "f32.to-f64",
		Params:       []parser.Type{},
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
		ReceiverType: ReceiverI64,
		MethodName:   "i64.to-f64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert i64 to f64",
		LLVMConv:     &convI64ToFP,
	})

	// f64-to-i64: float to integer conversion
	convFPToI64 := LLVMConvFPToI64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-i64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Convert f64 to i64",
		LLVMConv:     &convFPToI64,
	})

	// f64-to-f32: double to float conversion (fptrunc)
	convF64ToF32 := LLVMConvF64ToF32
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-f32",
		Params:       []parser.Type{},
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
		ReceiverType: ReceiverU64,
		MethodName:   "u64.to-f64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert u64 to f64",
		LLVMConv:     &convUIToFP,
	})

	// i128-to-f64: 128 位有號整數轉浮點（sitofp）
	convI128ToFP := LLVMConvI64ToFP
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI128,
		MethodName:   "i128.to-f64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert i128 to f64",
		LLVMConv:     &convI128ToFP,
	})

	// u128-to-f64: 128 位無號整數轉浮點（uitofp）
	convU128ToFP := LLVMConvUIToFP
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverU128,
		MethodName:   "u128.to-f64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert u128 to f64",
		LLVMConv:     &convU128ToFP,
	})

	// f64-to-u64: 浮點轉 64 位無號整數（llvm.fptoui.sat，饱和）
	convFPToUI64 := LLVMConvFPToUI
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-u64",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeU64},
		Doc:          "Convert f64 to u64 (saturating)",
		LLVMConv:     &convFPToUI64,
	})

	// f64-to-u32: 浮點轉 32 位無號整數（llvm.fptoui.sat）
	convFPToUI32 := LLVMConvFPToUI
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-u32",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeU32},
		Doc:          "Convert f64 to u32 (saturating)",
		LLVMConv:     &convFPToUI32,
	})

	// f64-to-i32: 浮點轉 32 位有號整數（llvm.fptosi.sat）
	convFPToI32 := LLVMConvFPToI64
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverF64,
		MethodName:   "f64.to-i32",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI32},
		Doc:          "Convert f64 to i32 (saturating)",
		LLVMConv:     &convFPToI32,
	})

	// i64-to-i32: 整數位寬收窄（trunc，低比特截斷）
	convI64ToI32 := LLVMConvTrunc
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverI64,
		MethodName:   "i64.to-i32",
		Params:       []parser.Type{},
		Return:       []parser.Type{parser.TypeI32},
		Doc:          "Truncate i64 to i32",
		LLVMConv:     &convI64ToI32,
	})
}
