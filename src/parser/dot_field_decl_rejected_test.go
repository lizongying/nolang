package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseErrors is a small helper: parse src and return the joined error text.
func parseErrors(t *testing.T, src string) string {
	t.Helper()
	p := New(lexer.New(src))
	p.ParseProgram()
	return strings.Join(p.Errors(), "\n")
}

const dotFieldDeclMsg = "is not allowed"

// The dotted field-type declaration `recv.field []T` (formerly parsed by
// parseDotExprLetStatement) is REJECTED.
//
// Rationale — it was worse than useless:
//   - it declared nothing: the field must already exist in the type, and the
//     statement emitted no code. Built with the same compiler, the ONLY
//     difference between HEAD (with `frame.out []byte` / `frame.dict []byte`)
//     and the same file with those lines deleted is two dead `let frame.out;`
//     declarations in the emitted JS — the executable code is byte-identical.
//   - it read like an assignment (`recv.field = []`), which is exactly how it
//     was mistaken for.
//   - it produced a LetStatement whose Name was the synthesised string
//     "recv.field", so the naming rule flagged it (2dhoris2) and, when the
//     field did not exist, the checker passed it through and MIR codegen died
//     with `EmitLLVM: setfield field`.
func TestDotFieldTypeDeclarationIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"slice", "pt {\n    out []byte\n}\n\nf = () (n i64) {\n    p pt\n    p.out []byte\n    n = 1\n}\n"},
		{"array", "pt {\n    out [16]byte\n}\n\nf = () (n i64) {\n    p pt\n    p.out [16]byte\n    n = 1\n}\n"},
		{"map", "pt {\n    out [str]i64\n}\n\nf = () (n i64) {\n    p pt\n    p.out [str]i64\n    n = 1\n}\n"},
		{"with-init", "pt {\n    out []byte\n}\n\nf = () (n i64) {\n    p pt\n    p.out []byte = p.out\n    n = 1\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseErrors(t, tc.src)
			if !strings.Contains(got, dotFieldDeclMsg) {
				t.Fatalf("want the declaration to be rejected, got: %q", got)
			}
			// The diagnostic must name the offending statement (line 7) and must
			// not cascade — the branch restores state and skips the statement.
			if !strings.Contains(got, "line 7,") {
				t.Fatalf("diagnostic should point at line 7, got: %q", got)
			}
			if n := strings.Count(got, dotFieldDeclMsg); n != 1 {
				t.Fatalf("want exactly 1 diagnostic, got %d: %q", n, got)
			}
		})
	}
}

// Controls — the shapes that merely LOOK like the rejected form must keep
// parsing. `isDotExprTypeDecl` keys off "] followed by an IDENT", so an index
// assignment (`p.out[0] = 1`, where '=' follows the ']') must not be swallowed.
func TestDotFormsThatStayLegal(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"field-assign", "pt {\n    out []byte\n}\n\nf = () (n i64) {\n    p pt\n    s []byte\n    p.out = s\n    n = p.out.len()\n}\n"},
		{"index-assign", "pt {\n    out [16]byte\n}\n\nf = () (n i64) {\n    p pt\n    p.out[0] = 1\n    p.out[1] = 2\n    n = 1\n}\n"},
		{"plain-decl", "f = () (n i64) {\n    out []byte\n    m [str]i64\n    n = out.len()\n}\n"},
		{"method-def", "pt {\n    out []byte\n}\n\npt.len2 = () (n i64, p pt) {\n    n = p.out.len()\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseErrors(t, tc.src); got != "" {
				t.Fatalf("must keep parsing, got: %q", got)
			}
		})
	}
}
