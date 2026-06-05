package lsp

import (
	"testing"
)

func TestDocumentManagerNew(t *testing.T) {
	dm := NewDocumentManager()
	if dm == nil {
		t.Fatal("NewDocumentManager returned nil")
	}
	if dm.documents == nil {
		t.Error("documents map is nil")
	}
}

func TestDocumentOpen(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`

	doc, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenDocument returned nil document")
	}
	if doc.Text != text {
		t.Errorf("expected text %q, got %q", text, doc.Text)
	}
	if doc.Item.URI != uri {
		t.Errorf("expected URI %q, got %q", uri, doc.Item.URI)
	}
	if doc.Item.LanguageID != "nolang" {
		t.Errorf("expected language ID 'nolang', got %q", doc.Item.LanguageID)
	}
	if doc.Item.Version != 1 {
		t.Errorf("expected version 1, got %d", doc.Item.Version)
	}
	if !doc.Dirty {
		t.Error("expected Dirty to be true for new document")
	}
}

func TestDocumentOpenExisting(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text1 := `x = 10`
	text2 := `y = 20`

	doc1, err := dm.OpenDocument(uri, text1)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	doc2, err := dm.OpenDocument(uri, text2)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	if doc1 != doc2 {
		t.Error("expected same document instance for same URI")
	}
	if doc2.Text != text2 {
		t.Errorf("expected text %q, got %q", text2, doc2.Text)
	}
	if !doc2.Dirty {
		t.Error("expected Dirty to be true after update")
	}
}

func TestDocumentClose(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	err = dm.CloseDocument(uri)
	if err != nil {
		t.Fatalf("CloseDocument failed: %v", err)
	}

	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.Dirty {
		t.Error("expected Dirty to be false after CloseDocument")
	}
}

func TestDocumentCloseNotFound(t *testing.T) {
	dm := NewDocumentManager()
	err := dm.CloseDocument("file:///nonexistent.no")
	if err != nil {
		t.Fatalf("CloseDocument should not return error for non-existent document: %v", err)
	}
}

func TestDocumentRemove(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	err = dm.RemoveDocument(uri)
	if err != nil {
		t.Fatalf("RemoveDocument failed: %v", err)
	}

	_, err = dm.GetDocument(uri)
	if err == nil {
		t.Error("expected error when getting removed document")
	}
}

func TestDocumentRemoveNotFound(t *testing.T) {
	dm := NewDocumentManager()
	err := dm.RemoveDocument("file:///nonexistent.no")
	if err != nil {
		t.Fatalf("RemoveDocument should not return error for non-existent document: %v", err)
	}
}

func TestDocumentUpdateFull(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`
	newText := `y = 20`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	doc, err := dm.UpdateDocument(uri, []TextDocumentContentChange{
		{Text: newText},
	}, 2)
	if err != nil {
		t.Fatalf("UpdateDocument failed: %v", err)
	}

	if doc.Text != newText {
		t.Errorf("expected text %q, got %q", newText, doc.Text)
	}
	if doc.Item.Version != 2 {
		t.Errorf("expected version 2, got %d", doc.Item.Version)
	}
}

func TestDocumentUpdateIncremental(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`
	newText := `y = 20`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	doc, err := dm.UpdateDocument(uri, []TextDocumentContentChange{
		{
			Range: &Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 6},
			},
			Text: "y = 20",
		},
	}, 2)
	if err != nil {
		t.Fatalf("UpdateDocument failed: %v", err)
	}

	if doc.Text != newText {
		t.Errorf("expected text %q, got %q", newText, doc.Text)
	}
}

func TestDocumentUpdateNotFound(t *testing.T) {
	dm := NewDocumentManager()
	_, err := dm.UpdateDocument("file:///nonexistent.no", []TextDocumentContentChange{
		{Text: "new text"},
	}, 1)
	if err == nil {
		t.Error("expected error when updating non-existent document")
	}
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestDocumentGet(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.Text != text {
		t.Errorf("expected text %q, got %q", text, doc.Text)
	}
}

func TestDocumentGetNotFound(t *testing.T) {
	dm := NewDocumentManager()
	_, err := dm.GetDocument("file:///nonexistent.no")
	if err == nil {
		t.Error("expected error when getting non-existent document")
	}
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestDocumentGetAll(t *testing.T) {
	dm := NewDocumentManager()

	dm.OpenDocument("file:///test1.no", "x = 10")
	dm.OpenDocument("file:///test2.no", "y = 20")

	docs := dm.GetAllDocuments()
	if len(docs) != 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}
}

func TestDocumentParse(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	ast, parseErrors, err := dm.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	if ast == nil {
		t.Fatal("ParseDocument returned nil AST")
	}
	if len(parseErrors) != 0 {
		t.Errorf("unexpected parse errors: %v", parseErrors)
	}

	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.AST != ast {
		t.Error("document AST was not updated")
	}
	if doc.Dirty {
		t.Error("expected Dirty to be false after ParseDocument")
	}
}

func TestDocumentParseNotFound(t *testing.T) {
	dm := NewDocumentManager()
	_, _, err := dm.ParseDocument("file:///nonexistent.no")
	if err == nil {
		t.Error("expected error when parsing non-existent document")
	}
	if err != ErrDocumentNotFound {
		t.Errorf("expected ErrDocumentNotFound, got %v", err)
	}
}

func TestDocumentIsDirty(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `x = 10`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	if !dm.IsDirty(uri) {
		t.Error("expected IsDirty to be true for new document")
	}

	_, _, err = dm.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}

	if dm.IsDirty(uri) {
		t.Error("expected IsDirty to be false after ParseDocument")
	}
}

func TestDocumentIsDirtyNotFound(t *testing.T) {
	dm := NewDocumentManager()
	if dm.IsDirty("file:///nonexistent.no") {
		t.Error("expected IsDirty to be false for non-existent document")
	}
}

func TestDocumentNotFoundError(t *testing.T) {
	err := &DocumentNotFoundError{}
	if err.Error() != "document not found" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestDocumentOpenWithFunction(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `swap = func(a, b) {
    temp = a
    a = b
    b = temp
}`

	doc, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if doc == nil {
		t.Fatal("OpenDocument returned nil document")
	}
	if doc.Text != text {
		t.Errorf("expected text %q, got %q", text, doc.Text)
	}
}

func TestDocumentParseFunction(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"
	text := `swap = func(a, b) {
    temp = a
    a = b
    b = temp
}`

	_, err := dm.OpenDocument(uri, text)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	ast, parseErrors, err := dm.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	if ast == nil {
		t.Fatal("ParseDocument returned nil AST")
	}
	if len(parseErrors) != 0 {
		t.Errorf("unexpected parse errors: %v", parseErrors)
	}
}

func TestDocumentMultipleUpdates(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/test.no"

	dm.OpenDocument(uri, "x = 10")
	dm.UpdateDocument(uri, []TextDocumentContentChange{{Text: "y = 20"}}, 2)
	dm.UpdateDocument(uri, []TextDocumentContentChange{{Text: "z = 30"}}, 3)

	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.Text != "z = 30" {
		t.Errorf("expected text 'z = 30', got %q", doc.Text)
	}
	if doc.Item.Version != 3 {
		t.Errorf("expected version 3, got %d", doc.Item.Version)
	}
}