package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsArrayToSliceAndIntLitToU64 reproduces two type-checker
// false positives:
//   1. [N]T passed where []T is expected → should be allowed (array-to-slice)
//   2. Integer literal 0 passed where u64 is expected → should be allowed
func TestValidateFuncArgsArrayToSliceAndIntLitToU64(t *testing.T) {
	src := `blake-g = (v []u64, a i64, b i64, c i64, d i64, x u64, y u64) {
    v[a] = v[a]
}

compress = (x []byte, y []byte) (out []byte) {
    v [128]u64
    i = 0
    for i < 8 {
        base = i * 16
        blake-g(v, base + 0, base + 4, base + 8, base + 12, 0, 0)
        i = i + 1
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected '[]u64', got '[128]u64'") {
			t.Errorf("false positive (array-to-slice): %s", r.Message)
		}
		if strings.Contains(r.Message, "expected 'u64', got 'i64'") {
			t.Errorf("false positive (int-lit-to-u64): %s", r.Message)
		}
	}
}
