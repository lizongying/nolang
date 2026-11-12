package mir

import (
	"strings"
	"testing"
)

func TestLayoutTagWidth(t *testing.T) {
	cases := []struct {
		n    int
		bits int64
		lt   string
	}{
		{1, 8, "i8"},
		{3, 8, "i8"},
		{256, 8, "i8"},
		{257, 16, "i16"},
		{65536, 16, "i16"},
		{65537, 32, "i32"},
	}
	for _, tc := range cases {
		bits, lt := tagWidth(tc.n)
		if bits != tc.bits || lt != tc.lt {
			t.Errorf("tagWidth(%d) = (%d,%q), want (%d,%q)", tc.n, bits, lt, tc.bits, tc.lt)
		}
	}
}

func TestLayoutOptTypeKeyIsInjective(t *testing.T) {
	cases := map[string]string{
		"i64":   "i64",
		"str":   "str",
		"[]i64": "_x5b_x5di64",
		"fs.file": "fs_x2efile",
		"a_b":   "a__b",
	}
	for raw, want := range cases {
		if got := optTypeKey(raw); got != want {
			t.Errorf("optTypeKey(%q) = %q, want %q", raw, got, want)
		}
	}
	// `[]i64` and `__i64` must not collide (sanitize would).
	if optTypeKey("[]i64") == optTypeKey("__i64") {
		t.Fatal("optTypeKey collides on []i64 vs __i64")
	}
}

func TestLayoutRoundUp(t *testing.T) {
	cases := [][3]int64{
		{0, 8, 0}, {1, 8, 8}, {8, 8, 8}, {9, 8, 16}, {3, 4, 4}, {5, 4, 8},
	}
	for _, c := range cases {
		if got := lasRoundUp(c[0], c[1]); got != c[2] {
			t.Errorf("lasRoundUp(%d,%d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

const layoutSrc = `small { a i64 b i64 }
big { pad [32]i64 }
huge { pad [33]i64 }
pair {
    one(v i64),
    two(v str),
}
nm {
    n(v char),
}
flag {
    on(v bool),
}
wide {
    w(v i128),
}

main = () {
    s small
    g big
    h huge
}
`

func layoutCodegen(t *testing.T) *codegen {
	t.Helper()
	mod := lowerForTest(t, layoutSrc)
	return &codegen{mod: mod, sb: &strings.Builder{}}
}

func TestLayoutOptionSizes(t *testing.T) {
	c := layoutCodegen(t)
	cases := []struct {
		raw   string
		size  int64
		k     int64
		boxed bool
	}{
		{"i64", 16, 1, false},
		{"bool", 16, 1, false}, // narrow scalars normalise to i64 payloads
		{"i32", 16, 1, false},
		{"f64", 16, 1, false},
		{"str", 24, 2, false},
		{"[]i64", 24, 2, false},
		{"txt", 264, 32, false},
		{"small", 24, 2, false},  // 16-byte struct payload
		{"big", 264, 32, false},  // 256-byte payload, exactly at the cap
		{"huge", 16, 1, true},    // 264-byte payload, over the cap -> boxed
	}
	for _, tc := range cases {
		lay := c.optionLayoutFor(tc.raw, optionInlineLimitDefault, nil)
		if lay.Size != tc.size || lay.K != tc.k || lay.Boxed != tc.boxed {
			t.Errorf("?%s: size=%d K=%d boxed=%v, want size=%d K=%d boxed=%v",
				tc.raw, lay.Size, lay.K, lay.Boxed, tc.size, tc.k, tc.boxed)
		}
		if lay.LLVM != "%option_"+optTypeKey(tc.raw) {
			t.Errorf("?%s: LLVM=%q", tc.raw, lay.LLVM)
		}
	}
}

func enumKeys(mod *Module) string {
	keys := make([]string, 0, len(mod.TaggedEnums))
	for k := range mod.TaggedEnums {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}

func findEnum(mod *Module, name string) *TaggedEnumInfo {
	if ei, ok := mod.TaggedEnums[name]; ok {
		return ei
	}
	for k, ei := range mod.TaggedEnums {
		if k == name || strings.HasSuffix(k, "."+name) {
			return ei
		}
	}
	return nil
}

func TestLayoutEnumUnion(t *testing.T) {
	c := layoutCodegen(t)

	t.Run("pair", func(t *testing.T) {
		ei := findEnum(c.mod, "pair")
		if ei == nil {
			t.Fatal("enum pair not found")
		}
		ub, ua, k, unit := c.enumUnionLayout(ei, nil)
		if unit || ub != 16 || ua != 8 || k != 2 {
			t.Fatalf("pair union = (%d,%d,%d,unit=%v), want (16,8,2,false)", ub, ua, k, unit)
		}
		off0, sz0, _, _ := c.enumVariantLayout(&ei.Variants[0], nil) // one(i64)
		if len(off0) != 1 || off0[0] != 0 || sz0 != 8 {
			t.Errorf("pair.one layout = %v/%d, want [0]/8", off0, sz0)
		}
		off1, sz1, _, _ := c.enumVariantLayout(&ei.Variants[1], nil) // two(str)
		if len(off1) != 1 || off1[0] != 0 || sz1 != 16 {
			t.Errorf("pair.two layout = %v/%d, want [0]/16", off1, sz1)
		}
	})

	// An all-unit tagged enum is not reachable through the surface syntax (a
	// bare `name { a, b }` parses as a plain enum of i64 constants), but the
	// layout engine must still answer correctly for the defensive path.
	t.Run("unit-only (synthetic)", func(t *testing.T) {
		ei := &TaggedEnumInfo{Variants: []VariantInfo{{Name: "a"}, {Name: "b"}}}
		_, _, k, unit := c.enumUnionLayout(ei, nil)
		if !unit || k != 0 {
			t.Fatalf("unit-only union = (k=%d,unit=%v), want (0,true)", k, unit)
		}
		sa := c.enumLLVMSizeAlign(ei, nil)
		if sa.Size != 1 || sa.Align != 1 {
			t.Fatalf("unit-only enum size = %+v, want {1 1}", sa)
		}
	})

	t.Run("i32 union", func(t *testing.T) {
		ei := findEnum(c.mod, "nm")
		if ei == nil {
			t.Fatal("enum nm not found; have: " + enumKeys(c.mod))
		}
		ub, ua, k, _ := c.enumUnionLayout(ei, nil)
		if ub != 4 || ua != 4 || k != 1 {
			t.Fatalf("nm union = (%d,%d,%d), want (4,4,1)", ub, ua, k)
		}
	})

	t.Run("i1 union", func(t *testing.T) {
		ei := findEnum(c.mod, "flag")
		if ei == nil {
			t.Fatal("enum flag not found; have: " + enumKeys(c.mod))
		}
		ub, ua, k, _ := c.enumUnionLayout(ei, nil)
		if ub != 1 || ua != 1 || k != 1 {
			t.Fatalf("flag union = (%d,%d,%d), want (1,1,1)", ub, ua, k)
		}
	})

	t.Run("i128 union", func(t *testing.T) {
		ei := findEnum(c.mod, "wide")
		if ei == nil {
			t.Fatal("enum wide not found")
		}
		ub, ua, k, _ := c.enumUnionLayout(ei, nil)
		if ub != 16 || ua != 16 || k != 1 {
			t.Fatalf("wide union = (%d,%d,%d), want (16,16,1)", ub, ua, k)
		}
	})
}
