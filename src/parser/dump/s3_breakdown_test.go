package dump

// S3 measurement: the HONEST end-to-end divergence, and its breakdown by cause.
//
// ⚠️ This file corrects a measurement error. An earlier version of the
// end-to-end counter inside hir_equivalence_test.go compared
//
//	shipped: ParseProgram()                          (no flags)
//	target : ParseProgram(SkipLowering) + surface flags + ASTToHIR
//
// Those are not the same pipeline: the surface flags (SkipUnwrapLowering +
// SkipSafeIndexLowering) suppress inferOptionArith / maybeCaptureOptionAssign /
// maybeIndexOutAssign, which the shipped side still performs. The counter was
// therefore charging the target pipeline for work the flags had deliberately
// switched off. The surface flags belong to the MATCH-equivalence gate, where
// they are the isolation mechanism; they do not belong in an end-to-end
// measurement.
//
// The target pipeline is SkipLowering ALONE:
//
//	ParseProgram(SkipLowering) → ASTToHIR
//
// vs the shipped one:
//
//	ParseProgram()             → ASTToHIR
//
// That is what this file measures. It does not assert anything — it reports
// which kind each divergence is, so the S3 work can be ordered by measured
// impact instead of guessed.
import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// lowerToHIRSkipOnly parses with SkipLowering and NOTHING else — the real
// target pipeline.
func lowerToHIRSkipOnly(path, src string) (string, bool) {
	var dump string
	var ok bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				dump, ok = "", false
			}
		}()
		p := parser.New(lexer.NewCached(path, src))
		p.Filename = path
		p.SkipLowering = true
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			dump, ok = "", false
			return
		}
		// ParseProgram prepared the same read-only bounds proof side-table for
		// both default and SkipLowering paths before this H0 conversion.
		pkg := parser.ASTToHIR(prog)
		if pkg == nil {
			dump, ok = "", false
			return
		}
		dump, ok = pkg.Dump(), true
	}()
	return dump, ok
}

type s3Breakdown struct {
	inBoundsOnly []string
	typeSynthOnly []string
	structural   []string
	other        []string
	okCount      int
}

func (k *s3Breakdown) total() int {
	return len(k.inBoundsOnly) + len(k.typeSynthOnly) + len(k.structural) + len(k.other)
}

// classifyEndToEndDiff buckets one file's diff by the kind of node involved.
//
// ⚠️ The type bucket must match `t=` vs NO `t=` on a let node, not just
// `t=?X`. An earlier version looked only for " t=?" and missed the `it`
// bindings, where the shipped side carries `t=i64` and the target side
// carries no type at all — the same root cause (a synthesized LetStatement.Type
// that only the lowering pass writes) landing in the "other" bucket. 3 of the
// 51 divergences were misclassified that way.
func classifyEndToEndDiff(shipped, target string) string {
	sl := strings.Split(shipped, "\n")
	tl := strings.Split(target, "\n")
	n := len(sl)
	if len(tl) < n {
		n = len(tl)
	}
	sawFlag, sawType, sawSynth := false, false, false
	for i := 0; i < n; i++ {
		if sl[i] == tl[i] {
			continue
		}
		joined := sl[i] + "\n" + tl[i]
		switch {
		case strings.Contains(joined, "flags=0x400000"):
			sawFlag = true
		case strings.Contains(joined, "__idx_out_"), strings.Contains(joined, "__cap_"):
			sawSynth = true
		case strings.Contains(joined, " t="):
			// A let's type node differs in either direction: `t=?i64` vs
			// nothing, or `t=i64` vs nothing.
			sawType = true
		}
	}
	if len(sl) != len(tl) {
		sawSynth = true
	}
	switch {
	case sawSynth:
		return "structural"
	case sawFlag && sawType:
		return "both"
	case sawFlag:
		return "inBounds"
	case sawType:
		return "typeSynth"
	default:
		return "other"
	}
}

func TestS3EndToEndBreakdown(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	var k s3Breakdown

	walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(raw)
		target, okT := lowerToHIRSkipOnly(path, src)
		shipped, okS := lowerToHIR(path, src, false)
		if !okT && !okS {
			return nil
		}
		if okT != okS {
			k.other = append(k.other, fmt.Sprintf("%s: ok mismatch h0=%v ship=%v", path, okT, okS))
			return nil
		}
		k.okCount++
		if shipped == target {
			return nil
		}
		switch classifyEndToEndDiff(shipped, target) {
		case "inBounds":
			k.inBoundsOnly = append(k.inBoundsOnly, path)
		case "typeSynth":
			k.typeSynthOnly = append(k.typeSynthOnly, path)
		case "structural":
			k.structural = append(k.structural, path)
		default:
			k.other = append(k.other, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", corpusRoot, walkErr)
	}

	sort.Strings(k.inBoundsOnly)
	sort.Strings(k.typeSynthOnly)
	sort.Strings(k.structural)
	sort.Strings(k.other)

	t.Logf("HONEST end-to-end breakdown: %d/%d programs diverge", k.total(), k.okCount)
	t.Logf("  inBounds only (AnalyzeInBoundsIndex)  : %d  %v", len(k.inBoundsOnly), firstN(k.inBoundsOnly, 5))
	t.Logf("  typeSynth only (inferOptionArith)     : %d  %v", len(k.typeSynthOnly), firstN(k.typeSynthOnly, 5))
	t.Logf("  structural (__idx_out_/__cap_ desugar) : %d  %v", len(k.structural), firstN(k.structural, 5))
	t.Logf("  other (needs eyes)                    : %d  %v", len(k.other), firstN(k.other, 8))
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
