package fmt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// corpusFiles returns every .no source file in the std library and the example
// projects. These are all expected to be well-formed nolang, so they serve as a
// realistic regression corpus for formatter changes.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	roots := []string{
		filepath.Join("..", "std"),
		filepath.Join("..", "..", "example"),
	}
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) == ".no" {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return files
}

// TestFormatReparseClean is the HARD invariant a formatter must hold: for every
// real .no file in the std library and example projects, the formatted output
// must re-parse without errors. This is the safety net that must hold before
// any refactor of the formatter's hand-rolled position switches onto AST
// Pos()/EndPos().
func TestFormatReparseClean(t *testing.T) {
	for _, path := range corpusFiles(t) {
		path := path
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			formatted := FormatFile(string(src))

			l := lexer.New(formatted)
			p := parser.New(l)
			p.ParseProgram()
			if perrs := p.Errors(); len(perrs) > 0 {
				t.Fatalf("formatted %s no longer parses:\n%s", path, joinErrors(perrs))
			}
		})
	}
}

// TestFormatIdempotent guards that formatting is stable: FormatFile(FormatFile(x))
// == FormatFile(x) for every real .no file in the corpus. Idempotency broke
// previously because stmtTokenEndLine/stmtExprEndLine returned a block's OPENING
// brace line as its "end", so hasBlankLineBetween scanned the block's interior
// (including blank lines the formatter itself inserted) and wrongly added a blank
// line after the block on the second pass. That was fixed by returning the
// closing brace (EndPos) instead; this test locks the fix in.
func TestFormatIdempotent(t *testing.T) {
	for _, path := range corpusFiles(t) {
		path := path
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			formatted := FormatFile(string(src))
			formatted2 := FormatFile(formatted)
			if formatted2 != formatted {
				t.Fatalf("formatter not idempotent on %s:\n--- first ---\n%s\n--- second ---\n%s",
					path, formatted, formatted2)
			}
		})
	}
}

func joinErrors(errs []string) string {
	out := ""
	for _, e := range errs {
		out += "- " + e + "\n"
	}
	return out
}
