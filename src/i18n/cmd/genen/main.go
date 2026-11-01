// Command genen regenerates the English message catalog from i18n call sites,
// and the English doc catalog from anchored source comments.
//
// Run it via `make i18n` from the repository root, or
// `cd src && go run ./i18n/cmd/genen`. Both outputs are committed; the catalog
// gate (`go test ./i18n/catalog/`) fails when a committed file is not byte-fresh,
// which is what keeps translators from working off a stale template.
//
// The two outputs have opposite ownership rules, which is why they are separate
// files:
//
//   - en.noml is fully GENERATED. A message's English text is the inline default
//     at the Go call site, so the generator is the only writer. It is never read
//     at runtime (T returns the inline default for English); it is a translator
//     template and a review aid.
//   - en-doc.noml is HAND-WRITTEN and only PRUNED. A doc comment's text lives in
//     the .no source, not at a call site, and English is a translation TARGET for
//     it. Seeding every anchor with its source text would fill the English
//     catalog with Chinese, so an untranslated anchor stays absent and falls back
//     to the comment at runtime. `-doc-missing` lists what is left to translate.
//
// A third output, en-alias.noml, holds the localized display names (别名) of
// every std function/method, keyed by the same doc anchor but scoped to
// src/std. Unlike doc catalogs an alias catalog is expected to be COMPLETE —
// the key domain is finite and machine-checkable — and `-alias-missing` lists
// the std anchors still without a name. Values are hand-written; the generator
// only prunes keys that no longer exist.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/i18n/catalog"
	"github.com/lizongying/nolang/i18n/docscan"
	"github.com/lizongying/nolang/i18n/msgscan"
)

func main() {
	root := flag.String("src", ".", "source root to scan (the src/ directory)")
	out := flag.String("o", filepath.Join("i18n", "catalog", "locales", "en.noml"), "message catalog output path")
	docOut := flag.String("doc-o", filepath.Join("i18n", "catalog", "locales", catalog.DocCatalogName("en")), "doc catalog output path")
	aliasOut := flag.String("alias-o", filepath.Join("i18n", "catalog", "locales", catalog.AliasCatalogName("en")), "alias catalog output path")
	check := flag.Bool("check", false, "verify the outputs are fresh instead of writing them")
	listMissing := flag.Bool("doc-missing", false, "print the anchors that have no doc translation yet, then exit")
	listAliasMissing := flag.Bool("alias-missing", false, "print the std anchors that have no alias yet, then exit")
	flag.Parse()

	res, err := msgscan.Scan(*root)
	if err != nil {
		fail(err)
	}
	texts, err := msgscan.Texts(res)
	if err != nil {
		fail(err)
	}

	// Doc links live in .no sources, not at Go call sites, so they come from a
	// second scan and land in their own catalog file.
	docs, missing, err := scanDocs(*root, *docOut)
	if err != nil {
		fail(err)
	}

	if *listMissing {
		for _, anchor := range missing {
			fmt.Println(anchor)
		}
		fmt.Fprintf(os.Stderr, "%d anchor(s) untranslated, %d translated\n", len(missing), len(docs))
		return
	}

	aliases, aliasMissing, err := scanAliases(*root, *aliasOut)
	if err != nil {
		fail(err)
	}
	if *listAliasMissing {
		for _, anchor := range aliasMissing {
			fmt.Println(anchor)
		}
		fmt.Fprintf(os.Stderr, "%d std anchor(s) without an alias, %d aliased\n", len(aliasMissing), len(aliases))
		return
	}

	renderedMessages := catalog.RenderEn(texts)
	renderedDocs := catalog.RenderDoc("en", docs)
	renderedAliases := catalog.RenderAlias("en", aliases)

	if *check {
		if err := checkFresh(*out, renderedMessages); err != nil {
			fail(err)
		}
		if err := checkFresh(*docOut, renderedDocs); err != nil {
			fail(err)
		}
		if err := checkFresh(*aliasOut, renderedAliases); err != nil {
			fail(err)
		}
		fmt.Printf("%s is fresh (%d messages)\n", *out, len(texts))
		fmt.Printf("%s is fresh (%d of %d doc anchors translated)\n", *docOut, len(docs), len(docs)+len(missing))
		fmt.Printf("%s is fresh (%d of %d std anchors aliased)\n", *aliasOut, len(aliases), len(aliases)+len(aliasMissing))
		return
	}

	if err := write(*out, renderedMessages); err != nil {
		fail(err)
	}
	if err := write(*docOut, renderedDocs); err != nil {
		fail(err)
	}
	if err := write(*aliasOut, renderedAliases); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s: %d messages\n", *out, len(texts))
	fmt.Printf("wrote %s: %d of %d doc anchors translated\n", *docOut, len(docs), len(docs)+len(missing))
	fmt.Printf("wrote %s: %d of %d std anchors aliased", *aliasOut, len(aliases), len(aliases)+len(aliasMissing))
	if res.Pending > 0 {
		fmt.Printf(" (%d call sites still carry %s — run scripts/stamp_traceid.sh)", res.Pending, msgscan.Placeholder)
	}
	fmt.Println()
}

func write(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func checkFresh(path, want string) error {
	cur, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if string(cur) != want {
		return fmt.Errorf("%s is stale: run `make i18n`", path)
	}
	return nil
}

// scanDocs scans the .no corpus and prunes whatever the committed doc catalog
// already says down to anchors that still exist. Nothing is seeded: see
// catalog.PruneDocTranslations for why an untranslated anchor must stay absent
// rather than become a copy of its source text.
func scanDocs(root, out string) (map[string]string, []string, error) {
	entries, err := docscan.Scan(root)
	if err != nil {
		return nil, nil, fmt.Errorf("scanning doc links: %w", err)
	}
	scanned, err := docscan.Texts(entries)
	if err != nil {
		return nil, nil, err
	}
	existing := map[string]string{}
	if data, err := os.ReadFile(out); err == nil {
		if m, perr := catalog.ParseCatalogData(out, data); perr == nil {
			existing = m
		}
	}
	return catalog.PruneDocTranslations(scanned, existing),
		catalog.MissingDocTranslations(scanned, existing), nil
}

// scanAliases prunes the committed alias catalog down to anchors the std
// sources still declare. The scope is `src/std` on purpose: an alias names a
// public API surface, and only std functions have that surface.
func scanAliases(root, out string) (map[string]string, []string, error) {
	entries, err := docscan.Scan(root)
	if err != nil {
		return nil, nil, fmt.Errorf("scanning doc links: %w", err)
	}
	scanned := map[string]bool{}
	stdPrefix := "std" + string(filepath.Separator)
	for _, e := range entries {
		rel, rerr := filepath.Rel(root, e.File)
		if rerr == nil && strings.HasPrefix(rel, stdPrefix) {
			scanned[e.Anchor] = true
		}
	}
	existing := map[string]string{}
	if data, err := os.ReadFile(out); err == nil {
		if m, perr := catalog.ParseCatalogData(out, data); perr == nil {
			existing = m
		}
	}
	return catalog.PruneAliasTranslations(scanned, existing),
		catalog.MissingAliasTranslations(scanned, existing), nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "genen: %v\n", err)
	os.Exit(1)
}
