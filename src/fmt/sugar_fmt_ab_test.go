package fmt

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// S4 harness — docs/design/nolang-parser-lowering-invariants.md §8 step S4.
//
// S4 makes the consumers of the parser (`no fmt`, `no fix`, LSP, astpatch) read
// the *sugar* AST instead of today's partially-lowered "surface" AST. Those two
// configurations differ by exactly one thing today:
//
//	surface  SkipUnwrapLowering + SkipSafeIndexLowering  (what `no fmt` uses)
//	sugar    SkipLowering                               (nothing is expanded)
//
// so the whole of S4 for the formatter can be measured as: does formatting the
// sugar tree reproduce formatting the surface tree, byte for byte, over the
// corpus? Every difference is either (a) a sugar node the formatter does not
// know how to render, or (b) formatting that silently depended on a desugared
// shape. Both are exactly the failure modes S4 is supposed to remove.
//
// This file is the ruler; fmt/control.go's `case *parser.SurfaceMatch` is the
// fix. It reports first (census) and gates second.

const sugarFmtCorpusRoot = "../../tests"

// sugarFmtGateEnabled flips the hard gate on. It stayed false while the
// formatter had no `case *parser.SurfaceMatch` at all (the sugar tree rendered
// matches as empty text: 169/532 differ) and was switched on once the census
// reached its floor. (S0 convention: a gate that cannot fail yet is not an
// argument that the invariant holds.)
const sugarFmtGateEnabled = true

// sugarFmtKnownDivergence lists the corpus files where the two configurations
// are allowed to disagree. Every entry must say *which side is wrong* — an
// entry without a root cause is just a suppressed failure.
var sugarFmtKnownDivergence = map[string]string{
	"tests/type-match.no": "surface is WRONG, sugar is right: formatting the " +
		"desugared type-match loses the subject and every arm pattern " +
		"(`r = x: { i32 -> lo ... }` comes out as `r = : { lo } else: { ... }`). " +
		"The surface output does not even reparse. The sugar tree keeps the " +
		"match node intact, so it round-trips. TestSugarFmtDivergenceIsAlwaysSurfaceBeingWrong " +
		"enforces both halves of this claim.",
}

// sugarFmtCorpus returns every *.no under tests/, sorted. A missing corpus is a
// hard failure, not a skip: a silently empty corpus is what made more than one
// gate in this repo vacuous (see docs §18.5 `ctl_empty`).
func sugarFmtCorpus(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(sugarFmtCorpusRoot, func(path string, d os.DirEntry, err error) error {
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
		t.Fatalf("walking %s: %v", sugarFmtCorpusRoot, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .no files under %s — corpus path is wrong; refusing to report a vacuous 0-differ", sugarFmtCorpusRoot)
	}
	sort.Strings(files)
	return files
}

// parseCfg describes one parser configuration the A/B compares.
type parseCfg struct {
	name  string
	apply func(*parser.Parser)
}

var (
	// cfgSurface is what `no fmt` / LSP / astpatch use today.
	cfgSurface = parseCfg{"surface", func(p *parser.Parser) {
		p.SkipUnwrapLowering = true
		p.SkipSafeIndexLowering = true
	}}
	// cfgSugar is the S1/S4 target shape: nothing is expanded at all.
	cfgSugar = parseCfg{"sugar", func(p *parser.Parser) {
		p.SkipLowering = true
	}}
)

// fmtUnderConfig parses src with cfg and formats the resulting tree.
func fmtUnderConfig(src string, cfg parseCfg) (out string, perrs []string) {
	lx := lexer.New(src)
	p := parser.New(lx)
	cfg.apply(p)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", p.Errors()
	}
	o, ok, errs := formatProgramAST(prog, src)
	if !ok {
		return "", errs
	}
	return ensureTrailingNewline(o), nil
}

// fmtCLIUnderConfig reproduces what `no fmt` actually does to a file: parse,
// ask the checker which `#{overflow=...}` annotations are ineffective, then
// format with that information (see fmtProcessFile in cmd/no/main.go).
//
// This is a *different* measurement from fmtUnderConfig: flipping the parse
// configuration changes not only what the formatter sees but also what the
// checker computes, so a green formatter A/B alone does not license the flip.
func fmtCLIUnderConfig(src string, cfg parseCfg) (out string, perrs []string) {
	lx := lexer.New(src)
	p := parser.New(lx)
	cfg.apply(p)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", p.Errors()
	}
	relevant, governed := checker.OverflowAnnotationRelevance(prog)
	return FormatProgramWithOverflow(prog, src, relevant, governed), nil
}

// TestNoFmtSugarVsSurfaceCensus is the instrument for the consumer flip.
func TestNoFmtSugarVsSurfaceCensus(t *testing.T) {
	files := sugarFmtCorpus(t)
	var differ, same, errs int
	var diffPaths []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		a, ae := fmtCLIUnderConfig(string(b), cfgSurface)
		c, ce := fmtCLIUnderConfig(string(b), cfgSugar)
		if len(ae) > 0 || len(ce) > 0 {
			errs++
			continue
		}
		if a != c {
			differ++
			diffPaths = append(diffPaths, path)
		} else {
			same++
		}
	}
	t.Logf("no-fmt A/B: corpus=%d identical=%d differ=%d unparseable=%d",
		len(files), same, differ, errs)
	shown := diffPaths
	if len(shown) > 12 {
		shown = shown[:12]
	}
	for _, p := range shown {
		t.Logf("  DIFF %s", p)
	}
}

// TestNoFmtSugarMatchesSurface gates the consumer flip: `no fmt` on the sugar
// tree must produce the same bytes as on the surface tree, minus the files
// where the surface formatter is known to be broken.
func TestNoFmtSugarMatchesSurface(t *testing.T) {
	if !sugarFmtGateEnabled {
		t.Skip("sugarFmtGateEnabled is false")
	}
	files := sugarFmtCorpus(t)
	var offenders []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		a, ae := fmtCLIUnderConfig(string(b), cfgSurface)
		c, ce := fmtCLIUnderConfig(string(b), cfgSugar)
		if len(ae) > 0 || len(ce) > 0 {
			continue
		}
		if a != c && !isKnownDivergence(path) {
			offenders = append(offenders, path)
		}
	}
	if len(offenders) == 0 {
		t.Logf("%d files: `no fmt` output is identical under both parse configurations", len(files))
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	t.Errorf("`no fmt` output differs on %d/%d corpus files: %s",
		len(offenders), len(files), strings.Join(shown, ", "))
}

// sugarFmtPair is one corpus file's A/B result.
type sugarFmtPair struct {
	path string
	// parseErr is set when either config fails to parse; such files are
	// excluded from the byte comparison (a formatter that refuses the file
	// cannot be compared) but counted and reported.
	parseErr string
	// onlySurfaceErr / onlySugarErr flag a *parse-acceptance* difference: one
	// configuration accepts the file and the other does not. That is a real S4
	// behavior change even though there is no output to diff.
	onlySurfaceErr bool
	onlySugarErr   bool
	differ         bool
	surface, sugar string
}

func sugarFmtAB(t *testing.T) []sugarFmtPair {
	t.Helper()
	files := sugarFmtCorpus(t)
	out := make([]sugarFmtPair, 0, len(files))
	for _, path := range files {
		srcBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(srcBytes)
		pr := sugarFmtPair{path: path}

		surfaceOut, surfaceErrs := fmtUnderConfig(src, cfgSurface)
		sugarOut, sugarErrs := fmtUnderConfig(src, cfgSugar)

		switch {
		case len(surfaceErrs) > 0 && len(sugarErrs) > 0:
			pr.parseErr = surfaceErrs[0]
			pr.surface, pr.sugar = surfaceOut, sugarOut
		case len(surfaceErrs) > 0:
			pr.onlySurfaceErr = true
			pr.parseErr = surfaceErrs[0]
			pr.sugar = sugarOut
		case len(sugarErrs) > 0:
			pr.onlySugarErr = true
			pr.parseErr = sugarErrs[0]
			pr.surface = surfaceOut
		default:
			pr.surface, pr.sugar = surfaceOut, sugarOut
			pr.differ = surfaceOut != sugarOut
		}
		out = append(out, pr)
	}
	return out
}

// TestSugarFmtVsSurfaceFmtCensus prints the A/B. It never fails: it is the
// instrument, and the instrument has to keep working while the fix lands.
func TestSugarFmtVsSurfaceFmtCensus(t *testing.T) {
	pairs := sugarFmtAB(t)

	var differ, bothErr, onlySurface, onlySugar, same int
	var diffPaths []string
	for _, pr := range pairs {
		switch {
		case pr.onlySurfaceErr:
			onlySurface++
		case pr.onlySugarErr:
			onlySugar++
		case len(pr.parseErr) > 0:
			bothErr++
		case pr.differ:
			differ++
			diffPaths = append(diffPaths, pr.path)
		default:
			same++
		}
	}

	t.Logf("corpus=%d  identical=%d  differ=%d  both-unparseable=%d  only-surface-rejects=%d  only-sugar-rejects=%d",
		len(pairs), same, differ, bothErr, onlySurface, onlySugar)

	shown := diffPaths
	if len(shown) > 12 {
		shown = shown[:12]
	}
	for _, p := range shown {
		t.Logf("  DIFF %s", p)
	}
	if onlySurface > 0 {
		for _, pr := range pairs {
			if pr.onlySurfaceErr {
				t.Logf("  ONLY-SURFACE-REJECTS %s: %s", pr.path, pr.parseErr)
			}
		}
	}
	if onlySugar > 0 {
		for _, pr := range pairs {
			if pr.onlySugarErr {
				t.Logf("  ONLY-SUGAR-REJECTS %s: %s", pr.path, pr.parseErr)
			}
		}
	}
}

// TestSugarFmtDumpOne prints the two renderings of a single corpus file.
// Set NOLANG_FMT_AB_FILE to a corpus path fragment (e.g. "json.no"); without it
// the test does nothing. This is the iteration tool for driving the census to 0.
func TestSugarFmtDumpOne(t *testing.T) {
	want := os.Getenv("NOLANG_FMT_AB_FILE")
	if want == "" {
		t.Skip("set NOLANG_FMT_AB_FILE to a corpus path fragment to dump one file's A/B")
	}
	var found bool
	for _, pr := range sugarFmtAB(t) {
		if !strings.Contains(pr.path, want) {
			continue
		}
		found = true
		t.Logf("=== %s (differ=%v) ===", pr.path, pr.differ)
		a := strings.Split(pr.surface, "\n")
		b := strings.Split(pr.sugar, "\n")
		n := len(a)
		if len(b) > n {
			n = len(b)
		}
		for i := 0; i < n; i++ {
			var sa, sb string
			if i < len(a) {
				sa = a[i]
			}
			if i < len(b) {
				sb = b[i]
			}
			mark := " "
			if sa != sb {
				mark = "*"
			}
			t.Logf("%s%4d | %-60s | %s", mark, i+1, sa, sb)
		}
	}
	if !found {
		t.Fatalf("no corpus file matched %q", want)
	}
}

// firstDiffLine returns a compact "surface vs sugar" excerpt for one file.
func firstDiffLine(pr sugarFmtPair) (string, string) {
	a := strings.Split(pr.surface, "\n")
	b := strings.Split(pr.sugar, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		var sa, sb string
		if i < len(a) {
			sa = a[i]
		}
		if i < len(b) {
			sb = b[i]
		}
		if sa != sb {
			return sa, sb
		}
	}
	return "", ""
}

// knownDivergenceKey returns the allowlist entry that covers path (suffix
// match, so the caller may pass an absolute or corpus-relative path), or "".
func knownDivergenceKey(path string) string {
	for p := range sugarFmtKnownDivergence {
		if strings.HasSuffix(path, p) {
			return p
		}
	}
	return ""
}

func isKnownDivergence(path string) bool { return knownDivergenceKey(path) != "" }

// TestSugarFmtMatchesSurfaceFmt is the S4 gate: formatting the sugar tree must
// reproduce the surface formatting byte for byte over the whole corpus, except
// where the surface formatter is itself broken (see sugarFmtKnownDivergence).
func TestSugarFmtMatchesSurfaceFmt(t *testing.T) {
	if !sugarFmtGateEnabled {
		t.Skip("sugarFmtGateEnabled is false — the formatter cannot render the sugar tree yet")
	}
	pairs := sugarFmtAB(t)
	var offenders []string
	var staleEntries []string
	stillDiverging := map[string]bool{}
	for _, pr := range pairs {
		if pr.differ || pr.onlySurfaceErr || pr.onlySugarErr {
			if k := knownDivergenceKey(pr.path); k != "" {
				stillDiverging[k] = true
				continue
			}
			offenders = append(offenders, pr.path)
		}
	}
	// Anti-vacuity, half 1: an allowlist entry that no longer diverges is a dead
	// exemption — it must be deleted, not kept around "just in case".
	for p := range sugarFmtKnownDivergence {
		if !stillDiverging[p] {
			staleEntries = append(staleEntries, p)
		}
	}
	if len(staleEntries) > 0 {
		sort.Strings(staleEntries)
		t.Errorf("allowlisted file(s) no longer diverge — delete the entry from "+
			"sugarFmtKnownDivergence: %s", strings.Join(staleEntries, ", "))
	}
	if len(offenders) == 0 {
		t.Logf("%d files: sugar formatting == surface formatting byte-for-byte "+
			"(%d known divergence(s), each with a stated root cause)",
			len(pairs), len(stillDiverging))
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	var sb strings.Builder
	for _, p := range shown {
		for _, pr := range pairs {
			if pr.path != p {
				continue
			}
			sa, sb2 := firstDiffLine(pr)
			sb.WriteString("\n  " + p + "\n    surface: " + strings.TrimRight(sa, "\n") +
				"\n    sugar  : " + strings.TrimRight(sb2, "\n"))
			break
		}
	}
	t.Errorf("sugar formatting differs on %d/%d corpus files outside the allowlist; "+
		"first offenders:%s", len(offenders), len(pairs), sb.String())
}

// TestSugarFmtDivergenceIsAlwaysSurfaceBeingWrong is the anti-vacuity partner of
// sugarFmtKnownDivergence: an allowlist entry is only credible if we can show
// which side is broken. For every file the two configurations actually disagree
// on, the surface rendering must be the broken one (it fails to reparse) and the
// sugar rendering must be sound (reparses, and formatting it again is a no-op).
//
// If someone fixes the surface path, the "surface must not reparse" half fails
// and says so explicitly — that is the signal to drop the allowlist entry, not
// to weaken this test.
func TestSugarFmtDivergenceIsAlwaysSurfaceBeingWrong(t *testing.T) {
	if !sugarFmtGateEnabled {
		t.Skip("sugarFmtGateEnabled is false")
	}
	checked := 0
	for _, pr := range sugarFmtAB(t) {
		if !pr.differ || !isKnownDivergence(pr.path) {
			continue
		}
		checked++

		if _, errs := fmtUnderConfig(pr.surface, cfgSurface); len(errs) == 0 {
			t.Errorf("%s: allowlisted as \"surface is wrong\", but the surface output "+
				"reparses cleanly. Either the surface formatter was fixed (delete the "+
				"allowlist entry) or the divergence has a different root cause — update "+
				"sugarFmtKnownDivergence either way.", pr.path)
		}

		once, errs := fmtUnderConfig(pr.sugar, cfgSugar)
		if len(errs) > 0 {
			t.Errorf("%s: sugar output does not reparse: %v", pr.path, errs)
			continue
		}
		twice, errs := fmtUnderConfig(once, cfgSugar)
		if len(errs) > 0 {
			t.Errorf("%s: sugar output is not idempotent (second pass failed): %v", pr.path, errs)
			continue
		}
		if once != twice {
			t.Errorf("%s: sugar output is not idempotent:\nfirst:\n%s\nsecond:\n%s",
				pr.path, once, twice)
		}
	}
	if checked != len(sugarFmtKnownDivergence) {
		t.Errorf("checked %d diverging file(s) but the allowlist has %d entries; "+
			"the corpus may have stopped exercising some of them",
			checked, len(sugarFmtKnownDivergence))
	}
}
