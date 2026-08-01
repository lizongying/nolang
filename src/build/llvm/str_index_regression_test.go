package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStringIndexNoTypeMismatch verifies that string indexing (s[i]) does not
// produce a type mismatch between i64 and i8 in LLVM IR.
// Regression: commit 0f6c3b7 added an IndexExpression case to intExprLLVMType
// that returned the element type (e.g. "i8") instead of "i64", but
// generateIndexExpression always zexts narrow integer elements to i64.
// This caused "and i8 %vec.idx.zext.X, 255" and "store i8 %idx.zext.X" errors
// because the value was actually i64 but the type was reported as i8.
func TestStringIndexNoTypeMismatch(t *testing.T) {
	src := `
main = () {
    s = 'hello'
    c = s[0]
    print(c.to-str())
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

	// The zext should be: zext i8 %val to i64 (correct)
	// NOT: and i8 %vec.idx.zext.X, 255 (wrong — value is i64 not i8)
	if strings.Contains(ir, "and i8 %vec.idx.zext") || strings.Contains(ir, "and i8 %idx.zext") {
		t.Errorf("IR should not contain 'and i8' on idx.zext register (value is i64, not i8), got:\n%s", ir)
	}

	// The store should be: store i64 %zext, i64* (correct)
	// NOT: store i8 %idx.zext.X (wrong — value is i64, not i8)
	if strings.Contains(ir, "store i8 %vec.idx.zext") || strings.Contains(ir, "store i8 %idx.zext") {
		t.Errorf("IR should not contain 'store i8' on idx.zext register (value is i64, not i8), got:\n%s", ir)
	}
}

// TestByteSliceIndexNoTypeMismatch verifies that []byte indexing does not
// produce a type mismatch between i64 and i8.
func TestByteSliceIndexNoTypeMismatch(t *testing.T) {
	src := `
main = () {
    v = 'abc'.to-bytes()
    c = v[0]
    print(c.to-str())
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

	// No type mismatch patterns
	if strings.Contains(ir, "and i8 %vec.idx.zext") || strings.Contains(ir, "and i8 %idx.zext") {
		t.Errorf("IR should not contain 'and i8' on idx.zext register, got:\n%s", ir)
	}
	if strings.Contains(ir, "store i8 %vec.idx.zext") || strings.Contains(ir, "store i8 %idx.zext") {
		t.Errorf("IR should not contain 'store i8' on idx.zext register, got:\n%s", ir)
	}
}

// TestVecPushFromIndex verifies that vec-push with a value from another vec
// index does not produce type mismatches.
func TestVecPushFromIndex(t *testing.T) {
	src := `
main = () {
    src = 'abc'.to-bytes()
    dst = vec-init-byte(3)
    i <- [0..3): {
        dst.push(src[i])
    }
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

	// No type mismatch patterns
	if strings.Contains(ir, "and i8 %vec.idx.zext") || strings.Contains(ir, "and i8 %idx.zext") {
		t.Errorf("IR should not contain 'and i8' on idx.zext register, got:\n%s", ir)
	}
	if strings.Contains(ir, "store i8 %vec.idx.zext") || strings.Contains(ir, "store i8 %idx.zext") {
		t.Errorf("IR should not contain 'store i8' on idx.zext register, got:\n%s", ir)
	}
}

// TestBitwiseNotOnIndex verifies that ~s[i] (bitwise NOT on a string index)
// does not produce type mismatches.
func TestBitwiseNotOnIndex(t *testing.T) {
	src := `
main = () {
    s = 'abc'.to-bytes()
    c = ~s[0]
    print(c.to-str())
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

	// No type mismatch patterns
	if strings.Contains(ir, "and i8 %vec.idx.zext") || strings.Contains(ir, "and i8 %idx.zext") {
		t.Errorf("IR should not contain 'and i8' on idx.zext register, got:\n%s", ir)
	}
}
