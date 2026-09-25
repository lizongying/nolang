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
				// BOTH encodings of OpMove — see moveSrc/moveDst in mir.go.
				if dst := moveDst(inst); dst > NoVal && len(inst.Args) >= 1 {
					srcOf[dst] = append(srcOf[dst], inst.Args[0])
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
				// BOTH encodings of OpMove — see moveSrc/moveDst in mir.go.
				add(moveDst(inst), moveSrc(inst))
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
					// follows frees the old buffer. Both encodings: moveDst.
					addClobber(moveDst(inst), at)
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
	// Rebuild the owning-enum marks from scratch: Analyze may run more than once
	// on the same Module (tests re-analyze after mutating the IR), and a stale
	// ValueID left owning would keep a drop the new IR no longer warrants.
	m.enumOwnsPayload = map[ValueID]bool{}
	// Phase 2 needs a whole-module pre-pass: a caller must know whether a callee
	// consumes the enum it passes, and a callee may be analyzed after its caller.
	m.markEnumParamOwners()
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
	// OWNING TAGGED ENUM (see enumOwnsPayload): the value is matched two or more
	// times, so no single extraction could take the payload out. The enum keeps
	// it and frees it through a tag-switched destructor. This is a VALUE-level
	// answer and must be asked BEFORE the type-level one: the enum's own
	// Type.Owned is false by design (widening it would change the ABI for every
	// enum and make single-use enums double-free).
	if m.enumOwnsPayload[v] {
		return true
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

// markEnumPayloadOwners decides, for one function, which tagged-enum VALUES own
// their payload. It is the Phase-1 core of
// docs/design/tagged-enum-payload-ownership.md, and it implements the language
// principle "one use is a move, several uses are a clone".
//
// Rationale. A tagged enum is a union whose payload extraction
// (OpEnumField/emitEnumField) is a plain by-value load — it MOVES the payload
// out, and the extraction result becomes the owner the drop pass frees. That is
// exactly right when the value is matched once. Matched twice, the first
// extraction's drop frees the buffer the second extraction still reads: the
// second match printed an empty string (and, before the arm-binding fix,
// double-freed). The fix has to be at the VALUE level, not the TYPE level —
// making every payload-carrying enum owning would double-free the single-use
// ones, whose payload was already moved out.
//
// The criterion is "matched twice", NOT "extracted twice", and the difference
// is load-bearing. ONE match of a multi-field variant extracts EVERY field:
//
//	rect(w, h) -> ...      ; emits OpEnumField slot 0 AND slot 1
//
// so counting extractions would call `rect` matched-once "owning", suppress the
// move-source exemption, and trip checkMoves' use-after-move on the value
// (observed on tests/tagged-enum.no's `shape`). What actually identifies a
// repeated MATCH is the ARM BODY, and each arm body is lowered into its own
// basic block, so:
//
//	owning(v) <=> two or more blocks extracted v with the SAME slot set
//
// One block extracting several slots is a single match of one variant; two
// blocks extracting the same slot set is the same variant matched twice. Two
// blocks extracting DIFFERENT slot sets are different arms of one match (the
// variant was not statically known) and are deliberately left on the move path.
//
// Two exclusions, both to keep the "exactly one drop per payload" invariant:
//
//   - MOVE DESTINATIONS. `r e-res = q` lowers to a bitwise OpMove of the
//     { tag, payload } struct, i.e. an ALIAS, not a transfer (an enum's payload
//     lives inline in the enum, so a bitwise copy copies the data pointer).
//     If both `q` and `r` were marked owning, both would be dropped and the
//     shared payload freed twice. Only the origin of the alias chain owns;
//     insertDrops' moveSrc rule is adjusted in the same way (see there).
//   - ENUMS WHOSE PAYLOAD THE HELPER CANNOT FREE. enumPayloadFreeable gates on
//     the field shapes emitEnumDropHelper knows: `str`, slices/`vec`, and
//     inline structs with owned leaves or pointees. An enum with, say, an
//     `?T` payload is left on the pre-existing move-once path rather than
//     half-freed — leaking is the safe side to err on.
//
// Values extracted ZERO times are deliberately left alone: that is the
// pre-existing leak tracked as Phase 3, and fixing it here would widen the
// blast radius of this change for no benefit to the reported bug.
func (m *Module) markEnumPayloadOwners(f *Function) {
	if f == nil {
		return
	}
	// byBlock[v][block] = the set of payload slots that block extracted from v.
	byBlock := map[ValueID]map[BlockID]map[int64]bool{}
	moveDest := map[ValueID]bool{}
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
			case OpEnumField:
				if len(inst.Args) == 0 || inst.Args[0] <= NoVal {
					continue
				}
				v := inst.Args[0]
				if byBlock[v] == nil {
					byBlock[v] = map[BlockID]map[int64]bool{}
				}
				if byBlock[v][bid] == nil {
					byBlock[v][bid] = map[int64]bool{}
				}
				byBlock[v][bid][inst.Int] = true
			case OpMove:
				// Record enum-typed move DESTINATIONS so the alias's own slot
				// is never given a second drop (see the doc comment above).
				//
				// ⚠️ BOTH encodings (moveDst). `t e-res = q` defines a fresh t
				// (Dst set), but the REASSIGNMENT `t = q` stores into t's
				// existing slot through EmitMoveInto (Dst == NoVal, target in
				// Args[1]). Reading only inst.Dst left the assignment target out
				// of moveDest, so R5 marked BOTH t and q owning and emitted two
				// drops for one buffer — a double free:
				//
				//	t e-res = fail
				//	q e-res = ok('hi')
				//	t = q            ; move dst=0 args=[q t]
				//	                 ; -> drop q AND drop t, one shared payload
				if moveSrc(inst) <= NoVal {
					continue
				}
				if dst := moveDst(inst); dst > NoVal {
					if ty := m.valueTypeOf(f, dst); ty != nil && ty.Kind == KindEnum {
						moveDest[dst] = true
					}
				}
			}
		}
	}
	// R3 (same function): two arm bodies extracted the same slot set — the same
	// variant matched twice, so no single extraction can take the payload.
	candidates := map[ValueID]bool{}
	for v, blocks := range byBlock {
		if sameSlotSetTwice(blocks) {
			candidates[v] = true
		}
	}

	// R2 (cross-function): v is handed to a callee that CONSUMES its enum
	// parameter. R1 already made that parameter owning, so the callee only
	// borrows or clones; the payload must therefore be freed HERE, by the side
	// that owns the storage. Values that also escape into a sink are excluded —
	// see enumEscapeSinks for why a leak beats a dangling alias there.
	escaped := m.enumEscapeSinks(f)
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil || inst.Op != OpCall || inst.Callee != NoVal {
				continue // not a direct call to a named function
			}
			if !m.calleeConsumesEnumArg(inst.Sym) {
				continue
			}
			for _, a := range inst.Args {
				if a <= NoVal || escaped[a] {
					continue
				}
				if ty := m.valueTypeOf(f, a); ty != nil && ty.Kind == KindEnum {
					candidates[a] = true
				}
			}
		}
	}

	// R5 (Phase 3, same function): the payload was NEVER extracted here, so
	// nothing took it out and this value is still the only possible drop site.
	// Before this rule `q e-res = ok('hi')` with no match at all — or handed
	// only to a callee that does not extract — leaked the payload silently:
	// no extraction meant no owner, and an unowned enum is never dropped.
	//
	// Excluded, each for a concrete reason:
	//   - parameters: a borrowed input is not owned here, and marking one would
	//     also flip R2's trigger (calleeConsumesEnumArg) for a callee that in
	//     fact consumes nothing. The CALLER's own value picks R5 up instead.
	//   - move destinations: an alias, not an owner (the moveDest gate below);
	//     the alias's SOURCE is the value R5 marks, which is also what makes
	//     Phase 2b's dead-alias transfer case drop.
	//   - escaped values: a sink (container element, option/struct field,
	//     opaque call) keeps a shallow alias that the drop machinery never
	//     traverses, so freeing here would leave it dangling. Same gate as R2.
	isParam := map[ValueID]bool{}
	for _, p := range f.Params {
		isParam[p] = true
	}
	for _, p := range f.ResultParams {
		isParam[p] = true
	}
	sites := m.enumExtractSites[f.ID]
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
			v := inst.Dst
			if sites[v] || isParam[v] || escaped[v] {
				continue
			}
			candidates[v] = true
		}
	}

	for v := range candidates {
		if moveDest[v] {
			continue
		}
		ty := m.valueTypeOf(f, v)
		if ty == nil || ty.Kind != KindEnum || !m.enumPayloadFreeable(ty) {
			continue
		}
		if m.enumOwnsPayload == nil {
			m.enumOwnsPayload = map[ValueID]bool{}
		}
		m.enumOwnsPayload[v] = true
	}
}

// markEnumParamOwners is the Phase-2 (cross-function) pre-pass. A tagged enum
// crosses a call boundary as a POINTER TO THE CALLER'S SLOT
//
//	call void @show(ptr %v1.s)                        ; caller
//	define void @show(ptr readonly captures(none) %p0) ; callee reads caller's storage
//
// so the callee's parameter IS the caller's storage: the callee must never free
// that payload, and the caller must. This function does the callee half (R1) and
// records the per-function extraction sites the caller half (R2, in
// markEnumPayloadOwners) consults.
//
// It must run for the WHOLE module before any per-function drop analysis, since
// a callee can be analyzed after its caller. See
// docs/design/tagged-enum-payload-ownership.md §9.
func (m *Module) markEnumParamOwners() {
	m.enumExtractSites = map[FuncID]map[ValueID]bool{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		sites := map[ValueID]bool{}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil || inst.Op != OpEnumField {
					continue
				}
				if len(inst.Args) == 0 || inst.Args[0] <= NoVal {
					continue
				}
				sites[inst.Args[0]] = true
			}
		}
		if len(sites) > 0 {
			m.enumExtractSites[f.ID] = sites
		}
	}
	// R1: a borrowed enum parameter the callee extracts must be owning IN THE
	// CALLEE, so its extraction clones (owned `str`) or borrows (%vec / inline
	// struct) instead of moving the payload out and dropping it. Parameters are
	// already excluded from `droppable`, so this never makes the callee free
	// anything — it only stops the callee from freeing the CALLER's buffer.
	//
	// Gated on enumPayloadFreeable for the same reason as the other rules: if
	// the helper cannot free the payload, the callee must keep moving it out
	// (today's behaviour) rather than borrow it and leave it unfreed forever.
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		sites := m.enumExtractSites[f.ID]
		if len(sites) == 0 {
			continue
		}
		for _, p := range f.Params {
			if !sites[p] {
				continue
			}
			ty := m.valueTypeOf(f, p)
			if ty == nil || ty.Kind != KindEnum || !m.enumPayloadFreeable(ty) {
				continue
			}
			m.enumOwnsPayload[p] = true
		}
	}
}

// calleeConsumesEnumArg reports whether the named callee takes ownership-relevant
// action on an enum parameter — precisely, whether markEnumParamOwners marked one
// of its parameters owning (R1). Only then must the caller take the payload's
// ownership over, which is what keeps R2's blast radius to the calls that
// actually move the payload rather than to every enum argument in the corpus.
//
// Variadic callees are skipped: their actual arguments do not map one-to-one
// onto parameters, so "this callee consumes an enum" cannot be attributed to a
// specific argument.
func (m *Module) calleeConsumesEnumArg(sym string) bool {
	if sym == "" {
		return false
	}
	cid, ok := m.FuncByName[sym]
	if !ok {
		return false
	}
	callee := m.Func(cid)
	if callee == nil || callee.IsExtern || callee.Variadic {
		return false
	}
	for _, p := range callee.Params {
		if m.enumOwnsPayload[p] {
			return true
		}
	}
	return false
}

// enumEscapeSinks returns the enum values in f whose payload may be aliased by
// storage that outlives the current statement: a container element, an option
// payload, another tagged enum's payload, a struct field, a result parameter, a
// call that may store its argument (see the OpCall case), or — transitively — a
// value that itself escapes.
//
// R2 declines to make such a value owning. Freeing it at its own last use would
// leave the sink pointing at freed memory, and the sink is NOT something the
// drop machinery frees (a tagged enum is not an owned type and its payload slots
// are not traversed), so the alias would dangle. A leak is the safe side to err
// on: see docs/design/tagged-enum-payload-ownership.md §9.3/§9.5.
//
// Every argument of a sink op counts, not just the stored one. Over-approximating
// escape is safe — it can only make R2 more conservative — whereas guessing the
// argument positions wrong could miss a real escape.
func (m *Module) enumEscapeSinks(f *Function) map[ValueID]bool {
	escaped := map[ValueID]bool{}
	for _, p := range f.ResultParams {
		escaped[p] = true // handed back to the caller: outlives this frame
	}
	for {
		changed := false
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
				case OpSetField, OpIndexStore, OpOptionWrap, OpEnumNew:
					for _, a := range inst.Args {
						if a > NoVal && !escaped[a] {
							escaped[a] = true
							changed = true
						}
					}
				case OpMove:
					// A move is a bitwise alias, so v's payload is reachable
					// through w: if w escapes, so does v.
					//
					// ⚠️ Read BOTH encodings of an OpMove (moveSrc/moveDst in
					// mir.go):
					//   `dst=w args=[v]`   the scrutinee binding
					//   `dst=0 args=[v w]` an assignment statement (EmitMoveInto)
					// Only reading the first left the second invisible, so
					// `r = q` with `r` a RESULT PARAMETER never propagated the
					// escape back to `q`. R5 then made `q` owning and dropped
					// the very payload the caller had been handed: `got: made`
					// became `got:    ` (silent use-after-free) — see
					// tests/tagged-enum-zero-match.no case 11/12.
					src := moveSrc(inst)
					if src > NoVal {
						if dst := moveDst(inst); dst > NoVal && escaped[dst] && !escaped[src] {
							escaped[src] = true
							changed = true
						}
					}
				case OpCall, OpCallExtern, OpCallFFI:
					// A call to a Nolang function is R2's own trigger and is
					// analysed by its callee's extraction sites — do not treat
					// its arguments as escaping, or R2 could never fire.
					if inst.Op == OpCall && inst.Callee == NoVal {
						if _, _, ok := lookupBuiltin(inst.Sym); !ok {
							continue
						}
					}
					// Everything else is opaque storage. `l.push(q)` is the
					// motivating case: it is an OpCall to the `vec.push`
					// builtin, NOT an OpIndexStore, and it copies q into a
					// container whose elements the drop machinery never
					// traverses (a tagged enum is not an owned type, so its
					// payload slots are not visited) — so the element aliases
					// q's buffer without owning it. Extern/FFI calls and
					// indirect calls through a function pointer are equally
					// opaque. Mark every argument escaped: over-approximating
					// can only make R2 decline to free, which leaks instead of
					// dangling — the same safe side as the sinks above.
					for _, a := range inst.Args {
						if a > NoVal && !escaped[a] {
							escaped[a] = true
							changed = true
						}
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	return escaped
}

// sameSlotSetTwice reports whether two of the given blocks extracted the same
// set of payload slots — i.e. the same variant was matched by two arm bodies.
func sameSlotSetTwice(blocks map[BlockID]map[int64]bool) bool {
	seen := map[string]bool{}
	for _, slots := range blocks {
		keys := make([]int64, 0, len(slots))
		for s := range slots {
			keys = append(keys, s)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		parts := make([]string, 0, len(keys))
		for _, s := range keys {
			parts = append(parts, fmt.Sprintf("%d", s))
		}
		key := strings.Join(parts, ",")
		if seen[key] {
			return true
		}
		seen[key] = true
	}
	return false
}

// enumPayloadFreeable reports whether every owned payload field of the enum
// type is one emitEnumDropHelper can free, so marking the value owning cannot
// produce a half-freed payload. Conservative by construction: an unrecognised
// owned field shape returns false, leaving that enum on the move-once path.
func (m *Module) enumPayloadFreeable(ty *Type) bool {
	if ty == nil || ty.Kind != KindEnum {
		return false
	}
	ei := m.TaggedEnums[ty.Raw]
	if ei == nil {
		return false
	}
	for vi := range ei.Variants {
		for _, raw := range ei.Variants[vi].Fields {
			ft := m.Type(m.internType(raw))
			if ft == nil {
				return false
			}
			if !m.typeOwnsHeap(ft) {
				continue // owns nothing: nothing to free, nothing to get wrong
			}
			if !m.enumFieldFreeable(ft) {
				return false
			}
		}
	}
	return true
}

// enumFieldFreeable is the field-shape whitelist behind enumPayloadFreeable; it
// must stay in lockstep with the free cases in emitEnumDropHelper.
func (m *Module) enumFieldFreeable(ft *Type) bool {
	switch ft.Kind {
	case KindStr, KindSlice:
		return true
	case KindStruct:
		key := m.StructKeyOf(ft.Raw)
		return key != "" && (m.StructHasPtrFields(key) || m.StructHasOwnedLeafFields(key))
	}
	return false
}

// enumPayloadCloneable reports whether every owned payload field of the enum
// type has a deep-copy helper available, so a bitwise alias of the enum can be
// BROKEN with a real copy rather than merely freed.
//
// Deliberately narrower than enumPayloadFreeable: freeing a field needs only its
// address, whereas cloning it needs a cloner, and today only `str` (@str_clone)
// and slice (vecDeepClone) have one. A struct-with-pointees field therefore
// returns false, which keeps that enum on the pre-existing path — a leak, or the
// analyzer's loud `[use-after-move]` — rather than emitting a half-deep copy
// that would double-free. See docs/design/tagged-enum-payload-ownership.md §10.
func (m *Module) enumPayloadCloneable(ty *Type) bool {
	if ty == nil || ty.Kind != KindEnum {
		return false
	}
	ei := m.TaggedEnums[ty.Raw]
	if ei == nil {
		return false
	}
	for vi := range ei.Variants {
		for _, raw := range ei.Variants[vi].Fields {
			ft := m.Type(m.internType(raw))
			if ft == nil {
				return false
			}
			if !m.typeOwnsHeap(ft) {
				continue // owns nothing: nothing to copy, nothing to get wrong
			}
			switch ft.Kind {
			case KindStr:
				// @str_clone
			case KindSlice:
				// vecDeepClone; the element type must be resolvable for the
				// helper to be specialised on it.
				if ft.Elem == NoType {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

// moveEnumSharesHeap reports whether inst is an OpMove of a tagged enum whose
// payload owns heap. Such a move is a bitwise ALIAS of that heap, not a transfer
// of it: the payload lives INLINE in the enum, so the copy leaves both slots
// pointing at one buffer.
func (m *Module) moveEnumSharesHeap(f *Function, inst *Inst) bool {
	if inst == nil || inst.Op != OpMove || len(inst.Args) == 0 || inst.Args[0] <= NoVal {
		return false
	}
	ty := m.valueTypeOf(f, inst.Args[0])
	return ty != nil && ty.Kind == KindEnum && m.enumPayloadCloneable(ty)
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

// readNonDropAfterInBlock is readAfterInBlock minus OpDrop: it answers "is v's
// DATA still used after this point", which is what decides clone-vs-transfer for
// a bitwise-copied value. readAfterInBlock itself is left alone because the
// struct-pointer path wants the conservative answer (any later mention).
func (m *Module) readNonDropAfterInBlock(bid BlockID, after InstID, v ValueID) bool {
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
		if inst == nil || inst.Op == OpDrop {
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
	dst := moveDst(inst)
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
	// A WRAP into an option (`o ?T = y`) shares heap for exactly the same
	// reason a struct copy does: the payload is stored by a BITWISE copy
	// (memcpy'd into the box, or straight into the inline slot), so the
	// option's leaves and pointees alias y's. It therefore goes through the
	// same liveness rule as any other heap-sharing copy — live source ->
	// OpClone (emitClone deep-copies the payload into the option, so each side
	// owns its memory and each is dropped once), dead source -> bitwise move (a
	// real transfer: insertDrops' OpOptionWrap rule exempts the source and the
	// option's drop is the single free site).
	//
	// This used to be excluded here, because emitClone's struct-clone paths
	// resolve the SOURCE's struct key and memcpy the whole struct over the
	// destination — which for a 32-byte %option writes the struct across the
	// tag and slot, so `o = c` read back EMPTY (verified on HEAD). emitClone
	// now has its own option branch and never routes a wrap through those
	// paths, so the liveness rule is safe to apply.
	if m.optionCopySharesHeap(f, inst) {
		return true
	}
	if m.moveStructHasPtrFields(f, inst) {
		return true
	}
	if inst.Op != OpMove || len(inst.Args) == 0 {
		return false
	}
	dst := moveDst(inst)
	return m.typeIsLeafStruct(f, dst) || m.typeIsLeafStruct(f, inst.Args[0])
}

// optionCopySharesHeap reports whether inst STORES a heap-sharing payload into
// an option — either shape:
//
//   - a WRAP (`o ?T = y`, OpOptionWrap): the payload is stored by a BITWISE
//     copy (memcpy'd into the box, or straight into the inline slot), so the
//     option's leaves and pointees alias y's.
//   - an option-to-option copy of a BOXED payload: emitClone re-boxes it
//     (optBoxClone — a fresh malloc plus a deep copy), so the copy does not
//     consume the source.
//
// Both must go through the live-source liveness rule instead of being assumed
// to consume the source; see the call site in moveStructSharesHeap.
func (m *Module) optionCopySharesHeap(f *Function, inst *Inst) bool {
	switch inst.Op {
	case OpOptionWrap:
		// `?str` / `?[]T` payloads are deliberately NOT included: the option's
		// drop frees the payload itself and the wrap is the transfer that hands
		// it over, so a bitwise move is exactly right there.
		if len(inst.Args) == 0 || inst.Args[0] <= NoVal {
			return false
		}
		src := m.valueTypeOf(f, inst.Args[0])
		if src == nil || src.Kind != KindStruct {
			return false
		}
		key := m.StructKeyOf(src.Raw)
		return key != "" && (m.StructHasOwnedLeafFields(key) || m.StructHasPtrFields(key))
	case OpMove:
		if len(inst.Args) == 0 || inst.Args[0] <= NoVal {
			return false
		}
		st := m.valueTypeOf(f, inst.Args[0])
		if st == nil || st.Kind != KindOption {
			return false
		}
		dst := moveDst(inst)
		if dst <= NoVal {
			return false
		}
		dt := m.valueTypeOf(f, dst)
		if dt == nil || dt.Kind != KindOption {
			return false
		}
		elem, ok := parseOptionElem(st.Raw)
		if !ok {
			return false
		}
		return m.OptionPayloadBoxed(elem)
	}
	return false
}

// moveStrSharesHeap reports whether an OpMove copies an owned `str` BITWISE,
// which leaves the source and the destination pointing at the SAME heap buffer.
//
// %str-long = { i64 len, i64 cap, i8* data } and `data` is malloc'd, so a
// load+store (what emitMove emits) duplicates the TRIPLE, not the bytes. That is
// only safe when the source dies right there and hands its drop responsibility to
// the destination; if the source is read again, both values' drops free the same
// pointer.
//
// The classic repro is a loop re-assigning one string from another:
//
//	src str = 'hello'
//	back str = 'seed'
//	j <- [0..3): { back = src }
//
// Each iteration drops `back` (freeing the buffer that is also `src`'s) and then
// bitwise-copies `src` into it again — iteration 2 frees src's buffer and
// iteration 3 frees it a second time -> `trace/BPT trap`; with no reads in
// between the symptom is just `back.len() == 0`. i64 and %txt are unaffected:
// neither owns heap (i64 is scalar, %txt keeps its bytes inline).
func (m *Module) moveStrSharesHeap(f *Function, inst *Inst) bool {
	if inst.Op != OpMove || len(inst.Args) == 0 {
		return false
	}
	src := inst.Args[0]
	if src <= NoVal {
		return false
	}
	dst := moveDst(inst)
	if dst <= NoVal || dst == src {
		return false
	}
	return m.typeIsOwnedStr(f, src) && m.typeIsOwnedStr(f, dst)
}

// typeIsOwnedSlice reports whether v is an owned slice — i.e. a %vec whose
// backing buffer is a heap allocation this value is responsible for freeing.
func (m *Module) typeIsOwnedSlice(f *Function, v ValueID) bool {
	t := m.valueTypeOf(f, v)
	return t != nil && t.Kind == KindSlice && t.Owned
}

// moveSliceSharesHeap reports whether inst is a slice move that shares the heap
// backing store. A %vec is {len, cap, data}; a bitwise OpMove copies the triple
// and aliases the SAME data pointer, so a later drop of the destination frees
// the source's buffer too. That is only safe as a transfer when the source is
// provably dead. When the source is still live (read or reassigned after the
// move) the move must be rewritten to OpClone — vecDeepClone gives the
// destination an independent buffer — exactly the move-vs-clone rule already
// applied to str and struct moves.
//
// Concretely, `a = x; b = x` with x used twice makes `a = x` a clone (a owns
// its own copy) and `b = x` a transfer; without this, both a and b aliased x's
// buffer and the caller double-freed it (tests/mem-safety/
// move-eligibility-improved.no: clone-then-move lost rb[0]).
func (m *Module) moveSliceSharesHeap(f *Function, inst *Inst) bool {
	if inst.Op != OpMove || len(inst.Args) == 0 {
		return false
	}
	src := inst.Args[0]
	if src <= NoVal {
		return false
	}
	dst := moveDst(inst)
	if dst <= NoVal || dst == src {
		return false
	}
	return m.typeIsOwnedSlice(f, src) && m.typeIsOwnedSlice(f, dst)
}

// typeIsOwnedStr reports whether v is an owned `str` — i.e. a %str-long whose
// `data` pointer is a heap allocation this value is responsible for freeing.
func (m *Module) typeIsOwnedStr(f *Function, v ValueID) bool {
	t := m.valueTypeOf(f, v)
	return t != nil && t.Kind == KindStr && t.Owned
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
	// OpEnumField: tagged-enum payload extraction. Its answer must MIRROR
	// emitEnumField's clone condition exactly (same reasoning as OpGetField
	// above), and the two shapes are chosen by whether the enum owns the
	// payload:
	//
	//   - NOT owning (the common single-use enum): the extraction MOVES the
	//     payload out and its result is the one and only owner. Return false so
	//     insertDrops frees it — this is the pre-existing path, untouched.
	//   - owning (two or more extractions, see markEnumPayloadOwners): the enum
	//     frees the payload itself, so an extraction must NOT free it too.
	//     - owned `str` -> emitEnumField calls @str_clone, so the result owns a
	//       PRIVATE buffer and must be dropped (false).
	//     - every other owned field (`%vec`, inline struct) -> not cloned (there
	//       is no @vec_clone), so the result aliases the enum's storage and is a
	//       genuine borrow (dropOwnsHeap).
	if inst.Op == OpEnumField {
		if !m.enumOwnsPayload[inst.Args[0]] {
			return false
		}
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
	//
	// ⚠️ Widened to moveSrc/moveDst for uniformity, but — unlike the two
	// consumers named in the note above — this one is a NO-OP today, and that is
	// provable rather than measured: both call sites (insertDrops, checkDropCount)
	// reach isBorrowRead only under `inst.Dst > NoVal`, and moveDst returns
	// inst.Dst whenever it is set. So the EmitMoveInto spelling of a peel can
	// never arrive here. Kept in the widened form so the branch stays correct if
	// a future call site ever drops that guard — do not "simplify" it back to
	// inst.Dst on the strength of it being dead, and do not assume the
	// assignment-shaped peel is covered here; it is not.
	//
	// (The premise above is also stale for `?[]T`: emitMove now CLONES a %vec
	// payload via vecDeepClone when the element type resolves, so for that case
	// the result owns a private buffer and suppressing its drop leaks the clone.
	// Measured on the fresh-binding spelling: `s []i64 = o` never drops the
	// peeled value, while the assignment spelling `s = o` does. Pre-existing and
	// orthogonal to the two-encoding trap — tracked separately, not fixed here.)
	if inst.Op == OpMove {
		src, dst := moveSrc(inst), moveDst(inst)
		if src > NoVal && dst > NoVal {
			if st := m.valueTypeOf(f, src); st != nil && st.Kind == KindOption {
				if dt := m.valueTypeOf(f, dst); dt != nil && dt.Kind == KindSlice {
					return m.dropOwnsHeap(f, dst)
				}
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
	// Decide which tagged-enum values own their payload BEFORE anything asks
	// dropOwnsHeap about them (below, and via isBorrowRead). See
	// markEnumPayloadOwners.
	m.markEnumPayloadOwners(f)

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
			// Same question, one level down: a plain `str` move. A %str-long
			// bitwise copy shares the heap buffer, so it is only safe when the
			// source is provably dead here. When the source is still live (the
			// loop case in moveStrSharesHeap's comment) rewrite to OpClone:
			// emitClone deep-copies through @str_clone, and because
			// isTransferringMove only recognises OpMove, BOTH sides keep their
			// drop and each frees its own buffer.
			if m.moveStrSharesHeap(f, inst) {
				// A later OpDrop of the source does NOT count as "still live":
				// that is the source releasing its own ownership, not a use of
				// the bytes. Counting it would clone on `w = v` followed by
				// `drop v`, which is the transfer case the use-after-move check
				// exists to catch (and TestAnalyzeUseAfterMoveDetected asserts).
				// Every other later use — including the source being read again
				// as the next iteration's move source — is a genuine read and
				// forces a clone.
				if liveOut[bid][inst.Args[0]] || m.readNonDropAfterInBlock(bid, iid, inst.Args[0]) {
					inst.Op = OpClone
				} else {
					moveSrc[inst.Args[0]] = true
				}
				continue
			}
			// Same liveness rule for an owned-slice move: a bitwise copy of a
			// %vec aliases the backing buffer, so the destination and source
			// would share one malloc. Promote to OpClone (vecDeepClone) when the
			// source is still live; otherwise treat it as a transfer that
			// exempts the source from dropping.
			if m.moveSliceSharesHeap(f, inst) {
				if liveOut[bid][inst.Args[0]] || m.readNonDropAfterInBlock(bid, iid, inst.Args[0]) {
					inst.Op = OpClone
				} else {
					moveSrc[inst.Args[0]] = true
				}
				continue
			}
			// A tagged enum whose payload owns heap is the same hazard as the
			// three cases above, one level down: `r e-res = q` lowers to OpMove,
			// which bit-copies {tag, payload}, so BOTH slots point at the SAME
			// payload buffer. The ownership model here is "the SOURCE owns" (see
			// the note below), which is sound only while the alias stays dead: if
			// the alias is READ later, freeing at the source's last use would
			// leave it dangling, while letting both own would double-free —
			// checkMoves reports exactly that, as
			// `[use-after-move] ... (double-free risk)`.
			//
			// Break the alias with a deep copy instead: each side then owns its
			// own buffer and both drop independently, whatever the block layout.
			// Only when the alias is actually read — a dead alias leaves the
			// single-buffer transfer, and the existing "source owns" rule, intact
			// (that is the `let it = q` scrutinee shape, which must not clone).
			if m.moveEnumSharesHeap(f, inst) {
				dst := moveDst(inst)
				if dst > NoVal && (liveOut[bid][dst] || m.readNonDropAfterInBlock(bid, iid, dst)) {
					inst.Op = OpClone
					m.enumOwnsPayload[inst.Args[0]] = true
					m.enumOwnsPayload[dst] = true
				}
			}
			// An OWNING tagged enum is exempt from this rule (see
			// moveTransfersOwnership): its move is a bitwise ALIAS (`let it = q`
			// for the match scrutinee, `r e-res = q` for a copy) and carries no
			// ownership — the payload lives inline in the enum, so the
			// destination shares it rather than receiving it. Marking the source
			// as a move source would suppress exactly the drop that frees the
			// payload, turning the fix into a leak; the destination is kept out
			// of enumOwnsPayload by markEnumPayloadOwners' moveDest rule (and
			// Phase 2b only ever lets it own when it got its own deep copy), so
			// there is still exactly one drop per payload.
			if m.moveTransfersOwnership(f, inst) {
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
			// leave the enum holding a freed buffer (tests/tagged-enum.no:
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
// on tests/str-test.no (std str.replace-n).
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
// isOptionPeelMove reports whether inst is the option PEEL shape `x = opt`,
// i.e. a move out of an option into a NON-option destination.
//
// A peel does not consume the option. emitMove's peel branch gives the
// destination its OWN copy of the payload (@str_clone for `?str`, vecDeepClone
// for `?[]T`, plus a leaf/pointee deep copy for a struct payload), and the
// option keeps its payload — it may be peeled again (str.replace-n unwraps the
// same `?str` twice) and it drops on its own. So the option must NOT be exempt
// from dropping, which is what returning true here achieves.
//
// The test used to be "the destination is a slice", which left every other peel
// — including `it.pad[0]` on a `?big` match arm — marking the option as a move
// source and suppressing its drop. With a boxed payload that meant the box was
// never freed: 200k iterations of `v ?big = b` leaked 104 MB.
func (m *Module) isOptionPeelMove(f *Function, inst *Inst) bool {
	if inst == nil || inst.Op != OpMove || len(inst.Args) == 0 || inst.Args[0] <= NoVal {
		return false
	}
	st := m.valueTypeOf(f, inst.Args[0])
	if st == nil || st.Kind != KindOption {
		return false
	}
	// Both OpMove encodings — see moveSrc/moveDst.
	dst := moveDst(inst)
	if dst <= NoVal {
		return false
	}
	dt := m.valueTypeOf(f, dst)
	return dt != nil && dt.Kind != KindOption
}

// ── OpMove has TWO encodings; every consumer must read both ──────────────────
//
//	b.Emit(OpMove, typ, [src])   // Dst = the fresh destination value
//	b.EmitMoveInto(dst, src)     // Dst = NoVal, Args = [src, dst]
//
// The second is what an ASSIGNMENT to an already-bound variable lowers to
// (`t = q`, `s = opt`, `x = x`, and any write to a result parameter), and it is
// the common shape in real code. An analysis that resolves the destination as
// `inst.Dst` alone therefore silently sees HALF the moves — with no error and no
// test failure, because the half it misses is simply never analysed.
//
// The encoding is shared by three ops, all of which must be covered:
//
//	OpMove        the move itself
//	OpClone       a move that insertDrops rewrote to a deep copy (same def/use
//	              shape, same operands — see the note in insertDrops)
//	OpOptionWrap  `o = c` on an already-bound option is an EmitMoveInto wrap
//	              (see emitOptionWrap)
//
// That is not hypothetical. Two ownership bugs in this file came from reading
// only inst.Dst, both of them silent:
//
//   - enumEscapeSinks did not propagate "the destination escapes" back to the
//     source for the EmitMoveInto shape, so `r = q` with `r` a RESULT PARAMETER
//     left `q` looking frame-local. The tagged-enum owner pass then freed the
//     very payload being handed back to the caller (`got: made` -> `got:    `).
//   - markEnumPayloadOwners never registered an assignment target as a move
//     DESTINATION, so for `t = q` it marked BOTH `t` and `q` as payload owners
//     and emitted two drops for one buffer — a double free.
//
// isBorrowRead's option-peel branch was widened too, but it is a NO-OP today
// (both its call sites already require inst.Dst > NoVal) — see the note there.
// That is worth knowing precisely: not every widened site was a live bug, and
// guessing which is which is how a "fix" gets credited for something it did not
// do.
//
// moveSrc/moveDst are the single place that knows the two shapes. Use them
// instead of reading inst.Dst / inst.Args[1] by hand, so the next walk cannot
// reintroduce the blind spot. Both return NoVal when the instruction is not
// move-like or the value is absent.
func moveSrc(inst *Inst) ValueID {
	if !moveLike(inst) || len(inst.Args) == 0 {
		return NoVal
	}
	return inst.Args[0]
}

// moveDst returns a move-like instruction's destination across BOTH encodings
// (see moveSrc): Dst when the instruction defines a fresh value, Args[1] when
// EmitMoveInto stores into an existing slot. NoVal when there is no destination.
func moveDst(inst *Inst) ValueID {
	if !moveLike(inst) {
		return NoVal
	}
	if inst.Dst > NoVal {
		return inst.Dst
	}
	if len(inst.Args) >= 2 {
		return inst.Args[1]
	}
	return NoVal
}

// moveLike reports whether inst uses OpMove's operand encoding — OpMove itself,
// the OpClone it may be rewritten to, or OpOptionWrap (which has the same two
// shapes). See the note above moveSrc.
func moveLike(inst *Inst) bool {
	if inst == nil {
		return false
	}
	switch inst.Op {
	case OpMove, OpClone, OpOptionWrap:
		return true
	}
	return false
}

func isTransferringMove(inst *Inst) bool {
	if inst == nil || inst.Op != OpMove || len(inst.Args) == 0 || inst.Args[0] <= NoVal {
		return false
	}
	return len(inst.Args) < 2 || inst.Args[1] != inst.Args[0]
}

// moveTransfersOwnership is isTransferringMove as the OWNERSHIP passes must read
// it: a move whose source hands its heap to the destination, so the source must
// NOT be dropped afterwards.
//
// An OWNING tagged enum is excluded. Its payload lives INLINE in the enum, so
// the move is a bitwise ALIAS rather than a hand-off: the SOURCE keeps the single
// drop (the destination is held out of enumOwnsPayload by markEnumPayloadOwners'
// moveDest rule, and Phase 2b only ever lets a destination own when it has been
// given its own deep copy). insertDrops and checkMoves must agree on this — with
// the exemption only in insertDrops, the drop it emitted for the source was then
// reported as `[use-after-move] value N dropped after move (double-free risk)`,
// i.e. the compiler refused to build its own correct output.
//
// ⚠️ The alias reading holds only while BOTH sides stay in this frame. A move
// whose destination is a PARAMETER hands the payload to the caller — the callee's
// parameter IS the caller's storage (see markEnumParamOwners), and a result
// parameter outlives this frame — so even an owning enum must be a transfer
// there. Getting this wrong drops a buffer the caller is about to read:
//
//	make = () (r e-res) { q e-res = ok('made'); r = q }
//
// lowered as `move dst=0 args=[q r]`, where q is owning (R5 marks it, or Phase 2b
// clones into a value that is then moved out). Without this arm the drop of the
// source freed the returned payload: `got: made` became `got:    ` — a silent
// use-after-free that 4ed84631 (before the enum work) did not have.
func (m *Module) moveTransfersOwnership(f *Function, inst *Inst) bool {
	if !isTransferringMove(inst) || m.isOptionPeelMove(f, inst) {
		return false
	}
	dst := moveDst(inst)
	if dst > NoVal && m.isParamValue(f, dst) {
		return true
	}
	return !m.enumOwnsPayload[inst.Args[0]]
}

// isParamValue reports whether v is one of f's parameters — an input parameter
// (borrowed storage owned by the caller) or a result parameter (the caller's
// slot for the return value). Both outlive this frame, so a move into one is an
// ownership transfer out of the function rather than an intra-frame alias.
func (m *Module) isParamValue(f *Function, v ValueID) bool {
	if f == nil || v <= NoVal {
		return false
	}
	for _, p := range f.Params {
		if p == v {
			return true
		}
	}
	for _, p := range f.ResultParams {
		if p == v {
			return true
		}
	}
	return false
}

// checkMoves flags use-after-move in nolang's sense: a value DROPPED after it
// has been moved (its ownership transferred to the move destination) is a
// double-free — the destination already owns the heap pointer, so dropping the
// source frees the same pointer twice. nolang's OpMove is a BITWISE COPY
// (emitMove does load+store), NOT a C++-style move that invalidates the source,
// so a plain READ of a moved value is always SAFE and is deliberately NOT
// flagged. Flagging reads was a false positive that blocked the MIR backend on
// std-hash.no (md5 reads its `data` []byte many times after moving it in).
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
				if m.moveTransfersOwnership(f, inst) {
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
	// a false positive that blocked the MIR backend on std-hash.no.
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
			if m.moveTransfersOwnership(f, inst) {
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
