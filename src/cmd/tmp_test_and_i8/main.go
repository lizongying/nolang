package main

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/build/llvm"
)

func main() {
	tests := []struct {
		name string
		src  string
	}{
		{
			"format_string_byte_and",
			"main = () {\n    h = 'abc'.to-bytes()\n    s = '{h[0] & 255:02x}'\n    print(s)\n}",
		},
		{
			"direct_byte_and",
			"main = () {\n    h = 'abc'.to-bytes()\n    v = h[0] & 255\n    print(v.to-str())\n}",
		},
		{
			"format_string_byte_direct",
			"main = () {\n    h = 'abc'.to-bytes()\n    s = '{h[0]:02x}'\n    print(s)\n}",
		},
		{
			"format_string_byte_and_multi",
			"main = () {\n    h = 'abcd'.to-bytes()\n    s = '{h[0] & 255:02x}{h[1] & 255:02x}'\n    print(s)\n}",
		},
		{
			"byte_array_and_255",
			"main = () {\n    h = [0, 1, 2, 3] []u8\n    v = h[0] & 255\n    print(v.to-str())\n}",
		},
	}

	for _, tt := range tests {
		fmt.Printf("=== %s ===\n", tt.name)
		l := lexer.New(tt.src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			fmt.Printf("parse errors: %v\n", p.Errors())
			continue
		}

		g := llvm.NewGenerator()
		ir := g.Generate(prog)

		if strings.Contains(ir, "and i8") {
			fmt.Printf("FOUND 'and i8' in IR!\n")
			for _, line := range strings.Split(ir, "\n") {
				if strings.Contains(line, "and i8") {
					fmt.Printf("  >> %s\n", line)
				}
			}
		} else {
			fmt.Printf("No 'and i8' found (OK)\n")
		}

		if strings.Contains(ir, "store i8 %vec.idx.zext") || strings.Contains(ir, "store i8 %idx.zext") {
			fmt.Printf("FOUND 'store i8' on idx.zext!\n")
			for _, line := range strings.Split(ir, "\n") {
				if strings.Contains(line, "store i8 %vec.idx.zext") || strings.Contains(line, "store i8 %idx.zext") {
					fmt.Printf("  >> %s\n", line)
				}
			}
		}

		fmt.Println()
	}
}
