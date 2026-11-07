package pkg

import "testing"

func TestValidateSourcePathName(t *testing.T) {
	valid := []string{
		"tmp/zz-stridx/r-index.no",
		"src/std/vec.no",
		"tests/u128.no",
		"a/no",
		"foo/bar-baz/qux2.no",
		"tests/mem-safety/bug19-struct-field-corruption.no",
		"1-test.no", // 数字开头允许
		".",
		"example/test1/main.no",
		"_tmp-vet-test/x.no",   // 領先底線是忽略標記；去掉首 '_' 後 tmp-vet-test 合法 → 通過
		"_drafts/x.no",         // 領先底線 + drafts 合法 → 通過
	}
	for _, p := range valid {
		if err := ValidateSourcePathName(p); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", p, err)
		}
	}

	invalid := []struct {
		path string
		want string // 应包含的字面量
	}{
		{"tmp/zz_stridx/r_index.no", "zz_stridx"},
		{"tmp/zz_stridx/r-index.no", "zz_stridx"},
		{"src/Foo/bar.no", "Foo"},
		{"a/--b/no.no", "--b"},
		{"a/-b/no.no", "-b"},
		{"a/b--c/no.no", "b--c"},
		{"x/BAD_name/no.no", "BAD_name"},
		{"_tmp_vet_test/x.no", "_tmp_vet_test"}, // 領先 '_' 但去掉後仍含底線 → 非法，應報錯
		{"a/_bad_name/no.no", "_bad_name"},      // 目錄領先 '_' 但剩餘含底線 → 非法
	}
	for _, c := range invalid {
		err := ValidateSourcePathName(c.path)
		if err == nil {
			t.Errorf("expected %q to be invalid, got nil", c.path)
			continue
		}
		if !containsSubstr(err.Error(), c.want) {
			t.Errorf("error for %q should mention %q, got: %v", c.path, c.want, err)
		}
	}
}

// TestIsIgnoredSourcePath 驗證「底線開頭的路徑段（目錄或 .no 檔名）= 被忽略」慣例：
// 任一以 '_' 開頭的段都命中（整個目錄或單一草稿檔）；非 .no 檔、恰好為 "_" 的空名不放行。
func TestIsIgnoredSourcePath(t *testing.T) {
	ignored := []string{
		"tests/_jp7.no",        // 檔名主體 _jp7
		"_draft.no",            // 檔名主體 _draft
		"src/std/_private.no",  // 檔名主體 _private
		"a/b/_c.no",            // 檔名主體 _c
		"_drafts/x.no",         // 上層目錄 _drafts
		"tests/_drafts/x.no",   // 上層目錄 _drafts
		"src/std/_hidden/y.no", // 上層目錄 _hidden
		"_tmp-vet-test/x.no",   // 領先 '_' 且剩餘 tmp-vet-test 合法 → 忽略
	}
	for _, p := range ignored {
		if !IsIgnoredSourcePath(p) {
			t.Errorf("expected %q to be an ignored source path", p)
		}
	}

	notIgnored := []string{
		"tests/jp7.no",      // 無底線
		"tests/_.no",        // 檔名主體恰好是 "_"（視為空，不放行）
		"tests/_/x.no",      // 段恰好是 "_"（視為空，不放行）
		"_readme.md",        // 非 .no 檔
		"src/std/vec.no",    // 正常檔名
		"main.no",           // 正常檔名
		"_tmp_vet_test/x.no", // 領先 '_' 但剩餘 tmp_vet_test 含底線 → 非法名，不忽略
		"_tmp_vet_test/",     // 同上，目錄情形
	}
	for _, p := range notIgnored {
		if IsIgnoredSourcePath(p) {
			t.Errorf("expected %q NOT to be an ignored source path", p)
		}
	}
}

// TestIsIgnoredSourceDir 驗證「以 '_' 開頭的目錄名 = 被忽略目錄」：
// 整個目錄（含子樹）被跳過；恰好為 "_" 的段不放行。
func TestIsIgnoredSourceDir(t *testing.T) {
	for _, d := range []string{"_drafts", "_scratch", "_private"} {
		if !IsIgnoredSourceDir(d) {
			t.Errorf("expected dir %q to be ignored", d)
		}
	}
	for _, d := range []string{"_", "drafts", "src", "a_b"} {
		if IsIgnoredSourceDir(d) {
			t.Errorf("expected dir %q NOT to be ignored", d)
		}
	}
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ValidateMarkdownDirName 只檢查目錄段，且放行隱藏目錄（`.agents` 等工具鏈約定名）；
// 檔名本身不受源碼命名規範約束（README.md / CHANGELOG.md 是慣例）。
func TestValidateMarkdownDirName(t *testing.T) {
	valid := []string{
		"README.md",
		"docs/README.md",
		"docs/i18n/en/docusaurus-plugin-content-docs/current/lang/syntax.md",
		".agents/skills/nolang-syntax/SKILL.md", // 隱藏目錄放行
		".github/workflows/build.md",
		"docs/index.mdx",
	}
	for _, p := range valid {
		if err := ValidateMarkdownDirName(p); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", p, err)
		}
	}

	invalid := map[string]string{
		"docs/bad_name/syntax.md": "bad_name", // 目錄段含底線
		"docs/Foo/x.md":           "Foo",
	}
	for p, want := range invalid {
		err := ValidateMarkdownDirName(p)
		if err == nil {
			t.Errorf("expected %q to be invalid, got nil", p)
			continue
		}
		if !containsSubstr(err.Error(), want) {
			t.Errorf("error for %q should mention %q, got: %v", p, want, err)
		}
	}
}
