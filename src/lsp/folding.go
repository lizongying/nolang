package lsp

import "strings"

// FoldingRangeProvider implements the textDocument/foldingRange feature.
// It computes foldable ranges based on brace matching.
type FoldingRangeProvider struct {
	doc *TextDocument
}

func NewFoldingRangeProvider(doc *TextDocument) *FoldingRangeProvider {
	return &FoldingRangeProvider{
		doc: doc,
	}
}

// GetFoldingRanges returns foldable ranges by matching opening and closing braces.
func (fp *FoldingRangeProvider) GetFoldingRanges() []FoldingRange {
	lines := getLines(fp.doc.Text)
	var ranges []FoldingRange
	stack := []int{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "{") {
			stack = append(stack, i)
		} else if strings.HasPrefix(trimmed, "}") {
			if len(stack) > 0 {
				start := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				ranges = append(ranges, FoldingRange{
					StartLine: uint32(start),
					EndLine:   uint32(i),
					Kind:      "region",
				})
			}
		}
	}

	return ranges
}