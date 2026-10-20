package js

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func parseProgram(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatal("ParseProgram returned nil")
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// A program with no native intent must not engage the native machinery.
func TestCollectNativePolicyNone(t *testing.T) {
	prog := parseProgram(t, "x = 1\nprint(x)\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false, got policy %+v", p)
	}
}

// An ESM/npm import carries no native intent.
func TestCollectNativePolicyIgnoresESM(t *testing.T) {
	prog := parseProgram(t, "# npm/lodash\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for npm import, got %+v", p)
	}
}

// bind=ffi on an import is recognized.
func TestCollectNativePolicyFFI(t *testing.T) {
	prog := parseProgram(t, "#{bind = ffi}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || p.Mode != BindFFI {
		t.Fatalf("expected ffi policy, got found=%v %+v", found, p)
	}
}

// bind=wasm with an explicit specifier is recognized and the init default
// is filled.
func TestCollectNativePolicyWASM(t *testing.T) {
	prog := parseProgram(t, "#{bind = wasm, wasm = '@scope/pkg/wasm'}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || p.Mode != BindWASM {
		t.Fatalf("expected wasm policy, got found=%v %+v", found, p)
	}
	if p.WASM != "@scope/pkg/wasm" {
		t.Fatalf("unexpected wasm specifier %q", p.WASM)
	}
	if p.Init != "default" {
		t.Fatalf("expected default init, got %q", p.Init)
	}
}

// Conflicting explicit modes are rejected.
func TestCollectNativePolicyConflict(t *testing.T) {
	prog := parseProgram(t, "#{bind = ffi}\n# /src/a\n#{bind = wasm}\n# /src/b\n")
	_, _, err := CollectNativePolicy(prog)
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

// bind=ffi in the browser is rejected by ResolveNativePolicy.
func TestResolveNativePolicyBrowserFFIRejected(t *testing.T) {
	prog := parseProgram(t, "#{bind = ffi}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	if _, err := ResolveNativePolicy(p, found, true, nil); err == nil {
		t.Fatal("expected browser+ffi error")
	}
}

// Browser forces wasm even with bind=auto (a wasm= specifier is required).
func TestResolveNativePolicyBrowserForcesWASM(t *testing.T) {
	prog := parseProgram(t, "#{bind = auto, wasm = '@scope/pkg'}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	r, err := ResolveNativePolicy(p, found, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mode != BindWASM {
		t.Fatalf("expected wasm in browser, got %q", r.Mode)
	}
}

// Browser + bind=auto without a wasm= specifier is a build error.
func TestResolveNativePolicyBrowserAutoNoWasm(t *testing.T) {
	prog := parseProgram(t, "#{bind = auto}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	if _, err := ResolveNativePolicy(p, found, true, nil); err == nil {
		t.Fatal("expected browser+auto-without-wasm error")
	}
}

// bind=auto on node resolves to ffi when link libs are present.
func TestResolveNativePolicyAutoFFIWithLib(t *testing.T) {
	prog := parseProgram(t, "#{bind = auto}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	r, err := ResolveNativePolicy(p, found, false, []string{"sqlite3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mode != BindFFI {
		t.Fatalf("expected ffi, got %q", r.Mode)
	}
}

// bind=auto on node resolves to wasm when no link lib is present — but a wasm
// specifier is mandatory, so an omitted wasm= is an error.
func TestResolveNativePolicyAutoWASMNoLib(t *testing.T) {
	prog := parseProgram(t, "#{bind = auto}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	if _, err := ResolveNativePolicy(p, found, false, nil); err == nil {
		t.Fatal("expected missing wasm= specifier error")
	}
}

// bind=auto on node with a wasm= specifier (and no link lib) resolves to wasm.
func TestResolveNativePolicyAutoWASMWithSpec(t *testing.T) {
	prog := parseProgram(t, "#{bind = auto, wasm = '@scope/pkg'}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	r, err := ResolveNativePolicy(p, found, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mode != BindWASM || r.WASM != "@scope/pkg" {
		t.Fatalf("expected wasm/@scope/pkg, got %q/%q", r.Mode, r.WASM)
	}
}

// bind=ffi with neither lib= nor link-libs is rejected.
func TestResolveNativePolicyFFIRequiresLib(t *testing.T) {
	prog := parseProgram(t, "#{bind = ffi}\n# /src/sqlite\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil || !found {
		t.Fatalf("collect failed: found=%v err=%v", found, err)
	}
	if _, err := ResolveNativePolicy(p, found, false, nil); err == nil {
		t.Fatal("expected missing-lib error")
	}
}

// A #{c} extern may itself carry the policy (the "existing #{c}" annotation
// location from the plan).
func TestCollectNativePolicyFromExtern(t *testing.T) {
	prog := parseProgram(t, "#{bind = wasm, wasm = 'pkg/wasm', c}\nsqlite3-open = (s str) (rc i32)\n")
	p, found, err := CollectNativePolicy(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || p.Mode != BindWASM {
		t.Fatalf("expected wasm policy from extern, got found=%v %+v", found, p)
	}
	if p.WASM != "pkg/wasm" {
		t.Fatalf("unexpected wasm specifier %q", p.WASM)
	}
}
