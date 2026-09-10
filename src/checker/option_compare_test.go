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
			// 裸指派來自 option 內建：size 是 ?i64，拿它跟 0 比是無意義的。
			name: "plain_assign_from_builtin_option_then_compare",
			src: `f = () () {
    size = fstat-size(.fd)
    size == 0 -> {
        content = nil
    }
}`,
			wantErr:  true,
			contains: "size",
		},
		{
			// 顯式 ?i64 標註：r 是 option，與 0 比要報錯。
			name: "explicit_option_annotation_then_compare",
			src: `f = () () {
    r ?i64 = get-size()
    r == 0 -> return
}`,
			wantErr:  true,
			contains: "r",
		},
		{
			// 結果參數是 option：在函式體內拿它跟數字比要報錯。
			name: "result_param_compared_with_number",
			src: `g = () (r ?i64) {
    r == 0 -> return
}`,
			wantErr:  true,
			contains: "r",
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

// TestOptionComparisonWiredIntoLints 確認規則已接入 RunAllLints（no vet / LSP 路徑）。
func TestOptionComparisonWiredIntoLints(t *testing.T) {
	src := `f = () () {
    size = fstat-size(.fd)
    size == 0 -> return
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
