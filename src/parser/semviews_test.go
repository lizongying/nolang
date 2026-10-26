package parser

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// semviews_test.go — the permanent gate for the 2026-10-26 cache collapse.
//
// `CachedProgram` is `{Prog}` only: the blob is structured source code, so it
// carries no compiler-derived data (see semviews.go for the reasoning). What
// makes that safe is this equivalence claim:
//
//	encode {Prog} → decode → BuildSemViews(cfg)   ≡   fresh parse under cfg
//
// on BOTH the reconstructed fact tables and the HIR they lower to. Without the
// gate, "the rebuild is good enough" is an assertion; with it, a divergence
// shows up as a failing corpus sweep instead of as a subtly wrong binary.
//
// Why HIR equality is the oracle and not just the fact tables: a new node-keyed
// fact table added without a rebuild step would be invisible to a hand-written
// per-fact fingerprint (nobody would remember to add a case), but it changes the
// HIR. The fact-table fingerprint is still checked separately, because a fact
// that only the checker consumes (and not the HIR) would otherwise slip through.

// TestBuildSemViewsMatchesFreshParse is the corpus sweep.
func TestBuildSemViewsMatchesFreshParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus found under ../../tests: %v", err)
	}
	if more, err := filepath.Glob(filepath.Join("..", "std", "*.no")); err == nil {
		files = append(files, more...)
	}

	checked, skipped, factDiverged, hirDiverged := 0, 0, 0, 0
	totalInferred, totalInBounds := 0, 0

	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		// Same configuration the build path uses for a bare parse of this file.
		cfg := SemRebuildConfig{Filename: filepath.Base(path)}
		p := New(lexer.NewCached(path, string(src)))
		cfg.Apply(p)
		fresh := p.ParseProgram()
		if fresh == nil || len(p.Errors()) > 0 {
			// Deliberately-broken fixtures live in tests/ too; they have no
			// parse output to cache.
			skipped++
			continue
		}

		// Fingerprint BEFORE anything lowers: ASTToHIR writes assembly-time
		// facts (TypeOverride) back into Sem, and that is not what we compare.
		wantFacts := factFingerprint(fresh)
		wantHIR := ASTToHIR(fresh).Dump()
		totalInferred += countNonEmpty(wantFacts, 0)
		totalInBounds += countNonEmpty(wantFacts, 2)

		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: fresh}); err != nil {
			t.Fatalf("%s: encode: %v", path, err)
		}
		var decoded CachedProgram
		if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
		if decoded.Prog == nil {
			t.Fatalf("%s: decode produced no program", path)
		}
		// The blob must be syntax only — checked per file so a future field
		// cannot ride along unnoticed.
		if n := decoded.Prog.TypeFacts().Len(); n != 0 {
			t.Fatalf("%s: blob carried %d type facts; CachedProgram must hold the syntax tree only", path, n)
		}

		BuildSemViews(decoded.Prog, cfg)

		gotFacts := factFingerprint(decoded.Prog)
		if len(gotFacts) != len(wantFacts) {
			factDiverged++
			t.Errorf("%s: occurrence count changed across decode+rebuild: fresh=%d rebuilt=%d",
				path, len(wantFacts), len(gotFacts))
		} else {
			for i := range wantFacts {
				if wantFacts[i] != gotFacts[i] {
					factDiverged++
					if factDiverged <= 5 {
						t.Errorf("%s: fact #%d differs: fresh=%q rebuilt=%q",
							path, i, wantFacts[i], gotFacts[i])
					}
					break
				}
			}
		}

		if got := ASTToHIR(decoded.Prog).Dump(); got != wantHIR {
			hirDiverged++
			if hirDiverged <= 5 {
				t.Errorf("%s: HIR from the rebuilt AST differs from a fresh parse "+
					"(%d vs %d bytes); a derived fact did not survive decode+rebuild",
					path, len(wantHIR), len(got))
			}
		}
		checked++
	}

	// Anti-vacuity #1: a glob that matched nothing, or a corpus that turned out
	// to be all syntax errors, would let this gate pass forever.
	if checked < 500 {
		t.Fatalf("only %d files round-tripped (%d skipped); expected the bulk of the 600-file corpus",
			checked, skipped)
	}
	// Anti-vacuity #2: if the corpus produced no facts at all, comparing fact
	// fingerprints proves nothing.
	if totalInferred == 0 || totalInBounds == 0 {
		t.Fatalf("corpus produced %d InferredType and %d IndexInBounds facts; "+
			"the fact comparison would be vacuous", totalInferred, totalInBounds)
	}
	if factDiverged == 0 && hirDiverged == 0 {
		t.Logf("%d files rebuilt identically (%d skipped); %d InferredType / %d IndexInBounds facts",
			checked, skipped, totalInferred, totalInBounds)
	}
}

// factFingerprint renders every node occurrence's derived facts in walk order.
//
// Occurrence order (not pointer identity) is the point: the rebuilt tree is a
// different set of objects, so the only meaningful comparison is "the Nth node
// the walker reaches carries the same facts". A rebuild that attached facts to
// the wrong nodes therefore shows up as a mismatch rather than as an equal count.
//
// Layout per entry: InferredType | TypeOverride | InBounds marker.
func factFingerprint(prog *Program) []string {
	out := make([]string, 0, 256)
	walkProgramNodes(prog, func(n Node) {
		inb := "-"
		if prog.Sem.IndexIsInBounds(n) {
			inb = "ib"
		}
		out = append(out, prog.Sem.InferredType(n)+"|"+prog.Sem.TypeOverrideOf(n)+"|"+inb)
	})
	return out
}

// countNonEmpty counts fingerprint entries whose field at index field is not
// empty. Used for the anti-vacuity totals above.
func countNonEmpty(fps []string, field int) int {
	n := 0
	for _, fp := range fps {
		// Split on '|' by hand: only three fields, and strings.Split in a
		// per-node loop over the whole corpus is the hot path here.
		parts := 0
		start := 0
		val := ""
		for i := 0; i <= len(fp); i++ {
			if i == len(fp) || fp[i] == '|' {
				if parts == field {
					val = fp[start:i]
					break
				}
				parts++
				start = i + 1
			}
		}
		if val != "" && val != "-" {
			n++
		}
	}
	return n
}
