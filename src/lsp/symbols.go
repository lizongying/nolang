package lsp

import (
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

const (
	SymbolKindFile          = 1
	SymbolKindModule        = 2
	SymbolKindNamespace     = 3
	SymbolKindPackage       = 4
	SymbolKindClass         = 5
	SymbolKindMethod        = 6
	SymbolKindProperty      = 7
	SymbolKindField         = 8
	SymbolKindConstructor   = 9
	SymbolKindEnum          = 10
	SymbolKindInterface     = 11
	SymbolKindFunction      = 12
	SymbolKindVariable      = 13
	SymbolKindConstant      = 14
	SymbolKindString        = 15
	SymbolKindNumber        = 16
	SymbolKindBoolean       = 17
	SymbolKindArray         = 18
	SymbolKindObject        = 19
	SymbolKindKey           = 20
	SymbolKindNull          = 21
	SymbolKindEnumMember    = 22
	SymbolKindStruct        = 23
	SymbolKindEvent         = 24
	SymbolKindOperator      = 25
	SymbolKindTypeParameter = 26
	SymbolKindParameter     = 27
)

type SymbolProvider struct {
	program *parser.Program
	doc     *TextDocument
}

func NewSymbolProvider(doc *TextDocument, program *parser.Program) *SymbolProvider {
	return &SymbolProvider{
		program: program,
		doc:     doc,
	}
}

func (sp *SymbolProvider) GetSymbols() []DocumentSymbol {
	var symbols []DocumentSymbol
	if sp.program == nil {
		return symbols
	}

	for _, stmt := range sp.program.Statements {
		sp.collectFromStatement(stmt, "", &symbols)
	}

	return symbols
}

func (sp *SymbolProvider) collectFromStatement(stmt parser.Statement, containerName string, symbols *[]DocumentSymbol) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		symbol := DocumentSymbol{
			Name:           s.Name,
			Kind:           SymbolKindFunction,
			SelectionRange: sp.rangeFromToken(s.Token),
			Range:          sp.rangeFromToken(s.Token),
			Children:       []DocumentSymbol{},
		}
		for _, param := range s.Parameters {
			paramSymbol := DocumentSymbol{
				Name:           param.Name,
				Kind:           SymbolKindParameter,
				SelectionRange: sp.rangeFromToken(param.Token),
				Range:          sp.rangeFromToken(param.Token),
				Children:       []DocumentSymbol{},
			}
			symbol.Children = append(symbol.Children, paramSymbol)
		}
		if s.Body != nil {
			for _, stmt := range s.Body.Statements {
				sp.collectLocalVariables(stmt, s.Name, &symbol.Children)
			}
		}
		*symbols = append(*symbols, symbol)

	case *parser.LetStatement:
		if s.Name != nil {
			if funcLit, ok := s.Value.(*parser.FunctionLiteral); ok {
				symbol := sp.createFunctionSymbol(s.Name.Value, funcLit, containerName)
				*symbols = append(*symbols, symbol)
			} else {
				symbol := sp.createVariableSymbol(s.Name, containerName)
				*symbols = append(*symbols, symbol)
			}
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			sp.collectFromExpression(s.Expression, containerName, symbols)
		}

	case *parser.InterfaceDefinition:
		symbol := DocumentSymbol{
			Name:           s.Name,
			Kind:           SymbolKindInterface,
			SelectionRange: sp.rangeFromToken(s.Token),
			Range:          sp.rangeFromToken(s.Token),
			Children:       []DocumentSymbol{},
		}
		for _, method := range s.Methods {
			methodSymbol := DocumentSymbol{
				Name:           method.Name,
				Kind:           SymbolKindMethod,
				SelectionRange: sp.rangeFromToken(method.Token),
				Range:          sp.rangeFromToken(method.Token),
				Children:       []DocumentSymbol{},
			}
			for _, param := range method.Parameters {
				paramSymbol := DocumentSymbol{
					Name:           param.Name,
					Kind:           SymbolKindParameter,
					SelectionRange: sp.rangeFromToken(param.Token),
					Range:          sp.rangeFromToken(param.Token),
					Children:       []DocumentSymbol{},
				}
				methodSymbol.Children = append(methodSymbol.Children, paramSymbol)
			}
			symbol.Children = append(symbol.Children, methodSymbol)
		}
		*symbols = append(*symbols, symbol)

	case *parser.BlockStatement:
		for _, innerStmt := range s.Statements {
			sp.collectFromStatement(innerStmt, containerName, symbols)
		}
	}
}

func (sp *SymbolProvider) collectFromExpression(expr parser.Expression, containerName string, symbols *[]DocumentSymbol) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *parser.FunctionLiteral:
		symbol := sp.createAnonymousFunctionSymbol(e, containerName)
		*symbols = append(*symbols, symbol)

	case *parser.CallExpression:
		sp.collectFromExpression(e.Function, containerName, symbols)
		for _, arg := range e.Arguments {
			sp.collectFromExpression(arg, containerName, symbols)
		}

	case *parser.DotExpression:
		sp.collectFromExpression(e.Receiver, containerName, symbols)

	case *parser.InfixExpression:
		sp.collectFromExpression(e.Left, containerName, symbols)
		sp.collectFromExpression(e.Right, containerName, symbols)

	case *parser.PrefixExpression:
		sp.collectFromExpression(e.Right, containerName, symbols)

	case *parser.GroupedExpression:
		sp.collectFromExpression(e.Expression, containerName, symbols)

	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, innerStmt := range e.Consequence.Statements {
				sp.collectFromStatement(innerStmt, containerName, symbols)
			}
		}
		if e.Alternative != nil {
			for _, innerStmt := range e.Alternative.Statements {
				sp.collectFromStatement(innerStmt, containerName, symbols)
			}
		}
	}
}

func (sp *SymbolProvider) createFunctionSymbol(name string, funcLit *parser.FunctionLiteral, containerName string) DocumentSymbol {
	selectionRange := sp.rangeFromToken(funcLit.Token)

	var children []DocumentSymbol
	for _, param := range funcLit.Parameters {
		paramSymbol := DocumentSymbol{
			Name:           param.Name,
			Kind:           SymbolKindParameter,
			SelectionRange: sp.rangeFromToken(param.Token),
			Range:          sp.rangeFromToken(param.Token),
			Children:       []DocumentSymbol{},
		}
		children = append(children, paramSymbol)
	}

	if funcLit.Body != nil {
		for _, stmt := range funcLit.Body.Statements {
			sp.collectLocalVariables(stmt, name, &children)
		}
	}

	return DocumentSymbol{
		Name:           name,
		Kind:           SymbolKindFunction,
		SelectionRange: selectionRange,
		Range:          selectionRange,
		Children:       children,
	}
}

func (sp *SymbolProvider) createAnonymousFunctionSymbol(funcLit *parser.FunctionLiteral, containerName string) DocumentSymbol {
	selectionRange := sp.rangeFromToken(funcLit.Token)

	var children []DocumentSymbol
	for _, param := range funcLit.Parameters {
		paramSymbol := DocumentSymbol{
			Name:           param.Name,
			Kind:           SymbolKindParameter,
			SelectionRange: sp.rangeFromToken(param.Token),
			Range:          sp.rangeFromToken(param.Token),
			Children:       []DocumentSymbol{},
		}
		children = append(children, paramSymbol)
	}

	if funcLit.Body != nil {
		for _, stmt := range funcLit.Body.Statements {
			sp.collectLocalVariables(stmt, "", &children)
		}
	}

	return DocumentSymbol{
		Name:           "anonymous",
		Kind:           SymbolKindFunction,
		SelectionRange: selectionRange,
		Range:          selectionRange,
		Children:       children,
	}
}

func (sp *SymbolProvider) createVariableSymbol(ident *parser.Identifier, containerName string) DocumentSymbol {
	return DocumentSymbol{
		Name:           ident.Value,
		Kind:           SymbolKindVariable,
		SelectionRange: sp.rangeFromToken(ident.Token),
		Range:          sp.rangeFromToken(ident.Token),
		Children:       []DocumentSymbol{},
	}
}

func (sp *SymbolProvider) collectLocalVariables(stmt parser.Statement, containerName string, symbols *[]DocumentSymbol) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Name != nil {
			symbol := DocumentSymbol{
				Name:           s.Name.Value,
				Kind:           SymbolKindVariable,
				SelectionRange: sp.rangeFromToken(s.Name.Token),
				Range:          sp.rangeFromToken(s.Name.Token),
				Children:       []DocumentSymbol{},
			}
			*symbols = append(*symbols, symbol)
		}
		if s.Value != nil {
			sp.collectFromExpression(s.Value, containerName, symbols)
		}

	case *parser.ExpressionStatement:
		sp.collectFromExpression(s.Expression, containerName, symbols)

	case *parser.BlockStatement:
		for _, innerStmt := range s.Statements {
			sp.collectLocalVariables(innerStmt, containerName, symbols)
		}
	}
}

func (sp *SymbolProvider) rangeFromToken(token lexer.Token) Range {
	return Range{
		Start: Position{
			Line:      uint32(token.Line - 1),
			Character: uint32(token.Column - 1),
		},
		End: Position{
			Line:      uint32(token.Line - 1),
			Character: uint32(token.Column - 1 + len(token.Literal)),
		},
	}
}
