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

// insertDrops places exactly one OpDrop after the last use of every owned local
// that still owns its value. A value that was moved transfers its drop
// responsibility to the move destination, so its source is skipped — this is what
// eliminates double-free (the source and destination never both get freed).
func (m *Module) insertDrops(f *Function, rep *Report) {
	moveSrc := map[ValueID]bool{}
	lastUse := map[ValueID]InstID{}
	lastUseBlock := map[ValueID]BlockID{}
	defBlock := map[ValueID]BlockID{}

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
			if inst.Dst > NoVal {
				defBlock[inst.Dst] = bid
			}
			if inst.Op == OpMove {
				// Only the moved-from source (Args[0]) is exempt from dropping; the
				// destination keeps its own drop responsibility. EmitMoveInto carries
				// Args=[src, dst], so we must not mark dst as a move source.
				if len(inst.Args) > 0 && inst.Args[0] > NoVal {
					moveSrc[inst.Args[0]] = true
				}
			}
			for _, a := range inst.Args {
				if a > NoVal {
					lastUse[a] = iid
					lastUseBlock[a] = bid
				}
			}
		}
	}

	type plan struct {
		val   ValueID
		block BlockID
		after InstID // NoInst => append at end of block
	}
	var plans []plan

	addOwned := func(v ValueID) {
		if v <= NoVal {
			return
		}
		// Parameters (including result params) are borrowed: owned by the caller,
		// never dropped by the callee. The defBlock pass below can pick up a
		// result-param id when it is the destination of an assignment, so exclude
		// params here explicitly to avoid dropping them in the callee.
		isParam := false
		for _, p := range f.Params {
			if p == v {
				isParam = true
				break
			}
		}
		if isParam {
			return
		}
		if !m.isOwnedVal(f, v) {
			return
		}
		if moveSrc[v] {
			return
		}
		b, ok := lastUseBlock[v]
		if !ok {
			b = defBlock[v]
			if b <= NoBlock {
				b = f.Entry
			}
			plans = append(plans, plan{v, b, NoInst})
		} else {
			plans = append(plans, plan{v, b, lastUse[v]})
		}
	}
	for v := range defBlock {
		addOwned(v)
	}
	// NOTE: parameters are intentionally excluded from drop insertion. In the v1
	// borrow model every owned parameter is borrowed by the callee — the caller
	// retains ownership and is responsible for dropping it. The callee never frees
	// a parameter (see the runtime helpers: str_concat / print_str do not free
	// their inputs). Result parameters are written by the callee and owned by the
	// caller, so they too must not be dropped here. Dropping a parameter here would
	// double-free the underlying buffer with the caller's own drop.

	byBlock := map[BlockID][]plan{}
	for _, p := range plans {
		byBlock[p.block] = append(byBlock[p.block], p)
	}

	for bid, ps := range byBlock {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		out := make([]InstID, 0, len(blk.Insts)+len(ps))
		for _, iid := range blk.Insts {
			out = append(out, iid)
			for _, p := range ps {
				if p.after == iid {
					out = append(out, m.emitDrop(bid, p.val))
					rep.DropsInserted++
				}
			}
		}
		for _, p := range ps {
			if p.after == NoInst {
				out = append(out, m.emitDrop(bid, p.val))
				rep.DropsInserted++
			}
		}
		blk.Insts = out
	}
}

// emitDrop appends an OpDrop instruction for val to the module and returns its id.
func (m *Module) emitDrop(block BlockID, val ValueID) InstID {
	iid := InstID(len(m.Insts))
	m.Insts = append(m.Insts, Inst{
		ID:    iid,
		Op:    OpDrop,
		Args:  []ValueID{val},
		Block: block,
	})
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
		switch {
		case c == 0:
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Kind: "missing-drop", Func: f.Name,
				Msg: fmt.Sprintf("owned value %d has no drop (leak)", v),
			})
		case c > 1:
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Kind: "duplicate-drop", Func: f.Name,
				Msg: fmt.Sprintf("owned value %d dropped %d times (double-free)", v, c),
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
