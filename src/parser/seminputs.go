package parser

// seminputs.go — SemRebuildConfig 的唯一构造入口。
//
// 为什么要有这一层：SemRebuildConfig 有三个生产者 ——
//
//   - build 的 astParseConfig（no build / no test / no vet）；
//   - build 的 FmtSigLoader.ConfigFor（no fmt 与 LSP format-on-save）；
//   - checker 的 stdSemConfig（Tier-0 std blob 的烤制与重建）。
//
// 三者各自按字段拼 cfg 时，「parser 多了一个输入」这件事没有任何机制会逼另外
// 两处跟上。这不是理论风险：compiler.anonymous-fn-type 就只进了 build 那一个，
// fmt / LSP 拿 false 去解析同一份源码，于是同一个 package 的同一份源码在两条
// 管道里得到不同的 AST（parser/type.go 直接报 zc1zfsat）。
//
// 现在生产者不再罗列 cfg 字段，只交出三个整体：文件名、package 级开关、
// 外部模块的签名表。字段到 cfg 的映射只发生在本文件。新增 parser 输入时：
//
//   - package 级开关 ⇒ 加进 CompilerSwitches，三个生产者自动获得（开关值本身
//     来自 package.noml，映射点只有 build.compilerSwitches）；
//   - 新的签名表 ⇒ 加进 SigTableSet，三个生产者同样自动获得。
//
// 加进 SemRebuildConfig 却忘了接进 NewSemRebuildConfig 会立刻被
// seminputs_test.go 抓住（反射遍历每个导出字段，要求非零）。

// CompilerSwitches 是 package.noml 的 compiler 区块中**会改变解析结果**的开关。
//
// 只收解析相关项：option-payload-inline-threshold 这类影响 MIR / codegen 的字段不在此，
// 它们不改变 AST，混进来会让「解析配置」这个概念失去边界。
type CompilerSwitches struct {
	// AnonymousFnType 对应 compiler.anonymous-fn-type，决定 `(cb () ())` 这类
	// 匿名函数类型语法能否解析（见 parser/type.go 的 zc1zfsat）。
	AnonymousFnType bool
}

// SigTableSet 是外部模块的四张解析期表，全部只读共享：调用方不得修改，
// 需要覆盖同名键时自行复制（见 build.preloadModuleSignatures 的本地覆盖层）。
//
// 四张表的来源目前并不对称，加表时请如实记录：
//   - FuncSigs / MethodSigs / StructFields：std 内嵌表 + 源码 use / # 指向的
//     本地模块（本地键覆盖同名 std 键）；
//   - EnumVariants：**只有 std**（checker.CollectStdEnumVariants）。本地模块的
//     枚举变体从未进过 cfg，三个生产者一致缺失 —— 不漂移，但模型不完整。
//     补它要同时改三处，且会改变 match 降糖的证据面，必须单独验证。
type SigTableSet struct {
	// FuncSigs / MethodSigs 是跨文件与内建的**结果类型**表（parser 只吃结果）。
	FuncSigs   map[string][]string
	MethodSigs map[string][]string
	// StructFields 是 struct 名 → 字段名 → 类型字符串。
	StructFields map[string]map[string]string
	// EnumVariants 是跨模块枚举变体表（match desugar 需要）。
	EnumVariants map[string][]string
}

// NewSemRebuildConfig 是 SemRebuildConfig 的**唯一**构造点。
//
// filename 必须是 filepath.Base 之后的文件名（空串表示「无文件名」，调用方
// 不要传 filepath.Base("") 得到的 "."）。
func NewSemRebuildConfig(filename string, sw CompilerSwitches, ext SigTableSet) SemRebuildConfig {
	return SemRebuildConfig{
		Filename:             filename,
		AllowAnonymousFnType: sw.AnonymousFnType,
		FuncSigs:             ext.FuncSigs,
		MethodSigs:           ext.MethodSigs,
		StructFields:         ext.StructFields,
		EnumVariants:         ext.EnumVariants,
	}
}
