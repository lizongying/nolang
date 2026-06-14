package parser

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestFunctionDefinitionExample(t *testing.T) {
	input := `
foo: (a int, b string) {
	x = 10
}

result = foo(1, 2)
`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Errorf("Expected no errors, got: %v", p.Errors())
		return
	}

	fmt.Printf("Program has %d statements\n", len(program.Statements))

	for i, stmt := range program.Statements {
		fmt.Printf("Statement %d: %T\n", i, stmt)

		if funcDef, ok := stmt.(*FunctionDefinition); ok {
			fmt.Printf("  Function: %s\n", funcDef.Name)
			fmt.Printf("  Parameters:\n")
			for _, param := range funcDef.Parameters {
				fmt.Printf("    - %s %s\n", param.Name, param.Type)
			}
		}
	}
}
