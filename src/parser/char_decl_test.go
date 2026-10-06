package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestCharDeclKeepsIdentifierRHS guards against the parser rewriting a bare
// Identifier initializer of a `char`-declared variable into a CharLiteral built
// from the identifier's NAME.
//
// `cc char = n` is a variable reference: cc must hold n's *value*, and the
// right-hand side must remain an *Identifier in the AST. The buggy behaviour
// converted any single-rune identifier (`n`, `x`, …) into `CharLiteral{"n"}`,
// so `n = 65; cc char = n; cc.to-str()` silently printed the letter 'n' instead
// of 'A'. A single-rune StringLiteral (`cc char = "A"`) is a genuine character
// literal and still folds into a CharLiteral.
func TestCharDeclKeepsIdentifierRHS(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"single_letter_var", "cc char = n"},
		{"multi_letter_var", "cc char = num"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "n = 65\nnum = 65\n" + tc.src + "\n"
			l := lexer.New(src)
			p := New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			ls := findLetStmt(t, prog, "cc")
			if _, ok := ls.Value.(*CharLiteral); ok {
				t.Fatalf("%s: Value was rewritten to CharLiteral %q (identifier hijack)", tc.name, ls.Value.(*CharLiteral).Value)
			}
			if _, ok := ls.Value.(*Identifier); !ok {
				t.Fatalf("%s: expected Value to stay an *Identifier, got %T", tc.name, ls.Value)
			}
		})
	}
}

// TestCharDeclFoldsStringLiteral verifies `cc char = "A"` (a single-rune string)
// still folds into a CharLiteral, which is the legitimate use of the fold.
func TestCharDeclFoldsStringLiteral(t *testing.T) {
	src := "cc char = \"A\"\n"
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ls := findLetStmt(t, prog, "cc")
	cl, ok := ls.Value.(*CharLiteral)
	if !ok {
		t.Fatalf("expected Value to fold to *CharLiteral, got %T", ls.Value)
	}
	if cl.Value != "A" {
		t.Fatalf("expected CharLiteral value 'A', got %q", cl.Value)
	}
}

func findLetStmt(t *testing.T, prog *Program, name string) *LetStatement {
	t.Helper()
	for _, s := range prog.Statements {
		if ls, ok := s.(*LetStatement); ok && ls.Name != nil && ls.Name.Value == name {
			return ls
		}
	}
	t.Fatalf("no LetStatement named %q found", name)
	return nil
}
