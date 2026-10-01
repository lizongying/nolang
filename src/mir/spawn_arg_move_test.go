package mir

import (
	"strings"
	"testing"
)

// These tests pin the "spawn argument as a move" intermediate of
// NOLANG-OWNERSHIP-MODEL.md §1.3.1 — the non-ABI way to avoid P0's
// spawn-boundary deep copy.
//
// WHAT THE CHANGE IS. `run f(x)` deep-copies every heap-owning argument into
// the task's own buffer, because the task may run at `awy` long after the caller
// reassigned or dropped x. When x is PROVABLY DEAD at the spawn that copy is
// pure waste: ownership transfers into the task instead. insertDrops suppresses
// x's drop and records x in mod.spawnArgMoves; emitAsyncRun reads that record and
// skips its clone. The task's wrapper (asyncWrapperFor's w_free) then frees the
// payload, exactly as it freed the copy before.
//
// WHY THE ASSERTIONS ARE ON THE MIR DROP SET AND THE IR, NOT ON STDOUT
// -------------------------------------------------------------------
// Both failure modes of this change are invisible to stdout:
//
//   * the move is NOT taken when it should be -> a redundant copy, correct
//     output, no way to see it from a print;
//   * the drop is suppressed but the copy is STILL made -> the payload leaks;
//   * the drop is kept but ownership also moves -> a DOUBLE FREE, and the second
//     free of a small string does not abort on this platform, so the program
//     still prints the right answer.
//
// So the observables are the ones the pass actually computes: which values are in
// spawnArgMoves, how many drops they have, and which side of the spawn boundary
// owns the destructor call.

// spawnArgSrc is the canonical "provably dead at the spawn" shape: `h` is never
// read after the spawn, so its ownership can be transferred.
const spawnArgSrc = `holder { s str }

echo-holder-async = (h holder) (r str) {
    r = h.s
}

main = () {
    h holder = holder { s: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' }
    t = run echo-holder-async(h)
    v = awy t
    print(v)
}
`

// spawnArgLiveSrc is the §2.3.2 regression shape: the source is REASSIGNED after
// the spawn, so it is live across it and the copy must stay.
const spawnArgLiveSrc = `holder { s str }

echo-holder-async = (h holder) (r str) {
    r = h.s
}

main = () {
    h holder = holder { s: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' }
    t = run echo-holder-async(h)
    print(h.s)
    v = awy t
    print(v)
}
`

// runArg returns the operand of the first real spawn (`run <callee>`) in fn.
func runArg(t *testing.T, mod *Module, fn string) ValueID {
	t.Helper()
	fid, ok := mod.FuncByName[fn]
	if !ok {
		t.Fatalf("function %q not found", fn)
	}
	f := mod.Func(fid)
	for _, b := range f.Blocks {
		blk := mod.Block(b)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst != nil && inst.Op == OpRun && inst.Sym != "" && len(inst.Args) > 0 {
				return inst.Args[0]
			}
		}
	}
	t.Fatalf("no `run <callee>` instruction found in %q", fn)
	return NoVal
}

// dropsOf counts OpDrop instructions in fn whose operand is v.
func dropsOf(mod *Module, fn string, v ValueID) int {
	f := mod.Func(mod.FuncByName[fn])
	n := 0
	for _, b := range f.Blocks {
		blk := mod.Block(b)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst != nil && inst.Op == OpDrop && len(inst.Args) > 0 && inst.Args[0] == v {
				n++
			}
		}
	}
	return n
}

// irFunc returns the text of the `define ... @name(...)` block in ir, or "".
// The generated IR always closes a definition with a line that is exactly "}".
//
// NOTE the entry symbol: a Nolang `main` is emitted as @_nolang_main, and the
// C-level `i32 @main(i32, i8**)` is a two-line stub that calls it. Looking up
// "main" therefore finds the stub, whose body contains neither the spawn nor the
// destructor, and every "is X absent?" assertion would pass vacuously.
func irFunc(ir, name string) string {
	for _, part := range strings.Split(ir, "\ndefine ") {
		head := part
		if i := strings.IndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
		if !strings.Contains(head, "@"+name+"(") {
			continue
		}
		if i := strings.Index(part, "\n}"); i >= 0 {
			return part[:i]
		}
		return part
	}
	return ""
}

// irWrapper returns the text of the first generated async_wrapper.N define.
// It matches on the HEAD, not on the whole block: the Nolang main also mentions
// `@async_wrapper.N` (it stores the pointer into the task), so a body-wide
// search would return main instead of the wrapper.
func irWrapper(ir string) string {
	for _, part := range strings.Split(ir, "\ndefine ") {
		head := part
		if i := strings.IndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
		if !strings.Contains(head, "@async_wrapper.") {
			continue
		}
		if i := strings.Index(part, "\n}"); i >= 0 {
			return part[:i]
		}
		return part
	}
	return ""
}

// TestSpawnArgMoveSuppressesSourceDrop is the core assertion: a spawn argument
// that is dead after the spawn is MOVED (recorded in spawnArgMoves) and the
// caller's drop of it is GONE, because the task now owns the payload.
func TestSpawnArgMoveSuppressesSourceDrop(t *testing.T) {
	mod := lowerForTest(t, spawnArgSrc)
	arg := runArg(t, mod, "main")
	if !mod.spawnArgMoves[arg] {
		t.Errorf("value %d is not in spawnArgMoves; the move was not taken for a provably-dead spawn argument", arg)
	}
	if n := dropsOf(mod, "main", arg); n != 0 {
		t.Errorf("main has %d OpDrop of the moved argument %d, want 0 (the task's wrapper frees it)", n, arg)
	}
}

// TestSpawnArgMoveKeepsWrapperAsTheSingleFreeSite is the other half of the same
// invariant, and the one that catches a double free: the destructor must appear
// on the TASK side and nowhere on the CALLER side. Before the move the caller
// dropped the source and the wrapper dropped the copy — two sites, two buffers.
// After the move there is one buffer, so there must be exactly one site.
func TestSpawnArgMoveKeepsWrapperAsTheSingleFreeSite(t *testing.T) {
	mod := lowerForTest(t, spawnArgSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	const drop = "@__nolang_drop_holder"
	if strings.Contains(irFunc(ir, "_nolang_main"), drop) {
		t.Errorf("main still frees the moved struct; the wrapper is supposed to be the single free site:\n%s",
			irFunc(ir, "_nolang_main"))
	}
	wrapper := irWrapper(ir)
	if wrapper == "" {
		t.Fatal("no async_wrapper.N emitted")
	}
	if !strings.Contains(wrapper, drop) {
		t.Errorf("the task wrapper does not free the moved struct's payload; the move would leak:\n%s", wrapper)
	}
}

// TestSpawnArgMoveNotTakenWhenSourceIsLive is the negative control. Without it
// "always move" would also pass the test above — and would be the §2.3.2
// use-after-free, since the caller reassigns the source while the task still
// holds it.
func TestSpawnArgMoveNotTakenWhenSourceIsLive(t *testing.T) {
	mod := lowerForTest(t, spawnArgLiveSrc)
	arg := runArg(t, mod, "main")
	if mod.spawnArgMoves[arg] {
		t.Errorf("value %d was moved even though it is read after the spawn", arg)
	}
	if n := dropsOf(mod, "main", arg); n != 1 {
		t.Errorf("main has %d OpDrop of the still-live argument %d, want exactly 1", n, arg)
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(irFunc(ir, "_nolang_main"), "@str_clone") {
		t.Error("the spawn-boundary deep copy is missing for a still-live argument")
	}
}

// TestSpawnArgMoveNotTakenForScalarArg pins that the move is gated on ownership,
// not on "the source happens to be dead". An i64 argument owns nothing, so there
// is no drop to suppress and nothing to transfer.
func TestSpawnArgMoveNotTakenForScalarArg(t *testing.T) {
	mod := lowerForTest(t, `dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    x i64 = 21
    t = run dbl(x)
    print(awy t)
}
`)
	if len(mod.spawnArgMoves) != 0 {
		t.Errorf("spawnArgMoves = %v, want empty for a scalar argument", mod.spawnArgMoves)
	}
}

// TestSpawnArgMoveOnlyWhenWrapperFreesPayload pins the SOUNDNESS precondition of
// the move: the wrapper only frees the payload for a callee parameter that
// SpawnArgClasses classifies as str/vec/struct. If a value were moved for a
// parameter of any other class, the wrapper would free only the container and
// the moved payload would leak.
//
// The class is read through the SAME shared predicate codegen uses — the point
// of Module.SpawnArgClasses — so this asserts the cross-layer agreement rather
// than a second copy of the rule.
func TestSpawnArgMoveOnlyWhenWrapperFreesPayload(t *testing.T) {
	mod := lowerForTest(t, `echo-async = (s str) (r str) {
    r = s
}

first-async = (v []i64) (r i64) {
    #{index-out=0}
    r = v[0]
}

dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

holder { s str }

echo-holder-async = (h holder) (r str) {
    r = h.s
}

main = () {
    s str = 'hello world'
    a []i64 = [7, 8, 9]
    h holder = holder { s: 'nested' }
    t1 = run echo-async(s)
    t2 = run first-async(a)
    t3 = run echo-holder-async(h)
    t4 = run dbl(21)
    print(awy t1)
    print(awy t2)
    print(awy t3)
    print(awy t4)
}
`)
	f := mod.Func(mod.FuncByName["main"])
	moved := 0
	for _, b := range f.Blocks {
		blk := mod.Block(b)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpRun || inst.Sym == "" {
				continue
			}
			cid, ok := mod.FuncByName[inst.Sym]
			if !ok {
				t.Fatalf("spawn callee %q not found", inst.Sym)
			}
			classes, _, _ := mod.SpawnArgClasses(mod.Func(cid))
			for i, a := range inst.Args {
				if a <= NoVal || !mod.spawnArgMoves[a] {
					continue
				}
				moved++
				if i >= len(classes) || classes[i] == "" {
					t.Errorf("moved value %d (arg %d of %s) has class %q; the wrapper would not free its payload",
						a, i, inst.Sym, classes[i])
				}
			}
		}
	}
	// The shape must actually exercise the move, or the assertion above is
	// vacuous: s, a and h are all dead after their spawn.
	if moved != 3 {
		t.Errorf("moved args = %d, want 3 (str, []i64 and struct); the corpus shape no longer exercises the move", moved)
	}
}

// TestSpawnArgClassesContract pins the shared predicate's answers directly, so a
// change to codegen's classification cannot silently change which arguments the
// analysis is allowed to move.
func TestSpawnArgClassesContract(t *testing.T) {
	// Every callee must be REACHABLE: lowering is worklist-driven from `main`, so
	// a function nobody calls is not in the module at all and FuncByName misses
	// it. That is why this source has a `main` that spawns all of them.
	mod := lowerForTest(t, `echo-async = (s str) (r str) {
    r = s
}

first-async = (v []i64) (r i64) {
    #{index-out=0}
    r = v[0]
}

dbl = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

holder { s str }

plain { n i64 }

echo-holder-async = (h holder) (r str) {
    r = h.s
}

echo-plain-async = (p plain) (r i64) {
    r = p.n
}

main = () {
    s str = 'hello world'
    a []i64 = [7, 8, 9]
    h holder = holder { s: 'nested' }
    p plain = plain { n: 1 }
    t1 = run echo-async(s)
    t2 = run first-async(a)
    t3 = run echo-holder-async(h)
    t4 = run echo-plain-async(p)
    t5 = run dbl(21)
    print(awy t1)
    print(awy t2)
    print(awy t3)
    print(awy t4)
    print(awy t5)
}
`)
	for _, tc := range []struct {
		callee string
		want   string
	}{
		{"echo-async", "str"},
		{"first-async", "vec"},
		{"dbl", ""},
		// A struct that owns heap through an inline `str` leaf.
		{"echo-holder-async", "struct"},
		// A struct that owns nothing: a bitwise copy IS a value copy, so it is
		// neither cloned nor freed and must not be classified.
		{"echo-plain-async", ""},
	} {
		cid, ok := mod.FuncByName[tc.callee]
		if !ok {
			t.Fatalf("callee %q not found", tc.callee)
		}
		classes, _, _ := mod.SpawnArgClasses(mod.Func(cid))
		if len(classes) != 1 {
			t.Fatalf("%s: got %d classes, want 1", tc.callee, len(classes))
		}
		if classes[0] != tc.want {
			t.Errorf("SpawnArgClasses(%s)[0] = %q, want %q", tc.callee, classes[0], tc.want)
		}
	}
}
