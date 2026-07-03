package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestSliceMethodLenCall verifies that .len() called inside a []byte method
// body is correctly resolved. The resolveSelfMethodCalls pass rewrites
// .len() to []byte.len(self), but []byte.len is not a user-defined function.
// The codegen must recognise the ".len" suffix and dispatch to the builtin
// len() handler, loading field 0 (i64 len) from the %vec struct.
func TestSliceMethodLenCall(t *testing.T) {
	src := `[]byte.test-len = () (n i64) {
    n = .len()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Run the transpiler pipeline
	t2 := NewTranspiler(nil)
	t2.sourcePath = "src/std/byte.no" // mark as std to avoid user-code restrictions
	llvmIR, err := t2.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// The LLVM IR should NOT contain a call to the non-existent []byte.len function.
	// In LLVM IR, "[]" is mangled to "_LB__RB_", so []byte.len becomes _LB__RB_byte.len
	if strings.Contains(llvmIR, "_LB__RB_byte.len") {
		t.Errorf("LLVM IR contains call to non-existent []byte.len function:\n%s", llvmIR)
	}

	// The test-len function should load the len field from the %vec struct
	// Find the test-len function body
	testLenIdx := strings.Index(llvmIR, "test-len")
	if testLenIdx < 0 {
		t.Fatalf("test-len function not found in LLVM IR")
	}
	// Extract a window around the function
	endIdx := strings.Index(llvmIR[testLenIdx:], "}")
	if endIdx < 0 {
		endIdx = len(llvmIR) - testLenIdx
	}
	fnBody := llvmIR[testLenIdx : testLenIdx+endIdx]

	if !strings.Contains(fnBody, "getelementptr") {
		t.Errorf("test-len function body does not contain getelementptr for len access:\n%s", fnBody)
	}

	if !strings.Contains(fnBody, "load i64") {
		t.Errorf("test-len function body does not contain 'load i64' for len field:\n%s", fnBody)
	}
}

// TestSliceMethodLenCallOnI64 verifies .len() works inside []i64 methods too.
func TestSliceMethodLenCallOnI64(t *testing.T) {
	src := `[]i64.test-len = () (n i64) {
    n = .len()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	t2 := NewTranspiler(nil)
	t2.sourcePath = "src/std/vec.no"
	llvmIR, err := t2.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if strings.Contains(llvmIR, "_LB__RB_i64.len") {
		t.Errorf("LLVM IR contains call to non-existent []i64.len function:\n%s", llvmIR)
	}

	// Find the test-len function body
	testLenIdx := strings.Index(llvmIR, "test-len")
	if testLenIdx < 0 {
		t.Fatalf("test-len function not found in LLVM IR")
	}
	endIdx := strings.Index(llvmIR[testLenIdx:], "}")
	if endIdx < 0 {
		endIdx = len(llvmIR) - testLenIdx
	}
	fnBody := llvmIR[testLenIdx : testLenIdx+endIdx]

	if !strings.Contains(fnBody, "load i64") {
		t.Errorf("test-len function body does not contain 'load i64' for len field:\n%s", fnBody)
	}
}

// TestSliceMethodLenCallOnStr verifies .len() works inside str methods too.
func TestSliceMethodLenCallOnStr(t *testing.T) {
	src := `str.test-len = () (n i64) {
    n = .len()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	t2 := NewTranspiler(nil)
	t2.sourcePath = "src/std/str.no"
	llvmIR, err := t2.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if strings.Contains(llvmIR, "@str.len") {
		t.Errorf("LLVM IR contains call to non-existent @str.len function:\n%s", llvmIR)
	}

	// Find the test-len function body
	testLenIdx := strings.Index(llvmIR, "test-len")
	if testLenIdx < 0 {
		t.Fatalf("test-len function not found in LLVM IR")
	}
	endIdx := strings.Index(llvmIR[testLenIdx:], "}")
	if endIdx < 0 {
		endIdx = len(llvmIR) - testLenIdx
	}
	fnBody := llvmIR[testLenIdx : testLenIdx+endIdx]

	// For str, .len() should extract the string length
	if !strings.Contains(fnBody, "load") {
		t.Errorf("test-len function body does not contain 'load' for len field:\n%s", fnBody)
	}
}
