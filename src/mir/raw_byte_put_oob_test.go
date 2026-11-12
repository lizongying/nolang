package mir

import (
	"strings"
	"testing"
)

// rawBytePutOOBSrc drives the raw-byte WRITER (`str._set-byte` / `txt._set-byte`,
// std-private since 2026-11-11) with an index past the receiver's PHYSICAL
// capacity.
//
// Why this test exists at all: the runtime suite used to cover the writer's
// fallible contract from tests/byte.no ("setbyte-fallible") and
// test/std/txt.no (`txt.set-byte`). Privatising the builtin took both away —
// user code cannot call `_set-byte` any more, so the only remaining caller is
// std internals (str.fill / txt._from-bytes), which never index out of range.
// The invariant still matters (a wild write is a heap overflow), so it is
// pinned here on the emitted IR instead.
const rawBytePutOOBSrc = `
main = () {
    s str = with-cap(4)
    r = s._set-byte(255, 89)
}
`

// TestBuiltinRawBytePutOOBGoesToScratch pins the out-of-range behaviour of
// emitBuiltinRawBytePut: the store MUST always happen, but when the index is
// past the receiver's capacity it has to land on a hoisted 1-byte scratch
// slot instead of the receiver's buffer.
//
// The observable is the IR shape:
//
//	%bps.N = alloca i8                       ; the scratch slot
//	%bpkg.N = getelementptr ... field 1      ; load cap
//	%bpb.N = icmp ult i64 %idx, %cap         ; the bounds check
//	%bpbp.N = select i1 %bpb.N, i8* %data, i8* %bps.N
//	%bpsi.N = select i1 %bpb.N, i64 %idx, i64 0
//	%bpv.N = getelementptr i8, i8* %bpbp.N, i64 %bpsi.N
//	store i8 ..., i8* %bpv.N
//
// Both selects are required: without the pointer select an OOB index writes
// through the real data pointer; without the index select a negative index
// (a huge unsigned value) would offset far outside the scratch slot.
func TestBuiltinRawBytePutOOBGoesToScratch(t *testing.T) {
	mod := lowerForTest(t, rawBytePutOOBSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}

	if !strings.Contains(ir, "alloca i8\n") {
		t.Fatalf("raw-byte put emitted no 1-byte scratch alloca; the OOB store has nowhere safe to land")
	}
	if n := strings.Count(ir, "= select i1"); n < 2 {
		t.Fatalf("raw-byte put emitted %d `select i1` (want >= 2: pointer select + index select)", n)
	}
	if !strings.Contains(ir, "i8* %bps.") {
		t.Fatalf("raw-byte put's pointer select does not reference the scratch slot (%%bps.N)")
	}
	if !strings.Contains(ir, "i64 0\n") {
		t.Fatalf("raw-byte put's index select does not clamp the OOB index to 0")
	}
	if !strings.Contains(ir, "store i8 ") {
		t.Fatalf("raw-byte put emitted no byte store at all")
	}
	// The bounds check must be UNSIGNED: a negative index is a huge unsigned
	// value, so `icmp ult` rejects it while `icmp slt` would accept it.
	if !strings.Contains(ir, "icmp ult i64") {
		t.Fatalf("raw-byte put's bounds check is not an unsigned compare; a negative index would pass")
	}

	// The store must NOT be an unconditional store through the receiver's data
	// pointer — i.e. no `store i8 ... , i8* %bpd.` (the raw data pointer).
	for _, line := range strings.Split(ir, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "store i8 ") {
			continue
		}
		if strings.Contains(line, "i8* %bpd.") {
			t.Fatalf("raw-byte put stores straight through the receiver's data pointer, bypassing the bounds check: %s", line)
		}
	}
}
