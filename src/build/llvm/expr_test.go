package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestU32ToU64Assignment tests that u32 arithmetic assigned to u64 variables
// correctly auto-widens without explicit casts, including overflow-safe computation.
func TestU32ToU64Assignment(t *testing.T) {
	src := `
h0 u32 = 100
h1 u32 = 200
myval u64 = h0 | (h1 << 26)
myval2 u64 = h0 + h1
myval3 u64 = h0 * h1
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The IR should NOT contain zext from i32 to i64 for the OR result,
	// because target type propagation should compute in i64 directly.
	// It should contain or i64, add i64, mul i64 operations.
	if !strings.Contains(ir, "or i64") {
		t.Errorf("IR should contain 'or i64' for u64 = u32 | u32 with target propagation, got:\n%s", ir)
	}
	if !strings.Contains(ir, "add i64") {
		t.Errorf("IR should contain 'add i64' for u64 = u32 + u32 with target propagation, got:\n%s", ir)
	}
	if !strings.Contains(ir, "mul i64") {
		t.Errorf("IR should contain 'mul i64' for u64 = u32 * u32 with target propagation, got:\n%s", ir)
	}
}

// TestNoEmptyOperandInStore verifies that codegen never emits a `store` instruction
// with an empty value operand (regression for B1: `store i64 , i64* %fmtval`).
// The pattern that triggered B1 was: a function with output params called as an
// argument to another function (emitArgAsStrLong path). We approximate it here by
// generating IR for a small program with chained function calls + output params.
func TestNoEmptyOperandInStore(t *testing.T) {
	src := `
make-val = () (out i64) {
    out = 42
}

use-val = (n i64) (r i64) {
    r = n + 1
}

main = () {
    a = make-val()
    b = use-val(a)
    print(b.to-str())
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Look for the B1 signature: "store <ty> ," (empty value operand between type and comma)
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "store ") {
			continue
		}
		// A well-formed store looks like:  store i64 %x, i64* %p
		// The B1 bug produced:              store i64 , i64* %p   (note space-comma right after type)
		// Detect by checking that after the type token there is a non-empty operand before the comma.
		rest := strings.TrimPrefix(trimmed, "store ")
		// rest = "i64 %x, i64* %p"  or  "i64 , i64* %p" (bug)
		commaIdx := strings.Index(rest, ",")
		if commaIdx < 0 {
			continue
		}
		operand := strings.TrimSpace(rest[:commaIdx])
		// operand should be like "i64 %x" (type + value). If it's just "i64" (type only, no value), that's the bug.
		fields := strings.Fields(operand)
		if len(fields) < 2 {
			t.Errorf("codegen emitted store with empty value operand (B1 regression): %s", trimmed)
		}
	}
}

// TestNoEmptyOperandInGEP verifies that codegen never emits a `getelementptr`
// instruction with an empty pointer operand (regression for B19: GEP on %str-long
// with empty `%str-long* ,`).
func TestNoEmptyOperandInGEP(t *testing.T) {
	src := `
user {
    name str
    age i64
}

user.greet = () {
    print('hi ' - .name)
}

make-user = () (out user) {
    out = user {
        name: 'bob'
        age: 30
    }
}

main = () {
    u = make-user()
    u.greet()
    n = u.name
    print(n)
    print(u.age.to-str())
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// Look for the B19 signature: "getelementptr inbounds <ty>, <ty>* ," (empty ptr operand)
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "getelementptr ") {
			continue
		}
		// A well-formed GEP looks like:
		//   getelementptr inbounds %str-long, %str-long* %ptr, i32 0, i32 0
		// The B19 bug produced:
		//   getelementptr inbounds %str-long, %str-long* , i32 0, i32 0   (empty ptr after `*`)
		// Detect by splitting on commas and checking the second field has a non-empty operand after `*`.
		parts := strings.SplitN(trimmed, ",", 3)
		if len(parts) < 2 {
			continue
		}
		second := strings.TrimSpace(parts[1]) // e.g. "%str-long* %ptr" or "%str-long* "
		// Find the last `*` in this field; what follows should be a non-empty operand.
		starIdx := strings.LastIndex(second, "*")
		if starIdx < 0 {
			continue
		}
		afterStar := strings.TrimSpace(second[starIdx+1:])
		if afterStar == "" {
			t.Errorf("codegen emitted getelementptr with empty pointer operand (B19 regression): %s", trimmed)
		}
	}
}
