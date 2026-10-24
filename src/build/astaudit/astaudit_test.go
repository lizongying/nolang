package astaudit

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func parseSrc(t *testing.T, src string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func firstStmt(t *testing.T, prog *parser.Program, want string) parser.Statement {
	t.Helper()
	if len(prog.Statements) == 0 {
		t.Fatalf("program has no statements")
	}
	s := prog.Statements[0]
	if got := fmt.Sprintf("%T", s); got != want {
		t.Fatalf("first statement type = %s, want %s", got, want)
	}
	return s
}

// TestSnapshotStableAcrossIdent snapshots the same tree twice; the gate must
// report no writes (no false positives from map iteration order, pointer
// rendering, or the walk itself).
func TestSnapshotStableAcrossIdenticalCaptures(t *testing.T) {
	prog := parseSrc(t, `x i64 = 1
f = (a i64) (r i64) {
  r = a + x
}
`)
	a, b := Capture(prog), Capture(prog)
	d := Diff(a, b)
	if !d.Empty() {
		t.Fatalf("identical captures differ:\n%s", d.Summary())
	}
}

// TestCaptureDetectsSyntheticWrite is the positive case: a deliberately
// introduced field write MUST be reported. Without it, an "all clean" census
// proves nothing (the walker could simply be blind).
func TestCaptureDetectsSyntheticWrite(t *testing.T) {
	prog := parseSrc(t, `x i64 = 1
`)
	before := Capture(prog)

	ls := firstStmt(t, prog, "*parser.LetStatement").(*parser.LetStatement)
	ls.Name.Value = "renamed"
	ls.ItArmType = "ok"

	after := Capture(prog)
	d := Diff(before, after)
	if d.Empty() {
		t.Fatal("synthetic write not detected: the gate is blind")
	}
	got := map[string]bool{}
	for _, w := range d.Writes {
		got[w.Type+"."+w.Field] = true
	}
	if !got["*parser.Identifier.Value"] {
		t.Errorf("missing write on *parser.Identifier.Value, got %v", got)
	}
	if !got["*parser.LetStatement.ItArmType"] {
		t.Errorf("missing write on *parser.LetStatement.ItArmType, got %v", got)
	}
}

// TestCaptureSeesNestedStatements guards the recursion into function bodies:
// a write two levels deep must still be localised.
func TestCaptureSeesNestedStatements(t *testing.T) {
	prog := parseSrc(t, `f = (a i64) (r i64) {
  y i64 = a
  r = y
}
`)
	before := Capture(prog)

	fd := firstStmt(t, prog, "*parser.FunctionDefinition").(*parser.FunctionDefinition)
	inner := fd.Body.Statements[0].(*parser.LetStatement)
	inner.Name.Value = "shadowed"

	after := Capture(prog)
	d := Diff(before, after)
	if d.Empty() {
		t.Fatal("nested write not detected")
	}
	if len(d.Writes) != 1 || d.Writes[0].Field != "Value" {
		t.Fatalf("unexpected writes: %s", d.Summary())
	}
	if d.Writes[0].Type != "*parser.Identifier" {
		t.Fatalf("write attributed to %s, want *parser.Identifier", d.Writes[0].Type)
	}
}

// TestCaptureTreatsAppendedNodeAsAssembly verifies that adding a brand-new
// statement is reported as a node addition, not as a field write. Assembly
// (transpiler_inject.go) legitimately synthesises nodes; §1.4 forbids writing
// *existing* node fields.
func TestCaptureTreatsAppendedNodeAsAssembly(t *testing.T) {
	prog := parseSrc(t, `x i64 = 1
`)
	before := Capture(prog)
	prog.Statements = append(prog.Statements, &parser.ExpressionStatement{})
	after := Capture(prog)

	d := Diff(before, after)
	for _, w := range d.Writes {
		t.Errorf("unexpected field write: %s.%s", w.Type, w.Field)
	}
	if len(d.AddedNodes) == 0 {
		t.Fatalf("appended statement not reported as an added node: %s", d.Summary())
	}
}

// TestCaptureSliceMembershipIsIndexStable checks that removing a statement
// shifts nothing: it must surface as one statement-slice delta on the Program
// node, not as dozens of phantom index writes.
func TestCaptureSliceMembershipIsIndexStable(t *testing.T) {
	prog := parseSrc(t, `a i64 = 1
b i64 = 2
c i64 = 3
`)
	before := Capture(prog)
	prog.Statements = prog.Statements[1:]
	after := Capture(prog)

	d := Diff(before, after)
	var writes, slices int
	for _, w := range d.Writes {
		t.Errorf("unexpected field write: %s.%s", w.Type, w.Field)
	}
	writes = len(d.Writes)
	slices = len(d.Slices)
	if writes != 0 {
		t.Fatalf("got %d phantom field writes", writes)
	}
	if slices == 0 {
		t.Fatalf("statement removal not reported as a slice delta: %s", d.Summary())
	}
}

// TestCaptureIgnoresPermittedSideTables: Sem / Warnings / Errors / comment
// bookkeeping are explicitly allowed by §1.4, so the gate must not report
// them (otherwise every build would fail the pin).
func TestCaptureIgnoresPermittedSideTables(t *testing.T) {
	prog := parseSrc(t, `x i64 = 1
`)
	before := Capture(prog)
	prog.Warnings = append(prog.Warnings, "synthetic")
	prog.Errors = append(prog.Errors, "synthetic")
	prog.Sem.SetDeclared("synthetic")
	after := Capture(prog)

	d := Diff(before, after)
	if !d.Empty() {
		t.Fatalf("permitted side-table writes reported:\n%s", d.Summary())
	}
}

// TestCaptureCoversPromotedCommentedNodeFields: Doc/Comment live on the embedded
// CommentedNode. The gate must see them flattened (no extra path segment) or the
// §13.5 census would name the wrong field and a migration that reads
// "CommentedNode.X" would silently miss the real write site.
//
// History: this test originally used SourceFile/ModuleOwner, which were the
// promoted fields then being migrated. S5 moved both into Program.Sem, so the
// test now pins the flattening rule against the two fields that remain on the
// embed. If CommentedNode is ever emptied, keep a promoted-field witness here —
// dropping it silently disables coverage of the flattening branch in walkFields.
func TestCaptureCoversPromotedCommentedNodeFields(t *testing.T) {
	prog := parseSrc(t, `x i64 = 1
`)
	before := Capture(prog)

	ls := firstStmt(t, prog, "*parser.LetStatement").(*parser.LetStatement)
	ls.Doc = &parser.CommentGroup{}
	ls.Comment = &parser.CommentGroup{}

	after := Capture(prog)
	d := Diff(before, after)
	if len(d.Writes) != 2 {
		t.Fatalf("want 2 writes, got:\n%s", d.Summary())
	}
	keys := map[string]bool{}
	for _, w := range d.Writes {
		keys[w.Field] = true
	}
	if !keys["Doc"] || !keys["Comment"] {
		t.Fatalf("promoted fields not flattened, got %v", keys)
	}
}

// TestSnapshotPinsEveryCapturedNode: Diff matches records by uintptr, so every
// node behind a key must stay reachable for as long as the snapshot lives.
// Without the pin the GC can free a node and a later allocation reuses its
// address, which turns an unrelated node into phantom writes on the old record
// (and a "<type>" write when the types differ). This is structural: it asserts
// the pin covers *every* recorded node, not just the root.
func TestSnapshotPinsEveryCapturedNode(t *testing.T) {
	prog := parseSrc(t, `x i64 = 1
f = (a i64) (r i64) {
  y i64 = a + x
  r = y
}
`)
	s := Capture(prog)
	if len(s.keep) != len(s.nodes) {
		t.Fatalf("pinned %d values for %d recorded nodes — some node is unpinned", len(s.keep), len(s.nodes))
	}
	for _, kept := range s.keep {
		v := reflect.ValueOf(kept)
		if v.Kind() != reflect.Ptr || v.IsNil() {
			t.Fatalf("pinned value %T is not a node pointer", kept)
		}
		if _, ok := s.nodes[v.Pointer()]; !ok {
			t.Fatalf("pinned %T@%d has no record — keys and pins disagree", kept, v.Pointer())
		}
	}
}
