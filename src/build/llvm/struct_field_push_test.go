package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// countVecPushExpansions counts the number of vec.push expansions emitted into
// the IR. Each expansion allocates exactly one "%vp.space.N" register (the
// len < cap comparison), which is unique to the vec-push codegen path. Only
// the register definition lines are counted — the following `br i1 %vp.space.N`
// branch also mentions the register but must not be double-counted.
func countVecPushExpansions(ir string) int {
	n := 0
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "%vp.space.") && strings.Contains(trimmed, "= icmp slt i64") {
			n++
		}
	}
	return n
}

// TestStructFieldPushSingleExpansion verifies that `.data.push(val)` on a
// struct field generates the vec-push expansion exactly ONCE.
//
// Bug 07 (struct-field-push-broken): for a method call whose receiver is a
// DotExpression (e.g. .data.push(b) inside a builder method), fnName is
// reduced to the bare builtin name "push" (see the fnName computation for
// non-Identifier receivers). The builtin is then dispatched TWICE:
//
//  1. at the first ForwardFunc dispatch site, genForwardFunc("vec-push", ...)
//     emits the full push expansion into sb and returns "" — but "vec-push"
//     was missing from that site's side-effect exception list, so the call
//     generation did NOT return and fell through;
//  2. after method resolution, the second dispatch site emitted the push
//     expansion AGAIN.
//
// Result: every .push() appended the element twice (len grew by 2 per push),
// so two add(72)/add(105) calls reported len == 4 instead of 2.
//
// The fix adds vec-push to the first dispatch site's exception list (guarded
// by receiver-form checks), so exactly one expansion is emitted.
func TestStructFieldPushSingleExpansion(t *testing.T) {
	src := `
builder {
    data []i64
}

builder.add = (b i64) {
    .data.push(b)
}

main = () {
    b = builder {}
    b.add(72)
    b.add(105)
    print(b.data.len)
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

	n := countVecPushExpansions(ir)
	if n != 1 {
		t.Errorf("struct field .data.push(b) must emit exactly 1 vec-push expansion, got %d.\nIR:\n%s", n, ir)
	}
}

// TestStructFieldPushTwoStatements verifies that two separate push statements
// on the same struct field emit two expansions (one per statement), not four.
func TestStructFieldPushTwoStatements(t *testing.T) {
	src := `
builder {
    data []byte
}

builder.add = (b i64) {
    .data.push(b)
    .data.push(b)
}

main = () {
    b = builder {}
    b.add(72)
    print(b.data.len)
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

	n := countVecPushExpansions(ir)
	if n != 2 {
		t.Errorf("two .data.push(b) statements must emit exactly 2 vec-push expansions, got %d.\nIR:\n%s", n, ir)
	}
}

// TestLocalVarPushSingleExpansion guards the local-variable path: v.push(val)
// on a plain Identifier receiver must still emit exactly one expansion.
func TestLocalVarPushSingleExpansion(t *testing.T) {
	src := `
main = () {
    v []i64
    v.push(1)
    v.push(2)
    print(v.len)
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

	n := countVecPushExpansions(ir)
	if n != 2 {
		t.Errorf("two local v.push(val) statements must emit exactly 2 vec-push expansions, got %d.\nIR:\n%s", n, ir)
	}
}

// TestStructFieldPushGlobalVarSingleExpansion guards the module-level global
// variable path from bug07's workaround (zw-data.push(b)): the push must emit
// exactly one expansion as well.
func TestStructFieldPushGlobalVarSingleExpansion(t *testing.T) {
	src := `
zw-data []byte

add-byte = (b i64) {
    zw-data.push(b)
}

main = () {
    add-byte(72)
    add-byte(105)
    print(zw-data.len)
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

	n := countVecPushExpansions(ir)
	if n != 1 {
		t.Errorf("the single zw-data.push(b) statement must emit exactly 1 vec-push expansion, got %d.\nIR:\n%s", n, ir)
	}
}
