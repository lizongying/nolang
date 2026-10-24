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

// ── 棄用方法（str.copy / txt.copy → a = b）──────────────────────────────
//
// 與自由函數不同，方法名屬於用戶命名空間：只有接收者型別可「肯定判定」為
// str/txt 時才提示（見 deprecatedMethodCalls 的表註釋）。

func TestDeprecatedCopyMethod_Fires(t *testing.T) {
	cases := map[string]string{
		// let 型別註解（最常見形態）
		"let_annotated": `d str = ''
s str = 'x'
d = s.copy()`,
		// 參數型別
		"param": `f = (s str) (out str) {
    out = s.copy()
}`,
		// 無註解、字面量推斷
		"literal_inferred": `s = 'hello'
d str = ''
d = s.copy()`,
		// txt 接收者
		"txt_recv": `t txt = 'hello'
u txt
u = t.copy()`,
		// 字面量接收者（number.no 形態）
		"literal_receiver": `f = () (out str) {
    out = 'nan'.copy()
}`,
	}
	for name, src := range cases {
		res := ValidateDeprecatedCalls(parseProg(t, src))
		if got := countTrace(res, deprecatedMethodCallTraceID); got != 1 {
			t.Errorf("%s: 期望 1 條 %s，實際 %d 條: %+v", name, deprecatedMethodCallTraceID, got, res)
			continue
		}
		for _, r := range res {
			if !strings.Contains(r.Message, "a = b") {
				t.Errorf("%s: 提示訊息應包含替代寫法 a = b，實際: %q", name, r.Message)
			}
			if r.File != "" {
				t.Errorf("%s: 單檔解析不該帶 File，實際 %q", name, r.File)
			}
		}
	}
}

func TestDeprecatedCopyMethod_NoFalsePositive(t *testing.T) {
	cases := map[string]string{
		// 用戶自訂結構體的同名方法不受影響（接收者型別不是 str/txt）
		"user_struct": `box {
    v i64
}
box.copy = () (out box) {
    out.v = .v
}
f = () {
    b box = box { v: 1 }
    c box
    c = b.copy()
}`,
		// 接收者型別未知（下標結果）⇒ 放行，寧漏報不誤報
		"unknown_recv": `f = (ss []str) (out str) {
    out = ss[0].copy()
}`,
		// 直接賦值（新寫法）本就該靜默
		"plain_assign": `d str = ''
s str = 'x'
d = s`,
		// 其他方法名
		"other_method": `s str = 'x'
n i64 = s.len()`,
		// bigint.copy 不在表內：賦值 `b = a` 共享 limbs 緩衝區，不具深拷貝語義
		"bigint": `a bigint = bigint.from-i64(7)
b bigint = a.copy()`,
	}
	for name, src := range cases {
		if res := ValidateDeprecatedCalls(parseProg(t, src)); len(res) > 0 {
			t.Errorf("%s: 期望不報，卻命中: %+v", name, res)
		}
	}
}

// std 內部實作（如 number.no 的 'nan'.copy()）不該對使用者刷屏：
// 語句自帶 std 來源檔時抑制，與 isSelfImplCall 對自由函數的豁免同一理由。
func TestDeprecatedCopyMethod_StdSourceSuppressed(t *testing.T) {
	prog := parseProg(t, `f = () (out str) {
    out = 'nan'.copy()
}`)
	for _, stmt := range prog.Statements {
		prog.Sem.SetSourceFileOf(stmt, "/repo/src/std/number.no")
	}
	if res := ValidateDeprecatedCalls(prog); len(res) > 0 {
		t.Errorf("std 內部呼叫不該提示，實際: %+v", res)
	}
}

// 作用域：內層同名變數遮蔽外層型別時，按內層型別判定（i64 的 copy 不該提示）；
// 內層塊結束後恢復外層型別（str 的 copy 應提示）。
func TestDeprecatedCopyMethod_ScopeShadowing(t *testing.T) {
	src := `f = () (out str) {
    s str = 'x'
    {
        s i64 = 1
        n i64 = s.copy()
    }
    out = s.copy()
}`
	res := ValidateDeprecatedCalls(parseProg(t, src))
	if got := countTrace(res, deprecatedMethodCallTraceID); got != 1 {
		t.Fatalf("期望 1 條（僅內層塊之後的 str 接收者），實際 %d 條: %+v", got, res)
	}
	if res[0].Line != 7 {
		t.Errorf("期望命中第 7 行（out = s.copy()），實際第 %d 行: %+v", res[0].Line, res[0])
	}
}
