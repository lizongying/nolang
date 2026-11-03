package mir

import (
	"strings"
	"testing"
)

// These tests pin two lowerer/codegen defects that both made a container walk
// stop at its first element. They are unrelated in mechanism but identical in
// symptom, so they were long mistaken for one "index codegen" bug.

// ── 1. A byte-slice module constant must fold to a BYTE array ────────────────
//
// A slice-typed top-level constant that a function body references is
// registered as a module global whose declared type comes from
// fixedArrayConstGlobalType, and whose initializer comes from foldConstText.
// Both used to hardcode i64:
//
//	@G = private global [4 x i64] [i64 66, i64 90, ...]
//
// while every reader treated G as []byte and indexed it per BYTE. Element 0 was
// correct by accident (the low byte of the first i64) and everything after it
// read that i64's high zero bytes — so a 121-byte golden bzip2 stream looked
// like `66, 0, 0, 0, 0, 0, 0, 0, 90, ...` to the decompressor.
//
// test/std/bzip2.no carried a workaround (copy the global into a fresh local
// slice) that existed only because of this.
const byteSliceGlobalSrc = `G []byte = [66, 90, 104, 57]

probe = () (r i64) {
    #{index-out=zero}
    r = G[1]
}

x = probe()
print(x)
`

func TestByteSliceGlobalFoldsToByteArray(t *testing.T) {
	mod := lowerForTest(t, byteSliceGlobalSrc)
	var g *GlobalDecl
	for i := range mod.Globals {
		if mod.Globals[i].Name == "G" {
			g = &mod.Globals[i]
			break
		}
	}
	if g == nil {
		names := make([]string, 0, len(mod.Globals))
		for _, gg := range mod.Globals {
			names = append(names, gg.Name)
		}
		t.Fatalf("global G was not registered (globals: %v) — the probe no longer "+
			"exercises the fold path, so this test would pass vacuously", names)
	}
	// The initializer carries its own LLVM type (`[N x i8] c"..."`), and that
	// type IS the global's type — emitGlobals prints ConstText verbatim.
	if strings.Contains(g.ConstText, "x i64") {
		t.Errorf("global G folds to %q; a []byte constant must use 1 byte per element. "+
			"With [N x i64] only element 0 of the byte view is correct: data[1] reads the "+
			"first i64's high zero byte", g.ConstText)
	}
	if !strings.HasPrefix(g.ConstText, "[4 x i8]") {
		t.Errorf("global G folds to %q, want an `[4 x i8]` constant", g.ConstText)
	}
}

// ── 2. `p = lf[p]` / `p ?= lf[p]` must read with p's OLD value ───────────────
//
// `#{index-out=zero}` on an assignment to an identifier is desugared by
// maybeIndexOutAssign into `__idx_out_N ?T = container[i]` + a `let v = DEF`
// pre-declaration + a match whose ok arm assigns `v = it`. The pre-declaration
// gives v a slot outside the match arms — but it also ZEROES v.
//
// That pre-declaration used to be emitted BEFORE the index read was evaluated.
// When v is the index itself — the self-index walk `p = lf[p]` used by every
// LF / next-chain traversal — the read saw the freshly zeroed p and always
// indexed element 0:
//
//	p = 0
//	p = lf[p]    ; reads lf[0] forever, p stuck at lf[0]
//
// The bzip2 inverse BWT hit exactly this and needed a `np = lf[p]; p = np`
// workaround; src/std/archive/bzip2.no now walks directly.
//
// THE FIX IS ORDERING, NOT "skip the pre-declaration when v is already
// declared": both arms always write v, so the zero is never observable — it
// only has to stop replying to its own question. Deciding "declared" by name
// lookup is unsound here anyway: FuncVarTypeRef / IsFuncDeclared fall back to
// the GLOBAL VarTypes / DeclaredVars maps, which are shared across modules, so
// short names like `a`, `b`, `f0` match some unrelated binding and the probe
// reads a slot that was never created (test/std/dns.no's parse-response dropped
// from 51/51 to 49/51 that way).
//
// THE ASSERTION IS ON INSTRUCTION ORDER, NOT ON STDOUT: the read is the same
// variable either way and the program still "runs" — it just traverses the
// wrong element.
//
// Two `=`-form probes cover the two ways v gets a type at lowering time: probeA
// from a literal-typed local, probeB from a call result (v's type is unknown,
// so the desugar cannot lean on it). probeC covers the `?=` form, a different
// lowering with the same ordering requirement.
const selfIndexAssignSrcA = `probeA = () (r i64) {
    lf []i64 = [1, 2, 3, 0]
    orig = 2
    p = orig
    k = 0
    (k < 3) {
        #{index-out=zero}
        p = lf[p]
        #{overflow=wrap}
        k = k + 1
    }
    r = p
}

x = probeA()
print(x)
`

const selfIndexAssignSrcB = `seed = () (n i64) {
    n = 2
}

probeB = () (r i64) {
    lf []i64 = [1, 2, 3, 0]
    p = seed()
    k = 0
    (k < 3) {
        #{index-out=zero}
        p = lf[p]
        #{overflow=wrap}
        k = k + 1
    }
    r = p
}

y = probeB()
print(y)
`

// probeC is the `?=` (UnwrapAssign) twin of probeA: same self-reference, but
// lowered by lowerUnwrapAssign instead of maybeIndexOutAssign. There the
// `v = <zero>` pre-declaration is unconditional, and the same rule applies:
// it must be emitted after `__unwrap_N = <RHS>`. Verified discriminating by
// moving it back before tmpAssign: p reads lf[0] and prints 1 instead of 3.
const selfIndexUnwrapSrcC = `probeC = () (r ?i64) {
    lf []i64 = [1, 2, 3, 0]
    p = 2
    p ?= lf[p]
    r = p
}

dump = (v ?i64) {
    v: {
        ok -> print(it)

        nil -> print('nil')
        err -> print('err')
    }
}

dump(probeC())
`

func TestSelfIndexAssignReadsBeforeDefaulting(t *testing.T) {
	assertNoZeroingBeforeSelfIndex(t, "probeA", selfIndexAssignSrcA)
	assertNoZeroingBeforeSelfIndex(t, "probeB", selfIndexAssignSrcB)
	assertNoZeroingBeforeSelfIndex(t, "probeC", selfIndexUnwrapSrcC)
}

// assertNoZeroingBeforeSelfIndex lowers src and fails when p's value is written
// with a constant zero anywhere before the `lf[p]` index read that consumes it.
func assertNoZeroingBeforeSelfIndex(t *testing.T, fn, src string) {
	t.Helper()
	mod := lowerForTest(t, src)
	fid, ok := mod.FuncByName[fn]
	if !ok {
		t.Fatalf("%s was not lowered — the probe shape changed and this test would "+
			"pass vacuously", fn)
	}
	f := mod.Func(fid)

	// Flat instruction order (block order, then per-block order). MIR has no
	// phi, so this is the order the emitters walk.
	var order []InstID
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		order = append(order, blk.Insts...)
	}

	// The first OpIndex is `lf[p]`; its index operand is p's MIR value.
	pVal := NoVal
	idxPos := -1
	for pos, iid := range order {
		inst := mod.Inst(iid)
		if inst == nil || inst.Op != OpIndex || len(inst.Args) != 2 {
			continue
		}
		pVal = inst.Args[1]
		idxPos = pos
		break
	}
	if idxPos < 0 {
		t.Fatalf("no OpIndex in %s — the probe shape changed and this test would "+
			"pass vacuously", fn)
	}

	// A value is "constant zero" when the instruction that defines it is an
	// OpConst with Int == 0.
	constZero := func(v ValueID) bool {
		for _, iid := range order {
			inst := mod.Inst(iid)
			if inst == nil || definedValue(inst) != v {
				continue
			}
			return inst.Op == OpConst && inst.Int == 0
		}
		return false
	}

	for pos, iid := range order {
		inst := mod.Inst(iid)
		if inst == nil || inst.Op != OpMove || len(inst.Args) < 2 || inst.Args[1] != pVal {
			continue
		}
		if !constZero(inst.Args[0]) {
			continue
		}
		if pos < idxPos {
			t.Fatalf("%s: p is zeroed (move of a const 0 at position %d) BEFORE the "+
				"index read (position %d); the read therefore always sees p == 0 and a "+
				"self-index walk `p = lf[p]` never advances", fn, pos, idxPos)
		}
	}
}
