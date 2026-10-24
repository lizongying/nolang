package parser

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stage 3 of the THIR plan: AnnTable behaviour + migration equivalence.
//
// The guarded risks are the OPPOSITE of Stage 2's FmtTrivia in one respect and
// identical in another:
//
//   - FmtTrivia is deliberately NOT serialized (fmt/LSP always fresh-parse).
//     AnnTable MUST be serialized: `#{platform=...}` drives build's platform
//     filter, `#{overflow=...}` drives overflow codegen, `#{index-out=...}`
//     drives the bounds guard. Losing them on a cache HIT does not error — it
//     silently produces a different program. TestAnnTableSurvivesCacheRoundTrip
//     is the gate.
//   - Both tables must keep overwrite-vs-accumulate semantics byte-exact and
//     every accessor nil-safe (a blob-decoded Program has an empty Sem).
//
// And one behavioural gain that needs pinning: PlatformKeysOf / GenericParamsOf
// are now PURE FUNCTIONS of the annotation entries, so they no longer depend on
// whether ResolveProgram has run. TestPlatformKeysOfIsPureFunction pins that;
// before Stage 3 a single-file vet path (no ResolveProgram) saw no platform keys.
// ─────────────────────────────────────────────────────────────────────────────

func annTestNode() *Identifier {
	return &Identifier{Token: lexer.Token{Type: lexer.IDENT, Literal: "n"}}
}

// TestAnnTableSetOverwrites pins the overwrite semantics of the original
// `SetRawAnnotations`: a rewriting pass (e.g. carryOverflowAnnotation) replaces
// the whole entry set. Appending instead would re-trigger overflow handling.
func TestAnnTableSetOverwrites(t *testing.T) {
	tb := NewAnnTable()
	n := annTestNode()
	tb.Set(n, []*AnnotationEntry{{Key: "a"}, {Key: "b"}})
	if got := len(tb.Of(n)); got != 2 {
		t.Fatalf("first Set recorded %d entries, want 2", got)
	}
	tb.Set(n, []*AnnotationEntry{{Key: "c"}})
	ents := tb.Of(n)
	if len(ents) != 1 || ents[0].Key != "c" {
		t.Fatalf("Set did not overwrite: %d entries left", len(ents))
	}
}

// TestAnnTableNilSafety mirrors the FmtTrivia guard: a blob-decoded Program's
// Sem is rebuilt by detToSem, which does not carry an AnnTable, so every
// accessor has to degrade quietly.
func TestAnnTableNilSafety(t *testing.T) {
	var tb *AnnTable
	n := annTestNode()
	tb.Set(n, []*AnnotationEntry{{Key: "a"}}) // must not panic
	if tb.Of(n) != nil {
		t.Error("nil AnnTable must report no entries")
	}
	if tb.Len() != 0 {
		t.Error("nil AnnTable Len must be 0")
	}
	if tb.Clone(nil) != nil {
		t.Error("nil Clone must return nil")
	}
	tb.Merge(NewAnnTable()) // must not panic

	live := NewAnnTable()
	live.Set(nil, []*AnnotationEntry{{Key: "a"}})
	if live.Len() != 0 {
		t.Errorf("nil node created an entry: Len=%d", live.Len())
	}
}

// semTableLens 是三張 node-keyed 語義表的條目數快照。
//
// Stage 7 前，四個 `TestXWritesDoNotCreateSemEntries` 測試用 `HasSemantics(n)`
// 證明「寫新表不會順手在 nodeSem 留下條目」。nodeSem 空殼連同該方法已於 Stage 7
// 刪除，判别力改由「一張表的寫入不影響其他表」承擔：任何一張表若被併回同一份
// 存儲、或某個訪問器寫錯了表，下面的交叉斷言會紅。三個 Len() 都 nil 安全。
func semTableLens(sem *SemanticContext) [3]int {
	return [3]int{sem.Anns().Len(), sem.TypeFactsTab().Len(), sem.NodeFactsTab().Len()}
}

// TestAnnWritesDoNotTouchOtherTables is the separation assertion: writing an
// annotation creates an entry in the annotation table ONLY — the two fact
// tables must be untouched.
func TestAnnWritesDoNotTouchOtherTables(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	before := semTableLens(sem)
	sem.SetRawAnnotations(n, []*AnnotationEntry{{Key: "overflow"}})
	after := semTableLens(sem)
	want := [3]int{before[0] + 1, before[1], before[2]}
	if after != want {
		t.Errorf("SetRawAnnotations touched another table: before=%v after=%v want=%v",
			before, after, want)
	}
	if got := len(sem.RawAnnotationsOf(n)); got != 1 {
		t.Fatalf("annotation not readable after write: %d entries", got)
	}
}

// TestAnnotationsOfEqualsRawAnnotationsOf pins the Stage 3 unification: the two
// accessors read one store, so the "has ResolveProgram run yet?" distinction
// that forced `AnnotationsOf(n) || RawAnnotationsOf(n)` double-queries
// downstream is gone.
func TestAnnotationsOfEqualsRawAnnotationsOf(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	// Write through one accessor, read through the other, WITHOUT ResolveProgram.
	sem.SetRawAnnotations(n, []*AnnotationEntry{{Key: "overflow"}})
	if len(sem.AnnotationsOf(n)) != 1 {
		t.Error("AnnotationsOf must see entries written via SetRawAnnotations " +
			"without running ResolveProgram")
	}
	// And the reverse direction.
	m := annTestNode()
	sem.SetAnnotations(m, []*AnnotationEntry{{Key: "index-out"}})
	if len(sem.RawAnnotationsOf(m)) != 1 {
		t.Error("RawAnnotationsOf must see entries written via SetAnnotations")
	}
}

// TestPlatformKeysOfIsPureFunction pins the behavioural fix: platform keys are
// computed from the entries on demand, not materialised by ResolveProgram.
// Before Stage 3 every path that skipped ResolveProgram (single-file vet,
// dependency-module parse) saw nil here.
func TestPlatformKeysOfIsPureFunction(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	sem.SetRawAnnotations(n, []*AnnotationEntry{
		{Key: "mac-arm64"}, // ValidPlatformKeys member (boolean key)
		{Key: "overflow", Value: &AnnotationIdentValue{Value: "wrap"}}, // not a platform key
	})
	// Stage 7 前這裡查 `HasSemantics(n)`（證明 PlatformKeysOf 不順手建 nodeSem
	// 條目）。nodeSem 已刪，等價的判别力是「讀不改變任何一張表的條目數」。
	before := semTableLens(sem)
	// No ResolveProgram call — deliberately.
	keys := sem.PlatformKeysOf(n)
	if len(keys) != 1 || keys[0] != "mac-arm64" {
		t.Fatalf("PlatformKeysOf = %v, want [mac-arm64]", keys)
	}
	if after := semTableLens(sem); after != before {
		t.Errorf("PlatformKeysOf is not pure: table lengths changed %v -> %v", before, after)
	}
}

// TestGenericParamsOfIsPureFunction is the same pin for `#{generic=[K,V]}`.
func TestGenericParamsOfIsPureFunction(t *testing.T) {
	sem := NewSemanticContext()
	n := annTestNode()
	sem.SetRawAnnotations(n, []*AnnotationEntry{{
		Key:   "generic",
		Value: &AnnotationArrayValue{Elements: []AnnotationValue{&AnnotationIdentValue{Value: "K"}}},
	}})
	// No ResolveProgram call.
	if got := sem.GenericParamsOf(n); len(got) != 1 || got[0] != "K" {
		t.Fatalf("GenericParamsOf = %v, want [K]", got)
	}
}

// TestAnnTableSurvivesCacheRoundTrip is THE Stage 3 gate.
//
// A blob HIT must restore annotations onto the same nodes. Nothing errors if it
// does not — the platform filter keeps a `mac-arm64`-only function, or overflow
// handling silently disappears. This is the exact failure mode that produced the
// `nolang-overflow-ineffective` false positive on cache hits only.
func TestAnnTableSurvivesCacheRoundTrip(t *testing.T) {
	src := "#{mac-arm64}\ntarget = 1\n\n#{overflow=wrap}\nother = 2\n"
	p := New(lexer.NewCached("ann-round-trip.no", src))
	p.Filename = "ann-round-trip.no"
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}

	// Anti-vacuity: the parse must really have attached annotations.
	if prog.Anns() == nil || prog.Anns().Len() == 0 {
		t.Fatal("anti-vacuity: no annotations attached, the round-trip below is vacuous")
	}
	before := prog.Anns().Len()

	// Round-trip through the real cache wrapper.
	cp := &CachedProgram{
		Prog:      prog,
		Anns:      SerializeAnns(prog),
		TypeFacts: SerializeTypeFacts(prog),
		NodeFacts: SerializeNodeFacts(prog),
	}
	if cp.Anns == nil {
		t.Fatal("anti-vacuity: SerializeAnns produced nothing")
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var dcp CachedProgram
	if err := gob.NewDecoder(&buf).Decode(&dcp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	after := dcp.Prog.Anns().Len()
	if after != before {
		t.Fatalf("annotation count changed across the blob round-trip: before=%d after=%d", before, after)
	}
	// And the platform key must still be derivable on the decoded tree.
	found := false
	walkProgramNodes(dcp.Prog, func(n Node) {
		if ks := dcp.Prog.Sem.PlatformKeysOf(n); len(ks) == 1 && ks[0] == "mac-arm64" {
			found = true
		}
	})
	if !found {
		t.Error("platform key lost across the blob round-trip; " +
			"FilterByPlatform would silently drop platform-specific code on a cache HIT")
	}
	// Same for the overflow entry, which drives codegen.
	foundOverflow := false
	walkProgramNodes(dcp.Prog, func(n Node) {
		for _, e := range dcp.Prog.Sem.AnnotationsOf(n) {
			if e.Key == "overflow" {
				foundOverflow = true
			}
		}
	})
	if !foundOverflow {
		t.Error("overflow annotation lost across the blob round-trip")
	}
}

// TestParsePopulatesAnnTable is the anti-vacuity half for the corpus: the table
// has to be genuinely filled by a real parse, otherwise every assertion above
// that uses a real Program is meaningless.
func TestParsePopulatesAnnTable(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil || len(files) == 0 {
		t.Skipf("no corpus under ../../tests: %v", err)
	}
	const sample = 80
	checked, nonEmpty := 0, 0
	for i, path := range files {
		if i >= sample {
			break
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		p := New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		prog := p.ParseProgram() // full lowering, the build shape
		if prog == nil || len(p.Errors()) > 0 {
			continue
		}
		checked++
		if prog.Anns() != nil && prog.Anns().Len() > 0 {
			nonEmpty++
		}
	}
	if checked < 20 {
		t.Fatalf("anti-vacuity: only %d corpus files parsed cleanly, want >= 20", checked)
	}
	if nonEmpty == 0 {
		t.Fatalf("anti-vacuity: AnnTable empty for all %d parsed corpus files", checked)
	}
	t.Logf("AnnTable populated for %d/%d parsed corpus files", nonEmpty, checked)
}

// TestCloneProgramCopiesAnnTable pins the clone contract: keys are remapped onto
// the cloned nodes (union monomorphization clones statements and their
// annotations must travel with them).
func TestCloneProgramCopiesAnnTable(t *testing.T) {
	src := "#{overflow=wrap}\nf = (i i64) (r i64) {\n  r = i + 1\n  r\n}\n"
	p := New(lexer.NewCached("ann-clone.no", src))
	p.Filename = "ann-clone.no"
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	if prog.Anns() == nil || prog.Anns().Len() == 0 {
		t.Fatal("anti-vacuity: no annotations to clone")
	}
	cp := CloneProgram(prog)
	if cp.Anns().Len() != prog.Anns().Len() {
		t.Fatalf("clone lost annotations: before=%d after=%d", prog.Anns().Len(), cp.Anns().Len())
	}
	// Keys must point into the CLONE, not the original tree.
	origKeys := make(map[Node]bool)
	for n := range prog.Anns().entries {
		origKeys[n] = true
	}
	for n := range cp.Anns().entries {
		if origKeys[n] {
			t.Error("clone's AnnTable still keys an ORIGINAL node; " +
				"a write to the clone would land on the template")
			break
		}
	}
}
