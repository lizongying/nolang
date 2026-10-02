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
