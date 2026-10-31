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
	if err == nil || stripID(err.Error()) != def {
		t.Fatalf("zero-arg Errorf = %v, want %q modulo the [id] prefix", err, def)
	}
	if got := IDOf(err); got != "cccccccc" {
		t.Fatalf("IDOf = %q, want cccccccc", got)
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
	if stripID(err.Error()) != "讀取 f.no：boom" {
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

// stripID removes the trailing " [xxxxxxxx]" slot that Error() renders, so
// assertions can keep matching the wording itself.
func stripID(msg string) string {
	if !strings.HasSuffix(msg, "]") {
		return msg
	}
	start := strings.LastIndex(msg, " [")
	if start < 0 {
		return msg
	}
	return msg[:start]
}

// TestErrorCarriesID locks the contract that makes a compiler message
// reportable: the 8-character base36 ID is rendered inline, survives %w
// wrapping, and never appears for a message that has no ID.
func TestErrorCarriesID(t *testing.T) {
	reset(t)
	defer reset(t)

	inner := Errorf("abcd1234", "member %q is empty", "a")
	if got, want := inner.Error(), `member "a" is empty [abcd1234]`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	outer := fmt.Errorf("loading module %s: %w", "m.no", inner)
	if !strings.Contains(outer.Error(), "[abcd1234]") {
		t.Fatalf("ID lost through %%w: %q", outer.Error())
	}
	if got := IDOf(outer); got != "abcd1234" {
		t.Fatalf("IDOf(wrapped) = %q, want abcd1234", got)
	}
	if got := IDOf(errors.New("plain")); got != "" {
		t.Fatalf("IDOf(plain) = %q, want empty", got)
	}

	// A wrapper must not add a second ID when the inner error already renders
	// one: the innermost frame is the one that names the problem.
	nested := fmt.Errorf("compilation error: %w", fmt.Errorf("parser errors: %w", inner))
	if got := strings.Count(nested.Error(), "[abcd1234]"); got != 1 {
		t.Fatalf("nested chain renders %d IDs, want exactly 1: %q", got, nested.Error())
	}
	if strings.Contains(nested.Error(), "parser errors: member") == false {
		t.Fatalf("nested chain lost its context: %q", nested.Error())
	}

	// No ID: the text must be byte-identical to the pre-migration rendering.
	plain := Errorf("", "no id here")
	if got, want := plain.Error(), "no id here"; got != want {
		t.Fatalf("Error() without ID = %q, want %q", got, want)
	}
}

// TestTranslateMatchesT guards the forwarding helper: it must behave exactly
// like T, because the parser funnels every message through it.
func TestTranslateMatchesT(t *testing.T) {
	reset(t)
	defer reset(t)

	SetCatalog("zh-CN", map[string]string{"eeeeeeee": "需要 %d 個"})
	if got, want := Translate("eeeeeeee", "needs %d", 3), T("eeeeeeee", "needs %d", 3); got != want {
		t.Fatalf("Translate = %q, T = %q", got, want)
	}
}
