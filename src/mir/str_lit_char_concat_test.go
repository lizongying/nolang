package mir

import "testing"

// hasStrInfix reports whether the module lowered some `+` / `-` / `*` to a
// STRING operation (concat / repeat) rather than integer arithmetic. That is
// the observable difference: a byte-folded operand makes the infix an i64
// add/sub, while a concatenation carries a `str` result type.
func hasStrInfix(m *Module) bool {
	for i := range m.Insts {
		switch m.Insts[i].Op {
		case OpAdd, OpSub, OpMul:
			if t := m.Type(m.Insts[i].Type); t != nil && t.Raw == "str" {
				return true
			}
		}
	}
	return false
}

func lowerBody(t *testing.T, body string) *Module {
	t.Helper()
	mod, _ := lowerSrcWithDiags(t, "main = () () {\n  "+body+"\n}\n")
	return mod
}

// TestStrLitCharConcatIsNotByteFolded pins the quoting rule for `+`/`-`:
//
//   - `'...'` is a StringLiteral, `"..."` is a CharLiteral.
//   - A ONE-character StringLiteral is byte-folded when its sibling is
//     neither a str nor a char LITERAL, so `'A' + 1` is 66 (tests/str-ops.no).
//   - A `"..."` char LITERAL sibling keeps the StringLiteral a STRING:
//     `'x' - "a"` is the concatenation "xa", not the integer 23 (120-97).
//
// Before the fix a char literal sibling was treated like any other non-str
// operand, so `'x' - "a"` silently produced 23 and `'x' + "a"` the integer 217.
func TestStrLitCharConcatIsNotByteFolded(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"strlit-minus-charlit", `s = 'x' - "a"`},
		{"strlit-plus-charlit", `s = 'x' + "a"`},
		{"charlit-plus-strlit", `s = "a" + 'x'`},
		{"charlit-minus-strlit", `s = "a" - 'x'`},
		{"multi-char-strlit", `s = 'xy' + "a"`},
		{"strlit-plus-int", `s = 'xy' + 1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !hasStrInfix(lowerBody(t, c.body)) {
				t.Errorf("`%s` must concatenate (str-result infix), but no str infix was lowered",
					c.body)
			}
		})
	}
}

// TestByteFoldingStillAppliesWithoutACharLitSibling is the reverse guard. The
// exception must be narrow: a char-typed VALUE that is not a `"..."` literal
// keeps the byte fold, which is what makes the digit-to-value idiom work.
// `c - '0'` concatenating into "50" instead of computing 5 would be a silent
// corruption, so that case is pinned explicitly.
func TestByteFoldingStillAppliesWithoutACharLitSibling(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"digit-to-value-charvar", "c char = \"5\"\n  n = c - '0'"},
		{"digit-to-value-index", "h = 'hello'\n  n = h[0] - 'a'"},
		{"strlit-minus-charvar", "c char = \"a\"\n  s = 'x' - c"},
		{"strlit-minus-index", "h = 'hello'\n  s = 'x' - h[0]"},
		{"strlit-plus-int", `s = 'A' + 1`},
		{"strlit-plus-int-narrow", `s = 'x' + 1`},
		{"char-minus-char", `s = "z" - "a"`},
		{"char-plus-char", `s = "a" + "b"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if hasStrInfix(lowerBody(t, c.body)) {
				t.Errorf("`%s` must stay integer arithmetic, but a str-result infix was lowered",
					c.body)
			}
		})
	}
}
