package lsp

import (
	"errors"
	"strings"
	"sync"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

type DocumentManager struct {
	documents map[string]*TextDocument
	mu        sync.RWMutex
}

func NewDocumentManager() *DocumentManager {
	return &DocumentManager{
		documents: make(map[string]*TextDocument),
	}
}

func (m *DocumentManager) OpenDocument(uri string, text string) (*TextDocument, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if doc, ok := m.documents[uri]; ok {
		doc.Text = text
		doc.Item.Text = text
		doc.Item.Version++
		doc.Dirty = true
		return doc, nil
	}

	doc := &TextDocument{
		Item: TextDocumentItem{
			URI:        uri,
			LanguageID: "nolang",
			Version:    1,
			Text:       text,
		},
		Text:  text,
		Dirty: true,
	}

	m.documents[uri] = doc
	return doc, nil
}

func (m *DocumentManager) RemoveDocument(uri string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.documents, uri)

	return nil
}

func (m *DocumentManager) UpdateDocument(uri string, changes []TextDocumentContentChange, version int) (*TextDocument, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}

	for _, change := range changes {
		if change.Range == nil {
			doc.Text = change.Text
		} else {
			m.applyContentChange(doc, change)
		}
	}

	doc.Item.Version = version
	doc.Dirty = true

	return doc, nil
}

func (m *DocumentManager) applyContentChange(doc *TextDocument, change TextDocumentContentChange) {
	lines := getLines(doc.Text)
	startLine := int(change.Range.Start.Line)
	startChar := int(change.Range.Start.Character)
	endLine := int(change.Range.End.Line)
	endChar := int(change.Range.End.Character)

	// Ensure line range is valid
	if startLine >= len(lines) {
		lines = append(lines, "")
	}

	// Get prefix of start line
	before := ""
	if startLine < len(lines) {
		if startChar <= len(lines[startLine]) {
			before = lines[startLine][:startChar]
		} else {
			before = lines[startLine] + strings.Repeat(" ", startChar-len(lines[startLine]))
		}
	}

	// Get suffix of end line
	after := ""
	if endLine < len(lines) {
		if endChar <= len(lines[endLine]) {
			after = lines[endLine][endChar:]
		}
	}

	// Build new line
	newLine := before + change.Text + after

	// Replace lines in range
	newLines := make([]string, 0, len(lines)-(endLine-startLine)+1)
	newLines = append(newLines, lines[:startLine]...)
	newLines = append(newLines, newLine)
	if endLine+1 < len(lines) {
		newLines = append(newLines, lines[endLine+1:]...)
	}

	// Rejoin text
	doc.Text = strings.Join(newLines, "\n")
}

func (m *DocumentManager) GetDocument(uri string) (*TextDocument, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	doc, ok := m.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}

	return doc, nil
}

func (m *DocumentManager) GetAllDocuments() map[string]*TextDocument {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*TextDocument, len(m.documents))
	for k, v := range m.documents {
		result[k] = v
	}

	return result
}

func (m *DocumentManager) ParseDocument(uri string) (*parser.Program, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.documents[uri]
	if !ok {
		return nil, nil, ErrDocumentNotFound
	}

	l := lexer.New(doc.Text)
	p := parser.New(l)
	ast := p.ParseProgram()

	// 僅在解析成功時緩存 AST，避免部分 AST 影響後續請求
	errs := p.Errors()
	if len(errs) == 0 {
		doc.AST = ast
	}
	doc.Dirty = len(errs) > 0

	return ast, errs, nil
}

func (m *DocumentManager) IsDirty(uri string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	doc, ok := m.documents[uri]
	if !ok {
		return false
	}

	return doc.Dirty
}

var ErrDocumentNotFound = errors.New("document not found")
