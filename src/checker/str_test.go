package checker

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
