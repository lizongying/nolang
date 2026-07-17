package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// abs: absolute value (f64)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "abs",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Return the absolute value of a float (fabs)",
		LLVMIntrinsic: "llvm.fabs.f64",
	})

	// max: maximum of two floats
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "max",
		Params:        []parser.Type{parser.TypeF64, parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Return the maximum of two floats",
		LLVMIntrinsic: "llvm.maxnum.f64",
	})

	// min: minimum of two floats
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "min",
		Params:        []parser.Type{parser.TypeF64, parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Return the minimum of two floats",
		LLVMIntrinsic: "llvm.minnum.f64",
	})

	// sqrt: square root (f64)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "sqrt",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the square root of a float",
		LLVMIntrinsic: "llvm.sqrt.f64",
	})

	// sin: sine (radians)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "sin",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the sine of an angle in radians",
		LLVMIntrinsic: "llvm.sin.f64",
	})

	// cos: cosine (radians)
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "cos",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the cosine of an angle in radians",
		LLVMIntrinsic: "llvm.cos.f64",
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

	// asin: arc sine via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "asin",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc sine of a float",
		LLVMIntrinsic: "llvm.asin.f64",
	})

	// acos: arc cosine via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "acos",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc cosine of a float",
		LLVMIntrinsic: "llvm.acos.f64",
	})

	// atan: arc tangent via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "atan",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc tangent of a float",
		LLVMIntrinsic: "llvm.atan.f64",
	})

	// atan2: arc tangent of y/x via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "atan2",
		Params:        []parser.Type{parser.TypeF64, parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the arc tangent of y/x",
		LLVMIntrinsic: "llvm.atan2.f64",
	})

	// sinh: hyperbolic sine via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "sinh",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the hyperbolic sine of a float",
		LLVMIntrinsic: "llvm.sinh.f64",
	})

	// cosh: hyperbolic cosine via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "cosh",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the hyperbolic cosine of a float",
		LLVMIntrinsic: "llvm.cosh.f64",
	})

	// tanh: hyperbolic tangent via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "tanh",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the hyperbolic tangent of a float",
		LLVMIntrinsic: "llvm.tanh.f64",
	})

	// ceil: round up via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "ceil",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Round a float up to the nearest integer",
		LLVMIntrinsic: "llvm.ceil.f64",
	})

	// floor: round down via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "floor",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Round a float down to the nearest integer",
		LLVMIntrinsic: "llvm.floor.f64",
	})

	// round: round to nearest integer via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "round",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Round a float to the nearest integer",
		LLVMIntrinsic: "llvm.round.f64",
	})

	// trunc: truncate toward zero via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "trunc",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Truncate a float toward zero",
		LLVMIntrinsic: "llvm.trunc.f64",
	})

	// exp: e^x via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "exp",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute e raised to the power of a float",
		LLVMIntrinsic: "llvm.exp.f64",
	})

	// log: natural logarithm via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "log",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the natural logarithm of a float",
		LLVMIntrinsic: "llvm.log.f64",
	})

	// log10: base-10 logarithm via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "log10",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the base-10 logarithm of a float",
		LLVMIntrinsic: "llvm.log10.f64",
	})

	// log2: base-2 logarithm via LLVM intrinsic
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "log2",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the base-2 logarithm of a float",
		LLVMIntrinsic: "llvm.log2.f64",
	})

	// degrees: convert radians to degrees
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "degrees",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert radians to degrees (r * 180 / Pi)",
		ForwardFunc:  "math-degrees",
	})

	// radians: convert degrees to radians
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "radians",
		Params:       []parser.Type{parser.TypeF64},
		Return:       []parser.Type{parser.TypeF64},
		Doc:          "Convert degrees to radians (d * Pi / 180)",
		ForwardFunc:  "math-radians",
	})
}
