package mir

import "testing"

// Imported plain enums keep their variant metadata under the bare type name,
// while references in the merged program may carry a module prefix. Lowering
// must resolve value.affinity.text-affinity against the affinity table entry.
func TestQualifiedPlainEnumValueUsesBareMetadataKey(t *testing.T) {
	pkg := parseHIR(t, `read = () (out i64) {
    out = value.affinity.text-affinity
}

main = () {
    print(read())
}
`)
	mod, _, diags := LowerHIR(pkg, map[string][]string{
		"affinity": {"integer-affinity", "text-affinity"},
	})
	if mod == nil {
		t.Fatalf("LowerHIR returned nil: %v", diags)
	}
	if _, err := mod.EmitLLVM(); err != nil {
		t.Fatalf("qualified plain-enum value failed codegen: %v\n%s", err, mod.String())
	}

	fid, ok := mod.FuncByName["read"]
	if !ok {
		t.Fatal("read not found")
	}
	fn := mod.Func(fid)
	if fn == nil {
		t.Fatal("read has no MIR function")
	}
	for _, bid := range fn.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpConst {
				continue
			}
			typ := mod.Type(inst.Type)
			if typ != nil && typ.Raw == "i64" && inst.Int == 1 {
				return
			}
		}
	}
	t.Fatalf("text-affinity did not lower to discriminant 1\n%s", mod.String())
}

// Inside an imported module the receiver remains bare, while module merging
// qualifies the enum metadata key. The same lookup must work in reverse.
func TestBarePlainEnumValueUsesQualifiedMetadataKey(t *testing.T) {
	pkg := parseHIR(t, `read = () (out i64) {
    out = affinity.text-affinity
}

main = () {
    print(read())
}
`)
	mod, _, diags := LowerHIR(pkg, map[string][]string{
		"value.affinity": {"integer-affinity", "text-affinity"},
	})
	if mod == nil {
		t.Fatalf("LowerHIR returned nil: %v", diags)
	}
	if _, err := mod.EmitLLVM(); err != nil {
		t.Fatalf("bare plain-enum value failed codegen: %v\n%s", err, mod.String())
	}

	fid, ok := mod.FuncByName["read"]
	if !ok {
		t.Fatal("read not found")
	}
	fn := mod.Func(fid)
	if fn == nil {
		t.Fatal("read has no MIR function")
	}
	for _, bid := range fn.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpConst {
				continue
			}
			typ := mod.Type(inst.Type)
			if typ != nil && typ.Raw == "i64" && inst.Int == 1 {
				return
			}
		}
	}
	t.Fatalf("text-affinity did not resolve qualified metadata\n%s", mod.String())
}
