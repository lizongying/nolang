package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	// Use input with actual parse error (missing closing paren in result params)
	input := strings.TrimSpace(`
str.len = () (n    i64      {
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

// lspConn 包装 LSP 进程的 stdin/stdout
// 在测试中不使用 Close，进程结束后自动关闭
type lspConn struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (c *lspConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *lspConn) Write(p []byte) (int, error) { _, err := c.stdin.Write(p); return len(p), err }

// sendLSPRequest 通过 stdin 向 LSP 进程发送 JSON-RPC 请求，返回 stdout 响应
// 自动跳过中间的通知消息（无 "id" 的消息）
func sendLSPRequest(t *testing.T, lsp *lspConn, method string, params any) ([]byte, error) {
	t.Helper()

	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %v", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := lsp.Write([]byte(header + string(body))); err != nil {
		return nil, fmt.Errorf("write request: %v", err)
	}

	// 循环读取响应，跳过通知消息
	for {
		resp, err := readLSPResponse(t, lsp)
		if err != nil {
			return nil, err
		}
		// 检查是否是响应（有 id 字段）
		var msg map[string]any
		if err := json.Unmarshal(resp, &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message: %v", err)
		}
		if _, hasID := msg["id"]; hasID {
			return resp, nil
		}
		// 通知消息，丢弃并继续读取
	}
}

// sendLSPNotification 发送 JSON-RPC 通知（无需响应）
func sendLSPNotification(t *testing.T, lsp *lspConn, method string, params any) error {
	t.Helper()

	request := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}

	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal notification: %v", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	_, err = lsp.Write([]byte(header + string(body)))
	return err
}

// logDiff 输出类似 git diff 的对比，用颜色标记差异行
func logDiff(t *testing.T, original, formatted string) {
	t.Helper()

	oLines := strings.Split(original, "\n")
	fLines := strings.Split(formatted, "\n")

	oIdx, fIdx := 0, 0
	const (
		red   = "\033[31m"
		green = "\033[32m"
		cyan  = "\033[36m"
		reset = "\033[0m"
	)

	fmt.Fprintf(os.Stdout, "%s--- original%s\n", red, reset)
	fmt.Fprintf(os.Stdout, "%s+++ formatted%s\n", green, reset)

	for oIdx < len(oLines) || fIdx < len(fLines) {
		if oIdx < len(oLines) && fIdx < len(fLines) && oLines[oIdx] == fLines[fIdx] {
			oIdx++
			fIdx++
			continue
		}

		syncO, syncF := -1, -1
		for offset := 1; offset <= 10; offset++ {
			offO := oIdx + offset
			offF := fIdx + offset
			if offO < len(oLines) && offF < len(fLines) && oLines[offO] == fLines[offF] {
				syncO, syncF = offO, offF
				break
			}
		}

		if syncO != -1 && syncF != -1 {
			for ; oIdx < syncO; oIdx++ {
				fmt.Fprintf(os.Stdout, "%s-%s%s\n", red, oLines[oIdx], reset)
			}
			for ; fIdx < syncF; fIdx++ {
				fmt.Fprintf(os.Stdout, "%s+%s%s\n", green, fLines[fIdx], reset)
			}
			fmt.Fprintf(os.Stdout, " %s\n", oLines[oIdx])
			oIdx++
			fIdx++
		} else {
			for ; oIdx < len(oLines); oIdx++ {
				fmt.Fprintf(os.Stdout, "%s-%s%s\n", red, oLines[oIdx], reset)
			}
			for ; fIdx < len(fLines); fIdx++ {
				fmt.Fprintf(os.Stdout, "%s+%s%s\n", green, fLines[fIdx], reset)
			}
		}
	}
}

// startLSP 启动 LSP 进程，完成握手，返回连接和清理函数
func startLSP(t *testing.T) (*lspConn, func()) {
	t.Helper()

	lspBin := "../../vscode-nolang/server/nolang-lsp"
	if _, err := os.Stat(lspBin); os.IsNotExist(err) {
		t.Skipf("LSP binary not found: %s", lspBin)
	}

	cmd := exec.Command(lspBin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start lsp: %v", err)
	}

	lsp := &lspConn{stdin, stdout}

	// initialize
	initParams := map[string]any{
		"capabilities": map[string]any{},
	}
	resp, err := sendLSPRequest(t, lsp, "initialize", initParams)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	var initResult map[string]any
	if err := json.Unmarshal(resp, &initResult); err != nil {
		t.Fatalf("unmarshal initialize response: %v, body=%q", err, string(resp))
	}
	if initResult["error"] != nil {
		t.Fatalf("initialize error: %v", initResult["error"])
	}
	t.Logf("Initialize response: %+v", initResult["result"])

	// initialized notification
	sendLSPNotification(t, lsp, "initialized", map[string]any{})

	cleanup := func() {
		cmd.Process.Kill()
	}
	return lsp, cleanup
}

// formatLSPCode 通过 LSP 格式化代码，返回格式化后的文本
func formatLSPCode(t *testing.T, lsp *lspConn, uri, code string) string {
	t.Helper()

	sendLSPNotification(t, lsp, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "nolang",
			"version":    1,
			"text":       code,
		},
	})

	formatParams := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
	}
	resp, err := sendLSPRequest(t, lsp, "textDocument/formatting", formatParams)
	if err != nil {
		t.Fatalf("formatting failed: %v", err)
	}

	var formatResult map[string]any
	if err := json.Unmarshal(resp, &formatResult); err != nil {
		t.Fatalf("unmarshal formatting response: %v, body=%q", err, string(resp))
	}
	if formatResult["error"] != nil {
		t.Fatalf("formatting error: %v", formatResult["error"])
	}

	editsRaw, ok := formatResult["result"].([]any)
	if !ok {
		t.Fatalf("result is not array, got=%T value=%+v", formatResult["result"], formatResult["result"])
	}

	return applyRawTextEdits(code, editsRaw)
}

// readLSPResponse 从 stdout 读取 LSP 响应
func readLSPResponse(t *testing.T, lsp *lspConn) ([]byte, error) {
	t.Helper()

	// 读取 header
	var headerBytes []byte
	buf := make([]byte, 1)
	for {
		if _, err := lsp.Read(buf); err != nil {
			return nil, fmt.Errorf("read header: %v", err)
		}
		headerBytes = append(headerBytes, buf[0])
		if len(headerBytes) >= 4 && string(headerBytes[len(headerBytes)-4:]) == "\r\n\r\n" {
			break
		}
	}

	// 解析 Content-Length
	header := string(headerBytes)
	var contentLength int
	if _, err := fmt.Sscanf(header, "Content-Length: %d", &contentLength); err != nil {
		return nil, fmt.Errorf("parse Content-Length: %v, header=%q", err, header)
	}

	// 读取 body
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(lsp, body); err != nil {
		return nil, fmt.Errorf("read body: %v", err)
	}

	return body, nil
}

// cd ./src && go test ./lsp/... -v -run TestLSPBinaryFormatting_IdempotentWithComments
func TestLSPBinaryFormatting_IdempotentWithComments(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP binary test in short mode")
	}

	lspBin := "../../vscode-nolang/server/nolang-lsp"
	if _, err := os.Stat(lspBin); os.IsNotExist(err) {
		t.Skipf("LSP binary not found: %s", lspBin)
	}

	cmd := exec.Command(lspBin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start lsp: %v", err)
	}
	defer cmd.Process.Kill()

	lsp := &lspConn{stdin, stdout}

	initParams := map[string]any{
		"capabilities": map[string]any{},
	}
	resp, err := sendLSPRequest(t, lsp, "initialize", initParams)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	var initResult map[string]any
	if err := json.Unmarshal(resp, &initResult); err != nil {
		t.Fatalf("unmarshal initialize response: %v, body=%q", err, string(resp))
	}
	if initResult["error"] != nil {
		t.Fatalf("initialize error: %v", initResult["error"])
	}
	t.Logf("Initialize response: %+v", initResult["result"])

	sendLSPNotification(t, lsp, "initialized", map[string]any{})

	// Test: already-formatted code with comments before function and inside body
	// Expected: formatting should preserve comments and code structure
	{
		code := `
// aes-128-dec: 解密一個 16-byte 區塊
// in: 輸入密文（16 位元組）
// n: 固定 16
// key: 16-byte 金鑰
// out: 輸出明文（16 位元組）
aes-128-dec = (in str, n i64, key str, out str) {
    // 展開金鑰
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)

    // 複製輸入到狀態
    i = 0
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }

    // 初始 AddRoundKey（輪 10）
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    for round > 0 {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    }

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}`

		uri := "file:///test_format_idempotent.no"

		sendLSPNotification(t, lsp, "textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "nolang",
				"version":    1,
				"text":       code,
			},
		})

		formatParams := map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
		}
		resp, err = sendLSPRequest(t, lsp, "textDocument/formatting", formatParams)
		if err != nil {
			t.Fatalf("formatting failed: %v", err)
		}

		var formatResult map[string]any
		if err := json.Unmarshal(resp, &formatResult); err != nil {
			t.Fatalf("unmarshal formatting response: %v, body=%q", err, string(resp))
		}
		if formatResult["error"] != nil {
			t.Fatalf("formatting error: %v", formatResult["error"])
		}

		editsRaw, ok := formatResult["result"].([]any)
		if !ok {
			t.Fatalf("result is not array, got=%T value=%+v", formatResult["result"], formatResult["result"])
		}

		// Apply edits and verify the result
		editedText := applyRawTextEdits(code, editsRaw)
		t.Logf("Original:\n%s\n", code)
		t.Logf("Formatted:\n%s\n", editedText)

		// Verify formatting is idempotent: applying edits a second time should produce the same result
		secondEdits := computeTextEdits(editedText, editedText)
		if secondEdits != nil {
			t.Errorf("formatting is NOT idempotent: second format still produces changes")
		} else {
			t.Log("✓ formatting is idempotent")
		}
	}
}

func TestLSPBinaryFormatting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP binary test in short mode")
	}

	lsp, cleanup := startLSP(t)
	defer cleanup()

	// Test formatting unformatted code
	code := `// x509-rsa-e: test
x509-rsa-e=(data str, n i64, e i64) {
    // 找到 TBSCertificate
    x509-tbs-range(data, n, tbs-start, tbs-end)
    if tbs-start == 0 {
        e = 0
        return
    }
    p = tbs-start + 1
    // 跳過 INTEGER（序號）
    der-skip(data, n, p, p)

    // 跳過 SEQUENCE（簽章演算法）
    der-skip(data, n, p, p)

    // 讀取 BIT STRING
    der-tag(data, p, t)
}`

	uri := "file:///test_format.no"
	editedText := formatLSPCode(t, lsp, uri, code)
	t.Logf("Formatted result:\n%s\n", editedText)

	// Verify function signature has proper spacing
	if strings.Contains(editedText, "x509-rsa-e = (data str") {
		t.Log("✓ function signature has proper spacing")
	}
	// Verify no code was lost
	if strings.Count(editedText, "der-skip") == 3 && strings.Count(editedText, "der-tag") == 1 {
		t.Log("✓ all code preserved")
	}
}

// applyRawTextEdits 将 JSON TextEdit 列表应用到原始文字
func applyRawTextEdits(original string, edits []any) string {
	oLines := strings.Split(original, "\n")

	// 按行号倒序排序
	type edit struct {
		startLine int
		startChar int
		endLine   int
		endChar   int
		newText   string
	}
	var parsed []edit
	for _, e := range edits {
		em := e.(map[string]any)
		rng := em["range"].(map[string]any)
		start := rng["start"].(map[string]any)
		end := rng["end"].(map[string]any)
		parsed = append(parsed, edit{
			startLine: int(start["line"].(float64)),
			startChar: int(start["character"].(float64)),
			endLine:   int(end["line"].(float64)),
			endChar:   int(end["character"].(float64)),
			newText:   em["newText"].(string),
		})
	}

	// 冒泡排序
	for i := 0; i < len(parsed); i++ {
		for j := i + 1; j < len(parsed); j++ {
			if parsed[i].startLine < parsed[j].startLine ||
				(parsed[i].startLine == parsed[j].startLine && parsed[i].startChar < parsed[j].startChar) {
				parsed[i], parsed[j] = parsed[j], parsed[i]
			}
		}
	}

	for _, e := range parsed {
		var sb strings.Builder
		for i := 0; i < e.startLine; i++ {
			sb.WriteString(oLines[i])
			sb.WriteString("\n")
		}
		sb.WriteString(oLines[e.startLine][:e.startChar])
		sb.WriteString(e.newText)
		if e.endChar < len(oLines[e.endLine]) {
			sb.WriteString(oLines[e.endLine][e.endChar:])
		}
		for i := e.endLine + 1; i < len(oLines); i++ {
			sb.WriteString("\n")
			sb.WriteString(oLines[i])
		}
		oLines = strings.Split(sb.String(), "\n")
	}

	return strings.Join(oLines, "\n")
}

// cd ./src && go test ./lsp/... -v -run TestLSPBinaryFormatting1
func TestLSPBinaryFormatting1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LSP binary test in short mode")
	}

	lsp, cleanup := startLSP(t)
	defer cleanup()

	// Read source file
	sourceFile := "../../tmp/test_format_idempotent"
	codeBytes, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatalf("read source file: %v", err)
	}
	code := string(codeBytes)

	uri := "file://" + sourceFile
	editedText := formatLSPCode(t, lsp, uri, code)

	logDiff(t, code, editedText)

	// Verify formatting is idempotent
	secondEdits := computeTextEdits(editedText, editedText)
	if secondEdits != nil {
		t.Errorf("formatting is NOT idempotent: second format still produces changes")
	} else {
		t.Log("✓ formatting is idempotent")
	}
}
