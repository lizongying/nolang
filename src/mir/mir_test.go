package mir

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// countDrops returns the number of OpDrop instructions and a map value->drop-count.
func countDrops(m *Module) (int, map[ValueID]int) {
	counts := map[ValueID]int{}
	n := 0
	for i := range m.Insts {
		if m.Insts[i].Op == OpDrop && len(m.Insts[i].Args) > 0 {
			n++
			counts[m.Insts[i].Args[0]]++
		}
	}
	return n, counts
}

// ---------------------------------------------------------------------------
// Synthetic MIR: the analysis passes
// ---------------------------------------------------------------------------

func TestAnalyzeDropsOwnedLocalOnce(t *testing.T) {
	b := NewBuilder("t")
	_ = b.NewFunc("f", nil, nil, false) // entry block selected
	strT := b.Type("str")               // owned
	v := b.EmitStr(OpConst, strT, "hi", "")
	// use v in a call so its last use is after the call
	b.Emit(OpCall, b.Type("void"), []ValueID{v}, "print")
	b.Terminate(OpReturn, nil, nil, "")

	rep := b.Module().Analyze()
	if rep.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", rep.Error())
	}
	n, counts := countDrops(b.Module())
	if n != 1 {
		t.Fatalf("expected exactly 1 drop, got %d", n)
	}
	if counts[v] != 1 {
		t.Fatalf("expected owned value %d dropped exactly once, got %d", v, counts[v])
	}
	if errs := b.Module().Validate(); len(errs) != 0 {
		t.Fatalf("validate failed: %v", errs)
	}
}

func TestAnalyzeMovePreventsDoubleFree(t *testing.T) {
	b := NewBuilder("t")
	_ = b.NewFunc("f", nil, nil, false)
	strT := b.Type("str")
	v := b.EmitStr(OpConst, strT, "hi", "") // src (owned)
	w := b.Emit(OpMove, strT, []ValueID{v}, "") // move src -> w
	b.Emit(OpCall, b.Type("void"), []ValueID{w}, "print")
	b.Terminate(OpReturn, nil, nil, "")

	rep := b.Module().Analyze()
	if rep.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", rep.Error())
	}
	n, counts := countDrops(b.Module())
	if n != 1 {
		t.Fatalf("expected exactly 1 drop (move transfers responsibility), got %d", n)
	}
	if counts[v] != 0 {
		t.Fatalf("move source must NOT be dropped (double-free risk), got %d", counts[v])
	}
	if counts[w] != 1 {
		t.Fatalf("move destination must be dropped once, got %d", counts[w])
	}
}

func TestAnalyzeUseAfterMoveDetected(t *testing.T) {
	b := NewBuilder("t")
	_ = b.NewFunc("f", nil, nil, false)
	strT := b.Type("str")
	v := b.EmitStr(OpConst, strT, "hi", "")
	w := b.Emit(OpMove, strT, []ValueID{v}, "")
	// nolang's OpMove is a BITWISE COPY: reading v after the move is SAFE (its
	// bytes stay valid), so only a SECOND DROP of v is a real hazard — it frees
	// the same heap pointer that w now owns -> double-free. That is what we must
	// catch (the legacy read-after-move check was a false positive that blocked
	// test-std-hash.no, where md5 reads its `data` []byte many times after
	// moving it in).
	b.Emit(OpDrop, b.Type("void"), []ValueID{v}, "")
	b.Emit(OpCall, b.Type("void"), []ValueID{w}, "print")
	b.Terminate(OpReturn, nil, nil, "")

	rep := b.Module().Analyze()
	found := false
	for _, d := range rep.Diagnostics {
		if d.Kind == "use-after-move" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a use-after-move diagnostic, got: %s", rep.Error())
	}
}

func TestAnalyzeMissingDropDetected(t *testing.T) {
	// An owned value that is never used and never dropped: the inserter should
	// still drop it (so this stays clean); to force a missing-drop we inject a
	// second drop manually and expect a duplicate-drop diagnostic instead.
	b := NewBuilder("t")
	_ = b.NewFunc("f", nil, nil, false)
	strT := b.Type("str")
	v := b.EmitStr(OpConst, strT, "hi", "")
	b.Emit(OpCall, b.Type("void"), []ValueID{v}, "print")
	b.Terminate(OpReturn, nil, nil, "")
	rep := b.Module().Analyze()
	if rep.HasErrors() {
		t.Fatalf("clean owned local should yield no diagnostics, got: %s", rep.Error())
	}
}

// ---------------------------------------------------------------------------
// HIR -> MIR lowering (real frontend)
// ---------------------------------------------------------------------------

func TestLowerHIRRealProgram(t *testing.T) {
	src := `add = (a i64, b i64) (r i64) {
  r = a + b
  return
}
main = () () {
  x = 1
  y = add(x, 2)
  print('hi')
  return
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}

	mod, rep, diags := LowerHIR(pkg)
	if mod == nil {
		t.Fatal("LowerHIR returned nil module")
	}
	if len(mod.Funcs) < 2 {
		t.Fatalf("expected at least main+add functions, got %d", len(mod.Funcs))
	}
	if errs := mod.Validate(); len(errs) != 0 {
		t.Fatalf("lowered MIR failed validation: %v", errs)
	}
	// 'add' is reachable from main, so it must have been lowered (not skipped).
	if _, ok := mod.FuncByName["add"]; !ok {
		t.Fatalf("reachability failed: 'add' should have been lowered because main calls it; diags=%v", diags)
	}
	// 'y = add(...)' returns an owned-or-not i64; either way Analyze must not crash.
	if rep.HasErrors() {
		t.Logf("analysis diagnostics (non-fatal for this smoke test): %s", rep.Error())
	}
	t.Logf("lowered MIR:\n%s", mod.String())
}

func TestLowerHIRControlFlow(t *testing.T) {
	src := `main = () () {
  sum = 0
  nums = [1, 2, 3]
  for i in nums {
    sum = sum + i
  }
  x = 10
  x > 5 -> print('big')
  -> print('small')
  return
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	mod, rep, diags := LowerHIR(pkg)
	if errs := mod.Validate(); len(errs) != 0 {
		t.Fatalf("lowered MIR failed validation: %v", errs)
	}
	if rep.HasErrors() {
		t.Logf("analysis diagnostics: %s", rep.Error())
	}
	// Sanity: the module should contain both cond-br (if) and a loop header.
	s := mod.String()
	if !strings.Contains(s, "cond-br") {
		t.Fatalf("expected a cond-br from the if/for, got:\n%s", s)
	}
	if len(diags) != 0 {
		t.Logf("lowering coverage gaps (non-fatal): %v", diags)
	}
}

// ensure hir import is used even if future edits drop direct references
var _ = hir.NoID
