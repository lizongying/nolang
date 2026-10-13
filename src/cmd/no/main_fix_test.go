package main

import (
	"os"
	"path/filepath"
	"testing"
)

// fixMainOn 把 src 寫進暫存檔並跑一次 fixMainInFile，回傳修復後源碼。
func fixMainOn(t *testing.T, src string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "m.no")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return fixMainInFile(f)
}

// TestFixMainInFile 回歸：`no fmt --fix=main` 把可省略的 `main = () {…}` 包裹去掉，
// 區塊內每一行（註解/空行）原樣保留並升到頂層。
func TestFixMainInFile(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "basic_hoist",
			src:  "main = () {\n    a()\n    print('x')\n}\n",
			want: "a()\nprint('x')\n",
		},
		{
			name: "helpers_and_comments_kept",
			src:  "f = () {\n    print('f')\n}\n\nmain = () {\n    ; call f\n    f()\n}\n",
			want: "f = () {\n    print('f')\n}\n\n; call f\nf()\n",
		},
		{
			name: "nested_function_return_ignored",
			src:  "main = () {\n    g = () (r i64) {\n        r = 1\n        return\n    }\n    print(g())\n}\n",
			want: "g = () (r i64) {\n    r = 1\n    return\n}\nprint(g())\n",
		},
		{
			// 顯式調用形：`main = () {…}` + 頂層 `main()`，主體升頂層後冗餘呼叫一併刪除。
			name: "hoist_and_drop_main_call",
			src:  "f = () {\n    print('f')\n}\n\nmain = () {\n    f()\n}\nmain()\n",
			want: "f = () {\n    print('f')\n}\n\nf()\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := fixMainOn(t, tc.src)
			if !ok {
				t.Fatalf("expected a fix, got ok=false")
			}
			if got != tc.want {
				t.Errorf("fixed source mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestFixMainSkipGuards 負對照：有結果參數 / 主體直接含 return 的 main 不可省，
// 無 main 的檔案也不應被改動。
func TestFixMainSkipGuards(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "main_with_result",
			src:  "main = () (out i64) {\n    out = 1\n}\n",
		},
		{
			name: "main_with_direct_return",
			src:  "main = () {\n    return\n}\n",
		},
		{
			name: "no_main",
			src:  "print('hello')\n",
		},
		{
			// main 被當值傳遞（非可刪除的 `main()` 呼叫）：安全閘門匹配不成立，跳過。
			name: "main_referenced_as_value",
			src:  "main = () {\n    print('x')\n}\nrun(main)\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := fixMainOn(t, tc.src); ok {
				t.Fatalf("must not rewrite %s", tc.name)
			}
		})
	}
}

// TestFixMainIdempotent 重跑冪等：升頂層後已無可省 main，第二趟不應再改。
func TestFixMainIdempotent(t *testing.T) {
	src := "main = () {\n    a()\n    print('x')\n}\n"
	first, ok := fixMainOn(t, src)
	if !ok {
		t.Fatalf("expected a fix on the first pass, got ok=false")
	}
	if _, ok := fixMainOn(t, first); ok {
		t.Fatalf("second pass must be a no-op")
	}
}
