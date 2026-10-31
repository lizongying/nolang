package checker

import (
	"testing"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestCheckPrivateBuiltinMethodCall verifies the compile-time privacy contract
// for the underscore-prefixed str/txt field accessors/mutators registered in
// src/builtin (str._len / str._cap / str._data / str._set-len / str._set-cap /
// str._set-data / txt._len / txt._data / txt._set-len):
//
//   - a call from INSIDE the owning std module (curModule == recvType) is allowed
//   - a call from user code or any other module is rejected with TraceID "8w3q93g2"
//   - non-underscore methods, underscore methods on other types, and unregistered
//     underscore methods are never enforced (left to the normal checks)
func TestCheckPrivateBuiltinMethodCall(t *testing.T) {
	mkCall := func(prop string, recvLine, recvCol int) (*parser.CallExpression, *parser.DotExpression) {
		recv := &parser.Identifier{Token: lexer.Token{Line: recvLine, Column: recvCol}}
		dot := &parser.DotExpression{
			Token:    lexer.Token{Line: recvLine, Column: recvCol},
			Receiver: recv,
			Property: prop,
		}
		e := &parser.CallExpression{
			Token:    lexer.Token{Line: recvLine, Column: recvCol},
			Function: dot,
		}
		return e, dot
	}

	cases := []struct {
		name      string
		prop      string
		recvType  string
		curModule string
		wantErr   bool
	}{
		{"str._len inside str module -> allowed", "_len", "str", "str", false},
		{"str._cap inside str module -> allowed", "_cap", "str", "str", false},
		{"str._data inside str module -> allowed", "_data", "str", "str", false},
		{"str._set-len inside str module -> allowed", "_set-len", "str", "str", false},
		{"str._set-cap inside str module -> allowed", "_set-cap", "str", "str", false},
		{"str._set-data inside str module -> allowed", "_set-data", "str", "str", false},
		{"txt._len inside txt module -> allowed", "_len", "txt", "txt", false},
		{"txt._data inside txt module -> allowed", "_data", "txt", "txt", false},
		{"txt._set-len inside txt module -> allowed", "_set-len", "txt", "txt", false},
		{"str._len from user code -> rejected", "_len", "str", "", true},
		{"str._set-len from user code -> rejected", "_set-len", "str", "", true},
		{"str._len from a DIFFERENT module -> rejected", "_len", "str", "txt", true},
		{"txt._data from user code -> rejected", "_data", "txt", "", true},
		{"non-underscore str.len from user code -> allowed", "len", "str", "", false},
		{"underscore method on another type -> allowed", "_foo", "vec", "user", false},
		{"registered underscore but wrong recv type -> allowed", "_len", "vec", "user", false},
		{"underscore str._bogus not registered -> allowed", "_bogus", "str", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, dot := mkCall(c.prop, 7, 3)
			got := checkPrivateBuiltinMethodCall(e, dot, c.recvType, c.curModule)
			if c.wantErr && len(got) == 0 {
				t.Fatalf("expected a privacy error for %s.%s from module %q, got none", c.recvType, c.prop, c.curModule)
			}
			if !c.wantErr && len(got) != 0 {
				t.Fatalf("expected NO error for %s.%s from module %q, got: %q", c.recvType, c.prop, c.curModule, got[0].Message)
			}
			if c.wantErr {
				if got[0].TraceID != "8w3q93g2" {
					t.Fatalf("expected TraceID %q, got %q", "8w3q93g2", got[0].TraceID)
				}
				if got[0].Line != 7 || got[0].Column != 3 {
					t.Fatalf("expected error position 7:3, got %d:%d", got[0].Line, got[0].Column)
				}
			}
		})
	}

	// Sanity: the registry really contains every private method we enforce.
	for _, name := range []string{
		"str._len", "str._cap", "str._data",
		"str._set-len", "str._set-cap", "str._set-data",
		"txt._len", "txt._data", "txt._set-len",
	} {
		if builtin.FindBuiltinMethod(name) == nil {
			t.Errorf("private builtin %q is not registered in src/builtin", name)
		}
	}
}
