package hir

import (
	"os"
	"strings"
)

// Coroutine groups (協程組) — HIR desugaring.
//
// SURFACE FORM
// ------------
// A bare block — a `{ ... }` that sits in STATEMENT position rather than being
// the body of an if/for/match arm/function — is a coroutine group:
//
//	{
//	    r1 = hello-async(1)
//	    r2 = hello-async(2)
//	}
//
// Every statement of the group whose right-hand side is a direct call to an
// `-async` callee is SPAWNED (`run`) rather than executed eagerly. The group
// then awaits whatever it spawned:
//
//   - a spawn whose result nothing else in the group needs is awaited at the
//     END of the group, so it overlaps with the group's other spawns;
//   - a spawn whose result a LATER statement of the group reads cannot overlap
//     with that statement, so it is awaited immediately before it ("退化成
//     await"): `r2 = hello-async(r1)` forces r1's await up front and the group
//     degrades to sequential execution.
//
// So the two examples above lower to
//
//	__ag0 = run hello-async(1)         __ag0 = run hello-async(1)
//	__ag1 = run hello-async(2)         r1    = awy __ag0
//	r1    = awy __ag0                  __ag1 = run hello-async(r1)
//	r2    = awy __ag1                  r2    = awy __ag1
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
// therefore never has to satisfy a declaration check — exactly like the `h1`
// in `h1 = run compute-async(10)`, which is undeclared in tests/async.no today.
//
// SCOPE, DELIBERATELY NARROW
// --------------------------
// Only a statement that is *itself* an `-async` call binding is spawnable.
// Anything else in the group (a plain call, a loop, an if, a nested block) is a
// BARRIER: every pending spawn is awaited before it, because a barrier's
// reads and writes are not analyzed. This costs concurrency in a mixed group
// and buys an ordering guarantee that does not depend on the analysis being
// complete. A group with no spawnable statement is left byte-for-byte alone,
// which is what keeps this pass a no-op for the existing corpus.
//
// Kill switch: NOLANG_ASYNC_GROUP=0 disables the pass (diagnostic only, like
// the other NOLANG_* switches). Desugaring happens before any MIR is built, so
// the flag is usable for A/B comparing a program against itself.

// DesugarAsyncGroups rewrites every bare block that contains at least one
// spawnable statement into explicit `run` / `awy` bindings. It returns the
// number of groups rewritten; 0 means the package was left untouched.
func DesugarAsyncGroups(p *Package) int {
	if p == nil || os.Getenv("NOLANG_ASYNC_GROUP") == "0" {
		return 0
	}
	// Collect FIRST, and only over the nodes that exist now: the rewrite
	// appends to p.Nodes, and a loop over a growing slice would walk its own
	// output. Bare blocks are found by PARENT, not by position: a fn body, an
	// if arm and a loop body all hang off a KSlot, so the only KBlock whose
	// parent is another KBlock is one written in statement position.
	var bare []int32
	for i := 1; i < len(p.Nodes); i++ {
		if p.Nodes[i].Kind != KBlock {
			continue
		}
		for c := p.Nodes[i].First; c != NoID; c = p.Nodes[c].Next {
			if p.Nodes[c].Kind == KBlock {
				bare = append(bare, c)
			}
		}
	}
	if len(bare) == 0 {
		return 0
	}
	g := &groupRewriter{p: p, taken: map[string]bool{}}
	g.collectNames()
	n := 0
	// Innermost first: an outer group rebuilds its child list, which relinks
	// (but never re-ids) the nested bare blocks, so processing the deeper ones
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

// GroupStep is one statement's entry in a group's schedule.
type GroupStep struct {
	// Spawn marks a statement that is a direct `-async` call binding: it is
	// launched (run) rather than executed eagerly.
	Spawn bool
	// Target is the binding the result is written back to; "" when the result
	// is discarded.
	Target string
	// Type is the declared type id of the original binding (NoID when none).
	// It travels with the AWAIT, not with the spawn: the handle is an opaque
	// i64 whatever the task returns.
	Type int32
	// Flush lists, by statement index, the spawns that must be awaited BEFORE
	// this statement runs. Empty means "nothing to wait for".
	Flush []int
}

// GroupPlan is the spawn/await schedule of one bare block: one step per
// statement, in source order, plus the spawns still in flight when the group
// ends (awaited after the last statement).
//
// WHY THE SCHEDULE IS A SEPARATE VALUE. Two backends need it. The MIR path
// rewrites the HIR into `run`/`awy` bindings; the JS backend emits promises
// and `await` from the same schedule. Deriving it twice would let the two
// disagree about which spawns overlap, and "one question, one implementation"
// is the rule this repo keeps re-learning. So: plan here, apply anywhere.
type GroupPlan struct {
	Steps []GroupStep
	Tail  []int
}

// AsyncGroupPlan computes the schedule of a bare block WITHOUT changing
// anything. ok is false when no statement in the block is spawnable — the
// caller must then leave the block alone, which is what keeps the whole
// feature a no-op for code that was never written against it.
func (p *Package) AsyncGroupPlan(blockID int32) (plan GroupPlan, ok bool) {
	stmts := p.Children(blockID)
	if len(stmts) == 0 {
		return plan, false
	}
	g := &groupRewriter{p: p}
	plan.Steps = make([]GroupStep, len(stmts))
	var pending []int // indices of spawned steps still in flight
	for i, sid := range stmts {
		s := p.Node(sid)
		call, target, typ := NoID, "", NoID
		if s != nil {
			call, target, typ = g.spawnable(s)
		}
		if call == NoID {
			// Barrier: its reads and writes are not analyzed, so everything
			// still in flight must land before it runs.
			plan.Steps[i].Flush = append([]int(nil), pending...)
			pending = nil
			continue
		}
		uses := g.reads(sid)
		var still, flush []int
		for _, j := range pending {
			// Two reasons a pending spawn cannot stay in flight: this
			// statement READS its result (a data dependency), or it rebinds
			// the same name (the pending value would be overwritten before it
			// ever landed).
			if (plan.Steps[j].Target != "" && uses[plan.Steps[j].Target]) ||
				(target != "" && plan.Steps[j].Target == target) {
				flush = append(flush, j)
			} else {
				still = append(still, j)
			}
		}
		pending = still
		// NOTE: assign the whole step at once — writing plan.Steps[i] again
		// later would discard the Flush collected above.
		plan.Steps[i] = GroupStep{Spawn: true, Target: target, Type: typ, Flush: flush}
		pending = append(pending, i)
		ok = true
	}
	plan.Tail = append([]int(nil), pending...)
	return plan, ok
}

// group applies a plan, rewriting one bare block into `run` / `awy` bindings.
func (g *groupRewriter) group(blockID int32) bool {
	plan, ok := g.p.AsyncGroupPlan(blockID)
	if !ok {
		return false
	}
	stmts := g.p.Children(blockID)
	out := make([]int32, 0, len(stmts)+len(plan.Steps)+2)
	// stepHandle[i] is the KLet that holds step i's handle, needed when a
	// later step (or the tail) has to await it.
	handle := make(map[int]int32, len(plan.Steps))
	for i, sid := range stmts {
		st := plan.Steps[i]
		for _, j := range st.Flush {
			out = append(out, g.awaitAt(handle[j], plan.Steps[j], sid))
		}
		if !st.Spawn {
			out = append(out, sid)
			continue
		}
		call, _, _ := g.spawnable(g.p.Node(sid))
		name := g.freshName()
		h := g.spawn(sid, call, name)
		handle[i] = h
		out = append(out, h)
	}
	for _, j := range plan.Tail {
		out = append(out, g.awaitAt(handle[j], plan.Steps[j], stmts[len(stmts)-1]))
	}
	g.p.Nodes[blockID].First = g.p.link(out)
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
	// The binding holds an opaque handle, so the original declared type must
	// NOT be carried over — `r1 i64` is the type of the RESULT, and it travels
	// with the await instead (see GroupStep.Type).
	return g.p.add(Node{Kind: KLet, S: g.p.intern(name), First: run, Line: s.Line, Col: s.Col})
}

// awaitAt builds `<target> = awy <handle>` for a spawned step, positioned at
// the statement `at` so diagnostics point at the code that forced the wait.
func (g *groupRewriter) awaitAt(handleID int32, st GroupStep, at int32) int32 {
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
	if st.Target == "" {
		return g.p.add(Node{Kind: KExprStmt, First: awy, Line: line, Col: col})
	}
	return g.p.add(Node{Kind: KLet, S: g.p.intern(st.Target), Type: st.Type, First: awy, Line: line, Col: col})
}

// spawnable reports whether stmt is a direct `-async` call binding. It returns
// the call node, the bound name ("" when the result is discarded) and the
// declared type of the binding.
func (g *groupRewriter) spawnable(stmt *Node) (call int32, target string, typ int32) {
	switch stmt.Kind {
	case KLet:
		// `r1 = hello-async(1)` lowers to KLet with the value as its child. A
		// declaration with no value has no child and is not a spawn.
		if stmt.First == NoID {
			return NoID, "", NoID
		}
		call, target, typ = stmt.First, g.p.Str(stmt.S), stmt.Type
	case KExprStmt:
		call, target, typ = stmt.First, "", NoID
	default:
		return NoID, "", NoID
	}
	cn := g.p.Node(call)
	if cn == nil || cn.Kind != KCall {
		return NoID, "", NoID
	}
	if !g.isAsyncCall(cn) {
		return NoID, "", NoID
	}
	// A nested async call in an argument position would lower to a handle being
	// passed as an argument — a shape `run` does not cover. Refuse rather than
	// emit it.
	if g.hasNestedAsyncCall(g.p.Children(call)) {
		return NoID, "", NoID
	}
	return call, target, typ
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

// reads returns every identifier the statement mentions, so a dependency on a
// pending spawn's result is recognized. Over-approximating is the safe
// direction: an extra dependency costs concurrency, a missed one compiles a
// program that reads a handle as if it were the result.
//
// The callee slot is excluded — the function's own name is not a read of a
// variable.
func (g *groupRewriter) reads(root int32) map[string]bool {
	uses := map[string]bool{}
	var walk func(id int32)
	walk = func(id int32) {
		n := g.p.Node(id)
		if n == nil {
			return
		}
		switch n.Kind {
		case KIdent:
			if s := g.p.Str(n.S); s != "" {
				uses[s] = true
			}
		case KSlot:
			if g.p.Str(n.S) == "fn" {
				return
			}
		}
		for c := n.First; c != NoID; c = g.p.Nodes[c].Next {
			walk(c)
		}
	}
	walk(root)
	return uses
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
