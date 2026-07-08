package lsp

import (
	"strings"
	"testing"
)

// TestExplicitTypeAnnotationInHover verifies that when a variable has an
// explicit type annotation (e.g. `MASK u64 = 18446744073709551615`),
// the hover shows the declared type ("u64"), not the inferred type
// from the value ("i64" for integer literals).
func TestExplicitTypeAnnotationInHover(t *testing.T) {
	text := `MASK u64 = 18446744073709551615`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	entry, ok := index.GetDefinition("MASK")
	if !ok {
		t.Fatal("expected to find MASK in definitions")
	}
	if entry.Type != "u64" {
		t.Errorf("expected type 'u64', got '%s'", entry.Type)
	}
	// Value should display the original literal, not the int64-wrapped value
	if entry.Value != "18446744073709551615" {
		t.Errorf("expected value '18446744073709551615', got '%s'", entry.Value)
	}
}

// TestExplicitTypeAnnotationHoverContent verifies the full hover content
// for a u64 constant with an overflow integer literal.
func TestExplicitTypeAnnotationHoverContent(t *testing.T) {
	text := `MASK u64 = 18446744073709551615`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: 0, Character: 0})
	if !found {
		t.Fatal("expected to find hover for MASK")
	}
	contents, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatal("expected MarkupContent")
	}
	if !strings.Contains(contents.Value, "u64") {
		t.Errorf("expected hover to contain 'u64', got: %s", contents.Value)
	}
	if strings.Contains(contents.Value, "f64") {
		t.Errorf("hover should NOT contain 'f64', got: %s", contents.Value)
	}
	if strings.Contains(contents.Value, "18446744073709551615") {
		// Value should be displayed correctly
	} else {
		t.Errorf("expected hover to contain '18446744073709551615', got: %s", contents.Value)
	}
}

// TestGoToDefinitionForConstant verifies that go-to-definition works
// for a module-level constant.
func TestGoToDefinitionForConstant(t *testing.T) {
	text := `MASK u64 = 18446744073709551615

foo = () {
    x = MASK
}`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	dp := NewDefinitionProvider(doc, index)

	// Position on "MASK" in `x = MASK` (line 3, column 8)
	loc, found := dp.GetDefinition(Position{Line: 3, Character: 8})
	if !found {
		t.Fatal("expected to find definition for MASK")
	}
	if loc.URI == "" {
		t.Error("expected non-empty URI for definition location")
	}
	// The definition should be on line 0 (where MASK is declared)
	if loc.Range.Start.Line != 0 {
		t.Errorf("expected definition at line 0, got line %d", loc.Range.Start.Line)
	}
}

// TestInferredTypeWithoutAnnotation verifies that without an explicit type
// annotation, the type is inferred from the value (i64 for integers).
func TestInferredTypeWithoutAnnotation(t *testing.T) {
	text := `x = 42`
	doc := createTestDocument(text)
	program := createTestProgram(text)

	index := createTestIndex(doc, program)
	entry, ok := index.GetDefinition("x")
	if !ok {
		t.Fatal("expected to find x in definitions")
	}
	if entry.Type != "i64" {
		t.Errorf("expected type 'i64' for unannotated integer, got '%s'", entry.Type)
	}
}
