package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// The vec-clear / vec-truncate / str-clear / str-truncate codegen paths register
// a distinctive register prefix each; count definition lines of one prefix.
func countRegDefs(ir string, prefix string) int {
	n := 0
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, prefix) && strings.Contains(trimmed, " = ") {
			n++
		}
	}
	return n
}

func mustGenerate(t *testing.T, src string) string {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	g := NewGenerator()
	return g.Generate(prog)
}

// TestStructFieldVecClearEmitted verifies that `.data.clear()` on a struct
// field actually emits the vec-clear codegen (store i64 0 to the vec len
// field through the field address).
//
// Bug 07 follow-up: the vec-clear case in genForwardFunc only accepted an
// Identifier receiver. For a struct field receiver (DotExpression, e.g.
// .data inside a builder method), it silently returned "" without emitting
// anything, so .data.clear() was a silent no-op (len stayed unchanged).
func TestStructFieldVecClearEmitted(t *testing.T) {
	ir := mustGenerate(t, `
builder {
    data []i64
}

builder.reset = () {
    .data.clear()
}

main = () {
    b = builder {}
    b.reset()
    print(b.data.len)
}
`)

	// vec-clear emits exactly one "%vc.len.gep.N = getelementptr inbounds %vec ..."
	if got := countRegDefs(ir, "%vc.len.gep."); got != 1 {
		t.Errorf("struct field .data.clear() must emit exactly 1 vec-clear len GEP, got %d.\nIR:\n%s", got, ir)
	}
	// and it must store 0 into that len field
	if !strings.Contains(ir, "store i64 0, i64* %vc.len.gep.") {
		t.Errorf("vec-clear must store i64 0 into the len field.\nIR:\n%s", ir)
	}
}

// TestStructFieldVecTruncateEmitted verifies that `.data.truncate(n)` on a
// struct field emits the vec-truncate codegen instead of being mis-dispatched
// to the POSIX truncate(path, len) C library call.
//
// Bug 07 follow-up: for a chained method call (.data.truncate(1)) fnName was
// reduced to the bare method name "truncate", which matched the global POSIX
// truncate CLibCall — generating `call i32 @truncate(i8* 1, i64 0)` with
// garbage arguments and failing LLVM verification. Even without that, the
// vec-truncate case only accepted Identifier receivers, so it could never
// handle the field form.
func TestStructFieldVecTruncateEmitted(t *testing.T) {
	ir := mustGenerate(t, `
builder {
    data []i64
}

builder.shrink = () {
    .data.truncate(1)
}

main = () {
    b = builder {}
    b.shrink()
    print(b.data.len)
}
`)

	// Must NOT generate a call to the POSIX truncate C function.
	// (A bare `declare i32 @truncate(...)` may still appear in the preamble —
	// that is a harmless global declaration, not a call.)
	if strings.Contains(ir, "call i32 @truncate(") {
		t.Errorf("struct field .data.truncate(n) must not be dispatched to the POSIX truncate CLibCall.\nIR:\n%s", ir)
	}
	// Must emit exactly one vec-truncate final-len computation.
	if got := countRegDefs(ir, "%vt.final-len."); got != 1 {
		t.Errorf("struct field .data.truncate(n) must emit exactly 1 vec-truncate final-len, got %d.\nIR:\n%s", got, ir)
	}
}

// TestStructFieldStrClearTruncateEmitted covers the str-typed struct field
// forms (.name.clear() / .name.truncate(n)) — same silent no-op bug family.
func TestStructFieldStrClearTruncateEmitted(t *testing.T) {
	ir := mustGenerate(t, `
rec {
    name str
}

rec.reset = () {
    .name.clear()
}

rec.shrink = () {
    .name.truncate(1)
}

main = () {
    r = rec {}
    r.reset()
    r.shrink()
    print(r.name.len)
}
`)

	if strings.Contains(ir, "call i32 @truncate(") {
		t.Errorf("struct field .name.truncate(n) must not be dispatched to the POSIX truncate CLibCall.\nIR:\n%s", ir)
	}
	if got := countRegDefs(ir, "%sc.len.gep."); got != 1 {
		t.Errorf("struct field .name.clear() must emit exactly 1 str-clear len GEP, got %d.\nIR:\n%s", got, ir)
	}
	if !strings.Contains(ir, "store i64 0, i64* %sc.len.gep.") {
		t.Errorf("str-clear must store i64 0 into the len field.\nIR:\n%s", ir)
	}
	if got := countRegDefs(ir, "%st.final-len."); got != 1 {
		t.Errorf("struct field .name.truncate(n) must emit exactly 1 str-truncate final-len, got %d.\nIR:\n%s", got, ir)
	}
}

// TestLocalVecClearTruncate guards the plain Identifier receiver paths: they
// must keep emitting exactly one expansion each.
func TestLocalVecClearTruncate(t *testing.T) {
	ir := mustGenerate(t, `
main = () {
    v []i64
    v.clear()
    v.truncate(0)
    s str
    s.clear()
    s.truncate(0)
}
`)

	if got := countRegDefs(ir, "%vc.len.gep."); got != 1 {
		t.Errorf("local v.clear() must emit exactly 1 vec-clear GEP, got %d.\nIR:\n%s", got, ir)
	}
	if got := countRegDefs(ir, "%vt.final-len."); got != 1 {
		t.Errorf("local v.truncate(0) must emit exactly 1 vec-truncate final-len, got %d.\nIR:\n%s", got, ir)
	}
	if got := countRegDefs(ir, "%sc.len.gep."); got != 1 {
		t.Errorf("local s.clear() must emit exactly 1 str-clear GEP, got %d.\nIR:\n%s", got, ir)
	}
	if got := countRegDefs(ir, "%st.final-len."); got != 1 {
		t.Errorf("local s.truncate(0) must emit exactly 1 str-truncate final-len, got %d.\nIR:\n%s", got, ir)
	}
}
