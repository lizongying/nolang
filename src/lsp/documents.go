package lsp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/mod"
	"github.com/lizongying/nolang/parser"
)

type DocumentManager struct {
	documents map[string]*TextDocument
	indices   map[string]*SymbolIndex
	mu        sync.RWMutex
	// moduleCache caches parsed module files (std / local / dependency) keyed
	// by absolute path. std modules rarely change within a session, so this
	// avoids re-parsing the entire std library on every keystroke.
	moduleCache map[string]*moduleCacheEntry
	moduleMu    sync.Mutex
}

// moduleCacheEntry holds a parsed module program together with the file's
// modtime and source text at parse time, so the entry can be invalidated when
// the file changes on disk.
type moduleCacheEntry struct {
	modTime time.Time
	prog    *parser.Program
	source  string
}

func NewDocumentManager() *DocumentManager {
	return &DocumentManager{
		documents:   make(map[string]*TextDocument),
		indices:     make(map[string]*SymbolIndex),
		moduleCache: make(map[string]*moduleCacheEntry),
	}
}

// parseModuleFile reads and parses a module .no file, caching the result by
// absolute path + modtime. This eliminates the per-keystroke re-parse of the
// whole std library that go-to-definition indexing previously performed.
func (m *DocumentManager) parseModuleFile(modFilePath string) (*parser.Program, string, error) {
	info, err := os.Stat(modFilePath)
	if err != nil {
		return nil, "", err
	}
	source, err := os.ReadFile(modFilePath)
	if err != nil {
		return nil, "", err
	}
	m.moduleMu.Lock()
	if e, ok := m.moduleCache[modFilePath]; ok && e.modTime.Equal(info.ModTime()) && e.source == string(source) {
		m.moduleMu.Unlock()
		return e.prog, e.source, nil
	}
	m.moduleMu.Unlock()

	l := lexer.New(string(source))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, "", fmt.Errorf("parse errors in module %s", modFilePath)
	}
	m.moduleMu.Lock()
	m.moduleCache[modFilePath] = &moduleCacheEntry{modTime: info.ModTime(), prog: prog, source: string(source)}
	m.moduleMu.Unlock()
	return prog, string(source), nil
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
	// Snapshot the document's text and version under a brief read lock.
	// All heavy work (lex/parse, file IO, indexing) runs OUTSIDE the lock so
	// that concurrent readers (GetDocument/GetIndex) and other parses are not
	// blocked while a single keystroke re-parses the std library.
	m.mu.RLock()
	doc, ok := m.documents[uri]
	var text string
	var version int
	if ok {
		text = doc.Text
		version = doc.Item.Version
	}
	m.mu.RUnlock()
	if !ok {
		return nil, nil, ErrDocumentNotFound
	}

	l := lexer.New(text)
	p := parser.New(l)
	p.Filename = filenameFromURI(uri)
	// Inject std module function signatures and struct field types so that
	// the parser can infer types from cross-module method calls (e.g.
	// tls-c.send() → ?i64), which enables option-match `it` binding injection.
	funcSigs, structFields := checker.CollectStdModuleSignatures()
	p.SetExternSignatures(funcSigs, structFields)
	ast := p.ParseProgram()

	errs := p.Errors()

	// Rebuild symbol index
	index := NewSymbolIndex(uri, version)
	index.AddBuiltinSymbols()

	// Pre-populate auto-imported module exports (e.g., pi/e from std/math)
	// before the AST walk, so user-defined vars in main take precedence.
	if ast != nil {
		moduleNames := checker.GetStdModuleShortNames()
		for _, stmt := range ast.Statements {
			if use, ok := stmt.(*parser.UseStatement); ok {
				short := use.Path
				if idx := strings.LastIndex(short, "/"); idx >= 0 {
					short = short[idx+1:]
				}
				moduleNames = append(moduleNames, short)
			}
		}
		exports := checker.GetModuleExports(moduleNames)
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

		// Index auto-imported std module files with location info for go-to-definition.
		// Module parses are cached by path+modtime (see parseModuleFile), so the
		// std library is only parsed once per session, not on every keystroke.
		for _, info := range checker.GetStdModules() {
			modFilePath := checker.ResolveStdModulePath(info.ShortPath)
			if modFilePath == "" {
				continue
			}
			modProg, source, err := m.parseModuleFile(modFilePath)
			if err != nil || modProg == nil {
				continue
			}
			if absPath, err := filepath.Abs(modFilePath); err == nil {
				modURI := "file://" + absPath
				// Scan for comment-only built-in function declarations FIRST,
				// so that built-in functions like `read`/`write` get their
				// comment declaration location before any struct method
				// (e.g. `file.read`) registers the same short name.
				m.indexBuiltinComments(index, string(source), modURI, info.ShortName)
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
				modProg, _, err := m.parseModuleFile(modFilePath)
				if err != nil || modProg == nil {
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
				modProg, _, err := m.parseModuleFile(modFilePath)
				if err != nil || modProg == nil {
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
				targetProg, _, err := m.parseModuleFile(targetFilePath)
				if err != nil || targetProg == nil {
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

		walker := NewASTWalker(index, &TextDocument{Item: TextDocumentItem{URI: uri}}, ast)
		walker.Walk()
	}
	// Publish results under a brief write lock. We only touch the maps here;
	// the heavy parse/index work above ran without holding m.mu.
	m.mu.Lock()
	if d, ok := m.documents[uri]; ok {
		if len(errs) == 0 {
			d.AST = ast
		}
		d.Dirty = len(errs) > 0
	}
	m.indices[uri] = index
	m.mu.Unlock()

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
	pkg, _ := mod.LoadPackage(docDir)
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
		params = buildParamInfos(s.Parameters, isVariadic)
		resultParams = buildParamInfos(s.Results, false)
	case *parser.LetStatement:
		if s.Name != nil {
			if funcLit, ok := s.Value.(*parser.FunctionLiteral); ok {
				name = s.Name.Value
				token = s.Name.Token
				isVariadic = funcLit.IsVariadic
				doc = extractDocComment(&s.CommentedNode)
				params = buildParamInfos(funcLit.Parameters, isVariadic)
				resultParams = buildParamInfos(funcLit.Results, false)
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
		params = buildParamInfos(s.Parameters, false)
		resultParams = buildParamInfos(s.Results, false)
	default:
		return
	}

	var line, column int
	switch t := token.(type) {
	case lexer.Token:
		line = t.Line
		column = t.Column
	default:
		return
	}

	entry := newFunctionEntry(name, modURI, "", doc, line, column, params, resultParams)
	index.functions[name] = entry
	index.definitions[name] = entry
	// Per the language spec (docs/docs/lang/module.md), only print/eprint/format
	// are exempt from module prefix. All other module-level functions and struct
	// methods must be called with a module prefix (e.g. fs.read) or via a
	// receiver instance (e.g. f.read()). Therefore, short names for dotted
	// methods (file.read → "read", tar.read → "read") are NOT registered —
	// they only cause ambiguity and conflicts across modules.
	// Built-in comment declarations (indexBuiltinComments) handle registering
	// the plain name for built-in functions that are called within the same
	// module file (e.g. read() inside fs.no's file.read method body).
}

// indexBuiltinComments scans raw source text for comment-only built-in function
// declarations (lines like `; write = (fd fd, data str, n i64) (written i64) { }`)
// and adds them to the symbol index with location info, so that go-to-definition
// on a built-in function jumps to its comment declaration in the std library.
//
// Without this, built-in functions added via AddBuiltinSymbols have empty Location
// fields, and go-to-definition does nothing.
func (m *DocumentManager) indexBuiltinComments(index *SymbolIndex, source, modURI, modShortName string) {
	// Build a set of known built-in function names (global, non-method).
	builtinNames := make(map[string]bool)
	for _, b := range builtin.BuiltinMethodList {
		// Skip type methods (e.g. "str.len", "[]byte.clear")
		if strings.Contains(b.MethodName, ".") {
			continue
		}
		builtinNames[b.MethodName] = true
	}

	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ";") {
			continue
		}
		// Strip the leading semicolon
		content := strings.TrimSpace(trimmed[1:])
		// Look for pattern: NAME = ( ...  — a comment-only function signature
		eqIdx := strings.Index(content, " = (")
		if eqIdx <= 0 {
			continue
		}
		name := content[:eqIdx]
		// Validate the name: must be alphanumeric with hyphens
		if !isValidIdent(name) {
			continue
		}
		// Only index if this is a known built-in function
		if !builtinNames[name] {
			continue
		}
		// If a real function definition with the SAME name exists (e.g. a
		// user-defined `write` function with a body), don't overwrite it —
		// the real implementation takes priority over the comment declaration.
		existing, exists := index.definitions[name]
		if exists && existing.Name == name && existing.Location.URI != "" && existing.Type != "build-in" {
			continue // real definition with same name, don't overwrite
		}

		loc := Location{
			URI: modURI,
			Range: Range{
				Start: Position{Line: uint32(i), Character: 0},
				End:   Position{Line: uint32(i), Character: uint32(len(line))},
			},
		}
		entry := &IndexEntry{
			Name:     name,
			Kind:     SymbolKindFunction,
			Type:     "build-in",
			Location: loc,
		}
		if existing != nil {
			// Preserve params/doc from AddBuiltinSymbols
			entry.Params = existing.Params
			entry.ResultParams = existing.ResultParams
			entry.Doc = existing.Doc
			entry.Value = existing.Value
		}
		index.functions[name] = entry
		index.definitions[name] = entry
		// Also store under the module-qualified name (e.g. "fs.read") so that
		// module-aware go-to-definition can resolve `fs.read` directly.
		if modShortName != "" {
			qualifiedName := modShortName + "." + name
			index.functions[qualifiedName] = entry
			index.definitions[qualifiedName] = entry
		}
	}
}

// isValidIdent checks if a string is a valid Nolang identifier
// (alphanumeric with hyphens, starting with a letter).
func isValidIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c == '-' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// filenameFromURI extracts the base filename from a file:// URI.
func filenameFromURI(uri string) string {
	// Strip "file://" prefix
	path := strings.TrimPrefix(uri, "file://")
	return filepath.Base(path)
}
