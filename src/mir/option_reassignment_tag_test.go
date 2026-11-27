package mir

import "testing"

// Reassigning an option-typed result parameter must use the existing slot's type
// as the assignment target. The RHS node's inferred payload type is not a target
// type: treating it as one peels `err(...)` to its boxed payload and then wraps
// that pointer again with the default ok tag.
func TestOptionConstructorReassignmentPreservesTag(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	const src = `err-probe = (s str) (out ?i64) {
    out = err('invalid number')
}

ok-probe = (n i64) (out ?i64) {
    out = ok(n)
}

main = () () {
    a = err-probe('3.5')
    b = ok-probe(7)
    print(a)
    print(b)
}
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}

	for _, tc := range []struct {
		name string
		tag  int64
	}{
		{name: "err-probe", tag: 2},
		{name: "ok-probe", tag: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fid, ok := mod.FuncByName[tc.name]
			if !ok {
				t.Fatalf("function %s was not lowered", tc.name)
			}
			fn := mod.Func(fid)
			if fn == nil {
				t.Fatalf("function %s is missing", tc.name)
			}

			wraps := 0
			for _, bid := range fn.Blocks {
				for _, iid := range mod.Block(bid).Insts {
					inst := mod.Inst(iid)
					if inst == nil {
						continue
					}
					if inst.Op == OpOptionWrap {
						wraps++
						if inst.Int != tc.tag {
							t.Errorf("constructor tag = %d, want %d", inst.Int, tc.tag)
						}
						continue
					}
					if inst.Op != OpMove || inst.Dst <= NoVal || len(inst.Args) == 0 {
						continue
					}
					src := mod.Value(inst.Args[0])
					dst := mod.Value(inst.Dst)
					if src == nil || dst == nil {
						continue
					}
					srcType := mod.Type(src.Type)
					dstType := mod.Type(dst.Type)
					if srcType != nil && srcType.Kind == KindOption &&
						dstType != nil && dstType.Kind != KindOption {
						t.Errorf("option constructor was peeled before rebind: %s", mod.DumpAnnotated())
					}
				}
			}
			if wraps != 1 {
				t.Errorf("constructor emitted %d option wraps, want exactly one:\n%s", wraps, mod.DumpAnnotated())
			}
		})
	}

}
