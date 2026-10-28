package mir

// negative_module_const_test.go — 未标註型的**负数**模块级常量必须可被函数读取。
//
// 这不是一个「好看」的缺口，它曾经让整个 map 不可用：
//
//	FNV-OFFSET = -3750763034362895579
//	hashmap-str-tmpl.hash = (key str) (idx i64) { h = FNV-OFFSET ... }
//
// `letTypeRaw` 对 `KPrefix` 节点返回 ""（它只认字面量 / 结构体字面量 / 调用），
// 而模块常量的物化有两条互斥的路径，两条都把它跳过了：
//
//	路径 A（注册成全局）要求 letTypeRaw 非空 —— 空则 skip；
//	路径 B（内联进合成 main）要求**不可**折叠 —— 可折叠则 skip。
//
// `-7` 是可折叠的（foldConstText 有 KPrefix 分支），于是两条路径都不认领它，
// 每个读它的函数拿到一个 void 值：`EmitLLVM: move src slot v20 -> v18`。
// 在 pristine HEAD 上，lit.no / p3.no / tests/mem-safety/map-key-leak.no /
// test/std/str-map.no **全部**因此编译失败，而 `no vet` 全绿。
//
// 修法只是让 raw-type 查询学会 foldConstText 已经知道的事：前缀运算不改变
// 绑定的类型（`-7` 是 i64、`~b` 是 b、`!f` 是 bool）。

import (
	"strings"
	"testing"
)

// TestNegativeModuleConstIsReadableFromFunction 读侧：负数模块常量进得了函数。
func TestNegativeModuleConstIsReadableFromFunction(t *testing.T) {
	const src = `K = -3750763034362895579
f = () (r i64) {
    r = K
}
main = () () {
    x = f()
}`
	m := lowerSrc(t, src)
	ir, err := m.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed for a function reading an unannotated negative module constant: %v", err)
	}
	// 正向证据：常量必须真的被物化成那个 i64 值，而不是留下一个 void 槽。
	if !strings.Contains(ir, "3750763034362895579") {
		t.Errorf("emitted IR does not contain the constant value 3750763034362895579; the binding was not materialised")
	}
}

// TestPositiveModuleConstIsReadableFromFunction 对照：正数常量本来就走得通。
// 有这条，「负数也通过」才不会因为「常量根本不进 IR」而空洞通过。
func TestPositiveModuleConstIsReadableFromFunction(t *testing.T) {
	const src = `K = 7
f = () (r i64) {
    r = K
}
main = () () {
    x = f()
}`
	m := lowerSrc(t, src)
	if _, err := m.EmitLLVM(); err != nil {
		t.Fatalf("EmitLLVM failed for a positive module constant (control case): %v", err)
	}
}

// TestNegativeModuleConstInsideFunctionBody 同一条规则在函数内也成立：函数体里
// 的 `h = -7` 必须得到一个 i64，而不是 void。
func TestNegativeModuleConstInsideFunctionBody(t *testing.T) {
	const src = `f = () (r i64) {
    h i64 = -7
    r = h
}
main = () () {
    x = f()
}`
	m := lowerSrc(t, src)
	if _, err := m.EmitLLVM(); err != nil {
		t.Fatalf("EmitLLVM failed for a local bound to a negative literal: %v", err)
	}
}
