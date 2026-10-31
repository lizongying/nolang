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
//   - en.doc.noml is SEEDED, then hand-maintained. A doc comment's text lives in
//     the .no source, not at a call site, and English is a translation TARGET for
//     it; re-seeding every run would silently revert every translation. New
//     anchors are seeded from the comment text and existing values are preserved.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lizongying/nolang/i18n/catalog"
	"github.com/lizongying/nolang/i18n/docscan"
	"github.com/lizongying/nolang/i18n/msgscan"
)

func main() {
	root := flag.String("src", ".", "source root to scan (the src/ directory)")
	out := flag.String("o", filepath.Join("i18n", "catalog", "locales", "en.noml"), "message catalog output path")
	docOut := flag.String("doc-o", filepath.Join("i18n", "catalog", "locales", "en"+catalog.DocCatalogSuffix), "doc catalog output path")
	check := flag.Bool("check", false, "verify the outputs are fresh instead of writing them")
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
	docs, err := scanDocs(*root, *docOut)
	if err != nil {
		fail(err)
	}

	renderedMessages := catalog.RenderEn(texts)
	renderedDocs := catalog.RenderDoc(docs)

	if *check {
		if err := checkFresh(*out, renderedMessages); err != nil {
			fail(err)
		}
		if err := checkFresh(*docOut, renderedDocs); err != nil {
			fail(err)
		}
		fmt.Printf("%s is fresh (%d messages)\n", *out, len(texts))
		fmt.Printf("%s is fresh (%d doc anchors)\n", *docOut, len(docs))
		return
	}

	if err := write(*out, renderedMessages); err != nil {
		fail(err)
	}
	if err := write(*docOut, renderedDocs); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s: %d messages\n", *out, len(texts))
	fmt.Printf("wrote %s: %d doc anchors", *docOut, len(docs))
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

// scanDocs scans the .no corpus and merges the result with whatever the committed
// doc catalog already says. A value is PRESERVED once it exists (see
// catalog.MergeDocTranslations): its source text is a comment, not a Go literal,
// so re-seeding would revert translations on every run.
func scanDocs(root, out string) (map[string]string, error) {
	entries, err := docscan.Scan(root)
	if err != nil {
		return nil, fmt.Errorf("scanning doc links: %w", err)
	}
	scanned, err := docscan.Texts(entries)
	if err != nil {
		return nil, err
	}
	existing := map[string]string{}
	if data, err := os.ReadFile(out); err == nil {
		if m, perr := catalog.ParseCatalogData(out, data); perr == nil {
			existing = m
		}
	}
	return catalog.MergeDocTranslations(scanned, existing), nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "genen: %v\n", err)
	os.Exit(1)
}
