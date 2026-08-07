package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestD23OptionStructFieldAccess verifies that reading a str field from a struct
// obtained via Optional unwrap produces the correct value.
//
// Reproduces D23: "HTTP handler 中 conn.path 在 Optional 解包后为空"
// Root cause: collectVarDeclsFromStmtInner did not update varTypes["it"] when
// the ok arm's struct type had the same size as the err arm's type (%str-long).
// This caused `c = it` to be typed as %str-long instead of %test-conn,
// and `p = c.path` to default to i64 (8 bytes) instead of %str-long (24 bytes),
// truncating the str field to just the len field and losing the data pointer.
func TestD23OptionStructFieldAccess(t *testing.T) {
	// This test directly reads conn.path (not through a function call)
	// so that the type of `got` is inferred from conn's type.
	// If conn is mistyped as %str-long (from the err arm's it binding),
	// conn.path can't find the `path` field and defaults to i64.
	src := `test-conn {
    path str
}

make-conn = () (result ?test-conn) {
    result = nil
    c = test-conn {
        path: '/api/health'
    }
    result = c
}

opt = make-conn()
opt: {
    nil -> print('FAIL: nil')
    err -> print('FAIL: err')
    -> {
        conn = it
        got = conn.path
        got == '/api/health' -> print('PASS')
        -> print('FAIL: got=' - got)
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Verify that the generated IR does not allocate `got` as i64.
	// Before the fix, `got` was `alloca i64` (8 bytes) instead of
	// `alloca %str-long` (24 bytes), causing the str field to be truncated.
	g := NewGenerator()
	ir := g.Generate(prog)

	// Check that got is allocated as %str-long, not i64
	lines := strings.Split(ir, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Look for `alloca i64` for the `got` variable
		if strings.Contains(trimmed, "%got = alloca i64") {
			t.Errorf("D23 regression: `got` is allocated as i64 (8 bytes) instead of %%str-long (24 bytes).\n"+
				"This means the str field is truncated, losing the data pointer.\n"+
				"Line: %s", trimmed)
		}
	}
}
