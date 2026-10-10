package dump

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// This file pins the *coverage boundary* of the L2 oracle spec.
//
// docs/design/nolang-selfhost.md §5.3 requires `noc ast` to reproduce `no ast`
// byte-for-byte. Most of the dump is mechanically reproducible, but two
// branches are not:
//
//	exprToString's `default: fmt.Sprintf("%v", e)`  -- prints a Go struct
//	    literal (lexer.Token included), which has no Nolang equivalent.
//	typeString's  `default: fmt.Sprintf("%T", n)`   -- reproducible, because
//	    the emitted kind table stores exactly the Go struct name.
//
// The first one is the dangerous one: if the corpus ever reaches it, the
// Nolang dump would silently diverge and the L2 gate would go red with a
// confusing diff. So rather than trusting a comment, this test *measures* the
// corpus and fails the moment the assumption stops holding.
//
// It also records how ArrayType.Size is shaped, because that is the only
// caller of exprToString in the whole dump.

// childrenOf returns the child nodes of n in dump order, using the generated
// spec tape so the walk matches dump.go exactly (including KIDS pointer
// elements such as MapLiteral.Pairs).
func childrenOf(n parser.Node) []parser.Node {
	ki, ok := specKindIndex[reflect.TypeOf(n).Elem().Name()]
	if !ok {
		return nil
	}
	k := specKinds[ki]
	var out []parser.Node
	for i := k.First; i < k.First+k.Num; i++ {
		t := specTape[i]
		switch t.Op {
		case opChild:
			out = append(out, nodeFromValue(fieldOf(n, specStr(t.A))))
		case opKids:
			out = append(out, nodesFromSlice(fieldOf(n, specStr(t.A)), t.C != 0)...)
		}
	}
	return out
}

func TestCorpusNeverHitsUnreproducibleExprString(t *testing.T) {
	progs := parseCorpus(t)

	hist := map[string]int{}
	var samples []string
	seen := map[parser.Node]bool{}

	var walk func(n parser.Node)
	walk = func(n parser.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		if at, ok := n.(*parser.ArrayType); ok {
			kind := "<nil>"
			if at.Size != nil {
				kind = fmt.Sprintf("%T", at.Size)
				if ie, ok := at.Size.(*parser.InfixExpression); ok {
					_, l := ie.Left.(*parser.IntegerLiteral)
					_, r := ie.Right.(*parser.IntegerLiteral)
					if !(l && r) {
						// evalConstIntExpr returns false, so exprToString
						// falls through to the recursive "+" join -- that
						// path IS reproducible.
						kind = "*InfixExpression{non-literal}"
					}
				}
			}
			hist[kind]++
			if len(samples) < 10 {
				samples = append(samples, at.String())
			}
		}
		for _, c := range childrenOf(n) {
			walk(c)
		}
	}
	for _, p := range progs {
		walk(p)
	}

	total := 0
	for k, v := range hist {
		t.Logf("ArrayType.Size: %-28s %d", k, v)
		total += v
		if k == "<nil>" || k == "*parser.IntegerLiteral" || k == "*parser.Identifier" ||
			k == "*parser.StringLiteral" || k == "*parser.InfixExpression" ||
			k == "*InfixExpression{non-literal}" {
			continue
		}
		t.Errorf("corpus has ArrayType.Size of type %s (e.g. %q); "+
			"exprToString's default branch prints a Go struct and CANNOT be "+
			"reproduced in Nolang -- ast.expr-string must be extended, or the "+
			"gap documented as reachable", k, samples[0])
	}
	if total == 0 {
		t.Fatal("no ArrayType in the corpus: this test is vacuous")
	}
	t.Logf("ArrayType.Size total: %d; all reproducible", total)
}
