package mir

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// These tests pin the SECOND encoding of OpMove.
//
//	b.Emit(OpMove, typ, [src])   // Dst = the fresh destination value
//	b.EmitMoveInto(dst, src)     // Dst = NoVal, Args = [src, dst]
//
// The second is what an ASSIGNMENT to an already-bound variable lowers to, and
// it is the common shape in real code. Any analysis that resolves the
// destination as `inst.Dst` alone therefore silently sees half the moves. The
// analysis-side fix is the shared moveSrc/moveDst pair in analysis.go; these
// tests exist so the blind spot cannot come back unnoticed.
//
// WHY THE ASSERTION IS ON THE MIR DROP COUNT, NOT ON STDOUT
// ---------------------------------------------------------
// The bug this pins was a DOUBLE FREE that produced completely correct program
// output: two owners of one payload, both dropped, and the second free of a
// small string does not abort on this platform. tests/tagged-enum-zero-match.no
// cannot catch it either — its own header says so ("標得不夠 ... stdout 看不出來,
// 要靠 MIR 的 drop 計數另外驗"). So the observable is the number of drops of
// enum-typed values, which is exactly the quantity the pass computes.

// lowerForTest parses and lowers a Nolang source string, returning the module.
func lowerForTest(t *testing.T, src string) *Module {
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
	mod, _, diags := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatalf("LowerHIR returned nil module; diags=%v", diags)
	}
	return mod
}

// enumDrops counts OpDrop instructions in func `name` whose operand is an
// enum-typed value, plus the total drop count (for context on failure).
func enumDrops(t *testing.T, mod *Module, name string) (enum int, total int) {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpDrop || len(inst.Args) == 0 {
				continue
			}
			total++
			if ty := mod.valueTypeOf(f, inst.Args[0]); ty != nil && ty.Kind == KindEnum {
				enum++
			}
		}
	}
	return enum, total
}

// The assignment spelling: `t = q` is `move dst=0 args=[q t]`.
//
// R5 (markEnumPayloadOwners) marks an enum value owning when nothing in the
// function extracts it, and excludes move DESTINATIONS (an alias is not an
// owner — the source is). Registering the destination by reading inst.Dst alone
// missed `t`, so BOTH t and q were marked owning and each got a drop: one
// payload, two frees. Measured on the pre-fix compiler, main's drops were
// `drop 1` + `drop 3`; the fix leaves only `drop 3`.
func TestEnumAssignmentMoveDropsPayloadOnce(t *testing.T) {
	mod := lowerForTest(t, `e-res {
    ok(v str),
    fail,
}

main = () {
    t e-res = fail
    q e-res = ok('hi')
    t = q
    print('assign')
}
`)
	enum, total := enumDrops(t, mod, "main")
	if enum != 1 {
		t.Fatalf("assignment-shaped enum move: expected exactly 1 drop of an enum-typed value "+
			"(one payload, one owner), got %d (total drops=%d)\n%s",
			enum, total, mod.String())
	}
}

// The fresh-binding spelling: `t e-res = q` is `move dst=t args=[q]`.
//
// This is the encoding the pre-fix code DID read, so it was never broken — the
// test is here to pin that both spellings of the same program agree, which is
// the property the shared helper exists to guarantee.
func TestEnumFreshBindingMoveDropsPayloadOnce(t *testing.T) {
	mod := lowerForTest(t, `e-res {
    ok(v str),
    fail,
}

main = () {
    q e-res = ok('hi')
    t e-res = q
    print('fresh')
}
`)
	enum, total := enumDrops(t, mod, "main")
	if enum != 1 {
		t.Fatalf("fresh-binding enum move: expected exactly 1 drop of an enum-typed value, "+
			"got %d (total drops=%d)\n%s", enum, total, mod.String())
	}
}

// The two spellings of the same program must lower to the same ownership
// decision. Before the fix they did not: the assignment form dropped the shared
// payload twice, the fresh form once. Asserting equality (rather than a magic
// number) is what makes this test state the actual invariant.
func TestEnumMoveSpellingsAgreeOnDropCount(t *testing.T) {
	assign := lowerForTest(t, `e-res {
    ok(v str),
    fail,
}

main = () {
    t e-res = fail
    q e-res = ok('hi')
    t = q
    print('assign')
}
`)
	fresh := lowerForTest(t, `e-res {
    ok(v str),
    fail,
}

main = () {
    q e-res = ok('hi')
    t e-res = q
    print('fresh')
}
`)
	ea, ta := enumDrops(t, assign, "main")
	ef, tf := enumDrops(t, fresh, "main")
	if ea != ef {
		t.Fatalf("the two OpMove encodings disagree on how many times the shared payload is "+
			"dropped: assignment form=%d (total %d), fresh-binding form=%d (total %d) — "+
			"an analysis is reading only one of the two encodings", ea, ta, ef, tf)
	}
}
