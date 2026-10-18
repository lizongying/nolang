package dump

// ─────────────────────────────────────────────────────────────────────────────
// S2 core: HIR EQUIVALENCE for the one desugar that moved into H0.
//
// Today the shipped pipeline is:
//
//	ParseProgram()                 // LowerSurface runs inside the parser
//	                     → ASTToHIR
//
// The target pipeline (design doc §8, P1+P2) is:
//
//	ParseProgram(SkipLowering)     // the parser stops after producing sugar nodes
//	                     → ASTToHIR // the single AST→HIR pass desugars WHILE emitting
//
// This gate does NOT compare those two end-to-end. That comparison cannot pass
// yet and would be uninformative: SkipLowering withholds far more than match
// desugaring, so a strict end-to-end diff reports 98 divergences that are all
// "some other pass has not moved yet" and none of them say whether the match
// desugar is correct. (Measured: 98/532 under that formulation; every
// divergence inspected was an inferred type on a non-match node, written by
// inferOptionArith / synthesizeNullableType — sugar items #4 and #5 in §4.1.)
//
// What IS testable now is the claim this increment actually makes: **H0 lowers
// SurfaceMatch to exactly what the parser's lowering produced.** To hold
// everything but the match desugar constant, both sides parse with the surface
// configuration (SkipUnwrapLowering + SkipSafeIndexLowering), which is exactly
// the pair of flags that gate inferOptionArith, maybeCaptureOptionAssign and
// maybeIndexOutAssign. On that base the parser's remaining work in LowerSurface
// IS the SurfaceMatch expansion:
//
//	surface AST ─┬─ LowerSurface → ASTToHIR   → HIR_par  (parser desugars matches)
//	             └─ ASTToHIR                   → HIR_h0   (H0 desugars matches)
//
// HIR_h0 == HIR_par proves the desugar moved without changing behaviour.
//
// The end-to-end comparison is kept as an INFORMATIONAL counter (sugar+H0 vs the
// shipped lowered pipeline) so the S3 work items stay visible and their numbers
// only ever go down.
//
// What counts as identical: Dump renders kind, interned strings, inferred type,
// flags, value AND @line:col. Positions are included deliberately — both sides
// run the same buildMatchDesugar code over the same SurfaceMatch token, so they
// must agree, and a divergence there is a real diagnostic-position regression
// rather than a cosmetic difference.
//
// Ordering matters and is not incidental: ASTToHIR is read-only over the AST
// (PopulateInferredTypes is a separate exported call, not invoked here), so the
// H0 side runs first and leaves the tree pristine for LowerSurface, which
// rewrites in place.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// hirMatchDesugarAllowlist records corpus files where H0's SurfaceMatch
// desugaring is KNOWN to differ from the parser's, with the reason. Both
// directions are enforced: a new divergence must be recorded here with a
// rationale (so landing a regression is a deliberate act), and an entry whose
// file has started matching again must be deleted — which is what stops this
// from becoming a permanent excuse.
var hirMatchDesugarAllowlist = map[string]string{}

// hirEquivalenceMinCompared guards against a vacuous run: if the corpus walk
// yields too few programs, "0 divergences" means nothing.
const hirEquivalenceMinCompared = 200

// TestH0MatchDesugarMatchesParserLowering is this increment's hard gate.
func TestH0MatchDesugarMatchesParserLowering(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	var compared, endToEndDiff, exercised int
	var diverged []string
	var notLowered []string

	walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(raw)

		hirH0, smH0, okH0 := matchDesugarViaH0(path, src)
		hirPar, smPar, okPar := matchDesugarViaParser(path, src)
		if !okH0 && !okPar {
			return nil
		}
		if okH0 != okPar {
			diverged = append(diverged, fmt.Sprintf("%s: h0=%v parser=%v", path, okH0, okPar))
			return nil
		}

		// Non-vacuity, measured per file: only count this file as an exercise of
		// the desugar when Route A actually carried SurfaceMatch nodes into H0.
		// Files with 0 matches compare two identical trees either way.
		if smH0 > 0 {
			exercised++
			// ...and Route B must have expanded them all, otherwise it is NOT the
			// parser-desugared tree this gate claims to compare.
			if smPar != 0 {
				notLowered = append(notLowered, fmt.Sprintf("%s (%d left)", path, smPar))
			}
		}

		compared++
		if hirH0 != hirPar {
			diverged = append(diverged, path)
		}

		// Informational: how far is the WHOLE target pipeline from parity?
		//
		// ⚠️ This compares SkipLowering-alone (the real target pipeline)
		// against the shipped default — NOT hirH0. hirH0 carries the surface
		// flags, which are this gate's isolation mechanism and deliberately
		// switch off inferOptionArith / maybeCaptureOptionAssign /
		// maybeIndexOutAssign. Charging the target pipeline for work the flags
		// had turned off overcounted the gap by ~2x (94 instead of 51) and
		// blamed passes that were never in scope. See s3_breakdown_test.go for
		// the honest measurement and its per-cause breakdown.
		shipped, okShip := lowerToHIR(path, src, false)
		target, okTarget := lowerToHIRSkipOnly(path, src)
		if !okShip || !okTarget || shipped != target {
			endToEndDiff++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", corpusRoot, walkErr)
	}

	if compared < hirEquivalenceMinCompared {
		t.Fatalf("only %d programs compared; the equivalence claim would be vacuous",
			compared)
	}

	if len(notLowered) > 0 {
		sort.Strings(notLowered)
		t.Errorf("%d file(s) still contain SurfaceMatch after the explicit "+
			"DesugarSurfaceMatches pass, so Route B is not the parser-desugared tree "+
			"this gate compares against; first 10: %v",
			len(notLowered), notLowered[:min(10, len(notLowered))])
	}
	// The exercise count is the anti-vacuity gate for the WHOLE comparison. With
	// the pre-flip shape of these helpers it was 0 and the test reported "0
	// divergences" while comparing a pipeline to itself.
	if exercised < hirEquivalenceMinCompared/2 {
		t.Fatalf("only %d of %d compared programs actually carried SurfaceMatch into "+
			"H0, so \"0 divergences\" may be 0 == 0; the two routes have collapsed "+
			"into the same pipeline", exercised, compared)
	}

	sort.Strings(diverged)

	// Allowlist must be bidirectional.
	seen := map[string]bool{}
	var fresh []string
	for _, f := range diverged {
		if _, ok := hirMatchDesugarAllowlist[f]; ok {
			seen[f] = true
			continue
		}
		fresh = append(fresh, f)
	}
	for f := range hirMatchDesugarAllowlist {
		if !seen[f] {
			t.Errorf("allowlisted %s no longer diverges — H0 now produces the same "+
				"HIR as the parser's lowering. Delete it from hirMatchDesugarAllowlist",
				f)
		}
	}

	if len(fresh) > 0 {
		const show = 10
		t.Errorf("%d/%d programs: H0's SurfaceMatch desugar produces DIFFERENT HIR "+
			"than the parser's lowering; the first %d: %v",
			len(fresh), compared, min(show, len(fresh)), fresh[:min(show, len(fresh))])
	}

	t.Logf("match desugar: %d programs compared, %d diverged (%d allowlisted, %d fresh); "+
		"%d actually exercised the desugar (SurfaceMatch reached H0); "+
		"end-to-end (sugar+H0 vs shipped pipeline) still differs on %d/%d — that is the "+
		"S3 backlog, not this gate",
		compared, len(diverged), len(diverged)-len(fresh), len(fresh), exercised,
		endToEndDiff, compared)
}

// matchDesugarViaH0 is ROUTE A — the pipeline that actually ships after the
// S1 flip: ParseProgram keeps every *SurfaceMatch (deferMatchDesugar), so ASTToHIR
// desugars them itself via tohir's `case *SurfaceMatch`.
//
// surface is how many *SurfaceMatch nodes were still in the tree going into H0.
// A file with 0 is not exercising the desugar at all, so the caller counts only
// files with surface > 0 — without that the gate is 0 == 0.
//
// ⚠️ History (this is why the shape changed): this used to parse with the
// "surface configuration" (SkipUnwrapLowering + SkipSafeIndexLowering) to hold
// every other pass constant. Those two flags were DELETED with §4.3
// (parser/lowering.go:337,416,432,442 say so explicitly), and a botched edit
// left `p.SkipLowering = true` written three times in their place — which made
// this route byte-identical to matchDesugarViaParser and the whole gate silently
// vacuous. The isolation is now achieved differently: both routes use the DEFAULT
// parse (identical everything), and only Route B additionally runs the explicit
// expansion pass. See matchDesugarViaParser.
func matchDesugarViaH0(path, src string) (dump string, surface int, ok bool) {
	func() {
		defer func() {
			if r := recover(); r != nil {
				dump, surface, ok = "", 0, false
			}
		}()
		p := parser.New(lexer.NewCached(path, src))
		p.Filename = path
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			dump, ok = "", false
			return
		}
		surface = countSurfaceMatches(prog)
		pkg := parser.ASTToHIR(prog)
		if pkg == nil {
			dump, ok = "", false
			return
		}
		dump, ok = pkg.Dump(), true
	}()
	return dump, surface, ok
}

// matchDesugarViaParser is ROUTE B — same default parse, then the SurfaceMatch
// nodes are expanded by the parser's own explicit pass `DesugarSurfaceMatches`
// (the same entry point the experimental js/wasm backends call from
// build/builder.go) before H0 runs. H0 therefore sees the lowered IfExpression
// chain and never enters its own `case *SurfaceMatch`.
//
// Both routes run the SAME ParseProgram, so every other lowering step is held
// constant and the only remaining difference is WHO expands the matches.
// That is the isolation this gate needs, and after the S1 flip it can no longer
// be obtained by leaving LowerSurface to do it: LowerSurface deliberately does
// NOT expand SurfaceMatch any more (that is the flip), so a Route B built on
// `SkipLowering = false` would now carry sugar into H0 exactly like Route A and
// compare equal trivially.
//
// surface must be 0 for every file whose Route A count was non-zero: the explicit
// pass is supposed to have removed them all. If it is not, Route B is not the
// "parser did the desugaring" tree the gate claims to compare against.
func matchDesugarViaParser(path, src string) (dump string, surface int, ok bool) {
	func() {
		defer func() {
			if r := recover(); r != nil {
				dump, surface, ok = "", 0, false
			}
		}()
		p := parser.New(lexer.NewCached(path, src))
		p.Filename = path
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			dump, ok = "", false
			return
		}
		p.DesugarSurfaceMatches(prog)
		surface = countSurfaceMatches(prog)
		pkg := parser.ASTToHIR(prog)
		if pkg == nil {
			dump, ok = "", false
			return
		}
		dump, ok = pkg.Dump(), true
	}()
	return dump, surface, ok
}

// lowerToHIR parses src with the requested pipeline and returns the HIR dump.
// ok is false when the program did not parse cleanly or lowering panicked — a
// panic in H0 is itself a divergence, but it is attributed to "could not lower"
// so the gate reports it instead of crashing the whole run.
func lowerToHIR(path, src string, skipLowering bool) (dump string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			dump, ok = "", false
		}
	}()

	p := parser.New(lexer.NewCached(path, src))
	p.Filename = path
	if skipLowering {
		p.SkipLowering = true
	}
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", false
	}
	var pkg *hir.Package
	func() {
		defer func() {
			if r := recover(); r != nil {
				pkg = nil
			}
		}()
		pkg = parser.ASTToHIR(prog)
	}()
	if pkg == nil {
		return "", false
	}
	return pkg.Dump(), true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
