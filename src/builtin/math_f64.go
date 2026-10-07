package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// f64.sqrt: square root (f64 方法)
	// LLVMIntrinsic 直接映射 llvm.sqrt.f64（硬件指令，不依赖 libm）；
	// qualified 名与 .no 声明字面一致，旧全局函数形式 `sqrt(x)`/`math.sqrt(x)`
	// 已打断（2026-10-05）。
	// 其余 f64 数学方法（sin/cos/tan/asin/acos/atan/sinh/cosh/tanh/
	// ceil/floor/round/trunc/exp/log/log10/log2）以及全局函数 pow/atan2
	// 现全部以纯 .no 实现（src/std/number.no、src/std/math.no），不再走
	// libm 内建——避免对 C 数学库的链接依赖（见 src/std 注释与 CI 链接约束）。
	BuiltinMethodList = append(BuiltinMethodList, BuiltinMethod{
		ReceiverType:  ReceiverF64,
		MethodName:    "f64.sqrt",
		Params:        []parser.Type{},
		Return:        []parser.Type{parser.TypeF64},
		Doc:           "Compute the square root of a float",
		LLVMIntrinsic: "llvm.sqrt.f64",
	})
}
