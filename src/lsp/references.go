package lsp

import (
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
	word := getWordAtPosition(rp.doc.Text, position)
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
		loc := locationFromIdentifier(rp.doc.Item.URI, definitionIdent)
		key := locationKey(loc)
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
			loc := locationFromIdentifier(rp.doc.Item.URI, s.Name)
			if _, exists := seen[locationKey(loc)]; !exists {
				seen[locationKey(loc)] = true
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
			loc := locationFromIdentifier(rp.doc.Item.URI, e)
			if _, exists := seen[locationKey(loc)]; !exists {
				seen[locationKey(loc)] = true
				*locations = append(*locations, loc)
			}
		}

	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, param := range e.Parameters {
				if param.Name == word {
					paramIdent := &parser.Identifier{Token: param.Token, Value: param.Name}
					loc := locationFromIdentifier(rp.doc.Item.URI, paramIdent)
					if _, exists := seen[locationKey(loc)]; !exists {
						seen[locationKey(loc)] = true
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
	collectDefinitions(rp.program.Statements, candidateScope, beforeLine)

	if def, ok := candidateScope.lookup(name); ok {
		return def
	}

	return nil
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
