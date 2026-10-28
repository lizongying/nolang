package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestTaggedEnumVariantDottedPayloadType locks in that a variant payload may be
// a dotted/qualified type (`err(e error.err)`): parseTaggedEnumVariantType must
// consume the DOT along with the following identifier.
//
// This is a regression guard for a real outage: with `option { ..., err(e
// error.err) }` in src/std/global.no, the old parser read `error` and left the
// DOT current. The field loop in parseTaggedEnumDefinition saw a token that was
// neither RPAREN nor COMMA/NEWLINE, allocated a TaggedEnumField, asked the type
// parser (which returned "" without consuming anything) and looped — allocating
// forever until genstdsig was OOM-killed. Since global.no is std, that hung
// `make gen` for the entire repo.
func TestTaggedEnumVariantDottedPayloadType(t *testing.T) {
	src := "option {\n    ok(v t),\n    nil,\n    err(e error.err),\n}\n"
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
	if len(ted.Variants) != 3 {
		t.Fatalf("variants = %d, want 3", len(ted.Variants))
	}
	errVar := ted.Variants[2]
	if errVar.Name != "err" {
		t.Fatalf("variants[2].Name = %q, want \"err\"", errVar.Name)
	}
	if len(errVar.Fields) != 1 {
		t.Fatalf("err fields = %d, want 1", len(errVar.Fields))
	}
	if got := errVar.Fields[0].Type.String(); got != "error.err" {
		t.Errorf("err payload type = %q, want \"error.err\"", got)
	}
}

// TestTaggedEnumVariantNoProgressDoesNotSpin guards the no-progress bail-out:
// a payload token the type parser cannot consume must yield a parse error,
// never an unbounded allocation loop (see TestTaggedEnumVariantDottedPayloadType
// for what the loop did to genstdsig).
func TestTaggedEnumVariantNoProgressDoesNotSpin(t *testing.T) {
	// INT is recognised by neither parseTaggedEnumVariantType's IDENT/NIL branch
	// nor its LBRACKET branch, so the field loop made no progress here.
	src := "option {\n    ok(v 42),\n}\n"
	p := New(lexer.New(src))
	_ = p.ParseProgram()
	if errs := p.Errors(); len(errs) == 0 {
		t.Errorf("expected a parse error for an unconsumable payload token, got none")
	}
}
