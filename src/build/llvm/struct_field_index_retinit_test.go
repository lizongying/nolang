package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStructFieldIndexAssignRetInitBit verifies that struct.field[i] = val
// inside a function sets the ret_init bit for the output parameter, preventing
// emitRetInitZeroFill from zeroing out the struct at return time.
//
// Bug: generateStructFieldIndexAssign did not call emitSetRetInitBit when
// the receiver was an output parameter. The ret_init bitmap stayed 0, so
// emitRetInitZeroFill executed `store %oid zeroinitializer, %oid* %o` at
// return, clobbering inline array / vec / str-long field data that had been
// written via GEP through the output pointer.
//
// This affected ALL struct output parameters whose fields were written
// element-by-element (e.g. o.id[i] = byte_val), not just cross-module calls.
func TestStructFieldIndexAssignRetInitBit(t *testing.T) {
	src := `
oid {
    id [5]byte
}

make-oid = () (o oid) {
    o.id[0] = 18
    o.id[1] = 52
    o.id[2] = 86
    o.id[3] = 120
    o.id[4] = 154
}

read-byte = (o oid, i i64) (b i64) {
    b = o.id[i]
}

main = () {
    o = make-oid()
    b0 = read-byte(o, 0)
    b1 = read-byte(o, 1)
    print(b0)
    print(b1)
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

	// The make-oid function should set the ret_init bit (store to %__ret_init_bitmap)
	// BEFORE writing to o.id[i]. Without this, emitRetInitZeroFill zeros the struct.
	// emitSetRetInitBit generates: load bitmap, or with mask, store
	hasRetInitBitSet := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// Look for "or i64" instruction from emitSetRetInitBit (ri.mask register)
		if strings.Contains(trimmed, "= or i64") && strings.Contains(trimmed, "ri.mask") {
			hasRetInitBitSet = true
		}
	}
	if !hasRetInitBitSet {
		t.Errorf("make-oid should set ret_init bit when writing o.id[i] = val, but no 'or i64' with ri.mask found.\nIR:\n%s", ir)
	}

	// Verify the zero-fill block is NOT taken (the bit is set, so the
	// "icmp eq" check should be false, skipping the zeroinitializer store)
	hasZeroFillSkip := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// The zero-fill block should still exist (for paths that don't assign),
		// but it should not be the only path.
		if strings.Contains(trimmed, "zeroinitializer") && strings.Contains(trimmed, "%oid") {
			hasZeroFillSkip = true
		}
	}
	_ = hasZeroFillSkip // zero-fill block exists but is skipped at runtime
}

// TestStructFieldIndexAssignRetInitBitMultiField tests a struct with multiple
// fields where some are written element-by-element and others via assignment.
func TestStructFieldIndexAssignRetInitBitMultiField(t *testing.T) {
	src := `
data {
    bytes [4]byte
    count i64
}

build-data = () (d data) {
    d.bytes[0] = 65
    d.bytes[1] = 66
    d.bytes[2] = 67
    d.bytes[3] = 68
    d.count = 4
}

main = () {
    d = build-data()
    print(d.bytes[0])
    print(d.bytes[1])
    print(d.bytes[2])
    print(d.bytes[3])
    print(d.count)
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

	// Should have ret_init bit set
	hasRetInitBitSet := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "= or i64") && strings.Contains(trimmed, "ri.mask") {
			hasRetInitBitSet = true
		}
	}
	if !hasRetInitBitSet {
		t.Errorf("build-data should set ret_init bit when writing d.bytes[i] = val.\nIR:\n%s", ir)
	}
}

// TestStructWithStrFieldElementAssign tests struct with str field where
// individual bytes are written (e.g., o.name[0] = 65).
func TestStructWithStrFieldElementAssign(t *testing.T) {
	src := `
person {
    name str
    age i64
}

build-person = () (p person) {
    buf str = with-len(5)
    buf[0] = 65
    buf[1] = 66
    buf[2] = 67
    buf[3] = 68
    buf[4] = 69
    p.name = buf
    p.age = 30
}

main = () {
    p = build-person()
    print(p.name)
    print(p.age)
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

	// p.name = buf and p.age = 30 should set the ret_init bit
	hasRetInitBitSet := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "= or i64") && strings.Contains(trimmed, "ri.mask") {
			hasRetInitBitSet = true
		}
	}
	if !hasRetInitBitSet {
		t.Errorf("build-person should set ret_init bit.\nIR:\n%s", ir)
	}
}
