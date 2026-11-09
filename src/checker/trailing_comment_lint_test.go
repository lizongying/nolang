package checker

// trailing-comments lint 的回歸測試。
//
// 契約：只有「檔案尾端、後面沒有任何陳述」的註解會被報為游離（它們是 parser
// 唯一真的收不下的那群——Program.TrailingComments）。nolang 給的解法是在註解
// 後面補一行 `...`（佔位陳述）：註解就成為 EmptyStatement 的 Doc，不再是游離的。
// 這條測試同時釘住「補了 `...` 就不再報」，否則使用者按提示修完還在報，會以為
// 提示是錯的。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseForTrailingCommentLint 解析 src 並返回程式。
func parseForTrailingCommentLint(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil for %q", src)
	}
	return prog
}

func TestTrailingCommentLint(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantN    int
		wantLine int
	}{
		{
			name:     "eof comment after statement",
			src:      "x = 1\n; 說明\n",
			wantN:    1,
			wantLine: 2,
		},
		{
			name:     "eof comment after function",
			src:      "f = () {\n    y = 1\n}\n; 說明\n",
			wantN:    1,
			wantLine: 4,
		},
		{
			// 三行連續註解是**一個**群組，只報一條——不然一段尾註會刷三條。
			name:     "multi-line eof group reports once",
			src:      "x = 1\n; a\n; b\n; c\n",
			wantN:    1,
			wantLine: 2,
		},
		{
			// 核心契約：`...` 把註解收成 EmptyStatement 的 Doc ⇒ 不再游離。
			name:     "placeholder anchors the comment",
			src:      "; 說明\n...\n",
			wantN:    0,
			wantLine: 0,
		},
		{
			name:     "comment followed by statement is a doc",
			src:      "; 說明\nx = 1\n",
			wantN:    0,
			wantLine: 0,
		},
		{
			// 空行不中斷 doc group：這種「banner + 空行」仍附著在宣告上，不報。
			name:     "banner separated by blank line still attaches",
			src:      "; banner\n\nx = 1\n",
			wantN:    0,
			wantLine: 0,
		},
		{
			name:     "no comments at all",
			src:      "x = 1\n",
			wantN:    0,
			wantLine: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseForTrailingCommentLint(t, tc.src)
			got := runLintTrailingComments(prog, LintOptions{SourcePath: "x.no"})
			if len(got) != tc.wantN {
				t.Fatalf("got %d diagnostic(s), want %d: %+v", len(got), tc.wantN, got)
			}
			if tc.wantN == 0 {
				return
			}
			r := got[0]
			if r.Severity != LintWarning {
				t.Errorf("severity = %q, want %q", r.Severity, LintWarning)
			}
			if r.Source != "nolang-comment" {
				t.Errorf("source = %q, want nolang-comment", r.Source)
			}
			if r.Line != tc.wantLine {
				t.Errorf("line = %d, want %d", r.Line, tc.wantLine)
			}
			if r.TraceID != "894luer5" {
				t.Errorf("trace id = %q, want 894luer5", r.TraceID)
			}
			// 提示必須指向 `...`：這是使用者唯一的修法，訊息裡掉了它就等於沒提示。
			if !strings.Contains(r.Message, "...") {
				t.Errorf("message must mention the `...` anchor, got %q", r.Message)
			}
		})
	}
}

// TestTrailingCommentLintIsSurfaceStep 確認新步驟註冊在 surface 軌：
// 它不碰型別推斷，若被歸到 THIR 軌，只跑 RunSurfaceLints 的路徑（如部分
// 增量檢查）就永遠不會報。
func TestTrailingCommentLintIsSurfaceStep(t *testing.T) {
	found := false
	for _, s := range lintSteps {
		if s.name != "trailing-comments" {
			continue
		}
		found = true
		if !s.surface {
			t.Errorf("step trailing-comments must be surface track")
		}
	}
	if !found {
		t.Fatalf("step trailing-comments not registered in lintSteps")
	}
}
