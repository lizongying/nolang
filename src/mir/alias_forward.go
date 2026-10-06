package mir

import (
	"fmt"
	"os"
)

// ─────────────────────────────────────────────────────────────────────────────
// Rule 1 (NOLANG-OWNERSHIP-MODEL.md §1.1) — `a = b` is NEVER a zero-copy alias
//
// A value-to-value assignment must give the destination its OWN storage. The
// lowerer already emits the deep copy (an owned-slice fresh binding is lowered
// to OpClone directly — hir2mir.go, "a fresh binding has no pre-existing slot
// to move into, so it always clones"); this pass must not undo it.
//
// It used to. "Correction A" / tier C argued that a copy whose alias is only
// ever READ is unobservable, so the pass re-pointed every use of the alias at
// the source, deleted the copy, and left the two names sharing one buffer with
// a single drop. It also forwarded a WRITTEN alias whenever the source was
// provably dead at every write (aliasWritesUnobservable). Both are GONE.
//
// The model now holds that arbitrary value-to-value aliasing has no semantic
// value and costs a whole extra class of cases for the analysis to get right —
// alias chains, second aliases, cross-block aliases, parameter aliases,
// option-peel aliases. One deep copy buys back "a value has exactly one
// owner", which is the property everything else is built on.
//
// ELEMENT AND FIELD READS ARE UNAFFECTED. `a = b[i]` and `a = b.c` are
// borrowing reads, not OpMove/OpClone bindings, so they never reached this
// pass. Their view semantics is kept on purpose (it is the "short alias"
// idiom), and it is what the borrow refcounting — OpRetain / OpRelease — is
// for.
//
// WHAT REMAINS is rule 1's second case: when the source is provably DEAD at
// the copy there is nothing to copy — the assignment is a MOVE, which produces
// no alias either. The lowerer emits OpClone unconditionally for a fresh slice
// binding (it does not consult the source's liveness), so this pass rewrites
// that OpClone to OpMove and hands the decision to insertDrops' existing
// slice-move logic: that logic keeps the zero-copy move and exempts the
// source's drop when the source is dead, and clones it right back when it is
// not. So this can only ever REMOVE a copy, never add one, and it cannot
// under-clone.
// ─────────────────────────────────────────────────────────────────────────────

// aliasStatsOn, when NOLANG_ALIAS_STATS=1, reports per-function how many
// assignment-point copies were lowered to moves, and why the others were left
// alone. It prints to stderr and is off by default.
var aliasStatsOn = os.Getenv("NOLANG_ALIAS_STATS") == "1"

func aliasStats(f *Function, moved int, reasons map[string]int) {
	if !aliasStatsOn {
		return
	}
	fmt.Fprintf(os.Stderr, "[alias] func %s clone-to-move=%d", f.Name, moved)
	for _, k := range []string{"src-live", "src-is-borrow", "not-owned-slice"} {
		if n := reasons[k]; n > 0 {
			fmt.Fprintf(os.Stderr, " %s=%d", k, n)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// lowerDeadSourceCopiesToMoves applies rule 1's "dead source ⇒ move" half: an
// OpClone whose source has no use left after the copy is a transfer, not a
// copy.
//
// It must run AFTER markEnumPayloadOwners (isBorrowRead consults
// enumOwnsPayload) and BEFORE the liveness/drop computation, which has to see
// the final instruction stream.
func (m *Module) lowerDeadSourceCopiesToMoves(f *Function) {
	if f == nil || f.IsExtern {
		return
	}
	reasons := map[string]int{}
	moved := 0
	// A closure, not `defer aliasStats(...)`: a deferred call's arguments are
	// evaluated at defer time, which would always report 0.
	defer func() { aliasStats(f, moved, reasons) }()

	// Liveness of the CURRENT IR. Rewriting an OpClone to OpMove changes what
	// insertDrops must place, so it recomputes its own sets afterwards — this
	// copy is only used to decide the rewrite.
	_, liveOut := m.Liveness(f)

	// The defining instruction of each value, for the "source must own its
	// buffer" test below. A PARAMETER has no defining instruction in this
	// frame, which is correct: it is the caller's storage and is never a
	// borrowing read.
	defInst := map[ValueID]*Inst{}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil || inst.Dst <= NoVal {
				continue
			}
			if defInst[inst.Dst] == nil {
				defInst[inst.Dst] = inst
			}
		}
	}

	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil || inst.Op != OpClone || len(inst.Args) == 0 {
				continue
			}
			src := inst.Args[0]
			if src <= NoVal {
				continue
			}
			// (r4) the source must OWN its buffer. A value produced by a
			// BORROWING read — an element read, a struct field read, a
			// tagged-enum payload peel, an array slice view — aliases storage
			// that ANOTHER value frees, so handing it over as a transfer gives
			// away a buffer the owner still frees: a double free, not a leak.
			//
			// This test is hoisted ABOVE the rewrite on purpose. It first
			// shipped below it, which let the rewrite apply to a BORROWING
			// peel; measured, tests/tagged-enum-two-match.no changed its output
			// and tests/tagged-enum-zero-match.no crashed (rc=133), while the
			// unit tests were green — only the corpus caught it.
			if di := defInst[src]; di != nil && (m.isBorrowRead(f, di) || m.isSliceViewOfArray(f, di)) {
				reasons["src-is-borrow"]++
				continue
			}
			// A PARAM source is caller storage too — it has no defining
			// instruction here, so the test above never sees it, yet a "transfer"
			// of it hands the CALLER's buffer to the destination while the caller
			// keeps ownership and drops it (bufio.reader.init's `r.buf = buf`
			// through exactly this hole: the lowering-side clone was rewritten
			// back to a move and the second read-byte freed the dangling
			// descriptor). Never lower the copy for a param source.
			if m.isParamValue(f, src) {
				reasons["src-is-borrow"]++
				continue
			}
			// The source must be DEAD here, otherwise rule 1 keeps the deep
			// copy (this is the only place rule 1 allows a copy to disappear).
			if liveOut[bid][src] || m.readNonDropAfterInBlock(bid, iid, src) {
				reasons["src-live"]++
				continue
			}
			dst := moveDst(inst)
			if dst <= NoVal || inst.Dst <= NoVal {
				continue
			}
			// Only the owned-slice shape. A str / struct / option / enum copy
			// is left alone: insertDrops already applies the same liveness rule
			// to their moves, and rewriting the OP here would skip the
			// destination's own type check (vecDeepClone picks its stride from
			// the element type, so a mismatched destination compiles and is
			// silently wrong).
			if !m.typeIsOwnedSlice(f, src) || !m.typeIsOwnedSlice(f, dst) {
				reasons["not-owned-slice"]++
				continue
			}
			inst.Op = OpMove
			moved++
		}
	}
}
