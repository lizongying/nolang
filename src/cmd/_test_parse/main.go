package main

import (
	"fmt"
	"os"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	data, _ := os.ReadFile(os.Args[1])
	src := string(data)
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	for _, e := range p.Errors() {
		fmt.Println("ERR:", e)
	}
	for _, s := range prog.Statements {
		fmt.Printf("  %T\n", s)
		switch v := s.(type) {
		case *parser.FunctionDefinition:
			fmt.Printf("Function: %q IsVariadic=%v VariadicUnion=%q\n", v.Name, v.IsVariadic, v.VariadicUnion)
		case *parser.TypeAlias:
			if v.IsUnion() {
				fmt.Printf("TypeAlias: %q = %s\n", v.Name, v.Union.String())
			} else {
				fmt.Printf("TypeAlias: %q = %s (single)\n", v.Name, v.Type.String())
			}
		}
	}
	fmt.Printf("=== Total stmts: %d ===\n", len(prog.Statements))
	// Test that ValidateUnionTypes + monomorphizeUnions work
	aliases, results := nbuild.ValidateUnionTypes(prog)
	fmt.Println("=== ALIASES ===")
	for name, ta := range aliases {
		if ta.IsUnion() {
			fmt.Printf("  %q = %s (union)\n", name, ta.Union.String())
		} else {
			fmt.Printf("  %q = %s (single)\n", name, ta.Type.String())
		}
	}
	fmt.Printf("=== ValidateResults: %d ===\n", len(results))
	for _, r := range results {
		fmt.Printf("  L%d: %s\n", r.Line, r.Message)
	}
}
