package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestEnumAnnotationPrefixRejected pins the placement rule for enum annotations:
// like every other target, an enum-definition annotation and a per-member
// annotation may only be written on their own line above the target or trailing
// on the target's line. The same-line prefix spelling is a parse error.
func TestEnumAnnotationPrefixRejected(t *testing.T) {
	cases := map[string]string{
		"enum definition prefix": "#{inline} color {\n    red,\n    green,\n}\n",
		"enum value prefix":      "color {\n    #{a} red,\n    green,\n}\n",
		"variant prefix":         "option {\n    #{a} ok(v i64),\n    nil,\n}\n",
	}
	for name, src := range cases {
		errs := parseAnnotationErrs(t, src)
		if len(errs) == 0 {
			t.Errorf("%s: same-line prefix annotation must be rejected, got no errors", name)
			continue
		}
		if joined := strings.Join(errs, "\n"); !strings.Contains(joined, "前方同一行") {
			t.Errorf("%s: errors %v do not mention the prefix rule", name, errs)
		}
	}
}

// TestEnumAnnotationAccepted is the control: definition-level (above the whole
// enum), per-member own-line, and per-member trailing annotations must all parse
// cleanly — for C-style enums and tagged enums alike, including the trailing
// form on the FIRST member (`red #{a},`), which previously mis-classified the
// whole `{ ... }` as a block statement.
func TestEnumAnnotationAccepted(t *testing.T) {
	cases := map[string]string{
		"definition above":     "#{inline}\ncolor {\n    red,\n    green,\n}\n",
		"enum value above":     "color {\n    #{a}\n    red,\n    green,\n}\n",
		"enum value trailing":  "color {\n    red #{a},\n    green,\n}\n",
		"explicit value trail": "color {\n    red = 5 #{a},\n    green,\n}\n",
		"single value":         "color {\n    red #{a},\n}\n",
		"variant above":        "option {\n    #{a}\n    ok(v i64),\n    nil,\n}\n",
		"variant trailing":     "option {\n    ok(v i64) #{a},\n    nil,\n}\n",
		"tagged first member":  "option {\n    nil #{x},\n    ok(v i64),\n}\n",
		"tagged def above":     "#{inline}\noption {\n    ok(v i64),\n    nil,\n}\n",
	}
	for name, src := range cases {
		if errs := parseAnnotationErrs(t, src); len(errs) > 0 {
			t.Errorf("%s: legal enum annotation reported errors: %v", name, errs)
		}
	}
}

// TestEnumValueAnnotationBinds guards that each enum value carries exactly its
// own annotation (own-line above or trailing), and that a value without one is
// left bare. A regression here would silently move a layout annotation onto the
// wrong value.
func TestEnumValueAnnotationBinds(t *testing.T) {
	src := "color {\n    #{a}\n    red,\n    green #{b},\n    blue,\n}\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(prog.Statements))
	}
	ed, ok := prog.Statements[0].(*EnumDefinition)
	if !ok {
		t.Fatalf("want *EnumDefinition, got %T", prog.Statements[0])
	}
	if len(ed.Values) != 3 {
		t.Fatalf("values = %d, want 3", len(ed.Values))
	}
	if anns := p.sem.RawAnnotationsOf(ed.Values[0]); len(anns) != 1 || anns[0].Key != "a" {
		t.Errorf("red (own-line above) annotations = %v, want one 'a'", anns)
	}
	if anns := p.sem.RawAnnotationsOf(ed.Values[1]); len(anns) != 1 || anns[0].Key != "b" {
		t.Errorf("green (trailing) annotations = %v, want one 'b'", anns)
	}
	if anns := p.sem.RawAnnotationsOf(ed.Values[2]); len(anns) != 0 {
		t.Errorf("blue must carry no annotations, got %v", anns)
	}
}

// TestTaggedEnumVariantAnnotationBinds is the tagged-enum counterpart: the
// first member's trailing annotation (`nil #{x},`) must bind to that variant
// and not disturb the classification of the block as a tagged enum.
func TestTaggedEnumVariantAnnotationBinds(t *testing.T) {
	src := "option {\n    nil #{x},\n    ok(v i64) #{y},\n}\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("statements = %d, want 1", len(prog.Statements))
	}
	ted, ok := prog.Statements[0].(*TaggedEnumDefinition)
	if !ok {
		t.Fatalf("want *TaggedEnumDefinition, got %T", prog.Statements[0])
	}
	if len(ted.Variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(ted.Variants))
	}
	if anns := p.sem.RawAnnotationsOf(ted.Variants[0]); len(anns) != 1 || anns[0].Key != "x" {
		t.Errorf("nil (trailing) annotations = %v, want one 'x'", anns)
	}
	if anns := p.sem.RawAnnotationsOf(ted.Variants[1]); len(anns) != 1 || anns[0].Key != "y" {
		t.Errorf("ok (trailing) annotations = %v, want one 'y'", anns)
	}
}
