package parser

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

func TestZZDumpHIR(t *testing.T) {
	src := "s []u32 = with-len(4)\ns[0]: {\n  ok -> it = 0x61626380\n  -> _\n}\nx i64 = 1633837952\nx == 1633837952 -> exit(42)\n-> exit(7)\n"
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = false
	p.SkipSafeIndexLowering = false
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("nil prog; errs=%v", p.Errors())
	}
	hirPkg, _ := ASTToHIRWithMap(prog)
	var walk func(id int32, indent string, depth int)
	walk = func(id int32, indent string, depth int) {
		if id <= hir.NoID || depth > 12 {
			return
		}
		n := hirPkg.Node(id)
		if n == nil {
			return
		}
		label := hirPkg.Str(n.S)
		fmt.Printf("%s%s %q kids=%d slot-first=%d\n", indent, n.Kind.String(), label, len(hirPkg.Children(id)), n.First)
		for _, c := range hirPkg.Children(id) {
			walk(c, indent+"  ", depth+1)
		}
	}
	fmt.Printf("=== TOP (%d) ===\n", len(hirPkg.Top))
	for _, id := range hirPkg.Top {
		walk(id, "", 0)
	}
}
