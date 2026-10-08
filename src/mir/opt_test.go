package mir

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tests for the in-compiler MIR optimiser (src/mir/opt.go).
//
// Two layers are covered, and both are needed:
//
//   - the pure helpers (the switch, the type gate, the arithmetic/comparison
//     evaluators), which is where the SEMANTICS live and where a wrong answer
//     would be invisible in an end-to-end test that happens not to exercise it;
//   - the pass end-to-end through LowerHIR, which is the only way to prove the
//     rules fire on real lowered IR (a pass that returns early everywhere also
//     passes every helper test).
//
// Every end-to-end case is paired with the SAME source lowered with the switch
// OFF. Without that control a green assertion proves nothing: the whole point is
// that the optimised module DIFFERS from the unoptimised one.
// ─────────────────────────────────────────────────────────────────────────────

// optFuncLive reports whether a Function is one codegen actually emits. Two
// kinds are not: the reserved index-0 slot of m.Funcs (FuncID 0, no name, no
// blocks — the same nil sentinel the instruction and value tables carry) and
// extern declarations, which have a signature but no body.
func optFuncLive(f *Function) bool {
	return f.ID != 0 && !f.IsExtern
}

// optInstCount is the total number of instructions codegen will emit.
func optInstCount(m *Module) int {
	n := 0
	for _, c := range optOpCounts(m) {
		n += c
	}
	return n
}

// optOpCounts counts how many instructions of each opcode the module's blocks
// hold. Only blocks a function lists are counted, i.e. exactly what codegen
// emits.
func optOpCounts(m *Module) map[Op]int {
	out := map[Op]int{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				if inst := m.Inst(iid); inst != nil {
					out[inst.Op]++
				}
			}
		}
	}
	return out
}

// optBlockCount counts the blocks codegen will emit (i.e. those still listed by
// their function).
func optBlockCount(m *Module) int {
	n := 0
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		n += len(f.Blocks)
	}
	return n
}

// optIntConsts returns the multiset of integer payloads carried by OpConst
// instructions. It is how a test asks "did the fold produce 20?" without
// depending on value ids.
func optIntConsts(m *Module) map[int64]int {
	out := map[int64]int{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst != nil && inst.Op == OpConst && inst.Flt == 0 {
					out[inst.Int]++
				}
			}
		}
	}
	return out
}

// lowerWithOpt lowers src through the real pipeline with the optimiser switch
// set to lvl ("" = off). Going through LowerHIR — rather than calling
// OptimizeMIR on an already-analysed module — is deliberate: it exercises the
// pass in its real position (before Analyze), which is the property the
// integration depends on.
func lowerWithOpt(t *testing.T, src, lvl string) *Module {
	t.Helper()
	t.Setenv("NOLANG_MIR_OPT", lvl)
	return lowerSrc(t, src)
}

// TestMIROptLevelSwitch pins the switch grammar. Anything unrecognised must be
// OFF: a typo silently enabling an optimisation would be a behaviour change
// nobody asked for, and it is the one failure mode a user cannot see.
func TestMIROptLevelSwitch(t *testing.T) {
	cases := []struct {
		val  string
		want int
	}{
		{"", OptOff},
		{"0", OptOff},
		{"off", OptOff},
		{"OFF", OptOff},
		{"false", OptOff},
		{"no", OptOff},
		{"none", OptOff},
		{"on", OptFold},
		{"ON", OptFold},
		{"true", OptFold},
		{"yes", OptFold},
		{"1", OptFold},
		{"2", OptCFG},
		{" 2 ", OptCFG},
		{"7", OptCFG},  // clamped, not rejected
		{"-1", OptOff}, // negative is off, never "very aggressive"
		{"tru", OptOff},
		{"enable", OptOff},
	}
	enabled := 0
	for _, c := range cases {
		t.Setenv("NOLANG_MIR_OPT", c.val)
		if got := MIROptLevel(); got != c.want {
			t.Errorf("NOLANG_MIR_OPT=%q: got level %d, want %d", c.val, got, c.want)
		}
		if c.want > OptOff {
			enabled++
		}
	}
	// Non-vacuity: the table must contain enabled cases, or "everything is off"
	// would pass it.
	if enabled == 0 {
		t.Fatal("switch table has no enabled case")
	}
}

// TestOptScalarLLVMRejectsWhatTheFolderCannotModel pins the type gate. Each
// rejection corresponds to a concrete way a fold would produce a WRONG value:
//
//   - i8/byte: emitConst has no i8 case and stores zeroinitializer, so an i8
//     OpConst materialises as 0 whatever inst.Int says;
//   - i128/u128: the payload lives in IntBig, which the folder does not write;
//   - owned/aggregate/option: not a number, and option arithmetic goes through
//     emitArith's overflow-wrapping path instead of a bare wrapping op.
func TestOptScalarLLVMRejectsWhatTheFolderCannotModel(t *testing.T) {
	m := NewModule("t")
	b := &Builder{Mod: m}
	accept := map[string]string{
		"i64":   "i64",
		"u64":   "i64",
		"i32":   "i64", // llvmTypeOf maps every non-byte int to i64
		"u16":   "i64",
		"char":  "i32",
		"bool":  "i1",
		"f64":   "double",
		"f32":   "double",
	}
	for raw, want := range accept {
		got, ok := optScalarLLVM(m.Type(b.Type(raw)))
		if !ok || got != want {
			t.Errorf("type %q: got (%q,%v), want (%q,true)", raw, got, ok, want)
		}
	}
	reject := []string{"byte", "u8", "i8", "i128", "u128", "str", "vec", "[]i64",
		"[i64]i64", "?i64", "[4]i64"}
	for _, raw := range reject {
		if got, ok := optScalarLLVM(m.Type(b.Type(raw))); ok {
			t.Errorf("type %q must not be foldable, got %q", raw, got)
		}
	}
	// Kind-based rejections cannot go through b.Type(raw): internType's
	// bare-identifier fallback turns any unrecognised raw name into KindInt, so
	// b.Type("void") is i64. `void` and friends only exist as kinds, and
	// hir2mir creates the real one with TypeExplicit("void", KindVoid, ...).
	kindReject := []struct {
		raw  string
		kind TypeKind
	}{
		{"void", KindVoid},
		{"somefn", KindFunc},
		{"someptr", KindPtr},
		{"somestruct", KindStruct},
		{"someenum", KindEnum},
		{"sometuple", KindArray},
		{"somemap", KindMap},
		{"", KindUnknown},
	}
	for _, c := range kindReject {
		id := b.TypeExplicit(c.raw, c.kind, false, NoType)
		if got, ok := optScalarLLVM(m.Type(id)); ok {
			t.Errorf("type %q (kind %v) must not be foldable, got %q", c.raw, c.kind, got)
		}
	}
	// An OWNED scalar-typed value is excluded even if its kind looks foldable —
	// deleting an owned value's definition would orphan its drop.
	owned := b.TypeExplicit("weird", KindInt, true, NoType)
	if _, ok := optScalarLLVM(m.Type(owned)); ok {
		t.Error("an owned value must never be foldable")
	}
}

// TestOptEvalIntBinary pins the arithmetic semantics, including the cases that
// must be REFUSED because the emitted instruction would be UB. Folding a UB
// case would replace a runtime trap (or an err option) with a compile-time
// constant — a semantic change, not an optimisation.
func TestOptEvalIntBinary(t *testing.T) {
	cases := []struct {
		name  string
		op    Op
		a, b  int64
		width int
		want  int64
		ok    bool
	}{
		{"add64", OpAdd, 2, 3, 64, 5, true},
		{"sub64", OpSub, 2, 3, 64, -1, true},
		{"mul64", OpMul, 1 << 40, 1 << 30, 64, 0, true}, // wraps: 2^70 -> 0
		{"add32wraps", OpAdd, 0x7fffffff, 1, 32, -0x80000000, true},
		{"mul32wraps", OpMul, 0x10000, 0x10000, 32, 0, true},
		{"div", OpDiv, 7, 2, 64, 3, true},
		{"mod", OpMod, 7, 2, 64, 1, true},
		{"div-by-zero", OpDiv, 7, 0, 64, 0, false},
		{"mod-by-zero", OpMod, 7, 0, 64, 0, false},
		{"udiv-by-zero", OpUDiv, 7, 0, 64, 0, false},
		{"intmin-over-neg1", OpDiv, math.MinInt64, -1, 64, 0, false},
		{"intmin32-over-neg1", OpDiv, math.MinInt32, -1, 32, 0, false},
		{"udiv-unsigned", OpUDiv, -1, 2, 64, 0x7fffffffffffffff, true},
		{"umod-unsigned", OpUMod, -1, 2, 64, 1, true},
		{"bitand", OpBitAnd, 0b1100, 0b1010, 64, 0b1000, true},
		{"bitor", OpBitOr, 0b1100, 0b1010, 64, 0b1110, true},
		{"xor", OpXor, 0b1100, 0b1010, 64, 0b0110, true},
		{"shl", OpShl, 1, 10, 64, 1024, true},
		{"shl-out-of-range", OpShl, 1, 64, 64, 0, false},
		{"shl-negative", OpShl, 1, -1, 64, 0, false},
		{"shr-is-logical", OpShr, -1, 1, 64, 0x7fffffffffffffff, true},
		{"shr-out-of-range", OpShr, 1, 64, 64, 0, false},
		{"and1", OpAnd, 1, 1, 1, 1, true},
		{"or1", OpOr, 0, 1, 1, 1, true},
		{"and1-refuses-wide", OpAnd, 1, 1, 64, 0, false},
	}
	okCount := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := optEvalIntBinary(c.op, c.a, c.b, c.width)
			if ok != c.ok {
				t.Fatalf("ok=%v want %v", ok, c.ok)
			}
			if ok && got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
		if c.ok {
			okCount++
		}
	}
	if okCount == 0 {
		t.Fatal("no positive case in the table")
	}
}

// TestOptEvalCmp pins the comparison semantics, including the two places where
// signed and unsigned (and ordered vs unordered) readings differ.
func TestOptEvalCmp(t *testing.T) {
	i64 := func(v int64) optConst { return optConst{llvm: "i64", i: v} }
	i32 := func(v int64) optConst { return optConst{llvm: "i32", i: v} }
	f64 := func(v float64) optConst { return optConst{llvm: "double", f: v} }
	nan := math.NaN()
	cases := []struct {
		name string
		op   Op
		a, b optConst
		want int64
		ok   bool
	}{
		{"eq", OpEq, i64(3), i64(3), 1, true},
		{"ne", OpNe, i64(3), i64(4), 1, true},
		{"lt", OpLt, i64(2), i64(3), 1, true},
		{"gt-false", OpGt, i64(2), i64(3), 0, true},
		{"signed-lt-negative", OpLt, i64(-1), i64(1), 1, true},
		// The same bit patterns under the UNSIGNED predicate: -1 is 2^64-1.
		{"unsigned-lt", OpULt, i64(-1), i64(1), 0, true},
		{"unsigned-gt", OpUGt, i64(-1), i64(1), 1, true},
		{"unsigned-le", OpULe, i64(1), i64(-1), 1, true},
		{"i32-signed", OpLt, i32(-1), i32(1), 1, true},
		{"float-eq", OpEq, f64(1.5), f64(1.5), 1, true},
		{"float-lt", OpLt, f64(1.5), f64(2.5), 1, true},
		// `!=` is the UNORDERED predicate (une), so NaN != NaN is TRUE, and
		// `==` is ordered (oeq), so NaN == NaN is false.
		{"float-nan-ne", OpNe, f64(nan), f64(nan), 1, true},
		{"float-nan-eq", OpEq, f64(nan), f64(nan), 0, true},
		// Unsigned float predicates do not exist in fcmp; emitCmp would emit an
		// invalid instruction, so there is nothing to fold.
		{"float-ult-refused", OpULt, f64(1), f64(2), 0, false},
	}
	pos := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := optEvalCmp(c.op, c.a, c.b)
			if ok != c.ok {
				t.Fatalf("ok=%v want %v", ok, c.ok)
			}
			if ok && got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
		if c.ok {
			pos++
		}
	}
	if pos == 0 {
		t.Fatal("no positive case in the table")
	}
}

// foldSrc is a program whose two arithmetic statements have all-constant
// operands and a wrapping (`#{overflow=wrap}`) destination, i.e. the exact shape
// the folder is allowed to rewrite: `(2+3)*4 == 20`.
const foldSrc = `main = () () {
    #{overflow=wrap}
    a = 2 + 3
    #{overflow=wrap}
    b = a * 4
    print(b)
}
`

// TestOptFoldsConstantArithmetic is the end-to-end fold, with the switch-off
// lowering as the control: the unoptimised module MUST still contain the add and
// the mul, or the "no add after folding" assertion below would pass on a program
// that never had one.
func TestOptFoldsConstantArithmetic(t *testing.T) {
	off := lowerWithOpt(t, foldSrc, "")
	offOps := optOpCounts(off)
	if offOps[OpAdd] == 0 || offOps[OpMul] == 0 {
		t.Fatalf("control program has no add/mul to fold: %v", offOps)
	}
	if optIntConsts(off)[20] != 0 {
		t.Fatalf("control already holds the folded value 20: %v", optIntConsts(off))
	}

	on := lowerWithOpt(t, foldSrc, "1")
	onOps := optOpCounts(on)
	if onOps[OpAdd] != 0 {
		t.Errorf("add survived folding: %v", onOps)
	}
	if onOps[OpMul] != 0 {
		t.Errorf("mul survived folding: %v", onOps)
	}
	if optIntConsts(on)[20] == 0 {
		t.Errorf("the folded result 20 was not materialised: %v", optIntConsts(on))
	}
}

// TestOptRemovesDeadConstants checks the second half of level 1: once the
// arithmetic is folded, the operand constants have no readers left and must be
// dropped. The control (switch off) is what makes the count meaningful — with
// folding disabled the operands are genuinely live.
func TestOptRemovesDeadConstants(t *testing.T) {
	off := lowerWithOpt(t, foldSrc, "")
	on := lowerWithOpt(t, foldSrc, "1")
	if off == nil || on == nil {
		t.Fatal("lowering failed")
	}
	offConsts := optOpCounts(off)[OpConst]
	onConsts := optOpCounts(on)[OpConst]
	if offConsts < 3 {
		t.Fatalf("control has only %d constants; nothing to eliminate", offConsts)
	}
	if onConsts >= offConsts {
		t.Errorf("dead constants not removed: off=%d on=%d", offConsts, onConsts)
	}
	// Exactly one constant should remain: the folded result feeding the call.
	if onConsts != 1 {
		t.Errorf("expected exactly 1 constant after folding+DCE, got %d", onConsts)
	}
}

// optArithSrc produces an OPTION-typed add from constant operands:
//
//	f = () (y ?i64) { y = 2 + 3 }
//
// With no `#{overflow=wrap}` annotation the result of `+` is `?i64`, so
// emitArith takes its overflow-wrapping path (wrapResult: an overflow intrinsic
// plus a tag store) instead of a bare `add`. The folder must refuse it: folding
// would turn "an err option on overflow" into a plain constant.
const optArithSrc = `f = () (y ?i64) {
    y = 2 + 3
}
main = () () {
    r = f()
    r: { ok(v) -> print(v) err(e) -> print('err') nil -> print('nil') }
}
`

// TestOptLeavesOptionArithmeticAlone is the load-bearing SAFETY test. If the
// folder ever starts rewriting option-typed arithmetic, this fails — and the
// symptom in a real program would be silent (an overflow that used to produce
// `err` would produce a wrapped number instead).
func TestOptLeavesOptionArithmeticAlone(t *testing.T) {
	off := lowerWithOpt(t, optArithSrc, "")
	offOps := optOpCounts(off)
	if offOps[OpAdd] == 0 {
		t.Fatalf("control has no option-typed add to protect: %v", offOps)
	}
	on := lowerWithOpt(t, optArithSrc, "2")
	onOps := optOpCounts(on)
	if onOps[OpAdd] != offOps[OpAdd] {
		t.Errorf("option-typed add was folded away: off=%d on=%d", offOps[OpAdd], onOps[OpAdd])
	}
	if optIntConsts(on)[5] != 0 {
		t.Errorf("the folder materialised a constant for option arithmetic: %v", optIntConsts(on))
	}
}

// optLoopSrc is the range-loop shape hir2mir lowers with empty pass-through
// blocks and constant range bounds — i.e. the shape level 2 is for.
const optLoopSrc = `main = () () {
    total = 0
    i <- [0..3) {
        #{overflow=wrap}
        total = total + i
    }
    print(total)
}
`

// TestOptLevelTwoShrinksTheCFG checks that level 2 removes blocks the level-1
// pass leaves behind, and that level 1 alone does not (the pair is what proves
// the CFG phase is what did it, rather than some pre-existing difference).
func TestOptLevelTwoShrinksTheCFG(t *testing.T) {
	l0 := lowerWithOpt(t, optLoopSrc, "0")
	l1 := lowerWithOpt(t, optLoopSrc, "1")
	l2 := lowerWithOpt(t, optLoopSrc, "2")

	b0, b1, b2 := optBlockCount(l0), optBlockCount(l1), optBlockCount(l2)
	if b2 >= b1 {
		t.Errorf("level 2 did not shrink the CFG: level1=%d blocks, level2=%d", b1, b2)
	}
	if b2 < 1 {
		t.Errorf("level 2 removed every block: %d", b2)
	}
	// Shrinking the CFG is level 2's job alone. If level 1 also changed the
	// block count the b2<b1 comparison above would no longer isolate the CFG
	// phase as the cause.
	if b1 != b0 {
		t.Errorf("level 1 changed the block count: off=%d level1=%d", b0, b1)
	}

	// The folding half must still be present at level 2: level 2 is a superset
	// of level 1, so the instruction count has to keep dropping. (Asserting
	// "no OpAdd survives" would be wrong — the loop's `total = total + i` has
	// non-constant operands and is legitimately unfolded at every level.)
	n0, n1, n2 := optInstCount(l0), optInstCount(l1), optInstCount(l2)
	if !(n2 < n1 && n1 < n0) {
		t.Errorf("folding did not compound across levels: off=%d level1=%d level2=%d", n0, n1, n2)
	}
	// The pass only ever rewrites instructions in place or deletes them, so it
	// must never introduce an opcode KIND that was not already there at level 0.
	// (Per-opcode counts legitimately RISE for OpConst: that is what folding
	// produces. Only the appearance of a brand-new opcode would be a bug.)
	c0, c2 := optOpCounts(l0), optOpCounts(l2)
	for op := range c2 {
		if c0[op] == 0 {
			t.Errorf("level 2 introduced opcode %v that level 0 never had", op)
		}
	}
}

// TestOptKeepsEveryBlockReachableFromEntry guards the one structural invariant
// level 2 could break: codegen emits f.Blocks[0] as the function's entry label
// (`name.bb0`) and derives every other label from the block's INDEX, so the
// first block must survive and every surviving block must still be reachable.
func TestOptKeepsEveryBlockReachableFromEntry(t *testing.T) {
	m := lowerWithOpt(t, optLoopSrc, "2")
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		if len(f.Blocks) == 0 {
			t.Fatalf("func %s lost every block", f.Name)
		}
		if f.Blocks[0] != f.Entry {
			t.Errorf("func %s: entry moved (Blocks[0]=%d Entry=%d)", f.Name, f.Blocks[0], f.Entry)
		}
		seen := map[BlockID]bool{}
		var stack []BlockID
		stack = append(stack, f.Entry)
		for len(stack) > 0 {
			b := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[b] {
				continue
			}
			seen[b] = true
			if blk := m.Block(b); blk != nil && blk.Term != nil {
				for _, tgt := range blk.Term.Targets {
					stack = append(stack, tgt)
				}
			}
		}
		for _, bid := range f.Blocks {
			if !seen[bid] {
				t.Errorf("func %s: block %d survived but is unreachable", f.Name, bid)
			}
		}
	}
}

// TestOptIsOffByDefault is the "no behaviour change unless asked for" claim in
// executable form: with the variable unset the lowered module must be identical
// (opcode by opcode) to one lowered with an explicit "0".
func TestOptIsOffByDefault(t *testing.T) {
	unset := lowerWithOpt(t, foldSrc, "")
	zero := lowerWithOpt(t, foldSrc, "0")
	a, b := optOpCounts(unset), optOpCounts(zero)
	if len(a) != len(b) {
		t.Fatalf("distinct opcode counts differ: %v vs %v", a, b)
	}
	for op, n := range a {
		if b[op] != n {
			t.Fatalf("opcode %s: unset=%d explicit-0=%d", op, n, b[op])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Regression tests for the block-removal hazard.
//
// Removing an unreachable block is only safe if the block was not the sole
// PRODUCER of a value somebody else still needs. hir2mir does not always
// materialise a value in the block that reads it, so a block can be
// control-flow-dead yet still hold the only definition of a live value. The
// first implementation removed it anyway and broke ten corpus files with
// `EmitLLVM: move slot` / `value N ... has no storage slot`.
//
// Both directions are tested: the fixtures must still contain an unreachable
// block (proving the guard is doing something, i.e. the test is not vacuous),
// and the verifier must report no violation (proving the result is sound). The
// verifier itself is tested against a deliberately broken module, because
// "no violations found" is worthless if the checker cannot find any.
// ─────────────────────────────────────────────────────────────────────────────

// optSoleProducerSrc: the arm not taken still holds the only `const` for a
// string the taken arm clones. This is tests/minimal-ok.no reduced.
const optSoleProducerSrc = `test-ok = () {
    n ?i64 = 200
    r = n: {
        ok(it > 127) -> 'too-big'

        -> 'in-range'
    }
    print(r)
}
test-ok()
`

// optMoveIntoSrc: the untaken arm holds the only `const` for the result
// variable, while the other two arms merely move INTO that variable. Because
// definedValue reports a move-into destination as a definition, counting those
// as producers is what made the guard let the block go. This is
// tests/simple-match.no reduced.
const optMoveIntoSrc = `test1 = () {
    n = 42
    r = n: {
        err -> 'err'

        -> ''
    }
    print(r)
}
test1()
`

// optUnreachableRetained counts blocks that are listed by a live function but
// cannot be reached from its entry — i.e. the blocks the guard refused to
// delete.
func optUnreachableRetained(m *Module) int {
	n := 0
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		seen := map[BlockID]bool{}
		var stack []BlockID
		if f.Entry > NoBlock {
			stack = append(stack, f.Entry)
		}
		for len(stack) > 0 {
			b := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[b] {
				continue
			}
			seen[b] = true
			if blk := m.Block(b); blk != nil && blk.Term != nil {
				stack = append(stack, blk.Term.Targets...)
			}
		}
		for _, bid := range f.Blocks {
			if !seen[bid] {
				n++
			}
		}
	}
	return n
}

func TestOptKeepsTheSoleProducerOfALiveValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"const-used-by-a-clone-in-another-block", optSoleProducerSrc},
		{"const-re-targeted-by-move-into-elsewhere", optMoveIntoSrc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			off := lowerWithOpt(t, tc.src, "0")
			if d := off.optVerifyOperands(); len(d) != 0 {
				t.Fatalf("the fixture is already broken with the pass OFF: %v", d)
			}
			on := lowerWithOpt(t, tc.src, "2")
			if d := on.optVerifyOperands(); len(d) != 0 {
				t.Errorf("level 2 left codegen-dependent values without a producer:\n  %s",
					strings.Join(d, "\n  "))
			}
			// Non-vacuity: the guard must actually have kept something, or this
			// test would pass even with the guard deleted.
			if n := optUnreachableRetained(on); n == 0 {
				t.Error("no unreachable block was retained — the guard did not fire, " +
					"so this test proves nothing about it")
			}
		})
	}
}

// TestOptSlotDefsIgnoresMoveDestinations pins the distinction the guard depends
// on. definedValue() reports a move-into destination as a definition — right for
// "this variable was reassigned" — but emitMove cannot create a slot, so a move
// destination must NOT inflate the count of surviving producers. The fixture is
// exactly that shape: the result variable is created once by a `const` and then
// re-targeted by two move-into instructions, so its definedValue count is 3
// while its slot-definition count is 1. Counting the moves as producers is what
// let the guard delete the block holding the `const`.
func TestOptSlotDefsIgnoresMoveDestinations(t *testing.T) {
	m := lowerWithOpt(t, optMoveIntoSrc, "0")
	slot := m.optSlotDefs()
	defs, _ := m.optDefUse()

	inflated := 0
	for v, n := range defs {
		if n > slot[v] {
			inflated++
		}
	}
	if inflated == 0 {
		t.Fatal("no value is defined by more instructions than can create its slot: " +
			"the fixture does not exercise the move-into distinction")
	}
	// The direction must be one-way: every slot-creating definition is also a
	// definedValue definition (Dst > NoVal implies definedValue finds it).
	for v, n := range slot {
		if defs[v] < n {
			t.Errorf("value %d: %d slot definitions but only %d definedValue definitions",
				v, n, defs[v])
		}
	}
}

// TestOptVerifyOperandsCatchesABrokenModule is the oracle's own non-vacuity
// test: a module whose value is referenced but never produced must be reported.
// Without this, every "no violations" assertion above could be passing because
// the checker is broken.
func TestOptVerifyOperandsCatchesABrokenModule(t *testing.T) {
	m := NewModule("t")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	// NewFunc creates and selects the entry block.
	b.NewFunc("f", nil, nil, false)
	// A value that is read but produced by nothing.
	dangling := b.newValueID(i64, "dangling")
	b.Emit(OpAdd, i64, []ValueID{dangling, dangling}, "")
	b.Terminate(OpReturn, nil, nil, "")
	if d := m.optVerifyOperands(); len(d) == 0 {
		t.Fatal("the verifier did not report a value with no producer")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integer identity laws (optTryIdentity / optTryIdentityConst /
// optIdentityOperands / optSetCopy).
//
// These are exercised ONE LAW AT A TIME against a hand-built module rather than
// through LowerHIR, and that is a coverage decision, not a shortcut: the front
// end folds `x * 1` and `x | 0` before MIR ever exists, so no nolang source
// reaches these rules. An end-to-end test would therefore compile, pass, and
// silently cover none of the table below.
//
// The module is the smallest shape each law needs — one function, one block, a
// parameter to stand for the variable operand (a parameter is the one scalar
// that is guaranteed to be neither a constant nor reassigned), and constants
// emitted on demand.
// ─────────────────────────────────────────────────────────────────────────────

// optOperand names one side of a binary identity law: either the variable
// operand or a literal constant.
type optOperand struct {
	isVar bool
	val   int64
}

// optVar is the variable operand of the law under test.
func optVar() optOperand { return optOperand{isVar: true} }

// optLit is a literal constant operand.
func optLit(v int64) optOperand { return optOperand{val: v} }

// optLawModule is the fixture for the identity-law tables.
type optLawModule struct {
	m   *Module
	b   *Builder
	f   *Function
	i64 TypeID
	b1  TypeID
	x   ValueID // i64 parameter — the variable operand of every i64 law
	p   ValueID // i1 parameter  — the variable operand of every i1 law
}

func newOptLawModule(t *testing.T) *optLawModule {
	t.Helper()
	m := NewModule("law")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b1 := b.Type("bool")
	if _, ok := optScalarLLVM(m.Type(b1)); !ok {
		t.Fatal("the fixture's bool type is not a scalar the optimiser models")
	}
	x := b.Param("x", i64)
	p := b.Param("p", b1)
	fid := b.NewFunc("f", []ValueID{x, p}, nil, false)
	return &optLawModule{m: m, b: b, f: m.Func(fid), i64: i64, b1: b1, x: x, p: p}
}

func (h *optLawModule) i64Const(v int64) ValueID { return h.b.EmitInt(OpConst, h.i64, v, "") }
func (h *optLawModule) b1Const(v int64) ValueID  { return h.b.EmitInt(OpConst, h.b1, v, "") }

// lastInst is the instruction the most recent Emit created. Emit appends, so
// the newest instruction is always the last element of the module table.
func (h *optLawModule) lastInst() *Inst {
	return h.m.Inst(InstID(len(h.m.Insts) - 1))
}

// emitOp emits `dst = op a, b` and returns the instruction, so a test can hand
// it straight to the rule under test and then inspect what the rule did to it.
func (h *optLawModule) emitOp(op Op, boolOp bool, a, b optOperand) *Inst {
	typ, variable := h.i64, h.x
	mk := h.i64Const
	if boolOp {
		typ, variable, mk = h.b1, h.p, h.b1Const
	}
	pick := func(o optOperand) ValueID {
		if o.isVar {
			return variable
		}
		return mk(o.val)
	}
	h.b.Emit(op, typ, []ValueID{pick(a), pick(b)}, "")
	return h.lastInst()
}

// snapshots returns the two module-wide maps the rules take, built the same way
// optimizeFunc builds them.
func (h *optLawModule) snapshots() (map[ValueID]int, map[ValueID]InstID) {
	defs, _ := h.m.optDefUse()
	constOf := map[ValueID]InstID{}
	for _, bid := range h.f.Blocks {
		blk := h.m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := h.m.Inst(iid); inst != nil && inst.Op == OpConst && inst.Dst > NoVal {
				constOf[inst.Dst] = iid
			}
		}
	}
	return defs, constOf
}

// TestOptIdentityRewritesToACopy covers the operand-producing half. Each law
// must rewrite the instruction into the single-result copy shape
// (`OpMove` with exactly one operand) naming the surviving operand, and must
// leave the DESTINATION alone — the destination keeps its identity so that no
// consumer is dangled.
func TestOptIdentityRewritesToACopy(t *testing.T) {
	cases := []struct {
		name   string
		op     Op
		boolOp bool
		a, b   optOperand
	}{
		{"x+0", OpAdd, false, optVar(), optLit(0)},
		{"0+x", OpAdd, false, optLit(0), optVar()},
		{"x-0", OpSub, false, optVar(), optLit(0)},
		{"x*1", OpMul, false, optVar(), optLit(1)},
		{"1*x", OpMul, false, optLit(1), optVar()},
		{"x/1", OpDiv, false, optVar(), optLit(1)},
		{"x/1-unsigned", OpUDiv, false, optVar(), optLit(1)},
		{"x<<0", OpShl, false, optVar(), optLit(0)},
		{"x>>0", OpShr, false, optVar(), optLit(0)},
		{"x^xor-0", OpXor, false, optVar(), optLit(0)},
		{"0-xor-x", OpXor, false, optLit(0), optVar()},
		{"x&x", OpBitAnd, false, optVar(), optVar()},
		{"x&-1", OpBitAnd, false, optVar(), optLit(-1)},
		{"-1&x", OpBitAnd, false, optLit(-1), optVar()},
		{"x|x", OpBitOr, false, optVar(), optVar()},
		{"x|0", OpBitOr, false, optVar(), optLit(0)},
		{"0|x", OpBitOr, false, optLit(0), optVar()},
		// OpAnd/OpOr are only identities at i1: `&&` on a wider integer is not
		// a bitwise and, so the i64 forms must NOT be rewritten (asserted in
		// TestOptIdentityLeavesUnsafeCasesAlone).
		{"p&&true", OpAnd, true, optVar(), optLit(1)},
		{"true&&p", OpAnd, true, optLit(1), optVar()},
		{"p&&p", OpAnd, true, optVar(), optVar()},
		{"p||false", OpOr, true, optVar(), optLit(0)},
		{"false||p", OpOr, true, optLit(0), optVar()},
		{"p||p", OpOr, true, optVar(), optVar()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newOptLawModule(t)
			inst := h.emitOp(tc.op, tc.boolOp, tc.a, tc.b)
			dst := inst.Dst
			defs, constOf := h.snapshots()

			if !h.m.optTryIdentity(h.f, inst, defs, constOf) {
				t.Fatalf("the rule did not fire on %v", tc.op)
			}
			if inst.Op != OpMove {
				t.Fatalf("rewrote to %v, want OpMove", inst.Op)
			}
			if len(inst.Args) != 1 {
				t.Fatalf("the copy has %d operands, want 1: %v", len(inst.Args), inst.Args)
			}
			want := h.x
			if tc.boolOp {
				want = h.p
			}
			if inst.Args[0] != want {
				t.Errorf("the copy names value %d, want the surviving operand %d",
					inst.Args[0], want)
			}
			if inst.Dst != dst {
				t.Errorf("the destination changed from %d to %d — every consumer would dangle",
					dst, inst.Dst)
			}
			if inst.Callee > NoVal || inst.Sym != "" || inst.Results != nil {
				t.Errorf("the copy kept stale payload: callee=%d sym=%q results=%v",
					inst.Callee, inst.Sym, inst.Results)
			}
			// The rewritten instruction must still be a well-formed module, or
			// the copy would just be a differently-broken instruction.
			if d := h.m.optVerifyOperands(); len(d) != 0 {
				t.Errorf("the rewrite left a broken module: %v", d)
			}
		})
	}
}

// TestOptIdentityConstProducesAConstant covers the constant-producing half.
// These laws mention no surviving operand, so they need no single-definition
// condition on it — and the same-value laws (`x - x`, `x ^ x`) are exact even
// for a variable that is reassigned, because both reads happen at this one
// instruction.
func TestOptIdentityConstProducesAConstant(t *testing.T) {
	cases := []struct {
		name   string
		op     Op
		boolOp bool
		a, b   optOperand
		want   int64
	}{
		{"x-x", OpSub, false, optVar(), optVar(), 0},
		{"x^xor-x", OpXor, false, optVar(), optVar(), 0},
		{"x*0", OpMul, false, optVar(), optLit(0), 0},
		{"0*x", OpMul, false, optLit(0), optVar(), 0},
		{"x&0", OpBitAnd, false, optVar(), optLit(0), 0},
		{"0&x", OpBitAnd, false, optLit(0), optVar(), 0},
		{"x|-1", OpBitOr, false, optVar(), optLit(-1), -1},
		{"-1|x", OpBitOr, false, optLit(-1), optVar(), -1},
		{"x%1", OpMod, false, optVar(), optLit(1), 0},
		{"x%1-unsigned", OpUMod, false, optVar(), optLit(1), 0},
		{"p&&false", OpAnd, true, optVar(), optLit(0), 0},
		{"false&&p", OpAnd, true, optLit(0), optVar(), 0},
		{"p||true", OpOr, true, optVar(), optLit(1), 1},
		{"true||p", OpOr, true, optLit(1), optVar(), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newOptLawModule(t)
			inst := h.emitOp(tc.op, tc.boolOp, tc.a, tc.b)
			dst := inst.Dst
			defs, constOf := h.snapshots()

			if !h.m.optTryIdentityConst(h.f, inst, defs, constOf) {
				t.Fatalf("the rule did not fire on %v", tc.op)
			}
			if inst.Op != OpConst {
				t.Fatalf("rewrote to %v, want OpConst", inst.Op)
			}
			if len(inst.Args) != 0 {
				t.Errorf("the constant kept %d operands: %v", len(inst.Args), inst.Args)
			}
			if inst.Int != tc.want {
				t.Errorf("produced %d, want %d", inst.Int, tc.want)
			}
			if inst.Dst != dst {
				t.Errorf("the destination changed from %d to %d", dst, inst.Dst)
			}
			// The rewritten value must still resolve as a constant, which is
			// what lets the NEXT round fold on top of it.
			if c, ok := h.m.optConstOf(h.f, dst, defs, map[ValueID]InstID{dst: inst.ID}); !ok || c.i != tc.want {
				t.Errorf("the produced constant does not read back: ok=%v got=%d want=%d",
					ok, c.i, tc.want)
			}
		})
	}
}

// TestOptIdentityLeavesUnsafeCasesAlone is the negative half of both tables.
// Every case here is one an over-eager rule would get wrong, and each is named
// for the reason it is refused — this is the test that would catch someone
// "simplifying" the rules into unsoundness.
//
// A case may refuse only ONE half: `x % 1` and `x * 0` are rightly refused by
// the copy half (the result is not an operand) but rightly accepted by the
// constant half. Those are marked with acceptConst, and the point of listing
// them here is precisely that the copy half must not steal them.
func TestOptIdentityLeavesUnsafeCasesAlone(t *testing.T) {
	cases := []struct {
		name        string
		op          Op
		boolOp      bool
		a, b        optOperand
		acceptConst bool
		why         string
	}{
		{"1/x", OpDiv, false, optLit(1), optVar(), false,
			"1/x is not x — only the DIVISOR may be 1"},
		{"x/-1", OpDiv, false, optVar(), optLit(-1), false,
			"INT_MIN/-1 is UB, and the folder refuses it for the same reason"},
		{"x%1-copy-half", OpMod, false, optVar(), optLit(1), true,
			"x%1 produces 0 — the constant half owns it, the copy half must not"},
		{"x*0-copy-half", OpMul, false, optVar(), optLit(0), true,
			"x*0 produces 0 — the constant half owns it, the copy half must not"},
		{"0*x-copy-half", OpMul, false, optLit(0), optVar(), true,
			"0*x produces 0 — the constant half owns it"},
		{"x+1", OpAdd, false, optVar(), optLit(1), false, "not an identity"},
		{"x-1", OpSub, false, optVar(), optLit(1), false, "not an identity"},
		{"x+x", OpAdd, false, optVar(), optVar(), false, "not an identity"},
		{"x*2", OpMul, false, optVar(), optLit(2), false, "not an identity"},
		{"x&1", OpBitAnd, false, optVar(), optLit(1), false, "not an identity"},
		{"x|1", OpBitOr, false, optVar(), optLit(1), false, "not an identity"},
		{"x<<1", OpShl, false, optVar(), optLit(1), false, "not an identity"},
		{"x%2", OpMod, false, optVar(), optLit(2), false, "not an identity"},
		{"0/x", OpDiv, false, optLit(0), optVar(), false, "0/x traps for x=0; not an identity"},
		{"x|-2", OpBitOr, false, optVar(), optLit(-2), false,
			"only an ALL-ONES mask is an identity, and -2 is not all ones"},
		{"x&-2", OpBitAnd, false, optVar(), optLit(-2), false,
			"only an ALL-ONES mask is an identity, and -2 is not all ones"},
		{"x%1-negative", OpMod, false, optVar(), optLit(-1), false,
			"x % -1 is UB for INT_MIN, so neither half may touch it"},
		// `&&`/`||` on a 64-bit integer is not a bitwise and/or, so neither
		// half may touch the i64 forms.
		{"i64-and", OpAnd, false, optVar(), optVar(), false, "&& on i64 is not a bitwise and"},
		{"i64-or", OpOr, false, optVar(), optVar(), false, "|| on i64 is not a bitwise or"},
		{"i64-and-0", OpAnd, false, optVar(), optLit(0), false, "&& on i64 is not a bitwise and"},
		{"i64-or-1", OpOr, false, optVar(), optLit(1), false, "|| on i64 is not a bitwise or"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newOptLawModule(t)
			inst := h.emitOp(tc.op, tc.boolOp, tc.a, tc.b)
			before := *inst
			defs, constOf := h.snapshots()

			if h.m.optTryIdentity(h.f, inst, defs, constOf) {
				t.Errorf("optTryIdentity fired on %s: %s", tc.name, tc.why)
			}
			if inst.Op != before.Op || len(inst.Args) != len(before.Args) {
				t.Errorf("the copy half mutated a refused instruction: %v %v -> %v %v",
					before.Op, before.Args, inst.Op, inst.Args)
			}
			// The copy half must have left the instruction alone, so the
			// constant half is now probed on the ORIGINAL instruction.
			fired := h.m.optTryIdentityConst(h.f, inst, defs, constOf)
			if fired == tc.acceptConst {
				return
			}
			if fired {
				t.Errorf("optTryIdentityConst fired on %s: %s", tc.name, tc.why)
			} else {
				t.Errorf("optTryIdentityConst refused %s, but the constant half is "+
					"supposed to own it: %s", tc.name, tc.why)
			}		})
	}
}

// TestOptIdentityRejectsFloats pins the deliberate absence of float identities.
// The integer laws do NOT carry over: `x * 0.0` is not 0 for x = inf or NaN,
// and `x + 0.0` is not x for x = -0.0. The rule must refuse doubles wholesale
// rather than reason about them.
func TestOptIdentityRejectsFloats(t *testing.T) {
	m := NewModule("floatlaw")
	b := &Builder{Mod: m}
	f64 := b.Type("f64")
	if lt, ok := optScalarLLVM(m.Type(f64)); !ok || lt != "double" {
		t.Fatalf("the fixture's f64 type is not modelled as double: %q %v", lt, ok)
	}
	x := b.Param("x", f64)
	fid := b.NewFunc("f", []ValueID{x}, nil, false)
	f := m.Func(fid)
	zero := b.EmitInt(OpConst, f64, 0, "")
	one := b.EmitInt(OpConst, f64, 1, "")

	// The const index the rules take, built the way optimizeFunc builds it.
	constIdx := map[ValueID]InstID{}
	for _, iid := range m.Block(f.Entry).Insts {
		if i := m.Inst(iid); i != nil && i.Op == OpConst && i.Dst > NoVal {
			constIdx[i.Dst] = iid
		}
	}
	if len(constIdx) != 2 {
		t.Fatalf("the fixture indexed %d constants, want 2", len(constIdx))
	}

	for _, tc := range []struct {
		name string
		op   Op
		a, b ValueID
	}{
		{"x+0.0", OpAdd, x, zero},
		{"x-0.0", OpSub, x, zero},
		{"x*1.0", OpMul, x, one},
		{"x/1.0", OpDiv, x, one},
		{"x*0.0", OpMul, x, zero},
		{"x-x", OpSub, x, x},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b.Emit(tc.op, f64, []ValueID{tc.a, tc.b}, "")
			inst := m.Inst(InstID(len(m.Insts) - 1))
			before := *inst
			defs, _ := m.optDefUse()
			if m.optTryIdentity(f, inst, defs, constIdx) {
				t.Errorf("optTryIdentity rewrote a float operation %s", tc.name)
			}
			if m.optTryIdentityConst(f, inst, defs, constIdx) {
				t.Errorf("optTryIdentityConst rewrote a float operation %s", tc.name)
			}
			if inst.Op != before.Op {
				t.Errorf("a refused float law still mutated the instruction: %v -> %v",
					before.Op, inst.Op)
			}
		})
	}
}

// TestOptIdentityRequiresASingleDefinitionSource pins the gate the copy half
// applies to the surviving operand. A MIR value is a VARIABLE: if the operand
// can be reassigned, pointing a later read of the result at the operand would
// read the NEW value. Requiring a single definition removes the question.
//
// The fixture gives x TWO definitions using the move-into encoding (no Dst, the
// destination in Args[1] — see moveDst), which is what hir2mir emits for an
// assignment to an existing binding.
func TestOptIdentityRequiresASingleDefinitionSource(t *testing.T) {
	h := newOptLawModule(t)
	zero := h.i64Const(0)
	for i := 0; i < 2; i++ {
		h.b.Emit(OpMove, h.i64, []ValueID{zero}, "")
		mi := h.m.Inst(InstID(len(h.m.Insts) - 1))
		mi.Dst = NoVal
		mi.Args = []ValueID{zero, h.x}
	}

	inst := h.emitOp(OpAdd, false, optVar(), optLit(0))
	defs, constOf := h.snapshots()
	if defs[h.x] < 2 {
		t.Fatalf("the fixture gave x %d definitions, want at least 2", defs[h.x])
	}
	if h.m.optTryIdentity(h.f, inst, defs, constOf) {
		t.Error("the rule rewrote through a reassignable operand — a later reader " +
			"would see the new value instead of the one this instruction saw")
	}
	if inst.Op != OpAdd {
		t.Errorf("the instruction changed to %v despite the rule reporting no rewrite", inst.Op)
	}
}

// TestOptCopyPropKeepsACopyOfAReassignedSource is the redefinition guard. A
// parameter contributes no definition of its own, so `defs[p] == 1` here comes
// entirely from the single assignment below — which is enough to satisfy the
// module-wide "defined once" test and NOT enough to prove the source is stable.
// Without the position check, the copy would be deleted and the reader would be
// pointed at the reassigned parameter.
func TestOptCopyPropKeepsACopyOfAReassignedSource(t *testing.T) {
	m := NewModule("reassign")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	p := b.Param("p", i64)
	f := m.Func(b.NewFunc("f", []ValueID{p}, nil, false))
	entry := b.CurrentBlock()

	seven := b.EmitInt(OpConst, i64, 7, "")
	copied := b.Emit(OpMove, i64, []ValueID{p}, "") // t = copy p
	copyInst := InstID(len(m.Insts) - 1)
	// p = 7, in the move-into encoding.
	b.Emit(OpMove, i64, []ValueID{seven}, "")
	assign := m.Inst(InstID(len(m.Insts) - 1))
	assign.Dst = NoVal
	assign.Args = []ValueID{seven, p}
	// The reader runs AFTER the reassignment, so it must keep reading the copy.
	reader := b.Emit(OpAdd, i64, []ValueID{copied, copied}, "")
	readerInst := InstID(len(m.Insts) - 1)
	b.Terminate(OpReturn, []ValueID{reader}, nil, "")

	defs, _ := m.optDefUse()
	if defs[p] != 1 {
		t.Fatalf("the fixture gave p %d definitions, want exactly 1 — the guard is "+
			"supposed to catch this shape by POSITION, not by count", defs[p])
	}
	refs := m.optRefCounts()
	var st OptStats
	if n := m.optCopyProp(f, defs, refs, &st); n != 0 {
		t.Fatalf("propagated %d copies, want 0 — the source is reassigned before the reader", n)
	}
	for _, iid := range m.Block(entry).Insts {
		if iid == copyInst {
			return
		}
	}
	t.Error("the copy was deleted even though its source is reassigned before the reader")
	_ = readerInst
}

// ─────────────────────────────────────────────────────────────────────────────
// Copy propagation (optCopySource / optCopyProp).
//
// The pass deletes a copy by pointing the copy's readers at its source, so the
// tests come in pairs: the deletion that must happen, and the guard that must
// stop it. Both are needed — a pass that deletes nothing also passes every
// "kept" assertion.
// ─────────────────────────────────────────────────────────────────────────────

// optCopyFixture builds `entry: c = const 7; t = move c; ...` and returns the
// module, the function, and the ids involved, so a test can append whatever
// shape it is probing.
type optCopyFixture struct {
	m        *Module
	b        *Builder
	f        *Function
	i64      TypeID
	entry    BlockID
	c        ValueID
	t        ValueID
	moveInst *Inst
}

func newOptCopyFixture(t *testing.T) *optCopyFixture {
	t.Helper()
	m := NewModule("copy")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	fid := b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	c := b.EmitInt(OpConst, i64, 7, "")
	copyDst := b.Emit(OpMove, i64, []ValueID{c}, "")
	moveInst := m.Inst(InstID(len(m.Insts) - 1))
	if moveInst.Op != OpMove || moveInst.Dst != copyDst {
		t.Fatal("the fixture did not build the copy it claims to")
	}
	return &optCopyFixture{m: m, b: b, f: m.Func(fid), i64: i64, entry: entry, c: c, t: copyDst, moveInst: moveInst}
}

// blockInsts is the instruction ids the block still lists, which is what
// codegen emits — a deleted instruction must be gone from here.
func (h *optCopyFixture) blockInsts(bid BlockID) []InstID {
	return h.m.Block(bid).Insts
}

func (h *optCopyFixture) containsMove() bool {
	for _, iid := range h.blockInsts(h.entry) {
		if iid == h.moveInst.ID {
			return true
		}
	}
	return false
}

func TestOptCopyPropDeletesASingleUseCopy(t *testing.T) {
	h := newOptCopyFixture(t)
	// The reader is in the same block, after the copy.
	r := h.b.Emit(OpAdd, h.i64, []ValueID{h.t, h.t}, "")
	addInst := InstID(len(h.m.Insts) - 1)
	h.b.Terminate(OpReturn, []ValueID{r}, nil, "")

	defs, _ := h.m.optDefUse()
	refs := h.m.optRefCounts()
	var st OptStats
	if n := h.m.optCopyProp(h.f, defs, refs, &st); n != 1 {
		t.Fatalf("propagated %d copies, want 1", n)
	}
	if h.containsMove() {
		t.Error("the copy is still listed by its block — the readers were not redirected")
	}
	add := h.m.Inst(addInst)
	if add.Op != OpAdd {
		t.Fatalf("the reader changed to %v", add.Op)
	}
	for i, a := range add.Args {
		if a != h.c {
			t.Errorf("the reader's operand %d is %d, want the copy's source %d", i, a, h.c)
		}
	}
	if d := h.m.optVerifyOperands(); len(d) != 0 {
		t.Errorf("propagation left a broken module: %v", d)
	}
	if st.Copies != 1 {
		t.Errorf("stats recorded %d copies, want 1", st.Copies)
	}
}

// TestOptCopyPropKeepsACopyReadFromAnotherBlock is the central guard. The copy
// is per-block precisely so that no dominance analysis is needed; if a read
// lives anywhere else, deleting the copy would leave that read pointing at a
// value whose only definition is gone.
func TestOptCopyPropKeepsACopyReadFromAnotherBlock(t *testing.T) {
	h := newOptCopyFixture(t)
	b1 := h.b.NewBlock("b1")
	h.b.SetBlock(b1)
	r := h.b.Emit(OpAdd, h.i64, []ValueID{h.t, h.t}, "")
	h.b.Terminate(OpReturn, []ValueID{r}, nil, "")
	h.b.SetBlock(h.entry)
	h.b.Terminate(OpBr, nil, []BlockID{b1}, "")

	defs, _ := h.m.optDefUse()
	refs := h.m.optRefCounts()
	var st OptStats
	if n := h.m.optCopyProp(h.f, defs, refs, &st); n != 0 {
		t.Fatalf("propagated %d copies, want 0 — the read is in another block", n)
	}
	if !h.containsMove() {
		t.Error("the copy was deleted even though a reader in another block needs it")
	}
}

// TestOptCopyPropKeepsACopyReadBeforeIt runs the ordering half of the guard: a
// read that precedes the copy in the same block must also block deletion.
// Within a block execution is ordered, so a reader that runs before the copy
// would, if redirected, read the value from a definition that has not happened
// yet.
func TestOptCopyPropKeepsACopyReadBeforeIt(t *testing.T) {
	m := NewModule("copybefore")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b.NewFunc("f", nil, nil, false)
	f := m.Func(m.FuncByName["f"])
	c := b.EmitInt(OpConst, i64, 7, "")

	// A read of the copy's destination, emitted BEFORE the copy defines it.
	early := b.Emit(OpAdd, i64, []ValueID{c, c}, "")
	earlyInst := InstID(len(m.Insts) - 1)
	if early <= NoVal {
		t.Fatal("the fixture's early read produced no value")
	}

	tv := b.Emit(OpMove, i64, []ValueID{c}, "")
	mv := m.Inst(InstID(len(m.Insts) - 1))

	// Re-target the earlier read at t, so it reads t before the copy defines it.
	m.Inst(earlyInst).Args = []ValueID{tv, tv}
	b.Terminate(OpReturn, []ValueID{tv}, nil, "")

	defs, _ := m.optDefUse()
	refs := m.optRefCounts()
	var st OptStats
	if n := m.optCopyProp(f, defs, refs, &st); n != 0 {
		t.Fatalf("propagated %d copies, want 0 — a reader precedes the copy", n)
	}
	for _, iid := range m.Block(f.Entry).Insts {
		if iid == mv.ID {
			return
		}
	}
	t.Error("the copy was deleted even though a reader precedes it")
}

// TestOptCopySourceRefusesOwnedAndMixedWidthValues pins the two type gates. The
// owned gate is not a technicality: an owned value's move TRANSFERS its buffer
// and its drop, so deleting the move would leak or double-free. Only non-owned
// scalars, whose copy carries nothing but bits, may ever be propagated.
//
// The width gate is checked with `char` against `i64`: i16/i32/u16/u32 all lower
// to i64 in this compiler, so only a genuinely different LLVM type (char is i32,
// bool is i1) exercises the comparison at all.
func TestOptCopySourceRefusesOwnedAndMixedWidthValues(t *testing.T) {
	m := NewModule("copygates")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	charT := b.Type("char")
	if lt, ok := optScalarLLVM(m.Type(i64)); !ok || lt != "i64" {
		t.Fatalf("the fixture's i64 is not modelled as i64: %q %v", lt, ok)
	}
	if lt, ok := optScalarLLVM(m.Type(charT)); !ok || lt != "i32" {
		t.Fatalf("the fixture's char is not modelled as i32: %q %v", lt, ok)
	}
	owned := b.TypeExplicit("thing", KindStruct, true, NoType)
	if ty := m.Type(owned); ty == nil || !ty.Owned {
		t.Fatal("the fixture failed to build an owned type")
	}
	b.NewFunc("f", nil, nil, false)
	f := m.Func(m.FuncByName["f"])

	ownedSrc := b.newValueID(owned, "ownedSrc")
	ownedCopy := b.Emit(OpMove, owned, []ValueID{ownedSrc}, "")
	wideSrc := b.newValueID(i64, "wide")
	narrowCopy := b.Emit(OpMove, charT, []ValueID{wideSrc}, "")
	plainSrc := b.newValueID(i64, "plain")
	plainCopy := b.Emit(OpMove, i64, []ValueID{plainSrc}, "")

	defs, _ := m.optDefUse()
	byDst := map[ValueID]*Inst{}
	for _, iid := range m.Block(f.Entry).Insts {
		if i := m.Inst(iid); i != nil && i.Dst > NoVal {
			byDst[i.Dst] = i
		}
	}

	if _, ok := m.optCopySource(f, byDst[ownedCopy], defs); ok {
		t.Error("propagated a copy of an OWNED value — its move transfers the buffer " +
			"and its drop, so deleting it would leak or double-free")
	}
	if _, ok := m.optCopySource(f, byDst[narrowCopy], defs); ok {
		t.Error("propagated a copy whose source and destination have different LLVM widths")
	}
	if _, ok := m.optCopySource(f, byDst[plainCopy], defs); !ok {
		t.Error("refused a plain i64 copy — the fixture does not exercise the accept path, " +
			"so the two refusals above prove nothing")
	}
}

// TestOptCopySourceRequiresASingleDefinition pins the defs gate: if the copy's
// destination has a second definition, deleting this one does not make it dead,
// it just changes which definition produces the value the readers see.
func TestOptCopySourceRequiresASingleDefinition(t *testing.T) {
	h := newOptCopyFixture(t)
	// A second definition of t, in the move-into encoding (no Dst).
	h.b.Emit(OpMove, h.i64, []ValueID{h.c}, "")
	mi := h.m.Inst(InstID(len(h.m.Insts) - 1))
	mi.Dst = NoVal
	mi.Args = []ValueID{h.c, h.t}

	defs, _ := h.m.optDefUse()
	if defs[h.t] != 2 {
		t.Fatalf("the fixture gave t %d definitions, want 2", defs[h.t])
	}
	if _, ok := h.m.optCopySource(h.f, h.moveInst, defs); ok {
		t.Error("accepted a copy whose destination is defined twice")
	}
}

// optHandleCopyFixture builds the MIR shape hir2mir emits for
//
//	h  = run worker(21)
//	h2 = h
//	a  = awy h
//	b  = awy h2
//
// which is `test-alias-both` in tests/async-rc.no — the case that file's own
// comment describes as "the two awaits shared one slot, so the second read a
// zeroed handle and printed 0 instead of 42".
//
// `handleOps` decides whether the R-tier pair (`task-retain` / the second
// `await`) is emitted, so one fixture serves as both the positive case and a
// control that proves the test is not passing for want of anything to propagate.
func optHandleCopyFixture(t *testing.T, handleOps bool) (m *Module, f *Function, h, h2 ValueID, moveInst InstID) {
	t.Helper()
	m = NewModule("handlecopy")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	fid := b.NewFunc("f", nil, nil, false)
	b.CurrentBlock()
	c := b.EmitInt(OpConst, i64, 21, "")
	h = b.Emit(OpRun, i64, []ValueID{c}, "")
	h2 = b.Emit(OpMove, i64, []ValueID{h}, "")
	moveInst = InstID(len(m.Insts) - 1)
	if mi := m.Inst(moveInst); mi.Op != OpMove || mi.Dst != h2 {
		t.Fatal("the fixture did not build the handle copy it claims to build")
	}
	if handleOps {
		b.EmitVoid(OpTaskRetain, []ValueID{h2}, "")
		b.Emit(OpAwait, i64, []ValueID{h}, "")
		b.Emit(OpAwait, i64, []ValueID{h2}, "")
	} else {
		// The control needs a reader that is NOT a handle consumer — an
		// `OpAwait` would trip the very guard under test, which is how the
		// first version of this fixture failed.
		r := b.Emit(OpAdd, i64, []ValueID{h, h2}, "")
		b.Terminate(OpReturn, []ValueID{r}, nil, "")
		return m, m.Func(fid), h, h2, moveInst
	}
	b.Terminate(OpReturn, nil, nil, "")
	return m, m.Func(fid), h, h2, moveInst
}

// TestOptCopyPropKeepsAHandleCopyAlive is the regression test for the
// miscompile that `optHandleConsumer` documents.
//
// `OpAwait` consumes its operand — it releases the reference and zeroes the
// slot — so redirecting the second await onto the first one's slot makes it
// read the zeroed handle. Before the fix this pass collapsed the pair and
// tests/async-rc.no printed `1 42 0` instead of `1 42 42`, and
// tests/async-handle-alias.no lost two of its three results.
//
// The pass runs BEFORE `Analyze`, so the drop/ownership rewrites are not
// present yet; the handle ops are the only destructive readers it can see.
func TestOptCopyPropKeepsAHandleCopyAlive(t *testing.T) {
	m, f, _, h2, moveInst := optHandleCopyFixture(t, true)

	defs, _ := m.optDefUse()
	refs := m.optRefCounts()
	var st OptStats
	if n := m.optCopyProp(f, defs, refs, &st); n != 0 {
		t.Errorf("propagated %d copies, want 0 — the destination is read as an R-tier handle", n)
	}
	blk := m.Block(f.Blocks[0])
	alive := false
	for _, iid := range blk.Insts {
		if iid == moveInst {
			alive = true
		}
	}
	if !alive {
		t.Fatal("the handle copy was deleted; the readers now share one slot")
	}
	// Every handle op must still name h2, not the source.
	for _, iid := range blk.Insts {
		inst := m.Inst(iid)
		if inst == nil || !optHandleConsumer(inst.Op) {
			continue
		}
		for _, a := range inst.Args {
			if a == h2 {
				return
			}
		}
	}
	t.Errorf("no handle op still reads the copy's destination %d — the copy survived "+
		"but its readers were redirected anyway, which is the same bug", h2)
}

// TestOptCopyPropStillPropagatesWithoutHandleOps is the control: the same
// fixture minus the R-tier pair must still be optimised. Without it the test
// above would pass even if the pass refused every copy unconditionally.
func TestOptCopyPropStillPropagatesWithoutHandleOps(t *testing.T) {
	m, f, h, _, moveInst := optHandleCopyFixture(t, false)

	defs, _ := m.optDefUse()
	refs := m.optRefCounts()
	var st OptStats
	if n := m.optCopyProp(f, defs, refs, &st); n != 1 {
		t.Fatalf("propagated %d copies, want 1 — the guard is refusing ordinary copies too", n)
	}
	for _, iid := range m.Block(f.Blocks[0]).Insts {
		if iid == moveInst {
			t.Fatal("the copy survived even though nothing reads it as a handle")
		}
	}
	for _, iid := range m.Block(f.Blocks[0]).Insts {
		inst := m.Inst(iid)
		if inst == nil || inst.Op != OpAdd {
			continue
		}
		for _, a := range inst.Args {
			if a == h {
				return
			}
		}
	}
	t.Error("the ordinary reader was not redirected onto the copy's source")
}

// TestOptCopyPropKeepsACopyWhoseSourceIsConsumed covers the mirror image of the
// test above: the copy's DESTINATION is read only by an ordinary instruction,
// but the SOURCE is consumed by an `awy` sitting between the copy and that
// reader. Redirecting the ordinary read onto the source would hand it the
// handle the await had already zeroed, so the copy must survive.
func TestOptCopyPropKeepsACopyWhoseSourceIsConsumed(t *testing.T) {
	m := NewModule("handleconsume")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	fid := b.NewFunc("f", nil, nil, false)
	b.CurrentBlock()
	c := b.EmitInt(OpConst, i64, 21, "")
	h := b.Emit(OpRun, i64, []ValueID{c}, "")
	hd := b.Emit(OpMove, i64, []ValueID{h}, "") // the copy under test
	moveInst := InstID(len(m.Insts) - 1)
	if mi := m.Inst(moveInst); mi.Op != OpMove || mi.Dst != hd {
		t.Fatal("the fixture did not build the copy it claims to build")
	}
	// The source is consumed here — after the copy, before the reader.
	b.Emit(OpAwait, i64, []ValueID{h}, "")
	// An ordinary reader of the copy's destination: on its own this is exactly
	// the shape the propagator is supposed to rewrite.
	r := b.Emit(OpAdd, i64, []ValueID{hd, hd}, "")
	b.Terminate(OpReturn, []ValueID{r}, nil, "")

	f := m.Func(fid)
	defs, _ := m.optDefUse()
	refs := m.optRefCounts()
	var st OptStats
	if n := m.optCopyProp(f, defs, refs, &st); n != 0 {
		t.Errorf("propagated %d copies, want 0 — the source is consumed by an await "+
			"between the copy and the reader", n)
	}
	for _, iid := range m.Block(f.Blocks[0]).Insts {
		if iid == moveInst {
			return
		}
	}
	t.Error("the copy was deleted even though its source is consumed after it")
}

// ─────────────────────────────────────────────────────────────────────────────
// Block merging (optMergeBlocks).
//
// Each test pins one guard, because the pass is only sound if EVERY guard holds:
// dropping any one of them merges a block that is not dominated by its
// predecessor, which silently changes the program.
// ─────────────────────────────────────────────────────────────────────────────

// optMergeModule builds a two-block function `entry -br-> b1` where entry holds
// one constant and b1 holds one constant and the return.
func optMergeModule(t *testing.T) (*Module, *Function, BlockID, BlockID, ValueID, ValueID) {
	t.Helper()
	m := NewModule("merge")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	a := b.EmitInt(OpConst, i64, 1, "")
	b1 := b.NewBlock("b1")
	b.SetBlock(b1)
	d := b.EmitInt(OpConst, i64, 2, "")
	b.Terminate(OpReturn, []ValueID{d}, nil, "")
	b.SetBlock(entry)
	b.Terminate(OpBr, nil, []BlockID{b1}, "")
	return m, m.Func(m.FuncByName["f"]), entry, b1, a, d
}

func TestOptMergeBlocksFoldsABlockIntoItsOnlyPredecessor(t *testing.T) {
	m, f, entry, b1, _, d := optMergeModule(t)
	var st OptStats
	m.optMergeBlocks(f, &st)

	if st.Merged != 1 {
		t.Fatalf("merged %d blocks, want 1", st.Merged)
	}
	if len(f.Blocks) != 1 {
		t.Fatalf("the function still lists %d blocks: %v", len(f.Blocks), f.Blocks)
	}
	if f.Blocks[0] != entry {
		t.Errorf("the surviving block is %d, want the entry %d", f.Blocks[0], entry)
	}
	// b1 is gone from the block table AND from the function's list; its
	// instructions moved into entry and its terminator became entry's.
	blk := m.Block(entry)
	if len(blk.Insts) != 2 {
		t.Errorf("the merged block holds %d instructions, want 2: %v", len(blk.Insts), blk.Insts)
	}
	if blk.Term == nil || blk.Term.Op != OpReturn || len(blk.Term.Args) != 1 || blk.Term.Args[0] != d {
		t.Errorf("the merged block did not inherit the successor's terminator: %+v", blk.Term)
	}
	if dead := m.Block(b1); dead.Term != nil || len(dead.Insts) != 0 {
		t.Errorf("the removed block still carries code: term=%+v insts=%v", dead.Term, dead.Insts)
	}
	if d := m.optVerifyOperands(); len(d) != 0 {
		t.Errorf("merging left a broken module: %v", d)
	}
}

// TestOptMergeBlocksRefusesAMultiPredecessor is the guard that makes the
// transformation legal: a block may only be folded into a predecessor that is
// its ONLY predecessor, because otherwise the merged code would run on paths
// that never branched there.
func TestOptMergeBlocksRefusesAMultiPredecessor(t *testing.T) {
	m := NewModule("merge2")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	cond := b.EmitInt(OpConst, i64, 1, "")
	left := b.NewBlock("left")
	right := b.NewBlock("right")
	join := b.NewBlock("join")
	// entry --condbr--> left, right ; left --> join ; right --> join
	b.SetBlock(left)
	b.Terminate(OpBr, nil, []BlockID{join}, "")
	b.SetBlock(right)
	b.Terminate(OpBr, nil, []BlockID{join}, "")
	b.SetBlock(join)
	b.Terminate(OpReturn, nil, nil, "")
	b.SetBlock(entry)
	b.Terminate(OpCondBr, []ValueID{cond}, []BlockID{left, right}, "")
	f := m.Func(m.FuncByName["f"])

	var st OptStats
	m.optMergeBlocks(f, &st)
	if st.Merged != 0 {
		t.Errorf("merged %d blocks, want 0 — join has two predecessors and left/right "+
			"are reached by a conditional branch", st.Merged)
	}
	if len(f.Blocks) != 4 {
		t.Errorf("the function lists %d blocks, want 4", len(f.Blocks))
	}
	if blk := m.Block(join); blk == nil || len(blk.Insts) != 0 {
		t.Error("join was modified")
	}
}

// TestOptMergeBlocksRefusesTheEntryBlock: the entry block must survive as
// f.Blocks[0], because codegen emits it as `name.bb0` and derives every other
// label from the block's index in the list.
func TestOptMergeBlocksRefusesTheEntryBlock(t *testing.T) {
	m := NewModule("merge3")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	// entry is its own predecessor's successor: `entry -br-> b1`, and b1 is
	// unreachable from anywhere else, so the pass must refuse on entry.
	b1 := b.NewBlock("b1")
	b.SetBlock(b1)
	b.Terminate(OpReturn, nil, nil, "")
	b.SetBlock(entry)
	b.Terminate(OpBr, nil, []BlockID{b1}, "")
	f := m.Func(m.FuncByName["f"])

	var st OptStats
	m.optMergeBlocks(f, &st)
	if f.Entry != entry {
		t.Fatalf("the entry block changed from %d to %d", entry, f.Entry)
	}
	if len(f.Blocks) == 0 || f.Blocks[0] != entry {
		t.Errorf("f.Blocks[0] is %v, want the entry %d — codegen labels blocks by index",
			f.Blocks, entry)
	}
	_ = i64
}

// TestOptMergeBlocksRefusesASelfLoop covers the `p == bid` guard: a block that
// is its own only predecessor must not be spliced into itself.
func TestOptMergeBlocksRefusesASelfLoop(t *testing.T) {
	m := NewModule("merge4")
	b := &Builder{Mod: m}
	b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	b.Terminate(OpReturn, nil, nil, "")
	loop := b.NewBlock("loop")
	b.SetBlock(loop)
	b.Terminate(OpBr, nil, []BlockID{loop}, "")
	f := m.Func(m.FuncByName["f"])

	var st OptStats
	m.optMergeBlocks(f, &st)
	if st.Merged != 0 {
		t.Errorf("merged %d blocks, want 0 — a self-loop must not be spliced into itself", st.Merged)
	}
	if blk := m.Block(loop); blk == nil || blk.Term == nil || blk.Term.Targets[0] != loop {
		t.Error("the self-loop block was modified")
	}
	_ = entry
}

// ─────────────────────────────────────────────────────────────────────────────
// Empty-block threading: the guard, and the miscompile it exists to prevent.
//
// The failure this pins is subtle and was found by the execution oracle in
// scripts/miropt_vs_llvm.py (--run), NOT by the compile sweep: every arm built
// cleanly, every unit test passed, and 49 of the corpus's 631 programs silently
// produced different output. See optThreadEmptyBlocks' comment for the shape.
// ─────────────────────────────────────────────────────────────────────────────

// optDropOnEntryDoubleFrees reports the "frees one value twice" pairs the
// threaded CFG must never contain: a block whose FIRST instruction drops v,
// entered from a predecessor whose LAST instruction already dropped v. One
// start-drop at a merge point cannot serve predecessors that disagree about
// whether v is still owned, so this pair is a guaranteed double free.
func optDropOnEntryDoubleFrees(m *Module) []string {
	var out []string
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		endDrop := map[BlockID]map[ValueID]bool{}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil || blk.Term == nil || len(blk.Insts) == 0 {
				continue
			}
			inst := m.Inst(blk.Insts[len(blk.Insts)-1])
			if inst == nil || inst.Op != OpDrop || len(inst.Args) != 1 {
				continue
			}
			for _, tgt := range blk.Term.Targets {
				if endDrop[tgt] == nil {
					endDrop[tgt] = map[ValueID]bool{}
				}
				endDrop[tgt][inst.Args[0]] = true
			}
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil || len(blk.Insts) == 0 {
				continue
			}
			inst := m.Inst(blk.Insts[0])
			if inst == nil || inst.Op != OpDrop || len(inst.Args) != 1 {
				continue
			}
			if endDrop[bid][inst.Args[0]] {
				out = append(out, fmt.Sprintf("%s: block %d drops value %d on entry, "+
					"but a predecessor already dropped it", f.Name, bid, inst.Args[0]))
			}
		}
	}
	return out
}

// optThreadFixture builds `entry -condbr-> a, b ; a -br-> join ; b -br-> tramp ;
// tramp -br-> join ; join -return`. `tramp` is the empty pass-through, `join` is
// the merge it feeds. `shared` selects whether `a` also branches straight into
// `join` — the difference between a safe redirect and a widening one.
func optThreadFixture(t *testing.T, shared bool) (*Module, *Function, BlockID, BlockID) {
	t.Helper()
	m := NewModule("thread")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	cond := b.EmitInt(OpConst, i64, 1, "")
	a := b.NewBlock("a")
	bb := b.NewBlock("b")
	tramp := b.NewBlock("tramp")
	join := b.NewBlock("join")

	b.SetBlock(a)
	b.EmitInt(OpConst, i64, 2, "")
	b.SetBlock(bb)
	b.EmitInt(OpConst, i64, 3, "")
	b.SetBlock(join)
	b.Terminate(OpReturn, nil, nil, "")
	b.SetBlock(tramp)
	b.Terminate(OpBr, nil, []BlockID{join}, "")
	if shared {
		b.SetBlock(a)
		b.Terminate(OpCondBr, []ValueID{cond}, []BlockID{tramp, join}, "")
	} else {
		b.SetBlock(a)
		b.Terminate(OpBr, nil, []BlockID{tramp}, "")
	}
	b.SetBlock(bb)
	b.Terminate(OpBr, nil, []BlockID{tramp}, "")
	b.SetBlock(entry)
	b.Terminate(OpCondBr, []ValueID{cond}, []BlockID{a, bb}, "")
	return m, m.Func(m.FuncByName["f"]), tramp, join
}

// TestOptThreadingRefusesToWidenATargetsPredecessors is the guard. When a
// predecessor already reaches `join` directly, redirecting the OTHER predecessor
// through the trampoline would give `join` a predecessor it did not have — and
// the drop `Analyze` then places at `join`'s start fires on the pre-existing
// path too.
func TestOptThreadingRefusesToWidenATargetsPredecessors(t *testing.T) {
	m, f, tramp, join := optThreadFixture(t, true)
	var st OptStats
	m.optThreadEmptyBlocks(f, &st)
	if st.Threaded != 0 {
		t.Errorf("threaded %d empty blocks, want 0 — the redirect would widen "+
			"block %d's predecessor set beyond block %d", st.Threaded, join, tramp)
	}
	for _, bid := range f.Blocks {
		if bid == tramp {
			return
		}
	}
	t.Error("the trampoline was removed even though its target has another predecessor")
}

// TestOptThreadingStillFiresWhenTheTargetIsPrivate is the non-vacuity half: with
// the trampoline the ONLY route into `join`, the redirect relabels the path
// instead of adding one, and the pass must still fire. Without this, the test
// above would pass even if threading had been deleted outright.
func TestOptThreadingStillFiresWhenTheTargetIsPrivate(t *testing.T) {
	m, f, tramp, _ := optThreadFixture(t, false)
	var st OptStats
	m.optThreadEmptyBlocks(f, &st)
	if st.Threaded != 1 {
		t.Fatalf("threaded %d empty blocks, want 1", st.Threaded)
	}
	for _, bid := range f.Blocks {
		if bid == tramp {
			t.Fatal("the trampoline survived even though its target is private to it")
		}
	}
	if d := m.optVerifyOperands(); len(d) != 0 {
		t.Errorf("threading left a broken module: %v", d)
	}
}

// optMatchSrc is tests/mem-safety/option-match-basic.no reduced to the shape
// that broke: a three-arm option match whose arms have different drop needs.
const optMatchSrc = `test-fn = () (result ?str) {
    result = nil
    result = 'hello'
}
main = () {
    v1 = test-fn()
    v1: {
        ok -> {
            print('ok branch')
            print(it)
        }

        nil -> {
            print('nil branch')
        }

        err -> {
            print('err branch')
        }
    }
    print('end')
}
main()
`

// TestOptNeverDoubleFreesOnBlockEntry is the end-to-end regression for the
// miscompile. It asserts the invariant directly on the lowered module, at the
// level where the damage was done, rather than only on the CFG shape: no block
// may drop a value on entry that a predecessor already dropped.
func TestOptNeverDoubleFreesOnBlockEntry(t *testing.T) {
	for _, lvl := range []string{"1", "2"} {
		t.Run("level-"+lvl, func(t *testing.T) {
			off := lowerWithOpt(t, optMatchSrc, "0")
			if d := optDropOnEntryDoubleFrees(off); len(d) != 0 {
				t.Fatalf("the fixture double-frees with the pass OFF, so this test "+
					"cannot attribute anything to the optimiser:\n  %s",
					strings.Join(d, "\n  "))
			}
			on := lowerWithOpt(t, optMatchSrc, lvl)
			if d := optDropOnEntryDoubleFrees(on); len(d) != 0 {
				t.Errorf("level %s frees a value twice:\n  %s", lvl, strings.Join(d, "\n  "))
			}
			if d := on.optVerifyOperands(); len(d) != 0 {
				t.Errorf("level %s left a broken module:\n  %s", lvl, strings.Join(d, "\n  "))
			}
		})
	}
}
// every step is a single-predecessor unconditional branch. One round can only
// fold one pair (after folding b1 into entry, b2's predecessor changes), so the
// pass must iterate to a fixpoint to collapse the whole chain.
func TestOptMergeBlocksIsAFixpoint(t *testing.T) {
	m := NewModule("chain")
	b := &Builder{Mod: m}
	i64 := b.Type("i64")
	b.NewFunc("f", nil, nil, false)
	entry := b.CurrentBlock()
	b.EmitInt(OpConst, i64, 1, "")
	var chain []BlockID
	for i := 0; i < 3; i++ {
		bid := b.NewBlock("b")
		chain = append(chain, bid)
		b.SetBlock(bid)
		b.EmitInt(OpConst, i64, int64(i+2), "")
	}
	b.Terminate(OpReturn, nil, nil, "")
	for i := len(chain) - 1; i > 0; i-- {
		b.SetBlock(chain[i-1])
		b.Terminate(OpBr, nil, []BlockID{chain[i]}, "")
	}
	b.SetBlock(entry)
	b.Terminate(OpBr, nil, []BlockID{chain[0]}, "")
	f := m.Func(m.FuncByName["f"])

	var st OptStats
	m.optMergeBlocks(f, &st)
	if st.Merged != 3 {
		t.Errorf("merged %d blocks, want 3 — the pass did not reach a fixpoint", st.Merged)
	}
	if len(f.Blocks) != 1 {
		t.Fatalf("the chain did not collapse to one block: %v", f.Blocks)
	}
	if got := len(m.Block(f.Blocks[0]).Insts); got != 4 {
		t.Errorf("the collapsed block holds %d instructions, want 4", got)
	}
	if d := m.optVerifyOperands(); len(d) != 0 {
		t.Errorf("collapsing the chain left a broken module: %v", d)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The new rules end-to-end through the real pipeline.
// ─────────────────────────────────────────────────────────────────────────────

// lowerThenOptimize lowers with the switch OFF and then runs OptimizeMIR
// directly, which is the only way to read the OptStats a test wants to assert
// on. Running the pass on already-analysed IR is a harsher input than the real
// pipeline gives it, so a pass that is sound here is sound there.
func lowerThenOptimize(t *testing.T, src string, lvl int) (*Module, OptStats) {
	t.Helper()
	off := lowerWithOpt(t, src, "0")
	if d := off.optVerifyOperands(); len(d) != 0 {
		t.Fatalf("the fixture is already broken before optimisation: %v", d)
	}
	st := off.OptimizeMIR(lvl)
	return off, st
}

// TestOptRulesFireOnLoweredIR is the anti-vacuity test for the new rules as a
// group: on real lowered IR the pass must actually delete copies and merge
// blocks. Without it, every table above could pass while the pass was never
// reached by the driver.
func TestOptRulesFireOnLoweredIR(t *testing.T) {
	off := lowerWithOpt(t, optLoopSrc, "0")
	offInsts, offBlocks := optInstCount(off), optBlockCount(off)

	on := lowerWithOpt(t, optLoopSrc, "2")
	if d := on.optVerifyOperands(); len(d) != 0 {
		t.Fatalf("level 2 left codegen-dependent values without a producer:\n  %s",
			strings.Join(d, "\n  "))
	}
	if got := optInstCount(on); got > offInsts {
		t.Errorf("the pass grew the program: %d instructions, was %d", got, offInsts)
	}
	if got := optBlockCount(on); got > offBlocks {
		t.Errorf("the pass added blocks: %d, was %d", got, offBlocks)
	}

	// The stats must show the new rules working, not just the old folding.
	_, st := lowerThenOptimize(t, optLoopSrc, OptCFG)
	if st.Copies == 0 && st.Merged == 0 && st.Idents == 0 {
		t.Errorf("not one copy was propagated, block merged or identity rewritten "+
			"on the loop fixture — the new rules are unreachable: %+v", st)
	}
	if st.InstrSaved <= 0 {
		t.Errorf("the pass saved no instructions at all: %+v", st)
	}
}

// TestOptKeepsLoweredProgramsVerifiable runs the verifier over a spread of real
// fixtures at level 2. The verifier is the oracle the whole design leans on, so
// it must hold on every shape the corpus actually contains, not just the one
// the loop fixture happens to have.
func TestOptKeepsLoweredProgramsVerifiable(t *testing.T) {
	for _, src := range []string{foldSrc, optLoopSrc, optSoleProducerSrc, optMoveIntoSrc} {
		on := lowerWithOpt(t, src, "2")
		if d := on.optVerifyOperands(); len(d) != 0 {
			t.Errorf("level 2 broke a lowered program:\n  %s", strings.Join(d, "\n  "))
		}
	}
}
