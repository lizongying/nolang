package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// REGRESSION GUARD (missing right operand used to be SILENT).
//
// parseInfixExpression skips NEWLINEs before the right operand so that
// multi-line expressions (`a +` ⏎ `b`) work. But if a `#{…}` line annotation
// sat between the trailing operator and the operand, parseExpression returned
// nil and the old code simply stored Right == nil and carried on. The 中綴 node
// then ended at the operator, the enclosing statement/block parse kept going,
// and the FUNCTION BODY WAS SILENTLY TRUNCATED — the program compiled and ran.
//
// That is how `des-block` in src/std/crypto/des.no lost its tail (the selector
// computation, the pre-output loop and the byte push loop) while `no build`
// still reported success, and it is why `tests/std-hash.no` produced
// wrong DES output with rc=0.
//
// The guard must therefore be loud. This test pins both halves: the broken
// shape errors, and the two legitimate shapes still parse.
func TestInfixMissingRightOperandIsError(t *testing.T) {
	// `|` at end of line, then a `#{…}` line, then the operand.
	src := `g = (ks [96]byte, i i64) (sk i64) {
    sk = ks[i] |
#{index-out = 0}
    ks[i + 1]
}
`
	p := New(lexer.New(src))
	p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatal("expected an error for a binary operator with no right operand, got none " +
			"(silent nil-Right is the body-truncation bug)")
	}
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "expected expression after operator '|'") {
		t.Fatalf("expected a 'missing right operand' error, got: %v", errs)
	}
	// The position must name the operator itself (line 2), not whatever token
	// the scanner happened to be parked on after skipping newlines.
	if !strings.Contains(joined, "line 2,") {
		t.Fatalf("error should point at the operator on line 2, got: %v", errs)
	}
}

// Control: the same expression broken across lines without an annotation is
// legal and must stay legal — the new guard keys off `Right == nil`, not off
// "operator at end of line".
func TestInfixMultiLineContinuationStillParses(t *testing.T) {
	src := `g = (ks [96]byte, i i64) (sk i64) {
    sk = ks[i] |
        ks[i + 1]
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("multi-line continuation must keep parsing, got: %v", errs)
	}
	fd, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("first statement = %T, want *FunctionDefinition", prog.Statements[0])
	}
	if n := len(fd.Body.Statements); n != 1 {
		t.Fatalf("body statement count = %d, want 1 (body must not be truncated/split)", n)
	}
}

// Control: `#{…}` on its own line ABOVE a multi-line statement is the correct
// spelling (the annotation applies to the whole statement) and must parse.
func TestAnnotationAboveMultiLineStatementParses(t *testing.T) {
	src := `h = (ks [96]byte, i i64) (sk i64) {
    #{index-out = 0}
    sk = (ks[i] << 8) |
        ks[i + 1]
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("annotation-above-statement must parse, got: %v", errs)
	}
	fd, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("first statement = %T, want *FunctionDefinition", prog.Statements[0])
	}
	if n := len(fd.Body.Statements); n != 1 {
		t.Fatalf("body statement count = %d, want 1", n)
	}
}
