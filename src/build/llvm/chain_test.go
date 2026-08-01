package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestChainedMethodCallNoUndefinedSymbol verifies that a chained method call
// like obj.field.to-str() does NOT generate an undefined function symbol
// such as @obj.field.to-str. Instead, it should resolve the method based
// on the field's type (e.g. i64.to-str) and generate a proper call.
//
// Regression test: before the fix, flattenDottedExpr would flatten the
// entire chain "u.age.to-str" into a single function name, generating
// `call void @u.age.to-str()` — an undefined symbol. The fix detects
// chained DotExpression receivers and uses just the method name (dot.Property),
// letting the method receiver resolution resolve the correct type-prefixed name.
func TestChainedMethodCallNoUndefinedSymbol(t *testing.T) {
	src := `
user {
    name str
    age i64
}

main = () {
    u = user {
        name: 'bob'
        age: 30
    }
    print(u.age.to-str())
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

	// The IR should NOT contain @u.age.to-str (undefined symbol from flattening)
	if strings.Contains(ir, "@u.age.to-str") {
		t.Errorf("IR should not contain undefined symbol @u.age.to-str, got:\n%s", ir)
	}

	// The IR should NOT contain any call with a flattened chain starting with "u."
	// as a function name (e.g. @u.age, @u.name, etc.)
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "call ") && strings.Contains(trimmed, "@u.age") {
			t.Errorf("IR should not contain call to @u.age... (flattened chain):\n%s", trimmed)
		}
	}
}
