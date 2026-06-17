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
	word := getWordAtPosition(dp.doc.Text, position)
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

	return locationFromIdentifier(dp.doc.Item.URI, def), true
}

func (dp *DefinitionProvider) findDefinition(ident *parser.Identifier, beforeLine uint32) *parser.Identifier {
	if dp.program == nil {
		return nil
	}

	candidateScope := newScope()
	collectDefinitions(dp.program.Statements, candidateScope, beforeLine)

	if def, ok := candidateScope.lookup(ident.Value); ok {
		return def
	}

	return nil
}
