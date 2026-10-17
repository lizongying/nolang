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
// diagnostics (checker). The formatter half is already gated in src/fmt; this
// file gates the *index* half, which is a completely different consumer with a
// completely different failure mode: ASTWalker.walkExpression is a hand-written
// `switch` with no default, so any node type it does not name makes that whole
// subtree invisible — no error, just silence (the §15.2(1) pattern).
//
// Under the sugar shape every match is a *parser.SurfaceMatch, so without a
// case for it every symbol referenced or declared inside a match arm vanishes
// from hover / go-to-definition / find-references.
//
// Same discipline as src/fmt/sugar_fmt_ab_test.go: census first (never fails),
// hard gate second.

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

var (
	lspCfgSurface = func(p *parser.Parser) {
		p.SkipUnwrapLowering = true
		p.SkipSafeIndexLowering = true
	}
	lspCfgSugar = func(p *parser.Parser) {
		p.SkipLowering = true
	}
)

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

// TestLspIndexSugarVsSurfaceCensus is the instrument.
func TestLspIndexSugarVsSurfaceCensus(t *testing.T) {
	files := sugarLspCorpus(t)
	var same, differ, unparseable int
	var diffPaths []string
	onlySurface := map[string]int{}
	onlySugar := map[string]int{}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		a, aok := lspIndexFingerprint(string(b), lspCfgSurface)
		c, cok := lspIndexFingerprint(string(b), lspCfgSugar)
		if !aok || !cok {
			unparseable++
			continue
		}
		if a != c {
			differ++
			diffPaths = append(diffPaths, path)
		} else {
			same++
		}
		an, cn := lspIndexNames(a), lspIndexNames(c)
		sa, sc := map[string]bool{}, map[string]bool{}
		for _, n := range an {
			sa[n] = true
		}
		for _, n := range cn {
			sc[n] = true
		}
		for _, n := range an {
			if !sc[n] {
				onlySurface[n]++
			}
		}
		for _, n := range cn {
			if !sa[n] {
				onlySugar[n]++
			}
		}
	}
	t.Logf("lsp-index A/B: corpus=%d identical=%d differ=%d unparseable=%d",
		len(files), same, differ, unparseable)
	pairs := []struct {
		p string
		m map[string]int
	}{{"only-surface", onlySurface}, {"only-sugar", onlySugar}}
	for _, pr := range pairs {
		prefix, m := pr.p, pr.m
		if len(m) == 0 {
			continue
		}
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		if len(ks) > 15 {
			t.Logf("  %s (%d distinct names): %s …", prefix, len(ks), strings.Join(ks[:15], ", "))
		} else {
			t.Logf("  %s (%d distinct names): %s", prefix, len(ks), strings.Join(ks, ", "))
		}
	}
	shown := diffPaths
	if len(shown) > 12 {
		shown = shown[:12]
	}
	for _, p := range shown {
		t.Logf("  DIFF %s", p)
	}
}

// lspIndexNames reduces a fingerprint to the set of names the index knows
// about. This — not the full entry dump — is what the gate compares, and the
// reason is deliberate:
//
// The full entries carry `Type` and `Range`, and those legitimately differ
// between the two shapes. Under the surface shape the arm bindings
// (`ok(v)` → v) and the implicit `it` are *synthetic let statements* injected
// by lowering, so the index sees them with lowering's inferred type but with a
// synthetic, often degenerate range (e.g. `{{3 3} {3 0}}`, end before start).
// Under the sugar shape they are registered from MatchArm metadata with the
// arm's real source position and no type. Both are "correct" for their shape;
// demanding byte equality here would just force the sugar shape to reproduce
// lowering's artifacts.
//
// What must NOT differ is *which names the index knows about*: that is the
// difference between "hover works inside a match" and "hover silently returns
// nothing", and it is exactly what regressed before ASTWalker learned about
// SurfaceMatch (158/532 files lost entries).
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

// TestLspIndexSugarMatchesSurface is the gate for flipping the LSP's parse
// shape: the index must know about exactly the same set of names under both
// shapes. A missing name means real user-visible loss (hover /
// go-to-definition / find-references inside a match), not a formatting
// preference. See lspIndexNames for why names and not full entries.
func TestLspIndexSugarMatchesSurface(t *testing.T) {
	files := sugarLspCorpus(t)
	var offenders []string
	var samples []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		a, aok := lspIndexFingerprint(string(b), lspCfgSurface)
		c, cok := lspIndexFingerprint(string(b), lspCfgSugar)
		if !aok || !cok {
			continue
		}
		an, cn := lspIndexNames(a), lspIndexNames(c)
		sc := map[string]bool{}
		for _, n := range cn {
			sc[n] = true
		}
		// Gate direction: the sugar shape must not LOSE a name. Sugar-only
		// extras are fine and are logged by the census (they are the implicit
		// `it`, which the desugared tree only sometimes materialises, and
		// type-match pattern names like `i32` that only exist in the sugar
		// tree). `__`-prefixed names are lowering's synthetic temporaries
		// (`__match_subj_L_C` …); dropping those is the point of S4.
		var lost []string
		for _, n := range an {
			if !sc[n] && !strings.HasPrefix(n, "__") {
				lost = append(lost, n)
			}
		}
		if len(lost) == 0 {
			continue
		}
		offenders = append(offenders, path)
		if len(samples) < 3 {
			var only []string
			for _, n := range lost {
				only = append(only, "  lost-under-sugar "+n)
			}
			if len(only) > 8 {
				only = only[:8]
			}
			samples = append(samples, path+"\n"+strings.Join(only, "\n"))
		}
	}
	if len(offenders) == 0 {
		t.Logf("%d files: LSP symbol index is identical under both parse shapes", len(files))
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	t.Errorf("LSP symbol index differs on %d/%d corpus files: %s\nfirst differences:\n%s",
		len(offenders), len(files), strings.Join(shown, ", "), strings.Join(samples, "\n"))
}
