package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	root := "../src/std"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	counts := map[string]int{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		data, _ := os.ReadFile(path)
		l := lexer.New(string(data))
		p := parser.New(l)
		p.SkipUnwrapLowering = true
		p.SkipSafeIndexLowering = true
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil
		}
		results := checker.ValidateIntOverflow(prog)
		typeMap := map[int]string{}
		var walk func(stmts []parser.Statement)
		walk = func(stmts []parser.Statement) {
			for _, s := range stmts {
				if s == nil {
					continue
				}
				typeMap[s.Pos().Line] = fmt.Sprintf("%T", s)
				switch v := s.(type) {
				case *parser.FunctionDefinition:
					if v.Body != nil {
						walk(v.Body.Statements)
					}
				case *parser.ForStatement:
					if v.Body != nil {
						walk(v.Body.Statements)
					}
				case *parser.BlockStatement:
					walk(v.Statements)
				case *parser.ExpressionStatement:
					if ie, ok := v.Expression.(*parser.IfExpression); ok {
						if ie.Consequence != nil {
							walk(ie.Consequence.Statements)
						}
						if ie.Alternative != nil {
							walk(ie.Alternative.Statements)
						}
					}
				case *parser.LetStatement:
					if fl, ok := v.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
						walk(fl.Body.Statements)
					}
				}
			}
		}
		walk(prog.Statements)
		for _, r := range results {
			t := typeMap[r.Line]
			if t == "" {
				t = "unknown"
			}
			counts[t]++
		}
		return nil
	})
	fmt.Println("=== statement type of ovf-int-default reports ===")
	for t, c := range counts {
		fmt.Printf("%6d  %s\n", c, t)
	}
}
