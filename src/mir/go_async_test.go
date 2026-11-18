package mir

import (
	"testing"
)

// `co` only spawns a PROCEDURE: the callee may not declare a result (a method's
// implicit `self` does not count), so every spawn target below is written with
// no result list and results travel through a channel.
//
// TestGoMonomorphizesAsyncVariant pins the colorless-async monomorphization:
// `co worker(1)` must generate a `worker-async` definition (cloned from the
// sync `worker`) and retarget the spawn at it. The observable is the presence
// of `worker-async` in the lowered function table — if monomorphization did
// not run, only `worker` would exist and the call would lower to a bare
// (non-async) out-param call, not an OpRun.
func TestGoMonomorphizesAsyncVariant(t *testing.T) {
	src := `worker = (n i64) {
		print(n)
	}
	main = () {
		awy {
			co worker(1)
		}
	}`
	mod := lowerForTest(t, src)
	if _, ok := mod.FuncByName["worker-async"]; !ok {
		t.Fatalf("expected monomorphized worker-async to exist; funcs=%v", mod.FuncByName)
	}
	// The spawn must be an OpRun (async launch), not an eager call.
	ops := asyncOpsIn(t, src, "main")
	found := false
	for _, op := range ops {
		if op == "run" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an OpRun for the `co worker` spawn; ops=%v", ops)
	}
}

// TestGoTransitiveAsyncColoring pins the upward propagation: a function that
// calls an async function (`worker` calls `child` via `co`) also receives an
// `-async` variant, so the whole async chain is monomorphized.
func TestGoTransitiveAsyncColoring(t *testing.T) {
	src := `child = (n i64) {
		print(n)
	}
	worker = (n i64) {
		awy {
			co child(n)
		}
	}
	main = () {
		awy {
			co worker(1)
		}
	}`
	mod := lowerForTest(t, src)
	for _, want := range []string{"child-async", "worker-async"} {
		if _, ok := mod.FuncByName[want]; !ok {
			t.Fatalf("expected monomorphized %s to exist; funcs=%v", want, mod.FuncByName)
		}
	}
}
