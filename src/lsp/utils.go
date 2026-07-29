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

	// 按優先級嘗試長度由長到短：`...` > `!=` > `!!` > `**` > `*` > `!`
	// `!=` 和 `!!` 必須在 `!` 之前，避免將 `!=` 中的 `!` 誤匹配為迴圈關鍵字
	for _, op := range []string{"...", "!=", "!!", "**", "*", "!"} {
		opLen := len(op)
		// 嘗試 op 在 [c-opLen, c) 的位置（游標在運算符之後）
		if c >= opLen && c <= len(line) && line[c-opLen:c] == op {
			return op
		}
		// 嘗試 op 在 [c, c+opLen) 的位置（游標在運算符第一個字元）
		if c+opLen <= len(line) && line[c:c+opLen] == op {
			return op
		}
		// 嘗試 op 跨越游標（游標在運算符中間字元上，如 != 的 = 上）
		for i := 1; i < opLen; i++ {
			start := c - i
			end := c - i + opLen
			if start >= 0 && end <= len(line) && line[start:end] == op {
				return op
			}
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
