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

// getQualifiedWordAtPosition checks if the word at the cursor position is
// preceded by a module qualifier (e.g. "fs" in "fs.read"). If so, it returns
// the fully qualified name ("fs.read"); otherwise it returns "".
//
// This enables module-aware go-to-definition: when the user clicks on `read`
// in `fs.read(...)`, the LSP can look up `fs.read` directly instead of the
// ambiguous short name `read`.
func getQualifiedWordAtPosition(text string, position Position) string {
	lines := getLines(text)
	if int(position.Line) >= len(lines) {
		return ""
	}
	line := lines[position.Line]
	if int(position.Character) >= len(line) {
		return ""
	}

	// Find the word boundaries at the cursor position
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

	// Check if the word is immediately preceded by a dot
	if start == 0 || line[start-1] != '.' {
		return ""
	}

	// Extract the module name before the dot
	modEnd := start - 1
	modStart := modEnd
	for modStart > 0 && isWordChar(line[modStart-1]) {
		modStart--
	}
	if modStart >= modEnd {
		return ""
	}

	moduleName := line[modStart:modEnd]
	word := line[start:end]
	return moduleName + "." + word
}

// getTokenAtPosition 在游標位置查找新式語法運算符（`!`/`*`/`**`/`...`）
// 游標可在運算符任意字元上。
func getTokenAtPosition(text string, position Position) string {
	lines := getLines(text)
	if int(position.Line) >= len(lines) {
		return ""
	}
	line := lines[position.Line]
	if int(position.Character) > len(line) {
		return ""
	}
	c := int(position.Character)

	// 按優先級嘗試長度由長到短：`...` > `**` > `*` > `!`
	for _, op := range []string{"...", "**", "*", "!"} {
		// 嘗試 op 在 [c-len(op), c) 的位置
		if c >= len(op) && c <= len(line) && line[c-len(op):c] == op {
			return op
		}
		// 嘗試 op 在 [c, c+len(op)) 的位置
		if c+len(op) <= len(line) && line[c:c+len(op)] == op {
			return op
		}
	}

	// 回退到一般單字
	return getWordAtPosition(text, position)
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
