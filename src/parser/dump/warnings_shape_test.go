package dump

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

// ─────────────────────────────────────────────────────────────────────────────
// S4 / §17.1: diagnostics must be SHAPE-INDEPENDENT.
//
// The other harnesses in this package compare what a *consumer* produces under
// the sugar AST vs the lowered one (tree dump, checker diagnostics). Those
// comparisons can only catch drift between the two shapes. They cannot catch a
// diagnostic that disappears from BOTH shapes at once — which is exactly what
// happened while moving the `'->' arm is unreachable` hint out of the desugar
// loop: with the parse-time pass disabled, sugar and surface both lose it and
// the sugar-vs-surface gate still passes (measured: sabotage of
// p.warnUnreachableMatchArms left the LSP lint gate green).
//
// This gate closes that hole from the other side: the parser's own diagnostics
// (Program.Warnings — the channel `no vet`, `no build` and the LSP all read)
// must be identical under SkipLowering and the default path, file by file.
//
// The parse-time warning pass lives in parser/match_warn.go.
// ─────────────────────────────────────────────────────────────────────────────

// TestParserWarningsAreShapeIndependent is the gate.
func TestParserWarningsAreShapeIndependent(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not found at %s: %v", corpusRoot, err)
	}

	var paths []string
	err := filepath.WalkDir(corpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusRoot, err)
	}
	sort.Strings(paths)
	if len(paths) < 200 {
		t.Fatalf("only %d corpus files; the comparison would be vacuous", len(paths))
	}

	compared, offenders := 0, 0
	var firstDiff string
	for _, path := range paths {
		lowered := parseCorpusFile(t, path, false)
		sugar := parseCorpusFile(t, path, true)
		if lowered == nil || sugar == nil {
			continue // does not parse cleanly in both shapes
		}
		compared++
		if strings.Join(lowered.Warnings, "\n") == strings.Join(sugar.Warnings, "\n") {
			continue
		}
		offenders++
		if firstDiff == "" {
			firstDiff = fmt.Sprintf("%s:\n  lowered: %s\n  sugar:   %s",
				path, strings.Join(lowered.Warnings, " | "), strings.Join(sugar.Warnings, " | "))
		}
	}
	if offenders > 0 {
		t.Errorf("%d/%d programs report different warnings under the two parse shapes; "+
			"diagnostics must be produced by a shape-independent pass "+
			"(see docs/design/nolang-parser-lowering-invariants.md §17.1).\nfirst: %s",
			offenders, compared, firstDiff)
	}
	t.Logf("parser warnings identical under both parse shapes for %d/%d programs",
		compared, len(paths))
}

// ── Why the gate above is not enough on its own ──────────────────────────────
//
// TestParserWarningsAreShapeIndependent compares sugar against lowered, so it
// can only catch DRIFT between them. A diagnostic that disappears from BOTH
// shapes at once keeps the two equal and sails through — which is exactly the
// regression the shape-independence migration has to prevent: the hint used to
// be produced as a side effect of lowering, so "not produced at all" is the
// natural failure mode, and it is shape-symmetric.
//
// The test below pins existence directly. It used to read one corpus program
// (tests/match-test.no, the only file carrying both hint flavours, at 19:9 and
// 79:9). Commit ed7b5236 merged that file into tests/match.no, so the hardcoded
// path started failing with "match-test.no did not parse" without exercising the
// hint at all. Scan the whole corpus instead: the invariant worth pinning is that
// BOTH halves of the diagnostic family exist and agree across parse shapes,
// whichever file happens to host them after the next corpus reorganisation.

// TestUnreachableArmHintSurvivesBothShapes pins that the `'->' arm is
// unreachable` hint is still produced, in both parse shapes, with identical
// text and position.
func TestUnreachableArmHintSurvivesBothShapes(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not found at %s: %v", corpusRoot, err)
	}

	var paths []string
	err := filepath.WalkDir(corpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusRoot, err)
	}
	sort.Strings(paths)
	if len(paths) < 200 {
		t.Fatalf("only %d corpus files; the scan would be vacuous", len(paths))
	}

	// Compare file by file: a program that fails to parse in either shape is
	// skipped in BOTH, so a parse failure cannot masquerade as a diagnostic
	// missing on just one side.
	perShape := map[string][]string{}
	for _, path := range paths {
		lowered := parseCorpusFile(t, path, false)
		sugar := parseCorpusFile(t, path, true)
		if lowered == nil || sugar == nil {
			continue
		}
		for _, w := range lowered.Warnings {
			if strings.Contains(w, "arm is unreachable") {
				perShape["lowered"] = append(perShape["lowered"], w)
			}
		}
		for _, w := range sugar.Warnings {
			if strings.Contains(w, "arm is unreachable") {
				perShape["sugar"] = append(perShape["sugar"], w)
			}
		}
	}

	if len(perShape["lowered"]) < 2 {
		t.Errorf("expected at least 2 'arm is unreachable' hints in the corpus, got %d (%v)",
			len(perShape["lowered"]), perShape["lowered"])
	}
	// Both halves of the message family must be present: the option-variant
	// case (ok/err/nil listed) and the enum-variant case.
	joined := strings.Join(perShape["lowered"], "\n")
	for _, want := range []string{
		"all option variants (ok, err, nil) have been listed",
		"all enum variants have been listed",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing the %q half of the diagnostic: %v", want, perShape["lowered"])
		}
	}
	if strings.Join(perShape["lowered"], "\n") != strings.Join(perShape["sugar"], "\n") {
		t.Errorf("the hint differs between shapes:\n  lowered: %v\n  sugar:   %v",
			perShape["lowered"], perShape["sugar"])
	}
	t.Logf("found %d 'arm is unreachable' hints across %d corpus programs, identical under both shapes",
		len(perShape["lowered"]), len(paths))
}

func parseCorpusFile(t *testing.T, path string, sugar bool) *parser.Program {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	p := parser.New(lexer.NewCached(path, string(src)))
	p.Filename = filepath.Base(path)
	if sugar {
		p.SkipLowering = true
	}
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		return nil
	}
	return prog
}
