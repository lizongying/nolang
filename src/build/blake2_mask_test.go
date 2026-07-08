package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateBlake2MaskConst tests that BLAKE2B-MASK u64 = 18446744073709551615
// is not reported as f64 by the type checker.
func TestValidateBlake2MaskConst(t *testing.T) {
	src := `BLAKE2B-MASK u64 = 18446744073709551615

rotr64 = (x u64, n i64) (r u64) {
    r = ((x >> n) | (x << (64 - n))) & BLAKE2B-MASK
}

blake2b-g = (v []u64, a i64, b i64, c i64, d i64, x u64, y u64) {
    v[a] = (v[a] + v[b] + x) & BLAKE2B-MASK
    v[d] = rotr64(v[d] ^ v[a], 32)
    v[c] = (v[c] + v[d]) & BLAKE2B-MASK
    v[b] = rotr64(v[b] ^ v[c], 24)
    v[a] = (v[a] + v[b] + y) & BLAKE2B-MASK
    v[d] = rotr64(v[d] ^ v[a], 16)
    v[c] = (v[c] + v[d]) & BLAKE2B-MASK
    v[b] = rotr64(v[b] ^ v[c], 63)
}

blake2b-compress = (h []u64, block []byte, t u64, last i64) {
    v []u64
    i = 0
    for i < 8 {
        v[i] = h[i]
        i = i + 1
    }
    v[12] = (v[12] ^ (t & BLAKE2B-MASK)) & BLAKE2B-MASK
    if last != 0 {
        v[14] = v[14] ^ BLAKE2B-MASK
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	t.Log("=== ValidateTypes ===")
	typeErrs := ValidateTypes(prog)
	for _, r := range typeErrs {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}

	t.Log("=== ValidateFuncArgs ===")
	funcArgErrs := ValidateFuncArgs(prog, "")
	for _, r := range funcArgErrs {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}

	totalErrors := len(typeErrs) + len(funcArgErrs)
	if totalErrors != 0 {
		t.Fatalf("expected no type errors, got %d", totalErrors)
	}
}
