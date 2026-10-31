package i18n

import (
	"errors"
	"testing"
)

// TestDocTranslatesIntoEnglish is the property that motivated Doc's existence:
// English is a translation TARGET for source comments, so the "en" fast path that
// protects T must not apply to Doc.
func TestDocTranslatesIntoEnglish(t *testing.T) {
	reset(t)
	defer reset(t)

	SetLang(defaultLang)
	SetDocCatalog(map[string]string{"args.get": "the i-th argument"})
	defer SetDocCatalog(nil)

	if got := Doc("args.get", "獲取第 i 個參數"); got != "the i-th argument" {
		t.Fatalf("Doc under en = %q, want the English translation", got)
	}
	// T must be unaffected: English messages still come from the inline text,
	// never from the catalog.
	if got := T("aaaaaaaa", "inline english"); got != "inline english" {
		t.Fatalf("T under en = %q, want the inline text", got)
	}
}

// TestDocFallsBackToSource covers the untranslated case: a hover must degrade to
// the text the author wrote, never to an empty string or a bare key.
func TestDocFallsBackToSource(t *testing.T) {
	reset(t)
	defer reset(t)
	SetDocCatalog(map[string]string{"args.get": "translated"})
	defer SetDocCatalog(nil)

	if got := Doc("args.nope", "source text"); got != "source text" {
		t.Fatalf("Doc(missing) = %q, want the source text", got)
	}
	if got := Doc("", "source text"); got != "source text" {
		t.Fatalf("Doc(empty key) = %q, want the source text", got)
	}
}

// TestDocNeverFormats is the second reason Doc is not T: doc prose contains
// literal percent signs, and there are no arguments to interpolate. Running
// Sprintf would corrupt them into %!s(NOVERB).
func TestDocNeverFormats(t *testing.T) {
	reset(t)
	defer reset(t)

	const prose = "clamps to 50% of the buffer; %s stays literal"
	if got := Doc("args.x", prose); got != prose {
		t.Fatalf("Doc = %q, want the text verbatim", got)
	}
	SetDocCatalog(map[string]string{"args.x": "翻译 100% 原样"})
	defer SetDocCatalog(nil)
	if got := Doc("args.x", prose); got != "翻译 100% 原样" {
		t.Fatalf("Doc(translated) = %q, want the translation verbatim", got)
	}
}

// TestSetLangDropsDocCatalog keeps the two tables in step: switching language
// without a catalog must not leave the previous language's docs installed.
func TestSetLangDropsDocCatalog(t *testing.T) {
	reset(t)
	defer reset(t)

	SetDocCatalog(map[string]string{"args.get": "translated"})
	SetLang("zh-CN")
	if got := Doc("args.get", "source"); got != "source" {
		t.Fatalf("Doc after SetLang = %q, want the source text", got)
	}
}

// TestInitInstallsDocCatalogForEnglish proves Init wires DocLoad for "en" too —
// the whole point of a separate loader. Message catalogs are skipped for English
// (T's fast path), doc catalogs are not.
func TestInitInstallsDocCatalogForEnglish(t *testing.T) {
	reset(t)
	defer reset(t)

	docLoads := 0
	err := Init(Options{
		Lang: "en", LangSet: true,
		Env: fakeEnv(nil),
		Load: func(string) (map[string]string, error) {
			t.Fatal("Init must not load a message catalog for English")
			return nil, nil
		},
		DocLoad: func(tag string) (map[string]string, error) {
			docLoads++
			if tag != defaultLang {
				t.Fatalf("DocLoad(%q), want %q", tag, defaultLang)
			}
			return map[string]string{"args.get": "the i-th argument"}, nil
		},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if docLoads != 1 {
		t.Fatalf("DocLoad called %d times, want 1", docLoads)
	}
	if got := Doc("args.get", "獲取第 i 個參數"); got != "the i-th argument" {
		t.Fatalf("Doc = %q, want the English translation", got)
	}
}

// TestInitDegradesOnBrokenDocCatalog: a doc translation is a presentation nicety,
// so a malformed doc catalog must degrade to the source text with a warning
// instead of stopping the compiler. Before doc links existed the en catalog was
// never read at runtime, so failing here would be a new way for `no build` to
// exit 2 over cosmetics.
func TestInitDegradesOnBrokenDocCatalog(t *testing.T) {
	reset(t)
	defer reset(t)

	var warns []string
	err := Init(Options{
		Lang: "en", LangSet: true,
		Env:     fakeEnv(nil),
		DocLoad: func(string) (map[string]string, error) { return nil, errors.New("boom") },
		Warn:    func(m string) { warns = append(warns, m) },
	})
	if err != nil {
		t.Fatalf("Init returned %v; a broken doc catalog must degrade, not fail", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
	if got := Doc("args.get", "source"); got != "source" {
		t.Fatalf("Doc = %q, want the source text", got)
	}
}

// TestInitFailsOnBrokenLocaleCatalog is the other half: a non-English MESSAGE
// catalog drives every diagnostic, so a failure there stays fatal (unchanged
// behaviour).
func TestInitFailsOnBrokenLocaleCatalog(t *testing.T) {
	reset(t)
	defer reset(t)

	err := Init(Options{
		Lang: "zh-CN", LangSet: true,
		Available: []string{"zh-CN"},
		Env:       fakeEnv(nil),
		Load:      func(string) (map[string]string, error) { return nil, errors.New("boom") },
	})
	if err == nil {
		t.Fatal("Init accepted a broken zh-CN catalog; it must fail")
	}
}
