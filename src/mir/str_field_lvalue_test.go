package mir

import (
	"regexp"
	"strings"
	"testing"
)

// These tests pin the rule that lvalueAddrOf and emitGetField must AGREE on:
// a field read that produced an INDEPENDENT COPY whose source has ALREADY BEEN
// DROPPED is not an lvalue projection.
//
// BACKGROUND — the bug this closes (pre-existing, reproduces on a pristine tree)
// ---------------------------------------------------------------------------
// emitGetField clones an owned `str` field (`str_clone`) so the read temp owns
// its own heap buffer: without it the temp shares the field's buffer and both
// owners free it. lvalueAddrOf, however, treated every getfield as a projection
// and handed callers `&struct.field` — the address of the SOURCE copy, not of the
// clone. Once the source is dropped, every access through that address is a
// use-after-free.
//
// The observable was std/markdown rendering a table row as a paragraph:
// `line.body.contains('|')` read a freed buffer, so `| Alice | 30 |` fell
// through to the paragraph branch. Nondeterministic by heap layout — 100%
// reproducible at NOLANG_OPT_LEVEL=-O0, ~25% at -O3.
//
// WHY THE ASSERTION IS ON THE EMITTED IR, NOT ON STDOUT
// -----------------------------------------------------
// A use-after-free on a just-freed small buffer very often returns the old
// bytes, so stdout is right while the program is unsound: the rate is a
// property of the allocator, not of the bug. Only the receiver operand — the
// value's own slot vs. an lvalue GEP into the source — is deterministic.
//
// NAMING: lvalueAddrOf names its GEPs `%lvg<N>`; value slots are `%v<N>.s`.
//
// WHY THE PROBE DECLARES ITS OWN `str.probe` INSTEAD OF USING `str.contains`
// --------------------------------------------------------------------------
// lowerForTest lowers a single source with no std in scope, so a real std
// method is "unknown callee" at EmitLLVM time. Declaring a one-line method on
// `str` in the probe gives the same shape — receiver is an owned str field read
// — with no std dependency.

// strFieldDropSrc: the source is a container element read, so the compiler drops
// it immediately after the field read and the method call afterwards would read
// freed memory. This is std/markdown's shape (`line` is a temp out of `lines[i]`).
const strFieldDropSrc = `line { body str }

str.probe = (target str) (r i64) {
    r = 1
}

probe = (c bool) (r i64) {
    ls []line = []
    l = ls[0]
    b = l.body
    c -> r = b.probe('|')
    -> r = 0
}

main = () {
    print(probe(true))
}
`

// strFieldLiveSrc: the source is an ordinary local whose drop is emitted at the
// end of the function, long after the call. Forwarding is CORRECT here — it is
// the only thing that makes `b.s.set-byte(0, 72)` write the field.
const strFieldLiveSrc = `line { body str }

str.probe = (target str) (r i64) {
    r = 1
}

probe = () (r i64) {
    l = line { body: '| a | b |' }
    b = l.body
    r = b.probe('|')
}

main = () {
    print(probe())
}
`

func strFieldLvalueIR(t *testing.T, src string) string {
	t.Helper()
	mod := lowerForTest(t, src)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	return ir
}

// strProbeReceiver returns the operand the generated `str.probe` call receives,
// and fails the test if the probe shape did not lower (which would let every
// assertion here pass vacuously).
func strProbeReceiver(t *testing.T, ir string) string {
	t.Helper()
	call := regexp.MustCompile(`call void @str_probe\(%str-long\*\s+(%[^,]+),`).FindStringSubmatch(ir)
	if call == nil {
		t.Fatalf("no @str_probe call in the emitted IR — the probe shape did not lower "+
			"as expected; the test would pass vacuously.\nIR:\n%s", ir)
	}
	return call[1]
}

// TestStrFieldReadAfterSourceDropIsNotForwarded is the regression pin. A cloned
// field read whose source has already been dropped must be used as the value it
// is — its own slot, holding the clone — and must never be projected back onto
// the source's storage.
//
// On the unfixed compiler the receiver is `%lvgN = getelementptr inbounds %line,
// ptr %vM.s, i32 0, i32 0`, i.e. the struct whose drop freed the bytes the
// method then reads.
func TestStrFieldReadAfterSourceDropIsNotForwarded(t *testing.T) {
	ir := strFieldLvalueIR(t, strFieldDropSrc)
	if got := strProbeReceiver(t, ir); !regexp.MustCompile(`^%v\d+\.s$`).MatchString(got) {
		t.Errorf("@str_probe receiver = %s, want the value's own slot (%%v<N>.s) holding "+
			"the clone; an lvalue GEP here points into storage the drop already released", got)
	}
	if !strings.Contains(ir, "@str_clone(") {
		t.Errorf("no @str_clone in the emitted IR: the fix must not remove the clone — " +
			"without it the temp and the field share one buffer and it is freed twice")
	}
}

// TestStrMethodReceiverIsTheCloneSlot ties the two halves together: the slot the
// method receives must be exactly the slot emitGetField stored the CLONE into.
// "Not an lvalue GEP" alone would also be satisfied by handing the method an
// unrelated slot; this is what makes the two decisions (clone in emitGetField,
// don't project in lvalueAddrOf) verifiably consistent.
func TestStrMethodReceiverIsTheCloneSlot(t *testing.T) {
	ir := strFieldLvalueIR(t, strFieldDropSrc)
	cloneStore := regexp.MustCompile(`store %str-long %lc\d+, %str-long\*\s+(%v\d+\.s)`).FindStringSubmatch(ir)
	if cloneStore == nil {
		t.Fatalf("no `store %%str-long %%lc<N>, %%str-long* %%v<M>.s` — emitGetField did not "+
			"clone the owned str field, so the probe shape is gone and every assertion "+
			"here would pass vacuously.\nIR:\n%s", ir)
	}
	if got := strProbeReceiver(t, ir); got != cloneStore[1] {
		t.Errorf("receiver = %s but the clone was stored into %s: emitGetField produced a "+
			"copy while the call site used different storage", got, cloneStore[1])
	}
}

// TestStrFieldReadWhileSourceAliveStillForwards is the scope guard, and it is
// the one that stops the fix from eating `b.s.set-byte(0, 72)`.
//
// Dropped and dead are different things: this source has no further use after the
// field read either, but its drop is emitted at the END of the function, so the
// projected address is still valid and is the only way a write through it can
// reach the field. "Never forward a cloned field" would make
// tests/set-byte-receiver-writeback.no, tests/len-assign-grow.no,
// tests/std-byte-indexing.no, tests/path-char2.no and
// tests/path-clean-dotdot.no lose their writes silently.
func TestStrFieldReadWhileSourceAliveStillForwards(t *testing.T) {
	ir := strFieldLvalueIR(t, strFieldLiveSrc)
	if got := strProbeReceiver(t, ir); !regexp.MustCompile(`^%lvg\d+$`).MatchString(got) {
		t.Errorf("@str_probe receiver = %s, want an lvalue projection (%%lvg<N>) — the "+
			"source has not been dropped yet, so forwarding is still correct and "+
			"required for write-through", got)
	}
}

// TestNonStrFieldReadStillLvalueForwarded is the other scope guard: only the
// cloned (owned str) shape is affected. An i64 field still has to forward, or
// `x = o.c; x.n = 7` writes a dead copy and the mutation the whole lvalueAddrOf
// mechanism exists for is lost again.
//
// This is also what distinguishes the fix from the NOLANG_MIR_NO_LVALUE kill
// switch: that env var also stops the markdown crash, by disabling projection
// for everything — which silently re-breaks every mutating std method.
func TestNonStrFieldReadStillLvalueForwarded(t *testing.T) {
	ir := strFieldLvalueIR(t, `counter { n i64 }
holder { c counter }

bump = () (r i64) {
    o = holder { c: counter { n: 1 } }
    x = o.c
    x.n = 7
    r = o.c.n
}

main = () {
    print(bump())
}
`)
	if !regexp.MustCompile(`%lvg\d+ = getelementptr inbounds %holder\b`).MatchString(ir) {
		t.Errorf("no lvalue projection for the i64 field: `x.n = 7` on a copied struct would " +
			"write a temporary and the fix has over-reached")
	}
}
