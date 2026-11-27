//go:build !wasm

package wasm

import (
	"strings"
	"testing"
)

// Regression guards for three Direct-WASM option-unwrapping defects that made
// `?=` and value-matches silently wrong (or unloadable) in the browser while
// the native LLVM backend produced correct output. All three were found by
// running `docs/src/playground/examples.ts` through the playground's own WASI
// loader (`make playground-demos`); that gate is the primary guard, these tests
// are the narrower, unit-level ones.
//
// The expected stdout of every case below was confirmed against the native
// backend (`no run` on the same source).
//
//  1. `?=` desugars its success path into an assignment EXPRESSION on the
//     target (`s ?= s + 1` → ok arm ends with `s = it`). `emitAssignExpr` had
//     no *parser.Identifier case, so the write was dropped and the target kept
//     the zero initialisation inserted by the desugar — `s` was always 0.
//  2. An arm body whose last statement is itself an IfExpression (every
//     `ok -> it` value-match arm) was emitted through the void `emitIfExpr`
//     path; the following `local.set` then popped an empty stack and the whole
//     module failed WebAssembly validation ("can't pop empty stack in
//     set_local").
//  3. The match tag probe compares against the bare keywords `ok` / `nil` /
//     `err`. They are unbound identifiers, so they emitted `i64.const 0` and
//     `x == ok` degraded to `x == 0`: every non-zero success value took the
//     fallback arm. The backend has no option representation (only the success
//     path exists), so the probe must be constant-folded.

func TestUnwrapAssignWritesBackTarget(t *testing.T) {
	// `s ?= s + 1` inside a plain function region: the success arm's
	// `s = it` assignment must reach the local.
	const src = "f = (n i64) (r ?i64) {\n" +
		"    s i64 = 0\n" +
		"    s ?= s + 1\n" +
		"    r = s\n" +
		"}\n" +
		"v = f(5)\n" +
		"print('s = {v}')\n"

	mod := parseAndGenerate(t, src)
	validateWithWasmTools(t, mod)
	if out, ok := runWithWasmtime(t, mod); ok && !strings.Contains(out, "s = 1") {
		t.Errorf("stdout = %q, want %q (the ?= target write was dropped)", out, "s = 1")
	}
}

func TestUnwrapAssignInLoopBody(t *testing.T) {
	// Same defect, loop form: `t ?= a + b` in a range loop body. This is the
	// Fibonacci demo that printed 0 in the playground.
	const src = "fib = (n i64) (r ?i64) {\n" +
		"    a i64 = 0\n" +
		"    b i64 = 1\n" +
		"    i <- [0..n): {\n" +
		"        t ?= a + b\n" +
		"        a = b\n" +
		"        b = t\n" +
		"    }\n" +
		"    r = a\n" +
		"}\n" +
		"v = fib(10)\n" +
		"print('fib = {v}')\n"

	mod := parseAndGenerate(t, src)
	validateWithWasmTools(t, mod)
	if out, ok := runWithWasmtime(t, mod); ok && !strings.Contains(out, "fib = 55") {
		t.Errorf("stdout = %q, want %q", out, "fib = 55")
	}
}

func TestValueMatchArmChainStaysValid(t *testing.T) {
	// arm body = one value-match arm whose last statement is an IfExpression.
	// Must not emit `local.set` on an empty stack.
	const src = "add = (a i64, b i64) (r i64) {\n" +
		"    r = a + b: {\n" +
		"        ok -> it\n" +
		"\n" +
		"        -> 0\n" +
		"    }\n" +
		"}\n" +
		"s = add(3, 4)\n" +
		"print('s = {s}')\n"

	mod := parseAndGenerate(t, src)
	validateWithWasmTools(t, mod)
	if out, ok := runWithWasmtime(t, mod); ok && !strings.Contains(out, "s = 7") {
		// `s == 0` here means the `ok` probe degraded to `x == 0`.
		t.Errorf("stdout = %q, want %q (ok-tag probe must fold to true)", out, "s = 7")
	}
}

// TestNilArmStaysLiveForScalarOption pins the scalar-option `nil ->` arm that
// the playground `option-match` demo depends on. Commit 50b900c6 (v0.4.38)
// taught optionTagLiteral about *parser.NilLiteral so raw VFS options could
// compare their real tag, but the same recognition also folded legacy scalar
// `x == nil` to a constant false: `r = nil` stores 0 into an i64 local, yet
// the match probe never fired. A scalar NilLiteral must therefore fall through
// to the normal numeric comparison (see emitInfix in codegen.go).
func TestNilArmStaysLiveForScalarOption(t *testing.T) {
	const src = "safe-div = (a i64, b i64) (r ?i64) {\n" +
		"    b == 0 -> {\n" +
		"        r = nil\n" +
		"        return\n" +
		"    }\n" +
		"    r = a / b\n" +
		"}\n" +
		"d = safe-div(10, 0)\n" +
		"d: {\n" +
		"    nil -> print('undefined')\n" +
		"    -> print('wrong: {it}')\n" +
		"}\n"

	mod := parseAndGenerate(t, src)
	validateWithWasmTools(t, mod)
	if out, ok := runWithWasmtime(t, mod); ok && !strings.Contains(out, "undefined") {
		t.Errorf("stdout = %q, want %q (scalar nil tag probe must not fold to false)", out, "undefined")
	}
}

func TestValueMatchFieldTargetStores(t *testing.T) {
	// `self.n = <value match>`: the address and the arm result go through
	// temporaries, then one store. Before the fix this emitted a store with
	// nothing on the stack and the module was rejected at validation.
	const src = "counter {\n" +
		"    n i64\n" +
		"}\n" +
		"counter.bump = () {\n" +
		"    .n = .n + 1: {\n" +
		"        ok -> it\n" +
		"\n" +
		"        -> .n\n" +
		"    }\n" +
		"}\n" +
		"c = counter{\n" +
		"    n: 41\n" +
		"}\n" +
		"c.bump()\n" +
		"v = c.n\n" +
		"print('n = {v}')\n"

	mod := parseAndGenerate(t, src)
	validateWithWasmTools(t, mod)
	if out, ok := runWithWasmtime(t, mod); ok && !strings.Contains(out, "n = 42") {
		t.Errorf("stdout = %q, want %q", out, "n = 42")
	}
}
