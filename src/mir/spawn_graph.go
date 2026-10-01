package mir

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Spawn-graph linearization — NOLANG-OWNERSHIP-MODEL.md §1.4 / §4.3, roadmap P3.
//
// WHY THIS EXISTS
// --------------
// "There is only one coroutine task, so it can run linearly" is right in
// intuition and wrong in form: "one task" is not the same as "linear". §1.4's
// counterexample is a single spawn whose argument is overwritten before the
// await — one task, two overlapping execution contexts, and "who frees last" is
// not statically decidable.
//
// So the criterion is a property of the SPAWN GRAPH, not of a count. A spawn
// edge may be downgraded to a static tier (S/C, no RC) only when all four hold:
//
//	1. the handle does not escape the caller;
//	2. EVERY path from the spawn to the function exit passes exactly one
//	   OpAwait of that handle;
//	3. the callee does not spawn (no nested OpRun, transitively);
//	4. the values crossing the boundary are unused after the await.
//
// SCOPE: P3 is REPORT ONLY. Nothing here changes codegen, drop insertion, or
// tier assignment; it exists so the verdicts can be checked by hand before P5
// starts acting on them. The only observable is the stderr dump gated by
// NOLANG_MIR_SPAWN_GRAPH=1, which is off by default.
//
// PER EDGE, NOT PER HANDLE (§4.3): one function may hold a linearizable spawn
// next to an escaping one, and the first must not be dragged into the R tier by
// the second. Every verdict below is computed for a single OpRun site.
//
// WHAT IS DELIBERATELY NOT A CRITERION
// ------------------------------------
// §1.4's motivating counterexample (`h = run f(s); s = 'overwrite'; awy h`) is
// NOT one of the four. It is already closed by P0: emitAsyncRun deep-copies
// every heap-owned argument into the task's own argbuf at spawn time, so the
// caller's later overwrite cannot pull the buffer out from under the task. The
// four criteria are what remains undecided AFTER that.

// SpawnEdge is one `run` site (an OpRun instruction) with the four §4.3
// criteria evaluated for it.
type SpawnEdge struct {
	Caller     FuncID
	CallerName string
	Callee     FuncID
	CalleeName string
	Block      BlockID
	Inst       InstID

	// Forwarded marks `run <var>`: the operand is already a handle, so this is
	// not a spawn site at all — emitAsyncRun just copies the handle. It gets an
	// entry so the report accounts for every OpRun, but no verdict.
	Forwarded bool

	// Handles is the handle value plus every value that aliases it (`h2 = h`).
	// The await criterion counts awaits of ANY of them: `awy h2` completes the
	// same task as `awy h`.
	Handles []ValueID

	// The four criteria. All four true => Linearizable.
	NoEscape bool // 1
	OneAwait bool // 2
	Quiet    bool // 3: the callee (transitively) contains no OpRun
	ArgsDead bool // 4

	Linearizable bool

	// Why, one line per failing criterion. Empty when Linearizable.
	Reasons []string
	// Non-blocking observations (e.g. a loop back edge was cut) — recorded so a
	// "linear" verdict can still be audited.
	Notes []string
}

// Verdict renders one line suitable for the stderr dump and for tests.
func (e SpawnEdge) Verdict() string {
	callee := e.CalleeName
	if callee == "" {
		callee = "<unresolved>"
	}
	v := "NON-LINEAR"
	if e.Linearizable {
		v = "LINEAR"
	}
	if e.Forwarded {
		v = "FORWARDED"
	}
	return fmt.Sprintf("%s: %s -> %s (block %d, inst %d)", v, e.CallerName, callee, e.Block, e.Inst)
}

// SpawnGraph evaluates every OpRun in the module. Report-only: it does not
// mutate the module. Requires BuildCFG (block successors) to have run.
func (m *Module) SpawnGraph() []SpawnEdge {
	var out []SpawnEdge
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		// Deterministic order: block order, then instruction order.
		for _, bid := range f.Blocks {
			b := m.Block(bid)
			if b == nil {
				continue
			}
			for _, iid := range b.Insts {
				inst := m.Inst(iid)
				if inst == nil || inst.Op != OpRun {
					continue
				}
				out = append(out, m.evalSpawnEdge(f, b, inst))
			}
		}
	}
	return out
}

func (m *Module) evalSpawnEdge(f *Function, b *Block, inst *Inst) SpawnEdge {
	e := SpawnEdge{
		Caller:     f.ID,
		CallerName: f.Name,
		Callee:     NoFunc,
		Block:      b.ID,
		Inst:       inst.ID,
	}
	if inst.Sym == "" {
		e.Forwarded = true
		e.Reasons = append(e.Reasons, "`run <var>` forwards an existing handle: not a spawn site")
		return e
	}
	e.CalleeName = inst.Sym
	if cid, ok := m.FuncByName[inst.Sym]; ok {
		e.Callee = cid
	} else {
		e.Reasons = append(e.Reasons, fmt.Sprintf("callee %q is not a lowered function (extern/builtin): criterion 3 cannot be checked", inst.Sym))
	}

	e.Handles = m.handleAliasSet(f, inst.Dst)
	handles := map[ValueID]bool{}
	for _, h := range e.Handles {
		handles[h] = true
	}

	// 1. escape.
	escapes := m.spawnHandleEscapes(f, handles)
	// OUT-PARAM RETURN: `mk = () (h i64) { h = run dbl(21) }` moves the handle
	// into a result slot. That is an OpMove, so the alias closure swallows it
	// and — unlike OpReturn, whose args are visible — nothing here would
	// otherwise notice the handle left the frame. The task outlives the caller,
	// so this is precisely the case criterion 1 exists to reject.
	for _, h := range e.Handles {
		for _, rp := range f.ResultParams {
			if h == rp {
				escapes = append(escapes, "returned through an out-param")
			}
		}
	}
	e.NoEscape = len(escapes) == 0
	if !e.NoEscape {
		e.Reasons = append(e.Reasons, "handle escapes: "+strings.Join(escapes, "; "))
	}

	// 2. exactly one await on every path to exit.
	startIdx := 0
	for i, iid := range b.Insts {
		if iid == inst.ID {
			startIdx = i + 1
			break
		}
	}
	oks, notes, loopCut := m.spawnAwaitPaths(b.ID, startIdx, inst.ID, handles)
	e.OneAwait = oks
	if !oks {
		e.Reasons = append(e.Reasons, "await is not exactly once on every path to exit: "+strings.Join(notes, "; "))
	}
	if loopCut {
		e.Notes = append(e.Notes, "a back edge to the spawn was cut (loop-carried spawn): the verdict covers one iteration")
	}

	// 3. callee does not spawn.
	spawns, chain := m.spawnsTransitively(e.Callee)
	e.Quiet = !spawns
	if spawns {
		e.Reasons = append(e.Reasons, "callee spawns (nested OpRun) via "+strings.Join(chain, " -> "))
	}

	// 4. boundary values unused after the await.
	dead, liveNotes := m.spawnArgsDeadAfterAwait(f, inst, handles)
	e.ArgsDead = dead
	if !dead {
		e.Reasons = append(e.Reasons, "boundary value still used after the await: "+strings.Join(liveNotes, "; "))
		e.Notes = append(e.Notes, "P0 deep-copies every heap-owned argument into the task's own "+
			"buffer at spawn, so this is not an ownership hazard today; the criterion is "+
			"reported as written because P5 needs it once the copy is replaced by RC")
	}

	// §1.4's motivating shape, recorded rather than decided: an owned argument
	// overwritten before the await. It is NOT one of the four criteria — P0
	// deep-copies every owned argument into the task's own buffer at spawn, so
	// the caller's overwrite cannot pull the buffer out from under the task —
	// but a reader checking this report against §1.4 will look for it, and
	// silence there reads like an omission.
	owned := 0
	for _, a := range inst.Args {
		if m.isOwnedVal(f, a) {
			owned++
		}
	}
	if owned > 0 {
		e.Notes = append(e.Notes, fmt.Sprintf("%d owned argument(s) are deep-copied into the "+
			"task's buffer at spawn (P0), which is what covers §1.4's \"argument overwritten "+
			"before the await\" shape — it is deliberately not one of the four criteria", owned))
	}

	e.Linearizable = e.NoEscape && e.OneAwait && e.Quiet && e.ArgsDead
	return e
}

// SpawnArgClasses classifies a callee's non-result parameters for the
// spawn-boundary ownership protocol, indexed by ARGUMENT POSITION — the same
// indexing emitAsyncRun uses for inst.Args. It is the SINGLE source of truth
// for that classification.
//
// WHY IT IS A Module METHOD AND NOT A codegen METHOD. Two layers now need the
// answer, and they must not be allowed to disagree:
//
//   - codegen (emitAsyncRun / asyncWrapperFor) uses it to decide what to
//     deep-copy into the task's argbuf and what the wrapper then frees;
//   - analysis (insertDrops) uses it to decide whether a spawn argument may be
//     MOVED into the task instead of copied. A move is only sound when the
//     wrapper will actually free the payload: with kind == "" the wrapper frees
//     only the container, so a moved payload would leak, and a moved payload
//     that the wrapper DOES free must have had its source drop suppressed.
//
// One question, one implementation. The emitter's classification is reached
// through a throwaway codegen with only `mod` set, exactly as
// Module.OptionPayloadBoxed reaches the emitter's sizing rules — see the
// rationale there, which applies verbatim: "if the two ever disagreed, one side
// would free a box the other shared."
//
// The throwaway is sound because asyncArgKinds resolves each parameter's type
// through localTypeOf, whose only function-scoped lookup is
// `mod.Func(c.cf).LocalTypes[p]`. A ValueID belongs to exactly one function, so
// for a CALLEE parameter that lookup always misses and the answer comes from
// mod.Value(p).Type — independent of which function `c.cf` names. (The throwaway
// leaves c.cf at 0; the real call leaves it at the caller.)
//
// vecDeepClone's "is there a clone helper" guard is evaluated on the throwaway
// codegen, so the helper it would register is discarded — which is what we want
// for a pure query. The clone site re-asks through the real codegen and
// registers it there. extraFuncs must nevertheless be a REAL map: vecDeepClone
// memoizes the helper name in it, and a nil map panics ("assignment to entry in
// nil map"), which the codegen entry point reports as a compile error.
func (m *Module) SpawnArgClasses(cf *Function) ([]string, []TypeID, []string) {
	c := &codegen{mod: m, extraFuncs: map[string]bool{}}
	return c.asyncArgKinds(cf)
}

// DumpSpawnArgMoveStats reports, for every `run` argument, whether the P5
// "parameter side" could avoid the P0 deep copy — i.e. whether the argument's
// SOURCE is dead immediately after the spawn, so ownership could be MOVED
// instead of copied. Gated on NOLANG_MIR_SPAWN_ARG_MOVE=1; report-only.
//
// WHY A SEPARATE SWITCH. This answers a question the four §4.3 criteria do not.
// They ask whether an EDGE is linearizable; this asks whether one ARGUMENT's
// copy is avoidable, so the granularity differs.
//
// Criterion 2 (every path awaits exactly once) is NOT required, and that is a
// deliberate departure from the estimate §1.3.1 recorded when this was still
// unimplemented. The reason is that a never-awaited task leaks its argbuf
// EITHER WAY: the wrapper is what frees the payload (asyncWrapperFor's w_free),
// and it only runs when the task is awaited. Copying leaks the copy; moving
// leaks the moved buffer. So the move does not trade a copy for a leak — it
// trades a copy for the same leak, minus the copy.
//
// It exists so the size of the remaining P5 work is a MEASUREMENT rather than an
// estimate, exactly as §1.2.3 closed out §4.2's remaining branch. The `moved=`
// field it prints is read back from m.spawnArgMoves, i.e. it reports what
// insertDrops actually decided rather than recomputing it.
func (m *Module) DumpSpawnArgMoveStats() {
	if os.Getenv("NOLANG_MIR_SPAWN_ARG_MOVE") == "" {
		return
	}
	// Edge verdicts, keyed by spawn instruction, so the leak gate reuses the
	// SAME criterion-2 computation rather than a second copy of it.
	oneAwait := map[InstID]bool{}
	for _, e := range m.SpawnGraph() {
		oneAwait[e.Inst] = e.OneAwait
	}
	var total, owned, dead, deadAwait, ownedAwait, moved int
	classesOf := map[FuncID][]string{}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		_, liveOut := m.Liveness(f)
		for _, bid := range f.Blocks {
			b := m.Block(bid)
			if b == nil {
				continue
			}
			for _, iid := range b.Insts {
				inst := m.Inst(iid)
				if inst == nil || inst.Op != OpRun || inst.Sym == "" {
					continue
				}
				cid, ok := m.FuncByName[inst.Sym]
				if !ok {
					continue
				}
				classes, ok := classesOf[cid]
				if !ok {
					classes, _, _ = m.SpawnArgClasses(m.Func(cid))
					classesOf[cid] = classes
				}
				oa := oneAwait[iid]
				for idx, a := range inst.Args {
					total++
					own := m.dropOwnsHeap(f, a)
					// The move condition, spelled with the SAME two predicates
					// insertDrops uses to decide a value is dead: not live out of
					// the block, and no non-drop read after this instruction.
					d := !liveOut[b.ID][a] && !m.readNonDropAfterInBlock(b.ID, iid, a)
					// Did insertDrops actually take the move? This is the
					// GROUND TRUTH (read back from the module), not a
					// recomputation — the point of the dump is to show what the
					// pass did.
					mv := m.spawnArgMoves[a]
					if own {
						owned++
						if d {
							dead++
							if oa {
								deadAwait++
							}
						}
						if oa {
							ownedAwait++
						}
					}
					if mv {
						moved++
					}
					fmt.Fprintf(os.Stderr,
						"[spawn-arg] %s inst=%d arg=%d val=%d kind=%v ownedVal=%v ownsHeap=%v deadAfterSpawn=%v oneAwait=%v moved=%v callee=%s\n",
						f.Name, iid, idx, a, m.typeKindOf(f, a), m.isOwnedVal(f, a), own, d, oa, mv, inst.Sym)
				}
			}
		}
	}
	fmt.Fprintf(os.Stderr,
		"[spawn-arg] totals: args=%d ownsHeap=%d ownsHeap+deadAfterSpawn=%d ownsHeap+deadAfterSpawn+oneAwait=%d ownsHeap+oneAwait=%d moved=%d\n",
		total, owned, dead, deadAwait, ownedAwait, moved)
}

// typeKindOf resolves a value's Kind for diagnostics (never decides anything).
func (m *Module) typeKindOf(f *Function, v ValueID) TypeKind {
	if t, ok := f.LocalTypes[v]; ok {
		if ty := m.Type(t); ty != nil {
			return ty.Kind
		}
	}
	if val := m.Value(v); val != nil {
		if ty := m.Type(val.Type); ty != nil {
			return ty.Kind
		}
	}
	return KindVoid
}

// handleAliasSet returns root plus every value that is a copy of it (or of a
// copy), following OpMove (both encodings), OpCast and OpPhi.
//
// WHY: `h2 = h; awy h` is TWO references to one task, and `awy h2` completes the
// same task. An analysis that only looked at the OpRun's own Dst would conclude
// "h2 was never awaited" and would separately miss that `awy h2` is a use of
// this edge's handle.
func (m *Module) handleAliasSet(f *Function, root ValueID) []ValueID {
	if root <= NoVal {
		return nil
	}
	set := map[ValueID]bool{root: true}
	order := []ValueID{root}
	for changed := true; changed; {
		changed = false
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
				var dst ValueID = NoVal
				switch inst.Op {
				case OpMove:
					// Encoding 1: Dst = the fresh destination, Args = [src].
					// Encoding 2: Dst = NoVal, Args = [src, dst] — an assignment
					// to an already-bound variable. Both are copies.
					if inst.Dst > NoVal {
						dst = inst.Dst
					} else if len(inst.Args) >= 2 {
						dst = inst.Args[1]
					}
				case OpCast, OpPhi:
					dst = inst.Dst
				default:
					continue
				}
				if dst <= NoVal || set[dst] {
					continue
				}
				for _, a := range inst.Args {
					if set[a] {
						set[dst] = true
						order = append(order, dst)
						changed = true
						break
					}
				}
			}
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	return order
}

// spawnHandleEscapes applies §4.3's decidable form of "the handle does not
// escape": it is used only by OpAwait, is not stored anywhere, is not passed to
// a call, and is not returned. Anything else is an escape — the rule is
// deliberately conservative, because the R tier is the safe fallback.
//
// Known-conservative case: `async-cancel(h)` is a call, so it is reported as an
// escape even though the builtin neither stores nor retains the handle. Flagged
// rather than special-cased: whitelisting a callee is how such an analysis
// starts lying.
func (m *Module) spawnHandleEscapes(f *Function, handles map[ValueID]bool) []string {
	var notes []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			notes = append(notes, s)
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
			for _, a := range inst.Args {
				if !handles[a] {
					continue
				}
				if esc, msg := spawnHandleEscapeOp(inst.Op, inst.Sym); esc {
					add(fmt.Sprintf("%s at inst %d", msg, iid))
				}
			}
		}
		if blk.Term != nil {
			for _, a := range blk.Term.Args {
				if handles[a] {
					add(fmt.Sprintf("returned at block %d", bid))
				}
			}
		}
	}
	return notes
}

func spawnHandleEscapeOp(op Op, sym string) (bool, string) {
	switch op {
	case OpAwait:
		// The one legal use — and the use criterion 2 counts.
		return false, ""
	case OpTaskRetain:
		// The compiler's own reference marker for a handle copy.
		return false, ""
	case OpMove, OpCast, OpPhi:
		// Alias formation: tracked by handleAliasSet, not an escape.
		return false, ""
	case OpDrop:
		// A handle dropped without ever being awaited LEAKS (rc stays above
		// zero); it does not escape. A separate defect from the one this
		// analysis decides, so it must not silently flip the verdict.
		return false, ""
	case OpSetField:
		return true, "stored into a struct field"
	case OpIndexStore:
		return true, "stored into a container"
	case OpStore:
		return true, "stored through a pointer or global"
	case OpCall, OpCallExtern, OpCallFFI:
		name := sym
		if name == "" {
			name = "<indirect>"
		}
		return true, "passed as an argument to " + name
	case OpRun:
		return true, "re-spawned by `run`"
	}
	return true, "used by " + op.String()
}

// spawnAwaitPaths walks every path from just after the spawn to a function
// exit, counting awaits of `handles`. It reports whether all paths saw exactly
// one.
//
// A back edge that reaches the spawn instruction again is CUT rather than
// followed: that instruction belongs to the NEXT iteration and spawns a
// different task. The segment already walked must still have contained its
// await, or the handle is genuinely abandoned once per iteration.
func (m *Module) spawnAwaitPaths(start BlockID, startIdx int, spawnInst InstID, handles map[ValueID]bool) (bool, []string, bool) {
	type state struct {
		b      BlockID
		idx    int
		awaits int
	}
	const capAwaits = 3 // "3+" is enough; no verdict distinguishes beyond that
	const maxSteps = 200000

	bad := map[string]bool{}
	seen := map[state]bool{}
	stack := []state{{start, startIdx, 0}}
	loopCut := false
	steps := 0

	for len(stack) > 0 {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		steps++
		if steps > maxSteps {
			bad["path walk exceeded its step budget (cyclic or very large CFG)"] = true
			break
		}
		blk := m.Block(st.b)
		if blk == nil {
			continue
		}
		if st.idx >= len(blk.Insts) {
			if blk.Term == nil || blk.Term.Op == OpReturn || len(blk.Term.Targets) == 0 {
				if st.awaits != 1 {
					bad[fmt.Sprintf("a path to exit has %d await(s), want exactly 1", st.awaits)] = true
				}
				continue
			}
			for _, t := range blk.Term.Targets {
				st2 := state{t, 0, st.awaits}
				if !seen[st2] {
					seen[st2] = true
					stack = append(stack, st2)
				}
			}
			continue
		}
		iid := blk.Insts[st.idx]
		if iid == spawnInst {
			loopCut = true
			if st.awaits != 1 {
				bad[fmt.Sprintf("a path back to the spawn has %d await(s), want exactly 1", st.awaits)] = true
			}
			continue
		}
		awaits := st.awaits
		if inst := m.Inst(iid); inst != nil && inst.Op == OpAwait && len(inst.Args) > 0 && handles[inst.Args[0]] {
			awaits++
			if awaits > capAwaits {
				awaits = capAwaits
			}
		}
		st2 := state{st.b, st.idx + 1, awaits}
		if !seen[st2] {
			seen[st2] = true
			stack = append(stack, st2)
		}
	}
	if len(bad) == 0 {
		return true, nil, loopCut
	}
	notes := make([]string, 0, len(bad))
	for n := range bad {
		notes = append(notes, n)
	}
	sort.Strings(notes)
	return false, notes, loopCut
}

// spawnsTransitively reports whether fid, or anything it calls, contains an
// OpRun. Direct-only would be unsound: `emitAsyncAwait`'s synchronous fast path
// is documented as correct only for FLAT await trees, so a nested spawn two
// levels down breaks it just as much as a direct one.
func (m *Module) spawnsTransitively(fid FuncID) (bool, []string) {
	if fid == NoFunc || fid < 0 {
		return false, nil
	}
	seen := map[FuncID]bool{}
	var walk func(id FuncID, chain []string) (bool, []string)
	walk = func(id FuncID, chain []string) (bool, []string) {
		if id == NoFunc || id < 0 || seen[id] {
			return false, nil
		}
		seen[id] = true
		f := m.Func(id)
		if f == nil {
			return false, nil
		}
		chain = append(chain, f.Name)
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
				if inst.Op == OpRun && inst.Sym != "" {
					return true, chain
				}
				if inst.Op != OpCall || inst.Sym == "" {
					continue
				}
				cid, ok := m.FuncByName[inst.Sym]
				if !ok {
					continue
				}
				if sp, ch := walk(cid, chain); sp {
					return true, ch
				}
			}
		}
		return false, nil
	}
	return walk(fid, nil)
}

// spawnArgsDeadAfterAwait implements criterion 4: no value that crossed the
// boundary is read again by the caller once the task has been awaited.
//
// "After the await" is computed as: the instructions following the await in its
// own block, plus every block reachable from it. Drop instructions are not
// reads — the same distinction `readNonDropAfterInBlock` makes, and for the same
// reason: a drop is the END of a value's life, not evidence that anything still
// depends on the buffer.
func (m *Module) spawnArgsDeadAfterAwait(f *Function, spawn *Inst, handles map[ValueID]bool) (bool, []string) {
	args := make([]ValueID, 0, len(spawn.Args))
	for _, a := range spawn.Args {
		if a > NoVal {
			args = append(args, a)
		}
	}
	if len(args) == 0 {
		return true, nil
	}
	argSet := map[ValueID]bool{}
	for _, a := range args {
		argSet[a] = true
	}
	// Locate the awaits of this handle: their block, and the index of the
	// FIRST await within that block (everything after it is "after the await").
	awaitMinIdx := map[BlockID]int{}
	for _, bid := range f.Blocks {
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		for i, iid := range blk.Insts {
			inst := m.Inst(iid)
			if inst == nil || inst.Op != OpAwait || len(inst.Args) == 0 || !handles[inst.Args[0]] {
				continue
			}
			if prev, ok := awaitMinIdx[bid]; !ok || i < prev {
				awaitMinIdx[bid] = i
			}
		}
	}
	if len(awaitMinIdx) == 0 {
		// Never awaited: nothing can be "used after the await". Criteria 1/2
		// carry the verdict; this one is vacuously satisfied.
		return true, nil
	}
	// Reachable from an await. Two sets, because they mean different things:
	//   reachAll              - the await's own block and everything after it
	//   reachViaSuccessorOnly - reached from a DIFFERENT await block, so even
	//                           the prefix of that block counts as "after"
	// Without the second set, an instruction sitting before the await in a
	// block that a loop brings back to would be skipped, i.e. a real
	// post-await use would go unreported.
	reachAll := map[BlockID]bool{}
	reachViaSuccessorOnly := map[BlockID]bool{}
	var visit func(BlockID, bool)
	visit = func(bid BlockID, fromSelf bool) {
		if !fromSelf {
			reachViaSuccessorOnly[bid] = true
		}
		if reachAll[bid] {
			return
		}
		reachAll[bid] = true
		blk := m.Block(bid)
		if blk == nil || blk.Term == nil {
			return
		}
		for _, t := range blk.Term.Targets {
			visit(t, false)
		}
	}
	for bid := range awaitMinIdx {
		visit(bid, true)
	}

	notes := map[string]bool{}
	for _, bid := range f.Blocks {
		if !reachAll[bid] {
			continue
		}
		blk := m.Block(bid)
		if blk == nil {
			continue
		}
		startIdx := 0
		if idx, ok := awaitMinIdx[bid]; ok && !reachViaSuccessorOnly[bid] {
			startIdx = idx + 1
		}
		for i := startIdx; i < len(blk.Insts); i++ {
			inst := m.Inst(blk.Insts[i])
			if inst == nil || inst.Op == OpDrop {
				continue
			}
			for _, a := range inst.Args {
				if argSet[a] {
					notes[fmt.Sprintf("value %d is read by %s at inst %d", a, inst.Op, inst.ID)] = true
				}
			}
		}
		if blk.Term != nil {
			for _, a := range blk.Term.Args {
				if argSet[a] {
					notes[fmt.Sprintf("value %d is read by the terminator of block %d", a, bid)] = true
				}
			}
		}
	}
	if len(notes) == 0 {
		return true, nil
	}
	out := make([]string, 0, len(notes))
	for n := range notes {
		out = append(out, n)
	}
	sort.Strings(out)
	return false, out
}

// DumpSpawnGraph writes the P3 report to stderr. Gated by
// NOLANG_MIR_SPAWN_GRAPH=1 — off by default, so a normal build prints nothing
// and emits byte-identical output.
func (m *Module) DumpSpawnGraph() {
	if os.Getenv("NOLANG_MIR_SPAWN_GRAPH") == "" {
		return
	}
	edges := m.SpawnGraph()
	fmt.Fprintf(os.Stderr, "[spawn-graph] %d OpRun site(s)\n", len(edges))
	for _, e := range edges {
		fmt.Fprintf(os.Stderr, "[spawn-graph] %s\n", e.Verdict())
		for _, r := range e.Reasons {
			fmt.Fprintf(os.Stderr, "[spawn-graph]     - %s\n", r)
		}
		for _, n := range e.Notes {
			fmt.Fprintf(os.Stderr, "[spawn-graph]     (note) %s\n", n)
		}
	}
}
