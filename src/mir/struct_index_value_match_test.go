package mir

import (
	"regexp"
	"strings"
	"testing"
)

// A struct-valued safe index whose receiver came from a method result must
// still be wrapped before a value-position match. The parser's static type
// recovery cannot see through that receiver as reliably as a plain parameter.
func TestStructIndexValueMatchWrapsSafeRead(t *testing.T) {
	mod := lowerForTest(t, `column {
    name str
}

table {
    columns []column
}

catalog {
    tables []table
}

empty-column = () (out column) {
}

empty-table = () (out table) {
}

catalog.table = (index i64) (out table) {
    out = .tables[index]: {
        ok -> it

        -> empty-table()
    }
}

read-name = (source catalog, index i64) (out str) {
    t = source.table(index)
    c = t.columns[index]: {
        ok -> it

        -> empty-column()
    }
    out = c.name
}

main = () {
    source catalog
    t table
    t.columns.push(column{ name: 'id' })
    source.tables.push(t)
    print(read-name(source, 0))
}
`)

	fid, ok := mod.FuncByName["read-name"]
	if !ok {
		t.Fatal("read-name not found in lowered module")
	}
	f := mod.Func(fid)
	defs := map[ValueID]*Inst{}
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if d := definedValue(inst); d > NoVal {
				defs[d] = inst
			}
		}
	}
	indexes, wrappedIndexes := 0, 0
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst.Op != OpIndex {
				continue
			}
			ty := mod.valueTypeOf(f, inst.Dst)
			if ty == nil || ty.Kind != KindStruct {
				continue
			}
			indexes++
			for _, def := range defs {
				if def.Op == OpOptionWrap && len(def.Args) > 0 && def.Args[0] == inst.Dst {
					wrappedIndexes++
				}
			}
		}
	}
	if indexes == 0 {
		t.Fatal("fixture did not emit a struct index read")
	}
	if wrappedIndexes != indexes {
		t.Fatalf("value-position match wrapped %d of %d struct index reads\n%s", wrappedIndexes, indexes, mod.String())
	}

	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ll, "read_name")
	loaded := regexp.MustCompile(`(?m)^\s*(%lx\.\d+) = load %column, %column\* `).FindStringSubmatch(body)
	if len(loaded) != 2 {
		t.Fatalf("indexed column was not loaded\n%s", body)
	}
	stored := regexp.MustCompile(`(?m)^\s*store %column ` + regexp.QuoteMeta(loaded[1]) + `, %column\* `)
	if !stored.MatchString(body) {
		t.Fatalf("the loaded column was discarded before the match\n%s", body)
	}
}

// A top-level range loop has its own lexical scope. Its local container types
// are not retained in the parser's module VarTypes after the loop closes, but
// HIR conversion still needs them to normalize a value-position safe-index
// match inside that loop.
func TestTopLevelLoopStructIndexValueMatchWrapsSafeRead(t *testing.T) {
	mod := lowerForTest(t, `column {
    name str
}

table {
    columns []column
}

catalog {
    tables []table
}

empty-column = () (out column) {
}

empty-table = () (out table) {
}

catalog.table = (index i64) (out table) {
    out = .tables[index]: {
        ok -> it

        -> empty-table()
    }
}

CATALOG catalog
CATALOG.tables.push(table { columns: [column { name: 'id' }] })
i <- [0..1): {
    t = CATALOG.table(0)
    c = t.columns[i]: {
        ok -> it

        -> empty-column()
    }
    print(c.name)
}
`)

	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("synthetic main not found in lowered module")
	}
	f := mod.Func(fid)
	defs := map[ValueID]*Inst{}
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if d := definedValue(inst); d > NoVal {
				defs[d] = inst
			}
		}
	}
	indexes, wrappedIndexes := 0, 0
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst.Op != OpIndex {
				continue
			}
			ty := mod.valueTypeOf(f, inst.Dst)
			if ty == nil || ty.Kind != KindStruct {
				continue
			}
			indexes++
			for _, def := range defs {
				if def.Op == OpOptionWrap && len(def.Args) > 0 && def.Args[0] == inst.Dst {
					wrappedIndexes++
				}
			}
		}
	}
	if indexes == 0 {
		t.Fatal("fixture did not emit a struct index read in synthetic main")
	}
	if wrappedIndexes != indexes {
		t.Fatalf("top-level value-position match wrapped %d of %d struct index reads\n%s", wrappedIndexes, indexes, mod.String())
	}
}

// Imported-call initializers can lack a parse-time type even though MIR knows
// the callee's first result. A multi-result helper models that exact HIR shape:
// returnTypeFromCallExpr intentionally declines multi-result calls, while MIR
// still types the value from the callee's first result. The later raw-index
// match must recover its safe option read and payload in MIR.
func TestUntypedCallStructIndexValueMatchWrapsSafeRead(t *testing.T) {
	mod := lowerForTest(t, `column {
    name str
}

table {
    columns []column
}

empty-column = () (out column) {
}

make-table = () (out table, extra i64) {
    out.columns.push(column { name: 'id' })
}

t = make-table()
c = t.columns[0]: {
    ok -> it

    -> empty-column()
}
print(c.name)
`)

	fid, ok := mod.FuncByName["main"]
	if !ok {
		t.Fatal("synthetic main not found in lowered module")
	}
	f := mod.Func(fid)
	defs := map[ValueID]*Inst{}
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if d := definedValue(inst); d > NoVal {
				defs[d] = inst
			}
		}
	}
	indexes, wrappedIndexes := 0, 0
	for _, bid := range f.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst.Op != OpIndex {
				continue
			}
			ty := mod.valueTypeOf(f, inst.Dst)
			if ty == nil || ty.Kind != KindStruct {
				continue
			}
			indexes++
			for _, def := range defs {
				if def.Op == OpOptionWrap && len(def.Args) > 0 && def.Args[0] == inst.Dst {
					wrappedIndexes++
				}
			}
		}
	}
	if indexes == 0 {
		t.Fatal("fixture did not emit a struct index read in synthetic main")
	}
	if wrappedIndexes != indexes {
		t.Fatalf("untyped-call value-position match wrapped %d of %d struct index reads\n%s", wrappedIndexes, indexes, mod.String())
	}

	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ll, "_nolang_main")
	if !strings.Contains(body, "%option__x25column") {
		t.Fatalf("safe struct index did not produce an option value\n%s", body)
	}
	if !strings.Contains(body, "@str_from_cstr") && !strings.Contains(body, "@str_clone") {
		t.Fatalf("indexed column payload was not preserved\n%s", body)
	}
}
