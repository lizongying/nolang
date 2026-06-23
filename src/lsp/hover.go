package lsp

import (
	"fmt"
	"strings"
)

type HoverProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewHoverProvider(doc *TextDocument, index *SymbolIndex) *HoverProvider {
	return &HoverProvider{
		index: index,
		doc:   doc,
	}
}

func (hp *HoverProvider) GetHover(position Position) (*Hover, bool) {
	word := getWordAtPosition(hp.doc.Text, position)
	if word == "" {
		return nil, false
	}

	if hp.index == nil {
		return nil, false
	}

	entry, ok := hp.index.LookupAtPosition(word, position)
	if !ok {
		entry, ok = hp.index.GetDefinition(word)
		if !ok {
			return nil, false
		}
	}

	contents := hp.formatHoverContent(entry)
	return &Hover{
		Contents: contents,
	}, true
}

func (hp *HoverProvider) formatHoverContent(entry *IndexEntry) interface{} {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("**%s**\n\n", entry.Name))

	if entry.Type != "" {
		builder.WriteString(fmt.Sprintf("- **Type**: `%s`\n", entry.Type))
	}

	if entry.Location.URI != "" {
		line := entry.Location.Range.Start.Line + 1
		col := entry.Location.Range.Start.Character + 1
		builder.WriteString(fmt.Sprintf("- **Declared at**: line %d, column %d\n", line, col))
	}

	if entry.Value != "" {
		builder.WriteString(fmt.Sprintf("- **Value**: %s\n", entry.Value))
	}

	if len(entry.Params) > 0 {
		builder.WriteString("- **Parameters**:\n")
		for _, p := range entry.Params {
			if p.Type != "" {
				builder.WriteString(fmt.Sprintf("  - `%s: %s`\n", p.Name, p.Type))
			} else {
				builder.WriteString(fmt.Sprintf("  - `%s`\n", p.Name))
			}
		}
	}

	return MarkupContent{
		Kind:  MarkupKindMarkdown,
		Value: builder.String(),
	}
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
