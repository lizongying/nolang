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
		// ESM third-party imports (# npm/<specifier>[.fn][as alias]).
		{"# npm/lodash", "npm/lodash", "", "", false},
		{"# npm/lodash.get", "npm/lodash", "get", "", false},
		{"# npm/lodash.get as gt", "npm/lodash", "get", "gt", true},
		{"# npm/lodash.default as _", "npm/lodash", "default", "_", true},
		{"# npm/@scope/pkg", "npm/@scope/pkg", "", "", false},
		{"# npm/@scope/pkg.run as r", "npm/@scope/pkg", "run", "r", true},
		{"# npm/lodash/fp.get as get", "npm/lodash/fp", "get", "get", true},
		// 模組別名（無 `as`、無函數名）：`# std/err error` 把整個模組綁到
		// `error` 下（`error.new(...)`、型別 `error.err`）。這一行原本會被
		// 拆成「`# std/err` + 獨立陳述 `error`」，`no fmt` 於是把它印成兩行。
		{"# std/err error", "std/err", "", "error", false},
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
