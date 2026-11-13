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
	// 門禁必須跑在**生產軌**（S3-c 後 HIRCaptureEnabled=true，見 capturefacts.go）。
	// 同包較早的 capturefacts_test.go 至今仍以 `defer ... = false` 把這個全域
	// 開關留在舊的 AST 降糖軌，且 option_capture_test.go 等 6 條測試隱式依賴
	// 那個外溢值；因此本測試不能靠「套件內測試順序」拿到正確設定，必須自己釘住。
	// 不這麼做的話，整包 `go test ./parser` 會在 number.no / str.no / yaml.no
	// 出現 HIR 分叉，而單跑本測試卻是全綠 —— 那個分叉是軌道設定，不是重建缺陷。
	prevCaptureTrack := HIRCaptureEnabled
	HIRCaptureEnabled = true
	defer func() { HIRCaptureEnabled = prevCaptureTrack }()

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
	//
	// 門檻用**比例**，不用寫死的檔數 —— 原值是 `checked < 280`（註釋說合併語料
	// 355 檔），但 `tests/*.no` 現在 174 檔、`src/std/*.no` 42 檔，合計 216，
	// 門檻永遠紅且理由與守的東西無關。比例式對語料增刪免疫。
	if len(files) < 50 {
		t.Fatalf("corpus glob matched only %d files; expected the bulk of tests/ + src/std", len(files))
	}
	if checked < len(files)*4/5 {
		t.Fatalf("only %d of %d files round-tripped (%d skipped); expected the bulk of the corpus",
			checked, len(files), skipped)
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
