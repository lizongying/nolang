package lsp

import (
	"fmt"

	"github.com/lizongying/nolang/parser"
)

type ReferencesProvider struct {
	program *parser.Program
	doc     *TextDocument
}

func NewReferencesProvider(doc *TextDocument, program *parser.Program) *ReferencesProvider {
	return &ReferencesProvider{
		program: program,
		doc:     doc,
	}
}

func (rp *ReferencesProvider) GetReferences(position Position, includeDeclaration bool) []Location {
	word := rp.getWordAtPosition(position)
	if word == "" {
		return []Location{}
	}

	definitionIdent := rp.findDefinition(word, position.Line)
	if definitionIdent == nil {
		return []Location{}
	}

	var locations []Location
	seen := make(map[string]bool)

	rp.collectReferences(rp.program.Statements, word, seen, &locations)

	if includeDeclaration {
		loc := rp.locationFromIdentifier(definitionIdent)
		key := rp.locationKey(loc)
		if _, exists := seen[key]; !exists {
			seen[key] = true
			locations = append(locations, loc)
		}
	}

	return locations
}

func (rp *ReferencesProvider) collectReferences(statements []parser.Statement, word string, seen map[string]bool, locations *[]Location) {
	for _, stmt := range statements {
		rp.collectReferencesFromStatement(stmt, word, seen, locations)
	}
}

func (rp *ReferencesProvider) collectReferencesFromStatement(stmt parser.Statement, word string, seen map[string]bool, locations *[]Location) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name != nil && s.Name.Value == word {
			loc := rp.locationFromIdentifier(s.Name)
			if _, exists := seen[rp.locationKey(loc)]; !exists {
				seen[rp.locationKey(loc)] = true
				*locations = append(*locations, loc)
			}
		}
		if s.Value != nil {
			rp.collectReferencesFromExpression(s.Value, word, seen, locations)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			rp.collectReferencesFromExpression(s.Expression, word, seen, locations)
		}

	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			rp.collectReferencesFromExpression(s.ReturnValue, word, seen, locations)
		}

	case *parser.BlockStatement:
		for _, innerStmt := range s.Statements {
			rp.collectReferencesFromStatement(innerStmt, word, seen, locations)
		}
	}
}

func (rp *ReferencesProvider) collectReferencesFromExpression(expr parser.Expression, word string, seen map[string]bool, locations *[]Location) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *parser.Identifier:
		if e.Value == word {
			loc := rp.locationFromIdentifier(e)
			if _, exists := seen[rp.locationKey(loc)]; !exists {
				seen[rp.locationKey(loc)] = true
				*locations = append(*locations, loc)
			}
		}

	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, param := range e.Parameters {
				if param.Name == word {
					paramIdent := &parser.Identifier{Token: param.Token, Value: param.Name}
					loc := rp.locationFromIdentifier(paramIdent)
					if _, exists := seen[rp.locationKey(loc)]; !exists {
						seen[rp.locationKey(loc)] = true
						*locations = append(*locations, loc)
					}
				}
			}

			for _, innerStmt := range e.Body.Statements {
				rp.collectReferencesFromStatement(innerStmt, word, seen, locations)
			}
		}

	case *parser.CallExpression:
		if e.Function != nil {
			rp.collectReferencesFromExpression(e.Function, word, seen, locations)
		}
		for _, arg := range e.Arguments {
			rp.collectReferencesFromExpression(arg, word, seen, locations)
		}

	case *parser.DotExpression:
		rp.collectReferencesFromExpression(e.Receiver, word, seen, locations)

	case *parser.InfixExpression:
		rp.collectReferencesFromExpression(e.Left, word, seen, locations)
		rp.collectReferencesFromExpression(e.Right, word, seen, locations)

	case *parser.PrefixExpression:
		rp.collectReferencesFromExpression(e.Right, word, seen, locations)

	case *parser.IfExpression:
		rp.collectReferencesFromExpression(e.Condition, word, seen, locations)
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				rp.collectReferencesFromStatement(innerStmt, word, seen, locations)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				rp.collectReferencesFromStatement(innerStmt, word, seen, locations)
			}
		}
	}
}

func (rp *ReferencesProvider) findDefinition(name string, beforeLine uint32) *parser.Identifier {
	if rp.program == nil {
		return nil
	}

	candidateScope := newScope()
	rp.collectDefinitions(rp.program.Statements, candidateScope, beforeLine)

	if def, ok := candidateScope.lookup(name); ok {
		return def
	}

	return nil
}

func (rp *ReferencesProvider) collectDefinitions(statements []parser.Statement, scope *scope, beforeLine uint32) {
	for _, stmt := range statements {
		rp.collectDefinitionsFromStatement(stmt, scope, beforeLine)
	}
}

func (rp *ReferencesProvider) collectDefinitionsFromStatement(stmt parser.Statement, scope *scope, beforeLine uint32) {
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
			rp.collectDefinitionsFromExpression(s.Value, scope, beforeLine)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			rp.collectDefinitionsFromExpression(s.Expression, scope, beforeLine)
		}

	case *parser.BlockStatement:
		blockScope := newScope()
		blockScope.parent = scope
		for _, innerStmt := range s.Statements {
			rp.collectDefinitionsFromStatement(innerStmt, blockScope, beforeLine)
		}
	}
}

func (rp *ReferencesProvider) collectDefinitionsFromExpression(expr parser.Expression, scope *scope, beforeLine uint32) {
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
				rp.collectDefinitionsFromStatement(innerStmt, funcScope, beforeLine)
			}
		}

	case *parser.CallExpression:
		rp.collectDefinitionsFromExpression(e.Function, scope, beforeLine)
		for _, arg := range e.Arguments {
			rp.collectDefinitionsFromExpression(arg, scope, beforeLine)
		}

	case *parser.DotExpression:
		rp.collectDefinitionsFromExpression(e.Receiver, scope, beforeLine)

	case *parser.InfixExpression:
		rp.collectDefinitionsFromExpression(e.Left, scope, beforeLine)
		rp.collectDefinitionsFromExpression(e.Right, scope, beforeLine)

	case *parser.PrefixExpression:
		rp.collectDefinitionsFromExpression(e.Right, scope, beforeLine)

	case *parser.IfExpression:
		rp.collectDefinitionsFromExpression(e.Condition, scope, beforeLine)
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				rp.collectDefinitionsFromStatement(innerStmt, scope, beforeLine)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				rp.collectDefinitionsFromStatement(innerStmt, scope, beforeLine)
			}
		}
	}
}

func (rp *ReferencesProvider) getWordAtPosition(position Position) string {
	return getWordAtPosition(rp.doc.Text, position)
}

func (rp *ReferencesProvider) locationFromIdentifier(ident *parser.Identifier) Location {
	if ident == nil {
		return Location{}
	}

	return Location{
		URI: rp.doc.Item.URI,
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

func (rp *ReferencesProvider) locationKey(loc Location) string {
	return fmt.Sprintf("%s:%d:%d", loc.URI, loc.Range.Start.Line, loc.Range.Start.Character)
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

func NewReferenceParams(textDocument TextDocumentIdentifier, position Position, includeDeclaration bool) ReferenceParams {
	return ReferenceParams{
		TextDocument: textDocument,
		Position:     position,
		Context: ReferenceContext{
			IncludeDeclaration: includeDeclaration,
		},
	}
}
