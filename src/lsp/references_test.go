package lsp

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestNewReferencesProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	rp := NewReferencesProvider(doc, program)
	if rp == nil {
		t.Fatal("NewReferencesProvider returned nil")
	}
	if rp.doc != doc {
		t.Error("doc not set correctly")
	}
	if rp.program != program {
		t.Error("program not set correctly")
	}
}

func TestReferencesProviderWithNilProgram(t *testing.T) {
	doc := createTestDocument("x = 10")

	rp := NewReferencesProvider(doc, nil)
	locations := rp.GetReferences(Position{Line: 0, Character: 0}, false)
	if len(locations) != 0 {
		t.Errorf("expected 0 locations for nil program, got %d", len(locations))
	}
}

func TestGetReferencesEmpty(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	rp := NewReferencesProvider(doc, program)
	locations := rp.GetReferences(Position{Line: 0, Character: 0}, false)
	if locations == nil {
		t.Error("expected non-nil locations")
	}
}

func TestNewReferenceParams(t *testing.T) {
	params := NewReferenceParams(
		TextDocumentIdentifier{URI: "file:///test.no"},
		Position{Line: 5, Character: 10},
		true,
	)

	if params.TextDocument.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", params.TextDocument.URI)
	}
	if params.Position.Line != 5 {
		t.Errorf("expected Position.Line 5, got %d", params.Position.Line)
	}
	if params.Context.IncludeDeclaration != true {
		t.Error("expected IncludeDeclaration true")
	}
}

func TestNewReferenceParamsFalse(t *testing.T) {
	params := NewReferenceParams(
		TextDocumentIdentifier{URI: "file:///test.no"},
		Position{Line: 0, Character: 0},
		false,
	)

	if params.Context.IncludeDeclaration != false {
		t.Error("expected IncludeDeclaration false")
	}
}

func TestReferencesLocationKey(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	rp := NewReferencesProvider(doc, program)

	loc := Location{
		URI: "file:///test.no",
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: 1},
		},
	}

	key := rp.locationKey(loc)
	if key != "file:///test.no:0:0" {
		t.Errorf("expected 'file:///test.no:0:0', got %q", key)
	}
}

func TestReferencesLocationKeyDifferent(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	rp := NewReferencesProvider(doc, program)

	loc := Location{
		URI: "file:///other.no",
		Range: Range{
			Start: Position{Line: 5, Character: 10},
			End:   Position{Line: 5, Character: 15},
		},
	}

	key := rp.locationKey(loc)
	if key != "file:///other.no:5:10" {
		t.Errorf("expected 'file:///other.no:5:10', got %q", key)
	}
}

func TestReferencesCollectDefinitions(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	rp := NewReferencesProvider(doc, program)

	scope := newScope()
	rp.collectDefinitions(program.Statements, scope, 2)

	_, found := scope.lookup("x")
	if !found {
		t.Error("expected to find x in scope")
	}

	_, found = scope.lookup("y")
	if !found {
		t.Error("expected to find y in scope")
	}
}

func TestReferencesCollectDefinitionsWithFunction(t *testing.T) {
	text := `add = func(a, b) {
    result = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	rp := NewReferencesProvider(doc, program)

	scope := newScope()
	rp.collectDefinitions(program.Statements, scope, 5)

	_, found := scope.lookup("add")
	if !found {
		t.Error("expected to find add in scope")
	}
}

func TestReferencesScopeOperations(t *testing.T) {
	scope := newScope()

	ident := &parser.Identifier{
		Token: lexer.Token{Literal: "x", Line: 1, Column: 1},
		Value: "x",
	}

	scope.define("x", ident)

	found, ok := scope.lookup("x")
	if !ok {
		t.Error("expected to find x")
	}
	if found.Value != "x" {
		t.Errorf("expected value 'x', got %q", found.Value)
	}
}

func TestReferencesScopeParentLookup(t *testing.T) {
	parent := newScope()
	child := newScope()
	child.parent = parent

	ident := &parser.Identifier{
		Token: lexer.Token{Literal: "x", Line: 1, Column: 1},
		Value: "x",
	}

	parent.define("x", ident)

	found, ok := child.lookup("x")
	if !ok {
		t.Error("expected to find x in child scope via parent")
	}
	if found.Value != "x" {
		t.Errorf("expected value 'x', got %q", found.Value)
	}
}