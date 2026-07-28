package lsp

import (
	"fmt"
	"runtime"
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
		w.addFunction(s.Name, s.Token, s.Parameters, s.Results, s.Body, scope, s.IsVariadic, extractDocComment(&s.CommentedNode))
		// Index parameters and result parameters as symbols so hover on
		// parameter names shows the declared type (e.g. `n i64` → i64)
		// instead of falling back to a same-named variable in another function.
		w.indexFunctionParams(s.Parameters, s.Results, s.Name)
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
				// For variadic functions, convert last param's []type to ..type
				if funcLit.IsVariadic && len(params) > 0 {
					last := &params[len(params)-1]
					if strings.HasPrefix(last.Type, "[]") {
						last.Type = ".." + last.Type[2:]
					}
				}
				resultParams := w.extractParams(funcLit.Results)
				value = detail
				entry := &IndexEntry{
					Name: s.Name.Value,
					Kind: kind,
					Type: detail,
					Location: Location{
						URI:   w.uri,
						Range: w.rangeFromIdent(s.Name),
					},
					Scope:        scope,
					Value:        value,
					Params:       params,
					ResultParams: resultParams,
					Doc:          extractDocComment(&s.CommentedNode),
				}
				// 平台變體偏好：當前開發平台變體覆蓋其他變體；平台通用宣告僅在無變體時儲存
				if existing, exists := w.index.functions[s.Name.Value]; !exists || existing.Location.URI == "" || matchesDevPlatform(w.program.Sem.PlatformKeysOf(s)) {
					w.index.functions[s.Name.Value] = entry
					w.index.definitions[s.Name.Value] = entry
				}
				// Index parameters and result parameters as symbols so hover on
				// parameter names shows the declared type.
				w.indexFunctionParams(funcLit.Parameters, funcLit.Results, s.Name.Value)
				if funcLit.Body != nil {
					for _, inner := range funcLit.Body.Statements {
						w.walkStatement(inner, s.Name.Value)
					}
				}
			} else {
			// Prefer the explicit type annotation (e.g. `BLAKE2B-MASK u64 = ...`)
			// over the inferred type from the value (which would be "i64" for
			// integer literals, or "f64" for float literals).
			detail = ""
			if s.Type != nil {
				detail = s.Type.String()
			}
			if detail == "" {
				detail = w.getExprType(s.Value)
			}
			// If the inferred type is a "call X" placeholder (not a real type)
			// or empty, preserve the type from the existing declaration so
			// hover doesn't lose type information (e.g. a parameter `n i64`
			// reassigned as `n = write(...)` should still show i64).
			if detail == "" || strings.HasPrefix(detail, "call ") {
				if existing, exists := w.index.symbols[s.Name.Value]; exists && existing.Type != "" && !strings.HasPrefix(existing.Type, "call ") {
					detail = existing.Type
				}
			}
				value = w.getExprValue(s.Value)
				entry := &IndexEntry{
					Name: s.Name.Value,
					Kind: kind,
					Type: detail,
					Location: Location{
						URI:   w.uri,
						Range: rangeFromNode(s),
					},
					Scope: scope,
					Value: value,
				}
				// Current file definitions always take precedence over auto-imported
				// module export entries (which lack proper Location info). This ensures
				// go-to-definition and hover use the correct type and location.
				// 平台變體偏好：當前開發平台變體覆蓋其他變體；平台通用宣告僅在無變體時儲存
				existing, exists := w.index.symbols[s.Name.Value]
				shouldStore := false
				if !exists {
					shouldStore = true
				} else if existing.Location.URI == "" {
					shouldStore = true // 覆蓋 auto-imported 條目
				} else if matchesDevPlatform(w.program.Sem.PlatformKeysOf(s)) {
					shouldStore = true // 當前開發平台變體優先
				}

				if shouldStore {
					w.index.symbols[s.Name.Value] = entry
					w.index.definitions[s.Name.Value] = entry
				}
				// Store all declarations for AST-range based lookup（保留所有變體）
				w.index.declarations[s.Name.Value] = append(w.index.declarations[s.Name.Value], entry)
			}

			if s.Value != nil {
				w.walkExpression(s.Value, scope)
			}
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
		for i, target := range s.Targets {
			if ident, ok := target.(*parser.Identifier); ok {
				exprType := ""
				if i < len(resultTypes) {
					exprType = resultTypes[i].Type
				}
				entry := &IndexEntry{
					Name: ident.Value,
					Kind: SymbolKindVariable,
					Type: exprType,
					Location: Location{
						URI:   w.uri,
						Range: w.rangeFromIdent(ident),
					},
					Scope: scope,
				}
				w.index.symbols[ident.Value] = entry
				w.index.definitions[ident.Value] = entry
			}
			// IndexExpression targets (e.g., fields[n]) are not new symbols
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

	case *parser.EnumDefinition:
		entry := &IndexEntry{
			Name:  s.Name,
			Kind:  SymbolKindEnum,
			Type:  "enum",
			Value: formatEnumValues(s.Values),
			Location: Location{
				URI:   w.uri,
				Range: rangeFromNode(s),
			},
		}
		w.index.symbols[s.Name] = entry
		w.index.definitions[s.Name] = entry

		// Register each enum variant as an EnumMember symbol
		for _, v := range s.Values {
			variantEntry := &IndexEntry{
				Name:  v.Name,
				Kind:  SymbolKindEnumMember,
				Type:  s.Name,
				Value: fmt.Sprintf("%d", v.Value),
				Location: Location{
					URI: w.uri,
					Range: Range{
						Start: Position{Line: uint32(v.Token.Line - 1), Character: uint32(v.Token.Column - 1)},
						End:   Position{Line: uint32(v.Token.Line - 1), Character: uint32(v.Token.Column - 1 + len(v.Name))},
					},
				},
			}
			if _, exists := w.index.symbols[v.Name]; !exists {
				w.index.symbols[v.Name] = variantEntry
				w.index.definitions[v.Name] = variantEntry
			}
			w.index.declarations[v.Name] = append(w.index.declarations[v.Name], variantEntry)
		}

	case *parser.ExternStatement:
		if s.Name != nil {
			w.addFunction(s.Name.Value, s.Token, s.Parameters, s.Results, nil, "", false, extractDocComment(&s.CommentedNode))
		}
	}
}

func formatEnumValues(values []*parser.EnumValue) string {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = v.Name
	}
	return strings.Join(names, " | ")
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

func (w *ASTWalker) addFunction(name string, token interface{}, params, results []*parser.Parameter, body *parser.BlockStatement, scope string, isVariadic bool, doc string) {
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
		paramInfos[i] = ParamInfo{Name: p.Name, Type: typeStr, DefaultValue: defaultExprString(p.DefaultExpr)}
	}

	// For variadic functions, the last parameter's type is stored as []type
	// but should be displayed as ..type
	if isVariadic && len(paramInfos) > 0 {
		last := &paramInfos[len(paramInfos)-1]
		if strings.HasPrefix(last.Type, "[]") {
			last.Type = ".." + last.Type[2:]
		}
	}

	resultInfos := make([]ParamInfo, len(results))
	for i, r := range results {
		typeStr := ""
		if r.Type != nil {
			typeStr = r.Type.String()
		}
		resultInfos[i] = ParamInfo{Name: r.Name, Type: typeStr}
	}

	s := "fn("
	for i, p := range paramInfos {
		if i > 0 {
			s += ", "
		}
	s += p.Name
	if p.Type != "" {
		s += " " + p.Type
	}
	if p.DefaultValue != "" {
		s += " = " + p.DefaultValue
	}
	}
	s += ")"
	if len(resultInfos) > 0 {
		s += " ("
		for i, r := range resultInfos {
			if i > 0 {
				s += ", "
			}
			s += r.Name
			if r.Type != "" {
				s += " " + r.Type
			}
		}
		s += ")"
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
		Doc:          doc,
	}
	w.index.functions[name] = entry
	w.index.definitions[name] = entry
}

// indexFunctionParams indexes function parameters and result parameters as
// symbols so that hovering over a parameter name shows its declared type
// (e.g. `n i64` → Type: i64). Without this, hover on a parameter falls back
// to flat symbol lookup and may find a same-named variable from another
// function body.
func (w *ASTWalker) indexFunctionParams(params, results []*parser.Parameter, scope string) {
	for _, p := range params {
		w.indexSingleParam(p, scope)
	}
	for _, p := range results {
		w.indexSingleParam(p, scope)
	}
}

func (w *ASTWalker) indexSingleParam(p *parser.Parameter, scope string) {
	if p == nil || p.Name == "" {
		return
	}
	typeStr := ""
	if p.Type != nil {
		typeStr = p.Type.String()
	}
	entry := &IndexEntry{
		Name: p.Name,
		Kind: SymbolKindVariable,
		Type: typeStr,
		Location: Location{
			URI: w.uri,
			Range: Range{
				Start: Position{Line: uint32(p.Token.Line - 1), Character: uint32(p.Token.Column - 1)},
				End:   Position{Line: uint32(p.Token.Line - 1), Character: uint32(p.Token.Column - 1 + len(p.Name))},
			},
		},
		Scope: scope,
	}
	// Add to declarations for position-based lookup (LookupAtPosition)
	w.index.declarations[p.Name] = append(w.index.declarations[p.Name], entry)
	// Add to symbols/definitions for flat lookup, but don't overwrite an
	// existing entry that has location info (e.g. a module-level constant).
	if existing, exists := w.index.symbols[p.Name]; !exists || existing.Location.URI == "" {
		w.index.symbols[p.Name] = entry
		w.index.definitions[p.Name] = entry
	}
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
		result[i] = ParamInfo{Name: p.Name, Type: typeStr, DefaultValue: defaultExprString(p.DefaultExpr)}
	}
	return result
}

// defaultExprString 將參數默認值表達式轉為字串表示。
func defaultExprString(expr parser.Expression) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *parser.Identifier:
		return e.Value
	case *parser.IntegerLiteral:
		return e.Token.Literal
	case *parser.FloatLiteral:
		return e.Token.Literal
	case *parser.StringLiteral:
		return "'" + e.Value + "'"
	case *parser.BooleanLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *parser.InfixExpression:
		return defaultExprString(e.Left) + " " + e.Operator + " " + defaultExprString(e.Right)
	case *parser.PrefixExpression:
		return e.Operator + defaultExprString(e.Right)
	default:
		return ""
	}
}

func (w *ASTWalker) formatFuncLitDetail(fl *parser.FunctionLiteral) string {
	params := make([]string, len(fl.Parameters))
	for i, p := range fl.Parameters {
		typeStr := ""
		if p.Type != nil {
			typeStr = p.Type.String()
		}
		// For variadic, convert []type to ..type on the last parameter
		if fl.IsVariadic && i == len(fl.Parameters)-1 && strings.HasPrefix(typeStr, "[]") {
			typeStr = ".." + typeStr[2:]
		}
		if typeStr != "" {
			params[i] = p.Name + " " + typeStr
		} else {
			params[i] = p.Name
		}
		if p.DefaultExpr != nil {
			params[i] += " = " + defaultExprString(p.DefaultExpr)
		}
	}
	s := "fn(" + strings.Join(params, ", ") + ")"
	if len(fl.Results) > 0 {
		s += " ("
		for i, r := range fl.Results {
			if i > 0 {
				s += ", "
			}
			typeStr := ""
			if r.Type != nil {
				typeStr = r.Type.String()
			}
			if typeStr != "" {
				s += r.Name + " " + typeStr
			} else {
				s += r.Name
			}
		}
		s += ")"
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
		// Look up the variable's type from the symbol index
		if entry, ok := w.index.symbols[e.Value]; ok && entry.Type != "" {
			return entry.Type
		}
		return ""
	case *parser.FunctionLiteral:
		return w.formatFuncLitDetail(e)
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			return "call " + ident.Value
		}
		return "call"
	case *parser.InfixExpression:
		// Try to infer the result type from the operands.
		// For arithmetic on two i64 values, the result is i64;
		// for string concatenation, the result is str.
		leftType := w.getExprType(e.Left)
		rightType := w.getExprType(e.Right)
		if leftType != "" {
			return leftType
		}
		return rightType
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
		// Use the token literal so values that overflow int64 (e.g. 18446744073709551615)
		// display correctly instead of showing the wrapped int64 value (e.g. -1).
		if e.Token.Literal != "" {
			return e.Token.Literal
		}
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

// rangeFromNode converts an AST node's position range to LSP Range (0-based).
func rangeFromNode(n parser.Node) Range {
	p := n.Pos()
	var ep lexer.Position
	func() {
		defer func() {
			if r := recover(); r != nil {
				ep = p
			}
		}()
		ep = n.EndPos()
	}()
	return Range{
		Start: Position{
			Line:      uint32(p.Line - 1),
			Character: uint32(p.Column - 1),
		},
		End: Position{
			Line:      uint32(ep.Line - 1),
			Character: uint32(ep.Column - 1),
		},
	}
}

// devPlatformKey 回傳當前開發平台的 platform key（如 "mac-arm64"、"linux-amd64"、"win-amd64"）。
// 用於 LSP 符號索引偏好當前開發平台的變體。
func devPlatformKey() string {
	var key string
	switch runtime.GOOS {
	case "linux":
		key = "linux"
	case "windows":
		key = "win"
	case "darwin":
		key = "mac"
	default:
		return ""
	}
	switch runtime.GOARCH {
	case "amd64":
		key += "-amd64"
	case "arm64":
		key += "-arm64"
	default:
		return ""
	}
	return key
}

// matchesDevPlatform 檢查宣告的 PlatformKeys 是否包含當前開發平台。
// 無 PlatformKeys（平台通用宣告）也回傳 true。
func matchesDevPlatform(platformKeys []string) bool {
	if len(platformKeys) == 0 {
		return true
	}
	devKey := devPlatformKey()
	for _, pk := range platformKeys {
		if pk == devKey {
			return true
		}
	}
	return false
}
