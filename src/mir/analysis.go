package mir

import (
	"fmt"
	"sort"
	"strings"
)

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
// escapingValues returns the set of values in f that OUTLIVE the frame: the
// result / out parameters, anything stored into a struct field or container,
// and — transitively — anything copied into one of those.
//
// The propagation is a linear worklist over srcOf, so escape THROUGH ANOTHER
// VARIABLE (`y = x[0..1]` then `res = y`) is caught, which a
// look-at-the-assignment-target-only rule would miss.
func (m *Module) escapingValues(f *Function) map[ValueID]bool {
	escaped := map[ValueID]bool{}
	var work []ValueID
	mark := func(v ValueID) {
		if v <= NoVal || escaped[v] {
			return
		}
		escaped[v] = true
		work = append(work, v)
	}
	for _, rp := range f.ResultParams {
		mark(rp)
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
			switch inst.Op {
			case OpSetField:
				// Args = [receiver, value]: stored into the struct, so it
				// outlives the frame.
				if len(inst.Args) >= 2 {
					mark(inst.Args[1])
				}
			case OpIndexStore:
				// Args = [container, index, value]
				if len(inst.Args) >= 3 {
					mark(inst.Args[2])
				}
			}
		}
	}
	// srcOf[v] = the values v was COPIED FROM. Built once, then the escape set
	// is propagated with a linear worklist instead of rescanning every
	// instruction per escaping value (quadratic on large std functions).
	srcOf := map[ValueID][]ValueID{}
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
			switch inst.Op {
			case OpMove:
				// Emit(OpMove, [src])     -> Dst is the fresh copy
				// EmitMoveInto(dst, src)  -> Dst == NoVal, Args = [src, dst]
				if inst.Dst > NoVal && len(inst.Args) >= 1 {
					srcOf[inst.Dst] = append(srcOf[inst.Dst], inst.Args[0])
				}
				if len(inst.Args) >= 2 && inst.Args[1] > NoVal {
					srcOf[inst.Args[1]] = append(srcOf[inst.Args[1]], inst.Args[0])
				}
			case OpOptionWrap:
				if inst.Dst > NoVal && len(inst.Args) >= 1 {
					srcOf[inst.Dst] = append(srcOf[inst.Dst], inst.Args[0])
				}
			}
		}
	}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		for _, s := range srcOf[v] {
			mark(s)
		}
	}
	return escaped
}

// frameEscapingValues returns the values in f that provably OUTLIVE the frame.
//
// It is the STRICT counterpart of escapingValues. That one is deliberately
// over-approximating — it treats every OpSetField / OpIndexStore target as
// escaping — which is right for slice views, where the only consequence of a
// false positive is a redundant copy. It is wrong for `&T` borrows, which
// cannot be demoted to a copy: `h.p = pt` into a LOCAL holder would be
// rejected even though nothing leaves the frame.
//
// So here a stored value escapes only when the CONTAINER escapes: the escape
// set is seeded from the result / out parameters and propagated along
// "container escapes => stored value escapes" and "copy escapes => source
// escapes" edges.
func (m *Module) frameEscapingValues(f *Function) map[ValueID]bool {
	// deps[v] = the values that escape WHEN v escapes.
	deps := map[ValueID][]ValueID{}
	add := func(v, w ValueID) {
		if v > NoVal && w > NoVal {
			deps[v] = append(deps[v], w)
		}
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
			switch inst.Op {
			case OpMove:
				// Emit(OpMove, [src])     -> Dst is the fresh copy
				// EmitMoveInto(dst, src)  -> Dst == NoVal, Args = [src, dst]
				add(inst.Dst, inst.Args[0])
				if len(inst.Args) >= 2 {
					add(inst.Args[1], inst.Args[0])
				}
			case OpOptionWrap:
				add(inst.Dst, inst.Args[0])
			case OpSetField:
				// Args = [receiver, value]: the value outlives the frame only
				// if the struct does.
				if len(inst.Args) >= 2 {
					add(inst.Args[0], inst.Args[1])
				}
			case OpIndexStore:
				// Args = [container, index, value]
				if len(inst.Args) >= 3 {
					add(inst.Args[0], inst.Args[2])
				}
			}
		}
	}
	escaped := map[ValueID]bool{}
	var work []ValueID
	mark := func(v ValueID) {
		if v <= NoVal || escaped[v] {
			return
		}
		escaped[v] = true
		work = append(work, v)
	}
	for _, rp := range f.ResultParams {
		mark(rp)
	}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		for _, w := range deps[v] {
			mark(w)
		}
	}
	return escaped
}

// checkBorrowEscapes rejects a `&T` (view) borrow that OUTLIVES the storage it
// points at.
//
// A view is a raw pointer to another value's slot. Borrowing from a PARAMETER
// is fine — the caller owns it, so it outlives this frame — and that is exactly
// what a method-result view (`v ?&point = self`) does. Borrowing from a LOCAL
// and then letting the pointer escape (returned, or written into a struct the
// caller receives) leaves the caller holding an address into a dead frame:
//
//	mk = () (h holder) { pt = point{...}; h = holder { p: pt, n: 0 } }
//
// `h` is the result out-param, `pt` is a local, so `h.p` dangles the moment
// `mk` returns — this silently read 0 instead of 1. There is no way to repair
// it at codegen time (the field is a pointer by declaration), so the program is
// REFUSED rather than miscompiled.
func (m *Module) checkBorrowEscapes(f *Function, rep *Report) {
	isParam := map[ValueID]bool{}
	for _, p := range f.Params {
		isParam[p] = true
	}
	escaped := m.frameEscapingValues(f)
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil || inst.Op != OpBorrow || len(inst.Args) == 0 {
				continue
			}
			if !escaped[inst.Dst] {
				continue
			}
			src := inst.Args[0]
			// Borrowed from a parameter: the caller owns the storage, so the
			// view remains valid after this frame returns.
			if isParam[src] {
				continue
			}
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				Kind:  "borrow-escape",
				Func:  f.Name,
				Block: bid,
				Inst:  iid,
				Msg: fmt.Sprintf("view %s borrows a local value but escapes the function; "+
					"the borrowed storage dies with the frame, so the caller would hold a "+
					"dangling pointer (borrow from a parameter or from 'self' instead)",
					m.typeStr(inst.Type)),
			})
		}
	}
}

// demoteUnsafeSliceViews turns an OpSliceOp's VIEW flag back off whenever the
// borrowed sub-range cannot be proven to stay valid for as long as the view is
// live. Two hazards, both of which would otherwise leave a dangling alias:
//
//  1. ESCAPE. The result outlives the frame that owns the source — moved into a
//     result / out parameter, stored into a struct field or container, or moved
//     into a value that itself escapes. The caller would be handed a pointer
//     into storage that dies with the frame (tests/slice1.no,
//     tests/move-slice.no).
//
//  2. CLOBBER. The source container's backing buffer is freed or replaced at or
//     after the point the slice is taken — reassigned (`arr = [...]`), mutated
//     through a call that may grow or reallocate it, or written anywhere inside
//     a loop, where a lexically earlier write still executes after the slice on
//     the next iteration (tests/mem-safety/slice-view-escape.no, test 5).
//
// A slice view aliases the source's buffer and carries cap == 0 so it is never
// freed; either hazard turns that into a use-after-free, so the sub-range is
// materialized as an owned copy instead.
//
// The escape set is computed with a small worklist over the instruction stream;
// it is deliberately transitive, so a view that escapes THROUGH ANOTHER VARIABLE
// (`y = x[0..1]` then `res = y`) is caught, which a look-at-the-target-only rule
// would miss.
func (m *Module) demoteUnsafeSliceViews() {
	for fi := range m.Funcs {
		f := &m.Funcs[fi]
		if f.IsExtern {
			continue
		}
		escaped := m.escapingValues(f)
		// ---- program order -------------------------------------------------
		// A linear index over (block, instruction) so we can ask "does the
		// source get clobbered AFTER the slice is taken?".
		ord := map[InstID]int{}
		blockIdx := map[BlockID]int{}
		n := 0
		for bi, bid := range f.Blocks {
			blockIdx[bid] = bi
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				ord[iid] = n
				n++
			}
		}
		// A block sits inside a loop when one of its predecessors appears at or
		// after it in program order (a back edge). There, program order says
		// nothing about execution order across iterations, so any write to the
		// source at all is treated as a clobber.
		inLoop := map[BlockID]bool{}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, p := range blk.Preds {
				if pi, ok := blockIdx[p]; ok && pi >= blockIdx[bid] {
					inLoop[bid] = true
					break
				}
			}
		}
		// clobbers[v] = program-order positions at which v's backing buffer may
		// be freed or replaced, invalidating every alias into it.
		clobbers := map[ValueID][]int{}
		addClobber := func(v ValueID, at int) {
			if v > NoVal {
				clobbers[v] = append(clobbers[v], at)
			}
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
				at := ord[iid]
				switch inst.Op {
				case OpMove:
					// `dst = src` (EmitMoveInto records the target in Args[1]).
					// Either shape replaces what dst held, and the drop that
					// follows frees the old buffer.
					addClobber(inst.Dst, at)
					if len(inst.Args) >= 2 {
						addClobber(inst.Args[1], at)
					}
				case OpCall, OpCallExtern, OpCallFFI:
					// A callee handed the container may reassign, grow or free
					// it; conservatively treat every argument as clobbered.
					for _, a := range inst.Args {
						addClobber(a, at)
					}
				}
			}
		}

		// Demote every slice view that is not provably safe to an owned copy.
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil || inst.Op != OpSliceOp {
					continue
				}
				if inst.Int&SliceFlagView == 0 {
					continue
				}
				if escaped[inst.Dst] {
					inst.Int &= ^int64(SliceFlagView)
					continue
				}
				if len(inst.Args) == 0 {
					continue
				}
				// The view aliases Args[0]'s buffer.
				src := inst.Args[0]
				at := ord[iid]
				if inLoop[bid] {
					if len(clobbers[src]) > 0 {
						inst.Int &= ^int64(SliceFlagView)
					}
					continue
				}
				for _, c := range clobbers[src] {
					if c >= at {
						inst.Int &= ^int64(SliceFlagView)
						break
					}
				}
			}
		}
	}
}

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
		m.checkBorrowEscapes(f, rep)
	}
	return rep
}

// isOwnedVal reports whether v is an OWNED value in the pre-existing sense
// (`Type.Owned`: str / vec / []T / map / ?owned).
//
// This is the LOWERER-facing predicate, and its meaning is narrower than it
// looks: hir2mir uses it (via isOwnedLocal) to decide whether a binding
// `let x = y` ALIASES `y` or gets its own copied slot — owned values alias, so
// only one drop is needed for the shared slot. Widening it would silently turn
// every struct binding into an alias.
//
// The DROP machinery must therefore ask a different question; see
// dropOwnsHeap below.
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

// dropOwnsHeap reports whether v must be freed when it dies. It is
// isOwnedVal's answer PLUS structs whose pointer layout gives them
// separately-allocated pointees (Module.typeOwnsHeap).
//
// The two predicates are deliberately separate. `Type.Owned` is stamped into
// TypeMap at intern time and doubles as the lowerer's "does a binding alias"
// signal; a struct is not owned in that sense (binding one must COPY it), yet
// it does own heap that a drop has to free.
func (m *Module) dropOwnsHeap(f *Function, v ValueID) bool {
	if v <= NoVal {
		return false
	}
	if t, ok := f.LocalTypes[v]; ok {
		if ty := m.Type(t); ty != nil {
			return m.typeOwnsHeap(ty)
		}
	}
	if val := m.Value(v); val != nil {
		if ty := m.Type(val.Type); ty != nil {
			return m.typeOwnsHeap(ty)
		}
	}
	return false
}

// valueTypeOf resolves v's nolang type the same way isOwnedVal / dropOwnsHeap
// (and codegen's ptype) do: the function-local type table first, then the value
// table. Returns nil when the type is unknown.
func (m *Module) valueTypeOf(f *Function, v ValueID) *Type {
	if v <= NoVal {
		return nil
	}
	if t, ok := f.LocalTypes[v]; ok {
		if ty := m.Type(t); ty != nil {
			return ty
		}
	}
	if val := m.Value(v); val != nil {
		if ty := m.Type(val.Type); ty != nil {
			return ty
		}
	}
	return nil
}

// readAfterInBlock reports whether v is read by any instruction that comes AFTER
// `after` in the same block, in program order.
//
// Liveness alone cannot answer this. `liveOut[b][v]` says "v is read in some
// SUCCESSOR of b", so a value whose last read sits in the same block as the
// instruction under test reports liveOut == false even though it is plainly
// still needed — and `h2 = h; print(h.p.x)` is exactly that shape, which would
// wrongly classify a copy as a move.
//
// This mirrors the linear program-order index demoteUnsafeSliceViews builds
// (`ord[InstID]`); a loop back-edge makes program order a conservative
// approximation, which is the safe direction here: it can only make us choose
// CLONE over MOVE.
func (m *Module) readAfterInBlock(bid BlockID, after InstID, v ValueID) bool {
	if v <= NoVal {
		return false
	}
	blk := m.Block(bid)
	if blk == nil {
		return false
	}
	seen := false
	for _, iid := range blk.Insts {
		if iid == after {
			seen = true
			continue
		}
		if !seen {
			continue
		}
		inst := m.Inst(iid)
		if inst == nil {
			continue
		}
		for _, a := range inst.Args {
			if a == v {
				return true
			}
		}
	}
	return false
}

// moveStructHasPtrFields reports whether an OpMove's payload is a struct with
// pointer-laid-out fields, i.e. whether a bitwise copy of it would make the
// source and destination SHARE their pointees.
//
// The test is on the DESTINATION when the move has one (the copy is typed by the
// destination in emitMove), falling back to the source. Both shapes of OpMove
// are covered: `b.Emit(OpMove, typ, [src])` (Dst set) and
// `EmitMoveInto(dst, src)` (Dst NoVal, Args=[src, dst]).
func (m *Module) moveStructHasPtrFields(f *Function, inst *Inst) bool {
	// NOT gated on FieldPtrLayout: typeIsPtrStruct walks FieldIsPointer, which
	// is the authority on whether a field is a `%T*`. Gating on the global
	// switch here would skip the clone promotion for a `#{inline=false}` field
	// in the default state, leaving source and destination sharing one pointee
	// that both then free.
	if inst.Op != OpMove || len(inst.Args) == 0 {
		return false
	}
	dst := inst.Dst
	if dst == NoVal && len(inst.Args) >= 2 {
		dst = inst.Args[1]
	}
	if m.typeIsPtrStruct(f, dst) {
		return true
	}
	return m.typeIsPtrStruct(f, inst.Args[0])
}

// moveStructSharesHeap reports whether a BITWISE copy of this OpMove's payload
// would leave source and destination sharing heap — either through
// pointer-laid-out fields (pointees) or through an inline owned leaf (`str`),
// whose {len,cap,data} descriptor is copied while its buffer is not.
//
// Both cases need the same treatment: promote to OpClone while the source is
// still live, so each side ends up owning its own memory.
//
// The leaf half is deliberately NOT gated on FieldPtrLayout. Sharing an inline
// str buffer is a property of the bitwise copy, not of the field layout flip —
// and emitSetField now clones AND frees the old occupant unconditionally, so
// leaving the copy sharing would make that free a use-after-free in BOTH switch
// states, not just under the pointer layout.
func (m *Module) moveStructSharesHeap(f *Function, inst *Inst) bool {
	if m.moveStructHasPtrFields(f, inst) {
		return true
	}
	if inst.Op != OpMove || len(inst.Args) == 0 {
		return false
	}
	dst := inst.Dst
	if dst == NoVal && len(inst.Args) >= 2 {
		dst = inst.Args[1]
	}
	return m.typeIsLeafStruct(f, dst) || m.typeIsLeafStruct(f, inst.Args[0])
}

// typeIsLeafStruct reports whether value v has a struct type with an inline
// owned leaf (`str`) that a bitwise copy would share.
func (m *Module) typeIsLeafStruct(f *Function, v ValueID) bool {
	if v <= NoVal {
		return false
	}
	var tid TypeID = NoType
	if t, ok := f.LocalTypes[v]; ok {
		tid = t
	} else if val := m.Value(v); val != nil {
		tid = val.Type
	}
	ty := m.Type(tid)
	if ty == nil || ty.Kind != KindStruct {
		return false
	}
	return m.StructHasOwnedLeafFields(m.StructKeyOf(ty.Raw))
}

// typeIsPtrStruct reports whether value v has a struct type that owns
// separately-allocated pointees.
func (m *Module) typeIsPtrStruct(f *Function, v ValueID) bool {
	if v <= NoVal {
		return false
	}
	var tid TypeID = NoType
	if t, ok := f.LocalTypes[v]; ok {
		tid = t
	} else if val := m.Value(v); val != nil {
		tid = val.Type
	}
	ty := m.Type(tid)
	if ty == nil || ty.Kind != KindStruct {
		return false
	}
	return m.StructHasPtrFields(m.StructKeyOf(ty.Raw))
}

// isSliceViewOfArray reports whether inst is an OpSliceOp whose receiver is a
// fixed array (`[N x T]`). Such a slice is a VIEW over the array's own stack
// storage (emitted by emitSliceOp's fixed-array->%vec aliasing path), so it
// owns no backing buffer and must NOT be vec_free'd. Dropping it would free the
// fixed array's stack memory and abort (the arr-slice.no "pointer being freed
// was not allocated" crash under NOLANG_MIR=3). A slice of a %vec or %str-long
// gets its own freshly-malloc'd copy and IS owned, so it is still dropped.
func (m *Module) isSliceViewOfArray(f *Function, inst *Inst) bool {
	if inst.Op != OpSliceOp || len(inst.Args) == 0 {
		return false
	}
	recvT := NoType
	if t, ok := f.LocalTypes[inst.Args[0]]; ok {
		recvT = t
	} else if val := m.Value(inst.Args[0]); val != nil {
		recvT = val.Type
	}
	if ty := m.Type(recvT); ty != nil && ty.Kind == KindArray {
		if dt := m.Type(inst.Type); dt != nil && dt.Kind == KindSlice {
			return true
		}
	}
	return false
}

// isBorrowRead reports whether inst yields a BORROWED value: a read of a
// container/struct element that aliases the owner's storage and therefore must
// NOT be dropped by the reader. The owner (vec / array / str / map / struct)
// owns and drops the element; a read merely borrows it.
//
// The ops that can yield a borrowed read are OpIndex (array/slice/str/map
// element read) and OpGetField (struct field read); see each branch below for
// the exact condition, which is NOT simply "the destination is owned".
// emitIndex lowers it by GEP + load with NO clone — the destination aliases
// the element in place (for constant elements the destination's data pointer
// points straight into read-only global memory). Dropping a borrowed read
// would `free` the owner's storage (or, for constant elements, read-only
// memory) and abort, exactly the `trace/BPT trap` we hit for `['a','b','c']
// .to-str()`: the loop read `.[i]` into a local and `insertDrops` emitted
// `@str_free` on it. The principled, lowering-consistent model is that an
// element READ borrows — the container owns the element and frees it once.
// (Ownership transfer out of a container is a distinct op, e.g. the `pop` /
// `remove` methods, never a bare OpIndex; those are handled by the move
// machinery instead.)
func (m *Module) isBorrowRead(f *Function, inst *Inst) bool {
	// OpIndex: array/slice/str/map element read aliases the owner's storage.
	if inst.Op == OpIndex {
		return m.dropOwnsHeap(f, inst.Dst)
	}
	// OpGetField: struct field read. There are two shapes, and which one
	// applies is decided by whether codegen CLONED the result:
	//
	//   - owned `str` field -> emitGetField calls @str_clone, so the read
	//     result owns a PRIVATE heap buffer. This is not a borrow at all, and
	//     suppressing its drop leaks one heap copy per read (a loop reading
	//     `s.name` leaked a copy every iteration). Return false so insertDrops
	//     frees it — the clone is exactly what makes that safe: the struct
	//     frees its own buffer, the reader frees its copy, no double-free.
	//   - every other owned field (e.g. `self.keys: []str` -> %vec) -> NOT
	//     cloned (there is no @vec_clone in the runtime), so the result really
	//     does alias the struct's storage. Dropping it would free the struct's
	//     internal buffer, causing a double-free when the struct itself is
	//     later freed (or when another getfield re-reads the same field and
	//     the now-freed buffer is accessed). The struct owns its fields; a
	//     field read borrows. This mirrors the OpIndex rationale above.
	//
	// The test must mirror emitGetField's clone condition EXACTLY. That
	// condition is `owned && fieldLT == "%str-long"`, and ptype derives both
	// halves from the same Type: llvmTypeOf(ty) == "%str-long" iff
	// ty.Kind == KindStr, and owned == ty.Owned. Checking Raw == "str" instead
	// would be a proxy that a future alias/qualified spelling could break.
	if inst.Op == OpGetField {
		if ty := m.valueTypeOf(f, inst.Dst); ty != nil && ty.Owned && ty.Kind == KindStr {
			return false
		}
		return m.dropOwnsHeap(f, inst.Dst)
	}
	// OpMove that peels an option into a SLICE: emitMove copies the %vec triple
	// out of the option's payload slot, so the result ALIASES the option's
	// backing store (only the %str-long peel clones, via @str_clone). Dropping
	// the copy frees the option's buffer out from under it. The option is the
	// owner and frees the payload on its own drop (see the matching exemption
	// from `moveSrc` in insertDrops).
	if inst.Op == OpMove && len(inst.Args) > 0 && inst.Args[0] > NoVal && inst.Dst > NoVal {
		if st := m.valueTypeOf(f, inst.Args[0]); st != nil && st.Kind == KindOption {
			if dt := m.valueTypeOf(f, inst.Dst); dt != nil && dt.Kind == KindSlice {
				return m.dropOwnsHeap(f, inst.Dst)
			}
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
//	(A) edge drop: for each CFG edge b->s, if v is live-in to b but NOT live-in
//	    to s, v dies on that edge and is dropped at the START of s (for a
//	    function's return edge, at the END of b before the return).
//	(B) intra-block drop: if v is DEFINED in b and is not live-out of b, it dies
//	    at the end of b and is dropped there.
//
// A move source (ownership transferred) is exempt; parameters and result params
// are borrowed and owned by the caller, so they are never dropped here (dropping
// them would double-free the caller's buffer).
func (m *Module) insertDrops(f *Function, rep *Report) {
	// Liveness is needed to decide whether a constructor store CONSUMES its
	// value (only when the value is dead after the store — a still-live value
	// keeps its own drop at its last use).
	liveIn, liveOut := m.Liveness(f)

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
			// STRUCT WITH POINTER FIELDS: `b = a` lowers to OpMove, and
			// emitMove lowers OpMove as a BITWISE copy. Under the inline layout
			// that *is* a value copy, so it was always correct. Under the
			// pointer layout it copies only the POINTER, so `b` and `a` would
			// share one pointee and `b.p.x = 9` would be visible through `a`
			// (tests/mem-safety/nested-container-clone.no, test 8).
			//
			// Liveness answers the clone-vs-move question directly: liveOut is
			// "this value is read on some path after here", which is exactly the
			// may-read set the plan's L3 calls for.
			//
			//   source still live  -> CLONE. Rewrite to OpClone, which
			//     isTransferringMove does NOT treat as a transfer, so BOTH sides
			//     keep their drop and each frees its own pointee.
			//   source provably dead -> MOVE. Keep the bitwise copy (zero-copy)
			//     and exempt the source from dropping, as before.
			//
			// OpClone and OpMove have the same def/use shape, so rewriting the
			// op does not invalidate the liveness sets computed above.
			if m.moveStructSharesHeap(f, inst) {
				if liveOut[bid][inst.Args[0]] || m.readAfterInBlock(bid, iid, inst.Args[0]) {
					inst.Op = OpClone
				} else {
					moveSrc[inst.Args[0]] = true
				}
				continue
			}
			if isTransferringMove(inst) && !m.isOptionPeelMove(f, inst) {
				moveSrc[inst.Args[0]] = true
			}
			// OpOptionWrap transfers ownership of its payload into the option
			// (the option's drop is the single free site), so the payload value
			// must be exempt from dropping — just like a move source. Without
			// this an owned payload (str/vec/heap-option) is str_freed both on
			// its own drop and on the option's drop -> double free.
			if inst.Op == OpOptionWrap && len(inst.Args) > 0 && inst.Args[0] > NoVal {
				moveSrc[inst.Args[0]] = true
			}
			// OpStrFromVec reinterprets a []byte as a str WITHOUT copying: the
			// result aliases the slice's buffer, so the slice must not be
			// dropped separately (double free -> trace/BPT trap in
			// tests/mem-safety/bug12-builtin-slice-to-str.no).
			if inst.Op == OpStrFromVec && len(inst.Args) > 0 && inst.Args[0] > NoVal {
				moveSrc[inst.Args[0]] = true
			}
			// A struct-literal field store (OpSetField with MovesArg) consumes
			// its value: ownership transfers into the field, so the value's
			// temporary must not be freed separately — that would free a buffer
			// the (possibly escaped) struct still points to. Only when the value
			// is dead after the store; a value still live afterwards keeps its
			// drop at its last use.
			// A tagged-enum constructor consumes its payload fields: the variant
			// value holds them inline in its payload union, so their ownership
			// transfers into the enum and they must not be reported as leaked.
			if inst.Op == OpEnumNew {
				for _, a := range inst.Args {
					if a > NoVal {
						moveSrc[a] = true
					}
				}
			}
			if inst.Op == OpSetField && inst.MovesArg && len(inst.Args) >= 2 && inst.Args[1] > NoVal {
				if !liveOut[bid][inst.Args[1]] {
					moveSrc[inst.Args[1]] = true
				}
			}
			// A tagged-enum constructor CONSUMES its payload fields: the
			// variant value holds them inline in its payload union, so the
			// source temporaries must not be freed separately — that would
			// leave the enum holding a freed buffer (tests/test-tagged-enum.no:
			// `q b-res = ok('hi')` printed nothing because the string was
			// freed at the end of the enclosing block).
			if inst.Op == OpEnumNew {
				for _, a := range inst.Args {
					if a > NoVal && !liveOut[bid][a] {
						moveSrc[a] = true
					}
				}
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
			if inst.Dst > NoVal && m.dropOwnsHeap(f, inst.Dst) && !isParam[inst.Dst] && !moveSrc[inst.Dst] && !m.isSliceViewOfArray(f, inst) && !m.isBorrowRead(f, inst) {
				droppable[inst.Dst] = true
			}
		}
	}
	if len(droppable) == 0 {
		return
	}

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

	// Loop headers: a block that can reach one of its own predecessors via a
	// back-edge. Dropping a value at a loop-header's START (or END) is unsafe
	// because the header executes on EVERY iteration: a value that merely dies
	// entering the header (e.g. a pre-loop local last used before the loop) would
	// be freed once per iteration -> multi-free / use-after-free. For such edges
	// we drop at the SOURCE block's END instead (the pre-loop block runs once),
	// and only fall back to dropping at the TARGET's START when the SOURCE itself
	// is a loop header (header -> exit edge), which also runs once per exit.
	isHeader := map[BlockID]bool{}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, p := range blk.Preds {
			if m.blockReaches(bid, p) {
				isHeader[bid] = true
				break
			}
		}
	}

	// (A) edge drops: live into b, dead entering successor s.
	type startKey struct {
		v ValueID
		s BlockID
	}
	type endKey struct {
		v ValueID
		b BlockID
	}
	seenStart := map[startKey]bool{}
	seenEnd := map[endKey]bool{}
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
		// For each owned value live into b, decide WHERE its (per-path) drop goes.
		// A drop must run exactly once on every path that no longer needs v:
		//   - if v dies entering EVERY outgoing edge (and the return edge): a single
		//     drop at the END of b is correct and runs once per path;
		//   - if v dies entering SOME successors but is still LIVE into others, the
		//     drop must go at the START of each dead successor so it executes only
		//     on that path. Dropping at the source block's end would free v before a
		//     sibling path that still reads it => use-after-free (the conditional /
		//     match-block crash: `ka`/`kb` compared in the guard then re-read in the
		//     other arm).
		for v := range li {
			if !droppable[v] {
				continue
			}
			deadSuccs := []BlockID{}
			liveIntoAnySucc := false
			returnEdgeDead := false
			for _, s := range succs {
				if s == NoBlock {
					returnEdgeDead = true // v dies at the function return
					continue
				}
				if liveIn[s][v] {
					liveIntoAnySucc = true
				} else {
					deadSuccs = append(deadSuccs, s)
				}
			}
			if len(deadSuccs) == 0 && !returnEdgeDead {
				continue // live into every successor: not dead on any edge
			}
			if liveIntoAnySucc {
				// Dies on some edges, live on others: drop at the START of each
				// dead successor (executes only on that path).
				for _, s := range deadSuccs {
					k := startKey{v, s}
					if seenStart[k] {
						continue
					}
					seenStart[k] = true
					dropAtStart[s] = append(dropAtStart[s], v)
				}
				// returnEdgeDead with a live sibling cannot happen (a return edge
				// leaves the function); if it ever did, the source-end drop below
				// would wrongly free the live sibling, so we deliberately do NOT
				// emit it here.
				continue
			}
			// Dead on ALL outgoing edges (and/or the return edge): one drop at the
			// END of b runs once per path and is safe for every successor.
			k := endKey{v, bid}
			if seenEnd[k] {
				continue
			}
			seenEnd[k] = true
			dropAtEnd[bid] = append(dropAtEnd[bid], v)
		}
	}

	// Emit edge drops at the START of the target block (executes on every path
	// that reaches the block, so a merge drop covers all its incoming arms).
	// insertDropAt already prepends into blk.Insts, so we must NOT re-assemble
	// blk.Insts here (that would double-insert the prepended drops).
	//
	// A start-drop is REDUNDANT when every path leaving its block runs into
	// another block that already drops the same value on entry. This is exactly
	// the `break` shape: `i > 2 -> break` compiles to a trampoline block whose
	// only instruction is `br <loop-exit>`, and the loop exit legitimately needs
	// a start-drop for the loop-invariant value (it dies on the header's exit
	// edge). Emitting one for the trampoline too freed the value TWICE on the
	// break path — the loop-invariant buffer/string was released at the
	// trampoline's start and again at the loop exit's start, aborting the
	// process (SIGTRAP/SIGABRT) on every
	// `{ ...; cond -> break } (true)` loop that reads an owned value declared
	// before the loop (minimal repro: `s str = 'xy'` + a loop that prints `s`
	// and breaks).
	//
	// Only single-successor chains are followed, so a justification can never
	// sit on a path a branch could skip. Blocks are visited in a deterministic
	// ascending order and a skip is only ever justified by a block that has NOT
	// been skipped itself, so a cycle of droppers can never justify removing
	// every drop in the cycle — at least one always survives. The residual risk
	// is the benign direction: over-removal leaks, it cannot double free.
	startDropSet := map[BlockID]map[ValueID]bool{}
	for bid, vs := range dropAtStart {
		startDropSet[bid] = map[ValueID]bool{}
		for _, v := range vs {
			startDropSet[bid][v] = true
		}
	}
	skipped := map[BlockID]map[ValueID]bool{}
	order := make([]BlockID, 0, len(dropAtStart))
	for bid := range dropAtStart {
		order = append(order, bid)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, bid := range order {
		vs := dropAtStart[bid]
		kept := vs[:0]
		for _, v := range vs {
			if m.redundantStartDrop(bid, v, startDropSet, skipped, map[BlockID]bool{}) {
				if skipped[bid] == nil {
					skipped[bid] = map[ValueID]bool{}
				}
				skipped[bid][v] = true
				continue
			}
			kept = append(kept, v)
		}
		dropAtStart[bid] = kept
	}
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

// redundantStartDrop reports whether every path leaving block b reaches a block
// that unconditionally drops v at its START — which makes b's own start-drop a
// redundant second free.
//
// It deliberately walks ONLY through single-successor blocks: on such a chain
// there is no branch that could avoid the downstream drop, so a drop placed at
// the chain's end is guaranteed to run for every path out of b. A block with two
// or more successors returns false immediately, and so does a revisit (a cycle),
// which keeps the caller's drop.
//
// `skipped` holds the values already deemed redundant; a block that was skipped
// cannot serve as justification for another skip, so a cycle cannot cancel out
// all of its own drops.
func (m *Module) redundantStartDrop(b BlockID, v ValueID, startDrop, skipped map[BlockID]map[ValueID]bool, seen map[BlockID]bool) bool {
	if seen[b] {
		return false
	}
	seen[b] = true
	blk := m.Block(b)
	if blk == nil || len(blk.Succs) != 1 {
		return false
	}
	t := blk.Succs[0]
	if startDrop[t][v] && !skipped[t][v] {
		return true
	}
	return m.redundantStartDrop(t, v, startDrop, skipped, seen)
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

// valueSet is a small set of ValueIDs used by the move dataflow.
type valueSet map[ValueID]bool

func cloneSet(s valueSet) valueSet {
	out := make(valueSet, len(s))
	for k := range s {
		out[k] = true
	}
	return out
}

// intersectSet returns s ∩ o (a new set).
func intersectSet(s, o valueSet) valueSet {
	out := valueSet{}
	if len(s) < len(o) {
		s, o = o, s
	}
	for k := range o {
		if s[k] {
			out[k] = true
		}
	}
	return out
}

func equalSet(a, b valueSet) bool {
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

// isTransferringMove reports whether an OpMove transfers ownership out of its
// source. A SELF-move (`move [x -> x]`, emitted by the lowering for a
// redundant re-assignment such as `x = x`) copies a slot onto itself and moves
// nothing: treating it as a transfer made the very next `drop x` look like a
// double-free ("value 1400 dropped after move"), which blocked the MIR backend
// on tests/test-str.no (std str.replace-n).
// isOptionPeelMove reports whether inst is the OpMove that reads an option's
// payload out into a slice value (`x = opt` where opt : ?[]T). That move is a
// BORROW, not an ownership transfer: emitMove copies the %vec triple out of the
// option's payload slot (only a %str-long peel clones, via @str_clone), so the
// extracted slice ALIASES the option's backing store. Treating it as a transfer
// exempted the OPTION from dropping and left the copy as the only free site, so
// the buffer was freed while the option was still live — `v ?[]i64 = [10,20,30]`
// followed by `v.len().to-str()` (which peels) and then `v[0]` read freed memory
// and printed 0. The option keeps ownership and frees the payload exactly once
// on its own drop; isBorrowRead exempts the extracted copy (no @vec_clone).
func (m *Module) isOptionPeelMove(f *Function, inst *Inst) bool {
	if inst == nil || inst.Op != OpMove || len(inst.Args) == 0 || inst.Args[0] <= NoVal || inst.Dst <= NoVal {
		return false
	}
	st := m.valueTypeOf(f, inst.Args[0])
	if st == nil || st.Kind != KindOption {
		return false
	}
	dt := m.valueTypeOf(f, inst.Dst)
	return dt != nil && dt.Kind == KindSlice
}

func isTransferringMove(inst *Inst) bool {
	if inst == nil || inst.Op != OpMove || len(inst.Args) == 0 || inst.Args[0] <= NoVal {
		return false
	}
	return len(inst.Args) < 2 || inst.Args[1] != inst.Args[0]
}

// checkMoves flags use-after-move in nolang's sense: a value DROPPED after it
// has been moved (its ownership transferred to the move destination) is a
// double-free — the destination already owns the heap pointer, so dropping the
// source frees the same pointer twice. nolang's OpMove is a BITWISE COPY
// (emitMove does load+store), NOT a C++-style move that invalidates the source,
// so a plain READ of a moved value is always SAFE and is deliberately NOT
// flagged. Flagging reads was a false positive that blocked the MIR backend on
// test-std-hash.no (md5 reads its `data` []byte many times after moving it in).
//
// The analysis is path-sensitive. The original implementation kept a single
// module-wide "moved at inst X" map, which falsely flagged a value moved on one
// branch of a match/if and read on a *different*, mutually-exclusive branch
// (e.g. o-match: each arm reads the scrutinee, but only one arm may move it).
// We instead compute, for every program point, the set of values moved on ALL
// paths reaching that point (must-moved), via a forward dataflow fixpoint with
// INTERSECTION merge at join points. Only DROPS of must-moved values are
// reported, which is SOUND: it never produces a false positive. It may
// under-report a genuine partial-move across a merge, but under-reporting is
// safe for a strangler-fig backend (it does not disable a valid program), and
// within-block and same-path moves are still caught exactly as before.
func (m *Module) checkMoves(f *Function, rep *Report) {
	// Universe of values in this function (the TOP lattice element: every value
	// is tentatively "must-moved").
	allVals := valueSet{}
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
				allVals[inst.Dst] = true
			}
			for _, a := range inst.Args {
				if a > NoVal {
					allVals[a] = true
				}
			}
		}
	}
	for _, p := range f.Params {
		allVals[p] = true
	}

	movedIn := map[BlockID]valueSet{}
	movedOut := map[BlockID]valueSet{}
	for _, bid := range f.Blocks {
		movedOut[bid] = cloneSet(allVals) // TOP
	}
	entry := f.Entry
	if entry == NoBlock && len(f.Blocks) > 0 {
		entry = f.Blocks[0]
	}

	// Forward fixpoint: movedOut[b] = movedIn[b] ∪ {moved within b};
	// movedIn[b] = ∩ movedOut[preds] (empty for entry / unreachable blocks).
	changed := true
	for changed {
		changed = false
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			var newIn valueSet
			if bid == entry || len(blk.Preds) == 0 {
				newIn = valueSet{} // entry and unreachable blocks have no moved-in
			} else {
				newIn = cloneSet(movedOut[blk.Preds[0]])
				for _, p := range blk.Preds[1:] {
					newIn = intersectSet(newIn, movedOut[p])
				}
			}
			cur := cloneSet(newIn)
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				if isTransferringMove(inst) && !m.isOptionPeelMove(f, inst) {
					cur[inst.Args[0]] = true
				}
			}
			if !equalSet(cur, movedOut[bid]) {
				movedOut[bid] = cur
				changed = true
			}
			movedIn[bid] = newIn
		}
	}

	// Flagging pass. nolang's OpMove is a BITWISE COPY (emitMove does
	// load+store), not a C++-style move that invalidates the source. So a
	// plain READ of a moved value is always safe — its bytes remain valid and
	// only its DROP responsibility transfers to the move destination. The real
	// memory hazard is a SECOND DROP of a moved value (the destination already
	// owns it, so dropping the source too frees the same heap pointer twice).
	// We therefore flag OpDrop of a moved value, and never flag reads of one.
	// This matches the legacy backend, where md5 passes its `data` []byte to
	// `load-le-u32` many times without moving it — flagging the reads there was
	// a false positive that blocked the MIR backend on test-std-hash.no.
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		cur := cloneSet(movedIn[bid])
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil {
				continue
			}
			// A moved value dropped again is a double-free. OpMove defines the
			// move (its source is not a drop) and is excluded here.
			if inst.Op == OpDrop {
				if len(inst.Args) > 0 && inst.Args[0] > NoVal && cur[inst.Args[0]] {
					rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
						Kind:  "use-after-move",
						Func:  f.Name,
						Block: bid,
						Inst:  iid,
						Msg:   fmt.Sprintf("value %d dropped after move at inst %d (double-free risk)", inst.Args[0], iid),
					})
				}
			}
			if isTransferringMove(inst) && !m.isOptionPeelMove(f, inst) {
				cur[inst.Args[0]] = true
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
	// Result parameters are OUT-PARAMETERS owned by the caller: the callee FILLS
	// them but never frees them (the caller drops after the call returns). They
	// are therefore never the callee's drop responsibility and must not be
	// flagged as leaks. Marking them in `declared` (as if the callee owned them)
	// produced a spurious `missing-drop` on EVERY function returning an owned
	// value — which is exactly the corpus-wide Stage-3 blocker. Input params are
	// handled by the existing `f.Params` loop below (Nolang passes owned params
	// by move, so the callee owns and drops them, or by reference; either way
	// the original accounting stands).
	resultParamSet := map[ValueID]bool{}
	for _, p := range f.ResultParams {
		resultParamSet[p] = true
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
			if inst.Op == OpMove {
				// Only the moved-from source (Args[0]) is exempt from dropping; the
				// destination keeps its own drop responsibility. EmitMoveInto carries
				// Args=[src, dst], so we must not mark dst as a move source.
				if len(inst.Args) > 0 && inst.Args[0] > NoVal {
					moveSrc[inst.Args[0]] = true
				}
			}
			// OpOptionWrap transfers ownership of its payload into the option (the
			// option's single drop is the free site), so the payload, like a move
			// source, must be exempt from dropping. Without this an owned payload
			// (str/vec/heap-option) is reported as a leak even though it is freed
			// exactly once by the option's drop.
			if inst.Op == OpOptionWrap && len(inst.Args) > 0 && inst.Args[0] > NoVal {
				moveSrc[inst.Args[0]] = true
			}
			// OpStrFromVec takes over the slice's buffer (see insertDrops above).
			if inst.Op == OpStrFromVec && len(inst.Args) > 0 && inst.Args[0] > NoVal {
				moveSrc[inst.Args[0]] = true
			}
			// A struct-literal field store consumes its value: ownership moves into
			// the field, so the value is freed by the struct (not by its own drop).
			// Exempt it here to match insertDrops, which suppresses its drop when it
			// is dead after the store — otherwise the leak check spuriously reports
			// `missing-drop` for every struct-literal field initializer.
			if inst.Op == OpSetField && inst.MovesArg && len(inst.Args) >= 2 && inst.Args[1] > NoVal {
				moveSrc[inst.Args[1]] = true
			}
			// A tagged-enum constructor CONSUMES its payload fields: the variant
			// value holds them inline in its payload union, so ownership transfers
			// into the enum and the fields must not be reported as leaked.
			if inst.Op == OpEnumNew {
				for _, a := range inst.Args {
					if a > NoVal {
						moveSrc[a] = true
					}
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
				if resultParamSet[inst.Dst] {
					// Caller-owned out-param: not dropped by the callee.
					continue
				}
				if m.dropOwnsHeap(f, inst.Dst) && !m.isSliceViewOfArray(f, inst) && !m.isBorrowRead(f, inst) {
					declared[inst.Dst] = true
				}
			}
		}
	}
	// Input parameters are BORROWED, not owned by the callee. Nolang passes owned
	// arguments by reference: the caller retains ownership and drops them at the
	// caller's own scope exit (the legacy emitHeapFree frees only LOCAL heap
	// variables, never input params). The callee must NOT drop a borrowed param,
	// so input params are intentionally NOT added to `declared` — doing so
	// produced a spurious `missing-drop` on EVERY function that takes an owned
	// parameter (str/vec/fe/...), which was the single largest Stage-3 blocker.
	// Result parameters (out-params) are already excluded above via resultParamSet
	// because they too are caller-owned.
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

// blockReaches reports whether there is a control-flow path from `from` to
// `target` (following block successors). Used to detect loop headers: a block
// is a loop header iff it can reach one of its own predecessors.
func (m *Module) blockReaches(from, target BlockID) bool {
	seen := map[BlockID]bool{}
	stack := []BlockID{from}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if b == target {
			return true
		}
		if seen[b] {
			continue
		}
		seen[b] = true
		blk := m.Block(b)
		if blk == nil {
			continue
		}
		for _, s := range blk.Succs {
			if !seen[s] {
				stack = append(stack, s)
			}
		}
	}
	return false
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

// DumpAnnotated prints a compact, human-readable view of the module for
// debugging the drop-insertion / move analysis. Gated by NOLANG_MIR_DUMP_MIR.
func (m *Module) DumpAnnotated() string {
	var b strings.Builder
	// Struct layouts with their definition-site field tags. Sorted, so map
	// iteration order never leaks into the dump.
	if len(m.StructFields) > 0 {
		names := make([]string, 0, len(m.StructFields))
		for name := range m.StructFields {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "### structs (%d)\n", len(names))
		for _, name := range names {
			fmt.Fprintf(&b, "  %s\n", name)
			for _, f := range m.StructFields[name] {
				fmt.Fprintf(&b, "    %s %s [%s]\n", f.Name, f.TypeRaw, f.Tag)
			}
		}
	}
	if len(m.Globals) > 0 {
		fmt.Fprintf(&b, "### globals (%d)\n", len(m.Globals))
		for _, g := range m.Globals {
			ct := g.ConstText
			if len(ct) > 48 {
				ct = ct[:48] + "..."
			}
			fmt.Fprintf(&b, "  @%s = value %d type %d const=%q\n", g.Name, g.Init, g.Type, ct)
		}
	}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.Name == "" {
			continue
		}
		fmt.Fprintf(&b, "### func %s (extern=%v params=%v results=%v resultParams=%v)\n", f.Name, f.IsExtern, f.Params, f.Results, f.ResultParams)
		for _, p := range f.Params {
			pt := TypeID(0)
			if t, ok := f.LocalTypes[p]; ok {
				pt = t
			} else if v := m.Value(p); v != nil {
				pt = v.Type
			}
			own := false
			if ty := m.Type(pt); ty != nil {
				own = ty.Owned
			}
			raw := ""
			if ty := m.Type(pt); ty != nil {
				raw = ty.Raw
			}
			fmt.Fprintf(&b, "    PARAM %d type=%s owned=%v\n", p, raw, own)
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			fmt.Fprintf(&b, "  block %d (preds=%v succs=%v)\n", bid, blk.Preds, blk.Succs)
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				dt := ""
				if inst.Dst > NoVal {
					if t := m.Type(inst.Type); t != nil {
						dt = fmt.Sprintf(":%s(owned=%v)", t.Raw, t.Owned)
					}
				}
				fmt.Fprintf(&b, "    %-10s dst=%d%s args=%v\n", inst.Op, inst.Dst, dt, inst.Args)
			}
			if blk.Term != nil {
				fmt.Fprintf(&b, "    TERM %s args=%v targets=%v\n", blk.Term.Op, blk.Term.Args, blk.Term.Targets)
			}
		}
	}
	return b.String()
}
