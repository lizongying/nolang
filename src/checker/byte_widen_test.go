package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsByteWidening verifies that a byte expression (e.g.
// array element access with bitwise AND) can be implicitly widened to i64
// when passed as a function argument.
//
// Reproduces: "argument 1 of 'inv-mix-col': expected 'i64', got 'byte'"
// where state is []byte and the call is inv-mix-col(state[0] & 255, ...)
func TestValidateFuncArgsByteWidening(t *testing.T) {
	src := `inv-mix-col = (b0 i64, b1 i64, b2 i64, b3 i64) (d0 i64, d1 i64, d2 i64, d3 i64) {
    d0 = b0
    d1 = b1
    d2 = b2
    d3 = b3
}

inv-mix-columns = (state []byte) {
    d0 = 0
    d1 = 0
    d2 = 0
    d3 = 0
    d0, d1, d2, d3 = inv-mix-col(state[0] & 255, state[1] & 255, state[2] & 255, state[3] & 255)
    state[0] = d0
}`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected 'i64', got 'byte'") {
			t.Errorf("false positive: byte should widen to i64: %s", r.Message)
		}
	}
}

// TestValidateFuncArgsByteWideningDirectArrayElement verifies that a plain
// byte array element (without bitwise AND) also widens to i64.
func TestValidateFuncArgsByteWideningDirectArrayElement(t *testing.T) {
	src := `take-i64 = (x i64) {}

process = (buf [16]byte) {
    take-i64(buf[0])
}`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected 'i64', got 'byte'") {
			t.Errorf("false positive: byte should widen to i64: %s", r.Message)
		}
	}
}

// TestValidateFuncArgsNarrowWideningAllTypes verifies implicit widening
// across various integer type pairs.
func TestValidateFuncArgsNarrowWideningAllTypes(t *testing.T) {
	src := `take-i64 = (x i64) {}
take-i32 = (x i32) {}
take-u32 = (x u32) {}

test = (b byte, u8v u8, i8v i8, i16v i16, u16v u16) {
    // byte → i64 (widening, should pass)
    take-i64(b)
    // u8 → i64 (widening, should pass)
    take-i64(u8v)
    // i8 → i64 (widening, should pass)
    take-i64(i8v)
    // i16 → i32 (widening, should pass)
    take-i32(i16v)
    // u16 → u32 (widening, should pass)
    take-u32(u16v)
}`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	if len(results) != 0 {
		for _, r := range results {
			t.Errorf("unexpected error: L%d:C%d %s", r.Line, r.Column, r.Message)
		}
	}
}

// TestValidateFuncArgsNarrowingStillErrors verifies that narrowing
// conversions (e.g. i64 → u8) still produce errors for non-literal
// expressions.
func TestValidateFuncArgsNarrowingStillErrors(t *testing.T) {
	src := `take-u8 = (x u8) {}

test = (n i64) {
    take-u8(n)
}`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected 'u8', got 'i64'") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a narrowing error for i64 → u8, but got none")
	}
}
