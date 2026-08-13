package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestBug11CondBranchAssign generates IR for a minimal case where
// an output parameter is assigned inside a conditional branch.
func TestBug11CondBranchAssign(t *testing.T) {
	src := `find-byte = (s str, target i64) (pos i64) {
    pos = -1
    i <- [0..s.len): {
        s[i] == target -> {
            pos = i
            break
        }
    }
}

main = () () {
    s str = with-len(3)
    s[0] = 65
    s[1] = 10
    s[2] = 67
    nl-fn i64 = find-byte(s, 10)
    print(nl-fn)
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

	// Find and print the find-byte function body
	lines := strings.Split(ir, "\n")
	inFindByte := false
	for _, line := range lines {
		if strings.Contains(line, "define") && strings.Contains(line, "find-byte") {
			inFindByte = true
		}
		if inFindByte {
			t.Logf("%s", line)
		}
		if inFindByte && strings.TrimSpace(line) == "}" {
			inFindByte = false
		}
	}
}
