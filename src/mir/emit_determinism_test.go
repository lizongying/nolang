package mir

import (
	"strings"
	"testing"
)

// TestEmitLLVMIsDeterministic re-runs the FULL lower + analyze + emit pipeline
// for one source N times and asserts byte-identical LLVM IR every time.
//
// WHY THIS EXISTS. The MIR emit path ranged over several Go maps whose
// iteration order is randomized per run:
//
//   - codegen.emitStructTypes ranged over c.mod.StructFields and
//     c.mod.TaggedEnums, so the `%name = type { ... }` declaration block came
//     out in a different order on every compile of the same source;
//   - analysis.Module.insertDrops built each block's start/end drop lists by
//     ranging over def-use / live-in maps, so the `call void @str_free` /
//     `call void @__nolang_drop_*` runs were permuted per run.
//
// LLVM IR is order-insensitive at the *module* level, so both produced valid,
// semantically identical modules — but two compiles of identical source emitted
// byte-DIFFERENT text. That breaks reproducible builds and every golden-IR
// comparison, and it makes any flaky-test investigation untrustworthy (you can
// no longer tell a real regression from a re-shuffle of the same lines).
//
// This test pins the compiler to reproduce itself: run the pipeline N times and
// require the emitted text to be identical. It FAILS on the reverted code
// (the two map ranges un-sorted) and PASSES after the sorts are restored.
//
// The source deliberately exercises BOTH fixed sites:
//   - a user struct `pt` and a tagged enum `shape` populate StructFields /
//     TaggedEnums (the type-decl-order fix);
//   - two owned `str` bindings plus a branch hold multiple drops at one block's
//     END and a drop at a merge-block START (the drop-order fix).
func TestEmitLLVMIsDeterministic(t *testing.T) {
	src := `
pt {
    name str
    x i64
    y i64
}

shape {
    circle(r i64)
    rect(w i64 h i64)
}

main = () () {
    a str = 'alpha'
    b str = 'beta'
    p pt = pt { name: 'p', x: 1, y: 2 }
    print(p.name)
    s shape = circle(7)
    s: {
        circle -> print('c')
        rect -> print('r')
    }
    if a.len() > 3 {
        c str = a + b
        print(c)
    } else {
        print(a)
    }
    print(b)
}
`
	const N = 6
	var baseline string
	for i := 0; i < N; i++ {
		m, diags := lowerSrcWithDiags(t, src)
		if len(diags) > 0 {
			t.Fatalf("compile %d: unexpected lowering diags: %v", i, diags)
		}
		m.Analyze()
		out, err := m.EmitLLVM()
		if err != nil {
			t.Fatalf("compile %d: EmitLLVM error: %v", i, err)
		}
		if out == "" {
			t.Fatalf("compile %d: EmitLLVM produced empty output", i)
		}
		if i == 0 {
			baseline = out
			// Sanity: the source really drove both fixed sites, otherwise this
			// test could pass vacuously.
			if !strings.Contains(out, "%pt = type") {
				t.Fatalf("compile 0: struct decl not emitted (test would pass vacuously):\n%s", out)
			}
			if !strings.Contains(out, "%tenum_shape = type") {
				t.Fatalf("compile 0: tagged-enum decl not emitted (test would pass vacuously):\n%s", out)
			}
			if !strings.Contains(out, "@str_free") {
				t.Fatalf("compile 0: no str drops emitted (test would pass vacuously):\n%s", out)
			}
		} else if out != baseline {
			// Surface the first differing line pair so a failure is actionable.
			bLines := strings.Split(baseline, "\n")
			oLines := strings.Split(out, "\n")
			firstDiff := -1
			for j := 0; j < len(bLines) && j < len(oLines); j++ {
				if bLines[j] != oLines[j] {
					firstDiff = j
					break
				}
			}
			msg := "compile %d produced non-deterministic IR"
			if firstDiff >= 0 {
				msg += "\n  baseline line %d: %q\n  this     line %d: %q"
				t.Fatalf(msg, i, firstDiff+1, bLines[firstDiff], firstDiff+1, oLines[firstDiff])
			}
			t.Fatalf(msg, i)
		}
	}
}
