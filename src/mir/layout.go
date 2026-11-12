package mir

import (
	"fmt"
	"strings"
)

// This file is the single source of truth for the byte-level layout of the two
// tagged aggregates: `?T` (option) and tagged enums.
//
// Target layout (see docs/docs/lang/syntax.md and AGENTS.md):
//
//	%option_<key> = type { i8 tag, [K x i64] payload }   ; 8 + 8K bytes
//	%tenum_X      = type { iN tag, [K x iW] payload }    ; unit-only: { iN }
//
// The tag width is chosen from the variant count (<=256 -> i8, <=65536 -> i16,
// <2^32 -> i32, else i64). The payload is a tight union: each variant's fields
// are laid out with their natural alignment, and the union is the max over
// variants. Option payloads are 8-byte normalised because the err variant is
// ALWAYS a heap-boxed pointer (8 bytes), so W == 8 for every option.

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
// stored inline iff `8 + roundUp8(sizeof(T)) <= limit`.
const optionInlineLimitDefault = 264

// optionInlineLimitMin is the smallest meaningful limit: the 8-byte tag area
// plus the 8-byte err pointer. Anything smaller could not even hold an i64.
const optionInlineLimitMin = 16

// optionPayloadCapBytes is the largest ok payload that can be inline at the
// default limit (264 - 8 = 256). `?txt` (256 bytes) sits exactly on it.
const optionPayloadCapBytes = optionInlineLimitDefault - 8

// tagWidth returns the discriminant width for an enum with n variants and the
// LLVM integer type that spells it. Tags are 0-based, so we need to represent
// n-1 and the bound is inclusive of n values.
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

// optTypeKey maps a raw element type to a single-injective LLVM-name fragment.
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

// OptionLayout describes one `?T`'s LLVM representation.
type OptionLayout struct {
	Key       string // raw element type, e.g. "i64"
	LLVM      string // "%option_i64"
	ElemRaw   string
	PayloadLT string // payload view type, e.g. "i64", "%str-long", "%txt"
	K         int64  // payload slot count (8-byte units)
	Boxed     bool   // ok payload lives behind a heap pointer in word0
	Recursive bool   // payload is/contains this same option (forces boxing)
	Size      int64  // 8 + 8*K
}

// optionLayoutFor computes the layout of `?elemRaw`. `seen` carries the elem
// raws already on the evaluation stack so a recursive payload (`node { next
// ?node }`) is forced to box instead of looping.
func (c *codegen) optionLayoutFor(elemRaw string, limit int64, seen map[string]bool) OptionLayout {
	key := optTypeKey(elemRaw)
	lay := OptionLayout{
		Key:     key,
		LLVM:    "%option_" + key,
		ElemRaw: elemRaw,
		K:       1,
	}
	lay.PayloadLT = c.optionPayloadLLVMType(elemRaw)

	recursive := seen[elemRaw]
	if limit <= 0 {
		limit = optionInlineLimitDefault
	}
	if limit < optionInlineLimitMin {
		limit = optionInlineLimitMin
	}

	if lay.PayloadLT == "" || recursive {
		lay.Boxed = true
		lay.Recursive = recursive
		lay.Size = 8 + 8*lay.K
		return lay
	}

	sa, ok := c.llvmSizeAlign(lay.PayloadLT, seen)
	if !ok {
		lay.Boxed = true
		lay.Size = 8 + 8*lay.K
		return lay
	}
	// 8-byte normalisation: the err variant always occupies an 8-byte pointer,
	// so the payload's effective width is at least one slot.
	need := lasRoundUp(lasMax(sa.Size, 8), 8)
	if 8+need <= limit {
		lay.Boxed = false
		lay.K = need / 8
		if lay.K < 1 {
			lay.K = 1
		}
	} else {
		lay.Boxed = true
		lay.K = 1
	}
	lay.Size = 8 + 8*lay.K
	return lay
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
	case "%option": // legacy singleton, only reachable before P1 lands
		return SizeAlign{Size: 32, Align: 8}, true
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
	// %option_<key>: consult the per-module layout table if it exists. During
	// P0 the table is not populated, so this returns false and callers box.
	if strings.HasPrefix(lt, "%option_") {
		if c.optByLLVM != nil {
			if lay := c.optByLLVM[lt]; lay != nil {
				return SizeAlign{Size: lay.Size, Align: 8}, true
			}
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
	lt := c.llvmTypeOf(t)
	return c.llvmSizeAlign(lt, seen)
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

// enumLLVMSizeAlign computes the struct size of `{ tag_t, [K x iW] }`.
func (c *codegen) enumLLVMSizeAlign(ei *TaggedEnumInfo, seen map[string]bool) SizeAlign {
	unionBytes, unionAlign, _, unitOnly := c.enumUnionLayout(ei, seen)
	tagBits, _ := tagWidth(len(ei.Variants))
	tagBytes := tagBits / 8
	if unitOnly {
		return SizeAlign{Size: tagBytes, Align: tagBytes}
	}
	w := unionAlign
	if w < 1 {
		w = 1
	}
	k := (unionBytes + w - 1) / w
	if k < 1 {
		k = 1
	}
	// { tag_t, [K x iW] }: tag padded to w, then K*w bytes.
	base := lasRoundUp(tagBytes, w)
	return SizeAlign{Size: base + k*w, Align: lasMax(w, tagBytes)}
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
// size, the max field alignment (the payload unit width W), and the number of
// W-sized array elements K.
func (c *codegen) enumUnionLayout(ei *TaggedEnumInfo, seen map[string]bool) (unionBytes, unionAlign, k int64, unitOnly bool) {
	unionAlign = 1
	for i := range ei.Variants {
		_, sz, al, _ := c.enumVariantLayout(&ei.Variants[i], seen)
		unionBytes = lasMax(unionBytes, sz)
		unionAlign = lasMax(unionAlign, al)
	}
	if unionBytes == 0 {
		return 0, 1, 0, true
	}
	w := unionAlign
	k = (unionBytes + w - 1) / w
	if k < 1 {
		k = 1
	}
	return unionBytes, unionAlign, k, false
}

// enumPayloadLT spells the payload array type `[K x iW]`.
func enumPayloadLT(k, w int64) string {
	if w < 1 {
		w = 1
	}
	return fmt.Sprintf("[%d x i%d]", k, w*8)
}
