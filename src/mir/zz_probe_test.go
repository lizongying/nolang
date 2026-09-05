package mir

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func probe(t *testing.T, name, src string) {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Logf("### %s: PARSE ERRORS: %v", name, errs)
		return
	}
	pkg := parser.ASTToHIR(prog)
	t.Logf("### %s HIR:\n%s", name, pkg.Dump())
	mod, rep, diags := LowerHIR(pkg)
	t.Logf("### %s MIR:\n%s", name, mod.String())
	t.Logf("### %s drops=%d", name, rep.DropsInserted)
	for _, d := range diags {
		t.Logf("### %s [lower-gap] %s/%s: %s", name, d.Func, d.Kind, d.Msg)
	}
}

// TestProbeMultiAssign inspects various HIR shapes during lowering development.
func TestProbeShapes(t *testing.T) {
	probe(t, "multi-assign", `f = () (a i64, b i64) {
  a = 1
  b = 2
  return
}
main = () () {
  x, y = f()
  print(x)
}
`)
	probe(t, "struct-lit", `p { name str, age i64 }
main = () () {
  q = p('bob', 7)
  print(q.age)
}
`)
	probe(t, "slice", `main = () () {
  a = [1, 2, 3, 4]
  b = a[1:3]
  print(b[0])
}
`)
	probe(t, "nil", `f = () (r ?i64) {
  return
}
main = () () {
  v ?i64 = nil
  print(1)
}
`)
	probe(t, "field-assign", `p { age i64 }
main = () () {
  q = p(1)
  q.age = 9
  print(q.age)
}
`)
	probe(t, "option-print", `o ?i64 = 5
print(o)
`)
	probe(t, "option-str", `o ?str = 'hi'
print(o)
`)
}
