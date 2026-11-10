package checker

import (
	"strings"
	"testing"
)

// TestValidateNamingTopLevel 釘住「頂層全域宣告也要守命名規範」的行為修正：
// 過去 ValidateNaming 直接 `continue` 跳過頂層 LetStatement，導致
// `tmp__1 = ...` 這類含中間底線的頂層變數名逃過檢查。現在頂層宣告與區域
// 變數同級（TraceID 2dhoris2），並沿用相同豁免：顯式捨棄 `_`、編譯器合成
// `__` 前綴、模組級 ALL-CAPS 常數。
func TestValidateNamingTopLevel(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantBad []string // 期望被 2dhoris2 標記的名字
		clean   bool     // 期望零命名違規
	}{
		{"internal-underscore", "tmp__1 = 1\n", []string{"tmp__1"}, false},
		{"double-underscore-mid", "my__var = 1\n", []string{"my__var"}, false},
		{"valid-kebab", "tmp-1 = 1\n", nil, true},
		{"valid-plain", "tmp = 1\n", nil, true},
		{"all-caps-const", "LEVEL-ERROR = 1\n", nil, true},
		{"discard", "_ = 1\n", nil, true},
		// 單一前導底線是 std 既有私有標記（_days-in-month / _RE-QUANT-NONE），
		// 不是「中間底線」，不得報錯；但前導 + 中間底線仍要攔。
		{"leading-underscore-private", "_days-in-month = 1\n", nil, true},
		{"leading-underscore-all-caps", "_RE-QUANT-NONE = 1\n", nil, true},
		{"leading-plus-middle-underscore", "_foo_bar = 1\n", []string{"_foo_bar"}, false},
		{"leading-plus-uppercase", "_Foo = 1\n", []string{"_Foo"}, false},
		{"leading-plus-trailing-underscore", "_foo_ = 1\n", []string{"_foo_"}, false},
		{
			"multi-assign-bad",
			"get-pair = () (a i64, b i64) {\n    a = 1\n    b = 2\n}\n" +
				"x__y, z = get-pair()\n",
			[]string{"x__y"}, false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := mustParse(t, tc.src)
			results := ValidateNaming(prog)
			var got []string
			for _, r := range results {
				if r.TraceID != "2dhoris2" {
					continue
				}
				// message: "'%s' should use only lowercase letters and hyphens"
				if i := strings.Index(r.Message, "'"); i >= 0 {
					if j := strings.Index(r.Message[i+1:], "'"); j >= 0 {
						got = append(got, r.Message[i+1:i+1+j])
					}
				}
			}
			if tc.clean {
				if len(got) != 0 {
					t.Fatalf("expected no naming violations, got %v", got)
				}
				return
			}
			for _, want := range tc.wantBad {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected naming violation for %q, got %v", want, got)
				}
			}
		})
	}
}
