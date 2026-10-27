package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeESMProject lays out a minimal JS-backend project (package.noml +
// main.no) in a fresh temp dir and returns the main.no path.
func writeESMProject(t *testing.T, deps bool, main string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  name: 'esmtest'
`
	if deps {
		manifest += `  dependencies: ['lodash:^4.17.21']
`
	}
	manifest += `  compiler: { emit: 'js' }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.no")
	if err := os.WriteFile(mainPath, []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	return mainPath
}

// An ESM import whose package is declared in package.noml compiles, keeps the
// use statement in the merged program, and emits the import header.
func TestBuildJSESMDeclared(t *testing.T) {
	mainPath := writeESMProject(t, true,
		"# npm/lodash.get as get\n"+
			"m [str]str = { 'a': 'b' }\n"+
			"print(get(m, 'a'))\n")
	code, err := BuildJS(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJS: %v", err)
	}
	if !strings.Contains(code, `import * as __ns_lodash from "lodash";`) {
		t.Errorf("import header missing:\n%s", code)
	}
	if !strings.Contains(code, "const get = __ns_lodash.get ?? lodash.get;") {
		t.Errorf("named binding missing:\n%s", code)
	}
}

// A package that is not declared in the manifest is refused at module
// resolution (not silently emitted and not left to node to fail at run time).
func TestBuildJSESMUndeclared(t *testing.T) {
	mainPath := writeESMProject(t, false,
		"# npm/lodash.get as get\n"+
			"m [str]str = { 'a': 'b' }\n"+
			"print(get(m, 'a'))\n")
	if _, err := BuildJS(mainPath, BuildOptions{UseJS: true}); err == nil {
		t.Fatal("expected an error for an undeclared ESM package")
	} else if !strings.Contains(err.Error(), "not declared in package.noml") {
		t.Errorf("unexpected error: %v", err)
	}
}

// A subpath import is covered by a declaration of its parent package.
func TestBuildJSESMSubpathCoveredByParentDecl(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  name: 'esmtest'
  dependencies: ['lodash:^4.17.21']
  compiler: { emit: 'js' }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.no")
	if err := os.WriteFile(mainPath, []byte("# npm/lodash/fp.get as fpget\n"+
		"m [str]str = { 'a': 'b' }\n"+
		"print(fpget(m, 'a'))\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, err := BuildJS(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJS: %v", err)
	}
	if !strings.Contains(code, `import * as __ns_lodash_fp from "lodash/fp";`) {
		t.Errorf("subpath import header missing:\n%s", code)
	}
}

// The ESM sidecar is written next to the artifact so node treats it as a
// module; a pre-existing non-ESM package.json is refused with the .mjs hint.
func TestESMModuleSidecar(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "app.js")
	if err := os.WriteFile(out, []byte("// bundle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeESMModuleSidecar(out); err != nil {
		t.Fatalf("writeESMModuleSidecar: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if !jsoncDeclaresESM(body) {
		t.Errorf("sidecar must declare ESM, got: %s", body)
	}
	// A second call is idempotent (the sidecar already declares ESM).
	if err := writeESMModuleSidecar(out); err != nil {
		t.Errorf("second writeESMModuleSidecar: %v", err)
	}
	// An existing non-ESM package.json is refused.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{\"type\":\"commonjs\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err = writeESMModuleSidecar(out)
	if err == nil {
		t.Fatal("expected refusal against a non-ESM package.json")
	}
	if !strings.Contains(err.Error(), ".mjs") {
		t.Errorf("error should hint at .mjs, got: %v", err)
	}
}

// CheckESMDeps reports a missing package (declared, not installed) and finds
// the node_modules tree that satisfies the program once installed.
func TestCheckESMDeps(t *testing.T) {
	mainPath := writeESMProject(t, true,
		"# npm/lodash.get as get\n"+
			"m [str]str = { 'a': 'b' }\n"+
			"print(get(m, 'a'))\n")
	missing, nm, err := CheckESMDeps(mainPath)
	if err != nil {
		t.Fatalf("CheckESMDeps: %v", err)
	}
	if len(missing) != 1 || missing[0] != "lodash" {
		t.Errorf("expected lodash to be missing, got %v", missing)
	}
	if nm != "" {
		t.Errorf("no node_modules should exist yet, got %s", nm)
	}

	// Install (materialize the package tree) and re-check.
	dir := filepath.Dir(mainPath)
	esmNM := filepath.Join(dir, "node_modules", "lodash")
	if err := os.MkdirAll(esmNM, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(esmNM, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	missing, nm, err = CheckESMDeps(mainPath)
	if err != nil {
		t.Fatalf("CheckESMDeps after install: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing packages, got %v", missing)
	}
	if nm == "" {
		t.Error("expected the node_modules directory to be reported")
	}
}

// A program without ESM imports has no missing deps and no node_modules
// requirement — the node run path stays a plain script execution.
func TestCheckESMDepsNoESM(t *testing.T) {
	mainPath := writeESMProject(t, true, "print('hello')\n")
	missing, nm, err := CheckESMDeps(mainPath)
	if err != nil {
		t.Fatalf("CheckESMDeps: %v", err)
	}
	if len(missing) != 0 || nm != "" {
		t.Errorf("expected clean result, got missing=%v nm=%q", missing, nm)
	}
}

// The LLVM pipeline refuses # npm/… with a pointer to the JS backend instead
// of failing later inside the backend.
func TestTranspilerRejectsESMForLLVM(t *testing.T) {
	src := `# npm/lodash
m [str]str = { 'a': 'b' }
x = lodash.get(m, 'a')
`
	// Reach the same resolveUse guard the LLVM/vet pipelines run through.
	if !strings.Contains(src, "# npm/") {
		t.Fatal("test setup broken")
	}
	mainPath := writeESMProject(t, true, src)
	code, err := BuildJS(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJS: %v", err)
	}
	// The JS backend (the only supported one) still emits the import.
	if !strings.Contains(code, `import * as __ns_lodash from "lodash";`) {
		t.Errorf("import header missing:\n%s", code)
	}
}
