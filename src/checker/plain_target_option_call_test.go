package checker

import (
	"testing"
)

// 本檔案是「顯式 plain 標註 + option 呼叫」嚴格模式的回歸釘（2026-10-21）。
//
// 背景：`str.byte` / `txt.byte` 的回傳型別由 plain 改為 `?byte`（越界回 nil，見
// docs/design/option-unified.md）。改動前 `b i64 = s.byte(0)` **靜默拆包**：MIR 只
// 把 option 的 payload 讀出來存進 plain 變數，tag（ok / nil / err）被丟掉——越界時
// 得到的是 0 而不是錯誤，且 `no vet` 一句話都不說。
//
// 這與 D1 嚴格模式（plain_target_option_arith_test.go）禁止的是同一件事，只是右值
// 從「算術」換成「呼叫」：明文標註只有寫在**收到 option 的那一行**上才是靜默解包
// 開關，因此必須是 ERROR，沿用 15w45dqk 的訊息與 trace。
//
// 實作在 checker.go：optionResultOfCallTarget 把「可靜態判定回傳 option 的呼叫」的
// 推斷型別升格為 `?T`（std 簽名表優先，Go builtin 表的 str/txt 裸名回落為輔），
// 交由既有的 15w45dqk 開火。
//
// 🔴 執行前提：std 簽名表要能解析（`str.at` 等純 std option 方法只有簽名表知道），
// 因此 `go test ./checker/` 必須帶 StdSigKey：
//
//	go test -count=1 -ldflags "-X github.com/lizongying/nolang/checker.StdSigKey=$(cd src && go run ./checker/genstdsig -key-only)" ./checker/
//
// 不帶 key 時本檔的 ERROR 案例會失敗（簽名表未暖表），這正是「不注入 key 就無法驗證
// std 簽名」的既有約定，不是迴歸。

// TestPlainTargetRejectsOptionCall 只有一種形狀必須是 ERROR：**本行**帶著使用者親手
// 寫下的 plain 標註、右值是可靜態判定回傳 option 的呼叫。
func TestPlainTargetRejectsOptionCall(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 使用者回報的形狀（tests/byte-option-index.no:17）。
			name: "byte_into_i64",
			src: `f = (s str, j i64) (out i64) {
    b i64 = s.byte(j)
    out = b
}`,
		},
		{
			// txt 接收者走同一個內建（Go 表以裸名 "byte" 同時覆蓋兩者）。
			name: "txt_byte_into_i64",
			src: `f = (t txt) (out i64) {
    b i64 = t.byte(0)
    out = b
}`,
		},
		{
			// 字面量接收者同樣是 str。
			name: "literal_receiver",
			src: `f = () (out i64) {
    b i64 = 'abc'.byte(0)
    out = b
}`,
		},
		{
			// char 目標：語義上是「碼點」，收到 byte 仍是靜默解包。
			name: "byte_into_char",
			src: `f = (s str) (out char) {
    c char = s.byte(0)
    out = c
}`,
		},
		{
			// 純 std option 方法（無 Go builtin 條目）：只有 std 簽名表知道它回
			// `?byte` —— 同一條規則必須一併覆蓋，否則只是把洞從 byte 挪到 at。
			name: "std_at_into_i64",
			src: `f = (s str) (out i64) {
    b i64 = s.at(0)
    out = b
}`,
		},
		{
			// 先無標註推斷出 option、之後又用明文標註重宣告。
			name: "redeclare_option_var_as_plain",
			src: `f = (s str) (out i64) {
    b = s.byte(0)
    out = 0
    b: {
        ok -> {
            out = it
        }
        -> {
            out = 0
        }
    }
    b i64 = s.byte(1)
    out = b
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n == 0 {
				t.Fatalf("explicit plain annotation on an option-returning call must be rejected, got 0; all: %v", results)
			}
		})
	}
}

// TestPlainTargetOptionCallLegalShapes 是反向守衛：合法的形狀不得被誤報。
// 每一條都對應 option-unified.md 的既有消費路徑或 D1 的鏡像表（標註不在收到
// option 的那一行上）。
func TestPlainTargetOptionCallLegalShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 兩段式解包（§11.5 慣用法）：match 陳句式，臂必須換行分隔。
			name: "two_step_match",
			src: `f = (s str) (out i64) {
    b = s.byte(0)
    out = 0
    b: {
        ok -> {
            out = it
        }
        -> {
            out = 0
        }
    }
}`,
		},
		{
			// 目標本來就是 option：相容比對放行，內容由 match 檢查。
			name: "option_target_declared",
			src: `f = (s str) (out i64) {
    b ?byte = s.byte(0)
    out = 0
    b: {
        ok -> {
            out = it
        }
        -> {
            out = 0
        }
    }
}`,
		},
		{
			// 顯式捨棄：三條消費路徑之一，永遠可用。
			name: "explicit_discard",
			src: `f = (s str) (out i64) {
    _ = s.byte(0)
    out = 0
}`,
		},
		{
			// 純量出參：plain 宣告在簽名上，本行無標註（D1 鏡像表的 result-param 列）。
			name: "result_param_plain_target",
			src: `f = (s str) (out i64) {
    out = s.byte(0).to-i64()
}`,
		},
		{
			// `?=` 上拋：呼叫本身在 `?=` 右側，是要求的顯式解包。
			name: "unwrap_assign_upcast",
			src: `f = (s str) (out ?byte) {
    out ?= s.byte(0)
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n != 0 {
				t.Fatalf("legal shape must not be rejected, got %d: %v", n, results)
			}
		})
	}
}

// TestOptionResultOfCallTargetResolution 釘住升格來源本身：std 簽名表與 Go builtin
// 表的裸名回落都必須解析到 `?byte`，且「模稜兩可的鍵」與「仍帶型別變數的結果」
// （泛型方法的 `?v`）一律不猜。
func TestOptionResultOfCallTargetResolution(t *testing.T) {
	if got := stdMethodOptionResult("str", "byte"); got != "?byte" {
		t.Fatalf("str.byte must resolve to ?byte, got %q", got)
	}
	if got := stdMethodOptionResult("txt", "byte"); got != "?byte" {
		t.Fatalf("txt.byte must resolve to ?byte, got %q", got)
	}
	// 非 option 結果不得升格（否則 plain 目標會被誤報）。
	if got := stdMethodOptionResult("str", "len"); got != "" {
		t.Fatalf("str.len is plain i64 and must not be upgraded, got %q", got)
	}
	// 泛型 map 的 `?v`：元素型別只有工廠函式知道，推斷不出來就不猜。
	if got := stdMethodOptionResult("hashmap-str", "get"); got != "" {
		t.Fatalf("generic ?v must not be resolved, got %q", got)
	}
	// 接收者型別未知 → 不猜。
	if got := optionResultOfCallTarget(nil, nil, ""); got != "" {
		t.Fatalf("nil value must not be upgraded, got %q", got)
	}
}
