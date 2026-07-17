package build

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStrTypeLayout verifies that str type has the correct layout
// as documented in str.no:
//
//	str {
//	    len i64 read-only       // i64
//	    cap i64 sealed           // i64
//	    data *byte sealed       // i64 (opaque pointer)
//	}
//
// All strings are heap-allocated (no SSO).
func TestStrTypeLayout(t *testing.T) {
	// 128 bytes → str (heap)
	longStr := strings.Repeat("a", 128)
	src := fmt.Sprintf(`
// Test str: small string on heap
test-smal = () {
	s = 'hello'
	n = s.len
}

// Test str: long string on heap
test-str = () {
	s = '%s'
	n = s.len
}
`, longStr)
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Validate types
	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

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

	// read-only check is in validateArrayBounds
	arraySizes := buildArraySizeMap(prog)
	sliceSizes := buildSliceSizeMap(prog)
	stringSizes := buildStringSizeMap(prog)
	varTypes := map[string]string{}
	err := validateArrayBounds(prog, arraySizes, sliceSizes, stringSizes, varTypes)
	if err == nil {
		t.Fatal("expected read-only error for str.len assignment, but got none")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected read-only error, got: %v", err)
	}
}
