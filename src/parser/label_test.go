package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestParseLabeledFor verifies that #N can be used as a label prefix for
// the various for-style loops that support break/continue:
//   - bare range-for:       #1 i <- [0..256): { ... }
//   - infinite loop:        #1! { ... }
//   - multiplicative count: #1 n * { ... }
//   - conditional:          #1 x == 1: { ... }
func TestParseLabeledFor(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "labeled bare range-for",
			input: `#1 i <- [0..256): {
    break #1
}`,
		},
		{
			name: "labeled infinite loop with !",
			input: `#1! {
    break #1
}`,
		},
		{
			name: "labeled multiplicative count",
			input: `#1 n * {
    break #1
}`,
		},
		{
			name: "labeled conditional",
			input: `#1 x == 1: {
    break #1
}`,
		},
		{
			name: "use statement still works (# std/...)",
			input: `# std/bigint
x = 0`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			if len(prog.Statements) == 0 {
				t.Fatalf("expected at least one statement, got 0")
			}
			// The use-statement case has nothing to check here.
			if tt.name == "use statement still works (# std/...)" {
				return
			}
			fs, ok := prog.Statements[0].(*ForStatement)
			if !ok {
				t.Fatalf("expected first stmt to be *ForStatement, got %T", prog.Statements[0])
			}
			if fs.Label != "1" {
				t.Fatalf("expected label %q, got %q", "1", fs.Label)
			}
		})
	}
}

// TestParseBreakContinueLabel verifies that break/continue can target a
// numeric `#N` label.
func TestParseBreakContinueLabel(t *testing.T) {
	input := `#1 i <- [0..10): {
    break #1
}
#2! {
    continue #2
}
`
	l := lexer.New(input)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) < 2 {
		t.Fatalf("expected 2 statements, got %d", len(prog.Statements))
	}
	// First: #1 i <- [0..10): { break #1 }
	fs1, ok := prog.Statements[0].(*ForStatement)
	if !ok {
		t.Fatalf("expected stmt[0] to be *ForStatement, got %T", prog.Statements[0])
	}
	if fs1.Label != "1" {
		t.Fatalf("expected fs1.Label=1, got %q", fs1.Label)
	}
	if len(fs1.Body.Statements) == 0 {
		t.Fatalf("expected fs1.Body.Statements to have break")
	}
	bs, ok := fs1.Body.Statements[0].(*BreakStatement)
	if !ok {
		t.Fatalf("expected first body stmt to be *BreakStatement, got %T", fs1.Body.Statements[0])
	}
	if bs.Label != "1" {
		t.Fatalf("expected break label=1, got %q", bs.Label)
	}
	// Second: #2! { continue #2 }
	fs2, ok := prog.Statements[1].(*ForStatement)
	if !ok {
		t.Fatalf("expected stmt[1] to be *ForStatement, got %T", prog.Statements[1])
	}
	if fs2.Label != "2" {
		t.Fatalf("expected fs2.Label=2, got %q", fs2.Label)
	}
	if len(fs2.Body.Statements) == 0 {
		t.Fatalf("expected fs2.Body.Statements to have continue")
	}
	cs, ok := fs2.Body.Statements[0].(*ContinueStatement)
	if !ok {
		t.Fatalf("expected first body stmt to be *ContinueStatement, got %T", fs2.Body.Statements[0])
	}
	if cs.Label != "2" {
		t.Fatalf("expected continue label=2, got %q", cs.Label)
	}
}

// TestParseStarBreakContinue verifies that `*` and `**` can be used as
// shorthand for `break` and `continue` respectively, optionally followed
// by a label (#N or IDENT).
func TestParseStarBreakContinue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKind string // "break" | "continue"
		wantLbl  string
	}{
		{
			name:     "single star, no label",
			input:    `*`,
			wantKind: "break",
			wantLbl:  "",
		},
		{
			name:     "double star, no label",
			input:    `**`,
			wantKind: "continue",
			wantLbl:  "",
		},
		{
			name:     "single star with numeric label",
			input:    `* #1`,
			wantKind: "break",
			wantLbl:  "1",
		},
		{
			name:     "double star with numeric label",
			input:    `** #2`,
			wantKind: "continue",
			wantLbl:  "2",
		},
		{
			name:     "single star with text label",
			input:    `* outer`,
			wantKind: "break",
			wantLbl:  "outer",
		},
		{
			name:     "double star with text label",
			input:    `** outer`,
			wantKind: "continue",
			wantLbl:  "outer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			if len(prog.Statements) == 0 {
				t.Fatalf("expected at least one statement, got 0")
			}
			// The first statement is the star/break.
			switch tt.wantKind {
			case "break":
				bs, ok := prog.Statements[0].(*BreakStatement)
				if !ok {
					t.Fatalf("expected *BreakStatement, got %T", prog.Statements[0])
				}
				if bs.Label != tt.wantLbl {
					t.Fatalf("expected label %q, got %q", tt.wantLbl, bs.Label)
				}
			case "continue":
				cs, ok := prog.Statements[0].(*ContinueStatement)
				if !ok {
					t.Fatalf("expected *ContinueStatement, got %T", prog.Statements[0])
				}
				if cs.Label != tt.wantLbl {
					t.Fatalf("expected label %q, got %q", tt.wantLbl, cs.Label)
				}
			}
		})
	}
}

// TestParseBreakContinueWithForLabel verifies the round-trip:
//
//	input:   break #1  /  * #1  /  ** #1
//	ast:     BreakStatement{Label:"1"} / ContinueStatement{Label:"1"}
//	fmt:     break #1 / * #1 / ** #1
//
// The test verifies the AST is preserved and that the token type tracks
// the source so the formatter can reproduce the `*` / `**` shorthand.
func TestParseBreakContinueWithForLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKind string // "break" | "continue"
		wantLbl  string
		wantTok  lexer.TokenType
	}{
		{
			name:     "break with numeric label",
			input:    `break #1`,
			wantKind: "break",
			wantLbl:  "1",
			wantTok:  lexer.BREAK,
		},
		{
			name:     "continue with numeric label",
			input:    `continue #1`,
			wantKind: "continue",
			wantLbl:  "1",
			wantTok:  lexer.CONTINUE,
		},
		{
			name:     "star with numeric label",
			input:    `* #1`,
			wantKind: "break",
			wantLbl:  "1",
			wantTok:  lexer.MUL,
		},
		{
			name:     "star star with numeric label",
			input:    `** #1`,
			wantKind: "continue",
			wantLbl:  "1",
			wantTok:  lexer.STAR_STAR,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			if len(prog.Statements) == 0 {
				t.Fatalf("expected at least one statement, got 0")
			}
			var stmt Statement
			switch tt.wantKind {
			case "break":
				s, ok := prog.Statements[0].(*BreakStatement)
				if !ok {
					t.Fatalf("expected *BreakStatement, got %T", prog.Statements[0])
				}
				stmt = s
			case "continue":
				s, ok := prog.Statements[0].(*ContinueStatement)
				if !ok {
					t.Fatalf("expected *ContinueStatement, got %T", prog.Statements[0])
				}
				stmt = s
			}
			var gotTok lexer.TokenType
			var gotLbl string
			switch v := stmt.(type) {
			case *BreakStatement:
				gotTok = v.Token.Type
				gotLbl = v.Label
			case *ContinueStatement:
				gotTok = v.Token.Type
				gotLbl = v.Label
			}
			if gotTok != tt.wantTok {
				t.Errorf("expected token type %s, got %s", tt.wantTok, gotTok)
			}
			if gotLbl != tt.wantLbl {
				t.Errorf("expected label %q, got %q", tt.wantLbl, gotLbl)
			}
		})
	}
}

// TestParseBreakContinueBlockSequence guards against a regression where
// `skipToStatementEnd()` swallows the `* #N` / `** #N` shorthand forms
// after a preceding `break` / `continue` / `return` statement, because
// `MUL`, `STAR_STAR`, and `LABEL` were missing from
// `isStatementBoundary`. Before the fix the second and third statements
// were silently dropped.
func TestParseBreakContinueBlockSequence(t *testing.T) {
	src := `f = () {
    break #1
    * #1
    ** #1
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) == 0 {
		t.Fatalf("expected at least one statement, got 0")
	}
	fn, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected first stmt to be *FunctionDefinition, got %T", prog.Statements[0])
	}
	if len(fn.Body.Statements) != 3 {
		t.Fatalf("expected 3 statements in block, got %d", len(fn.Body.Statements))
	}
	bs, ok := fn.Body.Statements[0].(*BreakStatement)
	if !ok || bs.Token.Type != lexer.BREAK || bs.Label != "1" {
		t.Errorf("expected break #1, got %T %+v", fn.Body.Statements[0], fn.Body.Statements[0])
	}
	bs2, ok := fn.Body.Statements[1].(*BreakStatement)
	if !ok || bs2.Token.Type != lexer.MUL || bs2.Label != "1" {
		t.Errorf("expected * #1, got %T %+v", fn.Body.Statements[1], fn.Body.Statements[1])
	}
	cs, ok := fn.Body.Statements[2].(*ContinueStatement)
	if !ok || cs.Token.Type != lexer.STAR_STAR || cs.Label != "1" {
		t.Errorf("expected ** #1, got %T %+v", fn.Body.Statements[2], fn.Body.Statements[2])
	}
}
