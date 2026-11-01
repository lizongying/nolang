package catalog

import (
	"embed"
	"sort"
	"strings"
)

// localesFS carries the built-in catalogs so `no --lang=zh-CN` works out of the
// box on any machine, with no install step and no writable state. A project's
// own `locales/<tag>.noml` is read from disk first and wins.
//
//go:embed locales/*.noml
var localesFS embed.FS

// embeddedTags lists the tags baked into the binary, sorted for determinism.
//
// File names are lower-case (`zh-cn.noml`) while tags are not (`zh-CN`), so each
// name goes back through Canonical; see LocaleFileName.
func embeddedTags() []string {
	entries, err := localesFS.ReadDir("locales")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".noml") ||
			IsDocCatalogName(name) || IsAliasCatalogName(name) {
			continue
		}
		out = append(out, Canonical(strings.TrimSuffix(name, ".noml")))
	}
	sort.Strings(out)
	return out
}

// Locales lists the built-in catalog tags (the files actually embedded), sorted.
// It excludes aliases and doc catalogs, so it is the right set for a
// per-catalog validation gate.
func Locales() []string { return embeddedTags() }

// AliasLocales lists the tags that ship an alias catalog (`<tag>-alias.noml`),
// sorted. It is the mirror of Locales for the alias family: the tags are the
// same seven, but the gate reads this set so a missing alias file fails loudly
// instead of silently falling back to the identifier.
func AliasLocales() []string {
	entries, err := localesFS.ReadDir("locales")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !IsAliasCatalogName(name) {
			continue
		}
		tag := strings.TrimSuffix(name, AliasCatalogSuffix)
		out = append(out, Canonical(tag))
	}
	sort.Strings(out)
	return out
}
