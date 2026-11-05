package mir

import "testing"

// blankDiscardSrc is a minimal reproducer of tests/uninit-output.no's SIGSEGV.
//
// `_ = expr` is an explicit DISCARD that consumes an option for the checker.
// MIR used to bind every discard to ONE local named `_`, typed by the FIRST
// discard. Later heterogeneous discards were lowered as EmitMoveInto rebinds
// into that same slot, so the end-of-scope drop helper was chosen by the FIRST
// payload's type. `?box` (32 B > the 24 B option slot) is heap-BOXED, so slot 0
// is a pointer and its drop calls free(); `?[]i64` stores the %vec INLINE, so
// slot 0 is a length. Dropping the latter with `__nolang_opt_drop_box` calls
// free(len) -> SIGSEGV after the program has already printed everything.
const blankDiscardSrc = `box {
    a i64
    b i64
    c i64
    d i64
}
fb = () (r ?box) {
}
fv = () (r ?[]i64) {
    r = [10, 20, 30]
}
a = fb()
b = fv()
_ = a
_ = b
print('done')
`

// blankDiscardOneSrc is the negative control: with only ONE discard there is
// nothing to type-pun, so exactly one option drop may be emitted. Without this
// the "two option drops" assertion could pass on an implementation that drops
// every value unconditionally.
const blankDiscardOneSrc = `box {
    a i64
    b i64
    c i64
    d i64
}
fb = () (r ?box) {
}
a = fb()
_ = a
print('done')
`

// blankDiscardOptionDrops returns the distinct option-typed values dropped in
// func `name`. Collapsing several discards into one `_` slot makes the later
// values invisible to the drop pass, so their count drops below the number of
// `_ =` statements even though each owned option still needs its own release.
func blankDiscardOptionDrops(t *testing.T, mod *Module, name string) map[ValueID]bool {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found in lowered module", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q (id %d) not resolvable", name, fid)
	}
	out := map[ValueID]bool{}
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
			ty := mod.valueTypeOf(f, inst.Args[0])
			if ty != nil && ty.Kind == KindOption {
				out[inst.Args[0]] = true
			}
		}
	}
	return out
}

func TestBlankDiscardsGetIndependentTypedDrops(t *testing.T) {
	mod := lowerForTest(t, blankDiscardSrc)
	got := blankDiscardOptionDrops(t, mod, "main")
	// Two `_ =` statements discard two DIFFERENT owned option payloads. Pre-fix
	// MIR was `%3 = move %1; move %3 <- %2; drop(%3)` — one slot, one drop.
	if len(got) != 2 {
		t.Errorf("want 2 distinct option drops (one per `_ =` discard), got %d (%v): "+
			"heterogeneous discards collapsed into one slot typed by the first value",
			len(got), got)
	}
}

func TestBlankDiscardSingleValueHasExactlyOneOptionDrop(t *testing.T) {
	mod := lowerForTest(t, blankDiscardOneSrc)
	got := blankDiscardOptionDrops(t, mod, "main")
	if len(got) != 1 {
		t.Errorf("want exactly 1 option drop for a single discard, got %d (%v)", len(got), got)
	}
}
