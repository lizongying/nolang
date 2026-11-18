package mir

import (
	"regexp"
	"strings"
	"testing"
)

// These tests pin the two async-boundary / struct-ownership defects fixed on
// 2026-09-30, on top of the three spawn-boundary defects already covered by
// tests/async-ownership.no.
//
// WHY THE ASSERTIONS ARE ON THE EMITTED MIR/IR, NOT ON STDOUT
// ----------------------------------------------------------
//   * The struct-rebind leak is INVISIBLE to stdout. The program computes the
//     right answer either way; what is wrong is that a heap buffer is never
//     freed. Measured over 2M rebinds: 66.1 MB peak RSS before, 33.8 MB after,
//     byte-identical output. A stdout test cannot see that, so this asserts the
//     OpDrop the lowerer must emit.
//   * The spawn-boundary deep copy for a struct parameter IS observable, but
//     only in combination: with the rebind leak still present the aliasing is
//     masked (the leaked buffer stays valid), so the program prints the right
//     answer for the wrong reason. Pinning the IR is what makes the fix
//     non-regressible: drop either half and the other half stops being tested.
//   * The ready-queue capacity is not observable at all in the MIR backend —
//     nothing ever drains the queue (see TestAsyncReadyQueueGrows).

// argbufCloneWindow returns the lines of ir that follow the first
// `%arun.argbuf.t.<n>_<i>` definition, i.e. the spawn-boundary copy of one
// argument. The deep copy of a struct's owned fields is emitted right there.
func argbufCloneWindow(ir string) string {
	lines := strings.Split(ir, "\n")
	for i, l := range lines {
		if !strings.Contains(l, "%arun.argbuf.t.") {
			continue
		}
		end := i + 8
		if end > len(lines) {
			end = len(lines)
		}
		return strings.Join(lines[i:end], "\n")
	}
	return ""
}

// TestAsyncStructArgDeepCopiedAtSpawn pins the spawn-boundary deep copy for a
// struct parameter that owns heap through an inline `str` leaf.
//
// The argbuf is a BITWISE copy of the argument, so before this fix the task's
// `holder.s` shared the caller's buffer. The caller is free to rebind or drop
// the struct before `awy` runs, so the task would then read freed memory. The
// bug was masked by the struct-rebind leak (the buffer was never freed, so
// reading it was accidentally safe) — which is exactly why the two fixes have
// to land together.
func TestAsyncStructArgDeepCopiedAtSpawn(t *testing.T) {
	mod := lowerForTest(t, `holder { s str }

echo-holder = (h holder) {
    print(h.s.len())
}

main = () {
    h holder = holder { s: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' }
    awy {
        co echo-holder(h)
    }
    h = holder { s: 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB' }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// 1. The argbuf must get its OWN copy of the `s` leaf. The clone is the
	//    in-place leaf walk: GEP into the argbuf's field, load, @str_clone,
	//    store back.
	win := argbufCloneWindow(ir)
	if !strings.Contains(win, "@str_clone") {
		t.Errorf("struct argument is stored into the argbuf WITHOUT cloning its owned leaf; the task aliases the caller's buffer:\n%s", win)
	}
	// 2. The wrapper must release that copy, with the SAME recursive
	//    destructor emitDrop uses, or the deep copy leaks instead of the
	//    caller's buffer.
	wi := strings.Index(ir, "define void @async_wrapper.")
	if wi < 0 {
		t.Fatalf("no async wrapper emitted:\n%s", ir)
	}
	body := ir[wi:]
	if end := strings.Index(body[1:], "\ndefine "); end > 0 {
		body = body[:end+1]
	}
	if !strings.Contains(body, "@__nolang_drop_holder") {
		t.Errorf("async wrapper does not free the struct argbuf's owned fields (leak):\n%s", body)
	}
}

// TestStructRebindFreesOldFields pins the struct-rebind leak.
//
// Re-binding a local used to free the old value only when the value was
// Type.Owned (str / vec / []T / map / ?owned). A struct is deliberately NOT
// Type.Owned — Type.Owned doubles as the lowerer's "does a binding alias"
// signal — so `h = holder { ... }` overwrote the old struct's owned leaf
// buffers and leaked them, one per rebind. The drop machinery already owned
// structs (dropOwnsHeap -> typeOwnsHeap, and emitDrop frees them with the
// recursive destructor); only the lowerer's gate was too narrow.
func TestStructRebindFreesOldFields(t *testing.T) {
	mod := lowerForTest(t, `holder { s str }

main = () {
    h holder = holder { s: 'A' }
    h = holder { s: 'B' }
    x str = h.s
    print(x)
}
`)
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("main not found in lowered module")
	}
	f := mod.Func(fid)
	var insts []*Inst
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := mod.Inst(iid); inst != nil {
				insts = append(insts, inst)
			}
		}
	}
	// The rebind is an EmitMoveInto: a two-operand OpMove whose destination
	// (Args[1]) is the struct local.
	moveIdx := -1
	dst := ValueID(NoVal)
	for i, inst := range insts {
		if inst.Op != OpMove || len(inst.Args) != 2 {
			continue
		}
		d := inst.Args[1]
		if tt := mod.Type(f.LocalTypes[d]); tt != nil && tt.Kind == KindStruct {
			moveIdx, dst = i, d
			break
		}
	}
	if moveIdx < 0 {
		t.Fatalf("no struct rebind (two-operand OpMove into a struct slot) found:\n%s", mod.DumpAnnotated())
	}
	for i := 0; i < moveIdx; i++ {
		if insts[i].Op == OpDrop && len(insts[i].Args) > 0 && insts[i].Args[0] == dst {
			return // found the pre-rebind drop
		}
	}
	t.Errorf("struct rebind does not free the destination's old fields: no OpDrop of value %d before the OpMove at index %d (leak).\n%s",
		dst, moveIdx, mod.DumpAnnotated())
}

// 旧的手写句柄重绑定测试（t i64 = 0; t = run f(...); v = awy t）随 run/awy
// 前缀语法一并移除：新语法只有 `awy { co f() }`，结果类型直接由 co 表达式
// 决定，不再存在「句柄槽位重绑定丢失 asyncResTypes」的 surface 路径。

// TestAsyncReadyQueueGrows pins the ready-queue overflow fix.
//
// The ready queue was `@nolang_ready_q = global [256 x i8*]` with head/tail
// wrapping mod 256 and NO overflow check, so the 257th outstanding enqueue
// silently overwrote the slot at head. It is a LATENT defect in the MIR
// backend: `run` enqueues every spawned task and `async-yield()` re-enqueues
// the running one, but nothing ever dequeues, because the MIR backend never
// calls @nolang_async_run (awaits drive their task inline). Measured: a
// 300-spawn program produced correct output with rc=0 — the corruption was
// invisible, not absent. The queue is now heap-allocated and doubles on demand.
func TestAsyncReadyQueueGrows(t *testing.T) {
	mod := lowerForTest(t, `mk = (n i64) {
    print(n)
}

main = () {
    awy {
        co mk(1)
    }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if regexp.MustCompile(`@nolang_ready_q = global \[256 x i8\*\]`).MatchString(ir) {
		t.Errorf("ready queue is still a fixed 256-slot ring with no overflow check")
	}
	for _, want := range []string{"@nolang_ready_grow", "@nolang_ready_cap", "@nolang_ready_q = global i8**"} {
		if !strings.Contains(ir, want) {
			t.Errorf("growable ready queue missing %s", want)
		}
	}
	// The enqueue path must consult the capacity and grow when full, or the
	// new buffer would never be used.
	ei := strings.Index(ir, "define void @nolang_async_enqueue")
	if ei < 0 {
		t.Fatalf("no @nolang_async_enqueue emitted:\n%s", ir)
	}
	body := ir[ei:]
	if end := strings.Index(body[1:], "\ndefine "); end > 0 {
		body = body[:end+1]
	}
	if !strings.Contains(body, "@nolang_ready_grow") {
		t.Errorf("@nolang_async_enqueue never grows the queue:\n%s", body)
	}
}

// TestAsyncWaitersNotAddressAliased pins the waiter-table fix.
//
// The scheduler used to keep `@nolang_waiters = global [256 x i8*]` and index
// it with `ptrtoint(task) & 255`. malloc returns 16-byte-aligned blocks, so
// only the low 4 bits of a task pointer ever varied: the 256 slots collapsed to
// 16 and any two tasks whose addresses shared those bits silently aliased each
// other's waiter — `@nolang_async_done` would then wake the WRONG task. It is a
// latent defect in the MIR backend (nothing calls @nolang_async_wait yet;
// awaits drive their task inline), but it is a real correctness hole in the
// emitted runtime.
//
// The waiter now lives in the waited task's own `waiter` field (%task field 4),
// which is exact: one slot per task, no hashing, no collisions.
func TestAsyncWaitersNotAddressAliased(t *testing.T) {
	mod := lowerForTest(t, `mk = (n i64) {
    print(n)
}

main = () {
    awy {
        co mk(1)
    }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// The address-indexed table must be gone, and with it the masking idiom.
	// Match the DEFINITION form, not a bare mention: the emitted IR carries an
	// explanatory comment naming the old table.
	if strings.Contains(ir, "@nolang_waiters = global") {
		t.Errorf("waiter table is still address-indexed:\n%s", ir)
	}
	if strings.Contains(ir, "and i64 %idx, 255") {
		t.Errorf("`ptrtoint(task) & 255` waiter indexing is still emitted")
	}
	// The waiter must live in the task itself: field 4, and an allocation big
	// enough to hold every field. The task is now an R-tier block, so the
	// request goes through @nolang_rc_alloc — which adds a 16-byte header in
	// FRONT and still returns the payload — so the size invariant is about the
	// payload. %task grew to 8 fields (frame/next for the coroutine scheduler,
	// waitnext for the waiter chain, depth/pad for the nesting guard, and
	// join_top for per-task join-set frames): 9 words plus two i32s = 72 bytes.
	// Pin the exact shape and the exact request so a field added without a
	// matching re-size fails here rather than as a heap overrun at run time.
	if !strings.Contains(ir, "%task = type { void (i8*)*, i64, i1, i1, i8*, i8*, i8*, i8*, i32, i32, i8* }") {
		t.Errorf("%%task does not carry the waiters/frame/next/waitnext/depth fields:\n%s", ir)
	}
	if !strings.Contains(ir, "@nolang_rc_alloc(i64 72)") && !strings.Contains(ir, "@malloc(i64 72)") {
		t.Errorf("task allocation is not sized for the 11-field %%task (want a 72-byte request)")
	}
	for _, bad := range []string{"@malloc(i64 24)", "@nolang_rc_alloc(i64 24)", "@malloc(i64 32)", "@nolang_rc_alloc(i64 32)", "@malloc(i64 48)", "@nolang_rc_alloc(i64 48)"} {
		if strings.Contains(ir, bad) {
			t.Errorf("task allocation is smaller than %%task (%s)", bad)
		}
	}
	for _, want := range []string{
		"define void @nolang_async_wait(i8* %waited)",
		"define void @nolang_async_done(i8* %task)",
		"i32 0, i32 4",
		"i32 0, i32 7",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("waiter-in-task plumbing missing %q", want)
		}
	}
	// The chain, not a single slot: a second waiter has to be linked onto the
	// one already registered, so the waiter field is read as well as written.
	if !strings.Contains(ir, "%old = load i8*, i8** %wgep") {
		t.Errorf("@nolang_async_wait does not prepend onto the existing chain")
	}
}
