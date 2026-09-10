package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// Option matches must cover err, nil, and ok (or use a catch-all `->`),
// including built-in options like ?i64. err is a real variant of any ?T
// (e.g. std's read-stdin-str returns ?str and matches err ->).
func TestOptionMatchRequiresErr(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{"ok-nil-no-err", "f = () (v ?i64) {\n    v = 1\n    v: {\n        ok -> x = 1\n        nil -> x = 2\n    }\n}\n", true},
		{"ok-nil-err-complete", "f = () (v ?i64) {\n    v = 1\n    v: {\n        ok -> x = 1\n        nil -> x = 2\n        err -> x = 3\n    }\n}\n", false},
		{"catch-all-ok", "f = () (v ?i64) {\n    v = 1\n    v: {\n        ok -> x = 1\n        -> x = 2\n    }\n}\n", false},
		{"ok-nil-multi-pattern", "f = () (v ?i64) {\n    v = 1\n    v: {\n        ok -> x = 1\n        nil || err -> x = 2\n    }\n}\n", false},
	}
	for _, c := range cases {
		p := New(lexer.New(c.src))
		p.ParseProgram()
		errs := p.Errors()
		if c.wantErr && len(errs) == 0 {
			t.Errorf("%s: expected exhaustiveness error, got none", c.name)
		}
		if !c.wantErr && len(errs) > 0 {
			t.Errorf("%s: unexpected errors: %v", c.name, errs)
		}
	}
}
