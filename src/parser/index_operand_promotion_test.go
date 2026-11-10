package parser

import (
	"strings"
	"testing"
)

// 本檔案釘住「option match 的安全索引主體，其下標算術會被提升成 option 綁定」。
//
// 背景（example/mysql-driver/src/mysql.no:743）：
//
//	seed[8 + i]: { ok -> it = sb2  -> _ }
//
// 原地 `arr[8 + i]` 的算術在 MIR 是**裸** add —— 沒有 `*.with.overflow`，
// 溢位靜默回繞，而且 `#{overflow=...}` 對它完全無效（實測 IR 逐字節相同）。
// 使用者能寫的補救只有「先把下標綁成 option」這一種，但那等於要為每個下標
// 多寫一條陳述。resolveSubjectMatch 因此自己把下標算術提升成
// `__idx_L_C ?T = <arith>`：顯式 `?T` 型別節點讓 MIR 發 overflow 內建，而下標
// 本身是 option 時索引讀取會傳播失敗 tag（短路在越界檢查之前），於是一個
// match 就消費了「下標溢位」與「越界」兩步。
//
// 判別力：把 subjectMatchPrep.idxOptElem 的賦值註釋掉，兩個斷言同時失敗
//（暫存不再有 `?i64` 型別、讀取也不再走暫存）。

const indexOperandPromotionSrc = `f = (seed [16]byte, i i64) () {
    seed[8 + i]: { ok -> it = 65  -> _ }
}
`

// 值 match 主體（沒有 ok/nil/err 臂 ⇒ 沒有失敗臂可消費）不該提升：
// `arr[i]: { 5 -> … }` 的語意是「拿元素跟 5 比」，不是選項消費。
const indexOperandValueMatchSrc = `f = (seed [16]byte, i i64) () {
    seed[8 + i]: { 5 -> print(1)  -> _ }
}
`

func TestIndexOperandArithPromotedToOptionTemp(t *testing.T) {
	dump := dumpHIR(t, indexOperandPromotionSrc)
	// 下標算術必須落在一條 `?i64` 宣告的暫存上 —— MIR 只對「結果型別是
	// option」的整數算術發 llvm.<s|u><add|sub|mul>.with.overflow。
	if !strings.Contains(dump, "__idx_") {
		t.Fatalf("index operand was not hoisted into an __idx temp; the arithmetic "+
			"stays a bare add and overflows silently.\n--- HIR ---\n%s", dump)
	}
	if !strings.Contains(dump, "t=?i64") {
		t.Fatalf("the __idx temp carries no ?i64 type, so MIR emits a plain add "+
			"(no overflow check).\n--- HIR ---\n%s", dump)
	}
	// 讀取必須改走暫存：`arr[8 + i]` 的求值只發生一次（暫存的宣告裡），
	// 主體讀的是 `arr[__idx]`。少了這一步，提升出來的 option 就沒人消費。
	if strings.Contains(dump, "8 + i") {
		t.Fatalf("the subject still computes `8 + i` inline; it must read through "+
			"the __idx temp.\n--- HIR ---\n%s", dump)
	}
}

func TestIndexOperandArithNotPromotedForValueMatch(t *testing.T) {
	dump := dumpHIR(t, indexOperandValueMatchSrc)
	if strings.Contains(dump, "__idx_") {
		t.Fatalf("a value match has no failure arm to consume the index option; "+
			"it must keep the inline index.\n--- HIR ---\n%s", dump)
	}
}
