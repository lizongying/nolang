package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// Regression tests for pointer-typed parameters in function definitions.
//
// `async-cancel = (task i8*) () { }` (src/std/async.no) used to fail with
// "expected comma or right parenthesis, got MUL(*)": three separate param-list
// parsers existed and none accepted a pointer suffix:
//  1. isFunctionDefinition (decl.go) — lookahead whitelist rejected MUL, so the
//     definition fell through to the function-literal path;
//  2. parseFunctionLiteral (expr.go) — type annotation accepted only a bare
//     IDENT, and it does not support a results list at all;
//  3. parseParamTypeAfterName (type.go) — used by parseFunctionDefinition.
//
// The fix touches all three so any `name = (params) (results) { }` form parses
// as a FunctionDefinition with a PointerType parameter.
func TestPointerParamFunctionDefinition(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"ptr-param-no-results", "f = (task i8*) { }\n"},
		{"ptr-param-empty-results", "f = (task i8*) () { }\n"},
		{"ptr-param-with-results", "f = (task i8*) (v i64) { }\n"},
		{"double-ptr-param", "f = (task i8**) () { }\n"},
		{"opt-ptr-param", "f = (task ?i8*) () { }\n"},
		{"plain-still-works", "f = (fd fd) (size ?i64) { }\n"},
		{"multi-ptr-params", "f = (a i8*, b i64, c str) (r i64) { }\n"},
	}
	for _, c := range cases {
		p := New(lexer.New(c.src))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Errorf("%-24s unexpected errors: %v", c.name, p.Errors())
			continue
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Errorf("%-24s produced no statements", c.name)
			continue
		}
		def, ok := prog.Statements[0].(*FunctionDefinition)
		if !ok {
			t.Errorf("%-24s parsed as %T, want *FunctionDefinition", c.name, prog.Statements[0])
			continue
		}
		if len(def.Parameters) == 0 {
			t.Errorf("%-24s has no parameters", c.name)
			continue
		}
		if _, ok := def.Parameters[0].Type.(*PointerType); !ok && c.name != "plain-still-works" {
			// ?i8* parses as NullableType{PointerType} — also acceptable.
			if nt, ok := def.Parameters[0].Type.(*NullableType); !ok || c.name != "opt-ptr-param" {
				t.Errorf("%-24s first param type is %T, want *PointerType", c.name, def.Parameters[0].Type)
			} else if _, ok := nt.Type.(*PointerType); !ok {
				t.Errorf("%-24s nullable elem is %T, want *PointerType", c.name, nt.Type)
			}
		}
	}
}

// A pointer parameter must survive as a PointerType through parseFunctionDefinition.
func TestPointerParamTypeIsPointerType(t *testing.T) {
	p := New(lexer.New("async-cancel = (task i8*) () { }\n"))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected errors: %v", p.Errors())
	}
	def, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("parsed as %T, want *FunctionDefinition", prog.Statements[0])
	}
	if len(def.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(def.Parameters))
	}
	param := def.Parameters[0]
	if param.Name != "task" {
		t.Errorf("param name = %q, want %q", param.Name, "task")
	}
	pt, ok := param.Type.(*PointerType)
	if !ok {
		t.Fatalf("param type is %T, want *PointerType", param.Type)
	}
	inner, ok := pt.Type.(*NamedType)
	if !ok || inner.Value != "i8" {
		t.Errorf("pointer elem = %#v, want NamedType(i8)", pt.Type)
	}
}
