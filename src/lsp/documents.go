package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

type DocumentManager struct {
	documents map[string]*TextDocument
	indices   map[string]*SymbolIndex
	mu        sync.RWMutex
}

func NewDocumentManager() *DocumentManager {
	return &DocumentManager{
		documents: make(map[string]*TextDocument),
		indices:   make(map[string]*SymbolIndex),
	}
}

func (m *DocumentManager) OpenDocument(uri string, text string) (*TextDocument, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if doc, ok := m.documents[uri]; ok {
		doc.Text = text
		doc.Item.Text = text
		doc.Item.Version++
		doc.Dirty = true
		return doc, nil
	}

	doc := &TextDocument{
		Item: TextDocumentItem{
			URI:        uri,
			LanguageID: "nolang",
			Version:    1,
			Text:       text,
		},
		Text:  text,
		Dirty: true,
	}

	m.documents[uri] = doc
	return doc, nil
}

func (m *DocumentManager) RemoveDocument(uri string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.documents, uri)
	delete(m.indices, uri)

	return nil
}

func (m *DocumentManager) UpdateDocument(uri string, changes []TextDocumentContentChange, version int) (*TextDocument, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}

	for _, change := range changes {
		if change.Range == nil {
			doc.Text = change.Text
		} else {
			m.applyContentChange(doc, change)
		}
	}

	doc.Item.Version = version
	doc.Dirty = true

	return doc, nil
}

func (m *DocumentManager) applyContentChange(doc *TextDocument, change TextDocumentContentChange) {
	lines := getLines(doc.Text)
	startLine := int(change.Range.Start.Line)
	startChar := int(change.Range.Start.Character)
	endLine := int(change.Range.End.Line)
	endChar := int(change.Range.End.Character)

	if startLine >= len(lines) {
		lines = append(lines, "")
	}

	before := ""
	if startLine < len(lines) {
		if startChar <= len(lines[startLine]) {
			before = lines[startLine][:startChar]
		} else {
			before = lines[startLine] + strings.Repeat(" ", startChar-len(lines[startLine]))
		}
	}

	after := ""
	if endLine < len(lines) {
		if endChar <= len(lines[endLine]) {
			after = lines[endLine][endChar:]
		}
	}

	newLine := before + change.Text + after

	newLines := make([]string, 0, len(lines)-(endLine-startLine)+1)
	newLines = append(newLines, lines[:startLine]...)
	newLines = append(newLines, newLine)
	if endLine+1 < len(lines) {
		newLines = append(newLines, lines[endLine+1:]...)
	}

	doc.Text = strings.Join(newLines, "\n")
}

func (m *DocumentManager) GetDocument(uri string) (*TextDocument, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	doc, ok := m.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}

	return doc, nil
}

func (m *DocumentManager) GetAllDocuments() map[string]*TextDocument {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*TextDocument, len(m.documents))
	for k, v := range m.documents {
		result[k] = v
	}

	return result
}

func (m *DocumentManager) ParseDocument(uri string) (*parser.Program, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.documents[uri]
	if !ok {
		return nil, nil, ErrDocumentNotFound
	}

	l := lexer.New(doc.Text)
	p := parser.New(l)
	p.Filename = filenameFromURI(uri)
	// Inject std module function signatures and struct field types so that
	// the parser can infer types from cross-module method calls (e.g.
	// tls-c.send() → ?i64), which enables option-match `it` binding injection.
	funcSigs, structFields := nbuild.CollectStdModuleSignatures()
	p.SetExternSignatures(funcSigs, structFields)
	ast := p.ParseProgram()

	errs := p.Errors()
	if len(errs) == 0 {
		doc.AST = ast
	}
	doc.Dirty = len(errs) > 0

	// Rebuild symbol index
	index := NewSymbolIndex(uri, doc.Item.Version)
	index.AddBuiltinSymbols()

	// Pre-populate auto-imported module exports (e.g., pi/e from std/math)
	// before the AST walk, so user-defined vars in main take precedence.
	if ast != nil {
		moduleNames := nbuild.GetStdModuleShortNames()
		for _, stmt := range ast.Statements {
			if use, ok := stmt.(*parser.UseStatement); ok {
				short := use.Path
				if idx := strings.LastIndex(short, "/"); idx >= 0 {
					short = short[idx+1:]
				}
				moduleNames = append(moduleNames, short)
			}
		}
		exports := nbuild.GetModuleExports(moduleNames)
		for _, ex := range exports {
			if ex.Value != "" {
				// Use the actual type from the module's type annotation
				// (e.g. "u64" for BLAKE2B-MASK), falling back to "i64"
				// for integer constants without explicit type.
				constType := ex.Type
				if constType == "" {
					constType = "i64"
				}
				index.symbols[ex.Name] = &IndexEntry{
					Name:  ex.Name,
					Kind:  SymbolKindConstant,
					Type:  constType,
					Value: ex.Value,
				}
				// Don't overwrite definitions — the module indexing loop below
				// provides proper Location info for go-to-definition.
				if _, exists := index.definitions[ex.Name]; !exists {
					index.definitions[ex.Name] = index.symbols[ex.Name]
				}
			} else {
				index.functions[ex.Name] = &IndexEntry{
					Name: ex.Name,
					Kind: SymbolKindFunction,
					Type: "fn",
				}
				if _, exists := index.definitions[ex.Name]; !exists {
					index.definitions[ex.Name] = index.functions[ex.Name]
				}
			}
		}

		// Index auto-imported std module files with location info for go-to-definition
		for _, info := range nbuild.GetStdModules() {
			modFilePath := nbuild.ResolveStdModulePath(info.ShortPath)
			if modFilePath == "" {
				continue
			}
			source, err := os.ReadFile(modFilePath)
			if err != nil {
				continue
			}
			l := lexer.New(string(source))
			p := parser.New(l)
			modProg := p.ParseProgram()
			if len(p.Errors()) > 0 {
				continue
			}
			if absPath, err := filepath.Abs(modFilePath); err == nil {
				modURI := "file://" + absPath
				for _, ms := range modProg.Statements {
					m.indexModuleStatement(index, ms, modURI)
				}
			}
		}

		// Index local module imports for go-to-definition
		for _, stmt := range ast.Statements {
			if use, ok := stmt.(*parser.UseStatement); ok && strings.HasPrefix(use.Path, "/") {
				relPath := strings.TrimPrefix(use.Path, "/")
				modFilePath := m.resolveLocalModuleFile(relPath, uri)
				if _, err := os.Stat(modFilePath); err != nil {
					continue
				}
				source, err := os.ReadFile(modFilePath)
				if err != nil {
					continue
				}
				l := lexer.New(string(source))
				p := parser.New(l)
				modProg := p.ParseProgram()
				if len(p.Errors()) > 0 {
					continue
				}
				modURI := "file://" + modFilePath
				for _, ms := range modProg.Statements {
					m.indexModuleStatement(index, ms, modURI)
				}
			}
		}

		// Index dependency-based module imports (e.g., github.com/org/repo/...)
		for _, stmt := range ast.Statements {
			if use, ok := stmt.(*parser.UseStatement); ok {
				if strings.HasPrefix(use.Path, "/") || strings.HasPrefix(use.Path, "std/") || use.Path == "std" {
					continue // handled by local or std module indexing
				}
				modFilePath := m.resolveDependencyModuleFile(use.Path, uri)
				if modFilePath == "" {
					continue
				}
				if _, err := os.Stat(modFilePath); err != nil {
					continue
				}
				source, err := os.ReadFile(modFilePath)
				if err != nil {
					continue
				}
				l := lexer.New(string(source))
				p := parser.New(l)
				modProg := p.ParseProgram()
				if len(p.Errors()) > 0 {
					continue
				}
				modURI := "file://" + modFilePath
				for _, ms := range modProg.Statements {
					m.indexModuleStatement(index, ms, modURI)
				}
			}
		}

		// Index ExportStatement symbols for go-to-definition
		// e.g. "@ /src/utils.greet a" resolves the alias "a" and function "greet"
		// to the definition in src/utils.no
		for _, stmt := range ast.Statements {
			if export, ok := stmt.(*parser.ExportStatement); ok && export.Path != "" && export.Function != "" {
				relPath := strings.TrimPrefix(export.Path, "/")
				targetFilePath := m.resolveLocalModuleFile(relPath, uri)
				if _, err := os.Stat(targetFilePath); err != nil {
					continue
				}
				source, err := os.ReadFile(targetFilePath)
				if err != nil {
					continue
				}
				l := lexer.New(string(source))
				p := parser.New(l)
				targetProg := p.ParseProgram()
				if len(p.Errors()) > 0 {
					continue
				}
				targetURI := "file://" + targetFilePath

				for _, ts := range targetProg.Statements {
					var name string
					var token interface{}

					switch s := ts.(type) {
					case *parser.FunctionDefinition:
						if s.Name == export.Function {
							name = s.Name
							token = s.Token
						}
					case *parser.LetStatement:
						if s.Name != nil && s.Name.Value == export.Function {
							if _, ok := s.Value.(*parser.FunctionLiteral); ok {
								name = s.Name.Value
								token = s.Name.Token
							}
						}
					}

					if name == "" {
						continue
					}

					var line, column int
					switch t := token.(type) {
					case lexer.Token:
						line = t.Line
						column = t.Column
					default:
						continue
					}

					loc := Location{
						URI: targetURI,
						Range: Range{
							Start: Position{Line: uint32(line - 1), Character: uint32(column - 1)},
							End:   Position{Line: uint32(line - 1), Character: uint32(column - 1 + len(name))},
						},
					}
					entry := &IndexEntry{
						Name:     export.Function,
						Kind:     CompletionItemKindFunction,
						Type:     "fn",
						Location: loc,
					}
					index.functions[export.Function] = entry
					index.definitions[export.Function] = entry

					// Also index the alias so clicking on it navigates to the target
					if export.Alias != "" {
						aliasEntry := *entry
						aliasEntry.Name = export.Alias
						index.functions[export.Alias] = &aliasEntry
						index.definitions[export.Alias] = &aliasEntry
					}
					break
				}
			}
		}

		// Register aliases for imported functions so that e.g.
		// "# path.fn as alias" creates an index entry for "alias".
		for _, stmt := range ast.Statements {
			if use, ok := stmt.(*parser.UseStatement); ok && use.Function != "" && use.Alias != "" {
				if entry, ok := index.functions[use.Function]; ok {
					aliasEntry := *entry
					aliasEntry.Name = use.Alias
					index.functions[use.Alias] = &aliasEntry
					index.definitions[use.Alias] = &aliasEntry
				}
			}
		}

		walker := NewASTWalker(index, doc, ast)
		walker.Walk()
	}
	m.indices[uri] = index

	return ast, errs, nil
}

func (m *DocumentManager) GetIndex(uri string) *SymbolIndex {
	m.mu.RLock()
	defer m.mu.RUnlock()

	idx, ok := m.indices[uri]
	if !ok {
		return nil
	}
	return idx
}

func (m *DocumentManager) IsDirty(uri string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	doc, ok := m.documents[uri]
	if !ok {
		return false
	}

	return doc.Dirty
}

var ErrDocumentNotFound = errors.New("document not found")

// resolveLocalModuleFile resolves a local module relative path (no leading /)
// to an absolute file path by searching for the project root (mod.jsonc).
func (m *DocumentManager) resolveLocalModuleFile(relPath, docURI string) string {
	// Extract document directory from URI (file:///path/to/file.no)
	docPath := strings.TrimPrefix(docURI, "file://")
	docDir := filepath.Dir(docPath)

	// Look for mod.jsonc upward
	root := docDir
	for {
		candidate := filepath.Join(root, "mod.jsonc")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(root, relPath) + ".no"
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}

	// Fallback: relative to document directory
	return filepath.Join(docDir, relPath) + ".no"
}

// resolveDependencyModuleFile resolves a dependency-based module import path
// (e.g., "github.com/org/repo/pkg/module") to an absolute .no file path.
func (m *DocumentManager) resolveDependencyModuleFile(usePath, docURI string) string {
	// Extract document directory from URI
	docPath := strings.TrimPrefix(docURI, "file://")
	docDir := filepath.Dir(docPath)

	// Load Package from the document's project root
	pkg, _ := nbuild.LoadPackage(docDir)
	if pkg == nil {
		return ""
	}

	// Check if this path matches a dependency
	modPath, err := pkg.ResolveDependencyModule(usePath)
	if err != nil || modPath == "" {
		return ""
	}
	return modPath
}

// indexModuleStatement adds a FunctionDefinition or function-typed LetStatement
// from a module file into the symbol index for go-to-definition support.
func (m *DocumentManager) indexModuleStatement(index *SymbolIndex, stmt parser.Statement, modURI string) {
	var name string
	var token interface{}
	var params []ParamInfo
	var resultParams []ParamInfo
	var isVariadic bool
	var doc string

	switch s := stmt.(type) {
	case *parser.FunctionDefinition:
		name = s.Name
		token = s.Token
		isVariadic = s.IsVariadic
		doc = extractDocComment(&s.CommentedNode)
		for _, p := range s.Parameters {
			typeStr := ""
			if p.Type != nil {
				typeStr = p.Type.String()
			}
			params = append(params, ParamInfo{Name: p.Name, Type: typeStr, DefaultValue: defaultExprString(p.DefaultExpr)})
		}
		for _, r := range s.Results {
			typeStr := ""
			if r.Type != nil {
				typeStr = r.Type.String()
			}
			resultParams = append(resultParams, ParamInfo{Name: r.Name, Type: typeStr})
		}
	case *parser.LetStatement:
		if s.Name != nil {
			if funcLit, ok := s.Value.(*parser.FunctionLiteral); ok {
				name = s.Name.Value
				token = s.Name.Token
				isVariadic = funcLit.IsVariadic
				doc = extractDocComment(&s.CommentedNode)
			for _, p := range funcLit.Parameters {
				typeStr := ""
				if p.Type != nil {
					typeStr = p.Type.String()
				}
				params = append(params, ParamInfo{Name: p.Name, Type: typeStr, DefaultValue: defaultExprString(p.DefaultExpr)})
			}
				for _, r := range funcLit.Results {
					typeStr := ""
					if r.Type != nil {
						typeStr = r.Type.String()
					}
					resultParams = append(resultParams, ParamInfo{Name: r.Name, Type: typeStr})
				}
			} else {
				// Regular variable/constant: index with location info so
				// go-to-definition works for module-level constants.
				typeStr := ""
				if s.Type != nil {
					typeStr = s.Type.String()
				}
				loc := Location{
					URI: modURI,
					Range: Range{
						Start: Position{Line: uint32(s.Name.Token.Line - 1), Character: uint32(s.Name.Token.Column - 1)},
						End:   Position{Line: uint32(s.Name.Token.Line - 1), Character: uint32(s.Name.Token.Column - 1 + len(s.Name.Value))},
					},
				}
				entry := &IndexEntry{
					Name:     s.Name.Value,
					Kind:     SymbolKindConstant,
					Type:     typeStr,
					Location: loc,
					Doc:      extractDocComment(&s.CommentedNode),
				}
				// Only add if not already defined with location info
				if existing, exists := index.definitions[s.Name.Value]; !exists || existing.Location.URI == "" {
					index.symbols[s.Name.Value] = entry
					index.definitions[s.Name.Value] = entry
				}
				return
			}
		} else {
			return
		}
	case *parser.ExternStatement:
		if s.Name == nil {
			return
		}
		name = s.Name.Value
		token = s.Token
		doc = extractDocComment(&s.CommentedNode)
		for _, p := range s.Parameters {
			typeStr := ""
			if p.Type != nil {
				typeStr = p.Type.String()
			}
			params = append(params, ParamInfo{Name: p.Name, Type: typeStr})
		}
		for _, r := range s.Results {
			typeStr := ""
			if r.Type != nil {
				typeStr = r.Type.String()
			}
			resultParams = append(resultParams, ParamInfo{Name: r.Name, Type: typeStr})
		}
	default:
		return
	}

	// For variadic functions, the last parameter's type is stored as []type
	// but should be displayed as ..type
	if isVariadic && len(params) > 0 {
		last := &params[len(params)-1]
		if strings.HasPrefix(last.Type, "[]") {
			last.Type = ".." + last.Type[2:]
		}
	}

	var line, column int
	switch t := token.(type) {
	case lexer.Token:
		line = t.Line
		column = t.Column
	default:
		return
	}

	// Build function signature string: fn(a num, b num) (r num)
	sig := "fn("
	for i, p := range params {
		if i > 0 {
			sig += ", "
		}
		sig += p.Name
		if p.Type != "" {
			sig += " " + p.Type
		}
	}
	sig += ")"
	if len(resultParams) > 0 {
		sig += " ("
		for i, r := range resultParams {
			if i > 0 {
				sig += ", "
			}
			sig += r.Name
			if r.Type != "" {
				sig += " " + r.Type
			}
		}
		sig += ")"
	}

	loc := Location{
		URI: modURI,
		Range: Range{
			Start: Position{Line: uint32(line - 1), Character: uint32(column - 1)},
			End:   Position{Line: uint32(line - 1), Character: uint32(column - 1 + len(name))},
		},
	}
	entry := &IndexEntry{
		Name:         name,
		Kind:         SymbolKindFunction,
		Type:         sig,
		Location:     loc,
		Params:       params,
		ResultParams: resultParams,
		Doc:          doc,
	}
	index.functions[name] = entry
	index.definitions[name] = entry
	// Union methods (e.g. "num.sign") can be called by short name ("sign")
	// because rewriteUnionCalls dispatches by argument type.
	// Register the short name so go-to-definition works for short-name calls.
	if idx := strings.LastIndex(name, "."); idx > 0 {
		shortName := name[idx+1:]
		// Don't overwrite an existing entry for the short name
		if _, exists := index.functions[shortName]; !exists {
			index.functions[shortName] = entry
			index.definitions[shortName] = entry
		}
	}
}

// filenameFromURI extracts the base filename from a file:// URI.
func filenameFromURI(uri string) string {
	// Strip "file://" prefix
	path := strings.TrimPrefix(uri, "file://")
	return filepath.Base(path)
}
