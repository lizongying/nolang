package mir

import (
	"strings"
	"testing"
)

// These two tests pin a pair of narrow-integer defects that both came from the
// same misunderstanding of how MIR stores integers, and that together made
// `no test test/std` hang and lose four crypto/math suites.
//
// Background: MIR flattens every integer EXCEPT byte/u8/i8 to an i64 slot —
// llvmTypeOf(KindInt) returns "i64" for i16/u16/i32/u32, GlobalDecl.Type holds
// that flattened type, and every reader (loadVal, emitMove, and the by-ref
// argument ABI `i64* @g`) therefore accesses an integer global as 8 bytes.
// The ONLY thing that decides the type a global is actually emitted with is its
// ConstText initializer text, so narrowing that text to `i16 ...` allocates two
// bytes and leaves every reader loading eight.

// ── 1. A narrow integer global must be STORED at MIR's width ────────────────
//
// `n i16 = -100` used to fold to `@n = private global i16 -100`, while the read
// emitted `load i64, i64* @n`. Under opaque pointers LLVM accepts that
// silently and reads 6 bytes of unrelated memory, so:
//
//	in i16  = -100   -> printed 65436
//	in u16  = 65535  -> printed -1
//	u16 65535.sqrt() -> mid took a garbage value and the binary search never
//	                    terminated (test/std/math.no hung until -timeout)
//
// The invariant that prevents the whole class: the emitted type must equal what
// llvmTypeOf gives for the declared type — i8 for byte/u8/i8, i64 otherwise.
func TestNarrowIntGlobalStoredAtMIRWidth(t *testing.T) {
	src := `N i16 = -100
U u16 = 65535

probe = () (r i64) {
    #{overflow=wrap}
    r = N + U
}

x = probe()
print(x)
`
	mod := lowerForTest(t, src)
	if len(mod.Globals) == 0 {
		t.Fatal("no module globals registered — the probe no longer exercises the " +
			"global fold path, so this test would pass vacuously")
	}
	found := 0
	for i := range mod.Globals {
		g := &mod.Globals[i]
		if g.ConstText == "" {
			continue
		}
		if strings.HasPrefix(g.ConstText, "i16 ") || strings.HasPrefix(g.ConstText, "i32 ") {
			found++
			t.Errorf("global %q folds to %q: MIR flattens i16/u16/i32/u32 to an i64 slot, "+
				"so a narrow initializer allocates a narrow global that every reader "+
				"(loadVal / emitMove / by-ref `i64*` argument) still loads 8 bytes from. "+
				"Emit the value at MIR width (`i64 ...`) instead.", g.Name, g.ConstText)
		}
	}
	if found == 0 {
		// Confirm the probe really produced narrow-typed globals, otherwise the
		// assertions above are unreachable and the test proves nothing.
		narrow := 0
		for i := range mod.Globals {
			if raw := mod.Globals[i].ConstText; strings.HasPrefix(raw, "i64 ") {
				narrow++
			}
		}
		if narrow == 0 {
			t.Fatalf("neither a narrow nor an i64 scalar initializer was emitted; "+
				"globals: %d — the probe no longer covers this path", len(mod.Globals))
		}
	}
}

// ── 2. Unsigned narrow arithmetic must extend with zext, never sext ─────────
//
// `intLitTypeFromHint` used to return the LLVM type spelling, mapping a u16 hint
// to "i16" and a u32 hint to "i32". Both spellings lower to i64, so the width was
// fine — but the returned name IS the value's raw type, and the narrow-wrap
// decision reads it back through isUnsignedRaw. Every unsigned narrow
// intermediate therefore sign-extended:
//
//	inside u16.sqrt: mid = (lo + hi) / 2 with lo=1, hi=32767
//	1 + 32767 = 32768 -> trunc to i16 -> sext -> -32768
//	-> `udiv i64 -32768, 2` and the binary search never converged
//
// 30000 + 2000 = 32000 is the same shape: it must survive as 32000, not as
// -33536, which is what a sext of its low 16 bits gives.
func TestUnsignedNarrowArithExtendsWithZext(t *testing.T) {
	// Two details are essential to reaching the defect:
	//   - `lo u16 = 1` is a narrow-typed LOCAL initialised from a literal, so it
	//     goes through intLitTypeFromHint and records the returned name as its
	//     raw type. Parameters carry their signature type and leave it
	//     unexercised; an all-literal body gets constant-folded away entirely.
	//   - the argument must come from a runtime read (a top-level store into a
	//     module global) so the call survives lowering and the function is
	//     actually emitted.
	src := `M u16 = 0
M = 32767

addw = (n u16) (r u16) {
    lo u16 = 1
    hi u16 = n
    #{overflow=wrap}
    mid u16 = (lo + hi) / 2
    r = mid
}

s = addw(M)
print(s)
`
	ir, err := lowerSourceForTest(t, src).EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(ir, "trunc i64") {
		t.Fatalf("expected a wrap truncation to the declared 16-bit width, got none — " +
			"the probe no longer exercises the narrow wrap path")
	}
	if strings.Contains(ir, "sext i16") {
		t.Error("a u16 arithmetic result is SIGN-extended: `trunc -> sext` turns " +
			"32000 into -33536, because intLitTypeFromHint returned the LLVM spelling " +
			"\"i16\" (signed) instead of the declared raw \"u16\".")
	}
	if !strings.Contains(ir, "zext i16") {
		t.Error("a u16 arithmetic result is not zero-extended: it must widen back to " +
			"the i64 MIR slot with zext so 32000 stays 32000.")
	}
}
