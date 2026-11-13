package lsp

// SignatureHelpProvider implements the textDocument/signatureHelp feature.
// It uses SymbolIndex to find function definitions and return parameter information.
type SignatureHelpProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewSignatureHelpProvider(doc *TextDocument, index *SymbolIndex) *SignatureHelpProvider {
	return &SignatureHelpProvider{
		index: index,
		doc:   doc,
	}
}

// GetSignatureHelp returns signature information for the function call at the given position.
// It looks up the word before the cursor position and finds the corresponding function entry.
func (sp *SignatureHelpProvider) GetSignatureHelp(position Position) (*SignatureHelp, bool) {
	word := getWordBeforePosition(sp.doc.Text, position)
	if word == "" {
		return nil, false
	}

	if sp.index == nil {
		return nil, false
	}

	entry, ok := sp.index.Lookup(word)
	if !ok {
		return nil, false
	}

	if len(entry.Params) == 0 {
		return nil, false
	}

	var paramsInfo []ParameterInformation
	for _, p := range entry.Params {
		label := p.Name
		if p.Type != "" {
			label += " " + p.Type
		}
		paramsInfo = append(paramsInfo, ParameterInformation{
			Label: label,
		})
	}

	return &SignatureHelp{
		Signatures: []SignatureInformation{
			{
				Label:      entry.Type,
				Parameters: paramsInfo,
			},
		},
		ActiveSignature: 0,
		ActiveParameter: 0,
	}, true
}