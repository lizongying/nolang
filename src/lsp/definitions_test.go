package lsp

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

func TestNewDefinitionProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))
	if dp == nil {
		t.Fatal("NewDefinitionProvider returned nil")
	}
	if dp.doc != doc {
		t.Error("doc not set correctly")
	}
	if dp.index == nil {
		t.Error("index not set correctly")
	}
}

func TestDefinitionProviderWithNilIndex(t *testing.T) {
	doc := createTestDocument("x = 10")

	dp := NewDefinitionProvider(doc, nil)
	location, found := dp.GetDefinition(Position{Line: 0, Character: 0})
	if found {
		t.Error("expected not found for nil index")
	}
	_ = location
}

func TestDefinitionGetDefinition(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))
	location, found := dp.GetDefinition(Position{Line: 1, Character: 4})

	if found {
		if location.URI != "file:///test.no" {
			t.Errorf("expected URI 'file:///test.no', got %q", location.URI)
		}
	}
}

func TestDefinitionGetDefinitionNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))
	location, found := dp.GetDefinition(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected found for 'x' at position (0,0)")
	}
	if location.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", location.URI)
	}
}

func TestDefinitionGetDefinitionInFunction(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))
	location, found := dp.GetDefinition(Position{Line: 0, Character: 0})

	if found {
		if location.URI != "file:///test.no" {
			t.Errorf("expected URI 'file:///test.no', got %q", location.URI)
		}
	}
}

func TestDefinitionGetWordAtPosition(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)

	word := getWordAtPosition(doc.Text, Position{Line: 0, Character: 0})
	if word != "x" {
		t.Errorf("expected word 'x', got %q", word)
	}

	word = getWordAtPosition(doc.Text, Position{Line: 0, Character: 4})
	if word != "10" {
		t.Errorf("expected word '10', got %q", word)
	}
}

func TestDefinitionGetWordAtPositionEmpty(t *testing.T) {
	word := getWordAtPosition("", Position{Line: 0, Character: 0})
	if word != "" {
		t.Errorf("expected empty word, got %q", word)
	}
}

func TestDefinitionGetWordAtPositionBeyondLine(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)

	word := getWordAtPosition(doc.Text, Position{Line: 5, Character: 0})
	if word != "" {
		t.Errorf("expected empty word for beyond line, got %q", word)
	}
}

func TestDefinitionFindDefinition(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	entry, found := index.GetDefinition("x")
	if !found {
		t.Error("expected to find definition of x")
	}
	if entry.Name != "x" {
		t.Errorf("expected definition name 'x', got %q", entry.Name)
	}
}

func TestDefinitionFindDefinitionNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	_, found := index.GetDefinition("unknown")
	if found {
		t.Error("expected not found for unknown symbol")
	}
}

func TestDefinitionCollectDefinitions(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	_, foundX := index.GetDefinition("x")
	if !foundX {
		t.Error("expected to find x in definitions")
	}
	_, foundY := index.GetDefinition("y")
	if !foundY {
		t.Error("expected to find y in definitions")
	}
}

func TestDefinitionCollectDefinitionsWithFunctionScope(t *testing.T) {
	text := `x = 10
add = (a i64, b i64) {
    result = x + a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	_, foundX := index.GetDefinition("x")
	if !foundX {
		t.Error("expected to find x in definitions")
	}
	_, foundAdd := index.GetDefinition("add")
	if !foundAdd {
		t.Error("expected to find add in definitions")
	}
}

func TestDefinitionLocationFromIdentifier(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	for _, stmt := range program.Statements {
		if letStmt, ok := stmt.(*parser.LetStatement); ok {
			loc := locationFromIdentifier(doc.Item.URI, letStmt.Name)
			if loc.URI != "file:///test.no" {
				t.Errorf("expected URI 'file:///test.no', got %q", loc.URI)
			}
			if loc.Range.Start.Line != 0 {
				t.Errorf("expected Start.Line 0, got %d", loc.Range.Start.Line)
			}
			break
		}
	}
}

func TestDefinitionLocationFromNilIdentifier(t *testing.T) {
	loc := locationFromIdentifier("", nil)

	if loc.URI != "" {
		t.Errorf("expected empty URI, got %q", loc.URI)
	}
}

func TestDefinitionGetDefinitionWithAssignment(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))
	location, found := dp.GetDefinition(Position{Line: 1, Character: 4})

	if !found {
		t.Error("expected to find definition of x in assignment")
	}
	if location.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", location.URI)
	}
	_ = location
}

func TestDefinitionGetDefinitionIfExpression(t *testing.T) {
	text := `x = 10
if x > 5 {
    y = x
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))
	location, found := dp.GetDefinition(Position{Line: 2, Character: 8})

	if !found {
		t.Error("expected to find definition of x in if expression")
	}
	_ = location
}
