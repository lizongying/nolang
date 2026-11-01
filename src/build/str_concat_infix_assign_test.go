package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStrAssignFromStringConcatInfix pins the front-end guard that decides
// whether `x str = <infix>` is legal.
//
// `-` and `+` concatenate and `*` repeats, so a str on one side makes the
// result a str — the other operand is rendered as a string. The guard used to
// require BOTH sides to be strings, which rejected valid code:
//
//	s str = 'x' - b     ; "xtrue"
//	s str = 'a' + 'b'   ; "ab"
//
// Widening it must NOT swallow the byte-arithmetic forms, because this pass is
// the type authority for infix results (the checker does not re-derive them):
// `'a' + 1` is 98 and `c - '0'` is 5, so both must stay rejected.
func TestStrAssignFromStringConcatInfix(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		// A single-character '...' is still a string literal; bool/float have
		// no byte meaning, so these are concatenation.
		{"bool-sibling", "b = true\ns str = 'x' - b\nprint(s)", false},
		{"bool-sibling-left", "b = true\ns str = b - 'x'\nprint(s)", false},
		{"float-sibling", "f = 1.5\ns str = 'x' - f\nprint(s)", false},
		{"both-literals-minus", "s str = 'a' - 'b'\nprint(s)", false},
		{"both-literals-plus", "s str = 'a' + 'b'\nprint(s)", false},
		{"repeat", "s str = 'x' * 5\nprint(s)", false},

		// Byte arithmetic: the digit-to-value idiom and its relatives.
		{"int-sibling-rejected", "s str = 'a' + 1\nprint(s)", true},
		{"char-var-sibling-rejected", "c char = \"5\"\ns str = c - '0'\nprint(s)", true},
		{"char-var-right-rejected", "c char = \"5\"\ns str = 'x' - c\nprint(s)", true},
		{"int-repeat-count-rejected", "n = 3\ns str = n * 2\nprint(s)", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lexer.New(tc.src)
			p := parser.New(l)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}

			// Build varTypes exactly the way the transpiler does, otherwise a
			// bool sibling is an untyped name and the guard cannot classify it.
			varTypes, _, _, _, err := collectEntryProgramFacts(prog, true)
			if err != nil {
				t.Fatalf("collectEntryProgramFacts: %v", err)
			}

			sizeMap := buildSizeMaps(prog)
			validateFuncStrReturns = sizeMap.funcStrReturns
			err = validateArrayBounds(prog, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes)

			if tc.wantErr && err == nil {
				t.Fatalf("expected a validation error, got none")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no validation error, got: %v", err)
			}
		})
	}
}
