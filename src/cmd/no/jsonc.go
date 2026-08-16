package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file implements a self-contained JSONC parser (JSON with Comments).
//
// JSONC extends standard JSON with:
//   - Line comments:   // until end of line
//   - Block comments:  /* ... */
//   - Trailing commas: {"a": 1,}  or  [1, 2,]
//
// The parser does NOT use encoding/json. It is a hand-written recursive
// descent parser that produces Go native types directly:
//
//	map[string]any  for JSON objects
//	[]any           for JSON arrays
//	string                  for JSON strings
//	float64                 for JSON numbers
//	bool                    for JSON booleans
//	nil                     for JSON null

// ─── Public API ───────────────────────────────────────────────────────────────

// jsoncParse parses JSONC data and returns the result as any.
func jsoncParse(data []byte) (any, error) {
	p := &jsoncParser{data: data, line: 1, col: 1}
	p.skipSpace()
	if p.pos >= len(p.data) {
		return nil, p.errf("empty input")
	}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos < len(p.data) {
		return nil, p.errf("unexpected character %q after value", p.data[p.pos])
	}
	return v, nil
}

// jsoncParseMap parses JSONC data that is expected to be a flat object whose
// values are all strings (e.g. workspace.jsonc). Returns map[string]string.
func jsoncParseMap(data []byte) (map[string]string, error) {
	v, err := jsoncParse(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("jsonc: expected object, got %T", v)
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("jsonc: value for key %q is not a string (got %T)", k, val)
		}
		result[k] = s
	}
	return result, nil
}

// jsoncMarshalMap serialises a map[string]string to JSONC text with 2-space
// indentation. Keys are sorted for deterministic output.
func jsoncMarshalMap(m map[string]string) string {
	if len(m) == 0 {
		return "{\n}\n"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("{\n")
	for i, k := range keys {
		sb.WriteString("  ")
		sb.WriteString(jsoncQuote(k))
		sb.WriteString(": ")
		sb.WriteString(jsoncQuote(m[k]))
		if i < len(keys)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("}\n")
	return sb.String()
}

// ─── Parser ──────────────────────────────────────────────────────────────────

type jsoncParser struct {
	data []byte
	pos  int
	line int
	col  int
}

// parseValue dispatches on the first non-whitespace character.
func (p *jsoncParser) parseValue() (any, error) {
	p.skipSpace()
	if p.pos >= len(p.data) {
		return nil, p.errf("unexpected end of input")
	}
	c := p.data[p.pos]
	switch {
	case c == '"':
		return p.parseString()
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, p.errf("unexpected character %q", c)
	}
}

// parseString parses a double-quoted JSON string with escape sequences.
func (p *jsoncParser) parseString() (string, error) {
	if p.data[p.pos] != '"' {
		return "", p.errf("expected '\"'")
	}
	p.advance() // skip opening quote

	var sb strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '"' {
			p.advance() // closing quote
			return sb.String(), nil
		}
		if c == '\\' {
			p.advance()
			if p.pos >= len(p.data) {
				return "", p.errf("unexpected end of input in string escape")
			}
			esc := p.data[p.pos]
			p.advance()
			switch esc {
			case '"', '\\', '/':
				sb.WriteByte(esc)
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				if p.pos+4 > len(p.data) {
					return "", p.errf("incomplete unicode escape")
				}
				hex := string(p.data[p.pos : p.pos+4])
				code, err := strconv.ParseInt(hex, 16, 32)
				if err != nil {
					return "", p.errf("invalid unicode escape \\u%s", hex)
				}
				p.pos += 4
				p.col += 4
				sb.WriteRune(rune(code))
			default:
				return "", p.errf("invalid escape \\%c", esc)
			}
		} else {
			sb.WriteByte(c)
			p.advance()
		}
	}
	return "", p.errf("unterminated string")
}

// parseObject parses a JSON object. Supports trailing commas and comments.
func (p *jsoncParser) parseObject() (map[string]any, error) {
	p.advance() // skip {
	result := make(map[string]any)

	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.advance()
		return result, nil
	}

	for {
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.errf("unexpected end of input in object")
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.errf("expected ':' after key %q", key)
		}
		p.advance() // skip :

		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		result[key] = val

		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.errf("unexpected end of input in object")
		}
		switch p.data[p.pos] {
		case '}':
			p.advance()
			return result, nil
		case ',':
			p.advance()
			// Trailing comma: allow ,}
			p.skipSpace()
			if p.pos < len(p.data) && p.data[p.pos] == '}' {
				p.advance()
				return result, nil
			}
		default:
			return nil, p.errf("expected ',' or '}' in object, got %q", p.data[p.pos])
		}
	}
}

// parseArray parses a JSON array. Supports trailing commas and comments.
func (p *jsoncParser) parseArray() ([]any, error) {
	p.advance() // skip [
	var result []any

	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.advance()
		return result, nil
	}

	for {
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		result = append(result, val)

		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.errf("unexpected end of input in array")
		}
		switch p.data[p.pos] {
		case ']':
			p.advance()
			return result, nil
		case ',':
			p.advance()
			// Trailing comma: allow ,]
			p.skipSpace()
			if p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.advance()
				return result, nil
			}
		default:
			return nil, p.errf("expected ',' or ']' in array, got %q", p.data[p.pos])
		}
	}
}

// parseBool parses true / false.
func (p *jsoncParser) parseBool() (bool, error) {
	if p.match("true") {
		return true, nil
	}
	if p.match("false") {
		return false, nil
	}
	return false, p.errf("invalid boolean literal")
}

// parseNull parses null.
func (p *jsoncParser) parseNull() (any, error) {
	if p.match("null") {
		return nil, nil
	}
	return nil, p.errf("invalid null literal")
}

// parseNumber parses a JSON number (integer or float, with optional exponent).
func (p *jsoncParser) parseNumber() (float64, error) {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.advance()
	}
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.advance()
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.advance()
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.advance()
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.advance()
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.advance()
		}
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.advance()
		}
	}
	numStr := string(p.data[start:p.pos])
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, p.errf("invalid number %q", numStr)
	}
	return num, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// match checks whether the current position starts with the given keyword and
// advances past it.
func (p *jsoncParser) match(kw string) bool {
	if p.pos+len(kw) > len(p.data) {
		return false
	}
	if string(p.data[p.pos:p.pos+len(kw)]) != kw {
		return false
	}
	p.pos += len(kw)
	p.col += len(kw)
	return true
}

// skipSpace advances past whitespace, line comments, and block comments.
func (p *jsoncParser) skipSpace() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.advance()
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/':
			// Line comment: skip until newline (inclusive)
			p.pos += 2
			p.col += 2
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
				p.col++
			}
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '*':
			// Block comment: skip until */
			p.pos += 2
			p.col += 2
			for p.pos+1 < len(p.data) {
				if p.data[p.pos] == '*' && p.data[p.pos+1] == '/' {
					p.pos += 2
					p.col += 2
					break
				}
				if p.data[p.pos] == '\n' {
					p.line++
					p.col = 1
				} else {
					p.col++
				}
				p.pos++
			}
		default:
			return
		}
	}
}

// advance moves forward one byte, tracking line/column.
func (p *jsoncParser) advance() {
	if p.pos < len(p.data) {
		if p.data[p.pos] == '\n' {
			p.line++
			p.col = 1
		} else {
			p.col++
		}
		p.pos++
	}
}

// errf formats a parser error with position information.
func (p *jsoncParser) errf(format string, args ...any) error {
	return fmt.Errorf("jsonc: line %d col %d: %s", p.line, p.col, fmt.Sprintf(format, args...))
}

// jsoncQuote produces a JSON-quoted string literal from a Go string.
func jsoncQuote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range []byte(s) {
		switch c {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if c < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, c))
			} else {
				sb.WriteByte(c)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// ─── Struct extraction helpers ───────────────────────────────────────────────
// These helpers extract typed values from a parsed map[string]any.
// They are used to replace json.Unmarshal when loading package.jsonc.

func jsoncGetString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func jsoncGetBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func jsoncGetFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func jsoncGetStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, elem := range arr {
		if s, ok := elem.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func jsoncGetStringMap(m map[string]any, key string) map[string]string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(obj))
	for k, val := range obj {
		if s, ok := val.(string); ok {
			result[k] = s
		}
	}
	return result
}

// jsoncParseProjectConfig parses JSONC data into a ProjectConfig without using
// encoding/json.
func jsoncParseProjectConfig(data []byte) (*ProjectConfig, error) {
	v, err := jsoncParse(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("jsonc: expected object, got %T", v)
	}

	cfg := &ProjectConfig{
		Name:         jsoncGetString(m, "name"),
		Version:      jsoncGetString(m, "version"),
		Description:  jsoncGetString(m, "description"),
		Keywords:     jsoncGetStringSlice(m, "keywords"),
		Author:       jsoncGetString(m, "author"),
		Email:        jsoncGetString(m, "email"),
		Organization: jsoncGetString(m, "organization"),
		Repository:   jsoncGetString(m, "repository"),
		Homepage:     jsoncGetString(m, "homepage"),
		License:      jsoncGetString(m, "license"),
		Mirrors:      jsoncGetStringSlice(m, "mirrors"),
		Dependencies: jsoncGetStringMap(m, "dependencies"),
		Main:         jsoncGetString(m, "main"),
		Output:       jsoncGetString(m, "output"),
		Ignore:       jsoncGetStringSlice(m, "ignore"),
	}

	// Parse nested compiler object.
	if comp, ok := m["compiler"].(map[string]any); ok {
		cfg.Compiler = CompilerConfig{
			Version: jsoncGetString(comp, "version"),
		}
	}

	return cfg, nil
}
