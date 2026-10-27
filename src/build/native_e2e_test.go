package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeNativeProject lays out a package.noml with compiler.link-libs plus a
// main.no whose native bind intent is supplied by main, returning main.no.
// linkLibs is the noml array body (single-quoted elements, comma separated).
func writeNativeProject(t *testing.T, linkLibs string, main string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  name: 'nativeffi'
  compiler: {
    emit: 'js'
    link-libs: [` + linkLibs + `]
  }
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

// A bind=ffi program with link-libs emits the koffi loader and symbol binding
// end to end through BuildJSEx (module merge + native policy + codegen).
func TestBuildNativeFFIEndToEnd(t *testing.T) {
	mainPath := writeNativeProject(t, `'sqlite3'`,
		`#{bind = ffi}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

main = () {
    r = _sqlite3-open('x.db', nil)
}
main()
`)
	res, err := BuildJSEx(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJSEx: %v", err)
	}
	if !strings.Contains(res.Code, `__ffi_libs = [__ffi_load("sqlite3")]`) {
		t.Errorf("ffi libs missing:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, `const _sqlite3Open = __ffi_fn("sqlite3_open", "int", ["str", koffi.pointer(koffi.pointer("uint8"))]);`) {
		t.Errorf("ffi binding missing:\n%s", res.Code)
	}
	// koffi must appear as a synthetic ESM import (sidecar + dependency check).
	foundKoffi := false
	for _, imp := range res.ESMImports {
		if imp.Specifier == "koffi" {
			foundKoffi = true
		}
	}
	if !foundKoffi {
		t.Errorf("koffi not in ESMImports: %+v", res.ESMImports)
	}
}

// bind=ffi forced to browser is a build-time error (never emits a doomed
// artifact).
func TestBuildNativeFFIBrowserRejected(t *testing.T) {
	mainPath := writeNativeProject(t, `'sqlite3'`,
		`#{bind = ffi}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)
`)
	_, err := BuildJSEx(mainPath, BuildOptions{UseJS: true, BrowserMode: true})
	if err == nil {
		t.Fatal("browser+ffi must be a build error")
	}
	if !strings.Contains(err.Error(), "browser") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A bind=ffi program with no #{c} extern is a build error.
func TestBuildNativeFFINoExtern(t *testing.T) {
	mainPath := writeNativeProject(t, `'sqlite3'`,
		`#{bind = ffi}
# /src/sqlite
main = () { }
main()
`)
	_, err := BuildJSEx(mainPath, BuildOptions{UseJS: true})
	if err == nil {
		t.Fatal("ffi without extern must be a build error")
	}
}

// A bind=wasm program declares its wasm specifier in package.noml and emits the
// wasm bootstrap; the wasm specifier reaches ESMImports for the browser import
// map.
func TestBuildNativeWasmEndToEnd(t *testing.T) {
	mainPath := writeWasmNativeProject(t,
		`#{bind = wasm, wasm = '@sqlite.org/sqlite-wasm'}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)
`)
	res, err := BuildJSEx(mainPath, BuildOptions{UseJS: true, BrowserMode: true})
	if err != nil {
		t.Fatalf("BuildJSEx: %v", err)
	}
	if !strings.Contains(res.Code, `import * as __wasm_ns_sqlite from "@sqlite.org/sqlite-wasm";`) {
		t.Errorf("wasm import missing:\n%s", res.Code)
	}
	if !strings.Contains(res.Code, `return __wasm_ns_sqlite["sqlite3_open"](...args);`) {
		t.Errorf("wasm forwarding missing:\n%s", res.Code)
	}
	foundWasm := false
	for _, imp := range res.ESMImports {
		if imp.Specifier == "@sqlite.org/sqlite-wasm" {
			foundWasm = true
		}
	}
	if !foundWasm {
		t.Errorf("wasm specifier not in ESMImports: %+v", res.ESMImports)
	}
}

func writeWasmNativeProject(t *testing.T, main string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  name: 'nativewasm'
  dependencies: ['@sqlite.org/sqlite-wasm:^3.49.0']
  compiler: { emit: 'js' }
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

// A program with no bind annotation must not engage native machinery.
func TestBuildNoNativeNoMachinery(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  name: 'plain'
  compiler: { emit: 'js' }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.no")
	if err := os.WriteFile(mainPath, []byte("main = () { print('hi') }\nmain()\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := BuildJSEx(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJSEx: %v", err)
	}
	if strings.Contains(res.Code, "koffi") || strings.Contains(res.Code, "__wasm") || strings.Contains(res.Code, "__ffi") {
		t.Errorf("no-annotation program emitted native machinery:\n%s", res.Code)
	}
	if len(res.ESMImports) != 0 {
		t.Errorf("no-annotation program has ESMImports: %+v", res.ESMImports)
	}
}

// writeLocalWasmProject lays out a project whose wasm specifier is a local
// file next to main.no — no npm dependencies at all.
func writeLocalWasmProject(t *testing.T, wasm string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  name: 'localwasm'
  compiler: { emit: 'js' }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	main := `#{bind = wasm, wasm = '` + wasm + `'}
# /math
#{c}
square = (x f64) (r f64)
`
	if err := os.WriteFile(filepath.Join(dir, "main.no"), []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "main.no")
}

// A relative wasm= specifier names a file next to the emitted output, not an
// npm package, so it must build with an empty dependency set.
func TestBuildWasmRelativeSpecifierNeedsNoDependency(t *testing.T) {
	mainPath := writeLocalWasmProject(t, "./math-wasm.mjs")
	res, err := BuildJSEx(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJSEx: %v", err)
	}
	if !strings.Contains(res.Code, `import * as __wasm_ns_math from "./math-wasm.mjs";`) {
		t.Errorf("local wasm import missing:\n%s", res.Code)
	}
	if len(res.ESMImports) != 1 || res.ESMImports[0].Specifier != "./math-wasm.mjs" {
		t.Fatalf("unexpected ESMImports: %+v", res.ESMImports)
	}
}

// A bare wasm= specifier is still an npm package and still needs a
// dependencies entry — the relaxation is scoped to relative paths.
func TestBuildWasmBareSpecifierStillRequiresDependency(t *testing.T) {
	mainPath := writeLocalWasmProject(t, "math-wasm-demo")
	if _, err := BuildJSEx(mainPath, BuildOptions{UseJS: true}); err == nil {
		t.Fatal("bare wasm specifier without a dependencies entry must fail")
	} else if !strings.Contains(err.Error(), "dependencies") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// LocalESMAssets reports the relative specifiers `no run --js` must link into
// its temp bundle directory, and nothing else.
func TestLocalESMAssets(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  name: 'localassets'
  compiler: { emit: 'js' }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	main := `#{bind = wasm, wasm = './math-wasm.mjs'}
# /math
#{c}
square = (x f64) (r f64)
`
	if err := os.WriteFile(filepath.Join(dir, "main.no"), []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := LocalESMAssets(filepath.Join(dir, "main.no"))
	if err != nil {
		t.Fatalf("LocalESMAssets: %v", err)
	}
	if len(got) != 1 || got[0] != "./math-wasm.mjs" {
		t.Fatalf("LocalESMAssets = %v, want [./math-wasm.mjs]", got)
	}

	// A program with no ESM imports at all reports none, without error.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "main.no"), []byte("main = () { print('hi') }\nmain()\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = LocalESMAssets(filepath.Join(plain, "main.no"))
	if err != nil {
		t.Fatalf("LocalESMAssets (plain): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LocalESMAssets (plain) = %v, want none", got)
	}
}

// CheckESMDeps must not report a relative specifier as a missing npm package:
// node resolves it against the bundle itself, not node_modules.
func TestCheckESMDepsSkipsRelativeSpecifiers(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  name: 'localdeps'
  compiler: { emit: 'js' }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	main := `#{bind = wasm, wasm = './math-wasm.mjs'}
# /math
#{c}
square = (x f64) (r f64)
`
	if err := os.WriteFile(filepath.Join(dir, "main.no"), []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	missing, _, err := CheckESMDeps(filepath.Join(dir, "main.no"))
	if err != nil {
		t.Fatalf("CheckESMDeps: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("relative specifier reported missing: %v", missing)
	}
}
