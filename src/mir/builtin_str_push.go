package mir

// builtin_str_push.go — str.push-str(): append a str to a str IN PLACE.
//
// Kept in its own file for the same reason as builtin_str_bytes.go: it must
// not touch codegen.go, whose %str-long layout region is under concurrent
// edits. Every layout-sensitive read/write goes through existing helpers
// (strHeaderLoadI64 / strHeaderStoreI64 / ensureStrLongBuffer / strLenI64),
// so this file never spells the u32 header width itself.
//
// WHY A BUILTIN AND NOT STD CODE
// ------------------------------
// The std surface is `str.push(c char)` -> `.push-str(c.to-str())`, i.e. one
// UTF-8 encode per call. Appending a whole str from pure std code would need
// a with-len buffer + copy + concat, which allocates a fresh string on every
// push (O(N^2) for a build-up loop). Growing the existing buffer in place is
// the only way to keep `push` cheap, and growing a %str-long buffer is
// compiler territory (`ensureStrLongBuffer`).
//
// THE 8-STEP EMISSION ORDER (do not reorder)
// ------------------------------------------
//  1. Resolve the receiver's REAL storage (resolveReceiverSlot + a
//     lvalueAddrOf projection guarded by layout equality). A struct-field /
//     container-element receiver reads out a COPY of the %str-long header, so
//     growing the copy would be silently discarded.
//  2. Load the source and @str_retain it BEFORE touching the receiver. This is
//     the only thing that makes `s.push-str(s)` safe: the growth path frees
//     the OLD buffer, and the retained reference keeps it alive until our
//     own @str_free at the end.
//  3. Zero-length source exits immediately. Otherwise a receiver whose data is
//     null would be allocated 1024 bytes for nothing.
//  4. Load the receiver's len / cap / data.
//  5. COW a BORROWED VIEW (cap == 0 && data != null) into an owned buffer
//     BEFORE ensureStrLongBuffer, which deliberately skips that case. The old
//     buffer belongs to someone else and is NOT freed.
//  6. ensureStrLongBuffer(slot, recv, len+slen-1): it needs = idx+1, so this
//     asks for exactly len+slen bytes.
//  7. Re-load data (step 4's value is stale after a grow).
//  8. llvm.memmove (NOT memcpy: self-append overlaps) + store the new length +
//     the paired @str_free.
//
// inst.Dst is NoVal (the method returns nothing), so there is no result slot.

import (
	"fmt"

	"github.com/lizongying/nolang/i18n"
)

// emitBuiltinStrPushBytes lowers the "str-push-bytes" ForwardFunc.
// inst.Args[0] is the str receiver, inst.Args[1] the str to append.
func (c *codegen) emitBuiltinStrPushBytes(inst *Inst) error {
	const ff = "str-push-bytes"
	if len(inst.Args) < 2 {
		return i18n.Errorf("k3xq7vna", "builtin %s: missing receiver or source", ff)
	}
	recv := inst.Args[0]
	src := inst.Args[1]

	// ---- 1. receiver storage ------------------------------------------------
	rt, _ := c.ptype(recv)
	if rt != "%str-long" {
		c.fail("builtin %s: unsupported receiver type %s", ff, rt)
		return i18n.Errorf("p9dt2hwe", "builtin %s receiver %s", ff, rt)
	}
	slot, _ := c.resolveReceiverSlot(recv)
	if slot == "" {
		c.fail("builtin %s: receiver has no slot", ff)
		return i18n.Errorf("rn4mb8sc", "builtin %s: receiver has no slot", ff)
	}
	// Projection write-back: `p.s.push-str(...)` / `arr[0].push-str(...)` hand
	// out a COPY of the header, so the mutation must reach the real field /
	// element storage. GUARD: lvalueAddrOf also serves projections that WIDEN
	// (an i8 element read out as i64), and a %txt field is a DIFFERENT
	// aggregate with the same conceptual role; only accept a projection that
	// preserves the receiver's own storage layout.
	if p, ptid, projected := c.lvalueAddrOf(recv, inst); projected && p != "" {
		if pt := c.mod.Type(ptid); pt != nil && c.llvmTypeOf(pt) == rt {
			slot = p
		}
	}

	// ---- 2. load + retain the source ---------------------------------------
	st, sv := c.loadVal(src)
	if st != "%str-long" {
		c.fail("builtin %s: source is %s, not str", ff, st)
		return i18n.Errorf("w6av3lqe", "builtin %s: bad source type %s", ff, st)
	}
	// Paired with the @str_free in the done block. Both are UNCONDITIONAL:
	// static aliasing between receiver and source cannot be decided here, and
	// both helpers already no-op on cap==0 / null data, so the pair is always
	// balanced.
	fmt.Fprintf(c.sb, "  call void @str_retain(%%str-long %s)\n", sv)
	slen := c.strLenI64(sv)
	sptr := c.treg("spsp")
	fmt.Fprintf(c.sb, "  %s = extractvalue %%str-long %s, 2\n", sptr, sv)

	// ---- 3. zero-length source: nothing to append ---------------------------
	lGo := c.label("spsG")
	lCow := c.label("spsC")
	lEns := c.label("spsE")
	lDone := c.label("spsD")
	empty := c.treg("spsz")
	fmt.Fprintf(c.sb, "  %s = icmp eq i64 %s, 0\n", empty, slen)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", empty, lDone, lGo)

	// ---- 4. receiver len / cap / data --------------------------------------
	fmt.Fprintf(c.sb, "%s:\n", lGo)
	lg := c.treg("spslg")
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 0\n", lg, slot)
	plen := c.strHeaderLoadI64(lg)
	cg := c.treg("spscg")
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 1\n", cg, slot)
	pcap := c.strHeaderLoadI64(cg)
	dg := c.treg("spsdg")
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds %%str-long, %%str-long* %s, i32 0, i32 2\n", dg, slot)
	pdata := c.treg("spsdp")
	fmt.Fprintf(c.sb, "  %s = load i8*, i8** %s\n", pdata, dg)

	// ---- 5. borrowed-view COW ----------------------------------------------
	// cap == 0 && data != null is the borrowed-view marker (a slice/coercion
	// view onto storage this value does not own). ensureStrLongBuffer skips
	// that case on purpose, so the copy-on-write has to happen here first.
	// The old buffer is NOT freed: it belongs to the source value.
	zcap := c.treg("spszc")
	fmt.Fprintf(c.sb, "  %s = icmp eq i64 %s, 0\n", zcap, pcap)
	ndata := c.treg("spsnd")
	fmt.Fprintf(c.sb, "  %s = icmp eq i8* %s, null\n", ndata, pdata)
	hasData := c.treg("spshd")
	fmt.Fprintf(c.sb, "  %s = xor i1 %s, true\n", hasData, ndata)
	view := c.treg("spsvw")
	fmt.Fprintf(c.sb, "  %s = and i1 %s, %s\n", view, zcap, hasData)
	fmt.Fprintf(c.sb, "  br i1 %s, label %%%s, label %%%s\n", view, lCow, lEns)

	fmt.Fprintf(c.sb, "%s:\n", lCow)
	need := c.treg("spsnw")
	fmt.Fprintf(c.sb, "  %s = add i64 %s, %s\n", need, plen, slen)
	overDefault := c.treg("spso")
	fmt.Fprintf(c.sb, "  %s = icmp sgt i64 %s, 1024\n", overDefault, need)
	cowCap := c.treg("spscs")
	fmt.Fprintf(c.sb, "  %s = select i1 %s, i64 %s, i64 1024\n", cowCap, overDefault, need)
	cowBuf := c.treg("spscb")
	fmt.Fprintf(c.sb, "  %s = call i8* @nolang_rc_alloc(i64 %s)\n", cowBuf, cowCap)
	c.decl("declare void @llvm.memset.p0i8.i64(i8*, i8, i64, i1)")
	fmt.Fprintf(c.sb, "  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %s, i1 false)\n", cowBuf, cowCap)
	c.decl("declare void @llvm.memcpy.p0i8.p0i8.i64(i8*, i8*, i64, i1)")
	fmt.Fprintf(c.sb, "  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n", cowBuf, pdata, plen)
	// Write the new descriptor back to the SLOT rather than threading a PHI:
	// ensureStrLongBuffer re-loads data/cap from the slot (codegen.go:10234),
	// so writing back here makes the next step idempotent by construction.
	fmt.Fprintf(c.sb, "  store i8* %s, i8** %s\n", cowBuf, dg)
	c.strHeaderStoreI64(cg, cowCap)
	fmt.Fprintf(c.sb, "  br label %%%s\n", lEns)

	// ---- 6. ensure the buffer can hold len+slen bytes ----------------------
	fmt.Fprintf(c.sb, "%s:\n", lEns)
	// ensureStrLongBuffer computes need = idxV + 1, so pass len+slen-1. Passing
	// len+slen would over-allocate by one byte; passing len would size the
	// buffer for a ONE-byte append and overflow the heap for any longer source.
	lastIdx := c.treg("spsli")
	fmt.Fprintf(c.sb, "  %s = sub i64 %s, 1\n", lastIdx, slen)
	idxV := c.treg("spsix")
	fmt.Fprintf(c.sb, "  %s = add i64 %s, %s\n", idxV, plen, lastIdx)
	c.ensureStrLongBuffer(slot, recv, idxV, "")

	// ---- 7. re-load data (a grow in step 6 replaced it) --------------------
	pdata2 := c.treg("spsd2")
	fmt.Fprintf(c.sb, "  %s = load i8*, i8** %s\n", pdata2, dg)

	// ---- 8. copy, extend the length, release the retained source -----------
	dest := c.treg("spsdt")
	fmt.Fprintf(c.sb, "  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", dest, pdata2, plen)
	// memmove, not memcpy: `t.push-str(t)` makes src and dest overlap.
	fmt.Fprintf(c.sb, "  call void @llvm.memmove.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 false)\n",
		dest, sptr, slen)
	newLen := c.treg("spsnl")
	fmt.Fprintf(c.sb, "  %s = add i64 %s, %s\n", newLen, plen, slen)
	// strHeaderStoreI64, NOT emitExtendLen: the latter writes max(len, idx+1),
	// which is a different (per-element) semantic.
	c.strHeaderStoreI64(lg, newLen)
	fmt.Fprintf(c.sb, "  br label %%%s\n", lDone)

	// ---- done: the unconditional release ----------------------------------
	fmt.Fprintf(c.sb, "%s:\n", lDone)
	fmt.Fprintf(c.sb, "  call void @str_free(%%str-long %s)\n", sv)
	return nil
}
