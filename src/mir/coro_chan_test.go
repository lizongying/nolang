package mir

import (
	"strings"
	"testing"
)

// coro_chan_test.go pins the channel half of coroutine planning: an empty
// receive is a SUSPENSION POINT, not an error code.
//
// The observable is the PLAN PlanCoroutines produced (Function.Coro), because
// the decision has two halves that no runtime assertion can separate:
//
//  1. The receiving clone must become a coroutine at all. Before this, only
//     OpAwait / OpYield qualified, so a `<- ch:` loop ran to completion and an
//     empty read surfaced as the "open but empty" status (the `err` arm).
//  2. The park's RESUME point must be the PARK BLOCK, not the receive. Being
//     woken only means "something changed": a send adds one element but wakes
//     the whole waiter chain, so a receiver that resumed straight at the
//     receive would re-read a channel another woken task just drained — in the
//     loop shape that is an early exit with a missing value.
//
// NON-VACUITY: TestCoroutineIgnoresSendOnlyClone is the control. A clone that
// only sends must stay a plain function; without it, a pass that turned every
// `-async` clone into a coroutine would still pass the first test.

// The call at the end is not decoration: lowering is REACHABILITY-DRIVEN, so a
// function nothing calls never reaches MIR (measured: without it the module
// contained only the synthesized `main`).
const coroChanSrc = `consume = (c chan[i64]) {
    last = 0
    i <- c: {
        last = i
    }
}

produce = (c chan[i64]) {
    c <- 1
}

group = () {
    c chan[i64] = with-cap(2)
    awy {
        co consume(c)
        co produce(c)
    }
}

group()
`

// mustFunc looks a function up by lowered name (the `-async` clones carry the
// suffix MonomorphizeGo appends).
func mustFunc(t *testing.T, mod *Module, name string) *Function {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		names := make([]string, 0, len(mod.FuncByName))
		for n := range mod.FuncByName {
			names = append(names, n)
		}
		t.Fatalf("function %q not found; have %v", name, names)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
	return f
}

func TestCoroutineParksOnChannelReceive(t *testing.T) {
	mod := lowerForTest(t, coroChanSrc)
	f := mustFunc(t, mod, "consume-async")

	if !f.IsCoro || f.Coro == nil {
		t.Fatalf("a clone whose loop receives must be a coroutine; IsCoro=%v", f.IsCoro)
	}
	if len(f.Coro.Parks) != 1 {
		t.Fatalf("want exactly one park (the receive), got %d", len(f.Coro.Parks))
	}
	for parkBlock, p := range f.Coro.Parks {
		if p.Kind != CoroParkChan {
			t.Fatalf("park kind = %v, want CoroParkChan", p.Kind)
		}
		if p.Handle <= NoVal {
			t.Fatalf("channel park must carry the channel value; Handle=%v", p.Handle)
		}
		if p.Resume != parkBlock {
			t.Errorf("resume block = %v, want the park block %v (a wake-up must re-check readiness)", p.Resume, parkBlock)
		}
		if p.Cont == p.Resume {
			t.Errorf("continuation and resume are the same block %v: the receive would be re-entered without a readiness check", p.Cont)
		}
		b := mod.Block(p.Cont)
		if b == nil || len(b.Insts) == 0 {
			t.Fatalf("continuation block %v is empty; the receive should start it", p.Cont)
		}
		if op := mod.Inst(b.Insts[0]).Op; op != OpChanRecv {
			t.Errorf("continuation opens with %v, want OpChanRecv", op)
		}
		// Dispatch must be able to reach the park block: it is the resume
		// target of state p.State, and the States slice is the dispatch chain.
		found := false
		for _, st := range f.Coro.States {
			if st.State == p.State && st.Resume == parkBlock {
				found = true
			}
		}
		if !found {
			t.Errorf("state %d is not in CoroInfo.States with resume = %v", p.State, parkBlock)
		}
	}
}

func TestCoroutineIgnoresSendOnlyClone(t *testing.T) {
	mod := lowerForTest(t, coroChanSrc)
	f := mustFunc(t, mod, "produce-async")
	if f.IsCoro || f.Coro != nil {
		t.Fatalf("a send-only clone must stay a plain function; IsCoro=%v", f.IsCoro)
	}
}

// TestChanFreeRejectsDanglingWaiters pins design/nolang-coro.md §9 gap #1.
// A parked task holds only a raw i64 channel handle, not a reference to the
// %chancb, so freeing a channel with a non-empty waiters chain left the task
// on a destroyed chain (use-after-free on resume). chan_free must now check
// field 6 and abort before releasing anything.
func TestChanFreeRejectsDanglingWaiters(t *testing.T) {
	src := `main = () {
	c chan[i64] = with-cap(1)
	c <- 1
}
`
	mod := lowerForTest(t, src)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	start := strings.Index(ir, "define void @nolang_chan_free")
	if start < 0 {
		t.Fatal("no chan_free emitted")
	}
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		t.Fatal("unterminated chan_free")
	}
	fn := ir[start : start+end]
	if !strings.Contains(fn, "waiters:") {
		t.Error("chan_free has no waiters check")
	}
	if !strings.Contains(fn, "call void @nolang_deadlock_abort()") {
		t.Error("chan_free does not fail loudly on a dangling waiters chain")
	}
}
