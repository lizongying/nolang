package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestUserWriteFileOverridesBuiltin verifies that a user-defined `write-file`
// function takes precedence over the builtin ForwardFunc `write-file`.
//
// Bug: when a user defined `write-file = (path str, content str) { }`,
// the compiler still dispatched to the builtin write-file (ForwardFunc),
// which treats the second argument as %vec ([]byte) instead of %str-long.
// This caused: "store %vec %content.val.NNN, %vec* %vec.tmp.NNN" where
// %content.val.NNN was actually %str-long, leading to LLVM type mismatch.
func TestUserWriteFileOverridesBuiltin(t *testing.T) {
	src := `
write-file = (path str, content str) {
}

main = () {
    content = 'hello' - ' ' - 'world'
    write-file('test.txt', content)
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

	// The call to write-file should use %str-long* (user-defined),
	// NOT the builtin which uses %vec*.
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// Look for the builtin write-file pattern: %vec.tmp with store %vec
		if strings.Contains(trimmed, "store %vec %content.val") {
			t.Errorf("builtin write-file was used instead of user-defined write-file (str-long vs vec mismatch): %s", trimmed)
		}
	}

	// The user-defined write-file function should be called, not inlined.
	if !strings.Contains(ir, "define internal void @write-file") {
		t.Errorf("expected user-defined write-file function definition in IR")
	}
}

// TestUserReadFileOverridesBuiltin verifies that a user-defined `read-file`
// function takes precedence over the builtin ForwardFunc `read-file`.
func TestUserReadFileOverridesBuiltin(t *testing.T) {
	src := `
read-file = (path str) (content str) {
    content = 'dummy'
}

main = () {
    c = read-file('test.txt')
    print(c)
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

	// The user-defined read-file should be called, not the builtin.
	if !strings.Contains(ir, "define internal void @read-file") {
		t.Errorf("expected user-defined read-file function definition in IR")
	}
}

// TestUserReadOverridesBuiltin verifies that a user-defined `read` function
// with multiple return values is not shadowed by the builtin ForwardFunc `read`.
// This is also related to issue 2 (function name `read` conflict).
func TestUserReadOverridesBuiltin(t *testing.T) {
	src := `
read = (dir str) (content str, ok bool) {
    content = 'dummy'
    ok = true
}

main = () {
    c, ok = read('testdir')
    print(c)
    print(ok)
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

	// The user-defined read should be called, not the builtin.
	// "read" is in clibFuncNames, so the user function is emitted as @n.read
	if !strings.Contains(ir, "define internal void @n.read") {
		t.Errorf("expected user-defined read function definition in IR:\n%s", ir)
	}
}
