package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// max: return the maximum of two integers
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "max",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the maximum value of the two integers",
		ForwardFunc:  "math-max",
	})

	// min: return the minimum of two integers
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "min",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the minimum value of the two integers",
		ForwardFunc:  "math-min",
	})

	// abs: return the absolute value
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "abs",
		Params:       []parser.Type{parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Return the absolute value of the integer",
		ForwardFunc:  "math-abs",
	})

	// clamp: clamp a value between min and max
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType: ReceiverGlobal,
		MethodName:   "clamp",
		Params:       []parser.Type{parser.TypeI64, parser.TypeI64, parser.TypeI64},
		Return:       []parser.Type{parser.TypeI64},
		Doc:          "Clamp value between min and max",
		ForwardFunc:  "math-clamp",
	})
}
