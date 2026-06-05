package lsp

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

func TestNewSymbolProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, program)
	if sp == nil {
		t.Fatal("NewSymbolProvider returned nil")
	}
	if sp.doc != doc {
		t.Error("doc not set correctly")
	}
	if sp.program != program {
		t.Error("program not set correctly")
	}
}

func TestSymbolProviderWithNilProgram(t *testing.T) {
	doc := createTestDocument("x = 10")

	sp := NewSymbolProvider(doc, nil)
	symbols := sp.GetSymbols()
	if symbols != nil {
		t.Error("expected nil symbols for nil program")
	}
}

func TestGetSymbols(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) != 2 {
		t.Errorf("expected 2 symbols, got %d", len(symbols))
	}
}

func TestGetSymbolsVariable(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "x" {
		t.Errorf("expected name 'x', got %q", symbols[0].Name)
	}
	if symbols[0].Kind != SymbolKindVariable {
		t.Errorf("expected Kind %d, got %d", SymbolKindVariable, symbols[0].Kind)
	}
}

func TestGetSymbolsFunction(t *testing.T) {
	text := `add(a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "add" {
		t.Errorf("expected name 'add', got %q", symbols[0].Name)
	}
	if symbols[0].Kind != SymbolKindFunction {
		t.Errorf("expected Kind %d, got %d", SymbolKindFunction, symbols[0].Kind)
	}
}

func TestGetSymbolsFunctionParameters(t *testing.T) {
	text := `add(a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if len(symbols[0].Children) < 2 {
		t.Errorf("expected at least 2 children (parameters), got %d", len(symbols[0].Children))
	}
	if symbols[0].Children[0].Name != "a" {
		t.Errorf("expected first param 'a', got %q", symbols[0].Children[0].Name)
	}
	if symbols[0].Children[1].Name != "b" {
		t.Errorf("expected second param 'b', got %q", symbols[0].Children[1].Name)
	}
}

func TestGetSymbolsFunctionLocalVariables(t *testing.T) {
	text := `add(a i64, b i64) {
    temp = a
    a = b
    b = temp
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if len(symbols[0].Children) < 4 {
		t.Errorf("expected at least 4 children (2 params + 3 local vars), got %d", len(symbols[0].Children))
	}
}

func TestGetSymbolsMultiple(t *testing.T) {
	text := `x = 10
y = 20
add(a i64, b i64) {
    result = a + b
}
mult(a i64, b i64) {
    result = a * b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) < 3 {
		t.Errorf("expected at least 3 symbols, got %d", len(symbols))
	}
}

func TestCollectFromStatement(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	var symbols []DocumentSymbol

	sp.collectFromStatement(program.Statements[0], "", &symbols)

	if len(symbols) != 1 {
		t.Errorf("expected 1 symbol, got %d", len(symbols))
	}
}

func TestCollectFromStatementNil(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, program)
	var symbols []DocumentSymbol

	sp.collectFromStatement(nil, "", &symbols)

	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for nil statement, got %d", len(symbols))
	}
}

func TestCollectFromExpressionNil(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, program)
	var symbols []DocumentSymbol

	sp.collectFromExpression(nil, "", &symbols)

	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for nil expression, got %d", len(symbols))
	}
}

func TestCollectLocalVariablesNil(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, program)
	var symbols []DocumentSymbol

	sp.collectLocalVariables(nil, "", &symbols)

	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for nil statement, got %d", len(symbols))
	}
}

func TestSymbolConstants(t *testing.T) {
	if SymbolKindFile != 1 {
		t.Errorf("expected SymbolKindFile 1, got %d", SymbolKindFile)
	}
	if SymbolKindFunction != 12 {
		t.Errorf("expected SymbolKindFunction 12, got %d", SymbolKindFunction)
	}
	if SymbolKindVariable != 13 {
		t.Errorf("expected SymbolKindVariable 13, got %d", SymbolKindVariable)
	}
	if SymbolKindParameter != 27 {
		t.Errorf("expected SymbolKindParameter 27, got %d", SymbolKindParameter)
	}
}

func TestGetSymbolsNestedFunction(t *testing.T) {
	text := `outer(a i64) {
    result = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	if len(symbols) != 1 {
		t.Errorf("expected 1 symbol, got %d", len(symbols))
	}
	if len(symbols[0].Children) < 1 {
		t.Error("expected at least 1 child")
	}
}

func TestSymbolProviderRangeFromToken(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, program)

	letStmt := program.Statements[0].(*parser.LetStatement)
	range_ := sp.rangeFromToken(letStmt.Name.Token)

	if range_.Start.Line != 0 {
		t.Errorf("expected Start.Line 0, got %d", range_.Start.Line)
	}
}

func TestSymbolProviderCreateVariableSymbol(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, program)

	letStmt := program.Statements[0].(*parser.LetStatement)
	symbol := sp.createVariableSymbol(letStmt.Name, "")

	if symbol.Name != "x" {
		t.Errorf("expected name 'x', got %q", symbol.Name)
	}
	if symbol.Kind != SymbolKindVariable {
		t.Errorf("expected Kind %d, got %d", SymbolKindVariable, symbol.Kind)
	}
}

func TestSymbolProviderCreateFunctionSymbol(t *testing.T) {
	text := `add(a i64, b i64) {
    result = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)

	symbols := sp.GetSymbols()
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}

	symbol := symbols[0]
	if symbol.Name != "add" {
		t.Errorf("expected name 'add', got %q", symbol.Name)
	}
	if symbol.Kind != SymbolKindFunction {
		t.Errorf("expected Kind %d, got %d", SymbolKindFunction, symbol.Kind)
	}
	if len(symbol.Children) < 2 {
		t.Errorf("expected at least 2 children (parameters), got %d", len(symbol.Children))
	}
}

func TestSymbolProviderCreateAnonymousFunctionSymbol(t *testing.T) {
	text := `add(a i64) {
    result = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)
	symbols := sp.GetSymbols()

	// Function definition should produce a named function symbol
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}
	if symbols[0].Name != "add" {
		t.Errorf("expected name 'add', got %q", symbols[0].Name)
	}
	if symbols[0].Kind != SymbolKindFunction {
		t.Errorf("expected Kind %d, got %d", SymbolKindFunction, symbols[0].Kind)
	}
}

func TestSymbolProviderCollectLocalVariables(t *testing.T) {
	text := `add(a i64, b i64) {
    temp = a
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, program)

	symbols := sp.GetSymbols()
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}

	// Verify local variable 'temp' is in the children
	foundTemp := false
	for _, child := range symbols[0].Children {
		if child.Name == "temp" {
			foundTemp = true
			break
		}
	}
	if !foundTemp {
		t.Error("expected local variable 'temp' in function children")
	}
}