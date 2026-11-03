package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// idBareElse / idBareElseSib are the message IDs of the "bare '->' else outside
// an if-group" error emitted by parseBlockStatement — the plain one and the
// variant naming the ignored sibling arms. Kept in sync with the call site in
// stmt.go; the test filters diagnostics by them so the wording can change freely.
const (
	idBareElse    = "k3r7mbqd"
	idBareElseSib = "m7vq3xrt"
)

// countBareElseDiags returns the ERROR-severity diagnostics carrying either of
// the two bare-else IDs.
func countBareElseDiags(p *Parser) []Diagnostic {
	var out []Diagnostic
	for _, d := range p.diags {
		if d.Severity == SeverityError && (d.ID == idBareElse || d.ID == idBareElseSib) {
			out = append(out, d)
		}
	}
	return out
}

// TestBareElseRequiresIfGroup locks the language rule: a lone `cond -> body`
// may stand outside an if-group, but a newline `-> else` arm may not — it folds
// onto the SINGLE nearest bare if, silently ignoring every sibling `cond ->`
// arm above it (str.no exponent-sign parsing was the real-world victim).
func TestBareElseRequiresIfGroup(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErrs   int
		wantLines  []int
		wantSibMsg bool // expect the "sibling arms ignored" wording
	}{
		{
			// 裸 if 单独使用是合法的，不需要 { } 包裹。
			name:     "bare_if_alone_ok",
			input:    "main = () {\n  a -> x = 1\n}\n",
			wantErrs: 0,
		},
		{
			// 函数体内：裸 if + 换行 `-> else` → 必须报错并指向 `->` 那一行。
			name:      "bare_if_then_else_in_func_body",
			input:     "main = () {\n  a -> x = 1\n  -> x = 2\n}\n",
			wantErrs:  1,
			wantLines: []int{3},
		},
		{
			// 多条裸 if 后接一条 `-> else`：只报一次，且提示点名被忽略的上游臂。
			name:       "multi_bare_if_then_else",
			input:      "main = () {\n  a -> { x = 1 }\n  b -> x = 2\n  -> break\n}\n",
			wantErrs:   1,
			wantLines:  []int{4},
			wantSibMsg: true,
		},
		{
			// 循环体内同样的写法同样报错（str.no 指数符号解析的实际形状）。
			name:       "bare_else_in_loop_body",
			input:      "main = () {\n  (i < n) {\n    c == 45 -> { e = -1 }\n    c == 43 -> i = i + 1\n    -> break\n  }\n}\n",
			wantErrs:   1,
			wantLines:  []int{5},
			wantSibMsg: true,
		},
		{
			// 正确写法：用 { } if 组包住所有臂， including the else.
			name:     "if_group_ok",
			input:    "main = () {\n  {\n    a -> { x = 1 }\n    b -> x = 2\n    -> break\n  }\n}\n",
			wantErrs: 0,
		},
		{
			// if 组里只有 if/else 两臂也合法。
			name:     "if_group_two_arm_ok",
			input:    "main = () {\n  {\n    a -> x = 1\n    -> x = 2\n  }\n}\n",
			wantErrs: 0,
		},
		{
			// 同行 `->` 属于链式管线，不是换行 else，不触发本规则。
			name:     "same_line_chain_ok",
			input:    "main = () {\n  a -> x = 1 -> x = 2\n}\n",
			wantErrs: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			p.ParseProgram()
			got := countBareElseDiags(p)
			if len(got) != tt.wantErrs {
				t.Fatalf("expected %d bare-else errors, got %d: %v", tt.wantErrs, len(got), got)
			}
			for i, want := range tt.wantLines {
				if got[i].Pos.Line != want {
					t.Errorf("error %d: expected line %d, got line %d", i, want, got[i].Pos.Line)
				}
			}
			for _, d := range got {
				// The diagnostic must teach the fix, not just name the symptom.
				if !strings.Contains(d.Message, "{") || !strings.Contains(strings.ToLower(d.Message), "if-group") {
					t.Errorf("expected message to point at the '{ }' if-group fix, got %q", d.Message)
				}
				if sib := strings.Contains(d.Message, "ignored"); sib != tt.wantSibMsg {
					t.Errorf("expected sibling-arms wording %v, got %q", tt.wantSibMsg, d.Message)
				}
				// 兩類提示各走自己的 ID：點名上游臂的那類不能混進普通 ID。
				wantID := idBareElse
				if tt.wantSibMsg {
					wantID = idBareElseSib
				}
				if d.ID != wantID {
					t.Errorf("expected message ID %q, got %q (%q)", wantID, d.ID, d.Message)
				}
			}
		})
	}
}
