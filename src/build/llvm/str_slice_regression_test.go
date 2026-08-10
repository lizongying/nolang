package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStrSliceElemSizeIsOne verifies that native slice on a str type uses
// elemSize=1 (byte addressing), not elemSize=8 (i64 addressing).
//
// Bug 06 described an inconsistency where str had two representations
// (%str-short stride=1 and %str-long stride=8), and slice operations
// did not correctly handle the stride difference, leading to wrong
// substring results or invalid LLVM IR.
//
// The current codebase unifies all str to %str-long with stride=1,
// and slice codegen correctly sets elemSize=1 when isStr is true.
// This test guards against regressions that might re-introduce the
// stride mismatch.
func TestStrSliceElemSizeIsOne(t *testing.T) {
	src := `
slice-native = (s str, a i64, b i64) (out str) {
    out = s[a..b)
}

main = () {
    s = 'hello world'
    sub = slice-native(s, 0, 5)
    print(sub)
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

	// The slice codegen should NOT produce a mul with 8 for the data pointer offset
	// when operating on a str type. The elemSize for str should be 1.
	// Pattern to detect: "mul i64 %..., 8" in the slice offset computation.
	// This is a heuristic — if elemSize were 8, the offset GEP would multiply
	// the start index by 8, producing wrong byte offsets for a byte-addressed string.
	if strings.Contains(ir, "mul i64 %sv.offset") || strings.Contains(ir, "mul i64 %vec.offset") {
		// If we see an offset multiply, check it's not by 8
		for _, line := range strings.Split(ir, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "mul i64") && strings.Contains(trimmed, "offset") {
				if strings.Contains(trimmed, ", 8") {
					t.Errorf("str slice offset should use elemSize=1, not 8. "+
						"Found 8-byte offset multiplication: %s", trimmed)
				}
			}
		}
	}

	// Verify the result is alloca %str-long (not %vec or %arr)
	if !strings.Contains(ir, "alloca %str-long") {
		t.Errorf("str slice result should be alloca %%str-long, got IR:\n%s", ir)
	}
}

// TestStrSliceViewAssignmentElemSize verifies that the slice view assignment path
// (generateSliceViewAssignment) also uses elemSize=1 for str types.
func TestStrSliceViewAssignmentElemSize(t *testing.T) {
	src := `
main = () {
    s = 'hello world'
    sub = s[0..5)
    print(sub)
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

	// Should not have mul with 8 for str slice offset
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "mul i64") && strings.Contains(trimmed, "offset") {
			if strings.Contains(trimmed, ", 8") {
				t.Errorf("str slice view assignment should use elemSize=1, not 8. "+
					"Found: %s", trimmed)
			}
		}
	}

	// Should produce a clone with memcpy for the slice data
	if !strings.Contains(ir, "@llvm.memcpy") && !strings.Contains(ir, "@nolang.malloc") {
		// slice view assignment always clones
		t.Errorf("str slice view should clone data (malloc+memcpy), got IR:\n%s", ir)
	}
}

// TestStrSliceWithLenConsistency verifies that slicing a with-len constructed
// string produces correct IR (no type mismatches, correct elemSize).
func TestStrSliceWithLenConsistency(t *testing.T) {
	src := `
main = () {
    s = with-len(11)
    s[0] = 'h'
    s[1] = 'e'
    s[2] = 'l'
    s[3] = 'l'
    s[4] = 'o'
    s[5] = ' '
    s[6] = 'w'
    s[7] = 'o'
    s[8] = 'r'
    s[9] = 'l'
    s[10] = 'd'
    sub = s[0..5)
    print(sub)
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

	// No type mismatch: store i8 or store i64 should be consistent
	// The key check: no "mul i64 %..., 8" for the slice offset on a str
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "mul i64") && strings.Contains(trimmed, "offset") {
			if strings.Contains(trimmed, ", 8") {
				t.Errorf("with-len str slice should use elemSize=1, not 8. "+
					"Found: %s", trimmed)
			}
		}
	}
}

// TestStrSliceConcatConsistency verifies that slicing a concatenated string
// produces correct IR.
func TestStrSliceConcatConsistency(t *testing.T) {
	src := `
main = () {
    s = 'hello' - ' ' - 'world'
    sub = s[0..5)
    print(sub)
    sub2 = s[6..11)
    print(sub2)
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

	// No 8-byte offset for str slice
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "mul i64") && strings.Contains(trimmed, "offset") {
			if strings.Contains(trimmed, ", 8") {
				t.Errorf("concat str slice should use elemSize=1, not 8. "+
					"Found: %s", trimmed)
			}
		}
	}

	// Should have alloca %str-long for results
	if !strings.Contains(ir, "alloca %str-long") {
		t.Errorf("concat str slice result should be alloca %%str-long, got IR:\n%s", ir)
	}
}

// TestStrSliceMidOffset verifies that mid-string slicing (start > 0) correctly
// computes the data pointer offset with elemSize=1.
func TestStrSliceMidOffset(t *testing.T) {
	src := `
main = () {
    s = 'hello world'
    sub = s[6..11)
    print(sub)
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

	// For start=6, elemSize=1, the offset should be 6 (not 48=6*8)
	// Check that the GEP uses the correct offset
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "mul i64") && strings.Contains(trimmed, "offset") {
			if strings.Contains(trimmed, ", 8") {
				t.Errorf("mid-string slice offset should use elemSize=1 (not 8). "+
					"Found: %s", trimmed)
			}
		}
	}
}
