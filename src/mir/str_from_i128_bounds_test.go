package mir

import (
	"regexp"
	"strconv"
	"testing"
)

// TestStrFromI128SignStaysInBounds pins the BUFFER SIZING of the four prelude
// helpers that render a 128-bit integer by hand (src/mir/codegen.go):
//
//	@str_from_i128  signed   -> %str-long   (x.to-str() for i128)
//	@str_from_u128  unsigned -> %str-long   (x.to-str() for u128)
//	@print_i128     signed   -> stdout      (print(x) for i128)
//	@print_u128     unsigned -> stdout      (print(x) for u128)
//
// Why this is a Go test on the emitted IR rather than a `.no` test: the defect
// is a one-byte STACK out-of-bounds write, and whether the stray byte survives
// to the emit `memcpy`/`write` is a toolchain / stack-layout accident. A `.no`
// assertion therefore passes on the broken helper on most toolchains (it did
// here at -O1/-O2/-O3), so it pins nothing. The invariant that does hold is
// geometric and is readable straight off the IR:
//
//	%buf   = alloca [N x i8]
//	%end   = &buf[E]                  ; the NUL terminator is stored here
//	loop   writes digits at %end-1, %end-2, ... (one byte lower each time)
//	negw   writes '-' one byte below the last digit written
//
// |i128.MIN| = 2^127 has 39 decimal digits, so the sign path reaches index
// E-39-1. Requiring that to be >= 0 gives E >= 40; the alloca must also cover
// the NUL, so N >= E+1.
//
// The signed helpers were both sized 40 with E = 39, i.e. E-39-1 = -1: the '-'
// was stored one byte BEFORE the alloca and the emit `memcpy` / `write` then
// read one byte from before it. ASan on the extracted helper reports
// "stack-buffer-underflow ... 'buf' [32,72)", and i128.MIN.to-str() rendered
// without its leading '-' (test/std/number.no "str-eq - i128.MIN").
//
// The unsigned helpers have no sign branch, so their bound is E >= 39 (39
// digits, NUL at E); they are checked too so a later buffer shrink cannot
// silently break the u128.MAX path either.
func TestStrFromI128SignStaysInBounds(t *testing.T) {
	// maxDigits is the decimal digit count of both |i128.MIN| = 2^127 and
	// u128.MAX = 2^128-1.
	const maxDigits = 39

	// The prelude is pruned down to the helpers actually referenced, so the
	// source must really render a signed and an unsigned 128-bit value, and
	// really print each one (print goes to the @print_i128 / @print_u128
	// helpers; to-str goes to the @str_from_* helpers).
	const src = `main = () () {
  x i128 = 170141183460469231731687303715884105727
  s str = x.to-str()
  print(s)
  y u128 = 340282366920938463463374607431768211455
  u str = y.to-str()
  print(u)
  m i128 = i128.MIN
  print(m)
  print(y)
}
`
	mod, _ := lowerSrcWithDiags(t, src)
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	for _, tc := range []struct {
		sym       string
		signBytes int // 1 for the signed helpers, 0 for the unsigned ones
	}{
		{"@str_from_i128", 1},
		{"@str_from_u128", 0},
		{"@print_i128", 1},
		{"@print_u128", 0},
	} {
		fn := extractDefineBlock(t, ll, tc.sym)
		size, end := parseBufAndEnd(t, fn, tc.sym)

		lowest := end - maxDigits - tc.signBytes // lowest index the helper touches

		if lowest < 0 {
			t.Errorf("%s: writes at %%buf[%d] — %d byte(s) BEFORE the alloca "+
				"(buffer=%d, %%end=%d, digits=%d, sign=%d); the sign/digits must stay inside the buffer",
				tc.sym, lowest, -lowest, size, end, maxDigits, tc.signBytes)
		}
		if size < end+1 {
			t.Errorf("%s: %%end = &buf[%d] but the buffer is only %d byte(s) — the NUL terminator is out of bounds",
				tc.sym, end, size)
		}
	}
}

// extractDefineBlock returns the `define ... @sym(...) { ... }` block for sym,
// definition line included. It fails the test when the symbol is missing, which
// is itself a regression (the helper must be emitted).
func extractDefineBlock(t *testing.T, ir, sym string) string {
	t.Helper()
	header := regexp.MustCompile(`(?m)^define[^\n]*` + regexp.QuoteMeta(sym) + `\([^\n]*\{$`)
	loc := header.FindStringIndex(ir)
	if loc == nil {
		t.Fatalf("%s not found in the emitted prelude", sym)
	}
	rest := ir[loc[0]:]
	end := regexp.MustCompile(`(?m)^\}`).FindStringIndex(rest)
	if end == nil {
		t.Fatalf("%s: unterminated define block", sym)
	}
	return rest[:end[1]]
}

// parseBufAndEnd pulls N and E out of the helper's alloca and its %end GEP.
func parseBufAndEnd(t *testing.T, fn, sym string) (size, end int) {
	t.Helper()
	m := regexp.MustCompile(`%buf = alloca \[(\d+) x i8\]`).FindStringSubmatch(fn)
	if m == nil {
		t.Fatalf("%s: no `%%buf = alloca [N x i8]` line found", sym)
	}
	size, _ = strconv.Atoi(m[1])

	// %end = getelementptr [N x i8], [N x i8]* %buf, i64 0, i64 E
	m = regexp.MustCompile(`%end = getelementptr \[\d+ x i8\], \[\d+ x i8\]\* %buf, i64 0, i64 (\d+)`).
		FindStringSubmatch(fn)
	if m == nil {
		t.Fatalf("%s: no `%%end = getelementptr ... %%buf ... i64 E` line found", sym)
	}
	end, _ = strconv.Atoi(m[1])
	return size, end
}
