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
	if !pkg.Publish {
		t.Error("Publish = false, want true (LoadPackage defaults publish to true)")
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
	// nolang 本身不是依賴；dev-dependencies 省略時就是空。
	if len(pkg.DevDependencies) != 0 {
		t.Errorf("DevDependencies = %v, want empty", pkg.DevDependencies)
	}
	// tests/package.noml 的 ignore 是空 vec（見該檔）；模式語意由
	// TestPackageIsIgnoredPatterns 覆蓋。
	if len(pkg.Ignore) != 0 {
		t.Errorf("Ignore = %v, want empty", pkg.Ignore)
	}
}

// output 省略时默认为当前包目录下的 dist，显式值一律以 workspace 根为准。
func TestResolveOutputDir(t *testing.T) {
	ws := t.TempDir()
	pkgRoot := filepath.Join(ws, "tests")

	cases := []struct {
		name string
		pkg  *Package
		want string
	}{
		{"default under package", &Package{RootDir: pkgRoot, workspaceRoot: ws}, filepath.Join(pkgRoot, "dist")},
		{"workspace absolute", &Package{RootDir: pkgRoot, workspaceRoot: ws, Output: "/tests/dist"}, filepath.Join(ws, "tests", "dist")},
		{"workspace relative", &Package{RootDir: pkgRoot, workspaceRoot: ws, Output: "./dist"}, filepath.Join(ws, "dist")},
		{"root package", &Package{RootDir: ws, workspaceRoot: ws}, filepath.Join(ws, "dist")},
		{"no workspace", &Package{RootDir: filepath.Join(ws, "solo")}, filepath.Join(ws, "solo", "dist")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pkg.ResolveOutputDir(); got != tc.want {
				t.Errorf("ResolveOutputDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ignore 模式既匹配檔案，也匹配整棵被忽略的目錄；相对路径直接以 RootDir 為基準。
func TestPackageIsIgnoredPatterns(t *testing.T) {
	p := &Package{
		RootDir: "/proj",
		Ignore:  []string{"*_skip.no", "build-cache", "src/*_skip", "dist/"},
	}
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"foo_skip.no", false, true},
		{"src/foo_skip.no", false, true},
		{"foo.no", false, false},
		{"build-cache/a.no", false, true},
		{"/proj/build-cache/a.no", false, true},
		{"src/gen_skip/a.no", false, true},
		{"dist/a.no", false, true},
		{"dist", false, false}, // 以 / 結尾的模式不命中同名檔案
		{"dist", true, true},
		{"other/a.no", false, false},
	}
	for _, tc := range cases {
		got := p.IsIgnored(tc.path)
		if tc.isDir {
			got = p.IsIgnoredDir(tc.path)
		}
		if got != tc.want {
			t.Errorf("IsIgnored(%q, dir=%v) = %v, want %v", tc.path, tc.isDir, got, tc.want)
		}
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
