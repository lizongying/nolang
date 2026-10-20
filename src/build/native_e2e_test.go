package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeNativeProject lays out a package.jsonc with compiler.link-libs plus a
// main.no whose native bind intent is supplied by main, returning main.no.
func writeNativeProject(t *testing.T, linkLibs string, main string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{
  "name": "nativeffi",
  "compiler": {
    "emit": "js",
    "link-libs": [` + linkLibs + `]
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.jsonc"), []byte(manifest), 0644); err != nil {
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
	mainPath := writeNativeProject(t, `"sqlite3"`,
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
	mainPath := writeNativeProject(t, `"sqlite3"`,
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
	mainPath := writeNativeProject(t, `"sqlite3"`,
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

// A bind=wasm program declares its wasm specifier in package.jsonc and emits the
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
  "name": "nativewasm",
  "dependencies": { "@sqlite.org/sqlite-wasm": "^3.49.0" },
  "compiler": { "emit": "js" }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.jsonc"), []byte(manifest), 0644); err != nil {
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
  "name": "plain",
  "compiler": { "emit": "js" }
}
`
	if err := os.WriteFile(filepath.Join(dir, "package.jsonc"), []byte(manifest), 0644); err != nil {
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
