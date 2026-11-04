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
		{"tests/_bad.no", "_bad"},
		{"x/BAD_name/no.no", "BAD_name"},
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
