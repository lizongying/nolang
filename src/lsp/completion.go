package lsp

import (
	"strings"

	"github.com/lizongying/nolang/builtin"
)

type SymbolInfo struct {
	Name      string
	Kind      int
	Detail    string
	Position  Position
	Scope     string
	IsBuiltin bool
}

type CompletionProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewCompletionProvider(doc *TextDocument, index *SymbolIndex) *CompletionProvider {
	return &CompletionProvider{
		index: index,
		doc:   doc,
	}
}

func (cp *CompletionProvider) GetCompletions(position Position, triggerCharacter string) []CompletionItem {
	trigger := getTriggerType(triggerCharacter)

	switch trigger {
	case TriggerDot:
		return cp.getCompletionsAfterDot(position)
	case TriggerColon:
		return cp.getCompletionsAfterColon(position)
	case TriggerEquals:
		return cp.getCompletionsAfterEquals(position)
	case TriggerWord:
		return cp.getWordBasedCompletions(position)
	default:
		return cp.getAllCompletions(position)
	}
}

type TriggerType int

const (
	TriggerNone TriggerType = iota
	TriggerDot
	TriggerColon
	TriggerEquals
	TriggerWord
)

func getTriggerType(trigger string) TriggerType {
	switch trigger {
	case ".":
		return TriggerDot
	case ":":
		return TriggerColon
	case "=":
		return TriggerEquals
	default:
		return TriggerWord
	}
}

func (cp *CompletionProvider) getAllCompletions(position Position) []CompletionItem {
	var items []CompletionItem
	items = append(items, cp.getKeywordCompletions()...)
	items = append(items, cp.getIdentifierCompletions(position)...)
	return items
}

func (cp *CompletionProvider) getWordBasedCompletions(position Position) []CompletionItem {
	word := cp.getCurrentWord(position)
	if word == "" {
		return cp.getAllCompletions(position)
	}
	var items []CompletionItem
	items = append(items, cp.getKeywordCompletionsWithFilter(word)...)
	items = append(items, cp.getIdentifierCompletionsWithFilter(position, word)...)
	return items
}

func (cp *CompletionProvider) getCurrentWord(position Position) string {
	return getWordAtPosition(cp.doc.Text, position)
}

func (cp *CompletionProvider) getKeywordCompletions() []CompletionItem {
	var items []CompletionItem
	for _, kw := range keywords {
		item := CompletionItem{
			Label:            kw.keyword,
			Kind:             kw.kind,
			Detail:           kw.detail,
			InsertText:       kw.snippet,
			InsertTextFormat: InsertTextFormatSnippet,
		}
		items = append(items, item)
	}
	return items
}

func (cp *CompletionProvider) getKeywordCompletionsWithFilter(filter string) []CompletionItem {
	var items []CompletionItem
	for _, kw := range keywords {
		if hasPrefixIgnoreCase(kw.keyword, filter) {
			item := CompletionItem{
				Label:            kw.keyword,
				Kind:             kw.kind,
				Detail:           kw.detail,
				InsertText:       kw.snippet,
				InsertTextFormat: InsertTextFormatSnippet,
			}
			items = append(items, item)
		}
	}
	return items
}

func (cp *CompletionProvider) getIdentifierCompletions(position Position) []CompletionItem {
	var items []CompletionItem
	if cp.index == nil {
		return items
	}
	entries := cp.index.GetSymbolsBeforeLine(position.Line)
	for _, e := range entries {
		item := CompletionItem{
			Label:  e.Name,
			Kind:   e.Kind,
			Detail: e.Type,
			TextEdit: &TextEdit{
				Range:   cp.getWordRange(position),
				NewText: e.Name,
			},
		}
		items = append(items, item)
	}
	return items
}

func (cp *CompletionProvider) getIdentifierCompletionsWithFilter(position Position, filter string) []CompletionItem {
	var items []CompletionItem
	if cp.index == nil {
		return items
	}
	entries := cp.index.GetSymbolsBeforeLine(position.Line)
	for _, e := range entries {
		if hasPrefixIgnoreCase(e.Name, filter) {
			item := CompletionItem{
				Label:  e.Name,
				Kind:   e.Kind,
				Detail: e.Type,
				TextEdit: &TextEdit{
					Range:   cp.getWordRange(position),
					NewText: e.Name,
				},
			}
			items = append(items, item)
		}
	}
	return items
}

func (cp *CompletionProvider) getWordRange(position Position) Range {
	lines := getLines(cp.doc.Text)
	if int(position.Line) >= len(lines) {
		return Range{Start: position, End: position}
	}
	line := lines[position.Line]
	start := position.Character
	end := position.Character
	if int(position.Character) >= len(line) {
		return Range{Start: position, End: position}
	}
	for start > 0 {
		if isWordChar(line[start-1]) {
			start--
		} else {
			break
		}
	}
	for end < uint32(len(line)) {
		if isWordChar(line[end]) {
			end++
		} else {
			break
		}
	}
	return Range{
		Start: Position{Line: position.Line, Character: start},
		End:   Position{Line: position.Line, Character: end},
	}
}

func (cp *CompletionProvider) getCompletionsAfterDot(position Position) []CompletionItem {
	var items []CompletionItem
	receiverStr := cp.getReceiverStringAtPosition(position)
	if receiverStr == "" {
		return items
	}

	// Builtin module completions
	moduleBuiltins := getModuleBuiltinCompletions()
	if symbols, ok := moduleBuiltins[receiverStr]; ok {
		rangeStart := Position{
			Line:      position.Line,
			Character: position.Character - uint32(len(receiverStr)) - 1,
		}
		for _, sym := range symbols {
			item := CompletionItem{
				Label:  sym.Name,
				Kind:   sym.Kind,
				Detail: sym.Detail,
				TextEdit: &TextEdit{
					Range:   Range{Start: rangeStart, End: position},
					NewText: receiverStr + "." + sym.Name,
				},
			}
			items = append(items, item)
		}
	}

	// Type-based method completions
	receiverType := cp.getReceiverTypeForString(receiverStr)
	if receiverType != "" {
		methods := cp.getMethodsForType(receiverType)
		items = append(items, methods...)
	}

	return items
}

func (cp *CompletionProvider) getReceiverStringAtPosition(position Position) string {
	lines := getLines(cp.doc.Text)
	if int(position.Line) >= len(lines) {
		return ""
	}
	line := lines[position.Line]
	if int(position.Character) < 2 {
		return ""
	}
	start := int(position.Character) - 2
	for start > 0 {
		if line[start] == '.' {
			break
		}
		start--
	}
	if start == 0 || line[start] != '.' {
		return ""
	}
	for start > 0 {
		c := line[start-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			start--
		} else {
			break
		}
	}
	if start >= int(position.Character)-1 {
		return ""
	}
	return line[start : int(position.Character)-1]
}

func (cp *CompletionProvider) getReceiverTypeForString(receiverStr string) string {
	if cp.index == nil {
		return ""
	}
	entry, ok := cp.index.Lookup(receiverStr)
	if !ok {
		return ""
	}
	return entry.Type
}

func (cp *CompletionProvider) getMethodsForType(typeName string) []CompletionItem {
	var items []CompletionItem
	if cp.index == nil {
		return items
	}
	for _, m := range builtin.BuiltinMethodList {
		if m.ReceiverType.String() != "" && m.ReceiverType.String() == typeName {
			item := CompletionItem{
				Label:  m.MethodName,
				Kind:   CompletionItemKindMethod,
				Detail: m.Doc,
			}
			items = append(items, item)
		}
	}
	return items
}

func (cp *CompletionProvider) getCompletionsAfterColon(position Position) []CompletionItem {
	var items []CompletionItem
	typeNames := []struct {
		name   string
		detail string
	}{
		{"i8", "int8"}, {"i16", "int16"}, {"i32", "int32"}, {"i64", "int64"},
		{"u8", "uint8"}, {"u16", "uint16"}, {"u32", "uint32"}, {"u64", "uint64"},
		{"f32", "float32"}, {"f64", "float64"},
		{"byte", "byte"}, {"char", "char"}, {"str", "string"}, {"bool", "boolean"},
	}
	for _, t := range typeNames {
		items = append(items, CompletionItem{
			Label:  t.name,
			Kind:   CompletionItemKindTypeParameter,
			Detail: t.detail,
		})
	}
	return items
}

func (cp *CompletionProvider) getCompletionsAfterEquals(position Position) []CompletionItem {
	var items []CompletionItem
	items = append(items,
		CompletionItem{Label: "true", Kind: CompletionItemKindKeyword, Detail: "boolean true", InsertText: "true"},
		CompletionItem{Label: "false", Kind: CompletionItemKindKeyword, Detail: "boolean false", InsertText: "false"},
		CompletionItem{Label: "nil", Kind: CompletionItemKindKeyword, Detail: "null value", InsertText: "nil"},
	)
	entries := cp.index.GetSymbolsBeforeLine(position.Line)
	for _, e := range entries {
		if e.Kind == CompletionItemKindVariable || e.Kind == CompletionItemKindConstant {
			items = append(items, CompletionItem{
				Label:  e.Name,
				Kind:   e.Kind,
				Detail: e.Type,
			})
		}
	}
	return items
}

func (cp *CompletionProvider) GetFunctionDeclarations() []CompletionItem {
	var items []CompletionItem
	funcs := cp.index.GetAllFunctions()
	for _, e := range funcs {
		item := CompletionItem{
			Label:  e.Name,
			Kind:   CompletionItemKindFunction,
			Detail: e.Type,
		}
		items = append(items, item)
	}
	return items
}

func (cp *CompletionProvider) ResolveCompletionItem(item CompletionItem) CompletionItem {
	if item.Kind == CompletionItemKindFunction {
		if cp.index != nil {
			entry, ok := cp.index.Lookup(item.Label)
			if ok {
				item.Documentation = entry.Value
			} else {
				item.Documentation = "Function"
			}
		} else {
			item.Documentation = "Function"
		}
	} else if item.Kind == CompletionItemKindVariable {
		if cp.index != nil {
			entry, ok := cp.index.Lookup(item.Label)
			if ok && entry.Value != "" {
				item.Documentation = "Value: " + entry.Value
			}
		}
	} else if item.Kind == CompletionItemKindKeyword {
		item.Documentation = "Nolang keyword"
	}
	return item
}

// getModuleBuiltinCompletions returns completions for module-like prefixes
func getModuleBuiltinCompletions() map[string][]SymbolInfo {
	result := make(map[string][]SymbolInfo)
	seen := make(map[string]map[string]bool)

	for _, m := range builtin.BuiltinMethodList {
		parts := strings.SplitN(m.MethodName, "-", 2)
		if len(parts) < 2 {
			continue
		}
		module := parts[0]
		if seen[module] == nil {
			seen[module] = make(map[string]bool)
		}
		if seen[module][m.MethodName] {
			continue
		}
		seen[module][m.MethodName] = true
		result[module] = append(result[module], SymbolInfo{
			Name:   m.MethodName,
			Kind:   CompletionItemKindFunction,
			Detail: m.Doc,
		})
	}

	// Add common module-like prefixes
	result["fmt"] = append(result["fmt"],
		SymbolInfo{Name: "println", Kind: CompletionItemKindFunction, Detail: "Print with newline"},
		SymbolInfo{Name: "print", Kind: CompletionItemKindFunction, Detail: "Print without newline"},
		SymbolInfo{Name: "printf", Kind: CompletionItemKindFunction, Detail: "Formatted print"},
	)

	return result
}

type keywordDef struct {
	keyword string
	kind    int
	detail  string
	snippet string
}

var keywords = []keywordDef{
	{"if", CompletionItemKindKeyword, "if statement", "if ${1:condition} {\n\t$0\n}"},
	{"else", CompletionItemKindKeyword, "else statement", "else {\n\t$0\n}"},
	{"elif", CompletionItemKindKeyword, "else if statement", "elif ${1:condition} {\n\t$0\n}"},
	{"for", CompletionItemKindKeyword, "for loop", "for ${1:condition} {\n\t$0\n}"},
	{"break", CompletionItemKindKeyword, "break statement", "break"},
	{"continue", CompletionItemKindKeyword, "continue statement", "continue"},
	{"return", CompletionItemKindKeyword, "return statement", "return"},
	{"true", CompletionItemKindKeyword, "boolean true", "true"},
	{"false", CompletionItemKindKeyword, "boolean false", "false"},
	{"nil", CompletionItemKindKeyword, "null value", "nil"},
	{"match", CompletionItemKindKeyword, "match expression", "match ${1:expr} {\n\t${2:pattern} => ${3:value},\n\t_ => ${4:default},\n}"},
	{"fn", CompletionItemKindKeyword, "function literal", "fn(${1:params}) {\n\t$0\n}"},
}

func hasPrefixIgnoreCase(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		sc := toLower(s[i])
		pc := toLower(prefix[i])
		if sc != pc {
			return false
		}
	}
	return true
}

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

func toLowerStr(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		b[i] = toLower(s[i])
	}
	return string(b)
}

func containsIgnoreCase(s, lowerQuery string) bool {
	return strings.Contains(toLowerStr(s), lowerQuery)
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}
