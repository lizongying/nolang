package mir

import (
	"strings"
	"testing"
)

// These tests pin the coroutine group (協程組) desugaring — an `awy { ... }`
// block whose statements are `co` calls. The observable is the ORDER of OpRun /
// OpAwait in the lowered function, because order is the entire point: "spawn
// everything, then await" is the concurrent case, and "spawn, await, spawn,
// await" is the case where a barrier sits between the spawns.
//
// SURFACE SHAPE (2026-11-18). `co` only ever spawns a PROCEDURE: the callee may
// not declare a result (a method's implicit `self` does not count), so a spawn
// result can no longer be bound in the group at all — results travel through a
// channel. That removes the old "r2 = co work(r1) forces r1's await up front"
// shape from the language; a dependency is now written as a channel read inside
// the group, and a channel read is a BARRIER (it is not an `-async` call), so it
// produces the same run/await/run/await order for a different, better stated
// reason. The old `reads`-based dependency branch has been REMOVED from
// AsyncGroupPlan (2026-11-18): with `co` producing no value there is no result
// to depend on, so `Flush` now only records barrier statements.
//
// NON-VACUITY: each case has a sibling that must produce the OTHER pattern.
// TestAsyncGroupSpawnsIndependentCalls (run run await await) is the control for
// TestAsyncGroupChanReadInsideGroupIsBarrier (run await run await), and
// TestAsyncGroupPlainCallIsNotASpawn / TestAsyncGroupWithoutAsyncCallsIsUntouched
// prove the pass does not fire outside a group or on a group with no spawn —
// without them, a pass that rewrote every block would still pass the first two.

const asyncGroupWorkSrc = `work = (n i64) (r i64) {
    r = n
}

; 過程版：` + "`co`" + ` 只啟動過程，結果走通道
work-into = (ch chan[i64], n i64) {
    ch <- work(n)
    ch.close()
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

// TestAsyncGroupSpawnsIndependentCalls: two spawns with nothing between them
// must BOTH be launched before either is awaited.
func TestAsyncGroupSpawnsIndependentCalls(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    c1 chan[i64] = with-cap(1)
    c2 chan[i64] = with-cap(1)
    awy {
        co work-into(c1, 1)
        co work-into(c2, 2)
    }
    r1 = 0
    <- c1: {
        ok -> r1 = it
    }
    r2 = 0
    <- c2: {
        ok -> r2 = it
    }
    print(r1)
    print(r2)
}
`, "main"), "run", "run", "await", "await")
}

// TestAsyncGroupChanReadInsideGroupIsBarrier: the group's dependency case, as
// the language can still express it. A receive loop is not an `-async` call, so
// it is a barrier: the spawn before it must land before the loop runs, and the
// spawn after it cannot start earlier either.
//
// This is what replaced `r2 = co work(r1)` — that spelling is rejected by the
// parser now (`co` produces no value), and "read the channel" states the same
// ordering without relying on an analysis of the spawned call's arguments.
func TestAsyncGroupChanReadInsideGroupIsBarrier(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    c1 chan[i64] = with-cap(1)
    awy {
        co work-into(c1, 1)
        <- c1: {
            ok -> print(it)
        }
        co work-into(c1, 2)
    }
}
`, "main"), "run", "await", "run", "await")
}

// TestAsyncGroupBarrierFlushes: a statement that is not an `-async` call is a
// barrier — its reads and writes are not analyzed, so everything in flight
// lands before it. This is what keeps a `print` between two spawns from
// observing a half-run group.
func TestAsyncGroupBarrierFlushes(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    c1 chan[i64] = with-cap(1)
    c2 chan[i64] = with-cap(1)
    awy {
        co work-into(c1, 1)
        print(9)
        co work-into(c2, 2)
    }
}
`, "main"), "run", "await", "run", "await")
}

// TestAsyncGroupNestedBareBlockDefersAwaits covers design/nolang-coro.md §9 #3
// for the straight-line case. A bare block inside a group is a barrier, but a
// `co` inside it is still a spawn of that group: its handle is bound where the
// source put it, while the await is deferred to the group tail. Without the
// HIR handle collection the lowerer's #108 fallback pairs each spawn with an
// immediate await, which prints here as run/await/run/await.
func TestAsyncGroupNestedBareBlockDefersAwaits(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    c1 chan[i64] = with-cap(1)
    c2 chan[i64] = with-cap(1)
    awy {
        {
            co work-into(c1, 1)
            co work-into(c2, 2)
        }
    }
}
`, "main"), "run", "run", "await", "await")
}

// TestAsyncGroupNestedBareBlockStillFlushesEarlierSpawns: the barrier before
// the nested block must still land first — deferring the inner awaits must not
// weaken the ordering guarantee that made the block a barrier in the first
// place.
func TestAsyncGroupNestedBareBlockStillFlushesEarlierSpawns(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    c1 chan[i64] = with-cap(1)
    c2 chan[i64] = with-cap(1)
    awy {
        co work-into(c1, 1)
        {
            co work-into(c2, 2)
        }
    }
}
`, "main"), "run", "await", "run", "await")
}

// TestAsyncGroupPlainCallIsNotASpawn: a plain call outside an `awy` group is
// NOT a spawn — `co` is the only spawn marker and it is only legal in a group.
// This is the control that proves the pass is scoped to explicit awy groups.
func TestAsyncGroupPlainCallIsNotASpawn(t *testing.T) {
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    f = work(1)
    print(f)
}
`, "main"))
}

// TestAsyncGroupWithoutAsyncCallsIsUntouched: an awy block with no `co` spawn
// must lower to no run/await at all. The pass reports "no group" and leaves the
// block byte-for-byte alone.
func TestAsyncGroupWithoutAsyncCallsIsUntouched(t *testing.T) {
	wantOps(t, asyncOpsIn(t, `work = (n i64) (r i64) {
    r = n
}
main = () {
    r1 i64
    r2 i64
    awy {
        r1 = work(1)
        r2 = work(2)
    }
    print(r1)
    print(r2)
}
`, "main"))
}

// TestAsyncGroupDiscardedResultIsAwaited: a `co` statement produces no value at
// all (that is now the only form there is), and it is still spawned AND still
// awaited — an un-awaited task leaks its argument buffer and its frame, so
// "nobody wants the value" is not a reason to skip the await.
func TestAsyncGroupDiscardedResultIsAwaited(t *testing.T) {
	wantOps(t, asyncOpsIn(t, `side = (n i64) {
    print(n)
}
main = () {
    awy {
        co side(7)
        co side(8)
    }
}
`, "main"), "run", "run", "await", "await")
}

// TestAsyncGroupKillSwitch: NOLANG_ASYNC_GROUP=0 must disable the GROUP REWRITE
// — the `awy` block is left as written, so neither handle gets parked in a
// group-level await. What still happens per call is lowerCall's own pairing:
// a spawn whose handle no group schedule will consume is driven to completion
// right there (#108), which gives run/await per call instead of the group's
// batched run/run/await/await.
//
// That IS the A/B lever working: the two outputs differ in exactly the way the
// pass changes the program (batched spawns vs. sequential ones), while the
// kill-switch build no longer leaks an un-awaited handle.
func TestAsyncGroupKillSwitch(t *testing.T) {
	t.Setenv("NOLANG_ASYNC_GROUP", "0")
	wantOps(t, asyncOpsIn(t, asyncGroupWorkSrc+`main = () {
    c1 chan[i64] = with-cap(1)
    c2 chan[i64] = with-cap(1)
    {
        work-into-async(c1, 1)
        work-into-async(c2, 2)
    }
}
`, "main"), "run", "await", "run", "await")
}

// TestFlowArmCoIsNotAwaitedInline: a `co` inside a loop/if arm must lower to
// OpRun + a __join-push call, with NO OpAwait in the enclosing function. An
// inline await is the old #108 serial fallback: it makes mutually dependent
// spawns deadlock because the second task has not been spawned yet. The MIR
// shape, not stdout, is the reliable observable here (a deadlock aborts before
// any assertion can run).
func TestFlowArmCoIsNotAwaitedInline(t *testing.T) {
	src := `work = (n i64) {
    print(n)
}
main = () {
    i i64 = 0
    awy {
        for i < 2 {
            i == 0 -> {
                co work(1)
            }
            i = i + 1
        }
    }
}
`
	mod := lowerForTest(t, src)
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatalf("main not found; funcs=%v", mod.FuncByName)
	}
	f := mod.Func(fid)
	var runs, awaits, pushes, enters, leaves int
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			switch inst.Op {
			case OpRun:
				runs++
			case OpAwait:
				awaits++
			case OpCall:
				switch inst.Sym {
				case "__join-push":
					pushes++
				case "__join-enter":
					enters++
				case "__join-leave":
					leaves++
				}
			}
		}
	}
	if runs != 1 || awaits != 0 {
		t.Errorf("run/await = %d/%d, want 1/0 (inline await is the serial fallback)", runs, awaits)
	}
	if pushes != 1 || enters != 1 || leaves != 1 {
		t.Errorf("join enter/push/leave = %d/%d/%d, want 1/1/1", enters, pushes, leaves)
	}
}
