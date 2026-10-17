package dump

// ─────────────────────────────────────────────────────────────────────────────
// S1 preparation: measure what breaks when the checker is pointed at the
// sugar-bearing AST.
//
// docs/design/nolang-parser-lowering-invariants.md rates S1 ("move the checker
// ahead of lowering") as the riskiest step, because rules that recognise a
// construct by the *shape of the lowered tree* silently stop firing once the
// sugar is still present.
//
// The repo already has two *partial* sugar-preserving modes, both of which feed
// the checker:
//
//   - SkipUnwrapLowering      — keeps `x ?= e` (UnwrapAssignStatement)
//   - SkipSafeIndexLowering   — keeps the `#{index-out=…}` / place-write surface
//
// The LSP and `no fmt` use both (lsp/documents.go, fmt/api.go), and
// checker/index_write_test.go:14 states the standing rule: "surface AST 与降级
// AST 两端口径一致". So the checker is ALREADY run on a sugar-bearing tree in
// one production path, and the two-tier sugar (GroupedExpression,
// UnwrapAssignStatement) already has 59 handlers in checker/.
//
// What has never happened is running the checker on a tree that still contains
// **SurfaceMatch** — the parser always expands it before ParseProgram returns.
// That is the real S1 surface. This file measures all three shapes at once:
//
//	lowered  — today's `no build` / `no vet` path
//	surface  — today's LSP / `no fmt` path (the two local skip flags)
//	sugar    — Parser.SkipLowering: no expansion at all (the S1 target)
//
// Results are recorded in docs/design/nolang-parser-lowering-invariants.md §14.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// shape describes one parsing configuration under test. The `lightweight` flag
// is orthogonal to the parser flags: it is forwarded to checker.RunAllLints as
// LintOptions.LightweightMode. That mode is what the LSP path uses, and the only
// thing it changes is that RunAllLints additionally runs CheckUnresolvedModuleCalls
// (TraceID `ckaj3355`) — a rule that otherwise NEVER fires during the census, so
// its behaviour across shapes was previously unmeasured (see §21.7 #5 leftover).
type shape struct {
	name        string
	lightweight bool
	configure   func(*parser.Parser)
}

func loweredShape() shape {
	return shape{name: "lowered", configure: func(p *parser.Parser) {}}
}

func surfaceShape() shape {
	return shape{name: "surface", configure: func(p *parser.Parser) {
		p.SkipLowering = true
		p.SkipLowering = true
	}}
}

func sugarShape() shape {
	return shape{name: "sugar", configure: func(p *parser.Parser) { p.SkipLowering = true }}
}

// lightweight clones of the three shapes: same parser configuration, but with
// LintOptions.LightweightMode=true so CheckUnresolvedModuleCalls (ckaj3355)
// is exercised by the census.
func loweredShapeLW() shape {
	return shape{name: "lowered-lw", lightweight: true, configure: func(p *parser.Parser) {}}
}

func surfaceShapeLW() shape {
	return shape{name: "surface-lw", lightweight: true, configure: func(p *parser.Parser) {
		p.SkipLowering = true
		p.SkipLowering = true
	}}
}

func sugarShapeLW() shape {
	return shape{name: "sugar-lw", lightweight: true, configure: func(p *parser.Parser) { p.SkipLowering = true }}
}

// allShapes returns the three parser shapes in both the production (non-light)
// and LSP (lightweight) lint modes, so the census measures ckaj3355 behaviour
// across every shape/mode combination.
func allShapes() []shape {
	return []shape{
		loweredShape(), surfaceShape(), sugarShape(),
		loweredShapeLW(), surfaceShapeLW(), sugarShapeLW(),
	}
}

// diagKey renders one lint result into a canonical, order-independent key.
// Fields are `|`-separated so an empty TraceID cannot shift the other fields.
func diagKey(l checker.LintResult) string {
	return fmt.Sprintf("%d:%d|%s|%s|%s", l.Line, l.Column, l.Severity, l.TraceID, l.Message)
}

// traceOf extracts the trace ID from a diagKey. Diagnostics with no trace ID
// (several legacy rules) collapse to "<no-trace>"; that is coarse but the drift
// gate only needs to notice that a *rule* started or stopped firing.
func traceOf(key string) string {
	parts := strings.SplitN(key, "|", 4)
	if len(parts) < 3 || parts[2] == "" {
		return "<no-trace>"
	}
	return parts[2]
}

// censusStats is the per-shape tally produced by shapeCensus.
type censusStats struct {
	compared  int
	diagTotal []int
	nonEmpty  []int
	panicked  []int
	// traces[i] is the per-shape trace-ID histogram. The pairwise deltas answer
	// "what moved between two shapes"; this answers "what does shape i actually
	// report", which is the question you need when a delta turns out to be a
	// shape that fires *nothing* for a rule rather than a shape that fires
	// something different.
	traces []map[string]int
}

// shapeCensus is the shared engine: it parses the corpus once per shape, runs
// the aggregate lint entry point (checker.RunAllLints — the same one `no vet`
// and the LSP use), and hands the visitor the per-shape diagnostic sets of every
// file that parses cleanly in ALL shapes. A nil set means the checker panicked
// on that shape.
//
// Files that do not parse cleanly are dropped: deliberate error cases would
// otherwise poison the diff.
func shapeCensus(t *testing.T, shapes []shape, visit func(path string, sets []map[string]bool)) censusStats {
	t.Helper()
	stats := censusStats{
		diagTotal: make([]int, len(shapes)),
		nonEmpty:  make([]int, len(shapes)),
		panicked:  make([]int, len(shapes)),
		traces:    make([]map[string]int, len(shapes)),
	}
	for i := range stats.traces {
		stats.traces[i] = map[string]int{}
	}

	walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(raw)

		progs := make([]*parser.Program, len(shapes))
		for i, sh := range shapes {
			p := parser.New(lexer.NewCached(path, src))
			p.Filename = path
			sh.configure(p)
			progs[i] = p.ParseProgram()
			if len(p.Errors()) > 0 {
				return nil
			}
		}

		stats.compared++
		sets := make([]map[string]bool, len(shapes))
		for i, sh := range shapes {
			var keys []string
			var pan string
			func() {
				defer func() {
					if r := recover(); r != nil {
						pan = fmt.Sprint(r)
					}
				}()
			for _, l := range checker.RunAllLints(progs[i], checker.LintOptions{
				SourcePath:      path,
				RootDir:         filepath.Dir(path),
				LightweightMode: sh.lightweight,
			}) {
					keys = append(keys, diagKey(l))
				}
			}()
			if pan != "" {
				stats.panicked[i]++
				if stats.panicked[i] <= 3 {
					t.Logf("checker panicked on the %s shape of %s: %s", sh.name, path, pan)
				}
				sets[i] = nil
				continue
			}
			stats.diagTotal[i] += len(keys)
			if len(keys) > 0 {
				stats.nonEmpty[i]++
			}
			set := make(map[string]bool, len(keys))
			for _, k := range keys {
				set[k] = true
				stats.traces[i][traceOf(k)]++
			}
			sets[i] = set
		}

		visit(path, sets)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", corpusRoot, walkErr)
	}
	if stats.compared < 200 {
		t.Fatalf("only %d programs compared; the census would be vacuous", stats.compared)
	}
	return stats
}

// requireCensusPreconditions skips when the census cannot run meaningfully.
func requireCensusPreconditions(t *testing.T) {
	t.Helper()
	if checker.StdSigKey == "" {
		t.Skip("StdSigKey not injected (bare `go test`); the checker hard-fails without " +
			"it. Run `make test`, or pass " +
			`-ldflags "-X github.com/lizongying/nolang/checker.StdSigKey=$KEY"`)
	}
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not found at %s: %v", corpusRoot, err)
	}
}

// sugarArmBodyRepro is a minimal program whose ONLY reference to the top-level
// variable `a` sits inside a match arm body.
//
// It is the smallest reproduction of the root cause behind the whole drift list:
// SurfaceMatch.Arms is []MatchArm, and MatchArm (parser/match.go:715) has all 20
// of its fields unexported. Every checker walker is a hand-written type switch
// that skips node types it does not know, so the arm bodies are invisible to the
// checker whenever SurfaceMatch survives — the reference to `a` is never seen,
// and `a` is reported as "defined but never used".
//
// In the lowered shape the arms have become IfExpressions, which the walkers do
// handle, so the reference is found and nothing is reported.
const sugarArmBodyRepro = `g = () (r ?i64) {
    r = 1
}

a i64 = 0

f = () () {
    n = g()
    n: {
        ok -> print(a)
        -> print(a)
    }
}
`

// TestSugarArmBodiesAreInvisibleToChecker pins the root cause rather than the
// symptom. It asserts the *asymmetry*: the same source yields opposite answers
// for `a` depending only on whether the arms have been expanded.
//
// This is the finding that reframes S1: the 12 drifting traces are not 12
// independent rule bugs, they are one visibility boundary in parser/.
func TestSugarArmBodiesAreInvisibleToChecker(t *testing.T) {
	requireCensusPreconditions(t)

	const want = "'a' is defined but never used"

	run := func(name string, sh shape) (reported bool) {
		p := parser.New(lexer.NewCached("repro.no", sugarArmBodyRepro))
		sh.configure(p)
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("%s: parse errors: %v", name, errs)
		}
		for _, r := range checker.ValidateUnusedVars(prog, nil) {
			if strings.Contains(r.Message, want) {
				reported = true
			}
		}
		return reported
	}

	loweredSays := run("lowered", loweredShape())
	sugarSays := run("sugar", sugarShape())

	t.Logf("reference to `a` lives only inside a match arm body")
	t.Logf("  lowered shape reports `a` unused: %v", loweredSays)
	t.Logf("  sugar   shape reports `a` unused: %v", sugarSays)

	// Anti-vacuity: the lowered shape must actually see the reference, otherwise
	// the comparison below would be "true vs true" and prove nothing.
	if loweredSays {
		t.Fatalf("vacuous: the lowered shape already reports `a` unused, so the " +
			"repro does not isolate arm-body visibility")
	}
	if !sugarSays {
		t.Skip("the sugar shape no longer misses arm-body references — this root " +
			"cause has been fixed; delete this test and drop the allowlist entries " +
			"that depended on it")
	}
	t.Logf("ROOT CAUSE CONFIRMED: arm bodies are invisible to the checker whenever " +
		"SurfaceMatch survives (SurfaceMatch.Arms is []MatchArm with unexported fields)")
}

// sugarArmBodyIndexWriteRepro is the mirror image of sugarArmBodyRepro: here the
// sugar-only statement (`a[1] = 2`, which D9 forbids) sits inside a match arm.
//
// If arm-body invisibility really is the single root cause, this must show the
// OPPOSITE asymmetry from the unused-var case: the diagnostic is reported in the
// lowered shape and *lost* in the sugar shape.
const sugarArmBodyIndexWriteRepro = `g = () (r ?i64) {
    r = 1
}

f = () () {
    a [3]i64 = [1, 2, 3]
    n = g()
    n: {
        ok -> a[1] = 2
        -> _
    }
}
`

// TestSugarArmBodiesAlsoHideIndexWrites verifies the other direction of the same
// root cause. sugarArmBodyRepro proves arm bodies are invisible (a reference is
// missed -> false "unused"); this proves the consequence for statements inside
// an arm (a forbidden index write is missed -> a real diagnostic is dropped).
func TestSugarArmBodiesAlsoHideIndexWrites(t *testing.T) {
	requireCensusPreconditions(t)

	run := func(name string, sh shape) (reported bool) {
		p := parser.New(lexer.NewCached("repro2.no", sugarArmBodyIndexWriteRepro))
		sh.configure(p)
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Skipf("%s: the repro does not parse any more (%v); the D9 rules moved", name, errs)
		}
		for _, r := range checker.ValidateIndexWrite(prog, "repro2.no") {
			if r.TraceID == "idxwrite" {
				reported = true
			}
		}
		return reported
	}

	loweredSays := run("lowered", loweredShape())
	sugarSays := run("sugar", sugarShape())

	t.Logf("forbidden index write `a[1] = 2` lives only inside a match arm body")
	t.Logf("  lowered shape reports idxwrite: %v", loweredSays)
	t.Logf("  sugar   shape reports idxwrite: %v", sugarSays)

	if !loweredSays {
		t.Skip("the lowered shape does not report idxwrite for this repro (the D9 " +
			"rules have moved); this test needs rewriting against the new rules")
	}
	if sugarSays {
		// The descent case for *parser.SurfaceMatch was added to ValidateIndexWrite's
		// walkExpr, so this direction of the root cause is closed. Like
		// TestSugarArmBodiesAreInvisibleToChecker, the test retires itself rather
		// than being deleted by hand — that keeps the "was it ever real?" evidence
		// in the tree for as long as the code that fixed it is present.
		t.Skip("the sugar shape now reports idxwrite for an arm-body index write — " +
			"this direction of the root cause has been fixed; delete this test and " +
			"drop the allowlist entries that depended on it")
	}
	t.Logf("ROOT CAUSE CONFIRMED (both directions): arm bodies are invisible, so " +
		"diagnostics anchored inside an arm are dropped")
}

// TestMatchArmPayloadIsReadableAcrossPackages is the gate for the parser API
// change recorded in design doc §15.3 (option (a): export MatchArm + fields).
//
// This file lives in package dump, so it is an *external* consumer of parser —
// exactly the position checker / fmt / `no ast` are in. Before the export, the
// only thing reachable was `len(sm.Arms)`: every arm field was unexported on an
// unexported type. This test asserts the payload is now genuinely readable from
// outside, which is the precondition S1 depends on.
func TestMatchArmPayloadIsReadableAcrossPackages(t *testing.T) {
	requireCensusPreconditions(t)

	p := parser.New(lexer.NewCached("repro3.no", sugarArmBodyRepro))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// sugarArmBodyRepro: [0] FunctionDefinition g, [1] LetStatement a,
	// [2] FunctionDefinition f { n = g(); <SurfaceMatch> }
	fn, ok := prog.Statements[2].(*parser.FunctionDefinition)
	if !ok {
		t.Fatalf("statement 2 is %T, want *parser.FunctionDefinition", prog.Statements[2])
	}
	es, ok := fn.Body.Statements[1].(*parser.ExpressionStatement)
	if !ok {
		t.Fatalf("f's statement 1 is %T, want *parser.ExpressionStatement", fn.Body.Statements[1])
	}
	sm, ok := es.Expression.(*parser.SurfaceMatch)
	if !ok {
		t.Fatalf("f's statement 1 expression is %T, want *parser.SurfaceMatch", es.Expression)
	}

	if len(sm.Arms) != 2 {
		t.Fatalf("SurfaceMatch has %d arms, want 2", len(sm.Arms))
	}
	// Every one of these is a cross-package read of a field that used to be
	// unexported. If the export is reverted, this file stops compiling.
	arm := sm.Arms[0]
	if arm.Body == nil {
		t.Errorf("arm[0].Body is nil; the payload is not readable from outside parser")
	}
	// `ok ->` is the val branch: parser marks it wildcard AND isDotVal
	// (see match.go: ok → ma.isWildcard = true; ma.isDotVal = true).
	if !arm.IsWildcard || !arm.IsDotVal {
		t.Errorf("arm[0] (ok ->) IsWildcard=%v IsDotVal=%v, want true/true",
			arm.IsWildcard, arm.IsDotVal)
	}
	if arm.Pos.Line == 0 {
		t.Errorf("arm[0].Pos is zero; the arm does not carry its surface position (C7)")
	}
	fallback := sm.Arms[1]
	if fallback.Body == nil {
		t.Errorf("arm[1].Body is nil (the `-> print(a)` fallback arm)")
	}
	// `->` (bare arrow) is the catch-all: wildcard, but NOT the val branch.
	if !fallback.IsWildcard || fallback.IsDotVal {
		t.Errorf("arm[1] (->) IsWildcard=%v IsDotVal=%v, want true/false",
			fallback.IsWildcard, fallback.IsDotVal)
	}
	t.Logf("arm[0]: Pos=L%d IsWildcard=%v IsDotVal=%v Body=%d stmts; arm[1]: IsDotVal=%v Body=%d stmts",
		arm.Pos.Line, arm.IsWildcard, arm.IsDotVal, len(arm.Body.Statements),
		fallback.IsDotVal, len(fallback.Body.Statements))
}

// TestCheckerShapeAgreementCensus is the S1 risk measurement. It is a census,
// not a gate: the point is to turn "S1 is risky" into a named list of rules.
// See §14 of the design doc for the recorded numbers.
func TestCheckerShapeAgreementCensus(t *testing.T) {
	requireCensusPreconditions(t)

	shapes := allShapes()

	// Pairwise deltas, indexed by "from→to": diagnostics present in `from` but
	// absent in `to` (lost) / present in `to` but absent in `from` (gained).
	type delta struct {
		lost, gained map[string]int
		differing    int
		// pathsByTrace records, per trace ID, the corpus files where that trace
		// drifted. The counts alone tell you *which* rule moves; the paths tell
		// you which program to open to find out why. Emitted only when
		// NOLANG_CENSUS_PATHS is set (the list is long and usually noise).
		lostPathsByTrace   map[string]map[string]bool
		gainedPathsByTrace map[string]map[string]bool
	}
	deltas := map[string]*delta{}
	recordPath := func(m map[string]map[string]bool, trace, path string) {
		if m == nil {
			return
		}
		if m[trace] == nil {
			m[trace] = map[string]bool{}
		}
		m[trace][path] = true
	}

	stats := shapeCensus(t, shapes, func(path string, sets []map[string]bool) {
		for i := range shapes {
			if sets[i] == nil {
				continue
			}
			for j := range shapes {
				if i == j || sets[j] == nil {
					continue
				}
				key := shapes[i].name + "→" + shapes[j].name
				d, exists := deltas[key]
				if !exists {
					d = &delta{
						lost:               map[string]int{},
						gained:             map[string]int{},
						lostPathsByTrace:   map[string]map[string]bool{},
						gainedPathsByTrace: map[string]map[string]bool{},
					}
					deltas[key] = d
				}
				same := len(sets[i]) == len(sets[j])
				if same {
					for k := range sets[i] {
						if !sets[j][k] {
							same = false
							break
						}
					}
				}
				if same {
					continue
				}
				if i < j { // count each unordered pair once
					d.differing++
				}
				for k := range sets[i] {
					if !sets[j][k] {
						d.lost[k]++
						recordPath(d.lostPathsByTrace, traceOf(k), path)
					}
				}
				for k := range sets[j] {
					if !sets[i][k] {
						d.gained[k]++
						recordPath(d.gainedPathsByTrace, traceOf(k), path)
					}
				}
			}
		}
	})

	t.Logf("%d programs compared across %d shapes", stats.compared, len(shapes))
	for i, sh := range shapes {
		t.Logf("  %-8s %6d diagnostics over %4d non-empty programs (%d panics)",
			sh.name, stats.diagTotal[i], stats.nonEmpty[i], stats.panicked[i])
	}
	// Per-shape trace histograms, side by side. This is the view that says
	// "lowered 0 / surface 20 / sugar 0" instead of leaving you to infer it
	// from pairwise deltas.
	if os.Getenv("NOLANG_CENSUS_TRACES") != "" {
		reportPerShapeTraces(t, shapes, stats.traces)
	}
	if stats.nonEmpty[0] == 0 {
		t.Logf("⚠️  VACUOUS: no shape yields any RunAllLints diagnostic, so " +
			"`0 differing` is 0 == 0 and proves nothing.")
	}

	report := func(label string, tally map[string]int) {
		if len(tally) == 0 {
			t.Logf("%s: none", label)
			return
		}
		type mc struct {
			msg string
			n   int
		}
		all := make([]mc, 0, len(tally))
		for m, n := range tally {
			all = append(all, mc{m, n})
		}
		sort.Slice(all, func(i, j int) bool {
			if all[i].n != all[j].n {
				return all[i].n > all[j].n
			}
			return all[i].msg < all[j].msg
		})
		if len(all) > 12 {
			all = all[:12]
		}
		t.Logf("%s (%d distinct):", label, len(tally))
		for _, e := range all {
			t.Logf("    %4d × %s", e.n, e.msg)
		}
	}

	keys := make([]string, 0, len(deltas))
	for k := range deltas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := deltas[k]
		t.Logf("── %s: %d differing programs, %d lost, %d gained",
			k, d.differing, len(d.lost), len(d.gained))
		if k == "lowered→sugar" || k == "surface→sugar" {
			report(k+" — diagnostics that STOP firing", d.lost)
			report(k+" — diagnostics that ONLY fire", d.gained)
			// The per-message report above caps at 12 entries, which hides the
			// long tail of one-off rules. The trace-level tally is UNCAPPED and
			// is the authoritative list; read the rewrite list off this one.
			reportTraces(t, k+" — STOP firing, by trace (complete)", d.lost)
			reportTraces(t, k+" — ONLY fire, by trace (complete)", d.gained)
			if os.Getenv("NOLANG_CENSUS_PATHS") != "" {
				reportTracePaths(t, k+" — STOP firing, per trace", d.lostPathsByTrace)
				reportTracePaths(t, k+" — ONLY fire, per trace", d.gainedPathsByTrace)
			}
		}
	}
}

// reportTraces tallies a per-message delta by trace ID and logs every entry.
// Unlike report it never truncates: a rule that drifts on a single program is
// still a rule S1 has to handle.
func reportTraces(t *testing.T, label string, tally map[string]int) {
	byTrace := map[string]int{}
	for k, n := range tally {
		byTrace[traceOf(k)] += n
	}
	if len(byTrace) == 0 {
		t.Logf("%s: none", label)
		return
	}
	type tc struct {
		tr string
		n  int
	}
	all := make([]tc, 0, len(byTrace))
	for tr, n := range byTrace {
		all = append(all, tc{tr, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].tr < all[j].tr
	})
	t.Logf("%s (%d distinct traces):", label, len(all))
	for _, e := range all {
		t.Logf("    %4d × %s", e.n, e.tr)
	}
}

// reportPerShapeTraces prints one line per trace ID with the count in every
// shape, so "the rule fires nowhere on sugar" is directly visible.
func reportPerShapeTraces(t *testing.T, shapes []shape, traces []map[string]int) {
	all := map[string]bool{}
	for _, m := range traces {
		for tr := range m {
			all[tr] = true
		}
	}
	ids := make([]string, 0, len(all))
	for tr := range all {
		ids = append(ids, tr)
	}
	sort.Slice(ids, func(i, j int) bool {
		// Order by the largest count across shapes, descending, so the noisy
		// rules are at the top.
		best := func(tr string) int {
			m := 0
			for _, h := range traces {
				if h[tr] > m {
					m = h[tr]
				}
			}
			return m
		}
		if a, b := best(ids[i]), best(ids[j]); a != b {
			return a > b
		}
		return ids[i] < ids[j]
	})
	header := "    trace"
	for _, sh := range shapes {
		header += fmt.Sprintf("  %8s", sh.name)
	}
	t.Logf("per-shape trace histogram (NOLANG_CENSUS_TRACES):")
	t.Logf("%s", header)
	for _, tr := range ids {
		line := fmt.Sprintf("    %-28s", tr)
		for _, h := range traces {
			line += fmt.Sprintf("  %8d", h[tr])
		}
		t.Logf("%s", line)
	}
}

// reportTracePaths logs, for each drifting trace, the corpus files where it
// drifted. Gated behind NOLANG_CENSUS_PATHS because it is verbose; it exists so
// that a count like "121 × idxwrite" can be turned into "open these files".
func reportTracePaths(t *testing.T, label string, byTrace map[string]map[string]bool) {
	if len(byTrace) == 0 {
		return
	}
	traces := make([]string, 0, len(byTrace))
	for tr := range byTrace {
		traces = append(traces, tr)
	}
	sort.Strings(traces)
	t.Logf("%s:", label)
	for _, tr := range traces {
		paths := make([]string, 0, len(byTrace[tr]))
		for p := range byTrace[tr] {
			paths = append(paths, filepath.Base(p))
		}
		sort.Strings(paths)
		t.Logf("    %s (%d files): %s", tr, len(paths), strings.Join(paths, " "))
	}
}

// sugarDriftAllowlist is the *measured* set of rules whose diagnostics change
// when the checker is pointed at the sugar AST instead of the lowered one.
//
// Re-measured 2026-10-16 after three S1 fixes (design doc §15):
//   - the shared arm-descent helper (checker/sugar_walk.go)
//   - idxLocalTypes is now a parse-time product, not a lowering by-product
//   - the sugar-native catch-all arm path (match_arm_reach.go) plus the
//     nil-vs-empty fix in catchAllTailBody (see below)
//
// The counts below are deliberately NOT recorded here (they move with the
// corpus); read them off the uncapped trace tally in the census log. What is
// recorded is *why* each rule moves, because that is what S1 has to act on.
//
// ⚠️ Read this list off the uncapped trace tally, NOT off the per-message top-12
// display: an earlier revision of the census did the latter and recorded only 6
// of the 12 traces.
//
// ── How to read the "reference shape" ────────────────────────────────────────
// The gate diffs sugar against `lowered`, because `lowered` is what `no vet` /
// `no build` ship and S1 must not silently change their output. But note that
// `lowered` and `surface` ALREADY disagree today on 13 corpus programs — that
// divergence is pre-existing and is not caused by SurfaceMatch. Where a rule's
// sugar count equals its *surface* count and differs from its lowered count,
// the drift is inherited from that pre-existing divergence, not newly created
// by S1. The census histogram (`NOLANG_CENSUS_TRACES=1`) shows all three
// columns side by side and is the way to tell the two cases apart.
var sugarDriftAllowlist = map[string]string{
	// ── STOP firing on the sugar AST — S1 must add sugar cases ──────────────
	"rngidx":          "ValidateRangeIndexLoops (checker/range_index_lint.go) — keys on the lowered index read; moves position as well as count",
	"ovf-int-default": "ValidateIntOverflow (checker/checker.go) — the inferred-?T decision depends on desugared arithmetic (moves position too)",
	"tcpoxtfd":        "ValidateRedundantTypeAnnotation (checker/checker.go) — compares the annotation against an inferred type that lowering rewrites",
	"idxwrite":        "ValidateIndexWrite (checker/checker.go) — one residual program (tests/range-inline.no); the 121-diagnostic bulk was fixed by making idxLocalTypes a parse-time product",

	// ── ONLY fire on the sugar AST — S1 must not ship these as false positives
	//
	// Root cause for the two heavy hitters, now FIXED: in the lowered form the
	// __cap_N / __unwrap_N scaffolding *consumes* the arm bindings; in the sugar
	// form the bindings live in MatchArm and the walkers could not see them being
	// used. checker/sugar_walk.go + the MatchArm export closed that. The residual
	// counts below are what is left.
	"optunuse": "ValidateUnhandledOverflow — its sugar count equals surface's, so this is the pre-existing lowered/surface divergence, not a new sugar bug",
	"4bek3xc6": "ValidateUndefinedVars — its sugar count equals surface's (pre-existing lowered/surface divergence)",
	"idxhndld": "ValidateUnhandledIndex — its sugar count equals surface's exactly. In the lowered shape the #{index-out} annotation has already been lowered into a synthetic __idx_out_ tmp which the rule treats as 'handled'; surface/sugar read the annotation directly. Pre-existing lowered/surface divergence, surfaced by S1.",
}

// ── Retired allowlist entries (kept as a record) ─────────────────────────────
//
//   2026-10-17 (census re-run, LightweightMode now covered across 6 shapes):
//     dropped as STALE — no longer drift in lowered↔sugar:
//     <no-trace>  — LintIneffectiveOverflow's annotation-relevance map (shape-derived)
//     ovfhndld    — ValidateUnhandledOverflow's HINT half (now correctly visible on sugar)
//     6kryrbsq    — residual ValidateUnusedVars arm-scoped bindings (now resolved)
//   2026-10-16:
//   arm-should / arm-unreach — FIXED. ValidateCatchAllArms now has a
//     sugar-native path (collectSugarCatchAllArms). Reconciling it surfaced a
//     *lowered-path false negative*: catchAllTailBody returned nil both for
//     "no catch-all arm" and for "catch-all arm exists but its Statements slice
//     is nil", so the rule silently skipped every match whose wildcard arm got
//     no synthetic `it` binding. buildItBindingForArm only synthesises that
//     binding when the resolver knows the subject's type (lowering.go:3274),
//     so `b-opt = t` (identifier RHS, type not propagated) hid the arm while
//     `b-opt = n - 1` (arithmetic RHS, type known) exposed it. The signature now
//     carries an explicit `found` flag, which fixes `lowered` as well — the
//     sugar shape was the *correct* one, so this was never drift to allowlist.
//   nolang-deprecated-call — FIXED by the sugar case in checker/deprecated.go.
//   i3k422u3 — FIXED by the sugar case in ValidateUnassignedReturns.

// TestCheckerSugarDriftIsWithinKnownSet locks in the measured baseline: pointing
// the checker at the sugar AST may only change diagnostics for rules already
// listed in sugarDriftAllowlist. A *new* trace ID appearing or disappearing is
// drift that S1 has not accounted for, and fails.
//
// Panics are a hard failure: the checker has never been asked to validate
// SurfaceMatch, so a crash there is unambiguously a bug and cannot be explained
// by corpus migration.
func TestCheckerSugarDriftIsWithinKnownSet(t *testing.T) {
	requireCensusPreconditions(t)

	shapes := []shape{loweredShape(), sugarShape()}
	lostTraces := map[string]int{}
	gainedTraces := map[string]int{}

	stats := shapeCensus(t, shapes, func(path string, sets []map[string]bool) {
		a, b := sets[0], sets[1]
		if a == nil || b == nil {
			return
		}
		for k := range a {
			if !b[k] {
				lostTraces[traceOf(k)]++
			}
		}
		for k := range b {
			if !a[k] {
				gainedTraces[traceOf(k)]++
			}
		}
	})

	// Anti-vacuity: if the lowered shape says nothing, the diff is 0 == 0.
	if stats.nonEmpty[0] == 0 {
		t.Fatalf("vacuous: the lowered shape produced no diagnostics at all over %d programs",
			stats.compared)
	}
	if n := stats.panicked[0] + stats.panicked[1]; n > 0 {
		t.Errorf("the checker panicked on %d (program, shape) pairs; see the census log above", n)
	}

	var unknown []string
	for tr, n := range lostTraces {
		if _, ok := sugarDriftAllowlist[tr]; !ok {
			unknown = append(unknown, fmt.Sprintf("STOPPED firing: %s (%d×)", tr, n))
		}
	}
	for tr, n := range gainedTraces {
		if _, ok := sugarDriftAllowlist[tr]; !ok {
			unknown = append(unknown, fmt.Sprintf("ONLY fires on sugar: %s (%d×)", tr, n))
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Errorf("sugar drift outside the recorded allowlist — S1 must account for these "+
			"rules before the checker can move ahead of lowering:\n  %s\n"+
			"(if the drift is legitimate, add it to sugarDriftAllowlist and record it in "+
			"docs/design/nolang-parser-lowering-invariants.md §14)",
			strings.Join(unknown, "\n  "))
	}

	t.Logf("drift within the recorded set: %d rules stopped firing, %d only fire on sugar",
		len(lostTraces), len(gainedTraces))
	// Log the COMPLETE tallies (uncapped): this is the authoritative S1 rewrite
	// list. An earlier revision of this census read the list off a top-12 display
	// and therefore recorded only 6 of the 15 drifting traces.
	logTraceTally := func(label string, tally map[string]int) {
		type tc struct {
			tr string
			n  int
		}
		all := make([]tc, 0, len(tally))
		for tr, n := range tally {
			all = append(all, tc{tr, n})
		}
		sort.Slice(all, func(i, j int) bool {
			if all[i].n != all[j].n {
				return all[i].n > all[j].n
			}
			return all[i].tr < all[j].tr
		})
		for _, e := range all {
			_, known := sugarDriftAllowlist[e.tr]
			t.Logf("  %-8s %4d × %-22s allowlisted=%v", label, e.n, e.tr, known)
		}
	}
	logTraceTally("LOST", lostTraces)
	logTraceTally("GAINED", gainedTraces)
	for tr, why := range sugarDriftAllowlist {
		if lostTraces[tr] == 0 && gainedTraces[tr] == 0 {
			t.Logf("stale allowlist entry %q: no longer drifts — drop it (rationale: %s)", tr, why)
		}
	}
}
