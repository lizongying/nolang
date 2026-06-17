package lsp

type ReferencesProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewReferencesProvider(doc *TextDocument, index *SymbolIndex) *ReferencesProvider {
	return &ReferencesProvider{
		index: index,
		doc:   doc,
	}
}

func (rp *ReferencesProvider) GetReferences(position Position, includeDeclaration bool) []Location {
	word := getWordAtPosition(rp.doc.Text, position)
	if word == "" {
		return []Location{}
	}

	if rp.index == nil {
		return []Location{}
	}

	refs := rp.index.GetReferences(word)

	if includeDeclaration {
		entry, ok := rp.index.GetDefinition(word)
		if ok {
			// Check if declaration is already in refs
			declKey := locationKey(entry.Location)
			found := false
			for _, r := range refs {
				if locationKey(r) == declKey {
					found = true
					break
				}
			}
			if !found {
				refs = append(refs, entry.Location)
			}
		}
	}

	return refs
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

func NewReferenceParams(textDocument TextDocumentIdentifier, position Position, includeDeclaration bool) ReferenceParams {
	return ReferenceParams{
		TextDocument: textDocument,
		Position:     position,
		Context: ReferenceContext{
			IncludeDeclaration: includeDeclaration,
		},
	}
}
