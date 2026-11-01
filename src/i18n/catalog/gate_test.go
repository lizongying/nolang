package catalog_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n"
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

// TestEnDocCatalogFresh is the same gate for the doc catalog, with two twists.
//
// A doc catalog holds TRANSLATIONS ONLY: an anchor with no committed value is
// not seeded, because its source text is in the author's language and English is
// a translation target. And a committed value is preserved even if the source
// comment was reworded, because the anchor rather than the text is the key. The
// gate applies the generator's prune, so it demands exactly what `make i18n`
// would write.
func TestEnDocCatalogFresh(t *testing.T) {
	entries, err := docscan.Scan(srcRoot(t))
	if err != nil {
		t.Fatalf("docscan: %v", err)
	}
	scanned, err := docscan.Texts(entries)
	if err != nil {
		t.Fatalf("doc texts: %v", err)
	}
	path := filepath.Join("locales", catalog.DocCatalogName("en"))
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := catalog.ParseCatalogData(path, committed)
	if err != nil {
		t.Fatalf("parse committed doc catalog: %v", err)
	}
	want := catalog.RenderDoc("en", catalog.PruneDocTranslations(scanned, existing))
	if string(committed) != want {
		t.Fatalf("en%s is stale: run `make i18n` (committed %d bytes, fresh %d bytes)",
			catalog.DocCatalogSuffix, len(committed), len(want))
	}
	if len(scanned) == 0 {
		t.Fatal("no anchored doc blocks found: the scanner or the anchors are broken")
	}
	t.Logf("%d of %d anchors translated into English", len(existing), len(scanned))
}

// TestDocCatalogKeys is the orphan gate for doc translations: every key in any
// doc catalog must still be an anchor that exists in the .no source. A
// translation of a deleted comment is dead weight and hides the fact that a
// block was renamed.
//
// Orphans are checked against the SOURCE, not against en-doc.noml, so a locale
// may translate a block that English has not got to yet.
//
// No verb check applies here: i18n.Doc never formats, so a `%` in a doc comment
// is literal text rather than a placeholder.
func TestDocCatalogKeys(t *testing.T) {
	entries, err := docscan.Scan(srcRoot(t))
	if err != nil {
		t.Fatalf("docscan: %v", err)
	}
	scanned, err := docscan.Texts(entries)
	if err != nil {
		t.Fatalf("doc texts: %v", err)
	}

	names, err := filepath.Glob(filepath.Join("locales", "*"+catalog.DocCatalogSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 2 {
		t.Fatalf("expected at least en and one translation, got %v", names)
	}
	for _, name := range names {
		// The file name is lowercase by the `.no` naming rule, so this label is
		// `zh-cn` rather than the `zh-CN` tag the user types. It is only a label:
		// the catalog is read straight from the path.
		tag := strings.TrimSuffix(filepath.Base(name), catalog.DocCatalogSuffix)
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
				if _, ok := scanned[key]; !ok {
					t.Errorf("orphan doc key %q: no .no source declares that anchor", key)
				}
			}
		})
	}
}

// stdAnchors scans the .no corpus and keeps only the anchors declared under
// src/std — the exact key domain of an alias catalog (see genen.scanAliases:
// an alias names a public API surface, and only std functions have one).
func stdAnchors(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := docscan.Scan(srcRoot(t))
	if err != nil {
		t.Fatalf("docscan: %v", err)
	}
	scanned := map[string]bool{}
	stdPrefix := "std" + string(filepath.Separator)
	for _, e := range entries {
		rel, rerr := filepath.Rel(srcRoot(t), e.File)
		if rerr == nil && strings.HasPrefix(rel, stdPrefix) {
			scanned[e.Anchor] = true
		}
	}
	if len(scanned) == 0 {
		t.Fatal("no std anchors found: the scanner or the anchors are broken")
	}
	return scanned
}

// TestEnAliasCatalogFresh is the alias twin of TestEnDocCatalogFresh: the
// committed en-alias.noml must equal `make i18n`'s prune of itself. Values are
// hand-written (or generated by a scratch script); the gate owns the key domain
// and the row schema.
func TestEnAliasCatalogFresh(t *testing.T) {
	scanned := stdAnchors(t)
	path := filepath.Join("locales", catalog.AliasCatalogName("en"))
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := catalog.ParseAliasCatalogData(path, committed)
	if err != nil {
		t.Fatalf("parse committed alias catalog: %v", err)
	}
	want := catalog.RenderAlias("en", catalog.PruneAliasTranslations(scanned, existing))
	if string(committed) != want {
		t.Fatalf("en%s is stale: run `make i18n` (committed %d bytes, fresh %d bytes)",
			catalog.AliasCatalogSuffix, len(committed), len(want))
	}
	if len(existing) < len(scanned) {
		t.Logf("%d of %d std anchors aliased (incomplete is allowed, `-alias-missing` lists the rest)",
			len(existing), len(scanned))
	}
}

// aliasCatalogFiles returns every committed alias catalog as (tag, path), where
// tag is the conventional spelling (`zh-CN`) rather than the file's lowercase
// one. Sorted by file name so a failure reports in a stable order.
func aliasCatalogFiles(t *testing.T) []struct{ tag, path string } {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("locales", "*"+catalog.AliasCatalogSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 2 {
		t.Fatalf("expected en and at least one translation, got %v", names)
	}
	out := make([]struct{ tag, path string }, 0, len(names))
	for _, name := range names {
		out = append(out, struct{ tag, path string }{
			tag:  catalog.Canonical(strings.TrimSuffix(filepath.Base(name), catalog.AliasCatalogSuffix)),
			path: name,
		})
	}
	return out
}

// TestAliasCatalogSchemaAllLocales is the gate the per-tag catalogs did not have
// before: `make i18n` only rewrites en, so a locale missing a column — or still
// written in an older layout — would silently degrade to identifiers at runtime
// with nothing but a warning. The optional fourth column is pinned to the
// Chinese catalogs: zh-CN rows carry full pinyin, zh-TW rows carry zhuyin key
// positions, and no other locale may carry either.
func TestAliasCatalogSchemaAllLocales(t *testing.T) {
	files := aliasCatalogFiles(t)
	if got, want := len(files), len(catalog.AliasLocales()); got != want {
		t.Fatalf("%d alias catalog files, want one per alias locale (%d)", got, want)
	}
	for _, f := range files {
		t.Run(f.tag, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			cat, err := catalog.ParseAliasCatalogData(f.path, data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(cat) == 0 {
				t.Fatal("empty catalog")
			}
			for anchor, e := range cat {
				if e.Alias == "" {
					t.Errorf("%q: empty alias", anchor)
				}
				if e.Keys == "" || e.EnKeys == "" {
					t.Errorf("%q: keys=%q enkeys=%q, both are required", anchor, e.Keys, e.EnKeys)
				}
				chinese := f.tag == "zh-CN" || f.tag == "zh-TW"
				if chinese && e.Full == "" {
					t.Errorf("%q: %s row has no full keystrokes (pinyin/zhuyin)", anchor, f.tag)
				}
				if !chinese && e.Full != "" {
					t.Errorf("%q: full=%q on a %s row; only the Chinese catalogs carry it", anchor, e.Full, f.tag)
				}
			}
		})
	}
}

// TestAliasEnKeysDerived keeps enkeys honest. It is a pure function of the
// anchor (initial of each word of the identifier), so a hand-edited cell that
// disagrees with the rule is a typo, not a translation choice.
func TestAliasEnKeysDerived(t *testing.T) {
	for _, f := range aliasCatalogFiles(t) {
		t.Run(f.tag, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			cat, err := catalog.ParseAliasCatalogData(f.path, data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for anchor, e := range cat {
				if want := i18n.EnKeysOf(anchor); e.EnKeys != want {
					t.Errorf("%q: enkeys=%q, derivation says %q", anchor, e.EnKeys, want)
				}
			}
		})
	}
}

// TestAliasCatalogCanonicalAllLocales pins the rendering of every locale, not
// just en. Without it, a hand edit to one of the nine non-generated files
// (stray indentation, fields reordered, a column written as '' instead of
// omitted) passes every gate until someone regenerates and gets a huge diff.
func TestAliasCatalogCanonicalAllLocales(t *testing.T) {
	for _, f := range aliasCatalogFiles(t) {
		t.Run(f.tag, func(t *testing.T) {
			committed, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			cat, err := catalog.ParseAliasCatalogData(f.path, committed)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if want := catalog.RenderAlias(f.tag, cat); string(committed) != want {
				t.Errorf("%s is not in canonical form (committed %d bytes, rendered %d bytes)",
					f.path, len(committed), len(want))
			}
		})
	}
}

// TestAliasCatalogHeaderMatches: the header is documentation for whoever edits
// a row, and it is part of the byte comparison above. A file whose header was
// hand-edited would otherwise only fail as a byte diff with no clue why.
func TestAliasCatalogHeaderMatches(t *testing.T) {
	for _, f := range aliasCatalogFiles(t) {
		t.Run(f.tag, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			header := catalog.AliasCatalogHeaderFor(f.tag)
			if !strings.HasPrefix(string(data), header) {
				t.Errorf("%s does not start with AliasCatalogHeaderFor(%q)", f.path, f.tag)
			}
		})
	}
}

// TestAliasCatalogKeys is the orphan gate for every alias catalog: a key must
// still name a std anchor. An alias of a renamed function is dead weight and
// hides the rename from hover.
func TestAliasCatalogKeys(t *testing.T) {
	scanned := stdAnchors(t)
	for _, f := range aliasCatalogFiles(t) {
		t.Run(f.tag, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			cat, err := catalog.ParseAliasCatalogData(f.path, data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			for key := range cat {
				if !scanned[key] {
					t.Errorf("orphan alias key %q: no src/std .no source declares that anchor", key)
				}
			}
		})
	}
}

// TestAliasCatalogNamesFollowSourceRules pins the alias file naming, and that
// every message locale also ships an alias catalog — the sets come from the
// same embedded directory, so a new tag without an alias file fails here.
func TestAliasCatalogNamesFollowSourceRules(t *testing.T) {
	segRe := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	for _, tag := range catalog.Locales() {
		name := catalog.AliasCatalogName(tag)
		if seg := strings.TrimSuffix(name, ".noml"); !segRe.MatchString(seg) {
			t.Errorf("alias catalog name for %q is %q: %q is not a legal source-file segment", tag, name, seg)
		}
		if name != catalog.AliasCatalogName(strings.ToLower(tag)) {
			t.Errorf("AliasCatalogName(%q) = %q depends on case", tag, name)
		}
		if _, err := os.Stat(filepath.Join("locales", name)); err != nil {
			t.Errorf("alias catalog %q for tag %q missing: %v", name, tag, err)
		}
	}
	if got, want := catalog.AliasCatalogName("zh-CN"), "zh-cn-alias.noml"; got != want {
		t.Errorf("AliasCatalogName(zh-CN) = %q, want %q", got, want)
	}
	if aliases := catalog.AliasLocales(); len(aliases) != len(catalog.Locales()) {
		t.Errorf("AliasLocales() = %v, want one alias catalog per locale %v", aliases, catalog.Locales())
	}
}

var idRe = regexp.MustCompile(`^[0-9a-z]{8}$`)

// TestLocaleFileNamesFollowSourceRules is the message-catalog twin of the doc
// gate below: a locale file must be a legal `.no` source-file segment, so the
// tag (which keeps its conventional spelling, `zh-CN`) is lowercased into the
// file name (`zh-cn.noml`). A tag and a file that disagree about case is exactly
// the bug this pins: `Load` would look for a file nobody wrote.
func TestLocaleFileNamesFollowSourceRules(t *testing.T) {
	segRe := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	locales := catalog.Locales()
	if len(locales) == 0 {
		t.Fatal("no embedded locales: the naming gate would pass vacuously")
	}
	for _, tag := range locales {
		name := catalog.LocaleFileName(tag)
		seg := strings.TrimSuffix(name, ".noml")
		if !segRe.MatchString(seg) {
			t.Errorf("catalog file for %q is %q: %q is not a legal source-file segment", tag, name, seg)
		}
		if name != catalog.LocaleFileName(strings.ToLower(tag)) {
			t.Errorf("LocaleFileName(%q) = %q depends on case", tag, name)
		}
		// The file must be the one Load actually reads: a stale `zh-CN.noml`
		// left behind would make the tag resolve while the new file went unused.
		if _, err := os.Stat(filepath.Join("locales", name)); err != nil {
			t.Errorf("locale file %q for tag %q: %v", name, tag, err)
		}
	}
	if got, want := catalog.LocaleFileName("zh-CN"), "zh-cn.noml"; got != want {
		t.Errorf("LocaleFileName(zh-CN) = %q, want %q", got, want)
	}
}

// TestDocCatalogNamesFollowSourceRules pins the naming rule for doc catalogs:
// a name must be a legal `.no` source-file segment (lowercase letters, digits
// and single hyphens), because that is the only naming convention this tree
// has. A locale tag is not constrained that way — `zh-CN` is the tag a user
// types — so DocCatalogName lowercases it. Without this gate a future tag such
// as `zh_Hant` or `pt-BR-x-formal` would quietly produce a name that `no`
// itself would reject, and nothing else in the build would notice.
func TestDocCatalogNamesFollowSourceRules(t *testing.T) {
	// Same shape as package.isValidSourceSegment: lowercase alphanumerics joined
	// by single hyphens, no leading, trailing or repeated hyphen.
	segRe := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	locales := catalog.Locales()
	if len(locales) == 0 {
		t.Fatal("no embedded locales: the naming gate would pass vacuously")
	}
	for _, tag := range locales {
		name := catalog.DocCatalogName(tag)
		seg := strings.TrimSuffix(name, ".noml")
		if !segRe.MatchString(seg) {
			t.Errorf("doc catalog name for %q is %q: %q is not a legal source-file segment", tag, name, seg)
		}
		if name != catalog.DocCatalogName(strings.ToLower(tag)) {
			t.Errorf("DocCatalogName(%q) = %q depends on case; a tag must lowercase into one name", tag, name)
		}
	}
	if got, want := catalog.DocCatalogName("zh-CN"), "zh-cn-doc.noml"; got != want {
		t.Errorf("DocCatalogName(zh-CN) = %q, want %q", got, want)
	}
}

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
