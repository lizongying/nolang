package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestCondLoopSingleIdent is the regression guard for the 2026-09-24 bug in
// isCondLoopPrefix: `(flag) { ... }` (a parenthesised *single identifier*) was
// classified as an anonymous function-literal parameter list, so the whole
// construct parsed as a function-literal expression statement — dead code that
// never executed. It silently disabled every `(flag) { ... }` loop in std:
//   - src/std/crypto/rsa.no front-zero trimming (4 sites) → un-trimmed limb
//     lengths → rsa-modpow out-of-bounds read → SIGSEGV in tests/https-server.no
//   - src/std/net/net.no icmp-conn.ping-host option match
//   - src/std/net/ws.no server-conn.recv-nb receive loop
//
// The key fact making the old exclusion wrong: Nolang function-literal
// parameters always carry a type (`(a i64)`), so `(flag)` is not a valid
// parameter list at all. In statement position the only meaning of
// `(flag) { ... }` is a condition loop.
func TestCondLoopSingleIdent(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLoop  bool
		wantIdent string // expected Condition identifier ("" = don't check)
	}{
		{
			name:      "single identifier condition is a loop",
			input:     `flag = true` + "\n" + `(flag) { flag = false }`,
			wantLoop:  true,
			wantIdent: "flag",
		},
		{
			name:     "explicit comparison still a loop",
			input:    `flag = true` + "\n" + `(flag == true) { flag = false }`,
			wantLoop: true,
		},
		{
			name:     "infix condition still a loop",
			input:    `a = 0` + "\n" + `b = 1` + "\n" + `(a < b) { a = 2 }`,
			wantLoop: true,
		},
		{
			name:     "empty parens is a loop (never runs)",
			input:    `() { }`,
			wantLoop: true,
		},
		{
			name:     "bool literal parens is a loop",
			input:    `(true) { }`,
			wantLoop: true,
		},
		{
			name:      "hyphenated identifier condition is a loop",
			input:     `keep-trimming = true` + "\n" + `(keep-trimming) { keep-trimming = false }`,
			wantLoop:  true,
			wantIdent: "keep-trimming",
		},
		{
			// A typed parameter list must NOT become a loop.
			name:     "typed param list stays a function literal",
			input:    `(a i64) { a }`,
			wantLoop: false,
		},
		{
			// A comma-separated parameter list must NOT become a loop.
			name:     "comma param list stays a function literal",
			input:    `(a, b) { a }`,
			wantLoop: false,
		},
		{
			// Expression position: an assignment of a function literal.
			name:     "assigned function literal is not a loop",
			input:    `f = (x i64) { x }`,
			wantLoop: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			if len(prog.Statements) == 0 {
				t.Fatalf("expected at least one statement, got 0")
			}
			// Cases with a `flag = true` preamble put the construct last.
			last := prog.Statements[len(prog.Statements)-1]
			fs, isLoop := last.(*ForStatement)
			if isLoop != tt.wantLoop {
				t.Fatalf("wantLoop=%v, got statement of type %T", tt.wantLoop, last)
			}
			if !isLoop {
				return
			}
			if fs.Body == nil {
				t.Fatalf("loop has nil body")
			}
			if tt.wantIdent != "" {
				id, ok := fs.Condition.(*Identifier)
				if !ok {
					t.Fatalf("expected Condition to be *Identifier, got %T", fs.Condition)
				}
				if id.Value != tt.wantIdent {
					t.Fatalf("expected Condition identifier %q, got %q", tt.wantIdent, id.Value)
				}
			}
		})
	}
}

// TestCondLoopSingleIdentLabeled covers the labelled form `#1 (flag) { ... }`.
// Before the fix this was a hard parse error ("expected loop body after label
// #1, got '(' without '(cond) { }'") because isCondLoopPrefix returned false.
func TestCondLoopSingleIdentLabeled(t *testing.T) {
	input := `flag = true` + "\n" + `#1 (flag) {` + "\n" + `    break #1` + "\n" + `}`
	l := lexer.New(input)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) == 0 {
		t.Fatalf("expected at least one statement, got 0")
	}
	last := prog.Statements[len(prog.Statements)-1]
	fs, ok := last.(*ForStatement)
	if !ok {
		t.Fatalf("expected *ForStatement, got %T", last)
	}
	if fs.Label != "1" {
		t.Fatalf("expected label %q, got %q", "1", fs.Label)
	}
	id, ok := fs.Condition.(*Identifier)
	if !ok {
		t.Fatalf("expected Condition to be *Identifier, got %T", fs.Condition)
	}
	if id.Value != "flag" {
		t.Fatalf("expected Condition identifier %q, got %q", "flag", id.Value)
	}
}
