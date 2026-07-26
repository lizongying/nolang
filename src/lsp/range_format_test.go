package lsp

import (
	"testing"
)

func TestRangePatternFormat(t *testing.T) {
	input := `grade = score: {
  [0..60) -> 'F'
  [60..80) -> 'C'
  [80..90) -> 'B'
  [90..100] -> 'A'
  -> 'invalid'
}
`
	s := NewServer()
	_, err := s.documents.OpenDocument("test://range.no", input)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	result, err := s.handleTextDocumentFormatting(DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: "test://range.no"},
	})
	if err != nil {
		t.Fatalf("formatting failed: %v", err)
	}

	edits, ok := result.([]TextEdit)
	if !ok {
		t.Fatalf("expected []TextEdit, got %T", result)
	}

	if len(edits) == 0 {
		t.Log("No edits (already formatted)")
		return
	}

	t.Logf("Formatted output:\n%s", edits[0].NewText)
}
