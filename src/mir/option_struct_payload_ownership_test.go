package mir

import "testing"

// These tests pin the ownership of an INLINE owning STRUCT option payload —
// `?person` (an owned `str` leaf), `?json` (a pointer field to a pool).
//
// WHY THE ASSERTION IS ON THE MIR DROP, NOT ON STDOUT
// ---------------------------------------------------
// The defect was a LEAK: the program prints the right thing and exits 0 either
// way, so no `tests/*.no` can pin it (measured: the loop probes print
// `5|5|5|` on both the leaking and the fixed compiler). The observable is
// whether the OPTION is dropped, which is the decision the analysis makes.
//
// THE BUG (two independent causes; fixing either one alone changes nothing)
// ------------------------------------------------------------------------
// (1) the predicate: OptionOwnsHeap / typeOwnsHeap answered false for a struct
//     payload (ClassifyOwnership does not claim structs), and emitOptionDrop's
//     inline `default:` branch admitted only StructHasOwnedLeafFields, not
//     StructHasPtrFields — so `?json` (pool pointee) had no drop AND the drop
//     did not recognise it.
// (2) the suppression — the DOMINANT cause: the option PEEL `q = ip` was
//     mistaken for a heap-sharing struct copy by moveStructSharesHeap. Its
//     typeIsPtrStruct / typeIsLeafStruct tests ask about the DESTINATION, which
//     for a peel IS the payload's own struct type, so moveSrc[ip] was set and
//     the option's drop was suppressed entirely.
//
// Measured on the pre-fix compiler with the 50k `?json` loop probe:
//
//	40,697,856 B  (pre-fix)   ->   5,373,952 B  (post-fix)
//
// and for `?person` at 800k: 27,443,200 B -> 1,720,320 B (flat).

// findOptionPeel returns the function and the first move-like instruction whose
// source is option-typed and whose destination is NOT (the `x = opt` peel).
func findOptionPeel(t *testing.T, mod *Module, name string) (*Function, *Inst) {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
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
			if st != nil && st.Kind == KindOption && dt != nil && dt.Kind != KindOption {
				return f, inst
			}
		}
	}
	t.Fatalf("no option peel (`x = opt`) found in the lowered module:\n%s", mod.String())
	return nil, nil
}

// optionTypedDrops counts OpDrop instructions in func `name` whose operand is
// option-typed. The invariant is "the option is dropped at all"; WHICH value id
// carries it is an implementation detail, so the count is not pinned to 1.
func optionTypedDrops(t *testing.T, mod *Module, name string) int {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
	n := 0
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
			if ty := mod.valueTypeOf(f, inst.Args[0]); ty != nil && ty.Kind == KindOption {
				n++
			}
		}
	}
	return n
}

// personStructSrc is the smallest inline owning struct payload: one owned `str`
// leaf, 24 bytes, so it fits the inline option slot (no boxing) — the shape the
// predicate used to miss.
const personStructSrc = `person { name str }
main = () {
    p person
    p.name = 'x'
    ip ?person = p
    print(ip.name.len().to-str())
}
`

// The predicate half. An inline struct payload with an owned leaf IS freed by
// emitOptionPayloadContentFree's struct arm, so the option owns heap and must
// be dropped. Pre-fix OptionOwnsHeap answered false here.
func TestOptionStructPayloadOwnsHeap(t *testing.T) {
	mod := lowerForTest(t, personStructSrc)
	if !mod.OptionOwnsHeap("?person") {
		t.Fatalf("OptionOwnsHeap(\"?person\") is false, but emitOptionPayloadContentFree "+
			"frees an inline struct payload's owned leaf through its struct arm — so the "+
			"option DOES own heap and needs a drop. Without one every rebind leaks the "+
			"payload.\n%s", mod.String())
	}
}

// An INLINE POD struct payload (16 bytes: two i64, no owned leaf, no pointer
// field) must NOT be claimed — it owns nothing, so a drop would be pure
// overhead. Negative control for the test above: it stops "claim every struct"
// from passing.
//
// ⚠️ The payload must stay under the inline slot threshold (24 B). A larger POD
// (`big { pad [32]i64 }`, 256 B) is BOXED, and a boxed payload correctly owns
// its box — so it is the wrong control and it passes for the wrong reason.
func TestOptionPodStructPayloadDoesNotOwnHeap(t *testing.T) {
	mod := lowerForTest(t, `small { a i64 b i64 }
main = () {
    s small
    is ?small = s
    print(is.a.to-str())
}
`)
	if mod.OptionOwnsHeap("?small") {
		t.Fatalf("OptionOwnsHeap(\"?small\") is true, but an inline POD struct payload "+
			"owns no heap — claiming it emits a drop that frees nothing.\n%s", mod.String())
	}
}

// The suppression half — the DOMINANT cause. A peel already gives its
// destination its own payload (cloneOptionPayloadInto) and the option keeps its
// drop, so claiming the peel here marks the OPTION as a move source and
// suppresses exactly that drop.
func TestOptionStructPayloadPeelIsNotAStructCopy(t *testing.T) {
	mod := lowerForTest(t, `person { name str }
main = () {
    p person
    p.name = 'x'
    ip ?person = p
    q person = ip
    print(q.name.len().to-str())
}
`)
	f, inst := findOptionPeel(t, mod, "main")
	if mod.moveStructSharesHeap(f, inst) {
		t.Fatalf("moveStructSharesHeap reports the option peel `q = ip` as a heap-sharing "+
			"struct copy. emitMove's peel branch already gives q its own payload and the "+
			"option keeps its own drop (isOptionPeelMove), so claiming the peel sets "+
			"moveSrc[ip] and suppresses that drop — one leaked payload per execution.\n%s",
			mod.String())
	}
}

// The integration half: with the peel present, the option must still be dropped.
// This is the shape the loop probes exercise, and the property that was false
// pre-fix (measured as an unbounded RSS slope).
func TestOptionStructPayloadPeelKeepsOptionDrop(t *testing.T) {
	mod := lowerForTest(t, `person { name str }
main = () {
    p person
    p.name = 'x'
    ip ?person = p
    q person = ip
    print(q.name.len().to-str())
}
`)
	if n := optionTypedDrops(t, mod, "main"); n < 1 {
		t.Fatalf("no option-typed value is dropped: the peel `q = ip` suppressed the "+
			"option's drop, so its payload is freed by nothing. Non-vacuity guard: the "+
			"same count is >= 1 once the peel guard is in place.\n%s", mod.String())
	}
}
