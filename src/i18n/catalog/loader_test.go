package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/i18n"
)

// writeLocale drops a locale file into dir/locales.
func writeLocale(t *testing.T, dir, tag, body string) {
	t.Helper()
	localeDir := filepath.Join(dir, LocaleDirName)
	if err := os.MkdirAll(localeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localeDir, tag+".noml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestProjectLocaleOverridesEmbedded is the whole point of the project dir: a
// team can fix or add a translation without rebuilding the toolchain.
func TestProjectLocaleOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	writeLocale(t, dir, "zh-CN", "{\n  'aaaaaaaa': '工程內覆蓋'\n}\n")

	msgs, err := Load([]string{dir}, "zh-CN")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := msgs["aaaaaaaa"]; got != "工程內覆蓋" {
		t.Fatalf("msgs[aaaaaaaa] = %q, want the project override", got)
	}
}

// TestEmbeddedLocaleLoads makes sure the baked-in catalog is reachable when no
// project override exists.
func TestEmbeddedLocaleLoads(t *testing.T) {
	msgs, err := Load(nil, "zh-CN")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if msgs == nil {
		t.Fatal("embedded zh-CN catalog should parse to an (possibly empty) map")
	}
}

// TestNonStringValueRejected: a nested object means the file was hand-edited
// wrong; failing loudly beats a half-loaded catalog.
func TestNonStringValueRejected(t *testing.T) {
	dir := t.TempDir()
	writeLocale(t, dir, "zh-CN", "{\n  'aaaaaaaa': {\n    x: 'y'\n  }\n}\n")

	if _, err := Load([]string{dir}, "zh-CN"); err == nil {
		t.Fatal("want an error for a non-string value")
	}
}

// TestAvailableIncludesAliases: a bare "zh" request must resolve, and the
// canonical tag must stay zh-CN so the reported language matches the file.
func TestAvailableIncludesAliases(t *testing.T) {
	avail := Available(nil)
	var hasCanon, hasAlias bool
	for _, tag := range avail {
		if tag == "zh-CN" {
			hasCanon = true
		}
		if tag == "zh" {
			hasAlias = true
		}
	}
	if !hasCanon {
		t.Fatalf("Available() = %v, want zh-CN", avail)
	}
	if !hasAlias {
		t.Fatalf("Available() = %v, want the zh alias", avail)
	}
	if got := Canonical("zh"); got != "zh-CN" {
		t.Fatalf("Canonical(zh) = %q, want zh-CN", got)
	}
}

// TestInitInstallsTranslatedCatalog exercises the full path a binary uses.
func TestInitInstallsTranslatedCatalog(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang("en")

	dir := t.TempDir()
	writeLocale(t, dir, "zh-CN", "{\n  'aaaaaaaa': '中文'\n}\n")

	if err := Init(Options{Lang: "zh-CN", LangSet: true, ProjectDirs: []string{dir}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if got := i18n.Lang(); got != "zh-CN" {
		t.Fatalf("Lang() = %q, want zh-CN", got)
	}
	if got := i18n.T("aaaaaaaa", "english"); got != "中文" {
		t.Fatalf("T = %q, want 中文", got)
	}
}

// TestInitEnglishNeedsNoCatalog: --lang=en must succeed even though no en.noml is
// installed as a runtime catalog.
func TestInitEnglishNeedsNoCatalog(t *testing.T) {
	if err := Init(Options{Lang: "en", LangSet: true}); err != nil {
		t.Fatalf("Init(en): %v", err)
	}
	if got := i18n.Lang(); got != "en" {
		t.Fatalf("Lang() = %q, want en", got)
	}
}
