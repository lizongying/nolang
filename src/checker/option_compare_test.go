package checker

import (
	"strings"
	"testing"
)

// TestOptionComparisonErrors 验证「option 值直接與非 option 值比較」會被報錯。
func TestOptionComparisonErrors(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantErr  bool
		contains string // 命中時訊息須含此子串
	}{
		{
			// 裸指派來自 option 內建：size 是 ?i64，拿它跟 0 比——nolang 會解開
			// 內層值再比（`?i64 == 0` 在 runtime 合法），故不應報錯。
			name: "plain_assign_from_builtin_option_then_compare",
			src: `f = () () {
    size = fstat-size(.fd)
    size == 0 -> {
        content = nil
    }
}`,
			wantErr: false,
		},
		{
			// 顯式 ?i64 標註：r 是 option，與 0 比（解開內層值）合法，不報錯。
			name: "explicit_option_annotation_then_compare",
			src: `f = () () {
    r ?i64 = get-size()
    r == 0 -> return
}`,
			wantErr: false,
		},
		{
			// 結果參數是 option：在函式體內拿它跟數字比（解開內層值）合法，不報錯。
			name: "result_param_compared_with_number",
			src: `g = () (r ?i64) {
    r == 0 -> return
}`,
			wantErr: false,
		},
		{
			// option 與「struct 值」比較仍報錯：struct 非值型別，語意不明
			// （非 option-vs-純量那種可解開內層值再比的情形），保留原規則。
			name: "option_compared_with_struct_still_errors",
			src: `point = (x i64, y i64) {}
f = () () {
    p ?point = make-point()
    q point = point { x: 1, y: 2 }
    p == q -> return
}`,
			wantErr:  true,
			contains: "p",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateOptionComparison(parseProg(t, c.src))
			if c.wantErr {
				if len(res) == 0 {
					t.Fatalf("expected an option-comparison error, got none")
				}
				if c.contains != "" {
					found := false
					for _, r := range res {
						if strings.Contains(r.Message, c.contains) {
							found = true
						}
					}
					if !found {
						t.Fatalf("expected message to mention %q, got: %v", c.contains, res)
					}
				}
			} else if len(res) != 0 {
				t.Fatalf("expected no error, got: %v", res)
			}
		})
	}
}

// TestOptionComparisonAllowed 验证合法寫法不應誤報。
func TestOptionComparisonAllowed(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// `v ?= expr` 解包後 v 是內層 i64，比較合法。
			name: "unwrap_assign_then_compare",
			src: `get-size = (x i64) (r ?i64) { r = x; return }
f = (fd i64) (out ?i64) {
    size ?= get-size(fd)
    size == 0 -> return
}`,
		},
		{
			name: "plain_i64_compare",
			src: `f = () () {
    n i64 = 5
    n == 0 -> return
}`,
		},
		{
			// 與 nil 比較是 match 的正規寫法。
			name: "compare_with_nil_is_allowed",
			src: `f = () () {
    r ?i64 = get-size()
    r == nil -> return
}`,
		},
		{
			// option 與 option 比較（整體相同型別）放行。
			name: "option_to_option_is_allowed",
			src: `f = () () {
    a ?i64 = get-a()
    b ?i64 = get-b()
    a == b -> return
}`,
		},
		{
			// match ok 臂內 `it` 是解包後的內層值：n = it; n <= 127 合法，
			// 不應被誤報成 option 比較（str.no / txt.no 的 to-i8 場景）。
			name: "match_it_unwrapped_no_false_positive",
			src: `to-i64 = () (r ?i64) { r = 0; return }
f = (fd i64) (out ?i64) {
    v = to-i64()
    v: {
        err -> return
        nil -> return
        ok -> {
            n = it
            n >= 0 - 128 && n <= 127 -> return
        }
    }
}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateOptionComparison(parseProg(t, c.src))
			if len(res) != 0 {
				t.Fatalf("expected no error for %s, got: %v", c.name, res)
			}
		})
	}
}

// TestNilCompareRequiresOption 验证 q4nilcmp 子规则：`nil` 只能与 option（`?T`）比较。
//
// 非 option 的 `T == nil` 必须硬错误：
//   - `str == nil` 恒为 false（拿 `%str-long` 结构体与 0 比），静默给错答案；
//   - `[]i64 == nil` 掉进整数比较路径，产生非法 LLVM（`icmp eq i64 %vec, i64`），
//     后端起才炸，错误讯息完全看不出原因。
func TestNilCompareRequiresOption(t *testing.T) {
	mustErr := []struct {
		name string
		src  string
	}{
		{"str_eq_nil", `s str = 'x'
print(s == nil)`},
		{"str_ne_nil", `s str = 'x'
print(s != nil)`},
		{"slice_eq_nil", `a []i64 = [1, 2, 3]
print(a == nil)`},
		{"txt_eq_nil", `t txt = 'x'
print(t == nil)`},
		{"fixed_array_eq_nil", `a [3]i64 = [1, 2, 3]
print(a == nil)`},
		{"int_eq_nil", `n i64 = 5
print(n == nil)`},
		// 推斷自字面量的型別同樣可靠（`s = 'x'` 就是 str），也要報。
		{"inferred_str_from_literal", `s = 'x'
print(s == nil)`},
		{"inferred_int_from_literal", `n = 5
print(n == nil)`},
		// 反向书写同样必须报。
		{"nil_on_left", `s str = 'x'
print(nil == s)`},
		// 函数体内（作用域 key 带函数名）也要报。
		{"inside_function", `f = () {
    s str = 'x'
    s == nil -> return
}`},
	}
	for _, c := range mustErr {
		t.Run("err/"+c.name, func(t *testing.T) {
			res := ValidateOptionComparison(parseProg(t, c.src))
			found := false
			for _, r := range res {
				if r.TraceID == optionNilCompareTraceID {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s diagnostic, got: %v", optionNilCompareTraceID, res)
			}
		})
	}

	allowed := []struct {
		name string
		src  string
	}{
		{"opt_str", `s ?str = nil
print(s == nil)`},
		{"opt_slice", `a ?[]i64 = nil
print(a == nil)`},
		{"opt_txt", `t ?txt = nil
print(t == nil)`},
		{"opt_int", `n ?i64 = nil
print(n != nil)`},
		// 型别未知（方法调用回传、跨模块、泛型）→ 保守放行。
		{"unknown_type", `e = ''.to-f64()
print(e == nil)`},
		// `v ?= expr` 解包后的 v 不是 option，但也推不出型别 → 放行。
		{"unwrapped_value", `get = () (r ?i64) {
    r = 1
}
f = () (out ?i64) {
    v ?= get()
    v == nil -> return
}`},
		// match 臂里对 option 的 nil 判定是正规写法。
		{"match_arm_on_option", `f = () (r ?i64) {
    r: {
        nil -> return
        ok -> return
        err -> return
    }
}`},
	}
	for _, c := range allowed {
		t.Run("ok/"+c.name, func(t *testing.T) {
			res := ValidateOptionComparison(parseProg(t, c.src))
			for _, r := range res {
				if r.TraceID == optionNilCompareTraceID {
					t.Fatalf("unexpected %s diagnostic: %v", optionNilCompareTraceID, r)
				}
			}
		})
	}
}

// TestOptionComparisonWiredIntoLints 確認規則已接入 RunAllLints（no vet / LSP 路徑）。
func TestOptionComparisonWiredIntoLints(t *testing.T) {
	src := `point = (x i64, y i64) {}
f = () () {
    p ?point = make-point()
    q point = point { x: 1, y: 2 }
    p == q -> return
}`
	prog := parseProg(t, src)
	lints := RunAllLints(prog, LintOptions{})
	found := false
	for _, l := range lints {
		if l.TraceID == optionCompareTraceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ValidateOptionComparison not surfaced through RunAllLints")
	}
}
