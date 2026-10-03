package mir

import "testing"

// These tests pin the slice-VIEW lifetime rule.
//
// WHAT THE BUG WAS. `b = a[i..j]` on a `[]T` produces a VIEW: `b.cap == 0` and
// `b.data == a.data + i*8`, i.e. it aliases `a`'s backing buffer. Liveness saw
// only `b`, never `a`, so `a`'s drop was placed at `a`'s own last use — BEFORE
// the view's — and every later read through `b` hit freed memory:
//
//     a = [10, 20, 30, 40, 50]
//     b = a[2..4]
//     print(b)              ; printed heap addresses (e.g. [4341880592, ...])
//
// The fix: `Module.viewSrc` (built in Analyze from the surviving SliceFlagView
// ops) maps a view to its source, and `defUse` counts a view's use as a use of
// that source — which extends the source's live range to the view's last use.
//
// WHY THE ASSERTION IS ON THE MIR, NOT STDOUT. Stdout is what the end-to-end
// test (tests/slice-view-liveness.no) pins; here the load-bearing property is
// the ORDER of the drop relative to the view's last use, which stdout cannot
// show (a use-after-free of a just-freed small buffer often returns the old
// bytes, i.e. the "right" answer, by luck).

// viewTestSrc is the minimal reproducer: `a` is dead by its own last use, but
// `b` (a view of `a`) is used afterwards.
const viewTestSrc = `main = () {
    a []i64 = [10, 20, 30, 40, 50]
    b = a[2..4]
    print(b)
}
`

// findView returns the dst and source ValueIDs of the first surviving slice view.
func findView(t *testing.T, mod *Module) (view, base ValueID) {
	t.Helper()
	for i := range mod.Insts {
		inst := &mod.Insts[i]
		if inst.Op == OpSliceOp && inst.Int&SliceFlagView != 0 && inst.Dst > NoVal && len(inst.Args) > 0 {
			return inst.Dst, inst.Args[0]
		}
	}
	return NoVal, NoVal
}

// linearOrder assigns a program-order index to every instruction of func `name`
// (block order, then intra-block order — sufficient for the straight-line
// shapes these tests use).
func linearOrder(mod *Module, name string) map[InstID]int {
	ord := map[InstID]int{}
	fid, ok := mod.FuncByName[name]
	if !ok {
		return ord
	}
	f := mod.Func(fid)
	if f == nil {
		return ord
	}
	n := 0
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			ord[iid] = n
			n++
		}
	}
	return ord
}

// TestSliceViewKeepsSourceAlive is the positive case: the source's drop must NOT
// precede the view's last use.
func TestSliceViewKeepsSourceAlive(t *testing.T) {
	mod := lowerForTest(t, viewTestSrc)
	view, base := findView(t, mod)
	if view == NoVal {
		t.Fatal("no surviving slice view found — the shape under test did not lower as expected")
	}
	if got := mod.viewSrc[view]; got != base {
		t.Fatalf("viewSrc[%d] = %d, want %d (the view's source)", view, got, base)
	}

	ord := linearOrder(mod, "main")
	dropPos, lastUse := -1, -1
	for i := range mod.Insts {
		inst := &mod.Insts[i]
		p, ok := ord[InstID(i)]
		if !ok {
			continue
		}
		if inst.Op == OpDrop && len(inst.Args) > 0 && inst.Args[0] == base {
			if p > dropPos {
				dropPos = p
			}
		}
		// A DROP is not a use. insertDrops fixes a drop's position from a
		// Go-map iteration, so the RELATIVE ORDER OF TWO DROPS is genuinely
		// nondeterministic — and counting `drop <view>` as a use of `<view>`
		// made this assertion compare exactly that (measured: 4 of 5 runs
		// failed, on `drop base` vs `drop view`). Only real reads pin
		// liveness, and those are emitted in program order.
		if inst.Op != OpDrop {
			for _, a := range inst.Args {
				if a == view && p > lastUse {
					lastUse = p
				}
			}
		}
	}
	if dropPos < 0 {
		t.Fatal("the source has no OpDrop at all — nothing to order against (test is vacuous)")
	}
	if lastUse < 0 {
		t.Fatal("the view is never used — test is vacuous")
	}
	if dropPos < lastUse {
		t.Fatalf("drop of the source at position %d precedes the view's last use at %d: the view reads a freed buffer", dropPos, lastUse)
	}
}

// TestViewSrcEmptyWithoutSlices is the non-vacuity control for the map itself: a
// program with no slices must produce no entries, so a bug that registered every
// value as a "view" would not pass unnoticed.
func TestViewSrcEmptyWithoutSlices(t *testing.T) {
	mod := lowerForTest(t, `main = () {
    a []i64 = [1, 2, 3]
    print(a[0])
}
`)
	if len(mod.viewSrc) != 0 {
		t.Fatalf("viewSrc has %d entries for a slice-free program: %v", len(mod.viewSrc), mod.viewSrc)
	}
}

// demotedViewSrc returns a slice of a local to the caller, which
// demoteUnsafeSliceViews must refuse (the view would outlive the frame), so its
// SliceFlagView bit is cleared and it must NOT appear in viewSrc.
const demotedViewSrc = `mk = () (r []i64) {
    a []i64 = [1, 2, 3, 4, 5]
    r = a[1..2]
}
main = () {
    x = mk()
    print(x[0])
}
`

// TestViewSrcMatchesSurvivingViewBit pins the exact invariant: an entry exists
// iff the SliceFlagView bit is still set. A stale entry would pin a source for a
// slice that owns its own buffer; a missing entry would re-open the
// use-after-free. Both inputs are checked so the test is non-vacuous whether or
// not demoteUnsafeSliceViews fired on the second one.
func TestViewSrcMatchesSurvivingViewBit(t *testing.T) {
	for _, src := range []string{viewTestSrc, demotedViewSrc} {
		mod := lowerForTest(t, src)
		checked := 0
		for i := range mod.Insts {
			inst := &mod.Insts[i]
			if inst.Op != OpSliceOp || inst.Dst <= NoVal {
				continue
			}
			checked++
			_, inMap := mod.viewSrc[inst.Dst]
			isView := inst.Int&SliceFlagView != 0
			if inMap != isView {
				t.Fatalf("viewSrc has entry for %d = %v but SliceFlagView bit = %v", inst.Dst, inMap, isView)
			}
		}
		if checked == 0 {
			t.Fatalf("no OpSliceOp lowered from %q — test would be vacuous", src)
		}
	}
}
