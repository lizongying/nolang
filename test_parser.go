package main

import (
	"fmt"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	src := `test = () {
    i = 0
    i = 0
}`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()

	for _, stmt := range prog.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			for _, s := range fd.Body.Statements {
				if ls, ok := s.(*parser.LetStatement); ok {
					fmt.Printf("Name: %s, Type: %v, Type==nil: %v\n", ls.Name.Value, ls.Type, ls.Type == nil)
					if ls.Type != nil {
						fmt.Printf("  Type.String(): %s, Name.Value: %s, Equal: %v\n", ls.Type.String(), ls.Name.Value, ls.Type.String() == ls.Name.Value)
						if nt, ok := ls.Type.(*parser.NamedType); ok {
							fmt.Printf("  NamedType Token: line=%d col=%d\n", nt.Token.Line, nt.Token.Column)
							fmt.Printf("  Name Token: line=%d col=%d\n", ls.Name.Token.Line, ls.Name.Token.Column)
							fmt.Printf("  Same position: %v\n", nt.Token.Line == ls.Name.Token.Line && nt.Token.Column == ls.Name.Token.Column)
						}
					}
				}
			}
		}
	}
}
