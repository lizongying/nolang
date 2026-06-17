package lsp

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
	index *SymbolIndex
	doc   *TextDocument
}

func NewSymbolProvider(doc *TextDocument, index *SymbolIndex) *SymbolProvider {
	return &SymbolProvider{
		index: index,
		doc:   doc,
	}
}

func (sp *SymbolProvider) GetSymbols() []DocumentSymbol {
	if sp.index == nil {
		return []DocumentSymbol{}
	}

	entries := sp.index.GetAllSymbols()

	// Two-pass approach:
	// Pass 1: register all top-level (scope=="") entries
	// Pass 2: attach scoped entries as children

	type namedSym struct {
		sym  DocumentSymbol
		name string
	}
	var roots []namedSym
	funcIdx := make(map[string]int) // name → index in roots

	// Pass 1: collect top-level entries
	for _, e := range entries {
		if e.Scope != "" {
			continue
		}
		sym := DocumentSymbol{
			Name:           e.Name,
			Kind:           e.Kind,
			Range:          e.Location.Range,
			SelectionRange: e.Location.Range,
			Children:       []DocumentSymbol{},
		}
		for _, p := range e.Params {
			sym.Children = append(sym.Children, DocumentSymbol{
				Name:           p.Name,
				Kind:           SymbolKindParameter,
				Range:          e.Location.Range,
				SelectionRange: e.Location.Range,
				Children:       []DocumentSymbol{},
			})
		}
		funcIdx[e.Name] = len(roots)
		roots = append(roots, namedSym{sym: sym, name: e.Name})
	}

	// Pass 2: attach scoped entries to their parent
	for _, e := range entries {
		if e.Scope == "" {
			continue
		}
		child := DocumentSymbol{
			Name:           e.Name,
			Kind:           e.Kind,
			Range:          e.Location.Range,
			SelectionRange: e.Location.Range,
			Children:       []DocumentSymbol{},
		}
		if idx, ok := funcIdx[e.Scope]; ok {
			roots[idx].sym.Children = append(roots[idx].sym.Children, child)
		} else {
			// Orphan: add as root
			roots = append(roots, namedSym{sym: child, name: e.Name})
		}
	}

	result := make([]DocumentSymbol, len(roots))
	for i, ns := range roots {
		result[i] = ns.sym
	}
	return result
}
