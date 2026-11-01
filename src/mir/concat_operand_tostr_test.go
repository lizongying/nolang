package mir

import (
	"testing"
)

// TestConcatStringifiesNonStringOperands pins the `str - X` / `str + X`
// auto-stringification of a non-string operand.
//
// A bool / f64 / struct operand used to be handed straight to @str_concat as a
// raw value, which LLVM rejects outright:
//
//	call %str-long @str_concat(%str-long %lv.27, i1 %lv.28)
//	call %str-long @str_concat(%str-long %lv.126, %point %lv.127)
//
// The lowering now renders such an operand through its `to-str` — the same
// choke point print uses — so `'b=' - flag` is "b=true" (bool goes through the
// `bool-to-str` builtin, matching `flag.to-str()`) and `'f=' - ratio` is
// "f=1.5". Integers and chars are deliberately NOT converted: codegen's
// strFromScalar already renders them (decimal / UTF-8 encoded).
//
// The assertion is MIR-level on purpose: the test harness lowers without the
// std modules, so the stringified callee (fmt-f64 / fmt-int) is enqueued but
// not yet materialized and EmitLLVM would report "unknown callee". What must
// hold regardless is the SHAPE — every str-typed concat takes two str operands
// and the non-string side is replaced by a to-str call.
func TestConcatStringifiesNonStringOperands(t *testing.T) {
	cases := map[string]struct {
		body string
		want string // the callee the operand must be stringified through
	}{
		"bool":          {body: "b = true\n  print('b=' - b)", want: "bool-to-str"},
		"bool-on-left":  {body: "b = false\n  print(b - ' <-bool')", want: "bool-to-str"},
		"f64":           {body: "f = 1.5\n  print('f=' - f)", want: "fmt-f64"},
		"struct":        {body: "pt = point {\n    x: 1\n    y: 2\n  }\n  print('p=' - pt)", want: "fmt-int"},
		"int-untouched": {body: "n = 42\n  print('n=' - n)", want: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := "point {\n  x i64\n  y i64\n}\n\nmain = () () {\n  " + tc.body + "\n}\n"
			mod, _ := lowerSrcWithDiags(t, src)

			tyOf := func(v ValueID) *Type {
				if v == NoVal {
					return nil
				}
				val := mod.Value(v)
				if val == nil {
					return nil
				}
				return mod.Type(val.Type)
			}
			rawOf := func(v ValueID) string {
				if ty := tyOf(v); ty != nil {
					return ty.Raw
				}
				return ""
			}
			// codegen renders these natively in a str concat: str itself,
			// integers (decimal), chars (UTF-8), and txt (widened).
			native := func(v ValueID) bool {
				ty := tyOf(v)
				if ty == nil {
					return true
				}
				switch ty.Kind {
				case KindStr, KindInt, KindChar:
					return true
				}
				return ty.Raw == "txt"
			}

			sawConcat := false
			sawCallee := tc.want == ""
			for i := range mod.Insts {
				inst := &mod.Insts[i]
				switch inst.Op {
				case OpAdd, OpSub:
					// Only the str-typed form is a concatenation; plain
					// arithmetic on ints/floats must be left alone.
					if rawOf(inst.Dst) != "str" || len(inst.Args) < 2 {
						continue
					}
					sawConcat = true
					for _, a := range inst.Args[:2] {
						if !native(a) {
							t.Fatalf("str concat received a %q operand (%s); it must be stringified first", rawOf(a), inst.Op)
						}
					}
				case OpCall:
					if inst.Sym == tc.want {
						sawCallee = true
					}
				}
			}
			if !sawConcat {
				t.Fatalf("expected a str-typed concat instruction, found none")
			}
			if !sawCallee {
				t.Fatalf("operand was not stringified through %q", tc.want)
			}
		})
	}
}
