package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestPathMethods verifies path.no methods are parseable and pass type validation
// when called as methods on a str receiver.
//
// Refactored from standalone functions to method form in 2026-06:
//   join  -> str.path-join
//   base  -> str.path-base
//   dir   -> str.path-dir
//   ext   -> str.path-ext
//   is-abs -> str.path-is-abs
//   clean -> str.path-clean
//   split -> str.path-split
func TestPathMethods(t *testing.T) {
	src := `
test-base = () {
	p = 'a/b/c.txt'
	buf = '(16)'
	n = p.path-base(buf)
}

test-dir = () {
	p = 'a/b/c.txt'
	buf = '(16)'
	n = p.path-dir(buf)
}

test-ext = () {
	p = 'a/b/c.txt'
	buf = '(16)'
	n = p.path-ext(buf)
}

test-is-abs = () {
	p = '/etc/hosts'
	yes = p.path-is-abs()
}

test-clean = () {
	p = 'a/./b/../c'
	buf = '(16)'
	n = p.path-clean(buf)
}

test-join = () {
	a = 'foo'
	b = 'bar'
	buf = '(16)'
	n = a.path-join(b, b.len, buf)
}

test-split = () {
	p = 'a/b/c.txt'
	d = '(16)'
	f = '(16)'
	dn = p.path-split(d, f)
	fn = p.path-split(d, f)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}
