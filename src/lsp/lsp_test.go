package lsp

import (
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.documents == nil {
		t.Error("documents is nil")
	}
	if s.shutdown {
		t.Error("shutdown should be false initially")
	}
}

func TestServerCapabilities(t *testing.T) {
	s := NewServer()

	if s.capabilities.TextDocumentSync == nil {
		t.Fatal("TextDocumentSync is nil")
	}
	if !s.capabilities.TextDocumentSync.OpenClose {
		t.Error("TextDocumentSync.OpenClose should be true")
	}
	if s.capabilities.TextDocumentSync.Change != TextDocumentSyncKindFull {
		t.Error("TextDocumentSync.Change should be Full")
	}

	if s.capabilities.CompletionProvider == nil {
		t.Fatal("CompletionProvider is nil")
	}
	if !s.capabilities.CompletionProvider.ResolveProvider {
		t.Error("CompletionProvider.ResolveProvider should be true")
	}

	if !s.capabilities.HoverProvider {
		t.Error("HoverProvider should be true")
	}
	if !s.capabilities.DefinitionProvider {
		t.Error("DefinitionProvider should be true")
	}
	if !s.capabilities.ReferencesProvider {
		t.Error("ReferencesProvider should be true")
	}
	if !s.capabilities.DocumentSymbolProvider {
		t.Error("DocumentSymbolProvider should be true")
	}
	if !s.capabilities.WorkspaceSymbolProvider {
		t.Error("WorkspaceSymbolProvider should be true")
	}
	if !s.capabilities.DocumentFormattingProvider {
		t.Error("DocumentFormattingProvider should be true")
	}
}

func TestHandleInitialize(t *testing.T) {
	s := NewServer()

	params := InitializeParams{
		ProcessID: nil,
		RootURI:   strPtr("file:///test"),
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Completion: &CompletionCapabilities{
					DynamicRegistration: true,
				},
			},
		},
	}

	result, err := s.handleInitialize(params)
	if err != nil {
		t.Fatalf("handleInitialize failed: %v", err)
	}
	if result == nil {
		t.Fatal("handleInitialize returned nil")
	}

	initResult, ok := result.(InitializeResult)
	if !ok {
		t.Fatal("result is not InitializeResult")
	}
	if initResult.ServerInfo == nil {
		t.Fatal("ServerInfo is nil")
	}
	if initResult.ServerInfo.Name != "nolang-lsp" {
		t.Errorf("expected server name 'nolang-lsp', got %q", initResult.ServerInfo.Name)
	}
	if initResult.ServerInfo.Version != "0.1.0" {
		t.Errorf("expected server version '0.1.0', got %q", initResult.ServerInfo.Version)
	}
}

func TestHandleShutdown(t *testing.T) {
	s := NewServer()

	if s.shutdown {
		t.Error("shutdown should be false initially")
	}

	_, err := s.handleShutdown()
	if err != nil {
		t.Fatalf("handleShutdown failed: %v", err)
	}

	if !s.shutdown {
		t.Error("shutdown should be true after handleShutdown")
	}
}

func TestGetDocumentManager(t *testing.T) {
	s := NewServer()
	dm := s.GetDocumentManager()
	if dm == nil {
		t.Fatal("GetDocumentManager returned nil")
	}
}

func TestGetLines(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{}},
		{"hello", []string{"hello"}},
		{"hello\nworld", []string{"hello", "world"}},
		{"line1\nline2\nline3", []string{"line1", "line2", "line3"}},
		{"a\nb\n", []string{"a", "b"}},
	}

	for _, tt := range tests {
		result := getLines(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("getLines(%q): expected %d lines, got %d", tt.input, len(tt.expected), len(result))
			continue
		}
		for i, line := range result {
			if line != tt.expected[i] {
				t.Errorf("getLines(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], line)
			}
		}
	}
}

func TestJsonrpc2Error(t *testing.T) {
	err := &jsonrpc2Error{
		Code:    -32601,
		Message: "method not found",
	}
	if err.Error() != "method not found" {
		t.Errorf("expected 'method not found', got %q", err.Error())
	}
}

func TestPosition(t *testing.T) {
	p := Position{Line: 10, Character: 5}
	if p.Line != 10 {
		t.Errorf("expected Line 10, got %d", p.Line)
	}
	if p.Character != 5 {
		t.Errorf("expected Character 5, got %d", p.Character)
	}
}

func TestRange(t *testing.T) {
	r := Range{
		Start: Position{Line: 1, Character: 2},
		End:   Position{Line: 1, Character: 5},
	}
	if r.Start.Line != 1 {
		t.Errorf("expected Start.Line 1, got %d", r.Start.Line)
	}
	if r.End.Character != 5 {
		t.Errorf("expected End.Character 5, got %d", r.End.Character)
	}
}

func TestLocation(t *testing.T) {
	l := Location{
		URI: "file:///test.no",
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: 5},
		},
	}
	if l.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", l.URI)
	}
}

func TestTextDocumentIdentifier(t *testing.T) {
	td := TextDocumentIdentifier{URI: "file:///test.no"}
	if td.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", td.URI)
	}
}

func TestTextDocumentItem(t *testing.T) {
	td := TextDocumentItem{
		URI:        "file:///test.no",
		LanguageID: "nolang",
		Version:    1,
		Text:       "x = 10",
	}
	if td.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", td.URI)
	}
	if td.LanguageID != "nolang" {
		t.Errorf("expected LanguageID 'nolang', got %q", td.LanguageID)
	}
	if td.Version != 1 {
		t.Errorf("expected Version 1, got %d", td.Version)
	}
	if td.Text != "x = 10" {
		t.Errorf("expected Text 'x = 10', got %q", td.Text)
	}
}

func TestTextDocumentPositionParams(t *testing.T) {
	tdpp := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///test.no"},
		Position:     Position{Line: 5, Character: 10},
	}
	if tdpp.TextDocument.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", tdpp.TextDocument.URI)
	}
	if tdpp.Position.Line != 5 {
		t.Errorf("expected Position.Line 5, got %d", tdpp.Position.Line)
	}
}

func TestCompletionList(t *testing.T) {
	cl := CompletionList{
		IsIncomplete: false,
		Items: []CompletionItem{
			{Label: "test", Kind: CompletionItemKindKeyword},
		},
	}
	if cl.IsIncomplete {
		t.Error("expected IsIncomplete false")
	}
	if len(cl.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(cl.Items))
	}
	if cl.Items[0].Label != "test" {
		t.Errorf("expected label 'test', got %q", cl.Items[0].Label)
	}
}

func TestCompletionItem(t *testing.T) {
	item := CompletionItem{
		Label:            "func",
		Kind:             CompletionItemKindKeyword,
		Detail:           "function definition",
		InsertText:       "func ${1:name}()",
		InsertTextFormat: InsertTextFormatSnippet,
		Documentation:    "A function definition",
		Deprecated:       false,
		Preselect:        false,
		SortText:         "0001",
		FilterText:       "func",
		CommitCharacters: []string{".", ":"},
	}

	if item.Label != "func" {
		t.Errorf("expected label 'func', got %q", item.Label)
	}
	if item.Kind != CompletionItemKindKeyword {
		t.Errorf("expected Kind %d, got %d", CompletionItemKindKeyword, item.Kind)
	}
	if item.InsertTextFormat != InsertTextFormatSnippet {
		t.Errorf("expected InsertTextFormat %d, got %d", InsertTextFormatSnippet, item.InsertTextFormat)
	}
}

func TestHover(t *testing.T) {
	h := Hover{
		Contents: MarkupContent{
			Kind:  MarkupKindMarkdown,
			Value: "**test**",
		},
		Range: &Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: 4},
		},
	}

	contents, ok := h.Contents.(MarkupContent)
	if !ok {
		t.Fatal("Contents is not MarkupContent")
	}
	if contents.Kind != MarkupKindMarkdown {
		t.Errorf("expected Kind %q, got %q", MarkupKindMarkdown, contents.Kind)
	}
	if h.Range == nil {
		t.Fatal("Range is nil")
	}
}

func TestDocumentSymbol(t *testing.T) {
	ds := DocumentSymbol{
		Name: "main",
		Kind: SymbolKindFunction,
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 5, Character: 1},
		},
		SelectionRange: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: 4},
		},
		Children: []DocumentSymbol{},
	}

	if ds.Name != "main" {
		t.Errorf("expected Name 'main', got %q", ds.Name)
	}
	if ds.Kind != SymbolKindFunction {
		t.Errorf("expected Kind %d, got %d", SymbolKindFunction, ds.Kind)
	}
	if len(ds.Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(ds.Children))
	}
}

func TestServerInfo(t *testing.T) {
	si := ServerInfo{
		Name:    "nolang-lsp",
		Version: "1.0.0",
	}
	if si.Name != "nolang-lsp" {
		t.Errorf("expected Name 'nolang-lsp', got %q", si.Name)
	}
	if si.Version != "1.0.0" {
		t.Errorf("expected Version '1.0.0', got %q", si.Version)
	}
}

func TestProtocolConstants(t *testing.T) {
	if ProtocolVersion != 3 {
		t.Errorf("expected ProtocolVersion 3, got %d", ProtocolVersion)
	}

	if MessageTypeError != 1 {
		t.Errorf("expected MessageTypeError 1, got %d", MessageTypeError)
	}
	if MessageTypeWarning != 2 {
		t.Errorf("expected MessageTypeWarning 2, got %d", MessageTypeWarning)
	}
	if MessageTypeInfo != 3 {
		t.Errorf("expected MessageTypeInfo 3, got %d", MessageTypeInfo)
	}
	if MessageTypeLog != 4 {
		t.Errorf("expected MessageTypeLog 4, got %d", MessageTypeLog)
	}

	if TextDocumentSyncKindNone != 0 {
		t.Errorf("expected TextDocumentSyncKindNone 0, got %d", TextDocumentSyncKindNone)
	}
	if TextDocumentSyncKindFull != 1 {
		t.Errorf("expected TextDocumentSyncKindFull 1, got %d", TextDocumentSyncKindFull)
	}
	if TextDocumentSyncKindIncremental != 2 {
		t.Errorf("expected TextDocumentSyncKindIncremental 2, got %d", TextDocumentSyncKindIncremental)
	}

	if InsertTextFormatPlainText != 1 {
		t.Errorf("expected InsertTextFormatPlainText 1, got %d", InsertTextFormatPlainText)
	}
	if InsertTextFormatSnippet != 2 {
		t.Errorf("expected InsertTextFormatSnippet 2, got %d", InsertTextFormatSnippet)
	}
}

func TestCompletionItemKindConstants(t *testing.T) {
	if CompletionItemKindText != 1 {
		t.Errorf("expected CompletionItemKindText 1, got %d", CompletionItemKindText)
	}
	if CompletionItemKindFunction != 3 {
		t.Errorf("expected CompletionItemKindFunction 3, got %d", CompletionItemKindFunction)
	}
	if CompletionItemKindVariable != 6 {
		t.Errorf("expected CompletionItemKindVariable 6, got %d", CompletionItemKindVariable)
	}
	if CompletionItemKindKeyword != 14 {
		t.Errorf("expected CompletionItemKindKeyword 14, got %d", CompletionItemKindKeyword)
	}
}

func TestSymbolKindConstants(t *testing.T) {
	if SymbolKindFunction != 12 {
		t.Errorf("expected SymbolKindFunction 12, got %d", SymbolKindFunction)
	}
	if SymbolKindVariable != 13 {
		t.Errorf("expected SymbolKindVariable 13, got %d", SymbolKindVariable)
	}
	if SymbolKindParameter != 27 {
		t.Errorf("expected SymbolKindParameter 27, got %d", SymbolKindParameter)
	}
}

func TestDiagnosticSeverityConstants(t *testing.T) {
	if DiagnosticSeverityError != 1 {
		t.Errorf("expected DiagnosticSeverityError 1, got %d", DiagnosticSeverityError)
	}
	if DiagnosticSeverityWarning != 2 {
		t.Errorf("expected DiagnosticSeverityWarning 2, got %d", DiagnosticSeverityWarning)
	}
	if DiagnosticSeverityInfo != 3 {
		t.Errorf("expected DiagnosticSeverityInfo 3, got %d", DiagnosticSeverityInfo)
	}
	if DiagnosticSeverityHint != 4 {
		t.Errorf("expected DiagnosticSeverityHint 4, got %d", DiagnosticSeverityHint)
	}
}

func TestFileChangeTypeConstants(t *testing.T) {
	if FileChangeTypeCreated != 1 {
		t.Errorf("expected FileChangeTypeCreated 1, got %d", FileChangeTypeCreated)
	}
	if FileChangeTypeChanged != 2 {
		t.Errorf("expected FileChangeTypeChanged 2, got %d", FileChangeTypeChanged)
	}
	if FileChangeTypeDeleted != 3 {
		t.Errorf("expected FileChangeTypeDeleted 3, got %d", FileChangeTypeDeleted)
	}
}

func TestDocumentHighlightKindConstants(t *testing.T) {
	if DocumentHighlightKindText != 1 {
		t.Errorf("expected DocumentHighlightKindText 1, got %d", DocumentHighlightKindText)
	}
	if DocumentHighlightKindRead != 2 {
		t.Errorf("expected DocumentHighlightKindRead 2, got %d", DocumentHighlightKindRead)
	}
	if DocumentHighlightKindWrite != 3 {
		t.Errorf("expected DocumentHighlightKindWrite 3, got %d", DocumentHighlightKindWrite)
	}
}

func TestDiagnosticTagConstants(t *testing.T) {
	if DiagnosticTagUnnecessary != 1 {
		t.Errorf("expected DiagnosticTagUnnecessary 1, got %d", DiagnosticTagUnnecessary)
	}
	if DiagnosticTagDeprecated != 2 {
		t.Errorf("expected DiagnosticTagDeprecated 2, got %d", DiagnosticTagDeprecated)
	}
}

func TestMarkupContent(t *testing.T) {
	mc := MarkupContent{
		Kind:  MarkupKindMarkdown,
		Value: "**bold**",
	}
	if mc.Kind != "markdown" {
		t.Errorf("expected Kind 'markdown', got %q", mc.Kind)
	}
	if mc.Value != "**bold**" {
		t.Errorf("expected Value '**bold**', got %q", mc.Value)
	}
}

func TestDocumentURI(t *testing.T) {
	var uri DocumentURI = "file:///test.no"
	if uri.URI() != "file:///test.no" {
		t.Errorf("expected 'file:///test.no', got %q", uri.URI())
	}
}

func TestReferenceParams(t *testing.T) {
	params := NewReferenceParams(
		TextDocumentIdentifier{URI: "file:///test.no"},
		Position{Line: 5, Character: 10},
		true,
	)
	if params.TextDocument.URI != "file:///test.no" {
		t.Errorf("expected URI 'file:///test.no', got %q", params.TextDocument.URI)
	}
	if params.Position.Line != 5 {
		t.Errorf("expected Position.Line 5, got %d", params.Position.Line)
	}
	if params.Context.IncludeDeclaration != true {
		t.Error("expected IncludeDeclaration true")
	}
}

func TestHandleTextDocumentFormatting_MethodDefinition(t *testing.T) {
	s := NewServer()

	// 使用可正確解析的輸入測試增量格式化
	// 僅第一行有變化（多餘空格），後續行保持不變
	input := strings.TrimSpace(`
x    =   5
y = x + 1
z = 3
	`)
	expected := strings.TrimSpace(`
x = 5
y = x + 1
z = 3
	`)

	uri := "file:///test_format.no"

	_, err := s.documents.OpenDocument(uri, input)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	result, err := s.handleTextDocumentFormatting(DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options:      &FormattingOptions{TabSize: 4, InsertSpaces: true},
	})
	if err != nil {
		t.Fatalf("handleTextDocumentFormatting failed: %v", err)
	}

	edits, ok := result.([]TextEdit)
	if !ok {
		t.Fatal("result is not []TextEdit")
	}
	if len(edits) == 0 {
		t.Fatal("expected at least one TextEdit, got none (formatted text should differ)")
	}

	// 驗證 edits 只包含有變化的行（不應替換整個文件）
	// 只有第一行 x    =   5 → x = 5 發生變化
	// 檢查 edit 範圍只覆蓋第一行
	if edits[0].Range.Start.Line != 0 || edits[0].Range.End.Line != 1 || edits[0].NewText != "x = 5\n" {
		t.Errorf("unexpected edit: Range {%d,%d}-{%d,%d}, NewText=%q",
			edits[0].Range.Start.Line, edits[0].Range.Start.Character,
			edits[0].Range.End.Line, edits[0].Range.End.Character,
			edits[0].NewText)
	}

	// 驗證通過應用 edits 重建的結果
	resultText := applyTextEdits(input, edits)
	if resultText != expected {
		t.Errorf("applyTextEdits result = %q, want %q", resultText, expected)
	}
}

func TestHandleTextDocumentFormatting_ParseErrorSafety(t *testing.T) {
	s := NewServer()

	// 當格式化遇到解析錯誤時，不應修改代碼
	input := strings.TrimSpace(`
str.len: () (n    i64)      {
    n = .len
}
	`)

	uri := "file:///test_method.no"

	_, err := s.documents.OpenDocument(uri, input)
	if err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}

	result, err := s.handleTextDocumentFormatting(DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options:      &FormattingOptions{TabSize: 4, InsertSpaces: true},
	})
	if err != nil {
		t.Fatalf("handleTextDocumentFormatting failed: %v", err)
	}

	edits, ok := result.([]TextEdit)
	if !ok {
		t.Fatal("result is not []TextEdit")
	}

	// 解析錯誤時應返回空 edits（不修改代碼）
	if len(edits) != 0 {
		t.Errorf("expected 0 edits on parse error, got %d edits", len(edits))
	}
}

// applyTextEdits 將 TextEdit 列表按倒序（從文件尾到文件頭）應用到原始文字
// 使用倒序確保位置偏移不受前面編輯的影響
func applyTextEdits(original string, edits []TextEdit) string {
	oLines := strings.Split(original, "\n")

	// 按起始行號倒序排序，從文件尾開始應用
	sorted := make([]TextEdit, len(edits))
	copy(sorted, edits)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].Range.Start.Line < sorted[j].Range.Start.Line ||
				(sorted[i].Range.Start.Line == sorted[j].Range.Start.Line &&
					sorted[i].Range.Start.Character < sorted[j].Range.Start.Character) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	for _, edit := range sorted {
		startLine := int(edit.Range.Start.Line)
		startChar := int(edit.Range.Start.Character)
		endLine := int(edit.Range.End.Line)
		endChar := int(edit.Range.End.Character)

		// 取出 edit Range 前的部分
		var sb strings.Builder
		for i := 0; i < startLine; i++ {
			sb.WriteString(oLines[i])
			sb.WriteString("\n")
		}
		sb.WriteString(oLines[startLine][:startChar])

		// 插入新文字
		sb.WriteString(edit.NewText)

		// 加入 edit Range 後的部分
		if endChar < len(oLines[endLine]) {
			sb.WriteString(oLines[endLine][endChar:])
		}
		for i := endLine + 1; i < len(oLines); i++ {
			sb.WriteString("\n")
			sb.WriteString(oLines[i])
		}

		oLines = strings.Split(sb.String(), "\n")
	}

	return strings.Join(oLines, "\n")
}

func strPtr(s string) *string {
	return &s
}
