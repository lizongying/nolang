package dump

// ─────────────────────────────────────────────────────────────────────────────
// Gates for migration step S1 of docs/design/nolang-parser-lowering-invariants.md.
//
// S1 needs the lowering pass to be *movable*: the checker has to see the
// sugar-bearing AST, so ParseProgram must be able to hand back an unlowered tree
// and the caller must be able to lower it afterwards. That refactor is only safe
// if the deferred route is provably identical to today's route — which is what
// TestSkipLoweringMatchesDefaultPath establishes, over the whole corpus.
//
// It is the "control group" discipline: one frozen tree, two code paths that
// differ only in *when* lowering runs, compared byte for byte. A difference in
// the AST dump, or in the diagnostics, fails here with the offending file named.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parserTierSugarTypes are the sugar node types the parser's own lowering pass is
// supposed to expand before ParseProgram returns (tier "removedByParser" in
// src/parser/sugar_seam_test.go). They are listed here rather than shared because
// this package cannot import the parser's internal test registry.
//
// This list is what stops the equivalence test below from being symmetric, and it
// is not a theoretical concern: with `p.lowerProgram(program)` temporarily
// commented out of LowerSurface, a comparison-only version of the test passed,
// because both routes then skipped lowering and produced the same sugar-bearing
// tree. The gate has to assert that the common result is the LOWERED form.
var parserTierSugarTypes = []string{"SurfaceMatch", "UnwrapAssignStatement"}

// TestSkipLoweringMatchesDefaultPath pins the S1 seam: parsing with
// Parser.SkipLowering and then calling (*Parser).LowerSurface explicitly must
// produce exactly what today's ParseProgram produces on its own — same tree, same
// warnings — and that shared result must actually be lowered.
func TestSkipLoweringMatchesDefaultPath(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not found at %s: %v", corpusRoot, err)
	}

	var checked, nonEmpty, withSugar int
	walkErr := filepath.WalkDir(corpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Route 1 — today's default: ParseProgram lowers before it returns.
		p1 := parser.New(lexer.NewCached(path, string(src)))
		prog1 := p1.ParseProgram()
		if len(p1.Errors()) > 0 {
			return nil // deliberate error cases; neither route is meaningful
		}

		// Route 2 — the seam: parse without lowering, then lower explicitly.
		p2 := parser.New(lexer.NewCached(path, string(src)))
		p2.SkipLowering = true
		prog2 := p2.ParseProgram()
		if len(p2.Errors()) > 0 {
			// Skipping lowering can only *remove* diagnostics, so a file that was
			// clean on route 1 must stay clean here.
			t.Errorf("%s: the deferred route reported %d error(s) the default route did "+
				"not: %v", path, len(p2.Errors()), p2.Errors())
			return nil
		}

		// Anti-vacuity, half 1: SkipLowering must have been honoured. Without this
		// the test cannot tell "the seam defers lowering" from "nobody lowers".
		deferredDump := Dump(prog2)
		hadSugar := false
		for _, typ := range parserTierSugarTypes {
			if strings.Contains(deferredDump, typ) {
				hadSugar = true
				break
			}
		}
		if hadSugar {
			withSugar++
		}

		p2.LowerSurface(prog2)
		got := Dump(prog2)

		// Anti-vacuity, half 2: the shared result must be the lowered form.
		for _, typ := range parserTierSugarTypes {
			if strings.Contains(got, typ) {
				t.Errorf("%s: after LowerSurface the tree still contains %s, so the "+
					"comparison below is between two unlowered trees and proves nothing",
					path, typ)
				return nil
			}
		}

		want := Dump(prog1)
		if got != want {
			t.Errorf("%s: SkipLowering + LowerSurface produced a different AST than "+
				"ParseProgram\n%s", path, firstDiff(want, got))
			return nil
		}

		// Program{warnings: N} is part of the dump only when N is non-zero, so an
		// all-zero-warnings corpus would let a diagnostics regression through.
		// Compare them explicitly as well.
		if w1, w2 := strings.Join(prog1.Warnings, "\n"), strings.Join(prog2.Warnings, "\n"); w1 != w2 {
			t.Errorf("%s: warnings differ between the two routes\n  default: %q\n  deferred: %q",
				path, w1, w2)
			return nil
		}

		checked++
		if want != "" {
			nonEmpty++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", corpusRoot, walkErr)
	}

	if nonEmpty < 100 {
		t.Fatalf("only %d of %d programs compared a non-empty dump; an empty-vs-empty "+
			"comparison would pass vacuously", nonEmpty, checked)
	}
	// If the corpus happened to contain no parser-tier sugar at all, every
	// assertion above would hold trivially.
	if withSugar < 100 {
		t.Fatalf("only %d of %d programs still contained sugar after the deferred parse; "+
			"either SkipLowering is not being honoured or the corpus stopped exercising "+
			"the sugar", withSugar, checked)
	}
	t.Logf("%d programs: SkipLowering + LowerSurface == ParseProgram byte-for-byte, "+
		"warnings included (%d non-empty, %d carried sugar into the deferred route)",
		checked, nonEmpty, withSugar)
}

// sugarProbes are minimal sources that each contain exactly one kind of sugar
// node, used by the C8 census below. They are kept here rather than in the
// parser package because this test needs the dumper.
var sugarProbes = map[string]string{
	"match":        "v = 1\nv: {\n    1 -> print('one')\n    -> print('other')\n}\n",
	"parens":       "x = (1 + 2)\n",
	"multi-assign": "f = () (a i64, b i64) {\n    a = 1\n    b = 2\n}\nx, y = f()\n",
	"unwrap":       "g = () (r ?i64) {\n    x ?= h()\n    r = 1\n}\n",
}

// TestSugarASTIsDumpable is the census for constraint C8: `no ast` must be able
// to render every sugar node.
//
// It matters now, not later: the dump is the L2 oracle for the self-hosting
// effort (see the package doc), and `no ast` currently only ever sees a lowered
// tree — which is exactly why the gap below has stayed invisible. The moment
// lowering moves out of ParseProgram (S1), `no ast` starts seeing sugar, and any
// type missing from dump.go's switch degrades to `<unknown %T>` and makes its
// whole subtree invisible to the oracle.
//
// This is a tripwire, not an aspiration: it asserts the gap that exists TODAY, so
// that closing it (or opening a new one) cannot happen silently.
func TestSugarASTIsDumpable(t *testing.T) {
	// Sugar node types dump.go cannot render yet, with the reason.
	//
	// EMPTY as of 2026-10-16: *parser.SurfaceMatch used to be exempt because
	// dump.go had no case for it. It gained one (plus a matchArm helper), so the
	// whole arm subtree — including ValueCtx and Body, the two fields the
	// desugar reads and writes — is now visible to `no ast`. This list must stay
	// empty: every sugar type is renderable, and adding a sugar node without a
	// dump case is what makes its subtree invisible to the L2 oracle.
	knownUnrenderable := map[string]string{}

	found := map[string]bool{}
	for name, src := range sugarProbes {
		p := parser.New(lexer.NewCached(name+".no", src))
		p.SkipLowering = true
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("probe %q no longer parses: %v", name, errs)
		}

		out := Dump(prog)
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "<unknown ") {
				continue
			}
			typ := strings.TrimSuffix(strings.TrimPrefix(line, "<unknown "), ">")
			found[typ] = true
		}

		// Sanity: the probe must actually produce sugar, otherwise it exercises
		// nothing and its `<unknown>` (or absence thereof) is meaningless.
		if !strings.Contains(out, "SurfaceMatch") && !strings.Contains(out, "GroupedExpression") &&
			!strings.Contains(out, "UnwrapAssignStatement") && !strings.Contains(out, "MultiAssignStatement") {
			t.Errorf("probe %q dumped no recognisable sugar node; it no longer exercises "+
				"what this test claims\n%s", name, out)
		}
	}

	var unexpected []string
	for typ := range found {
		if _, ok := knownUnrenderable[typ]; !ok {
			unexpected = append(unexpected, typ)
		}
	}
	for typ, why := range knownUnrenderable {
		if !found[typ] {
			t.Errorf("dump.go can now render %s, so the known-gap entry is stale — delete "+
				"it (C8 is closed for that type). Reason it used to be exempt: %s", typ, why)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("sugar node type(s) that dump.go cannot render and that are not in the "+
			"known-gap list: %v\nadd a case to dump.go (constraint C8: `no ast` must dump "+
			"every sugar node)", unexpected)
	}
	t.Logf("dump.go cannot render %d of the sugar node types: %v", len(found), found)
}
