package checker

import "testing"

// 本檔案釘住「`_ = expr` 顯式捨棄」豁免（option-capture-assign.md §4/§5 的並行判斷）：
// `_` 目標的裸索引 / 裸溢出運算不得報 idxhndld / ovfhndld。每個豁免測試都搭配一
// 個「非 `_` 目標」對照組，證明豁免是 `_` 帶來的，而非診斷本身不報。

// TestDiscardExemptsUnhandledIndex：`_ = arr[i]` 不得報 idxhndld。
func TestDiscardExemptsUnhandledIndex(t *testing.T) {
	src := `f = (arr []i64, i i64) () {
    _ = arr[i]
}`
	res := ValidateUnhandledIndex(parseProg(t, src), "src/app.no")
	for _, r := range res {
		if r.TraceID == unhandledIndexTraceID {
			t.Fatalf("idxhndld reported for `_ = arr[i]`: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestBareIndexStillReportedWithoutDiscard（對照組）：plain 函式內裸 `x = arr[i]`
// 仍須報 idxhndld。
func TestBareIndexStillReportedWithoutDiscard(t *testing.T) {
	src := `f = (arr []i64, i i64) () {
    x = arr[i]
}`
	res := ValidateUnhandledIndex(parseProg(t, src), "src/app.no")
	found := false
	for _, r := range res {
		if r.TraceID == unhandledIndexTraceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected idxhndld for bare `x = arr[i]` in plain function, got %+v", res)
	}
}

// TestDiscardExemptsUnhandledOverflow：`_ = a + b`（未標註整數運算）不得報 ovfhndld。
func TestDiscardExemptsUnhandledOverflow(t *testing.T) {
	src := `f = (a i64, b i64) () {
    _ = a + b
}`
	res := ValidateUnhandledOverflow(parseProg(t, src), "src/app.no")
	for _, r := range res {
		if r.TraceID == unhandledOverflowTraceID {
			t.Fatalf("ovfhndld reported for `_ = a + b`: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestBareOverflowStillReportedWithoutDiscard（對照組）：plain 函式內裸 `x = a + b`
// 仍須報 ovfhndld。
func TestBareOverflowStillReportedWithoutDiscard(t *testing.T) {
	src := `f = (a i64, b i64) () {
    x = a + b
}`
	res := ValidateUnhandledOverflow(parseProg(t, src), "src/app.no")
	found := false
	for _, r := range res {
		if r.TraceID == unhandledOverflowTraceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ovfhndld for bare `x = a + b` in plain function, got %+v", res)
	}
}

// TestDiscardExemptsIndexInOptionFunc：回傳 ?T 的函式內裸 `_ = arr[i]` 同樣豁免
// （捕獲路徑下 `_` 也只是捨棄，不影響周圍推斷）。
func TestDiscardExemptsIndexInOptionFunc(t *testing.T) {
	src := `f = (arr []i64, i i64) (r ?i64) {
    r = nil
    _ = arr[i]
    r = r
}`
	res := ValidateUnhandledIndex(parseProg(t, src), "src/app.no")
	for _, r := range res {
		if r.TraceID == unhandledIndexTraceID {
			t.Fatalf("idxhndld reported for `_ = arr[i]` inside ?T-returning fn: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}
