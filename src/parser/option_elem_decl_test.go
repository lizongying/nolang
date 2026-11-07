package parser

import (
	"testing"
)

// REGRESSION GUARD (2026-11-07): `name [N]?T = [...]` is a fixed-array
// declaration whose ELEMENT type is an option, NOT a ternary expression.
//
// Before the stmt.go dispatch fix the parser saw `name[N]` followed by `?`
// and committed to the conditional-expression path, reporting
// `expected colon in conditional expression, got ASSIGN` (trace oswu0as1).
// `[N]T` and `[]?T` already parsed; only the combination `[N]?T` fell through.
//
// The space between the identifier and `[` is what separates a declaration
// from `a[0]?x:y`, so the third case guards that a real ternary is NOT
// swallowed by the declaration branch.
func TestLetStatementOptionElemArrayType(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantType string
	}{
		{
			name:     "fixed_option_i64",
			src:      "f = () {\n    b [4]?i64 = [1, nil, 3, nil]\n}\n",
			wantType: "[4]?i64",
		},
		{
			name:     "fixed_option_slice",
			src:      "f = () {\n    b [2]?[]str = [['a'], []]\n}\n",
			wantType: "[2]?[]str",
		},
		{
			name:     "plain_fixed_array_still_parses",
			src:      "f = () {\n    b [4]i64 = [1, 2, 3, 4]\n}\n",
			wantType: "[4]i64",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stmts := parseClean(t, c.src)
			var lets []*LetStatement
			collectLets(stmts, &lets)
			var found *LetStatement
			for _, ls := range lets {
				if ls.Name != nil && ls.Name.Value == "b" {
					found = ls
					break
				}
			}
			if found == nil {
				t.Fatalf("no `b` declaration in AST — the line was parsed as an expression")
			}
			if found.Type == nil {
				t.Fatalf("`b` declaration has no type annotation")
			}
			if got := found.Type.String(); got != c.wantType {
				t.Fatalf("element type lost: got %q, want %q", got, c.wantType)
			}
		})
	}
}

// A real ternary after an index read (`a[0]?x:y`, no space before `[`) must
// remain an expression statement. The declaration branch is gated on
// hasSpaceBeforeBracket precisely for this case.
func TestIndexTernaryNotParsedAsOptionElemDecl(t *testing.T) {
	src := "f = () {\n    a [4]i64 = [1, 2, 3, 4]\n    r = a[0]?x:y\n}\n"
	// If the declaration branch swallowed `a[0]?x:y` (no space before `[`),
	// the remaining `x:y` tokens would be a parse error. parseClean also
	// rejects any error the parser recorded, so a clean parse is the guard.
	stmts := parseClean(t, src)
	var lets []*LetStatement
	collectLets(stmts, &lets)
	if len(lets) < 2 {
		t.Fatalf("expected both the `a` declaration and the `r` binding, got %d lets", len(lets))
	}
}
