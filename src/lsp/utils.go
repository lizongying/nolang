package lsp

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

func getLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func getWordAtPosition(text string, position Position) string {
	lines := getLines(text)
	if int(position.Line) >= len(lines) {
		return ""
	}
	line := lines[position.Line]
	if int(position.Character) >= len(line) {
		return ""
	}
	start := position.Character
	for start > 0 {
		if isWordChar(line[start-1]) {
			start--
		} else {
			break
		}
	}
	end := position.Character
	for end < uint32(len(line)) {
		if isWordChar(line[end]) {
			end++
		} else {
			break
		}
	}
	if start == end {
		return ""
	}
	return line[start:end]
}

func getProgram(doc *TextDocument) *parser.Program {
	return doc.AST
}

func locationFromIdentifier(uri string, ident *parser.Identifier) Location {
	if ident == nil {
		return Location{}
	}
	return Location{
		URI: uri,
		Range: Range{
			Start: Position{
				Line:      uint32(ident.Token.Line - 1),
				Character: uint32(ident.Token.Column - 1),
			},
			End: Position{
				Line:      uint32(ident.Token.Line - 1),
				Character: uint32(ident.Token.Column - 1 + len(ident.Token.Literal)),
			},
		},
	}
}

func locationKey(loc Location) string {
	return loc.URI + ":" + string(rune(loc.Range.Start.Line)) + ":" + string(rune(loc.Range.Start.Character))
}
