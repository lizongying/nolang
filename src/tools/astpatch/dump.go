// dump.go: 调试用 AST 打印，复用 parser/dump。
package main

import (
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

func dumpProgram(program *parser.Program) string {
	return dump.Dump(program)
}
