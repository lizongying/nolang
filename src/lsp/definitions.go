package lsp

type DefinitionProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewDefinitionProvider(doc *TextDocument, index *SymbolIndex) *DefinitionProvider {
	return &DefinitionProvider{
		index: index,
		doc:   doc,
	}
}

func (dp *DefinitionProvider) GetDefinition(position Position) (Location, bool) {
	word := getWordAtPosition(dp.doc.Text, position)
	if word == "" {
		return Location{}, false
	}

	if dp.index == nil {
		return Location{}, false
	}

	entry, ok := dp.index.GetDefinition(word)
	if !ok {
		entry, ok = dp.index.Lookup(word)
		if !ok {
			return Location{}, false
		}
	}

	return entry.Location, true
}
