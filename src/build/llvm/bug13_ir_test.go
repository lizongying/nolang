package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestBug13IR(t *testing.T) {
	src := `compute-str = (prefix str) (s str) {
    s = prefix - '-1234567890abcdef'
}

write-ref-sim = (ref-name str, hex-val str, val-i64 i64) (ok bool) {
    fd = fs.open-write('/tmp/bug13-test-ref.txt')
    fs.close(fd)
    content = hex-val - 'x'
    ok = true
}

main = () () {
    ch str = compute-str('commit')
    ok bool = write-ref-sim('refs', ch, 42)
    print(ok)
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

	lines := strings.Split(ir, "\n")
	inWriteRef := false
	for _, line := range lines {
		if strings.Contains(line, "define") && strings.Contains(line, "write-ref-sim") {
			inWriteRef = true
		}
		if inWriteRef {
			t.Logf("%s", line)
		}
		if inWriteRef && strings.TrimSpace(line) == "}" {
			inWriteRef = false
		}
	}
}
