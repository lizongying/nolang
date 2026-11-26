package mir

import "testing"

// A constructor wrapper moves its owned parameter into the returned enum. The
// caller must not also drop that argument: the enum destructor owns the payload
// after the call. Keeping both drops frees the same string/slice twice.
func TestEnumConstructorParamMoveSuppressesCallerDrop(t *testing.T) {
	mod := lowerHIR(t, `sample {
    integer(v i64),
    text(v str),
}

make-text = (s str) (out sample) {
    out = text(s)
}

t = make-text('abc')
print('done')
`)

	mainID, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("main not found")
	}
	mainFn := mod.Func(mainID)
	if mainFn == nil {
		t.Fatal("main has no MIR function")
	}

	var callArg ValueID
	for _, bid := range mainFn.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpCall || inst.Sym != "make-text" || len(inst.Args) == 0 {
				continue
			}
			callArg = inst.Args[0]
		}
	}
	if callArg == NoVal {
		t.Fatalf("test did not exercise the constructor call\n%s", mod.String())
	}
	for _, bid := range mainFn.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst != nil && inst.Op == OpDrop && len(inst.Args) > 0 && inst.Args[0] == callArg {
				t.Fatalf("moved constructor argument %d still has a caller drop\n%s", callArg, mod.String())
			}
		}
	}

	// The leak verifier must recognize the same transfer as drop insertion.
	// Otherwise the valid move is rejected as a missing caller-side drop.
	rep := &Report{}
	mod.checkDropCount(mainFn, rep)
	if rep.HasErrors() {
		t.Fatalf("transferred constructor argument must not be reported as leaked: %s\n%s", rep.Error(), mod.String())
	}
}
