//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func main() {
	var source string

	if len(os.Args) > 1 {
		// Read from file
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", os.Args[1], err)
			os.Exit(1)
		}
		source = string(data)
	} else {
		// Read from stdin
		data, err := os.ReadFile("/dev/stdin")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
		source = string(data)
	}

	lex := lexer.New(source)
	p := parser.New(lex)
	prog := p.ParseProgram()

	hasErrors := false

	for _, e := range p.Errors() {
		fmt.Fprintf(os.Stderr, "PARSE ERROR: %s\n", e)
		hasErrors = true
	}
	for _, w := range p.Warnings() {
		fmt.Fprintf(os.Stderr, "PARSE WARN: %s\n", w)
	}

	undefinedVars := build.ValidateUndefinedVars(prog, "")
	if len(undefinedVars) > 0 {
		hasErrors = true
		for _, u := range undefinedVars {
			fmt.Fprintf(os.Stderr, "Line %d, Col %d: %s\n", u.Line, u.Column, u.Message)
		}
	}

	namingWarnings := build.ValidateNaming(prog)
	for _, n := range namingWarnings {
		fmt.Fprintf(os.Stderr, "NAMING: Line %d, Col %d: %s\n", n.Line, n.Column, n.Message)
	}

	typeErrors := build.ValidateTypes(prog)
	for _, t := range typeErrors {
		hasErrors = true
		fmt.Fprintf(os.Stderr, "TYPE: Line %d, Col %d: %s\n", t.Line, t.Column, t.Message)
	}

	unusedVars := build.ValidateUnusedVars(prog)
	for _, u := range unusedVars {
		fmt.Fprintf(os.Stderr, "UNUSED: Line %d, Col %d: %s\n", u.Line, u.Column, u.Message)
	}

	if !hasErrors {
		fmt.Println("OK: no errors")
	} else {
		os.Exit(1)
	}
}
