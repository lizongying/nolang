package lsp

// DocumentHighlightProvider implements the textDocument/documentHighlight feature.
// It uses SymbolIndex to find all references and the definition of a symbol,
// and returns their ranges with highlight kinds.
type DocumentHighlightProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewDocumentHighlightProvider(doc *TextDocument, index *SymbolIndex) *DocumentHighlightProvider {
	return &DocumentHighlightProvider{
		index: index,
		doc:   doc,
	}
}

// GetHighlights returns document highlights for the symbol at the given position.
// References are marked as DocumentHighlightKindText, and the definition is marked as Write.
func (dp *DocumentHighlightProvider) GetHighlights(position Position) []DocumentHighlight {
	word := getWordAtPosition(dp.doc.Text, position)
	if word == "" {
		return []DocumentHighlight{}
	}

	if dp.index == nil {
		return []DocumentHighlight{}
	}

	refs := dp.index.GetReferences(word)
	var highlights []DocumentHighlight
	for _, ref := range refs {
		highlights = append(highlights, DocumentHighlight{
			Range: ref.Range,
			Kind:  DocumentHighlightKindText,
		})
	}

	def, ok := dp.index.GetDefinition(word)
	if ok {
		found := false
		for _, h := range highlights {
			if locationKey(Location{Range: h.Range}) == locationKey(def.Location) {
				found = true
				break
			}
		}
		if !found {
			highlights = append(highlights, DocumentHighlight{
				Range: def.Location.Range,
				Kind:  DocumentHighlightKindWrite,
			})
		}
	}

	return highlights
}