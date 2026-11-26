package checker

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestWorkspace 在測試用工作區裡放一個 workspace.noml 映射與一個包。
// libSource 由各案例提供，用來區分裸導出、顯式別名與多段別名。
func writeTestWorkspace(t *testing.T, libSource string) string {
	t.Helper()
	ws := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workspace.noml", "{\n  test: '/foo'\n}\n")
	write("foo/lib.no", libSource)
	write("foo/src/assert.no", "check = (v i64) (ok bool) {\n    {\n        v == 7 -> ok = true\n        -> ok = false\n    }\n}\n")
	return ws
}

// TestResolvePackageExport 釘住 `# test/assert` 的三種 lib.no 寫法與
// `# test/src/assert` 的直接路徑都解析到同一個模組檔案。
func TestResolvePackageExport(t *testing.T) {
	cases := []struct {
		name       string
		libSource  string
		importPath string
	}{
		{"bare export default alias", "@ /foo/src/assert\n", "test/assert"},
		{"explicit alias", "@ /foo/src/assert assert\n", "test/assert"},
		{"multi-segment alias", "@ /foo/src/assert test/assert\n", "test/assert"},
		{"direct package path", "@ /foo/src/assert\n", "test/src/assert"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := writeTestWorkspace(t, tc.libSource)
			got := ResolvePackageExport(tc.importPath, ws)
			want := filepath.Join(ws, "foo", "src", "assert.no")
			if got != want {
				t.Fatalf("ResolvePackageExport(%q) = %q, want %q", tc.importPath, got, want)
			}
		})
	}
}

// TestResolvePackageExportRejectsNonModuleExport 確認 `@ path.fn alias` 是符號
// 匯出，不能被當成模組別名解析（否則 `# test/assert.eq` 會載入錯誤模組）。
func TestResolvePackageExportRejectsNonModuleExport(t *testing.T) {
	ws := writeTestWorkspace(t, "@ /foo/src/assert.check assert\n")
	if got := ResolvePackageExport("test/assert", ws); got != "" {
		t.Fatalf("symbol export must not resolve as module alias, got %q", got)
	}
}

// TestResolvePackageExportNoAlias 確認非工作區別名（如一般 URL 風格導入）
// 不會被此解析器攔截，交由原有的依賴解析鏈處理。
func TestResolvePackageExportNoAlias(t *testing.T) {
	ws := writeTestWorkspace(t, "@ /foo/src/assert\n")
	for _, p := range []string{"github.com/x/y", "assert", "std/err"} {
		if got := ResolvePackageExport(p, ws); got != "" {
			t.Errorf("ResolvePackageExport(%q) = %q, want empty", p, got)
		}
	}
	if got := ResolvePackageExport("test/assert", ""); got != "" {
		t.Errorf("empty workspace root must not resolve, got %q", got)
	}
	if got := ResolvePackageExport("test/assert", filepath.Join(ws, "does-not-exist")); got != "" {
		t.Errorf("nonexistent workspace root must not resolve, got %q", got)
	}
}
