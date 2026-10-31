package catalog_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n/catalog"
	"github.com/lizongying/nolang/i18n/docscan"
	"github.com/lizongying/nolang/i18n/msgscan"
)

// srcRoot is the directory msgscan walks. The test runs with the package
// directory (src/i18n/catalog) as its working directory.
func srcRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestEnCatalogFresh proves the committed en.noml is byte-identical to a fresh
// render of the current source. A stale template is how translators end up
// working off keys that no longer exist.
func TestEnCatalogFresh(t *testing.T) {
	res, err := msgscan.Scan(srcRoot(t))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Pending > 0 {
		t.Errorf("%d call site(s) still carry %q; run scripts/stamp_traceid.sh", res.Pending, msgscan.Placeholder)
	}
	texts, err := msgscan.Texts(res)
	if err != nil {
		t.Fatalf("texts: %v", err)
	}
	want := catalog.RenderEn(texts)
	got, err := os.ReadFile(filepath.Join("locales", "en.noml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("en.noml is stale: run `make i18n` (committed %d bytes, fresh %d bytes)",
			len(got), len(want))
	}
}

// TestEnDocCatalogFresh is the same gate for the doc catalog, with one twist: a
// doc value is PRESERVED once present (its source text is a comment, not a Go
// literal, so re-seeding would revert translations). The gate must therefore
// apply the generator's merge, or it would demand that every hand-translated
// block be replaced by the Chinese source text.
func TestEnDocCatalogFresh(t *testing.T) {
	entries, err := docscan.Scan(srcRoot(t))
	if err != nil {
		t.Fatalf("docscan: %v", err)
	}
	scanned, err := docscan.Texts(entries)
	if err != nil {
		t.Fatalf("doc texts: %v", err)
	}
	path := filepath.Join("locales", "en"+catalog.DocCatalogSuffix)
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := catalog.ParseCatalogData(path, committed)
	if err != nil {
		t.Fatalf("parse committed doc catalog: %v", err)
	}
	want := catalog.RenderDoc(catalog.MergeDocTranslations(scanned, existing))
	if string(committed) != want {
		t.Fatalf("en%s is stale: run `make i18n` (committed %d bytes, fresh %d bytes)",
			catalog.DocCatalogSuffix, len(committed), len(want))
	}
}

// TestDocCatalogKeys is the orphan gate for doc translations: every key in a
// locale's doc catalog must still exist in en.doc.noml, which in turn must still
// exist in the source. A translation of a deleted comment is dead weight and
// hides the fact that a block was renamed.
//
// No verb check applies here: i18n.Doc never formats, so a `%` in a doc comment
// is literal text rather than a placeholder.
func TestDocCatalogKeys(t *testing.T) {
	enPath := filepath.Join("locales", "en"+catalog.DocCatalogSuffix)
	enData, err := os.ReadFile(enPath)
	if err != nil {
		t.Fatal(err)
	}
	en, err := catalog.ParseCatalogData(enPath, enData)
	if err != nil {
		t.Fatalf("parse en doc catalog: %v", err)
	}

	names, err := filepath.Glob(filepath.Join("locales", "*"+catalog.DocCatalogSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 2 {
		t.Fatalf("expected at least en and one translation, got %v", names)
	}
	for _, name := range names {
		tag := strings.TrimSuffix(filepath.Base(name), catalog.DocCatalogSuffix)
		if tag == "en" {
			continue
		}
		t.Run(tag, func(t *testing.T) {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			cat, err := catalog.ParseCatalogData(name, data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for key := range cat {
				if _, ok := en[key]; !ok {
					t.Errorf("orphan doc key %q: not in en%s (the anchor no longer exists in the source)",
						key, catalog.DocCatalogSuffix)
				}
			}
		})
	}
}

var idRe = regexp.MustCompile(`^[0-9a-z]{8}$`)

// TestLocaleKeysAndVerbs checks every shipped translation:
//   - the key is a well-formed message ID,
//   - the key exists in en (no orphans: a translation of a deleted message is
//     dead weight and hides drift),
//   - the format verbs match en exactly (a translator that drops a %s makes the
//     message print %!s(MISSING) at runtime).
func TestLocaleKeysAndVerbs(t *testing.T) {
	en, err := catalog.Load(nil, "en")
	if err != nil {
		t.Fatalf("load en: %v", err)
	}
	locales := catalog.Locales()
	if len(locales) < 2 {
		t.Fatalf("expected several built-in locales, got %v", locales)
	}
	for _, tag := range locales {
		if tag == "en" {
			continue
		}
		t.Run(tag, func(t *testing.T) {
			cat, err := catalog.Load(nil, tag)
			if err != nil {
				t.Fatalf("load %s: %v", tag, err)
			}
			if len(cat) == 0 {
				t.Skip("empty catalog")
			}
			for id, text := range cat {
				if !idRe.MatchString(id) {
					t.Errorf("key %q is not an 8-char base36 ID", id)
				}
				enText, ok := en[id]
				if !ok {
					t.Errorf("key %q has no en.noml entry (orphan translation)", id)
					continue
				}
				if got, want := verbs(text), verbs(enText); got != want {
					t.Errorf("%s: verb signature mismatch\n  en: %q (%s)\n  %s: %q (%s)",
						id, enText, want, tag, text, got)
				}
				if regexp.MustCompile(`%!`).MatchString(text) {
					t.Errorf("%s: translation contains a Go format error marker (%%!): %q", id, text)
				}
			}
		})
	}
}

// TestEveryIDWellFormed validates the en catalog's keys too, independently of
// the freshness check (which would pass even if every ID were malformed, as long
// as the source and the file agreed).
func TestEveryIDWellFormed(t *testing.T) {
	en, err := catalog.Load(nil, "en")
	if err != nil {
		t.Fatalf("load en: %v", err)
	}
	for id := range en {
		if !idRe.MatchString(id) {
			t.Errorf("en.noml key %q is not an 8-char base36 ID", id)
		}
	}
}

// verbRe captures the conversion character of each format directive, skipping
// the literal "%%".
var verbRe = regexp.MustCompile(`%(\[[0-9]+\])?[-+# 0-9.]*([a-zA-Z%])`)

// verbs returns the ordered conversion characters of a format string, e.g.
// "x %s y %d" -> "sd". Explicit argument indexes do not change the signature.
func verbs(format string) string {
	var out []byte
	for _, m := range verbRe.FindAllStringSubmatch(format, -1) {
		if m[2] == "%" {
			continue
		}
		out = append(out, m[2][0])
	}
	return string(out)
}
