package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// preserveInnerCollectLets walks just enough of the AST shape used by the
// regression cases below. It is deliberately local: the point is to prove that
// the statements inside a nested block survive parsing, not to be a generic
// visitor.
func preserveInnerCollectLets(n Node, out map[string]bool) {
	switch x := n.(type) {
	case *Program:
		for _, s := range x.Statements {
			preserveInnerCollectLets(s, out)
		}
	case *FunctionDefinition:
		preserveInnerCollectLets(x.Body, out)
	case *BlockStatement:
		for _, s := range x.Statements {
			preserveInnerCollectLets(s, out)
		}
	case *LetStatement:
		if x.Name != nil {
			out[x.Name.Value] = true
		}
	case *ExpressionStatement:
		if x.Expression != nil {
			preserveInnerCollectLets(x.Expression, out)
		}
	case *IfExpression:
		preserveInnerCollectLets(x.Consequence, out)
		if x.Alternative != nil {
			preserveInnerCollectLets(x.Alternative, out)
		}
	case *SurfaceMatch:
		for i := range x.Arms {
			preserveInnerCollectLets(x.Arms[i].Body, out)
		}
	}
}

func preserveInnerParse(t *testing.T, src string) (*Program, *Parser) {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	for _, d := range p.diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected parse error: %s", d.Message)
		}
	}
	return prog, p
}

// TestPreserveInnerBlockMixedContentFallback locks the direct-double-brace
// heuristic: an arm body of the shape `cond -> { { <standalone if> ; <let> ;
// <if-group> } }` is NOT a bare match. parseBareMatchExpr bails out halfway
// through it; without a full state rollback the remaining tokens are parsed as
// the *next* statement, silently dropping the standalone if and the let
// (real victim: notools/src/awk.no awk-find-eq lost `qm1 = q - 1`, so the
// checker reported `'qm1' is not defined` and `no fmt` appeared to delete
// code because it faithfully printed the broken AST).
func TestPreserveInnerBlockMixedContentFallback(t *testing.T) {
	const src = `f = (s str) (pos i64) {
    pos = -1
    c = s[0]
    {
        c == '=' -> {
            {
                s.len() > 0 -> pos = 1

                #{overflow=wrap}
                qm1 = 2 - 1
                {
                    pos == 1 -> {}
                }
            }
        }
    }
}
`
	prog, _ := preserveInnerParse(t, src)
	lets := map[string]bool{}
	preserveInnerCollectLets(prog, lets)
	for _, want := range []string{"pos", "c", "qm1"} {
		if !lets[want] {
			t.Errorf("statement declaring %q was dropped from the AST; surviving lets: %v", want, lets)
		}
	}
}

// TestPreserveInnerBlockBareMatchStillWrapped is the positive control: a real
// direct-double bare match must still produce the synthetic IsWrapper block,
// otherwise the formatter would add a brace level on every `no fmt` run.
func TestPreserveInnerBlockBareMatchStillWrapped(t *testing.T) {
	const src = `f = (s str) (pos i64) {
    pos = -1
    c = s[0]
    {
        c == '=' -> {
            {
                s.len() > 0 -> pos = 1
                -> pos = 2
            }
        }
    }
}
`
	prog, _ := preserveInnerParse(t, src)
	var wrappers int
	var walk func(Node)
	walk = func(n Node) {
		if n == nil {
			return
		}
		switch x := n.(type) {
		case *Program:
			for _, s := range x.Statements {
				walk(s)
			}
		case *FunctionDefinition:
			walk(x.Body)
		case *BlockStatement:
			if x.IsWrapper {
				wrappers++
			}
			for _, s := range x.Statements {
				walk(s)
			}
		case *ExpressionStatement:
			if x.Expression != nil {
				walk(x.Expression)
			}
		case *SurfaceMatch:
			for i := range x.Arms {
				walk(x.Arms[i].Body)
			}
		}
	}
	walk(prog)
	if wrappers == 0 {
		t.Error("direct-double bare match no longer produces an IsWrapper block")
	}
}
