package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPackageTestDir(t *testing.T) {
	// 確保 tests/package.noml 能被正確解析
	pkg, err := LoadPackage("../../tests")
	if err != nil {
		t.Fatalf("LoadPackage(tests) error: %v", err)
	}
	if pkg == nil {
		t.Fatal("LoadPackage(tests) returned nil")
	}
	if pkg.Name != "my-project" {
		t.Errorf("Name = %q, want %q", pkg.Name, "my-project")
	}
	// package.noml 的 version 約定帶前導 v（見 commit 3f88ca23 與 docs/docs/usage.md），
	// 編譯器僅在比較時對前綴不敏感，解析結果原樣保留前導 v。
	if pkg.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", pkg.Version, "v1.0.0")
	}
	if len(pkg.Keywords) != 1 || pkg.Keywords[0] != "nolang" {
		t.Errorf("Keywords = %v, want [nolang]", pkg.Keywords)
	}
	if pkg.Author != "lizongying" {
		t.Errorf("Author = %q, want %q", pkg.Author, "lizongying")
	}
	if pkg.Repository != "https://github.com/lizongying/nolang" {
		t.Errorf("Repository = %q, want %q", pkg.Repository, "https://github.com/lizongying/nolang")
	}
	if pkg.License != "Apache-2.0" {
		t.Errorf("License = %q, want %q", pkg.License, "Apache-2.0")
	}
	if pkg.DevDependencies == nil {
		t.Error("DevDependencies is nil")
	} else if pkg.DevDependencies["nolang"] != "^0.1.0" {
		t.Errorf("DevDependencies[nolang] = %q, want %q", pkg.DevDependencies["nolang"], "^0.1.0")
	}
	if len(pkg.Ignore) != 1 || pkg.Ignore[0] != "dist" {
		t.Errorf("Ignore = %v, want [dist]", pkg.Ignore)
	}
}

// TestCompilerLang covers the manifest read that runs before i18n is
// initialised: the nearest package.noml wins, a parent manifest is found by
// walking up, and every unusable state degrades to "" instead of an error (a
// broken config must not stop `no` from printing its own diagnostics).
func TestCompilerLang(t *testing.T) {
	root := t.TempDir()
	write := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, PackageManifestName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(root, `{
  name: 'outer'
  compiler: {
    version: 'v0.1.0'
    lang: 'zh-tw'
  }
}
`)
	nested := filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := CompilerLang(root); got != "zh-tw" {
		t.Errorf("CompilerLang(root) = %q, want zh-tw", got)
	}
	if got := CompilerLang(nested); got != "zh-tw" {
		t.Errorf("CompilerLang(nested) = %q, want zh-tw (walk up to the manifest)", got)
	}

	// A nearer manifest shadows the outer one — the language is a property of
	// the package you are in, not of the workspace.
	write(nested, "{\n  name: 'inner'\n  compiler: {\n    lang: 'ja'\n  }\n}\n")
	if got := CompilerLang(nested); got != "ja" {
		t.Errorf("CompilerLang(nested) = %q, want ja (nearest manifest wins)", got)
	}

	// No lang member at all.
	write(nested, "{\n  name: 'inner'\n  compiler: {\n    version: 'v0.2.0'\n  }\n}\n")
	if got := CompilerLang(nested); got != "" {
		t.Errorf("CompilerLang = %q, want empty when compiler.lang is absent", got)
	}

	// Malformed manifest: silence, not a crash.
	write(nested, "{\n  compiler: {\n")
	if got := CompilerLang(nested); got != "" {
		t.Errorf("CompilerLang = %q, want empty on a parse error", got)
	}

	// No manifest anywhere.
	if got := CompilerLang(t.TempDir()); got != "" {
		t.Errorf("CompilerLang = %q, want empty with no package.noml", got)
	}
}
