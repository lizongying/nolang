package lsp

import (
	"testing"
)

func TestNewReferencesProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	rp := NewReferencesProvider(doc, createTestIndex(doc, program))
	if rp == nil {
		t.Fatal("NewReferencesProvider returned nil")
	}
}

func TestReferencesProviderWithNilIndex(t *testing.T) {
	doc := createTestDocument("x = 10")

	rp := NewReferencesProvider(doc, nil)
	locations := rp.GetReferences(Position{Line: 0, Character: 0}, true)
	if len(locations) != 0 {
		t.Error("expected empty references for nil index")
	}
}

func TestGetReferencesEmpty(t *testing.T) {
	doc := createTestDocument("x = 10")

	rp := NewReferencesProvider(doc, nil)
	locations := rp.GetReferences(Position{Line: 0, Character: 0}, true)
	if len(locations) != 0 {
		t.Error("expected empty references")
	}
}

func TestNewReferenceParams(t *testing.T) {
	doc := TextDocumentIdentifier{URI: "file:///test.no"}
	position := Position{Line: 0, Character: 0}

	params := NewReferenceParams(doc, position, true)
	if params.TextDocument.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", params.TextDocument.URI)
	}
	if !params.Context.IncludeDeclaration {
		t.Error("expected IncludeDeclaration to be true")
	}
}

func TestNewReferenceParamsFalse(t *testing.T) {
	doc := TextDocumentIdentifier{URI: "file:///test.no"}
	position := Position{Line: 0, Character: 0}

	params := NewReferenceParams(doc, position, false)
	if params.Context.IncludeDeclaration {
		t.Error("expected IncludeDeclaration to be false")
	}
}

func TestReferencesLocationKey(t *testing.T) {
	loc := Location{
		URI:   "file:///test.no",
		Range: Range{Start: Position{Line: 0, Character: 0}},
	}

	key := locationKey(loc)
	if key == "" {
		t.Error("expected non-empty key")
	}
}

func TestReferencesLocationKeyDifferent(t *testing.T) {
	loc1 := Location{
		URI:   "file:///test.no",
		Range: Range{Start: Position{Line: 0, Character: 0}},
	}
	loc2 := Location{
		URI:   "file:///test.no",
		Range: Range{Start: Position{Line: 1, Character: 0}},
	}

	key1 := locationKey(loc1)
	key2 := locationKey(loc2)
	if key1 == key2 {
		t.Error("expected different keys for different locations")
	}
}

func TestReferencesCollectDefinitions(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	rp := NewReferencesProvider(doc, index)

	locations := rp.GetReferences(Position{Line: 0, Character: 0}, true)
	if len(locations) == 0 {
		t.Error("expected at least one reference for x")
	}
}

func TestReferencesCollectDefinitionsWithFunction(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}
x = add(1, 2)`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	rp := NewReferencesProvider(doc, index)

	locations := rp.GetReferences(Position{Line: 0, Character: 0}, true)
	if len(locations) == 0 {
		t.Error("expected at least one reference for add")
	}
}

func TestReferencesScopeOperations(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.references["x"] = []Location{
		{URI: "test", Range: Range{Start: Position{Line: 0, Character: 0}}},
	}

	refs := index.GetReferences("x")
	if len(refs) != 1 {
		t.Errorf("expected 1 reference, got %d", len(refs))
	}
}

func TestReferencesScopeParentLookup(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.references["x"] = []Location{
		{URI: "test", Range: Range{Start: Position{Line: 0, Character: 0}}},
	}

	refs := index.GetReferences("x")
	if len(refs) != 1 {
		t.Errorf("expected 1 reference, got %d", len(refs))
	}

	refs = index.GetReferences("y")
	if len(refs) != 0 {
		t.Errorf("expected 0 references for y, got %d", len(refs))
	}
}
