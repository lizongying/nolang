package lsp

import (
	"fmt"
	"strings"
)

// keywordDoc 為 LSP hover 提供新式/舊式關鍵字文檔
var keywordDoc = map[string]string{
	"!":     "**舊式語法（已廢棄）** — `! { }` / `!! { }` 無限迴圈。請改用新式 `{ } ()`。\n\n```nolang\n{\n    *     // break\n    **    // continue\n} ()\n```",
	"*":     "**新式語法** — 跳出當前迴圈（break）。",
	"**":    "**新式語法** — 跳過當前迴圈迭代（continue）。",
	"...":   "**新式語法** — 終止當前語句序列並回傳值，類似舊式 `return` 後接值。",
	"if":    "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"elif":  "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"else":  "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"for":   "**舊式語法（已廢棄）** — 請改用新式 `{ } (cond)` 條件迴圈、`{ } ()` 無限迴圈、`{ } * N` 計次迴圈或 `i <- [a..b]: { }` 範圍迴圈。",
	"match": "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
}

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
	word := getTokenAtPosition(hp.doc.Text, position)
	if word == "" {
		return nil, false
	}

	// 關鍵字 hover（新舊語法）
	if doc, ok := keywordDoc[word]; ok {
		return &Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindMarkdown,
				Value: doc,
			},
		}, true
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

	if entry.Doc != "" {
		builder.WriteString(fmt.Sprintf("%s\n\n", entry.Doc))
	}

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

	if entry.Type == "enum" && entry.Value != "" {
		builder.WriteString(fmt.Sprintf("- **Variants**: %s\n", entry.Value))
	}

	if entry.Kind == SymbolKindEnumMember && entry.Type != "" {
		builder.WriteString(fmt.Sprintf("- **Enum**: `%s`\n", entry.Type))
	}

	if len(entry.Params) > 0 {
		builder.WriteString("- **Parameters**:\n")
		for _, p := range entry.Params {
			if p.Type != "" {
				if p.DefaultValue != "" {
					builder.WriteString(fmt.Sprintf("  - `%s: %s = %s`\n", p.Name, p.Type, p.DefaultValue))
				} else {
					builder.WriteString(fmt.Sprintf("  - `%s: %s`\n", p.Name, p.Type))
				}
			} else {
				builder.WriteString(fmt.Sprintf("  - `%s`\n", p.Name))
			}
		}
	}

	if len(entry.ResultParams) > 0 {
		builder.WriteString("- **Returns**:\n")
		for _, r := range entry.ResultParams {
			if r.Type != "" {
				builder.WriteString(fmt.Sprintf("  - `%s: %s`\n", r.Name, r.Type))
			} else {
				builder.WriteString(fmt.Sprintf("  - `%s`\n", r.Name))
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
