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
	dm.indexBuiltinComments(index, source, "file:///std/io.no", "io")

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
	dm.indexBuiltinComments(index, source, "file:///std/io.no", "io")

	entry, ok := index.GetDefinition("write")
	if !ok {
		t.Fatal("expected to find write in definitions")
	}
	// Should point to the real definition (line 2), not the comment (line 0)
	if entry.Location.Range.Start.Line == 0 {
		t.Error("expected definition to point to real implementation, not comment")
	}
}

// TestIndexBuiltinCommentsNotOverwrittenByMethodShortName verifies that
// a built-in function's comment declaration (e.g. `read`) is correctly
// indexed and not confused by struct methods like `file.read`.
//
// Per the language spec, struct methods (file.read) are only registered under
// their full qualified name — NOT as a short name ("read"). So there is no
// conflict: "read" in the index comes solely from indexBuiltinComments.
func TestIndexBuiltinCommentsNotOverwrittenByMethodShortName(t *testing.T) {
	// fs.no: has both a built-in read comment AND a file.read method
	fsSource := `; build-in (ForwardFunc: read)
; read: read from fd
; read = (fd fd, buf str, n i64) (n i64) { }

file-mode {
    read,
    write,
}

file.read = (buf str, n i64) (read-n i64) {
    read-n = read(.fd, buf, n)
}
`
	index := NewSymbolIndex("file:///test.no", 1)
	index.AddBuiltinSymbols()

	dm := &DocumentManager{}
	fsURI := "file:///std/fs.no"

	// indexBuiltinComments must run BEFORE indexModuleStatement
	dm.indexBuiltinComments(index, fsSource, fsURI, "fs")
	fsProg := createTestProgram(fsSource)
	for _, ms := range fsProg.Statements {
		dm.indexModuleStatement(index, ms, fsURI)
	}

	// "read" should be found — from the built-in comment declaration
	entry, ok := index.GetDefinition("read")
	if !ok {
		t.Fatal("expected to find read in definitions")
	}
	// Should point to the comment declaration (line 2, 0-indexed), not file.read (line 9)
	if entry.Name != "read" {
		t.Errorf("expected Name 'read', got %q", entry.Name)
	}
	if entry.Location.Range.Start.Line != 2 {
		t.Errorf("expected definition at comment line 2, got line %d", entry.Location.Range.Start.Line)
	}

	// "file.read" should also be found — registered under its full qualified name
	fileEntry, ok := index.GetDefinition("file.read")
	if !ok {
		t.Fatal("expected to find file.read in definitions")
	}
	if fileEntry.Name != "file.read" {
		t.Errorf("expected Name 'file.read', got %q", fileEntry.Name)
	}
}

// TestNoShortNameRegisteredForMethodFromOtherModule verifies that
// struct methods (e.g. `tar.read`) do NOT register a short name ("read")
// in the index. Per the language spec, only print/eprint/format are exempt
// from module prefix; all other functions and methods must use a module
// prefix or receiver-based call. This prevents cross-module name conflicts
// (e.g. tar.read vs fs.read both claiming "read").
func TestNoShortNameRegisteredForMethodFromOtherModule(t *testing.T) {
	// tar.no: has a tar.read method (no builtin comment)
	tarSource := `tar.read = (idx i64) (out []byte) {
    out = nil
}
`
	// fs.no: has the built-in read comment declaration
	fsSource := `; build-in (ForwardFunc: read)
; read: read from fd
; read = (fd fd, buf str, n i64) (n i64) { }
`
	index := NewSymbolIndex("file:///test.no", 1)
	index.AddBuiltinSymbols()

	dm := &DocumentManager{}

	// 1. Index tar.no — "read" should NOT be registered as a short name
	tarProg := createTestProgram(tarSource)
	for _, ms := range tarProg.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/archive/tar.no")
	}
	// "read" should not exist in definitions (only "tar.read" is registered)
	if _, ok := index.GetDefinition("read"); ok {
		t.Fatal("short name 'read' should NOT be registered after indexing tar.read")
	}
	// "tar.read" should exist under its full qualified name
	tarEntry, ok := index.GetDefinition("tar.read")
	if !ok {
		t.Fatal("expected to find tar.read in definitions")
	}
	if tarEntry.Name != "tar.read" {
		t.Errorf("expected Name 'tar.read', got %q", tarEntry.Name)
	}

	// 2. Index fs.no — builtin comment registers "read" and "fs.read"
	dm.indexBuiltinComments(index, fsSource, "file:///std/fs.no", "fs")
	fsProg := createTestProgram(fsSource)
	for _, ms := range fsProg.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/fs.no")
	}

	// Now "read" should point to the builtin comment in fs.no
	entry, ok := index.GetDefinition("read")
	if !ok {
		t.Fatal("expected to find read after fs indexing")
	}
	if entry.Name != "read" {
		t.Errorf("expected Name 'read' (builtin), got %q", entry.Name)
	}
	if entry.Location.URI != "file:///std/fs.no" {
		t.Errorf("expected URI 'file:///std/fs.no', got %q", entry.Location.URI)
	}
}

// TestModuleQualifiedGoToDefinition verifies that go-to-definition on
// `fs.read` (module-qualified call) resolves to the fs module's definition,
// not to any unrelated symbol named `read` in the index.
func TestModuleQualifiedGoToDefinition(t *testing.T) {
	// Simulate: io.no calls fs.read(.fd, buf, n)
	// The cursor is on "read" in "fs.read"
	docText := `reader.read = (buf str, n i64) (read-n i64) {
    read-n = fs.read(.fd, buf, n)
}
`
	doc := createTestDocument(docText)

	// fs.no: has the built-in read comment declaration
	fsSource := `; build-in (ForwardFunc: read)
; read: read from fd
; read = (fd fd, buf str, n i64) (n i64) { }
`
	// tar.no: has a tar.read method (would interfere without module-aware lookup)
	tarSource := `tar.read = (idx i64) (out []byte) {
    out = nil
}
`
	index := NewSymbolIndex("file:///std/io.no", 1)
	index.AddBuiltinSymbols()

	dm := &DocumentManager{}

	// Index tar first (alphabetical order)
	tarProg := createTestProgram(tarSource)
	for _, ms := range tarProg.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/archive/tar.no")
	}
	// Index fs (builtin comment overwrites tar.read short name)
	dm.indexBuiltinComments(index, fsSource, "file:///std/fs.no", "fs")
	fsProg := createTestProgram(fsSource)
	for _, ms := range fsProg.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/fs.no")
	}

	dp := NewDefinitionProvider(doc, index)

	// Cursor on "read" in "fs.read(.fd, buf, n)" — line 1, column 16
	// "    read-n = fs.read(.fd, buf, n)"
	//  0123456789...
	//  positions: "fs.read" starts at column 13, "read" starts at column 16
	loc, found := dp.GetDefinition(Position{Line: 1, Character: 16})
	if !found {
		t.Fatal("expected to find definition for fs.read")
	}
	if loc.URI != "file:///std/fs.no" {
		t.Errorf("expected URI 'file:///std/fs.no', got %q", loc.URI)
	}

	// Also verify that the module-qualified entry exists
	entry, ok := index.GetDefinition("fs.read")
	if !ok {
		t.Fatal("expected to find 'fs.read' in definitions")
	}
	if entry.Location.URI != "file:///std/fs.no" {
		t.Errorf("expected fs.read URI 'file:///std/fs.no', got %q", entry.Location.URI)
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

// TestGlobalBuiltinShortNameResolution verifies that global built-in functions
// (with-cap, with-len, with-cap-len, print, eprint, format) are correctly
// indexed with location info via AddBuiltinSymbols + indexBuiltinComments,
// so that go-to-definition on their short names works across modules.
//
// Per the language spec, these functions can be called without module prefix.
// The compiler resolves them via FindBuiltinMethod by short name. The LSP
// must also resolve the short name to the comment declaration location.
func TestGlobalBuiltinShortNameResolution(t *testing.T) {
	// Simulate vec.no with comment declarations for with-cap/with-len/with-cap-len
	vecSource := `; with-cap: create with capacity
; build-in (ForwardFunc: with-cap)
; with-cap = (cap i64) { }

; with-len: create with length
; build-in (ForwardFunc: with-len)
; with-len = (len i64) { }

; with-cap-len: create with capacity and length
; build-in (ForwardFunc: with-cap-len)
; with-cap-len = (cap i64, len i64) { }
`
	// Simulate fmt.no with comment declarations for print/eprint/format
	fmtSource := `; print: output to stdout with newline
; build-in (ForwardFunc: print)
; print = (s str) { }

; eprint: output to stderr with newline
; build-in (ForwardFunc: eprint)
; eprint = (s str) { }

; format: return formatted string
; build-in (ForwardFunc: format)
; format = (s str) (out str) { }
`
	index := NewSymbolIndex("file:///test.no", 1)
	index.AddBuiltinSymbols()

	dm := &DocumentManager{}

	// Index vec.no
	dm.indexBuiltinComments(index, vecSource, "file:///std/vec.no", "vec")
	vecProg := createTestProgram(vecSource)
	for _, ms := range vecProg.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/vec.no")
	}

	// Index fmt.no
	dm.indexBuiltinComments(index, fmtSource, "file:///std/fmt.no", "fmt")
	fmtProg := createTestProgram(fmtSource)
	for _, ms := range fmtProg.Statements {
		dm.indexModuleStatement(index, ms, "file:///std/fmt.no")
	}

	// All these global built-in functions should be found by short name
	// with correct location pointing to their comment declaration.
	tests := []struct {
		name string
		uri  string
		line uint32
	}{
		{"with-cap", "file:///std/vec.no", 2},
		{"with-len", "file:///std/vec.no", 6},
		{"with-cap-len", "file:///std/vec.no", 10},
		{"print", "file:///std/fmt.no", 2},
		{"eprint", "file:///std/fmt.no", 6},
		{"format", "file:///std/fmt.no", 10},
	}

	for _, tt := range tests {
		entry, ok := index.GetDefinition(tt.name)
		if !ok {
			t.Errorf("expected to find %q in definitions", tt.name)
			continue
		}
		if entry.Location.URI != tt.uri {
			t.Errorf("%q: expected URI %q, got %q", tt.name, tt.uri, entry.Location.URI)
		}
		if entry.Location.Range.Start.Line != tt.line {
			t.Errorf("%q: expected line %d, got line %d", tt.name, tt.line, entry.Location.Range.Start.Line)
		}
	}
}
