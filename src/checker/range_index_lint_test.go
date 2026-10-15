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

// TestRangeIndexLoopHintWhenProvablyInBounds：D7 最終定案（兩層分離）後，下標可證明
// 在界內時用戶側**仍需寫 option 並解包**——容器迭代消除的正是這份解包負擔，故
// rngidx 提示照常發出。此前 D7-R1 曾在此降噪（in-bounds 不提示），已廢。
func TestRangeIndexLoopHintWhenProvablyInBounds(t *testing.T) {
	src := `f = (a [3]i64) (r i64) {
    sum = 0
    for i <- [0..3): {
        x = a[i]
        sum = sum + x
    }
    r = sum
}`
	counts := lintCounts(t, src)
	if counts[rngidxTraceID] != 1 {
		t.Fatalf("provably in-bounds loop index should still be hinted (user side is still option), got %v", counts)
	}
}
