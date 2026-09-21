package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// strOrderingMessages parses src and returns the messages ValidateStrOrdering
// produced (empty slice = accepted).
func strOrderingMessages(t *testing.T, src string) []string {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var msgs []string
	for _, r := range ValidateStrOrdering(prog) {
		msgs = append(msgs, r.Message)
	}
	return msgs
}

// TestStrOrderingAllowsCharComparable covers everything the rule must NOT
// reject: one-character string literals (implicitly a char), chars, numbers,
// string equality (which really is implemented, via @str_eq), the char
// iteration variable of `for ch <- s`, and a char read out of a string.
func TestStrOrderingAllowsCharComparable(t *testing.T) {
	src := `
main = () {
    print('e' >= 'a')
    print('e' > 'a')
    print('a' < 'e')
    print('e' >= "a")
    print("e" >= 'a')
    print("e" >= "a")
    print('a' == 'b')
    print('ab' == 'ab')
    print('ab' != 'cd')
    n i64 = 3
    print(n > 1 && n < 5)
    f f64 = 1.5
    print(f < 2.0)
    s str = 'hello'
    for ch <- s {
        print(ch >= 'a' && ch <= 'z')
    }
    print(s[0] < 'm')
    c char = "m"
    c: {
        ["a".."z") -> print('alpha')
        -> print('other')
    }
}
`
	if msgs := strOrderingMessages(t, src); len(msgs) > 0 {
		t.Fatalf("unexpected diagnostics: %v", msgs)
	}
}

// TestStrOrderingRejectsMultiChar covers the rejected forms: nolang has no
// lexicographic string comparison, so `a >= b` on str used to silently
// evaluate to false (codegen routed every string comparison through @str_eq).
func TestStrOrderingRejectsMultiChar(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "multi-character literal",
			src:  `main = () { print('ab' < 'c') }`,
		},
		{
			name: "empty literal",
			src:  `main = () { print('' < 'a') }`,
		},
		{
			name: "str variables",
			src: "main = () {\n\ta str = 'e'\n\tb str = 'a'\n\tprint(a >= b)\n}",
		},
		{
			name: "str variable vs char",
			src:  "main = () {\n\ta str = 'e'\n\tprint(a < \"b\")\n}",
		},
		{
			name: "unannotated let holding a string",
			src:  "main = () {\n\ta = 'e'\n\tprint(a < 'z')\n}",
		},
		{
			name: "str-returning method call",
			src:  "main = () {\n\ts str = ' x '\n\tprint(s.trim() < 'a')\n}",
		},
		{
			name: "str parameter",
			src:  "f = (v str) (r bool) {\n\tr = v > 'a'\n}\n",
		},
		{
			name: "inside if condition",
			src:  "main = () {\n\ta str = 'e'\n\t{ a <= 'a' -> print('x') }\n}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msgs := strOrderingMessages(t, tc.src); len(msgs) == 0 {
				t.Fatalf("expected a diagnostic, got none")
			}
		})
	}
}
