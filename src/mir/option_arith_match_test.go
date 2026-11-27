package mir

import (
	"strings"
	"testing"
)

const optionArithMatchSrc = `calc = (a i64, b i64) (out i64) {
    delta = a + b
    out = delta: {
        ok -> it
        -> 0
    }
}

holder {
    pos i64
}

holder.advance = (n i64) {
    delta = self.pos + n
    self.pos = delta: {
        ok -> it
        ->
    }
}

result = calc(2, 3)
state = holder {
    pos: 1
}
state.advance(4)
`

func TestOptionArithmeticTemporaryValueMatchKeepsSuccessTag(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, optionArithMatchSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	for _, sym := range []string{"@calc", "@holder_advance"} {
		t.Run(sym, func(t *testing.T) {
			fn := extractDefineBlock(t, ll, sym)
			if strings.Contains(fn, "store i1 false") || strings.Contains(fn, "br i1 false") {
				t.Fatalf("%s: option match condition was folded to false before testing the arithmetic result\n---\n%s", sym, fn)
			}
			if !strings.Contains(fn, "extractvalue %option") {
				t.Errorf("%s: option match does not read an option discriminant\n---\n%s", sym, fn)
			}
			if !strings.Contains(fn, "icmp eq") {
				t.Errorf("%s: option match has no discriminant comparison\n---\n%s", sym, fn)
			}
		})
	}
}
