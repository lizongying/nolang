package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/lizongying/nolang/lexer"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	lo, hi := 0, 1<<30
	if len(os.Args) > 2 {
		lo, _ = strconv.Atoi(os.Args[2])
	}
	if len(os.Args) > 3 {
		hi, _ = strconv.Atoi(os.Args[3])
	}
	l := lexer.New(string(data))
	for i := 0; i < 4000000; i++ {
		t := l.NextToken()
		if t.Line >= lo && t.Line <= hi {
			fmt.Printf("line %d col %d %-16s %q\n", t.Line, t.Column, t.Type.String(), t.Literal)
		}
		if t.Type == lexer.EOF {
			break
		}
	}
}
