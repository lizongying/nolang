package mir

import "testing"

// These tests pin the ownership of an option→slice PEEL: `s []i64 = o` where
// `o : ?[]i64`.
//
// WHY THE ASSERTION IS ON THE MIR DROP, NOT ON STDOUT
// ---------------------------------------------------
// The bug was a LEAK, and a leak cannot be seen in stdout at all: the program
// prints the right thing and exits 0 either way. The observable is whether the
// peeled destination is dropped, which is exactly the decision the analysis
// makes (isBorrowRead's OpMove branch).
//
// THE BUG
// -------
// emitMove's peel branch CLONES a %vec payload with vecDeepClone as soon as the
// element type resolves, so the destination owns a private buffer. But
// isBorrowRead still treated every option→slice peel as a BORROW and suppressed
// the destination's drop — correct under the older premise that the peel merely
// copied the %vec triple and aliased the option's backing store, stale once the
// clone was added.
//
// Measured on the pre-fix compiler, fresh-binding spelling `s []i64 = o`:
//
//	move  dst=10:[]i64(owned=true) args=[9]     <- the peel
//	drop  args=[9] [12] [11] [14]              <- value 10 is NEVER dropped
//
// while the assignment spelling `s = o` did drop its destination (value 11).
// The two spellings of the same program therefore disagreed, and one of them
// leaked the clone on every execution.
//
// The fix is analysis.optionSlicePeelClones, which mirrors codegen's clone
// predicate; the exemption now applies only when codegen could not clone.

// peelDst reports whether func `name` contains an option→slice peel (a move-like
// instruction whose source is option-typed and whose destination is slice-typed)
// and whether that destination is dropped.
func peelDst(t *testing.T, mod *Module, name string) (found, dropped bool) {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}

	var dst ValueID = NoVal
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || !moveLike(inst) {
				continue
			}
			src, d := moveSrc(inst), moveDst(inst)
			if src <= NoVal || d <= NoVal {
				continue
			}
			st := mod.valueTypeOf(f, src)
			dt := mod.valueTypeOf(f, d)
			if st != nil && st.Kind == KindOption && dt != nil && dt.Kind == KindSlice {
				found = true
				dst = d
			}
		}
	}
	if dst <= NoVal {
		return found, false
	}
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpDrop || len(inst.Args) == 0 {
				continue
			}
			if inst.Args[0] == dst {
				dropped = true
			}
		}
	}
	return found, dropped
}

// The fresh-binding spelling `s []i64 = o` lowers to `move dst=s args=[o]`.
// Pre-fix this peel's destination was never dropped: the clone leaked.
func TestOptionSlicePeelFreshBindingDropsClone(t *testing.T) {
	mod := lowerForTest(t, `main = () {
    o ?[]i64 = ok([1, 2, 3])
    s []i64 = o
    print(s.len().to-str())
}
`)
	found, dropped := peelDst(t, mod, "main")
	if !found {
		t.Fatalf("no option→slice peel found in the lowered module:\n%s", mod.String())
	}
	if !dropped {
		t.Fatalf("the option→slice peel's destination is never dropped — codegen clones the "+
			"payload with vecDeepClone, so the destination owns a private buffer and dropping "+
			"it is what frees that clone. Suppressing the drop leaks one buffer per execution.\n%s",
			mod.String())
	}
}

// The assignment spelling `s = o` lowers to `move dst=0 args=[o s]`, which never
// reached isBorrowRead (its call sites require inst.Dst > NoVal), so it always
// dropped correctly. Pinning it here states the invariant the two spellings must
// share: both own, both drop.
func TestOptionSlicePeelAssignmentDropsClone(t *testing.T) {
	mod := lowerForTest(t, `main = () {
    o ?[]i64 = ok([1, 2, 3])
    s []i64 = [0]
    s = o
    print(s.len().to-str())
}
`)
	found, dropped := peelDst(t, mod, "main")
	if !found {
		t.Fatalf("no option→slice peel found in the lowered module:\n%s", mod.String())
	}
	if !dropped {
		t.Fatalf("the assignment-shaped peel's destination is not dropped:\n%s", mod.String())
	}
}

// The two spellings of the same program must make the same ownership decision.
// Asserting equality states the invariant directly, so a future change that
// fixes one spelling and breaks the other fails here rather than silently
// reintroducing the leak on one side.
func TestOptionSlicePeelSpellingsAgreeOnDrop(t *testing.T) {
	fresh := lowerForTest(t, `main = () {
    o ?[]i64 = ok([1, 2, 3])
    s []i64 = o
    print(s.len().to-str())
}
`)
	assign := lowerForTest(t, `main = () {
    o ?[]i64 = ok([1, 2, 3])
    s []i64 = [0]
    s = o
    print(s.len().to-str())
}
`)
	_, fd := peelDst(t, fresh, "main")
	_, ad := peelDst(t, assign, "main")
	if fd != ad {
		t.Fatalf("the two peel spellings disagree on ownership: fresh-binding dropped=%v, "+
			"assignment dropped=%v — one of them leaks (or double-frees) the payload", fd, ad)
	}
}
