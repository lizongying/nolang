package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixIndexOutOn 把 src 寫進暫存檔並跑一次 fixIndexOutInFile，回傳修復後源碼。
func fixIndexOutOn(t *testing.T, src string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "io.no")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return fixIndexOutInFile(f)
}

// TestFixIndexOutInFile 回歸：`no fmt --fix=index-out` 把帶 `#{index-out=DEF}` 的安全
// 索引讀取賦值改寫為顯式 matched-match 語句（subject 為索引表達式、綁定寫在 arm 內）。
// 等價於編譯器 maybeIndexOutAssign 的脫糖結果，只是把「隱式」變「顯式」並移除註解。
// `=zero` 按元素型別產生具體零值字面量（str→''、i64→0、[]T→[]；結構體 Name {} 跳過）；
// char / 定長陣列 / 型別不可推斷 / 非直接變數基底（.field[i]）/ 子表達式索引則安全跳過。
func TestFixIndexOutInFile(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// 正向用例：want 為期望輸出（精確比對）；負向用例：wantOK=false 表示不應改寫。
		want     string
		wantOK   bool
	}{
		{
			// str 元素：=zero → ''。
			name:   "str_zero",
			src:    "f = (lines1 []str, i i64) (out str) {\n    #{index-out=zero}\n    s = lines1[i]\n    out = s\n}\n",
			want:   "f = (lines1 []str, i i64) (out str) {\n    s = lines1[i]: {\n        ok -> it\n\n        -> ''\n    }\n    out = s\n}\n",
			wantOK: true,
		},
		{
			// i64 元素：=zero → 0。
			name:   "i64_zero",
			src:    "f = (arr []i64, i i64) (out i64) {\n    #{index-out=zero}\n    v = arr[i]\n    out = v\n}\n",
			want:   "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]: {\n        ok -> it\n\n        -> 0\n    }\n    out = v\n}\n",
			wantOK: true,
		},
		{
			// bool 元素：=zero → false。
			name:   "bool_zero",
			src:    "f = (fl []bool, i i64) (out bool) {\n    #{index-out=zero}\n    b = fl[i]\n    out = b\n}\n",
			want:   "f = (fl []bool, i i64) (out bool) {\n    b = fl[i]: {\n        ok -> it\n\n        -> false\n    }\n    out = b\n}\n",
			wantOK: true,
		},
		{
			// 切片元素 [][]i64 → []（空切片字面量）。
			name:   "slice_zero",
			src:    "f = (a [][]i64, i i64) (out []i64) {\n    #{index-out=zero}\n    x = a[i]\n    out = x\n}\n",
			want:   "f = (a [][]i64, i i64) (out []i64) {\n    x = a[i]: {\n        ok -> it\n\n        -> []\n    }\n    out = x\n}\n",
			wantOK: true,
		},
		{
			// 結構體元素 =zero → `diff-op {}`：**跳過不改寫**。值語境的
			// `-> Diff-op {}` 會被解析成 block（空大括號不是 struct literal），
			// no 報 "match arm in expression context must end with an expression"。
			name:   "struct_zero_skipped",
			src:    "f = (ops []diff-op, i i64) (out diff-op) {\n    #{index-out=zero}\n    o = ops[i]\n    out = o\n}\n",
			wantOK: false,
		},
		{
			// 显式型别标注保留：`av i64 = a[i]`（a 为 []byte，element 是回绕型 byte）
			// 必须保留 `i64`，否则目标被推成 byte，大数算术静默 mod 256。
			name:   "explicit_type_preserved",
			src:    "f = (a []byte, i i64) (out i64) {\n    #{index-out=zero}\n    av i64 = a[i]\n    out = av\n}\n",
			want:   "f = (a []byte, i i64) (out i64) {\n    av i64 = a[i]: {\n        ok -> it\n\n        -> 0\n    }\n    out = av\n}\n",
			wantOK: true,
		},
		{
			// 顯式字面量 =5 直接複用（不改寫）。
			name:   "explicit_literal",
			src:    "f = (arr []i64, i i64) (out i64) {\n    #{index-out=5}\n    v = arr[i]\n    out = v\n}\n",
			want:   "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]: {\n        ok -> it\n\n        -> 5\n    }\n    out = v\n}\n",
			wantOK: true,
		},
		{
			// 複合註解：剝離 index-out，保留兄弟 overflow。
			name:   "compound_annotation_keeps_sibling",
			src:    "f = (arr []i64, i i64) (out i64) {\n    #{index-out=zero, overflow=wrap}\n    v = arr[i]\n    out = v\n}\n",
			want:   "f = (arr []i64, i i64) (out i64) {\n    #{overflow=wrap}\n    v = arr[i]: {\n        ok -> it\n\n        -> 0\n    }\n    out = v\n}\n",
			wantOK: true,
		},
		{
			// char 元素 =zero：零值歧義，安全跳過（不應改寫）。
			name:   "char_zero_skipped",
			src:    "f = (cs []char, i i64) (out char) {\n    #{index-out=zero}\n    c = cs[i]\n    out = c\n}\n",
			wantOK: false,
		},
		{
			// .field[i] 非直接變數基底：跳過（不應改寫）。
			name:   "dot_base_skipped",
			src:    "f = (obj objtype, i i64) (out i64) {\n    #{index-out=zero}\n    v = obj.field[i]\n    out = v\n}\n",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := fixIndexOutOn(t, tc.src)
			if tc.wantOK {
				if !ok {
					t.Fatalf("expected a fix, got ok=false\n got: %q", got)
				}
				if got != tc.want {
					t.Errorf("fixed source mismatch\n got: %q\nwant: %q", got, tc.want)
				}
				if strings.Contains(got, "index-out") {
					t.Errorf("annotation should be removed, but output still contains it:\n%s", got)
				}
			} else {
				if ok {
					t.Errorf("expected NO fix for unsafe site, but got a change:\n%s", got)
				}
				if got != tc.src {
					t.Errorf("unsafe site should be left untouched, but changed:\n got: %q\nwant: %q", got, tc.src)
				}
			}
		})
	}
}
