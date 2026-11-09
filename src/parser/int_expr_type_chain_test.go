package parser

import (
	"strings"
	"testing"
)

// 這一組測試鎖住「整數表達式的型別來源」：位元／移位右值、欄位讀取，以及
// 值位 match 主語是欄位時的 `it` 綁定。
//
// 三者失敗時都是**靜默**的 —— 編譯照過、沒有診斷，只是
//   - `x-opt: { ok -> v = it }` 的 ok 臂永不命中（運算元型別查不到 ⇒ 不發 ?T，
//     MIR 把 `ok` 當常量比對），或
//   - ok 臂的 `it` 被零填（主語是欄位時 buildItBinding 不建綁定）。
//
// 歷史事故：bigint._div-i64 的 `cur = (rem << 32) | (li & MASK)` 之後
// `rem-opt = cur % v` 從不賦值 ⇒ 餘數恆 0 ⇒ 多肢除法商全錯、`to-str` 只剩個位數；
// x509.der-len 的 `n = b & 127` 之後 `adv-opt = 1 + n` 同理 ⇒ adv 恆 0 ⇒ DER
// 長度欄位全錯；time.date-to-unix 的 `y = d.year` 無型別 ⇒ 年份沒校正。

// TestIntExprChainBitwiseRHS：`c = a & 255` 之後 `m = c % 10` 必須推出 ?i64。
func TestIntExprChainBitwiseRHS(t *testing.T) {
	src := `v = (a i64) (r i64) {
    c = a & 255
    m = c % 10
    m: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

print(v(7))
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=c t=i64") {
		t.Fatalf("位元運算的結果 (`a & 255`) 應登記為 plain i64:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=m t=?i64") {
		t.Fatalf("`c %% 10` 應推斷為 ?i64（c 由位元運算得到）:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok 臂的 it 應綁成 i64:\n%s", dump)
	}
}

// TestIntExprChainBitwiseWidens：`n = b & 127`（b 為 byte）的結果是 i64，
// 不是 byte —— 否則下游 `1 + n` 仍推不出 ?i64（x509.der-len）。
func TestIntExprChainBitwiseWidens(t *testing.T) {
	src := `v = (data []byte) (r i64) {
    b = data[0]: {
        ok -> it
        -> 0
    }
    n = b & 127
    adv-opt = 1 + n
    adv-opt: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

d []byte = with-len(2)
print(v(d))
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=n t=i64") {
		t.Fatalf("`b & 127`（b 為 byte）應取較寬的一側 i64:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=adv-opt t=?i64") {
		t.Fatalf("`1 + n` 應推斷為 ?i64:\n%s", dump)
	}
}

// TestIntExprChainFieldRHS：`y = d.year` 要取到結構體宣告的欄位型別，
// 否則 `y - 1` 推不出 ?i64（time.date-to-unix 的換算鏈）。
func TestIntExprChainFieldRHS(t *testing.T) {
	src := `pair {
    year i64
    name str
}

v = (d pair) (r i64) {
    y = d.year
    y-opt = y - 1
    y-opt: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

p = pair { year: 1970, name: 'x' }
print(v(p))
`
	dump := dumpHIR(t, src)
	// 注意：`y = d.year` 本身不是 option 算術，let 節點不帶 InferredType
	//（與 `n = .` 同口徑）；可觀測的契約是**下游**取到了欄位型別。
	if !strings.Contains(dump, "let s=y-opt t=?i64") {
		t.Fatalf("`y - 1` 應推斷為 ?i64（y 由欄位讀取得到 i64）:\n%s", dump)
	}
}

// TestIntExprChainFieldSubjectBindsIt：`v = p.i: { ok -> it }` 的主語是欄位，
// ok 臂的 it 必須有綁定，否則 MIR 零填（index-out 的
// `option 欄位直接指派值 → ok(42)`）。
func TestIntExprChainFieldSubjectBindsIt(t *testing.T) {
	src := `box {
    i ?i64
}

b = box { i: 42 }
v = b.i: {
    ok -> it
    -> -1
}
print(v)
`
	dump := dumpHIR(t, src)
	// 綁定的 Type 刻意留空（buildItBinding 的既有契約：parse 期未必知道主語型別，
	// 交給 codegen 從 varTypes 取 %option）。可觀測的事實是「ok 臂裡多了一條
	// `let it = <欄位讀取>`」——修好之前這條完全不存在，臂內的 `it` 因此被零填。
	if !strings.Contains(dump, "let s=it") {
		t.Fatalf("欄位主語的值位 match 必須為 ok 臂建 `it` 綁定:\n%s", dump)
	}
}
