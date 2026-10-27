package checker

import (
	"testing"
)

// 本檔案釘住「運算符重載（_add 等）」的 checker 型別約定（plan：
// quantum-pulse-tesla-ZuRb5xRs，計畫「測試 / 驗證」段的 checker 測試項）。
//
// 約定（對齊內建數值型別，參考 int.div 先例）：
//   - 運算符重載方法的結果參數**書寫**為 `?T`，但編譯器當作 payload T：
//     方法體內 `c = <T>`（plain 賦值）必須合法，不觸發 option 誤報。
//   - 運算符 `a op b`（降糖成 `a._op(b)`）與顯式 `a._op(b)` 呼叫，結果型別皆為
//     payload T。故 `r T = a + b`（plain 目標）合法；若結果被誤推成 `?T`，
//     plain 目標這一會命中 15w45dqk（option 不能隱式解包），據此反證結果是 plain。
//
// 降糖發生在 parser 的 lowering（ParseProgram 內），所以到 ValidateTypes 時
// `a + b` 已是 `a._add(b)` 的方法呼叫；本測試同時兼作「降糖確實发生」的守衛
// （若未降糖，`a + b` 對結構體是類型錯誤）。
//
// 🔴 執行前提同 option 系列：需 StdSigKey，但本測試只用**本地結構體**、不依賴
// std 簽名表，故不帶 key 亦應通過。

// olProg 是一個自足的最小運算符重載程式：結構體 big 定義 big._add（結果書寫
// ?big、體內把參數原樣賦回）。
const olProg = `big = {
    v i64
}

big._add = (b big) (c ?big) {
    c = b
}

f = (a big, b big) (out big) {
    r big = a + b
    out = r
}
`

// TestOperatorOverloadBodyPlainAssign：big._add 體內 `c = b`（plain 賦值給宣告為
// `?big` 的結果參數）必須合法——編譯器忽略 `?`，結果參數生效型別為 payload big。
// 若約定破功（結果參數被當 option），會冒出 option 相關的 15w45dqk。
func TestOperatorOverloadBodyPlainAssign(t *testing.T) {
	results := ValidateTypes(parseProg(t, olProg))
	if n := countTrace(results, "15w45dqk"); n != 0 {
		t.Fatalf("operator method body must be a plain (option-free) assign, got %d 15w45dqk: %v", n, results)
	}
}

// TestOperatorOverloadResultIsPayload：`a + b` 結果為 payload big，故 `r big = a + b`
// （plain 目標）合法、不被誤報為隱式解包。這同時證明降糖把 `a + b` 重寫成了
// `a._add(b)`（否則結構體加法會直接是類型錯誤）。
func TestOperatorOverloadResultIsPayload(t *testing.T) {
	results := ValidateTypes(parseProg(t, olProg))
	if n := countTrace(results, "15w45dqk"); n != 0 {
		t.Fatalf("`a + b` result assigned to a plain target must be option-free, got %d 15w45dqk: %v", n, results)
	}
}

// TestOperatorOverloadExplicitCallEquivalence：顯式 `a._add(b)` 呼叫與運算符 `a + b`
// 產出相同型別約定（皆 payload big、plain 目標合法）。
func TestOperatorOverloadExplicitCallEquivalence(t *testing.T) {
	src := `big = {
    v i64
}

big._add = (b big) (c ?big) {
    c = b
}

f = (a big, b big) (out big) {
    r big = a._add(b)
    out = r
}
`
	results := ValidateTypes(parseProg(t, src))
	if n := countTrace(results, "15w45dqk"); n != 0 {
		t.Fatalf("explicit a._add(b) on plain target must be option-free, got %d 15w45dqk: %v", n, results)
	}
}

// TestOperatorOverloadHarnessGuard：反向護欄——確認本 harness 裡 15w45dqk 機制
// 確實**會**開火（用 std 的 `str.byte`：回傳 `?byte`，賦給 plain i64 目標必須被
// 拒）。如此上面三條 `countTrace(...,"15w45dqk")==0` 才是「運算符重載把 `?` 剝掉、
// 結果為 plain」的真通過，而非「本測試根本觸發不了 15w45dqk」的空過。
// 這也间接守住了運算符重載「忽略 `?`」特例的作用域：只有 `_op` 閉集方法被特殊
// 處理，一般 option 呼叫的 plain 目標賦值仍照常上報。
func TestOperatorOverloadHarnessGuard(t *testing.T) {
	src := `f = (s str) (out i64) {
    b i64 = s.byte(0)
    out = b
}
`
	results := ValidateTypes(parseProg(t, src))
	if n := countTrace(results, "15w45dqk"); n == 0 {
		t.Fatalf("harness guard: option→plain must still be rejected here, got 0; all: %v", results)
	}
}
