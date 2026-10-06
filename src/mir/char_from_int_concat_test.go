package mir

import (
	"strings"
	"testing"
)

// TestCharVarFromIntUsesStrFromCp pins the MIR retyping of a `char`-declared
// binding whose initializer is a plain integer (i64/byte). Such a variable is a
// code point (equivalent to `x char = <literal>` or `c char = s[i]`), so a
// string concatenation that embeds it MUST UTF-8 encode it via @str_from_cp,
// not render the decimal text via @str_from_i64.
//
// Without the KindInt→KindChar retype in the KLet lowering, `c`'s MIR value kept
// raw type "i64", codegen.strFromScalar fell through to @str_from_i64, and
// `'ab-' - c` produced "ab-65" instead of "abA".
func TestCharVarFromIntUsesStrFromCp(t *testing.T) {
	cases := map[string]string{
		"i64-var":    "x = 65\n  c char = x",
		"int-lit":    "c char = 65",
		"byte-var":   "b byte = 65\n  c char = b",
		"masked-expr": "n = 65\n  c char = n & 255",
	}
	for name, decl := range cases {
		t.Run(name, func(t *testing.T) {
			src := "main = () () {\n  " + strings.ReplaceAll(decl, "\n", "\n  ") +
				"\n  print('ab-' - c - '-z')\n}\n"
			mod, _ := lowerSrcWithDiags(t, src)
			ll, err := mod.EmitLLVM()
			if err != nil {
				t.Fatalf("EmitLLVM failed: %v", err)
			}
			if !strings.Contains(ll, "call %str-long @str_from_cp") {
				t.Fatalf("char-from-int concat operand must UTF-8 encode via a @str_from_cp call, IR had none:\n%s", ll)
			}
		})
	}
}
