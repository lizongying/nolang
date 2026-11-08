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

// REGRESSION GUARD (multi-block documents: every block is formatted, not every other one).
//
// 結尾柵欄 ``` 在語法上同樣符合起始柵欄的正則。舊版格式化成功後把游標停在
// 結尾柵欄上，下一輪就把它當成新塊的開始，於是它與**下一個**真正的起始柵欄
// 配對，中間那個 Nolang 塊被當成「非 Nolang 塊的內容」整段跳過 —— 一份文件
// 裡只有第 1、3、5… 個塊會被格式化，第 2 個塊永遠原樣留底，且不入 skipped
// （作者無從察覺）。
func TestMdMultipleBlocksAllFormatted(t *testing.T) {
	input := mdDoc(
		mdFence+"no", "a=1", mdFence, "",
		"prose", "",
		mdFence+"no", "b=2", mdFence, "",
		"prose2", "",
		mdFence+"no", "c=3", mdFence, "",
	)
	want := mdDoc(
		mdFence+"no", "a = 1", mdFence, "",
		"prose", "",
		mdFence+"no", "b = 2", mdFence, "",
		"prose2", "",
		mdFence+"no", "c = 3", mdFence, "",
	)
	mdCheck(t, "multi-block", input, want, nil)
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
	// 只有 ```nolang 那一塊被格式化：`noml` / `bash` / 無語言標記的塊逐字保留。
	want := mdDoc(
		mdFence+"nolang",
		"x = 1",
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
	mdCheck(t, "langs", input, want, nil)
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

// 推薦寫法：塊頂部的 Nolang 註解 `#{fmt-skip}`，整塊既不驗證也不修改。
//
// 這個註解本身不是合法 Nolang 語句，所以跳過判定必須發生在解析之前 ——
// 否則塊會先因「註解項未知」解析失敗、被記成 skipped，作者的意圖就淹沒在
// 噪音裡。
func TestMdSkipAnnotation(t *testing.T) {
	for _, tc := range []struct{ name, block string }{
		{"annotation", "#{fmt-skip}" + mdNL + "x=1"},
		{"annotation-after-blank", mdNL + "#{fmt-skip}" + mdNL + "x=1"},
		{"annotation-with-indent", "  #{fmt-skip}" + mdNL + "  x=1"},
		{"annotation-shared-line", "#{fmt-skip, overflow=wrap}" + mdNL + "x=1"},
	} {
		in := mdDoc(mdFence+"no", tc.block, mdFence, "")
		mdCheck(t, tc.name, in, in, nil)
	}
}

// 註解必須在頂部：寫在塊中間不生效（整塊的跳過語義不能有歧義），該塊照常格式化。
// 同時驗證註解項不叫 fmt-skip 時照常格式化。
func TestMdSkipAnnotationOnlyAtTop(t *testing.T) {
	in := mdDoc(mdFence+"no", "x=1", "#{fmt-skip}", mdFence, "")
	got, skipped := FormatMarkdownFile(in)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped blocks: %v", skipped)
	}
	if !strings.Contains(got, "x = 1") {
		t.Fatalf("mid-block annotation must not skip formatting:\n got: %q", got)
	}
	other := mdDoc(mdFence+"no", "#{overflow=wrap}", "x=1+2", mdFence, "")
	if out := mustFormat(t, other); !strings.Contains(out, "x = 1 + 2") {
		t.Errorf("unrelated annotation must not skip formatting:\n got: %q", out)
	}
}

func mustFormat(t *testing.T, src string) string {
	t.Helper()
	out, skipped := FormatMarkdownFile(src)
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped blocks: %v", skipped)
	}
	return out
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

// REGRESSION GUARD: 塊裡出現中綴位置的詞法錯誤字元（`→`、`、`…）時，整塊必須
// 原樣留底並回報 skipped，絕不能被「格式化」成別的內容。
//
// `y = 1 → 2` 的 `→` 不屬任何中綴運算子，parseExpression 在 `1` 之後結束；
// 若該 token 不被報告，parser 會把它靜默丟掉，formatter 於是輸出兩條陳述
// （`y = 1` / `2`）——`no fmt -w -md` 就把文檔裡的 `→` 從原文裡刪掉了。
// 更早的版本還把 `→`(E2 86 92) 按位元組拆成三個 ILLEGAL，訊息印成 `\u0086`。
func TestMdInfixIllegalRuneKeepsBlockVerbatim(t *testing.T) {
	input := mdDoc(
		"prose",
		"",
		mdFence+"no",
		"y = 1 → 2",
		mdFence,
		"",
	)
	mdCheck(t, "infix-illegal-rune", input, input, []int{3})
}

// REGRESSION GUARD: 塊裡出現非 ASCII 文字（漢字）時，整塊原樣留底並回報 skipped。
//
// 舊版 isLetter 拿位元組去查 unicode 表（rune(0xE5) = `å`），漢字被拆成
// IDENT + ILLEGAL，於是 `可以 = 1` 報 `illegal token "\u008f"`。現在每個漢字
// 是一個完整碼點的 ILLEGAL，診斷給的是「識別字只允許 ASCII」這句可操作的話。
func TestMdNonASCIITextKeepsBlockVerbatim(t *testing.T) {
	input := mdDoc(
		"prose",
		"",
		mdFence+"no",
		"可以 = 1",
		mdFence,
		"",
	)
	mdCheck(t, "non-ascii-text", input, input, []int{3})
}

// REGRESSION GUARD: 跳過塊的行號必須是**原文件**行號。
//
// 格式化會改變前面塊的長度，而掃描游標走的是「當前緩衝區」下標。早期版本
// 直接報告 i+1，於是前面塊一變長，後面被跳過的塊行號就整體後移（實測報告
// 1040 行、柵欄實際在 1024），作者照行號找不到塊。
func TestMdSkippedLineIsOriginalLine(t *testing.T) {
	input := mdDoc(
		"prose",               // 1
		"",                    // 2
		mdFence+"no",          // 3
		"v: {",                // 4
		"    ok -> a()",       // 5
		"    -> b()",          // 6
		"}",                   // 7
		mdFence,               // 8
		"",                    // 9
		mdFence+"no",          // 10
		"not ⟦nolang⟧ at all", // 11
		mdFence,               // 12
		"",                    // 13
	)
	_, skipped := FormatMarkdownFile(input)
	if len(skipped) != 1 {
		t.Fatalf("want 1 skipped block, got %v", skipped)
	}
	if skipped[0].Line != 10 {
		t.Errorf("skipped line = %d, want 10 (original file line)", skipped[0].Line)
	}
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

// 純註解塊不是「無法解析」。
//
// 文檔裡常用「整塊只寫註解」的方式給出正確寫法（把程式碼註起來）。這種塊解析
// 成功、但一條陳述都沒有，formatProgramAST 把「無陳述」與「解析失敗」歸成同一
// 個 ok=false，於是 -md 把它報成 "not parseable as Nolang"，作者只能加
// #{fmt-skip} 去掩蓋一個並不存在的語法錯誤。它沒有可排版的程式碼，應原樣留底
// 且不計入 skipped。
func TestMdCommentOnlyBlockNotReported(t *testing.T) {
	input := mdDoc(
		"prose",
		"",
		mdFence+"no",
		"; 只有註解：正確寫法以註解呈現",
		"; fn-example = () {",
		"; }",
		mdFence,
		"",
	)
	mdCheck(t, "comment-only", input, input, nil)
}
