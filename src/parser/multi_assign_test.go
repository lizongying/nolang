package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestMultiAssignWithIndexExpression(t *testing.T) {
	src := `foo = (s str, pos i64) (field str, new-pos i64) {
    return
}

bar = (s str) (fields []str) {
    n = 0
    pos = 0
    fields[n], pos = foo(s, pos)
    n = n + 1
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	// Find the "bar" function
	var barFn *FunctionDefinition
	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*FunctionDefinition); ok && fd.Name == "bar" {
			barFn = fd
			break
		}
	}
	if barFn == nil {
		t.Fatal("function 'bar' not found")
	}

	// Check that stmt[2] is a MultiAssignStatement
	stmts := barFn.Body.Statements
	if len(stmts) < 3 {
		t.Fatalf("expected at least 3 statements, got %d", len(stmts))
	}

	mas, ok := stmts[2].(*MultiAssignStatement)
	if !ok {
		t.Fatalf("expected stmt[2] to be *MultiAssignStatement, got %T", stmts[2])
	}

	if len(mas.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(mas.Targets))
	}

	// First target should be an IndexExpression
	if _, ok := mas.Targets[0].(*IndexExpression); !ok {
		t.Fatalf("expected first target to be *IndexExpression, got %T", mas.Targets[0])
	}

	// Second target should be an Identifier
	if ident, ok := mas.Targets[1].(*Identifier); !ok {
		t.Fatalf("expected second target to be *Identifier, got %T", mas.Targets[1])
	} else if ident.Value != "pos" {
		t.Fatalf("expected second target name 'pos', got '%s'", ident.Value)
	}

	t.Logf("MultiAssignStatement with Targets: [IndexExpression, Identifier(pos)] — OK")
}
