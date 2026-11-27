package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A global initialized by an imported constructor must recover the imported
// struct result type before reachability-driven MIR lowering starts. Otherwise
// the global falls back to i64 and a later method call resolves as i64.<method>.
func TestBuildImportedConstructorInitializedGlobalMethodResolves(t *testing.T) {
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
	write("mod-probe.no", `probe {
    value i64
}

new-probe = () (out probe) {
    out = probe{value: 7}
}

probe.run = () {
    .value = 8
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-probe

DB = new-probe()

run = () {
    DB.run()
    print(DB.value)
}

run()
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	ir, err := trans.Compile(string(src))
	if err != nil {
		t.Fatalf("imported constructor-initialized global method call failed: %v", err)
	}
	if !strings.Contains(ir, "@DB = private global %mod_probe_probe zeroinitializer") {
		t.Fatalf("imported constructor global lost its struct type:\n%s", ir)
	}
	if !strings.Contains(ir, "mod_probe_probe_run") {
		t.Fatalf("global receiver method definition was not emitted:\n%s", ir)
	}
	if !strings.Contains(ir, "@new_probe") {
		t.Fatalf("imported global constructor was not executed at startup:\n%s", ir)
	}
}

// A method may extend a struct imported from another module. Module merging
// must not prefix the already-qualified receiver type a second time.
func TestBuildImportedTypeMethodExtensionResolves(t *testing.T) {
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
	write("mod-probe.no", `probe {
    value i64
}

new-probe = () (out probe) {
    out = probe{value: 7}
}
`)
	write("mod-extension.no", `# /mod-probe

probe.run = () {
    .value = 8
}
`)
	mainPath := filepath.Join(dir, "main.no")
	write("main.no", `# /mod-probe
# /mod-extension

DB = new-probe()

run = () {
    DB.run()
    print(DB.value)
}

run()
`)

	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	ir, err := trans.Compile(string(src))
	if err != nil {
		t.Fatalf("imported type method extension failed: %v", err)
	}
	if !strings.Contains(ir, "define void @mod_probe_probe_run(") {
		t.Fatalf("imported type method extension was not emitted with its canonical name:\n%s", ir)
	}
	if strings.Contains(ir, "mod_probe_probe_probe_run") {
		t.Fatalf("imported receiver type was prefixed twice:\n%s", ir)
	}
}

// Package export filtering applies to direct imports from outside the package.
// Private modules pulled in transitively by an exported API must remain available,
// and package-local tests may import private modules directly.
func TestBuildPackageExportKeepsPrivateTransitiveDependencies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
	}{
		{name: "same package", entry: "pkg/tests/main.no"},
		{name: "external package", entry: "main.no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			write("pkg/package.noml", `{ name: 'probe-pkg' }`)
			write("pkg/lib.no", "@ /pkg/src/api.run\n")
			write("pkg/src/helper.no", `answer = () (out i64) {
    out = 42
}
`)
			write("pkg/src/api.no", `# /pkg/src/helper

run = () (out i64) {
    out = helper.answer()
}
`)
			write(tc.entry, `# /pkg/src/api

api.run()
`)

			mainPath := filepath.Join(dir, tc.entry)
			src, err := os.ReadFile(mainPath)
			if err != nil {
				t.Fatal(err)
			}
			trans := NewTranspiler(nil)
			trans.sourcePath = mainPath
			ir, err := trans.Compile(string(src))
			if err != nil {
				t.Fatalf("exported API lost its private dependency: %v", err)
			}
			if !strings.Contains(ir, "@answer(") || !strings.Contains(ir, "@run(") {
				t.Fatalf("exported API or private helper was not emitted:\n%s", ir)
			}
		})
	}
}
