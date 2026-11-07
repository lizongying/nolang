//go:build !wasm

package wasm

import (
	"bytes"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// Regression guard for the playground's numeric demos.
//
// Direct-WASM keeps a hand-written whitelist of std calls (it has no std import
// graph). Two drift points broke `math.max/min` and `x.sqrt()` in the browser:
//
//  1. emitModuleCall only recognised `number.max/min`, but the real variadic
//     functions live in std/math.no as `math.max/min`. std/number.no moved
//     sqrt to the method form `f64.sqrt()`, which emitMethodCall did not know.
//     Both fell through to OpUnreachable and trapped at runtime.
//  2. inferType still answered I64 for those calls, so emitLetStmt declared an
//     I64 local for an F64 result and the whole module failed validation with
//     "set_local to type F64 expected I64".
//
// The first test is tool-free: it asserts the real WASM numeric opcode is
// emitted. The second pins the inference/emission agreement directly.

func codeSectionBodies(t *testing.T, mod []byte) [][]byte {
	t.Helper()
	body := findSection(mod, CodeSection)
	if body == nil {
		t.Fatal("code section not found")
	}
	count, n := readU32LEB128(body)
	off := n
	bodies := make([][]byte, 0, count)
	for i := uint32(0); i < count; i++ {
		size, sn := readU32LEB128(body[off:])
		off += sn
		if off+int(size) > len(body) {
			t.Fatalf("code body %d overruns section (off=%d size=%d len=%d)", i, off, size, len(body))
		}
		bodies = append(bodies, body[off:off+int(size)])
		off += int(size)
	}
	return bodies
}

func TestNumericApiEmitsWasmMathOpcodes(t *testing.T) {
	const (
		opF64Min  = byte(0xA4)
		opF64Max  = byte(0xA5)
		opF64Sqrt = byte(0x9F)
	)
	cases := []struct {
		name string
		src  string
		want byte
	}{
		{"math-max", "a = math.max(10.0, 20.0)\nprint('{a}')\n", opF64Max},
		{"math-min", "a = math.min(10.0, 20.0)\nprint('{a}')\n", opF64Min},
		{"number-max-legacy", "a = number.max(10.0, 20.0)\nprint('{a}')\n", opF64Max},
		{"f64-sqrt-method", "sq = 16.0.sqrt()\nprint('{sq}')\n", opF64Sqrt},
		{"i64-sqrt-method", "sq = 16.sqrt()\nprint('{sq}')\n", opF64Sqrt},
		{"math-sqrt-legacy", "sq = math.sqrt(16.0)\nprint('{sq}')\n", opF64Sqrt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := parseAndGenerate(t, tc.src)
			found := false
			for _, body := range codeSectionBodies(t, out) {
				if bytes.IndexByte(body, tc.want) >= 0 {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no 0x%02x opcode in code section; the call was probably lowered to unreachable", tc.want)
			}
			validateWithWasmTools(t, out)
			if stdout, ok := runWithWasmtime(t, out); ok && stdout == "" {
				t.Error("program produced no stdout")
			}
		})
	}
}

// Non-vacuity control: a program with no numeric call must NOT contain those
// opcodes, otherwise the assertion above would pass on any module.
func TestNumericApiOpcodeCheckIsNotVacuous(t *testing.T) {
	out := parseAndGenerate(t, "print('plain text')\n")
	for _, body := range codeSectionBodies(t, out) {
		for _, op := range []byte{0xA4, 0xA5, 0x9F} {
			if bytes.IndexByte(body, op) >= 0 {
				t.Errorf("unexpected numeric opcode 0x%02x in a text-only program", op)
			}
		}
	}
}

// TestNumericCallInferTypeMatchesEmission pins the local-slot half of the
// drift: the type inferred for the binding must equal what emitMethodCall /
// emitModuleCall actually push on the stack.
func TestNumericCallInferTypeMatchesEmission(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want ValType
	}{
		{"math-max", "a = math.max(10.0, 20.0)\nprint('{a}')\n", F64},
		{"math-min", "a = math.min(10.0, 20.0)\nprint('{a}')\n", F64},
		{"math-sqrt-legacy", "a = math.sqrt(16.0)\nprint('{a}')\n", F64},
		{"f64-sqrt-method", "a = 16.0.sqrt()\nprint('{a}')\n", F64},
		{"i64-sqrt-method", "a = 16.sqrt()\nprint('{a}')\n", I64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lexer.New(tc.src)
			p := parser.New(l)
			program := p.ParseProgram()
			if program == nil {
				t.Fatal("ParseProgram returned nil")
			}
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parser errors: %v", errs)
			}
			p.DesugarSurfaceMatchesPlain(program)
			if len(program.Statements) == 0 {
				t.Fatal("no statements")
			}
			ls, ok := program.Statements[0].(*parser.LetStatement)
			if !ok || ls.Value == nil {
				t.Fatalf("first statement is not a let with a value: %T", program.Statements[0])
			}
			g := &Generator{}
			if got := g.inferType(ls.Value); got != tc.want {
				t.Errorf("inferType = %v, want %v", got, tc.want)
			}
		})
	}
}
