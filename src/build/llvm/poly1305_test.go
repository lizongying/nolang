package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestPoly1305ReductionPattern tests the exact pattern used in chacha20-poly1305.no's
// final reduction: u32 values packed into u64 via shift+or, then u32 addition with carry.
// This verifies that target type propagation correctly widens u32 operations to u64.
func TestPoly1305ReductionPattern(t *testing.T) {
	// Must be inside a function body — at top level, `name = IDENT | ...` is
	// ambiguous with type aliases (name = TYPE | TYPE).
	src := `
poly1305-reduce = (h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, s0 u32, s1 u32, s2 u32, s3 u32) (f0 u32, f1 u32, f2 u32, f3 u32) {
    ; Pack 5x26-bit into 4x32-bit (little-endian), target type is u64
    f u64 = h0 | (h1 << 26)
    flo u32 = f & 4294967295
    fhi u32 = (f >> 32)
    f = fhi | (h2 << 20)
    f0 = f & 4294967295
    fhi2 u32 = (f >> 32)
    f = fhi2 | (h3 << 14)
    f1 u32 = f & 4294967295
    fhi3 u32 = (f >> 32)
    f = fhi3 | (h4 << 8)
    f2 u32 = f & 4294967295
    f3 u32 = (f >> 32)

    ; h += s (carry chain, target type is u64)
    c u32 = 0
    f = f0 + s0
    f0 = f & 4294967295
    c = (f >> 32)
    f = f1 + s1 + c
    f1 = f & 4294967295
    c = (f >> 32)
    f = f2 + s2 + c
    f2 = f & 4294967295
    c = (f >> 32)
    f3 = (f3 + s3 + c) & 4294967295
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

	// Verify that OR operations are in i64 (target type propagation worked)
	orCount := strings.Count(ir, "or i64")
	if orCount < 4 {
		t.Errorf("expected at least 4 'or i64' operations, got %d\nIR:\n%s", orCount, ir)
	}

	// Verify that ADD operations for the carry chain are in i64
	addCount := strings.Count(ir, "add i64")
	if addCount < 3 {
		t.Errorf("expected at least 3 'add i64' operations for carry chain, got %d\nIR:\n%s", addCount, ir)
	}

	// Verify zext i32 → i64 widening for u32 operands
	if !strings.Contains(ir, "zext i32") {
		t.Errorf("expected zext i32 operations for u32 → u64 widening, but none found\nIR:\n%s", ir)
	}
}
