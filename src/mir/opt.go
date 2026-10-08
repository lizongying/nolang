package mir

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// In-compiler MIR optimiser ("nolang's own opt")
//
// This is the compiler's OWN optimisation pass, as opposed to the external
// `opt` from LLVM that buildLLVMInternal runs over the emitted .ll. It rewrites
// the MIR before codegen, so the instruction stream that reaches LLVM is
// already partly optimised — which is what makes the two comparable on an
// identical footing: `mir` vs `opt -O2` is "how much does each optimiser
// remove from the same program".
//
// SWITCHES (all default to OFF — the pass is strictly opt-in):
//
//	NOLANG_MIR_OPT=0|off      disabled (default)
//	NOLANG_MIR_OPT=1|on       level 1: local constant folding, integer identity
//	                          rewriting, per-block copy propagation and dead
//	                          scalar elimination
//	NOLANG_MIR_OPT=2          level 2: level 1 + constant-branch folding,
//	                          unreachable-block removal, empty-block threading
//	                          and single-predecessor block merging
//	NOLANG_MIR_OPT_STATS=1    print a one-line summary per compilation
//	NOLANG_MIR_OPT_VERIFY=1   re-check the operand/definition invariant after
//	                          the pass and report a violation on stderr
//
// The `no build|run|test -opt[=N]` CLI flag simply exports NOLANG_MIR_OPT, so
// the flag and the variable are one switch with two spellings.
//
// WHERE IT RUNS, AND WHY THERE. LowerHIR calls OptimizeMIR after
// demoteUnsafeSliceViews and BEFORE Analyze. Analyze decides ownership
// (enumOwnsPayload / spawnArgMoves / spawnArgRetains / drop placement); running
// the optimiser after it would leave every one of those decisions describing an
// instruction stream that no longer exists. Running it before means Analyze
// sees the optimised program and its answers are correct by construction.
//
// WHAT IT DOES, AND WHAT IT DELIBERATELY DOES NOT. Every rule here is
// ownership-preserving: it never touches an owned value, never removes an
// instruction that can trap or allocate, and folding mutates an instruction IN
// PLACE so its destination keeps its identity and no consumer is left with a
// dangling reference.
//
// Copy propagation is the one rule that rewrites an operand into a DIFFERENT
// value id, and it is restricted to a single basic block for exactly that
// reason. Within a block the execution order is known, so the source can be
// proved available and unmodified afterwards, and every reader of the deleted
// copy proved to live in that same block; a cross-block version needs a
// dominance analysis. Inlining, store forwarding and value numbering are left
// to the LLVM `opt` at the back end.
// ─────────────────────────────────────────────────────────────────────────────

// MIR optimiser levels.
const (
	// OptOff — the pass does not run.
	OptOff = 0
	// OptFold — constant folding, integer identities, per-block copy
	// propagation and dead scalar-instruction removal.
	OptFold = 1
	// OptCFG — OptFold plus control-flow cleanup: constant branches,
	// unreachable blocks, empty-block threading and block merging.
	OptCFG = 2
)

// MIROptLevel returns the level requested by NOLANG_MIR_OPT. Anything that is
// not a recognised spelling of "on"/"off"/1/2 is treated as off, so a typo can
// never silently enable an optimisation.
func MIROptLevel() int {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("NOLANG_MIR_OPT")))
	if v == "" {
		return OptOff
	}
	switch v {
	case "0", "off", "false", "no", "none":
		return OptOff
	case "on", "true", "yes":
		return OptFold
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return OptOff
	}
	if n < 0 {
		return OptOff
	}
	if n > OptCFG {
		n = OptCFG
	}
	return n
}

// OptStats records what one OptimizeMIR run changed. It exists so the pass can
// be measured (and so a "0 changes" result can be distinguished from "the pass
// never ran" — the two look identical in the emitted IR).
type OptStats struct {
	Funcs      int // non-extern functions visited
	FoldArith  int // arithmetic/bitwise/shift instructions folded to a constant
	FoldCmp    int // comparison instructions folded to a constant
	Idents     int // integer identities rewritten to a copy of an operand
	Copies     int // scalar copies deleted by copy propagation
	DeadInsts  int // pure scalar instructions deleted (unused, non-owned)
	ConstBrs   int // OpCondBr with a constant condition replaced by OpBr
	Unreach    int // blocks removed as unreachable
	Threaded   int // empty blocks removed by branch threading
	Merged     int // blocks merged into their single predecessor
	InstrSaved int // instructions dropped by the CFG phases (unreachable blocks)
}

// Empty reports whether the pass changed nothing at all.
func (s OptStats) Empty() bool {
	return s.FoldArith == 0 && s.FoldCmp == 0 && s.Idents == 0 &&
		s.Copies == 0 && s.DeadInsts == 0 && s.ConstBrs == 0 &&
		s.Unreach == 0 && s.Threaded == 0 && s.Merged == 0
}

func (s OptStats) String() string {
	return fmt.Sprintf("funcs=%d folded=%d (arith=%d cmp=%d) ident=%d copies=%d dead=%d "+
		"cfg{condbr=%d unreachable=%d(%d insts) threaded=%d merged=%d}",
		s.Funcs, s.FoldArith+s.FoldCmp, s.FoldArith, s.FoldCmp, s.Idents,
		s.Copies, s.DeadInsts,
		s.ConstBrs, s.Unreach, s.InstrSaved, s.Threaded, s.Merged)
}

// OptimizeMIR runs the in-compiler optimiser over every non-extern function.
// level <= OptOff is a no-op, so the caller can invoke it unconditionally.
func (m *Module) OptimizeMIR(level int) OptStats {
	var st OptStats
	if level <= OptOff {
		return st
	}
	// Definition counts are MODULE-wide on purpose: a value id is a
	// module-global slot, and counting per function would let one function
	// delete a definition another function still reads. The reference counts
	// serve the same purpose for copy propagation, which must prove that every
	// reader of a copy's destination lives in the copy's own block.
	defs, _ := m.optDefUse()
	refs := m.optRefCounts()
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		st.Funcs++
		m.optimizeFunc(f, level, defs, refs, &st)
	}
	if os.Getenv("NOLANG_MIR_OPT_VERIFY") != "" {
		for _, d := range m.optVerifyOperands() {
			fmt.Fprintf(os.Stderr, "[miropt] VIOLATION %s\n", d)
		}
	}
	return st
}

// optForEachUse calls fn for every value an instruction READS. It is the single
// definition of "use" in this file, shared by the definition/use counter and by
// the block-removal guard below, so the two can never drift apart — a guard that
// counted uses differently from the counter it consults would silently license
// exactly the deletion it exists to prevent.
//
// The one position excluded is Args[1] of a move-like instruction, which is the
// DESTINATION of a move-into, not an operand (see moveLike/moveSrc/moveDst).
func optForEachUse(inst *Inst, fn func(ValueID)) {
	if inst == nil {
		return
	}
	for ai, a := range inst.Args {
		if ai == 1 && moveLike(inst) {
			continue
		}
		fn(a)
	}
	if inst.Callee > NoVal {
		fn(inst.Callee)
	}
}

// optDefUse computes, module-wide, how many times each value is DEFINED and how
// many times it is USED.
//
// "Defined" follows definedValue (Dst, or the move-into destination in Args[1]),
// i.e. the same predicate codegen uses to build its defInst map — a MIR value is
// a VARIABLE, so a second definition must invalidate the first. A definition
// count of 1 is what licenses "this value is that one instruction's constant".
//
// "Used" is deliberately over-inclusive: every operand position counts except
// the one position that is provably a DESTINATION (Args[1] of a move-like
// instruction). Over-counting only makes the pass more conservative; the
// dangerous direction is under-counting, which would delete a live definition.
func (m *Module) optDefUse() (defs, uses map[ValueID]int) {
	defs = map[ValueID]int{}
	uses = map[ValueID]int{}
	addUse := func(v ValueID) {
		if v > NoVal && int(v) < len(m.Values) {
			uses[v]++
		}
	}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				if d := definedValue(inst); d > NoVal {
					defs[d]++
				}
				optForEachUse(inst, addUse)
			}
			if blk.Term != nil {
				for _, a := range blk.Term.Args {
					addUse(a)
				}
			}
		}
	}
	return defs, uses
}

// optForEachRef calls fn for every value id an instruction NAMES — operands,
// the callee, and the move-into destination at Args[1].
//
// This is deliberately broader than optForEachUse. "Use" means "an operand this
// instruction reads", and for that purpose Args[1] of a move-into is a
// destination and must be excluded. But the question the block-removal guard
// asks is different: "would codegen still be able to find a storage slot for
// this value?" codegen allocates slots from the defining instruction
// (defInst/valSlot) and then reads them by plain map lookup in emitMove:
//
//	dstSlot := c.valSlot[dstVal]
//	if dstSlot == "" { return fmt.Errorf("move slot") }
//
// so a value that is ONLY ever a move destination still needs its definition to
// survive. Counting it as a "use" would be wrong; counting it as a REFERENCE is
// exactly right. (tests/match-2.no, pipeline-value.no, simple-match.no and
// simple-match2.no each failed with a bare `EmitLLVM: move slot` until this
// distinction was made.)
func optForEachRef(inst *Inst, fn func(ValueID)) {
	if inst == nil {
		return
	}
	for _, a := range inst.Args {
		fn(a)
	}
	if inst.Callee > NoVal {
		fn(inst.Callee)
	}
}

// optRefCounts counts, module-wide, how many times each value id is NAMED by an
// instruction or terminator (see optForEachRef). Global initialisers count as a
// reference from outside every block, because a global's slot is looked up the
// same way and its initialiser is not guaranteed to sit in the entry block.
func (m *Module) optRefCounts() map[ValueID]int {
	refs := map[ValueID]int{}
	add := func(v ValueID) {
		if v > NoVal && int(v) < len(m.Values) {
			refs[v]++
		}
	}
	for _, g := range m.Globals {
		add(g.Init)
	}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				optForEachRef(m.Inst(iid), add)
			}
			if blk.Term != nil {
				for _, a := range blk.Term.Args {
					add(a)
				}
			}
		}
	}
	return refs
}

// optSlotDefs counts, module-wide, how many instructions can CREATE a storage
// slot for each value. That is exactly the instructions with Dst > NoVal.
//
// This is NOT the same as definedValue. definedValue returns Args[1] for a
// move-like instruction, which is correct for its own purpose — a MIR value is a
// variable, so a move-into reassigns it — but a move-into does not create the
// slot it writes to. emitMove looks the destination slot up and fails outright
// if it is missing:
//
//	dstSlot := c.valSlot[dstVal]
//	if dstSlot == "" { return fmt.Errorf("move slot") }
//
// So counting a move destination as a definition lets the block-removal guard
// believe a value still has a producer when in fact only its consumers remain.
// tests/simple-match.no is the minimal repro: value 20 is created by a single
// `const dst=20:str` in one arm's block, and merely re-targeted by `move 0
// args=[25 20]` / `args=[23 20]` in the other two — so the old guard saw three
// "definitions", kept none of them, and codegen died with a bare `move slot`.
func (m *Module) optSlotDefs() map[ValueID]int {
	out := map[ValueID]int{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				if inst := m.Inst(iid); inst != nil && inst.Dst > NoVal {
					out[inst.Dst]++
				}
			}
		}
	}
	return out
}

// optBlockHoldsSoleLiveDef reports whether removing blk would leave some value
// with no surviving SLOT-CREATING definition while it is still referenced from
// OUTSIDE blk.
//
// This guard exists because hir2mir does not always materialise a value in the
// block that reads it: a string constant can be created in one arm's block and
// cloned from a different block entirely. Codegen tolerates that — a constant
// has no ordering dependency, so it is materialised wherever it is first needed
// — but it does NOT tolerate the creating block disappearing, and then fails the
// build with
//
//	value N has LLVM type %str-long but no storage slot — nothing produced it
//
// which is exactly what happened to tests/minimal-ok.no and the other match/str
// tests before this guard was added.
//
// The test is on surviving definitions, not on a module-wide count of exactly 1:
// a value may legitimately have several producers, and the question is only
// whether at least one of them outlives blk.
//
// References inside blk are subtracted, because a dead block that creates and
// consumes a value entirely within itself (the ubiquitous `const` + `drop`
// shape) is still perfectly removable; treating those as live would defeat the
// pass on exactly the code it is meant to clean up.
func (m *Module) optBlockHoldsSoleLiveDef(blk *Block, slotDefs, refs map[ValueID]int) bool {
	if blk == nil {
		return false
	}
	defsIn := map[ValueID]int{}
	refsIn := map[ValueID]int{}
	for _, iid := range blk.Insts {
		inst := m.Inst(iid)
		if inst == nil {
			continue
		}
		if inst.Dst > NoVal {
			defsIn[inst.Dst]++
		}
		optForEachRef(inst, func(v ValueID) { refsIn[v]++ })
	}
	if blk.Term != nil {
		for _, a := range blk.Term.Args {
			refsIn[a]++
		}
	}
	for d, inHere := range defsIn {
		if slotDefs[d]-inHere > 0 {
			continue // a slot-creating definition survives elsewhere
		}
		if refs[d]-refsIn[d] > 0 {
			return true // referenced from outside, and this is the only producer
		}
	}
	return false
}

// optVerifyOperands re-checks the invariants a rewriting pass can break, and
// returns one message per violation (empty means clean).
//
// It is both the NOLANG_MIR_OPT_VERIFY=1 debug aid and the oracle the unit tests
// assert against, so the same definition of "correct" governs the diagnostic and
// the test. Two invariants are checked, and the second one is the one this file
// actually got wrong first:
//
//  1. Every operand must still be supplied — by a definition, a parameter or a
//     global. A dangling operand is a hard codegen failure ("value id N has no
//     slot"), so it is worth catching with a readable message.
//
//  2. Every REFERENCED value must still have a surviving SLOT-CREATING
//     definition (an instruction with Dst == that value). This is stronger than
//     (1) because codegen needs a slot for a value even when nothing reads it:
//     emitMove looks the move destination's slot up and fails with a bare
//     "move slot" if the instruction that created it is gone. Deleting an
//     unreachable block is what used to break this, in two different ways —
//     the block held the only `const` for a string another block cloned, or it
//     held the only `const` for a variable two other blocks moved into.
//
// Reported rather than returned as an error, because the pass has already
// mutated the module by the time it runs; the caller decides what to do.
func (m *Module) optVerifyOperands() []string {
	slotDefs := m.optSlotDefs()
	params := map[ValueID]bool{}
	globals := map[ValueID]bool{}
	for _, g := range m.Globals {
		globals[g.Init] = true
	}
	for i := range m.Funcs {
		for _, p := range m.Funcs[i].Params {
			params[p] = true
		}
	}
	// supplied = "something other than an instruction can stand in for this
	// value": a parameter or a global initialiser.
	supplied := func(v ValueID) bool { return params[v] || globals[v] }

	var diags []string
	note := func(format string, args ...any) {
		diags = append(diags, fmt.Sprintf(format, args...))
	}

	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				// (1) operands must be supplied.
				optForEachUse(inst, func(v ValueID) {
					if v <= NoVal || int(v) >= len(m.Values) || supplied(v) || slotDefs[v] > 0 {
						return
					}
					note("func %s inst %d (%s): operand %d has no definition and is not a param/global",
						f.Name, iid, inst.Op, v)
				})
				// (2) everything referenced must have a slot.
				optForEachRef(inst, func(v ValueID) {
					if v <= NoVal || int(v) >= len(m.Values) || supplied(v) || slotDefs[v] > 0 {
						return
					}
					note("func %s inst %d (%s): value %d is referenced but has no slot-creating definition",
						f.Name, iid, inst.Op, v)
				})
			}
			if blk.Term != nil {
				for _, a := range blk.Term.Args {
					if a <= NoVal || int(a) >= len(m.Values) || supplied(a) || slotDefs[a] > 0 {
						continue
					}
					note("func %s block %d terminator: value %d is referenced but has no slot-creating definition",
						f.Name, bid, a)
				}
			}
		}
	}
	return diags
}

// ─── level 1: constant folding ───────────────────────────────────────────────

// optConst is a scalar constant the folder can model exactly.
type optConst struct {
	llvm string // "i1" | "i32" | "i64" | "double"
	i    int64  // integer/bool payload
	f    float64
}

// optScalarLLVM maps a scalar MIR type to the LLVM type codegen's llvmTypeOf
// would produce, and reports ok=false for every type the folder cannot model
// byte-for-byte. The exclusions are all deliberate:
//
//   - owned values (str / vec / map / ?owned) and aggregates — a constant there
//     is a pointer or a buffer, not a number, and removing one would leak.
//   - `i128`/`u128` — the payload does not fit inst.Int (emitConst reads
//     IntBig), so the folder would silently truncate.
//   - `i8` (byte/u8/i8) — emitConst has no i8 case and falls through to
//     `store i8 zeroinitializer`, so an i8 OpConst materialises as 0 no matter
//     what inst.Int says. Folding one would invent a wrong value.
//   - KindOption / KindArray / KindStruct / KindFunc / KindVoid / KindPtr —
//     not scalars; option arithmetic also goes through the overflow-wrapping
//     path (emitArith's wrapResult), whose semantics this pass does not model.
//
// `i16`/`i32`/`u16`/`u32` all lower to i64 (llvmTypeOf's KindInt branch returns
// i64 for everything that is not byte/i8/i128), so they are modelled at 64 bits,
// which is exactly what the emitted IR does.
func optScalarLLVM(t *Type) (string, bool) {
	if t == nil || t.Owned {
		return "", false
	}
	switch t.Kind {
	case KindBool:
		return "i1", true
	case KindChar:
		return "i32", true
	case KindFloat:
		return "double", true
	case KindInt:
		switch t.Raw {
		case "byte", "u8", "i8", "i128", "u128":
			return "", false
		}
		return "i64", true
	}
	return "", false
}

// optIntWidth returns the bit width of an integer LLVM type this file folds.
func optIntWidth(lt string) int {
	switch lt {
	case "i1":
		return 1
	case "i32":
		return 32
	case "i64":
		return 64
	}
	return 0
}

// optTrunc narrows a 64-bit two's-complement value to width bits and
// sign-extends it back. That is exactly what the emitted IR does: the operands
// are coerced to the operation width, the LLVM instruction wraps at that width,
// and the result is stored as a signed value of that width.
func optTrunc(v int64, width int) int64 {
	switch width {
	case 1:
		return v & 1
	case 32:
		return int64(int32(uint32(v)))
	default:
		return v
	}
}

// optUint reinterprets a value as unsigned at the given width.
func optUint(v int64, width int) uint64 {
	switch width {
	case 1:
		return uint64(v) & 1
	case 32:
		return uint64(uint32(v))
	default:
		return uint64(v)
	}
}

// optValueType resolves a value's MIR type the way codegen's ptype does: the
// function-local override first, then the module value table.
func (m *Module) optValueType(f *Function, v ValueID) *Type {
	if f != nil {
		if t, ok := f.LocalTypes[v]; ok {
			if ty := m.Type(t); ty != nil {
				return ty
			}
		}
	}
	if val := m.Value(v); val != nil {
		if ty := m.Type(val.Type); ty != nil {
			return ty
		}
	}
	return nil
}

// optConstOf returns the constant a value provably holds, if any. It requires
// the value to be defined EXACTLY ONCE module-wide by an OpConst, to not be a
// parameter, and to have a scalar type the folder models.
func (m *Module) optConstOf(f *Function, v ValueID, defs map[ValueID]int, constOf map[ValueID]InstID) (optConst, bool) {
	if defs[v] != 1 {
		return optConst{}, false
	}
	for _, p := range f.Params {
		if p == v {
			return optConst{}, false
		}
	}
	iid, ok := constOf[v]
	if !ok {
		return optConst{}, false
	}
	inst := m.Inst(iid)
	if inst == nil || inst.Op != OpConst {
		return optConst{}, false
	}
	lt, ok := optScalarLLVM(m.optValueType(f, v))
	if !ok {
		return optConst{}, false
	}
	return optConst{llvm: lt, i: inst.Int, f: inst.Flt}, true
}

// optFoldableArith lists the binary integer ops whose LLVM lowering this file
// models exactly when the destination is a plain scalar (i.e. NOT an option, so
// emitArith's wrapResult path is not taken and the op is a bare wrapping
// instruction).
func optFoldableArith(op Op) bool {
	switch op {
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpUDiv, OpUMod,
		OpBitAnd, OpBitOr, OpXor, OpShl, OpShr,
		OpAnd, OpOr:
		return true
	}
	return false
}

// optFoldableCmp lists the comparisons whose LLVM predicate this file models.
// OpULt/OpULe/OpUGt/OpUGe are included because emitCmp maps them to ult/ule/
// ugt/uge, and the unsigned reading is what the folder must use.
func optFoldableCmp(op Op) bool {
	switch op {
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe,
		OpULt, OpULe, OpUGt, OpUGe:
		return true
	}
	return false
}

// optEvalIntBinary evaluates a binary integer op the way the emitted LLVM
// instruction would, at the given width. ok=false means "not foldable" — either
// the op is not an integer binary op, or the specific operand values hit an
// LLVM UB case that must be left to the runtime (division by zero, INT_MIN/-1,
// an out-of-range shift amount). Folding those would turn a runtime trap or an
// err-option into a compile-time constant, which is a semantic change.
func optEvalIntBinary(op Op, a, b int64, width int) (int64, bool) {
	switch op {
	case OpAdd:
		return optTrunc(a+b, width), true
	case OpSub:
		return optTrunc(a-b, width), true
	case OpMul:
		return optTrunc(a*b, width), true
	case OpDiv:
		if b == 0 || optIsMinOverNeg1(a, b, width) {
			return 0, false
		}
		return optTrunc(a/b, width), true
	case OpMod:
		if b == 0 || optIsMinOverNeg1(a, b, width) {
			return 0, false
		}
		return optTrunc(a%b, width), true
	case OpUDiv:
		if b == 0 {
			return 0, false
		}
		return optTrunc(int64(optUint(a, width)/optUint(b, width)), width), true
	case OpUMod:
		if b == 0 {
			return 0, false
		}
		return optTrunc(int64(optUint(a, width)%optUint(b, width)), width), true
	case OpBitAnd:
		return optTrunc(a&b, width), true
	case OpBitOr:
		return optTrunc(a|b, width), true
	case OpXor:
		return optTrunc(a^b, width), true
	case OpShl:
		if b < 0 || b >= int64(width) {
			return 0, false
		}
		return optTrunc(a<<uint(b), width), true
	case OpShr:
		// nolang `>>` is a LOGICAL shift (emitBitwise emits lshr).
		if b < 0 || b >= int64(width) {
			return 0, false
		}
		return optTrunc(int64(optUint(a, width)>>uint(b)), width), true
	case OpAnd:
		if width != 1 {
			return 0, false
		}
		return a & b & 1, true
	case OpOr:
		if width != 1 {
			return 0, false
		}
		return (a | b) & 1, true
	}
	return 0, false
}

// optIsMinOverNeg1 reports the (INT_MIN / -1) case, which is UB for sdiv/srem
// in LLVM and therefore must not be folded.
func optIsMinOverNeg1(a, b int64, width int) bool {
	if b != -1 {
		return false
	}
	switch width {
	case 32:
		return int32(uint32(a)) == math.MinInt32
	case 64:
		return a == math.MinInt64
	}
	return false
}

// optEvalCmp evaluates a comparison the way emitCmp lowers it: signed icmp for
// OpLt/OpLe/OpGt/OpGe, unsigned for the OpU* variants, and the fcmp ordered
// predicates for double operands (oeq / une / olt / ole / ogt / oge — note
// `!=` is UNORDERED so `NaN != NaN` stays true).
func optEvalCmp(op Op, a, b optConst) (int64, bool) {
	b2i := func(x bool) int64 {
		if x {
			return 1
		}
		return 0
	}
	if a.llvm == "double" {
		switch op {
		case OpEq:
			return b2i(a.f == b.f), true
		case OpNe:
			return b2i(a.f != b.f), true
		case OpLt:
			return b2i(a.f < b.f), true
		case OpLe:
			return b2i(a.f <= b.f), true
		case OpGt:
			return b2i(a.f > b.f), true
		case OpGe:
			return b2i(a.f >= b.f), true
		}
		// The unsigned variants have no fcmp predicate at all; emitCmp would
		// emit an invalid `fcmp  double`, so there is nothing to fold.
		return 0, false
	}
	w := optIntWidth(a.llvm)
	if w == 0 {
		return 0, false
	}
	switch op {
	case OpEq:
		return b2i(a.i == b.i), true
	case OpNe:
		return b2i(a.i != b.i), true
	case OpLt:
		return b2i(a.i < b.i), true
	case OpLe:
		return b2i(a.i <= b.i), true
	case OpGt:
		return b2i(a.i > b.i), true
	case OpGe:
		return b2i(a.i >= b.i), true
	case OpULt:
		return b2i(optUint(a.i, w) < optUint(b.i, w)), true
	case OpULe:
		return b2i(optUint(a.i, w) <= optUint(b.i, w)), true
	case OpUGt:
		return b2i(optUint(a.i, w) > optUint(b.i, w)), true
	case OpUGe:
		return b2i(optUint(a.i, w) >= optUint(b.i, w)), true
	}
	return 0, false
}

// optTryFold rewrites inst in place to an OpConst when it is a scalar operation
// on scalar constants.
//
// The rewrite is IN PLACE (same InstID, same Dst value id, Args cleared), which
// is the whole reason this pass cannot dangle a reference: every consumer of the
// destination keeps pointing at a value that is still defined, only now by a
// constant instead of an arithmetic instruction.
//
// isCmp distinguishes the two stats counters and is only used for reporting.
func (m *Module) optTryFold(f *Function, inst *Inst, defs map[ValueID]int, constOf map[ValueID]InstID) (folded, isCmp bool) {
	if inst == nil || inst.Dst <= NoVal || len(inst.Args) == 0 {
		return false, false
	}
	dstLT, ok := optScalarLLVM(m.optValueType(f, inst.Dst))
	if !ok {
		return false, false
	}
	switch {
	case optFoldableCmp(inst.Op):
		// A comparison's result is always i1 and its operands carry the width
		// (the `le dst=8:bool args=[i64 i64]` shape). Require both operands to
		// have the SAME width so emitCmp performs no coercion this file would
		// have to model.
		if dstLT != "i1" || len(inst.Args) != 2 {
			return false, false
		}
		a, okA := m.optConstOf(f, inst.Args[0], defs, constOf)
		b, okB := m.optConstOf(f, inst.Args[1], defs, constOf)
		if !okA || !okB || a.llvm != b.llvm {
			return false, false
		}
		res, ok := optEvalCmp(inst.Op, a, b)
		if !ok {
			return false, false
		}
		m.optSetConst(inst, dstLT, res, 0)
		return true, true

	case inst.Op == OpNeg:
		if len(inst.Args) != 1 {
			return false, false
		}
		a, okA := m.optConstOf(f, inst.Args[0], defs, constOf)
		if !okA || a.llvm != dstLT {
			return false, false
		}
		if dstLT == "double" {
			m.optSetConst(inst, dstLT, 0, -a.f)
			return true, false
		}
		w := optIntWidth(dstLT)
		if w == 0 || w == 1 {
			return false, false
		}
		// emitNeg emits `sub resLT 0, v` (a bare wrapping subtraction) when the
		// destination is not an option.
		m.optSetConst(inst, dstLT, optTrunc(-a.i, w), 0)
		return true, false

	case inst.Op == OpNot:
		// emitNot emits `xor lt 1, v`; only meaningful for the i1 boolean.
		if len(inst.Args) != 1 || dstLT != "i1" {
			return false, false
		}
		a, okA := m.optConstOf(f, inst.Args[0], defs, constOf)
		if !okA || a.llvm != "i1" {
			return false, false
		}
		m.optSetConst(inst, dstLT, (a.i^1)&1, 0)
		return true, false

	case optFoldableArith(inst.Op):
		if len(inst.Args) != 2 {
			return false, false
		}
		a, okA := m.optConstOf(f, inst.Args[0], defs, constOf)
		b, okB := m.optConstOf(f, inst.Args[1], defs, constOf)
		// Both operands must share the destination's width: emitArith coerces
		// each operand to resLT, and a mixed-width fold would have to reproduce
		// that coercion.
		if !okA || !okB || a.llvm != dstLT || b.llvm != dstLT {
			return false, false
		}
		if dstLT == "double" {
			if inst.Op == OpAdd {
				m.optSetConst(inst, dstLT, 0, a.f+b.f)
			} else if inst.Op == OpSub {
				m.optSetConst(inst, dstLT, 0, a.f-b.f)
			} else if inst.Op == OpMul {
				m.optSetConst(inst, dstLT, 0, a.f*b.f)
			} else if inst.Op == OpDiv {
				m.optSetConst(inst, dstLT, 0, a.f/b.f)
			} else if inst.Op == OpMod {
				// emitArith emits `frem` for a double OpMod, which is C fmod.
				m.optSetConst(inst, dstLT, 0, math.Mod(a.f, b.f))
			} else {
				return false, false
			}
			return true, false
		}
		w := optIntWidth(dstLT)
		if w == 0 {
			return false, false
		}
		res, ok := optEvalIntBinary(inst.Op, a.i, b.i, w)
		if !ok {
			return false, false
		}
		m.optSetConst(inst, dstLT, res, 0)
		return true, false
	}
	return false, false
}

// optSetConst turns an already-allocated instruction into an OpConst with the
// given payload. Type/ID/Dst/Block/Line are preserved: the destination must keep
// its identity and its type (emitConst dispatches on the LLVM type of Dst).
func (m *Module) optSetConst(inst *Inst, dstLT string, i int64, f float64) {
	inst.Op = OpConst
	inst.Args = nil
	inst.Sym = ""
	inst.Callee = NoVal
	inst.Results = nil
	inst.MovesArg = false
	inst.BufClone = false
	if dstLT == "double" {
		inst.Flt = f
		inst.Int = 0
		inst.IntBig = ""
		return
	}
	inst.Int = i
	inst.Flt = 0
	inst.IntBig = ""
}

// ─── level 1: integer identities ─────────────────────────────────────────────

// optTryIdentity rewrites an integer binary operation that provably reduces to
// one of its operands into a COPY of that operand.
//
// Only the operand-producing half lives here; the half that produces a CONSTANT
// is handled by optTryIdentityConst. Splitting them keeps each function's
// precondition list short enough to check by reading it.
//
// Every law below is exact for two's-complement integers at the destination's
// width, which is what the emitted instruction computes (a bare wrapping op —
// option-typed arithmetic never reaches here because optScalarLLVM rejects
// KindOption).
//
// The `copy` result is only offered when the source is assigned AT MOST ONCE
// module-wide. A MIR value is a variable: if it can be reassigned between this
// instruction and a later read of the result, rewriting that read to name the
// source would read the NEW value instead of the one this instruction saw.
// Requiring a single definition removes the whole question.
//
// The operand that is NOT replaced is ignored, so `x * 1` and `1 * x` are the
// same rule with the sides swapped — except for division, where `1 / x` is
// emphatically not `x`.
func (m *Module) optTryIdentity(f *Function, inst *Inst, defs map[ValueID]int, constOf map[ValueID]InstID) bool {
	dstLT, a, b, ok := m.optIdentityOperands(f, inst)
	if !ok {
		return false
	}
	isZero := func(v ValueID) bool { c, k := m.optConstOf(f, v, defs, constOf); return k && c.i == 0 }
	isOne := func(v ValueID) bool { c, k := m.optConstOf(f, v, defs, constOf); return k && c.i == 1 }
	isAllOnes := func(v ValueID) bool {
		c, k := m.optConstOf(f, v, defs, constOf)
		if !k {
			return false
		}
		return optTrunc(c.i, optIntWidth(dstLT)) == optTrunc(-1, optIntWidth(dstLT))
	}
	w := optIntWidth(dstLT)

	// copyOf rewrites the instruction into `dst = copy of v`, which the copy
	// propagation pass then removes by pointing dst's readers straight at v.
	copyOf := func(v ValueID) bool {
		if defs[v] > 1 {
			return false
		}
		if _, ok := optScalarLLVM(m.optValueType(f, v)); !ok {
			return false
		}
		m.optSetCopy(f, inst, v)
		return true
	}

	switch inst.Op {
	case OpAdd:
		if isZero(a) {
			return copyOf(b)
		}
		if isZero(b) {
			return copyOf(a)
		}
	case OpSub:
		if isZero(b) {
			return copyOf(a)
		}
	case OpMul:
		if isOne(a) {
			return copyOf(b)
		}
		if isOne(b) {
			return copyOf(a)
		}
	case OpDiv, OpUDiv:
		// `1 / x` is not `x`; only the divisor may be 1. `x / -1` is skipped
		// entirely: INT_MIN / -1 is UB, and optEvalIntBinary refuses it for the
		// same reason.
		if isOne(b) {
			return copyOf(a)
		}
	case OpShl, OpShr:
		if isZero(b) {
			return copyOf(a)
		}
	case OpXor:
		// `x ^ 0` is x. The other Xor law (`x ^ x` = 0) mentions no surviving
		// operand, so it lives in optTryIdentityConst.
		if isZero(a) {
			return copyOf(b)
		}
		if isZero(b) {
			return copyOf(a)
		}
	case OpBitAnd:
		if a == b || isAllOnes(b) {
			return copyOf(a)
		}
		if isAllOnes(a) {
			return copyOf(b)
		}
	case OpBitOr:
		if a == b || isZero(b) {
			return copyOf(a)
		}
		if isZero(a) {
			return copyOf(b)
		}
	case OpAnd:
		// i1 only: `&&` on wider integers is not a bitwise and.
		if w != 1 {
			return false
		}
		if a == b || isOne(b) {
			return copyOf(a)
		}
		if isOne(a) {
			return copyOf(b)
		}
	case OpOr:
		if w != 1 {
			return false
		}
		if a == b || isZero(b) {
			return copyOf(a)
		}
		if isZero(a) {
			return copyOf(b)
		}
	}
	return false
}

// optTryIdentityConst is the other half: operations whose result is a constant
// no matter what the operands are. None of these mention the surviving operand,
// so unlike optTryIdentity they need no single-definition condition on it — and
// the same-value laws here (`x - x`, `x ^ x`) are exact even for a variable that
// is reassigned, because both reads happen at this one instruction.
func (m *Module) optTryIdentityConst(f *Function, inst *Inst, defs map[ValueID]int, constOf map[ValueID]InstID) bool {
	dstLT, a, b, ok := m.optIdentityOperands(f, inst)
	if !ok {
		return false
	}
	w := optIntWidth(dstLT)
	isZero := func(v ValueID) bool { c, k := m.optConstOf(f, v, defs, constOf); return k && c.i == 0 }
	isOne := func(v ValueID) bool { c, k := m.optConstOf(f, v, defs, constOf); return k && c.i == 1 }
	isAllOnes := func(v ValueID) bool {
		c, k := m.optConstOf(f, v, defs, constOf)
		if !k {
			return false
		}
		return optTrunc(c.i, w) == optTrunc(-1, w)
	}
	// setConst writes a constant of the destination's own width.
	setConst := func(k int64) bool {
		m.optSetConst(inst, dstLT, optTrunc(k, w), 0)
		return true
	}
	switch inst.Op {
	case OpSub, OpXor:
		if a == b {
			return setConst(0)
		}
	case OpMul, OpBitAnd:
		if isZero(a) || isZero(b) {
			return setConst(0)
		}
	case OpBitOr:
		if isAllOnes(a) || isAllOnes(b) {
			return setConst(-1)
		}
	case OpMod, OpUMod:
		// `x % 1` is 0; `x % -1` is skipped (INT_MIN % -1 is UB).
		if isOne(b) {
			return setConst(0)
		}
	case OpAnd:
		if w != 1 {
			return false
		}
		if isZero(a) || isZero(b) {
			return setConst(0)
		}
	case OpOr:
		if w != 1 {
			return false
		}
		if isOne(a) || isOne(b) {
			return setConst(1)
		}
	}
	return false
}

// optIdentityOperands validates the shape both identity halves need and returns
// the destination's LLVM type plus the two operands.
//
// Float is rejected wholesale. The integer laws do NOT carry over: `x * 0.0` is
// not 0 for x = inf or NaN, and `x + 0.0` is not x for x = -0.0. There is no
// float identity in this file on purpose.
func (m *Module) optIdentityOperands(f *Function, inst *Inst) (dstLT string, a, b ValueID, ok bool) {
	if inst == nil || inst.Dst <= NoVal || len(inst.Args) != 2 {
		return "", 0, 0, false
	}
	if !optFoldableArith(inst.Op) && inst.Op != OpAnd && inst.Op != OpOr {
		return "", 0, 0, false
	}
	dstLT, ok = optScalarLLVM(m.optValueType(f, inst.Dst))
	if !ok || dstLT == "double" || optIntWidth(dstLT) == 0 {
		return "", 0, 0, false
	}
	a, b = inst.Args[0], inst.Args[1]
	// Both operands must be plain scalars of the destination's width: emitArith
	// coerces each operand to the result type, and a mixed-width identity would
	// have to reproduce that coercion.
	aLT, okA := optScalarLLVM(m.optValueType(f, a))
	bLT, okB := optScalarLLVM(m.optValueType(f, b))
	if !okA || !okB || aLT != dstLT || bLT != dstLT {
		return "", 0, 0, false
	}
	return dstLT, a, b, true
}

// optSetCopy turns an already-allocated instruction into the single-result copy
// shape `dst = src` (what `b.Emit(OpMove, typ, [src])` builds).
//
// The destination keeps its identity, so no consumer is dangled; the readers are
// then redirected to src by optCopyProp, which is what actually removes the
// instruction. A copy that propagation cannot remove is still a win over the
// arithmetic op it replaced, because the back end sees a plain register move.
func (m *Module) optSetCopy(f *Function, inst *Inst, src ValueID) {
	inst.Op = OpMove
	inst.Args = []ValueID{src}
	inst.Sym = ""
	inst.Callee = NoVal
	inst.Results = nil
	inst.MovesArg = false
	inst.BufClone = false
	inst.Int = 0
	inst.Flt = 0
	inst.IntBig = ""
	if t, ok := f.LocalTypes[inst.Dst]; ok {
		inst.Type = t
	}
}

// ─── level 1: copy propagation ───────────────────────────────────────────────

// optCopySource reports the source of a copy that optCopyProp may delete: a
// single-result scalar copy (`dst = move src`) whose destination is assigned
// exactly once.
//
// The `defs[dst] == 1` gate is what makes deletion safe at all — if dst had a
// second definition, deleting this one would not make dst dead, it would just
// leave the other definition as the only producer while every reader that ran
// before it changed meaning.
//
// Owned values are excluded by optScalarLLVM. That is not a technicality: an
// owned value's move TRANSFERS the buffer and its drop, so deleting the move
// would leak or double-free. Only non-owned scalars, whose copy carries nothing
// but bits, are ever propagated.
func (m *Module) optCopySource(f *Function, inst *Inst, defs map[ValueID]int) (ValueID, bool) {
	if inst == nil || inst.Dst <= NoVal || len(inst.Args) != 1 {
		return 0, false
	}
	switch inst.Op {
	case OpMove, OpClone:
	default:
		return 0, false
	}
	if defs[inst.Dst] != 1 {
		return 0, false
	}
	src := inst.Args[0]
	if src == inst.Dst {
		return 0, false
	}
	dstLT, ok := optScalarLLVM(m.optValueType(f, inst.Dst))
	if !ok {
		return 0, false
	}
	srcLT, ok := optScalarLLVM(m.optValueType(f, src))
	if !ok || srcLT != dstLT {
		return 0, false
	}
	return src, true
}

// optHandleConsumer reports whether an instruction treats its operand as an
// R-tier task handle rather than as an ordinary value.
//
// WHY THE COPY PROPAGATOR HAS TO CARE
// -----------------------------------
// These two ops are the whole of the R-tier discipline (see tier.go's
// `case OpTaskRetain, OpAwait:` — they are what puts a value in tier R), and
// that discipline is "one reference count per handle COPY, released by
// `OpAwait`". hir2mir emits them in lockstep: every handle copy is immediately
// followed by the `OpTaskRetain` that accounts for the new reference.
//
// So a handle copy is NOT a redundant alias, and the two ops that read it are
// NOT ordinary reads:
//
//   - `OpAwait` CONSUMES its operand — it releases the reference and zeroes the
//     slot. Two awaits sharing one slot therefore cannot both succeed: the
//     first zeroes it and the second reads 0.
//   - `OpTaskRetain` is the count that the matching `OpAwait` will release.
//     Redirecting it onto the source slot moves the count without moving the
//     release, which unbalances the pair.
//
// Both were measured. `tests/async-rc.no`'s `h = run f(21); h2 = h; awy h;
// awy h2` printed `1 42 0` instead of `1 42 42`, and `tests/async-handle-alias.no`
// lost two of its three results (`1a=42 1b=0`, `2a=42 2b=0 2c=0`). The `.no`
// source documents the shape by name — "the two awaits shared one slot, so the
// second read a zeroed handle and printed 0 instead of 42".
//
// This is deliberately a whitelist of the ops that carry the semantics, not a
// guess about which ops mutate their arguments: `drop` and the ownership
// rewrites are inserted by `Analyze`, which runs AFTER this pass, so the only
// destructive readers present here are the async ones.
func optHandleConsumer(op Op) bool {
	switch op {
	case OpTaskRetain, OpAwait:
		return true
	}
	return false
}

// optCopyProp deletes scalar copies by pointing their readers at the source.
//
// It is a PER-BLOCK pass, and that restriction is the whole safety argument.
// Within a block, instructions execute in order, so:
//
//   - the source must already be available (a parameter, or defined earlier in
//     this same block) — that is `available` below;
//   - the source must not be redefined after the copy — a reader placed after
//     that redefinition would see the new value, not the copied one;
//   - every reference to the copy's destination must be in this block AND after
//     the copy — otherwise a reader would keep pointing at a value whose only
//     definition has just been deleted, which is precisely the dangling-slot
//     failure the block-removal guard exists to prevent.
//
// A cross-block version needs dominance, and the payoff does not justify it:
// the copies hir2mir emits are overwhelmingly consumed by the very next
// instruction (the `move dst=18 args=[16]` / `move dst=19 args=[18]` chains in
// the range-loop lowering).
//
// `defs` and `refs` are module-wide counts taken before Phase A. Deletions only
// ever shrink both, so `defs[v] == 1` in the snapshot implies `defs[v] == 1`
// here — the snapshot never over-reports a single definition.
func (m *Module) optCopyProp(f *Function, defs, refs map[ValueID]int, st *OptStats) int {
	params := map[ValueID]bool{}
	for _, p := range f.Params {
		params[p] = true
	}
	removed := 0
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil || len(blk.Insts) == 0 {
			continue
		}
		alias := map[ValueID]ValueID{}
		definedHere := map[ValueID]bool{}
		refsBefore := map[ValueID]int{}
		resolve := func(v ValueID) ValueID {
			for n := 0; n < 64; n++ {
				nv, ok := alias[v]
				if !ok {
					return v
				}
				v = nv
			}
			return v
		}
		// refsIn counts this block's references, used to prove that every
		// reference to a copy's destination lives here.
		refsIn := map[ValueID]int{}
		for _, iid := range blk.Insts {
			optForEachRef(m.Inst(iid), func(v ValueID) { refsIn[v]++ })
		}
		if blk.Term != nil {
			for _, a := range blk.Term.Args {
				refsIn[a]++
			}
		}
		// lastDefPos is the position of the LAST instruction in this block that
		// defines each value. `defs[s] == 1` alone is NOT enough to prove that a
		// source is stable: a parameter has zero definitions from parameters, so
		// a single later assignment to it also reads as `defs[s] == 1`. A reader
		// placed after that assignment would then see the new value instead of
		// the one the copy took — the exact bug this map exists to prevent.
		lastDefPos := map[ValueID]int{}
		for i, iid := range blk.Insts {
			if inst := m.Inst(iid); inst != nil {
				if d := definedValue(inst); d > NoVal {
					lastDefPos[d] = i
				}
			}
		}

		kept := blk.Insts[:0]
		for pos, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil {
				continue
			}
			// Every reference this instruction makes, recorded before the
			// instruction's own definition is considered — that ordering is
			// what makes refsBefore count only genuinely earlier reads.
			optForEachRef(inst, func(v ValueID) { refsBefore[v]++ })

			// Redirect this instruction's reads through the live aliases.
			for ai, a := range inst.Args {
				if ai == 1 && moveLike(inst) {
					continue // Args[1] of a move-into is a DESTINATION
				}
				if nv := resolve(a); nv != a {
					inst.Args[ai] = nv
				}
			}
			if inst.Callee > NoVal {
				if nv := resolve(inst.Callee); nv != inst.Callee {
					inst.Callee = nv
				}
			}

			if src, ok := m.optCopySource(f, inst, defs); ok {
				s := resolve(src)
				available := params[s] || definedHere[s]
				// s must not be reassigned after this point, or a redirected
				// reader would see the new value instead of the copied one.
				// `defs[s] == 1` bounds the count module-wide; lastDefPos
				// pins WHERE that one definition is, which is what actually
				// matters inside a block (a parameter contributes no
				// definition at all, so a single later assignment to it would
				// otherwise slip through as "defined once").
				singleDef := defs[s] == 1 || (defs[s] == 0 && params[s])
				redefinedLater := false
				if lp, ok := lastDefPos[s]; ok && lp >= pos {
					redefinedLater = true
				}
				// Every read of dst must be here and later, so that redirecting
				// them all leaves nothing pointing at a deleted definition.
				allReadsHere := refs[inst.Dst] == refsIn[inst.Dst] && refsBefore[inst.Dst] == 0
				// A reader that consumes dst as an R-tier handle pins the copy
				// in place: see optHandleConsumer. `allReadsHere` above already
				// proved every read of dst is in this block, so scanning from
				// `pos` is enough — and it only runs once the cheaper
				// conditions have already passed.
				//
				// The SOURCE is checked too, for the mirror-image shape: with
				// `move dst=t args=[h]` followed by `awy h` and then a plain
				// read of t, redirecting that read onto h hands it the handle
				// the await just zeroed. Redirecting is only sound if neither
				// side is destructively read at or after the copy.
				handleRead := false
				if allReadsHere {
				scan:
					for _, rid := range blk.Insts[pos:] {
						ri := m.Inst(rid)
						if ri == nil || !optHandleConsumer(ri.Op) {
							continue
						}
						for _, a := range ri.Args {
							if a == inst.Dst || a == s {
								handleRead = true
								break scan
							}
						}
					}
				}
				if available && singleDef && !redefinedLater && allReadsHere && !handleRead {
					alias[inst.Dst] = s
					removed++
					continue // the copy itself is deleted
				}
			}
			if d := definedValue(inst); d > NoVal {
				definedHere[d] = true
			}
			kept = append(kept, iid)
		}
		blk.Insts = kept
		if blk.Term != nil {
			for ti, a := range blk.Term.Args {
				if nv := resolve(a); nv != a {
					blk.Term.Args[ti] = nv
				}
			}
		}
	}
	st.Copies += removed
	return removed
}

// ─── level 1: dead scalar elimination ────────────────────────────────────────

// optRemovableScalar reports whether an instruction is a PURE scalar producer
// that may be deleted when its result is unused.
//
// "Pure" here is narrow by construction: no call, no memory write, no
// allocation, no drop/retain, no index or field read (a bounds-checked read can
// trap, so deleting it would change behaviour). The remaining ops are exactly
// the ones this file can also fold, plus OpConst.
//
// The ownership gate is separate and load-bearing: an OWNED value that is never
// read still needs its drop, so deleting its definition would leave the drop
// pointing at nothing. Only non-owned scalars are ever removed.
func optRemovableScalar(inst *Inst, dstType *Type) bool {
	if inst == nil || inst.Dst <= NoVal {
		return false
	}
	if dstType == nil || dstType.Owned {
		return false
	}
	switch dstType.Kind {
	case KindInt, KindChar, KindBool, KindFloat:
	default:
		return false
	}
	if inst.Op == OpConst {
		return true
	}
	return optFoldableArith(inst.Op) || optFoldableCmp(inst.Op) ||
		inst.Op == OpNeg || inst.Op == OpNot
}

// optDeleteUnused removes every unused, removable instruction from one
// function, iterating to a fixpoint because deleting an instruction can make its
// operands unused in turn.
//
// The definition/use maps are re-derived HERE rather than passed in, and that is
// load-bearing: the fold phase clears the operands of every instruction it
// rewrites, so a use count taken before folding still credits the (now gone)
// operands and nothing is ever removed. Measured on
// `#{overflow=wrap} a = 2 + 3` + `#{overflow=wrap} b = a * 4`: with a stale map
// both folds happened but `dead=0`, and the three now-unused constants survived.
//
// Only `Dst > NoVal` (fresh-value) instructions are candidates, and only when
// the value has exactly ONE definition module-wide. That second condition is
// what makes removal safe: a value with a second definition (a move-into) is a
// mutable slot, and dropping the initialiser would change what the slot holds
// for any reader this pass cannot see.
func (m *Module) optDeleteUnused(f *Function, st *OptStats) {
	for round := 0; round < 8; round++ {
		defs, uses := m.optDefUse()
		removed := 0
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil || len(blk.Insts) == 0 {
				continue
			}
			kept := blk.Insts[:0]
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst != nil && optRemovableScalar(inst, m.optValueType(f, inst.Dst)) &&
					defs[inst.Dst] == 1 && uses[inst.Dst] == 0 {
					// Deleting the only definition is safe precisely because
					// nothing reads the value: `uses` counts every operand
					// position in the whole module except a move destination.
					removed++
					continue
				}
				kept = append(kept, iid)
			}
			blk.Insts = kept
		}
		if removed == 0 {
			break
		}
		st.DeadInsts += removed
	}
}

// ─── level 2: control-flow cleanup ───────────────────────────────────────────

// optFoldConstBranches replaces an OpCondBr whose condition is a known constant
// with the unconditional branch the constant selects.
//
// emitTerm's cond-br is `br i1 <cond>, label Targets[0], label Targets[1]`, so
// Targets[0] is the TRUE edge; a non-i1 condition (nolang booleans sometimes
// ride on i64) is coerced by a `!= 0` test, which is what the fold reproduces.
// An option-typed condition is skipped: `nil` lowers to an OpConst option, but
// the tag extraction emitTerm performs is not modelled here.
func (m *Module) optFoldConstBranches(f *Function, defs map[ValueID]int, constOf map[ValueID]InstID, st *OptStats) {
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil || blk.Term == nil {
			continue
		}
		t := blk.Term
		if t.Op != OpCondBr || len(t.Args) < 1 || len(t.Targets) < 2 {
			continue
		}
		condType := m.optValueType(f, t.Args[0])
		if condType == nil || (condType.Kind != KindBool && condType.Kind != KindInt && condType.Kind != KindChar) {
			continue
		}
		c, ok := m.optConstOf(f, t.Args[0], defs, constOf)
		if !ok || c.llvm == "double" {
			continue
		}
		taken := t.Targets[1]
		if c.i != 0 {
			taken = t.Targets[0]
		}
		t.Op = OpBr
		t.Args = nil
		t.Targets = []BlockID{taken}
		st.ConstBrs++
	}
}

// optRemoveUnreachable drops every block a function's entry cannot reach.
//
// The entry must stay f.Blocks[0]: codegen emits `f.Blocks[0]` as the function's
// entry label (`name.bb0`) and derives every other label from the block's INDEX
// in f.Blocks, so the first element is not removable and the rest may be
// compacted freely.
//
// TWO things keep this from being a plain reachability sweep:
//
//  1. An unreachable block is NOT necessarily dead weight. If it holds the only
//     definition of a value a reachable block still reads (see
//     optBlockHoldsSoleLiveDef), it is added as an extra ROOT, and the flood
//     restarts from it. Adding it as a root rather than merely skipping it is
//     what keeps the result closed: a kept block's own successors must stay too,
//     or its terminator would point at a block that is no longer in f.Blocks.
//
//  2. A block removed from f.Blocks must also lose its terminator targets.
//     Analyze calls BuildCFG, which walks the whole module block table and
//     derives predecessors from Term.Targets — it does not know or care that a
//     block is no longer listed by its function. Leaving the edges in place
//     would hand Analyze's dataflow a predecessor that no longer exists.
func (m *Module) optRemoveUnreachable(f *Function, defs, refs map[ValueID]int, st *OptStats) {
	if len(f.Blocks) <= 1 {
		return
	}
	live := map[BlockID]bool{}
	var stack []BlockID
	if f.Entry > NoBlock {
		stack = append(stack, f.Entry)
	}
	for _, bid := range f.Blocks {
		if m.optBlockHoldsSoleLiveDef(m.Block(bid), defs, refs) {
			stack = append(stack, bid)
		}
	}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if b <= NoBlock || live[b] {
			continue
		}
		blk := m.Block(b)
		if blk == nil {
			continue
		}
		live[b] = true
		if blk.Term != nil {
			for _, tgt := range blk.Term.Targets {
				if !live[tgt] {
					stack = append(stack, tgt)
				}
			}
		}
	}
	kept := f.Blocks[:0]
	for i, bid := range f.Blocks {
		if i == 0 || live[bid] {
			kept = append(kept, bid)
			continue
		}
		if blk := m.Block(bid); blk != nil {
			st.InstrSaved += len(blk.Insts)
			// Drop the outgoing edges too: see (2) above.
			if blk.Term != nil {
				blk.Term.Targets = nil
			}
		}
		st.Unreach++
	}
	f.Blocks = kept
}

// optThreadEmptyBlocks removes a block that has no instructions and branches
// unconditionally to a single other block, rewiring every predecessor to branch
// there directly.
//
// This is the classic "empty block" shape hir2mir leaves behind when an if/loop
// is lowered: `block 4 (preds=[2 3]) TERM br targets=[5]` with nothing in it.
//
// ─── WHY THE OBVIOUS VERSION OF THIS PASS IS WRONG ──────────────────────────
//
// Removing an empty pass-through block looks unconditionally safe: the block
// performs no work, so `P -> B -> T` and `P -> T` execute the same
// instructions in the same order. That reasoning is CORRECT about the CFG and
// WRONG about this compiler, because `Analyze` places every `OpDrop` by asking
// "which edges reach this block?" — and the redirect changes exactly that.
//
// Measured failure (tests/mem-safety/option-match-basic.no, and 48 more of the
// 631-file corpus): an option match leaves the shape
//
//	block 2: ... drop v ; br 4        (v dies at the end of block 2)
//	block 6: cond-br -> 8, 9          (v live into 8, dead into 9)
//	block 9: (empty) br 10            (the trampoline)
//	block 4: print('end'); return
//
// `insertDrops` places v's drop at the START of the dead successor 9, because
// dropping at the end of block 6 would free v before the sibling arm 8 reads
// it. That start-drop fires on exactly one path — the one through the
// trampoline. Thread 9 (and 10, and 7) away and the dead successor becomes
// block 4 itself, which now has four predecessors: 2 and 8 (which already
// dropped v at their ends) and 5 and 6 (which have not). One start-drop at
// block 4 cannot serve both groups: it either double-frees on 2 and 8, or
// leaks on 5 and 6. The pass picked the first, and the program died with
// SIGTRAP after printing one line short of its output.
//
// So the empty trampoline is NOT dead weight — it is the only place a per-EDGE
// drop can be expressed, since MIR has no edge-splitting. The restriction below
// is therefore a correctness requirement, not a heuristic.
//
// Guards: never the entry, never the first slot in f.Blocks (codegen's bb0),
// never a self-loop, never a function containing OpPhi (this pass rewires edges
// without editing phi operands, and although hir2mir never emits OpPhi today, a
// future emitter would need phi fixups here), and — per the above — only when
// the empty block is the target's ONLY predecessor, so the redirect relabels a
// path instead of adding one. Note that the empty block itself may have any
// number of predecessors: a diamond's two arms may both enter the trampoline,
// and redirecting both is exactly the case the proof covers, because the target
// then inherits precisely the arms it already reached through it.
func (m *Module) optThreadEmptyBlocks(f *Function, st *OptStats) {
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := m.Inst(iid); inst != nil && inst.Op == OpPhi {
				return
			}
		}
	}
	for round := 0; round < 8; round++ {
		changed := 0
		// Build the set of removable blocks first, then rewire once, so a chain
		// of empty blocks collapses in a single pass without repeated scans.
		redirect := map[BlockID]BlockID{}
		inFunc := map[BlockID]bool{}
		for _, bid := range f.Blocks {
			inFunc[bid] = true
		}
		// Predecessors counted from terminators. blk.Preds cannot be used: this
		// pass runs before Analyze/BuildCFG, so those fields are stale.
		preds := map[BlockID][]BlockID{}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil || blk.Term == nil {
				continue
			}
			seenTgt := map[BlockID]bool{}
			for _, tgt := range blk.Term.Targets {
				if !inFunc[tgt] || seenTgt[tgt] {
					continue
				}
				seenTgt[tgt] = true
				preds[tgt] = append(preds[tgt], bid)
			}
		}
		for i, bid := range f.Blocks {
			if i == 0 || bid == f.Entry {
				continue
			}
			blk := m.Block(bid)
			if blk == nil || len(blk.Insts) != 0 || blk.Term == nil {
				continue
			}
			if blk.Term.Op != OpBr || len(blk.Term.Targets) != 1 {
				continue
			}
			tgt := blk.Term.Targets[0]
			if tgt == bid || !inFunc[tgt] {
				continue
			}
			// SAFETY (see the note above): `bid` must be the ONLY route into
			// `tgt`. Every path into `tgt` then already went through the empty
			// `bid`, so replacing the two-block hop by one edge hands `tgt` the
			// same set of paths it already had — only the intermediate empty
			// block disappears. That is enough because `Analyze`'s drop
			// placement keys off exactly "which edges reach this block", and
			// both inputs to that question are unchanged: `bid` executes
			// nothing, so `liveIn[bid] == liveIn[tgt]` and a drop the pass used
			// to place at `bid`'s start now lands at `tgt`'s start and fires on
			// the same paths.
			//
			// Anything looser hands `tgt` a predecessor it did not have, and a
			// start-drop placed there then fires on paths that had already
			// freed the value. Measured (see the note above): threading turned
			// a dead arm's empty trampoline into the arm's real successor,
			// which already had predecessors that dropped the value at their
			// ends, so the single start-drop became a second free and the
			// program died with SIGTRAP.
			if len(preds[tgt]) != 1 || preds[tgt][0] != bid {
				continue
			}
			// A `tgt` that itself branches into `bid` would become its own
			// predecessor: `tgt -> bid -> tgt` collapses to `tgt -> tgt`. The
			// loop is non-terminating either way, but a self-loop is not a
			// shape the rest of the pipeline expects to see.
			selfLoop := false
			for _, p := range preds[bid] {
				if p == tgt {
					selfLoop = true
					break
				}
			}
			if selfLoop {
				continue
			}
			redirect[bid] = tgt
		}
		if len(redirect) == 0 {
			break
		}
		// Resolve each removable block to the block that actually SURVIVES.
		//
		// Two failure modes are ruled out here, both of which would leave a
		// predecessor branching into a block that is no longer in f.Blocks:
		//
		//   * a CYCLE of empty blocks (a -> b -> a). Nothing in the cycle
		//     survives, so every member must be excluded. A fixed iteration cap
		//     would silently return a mid-cycle block that is itself deleted.
		//   * a chain longer than the cap — same wrong answer, for a different
		//     reason. The visited set below makes the walk exact instead.
		final := map[BlockID]BlockID{}
		for bid := range redirect {
			seen := map[BlockID]bool{}
			b, ok := bid, true
			for {
				next, isRedir := redirect[b]
				if !isRedir {
					break
				}
				if seen[b] {
					ok = false // cycle: no surviving target
					break
				}
				seen[b] = true
				b = next
			}
			if ok && b != bid && inFunc[b] {
				final[bid] = b
			}
		}
		if len(final) == 0 {
			break
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil || blk.Term == nil {
				continue
			}
			for ti, tgt := range blk.Term.Targets {
				if n, ok := final[tgt]; ok {
					blk.Term.Targets[ti] = n
				}
			}
		}
		kept := f.Blocks[:0]
		for _, bid := range f.Blocks {
			if _, drop := final[bid]; drop {
				changed++
				// As in optRemoveUnreachable: a block that leaves f.Blocks must
				// not keep feeding BuildCFG an edge from a predecessor that no
				// longer exists.
				if blk := m.Block(bid); blk != nil && blk.Term != nil {
					blk.Term.Targets = nil
				}
				continue
			}
			kept = append(kept, bid)
		}
		f.Blocks = kept
		st.Threaded += changed
	}
}

// optMergeBlocks merges a block into its only predecessor when that predecessor
// branches to it unconditionally.
// This is the non-empty counterpart of optThreadEmptyBlocks: threading removes
// blocks that contain nothing, merging removes the boundary between two blocks
// where nothing needs to intervene. Each merge costs one block AND one branch.
//
// It is semantics-preserving for the same reason threading is: P's terminator
// was an unconditional branch to B, so B's instructions already ran immediately
// after P's. Splicing them into P changes which block they are LABELLED with,
// not the order in which they execute.
//
// Guards, each for a concrete failure:
//
//   - B must have exactly ONE predecessor, and it must be P. With two
//     predecessors the other one still needs B's label.
//   - P's terminator must be an unconditional `br B` with that single target.
//     A cond-br means the other edge also needs B's label.
//   - Neither the entry nor f.Blocks[0] may be B: codegen emits f.Blocks[0] as
//     the function's entry label (`name.bb0`), so it can never be folded away.
//   - B must not branch to itself. (Unreachable while B has one predecessor and
//     P != B, but refusing is cheaper than reasoning about it.)
//
// The loop runs to a fixpoint because a chain P -> B -> C collapses one level
// per round: after B is spliced into P, P inherits B's terminator and becomes
// C's only predecessor.
func (m *Module) optMergeBlocks(f *Function, st *OptStats) {
	if len(f.Blocks) <= 1 {
		return
	}
	for round := 0; round < 16; round++ {
		inFunc := map[BlockID]bool{}
		for _, bid := range f.Blocks {
			inFunc[bid] = true
		}
		// Predecessors, counted from the terminators of this function's own
		// blocks. blk.Preds cannot be used: Analyze (which calls BuildCFG) has
		// not run yet, and this pass must not depend on being analysed first.
		preds := map[BlockID][]BlockID{}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil || blk.Term == nil {
				continue
			}
			seenTgt := map[BlockID]bool{}
			for _, tgt := range blk.Term.Targets {
				if !inFunc[tgt] || seenTgt[tgt] {
					continue
				}
				seenTgt[tgt] = true
				preds[tgt] = append(preds[tgt], bid)
			}
		}
		removed := map[BlockID]bool{}
		merged := 0
		for i, bid := range f.Blocks {
			if i == 0 || bid == f.Entry || removed[bid] {
				continue
			}
			ps := preds[bid]
			if len(ps) != 1 {
				continue
			}
			p := ps[0]
			if p == bid || !inFunc[p] || removed[p] {
				continue
			}
			pblk := m.Block(p)
			if pblk == nil || pblk.Term == nil {
				continue
			}
			if pblk.Term.Op != OpBr || len(pblk.Term.Targets) != 1 || pblk.Term.Targets[0] != bid {
				continue
			}
			blk := m.Block(bid)
			if blk == nil || blk.Term == nil {
				continue
			}
			selfLoop := false
			for _, tgt := range blk.Term.Targets {
				if tgt == bid {
					selfLoop = true
				}
			}
			if selfLoop {
				continue
			}

			pblk.Insts = append(pblk.Insts, blk.Insts...)
			pblk.Term = blk.Term
			// P's own terminator (the unconditional branch to B) is dropped
			// here. That is safe even if it carried Args: emitTerm's OpBr case
			// reads only Targets[0] and ignores Args entirely, and every OpBr
			// the front end emits passes nil anyway.
			//
			// The Term object now belongs to P. Clearing blk.Term is what keeps
			// the cleanup below from also clearing P's terminator, and it stops
			// BuildCFG (called by Analyze) from deriving edges out of a block
			// that is no longer in f.Blocks.
			blk.Term = nil
			blk.Insts = nil
			removed[bid] = true
			merged++
		}
		if merged == 0 {
			break
		}
		kept := f.Blocks[:0]
		for _, bid := range f.Blocks {
			if removed[bid] {
				continue
			}
			kept = append(kept, bid)
		}
		f.Blocks = kept
		st.Merged += merged
	}
}

// ─── driver ──────────────────────────────────────────────────────────────────

// optimizeFunc runs the whole pipeline over one function.
//
// `defs` is the module-wide definition snapshot taken before Phase A, which is
// what Phase A's fold needs. Everything after it re-derives what it needs from
// the current IR rather than reusing that snapshot: Phase B because folding has
// cleared operands and it must see the post-fold use counts, Phase C because
// both folding and Phase B have changed the instruction stream. The fold phase
// keeps a per-function constOf index that GROWS as instructions are folded (a
// folded instruction is a new constant, which can enable a further fold — hence
// the fixpoint).
func (m *Module) optimizeFunc(f *Function, level int, defs, refs map[ValueID]int, st *OptStats) {
	constOf := map[ValueID]InstID{}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst != nil && inst.Op == OpConst && inst.Dst > NoVal {
				constOf[inst.Dst] = iid
			}
		}
	}

	// Phase A — constant folding, integer identities and copy propagation, to a
	// fixpoint. The three feed each other, which is why they share one loop:
	// copy propagation turns `t = move c` + `r = t + 1` into `r = c + 1`, and
	// that is only foldable on the NEXT round; an identity such as `x * 1`
	// becomes a copy, which the next round's propagation can then delete.
	for round := 0; round < 8; round++ {
		n := 0
		n += m.optCopyProp(f, defs, refs, st)
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil || inst.Op == OpConst {
					continue
				}
				folded, isCmp := m.optTryFold(f, inst, defs, constOf)
				if folded {
					constOf[inst.Dst] = inst.ID
					if isCmp {
						st.FoldCmp++
					} else {
						st.FoldArith++
					}
					n++
					continue
				}
				if m.optTryIdentityConst(f, inst, defs, constOf) {
					constOf[inst.Dst] = inst.ID
					st.FoldArith++
					n++
					continue
				}
				if m.optTryIdentity(f, inst, defs, constOf) {
					st.Idents++
					n++
				}
			}
		}
		if n == 0 {
			break
		}
	}

	// Phase B — dead scalar elimination.
	m.optDeleteUnused(f, st)

	// Phase C — control-flow cleanup (level 2).
	if level >= OptCFG {
		// Re-derive the counts instead of reusing the snapshot from before
		// Phase A: folding cleared the operands of every instruction it
		// rewrote and Phase B deleted instructions outright, so the old maps
		// describe a module that no longer exists. Phase C's safety guard
		// (optBlockHoldsSoleLiveDef) reads them to decide which blocks may be
		// deleted, so it has to see the CURRENT slot-definition and reference
		// counts. Neither map is the operand-use map: a value needs a slot if
		// it is REFERENCED anywhere (including as a move-into destination), and
		// only an instruction with Dst > NoVal can create that slot.
		csd := m.optSlotDefs()
		crefs := m.optRefCounts()
		// optFoldConstBranches still wants the definedValue-based counts: its
		// "defined exactly once, by an OpConst" test is about the value being a
		// variable that is never reassigned, which is precisely what
		// definedValue (move destinations included) expresses.
		cdefs, _ := m.optDefUse()
		m.optFoldConstBranches(f, cdefs, constOf, st)
		m.optRemoveUnreachable(f, csd, crefs, st)
		m.optThreadEmptyBlocks(f, st)
		m.optMergeBlocks(f, st)
		// Merging can expose fresh folding opportunities (two blocks' worth of
		// constants now sit next to each other) and can strand instructions, so
		// re-run the level-1 half and re-check the invariants of the new CFG.
		m.optDeleteUnused(f, st)
	}
}
