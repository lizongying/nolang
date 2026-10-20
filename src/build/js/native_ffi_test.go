package js

import (
	"strings"
	"testing"
)

// The FFI emit must, for bind=ffi with link-libs, produce: the koffi CJS-interop
// import, a platform-aware loader, the multi-library __ffi_fn resolver, and one
// binding per #{c} extern with the right symbol name, jsIdent local name and
// koffi argument types (including nested pointers).
func TestFFIEmissions(t *testing.T) {
	src := `#{bind = ffi}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

#{c}
_sqlite3-close = (db *byte) (rc i32)
`
	g := NewGenerator()
	g.SetTargetPlatform("js", "")
	g.SetNativeConfig([]string{"sqlite3"}, false)
	out, err := g.Generate(parseProgram(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	checks := []string{
		`import * as __ns_koffi from "koffi";`,
		`const koffi = __ns_koffi.default ?? __ns_koffi;`,
		`const __ffi_libs = [__ffi_load("sqlite3")];`,
		`const _sqlite3Open = __ffi_fn("sqlite3_open", "int", ["str", koffi.pointer(koffi.pointer("uint8"))]);`,
		`const _sqlite3Close = __ffi_fn("sqlite3_close", "int", [koffi.pointer("uint8")]);`,
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("missing FFI emission %q\n--- generated ---\n%s", c, out)
		}
	}
}

// A lib= override replaces the link-libs list entirely.
func TestFFIEmissionLibOverride(t *testing.T) {
	src := `#{bind = ffi, lib = 'libz.1.so'}
# /src/zlib
#{c}
crc32 = (v u32, buf *byte, sz u32) (crc u32)
`
	g := NewGenerator()
	g.SetTargetPlatform("js", "")
	g.SetNativeConfig([]string{"unused"}, false)
	out, err := g.Generate(parseProgram(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(out, `__ffi_libs = [__ffi_load("libz.1.so")]`) {
		t.Errorf("lib override not applied:\n%s", out)
	}
	if strings.Contains(out, `"unused"`) {
		t.Errorf("link-libs should be replaced by lib override:\n%s", out)
	}
	// This specifier has a dot, so it must be loaded verbatim (no platform
	// filename mangling) — the loader branch handles that at runtime.
	if !strings.Contains(out, `const crc32 = __ffi_fn("crc32", "uint", ["uint", koffi.pointer("uint8"), "uint"]);`) {
		t.Errorf("crc32 binding mismatch:\n%s", out)
	}
}

// A void extern (no result) must bind "void" as the return type.
func TestFFIEmissionVoidReturn(t *testing.T) {
	src := `#{bind = ffi}
# /src/zlib
#{c}
my-init = ()
`
	g := NewGenerator()
	g.SetTargetPlatform("js", "")
	g.SetNativeConfig([]string{"z"}, false)
	out, err := g.Generate(parseProgram(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(out, `const myInit = __ffi_fn("my_init", "void", []);`) {
		t.Errorf("void binding mismatch:\n%s", out)
	}
}

// bind=ffi on a browser target must fail at build time, not emit a doomed
// artifact.
func TestFFIEmissionBrowserRejected(t *testing.T) {
	src := `#{bind = ffi}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)
`
	g := NewGenerator()
	g.SetTargetEnv("browser")
	g.SetNativeConfig([]string{"sqlite3"}, true)
	_, err := g.Generate(parseProgram(t, src))
	if err == nil {
		t.Fatal("browser+ffi must be a build error")
	}
	if !strings.Contains(err.Error(), "browser") {
		t.Fatalf("error should mention the browser: %v", err)
	}
}

// bind=ffi with no #{c} extern is a build error (nothing to bind).
func TestFFIEmissionNoExternRejected(t *testing.T) {
	src := `#{bind = ffi}
# /src/sqlite
`
	g := NewGenerator()
	g.SetTargetPlatform("js", "")
	g.SetNativeConfig([]string{"sqlite3"}, false)
	_, err := g.Generate(parseProgram(t, src))
	if err == nil {
		t.Fatal("ffi without extern must be a build error")
	}
}
