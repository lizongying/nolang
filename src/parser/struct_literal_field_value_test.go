package parser

import (
	"reflect"
	"strings"
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
		// `name : recv.field` / `name : recv.method(args)`.
		//
		// The arm separator in a match is `:`, so `s: n.to-str()` also looks
		// like an arm whose pattern is `s`. The disambiguator is the token
		// after `ident.ident`: `(` (a method call) can only be a field value.
		// LPAREN was missing, so the block fell through to blockMatch and the
		// struct name was reported as undefined ("'holder' is not defined").
		{"method_call", "str", "y.to-str()", "y = 3"},
		{"method_call_chain", "str", "y.to-str().slice(0, 1)", "y = 3"},
		// Control: `name : recv.field` (no call) already worked, via the token
		// after `ident.ident` being a member terminator.
		{"dot_field_value", "i64", "y.f", "y = 3"},
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
	if _, ok := es.Expression.(*SurfaceMatch); !ok {
		t.Fatalf("labelled block expression is %T, want *SurfaceMatch (a match)",
			es.Expression)
	}
}

// TestLabelledMatchWithMethodCallArmStillParsesAsMatch guards the reverse of
// the `method_call` shape above: widening the value-shape list must not steal
// `x: { arm -> body }` labelled matches. A real match arm separates its pattern
// from its body with `->`, so an arm whose BODY is a method call must stay a
// match even though its first member begins `ident -> ...`.
func TestLabelledMatchWithMethodCallArmStillParsesAsMatch(t *testing.T) {
	src := `n = 7
x = n
x: {
    k -> print(n.to-str())
    -> print(33)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) < 3 {
		t.Fatalf("expected at least 3 statements, got %d", len(prog.Statements))
	}
	es, ok := prog.Statements[2].(*ExpressionStatement)
	if !ok {
		t.Fatalf("labelled match parsed as %T, want *ExpressionStatement",
			prog.Statements[2])
	}
	if _, ok := es.Expression.(*SurfaceMatch); !ok {
		t.Fatalf("labelled match expression is %T, want *SurfaceMatch (a match)",
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

// TestStructLiteralRejectsValuelessField guards the parser-side hard error for
// "ghost fields". A bare identifier on its own line inside a struct literal
// used to be silently accepted as a StructField with Value == nil. The usual
// source is someone writing the element type under an empty vec field:
//
//	box {
//	    a: []
//	    i64        ; NOT a type annotation - becomes a ghost field
//	}
//
// parseSliceLiteral returns as soon as it sees `[]`, so the type name is read
// as the NEXT field name. Empty vec element types come from the struct field
// declaration, so that extra line is always a mistake: it must be reported,
// and it must not end up in the AST as a phantom field.
func TestStructLiteralRejectsValuelessField(t *testing.T) {
	const errID = "nu59u6hp"
	cases := []struct {
		name       string
		src        string
		wantLines  []int
		wantFields []string
	}{
		{
			name: "bare type names after empty vecs",
			src: `box {
    a []i64
    b []str
}
p = box {
    a: []
    i64
    b: []
    str
}
`,
			wantLines:  []int{7, 9},
			wantFields: []string{"a", "b"},
		},
		{
			name: "ghost field first",
			src: `box {
    a []i64
}
p = box {
    i64
    a: []
}
`,
			wantLines:  []int{5},
			wantFields: []string{"a"},
		},
		{
			name: "control - every field has a value",
			src: `box {
    a []i64
    b []str
}
p = box {
    a: []
    b: []
}
`,
			wantLines:  nil,
			wantFields: []string{"a", "b"},
		},
		{
			// `Point = { x i64  s str }` is the anonymous struct DEFINITION
			// form: every field is a bare `name type` pair with no value. It is
			// legitimate (checker's operator-overload / overflow fixtures use
			// it), so the ghost-field diagnostic must stay silent here.
			name: "control - anonymous struct definition form",
			src: `Point = {
    x i64
    s str
}
`,
			wantLines:  nil,
			wantFields: []string{"x", "i64", "s", "str"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.src))
			prog := p.ParseProgram()

			var got []Diagnostic
			for _, d := range p.Diagnostics() {
				if d.Severity == SeverityError && d.ID == errID {
					got = append(got, d)
				}
			}
			if len(got) != len(tc.wantLines) {
				t.Fatalf("got %d valueless-field errors, want %d; all errors: %v",
					len(got), len(tc.wantLines), p.Errors())
			}
			for i, d := range got {
				if d.Pos.Line != tc.wantLines[i] {
					t.Errorf("error %d: line %d, want %d", i, d.Pos.Line, tc.wantLines[i])
				}
				if !strings.Contains(d.Message, "has no value") {
					t.Errorf("message should explain the missing value, got %q", d.Message)
				}
			}

			if len(prog.Statements) == 0 {
				t.Fatal("no statements parsed")
			}
			let, ok := prog.Statements[len(prog.Statements)-1].(*LetStatement)
			if !ok {
				t.Fatalf("last statement is %T, want *LetStatement", prog.Statements[len(prog.Statements)-1])
			}
			sl, ok := let.Value.(*StructLiteral)
			if !ok {
				t.Fatalf("RHS is %T, want *StructLiteral", let.Value)
			}
			names := make([]string, 0, len(sl.Fields))
			for _, f := range sl.Fields {
				names = append(names, f.Name)
			}
			if !reflect.DeepEqual(names, tc.wantFields) {
				t.Errorf("struct literal fields = %v, want %v (ghost fields must be dropped)", names, tc.wantFields)
			}
		})
	}
}
