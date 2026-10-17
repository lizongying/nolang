package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
)

// fixUnwrapOn 把 src 寫進暫存檔並跑一次 fixUnwrapInFile，回傳修復後源碼。
func fixUnwrapOn(t *testing.T, src string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "u.no")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return fixUnwrapInFile(f)
}

// TestFixUnwrapInFile 回歸：`no fmt --fix=unwrap` 把被 `optunuse` 報告的綁定
// 改寫為顯式消費。情況 A（所在函式已有 `?T` 結果參數）→ `x ?= expr`；
// 情況 B（否則）→ `<x>-opt = expr` + 陳述式 match `x-opt: { ok -> x = it -> }`
// （D3/D4/D8；一律陳述式，值上下文會把整段推成 `err | nil`，見
// docs/design/option-unified.md §11 實測邊界 #2）。形態由
// TestFixUnwrapFixedCompiles 以完整前端管線把關。
func TestFixUnwrapInFile(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// 情況 B：plain 函式裡 `x = a + b` 從未被讀取 → 改名 + match 解包。
			name: "case_b_arith",
			src:  "f = (a i64, b i64) (out i64) {\n    x = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    x-opt = a + b\n    x-opt: {\n        ok -> x = it\n        ->\n    }\n    out = 0\n}\n",
		},
		{
			// 情況 A：函式已有 `?i64` 結果參數 → 上拋，只多一個 `?`。
			name: "case_a_propagate",
			src:  "f = (a i64, b i64) (out ?i64) {\n    x = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out ?i64) {\n    x ?= a + b\n    out = 0\n}\n",
		},
		{
			// 情況 B 也覆蓋索引讀取（D6：s[i] / arr[i] 同樣產生 option）。
			name: "case_b_index",
			src:  "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]\n    out = 0\n}\n",
			want: "f = (arr []i64, i i64) (out i64) {\n    v-opt = arr[i]\n    v-opt: {\n        ok -> v = it\n        ->\n    }\n    out = 0\n}\n",
		},
		{
			// tab 縮排沿用 tab。
			name: "tab_indent",
			src:  "f = (a i64, b i64) (out i64) {\n\tx = a + b\n\tout = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n\tx-opt = a + b\n\tx-opt: {\n\t\tok -> x = it\n\t\t->\n\t}\n\tout = 0\n}\n",
		},
		{
			// 顯式 `?T` 標註的綁定同樣是「未消費」，改寫時保留標註（暫存變數承接）。
			name: "explicit_option_annotation_kept",
			src:  "f = (a i64, b i64) (out i64) {\n    x ?i64 = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    x-opt ?i64 = a + b\n    x-opt: {\n        ok -> x = it\n        ->\n    }\n    out = 0\n}\n",
		},
		{
			// 暫存名碰撞時追加序號：`x-opt` 已被佔用 → `x-opt2`。
			name: "temp_name_collision",
			src:  "f = (a i64, b i64) (out i64) {\n    y = a + b\n    x-opt = 7\n    out = x-opt\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    y-opt = a + b\n    y-opt: {\n        ok -> y = it\n        ->\n    }\n    x-opt = 7\n    out = x-opt\n}\n",
		},
		{
			// 跨行陳述：`EndPos().Line` 有時指到陳述之後，續行必須被「吃掉」而
			// 不是重複輸出（此處曾把末尾的 `2` 印出兩次）。
			name: "multiline_statement_continuation_consumed",
			src:  "d = 1 +\n2\n",
			want: "d-opt = 1 +\n2\nd-opt: {\n    ok -> d = it\n    ->\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := fixUnwrapOn(t, tc.src)
			if !ok {
				t.Fatalf("expected a fix, got ok=false")
			}
			if got != tc.want {
				t.Errorf("fixed source mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestFixUnwrapInFileIdempotent 重跑冪等：消費後不再有 optunuse，第二趟不應再改。
func TestFixUnwrapInFileIdempotent(t *testing.T) {
	src := "f = (a i64, b i64) (out i64) {\n    x = a + b\n    out = 0\n}\n"
	first, ok := fixUnwrapOn(t, src)
	if !ok {
		t.Fatalf("expected a fix on the first pass, got ok=false")
	}
	second, ok := fixUnwrapOn(t, first)
	if ok {
		t.Fatalf("second pass must be a no-op, got ok=true with:\n%s", second)
	}
}

// TestFixUnwrapInFileNoReportNoChange 負對照：option 已被消費（被讀取）時不應
// 有任何改寫——`--fix=unwrap` 只動 `optunuse` 站點，不是通用重排器。
func TestFixUnwrapInFileNoReportNoChange(t *testing.T) {
	src := "f = (a i64, b i64) (out i64) {\n    x ?i64 = a + b\n    out = x: {\n        ok -> it\n        -> 0\n    }\n}\n"
	if _, ok := fixUnwrapOn(t, src); ok {
		t.Fatalf("consumed option must not be rewritten")
	}
}

// TestFixUnwrapDiscardExempt 負對照：`_ = expr`（顯式丟棄）豁免 optunuse，
// 不應被改寫。
func TestFixUnwrapDiscardExempt(t *testing.T) {
	src := "f = (a i64, b i64) (out i64) {\n    _ = a + b\n    out = 0\n}\n"
	if _, ok := fixUnwrapOn(t, src); ok {
		t.Fatalf("`_ = expr` (explicit discard) must not be rewritten")
	}
}

// TestFixUnwrapPlainTypedOptionBinding：15w45dqk 站點——option 被顯式綁定到
// **plain** 型別變數（`k i64 = a + b`）。依「option 絕不能窄化成 plain」的鐵律，
// 修法是先剝掉那枚 plain 型別標註（推斷為 ?T），再走情況 A/B 消費。有無
// #{overflow=…} 註解皆然（D1：註解只改行為、不解包）。對照：
// explicit_option_annotation_kept 釘住合法 `?T` 標註會被保留（plain→option 允許）。
func TestFixUnwrapPlainTypedOptionBinding(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// 情況 B：plain 函式。`k i64 = a + b` → 剝註解 + match 解包。
			// k 是新鲜绑定（本函式內未宣告過）→ 必須預宣告 `k i64 = 0`，
			// 否則臂內的 `k = it` 只建出臂作用域變數，函式後續看不到。
			name: "case_b_strip_plain_annotation",
			src:  "f = (a i64, b i64) (out i64) {\n    k i64 = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    k-opt = a + b\n    k i64 = 0\n    k-opt: {\n        ok -> k = it\n        ->\n    }\n    out = 0\n}\n",
		},
		{
			// 情況 A：函式已有 `?i64` 結果參數 → 剝註解後上拋 `k ?= a + b`。
			name: "case_a_strip_plain_annotation",
			src:  "f = (a i64, b i64) (out ?i64) {\n    k i64 = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out ?i64) {\n    k ?= a + b\n    out = 0\n}\n",
		},
		{
			// 帶 #{overflow=wrap}：註解留在算術行（行為不變），仍剝 plain 標註。
			name: "case_b_overflow_annotation_stripped",
			src:  "f = (a i64, b i64) (out i64) {\n    #{overflow=wrap}\n    k i64 = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    #{overflow=wrap}\n    k-opt = a + b\n    k i64 = 0\n    k-opt: {\n        ok -> k = it\n        ->\n    }\n    out = 0\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := fixUnwrapOn(t, tc.src)
			if !ok {
				t.Fatalf("expected a fix, got ok=false")
			}
			if got != tc.want {
				t.Errorf("fixed source mismatch\n got: %q\nwant: %q", got, tc.want)
			}
			// 判別力護欄：plain 標註絕不能殘留在暫存變數上（`-opt i64`）。
			if strings.Contains(got, "-opt i64") {
				t.Errorf("plain annotation must be stripped from the temp binding, got:\n%s", got)
			}
		})
	}
}

// TestFixUnwrapPlainTypedIdempotent：15w45dqk 剝註解+消費後重跑不再改。
func TestFixUnwrapPlainTypedIdempotent(t *testing.T) {
	src := "f = (a i64, b i64) (out i64) {\n    k i64 = a + b\n    out = 0\n}\n"
	first, ok := fixUnwrapOn(t, src)
	if !ok {
		t.Fatalf("expected a fix on the first pass, got ok=false")
	}
	if strings.Contains(first, "k-opt i64") {
		t.Fatalf("plain annotation must be stripped, got:\n%s", first)
	}
	second, ok := fixUnwrapOn(t, first)
	if ok {
		t.Fatalf("second pass must be a no-op, got ok=true with:\n%s", second)
	}
}

// TestFixUnwrapPlainMismatchNotTouched 負對照：plain → plain 的型別不符
// （i64 → str）同樣掛 15w45dqk，但非 option 消費問題，絕不能剝註解 + match
// （那會把非 option 值餵進 match 製造壞碼）。guard 只認訊息含 "assign ?" 的
// option → plain 站點。
func TestFixUnwrapPlainMismatchNotTouched(t *testing.T) {
	src := "f = (n i64) (out str) {\n    acc str = n\n    out = acc\n}\n"
	if _, ok := fixUnwrapOn(t, src); ok {
		t.Fatalf("plain->plain 15w45dqk (i64->str) must not be rewritten")
	}
}

// TestFixUnwrapModuleCallSite：模組限定 std 呼叫的結果型別只有完整 vet 管線
// 能推出來（`c = fs.read-str(path)` → ?str）。站點收集早年用裸解析，這類
// 站點整站漏報（實測 shs-check.no 的 `src = c` 就是漏網的那一個）；改用
// VetFileWithLints 後必須能抓到並改寫。此測試即釘住該回歸。
func TestFixUnwrapModuleCallSite(t *testing.T) {
	src := "f = (path str) (out i64) {\n" +
		"    src = ''\n" +
		"    c = fs.read-str(path)\n" +
		"    c: {\n" +
		"        ok -> {\n" +
		"            src = c\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"        }\n" +
		"    }\n" +
		"    out = 0\n" +
		"}\n"
	want := "f = (path str) (out i64) {\n" +
		"    src = ''\n" +
		"    c = fs.read-str(path)\n" +
		"    c: {\n" +
		"        ok -> {\n" +
		"            src-opt = c\n" +
		"            src-opt: {\n" +
		"                ok -> src = it\n" +
		"                ->\n" +
		"            }\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"        }\n" +
		"    }\n" +
		"    out = 0\n" +
		"}\n"
	got, ok := fixUnwrapOn(t, src)
	if !ok {
		t.Fatalf("expected a fix for the module-call site, got ok=false")
	}
	if got != want {
		t.Errorf("fixed source mismatch\n got: %q\nwant: %q", got, want)
	}
	// 第二趟冪等：消費後不再有 15w45dqk。
	if _, ok := fixUnwrapOn(t, got); ok {
		t.Fatalf("second pass must be a no-op")
	}
}

// fixUnwrapCompiles 斷言修復後的源碼能通過完整前端管線（語法+型別+模組合併+
// 單態化），且沒有任何 error 級診斷。早先工具曾產出「值上下文 match」形態
// （`x = x-opt: { ok -> it }`），語法層面合法、管線層面卻炸
// `6fgg3htw expected 'i64', got 'err | nil'`——只有跑真實管線才攔得住，
// 故每個代表形態都必須過此關（依賴檔診斷依 File 歸屬過濾，同 fixUnwrapInFile）。
func fixUnwrapCompiles(t *testing.T, name, src string) string {
	t.Helper()
	fixed, ok := fixUnwrapOn(t, src)
	if !ok {
		t.Fatalf("%s: expected a fix, got ok=false", name)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "c.no")
	if err := os.WriteFile(f, []byte(fixed), 0644); err != nil {
		t.Fatal(err)
	}
	lints, err := nbuild.VetFileWithLints(f, nbuild.BuildOptions{})
	if err != nil {
		t.Fatalf("%s: vet pipeline error: %v\nfixed source:\n%s", name, err, fixed)
	}
	for _, l := range lints {
		if l.Severity != checker.LintError {
			continue
		}
		if l.File != "" {
			if abs, aerr := filepath.Abs(l.File); aerr != nil || abs != f {
				continue // 依賴檔（std / 本地模組）的診斷不屬於本檔
			}
		}
		t.Errorf("%s: fixed source still has error lint L%d [%s] %s\nfixed source:\n%s",
			name, l.Line, l.TraceID, l.Message, fixed)
	}
	return fixed
}

// TestFixUnwrapFixedCompiles 把關「改寫結果真的能編過」：覆蓋情況 B 的各代表
// 形態（算術／索引／顯式 ?T 標註／新鮮綁定預宣告／overflow 註解／模組呼叫
// 站點／頂層多行）。值上下文形態就是在此類把關下被抓到的（6fgg3htw）。
func TestFixUnwrapFixedCompiles(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"case_b_arith", "f = (a i64, b i64) (out i64) {\n    x = a + b\n    out = 0\n}\n"},
		{"case_b_index", "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]\n    out = 0\n}\n"},
		{"explicit_option_annotation", "f = (a i64, b i64) (out i64) {\n    x ?i64 = a + b\n    out = 0\n}\n"},
		{"typed_fresh_predeclare", "f = (a i64, b i64) (out i64) {\n    k i64 = a + b\n    out = 0\n}\n"},
		{"typed_overflow_annotation", "f = (a i64, b i64) (out i64) {\n    #{overflow=wrap}\n    k i64 = a + b\n    out = 0\n}\n"},
		{"module_call_site", "f = (path str) (out i64) {\n    src = ''\n    c = fs.read-str(path)\n    c: {\n        ok -> {\n            src = c\n        }\n\n        -> {\n        }\n    }\n    out = 0\n}\n"},
		// S4：報告點在 match 臂內的兩個形態（糖形態 SurfaceMatch 下鑽的路徑）。
		// 沒有 collectUnwrapSites 的 SurfaceMatch 分支，這兩例在糖形態下靜默漏修。
		{"arm_block_index", "f = (arr []i64, i i64) (out i64) {\n    1: {\n        ok -> {\n            w = arr[i]\n            z = 0\n        }\n        -> z = 0\n    }\n    out = 0\n}\n"},
		{"arm_nested_match", "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]\n    v: {\n        ok -> {\n            u = arr[i]\n            z = 0\n        }\n        -> z = 0\n    }\n    out = 0\n}\n"},
		{"top_level_multiline", "d = 1 +\n2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixUnwrapCompiles(t, c.name, c.src)
		})
	}
}
