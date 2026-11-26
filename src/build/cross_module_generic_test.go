//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGenericWorkspace 建立一個最小工作區：test 包以裸導出導出一個含
// 隱式泛型函式 eq 的模組，入口檔用 importPath 引入它。
func writeGenericWorkspace(t *testing.T, importPath string) string {
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
	write("workspace.noml", "{\n  test: '/test'\n}\n")
	write("test/lib.no", "@ /test/src/assert\n")
	write("test/src/assert.no", `; 隱式泛型斷言（t 由呼叫點推斷）
eq = (a t, b t, msg str) (ok bool) {
    {
        a != b -> {
            ok = false
            print('FAIL: eq')
            print(msg)
        }

        -> ok = true
    }
}
`)
	write("main.no", importPath+"\nok = assert.eq(1, 1, 'generic cross-module')\nprint(ok)\n")
	return ws
}

// TestCrossModuleGenericCall 釘住泛型函式跨模組呼叫在完成import/export 路徑解析後
// 仍能通過單態化與 codegen。
//
// 回歸點：
//   - checker 對形參含未綁定型別變數（`expected 't'`）誤報 6fgg3htw；
//   - monomorphizeGenerics 只認 Identifier 形式的呼叫，跨模組 `assert.eq` 是
//     DotExpression，且合併後泛型定義是裸名 `eq`，導致單態化沒發生、
//     codegen 報 `unknown callee assert.eq`。
func TestCrossModuleGenericCall(t *testing.T) {
	for _, tc := range []struct {
		name       string
		importPath string
	}{
		{"workspace-relative", "# /test/src/assert"},
		{"package-direct", "# test/src/assert"},
		{"package-export-alias", "# test/assert"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := writeGenericWorkspace(t, tc.importPath)
			if _, err := BuildFile(filepath.Join(ws, "main.no"), BuildOptions{CheckOnly: true}); err != nil {
				t.Fatalf("BuildFile failed: %v", err)
			}
		})
	}
}
