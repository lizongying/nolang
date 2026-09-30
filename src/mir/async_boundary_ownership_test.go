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

echo-holder-async = (h holder) (r str) {
    r = h.s
}

main = () {
    h holder = holder { s: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' }
    t = run echo-holder-async(h)
    h = holder { s: 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB' }
    v = awy t
    print(v)
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

// TestAsyncAwaitKeepsResultTypeAcrossRebind pins the asyncResTypes propagation
// on the REBIND path.
//
// A task handle is an opaque i64; the task's real result type is remembered at
// the `run` site in lowerer.asyncResTypes, keyed by MIR value id. `awy t`
// resolves the handle through the LOCAL's value id, and a reassignment
// (`t i64 = 0` … `t = run f(...)`) moves the OpRun result into the EXISTING
// slot — a different value id. The type was never carried across, so the await
// fell back to i64 and the result buffer was reinterpreted: a `str` result
// printed as its LENGTH.
//
//	holder { s str }
//	echo-holder-async = (h holder) (r str) { r = h.s }
//	t i64 = 0
//	t = run echo-holder-async(h)
//	print(awy t)     ; printed 40, want the 40-char string
//
// The same hole existed on the module-global rebind path.
func TestAsyncAwaitKeepsResultTypeAcrossRebind(t *testing.T) {
	mod := lowerForTest(t, `holder { s str }

echo-holder-async = (h holder) (r str) {
    r = h.s
}

main = () {
    h holder = holder { s: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' }
    t i64 = 0
    t = run echo-holder-async(h)
    v = awy t
    print(v)
}
`)
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("main not found in lowered module")
	}
	f := mod.Func(fid)
	var awaited *Inst
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := mod.Inst(iid); inst != nil && inst.Op == OpAwait {
				awaited = inst
				break
			}
		}
		if awaited != nil {
			break
		}
	}
	if awaited == nil {
		t.Fatalf("no OpAwait emitted:\n%s", mod.DumpAnnotated())
	}
	tt := mod.Type(awaited.Type)
	if tt == nil {
		t.Fatalf("OpAwait has no type; the handle's result type was lost")
	}
	// The callee returns str, so the awaited value must not be a bare i64 —
	// that is exactly the shape that printed the string's length.
	if tt.Kind == KindInt {
		t.Errorf("OpAwait of a rebound handle is typed %q (KindInt): the task's "+
			"result type was not carried across the rebind, so a str result is "+
			"read as its length.\n%s", tt.Raw, mod.DumpAnnotated())
	}
}

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
	mod := lowerForTest(t, `mk = (n i64) (r i64) {
    r = n
}

main = () {
    h = run mk(1)
    v = awy h
    print(v)
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
	mod := lowerForTest(t, `mk = (n i64) (r i64) {
    r = n
}

main = () {
    h = run mk(1)
    v = awy h
    print(v)
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
	// The waiter must live in the task itself: a 5th field, and a malloc big
	// enough to hold it (offset 24 + 8 = 32; a 24-byte block would leave the
	// field dangling past the allocation).
	if !strings.Contains(ir, "%task = type { void (i8*)*, i64, i1, i1, i8* }") {
		t.Errorf("%%task does not carry a waiter field:\n%s", ir)
	}
	if !strings.Contains(ir, "@malloc(i64 32)") {
		t.Errorf("task allocation is not sized for the waiter field (want @malloc(i64 32))")
	}
	if strings.Contains(ir, "@malloc(i64 24)") {
		t.Errorf("task allocation is still 24 bytes but %%task is now 32 bytes")
	}
	for _, want := range []string{
		"define void @nolang_async_wait(i8* %waited)",
		"define void @nolang_async_done(i8* %task)",
		"i32 0, i32 4",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("waiter-in-task plumbing missing %q", want)
		}
	}
}
