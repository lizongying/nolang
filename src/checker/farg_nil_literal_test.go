package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 本檔案釘住「`nil` 字面量當實參」的兩條邊界（2026-11-13）。
//
// 背景：參數默認值從語言中移除後，`?T` 形參的「不傳值」只能靠呼叫端顯式寫
// `nil`（或傳一個 `?T` 變數）。但 inferExprType 沒有 *parser.NilLiteral 分支，
// 落進 default 後回的是整數家族預設值，於是
//
//	f = (s str, n ?i64) (r i64) { … }
//	f('x', nil)      ; 報 `argument 2 of 'f': expected '?i64', got 'i64'`
//
// 也就是把 nil 誤認成 i64。isArgTypeCompatible 的等值 / 隱式解包 / 模組前綴
// 幾條都救不了它（`IsOptionRaw("i64")` 為假），所以在該函式開頭按 AST 形狀
// 補了一條「nil 字面量 → 任何 ?T」。這條規則只放行 option 目標，非 option
// 目標的判定完全不動（負向對照見下）。

// countArgTraceIDs 跑 ValidateFuncArgs，統計指定 trace ID 的診斷數。
func countArgTraceIDs(t *testing.T, src string, ids ...string) int {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	n := 0
	for _, r := range ValidateFuncArgs(prog, "") {
		if want[r.TraceID] {
			n++
		}
	}
	return n
}

// TestNilLiteralArgForOptionParamAccepted：`nil` 是每個 `?T` 的 none 變體，
// 傳給 `?T` 形參必須放行，且不得同時觸發「裸 option 實參」規則（3604icnf）
// ——那是給「把 ?T 當 T 傳」用的，`nil` 不是 ?T 值。
func TestNilLiteralArgForOptionParamAccepted(t *testing.T) {
	src := `f = (s str, n ?i64) (r i64) {
	r = 0
	n: {
		ok(v) -> r = v

		nil ->

		err ->
	}
}
g = () (res i64) {
	res = f('x', nil)
}
`
	if n := countArgTraceIDs(t, src, "6fgg3htw", "3604icnf"); n != 0 {
		t.Fatalf("nil passed to a ?i64 parameter must be accepted, got %d diagnostic(s)", n)
	}
}

// TestNilLiteralArgForStrParamRejected：負向對照。放行只針對 option 目標，
// `nil` 傳給 `str` 形參仍然要報型別不符，否則這條規則就成了「nil 萬能」。
func TestNilLiteralArgForStrParamRejected(t *testing.T) {
	src := `f = (s str) (r i64) {
	r = s.len-bytes()
}
g = () (res i64) {
	res = f(nil)
}
`
	if n := countArgTraceIDs(t, src, "6fgg3htw"); n != 1 {
		t.Fatalf("nil passed to a str parameter must still be rejected, got %d 6fgg3htw error(s)", n)
	}
}
