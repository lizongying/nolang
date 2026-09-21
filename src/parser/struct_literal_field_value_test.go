package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestStructLiteralFirstFieldValueShapes guards the `{ name : value }`
// disambiguation in classifyBlock / classifyBlockAtCurrent.
//
// Only the FIRST field of a `{ ... }` decides how the whole block is
// classified, and the value-shape list used to be a hand-maintained allowlist
// covering bare literals plus a few identifier-led expressions. Any other value
// shape fell through to blockMatch, so the block was parsed as a match arm and
// the struct name was then reported as undefined ("'P' is not defined").
//
// These are the shapes that were missing. Each is written as the FIRST field,
// because a later field would be masked by the first one's classification:
//
//	P { items: [1, 2, 3] }        ; used to fail
//	P { name: 'x'  items: [1] }   ; used to work (first field is a literal)
func TestStructLiteralFirstFieldValueShapes(t *testing.T) {
	cases := []struct {
		name     string
		fieldTy  string
		value    string
		prelude  string
	}{
		{"array_literal", "[]i64", "[1, 2, 3]", ""},
		{"float", "f64", "1.5", ""},
		{"char", "char", "\"a\"", ""},
		{"int_expression", "i64", "1 + 2", ""},
		{"str_expression", "str", "'a' - 'b'", ""},
		{"str_concat_chain", "str", "'a' - 'b' - 'c'", ""},
		{"parenthesised", "i64", "(1 + 2)", ""},
		{"negated_bool", "bool", "!false", ""},
		{"negated_ident", "i64", "-y", "y = 3"},
		{"index_expression", "i64", "arr[0]", "arr = [7, 8, 9]"},
		{"slice_range", "[]i64", "arr[0..2]", "arr = [1, 2, 3]"},
		// Controls that already worked and must keep working.
		{"bare_int", "i64", "7", ""},
		{"bare_str", "str", "'ab'", ""},
		{"bare_bool", "bool", "true", ""},
		{"bare_nil", "?i64", "nil", ""},
		{"negative_int", "i64", "-7", ""},
		{"ident_expression", "i64", "y + 1", "y = 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "Point {\n  f " + tc.fieldTy + "\n}\n"
			if tc.prelude != "" {
				src += "\n" + tc.prelude + "\n"
			}
			src += "\np = Point { f: " + tc.value + " }\n"

			l := lexer.New(src)
			p := New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors for %q: %v", tc.value, errs)
			}
			if len(prog.Statements) == 0 {
				t.Fatalf("no statements parsed for %q", tc.value)
			}
			let, ok := prog.Statements[len(prog.Statements)-1].(*LetStatement)
			if !ok {
				t.Fatalf("last statement is %T, want *LetStatement (value %q)",
					prog.Statements[len(prog.Statements)-1], tc.value)
			}
			sl, ok := let.Value.(*StructLiteral)
			if !ok {
				t.Fatalf("value %q: RHS is %T, want *StructLiteral — the block was "+
					"misclassified as a match arm", tc.value, let.Value)
			}
			if sl.Type != "Point" {
				t.Fatalf("value %q: struct literal type %q, want %q", tc.value, sl.Type, "Point")
			}
		})
	}
}

// TestLabelledBlockStillParsesAsMatch guards the deliberate exception:
// `name : { ... }` is a labelled block / match, NOT a struct literal. The
// value-shape extension must not swallow it.
func TestLabelledBlockStillParsesAsMatch(t *testing.T) {
	src := `x = 3
x: {
    1 -> print(11)
    -> print(33)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) < 2 {
		t.Fatalf("expected at least 2 statements, got %d", len(prog.Statements))
	}
	es, ok := prog.Statements[1].(*ExpressionStatement)
	if !ok {
		t.Fatalf("labelled block parsed as %T, want *ExpressionStatement",
			prog.Statements[1])
	}
	if _, ok := es.Expression.(*IfExpression); !ok {
		t.Fatalf("labelled block expression is %T, want *IfExpression (a match)",
			es.Expression)
	}
}

// TestStructDefinitionFirstFieldUnaffected keeps `name : type` (a struct
// DEFINITION, where the value is a bare type name) classified as a struct.
func TestStructDefinitionFirstFieldUnaffected(t *testing.T) {
	src := `Point {
  f i64
  g str
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	sd, ok := prog.Statements[0].(*StructDefinition)
	if !ok {
		t.Fatalf("first statement is %T, want *StructDefinition", prog.Statements[0])
	}
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
	}
}
