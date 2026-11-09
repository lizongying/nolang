package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestZZPC7HIR(t *testing.T) {
	cases := map[string]string{
		// C：区域变数承接算术 → 失败
		"C_var": `f = (data []byte) (r i64) {
    n = data.len()
    c = n / 16
    c: {
        ok -> print('C ok=' + it.to-str())

        nil -> print('C nil')

        err -> print('C err')
    }
    r = 0
}

d []byte = with-len(176)
f(d)
`,
		// D：显式 i64 → 正常
		"D_i64": `f = (data []byte) (r i64) {
    m i64 = data.len()
    d = m / 16
    d: {
        ok -> print('D ok=' + it.to-str())

        nil -> print('D nil')

        err -> print('D err')
    }
    r = 0
}

d []byte = with-len(176)
f(d)
`,
		// E：直接以算术式为主语 → 正常
		"E_infix": `f = (data []byte) (r i64) {
    n = data.len()
    n / 16: {
        ok -> print('E ok=' + it.to-str())

        nil -> print('E nil')

        err -> print('E err')
    }
    r = 0
}

d []byte = with-len(176)
f(d)
`,
	}
	for _, k := range []string{"C_var", "D_i64", "E_infix"} {
		t.Logf("===== %s =====\n%s", k, dumpHIR(t, cases[k]))
	}
}

func TestZZPC7Types(t *testing.T) {
	src := `f = (data []byte) (r i64) {
    n = data.len()
    c = n / 16
    y i64 = c
    r = y
}

d []byte = with-len(176)
f(d)
`
	p := New(lexer.New(src))
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.curFuncName = "f"
	t.Logf("astLocalVarType(f,n) = %q", p.astLocalVarType("f", "n"))
	t.Logf("astLocalVarType(f,c) = %q", p.astLocalVarType("f", "c"))
}
