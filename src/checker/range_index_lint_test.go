package checker

import "testing"

// TestRangeIndexLoopHint：`for i <- [a..b) { … arr[i] … }` 這種「手動按下標遍歷」
// 會被建議改用容器迭代（trace rngidx，HINT）。
func TestRangeIndexLoopHint(t *testing.T) {
	src := `f = (a []i64) (r i64) {
    sum = 0
    for i <- [0..a.len()): {
        x = a[i]
        sum = sum + x
    }
    r = sum
}`
	counts := lintCounts(t, src)
	if counts[rngidxTraceID] != 1 {
		t.Fatalf("expected 1 rngidx hint, got %v", counts)
	}
}

// TestRangeIndexLoopNoHintForContainerIteration：容器迭代沒有下標，自然沒有
// 「越界」這回事 —— 不得提示。這是 rngidx 的正向對照。
func TestRangeIndexLoopNoHintForContainerIteration(t *testing.T) {
	src := `f = (a []i64) (r i64) {
    sum = 0
    for x <- a: {
        sum = sum + x
    }
    r = sum
}`
	counts := lintCounts(t, src)
	if counts[rngidxTraceID] != 0 {
		t.Fatalf("container iteration must not be hinted, got %v", counts)
	}
}

// TestRangeIndexLoopNoHintWhenProvablyInBounds：下標可證明在界內時，option 已被
// 靜默消除（D7-R1），沒有代價可省 —— 提示只會是噪音，故不報（§8.1(a)）。
func TestRangeIndexLoopNoHintWhenProvablyInBounds(t *testing.T) {
	src := `f = (a [3]i64) (r i64) {
    sum = 0
    for i <- [0..3): {
        x = a[i]
        sum = sum + x
    }
    r = sum
}`
	counts := lintCounts(t, src)
	if counts[rngidxTraceID] != 0 {
		t.Fatalf("provably in-bounds loop index must not be hinted, got %v", counts)
	}
}
