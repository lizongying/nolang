package checker

import (
	"strings"
	"testing"
)

// fireDeprecated 返回源码中被棄用呼叫 lint 命中的結果。
func fireDeprecated(t *testing.T, src string) []ValidateResult {
	t.Helper()
	return ValidateDeprecatedCalls(parseProg(t, src))
}

func TestDeprecatedCharToStr_Fires(t *testing.T) {
	cases := map[string]string{
		// 模組限定（LSP/單測未合併形態：DotExpression）
		"module_qualified": `f = () (s str) {
    s = number.char-to-str(13)
}`,
		// 裸呼叫（std 內 yaml.no/json.no 形態）
		"bare_call": `g = (c i64) (out str) {
    out = char-to-str(c)
}`,
		// 嵌套在拼接表達式內（yaml.no:710 形態）
		"nested_infix": `h = (c i64) (out str) {
    out = out - char-to-str(c)
}`,
		// 巢狀在 if/match 臂體內
		"in_if_arm": `k = (c i64) (out str) {
    c > 0 -> out = out - char-to-str(c)
}`,
	}
	for name, src := range cases {
		res := fireDeprecated(t, src)
		if len(res) == 0 {
			t.Errorf("%s: 期望命中，但未報", name)
			continue
		}
		for _, r := range res {
			if r.Message == "" || !strings.Contains(r.Message, "char.to-str()") {
				t.Errorf("%s: 提示訊息應包含替代寫法 char.to-str()，實際: %q", name, r.Message)
			}
		}
	}
}

// TestDeprecatedToStrFamily_Fires 覆蓋 number 的 i64/u64/f64-to-str 棄用提示。
func TestDeprecatedToStrFamily_Fires(t *testing.T) {
	cases := map[string]string{
		"i64-to-str": `a = (n i64) (s str) {
    s = number.i64-to-str(n)
}`,
		"u64-to-str": `b = (u u64) (s str) {
    s = u64-to-str(u)
}`,
		"f64-to-str": `c = (x f64) (s str) {
    s = f64-to-str(x)
}`,
	}
	want := map[string]string{
		"i64-to-str": "i64.to-str()",
		"u64-to-str": "u64.to-str()",
		"f64-to-str": "f64.to-str()",
	}
	for name, src := range cases {
		res := fireDeprecated(t, src)
		if len(res) == 0 {
			t.Errorf("%s: 期望命中，但未報", name)
			continue
		}
		if !strings.Contains(res[0].Message, want[name]) {
			t.Errorf("%s: 提示訊息應包含 %s，實際: %q", name, want[name], res[0].Message)
		}
	}
}

// TestDeprecatedToStr_DelegationSuppressed to-str 家族方法的底層實現委託給
// 棄用自由函數（i64.to-str 調 i64-to-str、i32.to-str 調 i64-to-str 等），
// 這類 std 內部實現呼叫不應被提示。
func TestDeprecatedToStr_DelegationSuppressed(t *testing.T) {
	src := `i64.to-str = () (out str) {
    out = i64-to-str(.)
}
i32.to-str = () (out str) {
    out = i64-to-str(.)
}
f32.to-str = () (out str) {
    d = f32-to-f64(.)
    out = f64-to-str(d)
}`
	if res := fireDeprecated(t, src); len(res) > 0 {
		t.Errorf("方法底層委託實現不該提示，實際: %v", res)
	}
}

// TestDeprecatedToStr_DeprecatedBodySuppressed 棄用函數自身的函數體內部
// 再呼叫其他棄用函數（f64-to-str 借 i64-to-str 拼指數段）不該提示。
func TestDeprecatedToStr_DeprecatedBodySuppressed(t *testing.T) {
	src := `f64-to-str = (x f64) (out str) {
    e i64 = 5
    estr = i64-to-str(e)
    out = estr
}`
	if res := fireDeprecated(t, src); len(res) > 0 {
		t.Errorf("棄用函數自身函數體不該提示，實際: %v", res)
	}
}

func TestDeprecatedCharToStr_NoFalsePositive(t *testing.T) {
	cases := map[string]string{
		// 新方法本身不該報
		"method_form": `f = (c char) (s str) {
    s = c.to-str()
}`,
		// 無方法形式替代的同族函數（純 Go builtin 轉換）不該報
		"other_to_num": `g = (x i64) (d f64) {
    d = number.i64-to-f64(x)
}`,
		// 普通呼叫
		"normal_call": `h = (p str) {
    print(p)
}`,
	}
	for name, src := range cases {
		if res := fireDeprecated(t, src); len(res) > 0 {
			t.Errorf("%s: 期望不報，卻命中: %v", name, res)
		}
	}
}
