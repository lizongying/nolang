package mir

import (
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func zzLower(t *testing.T, src string) *Module {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.LowerSurface(prog)
	pkg := parser.ASTToHIR(prog)
	mod, _, diags := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatalf("nil module; diags=%v", diags)
	}
	return mod
}

func zzDescribe(mod *Module, v ValueID) string {
	val := mod.Value(v)
	if val == nil {
		return "nil-value"
	}
	ty := mod.Type(val.Type)
	kind := "noType"
	name := ""
	if ty != nil {
		kind = fmt.Sprintf("%v", ty.Kind)
		name = ty.Raw
	}
	return fmt.Sprintf("v%d type=%s(%s)", v, kind, name)
}

func TestZZMirTypes(t *testing.T) {
	x8 := `f = (i i64, v i64) (r i64) {
    a [4]i64 = [0, 0, 0, 0]
    {
        a[i] ?= v
    }: {
        err -> _
        -> _
    }
    r = a[i]: {
        ok -> it

        -> -1
    }
}

print(f(2, 99))
`
	mod := zzLower(t, x8)
	fid := mod.FuncByName["f"]
	f := mod.Func(fid)
	for _, bid := range f.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			switch inst.Op {
			case OpMove, OpIndex, OpIndexStore:
				s := fmt.Sprintf("%s dst=%s", inst.Op, zzDescribe(mod, moveDst(inst)))
				for _, a := range inst.Args {
					s += fmt.Sprintf(" | arg %s", zzDescribe(mod, a))
				}
				fmt.Println(s)
			}
		}
	}
}
