package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestArrByteOffsetSliceAsArgElemSizeIsOne verifies that passing a fixed-array
// OFFSET slice (e.g. ek[16..]) of a [N]byte array directly as a []byte
// argument uses elemSize=1 (byte addressing), not elemSize=8 (i64 addressing).
//
// Regression: generateSliceCore's element-size switch only listed the SIGNED
// scalar types ("i8"/"i16"/"i32"/"i64"). A [N]byte array registers its element
// type as "u8" (byte → u8), which did not match the switch, so elemSize fell
// back to the default 8. The data-pointer offset was then start*8 instead of
// start*1, so the slice read garbage bytes. This broke AES-128/256, where
// add-round-key is called with ek[rk-off..] round keys (fixed-array offset
// slices). The assignment path (sl = ek[16..]) happened to work only because it
// calls llvmTypeSize directly, which handles u8 correctly.
//
// After the fix the switch includes u8/u16/u32/u64, so the offset multiply is by
// 1 for byte arrays. This test asserts no "mul i64 %vec.offset, 8" appears for
// a byte-array offset slice passed as an argument.
func TestArrByteOffsetSliceAsArgElemSizeIsOne(t *testing.T) {
	src := `
chk = (buf []byte) (first i64) {
    first = buf[0]
}

main = () {
    ek [32]byte
    i <- [0..32): {
        ek[i] = i
    }
    ; offset slice passed DIRECTLY as a []byte argument (the buggy path)
    b = chk(ek[8..])
    unused = b
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The slice codegen for ek[8..] must multiply the start offset by 1 (byte
	// stride), producing something like "%vec.offset.N = mul i64 8, 1".
	// The fixed-array offset-slice-as-arg path must NOT multiply by 8.
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "vec.offset") && strings.Contains(trimmed, "mul i64") {
			if strings.Contains(trimmed, ", 8") {
				t.Errorf("byte-array offset slice passed as []byte arg must use elemSize=1, "+
					"not 8. Found 8-byte offset multiplication: %s", trimmed)
			}
		}
	}

	// Positive guard: the offset multiply for start=8 must be by 1.
	if !strings.Contains(ir, "mul i64 8, 1") {
		t.Errorf("expected byte-array offset slice to use 'mul i64 8, 1' (elemSize=1); "+
			"IR missing it:\n%s", ir)
	}
}

// TestArrU16OffsetSliceAsArgUsesTwoByteStride verifies the same path for a
// [N]u16 array: element size must be 2, so the offset multiply is start*2.
// This guards the u16 (and by extension u32/u64) addition to the switch.
func TestArrU16OffsetSliceAsArgUsesTwoByteStride(t *testing.T) {
	src := `
chk = (buf []u16) (first i64) {
    first = buf[0]
}

main = () {
    ek [16]u16
    i <- [0..16): {
        ek[i] = i
    }
    b = chk(ek[4..])
    unused = b
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	foundTwo := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "vec.offset") && strings.Contains(trimmed, "mul i64") {
			if strings.Contains(trimmed, ", 8") {
				t.Errorf("u16-array offset slice must use elemSize=2, not 8. Found: %s", trimmed)
			}
			if strings.Contains(trimmed, "mul i64 4, 2") {
				foundTwo = true
			}
		}
	}
	if !foundTwo {
		t.Errorf("expected u16-array offset slice to use 'mul i64 4, 2' (elemSize=2); IR:\n%s", ir)
	}
}
