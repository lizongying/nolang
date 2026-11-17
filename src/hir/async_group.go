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
// An `awy { ... }` block is a coroutine group (the old statement-position bare
// block is no longer a group):
//
//	awy {
//	    r1 = co worker(1)
//	    r2 = co worker(2)
//	}
//
// Each `co <call>` in the group is SPAWNED (`run`) rather than executed eagerly;
// the `-async` callee is synthesized by MonomorphizeGo. `co` is only legal
// inside an `awy { ... }` group (enforced by the parser). The group then awaits
// whatever it spawned:
//
//   - a spawn whose result nothing else in the group needs is awaited at the
//     END of the group, so it overlaps with the group's other spawns;
//   - a spawn whose result a LATER statement of the group reads cannot overlap
//     with that statement, so it is awaited immediately before it ("退化成
//     await"): `r2 = hello-async(r1)` forces r1's await up front and the group
//     degrades to sequential execution.
//
// So the group above lowers to
//
//	__ag0 = run worker-async(1)
//	__ag1 = run worker-async(2)
//	r1    = awy __ag0
//	r2    = awy __ag1
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

// GroupPlan is the spawn/await schedule of one `awy { ... }` group: one step
// per statement, in source order, plus the spawns still in flight when the
// group ends (awaited after the last statement).
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

// group applies a plan, rewriting one `awy { ... }` block into KRun / KAwait bindings.
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

// ---------------------------------------------------------------------------
// Colorless async (`co`): monomorphization
//
// `co worker(1)` (inside an `awy { ... }` group) is sugar for spawning the
// compiler-generated `worker-async` variant of `worker` and reading its result.
// The user writes the uncolored name `worker`; the `-async` suffix is purely
// internal. This pass:
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
