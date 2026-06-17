package lsp

// RenameProvider implements the textDocument/rename feature.
// It uses SymbolIndex to find all references and the definition of a symbol,
// then creates a WorkspaceEdit to rename all occurrences.
type RenameProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewRenameProvider(doc *TextDocument, index *SymbolIndex) *RenameProvider {
	return &RenameProvider{
		index: index,
		doc:   doc,
	}
}

// GetRenameEdits returns a WorkspaceEdit that renames the symbol at the given position.
// It finds all references and the definition, and creates TextEdits for each.
func (rp *RenameProvider) GetRenameEdits(position Position, newName string) (*WorkspaceEdit, bool) {
	word := getWordAtPosition(rp.doc.Text, position)
	if word == "" {
		return nil, false
	}

	if rp.index == nil {
		return nil, false
	}

	changes := make(map[string][]TextEdit)
	uri := rp.doc.Item.URI

	// Collect definition location
	if entry, ok := rp.index.GetDefinition(word); ok {
		changes[uri] = append(changes[uri], TextEdit{
			Range:   entry.Location.Range,
			NewText: newName,
		})
	}

	// Collect reference locations (skip duplicates with definition)
	refs := rp.index.GetReferences(word)
	defKey := ""
	if entry, ok := rp.index.GetDefinition(word); ok {
		defKey = locationKey(entry.Location)
	}
	for _, ref := range refs {
		if locationKey(ref) == defKey {
			continue
		}
		changes[uri] = append(changes[uri], TextEdit{
			Range:   ref.Range,
			NewText: newName,
		})
	}

	if len(changes) == 0 {
		return nil, false
	}

	return &WorkspaceEdit{
		Changes: changes,
	}, true
}