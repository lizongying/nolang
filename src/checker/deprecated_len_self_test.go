package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// lenDeprTraceID is the TraceID ValidateDeprecatedLen stamps on every `.len`
// property diagnostic.
const lenDeprTraceID = "len-depr"

// validateLenDepr runs the production entry point (ValidateTypes, which
// populates validationStructFields before delegating to ValidateDeprecatedLen
// at checker.go:983) and returns only the deprecation diagnostics.
func validateLenDepr(t *testing.T, src string) []ValidateResult {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\nsrc:\n%s", errs, src)
	}
	var out []ValidateResult
	for _, r := range ValidateTypes(prog) {
		if r.TraceID == lenDeprTraceID {
			out = append(out, r)
		}
	}
	return out
}

// TestLenDeprSkipsSelfLenField pins the fix for the false positives that made
// `no vet src/std` report 17 errors on src/std/bigint.no and blocked
// `no build src/std/bigint.no` outright.
//
// `bigint` declares its own `len i64` field, so every `.len` in its methods is
// a field read on the receiver, not the removed container property. Before the
// fix only the explicit `self.len` form was recognised: `inferExprType` had a
// `self` case in both DotExpression branches but not in the base Identifier
// case, so the implicit form (`.len`, parsed as DotExpression{Receiver: self})
// fell through to the "unknown receiver" default and was flagged.
func TestLenDeprSkipsSelfLenField(t *testing.T) {
	src := `bag {
    items []i64
    len i64
}

bag.copy = () (out bag) {
    out.items = with-len(.len)
    out.len = .len
}

bag.explicit = () (out bag) {
    out.items = with-len(self.len)
    out.len = self.len
}

summarise = (b bag) (n i64) {
    n = b.len
}
`
	got := validateLenDepr(t, src)
	if len(got) != 0 {
		for _, r := range got {
			t.Errorf("false positive at L%d:C%d: %s", r.Line, r.Column, r.Message)
		}
		t.Fatalf("expected no len-depr on a struct that declares a `len` field, got %d", len(got))
	}
}

// TestLenDeprStillFlagsContainerReads is the guard against over-suppression:
// resolving `self` must not blind the validator to genuine container / str
// `.len` property reads. Each entry must still be reported.
func TestLenDeprStillFlagsContainerReads(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantSub  string
		wantHits int
	}{
		{
			name: "slice param",
			src: `plain = (s []i64) (n i64) {
    n = s.len
}
`,
			wantSub:  "recv.len property is removed",
			wantHits: 1,
		},
		{
			name: "str param keeps the byte-length hint",
			src: `plain = (s str) (n i64) {
    n = s.len
}
`,
			wantSub:  "str.len property is removed",
			wantHits: 1,
		},
		{
			name: "local vec",
			src: `plain = () (n i64) {
    v []i64 = [1, 2]
    n = v.len
}
`,
			wantSub:  "recv.len property is removed",
			wantHits: 1,
		},
		{
			// The receiver here is `.items`, a []i64 *field*, so `self`
			// resolution must feed the DotExpression branch rather than
			// suppress the diagnostic.
			name: "container field of self",
			src: `box {
    items []i64
}

box.count = () (n i64) {
    n = .items.len
}
`,
			wantSub:  "recv.len property is removed",
			wantHits: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateLenDepr(t, tc.src)
			if len(got) != tc.wantHits {
				t.Fatalf("got %d len-depr, want %d: %+v", len(got), tc.wantHits, got)
			}
			if !strings.Contains(got[0].Message, tc.wantSub) {
				t.Errorf("message %q does not contain %q", got[0].Message, tc.wantSub)
			}
		})
	}
}
