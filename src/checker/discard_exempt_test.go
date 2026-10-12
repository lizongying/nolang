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
// 仍須被報。
//
// 🔴 2026-10-12 勘誤：原本斷言「報 idxhndld」，且只跑 ValidateUnhandledIndex。D6 的
// 捕獲加寬（collectCaptureNodes 的 IndexExpression 分支不再受
// enclosingFuncHasOptResult 把關）之後，`x = arr[i]` 會被就地捕獲成 `x ?i64`，
// 捕獲把**產生處的 idxhndld HINT 整條吸收**，只剩「從未被讀取」的 optunuse ERROR
// —— 而 optunuse 由 ValidateUnhandledOverflow 產出，所以這裡必須走 RunAllLints
// （lintCounts）才看得到。放寬的是嚴重度分級（D2），不是「不報」。
//
// 產生處 idxhndld 仍在的形態是「沒有捕獲點」的賦值（結果參數 `r = arr[i]`），
// 由 option_capture_lint_test.go 的 index_into_plain_result_param 負責釘住。
func TestBareIndexStillReportedWithoutDiscard(t *testing.T) {
	src := `f = (arr []i64, i i64) () {
    x = arr[i]
}`
	counts := lintCounts(t, src)
	if counts[optUnuseTraceID] != 1 {
		t.Fatalf("expected 1 optunuse for bare `x = arr[i]` in plain function, got %v", counts)
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
