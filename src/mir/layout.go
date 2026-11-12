package mir

import (
	"fmt"
	"strings"
)

// This file is the single source of truth for the byte-level layout of the two
// tagged aggregates: `?T` (option) and tagged enums.
//
// Layout (see docs/docs/lang/syntax.md §option / §enum and AGENTS.md §3.3):
//
//	%option_<key> = type { i8 tag, [K x i64] payload }      ; 8 + 8K bytes
//	%tenum_X      = type { iN tag, [K x i64] payload }      ; 8 + 8K bytes
//	%tenum_X      = type { iN tag }                         ; unit-only variants
//
// Three properties this file exists to guarantee:
//
//  1. The tag width follows the VARIANT COUNT, not a fixed i64:
//     <=256 -> i8, <=65536 -> i16, < 2^32 -> i32, else i64. A `?T` has three
//     variants so its tag is always one byte.
//
//  2. The payload is sized to the ACTUAL payload, not to a module-wide
//     worst case. `?i64` is 16 bytes, `?str` is 24, `?txt` (256 bytes) is 264,
//     and anything above the limit degrades to a heap pointer (16 bytes).
//
//  3. The layout is a PURE FUNCTION of (payload LLVM type, option-inline
//     threshold, module struct table). hir2mir folds module-level `?T`
//     globals and sizes enum payload slots; codegen emits the accesses. Both
//     call the same functions here, so they cannot drift.
//
// LIMITATION (documented, not silently wrong): a payload whose alignment
// exceeds 8 bytes (i128 / u128) is HEAP-BOXED as an option payload, and inside
// a tagged enum it is laid out at 8-byte alignment. Nothing in the std corpus
// puts an i128 in a tagged enum.

// SizeAlign is the byte size and alignment of a type.
type SizeAlign struct {
	Size  int64
	Align int64
}

// lasRoundUp rounds n up to a multiple of a (a >= 1).
func lasRoundUp(n, a int64) int64 {
	if a <= 1 {
		return n
	}
	return ((n + a - 1) / a) * a
}

func lasMax(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// optionInlineLimitDefault is the default `option-inline-threshold` in BYTES of
// the whole option struct: 264 = 1 tag + 7 padding + 256 payload. A payload is
// stored inline iff `8 + roundUp8(sizeof(T)) <= limit`, i.e. iff
// sizeof(T) <= 256 at the default.
const optionInlineLimitDefault = 264

// optionInlineLimitMin is the smallest meaningful limit: the 8-byte tag area
// plus one 8-byte payload word. Anything smaller could not even hold an i64.
const optionInlineLimitMin = 16

// optionTagOffset / optionPayloadOffset are the fixed byte offsets inside
// every option struct. The payload always starts at 8: the struct is
// `{ i8, [K x i64] }` and LLVM pads the tag to the array's alignment.
const (
	optionTagOffset     = 0
	optionPayloadOffset = 8
)

// optionInlineLimit resolves the configured threshold, clamping/reporting.
func optionInlineLimit(threshold int) int64 {
	t := int64(threshold)
	if t <= 0 {
		return optionInlineLimitDefault
	}
	if t < optionInlineLimitMin {
		return optionInlineLimitMin
	}
	return t
}

// tagWidth returns the discriminant width for an enum with n variants and the
// LLVM integer type that spells it. Tags are 0-based, so the bound is inclusive
// of n values.
func tagWidth(nVariants int) (bits int64, lt string) {
	switch {
	case nVariants <= 256:
		return 8, "i8"
	case nVariants <= 65536:
		return 16, "i16"
	case int64(nVariants) < (int64(1) << 32):
		return 32, "i32"
	default:
		return 64, "i64"
	}
}

// optTypeKey maps a type spelling to a single-injective LLVM-name fragment.
// It is deliberately NOT `sanitize`: sanitize collapses different spellings
// (`[]i64` and `__i64`), which is fine for a debug-only struct name but fatal
// for an ABI type name. Every non-alphanumeric byte is escaped.
func optTypeKey(raw string) string {
	var b strings.Builder
	b.Grow(len(raw) + 8)
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case ch == '_':
			b.WriteString("__")
		default:
			fmt.Fprintf(&b, "_x%02x", ch)
		}
	}
	return b.String()
}

// OptionLayout describes one `?T`'s LLVM representation, keyed by the payload
// LLVM type (two options with the same payload type are the same type: they
// have identical layout by construction).
type OptionLayout struct {
	Key       string // optTypeKey(PayloadLT)
	LLVM      string // "%option_<Key>"
	PayloadLT string // payload view type, e.g. "i64", "%str-long", "%txt"
	SlotLT    string // "[K x i64]"
	K         int64  // payload slot count (8-byte units)
	Boxed     bool   // payload lives behind a heap pointer in word 0
	Recursive bool   // payload is/contains this same option (forces boxing)
	Size      int64  // 8 + 8*K
	done      bool   // fully computed (false == recursion placeholder)
}

// optLayoutOfPayload returns (memoising) the layout of an option whose payload
// is payloadLT.
//
// RECURSION: the map entry is installed BEFORE the size is computed, so a
// payload that mentions its own option (`node { next ?node }`) re-enters here,
// finds the placeholder, and marks it Recursive. The outer computation then
// keeps the payload boxed — which is the only self-consistent answer: a boxed
// payload is one word, so `%node` has a size that does not depend on sizeof
// `%option_node`.
func (c *codegen) optLayoutOfPayload(payloadLT string) *OptionLayout {
	if c.optLayouts == nil {
		c.optLayouts = map[string]*OptionLayout{}
		c.optByLLVM = map[string]*OptionLayout{}
	}
	if lay := c.optLayouts[payloadLT]; lay != nil {
		if !lay.done {
			// Re-entered while computing: this option is part of its own
			// payload. Force boxing and say so.
			lay.Recursive = true
		}
		return lay
	}
	// An unresolvable payload still needs a name; it is boxed (see the `!= ""`
	// guard below), so the spelling is all that matters.
	keySrc := payloadLT
	if keySrc == "" {
		keySrc = "unknown"
	}
	key := optTypeKey(keySrc)
	lay := &OptionLayout{
		Key:       key,
		LLVM:      "%option_" + key,
		PayloadLT: payloadLT,
		SlotLT:    "[1 x i64]",
		K:         1,
		Boxed:     true,
		Size:      optionPayloadOffset + 8,
	}
	c.optLayouts[payloadLT] = lay
	c.optByLLVM[lay.LLVM] = lay

	if payloadLT != "" && !lay.Recursive {
		// NOTE: the ERR variant does not participate here. It is always
		// heap-boxed (errPayloadBoxed), so it never needs room in the payload
		// area and must not constrain an OK payload that happens to share its
		// type — `?str` stays a 24-byte inline option even though a tag-2
		// `?str` keeps its message in a box.
		if sa, ok := c.llvmSizeAlign(payloadLT, map[string]bool{}); ok && sa.Align <= 8 && !lay.Recursive {
			limit := c.optInlineLimit
			if limit <= 0 {
				limit = optionInlineLimitDefault
			}
			// The payload occupies at least one word (an empty struct still
			// needs an address), and the slot is a whole number of i64s.
			need := lasRoundUp(lasMax(sa.Size, 8), 8)
			if optionPayloadOffset+need <= limit {
				lay.Boxed = false
				lay.K = need / 8
				lay.SlotLT = fmt.Sprintf("[%d x i64]", lay.K)
				lay.Size = optionPayloadOffset + need
			}
		}
	}
	lay.done = true
	return lay
}

// optLayoutOfElem is optLayoutOfPayload for a nolang element spelling.
func (c *codegen) optLayoutOfElem(elemRaw string) *OptionLayout {
	return c.optLayoutOfPayload(c.optionPayloadLLVMType(elemRaw))
}

// optLayoutOfLLVM finds the layout whose emitted LLVM name is lt.
func (c *codegen) optLayoutOfLLVM(lt string) *OptionLayout {
	if c.optByLLVM == nil {
		return nil
	}
	return c.optByLLVM[lt]
}

// optionPayloadInline reports whether a payload of LLVM type payloadLT is
// stored inline in its option. Anything larger, over-aligned, of unknown size,
// or recursive is heap-boxed. The decision must be CONSERVATIVE: claiming a
// payload fits when it does not corrupts the bytes after it, while boxing a
// payload that would have fitted only costs a malloc.
func (c *codegen) optionPayloadInline(payloadLT string) bool {
	return !c.optLayoutOfPayload(payloadLT).Boxed
}

// collectOptionLayouts populates the per-module option layout tables before the
// prelude is emitted. Every `?T` in the module is interned as a Type, so
// scanning mod.Types is complete; the tables are also filled lazily by
// optLayoutOfElem, which keeps a late-discovered option (one that only appears
// inside a function body) declared.
func (c *codegen) collectOptionLayouts() {
	limit := optionInlineLimit(c.mod.OptionInlineThreshold)
	if t := int64(c.mod.OptionInlineThreshold); t != 0 && t < optionInlineLimitMin {
		c.fail("option-inline-threshold %d is below the minimum %d (tag + one 8-byte payload word)", t, optionInlineLimitMin)
	}
	c.optInlineLimit = limit
	if c.optLayouts == nil {
		c.optLayouts = map[string]*OptionLayout{}
		c.optByLLVM = map[string]*OptionLayout{}
	}
	// Snapshot the length: resolution may intern more types (views /
	// containers), which appends to mod.Types.
	n := len(c.mod.Types)
	var elems []string
	for i := 0; i < n; i++ {
		t := c.mod.Type(TypeID(i))
		if t == nil || t.Kind != KindOption || t.T == nil || !t.T.IsOption() || t.T.Elem == nil {
			continue
		}
		elems = append(elems, t.T.ElemRaw())
	}
	for _, e := range elems {
		c.optLayoutOfElem(e)
	}
}

// sortedOptionLayouts returns every registered option layout ordered by LLVM
// name, for deterministic prelude emission.
func (c *codegen) sortedOptionLayouts() []*OptionLayout {
	out := make([]*OptionLayout, 0, len(c.optByLLVM))
	for _, lay := range c.optByLLVM {
		out = append(out, lay)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].LLVM > out[j].LLVM; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// newLayoutCodegen builds a codegen carrying ONLY the module, so the layout
// engine can run outside emitFunc. hir2mir needs the identical numbers for
// module-level `?T` constants and enum payload slots, and re-deriving them
// there is exactly the kind of drift this file exists to prevent.
//
// The threshold must be resolved HERE too: `c.optInlineLimit == 0` means "unset",
// and optLayoutOfPayload would then fall back to the DEFAULT rather than to the
// module's configured value. Analysis and emitter would then disagree about what
// is boxed, which is a double free, not a leak.
func newLayoutCodegen(mod *Module) *codegen {
	return &codegen{
		mod:            mod,
		optLayouts:     map[string]*OptionLayout{},
		optByLLVM:      map[string]*OptionLayout{},
		optInlineLimit: optionInlineLimit(mod.OptionInlineThreshold),
	}
}

// llvmSizeAlign computes sizeof/alignof for an LLVM type string, or ok=false
// when it cannot be proven. Callers must treat !ok as "unknown" and be
// conservative (box / pad), never as zero.
func (c *codegen) llvmSizeAlign(lt string, seen map[string]bool) (SizeAlign, bool) {
	if lt == "" {
		return SizeAlign{}, false
	}
	if n, ok := mirScalarByteSize[lt]; ok {
		return SizeAlign{Size: n, Align: n}, true
	}
	if lt == "i128" {
		return SizeAlign{Size: 16, Align: 16}, true
	}
	if strings.HasSuffix(lt, "*") {
		return SizeAlign{Size: 8, Align: 8}, true
	}
	switch lt {
	case "%str-long":
		return SizeAlign{Size: 16, Align: 8}, true // { i32 len, i32 cap, i8* data }
	case "%vec":
		return SizeAlign{Size: 16, Align: 8}, true // { i32 len, i32 cap, i64 data }
	case "%txt":
		return SizeAlign{Size: 256, Align: 1}, true
	}
	// Fixed array: [N x T].
	if strings.HasPrefix(lt, "[") {
		end := strings.LastIndex(lt, "]")
		if end < 0 {
			return SizeAlign{}, false
		}
		inner := lt[1:end]
		x := strings.Index(inner, " x ")
		if x < 0 {
			return SizeAlign{}, false
		}
		var n int64
		if _, err := fmt.Sscanf(inner[:x], "%d", &n); err != nil || n < 0 {
			return SizeAlign{}, false
		}
		sa, ok := c.llvmSizeAlign(inner[x+3:], seen)
		if !ok {
			return SizeAlign{}, false
		}
		return SizeAlign{Size: lasRoundUp(n*sa.Size, sa.Align), Align: sa.Align}, true
	}
	// %option_<key>: resolve through the option layout table, creating the
	// entry if this is the first mention.
	if strings.HasPrefix(lt, "%option_") {
		if lay := c.optLayoutOfLLVM(lt); lay != nil && lay.done {
			return SizeAlign{Size: lay.Size, Align: 8}, true
		}
		return SizeAlign{}, false
	}
	// %tenum_<name>: compute from the variant table.
	if strings.HasPrefix(lt, "%tenum_") {
		if ei := c.taggedEnumByLLVM(lt); ei != nil {
			return c.enumLLVMSizeAlign(ei, seen), true
		}
		return SizeAlign{}, false
	}
	// Named struct: reverse-map the LLVM name to its StructFields key.
	if strings.HasPrefix(lt, "%") {
		if key := c.structKeyByLLVM(lt); key != "" {
			return c.structSizeAlign(key, seen)
		}
	}
	return SizeAlign{}, false
}

// rawSizeAlign resolves a raw nolang type to its size/alignment by going
// through the authoritative llvmTypeOf mapping (no duplicated type rules).
func (c *codegen) rawSizeAlign(raw string, seen map[string]bool) (SizeAlign, bool) {
	if raw == "" {
		return SizeAlign{}, false
	}
	if seen != nil && seen["raw:"+raw] {
		return SizeAlign{}, false
	}
	tid := c.mod.internType(raw)
	if tid == NoType {
		return SizeAlign{}, false
	}
	t := c.mod.Type(tid)
	if t == nil {
		return SizeAlign{}, false
	}
	return c.llvmSizeAlign(c.llvmTypeOf(t), seen)
}

// structSizeAlign lays out a registered struct by its fields, honouring the
// pointer-layout override (a pointer-laid-out field occupies 8 bytes).
func (c *codegen) structSizeAlign(key string, seen map[string]bool) (SizeAlign, bool) {
	fields, ok := c.mod.StructFields[key]
	if !ok {
		return SizeAlign{}, false
	}
	next := seen
	if next == nil {
		next = map[string]bool{}
	}
	var cur, maxAlign int64 = 0, 1
	for _, f := range fields {
		var sz, al int64
		if c.mod.FieldIsPointer(f) {
			sz, al = 8, 8
		} else {
			sa, ok := c.rawSizeAlign(f.TypeRaw, next)
			if !ok {
				return SizeAlign{}, false
			}
			sz, al = sa.Size, sa.Align
		}
		if al < 1 {
			al = 1
		}
		cur = lasRoundUp(cur, al)
		cur += sz
		maxAlign = lasMax(maxAlign, al)
	}
	return SizeAlign{Size: lasRoundUp(cur, maxAlign), Align: maxAlign}, true
}

// taggedEnumByLLVM finds the enum whose emitted LLVM name equals lt.
func (c *codegen) taggedEnumByLLVM(lt string) *TaggedEnumInfo {
	for name, ei := range c.mod.TaggedEnums {
		if "%tenum_"+sanitize(name) == lt {
			return ei
		}
	}
	return nil
}

// structKeyByLLVM finds the StructFields key whose emitted name equals lt.
func (c *codegen) structKeyByLLVM(lt string) string {
	for k := range c.mod.StructFields {
		if "%"+sanitize(k) == lt {
			return k
		}
	}
	return ""
}

// enumPayloadLT spells the enum payload array type `[K x i64]`.
func enumPayloadLT(k int64) string {
	if k < 1 {
		k = 1
	}
	return fmt.Sprintf("[%d x i64]", k)
}

// enumPayloadOffset is the byte offset of the payload union inside a tagged
// enum struct. It equals optionPayloadOffset for the same reason: the array's
// 8-byte alignment pads the tag out to a full word.
const enumPayloadOffset = optionPayloadOffset

// enumTagLT is the discriminant's LLVM type. The fallback only fires for an
// enum whose variant table is unknown (an unresolved type), where the old
// fixed-i64 spelling is the least surprising answer.
func (c *codegen) enumTagLT(ei *TaggedEnumInfo) string {
	if ei != nil && ei.TagLT != "" {
		return ei.TagLT
	}
	return "i64"
}

// enumWords is the payload's word count (the K of `[K x i64]`), never zero:
// even a one-byte payload occupies a whole word so the union stays 8-aligned.
func enumWords(ei *TaggedEnumInfo) int64 {
	if ei == nil || ei.PayloadSlots < 1 {
		return 1
	}
	return ei.PayloadSlots
}

// enumHasPayload reports whether the struct carries a payload union at all.
// A unit-only enum is `{ iN }` — one or four bytes, not sixteen.
func enumHasPayload(ei *TaggedEnumInfo) bool {
	return ei != nil && !ei.UnitOnly
}

// enumStructBody spells the LLVM body of a tagged enum, for the prelude
// declaration and for the zeroed-initializer stores that build a value.
func (c *codegen) enumStructBody(ei *TaggedEnumInfo) string {
	if !enumHasPayload(ei) {
		return fmt.Sprintf("{ %s }", c.enumTagLT(ei))
	}
	return fmt.Sprintf("{ %s, %s }", c.enumTagLT(ei), enumPayloadLT(enumWords(ei)))
}

// enumTagLit spells `iN <tag>` for a store/subscript position.
func (c *codegen) enumTagLit(ei *TaggedEnumInfo, tag int64) string {
	return fmt.Sprintf("%s %d", c.enumTagLT(ei), tag)
}

// enumWidenTag appends the zext that lifts a tag register to the i64 the rest of
// the compiler works in (OpEnumTag results, comparisons against arm tags), and
// returns the widened SSA name. Tags are 0-based so the extension is unsigned.
func (c *codegen) enumWidenTag(b *strings.Builder, reg string, ei *TaggedEnumInfo) string {
	lt := c.enumTagLT(ei)
	if lt == "i64" {
		return reg
	}
	c.loadSeq++
	w := fmt.Sprintf("%%ez.%d", c.loadSeq)
	b.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", w, lt, reg))
	return w
}

// enumFieldOffset returns the BYTE offset of field idx inside variant v's
// payload union. The variant table records the offsets at registration
// (hir2mir.registerTaggedEnum); recomputing here is the fallback for a variant
// that was never registered, and it is the SAME function the registration used,
// so the two cannot disagree.
//
// Byte offsets — not 8-byte slots — are what makes the union compact: a variant
// `(a i32)(b i32)` occupies 8 bytes, not 16. Every offset is naturally aligned
// relative to the (8-byte aligned) union base, so each field access is
// correctly aligned for its own type.
func (c *codegen) enumFieldOffset(v *VariantInfo, idx int) int64 {
	if v == nil || idx < 0 || idx >= len(v.Fields) {
		return 0
	}
	if len(v.FieldOffsets) == len(v.Fields) {
		return v.FieldOffsets[idx]
	}
	offs, _, _, _ := c.enumVariantLayout(v, map[string]bool{})
	if idx < len(offs) {
		return offs[idx]
	}
	return 0
}

// enumPayloadFieldPtr appends the address computation for field idx of the
// variant stored at `base` (a `<enumLT>*`) and returns the SSA name holding a
// `<fieldLT>*`.
//
// The struct pointer is bitcast to i8* and advanced by the field's BYTE offset
// rather than by an index into `[K x i64]`: the union is packed with natural
// alignment, so a field is not necessarily on a word boundary (a `u8` followed
// by `u32` sits at offsets 0 and 4). Both the read and the write side go
// through here, so a store and its matching load always agree.
func (c *codegen) enumPayloadFieldPtr(b *strings.Builder, enumLT, base string, off int64, fieldLT string) string {
	if fieldLT == "" || fieldLT == "void" {
		fieldLT = "i64"
	}
	c.loadSeq++
	ap := fmt.Sprintf("%%ep.%d", c.loadSeq)
	b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", ap, enumLT, base))
	c.loadSeq++
	fp := fmt.Sprintf("%%ep.%d", c.loadSeq)
	b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %d\n",
		fp, ap, enumPayloadOffset+off))
	c.loadSeq++
	rp := fmt.Sprintf("%%ep.%d", c.loadSeq)
	b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", rp, fp, fieldLT))
	return rp
}

// enumLLVMSizeAlign computes the struct size of `{ tag_t, [K x i64] }`.
func (c *codegen) enumLLVMSizeAlign(ei *TaggedEnumInfo, seen map[string]bool) SizeAlign {
	unionBytes, _, _, unitOnly := c.enumUnionLayout(ei, seen)
	tagBits, _ := tagWidth(len(ei.Variants))
	tagBytes := tagBits / 8
	if tagBytes < 1 {
		tagBytes = 1
	}
	if unitOnly {
		return SizeAlign{Size: tagBytes, Align: tagBytes}
	}
	k := (unionBytes + 7) / 8
	if k < 1 {
		k = 1
	}
	return SizeAlign{Size: optionPayloadOffset + 8*k, Align: 8}
}

// enumVariantLayout lays out one variant's payload fields with their natural
// alignment. rounded=true means at least one field's exact size was unknown and
// an 8-byte upper bound was used (never underestimate).
func (c *codegen) enumVariantLayout(v *VariantInfo, seen map[string]bool) (offsets []int64, size, align int64, rounded bool) {
	align = 1
	var cur int64
	for _, raw := range v.Fields {
		sa, ok := c.rawSizeAlign(raw, seen)
		if !ok {
			sa = SizeAlign{Size: 8, Align: 8}
			rounded = true
		}
		if sa.Align < 1 {
			sa.Align = 1
		}
		cur = lasRoundUp(cur, sa.Align)
		offsets = append(offsets, cur)
		cur += sa.Size
		align = lasMax(align, sa.Align)
	}
	size = lasRoundUp(cur, align)
	return
}

// enumUnionLayout computes the tight union over all variants: the max variant
// size (byte-precise), the max field alignment, and unitOnly.
//
// This is what makes a tagged enum save memory: the payload is ONE buffer sized
// to the largest variant, not the SUM of every variant's fields.
func (c *codegen) enumUnionLayout(ei *TaggedEnumInfo, seen map[string]bool) (unionBytes, unionAlign, k int64, unitOnly bool) {
	unionAlign = 1
	for i := range ei.Variants {
		_, sz, al, _ := c.enumVariantLayout(&ei.Variants[i], seen)
		if sz > unionBytes {
			unionBytes = sz
		}
		unionAlign = lasMax(unionAlign, al)
	}
	if unionBytes == 0 {
		return 0, 1, 0, true
	}
	k = (unionBytes + 7) / 8
	if k < 1 {
		k = 1
	}
	return unionBytes, unionAlign, k, false
}
