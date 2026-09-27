package main

import (
	"os"
	"path/filepath"
	"testing"
)

// optSrc 是「被 checker 判為非窮盡」的真實形狀：match 主體來自函式回傳值，
// 靜態型別未知，故 parser 的 [RAL] 檢查不報、由 checker 補報。
// 刻意**不**用 `v ?i64 = ...`：`matchedIsOption` 會認出它，parser 當場要求
// ok/nil/err 三臂齊備，源碼根本解析不過（修復器只處理能解析的檔案）。
const optSrc = "get-v = () (r ?i64) {\n    r = 1\n}\n"

// fixMatchArmsOn 把 src 寫進暫存檔並跑一次 fixMatchArmsInFile，回傳修復後源碼。
func fixMatchArmsOn(t *testing.T, src string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "m.no")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return fixMatchArmsInFile(f)
}

// TestFixMatchArmsInFile 回歸：`no fmt --fix=match` 只在被
// checker.CollectNonExhaustiveMatches 判為非窮盡的 option match 的收尾 `}` 之前
// 插入缺失的 arm（`nil -> print('nil')` / `err -> print('err')`），且沿用檔案
// 既有的縮排單位——不做重排、不改既有 arm。
func TestFixMatchArmsInFile(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "missing nil and err",
			src: "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n    }\n}\n" + optSrc,
			want: "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n\n        nil -> print('nil')\n\n        err -> print('err')\n    }\n}\n" + optSrc,
		},
		{
			name: "missing err only",
			src: "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n\n        nil -> print('nil')\n    }\n}\n" + optSrc,
			want: "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n\n        nil -> print('nil')\n\n        err -> print('err')\n    }\n}\n" + optSrc,
		},
		{
			name: "nested match keeps its own indentation",
			src: "main = () {\n    v = get-v()\n    v: {\n        ok -> {\n            w = get-v()\n            w: {\n                ok -> print(w)\n            }\n        }\n    }\n}\n" + optSrc,
			want: "main = () {\n    v = get-v()\n    v: {\n        ok -> {\n            w = get-v()\n            w: {\n                ok -> print(w)\n\n                nil -> print('nil')\n\n                err -> print('err')\n            }\n        }\n\n        nil -> print('nil')\n\n        err -> print('err')\n    }\n}\n" + optSrc,
		},
		{
			name: "tab indentation is preserved",
			src: "main = () {\n\tv = get-v()\n\tv: {\n\t\tok -> print(v)\n\t}\n}\n" + optSrc,
			want: "main = () {\n\tv = get-v()\n\tv: {\n\t\tok -> print(v)\n\n\t\tnil -> print('nil')\n\n\t\terr -> print('err')\n\t}\n}\n" + optSrc,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := fixMatchArmsOn(t, tc.src)
			if !ok {
				t.Fatalf("expected a fix, got ok=false")
			}
			if got != tc.want {
				t.Errorf("fixed source mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestFixMatchArmsInFileIdempotent 重跑冪等：補齊後 match 已窮盡，不再被報告。
func TestFixMatchArmsInFileIdempotent(t *testing.T) {
	src := "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n    }\n}\n" + optSrc
	once, ok := fixMatchArmsOn(t, src)
	if !ok {
		t.Fatalf("expected a fix on the first pass")
	}
	if twice, ok := fixMatchArmsOn(t, once); ok || twice != "" {
		t.Fatalf("second pass must be a no-op, got ok=%v src=%q", ok, twice)
	}
}

// TestFixMatchArmsInFileLeavesExhaustiveAlone 已窮盡的三臂 match、含 catch-all 的
// match、以及 tagged enum match（不是 option match）都必須原樣保留（ok=false）。
func TestFixMatchArmsInFileLeavesExhaustiveAlone(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "all three variants",
			src:  "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n\n\n        nil -> print('nil')\n\n\n        err -> print('err')\n    }\n}\n" + optSrc,
		},
		{
			name: "wildcard catch-all",
			src:  "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n\n        -> print('other')\n    }\n}\n" + optSrc,
		},
		{
			name: "tagged enum match",
			src:  "e-res {\n    ok(v str),\n    fail,\n}\nshow = (q e-res) {\n    q: {\n        ok(v) -> print(v)\n\n        fail -> print('fail')\n    }\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := fixMatchArmsOn(t, tc.src); ok {
				t.Fatalf("expected no fix for %s, got %q", tc.name, got)
			}
		})
	}
}
