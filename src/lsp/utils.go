package lsp

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
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

func getProgram(doc *TextDocument) *parser.Program {
	if doc.AST != nil {
		return doc.AST
	}
	l := lexer.New(doc.Text)
	p := parser.New(l)
	return p.ParseProgram()
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

func collectDefinitions(statements []parser.Statement, scope *scope, beforeLine uint32) {
	for _, stmt := range statements {
		collectDefinitionsFromStatement(stmt, scope, beforeLine)
	}
}

func collectDefinitionsFromStatement(stmt parser.Statement, scope *scope, beforeLine uint32) {
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
			collectDefinitionsFromExpression(s.Value, scope, beforeLine)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			collectDefinitionsFromExpression(s.Expression, scope, beforeLine)
		}

	case *parser.BlockStatement:
		blockScope := newScope()
		blockScope.parent = scope
		for _, innerStmt := range s.Statements {
			collectDefinitionsFromStatement(innerStmt, blockScope, beforeLine)
		}
	}
}

func collectDefinitionsFromExpression(expr parser.Expression, scope *scope, beforeLine uint32) {
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
				collectDefinitionsFromStatement(innerStmt, funcScope, beforeLine)
			}
		}

	case *parser.CallExpression:
		collectDefinitionsFromExpression(e.Function, scope, beforeLine)
		for _, arg := range e.Arguments {
			collectDefinitionsFromExpression(arg, scope, beforeLine)
		}

	case *parser.DotExpression:
		collectDefinitionsFromExpression(e.Receiver, scope, beforeLine)

	case *parser.InfixExpression:
		collectDefinitionsFromExpression(e.Left, scope, beforeLine)
		collectDefinitionsFromExpression(e.Right, scope, beforeLine)

	case *parser.PrefixExpression:
		collectDefinitionsFromExpression(e.Right, scope, beforeLine)

	case *parser.IfExpression:
		collectDefinitionsFromExpression(e.Condition, scope, beforeLine)
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				collectDefinitionsFromStatement(innerStmt, scope, beforeLine)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				collectDefinitionsFromStatement(innerStmt, scope, beforeLine)
			}
		}
	}
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
	return fmt.Sprintf("%s:%d:%d", loc.URI, loc.Range.Start.Line, loc.Range.Start.Character)
}
