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

// TestRangeIndexLoopNoHintWhenRewriteNotEquivalent：提示必須是一張兌現得了的
// 支票 —— 只有「改寫成容器迭代等價」時才提示。下標另有其職（對齊兩個容器、
// 被當值用）、覆蓋範圍對不上容器長度、基底不可迭代、嵌套重綁同名下標，
// 都得沉默（見 range_index_convert.go）。
func TestRangeIndexLoopNoHintWhenRewriteNotEquivalent(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 下標在兩個容器間對齊：容器迭代表達不了。
			name: "parallel_arrays",
			src: `f = (dst []i64, src []i64) () {
    i <- [0..dst.len()): {
        dst[i]: {
            ok -> it = src[i]

            -> _
        }
    }
}`,
		},
		{
			// 下標本身被當值使用。
			name: "index_used_as_value",
			src: `f = (a []i64) (r i64) {
    i <- [0..a.len()): {
        x = a[i]
        r = r + x + i
    }
}`,
		},
		{
			// 上界不是字面量也不是 base.len()：覆蓋範圍證不出等價。
			name: "window_upper_bound",
			src: `f = (a []i64, n i64) (r i64) {
    i <- [0..a.len() - n): {
        x = a[i]
        r = r + x
    }
}`,
		},
		{
			// 只走容器的前綴：改寫成 `x <- b` 會多寫元素。
			name: "prefix_of_fixed_array",
			src: `f = (r i64) {
    b [8]i64
    i <- [0..4): {
        x = b[i]
        r = r + x
    }
}`,
		},
		{
			// 基底是接收者欄位：`.field` 不能出現在 `x <- …` 右側。
			name: "receiver_field_base",
			src: `t {
    f [4]i64
}

t.sum = () (r i64) {
    i <- [0..4): {
        x = .f[i]
        r = r + x
    }
}`,
		},
		{
			// 外層下標被內層迴圈重綁：只有內層能改寫。
			name: "nested_rebind",
			src: `f = (a []i64) (r i64) {
    i <- [0..a.len()): {
        x = a[i]
        r = r + x
        i <- [0..a.len()): {
            y = a[i]
            r = r + y
        }
    }
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counts := lintCounts(t, tc.src)
			want := 0
			if tc.name == "nested_rebind" {
				want = 1 // 內層自己等價可改寫，外層不行
			}
			if counts[rngidxTraceID] != want {
				t.Fatalf("%s: expected %d rngidx hint(s), got %v", tc.name, want, counts)
			}
		})
	}
}

// TestRangeIndexLoopHintForProvisionedVec：`with-len(N)` 分配好的向量配上
// `[0..N)` 是整段迭代，可以提示。
func TestRangeIndexLoopHintForProvisionedVec(t *testing.T) {
	src := `f = (r i64) {
    acc []i64 = with-len(32)
    i <- [0..32): {
        x = acc[i]
        r = r + x
    }
}`
	counts := lintCounts(t, src)
	if counts[rngidxTraceID] != 1 {
		t.Fatalf("expected 1 rngidx hint for with-len(32) + [0..32), got %v", counts)
	}
}
