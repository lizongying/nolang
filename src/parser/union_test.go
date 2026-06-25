package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestParseUnionTypeSingleLine(t *testing.T) {
	src := `int i8 | i16 | i32 | i64
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Statements))
	}
	ta, ok := prog.Statements[0].(*TypeAlias)
	if !ok {
		t.Fatalf("expected *TypeAlias, got %T", prog.Statements[0])
	}
	if ta.Name != "int" {
		t.Errorf("expected name 'int', got %q", ta.Name)
	}
	if !ta.IsUnion() {
		t.Fatalf("expected union, got single type %s", ta.Type.String())
	}
	if got := ta.Union.String(); got != "i8 | i16 | i32 | i64" {
		t.Errorf("union string mismatch: got %q", got)
	}
	if len(ta.Union.Types) != 4 {
		t.Errorf("expected 4 union members, got %d", len(ta.Union.Types))
	}
}

func TestParseUnionTypeAlias(t *testing.T) {
	src := `num int | float
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ta, ok := prog.Statements[0].(*TypeAlias)
	if !ok {
		t.Fatalf("expected *TypeAlias, got %T", prog.Statements[0])
	}
	if ta.Name != "num" {
		t.Errorf("expected name 'num', got %q", ta.Name)
	}
	if got := ta.Union.String(); got != "int | float" {
		t.Errorf("union string mismatch: got %q", got)
	}
}

func TestParseSingleTypeAlias(t *testing.T) {
	src := `my-int i64
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ta, ok := prog.Statements[0].(*TypeAlias)
	if !ok {
		t.Fatalf("expected *TypeAlias, got %T", prog.Statements[0])
	}
	if ta.Name != "my-int" {
		t.Errorf("expected name 'my-int', got %q", ta.Name)
	}
	if ta.IsUnion() {
		t.Fatalf("expected single-type alias, got union")
	}
	if ta.Type.String() != "i64" {
		t.Errorf("expected type 'i64', got %q", ta.Type.String())
	}
}

func TestParseUnionSingleLine(t *testing.T) {
	src := `int i8 | i16 | i32
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ta, ok := prog.Statements[0].(*TypeAlias)
	if !ok {
		t.Fatalf("expected *TypeAlias, got %T", prog.Statements[0])
	}
	if len(ta.Union.Types) != 3 {
		t.Errorf("expected 3 union members, got %d", len(ta.Union.Types))
	}
}

// TestTypeAliasDocComments verifies that doc comments preceding a type alias
// are attached to the TypeAlias statement via its embedded CommentedNode.
// Before the fix, setDoc() in parser.go was missing the *TypeAlias case,
// so doc comments were silently dropped, causing the LSP formatter to lose
// leading file-level documentation.
func TestTypeAliasDocComments(t *testing.T) {
	src := `// header comment
//
// describes int alias
int i8 | i16
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Statements))
	}
	ta, ok := prog.Statements[0].(*TypeAlias)
	if !ok {
		t.Fatalf("expected *TypeAlias, got %T", prog.Statements[0])
	}
	doc := ta.GetDoc()
	if doc == nil {
		t.Fatalf("expected doc comments attached to TypeAlias, got nil")
	}
	if len(doc.List) != 3 {
		t.Errorf("expected 3 doc comments, got %d", len(doc.List))
	}
}
