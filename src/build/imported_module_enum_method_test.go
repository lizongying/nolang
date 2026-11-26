package build

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildImportedEnumMethodPayload pins imported tagged-enum method lowering.
// The module is valid on its own; after merging, its method and receiver are
// prefixed while its enum metadata retains the bare enum name.
func TestBuildImportedEnumMethodPayload(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workspace.noml", `{ test: '.' }`)
	write("mod-value.no", `sql-value {
    sql-null,
    integer(v i64),
    real(v f64),
    text(v str),
}

sql-value.read-method = () (out i64) {
    out = 0
    current = .
    current: {
        integer(n) -> out = n

        real(n) -> out = n.to-i64()

        text(s) -> out = s.len-bytes()

        sql-null ->
    }
}

integer-value = (v i64) (out sql-value) {
    out = integer(v)
}

text-value = (v str) (out sql-value) {
    out = text(v)
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-value as value

a = value.integer-value(7)
print(a.read-method())
b = value.text-value('hello')
print(b.read-method())
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	if _, err := trans.Compile(string(src)); err != nil {
		t.Fatalf("imported enum method failed to compile: %v", err)
	}
}
