package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// S4 harness, second consumer: the LSP symbol index.
//
// The LSP parses each document once and uses that one tree for three things:
// format-on-save (fmt.FormatProgram), the symbol index (ASTWalker) and the
// diagnostics (checker). The formatter half is gated in src/fmt; the diagnostics
// half in sugar_lint_ab_test.go; this file gates the *index* half, which is a
// completely different consumer with a completely different failure mode:
// ASTWalker.walkExpression is a hand-written `switch` with no default, so any
// node type it does not name makes that whole subtree invisible — no error, just
// silence (the §15.2(1) pattern).
//
// Under the sugar shape every match is a *parser.SurfaceMatch, so without a case
// for it every symbol referenced or declared inside a match arm vanishes from
// hover / go-to-definition / find-references.
//
// §4.3（2026-10-17）已刪除 surface 形態與其 A/B 比較門禁；本檔由「sugar vs
// surface」A/B 改為「糖形態整體語料冒煙測試」——索引在 532 份語料上（糖形態）不
// 崩潰、每檔都能建出符號表。

const sugarLspCorpusRoot = "../../tests"

func sugarLspCorpus(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(sugarLspCorpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", sugarLspCorpusRoot, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .no files under %s — refusing to report a vacuous 0-differ", sugarLspCorpusRoot)
	}
	sort.Strings(files)
	return files
}

// lspCfgSugar is the sole non-compile parse shape: nothing is expanded.
var lspCfgSugar = func(p *parser.Parser) {
	p.SkipLowering = true
}

// lspIndexFingerprint builds the symbol index the way ParseDocument does
// (minus the auto-imported module exports, which are shape-independent) and
// returns a canonical, sorted fingerprint of everything the index remembers.
func lspIndexFingerprint(src string, cfg func(*parser.Parser)) (string, bool) {
	l := lexer.New(src)
	p := parser.New(l)
	cfg(p)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 || prog == nil {
		return "", false
	}
	doc := createTestDocument(src)
	index := NewSymbolIndex(doc.Item.URI, 1)
	index.AddBuiltinSymbols()
	NewASTWalker(index, doc, prog).Walk()

	var lines []string
	emit := func(kind, name string, e *IndexEntry) {
		if e == nil {
			return
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%v\t%s\t%v\t%s",
			kind, name, e.Kind, e.Type, e.Location.Range, e.Scope))
	}
	for name, e := range index.symbols {
		emit("sym", name, e)
	}
	for name, e := range index.functions {
		emit("fn", name, e)
	}
	for name, e := range index.definitions {
		emit("def", name, e)
	}
	for name, entries := range index.declarations {
		for _, e := range entries {
			emit("decl", name, e)
		}
	}
	for name, locs := range index.references {
		for _, loc := range locs {
			lines = append(lines, fmt.Sprintf("ref\t%s\t%v", name, loc.Range))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), true
}

// lspIndexNames reduces a fingerprint to the set of names the index knows
// about. This is what the gate cares about most: the difference between "hover
// works inside a match" and "hover silently returns nothing", and it is exactly
// what regressed before ASTWalker learned about SurfaceMatch (158/532 files lost
// entries).
func lspIndexNames(fp string) []string {
	seen := map[string]bool{}
	for _, l := range strings.Split(fp, "\n") {
		if l == "" {
			continue
		}
		fields := strings.Split(l, "\t")
		if len(fields) < 2 {
			continue
		}
		seen[fields[1]] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestLspIndexSugarCorpusCensus is the instrument: it walks the whole corpus
// under the sugar shape and logs parse-success / symbol-count stats. Never fails.
func TestLspIndexSugarCorpusCensus(t *testing.T) {
	files := sugarLspCorpus(t)
	var ok, unparseable, totalNames int
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		fp, okFp := lspIndexFingerprint(string(b), lspCfgSugar)
		if !okFp {
			unparseable++
			continue
		}
		ok++
		totalNames += len(lspIndexNames(fp))
	}
	t.Logf("lsp-index sugar-shape census: corpus=%d parseOK=%d unparseable=%d totalSymbolNames=%d",
		len(files), ok, unparseable, totalNames)
}

// TestLspIndexSugarCorpusSmoke gates the LSP index flip at the corpus level:
// the symbol index must build for every corpus file under the sugar shape
// without panicking or being rejected. A parse failure here means the walker
// cannot handle the sugar tree on real code.
func TestLspIndexSugarCorpusSmoke(t *testing.T) {
	files := sugarLspCorpus(t)
	var offenders []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, okFp := lspIndexFingerprint(string(b), lspCfgSugar); !okFp {
			offenders = append(offenders, path)
		}
	}
	if len(offenders) == 0 {
		t.Logf("%d files: LSP symbol index builds cleanly under the sugar shape", len(files))
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	t.Errorf("LSP symbol index fails to build on %d/%d corpus files: %s", len(offenders), len(files), strings.Join(shown, ", "))
}
