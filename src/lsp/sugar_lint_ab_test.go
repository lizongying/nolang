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
// parse shape (`lowered → sugar` drifts on 10 traces).
//
// §4.3（2026-10-17）已刪除 surface 形態（SkipUnwrapLowering + SkipSafeIndexLowering）
// 與其 A/B 比較門禁；糖形態（SkipLowering）是唯一非編譯形態。本檔由「sugar vs
// surface」A/B 改為「糖形態整體語料冒煙測試」+ 針對糖形態正確性的 fixture 斷言。
// 原有的 allowlist（sugarLintKnownDivergence，tests/type-match.no 等 surface 漏報處）
// 隨 surface 退役一併刪除——糖形態才是正確側。

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

// TestLspLintsSugarCorpusCensus is the instrument: it lints the whole corpus
// under the sugar shape and logs how many files parse and how many produce a
// non-empty diagnostic set. Never fails — it is the observation tool.
func TestLspLintsSugarCorpusCensus(t *testing.T) {
	files := sugarLspCorpus(t)
	var ok, unparseable, withHints int
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		fp, okFp := lintFingerprint(path, string(b), lspCfgSugar)
		if !okFp {
			unparseable++
			continue
		}
		ok++
		if fp != "" {
			withHints++
		}
	}
	t.Logf("lsp-lints sugar-shape census: corpus=%d parseOK=%d unparseable=%d withHints=%d",
		len(files), ok, unparseable, withHints)
}

// TestLspLintsSugarCorpusSmoke gates the diagnostics half of the LSP flip at the
// corpus level: every corpus file must parse and produce a (possibly empty)
// lint fingerprint under the sugar shape without panicking or being rejected.
func TestLspLintsSugarCorpusSmoke(t *testing.T) {
	files := sugarLspCorpus(t)
	var offenders []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, okFp := lintFingerprint(path, string(b), lspCfgSugar); !okFp {
			offenders = append(offenders, path)
		}
	}
	if len(offenders) == 0 {
		t.Logf("%d files: LSP diagnostics parse cleanly under the sugar shape", len(files))
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	t.Errorf("LSP diagnostics fail to parse on %d/%d corpus files: %s", len(offenders), len(files), strings.Join(shown, ", "))
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
// OUTSIDE the match arm, which both shapes can see. It is the reference that
// proves the rule is alive and type-exact.
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
// sugar descent must NOT create: in a value-context match the arm's trailing
// expression is the arm's value, so it must NOT be reported as "integer result
// discarded" (`ovfhndld`). The desugared shape already got this right (walkArm in
// ValidateUnhandledOverflow skips the trailing expression statement under
// valueCtx); the sugar branch must agree: zero.
func TestSugarValueCtxArmIsNotADiscardedOverflow(t *testing.T) {
	fp, ok := lintFingerprint("valuectx.no", valueCtxArmProg, lspCfgSugar)
	if !ok {
		t.Fatal("parse failed")
	}
	if n := strings.Count(fp, "ovfhndld"); n != 0 {
		t.Errorf("value-context arm must not raise ovfhndld, got %d:\n%s", n, fp)
	}
}

// TestSugarArmOverflowSeesValueCtxArmArithmetic proves the sugar shape reports
// the arm binding's integer arithmetic, which the old surface shape could not
// see (it lost the arm's binding type on expansion). Three facts, all measured
// on armOverflowControlProg:
//
//  1. the rule is alive on the outside-arm arithmetic (line 5);
//  2. sugar fires on the i64/i32 arm arithmetic (lines 8/9) — the binding type
//     `w i64`/`w i32` survives in the sugar node, so `w + 1` is recognised as
//     returning ?T;
//  3. sugar stays type-exact: the f64 arm (line 10) is NOT integer overflow.
func TestSugarArmOverflowSeesValueCtxArmArithmetic(t *testing.T) {
	sugar, okC := lintFingerprint("control.no", armOverflowControlProg, lspCfgSugar)
	if !okC {
		t.Fatal("parse failed")
	}
	// (1) The rule is alive on this very arithmetic — outside the arm.
	if n := len(lintLinesAt(sugar, "5")); n == 0 {
		t.Fatalf("expected the overflow hint on the outside-arm arithmetic (line 5); "+
			"the fixture no longer reproduces: %s", sugar)
	}
	// (2) Inside the arm, sugar sees it.
	if n := len(lintLinesAt(sugar, "8")); n == 0 {
		t.Errorf("sugar must see the i64 arm arithmetic (line 8), got none:\n%s", sugar)
	}
	if n := len(lintLinesAt(sugar, "9")); n == 0 {
		t.Errorf("sugar must see the i32 arm arithmetic (line 9), got none:\n%s", sugar)
	}
	// (3) …and it stays type-exact: the f64 arm is not integer arithmetic.
	if n := len(lintLinesAt(sugar, "10")); n != 0 {
		t.Errorf("sugar must not report the f64 arm (line 10) as integer overflow: %v",
			lintLinesAt(sugar, "10"))
	}
}
