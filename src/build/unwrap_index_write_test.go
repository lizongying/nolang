package build

import (
	"strings"
	"testing"
)

// REGRESSION GUARD (2026-11-07): `a[i] ?= v` must compile to a REAL store at the
// computed index — not to `getelementptr …, i64 undef`.
//
// Two independent defects made the write a silent no-op while still compiling
// and printing 0 (probe x8: expected 99):
//
//	A) emitSubjectMatchPlain rewrote the read index IN PLACE on the subject
//	   *IndexExpression, which H0 re-reads on its own lowering pass. The second
//	   pass captured the temp as its own initialiser (`let __idx_L_C =
//	   __idx_L_C`), so the temp was never initialised and both the bounds-check
//	   read and the write-back index were undef. src/parser's
//	   TestUnwrapAssignIndexTargetKeepsOriginalIndexExpr guards the AST shape;
//	   this guards the emitted IR.
//	B) the compiler-synthesised nil/err propagation arms received an `it`
//	   binding (`let it t=nil` / `let it t=err`) BEFORE the ok arm's `let it
//	   t=i64`. Locals are keyed by name, so `it` resolved to the void slot and
//	   the ok arm's `it = v` went into a void local — the store then read undef.
//
// The block-match form is the important one here: its `?=` is expanded by H0
// (lowerBlockMatchToHIR), so the parser-side AST test cannot see it.
func TestUnwrapAssignIndexWriteEmitsRealStore(t *testing.T) {
	cases := []struct {
		name string
		fn   string
		src  string
	}{
		{
			name: "block_match",
			fn:   "unwrap_idx_block_probe",
			src: `unwrap-idx-block-probe = (i i64, v i64) (r i64) {
    a [4]i64 = [0, 0, 0, 0]
    {
        a[i] ?= v
    }: {
        err -> _
        -> _
    }
    r = a[i]: {
        ok -> it

        -> -1
    }
}
print(unwrap-idx-block-probe(2, 99))
`,
		},
		{
			name: "bare_unwrap_assign",
			fn:   "unwrap_idx_bare_probe",
			src: `unwrap-idx-bare-probe = (i i64, v i64) (r ?i64) {
    a [4]i64 = [0, 0, 0, 0]
    a[i] ?= v
    r = a[i]: {
        ok -> it

        -> -1
    }
}

probe ?i64 = unwrap-idx-bare-probe(2, 99)
probe: {
    ok -> print(it)

    -> print(-1)
}
`,
		},
		{
			name: "method_field",
			fn:   "unwrap_idx_field_set",
			src: `unwrap-idx-field {
    keys [4]i64
}

unwrap-idx-field.set = (i i64, v i64) (r ?i64) {
    .keys[i] ?= v
    r = 0
}

n = unwrap-idx-field {}
n.set(2, 99)
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, ok := defineBody(t, compileWith(t, c.src, "src/std/number.no"), c.fn)
			if !ok {
				t.Fatalf("%s not emitted in LLVM IR (a caller is required so MIR keeps it reachable)", c.fn)
			}
			if !strings.Contains(body, "store i64") {
				t.Fatalf("no i64 store in %s — the ?= write-back was dropped:\n%s", c.fn, body)
			}
			// The write-back index must be a computed value. `undef` here means the
			// __idx temp was declared but never initialised (defect A) or the ok
			// arm's `it = v` landed in a void local (defect B).
			if strings.Contains(body, "i64 undef") {
				t.Fatalf("%s indexes with `i64 undef` — the index temp / `it` binding is uninitialised, "+
					"so the ?= write silently targets index 0:\n%s", c.fn, body)
			}
			if !strings.Contains(body, "getelementptr [4 x i64]") {
				t.Fatalf("%s emits no [4 x i64] element pointer — the element store is missing:\n%s", c.fn, body)
			}
		})
	}
}
