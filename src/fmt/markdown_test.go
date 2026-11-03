package fmt

import (
	"strings"
	"testing"
)

// markdown.go 的柵欄掃描測試：`no fmt` 對 .md / .mdx 只動 ```no / ```nolang
// 塊的內部，其他一切逐字保留；解析不了的塊原樣留底並回報；冪等可重複執行。
// 柵欄字面量用常量拼裝，避免 raw string 被反引號截斷。

const (
	mdFence   = "```"
	mdNL      = "\n"
	mdComment = "; nolang-fmt:skip"
)

func mdDoc(parts ...string) string { return strings.Join(parts, mdNL) }

// mdCheck 跑一遍格式化，比對結果、跳過清單，並驗證冪等（第二遍不再變化）。
func mdCheck(t *testing.T, name, input, want string, wantSkipped []int) {
	t.Helper()
	got, skipped := FormatMarkdownFile(input)
	if got != want {
		t.Errorf("%s:\n got: %q\nwant: %q", name, got, want)
	}
	var gotLines []int
	for _, s := range skipped {
		gotLines = append(gotLines, s.Line)
		if s.Reason == "" {
			t.Errorf("%s: skipped block at line %d has empty reason", name, s.Line)
		}
	}
	if len(gotLines) != len(wantSkipped) {
		t.Errorf("%s: skipped lines %v, want %v", name, gotLines, wantSkipped)
	} else {
		for i := range gotLines {
			if gotLines[i] != wantSkipped[i] {
				t.Errorf("%s: skipped lines %v, want %v", name, gotLines, wantSkipped)
				break
			}
		}
	}
	// 冪等：對輸出再跑一次，既不再改內容，也不再新增跳過。
	again, skipped2 := FormatMarkdownFile(got)
	if again != got {
		t.Errorf("%s: not idempotent:\n 1st: %q\n 2nd: %q", name, got, again)
	}
	if len(skipped2) != len(skipped) {
		t.Errorf("%s: skipped count changed on reformat: %d -> %d", name, len(skipped), len(skipped2))
	}
}

// 代碼塊內部被歸一化，柵欄行與正文一個字節都不動。
func TestMdFormatsNolangBlocks(t *testing.T) {
	input := mdDoc(
		"# Title",
		"",
		"Some prose with `inline code` and a 表格.",
		"",
		mdFence+"no",
		"n =  1",
		"x= n+1",
		mdFence,
		"",
		"trailing prose",
		"",
	)
	want := mdDoc(
		"# Title",
		"",
		"Some prose with `inline code` and a 表格.",
		"",
		mdFence+"no",
		"n = 1",
		"x = n + 1",
		mdFence,
		"",
		"trailing prose",
		"",
	)
	mdCheck(t, "basic", input, want, nil)
}

// `nolang` 是早期標籤，與 `no` 等價處理；`noml`（套件配置）與其他語言不碰。
func TestMdLangSelection(t *testing.T) {
	input := mdDoc(
		mdFence+"nolang",
		"x=1",
		mdFence,
		"",
		mdFence+"noml",
		"x=1",
		mdFence,
		"",
		mdFence+"bash",
		"x=1",
		mdFence,
		"",
		mdFence,
		"x=1",
		mdFence,
		"",
	)
	mdCheck(t, "langs", input, input, nil)
	// 只有 ```nolang 那一塊應該被改：把它換成格式化後的形態再驗一次。
	got, _ := FormatMarkdownFile(mdDoc(mdFence+"nolang", "x=1", mdFence, ""))
	if want := mdDoc(mdFence+"nolang", "x = 1", mdFence, ""); got != want {
		t.Errorf("nolang tag: got %q want %q", got, want)
	}
}

// 柵欄 meta 帶 skip / nofmt 時整塊留底；塊內 `; nolang-fmt:skip` 註解同效。
func TestMdSkipMarkers(t *testing.T) {
	for _, tc := range []struct{ name, fence, block string }{
		{"meta-skip", mdFence + "no skip", "x=1"},
		{"meta-nofmt", mdFence + "no nofmt", "x=1"},
		{"meta-with-title", mdFence + `no skip title="a.no"`, "x=1"},
		{"comment", mdFence + "no", mdComment + mdNL + "x=1"},
	} {
		in := mdDoc(tc.fence, tc.block, mdFence, "")
		mdCheck(t, tc.name, in, in, nil)
	}
}

// 列表項內的縮排柵欄：剝掉共同縮排格式化，再原樣貼回，層級縮排保留。
func TestMdIndentedFence(t *testing.T) {
	input := mdDoc(
		"- item:",
		"  "+mdFence+"no",
		"  for i <- [0..3): {",
		"  print(i)",
		"      print(  i )",
		"  }",
		"  "+mdFence,
		"",
	)
	want := mdDoc(
		"- item:",
		"  "+mdFence+"no",
		"  i <- [0..3): {",
		"      print(i)",
		"      print(i)",
		"  }",
		"  "+mdFence,
		"",
	)
	mdCheck(t, "indented", input, want, nil)
}

// 解析失敗的偽碼塊：內容逐字保留（含行尾空白與縮排），並回報行號。
func TestMdUnparseablePreserved(t *testing.T) {
	input := mdDoc(
		"prose",
		"",
		mdFence+"no",
		"this is not ⟦nolang⟧ at all …",
		mdFence,
		"",
		mdFence+"no",
		"y=2",
		mdFence,
		"",
	)
	want := mdDoc(
		"prose",
		"",
		mdFence+"no",
		"this is not ⟦nolang⟧ at all …",
		mdFence,
		"",
		mdFence+"no",
		"y = 2",
		mdFence,
		"",
	)
	mdCheck(t, "unparseable", input, want, []int{3})
}

// 空塊、未闭合塊、以及較長的結尾柵欄都不造成破壞。
func TestMdEdgeCases(t *testing.T) {
	mdCheck(t, "empty-block", mdDoc(mdFence+"no", mdFence, ""), mdDoc(mdFence+"no", mdFence, ""), nil)
	mdCheck(t, "unterminated", mdDoc(mdFence+"no", "x=1"), mdDoc(mdFence+"no", "x=1"), nil)
	mdCheck(t, "long-close", mdDoc(mdFence+"no", "x=1", "````", "x=1", mdFence, ""),
		mdDoc(mdFence+"no", "x = 1", "````", "x=1", mdFence, ""), nil)
	// 反引號出現在 info string 的行不是柵欄行，不應誤判。
	mdCheck(t, "bad-info", mdDoc(mdFence+"no `x`", "x=1", mdFence, ""),
		mdDoc(mdFence+"no `x`", "x=1", mdFence, ""), nil)
}

// 文件末尾有無換行都要保持原樣（格式化不擅自補尾換行到正文層級）。
func TestMdPreservesFinalNewline(t *testing.T) {
	got, _ := FormatMarkdownFile(mdDoc(mdFence+"no", "x=1", mdFence))
	if !strings.HasSuffix(got, mdFence) {
		t.Errorf("no trailing newline should be introduced: %q", got)
	}
}

// IsMarkdownPath 只認 .md / .mdx（大小寫不敏感），.no 走原本的路徑。
func TestIsMarkdownPath(t *testing.T) {
	for path, want := range map[string]bool{
		"docs/lang/syntax.md": true,
		"docs/index.mdx":      true,
		"README.MD":           true,
		"src/std/str.no":      false,
		"package.noml":        false,
		"notes/no":            false,
		"Makefile":            false,
		"a.md.bak":            false,
	} {
		if got := IsMarkdownPath(path); got != want {
			t.Errorf("IsMarkdownPath(%q) = %v, want %v", path, got, want)
		}
	}
}
