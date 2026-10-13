package build

import (
	"strings"
	"testing"
)

// TestMainReservedNameIsRejected verifies that an explicit `main` function
// definition is rejected at compile time. Nolang synthesizes the entry point
// from top-level statements (see hir2mir.synthesizeMainForTopLevel), so `main`
// is a reserved name: a program must not define it explicitly. The check covers
// top-level definitions, definitions with results, nested definitions (inside
// another function body) and methods whose name is `main`.
func TestMainReservedNameIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"top-level", "main = () {\n  print('hi')\n}\n"},
		{"with-result", "main = () (r i64) {\n  r = 0\n}\n"},
		{"nested", "outer = () {\n  main = () {\n    print('x')\n  }\n}\n"},
		{"method", "str.main = () {}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranspiler(nil)
			_, err := tr.Compile(tc.src)
			if err == nil {
				t.Fatalf("expected `main` to be rejected as a reserved name, got no error")
			}
			if !strings.Contains(err.Error(), "reserved name") {
				t.Fatalf("expected reserved-name error, got: %v", err)
			}
		})
	}
}

// TestTopLevelEntryStillCompiles verifies the intended replacement form: a
// program consisting only of top-level statements (no explicit `main`) compiles,
// because the entry point is synthesized automatically.
func TestTopLevelEntryStillCompiles(t *testing.T) {
	tr := NewTranspiler(nil)
	if _, err := tr.Compile("print('hi')\n"); err != nil {
		t.Fatalf("top-level-statement program should compile, got: %v", err)
	}
}
