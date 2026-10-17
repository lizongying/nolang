package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// S4 harness, third LSP consumer: the real-time diagnostics.
//
// doc.AST feeds three things: format-on-save (gated in src/fmt), the symbol
// index (gated in sugar_index_ab_test.go) and the lints (gated here). The lints
// are the semantically heaviest of the three — they are the whole checker, and
// §15 already established that the checker sees a different tree under each
// parse shape (`lowered → sugar` drifts on 10 traces). `surface → sugar` was
// never measured; this is that measurement.
//
// Same discipline: census first (never fails), gate second.

// lintFingerprint runs the real-time lint pipeline on one source under one
// parse configuration and returns a canonical, sorted fingerprint.
func lintFingerprint(path, src string, cfg func(*parser.Parser)) (string, bool) {
	l := lexer.New(src)
	p := parser.New(l)
	p.Filename = filepath.Base(path)
	cfg(p)
	prog := p.ParseProgram()
	if prog == nil {
		return "", false
	}
	abs, _ := filepath.Abs(path)
	results := checker.RunAllLints(prog, checker.LintOptions{
		SourcePath:      abs,
		RootDir:         filepath.Dir(abs),
		LightweightMode: true,
	})
	var lines []string
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("%d:%d\t%s\t%s\t%s",
			r.Line, r.Column, r.Severity, r.TraceID, r.Message))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n"), true
}

// TestLspLintsSugarVsSurfaceCensus is the instrument.
func TestLspLintsSugarVsSurfaceCensus(t *testing.T) {
	files := sugarLspCorpus(t)
	var same, differ, unparseable int
	gained, lost := 0, 0
	var diffPaths []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		a, aok := lintFingerprint(path, string(b), lspCfgSurface)
		c, cok := lintFingerprint(path, string(b), lspCfgSugar)
		if !aok || !cok {
			unparseable++
			continue
		}
		if a == c {
			same++
			continue
		}
		differ++
		diffPaths = append(diffPaths, path)
		sa, sc := map[string]bool{}, map[string]bool{}
		for _, l := range strings.Split(a, "\n") {
			sa[l] = true
		}
		for _, l := range strings.Split(c, "\n") {
			sc[l] = true
		}
		for _, l := range strings.Split(a, "\n") {
			if l != "" && !sc[l] {
				lost++
			}
		}
		for _, l := range strings.Split(c, "\n") {
			if l != "" && !sa[l] {
				gained++
			}
		}
	}
	t.Logf("lsp-lints A/B: corpus=%d identical=%d differ=%d unparseable=%d "+
		"(diagnostics only-in-surface=%d only-in-sugar=%d)",
		len(files), same, differ, unparseable, lost, gained)
	shown := diffPaths
	if len(shown) > 12 {
		shown = shown[:12]
	}
	for _, p := range shown {
		t.Logf("  DIFF %s", p)
	}
}

// sugarLintKnownDivergence records the one corpus file whose LSP diagnostics
// still differ, and — the part that matters — WHICH SIDE IS WRONG.
//
// A gate that merely lists exceptions is a place to hide bugs, so every entry
// must be backed by a test that proves the divergence is the *old* shape being
// wrong, not the new one (see TestSugarArmOverflowDivergenceIsSurfaceUnderReporting).
// The gate also re-checks that each entry still diverges, so an entry whose root
// cause gets fixed fails loudly instead of lingering as a dead exemption.
var sugarLintKnownDivergence = map[string]string{
	"tests/type-match.no": "surface is WRONG, sugar is right: sugar reports 2 extra " +
		"`ovf-int-default` hints on `w i64 -> w + 1` / `w i32 -> w + 2` (type-arm bindings). " +
		"`w` is bound i64/i32 by the arm itself, so `w + 1` really does return ?T — the hint is " +
		"true. surface loses the arm's binding type when it expands the match, so it cannot see " +
		"the arithmetic at all. Same class as the formatter's type-match.no divergence (§21.5): " +
		"the sugar shape is the one that is right.",
}

// lspKnownDivergenceKey matches corpus paths by suffix, because the corpus is
// walked relative to this package (`../../tests/...`).
func lspKnownDivergenceKey(path string) string {
	if i := strings.Index(path, "tests/"); i >= 0 {
		return path[i:]
	}
	return path
}

// TestLspLintsSugarMatchesSurface gates the diagnostics half of the LSP flip.
// A diagnostic that disappears is a silent hole in the editor; one that appears
// is a false positive shown to the user. Neither is acceptable as a side effect
// of changing the parse shape, so this gate is two-sided.
//
// History (all measured, see docs/design/nolang-parser-lowering-invariants.md
// §21.9): 6/532 → 4/532 → 1/532. The three root causes that were fixed:
//
//  1. LOST (match-test) — `'->' arm is unreachable: …` was emitted by
//     p.saveWarning inside the desugar loop, i.e. it was a *lowering-time*
//     diagnostic that never runs under SkipLowering. FIXED by moving the check
//     to a parse-time pass (parser/match_warn.go) that runs in both shapes;
//     verified byte-identical over 532 corpus programs.
//
//  2. LOST (sse / yaml) — `ckaj3355 unknown function` disappeared because
//     checker/unresolved.go's two hand-written walkers had no SurfaceMatch case,
//     so module calls inside match arms were invisible. FIXED by adding the
//     descent.
//
//  3. GAINED (net-client / path-char) — false positive `'ok' is defined but
//     never used`: in a bare match the parser consumed `ok` as the val-branch
//     keyword and dropped the source identifier. FIXED in parser/match.go (P1
//     fidelity: the parser must not discard a source identifier).
//
// What remains is one allowlisted entry, and it is sugar being *more* correct.
func TestLspLintsSugarMatchesSurface(t *testing.T) {
	files := sugarLspCorpus(t)
	var offenders []string
	var samples []string
	var stillDiverging int
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		a, aok := lintFingerprint(path, string(b), lspCfgSurface)
		c, cok := lintFingerprint(path, string(b), lspCfgSugar)
		if !aok || !cok {
			continue
		}
		if a == c {
			continue
		}
		key := lspKnownDivergenceKey(path)
		if _, ok := sugarLintKnownDivergence[key]; ok {
			stillDiverging++
			continue
		}
		offenders = append(offenders, path)
		if len(samples) < 3 {
			sa, sc := map[string]bool{}, map[string]bool{}
			for _, l := range strings.Split(a, "\n") {
				sa[l] = true
			}
			for _, l := range strings.Split(c, "\n") {
				sc[l] = true
			}
			var only []string
			for _, l := range strings.Split(a, "\n") {
				if l != "" && !sc[l] {
					only = append(only, "  only-surface "+l)
				}
			}
			for _, l := range strings.Split(c, "\n") {
				if l != "" && !sa[l] {
					only = append(only, "  only-sugar   "+l)
				}
			}
			if len(only) > 6 {
				only = only[:6]
			}
			samples = append(samples, path+"\n"+strings.Join(only, "\n"))
		}
	}
	// Anti-vacuity on the allowlist: an entry that no longer diverges is a dead
	// exemption and must be deleted, not kept.
	for key := range sugarLintKnownDivergence {
		found := false
		for _, path := range files {
			if lspKnownDivergenceKey(path) == key {
				found = true
			}
		}
		if !found {
			t.Errorf("allowlist entry %q does not match any corpus file — delete it", key)
		}
	}
	if stillDiverging != len(sugarLintKnownDivergence) {
		t.Errorf("allowlist has %d entries but only %d still diverge — the others are "+
			"dead exemptions and must be deleted",
			len(sugarLintKnownDivergence), stillDiverging)
	}
	if len(offenders) == 0 {
		t.Logf("%d files: LSP diagnostics are identical under both parse shapes "+
			"(%d known divergence%s, surface being the wrong side)",
			len(files), stillDiverging, map[bool]string{true: "", false: "s"}[stillDiverging == 1])
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	t.Errorf("LSP diagnostics differ on %d/%d corpus files outside the allowlist: %s\nfirst differences:\n%s",
		len(offenders), len(files), strings.Join(shown, ", "), strings.Join(samples, "\n"))
}

// valueCtxArmProg is a value-context type match (the tests/type-match.no shape):
// each arm's last expression is the arm's VALUE, not a discarded statement, and
// the arm's binding type (`w i64`) is carried only by the sugar node.
const valueCtxArmProg = `num = i32 | i64 | f64

add1 = (x num) {
    r = x: {
        w i64 -> w + 1
        w i32 -> w + 2
        w f64 -> w + 3.0
        -> 0
    }
    print(r)
}
`

// armOverflowControlProg adds the control: the very same arithmetic sitting
// OUTSIDE the match arm, which both shapes can see. Comparing the two programs
// is what proves which side is wrong in the remaining divergence.
const armOverflowControlProg = `num = i32 | i64 | f64

add1 = (x num) {
    a i64 = 5
    b = a + 1

    r = x: {
        w i64 -> w + 1
        w i32 -> w + 2
        w f64 -> w + 3.0
        -> 0
    }
    print(b)
    print(r)
}
`

// lintLinesAt filters a fingerprint down to the diagnostics on one source line.
func lintLinesAt(fp string, line string) []string {
	var out []string
	for _, l := range strings.Split(fp, "\n") {
		if strings.HasPrefix(l, line+":") || strings.HasPrefix(l, line+"\t") {
			out = append(out, l)
		}
	}
	return out
}

// TestSugarValueCtxArmIsNotADiscardedOverflow pins the false positive that the
// sugar descent used to create: in a value-context match the arm's trailing
// expression is the arm's value, so it must NOT be reported as "integer result
// discarded". The desugared shape already got this right (walkArm in
// ValidateUnhandledOverflow skips the trailing expression statement under
// valueCtx); the sugar branch did not, and reported 2 bogus `ovfhndld` hints on
// tests/type-match.no. Both shapes must now agree: zero.
func TestSugarValueCtxArmIsNotADiscardedOverflow(t *testing.T) {
	for _, sh := range []struct {
		name string
		cfg  func(*parser.Parser)
	}{{"surface", lspCfgSurface}, {"sugar", lspCfgSugar}} {
		fp, ok := lintFingerprint("valuectx.no", valueCtxArmProg, sh.cfg)
		if !ok {
			t.Fatalf("%s: parse failed", sh.name)
		}
		if n := strings.Count(fp, "ovfhndld"); n != 0 {
			t.Errorf("%s: value-context arm must not raise ovfhndld, got %d:\n%s",
				sh.name, n, fp)
		}
	}
}

// TestSugarArmOverflowDivergenceIsSurfaceUnderReporting proves WHICH side is
// wrong in the one remaining divergence, instead of just allowing it.
//
// Three independent facts, all measured on armOverflowControlProg:
//
//  1. surface DOES fire `ovf-int-default` for `b = a + 1` outside the arm
//     (line 5) — so the rule is not disabled, it recognises this arithmetic;
//  2. sugar fires it on `w + 1` / `w + 2` inside the arm (lines 8/9), surface
//     fires nothing there — surface cannot see the arm arithmetic at all,
//     because the arm's binding type (`w i64`) does not survive its expansion;
//  3. sugar does NOT fire on the f64 arm (`w + 3.0`, line 10) — the judgement
//     is type-exact, not indiscriminate.
//
// `w` is bound i64/i32 by the arm itself, so `w + 1` really does return ?T: the
// hint is true and merely invisible to the old shape. That is an under-report,
// not a false positive — the sugar shape is the correct one.
func TestSugarArmOverflowDivergenceIsSurfaceUnderReporting(t *testing.T) {
	surface, okS := lintFingerprint("control.no", armOverflowControlProg, lspCfgSurface)
	sugar, okC := lintFingerprint("control.no", armOverflowControlProg, lspCfgSugar)
	if !okS || !okC {
		t.Fatal("parse failed")
	}
	// (1) The rule is alive on this very arithmetic — outside the arm.
	for _, sh := range []struct {
		name string
		fp   string
	}{{"surface", surface}, {"sugar", sugar}} {
		if n := len(lintLinesAt(sh.fp, "5")); n == 0 {
			t.Fatalf("%s: expected the overflow hint on the outside-arm arithmetic "+
				"(line 5); the fixture no longer reproduces: %s", sh.name, sh.fp)
		}
	}
	// (2) Inside the arm, only sugar sees it.
	if n := len(lintLinesAt(sugar, "8")); n == 0 {
		t.Errorf("sugar must see the i64 arm arithmetic (line 8), got none:\n%s", sugar)
	}
	if n := len(lintLinesAt(surface, "8")); n != 0 {
		t.Errorf("premise is stale: surface now reports the arm arithmetic (%d hints) — "+
			"the divergence was fixed, so delete the allowlist entry and this test: %v",
			n, lintLinesAt(surface, "8"))
	}
	// (3) …and it stays type-exact: the f64 arm is not integer arithmetic.
	if n := len(lintLinesAt(sugar, "10")); n != 0 {
		t.Errorf("sugar must not report the f64 arm (line 10) as integer overflow: %v",
			lintLinesAt(sugar, "10"))
	}
}
