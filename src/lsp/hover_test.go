package lsp

import (
	"testing"
)

func TestNewHoverProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, program)
	if hp == nil {
		t.Fatal("NewHoverProvider returned nil")
	}
}

func TestHoverProviderWithNilProgram(t *testing.T) {
	doc := createTestDocument("x = 10")

	hp := NewHoverProvider(doc, nil)
	_, found := hp.GetHover(Position{Line: 0, Character: 0})
	if found {
		t.Error("expected not found for nil program")
	}
}

func TestGetHover(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover")
	}
	if hover == nil {
		t.Error("expected non-nil hover")
	}
}

func TestGetHoverNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	_, found := hp.GetHover(Position{Line: 0, Character: 10})

	if found {
		t.Error("expected not found for unknown position")
	}
}

func TestGetHoverInFunction(t *testing.T) {
	text := `add = func(a, b) {
    result = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover for function")
	}
	_ = hover
}

func TestGetHoverWordAtPosition(t *testing.T) {
	doc := createTestDocument("hello world")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, program)

	tests := []struct {
		position Position
		expected string
	}{
		{Position{Line: 0, Character: 0}, "hello"},
		{Position{Line: 0, Character: 5}, "hello"},
		{Position{Line: 0, Character: 6}, "world"},
		{Position{Line: 0, Character: 10}, "world"},
	}

	for _, tt := range tests {
		result := hp.getWordAtPosition(tt.position)
		if result != tt.expected {
			t.Errorf("getWordAtPosition(%v): expected %q, got %q", tt.position, tt.expected, result)
		}
	}
}

func TestGetHoverWordAtPositionEmpty(t *testing.T) {
	doc := createTestDocument("")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, program)
	result := hp.getWordAtPosition(Position{Line: 0, Character: 0})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestGetHoverWordAtPositionBeyondLine(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, program)
	result := hp.getWordAtPosition(Position{Line: 5, Character: 0})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestHoverScopeOperations(t *testing.T) {
	scope := newHoverScope()
	if scope == nil {
		t.Error("newHoverScope returned nil")
	}
	if scope.symbols == nil {
		t.Error("scope.symbols is nil")
	}

	result := &HoverResult{
		Name:       "x",
		Type:       "int",
		SymbolKind: "variable",
	}

	scope.define("x", result)

	found, ok := scope.lookup("x")
	if !ok {
		t.Error("expected to find x")
	}
	if found.Name != "x" {
		t.Errorf("expected name 'x', got %q", found.Name)
	}
}

func TestHoverScopeLookupNotFound(t *testing.T) {
	scope := newHoverScope()

	_, ok := scope.lookup("unknown")
	if ok {
		t.Error("expected not to find unknown")
	}
}

func TestHoverScopeParentLookup(t *testing.T) {
	parent := newHoverScope()
	child := newHoverScope()
	child.parent = parent

	parent.define("x", &HoverResult{Name: "x", Type: "int", SymbolKind: "variable"})

	found, ok := child.lookup("x")
	if !ok {
		t.Error("expected to find x in child scope via parent")
	}
	if found.Name != "x" {
		t.Errorf("expected name 'x', got %q", found.Name)
	}
}

func TestHoverCollectSymbols(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	scope := newHoverScope()
	hp.collectSymbols(program.Statements, scope, 2)

	found, ok := scope.lookup("x")
	if !ok {
		t.Error("expected to find x")
	}
	if found.Type != "int" {
		t.Errorf("expected type 'int', got %q", found.Type)
	}

	found, ok = scope.lookup("y")
	if !ok {
		t.Error("expected to find y")
	}
}

func TestHoverCollectSymbolsFromFunction(t *testing.T) {
	text := `add = func(a, b) {
    result = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	scope := newHoverScope()
	hp.collectSymbols(program.Statements, scope, 5)

	found, ok := scope.lookup("add")
	if !ok {
		t.Error("expected to find add")
	}
	if found.SymbolKind != "variable" {
		t.Errorf("expected SymbolKind 'variable', got %q", found.SymbolKind)
	}
}

func TestHoverGetExpressionType(t *testing.T) {
	tests := []struct {
		text     string
		expected string
	}{
		{"x = 10", "int"},
		{"x = 10.5", "float"},
		{`x = 'hello'`, "string"},
		{"x = true", "bool"},
		{"x = nil", "nil"},
	}

	for _, tt := range tests {
		doc := createTestDocument(tt.text)
		program := createTestProgram(tt.text)

		hp := NewHoverProvider(doc, program)
		_, found := hp.GetHover(Position{Line: 0, Character: 0})

		if !found {
			t.Errorf("expected to find hover for: %s", tt.text)
		}
	}
}

func TestHoverFindSymbol(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	result := hp.findSymbol("x", 0)

	if result == nil {
		t.Error("expected to find symbol x")
	}
	if result.Name != "x" {
		t.Errorf("expected name 'x', got %q", result.Name)
	}
}

func TestHoverFindSymbolNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	result := hp.findSymbol("unknown", 0)

	if result != nil {
		t.Error("expected nil for unknown symbol")
	}
}

func TestHoverFormatContent(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, program)
	result := &HoverResult{
		Name:       "x",
		Type:       "int",
		Declaration: "line 1, column 2",
		Value:      "10",
		SymbolKind: "variable",
	}

	contents := hp.formatHoverContent(result)

	markup, ok := contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}

	if markup.Kind != MarkupKindMarkdown {
		t.Errorf("expected Kind 'markdown', got %q", markup.Kind)
	}
	if markup.Value == "" {
		t.Error("expected non-empty Value")
	}
}

func TestHoverFormatContentParameter(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	hp := NewHoverProvider(doc, program)
	result := &HoverResult{
		Name:        "a",
		Type:        "parameter",
		Declaration: "line 1, column 5",
		SymbolKind:  "parameter",
	}

	contents := hp.formatHoverContent(result)

	markup, ok := contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}

	if markup.Value == "" {
		t.Error("expected non-empty Value")
	}
}

func TestGetHoverAtIdentifier(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	hover, found := hp.GetHover(Position{Line: 1, Character: 4})

	if !found {
		t.Error("expected to find hover for identifier x")
	}
	_ = hover
}

func TestGetHoverWithStringValue(t *testing.T) {
	text := `msg = 'hello'`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, program)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})

	if !found {
		t.Error("expected to find hover")
	}

	contents, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}
	if contents.Value == "" {
		t.Error("expected non-empty contents")
	}
}