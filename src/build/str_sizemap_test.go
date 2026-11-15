package build

import (
	"os"
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

// TestStrLenReadOnlyExplicitTypeNonStr pins the qon2wsev false positive that
// surfaced as "tests/byte-option-index.no:680:9: cannot modify read-only field
// 'len' of string 'r'". The reported file was just the CLI's entry file: this
// diagnostic carries no File, so the line/column came from src/std/bigint.no
// while the file name was stamped on afterwards.
//
// Root cause: buildSizeMaps pre-scans functions with a single str result into
// funcStrReturns, keyed by BARE function name. In a merged program that table
// is program-wide, so a same-named str-returning function in another module
// (str.copy) makes `r = .copy()` inside bigint.div-mod look like a string.
// That inferred name lands in stringSizes["r"], and the legal `r.len = 1`
// (r is explicitly declared bigint) is rejected. Explicit annotations now win
// over the expression heuristic, and the per-function pass drops parameters /
// results explicitly declared as non-str.
func TestStrLenReadOnlyExplicitTypeNonStr(t *testing.T) {
	// A same-named str-returning function plus an explicitly non-str result
	// that is re-assigned via that call must NOT be rejected.
	noFalsePositive := `
copy = () (out str) {
    out = 'x'
}
box.div-mod = (b box) (q box, r box) {
    r = .copy()
    r.len = 1
}
`
	// The genuine violation must still be rejected even when the assignment
	// goes through a str-returning function call.
	stillRejected := `
copy = () (out str) {
    out = 'x'
}
f = (s str) {
    s = copy()
    s.len = 1
}
`
	validate := func(t *testing.T, src string) error {
		t.Helper()
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("parse errors: %v", errs)
		}
		sm := buildSizeMaps(prog)
		validateFuncStrReturns = sm.funcStrReturns
		return validateArrayBounds(prog, sm.arraySizes, sm.sliceSizes, sm.stringSizes, buildVarTypes(prog))
	}

	if err := validate(t, noFalsePositive); err != nil {
		t.Fatalf("explicitly non-str 'r' was misreported as a string: %v", err)
	}
	if err := validate(t, stillRejected); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("genuine str.len assignment must still fail, got: %v", err)
	}
}

// TestBigintDivModLenNoFalsePositive replays the exact source from the report:
// src/std/bigint.no:680 (`r.len = 1` in bigint.div-mod) must survive validation
// even when the merged program contains a same-named str-returning function.
func TestBigintDivModLenNoFalsePositive(t *testing.T) {
	data, err := os.ReadFile("../std/bigint.no")
	if err != nil {
		t.Fatalf("read src/std/bigint.no: %v", err)
	}
	src := string(data) + "\ncopy = () (out str) {\n    out = 'x'\n}\n"
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	sm := buildSizeMaps(prog)
	validateFuncStrReturns = sm.funcStrReturns
	if err := validateArrayBounds(prog, sm.arraySizes, sm.sliceSizes, sm.stringSizes, buildVarTypes(prog)); err != nil {
		t.Fatalf("bigint.div-mod must not trip qon2wsev: %v", err)
	}
}
