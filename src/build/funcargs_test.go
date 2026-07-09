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

// TestValidateUndefinedVarsScopeAfterMatch verifies that variables defined
// before an `if` block containing a match expression (`w: { nil -> ... -> }`)
// are still recognized as defined after the `if` block, inside a `!! { }`
// loop, and after the loop.
// This is a regression test for a scope-tracking bug where the match
// desugaring inside an `if` consequence caused the validator to lose
// track of previously defined variables.
func TestValidateUndefinedVarsScopeAfterMatch(t *testing.T) {
	src := `tls-conn {
    fd i64
}

send = (c tls-conn, req str) (w ?i64) {
    w = nil
}

net-recv = (fd i64, buf str, n i64) (rn i64) {
    rn = 0
}

reconnect = () (ok bool) {
    ok = false
    req = ''
    crlf = ''

    written = 0
    if .use-tls {
        w = send(.tls-c, req)
        w: {
            nil -> {
                .close()
                return
            }
            ->
        }
    }
    if .use-tls == false {
        written = net-recv(.fd, req, req.len)
        if written < 0 {
            .close()
            return
        }
    }

    space = ' '
    raw = space.repeat(64)
    tmp = space.repeat(32)
    total = 0
    n = 0
    separator = crlf - crlf
    sep-pos i64 = -1

    !! {
        if total + 32 > 64 {
            .close()
            return
        }
        if .use-tls {
            rn = .tls-c.recv(tmp, 32)
            rn: {
                ok -> n = it
                -> {
                    .close()
                    return
                }
            }
        }
        if .use-tls == false {
            n = net-recv(.fd, tmp, 32)
        }
        if n <= 0 {
            .close()
            return
        }
        i <- [0..n): {
            raw[total + i] = tmp[i]
        }
        total = total + n
        data = raw.slice(0, total)
        sep-pos = data.index(separator)
        if sep-pos >= 0 {
            *
        }
    }

    head = raw.slice(0, sep-pos)
    first-line-end = head.index(crlf)
    ok = true
}`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUndefinedVars(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no undefined-var errors, got %d: %v", len(results), results)
	}
}

// TestValidateUndefinedVarsLabeledConditional verifies that the variable
// introduced in a labeled conditional (`#N val: { ... }`) is recognized as
// defined inside the body, and that the validator does not report it as
// undefined.
func TestValidateUndefinedVarsLabeledConditional(t *testing.T) {
	src := `encrypt = (a i64) (r i64) {
    r = a
}

zero = () (r i64) {
    r = 0
}

#1 i <- [0..256): {
    #2 val: {
        val == 1 -> encrypt() -> zero()
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateUndefinedVars(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	if len(results) != 0 {
		t.Fatalf("expected no undefined-var errors, got %d: %v", len(results), results)
	}
}
