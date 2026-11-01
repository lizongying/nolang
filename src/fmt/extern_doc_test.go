package fmt

import (
	"strings"
	"testing"
)

// TestFormatPreservesFFIDocComments 是 example/sqlite-driver 回歸測試：
// no fmt 不得刪除 `#{c}` FFI 宣告上方的 Doc 註釋。修復前 setDoc 不認得
// *ExternStatement，導致這些註釋在格式化時整組遺失。
func TestFormatPreservesFFIDocComments(t *testing.T) {
	src := "; FFI open: open or create database\n" +
		"; filename: db path\n" +
		"#{c}\n" +
		"_open = (filename str) (rc i32)\n" +
		"\n" +
		"; FFI close: close connection\n" +
		"#{c}\n" +
		"_close = (db *byte) (rc i32)\n"

	out := Format(src)

	for _, want := range []string{
		"; FFI open: open or create database",
		"; filename: db path",
		"; FFI close: close connection",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fmt deleted leading doc comment %q\n--- output ---\n%s", want, out)
		}
	}

	// 冪等：再次格式化不得改變已保留的註釋。
	out2 := Format(out)
	if out2 != out {
		t.Fatalf("fmt not idempotent for FFI doc comments\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}
