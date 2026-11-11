package mir

// builtin_str_bytes.go — str.bytes(): a zero-copy []byte VIEW over a str.
//
// Kept in its own file so this emitter does not have to touch codegen.go,
// which other sessions are actively editing (the %str-long header layout in
// particular). All layout-sensitive reads go through existing helpers:
//
//	c.strLenI64(sv)   -> logical byte length, widened to the i64 Nolang API
//	c.dataPtrOf(recv) -> i8* backing buffer (GEP field 2, layout-agnostic)
//
// The result is a BORROWED %vec: { len = str.len, cap = 0, data = ptrtoint }.
// cap == 0 is the marker @vec_free / @__nolang_vec_free_* use for "does not
// own its buffer", so the view can go out of scope without freeing the
// string's storage. Writes through the view are visible in the original str.
//
// Contrast str.to-bytes() in src/std/str.no: a real std function that does an
// O(N) byte-by-byte copy into a fresh with-len buffer.

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// emitBuiltinStrBytes lowers the "str-bytes" ForwardFunc. inst.Args[0] is the
// str receiver, inst.Dst is the []byte output slot.
func (c *codegen) emitBuiltinStrBytes(inst *Inst) error {
	const ff = "str-bytes"
	if len(inst.Args) < 1 {
		return i18n.Errorf("1u2vckag", "builtin %s: missing receiver", ff)
	}
	// A discarded result (`s.bytes()` as a bare statement) has no destination.
	// Mirrors emitBuiltinLen: there is nothing to assemble.
	if inst.Dst <= NoVal {
		return nil
	}
	rt, _ := c.ptype(inst.Args[0])
	if rt != "%str-long" {
		c.fail("builtin %s: unsupported receiver type %s", ff, rt)
		return i18n.Errorf("aywuxg5w", "builtin %s receiver %s: %s", ff, rt, strings.Join(c.errs, "; "))
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return i18n.Errorf("beppq99h", "builtin %s: no result slot", ff)
	}
	if dstLT != "%vec" {
		c.fail("builtin %s: []byte view into %s destination", ff, dstLT)
		return i18n.Errorf("4ccv952m", "builtin %s: cannot store i64 into %s", ff, dstLT)
	}

	// sv is a loaded %str-long value; strLenI64 owns the u32/i64 header
	// widening boundary, so this emitter never spells the storage width.
	_, sv := c.loadVal(inst.Args[0])
	lenV := c.strLenI64(sv)
	dataPtr := c.dataPtrOf(inst.Args[0])
	if dataPtr == "" {
		return i18n.Errorf("j8e31b1f", "builtin %s: receiver has no slot", ff)
	}

	// %vec stores its data pointer as an i64 (see vec_free: inttoptr i64).
	dataI := c.treg("sbd")
	fmt.Fprintf(c.sb, "  %s = ptrtoint i8* %s to i64\n", dataI, dataPtr)
	v0 := c.treg("sbv")
	fmt.Fprintf(c.sb, "  %s = insertvalue %%vec zeroinitializer, i64 %s, 0\n", v0, lenV)
	v1 := c.treg("sbv")
	fmt.Fprintf(c.sb, "  %s = insertvalue %%vec %s, i64 0, 1\n", v1, v0)
	v2 := c.treg("sbv")
	fmt.Fprintf(c.sb, "  %s = insertvalue %%vec %s, i64 %s, 2\n", v2, v1, dataI)
	fmt.Fprintf(c.sb, "  store %%vec %s, %%vec* %s\n", v2, dstSlot)
	return nil
}

// emitBuiltinStrFromBytes lowers "str-from-bytes": str.from-bytes(bs, n).
// Args[0] is a []byte (%vec), Args[1] the i64 length. The result TAKES OVER
// the vec's backing buffer (no copy), so the caller must treat bs as moved.
// n is clamped to [0, len(bs)]: neither a negative nor an over-long length can
// address outside the allocation, and cap is the same clamped value so the
// result never claims more capacity than the buffer really has.
func (c *codegen) emitBuiltinStrFromBytes(inst *Inst) error {
	const ff = "str-from-bytes"
	if len(inst.Args) < 2 {
		return i18n.Errorf("1u2vckag", "builtin %s: missing receiver", ff)
	}
	if inst.Dst <= NoVal {
		return nil
	}
	dstLT, _ := c.ptype(inst.Dst)
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return i18n.Errorf("beppq99h", "builtin %s: no result slot", ff)
	}
	if dstLT != "%str-long" {
		c.fail("builtin %s: result must be str, got %s", ff, dstLT)
		return i18n.Errorf("4ccv952m", "builtin %s: cannot store i64 into %s", ff, dstLT)
	}
	vt, vv := c.loadVal(inst.Args[0])
	if vt != "%vec" {
		c.fail("builtin %s: expected []byte argument, got %s", ff, vt)
		return i18n.Errorf("u3g4qexx", "builtin %s: bad arg type %s", ff, vt)
	}
	// %vec = { i64 len, i64 cap, i64 data }; data is a ptrtoint-encoded pointer.
	vecLen := c.treg("fbl")
	fmt.Fprintf(c.sb, "  %s = extractvalue %%vec %s, 0\n", vecLen, vv)
	vecData := c.treg("fbd")
	fmt.Fprintf(c.sb, "  %s = extractvalue %%vec %s, 2\n", vecData, vv)
	dataPtr := c.treg("fbp")
	fmt.Fprintf(c.sb, "  %s = inttoptr i64 %s to i8*\n", dataPtr, vecData)

	// The result SHARES the vec's buffer. Retain it so BOTH owners release
	// once: the source []byte's @vec_free and the result's @str_free would
	// otherwise free the same block twice (measured: all assertions pass,
	// then `trace/BPT trap` on exit). @vec_retain skips cap==0 borrowed
	// views, so passing a str.bytes() view here is a caller error — it has
	// no ownership to transfer.
	fmt.Fprintf(c.sb, "  call void @vec_retain(%%vec %s)\n", vv)

	nt, nv := c.loadVal(inst.Args[1])
	if nt != "i64" {
		nv = c.coerce(nt, nv, "i64")
		if nv == "" {
			return i18n.Errorf("4ccv952m", "builtin %s: cannot store i64 into %s", ff, nt)
		}
	}
	// n = max(0, min(n, len(bs))). The signed compares are deliberate: an
	// unsigned compare would treat a negative i64 as a huge length.
	neg := c.treg("fbn")
	fmt.Fprintf(c.sb, "  %s = icmp slt i64 %s, 0\n", neg, nv)
	zeroed := c.treg("fbz")
	fmt.Fprintf(c.sb, "  %s = select i1 %s, i64 0, i64 %s\n", zeroed, neg, nv)
	over := c.treg("fbo")
	fmt.Fprintf(c.sb, "  %s = icmp sgt i64 %s, %s\n", over, zeroed, vecLen)
	clamped := c.treg("fbc")
	fmt.Fprintf(c.sb, "  %s = select i1 %s, i64 %s, i64 %s\n", clamped, over, vecLen, zeroed)

	// strLongFromParts owns the u32/i64 header narrowing boundary.
	s := c.strLongFromParts(clamped, clamped, dataPtr)
	fmt.Fprintf(c.sb, "  store %%str-long %s, %%str-long* %s\n", s, dstSlot)
	return nil
}
