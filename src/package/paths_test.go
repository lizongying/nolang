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
