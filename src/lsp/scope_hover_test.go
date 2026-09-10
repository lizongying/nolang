package lsp

import (
	"strings"
	"testing"
)

// REGRESSION GUARD: 同名符號在不同函式間不得互相污染。
//
// nolang 的符號索引本質是扁平的（symbols[name] 只留一筆，後寫覆蓋），但
// 不同函式可以各自宣告同名區域變數。src/std/fs.no 就是典型：
//   - `size` 是 stat-size / fstat-size / file-size 的結果參數（?i64）
//   - `size ?= fstat-size(.fd)` 是 file.read-bytes 的區域變數（解包後 i64）
//   - `size = stat-size(save-path)` 是 file.append 的區域變數（?i64）
//
// 若 hover 只走扁平表，`size ?= ...` 的引用會顯示成別的函式的 ?i64。
// LookupAtPosition 必須先用 scopeRanges 把游標映射回所在函式，再在該作用域
// 內選取游標之前最近的宣告。
func TestHoverResolvesSameNamePerFunctionScope(t *testing.T) {
	// 0-based 行號：
	//   0: stat-size = (p str) (size ?i64) {
	//   1: }
	//   2: (blank)
	//   3: fstat-size = (fd fd) (size ?i64) {
	//   4: }
	//   5: (blank)
	//   6: file.read-bytes = () (content ?i64) {
	//   7:     size ?= fstat-size(0)
	//   8:     content = size
	//   9: }
	//  10: (blank)
	//  11: file.append = () (ok ?bool) {
	//  12:     size = stat-size('x')
	//  13:     ok = size
	//  14: }
	text := "stat-size = (p str) (size ?i64) {\n}\n\n" +
		"fstat-size = (fd fd) (size ?i64) {\n}\n\n" +
		"file.read-bytes = () (content ?i64) {\n" +
		"    size ?= fstat-size(0)\n" +
		"    content = size\n" +
		"}\n\n" +
		"file.append = () (ok ?bool) {\n" +
		"    size = stat-size('x')\n" +
		"    ok = size\n" +
		"}\n"

	dm := NewDocumentManager()
	uri := "file:///test/scope_hover.no"
	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, _, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	idx := dm.GetIndex(uri)
	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	hp := NewHoverProvider(doc, idx)

	cases := []struct {
		name string
		line uint32
		col  uint32
		want string
	}{
		// 結果參數本身（宣告處）仍是 ?i64。
		{"stat-size result param", 0, 21, "?i64"},
		{"fstat-size result param", 3, 22, "?i64"},
		// `?=` 解包後的區域變數：宣告處與後續引用都是 i64。
		{"read-bytes ?= declaration", 7, 6, "i64"},
		{"read-bytes ?= reference", 8, 15, "i64"},
		// 另一個函式的同名變數保持自己的 option 型別。
		{"append plain assign", 12, 6, "?i64"},
		{"append reference", 13, 10, "?i64"},
	}
	for _, tc := range cases {
		h, ok := hp.GetHover(Position{Line: tc.line, Character: tc.col})
		if !ok {
			t.Errorf("%s: no hover at line %d col %d", tc.name, tc.line+1, tc.col)
			continue
		}
		mc, ok := h.Contents.(MarkupContent)
		if !ok {
			t.Errorf("%s: unexpected hover contents %T", tc.name, h.Contents)
			continue
		}
		want := "`" + tc.want + "`"
		if !strings.Contains(mc.Value, want) {
			t.Errorf("%s: hover missing %s:\n%s", tc.name, want, mc.Value)
		}
	}
}
