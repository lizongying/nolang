//go:build !wasm

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Playground drift guards for the Direct-WASM backend.
//
// The browser playground compiles user code with
//
//	no build --wasm-direct -target wasm32-wasi
//
// which bypasses LLVM/MIR entirely. Recent language changes (D11 safe indexing,
// stricter option rules, std signature moves) mostly landed on the main path,
// so this table pins the *shapes the demos actually use* to the direct path.
// It is intentionally tool-free where possible: CI additionally runs the full
// demo corpus through `make playground-demos` with wasmtime.

func directWasmBuild(t *testing.T, src string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "input.no")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return BuildDirectWasm(path, BuildOptions{Target: "wasm32-wasi", UseDirectWasm: true})
}

func TestBuildDirectWasmPlaygroundShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"hello", "print('Hello, World!')\n"},
		{"named-format", "x i64 = 42\nprint('x={x}')\n"},
		// D1（2026-11-08）：plain 結果參數不能直接收 ?i64；用兩段式
		// match 解包，順便讓 Direct-WASM 路徑也覆蓋陳述式 match。
		{"plain-function", "add = (a i64, b i64) (r i64) {\n    r-opt = a + b\n    r-opt: {\n        ok -> r = it\n        ->\n    }\n}\nprint(add(2, 3))\n"},
		{"range-loop", "i <- [0..3): {\n    print('i={i}')\n}\n"},
		{"struct-method", "user {\n    name str\n}\nuser.greet = () {\n    print('hi')\n}\nu = user{\n    name: 'Alice'\n}\nu.greet()\n"},
		{"struct-method-return", "counter {\n    n i64\n}\ncounter.read = () (v i64) {\n    v = .n\n}\nc = counter{\n    n: 42\n}\nprint(c.read())\n"},
		{"safe-index-option", "a []i64 = [1, 2, 3]\nx = a[0]\nx: {\n    ok -> print('{it}')\n    err -> print('err')\n    nil -> print('nil')\n}\n"},
		// std/math.no keeps the variadic max/min; the direct backend used to
		// recognise only the stale `number.max/min` spelling and emitted
		// `unreachable` for the real one.
		{"math-max-min", "a = math.max(10.0, 20.0)\nb = math.min(10.0, 20.0)\nprint('max = {a}')\nprint('min = {b}')\n"},
		// std/number.no moved sqrt from a module function to a numeric method.
		{"sqrt-method-f64", "sq = 16.0.sqrt()\nprint('sqrt = {sq}')\n"},
		{"sqrt-method-i64", "sq = 16.sqrt()\nprint('sqrt = {sq}')\n"},
		// rand.rand returns TWO values; `state, r = ...` is the playground's
		// multi-assignment shape and must not leave a stray stack value.
		{"rand-multi-result", "seed = 123456789\nstate = seed\ni <- [0..3): {\n    state, r = rand.rand(state)\n    print('r = {r}')\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := directWasmBuild(t, tc.src)
			if err != nil {
				t.Fatalf("BuildDirectWasm failed: %v", err)
			}
			if len(out) < 8 || !bytes.Equal(out[:4], []byte{0x00, 0x61, 0x73, 0x6d}) {
				t.Fatalf("not a WASM module (len=%d)", len(out))
			}
			validateDirectWasmIfAvailable(t, out)
		})
	}
}

// TestDirectWasmValidateParity pins the four checker passes that
// validateProgram runs. These are the errors the LLVM path also rejects at
// compile time; if any of them slips through, a playground user sees an opaque
// WebAssembly runtime trap instead of a located diagnostic.
func TestDirectWasmValidateParity(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr string
	}{
		{"undefined-variable", "print(missing_name)\n", "is not defined"},
		{"assignment-type-mismatch", "x i64 = 1\nx = 'hello'\nprint(x)\n", "cannot assign str value to i64 variable"},
		{"argument-type-mismatch", "f = (a i64) { print(a) }\nf('not-an-int')\n", "expected 'i64', got 'str'"},
		{"valid-control", "x i64 = 1\nprint('x={x}')\n", ""},
		{"valid-option-control", "a []i64 = [1, 2, 3]\nx = a[0]\nprint('ok')\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := directWasmBuild(t, tc.src)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				validateDirectWasmIfAvailable(t, out)
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got a %d-byte module", tc.wantErr, len(out))
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// validateDirectWasmIfAvailable uses wasmtime to type-check the emitted module
// when the tool is installed (CI has it). On hosts without it the structural
// assertions in TestBuildDirectWasmPlaygroundShapes still apply.
func validateDirectWasmIfAvailable(t *testing.T, wasmBytes []byte) {
	t.Helper()
	path, err := exec.LookPath("wasmtime")
	if err != nil {
		return
	}
	if err := exec.Command(path, "--version").Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return // present but not executable on this host
		}
	}
	file := filepath.Join(t.TempDir(), "out.wasm")
	if err := os.WriteFile(file, wasmBytes, 0o644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	cmd := exec.Command(path, "run", file)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wasmtime rejected the module: %v\nstderr: %s", err, stderr.String())
	}
}
