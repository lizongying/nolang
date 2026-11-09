package build

// value_match_assign_field_test.go — 值位 match 作為**指派右值**時不能被丟棄。
//
// 歷史缺陷（2026-11-09 根治）：MIR 是 statement-oriented，lowerExpr 的
// hir.KIf 分支在沒有 exprCapture 時回 NoVal（「control flow used as
// expression value」）。KLet 分支早有 exprSink capture 機制，但
// lowerAssignNode（KAssign）沒有 —— 拿到 NoVal 就 `return NoVal`，整條
// 指派連同 match 鏈與 setfield 一起被靜默丟棄：
//
//	b.n = k: { ok -> 777  -> 999 }
//
// 編出的 LLVM IR 裡完全沒有 777/999、沒有條件分支、沒有欄位 store，
// 欄位維持零初始值，觀測恆為 0。寫成 statement match
//（`k: { ok -> b.n = it  -> _ }`）卻正常，這個不對稱正是 bzip2 解碼
// 迴圈計數器不推進、以及 std 裡 9 處「identifier subject 一步式 match
// 恆為 0」的同源根因。
//
// 修復：lowerAssignNode 對稱補上 KLet 的 exprCapture / exprSink 流程，
// 把 if 鏈當語句 lower、各 arm 的值 capture 進共享 slot，再讓 slot 走
// 原本的 KIdent / KDot / KIndex store 分支。
//
// 判別力：把 lowerAssignNode 新增的 KIf capture 區塊註解掉，本檔第一個
// 測試立即失敗（IR 裡 777/999 與 br i1 全部消失）；第二個對照組
//（局部變數 LHS，走 KLet）仍須通過 —— 證明命中確實是 KAssign 這條路徑。

import (
	"strings"
	"testing"
)

const valueMatchFieldAssignSrc = `box {
    n i64
}

p = (a i64) (r i64) {
    b = box {}
    k = a + 1
    b.n = k: {
        ok -> 777
        -> 999
    }
    r = b.n
}

print(p(41))
`

// 對照組：同一個 match 寫給局部變數（KLet 路徑，修復前就正確）。
const valueMatchLocalAssignSrc = `p = (a i64) (r i64) {
    k = a + 1
    r = k: {
        ok -> 777
        -> 999
    }
}

print(p(41))
`

// irHasMatchChain 檢查 IR 真的含有值位 match 的兩條臂。修復前整條
// 指派被丟棄，777/999 兩個常數都不會出現在模組裡（prelude 也沒有）。
func irHasMatchChain(ir string) bool {
	return strings.Contains(ir, "777") && strings.Contains(ir, "999")
}

func TestValueMatchAssignToFieldKeepsMatchChain(t *testing.T) {
	tr := NewTranspiler(nil)
	ir, err := tr.Compile(valueMatchFieldAssignSrc)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	if !irHasMatchChain(ir) {
		t.Fatalf("field assignment with a value-position match RHS was dropped: "+
			"IR has no 777/999 arms and no conditional branch (lowerAssignNode "+
			"returned NoVal for a hir.KIf RHS).\n--- IR ---\n%s", ir)
	}
	// 欄位 store 必須真的存在（getelementptr into %box + store i64）。
	if !strings.Contains(ir, "getelementptr inbounds %box") || !strings.Contains(ir, "store i64") {
		t.Fatalf("field assignment emitted no setfield store.\n--- IR ---\n%s", ir)
	}
}

func TestValueMatchAssignToLocalStillKeepsMatchChain(t *testing.T) {
	tr := NewTranspiler(nil)
	ir, err := tr.Compile(valueMatchLocalAssignSrc)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	if !irHasMatchChain(ir) {
		t.Fatalf("control group (local LHS / KLet path) regressed.\n--- IR ---\n%s", ir)
	}
}
