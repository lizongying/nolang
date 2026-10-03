package mir

import (
	"strings"
	"testing"
)

// These tests pin OpRelease — the shallow give-back that pairs with the
// OpRetain insertBorrowRetains emits for a borrow read of an owned slice.
//
// Both failure modes are covered, and they need different observables:
//
//   - a MISSING release is invisible in stdout. The program still prints the
//     right answer; it just leaks, because @nolang_free decrements the header
//     refcount and frees only at zero. It shows up only in the op counts.
//   - a release that lowers to the DEEP free is a double free: a borrow does
//     not own its buffer's elements, so freeing them frees buffers the
//     container still owns. That shows up in the emitted IR, and at runtime
//     only as a crash.

// borrowReleaseSrc is the canonical shape the release exists for: a borrow read
// of a slice element (`b0 = b[0]`) whose value is then GROWN. The grow releases
// the buffer it replaces, so the borrow must have retained it first — otherwise
// the grow frees a buffer `b` still points at (measured: tests/nested-vec-test.no
// aborts 20/20 without the retain). And the retain must be given back when b0
// dies, or the buffer is pinned forever.
const borrowReleaseSrc = `
mk = () (v []i64) {
    v.push(1)
    v.push(2)
}

main = () {
    a [][]i64
    a.push(mk())
    b = a
    b0 = b[0]
    b0.push(100)
    print(b0.len())
}
`

// deepBorrowReleaseSrc borrows an element whose ELEMENT TYPE owns heap: `x` is
// a [][]i64, so vecElemNeedsDeepFree classifies it as deep and an OpDrop of it
// would be routed to the deep-free helper. A borrow does not own its elements,
// so the release has to stay shallow.
//
// `probe` takes the container as a PARAMETER, which is never dropped by the
// callee, so the release is the only disposal in that function and the IR
// assertion is unambiguous (main still deep-frees its own `a`, which is why the
// check is scoped to @probe rather than the whole module).
const deepBorrowReleaseSrc = `
mk2 = () (v [][]i64) {
    inner []i64
    inner.push(1)
    v.push(inner)
}

probe = (a [][][]i64) (n i64) {
    x = a[0]
    n = x.len()
}

main = () {
    a [][][]i64
    a.push(mk2())
    print(probe(a))
}
`

// retainPairingSrc combines a retained borrow with the shape that made an
// earlier version of the release pass emit a release with NO retain: a tagged
// enum's slice payload extracted as a borrow (OpEnumField) and matched twice.
//
// The seed set was originally collected by re-evaluating borrowRetained, whose
// answer depends on enumOwnsPayload — a map that markEnumPayloadOwners and
// insertDrops' moveSrc loop MUTATE after the retain pass has already run. The
// re-evaluation therefore did not reproduce the retain set, and the difference
// is not benign: it produced a bare `release args=[76]` with no retain anywhere
// in the file, and tests/tagged-enum-two-match.no segfaulted 20/20 (rc 139).
// Reading the seeds back from the OpRetain instructions actually in the stream
// is what makes the pairing structural.
//
// The first half is here so the vacuity guard below has something to find: the
// enum shape alone emits no retain at all, and a pairing check over a module
// with no retains would pass no matter how broken the pass was.
const retainPairingSrc = `
mk = () (v []i64) {
    v.push(1)
    v.push(2)
}

list-res {
    ok(items []str),
    fail,
}

main = () {
    a [][]i64
    a.push(mk())
    b = a
    b0 = b[0]
    b0.push(100)
    print(b0.len())

    l4 = ['p', 'q']
    q4 list-res = ok(l4)
    q4: {
        ok(items) -> {
            for x <- items: {
                print('4a: ' - x)
            }
        }

        fail -> print('fail 4a')
    }
    q4: {
        ok(items) -> {
            for x <- items: {
                print('4b: ' - x)
            }
        }

        fail -> print('fail 4b')
    }
}
`

// moveIntoSlotSrc is the shape that needs the closure over transferring moves —
// and the one a naive reading of the corpus misses, because it is an assignment
// into an EXISTING slot rather than to a fresh variable. `out = c.data` on a
// result parameter lowers to the two-operand EmitMoveInto encoding
// `move dst=0 args=[src dst]`, so the destination lives in Args[1] and has no
// Dst of its own.
//
// Measured MIR: `getfield dst=24 -> retain args=[24] -> move dst=0 args=[24 23]
// -> release args=[23]`. The release belongs to 23, not 24. Three corpus files
// exercise this (mem-safety/struct-field-{move-test,shallow-copy-bug,uaf-bug}.no);
// dropping the closure silently loses exactly those three releases.
const moveIntoSlotSrc = `
container {
    data []i64
    name str
}

extract-data = (c container) (out []i64) {
    out = c.data
}

main = () {
    c container
    c.data = [1, 2, 3, 4, 5]
    c.name = 'test'
    d = extract-data(c)
    print(d.len())
}
`

// opCountsInFunc counts instructions by op in the named function.
func opCountsInFunc(t *testing.T, mod *Module, name string) map[Op]int {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in the lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) is not resolvable", name, fid)
	}
	counts := map[Op]int{}
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			if inst := mod.Inst(iid); inst != nil {
				counts[inst.Op]++
			}
		}
	}
	return counts
}

// TestBorrowReadEmitsPairedRelease pins the pairing itself: a borrow read of an
// owned slice retains, and the value's death gives that reference back.
func TestBorrowReadEmitsPairedRelease(t *testing.T) {
	mod := lowerForTest(t, borrowReleaseSrc)
	c := opCountsInFunc(t, mod, "main")
	if c[OpRetain] == 0 {
		t.Fatalf("a borrow read of an owned slice was not retained:\n%s", mod.DumpAnnotated())
	}
	if c[OpRelease] != c[OpRetain] {
		t.Errorf("retain/release are unpaired in main: %d retain vs %d release — "+
			"an unmatched retain pins the buffer forever (@nolang_free frees only at zero)\n%s",
			c[OpRetain], c[OpRelease], mod.DumpAnnotated())
	}
}

// TestReleaseIsShallowForNestedElementType pins the reason OpRelease is a
// separate op from OpDrop. For a value whose element type owns heap, emitDrop
// selects the deep free; the release must not.
func TestReleaseIsShallowForNestedElementType(t *testing.T) {
	mod := lowerForTest(t, deepBorrowReleaseSrc)
	c := opCountsInFunc(t, mod, "probe")
	if c[OpRetain] != 1 || c[OpRelease] != 1 {
		t.Fatalf("probe should retain once and release once, got %d retain / %d release\n%s",
			c[OpRetain], c[OpRelease], mod.DumpAnnotated())
	}

	ir := mustEmit(t, deepBorrowReleaseSrc)
	body := irFunc(ir, "probe")
	if body == "" {
		t.Fatalf("could not locate @probe in the emitted IR")
	}
	if !strings.Contains(body, "call void @vec_free(") {
		t.Errorf("the borrow release does not lower to the shallow @vec_free:\n%s", body)
	}
	if strings.Contains(body, "@__nolang_vec_free_") {
		t.Errorf("the borrow release was routed through the DEEP free. A borrow does not own "+
			"its elements, so this frees buffers the container still owns:\n%s", body)
	}
}

// TestMovedBorrowReferenceIsReleasedOnTheDestination pins the closure over
// transferring moves: the obligation follows the descriptor into the slot. The
// release must name the DESTINATION, not the borrow value — the borrow's
// reference left with the descriptor, so releasing the source too would give
// back a reference nobody holds.
func TestMovedBorrowReferenceIsReleasedOnTheDestination(t *testing.T) {
	mod := lowerForTest(t, moveIntoSlotSrc)
	c := opCountsInFunc(t, mod, "extract-data")
	if c[OpRetain] != 1 {
		t.Fatalf("expected exactly one retain in extract-data, got %d\n%s", c[OpRetain], mod.DumpAnnotated())
	}
	if c[OpRelease] != 1 {
		t.Errorf("the move did not carry the release to its destination: %d release, %d drop\n%s",
			c[OpRelease], c[OpDrop], mod.DumpAnnotated())
	}

	fid, ok := mod.FuncByName["extract-data"]
	if !ok {
		t.Fatalf("extract-data not found in the lowered module")
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("extract-data (id %d) is not resolvable", fid)
	}
	retainV, releaseV := NoVal, NoVal
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || len(inst.Args) == 0 {
				continue
			}
			switch inst.Op {
			case OpRetain:
				retainV = inst.Args[0]
			case OpRelease:
				releaseV = inst.Args[0]
			}
		}
	}
	if retainV == NoVal || releaseV == NoVal {
		t.Fatalf("retain and release are not both present\n%s", mod.DumpAnnotated())
	}
	if retainV == releaseV {
		t.Errorf("the release still names the borrow value %d; the EmitMoveInto encoding moved the "+
			"reference to the slot, so the release must name the destination", retainV)
	}
}

// borrowInLoopSrc is the shape NOLANG-OWNERSHIP-MODEL.md §4.3 named as a leak:
// a borrow read of a slice element INSIDE a loop, whose reference is handed to
// the option-wrap constructor and then peeled back out into the loop-carried
// accumulator `x`.
//
// §4.3 claimed the peeled value's drop is placed by (conservative) liveness and
// "in a loop gets hoisted out of the loop => one block leaked per iteration".
// Measured on this tree it is NOT: the option's drop lowers to the refcount-aware
// deep free (rc 2 -> 1) and the accumulator's rebind drop runs inside the loop
// (rc 1 -> 0), so the buffer is freed once per iteration. See the test below and
// the runtime battery recorded in §4.3.
const borrowInLoopSrc = `
mk = () (v []i64) {
    v.push(1)
    v.push(2)
}

main = () {
    a [][]i64
    a.push(mk())
    n = 0
    i <- [0..4) {
        #{index-out=zero}
        x = a[0]
        #{overflow=wrap}
        n = n + x.len()
    }
    print(n)
}
`

// borrowInLoopNoAccumulatorSrc is the NEGATIVE CONTROL for the test below: the
// same borrow-in-a-loop, but nothing carries the peel result across the
// back-edge. It still retains once and still option-wraps once, yet no value is
// disposed BOTH inside and outside the loop — which is exactly what makes the
// assertion discriminating rather than vacuous.
const borrowInLoopNoAccumulatorSrc = `
mk = () (v []i64) {
    v.push(1)
    v.push(2)
}

main = () {
    a [][]i64
    a.push(mk())
    i <- [0..4) {
        print(a[0].len())
    }
}
`

// loopHeader returns the block that can reach one of its own predecessors via a
// back-edge — the same predicate insertDrops uses to keep header drops safe.
func loopHeader(mod *Module, f *Function) BlockID {
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, p := range blk.Preds {
			if mod.blockReaches(bid, p) {
				return bid
			}
		}
	}
	return NoBlock
}

// TestBorrowInLoopIsDisposedEveryIteration pins the geometry that makes a borrow
// read inside a loop a no-op instead of a leak.
//
// A retained borrow raises the header refcount to 2, so the buffer is freed only
// when BOTH disposals happen: the option's (refcount-aware) deep free, and the
// drop of the peeled value that is carried across the back-edge. If the latter
// were hoisted out of the loop, the accumulator would be overwritten every
// iteration without its old buffer ever reaching rc 0 — one leaked block per
// iteration, exactly the §4.3 claim.
//
// The observable is therefore: some value must be disposed BOTH inside the loop
// and outside it (the per-iteration rebind drop, plus the final one after the
// loop exits). The negative control shows that is a real property and not
// something every borrow shape satisfies.
func TestBorrowInLoopIsDisposedEveryIteration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		src        string
		minWrap    int
		wantInside bool
	}{
		{"loop-carried accumulator", borrowInLoopSrc, 1, true},
		{"no accumulator (control)", borrowInLoopNoAccumulatorSrc, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := lowerForTest(t, tc.src)
			fid, ok := mod.FuncByName["main"]
			if !ok {
				t.Fatalf("main not found in the lowered module")
			}
			f := mod.Func(fid)
			if f == nil {
				t.Fatalf("main (id %d) is not resolvable", fid)
			}
			header := loopHeader(mod, f)
			if header == NoBlock {
				t.Fatalf("the probe no longer contains a loop:\n%s", mod.DumpAnnotated())
			}
			retain, wrap := 0, 0
			inside := map[ValueID]bool{}
			outside := map[ValueID]bool{}
			for _, bid := range f.Blocks {
				blk := mod.Block(bid)
				if blk == nil {
					continue
				}
				for _, iid := range blk.Insts {
					inst := mod.Inst(iid)
					if inst == nil {
						continue
					}
					switch inst.Op {
					case OpRetain:
						retain++
					case OpOptionWrap:
						wrap++
					case OpDrop, OpRelease:
						if len(inst.Args) == 0 || inst.Args[0] <= NoVal {
							continue
						}
						if mod.blockReaches(bid, header) {
							inside[inst.Args[0]] = true
						} else {
							outside[inst.Args[0]] = true
						}
					}
				}
			}
			// Vacuity guard: without the retain the assertion below says nothing
			// about the borrow path, and the accumulator case must actually
			// exercise the option-wrap constructor it is about.
			if retain != 1 || wrap < tc.minWrap {
				t.Fatalf("expected exactly one borrow retain and at least %d option-wrap, got %d / %d:\n%s",
					tc.minWrap, retain, wrap, mod.DumpAnnotated())
			}
			both := false
			for v := range inside {
				if outside[v] {
					both = true
				}
			}
			if both != tc.wantInside {
				t.Errorf("a value disposed both inside and outside the loop = %v, want %v — "+
					"the loop-carried peel result must be disposed on EVERY iteration "+
					"(otherwise each iteration leaks one block; §4.3):\n%s",
					both, tc.wantInside, mod.DumpAnnotated())
			}
		})
	}
}

// TestNoReleaseWithoutARetain is the module-wide invariant, and the pin for the
// bug that motivated it: a release whose value was never retained is an
// underflow on a count the container still owns. Checked over every function
// because the offending shape (an enum payload borrow) is not in main alone.
func TestNoReleaseWithoutARetain(t *testing.T) {
	mod := lowerForTest(t, retainPairingSrc)
	total := 0
	for i := range mod.Funcs {
		f := &mod.Funcs[i]
		retained := map[ValueID]int{}
		released := map[ValueID]int{}
		for _, bid := range f.Blocks {
			blk := mod.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := mod.Inst(iid)
				if inst == nil || len(inst.Args) == 0 || inst.Args[0] <= NoVal {
					continue
				}
				switch inst.Op {
				case OpRetain:
					retained[inst.Args[0]]++
					total++
				case OpRelease:
					released[inst.Args[0]]++
				}
			}
		}
		for v, n := range released {
			if retained[v] < n {
				t.Errorf("%s: value %d is released %d time(s) but retained %d — a release "+
					"without a retain gives back a reference nobody took",
					f.Name, v, n, retained[v])
			}
		}
	}
	if total == 0 {
		t.Fatalf("no OpRetain anywhere: the source no longer exercises the borrow path, "+
			"so this test would pass vacuously:\n%s", mod.DumpAnnotated())
	}
}
