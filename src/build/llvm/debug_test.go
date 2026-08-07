package llvm

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestDebugStructArrayDeepCopyIR(t *testing.T) {
	src := `
session {
    id i64
    count i64
}

session-manager {
    sessions [8]session
    count i64
}

session-manager.get-session = (idx i64) (s session) {
    s = .sessions[idx]
}

main = () {
    sm session-manager
    sm.sessions[0].id = 100
    sm.sessions[0].count = 5
    s session
    sm.get-session(0, s)
    print(s.id)
    print(s.count)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	fmt.Printf("=== Generated IR ===\n%s\n=== End IR ===\n", ir)
}
