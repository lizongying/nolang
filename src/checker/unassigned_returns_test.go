package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestUnassignedReturnsWarning verifies that the checker reports a warning
// when a named result parameter is never assigned in the function body.
func TestUnassignedReturnsWarning(t *testing.T) {
	src := `foo = () (a i64, b i64) {
    a = 42
    // b is never assigned — will be zero-filled
}

main = () {
    x = 0
    y = 0
    x, y = foo()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	found := false
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "result parameter 'b'") && strings.Contains(r.Message, "never assigned") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected warning about unassigned result parameter 'b', got %d results: %v", len(results), results)
	}
}

// TestAllReturnsAssigned verifies that no warning is reported when all
// result parameters are assigned.
func TestAllReturnsAssigned(t *testing.T) {
	src := `foo = () (a i64, b i64) {
    a = 42
    b = 99
}

main = () {
    x = 0
    y = 0
    x, y = foo()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when all returns are assigned, got %d: %v", len(results), results)
	}
}

// TestNullableReturnSkipped verifies that nullable result parameters are
// not reported by ValidateUnassignedReturns (they are handled by
// ValidateUninitOutputParams instead).
func TestNullableReturnSkipped(t *testing.T) {
	src := `foo = () (a i64, b ?str) {
    a = 42
    // b is nullable and never assigned — handled by ValidateUninitOutputParams
}

main = () {
    x = 0
    x = foo()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "'b'") {
			t.Fatalf("nullable parameter 'b' should not be reported by ValidateUnassignedReturns: %s", r.Message)
		}
	}
}

// TestElementAssignedReturnNotWarned verifies that a named result parameter
// populated via element assignment (out[i] = ...) or field assignment
// (out.field = ...) is recognized as assigned and does NOT trigger i3k422u3.
// This guards against the regression where ValidateUnassignedReturns only
// tracked bare-identifier assignment targets and falsely flagged element-
// assigned result params (a very common pattern in std, e.g. set.intersection).
func TestElementAssignedReturnNotWarned(t *testing.T) {
	src := `make-vec = () (out []i64) {
    out = with-len(3)
    out[0] = 1
    out[1] = 2
    out[2] = 3
}

main = () {
    a = make-vec()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when result param is populated via element assignment, got %d: %v", len(results), results)
	}
}

// TestElementAssignedReturnInLoopNotWarned verifies that a named result
// parameter populated via element assignment (dst[i] = ...) *inside a range
// loop* (i <- [0..n): { ... }) is recognized as assigned. Inside loop bodies
// such assignments are parsed as ExpressionStatement→AssignExpression rather
// than MultiAssignStatement, so this exercises the AssignExpression branch of
// collectAssignedNamesInExpr (this is the dominant i3k422u3 false-positive
// pattern in std, e.g. txt.copy / set.intersection).
func TestElementAssignedReturnInLoopNotWarned(t *testing.T) {
	src := `copy-txt = () (dst txt) {
    n i64 = 3
    i <- [0..n): {
        dst[i] = i
    }
    dst.len = n
}

main = () {
    a = copy-txt()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when result param is populated via element assignment inside a loop, got %d: %v", len(results), results)
	}
}

// TestMethodReceiverReturnAssigned verifies that a named result parameter
// initialized via a method call on itself (p.init(...), entries.push(...),
// w.init-boundary(...)) is recognized as assigned and does NOT trigger
// i3k422u3. These are mutation-assignment patterns the checker can only
// detect by treating the call receiver as the assigned target.
func TestMethodReceiverReturnAssigned(t *testing.T) {
	src := `make-pool = (h str, pt i64) (p pool) {
    p.init(h, pt, 8)
}

main = () {
    x = make-pool('a', 1)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when result param is initialized via method call on itself, got %d: %v", len(results), results)
	}
}

// TestRangeForLoopVarReturnAssigned verifies that a named result parameter
// used as the loop variable of a range-for (`p <- [pos..n): {...}`) is
// recognized as assigned. The loop variable is always assigned by iteration,
// so flagging it as "never assigned" is a false positive.
func TestRangeForLoopVarReturnAssigned(t *testing.T) {
	src := `skip-ws = (s str, pos i64) (p i64) {
    p <- [pos..s.len-bytes()): {
        c = s[p]
        c != 32 -> return
    }
}

main = () {
    x = skip-ws('  x', 0)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when result param is the range-for loop variable, got %d: %v", len(results), results)
	}
}

// TestIntrinsicAnnotationSuppressesUnassigned verifies that a function marked
// with #{intrinsic} is exempt from the i3k422u3 "never assigned" check even if
// its named result parameter is never assigned in the nolang source (it is
// populated by codegen / a builtin / an out-parameter reference).
func TestIntrinsicAnnotationSuppressesUnassigned(t *testing.T) {
	src := `#{intrinsic}
count = () (n i64) {
    ; LLVM: call os.args
}

main = () {
    x = count()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected #{intrinsic} to suppress the unassigned-returns warning, got %d: %v", len(results), results)
	}
}

// TestMultiAssignReturnsAssigned verifies that result parameters assigned
// via multi-assignment (e.g. a, b = func()) are recognized as assigned.
func TestMultiAssignReturnsAssigned(t *testing.T) {
	src := `bar = () (r i64, s i64) {
    r, s = quux()
}

quux = () (x i64, y i64) {
    x = 1
    y = 2
}

main = () {
    a = 0
    b = 0
    a, b = bar()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no warnings when returns are assigned via multi-assign, got %d: %v", len(results), results)
	}
}
