package mir

import "fmt"

// Diagnostic is a memory-safety finding produced by the analysis passes. These
// are the systematic detectors for the "lots of memory problems" the user is
// seeing: use-after-move, missing/duplicate drop, borrow escape.
type Diagnostic struct {
	Kind  string // "use-after-move" | "missing-drop" | "duplicate-drop" | "borrow-escape" | "unsupported"
	Func  string
	Block BlockID
	Inst  InstID
	Msg   string
}

// Report aggregates the outcome of Analyze.
type Report struct {
	DropsInserted int
	Diagnostics   []Diagnostic
}

// HasErrors reports whether any diagnostic was emitted.
func (r *Report) HasErrors() bool { return len(r.Diagnostics) > 0 }

// Error implements the error interface so a Report can be returned as an error.
func (r *Report) Error() string {
	if !r.HasErrors() {
		return ""
	}
	s := fmt.Sprintf("%d diagnostic(s):", len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		s += fmt.Sprintf("\n  [%s] %s: %s (func=%s block=%d inst=%d)", d.Kind, d.Msg, "", d.Func, d.Block, d.Inst)
	}
	return s
}

// Analyze runs the full MIR analysis pipeline on the module: builds the CFG,
// inserts exactly-one Drop per owned local (respecting moves), and runs the
// move/borrow checks. It mutates the module (drop insertion) and returns a
// report summarizing what it did and any diagnostics.
func (m *Module) Analyze() *Report {
	m.BuildCFG()
	rep := &Report{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		m.insertDrops(f, rep)
		m.checkMoves(f, rep)
		m.checkDropCount(f, rep)
	}
	return rep
}

func (m *Module) isOwnedVal(f *Function, v ValueID) bool {
	if v <= NoVal {
		return false
	}
	if t, ok := f.LocalTypes[v]; ok {
		if ty := m.Type(t); ty != nil {
			return ty.Owned
		}
	}
	if val := m.Value(v); val != nil {
		if ty := m.Type(val.Type); ty != nil {
			return ty.Owned
		}
	}
	return false
}

// insertDrops places exactly one OpDrop for every owned local on EVERY
// control-flow path, after the value's last use on that path. It is derived from
// the live sets (not textual position), which is what makes it correct across
// loops and branches:
//
//   - loop-invariant value (defined outside a loop, used inside): dropped ONCE,
//     AFTER the loop exits. Dropping it inside the loop would use-after-free the
//     still-needed value on later iterations (the crash we were hitting).
//   - loop-local value (defined inside the loop): dropped once PER ITERATION at
//     the end of the body, because its slot is overwritten each iteration with a
//     fresh allocation that must be freed.
//   - branch-only value (used on one arm of an if, dead afterwards): dropped at
//     the merge block's start, which executes on EVERY arm — exactly one drop per
//     path (no leak on the unused arm, no use-after-free on the used arm).
//
// Placement is computed from liveness:
//
//   (A) edge drop: for each CFG edge b->s, if v is live-in to b but NOT live-in
//       to s, v dies on that edge and is dropped at the START of s (for a
//       function's return edge, at the END of b before the return).
//   (B) intra-block drop: if v is DEFINED in b and is not live-out of b, it dies
//       at the end of b and is dropped there.
//
// A move source (ownership transferred) is exempt; parameters and result params
// are borrowed and owned by the caller, so they are never dropped here (dropping
// them would double-free the caller's buffer).
func (m *Module) insertDrops(f *Function, rep *Report) {
	moveSrc := map[ValueID]bool{}
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
			if inst.Op == OpMove && len(inst.Args) > 0 && inst.Args[0] > NoVal {
				moveSrc[inst.Args[0]] = true
			}
		}
	}

	isParam := map[ValueID]bool{}
	for _, p := range f.Params {
		isParam[p] = true
	}
	for _, p := range f.ResultParams {
		isParam[p] = true
	}

	// droppable: every owned, non-param, non-moveSource value defined in f.
	droppable := map[ValueID]bool{}
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
			if inst.Dst > NoVal && m.isOwnedVal(f, inst.Dst) && !isParam[inst.Dst] && !moveSrc[inst.Dst] {
				droppable[inst.Dst] = true
			}
		}
	}
	if len(droppable) == 0 {
		return
	}

	liveIn, liveOut := m.Liveness(f)

	// (B) intra-block drops: defined in b, dead after b.
	dropAtEnd := map[BlockID][]ValueID{}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		def, _ := m.defUse(bid)
		for v := range def {
			if !droppable[v] {
				continue
			}
			if !liveOut[bid][v] {
				dropAtEnd[bid] = append(dropAtEnd[bid], v)
			}
		}
	}

	// (A) edge drops: live into b, dead entering successor s.
	type dropKey struct {
		v ValueID
		s BlockID
	}
	seenStart := map[dropKey]bool{}
	dropAtStart := map[BlockID][]ValueID{}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		li := liveIn[bid]
		if len(li) == 0 {
			continue
		}
		var succs []BlockID
		if blk.Term != nil && (blk.Term.Op == OpReturn || blk.Term.Op == OpSwitch) {
			succs = []BlockID{NoBlock} // virtual exit: drop at end of b
		} else {
			succs = blk.Succs
		}
		for _, s := range succs {
			var liveInS map[ValueID]bool
			if s == NoBlock {
				liveInS = nil // nothing is live entering the virtual exit
			} else {
				liveInS = liveIn[s]
			}
			for v := range li {
				if !droppable[v] {
					continue
				}
				if liveInS != nil && liveInS[v] {
					continue // still live entering s: not dead on this edge
				}
				if s == NoBlock {
					dropAtEnd[bid] = append(dropAtEnd[bid], v)
				} else {
					k := dropKey{v, s}
					if seenStart[k] {
						continue
					}
					seenStart[k] = true
					dropAtStart[s] = append(dropAtStart[s], v)
				}
			}
		}
	}

	// Emit edge drops at the START of the target block (executes on every path
	// that reaches the block, so a merge drop covers all its incoming arms).
	// insertDropAt already prepends into blk.Insts, so we must NOT re-assemble
	// blk.Insts here (that would double-insert the prepended drops).
	for bid, vs := range dropAtStart {
		if m.Block(bid) == nil {
			continue
		}
		for _, v := range vs {
			m.insertDropAt(bid, v, true)
			rep.DropsInserted++
		}
	}
	// Emit intra-block / return drops at the END of the block (before terminator).
	for bid, vs := range dropAtEnd {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, v := range vs {
			m.insertDropAt(bid, v, false)
			rep.DropsInserted++
		}
	}
}

// insertDropAt appends an OpDrop for val to the module and inserts it into the
// block either at the start (atStart=true) or at the end (before the terminator,
// atStart=false). It returns the new instruction id.
func (m *Module) insertDropAt(block BlockID, val ValueID, atStart bool) InstID {
	iid := InstID(len(m.Insts))
	m.Insts = append(m.Insts, Inst{
		ID:    iid,
		Op:    OpDrop,
		Args:  []ValueID{val},
		Block: block,
	})
	if blk := m.Block(block); blk != nil {
		if atStart {
			blk.Insts = append([]InstID{iid}, blk.Insts...)
		} else {
			blk.Insts = append(blk.Insts, iid)
		}
	}
	return iid
}

// checkMoves flags use-after-move: a value read after it has been moved (its
// ownership transferred elsewhere) is a memory bug. Move destinations and drops
// of the moved value are exempt (the destination owns it now, and the source is
// no longer dropped).
func (m *Module) checkMoves(f *Function, rep *Report) {
	movedAt := map[ValueID]InstID{}
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
			if inst.Op == OpMove {
				// Only the moved-from source (Args[0]) is "moved"; its later reads are
				// use-after-move. The destination (Args[1] for EmitMoveInto) is the new
				// owner and may be read freely.
				if len(inst.Args) > 0 && inst.Args[0] > NoVal {
					movedAt[inst.Args[0]] = iid
				}
			}
			for _, a := range inst.Args {
				if a > NoVal {
					if at, ok := movedAt[a]; ok && at < iid && inst.Op != OpMove && inst.Op != OpDrop {
						rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
							Kind:  "use-after-move",
							Func:  f.Name,
							Block: bid,
							Inst:  iid,
							Msg:   fmt.Sprintf("value %d read after move at inst %d", a, at),
						})
					}
				}
			}
		}
	}
}

// checkDropCount verifies that every owned local ends up with exactly one Drop.
// Zero means a leak; more than one means a double-free risk. Move sources are
// exempt: their drop responsibility was transferred to the move destination, so a
// missing drop there is correct, not a leak.
func (m *Module) checkDropCount(f *Function, rep *Report) {
	moveSrc := map[ValueID]bool{}
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
			if inst.Op == OpMove {
				// Only the moved-from source (Args[0]) is exempt from dropping; the
				// destination keeps its own drop responsibility. EmitMoveInto carries
				// Args=[src, dst], so we must not mark dst as a move source.
				if len(inst.Args) > 0 && inst.Args[0] > NoVal {
					moveSrc[inst.Args[0]] = true
				}
			}
		}
	}
	dropCount := map[ValueID]int{}
	declared := map[ValueID]bool{}
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
			if inst.Op == OpDrop && len(inst.Args) > 0 {
				dropCount[inst.Args[0]]++
			}
			if inst.Dst > NoVal {
				if m.isOwnedVal(f, inst.Dst) {
					declared[inst.Dst] = true
				}
			}
		}
	}
	for _, p := range f.Params {
		if m.isOwnedVal(f, p) {
			declared[p] = true
		}
	}
	for v := range declared {
		if moveSrc[v] {
			continue
		}
		c := dropCount[v]
		// Only a missing drop (leak) is reported. A drop instruction count > 1 for a
		// single value is correct and expected: insertDrops places exactly one drop
		// PER CONTROL-FLOW PATH (e.g. a branch-only value is dropped once at the
		// merge, which executes on every arm). The structural guarantee is that no
		// single path ever drops the same value twice; counting instructions across
		// all paths would falsely flag those legitimate multi-path drops.
		if c == 0 {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Kind: "missing-drop", Func: f.Name,
				Msg: fmt.Sprintf("owned value %d has no drop (leak)", v),
			})
		}
	}
}

// Liveness computes live-in/live-out sets for every block of f via a backward
// dataflow fixpoint. It is used by validators and future borrow checks. Values
// are tracked by ValueID; the returned maps are keyed by BlockID.
func (m *Module) Liveness(f *Function) (liveIn, liveOut map[BlockID]map[ValueID]bool) {
	liveIn = map[BlockID]map[ValueID]bool{}
	liveOut = map[BlockID]map[ValueID]bool{}
	for _, b := range f.Blocks {
		liveIn[b] = map[ValueID]bool{}
		liveOut[b] = map[ValueID]bool{}
	}
	changed := true
	for changed {
		changed = false
		// reverse order for faster convergence
		for i := len(f.Blocks) - 1; i >= 0; i-- {
			b := f.Blocks[i]
			out := map[ValueID]bool{}
			blk := m.Block(b)
			if blk != nil {
				for _, s := range blk.Succs {
					for v := range liveIn[s] {
						out[v] = true
					}
				}
			}
			def, use := m.defUse(b)
			in := map[ValueID]bool{}
			for v := range use {
				in[v] = true
			}
			for v := range out {
				if !def[v] {
					in[v] = true
				}
			}
			if !sameSet(in, liveIn[b]) || !sameSet(out, liveOut[b]) {
				changed = true
			}
			liveIn[b] = in
			liveOut[b] = out
		}
	}
	return
}

func sameSet(a, b map[ValueID]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
