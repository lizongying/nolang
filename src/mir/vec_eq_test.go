package mir

// vec_eq_test.go — `[]T == []T` 必須是**結構比較**。
//
// %vec 是 {i64 len, i64 cap, i64 data} 這個聚合型別，所以 emitCmp 的整數路徑
// 會發出 `icmp eq %vec`，LLVM 直接拒絕（"icmp requires integer operands"）。
// 修復前 `a == b`（兩個 `[]i64`）**根本編不過**，而經過元素讀取的同一比較
// （`[][]i64` 上的 `aa[i] == bb[i]`）也一樣失敗。
//
// 修法是把比較導到特化 helper：先比長度，再比元素。這裡釘住三件事：
//  1. 不再有 `icmp ... %vec`；
//  2. 比較走 @__nolang_vec_eq_* 這個 helper；
//  3. 元素型別分派正確 —— 純量元素用 memcmp，**擁有堆記憶體的 `str` 元素必須
//     逐元素用 @str_eq 比內容**（平鋪 memcmp 比的是字串的 data 指標，兩個相等的
//     字串會被判成不同，這正是本專案反覆踩的「靜默錯誤答案」）。

import (
	"strings"
	"testing"
)

// irFuncsPrefix 回傳所有 head 含 `@<prefix>` 的 define 區塊文字。
// 巢狀 slice 會產生多個 helper（外層與內層各一），它們在 IR 中的先後由型別 id
// 決定，測試不能假設哪一個先出現。
func irFuncsPrefix(ir, prefix string) []string {
	var out []string
	for _, part := range strings.Split(ir, "\ndefine ") {
		head := part
		if i := strings.IndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
		if !strings.Contains(head, "@"+prefix) {
			continue
		}
		if i := strings.Index(part, "\n}"); i >= 0 {
			out = append(out, part[:i])
			continue
		}
		out = append(out, part)
	}
	return out
}

func TestVecEqualityIsStructural(t *testing.T) {
	scalar := mustEmit(t, `main = () () {
    a []i64 = [1, 2]
    b []i64 = [1, 2]
    if a == b { print('eq') }
}`)
	if strings.Contains(scalar, "icmp eq %vec") {
		t.Fatalf("`[]i64 == []i64` still emits `icmp eq %%vec`:\n%s", irFunc(scalar, "_nolang_main"))
	}
	if !strings.Contains(scalar, "@__nolang_vec_eq_") {
		t.Fatalf("`[]i64 == []i64` was not routed through the structural helper:\n%s", irFunc(scalar, "_nolang_main"))
	}
	helper := irFuncsPrefix(scalar, "__nolang_vec_eq_")
	if len(helper) != 1 {
		t.Fatalf("want exactly 1 vec equality helper for `[]i64`, got %d", len(helper))
	}
	if !strings.Contains(helper[0], "@memcmp") {
		t.Fatalf("a scalar element type must be compared with memcmp:\n%s", helper[0])
	}
	if strings.Contains(helper[0], "@str_eq") {
		t.Fatalf("a scalar element type must not go element-wise:\n%s", helper[0])
	}

	strs := mustEmit(t, `main = () () {
    a []str = ['ab']
    b []str = ['ab']
    if a == b { print('eq') }
}`)
	if strings.Contains(strs, "icmp eq %vec") {
		t.Fatalf("`[]str == []str` still emits `icmp eq %%vec`:\n%s", irFunc(strs, "_nolang_main"))
	}
	sh := irFuncsPrefix(strs, "__nolang_vec_eq_")
	if len(sh) == 0 {
		t.Fatal("no vec equality helper emitted for `[]str`")
	}
	found := false
	for _, h := range sh {
		if strings.Contains(h, "@str_eq") {
			found = true
		}
	}
	if !found {
		t.Fatalf("`[]str` elements are compared by POINTER, not content (no @str_eq in any helper):\n%s", strings.Join(sh, "\n"))
	}
}

// TestVecEqualityNestedSliceIsRecursive：`[][]i64` 整體相比時，被比的兩個值是
// `[][]i64`，元素型別是 `[]i64` —— 元素本身是 %vec，必須遞迴到下一層 helper，
// 而不是把兩個 {len,cap,data} 三元組當純量比（那會比 data 指標，內容相同也會
// 判成不同）。
//
// 反向對照：`aa[i] == bb[i]` 比的是 `[]i64` 這個**元素**，元素型別是 `i64`，
// 所以只需要一個 helper 且用 memcmp —— 這是 `[][]i64` 上元素比較的正確形態。
func TestVecEqualityNestedSliceIsRecursive(t *testing.T) {
	ir := mustEmit(t, `main = () () {
    aa [][]i64 = [[1, 2], [3, 4]]
    bb [][]i64 = [[1, 2], [9, 9]]
    if aa == bb { print('eq') }
}`)
	if strings.Contains(ir, "icmp eq %vec") {
		t.Fatalf("`[][]i64 == [][]i64` still emits `icmp eq %%vec`:\n%s", irFunc(ir, "_nolang_main"))
	}
	helpers := irFuncsPrefix(ir, "__nolang_vec_eq_")
	if len(helpers) < 2 {
		t.Fatalf("`[][]i64 == [][]i64` emitted %d helper(s), want >= 2 (outer + inner): the nested element type was compared as a flat triple", len(helpers))
	}
	recursed := false
	for _, h := range helpers {
		if strings.Contains(h, "@__nolang_vec_eq_") {
			recursed = true
		}
	}
	if !recursed {
		t.Fatalf("no helper recurses into an inner vec helper: nested slices would be compared by data POINTER:\n%s", strings.Join(helpers, "\n"))
	}

	// 元素讀取形態：只需一個 helper，元素是 i64 → memcmp。
	elem := mustEmit(t, `main = () () {
    aa [][]i64 = [[1, 2], [3, 4]]
    bb [][]i64 = [[1, 2], [9, 9]]
    i i64 = 0
    if aa[i] == bb[i] { print('eq') }
}`)
	eh := irFuncsPrefix(elem, "__nolang_vec_eq_")
	if len(eh) != 1 {
		t.Fatalf("`aa[i] == bb[i]` on `[][]i64` emitted %d helper(s), want exactly 1 (element type is i64)", len(eh))
	}
	if !strings.Contains(eh[0], "@memcmp") {
		t.Fatalf("`aa[i] == bb[i]` did not compare the i64 payload with memcmp:\n%s", eh[0])
	}
}
