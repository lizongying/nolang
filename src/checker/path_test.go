package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestPathMethods verifies path.no methods are parseable and pass type validation
// when called as methods on a str receiver.
//
// Refactored from standalone functions to method form in 2026-06:
//   join  -> path.join
//   base  -> path.base
//   dir   -> path.dir
//   ext   -> path.ext
//   is-abs -> path.is-abs
//   clean -> path.clean
//   split -> str.path-split
func TestPathMethods(t *testing.T) {
	src := `
test-base = () {
	p = path{}
	p.p = 'a/b/c.txt'
	base = p.base()
}

test-dir = () {
	p = path{}
	p.p = 'a/b/c.txt'
	p.dir()
}

test-ext = () {
	p = path{}
	p.p = 'a/b/c.txt'
	ext = p.ext()
}

test-is-abs = () {
	p = path{}
	p.p = '/etc/hosts'
	yes = p.is-abs()
}

test-clean = () {
	p = path{}
	p.p = 'a/./b/../c'
	p.clean()
}

test-join = () {
	p = path{}
	p.p = 'foo'
	p.join('bar')
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
