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
