package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsExplicitTypeAnnotation verifies that when a variable
// has an explicit type annotation (e.g. `t u64 = 0`), the declared type
// is used for type checking, not the inferred type from the value (which
// would be "i64" for an integer literal).
func TestValidateFuncArgsExplicitTypeAnnotation(t *testing.T) {
	src := `compress = (h []u64, block []byte, t u64, last i64) {}

blake2b = (data []byte) (hash [64]byte) {
    h []u64
    t u64 = 0
    offset = 0
    for offset + 128 < data.len {
        t = (t + 128) & 18446744073709551615
        compress(h, data[offset..], t, 0)
        offset = offset + 128
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
	}
	if len(results) != 0 {
		t.Fatalf("expected no type errors, got %d", len(results))
	}
}
