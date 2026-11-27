package mir

import (
	"testing"

	"github.com/lizongying/nolang/hir"
)

// Once a method has been lowered, its MIR Results slice starts with the hidden
// self output parameter. A call used in a larger expression must use the first
// real result type, not the receiver type.
func TestMethodCallTypeQuerySkipsLoweredReceiverResult(t *testing.T) {
	hb := hir.NewBuilder("method-result")
	callee := hb.Add(hir.Node{Kind: hir.KIdent, S: hb.Intern("probe.check")})
	fnSlot := hb.Add(hir.Node{Kind: hir.KSlot, S: hb.Intern("fn"), First: callee})
	call := hb.Add(hir.Node{Kind: hir.KCall, First: fnSlot})
	pkg := hb.Package()

	b := NewBuilder("method-result")
	receiverType := b.Type("probe")
	resultType := b.Type("bool")
	self := b.Param("self", receiverType)
	fid := b.NewFunc("probe.check", []ValueID{self}, []TypeID{receiverType, resultType}, false)
	b.Mod.Funcs[fid].IsMethod = true
	b.Mod.Funcs[fid].Receiver = receiverType

	l := &lowerer{pkg: pkg, mod: b.Mod, b: b, voidType: b.Type("void")}
	if got := l.typeOfNode(pkg.Node(call)); got != resultType {
		t.Fatalf("call type = %d (%s), want first real method result %d (%s)",
			got, b.Mod.Type(got).Raw, resultType, b.Mod.Type(resultType).Raw)
	}
}
