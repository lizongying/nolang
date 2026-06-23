package lsp

import (
	"sync"

	"github.com/lizongying/nolang/builtin"
)

type IndexEntry struct {
	Name         string
	Kind         int
	Type         string
	Location     Location
	Scope        string
	Value        string
	Params       []ParamInfo
	ResultParams []ParamInfo // result/output parameter types
}

type ParamInfo struct {
	Name string
	Type string
}

type SymbolIndex struct {
	mu           sync.RWMutex
	uri          string
	version      int
	symbols      map[string]*IndexEntry
	definitions  map[string]*IndexEntry
	references   map[string][]Location
	functions    map[string]*IndexEntry
	declarations map[string][]*IndexEntry // all declarations per name, for AST-range lookup
}

func NewSymbolIndex(uri string, version int) *SymbolIndex {
	return &SymbolIndex{
		uri:          uri,
		version:      version,
		symbols:      make(map[string]*IndexEntry),
		definitions:  make(map[string]*IndexEntry),
		references:   make(map[string][]Location),
		functions:    make(map[string]*IndexEntry),
		declarations: make(map[string][]*IndexEntry),
	}
}

func (idx *SymbolIndex) Lookup(name string) (*IndexEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if e, ok := idx.symbols[name]; ok {
		return e, true
	}
	if e, ok := idx.functions[name]; ok {
		return e, true
	}
	return nil, false
}

func (idx *SymbolIndex) GetDefinition(name string) (*IndexEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if e, ok := idx.definitions[name]; ok {
		return e, true
	}
	return nil, false
}

// LookupAtPosition finds the declaration whose AST range contains the given position.
// Uses AST node range containment: node.Start ≤ cursor < node.End
// Among nested matches, picks the innermost (highest start offset).
func (idx *SymbolIndex) LookupAtPosition(name string, pos Position) (*IndexEntry, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var best *IndexEntry
	for _, e := range idx.declarations[name] {
		s := e.Location.Range.Start
		ePos := e.Location.Range.End
		// Check if cursor is within the AST node's range: start ≤ cursor < end
		if isPosBeforeOrAt(s, pos) && isPosBefore(pos, ePos) {
			if best == nil || isPosBefore(best.Location.Range.Start, s) {
				best = e
			}
		}
	}
	if best != nil {
		return best, true
	}
	// Fall back to flat lookup
	if e, ok := idx.symbols[name]; ok {
		return e, true
	}
	if e, ok := idx.functions[name]; ok {
		return e, true
	}
	return nil, false
}

// isPosBefore returns true if a is strictly before b.
func isPosBefore(a, b Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}

// isPosBeforeOrAt returns true if a is before or at the same position as b.
func isPosBeforeOrAt(a, b Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character <= b.Character
}

func (idx *SymbolIndex) GetReferences(name string) []Location {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.references[name]
}

func (idx *SymbolIndex) GetAllSymbols() []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []*IndexEntry
	for _, e := range idx.symbols {
		result = append(result, e)
	}
	for _, e := range idx.functions {
		result = append(result, e)
	}
	return result
}

func (idx *SymbolIndex) GetAllFunctions() []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []*IndexEntry
	for _, e := range idx.functions {
		result = append(result, e)
	}
	return result
}

func (idx *SymbolIndex) GetSymbolsBeforeLine(line uint32) []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []*IndexEntry
	for _, e := range idx.symbols {
		if e.Location.Range.Start.Line <= line {
			result = append(result, e)
		}
	}
	for _, e := range idx.functions {
		if e.Location.Range.Start.Line <= line {
			result = append(result, e)
		}
	}
	return result
}

func (idx *SymbolIndex) GetFunctionsBeforeLine(line uint32) []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []*IndexEntry
	for _, e := range idx.functions {
		if e.Location.Range.Start.Line <= line {
			result = append(result, e)
		}
	}
	return result
}

func (idx *SymbolIndex) Search(query string) []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []*IndexEntry
	lowerQuery := toLowerStr(query)
	for _, e := range idx.symbols {
		if containsIgnoreCase(e.Name, lowerQuery) {
			result = append(result, e)
		}
	}
	for _, e := range idx.functions {
		if containsIgnoreCase(e.Name, lowerQuery) {
			result = append(result, e)
		}
	}
	return result
}

func (idx *SymbolIndex) AddBuiltinSymbols() {
	for _, m := range builtin.BuiltinMethodList {
		name := m.MethodName
		if _, exists := idx.functions[name]; exists {
			continue
		}
		kind := SymbolKindFunction
		if len(m.Params) == 0 && len(m.Return) == 0 {
			kind = SymbolKindConstant
		}
		params := make([]ParamInfo, len(m.Params))
		for i, p := range m.Params {
			params[i] = ParamInfo{Name: p.String(), Type: p.String()}
		}
		retType := ""
		if len(m.Return) > 0 {
			retType = m.Return[0].String()
		}
		idx.functions[name] = &IndexEntry{
			Name:   name,
			Kind:   kind,
			Type:   formatFuncType(params, retType),
			Params: params,
			Value:  m.Doc,
		}
	}
}

func formatFuncType(params []ParamInfo, retType string) string {
	s := "fn("
	for i, p := range params {
		if i > 0 {
			s += ", "
		}
		s += p.Name
	}
	s += ")"
	if retType != "" {
		s += " " + retType
	}
	return s
}
