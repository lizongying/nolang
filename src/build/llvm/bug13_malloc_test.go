package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestBug13NoDynamicAlloca verifies that makeNullTerminatedStr uses @nolang.malloc
// instead of dynamic alloca (alloca i8, i64 %reg). Dynamic alloca in the middle of
// a function causes LLVM -O3 optimization issues (stack corruption).
func TestBug13NoDynamicAlloca(t *testing.T) {
	src := `
main = () {
    print('hello')
    is-file('/tmp/test.txt')
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

	// Check that there are no dynamic alloca instructions (alloca i8, i64 %reg)
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// Dynamic alloca pattern: alloca i8, i64 %xxx (where xxx is a register, not a constant)
		if strings.Contains(trimmed, "alloca i8, i64 %") {
			t.Errorf("Found dynamic alloca instruction: %s", line)
		}
	}
}

// TestBug13MallocUsed verifies that makeNullTerminatedStr uses @nolang.malloc
// and the buffers are tracked in stmtTempRawPtrs and freed at statement end.
func TestBug13MallocUsed(t *testing.T) {
	src := `
main = () {
    print('hello world')
    is-file('/tmp/test.txt')
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

	// Check that @nolang.malloc is used for null-terminated strings
	// and @free is called at statement end
	mallocCount := 0
	freeCount := 0
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call i8* @nolang.malloc") {
			mallocCount++
		}
		if strings.Contains(trimmed, "call void @free(i8*") {
			freeCount++
		}
	}

	// Should have at least one malloc (for null-terminated string buffer)
	// and one free (for the same buffer)
	if mallocCount == 0 {
		t.Errorf("Expected at least 1 @nolang.malloc call for null-terminated string buffer, got 0")
	}
	if freeCount == 0 {
		t.Errorf("Expected at least 1 @free call for null-terminated string buffer, got 0")
	}
}

// TestBug13Simple verifies the simple bug13 case works.
// This is the case that was failing before the fix due to dynamic alloca.
func TestBug13Simple(t *testing.T) {
	src := `compute-str = (prefix str) (s str) {
    s = prefix - '-1234567890abcdef'
}

write-ref-sim = (ref-name str, hex-val str, val-i64 i64) (ok bool) {
    content = ref-name - ' ' - hex-val - '\n'
    ok = true
}

main = () () {
    ch str = compute-str('commit')
    ok bool = write-ref-sim('refs', ch, 42)
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

	// Just verify that the IR is generated without errors
	// (the actual runtime behavior is tested separately)
	if strings.Contains(ir, "codegen error") {
		t.Errorf("IR generation produced errors: %s", ir)
	}
}