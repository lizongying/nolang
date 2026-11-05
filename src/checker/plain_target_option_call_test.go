package checker

import (
	"strings"
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
// 🔴 兩種「呼叫」形狀（2026-11-05 補上第二種）：
//   1. **方法呼叫** `recv.method(...)`——接收者型別已知（stdMethodOptionResult）；
//   2. **模組限定呼叫** `mod.fn(...)`（如 `os.arg(...)`）——接收者是模組名而非變數，
//      型別推不出來，改查 std 簽名表的**模組限定鍵**（stdFuncOptionResult）。
//      少了第 2 條，`x str = os.arg(i + 1)` 這種「option 當值傳遞」會被整條型別
//      比對跳過而**靜默拆包**（見 TestPlainTargetRejectsModuleQualifiedOptionCall）。
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

// 🔴 模組限定 std 呼叫（2026-11-05 新增）——使用者回報的形狀：
//
//	x str = os.arg(i + 1)
//
// `os.arg` 的回傳型別是 `?str`（越界回 nil，見 src/std/os.no）。把它指派給一個
// **plain** 的 `str` 變數就是「option 當值傳遞」：option 的 payload 被讀出來存進
// plain 變數，tag（ok / nil）被丟掉，越界時拿到的是空字串而不是錯誤——而且
// `no vet` / `no build` **一句話都不說**（靜默拆包）。
//
// 為什麼之前沒報：`os.arg(i + 1)` 在 AST 裡是
// `CallExpression{Function: DotExpression{Property:"arg", Receiver: Identifier("os")}}`，
// 接收者是**模組名**而不是變數。方法路徑（optionResultOfCallTarget →
// stdMethodOptionResult）要的是「接收者型別 → 方法名」，模組名推不出型別，
// 於是升格結果是 ""，整條型別比對被 `inferredType != ""` 守衛跳過。
//
// 修法（checker.go 的 optionResultOfCallTarget + funcargs.go 的
// stdFuncOptionResult）：模組限定的接收者改查 **std 簽名表的模組限定鍵**
// （`os.arg` → `?str`）。用模組限定鍵而不是 Go builtin 表的裸名（"arg"），
// 是為了不與另一個模組的同名函式（`foo.arg`）相撞。
//
// 🔴 執行前提：與本檔既有的案例相同，std 簽名表要能解析，因此 `go test
// ./checker/` 必須帶 StdSigKey（見本檔檔頭）。
//
// 🔴 界線（刻意不報，與 D1 鏡像表逐字對稱）：**重指派**與**結果參數重綁定**不在
// 本規則的射程內——
//
//	x str = ''; x = os.arg(i + 1)   ; 標註在**前一行** → 刻意不報（D1 鏡像表）
//	f = (r str) { r = os.arg(i) }   ; 結果參數同上     → 刻意不報（D1 鏡像表）
//
// 這不是遺漏而是承載：std 自己就靠它——src/std/path.no 的
// `path.size = () (sz i64) { sz = fs.stat-size(.p) }` 正是「plain 結果參數接住
// `?i64`」。方法呼叫那條路徑（`b = s.byte(0)`）也是同樣的待遇，所以模組限定與
// 方法兩條路徑的行為一致。要把這一格也變成 ERROR，必須同時動 lowering 的鏡像表
// （captureTargetAllowsOption），不是單改 checker 能決定的。
func TestPlainTargetRejectsModuleQualifiedOptionCall(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 使用者回報的形狀：模組限定的 std 內建回傳 `?str`，目標是 plain str。
			name: "os_arg_into_str",
			src: `f = (i i64) (out i64) {
    x str = os.arg(i + 1)
    out = 0
}`,
		},
		{
			// 同一個內建，索引是字面量：引數形狀不同，回傳型別仍是 `?str`。
			name: "os_arg_literal_index_into_str",
			src: `f = () (out i64) {
    x str = os.arg(0)
    out = 0
}`,
		},
		{
			// 另一個模組限定的 option 回傳（`os.get-env` → `?str`）：規則必須是
			// 「回傳 option 的模組限定呼叫」，不是「os.arg 專屬」。
			name: "os_get_env_into_str",
			src: `f = () (out i64) {
    x str = os.get-env('HOME')
    out = 0
}`,
		},
		{
			// std 模組自己的 wrapper（`args.get` → `?str`）走同一條路徑。
			name: "args_get_into_str",
			src: `f = () (out i64) {
    x str = args.get(0)
    out = 0
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateTypes(parseProg(t, c.src))
			if n := countTrace(results, "15w45dqk"); n == 0 {
				t.Fatalf("a plain target must not silently receive an option-returning module-qualified call, got 0; all: %v", results)
			}
		})
	}
}

// TestPlainTargetOptionCallMessage 釘住診斷**本身**的內容：使用者看到的必須是
// 「為什麼錯」加上「怎麼改」，而不是一句乾巴巴的型別不符。
//
// 兩段文字都必須在：
//   - 具體原因：option 不能被隱含解包（這只是複製，失敗分支被丟掉）；
//   - 可行修法：兩段式解包 / match 陳述 / `?=` 上拋 / `_ =` 捨棄。
func TestPlainTargetOptionCallMessage(t *testing.T) {
	src := `f = (i i64) (out i64) {
    x str = os.arg(i + 1)
    out = 0
}`
	results := ValidateTypes(parseProg(t, src))
	var msg string
	for _, r := range results {
		if r.TraceID == "15w45dqk" {
			msg = r.Message
			break
		}
	}
	if msg == "" {
		t.Fatalf("expected a 15w45dqk diagnostic, got: %v", results)
	}
	for _, want := range []string{
		"cannot assign ?str value to str variable 'x'",
		"option cannot be unwrapped implicitly",
		"two-step unwrap",
		"match statement",
		"?=",
		"_ =",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic must mention %q, got: %s", want, msg)
		}
	}
}

// TestModuleQualifiedOptionCallLegalShapes 是反向守衛：模組限定的 option 回傳
// 呼叫在**合法**的消費形狀下不得被誤報。這些形狀與 option-unified.md 的既有消費
// 路徑一一對應（選項目標 / 顯式捨棄 / `?=` 上拋 / 新綁定推斷成 option / match
// 消費）。
func TestModuleQualifiedOptionCallLegalShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 目標本來就是 option：相容比對放行，內容由 match 檢查。
			name: "option_target_declared",
			src: `f = (i i64) (out i64) {
    x ?str = os.arg(i + 1)
    out = 0
    x: {
        ok -> {
            out = 1
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
			src: `f = (i i64) (out i64) {
    _ = os.arg(i + 1)
    out = 0
}`,
		},
		{
			// `?=` 上拋：呼叫本身在 `?=` 右側，是要求的顯式解包。
			name: "unwrap_assign_upcast",
			src: `f = (i i64) (out ?str) {
    out ?= os.arg(i)
}`,
		},
		{
			// 新綁定、本行無標註 → 綁定本身就是 option（`x` 是 `?str`），由 match
			// 消費。這是 D1 鏡像表的「合法」列。
			name: "fresh_binding_inferred_option",
			src: `f = (i i64) (out i64) {
    x = os.arg(i + 1)
    out = 0
    x: {
        ok -> {
            out = 1
        }
        -> {
            out = 0
        }
    }
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

// TestStdFuncOptionResultResolution 釘住模組限定升格來源本身：簽名表的模組限定鍵
// 必須解析到 option 結果，且非 option 結果與未知模組一律不猜。
func TestStdFuncOptionResultResolution(t *testing.T) {
	if got := stdFuncOptionResult("os", "arg"); got != "?str" {
		t.Fatalf("os.arg must resolve to ?str, got %q", got)
	}
	if got := stdFuncOptionResult("args", "get"); got != "?str" {
		t.Fatalf("args.get must resolve to ?str, got %q", got)
	}
	// 非 option 結果不得升格（否則 plain 目標會被誤報）。
	if got := stdFuncOptionResult("os", "get-pid"); got != "" {
		t.Fatalf("os.get-pid is plain i64 and must not be upgraded, got %q", got)
	}
	// 未知模組 / 未知函式 → 不猜。
	if got := stdFuncOptionResult("no-such-mod", "arg"); got != "" {
		t.Fatalf("unknown module must not be resolved, got %q", got)
	}
	if got := stdFuncOptionResult("os", "no-such-fn"); got != "" {
		t.Fatalf("unknown function must not be resolved, got %q", got)
	}
	// 模組限定鍵必須與另一個模組的同名函式分開：`foo.arg` 不是 `os.arg`。
	if got := stdFuncOptionResult("foo", "arg"); got != "" {
		t.Fatalf("foo.arg must not resolve to os.arg's ?str, got %q", got)
	}
}
