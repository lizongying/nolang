package lsp

import (
	"encoding/json"
	"testing"
)

// TestOverflowCodeActionEdit 驗證「整數運算溢出」提示的 code action 能正確
// 在運算所在行的上方、以與該行一致的縮排插入 #{overflow = <mode>} 註解（5 種模式）。
func TestOverflowCodeActionEdit(t *testing.T) {
	s := NewServer()
	uri := "file:///tmp/ovf-hint.no"
	// 第 0 行：函式定義；第 1 行：縮排 4 格的 `r = a - b`（相減所在行）。
	text := "sub = (a i64, b i64) (r i64) {\n    r = a - b\n}\n"
	if _, err := s.documents.OpenDocument(uri, text); err != nil {
		t.Fatalf("open doc: %v", err)
	}

	params := CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range:        Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 0}},
		Context: CodeActionContext{
			Diagnostics: []Diagnostic{
				{Code: "overflow-wrap", Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 5}}},
			},
		},
	}

	res, err := s.handleTextDocumentCodeAction(params)
	if err != nil {
		t.Fatalf("code action: %v", err)
	}
	actions, ok := res.([]CodeAction)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	wantModes := []string{"wrap", "clamp0", "min", "max", "saturate"}
	if len(actions) != len(wantModes) {
		t.Fatalf("expected %d actions, got %d", len(wantModes), len(actions))
	}
	for i, mode := range wantModes {
		if actions[i].Title != "Add #{overflow = "+mode+"}" {
			t.Fatalf("unexpected title[%d] %q", i, actions[i].Title)
		}
		if actions[i].Kind != CodeActionKindQuickFix {
			t.Fatalf("expected quickfix kind for %q, got %s", mode, actions[i].Kind)
		}
		edits := actions[i].Edit.Changes[uri]
		if len(edits) != 1 {
			t.Fatalf("expected 1 edit for %q, got %d", mode, len(edits))
		}
		e := edits[0]
		if e.Range.Start.Line != 1 || e.Range.Start.Character != 0 {
			t.Fatalf("unexpected edit range for %q: %+v", mode, e.Range)
		}
		want := "    #{overflow = " + mode + "}\n"
		if e.NewText != want {
			t.Fatalf("unexpected edit text for %q: %q (want %q)", mode, e.NewText, want)
		}
	}
	t.Logf("all 5 overflow quickfix edits verified")
}

// TestCodeActionContextOnlyUnmarshal 是對本 bug 的回歸測試。
// 修復前 CodeActionContext.Only 被定義為 []int，而 VS Code 在 textDocument/codeAction
// 請求中傳來的 context.only 是 CodeActionKind 字串陣列（如 ["quickfix","refactor","source"]），
// 導致 json.Unmarshal 失敗、整個 codeAction 請求回傳
// "cannot unmarshal JSON string into CodeActionParams.context.only.0 of Go type int"。
// 本測試確認字串陣列現在能被正確解析。
func TestCodeActionContextOnlyUnmarshal(t *testing.T) {
	raw := `{
		"textDocument": {"uri": "file:///tmp/x.no"},
		"range": {"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 0}},
		"context": {
			"diagnostics": [],
			"only": ["quickfix", "refactor", "source"]
		}
	}`
	var p CodeActionParams
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal CodeActionParams with string only: %v", err)
	}
	want := []CodeActionKind{CodeActionKindQuickFix, CodeActionKindRefactor, CodeActionKindSource}
	if len(p.Context.Only) != len(want) {
		t.Fatalf("expected %d only kinds, got %d", len(want), len(p.Context.Only))
	}
	for i, w := range want {
		if p.Context.Only[i] != w {
			t.Fatalf("only[%d] = %q, want %q", i, p.Context.Only[i], w)
		}
	}
}

// TestCodeActionOnlyFilter 驗證 handleTextDocumentCodeAction 尊重 context.only：
// 僅請求 refactor 時不回傳 quickfix；請求含 quickfix 時回傳 5 個 overflow quickfix。
func TestCodeActionOnlyFilter(t *testing.T) {
	s := NewServer()
	uri := "file:///tmp/ovf-hint.no"
	text := "sub = (a i64, b i64) (r i64) {\n    r = a - b\n}\n"
	if _, err := s.documents.OpenDocument(uri, text); err != nil {
		t.Fatalf("open doc: %v", err)
	}
	diag := Diagnostic{Code: "overflow-wrap", Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 5}}}

	pRefactor := CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range:        Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 0}},
		Context:      CodeActionContext{Diagnostics: []Diagnostic{diag}, Only: []CodeActionKind{CodeActionKindRefactor}},
	}
	res, err := s.handleTextDocumentCodeAction(pRefactor)
	if err != nil {
		t.Fatalf("code action: %v", err)
	}
	if acts, _ := res.([]CodeAction); len(acts) != 0 {
		t.Fatalf("expected 0 actions for only=refactor, got %d", len(acts))
	}

	pQuick := CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range:        Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 0}},
		Context:      CodeActionContext{Diagnostics: []Diagnostic{diag}, Only: []CodeActionKind{CodeActionKindQuickFix, CodeActionKindRefactor}},
	}
	res2, err := s.handleTextDocumentCodeAction(pQuick)
	if err != nil {
		t.Fatalf("code action: %v", err)
	}
	if acts, _ := res2.([]CodeAction); len(acts) != 5 {
		t.Fatalf("expected 5 actions for only=quickfix, got %d", len(acts))
	}
}
