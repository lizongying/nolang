package lsp

import (
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

type DefinitionProvider struct {
	program *parser.Program
	doc     *TextDocument
}

type DefinitionResult struct {
	Name     string
	Location Location
}

func NewDefinitionProvider(doc *TextDocument, program *parser.Program) *DefinitionProvider {
	return &DefinitionProvider{
		program: program,
		doc:     doc,
	}
}

func (dp *DefinitionProvider) GetDefinition(position Position) (Location, bool) {
	word := dp.getWordAtPosition(position)
	if word == "" {
		return Location{}, false
	}

	ident := &parser.Identifier{
		Token: lexer.Token{Literal: word},
		Value: word,
	}

	def := dp.findDefinition(ident, position.Line)
	if def == nil {
		return Location{}, false
	}

	return dp.locationFromIdentifier(def), true
}

func (dp *DefinitionProvider) getWordAtPosition(position Position) string {
	return getWordAtPosition(dp.doc.Text, position)
}

func (dp *DefinitionProvider) findDefinition(ident *parser.Identifier, beforeLine uint32) *parser.Identifier {
	if dp.program == nil {
		return nil
	}

	candidateScope := newScope()
	dp.collectDefinitions(dp.program.Statements, candidateScope, beforeLine)

	if def, ok := candidateScope.lookup(ident.Value); ok {
		return def
	}

	return nil
}

func (dp *DefinitionProvider) collectDefinitions(statements []parser.Statement, scope *scope, beforeLine uint32) {
	for _, stmt := range statements {
		dp.collectDefinitionsFromStatement(stmt, scope, beforeLine)
	}
}

func (dp *DefinitionProvider) collectDefinitionsFromStatement(stmt parser.Statement, scope *scope, beforeLine uint32) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name != nil {
			line := uint32(0)
			if s.Name.Token.Line > 0 {
				line = uint32(s.Name.Token.Line - 1)
			}
			if line <= beforeLine {
				scope.define(s.Name.Value, s.Name)
			}
		}

		if s.Value != nil {
			dp.collectDefinitionsFromExpression(s.Value, scope, beforeLine)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			dp.collectDefinitionsFromExpression(s.Expression, scope, beforeLine)
		}

	case *parser.BlockStatement:
		blockScope := newScope()
		blockScope.parent = scope
		for _, innerStmt := range s.Statements {
			dp.collectDefinitionsFromStatement(innerStmt, blockScope, beforeLine)
		}
	}
}

func (dp *DefinitionProvider) collectDefinitionsFromExpression(expr parser.Expression, scope *scope, beforeLine uint32) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *parser.Identifier:

	case *parser.FunctionLiteral:
		if e.Body != nil {
			funcScope := newScope()
			funcScope.parent = scope

			for _, param := range e.Parameters {
				paramIdent := &parser.Identifier{Token: param.Token, Value: param.Name}
				funcScope.define(param.Name, paramIdent)
			}

			for _, innerStmt := range e.Body.Statements {
				dp.collectDefinitionsFromStatement(innerStmt, funcScope, beforeLine)
			}
		}

	case *parser.CallExpression:
		dp.collectDefinitionsFromExpression(e.Function, scope, beforeLine)
		for _, arg := range e.Arguments {
			dp.collectDefinitionsFromExpression(arg, scope, beforeLine)
		}

	case *parser.DotExpression:
		dp.collectDefinitionsFromExpression(e.Receiver, scope, beforeLine)

	case *parser.InfixExpression:
		dp.collectDefinitionsFromExpression(e.Left, scope, beforeLine)
		dp.collectDefinitionsFromExpression(e.Right, scope, beforeLine)

	case *parser.PrefixExpression:
		dp.collectDefinitionsFromExpression(e.Right, scope, beforeLine)

	case *parser.IfExpression:
		dp.collectDefinitionsFromExpression(e.Condition, scope, beforeLine)
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				dp.collectDefinitionsFromStatement(innerStmt, scope, beforeLine)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				dp.collectDefinitionsFromStatement(innerStmt, scope, beforeLine)
			}
		}
	}
}

func (dp *DefinitionProvider) locationFromIdentifier(ident *parser.Identifier) Location {
	if ident == nil {
		return Location{}
	}

	return Location{
		URI: dp.doc.Item.URI,
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

type scope struct {
	parent  *scope
	symbols map[string]*parser.Identifier
}

func newScope() *scope {
	return &scope{
		symbols: make(map[string]*parser.Identifier),
	}
}

func (s *scope) define(name string, ident *parser.Identifier) {
	s.symbols[name] = ident
}

func (s *scope) lookup(name string) (*parser.Identifier, bool) {
	if ident, ok := s.symbols[name]; ok {
		return ident, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return nil, false
}
