package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestUseSyntax(t *testing.T) {
	tests := []struct {
		input    string
		wantPath string
		wantFn   string
		wantAlias string
	}{
		{"use std/math.add",              "std/math",   "add", ""},
		{"use std/math.add a",            "std/math",   "add", "a"},
		{"use github.com/utils/math.add", "github.com/utils/math", "add", ""},
		{"use /utils/math.add",           "/utils/math", "add", ""},
		{"use fab.fib",                   "fab",        "fib", ""},
		{"use fmt",                       "fmt",        "",    ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			l := lexer.New(tt.input + "\n")
			p := New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Fatalf("parse errors: %v", p.Errors())
			}
			if len(prog.Statements) == 0 {
				t.Fatal("no statements")
			}
			us, ok := prog.Statements[0].(*UseStatement)
			if !ok {
				t.Fatalf("expected UseStatement, got %T", prog.Statements[0])
			}
			if us.Path != tt.wantPath {
				t.Errorf("Path: got %q, want %q", us.Path, tt.wantPath)
			}
			if us.Function != tt.wantFn {
				t.Errorf("Function: got %q, want %q", us.Function, tt.wantFn)
			}
			if us.Alias != tt.wantAlias {
				t.Errorf("Alias: got %q, want %q", us.Alias, tt.wantAlias)
			}
		})
	}
}
