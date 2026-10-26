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
// Stage 4 of the THIR plan: TypeFacts behaviour + migration equivalence.
//
// This stage is a PURE REFACTOR: the seven SemanticContext accessors keep their
// signatures and merely forward to the new table, so the 11 downstream
// read/write sites are untouched. What can still break is subtle:
//
//  1. the two string setters have the SAME empty-value semantics —
//     `SetInferred("")`/`SetOverride("")` must NOT create an entry (it would
//     change the entry set, hence the blob layout and merge results).
//     ⚠️ There used to be a third setter, `SetByOptionArith`, with the OPPOSITE
//     rule (always recorded, even false). It was removed on 2026-10-26 together
//     with its field: the bit was a walk-local decision, now passed by value.
//  2. both remaining facts must survive a blob HIT — losing InferredType empties
//     the KLet HIR type and disables four checker option exemptions; losing
//     TypeOverride silently falls back to the declared type. Neither errors.
//  3. clone must remap keys, or a write to a union monomorph would land on the
//     template.
// ─────────────────────────────────────────────────────────────────────────────

func factsTestNode() *Identifier {
	return &Identifier{Token: lexer.Token{Type: lexer.IDENT, Literal: "n"}}
}

// TestTypeFactsEmptyStringIgnored pins the asymmetric guard: an empty type
// string must not create an entry. Creating one would change the entry set,
// which ripples into Merge (which treats "key exists" as "don't overwrite")
// and into the blob key space.
func TestTypeFactsEmptyStringIgnored(t *testing.T) {
	tb := NewTypeFacts()
	n := factsTestNode()
	tb.SetInferred(n, "")
	tb.SetOverride(n, "")
	if tb.Len() != 0 {
		t.Fatalf("empty type string created an entry: Len=%d", tb.Len())
	}
	// Non-empty writes do create one, and overwrite rather than accumulate.
	tb.SetInferred(n, "?i64")
	if got := tb.InferredOf(n); got != "?i64" {
		t.Fatalf("InferredOf = %q, want \"?i64\"", got)
	}
	tb.SetInferred(n, "?str")
	if got := tb.InferredOf(n); got != "?str" {
		t.Fatalf("SetInferred did not overwrite: %q", got)
	}
}

// TestTypeFactsNilSafety mirrors the Stage 2/3 guards: a blob-decoded Program's
// Sem is rebuilt by detToSem, which carries no TypeFacts, so every accessor has
// to degrade quietly.
func TestTypeFactsNilSafety(t *testing.T) {
	var tb *TypeFacts
	n := factsTestNode()
	tb.SetInferred(n, "?i64")    // must not panic
	tb.SetOverride(n, "vec.i64") // must not panic
	if tb.InferredOf(n) != "" {
		t.Error("nil TypeFacts must report no inferred type")
	}
	if tb.OverrideOf(n) != "" {
		t.Error("nil TypeFacts must report no override")
	}
	if tb.Len() != 0 {
		t.Error("nil TypeFacts Len must be 0")
	}
	if tb.Clone(nil) != nil {
		t.Error("nil Clone must return nil")
	}
	tb.Merge(NewTypeFacts()) // must not panic

	live := NewTypeFacts()
	live.SetInferred(nil, "?i64")
	live.SetOverride(nil, "vec.i64")
	if live.Len() != 0 {
		t.Errorf("nil node created an entry: Len=%d", live.Len())
	}
}

// TestTypeFactsWritesDoNotTouchOtherTables is the separation assertion: writing
// type facts creates an entry in the type-facts table ONLY. Both writes land on
// the SAME NodeFact-style entry, so the count goes up by one, not two.
func TestTypeFactsWritesDoNotTouchOtherTables(t *testing.T) {
	sem := NewSemanticContext()
	n := factsTestNode()
	before := semTableLens(sem)
	sem.SetInferredType(n, "?i64")
	sem.SetTypeOverrideOf(n, "vec.i64")
	after := semTableLens(sem)
	want := [2]int{before[0] + 1, before[1]}
	if after != want {
		t.Errorf("writing type facts touched another table: before=%v after=%v want=%v",
			before, after, want)
	}
	if got := sem.InferredType(n); got != "?i64" {
		t.Fatalf("InferredType not readable after write: %q", got)
	}
	if got := sem.TypeOverrideOf(n); got != "vec.i64" {
		t.Fatalf("TypeOverrideOf not readable after write: %q", got)
	}
}

// TestTypeFactsOverrideAndInferredCoexist pins that the two type strings are
// independent slots. The precedence chain (TypeOverride > Type > InferredType)
// is resolved by tohir's KLet, NOT by the table — so the table must keep both
// even when they disagree.
func TestTypeFactsOverrideAndInferredCoexist(t *testing.T) {
	sem := NewSemanticContext()
	n := factsTestNode()
	sem.SetInferredType(n, "?i64")
	sem.SetTypeOverrideOf(n, "vec.i64")
	if got := sem.InferredType(n); got != "?i64" {
		t.Fatalf("InferredType = %q, want \"?i64\" (clobbered by the override?)", got)
	}
	if got := sem.TypeOverrideOf(n); got != "vec.i64" {
		t.Fatalf("TypeOverrideOf = %q, want \"vec.i64\"", got)
	}
	if !sem.HasInferredType(n) {
		t.Error("HasInferredType must stay true alongside an override")
	}
}

// TestTypeFactsSurviveCacheRoundTrip is THE Stage 4 gate, re-expressed for the
// 2026-10-26 collapse (`CachedProgram` carries the syntax tree only).
//
// Two things must hold, and the second is what keeps the first from being
// vacuous:
//   - a decode WITHOUT BuildSemViews yields an EMPTY fact table — proof that the
//     blob really carries no derived data;
//   - a decode WITH BuildSemViews restores the inferred ?T onto the same nodes.
//
// Nothing errors if the second fails: the inferred ?T disappears (KLet's HIR
// Type goes empty, four checker option exemptions stop firing) — a silently
// wrong program, not a build failure.
//
// TypeOverride is deliberately NOT asserted. It is an assembly-time fact written
// after the cache is filled (measured: zero entries at Put time), so it was
// never in the blob, and no rebuild can invent it.
func TestTypeFactsSurviveCacheRoundTrip(t *testing.T) {
	src := "f = (i i64) (r ?i64) {\n  r = i + 1\n  r\n}\n"
	cfg := SemRebuildConfig{Filename: "facts-round-trip.no"}
	p := New(lexer.NewCached("facts-round-trip.no", src))
	cfg.Apply(p)
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}

	// Anti-vacuity: a real parse must have produced at least one inferred ?T.
	if prog.TypeFacts().Len() == 0 {
		t.Fatal("anti-vacuity: no type facts recorded, the round-trip below is vacuous")
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: prog}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var dcp CachedProgram
	if err := gob.NewDecoder(&buf).Decode(&dcp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dcp.Prog == nil {
		t.Fatal("decode produced no program")
	}

	// The blob must carry no derived fact at all.
	if got := dcp.Prog.TypeFacts().Len(); got != 0 {
		t.Fatalf("blob carried %d type facts; CachedProgram must hold the syntax tree only", got)
	}

	BuildSemViews(dcp.Prog, cfg)
	if got := dcp.Prog.TypeFacts().Len(); got == 0 {
		t.Fatal("BuildSemViews restored no type facts; " +
			"a cache HIT would emit an empty HIR type for every `?T` binding")
	}
	sawInferred := false
	walkProgramNodes(dcp.Prog, func(n Node) {
		if _, ok := n.(*LetStatement); !ok {
			return
		}
		if dcp.Prog.Sem.InferredType(n) != "" {
			sawInferred = true
		}
	})
	if !sawInferred {
		t.Error("InferredType lost across the decode+rebuild; " +
			"KLet would emit an empty HIR type on a cache HIT")
	}
}

// TestParsePopulatesTypeFacts is the anti-vacuity half for the corpus: the
// table has to be genuinely filled by real parses.
func TestParsePopulatesTypeFacts(t *testing.T) {
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
		if prog.TypeFacts() != nil && prog.TypeFacts().Len() > 0 {
			nonEmpty++
		}
	}
	if checked < 20 {
		t.Fatalf("anti-vacuity: only %d corpus files parsed cleanly, want >= 20", checked)
	}
	if nonEmpty == 0 {
		t.Fatalf("anti-vacuity: TypeFacts empty for all %d parsed corpus files", checked)
	}
	t.Logf("TypeFacts populated for %d/%d parsed corpus files", nonEmpty, checked)
}

// TestCloneProgramCopiesTypeFacts pins the clone contract: keys are remapped
// onto the cloned nodes. Union monomorphization clones statements and a write
// to the clone must not land on the template.
func TestCloneProgramCopiesTypeFacts(t *testing.T) {
	src := "f = (i i64) (r ?i64) {\n  r = i + 1\n  r\n}\n"
	p := New(lexer.NewCached("facts-clone.no", src))
	p.Filename = "facts-clone.no"
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	if prog.TypeFacts() == nil || prog.TypeFacts().Len() == 0 {
		t.Fatal("anti-vacuity: no type facts to clone")
	}
	cp := CloneProgram(prog)
	if cp.TypeFacts().Len() != prog.TypeFacts().Len() {
		t.Fatalf("clone lost type facts: before=%d after=%d",
			prog.TypeFacts().Len(), cp.TypeFacts().Len())
	}
	// Keys must point into the CLONE, not the original tree.
	origKeys := make(map[Node]bool)
	for n := range prog.TypeFacts().entries {
		origKeys[n] = true
	}
	for n := range cp.TypeFacts().entries {
		if origKeys[n] {
			t.Error("clone's TypeFacts still keys an ORIGINAL node; " +
				"a write to the clone would land on the template")
			break
		}
	}
}
