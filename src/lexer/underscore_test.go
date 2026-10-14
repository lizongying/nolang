package lexer

import "testing"

// A name may begin with a SINGLE underscore: constants, global variables and
// method names use a leading '_' as a private-style marker. A leading DOUBLE
// (or more) underscore '__' is reserved and must be rejected with a clear
// lexical error instead of the confusing "expected identifier after dot, got
// UNDERSCORE" the parser would otherwise emit. A lone '_' (not followed by a
// letter/digit) stays the UNDERSCORE wildcard token (e.g. `_ = expr`).
func TestUnderscoreIdentifier(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantType TokenType
		wantLit  string
		wantErr  bool
	}{
		{"single-underscore-then-letter", "_len", IDENT, "_len", false},
		{"single-underscore-then-digit", "_2", IDENT, "_2", false},
		{"sqlite3-style-name", "_sqlite3-open", IDENT, "_sqlite3-open", false},
		{"lone-underscore-wildcard", "_", UNDERSCORE, "_", false},
		{"double-underscore-rejected", "__foo", ILLEGAL, "__", true},
		{"triple-underscore-rejected", "___foo", ILLEGAL, "__", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lex := New(c.input)
			tok := lex.NextToken()
			if tok.Type != c.wantType {
				t.Fatalf("expected token type %s, got %s", c.wantType.String(), tok.Type.String())
			}
			if tok.Literal != c.wantLit {
				t.Fatalf("expected literal %q, got %q", c.wantLit, tok.Literal)
			}
			if c.wantErr {
				if tok.ErrMsg == "" {
					t.Fatalf("expected a lexical error message, got empty")
				}
			} else {
				if tok.ErrMsg != "" {
					t.Fatalf("expected no error message, got %q", tok.ErrMsg)
				}
			}
		})
	}
}

// Single-underscore private method names must stay valid after a dot, so the
// private-builtin feature (str._len, txt._data, ...) keeps parsing. Only a
// LEADING DOUBLE underscore after a dot is rejected.
func TestUnderscoreAfterDot(t *testing.T) {
	t.Run("single-underscore-method-valid", func(t *testing.T) {
		lex := New("._len")
		dots := lex.NextToken()
		if dots.Type != DOT {
			t.Fatalf("expected DOT, got %s", dots.Type.String())
		}
		prop := lex.NextToken()
		if prop.Type != IDENT || prop.Literal != "_len" {
			t.Fatalf("expected IDENT '_len', got %s %q", prop.Type.String(), prop.Literal)
		}
		if prop.ErrMsg != "" {
			t.Fatalf("expected no error for single-underscore method, got %q", prop.ErrMsg)
		}
	})

	t.Run("double-underscore-method-rejected", func(t *testing.T) {
		lex := New(".__foo")
		dots := lex.NextToken()
		if dots.Type != DOT {
			t.Fatalf("expected DOT, got %s", dots.Type.String())
		}
		prop := lex.NextToken()
		if prop.Type != ILLEGAL || prop.Literal != "__" {
			t.Fatalf("expected ILLEGAL '__', got %s %q", prop.Type.String(), prop.Literal)
		}
		if prop.ErrMsg == "" {
			t.Fatalf("expected a lexical error message for double-underscore method")
		}
	})
}
