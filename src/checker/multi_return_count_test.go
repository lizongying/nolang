package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestMultiReturnCountMismatch verifies that the checker reports an error
// when the number of assignment targets doesn't match the function's
// return value count.
func TestMultiReturnCountMismatch(t *testing.T) {
	src := `two = () (a i64, b i64) {
    a = 1
    b = 2
}

one = () (a i64) {
    a = 42
}

main = () {
    x = 0
    y = 0
    x, y = two()
    x = one()
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
		t.Fatalf("expected no validation errors for correct usage, got %d", len(results))
	}
}

// TestMultiReturnTooManyTargets verifies that assigning more targets than
// return values triggers a validation error.
func TestMultiReturnTooManyTargets(t *testing.T) {
	src := `one = () (a i64) {
    a = 42
}

main = () {
    x = 0
    y = 0
    x, y = one()
}
`
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
		if strings.Contains(r.Message, "returns 1 value(s) but 2 target(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about return count mismatch (1 return, 2 targets), got %d results: %v", len(results), results)
	}
}

// TestMultiReturnTooManyReturns verifies that assigning fewer targets than
// return values triggers a validation error (e.g. 3 returns, 2 targets).
func TestMultiReturnTooManyReturns(t *testing.T) {
	src := `three = () (a i64, b i64, c i64) {
    a = 1
    b = 2
    c = 3
}

main = () {
    x = 0
    y = 0
    x, y = three()
}
`
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
		if strings.Contains(r.Message, "returns 3 value(s) but 2 target(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about return count mismatch (3 returns, 2 targets), got %d results: %v", len(results), results)
	}
}

// TestMultiReturnBuiltinCount verifies that built-in functions returning
// multiple values (e.g. stat-size returns i64, bool) are correctly checked.
func TestMultiReturnBuiltinCount(t *testing.T) {
	// stat-size returns (i64, bool) — 2 values
	// Assigning to only 1 target should error
	src := `main = () {
    s = 0
    ok = false
    s, ok, extra = stat-size('/tmp')
}
`
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
		if strings.Contains(r.Message, "returns 2 value(s) but 3 target(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error about builtin return count mismatch, got %d results: %v", len(results), results)
	}
}

// TestMultiReturnBuiltinCorrect verifies that correctly using a built-in
// function with multiple returns does not trigger an error.
func TestMultiReturnBuiltinCorrect(t *testing.T) {
	src := `main = () {
    s = 0
    ok = false
    s, ok = stat-size('/tmp')
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
	// Should not have any return count mismatch errors
	for _, r := range results {
		if strings.Contains(r.Message, "value(s) but") && strings.Contains(r.Message, "target(s)") {
			t.Fatalf("unexpected return count error: %s", r.Message)
		}
	}
}
