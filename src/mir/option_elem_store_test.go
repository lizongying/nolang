package mir

import "testing"

// TestOptionValueElementStoreKeepsPayload pins the std/dom.no build failure that
// made `no test test/std` red:
//
//	dom.ll: '%lv.7954' defined with type '%option = type { i64, [3 x i64] }'
//	         but expected '%str-long = type { i64, i64, ptr }'
//
// The source shape is ordinary:
//
//	tgt = s.slice-bytes(p + 2, ne)     ; unannotated local, ARGUMENT has `p + 2`
//	.names[idx] = tgt                  ; `.names` is []str
//
// `p + 2` is a signed-integer arith node, so the parser's capture machinery
// treats the statement as capturable and synthesises the statement type
// `?<ret of slice-bytes>` = `?str` (parser/lowering.go synthesizeNullableType,
// reached through maybeCaptureOptionAssign's `case *CallExpression` — the
// capture sits in an ARGUMENT, yet the `?` lands on the target). Every other
// consumer of such a value already uses the payload — `tgt.len()` prints the
// right length — but the element store handed the whole `%option` struct to a
// `%str-long` slot, and LLVM rejected the module rather than mis-storing.
//
// So the invariant is: an OpIndexStore into a NON-option element slot must
// carry a non-option value. It is asserted on the MIR operand type rather than
// on the emitted IR because the raw store is only ill-typed inside LLVM — Go
// side EmitLLVM succeeds either way, which is why the failure surfaced as a
// `clang` diagnostic during link and not as a compiler error.
func TestOptionValueElementStoreKeepsPayload(t *testing.T) {
	src := `dup = (s str, p i64) (r str) {
    r = s
}

keep = (a []str, s str, i i64) {
    tgt = dup(s, i + 2)
    a[i] = tgt
}

buf []str = with-len(4)
keep(buf, 'abcd', 1)
print(buf.len())`
	mod := lowerForTest(t, src)
	stores := 0
	for i := range mod.Funcs {
		f := mod.Func(FuncID(i))
		if f == nil {
			continue
		}
		for _, bid := range f.Blocks {
			blk := mod.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := mod.Inst(iid)
				if inst == nil || inst.Op != OpIndexStore || len(inst.Args) < 3 {
					continue
				}
				stores++
				vt := mod.valueTypeOf(f, inst.Args[2])
				if vt != nil && vt.Kind == KindOption {
					t.Fatalf("OpIndexStore %d carries an option-typed value into an element slot "+
						"(type %q): the store must take the PAYLOAD — this is the std/dom.no "+
						"%%option-vs-%%str-long LLVM verification failure", iid, vt.Raw)
				}
			}
		}
	}
	if stores == 0 {
		t.Fatal("no OpIndexStore found — the fixture no longer reproduces the shape " +
			"(`.names[i] = tgt`), so this test is vacuous")
	}
}
