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
	want := [3]int{before[0], before[1], before[2] + 1}
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

// TestNodeFactsSurvivesCacheRoundTrip is THE Stage 5 gate.
//
// A blob HIT must restore all nine facts onto the same nodes. Nothing errors if
// it does not: diagnostics get reattributed to the main file (reporting line
// numbers that file does not have), const propagation and the index-guard
// exemption silently stop firing, and union monomorphisation marks vanish for
// std modules because the Tier-0 blob path never re-runs monomorphizeUnions.
func TestNodeFactsSurvivesCacheRoundTrip(t *testing.T) {
	src := "f = (i i64) (r ?i64) {\n  r = i + 1\n  r\n}\n"
	p := New(lexer.NewCached("nf-round-trip.no", src))
	p.Filename = "nf-round-trip.no"
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}

	// Plant all nine facts on real, walk-reachable nodes.
	var fn, let Node
	walkProgramNodes(prog, func(n Node) {
		switch n.(type) {
		case *FunctionDefinition:
			if fn == nil {
				fn = n
			}
		case *LetStatement:
			if let == nil {
				let = n
			}
		}
	})
	if fn == nil || let == nil {
		t.Fatal("anti-vacuity: fixture has no FunctionDefinition / LetStatement")
	}
	prog.Sem.SetSourceFileOf(fn, "/repo/src/std/math.no")
	prog.Sem.SetModuleOwnerOf(fn, "math")
	prog.Sem.SetVariadicUnionOf(fn, "num")
	prog.Sem.SetGenericUnionOf(fn, "ord")
	prog.Sem.SetEmbedData(fn, []byte{1, 2, 3})
	prog.Sem.SetEmbedFiles(fn, map[string][]byte{"b.bin": {2}, "a.bin": {1}})
	prog.Sem.SetModuleConstOf(let, true)
	prog.Sem.SetIndexInBounds(let, true)
	prog.Sem.SetCallee(let, CalleeMethod)

	before := prog.NodeFacts().Len()
	if before < 2 {
		t.Fatalf("anti-vacuity: only %d node-fact entries planted", before)
	}

	cp := &CachedProgram{
		Prog:      prog,
		Anns:      SerializeAnns(prog),
		TypeFacts: SerializeTypeFacts(prog),
		NodeFacts: SerializeNodeFacts(prog),
	}
	if cp.NodeFacts == nil {
		t.Fatal("anti-vacuity: SerializeNodeFacts produced nothing")
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var dcp CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&dcp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dcp.NodeFacts == nil {
		t.Fatal("NodeFacts missing from the decoded CachedProgram")
	}
	// Anti-vacuity: both planted nodes must be re-locatable on the decoded tree.
	var dfn, dlet Node
	walkProgramNodes(dcp.Prog, func(n Node) {
		switch n.(type) {
		case *FunctionDefinition:
			if dfn == nil {
				dfn = n
			}
		case *LetStatement:
			if dlet == nil {
				dlet = n
			}
		}
	})
	if dfn == nil || dlet == nil {
		t.Fatal("anti-vacuity: decoded tree lost the planted nodes")
	}
	sem := dcp.Prog.Sem
	if got := sem.SourceFileOf(dfn); got != "/repo/src/std/math.no" {
		t.Errorf("SourceFile lost across the round-trip: %q", got)
	}
	if got := sem.ModuleOwnerOf(dfn); got != "math" {
		t.Errorf("ModuleOwner lost across the round-trip: %q", got)
	}
	if got := sem.VariadicUnionOf(dfn); got != "num" {
		t.Errorf("VariadicUnion lost across the round-trip: %q", got)
	}
	if got := sem.GenericUnionOf(dfn); got != "ord" {
		t.Errorf("GenericUnion lost across the round-trip: %q", got)
	}
	if got := sem.EmbedDataOf(dfn); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Errorf("EmbedData lost across the round-trip: %v", got)
	}
	files := sem.EmbedFilesOf(dfn)
	if len(files) != 2 || !bytes.Equal(files["a.bin"], []byte{1}) || !bytes.Equal(files["b.bin"], []byte{2}) {
		t.Errorf("EmbedFiles lost across the round-trip: %v", files)
	}
	if !sem.ModuleConstOf(dlet) {
		t.Error("ModuleConst lost across the round-trip")
	}
	if !sem.IndexIsInBounds(dlet) {
		t.Error("IndexInBounds lost across the round-trip")
	}
	if sem.CalleeOf(dlet) != CalleeMethod {
		t.Errorf("Callee lost across the round-trip: %v", sem.CalleeOf(dlet))
	}
	if got := dcp.Prog.NodeFacts().Len(); got != before {
		t.Errorf("node-fact count changed across the round-trip: before=%d after=%d", before, got)
	}
}

// TestNodeFactsEmbedFilesEncodingIsDeterministic pins the one property
// EmbedFiles endangers: `NodeFact.EmbedFiles` is the only map in the table, and
// gob encodes maps in randomized iteration order. A map that reaches gob would
// make the std blob un-reproducible (genstdsig's "content unchanged → don't
// rewrite" guard would never fire, and two builds would produce different `no`
// binaries). Several entries are required — a one-entry map cannot expose
// order sensitivity.
func TestNodeFactsEmbedFilesEncodingIsDeterministic(t *testing.T) {
	m := map[uint32]*NodeFact{
		1: {EmbedFiles: map[string][]byte{"b.bin": {2}, "a.bin": {1}, "c.bin": {3}}},
		2: {EmbedFiles: map[string][]byte{"z.bin": {26}, "y.bin": {25}}},
		3: {SourceFile: "/repo/src/std/math.no", Callee: CalleeMethod},
	}
	var prev []byte
	for i := 0; i < 5; i++ {
		d := detCached{NodeFacts: detFromNodeFactsMap(m)}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(&d); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if i == 0 {
			prev = buf.Bytes()
			continue
		}
		if !bytes.Equal(prev, buf.Bytes()) {
			t.Fatalf("NodeFacts encoding is not deterministic: run 0 = %d bytes, "+
				"run %d = %d bytes with different content", len(prev), i, buf.Len())
		}
	}
	if len(prev) == 0 {
		t.Fatal("encoded 0 bytes")
	}
	// And the round-trip must still restore the maps.
	var d detCached
	if err := gob.NewDecoder(bytes.NewReader(prev)).Decode(&d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	back := detToNodeFactsMap(d.NodeFacts)
	if len(back) != 3 {
		t.Fatalf("round-trip lost entries: %d", len(back))
	}
	if len(back[1].EmbedFiles) != 3 || back[1].EmbedFiles["c.bin"][0] != 3 {
		t.Errorf("EmbedFiles lost across the round-trip: %v", back[1].EmbedFiles)
	}
	if back[3].SourceFile != "/repo/src/std/math.no" || back[3].Callee != CalleeMethod {
		t.Errorf("scalar slots lost across the round-trip: %+v", back[3])
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
