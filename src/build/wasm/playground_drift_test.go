//go:build !wasm

package wasm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// Regression guards for the Direct-WASM drift found while building the
// playground demo corpus. The native LLVM backend is the oracle: every output
// asserted below was first confirmed by compiling the same source with
// `no build` and running it natively.
//
// The structural assertions are deliberately tool-free (no wasmtime /
// wasm-tools needed) so the regressions are caught on any developer machine;
// the runtime assertions additionally run in CI where wasmtime is installed.

// loopPrologue is block/loop/block, each with an empty block type. The third
// block is the continue target: without it `continue` branches straight back
// to the loop head, skipping the loop-variable increment forever.
var loopPrologue = []byte{OpBlock, BlockTypeEmpty, OpLoop, BlockTypeEmpty, OpBlock, BlockTypeEmpty}

func startBody(t *testing.T, src string) ([]byte, []byte) {
	t.Helper()
	out := parseAndGenerate(t, src)
	bodies := codeSectionBodies(t, out)
	if len(bodies) == 0 {
		t.Fatal("no code section bodies")
	}
	return out, bodies[0]
}

func TestLoopLoweringHasContinueBlock(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"bounded-range", "i <- [0..8): {\n    i == 2 -> continue\n    i == 6 -> break\n    print('{i}')\n}\nprint('done')\n"},
		{"conditional-while", "n = 0\nkeep = true\n(keep) {\n    n = n + 1\n    n >= 3 -> keep = false\n}\nprint('n = {n}')\n"},
		{"unbounded-range", "t <- [0..): {\n    print('{t}')\n    t == 3 -> break\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, body := startBody(t, tc.src)
			if !bytes.Contains(body, loopPrologue) {
				t.Errorf("%s: code body is missing the block/loop/block prologue; "+
					"continue would skip the increment (or the loop was dropped)", tc.name)
			}
			validateWithWasmTools(t, mod)
		})
	}
}

func TestLoopAndConditionRuntimeMatchesNative(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"continue-runs-increment", "i <- [0..8): {\n    i == 2 -> continue\n    i == 6 -> break\n    print('i={i}')\n}\nprint('done')\n",
			"i=0\ni=1\ni=3\ni=4\ni=5\ndone"},
		{"conditional-while", "n = 0\nkeep = true\n(keep) {\n    n = n + 1\n    n >= 3 -> keep = false\n}\nprint('n = {n}')\n",
			"n = 3"},
		{"unbounded-range-breaks", "t <- [0..): {\n    print('{t}')\n    t == 3 -> break\n}\n",
			"0\n1\n2\n3"},
		{"match-arms-in-loop", "i <- [1..16): {\n    i % 15: {\n        0 -> print('FizzBuzz')\n        3 || 6 || 9 || 12 -> print('Fizz')\n        5 || 10 -> print('Buzz')\n        -> print('{i}')\n    }\n}\n",
			"1\n2\nFizz\n4\nBuzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod, _ := startBody(t, tc.src)
			validateWithWasmTools(t, mod)
			if stdout, ok := runWithWasmtime(t, mod); ok && !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout = %q, want substring %q", stdout, tc.want)
			}
		})
	}
}

func TestToStrInferTypeMatchesEmission(t *testing.T) {
	const src = "n = 42\nns = n.to-str()\nprint('n as str = {ns}')\n"
	g := generateForSignatureTest(t, src)
	g.currentFunc = "_start"

	l := lexer.New(src)
	p := parser.New(l)
	program := p.ParseProgram()
	if program == nil {
		t.Fatal("ParseProgram returned nil")
	}
	p.DesugarSurfaceMatchesPlain(program)
	var value parser.Expression
	for _, s := range program.Statements {
		if ls, ok := s.(*parser.LetStatement); ok && ls.Name != nil && ls.Name.Value == "ns" {
			value = ls.Value
		}
	}
	if value == nil {
		t.Fatal("could not find `ns = n.to-str()` statement")
	}
	if got := g.inferType(value); got != I32 {
		t.Errorf("inferType(n.to-str()) = %v, want I32 (emitToStrMethod returns I32)", got)
	}

	mod, _ := startBody(t, src)
	validateWithWasmTools(t, mod)
	if stdout, ok := runWithWasmtime(t, mod); ok && !strings.Contains(stdout, "n as str = 42") {
		t.Errorf("stdout = %q, want 'n as str = 42'", stdout)
	}
}

func TestBoolFormattingMatchesNative(t *testing.T) {
	const src = "t = true\nf = false\nprint('t = {t}, f = {f}')\n" +
		"lt = 3 < 7\neq = 3 == 7\nprint('lt = {lt}, eq = {eq}')\n" +
		"both = lt && !eq\nprint('both = {both}')\n"
	mod, _ := startBody(t, src)
	// Tool-free: emitting bool formatting must add the literals to the string pool.
	for _, want := range []string{"true", "false"} {
		if !bytes.Contains(mod, []byte(want)) {
			t.Errorf("module does not contain %q; bool formatting fell back to itoa", want)
		}
	}
	validateWithWasmTools(t, mod)
	if stdout, ok := runWithWasmtime(t, mod); ok {
		for _, want := range []string{"t = true, f = false", "lt = true, eq = false", "both = true"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout = %q, want substring %q", stdout, want)
			}
		}
	}
}

func TestStringIndexReadsOneByte(t *testing.T) {
	const src = "s = 'ABC'\nc = s[0]\nprint('s[0] = {c}')\n"
	mod, body := startBody(t, src)
	if !bytes.Contains(body, []byte{OpI32Load8U}) {
		t.Error("string index did not emit i32.load8_u (would read an i64 and print garbage)")
	}
	validateWithWasmTools(t, mod)
	if stdout, ok := runWithWasmtime(t, mod); ok && !strings.Contains(stdout, "s[0] = 65") {
		t.Errorf("stdout = %q, want 's[0] = 65'", stdout)
	}
}

func TestRandRandMasksEveryXorshiftStep(t *testing.T) {
	const src = "seed = 123456789\nstate = seed\ni <- [0..2): {\n    state, r = rand.rand(state)\n    print('r = {r}')\n}\n"
	mod, body := startBody(t, src)
	// i64.const 0xFFFFFFFF (signed LEB FF FF FF FF 0F) followed by i64.and.
	maskOp := []byte{OpI64Const, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F, OpI64And}
	if got := bytes.Count(body, maskOp); got != 4 {
		t.Errorf("found %d 32-bit masks, want 4 (input + 3 xorshift steps); "+
			"masking only the final result diverges from std/crypto/rand.no", got)
	}
	validateWithWasmTools(t, mod)

	const single = "seed = 123456789\nstate, r = rand.rand(seed)\nprint('r = {r}')\n"
	singleMod, _ := startBody(t, single)
	if stdout, ok := runWithWasmtime(t, singleMod); ok && !strings.Contains(stdout, "r = 2714967881") {
		t.Errorf("stdout = %q, want 'r = 2714967881' (native xorshift32 value)", stdout)
	}
}
