package lsp

import (
	"testing"
)

func TestNewSymbolProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	if sp == nil {
		t.Fatal("NewSymbolProvider returned nil")
	}
	if sp.doc != doc {
		t.Error("doc not set correctly")
	}
	if sp.index == nil {
		t.Error("index not set correctly")
	}
}

func TestSymbolProviderWithNilIndex(t *testing.T) {
	doc := createTestDocument("x = 10")

	sp := NewSymbolProvider(doc, nil)
	symbols := sp.GetSymbols()
	if len(symbols) != 0 {
		t.Error("expected empty symbols for nil index")
	}
}

func TestGetSymbols(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) < 2 {
		t.Errorf("expected at least 2 symbols, got %d", len(symbols))
	}
}

func TestGetSymbolsVariable(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	found := false
	for _, sym := range symbols {
		if sym.Name == "x" {
			found = true
			if sym.Kind != SymbolKindVariable {
				t.Errorf("expected SymbolKindVariable, got %d", sym.Kind)
			}
		}
	}
	if !found {
		t.Error("expected to find symbol 'x'")
	}
}

func TestGetSymbolsFunction(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	found := false
	for _, sym := range symbols {
		if sym.Name == "add" {
			found = true
			if sym.Kind != SymbolKindFunction {
				t.Errorf("expected SymbolKindFunction, got %d", sym.Kind)
			}
		}
	}
	if !found {
		t.Error("expected to find symbol 'add'")
	}
}

func TestGetSymbolsFunctionParameters(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	for _, sym := range symbols {
		if sym.Name == "add" {
			if len(sym.Children) < 2 {
				t.Errorf("expected at least 2 parameters, got %d", len(sym.Children))
			}
		}
	}
}

func TestGetSymbolsFunctionLocalVariables(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	for _, sym := range symbols {
		if sym.Name == "add" {
			found := false
			for _, child := range sym.Children {
				if child.Name == "result" {
					found = true
				}
			}
			if !found {
				t.Error("expected to find local variable 'result'")
			}
		}
	}
}

func TestGetSymbolsMultiple(t *testing.T) {
	text := `x = 10
y = 20
z = 30`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) < 3 {
		t.Errorf("expected at least 3 symbols, got %d", len(symbols))
	}
}

func TestCollectFromStatement(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}
}

func TestCollectFromStatementNil(t *testing.T) {
	sp := NewSymbolProvider(nil, nil)
	symbols := sp.GetSymbols()
	if len(symbols) != 0 {
		t.Error("expected empty symbols")
	}
}

func TestCollectFromExpressionNil(t *testing.T) {
	sp := NewSymbolProvider(nil, nil)
	symbols := sp.GetSymbols()
	if len(symbols) != 0 {
		t.Error("expected empty symbols")
	}
}

func TestCollectLocalVariablesNil(t *testing.T) {
	sp := NewSymbolProvider(nil, nil)
	symbols := sp.GetSymbols()
	if len(symbols) != 0 {
		t.Error("expected empty symbols")
	}
}

func TestSymbolConstants(t *testing.T) {
	if SymbolKindFunction != 12 {
		t.Errorf("expected SymbolKindFunction 12, got %d", SymbolKindFunction)
	}
	if SymbolKindVariable != 13 {
		t.Errorf("expected SymbolKindVariable 13, got %d", SymbolKindVariable)
	}
}

func TestGetSymbolsNestedFunction(t *testing.T) {
	text := `outer = () {
    inner = () {
        x = 10
    }
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}
}

func TestSymbolProviderRangeFromToken(t *testing.T) {
	index := NewSymbolIndex("test", 1)
	index.symbols["x"] = &IndexEntry{
		Name: "x",
		Location: Location{
			URI:   "test",
			Range: Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 1}},
		},
	}

	doc := createTestDocument("x = 10")
	sp := NewSymbolProvider(doc, index)
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}
}

func TestSymbolProviderCreateVariableSymbol(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	for _, sym := range symbols {
		if sym.Name == "x" && sym.Kind != SymbolKindVariable {
			t.Errorf("expected SymbolKindVariable for x, got %d", sym.Kind)
		}
	}
}

func TestSymbolProviderCreateAnonymousFunctionSymbol(t *testing.T) {
	text := `x = () {
    y = 10
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}
}

func TestSymbolProviderCreateFunctionSymbol(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}

	for _, sym := range symbols {
		if sym.Name == "add" {
			if sym.Kind != SymbolKindFunction {
				t.Errorf("expected SymbolKindFunction, got %d", sym.Kind)
			}
		}
	}
}

func TestSymbolProviderCollectLocalVariables(t *testing.T) {
	text := `add = (a i64, b i64) {
    result = a + b
    sum = result
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	sp := NewSymbolProvider(doc, createTestIndex(doc, program))
	symbols := sp.GetSymbols()

	if len(symbols) == 0 {
		t.Fatal("expected at least 1 symbol")
	}
}
