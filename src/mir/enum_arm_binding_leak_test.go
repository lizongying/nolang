package mir

import (
	"strings"
	"testing"
)

// Regression for tests/enum-annotation.no: an enum arm's named payload binding
// that creates a BRAND-NEW local (no prior binding of that name) must be removed
// at arm end. Otherwise it leaks into the enclosing scope and shadows a later
// binding of the same name.
//
// Here `rect(w f64, h f64)` binds `h` (f64) inside the match; later
// `h = holder{...}` reuses that f64 slot, so `h.inl` lowers as a field read on
// f64 -> "no struct layout for f64 (field inl)" -> a NoVal operand that codegen
// rejects as bug #85. The .no source is valid; the leak is the compiler bug.
func TestEnumArmPayloadBindingDoesNotLeak(t *testing.T) {
	const src = `shape {
    circle(r f64),
    rect(w f64, h f64),
    dot,
}
pt {
    x i64
    y i64
}
holder {
    inl pt #{inline=true}
    out pt #{inline=false}
    bare pt #{inline}
}
s shape = rect(2.0, 3.0)
s: {
    circle(r) -> print(r)

    rect(w, h) -> print(w * h)

    dot -> print('dot')
}
h = holder{
    inl: pt{
        x: 1
        y: 2
    }
    out: pt{
        x: 3
        y: 4
    }
    bare: pt{
        x: 5
        y: 6
    }
}
print(h.inl.x)
print(h.out.y)
print(h.bare.x)
`
	_, diags := lowerSrcWithDiags(t, src)
	for _, d := range diags {
		if strings.Contains(d.Msg, "no struct layout for f64") ||
			strings.Contains(d.Msg, "(field inl)") ||
			strings.Contains(d.Msg, "(field out)") ||
			strings.Contains(d.Msg, "(field bare)") {
			t.Fatalf("arm payload binding leaked into enclosing scope: %s/%s: %s",
				d.Func, d.Kind, d.Msg)
		}
	}
}
