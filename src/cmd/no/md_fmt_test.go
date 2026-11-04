package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	nfmt "github.com/lizongying/nolang/fmt"
)

// `no fmt -md` 的 CLI 接線測試：Markdown 檔案名不受「小寫字母/數字/中劃線」
// 的源碼命名規範約束（README.md 這種帶點的檔名本來就合法），但目錄段仍然要
// 遵守；.no 則照舊全段校驗。
func TestValidateFmtPathAllowsMarkdownName(t *testing.T) {
	for _, ok := range []string{
		"README.md",
		"docs/lang/syntax.md",
		"docs/index.mdx",
		"src/std/str.no",
		"tmp/zz-stridx/r-index.no",
	} {
		if err := validateFmtPath(ok); err != nil {
			t.Errorf("validateFmtPath(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"docs/bad_name/syntax.md", // 目錄段含底線：Markdown 也不例外
		"tmp/some_dir/x.no",
	} {
		if err := validateFmtPath(bad); err == nil {
			t.Errorf("validateFmtPath(%q) = nil, want an error", bad)
		}
	}
}

// 端到端：一份文件裡三個 Nolang 塊（含一個解析不了的偽碼塊）都要照顧到 ——
// 前兩個被格式化、偽碼塊逐字保留、塊外的正文一個字節都不動。
func TestFmtProcessMarkdownFileFormatsEveryBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	src := strings.Join([]string{
		"# Title",
		"",
		"```no",
		"a=1",
		"```",
		"",
		"prose 保持原樣",
		"",
		"```nolang",
		"b=2",
		"```",
		"",
		"```no",
		"this is not ⟦nolang⟧",
		"```",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fmtProcessMarkdownFile(path, true, false, nfmt.LoopStylePrefix); err != nil {
		t.Fatalf("fmtProcessMarkdownFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"# Title",
		"",
		"```no",
		"a = 1",
		"```",
		"",
		"prose 保持原樣",
		"",
		"```nolang",
		"b = 2",
		"```",
		"",
		"```no",
		"this is not ⟦nolang⟧", // 解析失敗：逐字保留
		"```",
		"",
	}, "\n")
	if string(got) != want {
		t.Fatalf("unexpected result:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	// 冪等：再跑一次不應再變。
	if err := fmtProcessMarkdownFile(path, true, false, nfmt.LoopStylePrefix); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(got) {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", got, again)
	}
}
