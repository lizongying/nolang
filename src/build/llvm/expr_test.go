package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestU32ToU64Assignment tests that u32 arithmetic assigned to u64 variables
// correctly auto-widens without explicit casts, including overflow-safe computation.
func TestU32ToU64Assignment(t *testing.T) {
	src := `
h0 u32 = 100
h1 u32 = 200
myval u64 = h0 | (h1 << 26)
myval2 u64 = h0 + h1
myval3 u64 = h0 * h1
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR should NOT contain zext from i32 to i64 for the OR result,
	// because target type propagation should compute in i64 directly.
	// It should contain or i64, add i64, mul i64 operations.
	if !strings.Contains(ir, "or i64") {
		t.Errorf("IR should contain 'or i64' for u64 = u32 | u32 with target propagation, got:\n%s", ir)
	}
	if !strings.Contains(ir, "add i64") {
		t.Errorf("IR should contain 'add i64' for u64 = u32 + u32 with target propagation, got:\n%s", ir)
	}
	if !strings.Contains(ir, "mul i64") {
		t.Errorf("IR should contain 'mul i64' for u64 = u32 * u32 with target propagation, got:\n%s", ir)
	}
}
