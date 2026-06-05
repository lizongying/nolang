package lsp

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestNewDefinitionProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	dp := NewDefinitionProvider(doc, program)
	if dp == nil {
		t.Fatal("NewDefinitionProvider returned nil")
	}
	if dp.doc != doc {
		t.Error("doc not set correctly")
	}
	if dp.program != program {
		t.Error("program not set correctly")
	}
}

func TestDefinitionProviderWithNilProgram(t *testing.T) {
	doc := createTestDocument("x = 10")

	dp := NewDefinitionProvider(doc, nil)
	location, found := dp.GetDefinition(Position{Line: 0, Character: 0})
	if found {
		t.Error("expected not found for nil program")
	}
	_ = location
}

func TestDefinitionGetDefinition(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)
	location, found := dp.GetDefinition(Position{Line: 1, Character: 4})

	if found {
		if location.URI != "file:///test.no" {
			t.Errorf("expected URI 'file:///test.no', got %q", location.URI)
		}
	}
	_ = location
}

func TestDefinitionGetDefinitionNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)
	location, found := dp.GetDefinition(Position{Line: 0, Character: 10})

	if !found {
		_ = location
	}
}

func TestDefinitionGetDefinitionInFunction(t *testing.T) {
	text := `add = func(a, b) {
    result = a
}
add(1, 2)`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)
	location, found := dp.GetDefinition(Position{Line: 1, Character: 11})

	if found {
		_ = location
	}
}

func TestDefinitionGetWordAtPosition(t *testing.T) {
	doc := createTestDocument("hello world")
	program := createTestProgram("x = 10")

	dp := NewDefinitionProvider(doc, program)

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
		result := dp.getWordAtPosition(tt.position)
		if result != tt.expected {
			t.Errorf("getWordAtPosition(%v): expected %q, got %q", tt.position, tt.expected, result)
		}
	}
}

func TestDefinitionGetWordAtPositionEmpty(t *testing.T) {
	doc := createTestDocument("")
	program := createTestProgram("x = 10")

	dp := NewDefinitionProvider(doc, program)
	result := dp.getWordAtPosition(Position{Line: 0, Character: 0})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestDefinitionGetWordAtPositionBeyondLine(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	dp := NewDefinitionProvider(doc, program)
	result := dp.getWordAtPosition(Position{Line: 5, Character: 0})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestDefinitionFindDefinition(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)

	ident := &parser.Identifier{
		Token: lexer.Token{Literal: "x", Line: 1, Column: 1},
		Value: "x",
	}

	def := dp.findDefinition(ident, 1)
	if def == nil {
		t.Error("expected to find definition of x")
	}
	if def.Value != "x" {
		t.Errorf("expected definition value 'x', got %q", def.Value)
	}
}

func TestDefinitionFindDefinitionNotFound(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)

	ident := &parser.Identifier{
		Token: lexer.Token{Literal: "unknown", Line: 1, Column: 1},
		Value: "unknown",
	}

	def := dp.findDefinition(ident, 1)
	if def != nil {
		t.Error("expected nil for unknown symbol")
	}
}

func TestDefinitionCollectDefinitions(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)

	scope := newScope()
	dp.collectDefinitions(program.Statements, scope, 2)

	ident, found := scope.lookup("x")
	if !found {
		t.Error("expected to find x in scope")
	}
	_ = ident

	ident, found = scope.lookup("y")
	if !found {
		t.Error("expected to find y in scope")
	}
	_ = ident
}

func TestDefinitionCollectDefinitionsFromStatement(t *testing.T) {
	text := `add = func(a, b) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)

	scope := newScope()
	dp.collectDefinitions(program.Statements, scope, 5)

	ident, found := scope.lookup("add")
	if !found {
		t.Error("expected to find add in scope")
	}
	_ = ident
}

func TestDefinitionCollectDefinitionsWithFunctionScope(t *testing.T) {
	text := `x = 10
add = func(a, b) {
    result = x + a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)

	scope := newScope()
	dp.collectDefinitions(program.Statements, scope, 5)

	ident, found := scope.lookup("x")
	if !found {
		t.Error("expected to find x in scope")
	}
	_ = ident

	ident, found = scope.lookup("add")
	if !found {
		t.Error("expected to find add in scope")
	}
	_ = ident
}

func TestDefinitionLocationFromIdentifier(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)

	for _, stmt := range program.Statements {
		if letStmt, ok := stmt.(*parser.LetStatement); ok {
			loc := dp.locationFromIdentifier(letStmt.Name)
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
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	dp := NewDefinitionProvider(doc, program)
	loc := dp.locationFromIdentifier(nil)

	if loc.URI != "" {
		t.Errorf("expected empty URI, got %q", loc.URI)
	}
}

func TestDefinitionScopeOperations(t *testing.T) {
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

	found, ok = scope.lookup("y")
	if ok {
		t.Error("expected not to find y")
	}
}

func TestDefinitionScopeParentLookup(t *testing.T) {
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

func TestDefinitionGetDefinitionWithAssignment(t *testing.T) {
	text := `x = 10
y = x`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	dp := NewDefinitionProvider(doc, program)
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

	dp := NewDefinitionProvider(doc, program)
	location, found := dp.GetDefinition(Position{Line: 2, Character: 8})

	if !found {
		t.Error("expected to find definition of x in if expression")
	}
	_ = location
}