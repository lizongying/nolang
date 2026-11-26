package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildImportedQualifiedEnumValue pins plain-enum values referenced through
// both their module and type namespace. Module merging prefixes the type name,
// while the enum variant table retains the bare type key.
func TestBuildImportedQualifiedEnumValue(t *testing.T) {
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
	write("mod-enum.no", `affinity {
    integer-affinity,
    text-affinity,
}

truth {
    sql-false,
    sql-true,
    unknown,
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-enum as enums

a = enums.affinity.integer-affinity
print(a)
t = enums.truth.sql-true
print(t)
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	if _, err := trans.Compile(string(src)); err != nil {
		t.Fatalf("imported qualified enum value failed to compile: %v", err)
	}
}

// A bare variant inside its defining imported module must resolve even when
// module merging stores the enum metadata under a qualified key. In particular,
// `unknown` must not collapse to zero merely because std also has code.unknown.
func TestBuildImportedBareEnumVariantUsesQualifiedMetadata(t *testing.T) {
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
	write("mod-truth.no", `truth {
    sql-false,
    sql-true,
    unknown,
}

read = () (out truth) {
    out = unknown
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-truth as value

print(value.read())
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	ir, err := trans.Compile(string(src))
	if err != nil {
		t.Fatalf("imported bare enum variant failed to compile: %v", err)
	}
	start := strings.Index(ir, "define void @value_read(")
	if start < 0 {
		t.Fatal("imported read function missing from IR")
	}
	rest := ir[start:]
	end := strings.Index(rest, "\ndefine ")
	if end < 0 {
		end = len(rest)
	}
	body := rest[:end]
	if !strings.Contains(body, "store i64 2,") {
		t.Fatalf("imported truth.unknown must lower to discriminant 2\n%s", body)
	}
}
