package mir

import (
	"strings"
	"testing"
)

// These tests pin the §4.2(b) "static gate" of NOLANG-OWNERSHIP-MODEL.md — the
// spawn-boundary SHARE that replaces the P0 deep copy when the caller provably
// never writes the argument after the spawn.
//
// WHAT THE CHANGE IS. `run f(x)` deep-copies every owned argument into the task's
// own buffer so the task sees the value AS OF SPAWN. When x is still live but the
// caller never writes it in place before the await, that copy is avoidable:
// insertDrops records x in mod.spawnArgRetains, and emitAsyncRun emits ONE
// @str_retain / @vec_retain instead of the copy. The caller keeps its binding AND
// its drop; the task's wrapper (w_free) releases the second reference; since
// @nolang_free frees only at refcount zero, the buffer dies exactly once.
//
// WHY THE ASSERTIONS ARE ON THE MIR SET AND THE IR, NOT ON STDOUT
// ---------------------------------------------------------------
// A share and a deep copy are OBSERVABLY IDENTICAL by construction — that is the
// whole point of the gate. So stdout cannot distinguish them. What can is:
//
//   - whether the value is in spawnArgRetains;
//   - whether the spawn site emitted @vec_retain / @str_retain instead of the
//     deep-clone call;
//   - whether the caller still has its OpDrop (a suppressed drop would leave the
//     retain with no second release to give back — a permanently pinned block).
//
// The NEGATIVE cases matter as much as the positive one: an "always share"
// implementation would pass the positive test and be the §6.3 snapshot violation
// (the task would observe the caller's a[0] = 99).

// retainVecSrc is the positive shape: `a` is READ after the spawn (a[1], a direct
// index read, which cannot write) and never written, so the buffer can be shared.
const retainVecSrc = `first-async = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

main = () {
    a []i64 = [1, 2, 3]
    t = run first-async(a)
    m = a[1]
    n = awy t
    print(n)
    print(m)
}
`

// retainVecWrittenSrc is the §4.2(b)② counterexample: the caller writes a[0] in
// place after the spawn, so the task must see the SPAWN-TIME value. Sharing here
// would print 99 instead of 1 — the silent semantic change the gate exists to
// prevent.
const retainVecWrittenSrc = `first-async = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

main = () {
    a []i64 = [1, 2, 3]
    t = run first-async(a)
    a[0] = 99
    n = awy t
    print(n)
}
`

// irContainsCall reports whether the IR text contains the given call token.
func irContains(ir, token string) bool { return strings.Contains(ir, token) }

// TestSpawnArgRetainTakenForLiveUnwrittenVec is the core assertion: a live,
// never-written spawn argument is SHARED — recorded in spawnArgRetains, the deep
// clone replaced by @vec_retain at the spawn site, and the caller's own drop
// KEPT (which is what balances the retain).
func TestSpawnArgRetainTakenForLiveUnwrittenVec(t *testing.T) {
	mod := lowerForTest(t, retainVecSrc)
	arg := runArg(t, mod, "main")
	if mod.spawnArgMoves[arg] {
		t.Errorf("value %d was MOVED; it is live after the spawn, so the share path should apply, not the move", arg)
	}
	if !mod.spawnArgRetains[arg] {
		t.Fatalf("value %d is not in spawnArgRetains; the share was not taken for a live, never-written argument", arg)
	}
	if n := dropsOf(mod, "main", arg); n != 1 {
		t.Errorf("main has %d OpDrop of the shared argument %d, want exactly 1 (the caller's release)", n, arg)
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := irFunc(ir, "_nolang_main")
	if !irContains(body, "call void @vec_retain(") {
		t.Errorf("the spawn site did not take a second reference; the buffer is shared but unreffed:\n%s", body)
	}
	if irContains(body, "call %vec @__nolang_vec_clone_") {
		t.Errorf("the spawn-boundary deep copy is still present for a shared argument:\n%s", body)
	}
}

// TestSpawnArgRetainNotTakenWhenWritten is the load-bearing NEGATIVE: an in-place
// write after the spawn must keep the deep copy, or the task observes the write
// and the snapshot guarantee is broken.
func TestSpawnArgRetainNotTakenWhenWritten(t *testing.T) {
	mod := lowerForTest(t, retainVecWrittenSrc)
	arg := runArg(t, mod, "main")
	if mod.spawnArgRetains[arg] {
		t.Errorf("value %d was shared even though the caller writes a[0] in place after the spawn", arg)
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := irFunc(ir, "_nolang_main")
	if irContains(body, "call void @vec_retain(") {
		t.Errorf("the spawn site took a second reference for an argument that is written in place:\n%s", body)
	}
	if !irContains(body, "call %vec @__nolang_vec_clone_") {
		t.Errorf("the spawn-boundary deep copy is missing for an argument written in place:\n%s", body)
	}
}

// TestSpawnArgRetainNotTakenForOwnedElements pins the class boundary that makes
// the single outer retain sufficient: for `[]str` the caller's drop is the DEEP
// free, which also releases every element buffer, so one outer retain would not
// cover them. The shape is otherwise identical to the positive one (a direct
// index read, no write), so only the element class can explain the refusal.
func TestSpawnArgRetainNotTakenForOwnedElements(t *testing.T) {
	mod := lowerForTest(t, `vecstr-async = (v []str) (r i64) {
    #{index-out=zero}
    r = v[0].len()
}

main = () {
    a []str = ['aaa', 'bbb']
    t = run vecstr-async(a)
    s = a[0]
    m = awy t
    print(m)
    print(s)
}
`)
	arg := runArg(t, mod, "main")
	if mod.spawnArgRetains[arg] {
		t.Errorf("value %d ([]str) was shared; the caller's deep free would free an element the task still reads", arg)
	}
}

// TestSpawnArgRetainNotTakenForStruct pins the second class boundary: a struct's
// owned leaves need a recursive retain mirroring emitPtrFieldsClone, which does
// not exist. Refused rather than approximated.
func TestSpawnArgRetainNotTakenForStruct(t *testing.T) {
	mod := lowerForTest(t, `holder { s str }

holder-async = (h holder) (r str) {
    r = h.s
}

main = () {
    h holder = holder { s: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' }
    t = run holder-async(h)
    c = h.s
    v = awy t
    print(v)
    print(c)
}
`)
	arg := runArg(t, mod, "main")
	if mod.spawnArgRetains[arg] {
		t.Errorf("value %d (struct) was shared; the recursive retain it needs does not exist", arg)
	}
}

// TestSpawnArgRetainNotTakenWhenDead pins the move/retain PRECEDENCE: a dead
// source is MOVED (zero-copy ownership transfer), never merely retained. Without
// this, "always retain" would also satisfy the positive test while leaking the
// reference the move would have consumed.
func TestSpawnArgRetainNotTakenWhenDead(t *testing.T) {
	mod := lowerForTest(t, `first-async = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

main = () {
    a []i64 = [1, 2, 3]
    t = run first-async(a)
    n = awy t
    print(n)
}
`)
	arg := runArg(t, mod, "main")
	if !mod.spawnArgMoves[arg] {
		t.Errorf("value %d is dead after the spawn and should have been moved", arg)
	}
	if mod.spawnArgRetains[arg] {
		t.Errorf("value %d is both moved and shared; the move must take precedence", arg)
	}
}

// TestSpawnArgRetainNotTakenForCallUse pins the CONSERVATIVE treatment of calls:
// a call may mutate the buffer (a.push(1) lowers to OpCall with the receiver as
// Args[0]) and its effect is not visible here, so ANY call use keeps the deep
// copy — even one whose callee only reads. `peek` is deliberately read-only, so
// this asserts the conservatism rather than just "mutations are caught".
//
// NOTE `print(a)` is NOT a call: a container print lowers to an inline
// len/index loop, which is read-only and therefore DOES admit the share. The
// gate keys on the ops, not on the source spelling.
func TestSpawnArgRetainNotTakenForCallUse(t *testing.T) {
	mod := lowerForTest(t, `peek = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

first-async = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

main = () {
    a []i64 = [1, 2, 3]
    t = run first-async(a)
    p = peek(a)
    n = awy t
    print(n)
    print(p)
}
`)
	arg := runArg(t, mod, "main")
	if mod.spawnArgRetains[arg] {
		t.Errorf("value %d was shared across a call use; a call is not provably write-free", arg)
	}
}

// TestSpawnArgRetainTakenForContainerPrint is the mirror of the test above and
// pins that the conservatism is about OPS, not about source spelling: printing a
// container lowers to a len/index loop (read-only), so the share IS taken.
func TestSpawnArgRetainTakenForContainerPrint(t *testing.T) {
	mod := lowerForTest(t, `first-async = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

main = () {
    a []i64 = [1, 2, 3]
    t = run first-async(a)
    print(a)
    n = awy t
    print(n)
}
`)
	arg := runArg(t, mod, "main")
	if !mod.spawnArgRetains[arg] {
		t.Errorf("value %d was not shared; a container print is a read-only len/index loop", arg)
	}
}

// TestSpawnArgRetainSafeContract pins the class predicate directly, so a change
// to the classification cannot silently widen what the analysis may share.
func TestSpawnArgRetainSafeContract(t *testing.T) {
	// The source must MENTION every element type whose TypeID is looked up, or
	// internType never ran on it and TypeMap yields NoType (which would answer
	// "owns no heap" for the wrong reason).
	mod := lowerForTest(t, `first-async = (v []i64) (r i64) {
    #{index-out=zero}
    r = v[0]
}

vecstr-async = (v []str) (r i64) {
    #{index-out=zero}
    r = v[0].len()
}

main = () {
    a []i64 = [1, 2, 3]
    b []str = ['x', 'y']
    t1 = run first-async(a)
    t2 = run vecstr-async(b)
    m = a[1]
    n = awy t1
    print(n)
    print(m)
    print(awy t2)
}
`)
	i64t := mod.TypeMap["i64"]
	strt := mod.TypeMap["str"]
	veci64 := mod.TypeMap["[]i64"]
	if i64t == NoType || strt == NoType || veci64 == NoType {
		t.Fatalf("element types not interned: i64=%d str=%d []i64=%d", i64t, strt, veci64)
	}
	for _, tc := range []struct {
		name string
		kind string
		elem TypeID
		want bool
	}{
		{"str", "str", NoType, true},
		{"[]i64 (trivial element)", "vec", i64t, true},
		{"[]str (element owns heap)", "vec", strt, false},
		{"nested []i64 element (owns heap)", "vec", veci64, false},
		{"struct", "struct", NoType, false},
		{"unclassified", "", NoType, false},
	} {
		if got := mod.spawnArgRetainSafe(tc.kind, tc.elem); got != tc.want {
			t.Errorf("spawnArgRetainSafe(%q, %s) = %v, want %v", tc.kind, tc.name, got, tc.want)
		}
	}
}

// TestSpawnArgUseWritesIsAWhitelist pins the two properties that keep the gate
// sound against a forgotten op: an op that uses the value and is NOT on the
// read-only list is reported as a write, and a move (which would transfer the
// caller's reference and unbalance the retain) is likewise a write.
func TestSpawnArgUseWritesIsAWhitelist(t *testing.T) {
	v := ValueID(7)
	inst := func(op Op, args ...ValueID) *Inst { return &Inst{Op: op, Args: args} }
	if spawnArgUseWrites(inst(OpIndex, v, 8), v) {
		t.Error("OpIndex must be read-only")
	}
	if !spawnArgUseWrites(inst(OpIndexStore, v, 8, 9), v) {
		t.Error("OpIndexStore writes through the container; it must be reported as a write")
	}
	if !spawnArgUseWrites(inst(OpCall, v), v) {
		t.Error("a call must be treated as a potential write")
	}
	if !spawnArgUseWrites(inst(OpMove, v), v) {
		t.Error("a move transfers the caller's reference; it must be treated as a write")
	}
	if spawnArgUseWrites(inst(OpIndex, 8, 9), v) {
		t.Error("an instruction that does not use v must not be reported")
	}
}
