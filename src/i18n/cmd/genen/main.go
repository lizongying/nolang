// Command genen regenerates the English message catalog from i18n call sites.
//
// Run it via `make i18n` from the repository root, or
// `cd src && go run ./i18n/cmd/genen`. The output is committed; the catalog gate
// (`go test ./i18n/catalog/`) fails when the committed file is not byte-fresh,
// which is what keeps translators from working off a stale template.
//
// English is never read at runtime — i18n.T returns the inline default when the
// language is English — so this file is a template and a review aid, not an
// input to the binary.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lizongying/nolang/i18n/catalog"
	"github.com/lizongying/nolang/i18n/msgscan"
)

func main() {
	root := flag.String("src", ".", "source root to scan (the src/ directory)")
	out := flag.String("o", filepath.Join("i18n", "catalog", "locales", "en.noml"), "output path")
	check := flag.Bool("check", false, "verify the output is fresh instead of writing it")
	flag.Parse()

	res, err := msgscan.Scan(*root)
	if err != nil {
		fail(err)
	}
	texts, err := msgscan.Texts(res)
	if err != nil {
		fail(err)
	}

	rendered := catalog.RenderEn(texts)

	if *check {
		cur, err := os.ReadFile(*out)
		if err != nil {
			fail(fmt.Errorf("reading %s: %w", *out, err))
		}
		if string(cur) != rendered {
			fail(fmt.Errorf("%s is stale: run `make i18n`", *out))
		}
		fmt.Printf("%s is fresh (%d messages)\n", *out, len(texts))
		return
	}

	if err := os.WriteFile(*out, []byte(rendered), 0o644); err != nil {
		fail(fmt.Errorf("writing %s: %w", *out, err))
	}
	fmt.Printf("wrote %s: %d messages", *out, len(texts))
	if res.Pending > 0 {
		fmt.Printf(" (%d call sites still carry %s — run scripts/stamp_traceid.sh)", res.Pending, msgscan.Placeholder)
	}
	fmt.Println()
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "genen: %v\n", err)
	os.Exit(1)
}
