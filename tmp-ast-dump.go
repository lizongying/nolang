package main

import (
	"fmt"
	"os"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	src := `s = 'abcde'
x = s[..]
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, "parse errors:")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  ", e)
		}
		os.Exit(1)
	}
	for _, st := range prog.Statements {
		if ls, ok := st.(*parser.LetStatement); ok {
			fmt.Printf("Let: %s =\n", ls.Name.Value)
			switch v := ls.Value.(type) {
			case *parser.SliceExpression:
				fmt.Printf("  SliceExpression:\n")
				fmt.Printf("    Left: %T %v\n", v.Left, v.Left)
				if v.Range != nil {
					fmt.Printf("    Range: Start=%v End=%v LeftInc=%v RightInc=%v\n",
						v.Range.Start, v.Range.End, v.Range.LeftInc, v.Range.RightInc)
				} else {
					fmt.Printf("    Range: nil\n")
				}
			default:
				fmt.Printf("  %T: %v\n", v, v)
			}
		}
	}
}
