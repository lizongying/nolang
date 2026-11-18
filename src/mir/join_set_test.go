package mir

import (
	"strings"
	"testing"
)

const joinSetSrc = `work = (n i64) {
    print(n)
}
main = () {
    i i64 = 0
    awy {
        for i < 2 {
            co work(1)
        }
    }
}
`

// TestJoinSetRuntimeIR pins the compiler-internal runtime that implements
// group-tail join-all for `co` statements inside control-flow arms. The HIR/MIR
// shape tests only prove __join-* calls are emitted; this one proves the
// runtime they call actually exists and performs the two required operations:
// drain each target, then retire exactly one reference per recorded handle.
func TestJoinSetRuntimeIR(t *testing.T) {
	mod := lowerForTest(t, joinSetSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	for _, want := range []string{
		"@nolang_join_sp = global i32 0",
		"@nolang_join_n = global [64 x i32] zeroinitializer",
		"@nolang_join_h = global [64 x [256 x i64]] zeroinitializer",
		"define void @nolang_join_reap(i32 %n, i64* %slots)",
		"define void @nolang_join_enter()",
		"define void @nolang_join_push(i64 %h)",
		"define void @nolang_join_leave()",
		"define void @nolang_join_abort()",
		"define void @nolang_task_retire(i8* %task)",
		"%joinframe = type { i8*, i32, i32, [256 x i64] }",
		"call void @nolang_async_drain_until(i8* %hp)",
		"call void @nolang_task_retire(i8* %hp)",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("IR does not contain %q (join-set runtime incomplete)", want)
		}
	}
	// A join-set that emits enter/leave but forgets to pop the frame would
	// leak a frame per group; pin the stack-pointer writeback in leave.
	if !strings.Contains(ir, "store i32 %rtop, i32* @nolang_join_sp") {
		t.Error("join-leave does not pop the root join frame")
	}
	// A task's frame is chained off the task, not the global array; this is
	// what prevents two suspended groups from popping each other's frames.
	for _, want := range []string{
		"getelementptr inbounds %task, %task* %tt, i32 0, i32 10",
		"call i8* @nolang_rc_alloc(i64 " + itoaI(joinFrameBytes) + ")",
		"call void @nolang_free(i8* %frraw)",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("IR does not contain %q (per-task join frames incomplete)", want)
		}
	}
}

// joinFuncBody isolates one emitted runtime definition. Checking the whole
// module would be wrong: @nolang_async_drain_until(i8* null) is also emitted
// for channel empty-read paths and legitimately appears elsewhere.
func joinFuncBody(ir, name string) string {
	start := strings.Index(ir, "define void @"+name+"(")
	if start < 0 {
		return ""
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestJoinLeaveDrainsEachTargetBeforeRetire is the IR-shape regression for the
// parked-task UAF: a null-target drain exits on an empty ready queue even when
// a recorded task is still parked, and retire then frees its live frame. The
// per-handle target path carries the deadlock check; root and per-task frames
// both delegate the drain/retire loop to @nolang_join_reap.
func TestJoinLeaveDrainsEachTargetBeforeRetire(t *testing.T) {
	mod := lowerForTest(t, joinSetSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	body := joinFuncBody(ir, "nolang_join_leave")
	if body == "" {
		t.Fatal("no @nolang_join_leave definition in emitted IR")
	}
	if strings.Contains(body, "drain_until(i8* null)") {
		t.Error("join-leave uses a null-target drain; parked tasks would be retired while suspended")
	}
	if !strings.Contains(body, "call void @nolang_join_reap(") {
		t.Error("join-leave does not delegate to the per-handle reap loop")
	}
	reap := joinFuncBody(ir, "nolang_join_reap")
	drain := strings.Index(reap, "drain_until(i8* %hp)")
	retire := strings.Index(reap, "task_retire(i8* %hp)")
	if drain < 0 || retire < 0 || drain > retire {
		t.Errorf("reap must drain %%hp before retiring it (drain=%d, retire=%d)", drain, retire)
	}
	for _, want := range []string{
		"%h = load i64, i64* %slot",
		"%hp = inttoptr i64 %h to i8*",
	} {
		if !strings.Contains(reap, want) {
			t.Errorf("reap body does not contain %q", want)
		}
	}
}

// TestJoinSetPerTaskFrames pins the P1 fix: a task that suspends inside an awy
// group owns its join frame, so another task's enter/leave cannot pop it. The
// global arrays stay for top-level main only.
func TestJoinSetPerTaskFrames(t *testing.T) {
	mod := lowerForTest(t, joinSetSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	for _, tc := range []struct {
		fn   string
		want []string
	}{
		{"nolang_join_enter", []string{
			"load i8*, i8** @nolang_current_task",
			"getelementptr inbounds %task, %task* %tt, i32 0, i32 10",
			"call i8* @nolang_rc_alloc(i64 " + itoaI(joinFrameBytes) + ")",
			"store i8* %raw, i8** %tp",
		}},
		{"nolang_join_leave", []string{
			"load i8*, i8** @nolang_current_task",
			"call void @nolang_free(i8* %frraw)",
			"store i8* %jprev, i8** %tp",
		}},
		{"nolang_task_retire", []string{
			"getelementptr inbounds %task, %task* %t, i32 0, i32 10",
			"%jf = phi i8* [ %jtop, %entry ], [ %jprev, %jbody ]",
			"%jfr = bitcast i8* %jf to %joinframe*",
		}},
	} {
		body := joinFuncBody(ir, tc.fn)
		if body == "" {
			t.Fatalf("no @%s definition in emitted IR", tc.fn)
		}
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("@%s does not contain %q (per-task join frames incomplete)", tc.fn, want)
			}
		}
	}
}

// TestJoinSetGatedWhenUnused: a module with no control-flow-arm `co` must not
// carry the join-set runtime. Without this gate every build paid a 128 KiB BSS
// table (@nolang_join_h) plus 5 dead defines, even for a hello-world program.
// The core scheduler runtime stays unconditional; only the join leaf is gated.
func TestJoinSetGatedWhenUnused(t *testing.T) {
	src := `print('hi')
`
	mod := lowerForTest(t, src)
	if mod.scanJoinSetUse() {
		t.Error("scanJoinSetUse = true for a program with no `awy` group")
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	for _, unwanted := range []string{
		"@nolang_join_sp = ",
		"@nolang_join_h = ",
		"define void @nolang_join_reap(",
		"define void @nolang_join_enter(",
		"define void @nolang_join_push(",
		"define void @nolang_join_leave(",
		"define void @nolang_join_abort(",
	} {
		if strings.Contains(ir, unwanted) {
			t.Errorf("unused join runtime still emitted: %q", unwanted)
		}
	}
	// The scheduler itself must remain (OpRun/OpAwait callers and channel code
	// still reference it regardless of join-set use).
	if !strings.Contains(ir, "@nolang_async_drain_until") {
		t.Error("gating join-set also removed the core async scheduler")
	}
}

// TestJoinSetScanDetectsRewrittenSpawns: the gate must key off the rewritten
// __join-* OpCalls, not off the source-level presence of `co`. A control-flow
// arm `co` is desugared into those calls, so the scanner sees them.
func TestJoinSetScanDetectsRewrittenSpawns(t *testing.T) {
	mod := lowerForTest(t, joinSetSrc)
	if !mod.scanJoinSetUse() {
		t.Error("scanJoinSetUse = false for a module with a control-flow-arm `co`")
	}
}
