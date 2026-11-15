package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestFindLetByTypePos 钉住 CLI 与 LSP 共用的下钻口径：顶层、函式字面值
// 内部、以及「找不到 / program 为 nil」三个边界。
func TestFindLetByTypePos(t *testing.T) {
	src := "f = () {\n    x str = ''\n    g = () {\n        y i64 = 1\n    }\n}\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	if FindLetByTypePos(nil, 1, 1) != nil {
		t.Fatal("nil program must return nil")
	}

	// 独立地（不靠被测 walker）取出带标注的两个 LetStatement。
	var outer, inner *LetStatement
	var collect func(stmts []Statement)
	collect = func(stmts []Statement) {
		for _, s := range stmts {
			if ls, ok := s.(*LetStatement); ok {
				if ls.Type == nil {
					continue
				}
				switch ls.Name.Value {
				case "x":
					outer = ls
				case "y":
					inner = ls
				}
				if fl, ok := ls.Value.(*FunctionLiteral); ok && fl.Body != nil {
					collect(fl.Body.Statements)
				}
			}
			if fd, ok := s.(*FunctionDefinition); ok && fd.Body != nil {
				collect(fd.Body.Statements)
			}
		}
	}
	collect(prog.Statements)
	if outer == nil || inner == nil {
		t.Fatal("fixture did not produce both annotated lets")
	}

	op := outer.Type.Pos()
	if got := FindLetByTypePos(prog, op.Line, op.Column); got != outer {
		t.Fatalf("top-level let not found: %#v", got)
	}
	ip := inner.Type.Pos()
	if got := FindLetByTypePos(prog, ip.Line, ip.Column); got != inner {
		t.Fatalf("let inside a function literal was not found (walk shape drifted): %#v", got)
	}
	if got := FindLetByTypePos(prog, 999, 999); got != nil {
		t.Fatalf("missing position must return nil, got %#v", got)
	}
}
