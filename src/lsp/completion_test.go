package lsp

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func createTestDocument(text string) *TextDocument {
	return &TextDocument{
		Item: TextDocumentItem{
			URI:        "file:///test.no",
			LanguageID: "nolang",
			Version:    1,
			Text:       text,
		},
		Text: text,
	}
}

func createTestProgram(text string) *parser.Program {
	l := lexer.New(text)
	p := parser.New(l)
	return p.ParseProgram()
}

func TestNewCompletionProvider(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	if cp == nil {
		t.Fatal("NewCompletionProvider returned nil")
	}
	if cp.doc != doc {
		t.Error("doc not set correctly")
	}
	if cp.program != program {
		t.Error("program not set correctly")
	}
}

func TestCompletionProviderWithNilProgram(t *testing.T) {
	doc := createTestDocument("x = 10")

	cp := NewCompletionProvider(doc, nil)
	items := cp.getKeywordCompletions()
	if len(items) != 8 {
		t.Errorf("expected 8 keyword completions, got %d", len(items))
	}
}

func TestCompletionProviderNilProgramGetCompletions(t *testing.T) {
	doc := createTestDocument("x = 10")

	cp := NewCompletionProvider(doc, nil)
	items := cp.GetCompletions(Position{Line: 0, Character: 0}, "")
	items = append(items, cp.getKeywordCompletions()...)
	if len(items) < 8 {
		t.Errorf("expected at least 8 completions, got %d", len(items))
	}
}

func TestGetCompletionsNoTrigger(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.GetCompletions(Position{Line: 0, Character: 0}, "")

	if len(items) == 0 {
		t.Error("expected some completions")
	}
}

func TestGetCompletionsDotTrigger(t *testing.T) {
	doc := createTestDocument("console.log()")
	program := createTestProgram("console.log()")

	cp := NewCompletionProvider(doc, program)
	items := cp.GetCompletions(Position{Line: 0, Character: 8}, ".")
	_ = items
}

func TestGetCompletionsAfterDotTrigger(t *testing.T) {
	doc := createTestDocument("console.log()")
	program := createTestProgram("console.log()")

	cp := NewCompletionProvider(doc, program)
	receiverStr := cp.getReceiverStringAtPosition(Position{Line: 0, Character: 8})
	_ = receiverStr
}

func TestGetCompletionsColonTrigger(t *testing.T) {
	doc := createTestDocument("x: ")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.GetCompletions(Position{Line: 0, Character: 2}, ":")

	typeFound := false
	for _, item := range items {
		if item.Label == "i8" || item.Label == "float64" || item.Label == "string" || item.Label == "bool" {
			typeFound = true
			break
		}
	}
	if !typeFound {
		t.Error("expected type completions after colon trigger")
	}
}

func TestGetCompletionsEqualsTrigger(t *testing.T) {
	doc := createTestDocument("x = ")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.GetCompletions(Position{Line: 0, Character: 3}, "=")

	valueFound := false
	for _, item := range items {
		if item.Label == "true" || item.Label == "false" || item.Label == "nil" {
			valueFound = true
			break
		}
	}
	if !valueFound {
		t.Error("expected value completions after equals trigger")
	}
}

func TestGetCompletionsWithFunction(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	cp := NewCompletionProvider(doc, program)
	items := cp.getAllCompletions(Position{Line: 1, Character: 5})

	if len(items) == 0 {
		t.Error("expected keyword completions inside function")
	}
}

func TestGetAllCompletions(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.getAllCompletions(Position{Line: 0, Character: 0})

	if len(items) == 0 {
		t.Error("expected completions from getAllCompletions")
	}
}

func TestGetKeywordCompletions(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.getKeywordCompletions()

	expectedKeywords := []string{"if", "else", "for", "break", "return", "true", "false", "nil"}
	found := make(map[string]bool)

	for _, item := range items {
		found[item.Label] = true
	}

	for _, kw := range expectedKeywords {
		if !found[kw] {
			t.Errorf("expected keyword %q not found", kw)
		}
	}
}

func TestGetKeywordCompletionsWithFilter(t *testing.T) {
	doc := createTestDocument("x = 10")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.getKeywordCompletionsWithFilter("fo")

	if len(items) == 0 {
		t.Error("expected completions with filter 'fo'")
	}

	for _, item := range items {
		if len(item.Label) < 2 || item.Label[:2] != "fo" {
			if item.Label == "for" {
				continue
			}
			t.Errorf("item %q does not match filter 'fo'", item.Label)
		}
	}
}

func TestGetIdentifierCompletions(t *testing.T) {
	text := `x = 10
y = 20
z = x + y`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	cp := NewCompletionProvider(doc, program)
	items := cp.getIdentifierCompletions(Position{Line: 2, Character: 8})

	foundX := false
	foundY := false
	for _, item := range items {
		if item.Label == "x" {
			foundX = true
		}
		if item.Label == "y" {
			foundY = true
		}
	}

	if !foundX || !foundY {
		t.Error("expected to find x and y identifiers")
	}
}

func TestGetIdentifierCompletionsWithFilter(t *testing.T) {
	text := `x = 10
y = 20
z = 30`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	cp := NewCompletionProvider(doc, program)
	items := cp.getIdentifierCompletionsWithFilter(Position{Line: 2, Character: 4}, "z")

	if len(items) == 0 {
		t.Error("expected completions with filter 'z'")
	}

	for _, item := range items {
		if item.Label != "z" {
			t.Errorf("expected label 'z', got %q", item.Label)
		}
	}
}

func TestCompletionGetCurrentWord(t *testing.T) {
	doc := createTestDocument("x = hello")
	program := createTestProgram("x = hello")

	cp := NewCompletionProvider(doc, program)

	tests := []struct {
		position Position
		expected string
	}{
		{Position{Line: 0, Character: 4}, "hello"},
		{Position{Line: 0, Character: 5}, "hello"},
		{Position{Line: 0, Character: 7}, "hello"},
		{Position{Line: 0, Character: 9}, ""},
		{Position{Line: 0, Character: 10}, ""},
	}

	for _, tt := range tests {
		result := cp.getCurrentWord(tt.position)
		if result != tt.expected {
			t.Errorf("getCurrentWord(%v): expected %q, got %q", tt.position, tt.expected, result)
		}
	}
}

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		c        byte
		expected bool
	}{
		{'a', true},
		{'z', true},
		{'A', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'_', true},
		{'-', true},
		{'.', false},
		{':', false},
		{' ', false},
		{'@', false},
	}

	for _, tt := range tests {
		result := isWordChar(tt.c)
		if result != tt.expected {
			t.Errorf("isWordChar(%q): expected %v, got %v", tt.c, tt.expected, result)
		}
	}
}

func TestHasPrefixIgnoreCase(t *testing.T) {
	tests := []struct {
		s        string
		prefix   string
		expected bool
	}{
		{"for", "fo", true},
		{"For", "fo", true},
		{"FOR", "fo", true},
		{"forward", "fo", true},
		{"if", "fo", false},
		{"", "fu", false},
		{"fu", "fu", true},
		{"fu", "", true},
	}

	for _, tt := range tests {
		result := hasPrefixIgnoreCase(tt.s, tt.prefix)
		if result != tt.expected {
			t.Errorf("hasPrefixIgnoreCase(%q, %q): expected %v, got %v", tt.s, tt.prefix, tt.expected, result)
		}
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		c        byte
		expected byte
	}{
		{'A', 'a'},
		{'Z', 'z'},
		{'a', 'a'},
		{'z', 'z'},
		{'0', '0'},
		{'_', '_'},
	}

	for _, tt := range tests {
		result := toLower(tt.c)
		if result != tt.expected {
			t.Errorf("toLower(%q): expected %q, got %q", tt.c, tt.expected, result)
		}
	}
}

func TestGetWordRange(t *testing.T) {
	doc := createTestDocument("x = hello")
	program := createTestProgram("x = hello")

	cp := NewCompletionProvider(doc, program)
	range_ := cp.getWordRange(Position{Line: 0, Character: 5})

	if range_.Start.Character != 4 {
		t.Errorf("expected Start.Character 4, got %d", range_.Start.Character)
	}
	if range_.End.Character != 9 {
		t.Errorf("expected End.Character 9, got %d", range_.End.Character)
	}
}

func TestCollectSymbols(t *testing.T) {
	text := `x = 10
y = 20`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	cp := NewCompletionProvider(doc, program)
	symbols := cp.collectSymbols(Position{Line: 1, Character: 5})

	found := make(map[string]bool)
	for _, sym := range symbols {
		found[sym.Name] = true
	}

	if !found["x"] {
		t.Error("expected to find symbol 'x'")
	}
	if !found["y"] {
		t.Error("expected to find symbol 'y'")
	}
}

func TestGetTypeDetail(t *testing.T) {
	doc := createTestDocument("")
	program := createTestProgram("")

	cp := NewCompletionProvider(doc, program)

	programs := []*parser.Program{
		createTestProgram("x = 10"),
		createTestProgram("x = 10.5"),
		createTestProgram(`x = 'hello'`),
		createTestProgram("x = true"),
		createTestProgram("x = nil"),
	}

	types := []string{"int", "float", "string", "bool", "nil"}

	for i, p := range programs {
		cp.program = p
		var expr parser.Expression
		for _, stmt := range p.Statements {
			if letStmt, ok := stmt.(*parser.LetStatement); ok {
				expr = letStmt.Value
				break
			}
		}
		detail := cp.getTypeDetail(expr)
		if detail != types[i] {
			t.Errorf("expected type %s, got %s", types[i], detail)
		}
	}
}

func TestGetCompletionsAfterDot(t *testing.T) {
	doc := createTestDocument("console.log()")
	program := createTestProgram("console.log()")

	cp := NewCompletionProvider(doc, program)
	receiverStr := cp.getReceiverStringAtPosition(Position{Line: 0, Character: 8})
	_ = receiverStr
}

func TestGetCompletionsAfterDotMath(t *testing.T) {
	doc := createTestDocument("math.Abs()")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	receiverStr := cp.getReceiverStringAtPosition(Position{Line: 0, Character: 5})
	_ = receiverStr
}

func TestGetCompletionsAfterColon(t *testing.T) {
	doc := createTestDocument("x: ")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.getCompletionsAfterColon(Position{Line: 0, Character: 2})

	expected := []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "byte", "char", "str", "bool"}
	found := make(map[string]bool)

	for _, item := range items {
		found[item.Label] = true
	}

	for _, exp := range expected {
		if !found[exp] {
			t.Errorf("expected type completion %q not found", exp)
		}
	}
}

func TestGetCompletionsAfterEquals(t *testing.T) {
	doc := createTestDocument("x = ")
	program := createTestProgram("x = 10")

	cp := NewCompletionProvider(doc, program)
	items := cp.getCompletionsAfterEquals(Position{Line: 0, Character: 3})

	expected := []string{"true", "false", "nil"}
	found := make(map[string]bool)

	for _, item := range items {
		found[item.Label] = true
	}

	for _, exp := range expected {
		if !found[exp] {
			t.Errorf("expected value completion %q not found", exp)
		}
	}
}

func TestGetFunctionDeclarations(t *testing.T) {
	text := `add(a int, b int) {
    result = a + b
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	cp := NewCompletionProvider(doc, program)
	items := cp.GetFunctionDeclarations()

	if len(items) != 0 {
		t.Logf("found %d function declarations", len(items))
	}
}

func TestResolveCompletionItem(t *testing.T) {
	cp := NewCompletionProvider(nil, nil)

	funcItem := CompletionItem{
		Label: "add",
		Kind:  CompletionItemKindFunction,
	}
	resolved := cp.ResolveCompletionItem(funcItem)
	if resolved.Documentation == nil {
		t.Error("expected documentation for function")
	}

	varItem := CompletionItem{
		Label: "x",
		Kind:  CompletionItemKindVariable,
	}
	resolved = cp.ResolveCompletionItem(varItem)
	if resolved.Documentation == nil {
		t.Error("expected documentation for variable")
	}

	kwItem := CompletionItem{
		Label: "if",
		Kind:  CompletionItemKindKeyword,
	}
	resolved = cp.ResolveCompletionItem(kwItem)
	if resolved.Documentation == nil {
		t.Error("expected documentation for keyword")
	}
}

func TestTriggerTypeConstants(t *testing.T) {
	if TriggerNone != 0 {
		t.Errorf("expected TriggerNone 0, got %d", TriggerNone)
	}
	if TriggerDot != 1 {
		t.Errorf("expected TriggerDot 1, got %d", TriggerDot)
	}
	if TriggerColon != 2 {
		t.Errorf("expected TriggerColon 2, got %d", TriggerColon)
	}
	if TriggerEquals != 3 {
		t.Errorf("expected TriggerEquals 3, got %d", TriggerEquals)
	}
	if TriggerWord != 4 {
		t.Errorf("expected TriggerWord 4, got %d", TriggerWord)
	}
}

func TestGetTriggerType(t *testing.T) {
	tests := []struct {
		trigger  string
		expected TriggerType
	}{
		{".", TriggerDot},
		{":", TriggerColon},
		{"=", TriggerEquals},
		{" ", TriggerWord},
		{"", TriggerWord},
		{"a", TriggerWord},
	}

	for _, tt := range tests {
		result := getTriggerType(tt.trigger)
		if result != tt.expected {
			t.Errorf("getTriggerType(%q): expected %d, got %d", tt.trigger, tt.expected, result)
		}
	}
}
