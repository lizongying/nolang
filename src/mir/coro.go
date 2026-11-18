package mir

import "strings"

// Coroutines: turning a spawn-bound clone into a resumable state machine.
//
// A `co` spawns a task whose body is the `-async` clone of the callee (see
// hir.MonomorphizeGo). Until this pass existed the clone ran to completion in
// one shot: `emitAsyncAwait` invoked %task.resume_fn once and the whole body
// ran, so a `co` inside a loop could not interleave with anything and an
// `async-yield()` was a no-op that happened to re-enqueue a task nobody ran.
//
// PlanCoroutines turns exactly those clones into COROUTINES:
//
//   - the function's locals move from stack slots into a heap FRAME hanging off
//     %task.frame, so they survive a suspension;
//   - every suspension point (OpAwait / OpYield / OpChanRecv) becomes the
//     terminator of its block — an OpCoroPark that records a state number and
//     returns to the scheduler;
//   - the function grows a synthetic entry block (Dispatch) that reads __state
//     and jumps straight to the block that follows the suspension it parked on.
//
// Two design constraints drive the shape of this pass:
//
//  1. It runs AFTER Analyze. Drop insertion, view liveness and the spawn-arg
//     ownership decisions are all computed on the ORIGINAL control-flow graph,
//     where a suspension point is just an ordinary instruction. Splitting
//     afterwards cannot move a drop across a park, because no drop was ever
//     placed there. Running it before Analyze would require teaching every
//     dataflow pass that OpCoroPark is a non-sink with a live successor.
//
//  2. A park block keeps the ORIGINAL terminator on its continuation, and gets
//     OpCoroPark itself. The continuation block is therefore still reachable
//     (Targets[0] is a real CFG edge), which keeps dominator-based reasoning —
//     and BuildCFG, which derives edges from Targets alone — correct.

// coroAsyncSuffix is the HIR name suffix hir.MonomorphizeGo appends to the
// callee of a `co`. Only such a clone can be spawned, so only it is eligible.
const coroAsyncSuffix = "-async"

// CoroFrameHeader is the number of leading frame fields before slot 0:
// { i32 __state, i32 __pad }. Slot i is field index i+CoroFrameHeader.
const CoroFrameHeader = 2

// PlanCoroutines rewrites every spawn-bound clone that contains a suspension
// point into a state machine. Idempotent: a function already marked IsCoro is
// skipped. Non-coroutine functions are left untouched, so a module that never
// spawns is bit-identical to before this pass ran.
func (m *Module) PlanCoroutines() {
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.ID == NoFunc || f.IsExtern || f.IsCoro {
			continue
		}
		if !strings.HasSuffix(f.Name, coroAsyncSuffix) {
			continue
		}
		if !m.functionHasSuspend(f) {
			continue
		}
		m.planCoroutine(f)
	}
	m.BuildCFG()
}

// isSuspendInst reports whether an instruction is a SUSPENSION POINT, i.e. a
// place where the coroutine must give up the CPU and be resumable.
//
// OpChanRecv qualifies: a receive from an empty channel has to wait for a
// sender, and waiting is only possible if the frame and the state number
// survive (see CoroParkChan). `async-yield()` arrives as an ordinary call and is
// recognized by name — only codegen knows the builtin's forward name, so the
// marker op is created here.
func isSuspendInst(inst *Inst) bool {
	if inst == nil {
		return false
	}
	switch inst.Op {
	case OpAwait, OpYield, OpChanRecv:
		return true
	case OpCall:
		return isYieldCall(inst)
	}
	return false
}

// isYieldCall reports whether an instruction is a call to `async-yield()`.
//
// The builtin survives lowering as an ordinary OpCall whose Sym is the builtin's
// forward name, because only codegen knows it (builtin_call.go). This pass has to
// see it as a SUSPENSION POINT, so it is recognized here and rewritten to the
// OpYield marker — which is what keeps a call to it outside a coroutine behaving
// exactly as before (see emitInst's OpYield case).
func isYieldCall(inst *Inst) bool {
	return inst != nil && inst.Op == OpCall && strings.HasSuffix(inst.Sym, "async-yield")
}

// functionHasSuspend reports whether the function contains any instruction that
// can park.
func (m *Module) functionHasSuspend(f *Function) bool {
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if isSuspendInst(m.Inst(iid)) {
				return true
			}
		}
	}
	return false
}

// newCoroBlock allocates a block and appends it to the function. Pointers into
// m.Blocks must never be held across this call: appending can reallocate.
func (m *Module) newCoroBlock(f *Function, name string) BlockID {
	bid := BlockID(len(m.Blocks))
	m.Blocks = append(m.Blocks, Block{ID: bid, Name: name})
	f.Blocks = append(f.Blocks, bid)
	return bid
}

func (m *Module) planCoroutine(f *Function) {
	ci := &CoroInfo{
		FrameName: "frame." + itoa32(int32(f.ID)),
		SlotIdx:   map[ValueID]int32{},
		Parks:     map[BlockID]CoroPark{},
	}
	frameSlot := func(v ValueID) {
		if v <= NoVal {
			return
		}
		if _, ok := ci.SlotIdx[v]; ok {
			return
		}
		ci.SlotIdx[v] = int32(len(ci.Slots))
		ci.Slots = append(ci.Slots, v)
	}
	// The method receiver keeps aliasing the caller-provided result buffer
	// (which is heap-allocated by the spawn site and outlives every
	// suspension), so it is the one parameter that needs no frame slot.
	// Everything else — including the other out-params, whose values are
	// written back at return — lives in the frame.
	self := NoVal
	if f.IsMethod && len(f.ResultParams) > 0 {
		self = f.ResultParams[0]
	}
	for _, p := range f.Params {
		if p == self {
			continue
		}
		frameSlot(p)
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
			if inst.Dst > NoVal {
				frameSlot(inst.Dst)
			}
			for _, rv := range inst.Results {
				frameSlot(rv)
			}
		}
	}

	body := f.Entry
	state := int32(1)
	for _, bid := range append([]BlockID(nil), f.Blocks...) {
		insts := append([]InstID(nil), m.Blocks[bid].Insts...)
		if !m.instsHaveSuspend(insts) {
			continue
		}
		// Segment the block at each suspension point. A segment ENDS at a
		// suspend, and the suspend itself opens the next segment, so on resume
		// the continuation re-executes it — for an await that means "read the
		// result that is now ready", for a receive "take the value that has
		// just been checked for".
		var segs [][]InstID
		var kinds []CoroParkKind
		var handles []ValueID
		cur := make([]InstID, 0, len(insts))
		for _, iid := range insts {
			inst := m.Inst(iid)
			if inst == nil {
				continue
			}
			if !isSuspendInst(inst) {
				cur = append(cur, iid)
				continue
			}
			segs = append(segs, cur)
			kind := CoroParkYield
			h := NoVal
			switch {
			case inst.Op == OpAwait:
				kind = CoroParkAwait
				if len(inst.Args) > 0 {
					h = inst.Args[0]
				}
			case inst.Op == OpChanRecv:
				kind = CoroParkChan
				if len(inst.Args) > 0 {
					h = inst.Args[0]
				}
			case isYieldCall(inst):
				// Rewrite the call to the marker so codegen emits nothing
				// for it: the park terminator does the enqueue-and-return.
				inst.Op = OpYield
				inst.Sym = ""
				inst.Args = nil
			}
			kinds = append(kinds, kind)
			handles = append(handles, h)
			cur = []InstID{iid}
		}
		segs = append(segs, cur)

		// Build the chain of segment blocks. The first segment stays in `bid`;
		// the rest are fresh blocks, and the ORIGINAL terminator moves to the
		// last one.
		origTerm := m.Blocks[bid].Term
		m.Blocks[bid].Insts = segs[0]
		forward := make([]BlockID, len(kinds))
		for k := range kinds {
			forward[k] = m.newCoroBlock(f, "cs."+itoa32(int32(bid))+"."+itoa32(int32(k)))
			m.Blocks[forward[k]].Insts = segs[k+1]
		}
		for k := range kinds {
			park := CoroPark{State: state, Kind: kinds[k], Handle: handles[k]}
			state++
			// Segment k parks; execution resumes in the block that holds
			// segment k+1, which is always forward[k] (forward[j] holds
			// segs[j+1]).
			park.Cont = forward[k]
			park.Resume = park.Cont
			if park.Kind == CoroParkChan {
				// A woken channel waiter has to re-run the readiness test, so
				// it resumes at the park block itself, not at the receive.
				park.Resume = parkBlockOf(bid, forward, k)
			}
			term := &Term{Op: OpCoroPark, Targets: []BlockID{park.Cont}}
			term.Int = int64(park.State)
			switch park.Kind {
			case CoroParkAwait:
				term.Sym = "await"
			case CoroParkChan:
				term.Sym = "chan"
			default:
				term.Sym = "yield"
			}
			if park.Handle > NoVal && park.Kind != CoroParkYield {
				term.Args = []ValueID{park.Handle}
			}
			// The park's own block: segment k (for k == 0 that is `bid`).
			if k == 0 {
				m.Blocks[bid].Term = term
			} else {
				m.Blocks[forward[k-1]].Term = term
			}
			ci.Parks[parkBlockOf(bid, forward, k)] = park
			ci.States = append(ci.States, park)
		}
		if origTerm != nil {
			m.Blocks[forward[len(forward)-1]].Term = origTerm
		}
	}

	// Dispatch: the synthetic entry. Its OpBr to the body is the fallthrough of
	// the state comparison chain, so BuildCFG sees the body as reachable from
	// the entry even before codegen writes the chain out.
	d := m.newCoroBlock(f, "cs.dispatch")
	m.Blocks[d].Term = &Term{Op: OpBr, Targets: []BlockID{body}}
	ci.Dispatch = d
	ci.Body = body
	f.Blocks = append([]BlockID{d}, f.Blocks...)
	f.Entry = d
	f.IsCoro = true
	f.Coro = ci
}

// parkBlockOf returns the block that holds segment k of a split: segment 0 stays
// in the original block, segment k lives in forward[k-1].
func parkBlockOf(orig BlockID, forward []BlockID, k int) BlockID {
	if k == 0 {
		return orig
	}
	return forward[k-1]
}

func (m *Module) instsHaveSuspend(insts []InstID) bool {
	for _, iid := range insts {
		if isSuspendInst(m.Inst(iid)) {
			return true
		}
	}
	return false
}

// itoa32 is a tiny decimal formatter, kept local so the pass does not pull in
// fmt (the MIR package deliberately keeps module names allocation-light).
func itoa32(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
