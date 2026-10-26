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
// Stage 5 of the THIR plan: NodeFacts behaviour + migration equivalence.
//
// This stage is a PURE REFACTOR: the 18 SemanticContext accessors keep their
// signatures and merely forward to the new table, so every downstream
// read/write site (checker's ~50 SourceFileOf call sites, build, tohir, dump)
// is untouched. What can still break is subtle:
//
//  1. TWO of the nine setters have asymmetric false-semantics —
//     `SetModuleConst(n,false)` and `SetIndexInBounds(n,false)` must only CLEAR
//     an existing entry, never create one. The entry set feeds the blob key
//     space and Merge (which treats "key exists" as "don't overwrite").
//  2. all nine facts must survive a blob HIT. Losing SourceFile silently
//     reattributes module diagnostics to the main file; losing ModuleConst /
//     IndexInBounds silently disables const propagation and the index-guard
//     exemption; losing the union marks kills monomorphisation (the Tier-0 std
//     blob path does NOT re-run monomorphizeUnions); losing Callee makes
//     checker mistake a plain argument for a receiver. None of it errors.
//  3. `NodeFact.EmbedFiles` is the only map in the table — it must be routed
//     through a sorted shadow or the blob stops being byte-reproducible.
// ─────────────────────────────────────────────────────────────────────────────

func nfTestNode() *Identifier {
	return &Identifier{Token: lexer.Token{Type: lexer.IDENT, Literal: "nf"}}
}

// TestNodeFactsFalseDoesNotCreateEntry pins the asymmetric guard on the two
// bool setters: a FALSE write must not create an entry, a TRUE write must.
//
// The other seven setters are unconditional `ensure` — this asymmetry is not a
// wart to clean up, it is the pre-migration behaviour of
// `SemanticContext.SetModuleConstOf` / `SetIndexInBounds` preserved verbatim.
func TestNodeFactsFalseDoesNotCreateEntry(t *testing.T) {
	tb := NewNodeFacts()
	n := nfTestNode()

	tb.SetModuleConst(n, false)
	tb.SetIndexInBounds(n, false)
	if tb.Len() != 0 {
		t.Fatalf("a false write created an entry: Len=%d", tb.Len())
	}
	if tb.ModuleConstOf(n) || tb.IndexInBoundsOf(n) {
		t.Fatal("a false write reported true")
	}

	tb.SetModuleConst(n, true)
	tb.SetIndexInBounds(n, true)
	if tb.Len() != 1 {
		t.Fatalf("both true writes must share ONE entry: Len=%d", tb.Len())
	}
	if !tb.ModuleConstOf(n) || !tb.IndexInBoundsOf(n) {
		t.Fatal("true writes not readable")
	}

	// And clearing again must not resurrect / delete the entry.
	tb.SetModuleConst(n, false)
	if tb.Len() != 1 {
		t.Fatalf("clearing deleted the entry: Len=%d", tb.Len())
	}
	if tb.ModuleConstOf(n) {
		t.Error("clearing did not take effect")
	}
	if !tb.IndexInBoundsOf(n) {
		t.Error("clearing ModuleConst clobbered IndexInBounds")
	}
}

// TestNodeFactsNilSafety mirrors the Stage 2/3/4 guards: a blob-decoded
// Program's Sem is rebuilt by detToSem, which carries no NodeFacts, so every
// accessor has to degrade quietly.
func TestNodeFactsNilSafety(t *testing.T) {
	var tb *NodeFacts
	n := nfTestNode()
	tb.SetEmbedData(n, []byte{1})                    // must not panic
	tb.SetEmbedFiles(n, map[string][]byte{"a": {1}}) // must not panic
	tb.SetCallee(n, CalleeModuleFn)                  // must not panic
	tb.SetIndexInBounds(n, true)                     // must not panic
	tb.SetModuleConst(n, true)                       // must not panic
	tb.SetSourceFile(n, "/x.no")                     // must not panic
	tb.SetModuleOwner(n, "x")                        // must not panic
	tb.SetVariadicUnion(n, "num")                    // must not panic
	tb.SetGenericUnion(n, "num")                     // must not panic
	if tb.EmbedDataOf(n) != nil || tb.EmbedFilesOf(n) != nil {
		t.Error("nil NodeFacts must report no embed payload")
	}
	if tb.CalleeOf(n) != CalleeUnknown {
		t.Error("nil NodeFacts must report CalleeUnknown")
	}
	if tb.IndexInBoundsOf(n) || tb.ModuleConstOf(n) {
		t.Error("nil NodeFacts must report false bools")
	}
	if tb.SourceFileOf(n) != "" || tb.ModuleOwnerOf(n) != "" ||
		tb.VariadicUnionOf(n) != "" || tb.GenericUnionOf(n) != "" {
		t.Error("nil NodeFacts must report empty strings")
	}
	if tb.Len() != 0 {
		t.Error("nil NodeFacts Len must be 0")
	}
	if tb.Clone(nil) != nil {
		t.Error("nil Clone must return nil")
	}
	tb.Merge(NewNodeFacts()) // must not panic

	live := NewNodeFacts()
	live.SetEmbedData(nil, []byte{1})
	live.SetEmbedFiles(nil, map[string][]byte{"a": {1}})
	live.SetCallee(nil, CalleeMethod)
	live.SetIndexInBounds(nil, true)
	live.SetModuleConst(nil, true)
	live.SetSourceFile(nil, "/x.no")
	live.SetModuleOwner(nil, "x")
	live.SetVariadicUnion(nil, "num")
	live.SetGenericUnion(nil, "num")
	if live.Len() != 0 {
		t.Errorf("a nil node created an entry: Len=%d", live.Len())
	}
}

// TestNodeFactsWritesDoNotTouchOtherTables is the separation assertion: writing
// node facts creates an entry in the node-facts table ONLY.
//
// ⚠️ 四個寫入全部落在**同一個** `NodeFact` 條目上（九個槽位共存，見
// `TestNodeFactsSlotsIndependent`），所以條目數是 +1 不是 +4 —— 這正是本測試
// 要釘的語義，別寫成 +4。
func TestNodeFactsWritesDoNotTouchOtherTables(t *testing.T) {
	sem := NewSemanticContext()
	n := nfTestNode()
	before := semTableLens(sem)
	sem.SetEmbedData(n, []byte{1})
	sem.SetSourceFileOf(n, "/repo/src/std/math.no")
	sem.SetModuleConstOf(n, true)
	sem.SetVariadicUnionOf(n, "num")
	after := semTableLens(sem)
	want := [2]int{before[0], before[1] + 1}
	if after != want {
		t.Errorf("writing node facts touched another table: before=%v after=%v want=%v",
			before, after, want)
	}
	if got := sem.SourceFileOf(n); got != "/repo/src/std/math.no" {
		t.Fatalf("SourceFileOf not readable after write: %q", got)
	}
	if !sem.ModuleConstOf(n) {
		t.Fatal("ModuleConstOf not readable after write")
	}
}

// TestNodeFactsSlotsIndependent pins that the nine slots do not clobber each
// other: a node can carry embed bytes, a callee kind, both bools and four
// strings at once.
func TestNodeFactsSlotsIndependent(t *testing.T) {
	tb := NewNodeFacts()
	n := nfTestNode()
	tb.SetEmbedData(n, []byte{7, 8})
	tb.SetEmbedFiles(n, map[string][]byte{"a.bin": {1}})
	tb.SetCallee(n, CalleeMethod)
	tb.SetIndexInBounds(n, true)
	tb.SetModuleConst(n, true)
	tb.SetSourceFile(n, "/repo/src/std/path.no")
	tb.SetModuleOwner(n, "path")
	tb.SetVariadicUnion(n, "num")
	tb.SetGenericUnion(n, "ord")

	if !bytes.Equal(tb.EmbedDataOf(n), []byte{7, 8}) {
		t.Errorf("EmbedDataOf = %v", tb.EmbedDataOf(n))
	}
	if len(tb.EmbedFilesOf(n)) != 1 {
		t.Errorf("EmbedFilesOf = %v", tb.EmbedFilesOf(n))
	}
	if tb.CalleeOf(n) != CalleeMethod {
		t.Errorf("CalleeOf = %v", tb.CalleeOf(n))
	}
	if !tb.IndexInBoundsOf(n) || !tb.ModuleConstOf(n) {
		t.Error("bool slots clobbered each other")
	}
	if tb.SourceFileOf(n) != "/repo/src/std/path.no" {
		t.Errorf("SourceFileOf = %q", tb.SourceFileOf(n))
	}
	if tb.ModuleOwnerOf(n) != "path" {
		t.Errorf("ModuleOwnerOf = %q", tb.ModuleOwnerOf(n))
	}
	if tb.VariadicUnionOf(n) != "num" || tb.GenericUnionOf(n) != "ord" {
		t.Errorf("union slots clobbered: %q / %q", tb.VariadicUnionOf(n), tb.GenericUnionOf(n))
	}
}

// TestNodeFactsCloneCopiesEmbedFiles pins the clone contract: keys are remapped
// onto the cloned nodes AND the EmbedFiles map is deep-copied. Union
// monomorphization clones statements; a write to the clone must not land on the
// template, and an in-place edit of the clone's file map must not be visible
// through the original.
func TestNodeFactsCloneCopiesEmbedFiles(t *testing.T) {
	tb := NewNodeFacts()
	n := nfTestNode()
	tb.SetEmbedFiles(n, map[string][]byte{"a.bin": {1}})
	tb.SetSourceFile(n, "/x.no")

	// remap == nil: keys kept, values still copied.
	cp := tb.Clone(nil)
	if cp.Len() != 1 {
		t.Fatalf("Clone(nil) lost entries: %d", cp.Len())
	}
	cp.EmbedFilesOf(n)["b.bin"] = []byte{2}
	if len(tb.EmbedFilesOf(n)) != 1 {
		t.Error("Clone shares the EmbedFiles map with the original; " +
			"a clone-side edit leaked back")
	}

	// remap: keys move onto the clone's nodes.
	other := nfTestNode()
	remapped := tb.Clone(func(Node) Node { return other })
	if remapped.Len() != 1 {
		t.Fatalf("Clone(remap) lost entries: %d", remapped.Len())
	}
	if _, ok := remapped.entries[n]; ok {
		t.Error("Clone(remap) kept the ORIGINAL key")
	}
	if got := remapped.SourceFileOf(other); got != "/x.no" {
		t.Errorf("Clone(remap) fact not reachable through the new key: %q", got)
	}
}

// TestNodeFactsMergeDoesNotOverwrite pins the merge contract used at the module
// merge point: an existing key is never overwritten, a new one is added.
func TestNodeFactsMergeDoesNotOverwrite(t *testing.T) {
	a := NewNodeFacts()
	n := nfTestNode()
	a.SetSourceFile(n, "/main.no")
	a.SetModuleOwner(n, "sql")

	b := NewNodeFacts()
	b.SetSourceFile(n, "/std/sql.no") // would clobber if Merge overwrote
	m := nfTestNode()
	b.SetGenericUnion(m, "num")

	a.Merge(b)
	if got := a.SourceFileOf(n); got != "/main.no" {
		t.Errorf("Merge overwrote an existing entry: %q", got)
	}
	if got := a.ModuleOwnerOf(n); got != "sql" {
		t.Errorf("Merge clobbered a sibling slot: %q", got)
	}
	if got := a.GenericUnionOf(m); got != "num" {
		t.Errorf("Merge did not import the new key: %q", got)
	}
	if a.Len() != 2 {
		t.Errorf("Merge entry count = %d, want 2", a.Len())
	}
}

// TestNodeFactsBlobCarriesNoFacts is THE Stage 5 gate, re-expressed for the
// 2026-10-26 collapse (`CachedProgram` carries the syntax tree only).
//
// It pins three things at once, and the first two are what keep the third from
// being vacuous:
//
//  1. a decode WITHOUT BuildSemViews yields an EMPTY node-fact table, even
//     though the tree it was encoded from had one — proof the blob carries no
//     derived data;
//  2. BuildSemViews regenerates exactly the facts that have a pure post-pass
//     (`IndexInBounds` via AnalyzeInBoundsIndex);
//  3. the eight ASSEMBLY-time facts (SourceFile / ModuleOwner / EmbedData /
//     EmbedFiles / Callee / VariadicUnion / GenericUnion / ModuleConst) are
//     gone after the round-trip — deliberately, and not a regression: they are
//     written by build/transpiler_inject.go and the union monomorphiser AFTER
//     the cache is filled, so they were never in the blob to begin with
//     (measured: all eight have zero entries at Put time).
//
// Nothing errors if (1) or (2) regresses: diagnostics get reattributed to the
// main file (reporting line numbers that file does not have) and the index-guard
// exemption silently stops firing.
func TestNodeFactsBlobCarriesNoFacts(t *testing.T) {
	src := "f = () (r i64) {\n" +
		"    n [4]i64\n" +
		"    n[1] = 7\n" +
		"    r = n[1]\n" +
		"}\n"
	cfg := SemRebuildConfig{Filename: "nf-round-trip.no"}
	p := New(lexer.NewCached("nf-round-trip.no", src))
	cfg.Apply(p)
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}

	// Anti-vacuity: the fixture must actually produce a parse-time in-bounds proof.
	countInBounds := func(pr *Program) int {
		n := 0
		walkProgramNodes(pr, func(node Node) {
			if pr.Sem.IndexIsInBounds(node) {
				n++
			}
		})
		return n
	}
	if countInBounds(prog) == 0 {
		t.Fatal("anti-vacuity: fixture produced no IndexInBounds fact")
	}

	// Plant the assembly-time facts on a real node so their disappearance is
	// observable rather than assumed.
	var fn Node
	walkProgramNodes(prog, func(n Node) {
		if _, ok := n.(*FunctionDefinition); ok && fn == nil {
			fn = n
		}
	})
	if fn == nil {
		t.Fatal("anti-vacuity: fixture has no FunctionDefinition")
	}
	prog.Sem.SetSourceFileOf(fn, "/repo/src/std/math.no")
	prog.Sem.SetModuleOwnerOf(fn, "math")
	prog.Sem.SetVariadicUnionOf(fn, "num")
	prog.Sem.SetGenericUnionOf(fn, "ord")
	prog.Sem.SetEmbedData(fn, []byte{1, 2, 3})
	prog.Sem.SetEmbedFiles(fn, map[string][]byte{"b.bin": {2}, "a.bin": {1}})
	prog.Sem.SetCallee(fn, CalleeMethod)

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: prog}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var dcp CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&dcp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dcp.Prog == nil {
		t.Fatal("decode produced no program")
	}

	// (1) The blob carries no fact of any kind.
	if got := dcp.Prog.NodeFacts().Len(); got != 0 {
		t.Fatalf("blob carried %d node facts; CachedProgram must hold the syntax tree only", got)
	}

	// (2) The rebuildable fact comes back.
	BuildSemViews(dcp.Prog, cfg)
	if got := countInBounds(dcp.Prog); got == 0 {
		t.Fatal("BuildSemViews restored no IndexInBounds fact; " +
			"the index-guard exemption would silently stop firing on a cache HIT")
	}

	// (3) The assembly-time facts stay absent — they are not the blob's job.
	var dfn Node
	walkProgramNodes(dcp.Prog, func(n Node) {
		if _, ok := n.(*FunctionDefinition); ok && dfn == nil {
			dfn = n
		}
	})
	if dfn == nil {
		t.Fatal("decoded tree lost the FunctionDefinition")
	}
	sem := dcp.Prog.Sem
	if got := sem.SourceFileOf(dfn); got != "" {
		t.Errorf("SourceFile came back as %q; assembly-time facts are not rebuilt", got)
	}
	if got := sem.EmbedDataOf(dfn); got != nil {
		t.Errorf("EmbedData came back as %v; assembly-time facts are not rebuilt", got)
	}
	if got := sem.CalleeOf(dfn); got != CalleeUnknown {
		t.Errorf("Callee came back as %v; assembly-time facts are not rebuilt", got)
	}
}

// TestParsePopulatesNodeFacts is the anti-vacuity half for the corpus: the
// table has to be genuinely filled by real parses (the bounds analyzer writes
// IndexInBounds during lowering, which still runs inside ParseProgram).
func TestParsePopulatesNodeFacts(t *testing.T) {
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
		if prog.NodeFacts() != nil && prog.NodeFacts().Len() > 0 {
			nonEmpty++
		}
	}
	if checked < 20 {
		t.Fatalf("anti-vacuity: only %d corpus files parsed cleanly, want >= 20", checked)
	}
	if nonEmpty == 0 {
		t.Fatalf("anti-vacuity: NodeFacts empty for all %d parsed corpus files", checked)
	}
	t.Logf("NodeFacts populated for %d/%d parsed corpus files", nonEmpty, checked)
}
