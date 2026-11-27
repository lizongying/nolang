package mir

import (
	"strings"
	"testing"
)

func TestNestedSafeIndexEnumArmSubjectBindsRealPayloads(t *testing.T) {
	mod := lowerHIR(t, `cell {
    sql-null,
    integer(v i64),
    text(v str)
}

row {
    values []cell
}

result {
    rows []row
}

make-result = () (out result) {
    source = row{}
    source.values.push(integer(2))
    source.values.push(text('BETA'))
    source.values.push(integer(12))
    out.rows.push(source)
}

run = () {
    returned = make-result()
    returned.rows[0]: {
        ok -> {
            it.values[1]: {
                text(value) -> print('name=' - value)

                -> print('name-missing')
            }
            it.values[2]: {
                integer(value) -> print('score=' - value)

                -> print('score-missing')
            }
        }

        -> print('row-missing')
    }
}

main = () {
    run()
}
`)

	fid, ok := mod.FuncByName["run"]
	if !ok {
		t.Fatal("run not found")
	}
	fn := mod.Func(fid)
	defs := map[ValueID]*Inst{}
	for _, bid := range fn.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			if inst := mod.Inst(iid); inst != nil {
				if dst := definedValue(inst); dst > NoVal {
					defs[dst] = inst
				}
			}
		}
	}

	indexes, wraps := 0, 0
	payloads := map[string]int{}
	for _, bid := range fn.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			switch inst.Op {
			case OpIndex:
				indexes++
				for _, def := range defs {
					if def.Op == OpOptionWrap && len(def.Args) > 0 && def.Args[0] == inst.Dst {
						wraps++
					}
				}
			case OpEnumField:
				if ty := mod.Type(inst.Type); ty != nil {
					payloads[ty.Raw]++
				}
			case OpEnumNew:
				for _, arg := range inst.Args {
					if def := defs[arg]; def != nil {
						if ty := mod.Type(def.Type); ty != nil && ty.Kind == KindVoid {
							t.Fatalf("nested enum pattern evaluated an unbound payload as void:\n%s", mod.String())
						}
					}
				}
			}
		}
	}
	if indexes != 3 || wraps != 3 {
		t.Fatalf("nested safe enum indexes: got %d reads and %d option wraps, want three each:\n%s", indexes, wraps, mod.String())
	}
	if payloads["str"] == 0 || payloads["i64"] == 0 {
		t.Fatalf("nested enum arms did not project typed str/i64 payloads: %v\n%s", payloads, mod.String())
	}
}

func TestSafeIndexEnumArmSubjectBindsRealPayloads(t *testing.T) {
	mod := lowerHIR(t, `cell {
    sql-null,
    integer(v i64),
    text(v str)
}

row {
    values []cell
}

read-cell = (source row, index i64) (out i64) {
    out = 0
    source.values[index]: {
        text(value) -> out = value.len-bytes()

        integer(value) -> out = value

        -> out = -1
    }
}

main = () {
    source = row{}
    source.values.push(integer(2))
    source.values.push(text('BETA'))
    print(read-cell(source, 1))
}
`)

	fid, ok := mod.FuncByName["read-cell"]
	if !ok {
		t.Fatal("read-cell not found")
	}
	fn := mod.Func(fid)
	defs := map[ValueID]*Inst{}
	for _, bid := range fn.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			if inst := mod.Inst(iid); inst != nil {
				if dst := definedValue(inst); dst > NoVal {
					defs[dst] = inst
				}
			}
		}
	}

	indexes, wraps := 0, 0
	payloads := map[string]int{}
	for _, bid := range fn.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			switch inst.Op {
			case OpIndex:
				indexes++
				for _, def := range defs {
					if def.Op == OpOptionWrap && len(def.Args) > 0 && def.Args[0] == inst.Dst {
						wraps++
					}
				}
			case OpEnumField:
				if ty := mod.Type(inst.Type); ty != nil {
					payloads[ty.Raw]++
				}
			case OpEnumNew:
				for _, arg := range inst.Args {
					if def := defs[arg]; def != nil {
						if ty := mod.Type(def.Type); ty != nil && ty.Kind == KindVoid {
							t.Fatalf("enum pattern evaluated an unbound payload as void:\n%s", mod.String())
						}
					}
				}
			}
		}
	}
	if indexes != 1 || wraps != 1 {
		t.Fatalf("safe enum index: got %d reads and %d option wraps, want one each:\n%s", indexes, wraps, mod.String())
	}
	if payloads["str"] == 0 || payloads["i64"] == 0 {
		t.Fatalf("enum arms did not project typed str/i64 payloads: %v\n%s", payloads, mod.String())
	}

	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "read_cell")
	if !strings.Contains(body, "@__nolang_enum_clone_tenum_cell") {
		t.Fatalf("safe enum read or option peel did not preserve owning payloads:\n%s", body)
	}
}
