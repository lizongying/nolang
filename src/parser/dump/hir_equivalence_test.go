package dump

// ─────────────────────────────────────────────────────────────────────────────
// S2 core: H0-direct match desugar gate.
//
// The shipped pipeline is:
//
//	ParseProgram()                 // LowerSurface runs inside the parser
//	                     → ASTToHIR   // H0 desugars *SurfaceMatch while emitting
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
// History — equivalence was proven, then Route B retired. Originally this gate
// compared two routes: Route A (H0 desugars SurfaceMatch → HIR) vs Route B (the
// parser's legacy buildMatchDesugar expanded SurfaceMatch to an IfExpression
// chain → HIR). After the S-a/S-b/S-c slices landed, Route A == Route B held
// byte-for-byte across 622 corpus programs (0 diverged), proving the moved
// desugar was behaviour-preserving. P7 (design doc §5) then DELETED the legacy
// desugar; the js/wasm backends were re-homed onto DesugarSurfaceMatchesPlain, and
// Route B — which depended on the deleted path — was retired. This gate is now a
// single-route corpus exercise + regression smoke test: it runs H0-direct on every
// standalone-parseable corpus program and asserts (1) H0 produces valid HIR (a
// panic/checker failure is a real regression) and (2) the desugar is actually
// exercised on a large fraction of the corpus (anti-vacuity).
//
// The end-to-end comparison is kept as an INFORMATIONAL counter (sugar+H0 vs the
// shipped lowered pipeline) so the S3 work items stay visible and their numbers
// only ever go down.
// ─────────────────────────────────────────────────────────────────────────────

import (
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

// equivalenceCorpusRoots is every corpus this gate walks.
//
// tests/ is the historical corpus. test/std/ was added with slice S-b (§5.2.7):
// the std test programs are the densest bare-match ({ cond -> body }) sources in
// the repo — 82 bare matches over 87 programs — and bare matches got their own
// HIR emitter, so leaving that tree out would have left the emitter covered only
// incidentally. Cost of the extra walk is ~0.8s (measured).
var equivalenceCorpusRoots = []string{corpusRoot, "../../../test/std"}

// hirEquivalenceMinCompared guards against a vacuous run: if the corpus walk
// yields too few programs, "0 divergences" means nothing.
const hirEquivalenceMinCompared = 200

// TestH0MatchDesugarMatchesParserLowering is the H0-direct match desugar gate.
//
// History: this gate originally compared two routes — Route A (H0 desugars
// SurfaceMatch while emitting HIR) against Route B (the parser's legacy
// buildMatchDesugar expanded SurfaceMatch to an IfExpression chain, then H0 emitted
// that). After the S-a/S-b/S-c slices landed, Route A == Route B held byte-for-byte
// across 622 corpus programs (0 diverged), proving the moved desugar was
// behaviour-preserving.
//
// P7 (design doc §5) then DELETED the legacy desugar (buildMatchDesugar /
// emitSubjectMatchAST / desugarSurfaceMatch): its only remaining consumers were
// Route B and the experimental js/wasm backends (the latter re-homed onto
// DesugarSurfaceMatchesPlain, a field-less IfExpression chain). With the legacy
// path gone, Route B can no longer exist, so this gate is now a single-route
// corpus exercise + regression smoke test:
//
//	surface AST → ASTToHIR (H0 desugars SurfaceMatch) → HIR_h0
//
// It asserts (1) H0-direct produces valid HIR for every standalone-parseable
// corpus program — a parse or H0 panic is a real regression; and (2) the desugar
// is actually exercised on a large fraction of the corpus (anti-vacuity — a gate
// that never carries SurfaceMatch into H0 reports "0" meaninglessly). The
// end-to-end informational counter (shipped default pipeline vs the target
// SkipLowering pipeline) is retained unchanged; it tracks the S3 backlog, not
// this gate.
func TestH0MatchDesugarMatchesParserLowering(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	var compared, endToEndDiff, exercised, h0Failures int
	var failures []string

	walk := func(root string) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			src := string(raw)

			// Standalone-parseable? Files that don't parse on their own (multi-file
			// programs, fragments) are not a H0 regression — skip without counting.
			pp := parser.New(lexer.NewCached(path, src))
			pp.Filename = path
			pprog := pp.ParseProgram()
			if pprog == nil || len(pp.Errors()) > 0 {
				return nil
			}

			_, smH0, okH0 := matchDesugarViaH0(path, src)
			if okH0 {
				// Anti-vacuity: only count this file as an exercise of the desugar
				// when H0 actually carried SurfaceMatch nodes into HIR.
				if smH0 > 0 {
					exercised++
				}
				compared++
			} else {
				// H0-direct failed to produce HIR on a corpus program that parses
				// standalone — a real regression (panic or checker-level failure).
				h0Failures++
				failures = append(failures, path)
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
	}

	for _, root := range equivalenceCorpusRoots {
		if _, err := os.Stat(root); err != nil {
			t.Logf("corpus %s not present, skipped: %v", root, err)
			continue
		}
		if walkErr := walk(root); walkErr != nil {
			t.Fatalf("walking %s: %v", root, walkErr)
		}
	}

	if compared < hirEquivalenceMinCompared {
		t.Fatalf("only %d programs lowered via H0; the equivalence claim would be vacuous",
			compared)
	}

	if h0Failures > 0 {
		sort.Strings(failures)
		t.Errorf("%d/%d standalone-parseable corpus programs failed to lower via "+
			"H0-direct (panic or checker failure); first 10: %v",
			h0Failures, compared, failures[:min(10, len(failures))])
	}

	// Anti-vacuity for the whole run: a meaningful gate must actually carry
	// SurfaceMatch into H0 on a large fraction of the corpus.
	if exercised < hirEquivalenceMinCompared/2 {
		t.Fatalf("only %d of %d lowered programs actually carried SurfaceMatch into "+
			"H0, so the desugar may be going unexercised; the gate would be vacuous",
			exercised, compared)
	}

	t.Logf("H0-direct match desugar: %d programs lowered, %d exercised the desugar "+
		"(SurfaceMatch reached H0), %d H0 failures; end-to-end (sugar+H0 vs shipped "+
		"pipeline) still differs on %d/%d — that is the S3 backlog, not this gate",
		compared, exercised, h0Failures, endToEndDiff, compared)
}

// TestBareMatchEquivalenceIsExercised is the S-b anti-vacuity witness.
//
// Since §5.2.7 (slice S-b) a bare match — SurfaceMatch with Matched == nil — no
// longer takes the buildBareMatchDesugar → IfExpression → case *IfExpression
// route; tohir's lowerBareMatchToHIR emits the KIf straight from
// resolveBareMatchPlan. TestH0MatchDesugarMatchesParserLowering still exercises it
// on every corpus program (the H0-direct route carries every bare match into H0),
// but only for bare matches the corpus actually contains. If a future edit made
// every bare match parse as something else, that gate would report "0 exercised"
// over 0 bare matches and the S-b emitter would go untested.
//
// Measured 2026-10-19: 436 bare matches across tests/ + test/std/ (354 in tests/,
// 82 in test/std/), so the floor below sits well under the real count — it is a
// smoke alarm, not a cap.
func TestBareMatchEquivalenceIsExercised(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	const minBare = 50

	var files, bare, withSubj int
	count := func(corpusRoot string) error {
		return filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			func() {
				// A parse panic in one corpus file must not take the gate down.
				defer func() { _ = recover() }()
				p := parser.New(lexer.NewCached(path, string(raw)))
				p.Filename = path
				prog := p.ParseProgram()
				if prog == nil || len(p.Errors()) > 0 {
					return
				}
				files++
				n, m := countBareMatches(prog)
				bare += n
				withSubj += m
			}()
			return nil
		})
	}

	for _, root := range equivalenceCorpusRoots {
		if _, err := os.Stat(root); err != nil {
			t.Logf("corpus %s not present, skipped: %v", root, err)
			continue
		}
		if walkErr := count(root); walkErr != nil {
			t.Fatalf("walking %s: %v", root, walkErr)
		}
	}
	if bare < minBare {
		t.Fatalf("corpus contains only %d bare matches (%d with a subject) over %d "+
			"programs; the equivalence gate's S-b coverage would be vacuous",
			bare, withSubj, files)
	}
	t.Logf("bare-match coverage: %d bare / %d with-subject over %d programs", bare, withSubj, files)
}

// TestSubjectMatchDirectHIRIsExercised is the S-c1 anti-vacuity witness.
//
// Since §5.2.9 (slice S-c1) a subject match on a plain identifier — no
// index-subject normalisation, no D9 place-write, no ok(cond) wrapper, no enum
// complement — no longer takes the resolveSubjectMatch → IfExpression →
// case *IfExpression route; tohir's emitSubjectMatchHIR streams the KIf chain
// straight from resolveSubjectMatchPlan. TestH0MatchDesugarMatchesParserLowering
// still exercises it on every corpus program (the H0-direct route carries every
// subject match into H0), but only for subject matches the corpus actually contains
// AND that satisfy subjectMatchDirectHIR's four exclusions. If a future edit made
// every such match decline the direct route, that gate would report "0 exercised"
// while the S-c1 emitter went untested.
//
// The count comes from the parser's own counter (parser.SubjectMatchDirectEmits),
// so the decision is made by the shipped predicate rather than re-derived here —
// a re-derivation would be a second copy of the exclusion list, free to drift.
func TestSubjectMatchDirectHIRIsExercised(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	const minDirect = 20

	before := parser.SubjectMatchDirectEmits()

	var files, withSubj int
	count := func(corpusRoot string) error {
		return filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			func() {
				// A parse panic in one corpus file must not take the gate down.
				defer func() { _ = recover() }()
				p := parser.New(lexer.NewCached(path, string(raw)))
				p.Filename = path
				prog := p.ParseProgram()
				if prog == nil || len(p.Errors()) > 0 {
					return
				}
				files++
				_, m := countBareMatches(prog)
				withSubj += m
				// Route A: the shipped shape — the parser keeps the SurfaceMatch
				// nodes and ASTToHIR desugars them, which is where the counter
				// lives. A panic inside ASTToHIR is recovered by nothing here on
				// purpose: the equivalence gate is the one that reports panics as
				// divergences, and a panic would also leave the counter short,
				// which this gate then reports as insufficient coverage.
				parser.ASTToHIR(prog)
			}()
			return nil
		})
	}

	for _, root := range equivalenceCorpusRoots {
		if _, err := os.Stat(root); err != nil {
			t.Logf("corpus %s not present, skipped: %v", root, err)
			continue
		}
		if walkErr := count(root); walkErr != nil {
			t.Fatalf("walking %s: %v", root, walkErr)
		}
	}

	direct := int(parser.SubjectMatchDirectEmits() - before)
	if direct < minDirect {
		t.Fatalf("only %d subject matches took the S-c1 direct-HIR route over %d "+
			"programs (%d carry a subject match); the equivalence gate's S-c1 "+
			"coverage would be vacuous", direct, files, withSubj)
	}
	t.Logf("S-c1 direct-HIR coverage: %d direct emissions over %d programs "+
		"(%d carry a subject match)", direct, files, withSubj)
}

// matchDesugarViaH0 is the H0-direct route — the pipeline that actually ships
// after the S1 flip: ParseProgram keeps every *SurfaceMatch (deferMatchDesugar),
// so ASTToHIR desugars them itself via tohir's `case *SurfaceMatch`.
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
// this route byte-identical to the now-retired Route B (the parser's legacy match
// desugar) and the whole gate silently vacuous. The isolation is now achieved
// differently: this route uses the DEFAULT parse (identical everything), and the
// gate simply runs H0-direct on every corpus program. As of P7 (design doc §5)
// the legacy desugar was deleted and Route B retired, so this is the only route
// the gate exercises.
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
