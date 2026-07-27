package lsp

import (
	"strings"
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
		Name:     "x",
		Type:     "i64",
		Value:    "10",
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

// 新式語法關鍵字 hover
func TestGetHoverNewSyntaxKeywordBang(t *testing.T) {
	doc := createTestDocument("! {\n    *\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected hover for !")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverNewSyntaxKeywordBreak(t *testing.T) {
	doc := createTestDocument("! {\n    *\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 1, Character: 4})
	if !found {
		t.Fatal("expected hover for *")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverNewSyntaxKeywordContinue(t *testing.T) {
	doc := createTestDocument("! {\n    **\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 1, Character: 4})
	if !found {
		t.Fatal("expected hover for **")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverNewSyntaxKeywordReturn(t *testing.T) {
	doc := createTestDocument("foo = () {\n    ...\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 1, Character: 4})
	if !found {
		t.Fatal("expected hover for ...")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

// 舊式關鍵字 hover（含 deprecation warning）
func TestGetHoverDeprecatedIf(t *testing.T) {
	doc := createTestDocument("if x > 0 {\n    1\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected hover for if")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverDeprecatedFor(t *testing.T) {
	doc := createTestDocument("for i in 0..10 {\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected hover for for")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverDeprecatedMatch(t *testing.T) {
	doc := createTestDocument("match x {\n    _ => 0\n}")
	hp := NewHoverProvider(doc, nil)
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected hover for match")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

// 普通變數在關鍵字 hover 路徑之後仍能正常解析
func TestGetHoverIdentifierOverridesKeyword(t *testing.T) {
	text := `x = 10`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected hover for x")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
}

func TestGetHoverEnumType(t *testing.T) {
	text := `status {s1, s2, s3}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected hover for status")
	}
	if hover == nil {
		t.Fatal("hover is nil")
	}
	contents, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}
	if !strings.Contains(contents.Value, "s1 | s2 | s3") {
		t.Errorf("expected hover to contain 's1 | s2 | s3', got: %s", contents.Value)
	}
}

func TestGetHoverEnumVariant(t *testing.T) {
	text := `code {
    ok,
    not-found,
    io,
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	hp := NewHoverProvider(doc, index)

	// Hover over "io" on line 3
	// "    io," — "io" starts at column 4 (0-based)
	hover, found := hp.GetHover(Position{Line: 3, Character: 5})
	if !found {
		t.Fatal("expected hover for enum variant 'io'")
	}
	contents, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}
	if !strings.Contains(contents.Value, "io") {
		t.Errorf("expected hover to contain 'io', got: %s", contents.Value)
	}
	if !strings.Contains(contents.Value, "code") {
		t.Errorf("expected hover to contain enum type 'code', got: %s", contents.Value)
	}
}

func TestEnumVariantCompletion(t *testing.T) {
	text := `code {
    ok,
    not-found,
    io,
}

e = new(`
	doc := createTestDocument(text)
	program := createTestProgram(text)
	index := createTestIndex(doc, program)

	cp := NewCompletionProvider(doc, index)
	// Position after '=' on line 6 — should offer enum variants
	completions := cp.getCompletionsAfterEquals(Position{Line: 6, Character: 12})

	foundOk := false
	foundIo := false
	for _, c := range completions {
		if c.Label == "ok" {
			foundOk = true
		}
		if c.Label == "io" {
			foundIo = true
		}
	}
	if !foundOk {
		t.Error("expected completion to include 'ok' enum variant")
	}
	if !foundIo {
		t.Error("expected completion to include 'io' enum variant")
	}
}

func TestEnumVariantInSymbolIndex(t *testing.T) {
	text := `code {
    ok,
    not-found,
    io,
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)
	index := createTestIndex(doc, program)

	// Each variant should be in the symbol index
	variants := []string{"ok", "not-found", "io"}
	for _, v := range variants {
		entry, found := index.Lookup(v)
		if !found {
			t.Errorf("expected enum variant '%s' to be in symbol index", v)
			continue
		}
		if entry.Kind != SymbolKindEnumMember {
			t.Errorf("expected '%s' to have Kind SymbolKindEnumMember (%d), got %d", v, SymbolKindEnumMember, entry.Kind)
		}
		if entry.Type != "code" {
			t.Errorf("expected '%s' to have Type 'code', got '%s'", v, entry.Type)
		}
	}
}

// TestHoverReassignedVariablePreservesType verifies that when a variable is
// reassigned with an expression whose type cannot be directly inferred (e.g.
// `n = n + m`), the hover still shows the type from the previous declaration
// rather than an empty type.
func TestHoverReassignedVariablePreservesType(t *testing.T) {
	text := `errln = (s str) (n i64) {
    n = 42
    n = n + 1
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))

	// Hover over "n" on line 2 (0-based): "    n = n + 1"
	// "n" starts at column 4 (0-based)
	hover, found := hp.GetHover(Position{Line: 2, Character: 4})
	if !found {
		t.Fatal("expected to find hover for n on line 3")
	}
	contents, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}
	if !strings.Contains(contents.Value, "**Type**") {
		t.Errorf("expected hover to contain Type, got: %s", contents.Value)
	}
	if !strings.Contains(contents.Value, "i64") {
		t.Errorf("expected hover to contain type 'i64', got: %s", contents.Value)
	}
}

// TestHoverReassignedVariableFromCallPreservesType verifies that when a variable
// is first assigned from a function call and then reassigned with an
// InfixExpression, the type is preserved from the first declaration.
func TestHoverReassignedVariableFromCallPreservesType(t *testing.T) {
	text := `foo = () (n i64) {
    n = 10
    m = 20
    n = n + m
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)

	// The entry for "n" should have a non-empty type even after reassignment
	entry, ok := index.GetDefinition("n")
	if !ok {
		t.Fatal("expected to find n in definitions")
	}
	if entry.Type == "" {
		t.Error("expected n to have a non-empty type after reassignment")
	}
}

// TestHoverFunctionParameterShowsDeclaredType verifies that hovering over a
// function parameter name shows the declared type (e.g. `n i64` → i64),
// not a "call X" placeholder from a same-named variable in another function.
func TestHoverFunctionParameterShowsDeclaredType(t *testing.T) {
	text := `reader.read = (buf str, n i64) (read-n i64) {
    read-n = 0
    n = 42
}
errln = (s str) (n i64) {
    n = write(1, s, s.len)
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))

	// Hover over parameter "n" in reader.read's signature.
	// Line 0: "reader.read = (buf str, n i64) (read-n i64) {"
	// "n" is at column 23 (0-based): "reader.read = (buf str, n i64)..."
	//                              0123456789012345678901234
	hover, found := hp.GetHover(Position{Line: 0, Character: 24})
	if !found {
		t.Fatal("expected to find hover for parameter n")
	}
	contents, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}
	if !strings.Contains(contents.Value, "i64") {
		t.Errorf("expected hover to contain type 'i64', got: %s", contents.Value)
	}
	if strings.Contains(contents.Value, "call write") {
		t.Errorf("hover should NOT contain 'call write' placeholder, got: %s", contents.Value)
	}
}

// TestHoverFunctionParameterNotOverwrittenByBodyVariable verifies that
// a parameter's type is preserved even when the body reassigns it with
// a function call (which would normally produce a "call X" type).
func TestHoverFunctionParameterNotOverwrittenByBodyVariable(t *testing.T) {
	text := `foo = (x i64) (n i64) {
    n = write(1, x, 1)
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)

	// The entry for "n" should have type "i64" (from the result parameter),
	// not "call write" (from the body assignment).
	entry, ok := index.GetDefinition("n")
	if !ok {
		t.Fatal("expected to find n in definitions")
	}
	if entry.Type != "i64" {
		t.Errorf("expected n to have type 'i64', got '%s'", entry.Type)
	}
}
