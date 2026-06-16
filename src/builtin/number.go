package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// is-nan: check if float is NaN
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "is-nan",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeI64},
		Doc:           "Check if a float is NaN (not a number)",
		LLVMIntrinsic: "llvm.isnan.f64",
	})

	// is-inf: check if float is infinite
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverGlobal,
		MethodName:    "is-inf",
		Params:        []parser.Type{parser.TypeF64},
		Return:        []parser.Type{parser.TypeI64},
		Doc:           "Check if a float is infinite",
		LLVMIntrinsic: "llvm.isinf.f64",
	})
}
