package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestSemiSwallowWarning 驗證「單個 ; 行尾註釋疑似吞掉代碼」警告（W_SEMI_EAT）。
// nolang 中單個 `;` 是行註釋標記；行中出現 `;` 會把其後代碼（含 `}`）整體吞為註釋，
// 造成大括號失衡與後續定義被錯誤嵌套（見 test-od-full.no 的 od-print-addr 案例）。
func TestSemiSwallowWarning(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantWarns int
	}{
		{
			name:      "arm_body_semicolon_eats_brace",
			input:     "f = () {\n    i <- [0..3): {\n        i > 1 -> { a = 1; b = 2 }\n    }\n}\n",
			wantWarns: 1,
		},
		{
			name:      "line_start_comment_with_braces_ok",
			input:     "; example: { x -> y }\nf = () {\n    a = 1\n}\n",
			wantWarns: 0,
		},
		{
			name:      "line_end_comment_without_braces_ok",
			input:     "f = () {\n    a = 42 ; the answer\n}\n",
			wantWarns: 0,
		},
		{
			name:      "double_semicolon_comment_ok",
			input:     "f = () {\n    a = 1 ;; note: { b = 2 }\n}\n",
			wantWarns: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			p.ParseProgram()
			warns := p.WarningsByCode(WarnSemiSwallow)
			if len(warns) != tt.wantWarns {
				t.Errorf("expected %d W_SEMI_EAT warnings, got %d: %v", tt.wantWarns, len(warns), warns)
			}
		})
	}
}
