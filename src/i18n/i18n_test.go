package i18n

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// reset puts the package back to a known state. Tests here mutate global
// language/catalog state, so none of them may call t.Parallel().
func reset(t *testing.T) {
	t.Helper()
	SetLang(defaultLang)
	ResetMissing()
}

// TestEnglishIsByteIdentical is the invariant the whole migration rests on:
// with lang == en, T must be indistinguishable from fmt.Sprintf, and must not
// consult the catalog even when one is installed.
func TestEnglishIsByteIdentical(t *testing.T) {
	reset(t)
	SetCatalog("en", map[string]string{"j7dzteja": "TRANSLATED SHOULD NOT APPEAR"})
	defer reset(t)

	cases := []struct {
		id   string
		def  string
		args []any
	}{
		{"j7dzteja", "function '%s' expects %d argument(s), got %d", []any{"f", 2, 1}},
		{"aaaaaaaa", "plain text", nil},
		{"bbbbbbbb", "100%% sure", nil},
	}
	for _, c := range cases {
		got := T(c.id, c.def, c.args...)
		want := c.def
		if len(c.args) > 0 {
			want = fmt.Sprintf(c.def, c.args...)
		}
		if got != want {
			t.Errorf("T(%q) = %q, want %q", c.id, got, want)
		}
	}
}

// TestZeroArgsSkipsSprintf locks the literal-percent rule: fmt.Sprintf("100%")
// renders "100%!(NOVERB)", which would corrupt every help line containing %.
func TestZeroArgsSkipsSprintf(t *testing.T) {
	reset(t)
	defer reset(t)

	const def = "use 100% of the cores"
	if got := T("cccccccc", def); got != def {
		t.Fatalf("zero-arg T = %q, want verbatim %q", got, def)
	}
	if got := P("cccccccc", def); got != def {
		t.Fatalf("P = %q, want verbatim %q", got, def)
	}
	err := Errorf("cccccccc", def)
	if err == nil || err.Error() != def {
		t.Fatalf("zero-arg Errorf = %v, want %q", err, def)
	}
}

// TestTranslationApplies checks the happy path and that args still interpolate.
func TestTranslationApplies(t *testing.T) {
	reset(t)
	defer reset(t)

	SetCatalog("zh-CN", map[string]string{
		"j7dzteja": "函數 '%s' 至少需要 %d 個參數，實際 %d 個",
	})
	if got := Lang(); got != "zh-CN" {
		t.Fatalf("Lang() = %q, want zh-CN", got)
	}
	got := T("j7dzteja", "function '%s' expects at least %d argument(s), got %d", "f", 2, 1)
	want := "函數 'f' 至少需要 2 個參數，實際 1 個"
	if got != want {
		t.Fatalf("T = %q, want %q", got, want)
	}
	if n := len(Missing()); n != 0 {
		t.Fatalf("Missing() = %v, want empty", Missing())
	}
}

// TestMissingFallsBackToEnglish records the fallback and keeps output usable.
func TestMissingFallsBackToEnglish(t *testing.T) {
	reset(t)
	defer reset(t)

	SetCatalog("zh-CN", map[string]string{"aaaaaaaa": "已知"})
	got := T("zzzzzzzz", "unknown %s", "x")
	if got != "unknown x" {
		t.Fatalf("T = %q, want English fallback", got)
	}
	miss := Missing()
	if len(miss) != 1 || miss[0] != "zzzzzzzz" {
		t.Fatalf("Missing() = %v, want [zzzzzzzz]", miss)
	}
}

// TestErrorfPreservesWrap is why Errorf exists instead of T: %w must survive so
// errors.Is can still see through the wrapper.
func TestErrorfPreservesWrap(t *testing.T) {
	reset(t)
	defer reset(t)

	sentinel := errors.New("boom")
	err := Errorf("dddddddd", "reading %s: %w", "f.no", sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is lost the sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "reading f.no") {
		t.Fatalf("message not formatted: %v", err)
	}

	SetCatalog("zh-CN", map[string]string{"dddddddd": "讀取 %s：%w"})
	err = Errorf("dddddddd", "reading %s: %w", "f.no", sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("translated errors.Is lost the sentinel: %v", err)
	}
	if err.Error() != "讀取 f.no：boom" {
		t.Fatalf("translated message = %q", err.Error())
	}
}

// TestWithLangRestores guards against cross-test leakage of global state.
func TestWithLangRestores(t *testing.T) {
	reset(t)
	defer reset(t)

	SetCatalog("zh-CN", map[string]string{"aaaaaaaa": "中文"})
	WithLang("en", func() {
		if got := T("aaaaaaaa", "english"); got != "english" {
			t.Fatalf("inside WithLang(en): %q", got)
		}
	})
	if got := T("aaaaaaaa", "english"); got != "中文" {
		t.Fatalf("after WithLang: %q, want the zh catalog restored", got)
	}
}

// TestSetCatalogCopiesTable makes sure a caller mutating its map afterwards
// cannot change translations mid-run.
func TestSetCatalogCopiesTable(t *testing.T) {
	reset(t)
	defer reset(t)

	src := map[string]string{"aaaaaaaa": "one"}
	SetCatalog("zh-CN", src)
	src["aaaaaaaa"] = "two"
	if got := T("aaaaaaaa", "def"); got != "one" {
		t.Fatalf("T = %q, want the copied value", got)
	}
}
