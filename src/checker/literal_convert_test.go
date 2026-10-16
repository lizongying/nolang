package checker

import (
	"strings"
	"testing"
)

// TestValidateLiteralConversion 釘住「对数字字面量直接做 .to-<type>() 转换」的 HINT。
func TestValidateLiteralConversion(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantHit   bool   // 是否应产生提示
		wantSub   string // 命中时期望消息中包含的建议片段
		wantNoSub string // 不应出现的片段
	}{
		{"int-to-str", "s = (123).to-str()\n", true, "'123'", ""},
		{"zero-to-str", "s = (0).to-str()\n", true, "'0'", ""},
		{"float-to-str", "s = (3.14).to-str()\n", true, "'3.14'", ""},
		{"float-zero-to-str", "s = (0.0).to-str()\n", true, "'0'", "'0.0'"},
		{"float-to-f32", "f f32 = (3.14).to-f32()\n", true, "v f32 = 3.14", ""},
		{"int-to-f64", "x = (123).to-f64()\n", true, "v f64 = 123", ""},
		{"int-to-i64", "x = (123).to-i64()\n", true, "v i64 = 123", ""},
		// 不应提示：变量接收者（真正的运行时转换）。
		{"var-receiver", "s = n.to-str()\n", false, "", ""},
		// 不应提示：浮点→整数会截断，字面量无法等价表达。
		{"float-to-int", "x = (3.14).to-i64()\n", false, "", ""},
		// 不应提示：整数越界无法直接声明。
		{"int-overflow-narrow", "x = (300).to-u8()\n", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := mustParse(t, tc.src)
			var hit *ValidateResult
			results := ValidateLiteralConversion(prog)
			for i := range results {
				if results[i].TraceID == literalConvTraceID {
					hit = &results[i]
					break
				}
			}
			if tc.wantHit && hit == nil {
				t.Fatalf("expected a literal-conv hint for %q, got none", tc.src)
			}
			if !tc.wantHit && hit != nil {
				t.Fatalf("unexpected hint for %q: %+v", tc.src, *hit)
			}
			if hit != nil {
				if tc.wantSub != "" && !strings.Contains(hit.Message, tc.wantSub) {
					t.Fatalf("message %q missing suggestion %q", hit.Message, tc.wantSub)
				}
				if tc.wantNoSub != "" && strings.Contains(hit.Message, tc.wantNoSub) {
					t.Fatalf("message %q should not contain %q", hit.Message, tc.wantNoSub)
				}
			}
		})
	}
}
