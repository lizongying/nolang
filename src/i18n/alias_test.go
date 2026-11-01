package i18n

import (
	"errors"
	"testing"
)

// entry builds a one-alias row for the tests. Keys/EnKeys are filled in because
// a real catalog row always carries them (the parser rejects a row without
// them); the values themselves do not matter unless a test reads them.
func entry(display string) AliasEntry {
	return AliasEntry{Alias: display, Keys: "x", EnKeys: "e"}
}

// TestAliasTranslatesEveryLanguage mirrors TestDocTranslatesIntoEnglish: the
// alias table is keyed by doc anchors and, like Doc, "en" is a real lookup
// target — the identifier fallback is not the same as a translation.
func TestAliasTranslatesEveryLanguage(t *testing.T) {
	reset(t)
	defer reset(t)

	SetLang(defaultLang)
	SetAliasCatalog(map[string]AliasEntry{"args.get": entry("Get argument")})
	defer SetAliasCatalog(nil)

	if got := Alias("args.get"); got != "Get argument" {
		t.Fatalf("Alias under en = %q, want the English alias", got)
	}
}

// TestAliasFallsBackToIdentifier is the degradation contract: an anchor with no
// alias entry shows its final dotted segment — the very label the source
// comment already writes — and never an empty string or the full anchor.
func TestAliasFallsBackToIdentifier(t *testing.T) {
	reset(t)
	defer reset(t)
	SetAliasCatalog(map[string]AliasEntry{"args.get": entry("获取参数")})
	defer SetAliasCatalog(nil)

	if got := Alias("args.nope"); got != "nope" {
		t.Fatalf("Alias(missing) = %q, want the identifier fallback", got)
	}
	if got := Alias("arr.arr-stack-pop"); got != "arr-stack-pop" {
		t.Fatalf("Alias(missing, nested) = %q, want the last segment", got)
	}
	if got := Alias(""); got != "" {
		t.Fatalf("Alias(empty) = %q, want empty", got)
	}
}

// TestIdentifierOf pins the fallback spelling independently, because hover
// compares Alias against it to decide whether a real translation exists.
func TestIdentifierOf(t *testing.T) {
	for anchor, want := range map[string]string{
		"args.get":          "get",
		"arr.arr-stack-pop": "arr-stack-pop",
		"no-dot-at-all":     "no-dot-at-all",
	} {
		if got := IdentifierOf(anchor); got != want {
			t.Errorf("IdentifierOf(%q) = %q, want %q", anchor, got, want)
		}
	}
}

// TestInitInstallsAliasCatalogForEnglish proves Init wires AliasLoad for "en"
// too — same rule as DocLoad.
func TestInitInstallsAliasCatalogForEnglish(t *testing.T) {
	reset(t)
	defer reset(t)

	aliasLoads := 0
	err := Init(Options{
		Lang: "en", LangSet: true,
		Env: fakeEnv(nil),
		AliasLoad: func(tag string) (map[string]AliasEntry, error) {
			aliasLoads++
			if tag != defaultLang {
				t.Fatalf("AliasLoad(%q), want %q", tag, defaultLang)
			}
			return map[string]AliasEntry{"sse.server.close": entry("Close SSE server")}, nil
		},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if aliasLoads != 1 {
		t.Fatalf("AliasLoad called %d times, want 1", aliasLoads)
	}
	if got := Alias("sse.server.close"); got != "Close SSE server" {
		t.Fatalf("Alias = %q, want the English alias", got)
	}
}

// TestInitDegradesOnBrokenAliasCatalog: like docs, a malformed alias file must
// degrade to identifiers with a warning, never stop the toolchain.
func TestInitDegradesOnBrokenAliasCatalog(t *testing.T) {
	reset(t)
	defer reset(t)

	var warns []string
	err := Init(Options{
		Lang: "en", LangSet: true,
		Env:       fakeEnv(nil),
		AliasLoad: func(string) (map[string]AliasEntry, error) { return nil, errors.New("boom") },
		Warn:      func(m string) { warns = append(warns, m) },
	})
	if err != nil {
		t.Fatalf("Init returned %v; a broken alias catalog must degrade, not fail", err)
	}
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
	if got := Alias("args.get"); got != "get" {
		t.Fatalf("Alias = %q, want the identifier fallback", got)
	}
}

// TestSetLangDropsAliasCatalog keeps the three tables in step: switching
// language without reloading must not leak the previous language's aliases.
func TestSetLangDropsAliasCatalog(t *testing.T) {
	reset(t)
	defer reset(t)

	SetAliasCatalog(map[string]AliasEntry{"args.get": entry("获取参数")})
	SetLang("en")
	if got := Alias("args.get"); got != "get" {
		t.Fatalf("Alias after SetLang = %q, want the identifier fallback", got)
	}
	if got := AliasEntries(); len(got) != 0 {
		t.Fatalf("AliasEntries after SetLang = %v, want empty", got)
	}
}

// TestAliasUsesFirstAlias pins the array contract: several synonyms may be
// listed, but hover and the completion label show the first one only.
func TestAliasUsesFirstAlias(t *testing.T) {
	reset(t)
	defer reset(t)

	SetAliasCatalog(map[string]AliasEntry{
		"args.get": {Alias: "获取参数", Keys: "hqcs", EnKeys: "g"},
	})
	if got := Alias("args.get"); got != "获取参数" {
		t.Fatalf("Alias = %q, want the first alias", got)
	}
	if got := AliasEntries(); len(got) != 1 || got[0].Display() != "获取参数" {
		t.Fatalf("AliasEntries = %v, want one row displaying the first alias", got)
	}
}

// TestAliasFallsBackWhenAliasListEmpty is the other half of the array contract:
// an empty list must behave exactly like a missing anchor — identifier, not "".
func TestAliasFallsBackWhenAliasListEmpty(t *testing.T) {
	reset(t)
	defer reset(t)

	SetAliasCatalog(map[string]AliasEntry{"args.get": {Keys: "x", EnKeys: "g"}})
	if got := Alias("args.get"); got != "get" {
		t.Fatalf("Alias = %q, want the identifier fallback", got)
	}
	if got := AliasEntries(); len(got) != 0 {
		t.Fatalf("AliasEntries = %v, want a row with no alias dropped", got)
	}
}

// TestSetAliasCatalogCopiesAndSorts: the table is copied (a caller mutating its
// own map afterwards cannot corrupt the installed one) and AliasEntries is
// ordered by anchor, because a completion list that reshuffles between
// keystrokes is unusable.
func TestSetAliasCatalogCopiesAndSorts(t *testing.T) {
	reset(t)
	defer reset(t)

	table := map[string]AliasEntry{
		"zip.zip.count": {Alias: "计数", Keys: "js", EnKeys: "c"},
		"args.get":      {Alias: "获取参数", Keys: "hqcs", EnKeys: "g"},
	}
	SetAliasCatalog(table)
	table["args.get"] = AliasEntry{Alias: "mutated", Keys: "zz", EnKeys: "zz"}
	table["new.anchor"] = AliasEntry{Alias: "新增", Keys: "xz", EnKeys: "xz"}

	if got := Alias("args.get"); got != "获取参数" {
		t.Fatalf("Alias = %q, want the value captured at install time", got)
	}
	got := AliasEntries()
	if len(got) != 2 || got[0].Anchor != "args.get" || got[1].Anchor != "zip.zip.count" {
		t.Fatalf("AliasEntries = %v, want ascending anchors [args.get zip.zip.count]", got)
	}
}

// TestEnKeysOf pins the derivation the gate demands of every catalog: initials
// of the identifier's hyphen-separated words, digits counted as a word, and the
// anchor's module/type prefix ignored.
func TestEnKeysOf(t *testing.T) {
	for anchor, want := range map[string]string{
		"args.get":          "g",
		"arr.arr-stack-pop": "asp",
		"aes-cbc.dec-128":   "d1",
		"no-dot-at-all":     "ndaa",
		"sse.server.close":  "c",
		"mod.underscore_io": "ui",
		"mod.weird--double": "wd",
		"CBC.Gen":           "g",
	} {
		if got := EnKeysOf(anchor); got != want {
			t.Errorf("EnKeysOf(%q) = %q, want %q", anchor, got, want)
		}
	}
	if got := EnKeysOf(""); got != "" {
		t.Errorf(`EnKeysOf("") = %q, want empty`, got)
	}
}
