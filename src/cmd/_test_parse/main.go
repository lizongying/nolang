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
		if v, ok := s.(*parser.FunctionDefinition); ok {
			if v.Name == "str.to-i64" {
				fmt.Printf("Function: %q IsVariadic=%v VariadicUnion=%q\n", v.Name, v.IsVariadic, v.VariadicUnion)
				fmt.Printf("  Parameters:\n")
				for _, p := range v.Parameters {
					fmt.Printf("    %q : %s\n", p.Name, p.Type.String())
				}
				fmt.Printf("  Results:\n")
				for _, r := range v.Results {
					fmt.Printf("    %q : %s\n", r.Name, r.Type.String())
				}
			}
		}
	}
	fmt.Printf("=== Total stmts: %d ===\n", len(prog.Statements))
	_ = nbuild.ValidateUnionTypes // touch
}
