package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsItUnwrapFromOptional verifies that when `it` is used
// inside a match arm's default (else) branch on an Optional variable, the
// checker treats `it` as the unwrapped inner type (e.g., str), not the full
// Optional type (?str).
//
// Reproduces: "argument 1 of 'tool-read-file': expected 'str', got '?str'"
// where `path` is ?str and the else arm calls `tool-read-file(it)`.
//
// Root cause: checkCallArgsInStmtWithResultParams in funcargs.go did not
// handle IsSynthetic LetStatements. When the synthetic `it = path` binding
// had no explicit Type (fallback path when parser couldn't resolve the
// matched variable's type at parse time), it inferred the type from the
// matched variable (path: ?str) without unwrapping the Optional, causing
// `it` to be incorrectly typed as ?str.
func TestValidateFuncArgsItUnwrapFromOptional(t *testing.T) {
	src := `extract-required-param = (args-str str, param-name str) (result ?str) {
    result = nil
    val = args-str
    val: {
        nil -> {
            result = err('missing ' - param-name)
            return
        }
        err -> {
            result = err('invalid ' - param-name)
            return
        }
        -> result = it
    }
}

tool-read-file = (path str) (result ?str) {
    result = nil
    result = path
}

exec-read-file = (args-str str) (result ?str) {
    result = nil
    path = extract-required-param(args-str, 'path')
    path: {
        nil -> return
        err -> {
            result = path
            return
        }
        -> result = tool-read-file(it)
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
		if strings.Contains(r.Message, "expected 'str', got '?str'") {
			t.Errorf("false positive: %s", r.Message)
		}
	}
}

// TestValidateFuncArgsItUnwrapFromOptionalI64 verifies the same fix works
// for non-str Optional types (e.g., ?i64).
func TestValidateFuncArgsItUnwrapFromOptionalI64(t *testing.T) {
	src := `get-val = (key str) (result ?i64) {
    result = nil
    result = 42
}

use-val = (key str) (out i64) {
    out = 0
    v = get-val(key)
    v: {
        nil -> return
        err -> return
        -> out = it
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
		if strings.Contains(r.Message, "expected 'i64', got '?i64'") {
			t.Errorf("false positive: %s", r.Message)
		}
	}
}
