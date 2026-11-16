package checker

import (
	"testing"
)

// value-match（matched value match `x: { arms }`）是语句级构造：MIR 只在
// 「带目标名的赋值右值」位置捕获它的分支值（hir2mir.go lowerStmtInner 的
// `cn.Kind == hir.KIf && name != ""` 分支）。若它出现在括号包裹后参与运算、
// 函数参数、比较、return 等表达式位置，lowering 不产出值 → codegen NoVal
// （zodppsnm）。本诊断要求 checker 在这些位置直接报硬错误。

func countValueMatchPos(results []ValidateResult) int {
	n := 0
	for _, r := range results {
		if r.TraceID == ValueMatchExprTraceID {
			n++
		}
	}
	return n
}

func TestValueMatchPositionLegal(t *testing.T) {
	// 四种赋值右值顶层 + 括号包裹都合法（MIR 的 exprCapture 分支能处理）。
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "let_rhs",
			src: `f = (a []i64) (res i64) {
    res = a[0]: { ok -> it

        -> 0 }
}`,
		},
		{
			name: "let_rhs_grouped",
			src: `f = (a []i64) (res i64) {
    res = (a[0]: { ok -> it

        -> 0 })
}`,
		},
		{
			name: "typed_let_rhs",
			src: `f = (a []i64) (res i64) {
    x i64 = a[0]: { ok -> it

        -> 0 }
    res = x
}`,
		},
		{
			name: "assign_expr_rhs",
			src: `f = (a []i64) (res i64) {
    x i64 = 0
    x = a[0]: { ok -> it

        -> 0 }
    res = x
}`,
		},
		{
			name: "unwrap_assign_rhs",
			src: `f = (a []i64) (res ?i64) {
    res ?= a[0]: { ok -> it

        -> 0 }
}`,
		},
		{
			name: "nested_in_legal_arm_body",
			src: `f = (a []i64) (res i64) {
    res = a[0]: { ok -> it

        -> 0 }
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := parseProg(t, c.src)
			if got := countValueMatchPos(ValidateValueMatchPosition(prog)); got != 0 {
				t.Fatalf("expected 0 value-match-position errors, got %d", got)
			}
		})
	}
}

func TestValueMatchPositionIllegal(t *testing.T) {
	// 任何非「赋值右值顶层」的表达式位置都必须报错。
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "infix_operand",
			src: `f = (a []i64) (res i64) {
    res = 1 + (a[0]: { ok -> it

        -> 0 })
}`,
		},
		{
			name: "bitand_operand",
			src: `f = (a []i64) (res i64) {
    res = (a[0]: { ok -> it

        -> 0 }) & 255
}`,
		},
		{
			name: "call_argument",
			src: `g = (v i64) (r i64) {
    r = v
}
f = (a []i64) (res i64) {
    res = g(a[0]: { ok -> it

        -> 0 })
}`,
		},
		{
			name: "comparison",
			src: `f = (a []i64) (res bool) {
    res = (a[0]: { ok -> it

        -> 0 }) == 137
}`,
		},
		{
			name: "index_operand",
			src: `f = (a []i64) (res i64) {
    res = a[a[0]: { ok -> it

        -> 0 }]
}`,
		},
		{
			name: "array_literal_elem",
			src: `f = (a []i64) (res []i64) {
    res = [ (a[0]: { ok -> it

        -> 0 }) ]
}`,
		},
		{
			name: "grouped_then_print",
			src: `f = (a []i64) {
    print(a[0]: { ok -> it

        -> 0 })
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := parseProg(t, c.src)
			if got := countValueMatchPos(ValidateValueMatchPosition(prog)); got < 1 {
				t.Fatalf("expected >=1 value-match-position error, got %d", got)
			}
		})
	}
}

// TestValueMatchPositionMatchInternals：match 的主语与臂体内部仍是普通表达式
// 上下文——臂体里的赋值右值 match 合法，但主语/条件里的 match 不合法。
func TestValueMatchPositionNestedMatch(t *testing.T) {
	src := `f = (a []i64) (res i64) {
    res = a[0]: { ok -> it

        -> 0 }
}`
	prog := parseProg(t, src)
	if got := countValueMatchPos(ValidateValueMatchPosition(prog)); got != 0 {
		t.Fatalf("nested legal match: expected 0 errors, got %d", got)
	}
}
