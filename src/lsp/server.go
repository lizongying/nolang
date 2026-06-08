package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	nolangfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/lexer"
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
				TriggerCharacters: []string{".", ":", "="},
			},
			HoverProvider:              true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			DocumentSymbolProvider:     true,
			WorkspaceSymbolProvider:    true,
			DocumentFormattingProvider: true,
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
	log.Printf("Initialize request received with params: %+v", params)

	s.mu.Lock()
	s.shutdown = false
	s.mu.Unlock()

	result := InitializeResult{
		Capabilities: s.capabilities,
		ServerInfo: &ServerInfo{
			Name:    "nolang-lsp",
			Version: "0.1.0",
		},
	}

	log.Printf("Returning capabilities: %+v", result.Capabilities)
	return result, nil
}

func (s *Server) handleShutdown() (interface{}, error) {
	log.Println("Shutdown request received")
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	return nil, nil
}

func (s *Server) handleExit() (interface{}, error) {
	log.Println("Exit request received")
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
	log.Printf("TextDocumentDidOpen: %+v", params)

	_, err := s.documents.OpenDocument(params.TextDocument.URI, params.TextDocument.Text)
	if err != nil {
		return nil, err
	}

	_, parseErrors, err := s.documents.ParseDocument(params.TextDocument.URI)
	if err != nil {
		log.Printf("Error parsing document: %v", err)
	}

	s.publishDocumentDiagnostics(params.TextDocument.URI, parseErrors)

	return nil, nil
}

func (s *Server) publishDocumentDiagnostics(uri string, parseErrors []string) {
	var diagnostics []Diagnostic

	for _, errMsg := range parseErrors {
		diagnostic := s.parseErrorToDiagnostic(errMsg)
		diagnostics = append(diagnostics, diagnostic)
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
	log.Printf("TextDocumentDidChange: %+v", params)

	_, err := s.documents.UpdateDocument(params.TextDocument.URI, params.ContentChanges, params.TextDocument.Version)
	if err != nil {
		return nil, err
	}

	_, parseErrors, err := s.documents.ParseDocument(params.TextDocument.URI)
	if err != nil {
		log.Printf("Error parsing document: %v", err)
	}

	s.publishDocumentDiagnostics(params.TextDocument.URI, parseErrors)

	return nil, nil
}

func (s *Server) handleTextDocumentDidClose(params DidCloseTextDocumentParams) (interface{}, error) {
	log.Printf("TextDocumentDidClose: %+v", params)

	err := s.documents.CloseDocument(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	err = s.documents.RemoveDocument(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	s.publishDocumentDiagnostics(params.TextDocument.URI, nil)

	return nil, nil
}

func (s *Server) handleTextDocumentCompletion(params TextDocumentPositionParams) (interface{}, error) {
	log.Printf("TextDocumentCompletion: %+v", params)

	uri := params.TextDocument.URI
	doc, err := s.documents.GetDocument(uri)
	if err != nil {
		return CompletionList{
			IsIncomplete: false,
			Items:        []CompletionItem{},
		}, nil
	}

	var program *parser.Program
	if doc.AST != nil {
		program = doc.AST
	} else {
		l := lexer.New(doc.Text)
		p := parser.New(l)
		program = p.ParseProgram()
	}

	provider := NewCompletionProvider(doc, program)

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
	log.Printf("CompletionItemResolve: %+v", item)

	if item.Kind == CompletionItemKindFunction {
		item.Documentation = "Function defined in the current scope"
		item.Command = nil
	} else if item.Kind == CompletionItemKindVariable {
		item.Documentation = "Variable defined in the current scope"
	} else if item.Kind == CompletionItemKindKeyword {
		item.Documentation = "Nolang keyword"
	}

	return item, nil
}

func (s *Server) handleTextDocumentHover(params TextDocumentPositionParams) (interface{}, error) {
	log.Printf("TextDocumentHover: %+v", params)

	uri := params.TextDocument.URI
	doc, err := s.documents.GetDocument(uri)
	if err != nil {
		return nil, nil
	}

	var program *parser.Program
	if doc.AST != nil {
		program = doc.AST
	} else {
		l := lexer.New(doc.Text)
		p := parser.New(l)
		program = p.ParseProgram()
	}

	provider := NewHoverProvider(doc, program)
	hover, found := provider.GetHover(params.Position)
	if !found {
		return nil, nil
	}

	return hover, nil
}

func (s *Server) handleTextDocumentDefinition(params TextDocumentPositionParams) (interface{}, error) {
	log.Printf("TextDocumentDefinition: %+v", params)

	uri := params.TextDocument.URI
	doc, err := s.documents.GetDocument(uri)
	if err != nil {
		return nil, nil
	}

	var program *parser.Program
	if doc.AST != nil {
		program = doc.AST
	} else {
		l := lexer.New(doc.Text)
		p := parser.New(l)
		program = p.ParseProgram()
	}

	provider := NewDefinitionProvider(doc, program)
	location, found := provider.GetDefinition(params.Position)
	if !found {
		return nil, nil
	}

	return location, nil
}

func (s *Server) handleTextDocumentReferences(params ReferenceParams) (interface{}, error) {
	log.Printf("TextDocumentReferences: %+v", params)

	uri := params.TextDocument.URI
	doc, err := s.documents.GetDocument(uri)
	if err != nil {
		return []Location{}, nil
	}

	var program *parser.Program
	if doc.AST != nil {
		program = doc.AST
	} else {
		l := lexer.New(doc.Text)
		p := parser.New(l)
		program = p.ParseProgram()
	}

	provider := NewReferencesProvider(doc, program)
	locations := provider.GetReferences(params.Position, params.Context.IncludeDeclaration)

	return locations, nil
}

func (s *Server) handleTextDocumentSymbol(params DocumentSymbolParams) (interface{}, error) {
	log.Printf("TextDocumentSymbol: %+v", params)

	uri := params.TextDocument.URI
	doc, err := s.documents.GetDocument(uri)
	if err != nil {
		return []DocumentSymbol{}, nil
	}

	var program *parser.Program
	if doc.AST != nil {
		program = doc.AST
	} else {
		l := lexer.New(doc.Text)
		p := parser.New(l)
		program = p.ParseProgram()
	}

	provider := NewSymbolProvider(doc, program)
	symbols := provider.GetSymbols()

	return symbols, nil
}

func (s *Server) handleWorkspaceSymbol(params WorkspaceSymbolParams) (interface{}, error) {
	log.Printf("WorkspaceSymbol: %+v", params)

	var symbols []SymbolInformation
	documents := s.documents.GetAllDocuments()

	for uri, doc := range documents {
		var program *parser.Program
		if doc.AST != nil {
			program = doc.AST
		} else {
			l := lexer.New(doc.Text)
			p := parser.New(l)
			program = p.ParseProgram()
		}

		provider := NewSymbolProvider(doc, program)
		docSymbols := provider.GetSymbols()

		for _, sym := range docSymbols {
			s.collectWorkspaceSymbols(sym, uri, params.Query, "", &symbols)
		}
	}

	s.sortByRelevance(&symbols, params.Query)

	return symbols, nil
}

func (s *Server) collectWorkspaceSymbols(docSym DocumentSymbol, uri, query, containerName string, symbols *[]SymbolInformation) {
	lowerName := strings.ToLower(docSym.Name)
	lowerQuery := strings.ToLower(query)

	if strings.Contains(lowerName, lowerQuery) {
		*symbols = append(*symbols, SymbolInformation{
			Name: docSym.Name,
			Kind: docSym.Kind,
			Location: Location{
				URI:   uri,
				Range: docSym.SelectionRange,
			},
			ContainerName: containerName,
		})
	}

	for _, child := range docSym.Children {
		s.collectWorkspaceSymbols(child, uri, query, docSym.Name, symbols)
	}
}

func (s *Server) sortByRelevance(symbols *[]SymbolInformation, query string) {
	lowerQuery := strings.ToLower(query)

	sort.Slice(*symbols, func(i, j int) bool {
		a := (*symbols)[i]
		b := (*symbols)[j]

		aLower := strings.ToLower(a.Name)
		bLower := strings.ToLower(b.Name)

		aStartsWith := strings.HasPrefix(aLower, lowerQuery)
		bStartsWith := strings.HasPrefix(bLower, lowerQuery)

		if aStartsWith && !bStartsWith {
			return true
		}
		if !aStartsWith && bStartsWith {
			return false
		}

		if len(aLower) != len(bLower) {
			return len(aLower) < len(bLower)
		}

		return aLower < bLower
	})
}

// formatNolangCode 格式化 Nolang 程式碼
func formatNolangCode(content string) string {
	return nolangfmt.Format(content)
}

func (s *Server) handleTextDocumentFormatting(params DocumentFormattingParams) (interface{}, error) {
	log.Printf("TextDocumentFormatting: %+v", params)

	doc, err := s.documents.GetDocument(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	// 獲取文檔內容
	content := doc.Text

	// 實現格式化邏輯
	formatted := formatNolangCode(content)

	// 如果內容沒變化，返回空陣列
	if formatted == content {
		return []TextEdit{}, nil
	}

	// 計算整個文檔的範圍
	lines := strings.Split(content, "\n")
	lastLine := len(lines) - 1
	lastChar := 0
	if lastLine >= 0 {
		lastChar = len(lines[lastLine])
	}

	return []TextEdit{
		{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: uint32(lastLine), Character: uint32(lastChar)},
			},
			NewText: formatted,
		},
	}, nil
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
		method := req.Method()
		params := req.Params()
		log.Printf("Received request: %s", method)

		result, err := server.Handle(method, json.RawMessage(params))
		return reply(ctx, result, err)
	}

	conn.Go(ctx, handler)
	<-conn.Done()
	return conn.Err()
}

// stdrwc 實現 io.ReadWriteCloser 適配 os.Stdin/Stdout
type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdrwc) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdrwc) Close() error                { return nil }
