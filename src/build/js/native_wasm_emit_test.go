package js

import (
	"regexp"
	"strings"
	"testing"
)

// The WASM emit must, for bind=wasm, produce: the wasm import from the given
// specifier, the init factory (`ns["default"]`), the shared ready state, a ready
// Promise that is NOT awaited but that flips the state synchronously when the
// init is non-async, and one same-name forwarding wrapper per #{c} extern
// guarded by the ready state.
func TestWASMEmission(t *testing.T) {
	src := `#{bind = wasm, wasm = '@sqlite.org/sqlite-wasm'}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

#{c}
_sqlite3-close = (db *byte) (rc i32)
`
	g := NewGenerator()
	g.SetTargetEnv("browser")
	g.SetNativeConfig(nil, true)
	out, err := g.Generate(parseProgram(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	checks := []string{
		`import * as __wasm_ns_sqlite from "@sqlite.org/sqlite-wasm";`,
		`const __wasm_init_sqlite = __wasm_ns_sqlite.default;`,
		`const __wasm_state = globalThis.__nolang_wasm_state || (globalThis.__nolang_wasm_state = {});`,
		`const __wasm_ret_sqlite = __wasm_init_sqlite();`,
		`? __wasm_ret_sqlite.then(() => { __wasm_state["sqlite"] = true; })`,
		`: (__wasm_state["sqlite"] = true, Promise.resolve());`,
		`(globalThis.__nolang_wasm_ready || (globalThis.__nolang_wasm_ready = {}))["sqlite"] = __wasm_ready_sqlite;`,
		`const _sqlite3Open = (...args) => {`,
		`throw new Error('WASM module sqlite is not ready; await globalThis.__nolang_wasm_ready["sqlite"] before calling native functions');`,
		`return __wasm_ns_sqlite["sqlite3_open"](...args);`,
		`return __wasm_ns_sqlite["sqlite3_close"](...args);`,
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("missing WASM emission %q\n--- generated ---\n%s", c, out)
		}
	}
	// The ready Promise must never be awaited at the call sites: no *statement*
	// in the emitted module may start with `await`. (The word "await" also
	// appears inside the not-ready error string, which must not trigger this.)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "await ") {
			t.Errorf("emitted module must not await the ready promise:\n%s", line)
		}
	}
	// The wasm import path must NOT be registered as a module-object binding;
	// an extern referenced bare must resolve to the forwarding const, not a
	// `sqlite.xxx` property access.
	if regexp.MustCompile(`jsProp`).MatchString(out) {
		t.Errorf("unexpected jsProp helper leak:\n%s", out)
	}
}

// A wasm extern call is rewritten to the forwarding const, not a property
// access on the import short name.
func TestWASMForwardingNoModuleRewrite(t *testing.T) {
	src := `#{bind = wasm, wasm = 'pkg'}
# /src/sqlite
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)
`
	g := NewGenerator()
	g.SetTargetEnv("browser")
	g.SetNativeConfig(nil, true)
	out, err := g.Generate(parseProgram(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// No `const sqlite = ...` module object.
	if strings.Contains(out, "const sqlite =") {
		t.Errorf("wasm import must not be bound as a module object:\n%s", out)
	}
}

// Without a bind annotation the output must not contain any native machinery.
func TestNoBindNoNative(t *testing.T) {
	src := "#{c}\nfoo = (x i32) (y i32)\nbar = () { r = foo(1) }\nbar()\n"
	g := NewGenerator()
	g.SetTargetPlatform("js", "")
	out, err := g.Generate(parseProgram(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(out, "koffi") || strings.Contains(out, "__wasm") || strings.Contains(out, "__ffi") {
		t.Errorf("no-annotation program must not emit native machinery:\n%s", out)
	}
}
