package checker

// union_arg_test.go — 聯合別名實參的「成員歸屬」守衛（unionarg）。
//
// 缺陷形狀（實測 tmp/bx/d3.no）：
//
//	f = (s str) (out str) {
//	    m bigint = bigint.zero()
//	    c = s.byte(0)            ; ?byte
//	    m = m * 10 + (c - 48)    ; big = bigint | i64 收到 byte
//	}
//
// `+` 被降成 `bigint._add`，其形參型別是 union 別名 `big`。當時的參數檢查走
// `isArgTypeCompatible` 的「聯合別名反向相容」分支——它對每個成員**遞迴**跑通用
// 規則，於是 `byte → i64` 的整數加寬把這一路判成合法。可是選成员的動作发生在
// 更晚的 monomorph：`build.inferExprMemberType` 只拿型別**名字**去比對成員列表，
// `byte` 不在 `bigint | i64` 裡就一個也匹配不上，改寫靜默不發作，模板名
// `bigint._add` 原样留在呼叫點，最後以內部錯誤收場：
//
//	Error: ... EmitLLVM: unknown callee bigint.bigint._add in func f
//
// 「vet 過、run 炸」是這裡最貴的那類失敗：報錯沒有源碼位置，也沒有可执行的修法。
// 而在另一侧，同一條比對路徑只要 union 帶聚合體 fallback，推定失敗就會**默默選中
// 錯誤的成員**，ABI 對不上卻被 opaque pointer 放過編譯期，運行期算出 0（即 D3）。
//
// 因此規則定案為「拒收並報診斷」：不做隱式加寬、不退回聚合體，實參型別必須**本身就是
// 某個成員**（允許 ?T 解包與模組前綴），否則在源碼位置報 ERROR，並告訴使用者先顯式轉換。
// 整數字面量豁免——它本來就由既有的範圍／新類型規則裁決，union 成員選擇另有推定路徑。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// countTraceIDErrors 跑完整參數檢查，回傳指定 TraceID 的診斷數目與訊息。
func countTraceIDErrors(t *testing.T, src, traceID string) (int, []string) {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	n := 0
	var msgs []string
	for _, r := range ValidateFuncArgs(prog, "") {
		if r.TraceID == traceID {
			n++
			msgs = append(msgs, r.Message)
		}
	}
	return n, msgs
}

// TestUnionArgNonMemberRejected pin 住缺陷本身：byte 進 bigint|i64。
func TestUnionArgNonMemberRejected(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test`")
	}
	src := `f = (s str) (out str) {
    m bigint = bigint.zero()
    c = s.byte(0)
    m = m * 10 + (c - 48)
    out = m.to-str()
}
print(f('4'))
`
	n, msgs := countTraceIDErrors(t, src, "unionarg")
	if n != 1 {
		t.Fatalf("expected 1 unionarg error for `bigint + byte` (big = bigint | i64), got %d: %v", n, msgs)
	}
	// 訊息必須可執行：點出實參型別、union 名、成員列表。
	for _, want := range []string{"byte", "big", "bigint", "i64"} {
		if !strings.Contains(msgs[0], want) {
			t.Errorf("unionarg message should mention %q, got %q", want, msgs[0])
		}
	}
}

// TestUnionArgMemberAllowed 對照組：合法成員實參不得被打斷。
// 若這三條也報錯，說明守衛把規則收得太緊（會把 std 全部運算符呼叫判死）。
func TestUnionArgMemberAllowed(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("std signature tables not baked; run via `make test`")
	}
	cases := []struct {
		name string
		src  string
	}{
		// bigint 本身就是成員。
		{"bigint arg", `f = (a bigint, b bigint) (out str) {
    m = a + b
    out = m.to-str()
}
print(f(bigint.zero(), bigint.zero()))
`},
		// i64 是成員。
		{"i64 arg", `f = (a bigint, k i64) (out str) {
    m = a + k
    out = m.to-str()
}
print(f(bigint.zero(), 1))
`},
		// ?i64（標註驱动的窄化結果）靠解包算成員——std 四處是這種寫法。
		{"?i64 arg", `f = (a bigint, c ?byte) (out str) {
    d ?i64 = 0
    m = a * 10 + d
    out = m.to-str()
}
print(f(bigint.zero(), c))
`},
		// 整數字面量豁免（走既有範圍規則）。
		{"integer literal", `f = (a bigint) (out str) {
    m = a * 10
    out = m.to-str()
}
print(f(bigint.zero()))
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if n, msgs := countTraceIDErrors(t, c.src, "unionarg"); n != 0 {
				t.Fatalf("expected no unionarg error, got %d: %v", n, msgs)
			}
		})
	}
}
