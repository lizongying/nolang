package hir

import (
	"os"
	"sort"
	"strings"
)

// Coroutine groups (協程組) — HIR desugaring.
//
// SURFACE FORM
// ------------
// An `awy { ... }` block is a coroutine group:
//
//	awy {
//	    co produce(ch, 1)
//	    co produce(ch, 2)
//	}
//
// `co` is a statement that spawns a PROCEDURE and produces no value: the callee
// writes its result to a channel, read after the group. Each `co` becomes
// `__agN = run <callee>-async` (the clone is synthesized by MonomorphizeGo), and
// the group awaits every in-flight task at its end:
//
//	__ag0 = run produce-async(ch, 1)
//	__ag1 = run produce-async(ch, 2)
//	awy __ag0
//	awy __ag1
//
// A statement that is not a `co` is a BARRIER: all in-flight spawns are awaited
// before it runs. That is the ONLY reason a spawn is awaited early — `co` never
// produces a result, so there is no result-dependency analysis to do.
//
// WHY A DESUGARING AND NOT A NEW MIR OP
// -------------------------------------
// `run` / `awy` already own the hard part: emitAsyncRun deep-copies every
// heap-owned argument into the task's own buffer, asyncHandles/asyncResTypes
// carry the result type across the opaque i8* handle, and the paired
// retain/release discipline (OpTaskRetain, one release per await) is what keeps
// an aliased handle from being freed twice. Rewriting the group into the two
// spellings that already exist gets all of that for free and cannot drift from
// it; hand-rolling OpRun/OpAwait in the lowerer would be a second, untested
// implementation of the same protocol.
//
// WHY THIS RUNS AFTER THE CHECKER
// -------------------------------
// The checker runs on the surface AST, before ASTToHIR. A synthesized binding
// therefore never has to satisfy a declaration check.
//
// SCOPE, DELIBERATELY NARROW
// --------------------------
// A statement that is itself a bare `co` is spawnable. Anything else in the
// group is a BARRIER: every pending spawn is awaited before it, because a
// barrier's reads and writes are not analyzed. This costs concurrency in a
// mixed group and buys an ordering guarantee that does not depend on the
// analysis being complete. A group with no spawnable statement is left
// byte-for-byte alone, which is what keeps this pass a no-op for the existing
// corpus.
//
// A BARRIER THAT IS A BARE BLOCK is scanned recursively: its own `co`
// statements are rewritten in place (so argument evaluation stays where the
// source put it), but their awaits are moved to the GROUP tail. Control-flow
// barriers (loops, ifs, matches, channel receive loops) are entered too, but
// their handles cannot live in a static group-tail slot — the arm may run many
// times or not at all. Those `co`s are rewritten to `run` + `__join-push` and
// reaped by `__join-leave` at the group tail (the runtime join-set; see
// desugarFlowArmSpawns). The only remaining sequential case is a `co` inside a
// nested function literal, where no enclosing group schedule applies.
//
// Kill switch: NOLANG_ASYNC_GROUP=0 disables the pass (diagnostic only, like
// the other NOLANG_* switches). Desugaring happens before any MIR is built, so
// the flag is usable for A/B comparing a program against itself.

// DesugarAsyncGroups rewrites every `awy { ... }` block (KBlock carrying
// FlagAwaitGroup) that contains at least one spawnable statement into explicit
// KRun / KAwait bindings. It returns the number of groups rewritten; 0 means
// the package was left untouched.
func DesugarAsyncGroups(p *Package) int {
	if p == nil || os.Getenv("NOLANG_ASYNC_GROUP") == "0" {
		return 0
	}
	// Collect FIRST, and only over the nodes that exist now: the rewrite
	// appends to p.Nodes, and a loop over a growing slice would walk its own
	// output. Coroutine groups are found by the explicit FlagAwaitGroup mark
	// emitted by `awy { ... }` (the old "parent is a block" heuristic is gone:
	// a plain bare block is no longer a coroutine group).
	var bare []int32
	for i := 1; i < len(p.Nodes); i++ {
		if p.Nodes[i].Kind == KBlock && p.Nodes[i].Flags&FlagAwaitGroup != 0 {
			bare = append(bare, int32(i))
		}
	}
	if len(bare) == 0 {
		return 0
	}
	g := &groupRewriter{p: p, taken: map[string]bool{}}
	g.collectNames()
	n := 0
	// Innermost first: an outer group rebuilds its child list, which relinks
	// (but never re-ids) the nested groups, so processing the deeper ones
	// afterwards still sees their own untouched children.
	for i := len(bare) - 1; i >= 0; i-- {
		if g.group(bare[i]) {
			n++
		}
	}
	return n
}

// groupRewriter carries the state one package rewrite needs: the arena being
// extended and the set of identifier spellings already in use, so a synthesized
// handle binding cannot shadow a real one.
type groupRewriter struct {
	p     *Package
	taken map[string]bool
	seq   int
}

// collectNames records every identifier the package mentions. A synthesized
// name must avoid all of them, not just the ones in the enclosing function:
// hir2mir resolves a bare name through module-scope fallbacks, so a collision
// anywhere is a collision.
func (g *groupRewriter) collectNames() {
	// EVERY node's primary string, not just the binding kinds. A name only has
	// to be absent from the namespace lowering resolves against, and that
	// namespace is built from more than KIdent/KLet: parser/lowering.go injects
	// its own bindings (`__cap_1`, `__opt_1`, `__match_subj_..`) before this
	// pass runs. Over-collecting can only push the synthesized name to a
	// different suffix, which costs nothing.
	for i := 1; i < len(g.p.Nodes); i++ {
		if s := g.p.Str(g.p.Nodes[i].S); s != "" {
			g.taken[s] = true
		}
	}
}

func (g *groupRewriter) freshName() string {
	for {
		name := "__ag" + itoa(g.seq)
		g.seq++
		if !g.taken[name] {
			g.taken[name] = true
			return name
		}
	}
}

// spawnRef is one rewritten `co` site. block is the KBlock whose child list
// contains stmt; keeping it explicit lets the same machinery rewrite both the
// group's own statements and `co`s found inside nested bare blocks.
type spawnRef struct {
	block int32
	stmt  int32
	call  int32
	// deferred marks a `co` inside a CONTROL-FLOW arm (loop/if/match). Its
	// handle is rebound per iteration / absent on untaken paths, so it cannot
	// be awaited from a static group-tail slot. Instead it is recorded in the
	// runtime join-set (__join-push) and reaped by __join-leave at the tail.
	deferred bool
}

// GroupStep is one statement's entry in a group's schedule.
type GroupStep struct {
	// Spawn marks a `co` statement at the group's own level.
	Spawn bool
	// Handle is the index into GroupPlan.Handles for a direct spawn, or -1.
	Handle int
	// Flush lists, by handle index, the spawns that must be awaited BEFORE
	// a barrier statement runs. Empty means "nothing in flight".
	Flush []int
	// Nested lists the handle indices of `co`s found inside a barrier that
	// is a bare block. Their KRun bindings stay inside that block, but their
	// awaits belong to the group tail — that is what turns nested straight-line
	// spawns from serial into overlapping.
	Nested []int
}

// GroupPlan is the spawn/await schedule of one `awy { ... }` group: one step
// per statement, in source order, plus the spawns still in flight when the
// group ends (awaited after the last statement).
//
// WHY A SCHEDULE AT ALL
// ---------------------
// AsyncGroupPlan computes which spawns overlap as a separate value that the
// rewrite then applies. Keeping "who is in flight" in one place makes the
// invariant inspectable (and assertable in tests) instead of buried in the
// node-building loop.
type GroupPlan struct {
	Steps   []GroupStep
	Tail    []int      // handle indices still in flight at the group end
	Handles []spawnRef // every spawn site, in source order
}

// AsyncGroupPlan computes the schedule of an `awy { ... }` group WITHOUT
// changing anything. ok is false when no statement in the block is spawnable — the
// caller must then leave the block alone, which is what keeps the whole
// feature a no-op for code that was never written against it.
func (p *Package) AsyncGroupPlan(blockID int32) (plan GroupPlan, ok bool) {
	stmts := p.Children(blockID)
	if len(stmts) == 0 {
		return plan, false
	}
	g := &groupRewriter{p: p}
	plan.Steps = make([]GroupStep, len(stmts))
	var pending []int // handle indices of spawns still in flight
	register := func(block, stmt, call int32) int {
		idx := len(plan.Handles)
		plan.Handles = append(plan.Handles, spawnRef{block: block, stmt: stmt, call: call})
		return idx
	}
	for i := range plan.Steps {
		plan.Steps[i].Handle = -1
	}
	for i, sid := range stmts {
		s := p.Node(sid)
		call := NoID
		if s != nil {
			call = g.spawnable(s)
		}
		if call == NoID {
			// Barrier. A `co` result is impossible, so there is no data
			// dependency to compute — everything already in flight must land
			// before it runs. Nested bare blocks are special: their `co`s are
			// spawns of THIS group, so register them only after the flush.
			plan.Steps[i].Flush = append([]int(nil), pending...)
			pending = nil
			var nested []spawnRef
			p.collectBareNestedSpawns(sid, &nested)
			for _, ns := range nested {
				idx := register(ns.block, ns.stmt, ns.call)
				plan.Steps[i].Nested = append(plan.Steps[i].Nested, idx)
				pending = append(pending, idx)
				ok = true
			}
			continue
		}
		h := register(blockID, sid, call)
		plan.Steps[i] = GroupStep{Spawn: true, Handle: h}
		pending = append(pending, h)
		ok = true
	}
	plan.Tail = append([]int(nil), pending...)
	return plan, ok
}

// collectBareNestedSpawns finds `co` statements inside a barrier that is a
// bare block, descending through further bare blocks. It stops at any other
// node kind: a loop/if/match arm cannot use a STATIC slot (it may run many
// times or not at all), so those are collected separately by
// collectFlowArmSpawns and routed through the runtime join-set. An inner
// `awy` group is also skipped — it was desugared first (innermost-first in
// DesugarAsyncGroups) and owns its awaits.
func (p *Package) collectBareNestedSpawns(stmtID int32, out *[]spawnRef) {
	n := p.Node(stmtID)
	if n == nil || n.Kind != KBlock || n.Flags&FlagAwaitGroup != 0 {
		return
	}
	g := &groupRewriter{p: p}
	for _, sid := range p.Children(stmtID) {
		s := p.Node(sid)
		if s == nil {
			continue
		}
		if s.Kind == KExprStmt {
			if call := g.spawnable(s); call != NoID {
				*out = append(*out, spawnRef{block: stmtID, stmt: sid, call: call})
				continue
			}
		}
		if s.Kind == KBlock {
			p.collectBareNestedSpawns(sid, out)
		}
	}
}

// controlFlowKind reports whether a node is a control-flow construct whose
// arms execute a variable number of times (or not at all). A `co` inside one
// cannot be awaited from a static group-tail slot.
func controlFlowKind(n *Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case KFor, KIf, KTypeMatch, KChanIter:
		return true
	}
	return false
}

// collectFlowArmSpawns finds every bare `co` statement nested inside a
// control-flow arm of `blockID` (loops, ifs, type matches, channel receive
// loops) and returns it as a deferred spawnRef. It does NOT descend into a
// nested `awy` group — that group was desugared first and owns its own awaits.
// Direct children of `blockID` and `co`s in plain nested bare blocks are NOT
// collected here: the existing GroupPlan / collectBareNestedSpawns handle
// those with static slots.
func (p *Package) collectFlowArmSpawns(blockID int32) []spawnRef {
	g := &groupRewriter{p: p}
	var out []spawnRef
	var walk func(id int32, inFlow bool)
	walk = func(id int32, inFlow bool) {
		n := p.Node(id)
		if n == nil {
			return
		}
		if n.Kind == KBlock && n.Flags&FlagAwaitGroup != 0 && id != blockID {
			return // nested group owns its own awaits
		}
		if n.Kind == KFuncDef || n.Kind == KFuncLit {
			return
		}
		if inFlow && n.Kind == KExprStmt {
			if call := g.spawnable(n); call != NoID {
				// The containing block id is recovered by the caller's walk;
				// store the immediate parent so group() can relink it.
				out = append(out, spawnRef{stmt: id, call: call, deferred: true})
				return
			}
		}
		for _, c := range p.Children(id) {
			nextInFlow := inFlow || controlFlowKind(n)
			walk(c, nextInFlow)
		}
	}
	// Walk EVERY direct child with inFlow=false. A control-flow node may be
	// wrapped in a KExprStmt (that is how `if` appears in HIR), so filtering
	// by controlFlowKind(child) here would miss it. walk() flips inFlow on as
	// soon as it reaches the control-flow node itself, and never collects a
	// `co` that is a direct group statement (inFlow=false) — those are handled
	// by AsyncGroupPlan with static slots.
	for _, sid := range p.Children(blockID) {
		walk(sid, false)
	}
	return out
}

// findParentBlock returns the id of the nearest KBlock ancestor of `stmt` that
// is inside `root`. HIR wraps control-flow nodes in KExprStmt and wraps their
// bodies in KSlot, so the descent must follow EVERY child edge — an earlier
// version that only descended through KBlock/controlFlowKind missed every `co`
// inside an `if` arm and returned NoID, which silently skipped the rewrite
// while still emitting __join-enter/__join-leave around the group.
func (p *Package) findParentBlock(root, stmt int32) int32 {
	var find func(id int32) int32
	find = func(id int32) int32 {
		n := p.Node(id)
		isBlock := n != nil && n.Kind == KBlock
		for _, c := range p.Children(id) {
			if c == stmt && isBlock {
				return id
			}
			if got := find(c); got != NoID {
				return got
			}
		}
		return NoID
	}
	return find(root)
}

// globalCall builds a zero-arg global builtin call statement: `name()`.
func (g *groupRewriter) globalCall(name string, at int32) int32 {
	line, col := int32(0), int32(0)
	if n := g.p.Node(at); n != nil {
		line, col = n.Line, n.Col
	}
	ident := g.p.add(Node{Kind: KIdent, S: g.p.intern(name), Line: line, Col: col})
	fnSlot := g.p.add(Node{Kind: KSlot, S: g.p.intern("fn"), First: ident, Line: line, Col: col})
	call := g.p.add(Node{Kind: KCall, First: fnSlot, Line: line, Col: col})
	return g.p.add(Node{Kind: KExprStmt, First: call, Line: line, Col: col})
}

// callWithHandle builds `<name>(<handleIdent>)` as a statement.
func (g *groupRewriter) callWithHandle(name string, handle int32, at int32) int32 {
	line, col := int32(0), int32(0)
	if n := g.p.Node(at); n != nil {
		line, col = n.Line, n.Col
	}
	ident := g.p.add(Node{Kind: KIdent, S: g.p.intern(name), Line: line, Col: col})
	fnSlot := g.p.add(Node{Kind: KSlot, S: g.p.intern("fn"), First: ident, Line: line, Col: col})
	hName := g.p.Str(g.p.Nodes[handle].S)
	hIdent := g.p.add(Node{Kind: KIdent, S: g.p.intern(hName), Line: line, Col: col})
	argSlot := g.p.add(Node{Kind: KSlot, S: g.p.intern("arg"), First: hIdent, Line: line, Col: col})
	call := g.p.add(Node{Kind: KCall, First: g.p.link([]int32{fnSlot, argSlot}), Line: line, Col: col})
	return g.p.add(Node{Kind: KExprStmt, First: call, Line: line, Col: col})
}

// desugarFlowArmSpawns rewrites `co` statements inside control-flow arms of an
// awy group into `run` + __join-push, and wraps the group with __join-enter /
// __join-leave. Returns true when anything changed. It runs AFTER the static
// GroupPlan rewrite so the group's own children are already KRun/KLet/KAwait.
func (g *groupRewriter) desugarFlowArmSpawns(blockID int32) bool {
	spawns := g.p.collectFlowArmSpawns(blockID)
	if len(spawns) == 0 {
		return false
	}
	// Rewrite each deferred co in its own parent block: replace the co
	// statement with `__agN = run f-async()` followed by `__join-push(__agN)`.
	for _, ref := range spawns {
		parent := g.p.findParentBlock(blockID, ref.stmt)
		if parent == NoID {
			continue
		}
		name := g.freshName()
		runBinding := g.spawn(ref.stmt, ref.call, name)
		push := g.callWithHandle("__join-push", runBinding, ref.stmt)
		kids := g.p.Children(parent)
		replaced := make([]int32, 0, len(kids)+1)
		for _, kid := range kids {
			if kid == ref.stmt {
				replaced = append(replaced, runBinding, push)
			} else {
				replaced = append(replaced, kid)
			}
		}
		g.p.Nodes[parent].First = g.p.link(replaced)
	}
	// Wrap the group: enter at the head, leave at the tail.
	stmts := g.p.Children(blockID)
	if len(stmts) == 0 {
		return false
	}
	enter := g.globalCall("__join-enter", stmts[0])
	leave := g.globalCall("__join-leave", stmts[len(stmts)-1])
	out := make([]int32, 0, len(stmts)+2)
	out = append(out, enter)
	out = append(out, stmts...)
	out = append(out, leave)
	g.p.Nodes[blockID].First = g.p.link(out)
	return true
}

// group applies a plan, rewriting one `awy { ... }` block into KRun / KAwait bindings.
func (g *groupRewriter) group(blockID int32) bool {
	plan, ok := g.p.AsyncGroupPlan(blockID)
	if !ok {
		// Even with no DIRECT spawnable statement, control-flow arms may still
		// hold `co`s; handle those through the join-set.
		return g.desugarFlowArmSpawns(blockID)
	}
	stmts := g.p.Children(blockID)
	out := make([]int32, 0, len(stmts)+len(plan.Handles)+2)
	// handle[i] is the KLet that holds spawn i's handle, needed when a later
	// step (or the tail) has to await it.
	handle := make(map[int]int32, len(plan.Handles))
	for i, ref := range plan.Handles {
		name := g.freshName()
		handle[i] = g.spawn(ref.stmt, ref.call, name)
		if ref.block == blockID {
			// A direct child of the group: its binding is emitted at the
			// statement's own position in the loop below, NOT here — moving
			// it would break the barrier that sits between two `co`s.
			continue
		}
		// Nested bare block: leave the binding where the source put it, but
		// replace the original `co` statement in that block's child list.
		kids := g.p.Children(ref.block)
		replaced := make([]int32, 0, len(kids))
		for _, kid := range kids {
			if kid == ref.stmt {
				replaced = append(replaced, handle[i])
			} else {
				replaced = append(replaced, kid)
			}
		}
		g.p.Nodes[ref.block].First = g.p.link(replaced)
	}
	for i, sid := range stmts {
		st := plan.Steps[i]
		for _, j := range st.Flush {
			out = append(out, g.awaitAt(handle[j], sid))
		}
		if st.Spawn {
			out = append(out, handle[st.Handle])
		} else {
			out = append(out, sid)
		}
	}
	for _, j := range plan.Tail {
		out = append(out, g.awaitAt(handle[j], stmts[len(stmts)-1]))
	}
	g.p.Nodes[blockID].First = g.p.link(out)
	// Control-flow-arm `co`s (loops/if/match) cannot use static slots; route
	// them through the runtime join-set. This re-reads the group's (now
	// rewritten) children and wraps them with enter/leave if any are found.
	g.desugarFlowArmSpawns(blockID)
	return true
}

// spawn builds `__agN = run <call>`, reusing the original call node so argument
// lowering, annotations and source positions are unchanged.
func (g *groupRewriter) spawn(stmtID, call int32, name string) int32 {
	s := g.p.Node(stmtID)
	if s == nil {
		return NoID
	}
	run := g.p.add(Node{Kind: KRun, First: call, Line: s.Line, Col: s.Col})
	// The binding holds an opaque handle, never a result: `co` produces no
	// value, so no result type is carried anywhere.
	return g.p.add(Node{Kind: KLet, S: g.p.intern(name), First: run, Line: s.Line, Col: s.Col})
}

// awaitAt builds `awy <handle>` as a statement, positioned at the statement
// `at` so diagnostics point at the barrier/group end that forced the wait.
func (g *groupRewriter) awaitAt(handleID int32, at int32) int32 {
	src := g.p.Node(at)
	line, col := int32(0), int32(0)
	if src != nil {
		line, col = src.Line, src.Col
	}
	if handleID == NoID {
		return NoID
	}
	name := g.p.Str(g.p.Nodes[handleID].S)
	h := g.p.add(Node{Kind: KIdent, S: g.p.intern(name), Line: line, Col: col})
	awy := g.p.add(Node{Kind: KAwait, First: h, Line: line, Col: col})
	return g.p.add(Node{Kind: KExprStmt, First: awy, Line: line, Col: col})
}

// spawnable reports whether stmt is a bare `co <call>` statement. It returns
// the call node, or NoID for a barrier. Only KExprStmt qualifies: `co` is not a
// value, so it can never appear as the right-hand side of a binding.
func (g *groupRewriter) spawnable(stmt *Node) int32 {
	if stmt == nil || stmt.Kind != KExprStmt {
		return NoID
	}
	call := stmt.First
	cn := g.p.Node(call)
	if cn == nil || cn.Kind != KCall {
		return NoID
	}
	if !g.isAsyncCall(cn) {
		return NoID
	}
	// A nested async call in an argument position would lower to a handle being
	// passed as an argument — a shape `run` does not cover. Refuse rather than
	// emit it.
	if g.hasNestedAsyncCall(g.p.Children(call)) {
		return NoID
	}
	return call
}

// isAsyncCall reports whether the callee is spelled `-async`, the SAME rule
// hir2mir uses to decide a bare call lowers to OpRun. Two layers, one
// predicate: if they ever disagreed, a call would be spawned by one and awaited
// by the other.
func (g *groupRewriter) isAsyncCall(call *Node) bool {
	for _, c := range g.p.Children(call.Id) {
		slot := g.p.Node(c)
		if slot == nil || slot.Kind != KSlot || g.p.Str(slot.S) != "fn" {
			continue
		}
		fn := g.p.Node(slot.First)
		if fn == nil || fn.Kind != KIdent {
			continue
		}
		return strings.HasSuffix(g.p.Str(fn.S), "-async")
	}
	return false
}

func (g *groupRewriter) hasNestedAsyncCall(kids []int32) bool {
	for _, c := range kids {
		slot := g.p.Node(c)
		if slot == nil || slot.Kind != KSlot || g.p.Str(slot.S) == "fn" {
			continue
		}
		n := g.p.Node(slot.First)
		if n == nil {
			continue
		}
		if n.Kind == KCall && g.isAsyncCall(n) {
			return true
		}
		if g.hasNestedAsyncCall(g.p.Children(n.Id)) {
			return true
		}
	}
	return false
}

// add appends a node to the arena. A Package is immutable to its readers but a
// rewrite pass owns it for the duration of DesugarAsyncGroups.
func (p *Package) add(n Node) int32 {
	id := int32(len(p.Nodes))
	n.Id = id
	p.Nodes = append(p.Nodes, n)
	return id
}

// intern returns the id of s in the string table, appending when unseen. The
// pass runs a handful of times per package, so the linear scan is cheaper than
// rebuilding an index the Package does not carry.
func (p *Package) intern(s string) int32 {
	if s == "" {
		return NoID
	}
	for i, x := range p.Strings {
		if x == s {
			return int32(i)
		}
	}
	id := int32(len(p.Strings))
	p.Strings = append(p.Strings, s)
	return id
}

// link ties ids into a sibling chain and returns the head, dropping NoID.
func (p *Package) link(ids []int32) int32 {
	head := NoID
	var prev int32 = NoID
	for _, id := range ids {
		if id == NoID {
			continue
		}
		if prev == NoID {
			head = id
		} else {
			p.Nodes[prev].Next = id
		}
		prev = id
	}
	if prev != NoID {
		p.Nodes[prev].Next = NoID
	}
	return head
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ---------------------------------------------------------------------------
// Colorless async (`co`): monomorphization
//
// `co worker(1)` (inside an `awy { ... }` group) is sugar for spawning the
// compiler-generated `worker-async` variant of `worker`. The user writes the
// uncolored name `worker`; the `-async` suffix is purely internal. This pass:
//
//   1. clones every `co`-targeted (or transitively async) function `F` into a
//      fresh `F-async` definition (identical body, renamed), and
//   2. rewrites each `co <call>` KCall's callee from `F` to `F-async`.
//
// The coroutine-group pass (which runs AFTER this one) then treats the
// rewritten bare `-async` calls as spawnable, so `co` inside an `awy { }` group
// overlaps with its siblings. `co` is only legal inside such a group (parser
// enforced), so there is no group-external inline-await path. The async coloring
// is propagated upward: any function that calls an async-colored function also
// gets an `-async` variant, so a `co` deep in the call tree lifts every ancestor
// that could spawn it.
//
// Kill switch: NOLANG_ASYNC_GO=0 disables the pass (diagnostic only). The pass
// is a no-op whenever pkg.GoSpawns is empty, i.e. for programs that never use
// `co` — which keeps the corpus guard untouched.
// ---------------------------------------------------------------------------

// MonomorphizeGo clones `-async` variants for every function in the async
// call graph and rewrites the `go` spawn sites to target them. It returns the
// number of functions cloned (0 means nothing changed).
func MonomorphizeGo(p *Package) int {
	if p == nil || len(p.GoSpawns) == 0 || os.Getenv("NOLANG_ASYNC_GO") == "0" {
		return 0
	}
	// funcByName maps a function name to its KFuncDef id (top-level only).
	funcByName := map[string]int32{}
	for _, id := range p.Top {
		n := p.Node(id)
		if n != nil && n.Kind == KFuncDef {
			funcByName[p.Str(n.S)] = id
		}
	}
	// Base async-colored set: the direct callees of `go` spawns. These MUST
	// have `-async` variants because they are launched as tasks.
	asyncColored := map[string]bool{}
	for _, callID := range p.GoSpawns {
		if f := calleeName(p, callID); f != "" {
			asyncColored[f] = true
		}
	}
	// Fixpoint: any function that calls an async-colored function is itself
	// async-colored ("lower async → upper also gets -async"). This lifts the
	// coloring up every caller to the program entry.
	for changed := true; changed; {
		changed = false
		for name, fid := range funcByName {
			if asyncColored[name] {
				continue
			}
			for _, callee := range calleesOf(p, fid) {
				if asyncColored[callee] {
					asyncColored[name] = true
					changed = true
					break
				}
			}
		}
	}
	// Clone each async-colored function that lacks an `-async` variant.
	//
	// ⚠️ asyncColored is a MAP, and the clone order becomes the order the new
	// KFuncDefs are appended to p.Top — i.e. the order functions appear in the
	// emitted HIR. Iterating the map directly makes the compiler's output
	// non-deterministic: the same source compiled twice in the same process
	// produced a different function order (measured on tests/async.no, 18
	// functions, differing from index 11 onward). That breaks any golden /
	// hashed comparison of HIR and makes a build non-reproducible. Sorting the
	// names first pins the order.
	//
	// The fixpoint loop above iterates funcByName unsorted too, but that one is
	// safe: it only propagates booleans to a fixed point, so the final SET is
	// order-independent even though the traversal is not.
	n := 0
	names := make([]string, 0, len(asyncColored))
	for name := range asyncColored {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := funcByName[name+"-async"]; ok {
			continue
		}
		fid, ok := funcByName[name]
		if !ok {
			continue
		}
		newID, oldToNew := p.cloneSubtree(fid)
		p.Nodes[newID].S = p.intern(name + "-async")
		p.Top = append(p.Top, newID)
		funcByName[name+"-async"] = newID
		// The clone's body may itself contain `go` calls (to other async
		// functions). Record their new node ids as go-spawns so the rewrite
		// below retargets them too. Sorted for the same reason: oldToNew is a
		// map keyed by old node id, and this appends to p.GoSpawns, which
		// later loops iterate.
		goSpawns := make([]int32, 0, len(oldToNew))
		for oldID := range oldToNew {
			if goSpawnIndex(p, oldID) >= 0 {
				goSpawns = append(goSpawns, oldID)
			}
		}
		sort.Slice(goSpawns, func(a, b int) bool { return goSpawns[a] < goSpawns[b] })
		for _, oldID := range goSpawns {
			p.GoSpawns = append(p.GoSpawns, oldToNew[oldID])
		}
		n++
	}
	// Rewrite every `go` spawn's callee from the uncolored name to its
	// `-async` monomorph.
	for _, callID := range p.GoSpawns {
		f := calleeName(p, callID)
		if f != "" && asyncColored[f] {
			setCallee(p, callID, f+"-async")
		}
	}
	return n
}

// calleeName returns the function name a KCall invokes, or "" if it is not a
// direct call. Mirrors groupRewriter.isAsyncCall's callee extraction.
func calleeName(p *Package, callID int32) string {
	call := p.Node(callID)
	if call == nil || call.Kind != KCall {
		return ""
	}
	for c := call.First; c != NoID; c = p.Nodes[c].Next {
		slot := p.Node(c)
		if slot == nil || slot.Kind != KSlot || p.Str(slot.S) != "fn" {
			continue
		}
		if ident := p.Node(slot.First); ident != nil && ident.Kind == KIdent {
			return p.Str(ident.S)
		}
	}
	return ""
}

// setCallee rewrites the callee of a KCall node to newName.
func setCallee(p *Package, callID int32, newName string) {
	call := p.Node(callID)
	if call == nil || call.Kind != KCall {
		return
	}
	for c := call.First; c != NoID; c = p.Nodes[c].Next {
		slot := p.Node(c)
		if slot == nil || slot.Kind != KSlot || p.Str(slot.S) != "fn" {
			continue
		}
		if ident := p.Node(slot.First); ident != nil && ident.Kind == KIdent {
			ident.S = p.intern(newName)
		}
	}
}

// calleesOf returns the names of every function a KFuncDef calls (transitively
// through its body), stopping at nested KFuncDef boundaries so a nested
// function literal is treated as opaque.
func calleesOf(p *Package, fid int32) []string {
	var out []string
	seen := map[int32]bool{}
	var walk func(id int32)
	walk = func(id int32) {
		for c := id; c != NoID; c = p.Nodes[c].Next {
			if seen[c] {
				continue
			}
			seen[c] = true
			node := p.Node(c)
			if node == nil {
				continue
			}
			if node.Kind == KFuncDef {
				// Do not descend into a nested function definition.
				continue
			}
			if node.Kind == KCall {
				if name := calleeName(p, c); name != "" {
					out = append(out, name)
				}
			}
			walk(node.First)
		}
	}
	walk(p.Node(fid).First)
	return out
}

// goSpawnIndex returns the index of callID in p.GoSpawns, or -1.
func goSpawnIndex(p *Package, callID int32) int {
	for i, id := range p.GoSpawns {
		if id == callID {
			return i
		}
	}
	return -1
}

// cloneSubtree deep-copies the closed subtree rooted at root into fresh arena
// nodes and returns the new root id plus the old->new id map. The root's own
// Next link is NOT followed (so cloning a KFuncDef does not drag in sibling
// top-level nodes); every other node's Next (sibling) chain IS cloned, because
// it is part of the function body. Callees are referenced by name string, not
// node id, so the subtree is self-contained.
func (p *Package) cloneSubtree(root int32) (int32, map[int32]int32) {
	oldToNew := map[int32]int32{}
	var clone func(id int32, isRoot bool) int32
	clone = func(id int32, isRoot bool) int32 {
		if id == NoID {
			return NoID
		}
		if v, ok := oldToNew[id]; ok {
			return v
		}
		src := p.Node(id)
		newID := int32(len(p.Nodes))
		p.Nodes = append(p.Nodes, Node{Id: newID})
		oldToNew[id] = newID
		dst := &p.Nodes[newID]
		*dst = *src
		dst.Id = newID
		dst.First = clone(src.First, false)
		if isRoot {
			dst.Next = NoID
		} else {
			dst.Next = clone(src.Next, false)
		}
		return newID
	}
	return clone(root, true), oldToNew
}
