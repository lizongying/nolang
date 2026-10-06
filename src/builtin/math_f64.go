package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// f64.sqrt: square root (f64 方法)
	// LLVMIntrinsic 直接映射 llvm.sqrt.f64； qualified 名与 .no 声明字面一致，
	// 旧全局函数形式 `sqrt(x)`/`math.sqrt(x)` 已打断（2026-10-05）。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.sqrt",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the square root of a float",
		LLVMIntrinsic: "llvm.sqrt.f64",
	})

	// f64.sin: sine (radians) via libm
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.sin",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the sine of an angle in radians",
		LLVMIntrinsic: "llvm.sin.f64",
	})

	// f64.cos: cosine (radians) via libm
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.cos",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the cosine of an angle in radians",
		LLVMIntrinsic: "llvm.cos.f64",
	})

	// f64.tan: tangent (radians) via libm
	// （MIR 无法调度接收者为内建标量类型的用户方法，故 tan 也必须为內建；
	// 旧实现 sin(x)/cos(x) 的函数形式已改为直接调用 libm tan。）
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.tan",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the tangent of an angle in radians",
		LLVMIntrinsic: "llvm.tan.f64",
	})

	// f64.asin: arc sine via libm
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.asin",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc sine of a float",
		LLVMIntrinsic: "llvm.asin.f64",
	})

	// f64.acos: arc cosine via libm
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.acos",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc cosine of a float",
		LLVMIntrinsic: "llvm.acos.f64",
	})

	// f64.atan: arc tangent via libm
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.atan",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc tangent of a float",
		LLVMIntrinsic: "llvm.atan.f64",
	})

	// pow: power (f64^f64)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "pow",
		Params:        []parser.Type{parser.TypeF64, parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute x raised to the power of y",
		LLVMIntrinsic: "llvm.pow.f64",
	})

	// atan2: arc tangent of y/x via LLVM intrinsic
	// （仍为函数形式：双参数，无法套用单值 f64 接收者的方法形式）
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "atan2",
		Params:        []parser.Type{parser.TypeF64, parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc tangent of y/x",
		LLVMIntrinsic: "llvm.atan2.f64",
	})

	// ── f64 方法形式內建（std/math.no 以 #{buildin} f64.NAME 声明）──
	// MethodName 使用限定名（"f64.sin"），仅接受 x.sin() 方法调用；
	// 旧的函数形式 sin(x) / math.sin(x) 不再通过 name-only 回退解析。
	// f64.sin/cos/asin/acos/atan 注册在本文件上方。

	// f64.sinh: hyperbolic sine via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.sinh",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the hyperbolic sine of a float",
		LLVMIntrinsic: "llvm.sinh.f64",
	})

	// f64.cosh: hyperbolic cosine via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.cosh",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the hyperbolic cosine of a float",
		LLVMIntrinsic: "llvm.cosh.f64",
	})

	// f64.tanh: hyperbolic tangent via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.tanh",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the hyperbolic tangent of a float",
		LLVMIntrinsic: "llvm.tanh.f64",
	})

	// f64.ceil: round up via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.ceil",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Round a float up to the nearest integer",
		LLVMIntrinsic: "llvm.ceil.f64",
	})

	// f64.floor: round down via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.floor",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Round a float down to the nearest integer",
		LLVMIntrinsic: "llvm.floor.f64",
	})

	// f64.round: round to nearest integer via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.round",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Round a float to the nearest integer",
		LLVMIntrinsic: "llvm.round.f64",
	})

	// f64.trunc: truncate toward zero via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.trunc",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Truncate a float toward zero",
		LLVMIntrinsic: "llvm.trunc.f64",
	})

	// f64.exp: e^x via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.exp",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute e raised to the power of a float",
		LLVMIntrinsic: "llvm.exp.f64",
	})

	// f64.log: natural logarithm via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.log",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the natural logarithm of a float",
		LLVMIntrinsic: "llvm.log.f64",
	})

	// f64.log10: base-10 logarithm via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.log10",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the base-10 logarithm of a float",
		LLVMIntrinsic: "llvm.log10.f64",
	})

	// f64.log2: base-2 logarithm via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.log2",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the base-2 logarithm of a float",
		LLVMIntrinsic: "llvm.log2.f64",
	})

	// degrees / radians 已改方法形式（f64.degrees / f64.radians，2026-10-05）：
	// 纯 .no 实现（见 src/std/math.no，同 f64.tan / f64.to-str 先例），无需
	// 注册表条目；旧的裸名 ReceiverGlobal 注册（ForwardFunc math-degrees，
	// 且 math-radians 根本没有 MIR 分支）随之移除——函数形式已不存在。
}
