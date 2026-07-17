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
		{name: "for_cstyle", input: "for i = 0, i < 5, i++ {\n}", wantErr: false},
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
		{name: "bang_loop", input: "!! {\n    break\n}", wantErr: false},
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
		// 單語句體 (single-statement body)
		{name: "for_range_single_stmt", input: "for i <- [0..5): print(i)", wantErr: false},
		{name: "bare_range_single_stmt", input: "i <- [0..5): print(i)", wantErr: false},
		{name: "for_range_str_single_stmt", input: "for i <- 'abc': print(i)", wantErr: false},
		{name: "labeled_range_single_stmt", input: "outer i <- [0..5): print(i)", wantErr: false},
		{name: "for_cond_single_stmt", input: "for i < 5: print(i)", wantErr: false},
		// 結構體欄位方法調用 (struct field method call)
		{name: "struct_field_method_call", input: "inner-type {\n    value i64\n}\ninner-type.get-value = () (v i64) {\n    v = self.value\n}\ncontainer {\n    inner inner-type\n}\nc container\nc.inner = inner-type {\n    value: 42\n}\nv = c.inner.get-value()\nc.inner.set-value(100)", wantErr: false},
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
    1 ->
        a = 1
    2 ->
        do-something()
    ->
        c = 0
}`, wantErr: false},
		// switch - 有返回值
		{name: "switch_with_result", input: `result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}`, wantErr: false},
		// bare match (if/else) - 無 matched expression
		{name: "bare_match", input: `{
    a == 1 ->
        a = 1
        b = 2
    a == 2 ->
        do-something()
    ->
        c = 0
}`, wantErr: false},
		// match - err/nil
		{name: "match_err_nil", input: `x: {
    err -> log(it)
    nil -> log('nil')
    ->
        do-right-thing(it)
}`, wantErr: false},
		// 舊式寫法 without colon (deprecated)
		{name: "old_switch_no_colon", input: `x {
    1 -> 10
    2 -> 20
    _-> 0
}`, wantErr: false},
		// new syntax with colon
		{name: "new_switch_colon", input: `x: {
    1 -> 10
    2 -> 20
    -> 0
}`, wantErr: false},
		// new syntax with result
		{name: "new_switch_result_colon", input: `result = x: {
    1 -> 10
    2 -> 20
    -> 0
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

func TestCombinedOptionPatterns(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// nil || err with ok arm — should parse without errors
		{name: "nil_or_err_with_ok", input: `x ?i64
x: {
    nil || err -> log('failed')
    ok -> process(it)
}`, wantErr: false},
		// err || nil — order should not matter
		{name: "err_or_nil_with_ok", input: `x ?i64
x: {
    err || nil -> log('failed')
    ok -> process(it)
}`, wantErr: false},
		// nil || err with block body
		{name: "nil_or_err_block_body", input: `x ?i64
x: {
    nil || err -> {
        cleanup()
        return
    }
    ok -> process(it)
}`, wantErr: false},
		// nil || err without ok — should error (missing ok branch)
		{name: "nil_or_err_missing_ok", input: `x ?i64
x: {
    nil || err -> log('failed')
}`, wantErr: true},
		// nil only without err and ok — should error (missing err and ok)
		{name: "nil_only_missing_err_ok", input: `x ?i64
x: {
    nil -> log('nil')
}`, wantErr: true},
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
		// 'for cond { }' is now a valid form (for loops where range-for doesn't apply)
		{name: "for_condition_no_warning", input: "for i < 5 {\n    break\n}", wantWarnings: 0},
		{name: "for_in_deprecated", input: "for i in [0..10) {\n    break\n}", wantWarnings: 1},
		{name: "switch_deprecated", input: "switch x {\n    case 1: a = 1\n    default: c = 0\n}", wantWarnings: 1},
		{name: "while_no_colon_deprecated", input: "while i < 5 {\n    break\n}", wantWarnings: 1},
		{name: "bare_range_no_colon_deprecated", input: "i <- [0..10) {\n    break\n}", wantWarnings: 1},
		{name: "match_no_colon_deprecated", input: "x {\n    1-> 10\n    -> 0\n}", wantWarnings: 1},
		{name: "match_keyword_deprecated", input: "match x {\n    1 -> 10\n    -> 0\n}", wantWarnings: 1},
		{name: "if_keyword_deprecated", input: "if x > 0 {\n    a = 1\n} else {\n    a = 0\n}", wantWarnings: 1},
		// New syntax — no warnings
		{name: "while_with_colon_no_warning", input: "while i < 5: {\n    break\n}", wantWarnings: 0},
		{name: "bare_range_with_colon_no_warning", input: "i <- [0..10): {\n    break\n}", wantWarnings: 0},
		{name: "for_infinite_no_warning", input: "for {\n    break\n}", wantWarnings: 0},
		{name: "for_cstyle_no_warning", input: "for i = 0, i < 5, i++ {\n}", wantWarnings: 0},
		{name: "new_match_no_warning", input: "x: {\n    1-> 10\n    -> 0\n}", wantWarnings: 0},
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

func TestFFIDeclarationSyntax(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		filename string
		wantErr  bool
	}{
		{name: "ffi_basic", input: "#c\nc-strlen = (s str) (n i64)", filename: "test.no", wantErr: false},
		{name: "ffi_single_ptr", input: "#c\nsqlite3-close = (db *byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "ffi_double_ptr", input: "#c\nsqlite3-open = (filename str, db **byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "ffi_triple_ptr", input: "#c\nfoo = (p ***byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "ffi_mixed_ptrs", input: "#c\nsqlite3-exec = (db *byte, sql str, cb *byte, arg *byte, errmsg *byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "ffi_ptr_result", input: "#c\nmalloc = (n i64) (p *byte)", filename: "test.no", wantErr: false},
		{name: "ffi_ptr_i64", input: "#c\nfoo = (p *i64) (r i32)", filename: "test.no", wantErr: false},
		{name: "ffi_private_underscore", input: "#c\n_sqlite3-open = (filename str, db **byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "ffi_with_newline_gap", input: "#c\n\n_sqlite3-open = (db **byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "ffi_no_results", input: "#c\nfoo = (n i64)", filename: "test.no", wantErr: false},
		// #{c} annotation syntax (new style, replacing #c)
		{name: "annot_ffi_basic", input: "#{c}\nc-strlen = (s str) (n i64)", filename: "test.no", wantErr: false},
		{name: "annot_ffi_single_ptr", input: "#{c}\nsqlite3-close = (db *byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "annot_ffi_double_ptr", input: "#{c}\nsqlite3-open = (filename str, db **byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "annot_ffi_private_underscore", input: "#{c}\n_sqlite3-open = (filename str, db **byte) (rc i32)", filename: "test.no", wantErr: false},
		{name: "annot_ffi_no_results", input: "#{c}\nfoo = (n i64)", filename: "test.no", wantErr: false},
		{name: "annot_ffi_with_extra", input: "#{c, debug}\n_sqlite3-open = (filename str, db **byte) (rc i32)", filename: "test.no", wantErr: false},
		// General annotations (non-FFI)
		{name: "annot_bool", input: "#{debug}", filename: "test.no", wantErr: false},
		{name: "annot_int", input: "#{max=100}", filename: "test.no", wantErr: false},
		{name: "annot_string", input: "#{name='hello'}", filename: "test.no", wantErr: false},
		{name: "annot_array", input: "#{derive=[Serialize, Deserialize]}", filename: "test.no", wantErr: false},
		{name: "annot_range", input: "#{range=[0..256)}", filename: "test.no", wantErr: false},
		{name: "annot_complex", input: "#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}", filename: "test.no", wantErr: false},
		// Annotation attached to declaration
		{name: "annot_attach_let", input: "#{range=[0..256)}\nx num = 42", filename: "test.no", wantErr: false},
		{name: "annot_attach_let_range_closed", input: "#{range=[0..255]}\nx i64 = 0", filename: "test.no", wantErr: false},
		{name: "annot_attach_let_paren", input: "#{range=(0..256)}\nx i64 = 1", filename: "test.no", wantErr: false},
		{name: "annot_attach_struct", input: "#{range=[0..256)}\npoint {\n    x i64\n    y i64\n}", filename: "test.no", wantErr: false},
		{name: "annot_struct_field", input: "person {\n    #{range=[0..256)}\n    age num\n    name str\n}", filename: "test.no", wantErr: false},
		{name: "annot_struct_field_range", input: "config {\n    #{range=[0..100)}\n    port i64\n}", filename: "test.no", wantErr: false},
		{name: "annot_standalone", input: "#{debug}\nx = 1", filename: "test.no", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			p.Filename = tt.filename
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
