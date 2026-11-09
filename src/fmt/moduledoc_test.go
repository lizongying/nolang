package fmt

import (
	"strings"
	"testing"
)

// TestFormatModuleDocRoundTrip 釘住模組 doc 的排版契約：
//   - 檔案開頭那段（以無點錨點 `; [arr](#arr)` 收尾）整段留在最前面；
//   - 它與下方首個宣告之間的空行照原樣保留（不吞、不補）；
//   - 錨點之後的註解仍然是首個宣告的 Doc，不會被併進模組說明；
//   - 再格式化一次結果不變（冪等）。
func TestFormatModuleDocRoundTrip(t *testing.T) {
	src := "; arr — 陣列操作\n" +
		"; arr {\n" +
		";     len i64 read-only\n" +
		"; }\n" +
		"; [arr](#arr)\n" +
		"\n" +
		"; len: 回傳陣列長度\n" +
		"; [len](#arr.len)\n" +
		"[n]t.len = () (out i64) {\n" +
		"    out = n\n" +
		"}\n"

	// Format 收尾會去掉尾部換行（見 Format），故比對時同樣剝掉。
	want := strings.TrimRight(src, "\n")
	out := Format(src)
	if out != want {
		t.Fatalf("module doc round-trip changed the file\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}

	// 模組說明只有一份：錨點行不得被搬去第一個函式上方，也不得消失。
	if n := strings.Count(out, "[arr](#arr)"); n != 1 {
		t.Fatalf("module anchor appears %d times, want 1\n%s", n, out)
	}
	if idx := strings.Index(out, "[arr](#arr)"); idx < 0 || idx > strings.Index(out, "[len](#arr.len)") {
		t.Fatalf("module doc must stay above the member doc\n%s", out)
	}

	if out2 := Format(out); out2 != out {
		t.Fatalf("fmt not idempotent for module doc\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatModuleDocBlankLinePreserved 確認空行的有無都由作者決定：
// 模組 doc 直接貼著宣告（無空行）時，fmt 不得自行插入空行。
func TestFormatModuleDocBlankLinePreserved(t *testing.T) {
	src := "; util — 工具\n" +
		"; [util](#util)\n" +
		"add = (a i64, b i64) (r i64) {\n" +
		"    r = a + b\n" +
		"}\n"
	want := strings.TrimRight(src, "\n")
	if out := Format(src); out != want {
		t.Fatalf("fmt inserted/removed a blank line after the module doc\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}
