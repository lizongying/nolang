package mir

import (
	"fmt"
	"os"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tier C — a read-only alias is free (correction A, "first-write clone")
//
// NOLANG-OWNERSHIP-MODEL.md §1.2 corrects the "write ⇒ clone" (CoW) idea: a
// statically-determinable value must NOT pay a runtime shared-bit branch. The
// right static form is to move the clone from the ASSIGNMENT point to the
// alias's FIRST WRITE point — and, when the alias is never written at all, not
// to clone at all.
//
// insertDrops already answers "is the source still live?" at the assignment
// (`b = a`) and clones when it is — but that is NOT where the assignment-point
// copy of a fresh binding comes from. An owned-slice binding is lowered to
// OpClone DIRECTLY (hir2mir.go:2996, "a fresh binding has no pre-existing slot
// to move into, so it always clones"), so insertDrops never sees it. Both
// sources of an assignment-point copy are therefore recognised here:
//
//	OpMove  — an aliasing move insertDrops would turn into a deep clone;
//	OpClone — the copy hir2mir emits for `b []i64 = a`.
//
// The clone is load-bearing for value semantics, so it cannot simply be
// dropped: `b = a; b[0] = 99` must not disturb `a`. But when the alias is only
// ever READ, the copy is UNOBSERVABLE:
//
//	a []i64 = [1, 2, 3]
//	b []i64 = a            ; today: clone (a is still live) → tier C: no clone
//	print(a[0])            ; 1
//	print(b[0])            ; 1
//
// Every use of `b` reads `a`'s bytes and nothing writes either buffer in
// between, so `b` may simply BE `a`. This pass does exactly that: it rewrites
// every use of the alias to the source and deletes the copy, leaving the buffer
// SHARED with exactly ONE drop (the source's). That is the tier-C "zero-cost
// read-only reference" the model asks for — no new opcode, no header, no
// runtime branch.
//
// SAFETY. Under-cloning is a use-after-free, so the guard is a whitelist: every
// case the analysis cannot PROVE safe keeps today's clone.
//
//	(d1) the alias is defined exactly once and is never re-bound;
//	(d2) the alias carries no drop of its own (it is not an owner);
//	(r1) EVERY use of the alias is a pure read — never a call argument, a store,
//	     a return, a field/element write, a slice view or a second move;
//	(r2) the SOURCE is neither written nor allowed to escape in any block where
//	     the alias is live. A callee handed `a` may mutate it through the
//	     borrowed pointer (measured: `mutate(v []i64) { v[0] = 99 }` called as
//	     `mutate(a)` really does change the caller's buffer), and the alias would
//	     then observe a change that a private copy would not have seen;
//	(r3) the alias is not a parameter slot the frame must store back;
//	(r4) the SOURCE must OWN its buffer. A value produced by a borrowing read —
//	     an element read, a struct field read, a tagged-enum payload peel, an
//	     array slice view — aliases storage that ANOTHER value frees, so the
//	     alias would outlive that owner's drop. This is not hypothetical: it is
//	     what tests/tagged-enum-two-match.no caught. `items` aliased the enum's
//	     payload, the enum was dropped at the end of the match arm, and the loop
//	     then iterated freed memory (4b printed nothing at all).
//
// (r1)-(r4) are why the guard is a whitelist rather than a list of known
// hazards: `aliasUse`'s DEFAULT is aliasUseEscape, so a new opcode added to the
// IR can only make this pass more conservative, never open a hole.
//
// The "first write" half of correction A — a path-sensitive clone inserted at
// the alias's first observably-sharing use — is deliberately NOT implemented
// here. Keeping the clone at the assignment point is a conservative superset of
// it (it can only clone earlier, never later), so every alias that IS written
// behaves exactly as before. Only the read-only case changes, which is precisely
// the case where the design doc claims the win ("現況在「賦值且從沒寫」時也會
// clone").
// ─────────────────────────────────────────────────────────────────────────────

// aliasUseKind classifies what one instruction does with one of its operands.
type aliasUseKind int

const (
	aliasUseNone   aliasUseKind = iota // the value does not appear here
	aliasUseRead                       // reads bytes/len only: cannot retain or write
	aliasUseMutate                     // writes through the value, or (re)binds it
	aliasUseEscape                     // may be retained, written or freed elsewhere
)

// aliasUse classifies the relationship between inst and the operand v.
//
// The DEFAULT is aliasUseEscape. Only ops whose operand handling is provably
// side-effect-free on the buffer are listed as aliasUseRead, so a new opcode
// added to the IR can never silently open a hole here — it will simply keep
// today's clone until it is classified on purpose.
func aliasUse(inst *Inst, v ValueID) aliasUseKind {
	if inst == nil || v <= NoVal {
		return aliasUseNone
	}
	// A definition or a re-binding of v (both OpMove encodings — see moveSrc).
	if inst.Dst == v || (inst.Op == OpMove && inst.Dst == NoVal && len(inst.Args) >= 2 && inst.Args[1] == v) {
		return aliasUseMutate
	}
	idx := -1
	for i, a := range inst.Args {
		if a == v {
			idx = i
			break
		}
	}
	if idx < 0 {
		return aliasUseNone
	}
	switch inst.Op {
	// ---- pure reads: the operand's bytes are only looked at -----------------
	case OpIndex:
		// Args = [container, index]. The index operand is a scalar.
		return aliasUseRead
	case OpLen, OpCap, OpUtf8At, OpEnumTag:
		return aliasUseRead
	case OpStrEq:
		// Args = [a, b], both %str-long: @str_eq compares, retains nothing.
		return aliasUseRead
	case OpTxtFromStr:
		// str -> txt copies the bytes into a fixed buffer.
		return aliasUseRead
	case OpClone:
		// A deep copy reads the source and duplicates it; the source's own
		// buffer is neither retained nor written. (Only reachable when Analyze
		// runs a second time over already-lowered IR.)
		return aliasUseRead
	case OpDrop:
		// Releasing the value's OWN ownership is not a read of its bytes. The
		// forwarded alias has no drop of its own, so a drop here can only be
		// the owner's own — see rules (d2) and (r4).
		return aliasUseRead
	case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpUDiv, OpUMod, OpNeg, OpNot,
		OpAnd, OpOr, OpBitAnd, OpBitOr, OpXor, OpShl, OpShr,
		OpEq, OpNe, OpLt, OpLe, OpGt, OpGe,
		OpCondBr, OpSwitch, OpCast:
		// Scalar-only operands: nothing is retained or written.
		return aliasUseRead
	}
	// ---- hazards: distinguish an in-place write from an escape --------------
	switch inst.Op {
	case OpIndexStore:
		// Args = [container, index, value]
		if idx == 0 {
			return aliasUseMutate
		}
	case OpSetField:
		// Args = [receiver, value]
		if idx == 0 {
			return aliasUseMutate
		}
	case OpStore:
		// Args = [slot, value]
		if idx == 0 {
			return aliasUseMutate
		}
	}
	return aliasUseEscape
}

// aliasHazard is one instruction (or terminator) that touches a value in a
// non-read way. `pos` is its position in program order, so a hazard inside the
// alias's own block can be compared against the copy itself: a write BEFORE the
// alias is irrelevant, one after it is not.
type aliasHazard struct {
	bid BlockID
	iid InstID // NoInst for a terminator operand
	pos int
}

// aliasStats, when NOLANG_ALIAS_STATS=1, reports per-function how many alias
// copies were forwarded and why the others were rejected. It exists to keep the
// "did this actually buy anything?" question answerable with numbers instead of
// adjectives; it prints to stderr and is off by default.
var aliasStatsOn = os.Getenv("NOLANG_ALIAS_STATS") == "1"

func aliasStats(f *Function, forwarded int, reasons map[string]int) {
	if !aliasStatsOn {
		return
	}
	fmt.Fprintf(os.Stderr, "[alias] func %s forwarded=%d", f.Name, forwarded)
	for _, k := range []string{"no-shares-heap", "src-dead", "clone-src-dead-to-move", "dst-not-fresh", "dst-is-param", "multi-def", "dst-has-drop", "dst-bad-use", "src-is-borrow", "src-hazard-while-alias-live"} {
		if n := reasons[k]; n > 0 {
			fmt.Fprintf(os.Stderr, " %s=%d", k, n)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// forwardReadOnlyAliases deletes alias copies whose alias is only ever read,
// rewriting the alias's uses to the source. See the block comment above for the
// exact soundness conditions.
//
// It must run AFTER markEnumPayloadOwners (isBorrowRead consults
// enumOwnsPayload) and BEFORE the liveness/drop computation, which has to see
// the final instruction stream.
func (m *Module) forwardReadOnlyAliases(f *Function) {
	if f == nil || f.IsExtern {
		return
	}
	reasons := map[string]int{}
	forwarded := 0
	// A closure, not `defer aliasStats(...)`: a deferred call's arguments are
	// evaluated at defer time, which would always report 0.
	defer func() { aliasStats(f, forwarded, reasons) }()

	// Liveness of the CURRENT IR. Forwarding deletes an instruction and moves
	// the alias's uses onto the source, so insertDrops must recompute its own
	// sets afterwards — this copy is only used to decide the guard.
	liveIn, liveOut := m.Liveness(f)

	// ---- one sweep: definitions, drops and hazards -------------------------
	defCount := map[ValueID]int{}
	defInst := map[ValueID]*Inst{}
	moveInto := map[ValueID]bool{}
	dropped := map[ValueID]bool{}
	mutateAt := map[ValueID][]aliasHazard{}
	escapeAt := map[ValueID][]aliasHazard{}
	// Program order, so a hazard inside the defining block can be compared
	// against the copy itself (a write BEFORE the alias is irrelevant).
	ord := map[InstID]int{}
	order := 0
	scan := func(bid BlockID, iid InstID, inst *Inst) {
		if inst == nil {
			return
		}
		pos := order
		ord[iid] = pos
		order++
		if inst.Dst > NoVal {
			defCount[inst.Dst]++
			if defInst[inst.Dst] == nil {
				defInst[inst.Dst] = inst
			}
		}
		if inst.Op == OpMove && inst.Dst == NoVal && len(inst.Args) >= 2 {
			moveInto[inst.Args[1]] = true
		}
		if inst.Op == OpDrop && len(inst.Args) > 0 {
			dropped[inst.Args[0]] = true
		}
		for _, v := range inst.Args {
			switch aliasUse(inst, v) {
			case aliasUseMutate:
				mutateAt[v] = append(mutateAt[v], aliasHazard{bid, iid, pos})
			case aliasUseEscape:
				escapeAt[v] = append(escapeAt[v], aliasHazard{bid, iid, pos})
			}
		}
	}
	// Terminators carry operands too (OpReturn's value, a branch's condition) and
	// are NOT part of blk.Insts. A return hands the value to the caller, so it is
	// an escape; a branch operand is a scalar condition, which is a read.
	scanTerm := func(bid BlockID, t *Term) {
		if t == nil {
			return
		}
		pos := order
		order++
		for _, v := range t.Args {
			switch t.Op {
			case OpReturn:
				escapeAt[v] = append(escapeAt[v], aliasHazard{bid, NoInst, pos})
			case OpCondBr, OpSwitch:
				// scalar condition
			default:
				escapeAt[v] = append(escapeAt[v], aliasHazard{bid, NoInst, pos})
			}
		}
	}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			scan(bid, iid, m.Inst(iid))
		}
		scanTerm(bid, blk.Term)
	}

	// rewriteUses re-points every operand equal to `from` at `to`, across the
	// whole function (terminators included).
	rewriteUses := func(from, to ValueID) {
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
				for i, a := range inst.Args {
					if a == from {
						inst.Args[i] = to
					}
				}
			}
			if blk.Term != nil {
				for i, a := range blk.Term.Args {
					if a == from {
						blk.Term.Args[i] = to
					}
				}
			}
		}
	}

	// ---- decide and rewrite in PROGRAM ORDER -------------------------------
	// Rewriting as we go, rather than collecting decisions and applying them
	// afterwards, is what makes alias CHAINS correct. For `c = b` where `b` is
	// itself a forwarded alias, b's operand has already been re-pointed at the
	// surviving source by the time we look at it; collecting first left `c`
	// pointing at a value whose definition had been deleted, and the LLVM
	// backend then refused the module ("value id 124 (%vec) has no slot in
	// func 2", tests/mem-safety/nested-container-clone.no).
	var toDelete []InstID

	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil || len(inst.Args) == 0 {
				continue
			}
			// Two sources of an assignment-point copy:
			//   OpMove  — an aliasing move that insertDrops is about to turn
			//             into a deep clone because the source is still live;
			//   OpClone — the copy hir2mir emits DIRECTLY for a fresh binding
			//             of an owned slice (`b []i64 = a`, hir2mir.go:2996).
			if inst.Op != OpMove && inst.Op != OpClone {
				continue
			}
			src := inst.Args[0]
			if src <= NoVal {
				reasons["dst-not-fresh"]++
				continue
			}
			// (r4) the source must OWN its buffer. A value produced by a
			// BORROWING read — an element read, a struct field read, a
			// tagged-enum payload peel, an array slice view, or a non-cloning
			// option peel — aliases storage that ANOTHER value frees.
			//
			// This is hoisted ABOVE the clone→move rule on purpose. That rule
			// first shipped below this test, which let it rewrite a BORROWING
			// peel's clone into a move and hand the destination a buffer the
			// owner still freed — a double free, not a leak. `isBorrowRead` is
			// the single source of truth for "does this instruction hand out
			// ownership or a view", so it must be consulted before EITHER
			// decision, not just the read-only one.
			srcIsBorrow := false
			if di := defInst[src]; di != nil && (m.isBorrowRead(f, di) || m.isSliceViewOfArray(f, di)) {
				srcIsBorrow = true
			}
			// Tier C, §1.2 third bullet — "if the source is dead there is
			// nothing to copy": an OpClone whose SOURCE has no use left after
			// this instruction is a MOVE, not a copy. hir2mir emits OpClone
			// directly for a fresh binding whose initializer is a bare
			// reference to an existing owned local slice (`b []i64 = a`,
			// hir2mir.go:2994-3008 — it clones only for the KIdent case, so a
			// call result or a slice literal never reaches here), and it emits
			// it unconditionally: the source's liveness is not consulted. So
			// the transfer is paid for as a deep copy even when the source is
			// provably dead here.
			//
			// This is also where the MEASURED win is, and it is one level down
			// from the shape above. An option→slice peel that CLONES (`?[]T`,
			// see optionSlicePeelClones) hands its own destination a private
			// vecDeepClone; hir2mir then clones THAT again for the fresh
			// binding, so `s []i64 = o` pays for two deep copies of one
			// payload. Rewriting the binding's copy to a move leaves exactly
			// one. On the 508-file corpus that removes 5 clones across
			// mem-safety/nested-container-clone.no and std-new.no, with every
			// affected program's stdout byte-identical (verified), and a
			// 500k-iteration `s []i64 = o` loop flat at ~1.6 MB peak RSS
			// (control 1671168 B, this 1687552 B) — the payload is still freed
			// exactly once, only by the binding instead of by the peel's
			// temporary. See peelDst in option_peel_ownership_test.go, which
			// follows that transfer chain rather than pinning one value id.
			//
			// Rewriting the OP (rather than deleting the copy, as the read-only
			// case does) hands the decision to insertDrops' existing slice-move
			// logic: that logic keeps the zero-copy move and exempts the
			// source's drop when the source is dead, and clones it right back
			// when it is not — so this can only ever REMOVE a copy, never add
			// one, and it cannot under-clone.
			if inst.Op == OpClone && !srcIsBorrow &&
				!liveOut[bid][src] && !m.readNonDropAfterInBlock(bid, iid, src) {
				dst := moveDst(inst)
				if dst > NoVal && inst.Dst > NoVal && m.typeIsOwnedSlice(f, src) && m.typeIsOwnedSlice(f, dst) {
					inst.Op = OpMove
					reasons["clone-src-dead-to-move"]++
					continue
				}
			}
			if inst.Op == OpMove {
				// Only the three bitwise-copy shapes that insertDrops would turn
				// into a deep clone. Tagged enums are left alone: they carry
				// their own payload-ownership marks (markEnumPayloadOwners) and
				// a half-shared enum payload is a far sharper edge than a shared
				// vec.
				if !m.moveStrSharesHeap(f, inst) && !m.moveSliceSharesHeap(f, inst) && !m.moveStructSharesHeap(f, inst) {
					reasons["no-shares-heap"]++
					continue
				}
				// The source must be LIVE here, otherwise insertDrops already
				// keeps the zero-copy move and there is nothing to forward.
				if !liveOut[bid][src] && !m.readNonDropAfterInBlock(bid, iid, src) {
					reasons["src-dead"]++
					continue
				}
			} else if !m.typeIsOwnedStr(f, src) && !m.typeIsOwnedSlice(f, src) && !m.typeIsPtrStruct(f, src) {
				reasons["no-shares-heap"]++
				continue
			}
			dst := moveDst(inst)
			if dst <= NoVal || inst.Dst <= NoVal {
				// No destination, or the EmitMoveInto shape (`b = a` on an
				// already-bound b). Forwarding that means renaming a slot other
				// code may still address, so it is left to the conservative path.
				reasons["dst-not-fresh"]++
				continue
			}
			if m.isParamValue(f, dst) {
				reasons["dst-is-param"]++
				continue
			}
			// A value may be forwarded only when it is defined exactly once, so
			// that "rewrite every use" cannot miss a re-binding. A PARAMETER has
			// no defining instruction in this frame, which is fine — it is the
			// caller's storage and cannot be re-bound except through an
			// EmitMoveInto, which `moveInto` catches.
			srcDefinedOnce := defCount[src] == 1 || m.isParamValue(f, src)
			if defCount[dst] != 1 || !srcDefinedOnce || moveInto[dst] || moveInto[src] {
				reasons["multi-def"]++
				continue
			}
			if dropped[dst] {
				reasons["dst-has-drop"]++
				continue
			}
			// (r4) — computed once, above, and shared with the clone→move rule.
			if srcIsBorrow {
				reasons["src-is-borrow"]++
				continue
			}
			// (r1) every use of the alias must be a pure read.
			bad := false
			for _, h := range mutateAt[dst] {
				if h.iid != iid {
					bad = true
					break
				}
			}
			if !bad {
				for _, h := range escapeAt[dst] {
					if h.iid != iid {
						bad = true
						break
					}
				}
			}
			if bad {
				reasons["dst-bad-use"]++
				continue
			}
			// (r2) the source must not be written or escaped while the alias is
			// live. A hazard in the defining block only counts when it comes
			// AFTER the alias; elsewhere, "the alias is live in this block" is
			// the conservative test.
			aliasLiveIn := func(b BlockID) bool {
				if liveIn[b][dst] || liveOut[b][dst] {
					return true
				}
				return b == bid && m.readAfterInBlock(bid, iid, dst)
			}
			for _, hs := range [][]aliasHazard{mutateAt[src], escapeAt[src]} {
				for _, h := range hs {
					if h.iid == iid {
						continue // the aliasing copy itself reads src
					}
					if h.bid == bid {
						if h.pos > ord[iid] {
							bad = true
						}
					} else if aliasLiveIn(h.bid) {
						bad = true
					}
					if bad {
						break
					}
				}
				if bad {
					break
				}
			}
			if bad {
				reasons["src-hazard-while-alias-live"]++
				continue
			}
			// Accept: the alias IS the source from here on. Re-point every use
			// first (which also fixes up later copies that read this alias), then
			// schedule the copy itself for removal.
			rewriteUses(dst, src)
			toDelete = append(toDelete, iid)
			forwarded++
		}
	}

	if len(toDelete) == 0 {
		return
	}
	dead := map[InstID]bool{}
	for _, iid := range toDelete {
		dead[iid] = true
	}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		kept := blk.Insts[:0]
		for _, iid := range blk.Insts {
			if !dead[iid] {
				kept = append(kept, iid)
			}
		}
		blk.Insts = kept
	}
}
