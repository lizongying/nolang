// Package msgscan extracts message IDs and their inline English text from Go
// source by walking calls to the i18n helpers.
//
// It is shared by the catalog generator (`make i18n`) and by the catalog gate, so
// "what the generator emitted" and "what the gate expects" can never drift: both
// derive from this one walk.
//
// A call site is
//
//	i18n.T("id000000", "text %s", args...)
//	i18n.P("id000000", "text")
//	i18n.Errorf("id000000", "text %w", args...)
//
// Both the ID and the default text must be string literals. Anything else is
// reported as an error rather than guessed at, because a computed message cannot
// be translated.
package msgscan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Placeholder is the token scripts/stamp_traceid.sh replaces with a fresh
// 8-character base36 ID. Entries still carrying it are not yet translatable.
const Placeholder = "PLACEHOLDER"

// helper is where a call site keeps its ID and its default text.
type helper struct {
	idArg   int
	textArg int
}

// pkgHelpers are functions on the i18n package itself.
var pkgHelpers = map[string]helper{
	"T":      {0, 1},
	"P":      {0, 1},
	"Errorf": {0, 1},
}

// methodHelpers are receiver-bound helpers whose ID/text are pushed to the right
// by leading positional or semantic-code arguments. The parser needs them because
// its diagnostics carry a token or a rule code before the ID, and every parser
// message must reach the catalog too.
var methodHelpers = map[string]helper{
	"saveErrorf":           {0, 1},
	"saveWarningf":         {0, 1},
	"saveWarningWithCodef": {1, 2}, // (code, id, format, ...)
	"saveErrorAt":          {0, 3}, // (id, line, col, format, ...)
	"saveWarningAt":        {0, 3},
	"errorf":               {1, 3}, // (tok, id, code, format, ...)
	"warnf":                {1, 3},
	"fatalf":               {1, 3},
}

// stripPrefixHelpers are the parser helpers that strip a leading "line N, column
// M: " position prefix from their format string at RUNTIME (parser.splitLocFormat),
// because the position lives in Diagnostic.Pos rather than in the translatable
// text. The generator must perform the same strip when extracting the default,
// otherwise en.noml would record the prefix that the runtime never translates and
// the verb-signature gate would compare against the wrong format. Helpers that
// receive the position as an explicit argument (…At / errorf / warnf / fatalf)
// already carry the body only, so they are not in this set.
var stripPrefixHelpers = map[string]bool{
	"saveErrorf":           true,
	"saveWarningf":         true,
	"saveWarningWithCodef": true,
}

// locPrefixFormats mirrors parser.locPrefixFormats: the machine-generated
// position prefix, longest first so a longer prefix wins over a shorter one that
// it subsumes.
var locPrefixFormats = []string{
	"line %d, column %d: ",
	"line %d: ",
}

// stripLocFormatPrefix removes a leading position prefix, matching what the parser
// does at runtime for stripPrefixHelpers. Texts without such a prefix are returned
// unchanged.
func stripLocFormatPrefix(text string) string {
	for _, p := range locPrefixFormats {
		if strings.HasPrefix(text, p) {
			return text[len(p):]
		}
	}
	return text
}

// Helpers is the union of every helper name, for documentation and tests.
var Helpers = func() map[string]bool {
	out := make(map[string]bool, len(pkgHelpers)+len(methodHelpers))
	for k := range pkgHelpers {
		out[k] = true
	}
	for k := range methodHelpers {
		out[k] = true
	}
	return out
}()

// Entry is one call site.
type Entry struct {
	ID   string
	Text string
	File string // path relative to the scan root, slash-separated
	Line int
}

// Result is the outcome of a scan.
type Result struct {
	Entries []Entry
	// Pending counts call sites whose ID is still Placeholder; they are excluded
	// from Entries because they have no stable key yet.
	Pending int
}

// Scan walks root and collects every i18n helper call site.
func Scan(root string) (*Result, error) {
	res := &Result{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return scanFile(root, path, res)
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(res.Entries, func(i, j int) bool {
		if res.Entries[i].File != res.Entries[j].File {
			return res.Entries[i].File < res.Entries[j].File
		}
		return res.Entries[i].Line < res.Entries[j].Line
	})
	return res, nil
}

func scanFile(root, path string, res *Result) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	// A file may use only receiver-bound helpers (the parser does), so a missing
	// i18n import is not a reason to skip it — local is simply "".
	local := i18nImportName(f)

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	var firstErr error
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || firstErr != nil {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		// A package-level helper (i18n.T) or a receiver-bound one (p.saveErrorf).
		var h helper
		if local != "" && ident.Name == local {
			ph, found := pkgHelpers[sel.Sel.Name]
			if !found {
				return true
			}
			h = ph
		} else {
			mh, found := methodHelpers[sel.Sel.Name]
			if !found {
				return true
			}
			h = mh
		}

		pos := fset.Position(call.Pos())
		if len(call.Args) <= h.textArg {
			firstErr = fmt.Errorf("%s:%d: %s needs at least %d arguments",
				rel, pos.Line, sel.Sel.Name, h.textArg+1)
			return false
		}
		id, ok := stringLit(call.Args[h.idArg])
		if !ok {
			firstErr = fmt.Errorf("%s:%d: %s: message ID must be a string literal",
				rel, pos.Line, sel.Sel.Name)
			return false
		}
		text, ok := stringLit(call.Args[h.textArg])
		if !ok {
			firstErr = fmt.Errorf("%s:%d: %s(%q, ...): default text must be a string literal",
				rel, pos.Line, sel.Sel.Name, id)
			return false
		}
		if id == Placeholder {
			res.Pending++
			return true
		}
		// The parser's f-variants strip a leading "line N, column M: " position
		// prefix at runtime (the position lives in Diagnostic.Pos). Mirror that
		// here so the recorded default matches what is actually translated.
		if stripPrefixHelpers[sel.Sel.Name] {
			text = stripLocFormatPrefix(text)
		}
		res.Entries = append(res.Entries, Entry{ID: id, Text: text, File: rel, Line: pos.Line})
		return true
	})
	return firstErr
}

// i18nImportName reports the local name the file binds the i18n package to, or
// "" when the file does not import it.
func i18nImportName(f *ast.File) string {
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if path != "github.com/lizongying/nolang/i18n" && !strings.HasSuffix(path, "/i18n") {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				return ""
			}
			return spec.Name.Name
		}
		return "i18n"
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// Texts collapses entries into id -> text, failing when one ID carries two
// different texts (the invariant that keeps a translation unambiguous).
func Texts(res *Result) (map[string]string, error) {
	out := make(map[string]string, len(res.Entries))
	for _, e := range res.Entries {
		if prev, ok := out[e.ID]; ok && prev != e.Text {
			return nil, fmt.Errorf("message ID %q maps to two different texts:\n  %q\n  %q",
				e.ID, prev, e.Text)
		}
		out[e.ID] = e.Text
	}
	return out, nil
}
