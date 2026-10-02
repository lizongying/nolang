package mir

import (
	"strings"
	"testing"
)

// These tests pin the coroutine group (協程組) desugaring — a bare `{ ... }`
// block in statement position whose statements are `-async` calls. The
// observable is the ORDER of OpRun / OpAwait in the lowered function, because
// order is the entire point: "spawn everything, then await" is the concurrent
// case, and "spawn, await, spawn, await" is the degraded sequential case. A
// test that only checked that the program produced the right numbers could not
// tell them apart.
//
// NON-VACUITY: each case has a sibling that must produce the OTHER pattern.
// TestAsyncGroupSpawnsIndependentCalls (run run await await) is the control for
// TestAsyncGroupDegradesOnDependency (run await run await), and
// TestAsyncGroupFnBodyIsNotAGroup proves the pass does not fire on a function
// body — without it, a pass that rewrote every block would still pass the first
// two.

const asyncGroupWorkSrc = `work-async = (n i64) (r i64) {
    r = n
}
`

// asyncOpsIn returns the OpRun / OpAwait op names of `fn` in program order.
// Every other instruction is filtered out: the group pass does not move
// anything except the awaits, so those two are the whole signal.
func asyncOpsIn(t *testing.T, src, fn string) []string {
	t.Helper()
	mod := lowerForTest(t, src)
	fid, ok := mod.FuncByName[fn]
	if !ok {
		t.Fatalf("function %q not found; funcs=%v", fn, mod.FuncByName)
	}
	f := mod.Func(fid)
	var ops []string
	for _, bid := range f.Blocks {
		b := mod.Block(bid)
		if b == nil {
			continue
		}
		for _, iid := range b.Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			switch inst.Op {
			case OpRun, OpAwait:
				ops = append(ops, inst.Op.String())
			}
		}
	}
	return ops
}

func wantOps(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("async op order = [%s], want [%s]", strings.Join(got, " "), strings.Join(want, " "))
	}
}

// TestAsyncGroupSpawnsIndependentCalls: two spawns whose results nothing else
// in the group reads must BOTH be launched before either is awaited.
func TestAsyncGroupSpawnsIndependentCalls(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    r1 i64
    r2 i64
    {
        r1 = work-async(1)
        r2 = work-async(2)
    }
    print(r1)
    print(r2)
}
`, "main"), "run", "run", "await", "await")
}

// TestAsyncGroupDegradesOnDependency: `r2 = work-async(r1)` reads r1, so r1's
// task must land BEFORE r2 is launched. The group degrades to sequential
// execution — this is the whole reason the dependency analysis exists.
func TestAsyncGroupDegradesOnDependency(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    r1 i64
    r2 i64
    {
        r1 = work-async(1)
        r2 = work-async(r1)
    }
    print(r1)
    print(r2)
}
`, "main"), "run", "await", "run", "await")
}

// TestAsyncGroupTransitiveDependency: r3 reads r2 which reads r1, so the whole
// chain is sequential. A one-level-deep analysis would leave r1 in flight and
// hand r2 a handle where a value was expected.
func TestAsyncGroupTransitiveDependency(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    r1 i64
    r2 i64
    r3 i64
    {
        r1 = work-async(1)
        r2 = work-async(r1)
        r3 = work-async(r2)
    }
    print(r3)
}
`, "main"), "run", "await", "run", "await", "run", "await")
}

// TestAsyncGroupBarrierFlushes: a statement that is not an `-async` call is a
// barrier — its reads and writes are not analyzed, so everything in flight
// lands before it. This is what keeps `print(r1)` inside a group from reading a
// handle as if it were the value.
func TestAsyncGroupBarrierFlushes(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    r1 i64
    r2 i64
    {
        r1 = work-async(1)
        print(9)
        r2 = work-async(2)
    }
    print(r1)
    print(r2)
}
`, "main"), "run", "await", "run", "await")
}

// TestAsyncGroupBarrierSeesValueNotHandle: the barrier variant that matters —
// a read of a group result INSIDE the group. Without the flush, `print(r1)`
// would be handed the opaque i8* handle. The op order alone proves the flush;
// this case is kept separate so the reason for the barrier rule is written down
// next to a failing shape rather than only in the pass's comment.
func TestAsyncGroupBarrierSeesValueNotHandle(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    r1 i64
    {
        r1 = work-async(1)
        print(r1)
    }
}
`, "main"), "run", "await")
}

// TestAsyncGroupFnBodyIsNotAGroup: the function body is NOT a bare block (it
// hangs off the "body" slot), so a bare `-async` call there keeps its
// pre-existing meaning — a lazily enqueued future, no await. This is the
// control that proves the pass is scoped to statement-position blocks.
func TestAsyncGroupFnBodyIsNotAGroup(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    f = work-async(1)
    print(1)
}
`, "main"), "run")
}

// TestAsyncGroupWithoutAsyncCallsIsUntouched: a bare block with no `-async`
// call must lower to no run/await at all. The pass reports "no group" and
// leaves the block byte-for-byte alone, which is what makes it a no-op for the
// existing corpus.
func TestAsyncGroupWithoutAsyncCallsIsUntouched(t *testing.T) {
	wantOps(t, asyncOpsIn(t, `work = (n i64) (r i64) {
    r = n
}
main = () {
    r1 i64
    r2 i64
    {
        r1 = work(1)
        r2 = work(2)
    }
    print(r1)
    print(r2)
}
`, "main"))
}

// TestAsyncGroupDiscardedResultIsAwaited: a bare `-async` call statement drops
// its result. It is still spawned AND still awaited — an un-awaited task leaks
// its argument buffer, so "nobody wants the value" is not a reason to skip the
// await.
func TestAsyncGroupDiscardedResultIsAwaited(t *testing.T) {
	wantOps(t, asyncOpsIn(t, `side-async = (n i64) {
    print(n)
}
main = () {
    {
        side-async(7)
        side-async(8)
    }
}
`, "main"), "run", "run", "await", "await")
}

// TestAsyncGroupKillSwitch: NOLANG_ASYNC_GROUP=0 must restore the pre-pass
// lowering exactly. It is the A/B lever for validating the pass against a
// corpus without building a second binary.
func TestAsyncGroupKillSwitch(t *testing.T) {
	t.Setenv("NOLANG_ASYNC_GROUP", "0")
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    r1 i64
    r2 i64
    {
        r1 = work-async(1)
        r2 = work-async(2)
    }
    print(r1)
    print(r2)
}
`, "main"), "run", "run")
}
