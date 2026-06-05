package lsp

import "strings"

// getLines splits text into lines, removing trailing empty line from trailing newline.
func getLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	// Remove trailing empty line produced by trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// getWordAtPosition returns the word at the given position in the text.
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
