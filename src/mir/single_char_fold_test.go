package mir

import "testing"

// TestSingleCharLiteralByteFoldRequiresArithSibling pins the gate on the
// single-character `'...'` byte fold in the KInfix lowering.
//
// Nolang quoting makes `'x'` a STRING literal (`"x"` would be a char), so
// `'x' - b` is concatenation. The fold that rewrites `'x'` to the byte 120
// exists only to keep the digit-to-value idiom working — `c - '0'` is 5 and
// `'A' + 1` is 66 — and is therefore sound only when the sibling operand is
// integer/char-valued.
//
// It used to fire for ANY non-string sibling, so `'x' - flag` became
// 120 - 1 = 119: a plausible-looking number that silently replaced the
// intended "xtrue". A bool/float/struct sibling has no byte meaning at all.
func TestSingleCharLiteralByteFoldRequiresArithSibling(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantStr bool   // infix must lower to a str-typed concatenation
		wantCal string // callee the sibling must be stringified through
	}{
		{"bool-sibling", "b = true\n  print('x' - b)", true, "bool-to-str"},
		{"bool-sibling-left", "b = true\n  print(b - 'x')", true, "bool-to-str"},
		{"f64-sibling", "f = 1.5\n  print('x' - f)", true, "fmt-f64"},
		{"str-sibling", "print('x' - 'y')", true, ""},
		{"multi-char-already-str", "b = true\n  print('xy' - b)", true, "bool-to-str"},
		{"int-sibling-folds", "print('A' + 1)", false, ""},
		{"char-var-sibling-folds", "c char = \"5\"\n  print(c - '0')", false, ""},
		{"char-var-right-folds", "c char = \"5\"\n  print('x' - c)", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "main = () () {\n  " + tc.body + "\n}\n"
			mod, _ := lowerSrcWithDiags(t, src)

			rawOf := func(v ValueID) string {
				if v == NoVal {
					return ""
				}
				val := mod.Value(v)
				if val == nil {
					return ""
				}
				if ty := mod.Type(val.Type); ty != nil {
					return ty.Raw
				}
				return ""
			}

			var strInf, intInf int
			sawCallee := tc.wantCal == ""
			for i := range mod.Insts {
				inst := &mod.Insts[i]
				switch inst.Op {
				case OpAdd, OpSub:
					switch rawOf(inst.Dst) {
					case "str":
						strInf++
					case "i64":
						intInf++
					}
				case OpCall:
					if inst.Sym == tc.wantCal {
						sawCallee = true
					}
				}
			}

			// Both operands literal (`'x' - 'y'`), or a local propagated from a
			// literal (`c - '0'` with `c char = "5"`), is constant-folded before
			// this point, so there is no instruction to inspect. The rendered
			// VALUE of those forms is covered end-to-end by tests/str-ops.no
			// ("xy" / 66 / 5); here only the stringification callee still matters.
			if strInf+intInf == 0 {
				if !sawCallee {
					t.Fatalf("sibling was not stringified through %q", tc.wantCal)
				}
				return
			}

			if tc.wantStr {
				if strInf == 0 {
					t.Fatalf("expected a str-typed concat; got %d int and %d str infixes — "+
						"the single-char literal was byte-folded despite a non-arith sibling", intInf, strInf)
				}
				if intInf != 0 {
					t.Fatalf("expected no integer infix, got %d (byte arithmetic leaked into a concat)", intInf)
				}
			} else {
				if intInf == 0 {
					t.Fatalf("expected integer byte arithmetic; got %d str infixes — "+
						"the digit-to-value idiom must keep folding", strInf)
				}
				if strInf != 0 {
					t.Fatalf("expected no str concat, got %d", strInf)
				}
			}
			if !sawCallee {
				t.Fatalf("sibling was not stringified through %q", tc.wantCal)
			}
		})
	}
}
