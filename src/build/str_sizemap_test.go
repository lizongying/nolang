package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStrLenReadOnly verifies that str.len cannot be modified
func TestStrLenReadOnly(t *testing.T) {
	src := `
test = () {
	s = 'hello'
	s.len = 10
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// read-only check is in validateArrayBounds（使用合併後的單次遍歷）
	sizeMap := buildSizeMaps(prog)
	validateFuncStrReturns = sizeMap.funcStrReturns
	varTypes := map[string]string{}
	err := validateArrayBounds(prog, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes)
	if err == nil {
		t.Fatal("expected read-only error for str.len assignment, but got none")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected read-only error, got: %v", err)
	}
}

// TestBytarithSubFuncReturnStr verifies that when a str variable is assigned
// from a user-defined function call returning str (without an explicit type
// annotation), the variable is correctly tracked as a string in stringSizes.
// This prevents s - 'x' from being misidentified as byte arithmetic (i64)
// instead of string concatenation (str).
//
// Regression for bug01-bytarith-sub:
//   - Root cause: isStringExprForCollect did not recognize calls to user-defined
//     str-returning functions, so the variable was not added to stringSizes.
//   - Fix: buildSizeMaps pre-scans for functions returning str and passes the
//     result to isStringExprForCollect via the funcStrReturns map.
func TestBytarithSubFuncReturnStr(t *testing.T) {
	src := `
get-str = () (s str) {
    s = 'hello'
}

main = () () {
    s = get-str()
    result str = s - ' '
    print(result)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	sizeMap := buildSizeMaps(prog)

	// Verify get-str is recognized as a str-returning function
	if !sizeMap.funcStrReturns["get-str"] {
		t.Errorf("expected get-str in funcStrReturns, got: %v", sizeMap.funcStrReturns)
	}

	// Verify s is tracked as a string variable (inferred from get-str() call)
	if _, ok := sizeMap.stringSizes["s"]; !ok {
		t.Errorf("expected 's' in stringSizes (inferred from get-str() call), got: %v", sizeMap.stringSizes)
	}

	// The validation should pass without "cannot assign non-string value" error
	validateFuncStrReturns = sizeMap.funcStrReturns
	varTypes := map[string]string{}
	err := validateArrayBounds(prog, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
}

// TestBytarithSubExplicitStrType verifies that the original case (with explicit
// str type annotation) also works correctly.
func TestBytarithSubExplicitStrType(t *testing.T) {
	src := `
get-str = () (s str) {
    s = 'hello'
}

main = () () {
    s str = get-str()
    result str = s - ' '
    print(result)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	sizeMap := buildSizeMaps(prog)
	validateFuncStrReturns = sizeMap.funcStrReturns
	varTypes := map[string]string{}
	err := validateArrayBounds(prog, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes)
	if err != nil {
		t.Fatalf("expected no validation error, got: %v", err)
	}
}
