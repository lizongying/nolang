package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestPrefixBinaryOperatorRejected pins the root-cause fix for `%0`-style
// malformed input: a binary operator in expression-start position must be a
// parse error. Previously the prefix switch's default branch skipped the token
// silently, so `%0` parsed with zero diagnostics into a nil-expression
// statement plus `0`; `no fmt` printed the phantom as a blank line and dropped
// it on the next pass, breaking idempotence.
//
// The table covers every token in isBinaryOp except SUB (which has its own
// unary case), so the whole operator family stays rejected.
func TestPrefixBinaryOperatorRejected(t *testing.T) {
	for _, tc := range []struct {
		src string
		op  string
	}{
		{"%0\n", "%"},
		{"+1\n", "+"},
		{"*1\n", "*"},
		{"/1\n", "/"},
		{"&1\n", "&"},
		{"|1\n", "|"},
		{"^1\n", "^"},
		{"<<1\n", "<<"},
		{">>1\n", ">>"},
		{"==1\n", "=="},
		{"!=1\n", "!="},
		{"<1\n", "<"},
		{"<=1\n", "<="},
		{">1\n", ">"},
		{">=1\n", ">="},
		{"&&1\n", "&&"},
		{"||1\n", "||"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			p := New(lexer.New(tc.src))
			p.ParseProgram()

			var hit *Diagnostic
			for _, d := range p.Diagnostics() {
				if d.ID == "m0db1nfx" {
					d := d
					hit = &d
					break
				}
			}
			if hit == nil {
				t.Fatalf("%q parsed without m0db1nfx: errors=%v diagnostics=%+v", tc.src, p.Errors(), p.Diagnostics())
			}
			if hit.Severity != SeverityError {
				t.Errorf("severity = %v, want error", hit.Severity)
			}
			if hit.Pos.Line != 1 || hit.Pos.Column != 1 {
				t.Errorf("position = %+v, want line 1 column 1", hit.Pos)
			}
			if !strings.Contains(hit.Message, tc.op) {
				t.Errorf("message %q does not name operator %q", hit.Message, tc.op)
			}
		})
	}
}

// TestInfixOperatorsStillAccepted guards the other side: the fix rejects
// operators only at expression start, never in their normal infix position.
func TestInfixOperatorsStillAccepted(t *testing.T) {
	for _, src := range []string{
		"x i64 = 5 % 2\n",
		"x bool = 1 < 2\n",
		"x i64 = 1 + 2 * 3\n",
		"x bool = true && false\n",
	} {
		p := New(lexer.New(src))
		p.ParseProgram()
		for _, d := range p.Diagnostics() {
			if d.ID == "m0db1nfx" {
				t.Errorf("%q was rejected by m0db1nfx: %s", src, d.Message)
			}
		}
		if len(p.Errors()) > 0 {
			t.Errorf("%q has parse errors: %v", src, p.Errors())
		}
	}
}
