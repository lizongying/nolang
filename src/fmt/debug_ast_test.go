package fmt

import (
	"testing"
	"fmt"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

func dumpAST(t *testing.T, name, input string) {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Logf("[%s] parse errors:", name)
		for _, e := range errs {
			t.Logf("  %s", e)
		}
		return
	}
	t.Logf("[%s] AST:\n%s", name, dump.Dump(program))
}

func TestDebugAstCondColon(t *testing.T) {
	// bug 1: condition: { body } 被拆散
	dumpAST(t, "cond_colon", `n = 10
n > 0: {
    print("yes")
}`)
}

func TestDebugAstBang(t *testing.T) {
	// bug 2: ! { body } 被拆散
	dumpAST(t, "bang", `text = ''
line = read-line()
    ! {
        line = read-line()
        line.len == 0 -> break
    }`)
}

func TestDebugAstArrowChain(t *testing.T) {
	// bug 3: -> cond -> action 被拆散
	dumpAST(t, "arrow_chain", `{
    x > 0 -> y = 1
    -> z > 0 -> y = 2
}`)
}

func TestDebugAstCondElse(t *testing.T) {
	// bug 4: condition: { } else: { } 被拆散
	dumpAST(t, "cond_else", `n = 10
n > 0: {
    print("yes")
} else: {
    print("no")
}`)
}

// suppress unused import
var _ = fmt.Sprintf
