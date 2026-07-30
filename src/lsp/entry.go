package lsp

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// This file is the single place where "function-like AST node → IndexEntry
// building blocks" conversion lives. Both index paths — the current-document
// walk (walker.go ASTWalker) and the module-file indexing loop
// (documents.go indexModuleStatement) — share these helpers so parameter
// extraction, variadic display (..type) and signature formatting never
// drift apart again.

// buildParamInfos converts an AST parameter list to ParamInfo slices.
// When isVariadic is true the last parameter's []type is displayed as ..type.
// Result parameter lists should pass isVariadic=false.
func buildParamInfos(params []*parser.Parameter, isVariadic bool) []ParamInfo {
	infos := make([]ParamInfo, len(params))
	for i, p := range params {
		typeStr := ""
		if p.Type != nil {
			typeStr = p.Type.String()
		}
		infos[i] = ParamInfo{Name: p.Name, Type: typeStr, DefaultValue: defaultExprString(p.DefaultExpr)}
	}
	if isVariadic && len(infos) > 0 {
		last := &infos[len(infos)-1]
		if strings.HasPrefix(last.Type, "[]") {
			last.Type = ".." + last.Type[2:]
		}
	}
	return infos
}

// fnSignature renders the canonical function signature string used in hover,
// completion detail and the symbol index: fn(a i64, b i64 = 10) (r i64).
// Parameter default values are included; result names/types are appended in
// a trailing parenthesized group when present.
func fnSignature(params, results []ParamInfo) string {
	var b strings.Builder
	b.WriteString("fn(")
	for i, p := range params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name)
		if p.Type != "" {
			b.WriteString(" ")
			b.WriteString(p.Type)
		}
		if p.DefaultValue != "" {
			b.WriteString(" = ")
			b.WriteString(p.DefaultValue)
		}
	}
	b.WriteString(")")
	if len(results) > 0 {
		b.WriteString(" (")
		for i, r := range results {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(r.Name)
			if r.Type != "" {
				b.WriteString(" ")
				b.WriteString(r.Type)
			}
		}
		b.WriteString(")")
	}
	return b.String()
}

// newFunctionEntry builds a function IndexEntry at the given 1-based
// line/column with the shared signature format.
func newFunctionEntry(name, uri, scope, doc string, line, column int, params, results []ParamInfo) *IndexEntry {
	return &IndexEntry{
		Name: name,
		Kind: SymbolKindFunction,
		Type: fnSignature(params, results),
		Location: Location{
			URI: uri,
			Range: Range{
				Start: Position{Line: uint32(line - 1), Character: uint32(column - 1)},
				End:   Position{Line: uint32(line - 1), Character: uint32(column - 1 + len(name))},
			},
		},
		Scope:        scope,
		Params:       params,
		ResultParams: results,
		Doc:          doc,
	}
}
