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

// Script programs build a synthetic main after the initial metadata/layout
// passes. That path must not replace the populated enum entries with fresh ones:
// doing so silently restored the old i64-tag, one-word layout and made complex
// payload reads run past the end of the enum slot.
func TestEnumScriptLayoutsSurviveSyntheticMain(t *testing.T) {
	mod := lowerHIR(t, `result {
    valid(values []byte, next i64),
    text(v str),
    failed,
}

blob-result {
    valid(data []byte),
    too-big,
}

r result = valid([1, 2], 3)
r: {
    valid(values, next) -> print(values.len() + next)

    -> print('failed')
}
`)

	result := findEnum(mod, "result")
	if result == nil {
		t.Fatal("enum result not found; have: " + enumKeys(mod))
	}
	if result.TagLT != "i8" || result.PayloadSlots != 3 || result.UnionBytes != 24 {
		t.Errorf("result layout = tag %q, %d words, %d bytes; want i8, 3 words, 24 bytes",
			result.TagLT, result.PayloadSlots, result.UnionBytes)
	}
	valid := &result.Variants[0]
	if len(valid.FieldOffsets) != 2 || valid.FieldOffsets[0] != 0 || valid.FieldOffsets[1] != 16 {
		t.Errorf("valid offsets = %v, want [0 16]", valid.FieldOffsets)
	}
	blob := findEnum(mod, "blob-result")
	if blob == nil {
		t.Fatal("enum blob-result not found; have: " + enumKeys(mod))
	}
	if blob.TagLT != "i8" || blob.PayloadSlots != 2 || blob.UnionBytes != 16 {
		t.Errorf("blob-result layout = tag %q, %d words, %d bytes; want i8, 2 words, 16 bytes",
			blob.TagLT, blob.PayloadSlots, blob.UnionBytes)
	}

	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("script enum layout failed codegen: %v\n%s", err, mod.String())
	}
	for _, decl := range []string{
		"%tenum_result = type { i8, [3 x i64] }",
		"%tenum_blob_result = type { i8, [2 x i64] }",
	} {
		if !strings.Contains(ir, decl) {
			t.Errorf("missing enum declaration %q\n%s", decl, ir)
		}
	}
}

// A slice literal used as a tagged-enum constructor argument must take its
// element type from the variant field. Decimal literals otherwise infer []i64;
// storing that descriptor unchanged into a []byte field allocates eight bytes
// per element while readers use a one-byte stride, silently replacing every
// value after the first with a NUL byte. Hex literals would hide this because
// they already infer byte without contextual help.
func TestEnumCtorSliceLiteralUsesFieldType(t *testing.T) {
	mod := lowerForTest(t, `payload {
    blob(v []byte),
    nothing,
}

main = () {
    item payload = blob([65, 66])
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "_nolang_main")
	if !strings.Contains(body, "store i8 ") {
		t.Errorf("enum blob literal did not store byte elements; IR was:\n%s", body)
	}
	wideStride := false
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "= mul i64 ") && strings.HasSuffix(strings.TrimSpace(line), ", 8") {
			wideStride = true
			break
		}
	}
	if wideStride {
		t.Errorf("enum blob literal still allocates an eight-byte element stride; IR was:\n%s", body)
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

// Wrapping a tagged enum in an option, including the ?T produced by a safe
// index, must use the enum's aggregate layout rather than the scalar i64
// fallback. Punning the enum to i64 made optStoreRawBytes fail to resolve its
// size and store only a zero tag; unwrapping then observed the first variant.
// The nosql codec test covers the safe-index path end to end.
func TestOptionTaggedEnumPayloadUsesEnumType(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    text(v str),
    blob(v []byte),
    nothing,
}

main = () {
    source payload = number(1)
    wrapped ?payload = source
    wrapped: {
        number(value) -> print(value)

        ->
    }
}
`)
	c := newLayoutCodegen(mod)
	if got, want := c.optionPayloadLLVMType("payload"), "%tenum_payload"; got != want {
		t.Errorf("optionPayloadLLVMType(payload) = %q, want %q", got, want)
	}
	lay := c.optLayoutOfElem("payload")
	if lay.PayloadLT != "%tenum_payload" || lay.Boxed {
		t.Errorf("option layout = payload %q, boxed=%v; want inline %q", lay.PayloadLT, lay.Boxed, "%tenum_payload")
	}

	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	for _, decl := range []string{
		"%tenum_payload = type { i8, [2 x i64] }",
		"%option_" + optTypeKey("%tenum_payload") + " = type",
	} {
		if !strings.Contains(ir, decl) {
			t.Errorf("missing tagged-enum option declaration %q", decl)
		}
	}
	if strings.Contains(ir, "store %option_i64 { i8 0, [1 x i64] zeroinitializer }") {
		t.Error("tagged enum still stores a zeroed scalar option payload")
	}
	enumDecl := strings.Index(ir, "%tenum_payload = type")
	zeroGlobal := strings.Index(ir, "@__nolang_opt_zero_"+optTypeKey("%tenum_payload")+" =")
	if enumDecl < 0 || zeroGlobal < 0 || zeroGlobal < enumDecl {
		t.Errorf("enum zero global must follow its type declaration: enum=%d zero=%d", enumDecl, zeroGlobal)
	}
}

// A tagged enum stored inline in an option owns its payload independently of
// every value peeled out of that option. The initial dead-source wrap transfers
// its payload; each peel must deep-copy because the option retains its owner.
func TestOptionTaggedEnumCopiesClonePayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    text(v str),
    nothing,
}

text-len = (v payload) (n i64) {
    v: {
        text(s) -> n = s.len-bytes()

        -> n = 0
    }
}

main = () {
    source payload = text('A')
    wrapped ?payload = source
    first payload = wrapped
    second payload = wrapped
    print(text-len(first))
    print(text-len(second))
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	if !mod.OptionOwnsHeap("?payload") {
		t.Error("an inline option carrying a heap-owning tagged enum must own its payload")
	}
	body := definedBody(t, ir, "_nolang_main")
	const cloneCall = "@__nolang_enum_clone_tenum_payload"
	const dropCall = "@__nolang_enum_drop_tenum_payload"
	if got := strings.Count(body, cloneCall); got != 2 {
		t.Errorf("two option peels emitted %d enum deep clones, want 2\n%s", got, body)
	}
	if got := strings.Count(body, dropCall); got != 2 {
		t.Errorf("two independent peeled enums emitted %d destructors, want 2\n%s", got, body)
	}
	if got := strings.Count(body, "call void @__nolang_opt_drop_"); got != 1 {
		t.Errorf("the transferred option payload needs exactly one destructor, got %d\n%s", got, body)
	}
}

// A vector stores tagged enums by value, but a heap-owning variant's payload
// must be cloned just like a str or nested vec element. Otherwise the returned
// source enum and the vector both free the same payload.
func TestVecPushTaggedEnumClonesOwnedPayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    text(v str),
    nothing,
}

result {
    valid(v payload, next i64),
    failed,
}

make-result = (s str) (out result) {
    item payload = text(s)
    out = valid(item, 1)
}

main = () {
    values []payload
    decoded = make-result('owned')
    decoded: {
        valid(item, next) -> values.push(item)

        ->
    }
    stored = values[0]
    stored: {
        text(value) -> print(value)

        -> print(0)
    }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "_nolang_main")
	if !strings.Contains(body, "@__nolang_enum_clone_tenum_payload") {
		t.Errorf("vec.push of a heap-owning tagged enum did not deep-clone its payload; "+
			"the source destructor would leave the vector element dangling\n%s", body)
	}
}

// A safe index wraps its result in an owning ?T. For a heap-owning tagged
// enum, the element load must first deep-clone the live variant; otherwise the
// option destructor frees payload storage still owned by the container.
func TestSafeIndexTaggedEnumClonesOwnedPayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    text(v str),
    nothing,
}

read-value = (values []payload) (out ?payload) {
    out = values[0]
}

main = () {
    values []payload
    item payload = text('owned')
    values.push(item)
    result ?payload = read-value(values)
    result: {
        text(value) -> print(value)

        ->
    }
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "read_value")
	if got := strings.Count(body, "@__nolang_enum_clone_tenum_payload"); got != 1 {
		t.Errorf("safe index emitted %d tagged-enum deep clones, want 1; "+
			"the ?payload result must not alias the vector element\n%s", got, body)
	}
}

// Vector literals are lowered through indexed element stores. A heap-owning
// enum temporary must be cloned into the literal, just like vec.push and an
// explicit element assignment, or both the temporary and vector free it.
func TestVecLiteralTaggedEnumClonesOwnedPayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    text(v str),
    nothing,
}

main = () {
    values []payload = [text('owned')]
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "_nolang_main")
	if got := strings.Count(body, "@__nolang_enum_clone_tenum_payload"); got != 1 {
		t.Errorf("vector literal emitted %d tagged-enum deep clones, want 1; "+
			"the literal must not alias its temporary's payload\n%s", got, body)
	}
}

// An owning enum keeps its payload alive until its destructor runs. Extracting
// an owned slice must therefore produce a private copy, including []byte, which
// is not part of the struct-owned-leaf allowlist. The analysis must agree that
// the copy is owned so a non-consuming read drops it.
func TestOwnedEnumSliceExtractionClonesPayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    blob(v []byte),
    nothing,
}

to-blob = (v payload) (out ?[]byte) {
    v: {
        blob(bytes) -> out = ok(bytes)

        -> out = nil
    }
}

blob-len = (v payload) (n i64) {
    v: {
        blob(bytes) -> n = bytes.len()

        -> n = 0
    }
}

main = () {
    source payload = blob([0xff])
    wrapped ?[]byte = to-blob(source)
    n = blob-len(source)
    print(n)
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	// Count only clones emitted immediately after an enum payload load. Later
	// OpClone instructions (for example, a method receiver) also clone slices and
	// would mask a shallow OpEnumField extraction.
	checkExtraction := func(name, body string) {
		t.Helper()
		lines := strings.Split(body, "\n")
		extractions := 0
		for i, line := range lines {
			if !strings.Contains(line, "= load %vec, %vec* %ep.") {
				continue
			}
			extractions++
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "= call %vec @__nolang_vec_clone_") {
				t.Errorf("%s has a shallow owning-enum slice extraction; the payload load must be immediately deep-cloned\n%s", name, body)
			}
		}
		if extractions == 0 {
			t.Errorf("%s emitted no enum slice extraction to inspect\n%s", name, body)
		}
		if strings.Contains(body, "call void @vec_retain(") {
			t.Errorf("%s still retains an owning-enum slice extraction as a borrow\n%s", name, body)
		}
	}
	toBlob := definedBody(t, ir, "to_blob")
	checkExtraction("to-blob", toBlob)
	if !strings.Contains(toBlob, "@__nolang_enum_drop_tenum_payload") {
		t.Errorf("to-blob must still destroy its owning enum copy\n%s", toBlob)
	}
	blobLen := definedBody(t, ir, "blob_len")
	checkExtraction("blob-len", blobLen)
}

// The common single-use enum remains a move: cloning there would change the
// established ownership path and add an unnecessary allocation.
func TestSingleUseEnumSliceExtractionMovesPayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    blob(v []byte),
    nothing,
}

blob-len = () (n i64) {
    local payload = blob([0xff])
    local: {
        blob(bytes) -> n = bytes.len()

        -> n = 0
    }
}

main = () {
    print(blob-len())
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "blob_len")
	if strings.Contains(body, "call %vec @__nolang_vec_clone_") {
		t.Errorf("single-use enum slice extraction unexpectedly cloned its moved payload\n%s", body)
	}
}

// vec.insert is the third by-value tagged-enum container sink (alongside push
// and indexed stores). It must clone the live variant before either branch
// stores the element, or the inserted value dangles after its source is dropped.
func TestVecInsertTaggedEnumClonesOwnedPayload(t *testing.T) {
	mod := lowerForTest(t, `payload {
    number(v i64),
    text(v str),
    nothing,
}

main = () {
    values []payload
    item payload = text('owned')
    values.insert(0, item)
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v\n%s", err, mod.String())
	}
	body := definedBody(t, ir, "_nolang_main")
	if got := strings.Count(body, "@__nolang_enum_clone_tenum_payload"); got != 1 {
		t.Errorf("vec.insert emitted %d tagged-enum deep clones, want 1; "+
			"the inserted element must not alias its source payload\n%s", got, body)
	}
	if !strings.Contains(body, "ptrtoint ptr getelementptr (%tenum_payload, ptr null, i64 1) to i64") {
		t.Errorf("vec.insert must derive its stride from the tagged-enum element type, not scalar i64\n%s", body)
	}
	if got := strings.Count(body, ", %tenum_payload* %ivep"); got != 2 {
		t.Errorf("vec.insert emitted %d tagged-enum element stores, want 2 (grow and fast paths)\n%s", got, body)
	}
}
