package lsp

import (
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// LSP Semantic Token types (standard legend)
const (
	SemTokenTypeNamespace  = 0
	SemTokenTypeType       = 1
	SemTokenTypeClass      = 2
	SemTokenTypeEnum       = 3
	SemTokenTypeInterface  = 4
	SemTokenTypeStruct     = 5
	SemTokenTypeTypeParam  = 6
	SemTokenTypeParameter  = 7
	SemTokenTypeVariable   = 8
	SemTokenTypeProperty   = 9
	SemTokenTypeEnumMember = 10
	SemTokenTypeEvent      = 11
	SemTokenTypeFunction   = 12
	SemTokenTypeMethod     = 13
	SemTokenTypeMacro      = 14
	SemTokenTypeKeyword    = 15
	SemTokenTypeModifier   = 16
	SemTokenTypeComment    = 17
	SemTokenTypeString     = 18
	SemTokenTypeNumber     = 19
	SemTokenTypeRegexp     = 20
	SemTokenTypeOperator   = 21
)

// LSP Semantic Token modifiers
const (
	SemTokenModDeclaration    = 1 << 0
	SemTokenModDefinition     = 1 << 1
	SemTokenModReadonly       = 1 << 2
	SemTokenModStatic         = 1 << 3
	SemTokenModDeprecated     = 1 << 4
	SemTokenModAbstract       = 1 << 5
	SemTokenModAsync          = 1 << 6
	SemTokenModModification   = 1 << 7
	SemTokenModDocumentation  = 1 << 8
	SemTokenModDefaultLibrary = 1 << 9
)

// SemanticTokensLegend defines the token types and modifiers used by the server.
type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// SemanticTokens contains the encoded semantic tokens data.
type SemanticTokens struct {
	ResultID string   `json:"resultId,omitempty"`
	Data     []uint32 `json:"data"`
}

// SemanticTokensParams represents the parameters for textDocument/semanticTokens/full.
type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SemanticTokensProvider implements the textDocument/semanticTokens/full feature.
// It lexes the document and maps tokens to LSP semantic token types.
type SemanticTokensProvider struct {
	doc *TextDocument
}

// NewSemanticTokensProvider creates a new SemanticTokensProvider.
func NewSemanticTokensProvider(doc *TextDocument) *SemanticTokensProvider {
	return &SemanticTokensProvider{
		doc: doc,
	}
}

// GetSemanticTokensLegend returns the legend describing token types and modifiers used.
func GetSemanticTokensLegend() SemanticTokensLegend {
	return SemanticTokensLegend{
		TokenTypes: []string{
			"namespace", "type", "class", "enum", "interface",
			"struct", "typeParameter", "parameter", "variable", "property",
			"enumMember", "event", "function", "method", "macro",
			"keyword", "modifier", "comment", "string", "number",
			"regexp", "operator",
		},
		TokenModifiers: []string{
			"declaration", "definition", "readonly", "static", "deprecated",
			"abstract", "async", "modification", "documentation", "defaultLibrary",
		},
	}
}

// GetSemanticTokens returns encoded semantic tokens using the AST for identifier classification.
func (sp *SemanticTokensProvider) GetSemanticTokens() *SemanticTokens {
	l := lexer.New(sp.doc.Text)

	// Build identifier type and modifier maps from AST (if available)
	identTypes, identMods := sp.buildIdentifierMaps()

	var data []uint32
	prevLine := uint32(0)
	prevChar := uint32(0)

	for {
		tok := l.NextToken()
		if tok.Type == lexer.EOF {
			break
		}
		if tok.Type == lexer.NEWLINE {
			continue
		}

		tokenType := sp.mapTokenType(tok, identTypes)
		tokenModifiers := sp.mapTokenModifiers(tok, identMods)

		if tokenType < 0 {
			continue
		}

		line := uint32(tok.Line - 1)
		col := uint32(tok.Column - 1)
		length := uint32(len(tok.Literal))

		var deltaLine, deltaChar uint32
		if line == prevLine {
			deltaLine = 0
			deltaChar = col - prevChar
		} else {
			deltaLine = line - prevLine
			deltaChar = col
		}

		data = append(data, deltaLine, deltaChar, length, uint32(tokenType), uint32(tokenModifiers))
		prevLine = line
		prevChar = col
	}

	if data == nil {
		data = []uint32{}
	}

	return &SemanticTokens{
		Data: data,
	}
}

// buildIdentifierMaps walks the AST and classifies identifiers by (line, column).
// Returns: typeMap (position → token type), modMap (position → modifier bits)
func (sp *SemanticTokensProvider) buildIdentifierMaps() (map[[2]int]int, map[[2]int]uint32) {
	typeMap := make(map[[2]int]int)
	modMap := make(map[[2]int]uint32)
	if sp.doc.AST == nil {
		return typeMap, modMap
	}
	for _, stmt := range sp.doc.AST.Statements {
		sp.walkStmt(stmt, typeMap, modMap)
	}
	return typeMap, modMap
}

func (sp *SemanticTokensProvider) walkStmt(stmt parser.Statement, typeMap map[[2]int]int, modMap map[[2]int]uint32) {
	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		// Function name
		pos := [2]int{s.Token.Line, s.Token.Column}
		typeMap[pos] = SemTokenTypeFunction
		modMap[pos] = SemTokenModDefinition
		// Input parameters
		for _, p := range s.Parameters {
			pp := [2]int{p.Token.Line, p.Token.Column}
			typeMap[pp] = SemTokenTypeParameter
			modMap[pp] = SemTokenModReadonly
		}
		// Result parameters
		for _, p := range s.Results {
			pp := [2]int{p.Token.Line, p.Token.Column}
			typeMap[pp] = SemTokenTypeParameter
			modMap[pp] = SemTokenModReadonly
		}
		// Walk body
		if s.Body != nil {
			sp.walkStmt(s.Body, typeMap, modMap)
		}
	case *parser.BlockStatement:
		for _, st := range s.Statements {
			sp.walkStmt(st, typeMap, modMap)
		}
	case *parser.LetStatement:
		if s.Name != nil {
			pos := [2]int{s.Name.Token.Line, s.Name.Token.Column}
			typeMap[pos] = SemTokenTypeVariable
			modMap[pos] = SemTokenModDefinition
		}
	case *parser.ForStatement:
		if s.Body != nil {
			sp.walkStmt(s.Body, typeMap, modMap)
		}
		if s.Condition != nil {
			sp.walkExpr(s.Condition, typeMap, modMap)
		}
		if s.IterRange != nil {
			sp.walkExpr(s.IterRange, typeMap, modMap)
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			sp.walkExpr(s.Expression, typeMap, modMap)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			sp.walkExpr(s.ReturnValue, typeMap, modMap)
		}
	}
}

func (sp *SemanticTokensProvider) walkExpr(expr parser.Expression, typeMap map[[2]int]int, modMap map[[2]int]uint32) {
	switch e := expr.(type) {
	case *parser.AssignExpression:
		if ident, ok := e.Left.(*parser.Identifier); ok {
			pos := [2]int{ident.Token.Line, ident.Token.Column}
			typeMap[pos] = SemTokenTypeVariable
			modMap[pos] = SemTokenModDefinition
		}
		if e.Value != nil {
			sp.walkExpr(e.Value, typeMap, modMap)
		}
	case *parser.CallExpression:
		for _, arg := range e.Arguments {
			sp.walkExpr(arg, typeMap, modMap)
		}
	case *parser.InfixExpression:
		sp.walkExpr(e.Left, typeMap, modMap)
		sp.walkExpr(e.Right, typeMap, modMap)
	case *parser.PrefixExpression:
		sp.walkExpr(e.Right, typeMap, modMap)
	case *parser.IndexExpression:
		sp.walkExpr(e.Left, typeMap, modMap)
		sp.walkExpr(e.Index, typeMap, modMap)
	case *parser.IfExpression:
		if e.Condition != nil {
			sp.walkExpr(e.Condition, typeMap, modMap)
		}
		if e.Consequence != nil {
			sp.walkStmt(e.Consequence, typeMap, modMap)
		}
		if e.Alternative != nil {
			sp.walkStmt(e.Alternative, typeMap, modMap)
		}
	}
}

func (sp *SemanticTokensProvider) mapTokenType(tok lexer.Token, identTypes map[[2]int]int) int {
	// Check identifier type map first
	if tok.Type == lexer.IDENT {
		// Known type names → class
		if isTypeName(tok.Literal) {
			return SemTokenTypeClass
		}
		if t, ok := identTypes[[2]int{tok.Line, tok.Column}]; ok {
			return t
		}
		// Unknown identifier — skip, let TextMate grammar handle it
		return -1
	}
	switch tok.Type {
	case lexer.INT, lexer.FLOAT, lexer.BYTE:
		return SemTokenTypeNumber
	case lexer.STRING:
		return SemTokenTypeString
	case lexer.TRUE, lexer.FALSE, lexer.NIL:
		return SemTokenTypeKeyword
	case lexer.IF, lexer.ELIF, lexer.ELSE, lexer.RETURN,
		lexer.FOR, lexer.IN, lexer.BREAK, lexer.CONTINUE,
		lexer.USE, lexer.AS, lexer.CHAN, lexer.GO, lexer.PTR,
		lexer.SELF, lexer.SUPER, lexer.IT, lexer.UNDERSCORE:
		return SemTokenTypeKeyword
	case lexer.COMMENT:
		return SemTokenTypeComment
	case lexer.ASSIGN, lexer.ADD, lexer.SUB, lexer.MUL, lexer.QUO, lexer.MOD,
		lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.LESS_EQUALS,
		lexer.GREATER, lexer.GREATER_EQUALS, lexer.LAND, lexer.LOR,
		lexer.INC, lexer.DEC, lexer.NOT, lexer.XOR, lexer.SHL, lexer.SHR,
		lexer.ARROW, lexer.AND, lexer.OR, lexer.AND_NOT,
		lexer.ADD_ASSIGN, lexer.SUB_ASSIGN, lexer.MUL_ASSIGN,
		lexer.QUO_ASSIGN, lexer.MOD_ASSIGN, lexer.AND_ASSIGN,
		lexer.OR_ASSIGN, lexer.XOR_ASSIGN, lexer.SHL_ASSIGN,
		lexer.SHR_ASSIGN, lexer.AND_NOT_ASSIGN:
		return SemTokenTypeOperator
	default:
		return -1
	}
}

func (sp *SemanticTokensProvider) mapTokenModifiers(tok lexer.Token, identMods map[[2]int]uint32) uint32 {
	switch {
	case tok.Type == lexer.COMMENT:
		return SemTokenModDocumentation
	case tok.Type == lexer.IDENT && isTypeName(tok.Literal):
		return SemTokenModDefaultLibrary
	case tok.Type == lexer.IDENT:
		if m, ok := identMods[[2]int{tok.Line, tok.Column}]; ok {
			return m
		}
		return 0
	default:
		return 0
	}
}

// isTypeName returns true if the literal is a known Nolang built-in type.
func isTypeName(s string) bool {
	switch s {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"f32", "f64", "bool", "str", "byte", "char", "ptr", "err", "bigint",
		"void":
		return true
	}
	return false
}
