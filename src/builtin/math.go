package builtin

import "github.com/lizongying/nolang/parser"

func init() {
	// max / min 已移除（2026-10-05）：`number.max` / `number.min` 是變參泛型
	// （`a ..num`），i64 與 f64 都覆蓋，而且 f64 走同一條單態化路徑，不會像
	// 舊的兩條同名內建註冊那樣被 FindBuiltinMethod「取第一條」截斷成整數。
	// 舊註冊（ForwardFunc math-max / math-min）留著只會多出一個沉默錯誤的入口。

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
