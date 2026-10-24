package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stage 2 of the THIR plan: FmtTrivia behaviour + migration equivalence.
//
// The risk being guarded is subtle and silent: RTFlags and OpeningBraceComment
// used to live in NodeSemantics keyed by AST pointer. Moving them to their own
// table has to preserve three things exactly, or `no fmt` output drifts with
// no compile error anywhere:
//
//   1. SetRTFlag is OR-accumulate, not overwrite (several sites set different
//      bits on the same node, e.g. `RTStandalone | RTMatchWildcard`);
//   2. SetOpeningBraceComment(node, nil) clears the field of an EXISTING entry
//      but never creates one (decl.go relies on this after moving the comment
//      to def.Comment);
//   3. every accessor is nil-safe, because Program.FmtTrivia is nil for any
//      Program that did not come from ParseProgram (blob decode).
// ─────────────────────────────────────────────────────────────────────────────

func triviaTestNode() *Identifier {
	return &Identifier{Token: lexer.Token{Type: lexer.IDENT, Literal: "n"}}
}

// TestFmtTriviaSetRTFlagAccumulates pins the OR semantics: two different bits
// written to one node must both survive. A plain assignment would silently drop
// the first one.
func TestFmtTriviaSetRTFlagAccumulates(t *testing.T) {
	tr := NewFmtTrivia()
	n := triviaTestNode()
	tr.SetRTFlag(n, RTStandalone)
	tr.SetRTFlag(n, RTMatchWildcard)
	if !tr.HasRTFlag(n, RTStandalone) {
		t.Error("RTStandalone lost: SetRTFlag must OR, not overwrite")
	}
	if !tr.HasRTFlag(n, RTMatchWildcard) {
		t.Error("RTMatchWildcard not set")
	}
	if tr.HasRTFlag(n, RTElif) {
		t.Error("RTElif set although never written")
	}
}

// TestFmtTriviaSetOpeningBraceCommentNilDoesNotCreateEntry pins the subtle
// clear semantics. Creating an entry here would change the entry set, which
// would ripple into merging (Merge treats "key exists" as "don't overwrite").
func TestFmtTriviaSetOpeningBraceCommentNilDoesNotCreateEntry(t *testing.T) {
	tr := NewFmtTrivia()
	n := triviaTestNode()
	// Clearing a node that has no entry must not create one.
	tr.SetOpeningBraceComment(n, nil)
	if tr.Len() != 0 {
		t.Fatalf("SetOpeningBraceComment(nil) created an entry: Len=%d", tr.Len())
	}
	// Setting then clearing leaves the entry but empties the field.
	cg := &CommentGroup{List: []*Comment{{Text: "; hi"}}}
	tr.SetOpeningBraceComment(n, cg)
	if tr.Len() != 1 || tr.OpeningBraceCommentOf(n) != cg {
		t.Fatalf("OBC not recorded: Len=%d", tr.Len())
	}
	tr.SetOpeningBraceComment(n, nil)
	if tr.OpeningBraceCommentOf(n) != nil {
		t.Error("OBC not cleared")
	}
	// Re-setting must replace (overwrite, not accumulate).
	cg2 := &CommentGroup{List: []*Comment{{Text: "; bye"}}}
	tr.SetOpeningBraceComment(n, cg2)
	if tr.OpeningBraceCommentOf(n) != cg2 {
		t.Error("OBC not overwritten")
	}
}

// TestFmtTriviaNilSafety is the guard for blob-decoded Programs: FmtTrivia is
// deliberately not serialized, so a decoded Program has nil FmtTrivia and every
// accessor must degrade quietly instead of panicking.
func TestFmtTriviaNilSafety(t *testing.T) {
	var tr *FmtTrivia
	n := triviaTestNode()
	tr.SetRTFlag(n, RTStandalone) // must not panic
	tr.SetOpeningBraceComment(n, nil)
	if tr.HasRTFlag(n, RTStandalone) {
		t.Error("nil FmtTrivia must report no flags")
	}
	if tr.OpeningBraceCommentOf(n) != nil {
		t.Error("nil FmtTrivia must report no OBC")
	}
	if tr.Len() != 0 {
		t.Error("nil FmtTrivia Len must be 0")
	}
	if tr.Clone(nil) != nil {
		t.Error("nil Clone must return nil")
	}
	tr.Merge(NewFmtTrivia()) // must not panic

	// nil node on a live table must not create an entry either.
	live := NewFmtTrivia()
	live.SetRTFlag(nil, RTStandalone)
	live.SetOpeningBraceComment(nil, &CommentGroup{})
	if live.Len() != 0 {
		t.Errorf("nil node created an entry: Len=%d", live.Len())
	}
}

// TestParsePopulatesFmtTrivia is the anti-vacuity half: a trivia table that is
// always empty would make every other assertion here meaningless. It parses a
// slice of the shipped corpus and requires the table to actually be filled.
func TestParsePopulatesFmtTrivia(t *testing.T) {
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
		p.SkipLowering = true // the fmt/LSP shape
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			continue
		}
		checked++
		if prog.FmtTrivia != nil && prog.FmtTrivia.Len() > 0 {
			nonEmpty++
		}
	}
	if checked < 20 {
		t.Fatalf("anti-vacuity: only %d corpus files parsed cleanly, want >= 20", checked)
	}
	if nonEmpty == 0 {
		t.Fatalf("anti-vacuity: FmtTrivia empty for all %d parsed corpus files; "+
			"the table is not being filled, so every trivia assertion is vacuous", checked)
	}
	t.Logf("FmtTrivia populated for %d/%d parsed corpus files", nonEmpty, checked)
}

// TestFmtTriviaWritesDoNotTouchSemanticTables is the separation assertion for
// the trivia side: writing FmtTrivia must leave all three semantic tables alone.
//
// ⚠️ 交叉檢查必須在 **Program 級**做：`FmtTrivia` 掛在 `Program.FmtTrivia` 而
// 不是 `SemanticContext`，所以拿一個獨立 `NewSemanticContext()` 來比對是空斷言
// （寫 tr 不可能動那個 sem）。故這裡用真實解析出來的 program（`ParseProgram`
// 會掛 FmtTrivia），對一個**不在三張語義表裡**的合成節點寫 trivia。
func TestFmtTriviaWritesDoNotTouchSemanticTables(t *testing.T) {
	p := New(lexer.NewCached("trivia-sep.no", "x = 1 > 0 -> 42\n"))
	p.Filename = "trivia-sep.no"
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	// Anti-vacuity: the table must really be populated, otherwise "unchanged"
	// below proves nothing.
	if prog.FmtTrivia == nil || prog.FmtTrivia.Len() == 0 {
		t.Fatal("anti-vacuity: FmtTrivia empty; the assertion below would be vacuous")
	}
	before := semTableLens(prog.Sem)
	triviaBefore := prog.FmtTrivia.Len()

	n := Node(&IntegerLiteral{Value: 7})
	prog.FmtTrivia.SetRTFlag(n, RTStandalone)

	if after := semTableLens(prog.Sem); after != before {
		t.Errorf("writing FmtTrivia changed the semantic tables: before=%v after=%v",
			before, after)
	}
	if got := prog.FmtTrivia.Len(); got != triviaBefore+1 {
		t.Errorf("FmtTrivia.Len() = %d, want %d (entry was not created)",
			got, triviaBefore+1)
	}
}
