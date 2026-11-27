package mir

import (
	"regexp"
	"testing"
)

func TestLocalEnumConstructorMatchPreservesPayload(t *testing.T) {
	mod := lowerHIR(t, `sample {
    integer(v i64),
    real(v f64),
    text(v str),
}

run = () {
    value = integer(5)
    value: {
        integer(iv) -> print('integer', iv)

        real(rv) -> print('real', rv)

        -> print('fallback')
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
	constructors, fields, voidPrintArgs := 0, 0, 0
	for _, bid := range fn.Blocks {
		for _, iid := range mod.Block(bid).Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			switch inst.Op {
			case OpEnumNew:
				constructors++
			case OpEnumField:
				if ty := mod.Type(inst.Type); ty != nil && ty.Raw == "i64" {
					fields++
				}
			case OpCall:
				if inst.Sym != "print" {
					continue
				}
				for _, arg := range inst.Args {
					if ty := mod.Type(mod.Values[arg].Type); ty != nil && ty.Raw == "void" {
						voidPrintArgs++
					}
				}
			}
		}
	}
	if constructors == 0 || fields != 1 || voidPrintArgs != 0 {
		t.Fatalf("got %d enum constructors, %d i64 payload extracts, and %d void print arguments\n%s",
			constructors, fields, voidPrintArgs, mod.String())
	}

	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "run")
	t.Logf("MIR:\n%s\nLLVM:\n%s", mod.String(), body)
	if !regexp.MustCompile(`store i64 %[^,]+, i64\* %ep\.`).MatchString(body) {
		t.Fatalf("constructor did not store its i64 payload into the enum union\n%s", body)
	}
	if !regexp.MustCompile(`load i64, i64\* %ep\.`).MatchString(body) {
		t.Fatalf("match did not load the i64 payload from the enum union\n%s", body)
	}
}
