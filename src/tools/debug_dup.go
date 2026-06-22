//go:build ignore

package main

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	input := `aes-128-dec = (in str, n i64, key str, out str) {
    ek = '(16+160 bytes)'
    aes-key-expand(key, ek)
    i = 0
    for i < 16 {
        out[i] = in[i]
        i = i + 1
    }
    // 初始 AddRoundKey
    add-round-key(out, ek + 160)

    // 第 9-1 輪
    round = 9
    for round > 0 {
        inv-shift-rows(out)
        inv-sub-bytes(out, 16)
        rk-off = round * 16
        add-round-key(out, ek + rk-off)
        inv-mix-columns(out)
        round = round - 1
    }

    // 第 0 輪
    inv-shift-rows(out)
    inv-sub-bytes(out, 16)
    add-round-key(out, ek)
}`

	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		for _, err := range p.Errors() {
			fmt.Println("Parse error:", err)
		}
	}

	dumpAST(program, 0)
}

func dumpAST(node interface{}, indent int) {
	prefix := strings.Repeat("  ", indent)
	switch n := node.(type) {
	case *parser.Program:
		fmt.Printf("%sProgram (%d statements)\n", prefix, len(n.Statements))
		for _, stmt := range n.Statements {
			dumpAST(stmt, indent+1)
		}
	case *parser.FunctionDefinition:
		fmt.Printf("%sFunctionDefinition{name: %s, line: %d}\n", prefix, n.Name, n.Token.Line)
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		if n.Comment != nil {
			for _, c := range n.Comment.List {
				fmt.Printf("%s  Comment: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		dumpAST(n.Body, indent+1)
	case *parser.BlockStatement:
		fmt.Printf("%sBlockStatement{line: %d, %d statements}\n", prefix, n.Token.Line, len(n.Statements))
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		if n.TrailingComments != nil {
			for _, c := range n.TrailingComments.List {
				fmt.Printf("%s  TrailingComments: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		for _, stmt := range n.Statements {
			dumpAST(stmt, indent+1)
		}
	case *parser.LetStatement:
		fmt.Printf("%sLetStatement{name: %s, line: %d}\n", prefix, n.Name.Value, n.Token.Line)
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
	case *parser.ExpressionStatement:
		if n.Expression == nil {
			fmt.Printf("%sExpressionStatement{nil, line: %d}\n", prefix, n.Token.Line)
		} else {
			fmt.Printf("%sExpressionStatement{line: %d}\n", prefix, n.Token.Line)
		}
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		if n.Comment != nil {
			for _, c := range n.Comment.List {
				fmt.Printf("%s  Comment: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
	case *parser.ForStatement:
		fmt.Printf("%sForStatement{line: %d}\n", prefix, n.Token.Line)
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		if n.Comment != nil {
			for _, c := range n.Comment.List {
				fmt.Printf("%s  Comment: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
		dumpAST(n.Body, indent+1)
	case *parser.ReturnStatement:
		fmt.Printf("%sReturnStatement{line: %d}\n", prefix, n.Token.Line)
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
	case *parser.BreakStatement:
		fmt.Printf("%sBreakStatement{line: %d}\n", prefix, n.Token.Line)
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
	case *parser.ContinueStatement:
		fmt.Printf("%sContinueStatement{line: %d}\n", prefix, n.Token.Line)
		if n.Doc != nil {
			for _, c := range n.Doc.List {
				fmt.Printf("%s  Doc: %s (line %d)\n", prefix, c.Text, c.Pos.Line)
			}
		}
	case *parser.UseStatement:
		fmt.Printf("%sUseStatement{path: %s, fn: %s, alias: %s, line: %d}\n", prefix, n.Path, n.Function, n.Alias, n.Token.Line)
	case *parser.ExportStatement:
		fmt.Printf("%sExportStatement{path: %s, fn: %s, alias: %s, line: %d}\n", prefix, n.Path, n.Function, n.Alias, n.Token.Line)
	case *parser.EnumDefinition:
		fmt.Printf("%sEnumDefinition{name: %s, line: %d}\n", prefix, n.Name, n.Token.Line)
		for _, v := range n.Values {
			fmt.Printf("%s  %s = %d\n", prefix, v.Name, v.Value)
		}
	case *parser.TaggedEnumDefinition:
		fmt.Printf("%sTaggedEnumDefinition{name: %s, line: %d}\n", prefix, n.Name, n.Token.Line)
		for _, v := range n.Variants {
			fmt.Printf("%s  %s: %s (index %d)\n", prefix, v.Name, v.Type.String(), v.Index)
		}
	case *parser.InterfaceDefinition:
		fmt.Printf("%sInterfaceDefinition{name: %s, line: %d}\n", prefix, n.Name, n.Token.Line)
		for _, m := range n.Methods {
			fmt.Printf("%s  Method %s (%d params)\n", prefix, m.Name, len(m.Parameters))
		}
	case *parser.StructDefinition:
		fmt.Printf("%sStructDefinition{name: %s, line: %d}\n", prefix, n.Name, n.Token.Line)
		for _, f := range n.Fields {
			fmt.Printf("%s  Field %s: %s\n", prefix, f.Name, f.Type.String())
		}
	default:
		fmt.Printf("%s%T\n", prefix, node)
	}
}
