package lsp

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

type ASTWalker struct {
	index   *SymbolIndex
	doc     *TextDocument
	program *parser.Program
	uri     string
}

func NewASTWalker(index *SymbolIndex, doc *TextDocument, program *parser.Program) *ASTWalker {
	return &ASTWalker{
		index:   index,
		doc:     doc,
		program: program,
		uri:     doc.Item.URI,
	}
}

func (w *ASTWalker) Walk() {
	if w.program == nil {
		return
	}
	for _, stmt := range w.program.Statements {
		w.walkStatement(stmt, "")
	}
}

func (w *ASTWalker) walkStatement(stmt parser.Statement, scope string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		w.addFunction(s.Name, s.Token, s.Parameters, s.Results, s.Body, scope)
		if s.Body != nil {
			for _, inner := range s.Body.Statements {
				w.walkStatement(inner, s.Name)
			}
		}

	case *parser.LetStatement:
		if s.Name != nil {
			kind := SymbolKindVariable
			detail := ""
			value := ""
			var params []ParamInfo

			if funcLit, ok := s.Value.(*parser.FunctionLiteral); ok {
				kind = SymbolKindFunction
				detail = w.formatFuncLitDetail(funcLit)
				params = w.extractParams(funcLit.Parameters)
				value = detail
				entry := &IndexEntry{
					Name: s.Name.Value,
					Kind: kind,
					Type: detail,
					Location: Location{
						URI:   w.uri,
						Range: w.rangeFromIdent(s.Name),
					},
					Scope:  scope,
					Value:  value,
					Params: params,
				}
				w.index.functions[s.Name.Value] = entry
				w.index.definitions[s.Name.Value] = entry
				if funcLit.Body != nil {
					for _, inner := range funcLit.Body.Statements {
						w.walkStatement(inner, s.Name.Value)
					}
				}
			} else {
				detail = w.getExprType(s.Value)
				value = w.getExprValue(s.Value)
				entry := &IndexEntry{
					Name: s.Name.Value,
					Kind: kind,
					Type: detail,
					Location: Location{
						URI:   w.uri,
						Range: w.rangeFromIdent(s.Name),
					},
					Scope: scope,
					Value: value,
				}
				w.index.symbols[s.Name.Value] = entry
				w.index.definitions[s.Name.Value] = entry
			}
		}
		if s.Value != nil {
			w.walkExpression(s.Value, scope)
		}

	case *parser.MultiAssignStatement:
		// Resolve types from the function's result parameters
		var resultTypes []ParamInfo
		if callExpr, ok := s.Value.(*parser.CallExpression); ok {
			if ident, ok := callExpr.Function.(*parser.Identifier); ok {
				if entry, ok := w.index.functions[ident.Value]; ok && len(entry.ResultParams) > 0 {
					resultTypes = entry.ResultParams
				}
			}
		}
		// Register each multi-assign target variable as a symbol
		for i, name := range s.Names {
			exprType := ""
			if i < len(resultTypes) {
				exprType = resultTypes[i].Type
			}
			entry := &IndexEntry{
				Name: name.Value,
				Kind: SymbolKindVariable,
				Type: exprType,
				Location: Location{
					URI:   w.uri,
					Range: w.rangeFromIdent(name),
				},
				Scope: scope,
			}
			w.index.symbols[name.Value] = entry
			w.index.definitions[name.Value] = entry
		}
		if s.Value != nil {
			w.walkExpression(s.Value, scope)
		}

	case *parser.ExpressionStatement:
		if s.Expression != nil {
			w.walkExpression(s.Expression, scope)
		}

	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			w.walkExpression(s.ReturnValue, scope)
		}

	case *parser.BlockStatement:
		for _, inner := range s.Statements {
			w.walkStatement(inner, scope)
		}
	}
}

func (w *ASTWalker) walkExpression(expr parser.Expression, scope string) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		w.addReference(e.Value, e.Token)

	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, inner := range e.Body.Statements {
				w.walkStatement(inner, scope)
			}
		}

	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			w.addReference(ident.Value, ident.Token)
		}
		w.walkExpression(e.Function, scope)
		for _, arg := range e.Arguments {
			w.walkExpression(arg, scope)
		}

	case *parser.DotExpression:
		w.walkExpression(e.Receiver, scope)

	case *parser.InfixExpression:
		w.walkExpression(e.Left, scope)
		w.walkExpression(e.Right, scope)

	case *parser.PrefixExpression:
		w.walkExpression(e.Right, scope)

	case *parser.GroupedExpression:
		w.walkExpression(e.Expression, scope)

	case *parser.IfExpression:
		w.walkExpression(e.Condition, scope)
		if e.Consequence != nil {
			for _, inner := range e.Consequence.Statements {
				w.walkStatement(inner, scope)
			}
		}
		if e.Alternative != nil {
			for _, inner := range e.Alternative.Statements {
				w.walkStatement(inner, scope)
			}
		}

	case *parser.IndexExpression:
		w.walkExpression(e.Left, scope)
		if e.Index != nil {
			w.walkExpression(e.Index, scope)
		}
	}
}

func (w *ASTWalker) addFunction(name string, token interface{}, params, results []*parser.Parameter, body *parser.BlockStatement, scope string) {
	var line, column int
	switch t := token.(type) {
	case lexer.Token:
		line = t.Line
		column = t.Column
	case lexer.Position:
		line = t.Line
		column = t.Column
	}

	paramInfos := make([]ParamInfo, len(params))
	for i, p := range params {
		typeStr := ""
		if p.Type != nil {
			typeStr = p.Type.String()
		}
		paramInfos[i] = ParamInfo{Name: p.Name, Type: typeStr}
	}

	resultInfos := make([]ParamInfo, len(results))
	for i, r := range results {
		typeStr := ""
		if r.Type != nil {
			typeStr = r.Type.String()
		}
		resultInfos[i] = ParamInfo{Name: r.Name, Type: typeStr}
	}

	retType := ""
	s := fmt.Sprintf("fn(")
	for i, p := range paramInfos {
		if i > 0 {
			s += ", "
		}
		s += p.Name
	}
	s += ")"
	if retType != "" {
		s += " " + retType
	}

	entry := &IndexEntry{
		Name: name,
		Kind: SymbolKindFunction,
		Type: s,
		Location: Location{
			URI: w.uri,
			Range: Range{
				Start: Position{Line: uint32(line - 1), Character: uint32(column - 1)},
				End:   Position{Line: uint32(line - 1), Character: uint32(column - 1 + len(name))},
			},
		},
		Scope:        scope,
		Params:       paramInfos,
		ResultParams: resultInfos,
	}
	w.index.functions[name] = entry
	w.index.definitions[name] = entry
}

func (w *ASTWalker) addReference(name string, token interface{}) {
	var line, column int
	switch t := token.(type) {
	case lexer.Token:
		line = t.Line
		column = t.Column
	case lexer.Position:
		line = t.Line
		column = t.Column
	default:
		return
	}
	loc := Location{
		URI: w.uri,
		Range: Range{
			Start: Position{Line: uint32(line - 1), Character: uint32(column - 1)},
			End:   Position{Line: uint32(line - 1), Character: uint32(column - 1 + len(name))},
		},
	}
	w.index.references[name] = append(w.index.references[name], loc)
}

func (w *ASTWalker) addToScope(name string, entry *IndexEntry) {
	// already added directly to maps
}

func (w *ASTWalker) extractParams(params []*parser.Parameter) []ParamInfo {
	result := make([]ParamInfo, len(params))
	for i, p := range params {
		typeStr := ""
		if p.Type != nil {
			typeStr = p.Type.String()
		}
		result[i] = ParamInfo{Name: p.Name, Type: typeStr}
	}
	return result
}

func (w *ASTWalker) formatFuncLitDetail(fl *parser.FunctionLiteral) string {
	params := make([]string, len(fl.Parameters))
	for i, p := range fl.Parameters {
		typeStr := ""
		if p.Type != nil {
			typeStr = p.Type.String()
		}
		if typeStr != "" {
			params[i] = p.Name + " " + typeStr
		} else {
			params[i] = p.Name
		}
	}
	retType := ""
	if len(fl.Results) > 0 {
		retType = fl.Results[0].Type.String()
	}
	s := fmt.Sprintf("fn(%s)", strings.Join(params, ", "))
	if retType != "" {
		s += " " + retType
	}
	return s
}

func (w *ASTWalker) getExprType(expr parser.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return "i64"
	case *parser.FloatLiteral:
		return "f64"
	case *parser.StringLiteral:
		return "str"
	case *parser.CharLiteral:
		return "char"
	case *parser.BooleanLiteral:
		return "bool"
	case *parser.NilLiteral:
		return "nil"
	case *parser.Identifier:
		return ""
	case *parser.FunctionLiteral:
		return w.formatFuncLitDetail(e)
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			return "call " + ident.Value
		}
		return "call"
	case *parser.InfixExpression:
		return ""
	case *parser.PrefixExpression:
		return ""
	case *parser.GroupedExpression:
		return w.getExprType(e.Expression)
	default:
		return ""
	}
}

func (w *ASTWalker) getExprValue(expr parser.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value)
	case *parser.FloatLiteral:
		return fmt.Sprintf("%f", e.Value)
	case *parser.StringLiteral:
		return "\"" + e.Value + "\""
	case *parser.BooleanLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *parser.NilLiteral:
		return "nil"
	default:
		return ""
	}
}

func (w *ASTWalker) rangeFromIdent(ident *parser.Identifier) Range {
	return Range{
		Start: Position{
			Line:      uint32(ident.Token.Line - 1),
			Character: uint32(ident.Token.Column - 1),
		},
		End: Position{
			Line:      uint32(ident.Token.Line - 1),
			Character: uint32(ident.Token.Column - 1 + len(ident.Value)),
		},
	}
}
