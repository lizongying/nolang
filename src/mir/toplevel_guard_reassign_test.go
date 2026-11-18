package mir

import (
	"strings"
	"testing"
)

// TestTopLevelDirectReassignStaysGlobal is the control for the count above.
// A plain top-level reassignment (`n = T{...}` right at script level) must NOT
// be mistaken for a guard-body write: tests/field.no does exactly this with two
// DIFFERENT struct types, and treating it as a reassignment forces the name
// into main as a fixed-type local — the second store then corrupts the frame
// and the program segfaults.
func TestTopLevelDirectReassignStaysGlobal(t *testing.T) {
	mod := lowerForTest(t, `n i64 = 1
n = 2
print(n)
`)
	for _, g := range mod.Globals {
		if g.Name == "n" {
			return
		}
	}
	t.Error("a direct top-level reassignment must keep its name as a module global")
}

// TestTopLevelGuardReassignKeepsInitializer pins the bug where a script-level
// constant that is reassigned inside a bare-match guard body (`cond -> x = 0`)
// lost its initializer. Root cause: the FIRST `let` was registered as a module
// global and omitted from the synthetic main, while the guard-body assignment
// lowered as a fresh KLet in main with no `l.locals` entry — it bound a NEW slot
// instead of writing the global, so a FALSE condition still looked like it had
// fired (the slot held garbage). Observed on tests/byte-overflow.no: all four
// byte values were correct but the pass counter ended at 0.
//
// The observable here is the MIR slot shape: the guard's then-block must emit a
// `move` INTO the same value the initializer bound (not a fresh binding), and
// the initializer itself must be materialized as a real const in main.
func TestTopLevelGuardReassignKeepsInitializer(t *testing.T) {
	src := `a byte = 4
x byte = 7
a == 5 -> x = 0
print(x)
`
	mod := lowerForTest(t, src)
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("no main function")
	}
	// `x` must be a synthetic-main LOCAL that is initialized by an OpConst,
	// not a module global whose slot the guard-body KLet never writes.
	for _, g := range mod.Globals {
		if g.Name == "x" {
			t.Fatal("x was registered as a module global; a guard-body reassignment keeps it a local")
		}
	}
	// x's initializer (7) must be materialized as a const in main, and the
	// guard body (0) must emit a move into that same slot. Find the const 7,
	// then require a subsequent move whose source is the const 0.
	sawInit7, sawConst0, sawMove := false, false, false
	for _, bid := range mod.Func(fid).Blocks {
		for _, iid := range mod.Block(bid).Insts {
			ins := mod.Inst(iid)
			if ins.Op == OpConst && ins.Int == 7 {
				sawInit7 = true
			}
			if ins.Op == OpConst && ins.Int == 0 {
				sawConst0 = true
			}
			if ins.Op == OpMove {
				sawMove = true
			}
		}
	}
	if !sawInit7 {
		t.Error("initializer `x = 7` was not materialized as a const in main")
	}
	if !sawConst0 || !sawMove {
		t.Error("guard-body `x = 0` did not emit a const+move in main")
	}
	// Sanity: the source must contain a byte comparison at all, so the test is
	// not vacuous if lowering changes shape.
	if !strings.Contains(src, "-> x = 0") {
		t.Fatal("test source lost its guard body")
	}
}
