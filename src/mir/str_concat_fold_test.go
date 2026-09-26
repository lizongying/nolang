package mir

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// lowerSrcWithDiags parses + lowers `src` and returns the module together with
// the lowering diagnostics, so a test can assert on BOTH the emitted
// instructions and the "unsupported construct" fallbacks.
func lowerSrcWithDiags(t *testing.T, src string) (*Module, []LowerDiag) {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pkg := parser.ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	mod, _, diags := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil module")
	}
	return mod, diags
}

func interpDiags(diags []LowerDiag) []string {
	var out []string
	for _, d := range diags {
		if d.Kind == "interp" {
			out = append(out, d.Msg)
		}
	}
	return out
}

// strConsts returns the multiset of string constants materialized in the module.
func strConsts(m *Module) map[string]int {
	out := map[string]int{}
	for i := range m.Insts {
		if m.Insts[i].Op == OpConst && m.Insts[i].Str != "" {
			out[m.Insts[i].Str]++
		}
	}
	return out
}

func callSyms(m *Module) map[string]int {
	out := map[string]int{}
	for i := range m.Insts {
		if m.Insts[i].Op == OpCall && m.Insts[i].Sym != "" {
			out[m.Insts[i].Sym]++
		}
	}
	return out
}

// TestStrConcatLitFoldInFormat verifies that a format string written as a
// CONCATENATION of string literals is folded at compile time and then
// substituted, instead of being reported as
// "interp: string interpolation not lowered" (a hard compile error):
//
//	format('a=' - '{x}')      ==  format('a={x}')
//	print('a=' - '{x}' - '!') ==  print('a={x}!')
//
// Nolang concatenates strings with `-` (and `+`), so a format string built from
// literal pieces is a compile-time constant. Before the fold, the interception
// only recognised a single literal argument, the `{x}` fragment fell through to
// KStrLit lowering inside a print-family argument list, and the whole program
// was refused.
func TestStrConcatLitFoldInFormat(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // the literal segment that must be materialized
	}{
		{"format-two-parts", `s = format('a=' - '{x}')`, "a="},
		{"format-chain", `s = format('a=' - '{x}' - '!')`, "a="},
		{"format-plus", `s = format('a=' + '{x}')`, "a="},
		{"format-group", `s = format('[' - ('a=' - '{x}') - ']')`, "[a="},
		{"print-two-parts", `print('a=' - '{x}')`, "a="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "main = () () {\n  x = 42\n  " + c.body + "\n}\n"
			mod, diags := lowerSrcWithDiags(t, src)
			if got := interpDiags(diags); len(got) != 0 {
				t.Fatalf("unexpected interp diagnostics: %v", got)
			}
			// The `{x}` fragment must NOT survive as a literal — it was folded
			// into the format string and then substituted.
			if strConsts(mod)["{x}"] != 0 {
				t.Errorf("format fragment %q was emitted verbatim; consts=%v", "{x}", strConsts(mod))
			}
			if callSyms(mod)["fmt-int"] == 0 {
				t.Errorf("no fmt-int call: the field was not substituted; syms=%v", callSyms(mod))
			}
			if strConsts(mod)[c.want] == 0 {
				t.Errorf("literal segment %q missing; consts=%v", c.want, strConsts(mod))
			}
		})
	}
}

// TestStrConcatLitFold verifies the general fold: a concatenation whose every
// part is a string literal lowers to ONE string constant, with no runtime
// concatenation left behind.
func TestStrConcatLitFold(t *testing.T) {
	src := "main = () () {\n  s = 'ab' - 'cd'\n  t = 'xy' + 'z'\n  u = ('p' - 'q')\n}\n"
	mod, diags := lowerSrcWithDiags(t, src)
	if got := interpDiags(diags); len(got) != 0 {
		t.Fatalf("unexpected interp diagnostics: %v", got)
	}
	consts := strConsts(mod)
	for _, want := range []string{"abcd", "xyz", "pq"} {
		if consts[want] == 0 {
			t.Errorf("folded constant %q missing; consts=%v", want, consts)
		}
	}
	// The individual pieces must be GONE — they were folded away, not emitted
	// and then concatenated at runtime.
	for _, gone := range []string{"ab", "cd", "xy", "z", "p", "q"} {
		if consts[gone] != 0 {
			t.Errorf("operand literal %q still materialized; consts=%v", gone, consts)
		}
	}
}

// TestStrConcatFoldDoesNotInventInterpolation is the reverse guard for the
// "don't fold a brace-shaped join" rule: `'{' - 'x}'` joins into `{x}`, but
// both parts are ordinary text, so the join must be printed verbatim instead of
// being substituted. Folding first and interpreting afterwards would silently
// change what such a program prints (it is a common way to build JSON-ish text).
func TestStrConcatFoldDoesNotInventInterpolation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // the verbatim text that must be materialized
	}{
		{"print", `print('{' - 'x}')`, "{x}"},
		{"format", `s = format('{' - '"k":1}')`, `{"k":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "main = () () {\n  x = 42\n  " + c.body + "\n}\n"
			mod, diags := lowerSrcWithDiags(t, src)
			if got := interpDiags(diags); len(got) != 0 {
				t.Fatalf("unexpected interp diagnostics: %v", got)
			}
			if callSyms(mod)["fmt-int"] != 0 {
				t.Errorf("a join of plain text fragments must not be interpolated; syms=%v", callSyms(mod))
			}
			if strConsts(mod)[c.want] == 0 {
				t.Errorf("verbatim text %q missing; consts=%v", c.want, strConsts(mod))
			}
		})
	}
}

// TestStrConcatLitFoldNotForRuntimeValues is the reverse guard: a concatenation
// that mixes a literal with a RUNTIME value is not a constant and must keep its
// old behaviour — including the "interpolation not lowered" diagnostic when the
// literal part carries an unsubstituted {field}, because emitting the braces
// verbatim would be silently wrong.
func TestStrConcatLitFoldNotForRuntimeValues(t *testing.T) {
	// 1. no field: plain runtime concat, no diagnostic.
	src := "main = () () {\n  n str = 'bob'\n  s = 'a=' - n\n  print(s)\n}\n"
	mod, diags := lowerSrcWithDiags(t, src)
	if got := interpDiags(diags); len(got) != 0 {
		t.Fatalf("unexpected interp diagnostics: %v", got)
	}
	if callSyms(mod)["fmt-int"] != 0 {
		t.Errorf("a literal-only concat must not be substituted as a format string")
	}

	// 2. with a field: substitution is impossible, so it must stay an error.
	src = "main = () () {\n  x = 42\n  n str = 'bob'\n  print('a=' - '{x}' - n)\n}\n"
	_, diags = lowerSrcWithDiags(t, src)
	if got := interpDiags(diags); len(got) == 0 {
		t.Fatalf("expected the 'interpolation not lowered' diagnostic for a mixed concat, got none")
	}
}
