package lsp

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

type HoverProvider struct {
	program *parser.Program
	doc     *TextDocument
}

type HoverResult struct {
	Name        string
	Type        string
	Declaration string
	Value       string
	SymbolKind  string
}

func NewHoverProvider(doc *TextDocument, program *parser.Program) *HoverProvider {
	return &HoverProvider{
		program: program,
		doc:     doc,
	}
}

func (hp *HoverProvider) GetHover(position Position) (*Hover, bool) {
	word := hp.getWordAtPosition(position)
	if word == "" {
		return nil, false
	}

	result := hp.findSymbol(word, position.Line)
	if result == nil {
		return nil, false
	}

	contents := hp.formatHoverContent(result)
	return &Hover{
		Contents: contents,
	}, true
}

func (hp *HoverProvider) getWordAtPosition(position Position) string {
	return getWordAtPosition(hp.doc.Text, position)
}

func (hp *HoverProvider) findSymbol(name string, beforeLine uint32) *HoverResult {
	if hp.program == nil {
		return nil
	}

	candidateScope := newHoverScope()
	hp.collectSymbols(hp.program.Statements, candidateScope, beforeLine)

	if result, ok := candidateScope.lookup(name); ok {
		return result
	}

	return nil
}

func (hp *HoverProvider) collectSymbols(statements []parser.Statement, scope *hoverScope, beforeLine uint32) {
	for _, stmt := range statements {
		hp.collectSymbolsFromStatement(stmt, scope, beforeLine)
	}
}

func (hp *HoverProvider) collectSymbolsFromStatement(stmt parser.Statement, scope *hoverScope, beforeLine uint32) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name != nil {
			line := uint32(0)
			if s.Name.Token.Line > 0 {
				line = uint32(s.Name.Token.Line - 1)
			}
			if line <= beforeLine {
				typeStr := hp.getExpressionType(s.Value)
				result := &HoverResult{
					Name:        s.Name.Value,
					Type:        typeStr,
					Declaration: fmt.Sprintf("line %d, column %d", s.Name.Token.Line, s.Name.Token.Column),
					SymbolKind:  "variable",
				}
				if s.Value != nil {
					result.Value = hp.getExpressionValue(s.Value)
				}
				scope.define(s.Name.Value, result)
			}
		}

		if s.Value != nil {
			hp.collectSymbolsFromExpression(s.Value, scope, beforeLine)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			hp.collectSymbolsFromExpression(s.Expression, scope, beforeLine)
		}

	case *parser.BlockStatement:
		blockScope := newHoverScope()
		blockScope.parent = scope
		for _, innerStmt := range s.Statements {
			hp.collectSymbolsFromStatement(innerStmt, blockScope, beforeLine)
		}
	}
}

func (hp *HoverProvider) collectSymbolsFromExpression(expr parser.Expression, scope *hoverScope, beforeLine uint32) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *parser.Identifier:
		// Identifier references - don't define, just reference

	case *parser.FunctionLiteral:
		if e.Body != nil {
			funcScope := newHoverScope()
			funcScope.parent = scope

			params := make([]string, len(e.Parameters))
			for i, param := range e.Parameters {
				params[i] = param.Name
				funcScope.define(param.Name, &HoverResult{
					Name:        param.Name,
					Type:        "parameter",
					Declaration: fmt.Sprintf("line %d, column %d", param.Token.Line, param.Token.Column),
					SymbolKind:  "parameter",
				})
			}

			result := &HoverResult{
				Name:        "fn",
				Type:        fmt.Sprintf("fn(%s)", strings.Join(params, ", ")),
				Declaration: fmt.Sprintf("line %d, column %d", e.Token.Line, e.Token.Column),
				SymbolKind:  "function",
			}

			for _, innerStmt := range e.Body.Statements {
				hp.collectSymbolsFromStatement(innerStmt, funcScope, beforeLine)
			}

			_ = result
		}

	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			result := &HoverResult{
				Name:        ident.Value,
				Type:        "function call",
				Declaration: fmt.Sprintf("line %d, column %d", ident.Token.Line, ident.Token.Column),
				SymbolKind:  "function",
			}
			scope.define(ident.Value, result)
		}

		hp.collectSymbolsFromExpression(e.Function, scope, beforeLine)
		for _, arg := range e.Arguments {
			hp.collectSymbolsFromExpression(arg, scope, beforeLine)
		}

	case *parser.DotExpression:
		hp.collectSymbolsFromExpression(e.Receiver, scope, beforeLine)

	case *parser.InfixExpression:
		hp.collectSymbolsFromExpression(e.Left, scope, beforeLine)
		hp.collectSymbolsFromExpression(e.Right, scope, beforeLine)

	case *parser.PrefixExpression:
		hp.collectSymbolsFromExpression(e.Right, scope, beforeLine)

	case *parser.IfExpression:
		hp.collectSymbolsFromExpression(e.Condition, scope, beforeLine)
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				hp.collectSymbolsFromStatement(innerStmt, scope, beforeLine)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				hp.collectSymbolsFromStatement(innerStmt, scope, beforeLine)
			}
		}
	}
}

func (hp *HoverProvider) getExpressionType(expr parser.Expression) string {
	if expr == nil {
		return "unknown"
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
		return "identifier"
	case *parser.FunctionLiteral:
		params := make([]string, len(e.Parameters))
		for i, p := range e.Parameters {
			params[i] = p.Name
		}
		return fmt.Sprintf("fn(%s)", strings.Join(params, ", "))
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			return fmt.Sprintf("call %s", ident.Value)
		}
		return "call"
	case *parser.InfixExpression:
		return fmt.Sprintf(" %s ", e.Operator)
	case *parser.PrefixExpression:
		return e.Operator
	case *parser.IfExpression:
		return "if"
	default:
		return "unknown"
	}
}

func (hp *HoverProvider) getExpressionValue(expr parser.Expression) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.FloatLiteral:
		return fmt.Sprintf("%f", e.Value)
	case *parser.StringLiteral:
		return e.Value
	case *parser.CharLiteral:
		return e.Value
	case *parser.BooleanLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *parser.NilLiteral:
		return "nil"
	case *parser.Identifier:
		return e.Value
	default:
		return ""
	}
}

func (hp *HoverProvider) formatHoverContent(result *HoverResult) interface{} {
	var builder strings.Builder

	if result.SymbolKind == "function" || result.SymbolKind == "variable" {
		builder.WriteString(fmt.Sprintf("**%s**\n\n", result.Name))
		builder.WriteString(fmt.Sprintf("- **Type**: `%s`\n", result.Type))
		if result.Declaration != "" {
			builder.WriteString(fmt.Sprintf("- **Declared at**: %s\n", result.Declaration))
		}
		if result.Value != "" {
			builder.WriteString(fmt.Sprintf("- **Value**: %s\n", result.Value))
		}
	} else {
		builder.WriteString(fmt.Sprintf("**%s**\n\n", result.Name))
		builder.WriteString(fmt.Sprintf("- **Type**: `%s`\n", result.Type))
		if result.Declaration != "" {
			builder.WriteString(fmt.Sprintf("- **At**: %s\n", result.Declaration))
		}
	}

	return MarkupContent{
		Kind:  MarkupKindMarkdown,
		Value: builder.String(),
	}
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type hoverScope struct {
	parent  *hoverScope
	symbols map[string]*HoverResult
}

func newHoverScope() *hoverScope {
	return &hoverScope{
		symbols: make(map[string]*HoverResult),
	}
}

func (s *hoverScope) define(name string, result *HoverResult) {
	s.symbols[name] = result
}

func (s *hoverScope) lookup(name string) (*HoverResult, bool) {
	if result, ok := s.symbols[name]; ok {
		return result, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return nil, false
}
