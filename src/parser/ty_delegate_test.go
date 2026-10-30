package parser

// These tests pin the PARSER side of the type-system migration: the surface
// parser no longer decides what a type spelling means. It asks ty.Parse and
// only rebuilds the syntax node, so `[K]V`, `ptr T` and `?T` have exactly one
// classifier for the whole compiler.

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/ty"
)

// TestBuildTypeDelegatesClassification asserts that the node buildType emits is
// the node implied by ty.Parse's Kind — including the cases the old hand-rolled
// chain got wrong.
func TestBuildTypeDelegatesClassification(t *testing.T) {
	cases := []struct {
		raw  string
		kind ty.Kind // what ty says
		want string  // String() of the built node
		desc string
	}{
		{"?i64", ty.Option, "?i64", "option"},
		{"ptr byte", ty.Ptr, "*byte", "AST renders `*T` for both spellings (unchanged)"},
		{"*byte", ty.Ptr, "*byte", "`*T` is Ptr too"},
		{"&str", ty.View, "&str", "view"},
		{"[]byte", ty.Slice, "[]byte", "slice"},
		{"[32]byte", ty.Array, "[32]byte", "numeric size is an array"},
		{"[n]t", ty.Array, "[n]t", "single-letter size stays an array"},
		{"[str]i64", ty.Map, "[str]i64", "builtin key is a map"},
		{"[fd]i64", ty.Map, "[fd]i64", "non-builtin key is a map too (was an array)"},
		{"n", ty.Named, "n", "bare name"},
		{"i64", ty.Int, "i64", "scalar"},
	}
	for _, c := range cases {
		if st := tyPool.Parse(c.raw); st.Kind != c.kind {
			t.Errorf("ty.Parse(%q).Kind = %v, want %v (%s)", c.raw, st.Kind, c.kind, c.desc)
			continue
		}
		got := buildType(c.raw, lexer.Token{})
		if got == nil {
			t.Errorf("buildType(%q) = nil", c.raw)
			continue
		}
		if s := got.String(); s != c.want {
			t.Errorf("buildType(%q).String() = %q, want %q (%s)", c.raw, s, c.want, c.desc)
		}
	}
}

// TestParseMapTypeStringAgreesWithTy pins the three-way agreement the
// migration was for: the parser's map predicate, ty's Kind and MIR's
// classification must never disagree again.
func TestParseMapTypeStringAgreesWithTy(t *testing.T) {
	spellings := []string{
		"[str]i64", "[fd]i64", "[i64]str", "[]i64", "[3]i64", "[?]i64",
		"[n]t", "[str]?", "i64", "?[str]i64",
	}
	for _, raw := range spellings {
		wantMap := tyPool.Parse(raw).Kind == ty.Map
		gotMap := parseMapTypeString(raw, lexer.Token{}) != nil
		if gotMap != wantMap {
			t.Errorf("parseMapTypeString(%q) = %v but ty says map=%v", raw, gotMap, wantMap)
		}
		if IsMapTypeString(raw) != wantMap {
			t.Errorf("IsMapTypeString(%q) disagrees with ty", raw)
		}
	}
}

// TestTyOfMatchesBuildTypeRT asserts the node→structure conversion is the
// inverse of buildType's classification, without a string round-trip.
func TestTyOfMatchesBuildTypeRT(t *testing.T) {
	spellings := []string{
		"i64", "byte", "str", "?i64", "[]byte", "[32]byte", "[str]i64",
		"ptr byte", "&str", "?&str", "[]?str", "[200]?[str]i64",
	}
	for _, raw := range spellings {
		node := buildType(raw, lexer.Token{})
		if node == nil {
			t.Fatalf("buildType(%q) = nil", raw)
		}
		st := TyOf(node)
		if st == nil {
			t.Errorf("TyOf(buildType(%q)) = nil", raw)
			continue
		}
		if want := tyPool.Parse(raw); st.Kind != want.Kind {
			t.Errorf("TyOf(buildType(%q)).Kind = %v, want %v", raw, st.Kind, want.Kind)
		}
	}
	// nil in, nil out: the gate every caller relies on.
	if TyOf(nil) != nil {
		t.Error("TyOf(nil) must be nil")
	}
}

// TestTypeToStringKeepsPointerSpelling guards the printer contract: a nested
// pointer must not grow a second `ptr ` prefix.
func TestTypeToStringKeepsPointerSpelling(t *testing.T) {
	inner := &PointerType{Type: &NamedType{Value: "byte"}}
	if got := typeToString(inner); got != "ptr byte" {
		t.Errorf("typeToString(ptr byte) = %q", got)
	}
	outer := &PointerType{Type: inner}
	if got := typeToString(outer); got != "ptr byte" {
		t.Errorf("double pointer rendered as %q, want the inner spelling", got)
	}
}
