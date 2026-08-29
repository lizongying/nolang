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
	// Note: []byte.len may exist as a function definition (from byte.no), so we only
	// check that the test-len function body does not contain a CALL to it.
	// (Like TestSliceMethodLenCallOnStr, we check the function body, not the whole IR.)
	testLenIdx2 := strings.Index(llvmIR, "test-len")
	if testLenIdx2 < 0 {
		t.Fatalf("test-len function not found in LLVM IR (pre-check)")
	}
	endIdx2 := strings.Index(llvmIR[testLenIdx2:], "}")
	if endIdx2 < 0 {
		endIdx2 = len(llvmIR) - testLenIdx2
	}
	fnBodyCheck := llvmIR[testLenIdx2 : testLenIdx2+endIdx2]
	if strings.Contains(fnBodyCheck, "call") && strings.Contains(fnBodyCheck, "_LB__RB_byte.len") {
		t.Errorf("test-len function body contains call to []byte.len function:\n%s", fnBodyCheck)
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
// Unlike []byte.len (which is only a builtin), str.len is both a builtin
// (ForwardFunc: str-len) AND a user-defined function in str.no. When .len()
// is called inside a str method, the codegen should dispatch to the builtin
// len handler (inline field access), NOT generate a call to @str.len.
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

	// The test-len function body should NOT contain a call to @str.len.
	// (str.len IS defined in str.no, but .len() should be inlined to a
	// field access, not dispatched through a function call.)
	if strings.Contains(fnBody, "call") && strings.Contains(fnBody, "@str.len") {
		t.Errorf("test-len function body contains call to @str.len instead of inline field access:\n%s", fnBody)
	}

	// For str, .len() should extract the string length via a load instruction
	if !strings.Contains(fnBody, "load") {
		t.Errorf("test-len function body does not contain 'load' for len field:\n%s", fnBody)
	}
}
