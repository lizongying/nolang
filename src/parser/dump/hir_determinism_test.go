package dump

// ─────────────────────────────────────────────────────────────────────────────
// HIR determinism gate.
//
// Found while building TestH0MatchDesugarMatchesParserLowering: 6 corpus files
// diverged between two pipelines that should agree, and the divergence was the
// ORDER of functions in pkg.Top, not their content. Compiling the SAME source
// with the SAME pipeline twice in the same process produced different output.
//
// Root cause: hir.MonomorphizeGo (async_group.go) iterated a MAP to decide the
// order in which `-async` monomorph clones are appended to p.Top:
//
//	for name := range asyncColored {          // ← randomized
//		newID, _ := p.cloneSubtree(fid)
//		p.Top = append(p.Top, newID)
//	}
//
// Go randomizes map iteration order per range statement, so the emitted HIR —
// and therefore the generated code — differed run to run. Measured on
// tests/async.no: 18 functions, identical set, differing order from index 11
// onward. Fixed by sorting the names before iterating; the goSpawn collection
// was sorted for the same reason.
//
// Why this needs its own gate rather than being folded into the equivalence
// test: that test compares two DIFFERENT pipelines, so it can only notice
// non-determinism by accident. This one asserts the stronger, cheaper property
// directly — one pipeline, one input, N runs, byte-identical output.
//
// A non-deterministic compiler breaks reproducible builds, every golden/hashed
// comparison downstream, and any A/B diff harness that does not control for
// process-level randomness. It is also the single most expensive class of bug
// to diagnose from the outside, because the symptom is "sometimes it differs".
// ─────────────────────────────────────────────────────────────────────────────

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hirDeterminismRuns is how many times each program is lowered. 8 is enough to
// catch map-iteration randomization (Go's range randomization produces a fresh
// order essentially every time for maps of this size) while keeping the corpus
// sweep fast.
const hirDeterminismRuns = 8

// TestHIRIsDeterministic lowers every corpus program N times with the same
// pipeline and requires byte-identical HIR dumps.
//
// It uses the surface configuration (SkipUnwrapLowering + SkipSafeIndexLowering)
// so that programs using `go` still reach MonomorphizeGo, which is the pass that
// used to randomize the output.
func TestHIRIsDeterministic(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	var compared int
	var unstable []string

	walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(raw)

		var first string
		var have bool
		for i := 0; i < hirDeterminismRuns; i++ {
			dump, good := lowerToHIR(path, src, true)
			if !good {
				return nil // parse/lowering failure is a different question
			}
			if i == 0 {
				first, have = dump, true
				continue
			}
			if dump != first {
				unstable = append(unstable, path)
				return nil
			}
		}
		if have {
			compared++
		}
		return nil
	})
	_ = walkErr

	if compared < hirEquivalenceMinCompared {
		t.Fatalf("only %d programs lowered deterministically; the gate would be vacuous",
			compared)
	}
	if len(unstable) > 0 {
		const show = 10
		t.Errorf("%d/%d programs produce NON-DETERMINISTIC HIR: lowering the same "+
			"source %d times with the same pipeline yields different output. The "+
			"first %d: %v", len(unstable), compared, hirDeterminismRuns,
			min(show, len(unstable)), unstable[:min(show, len(unstable))])
	}

	t.Logf("%d programs x %d runs: all byte-identical", compared, hirDeterminismRuns)
}
