package mir

import (
	"testing"

	"github.com/lizongying/nolang/hir"
)

// A bare callable alias must win over a same-named function registered by an
// unrelated std module. io.err returns i64, while the global err alias points to
// err.new and returns the structured error type.
func TestCallTypeQueryResolvesAliasBeforeBareCollision(t *testing.T) {
	hb := hir.NewBuilder("call-alias-type")
	addFunc := func(name, result string) int32 {
		res := hb.Add(hir.Node{Kind: hir.KResult, S: hb.Intern("out"), Type: hb.InternType(result)})
		fn := hb.Add(hir.Node{Kind: hir.KFuncDef, S: hb.Intern(name), First: res})
		hb.AddTop(fn)
		return fn
	}
	ioErr := addFunc("err", "i64")
	errNew := addFunc("err.new", "err")
	callee := hb.Add(hir.Node{Kind: hir.KIdent, S: hb.Intern("err")})
	fnSlot := hb.Add(hir.Node{Kind: hir.KSlot, S: hb.Intern("fn"), First: callee})
	call := hb.Add(hir.Node{Kind: hir.KCall, First: fnSlot})
	pkg := hb.Package()
	pkg.FuncOwners = map[string]string{"err": "io", "err.new": "err"}

	b := NewBuilder("call-alias-type")
	l := &lowerer{
		pkg:       pkg,
		mod:       b.Mod,
		b:         b,
		voidType:  b.Type("void"),
		curMod:    "main",
		funcNames: map[string]int32{"err": ioErr, "err.new": errNew},
		aliases:   map[string]aliasRef{"err": {Module: "err", Fn: "new"}},
	}

	if got, want := l.typeOfNode(pkg.Node(call)), b.Type("err"); got != want {
		t.Fatalf("aliased call type = %d (%s), want %d (%s)",
			got, b.Mod.Type(got).Raw, want, b.Mod.Type(want).Raw)
	}

	pkg.Node(call).Flags |= hir.FlagQualifiedCall
	if got, want := l.typeOfNode(pkg.Node(call)), b.Type("i64"); got != want {
		t.Fatalf("qualified io.err call type = %d (%s), want %d (%s)",
			got, b.Mod.Type(got).Raw, want, b.Mod.Type(want).Raw)
	}
}
