package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestBug15MultiReturnBoolSignature verifies that bool output parameters use i64*
// (not i1*) in the function signature, matching the caller's i64 allocation.
// Before the fix, the function signature used i1* while the caller passed i64*,
// causing the function to write only 1 byte but the caller to read 8 bytes,
// resulting in garbage in the upper 7 bytes.
func TestBug15MultiReturnBoolSignature(t *testing.T) {
	src := `
parse-file = (path str) (a str, b str, n i64, ok bool) {
    a = ''
    b = ''
    n = 0
    ok = true
}

main = () () {
    a str; b str; n i64; ok bool
    a, b, n, ok = parse-file('/tmp/test.txt')
    print('ok=' - util.i64-to-str(ok))
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

	// The function signature must use i64* for the bool output parameter (ok),
	// not i1*. With i1*, the function writes only 1 byte but the caller reads 8.
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// Check that no function definition uses i1* for an output parameter
		if strings.HasPrefix(trimmed, "define ") && strings.Contains(trimmed, "i1*") {
			t.Errorf("Function signature uses i1* (should be i64* for bool output params): %s", line)
		}
	}

	// Verify the function signature contains i64* %ok
	if !strings.Contains(ir, "i64* %ok") {
		t.Errorf("Expected i64* %%ok in IR for bool output parameter, not found")
	}
}

// TestBug15SimpleBoolSignature verifies the simple 2-return case (str + bool)
// also uses i64* for the bool output parameter.
func TestBug15SimpleBoolSignature(t *testing.T) {
	src := `
simple-bool = () (s str, ok bool) {
    s = 'hello'
    ok = true
}

main = () () {
    a2 str; ok2 bool
    a2, ok2 = simple-bool()
    print('ok2=' - util.i64-to-str(ok2))
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

	// Check that no function definition uses i1* for an output parameter
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "define ") && strings.Contains(trimmed, "i1*") {
			t.Errorf("Function signature uses i1* (should be i64* for bool output params): %s", line)
		}
	}

	// Verify the function signature contains i64* %ok
	if !strings.Contains(ir, "i64* %ok") {
		t.Errorf("Expected i64* %%ok in IR for bool output parameter, not found")
	}
}

// TestBug15BoolStoreI64 verifies that storing true/false to a bool output
// parameter generates store i64 (not store i1), ensuring all 8 bytes are
// written and the caller reads a clean value.
func TestBug15BoolStoreI64(t *testing.T) {
	src := `
set-bool = () (ok bool) {
    ok = true
}

main = () () {
    ok bool
    ok = set-bool()
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

	// Check that no store i1 is generated for the bool output parameter
	// (store i64 1 is correct, store i1 1 only writes 1 byte)
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store i1 1,") {
			t.Errorf("Found store i1 1 (should be store i64 1 for bool output param): %s", line)
		}
	}
}
