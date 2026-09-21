package lsp

import (
	"strings"
	"sync"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
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
	Doc          string      // doc comment text
	// IsBuiltin marks an entry whose "definition" is a compiler built-in
	// declaration (a `NAME = (...) (...) { }` stub, usually preceded by a
	// `#{buildin}` annotation or a `; build-in` comment) inside a std module.
	// Such entries carry the built-in's authoritative signature (Type /
	// Params / ResultParams from AddBuiltinSymbols) plus a source Location for
	// go-to-definition. They must NOT be overwritten by the stub's own AST
	// declaration (indexModuleStatement) nor by a later comment scan.
	IsBuiltin bool
}

type ParamInfo struct {
	Name         string
	Type         string
	DefaultValue string // 參數默認值的字串表示（如 "1024"），空串表示無默認值
}

// scopeRange 記錄一個函式（或方法）宣告在檔案中的行號區間，用於把游標位置
// 映射回「所在的函式作用域」。LSP 的符號表本質是扁平的（同名符號只留一個），
// 但 nolang 允許不同函式各自宣告同名區域變數（如 fs.no 裡 `size` 同時是
// stat-size 的結果參數、read-bytes 的區域變數、append 的區域變數）。
// 有了 scopeRange，hover / go-to-definition 才能在同一個作用域內精確解析，
// 避免「A 函式的區域變數」被「B 函式的結果參數」蓋掉。
type scopeRange struct {
	Name  string
	Start Position
	End   Position
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
	scopeRanges  []scopeRange             // function/method body ranges, for scope-aware lookup
}

// AddScopeRange 由 ASTWalker 在走訪函式宣告時呼叫。
func (idx *SymbolIndex) AddScopeRange(name string, r Range) {
	if name == "" {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.scopeRanges = append(idx.scopeRanges, scopeRange{Name: name, Start: r.Start, End: r.End})
}

// enclosingScopeAt 回傳游標所在函式（或方法）的名稱；游標位於頂層時回傳 ""。
// 多個區間重疊（巢狀函式）時取最內層（Start 最大者）。
func (idx *SymbolIndex) enclosingScopeAt(pos Position) string {
	best := ""
	var bestStart Position
	found := false
	for _, sr := range idx.scopeRanges {
		if isPosBeforeOrAt(sr.Start, pos) && isPosBefore(pos, sr.End) {
			if !found || isPosBefore(bestStart, sr.Start) {
				best, bestStart, found = sr.Name, sr.Start, true
			}
		}
	}
	return best
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
	// 游標不在任何宣告區間內（一般是一次「引用」）。扁平符號表對同名符號採
	// 後寫覆蓋，跨函式同名區域變數會互相污染（典型：`size ?= fstat-size(.fd)`
	// 被 stat-size 的結果參數 `size ?i64` 蓋掉）。先做作用域精確比對：
	// 找出「與游標同一函式」的宣告，並取游標之前最近的一筆。
	if scope := idx.enclosingScopeAt(pos); scope != "" {
		var scoped, fallbackEntry *IndexEntry
		for _, e := range idx.declarations[name] {
			if e.Scope != scope {
				continue
			}
			fallbackEntry = e
			if isPosBeforeOrAt(e.Location.Range.Start, pos) {
				scoped = e
			}
		}
		if scoped == nil {
			scoped = fallbackEntry
		}
		if scoped != nil {
			return scoped, true
		}
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
			// A parameter that is the RECEIVER'S ELEMENT TYPE is registered as
			// i64 (the registry serves every []T from one entry), so hovering
			// `[]t.push` would read "push(i64)". ElemParams marks those
			// positions: render them as `t`, the element type variable.
			typ := p.String()
			if i < len(m.ElemParams) && m.ElemParams[i] {
				typ = "t"
			}
			params[i] = ParamInfo{Name: p.String(), Type: typ}
		}
		// 語言層返回型別：option-return 內建（stat-size/fstat-size/file-size）
		// 的註冊表 Return 是原始 C pair (T, ok)，但 std 宣告是單一 ?T，
		// 必須折疊為單一 ?T 作為返回型別（見 builtin.optionReturnBuiltins），
		// 否則 `size ?= fstat-size(.fd)` 的 LSP 型別推導會拿到 (?i64, bool)。
		retType := ""
		resultParams := make([]ParamInfo, 0, len(m.Return))
		if n := len(m.Return); n > 0 {
			if builtin.IsOptionReturnBuiltin(m.MethodName) {
				retType = "?" + m.Return[0].String()
				resultParams = append(resultParams, ParamInfo{Type: retType})
			} else {
				types := make([]string, n)
				for i, rt := range m.Return {
					types[i] = rt.String()
					resultParams = append(resultParams, ParamInfo{Type: types[i]})
				}
				if n == 1 {
					retType = types[0]
				} else {
					retType = "(" + strings.Join(types, ", ") + ")"
				}
			}
		}
		idx.functions[name] = &IndexEntry{
			Name:         name,
			Kind:         kind,
			Type:         formatFuncType(params, retType),
			Params:       params,
			ResultParams: resultParams,
			Value:        m.Doc,
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

// extractDocComment extracts doc comment text from a CommentedNode.
func extractDocComment(cn *parser.CommentedNode) string {
	if cn == nil || cn.Doc == nil || len(cn.Doc.List) == 0 {
		return ""
	}
	var lines []string
	for _, c := range cn.Doc.List {
		text := strings.TrimSpace(c.Text)
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}
