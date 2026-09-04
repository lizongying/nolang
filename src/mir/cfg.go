package mir

// BuildCFG recomputes predecessor/successor edges for every block from their
// terminators. It is idempotent and cheap; call it before any dataflow pass.
func (m *Module) BuildCFG() {
	for i := range m.Blocks {
		m.Blocks[i].Preds = m.Blocks[i].Preds[:0]
		m.Blocks[i].Succs = m.Blocks[i].Succs[:0]
	}
	for i := range m.Blocks {
		blk := &m.Blocks[i]
		if blk.Term == nil {
			continue
		}
		for _, t := range blk.Term.Targets {
			if t <= NoBlock || int(t) >= len(m.Blocks) {
				continue
			}
			blk.Succs = append(blk.Succs, t)
			// avoid duplicate predecessors from a self-cond-br
			dup := false
			for _, p := range m.Blocks[t].Preds {
				if p == BlockID(i) {
					dup = true
					break
				}
			}
			if !dup {
				m.Blocks[t].Preds = append(m.Blocks[t].Preds, BlockID(i))
			}
		}
	}
}

// defUse computes, for a single block, the set of values defined within it and the
// set of values used before their (intra-block) definition. Use is conservative
// in the forward direction: an argument that appears before the instruction that
// defines it counts as a use (live-in needed); an argument defined later in the
// same block is not a live-in. This is sufficient for correct drop placement.
func (m *Module) defUse(b BlockID) (def map[ValueID]bool, use map[ValueID]bool) {
	def = map[ValueID]bool{}
	use = map[ValueID]bool{}
	definedSoFar := map[ValueID]bool{}
	blk := m.Block(b)
	if blk == nil {
		return
	}
	for _, iid := range blk.Insts {
		inst := m.Inst(iid)
		if inst == nil {
			continue
		}
		for _, a := range inst.Args {
			if a <= NoVal {
				continue
			}
			if !definedSoFar[a] {
				use[a] = true
			}
		}
		if inst.Dst > NoVal {
			def[inst.Dst] = true
			definedSoFar[inst.Dst] = true
		}
	}
	return
}

// Dominators computes the immediate dominator of each block using the iterative
// dataflow algorithm. entry dominates itself. Returns a map block -> idom (NoBlock
// for unreachable/entry). Used by borrow-lifetime checks (a borrow must not
// escape its owner's dominator scope).
func (m *Module) Dominators(entry BlockID) map[BlockID]BlockID {
	idom := map[BlockID]BlockID{}
	// collect reachable blocks in some order
	var order []BlockID
	seen := map[BlockID]bool{}
	var visit func(b BlockID)
	visit = func(b BlockID) {
		if b <= NoBlock || seen[b] || int(b) >= len(m.Blocks) {
			return
		}
		seen[b] = true
		order = append(order, b)
		for _, s := range m.Block(b).Succs {
			visit(s)
		}
	}
	visit(entry)

	// initialize: idom[entry]=entry, others undefined
	for _, b := range order {
		if b == entry {
			idom[b] = entry
		} else {
			idom[b] = NoBlock
		}
	}
	changed := true
	for changed {
		changed = false
		for _, b := range order {
			if b == entry {
				continue
			}
			var newIdom BlockID = NoBlock
			for _, p := range m.Block(b).Preds {
				if idom[p] != NoBlock {
					if newIdom == NoBlock {
						newIdom = p
					} else {
						newIdom = intersect(m, idom, p, newIdom)
					}
				}
			}
			if newIdom != NoBlock && idom[b] != newIdom {
				idom[b] = newIdom
				changed = true
			}
		}
	}
	return idom
}

func intersect(m *Module, idom map[BlockID]BlockID, a, b BlockID) BlockID {
	for a != b {
		// walk the deeper of the two up; use block order index as a proxy for depth
		if blockIndex(m, a) < blockIndex(m, b) {
			b = idom[b]
		} else {
			a = idom[a]
		}
		if a == NoBlock || b == NoBlock {
			return NoBlock
		}
	}
	return a
}

func blockIndex(m *Module, b BlockID) int {
	for i, blk := range m.Blocks {
		if blk.ID == b {
			return i
		}
	}
	return 0
}
