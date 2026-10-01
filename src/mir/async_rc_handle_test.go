package mir

import (
	"strings"
	"testing"
)

// These tests pin correction B (NOLANG-OWNERSHIP-MODEL.md §1.3): RC's scope is
// exactly the coroutine-spawn edge, so the only refcounted object is the %task
// that `run` produces.
//
// WHY THE ASSERTIONS ARE ON THE EMITTED MIR/IR, NOT ON STDOUT
// -----------------------------------------------------------
//   * A leak is invisible to stdout. The un-awaited-reference case below
//     computes the right answer while retaining a count nothing will release;
//     only the emitted retain/release sites can show that.
//   * The crash the change fixes IS observable, but only for the aliased-handle
//     shape, and only as a signal — a stdout test cannot distinguish "printed
//     the right thing then died" from "printed the right thing". The IR
//     assertions are what make the ownership discipline non-regressible:
//     dropping the retain, or restoring the unconditional free, must fail here.

const rcHandleSrc = `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h = run dbl(21)
    h2 = h
    a = awy h
    b = awy h2
    print(a)
    print(b)
}
`

// countOp counts instructions of a given op in a named function.
func countOp(t *testing.T, mod *Module, fn string, op Op) int {
	t.Helper()
	fid, ok := mod.FuncByName[fn]
	if !ok {
		t.Fatalf("function %q not found in lowered module", fn)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", fn, fid)
	}
	n := 0
	for _, b := range f.Blocks {
		blk := mod.Block(b)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := mod.Inst(iid); inst != nil && inst.Op == op {
				n++
			}
		}
	}
	return n
}

// TestHandleCopyEmitsTaskRetain pins the core of the R-tier discipline: copying
// a handle is a NEW reference to the same %task, so it must retain. Without it
// the first await frees the task out from under the aliased handle — the
// measured SIGSEGV this change fixes (`h2 = h; awy h; awy h2` printed 42 and
// then died, because the old scheme freed unconditionally and only zeroed the
// awaited SLOT).
func TestHandleCopyEmitsTaskRetain(t *testing.T) {
	mod := lowerForTest(t, rcHandleSrc)
	if got := countOp(t, mod, "main", OpTaskRetain); got != 1 {
		t.Errorf("OpTaskRetain count = %d, want exactly 1 (one handle copy `h2 = h`)", got)
	}
	if got := countOp(t, mod, "main", OpAwait); got != 2 {
		t.Errorf("OpAwait count = %d, want 2", got)
	}
}

// TestSpawnWithoutHandleCopyEmitsNoRetain is the negative control. A retain is
// only correct for a COPY; emitting one for the spawn's own reference would
// leave the count permanently above zero and leak every task. Without this case
// "retain unconditionally" would also pass the test above.
func TestSpawnWithoutHandleCopyEmitsNoRetain(t *testing.T) {
	mod := lowerForTest(t, `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h = run dbl(21)
    print(awy h)
}
`)
	if got := countOp(t, mod, "main", OpTaskRetain); got != 0 {
		t.Errorf("OpTaskRetain count = %d, want 0 for a spawn+await with no handle copy", got)
	}
}

// TestTaskIsRefcountedBlockAndAwaitReleases pins the allocation and the release
// side in the emitted IR.
func TestTaskIsRefcountedBlockAndAwaitReleases(t *testing.T) {
	mod := lowerForTest(t, rcHandleSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	for _, want := range []string{
		"%nolang_hdr = type { i64, i64 }",
		"define i8* @nolang_rc_alloc(i64 %n)",
		"define void @nolang_rc_retain(i8* %p)",
		"define i1 @nolang_rc_release(i8* %p)",
		// the task itself is the R-tier block
		"@nolang_rc_alloc(i64 32)",
		// the await releases rather than freeing unconditionally
		"call i1 @nolang_rc_release(i8*",
		// the handle copy retains
		"call void @nolang_rc_retain(i8*",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("emitted IR is missing %q", want)
		}
	}
}

// TestAwaitDoesNotFreeTaskDataPointer guards the exact bug hit while writing
// this change: @nolang_rc_release already frees the allocation's BASE (data-16)
// when the count reaches zero, so the await's free block must free only the two
// borrowed containers. Freeing the %task data pointer as well frees an interior
// address of a block that is already gone — measured trace/BPT trap.
func TestAwaitDoesNotFreeTaskDataPointer(t *testing.T) {
	mod := lowerForTest(t, rcHandleSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if strings.Contains(ir, "%aawy.freetask") {
		t.Errorf("await still frees the task data pointer; @nolang_rc_release owns that block\n%s",
			ir)
	}
	// The release helper must free %base (p - 16), never %p.
	if !strings.Contains(ir, "%base = getelementptr inbounds i8, i8* %p, i64 -16") {
		t.Errorf("nolang_rc_release does not compute the block base as p - 16")
	}
	if strings.Contains(ir, "call void @free(i8* %p)") {
		t.Errorf("nolang_rc_release frees the DATA pointer instead of the block base")
	}
}

// TestRcReleaseHasBorrowedSentinel pins the rc == 0 guard (§3.4). A borrowed
// block (an FFI pointer, a string constant, the cap==0 str -> []byte view) must
// never be freed; this codebase has already been bitten once by freeing a
// borrowed view (the ensureVecBuffer bug, trace/BPT trap). The sentinel is what
// keeps the helpers safe when the R tier is extended past the task object.
func TestRcReleaseHasBorrowedSentinel(t *testing.T) {
	mod := lowerForTest(t, rcHandleSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// Both helpers must bail out on a zero count before touching the block.
	if got := strings.Count(ir, "%borrowed = icmp eq i64 %rc, 0"); got != 2 {
		t.Errorf("borrowed sentinel test count = %d, want 2 (retain and release)", got)
	}
}

// TestHandleAliasesChainRetainsEachCopy covers `h3 = h2`: every copy is its own
// reference, so a chain of n aliases needs n retains. Measured before the fix:
// `h = run dbl(21); h2 = h; h3 = h2; awy h; awy h2; awy h3` segfaulted; now it
// prints 42 three times.
func TestHandleAliasesChainRetainsEachCopy(t *testing.T) {
	mod := lowerForTest(t, `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h = run dbl(21)
    h2 = h
    h3 = h2
    a = awy h
    b = awy h2
    c = awy h3
    print(a)
    print(b)
    print(c)
}
`)
	if got := countOp(t, mod, "main", OpTaskRetain); got != 2 {
		t.Errorf("OpTaskRetain count = %d, want 2 (two copies: h2 = h, h3 = h2)", got)
	}
}

// TestReboundHandleStillCarriesRetain covers the OTHER copy path: a rebind
// (`t = run f(...)` into a pre-existing slot) goes through EmitMoveInto +
// carryAsyncResType rather than the fresh-binding path. Both must retain, or one
// of the two spellings silently keeps the old use-after-free.
func TestReboundHandleStillCarriesRetain(t *testing.T) {
	mod := lowerForTest(t, `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h i64 = 0
    h = run dbl(21)
    h2 i64 = 0
    h2 = h
    a = awy h
    b = awy h2
    print(a)
    print(b)
}
`)
	if got := countOp(t, mod, "main", OpTaskRetain); got < 1 {
		t.Errorf("OpTaskRetain count = %d, want >= 1 for a handle copy onto a pre-existing slot", got)
	}
}

// voidAliasSrc is the VOID-result handle-copy shape. It is the fixture for both
// the lowering fix and checkRefBalance: the task returns nothing, so it has a
// handle but no result type, which is exactly the combination the pre-fix code
// mis-handled.
const voidAliasSrc = `void-async = () {
    print('inside')
}

main = () {
    h = run void-async()
    h2 = h
    awy h
    awy h2
}
`

// TestVoidTaskHandleCopyRetains pins the VOID-result half of the R-tier
// discipline. A task that returns nothing is still a task: `run` allocates it
// with rc = 1, so every copy of its handle must retain.
//
// The retain used to be gated on the task's RESULT TYPE being known
// (asyncResTypes[v]), which a void task never has. Its handle copies therefore
// went uncounted: the first await drove rc to 0 and freed the task while the
// second reference still pointed at it, and the second await released a freed
// block. Measured on the pre-fix binary: rc = 139 (SIGSEGV) for this exact
// program, while the same shape with an i64 result printed correctly. The
// asymmetry between the two was the whole bug.
func TestVoidTaskHandleCopyRetains(t *testing.T) {
	mod := lowerForTest(t, voidAliasSrc)
	if got := countOp(t, mod, "main", OpTaskRetain); got != 1 {
		t.Errorf("OpTaskRetain count = %d, want 1: a VOID task's handle copy is still a reference", got)
	}
	if got := countOp(t, mod, "main", OpAwait); got != 2 {
		t.Errorf("OpAwait count = %d, want 2", got)
	}
}

// TestRunHandleForwardCreatesFreshReference pins `h2 = run h`. `run` of an
// existing handle is NOT a spawn site — it forwards the handle — so it yields
// another REFERENCE to the same task and must retain, exactly like `h2 = h`.
//
// Returning the operand value as-is made the two spellings disagree. `h2 = h`
// retained and printed 42 twice; `h2 = run h` added no count, so the first await
// freed the task and zeroed the slot the copy shared, and the second await read
// 0 (measured `42 0`, with only a stderr note). Two spellings of "a second
// reference to this task" must not have different ownership.
func TestRunHandleForwardCreatesFreshReference(t *testing.T) {
	mod := lowerForTest(t, `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h = run dbl(21)
    h2 = run h
    a = awy h
    b = awy h2
    print(a)
    print(b)
}
`)
	if got := countOp(t, mod, "main", OpTaskRetain); got != 1 {
		t.Errorf("OpTaskRetain count = %d, want 1: `run h` forwards the handle, so it is a new reference", got)
	}
	// The forwarding must NOT spawn a second task: exactly one OpRun, and it is
	// the `run dbl(21)` that owns the single rc = 1.
	if got := countOp(t, mod, "main", OpRun); got != 1 {
		t.Errorf("OpRun count = %d, want 1: `run h` must not spawn a second task", got)
	}
}

// stripTaskRetains clears the operand of every OpTaskRetain in `fn`, making the
// module look like the pre-fix output. Inst() returns a pointer into the module's
// instruction slice, so the mutation is visible to later analysis — which is what
// lets the validator test below construct the defect instead of asserting on a
// fixture that happens to contain it.
func stripTaskRetains(mod *Module, fn string) int {
	fid, ok := mod.FuncByName[fn]
	if !ok {
		return 0
	}
	f := mod.Func(fid)
	if f == nil {
		return 0
	}
	n := 0
	for _, b := range f.Blocks {
		blk := mod.Block(b)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := mod.Inst(iid); inst != nil && inst.Op == OpTaskRetain {
				inst.Args = nil
				n++
			}
		}
	}
	return n
}

// TestCheckRefBalanceSilentOnFixedModule is the false-positive control. The
// validator gates codegen (§4.5), so a spurious diagnostic is a hard build
// failure — the historical failure mode was checkDropCount using the wrong
// predicate and refusing 14 files.
func TestCheckRefBalanceSilentOnFixedModule(t *testing.T) {
	mod := lowerForTest(t, voidAliasSrc)
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("function main not found")
	}
	rep := &Report{}
	mod.checkRefBalance(mod.Func(fid), rep)
	if rep.HasErrors() {
		t.Errorf("checkRefBalance reported %v on a correctly-retained module", rep.Diagnostics)
	}
}

// TestCheckRefBalanceFiresOnMissingRetain is the "must fail before the fix" test
// §7.2 requires of every new validator: strip the retain the fix added and the
// check must notice. Without this, the validator could be silently vacuous —
// passing because it looks at nothing, not because the program is sound.
func TestCheckRefBalanceFiresOnMissingRetain(t *testing.T) {
	mod := lowerForTest(t, voidAliasSrc)
	if n := stripTaskRetains(mod, "main"); n == 0 {
		t.Fatal("no OpTaskRetain to strip: the fixture no longer exercises the defect")
	}
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("function main not found")
	}
	rep := &Report{}
	mod.checkRefBalance(mod.Func(fid), rep)
	if !rep.HasErrors() {
		t.Fatal("checkRefBalance found nothing after the retain was removed; " +
			"it cannot catch the SIGSEGV class it exists for")
	}
	if k := rep.Diagnostics[0].Kind; k != "missing-task-retain" {
		t.Errorf("diagnostic kind = %q, want %q", k, "missing-task-retain")
	}
}

// TestCheckRefBalanceFiresOnForwardedHandleWithoutRetain is the same test for
// the `run h` spelling. Both spellings must be covered, because the two bugs
// were in different lowering paths and a single-shape test would have let one
// of them regress.
func TestCheckRefBalanceFiresOnForwardedHandleWithoutRetain(t *testing.T) {
	mod := lowerForTest(t, `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h = run dbl(21)
    h2 = run h
    a = awy h
    b = awy h2
    print(a)
    print(b)
}
`)
	if n := stripTaskRetains(mod, "main"); n == 0 {
		t.Fatal("no OpTaskRetain to strip: the `run h` forwarding no longer retains")
	}
	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("function main not found")
	}
	rep := &Report{}
	mod.checkRefBalance(mod.Func(fid), rep)
	if !rep.HasErrors() {
		t.Fatal("checkRefBalance found nothing for a forwarded handle with no retain")
	}
}
