package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// useBindAnnotations parses src and returns the annotation entries attached to
// the program's single UseStatement.
func useBindAnnotations(t *testing.T, src string) ([]*AnnotationEntry, []string) {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatal("ParseProgram returned nil")
	}
	var use *UseStatement
	for _, stmt := range prog.Statements {
		if u, ok := stmt.(*UseStatement); ok {
			use = u
			break
		}
	}
	if use == nil {
		t.Fatalf("no UseStatement parsed from %q, statements: %d", src, len(prog.Statements))
	}
	return prog.Sem.AnnotationsOf(use), p.Errors()
}

// TestUseAnnotationOwnLineAbove pins that `#{bind = ...}` may be written on its
// own line above a `# import` directive and still attaches to the import. The
// native-call policy (browser → wasm, node → koffi) is declared on the import
// line, so it must survive the parser's attach step; without USE in the
// "attachable statement" set the annotation degrades into a standalone
// AnnotationStatement the JS backend never consults.
func TestUseAnnotationOwnLineAbove(t *testing.T) {
	entries, errs := useBindAnnotations(t, "#{bind = ffi}\n# /src/sqlite\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 annotation entry on the use statement, got %d", len(entries))
	}
	if entries[0].Key != "bind" {
		t.Fatalf("expected key %q, got %q", "bind", entries[0].Key)
	}
	v, ok := entries[0].Value.(*AnnotationIdentValue)
	if !ok {
		t.Fatalf("expected an ident value, got %T", entries[0].Value)
	}
	if v.Value != "ffi" {
		t.Fatalf("expected value %q, got %q", "ffi", v.Value)
	}
}

// TestUseAnnotationTrailing pins the same-line trailing form
// (`# import #{bind = wasm, wasm = '@sqlite.org/sqlite-wasm'}`) which the
// generic trailing-attach path already supports.
func TestUseAnnotationTrailing(t *testing.T) {
	entries, errs := useBindAnnotations(t, "# /src/sqlite #{bind = ffi}\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(entries) != 1 || entries[0].Key != "bind" {
		t.Fatalf("expected one bind entry, got %+v", entries)
	}
	if !entries[0].Trailing {
		t.Fatal("expected the entry to be marked Trailing")
	}
}

// TestUseAnnotationPrefixStillRejected keeps the placement rule uniform: writing
// `#{...}` ahead of the import on the *same* line is the same error as for every
// other statement kind, not a silent no-op.
func TestUseAnnotationPrefixStillRejected(t *testing.T) {
	_, errs := useBindAnnotations(t, "#{bind = ffi} # /src/sqlite\n")
	if len(errs) == 0 {
		t.Fatal("same-line prefix annotation on a use must be rejected")
	}
	if joined := strings.Join(errs, "\n"); !strings.Contains(joined, "前方同一行") {
		t.Fatalf("errors %v do not mention the prefix rule", errs)
	}
}

// TestUseAnnotationStringValue pins that a specifier containing characters the
// ident parser rejects (`@`, `.` inside a URL) must be quoted.
func TestUseAnnotationStringValue(t *testing.T) {
	entries, errs := useBindAnnotations(t, "#{bind = wasm, wasm = '@sqlite.org/sqlite-wasm'}\n# /src/sqlite\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	byKey := map[string]*AnnotationEntry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	w, ok := byKey["wasm"].Value.(*AnnotationStringValue)
	if !ok {
		t.Fatalf("expected wasm value to be a string, got %T", byKey["wasm"].Value)
	}
	if w.Value != "@sqlite.org/sqlite-wasm" {
		t.Fatalf("unexpected wasm specifier %q", w.Value)
	}
}
