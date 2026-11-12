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
//
//	join  -> path.join
//	base  -> path.base
//	dir   -> path.dir
//	ext   -> path.ext
//	is-abs -> path.is-abs
//	clean -> path.clean
//	split -> path.split
//
// 2026-11-12：split 这一条以前写成 `p.path-split(d, f)`（接收者是 str，还带两个
// 出参缓冲）—— `str.path-split` 早已被 `path.split() (f str)` 取代，这段调用在
// 今天的 std 里**根本没有对应成员**。它能一直"通过"，只是因为方法调用位置当时
// 没有任何诊断（ValidateUnknownFields 只管字段读）。callee 纳入判定后这条立刻
// 报 o4x247ya，属于真错，按现行 API 改写。
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
	p = path{}
	p.p = 'a/b/c.txt'
	f = p.split()
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
