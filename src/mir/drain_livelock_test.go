package mir

import (
	"strings"
	"testing"
)

// TestDrainUntilNullHasStepBound pins design/nolang-coro.md §9 gap #2. The
// null-target drain (used by non-coroutine blocking channel receive) used to
// spin forever when a task yielded unconditionally: each yield re-enqueues the
// task, so the ready queue never drains, the loop never reaches `exit`, and the
// existing "queue empty + target unfinished" deadlock check was unreachable.
// The drain now carries a phi step counter bounded at 1e6 in null mode only;
// ordinary `awy` (non-null target) still bounds on target.done.
func TestDrainUntilNullHasStepBound(t *testing.T) {
	src := `work = (n i64) { print(n) }
main = () {
	awy {
		co work(1)
	}
}
`
	mod := lowerForTest(t, src)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(ir, "define void @nolang_async_drain_until") {
		t.Fatal("no drain_until emitted; test cannot observe the step bound")
	}
	start := strings.Index(ir, "define void @nolang_async_drain_until")
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		t.Fatal("unterminated drain_until")
	}
	fn := ir[start : start+end]
	if !strings.Contains(fn, "%steps = phi i32") {
		t.Error("drain_until has no phi step counter")
	}
	if !strings.Contains(fn, "1000000") {
		t.Error("drain_until has no 1e6 livelock bound")
	}
	if !strings.Contains(fn, "%spin = and i1 %isnull, %over") {
		t.Error("step bound is not restricted to null-target drain mode")
	}
}
