package lsp

import (
	"fmt"

	"github.com/lizongying/nolang/parser"
)

type CompletionProvider struct {
	program *parser.Program
	doc     *TextDocument
}

type SymbolInfo struct {
	Name      string
	Kind      int
	Detail    string
	Position  Position
	Scope     string
	IsBuiltin bool
}

func NewCompletionProvider(doc *TextDocument, program *parser.Program) *CompletionProvider {
	return &CompletionProvider{
		program: program,
		doc:     doc,
	}
}

func (cp *CompletionProvider) GetCompletions(position Position, triggerCharacter string) []CompletionItem {
	var items []CompletionItem

	trigger := getTriggerType(triggerCharacter)

	switch trigger {
	case TriggerDot:
		items = cp.getCompletionsAfterDot(position)
	case TriggerColon:
		items = cp.getCompletionsAfterColon(position)
	case TriggerEquals:
		items = cp.getCompletionsAfterEquals(position)
	case TriggerWord:
		items = cp.getWordBasedCompletions(position)
	default:
		items = cp.getAllCompletions(position)
	}

	return items
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
	var items []CompletionItem

	word := cp.getCurrentWord(position)
	if word == "" {
		return cp.getAllCompletions(position)
	}

	items = append(items, cp.getKeywordCompletionsWithFilter(word)...)
	items = append(items, cp.getIdentifierCompletionsWithFilter(position, word)...)

	return items
}

func (cp *CompletionProvider) getCurrentWord(position Position) string {
	return getWordAtPosition(cp.doc.Text, position)
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

func (cp *CompletionProvider) getKeywordCompletions() []CompletionItem {
	keywords := []struct {
		keyword string
		kind    int
		detail  string
		snippet string
	}{
		{"if", CompletionItemKindKeyword, "if statement", "if ${1:condition} {\n\t$0\n}"},
		{"else", CompletionItemKindKeyword, "else statement", "else {\n\t$0\n}"},
		{"for", CompletionItemKindKeyword, "for loop", "for ${1:condition} {\n\t$0\n}"},
		{"break", CompletionItemKindKeyword, "break statement", "break"},
		{"return", CompletionItemKindKeyword, "return statement", "return"},
		{"true", CompletionItemKindKeyword, "boolean true", "true"},
		{"false", CompletionItemKindKeyword, "boolean false", "false"},
		{"nil", CompletionItemKindKeyword, "null value", "nil"},
	}

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
	keywords := []struct {
		keyword string
		kind    int
		detail  string
		snippet string
	}{
		{"if", CompletionItemKindKeyword, "if statement", "if ${1:condition} {\n\t$0\n}"},
		{"else", CompletionItemKindKeyword, "else statement", "else {\n\t$0\n}"},
		{"for", CompletionItemKindKeyword, "for loop", "for ${1:condition} {\n\t$0\n}"},
		{"break", CompletionItemKindKeyword, "break statement", "break"},
		{"return", CompletionItemKindKeyword, "return statement", "return"},
		{"true", CompletionItemKindKeyword, "boolean true", "true"},
		{"false", CompletionItemKindKeyword, "boolean false", "false"},
		{"nil", CompletionItemKindKeyword, "null value", "nil"},
	}

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

func (cp *CompletionProvider) getIdentifierCompletions(position Position) []CompletionItem {
	var items []CompletionItem

	symbols := cp.collectSymbols(position)

	for _, sym := range symbols {
		item := CompletionItem{
			Label:  sym.Name,
			Kind:   sym.Kind,
			Detail: sym.Detail,
			TextEdit: &TextEdit{
				Range:   cp.getWordRange(position),
				NewText: sym.Name,
			},
		}
		items = append(items, item)
	}

	return items
}

func (cp *CompletionProvider) getIdentifierCompletionsWithFilter(position Position, filter string) []CompletionItem {
	var items []CompletionItem

	symbols := cp.collectSymbols(position)

	for _, sym := range symbols {
		if hasPrefixIgnoreCase(sym.Name, filter) {
			item := CompletionItem{
				Label:  sym.Name,
				Kind:   sym.Kind,
				Detail: sym.Detail,
				TextEdit: &TextEdit{
					Range:   cp.getWordRange(position),
					NewText: sym.Name,
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

func (cp *CompletionProvider) collectSymbols(position Position) []SymbolInfo {
	var symbols []SymbolInfo

	if cp.program == nil {
		return symbols
	}

	for _, stmt := range cp.program.Statements {
		cp.collectSymbolsFromStatement(stmt, &symbols, position.Line)
	}

	return symbols
}

func (cp *CompletionProvider) collectSymbolsFromStatement(stmt parser.Statement, symbols *[]SymbolInfo, beforeLine uint32) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name != nil {
			line := uint32(0)
			if s.Name.Token.Line > 0 {
				line = uint32(s.Name.Token.Line - 1)
			}
			*symbols = append(*symbols, SymbolInfo{
				Name:     s.Name.Value,
				Kind:     CompletionItemKindVariable,
				Detail:   cp.getTypeDetail(s.Value),
				Position: Position{Line: line, Character: uint32(s.Name.Token.Column - 1)},
			})
		}

		if s.Value != nil {
			cp.collectSymbolsFromExpression(s.Value, symbols, beforeLine)
		}

	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			cp.collectSymbolsFromExpression(s.ReturnValue, symbols, beforeLine)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			cp.collectSymbolsFromExpression(s.Expression, symbols, beforeLine)
		}

	case *parser.BlockStatement:
		for _, innerStmt := range s.Statements {
			cp.collectSymbolsFromStatement(innerStmt, symbols, beforeLine)
		}
	}
}

func (cp *CompletionProvider) collectSymbolsFromExpression(expr parser.Expression, symbols *[]SymbolInfo, beforeLine uint32) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *parser.Identifier:
		// Identifier 是值引用，不添加为补全项

	case *parser.FunctionLiteral:
		// 函数字面量本身是表达式，它的处理在下面通过语句级别处理
		if e.Body != nil {
			for _, stmt := range e.Body.Statements {
				if uint32(e.Body.Token.Line-1) <= beforeLine {
					cp.collectSymbolsFromStatement(stmt, symbols, beforeLine)
				}
			}
		}

	case *parser.CallExpression:
		cp.collectSymbolsFromExpression(e.Function, symbols, beforeLine)
		for _, arg := range e.Arguments {
			cp.collectSymbolsFromExpression(arg, symbols, beforeLine)
		}

	case *parser.DotExpression:
		cp.collectSymbolsFromExpression(e.Receiver, symbols, beforeLine)

	case *parser.InfixExpression:
		cp.collectSymbolsFromExpression(e.Left, symbols, beforeLine)
		cp.collectSymbolsFromExpression(e.Right, symbols, beforeLine)

	case *parser.PrefixExpression:
		cp.collectSymbolsFromExpression(e.Right, symbols, beforeLine)

	case *parser.GroupedExpression:
		cp.collectSymbolsFromExpression(e.Expression, symbols, beforeLine)

	case *parser.IfExpression:
		cp.collectSymbolsFromExpression(e.Condition, symbols, beforeLine)
		if e.Consequence != nil {
			for _, stmt := range e.Consequence.Statements {
				if uint32(e.Consequence.Token.Line-1) <= beforeLine {
					cp.collectSymbolsFromStatement(stmt, symbols, beforeLine)
				}
			}
		}
		if e.Alternative != nil {
			for _, stmt := range e.Alternative.Statements {
				if uint32(e.Alternative.Token.Line-1) <= beforeLine {
					cp.collectSymbolsFromStatement(stmt, symbols, beforeLine)
				}
			}
		}
	}
}

func (cp *CompletionProvider) getTypeDetail(expr parser.Expression) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return "int"
	case *parser.FloatLiteral:
		return "float"
	case *parser.StringLiteral:
		return "string"
	case *parser.CharLiteral:
		return "char"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.NilLiteral:
		return "nil"
	case *parser.Identifier:
		return "unknown"
	case *parser.FunctionLiteral:
		paramCount := len(e.Parameters)
		if paramCount == 0 {
			return "function()"
		}
		return fmt.Sprintf("function(%d params)", paramCount)
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			return "call: " + ident.Value
		}
		return "call expression"
	default:
		return "unknown"
	}
}

func (cp *CompletionProvider) getCompletionsAfterDot(position Position) []CompletionItem {
	var items []CompletionItem

	receiverType := cp.getReceiverTypeAtPosition(position)
	receiverStr := cp.getReceiverStringAtPosition(position)

	if receiverStr == "" {
		return items
	}

	builtins := map[string][]SymbolInfo{
		"console": {
			{Name: "log", Kind: CompletionItemKindMethod, Detail: "console.log()"},
			{Name: "error", Kind: CompletionItemKindMethod, Detail: "console.error()"},
			{Name: "warn", Kind: CompletionItemKindMethod, Detail: "console.warn()"},
		},
		"fmt": {
			{Name: "Println", Kind: CompletionItemKindFunction, Detail: "fmt.Println()"},
			{Name: "Print", Kind: CompletionItemKindFunction, Detail: "fmt.Print()"},
			{Name: "Printf", Kind: CompletionItemKindFunction, Detail: "fmt.Printf()"},
			{Name: "Sprintf", Kind: CompletionItemKindFunction, Detail: "fmt.Sprintf()"},
		},
		"math": {
			{Name: "PI", Kind: CompletionItemKindConstant, Detail: "math.PI"},
			{Name: "E", Kind: CompletionItemKindConstant, Detail: "math.E"},
			{Name: "Abs", Kind: CompletionItemKindFunction, Detail: "math.Abs()"},
			{Name: "Max", Kind: CompletionItemKindFunction, Detail: "math.Max()"},
			{Name: "Min", Kind: CompletionItemKindFunction, Detail: "math.Min()"},
		},
		"string": {
			{Name: "Length", Kind: CompletionItemKindProperty, Detail: "string.length"},
			{Name: "ToUpper", Kind: CompletionItemKindMethod, Detail: "string.ToUpper()"},
			{Name: "ToLower", Kind: CompletionItemKindMethod, Detail: "string.ToLower()"},
			{Name: "Trim", Kind: CompletionItemKindMethod, Detail: "string.Trim()"},
		},
		"array": {
			{Name: "Length", Kind: CompletionItemKindProperty, Detail: "array.length"},
			{Name: "Push", Kind: CompletionItemKindMethod, Detail: "array.Push()"},
			{Name: "Pop", Kind: CompletionItemKindMethod, Detail: "array.Pop()"},
			{Name: "Map", Kind: CompletionItemKindMethod, Detail: "array.Map()"},
			{Name: "Filter", Kind: CompletionItemKindMethod, Detail: "array.Filter()"},
		},
	}

	if symbols, ok := builtins[receiverStr]; ok {
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

	if receiverType != "" {
		methods := cp.getMethodsForType(receiverType)
		for _, method := range methods {
			items = append(items, method)
		}
	}

	return items
}

func (cp *CompletionProvider) getReceiverTypeAtPosition(position Position) string {
	return ""
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

func (cp *CompletionProvider) getMethodsForType(typeName string) []CompletionItem {
	return nil
}

func (cp *CompletionProvider) getCompletionsAfterColon(position Position) []CompletionItem {
	var items []CompletionItem

	items = append(items, CompletionItem{
		Label:  "i8",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "int8 type",
	}, CompletionItem{
		Label:  "i16",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "int16 type",
	}, CompletionItem{
		Label:  "i32",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "int32 type",
	}, CompletionItem{
		Label:  "i64",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "int64 type",
	}, CompletionItem{
		Label:  "u8",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "uint8 type",
	}, CompletionItem{
		Label:  "u16",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "uint16 type",
	}, CompletionItem{
		Label:  "u32",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "uint32 type",
	}, CompletionItem{
		Label:  "u64",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "uint64 type",
	}, CompletionItem{
		Label:  "f32",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "float32 type",
	}, CompletionItem{
		Label:  "f64",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "float64 type",
	}, CompletionItem{
		Label:  "byte",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "byte type",
	}, CompletionItem{
		Label:  "char",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "char type",
	}, CompletionItem{
		Label:  "str",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "string type",
	}, CompletionItem{
		Label:  "bool",
		Kind:   CompletionItemKindTypeParameter,
		Detail: "boolean type",
	})

	return items
}

func (cp *CompletionProvider) getCompletionsAfterEquals(position Position) []CompletionItem {
	var items []CompletionItem

	items = append(items, CompletionItem{
		Label:      "true",
		Kind:       CompletionItemKindKeyword,
		Detail:     "boolean true",
		InsertText: "true",
	}, CompletionItem{
		Label:      "false",
		Kind:       CompletionItemKindKeyword,
		Detail:     "boolean false",
		InsertText: "false",
	}, CompletionItem{
		Label:      "nil",
		Kind:       CompletionItemKindKeyword,
		Detail:     "null value",
		InsertText: "nil",
	})

	symbols := cp.collectSymbols(position)
	for _, sym := range symbols {
		if sym.Kind == CompletionItemKindVariable || sym.Kind == CompletionItemKindConstant {
			item := CompletionItem{
				Label:  sym.Name,
				Kind:   sym.Kind,
				Detail: sym.Detail,
			}
			items = append(items, item)
		}
	}

	return items
}

func (cp *CompletionProvider) GetFunctionDeclarations() []CompletionItem {
	var items []CompletionItem

	if cp.program == nil {
		return items
	}

	for _, stmt := range cp.program.Statements {
		if letStmt, ok := stmt.(*parser.LetStatement); ok {
			if letStmt.Name != nil && letStmt.Value != nil {
				if _, ok := letStmt.Value.(*parser.FunctionLiteral); ok {
					line := uint32(0)
					if letStmt.Name.Token.Line > 0 {
						line = uint32(letStmt.Name.Token.Line - 1)
					}
					item := CompletionItem{
						Label:  letStmt.Name.Value,
						Kind:   CompletionItemKindFunction,
						Detail: cp.getTypeDetail(letStmt.Value),
						TextEdit: &TextEdit{
							Range: Range{
								Start: Position{Line: line, Character: uint32(letStmt.Name.Token.Column - 1)},
								End:   Position{Line: line, Character: uint32(letStmt.Name.Token.Column - 1 + len(letStmt.Name.Value))},
							},
							NewText: letStmt.Name.Value,
						},
					}
					items = append(items, item)
				}
			}
		}
	}

	return items
}

func (cp *CompletionProvider) ResolveCompletionItem(item CompletionItem) CompletionItem {
	if item.Kind == CompletionItemKindFunction {
		item.Documentation = "Function defined in the current scope"
	} else if item.Kind == CompletionItemKindVariable {
		item.Documentation = "Variable defined in the current scope"
	} else if item.Kind == CompletionItemKindKeyword {
		item.Documentation = "Nolang keyword"
	}

	return item
}
