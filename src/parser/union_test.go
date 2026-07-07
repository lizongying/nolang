package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestParseUnionTypeSingleLine(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
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
	src := `num = int | float
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
	src := `my-int = i64
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
	src := `int = i8 | i16 | i32
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
int = i8 | i16
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

// --- Equals syntax tests ---

func TestParseEqualsUnionType(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
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
	if got := ta.Union.String(); got != "i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64" {
		t.Errorf("union string mismatch: got %q", got)
	}
	if len(ta.Union.Types) != 8 {
		t.Errorf("expected 8 union members, got %d", len(ta.Union.Types))
	}
}

func TestParseEqualsChainedUnion(t *testing.T) {
	src := `num = int | float
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

func TestParseEqualsSliceTypeAlias(t *testing.T) {
	src := `bytes = []byte
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
	if ta.Name != "bytes" {
		t.Errorf("expected name 'bytes', got %q", ta.Name)
	}
	if ta.IsUnion() {
		t.Fatalf("expected single-type alias, got union")
	}
	if ta.Type.String() != "[]byte" {
		t.Errorf("expected type '[]byte', got %q", ta.Type.String())
	}
}

func TestParseEqualsArrayTypeAlias(t *testing.T) {
	src := `buf = [16]u8
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
	if ta.Name != "buf" {
		t.Errorf("expected name 'buf', got %q", ta.Name)
	}
	if ta.Type.String() != "[16]u8" {
		t.Errorf("expected type '[16]u8', got %q", ta.Type.String())
	}
}

func TestParseEqualsSimpleAlias(t *testing.T) {
	src := `my-int = i64
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

// TestParseEqualsAliasOfAlias verifies that `num = int` is parsed as a type
// alias when `int` was previously defined as a type alias.
func TestParseEqualsAliasOfAlias(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
num = int
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(prog.Statements))
	}
	ta, ok := prog.Statements[1].(*TypeAlias)
	if !ok {
		t.Fatalf("expected *TypeAlias, got %T", prog.Statements[1])
	}
	if ta.Name != "num" {
		t.Errorf("expected name 'num', got %q", ta.Name)
	}
	if ta.IsUnion() {
		t.Fatalf("expected single-type alias, got union")
	}
	if ta.Type.String() != "int" {
		t.Errorf("expected type 'int', got %q", ta.Type.String())
	}
}

// --- Let statement still works (not confused with type alias) ---

func TestLetStatementNotTypeAlias(t *testing.T) {
	src := `x = 5
y = [1, 2, 3]
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(prog.Statements))
	}
	for i, s := range prog.Statements {
		if _, ok := s.(*LetStatement); !ok {
			t.Errorf("statement %d: expected *LetStatement, got %T", i, s)
		}
	}
}
