package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
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

// Imported single-field payload bindings must project the variant payload even
// when the consumer module spells the imported enum type without its alias.
func TestBuildImportedTaggedEnumPayloadDestructuring(t *testing.T) {
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
    sql-null,
    integer(v i64),
    real(v f64),
    text(v str),
    blob(v []byte),
}

text-value = (v str) (out sample) {
    out = text(v)
}

blob-value = (v []byte) (out sample) {
    out = blob(v)
}
`)
	// Merge the consumer first. Its sample-result payload interns sample before
	// sample's own definition is visited, reproducing the import-order bug.
	write("mod-codec.no", `# /mod-value as value

sample-result {
    valid(value sample, next i64),
    invalid,
}

payload-len = (v sample) (out i64) {
    out = 0
    v: {
        sql-null -> out = 0

        integer(number) -> out = number

        real -> out = 1

        text(text-value) -> out = text-value.len-bytes()

        blob(blob-value) -> out = blob-value.len()
    }
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-codec as codec
# /mod-value as value

print(codec.payload-len(value.text-value('abc')))
print(codec.payload-len(value.blob-value([1, 2, 3, 4])))
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	if _, err := trans.Compile(string(src)); err != nil {
		t.Fatalf("imported tagged-enum payload destructuring failed to compile: %v", err)
	}
}

// Vetting an imported consumer module directly runs undefined-variable validation
// before module merging. Bare tagged-enum variants from its local imports must be
// visible in that pass just as they are when the same module is reached through a
// package entry point.
func TestVetImportedTaggedEnumBareVariants(t *testing.T) {
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
    sql-null,
    integer(v i64),
    real(v f64),
    text(v str),
    blob(v []byte),
}
`)
	codecPath := filepath.Join(dir, "mod-codec.no")
	write("mod-codec.no", `# /mod-value as value

payload-len = (v sample) (out i64) {
    out = 0
    v: {
        sql-null -> out = 0

        integer(number) -> out = number

        real -> out = 1

        text(text-value) -> out = text-value.len-bytes()

        blob(blob-value) -> out = blob-value.len()
    }
}
`)

	lints, err := VetFileWithLints(codecPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	for _, l := range lints {
		if l.Severity == checker.LintError {
			t.Errorf("imported tagged-enum variants must be visible to vet: %s:%d:%d %s [%s]",
				l.File, l.Line, l.Column, l.Message, l.TraceID)
		}
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
