package build

import "github.com/lizongying/nolang/parser"

// compiler_switches.go — package.noml 的 compiler 区块 → parser 解析期开关。
//
// 这是**唯一**映射点。build（astParseConfig）与 fmt / LSP（FmtSigLoader）都从
// 这里取开关，因此新增一个影响解析的 compiler.* 选项时，只需改
// package.CompilerOptions 与本函数各一处，两条管道同时获得。
//
// 反面教材：compiler.anonymous-fn-type 当初直接抄在 Transpiler 与
// FmtSigLoader 各自的构造里，于是 fmt 漏了它 —— 同一个 package 的同一份源码
// 在两条管道里解析出不同的 AST。
func compilerSwitches(p *Package) parser.CompilerSwitches {
	if p == nil {
		return parser.CompilerSwitches{}
	}
	return parser.CompilerSwitches{
		AnonymousFnType: p.Compiler.AnonymousFnType,
	}
}
