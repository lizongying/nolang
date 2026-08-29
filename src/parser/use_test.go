package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestUseSyntax(t *testing.T) {
	tests := []struct {
		input     string
		wantPath  string
		wantFn    string
		wantAlias string
		wantAsKw  bool
	}{
		{"# std/math.add", "std/math", "add", "", false},
		{"# std/math.add a", "std/math", "add", "a", false},
		{"# std/math.add as alias1", "std/math", "add", "alias1", true},
		{"# github.com/utils/math.add", "github.com/utils/math", "add", "", false},
		{"# github.com/utils/math.add as alias2", "github.com/utils/math", "add", "alias2", true},
		{"# /utils/math.add", "/utils/math", "add", "", false},
		{"# fab.fib", "fab", "fib", "", false},
		{"# fmt", "fmt", "", "", false},
		// regression: keyword as path segment should be accepted
		// `run` and `chan` are keyword tokens but should be valid path segments.
		// `# /nonpm/src/run` imports the module `/nonpm/src/run` (no function name).
		{"# /nonpm/src/run", "/nonpm/src/run", "", "", false},
		{"# mymod.run", "mymod", "run", "", false},
		{"# /path/to/chan", "/path/to/chan", "", "", false},
		{"# /nonpm/src.run", "/nonpm/src", "run", "", false},
		{"# /nonpm/src.run as runner", "/nonpm/src", "run", "runner", true},
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
			if us.AsKeyword != tt.wantAsKw {
				t.Errorf("AsKeyword: got %v, want %v", us.AsKeyword, tt.wantAsKw)
			}
		})
	}
}
