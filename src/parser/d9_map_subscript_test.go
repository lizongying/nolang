package parser

// d9_map_subscript_test.go — `?=` 用在 map 下标上必须被**拒绝**（D9 §12.2.1）。
//
// D9 要求 `a[i] = v` 显式解包，理由是「位置可能越界，写入无落地处」。map 的下标
// 是**键**：读缺席 → nil，写 = put（任何键恒有定义），没有越界写，也就没有可解包
// 的东西 —— 所以 `m[k] = v` 直接写（见 hir2mir 的 map 下标降糖与
// test/std/map-index.no）。
//
// `m[k] ?= v` 则不同：它不是「多余」而是**降糖出来就是错的**。
//   - `?=` 的 ok 臂是「读出 a[i]，再写回元素**位置**」；map 读出的 ?V 只是 get 的
//     out-param 副本，写回那个副本不会回到 map。
//   - 更糟的是 containerElemType("[str]i64") 把 map 当数组，于是这条路径**会**
//     命中 isSafeIndexBase 分支，降糖出的控制流让同一张 map 在多条路径上各 drop
//     一次（三重 free）。
//
// 实测后果（这是本测试存在的理由）：pristine HEAD 是编译期
// `lencap struct: len/cap unsupported on struct hashmap-str-i64`；MIR 补上 map
// 下标降糖后变成**编译成功、运行期 SIGBUS** —— 从「编不过」退化成「跑起来崩」，
// 是最不该出现的那一类回归。故 lowerUnwrapAssign 必须在 isSafeIndexBase 之前
// 拦下并报错。
//
// ⚠️ 验证时务必 NOLANG_AST_CACHE_OFF=1：AST 缓存内容寻址，编译器改了而程序内容
// 不变时 build 会重放旧 AST（本次据此误判过一次）。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseForErrors 解析并完整 lowering，返回 p.Errors()（不因错误而 Fatal）。
func parseForErrors(src string) []string {
	p := New(lexer.New(src))
	p.ParseProgram()
	return p.Errors()
}

func hasErrContaining(errs []string, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

// TestMapSubscriptUnwrapIsRejected 正例：`m['a'] ?= 5` 必须是错误，且文案要把
// 正确写法（`m[k] = v`）指出来。
func TestMapSubscriptUnwrapIsRejected(t *testing.T) {
	src := `f = () (r ?i64) {
    m [str]i64 = { 'a': 1 }
    m['a'] ?= 5
    r = 0
}
f()`
	errs := parseForErrors(src)
	if !hasErrContaining(errs, "`?=` on a map subscript is not supported") {
		t.Fatalf("`?=` on a map subscript was accepted (or reported differently); errors=%v", errs)
	}
	if !hasErrContaining(errs, "[str]i64") {
		t.Errorf("diagnostic does not name the base type: %v", errs)
	}
	if !hasErrContaining(errs, "= v` directly") {
		t.Errorf("diagnostic does not point at the correct spelling `m[k] = v`: %v", errs)
	}
}

// TestMapSubscriptPlainWriteAccepted 对照 1：同一段代码用 `=` 必须无错误。
// 没有这条，上面的「有错误」也能靠「map 下标写根本不被接受」通过。
func TestMapSubscriptPlainWriteAccepted(t *testing.T) {
	src := `f = () (r ?i64) {
    m [str]i64 = { 'a': 1 }
    m['a'] = 5
    r = 0
}
f()`
	if errs := parseForErrors(src); len(errs) > 0 {
		t.Fatalf("plain map subscript write `m['a'] = 5` must be accepted; errors=%v", errs)
	}
}

// TestArraySubscriptUnwrapStillAccepted 对照 2：数组/vec 的 `a[i] ?= v` 仍走
// D9 的 place-write 降糖，不得被 map 的拦截误伤（containerElemType 对两者
// 都给「元素型别」，唯一的区分点就是 IsMapTypeString）。
func TestArraySubscriptUnwrapStillAccepted(t *testing.T) {
	src := `f = () (r ?i64) {
    a []i64 = [1, 2]
    a[0] ?= 5
    r = 0
}
f()`
	if errs := parseForErrors(src); len(errs) > 0 {
		t.Fatalf("array subscript `?=` must keep working; errors=%v", errs)
	}
}
