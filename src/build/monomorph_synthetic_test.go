package build

// monomorph_synthetic_test.go — 泛型实例化必须保留合成的赋值节点标记。
//
// 背景（2026-11-10）：substituteExpr 对 *parser.AssignExpression 是逐字段重建，
// 早期只搬 Token/Left/Value，把 IsSynthetic 丢了。D9 place-write 的写回
// （`a[__idx] = it`）本身带 IsSynthetic，checker 靠它放行编译器自己的产物；
// 一旦这个标记在泛型实例化时丢失，那段写回就被 `no vet` 当成用户手写的
// 下标直写报 idxwrite。典型受害者是 std/heap 的 `?=` 写入——模板阶段不报，
// 一旦被具体型别实例化就报，而 `no build` 走 THIR 路径（不重建该节点）不报，
// 于是 vet 红 / build 绿，两端口径分裂。

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// TestSubstituteExprPreservesAssignSynthetic 守門：合成赋值经过型别替换后
// IsSynthetic 必须仍然为 true，且注解一并带过去。
func TestSubstituteExprPreservesAssignSynthetic(t *testing.T) {
	src := &parser.AssignExpression{
		Left:        &parser.IndexExpression{Left: &parser.Identifier{Value: "a"}},
		Value:       &parser.Identifier{Value: "it"},
		IsSynthetic: true,
	}
	got, ok := substituteExpr(src, map[string]string{"t": "i64"}).(*parser.AssignExpression)
	if !ok {
		t.Fatalf("substituteExpr returned %T, want *parser.AssignExpression", got)
	}
	if !got.IsSynthetic {
		t.Fatal("IsSynthetic lost during generic instantiation: the D9 write-back store would be reported as a user index write (idxwrite)")
	}
}

// TestSubstituteExprKeepsUserAssignNonSynthetic 反例守門：用户手写的赋值不能被
// 误标成合成——否则真正的下标直写会被静默放行。
func TestSubstituteExprKeepsUserAssignNonSynthetic(t *testing.T) {
	src := &parser.AssignExpression{
		Left:  &parser.IndexExpression{Left: &parser.Identifier{Value: "a"}},
		Value: &parser.Identifier{Value: "v"},
	}
	got, ok := substituteExpr(src, map[string]string{"t": "i64"}).(*parser.AssignExpression)
	if !ok {
		t.Fatalf("substituteExpr returned %T, want *parser.AssignExpression", got)
	}
	if got.IsSynthetic {
		t.Fatal("user-written assignment became synthetic: real raw index writes would escape idxwrite")
	}
}
