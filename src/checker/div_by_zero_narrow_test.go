package checker

// div_by_zero_narrow_test.go — 「窄型別域裡的除數字面量被截斷成 0」規則的回歸測試。
//
// 規則（trace `q7k2nzr4`，ERROR，阻斷 no build）：整數 `/` 或 `%` 的**結果型別
// 取左運算元**，右運算元被強制轉成同型別。故 `b % 256` 且 `b byte` 實際算的是
// `byte % (256 as byte)` = `b % 0`：字面量 256 落不進 8 位，強制轉換只留低 8 位
// （0），除數於編譯期就恆為 0。
//
// 實測依據（2026-11-04，把本規則臨時關掉跑 probe）：
//
//	i byte = 5
//	i = (i + 1) % 256      → 執行期印 "integer division or remainder by
//	                          zero (the divisor is 0; result forced to 0)"，i 得 0
//
// 這正是 `src/std/crypto/rc4.no` 原本 `s[(s[i] + s[j]) % 256]` 的坑（兩個運算元
// 都是 `?byte`）——金鑰流全錯、KAT 掛掉。同檔 line 36/62/65 的 `% 256` 無事，因
// 其 `j`/`i` 是未標註 let 推成 i64，左運算元是 i64 就不截斷。故本規則必須
// **只認型別、不認拼寫**：同樣的 `% 256` 在 i64 域是正確寫法。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// divNarrowCount 回傳指定 trace 的報告數，並把全部報告印進 -v 日誌。
func divNarrowCount(t *testing.T, prog *parser.Program, trace string) int {
	t.Helper()
	view := BuildTHIRView(prog)
	if view == nil {
		t.Fatal("BuildTHIRView returned nil")
	}
	n := 0
	for _, r := range ValidateDivByZero(prog, view) {
		t.Logf("L%d:C%d [%s] %s", r.Line, r.Column, r.TraceID, r.Message)
		if r.TraceID == trace {
			n++
		}
	}
	return n
}

// TestDivByZeroNarrowByteMod256 正例：`byte` 域 `% 256` 必須報 q7k2nzr4，且文案
// 同時給出字面量值與被繼承的窄型別（使用者要據此改寫）。
func TestDivByZeroNarrowByteMod256(t *testing.T) {
	const src = `narrow-mod = (b byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) % 256
}`
	prog := parseProg(t, src)
	view := BuildTHIRView(prog)
	if view == nil {
		t.Fatal("BuildTHIRView returned nil")
	}
	res := ValidateDivByZero(prog, view)
	if len(res) != 1 {
		t.Fatalf("expected exactly 1 report, got %d: %+v", len(res), res)
	}
	if res[0].TraceID != divZeroNarrowTraceID {
		t.Errorf("TraceID = %q, want %q", res[0].TraceID, divZeroNarrowTraceID)
	}
	for _, want := range []string{"256", "byte", "truncated to 0"} {
		if !strings.Contains(res[0].Message, want) {
			t.Errorf("message missing %q:\n%s", want, res[0].Message)
		}
	}
}

// TestDivByZeroNarrowOptionPayload 正例：左運算元是 option 承載的 byte（`b ?byte`
// 參數、本地 `x ?byte`、`s[i]` 這種 `?byte` 下標讀取）都要報 —— 這正是 rc4 line 95
// 的原始形態（`s[i]` 與 `s[j]` 都是 `?byte`）。
//
// 三個子形是**判別力**要求：實測把 unwrap 迴圈關掉後，param 與 local 兩形退回 0
// 報，index 形因為型別表已直接給 byte 仍照報 —— 只釘 index 形就等於沒釘住 option
// 解包這段判據。
func TestDivByZeroNarrowOptionPayload(t *testing.T) {
	cases := map[string]string{
		"param": `f = (b ?byte) (r ?byte) {
    r = b % 256
}`,
		"local": `f = (b byte) (r byte) {
    x ?byte = b
    r = x % 256
}`,
		"index": `f = (s []byte, i i64) (r ?byte) {
    #{d11-probe=wrap}
    r = s[i] % 256
}`,
	}
	for name, src := range cases {
		if n := divNarrowCount(t, parseProg(t, src), divZeroNarrowTraceID); n != 1 {
			t.Errorf("[%s] got %d reports of %s, want 1", name, n, divZeroNarrowTraceID)
		}
	}
}

// TestDivByZeroNarrowWiderTypes 正例：i16 域的字面量上限是 65535，`% 65536`
// 同樣截斷成 0；文案必須印出使用者寫的那個型別（i16）。
func TestDivByZeroNarrowWiderTypes(t *testing.T) {
	const src = `narrow-i16 = (v i16) (r i16) {
    #{overflow=wrap}
    r = (v + 1) % 65536
}`
	prog := parseProg(t, src)
	view := BuildTHIRView(prog)
	if view == nil {
		t.Fatal("BuildTHIRView returned nil")
	}
	res := ValidateDivByZero(prog, view)
	if len(res) != 1 {
		t.Fatalf("expected exactly 1 report for i16, got %d: %+v", len(res), res)
	}
	if !strings.Contains(res[0].Message, "i16") {
		t.Errorf("message should name i16:\n%s", res[0].Message)
	}
}

// TestDivByZeroNarrowNegativeLiteral 正例：負字面量 `-256` 取模同樣截斷成
// `-256 as byte` = 0（低 8 位全 0），不能因為是 PrefixExpression 就漏掉。
func TestDivByZeroNarrowNegativeLiteral(t *testing.T) {
	const src = `narrow-neg = (b byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) % -256
}`
	if n := divNarrowCount(t, parseProg(t, src), divZeroNarrowTraceID); n != 1 {
		t.Errorf("negative literal: got %d reports, want 1", n)
	}
}

// TestDivByZeroNarrowQuiet 負例群：以下每一條都**不得**報 q7k2nzr4。
//
// 判別力要點：
//   - i64 是寬型別（Bits=64 直接排除）——這正是 rc4 line 36/62/65 的形態，
//     誤報會讓 std 直接編不過；
//   - `% 255` 的低 8 位非 0，截斷後仍是 255，是合法寫法；
//   - 變數除數無法靜態判定；
//   - u32 域裡 256 完全放得下；
//   - 字面量直接寫 0 屬另一條規則（onpn7zgb），不能重複報成截斷。
func TestDivByZeroNarrowQuiet(t *testing.T) {
	cases := []struct{ name, src string }{
		{"i64-left", `f = (n i64) (r i64) {
    #{overflow=wrap}
    r = (n + 1) % 256
}`},
		{"byte-mod-255", `f = (b byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) % 255
}`},
		{"byte-var-divisor", `f = (b byte, m byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) % m
}`},
		{"u32-fits", `f = (u u32) (r u32) {
    #{overflow=wrap}
    r = (u + 1) % 256
}`},
		{"byte-div-keeps", `f = (b byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) / 300
}`},
	}
	for _, c := range cases {
		if n := divNarrowCount(t, parseProg(t, c.src), divZeroNarrowTraceID); n != 0 {
			t.Errorf("[%s] false positive: %d reports of %s", c.name, n, divZeroNarrowTraceID)
		}
	}
}

// TestDivByZeroLiteralZeroNotDoubleReported 守門：字面量 0 只走舊規則
// （onpn7zgb），新規則必須讓路，不能一條語句報兩次。
func TestDivByZeroLiteralZeroNotDoubleReported(t *testing.T) {
	const src = `f = (b byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) % 0
}`
	prog := parseProg(t, src)
	if n := divNarrowCount(t, prog, divZeroTraceID); n != 1 {
		t.Errorf("literal zero: got %d reports of %s, want 1", n, divZeroTraceID)
	}
	if n := divNarrowCount(t, prog, divZeroNarrowTraceID); n != 0 {
		t.Errorf("literal zero must NOT also report as narrowing: got %d", n)
	}
}

// TestDivByZeroNarrowNeedsView 守門：本規則依賴型別表，view=nil（純語法口徑）
// 時必須靜默 —— 否則 ValidateDivByZero 的 legacy 呼叫端（transpiler 前置的
// 語法通道）會在沒有型別資訊時亂報。
func TestDivByZeroNarrowNeedsView(t *testing.T) {
	const src = `f = (b byte) (r byte) {
    #{overflow=wrap}
    r = (b + 1) % 256
}`
	if res := ValidateDivByZero(parseProg(t, src), nil); len(res) != 0 {
		t.Fatalf("view=nil must not report the narrowing rule, got %d: %+v", len(res), res)
	}
}
