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
func embeddedTags() []string {
	entries, err := localesFS.ReadDir("locales")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".noml") || IsDocCatalogName(name) {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".noml"))
	}
	sort.Strings(out)
	return out
}

// Locales lists the built-in catalog tags (the files actually embedded), sorted.
// It excludes aliases and doc catalogs, so it is the right set for a
// per-catalog validation gate.
func Locales() []string { return embeddedTags() }
