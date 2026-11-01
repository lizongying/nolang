package i18n

import (
	"errors"
	"testing"
)

// TestAliasTranslatesEveryLanguage mirrors TestDocTranslatesIntoEnglish: the
// alias table is keyed by doc anchors and, like Doc, "en" is a real lookup
// target — the identifier fallback is not the same as a translation.
func TestAliasTranslatesEveryLanguage(t *testing.T) {
	reset(t)
	defer reset(t)

	SetLang(defaultLang)
	SetAliasCatalog(map[string]string{"args.get": "Get argument"})
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
	SetAliasCatalog(map[string]string{"args.get": "获取参数"})
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
		AliasLoad: func(tag string) (map[string]string, error) {
			aliasLoads++
			if tag != defaultLang {
				t.Fatalf("AliasLoad(%q), want %q", tag, defaultLang)
			}
			return map[string]string{"sse.server.close": "Close SSE server"}, nil
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
		AliasLoad: func(string) (map[string]string, error) { return nil, errors.New("boom") },
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

	SetAliasCatalog(map[string]string{"args.get": "获取参数"})
	SetLang("en")
	if got := Alias("args.get"); got != "get" {
		t.Fatalf("Alias after SetLang = %q, want the identifier fallback", got)
	}
}
