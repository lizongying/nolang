package mir

import (
	"regexp"
	"strings"
	"testing"
)

// These tests pin the rule that codegen's defInst projection must be RETRACTED
// as soon as the value it describes is defined a SECOND time.
//
// BACKGROUND — the bug this closes
// --------------------------------
// A MIR value id is a VARIABLE, not an SSA name. `v = ls[0]` defines `v` with an
// OpIndex; a later `v = ...` re-defines the SAME id, and the assignment lands as
// a `move` in the "store into an existing binding" encoding — Dst = NoVal with
// Args = [src, dst]. codegen's defInst builder only recorded `inst.Dst > NoVal`,
// so that move was invisible and defInst kept pointing at the OpIndex forever.
//
// lvalueAddrOf then projected every later use of `v` back onto the CONTAINER
// ELEMENT (`%ep<N> = getelementptr ..., %edp<M>, i64 %i`), so a method call on
// the rebound variable received `&ls[0]` instead of `v`'s own slot. The write
// half of such a call goes to its out-param, so `ls[0]` itself never changed —
// and the read half, e.g. `v.len()`, read the STALE element:
//
//	lines = ['  a  ']
//	line  = lines[0]
//	line  = line.trim()
//	line.len()      ; 5 — or 0, once `lines` has been dropped — instead of 1
//
// The reported symptom was exactly that shape: content correct, length field
// wrong, including `len == 0`. The same hole swallowed the write direction:
// `v = ls[0]; v = ls[1]; v.len()` read ls[0].
//
// WHY THE ASSERTION IS ON THE EMITTED IR, NOT ON STDOUT
// -----------------------------------------------------
// Once the container's drop has been placed, the stale read is a use-after-free,
// so stdout is a property of the allocator rather than of the bug: the same
// program printed 5 in one file and 0 in another. Only the receiver operand is
// deterministic — either the value's own slot (`%v<N>.s`) or a projection into
// the container (`%ep<N>`).
//
// NAMING: elemAddr names its element GEPs `%ep<N>` (lvalueAddrOf names field
// GEPs `%lvg<N>`); value slots are `%v<N>.s`.
//
// WHY THE PROBE DECLARES ITS OWN `str.plen` INSTEAD OF USING A REAL STD METHOD
// ----------------------------------------------------------------------------
// lowerForTest lowers a single source with no std in scope, so a real std method
// is "unknown callee" at EmitLLVM time. Declaring a one-line method on `str`
// gives the same shape — receiver is a rebound view — with no std dependency.

const rebindElementSrc = `str.plen = () (r i64) {
    r = 1
}

probe = () (r i64) {
    ls []str = ['  a  ']
    v = ls[0]
    v = 'b'
    r = v.plen()
}

main = () {
    print(probe())
}
`

const rebindFieldSrc = `hold { f str }

str.plen = () (r i64) {
    r = 1
}

probe = () (r i64) {
    h = hold { f: 'ab' }
    v = h.f
    v = 'b'
    r = v.plen()
}

main = () {
    print(probe())
}
`

// singleDefElementSrc is the scope guard: the receiver is a container element
// read that is NEVER rebound, so it must STILL project. That projection is what
// makes `rows[0].set-byte(0, 90)` write the element instead of a copy
// (tests/set-byte-receiver-writeback.no case 4), and retraction must therefore
// key on a SECOND definition, not on "an element read happened".
const singleDefElementSrc = `str.pset = () (r i64) {
    r = 1
}

probe = () (r i64) {
    rows []str = ['x', 'y']
    r = rows[0].pset()
}

main = () {
    print(probe())
}
`

func rebindIR(t *testing.T, src string) string {
	t.Helper()
	mod := lowerForTest(t, src)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	return ir
}

// receiverOf returns the operand the generated call to `fn` receives, and fails
// the test if the probe shape did not lower (which would let every assertion
// here pass vacuously).
func receiverOf(t *testing.T, ir, fn string) string {
	t.Helper()
	re := regexp.MustCompile(`call void @` + fn + `\(%str-long\*\s+(%[^,]+),`)
	call := re.FindStringSubmatch(ir)
	if call == nil {
		t.Fatalf("no @%s call in the emitted IR — the probe shape did not lower as "+
			"expected; the test would pass vacuously.\nIR:\n%s", fn, ir)
	}
	return call[1]
}

var rebindValueSlotRe = regexp.MustCompile(`^%v\d+\.s$`)

// TestReboundElementViewIsNotProjected is the regression pin for the container
// element shape.
//
// On the unfixed compiler the receiver is an `%ep<N>` GEP into the slice's data
// buffer, i.e. the element the rebinding left behind.
func TestReboundElementViewIsNotProjected(t *testing.T) {
	ir := rebindIR(t, rebindElementSrc)
	if got := receiverOf(t, ir, "str_plen"); !rebindValueSlotRe.MatchString(got) {
		t.Errorf("@str_plen receiver = %s, want the value's own slot (%%v<N>.s). A "+
			"projection here reads the container element the rebinding left behind — "+
			"the stale-length bug (`line = lines[0]; line = line.trim(); line.len()` "+
			"reported 5, or 0 once the container was dropped, for correct content)", got)
	}
	// Guard against the probe silently changing shape: the first definition must
	// still be the borrowing element read.
	if !strings.Contains(ir, "@str_clone(") {
		t.Errorf("no @str_clone in the emitted IR — the element read no longer clones, "+
			"so the probe no longer exercises the rebind path.\nIR:\n%s", ir)
	}
}

// TestReboundFieldViewIsNotProjected is the same rule for the OpGetField shape:
// `v = h.f` then a rebinding must stop forwarding to the field.
func TestReboundFieldViewIsNotProjected(t *testing.T) {
	ir := rebindIR(t, rebindFieldSrc)
	if got := receiverOf(t, ir, "str_plen"); !rebindValueSlotRe.MatchString(got) {
		t.Errorf("@str_plen receiver = %s, want the value's own slot (%%v<N>.s); a "+
			"getfield projection after a rebinding reads the field's stale value", got)
	}
}

// TestSingleDefinitionElementViewStillProjects is the scope guard described on
// singleDefElementSrc.
func TestSingleDefinitionElementViewStillProjects(t *testing.T) {
	ir := rebindIR(t, singleDefElementSrc)
	if got := receiverOf(t, ir, "str_pset"); !regexp.MustCompile(`^%ep\d+$`).MatchString(got) {
		t.Errorf("@str_pset receiver = %s, want an element projection (%%ep<N>) — the "+
			"value is defined exactly once, so forwarding is what lets a mutating "+
			"method write through to the element", got)
	}
}

// The two sources below are the SECOND level of the same hazard. The tests above
// retract a projection when the VIEWED VALUE is rebound; these cover a write to
// the projected STORAGE while the view itself is never rebound. The projected
// address keeps denoting the same location, but that location no longer holds
// the value the read produced.

// reboundContainerSrc rebinds the CONTAINER after the view was taken. The view
// is defined exactly once, so only the container-level guard can catch it.
const reboundContainerSrc = `str.plen = () (r i64) {
    r = 1
}

probe = () (r i64) {
    ls []str = ['aaaa']
    v = ls[0]
    ls = ['b']
    r = v.plen()
}

main = () {
    print(probe())
}
`

// reboundFieldWriteSrc rebinds the FIELD after the view was taken. Nothing is
// dropped anywhere in this program, so it isolates the WRITE guard: the drop
// guard cannot fire, and the view is defined exactly once.
const reboundFieldWriteSrc = `hold { f str }

str.plen = () (r i64) {
    r = 1
}

probe = () (r i64) {
    h = hold { f: 'ab' }
    g = h.f
    h.f = 'z'
    r = g.plen()
}

main = () {
    print(probe())
}
`

// TestReboundContainerViewIsNotProjected pins the container-level guard.
//
// On the unfixed compiler the receiver is an `%ep<N>` GEP into the container's
// buffer — and once `ls` has been rebound, its OLD buffer has been freed, so
// this is a use-after-free rather than merely a wrong number
// (tests/rebind-retracts-view.no case 8 dies with SIGTRAP for exactly this).
func TestReboundContainerViewIsNotProjected(t *testing.T) {
	ir := rebindIR(t, reboundContainerSrc)
	if got := receiverOf(t, ir, "str_plen"); !rebindValueSlotRe.MatchString(got) {
		t.Errorf("@str_plen receiver = %s, want the value's own slot (%%v<N>.s). The "+
			"container was rebound, so this projection addresses storage the view no "+
			"longer belongs to (`line = lines[0]; lines = ['bb']; line.len()` reported "+
			"the NEW element's length for the old content)", got)
	}
}

// TestReboundFieldWriteViewIsNotProjected pins the write guard, with no drop
// anywhere to hide behind.
func TestReboundFieldWriteViewIsNotProjected(t *testing.T) {
	ir := rebindIR(t, reboundFieldWriteSrc)
	if got := receiverOf(t, ir, "str_plen"); !rebindValueSlotRe.MatchString(got) {
		t.Errorf("@str_plen receiver = %s, want the value's own slot (%%v<N>.s). The "+
			"field was rebound after the read, so the projection addresses a "+
			"DIFFERENT string than the one read (`g = h.f; h.f = 'zzzz'; g.len()` "+
			"reported the new field's length for the old content)", got)
	}
}
