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

// Indexing an imported enum vector and unwrapping the safe-index option can keep
// the element's bare type spelling, while module merging registers the method
// under its qualified type name. The call must resolve that unique qualified
// method instead of emitting an unknown callee such as `sample.read-method`.
func TestBuildImportedEnumMethodOnBareReceiver(t *testing.T) {
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
	write("mod-value.no", `sample {
    integer(v i64),
    text(v str),
}

sample.read-method = () (out i64) {
    current = .
    current: {
        integer(n) -> out = n

        text(s) -> out = s.len-bytes()
    }
}

integer-value = (v i64) (out sample) {
    out = integer(v)
}
`)
	write("mod-codec.no", `# /mod-value as value

sample-result {
    valid(values []sample),
    invalid,
}

wrap = (values []sample) (out sample-result) {
    out = valid(values)
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-codec as codec
# /mod-value as value

read-first = () (out i64) {
    decoded = codec.wrap([value.integer-value(7)])
    decoded: {
        valid(values) -> {
            first = values[0]: {
                ok(column) -> column

                -> value.integer-value(0)
            }
            out = first.read-method()
        }

        -> out = 0
    }
}

print(read-first())
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	if _, err := trans.Compile(string(src)); err != nil {
		t.Fatalf("imported enum method on bare receiver failed to compile: %v", err)
	}
}
