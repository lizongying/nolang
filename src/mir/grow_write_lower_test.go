package mir

// grow_write_lower_test.go — D9 grow-write（place-match 寫主體在可增長容器上）
// 的 MIR 級回歸。
//
// 未分配 vec（裸宣告 `v []u64`，len=cap=data=0）上的 `v[i]: { ok -> it = x }`
// 曾靜默丟寫：match 主體沿用讀語義 `0 <= i < len` 門控，len=0 使每個下標都
// 「越界」，ok 臂永不觸發，ensureVecBuffer 的分配+寫入永不執行。修法是
// parser 為寫主體打 hir.FlagGrowWrite，hir2mir 對可增長容器改用
// lowerGrowWriteIndex（`i >= 0` 門控，gwo.ok/gwo.none/gwo.merge 三塊）。
//
// 本檔在 MIR 層釘住兩條不變量：
//   - 未分配 slice 寫主體（非可證下標）：必須走 gwo.* 塊，且不得出現
//     lowerSafeIndex 的 idx.ok/idx.none/idx.merge（len 門控已移除）。
//   - 固定陣列寫主體（非可證下標）：保持 idx.* 塊（陣列不可增長，讀語義
//     界檢查不變）——控制組，證明改道是「按容器型別」而非「一律改」。

import "testing"

func TestGrowWriteSliceSubjectUsesGrowBlocks(t *testing.T) {
	src := `foo = (v []i64) () {
    v[3]: {
        ok -> it = 7
        -> _
    }
}
main = () {
    w []i64 = with-len(1)
    foo(w)
}`
	m := lowerSrc(t, src)
	if !hasBlockNamed(m, "gwo.ok") || !hasBlockNamed(m, "gwo.none") || !hasBlockNamed(m, "gwo.merge") {
		t.Fatalf("slice place-write subject did not lower via lowerGrowWriteIndex (missing gwo.ok/gwo.none/gwo.merge blocks)")
	}
	if hasBlockNamed(m, "idx.ok") || hasBlockNamed(m, "idx.none") || hasBlockNamed(m, "idx.merge") {
		t.Fatalf("slice place-write subject still emitted lowerSafeIndex blocks (idx.ok/idx.none/idx.merge); len-gating must not gate a grow-write")
	}
}

func TestFixedArraySubjectKeepsSafeIndexBlocks(t *testing.T) {
	// 局部固定陣列（陣列「參數」會退化成 slice，故不用參數形態——那樣
	// isGrowableContainer 為真、走 gwo 路徑是正確行為）。下標用參數 i，
	// 與 mir/d9_write_bce_test.go 用例 C 同構。
	src := `foo = (i i64) () {
    a [4]i64 = [0, 0, 0, 0]
    a[i]: {
        ok -> it = 7
        -> _
    }
}
main = () {
    foo(3)
}`
	m := lowerSrc(t, src)
	if !hasBlockNamed(m, "idx.ok") || !hasBlockNamed(m, "idx.none") || !hasBlockNamed(m, "idx.merge") {
		t.Fatalf("fixed-array place-write subject lost the lowerSafeIndex blocks; a non-growable container must keep the read-style bounds check")
	}
	if hasBlockNamed(m, "gwo.ok") {
		t.Fatalf("fixed-array place-write subject wrongly routed to lowerGrowWriteIndex")
	}
}
