package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsBigintResultParam reproduces the issue where calling
// `normalize(q)` inside a function whose result parameter is `q bigint`
// is wrongly reported as "expected 'bigint', got 'i64'".
func TestValidateFuncArgsBigintResultParam(t *testing.T) {
	src := `bigint {
    sign i64
    len i64
    limbs [64]i64
}

normalize = (a bigint) {
    for a.len > 1 {
        a.len = a.len - 1
    }
}

zero = () (z bigint) {
    z.sign = 0
    z.len = 1
    z.limbs[0] = 0
}

one = () (o bigint) {
    o.sign = 1
    o.len = 1
    o.limbs[0] = 1
}

copy = (a bigint) (c bigint) {
    c = a
}

abs-cmp = (a bigint, b bigint) (res i64) {
    res = a.len - b.len
}

div-mod = (a bigint, b bigint) (q bigint, r bigint) {
    if b.len == 1 {
        q = zero()
        r = zero()
        return
    }
    cresult = abs-cmp(a, b)
    q.sign = a.sign
    normalize(q)
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
		t.Fatalf("expected no type errors, got %d: %v", len(results), results)
	}
}

// TestValidateFuncArgsUserFuncReturnType ensures that when a local variable is
// assigned the result of a user-defined function call, the inferred type is
// the function's first return type, not the generic "i64" default.
func TestValidateFuncArgsUserFuncReturnType(t *testing.T) {
	src := `bigint {
    sign i64
    len i64
    limbs [64]i64
}

copy = (a bigint) (c bigint) {
    c = a
}

mul = (a bigint, b bigint) (c bigint) {
    c = a
}

pow = (a bigint, n i64, out bigint) {
    base = copy(a)
    i = n
    for i > 0 {
        if i & 1 == 1 {
            out = mul(out, base)
        }
        base = mul(base, base)
        i = i >> 1
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
		t.Fatalf("expected no type errors, got %d: %v", len(results), results)
	}
}

// TestValidateFuncArgsStructLiteralType ensures that `g = bigint{}` infers
// `g` as type `bigint`, not `i64`.
func TestValidateFuncArgsStructLiteralType(t *testing.T) {
	src := `bigint {
    sign i64
    len i64
    limbs [64]i64
}

gcd = (a bigint, b bigint, g bigint) {
    g = a
}

div-mod = (a bigint, b bigint) (q bigint, r bigint) {
    r = a
}

lcm = (a bigint, b bigint, l bigint) {
    g = bigint{}
    gcd(a, b, g)
    q, r = div-mod(a, g)
    l = a
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
		t.Fatalf("expected no type errors, got %d: %v", len(results), results)
	}
}
