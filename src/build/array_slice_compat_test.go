package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsArrayToSliceAndIntLitToU64 verifies range-aware
// integer literal compatibility and array-to-slice coercion:
//   1. [N]T passed where []T is expected → allowed (array-to-slice)
//   2. Integer literal 0 → u64 → allowed (in range)
//   3. Integer literal 200 → u8 → allowed (in range, u8 max=255)
//   4. Integer literal -100 → i8 → allowed (in range, i8 min=-128)
//   5. Integer literal 300 → u8 → error (out of range, u8 max=255)
//   6. Integer literal -1 → u64 → error (negative to unsigned)
//   7. Integer literal -200 → i8 → error (out of range, i8 min=-128)
func TestValidateFuncArgsArrayToSliceAndIntLitToU64(t *testing.T) {
	src := `blake-g = (v []u64, a i64, b i64, c i64, d i64, x u64, y u64) {
    v[a] = v[a]
}

set-u8 = (val u8) {}
set-u16 = (val u16) {}
set-u32 = (val u32) {}
set-u64 = (val u64) {}
set-i8 = (val i8) {}

compress = (x []byte, y []byte) (out []byte) {
    v [128]u64
    i = 0
    for i < 8 {
        base = i * 16
        blake-g(v, base + 0, base + 4, base + 8, base + 12, 0, 0)
        i = i + 1
    }
    // In-range literals
    set-u8(200)
    set-u16(40000)
    set-u32(3000000000)
    set-u64(0)
    set-i8(-100)
    // Out-of-range literals — these SHOULD produce errors
    set-u8(300)
    set-u64(-1)
    set-i8(-200)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")

	// Map line → error message for easy lookup
	errByLine := make(map[int]string)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		errByLine[r.Line] = r.Message
	}

	// In-range: should NOT have errors on these lines
	// Line 11: blake-g(v, ..., 0, 0) — v is [128]u64 → []u64, 0 → u64
	// Line 20: set-u8(200) — 200 <= 255 ✓
	// Line 21: set-u16(40000) — 40000 <= 65535 ✓
	// Line 22: set-u32(3000000000) — 3000000000 <= 4294967295 ✓
	// Line 23: set-u64(0) — 0 >= 0 ✓
	// Line 24: set-i8(-100) — -100 >= -128 ✓
	for _, line := range []int{11, 20, 21, 22, 23, 24} {
		if msg, ok := errByLine[line]; ok {
			t.Errorf("L%d: false positive: %s", line, msg)
		}
	}

	// Out-of-range: SHOULD have errors on these lines
	// Line 26: set-u8(300) — 300 > 255 ✗
	// Line 27: set-u64(-1) — -1 < 0 ✗
	// Line 28: set-i8(-200) — -200 < -128 ✗
	for _, line := range []int{26, 27, 28} {
		if _, ok := errByLine[line]; !ok {
			t.Errorf("L%d: expected error but none found", line)
		}
	}
}
