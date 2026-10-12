package main

import (
	"os"
	"path/filepath"
	"testing"
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
// 情況 B（否則）→ `<x>-opt = expr` + `x = <x>-opt: { ok -> it -> }`（D3/D4/D8）。
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
			want: "f = (a i64, b i64) (out i64) {\n    x-opt = a + b\n    x = x-opt: {\n        ok -> it\n        ->\n    }\n    out = 0\n}\n",
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
			want: "f = (arr []i64, i i64) (out i64) {\n    v-opt = arr[i]\n    v = v-opt: {\n        ok -> it\n        ->\n    }\n    out = 0\n}\n",
		},
		{
			// tab 縮排沿用 tab。
			name: "tab_indent",
			src:  "f = (a i64, b i64) (out i64) {\n\tx = a + b\n\tout = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n\tx-opt = a + b\n\tx = x-opt: {\n\t\tok -> it\n\t\t->\n\t}\n\tout = 0\n}\n",
		},
		{
			// 顯式 `?T` 標註的綁定同樣是「未消費」，改寫時保留標註（暫存變數承接）。
			name: "explicit_option_annotation_kept",
			src:  "f = (a i64, b i64) (out i64) {\n    x ?i64 = a + b\n    out = 0\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    x-opt ?i64 = a + b\n    x = x-opt: {\n        ok -> it\n        ->\n    }\n    out = 0\n}\n",
		},
		{
			// 暫存名碰撞時追加序號：`x-opt` 已被佔用 → `x-opt2`。
			name: "temp_name_collision",
			src:  "f = (a i64, b i64) (out i64) {\n    y = a + b\n    x-opt = 7\n    out = x-opt\n}\n",
			want: "f = (a i64, b i64) (out i64) {\n    y-opt = a + b\n    y = y-opt: {\n        ok -> it\n        ->\n    }\n    x-opt = 7\n    out = x-opt\n}\n",
		},
		{
			// 跨行陳述：`EndPos().Line` 有時指到陳述之後，續行必須被「吃掉」而
			// 不是重複輸出（此處曾把末尾的 `2` 印出兩次）。
			name: "multiline_statement_continuation_consumed",
			src:  "d = 1 +\n2\n",
			want: "d-opt = 1 +\n2\nd = d-opt: {\n    ok -> it\n    ->\n}\n",
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
