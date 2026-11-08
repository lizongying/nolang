package parser

import "testing"

// TestFuncVarTypeToleratesDoubledModulePrefix 钉住 FuncVarTypeRef 的前缀回退。
//
// 背景（src/std/uuid.no 的 `sb ?= b.bytes[i]` 曾因此静默失效）：std 模块从 AST
// blob 回放时 curFuncName 会多一层模块前缀（`uuid.eq` → `uuid.uuid.eq`），而参数
// / 局部的型别是在烤 blob 那条路径上以单层名登记的。精确键落空时若不回退，
// indexBaseVarType 拿不到参数 `b` 的型别 → `?=` 推不出元素型别 → 不挂 ?T、不发
// LHS 预宣告 → 绑定名无型别无 slot（`sa` 走 self 分支有 methodStack 兜底所以正常，
// `b.` 开头的第二个绑定直接失效，表现为「同一 block 第二个 `?=` 不可用」）。
//
// 反向守门：**精确键优先**，剥前缀不得覆盖正确结果。
func TestFuncVarTypeToleratesDoubledModulePrefix(t *testing.T) {
	s := NewSemanticContext()
	s.SetFuncVarType("uuid.eq", "b", "uuid")
	s.SetFuncVarType("uuid.cmp", "i", "i64")

	cases := []struct {
		fn, varName, want string
	}{
		{"uuid.eq", "b", "uuid"},        // 精确键
		{"uuid.uuid.eq", "b", "uuid"},   // 多一层模块前缀（blob 回放）
		{"uuid.uuid.uuid.eq", "b", "uuid"}, // 逐层剥
		{"uuid.cmp", "i", "i64"},
		{"uuid.uuid.cmp", "i", "i64"},
	}
	for _, c := range cases {
		got, ok := s.FuncVarType(c.fn, c.varName)
		if !ok || got != c.want {
			t.Fatalf("FuncVarType(%q, %q) = (%q, %v), want (%q, true)", c.fn, c.varName, got, ok, c.want)
		}
	}

	// 未知变量名不得因剥前缀而"猜中"：回退只在键存在时生效。
	if got, ok := s.FuncVarType("uuid.uuid.eq", "nope"); ok {
		t.Fatalf("unknown var must stay unknown, got (%q, true)", got)
	}

	// 精确键优先：同名变量在不同函数作用域下各自取自己的型别。
	s.SetFuncVarType("uuid.uuid.eq", "b", "str")
	if got, _ := s.FuncVarType("uuid.uuid.eq", "b"); got != "str" {
		t.Fatalf("exact key must win over the stripped fallback, got %q", got)
	}
}
