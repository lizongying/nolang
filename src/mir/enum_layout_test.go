package mir

import (
	"strings"
	"testing"
)

// enumTightSrc exercises the three shapes that the tight-union layout has to get
// right: a variant whose fields are NOT word-sized (three char, 4 bytes each ->
// byte offsets 0/4/8), a wide descriptor payload (str, 16 bytes), and a
// payload-less variant (0 bytes, so it must not widen the union).
//
// `char`, not `i32`: MIR has ONE integer register width, so `i32` lowers to an
// 8-byte i64 payload and every integer variant is already word-sized. `char`
// lowers to i32 (4 bytes) and `bool` to i1, which is what makes a non-word
// field offset reachable at all.
const enumTightSrc = `tight {
    triple(a char, b char, c char),
    pair(a char, b char),
    wide(s str),
    unit,
}

main = () {
    t tight
}
`

func TestEnumTightUnionLayout(t *testing.T) {
	mod := lowerForTest(t, enumTightSrc)
	c := newLayoutCodegen(mod)
	ei := findEnum(mod, "tight")
	if ei == nil {
		t.Fatal("enum tight not found; have: " + enumKeys(mod))
	}

	// The discriminant is one byte: four variants fit far below the 256 bound.
	if ei.TagLT != "i8" || ei.TagBits != 8 {
		t.Errorf("tag = (%q,%d), want (i8,8)", ei.TagLT, ei.TagBits)
	}

	// triple: 12 bytes, fields at byte offsets 0/4/8 — the whole point of byte
	// addressing. Under the old slot layout the same variant took 24 bytes.
	if got := ei.Variants[0].FieldOffsets; len(got) != 3 || got[0] != 0 || got[1] != 4 || got[2] != 8 {
		t.Errorf("triple offsets = %v, want [0 4 8]", got)
	}
	if sz := ei.Variants[0].VariantSize; sz != 12 {
		t.Errorf("triple size = %d, want 12", sz)
	}
	if got := ei.Variants[1].FieldOffsets; len(got) != 2 || got[0] != 0 || got[1] != 4 {
		t.Errorf("pair offsets = %v, want [0 4]", got)
	}
	// wide(str): the descriptor is 16 bytes, so it alone sets the union size.
	if sz := ei.Variants[2].VariantSize; sz != 16 {
		t.Errorf("wide size = %d, want 16", sz)
	}
	// unit: no payload, so it neither widens nor aligns anything.
	if sz := ei.Variants[3].VariantSize; sz != 0 {
		t.Errorf("unit size = %d, want 0", sz)
	}

	// The union is the WIDEST variant, never the sum: 12 + 8 + 16 would be 36.
	if ei.UnionBytes != 16 || ei.UnionAlign != 8 || ei.PayloadSlots != 2 {
		t.Errorf("union = (%d bytes, align %d, %d words), want (16,8,2)",
			ei.UnionBytes, ei.UnionAlign, ei.PayloadSlots)
	}
	if ei.UnitOnly {
		t.Error("tight is not unit-only")
	}

	// The declared struct is the tag plus the union rounded to whole words.
	if got, want := c.enumStructBody(ei), "{ i8, [2 x i64] }"; got != want {
		t.Errorf("struct body = %q, want %q", got, want)
	}
	if sa := c.enumLLVMSizeAlign(ei, nil); sa.Size != 24 || sa.Align != 8 {
		t.Errorf("enum size = %+v, want {24 8}", sa)
	}
}

// TestEnumStructBodyUnitOnly pins the payload-less shape: a tagged enum whose
// every variant carries nothing is just the tag (one byte for <=256 variants),
// not a 16-byte tag+zeroed-word pair. Not reachable through the surface syntax
// today (a bare `name { a, b }` is a plain i64 enum), so the table is built by
// hand — exactly as TestLayoutEnumUnion does.
func TestEnumStructBodyUnitOnly(t *testing.T) {
	c := newLayoutCodegen(&Module{})
	ei := &TaggedEnumInfo{
		Variants: []VariantInfo{{Name: "a"}, {Name: "b"}},
		TagLT:    "i8",
		TagBits:  8,
		UnitOnly: true,
	}
	if got, want := c.enumStructBody(ei), "{ i8 }"; got != want {
		t.Errorf("unit-only body = %q, want %q", got, want)
	}
	if got := c.enumZeroValue(ei, 1); got != "{ i8 1 }" {
		t.Errorf("unit-only zero value = %q, want %q", got, "{ i8 1 }")
	}
}

// TestEnumTagWidthFromVariantCount pins the width the discriminant gets at each
// boundary, and the fact that a zeroed value spells the tag at that width.
func TestEnumTagWidthFromVariantCount(t *testing.T) {
	c := newLayoutCodegen(&Module{})
	cases := []struct {
		n    int
		want string
	}{
		{2, "{ i8 2 }"},
		{256, "{ i8 2 }"},
		{257, "{ i16 2 }"},
		{65537, "{ i32 2 }"},
	}
	for _, tc := range cases {
		ei := &TaggedEnumInfo{Variants: make([]VariantInfo, tc.n)}
		ei.TagBits, ei.TagLT = tagWidth(tc.n)
		ei.UnitOnly = true
		if got := c.enumZeroValue(ei, 2); got != tc.want {
			t.Errorf("n=%d zero value = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestEnumFieldOffsetFallsBackToLayout covers a variant that was never
// registered (no FieldOffsets): the accessor must compute the same numbers the
// registration would have, not return zero.
func TestEnumFieldOffsetFallsBackToLayout(t *testing.T) {
	mod := lowerForTest(t, enumTightSrc)
	c := newLayoutCodegen(mod)
	bare := &VariantInfo{Name: "triple", Fields: []string{"char", "char", "char"}}
	for idx, want := range []int64{0, 4, 8} {
		if got := c.enumFieldOffset(bare, idx); got != want {
			t.Errorf("computed offset[%d] = %d, want %d", idx, got, want)
		}
	}
	if got := c.enumFieldOffset(bare, 9); got != 0 {
		t.Errorf("out-of-range offset = %d, want 0", got)
	}
}

// TestEnumPayloadFieldPtrUsesByteDisplacement pins the emitted addressing form:
// a bitcast to i8* plus a byte GEP at 8+offset, NOT a `[K x i64]` element GEP
// (which could only express word-aligned offsets).
func TestEnumPayloadFieldPtrUsesByteDisplacement(t *testing.T) {
	c := newLayoutCodegen(&Module{})
	var b strings.Builder
	ptr := c.enumPayloadFieldPtr(&b, "%tenum_tight", "%v1.s", 4, "i32")
	ir := b.String()
	if !strings.Contains(ir, "getelementptr inbounds i8, i8* %ep.") {
		t.Errorf("addressing is not a byte displacement:\n%s", ir)
	}
	if !strings.Contains(ir, "i64 12\n") {
		t.Errorf("displacement should be payloadOffset+4 = 12:\n%s", ir)
	}
	if !strings.Contains(ir, "bitcast i8* %ep.") || !strings.Contains(ir, "to i32*") {
		t.Errorf("field pointer is not typed by the field's own type:\n%s", ir)
	}
	if ptr == "" {
		t.Error("empty pointer result")
	}
}
