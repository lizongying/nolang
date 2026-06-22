package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	nbuild "github.com/lizongying/nolang/build"
	nolangfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/parser"
	"go.lsp.dev/jsonrpc2"
)

type Server struct {
	conn         jsonrpc2.Conn
	documents    *DocumentManager
	shutdown     bool
	mu           sync.RWMutex
	capabilities ServerCapabilities
}

func NewServer() *Server {
	return &Server{
		documents: NewDocumentManager(),
		shutdown:  false,
		capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				OpenClose:         true,
				Change:            TextDocumentSyncKindFull,
				WillSave:          true,
				WillSaveWaitUntil: true,
				Save: &SaveOptions{
					IncludeText: true,
				},
			},
			CompletionProvider: &CompletionOptions{
				ResolveProvider:   true,
				TriggerCharacters: []string{".", ":", "=", "@", "/"},
			},
			SignatureHelpProvider: &SignatureHelpOptions{
				TriggerCharacters: []string{"(", ","},
			},
			HoverProvider:              true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			DocumentHighlightProvider:  true,
			DocumentSymbolProvider:     true,
			WorkspaceSymbolProvider:    true,
			DocumentFormattingProvider: true,
			FoldingRangeProvider:       true,
			RenameProvider:             true,
			SemanticTokensProvider: &SemanticTokensOptions{
				Legend: GetSemanticTokensLegend(),
				Full:   true,
			},
		},
	}
}

func (s *Server) sendNotification(method string, params interface{}) error {
	return s.conn.Notify(context.Background(), method, params)
}

func (s *Server) publishDiagnostics(uri string, diagnostics []Diagnostic) error {
	return s.sendNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	})
}

func (s *Server) handleInitialize(params InitializeParams) (interface{}, error) {
	s.mu.Lock()
	s.shutdown = false
	s.mu.Unlock()

	return InitializeResult{
		Capabilities: s.capabilities,
		ServerInfo: &ServerInfo{
			Name:    "lsp",
			Version: "0.2.0",
		},
	}, nil
}

func (s *Server) handleShutdown() (interface{}, error) {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	return nil, nil
}

func (s *Server) handleExit() (interface{}, error) {
	s.mu.RLock()
	shutdown := s.shutdown
	s.mu.RUnlock()
	if shutdown {
		os.Exit(0)
	}
	os.Exit(1)
	return nil, nil
}

func (s *Server) handleTextDocumentDidOpen(params DidOpenTextDocumentParams) (interface{}, error) {
	_, err := s.documents.OpenDocument(params.TextDocument.URI, params.TextDocument.Text)
	if err != nil {
		return nil, err
	}

	go func() {
		ast, parseErrors, err := s.documents.ParseDocument(params.TextDocument.URI)
		if err != nil {
			log.Printf("Error parsing document: %v", err)
		}

		s.publishDocumentDiagnostics(params.TextDocument.URI, parseErrors, ast)
	}()

	return nil, nil
}

func (s *Server) publishDocumentDiagnostics(uri string, parseErrors []string, ast interface{}) {
	var diagnostics []Diagnostic

	for _, errMsg := range parseErrors {
		diagnostic := s.parseErrorToDiagnostic(errMsg)
		diagnostics = append(diagnostics, diagnostic)
	}

	if ast != nil {
		prog, ok := ast.(*parser.Program)
		if ok && prog != nil {
			typeErrs := nbuild.ValidateTypes(prog)
			for _, e := range typeErrs {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(e.Line - 1), Character: uint32(e.Column - 1)},
						End:   Position{Line: uint32(e.Line - 1), Character: uint32(e.Column)},
					},
					Severity: DiagnosticSeverityError,
					Source:   "nolang-type-checker",
					Message:  e.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			namingWarnings := nbuild.ValidateNaming(prog)
			for _, w := range namingWarnings {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(w.Line - 1), Character: uint32(w.Column - 1)},
						End:   Position{Line: uint32(w.Line - 1), Character: uint32(w.Column)},
					},
					Severity: DiagnosticSeverityWarning,
					Source:   "nolang-lint",
					Message:  w.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			unusedVars := nbuild.ValidateUnusedVars(prog)
			for _, u := range unusedVars {
				endChar := uint32(u.Column)
				if u.EndColumn > 0 {
					endChar = uint32(u.EndColumn)
				}
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: endChar},
					},
					Severity: DiagnosticSeverityHint,
					Source:   "nolang-lint",
					Tags:     []DiagnosticTag{DiagnosticTagUnnecessary},
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			undefinedVars := nbuild.ValidateUndefinedVars(prog)
			for _, u := range undefinedVars {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityError,
					Source:   "nolang-lint",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			useKeywordHints := nbuild.ValidateUseKeyword(prog)
			for _, u := range useKeywordHints {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityHint,
					Source:   "nolang-lint",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			useAliasHints := nbuild.ValidateUseAlias(prog)
			for _, u := range useAliasHints {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityHint,
					Source:   "nolang-lint",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			// Validate URL-style import paths are declared in mod.jsonc dependencies
			docPath := strings.TrimPrefix(uri, "file://")
			docDir := filepath.Dir(docPath)
			depErrs := nbuild.ValidateDependencyImports(prog, docDir)
			for _, u := range depErrs {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityError,
					Source:   "nolang-lint",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			// Validate export symbols in lib.no
			exportErrs := nbuild.ValidateExportSymbols(prog, docPath)
			for _, u := range exportErrs {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityError,
					Source:   "nolang-lint",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			stringConcatHints := nbuild.ValidateStringConcat(prog)
			for _, u := range stringConcatHints {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityHint,
					Source:   "nolang-lint",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}

			funcArgErrs := nbuild.ValidateFuncArgs(prog, docDir)
			for _, u := range funcArgErrs {
				diagnostic := Diagnostic{
					Range: Range{
						Start: Position{Line: uint32(u.Line - 1), Character: uint32(u.Column - 1)},
						End:   Position{Line: uint32(u.Line - 1), Character: uint32(u.Column)},
					},
					Severity: DiagnosticSeverityError,
					Source:   "nolang-type-checker",
					Message:  u.Message,
				}
				diagnostics = append(diagnostics, diagnostic)
			}
		}
	}

	if err := s.publishDiagnostics(uri, diagnostics); err != nil {
		log.Printf("Error publishing diagnostics: %v", err)
	}
}

func (s *Server) parseErrorToDiagnostic(errMsg string) Diagnostic {
	var diagnostic Diagnostic
	diagnostic.Source = "nolang-parser"
	fmt.Sscanf(errMsg, "line %d, column %d:", &diagnostic.Range.Start.Line, &diagnostic.Range.Start.Character)
	diagnostic.Range.End = diagnostic.Range.Start
	diagnostic.Range.End.Character += 1
	diagnostic.Severity = DiagnosticSeverityError
	diagnostic.Message = errMsg
	return diagnostic
}

func (s *Server) handleTextDocumentDidChange(params DidChangeTextDocumentParams) (interface{}, error) {
	_, err := s.documents.UpdateDocument(params.TextDocument.URI, params.ContentChanges, params.TextDocument.Version)
	if err != nil {
		return nil, err
	}

	go func() {
		ast, parseErrors, err := s.documents.ParseDocument(params.TextDocument.URI)
		if err != nil {
			log.Printf("Error parsing document: %v", err)
		}

		s.publishDocumentDiagnostics(params.TextDocument.URI, parseErrors, ast)
	}()

	return nil, nil
}

func (s *Server) handleTextDocumentDidClose(params DidCloseTextDocumentParams) (interface{}, error) {
	s.documents.RemoveDocument(params.TextDocument.URI)
	s.publishDocumentDiagnostics(params.TextDocument.URI, nil, nil)
	return nil, nil
}

func (s *Server) handleTextDocumentCompletion(params TextDocumentPositionParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return CompletionList{IsIncomplete: false, Items: []CompletionItem{}}, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		index = NewSymbolIndex(params.TextDocument.URI, 0)
		index.AddBuiltinSymbols()
	}

	provider := NewCompletionProvider(doc, index)

	triggerChar := ""
	if params.Position.Character > 0 {
		lines := getLines(doc.Text)
		if int(params.Position.Line) < len(lines) {
			line := lines[params.Position.Line]
			if int(params.Position.Character-1) < len(line) {
				triggerChar = string(line[params.Position.Character-1])
			}
		}
	}

	items := provider.GetCompletions(params.Position, triggerChar)

	return CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func (s *Server) handleCompletionItemResolve(item CompletionItem) (interface{}, error) {
	// Resolve is handled inline now
	return item, nil
}

func (s *Server) handleTextDocumentHover(params TextDocumentPositionParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return nil, nil
	}

	provider := NewHoverProvider(doc, index)
	hover, found := provider.GetHover(params.Position)
	if !found {
		return nil, nil
	}

	return hover, nil
}

func (s *Server) handleTextDocumentDefinition(params TextDocumentPositionParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return nil, nil
	}

	provider := NewDefinitionProvider(doc, index)
	location, found := provider.GetDefinition(params.Position)
	if !found {
		return nil, nil
	}

	return location, nil
}

func (s *Server) handleTextDocumentReferences(params ReferenceParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return []Location{}, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return []Location{}, nil
	}

	provider := NewReferencesProvider(doc, index)
	locations := provider.GetReferences(params.Position, params.Context.IncludeDeclaration)

	return locations, nil
}

func (s *Server) handleTextDocumentDocumentHighlight(params TextDocumentPositionParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return []DocumentHighlight{}, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return []DocumentHighlight{}, nil
	}

	provider := NewDocumentHighlightProvider(doc, index)
	return provider.GetHighlights(params.Position), nil
}

func (s *Server) handleTextDocumentSymbol(params DocumentSymbolParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return []DocumentSymbol{}, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return []DocumentSymbol{}, nil
	}

	provider := NewSymbolProvider(doc, index)
	symbols := provider.GetSymbols()

	return symbols, nil
}

func (s *Server) handleWorkspaceSymbol(params WorkspaceSymbolParams) (interface{}, error) {
	var symbols []SymbolInformation
	documents := s.documents.GetAllDocuments()

	for uri := range documents {
		index := s.documents.GetIndex(uri)
		if index == nil {
			continue
		}

		entries := index.Search(params.Query)
		for _, e := range entries {
			symbols = append(symbols, SymbolInformation{
				Name:     e.Name,
				Kind:     e.Kind,
				Location: e.Location,
			})
		}
	}

	sort.Slice(symbols, func(i, j int) bool {
		aLower := strings.ToLower(symbols[i].Name)
		bLower := strings.ToLower(symbols[j].Name)
		aStartsWith := strings.HasPrefix(aLower, strings.ToLower(params.Query))
		bStartsWith := strings.HasPrefix(bLower, strings.ToLower(params.Query))
		if aStartsWith && !bStartsWith {
			return true
		}
		if !aStartsWith && bStartsWith {
			return false
		}
		return len(aLower) < len(bLower)
	})

	return symbols, nil
}

func (s *Server) handleTextDocumentSignatureHelp(params TextDocumentPositionParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return nil, nil
	}

	provider := NewSignatureHelpProvider(doc, index)
	result, found := provider.GetSignatureHelp(params.Position)
	if !found {
		return nil, nil
	}
	return result, nil
}

func (s *Server) handleTextDocumentRename(params RenameParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, nil
	}

	index := s.documents.GetIndex(params.TextDocument.URI)
	if index == nil {
		return nil, nil
	}

	provider := NewRenameProvider(doc, index)
	edit, found := provider.GetRenameEdits(params.Position, params.NewName)
	if !found {
		return nil, nil
	}

	return edit, nil
}

func (s *Server) handleTextDocumentFoldingRange(params FoldingRangeParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return []FoldingRange{}, nil
	}

	provider := NewFoldingRangeProvider(doc)
	return provider.GetFoldingRanges(), nil
}

func (s *Server) handleTextDocumentSemanticTokensFull(params SemanticTokensParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, nil
	}

	provider := NewSemanticTokensProvider(doc)
	return provider.GetSemanticTokens(), nil
}

func getWordBeforePosition(text string, position Position) string {
	lines := getLines(text)
	if int(position.Line) >= len(lines) {
		return ""
	}
	line := lines[position.Line]
	if int(position.Character) < 1 {
		return ""
	}

	end := position.Character - 1
	// Skip whitespace
	for end > 0 && line[end] == ' ' {
		end--
	}
	if end == 0 {
		return ""
	}

	start := end
	for start > 0 {
		c := line[start-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			start--
		} else {
			break
		}
	}

	if start > end {
		return ""
	}
	return line[start : end+1]
}

func formatNolangCode(content string) string {
	return nolangfmt.Format(content)
}

func computeTextEdits(original, formatted string) []TextEdit {
	if original == formatted {
		return nil
	}
	oLines := strings.Split(original, "\n")
	lastLine := uint32(len(oLines) - 1)
	lastChar := uint32(0)
	if lastLine >= 0 && len(oLines) > 0 {
		lastChar = uint32(len(oLines[lastLine]))
	}
	return []TextEdit{
		{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: lastLine, Character: lastChar},
			},
			NewText: formatted,
		},
	}
}

func (s *Server) handleTextDocumentFormatting(params DocumentFormattingParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	formatted := formatNolangCode(doc.Text)
	edits := computeTextEdits(doc.Text, formatted)
	if edits == nil {
		return []TextEdit{}, nil
	}
	return edits, nil
}

func (s *Server) handleTextDocumentWillSaveWaitUntil(params WillSaveWaitUntilParams) (interface{}, error) {
	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	formatted := formatNolangCode(doc.Text)
	edits := computeTextEdits(doc.Text, formatted)
	if edits == nil {
		return []TextEdit{}, nil
	}
	return edits, nil
}

func (s *Server) Handle(method string, params json.RawMessage) (interface{}, error) {
	switch method {
	case "initialize":
		var p InitializeParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleInitialize(p)
	case "shutdown":
		return s.handleShutdown()
	case "exit":
		return s.handleExit()
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentDidOpen(p)
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentDidChange(p)
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentDidClose(p)
	case "textDocument/completion":
		var p TextDocumentPositionParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentCompletion(p)
	case "completionItem/resolve":
		var item CompletionItem
		if params != nil {
			if err := json.Unmarshal(params, &item); err != nil {
				return nil, err
			}
		}
		return s.handleCompletionItemResolve(item)
	case "textDocument/hover":
		var p TextDocumentPositionParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentHover(p)
	case "textDocument/definition":
		var p TextDocumentPositionParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentDefinition(p)
	case "textDocument/references":
		var p ReferenceParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentReferences(p)
	case "textDocument/documentHighlight":
		var p TextDocumentPositionParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentDocumentHighlight(p)
	case "textDocument/documentSymbol":
		var p DocumentSymbolParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentSymbol(p)
	case "textDocument/formatting":
		var p DocumentFormattingParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentFormatting(p)
	case "textDocument/willSaveWaitUntil":
		var wp WillSaveWaitUntilParams
		if params != nil {
			if err := json.Unmarshal(params, &wp); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentWillSaveWaitUntil(wp)
	case "textDocument/signatureHelp":
		var p TextDocumentPositionParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentSignatureHelp(p)
	case "textDocument/rename":
		var p RenameParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentRename(p)
	case "textDocument/foldingRange":
		var p FoldingRangeParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentFoldingRange(p)
	case "textDocument/semanticTokens/full":
		var p SemanticTokensParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleTextDocumentSemanticTokensFull(p)
	case "workspace/symbol":
		var p WorkspaceSymbolParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
		}
		return s.handleWorkspaceSymbol(p)
	default:
		return nil, fmt.Errorf("method not found: %s", method)
	}
}

func (s *Server) GetDocumentManager() *DocumentManager {
	return s.documents
}

func RunServer(ctx context.Context, server *Server) error {
	stream := jsonrpc2.NewStream(stdrwc{})
	conn := jsonrpc2.NewConn(stream)
	server.conn = conn

	log.SetOutput(os.Stderr)
	log.Println("Nolang LSP Server starting...")

	handler := func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		result, err := server.Handle(req.Method(), json.RawMessage(req.Params()))
		return reply(ctx, result, err)
	}

	conn.Go(ctx, handler)
	<-conn.Done()
	return conn.Err()
}

type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdrwc) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdrwc) Close() error                { return nil }
