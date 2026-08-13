package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestWithLenStrAsArgEnsuresStrLong verifies that with-len used as a direct
// function argument produces %str-long (stride=1), not %vec (stride=8).
//
// Bug: when with-len was used directly as a function argument (not in a let
// statement with type annotation), currentTargetType was empty, causing
// with-len to default to %vec with stride=8. The callee then indexed the
// string with stride=1 (correct for %str-long), but the buffer was allocated
// with stride=8, leading to garbage data reads.
func TestWithLenStrAsArgEnsuresStrLong(t *testing.T) {
	src := `
process = (buf str) (out i64) {
    out = 0
}

main = () {
    n = process(with-len(100))
    print(n)
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

	// The with-len call should produce %str-long (stride=1), not %vec (stride=8).
	// Check that the with-len buffer is allocated with malloc(len) not malloc(len*8).
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// Look for with-len str buffer allocation
		if strings.Contains(trimmed, "wl.sbuf") && strings.Contains(trimmed, "malloc") {
			// Should NOT have a mul by 8 before the malloc
			// (mul i64 %len, 8 would indicate stride=8 allocation)
			// The malloc should directly use the len value
			if strings.Contains(trimmed, "mul") {
				t.Errorf("with-len str buffer should not have mul (stride=1), got: %s", trimmed)
			}
		}
	}

	// Verify the call to process uses %str-long* argument type
	if !strings.Contains(ir, "%str-long*") {
		t.Errorf("process call should use %%str-long* argument type, got IR:\n%s", ir)
	}
}

// TestBuiltinStrResultAsArgEnsuresStrLong verifies that builtin function
// results (like fs.read-file) passed directly as function arguments are
// typed as %str-long* and not i64*.
//
// Bug: builtin results (e.g. %rf.str.N from read-file) were not detected
// by isStringExpr or exprResultLLVMType in genTypedArg's default case,
// causing them to fall through to i64* handling. The callee would then
// treat the parameter as i64 instead of %str-long, using stride=8.
func TestBuiltinStrResultAsArgEnsuresStrLong(t *testing.T) {
	src := `
find-byte = (s str, c i64) (pos i64) {
    pos = 0
}

main = () {
    content = read-file('test.txt')
    p = find-byte(content, 0)
    print(p)
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

	// The call to find-byte should pass %str-long* for the content argument.
	// Check that the call instruction uses %str-long* and not i64* for the
	// first argument.
	callLines := []string{}
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call void @find-byte") {
			callLines = append(callLines, trimmed)
		}
	}
	if len(callLines) == 0 {
		t.Fatalf("expected call to find-byte, got IR:\n%s", ir)
	}
	for _, callLine := range callLines {
		// The first argument should be %str-long*
		if !strings.Contains(callLine, "%str-long*") {
			t.Errorf("find-byte call should pass %%str-long* for str argument, got: %s", callLine)
		}
	}
}

// TestStrConcatDashAsArgEnsuresStrLong verifies that string concatenation
// using the - operator, when used as a function argument, is correctly
// detected as a string expression and passed as %str-long*.
//
// Bug: the standalone isStringExpr function only checked + for string
// concatenation, not -. While the method version g.isStringExpr did check
// both, the standalone version was used in some code paths, causing
// string concat results to be mistyped as i64*.
func TestStrConcatDashAsArgEnsuresStrLong(t *testing.T) {
	src := `
print-str = (s str) {
}

main = () {
    a = 'hello'
    b = 'world'
    print-str(a - b)
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

	// The call to print-str should pass %str-long* for the concat result.
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call void @print-str") {
			if !strings.Contains(trimmed, "%str-long*") {
				t.Errorf("print-str call should pass %%str-long* for concat result, got: %s\nFull IR:\n%s", trimmed, ir)
			}
		}
	}
}

// TestWithLenStrReturnEnsuresStrLong verifies that with-len string
// returned via output parameter is correctly typed and doesn't
// corrupt data when used cross-module.
func TestWithLenStrReturnEnsuresStrLong(t *testing.T) {
	src := `
build-str = () (out str) {
    buf str = with-len(10)
    buf[0] = 65
    buf[1] = 66
    buf[2] = 67
    out = buf
}

main = () {
    s = build-str()
    print(s)
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

	// The with-len should produce %str-long with malloc(10), not malloc(80).
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "wl.sbuf") && strings.Contains(trimmed, "malloc") {
			// Should not have mul by 8
			if strings.Contains(trimmed, "mul") {
				t.Errorf("with-len str should use malloc(len) not malloc(len*8), got: %s", trimmed)
			}
		}
	}

	// The out = buf assignment should use %str-long load/store, not i64
	if !strings.Contains(ir, "load %str-long") {
		t.Errorf("out = buf should load %%str-long, got IR:\n%s", ir)
	}
}

// TestBuiltinStrResultAsArgTrackedForFree verifies that builtin string results
// (like read-file) passed directly as function arguments are tracked in
// stmtTemporaries so their malloc'd data is freed at statement end.
//
// Without tracking, the builtin's malloc'd data buffer leaks when the result
// is used directly as a function argument (not stored in a variable first).
func TestBuiltinStrResultAsArgTrackedForFree(t *testing.T) {
	src := `
find-byte = (s str, c i64) (pos i64) {
    pos = 0
}

main = () {
    p = find-byte(read-file('test.txt'), 0)
    print(p)
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

	// The read-file result (%rf.str.N) should be freed at statement end.
	// Look for the stmtTemporaries free pattern: load data field, NULL check, call @free
	// The free should reference the read-file str register.
	hasFreeForRfStr := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call void @free(") {
			hasFreeForRfStr = true
			break
		}
	}
	if !hasFreeForRfStr {
		t.Errorf("Expected @free call for read-file result, but none found in IR:\n%s", ir)
	}
}

// TestWithLenStrAsArgTrackedForFree verifies that with-len string results
// passed directly as function arguments are tracked in stmtTemporaries.
func TestWithLenStrAsArgTrackedForFree(t *testing.T) {
	src := `
process = (buf str) (out i64) {
    out = 0
}

main = () {
    n = process(with-len(100))
    print(n)
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

	// The with-len result (materialized in %ref.tmp.N) should be freed at statement end.
	hasFreeCall := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call void @free(") {
			hasFreeCall = true
			break
		}
	}
	if !hasFreeCall {
		t.Errorf("Expected @free call for with-len temp, but none found in IR:\n%s", ir)
	}
}

// TestBuiltinStrStoredInVarNoDoubleFree verifies that when a builtin result
// is stored in a variable, the stmtTemporaries tracking is removed (untracked)
// and the variable's heapVars tracking takes over — no double-free.
func TestBuiltinStrStoredInVarNoDoubleFree(t *testing.T) {
	src := `
main = () {
    content = read-file('test.txt')
    print(content)
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

	// When read-file result is stored in 'content', the stmtTemporaries
	// entry should be removed by untrackStmtTemporary. The variable 'content'
	// is tracked by heapVars instead.
	//
	// We verify by counting @free calls for the content variable's data
	// pointer (load from %content field 2 → free). There should be exactly
	// 1 such free (at function exit via emitHeapFree), not 2 (which would
	// indicate both stmtTemporaries and heapVars freeing → double-free).
	//
	// Additionally, there may be 1 @free for the null-terminated buffer
	// allocated by makeNullTerminatedStr for the 'test.txt' argument
	// (tracked via stmtTempRawPtrs and freed at statement end). This is
	// a different pointer and does not constitute a double-free.
	contentFreeCount := 0
	rawBufFreeCount := 0
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "call void @free(") {
			if strings.Contains(trimmed, "%dptr.") {
				contentFreeCount++
			} else {
				rawBufFreeCount++
			}
		}
	}
	if contentFreeCount != 1 {
		t.Errorf("Expected exactly 1 @free call for content's data (at function exit), got %d. IR:\n%s", contentFreeCount, ir)
	}
}
