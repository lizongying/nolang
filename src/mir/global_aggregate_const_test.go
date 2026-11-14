package mir

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// A top-level binding initialized with an AGGREGATE literal must become a real
// module global with a folded constant initializer, so a helper function that
// names it reads the declared value.
//
// Two independent bugs met here:
//
//  1. foldConstText has no KStructLit / variant-constructor case, so the
//     initializer could not be folded. The registration path in LowerHIR treats
//     "cannot fold" as "not a module constant" and skips the binding, and the
//     lazy materialization in lowerGlobalRef then emitted
//     `private global <T> zeroinitializer` — every field/tag read 0 outside the
//     synthetic main (which had inlined the value as its own local).
//  2. For a tagged enum, "zeroinitializer" is not merely empty: tag 0 IS the
//     first declared variant, so `a e-num = lo` read back as `hi(0)` and a
//     match took the wrong arm.
//
// The end-to-end shape is tests/top-level-aggregate-init.no.
// ─────────────────────────────────────────────────────────────────────────────

const globalAggregateSrc = `point {
    n i64
}

pr point = point{n: 7}

e-num {
    hi(v i64),
    lo,
}

a e-num = lo
b e-num = hi(42)

read-pr = () (out i64) {
    out = pr.n
}

read-a = () (out i64) {
    out = 0
    a: {
        hi(v) -> out = v

        lo -> out = 99
    }
}

read-b = () (out i64) {
    out = 0
    b: {
        hi(v) -> out = v

        lo -> out = 42
    }
}

print(read-pr(), read-a(), read-b())
`

func TestTopLevelAggregateBindingsAreFoldedGlobals(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, globalAggregateSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// Inline the declarations we care about, so a failure prints the whole
	// prelude slice instead of only the missing line.
	var decls []string
	for _, line := range strings.Split(ll, "\n") {
		if strings.HasPrefix(line, "@pr =") || strings.HasPrefix(line, "@a =") || strings.HasPrefix(line, "@b =") {
			decls = append(decls, line)
		}
	}
	got := strings.Join(decls, "\n")
	// `hi` is declared first, so it is tag 0; `lo` is tag 1. Getting these
	// backwards is exactly the bug this pins.
	want := []string{
		"@pr = private global %point { i64 7 }",
		"@a = private global %tenum_e_num { i8 1, [1 x i64]",
		"@b = private global %tenum_e_num { i8 0, [1 x i64]",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing folded global initializer %q\ngot:\n%s", w, got)
		}
	}
	// The payload of `hi(42)` must be the constant 42, not a zero word.
	if !strings.Contains(got, "i8 0, [1 x i64] [i64 42]") {
		t.Errorf("`b e-num = hi(42)` did not fold its payload:\n%s", got)
	}
}
