package mir

import (
	"strings"
	"testing"
)

// A match subject is an ordinary lexical binding even when the current enum
// variant declares a payload field with the same name. Re-reading the subject
// after activating the variant pattern must not turn it into a field projection.
func TestEnumArmSubjectNamedLikePayloadField(t *testing.T) {
	mod := lowerHIR(t, `sample {
    integer(v i64),
    real(v f64),
    text(v str),
}

read-sample = (v sample) (out i64) {
    out = 0
    v: {
        integer(n) -> out = n

        real(r) -> out = r.to-i64()

        text(s) -> out = s.len-bytes()
    }
}

main = () {
    print(read-sample(integer(1)))
}
`)

	fid, ok := mod.FuncByName["read-sample"]
	if !ok {
		t.Fatal("read-sample not found")
	}
	fn := mod.Func(fid)
	if fn == nil || len(fn.Params) == 0 {
		t.Fatal("read-sample has no subject parameter")
	}
	subject := fn.Params[0]
	fields := 0
	for _, bid := range fn.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpEnumField {
				continue
			}
			fields++
			if len(inst.Args) != 1 || inst.Args[0] != subject {
				t.Fatalf("payload projection must read the whole subject parameter %d, got args=%v\n%s",
					subject, inst.Args, mod.String())
			}
		}
	}
	if fields == 0 {
		t.Fatalf("test did not exercise payload extraction\n%s", mod.String())
	}
}

// The same collision also affects a whole-value read in the arm body. A helper
// expecting the enum must receive the subject, not a projected string payload.
func TestEnumArmSubjectWholeValueRead(t *testing.T) {
	mod := lowerHIR(t, `sample {
    integer(v i64),
    text(v str),
}

inspect-sample = (v sample) (out i64) {
    out = 0
    v: {
        integer(n) -> out = n

        text(s) -> out = s.len-bytes()
    }
}

read-through-helper = (v sample) (out i64) {
    out = 0
    v: {
        integer(n) -> out = n

        text(s) -> out = inspect-sample(v)
    }
}

main = () {
    print(read-through-helper(text('x')))
}
`)

	fid, ok := mod.FuncByName["read-through-helper"]
	if !ok {
		t.Fatal("read-through-helper not found")
	}
	fn := mod.Func(fid)
	if fn == nil || len(fn.Params) == 0 {
		t.Fatal("read-through-helper has no subject parameter")
	}
	subject := fn.Params[0]
	calls := 0
	for _, bid := range fn.Blocks {
		blk := mod.Block(bid)
		if blk == nil {
			continue
		}
		for _, iid := range blk.Insts {
			inst := mod.Inst(iid)
			if inst == nil || inst.Op != OpCall || inst.Sym != "inspect-sample" {
				continue
			}
			calls++
			if len(inst.Args) == 0 || inst.Args[0] != subject {
				t.Fatalf("whole-value helper must receive subject parameter %d, got args=%v\n%s",
					subject, inst.Args, mod.String())
			}
		}
	}
	if calls == 0 {
		t.Fatalf("test did not exercise the helper call\n%s", mod.String())
	}
}

// Multi-field tagged-enum patterns are lowered to positional synthetic extracts
// (`let values = it.f0`, `let next = it.f1`). Both extracts must bind real payload
// values: losing the complex first field makes a later `values.len()` resolve as
// a receiver-less builtin call.
func TestEnumArmMultiFieldDestructuringBindsComplexPayload(t *testing.T) {
	mod := lowerHIR(t, `result {
    valid(values []i64, next i64),
    failed,
}

read-result = (r result) (out i64) {
    out = 0
    r: {
        valid(values, next) -> {
            out = values.len()
            out = out + next
        }

        failed -> out = -1
    }
}

main = () {
    print(read-result(valid([1, 2], 3)))
}
`)

	fid, ok := mod.FuncByName["read-result"]
	if !ok {
		t.Fatal("read-result not found")
	}
	fn := mod.Func(fid)
	if fn == nil {
		t.Fatal("read-result has no body")
	}
	var valuesField, nextField ValueID
	lenCalls := 0
	for _, bid := range fn.Blocks {
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
			case OpEnumField:
				if len(inst.Args) != 1 {
					t.Fatalf("enum field projection has args=%v, want one subject\n%s", inst.Args, mod.String())
				}
				raw := ""
				if ty := mod.Type(inst.Type); ty != nil {
					raw = ty.Raw
				}
				switch raw {
				case "[]i64":
					valuesField = inst.Dst
				case "i64":
					nextField = inst.Dst
				}
			case OpLen:
				lenCalls++
				if len(inst.Args) != 1 {
					t.Fatalf("len has args=%v, want the values payload\n%s", inst.Args, mod.String())
				}
			case OpCall:
				if inst.Sym == "len" || strings.HasSuffix(inst.Sym, ".len") {
					lenCalls++
					if len(inst.Args) != 1 {
						t.Fatalf("receiver-less len call %q args=%v\n%s", inst.Sym, inst.Args, mod.String())
					}
				}
			}
		}
	}
	if valuesField == NoVal || nextField == NoVal {
		t.Fatalf("multi-field pattern did not bind both payloads: values=%d next=%d\n%s", valuesField, nextField, mod.String())
	}
	if lenCalls == 0 {
		t.Fatalf("test did not exercise values.len()\n%s", mod.String())
	}
}
