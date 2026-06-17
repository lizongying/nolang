package lsp

import (
	"testing"
)

func TestNewHoverProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	if hp == nil {
		t.Fatal("NewHoverProvider returned nil")
	}
}

func TestHoverProviderWithNilIndex(t *testing.T) {
	doc := createTestDocument("x = 10")

	hp := NewHoverProvider(doc, nil)
	_, found := hp.GetHover(Position{Line: 0, Character: 0})
	if found {
		t.Error("expected not found for nil index")
	}
}

func TestGetHover(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover for x")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	_, found := hp.GetHover(Position{Line: 0, Character: 4})

	if found {
		t.Error("expected not found for position without identifier")
	}
}

func TestGetHoverInFunction(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover for add")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverWordAtPosition(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)

	word := getWordAtPosition(doc.Text, Position{Line: 0, Character: 0})
	if word != "x" {
		t.Errorf("expected word 'x', got %q", word)
	}
}

func TestGetHoverWordAtPositionEmpty(t *testing.T) {
	word := getWordAtPosition("", Position{Line: 0, Character: 0})
	if word != "" {
		t.Errorf("expected empty word, got %q", word)
	}
}

func TestGetHoverWordAtPositionBeyondLine(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)

	word := getWordAtPosition(doc.Text, Position{Line: 5, Character: 0})
	if word != "" {
		t.Errorf("expected empty word for beyond line, got %q", word)
	}
}

func TestHoverScopeOperations(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.symbols["x"] = &IndexEntry{
		Name: "x",
		Type: "i64",
	}

	entry, ok := index.Lookup("x")
	if !ok {
		t.Error("expected to find x")
	}
	if entry.Name != "x" {
		t.Errorf("expected name 'x', got %q", entry.Name)
	}

	_, ok = index.Lookup("y")
	if ok {
		t.Error("expected not to find y")
	}
}

func TestHoverScopeParentLookup(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.symbols["x"] = &IndexEntry{
		Name: "x",
		Type: "i64",
	}

	entry, ok := index.Lookup("x")
	if !ok {
		t.Error("expected to find x")
	}
	if entry.Name != "x" {
		t.Errorf("expected name 'x', got %q", entry.Name)
	}
}

func TestHoverCollectSymbols(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	entries := index.GetSymbolsBeforeLine(1)

	found := make(map[string]bool)
	for _, e := range entries {
		found[e.Name] = true
	}

	if !found["x"] {
		t.Error("expected to find x")
	}
	if !found["y"] {
		t.Error("expected to find y")
	}
}

func TestHoverCollectSymbolsFromFunction(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	entry, ok := index.GetDefinition("add")
	if !ok {
		t.Error("expected to find add in definitions")
	}
	if entry.Type == "" {
		t.Error("expected type information for add")
	}
}

func TestHoverGetExpressionType(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	entry, ok := index.GetDefinition("x")
	if !ok {
		t.Error("expected to find x")
	}
	if entry.Type != "i64" {
		t.Errorf("expected type 'i64', got %q", entry.Type)
	}
}

func TestHoverFindSymbol(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestHoverFindSymbolNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	_, found := hp.GetHover(Position{Line: 0, Character: 4})

	if found {
		t.Error("expected not found")
	}
}

func TestHoverFormatContent(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.symbols["x"] = &IndexEntry{
		Name:   "x",
		Type:   "i64",
		Value:  "10",
		Location: Location{URI: "test", Range: Range{Start: Position{Line: 0, Character: 0}}},
	}

	entry, _ := index.Lookup("x")
	hp := NewHoverProvider(nil, index)
	content := hp.formatHoverContent(entry)

	if content == nil {
		t.Error("expected content")
	}
}

func TestHoverFormatContentParameter(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.functions["add"] = &IndexEntry{
		Name:   "add",
		Type:   "fn(a i64, b i64) i64",
		Params: []ParamInfo{{Name: "a", Type: "i64"}, {Name: "b", Type: "i64"}},
	}

	entry, _ := index.Lookup("add")
	hp := NewHoverProvider(nil, index)
	content := hp.formatHoverContent(entry)

	if content == nil {
		t.Error("expected content")
	}
}

func TestGetHoverAtIdentifier(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 1, Character: 4})

	if !found {
		t.Error("expected to find hover for x in y = x")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverWithStringValue(t *testing.T) {
	text := `x = 'hello'`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover for x")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}
