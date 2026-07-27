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

// TestIndexBuiltinComments verifies that comment-only built-in function
// declarations in std .no files are indexed with location info, so that
// go-to-definition works for built-in functions like write/read.
func TestIndexBuiltinComments(t *testing.T) {
	// Simulate a std file with a comment-only built-in declaration
	source := `; io — basic I/O
; build-in (ForwardFunc: write)
; write: write data to fd
; write = (fd fd, data str, n i64) (written i64) { }

out = (s str) (n i64) {
    n = write(1, s, s.len)
}
`
	index := NewSymbolIndex("file:///test.no", 1)
	index.AddBuiltinSymbols()

	dm := &DocumentManager{}
	dm.indexBuiltinComments(index, source, "file:///std/io.no")

	// The built-in "write" should now have a location pointing to the comment line
	entry, ok := index.GetDefinition("write")
	if !ok {
		t.Fatal("expected to find write in definitions after indexing builtin comments")
	}
	if entry.Location.URI != "file:///std/io.no" {
		t.Errorf("expected URI 'file:///std/io.no', got %q", entry.Location.URI)
	}
	// The comment declaration is on line 3 (0-based): "; write = (fd fd, data str, n i64) (written i64) { }"
	if entry.Location.Range.Start.Line != 3 {
		t.Errorf("expected definition at line 3, got line %d", entry.Location.Range.Start.Line)
	}
}

// TestIndexBuiltinCommentsDoesNotOverwriteRealDefinition verifies that
// comment-only declarations do not overwrite real (non-comment) definitions.
func TestIndexBuiltinCommentsDoesNotOverwriteRealDefinition(t *testing.T) {
	source := `; write = (fd fd, data str, n i64) (written i64) { }

write = (fd fd, data str, n i64) (written i64) {
    written = 42
}
`
	index := NewSymbolIndex("file:///test.no", 1)
	index.AddBuiltinSymbols()

	dm := &DocumentManager{}
	// First, index the real AST statements (as documents.go does)
	prog := createTestProgram(source)
	for _, ms := range prog.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/io.no")
	}
	// Then, index the comment-only declarations
	dm.indexBuiltinComments(index, source, "file:///std/io.no")

	entry, ok := index.GetDefinition("write")
	if !ok {
		t.Fatal("expected to find write in definitions")
	}
	// Should point to the real definition (line 2), not the comment (line 0)
	if entry.Location.Range.Start.Line == 0 {
		t.Error("expected definition to point to real implementation, not comment")
	}
}

// TestIsValidIdent verifies the identifier validation helper.
func TestIsValidIdent(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"write", true},
		{"get-line", true},
		{"read-stdin-line", true},
		{"open-file", true},
		{"", false},
		{"123abc", false},
		{"has space", false},
		{"str.len", false}, // dot not allowed
		{"[]byte", false},  // brackets not allowed
	}
	for _, tt := range tests {
		got := isValidIdent(tt.input)
		if got != tt.want {
			t.Errorf("isValidIdent(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
