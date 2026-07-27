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

	// Check if the word is part of a module-qualified call (e.g. "fs.read"
	// where the cursor is on "read" and "fs." precedes it). If so, try the
	// qualified name first so that `fs.read` resolves to the `fs` module's
	// definition, not to any unrelated symbol named `read` in the index.
	qualified := getQualifiedWordAtPosition(dp.doc.Text, position)
	if qualified != "" {
		if entry, ok := dp.index.GetDefinition(qualified); ok {
			return entry.Location, true
		}
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
