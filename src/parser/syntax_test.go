package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestOldSyntax(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// 舊式寫法 - 不需要冒號
		{name: "for_infinite", input: "for {\n    break\n}", wantErr: false},
		{name: "for_condition", input: "for i < 5 {\n    continue\n}", wantErr: false},
		{name: "for_cstyle", input: "for i = 0; i < 5; i++ {\n}", wantErr: false},
		{name: "for_range_bracket", input: "for i <- [0..5) {\n}", wantErr: false},
		{name: "for_range_paren", input: "for i <- (0..5] {\n}", wantErr: false},
		{name: "for_range_closed", input: "for i <- [0..5] {\n}", wantErr: false},
		{name: "for_range_open", input: "for i <- (0..5) {\n}", wantErr: false},
		{name: "for_in", input: "for i in [0..10) {\n}", wantErr: false},
		{name: "while_keyword", input: "while i < 5 {\n    break\n}", wantErr: false},
		{name: "bare_range", input: "i <- (a..b] {\n}", wantErr: false},
		{name: "bare_range_bracket", input: "i <- [a..b] {\n}", wantErr: false},
		{name: "labeled_for", input: "outer for i < 5 {\n    break\n}", wantErr: false},
		{name: "labeled_for_range", input: "outer for i <- [0..5) {\n}", wantErr: false},
		{name: "bang_loop", input: "! {\n    break\n}", wantErr: false},
		{name: "counted_loop", input: "10 * {\n    break\n}", wantErr: false},
		{name: "for_string", input: "for i <- 'abc' {\n}", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

func TestNewSyntax(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// 新式寫法 - 需要冒號
		{name: "while_colon", input: "while x == 1: {\n    b = 2\n}", wantErr: false},
		{name: "while_colon_complex", input: "while a > 0 && b < 10: {\n    c = a + b\n}", wantErr: false},
		{name: "bare_range_paren", input: "i <- (a..b]: {\n}", wantErr: false},
		{name: "bare_range_bracket", input: "i <- [a..b]: {\n}", wantErr: false},
		{name: "bare_range_open", input: "i <- (a..b): {\n}", wantErr: false},
		{name: "bare_range_closed", input: "i <- [a..b]: {\n}", wantErr: false},
		{name: "bare_range_literal", input: "i <- [0..10]: {\n}", wantErr: false},
		{name: "for_range_colon", input: "for i <- [0..5]: {\n}", wantErr: false},
		{name: "for_range_paren_colon", input: "for i <- (0..5]: {\n}", wantErr: false},
		{name: "for_condition_colon", input: "for i < 5: {\n    break\n}", wantErr: false},
		{name: "labeled_bare_range", input: "outer i <- (0..5]: {\n}", wantErr: false},
		{name: "labeled_bare_range_bracket", input: "outer i <- [0..5]: {\n}", wantErr: false},
		{name: "labeled_while_colon", input: "loop while x == 1: {\n    b = 2\n}", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

func TestMixedSyntax(t *testing.T) {
	input := `// 舊式寫法
for i < 5 {
    continue
}
for i <- [0..10) {
}
while x == 1 {
    b = 2
}
i <- (a..b] {
}

// 新式寫法
while x == 1: {
    b = 2
}
i <- (a..b]: {
}
for i <- [0..5]: {
}

// 混合
for i < 10 {
    i <- (0..5]: {
    }
}`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}
	t.Logf("parsed %d statements", len(program.Statements))
}

func TestSwitchMatchSyntax(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// switch - 無返回值
		{name: "switch_no_result", input: `x: {
    1|
        a = 1
    2|
        do-something()
    |
        c = 0
}`, wantErr: false},
		// switch - 有返回值
		{name: "switch_with_result", input: `result = x: {
    1| 1
    2| 2 + 1
    | a + b
}`, wantErr: false},
		// bare match (if/else) - 無 matched expression
		{name: "bare_match", input: `{
    a == 1|
        a = 1
        b = 2
    a == 2|
        do-something()
    |
        c = 0
}`, wantErr: false},
		// match - err/nil
		{name: "match_err_nil", input: `x: {
    err| log(it)
    nil| log('nil')
    |
        do-right-thing(it)
}`, wantErr: false},
		// 舊式寫法 without colon
		{name: "old_switch_no_colon", input: `x {
    1| 10
    2| 20
    _| 0
}`, wantErr: false},
		// new syntax with colon
		{name: "new_switch_colon", input: `x: {
    1| 10
    2| 20
    | 0
}`, wantErr: false},
		// new syntax with result
		{name: "new_switch_result_colon", input: `result = x: {
    1| 10
    2| 20
    | 0
}`, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

func TestContinueBreakReturnSyntax(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// 舊式 continue/break/return
		{name: "old_continue", input: "for i < 5 {\n    continue\n}", wantErr: false},
		{name: "old_break", input: "for i < 5 {\n    break\n}", wantErr: false},
		{name: "old_return", input: "for i < 5 {\n    return\n}", wantErr: false},
		{name: "old_labeled_break", input: "outer for i < 5 {\n    break outer\n}", wantErr: false},
		{name: "old_labeled_continue", input: "outer for i < 5 {\n    continue outer\n}", wantErr: false},
		// 新式 bare range + continue/break/return
		{name: "new_continue", input: "i <- (0..5]: {\n    continue\n}", wantErr: false},
		{name: "new_break", input: "i <- (0..5]: {\n    break\n}", wantErr: false},
		{name: "new_return", input: "i <- (0..5]: {\n    return\n}", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

func TestOldSwitchSyntax(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantErr      bool
		wantWarnings int
	}{
		{name: "switch_basic", input: "switch x {\n    case 1:\n        a = 1\n    case 2:\n        b = 2\n    default:\n        c = 0\n}", wantErr: false, wantWarnings: 1},
		{name: "switch_no_colon", input: "switch x {\n    case 1\n    case 2\n    default\n}", wantErr: false, wantWarnings: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
			if len(p.Warnings()) != tt.wantWarnings {
				t.Errorf("expected %d warnings, got %d", tt.wantWarnings, len(p.Warnings()))
				for _, w := range p.Warnings() {
					t.Logf("warning: %s", w)
				}
			}
		})
	}
}

func TestDeprecationWarnings(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantWarnings int
	}{
		// Old syntax — should warn
		{name: "for_condition_deprecated", input: "for i < 5 {\n    break\n}", wantWarnings: 1},
		{name: "for_in_deprecated", input: "for i in [0..10) {\n    break\n}", wantWarnings: 1},
		{name: "switch_deprecated", input: "switch x {\n    case 1: a = 1\n    default: c = 0\n}", wantWarnings: 1},
		{name: "while_no_colon_deprecated", input: "while i < 5 {\n    break\n}", wantWarnings: 1},
		{name: "bare_range_no_colon_deprecated", input: "i <- [0..10) {\n    break\n}", wantWarnings: 1},
		// New syntax — no warnings
		{name: "while_with_colon_no_warning", input: "while i < 5: {\n    break\n}", wantWarnings: 0},
		{name: "bare_range_with_colon_no_warning", input: "i <- [0..10): {\n    break\n}", wantWarnings: 0},
		{name: "for_infinite_no_warning", input: "for {\n    break\n}", wantWarnings: 0},
		{name: "for_cstyle_no_warning", input: "for i = 0; i < 5; i++ {\n}", wantWarnings: 0},
		{name: "new_match_no_warning", input: "x: {\n    1| 10\n    | 0\n}", wantWarnings: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Errorf("parser errors: %v", p.Errors())
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
			if len(p.Warnings()) != tt.wantWarnings {
				t.Errorf("expected %d warnings, got %d", tt.wantWarnings, len(p.Warnings()))
				for _, w := range p.Warnings() {
					t.Logf("warning: %s", w)
				}
			}
		})
	}
}
