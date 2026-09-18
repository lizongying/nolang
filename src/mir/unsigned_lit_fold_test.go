package mir

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// constInts returns the set of integer constants materialized in the module.
func constInts(m *Module) map[int64]bool {
	out := map[int64]bool{}
	for i := range m.Insts {
		if m.Insts[i].Op == OpConst {
			out[m.Insts[i].Int] = true
		}
	}
	return out
}

// hasNegOfOne reports whether the module materializes -1 as `neg` of the
// constant 1 (the shape `-1` takes before any folding).
func hasNegOfOne(m *Module) bool {
	for i := range m.Insts {
		if m.Insts[i].Op != OpNeg {
			continue
		}
		for _, a := range m.Insts[i].Args {
			for j := range m.Insts {
				if m.Insts[j].Dst == a && m.Insts[j].Op == OpConst && m.Insts[j].Int == 1 {
					return true
				}
			}
		}
	}
	return false
}

func lowerSrc(t *testing.T, src string) *Module {
	t.Helper()
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
	mod, _, _ := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil module")
	}
	return mod
}

// TestUnsignedLitFold verifies that a negative integer literal assigned to a
// binding DECLARED with an unsigned integer type is folded into the target's
// value range at lowering time, so the value reads back unsigned:
//
//	b byte = -1   →  255           (before: stored as i64 -1, printed "-1")
//	c byte = 0  /  c = -1  →  255  (re-assignment keeps the declared type)
//	u u16  = -1   →  65535
//	w u32  = -2   →  4294967294
//
// MIR flattens every scalar integer to i64, so without the fold a byte-typed
// binding disagreed with the SAME literal written into a []byte element (which
// is truncated to i8 and printed through a zext → 255).
//
// The signed cases (`s i8 = -1`, `n i64 = -1`) must be left untouched — that
// is the reverse guard in this same test.
func TestUnsignedLitFold(t *testing.T) {
	src := `main = () () {
  b byte = -1
  c byte = 0
  c = -1
  u u16 = -1
  w u32 = -2
  s i8 = -1
  n i64 = -1
  print(b)
  print(c)
  print(u)
  print(w)
  print(s)
  print(n)
}
`
	mod := lowerSrc(t, src)
	consts := constInts(mod)
	t.Logf("const values: %v", consts)

	// Folded: the unsigned targets must carry their two's-complement value.
	for _, want := range []int64{255, 65535, 4294967294} {
		if !consts[want] {
			t.Errorf("expected folded constant %d in the lowered MIR", want)
		}
	}
	// Reverse guard: signed targets must keep -1 as-is (materialized either as
	// the constant -1 or as `neg` of the constant 1).
	if !consts[-1] && !hasNegOfOne(mod) {
		t.Errorf("expected -1 to survive for the signed (i8/i64) bindings")
	}
	// The in-range `c byte = 0` initializer must not be rewritten.
	if !consts[0] {
		t.Errorf("expected the in-range constant 0 to be preserved")
	}
}

// TestUnsignedLitFoldDoesNotLeak guards the fold's blast radius: with no
// unsigned binding in sight, a negative literal must reach codegen unchanged —
// both as a CALL ARGUMENT (the checker still rejects `set-u64(-1)`, so the
// value must not be silently rewritten) and as an untyped binding.
func TestUnsignedLitFoldDoesNotLeak(t *testing.T) {
	src := `set-u64 = (v u64) {}

main = () () {
  set-u64(-1)
  z = -1
  print(z)
}
`
	mod := lowerSrc(t, src)
	consts := constInts(mod)
	for _, folded := range []int64{255, 65535, 4294967294} {
		if consts[folded] {
			t.Errorf("negative literal was folded to %d with no unsigned target", folded)
		}
	}
	if !consts[-1] && !hasNegOfOne(mod) {
		t.Errorf("expected the negative literal -1 to reach codegen unchanged")
	}
}
